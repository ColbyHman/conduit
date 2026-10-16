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

// OllamaClient implements LLMClient for Ollama
type OllamaClient struct {
	config     ClientConfig
	httpClient *http.Client
	baseURL    string
}

// NewOllamaClient creates a new Ollama client
func NewOllamaClient(config ClientConfig) (LLMClient, error) {
	baseURL := config.OllamaHost
	if baseURL == "" {
		baseURL = "http://host.docker.internal:11434"
	}

	client := &OllamaClient{
		config: config,
		baseURL: baseURL,
		httpClient: &http.Client{
			Timeout: time.Duration(config.Timeout) * time.Second,
		},
	}

	return client, nil
}

// Complete sends a completion request to Ollama
func (c *OllamaClient) Complete(ctx context.Context, req CompletionRequest) (*CompletionResponse, error) {
	if req.Model == "" {
		req.Model = c.config.Model
	}
	if req.Temperature == 0 {
		req.Temperature = c.config.Temperature
	}

	// Ollama uses a different API format
	body := map[string]interface{}{
		"model":    req.Model,
		"messages": req.Messages,
		"options": map[string]interface{}{
			"temperature": req.Temperature,
			"num_predict": req.MaxTokens,
		},
		"stream": false,
	}

	jsonBody, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	url := c.baseURL + "/api/chat"
	httpReq, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(jsonBody))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}

	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("do request: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("Ollama error %d: %s", resp.StatusCode, string(respBody))
	}

	var result struct {
		Model              string `json:"model"`
		CreatedAt          string `json:"created_at"`
		Message            struct {
			Role    string `json:"role"`
			Content string `json:"content"`
		} `json:"message"`
		DoneReason string `json:"done_reason"`
		Done       bool   `json:"done"`
		PromptEvalCount    int `json:"prompt_eval_count"`
		EvalCount          int `json:"eval_count"`
	}

	if err := json.Unmarshal(respBody, &result); err != nil {
		return nil, fmt.Errorf("unmarshal response: %w", err)
	}

	return &CompletionResponse{
		Content:           result.Message.Content,
		PromptTokens:      result.PromptEvalCount,
		CompletionTokens:  result.EvalCount,
		TotalTokens:       result.PromptEvalCount + result.EvalCount,
		Model:             result.Model,
		FinishReason:      result.DoneReason,
	}, nil
}

// CountTokens estimates token count (rough approximation)
func (c *OllamaClient) CountTokens(text string) (int, error) {
	// Rough approximation
	return len(text) / 4, nil
}

// Model returns the model name
func (c *OllamaClient) Model() string {
	return c.config.Model
}

// Close closes the client
func (c *OllamaClient) Close() error {
	return nil
}

func init() {
	Register("ollama", NewOllamaClient)
}