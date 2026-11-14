package sync

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"sync"
	"time"
)

const (
	maxFfmpegOutput = 256 * 1024 * 1024
	maxFfmpegStderr = 64 * 1024
	extractTimeout  = 60 * time.Second
	probeTimeout    = 15 * time.Second
)

var errOutputLimit = errors.New("output exceeds limit")

// limitedBuffer caps how much data a child process may write. Once the cap is
// exceeded it records the overflow, stops the copy (which closes the pipe and
// lets the child exit) and reports a sentinel error.
type limitedBuffer struct {
	bytes.Buffer
	limit    int64
	exceeded bool
}

func (b *limitedBuffer) Write(p []byte) (int, error) {
	if int64(b.Buffer.Len())+int64(len(p)) > b.limit {
		b.exceeded = true
		return 0, errOutputLimit
	}
	return b.Buffer.Write(p)
}

// FfmpegExtractor decodes original media files to low-resolution RGB frames.
type FfmpegExtractor struct {
	mu       sync.Mutex
	hdrCache map[string]bool
	hasHdr   bool
}

// NewFfmpegExtractor probes ffmpeg for HDR filter support on construction.
func NewFfmpegExtractor() *FfmpegExtractor {
	return &FfmpegExtractor{
		hdrCache: make(map[string]bool),
		hasHdr:   detectHdrSupport(),
	}
}

// HasHdrSupport reports whether ffmpeg has zscale + tonemap filters.
func (e *FfmpegExtractor) HasHdrSupport() bool { return e.hasHdr }

// IsHdr reports whether the file's video stream uses an HDR transfer function.
func (e *FfmpegExtractor) IsHdr(path string) bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	if v, ok := e.hdrCache[path]; ok {
		return v
	}
	v := probeHdr(path)
	e.hdrCache[path] = v
	return v
}

// Extract decodes [startSeconds, startSeconds+durationSeconds] to raw RGB24.
func (e *FfmpegExtractor) Extract(ctx context.Context, path string, startSeconds, durationSeconds float64, w, h, fps int, hdr bool) ([]byte, error) {
	vf := fmt.Sprintf("fps=%d,scale=%d:%d:flags=area,format=rgb24", fps, w, h)
	if hdr && e.hasHdr {
		vf = fmt.Sprintf("fps=%d,scale=%d:%d:flags=area,zscale=t=linear:npl=100,format=gbrpf32le,zscale=p=bt709,tonemap=tonemap=hable:desat=0,zscale=t=bt709:m=bt709:r=tv,format=rgb24", fps, w, h)
	}

	ctx, cancel := context.WithTimeout(ctx, extractTimeout)
	defer cancel()

	var stdout, stderr limitedBuffer
	stdout.limit = maxFfmpegOutput
	stderr.limit = maxFfmpegStderr

	cmd := exec.CommandContext(ctx, "ffmpeg",
		"-hide_banner", "-loglevel", "error",
		"-ss", fmtSeconds(startSeconds),
		"-i", path,
		"-t", fmtSeconds(durationSeconds),
		"-vf", vf,
		"-f", "rawvideo",
		"-pix_fmt", "rgb24",
		"pipe:1",
	)
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	if ctx.Err() == context.DeadlineExceeded {
		return nil, fmt.Errorf("ffmpeg timed out for %s", path)
	}
	if stdout.exceeded {
		return nil, fmt.Errorf("ffmpeg output too large for %s", path)
	}
	if err != nil {
		return nil, fmt.Errorf("ffmpeg failed (exit %d): %s", exitCode(cmd), truncate(stderr.String(), 400))
	}
	return stdout.Bytes(), nil
}

func exitCode(cmd *exec.Cmd) int {
	if cmd.ProcessState == nil {
		return -1
	}
	return cmd.ProcessState.ExitCode()
}

func detectHdrSupport() bool {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, "ffmpeg", "-hide_banner", "-filters").Output()
	if err != nil {
		return false
	}
	hasZscale, hasTonemap := false, false
	for _, line := range strings.Split(string(out), "\n") {
		trimmed := strings.TrimLeft(line, " \t")
		if strings.HasPrefix(trimmed, "zscale ") {
			hasZscale = true
		}
		if strings.HasPrefix(trimmed, "tonemap ") {
			hasTonemap = true
		}
	}
	return hasZscale && hasTonemap
}

func probeHdr(path string) bool {
	ctx, cancel := context.WithTimeout(context.Background(), probeTimeout)
	defer cancel()
	out, err := exec.CommandContext(ctx, "ffprobe",
		"-v", "error",
		"-select_streams", "v:0",
		"-show_entries", "stream=color_transfer",
		"-of", "json",
		path,
	).Output()
	if err != nil {
		return false
	}
	var result struct {
		Streams []struct {
			ColorTransfer string `json:"color_transfer"`
		} `json:"streams"`
	}
	if err := json.Unmarshal(out, &result); err != nil || len(result.Streams) == 0 {
		return false
	}
	t := result.Streams[0].ColorTransfer
	return t == "smpte2084" || t == "arib-std-b67"
}

func fmtSeconds(v float64) string {
	ms := int64(v * 1000)
	if ms < 0 {
		ms = 0
	}
	return fmt.Sprintf("%d.%03d", ms/1000, ms%1000)
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}
