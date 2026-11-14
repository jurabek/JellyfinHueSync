package hue

import (
	"context"
	"encoding/hex"
	"errors"
	"net"
	"time"

	"github.com/pion/dtls/v3"
)

// Stream is a DTLS 1.2 PSK connection to the Hue Entertainment port.
type Stream struct {
	conn *dtls.Conn
}

// DialStream establishes a DTLS-PSK connection to the Bridge (UDP port 2100).
// identity is the Hue application key; clientKey is the 32-hex pairing secret.
func DialStream(ctx context.Context, ip, identity, clientKey string) (*Stream, error) {
	if err := ValidateLocalIP(ip); err != nil {
		return nil, err
	}
	if !clientKeyRe.MatchString(clientKey) {
		return nil, errors.New("Invalid Hue client key")
	}
	psk, err := hex.DecodeString(clientKey)
	if err != nil || len(psk) != 16 {
		return nil, errors.New("Invalid Hue client key")
	}
	defer clear(psk)

	conn, err := dtls.Dial("udp", &net.UDPAddr{IP: net.ParseIP(ip), Port: 2100}, &dtls.Config{
		PSK:             func([]byte) ([]byte, error) { return psk, nil },
		PSKIdentityHint: []byte(identity),
		CipherSuites:    []dtls.CipherSuiteID{dtls.TLS_PSK_WITH_AES_128_GCM_SHA256},
	})
	if err != nil {
		return nil, err
	}

	handshakeCtx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	if err := conn.HandshakeContext(handshakeCtx); err != nil {
		_ = conn.Close()
		return nil, err
	}
	return &Stream{conn: conn}, nil
}

// Send writes a packet to the Bridge.
func (s *Stream) Send(packet []byte) error {
	_, err := s.conn.Write(packet)
	return err
}

// Close closes the connection, unblocking any in-flight write or handshake.
func (s *Stream) Close() error {
	if s.conn == nil {
		return nil
	}
	return s.conn.Close()
}
