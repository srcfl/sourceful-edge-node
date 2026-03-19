package luarunner

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/srcfl/sourceful-edge-node/internal/modbus"

	lua "github.com/yuin/gopher-lua"
)

// Emission represents a single host.emit() call from a driver.
type Emission struct {
	DERType string             `json:"der_type"`
	Data    map[string]float64 `json:"data"`
}

// TestResult contains the outcome of running a driver.
type TestResult struct {
	Success   bool      `json:"success"`
	Emissions []Emission `json:"emissions"`
	Logs      []string  `json:"logs"`
	Errors    []string  `json:"errors"`
	Duration  string    `json:"duration"`
}

// TargetConfig describes the target device for driver testing.
type TargetConfig struct {
	Host     string `json:"host"`
	Port     int    `json:"port"`
	SlaveID  int    `json:"slave_id"`
	Username string `json:"username,omitempty"`
	Password string `json:"password,omitempty"`
}

// RunDriver executes a Lua driver source against a target device and collects results.
func RunDriver(ctx context.Context, driverSource string, target TargetConfig) *TestResult {
	start := time.Now()
	result := &TestResult{}

	var mu sync.Mutex
	collectLog := func(msg string) {
		mu.Lock()
		defer mu.Unlock()
		result.Logs = append(result.Logs, msg)
	}
	collectEmit := func(derType string, data map[string]float64) {
		mu.Lock()
		defer mu.Unlock()
		result.Emissions = append(result.Emissions, Emission{DERType: derType, Data: data})
	}

	// Create and configure the runtime
	rt := New()
	RegisterDecodeHostFuncs(rt)
	RegisterStandardHostAPI(rt, EmitFunc(collectEmit), LogFunc(collectLog))

	// Detect protocol from driver source
	protocol := DetectProtocol(driverSource)

	// Connect to device based on protocol
	if target.Host != "" {
		switch protocol {
		case "mqtt":
			port := target.Port
			if port == 0 {
				port = 1883
			}
			mqttBridge, err := NewMQTTBridge(target.Host, port, target.Username, target.Password)
			if err != nil {
				result.Errors = append(result.Errors, fmt.Sprintf("mqtt connect: %v", err))
				result.Duration = time.Since(start).String()
				return result
			}
			defer mqttBridge.Close()
			mqttBridge.RegisterHostFuncs(rt)
		case "http":
			httpBridge := NewHTTPBridge()
			httpBridge.RegisterHostFuncs(rt)
		default: // modbus
			port := target.Port
			if port == 0 {
				port = 502
			}
			slaveID := byte(target.SlaveID)
			if slaveID == 0 {
				slaveID = 1
			}
			client, err := modbus.NewTCPClientWithTimeout(target.Host, port, slaveID, 5*time.Second)
			if err != nil {
				result.Errors = append(result.Errors, fmt.Sprintf("modbus connect: %v", err))
				result.Duration = time.Since(start).String()
				return result
			}
			defer client.Close()
			bridge := NewModbusBridge(client)
			bridge.RegisterHostFuncs(rt)
		}
	} else {
		// No host — register stubs for non-modbus protocols so drivers don't crash
		switch protocol {
		case "mqtt":
			RegisterMQTTStubs(rt)
		case "http":
			httpBridge := NewHTTPBridge()
			httpBridge.RegisterHostFuncs(rt)
		}
	}

	if err := rt.Init(); err != nil {
		result.Errors = append(result.Errors, fmt.Sprintf("lua init: %v", err))
		result.Duration = time.Since(start).String()
		return result
	}
	defer rt.Close()

	// Load driver source
	if err := rt.DoString(driverSource); err != nil {
		result.Errors = append(result.Errors, fmt.Sprintf("load driver: %v", err))
		result.Duration = time.Since(start).String()
		return result
	}

	// Build config table for driver_init
	configTable := buildConfigTable(rt, target)

	// driver_init(config)
	if _, err := rt.CallGlobal("driver_init", configTable); err != nil {
		result.Errors = append(result.Errors, fmt.Sprintf("driver_init: %v", err))
		result.Duration = time.Since(start).String()
		return result
	}

	// For MQTT drivers, wait for messages then poll multiple times
	if protocol == "mqtt" && target.Host != "" {
		time.Sleep(3 * time.Second)

		// Poll up to 3 times with 2s waits to catch data
		var lastErr error
		for i := 0; i < 3; i++ {
			_, lastErr = rt.CallGlobal("driver_poll")
			if lastErr != nil {
				result.Errors = append(result.Errors, fmt.Sprintf("driver_poll[%d]: %v", i, lastErr))
				break
			}
			if len(result.Emissions) > 0 {
				break // Got data, done
			}
			if i < 2 {
				time.Sleep(2 * time.Second)
			}
		}
		rt.CallGlobal("driver_cleanup")
		result.Success = lastErr == nil && len(result.Errors) == 0
		result.Duration = time.Since(start).String()
		return result
	}

	// driver_poll()
	_, err := rt.CallGlobal("driver_poll")
	if err != nil {
		result.Errors = append(result.Errors, fmt.Sprintf("driver_poll: %v", err))
	}

	// driver_cleanup() — optional, don't fail if missing
	rt.CallGlobal("driver_cleanup")

	result.Success = err == nil && len(result.Errors) == 0
	result.Duration = time.Since(start).String()
	return result
}

// DetectProtocol scans the driver source for PROTOCOL = "..." to determine
// which transport to use.
func DetectProtocol(source string) string {
	for _, line := range strings.Split(source, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "PROTOCOL") && strings.Contains(trimmed, "=") {
			if strings.Contains(trimmed, `"mqtt"`) || strings.Contains(trimmed, `'mqtt'`) {
				return "mqtt"
			}
			if strings.Contains(trimmed, `"http"`) || strings.Contains(trimmed, `'http'`) {
				return "http"
			}
			if strings.Contains(trimmed, `"serial"`) || strings.Contains(trimmed, `'serial'`) || strings.Contains(trimmed, `"p1"`) || strings.Contains(trimmed, `'p1'`) {
				return "serial"
			}
		}
	}
	return "modbus"
}

func buildConfigTable(rt *Runtime, target TargetConfig) lua.LValue {
	rt.Lock()
	defer rt.Unlock()
	L := rt.State()
	if L == nil {
		return lua.LNil
	}
	tbl := L.NewTable()
	tbl.RawSetString("host", lua.LString(target.Host))
	tbl.RawSetString("port", lua.LNumber(target.Port))
	tbl.RawSetString("slave_id", lua.LNumber(target.SlaveID))
	return tbl
}
