package models

import (
	"database/sql/driver"
	"encoding/json"
	"errors"
	"time"

	"github.com/google/uuid"
)

// JobType represents the type of job
type JobType string

const (
	JobTypeJournalTranscribe JobType = "journal_transcribe"
	JobTypeJournalAnalyze    JobType = "journal_analyze"
	JobTypeMTGAnalyze        JobType = "mtg_analyze"
)

// JobStatus represents the status of a job
type JobStatus string

const (
	JobStatusPending      JobStatus = "pending"
	JobStatusProcessing   JobStatus = "processing"
	JobStatusCompleted    JobStatus = "completed"
	JobStatusFailed       JobStatus = "failed"
	JobStatusDeadLetter   JobStatus = "dead_letter"
)

// Job represents a job in the queue
type Job struct {
	ID              uuid.UUID  `json:"id" db:"id"`
	JobType         JobType    `json:"job_type" db:"job_type"`
	Status          JobStatus  `json:"status" db:"status"`
	Payload         JSONB      `json:"payload" db:"payload"`
	Result          *JSONB     `json:"result,omitempty" db:"result"`
	Error           *string    `json:"error,omitempty" db:"error"`
	IdempotencyKey  *string    `json:"idempotency_key,omitempty" db:"idempotency_key"`
	AttemptCount    int        `json:"attempt_count" db:"attempt_count"`
	MaxRetries      int        `json:"max_retries" db:"max_retries"`
	NextRetryAt     *time.Time `json:"next_retry_at,omitempty" db:"next_retry_at"`
	LockedAt        *time.Time `json:"locked_at,omitempty" db:"locked_at"`
	LockedBy        *string    `json:"locked_by,omitempty" db:"locked_by"`
	CreatedAt       time.Time  `json:"created_at" db:"created_at"`
	UpdatedAt       time.Time  `json:"updated_at" db:"updated_at"`
	CompletedAt     *time.Time `json:"completed_at,omitempty" db:"completed_at"`
}

// IsTerminal returns true if the job is in a terminal state
func (j *Job) IsTerminal() bool {
	return j.Status == JobStatusCompleted || j.Status == JobStatusDeadLetter
}

// CanRetry returns true if the job can be retried
func (j *Job) CanRetry() bool {
	return j.Status == JobStatusFailed && j.AttemptCount < j.MaxRetries
}

// JSONB is a wrapper for json.RawMessage that implements driver.Valuer and sql.Scanner
type JSONB json.RawMessage

// Value implements driver.Valuer for database storage
func (j JSONB) Value() (driver.Value, error) {
	if len(j) == 0 {
		return nil, nil
	}
	return json.RawMessage(j).MarshalJSON()
}

// Scan implements sql.Scanner for database retrieval
func (j *JSONB) Scan(value interface{}) error {
	if value == nil {
		*j = JSONB(`null`)
		return nil
	}
	bytes, ok := value.([]byte)
	if !ok {
		return errors.New("type assertion to []byte failed")
	}
	*j = JSONB(bytes)
	return nil
}

// MarshalJSON implements json.Marshaler
func (j JSONB) MarshalJSON() ([]byte, error) {
	if len(j) == 0 {
		return []byte("null"), nil
	}
	return json.RawMessage(j).MarshalJSON()
}

// UnmarshalJSON implements json.Unmarshaler
func (j *JSONB) UnmarshalJSON(data []byte) error {
	if j == nil {
		return errors.New("JSONB: UnmarshalJSON on nil pointer")
	}
	*j = JSONB(data)
	return nil
}

// String returns the JSON string representation
func (j JSONB) String() string {
	return json.RawMessage(j).String()
}

// JournalTranscribePayload is the payload for journal_transcribe jobs
type JournalTranscribePayload struct {
	AudioURL     string  `json:"audio_url,omitempty"`      // Presigned S3 URL or HTTP URL
	AudioPath    string  `json:"audio_path,omitempty"`     // Local file path (dev)
	AudioBase64  string  `json:"audio_base64,omitempty"`   // Base64 encoded audio (small files)
	Model        string  `json:"model,omitempty"`          // Whisper model (tiny, base, small, medium, large-v3)
	Language     string  `json:"language,omitempty"`       // Language code (en, es, etc.) or empty for auto
	ResponseFormat string `json:"response_format,omitempty"` // json, verbose_json, text, srt, vtt
}

// JournalAnalyzePayload is the payload for journal_analyze jobs
type JournalAnalyzePayload struct {
	TranscriptionText string `json:"transcription_text"`    // Text from Whisper
	Segments          []Segment `json:"segments,omitempty"`  // Word-level segments from Whisper
	Language          string `json:"language,omitempty"`    // Detected language
	Duration          float64 `json:"duration,omitempty"`    // Audio duration in seconds
	EntryType         string `json:"entry_type,omitempty"`  // journal, idea, thought
	SourceReference   string `json:"source_reference,omitempty"` // Original audio file reference
}

// Segment represents a Whisper segment with timestamps
type Segment struct {
	ID        int     `json:"id"`
	Seek      int     `json:"seek"`
	Start     float64 `json:"start"`
	End       float64 `json:"end"`
	Text      string  `json:"text"`
	Tokens    []int   `json:"tokens"`
	Temperature float64 `json:"temperature"`
	AvgLogprob float64 `json:"avg_logprob"`
	CompressionRatio float64 `json:"compression_ratio"`
	NoSpeechProb float64 `json:"no_speech_prob"`
}

// JournalAnalyzeResult is the result of journal_analyze job
type JournalAnalyzeResult struct {
	Summary   string         `json:"summary"`
	Analysis  JournalAnalysis `json:"analysis"`
	EntryType string         `json:"entry_type"`
}

// JournalAnalysis contains structured analysis of a journal entry
type JournalAnalysis struct {
	Mood            string   `json:"mood,omitempty"`
	Topics          []string `json:"topics,omitempty"`
	KeyInsights     []string `json:"key_insights,omitempty"`
	ActionItems     []string `json:"action_items,omitempty"`
	SentimentScore  float64  `json:"sentiment_score,omitempty"` // -1 to 1
	WordCount       int      `json:"word_count,omitempty"`
	DurationMinutes float64  `json:"duration_minutes,omitempty"`
}

// MTGAnalyzePayload is the payload for mtg_analyze jobs
type MTGAnalyzePayload struct {
	ImageURL   string `json:"image_url,omitempty"`   // HTTP/HTTPS URL to card image
	ImagePath  string `json:"image_path,omitempty"`  // Local file path (dev)
	ImageBase64 string `json:"image_base64,omitempty"` // Base64 encoded image
}

// MTGAnalyzeResult is the result of mtg_analyze job
type MTGAnalyzeResult struct {
	CardName   string         `json:"card_name"`
	Analysis   MTGAnalysis    `json:"analysis"`
	RawResponse string        `json:"raw_response,omitempty"`
}

// MTGAnalysis contains structured analysis of an MTG card
type MTGAnalysis struct {
	ManaCost      string   `json:"mana_cost,omitempty"`
	TypeLine      string   `json:"type_line,omitempty"`
	OracleText    string   `json:"oracle_text,omitempty"`
	PowerToughness string  `json:"power_toughness,omitempty"`
	Colors        []string `json:"colors,omitempty"`
	ColorIdentity []string `json:"color_identity,omitempty"`
	Rarity        string   `json:"rarity,omitempty"`
	SetCode       string   `json:"set_code,omitempty"`
	CollectorNumber string `json:"collector_number,omitempty"`
	PriceUSD      float64  `json:"price_usd,omitempty"`
	Legalities    map[string]string `json:"legalities,omitempty"`
	Keywords      []string `json:"keywords,omitempty"`
}

// JobEvent represents an event in the job lifecycle
type JobEvent struct {
	ID        int64   `json:"id" db:"id"`
	JobID     uuid.UUID `json:"job_id" db:"job_id"`
	EventType string  `json:"event_type" db:"event_type"`
	WorkerID  *string `json:"worker_id,omitempty" db:"worker_id"`
	Payload   *JSONB  `json:"payload,omitempty" db:"payload"`
	CreatedAt time.Time `json:"created_at" db:"created_at"`
}

// Entry represents a journal entry
type Entry struct {
	ID              uuid.UUID `json:"id" db:"id"`
	EntryType       string    `json:"entry_type" db:"entry_type"`
	Title           *string   `json:"title,omitempty" db:"title"`
	Content         string    `json:"content" db:"content"`
	Summary         *string   `json:"summary,omitempty" db:"summary"`
	Analysis        *JSONB    `json:"analysis,omitempty" db:"analysis"`
	SourceReference *string   `json:"source_reference,omitempty" db:"source_reference"`
	JobID           *uuid.UUID `json:"job_id,omitempty" db:"job_id"`
	CreatedAt       time.Time `json:"created_at" db:"created_at"`
	UpdatedAt       time.Time `json:"updated_at" db:"updated_at"`
}

// MagicCard represents an analyzed MTG card
type MagicCard struct {
	ID              uuid.UUID `json:"id" db:"id"`
	ImageURL        *string   `json:"image_url,omitempty" db:"image_url"`
	ImagePath       *string   `json:"image_path,omitempty" db:"image_path"`
	CardName        *string   `json:"card_name,omitempty" db:"card_name"`
	Analysis        *JSONB    `json:"analysis,omitempty" db:"analysis"`
	RawResponse     *string   `json:"raw_response,omitempty" db:"raw_response"`
	JobID           *uuid.UUID `json:"job_id,omitempty" db:"job_id"`
	CreatedAt       time.Time `json:"created_at" db:"created_at"`
	UpdatedAt       time.Time `json:"updated_at" db:"updated_at"`
}