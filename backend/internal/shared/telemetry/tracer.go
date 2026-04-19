// Package telemetry initializes the OpenTelemetry tracer provider used
// across the backend. ADR-0001 Phase 4.4 requires that every request
// span carries tenant.id, tenant.kind, user.id, and (future)
// cedar.policy_id attributes — this package sets up the provider so
// those attributes land wherever the backend is pointed.
//
// By default the provider is a no-op: when the environment variable
// OTEL_EXPORTER_OTLP_ENDPOINT is unset, spans are created but never
// exported. Setting the endpoint swaps in the OTLP HTTP exporter so a
// collector (Grafana Tempo, Jaeger, Honeycomb, …) receives the data.
// Callers don't need to branch — the tracer API is the same either way.
package telemetry

import (
	"context"
	"log/slog"
	"os"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
)

// ShutdownFunc graceful-shutdowns the tracer provider, flushing any
// buffered spans. main.go defers it so span loss on SIGTERM is bounded.
type ShutdownFunc func(ctx context.Context) error

// Init configures the global tracer provider. The provider:
//
//   - Exports to OTEL_EXPORTER_OTLP_ENDPOINT via OTLP HTTP when set.
//     Standard env vars (OTEL_EXPORTER_OTLP_HEADERS,
//     OTEL_EXPORTER_OTLP_INSECURE) are honored through the OTLP
//     library's own env parser, so the backend doesn't need to wrap
//     every knob.
//   - Is a no-op otherwise. The tracer still works — `tracer.Start`
//     returns a span, `SetAttributes` succeeds — but nothing is
//     exported. Lets the baggage middleware run unconditionally.
//   - Stamps resource attributes (service.name, deployment.environment)
//     so every span is self-describing at the collector.
//
// Callers must defer the returned ShutdownFunc so span batches aren't
// lost when the process exits.
func Init(serviceName, environment string, logger *slog.Logger) ShutdownFunc {
	endpoint := os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT")

	// Service + deployment attributes follow OTEL semantic conventions
	// (service.name, deployment.environment). Raw attribute.String is
	// used so we don't pin a specific semconv schema version —
	// otlptracehttp's default resource carries its own schema URL and
	// merging across versions is flaky.
	res, err := resource.Merge(
		resource.Default(),
		resource.NewSchemaless(
			attribute.String("service.name", serviceName),
			attribute.String("deployment.environment", environment),
		),
	)
	if err != nil {
		logger.Warn("telemetry: building resource failed; using defaults", slog.String("error", err.Error()))
		res = resource.Default()
	}

	var tp *sdktrace.TracerProvider
	if endpoint == "" {
		// No-op provider: creates real spans but never exports. Cheap —
		// the cost is a handful of allocations per request that a
		// compiled-out provider would also incur. Kept on so tenant
		// baggage middleware runs uniformly whether or not collection
		// is wired.
		tp = sdktrace.NewTracerProvider(sdktrace.WithResource(res))
		logger.Info("telemetry: OTEL_EXPORTER_OTLP_ENDPOINT unset — tracer runs no-op")
	} else {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		exp, err := otlptracehttp.New(ctx, otlptracehttp.WithEndpoint(endpoint))
		if err != nil {
			logger.Warn("telemetry: OTLP exporter init failed — falling back to no-op",
				slog.String("endpoint", endpoint),
				slog.String("error", err.Error()),
			)
			tp = sdktrace.NewTracerProvider(sdktrace.WithResource(res))
		} else {
			tp = sdktrace.NewTracerProvider(
				sdktrace.WithBatcher(exp),
				sdktrace.WithResource(res),
			)
			logger.Info("telemetry: OTLP exporter ready", slog.String("endpoint", endpoint))
		}
	}

	otel.SetTracerProvider(tp)
	// TraceContext propagator: honors W3C Trace-Context headers on
	// incoming requests so upstream callers (frontend, mobile, AI
	// sidecar) can stitch their spans into the same trace.
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{},
		propagation.Baggage{},
	))

	return func(ctx context.Context) error {
		// Force-flush then shutdown so any batched spans are exported
		// before process exit. Budget 3 seconds — the outer deadline is
		// whatever main.go supplies.
		fctx, cancel := context.WithTimeout(ctx, 3*time.Second)
		defer cancel()
		if err := tp.ForceFlush(fctx); err != nil {
			logger.Warn("telemetry: flush failed", slog.String("error", err.Error()))
		}
		return tp.Shutdown(ctx)
	}
}
