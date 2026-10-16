package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"text/template"
	"time"

	"github.com/conduit/pkg/log"
	"github.com/conduit/pkg/models"
	"github.com/conduit/worker/internal/llm"
	"github.com/conduit/worker/internal/store"
)

// JobHandler defines the interface for job handlers
type JobHandler interface {
	JobType() models.JobType
	Handle(ctx context.Context, job *models.Job, llmClient llm.LLMClient) (*models.JSONB, error)
}

// HandlerRegistry holds registered job handlers
type HandlerRegistry map[models.JobType]JobHandler

var registry = make(HandlerRegistry)

// Register registers a job handler
func Register(handler JobHandler) {
	registry[handler.JobType()] = handler
}

// Get retrieves a handler for a job type
func Get(jobType models.JobType) (JobHandler, bool) {
	h, ok := registry[jobType]
	return h, ok
}

// PromptLoader loads and renders prompt templates
type PromptLoader struct {
	directory string
	templates map[string]*template.Template
	logger    *log.Logger
}

// NewPromptLoader creates a new prompt loader
func NewPromptLoader(directory string, logger *log.Logger) (*PromptLoader, error) {
	pl := &PromptLoader{
		directory: directory,
		templates: make(map[string]*template.Template),
		logger:    logger.WithHandler("prompt_loader"),
	}

	files, err := os.ReadDir(directory)
	if err != nil {
		return nil, fmt.Errorf("read prompt directory: %w", err)
	}

	for _, file := range files {
		if filepath.Ext(file.Name()) == ".txt" {
			name := strings.TrimSuffix(file.Name(), ".txt")
			path := filepath.Join(directory, file.Name())
			content, err := os.ReadFile(path)
			if err != nil {
				return nil, fmt.Errorf("read prompt %s: %w", name, err)
			}
			tmpl, err := template.New(name).Parse(string(content))
			if err != nil {
				return nil, fmt.Errorf("parse prompt %s: %w", name, err)
			}
			pl.templates[name] = tmpl
			pl.logger.Debug().Str("prompt", name).Msg("loaded prompt template")
		}
	}

	return pl, nil
}

// Render renders a prompt template with data
func (pl *PromptLoader) Render(name string, data interface{}) (string, error) {
	tmpl, ok := pl.templates[name]
	if !ok {
		return "", fmt.Errorf("prompt not found: %s", name)
	}

	var buf strings.Builder
	if err := tmpl.Execute(&buf, data); err != nil {
		return "", fmt.Errorf("render prompt %s: %w", name, err)
	}

	return buf.String(), nil
}

// JournalAnalyzeHandler handles journal_analyze jobs
type JournalAnalyzeHandler struct {
	store       *store.JobStore
	prompts     *PromptLoader
	logger      *log.Logger
	maxTokens   int
}

// NewJournalAnalyzeHandler creates a new journal analyze handler
func NewJournalAnalyzeHandler(store *store.JobStore, prompts *PromptLoader, logger *log.Logger, maxTokens int) *JournalAnalyzeHandler {
	return &JournalAnalyzeHandler{
		store:     store,
		prompts:   prompts,
		logger:    logger.WithHandler("journal_analyze"),
		maxTokens: maxTokens,
	}
}

// JobType returns the job type this handler processes
func (h *JournalAnalyzeHandler) JobType() models.JobType {
	return models.JobTypeJournalAnalyze
}

// Handle processes a journal_analyze job
func (h *JournalAnalyzeHandler) Handle(ctx context.Context, job *models.Job, llmClient llm.LLMClient) (*models.JSONB, error) {
	start := time.Now()
	h.logger.LogJobEvent(ctx, models.EventProcessingStart, map[string]interface{}{
		"handler": h.JobType(),
	})

	// Parse payload
	var payload models.JournalAnalyzePayload
	if err := job.Payload.UnmarshalJSON(); err != nil {
		return nil, fmt.Errorf("unmarshal payload: %w", err)
	}

	// Prepare prompt data
	promptData := struct {
		Content   string
		Segments  []models.Segment
		Language  string
		Duration  float64
		EntryType string
	}{
		Content:   payload.TranscriptionText,
		Segments:  payload.Segments,
		Language:  payload.Language,
		Duration:  payload.Duration,
		EntryType: payload.EntryType,
	}

	// Render prompt
	prompt, err := h.prompts.Render("journal_summary", promptData)
	if err != nil {
		return nil, fmt.Errorf("render prompt: %w", err)
	}

	// Call LLM
	h.logger.LogJobEvent(ctx, models.EventLLMRequest, map[string]interface{}{
		"model":         llmClient.Model(),
		"prompt_tokens": len(prompt) / 4,
	})

	llmResp, err := llmClient.Complete(ctx, llm.CompletionRequest{
		Messages: []llm.Message{
			{Role: "system", Content: "You are an expert journal analyst. Respond only with valid JSON."},
			{Role: "user", Content: prompt},
		},
		Temperature: 0.3,
		MaxTokens:   h.maxTokens,
	})
	if err != nil {
		h.logger.LogJobEvent(ctx, models.EventLLMResponse, map[string]interface{}{
			"error":      err.Error(),
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
	var result models.JournalAnalyzeResult
	if err := json.Unmarshal([]byte(llmResp.Content), &result); err != nil {
		// Try to extract JSON from response
		result, err = h.extractJSON(llmResp.Content)
		if err != nil {
			return nil, fmt.Errorf("parse LLM response: %w", err)
		}
	}

	// Set entry type from payload if not in result
	if result.EntryType == "" {
		result.EntryType = payload.EntryType
		if result.EntryType == "" {
			result.EntryType = "journal"
		}
	}

	// Create entry in database
	entry := &models.Entry{
		EntryType:       result.EntryType,
		Title:           &result.Summary,
		Content:         payload.TranscriptionText,
		Summary:         &result.Summary,
		Analysis:        h.toJSONB(result.Analysis),
		SourceReference: &payload.SourceReference,
		JobID:           &job.ID,
	}

	if err := h.store.CreateEntry(ctx, entry); err != nil {
		return nil, fmt.Errorf("create entry: %w", err)
	}

	h.logger.LogJobEvent(ctx, models.EventCompleted, map[string]interface{}{
		"handler":       h.JobType(),
		"duration_ms":   time.Since(start).Milliseconds(),
		"entry_id":      entry.ID,
		"word_count":    result.Analysis.WordCount,
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
func (h *JournalAnalyzeHandler) extractJSON(text string) (models.JournalAnalyzeResult, error) {
	var result models.JournalAnalyzeResult
	// Find first { and last }
	start := strings.Index(text, "{")
	end := strings.LastIndex(text, "}")
	if start == -1 || end == -1 || end <= start {
		return result, fmt.Errorf("no JSON object found in response")
	}
	jsonStr := text[start : end+1]
	err := json.Unmarshal([]byte(jsonStr), &result)
	return result, err
}

// toJSONB converts a struct to JSONB
func (h *JournalAnalyzeHandler) toJSONB(v interface{}) *models.JSONB {
	data, err := json.Marshal(v)
	if err != nil {
		return nil
	}
	jsonb := models.JSONB(data)
	return &jsonb
}

func init() {
	// Handler registration happens in main after dependencies are created
}