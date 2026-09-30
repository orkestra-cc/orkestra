package telemetry

import (
	"context"
	"testing"

	"github.com/orkestra/backend/pkg/sdk/iface"
	"go.opentelemetry.io/otel/attribute"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

type mapResolver map[string]*iface.LogContentPolicy

func (m mapResolver) LogContentFor(tenant string) *iface.LogContentPolicy {
	if p, ok := m[tenant]; ok {
		return p
	}
	return m[""]
}

func spanAttrs(s tracetest.SpanStub) map[string]string {
	out := map[string]string{}
	for _, kv := range s.Attributes {
		out[string(kv.Key)] = kv.Value.Emit()
	}
	return out
}

func TestMaskingExporter(t *testing.T) {
	strict := iface.DefaultLogContentPolicy()
	strict.IPAddress = iface.IPAddressOmitted
	loose := iface.DefaultLogContentPolicy()
	loose.IPAddress = iface.IPAddressFull
	mem := tracetest.NewInMemoryExporter()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSyncer(NewMaskingExporter(mem, mapResolver{"": &strict, "t-loose": &loose}, nil)))
	tr := tp.Tracer("test")

	_, s1 := tr.Start(context.Background(), "no-tenant")
	s1.SetAttributes(
		attribute.String("client.address", "203.0.113.7"),
		attribute.String("http.request.header.x-forwarded-for", "203.0.113.7"),
		attribute.String("http.route", "/v1/users/{id}"),
		attribute.String("url.path", "/v1/users/anna@example.com"),
		attribute.String("url.full", "https://api.example/v1/users/anna@example.com?x=1"),
		attribute.String("url.query", "email=anna@example.com"),
		attribute.String("password", "s3cr3t"),
		attribute.Int("http.response.status_code", 200),
	)
	s1.End()
	_, s2 := tr.Start(context.Background(), "tenant")
	s2.SetAttributes(attribute.String("tenant.id", "t-loose"), attribute.String("client.address", "203.0.113.7"))
	s2.End()

	spans := mem.GetSpans()
	if len(spans) != 2 {
		t.Fatalf("spans = %d", len(spans))
	}
	a := spanAttrs(spans[0])
	if _, ok := a["client.address"]; ok {
		t.Error("no-tenant span kept the IP: the strictest policy must apply")
	}
	if _, ok := a["http.request.header.x-forwarded-for"]; ok {
		t.Error("x-forwarded-for header kept the IP: it must follow the IP policy")
	}
	if a["url.path"] != "/v1/users/{id}" {
		t.Errorf("url.path = %q, want the route template", a["url.path"])
	}
	for _, k := range []string{"url.full", "url.query"} {
		if _, ok := a[k]; ok {
			t.Errorf("%s must be dropped", k)
		}
	}
	if a["password"] != "[REDACTED]" {
		t.Errorf("password = %q", a["password"])
	}
	if a["http.response.status_code"] != "200" {
		t.Errorf("non-string attribute changed: %q", a["http.response.status_code"])
	}
	if b := spanAttrs(spans[1]); b["client.address"] != "203.0.113.7" {
		t.Errorf("tenant policy not applied to its span: %v", b)
	}
}

func TestSwapSpanPolicyResolver(t *testing.T) {
	strict := iface.DefaultLogContentPolicy()
	strict.UserAgent = iface.UserAgentOmitted
	mem := tracetest.NewInMemoryExporter()
	exp := NewMaskingExporter(mem, mapResolver{"": func() *iface.LogContentPolicy { p := iface.DefaultLogContentPolicy(); return &p }()}, nil)
	prev := globalSpanBox.Load()
	t.Cleanup(func() { globalSpanBox.Store(prev) })
	globalSpanBox.Store(exp.box)
	SwapSpanPolicyResolver(mapResolver{"": &strict})
	tp := sdktrace.NewTracerProvider(sdktrace.WithSyncer(exp))
	_, s := tp.Tracer("t").Start(context.Background(), "x")
	s.SetAttributes(attribute.String("user_agent.original", "Mozilla/5.0"))
	s.End()
	if _, ok := spanAttrs(mem.GetSpans()[0])["user_agent.original"]; ok {
		t.Fatal("swapped resolver not used")
	}
}
