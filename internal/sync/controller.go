// Package sync orchestrates the ambient-lighting pipeline: Jellyfin playback
// clock -> ffmpeg decode -> per-channel color mapping -> smoothing -> DTLS
// Entertainment stream.
package sync

import (
	"context"
	"fmt"
	"os"
	"path"
	"strings"
	"sync"
	"time"

	"github.com/jurabek/JellyfinHueSync/internal/config"
	"github.com/jurabek/JellyfinHueSync/internal/hue"
)

// Controller runs one sync session at a time and publishes live status to the UI.
type Controller struct {
	config *config.Store

	mu      sync.Mutex
	running bool
	gen     uint64
	cancel  context.CancelFunc
	stream  *hue.Stream
	done    chan struct{}

	uiMu sync.RWMutex
	ui   uiSnapshot
}

type uiSnapshot struct {
	syncing  bool
	message  string
	colors   []int
	playback *PlaybackState
	packets  int64
}

// NewController returns a ready controller.
func NewController(store *config.Store) *Controller {
	return &Controller{
		config: store,
		ui:     uiSnapshot{message: "Ready"},
	}
}

// Start launches a sync session, returning a user-facing error when the app is
// not fully configured.
func (c *Controller) Start() string {
	cfg := c.config.Config()
	if cfg.Credentials() == nil {
		return "Pair the Hue Bridge first"
	}
	if cfg.AreaId == "" {
		return "Select an Entertainment area first"
	}
	if !cfg.JellyfinConfigured() {
		return "Configure Jellyfin first"
	}

	c.mu.Lock()
	if c.running {
		c.mu.Unlock()
		return ""
	}
	c.running = true
	c.gen++
	gen := c.gen
	ctx, cancel := context.WithCancel(context.Background())
	c.cancel = cancel
	prevDone := c.done
	c.done = make(chan struct{})
	done := c.done
	c.mu.Unlock()

	// Serialize sessions: wait for any previous cleanup (e.g. releasing the
	// Entertainment area) to finish before starting a new one.
	if prevDone != nil {
		select {
		case <-prevDone:
		case <-time.After(5 * time.Second):
		}
	}

	c.setSyncing(true)
	c.setMessage("Connecting to Bridge…")
	go c.run(ctx, gen, done)
	return ""
}

// Stop halts the active session, if any.
func (c *Controller) Stop() {
	c.mu.Lock()
	if !c.running {
		c.mu.Unlock()
		return
	}
	c.running = false
	c.gen++
	cancel := c.cancel
	c.cancel = nil
	stream := c.stream
	c.stream = nil
	c.mu.Unlock()

	c.setMessage("Stopped")
	c.setSyncing(false)
	if stream != nil {
		_ = stream.Close()
	}
	if cancel != nil {
		cancel()
	}
}

func (c *Controller) run(ctx context.Context, gen uint64, done chan struct{}) {
	defer close(done)
	defer c.finish(gen)

	cfg := c.config.Config()
	creds := cfg.Credentials()
	if creds == nil {
		return
	}

	api := hue.NewAPI(creds.Bridge, creds.ApplicationKey)
	defer api.Close()

	area, applicationID, err := c.prepare(ctx, gen, api, cfg)
	if err != nil {
		c.fail(gen, err.Error())
		return
	}

	options := cfg.Options.Validated()
	packet, err := hue.NewPacket(area.ID, area.Channels)
	if err != nil {
		c.fail(gen, err.Error())
		return
	}

	filter := NewSmoothFilter(len(area.Channels))
	segments := NewSegmentProvider(NewFfmpegExtractor(), area.Channels, options)
	defer segments.Close()

	jellyfin := NewJellyfinClient(cfg.JellyfinUrl, cfg.JellyfinApiKey)

	if err := api.Action(ctx, area.ID, true); err != nil {
		c.fail(gen, "Bridge error: "+err.Error())
		return
	}
	defer stopArea(api, area.ID, applicationID)

	stream, err := hue.DialStream(ctx, creds.Bridge.IP, creds.ApplicationKey, creds.ClientKey)
	if err != nil {
		c.fail(gen, "Stream connect failed: "+err.Error())
		return
	}
	c.mu.Lock()
	c.stream = stream
	c.mu.Unlock()
	defer func() {
		c.mu.Lock()
		if c.stream == stream {
			c.stream = nil
		}
		c.mu.Unlock()
		_ = stream.Close()
	}()

	owned, err := api.OwnedBy(ctx, area.ID, applicationID)
	if err != nil {
		c.fail(gen, "Bridge error: "+err.Error())
		return
	}
	if !owned {
		c.fail(gen, "Bridge ended the stream")
		return
	}

	go c.ownerLoop(ctx, gen, api, area.ID, applicationID)
	c.setMessage("Synchronization active")
	c.syncLoop(ctx, gen, cfg, options, packet, filter, segments, jellyfin, stream, len(area.Channels))
}

// prepare fetches areas, selects the configured one, and claims it.
func (c *Controller) prepare(ctx context.Context, gen uint64, api *hue.API, cfg config.AppConfig) (*hue.Area, string, error) {
	areas, err := api.Areas(ctx)
	if err != nil {
		return nil, "", fmt.Errorf("Bridge error: %w", err)
	}
	var area *hue.Area
	for i := range areas {
		if areas[i].ID == cfg.AreaId {
			area = &areas[i]
			break
		}
	}
	if area == nil {
		return nil, "", fmt.Errorf("Entertainment area is no longer available")
	}
	if len(area.Channels) == 0 || len(area.Channels) > hue.MaxChannels {
		return nil, "", fmt.Errorf("Area has no compatible channels (max %d)", hue.MaxChannels)
	}

	applicationID, err := api.ApplicationID(ctx)
	if err != nil {
		return nil, "", fmt.Errorf("Bridge error: %w", err)
	}

	if area.Active {
		owned, err := api.OwnedBy(ctx, area.ID, applicationID)
		if err != nil {
			return nil, "", fmt.Errorf("Bridge error: %w", err)
		}
		if !owned {
			return nil, "", fmt.Errorf("Area is already in use by another Hue app")
		}
		if err := api.Action(ctx, area.ID, false); err != nil {
			return nil, "", fmt.Errorf("Bridge error: %w", err)
		}
	}
	if !c.current(gen) {
		return nil, "", context.Canceled
	}
	return area, applicationID, nil
}

func (c *Controller) syncLoop(ctx context.Context, gen uint64, cfg config.AppConfig, options config.SyncOptions, packet *hue.Packet, filter *SmoothFilter, segments *SegmentProvider, jellyfin *JellyfinClient, stream *hue.Stream, channelCount int) {
	tick := time.Second / time.Duration(options.StreamRate)
	nextTick := time.Now()
	nextPoll := time.Time{}

	lastColors := make([]float32, channelCount*3)
	black := make([]float32, channelCount*3)

	var baseState *PlaybackState
	baseAt := time.Now()

	for {
		if !c.current(gen) || ctx.Err() != nil {
			return
		}

		now := time.Now()
		if now.After(nextPoll) {
			nextPoll = now.Add(time.Second)
			s := c.pollJellyfin(ctx, gen, cfg, jellyfin)
			baseState = s
			baseAt = time.Now()
			c.setPlayback(s)
		}

		colors := c.computeColors(gen, baseState, cfg, options, baseAt, time.Now(), segments, filter, lastColors, black)
		lastColors = colors

		if err := c.send(packet, stream, colors); err != nil {
			c.fail(gen, "Stream failed: "+err.Error())
			return
		}
		c.publishColors(colors)

		nextTick = nextTick.Add(tick)
		if d := time.Until(nextTick); d > 0 {
			select {
			case <-ctx.Done():
				return
			case <-time.After(d):
			}
		} else {
			nextTick = time.Now()
		}
	}
}

func (c *Controller) computeColors(gen uint64, state *PlaybackState, cfg config.AppConfig, options config.SyncOptions, baseAt time.Time, now time.Time, segments *SegmentProvider, filter *SmoothFilter, lastColors, black []float32) []float32 {
	if state == nil {
		c.setMessage("No active Jellyfin playback")
		return black
	}
	if state.IsPaused {
		c.setMessage("Paused · holding last colors")
		return lastColors
	}

	mapped := mapPath(state.Path, cfg.MediaMappings)
	if mapped == "" {
		leaf := path.Base(state.Path)
		if leaf == "." || leaf == "/" {
			leaf = state.Path
		}
		c.setMessage("Media not mounted: " + leaf)
		return black
	}

	position := state.PositionSeconds + now.Sub(baseAt).Seconds()
	segments.Request(mapped, position)
	sample := segments.Sample(position)
	if sample == nil {
		c.setMessage("Buffering…")
		return lastColors
	}

	msg := "Syncing"
	if state.ItemName != "" {
		msg += " · " + state.ItemName
	}
	if state.DeviceName != "" {
		msg += " · " + state.DeviceName
	}
	c.setMessage(msg)
	return filter.Apply(sample, now.UnixNano(), options)
}

func (c *Controller) pollJellyfin(ctx context.Context, gen uint64, cfg config.AppConfig, jf *JellyfinClient) *PlaybackState {
	sessions, err := jf.Sessions(ctx)
	if err != nil {
		if c.current(gen) {
			c.setMessage("Jellyfin error: " + err.Error())
		}
		return nil
	}
	return jf.Select(sessions, cfg.JellyfinUserId, cfg.JellyfinDeviceId)
}

func (c *Controller) ownerLoop(ctx context.Context, gen uint64, api *hue.API, areaID, applicationID string) {
	for {
		select {
		case <-ctx.Done():
			return
		case <-time.After(3 * time.Second):
		}
		if !c.current(gen) {
			return
		}
		owned, err := api.OwnedBy(ctx, areaID, applicationID)
		if err != nil {
			if c.current(gen) {
				c.fail(gen, "Ownership check failed: "+err.Error())
			}
			return
		}
		if !owned {
			c.fail(gen, "Bridge ended the stream or another app took over")
			return
		}
	}
}

func (c *Controller) send(packet *hue.Packet, stream *hue.Stream, colors []float32) error {
	data, err := packet.Update(colors)
	if err != nil {
		return err
	}
	if err := stream.Send(data); err != nil {
		return err
	}
	c.uiMu.Lock()
	c.ui.packets++
	c.uiMu.Unlock()
	return nil
}

func (c *Controller) publishColors(colors []float32) {
	if len(colors) == 0 {
		return
	}
	arr := make([]int, len(colors)/3)
	for i := range arr {
		r := clampInt(int(colors[i*3]*255), 0, 255)
		g := clampInt(int(colors[i*3+1]*255), 0, 255)
		b := clampInt(int(colors[i*3+2]*255), 0, 255)
		arr[i] = r<<16 | g<<8 | b
	}
	c.uiMu.Lock()
	c.ui.colors = arr
	c.uiMu.Unlock()
}

// fail marks the session as failed and unblocks its loops.
func (c *Controller) fail(gen uint64, msg string) {
	c.mu.Lock()
	if c.gen != gen {
		c.mu.Unlock()
		return
	}
	c.running = false
	cancel := c.cancel
	c.cancel = nil
	stream := c.stream
	c.stream = nil
	c.mu.Unlock()

	c.setMessage(msg)
	c.setSyncing(false)
	if stream != nil {
		_ = stream.Close()
	}
	if cancel != nil {
		cancel()
	}
}

// finish runs when run returns; it only resets state it still owns.
func (c *Controller) finish(gen uint64) {
	c.mu.Lock()
	current := c.gen == gen
	if current {
		c.running = false
		c.cancel = nil
		c.stream = nil
	}
	c.mu.Unlock()
	if current {
		c.setSyncing(false)
	}
}

func (c *Controller) current(gen uint64) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.gen == gen
}

func (c *Controller) setMessage(msg string) {
	c.uiMu.Lock()
	c.ui.message = msg
	c.uiMu.Unlock()
}

func (c *Controller) setSyncing(v bool) {
	c.uiMu.Lock()
	c.ui.syncing = v
	c.uiMu.Unlock()
}

func (c *Controller) setPlayback(p *PlaybackState) {
	c.uiMu.Lock()
	c.ui.playback = p
	c.uiMu.Unlock()
}

// Running reports whether a sync session is active.
func (c *Controller) Running() bool {
	c.uiMu.RLock()
	defer c.uiMu.RUnlock()
	return c.ui.syncing
}

// Message returns the current status line.
func (c *Controller) Message() string {
	c.uiMu.RLock()
	defer c.uiMu.RUnlock()
	return c.ui.message
}

// Colors returns a copy of the live color swatches (RGB ints).
func (c *Controller) Colors() []int {
	c.uiMu.RLock()
	defer c.uiMu.RUnlock()
	return append([]int(nil), c.ui.colors...)
}

// Playback returns the current playback state, or nil.
func (c *Controller) Playback() *PlaybackState {
	c.uiMu.RLock()
	defer c.uiMu.RUnlock()
	return c.ui.playback
}

// Packets returns the number of packets sent.
func (c *Controller) Packets() int64 {
	c.uiMu.RLock()
	defer c.uiMu.RUnlock()
	return c.ui.packets
}

func stopArea(api *hue.API, areaID, applicationID string) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	owned, err := api.OwnedBy(ctx, areaID, applicationID)
	if err != nil || !owned {
		return
	}
	_ = api.Action(ctx, areaID, false)
}

func mapPath(p string, mappings []config.PathMapping) string {
	if p == "" {
		return ""
	}
	if _, err := os.Stat(p); err == nil {
		return p
	}
	for _, m := range mappings {
		jp := strings.TrimRight(m.Jellyfin, "/")
		if jp == "" {
			continue
		}
		var rel string
		switch {
		case p == jp:
			rel = ""
		case strings.HasPrefix(p, jp+"/"):
			rel = strings.TrimLeft(p[len(jp):], "/")
		default:
			continue
		}
		cp := strings.TrimRight(m.Container, "/")
		mapped := cp
		if rel != "" {
			mapped = cp + "/" + rel
		}
		if _, err := os.Stat(mapped); err == nil {
			return mapped
		}
	}
	return ""
}
