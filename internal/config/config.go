package config

import (
	_ "embed"
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

//go:embed default_config.yaml
var defaultConfigYAML []byte

const defaultSecureLinkTTLSec = 3600
const defaultPlaylistTokenTTLSec = 900

type Config struct {
	S3            S3Config            `yaml:"s3"`
	HLS           HLSConfig           `yaml:"hls"`
	Thumbnails    ThumbnailConfig     `yaml:"thumbnails"`
	Worker        WorkerConfig        `yaml:"worker"`
	SecureLink    SecureLinkConfig    `yaml:"secure_link"`
	PlaylistToken PlaylistTokenConfig `yaml:"playlist_token"`
	// APIKey is the static API key required to access the public API.
	// It must be supplied via the GORO_API_KEY environment variable.
	APIKey string `yaml:"-"`
}

// ThumbnailConfig holds the list of thumbnail specs to generate after HLS encoding.
type ThumbnailConfig struct {
	Specs []ThumbnailSpec `yaml:"specs"`
}

// ThumbnailSpec describes a single thumbnail to be generated.
// Type must be either "fixed_second" or "representative".
// For "fixed_second", DurationSec specifies the target time in seconds; 0 means
// auto (>= 5 s video → 5 s, shorter → duration/2).
type ThumbnailSpec struct {
	Name        string  `yaml:"name"`
	Type        string  `yaml:"type"`
	DurationSec float64 `yaml:"duration_sec"`
}

type SecureLinkConfig struct {
	// Secret is the shared key used to sign HLS segment URLs.
	// Can be overridden at runtime with the GORO_SECURE_LINK_SECRET environment variable.
	Secret string `yaml:"secret"`
	// TTLSec is how long (in seconds) a signed URL remains valid. Defaults to 3600.
	TTLSec int `yaml:"ttl_sec"`
}

// PlaylistTokenConfig controls the short-lived opaque tokens used to grant
// access to private video playlists.
type PlaylistTokenConfig struct {
	// TTLSec is how long (in seconds) a playlist token remains valid. Defaults to 900.
	TTLSec int `yaml:"ttl_sec"`
}

type WorkerConfig struct {
	// Concurrency controls how many worker goroutines run in parallel.
	// Keep this value low; ffmpeg is CPU-intensive and excessive parallelism
	// will saturate the host and degrade encoding performance.
	Concurrency int `yaml:"concurrency"`
}

type S3Config struct {
	Endpoint string `yaml:"endpoint"`
	// AccessKey can be overridden at runtime with the GORO_S3_ACCESS_KEY environment variable.
	AccessKey string `yaml:"access_key"`
	// SecretKey can be overridden at runtime with the GORO_S3_SECRET_KEY environment variable.
	SecretKey string `yaml:"secret_key"`
	Bucket    string `yaml:"bucket"`
	UseSSL    bool   `yaml:"use_ssl"`
	Region    string `yaml:"region"`
}

type HLSConfig struct {
	Profiles []HLSProfile `yaml:"profiles"`
}

type ProfileFormat string

const (
	ProfileFormatHLSTS    ProfileFormat = "hls_ts"
	ProfileFormatHLSFMP4  ProfileFormat = "hls_fmp4"
	ProfileFormatDASHFMP4 ProfileFormat = "dash_fmp4"
)

type HLSProfile struct {
	Name           string        `yaml:"name"`
	Format         ProfileFormat `yaml:"format"`
	Width          int           `yaml:"width"`
	Height         int           `yaml:"height"`
	VideoBitrate   string        `yaml:"video_bitrate"`
	AudioBitrate   string        `yaml:"audio_bitrate"`
	SegmentSeconds int           `yaml:"segment_seconds"`
}

func (f ProfileFormat) IsHLS() bool {
	return f == ProfileFormatHLSTS || f == ProfileFormatHLSFMP4
}

func (f ProfileFormat) IsDASH() bool {
	return f == ProfileFormatDASHFMP4
}

func (p HLSProfile) EffectiveFormat() ProfileFormat {
	if p.Format == "" {
		return ProfileFormatHLSFMP4
	}
	return p.Format
}

func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return LoadBytes(data)
}

// LoadDefault loads the configuration that was embedded into the binary at
// build time. Environment variables are applied on top, so all sensitive
// values (GORO_API_KEY, GORO_S3_ACCESS_KEY, etc.) can still be injected at
// runtime.
func LoadDefault() (*Config, error) {
	return LoadBytes(defaultConfigYAML)
}

// LoadBytes parses YAML configuration from the supplied byte slice and applies
// defaults and environment-variable overrides.
func LoadBytes(data []byte) (*Config, error) {
	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, err
	}

	if err := cfg.validateAndApplyDefaults(); err != nil {
		return nil, err
	}

	return &cfg, nil
}

func (c *Config) validateAndApplyDefaults() error {
	// Allow S3 credentials to be supplied (or overridden) via environment variables
	// so that the config file can remain free of secrets.
	if v := os.Getenv("GORO_S3_ACCESS_KEY"); v != "" {
		c.S3.AccessKey = v
	}
	if v := os.Getenv("GORO_S3_SECRET_KEY"); v != "" {
		c.S3.SecretKey = v
	}

	if c.S3.Endpoint == "" || c.S3.AccessKey == "" || c.S3.SecretKey == "" || c.S3.Bucket == "" {
		return fmt.Errorf("s3 config is incomplete")
	}
	if c.S3.Region == "" {
		c.S3.Region = "us-east-1"
	}

	if len(c.HLS.Profiles) == 0 {
		return fmt.Errorf("at least one hls profile is required")
	}

	for i := range c.HLS.Profiles {
		p := &c.HLS.Profiles[i]
		if p.Format == "" {
			p.Format = ProfileFormatHLSFMP4
		}
		if p.Name == "" || p.Width <= 0 || p.Height <= 0 || p.VideoBitrate == "" || p.AudioBitrate == "" {
			return fmt.Errorf("invalid hls profile at index %d", i)
		}
		if p.Format != ProfileFormatHLSTS && p.Format != ProfileFormatHLSFMP4 && p.Format != ProfileFormatDASHFMP4 {
			return fmt.Errorf("invalid hls profile format %q at index %d", p.Format, i)
		}
		if p.SegmentSeconds <= 0 {
			p.SegmentSeconds = 4
		}
	}

	if len(c.Thumbnails.Specs) == 0 {
		c.Thumbnails.Specs = []ThumbnailSpec{
			{Name: "fixed_5s", Type: "fixed_second"},
			{Name: "representative", Type: "representative"},
		}
	}
	for i, spec := range c.Thumbnails.Specs {
		if spec.Name == "" {
			return fmt.Errorf("thumbnail spec at index %d has no name", i)
		}
		if spec.Type != "fixed_second" && spec.Type != "representative" {
			return fmt.Errorf("thumbnail spec %q has invalid type %q (must be fixed_second or representative)", spec.Name, spec.Type)
		}
		if spec.DurationSec < 0 {
			return fmt.Errorf("thumbnail spec %q has negative duration_sec", spec.Name)
		}
	}

	if c.Worker.Concurrency <= 0 {
		c.Worker.Concurrency = 2
	}

	// Allow the secure-link secret to be supplied (or overridden) via the
	// GORO_SECURE_LINK_SECRET environment variable so that docker-compose can
	// inject it without modifying the config file.
	if envSecret := os.Getenv("GORO_SECURE_LINK_SECRET"); envSecret != "" {
		c.SecureLink.Secret = envSecret
	}
	if c.SecureLink.TTLSec <= 0 {
		c.SecureLink.TTLSec = defaultSecureLinkTTLSec
	}

	if c.PlaylistToken.TTLSec <= 0 {
		c.PlaylistToken.TTLSec = defaultPlaylistTokenTTLSec
	}

	// GORO_API_KEY must be set; the server refuses to start without it.
	c.APIKey = os.Getenv("GORO_API_KEY")
	if c.APIKey == "" {
		return fmt.Errorf("GORO_API_KEY environment variable must be set")
	}

	return nil
}
