package handler

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/conduit/pkg/log"
	"github.com/conduit/pkg/models"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

// JobHandler handles job submission endpoints
type JobHandler struct {
	store      JobStore
	logger     *log.Logger
	idempotencyHeader string
	maxKeyLength     int
}

// JobStore interface for job persistence
type JobStore interface {
	CreateJob(ctx context.Context, job *models.Job) error
	GetJobByID(ctx context.Context, id uuid.UUID) (*models.Job, error)
	GetJobByIdempotencyKey(ctx context.Context, jobType models.JobType, key string) (*models.Job, error)
}

// NewJobHandler creates a new job handler
func NewJobHandler(store JobStore, logger *log.Logger, idempotencyHeader string, maxKeyLength int) *JobHandler {
	return &JobHandler{
		store:              store,
		logger:             logger.WithHandler("job_handler"),
		idempotencyHeader:  idempotencyHeader,
		maxKeyLength:       maxKeyLength,
	}
}

// CreateJournalTranscribe handles POST /api/jobs/journal/transcribe
func (h *JobHandler) CreateJournalTranscribe(c *gin.Context) {
	h.createJob(c, models.JobTypeJournalTranscribe)
}

// CreateJournalAnalyze handles POST /api/jobs/journal/analyze
func (h *JobHandler) CreateJournalAnalyze(c *gin.Context) {
	h.createJob(c, models.JobTypeJournalAnalyze)
}

// CreateMTGAnalyze handles POST /api/jobs/mtg/analyze
func (h *JobHandler) CreateMTGAnalyze(c *gin.Context) {
	h.createJob(c, models.JobTypeMTGAnalyze)
}

func (h *JobHandler) createJob(c *gin.Context, jobType models.JobType) {
	ctx := c.Request.Context()

	// Extract idempotency key
	idempotencyKey := c.GetHeader(h.idempotencyHeader)
	if idempotencyKey != "" && len(idempotencyKey) > h.maxKeyLength {
		h.logger.Warn().
			Str("job_type", string(jobType)).
			Int("key_len", len(idempotencyKey)).
			Int("max_len", h.maxKeyLength).
			Msg("idempotency key too long")
		c.JSON(http.StatusBadRequest, gin.H{"error": "idempotency key too long"})
		return
	}

	// Parse payload
	var payload models.JSONB
	if err := c.ShouldBindJSON(&payload); err != nil {
		h.logger.Warn().Err(err).Str("job_type", string(jobType)).Msg("invalid JSON payload")
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid JSON payload"})
		return
	}

	// Validate required fields based on job type
	if err := h.validatePayload(jobType, payload); err != nil {
		h.logger.Warn().Err(err).Str("job_type", string(jobType)).Msg("payload validation failed")
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// Check for existing job with same idempotency key
	if idempotencyKey != "" {
		existing, err := h.store.GetJobByIdempotencyKey(ctx, jobType, idempotencyKey)
		if err != nil {
			h.logger.Error().Err(err).Str("job_type", string(jobType)).Msg("failed to check idempotency key")
			c.JSON(http.StatusInternalServerError, gin.H{"error": "internal error"})
			return
		}
		if existing != nil {
			// Return existing job (idempotent)
			h.logger.Info().
				Str("job_id", existing.ID.String()).
				Str("job_type", string(jobType)).
				Str("status", string(existing.Status)).
				Msg("returning existing job for idempotency key")
			c.JSON(http.StatusOK, gin.H{
				"job_id":     existing.ID,
				"status":     existing.Status,
				"created_at": existing.CreatedAt,
			})
			return
		}
	}

	// Create new job
	job := &models.Job{
		JobType:        jobType,
		Status:         models.JobStatusPending,
		Payload:        payload,
		IdempotencyKey: &idempotencyKey,
		MaxRetries:     3,
	}

	if err := h.store.CreateJob(ctx, job); err != nil {
		h.logger.Error().Err(err).Str("job_type", string(jobType)).Msg("failed to create job")
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to create job"})
		return
	}

	// Log job received event
	h.logger.LogJobEvent(ctx, models.EventReceived, map[string]interface{}{
		"job_type":         string(jobType),
		"idempotency_key":  idempotencyKey,
		"client_ip":        c.ClientIP(),
	})

	h.logger.Info().
		Str("job_id", job.ID.String()).
		Str("job_type", string(jobType)).
		Msg("job created")

	c.JSON(http.StatusAccepted, gin.H{
		"job_id":     job.ID,
		"status":     job.Status,
		"created_at": job.CreatedAt,
	})
}

// GetJob handles GET /api/jobs/:id
func (h *JobHandler) GetJob(c *gin.Context) {
	ctx := c.Request.Context()

	idStr := c.Param("id")
	id, err := uuid.Parse(idStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid job ID"})
		return
	}

	job, err := h.store.GetJobByID(ctx, id)
	if err != nil {
		h.logger.Error().Err(err).Str("job_id", idStr).Msg("failed to get job")
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal error"})
		return
	}

	if job == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "job not found"})
		return
	}

	response := gin.H{
		"job_id":      job.ID,
		"job_type":    job.JobType,
		"status":      job.Status,
		"payload":     job.Payload,
		"created_at":  job.CreatedAt,
		"updated_at":  job.UpdatedAt,
	}

	if job.Status == models.JobStatusCompleted {
		response["result"] = job.Result
		response["completed_at"] = job.CompletedAt
	} else if job.Status == models.JobStatusFailed || job.Status == models.JobStatusDeadLetter {
		response["error"] = job.Error
		response["attempt_count"] = job.AttemptCount
		response["max_retries"] = job.MaxRetries
		if job.NextRetryAt != nil {
			response["next_retry_at"] = job.NextRetryAt
		}
	}

	c.JSON(http.StatusOK, response)
}

// Health handles GET /health
func (h *JobHandler) Health(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"status":    "healthy",
		"timestamp": time.Now().UTC(),
		"service":   "ai-gateway",
	})
}

func (h *JobHandler) validatePayload(jobType models.JobType, payload models.JSONB) error {
	var m map[string]interface{}
	if err := payload.UnmarshalJSON(); err != nil {
		return err
	}

	switch jobType {
	case models.JobTypeJournalTranscribe:
		// Require at least one audio source
		hasAudio := false
		for _, key := range []string{"audio_url", "audio_path", "audio_base64"} {
			if v, ok := m[key]; ok && v != "" {
				hasAudio = true
				break
			}
		}
		if !hasAudio {
			return errors.New("journal_transcribe requires audio_url, audio_path, or audio_base64")
		}
	case models.JobTypeJournalAnalyze:
		if _, ok := m["transcription_text"]; !ok {
			return errors.New("journal_analyze requires transcription_text")
		}
	case models.JobTypeMTGAnalyze:
		hasImage := false
		for _, key := range []string{"image_url", "image_path", "image_base64"} {
			if v, ok := m[key]; ok && v != "" {
				hasImage = true
				break
			}
		}
		if !hasImage {
			return errors.New("mtg_analyze requires image_url, image_path, or image_base64")
		}
	}
	return nil
}