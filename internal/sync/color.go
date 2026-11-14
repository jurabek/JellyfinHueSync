package sync

import (
	"math"

	"github.com/jurabek/JellyfinHueSync/internal/config"
	"github.com/jurabek/JellyfinHueSync/internal/hue"
)

// sampleFrame averages the sRGB neighborhood around each channel position and
// applies saturation/brightness in linear light, returning per-channel sRGB.
func sampleFrame(rgb []byte, offset, w, h int, channels []hue.Channel, opts config.SyncOptions) []float32 {
	result := make([]float32, len(channels)*3)
	radius := max(1, min(w, h)/24)

	for ci, ch := range channels {
		cx := clampInt(int(ch.SampleX(opts.InvertHorizontal)*float32(w)), 0, w-1)
		cy := clampInt(int(ch.ScreenY()*float32(h)), 0, h-1)

		var r, g, b float64
		n := 0
		for dy := -radius; dy <= radius; dy++ {
			for dx := -radius; dx <= radius; dx++ {
				x := clampInt(cx+dx, 0, w-1)
				y := clampInt(cy+dy, 0, h-1)
				off := offset + (y*w+x)*3
				r += float64(rgb[off])
				g += float64(rgb[off+1])
				b += float64(rgb[off+2])
				n++
			}
		}

		lr := srgbToLinear(float32(r / float64(n) / 255.0))
		lg := srgbToLinear(float32(g / float64(n) / 255.0))
		lb := srgbToLinear(float32(b / float64(n) / 255.0))
		luma := 0.2126*lr + 0.7152*lg + 0.0722*lb
		sat := opts.Saturation
		bright := opts.Brightness

		result[ci*3] = linearToSrgb(clampF(luma+(lr-luma)*sat, 0, 1) * bright)
		result[ci*3+1] = linearToSrgb(clampF(luma+(lg-luma)*sat, 0, 1) * bright)
		result[ci*3+2] = linearToSrgb(clampF(luma+(lb-luma)*sat, 0, 1) * bright)
	}
	return result
}

// lerp linearly interpolates two color frames.
func lerp(a, b []float32, t float32) []float32 {
	out := make([]float32, len(a))
	for i := range a {
		out[i] = a[i] + (b[i]-a[i])*t
	}
	return out
}

func srgbToLinear(c float32) float32 {
	if c <= 0.04045 {
		return c / 12.92
	}
	return float32(math.Pow(float64((c+0.055)/1.055), 2.4))
}

func linearToSrgb(c float32) float32 {
	if c <= 0.0031308 {
		return c * 12.92
	}
	return float32(1.055*math.Pow(float64(c), 1.0/2.4) - 0.055)
}

func clampInt(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
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
