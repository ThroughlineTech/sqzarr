package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/BurntSushi/toml"
)

type Config struct {
	Server     ServerConfig     `toml:"server"`
	Scanner    ScannerConfig    `toml:"scanner"`
	Transcoder TranscoderConfig `toml:"transcoder"`
	Safety     SafetyConfig     `toml:"safety"`
	Plex       PlexConfig       `toml:"plex"`
	Auth       AuthConfig       `toml:"auth"`
}

type ServerConfig struct {
	Host    string `toml:"host"`
	Port    int    `toml:"port"`
	DataDir string `toml:"data_dir"`
}

type ScannerConfig struct {
	IntervalHours     int `toml:"interval_hours"`
	WorkerConcurrency int `toml:"worker_concurrency"`
	// RootDirs are the filesystem roots the browser and directory picker are
	// allowed to access. Paths outside these roots are rejected.
	RootDirs []string `toml:"root_dirs"`
	// Paused persists the queue pause state across restarts.
	Paused bool `toml:"paused"`
}

type TranscoderConfig struct {
	TempDir string `toml:"temp_dir"`
	// Encoder selects the preferred encoder: "vaapi", "videotoolbox", "nvenc",
	// "software", or "" (empty = auto-detect best available).
	Encoder string `toml:"encoder"`
	// HardwareDecode enables qualified VAAPI decoding ("auto") or software decoding ("off").
	HardwareDecode string `toml:"hardware_decode"`
	// LowPower permits only qualified bitrate-controlled encoding, never a CQP substitution.
	LowPower string `toml:"low_power"`
}

type SafetyConfig struct {
	// ProcessedDirName is the name of the subdirectory within each root directory
	// where original files are moved after successful transcoding.
	// Default: ".processed"
	ProcessedDirName string `toml:"processed_dir_name"`

	// OriginalsRetentionDays is how long originals are kept before the GC
	// automatically deletes them. Default: 10.
	OriginalsRetentionDays int `toml:"originals_retention_days"`

	// FailThreshold is the number of transcode failures for a single file
	// before it is automatically excluded. Default: 1.
	FailThreshold int `toml:"fail_threshold"`

	// SystemFailThreshold is the number of consecutive system-wide failures
	// before the worker is auto-paused. Default: 5.
	SystemFailThreshold int `toml:"system_fail_threshold"`

	// DeleteConfirmSingle controls whether the UI asks for confirmation before
	// deleting a single original. Bulk deletes always confirm. Default: false.
	DeleteConfirmSingle bool `toml:"delete_confirm_single"`
}

type PlexConfig struct {
	Enabled bool   `toml:"enabled"`
	BaseURL string `toml:"base_url"`
	Token   string `toml:"token"`
}

type AuthConfig struct {
	PasswordHash string `toml:"password_hash"`
	JWTSecret    string `toml:"jwt_secret"`
}

// Defaults returns a Config with safe default values.
func Defaults() *Config {
	return &Config{
		Server: ServerConfig{
			Host:    "127.0.0.1",
			Port:    8080,
			DataDir: "/var/lib/sqzarr",
		},
		Scanner: ScannerConfig{
			IntervalHours:     6,
			WorkerConcurrency: 1,
		},
		Transcoder: TranscoderConfig{
			HardwareDecode: "auto",
			LowPower:       "auto",
		},
		Safety: SafetyConfig{
			ProcessedDirName:       ".processed",
			OriginalsRetentionDays: 10,
			FailThreshold:          1,
			SystemFailThreshold:    5,
			DeleteConfirmSingle:    false,
		},
	}
}

// Load reads and validates a TOML config file, applying defaults for missing fields.
func Load(path string) (*Config, error) {
	cfg := Defaults()

	if _, err := os.Stat(path); errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("config file not found: %s", path)
	}

	if _, err := toml.DecodeFile(path, cfg); err != nil {
		return nil, fmt.Errorf("parse config: %w", err)
	}

	return cfg, cfg.validate()
}

func (c *Config) validate() error {
	if err := ValidateTranscodePolicy(c.Transcoder.HardwareDecode); err != nil {
		return fmt.Errorf("transcoder.hardware_decode: %w", err)
	}
	if err := ValidateTranscodePolicy(c.Transcoder.LowPower); err != nil {
		return fmt.Errorf("transcoder.low_power: %w", err)
	}
	if c.Server.Port <= 0 || c.Server.Port > 65535 {
		return fmt.Errorf("server.port must be between 1 and 65535")
	}
	if c.Server.DataDir == "" {
		return fmt.Errorf("server.data_dir must not be empty")
	}
	if c.Scanner.WorkerConcurrency < 1 || c.Scanner.WorkerConcurrency > 8 {
		return fmt.Errorf("scanner.worker_concurrency must be between 1 and 8")
	}
	if c.Scanner.IntervalHours < 1 {
		return fmt.Errorf("scanner.interval_hours must be at least 1")
	}
	if c.Plex.Enabled && c.Plex.BaseURL == "" {
		return fmt.Errorf("plex.base_url is required when plex.enabled is true")
	}
	if c.Plex.Enabled && c.Plex.Token == "" {
		return fmt.Errorf("plex.token is required when plex.enabled is true")
	}
	return nil
}

// ValidateTranscodePolicy rejects unknown policies rather than silently changing behavior.
func ValidateTranscodePolicy(value string) error {
	if value != "auto" && value != "off" {
		return fmt.Errorf("must be auto or off")
	}
	return nil
}

// ProcessedDirFor returns the processed directory path for a given root directory.
// Originals are held at <rootDir>/<ProcessedDirName>/<relative_path>.
func (c *Config) ProcessedDirFor(rootDir string) string {
	name := c.Safety.ProcessedDirName
	if name == "" {
		name = ".processed"
	}
	return filepath.Join(rootDir, name)
}

// DBPath returns the path to the SQLite database file.
func (c *Config) DBPath() string {
	return filepath.Join(c.Server.DataDir, "sqzarr.db")
}

// Addr returns the host:port listen address.
func (c *Config) Addr() string {
	return fmt.Sprintf("%s:%d", c.Server.Host, c.Server.Port)
}
