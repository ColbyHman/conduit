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

// JobStore handles job persistence
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

// CreateJob inserts a new job with idempotency key support
func (s *JobStore) CreateJob(ctx context.Context, job *models.Job) error {
	query := `
		INSERT INTO jobs (job_type, status, payload, idempotency_key, max_retries)
		VALUES ($1, $2, $3, $4, $5)
		ON CONFLICT (job_type, idempotency_key) WHERE idempotency_key IS NOT NULL
		DO UPDATE SET id = jobs.id
		RETURNING id, created_at, updated_at
	`

	var id uuid.UUID
	var createdAt, updatedAt time.Time
	err := s.pool.QueryRow(ctx, query,
		job.JobType,
		job.Status,
		job.Payload,
		job.IdempotencyKey,
		job.MaxRetries,
	).Scan(&id, &createdAt, &updatedAt)

	if err != nil {
		s.log.Error().Err(err).Str("job_type", string(job.JobType)).Msg("failed to create job")
		return fmt.Errorf("create job: %w", err)
	}

	job.ID = id
	job.CreatedAt = createdAt
	job.UpdatedAt = updatedAt
	return nil
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

// GetJobByIdempotencyKey retrieves a job by idempotency key
func (s *JobStore) GetJobByIdempotencyKey(ctx context.Context, jobType models.JobType, key string) (*models.Job, error) {
	query := `
		SELECT id, job_type, status, payload, result, error, idempotency_key,
		       attempt_count, max_retries, next_retry_at, locked_at, locked_by,
		       created_at, updated_at, completed_at
		FROM jobs
		WHERE job_type = $1 AND idempotency_key = $2
	`

	job := &models.Job{}
	err := s.pool.QueryRow(ctx, query, jobType, key).Scan(
		&job.ID, &job.JobType, &job.Status, &job.Payload, &job.Result, &job.Error,
		&job.IdempotencyKey, &job.AttemptCount, &job.MaxRetries, &job.NextRetryAt,
		&job.LockedAt, &job.LockedBy, &job.CreatedAt, &job.UpdatedAt, &job.CompletedAt,
	)

	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		s.log.Error().Err(err).Str("job_type", string(jobType)).Str("key", key).Msg("failed to get job by idempotency key")
		return nil, fmt.Errorf("get job by idempotency key: %w", err)
	}

	return job, nil
}

// ClaimJob atomically claims a pending job for processing
func (s *JobStore) ClaimJob(ctx context.Context, workerID string, jobTypes []models.JobType) (*models.Job, error) {
	// Build job type filter
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
			return nil, nil // No jobs available
		}
		s.log.Error().Err(err).Str("worker_id", workerID).Msg("failed to claim job")
		return nil, fmt.Errorf("claim job: %w", err)
	}

	return job, nil
}

// CompleteJob marks a job as completed with result
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
func (s *JobStore) FailJob(ctx context.Context, jobID uuid.UUID, errMsg string, maxRetries int, baseDelay time.Duration) error {
	// Calculate next retry with exponential backoff + jitter
	// This is done in the worker, but we need attempt_count to decide
	var attemptCount int
	var jobMaxRetries int
	err := s.pool.QueryRow(ctx, `SELECT attempt_count, max_retries FROM jobs WHERE id = $1`, jobID).Scan(&attemptCount, &jobMaxRetries)
	if err != nil {
		return fmt.Errorf("get job retry info: %w", err)
	}

	if attemptCount >= jobMaxRetries {
		// Dead letter
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
		_, err = s.pool.Exec(ctx, query, jobID, errMsg)
		return err
	}

	// Schedule retry with exponential backoff
	// delay = min(baseDelay * 2^attempt, 10m) with ±25% jitter
	// This is approximate; worker calculates exact next_retry_at
	query := `
		UPDATE jobs
		SET status = 'pending',
		    error = $2,
		    next_retry_at = NOW() + ($3 * (2 ^ attempt_count)) + (random() * $3 * 0.5 - $3 * 0.25),
		    updated_at = NOW(),
		    locked_at = NULL,
		    locked_by = NULL
		WHERE id = $1
	`
	_, err = s.pool.Exec(ctx, query, jobID, errMsg, baseDelay)
	return err
}

// ReleaseJob releases a job back to pending (worker crash recovery)
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