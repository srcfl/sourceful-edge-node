package luarunner

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/srcfl/sourceful-edge-node/internal/modbus"
	lua "github.com/yuin/gopher-lua"
)

// LiveEvent is emitted during a live driver test.
type LiveEvent struct {
	Type      string     `json:"type"`      // "emission", "log", "error", "command_result", "status"
	Emissions []Emission `json:"emissions,omitempty"`
	Log       string     `json:"log,omitempty"`
	Error     string     `json:"error,omitempty"`
	PollCount int        `json:"poll_count,omitempty"`
}

// CommandRequest is sent from the UI to invoke driver_command.
type CommandRequest struct {
	Action string  `json:"action"`  // "init", "battery", "curtail", "curtail_disable", "deinit"
	PowerW float64 `json:"power_w"` // target power in watts
}

// RunDriverLive executes a driver continuously, streaming emissions.
// Polls at the driver's requested interval until ctx is cancelled.
// Commands can be sent via the commands channel.
func RunDriverLive(ctx context.Context, driverSource string, target TargetConfig, events chan<- LiveEvent, commands <-chan CommandRequest) {
	defer close(events)

	var mu sync.Mutex
	var currentEmissions []Emission

	collectLog := func(msg string) {
		events <- LiveEvent{Type: "log", Log: msg}
	}
	collectEmit := func(derType string, data map[string]float64) {
		mu.Lock()
		currentEmissions = append(currentEmissions, Emission{DERType: derType, Data: data})
		mu.Unlock()
	}

	rt := New()
	RegisterDecodeHostFuncs(rt)
	RegisterStandardHostAPI(rt, EmitFunc(collectEmit), LogFunc(collectLog))

	protocol := DetectProtocol(driverSource)

	if target.Host != "" {
		switch protocol {
		case "mqtt":
			port := target.Port
			if port == 0 {
				port = 1883
			}
			mqttBridge, err := NewMQTTBridge(target.Host, port, target.Username, target.Password)
			if err != nil {
				events <- LiveEvent{Type: "error", Error: fmt.Sprintf("mqtt connect: %v", err)}
				return
			}
			defer mqttBridge.Close()
			mqttBridge.RegisterHostFuncs(rt)
		case "http":
			httpBridge := NewHTTPBridge()
			httpBridge.RegisterHostFuncs(rt)
		default:
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
				events <- LiveEvent{Type: "error", Error: fmt.Sprintf("modbus connect: %v", err)}
				return
			}
			defer client.Close()
			bridge := NewModbusBridge(client)
			bridge.RegisterHostFuncs(rt)
		}
	} else {
		switch protocol {
		case "mqtt":
			RegisterMQTTStubs(rt)
		case "http":
			httpBridge := NewHTTPBridge()
			httpBridge.RegisterHostFuncs(rt)
		}
	}

	if err := rt.Init(); err != nil {
		events <- LiveEvent{Type: "error", Error: fmt.Sprintf("lua init: %v", err)}
		return
	}
	defer rt.Close()

	if err := rt.DoString(driverSource); err != nil {
		events <- LiveEvent{Type: "error", Error: fmt.Sprintf("load driver: %v", err)}
		return
	}

	configTable := buildConfigTable(rt, target)
	if _, err := rt.CallGlobal("driver_init", configTable); err != nil {
		events <- LiveEvent{Type: "error", Error: fmt.Sprintf("driver_init: %v", err)}
		return
	}
	defer rt.CallGlobal("driver_cleanup")

	events <- LiveEvent{Type: "status", Log: "Driver initialized. Polling..."}

	// For MQTT, wait for initial messages
	if protocol == "mqtt" && target.Host != "" {
		time.Sleep(3 * time.Second)
	}

	pollCount := 0
	for {
		if ctx.Err() != nil {
			return
		}

		// Check for commands
		select {
		case cmd, ok := <-commands:
			if ok {
				executeCommand(rt, cmd, events)
			}
		default:
		}

		// Clear emissions for this poll
		mu.Lock()
		currentEmissions = nil
		mu.Unlock()

		// driver_poll()
		ret, err := rt.CallGlobal("driver_poll")
		pollCount++
		if err != nil {
			events <- LiveEvent{Type: "error", Error: fmt.Sprintf("driver_poll[%d]: %v", pollCount, err)}
			// Don't exit — keep polling, transient errors are common
		}

		// Collect emissions from this poll
		mu.Lock()
		if len(currentEmissions) > 0 {
			emCopy := make([]Emission, len(currentEmissions))
			copy(emCopy, currentEmissions)
			events <- LiveEvent{Type: "emission", Emissions: emCopy, PollCount: pollCount}
		}
		mu.Unlock()

		// Determine poll interval from return value
		interval := 5000 * time.Millisecond
		if num, ok := ret.(lua.LNumber); ok && num > 0 {
			ms := int(num)
			if ms < 100 {
				ms = 100
			}
			if ms > 60000 {
				ms = 60000
			}
			interval = time.Duration(ms) * time.Millisecond
		}

		// Wait for interval or context cancellation
		select {
		case <-ctx.Done():
			return
		case <-time.After(interval):
		}
	}
}

func executeCommand(rt *Runtime, cmd CommandRequest, events chan<- LiveEvent) {
	rt.Lock()
	L := rt.State()
	if L == nil {
		rt.Unlock()
		events <- LiveEvent{Type: "error", Error: "runtime not initialized"}
		return
	}

	fn := L.GetGlobal("driver_command")
	if fn == lua.LNil {
		rt.Unlock()
		events <- LiveEvent{Type: "error", Error: "driver_command not defined in this driver"}
		return
	}
	rt.Unlock()

	// Build cmd table
	rt.Lock()
	cmdTable := L.NewTable()
	cmdTable.RawSetString("id", lua.LString("hugin-test"))
	cmdTable.RawSetString("source", lua.LString("hugin"))
	rt.Unlock()

	ret, err := rt.CallGlobal("driver_command", lua.LString(cmd.Action), lua.LNumber(cmd.PowerW), cmdTable)
	if err != nil {
		events <- LiveEvent{Type: "command_result", Error: fmt.Sprintf("driver_command: %v", err)}
		return
	}

	success := ret == lua.LTrue
	events <- LiveEvent{
		Type: "command_result",
		Log:  fmt.Sprintf("command %s(%.0fW) → %v", cmd.Action, cmd.PowerW, success),
	}
}
