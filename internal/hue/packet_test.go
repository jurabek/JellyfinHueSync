package hue

import (
	"bytes"
	"testing"
)

func TestNewPacketRejectsBadInput(t *testing.T) {
	valid := []Channel{{ID: 1, X: 0, Height: 0.5}, {ID: 2, X: 0.5, Height: 0.5}}
	const areaID = "550e8400-e29b-41d4-a716-446655440000"

	if _, err := NewPacket("not-a-uuid", valid); err == nil {
		t.Error("expected error for non-UUID area id")
	}
	if _, err := NewPacket(areaID, nil); err == nil {
		t.Error("expected error for zero channels")
	}
	if _, err := NewPacket(areaID, []Channel{{ID: 1}, {ID: 1}}); err == nil {
		t.Error("expected error for duplicate channel ids")
	}
	if _, err := NewPacket(areaID, []Channel{{ID: 256}}); err == nil {
		t.Error("expected error for out-of-range channel id")
	}
}

func TestPacketUpdate(t *testing.T) {
	const areaID = "550e8400-e29b-41d4-a716-446655440000"
	channels := []Channel{{ID: 7}, {ID: 200}}

	p, err := NewPacket(areaID, channels)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(p.data[:9], []byte("HueStream")) {
		t.Fatalf("magic = %q", p.data[:9])
	}
	if p.data[9] != 2 {
		t.Fatalf("version = %d, want 2", p.data[9])
	}
	if !bytes.Equal(p.data[16:52], []byte(areaID)) {
		t.Fatalf("area id = %q", p.data[16:52])
	}

	data, err := p.Update([]float32{1, 0, 0.5, 0, 1, 0.25})
	if err != nil {
		t.Fatal(err)
	}
	if data[11] != 0 {
		t.Fatalf("sequence = %d, want 0", data[11])
	}

	base := packetHeaderBytes
	if got := data[base]; got != 7 {
		t.Fatalf("channel id = %d, want 7", got)
	}
	if got := int(data[base+1])<<8 | int(data[base+2]); got != 0xffff {
		t.Fatalf("red = %#x, want 0xffff", got)
	}
	if got := int(data[base+5])<<8 | int(data[base+6]); got != 0x8000 {
		t.Fatalf("blue = %#x, want 0x8000", got)
	}
	if got := data[base+packetChannelBytes]; got != 200 {
		t.Fatalf("channel id = %d, want 200", got)
	}

	if data, _ = p.Update([]float32{0, 0, 0, 0, 0, 0}); data[11] != 1 {
		t.Fatalf("sequence = %d, want 1", data[11])
	}
}
