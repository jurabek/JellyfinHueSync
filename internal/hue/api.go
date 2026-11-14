package hue

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

var (
	bridgeIDRe      = regexp.MustCompile(`^[0-9a-f]{16}$`)
	clientKeyRe     = regexp.MustCompile(`^[0-9a-fA-F]{32}$`)
	lowercaseUUIDRe = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)
)

// ErrLinkButtonRequired is returned when the Bridge link button has not been pressed.
var ErrLinkButtonRequired = errors.New("Press the button on the Bridge, then try pairing again.")

// API is a verified HTTPS client for a single Hue Bridge.
type API struct {
	ip               string
	key              string
	expectedBridgeID string
	client           *http.Client

	peerMu sync.Mutex
	peerID string
}

// NewAPI returns a Hue API client. When key is non-empty it is sent as the
// hue-application-key header on every request.
func NewAPI(bridge Bridge, key string) *API {
	a := &API{ip: bridge.IP, key: key, expectedBridgeID: bridge.ID}
	a.client = &http.Client{
		Timeout: 10 * time.Second,
		Transport: &http.Transport{
			DialContext: (&net.Dialer{Timeout: 5 * time.Second}).DialContext,
			TLSClientConfig: &tls.Config{
				InsecureSkipVerify: true, // verified manually against the Hue roots below
				VerifyPeerCertificate: func(rawCerts [][]byte, _ [][]*x509.Certificate) error {
					id, err := verifyBridgeCertificate(rawCerts, a.expectedBridgeID)
					if err != nil {
						return err
					}
					a.setPeerID(id)
					return nil
				},
			},
		},
	}
	return a
}

// Close releases idle connections.
func (a *API) Close() { a.client.CloseIdleConnections() }

func (a *API) setPeerID(id string) {
	a.peerMu.Lock()
	a.peerID = id
	a.peerMu.Unlock()
}

func (a *API) peerBridgeID() string {
	a.peerMu.Lock()
	defer a.peerMu.Unlock()
	return a.peerID
}

// Identify returns the Bridge identity, verifying that the TLS certificate and
// the Bridge's reported ID match.
func (a *API) Identify(ctx context.Context) (Bridge, error) {
	var cfg struct {
		BridgeID string `json:"bridgeid"`
		Name     string `json:"name"`
	}
	if err := a.doJSON(ctx, http.MethodGet, "/api/config", nil, &cfg); err != nil {
		return Bridge{}, err
	}
	id := strings.ToLower(cfg.BridgeID)
	if !bridgeIDRe.MatchString(id) {
		return Bridge{}, errors.New("Invalid Bridge ID")
	}
	if a.expectedBridgeID != "" && id != strings.ToLower(a.expectedBridgeID) {
		return Bridge{}, errors.New("This is not the paired Bridge")
	}
	if id != a.peerBridgeID() {
		return Bridge{}, errors.New("Bridge certificate and ID do not match")
	}
	name := cfg.Name
	if name == "" {
		name = "Hue Bridge"
	}
	return Bridge{IP: a.ip, ID: id, Name: name}, nil
}

// Pair creates an application key and client key. It retries for up to 30
// seconds so the Bridge link button can be pressed after the request begins.
func (a *API) Pair(ctx context.Context) (Credentials, error) {
	if a.expectedBridgeID == "" {
		return Credentials{}, errors.New("Identify the Bridge first")
	}

	deadline := time.Now().Add(30 * time.Second)
	for {
		creds, retry, err := a.pairAttempt(ctx)
		if err == nil {
			return creds, nil
		}
		if !retry || time.Now().After(deadline) {
			return Credentials{}, err
		}
		select {
		case <-ctx.Done():
			return Credentials{}, ctx.Err()
		case <-time.After(2 * time.Second):
		}
	}
}

func (a *API) pairAttempt(ctx context.Context) (Credentials, bool, error) {
	var resp []struct {
		Success *struct {
			Username  string `json:"username"`
			ClientKey string `json:"clientkey"`
		} `json:"success"`
		Error *struct {
			Type int `json:"type"`
		} `json:"error"`
	}
	if err := a.doJSON(ctx, http.MethodPost, "/api", map[string]any{
		"devicetype":        "huesync#jellyfin",
		"generateclientkey": true,
	}, &resp); err != nil {
		return Credentials{}, false, err
	}
	if len(resp) == 0 {
		return Credentials{}, false, errors.New("Bridge returned an empty pairing response")
	}
	first := resp[0]
	if first.Error != nil {
		if first.Error.Type == 101 {
			return Credentials{}, true, ErrLinkButtonRequired
		}
		return Credentials{}, false, fmt.Errorf("Bridge rejected pairing (%d)", first.Error.Type)
	}
	if first.Success == nil {
		return Credentials{}, false, errors.New("Bridge returned an invalid pairing response")
	}
	if !clientKeyRe.MatchString(first.Success.ClientKey) {
		return Credentials{}, false, errors.New("Invalid Hue client key")
	}
	return Credentials{
		Bridge:         Bridge{IP: a.ip, ID: a.expectedBridgeID},
		ApplicationKey: first.Success.Username,
		ClientKey:      first.Success.ClientKey,
	}, false, nil
}

// ApplicationID returns the identifier used to claim an Entertainment area.
func (a *API) ApplicationID(ctx context.Context) (string, error) {
	req, err := a.newRequest(ctx, http.MethodGet, "/auth/v1", nil)
	if err != nil {
		return "", err
	}
	resp, err := a.client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return "", fmt.Errorf("Bridge returned HTTP %d", resp.StatusCode)
	}
	if id := resp.Header.Get("hue-application-id"); id != "" {
		return id, nil
	}
	return "", errors.New("Bridge did not return hue-application-id; update its firmware.")
}

// Areas returns the configured Entertainment areas.
func (a *API) Areas(ctx context.Context) ([]Area, error) {
	configs, err := a.clipResource(ctx, http.MethodGet, "entertainment_configuration", nil)
	if err != nil {
		return nil, err
	}
	areas := make([]Area, 0, len(configs))
	for _, cfg := range configs {
		channels := make([]Channel, 0, len(cfg.Channels))
		for _, ch := range cfg.Channels {
			channels = append(channels, Channel{ID: ch.ChannelID, X: ch.Position.X, Height: ch.Position.Z})
		}
		areas = append(areas, Area{
			ID:       cfg.ID,
			Name:     cfg.Metadata.Name,
			Channels: channels,
			Active:   cfg.Status == "active",
		})
	}
	return areas, nil
}

// Action starts or stops an Entertainment area stream.
func (a *API) Action(ctx context.Context, id string, start bool) error {
	if !lowercaseUUIDRe.MatchString(id) {
		return errors.New("invalid area id")
	}
	action := "stop"
	if start {
		action = "start"
	}
	_, err := a.clipResource(ctx, http.MethodPut, "entertainment_configuration/"+id, map[string]string{"action": action})
	return err
}

// OwnedBy reports whether the area is actively streamed by applicationID.
func (a *API) OwnedBy(ctx context.Context, id, applicationID string) (bool, error) {
	configs, err := a.clipResource(ctx, http.MethodGet, "entertainment_configuration/"+id, nil)
	if err != nil {
		return false, err
	}
	if len(configs) == 0 {
		return false, nil
	}
	cfg := configs[0]
	rid := ""
	if cfg.ActiveStreamer != nil {
		rid = cfg.ActiveStreamer.RID
	}
	return cfg.Status == "active" && rid == applicationID, nil
}

func (a *API) newRequest(ctx context.Context, method, path string, body any) (*http.Request, error) {
	var r io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}
		r = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, "https://"+a.ip+path, r)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if a.key != "" {
		req.Header.Set("hue-application-key", a.key)
	}
	return req, nil
}

func (a *API) doJSON(ctx context.Context, method, path string, body, out any) error {
	req, err := a.newRequest(ctx, method, path, body)
	if err != nil {
		return err
	}
	resp, err := a.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	switch resp.StatusCode {
	case http.StatusUnauthorized, http.StatusForbidden:
		return errors.New("Hue pairing was revoked. Pair the Bridge again.")
	}
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return fmt.Errorf("Bridge returned HTTP %d", resp.StatusCode)
	}
	if out != nil {
		if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
			return fmt.Errorf("invalid Bridge response: %w", err)
		}
	}
	return nil
}

type clipError struct {
	Description string `json:"description"`
}

type entertainmentConfig struct {
	ID       string `json:"id"`
	Metadata struct {
		Name string `json:"name"`
	} `json:"metadata"`
	Channels []struct {
		ChannelID int `json:"channel_id"`
		Position  struct {
			X float32 `json:"x"`
			Z float32 `json:"z"`
		} `json:"position"`
	} `json:"channels"`
	Status         string `json:"status"`
	ActiveStreamer *struct {
		RID string `json:"rid"`
	} `json:"active_streamer"`
}

func (a *API) clipResource(ctx context.Context, method, path string, body any) ([]entertainmentConfig, error) {
	var resp struct {
		Errors []clipError           `json:"errors"`
		Data   []entertainmentConfig `json:"data"`
	}
	if err := a.doJSON(ctx, method, "/clip/v2/resource/"+path, body, &resp); err != nil {
		return nil, err
	}
	if len(resp.Errors) > 0 {
		desc := resp.Errors[0].Description
		if desc == "" {
			desc = "Bridge error"
		}
		return nil, fmt.Errorf("Hue operation rejected: %s", desc)
	}
	return resp.Data, nil
}

func verifyBridgeCertificate(rawCerts [][]byte, expectedID string) (string, error) {
	if len(rawCerts) == 0 {
		return "", errors.New("Bridge sent no certificate")
	}
	leaf, err := x509.ParseCertificate(rawCerts[0])
	if err != nil {
		return "", fmt.Errorf("invalid Bridge certificate: %w", err)
	}
	intermediates := x509.NewCertPool()
	for _, rc := range rawCerts[1:] {
		if c, err := x509.ParseCertificate(rc); err == nil {
			intermediates.AddCert(c)
		}
	}
	if _, err := leaf.Verify(x509.VerifyOptions{
		Roots:         RootCAs(),
		Intermediates: intermediates,
		KeyUsages:     []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}); err != nil {
		return "", fmt.Errorf("untrusted Bridge certificate: %w", err)
	}
	id := strings.ToLower(leaf.Subject.CommonName)
	if !bridgeIDRe.MatchString(id) {
		return "", errors.New("Bridge certificate has no valid Bridge ID")
	}
	if expectedID != "" && id != strings.ToLower(expectedID) {
		return "", errors.New("This is not the paired Bridge")
	}
	return id, nil
}

// ValidateLocalIP rejects anything that is not a site-local or link-local IPv4 address.
func ValidateLocalIP(value string) error {
	parts := strings.Split(value, ".")
	if len(parts) != 4 {
		return errors.New("Enter a local IPv4 address, such as 192.168.1.20")
	}
	var b [4]int
	for i, p := range parts {
		if p == "" || len(p) > 3 {
			return errors.New("Invalid IPv4 address")
		}
		n, err := strconv.Atoi(p)
		if err != nil || n < 0 || n > 255 {
			return errors.New("Invalid IPv4 address")
		}
		b[i] = n
	}
	siteLocal := b[0] == 10 || (b[0] == 172 && b[1] >= 16 && b[1] <= 31) || (b[0] == 192 && b[1] == 168)
	linkLocal := b[0] == 169 && b[1] == 254
	if !siteLocal && !linkLocal {
		return errors.New("The Bridge must be on the local network")
	}
	return nil
}
