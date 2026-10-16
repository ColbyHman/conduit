package whisper

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"time"
)

// Client calls the Whisper API for transcription
type Client struct {
	baseURL    string
	httpClient *http.Client
}

// TranscribeRequest represents a transcription request
type TranscribeRequest struct {
	File              *os.File
	FilePath          string
	FileName          string
	Model             string
	Language          string
	ResponseFormat    string
	Temperature       float64
	Prompt            string
}

// TranscribeResponse represents the Whisper API response (verbose_json)
type TranscribeResponse struct {
	Text     string    `json:"text"`
	Segments []Segment `json:"segments,omitempty"`
	Language string    `json:"language,omitempty"`
	Duration float64   `json:"duration,omitempty"`
}

// Segment represents a transcription segment with timestamps
type Segment struct {
	ID           int     `json:"id"`
	Seek         int     `json:"seek"`
	Start        float64 `json:"start"`
	End          float64 `json:"end"`
	Text         string  `json:"text"`
	Tokens       []int   `json:"tokens,omitempty"`
	Temperature  float64 `json:"temperature,omitempty"`
	AvgLogprob   float64 `json:"avg_logprob,omitempty"`
	CompressionRatio float64 `json:"compression_ratio,omitempty"`
	NoSpeechProb float64 `json:"no_speech_prob,omitempty"`
}

// NewClient creates a new Whisper client
func NewClient(baseURL string, timeout time.Duration) *Client {
	return &Client{
		baseURL: baseURL,
		httpClient: &http.Client{
			Timeout: timeout,
		},
	}
}

// Transcribe sends audio to Whisper API and returns transcription
func (c *Client) Transcribe(ctx context.Context, req TranscribeRequest) (*TranscribeResponse, error) {
	// Prepare file
	var file *os.File
	var fileName string
	var err error

	if req.File != nil {
		file = req.File
		fileName = req.FileName
		if fileName == "" {
			fileName = filepath.Base(file.Name())
		}
	} else if req.FilePath != "" {
		file, err = os.Open(req.FilePath)
		if err != nil {
			return nil, fmt.Errorf("open file: %w", err)
		}
		defer file.Close()
		fileName = req.FileName
		if fileName == "" {
			fileName = filepath.Base(req.FilePath)
		}
	} else {
		return nil, errors.New("no file provided")
	}

	// Build multipart form
	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)

	// Add file
	part, err := writer.CreateFormFile("file", fileName)
	if err != nil {
		return nil, fmt.Errorf("create form file: %w", err)
	}
	if _, err := io.Copy(part, file); err != nil {
		return nil, fmt.Errorf("copy file: %w", err)
	}

	// Add parameters
	if req.Model != "" {
		writer.WriteField("model", req.Model)
	}
	if req.Language != "" {
		writer.WriteField("language", req.Language)
	}
	if req.ResponseFormat != "" {
		writer.WriteField("response_format", req.ResponseFormat)
	}
	if req.Temperature > 0 {
		writer.WriteField("temperature", fmt.Sprintf("%f", req.Temperature))
	}
	if req.Prompt != "" {
		writer.WriteField("prompt", req.Prompt)
	}

	writer.Close()

	// Create request
	endpoint := c.baseURL + "/transcribe"
	httpReq, err := http.NewRequestWithContext(ctx, "POST", endpoint, body)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}

	httpReq.Header.Set("Content-Type", writer.FormDataContentType())

	// Send request
	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("do request: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("Whisper API error %d: %s", resp.StatusCode, string(respBody))
	}

	// Parse response based on format
	var result TranscribeResponse
	if req.ResponseFormat == "verbose_json" || req.ResponseFormat == "" {
		if err := json.Unmarshal(respBody, &result); err != nil {
			return nil, fmt.Errorf("unmarshal response: %w", err)
		}
	} else {
		// For text, json, srt, vtt formats - just return text
		result.Text = string(respBody)
	}

	return &result, nil
}

// TranscribeFromURL downloads audio from URL and transcribes
func (c *Client) TranscribeFromURL(ctx context.Context, audioURL string, req TranscribeRequest) (*TranscribeResponse, error) {
	// Download file
	httpReq, err := http.NewRequestWithContext(ctx, "GET", audioURL, nil)
	if err != nil {
		return nil, fmt.Errorf("create download request: %w", err)
	}

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("download audio: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("download failed: %d", resp.StatusCode)
	}

	// Save to temp file
	tmpFile, err := os.CreateTemp("", "whisper-*.m4a")
	if err != nil {
		return nil, fmt.Errorf("create temp file: %w", err)
	}
	defer os.Remove(tmpFile.Name())

	if _, err := io.Copy(tmpFile, resp.Body); err != nil {
		return nil, fmt.Errorf("save audio: %w", err)
	}
	tmpFile.Close()

	req.FilePath = tmpFile.Name()
	return c.Transcribe(ctx, req)
}

// TranscribeFromBase64 decodes base64 audio and transcribes
func (c *Client) TranscribeFromBase64(ctx context.Context, base64Audio string, req TranscribeRequest) (*TranscribeResponse, error) {
	// This would need base64 decoding - simplified for now
	return nil, errors.New("base64 transcription not yet implemented")
}

// Health checks if Whisper API is reachable
func (c *Client) Health(ctx context.Context) error {
	healthURL := c.baseURL
	if u, err := url.Parse(c.baseURL); err == nil {
		healthURL = u.Scheme + "://" + u.Host + "/health"
	}

	req, err := http.NewRequestWithContext(ctx, "GET", healthURL, nil)
	if err != nil {
		return err
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("health check failed: %d", resp.StatusCode)
	}
	return nil
}