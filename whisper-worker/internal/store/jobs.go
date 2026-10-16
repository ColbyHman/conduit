package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/conduit/pkg/log"
	"github.com/conduit/pkg/models"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// JobStore handles job persistence for whisper worker
type JobStore struct {
	pool *pgxpool.Pool
	log  *log.Logger
}

// NewJobStore creates a new job store
func NewJobStore(pool *pgxpool.Pool, logger *log.Logger) *JobStore {
	return &JobStore{
		pool: pool,
		log:  logger.WithHandler("job_store"),
	}
}

// ClaimJob atomically claims a pending transcribe job
func (s *JobStore) ClaimJob(ctx context.Context, workerID string, jobTypes []models.JobType) (*models.Job, error) {
	placeholders := make([]string, len(jobTypes))
	args := make([]interface{}, len(jobTypes)+1)
	args[0] = workerID
	for i, jt := range jobTypes {
		placeholders[i] = fmt.Sprintf("$%d", i+2)
		args[i+1] = jt
	}

	query := fmt.Sprintf(`
		UPDATE jobs
		SET status = 'processing',
		    locked_at = NOW(),
		    locked_by = $1,
		    attempt_count = attempt_count + 1,
		    updated_at = NOW()
		WHERE id = (
			SELECT id FROM jobs
			WHERE status = 'pending'
			  AND (next_retry_at IS NULL OR next_retry_at <= NOW())
			  AND job_type IN (%s)
			ORDER BY created_at ASC
			LIMIT 1
			FOR UPDATE SKIP LOCKED
		)
		RETURNING id, job_type, status, payload, result, error, idempotency_key,
		          attempt_count, max_retries, next_retry_at, locked_at, locked_by,
		          created_at, updated_at, completed_at
	`, placeholders)

	job := &models.Job{}
	err := s.pool.QueryRow(ctx, query, args...).Scan(
		&job.ID, &job.JobType, &job.Status, &job.Payload, &job.Result, &job.Error,
		&job.IdempotencyKey, &job.AttemptCount, &job.MaxRetries, &job.NextRetryAt,
		&job.LockedAt, &job.LockedBy, &job.CreatedAt, &job.UpdatedAt, &job.CompletedAt,
	)

	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		s.log.Error().Err(err).Str("worker_id", workerID).Msg("failed to claim job")
		return nil, fmt.Errorf("claim job: %w", err)
	}

	return job, nil
}

// GetJobByID retrieves a job by ID
func (s *JobStore) GetJobByID(ctx context.Context, id uuid.UUID) (*models.Job, error) {
	query := `
		SELECT id, job_type, status, payload, result, error, idempotency_key,
		       attempt_count, max_retries, next_retry_at, locked_at, locked_by,
		       created_at, updated_at, completed_at
		FROM jobs
		WHERE id = $1
	`

	job := &models.Job{}
	err := s.pool.QueryRow(ctx, query, id).Scan(
		&job.ID, &job.JobType, &job.Status, &job.Payload, &job.Result, &job.Error,
		&job.IdempotencyKey, &job.AttemptCount, &job.MaxRetries, &job.NextRetryAt,
		&job.LockedAt, &job.LockedBy, &job.CreatedAt, &job.UpdatedAt, &job.CompletedAt,
	)

	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		s.log.Error().Err(err).Str("job_id", id.String()).Msg("failed to get job")
		return nil, fmt.Errorf("get job: %w", err)
	}

	return job, nil
}

// CompleteJob marks a job as completed with transcription result
func (s *JobStore) CompleteJob(ctx context.Context, jobID uuid.UUID, result models.JSONB) error {
	query := `
		UPDATE jobs
		SET status = 'completed',
		    result = $2,
		    completed_at = NOW(),
		    updated_at = NOW(),
		    locked_at = NULL,
		    locked_by = NULL
		WHERE id = $1
	`

	cmd, err := s.pool.Exec(ctx, query, jobID, result)
	if err != nil {
		s.log.Error().Err(err).Str("job_id", jobID.String()).Msg("failed to complete job")
		return fmt.Errorf("complete job: %w", err)
	}

	if cmd.RowsAffected() == 0 {
		return sql.ErrNoRows
	}

	return nil
}

// FailJob marks a job as failed and schedules retry or dead letter
func (s *JobStore) FailJob(ctx context.Context, jobID uuid.UUID, errMsg string, attemptCount, maxRetries int, baseDelay, maxDelay time.Duration) error {
	if attemptCount >= maxRetries {
		query := `
			UPDATE jobs
			SET status = 'dead_letter',
			    error = $2,
			    completed_at = NOW(),
			    updated_at = NOW(),
			    locked_at = NULL,
			    locked_by = NULL
			WHERE id = $1
		`
		_, err := s.pool.Exec(ctx, query, jobID, errMsg)
		if err != nil {
			s.log.Error().Err(err).Str("job_id", jobID.String()).Msg("failed to mark job dead_letter")
			return fmt.Errorf("mark dead_letter: %w", err)
		}
		s.log.Warn().
			Str("job_id", jobID.String()).
			Int("attempts", attemptCount).
			Str("error", errMsg).
			Msg("job moved to dead_letter after max retries")
		return nil
	}

	delay := baseDelay
	for i := 0; i < attemptCount; i++ {
		delay *= 2
	}
	if delay > maxDelay {
		delay = maxDelay
	}
	jitter := time.Duration(float64(delay) * 0.25 * (2*float64(time.Now().UnixNano()%1000)/1000.0 - 1))
	nextRetryAt := time.Now().Add(delay + jitter)

	query := `
		UPDATE jobs
		SET status = 'pending',
		    error = $2,
		    next_retry_at = $3,
		    updated_at = NOW(),
		    locked_at = NULL,
		    locked_by = NULL
		WHERE id = $1
	`
	_, err := s.pool.Exec(ctx, query, jobID, errMsg, nextRetryAt)
	if err != nil {
		s.log.Error().Err(err).Str("job_id", jobID.String()).Msg("failed to schedule retry")
		return fmt.Errorf("schedule retry: %w", err)
	}

	s.log.Info().
		Str("job_id", jobID.String()).
		Int("attempt", attemptCount).
		Int("max_retries", maxRetries).
		Time("next_retry_at", nextRetryAt).
		Msg("job retry scheduled")

	return nil
}

// ReleaseJob releases a job back to pending
func (s *JobStore) ReleaseJob(ctx context.Context, jobID uuid.UUID, workerID string) error {
	query := `
		UPDATE jobs
		SET status = 'pending',
		    locked_at = NULL,
		    locked_by = NULL,
		    updated_at = NOW()
		WHERE id = $1 AND locked_by = $2
	`
	cmd, err := s.pool.Exec(ctx, query, jobID, workerID)
	if err != nil {
		return fmt.Errorf("release job: %w", err)
	}
	if cmd.RowsAffected() == 0 {
		return sql.ErrNoRows
	}
	return nil
}

// CleanupStaleJobs releases jobs locked by dead workers
func (s *JobStore) CleanupStaleJobs(ctx context.Context, staleThreshold time.Duration) (int64, error) {
	query := `
		UPDATE jobs
		SET status = 'pending',
		    locked_at = NULL,
		    locked_by = NULL,
		    updated_at = NOW()
		WHERE status = 'processing'
		  AND locked_at < NOW() - $1
	`
	cmd, err := s.pool.Exec(ctx, query, staleThreshold)
	if err != nil {
		return 0, fmt.Errorf("cleanup stale jobs: %w", err)
	}
	return cmd.RowsAffected(), nil
}

// LogJobEvent records a job lifecycle event
func (s *JobStore) LogJobEvent(ctx context.Context, event *models.JobEvent) error {
	query := `
		INSERT INTO job_events (job_id, event_type, worker_id, payload)
		VALUES ($1, $2, $3, $4)
	`
	_, err := s.pool.Exec(ctx, query, event.JobID, event.EventType, event.WorkerID, event.Payload)
	if err != nil {
		s.log.Error().Err(err).Str("job_id", event.JobID.String()).Str("event", event.EventType).Msg("failed to log job event")
		return fmt.Errorf("log job event: %w", err)
	}
	return nil
}