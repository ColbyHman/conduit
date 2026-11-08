package store

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/conduit/pkg/log"
	"github.com/conduit/pkg/models"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// ScheduleStore handles schedule persistence and due-row enqueueing.
type ScheduleStore struct {
	pool *pgxpool.Pool
	log  *log.Logger
}

// NewScheduleStore creates a new schedule store.
func NewScheduleStore(pool *pgxpool.Pool, logger *log.Logger) *ScheduleStore {
	return &ScheduleStore{pool: pool, log: logger.WithHandler("schedule_store")}
}

// ClaimDueSchedule atomically claims one due schedule row using the same
// SELECT ... FOR UPDATE SKIP LOCKED pattern the job queue uses (ADR 0007),
// so multiple scheduler replicas never double-fire the same row.
func (s *ScheduleStore) ClaimDueSchedule(ctx context.Context) (*models.Schedule, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback(ctx)

	row := tx.QueryRow(ctx, `
		SELECT id, name, job_type, payload_template, interval_seconds, enabled,
		       next_run_at, last_run_at, last_job_id, created_at, updated_at
		FROM schedules
		WHERE enabled = true AND next_run_at <= NOW()
		ORDER BY next_run_at ASC
		LIMIT 1
		FOR UPDATE SKIP LOCKED
	`)

	sched := &models.Schedule{}
	err = row.Scan(
		&sched.ID, &sched.Name, &sched.JobType, &sched.PayloadTemplate, &sched.IntervalSeconds,
		&sched.Enabled, &sched.NextRunAt, &sched.LastRunAt, &sched.LastJobID,
		&sched.CreatedAt, &sched.UpdatedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("claim due schedule: %w", err)
	}

	// idempotency_key = schedule_id + next_run_at truncated to the interval
	// boundary, so a crash-and-restart mid-tick can't double-enqueue the
	// same fire (ADR 0007).
	idempotencyKey := fmt.Sprintf("%s:%d", sched.ID.String(), sched.NextRunAt.Unix())

	var jobID uuid.UUID
	err = tx.QueryRow(ctx, `
		INSERT INTO jobs (job_type, status, payload, idempotency_key, max_retries)
		VALUES ($1, 'pending', $2, $3, 3)
		ON CONFLICT (job_type, idempotency_key) WHERE idempotency_key IS NOT NULL
		DO UPDATE SET id = jobs.id
		RETURNING id
	`, sched.JobType, sched.PayloadTemplate, idempotencyKey).Scan(&jobID)
	if err != nil {
		return nil, fmt.Errorf("enqueue job from schedule: %w", err)
	}

	nextRunAt := time.Now().Add(time.Duration(sched.IntervalSeconds) * time.Second)
	_, err = tx.Exec(ctx, `
		UPDATE schedules
		SET last_run_at = NOW(), last_job_id = $2, next_run_at = $3, updated_at = NOW()
		WHERE id = $1
	`, sched.ID, jobID, nextRunAt)
	if err != nil {
		return nil, fmt.Errorf("update schedule after fire: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit schedule fire: %w", err)
	}

	sched.LastJobID = &jobID
	sched.NextRunAt = nextRunAt
	return sched, nil
}
