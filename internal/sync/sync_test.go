package sync

import (
	"math"
	"os"
	"path/filepath"
	"testing"

	"github.com/jurabek/JellyfinHueSync/internal/config"
)

func TestSrgbLinearRoundTrip(t *testing.T) {
	for _, c := range []float32{0, 0.01, 0.04, 0.045, 0.2, 0.5, 0.8, 1} {
		if got := linearToSrgb(srgbToLinear(c)); math.Abs(float64(got-c)) > 1e-3 {
			t.Errorf("roundtrip(%v) = %v", c, got)
		}
	}
}

func TestMapPath(t *testing.T) {
	dir := filepath.ToSlash(t.TempDir())
	container := dir + "/movies"
	file := container + "/a.mkv"
	if err := os.MkdirAll(filepath.FromSlash(container), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.FromSlash(file), nil, 0o644); err != nil {
		t.Fatal(err)
	}

	mappings := []config.PathMapping{
		{Jellyfin: "/data/media/movies", Container: container},
	}

	if got := mapPath(file, mappings); got != file {
		t.Errorf("direct path = %q, want %q", got, file)
	}
	if got := mapPath("/data/media/movies/a.mkv", mappings); got != file {
		t.Errorf("mapped path = %q, want %q", got, file)
	}
	if got := mapPath("/other/a.mkv", mappings); got != "" {
		t.Errorf("unmapped path = %q, want empty", got)
	}
}

func TestFmtSeconds(t *testing.T) {
	if got := fmtSeconds(5.5); got != "5.500" {
		t.Errorf("fmtSeconds(5.5) = %q, want %q", got, "5.500")
	}
	if got := fmtSeconds(0); got != "0.000" {
		t.Errorf("fmtSeconds(0) = %q, want %q", got, "0.000")
	}
}
