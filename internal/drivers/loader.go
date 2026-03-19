package drivers

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"gopkg.in/yaml.v3"
)

// Store manages loading and accessing Lua drivers from a filesystem directory.
type Store struct {
	basePath string
	mu       sync.RWMutex
	drivers  map[string]*Driver // name -> driver
}

// NewStore creates a new driver store rooted at basePath.
// Expects basePath to contain drivers/ and manifests/ subdirectories.
func NewStore(basePath string) *Store {
	return &Store{
		basePath: basePath,
		drivers:  make(map[string]*Driver),
	}
}

// BasePath returns the configured base path, or empty if no path configured.
func (s *Store) BasePath() string {
	return s.basePath
}

// Load scans the filesystem for drivers/*.lua + manifests/*.yaml and loads them.
func (s *Store) Load() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.drivers = make(map[string]*Driver)

	// Load manifests first
	// Support two layouts:
	//   basePath/manifests/*.yaml + basePath/drivers/*.lua
	//   basePath/manifests/*.yaml + basePath/lua/*.lua
	manifests := make(map[string]*Manifest)
	manifestDir := filepath.Join(s.basePath, "manifests")
	manifestFiles, _ := filepath.Glob(filepath.Join(manifestDir, "*.yaml"))
	for _, mf := range manifestFiles {
		data, err := os.ReadFile(mf)
		if err != nil {
			continue
		}
		var m Manifest
		if err := yaml.Unmarshal(data, &m); err != nil {
			continue
		}
		name := strings.TrimSuffix(filepath.Base(mf), ".yaml")
		if m.Name == "" {
			m.Name = name
		}
		manifests[name] = &m
	}

	// Load Lua sources — try "drivers/" then "lua/" subdirs
	driverDir := filepath.Join(s.basePath, "drivers")
	driverFiles, _ := filepath.Glob(filepath.Join(driverDir, "*.lua"))
	if len(driverFiles) == 0 {
		driverDir = filepath.Join(s.basePath, "lua")
		driverFiles, _ = filepath.Glob(filepath.Join(driverDir, "*.lua"))
	}
	for _, df := range driverFiles {
		name := strings.TrimSuffix(filepath.Base(df), ".lua")
		source, err := os.ReadFile(df)
		if err != nil {
			continue
		}
		d := &Driver{
			Source: string(source),
			Path:   df,
		}
		if m, ok := manifests[name]; ok {
			d.Manifest = *m
		} else {
			d.Manifest = Manifest{Name: name}
		}
		s.drivers[name] = d
	}

	return nil
}

// List returns all loaded drivers.
func (s *Store) List() []*Driver {
	s.mu.RLock()
	defer s.mu.RUnlock()
	result := make([]*Driver, 0, len(s.drivers))
	for _, d := range s.drivers {
		result = append(result, d)
	}
	return result
}

// Get returns a driver by name.
func (s *Store) Get(name string) (*Driver, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	d, ok := s.drivers[name]
	return d, ok
}

// SaveSource writes updated Lua source to disk and reloads the driver.
func (s *Store) SaveSource(name, source string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	d, ok := s.drivers[name]
	if !ok {
		return fmt.Errorf("driver %q not found", name)
	}

	if err := os.WriteFile(d.Path, []byte(source), 0644); err != nil {
		return fmt.Errorf("write %s: %w", d.Path, err)
	}

	d.Source = source
	return nil
}

// CreateDriver creates a new driver with a manifest and Lua source template.
func (s *Store) CreateDriver(name string, manifest Manifest, source string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, exists := s.drivers[name]; exists {
		return fmt.Errorf("driver %q already exists", name)
	}

	// Ensure directories exist — use "lua/" if that's the existing layout
	manifestDir := filepath.Join(s.basePath, "manifests")
	driverDir := filepath.Join(s.basePath, "drivers")
	if _, err := os.Stat(filepath.Join(s.basePath, "lua")); err == nil {
		driverDir = filepath.Join(s.basePath, "lua")
	}
	if err := os.MkdirAll(manifestDir, 0755); err != nil {
		return fmt.Errorf("create manifests dir: %w", err)
	}
	if err := os.MkdirAll(driverDir, 0755); err != nil {
		return fmt.Errorf("create drivers dir: %w", err)
	}

	// Write manifest
	manifest.Name = name
	manifestData, err := yaml.Marshal(&manifest)
	if err != nil {
		return fmt.Errorf("marshal manifest: %w", err)
	}
	manifestPath := filepath.Join(manifestDir, name+".yaml")
	if err := os.WriteFile(manifestPath, manifestData, 0644); err != nil {
		return fmt.Errorf("write manifest: %w", err)
	}

	// Write Lua source
	luaPath := filepath.Join(driverDir, name+".lua")
	if err := os.WriteFile(luaPath, []byte(source), 0644); err != nil {
		return fmt.Errorf("write lua: %w", err)
	}

	s.drivers[name] = &Driver{
		Manifest: manifest,
		Source:   source,
		Path:     luaPath,
	}
	return nil
}

// BuildVendorMap creates a mapping from lowercase manufacturer name to driver name.
// Used by detection to map FC43/SunSpec vendor strings to Lua drivers.
func (s *Store) BuildVendorMap() map[string]string {
	s.mu.RLock()
	defer s.mu.RUnlock()

	vendorMap := make(map[string]string)
	for _, d := range s.drivers {
		for _, td := range d.Manifest.TestedDevices {
			if td.Manufacturer != "" {
				vendorMap[strings.ToLower(td.Manufacturer)] = d.Manifest.Name
			}
		}
		// Also map the driver name itself for direct matching
		vendorMap[strings.ToLower(d.Manifest.Name)] = d.Manifest.Name
	}
	return vendorMap
}
