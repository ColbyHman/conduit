package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/conduit/pkg/log"
	"github.com/conduit/scheduler/internal/config"
	"github.com/conduit/scheduler/internal/store"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/joho/godotenv"
)

// The scheduler is enqueue-only (ADR 0007): each tick it claims due
// `schedules` rows and inserts a job into the existing `jobs` queue.
// It never executes job logic itself — existing workers pick jobs up
// through their normal poll loop, reusing the existing retry/dead-letter
// machinery (ADR 0004) for free.
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
	logger.Info().Str("version", "dev").Msg("starting Scheduler")

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

	scheduleStore := store.NewScheduleStore(pool, logger)

	logger.Info().Dur("poll_interval", cfg.Scheduler.PollInterval).Msg("scheduler started")

	ticker := time.NewTicker(cfg.Scheduler.PollInterval)
	defer ticker.Stop()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	for {
		select {
		case <-sigCh:
			logger.Info().Msg("shutdown signal received")
			return
		case <-ticker.C:
			tick(ctx, scheduleStore, logger)
		}
	}
}

// tick claims and fires every currently-due schedule row, not just one,
// so a slow poll interval doesn't starve other due schedules.
func tick(ctx context.Context, scheduleStore *store.ScheduleStore, logger *log.Logger) {
	for {
		sched, err := scheduleStore.ClaimDueSchedule(ctx)
		if err != nil {
			logger.Error().Err(err).Msg("failed to claim due schedule")
			return
		}
		if sched == nil {
			return // nothing due
		}
		logger.Info().
			Str("schedule_id", sched.ID.String()).
			Str("schedule_name", sched.Name).
			Str("job_type", string(sched.JobType)).
			Str("job_id", sched.LastJobID.String()).
			Time("next_run_at", sched.NextRunAt).
			Msg("schedule fired: job enqueued")
	}
}
