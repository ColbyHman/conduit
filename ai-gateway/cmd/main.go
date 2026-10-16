package main

import (
	"context"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/conduit/ai-gateway/internal/config"
	"github.com/conduit/ai-gateway/internal/handler"
	"github.com/conduit/ai-gateway/internal/store"
	"github.com/conduit/pkg/log"
	"github.com/gin-gonic/gin"
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
	logger.Info().Str("version", "dev").Msg("starting AI Gateway")

	// Connect to database
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, cfg.Database.URL)
	if err != nil {
		logger.Fatal().Err(err).Msg("failed to connect to database")
	}
	defer pool.Close()

	// Test connection
	if err := pool.Ping(ctx); err != nil {
		logger.Fatal().Err(err).Msg("failed to ping database")
	}
	logger.Info().Msg("database connected")

	// Initialize store and handlers
	jobStore := store.NewJobStore(pool, logger)
	jobHandler := handler.NewJobHandler(
		jobStore,
		logger,
		cfg.Idempotency.HeaderName,
		cfg.Idempotency.MaxKeyLength,
	)

	// Setup Gin router
	if cfg.Logging.Level == "debug" {
		gin.SetMode(gin.DebugMode)
	} else {
		gin.SetMode(gin.ReleaseMode)
	}

	router := gin.New()
	router.Use(gin.Recovery())
	router.Use(loggingMiddleware(logger))

	// Routes
	router.GET("/health", jobHandler.Health)

	api := router.Group("/api")
	{
		jobs := api.Group("/jobs")
		{
			jobs.POST("/journal/transcribe", jobHandler.CreateJournalTranscribe)
			jobs.POST("/journal/analyze", jobHandler.CreateJournalAnalyze)
			jobs.POST("/mtg/analyze", jobHandler.CreateMTGAnalyze)
			jobs.GET("/:id", jobHandler.GetJob)
		}
	}

	// Start server
	srv := &http.Server{
		Addr:         ":" + string(rune(cfg.Server.Port)),
		Handler:      router,
		ReadTimeout:  cfg.Server.ReadTimeout,
		WriteTimeout: cfg.Server.WriteTimeout,
		IdleTimeout:  cfg.Server.IdleTimeout,
	}

	// Graceful shutdown
	go func() {
		logger.Info().Int("port", cfg.Server.Port).Msg("starting HTTP server")
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Fatal().Err(err).Msg("server failed")
		}
	}()

	// Wait for interrupt signal
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	logger.Info().Msg("shutting down server...")

	// Graceful shutdown with timeout
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := srv.Shutdown(shutdownCtx); err != nil {
		logger.Error().Err(err).Msg("server forced to shutdown")
	}

	logger.Info().Msg("server exited")
}

func loggingMiddleware(logger *log.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()
		path := c.Request.URL.Path
		query := c.Request.URL.RawQuery

		c.Next()

		logger.Info().
			Str("method", c.Request.Method).
			Str("path", path).
			Str("query", query).
			Int("status", c.Writer.Status()).
			Dur("latency", time.Since(start)).
			Str("client_ip", c.ClientIP()).
			Msg("HTTP request")
	}
}