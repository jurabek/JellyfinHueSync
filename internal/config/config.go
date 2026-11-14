// Package config persists application configuration as JSON in CONFIG_DIR.
package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"

	"github.com/jurabek/JellyfinHueSync/internal/hue"
)

// PathMapping rewrites a Jellyfin media path into a path this container can read.
type PathMapping struct {
	Jellyfin  string `json:"Jellyfin"`
	Container string `json:"Container"`
}

// SyncOptions controls the decode/stream pipeline and color response.
type SyncOptions struct {
	Brightness       float32 `json:"Brightness"`
	Saturation       float32 `json:"Saturation"`
	SmoothingMs      int     `json:"SmoothingMs"`
	InvertHorizontal bool    `json:"InvertHorizontal"`
	ExtractFps       int     `json:"ExtractFps"`
	StreamRate       int     `json:"StreamRate"`
	FrameWidth       int     `json:"FrameWidth"`
	FrameHeight      int     `json:"FrameHeight"`
	SegmentSeconds   int     `json:"SegmentSeconds"`
}

// DefaultOptions returns the recommended defaults.
func DefaultOptions() SyncOptions {
	return SyncOptions{
		Brightness:     0.75,
		Saturation:     1.15,
		SmoothingMs:    150,
		ExtractFps:     4,
		StreamRate:     25,
		FrameWidth:     96,
		FrameHeight:    54,
		SegmentSeconds: 30,
	}
}

// Validated returns a copy with every field clamped to its valid range.
func (o SyncOptions) Validated() SyncOptions {
	return SyncOptions{
		Brightness:       clampF(o.Brightness, 0, 1),
		Saturation:       clampF(o.Saturation, 0, 2),
		SmoothingMs:      clampI(o.SmoothingMs, 0, 1000),
		InvertHorizontal: o.InvertHorizontal,
		ExtractFps:       clampI(o.ExtractFps, 1, 10),
		StreamRate:       clampI(o.StreamRate, 5, 60),
		FrameWidth:       clampI(o.FrameWidth, 32, 320),
		FrameHeight:      clampI(o.FrameHeight, 18, 180),
		SegmentSeconds:   clampI(o.SegmentSeconds, 10, 120),
	}
}

// UnmarshalJSON fills in defaults before decoding, so partially written option
// objects keep the recommended values for fields they omit.
func (o *SyncOptions) UnmarshalJSON(data []byte) error {
	type plain SyncOptions
	*o = DefaultOptions()
	return json.Unmarshal(data, (*plain)(o))
}

// AppConfig is the persisted configuration file.
type AppConfig struct {
	BridgeIp         string        `json:"BridgeIp"`
	BridgeId         string        `json:"BridgeId"`
	BridgeName       string        `json:"BridgeName"`
	ApplicationKey   string        `json:"ApplicationKey"`
	ClientKey        string        `json:"ClientKey"`
	AreaId           string        `json:"AreaId"`
	JellyfinUrl      string        `json:"JellyfinUrl"`
	JellyfinApiKey   string        `json:"JellyfinApiKey"`
	JellyfinUserId   string        `json:"JellyfinUserId"`
	JellyfinDeviceId string        `json:"JellyfinDeviceId"`
	MediaMappings    []PathMapping `json:"MediaMappings"`
	Options          SyncOptions   `json:"Options"`
}

// Credentials returns the Hue pairing, or nil when the Bridge is not paired.
func (c AppConfig) Credentials() *hue.Credentials {
	if c.ApplicationKey == "" || c.ClientKey == "" {
		return nil
	}
	return &hue.Credentials{
		Bridge:         hue.Bridge{IP: c.BridgeIp, ID: c.BridgeId, Name: c.BridgeName},
		ApplicationKey: c.ApplicationKey,
		ClientKey:      c.ClientKey,
	}
}

// JellyfinConfigured reports whether the minimum Jellyfin settings are present.
func (c AppConfig) JellyfinConfigured() bool {
	return c.JellyfinUrl != "" && c.JellyfinApiKey != ""
}

func clampF(v, lo, hi float32) float32 {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

func clampI(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

// Store persists AppConfig as JSON (0600) inside dir, with atomic writes.
type Store struct {
	dir    string
	mu     sync.Mutex
	cached AppConfig
}

// NewStore loads (or initializes) the configuration in dir.
func NewStore(dir string) *Store {
	s := &Store{dir: dir}
	s.cached = s.load()
	return s
}

// Config returns a copy of the current configuration.
func (s *Store) Config() AppConfig {
	s.mu.Lock()
	defer s.mu.Unlock()
	return clone(s.cached)
}

// Mutate applies fn to the cached config and persists the result.
func (s *Store) Mutate(fn func(*AppConfig)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	fn(&s.cached)
	s.save()
}

func (s *Store) load() AppConfig {
	cfg := AppConfig{}
	if data, err := os.ReadFile(filepath.Join(s.dir, "config.json")); err == nil {
		if err := json.Unmarshal(data, &cfg); err != nil {
			cfg = AppConfig{}
		}
	}
	if cfg.Options == (SyncOptions{}) {
		cfg.Options = DefaultOptions()
	}
	return cfg
}

func (s *Store) save() {
	path := filepath.Join(s.dir, "config.json")
	tmp := path + ".tmp"
	data, err := json.MarshalIndent(s.cached, "", "  ")
	if err != nil {
		return
	}
	if err := os.MkdirAll(s.dir, 0o755); err != nil {
		return
	}
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return
	}
	if err := os.Rename(tmp, path); err != nil {
		return
	}
	// The pairing secret lives here; keep the file owner-only on Unix.
	_ = os.Chmod(path, 0o600)
}

func clone(c AppConfig) AppConfig {
	c.MediaMappings = append([]PathMapping(nil), c.MediaMappings...)
	return c
}
