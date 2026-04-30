package config

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

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
	// Can be overridden at runtime with the SECURE_LINK_SECRET environment variable.
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
	Endpoint  string `yaml:"endpoint"`
	AccessKey string `yaml:"access_key"`
	SecretKey string `yaml:"secret_key"`
	Bucket    string `yaml:"bucket"`
	UseSSL    bool   `yaml:"use_ssl"`
	Region    string `yaml:"region"`
}

type HLSConfig struct {
	Profiles []HLSProfile `yaml:"profiles"`
}

type HLSProfile struct {
	Name           string `yaml:"name"`
	Width          int    `yaml:"width"`
	Height         int    `yaml:"height"`
	VideoBitrate   string `yaml:"video_bitrate"`
	AudioBitrate   string `yaml:"audio_bitrate"`
	SegmentSeconds int    `yaml:"segment_seconds"`
}

func Load(path string) (*Config, error) {
	bytes, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	var cfg Config
	if err := yaml.Unmarshal(bytes, &cfg); err != nil {
		return nil, err
	}

	if err := cfg.validateAndApplyDefaults(); err != nil {
		return nil, err
	}

	return &cfg, nil
}

func (c *Config) validateAndApplyDefaults() error {
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
		if p.Name == "" || p.Width <= 0 || p.Height <= 0 || p.VideoBitrate == "" || p.AudioBitrate == "" {
			return fmt.Errorf("invalid hls profile at index %d", i)
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
	// SECURE_LINK_SECRET environment variable so that docker-compose can
	// inject it without modifying the config file.
	if envSecret := os.Getenv("SECURE_LINK_SECRET"); envSecret != "" {
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
