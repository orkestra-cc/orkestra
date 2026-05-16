package utils

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"strings"
	"testing"
	"time"

	"go.opentelemetry.io/otel/sdk/trace"
	otelTrace "go.opentelemetry.io/otel/trace"
)

func TestTraceContextHandler_StampsTraceAndSpanID(t *testing.T) {
	var buf bytes.Buffer
	base := slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelInfo})
	logger := slog.New(NewTraceContextHandler(base))

	tp := trace.NewTracerProvider()
	tracer := tp.Tracer("test")
	ctx, span := tracer.Start(context.Background(), "test-op")
	defer span.End()

	logger.InfoContext(ctx, "hello")

	var line map[string]any
	if err := json.Unmarshal(bytes.TrimSpace(buf.Bytes()), &line); err != nil {
		t.Fatalf("expected JSON output, got %q: %v", buf.String(), err)
	}

	traceID, _ := line["trace_id"].(string)
	spanID, _ := line["span_id"].(string)

	wantTrace := span.SpanContext().TraceID().String()
	wantSpan := span.SpanContext().SpanID().String()
	if traceID != wantTrace {
		t.Errorf("trace_id = %q, want %q", traceID, wantTrace)
	}
	if spanID != wantSpan {
		t.Errorf("span_id = %q, want %q", spanID, wantSpan)
	}
}

func TestTraceContextHandler_NoSpanInContext(t *testing.T) {
	var buf bytes.Buffer
	base := slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelInfo})
	logger := slog.New(NewTraceContextHandler(base))

	logger.InfoContext(context.Background(), "no-span")

	out := buf.String()
	if strings.Contains(out, "trace_id") {
		t.Errorf("expected no trace_id in output without a span, got %q", out)
	}
	if strings.Contains(out, "span_id") {
		t.Errorf("expected no span_id in output without a span, got %q", out)
	}
}

func TestTraceContextHandler_PreservesAttrsAndGroups(t *testing.T) {
	var buf bytes.Buffer
	base := slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelInfo})
	handler := NewTraceContextHandler(base)

	withAttrs := handler.WithAttrs([]slog.Attr{slog.String("svc", "test")})
	if _, ok := withAttrs.(TraceContextHandler); !ok {
		t.Errorf("WithAttrs should return TraceContextHandler, got %T", withAttrs)
	}

	withGroup := handler.WithGroup("g")
	if _, ok := withGroup.(TraceContextHandler); !ok {
		t.Errorf("WithGroup should return TraceContextHandler, got %T", withGroup)
	}

	logger := slog.New(withAttrs)
	tp := trace.NewTracerProvider()
	ctx, span := tp.Tracer("t").Start(context.Background(), "op")
	defer span.End()
	logger.InfoContext(ctx, "msg")

	var line map[string]any
	if err := json.Unmarshal(bytes.TrimSpace(buf.Bytes()), &line); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if line["svc"] != "test" {
		t.Errorf("WithAttrs lost svc attribute: %v", line)
	}
	if _, ok := line["trace_id"]; !ok {
		t.Errorf("WithAttrs lost trace stamping: %v", line)
	}
}

func TestTraceContextHandler_NilContext(t *testing.T) {
	var buf bytes.Buffer
	base := slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelInfo})
	handler := NewTraceContextHandler(base)

	rec := slog.NewRecord(time.Now(), slog.LevelInfo, "msg", 0)
	if err := handler.Handle(nil, rec); err != nil {
		t.Fatalf("Handle with nil ctx returned error: %v", err)
	}
	if strings.Contains(buf.String(), "trace_id") {
		t.Errorf("nil context should not stamp trace_id, got %q", buf.String())
	}
}

func TestTraceContextHandler_InvalidSpanContext(t *testing.T) {
	var buf bytes.Buffer
	base := slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelInfo})
	handler := NewTraceContextHandler(base)

	// Inject an explicitly-invalid SpanContext into the context. The
	// handler must not stamp trace_id when IsValid() is false, even
	// though SpanContextFromContext returns a value.
	ctx := otelTrace.ContextWithSpanContext(context.Background(), otelTrace.SpanContext{})
	rec := slog.NewRecord(time.Now(), slog.LevelInfo, "msg", 0)
	if err := handler.Handle(ctx, rec); err != nil {
		t.Fatalf("Handle returned error: %v", err)
	}
	if strings.Contains(buf.String(), "trace_id") {
		t.Errorf("invalid SpanContext should not stamp trace_id, got %q", buf.String())
	}
}
