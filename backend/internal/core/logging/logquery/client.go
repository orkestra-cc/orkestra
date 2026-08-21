// Package logquery implements the logging module's constrained Loki preview
// boundary. It accepts only registered module names and closed filter values;
// callers cannot submit a URL or arbitrary LogQL.
package logquery

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/orkestra/backend/internal/core/logging/models"
)

const (
	requestTimeout   = 3 * time.Second
	maxResponseBytes = 1 << 20
	maxEvents        = 100
	defaultLimit     = 50
	maxSearchRunes   = 200
)

var (
	ErrUnavailable      = errors.New("log provider unavailable")
	ErrInvalidQuery     = errors.New("invalid log preview query")
	ErrTimeout          = errors.New("log provider timeout")
	ErrUpstream         = errors.New("log provider request failed")
	ErrResponseTooLarge = errors.New("log provider response too large")
)

// Status reports whether a provider was configured successfully. It is a
// local configuration check and never performs upstream I/O.
type Status struct {
	Available bool `json:"available"`
}

// Query is the complete constrained filter accepted by the Loki adapter.
// Base URLs and raw LogQL are intentionally absent.
type Query struct {
	Module        string
	WindowMinutes int
	Level         string
	Text          string
	Limit         int
}

// Provider is the optional log-preview seam consumed by the HTTP handler.
type Provider interface {
	Status(context.Context) Status
	Query(context.Context, Query) ([]models.LogEvent, error)
}

type Client struct {
	baseURL    *url.URL
	modules    map[string]struct{}
	httpClient *http.Client
	now        func() time.Time
}

type unavailableProvider struct{}

// New creates a provider from trusted process configuration. A blank or
// malformed base URL yields an unavailable provider so log-level management
// remains usable without Loki.
func New(rawBaseURL string, modules []string) Provider {
	return newClient(rawBaseURL, modules, requestTimeout, time.Now)
}

func newClient(rawBaseURL string, modules []string, timeout time.Duration, now func() time.Time) Provider {
	baseURL, err := parseBaseURL(rawBaseURL)
	if err != nil {
		return unavailableProvider{}
	}
	registered := make(map[string]struct{}, len(modules))
	for _, name := range modules {
		if name != "" {
			registered[name] = struct{}{}
		}
	}
	if timeout <= 0 {
		timeout = requestTimeout
	}
	if now == nil {
		now = time.Now
	}
	return &Client{
		baseURL: baseURL,
		modules: registered,
		httpClient: &http.Client{
			Timeout: timeout,
			CheckRedirect: func(*http.Request, []*http.Request) error {
				return http.ErrUseLastResponse
			},
		},
		now: now,
	}
}

func parseBaseURL(raw string) (*url.URL, error) {
	if strings.TrimSpace(raw) == "" {
		return nil, ErrUnavailable
	}
	parsed, err := url.Parse(raw)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" || parsed.User != nil {
		return nil, ErrUnavailable
	}
	parsed.RawQuery = ""
	parsed.Fragment = ""
	return parsed, nil
}

func (unavailableProvider) Status(context.Context) Status {
	return Status{Available: false}
}

func (unavailableProvider) Query(context.Context, Query) ([]models.LogEvent, error) {
	return nil, ErrUnavailable
}

func (*Client) Status(context.Context) Status {
	return Status{Available: true}
}

func (c *Client) Query(ctx context.Context, query Query) ([]models.LogEvent, error) {
	normalized, err := c.validate(query)
	if err != nil {
		return nil, err
	}

	endpoint := *c.baseURL
	endpoint.Path = "/loki/api/v1/query_range"
	endpoint.RawPath = ""
	end := c.now().UTC()
	parameters := url.Values{}
	parameters.Set("direction", "backward")
	parameters.Set("end", strconv.FormatInt(end.UnixNano(), 10))
	parameters.Set("limit", strconv.Itoa(normalized.Limit))
	parameters.Set("query", buildLogQL(normalized))
	parameters.Set("start", strconv.FormatInt(end.Add(-time.Duration(normalized.WindowMinutes)*time.Minute).UnixNano(), 10))
	endpoint.RawQuery = parameters.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint.String(), nil)
	if err != nil {
		return nil, ErrUpstream
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		if isTimeoutError(ctx, err) {
			return nil, ErrTimeout
		}
		return nil, ErrUpstream
	}
	defer resp.Body.Close()
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return nil, ErrUpstream
	}

	// Read one probe byte beyond the cap so an exactly one-MiB response remains
	// valid while larger responses are rejected without unbounded buffering.
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes+1))
	if err != nil {
		if isTimeoutError(ctx, err) {
			return nil, ErrTimeout
		}
		return nil, ErrUpstream
	}
	if len(body) > maxResponseBytes {
		return nil, ErrResponseTooLarge
	}
	events, err := parseResponse(body, normalized.Module, normalized.Limit)
	if err != nil {
		return nil, ErrUpstream
	}
	sort.SliceStable(events, func(i, j int) bool {
		return events[i].Timestamp.Before(events[j].Timestamp)
	})
	return events, nil
}

func isTimeoutError(ctx context.Context, err error) bool {
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return true
	}
	var timeout interface{ Timeout() bool }
	return errors.As(err, &timeout) && timeout.Timeout()
}

func (c *Client) validate(query Query) (Query, error) {
	if _, ok := c.modules[query.Module]; !ok {
		return Query{}, fmt.Errorf("%w: unknown module", ErrInvalidQuery)
	}
	switch query.WindowMinutes {
	case 5, 15, 60:
	default:
		return Query{}, fmt.Errorf("%w: windowMinutes must be 5, 15, or 60", ErrInvalidQuery)
	}
	if query.Level != "" {
		switch strings.ToLower(strings.TrimSpace(query.Level)) {
		case string(models.LogLevelDebug), string(models.LogLevelInfo), string(models.LogLevelWarn), string(models.LogLevelError):
			query.Level = strings.ToLower(strings.TrimSpace(query.Level))
		default:
			return Query{}, fmt.Errorf("%w: invalid level", ErrInvalidQuery)
		}
	}
	if utf8.RuneCountInString(query.Text) > maxSearchRunes {
		return Query{}, fmt.Errorf("%w: search exceeds 200 characters", ErrInvalidQuery)
	}
	if query.Limit < 0 {
		return Query{}, fmt.Errorf("%w: limit must be positive", ErrInvalidQuery)
	}
	if query.Limit == 0 {
		query.Limit = defaultLimit
	}
	if query.Limit > maxEvents {
		query.Limit = maxEvents
	}
	return query, nil
}

func buildLogQL(query Query) string {
	logQL := `{service="orkestra-backend", module=` + strconv.Quote(query.Module) + `} | json`
	if query.Level != "" {
		logQL += ` | level=` + strconv.Quote(strings.ToUpper(query.Level))
	}
	if query.Text != "" {
		logQL += ` |= ` + strconv.Quote(query.Text)
	}
	return logQL
}

type lokiResponse struct {
	Status string `json:"status"`
	Data   struct {
		ResultType string `json:"resultType"`
		Result     []struct {
			Stream map[string]string `json:"stream"`
			Values []json.RawMessage `json:"values"`
		} `json:"result"`
	} `json:"data"`
}

func parseResponse(body []byte, requestedModule string, limit int) ([]models.LogEvent, error) {
	var payload lokiResponse
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, err
	}
	if payload.Status != "success" || payload.Data.ResultType != "streams" {
		return nil, ErrUpstream
	}

	events := make([]models.LogEvent, 0, limit)
	resultCapReached := false
	for _, result := range payload.Data.Result {
		for _, rawValue := range result.Values {
			var value []json.RawMessage
			if err := json.Unmarshal(rawValue, &value); err != nil || len(value) < 2 || len(value) > 3 {
				return nil, ErrUpstream
			}
			var timestamp, line string
			if err := json.Unmarshal(value[0], &timestamp); err != nil {
				return nil, ErrUpstream
			}
			if err := json.Unmarshal(value[1], &line); err != nil {
				return nil, ErrUpstream
			}
			// Loki v3 may append a structured-metadata object. It is deliberately
			// ignored: only the normalized JSON log line is allowlist-projected.
			nanoseconds, err := strconv.ParseInt(timestamp, 10, 64)
			if err != nil {
				return nil, ErrUpstream
			}
			events = append(events, projectEvent(time.Unix(0, nanoseconds).UTC(), requestedModule, result.Stream, line))
			if len(events) == limit {
				resultCapReached = true
				break
			}
		}
		if resultCapReached {
			break
		}
	}
	return events, nil
}

func projectEvent(timestamp time.Time, requestedModule string, stream map[string]string, line string) models.LogEvent {
	record := make(map[string]any)
	message := line
	if json.Unmarshal([]byte(line), &record) == nil {
		message = stringValue(record["msg"])
		if message == "" {
			message = stringValue(record["message"])
		}
	}

	level := models.LogLevelInfo
	if parsed, err := models.Parse(stringValue(record["level"])); err == nil {
		level = parsed
	} else if parsed, err := models.Parse(stream["level"]); err == nil {
		level = parsed
	}

	return models.LogEvent{
		Timestamp:  timestamp,
		Level:      level,
		Message:    message,
		Module:     requestedModule,
		Attributes: projectAttributes(record),
	}
}

var stringAttributeKeys = [...]string{"trace_id", "span_id", "request_id", "route"}
var numericAttributeKeys = [...]string{"duration_ms", "duration_ns", "duration_seconds"}

func projectAttributes(record map[string]any) map[string]any {
	attributes := make(map[string]any)
	for _, key := range stringAttributeKeys {
		if value, ok := record[key].(string); ok && value != "" {
			attributes[key] = value
		}
	}
	for _, key := range numericAttributeKeys {
		switch value := record[key].(type) {
		case float64:
			attributes[key] = value
		case json.Number:
			attributes[key] = value
		}
	}
	redacted, _ := Redact(attributes).(map[string]any)
	return redacted
}

func stringValue(value any) string {
	text, _ := value.(string)
	return text
}
