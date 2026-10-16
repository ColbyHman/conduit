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
	"github.com/conduit/worker/internal/config"
	"github.com/conduit/worker/internal/handler"
	"github.com/conduit/worker/internal/llm"
	"github.com/conduit/worker/internal/store"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/joho/godotenv"
)

func main() {
	// Load .env file if exists
	_ = godotenv.Load()

	// Load configuration
	cfgPath := os.Getenv("CONFIG_PATH")
	if cfgPath == "" {
		cfgPath = "config.yaml"
	}
	cfg, err := config.Load(cfgPath)
	if err != nil {
		panic(err)
	}

	// Initialize logger
	logger := log.New(cfg.Logging)
	logger.Info().Str("version", "dev").Msg("starting Worker")

	// Generate worker ID if not provided
	workerID := cfg.Worker.ID
	if workerID == "" {
		hostname, _ := os.Hostname()
		workerID = fmt.Sprintf("worker-%s-%d", hostname, os.Getpid())
	}
	logger = logger.WithWorkerID(workerID)

	// Connect to database
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

	// Initialize store
	jobStore := store.NewJobStore(pool, logger)

	// Initialize LLM client
	llmConfig := llm.ClientConfig{
		Provider:     cfg.LLM.Provider,
		Model:        cfg.LLM.Model,
		APIKey:       cfg.LLM.APIKey,
		Endpoint:     cfg.LLM.Endpoint,
		Temperature:  cfg.LLM.Temperature,
		MaxTokens:    cfg.LLM.MaxTokens,
		Timeout:      int64(cfg.LLM.Timeout.Seconds()),
		OllamaHost:   cfg.LLM.Ollama.Host,
	}

	llmClient, err := llm.Create(llmConfig)
	if err != nil {
		logger.Fatal().Err(err).Str("provider", cfg.LLM.Provider).Msg("failed to create LLM client")
	}
	defer llmClient.Close()
	logger.Info().Str("provider", cfg.LLM.Provider).Str("model", cfg.LLM.Model).Msg("LLM client initialized")

	// Initialize prompt loader
	prompts, err := handler.NewPromptLoader(cfg.Prompts.Directory, logger)
	if err != nil {
		logger.Fatal().Err(err).Msg("failed to load prompts")
	}
	logger.Info().Str("directory", cfg.Prompts.Directory).Msg("prompts loaded")

	// Register handlers
	journalHandler := handler.NewJournalAnalyzeHandler(jobStore, prompts, logger, cfg.LLM.MaxTokens)
	mtgHandler := handler.NewMTGAnalyzeHandler(jobStore, prompts, logger, cfg.LLM.MaxTokens)
	handler.Register(journalHandler)
	handler.Register(mtgHandler)

	// Convert job types
	jobTypes := make([]models.JobType, len(cfg.Worker.JobTypes))
	for i, jt := range cfg.Worker.JobTypes {
		jobTypes[i] = models.JobType(jt)
	}

	// Start worker loop
	logger.Info().
		Str("worker_id", workerID).
		Dur("poll_interval", cfg.Worker.PollInterval).
		Strs("job_types", cfg.Worker.JobTypes).
		Msg("worker started")

	// Cleanup stale jobs on startup
	if released, err := jobStore.CleanupStaleJobs(ctx, cfg.Worker.StaleLockThreshold); err != nil {
		logger.Warn().Err(err).Msg("failed to cleanup stale jobs")
	} else if released > 0 {
		logger.Info().Int64("released", released).Msg("cleaned up stale jobs")
	}

	// Main polling loop
	ticker := time.NewTicker(cfg.Worker.PollInterval)
	defer ticker.Stop()

	// Signal handling
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	for {
		select {
		case <-sigCh:
			logger.Info().Msg("shutdown signal received")
			return

		case <-ticker.C:
			// Claim job
			job, err := jobStore.ClaimJob(ctx, workerID, jobTypes)
			if err != nil {
				logger.Error().Err(err).Msg("failed to claim job")
				continue
			}
			if job == nil {
				// No jobs available
				continue
			}

			// Process job
			processJob(ctx, job, workerID, jobStore, llmClient, logger, cfg.Retry)
		}
	}
}

func processJob(
	ctx context.Context,
	job *models.Job,
	workerID string,
	jobStore *store.JobStore,
	llmClient llm.LLMClient,
	logger *log.Logger,
	retryCfg config.RetryConfig,
) {
	jobLogger := logger.WithJobID(job.ID.String()).WithAttempt(job.AttemptCount)

	// Add job_id to context for logging
	ctx = log.WithJobIDContext(ctx, job.ID.String())
	ctx = log.WithWorkerIDContext(ctx, workerID)

	jobLogger.LogJobEvent(ctx, models.EventClaimed, map[string]interface{}{
		"attempt": job.AttemptCount,
	})

	// Get handler
	jobHandler, ok := handler.Get(job.JobType)
	if !ok {
		errMsg := fmt.Sprintf("no handler for job type: %s", job.JobType)
		jobLogger.Error().Str("job_type", string(job.JobType)).Msg(errMsg)
		_ = jobStore.FailJob(ctx, job.ID, errMsg, job.AttemptCount, job.MaxRetries, retryCfg.BaseDelay, retryCfg.MaxDelay)
		return
	}

	// Handle job
	result, err := jobHandler.Handle(ctx, job, llmClient)
	if err != nil {
		errMsg := fmt.Sprintf("handler error: %v", err)
		jobLogger.Error().Err(err).Msg("job processing failed")
		_ = jobStore.FailJob(ctx, job.ID, errMsg, job.AttemptCount, job.MaxRetries, retryCfg.BaseDelay, retryCfg.MaxDelay)
		return
	}

	// Complete job
	if err := jobStore.CompleteJob(ctx, job.ID, *result); err != nil {
		jobLogger.Error().Err(err).Msg("failed to complete job")
		return
	}

	jobLogger.Info().Dur("duration", time.Since(time.Now())).Msg("job completed successfully")
}