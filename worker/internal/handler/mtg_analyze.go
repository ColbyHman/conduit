package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/conduit/pkg/log"
	"github.com/conduit/pkg/models"
	"github.com/conduit/worker/internal/llm"
	"github.com/conduit/worker/internal/store"
)

// MTGAnalyzeHandler handles mtg_analyze jobs
type MTGAnalyzeHandler struct {
	store     *store.JobStore
	prompts   *PromptLoader
	logger    *log.Logger
	maxTokens int
}

// NewMTGAnalyzeHandler creates a new MTG analyze handler
func NewMTGAnalyzeHandler(store *store.JobStore, prompts *PromptLoader, logger *log.Logger, maxTokens int) *MTGAnalyzeHandler {
	return &MTGAnalyzeHandler{
		store:     store,
		prompts:   prompts,
		logger:    logger.WithHandler("mtg_analyze"),
		maxTokens: maxTokens,
	}
}

// JobType returns the job type this handler processes
func (h *MTGAnalyzeHandler) JobType() models.JobType {
	return models.JobTypeMTGAnalyze
}

// Handle processes an mtg_analyze job
func (h *MTGAnalyzeHandler) Handle(ctx context.Context, job *models.Job, llmClient llm.LLMClient) (*models.JSONB, error) {
	start := time.Now()
	h.logger.LogJobEvent(ctx, models.EventProcessingStart, map[string]interface{}{
		"handler": h.JobType(),
	})

	// Parse payload
	var payload models.MTGAnalyzePayload
	if err := job.Payload.UnmarshalJSON(); err != nil {
		return nil, fmt.Errorf("unmarshal payload: %w", err)
	}

	// Render prompt (no dynamic data needed, but use template for consistency)
	prompt, err := h.prompts.Render("mtg_analysis", payload)
	if err != nil {
		return nil, fmt.Errorf("render prompt: %w", err)
	}

	// For image analysis, we need to include the image
	// Note: This assumes the LLM supports vision (gpt-4o, gpt-4v, etc.)
	// For non-vision models, this would need a different approach
	messages := []llm.Message{
		{Role: "system", Content: "You are an expert Magic: The Gathering card analyst. Respond only with valid JSON."},
		{Role: "user", Content: prompt},
	}

	// Add image if provided
	if payload.ImageURL != "" {
		// For vision models, we'd add image_url to the message
		// This is a simplified version - real implementation would use multimodal format
		messages[1].Content = prompt + "\n\nImage URL: " + payload.ImageURL
	} else if payload.ImageBase64 != "" {
		messages[1].Content = prompt + "\n\nImage (base64): " + payload.ImageBase64[:100] + "..."
	}

	// Call LLM
	h.logger.LogJobEvent(ctx, models.EventLLMRequest, map[string]interface{}{
		"model":         llmClient.Model(),
		"prompt_tokens": len(prompt) / 4,
	})

	llmResp, err := llmClient.Complete(ctx, llm.CompletionRequest{
		Messages:    messages,
		Temperature: 0.1, // Lower temperature for factual extraction
		MaxTokens:   h.maxTokens,
	})
	if err != nil {
		h.logger.LogJobEvent(ctx, models.EventLLMResponse, map[string]interface{}{
			"error":       err.Error(),
			"duration_ms": time.Since(start).Milliseconds(),
		})
		return nil, fmt.Errorf("LLM request failed: %w", err)
	}

	h.logger.LogJobEvent(ctx, models.EventLLMResponse, map[string]interface{}{
		"model":             llmResp.Model,
		"prompt_tokens":     llmResp.PromptTokens,
		"completion_tokens": llmResp.CompletionTokens,
		"duration_ms":       time.Since(start).Milliseconds(),
		"cached":            llmResp.Cached,
	})

	// Parse LLM response
	var result models.MTGAnalyzeResult
	if err := json.Unmarshal([]byte(llmResp.Content), &result); err != nil {
		// Try to extract JSON from response
		result, err = h.extractJSON(llmResp.Content)
		if err != nil {
			return nil, fmt.Errorf("parse LLM response: %w", err)
		}
	}

	// Create magic card in database
	card := &models.MagicCard{
		ImageURL:    &payload.ImageURL,
		ImagePath:   &payload.ImagePath,
		CardName:    &result.CardName,
		Analysis:    h.toJSONB(result.Analysis),
		RawResponse: &llmResp.Content,
		JobID:       &job.ID,
	}

	if err := h.store.CreateMagicCard(ctx, card); err != nil {
		return nil, fmt.Errorf("create magic card: %w", err)
	}

	h.logger.LogJobEvent(ctx, models.EventCompleted, map[string]interface{}{
		"handler":     h.JobType(),
		"duration_ms": time.Since(start).Milliseconds(),
		"card_id":     card.ID,
		"card_name":   result.CardName,
	})

	// Return result
	resultJSON, err := json.Marshal(result)
	if err != nil {
		return nil, fmt.Errorf("marshal result: %w", err)
	}

	jsonb := models.JSONB(resultJSON)
	return &jsonb, nil
}

// extractJSON attempts to extract JSON from a text response
func (h *MTGAnalyzeHandler) extractJSON(text string) (models.MTGAnalyzeResult, error) {
	var result models.MTGAnalyzeResult
	start := -1
	end := -1
	for i, c := range text {
		if c == '{' && start == -1 {
			start = i
		}
		if c == '}' {
			end = i
		}
	}
	if start == -1 || end == -1 || end <= start {
		return result, fmt.Errorf("no JSON object found in response")
	}
	jsonStr := text[start : end+1]
	err := json.Unmarshal([]byte(jsonStr), &result)
	return result, err
}

// toJSONB converts a struct to JSONB
func (h *MTGAnalyzeHandler) toJSONB(v interface{}) *models.JSONB {
	data, err := json.Marshal(v)
	if err != nil {
		return nil
	}
	jsonb := models.JSONB(data)
	return &jsonb
}