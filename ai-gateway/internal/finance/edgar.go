// Package finance provides the SEC EDGAR ticker-lookup client used by
// ai-gateway's POST /finance/holdings validation step (ADR 0015).
// Mirrors worker/internal/finance's EDGAR client (separate module,
// can't share the package directly, so this is the minimal subset the
// gateway needs: validate a ticker exists before queuing work for it).
package finance

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// EdgarClient validates tickers against SEC EDGAR's company_tickers.json.
type EdgarClient struct {
	httpClient *http.Client
	userAgent  string
}

// NewEdgarClient creates a client. userAgent MUST include contact info
// per SEC's fair-access policy.
func NewEdgarClient(userAgent string) *EdgarClient {
	if userAgent == "" {
		userAgent = "Conduit-ai-gateway/0.1 (contact: ops@conduit.local)"
	}
	return &EdgarClient{
		httpClient: &http.Client{Timeout: 15 * time.Second},
		userAgent:  userAgent,
	}
}

type companyTicker struct {
	CIK    int    `json:"cik_str"`
	Ticker string `json:"ticker"`
	Title  string `json:"title"`
}

// LookupTicker returns (companyName, found, error). A typo'd ticker
// returns found=false, not an error, so the handler can turn it into
// a plain-language 400 response.
func (c *EdgarClient) LookupTicker(ctx context.Context, ticker string) (companyName string, found bool, err error) {
	req, err := http.NewRequestWithContext(ctx, "GET", "https://www.sec.gov/files/company_tickers.json", nil)
	if err != nil {
		return "", false, err
	}
	req.Header.Set("User-Agent", c.userAgent)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", false, fmt.Errorf("fetch company_tickers.json: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return "", false, fmt.Errorf("company_tickers.json: status %d: %s", resp.StatusCode, string(body))
	}

	var data map[string]companyTicker
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		return "", false, fmt.Errorf("decode company_tickers.json: %w", err)
	}

	want := strings.ToUpper(strings.TrimSpace(ticker))
	for _, entry := range data {
		if strings.ToUpper(entry.Ticker) == want {
			return entry.Title, true, nil
		}
	}
	return "", false, nil
}
