package gateway

import (
	"encoding/json"
	"fmt"
	"os"
)

// DevicesConfig is the top-level devices configuration.
type DevicesConfig struct {
	Devices []DeviceConfig `json:"devices"`
}

// DeviceConfig describes a single managed device.
type DeviceConfig struct {
	Type        string      `json:"type"`                  // always "lua"
	RegisterMap string      `json:"register_map"`          // Lua driver filename (e.g. "sungrow.lua")
	SN          string      `json:"sn"`                    // Stable device serial/ID
	Host        string      `json:"host,omitempty"`        // IP or hostname
	Port        int         `json:"port,omitempty"`        // Protocol port (502 for Modbus, 1883 for MQTT, etc.)
	UnitID      int         `json:"unit_id,omitempty"`     // Modbus slave ID
	Username    string      `json:"username,omitempty"`    // MQTT/HTTP auth
	Password    string      `json:"password,omitempty"`    // MQTT/HTTP auth
	DERs        []DERConfig `json:"ders,omitempty"`        // DER type configuration
}

// DERConfig describes a DER attached to a device.
type DERConfig struct {
	Type    string `json:"type"`    // battery, pv, meter, v2x_charger
	Enabled bool   `json:"enabled"`
}

// DEREnabled returns true if the given DER type is enabled for this device.
func (c *DeviceConfig) DEREnabled(derType string) bool {
	for _, d := range c.DERs {
		if d.Type == derType && d.Enabled {
			return true
		}
	}
	// If no DERs configured, allow all
	return len(c.DERs) == 0
}

// LoadDevicesConfig reads devices.json from disk.
func LoadDevicesConfig(path string) (*DevicesConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return &DevicesConfig{}, nil
		}
		return nil, fmt.Errorf("read devices config: %w", err)
	}

	var cfg DevicesConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parse devices config: %w", err)
	}
	return &cfg, nil
}

// SaveDevicesConfig writes devices.json to disk.
func SaveDevicesConfig(path string, cfg *DevicesConfig) error {
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal devices config: %w", err)
	}
	return os.WriteFile(path, append(data, '\n'), 0644)
}
