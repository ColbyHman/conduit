package store

import (
	"context"
	"fmt"

	"github.com/conduit/pkg/models"
	"github.com/google/uuid"
)

// CreateFinanceDocument inserts a fetched filing/news document.
// Returns (nil, nil) if the document already exists for this
// ticker+source+url (dedupe via the DB unique index), so re-ingesting
// the same filing twice is a no-op, not an error.
func (s *JobStore) CreateFinanceDocument(ctx context.Context, doc *models.FinanceDocument) (bool, error) {
	query := `
		INSERT INTO finance_documents (ticker, source, doc_type, url, title, published_at, raw_text, job_id)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		ON CONFLICT (ticker, source, url) DO NOTHING
		RETURNING id, fetched_at
	`
	err := s.pool.QueryRow(ctx, query,
		doc.Ticker, doc.Source, doc.DocType, doc.URL, doc.Title, doc.PublishedAt, doc.RawText, doc.JobID,
	).Scan(&doc.ID, &doc.FetchedAt)
	if err != nil {
		if err.Error() == "no rows in result set" {
			return false, nil // already existed, not new
		}
		s.log.Error().Err(err).Str("ticker", doc.Ticker).Msg("failed to create finance document")
		return false, fmt.Errorf("create finance document: %w", err)
	}
	return true, nil
}

// GetFinanceDocumentsByIDs fetches the full document rows for a set of
// IDs — used by finance_analyze to validate every LLM citation really
// references a document that was actually fetched for this run.
func (s *JobStore) GetFinanceDocumentsByIDs(ctx context.Context, ids []uuid.UUID) ([]models.FinanceDocument, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	rows, err := s.pool.Query(ctx, `
		SELECT id, ticker, source, doc_type, url, title, published_at, raw_text, fetched_at, job_id
		FROM finance_documents
		WHERE id = ANY($1)
	`, ids)
	if err != nil {
		return nil, fmt.Errorf("get finance documents by ids: %w", err)
	}
	defer rows.Close()

	var docs []models.FinanceDocument
	for rows.Next() {
		var d models.FinanceDocument
		if err := rows.Scan(&d.ID, &d.Ticker, &d.Source, &d.DocType, &d.URL, &d.Title, &d.PublishedAt, &d.RawText, &d.FetchedAt, &d.JobID); err != nil {
			return nil, fmt.Errorf("scan finance document: %w", err)
		}
		docs = append(docs, d)
	}
	return docs, nil
}

// CreateFinanceAnalysis inserts one finance_analyze result row.
func (s *JobStore) CreateFinanceAnalysis(ctx context.Context, a *models.FinanceAnalysis) error {
	query := `
		INSERT INTO finance_analyses (ticker, summary, event_type, sentiment, materiality, citations, job_id)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
		RETURNING id, created_at
	`
	err := s.pool.QueryRow(ctx, query,
		a.Ticker, a.Summary, a.EventType, a.Sentiment, a.Materiality, a.Citations, a.JobID,
	).Scan(&a.ID, &a.CreatedAt)
	if err != nil {
		s.log.Error().Err(err).Str("ticker", a.Ticker).Msg("failed to create finance analysis")
		return fmt.Errorf("create finance analysis: %w", err)
	}
	return nil
}

// EnqueueJob inserts a new job directly into the queue (used by
// finance_ingest to chain into finance_analyze on completion, and by
// the gateway for immediate/manual enqueues).
func (s *JobStore) EnqueueJob(ctx context.Context, jobType models.JobType, payload models.JSONB, idempotencyKey string) (uuid.UUID, error) {
	var key *string
	if idempotencyKey != "" {
		key = &idempotencyKey
	}
	var id uuid.UUID
	err := s.pool.QueryRow(ctx, `
		INSERT INTO jobs (job_type, status, payload, idempotency_key, max_retries)
		VALUES ($1, 'pending', $2, $3, 3)
		ON CONFLICT (job_type, idempotency_key) WHERE idempotency_key IS NOT NULL
		DO UPDATE SET id = jobs.id
		RETURNING id
	`, jobType, payload, key).Scan(&id)
	if err != nil {
		return uuid.Nil, fmt.Errorf("enqueue job: %w", err)
	}
	return id, nil
}
