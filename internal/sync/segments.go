package sync

import (
	"context"
	"math"
	"sync"

	"github.com/jurabek/JellyfinHueSync/internal/config"
	"github.com/jurabek/JellyfinHueSync/internal/hue"
)

// Segment holds the per-channel color for every decoded frame in one segment.
type Segment struct {
	frames [][]float32
}

// SegmentProvider decodes the current + next segment asynchronously and maps a
// playback position to an interpolated color snapshot.
type SegmentProvider struct {
	extractor *FfmpegExtractor
	channels  []hue.Channel
	options   config.SyncOptions

	mu         sync.Mutex
	loaded     map[int64]*Segment
	inflight   map[int64]struct{}
	hdrCache   map[string]bool
	activePath string

	queue  chan segmentJob
	ctx    context.Context
	cancel context.CancelFunc
	done   chan struct{}
}

type segmentJob struct {
	path     string
	segStart int64
	hdr      bool
}

// NewSegmentProvider starts the decode worker.
func NewSegmentProvider(extractor *FfmpegExtractor, channels []hue.Channel, options config.SyncOptions) *SegmentProvider {
	ctx, cancel := context.WithCancel(context.Background())
	p := &SegmentProvider{
		extractor: extractor,
		channels:  channels,
		options:   options,
		loaded:    make(map[int64]*Segment),
		inflight:  make(map[int64]struct{}),
		hdrCache:  make(map[string]bool),
		queue:     make(chan segmentJob, 16),
		ctx:       ctx,
		cancel:    cancel,
		done:      make(chan struct{}),
	}
	go p.worker()
	return p
}

// Request enqueues the current + next segment and evicts stale ones.
func (p *SegmentProvider) Request(path string, positionSeconds float64) {
	segStart := p.segStartOf(positionSeconds)
	hdr := p.hdrFor(path)

	p.mu.Lock()
	if p.activePath != path {
		p.loaded = make(map[int64]*Segment)
		p.inflight = make(map[int64]struct{})
		p.activePath = path
	}

	for _, s := range []int64{segStart, segStart + int64(p.options.SegmentSeconds)} {
		if _, ok := p.loaded[s]; ok {
			continue
		}
		if _, ok := p.inflight[s]; ok {
			continue
		}
		p.inflight[s] = struct{}{}
		select {
		case p.queue <- segmentJob{path: path, segStart: s, hdr: hdr}:
		default:
			delete(p.inflight, s) // queue is full; retry on the next Request
		}
	}

	cutoff := segStart - int64(p.options.SegmentSeconds)
	for k := range p.loaded {
		if k < cutoff {
			delete(p.loaded, k)
		}
	}
	p.mu.Unlock()
}

// Sample returns the interpolated color frame for a playback position, or nil
// while the segment is still decoding.
func (p *SegmentProvider) Sample(positionSeconds float64) []float32 {
	segStart := p.segStartOf(positionSeconds)
	p.mu.Lock()
	seg := p.loaded[segStart]
	p.mu.Unlock()

	if seg == nil || len(seg.frames) == 0 {
		return nil
	}
	idx := (positionSeconds - float64(segStart)) * float64(p.options.ExtractFps)
	i0 := clampInt(int(math.Floor(idx)), 0, len(seg.frames)-1)
	i1 := min(i0+1, len(seg.frames)-1)
	t := clampF(float32(idx-float64(i0)), 0, 1)
	return lerp(seg.frames[i0], seg.frames[i1], t)
}

// Close stops the worker, cancelling any in-flight ffmpeg process.
func (p *SegmentProvider) Close() {
	p.cancel()
	<-p.done
}

func (p *SegmentProvider) worker() {
	defer close(p.done)
	for {
		select {
		case <-p.ctx.Done():
			return
		case job := <-p.queue:
			seg := p.extract(job.path, job.segStart, job.hdr)
			p.mu.Lock()
			delete(p.inflight, job.segStart)
			if p.activePath == job.path {
				p.loaded[job.segStart] = seg
			}
			p.mu.Unlock()
		}
	}
}

func (p *SegmentProvider) extract(path string, segStart int64, hdr bool) *Segment {
	w := p.options.FrameWidth
	h := p.options.FrameHeight
	raw, err := p.extractor.Extract(p.ctx, path, float64(segStart), float64(p.options.SegmentSeconds), w, h, p.options.ExtractFps, hdr)
	seg := &Segment{}
	if err != nil {
		return seg
	}
	frameBytes := w * h * 3
	count := len(raw) / frameBytes
	for i := 0; i < count; i++ {
		seg.frames = append(seg.frames, sampleFrame(raw, i*frameBytes, w, h, p.channels, p.options))
	}
	return seg
}

func (p *SegmentProvider) segStartOf(position float64) int64 {
	return int64(math.Floor(position/float64(p.options.SegmentSeconds)) * float64(p.options.SegmentSeconds))
}

func (p *SegmentProvider) hdrFor(path string) bool {
	p.mu.Lock()
	if v, ok := p.hdrCache[path]; ok {
		p.mu.Unlock()
		return v
	}
	p.mu.Unlock()

	v := p.extractor.IsHdr(path)

	p.mu.Lock()
	p.hdrCache[path] = v
	p.mu.Unlock()
	return v
}
