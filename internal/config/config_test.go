package config

import (
	"encoding/json"
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
