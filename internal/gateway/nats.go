package gateway

import (
	"fmt"
	"log"
	"net/url"
	"time"

	"github.com/nats-io/nats-server/v2/server"
	"github.com/nats-io/nats.go"
	"github.com/srcfl/sourceful-edge-node/internal/identity"
)

// NATSConfig holds NATS connection configuration.
type NATSConfig struct {
	URL       string // External NATS URL (empty = use embedded)
	Creds     string // Credentials file path for upstream auth
	Embed     bool   // Run embedded NATS server
	EmbedPort int    // Embedded server port (default 4222)
	LeafURL   string // Upstream NATS URL for leaf node connection (optional)
}

// NATSConn wraps a NATS connection and optional embedded server.
type NATSConn struct {
	Conn   *nats.Conn
	Server *server.Server // nil if not embedded
}

// ConnectNATS establishes a NATS connection with optional embedded server.
func ConnectNATS(cfg NATSConfig, id *identity.Identity) (*NATSConn, error) {
	nc := &NATSConn{}

	connectURL := cfg.URL

	// Start embedded server if requested
	if cfg.Embed {
		port := cfg.EmbedPort
		if port == 0 {
			port = 4222
		}

		opts := &server.Options{
			Host:       "127.0.0.1",
			Port:       port,
			NoLog:      true,
			NoSigs:     true,
			MaxPayload: 1024 * 1024, // 1MB
		}

		// If leaf URL is provided, configure leaf node connection to upstream
		if cfg.LeafURL != "" {
			leafURL, err := url.Parse(cfg.LeafURL)
			if err != nil {
				return nil, fmt.Errorf("parse leaf URL %q: %w", cfg.LeafURL, err)
			}
			leaf := &server.RemoteLeafOpts{
				URLs: []*url.URL{leafURL},
			}
			if cfg.Creds != "" {
				leaf.Credentials = cfg.Creds
			}
			opts.LeafNode = server.LeafNodeOpts{
				Remotes: []*server.RemoteLeafOpts{leaf},
			}
		}

		srv, err := server.NewServer(opts)
		if err != nil {
			return nil, fmt.Errorf("embedded NATS server: %w", err)
		}

		srv.Start()
		if !srv.ReadyForConnections(5 * time.Second) {
			return nil, fmt.Errorf("embedded NATS server not ready after 5s")
		}

		nc.Server = srv
		connectURL = fmt.Sprintf("nats://127.0.0.1:%d", port)
		log.Printf("Embedded NATS server running on %s", connectURL)
	}

	if connectURL == "" {
		return nil, fmt.Errorf("no NATS URL configured (use --nats-url or --nats-embed)")
	}

	// Build connection options
	natsOpts := []nats.Option{
		nats.Name("hugin-" + id.Serial),
		nats.RetryOnFailedConnect(true),
		nats.MaxReconnects(-1),
		nats.ReconnectWait(2 * time.Second),
		nats.DisconnectErrHandler(func(_ *nats.Conn, err error) {
			if err != nil {
				log.Printf("NATS disconnected: %v", err)
			}
		}),
		nats.ReconnectHandler(func(_ *nats.Conn) {
			log.Println("NATS reconnected")
		}),
	}

	// Auth: credentials file or ES256 JWT (username=serial, password=jwt)
	if cfg.Creds != "" && !cfg.Embed {
		natsOpts = append(natsOpts, nats.UserCredentials(cfg.Creds))
	} else if id.PrivateKey != nil && !cfg.Embed {
		// NovaCore auth-callout reads ConnectOpts.User + ConnectOpts.Pass
		jwt, err := id.GenerateAuthJWT()
		if err != nil {
			return nil, fmt.Errorf("generate auth JWT: %w", err)
		}
		natsOpts = append(natsOpts, nats.UserInfo(id.Serial, jwt))
	}

	conn, err := nats.Connect(connectURL, natsOpts...)
	if err != nil {
		return nil, fmt.Errorf("NATS connect to %s: %w", connectURL, err)
	}

	nc.Conn = conn
	log.Printf("NATS connected to %s", connectURL)
	return nc, nil
}

// Close closes the NATS connection and shuts down embedded server if running.
func (nc *NATSConn) Close() {
	if nc.Conn != nil {
		nc.Conn.Drain()
	}
	if nc.Server != nil {
		nc.Server.Shutdown()
		nc.Server.WaitForShutdown()
	}
}
