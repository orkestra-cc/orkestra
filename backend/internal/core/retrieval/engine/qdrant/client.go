// Package qdrant is the Qdrant driver of the retrieval engine contract
// (ADR-0023): REST over net/http, Qdrant >= 1.20. The conformance suite in
// enginetest defines its behaviour.
package qdrant

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/orkestra/backend/internal/core/retrieval/engine"
)

const (
	denseVector = "dense"
	textField   = "_rt.text"

	maxPointsPerRequest = 256
	maxQueryLimit       = 500

	minVersionMajor, minVersionMinor = 1, 20

	defaultTimeout          = 10 * time.Second
	defaultMaxResponseBytes = 16 << 20
)

// Config configures a Driver.
type Config struct {
	URL              string
	APIKey           string
	Timeout          time.Duration // per call; 0 = 10s; the caller's earlier deadline wins
	MaxResponseBytes int64         // 0 = 16 MiB
	HTTPClient       *http.Client  // nil = own client (dial 5s, TLS handshake 5s)
}

// Driver is the Qdrant implementation of engine.Engine. It is safe for
// concurrent use. Errors it returns carry only the operation and the HTTP
// status, never the URL, the API key or a response body.
type Driver struct {
	cfg     Config
	baseURL string
	client  *http.Client
}

var _ engine.Engine = (*Driver)(nil)

// errNotImplemented is the answer of the methods Task 7 lands.
var errNotImplemented = errors.New("qdrant: not implemented")

// statusError is a non-success answer from Qdrant (or a transport failure
// when code is 0). Unwrap maps it to the engine sentinels.
type statusError struct {
	op   string
	code int
}

func (e *statusError) Error() string {
	switch {
	case e.code == 0:
		return "qdrant: " + e.op + " failed"
	case e.Unwrap() == engine.ErrUnavailable:
		return fmt.Sprintf("qdrant: %s unavailable (status %d)", e.op, e.code)
	case e.code == http.StatusNotFound:
		return fmt.Sprintf("qdrant: %s: not found (status %d)", e.op, e.code)
	}
	return fmt.Sprintf("qdrant: %s rejected (status %d)", e.op, e.code)
}

func (e *statusError) Unwrap() error {
	switch {
	case e.code == http.StatusNotFound:
		return engine.ErrNotFound
	case e.code >= 500, e.code == http.StatusTooManyRequests,
		e.code == http.StatusUnauthorized, e.code == http.StatusForbidden:
		return engine.ErrUnavailable
	}
	return nil
}

// transportError is a failure before a usable answer: dial, TLS, timeout,
// oversize or undecodable body. cause is only ever a context error.
type transportError struct {
	op     string
	reason string
	cause  error
}

func (e *transportError) Error() string { return "qdrant: " + e.op + " " + e.reason }

func (e *transportError) Unwrap() []error {
	if e.cause == nil {
		return []error{engine.ErrUnavailable}
	}
	return []error{engine.ErrUnavailable, e.cause}
}

// New validates cfg and returns a Driver. It never dials.
func New(cfg Config) (*Driver, error) {
	u, err := url.Parse(cfg.URL)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return nil, errors.New("qdrant: invalid URL")
	}
	if cfg.Timeout <= 0 {
		cfg.Timeout = defaultTimeout
	}
	if cfg.MaxResponseBytes <= 0 {
		cfg.MaxResponseBytes = defaultMaxResponseBytes
	}
	client := cfg.HTTPClient
	if client == nil {
		client = &http.Client{
			Transport: &http.Transport{
				DialContext:         (&net.Dialer{Timeout: 5 * time.Second}).DialContext,
				TLSHandshakeTimeout: 5 * time.Second,
				MaxIdleConnsPerHost: 8,
				IdleConnTimeout:     90 * time.Second,
			},
			CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
		}
	}
	return &Driver{cfg: cfg, baseURL: strings.TrimRight(cfg.URL, "/"), client: client}, nil
}

// send performs one request and returns the raw 2xx body.
func (d *Driver) send(ctx context.Context, op, method, path string, body any) ([]byte, error) {
	var rd io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			return nil, fmt.Errorf("qdrant: %s: encode request", op)
		}
		rd = bytes.NewReader(raw)
	}
	tctx, cancel := context.WithTimeout(ctx, d.cfg.Timeout)
	defer cancel()
	req, err := http.NewRequestWithContext(tctx, method, d.baseURL+path, rd)
	if err != nil {
		return nil, fmt.Errorf("qdrant: %s: build request", op)
	}
	if d.cfg.APIKey != "" {
		req.Header.Set("api-key", d.cfg.APIKey)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("Accept", "application/json")

	resp, err := d.client.Do(req)
	if err != nil {
		return nil, d.transportFailure(ctx, tctx, op, "request failed")
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return nil, &statusError{op: op, code: resp.StatusCode}
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, d.cfg.MaxResponseBytes+1))
	if err != nil {
		return nil, d.transportFailure(ctx, tctx, op, "read failed")
	}
	if int64(len(data)) > d.cfg.MaxResponseBytes {
		return nil, &transportError{op: op, reason: "response too large"}
	}
	return data, nil
}

// transportFailure classifies a failed round trip. A cancellation by the
// caller is not an outage; a deadline (the caller's or ours) is.
func (d *Driver) transportFailure(caller, timed context.Context, op, reason string) error {
	if errors.Is(caller.Err(), context.Canceled) {
		return fmt.Errorf("qdrant: %s canceled: %w", op, context.Canceled)
	}
	if errors.Is(timed.Err(), context.DeadlineExceeded) {
		return &transportError{op: op, reason: "timed out", cause: context.DeadlineExceeded}
	}
	return &transportError{op: op, reason: reason}
}

// do sends a request and decodes the {"result": ...} envelope into out
// (ignored when nil).
func (d *Driver) do(ctx context.Context, op, method, path string, body, out any) error {
	data, err := d.send(ctx, op, method, path, body)
	if err != nil {
		return err
	}
	if out == nil {
		return nil
	}
	var env struct {
		Result json.RawMessage `json:"result"`
	}
	if err := json.Unmarshal(data, &env); err != nil || len(env.Result) == 0 {
		return &transportError{op: op, reason: "returned an undecodable body"}
	}
	if err := json.Unmarshal(env.Result, out); err != nil {
		return &transportError{op: op, reason: "returned an undecodable body"}
	}
	return nil
}

// Health checks that Qdrant answers with a supported version and that the
// API key is accepted (the version endpoint may be open; /collections is not).
func (d *Driver) Health(ctx context.Context) error {
	data, err := d.send(ctx, "health", http.MethodGet, "/", nil)
	if err != nil {
		return err
	}
	var info struct {
		Version string `json:"version"`
	}
	if err := json.Unmarshal(data, &info); err != nil {
		return &transportError{op: "health", reason: "returned an undecodable body"}
	}
	major, minor, ok := parseVersion(info.Version)
	if !ok {
		return &transportError{op: "health", reason: "returned an unrecognised version"}
	}
	if major < minVersionMajor || (major == minVersionMajor && minor < minVersionMinor) {
		return fmt.Errorf("%w: qdrant version %s is older than %d.%d.0",
			engine.ErrUnavailable, info.Version, minVersionMajor, minVersionMinor)
	}
	return d.do(ctx, "health", http.MethodGet, "/collections", nil, nil)
}

// parseVersion reads "major.minor[.patch][-suffix]".
func parseVersion(v string) (major, minor int, ok bool) {
	v, _, _ = strings.Cut(v, "-")
	parts := strings.Split(v, ".")
	if len(parts) < 2 {
		return 0, 0, false
	}
	var err1, err2 error
	major, err1 = strconv.Atoi(parts[0])
	minor, err2 = strconv.Atoi(parts[1])
	return major, minor, err1 == nil && err2 == nil
}

func (d *Driver) Upsert(ctx context.Context, collection string, points []engine.Point) error {
	return errNotImplemented
}

func (d *Driver) DeleteOtherRevisions(ctx context.Context, collection, tenantID, sourceID string, keep int64) error {
	return errNotImplemented
}

func (d *Driver) DeleteByFilter(ctx context.Context, collection string, sel engine.Selector) error {
	return errNotImplemented
}

func (d *Driver) Count(ctx context.Context, collection string, sel engine.Selector) (int64, error) {
	return 0, errNotImplemented
}

func (d *Driver) Query(ctx context.Context, collection string, q engine.Query) ([]engine.Candidate, error) {
	return nil, errNotImplemented
}
