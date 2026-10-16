package log

import (
	"context"
	"io"
	"os"
	"runtime"
	"strings"
	"time"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/pkgerrors"
	gopkg.in/natefinch/lumberjack.v2
)

// Context keys for structured logging
type contextKey string

const (
	jobIDKey     contextKey = "job_id"
	workerIDKey  contextKey = "worker_id"
	handlerKey   contextKey = "handler"
	attemptKey   contextKey = "attempt"
)

// Logger wraps zerolog with Conduit-specific helpers
type Logger struct {
	*zerolog.Logger
}

// Config holds logger configuration
type Config struct {
	Level      string // debug, info, warn, error
	Pretty     bool   // human-readable console output
	FilePath   string // empty = stdout only
	MaxSizeMB  int    // log rotation max size
	MaxBackups int    // rotated files to keep
	MaxAgeDays int    // days to keep rotated files
}

// DefaultConfig returns sensible defaults
func DefaultConfig() Config {
	return Config{
		Level:      "info",
		Pretty:     false,
		FilePath:   "",
		MaxSizeMB:  10,
		MaxBackups: 3,
		MaxAgeDays: 7,
	}
}

// New creates a new Conduit logger
func New(cfg Config) *Logger {
	zerolog.TimeFieldFormat = time.RFC3339Nano
	zerolog.ErrorStackMarshaler = pkgerrors.MarshalStack

	level, err := zerolog.ParseLevel(strings.ToLower(cfg.Level))
	if err != nil {
		level = zerolog.InfoLevel
	}
	zerolog.SetGlobalLevel(level)

	var output io.Writer = os.Stdout
	if cfg.FilePath != "" {
		output = &lumberjack.Logger{
			Filename:   cfg.FilePath,
			MaxSize:    cfg.MaxSizeMB,
			MaxBackups: cfg.MaxBackups,
			MaxAge:     cfg.MaxAgeDays,
			Compress:   true,
		}
	}

	if cfg.Pretty {
		output = zerolog.ConsoleWriter{Out: output, TimeFormat: time.RFC3339}
	}

	logger := zerolog.New(output).With().
		Timestamp().
		Str("service", "conduit").
		Str("go_version", runtime.Version()).
		Logger()

	return &Logger{Logger: &logger}
}

// WithJobID adds job_id to log context
func (l *Logger) WithJobID(jobID string) *Logger {
	return &Logger{Logger: l.Logger.With().Str("job_id", jobID).Logger()}
}

// WithWorkerID adds worker_id to log context
func (l *Logger) WithWorkerID(workerID string) *Logger {
	return &Logger{Logger: l.Logger.With().Str("worker_id", workerID).Logger()}
}

// WithHandler adds handler name to log context
func (l *Logger) WithHandler(handler string) *Logger {
	return &Logger{Logger: l.Logger.With().Str("handler", handler).Logger()}
}

// WithAttempt adds attempt number to log context
func (l *Logger) WithAttempt(attempt int) *Logger {
	return &Logger{Logger: l.Logger.With().Int("attempt", attempt).Logger()}
}

// FromContext extracts logger from context or returns background logger
func FromContext(ctx context.Context) *Logger {
	if logger, ok := ctx.Value(contextKey("logger")).(*Logger); ok {
		return logger
	}
	return &Logger{Logger: zerolog.Ctx(ctx)}
}

// ToContext adds logger to context
func ToContext(ctx context.Context, logger *Logger) context.Context {
	return context.WithValue(ctx, contextKey("logger"), logger)
}

// WithJobIDContext adds job_id to context for propagation
func WithJobIDContext(ctx context.Context, jobID string) context.Context {
	return context.WithValue(ctx, jobIDKey, jobID)
}

// JobIDFromContext extracts job_id from context
func JobIDFromContext(ctx context.Context) string {
	if v := ctx.Value(jobIDKey); v != nil {
		return v.(string)
	}
	return ""
}

// WithWorkerIDContext adds worker_id to context
func WithWorkerIDContext(ctx context.Context, workerID string) context.Context {
	return context.WithValue(ctx, workerIDKey, workerID)
}

// WorkerIDFromContext extracts worker_id from context
func WorkerIDFromContext(ctx context.Context) string {
	if v := ctx.Value(workerIDKey); v != nil {
		return v.(string)
	}
	return ""
}

// Event types for job_events table
const (
	EventReceived          = "received"
	EventClaimed           = "claimed"
	EventProcessingStart   = "processing_start"
	EventLLMRequest        = "llm_request"
	EventLLMResponse       = "llm_response"
	EventCompleted         = "completed"
	EventFailed            = "failed"
	EventRetryScheduled    = "retry_scheduled"
	EventDeadLetter        = "dead_letter"
)

// LogJobEvent logs a job lifecycle event with structured fields
func (l *Logger) LogJobEvent(ctx context.Context, eventType string, fields map[string]interface{}) {
	e := l.Info().
		Str("event_type", eventType).
		Str("job_id", JobIDFromContext(ctx))

	if workerID := WorkerIDFromContext(ctx); workerID != "" {
		e = e.Str("worker_id", workerID)
	}

	for k, v := range fields {
		switch val := v.(type) {
		case string:
			e = e.Str(k, val)
		case int:
			e = e.Int(k, val)
		case int64:
			e = e.Int64(k, val)
		case float64:
			e = e.Float64(k, val)
		case bool:
			e = e.Bool(k, val)
		case time.Duration:
			e = e.Dur(k, val)
		default:
			e = e.Interface(k, val)
		}
	}

	e.Msg("job event")
}