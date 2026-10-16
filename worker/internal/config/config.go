package config

import (
	"fmt"
	"os"
	"time"

	"github.com/conduit/pkg/log"
	"gopkg.in/yaml.v3"
)

// Config holds all configuration for the Worker
type Config struct {
	Worker    WorkerConfig    `yaml:"worker"`
	Database  DatabaseConfig  `yaml:"database"`
	LLM       LLMConfig       `yaml:"llm"`
	Retry     RetryConfig     `yaml:"retry"`
	Logging   log.Config      `yaml:"logging"`
	Prompts   PromptsConfig   `yaml:"prompts"`
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

// LLMConfig holds LLM provider configuration
type LLMConfig struct {
	Provider  string         `yaml:"provider"`
	Model     string         `yaml:"model"`
	APIKey    string         `yaml:"api_key"`
	Endpoint  string         `yaml:"endpoint"`
	Temperature float64      `yaml:"temperature"`
	MaxTokens int            `yaml:"max_tokens"`
	Timeout   time.Duration  `yaml:"timeout"`
	Ollama    OllamaConfig   `yaml:"ollama"`
}

// OllamaConfig holds Ollama-specific configuration
type OllamaConfig struct {
	Host string `yaml:"host"`
}

// RetryConfig holds retry policy configuration
type RetryConfig struct {
	MaxRetries int           `yaml:"max_retries"`
	BaseDelay  time.Duration `yaml:"base_delay"`
	MaxDelay   time.Duration `yaml:"max_delay"`
}

// PromptsConfig holds prompt template configuration
type PromptsConfig struct {
	Directory string `yaml:"directory"`
}

// Load loads configuration from file and environment variables
func Load(path string) (*Config, error) {
	cfg := &Config{}

	// Set defaults
	cfg.Worker.PollInterval = 5 * time.Second
	cfg.Worker.JobTypes = []string{"journal_analyze", "mtg_analyze"}
	cfg.Worker.ClaimLimit = 1
	cfg.Worker.StaleLockThreshold = 5 * time.Minute

	cfg.Database.MaxConns = 10
	cfg.Database.MinConns = 2
	cfg.Database.MaxConnLifetime = time.Hour
	cfg.Database.MaxConnIdleTime = 30 * time.Minute

	cfg.LLM.Provider = "openai"
	cfg.LLM.Model = "gpt-4o-mini"
	cfg.LLM.Temperature = 0.3
	cfg.LLM.MaxTokens = 2000
	cfg.LLM.Timeout = 60 * time.Second
	cfg.LLM.Ollama.Host = "http://host.docker.internal:11434"

	cfg.Retry.MaxRetries = 3
	cfg.Retry.BaseDelay = 30 * time.Second
	cfg.Retry.MaxDelay = 10 * time.Minute

	cfg.Logging = log.DefaultConfig()
	cfg.Prompts.Directory = "prompts"

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
	if v := os.Getenv("WORKER_ID"); v != "" {
		cfg.Worker.ID = v
	}
	if v := os.Getenv("DATABASE_URL"); v != "" {
		cfg.Database.URL = v
	}
	if v := os.Getenv("LLM_PROVIDER"); v != "" {
		cfg.LLM.Provider = v
	}
	if v := os.Getenv("LLM_MODEL"); v != "" {
		cfg.LLM.Model = v
	}
	if v := os.Getenv("LLM_API_KEY"); v != "" {
		cfg.LLM.APIKey = v
	}
	if v := os.Getenv("LLM_ENDPOINT"); v != "" {
		cfg.LLM.Endpoint = v
	}
	if v := os.Getenv("LLM_TEMPERATURE"); v != "" {
		cfg.LLM.Temperature = parseFloat(v, cfg.LLM.Temperature)
	}
	if v := os.Getenv("WORKER_POLL_INTERVAL"); v != "" {
		cfg.Worker.PollInterval = parseDuration(v, cfg.Worker.PollInterval)
	}
	if v := os.Getenv("LOG_LEVEL"); v != "" {
		cfg.Logging.Level = v
	}
	if v := os.Getenv("LOG_PRETTY"); v == "true" {
		cfg.Logging.Pretty = true
	}

	return cfg, nil
}

func parseFloat(s string, fallback float64) float64 {
	var f float64
	_, err := fmt.Sscanf(s, "%f", &f)
	if err != nil {
		return fallback
	}
	return f
}

func parseDuration(s string, fallback time.Duration) time.Duration {
	d, err := time.ParseDuration(s)
	if err != nil {
		return fallback
	}
	return d
}