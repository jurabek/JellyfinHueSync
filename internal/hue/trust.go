package hue

import (
	"crypto/x509"
	_ "embed"
)

// hueCAPEM bundles the Philips Hue root CA certificates used to verify the
// Bridge's HTTPS certificate. These are public trust anchors, not secrets.
//
//go:embed hue_ca.pem
var hueCAPEM []byte

// RootCAs returns the Hue root CA pool.
func RootCAs() *x509.CertPool {
	pool := x509.NewCertPool()
	pool.AppendCertsFromPEM(hueCAPEM)
	return pool
}
