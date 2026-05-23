// Package odoo wraps the Odoo 19.0 External JSON-2 API for the
// marketing importer. The JSON-2 surface replaces the legacy XML-RPC
// endpoint (`/xmlrpc/2/object`) that the design doc §5.4 originally
// targeted — Odoo 19 ships a REST-shaped JSON API that's cheaper to
// implement against and matches how the rest of the marketing
// importer pipeline already speaks (JSON throughout).
//
// Contract assumed (subject to confirmation against a live Odoo 19
// tenant during deployment):
//
//   - Base URL is operator-supplied (`https://my-tenant.odoo.com`).
//   - All requests POST to `/api/v2/<model>/<action>` with a JSON body.
//   - Auth headers: `X-API-Key: <api_key>` + `X-DB-Name: <database>`.
//   - Errors:
//     401 → invalid credentials → ErrOdooAuth (operator-visible 400).
//     404 → unknown model/action → ErrOdooNotFound.
//     5xx → exponential-backoff retry up to 3 attempts.
//   - SearchRead returns `{records: [...], totalCount: N}` shape.
//     Pagination via `offset` + `limit` (defaults: 200 per page).
//
// If the live Odoo 19 spec diverges (e.g. header names, body shape,
// pagination cursor instead of offset), only this file changes — the
// adapter + mapper interact through the SearchRead method only.
package odoo

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// ErrOdooAuth means the API key or database name was rejected by the
// Odoo tenant. The handler surfaces this as a 400 with an
// operator-actionable message ("check your API key + database in
// /admin/modules").
var ErrOdooAuth = errors.New("odoo: authentication failed")

// ErrOdooNotFound is returned when the JSON-2 API rejects the
// requested model or action (404). Surfaces as 400 at the handler;
// usually points to a misconfigured Odoo module (e.g. mail.message
// requires the Mail addon to be installed on the tenant).
var ErrOdooNotFound = errors.New("odoo: model or action not found")

// ErrOdooServer is returned after the retry budget is exhausted on
// 5xx responses. The caller (adapter Run) bubbles it up as the
// job's failure reason.
var ErrOdooServer = errors.New("odoo: server error")

// Config carries the per-import credentials. Persisted as encrypted
// secrets in module_configs (per environment); the handler hydrates
// the struct from the active environment before constructing the
// client.
type Config struct {
	BaseURL  string
	Database string
	APIKey   string

	// HTTPClient is optional; defaults to http.DefaultClient. Tests
	// inject httptest.NewServer's client here.
	HTTPClient *http.Client

	// RetryAttempts caps the 5xx retry budget. Default 3.
	RetryAttempts int
	// RetryBaseDelay is the first sleep on the exponential ladder.
	// Default 1s; the ladder is 1s, 2s, 4s.
	RetryBaseDelay time.Duration
}

// Client is the per-import handle. Stateless beyond the config; safe
// for concurrent use from multiple goroutines.
type Client struct {
	cfg Config
}

// NewClient validates the config + returns a ready-to-use client.
// Returns an error when required fields are empty so the adapter can
// fail fast with a clear 400 instead of waiting for the first request
// to roundtrip.
func NewClient(cfg Config) (*Client, error) {
	if cfg.BaseURL == "" {
		return nil, errors.New("odoo: base url is required")
	}
	if cfg.Database == "" {
		return nil, errors.New("odoo: database is required")
	}
	if cfg.APIKey == "" {
		return nil, errors.New("odoo: api key is required")
	}
	if cfg.HTTPClient == nil {
		cfg.HTTPClient = http.DefaultClient
	}
	if cfg.RetryAttempts <= 0 {
		cfg.RetryAttempts = 3
	}
	if cfg.RetryBaseDelay <= 0 {
		cfg.RetryBaseDelay = time.Second
	}
	cfg.BaseURL = strings.TrimRight(cfg.BaseURL, "/")
	return &Client{cfg: cfg}, nil
}

// SearchReadOpts parameterises a paginated read. Domain mirrors
// Odoo's filter DSL (a list of triplets); Fields is the explicit
// field allow-list (omit to return every field, but cost-effective
// imports always pass an explicit list).
type SearchReadOpts struct {
	Domain []any    `json:"domain,omitempty"`
	Fields []string `json:"fields,omitempty"`
	Offset int      `json:"offset,omitempty"`
	Limit  int      `json:"limit,omitempty"`
	Order  string   `json:"order,omitempty"`
}

// SearchReadResult is the JSON-2 read response. TotalCount drives
// the pagination loop; Records is a slice of raw maps the adapter
// projects via the model-specific mapper.
type SearchReadResult struct {
	Records    []map[string]any `json:"records"`
	TotalCount int              `json:"totalCount"`
}

// SearchRead issues one POST to /api/v2/<model>/search_read. The
// caller is responsible for the pagination loop (incrementing Offset
// until Records < Limit or TotalCount reached).
func (c *Client) SearchRead(model string, opts SearchReadOpts) (*SearchReadResult, error) {
	if opts.Limit <= 0 {
		opts.Limit = 200
	}
	body, err := json.Marshal(opts)
	if err != nil {
		return nil, fmt.Errorf("odoo: marshal request: %w", err)
	}
	path := fmt.Sprintf("/api/v2/%s/search_read", model)
	resp, err := c.doWithRetry(http.MethodPost, path, body)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var out SearchReadResult
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, fmt.Errorf("odoo: decode response: %w", err)
	}
	return &out, nil
}

// doWithRetry executes the HTTP request with the configured retry
// budget. 5xx responses retry; 4xx surface immediately (no retry
// would help a misconfigured request).
func (c *Client) doWithRetry(method, path string, body []byte) (*http.Response, error) {
	url := c.cfg.BaseURL + path

	var lastErr error
	delay := c.cfg.RetryBaseDelay
	for attempt := 1; attempt <= c.cfg.RetryAttempts; attempt++ {
		req, err := http.NewRequest(method, url, bytes.NewReader(body))
		if err != nil {
			return nil, fmt.Errorf("odoo: build request: %w", err)
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-API-Key", c.cfg.APIKey)
		req.Header.Set("X-DB-Name", c.cfg.Database)

		resp, err := c.cfg.HTTPClient.Do(req)
		if err != nil {
			lastErr = fmt.Errorf("odoo: http call: %w", err)
			if attempt < c.cfg.RetryAttempts {
				time.Sleep(delay)
				delay *= 2
				continue
			}
			return nil, lastErr
		}

		switch {
		case resp.StatusCode >= 200 && resp.StatusCode < 300:
			return resp, nil
		case resp.StatusCode == http.StatusUnauthorized:
			drainAndClose(resp)
			return nil, ErrOdooAuth
		case resp.StatusCode == http.StatusNotFound:
			drainAndClose(resp)
			return nil, ErrOdooNotFound
		case resp.StatusCode >= 500:
			snippet := readSnippet(resp)
			drainAndClose(resp)
			lastErr = fmt.Errorf("%w: %d %s", ErrOdooServer, resp.StatusCode, snippet)
			if attempt < c.cfg.RetryAttempts {
				time.Sleep(delay)
				delay *= 2
				continue
			}
			return nil, lastErr
		default:
			snippet := readSnippet(resp)
			drainAndClose(resp)
			return nil, fmt.Errorf("odoo: unexpected status %d: %s", resp.StatusCode, snippet)
		}
	}
	return nil, lastErr
}

func drainAndClose(resp *http.Response) {
	if resp == nil {
		return
	}
	_, _ = io.Copy(io.Discard, resp.Body)
	_ = resp.Body.Close()
}

func readSnippet(resp *http.Response) string {
	if resp == nil {
		return ""
	}
	b, err := io.ReadAll(io.LimitReader(resp.Body, 1<<10))
	if err != nil {
		return ""
	}
	return string(b)
}
