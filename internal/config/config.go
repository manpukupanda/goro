package config

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

type Config struct {
	S3  S3Config  `yaml:"s3"`
	HLS HLSConfig `yaml:"hls"`
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

	return nil
}
