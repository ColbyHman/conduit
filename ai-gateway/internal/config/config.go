package config

import (
	"fmt"
	"os"
	"time"

	"github.com/conduit/pkg/log"
	"gopkg.in/yaml.v3"
)

// Config holds all configuration for the AI Gateway
type Config struct {
	Server      ServerConfig      `yaml:"server"`
	Database    DatabaseConfig    `yaml:"database"`
	Logging     log.Config        `yaml:"logging"`
	Idempotency IdempotencyConfig `yaml:"idempotency"`
}

// ServerConfig holds HTTP server configuration
type ServerConfig struct {
	Port         int           `yaml:"port"`
	ReadTimeout  time.Duration `yaml:"read_timeout"`
	WriteTimeout time.Duration `yaml:"write_timeout"`
	IdleTimeout  time.Duration `yaml:"idle_timeout"`
}

// DatabaseConfig holds PostgreSQL connection pool configuration
type DatabaseConfig struct {
	URL             string        `yaml:"url"`
	MaxConns        int           `yaml:"max_conns"`
	MinConns        int           `yaml:"min_conns"`
	MaxConnLifetime time.Duration `yaml:"max_conn_lifetime"`
	MaxConnIdleTime time.Duration `yaml:"max_conn_idle_time"`
}

// IdempotencyConfig holds idempotency key configuration
type IdempotencyConfig struct {
	HeaderName   string `yaml:"header_name"`
	MaxKeyLength int    `yaml:"max_key_length"`
}

// Load loads configuration from file and environment variables
func Load(path string) (*Config, error) {
	cfg := &Config{}

	// Set defaults
	cfg.Server.Port = 8080
	cfg.Server.ReadTimeout = 30 * time.Second
	cfg.Server.WriteTimeout = 30 * time.Second
	cfg.Server.IdleTimeout = 120 * time.Second

	cfg.Database.MaxConns = 10
	cfg.Database.MinConns = 2
	cfg.Database.MaxConnLifetime = time.Hour
	cfg.Database.MaxConnIdleTime = 30 * time.Minute

	cfg.Logging = log.DefaultConfig()

	cfg.Idempotency.HeaderName = "Idempotency-Key"
	cfg.Idempotency.MaxKeyLength = 64

	// Load from YAML file if exists
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

	// Override from environment variables
	if v := os.Getenv("PORT"); v != "" {
		cfg.Server.Port = parseInt(v, cfg.Server.Port)
	}
	if v := os.Getenv("DATABASE_URL"); v != "" {
		cfg.Database.URL = v
	}
	if v := os.Getenv("LOG_LEVEL"); v != "" {
		cfg.Logging.Level = v
	}
	if v := os.Getenv("LOG_PRETTY"); v == "true" {
		cfg.Logging.Pretty = true
	}
	if v := os.Getenv("IDEMPOTENCY_HEADER"); v != "" {
		cfg.Idempotency.HeaderName = v
	}

	return cfg, nil
}

func parseInt(s string, fallback int) int {
	var n int
	_, err := fmt.Sscanf(s, "%d", &n)
	if err != nil {
		return fallback
	}
	return n
}