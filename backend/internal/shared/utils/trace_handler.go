package utils

import (
	"context"
	"log/slog"

	"go.opentelemetry.io/otel/trace"
)

// TraceContextHandler wraps an slog.Handler and stamps trace_id +
// span_id from the OpenTelemetry SpanContext onto every log record.
//
// ADR-0005 §1.1 — the multi-tenant correlation guarantee depends on
// every operational log line carrying the trace identifiers so that
// Grafana / Tempo / any OTLP backend can join logs to traces without
// an external mapping table. Module code reaches this for free as
// long as it calls slog.*Context variants (slog.InfoContext etc.); the
// stdlib falls back to context.Background when the no-context variants
// are used, in which case there is no span and nothing is stamped.
//
// Tenant / user / audience attributes are NOT stamped here on
// purpose — that would create a coupling between this package and
// the auth/audience middleware context keys. Those attributes are
// attached by the request_logger middleware (which owns the request
// context). Module-emitted logs still join to traces via trace_id,
// and the trace itself carries tenant.id baggage via TenantBaggage.
type TraceContextHandler struct {
	slog.Handler
}

// NewTraceContextHandler wraps the supplied base handler. Passing nil
// is a programmer error — SetupLogger always passes a real handler.
func NewTraceContextHandler(base slog.Handler) TraceContextHandler {
	return TraceContextHandler{Handler: base}
}

// Handle stamps trace_id / span_id when the record's context carries
// a valid SpanContext, then delegates. SpanContextFromContext returns
// an empty (invalid) value cheaply when no span is in flight, so the
// IsValid short-circuit keeps the cost to a single boolean check on
// the hot path. The no-op OTEL provider still produces real (valid)
// SpanContexts because Init in shared/telemetry constructs a
// TracerProvider unconditionally — so trace_id appears even when the
// collector is unwired.
func (h TraceContextHandler) Handle(ctx context.Context, r slog.Record) error {
	if ctx != nil {
		if sc := trace.SpanContextFromContext(ctx); sc.IsValid() {
			r.AddAttrs(
				slog.String("trace_id", sc.TraceID().String()),
				slog.String("span_id", sc.SpanID().String()),
			)
		}
	}
	return h.Handler.Handle(ctx, r)
}

// WithAttrs returns a new handler that wraps the base handler's
// WithAttrs result, preserving the trace stamping behavior.
func (h TraceContextHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return TraceContextHandler{Handler: h.Handler.WithAttrs(attrs)}
}

// WithGroup returns a new handler that wraps the base handler's
// WithGroup result, preserving the trace stamping behavior.
func (h TraceContextHandler) WithGroup(name string) slog.Handler {
	return TraceContextHandler{Handler: h.Handler.WithGroup(name)}
}
