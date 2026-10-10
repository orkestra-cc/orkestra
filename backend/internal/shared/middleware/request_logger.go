package middleware

import (
	"log/slog"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	chiMiddleware "github.com/go-chi/chi/v5/middleware"
	"github.com/orkestra/backend/pkg/sdk/ctxauth"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

// HTTPMetricsRecorder is the small interface the structured request
// logger uses to report request latency. The concrete production
// implementation is *metrics.Collector — kept behind an interface so
// the middleware package doesn't have to import the SDK metrics package
// transitively (tests can pass a fake; main.go wires metrics.Default()).
//
// ADR-0005 Phase B / ADR-0002 (labels: audience, method, route,
// status_class). route MUST be the Chi route template, not the raw
// path — the recorder rewrites empty values to "unknown" but does not
// otherwise normalize.
type HTTPMetricsRecorder interface {
	RecordHTTPRequest(audience, method, route string, status int, duration time.Duration, traceID string)
}

// RequestLoggerOptions configures the structured HTTP request logger.
//
// Defaults match ADR-0005 §2 (Tier 0 zero-config):
//   - SkipPaths: /health, /ready, /metrics, /openapi.json
//   - SlowThreshold: 1s
//
// SkipPaths and SlowThreshold can be overridden via env vars at
// construction time (NewRequestLoggerOptions reads LOG_HTTP_SKIP_PATHS
// and LOG_HTTP_SLOW_THRESHOLD_MS).
//
// Metrics is the optional Prometheus recorder for ADR-0005 Phase B. nil
// is fine — the middleware skips the metric observation but still emits
// the log line. Audience identifies which mux this logger is mounted on
// ("operator" | "client" | "service" | ""); it propagates into the
// histogram's `audience` label.
type RequestLoggerOptions struct {
	SkipPaths     map[string]struct{}
	SlowThreshold time.Duration
	Metrics       HTTPMetricsRecorder
	Audience      string
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
// bodies, no headers, no raw query strings, and the route template instead of the raw path.
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
// tenant_id / tenant_kind / user_id / user_role / audience come from the
// request annotations the auth and audience middlewares fill downstream
// (ctxauth.RequestAnnotations); without them this outer middleware could
// not see the principal at all. Empty values are dropped rather than logged
// as "" to keep collector-side filtering predictable.
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

			ctx, ann := ctxauth.WithRequestAnnotations(r.Context())
			r = r.WithContext(ctx)

			start := time.Now()
			ww := chiMiddleware.NewWrapResponseWriter(w, r.ProtoMajor)
			next.ServeHTTP(ww, r)
			duration := time.Since(start)

			// otelhttp computes http.route once, at span start, from a
			// pattern chi has not set yet, so server spans never carry it.
			// Stamp the matched template on the still-open span: the trace
			// exporter substitutes it for the raw url.path (spec §2.5).
			if route := chiRoutePattern(r); route != "" {
				trace.SpanFromContext(r.Context()).SetAttributes(attribute.String("http.route", route))
			}

			attrs := make([]slog.Attr, 0, 16)
			attrs = append(attrs,
				slog.String("method", r.Method),
				slog.Int("status", ww.Status()),
				slog.Int64("duration_ms", duration.Milliseconds()),
				slog.Int("bytes", ww.BytesWritten()),
				slog.String("remote", r.RemoteAddr),
				slog.String("ua", r.UserAgent()),
			)
			// Spec §2.4: the route template identifies the endpoint without
			// the identifiers a raw path carries (user ids, e-mails). The
			// raw path is logged only when chi matched no template (404s,
			// requests outside the router); the PolicyHandler still masks it.
			if route := chiRoutePattern(r); route != "" {
				attrs = append(attrs, slog.String("route", route))
			} else {
				attrs = append(attrs, slog.String("path", r.URL.Path))
			}
			if reqID := chiMiddleware.GetReqID(r.Context()); reqID != "" {
				attrs = append(attrs, slog.String("request_id", reqID))
			}
			if duration >= opts.SlowThreshold {
				attrs = append(attrs, slog.Bool("slow", true))
			}

			// The auth middlewares run downstream on a derived context; they
			// report the principal through the annotations installed above.
			// Values already on this context (tests, outer middleware) are the
			// fallback. Empty values are omitted from the line.
			snap := ann.Snapshot()
			tenantID := snap.TenantID
			if tenantID == "" {
				tenantID, _ = ctxauth.GetTenantID(r.Context())
			}
			tenantKind := snap.TenantKind
			if tenantKind == "" {
				tenantKind = ctxauth.TenantKindFromContext(r.Context())
			}
			userID := snap.UserID
			if userID == "" {
				userID, _ = ctxauth.GetUserUUID(r.Context())
			}
			userRole := snap.UserRole
			if userRole == "" {
				userRole, _ = ctxauth.GetSystemRole(r.Context())
			}
			for _, kv := range [...]struct{ k, v string }{
				{"tenant_id", tenantID}, {"tenant_kind", tenantKind},
				{"user_id", userID}, {"user_role", userRole},
			} {
				if kv.v != "" {
					attrs = append(attrs, slog.String(kv.k, kv.v))
				}
			}
			audience := snap.Audience
			if audience == "" {
				audience = AudienceFromContext(r.Context())
			}
			if audience == "" {
				audience = opts.Audience
			}
			if audience != "" {
				attrs = append(attrs, slog.String("audience", audience))
			}

			logger.LogAttrs(r.Context(), levelForStatus(ww.Status(), snap.ExpectedUnavailable), "http_request", attrs...)

			// ADR-0005 Phase B — histogram observation. Reads the route
			// template from chi AFTER next.ServeHTTP has returned so the
			// pattern is populated. RoutePattern is empty for 404s and
			// for paths that didn't reach a chi-routed handler; the
			// recorder rewrites "" to "unknown". Streaming endpoints
			// (paths ending in /stream) are excluded because the
			// histogram measures user-facing latency, not long-lived SSE
			// connection lifetime.
			if opts.Metrics != nil && !strings.HasSuffix(r.URL.Path, "/stream") {
				route := chiRoutePattern(r)
				traceID := ""
				if sc := trace.SpanContextFromContext(r.Context()); sc.IsValid() {
					traceID = sc.TraceID().String()
				}
				opts.Metrics.RecordHTTPRequest(audience, r.Method, route, ww.Status(), duration, traceID)
			}
		})
	}
}

// chiRoutePattern returns the matched chi route template for the
// request, or "" when none matched (404s, requests served outside the
// chi router, or requests that didn't reach a handler at all). The
// caller is responsible for substituting an "unknown" placeholder
// before using the value as a Prometheus label — keeping that
// substitution in the recorder centralizes the cardinality discipline.
func chiRoutePattern(r *http.Request) string {
	if rc := chi.RouteContext(r.Context()); rc != nil {
		return rc.RoutePattern()
	}
	return ""
}

// levelForStatus maps an HTTP status to a slog level per ADR-0005 §2:
// 5xx → Error, 4xx → Warn, else → Info. 0 means the handler never wrote
// a status — most chi paths default to 200, but a hijacked connection
// (websocket, SSE bypass) or a panic recovered upstream may report 0;
// log it at Info so the line is still discoverable.
//
// One opt-in exception to the 5xx rule: a 503 whose error declared itself an
// expected unavailability of an optional feature this installation has not
// configured (errcode.FeatureNotConfigured) is logged at Warn — the UI renders
// it inline and it is not a server fault. expectedUnavailable carries that
// declaration, recorded through ctxauth.RequestAnnotations by
// errors.RecordErrorCode; the error code is never interpreted, so a 503 that
// did not opt in (a missing signing key, a down dependency) and every other
// 5xx stay at Error.
func levelForStatus(status int, expectedUnavailable bool) slog.Level {
	switch {
	case status == http.StatusServiceUnavailable && expectedUnavailable:
		return slog.LevelWarn
	case status >= 500:
		return slog.LevelError
	case status >= 400:
		return slog.LevelWarn
	default:
		return slog.LevelInfo
	}
}
