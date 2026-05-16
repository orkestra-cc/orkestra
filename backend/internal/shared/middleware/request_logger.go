package middleware

import (
	"log/slog"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	chiMiddleware "github.com/go-chi/chi/v5/middleware"
	"github.com/orkestra-cc/orkestra-sdk/ctxauth"
)

// RequestLoggerOptions configures the structured HTTP request logger.
//
// Defaults match ADR-0005 §2 (Tier 0 zero-config):
//   - SkipPaths: /health, /ready, /metrics, /openapi.json
//   - SlowThreshold: 1s
//
// Both can be overridden via env vars at construction time
// (NewRequestLogger reads LOG_HTTP_SKIP_PATHS and
// LOG_HTTP_SLOW_THRESHOLD_MS).
type RequestLoggerOptions struct {
	SkipPaths     map[string]struct{}
	SlowThreshold time.Duration
}

// defaultSkipPaths matches the ADR-0005 §2 default. Operators override
// via LOG_HTTP_SKIP_PATHS. Keeping these out of the log keeps Loki bills
// down on probes and scrapes; they're still visible via /metrics
// histograms and the chi access patterns.
var defaultSkipPaths = map[string]struct{}{
	"/health":       {},
	"/ready":        {},
	"/metrics":      {},
	"/openapi.json": {},
}

// NewRequestLogger reads environment overrides and returns options
// suitable for passing to RequestLogger. Empty / unset env vars leave
// the defaults in place.
//
// LOG_HTTP_SKIP_PATHS — comma-separated list of exact paths to skip.
// When set, it REPLACES the default list. To extend rather than replace,
// include the defaults explicitly.
//
// LOG_HTTP_SLOW_THRESHOLD_MS — integer milliseconds. Requests slower
// than this get slog.Bool("slow", true) stamped on the line so operators
// can query {slow=true} in Loki without bucketing on duration_ms.
func NewRequestLoggerOptions() RequestLoggerOptions {
	opts := RequestLoggerOptions{
		SkipPaths:     defaultSkipPaths,
		SlowThreshold: time.Second,
	}
	if raw := os.Getenv("LOG_HTTP_SKIP_PATHS"); raw != "" {
		opts.SkipPaths = parseSkipPaths(raw)
	}
	if raw := os.Getenv("LOG_HTTP_SLOW_THRESHOLD_MS"); raw != "" {
		if ms, err := strconv.Atoi(raw); err == nil && ms > 0 {
			opts.SlowThreshold = time.Duration(ms) * time.Millisecond
		}
	}
	return opts
}

// parseSkipPaths splits a comma list into a set, ignoring blanks. Caller
// owns the returned map (no mutation of defaults).
func parseSkipPaths(raw string) map[string]struct{} {
	parts := strings.Split(raw, ",")
	out := make(map[string]struct{}, len(parts))
	for _, p := range parts {
		if trimmed := strings.TrimSpace(p); trimmed != "" {
			out[trimmed] = struct{}{}
		}
	}
	return out
}

// RequestLogger returns an HTTP middleware that emits one structured
// JSON log line per request, replacing chi's default unstructured
// Logger. ADR-0005 §1.2 — only allowlisted attributes are written; no
// bodies, no headers, no raw query strings.
//
// The middleware must run AFTER chiMiddleware.RequestID and
// chiMiddleware.RealIP so request_id and r.RemoteAddr are populated,
// but BEFORE CORS, RequireAudience, and the body-size gate so rejects
// from those middlewares still produce a log line.
//
// trace_id and span_id are stamped by the slog handler chain
// (TraceContextHandler in shared/utils) via the request's context — not
// added here — so module-emitted logs in the same request scope share
// the same correlation IDs.
//
// tenant_id / tenant_kind / user_id / user_role / audience are stamped
// here when available (the auth/audience middleware may have already run
// and populated them; CORS/audience rejects naturally have them empty).
// Empty values are dropped rather than logged as "" to keep collector-
// side filtering predictable.
func RequestLogger(logger *slog.Logger, opts RequestLoggerOptions) func(http.Handler) http.Handler {
	if opts.SkipPaths == nil {
		opts.SkipPaths = defaultSkipPaths
	}
	if opts.SlowThreshold <= 0 {
		opts.SlowThreshold = time.Second
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if _, skip := opts.SkipPaths[r.URL.Path]; skip {
				next.ServeHTTP(w, r)
				return
			}

			start := time.Now()
			ww := chiMiddleware.NewWrapResponseWriter(w, r.ProtoMajor)
			next.ServeHTTP(ww, r)
			duration := time.Since(start)

			attrs := make([]slog.Attr, 0, 16)
			attrs = append(attrs,
				slog.String("method", r.Method),
				slog.String("path", r.URL.Path),
				slog.Int("status", ww.Status()),
				slog.Int64("duration_ms", duration.Milliseconds()),
				slog.Int("bytes", ww.BytesWritten()),
				slog.String("remote", r.RemoteAddr),
				slog.String("ua", r.UserAgent()),
			)
			if reqID := chiMiddleware.GetReqID(r.Context()); reqID != "" {
				attrs = append(attrs, slog.String("request_id", reqID))
			}
			if duration >= opts.SlowThreshold {
				attrs = append(attrs, slog.Bool("slow", true))
			}

			// Tenant + user + audience are best-effort: when CORS or
			// audience-mismatch rejects fire before RequireAuth runs,
			// these are empty and intentionally omitted from the line.
			if v, ok := ctxauth.GetTenantID(r.Context()); ok && v != "" {
				attrs = append(attrs, slog.String("tenant_id", v))
			}
			if v := ctxauth.TenantKindFromContext(r.Context()); v != "" {
				attrs = append(attrs, slog.String("tenant_kind", v))
			}
			if v, ok := ctxauth.GetUserUUID(r.Context()); ok && v != "" {
				attrs = append(attrs, slog.String("user_id", v))
			}
			if v, ok := ctxauth.GetSystemRole(r.Context()); ok && v != "" {
				attrs = append(attrs, slog.String("user_role", v))
			}
			if v := AudienceFromContext(r.Context()); v != "" {
				attrs = append(attrs, slog.String("audience", v))
			}

			logger.LogAttrs(r.Context(), levelForStatus(ww.Status()), "http_request", attrs...)
		})
	}
}

// levelForStatus maps an HTTP status to a slog level per ADR-0005 §2:
// 5xx → Error, 4xx → Warn, else → Info. 0 means the handler never wrote
// a status — most chi paths default to 200, but a hijacked connection
// (websocket, SSE bypass) or a panic recovered upstream may report 0;
// log it at Info so the line is still discoverable.
func levelForStatus(status int) slog.Level {
	switch {
	case status >= 500:
		return slog.LevelError
	case status >= 400:
		return slog.LevelWarn
	default:
		return slog.LevelInfo
	}
}
