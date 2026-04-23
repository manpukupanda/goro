package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadAppliesDefaults(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(configPath, []byte(`
s3:
  endpoint: minio:9000
  access_key: minio
  secret_key: minio123
  bucket: goro
hls:
  profiles:
    - name: 720p
      width: 1280
      height: 720
      video_bitrate: 2800k
      audio_bitrate: 128k
`), 0o644); err != nil {
		t.Fatalf("failed to write config: %v", err)
	}

	cfg, err := Load(configPath)
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}

	if cfg.S3.Region != "us-east-1" {
		t.Fatalf("expected default region us-east-1, got %s", cfg.S3.Region)
	}
	if got := cfg.HLS.Profiles[0].SegmentSeconds; got != 4 {
		t.Fatalf("expected default segment_seconds 4, got %d", got)
	}
	if cfg.Worker.Concurrency != 2 {
		t.Fatalf("expected default worker concurrency 2, got %d", cfg.Worker.Concurrency)
	}
}
