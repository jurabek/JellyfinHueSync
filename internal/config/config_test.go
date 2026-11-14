package config

import (
	"encoding/json"
	"path/filepath"
	"testing"
)

func TestSyncOptionsValidatedClamps(t *testing.T) {
	o := SyncOptions{
		Brightness:     2,
		Saturation:     -1,
		SmoothingMs:    5000,
		ExtractFps:     100,
		StreamRate:     0,
		FrameWidth:     10,
		FrameHeight:    1000,
		SegmentSeconds: 1,
	}
	v := o.Validated()
	cases := []struct {
		name string
		got  any
		want any
	}{
		{"Brightness", v.Brightness, float32(1)},
		{"Saturation", v.Saturation, float32(0)},
		{"SmoothingMs", v.SmoothingMs, 1000},
		{"ExtractFps", v.ExtractFps, 10},
		{"StreamRate", v.StreamRate, 5},
		{"FrameWidth", v.FrameWidth, 32},
		{"FrameHeight", v.FrameHeight, 180},
		{"SegmentSeconds", v.SegmentSeconds, 10},
	}
	for _, c := range cases {
		if c.got != c.want {
			t.Errorf("%s = %v, want %v", c.name, c.got, c.want)
		}
	}
}

func TestSyncOptionsUnmarshalJSONPreservesDefaults(t *testing.T) {
	var o SyncOptions
	if err := json.Unmarshal([]byte(`{"Brightness":0.5}`), &o); err != nil {
		t.Fatal(err)
	}
	if o.Brightness != 0.5 {
		t.Errorf("Brightness = %v, want 0.5", o.Brightness)
	}
	if o.Saturation != 1.15 {
		t.Errorf("Saturation = %v, want default 1.15", o.Saturation)
	}
	if o.SmoothingMs != 150 {
		t.Errorf("SmoothingMs = %v, want default 150", o.SmoothingMs)
	}
}

func TestStoreAppliesDefaultsWhenConfigMissing(t *testing.T) {
	s := NewStore(filepath.Join(t.TempDir(), "config"))
	cfg := s.Config()
	if cfg.Options.Brightness != 0.75 {
		t.Errorf("Brightness = %v, want 0.75", cfg.Options.Brightness)
	}
	if cfg.Options.ExtractFps != 4 {
		t.Errorf("ExtractFps = %v, want 4", cfg.Options.ExtractFps)
	}
	if cfg.Options.FrameWidth != 96 {
		t.Errorf("FrameWidth = %v, want 96", cfg.Options.FrameWidth)
	}
}
