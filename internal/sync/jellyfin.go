package sync

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// PlaybackState is the Jellyfin playback clock + media path for one session.
type PlaybackState struct {
	PositionSeconds float64
	IsPaused        bool
	Path            string
	ItemName        string
	DeviceName      string
}

// JellyfinClient reads playback state from the Jellyfin Sessions API.
type JellyfinClient struct {
	baseURL string
	apiKey  string
	client  *http.Client
}

// NewJellyfinClient returns a client for the given server URL and API key.
func NewJellyfinClient(baseURL, apiKey string) *JellyfinClient {
	return &JellyfinClient{
		baseURL: strings.TrimRight(baseURL, "/"),
		apiKey:  apiKey,
		client:  &http.Client{Timeout: 8 * time.Second},
	}
}

type session struct {
	DeviceID   string `json:"DeviceId"`
	UserID     string `json:"UserId"`
	DeviceName string `json:"DeviceName"`
	Client     string `json:"Client"`
	PlayState  *struct {
		IsPaused      bool  `json:"IsPaused"`
		PositionTicks int64 `json:"PositionTicks"`
	} `json:"PlayState"`
	NowPlayingItem *struct {
		Path      string `json:"Path"`
		Name      string `json:"Name"`
		MediaType string `json:"MediaType"`
	} `json:"NowPlayingItem"`
}

func (s session) isPaused() bool { return s.PlayState != nil && s.PlayState.IsPaused }

func (s session) positionTicks() int64 {
	if s.PlayState == nil {
		return 0
	}
	return s.PlayState.PositionTicks
}

func (s session) itemPath() string {
	if s.NowPlayingItem == nil {
		return ""
	}
	return s.NowPlayingItem.Path
}

func (s session) itemName() string {
	if s.NowPlayingItem == nil {
		return ""
	}
	return s.NowPlayingItem.Name
}

// Sessions fetches the current playback sessions.
func (c *JellyfinClient) Sessions(ctx context.Context) ([]session, error) {
	u := fmt.Sprintf("%s/Sessions?apiKey=%s", c.baseURL, url.QueryEscape(c.apiKey))
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	resp, err := c.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return nil, fmt.Errorf("Jellyfin returned HTTP %d", resp.StatusCode)
	}
	var sessions []session
	if err := json.NewDecoder(resp.Body).Decode(&sessions); err != nil {
		return nil, fmt.Errorf("invalid Jellyfin response: %w", err)
	}
	return sessions, nil
}

// Select picks the session matching userID/deviceID (or the first playing one)
// and converts it to a PlaybackState.
func (c *JellyfinClient) Select(sessions []session, userID, deviceID string) *PlaybackState {
	var candidates []session
	for _, s := range sessions {
		if s.itemPath() != "" {
			candidates = append(candidates, s)
		}
	}
	if len(candidates) == 0 {
		return nil
	}

	chosen, ok := pick(candidates, userID, deviceID)
	if !ok {
		return nil
	}
	return &PlaybackState{
		PositionSeconds: float64(chosen.positionTicks()) / 10_000_000.0,
		IsPaused:        chosen.isPaused(),
		Path:            chosen.itemPath(),
		ItemName:        chosen.itemName(),
		DeviceName:      chosen.DeviceName,
	}
}

func pick(candidates []session, userID, deviceID string) (session, bool) {
	switch {
	case deviceID != "":
		for _, s := range candidates {
			if s.DeviceID == deviceID {
				return s, true
			}
		}
		return session{}, false
	case userID != "":
		for _, s := range candidates {
			if s.UserID == userID {
				return s, true
			}
		}
		return session{}, false
	default:
		for _, s := range candidates {
			if !s.isPaused() {
				return s, true
			}
		}
		return candidates[0], true
	}
}
