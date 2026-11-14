package hue

import (
	"errors"
	"fmt"
)

const (
	// MaxChannels is the maximum number of channels in a Hue Entertainment area.
	MaxChannels = 20

	packetHeaderBytes  = 52
	packetChannelBytes = 7
)

// Packet encodes the HueStream 2.0 protocol: header, area UUID, and seven
// bytes per channel (id + RGB as three big-endian 16-bit values).
type Packet struct {
	data     []byte
	sequence uint8
}

// NewPacket validates the area and channels and prepares a reusable packet.
func NewPacket(areaID string, channels []Channel) (*Packet, error) {
	if !lowercaseUUIDRe.MatchString(areaID) {
		return nil, errors.New("Area id must be a lowercase UUID")
	}
	if len(channels) == 0 || len(channels) > MaxChannels {
		return nil, errors.New("Area must have between 1 and 20 channels")
	}
	seen := make(map[int]struct{}, len(channels))
	for _, ch := range channels {
		if ch.ID < 0 || ch.ID > 255 {
			return nil, errors.New("Channel id out of range")
		}
		if _, dup := seen[ch.ID]; dup {
			return nil, errors.New("Channel ids must be unique")
		}
		seen[ch.ID] = struct{}{}
	}

	p := &Packet{data: make([]byte, packetHeaderBytes+len(channels)*packetChannelBytes)}
	copy(p.data, "HueStream")
	p.data[9] = 2 // protocol version major; bytes 10..15 stay zero (incl. sequence at 11)
	copy(p.data[16:], areaID)
	for i, ch := range channels {
		p.data[packetHeaderBytes+i*packetChannelBytes] = byte(ch.ID) // rgb stays zero
	}
	return p, nil
}

// Update writes the next color frame and returns the wire bytes.
func (p *Packet) Update(rgb []float32) ([]byte, error) {
	expected := (len(p.data) - packetHeaderBytes) / packetChannelBytes * 3
	if len(rgb) != expected {
		return nil, fmt.Errorf("expected %d color components, got %d", expected, len(rgb))
	}
	p.data[11] = byte(p.sequence)
	p.sequence++
	for i, v := range rgb {
		val := uint16(clampF32(v, 0, 1)*65535 + 0.5)
		off := packetHeaderBytes + 1 + (i/3)*packetChannelBytes + (i%3)*2
		p.data[off] = byte(val >> 8)
		p.data[off+1] = byte(val)
	}
	return p.data, nil
}
