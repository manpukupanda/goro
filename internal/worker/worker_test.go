package worker

import (
	"path/filepath"
	"strings"
	"testing"

	"goro/internal/config"
)

func TestBuildFFmpegArgsIncludesProfileSettings(t *testing.T) {
	profileDir := filepath.Join(t.TempDir(), "720p")
	profile := config.HLSProfile{
		Name:           "720p",
		Width:          1280,
		Height:         720,
		VideoBitrate:   "2800k",
		AudioBitrate:   "128k",
		SegmentSeconds: 4,
	}

	args := buildFFmpegArgs("/tmp/input.mp4", profile, profileDir)
	joined := strings.Join(args, " ")

	for _, expected := range []string{
		"-i /tmp/input.mp4",
		"-vf scale=1280:720",
		"-b:v 2800k",
		"-b:a 128k",
		"-hls_time 4",
		filepath.Join(profileDir, "segment%03d.ts"),
		filepath.Join(profileDir, "index.m3u8"),
	} {
		if !strings.Contains(joined, expected) {
			t.Fatalf("expected ffmpeg args to contain %q, got %q", expected, joined)
		}
	}
}
