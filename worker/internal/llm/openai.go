package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"
)

// OpenAIClient implements LLMClient for OpenAI-compatible APIs
type OpenAIClient struct {
	config     ClientConfig
	httpClient *http.Client
	baseURL    string
}

// NewOpenAIClient creates a new OpenAI-compatible client
func NewOpenAIClient(config ClientConfig) (LLMClient, error) {
	if config.APIKey == "" && config.Provider != "ollama" {
		return nil, errors.New("API key required for OpenAI-compatible provider")
	}

	baseURL := config.Endpoint
	if baseURL == "" {
		switch config.Provider {
		case "openai":
			baseURL = "https://api.openai.com/v1"
		case "anthropic":
			baseURL = "https://api.anthropic.com/v1"
		case "openai-compatible":
			return nil, errors.New("endpoint required for openai-compatible provider")
		default:
			baseURL = "https://api.openai.com/v1"
		}
	}

	client := &OpenAIClient{
		config: config,
		baseURL: baseURL,
		httpClient: &http.Client{
			Timeout: time.Duration(config.Timeout) * time.Second,
		},
	}

	return client, nil
}

// Complete sends a completion request
func (c *OpenAIClient) Complete(ctx context.Context, req CompletionRequest) (*CompletionResponse, error) {
	// Use config defaults if not specified in request
	if req.Model == "" {
		req.Model = c.config.Model
	}
	if req.Temperature == 0 {
		req.Temperature = c.config.Temperature
	}
	if req.MaxTokens == 0 {
		req.MaxTokens = c.config.MaxTokens
	}

	// Build request body
	body := map[string]interface{}{
		"model":       req.Model,
		"messages":    req.Messages,
		"temperature": req.Temperature,
		"max_tokens":  req.MaxTokens,
		"stream":      req.Stream,
	}

	jsonBody, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	// Create HTTP request
	url := c.baseURL + "/chat/completions"
	httpReq, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(jsonBody))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}

	// Set headers
	httpReq.Header.Set("Content-Type", "application/json")
	if c.config.APIKey != "" {
		httpReq.Header.Set("Authorization", "Bearer "+c.config.APIKey)
	}

	// Send request
	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("do request: %w", err)
	}
	defer resp.Body.Close()

	// Read response
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("API error %d: %s", resp.StatusCode, string(respBody))
	}

	// Parse response
	var result struct {
		ID      string `json:"id"`
		Object  string `json:"object"`
		Created int64  `json:"created"`
		Model   string `json:"model"`
		Choices []struct {
			Index        int `json:"index"`
			Message      struct {
				Role    string `json:"role"`
				Content string `json:"content"`
			} `json:"message"`
			FinishReason string `json:"finish_reason"`
		} `json:"choices"`
		Usage struct {
			PromptTokens     int `json:"prompt_tokens"`
			CompletionTokens int `json:"completion_tokens"`
			TotalTokens      int `json:"total_tokens"`
		} `json:"usage"`
	}

	if err := json.Unmarshal(respBody, &result); err != nil {
		return nil, fmt.Errorf("unmarshal response: %w", err)
	}

	if len(result.Choices) == 0 {
		return nil, errors.New("no choices in response")
	}

	return &CompletionResponse{
		Content:           result.Choices[0].Message.Content,
		PromptTokens:      result.Usage.PromptTokens,
		CompletionTokens:  result.Usage.CompletionTokens,
		TotalTokens:       result.Usage.TotalTokens,
		Model:             result.Model,
		FinishReason:      result.Choices[0].FinishReason,
	}, nil
}

// CountTokens estimates token count (rough approximation)
func (c *OpenAIClient) CountTokens(text string) (int, error) {
	// Rough approximation: ~4 characters per token for English
	return len(text) / 4, nil
}

// Model returns the model name
func (c *OpenAIClient) Model() string {
	return c.config.Model
}

// Close closes the client
func (c *OpenAIClient) Close() error {
	return nil
}

func init() {
	Register("openai", NewOpenAIClient)
	Register("anthropic", NewOpenAIClient) // Uses same API shape
	Register("openai-compatible", NewOpenAIClient)
}