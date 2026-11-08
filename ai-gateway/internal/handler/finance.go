package handler

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/conduit/ai-gateway/internal/finance"
	"github.com/conduit/ai-gateway/internal/store"
	"github.com/conduit/pkg/log"
	"github.com/conduit/pkg/models"
	"github.com/gin-gonic/gin"
)

// FinanceHandler exposes the ADR 0015 finance domain endpoints:
// holdings CRUD + the triage aggregation view + manual refresh.
type FinanceHandler struct {
	financeStore  *store.FinanceStore
	scheduleStore *store.ScheduleStore
	jobStore      JobStore // reuse the existing job store interface for enqueue
	edgar         *finance.EdgarClient
	logger        *log.Logger
}

// NewFinanceHandler creates a new finance handler.
func NewFinanceHandler(fs *store.FinanceStore, ss *store.ScheduleStore, js JobStore, edgar *finance.EdgarClient, logger *log.Logger) *FinanceHandler {
	return &FinanceHandler{
		financeStore:  fs,
		scheduleStore: ss,
		jobStore:      js,
		edgar:         edgar,
		logger:        logger.WithHandler("finance_handler"),
	}
}

// ListHoldings handles GET /finance/holdings
func (h *FinanceHandler) ListHoldings(c *gin.Context) {
	ctx := c.Request.Context()
	holdings, err := h.financeStore.ListActiveHoldings(ctx)
	if err != nil {
		h.logger.Error().Err(err).Msg("failed to list holdings")
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal error"})
		return
	}
	if holdings == nil {
		holdings = []models.FinanceHolding{}
	}
	c.JSON(http.StatusOK, gin.H{"holdings": holdings})
}

// CreateHoldingRequest is the body for POST /finance/holdings
type CreateHoldingRequest struct {
	Ticker    string `json:"ticker"`
	AssetType string `json:"asset_type"`
}

var validAssetTypes = map[string]bool{"equity": true, "etf": true, "treasury": true, "index": true}

// CreateHolding handles POST /finance/holdings: validates the ticker
// against EDGAR's company-ticker lookup, inserts the holding + its
// ADR 0007 schedule row, and enqueues an immediate finance_ingest job.
func (h *FinanceHandler) CreateHolding(c *gin.Context) {
	ctx := c.Request.Context()

	var req CreateHoldingRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid JSON body"})
		return
	}
	ticker := strings.ToUpper(strings.TrimSpace(req.Ticker))
	assetType := strings.ToLower(strings.TrimSpace(req.AssetType))

	if ticker == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "ticker is required"})
		return
	}
	if !validAssetTypes[assetType] {
		c.JSON(http.StatusBadRequest, gin.H{"error": "asset_type must be one of: equity, etf, treasury, index"})
		return
	}

	// Validate the ticker against SEC EDGAR's company-ticker lookup so
	// a typo fails fast with a plain-language error instead of
	// silently queuing a job that will never find data.
	companyName, found, err := h.edgar.LookupTicker(ctx, ticker)
	if err != nil {
		h.logger.Warn().Err(err).Str("ticker", ticker).Msg("EDGAR ticker lookup failed")
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "couldn't reach SEC EDGAR to validate this ticker right now — try again shortly"})
		return
	}
	if !found {
		// Treasury/index tickers may legitimately not appear in
		// EDGAR's company list (it's equity-focused) — only hard-fail
		// for asset_type=equity, where a typo is the most likely cause.
		if assetType == "equity" {
			c.JSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf("%q doesn't match any company in SEC EDGAR's ticker list — double check the symbol", ticker)})
			return
		}
		h.logger.Info().Str("ticker", ticker).Str("asset_type", assetType).Msg("ticker not found in EDGAR company list, accepting anyway (non-equity asset type)")
	}

	holding, err := h.financeStore.CreateHolding(ctx, ticker, assetType)
	if err != nil {
		h.logger.Error().Err(err).Str("ticker", ticker).Msg("failed to create holding")
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to add holding"})
		return
	}

	if err := h.scheduleStore.CreateScheduleForTicker(ctx, ticker); err != nil {
		h.logger.Error().Err(err).Str("ticker", ticker).Msg("failed to create schedule for ticker")
		// Holding is created; surface a warning but don't fail the request outright.
	}

	// Immediate finance_ingest job so the live demo doesn't wait 6h.
	payload, _ := json.Marshal(models.FinanceIngestPayload{Ticker: ticker})
	job := &models.Job{
		JobType:    models.JobTypeFinanceIngest,
		Status:     models.JobStatusPending,
		Payload:    models.JSONB(payload),
		MaxRetries: 3,
	}
	if err := h.jobStore.CreateJob(ctx, job); err != nil {
		h.logger.Error().Err(err).Str("ticker", ticker).Msg("failed to enqueue initial finance_ingest")
	}

	h.logger.Info().Str("ticker", ticker).Str("company", companyName).Msg("holding added")
	c.JSON(http.StatusCreated, gin.H{"holding": holding, "job_id": job.ID})
}

// DeleteHolding handles DELETE /finance/holdings/:ticker — soft delete.
func (h *FinanceHandler) DeleteHolding(c *gin.Context) {
	ctx := c.Request.Context()
	ticker := strings.ToUpper(strings.TrimSpace(c.Param("ticker")))

	if err := h.financeStore.SoftDeleteHolding(ctx, ticker); err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "holding not found or already inactive"})
		return
	}
	if err := h.scheduleStore.DisableScheduleForTicker(ctx, ticker); err != nil {
		h.logger.Warn().Err(err).Str("ticker", ticker).Msg("failed to disable schedule for removed holding")
	}
	c.JSON(http.StatusOK, gin.H{"ticker": ticker, "active": false})
}

// RefreshHolding handles POST /finance/holdings/:ticker/refresh —
// enqueues finance_ingest immediately, bypassing the schedule.
func (h *FinanceHandler) RefreshHolding(c *gin.Context) {
	ctx := c.Request.Context()
	ticker := strings.ToUpper(strings.TrimSpace(c.Param("ticker")))

	holding, err := h.financeStore.GetActiveHoldingByTicker(ctx, ticker)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal error"})
		return
	}
	if holding == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "no active holding for this ticker"})
		return
	}

	payload, _ := json.Marshal(models.FinanceIngestPayload{Ticker: ticker})
	job := &models.Job{
		JobType:    models.JobTypeFinanceIngest,
		Status:     models.JobStatusPending,
		Payload:    models.JSONB(payload),
		MaxRetries: 3,
	}
	if err := h.jobStore.CreateJob(ctx, job); err != nil {
		h.logger.Error().Err(err).Str("ticker", ticker).Msg("failed to enqueue manual refresh")
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to enqueue refresh"})
		return
	}
	c.JSON(http.StatusAccepted, gin.H{"ticker": ticker, "job_id": job.ID, "status": job.Status})
}

// TriageItem matches apps/web/src/lib/triage.ts's TriageHolding shape
// exactly, per ADR 0015 ("same response shape ... so the frontend data
// layer swaps source, not shape").
type TriageItem struct {
	Ticker   string   `json:"ticker"`
	Status   string   `json:"status"` // "act" | "watch" | "none"
	Headline string   `json:"headline"`
	Summary  string   `json:"summary,omitempty"`
	Reason   string   `json:"reason,omitempty"`
	Reviewed bool     `json:"reviewed"`
	AssetType string  `json:"asset_type"`
}

// GetTriage handles GET /finance/triage: aggregates the latest
// finance_analyses row per active holding into the Act/Watch/Reviewed
// shape the frontend already renders from fixtures.
func (h *FinanceHandler) GetTriage(c *gin.Context) {
	ctx := c.Request.Context()

	holdings, err := h.financeStore.ListActiveHoldings(ctx)
	if err != nil {
		h.logger.Error().Err(err).Msg("failed to list holdings for triage")
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal error"})
		return
	}

	items := make([]TriageItem, 0, len(holdings))
	for _, holding := range holdings {
		item := TriageItem{Ticker: holding.Ticker, AssetType: holding.AssetType}

		analysis, aerr := h.financeStore.LatestAnalysisByTicker(ctx, holding.Ticker)
		if aerr != nil {
			h.logger.Error().Err(aerr).Str("ticker", holding.Ticker).Msg("failed to fetch latest analysis")
			c.JSON(http.StatusInternalServerError, gin.H{"error": "internal error"})
			return
		}

		if analysis == nil {
			// Never fabricate — if ingest hasn't produced an analysis
			// yet (including the "zero documents found" case for
			// treasury/index tickers), render the plain no-activity
			// state instead of crashing or inventing a narrative.
			docCount, derr := h.financeStore.DocumentCountByTicker(ctx, holding.Ticker)
			if derr != nil {
				docCount = 0
			}
			item.Status = "none"
			item.Reviewed = true
			if docCount == 0 {
				item.Headline = "No recent filings or news found for this holding"
			} else {
				item.Headline = "Documents fetched, analysis pending"
			}
			items = append(items, item)
			continue
		}

		item.Summary = analysis.Summary
		item.Headline = analysis.Summary
		if len(item.Headline) > 140 {
			item.Headline = item.Headline[:137] + "..."
		}
		if analysis.EventType != nil {
			item.Reason = "Event type: " + *analysis.EventType
		}

		materiality := 0
		if analysis.Materiality != nil {
			materiality = *analysis.Materiality
		}
		switch {
		case analysis.EventType != nil && *analysis.EventType == "no_activity":
			item.Status = "none"
			item.Reviewed = true
		case materiality >= 50:
			item.Status = "act"
		case materiality >= 20:
			item.Status = "watch"
		default:
			item.Status = "none"
			item.Reviewed = true
		}
		items = append(items, item)
	}

	c.JSON(http.StatusOK, gin.H{
		"holdings": items,
		"disclaimer": "This ranks and summarizes holdings by how much attention recent evidence suggests they need — it is not a recommendation to buy, sell, or hold anything. Every claim links to its source.",
	})
}
