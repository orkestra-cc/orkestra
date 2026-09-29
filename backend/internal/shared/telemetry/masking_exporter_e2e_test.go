package telemetry

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/orkestra/backend/internal/shared/middleware"
	"github.com/orkestra/backend/pkg/sdk/iface"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"go.opentelemetry.io/otel/attribute"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	oteltrace "go.opentelemetry.io/otel/trace"
)

func TestMaskingExporter_DropsPathWithoutRoute(t *testing.T) {
	p := iface.DefaultLogContentPolicy()
	mem := tracetest.NewInMemoryExporter()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSyncer(NewMaskingExporter(mem, mapResolver{"": &p}, nil)))
	_, s := tp.Tracer("t").Start(context.Background(), "x")
	s.SetAttributes(
		attribute.String("url.path", "/v1/public/files/SECRETTOKEN123"),
		attribute.String("http.target", "/v1/public/files/SECRETTOKEN123"),
	)
	s.End()
	a := spanAttrs(mem.GetSpans()[0])
	for _, k := range []string{"url.path", "http.target"} {
		if v, ok := a[k]; ok {
			t.Errorf("%s exported without http.route: %q", k, v)
		}
	}
}

func TestMaskingExporter_PeerAddressAliases(t *testing.T) {
	omit := iface.DefaultLogContentPolicy()
	omit.IPAddress = iface.IPAddressOmitted
	trunc := iface.DefaultLogContentPolicy()
	trunc.ScanFreeText = false // the alias, not the free-text scan, must catch it
	for _, key := range []string{"network.peer.address", "net.peer.addr", "net.sock.peer.addr"} {
		mem := tracetest.NewInMemoryExporter()
		tp := sdktrace.NewTracerProvider(sdktrace.WithSyncer(NewMaskingExporter(mem, mapResolver{"": &omit, "tt": &trunc}, nil)))
		_, s1 := tp.Tracer("t").Start(context.Background(), "omit")
		s1.SetAttributes(attribute.String(key, "203.0.113.9"))
		s1.End()
		_, s2 := tp.Tracer("t").Start(context.Background(), "trunc")
		s2.SetAttributes(attribute.String("tenant.id", "tt"), attribute.String(key, "203.0.113.9"))
		s2.End()
		spans := mem.GetSpans()
		if v, ok := spanAttrs(spans[0])[key]; ok {
			t.Errorf("%s kept under IP=omitted: %q", key, v)
		}
		if v := spanAttrs(spans[1])[key]; v != "203.0.113.0/24" {
			t.Errorf("%s = %q, want /24 truncation even without free-text scan", key, v)
		}
	}
}

func TestMaskingExporter_SliceAttributes(t *testing.T) {
	p := iface.DefaultLogContentPolicy()
	mem := tracetest.NewInMemoryExporter()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSyncer(NewMaskingExporter(mem, mapResolver{"": &p}, nil)))
	_, s := tp.Tracer("t").Start(context.Background(), "x")
	s.SetAttributes(
		attribute.StringSlice("emails", []string{"anna@example.com"}),
		attribute.StringSlice("peers", []string{"203.0.113.7", "ok"}),
		attribute.StringSlice("password", []string{"a", "b"}),
		attribute.IntSlice("codes", []int{1, 2}),
		attribute.Int("token", 5),
	)
	s.End()
	got := map[string]attribute.Value{}
	for _, kv := range mem.GetSpans()[0].Attributes {
		got[string(kv.Key)] = kv.Value
	}
	if v := got["emails"]; v.Type() != attribute.STRINGSLICE || v.AsStringSlice()[0] != "[EMAIL]" {
		t.Errorf("emails = %v", v.Emit())
	}
	if v := got["peers"]; v.Type() != attribute.STRINGSLICE || v.AsStringSlice()[0] != "203.0.113.0/24" || v.AsStringSlice()[1] != "ok" {
		t.Errorf("peers = %v", v.Emit())
	}
	if v := got["password"]; v.Emit() != "[REDACTED]" {
		t.Errorf("password slice = %v, want fully redacted", v.Emit())
	}
	if v := got["codes"]; v.Type() != attribute.INT64SLICE {
		t.Errorf("unchanged int slice altered: %v", v.Emit())
	}
	if v := got["token"]; v.Emit() != "[REDACTED]" {
		t.Errorf("secret-keyed int leaked: %v", v.Emit())
	}
}

func TestMaskingExporter_DoesNotMutateOriginalSpan(t *testing.T) {
	p := iface.DefaultLogContentPolicy()
	mem := tracetest.NewInMemoryExporter()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSyncer(NewMaskingExporter(mem, mapResolver{"": &p}, nil)))
	_, s := tp.Tracer("t").Start(context.Background(), "x")
	s.AddEvent("e", oteltrace.WithAttributes(attribute.String("password", "s3cr3t")))
	s.End()
	if v := mem.GetSpans()[0].Events[0].Attributes[0].Value.Emit(); v != "[REDACTED]" {
		t.Errorf("event attribute = %q", v)
	}
}

// TestMaskingExporter_RealStack drives the production wiring: otelhttp span
// around a chi router behind middleware.RequestLogger.
func TestMaskingExporter_RealStack(t *testing.T) {
	run := func(t *testing.T, p iface.LogContentPolicy) map[string]string {
		t.Helper()
		mem := tracetest.NewInMemoryExporter()
		tp := sdktrace.NewTracerProvider(sdktrace.WithSyncer(NewMaskingExporter(mem, mapResolver{"": &p}, nil)))
		logger := slog.New(slog.NewTextHandler(io.Discard, nil))
		r := chi.NewRouter()
		r.Use(middleware.RequestLogger(logger, middleware.RequestLoggerOptions{SkipPaths: map[string]struct{}{}}))
		r.Get("/v1/x/{id}", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })
		h := otelhttp.NewHandler(r, "test", otelhttp.WithTracerProvider(tp))

		req := httptest.NewRequest(http.MethodGet, "/v1/x/SECRET123?email=anna@example.com", nil)
		req.RemoteAddr = "203.0.113.9:1234"
		req.Header.Set("User-Agent", "Mozilla/5.0 (probe)")
		req.Header.Set("X-Forwarded-For", "198.51.100.4")
		h.ServeHTTP(httptest.NewRecorder(), req)

		spans := mem.GetSpans()
		if len(spans) != 1 {
			t.Fatalf("spans = %d", len(spans))
		}
		a := spanAttrs(spans[0])
		for k, v := range a {
			for _, secret := range []string{"SECRET123", "anna@example.com", "203.0.113.9", "198.51.100.4"} {
				if strings.Contains(v, secret) && p.IPAddress != iface.IPAddressFull {
					t.Errorf("%s = %q leaks %q", k, v, secret)
				}
			}
		}
		if a["http.route"] != "/v1/x/{id}" {
			t.Errorf("http.route = %q", a["http.route"])
		}
		if v, ok := a["url.path"]; ok && v != "/v1/x/{id}" {
			t.Errorf("url.path = %q", v)
		}
		return a
	}

	t.Run("strict", func(t *testing.T) {
		p := iface.DefaultLogContentPolicy()
		p.IPAddress = iface.IPAddressOmitted
		p.UserAgent = iface.UserAgentOmitted
		a := run(t, p)
		for _, k := range []string{"network.peer.address", "client.address", "user_agent.original"} {
			if v, ok := a[k]; ok {
				t.Errorf("%s = %q must be absent under the strict policy", k, v)
			}
		}
	})
	t.Run("default", func(t *testing.T) {
		a := run(t, iface.DefaultLogContentPolicy())
		for _, k := range []string{"network.peer.address", "client.address"} {
			if v, ok := a[k]; ok && v != "203.0.113.0/24" && v != "198.51.100.0/24" {
				t.Errorf("%s = %q, want a /24", k, v)
			}
		}
		if a["user_agent.original"] != "Mozilla/5.0 (probe)" {
			t.Errorf("user_agent.original = %q under UserAgent=full", a["user_agent.original"])
		}
	})
}
