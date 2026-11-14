package app

import (
	"github.com/jurabek/JellyfinHueSync/internal/config"
	"github.com/jurabek/JellyfinHueSync/internal/hue"
	"github.com/jurabek/JellyfinHueSync/internal/sync"
)

type bridgeDTO struct {
	IP   string `json:"ip"`
	ID   string `json:"id"`
	Name string `json:"name"`
}

type areaDTO struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Channels int    `json:"channels"`
	Active   bool   `json:"active"`
}

type playbackDTO struct {
	ItemName        string  `json:"itemName"`
	DeviceName      string  `json:"deviceName"`
	PositionSeconds float64 `json:"positionSeconds"`
	IsPaused        bool    `json:"isPaused"`
}

type pathMappingDTO struct {
	Jellyfin  string `json:"jellyfin"`
	Container string `json:"container"`
}

type syncOptionsDTO struct {
	Brightness       float32 `json:"brightness"`
	Saturation       float32 `json:"saturation"`
	SmoothingMs      int     `json:"smoothingMs"`
	InvertHorizontal bool    `json:"invertHorizontal"`
	ExtractFps       int     `json:"extractFps"`
	StreamRate       int     `json:"streamRate"`
	FrameWidth       int     `json:"frameWidth"`
	FrameHeight      int     `json:"frameHeight"`
	SegmentSeconds   int     `json:"segmentSeconds"`
}

type uiState struct {
	Paired             bool             `json:"paired"`
	BridgeIP           string           `json:"bridgeIp"`
	BridgeName         string           `json:"bridgeName"`
	BridgeID           string           `json:"bridgeId"`
	Areas              []areaDTO        `json:"areas"`
	AreaID             string           `json:"areaId"`
	JellyfinConfigured bool             `json:"jellyfinConfigured"`
	JellyfinURL        string           `json:"jellyfinUrl"`
	JellyfinUserID     string           `json:"jellyfinUserId"`
	JellyfinDeviceID   string           `json:"jellyfinDeviceId"`
	MediaMappings      []pathMappingDTO `json:"mediaMappings"`
	Syncing            bool             `json:"syncing"`
	Status             string           `json:"status"`
	Playback           *playbackDTO     `json:"playback"`
	Colors             []int            `json:"colors"`
	Packets            int64            `json:"packets"`
	Options            syncOptionsDTO   `json:"options"`
	Discovering        bool             `json:"discovering"`
	Discovered         []bridgeDTO      `json:"discovered"`
}

type identifyRequest struct {
	IP string `json:"ip"`
}

type areaRequest struct {
	ID string `json:"id"`
}

type jellyfinRequest struct {
	URL           string           `json:"url"`
	APIKey        string           `json:"apiKey"`
	UserID        string           `json:"userId"`
	DeviceID      string           `json:"deviceId"`
	MediaMappings []pathMappingDTO `json:"mediaMappings"`
}

type apiError struct {
	Error string `json:"error"`
}

func toBridgeDTO(b hue.Bridge) bridgeDTO {
	return bridgeDTO{IP: b.IP, ID: b.ID, Name: b.Name}
}

func toBridgeDTOs(bridges []hue.Bridge) []bridgeDTO {
	out := make([]bridgeDTO, 0, len(bridges))
	for _, b := range bridges {
		out = append(out, toBridgeDTO(b))
	}
	return out
}

func toAreaDTOs(areas []hue.Area) []areaDTO {
	out := make([]areaDTO, 0, len(areas))
	for _, a := range areas {
		out = append(out, areaDTO{ID: a.ID, Name: a.Name, Channels: len(a.Channels), Active: a.Active})
	}
	return out
}

func toPathMappingDTOs(ms []config.PathMapping) []pathMappingDTO {
	out := make([]pathMappingDTO, 0, len(ms))
	for _, m := range ms {
		out = append(out, pathMappingDTO{Jellyfin: m.Jellyfin, Container: m.Container})
	}
	return out
}

func (o syncOptionsDTO) toConfig() config.SyncOptions {
	return config.SyncOptions{
		Brightness:       o.Brightness,
		Saturation:       o.Saturation,
		SmoothingMs:      o.SmoothingMs,
		InvertHorizontal: o.InvertHorizontal,
		ExtractFps:       o.ExtractFps,
		StreamRate:       o.StreamRate,
		FrameWidth:       o.FrameWidth,
		FrameHeight:      o.FrameHeight,
		SegmentSeconds:   o.SegmentSeconds,
	}
}

func toOptionsDTO(o config.SyncOptions) syncOptionsDTO {
	return syncOptionsDTO{
		Brightness:       o.Brightness,
		Saturation:       o.Saturation,
		SmoothingMs:      o.SmoothingMs,
		InvertHorizontal: o.InvertHorizontal,
		ExtractFps:       o.ExtractFps,
		StreamRate:       o.StreamRate,
		FrameWidth:       o.FrameWidth,
		FrameHeight:      o.FrameHeight,
		SegmentSeconds:   o.SegmentSeconds,
	}
}

func toPlaybackDTO(p *sync.PlaybackState) *playbackDTO {
	if p == nil {
		return nil
	}
	return &playbackDTO{
		ItemName:        p.ItemName,
		DeviceName:      p.DeviceName,
		PositionSeconds: p.PositionSeconds,
		IsPaused:        p.IsPaused,
	}
}

func (r jellyfinRequest) toConfigMappings() []config.PathMapping {
	out := make([]config.PathMapping, 0, len(r.MediaMappings))
	for _, m := range r.MediaMappings {
		out = append(out, config.PathMapping{Jellyfin: m.Jellyfin, Container: m.Container})
	}
	return out
}
