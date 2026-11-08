package store

import (
	"context"
	"fmt"

	"github.com/conduit/pkg/log"
	"github.com/conduit/pkg/models"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// ScheduleStore handles admin visibility into ADR 0007 schedules.
type ScheduleStore struct {
	pool *pgxpool.Pool
	log  *log.Logger
}

// NewScheduleStore creates a new schedule store.
func NewScheduleStore(pool *pgxpool.Pool, logger *log.Logger) *ScheduleStore {
	return &ScheduleStore{pool: pool, log: logger.WithHandler("schedule_store")}
}

// ListSchedules returns every schedule row, newest first.
func (s *ScheduleStore) ListSchedules(ctx context.Context) ([]models.Schedule, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, name, job_type, payload_template, interval_seconds, enabled,
		       next_run_at, last_run_at, last_job_id, created_at, updated_at
		FROM schedules
		ORDER BY created_at DESC
	`)
	if err != nil {
		return nil, fmt.Errorf("list schedules: %w", err)
	}
	defer rows.Close()

	var out []models.Schedule
	for rows.Next() {
		var sch models.Schedule
		if err := rows.Scan(&sch.ID, &sch.Name, &sch.JobType, &sch.PayloadTemplate, &sch.IntervalSeconds,
			&sch.Enabled, &sch.NextRunAt, &sch.LastRunAt, &sch.LastJobID, &sch.CreatedAt, &sch.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scan schedule: %w", err)
		}
		out = append(out, sch)
	}
	return out, nil
}

// SetScheduleEnabled pauses/resumes a schedule by id (admin PATCH).
func (s *ScheduleStore) SetScheduleEnabled(ctx context.Context, id uuid.UUID, enabled bool) error {
	cmd, err := s.pool.Exec(ctx, `UPDATE schedules SET enabled = $2, updated_at = NOW() WHERE id = $1`, id, enabled)
	if err != nil {
		return fmt.Errorf("set schedule enabled: %w", err)
	}
	if cmd.RowsAffected() == 0 {
		return pgx.ErrNoRows
	}
	return nil
}

// CreateScheduleForTicker inserts (or re-enables) the ADR 0015
// finance_ingest schedule for a holding — interval_seconds=21600 (6h).
func (s *ScheduleStore) CreateScheduleForTicker(ctx context.Context, ticker string) error {
	name := "finance_ingest_" + stringsLower(ticker)
	payload := models.JSONB(fmt.Sprintf(`{"ticker": %q}`, ticker))
	_, err := s.pool.Exec(ctx, `
		INSERT INTO schedules (name, job_type, payload_template, interval_seconds, enabled, next_run_at)
		VALUES ($1, 'finance_ingest', $2, 21600, true, NOW())
		ON CONFLICT (name) DO UPDATE SET enabled = true, updated_at = NOW()
	`, name, payload)
	if err != nil {
		return fmt.Errorf("create schedule for ticker: %w", err)
	}
	return nil
}

// DisableScheduleForTicker disables (not deletes) a ticker's schedule,
// so re-adding the same ticker later doesn't duplicate schedules (ADR 0015).
func (s *ScheduleStore) DisableScheduleForTicker(ctx context.Context, ticker string) error {
	name := "finance_ingest_" + stringsLower(ticker)
	_, err := s.pool.Exec(ctx, `UPDATE schedules SET enabled = false, updated_at = NOW() WHERE name = $1`, name)
	if err != nil {
		return fmt.Errorf("disable schedule for ticker: %w", err)
	}
	return nil
}

func stringsLower(s string) string {
	b := []byte(s)
	for i, c := range b {
		if c >= 'A' && c <= 'Z' {
			b[i] = c + ('a' - 'A')
		}
	}
	return string(b)
}
