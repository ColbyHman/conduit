package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/conduit/pkg/log"
	"github.com/conduit/pkg/models"
	"github.com/conduit/worker/internal/llm"
	"github.com/conduit/worker/internal/store"
	"github.com/google/uuid"
)

// FinanceAnalyzeHandler handles finance_analyze jobs (ADR 0015): an
// LLM pass over the documents fetched by a finance_ingest run,
// producing one finance_analyses row. Every citation the LLM returns
// MUST reference a document_id that was actually fetched for this
// run — if it invents one, the whole row is hard-failed (not written),
// per the anti-fabrication guardrail.
type FinanceAnalyzeHandler struct {
	store     *store.JobStore
	prompts   *PromptLoader
	logger    *log.Logger
	maxTokens int
}

// NewFinanceAnalyzeHandler creates a new finance_analyze handler.
func NewFinanceAnalyzeHandler(s *store.JobStore, prompts *PromptLoader, logger *log.Logger, maxTokens int) *FinanceAnalyzeHandler {
	return &FinanceAnalyzeHandler{
		store:     s,
		prompts:   prompts,
		logger:    logger.WithHandler("finance_analyze"),
		maxTokens: maxTokens,
	}
}

// JobType returns the job type this handler processes
func (h *FinanceAnalyzeHandler) JobType() models.JobType {
	return models.JobTypeFinanceAnalyze
}

type financePromptDoc struct {
	ID      string
	Source  string
	DocType string
	Title   string
	URL     string
}

// Handle processes a finance_analyze job.
func (h *FinanceAnalyzeHandler) Handle(ctx context.Context, job *models.Job, llmClient llm.LLMClient) (*models.JSONB, error) {
	start := time.Now()
	h.logger.LogJobEvent(ctx, models.EventProcessingStart, map[string]interface{}{
		"handler": h.JobType(),
	})

	var payload models.FinanceAnalyzePayload
	if err := json.Unmarshal(job.Payload, &payload); err != nil {
		return nil, fmt.Errorf("unmarshal payload: %w", err)
	}
	if payload.Ticker == "" || len(payload.DocumentIDs) == 0 {
		return nil, fmt.Errorf("finance_analyze requires ticker and document_ids")
	}

	// Fetch the actual rows for the document IDs — this is the
	// source-of-truth set every citation must be checked against.
	ids := make([]uuid.UUID, 0, len(payload.DocumentIDs))
	for _, idStr := range payload.DocumentIDs {
		id, err := uuid.Parse(idStr)
		if err != nil {
			return nil, fmt.Errorf("invalid document id %q: %w", idStr, err)
		}
		ids = append(ids, id)
	}
	docs, err := h.store.GetFinanceDocumentsByIDs(ctx, ids)
	if err != nil {
		return nil, fmt.Errorf("fetch documents: %w", err)
	}
	if len(docs) == 0 {
		return nil, fmt.Errorf("no documents found for ids %v", payload.DocumentIDs)
	}

	fetchedSet := make(map[string]models.FinanceDocument, len(docs))
	promptDocs := make([]financePromptDoc, 0, len(docs))
	for _, d := range docs {
		fetchedSet[d.ID.String()] = d
		pd := financePromptDoc{ID: d.ID.String(), Source: d.Source}
		if d.DocType != nil {
			pd.DocType = *d.DocType
		}
		if d.Title != nil {
			pd.Title = *d.Title
		}
		if d.URL != nil {
			pd.URL = *d.URL
		}
		promptDocs = append(promptDocs, pd)
	}

	prompt, err := h.prompts.Render("finance_analysis", struct {
		Ticker    string
		Documents []financePromptDoc
	}{Ticker: payload.Ticker, Documents: promptDocs})
	if err != nil {
		return nil, fmt.Errorf("render prompt: %w", err)
	}

	// System prompt carries the language guardrail forward: no
	// buy/sell/hold, information not a directive (ADR 0010/0011,
	// carried into ADR 0015).
	messages := []llm.Message{
		{Role: "system", Content: "You are a financial-document analyst. You never use buy/sell/hold language or give directives — you surface information for the reader to weigh themselves. You never cite a document_id that wasn't given to you. Respond only with valid JSON."},
		{Role: "user", Content: prompt},
	}

	h.logger.LogJobEvent(ctx, models.EventLLMRequest, map[string]interface{}{
		"model":         llmClient.Model(),
		"ticker":        payload.Ticker,
		"document_count": len(docs),
	})

	llmResp, err := llmClient.Complete(ctx, llm.CompletionRequest{
		Messages:    messages,
		Temperature: 0.2,
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
	})

	var result models.FinanceAnalyzeResult
	if err := json.Unmarshal([]byte(llmResp.Content), &result); err != nil {
		result, err = h.extractJSON(llmResp.Content)
		if err != nil {
			return nil, fmt.Errorf("parse LLM response: %w", err)
		}
	}

	// Anti-fabrication guardrail: hard-fail (do not write the row) if
	// any citation references a document_id outside the fetched set
	// for this run.
	for _, c := range result.Citations {
		if _, ok := fetchedSet[c.DocumentID]; !ok {
			return nil, fmt.Errorf("finance_analyze guardrail violation: citation references document_id %q which is not in the fetched set for this run", c.DocumentID)
		}
	}

	citationsJSON, err := json.Marshal(result.Citations)
	if err != nil {
		return nil, fmt.Errorf("marshal citations: %w", err)
	}

	materiality := result.Materiality
	analysis := &models.FinanceAnalysis{
		Ticker:      payload.Ticker,
		Summary:     result.Summary,
		EventType:   strPtr(result.EventType),
		Sentiment:   strPtr(result.Sentiment),
		Materiality: &materiality,
		Citations:   models.JSONB(citationsJSON),
		JobID:       &job.ID,
	}
	if err := h.store.CreateFinanceAnalysis(ctx, analysis); err != nil {
		return nil, fmt.Errorf("create finance analysis: %w", err)
	}

	h.logger.LogJobEvent(ctx, models.EventCompleted, map[string]interface{}{
		"handler":      h.JobType(),
		"duration_ms":  time.Since(start).Milliseconds(),
		"ticker":       payload.Ticker,
		"analysis_id":  analysis.ID,
		"citation_count": len(result.Citations),
	})

	resultJSON, err := json.Marshal(result)
	if err != nil {
		return nil, fmt.Errorf("marshal result: %w", err)
	}
	jsonb := models.JSONB(resultJSON)
	return &jsonb, nil
}

// extractJSON attempts to extract a JSON object from a text response
// that may have surrounding prose despite the "respond only with JSON"
// instruction.
func (h *FinanceAnalyzeHandler) extractJSON(text string) (models.FinanceAnalyzeResult, error) {
	var result models.FinanceAnalyzeResult
	start := strings.Index(text, "{")
	end := strings.LastIndex(text, "}")
	if start == -1 || end == -1 || end <= start {
		return result, fmt.Errorf("no JSON object found in response")
	}
	err := json.Unmarshal([]byte(text[start:end+1]), &result)
	return result, err
}
