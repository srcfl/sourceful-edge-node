package drivers

// Manifest represents a V2 driver manifest (YAML).
type Manifest struct {
	Name           string         `yaml:"name"            json:"name"`
	Version        string         `yaml:"version"         json:"version"`
	Tier           string         `yaml:"tier"            json:"tier"`
	Author         string         `yaml:"author"          json:"author,omitempty"`
	Protocol       string         `yaml:"protocol"        json:"protocol"`
	DERs           []string       `yaml:"ders"            json:"ders"`
	Control        bool           `yaml:"control"         json:"control"`
	TestedDevices  []TestedDevice `yaml:"tested_devices"  json:"tested_devices,omitempty"`
	MinHostVersion string         `yaml:"min_host_version" json:"min_host_version,omitempty"`
	SizeBytes      int            `yaml:"size_bytes"      json:"size_bytes,omitempty"`
	DKBID          string         `yaml:"dkb_id"          json:"dkb_id,omitempty"`
	SHA256         string         `yaml:"sha256"          json:"sha256,omitempty"`
	Signature      string         `yaml:"signature"       json:"signature,omitempty"`
}

// TestedDevice describes a device that a driver has been tested with.
type TestedDevice struct {
	Manufacturer     string   `yaml:"manufacturer"      json:"manufacturer"`
	ModelFamily      string   `yaml:"model_family"      json:"model_family"`
	Variants         []string `yaml:"variants"          json:"variants,omitempty"`
	Regions          []string `yaml:"regions"           json:"regions,omitempty"`
	FirmwareVersions string   `yaml:"firmware_versions" json:"firmware_versions,omitempty"`
	Notes            string   `yaml:"notes"             json:"notes,omitempty"`
}

// Driver represents a loaded driver with its manifest and Lua source.
type Driver struct {
	Manifest Manifest `json:"manifest"`
	Source   string   `json:"source"`
	Path     string   `json:"path"` // filesystem path to .lua file
}
