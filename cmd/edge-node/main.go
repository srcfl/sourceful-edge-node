// sourceful-edge-node — Universal Sourceful edge node binary.
//
// One binary that turns any machine into a Sourceful gateway:
//   - Runs Lua device drivers continuously
//   - Publishes telemetry to NovaCore via NATS
//   - Handles control commands with watchdog failsafe
//   - Serves as a probe target for Hugin AI integration
//   - Scans local network, reads registers, inspects HTTP/MQTT devices
//
// Usage:
//   edge-node                                     # Start with embedded NATS
//   edge-node --nats-url wss://novacore.dev:4443  # Connect to NovaCore
//   edge-node --serial my-gateway-001             # Custom identity

package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"github.com/srcfl/sourceful-edge-node/internal/drivers"
	"github.com/srcfl/sourceful-edge-node/internal/gateway"
	"github.com/srcfl/sourceful-edge-node/internal/identity"
)

var version = "dev"

func main() {
	dataDir := flag.String("data-dir", "", "Data directory (default ~/.sourceful/)")
	driversPath := flag.String("drivers", "", "Path to device drivers directory")
	devicesConfig := flag.String("devices", "", "Path to devices.json (default {data-dir}/devices.json)")
	natsURL := flag.String("nats-url", "", "NovaCore NATS URL (e.g. wss://novacore-testnet.sourceful.dev:4443)")
	natsEmbed := flag.Bool("nats-embed", false, "Run embedded NATS server")
	natsCreds := flag.String("nats-creds", "", "NATS credentials file")
	natsLeafURL := flag.String("nats-leaf-url", "", "Upstream NATS URL for leaf node")
	serialOverride := flag.String("serial", "", "Override gateway serial")
	flag.Parse()

	// Resolve data dir
	if *dataDir == "" {
		*dataDir = os.Getenv("SOURCEFUL_DATA_DIR")
	}
	if *dataDir == "" {
		home, _ := os.UserHomeDir()
		*dataDir = filepath.Join(home, ".sourceful")
	}
	os.MkdirAll(*dataDir, 0755)

	// Resolve NATS URL from env
	if *natsURL == "" {
		*natsURL = os.Getenv("SOURCEFUL_NATS_URL")
	}

	// Signal handling
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		sig := <-sigCh
		log.Printf("Received %s, shutting down...", sig)
		cancel()
	}()

	// Identity
	id, err := identity.LoadOrCreateWithKeys(*dataDir, *serialOverride)
	if err != nil {
		log.Fatalf("Identity: %v", err)
	}
	log.Printf("Identity: %s (pubkey: %s...)", id.Serial, id.PublicKey[:16])

	// NATS
	natsCfg := gateway.NATSConfig{
		URL:     *natsURL,
		Creds:   *natsCreds,
		Embed:   *natsEmbed,
		LeafURL: *natsLeafURL,
	}
	if natsCfg.URL == "" && !natsCfg.Embed {
		natsCfg.Embed = true
	}
	nc, err := gateway.ConnectNATS(natsCfg, id)
	if err != nil {
		log.Fatalf("NATS: %v", err)
	}
	defer nc.Close()

	// Drivers
	var ds *drivers.Store
	if *driversPath == "" {
		// Try common locations
		for _, candidate := range []string{
			filepath.Join(*dataDir, "drivers"),
			"device-support/drivers",
		} {
			if info, err := os.Stat(candidate); err == nil && info.IsDir() {
				*driversPath = candidate
				break
			}
		}
	}
	if *driversPath != "" {
		ds = drivers.NewStore(*driversPath)
		if err := ds.Load(); err != nil {
			log.Printf("Warning: drivers: %v", err)
		} else {
			log.Printf("Loaded %d device drivers from %s", len(ds.List()), *driversPath)
		}
	} else {
		ds = drivers.NewStore("")
		log.Println("No drivers directory found. Driver features disabled.")
	}

	// Probe handler — makes this binary controllable by Hugin
	if err := gateway.StartProbeHandler(nc.Conn, id.Serial); err != nil {
		log.Printf("Warning: probe handler: %v", err)
	}

	// Fleet monitor
	fleetMon, err := gateway.NewFleetMonitor(nc.Conn, id.Serial)
	if err != nil {
		log.Printf("Warning: fleet monitor: %v", err)
	}
	if fleetMon != nil {
		defer fleetMon.Close()
	}

	// Device manager
	cfgPath := *devicesConfig
	if cfgPath == "" {
		cfgPath = filepath.Join(*dataDir, "devices.json")
	}
	devMgr := gateway.NewDeviceManager(id.Serial, nc.Conn, ds, *dataDir, cfgPath)
	if err := devMgr.Start(ctx); err != nil {
		log.Printf("Warning: device manager: %v", err)
	}
	defer devMgr.Shutdown()

	// Gateway state publisher runs inside DeviceManager

	fmt.Printf("Sourceful Edge Node %s [%s]\n", version, id.Serial)
	fmt.Printf("NATS: %s\n", nc.Conn.ConnectedUrl())

	// Wait for shutdown
	<-ctx.Done()
	log.Println("Shutdown complete.")
}
