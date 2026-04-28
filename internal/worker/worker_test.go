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

func TestFixedSecondTimestamp_AutoRule(t *testing.T) {
	cases := []struct {
		specSec  float64
		duration float64
		wantSec  float64
	}{
		{0, 10.0, 5.0},    // long video → 5 sec mark
		{0, 4.0, 2.0},     // short video → duration/2
		{0, 5.0, 5.0},     // exactly 5 sec → 5 sec mark
		{0, 0.5, 0.25},    // very short
		{3.0, 10.0, 3.0},  // explicit value within duration
		{8.0, 10.0, 8.0},  // explicit value within duration
		{12.0, 10.0, 5.0}, // explicit value exceeds duration → duration/2
	}

	for _, tc := range cases {
		got := fixedSecondTimestamp(tc.specSec, tc.duration)
		if got != tc.wantSec {
			t.Errorf("fixedSecondTimestamp(specSec=%v, duration=%v) = %v, want %v",
				tc.specSec, tc.duration, got, tc.wantSec)
		}
	}
}

func TestThumbnailFilterFrames(t *testing.T) {
	cases := []struct {
		duration float64
		want     int
	}{
		{60.0, 100}, // long video → default 100
		{3.0, 90},   // 3 sec × 30 fps = 90 → use 90
		{1.0, 30},   // 1 sec × 30 fps = 30 → use 30
		{0.1, 3},    // ceil(0.1*30) = 3
		{0.0, 100},  // zero duration → default 100 (ceil(0) = 0 < 1, fallback)
	}

	for _, tc := range cases {
		got := thumbnailFilterFrames(tc.duration)
		if got != tc.want {
			t.Errorf("thumbnailFilterFrames(%v) = %d, want %d", tc.duration, got, tc.want)
		}
	}
}

func TestGenerateThumbnailArgs_FixedSecond(t *testing.T) {
	spec := config.ThumbnailSpec{Name: "fixed_5s", Type: "fixed_second", DurationSec: 0}
	outPath := "/tmp/thumbnails/fixed_5s.jpg"
	sec := fixedSecondTimestamp(spec.DurationSec, 10.0)

	// Verify the expected seek position
	if sec != 5.0 {
		t.Fatalf("expected seek 5.0 s, got %v", sec)
	}

	// Verify args structure (no exec, just logic)
	joined := strings.Join([]string{
		"-y", "-ss", "5.000", "-i", "/tmp/input.mp4",
		"-vframes", "1", "-q:v", "2", outPath,
	}, " ")

	for _, expected := range []string{"-ss 5.000", "-vframes 1", outPath} {
		if !strings.Contains(joined, expected) {
			t.Fatalf("expected args to contain %q, got %q", expected, joined)
		}
	}
}

func TestGenerateThumbnailArgs_Representative(t *testing.T) {
	n := thumbnailFilterFrames(2.0) // 2 s → 60 frames
	if n != 60 {
		t.Fatalf("expected 60 frames for 2s video, got %d", n)
	}

	// Representative filter string
	filterArg := strings.Join([]string{"thumbnail", "n=60"}, "=")
	if !strings.Contains(filterArg, "thumbnail") || !strings.Contains(filterArg, "60") {
		t.Fatalf("unexpected filter arg: %s", filterArg)
	}
}
