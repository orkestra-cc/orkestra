package middleware

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/orkestra-cc/orkestra-sdk/ctxauth"
)

// newCapturingLogger returns a logger writing JSON to a buffer plus a
// helper to parse the latest line. Slog records appear newline-delimited,
// so tests that emit multiple lines can use the buffer directly.
func newCapturingLogger(t *testing.T) (*slog.Logger, *bytes.Buffer) {
	t.Helper()
	buf := &bytes.Buffer{}
	h := slog.NewJSONHandler(buf, &slog.HandlerOptions{Level: slog.LevelDebug})
	return slog.New(h), buf
}

func parseLine(t *testing.T, raw []byte) map[string]any {
	t.Helper()
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 {
		t.Fatalf("expected JSON output, got empty buffer")
	}
	var out map[string]any
	if err := json.Unmarshal(trimmed, &out); err != nil {
		t.Fatalf("invalid JSON %q: %v", string(trimmed), err)
	}
	return out
}

func TestRequestLogger_EmitsAllowlistedAttributes(t *testing.T) {
	logger, buf := newCapturingLogger(t)
	handler := RequestLogger(logger, RequestLoggerOptions{
		SkipPaths:     map[string]struct{}{},
		SlowThreshold: time.Hour,
	})(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, "ok")
	}))

	req := httptest.NewRequest(http.MethodGet, "/v1/users", nil)
	req.Header.Set("User-Agent", "test-client")
	req.RemoteAddr = "10.0.0.1:1234"
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	line := parseLine(t, buf.Bytes())

	if line["msg"] != "http_request" {
		t.Errorf("msg = %v, want http_request", line["msg"])
	}
	if line["method"] != "GET" {
		t.Errorf("method = %v, want GET", line["method"])
	}
	if line["path"] != "/v1/users" {
		t.Errorf("path = %v, want /v1/users", line["path"])
	}
	if line["status"] != float64(200) {
		t.Errorf("status = %v, want 200", line["status"])
	}
	if line["ua"] != "test-client" {
		t.Errorf("ua = %v, want test-client", line["ua"])
	}
	if _, ok := line["duration_ms"]; !ok {
		t.Errorf("expected duration_ms attribute, line: %v", line)
	}
	// Allowlist guard — none of these should ever appear.
	for _, forbidden := range []string{"authorization", "Authorization", "cookie", "Cookie", "body"} {
		if _, present := line[forbidden]; present {
			t.Errorf("forbidden attr %q present: %v", forbidden, line)
		}
	}
}

func TestRequestLogger_LevelMapping(t *testing.T) {
	tests := []struct {
		name      string
		status    int
		wantLevel string
	}{
		{"info on 200", http.StatusOK, "INFO"},
		{"info on 301", http.StatusMovedPermanently, "INFO"},
		{"warn on 400", http.StatusBadRequest, "WARN"},
		{"warn on 404", http.StatusNotFound, "WARN"},
		{"error on 500", http.StatusInternalServerError, "ERROR"},
		{"error on 503", http.StatusServiceUnavailable, "ERROR"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			logger, buf := newCapturingLogger(t)
			handler := RequestLogger(logger, RequestLoggerOptions{})(
				http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
					w.WriteHeader(tc.status)
				}),
			)
			handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/x", nil))
			line := parseLine(t, buf.Bytes())
			if line["level"] != tc.wantLevel {
				t.Errorf("level = %v, want %v (status %d)", line["level"], tc.wantLevel, tc.status)
			}
		})
	}
}

func TestRequestLogger_SkipsDefaultPaths(t *testing.T) {
	for _, path := range []string{"/health", "/ready", "/metrics", "/openapi.json"} {
		t.Run(path, func(t *testing.T) {
			logger, buf := newCapturingLogger(t)
			handler := RequestLogger(logger, RequestLoggerOptions{})(
				http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
					w.WriteHeader(http.StatusOK)
				}),
			)
			handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, path, nil))
			if buf.Len() != 0 {
				t.Errorf("expected no output for %s, got %q", path, buf.String())
			}
		})
	}
}

func TestRequestLogger_CustomSkipPaths(t *testing.T) {
	logger, buf := newCapturingLogger(t)
	opts := RequestLoggerOptions{
		SkipPaths: map[string]struct{}{"/quiet": {}},
	}
	handler := RequestLogger(logger, opts)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/quiet", nil))
	if buf.Len() != 0 {
		t.Errorf("custom skip should suppress /quiet, got %q", buf.String())
	}

	// /health is NOT skipped when custom list replaces defaults.
	handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/health", nil))
	if buf.Len() == 0 {
		t.Errorf("custom skip list should not include /health by default")
	}
}

func TestRequestLogger_SlowFlag(t *testing.T) {
	logger, buf := newCapturingLogger(t)
	handler := RequestLogger(logger, RequestLoggerOptions{
		SlowThreshold: time.Millisecond,
	})(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		time.Sleep(5 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
	}))

	handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/slow", nil))
	line := parseLine(t, buf.Bytes())
	if line["slow"] != true {
		t.Errorf("expected slow=true, got %v", line["slow"])
	}
}

func TestRequestLogger_FastDoesNotFlag(t *testing.T) {
	logger, buf := newCapturingLogger(t)
	handler := RequestLogger(logger, RequestLoggerOptions{
		SlowThreshold: time.Hour,
	})(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/fast", nil))
	line := parseLine(t, buf.Bytes())
	if _, ok := line["slow"]; ok {
		t.Errorf("expected slow attr absent for fast request, got %v", line)
	}
}

func TestRequestLogger_TenantUserAudienceFromContext(t *testing.T) {
	logger, buf := newCapturingLogger(t)
	handler := RequestLogger(logger, RequestLoggerOptions{})(
		http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusOK)
		}),
	)

	req := httptest.NewRequest(http.MethodGet, "/v1/scoped", nil)
	ctx := req.Context()
	ctx = context.WithValue(ctx, ctxauth.KeyUserUUID, "user-uuid-1")
	ctx = context.WithValue(ctx, ctxauth.KeyTenantID, "tenant-uuid-1")
	ctx = context.WithValue(ctx, ctxauth.KeyTenantKind, "external")
	ctx = context.WithValue(ctx, ctxauth.KeySystemRole, "administrator")
	ctx = context.WithValue(ctx, AudienceContextKey, "operator")
	req = req.WithContext(ctx)

	handler.ServeHTTP(httptest.NewRecorder(), req)

	line := parseLine(t, buf.Bytes())
	if line["tenant_id"] != "tenant-uuid-1" {
		t.Errorf("tenant_id = %v", line["tenant_id"])
	}
	if line["tenant_kind"] != "external" {
		t.Errorf("tenant_kind = %v", line["tenant_kind"])
	}
	if line["user_id"] != "user-uuid-1" {
		t.Errorf("user_id = %v", line["user_id"])
	}
	if line["user_role"] != "administrator" {
		t.Errorf("user_role = %v", line["user_role"])
	}
	if line["audience"] != "operator" {
		t.Errorf("audience = %v", line["audience"])
	}
}

func TestRequestLogger_AnonymousOmitsTenantUserAudience(t *testing.T) {
	logger, buf := newCapturingLogger(t)
	handler := RequestLogger(logger, RequestLoggerOptions{})(
		http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusOK)
		}),
	)
	handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/v1/anon", nil))
	line := parseLine(t, buf.Bytes())
	for _, k := range []string{"tenant_id", "tenant_kind", "user_id", "user_role", "audience"} {
		if _, ok := line[k]; ok {
			t.Errorf("expected %s omitted for anonymous request, got %v", k, line[k])
		}
	}
}

func TestNewRequestLoggerOptions_EnvOverrides(t *testing.T) {
	t.Setenv("LOG_HTTP_SKIP_PATHS", "/foo, /bar")
	t.Setenv("LOG_HTTP_SLOW_THRESHOLD_MS", "250")

	opts := NewRequestLoggerOptions()

	if _, ok := opts.SkipPaths["/foo"]; !ok {
		t.Errorf("expected /foo in skip paths, got %v", opts.SkipPaths)
	}
	if _, ok := opts.SkipPaths["/bar"]; !ok {
		t.Errorf("expected /bar in skip paths, got %v", opts.SkipPaths)
	}
	// Defaults are replaced, not extended — that's the documented behavior.
	if _, ok := opts.SkipPaths["/health"]; ok {
		t.Errorf("LOG_HTTP_SKIP_PATHS should REPLACE defaults, got %v", opts.SkipPaths)
	}
	if opts.SlowThreshold != 250*time.Millisecond {
		t.Errorf("SlowThreshold = %v, want 250ms", opts.SlowThreshold)
	}
}

func TestNewRequestLoggerOptions_DefaultsWhenUnset(t *testing.T) {
	t.Setenv("LOG_HTTP_SKIP_PATHS", "")
	t.Setenv("LOG_HTTP_SLOW_THRESHOLD_MS", "")

	opts := NewRequestLoggerOptions()
	if _, ok := opts.SkipPaths["/health"]; !ok {
		t.Errorf("expected /health in default skip paths, got %v", opts.SkipPaths)
	}
	if opts.SlowThreshold != time.Second {
		t.Errorf("SlowThreshold = %v, want 1s", opts.SlowThreshold)
	}
}

func TestNewRequestLoggerOptions_InvalidSlowMs(t *testing.T) {
	t.Setenv("LOG_HTTP_SLOW_THRESHOLD_MS", "not-a-number")
	opts := NewRequestLoggerOptions()
	if opts.SlowThreshold != time.Second {
		t.Errorf("invalid LOG_HTTP_SLOW_THRESHOLD_MS should fall back to 1s, got %v", opts.SlowThreshold)
	}

	t.Setenv("LOG_HTTP_SLOW_THRESHOLD_MS", "-5")
	opts = NewRequestLoggerOptions()
	if opts.SlowThreshold != time.Second {
		t.Errorf("negative LOG_HTTP_SLOW_THRESHOLD_MS should fall back to 1s, got %v", opts.SlowThreshold)
	}
}

func TestRequestLogger_DefaultStatusIs200(t *testing.T) {
	// Handlers that don't call WriteHeader still produce 200 via the
	// ResponseWriter contract. Confirm the wrap logs it correctly.
	logger, buf := newCapturingLogger(t)
	handler := RequestLogger(logger, RequestLoggerOptions{})(
		http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			_, _ = io.WriteString(w, "implicit 200")
		}),
	)
	handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/x", nil))
	line := parseLine(t, buf.Bytes())
	if line["status"] != float64(200) {
		t.Errorf("status = %v, want 200", line["status"])
	}
}

func TestLevelForStatus(t *testing.T) {
	if levelForStatus(0) != slog.LevelInfo {
		t.Errorf("status 0 should map to Info")
	}
	if levelForStatus(199) != slog.LevelInfo {
		t.Errorf("status 199 should map to Info")
	}
	if levelForStatus(399) != slog.LevelInfo {
		t.Errorf("status 399 should map to Info")
	}
	if levelForStatus(400) != slog.LevelWarn {
		t.Errorf("status 400 should map to Warn")
	}
	if levelForStatus(499) != slog.LevelWarn {
		t.Errorf("status 499 should map to Warn")
	}
	if levelForStatus(500) != slog.LevelError {
		t.Errorf("status 500 should map to Error")
	}
}

func TestParseSkipPaths_TrimsAndDropsBlanks(t *testing.T) {
	got := parseSkipPaths("  /a , ,/b,  /c  ")
	for _, want := range []string{"/a", "/b", "/c"} {
		if _, ok := got[want]; !ok {
			t.Errorf("expected %q in %v", want, got)
		}
	}
	if len(got) != 3 {
		t.Errorf("expected 3 entries, got %d: %v", len(got), got)
	}
}
