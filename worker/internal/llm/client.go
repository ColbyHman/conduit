package llm

import (
	"context"
	"errors"
)

// CompletionRequest represents a request to the LLM
type CompletionRequest struct {
	Messages    []Message `json:"messages"`
	Temperature float64   `json:"temperature,omitempty"`
	MaxTokens   int       `json:"max_tokens,omitempty"`
	Model       string    `json:"model,omitempty"`
	Stream      bool      `json:"stream,omitempty"`
}

// Message represents a chat message
type Message struct {
	Role    string `json:"role"`    // system, user, assistant
	Content string `json:"content"`
}

// CompletionResponse represents a response from the LLM
type CompletionResponse struct {
	Content      string         `json:"content"`
	PromptTokens int            `json:"prompt_tokens"`
	CompletionTokens int        `json:"completion_tokens"`
	TotalTokens  int            `json:"total_tokens"`
	Model        string         `json:"model"`
	FinishReason string         `json:"finish_reason"`
	Cached       bool           `json:"cached,omitempty"`
}

// LLMClient defines the interface for LLM providers
type LLMClient interface {
	Complete(ctx context.Context, req CompletionRequest) (*CompletionResponse, error)
	CountTokens(text string) (int, error)
	Model() string
	Close() error
}

// Registry holds registered LLM clients
type Registry map[string]func(config ClientConfig) (LLMClient, error)

var registry = make(Registry)

// Register registers an LLM client factory
func Register(name string, factory func(config ClientConfig) (LLMClient, error)) {
	registry[name] = factory
}

// Create creates an LLM client from config
func Create(config ClientConfig) (LLMClient, error) {
	factory, ok := registry[config.Provider]
	if !ok {
		return nil, ErrUnknownProvider
	}
	return factory(config)
}

// ClientConfig holds configuration for creating an LLM client
type ClientConfig struct {
	Provider   string
	Model      string
	APIKey     string
	Endpoint   string
	Temperature float64
	MaxTokens   int
	Timeout    int64 // seconds
	OllamaHost string
}

// ErrUnknownProvider is returned when provider is not registered
var ErrUnknownProvider = errors.New("unknown LLM provider")