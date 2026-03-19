# Sourceful Edge Node

Universal Sourceful edge node — one Go binary that turns any machine into a gateway on the Sourceful Energy network.

## What It Does

- **Runs device drivers** — Lua 5.1 drivers poll energy devices (inverters, batteries, meters, chargers) via Modbus, MQTT, HTTP
- **Publishes telemetry** — DER data flows to NovaCore via NATS in real-time
- **Handles control commands** — Grid commands execute locally with 60s watchdog failsafe
- **Serves as a probe target** — Hugin (AI integration tool) can scan, detect, and probe devices through this node remotely
- **Works everywhere** — Laptop, Raspberry Pi, industrial server, Zap, Blaxt — same binary

## Quick Start

```bash
# Build
go build -o edge-node ./cmd/edge-node/

# Run with embedded NATS (standalone)
./edge-node

# Connect to Sourceful testnet
./edge-node --nats-url wss://novacore-testnet.sourceful.dev:4443

# With device drivers
./edge-node --nats-url wss://novacore-testnet.sourceful.dev:4443 --drivers ./drivers/
```

## Configuration

| Flag | Env | Default | Description |
|------|-----|---------|-------------|
| `--nats-url` | `SOURCEFUL_NATS_URL` | embedded | NovaCore NATS URL |
| `--nats-embed` | | false | Run embedded NATS server |
| `--data-dir` | `SOURCEFUL_DATA_DIR` | `~/.sourceful/` | Data directory |
| `--drivers` | | auto-detect | Path to device drivers |
| `--devices` | | `{data-dir}/devices.json` | Device configuration |
| `--serial` | | auto-derived | Gateway serial override |

## Identity

On first run, generates an ES256 keypair stored at `{data-dir}/identity.json` and `{data-dir}/gateway.pem`. The serial format is `hugin-{8hex}` derived from hostname + MAC address.

## Architecture

```
┌─────────────────────────────────────────┐
│  SOURCEFUL EDGE NODE                     │
├─────────────────────────────────────────┤
│  NATS Client (NovaCore / embedded)       │
│  Probe Handler (remote AI integration)   │
│  Device Manager (continuous poll loop)    │
│  Control Handler + Watchdog              │
│  Fleet Monitor                           │
├─────────────────────────────────────────┤
│  Lua 5.1 Runtime (gopher-lua)           │
│  Protocol Bridges: Modbus, MQTT, HTTP    │
│  Driver Loader (YAML manifests + Lua)    │
│  ES256 Identity + JWT Auth               │
└─────────────────────────────────────────┘
```
