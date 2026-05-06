package config

import (
	"encoding/json"
	"fmt"
	"os"
	"sync"
)

// configPath returns the file path from CONFIG_PATH env, falling back to cwd.
func configPath() string {
	if p := os.Getenv("CONFIG_PATH"); p != "" {
		return p
	}
	return "sim_config.json"
}

// SimConfig is the top-level config persisted to disk.
type SimConfig struct {
	mu      sync.RWMutex
	Cameras []CameraConfig `json:"cameras"`
}

// CameraConfig holds all settings for a single simulated camera.
type CameraConfig struct {
	ID                string   `json:"id"`
	Label             string   `json:"label"`
	BackendURL        string   `json:"backend_url"`
	DeviceID          string   `json:"device_id"`
	DeviceName        string   `json:"device_name"`
	DeviceModel       string   `json:"device_model"`
	DeviceType        string   `json:"device_type"` // "Tollgate" | "E-Police"
	Manufacturer      string   `json:"manufacturer"`
	IPAddress         string   `json:"ip_address"`
	AuthEnabled       bool     `json:"auth_enabled"`
	Username          string   `json:"username"`
	Password          string   `json:"password"`
	HeartbeatInterval int      `json:"heartbeat_interval"` // seconds, 0 = disabled
	PlatePool         []string `json:"plate_pool"`
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
	// Ensure parent directory exists (e.g. /app/data/)
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

// All returns a copy of the camera list.
func (s *SimConfig) All() []CameraConfig {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]CameraConfig, len(s.Cameras))
	copy(out, s.Cameras)
	return out
}

// Get returns a camera by ID.
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

// Upsert adds or replaces a camera config by ID.
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

// Delete removes a camera by ID.
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
