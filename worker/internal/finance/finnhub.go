package finance

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// FinnhubClient is a SECOND, OPTIONAL news source for finance_ingest
// (ADR 0015 scope item 3). Scaffolded now so dropping in a real
// FINNHUB_API_KEY later just works; when the key is empty the caller
// must skip this client entirely (see handler) — no error, no block.
type FinnhubClient struct {
	httpClient *http.Client
	apiKey     string
}

// NewFinnhubClient creates a client. Returns nil if apiKey is empty —
// callers should check for a nil client and skip the Finnhub call.
func NewFinnhubClient(apiKey string) *FinnhubClient {
	if apiKey == "" {
		return nil
	}
	return &FinnhubClient{
		httpClient: &http.Client{Timeout: 15 * time.Second},
		apiKey:     apiKey,
	}
}

// NewsItem is one normalized Finnhub company-news article.
type NewsItem struct {
	Headline string
	Summary  string
	URL      string
	Source   string
	DateUnix int64
}

// CompanyNews fetches recent company news for a ticker from Finnhub.
// Callers should treat an empty result as a non-error — not every
// ticker (especially non-equity asset types) will have coverage.
func (c *FinnhubClient) CompanyNews(ctx context.Context, ticker string, from, to string) ([]NewsItem, error) {
	if c == nil {
		return nil, nil
	}
	url := fmt.Sprintf("https://finnhub.io/api/v1/company-news?symbol=%s&from=%s&to=%s&token=%s",
		ticker, from, to, c.apiKey)
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, err
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch finnhub company news: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("finnhub company-news: status %d: %s", resp.StatusCode, string(body))
	}

	var raw []struct {
		Headline string `json:"headline"`
		Summary  string `json:"summary"`
		URL      string `json:"url"`
		Source   string `json:"source"`
		Datetime int64  `json:"datetime"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		return nil, fmt.Errorf("decode finnhub company-news: %w", err)
	}

	items := make([]NewsItem, 0, len(raw))
	for _, r := range raw {
		items = append(items, NewsItem{
			Headline: r.Headline,
			Summary:  r.Summary,
			URL:      r.URL,
			Source:   r.Source,
			DateUnix: r.Datetime,
		})
	}
	return items, nil
}
