package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/conduit/pkg/log"
	"github.com/conduit/pkg/models"
	"github.com/conduit/worker/internal/finance"
	"github.com/conduit/worker/internal/llm"
	"github.com/conduit/worker/internal/store"
)

// FinanceIngestHandler handles finance_ingest jobs (ADR 0015): fetches
// new SEC EDGAR filings (required) and Finnhub company news (optional,
// additive) for a ticker, writes finance_documents rows, then enqueues
// finance_analyze for any newly-fetched documents.
type FinanceIngestHandler struct {
	store   *store.JobStore
	logger  *log.Logger
	edgar   *finance.EdgarClient
	finnhub *finance.FinnhubClient // nil when FINNHUB_API_KEY is unset — degrade gracefully
}

// NewFinanceIngestHandler creates a new finance_ingest handler.
func NewFinanceIngestHandler(s *store.JobStore, logger *log.Logger, edgar *finance.EdgarClient, finnhub *finance.FinnhubClient) *FinanceIngestHandler {
	return &FinanceIngestHandler{
		store:   s,
		logger:  logger.WithHandler("finance_ingest"),
		edgar:   edgar,
		finnhub: finnhub,
	}
}

// JobType returns the job type this handler processes
func (h *FinanceIngestHandler) JobType() models.JobType {
	return models.JobTypeFinanceIngest
}

// Handle processes a finance_ingest job.
func (h *FinanceIngestHandler) Handle(ctx context.Context, job *models.Job, _ llm.LLMClient) (*models.JSONB, error) {
	start := time.Now()
	h.logger.LogJobEvent(ctx, models.EventProcessingStart, map[string]interface{}{
		"handler": h.JobType(),
	})

	var payload models.FinanceIngestPayload
	if err := json.Unmarshal(job.Payload, &payload); err != nil {
		return nil, fmt.Errorf("unmarshal payload: %w", err)
	}
	if payload.Ticker == "" {
		return nil, fmt.Errorf("finance_ingest requires ticker")
	}
	ticker := payload.Ticker

	newDocIDs := []string{}
	totalFound := 0

	// --- Required: SEC EDGAR filings ---
	filings, err := h.edgar.RecentFilings(ctx, ticker, 10)
	if err != nil {
		// Network/API failure against the required source is a real
		// failure — let the job retry via the normal retry machinery.
		return nil, fmt.Errorf("fetch EDGAR filings: %w", err)
	}
	totalFound += len(filings)
	for _, f := range filings {
		doc := &models.FinanceDocument{
			Ticker:  ticker,
			Source:  "sec_edgar",
			DocType: strPtr(f.Form),
			URL:     strPtr(f.URL),
			Title:   strPtr(fmt.Sprintf("%s filing (%s)", f.Form, f.FilingDate)),
			JobID:   &job.ID,
		}
		if publishedAt, perr := time.Parse("2006-01-02", f.FilingDate); perr == nil {
			doc.PublishedAt = &publishedAt
		}
		isNew, cerr := h.store.CreateFinanceDocument(ctx, doc)
		if cerr != nil {
			return nil, fmt.Errorf("store EDGAR document: %w", cerr)
		}
		if isNew {
			newDocIDs = append(newDocIDs, doc.ID.String())
		}
	}

	// --- Optional: Finnhub news. Degrade gracefully when no key is
	// configured (h.finnhub == nil) — this must not error or block
	// the EDGAR-only path (ADR 0015 scope). ---
	finnhubUsed := false
	if h.finnhub != nil {
		finnhubUsed = true
		to := time.Now().UTC().Format("2006-01-02")
		from := time.Now().UTC().AddDate(0, 0, -14).Format("2006-01-02")
		news, nerr := h.finnhub.CompanyNews(ctx, ticker, from, to)
		if nerr != nil {
			// Finnhub is additive, not required — log and continue
			// rather than failing the whole ingest run.
			h.logger.Warn().Err(nerr).Str("ticker", ticker).Msg("finnhub company news fetch failed, continuing with EDGAR-only data")
		} else {
			totalFound += len(news)
			for _, n := range news {
				doc := &models.FinanceDocument{
					Ticker:  ticker,
					Source:  "finnhub_news",
					DocType: strPtr("news"),
					URL:     strPtr(n.URL),
					Title:   strPtr(n.Headline),
					JobID:   &job.ID,
				}
				if n.DateUnix > 0 {
					publishedAt := time.Unix(n.DateUnix, 0).UTC()
					doc.PublishedAt = &publishedAt
				}
				text := n.Summary
				doc.RawText = &text
				isNew, cerr := h.store.CreateFinanceDocument(ctx, doc)
				if cerr != nil {
					return nil, fmt.Errorf("store finnhub document: %w", cerr)
				}
				if isNew {
					newDocIDs = append(newDocIDs, doc.ID.String())
				}
			}
		}
	}

	note := ""
	if totalFound == 0 {
		// Treasury/index tickers may legitimately have zero filings
		// and zero news coverage (ADR 0015 scope) — complete
		// successfully, log it, don't error.
		note = "no recent filings or news found for this ticker"
		h.logger.Info().Str("ticker", ticker).Msg(note)
	}

	// Chain into finance_analyze only when there's new material to
	// analyze — never analyze on an empty/no-new-documents run.
	if len(newDocIDs) > 0 {
		analyzePayload, merr := json.Marshal(models.FinanceAnalyzePayload{
			Ticker:      ticker,
			DocumentIDs: newDocIDs,
		})
		if merr != nil {
			return nil, fmt.Errorf("marshal finance_analyze payload: %w", merr)
		}
		idempotencyKey := fmt.Sprintf("finance_analyze:%s:%s", ticker, job.ID.String())
		analyzeJobID, eerr := h.store.EnqueueJob(ctx, models.JobTypeFinanceAnalyze, models.JSONB(analyzePayload), idempotencyKey)
		if eerr != nil {
			return nil, fmt.Errorf("enqueue finance_analyze: %w", eerr)
		}
		h.logger.Info().
			Str("ticker", ticker).
			Str("analyze_job_id", analyzeJobID.String()).
			Int("new_documents", len(newDocIDs)).
			Msg("enqueued finance_analyze for newly-fetched documents")
	}

	result := models.FinanceIngestResult{
		Ticker:         ticker,
		DocumentsFound: totalFound,
		NewDocumentIDs: newDocIDs,
		FinnhubUsed:    finnhubUsed,
		Note:           note,
	}

	h.logger.LogJobEvent(ctx, models.EventCompleted, map[string]interface{}{
		"handler":         h.JobType(),
		"duration_ms":     time.Since(start).Milliseconds(),
		"ticker":          ticker,
		"documents_found": totalFound,
		"new_documents":   len(newDocIDs),
	})

	resultJSON, err := json.Marshal(result)
	if err != nil {
		return nil, fmt.Errorf("marshal result: %w", err)
	}
	jsonb := models.JSONB(resultJSON)
	return &jsonb, nil
}

func strPtr(s string) *string { return &s }
