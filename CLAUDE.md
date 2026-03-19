# Sourceful Edge Node — AI-First Development Guide

## What This Is

Universal Go binary that turns any machine (laptop, Raspberry Pi, industrial server, Zap, Blaxt) into a Sourceful energy gateway. One binary, zero dependencies.

**Core loop:** Load Lua drivers → Poll energy devices → Publish telemetry via NATS → Handle grid control commands locally with watchdog failsafe.

## Running

```bash
make build                # Build for current platform → bin/edge-node
make build-all            # Cross-compile (Linux amd64/arm64, macOS arm64, Windows amd64)
make test                 # Run tests

# Standalone (embedded NATS)
./bin/edge-node

# Connect to Sourceful testnet
./bin/edge-node --nats-url wss://novacore-testnet.sourceful.dev:4443

# With drivers and device config
./bin/edge-node --drivers ./device-support/drivers/ --devices ~/.sourceful/devices.json
```

## Architecture

```
cmd/edge-node/
  main.go                     # Bootstrap: identity → NATS → drivers → probe handler → device manager

internal/
  gateway/
    nats.go                   # NATS connection (external or embedded server + leaf node)
    manager.go                # DeviceManager: load config, start devices, poll loop, telemetry publish
    control.go                # Control commands: NATS subscriptions, driver_command(), watchdog integration
    watchdog.go               # 60s watchdog → driver_default_mode() on expiry
    config.go                 # devices.json config structs + loader
    fleet.go                  # FleetMonitor: discover peer gateways via NATS, probe response dispatch
    probe_handler.go          # 10 probe methods for remote Hugin AI integration
    helpers.go                # Lua/runtime helpers

  luarunner/
    runtime.go                # Sandboxed Lua 5.1 VM lifecycle (gopher-lua)
    decode.go                 # 9 decode helpers (i16, u32, f32, etc.)
    hostapi.go                # host.log, host.millis, host.emit, host.set_make
    modbus_bridge.go          # host.modbus_read/write/write_multiple → goburrow/modbus
    mqtt_bridge.go            # host.mqtt_connect/subscribe/publish → paho
    http_bridge.go            # host.http_get/post → net/http
    live.go                   # Live VM for continuous operation (used by DeviceManager)
    runner.go                 # One-shot RunDriver() test harness

  drivers/
    loader.go                 # Store: Load, List, Get from filesystem (manifests/*.yaml + drivers/*.lua)
    manifest.go               # V2 YAML manifest struct

  identity/
    identity.go               # ES256 keypair generation, serial derivation (hostname + MAC → hugin-{8hex})
    jwt.go                    # JWT generation for NovaCore auth-callout

  messaging/
    subjects.go               # NATS subject builders (telemetry, control, state, probe)
    telemetry.go              # DER telemetry structs (meter, pv, battery, v2x_charger)
    control.go                # Control command/ack/heartbeat structs

  modbus/
    client.go                 # Modbus TCP client wrapper (goburrow/modbus)
    registers.go              # Register utilities (BytesToRegisters, etc.)
```

## Dependencies

| Package | Purpose |
|---------|---------|
| `goburrow/modbus` | Modbus TCP client (same lib as Zap/Blaxt firmware) |
| `yuin/gopher-lua` | Lua 5.1 VM (sandboxed, same VM as Blaxt production) |
| `nats-io/nats.go` | NATS client |
| `nats-io/nats-server/v2` | Embedded NATS server (standalone mode) |
| `eclipse/paho.mqtt.golang` | MQTT client for MQTT-based device drivers |
| `google/uuid` | UUID generation for control acks |
| `gopkg.in/yaml.v3` | YAML manifest parsing |

## NATS Subjects

### Published by Edge Node
| Subject | Purpose |
|---------|---------|
| `gateways.{serial}.devices.{deviceID}.ders.{derType}.telemetry.json.v1` | DER telemetry (every poll) |
| `gateways.{serial}.state.json.v1` | Gateway state (every 15s) |
| `gateways.{serial}.devices.{deviceID}.ders.{derType}.control.ems.ack` | Control command acks |

### Subscribed by Edge Node
| Subject | Purpose |
|---------|---------|
| `gateways.{serial}.hugin.probe` | Hugin remote probe requests |
| `gateways.{serial}.devices.{deviceID}.ders.{derType}.control.ems.commands` | Grid control commands |
| `gateways.{serial}.devices.{deviceID}.ders.{derType}.control.ems.heartbeat` | Watchdog heartbeat |
| `gateways.>` | Fleet monitor (discover peer gateways) |

## Probe Handler Methods

The probe handler makes this binary controllable by Hugin for remote AI-driven device development:

| Method | Description |
|--------|-------------|
| `tcp_test` | TCP connectivity check |
| `detect_device` | Modbus SunSpec auto-detection |
| `read_registers` | Read holding/input registers |
| `scan_register_range` | Bulk scan for non-zero registers (50/chunk) |
| `network_scan` | TCP port scan on subnet (32 workers) |
| `rest_fetch` | HTTP GET/POST to URL |
| `rest_inspect` | Probe common REST endpoints |
| `mqtt_connect` | MQTT broker connectivity test |
| `mqtt_subscribe` | Subscribe to topic (5-30s, max 50 samples) |
| `list_devices` | List configured devices |

## Device Configuration (devices.json)

```json
{
  "devices": [
    {
      "type": "lua",
      "register_map": "sungrow.lua",
      "sn": "SHI-ABC123",
      "host": "192.168.1.100",
      "port": 502,
      "unit_id": 1,
      "ders": [
        {"type": "battery", "enabled": true},
        {"type": "pv", "enabled": true},
        {"type": "meter", "enabled": true}
      ]
    }
  ]
}
```

## Key Patterns

- `CGO_ENABLED=0` — pure Go, static binary, cross-compiles everywhere
- Identity persisted at `{data-dir}/identity.json` + `{data-dir}/gateway.pem`
- ES256 JWT auth for NovaCore (username=serial, password=jwt)
- Embedded NATS with optional leaf node to upstream NovaCore
- Watchdog failsafe: 60s timeout → `driver_default_mode()` → safe state
- Device poll loop with exponential backoff on errors
- Protocol auto-detection from Lua source (modbus/mqtt/http)

## Sign Conventions (NovaCore Standard)

- **PV w**: <= 0 (negative = generating)
- **Battery w**: positive = charging, negative = discharging
- **Battery soc**: 0.0 to 1.0 (fraction, NOT percentage)
- **Meter w**: positive = importing, negative = exporting
- **Energy counters (wh)**: always positive, monotonically increasing

## Relationship to Other Repos

- **sourceful-hugin** — AI driver development tool. Can probe this edge-node remotely via NATS for device scanning, register reading, and driver testing.
- **device-support** (srcful/srcful-device-support) — Git submodule with Lua drivers and YAML manifests. Auto-detected at `device-support/drivers/` or `{data-dir}/drivers/`.
- **Blaxt** — Production gateway firmware. Edge-node shares the same Lua VM, Modbus library, and driver contract.
