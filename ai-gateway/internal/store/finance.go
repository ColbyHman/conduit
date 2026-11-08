package store

import (
	"context"
	"errors"
	"fmt"

	"github.com/conduit/pkg/log"
	"github.com/conduit/pkg/models"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// FinanceStore handles finance_holdings / finance_documents /
// finance_analyses reads for the ai-gateway (ADR 0015).
type FinanceStore struct {
	pool *pgxpool.Pool
	log  *log.Logger
}

// NewFinanceStore creates a new finance store.
func NewFinanceStore(pool *pgxpool.Pool, logger *log.Logger) *FinanceStore {
	return &FinanceStore{pool: pool, log: logger.WithHandler("finance_store")}
}

// ListActiveHoldings returns every active holding.
func (s *FinanceStore) ListActiveHoldings(ctx context.Context) ([]models.FinanceHolding, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, ticker, asset_type, active, created_at, updated_at
		FROM finance_holdings WHERE active = true ORDER BY ticker ASC
	`)
	if err != nil {
		return nil, fmt.Errorf("list active holdings: %w", err)
	}
	defer rows.Close()

	var out []models.FinanceHolding
	for rows.Next() {
		var h models.FinanceHolding
		if err := rows.Scan(&h.ID, &h.Ticker, &h.AssetType, &h.Active, &h.CreatedAt, &h.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scan holding: %w", err)
		}
		out = append(out, h)
	}
	return out, nil
}

// CreateHolding inserts a new active holding for a ticker, or
// reactivates a previously soft-deleted one.
func (s *FinanceStore) CreateHolding(ctx context.Context, ticker, assetType string) (*models.FinanceHolding, error) {
	h := &models.FinanceHolding{}
	err := s.pool.QueryRow(ctx, `
		INSERT INTO finance_holdings (ticker, asset_type, active)
		VALUES ($1, $2, true)
		RETURNING id, ticker, asset_type, active, created_at, updated_at
	`, ticker, assetType).Scan(&h.ID, &h.Ticker, &h.AssetType, &h.Active, &h.CreatedAt, &h.UpdatedAt)
	if err != nil {
		return nil, fmt.Errorf("create holding: %w", err)
	}
	return h, nil
}

// SoftDeleteHolding sets active=false for a ticker's current active row.
func (s *FinanceStore) SoftDeleteHolding(ctx context.Context, ticker string) error {
	cmd, err := s.pool.Exec(ctx, `
		UPDATE finance_holdings SET active = false, updated_at = NOW()
		WHERE ticker = $1 AND active = true
	`, ticker)
	if err != nil {
		return fmt.Errorf("soft delete holding: %w", err)
	}
	if cmd.RowsAffected() == 0 {
		return pgx.ErrNoRows
	}
	return nil
}

// GetActiveHoldingByTicker fetches a single active holding.
func (s *FinanceStore) GetActiveHoldingByTicker(ctx context.Context, ticker string) (*models.FinanceHolding, error) {
	h := &models.FinanceHolding{}
	err := s.pool.QueryRow(ctx, `
		SELECT id, ticker, asset_type, active, created_at, updated_at
		FROM finance_holdings WHERE ticker = $1 AND active = true
	`, ticker).Scan(&h.ID, &h.Ticker, &h.AssetType, &h.Active, &h.CreatedAt, &h.UpdatedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("get active holding: %w", err)
	}
	return h, nil
}

// LatestAnalysisByTicker returns the most recent finance_analyses row
// for a ticker, or nil if none exists yet (e.g. ingest hasn't produced
// a citable analysis for this holding yet).
func (s *FinanceStore) LatestAnalysisByTicker(ctx context.Context, ticker string) (*models.FinanceAnalysis, error) {
	a := &models.FinanceAnalysis{}
	err := s.pool.QueryRow(ctx, `
		SELECT id, ticker, summary, event_type, sentiment, materiality, citations, job_id, created_at
		FROM finance_analyses WHERE ticker = $1 ORDER BY created_at DESC LIMIT 1
	`, ticker).Scan(&a.ID, &a.Ticker, &a.Summary, &a.EventType, &a.Sentiment, &a.Materiality, &a.Citations, &a.JobID, &a.CreatedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("get latest analysis: %w", err)
	}
	return a, nil
}

// DocumentCountByTicker returns how many finance_documents exist for a
// ticker, used to distinguish "never ingested" from "ingested, zero
// found" in the triage no-activity state.
func (s *FinanceStore) DocumentCountByTicker(ctx context.Context, ticker string) (int, error) {
	var n int
	err := s.pool.QueryRow(ctx, `SELECT COUNT(*) FROM finance_documents WHERE ticker = $1`, ticker).Scan(&n)
	if err != nil {
		return 0, fmt.Errorf("count documents: %w", err)
	}
	return n, nil
}
