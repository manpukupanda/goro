package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadAppliesDefaults(t *testing.T) {
	t.Setenv("GORO_API_KEY", "test-key")
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
	if cfg.APIKey != "test-key" {
		t.Fatalf("expected APIKey to be set from env, got %q", cfg.APIKey)
	}
}

func TestLoadS3CredsFromEnv(t *testing.T) {
	t.Setenv("GORO_API_KEY", "test-key")
	t.Setenv("GORO_S3_ACCESS_KEY", "env-access-key")
	t.Setenv("GORO_S3_SECRET_KEY", "env-secret-key")
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.yaml")
	// Config file intentionally omits access_key and secret_key.
	if err := os.WriteFile(configPath, []byte(`
s3:
  endpoint: minio:9000
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

	if cfg.S3.AccessKey != "env-access-key" {
		t.Fatalf("expected AccessKey from env, got %q", cfg.S3.AccessKey)
	}
	if cfg.S3.SecretKey != "env-secret-key" {
		t.Fatalf("expected SecretKey from env, got %q", cfg.S3.SecretKey)
	}
}

func TestLoadS3EnvOverridesYAML(t *testing.T) {
	t.Setenv("GORO_API_KEY", "test-key")
	t.Setenv("GORO_S3_ACCESS_KEY", "override-access")
	t.Setenv("GORO_S3_SECRET_KEY", "override-secret")
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(configPath, []byte(`
s3:
  endpoint: minio:9000
  access_key: yaml-access
  secret_key: yaml-secret
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

	if cfg.S3.AccessKey != "override-access" {
		t.Fatalf("expected AccessKey overridden by env, got %q", cfg.S3.AccessKey)
	}
	if cfg.S3.SecretKey != "override-secret" {
		t.Fatalf("expected SecretKey overridden by env, got %q", cfg.S3.SecretKey)
	}
}


func TestLoadFailsWithoutAPIKey(t *testing.T) {
	t.Setenv("GORO_API_KEY", "")
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

	_, err := Load(configPath)
	if err == nil {
		t.Fatal("expected Load to fail when GORO_API_KEY is not set")
	}
}
