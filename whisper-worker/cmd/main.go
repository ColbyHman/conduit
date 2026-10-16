package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/conduit/pkg/log"
	"github.com/conduit/pkg/models"
	"github.com/conduit/whisper-worker/internal/config"
	"github.com/conduit/whisper-worker/internal/store"
	"github.com/conduit/whisper-worker/internal/whisper"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/joho/godotenv"
)

func main() {
	_ = godotenv.Load()

	cfgPath := os.Getenv("CONFIG_PATH")
	if cfgPath == "" {
		cfgPath = "config.yaml"
	}
	cfg, err := config.Load(cfgPath)
	if err != nil {
		panic(err)
	}

	logger := log.New(cfg.Logging)
	logger.Info().Str("version", "dev").Msg("starting Whisper Worker")

	workerID := cfg.Worker.ID
	if workerID == "" {
		hostname, _ := os.Hostname()
		workerID = fmt.Sprintf("whisper-worker-%s-%d", hostname, os.Getpid())
	}
	logger = logger.WithWorkerID(workerID)

	ctx := context.Background()
	pool, err := pgxpool.New(ctx, cfg.Database.URL)
	if err != nil {
		logger.Fatal().Err(err).Msg("failed to connect to database")
	}
	defer pool.Close()

	if err := pool.Ping(ctx); err != nil {
		logger.Fatal().Err(err).Msg("failed to ping database")
	}
	logger.Info().Msg("database connected")

	jobStore := store.NewJobStore(pool, logger)

	whisperClient := whisper.NewClient(cfg.Whisper.URL, cfg.Whisper.Timeout)
	logger.Info().Str("whisper_url", cfg.Whisper.URL).Msg("Whisper client initialized")

	// Health check Whisper
	if err := whisperClient.Health(ctx); err != nil {
		logger.Warn().Err(err).Msg("Whisper health check failed, continuing anyway")
	} else {
		logger.Info().Msg("Whisper API healthy")
	}

	jobTypes := make([]models.JobType, len(cfg.Worker.JobTypes))
	for i, jt := range cfg.Worker.JobTypes {
		jobTypes[i] = models.JobType(jt)
	}

	logger.Info().
		Str("worker_id", workerID).
		Dur("poll_interval", cfg.Worker.PollInterval).
		Strs("job_types", cfg.Worker.JobTypes).
		Msg("whisper worker started")

	if released, err := jobStore.CleanupStaleJobs(ctx, cfg.Worker.StaleLockThreshold); err != nil {
		logger.Warn().Err(err).Msg("failed to cleanup stale jobs")
	} else if released > 0 {
		logger.Info().Int64("released", released).Msg("cleaned up stale jobs")
	}

	ticker := time.NewTicker(cfg.Worker.PollInterval)
	defer ticker.Stop()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	for {
		select {
		case <-sigCh:
			logger.Info().Msg("shutdown signal received")
			return

		case <-ticker.C:
			job, err := jobStore.ClaimJob(ctx, workerID, jobTypes)
			if err != nil {
				logger.Error().Err(err).Msg("failed to claim job")
				continue
			}
			if job == nil {
				continue
			}

			processJob(ctx, job, workerID, jobStore, whisperClient, logger, cfg.Whisper, cfg.Retry)
		}
	}
}

func processJob(
	ctx context.Context,
	job *models.Job,
	workerID string,
	jobStore *store.JobStore,
	whisperClient *whisper.Client,
	logger *log.Logger,
	whisperCfg config.WhisperConfig,
	retryCfg config.RetryConfig,
) {
	jobLogger := logger.WithJobID(job.ID.String()).WithAttempt(job.AttemptCount)
	ctx = log.WithJobIDContext(ctx, job.ID.String())
	ctx = log.WithWorkerIDContext(ctx, workerID)

	jobLogger.LogJobEvent(ctx, models.EventClaimed, map[string]interface{}{
		"attempt": job.AttemptCount,
	})

	var payload models.JournalTranscribePayload
	if err := job.Payload.UnmarshalJSON(); err != nil {
		errMsg := fmt.Sprintf("unmarshal payload: %v", err)
		jobLogger.Error().Err(err).Msg(errMsg)
		_ = jobStore.FailJob(ctx, job.ID, errMsg, job.AttemptCount, job.MaxRetries, retryCfg.BaseDelay, retryCfg.MaxDelay)
		return
	}

	// Prepare transcription request
	req := whisper.TranscribeRequest{
		Model:          payload.Model,
		Language:       payload.Language,
		ResponseFormat: payload.ResponseFormat,
	}

	if req.Model == "" {
		req.Model = whisperCfg.DefaultModel
	}
	if req.ResponseFormat == "" {
		req.ResponseFormat = whisperCfg.DefaultResponseFormat
	}
	if req.Language == "" {
		req.Language = whisperCfg.DefaultLanguage
	}

	// Transcribe based on audio source
	var result *whisper.TranscribeResponse
	var err error

	switch {
	case payload.AudioURL != "":
		result, err = whisperClient.TranscribeFromURL(ctx, payload.AudioURL, req)
	case payload.AudioPath != "":
		req.FilePath = payload.AudioPath
		result, err = whisperClient.Transcribe(ctx, req)
	case payload.AudioBase64 != "":
		result, err = whisperClient.TranscribeFromBase64(ctx, payload.AudioBase64, req)
	default:
		err = errors.New("no audio source provided")
	}

	if err != nil {
		errMsg := fmt.Sprintf("transcription failed: %v", err)
		jobLogger.Error().Err(err).Msg(errMsg)
		_ = jobStore.FailJob(ctx, job.ID, errMsg, job.AttemptCount, job.MaxRetries, retryCfg.BaseDelay, retryCfg.MaxDelay)
		return
	}

	// Build result
	transcribeResult := map[string]interface{}{
		"text":      result.Text,
		"language":  result.Language,
		"duration":  result.Duration,
		"segments":  result.Segments,
	}

	resultJSON, err := json.Marshal(transcribeResult)
	if err != nil {
		errMsg := fmt.Sprintf("marshal result: %v", err)
		jobLogger.Error().Err(err).Msg(errMsg)
		_ = jobStore.FailJob(ctx, job.ID, errMsg, job.AttemptCount, job.MaxRetries, retryCfg.BaseDelay, retryCfg.MaxDelay)
		return
	}

	jsonb := models.JSONB(resultJSON)
	if err := jobStore.CompleteJob(ctx, job.ID, jsonb); err != nil {
		jobLogger.Error().Err(err).Msg("failed to complete job")
		return
	}

	jobLogger.LogJobEvent(ctx, models.EventCompleted, map[string]interface{}{
		"handler":      "journal_transcribe",
		"duration_ms":  0, // Could track actual duration
		"text_length":  len(result.Text),
		"segments":     len(result.Segments),
		"language":     result.Language,
	})

	jobLogger.Info().
		Str("language", result.Language).
		Float64("duration", result.Duration).
		Int("segments", len(result.Segments)).
		Msg("transcription completed")
}