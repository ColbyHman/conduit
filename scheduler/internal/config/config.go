package config

import (
	"os"
	"time"

	"github.com/conduit/pkg/log"
	"gopkg.in/yaml.v3"
)

// Config holds all configuration for the Scheduler (ADR 0007)
type Config struct {
	Scheduler SchedulerConfig `yaml:"scheduler"`
	Database  DatabaseConfig  `yaml:"database"`
	Logging   log.Config      `yaml:"logging"`
}

// SchedulerConfig holds scheduler-specific configuration
type SchedulerConfig struct {
	PollInterval time.Duration `yaml:"poll_interval"`
}

// DatabaseConfig holds PostgreSQL connection pool configuration
type DatabaseConfig struct {
	URL             string        `yaml:"url"`
	MaxConns        int           `yaml:"max_conns"`
	MinConns        int           `yaml:"min_conns"`
	MaxConnLifetime time.Duration `yaml:"max_conn_lifetime"`
	MaxConnIdleTime time.Duration `yaml:"max_conn_idle_time"`
}

// Load loads configuration from file and environment variables
func Load(path string) (*Config, error) {
	cfg := &Config{}

	// Defaults
	cfg.Scheduler.PollInterval = 30 * time.Second
	cfg.Database.MaxConns = 5
	cfg.Database.MinConns = 1
	cfg.Database.MaxConnLifetime = time.Hour
	cfg.Database.MaxConnIdleTime = 30 * time.Minute
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

	if v := os.Getenv("DATABASE_URL"); v != "" {
		cfg.Database.URL = v
	}
	if v := os.Getenv("SCHEDULER_POLL_INTERVAL"); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			cfg.Scheduler.PollInterval = d
		}
	}
	if v := os.Getenv("LOG_LEVEL"); v != "" {
		cfg.Logging.Level = v
	}
	if v := os.Getenv("LOG_PRETTY"); v == "true" {
		cfg.Logging.Pretty = true
	}

	return cfg, nil
}
