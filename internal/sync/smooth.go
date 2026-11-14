package sync

import (
	"math"

	"github.com/jurabek/JellyfinHueSync/internal/config"
)

// SmoothFilter applies time-based exponential smoothing so transitions stay
// steady regardless of the send rate.
type SmoothFilter struct {
	smoothed      []float32
	previousNanos int64
}

// NewSmoothFilter returns a filter for the given number of channels.
func NewSmoothFilter(channels int) *SmoothFilter {
	return &SmoothFilter{smoothed: make([]float32, channels*3)}
}

// Apply smooths target toward the current value and returns the result.
func (f *SmoothFilter) Apply(target []float32, nowNanos int64, opts config.SyncOptions) []float32 {
	if len(target) != len(f.smoothed) {
		return target
	}

	elapsed := 1.0
	if f.previousNanos != 0 {
		elapsed = float64(max(int64(0), nowNanos-f.previousNanos)) / 1e9
	}
	alpha := 1.0
	if opts.SmoothingMs > 0 {
		alpha = 1 - math.Exp(-elapsed*1000.0/float64(opts.SmoothingMs))
	}
	f.previousNanos = nowNanos

	for i := range f.smoothed {
		f.smoothed[i] += (target[i] - f.smoothed[i]) * float32(alpha)
	}
	return f.smoothed
}
