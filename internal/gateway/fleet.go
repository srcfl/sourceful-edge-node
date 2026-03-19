package gateway

import (
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	"github.com/nats-io/nats.go"
)

// FleetMonitor subscribes to telemetry from all gateways visible via NATS.
// Also dispatches probe responses piggy-backing on the same subscription.
type FleetMonitor struct {
	nc     *nats.Conn
	serial string // our own serial (to exclude from fleet view)
	sub    *nats.Subscription

	mu       sync.RWMutex
	gateways map[string]*GatewayState

	// Probe response dispatch (piggybacks on gateways.> subscription)
	probeMu      sync.Mutex
	probeWaiters map[string]chan json.RawMessage
}

// GatewayState tracks the live state of a remote gateway.
type GatewayState struct {
	Serial   string                `json:"serial"`
	LastSeen time.Time             `json:"last_seen"`
	Devices  map[string]*DeviceState `json:"devices"`
}

// DeviceState tracks a single device on a remote gateway.
type DeviceState struct {
	DeviceID string             `json:"device_id"`
	DERs     map[string]*DERState `json:"ders"`
	LastSeen time.Time          `json:"last_seen"`
}

// DERState tracks the latest telemetry for a DER.
type DERState struct {
	DERType   string         `json:"der_type"`
	Data      map[string]any `json:"data"`
	Timestamp time.Time      `json:"timestamp"`
}

// NewFleetMonitor creates a fleet monitor that subscribes to all gateway telemetry.
func NewFleetMonitor(nc *nats.Conn, ownSerial string) (*FleetMonitor, error) {
	fm := &FleetMonitor{
		nc:           nc,
		serial:       ownSerial,
		gateways:     make(map[string]*GatewayState),
		probeWaiters: make(map[string]chan json.RawMessage),
	}

	// Subscribe to all gateway telemetry and state
	sub, err := nc.Subscribe("gateways.>", func(msg *nats.Msg) {
		fm.handleMessage(msg)
	})
	if err != nil {
		return nil, err
	}
	fm.sub = sub

	log.Printf("Fleet monitor: subscribed to gateways.>")
	return fm, nil
}

func (fm *FleetMonitor) handleMessage(msg *nats.Msg) {
	// Parse subject: gateways.{serial}.devices.{deviceID}.ders.{derType}.telemetry.json.v1
	// Or: gateways.{serial}.state.json.v1
	// Or: gateways.{serial}.logs.*
	parts := strings.Split(msg.Subject, ".")
	if len(parts) < 2 {
		return
	}
	gwSerial := parts[1]

	// Skip our own telemetry
	if gwSerial == fm.serial {
		return
	}

	fm.mu.Lock()
	defer fm.mu.Unlock()

	gw, exists := fm.gateways[gwSerial]
	if !exists {
		gw = &GatewayState{
			Serial:  gwSerial,
			Devices: make(map[string]*DeviceState),
		}
		fm.gateways[gwSerial] = gw
	}
	// Dispatch probe responses: gateways.{serial}.hugin.responses
	if len(parts) >= 4 && parts[2] == "hugin" && parts[3] == "responses" {
		fm.dispatchProbeResponse(msg.Data)
		return
	}

	// Only track telemetry messages, ignore logs/state/events
	if len(parts) < 7 || parts[2] != "devices" || parts[4] != "ders" || parts[6] != "telemetry" {
		return
	}

	// Parse telemetry: gateways.{serial}.devices.{deviceID}.ders.{derType}.telemetry.json.v1
	{
		gw.LastSeen = time.Now()

		deviceID := parts[3]
		derType := parts[5]

		dev, exists := gw.Devices[deviceID]
		if !exists {
			dev = &DeviceState{
				DeviceID: deviceID,
				DERs:     make(map[string]*DERState),
			}
			gw.Devices[deviceID] = dev
		}
		dev.LastSeen = time.Now()

		var data map[string]any
		if err := json.Unmarshal(msg.Data, &data); err == nil {
			dev.DERs[derType] = &DERState{
				DERType:   derType,
				Data:      data,
				Timestamp: time.Now(),
			}
		}
	}
}

// Snapshot returns the current fleet state.
func (fm *FleetMonitor) Snapshot() []GatewayState {
	fm.mu.RLock()
	defer fm.mu.RUnlock()

	result := make([]GatewayState, 0, len(fm.gateways))
	for _, gw := range fm.gateways {
		// Deep copy
		gwCopy := GatewayState{
			Serial:   gw.Serial,
			LastSeen: gw.LastSeen,
			Devices:  make(map[string]*DeviceState),
		}
		for did, dev := range gw.Devices {
			devCopy := &DeviceState{
				DeviceID: dev.DeviceID,
				LastSeen: dev.LastSeen,
				DERs:     make(map[string]*DERState),
			}
			for dt, der := range dev.DERs {
				derCopy := *der
				devCopy.DERs[dt] = &derCopy
			}
			gwCopy.Devices[did] = devCopy
		}
		result = append(result, gwCopy)
	}
	return result
}

// WaitForProbeResponse registers a waiter for a probe response by ID.
func (fm *FleetMonitor) WaitForProbeResponse(id string, timeout time.Duration) (json.RawMessage, error) {
	ch := make(chan json.RawMessage, 1)
	fm.probeMu.Lock()
	fm.probeWaiters[id] = ch
	fm.probeMu.Unlock()

	defer func() {
		fm.probeMu.Lock()
		delete(fm.probeWaiters, id)
		fm.probeMu.Unlock()
	}()

	select {
	case data := <-ch:
		return data, nil
	case <-time.After(timeout):
		return nil, fmt.Errorf("timeout after %s", timeout)
	}
}

func (fm *FleetMonitor) dispatchProbeResponse(data []byte) {
	// Extract ID from response JSON
	var resp struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(data, &resp); err != nil || resp.ID == "" {
		return
	}

	fm.probeMu.Lock()
	ch, ok := fm.probeWaiters[resp.ID]
	if ok {
		delete(fm.probeWaiters, resp.ID)
	}
	fm.probeMu.Unlock()

	if ok {
		ch <- data
	}
}

// Close unsubscribes from NATS.
func (fm *FleetMonitor) Close() {
	if fm.sub != nil {
		fm.sub.Unsubscribe()
	}
}
