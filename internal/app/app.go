// Package app wires configuration, Hue, and sync into the HTTP API.
package app

import (
	"context"
	"strings"
	stdsync "sync"

	"github.com/jurabek/JellyfinHueSync/internal/config"
	"github.com/jurabek/JellyfinHueSync/internal/hue"
	"github.com/jurabek/JellyfinHueSync/internal/sync"
)

// App is the application facade used by the HTTP handlers.
type App struct {
	config *config.Store
	sync   *sync.Controller

	mu          stdsync.Mutex
	discovering bool
	discovered  []hue.Bridge
	areas       []hue.Area
}

// New wires the configuration store and sync controller.
func New(store *config.Store) *App {
	return &App{
		config: store,
		sync:   sync.NewController(store),
	}
}

// Identify looks up a Bridge by IP.
func (a *App) Identify(ctx context.Context, ip string) (hue.Bridge, error) {
	if err := hue.ValidateLocalIP(ip); err != nil {
		return hue.Bridge{}, err
	}
	api := hue.NewAPI(hue.Bridge{IP: ip}, "")
	defer api.Close()
	return api.Identify(ctx)
}

// Pair identifies then pairs with a Bridge, persisting the credentials.
func (a *App) Pair(ctx context.Context, ip string) (hue.Bridge, error) {
	identified, err := a.Identify(ctx, ip)
	if err != nil {
		return hue.Bridge{}, err
	}
	api := hue.NewAPI(identified, "")
	defer api.Close()
	creds, err := api.Pair(ctx)
	if err != nil {
		return hue.Bridge{}, err
	}
	creds.Bridge.Name = identified.Name

	a.config.Mutate(func(c *config.AppConfig) {
		c.BridgeIp = creds.Bridge.IP
		c.BridgeId = creds.Bridge.ID
		c.BridgeName = creds.Bridge.Name
		c.ApplicationKey = creds.ApplicationKey
		c.ClientKey = creds.ClientKey
	})
	a.RefreshAreas(ctx)
	return creds.Bridge, nil
}

// Discover starts a background subnet scan for Bridges.
func (a *App) Discover() {
	a.mu.Lock()
	if a.discovering {
		a.mu.Unlock()
		return
	}
	a.discovering = true
	a.discovered = nil
	a.mu.Unlock()

	go func() {
		found := hue.Discover(context.Background())
		a.mu.Lock()
		a.discovered = found
		a.discovering = false
		a.mu.Unlock()
	}()
}

// RefreshAreas reloads the Entertainment areas from the Bridge.
func (a *App) RefreshAreas(ctx context.Context) []hue.Area {
	creds := a.config.Config().Credentials()
	if creds == nil {
		a.mu.Lock()
		a.areas = nil
		a.mu.Unlock()
		return nil
	}
	api := hue.NewAPI(creds.Bridge, creds.ApplicationKey)
	defer api.Close()
	areas, err := api.Areas(ctx)
	if err != nil {
		areas = nil
	}
	a.mu.Lock()
	a.areas = areas
	a.mu.Unlock()
	return areas
}

// SelectArea persists the chosen Entertainment area.
func (a *App) SelectArea(id string) {
	a.config.Mutate(func(c *config.AppConfig) { c.AreaId = id })
}

// SaveJellyfin persists the Jellyfin connection settings.
func (a *App) SaveJellyfin(r jellyfinRequest) {
	a.config.Mutate(func(c *config.AppConfig) {
		c.JellyfinUrl = strings.TrimRight(r.URL, "/")
		c.JellyfinApiKey = r.APIKey
		c.JellyfinUserId = r.UserID
		c.JellyfinDeviceId = r.DeviceID
		c.MediaMappings = r.toConfigMappings()
	})
}

// TestJellyfin checks connectivity to a Jellyfin server.
func (a *App) TestJellyfin(ctx context.Context, url, apiKey string) bool {
	_, err := sync.NewJellyfinClient(url, apiKey).Sessions(ctx)
	return err == nil
}

// UpdateOptions persists the (validated) sync options.
func (a *App) UpdateOptions(o syncOptionsDTO) {
	a.config.Mutate(func(c *config.AppConfig) {
		c.Options = o.toConfig().Validated()
	})
}

// StartSync starts a session and returns an error message, if any.
func (a *App) StartSync() string { return a.sync.Start() }

// StopSync stops the active session.
func (a *App) StopSync() { a.sync.Stop() }

// State assembles the UI state snapshot.
func (a *App) State() uiState {
	cfg := a.config.Config()

	a.mu.Lock()
	discovering := a.discovering
	discovered := append([]hue.Bridge(nil), a.discovered...)
	areas := append([]hue.Area(nil), a.areas...)
	a.mu.Unlock()

	return uiState{
		Paired:             cfg.Credentials() != nil,
		BridgeIP:           cfg.BridgeIp,
		BridgeName:         cfg.BridgeName,
		BridgeID:           cfg.BridgeId,
		Areas:              toAreaDTOs(areas),
		AreaID:             cfg.AreaId,
		JellyfinConfigured: cfg.JellyfinConfigured(),
		JellyfinURL:        cfg.JellyfinUrl,
		JellyfinUserID:     cfg.JellyfinUserId,
		JellyfinDeviceID:   cfg.JellyfinDeviceId,
		MediaMappings:      toPathMappingDTOs(cfg.MediaMappings),
		Syncing:            a.sync.Running(),
		Status:             a.sync.Message(),
		Playback:           toPlaybackDTO(a.sync.Playback()),
		Colors:             a.sync.Colors(),
		Packets:            a.sync.Packets(),
		Options:            toOptionsDTO(cfg.Options.Validated()),
		Discovering:        discovering,
		Discovered:         toBridgeDTOs(discovered),
	}
}
