package config

import (
	"encoding/json"
	"fmt"
	"os"
	"sync"
)

const MaxImages = 20

// configPath returns the file path from CONFIG_PATH env, falling back to cwd.
func configPath() string {
	if p := os.Getenv("CONFIG_PATH"); p != "" {
		return p
	}
	return "sim_config.json"
}

// ImageEntry is one entry in the global image library.
type ImageEntry struct {
	ID   string `json:"id"`   // unique, e.g. "img_1234"
	Name string `json:"name"` // original filename
	Data string `json:"data"` // raw base64, no data-URI prefix
}

// SpotConfig is a single parking spot within a channel.
type SpotConfig struct {
	ID      string `json:"id"`
	Plate   string `json:"plate"`
	ImageID string `json:"image_id"` // optional, references ImageEntry.ID
}

// ChannelConfig is one camera channel containing up to 4 spots.
type ChannelConfig struct {
	ChannelNo int          `json:"channel_no"`
	Label     string       `json:"label"`
	Spots     []SpotConfig `json:"spots"`
}

// CameraConfig holds all settings for a single simulated camera.
type CameraConfig struct {
	ID                string          `json:"id"`
	Label             string          `json:"label"`
	BackendURL        string          `json:"backend_url"`
	DeviceID          string          `json:"device_id"`
	DeviceName        string          `json:"device_name"`
	DeviceModel       string          `json:"device_model"`
	DeviceType        string          `json:"device_type"` // "Tollgate" | "E-Police"
	Manufacturer      string          `json:"manufacturer"`
	IPAddress         string          `json:"ip_address"`
	IPv6Address       string          `json:"ipv6_address"`
	MACAddress        string          `json:"mac_address"`
	AuthEnabled       bool            `json:"auth_enabled"`
	Username          string          `json:"username"`
	Password          string          `json:"password"`
	HeartbeatInterval int             `json:"heartbeat_interval"`
	PlatePool         []string        `json:"plate_pool"`
	Channels          []ChannelConfig `json:"channels"`
}

// SimConfig is the top-level config persisted to disk.
type SimConfig struct {
	mu      sync.RWMutex
	Cameras []CameraConfig `json:"cameras"`
	Images  []ImageEntry   `json:"images"`
}

// Load reads the config file. Returns empty SimConfig if file does not exist.
func Load() (*SimConfig, error) {
	cfg := &SimConfig{}
	data, err := os.ReadFile(configPath())
	if os.IsNotExist(err) {
		return cfg, nil
	}
	if err != nil {
		return nil, fmt.Errorf("config.Load: %w", err)
	}
	if err := json.Unmarshal(data, cfg); err != nil {
		return nil, fmt.Errorf("config.Load: %w", err)
	}
	return cfg, nil
}

// Save writes the current config to disk.
func (s *SimConfig) Save() error {
	s.mu.RLock()
	defer s.mu.RUnlock()
	path := configPath()
	if dir := dirOf(path); dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return fmt.Errorf("config.Save mkdir: %w", err)
		}
	}
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return fmt.Errorf("config.Save: %w", err)
	}
	if err := os.WriteFile(path, data, 0644); err != nil {
		return fmt.Errorf("config.Save: %w", err)
	}
	return nil
}

func dirOf(path string) string {
	for i := len(path) - 1; i >= 0; i-- {
		if path[i] == '/' || path[i] == '\\' {
			return path[:i]
		}
	}
	return ""
}

// ── Camera CRUD ───────────────────────────────────────────────────────────────

func (s *SimConfig) All() []CameraConfig {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]CameraConfig, len(s.Cameras))
	copy(out, s.Cameras)
	return out
}

func (s *SimConfig) Get(id string) (CameraConfig, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, c := range s.Cameras {
		if c.ID == id {
			return c, true
		}
	}
	return CameraConfig{}, false
}

func (s *SimConfig) ExistsByDeviceID(deviceID string) bool {
	return s.ExistsByDeviceIDExcluding(deviceID, "")
}

func (s *SimConfig) ExistsByDeviceIDExcluding(deviceID string, excludeID string) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, c := range s.Cameras {
		if c.ID == excludeID {
			continue
		}
		if c.DeviceID == deviceID {
			return true
		}
	}
	return false
}

func (s *SimConfig) Upsert(cam CameraConfig) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i, c := range s.Cameras {
		if c.ID == cam.ID {
			s.Cameras[i] = cam
			return
		}
	}
	s.Cameras = append(s.Cameras, cam)
}

func (s *SimConfig) Delete(id string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := s.Cameras[:0]
	for _, c := range s.Cameras {
		if c.ID != id {
			out = append(out, c)
		}
	}
	s.Cameras = out
}

// ── Image library CRUD ────────────────────────────────────────────────────────

func (s *SimConfig) AllImages() []ImageEntry {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]ImageEntry, len(s.Images))
	copy(out, s.Images)
	return out
}

// AllImagesMeta returns image list without base64 data — for the list endpoint.
func (s *SimConfig) AllImagesMeta() []map[string]string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]map[string]string, len(s.Images))
	for i, img := range s.Images {
		out[i] = map[string]string{"id": img.ID, "name": img.Name}
	}
	return out
}

func (s *SimConfig) GetImage(id string) (ImageEntry, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, img := range s.Images {
		if img.ID == id {
			return img, true
		}
	}
	return ImageEntry{}, false
}

// AddImage appends an image. Returns false if the library is at capacity.
func (s *SimConfig) AddImage(img ImageEntry) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.Images) >= MaxImages {
		return false
	}
	s.Images = append(s.Images, img)
	return true
}

func (s *SimConfig) DeleteImage(id string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := s.Images[:0]
	for _, img := range s.Images {
		if img.ID != id {
			out = append(out, img)
		}
	}
	s.Images = out
}
