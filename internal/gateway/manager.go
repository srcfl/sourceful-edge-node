package gateway

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/srcfl/sourceful-edge-node/internal/drivers"
	"github.com/srcfl/sourceful-edge-node/internal/luarunner"
	"github.com/srcfl/sourceful-edge-node/internal/messaging"
	"github.com/srcfl/sourceful-edge-node/internal/modbus"
)

// DeviceManager manages the lifecycle of all device runtimes.
type DeviceManager struct {
	serial     string
	nc         *nats.Conn
	drivers    *drivers.Store
	dataDir    string
	configPath string

	mu      sync.RWMutex
	devices map[string]*managedDevice
}


type managedDevice struct {
	cfg    DeviceConfig
	cancel context.CancelFunc
	make_  string // set by host.set_make()

	mu        sync.RWMutex
	connected bool
	lastErr   string
}

// NewDeviceManager creates a new device manager.
func NewDeviceManager(serial string, nc *nats.Conn, ds *drivers.Store, dataDir, configPath string) *DeviceManager {
	return &DeviceManager{
		serial:     serial,
		nc:         nc,
		drivers:    ds,
		dataDir:    dataDir,
		configPath: configPath,
		devices:    make(map[string]*managedDevice),
	}
}

// Start loads device config and starts all device runtimes.
func (dm *DeviceManager) Start(ctx context.Context) error {
	// Always start gateway state publisher — even with 0 devices,
	// state messages are needed for fleet monitor discovery.
	go dm.publishState(ctx)

	cfg, err := LoadDevicesConfig(dm.configPath)
	if err != nil {
		return fmt.Errorf("load devices config: %w", err)
	}

	if len(cfg.Devices) == 0 {
		log.Println("No devices configured in", dm.configPath)
		return nil
	}

	dm.mu.Lock()
	defer dm.mu.Unlock()

	for _, devCfg := range cfg.Devices {
		if devCfg.SN == "" {
			log.Printf("Skipping device with no SN")
			continue
		}
		dm.startDevice(ctx, devCfg)
	}

	log.Printf("DeviceManager started %d devices", len(dm.devices))

	return nil
}

func (dm *DeviceManager) startDevice(ctx context.Context, cfg DeviceConfig) {
	devCtx, cancel := context.WithCancel(ctx)
	md := &managedDevice{cfg: cfg, cancel: cancel}
	dm.devices[cfg.SN] = md

	go dm.runDevice(devCtx, md)
}

// runDevice is the goroutine that manages a single device's lifecycle.
func (dm *DeviceManager) runDevice(ctx context.Context, md *managedDevice) {
	cfg := md.cfg
	log.Printf("[%s] Starting device (driver: %s, host: %s:%d)", cfg.SN, cfg.RegisterMap, cfg.Host, cfg.Port)

	// Resolve driver source
	source, err := dm.loadDriverSource(cfg.RegisterMap)
	if err != nil {
		log.Printf("[%s] Failed to load driver: %v", cfg.SN, err)
		md.mu.Lock()
		md.lastErr = err.Error()
		md.mu.Unlock()
		return
	}

	// Detect protocol
	protocol := luarunner.DetectProtocol(source)
	log.Printf("[%s] Protocol: %s", cfg.SN, protocol)

	// Create emit callback → NATS publish
	emitFn := func(derType string, data map[string]float64) {
		if !cfg.DEREnabled(derType) {
			return
		}
		dm.publishTelemetry(cfg.SN, derType, md.make_, data)
	}

	// Create runtime
	rt := luarunner.New()
	luarunner.RegisterDecodeHostFuncs(rt)
	luarunner.RegisterStandardHostAPI(rt, emitFn, func(msg string) {
		log.Printf("[%s:lua] %s", cfg.SN, msg)
	})

	// Set up protocol bridge (skip if no host configured — standalone driver)
	var cleanup func()
	if cfg.Host != "" {
		switch protocol {
		case "modbus":
			client, bridge, cl := dm.setupModbus(cfg, rt)
			if client == nil {
				md.mu.Lock()
				md.lastErr = "modbus connect failed"
				md.mu.Unlock()
				dm.retryLoop(ctx, md, source, emitFn, protocol)
				return
			}
			cleanup = cl
			_ = bridge
		case "mqtt":
			port := cfg.Port
			if port == 0 {
				port = 1883
			}
			mqttBridge, err := luarunner.NewMQTTBridge(cfg.Host, port, cfg.Username, cfg.Password)
			if err != nil {
				log.Printf("[%s] MQTT connect failed: %v", cfg.SN, err)
				md.mu.Lock()
				md.lastErr = err.Error()
				md.mu.Unlock()
				dm.retryLoop(ctx, md, source, emitFn, protocol)
				return
			}
			mqttBridge.RegisterHostFuncs(rt)
			cleanup = func() { mqttBridge.Close() }
		case "http":
			httpBridge := luarunner.NewHTTPBridge()
			httpBridge.RegisterHostFuncs(rt)
			cleanup = func() {}
		default:
			cleanup = func() {}
		}
	} else {
		log.Printf("[%s] No host configured — standalone driver mode", cfg.SN)
		cleanup = func() {}
	}
	defer func() {
		if cleanup != nil {
			cleanup()
		}
	}()

	// Init VM
	if err := rt.Init(); err != nil {
		log.Printf("[%s] Lua init failed: %v", cfg.SN, err)
		md.mu.Lock()
		md.lastErr = err.Error()
		md.mu.Unlock()
		return
	}
	defer rt.Close()

	// Load driver
	if err := rt.DoString(source); err != nil {
		log.Printf("[%s] Load driver failed: %v", cfg.SN, err)
		md.mu.Lock()
		md.lastErr = err.Error()
		md.mu.Unlock()
		return
	}

	// Build config table
	rt.Lock()
	L := rt.State()
	configTbl := L.NewTable()
	configTbl.RawSetString("host", luaString(cfg.Host))
	configTbl.RawSetString("port", luaNumber(cfg.Port))
	configTbl.RawSetString("unit_id", luaNumber(cfg.UnitID))
	rt.Unlock()

	// driver_init(config)
	if _, err := rt.CallGlobal("driver_init", configTbl); err != nil {
		log.Printf("[%s] driver_init failed: %v", cfg.SN, err)
		// Continue anyway — some drivers don't need init
	}

	// Read make
	if makeRaw, ok := rt.GetUserData("driver_make"); ok {
		if m, ok := makeRaw.(string); ok {
			md.mu.Lock()
			md.make_ = m
			md.mu.Unlock()
		}
	}

	md.mu.Lock()
	md.connected = true
	md.lastErr = ""
	md.mu.Unlock()

	// Set up control subscriptions for controllable DERs
	done := make(chan struct{})
	defer close(done)
	var controlSubs []*controlSubscriber

	if dm.nc != nil {
		for _, der := range cfg.DERs {
			if !der.Enabled {
				continue
			}
			// Only battery and v2x_charger are controllable
			if der.Type != "battery" && der.Type != "v2x_charger" {
				continue
			}
			cs, err := setupControl(dm.serial, cfg.SN, der.Type, dm.nc, rt, done)
			if err != nil {
				log.Printf("[%s] Control setup for %s failed: %v", cfg.SN, der.Type, err)
				continue
			}
			controlSubs = append(controlSubs, cs)
		}
	}
	defer func() {
		for _, cs := range controlSubs {
			cs.Close()
		}
	}()

	log.Printf("[%s] Device ready, starting poll loop (control subs: %d)", cfg.SN, len(controlSubs))

	// Poll loop
	interval := 5 * time.Second
	consecutiveErrors := 0

	for {
		select {
		case <-ctx.Done():
			rt.CallGlobal("driver_cleanup")
			md.mu.Lock()
			md.connected = false
			md.mu.Unlock()
			log.Printf("[%s] Stopped", cfg.SN)
			return
		default:
		}

		ret, err := rt.CallGlobal("driver_poll")
		if err != nil {
			consecutiveErrors++
			log.Printf("[%s] driver_poll error (%d): %v", cfg.SN, consecutiveErrors, err)
			md.mu.Lock()
			md.lastErr = err.Error()
			md.connected = false
			md.mu.Unlock()

			backoff := 10 * time.Second
			if consecutiveErrors >= 10 {
				backoff = 60 * time.Second
			}

			select {
			case <-ctx.Done():
				return
			case <-time.After(backoff):
			}
			continue
		}

		consecutiveErrors = 0
		md.mu.Lock()
		md.connected = true
		md.lastErr = ""
		md.mu.Unlock()

		// driver_poll can return interval in ms
		if ret != nil {
			if num, ok := ret.(interface{ String() string }); ok {
				_ = num // gopher-lua returns LNumber
			}
		}
		// For simplicity, parse returned value
		if lnum, ok := luarunner.ToFloat64(ret); ok && lnum > 0 {
			newInterval := time.Duration(lnum) * time.Millisecond
			if newInterval >= 100*time.Millisecond && newInterval <= 60*time.Second {
				interval = newInterval
			}
		}

		select {
		case <-ctx.Done():
			rt.CallGlobal("driver_cleanup")
			return
		case <-time.After(interval):
		}
	}
}

func (dm *DeviceManager) setupModbus(cfg DeviceConfig, rt *luarunner.Runtime) (*modbus.Client, *luarunner.ModbusBridge, func()) {
	port := cfg.Port
	if port == 0 {
		port = 502
	}
	slaveID := byte(cfg.UnitID)
	if slaveID == 0 {
		slaveID = 1
	}

	client, err := modbus.NewTCPClientWithTimeout(cfg.Host, port, slaveID, 5*time.Second)
	if err != nil {
		log.Printf("[%s] Modbus connect failed: %v", cfg.SN, err)
		return nil, nil, nil
	}

	bridge := luarunner.NewModbusBridge(client)
	bridge.RegisterHostFuncs(rt)
	return client, bridge, func() { client.Close() }
}

// retryLoop handles reconnection attempts for failed device startup.
func (dm *DeviceManager) retryLoop(ctx context.Context, md *managedDevice, source string, emitFn luarunner.EmitFunc, protocol string) {
	for {
		select {
		case <-ctx.Done():
			return
		case <-time.After(30 * time.Second):
		}
		log.Printf("[%s] Retrying connection...", md.cfg.SN)
		// Re-run the full device startup
		go dm.runDevice(ctx, md)
		return
	}
}

func (dm *DeviceManager) loadDriverSource(registerMap string) (string, error) {
	// Try driver store first (device-support/drivers/)
	name := registerMap
	// Strip .lua extension if present
	if len(name) > 4 && name[len(name)-4:] == ".lua" {
		name = name[:len(name)-4]
	}

	if d, ok := dm.drivers.Get(name); ok {
		return d.Source, nil
	}

	// Try data dir
	path := filepath.Join(dm.dataDir, "drivers", registerMap)
	data, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("driver %q not found in store or %s: %w", registerMap, path, err)
	}
	return string(data), nil
}

// publishTelemetry maps emit data to telemetry structs and publishes to NATS.
func (dm *DeviceManager) publishTelemetry(deviceSN, derType, make_ string, data map[string]float64) {
	if dm.nc == nil || !dm.nc.IsConnected() {
		return
	}

	subject := messaging.TelemetrySubject(dm.serial, deviceSN, derType)
	now := time.Now().Unix()

	var payload any
	switch derType {
	case "meter":
		payload = messaging.MeterTelemetry{
			Type:          "meter",
			Timestamp:     now,
			Make:          make_,
			W:             data["w"],
			Hz:            data["hz"],
			L1W:           data["l1_w"],
			L2W:           data["l2_w"],
			L3W:           data["l3_w"],
			L1V:           data["l1_v"],
			L2V:           data["l2_v"],
			L3V:           data["l3_v"],
			L1A:           data["l1_a"],
			L2A:           data["l2_a"],
			L3A:           data["l3_a"],
			TotalImportWh: data["import_wh"],
			TotalExportWh: data["export_wh"],
		}
	case "pv":
		payload = messaging.SolarTelemetry{
			Type:      "pv",
			Timestamp: now,
			Make:      make_,
			W:         data["w"],
			Mppt1V:    data["mppt1_v"],
			Mppt1A:    data["mppt1_a"],
			Mppt2V:    data["mppt2_v"],
			Mppt2A:    data["mppt2_a"],
		}
	case "battery":
		payload = messaging.BatteryTelemetry{
			Type:             "battery",
			Timestamp:        now,
			Make:             make_,
			W:                data["w"],
			V:                data["v"],
			A:                data["a"],
			SoCNomFract:      data["soc"],
			HeatsinkC:        data["temp_c"],
			TotalChargeWh:    int64(data["charge_wh"]),
			TotalDischargeWh: int64(data["discharge_wh"]),
			UpperLimitW:      data["upper_limit_w"],
			LowerLimitW:      data["lower_limit_w"],
		}
	default:
		return
	}

	jsonData, err := json.Marshal(payload)
	if err != nil {
		log.Printf("[%s] marshal telemetry: %v", deviceSN, err)
		return
	}

	if dm.nc != nil && dm.nc.IsConnected() {
		if err := dm.nc.Publish(subject, jsonData); err != nil {
			log.Printf("[%s] publish telemetry (local): %v", deviceSN, err)
		}
	}

}

// publishState publishes gateway state periodically.
func (dm *DeviceManager) publishState(ctx context.Context) {
	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			dm.doPublishState()
		}
	}
}

func (dm *DeviceManager) doPublishState() {
	if dm.nc == nil || !dm.nc.IsConnected() {
		return
	}

	dm.mu.RLock()
	type devState struct {
		SN        string   `json:"sn"`
		Connected bool     `json:"connected"`
		DERs      []string `json:"ders"`
		Error     string   `json:"error,omitempty"`
	}
	var devStates []devState
	for _, md := range dm.devices {
		md.mu.RLock()
		ds := devState{
			SN:        md.cfg.SN,
			Connected: md.connected,
			Error:     md.lastErr,
		}
		for _, d := range md.cfg.DERs {
			if d.Enabled {
				ds.DERs = append(ds.DERs, d.Type)
			}
		}
		md.mu.RUnlock()
		devStates = append(devStates, ds)
	}
	dm.mu.RUnlock()

	state := map[string]any{
		"serial":    dm.serial,
		"version":   "dev",
		"platform":  fmt.Sprintf("%s-%s", runtimeGOOS(), runtimeGOARCH()),
		"devices":   devStates,
		"timestamp": time.Now().Unix(),
	}

	data, err := json.Marshal(state)
	if err != nil {
		return
	}
	stateSubject := messaging.GatewayStateSubject(dm.serial)
	if dm.nc != nil && dm.nc.IsConnected() {
		dm.nc.Publish(stateSubject, data)
	}
}

// Shutdown stops all devices.
func (dm *DeviceManager) Shutdown() {
	dm.mu.Lock()
	defer dm.mu.Unlock()
	for sn, md := range dm.devices {
		log.Printf("[%s] Stopping device", sn)
		md.cancel()
	}
}

// Status returns the current status of all devices.
func (dm *DeviceManager) Status() []map[string]any {
	dm.mu.RLock()
	defer dm.mu.RUnlock()

	var result []map[string]any
	for _, md := range dm.devices {
		md.mu.RLock()
		result = append(result, map[string]any{
			"sn":         md.cfg.SN,
			"driver":     md.cfg.RegisterMap,
			"host":       md.cfg.Host,
			"port":       md.cfg.Port,
			"connected":  md.connected,
			"last_error": md.lastErr,
			"make":       md.make_,
		})
		md.mu.RUnlock()
	}
	return result
}
