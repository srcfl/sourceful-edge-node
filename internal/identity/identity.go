package identity

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"sort"
)

// Identity holds the gateway's stable identity including cryptographic keys.
type Identity struct {
	Serial     string            `json:"serial"`               // e.g. "hugin-04772a97"
	PublicKey  string            `json:"public_key,omitempty"` // 128-char hex (uncompressed x||y)
	PrivateKey *ecdsa.PrivateKey `json:"-"`                    // ES256 / P-256 key (not serialized to JSON)
}

// LoadOrCreate loads identity from dataDir/identity.json, or derives one
// and persists it for stability across reboots.
// If serial is non-empty, it's used as-is (override). Otherwise platform-agnostic
// derivation from hostname + MAC address is used.
func LoadOrCreate(dataDir, serial string) (*Identity, error) {
	path := filepath.Join(dataDir, "identity.json")

	// Try loading existing identity
	data, readErr := os.ReadFile(path)
	if readErr == nil {
		var id Identity
		if err := json.Unmarshal(data, &id); err == nil && id.Serial != "" {
			// If an override serial is given and differs, regenerate
			if serial != "" && serial != id.Serial {
				// Fall through to create new identity with override serial
			} else {
				return &id, nil
			}
		}
	} else if !os.IsNotExist(readErr) {
		return nil, fmt.Errorf("identity: read %s: %w", path, readErr)
	}

	// Derive serial if not provided
	if serial == "" {
		derived, err := deriveSerial()
		if err != nil {
			return nil, fmt.Errorf("identity: derive serial: %w", err)
		}
		serial = derived
	}

	id := &Identity{Serial: serial}

	// Persist identity
	if err := os.MkdirAll(dataDir, 0755); err != nil {
		return nil, fmt.Errorf("identity: create dir %s: %w", dataDir, err)
	}
	jsonData, err := json.MarshalIndent(id, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("identity: marshal: %w", err)
	}
	if err := os.WriteFile(path, jsonData, 0644); err != nil {
		return nil, fmt.Errorf("identity: write %s: %w", path, err)
	}

	return id, nil
}

// deriveSerial creates a platform-agnostic serial from hostname + first non-loopback MAC.
func deriveSerial() (string, error) {
	hostname, err := os.Hostname()
	if err != nil {
		hostname = "unknown"
	}

	mac := firstMAC()
	seed := hostname + ":" + mac
	hash := sha256.Sum256([]byte(seed))
	return fmt.Sprintf("hugin-%x", hash[:4]), nil
}

// firstMAC returns the MAC address of the first non-loopback network interface.
func firstMAC() string {
	ifaces, err := net.Interfaces()
	if err != nil {
		return "00:00:00:00:00:00"
	}

	// Sort for determinism across reboots
	sort.Slice(ifaces, func(i, j int) bool {
		return ifaces[i].Name < ifaces[j].Name
	})

	for _, iface := range ifaces {
		if iface.Flags&net.FlagLoopback != 0 {
			continue
		}
		if len(iface.HardwareAddr) == 0 {
			continue
		}
		return iface.HardwareAddr.String()
	}
	return "00:00:00:00:00:00"
}

// LoadOrCreateWithKeys loads identity with ES256 keys. Keys are stored in
// dataDir/gateway.pem (0600 permissions). If no key exists, one is generated.
func LoadOrCreateWithKeys(dataDir, serial string) (*Identity, error) {
	id, err := LoadOrCreate(dataDir, serial)
	if err != nil {
		return nil, err
	}

	keyPath := filepath.Join(dataDir, "gateway.pem")
	key, err := loadOrGenerateKey(keyPath)
	if err != nil {
		return nil, fmt.Errorf("identity: keys: %w", err)
	}

	id.PrivateKey = key
	// Public key as 128-char hex (uncompressed x||y, matching ESP32/Blaxt format)
	xBytes := key.PublicKey.X.Bytes()
	yBytes := key.PublicKey.Y.Bytes()
	pubBytes := make([]byte, 64)
	copy(pubBytes[32-len(xBytes):32], xBytes)
	copy(pubBytes[64-len(yBytes):64], yBytes)
	id.PublicKey = hex.EncodeToString(pubBytes)

	// Update persisted identity with public key
	path := filepath.Join(dataDir, "identity.json")
	jsonData, _ := json.MarshalIndent(id, "", "  ")
	os.WriteFile(path, jsonData, 0644)

	return id, nil
}

func loadOrGenerateKey(path string) (*ecdsa.PrivateKey, error) {
	data, readErr := os.ReadFile(path)
	if readErr == nil {
		block, _ := pem.Decode(data)
		if block != nil && block.Type == "EC PRIVATE KEY" {
			key, err := x509.ParseECPrivateKey(block.Bytes)
			if err == nil {
				return key, nil
			}
		}
	} else if !os.IsNotExist(readErr) {
		return nil, fmt.Errorf("read key %s: %w", path, readErr)
	}

	// Generate new ES256 key
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("generate key: %w", err)
	}

	// Persist as PEM
	der, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		return nil, fmt.Errorf("marshal key: %w", err)
	}
	pemBlock := &pem.Block{Type: "EC PRIVATE KEY", Bytes: der}
	pemData := pem.EncodeToMemory(pemBlock)
	if err := os.WriteFile(path, pemData, 0600); err != nil {
		return nil, fmt.Errorf("write key %s: %w", path, err)
	}

	return key, nil
}
