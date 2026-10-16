package config

import (
	"fmt"
	"os"
	"time"

	"github.com/conduit/pkg/log"
	"gopkg.in/yaml.v3"
)

// Config holds all configuration for the Whisper Worker
type Config struct {
	Worker   WorkerConfig   `yaml:"worker"`
	Database DatabaseConfig `yaml:"database"`
	Whisper  WhisperConfig  `yaml:"whisper"`
	Retry    RetryConfig    `yaml:"retry"`
	Logging  log.Config     `yaml:"logging"`
}

// WorkerConfig holds worker-specific configuration
type WorkerConfig struct {
	ID                 string        `yaml:"id"`
	PollInterval       time.Duration `yaml:"poll_interval"`
	JobTypes           []string      `yaml:"job_types"`
	ClaimLimit         int           `yaml:"claim_limit"`
	StaleLockThreshold time.Duration `yaml:"stale_lock_threshold"`
}

// DatabaseConfig holds PostgreSQL connection pool configuration
type DatabaseConfig struct {
	URL             string        `yaml:"url"`
	MaxConns        int           `yaml:"max_conns"`
	MinConns        int           `yaml:"min_conns"`
	MaxConnLifetime time.Duration `yaml:"max_conn_lifetime"`
	MaxConnIdleTime time.Duration `yaml:"max_conn_idle_time"`
}

// WhisperConfig holds Whisper API configuration
type WhisperConfig struct {
	URL                     string        `yaml:"url"`
	Timeout                 time.Duration `yaml:"timeout"`
	DefaultModel            string        `yaml:"default_model"`
	DefaultLanguage         string        `yaml:"default_language"`
	DefaultResponseFormat   string        `yaml:"default_response_format"`
}

// RetryConfig holds retry policy configuration
type RetryConfig struct {
	MaxRetries int           `yaml:"max_retries"`
	BaseDelay  time.Duration `yaml:"base_delay"`
	MaxDelay   time.Duration `yaml:"max_delay"`
}

// Load loads configuration from file and environment variables
func Load(path string) (*Config, error) {
	cfg := &Config{}

	cfg.Worker.PollInterval = 5 * time.Second
	cfg.Worker.JobTypes = []string{"journal_transcribe"}
	cfg.Worker.ClaimLimit = 1
	cfg.Worker.StaleLockThreshold = 5 * time.Minute

	cfg.Database.MaxConns = 10
	cfg.Database.MinConns = 2
	cfg.Database.MaxConnLifetime = time.Hour
	cfg.Database.MaxConnIdleTime = 30 * time.Minute

	cfg.Whisper.URL = "http://host.docker.internal:9000"
	cfg.Whisper.Timeout = 300 * time.Second
	cfg.Whisper.DefaultModel = "medium"
	cfg.Whisper.DefaultLanguage = ""
	cfg.Whisper.DefaultResponseFormat = "verbose_json"

	cfg.Retry.MaxRetries = 3
	cfg.Retry.BaseDelay = 30 * time.Second
	cfg.Retry.MaxDelay = 10 * time.Minute

	cfg.Logging = log.DefaultConfig()

	if path != "" {
		data, err := os.ReadFile(path)
		if err != nil && !os.IsNotExist(err) {
			return nil, err
		}
		if err == nil {
			if err := yaml.Unmarshal(data, cfg); err != nil {
				return nil, err
			}
		}
	}

	if v := os.Getenv("WORKER_ID"); v != "" {
		cfg.Worker.ID = v
	}
	if v := os.Getenv("DATABASE_URL"); v != "" {
		cfg.Database.URL = v
	}
	if v := os.Getenv("WHISPER_URL"); v != "" {
		cfg.Whisper.URL = v
	}
	if v := os.Getenv("WHISPER_TIMEOUT"); v != "" {
		cfg.Whisper.Timeout = parseDuration(v, cfg.Whisper.Timeout)
	}
	if v := os.Getenv("WHISPER_MODEL"); v != "" {
		cfg.Whisper.DefaultModel = v
	}
	if v := os.Getenv("LOG_LEVEL"); v != "" {
		cfg.Logging.Level = v
	}
	if v := os.Getenv("LOG_PRETTY"); v == "true" {
		cfg.Logging.Pretty = true
	}

	return cfg, nil
}

func parseDuration(s string, fallback time.Duration) time.Duration {
	d, err := time.ParseDuration(s)
	if err != nil {
		return fallback
	}
	return d
}