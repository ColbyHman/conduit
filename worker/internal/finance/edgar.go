// Package finance provides HTTP clients for the finance_ingest job
// handler (ADR 0015): SEC EDGAR (required, no key) and Finnhub
// (optional, additive news source).
package finance

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"
)

// EdgarClient fetches recent filings from SEC EDGAR's data.sec.gov.
// SEC requires every request to carry a descriptive User-Agent with
// contact info, and asks callers to self-rate-limit (we cap at well
// under the documented 10 req/sec ceiling).
type EdgarClient struct {
	httpClient *http.Client
	userAgent  string
	limiter    *rateLimiter
}

// NewEdgarClient creates a client. userAgent MUST include contact info
// per SEC's fair-access policy, e.g. "Conduit/finance-ingest (ops@example.com)".
func NewEdgarClient(userAgent string) *EdgarClient {
	if userAgent == "" {
		userAgent = "Conduit-finance-ingest/0.1 (contact: ops@conduit.local)"
	}
	return &EdgarClient{
		httpClient: &http.Client{Timeout: 20 * time.Second},
		userAgent:  userAgent,
		limiter:    newRateLimiter(5), // well under SEC's 10 req/sec cap
	}
}

// CompanyTicker is one entry from EDGAR's company_tickers.json lookup.
type CompanyTicker struct {
	CIK     int    `json:"cik_str"`
	Ticker  string `json:"ticker"`
	Title   string `json:"title"`
}

// LookupTicker validates a ticker against EDGAR's company_tickers.json
// and returns its zero-padded CIK, used both by finance_ingest and by
// the gateway's POST /finance/holdings validation step.
func (c *EdgarClient) LookupTicker(ctx context.Context, ticker string) (cik string, companyName string, found bool, err error) {
	c.limiter.wait()
	req, err := http.NewRequestWithContext(ctx, "GET", "https://www.sec.gov/files/company_tickers.json", nil)
	if err != nil {
		return "", "", false, err
	}
	req.Header.Set("User-Agent", c.userAgent)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", "", false, fmt.Errorf("fetch company_tickers.json: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return "", "", false, fmt.Errorf("company_tickers.json: status %d: %s", resp.StatusCode, string(body))
	}

	var data map[string]CompanyTicker
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		return "", "", false, fmt.Errorf("decode company_tickers.json: %w", err)
	}

	want := strings.ToUpper(strings.TrimSpace(ticker))
	for _, entry := range data {
		if strings.ToUpper(entry.Ticker) == want {
			return fmt.Sprintf("%010d", entry.CIK), entry.Title, true, nil
		}
	}
	return "", "", false, nil
}

// Filing is one normalized recent filing for a ticker.
type Filing struct {
	AccessionNumber string
	Form            string // 10-K, 10-Q, 8-K, 4, etc.
	FilingDate      string
	PrimaryDocument string
	URL             string
}

// RecentFilings fetches the N most recent filings for a ticker via
// EDGAR's submissions API (data.sec.gov/submissions/CIK##########.json).
// Returns an empty slice (not an error) if the ticker has no CIK match
// or no filings — treasury/index tickers often fall in this bucket
// (ADR 0015 scope note), and finance_ingest must complete gracefully.
func (c *EdgarClient) RecentFilings(ctx context.Context, ticker string, limit int) ([]Filing, error) {
	cik, _, found, err := c.LookupTicker(ctx, ticker)
	if err != nil {
		return nil, err
	}
	if !found {
		return nil, nil
	}

	c.limiter.wait()
	url := fmt.Sprintf("https://data.sec.gov/submissions/CIK%s.json", cik)
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", c.userAgent)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch submissions: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return nil, nil
	}
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("submissions: status %d: %s", resp.StatusCode, string(body))
	}

	var sub struct {
		Filings struct {
			Recent struct {
				AccessionNumber []string `json:"accessionNumber"`
				Form            []string `json:"form"`
				FilingDate      []string `json:"filingDate"`
				PrimaryDocument []string `json:"primaryDocument"`
			} `json:"recent"`
		} `json:"filings"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&sub); err != nil {
		return nil, fmt.Errorf("decode submissions: %w", err)
	}

	cikNoPad := strings.TrimLeft(cik, "0")
	var filings []Filing
	for i := 0; i < len(sub.Filings.Recent.AccessionNumber) && len(filings) < limit; i++ {
		accNoDashes := strings.ReplaceAll(sub.Filings.Recent.AccessionNumber[i], "-", "")
		docURL := fmt.Sprintf("https://www.sec.gov/Archives/edgar/data/%s/%s/%s",
			cikNoPad, accNoDashes, sub.Filings.Recent.PrimaryDocument[i])
		filings = append(filings, Filing{
			AccessionNumber: sub.Filings.Recent.AccessionNumber[i],
			Form:            sub.Filings.Recent.Form[i],
			FilingDate:      sub.Filings.Recent.FilingDate[i],
			PrimaryDocument: sub.Filings.Recent.PrimaryDocument[i],
			URL:             docURL,
		})
	}
	return filings, nil
}

// rateLimiter is a simple token-interval limiter so finance_ingest
// self-rate-limits well under SEC's documented 10 req/sec ceiling.
type rateLimiter struct {
	mu       sync.Mutex
	last     time.Time
	minDelay time.Duration
}

func newRateLimiter(perSecond int) *rateLimiter {
	return &rateLimiter{minDelay: time.Second / time.Duration(perSecond)}
}

func (r *rateLimiter) wait() {
	r.mu.Lock()
	defer r.mu.Unlock()
	elapsed := time.Since(r.last)
	if elapsed < r.minDelay {
		time.Sleep(r.minDelay - elapsed)
	}
	r.last = time.Now()
}
