package gateway

import (
	"encoding/json"
	"fmt"
	"log"
	"time"

	"github.com/google/uuid"
	"github.com/nats-io/nats.go"
	lua "github.com/yuin/gopher-lua"

	"github.com/srcfl/sourceful-edge-node/internal/luarunner"
	"github.com/srcfl/sourceful-edge-node/internal/messaging"
)

// controlSubscriber manages NATS control subscriptions for a single device/DER.
type controlSubscriber struct {
	serial   string
	deviceSN string
	derType  string
	nc       *nats.Conn
	rt       *luarunner.Runtime
	watchdog *Watchdog
	subs     []*nats.Subscription
}

// setupControl subscribes to control commands and heartbeats for a controllable DER.
// Returns a cleanup function and list of subscriptions.
func setupControl(serial, deviceSN, derType string, nc *nats.Conn, rt *luarunner.Runtime, done <-chan struct{}) (*controlSubscriber, error) {
	cs := &controlSubscriber{
		serial:   serial,
		deviceSN: deviceSN,
		derType:  derType,
		nc:       nc,
		rt:       rt,
	}

	// Watchdog: on expiry, call driver_default_mode()
	cs.watchdog = NewWatchdog(defaultWatchdogTimeout, func() {
		log.Printf("[%s:%s] Watchdog expired — calling driver_default_mode()", deviceSN, derType)
		if _, err := rt.CallGlobal("driver_default_mode"); err != nil {
			log.Printf("[%s:%s] driver_default_mode failed: %v", deviceSN, derType, err)
		}
	})

	// Subscribe to control commands
	cmdSubject := messaging.ControlCommandsSubject(serial, deviceSN, derType)
	cmdSub, err := nc.Subscribe(cmdSubject, func(msg *nats.Msg) {
		cs.handleCommand(msg.Data)
	})
	if err != nil {
		return nil, fmt.Errorf("subscribe commands %s: %w", cmdSubject, err)
	}
	cs.subs = append(cs.subs, cmdSub)
	log.Printf("[%s:%s] Subscribed to control commands: %s", deviceSN, derType, cmdSubject)

	// Subscribe to heartbeats
	hbSubject := messaging.ControlHeartbeatSubject(serial, deviceSN, derType)
	hbSub, err := nc.Subscribe(hbSubject, func(msg *nats.Msg) {
		cs.watchdog.Feed()
	})
	if err != nil {
		cmdSub.Unsubscribe()
		return nil, fmt.Errorf("subscribe heartbeat %s: %w", hbSubject, err)
	}
	cs.subs = append(cs.subs, hbSub)

	// Start watchdog
	go cs.watchdog.Run(done)

	return cs, nil
}

// handleCommand processes an incoming control command.
func (cs *controlSubscriber) handleCommand(data []byte) {
	var cmd messaging.ControlCommand
	if err := json.Unmarshal(data, &cmd); err != nil {
		log.Printf("[%s:%s] Invalid command JSON: %v", cs.deviceSN, cs.derType, err)
		return
	}

	log.Printf("[%s:%s] Received command: action=%s power=%dW id=%s", cs.deviceSN, cs.derType, cmd.Action, cmd.PowerW, cmd.ID)

	// Check expiration
	if cmd.ExpiresAt > 0 && time.Now().UnixMilli() > cmd.ExpiresAt {
		log.Printf("[%s:%s] Command %s expired, ignoring", cs.deviceSN, cs.derType, cmd.ID)
		cs.publishAck(cmd.ID, "failed", "command expired")
		return
	}

	// ACK receipt
	cs.publishAck(cmd.ID, "ack", "")

	// Build Lua command table
	cs.rt.Lock()
	L := cs.rt.State()
	cmdTable := L.NewTable()
	cmdTable.RawSetString("id", lua.LString(cmd.ID))
	cmdTable.RawSetString("source", lua.LString(cmd.Source))
	cmdTable.RawSetString("duration_ms", lua.LNumber(cmd.DurationMs))
	cmdTable.RawSetString("expires_at", lua.LNumber(cmd.ExpiresAt))
	cs.rt.Unlock()

	// Call driver_command(action, power_w, cmd_table)
	ret, err := cs.rt.CallGlobal("driver_command",
		lua.LString(cmd.Action),
		lua.LNumber(cmd.PowerW),
		cmdTable,
	)
	if err != nil {
		log.Printf("[%s:%s] driver_command failed: %v", cs.deviceSN, cs.derType, err)
		cs.publishAck(cmd.ID, "failed", err.Error())
		return
	}

	// Check return value
	if ret == lua.LFalse {
		cs.publishAck(cmd.ID, "failed", "driver returned false")
	} else {
		cs.publishAck(cmd.ID, "executed", "")
	}
}

// publishAck sends a control acknowledgment to NATS.
func (cs *controlSubscriber) publishAck(cmdID, status, errMsg string) {
	if cs.nc == nil || !cs.nc.IsConnected() {
		return
	}

	ack := messaging.ControlAck{
		ID:        uuid.New().String(),
		Version:   1,
		Timestamp: time.Now().UnixMilli(),
		Source:    "hugin-gateway",
		Ref:       cmdID,
		Status:    status,
		Error:     errMsg,
	}

	data, err := json.Marshal(ack)
	if err != nil {
		return
	}

	subject := messaging.ControlAckSubject(cs.serial, cs.deviceSN, cs.derType)
	cs.nc.Publish(subject, data)
}

// Close unsubscribes from all NATS subjects.
func (cs *controlSubscriber) Close() {
	for _, sub := range cs.subs {
		sub.Unsubscribe()
	}
}
