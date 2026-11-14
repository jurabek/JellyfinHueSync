// Package hue implements the Philips Hue Bridge integration: verified HTTPS
// API access, local discovery, the Entertainment DTLS-PSK stream, and the
// HueStream 2.0 packet format.
package hue

// Bridge identifies a Philips Hue Bridge on the local network.
type Bridge struct {
	IP   string
	ID   string
	Name string
}

// Credentials are the secrets returned by a successful pairing.
type Credentials struct {
	Bridge         Bridge
	ApplicationKey string
	ClientKey      string
}

// Channel is one lamp position inside an Entertainment area.
//
// X is horizontal (-1 left, +1 right); Height (z) is 0 (bottom) to 1 (top).
type Channel struct {
	ID     int
	X      float32
	Height float32
}

// Area is an Entertainment area with its ordered list of channels.
type Area struct {
	ID       string
	Name     string
	Channels []Channel
	Active   bool
}

// ScreenX maps the -1..1 horizontal position to a 0..1 screen coordinate.
func (c Channel) ScreenX() float32 {
	return clampF32((c.X+1)*0.5, 0.08, 0.92)
}

// ScreenY maps the 0..1 height to a 0..1 screen coordinate.
func (c Channel) ScreenY() float32 {
	return clampF32((1-c.Height)*0.5, 0.08, 0.92)
}

// SampleX returns the horizontal sample coordinate, optionally inverted.
func (c Channel) SampleX(invert bool) float32 {
	if invert {
		return clampF32((1-c.X)*0.5, 0.08, 0.92)
	}
	return c.ScreenX()
}

func clampF32(v, lo, hi float32) float32 {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}
