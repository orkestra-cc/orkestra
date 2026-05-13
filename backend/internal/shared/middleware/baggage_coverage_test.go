package middleware

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/orkestra-cc/orkestra-sdk/ctxauth"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

// TestTenantBaggage_ProtectedRouteCarriesFullBaggage is the Phase 5.2
// "OTEL tenant attributes everywhere" verification. It installs a real
// OpenTelemetry tracer provider with an in-memory exporter, runs a
// simulated protected request through the actual TenantBaggage middleware,
// and asserts the exported span carries tenant.id, tenant.kind, user.id,
// and user.role — the four attributes auditors need to correlate a
// request back to a principal and tenant without joining against the
// audit trail.
//
// The test owns its tracer provider (otel.SetTracerProvider) so it does
// not depend on main.go's telemetry.Init; it can run in CI without any
// collector wired.
func TestTenantBaggage_ProtectedRouteCarriesFullBaggage(t *testing.T) {
	exp := tracetest.NewInMemoryExporter()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSyncer(exp))
	t.Cleanup(func() { _ = tp.Shutdown(context.Background()) })
	otel.SetTracerProvider(tp)

	// Chain: start a span → populate identity on context → TenantBaggage
	// reads the context → inner handler finishes. Mirrors the production
	// wiring in cmd/server/main.go where TenantBaggage is mounted
	// directly after RequireAuth.
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	h := tracingStart(tp)(seedIdentity("tenant-abc", "external", "user-xyz", "administrator")(TenantBaggage(inner)))

	r := httptest.NewRequest(http.MethodGet, "/v1/billing/invoices", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, r)
	if rec.Code != http.StatusOK {
		t.Fatalf("handler returned status %d", rec.Code)
	}

	spans := exp.GetSpans()
	if len(spans) == 0 {
		t.Fatal("expected at least one span, got none")
	}
	attrs := attrMap(spans[0].Attributes)
	for _, want := range []struct {
		key, value string
	}{
		{"tenant.id", "tenant-abc"},
		{"tenant.kind", "external"},
		{"user.id", "user-xyz"},
		{"user.role", "administrator"},
	} {
		got, ok := attrs[want.key]
		if !ok {
			t.Errorf("span missing attribute %s", want.key)
			continue
		}
		if got != want.value {
			t.Errorf("span attribute %s = %q, want %q", want.key, got, want.value)
		}
	}
}

// TestTenantBaggage_PublicRouteIsTolerated verifies the middleware is a
// no-op for routes where no tenant context exists — that is, a request
// whose path is in PublicRoutes. The baggage middleware must not
// synthesize empty-string attributes, because semconv filtering on the
// collector treats "" as a wildcard rather than as "absent".
func TestTenantBaggage_PublicRouteIsTolerated(t *testing.T) {
	exp := tracetest.NewInMemoryExporter()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSyncer(exp))
	t.Cleanup(func() { _ = tp.Shutdown(context.Background()) })
	otel.SetTracerProvider(tp)

	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	h := tracingStart(tp)(TenantBaggage(inner))

	for _, path := range []string{"/health", "/v1/setup/status", "/v1/payments/webhooks/stripe"} {
		if !IsPublicRoute(path) {
			t.Errorf("expected %q to be a public route", path)
		}
	}

	r := httptest.NewRequest(http.MethodGet, "/health", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, r)
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d", rec.Code)
	}

	spans := exp.GetSpans()
	if len(spans) == 0 {
		t.Fatal("no spans exported")
	}
	attrs := attrMap(spans[0].Attributes)
	for _, key := range []string{"tenant.id", "tenant.kind", "user.id", "user.role"} {
		if _, ok := attrs[key]; ok {
			t.Errorf("public route span should not carry %s, but did (value=%q)", key, attrs[key])
		}
	}
}

// TestIsPublicRoute_PrefixSemantics guards against a future edit of
// PublicRoutes that drops a trailing slash and accidentally widens or
// narrows the match set.
func TestIsPublicRoute_PrefixSemantics(t *testing.T) {
	cases := []struct {
		path string
		want bool
	}{
		{"/health", true},
		{"/health/detailed", true}, // prefix match is intentional
		{"/v1/setup/status", true},
		// /v1/setup (no trailing slash, no child path) intentionally
		// does not match — the wizard always calls a specific endpoint
		// like /v1/setup/status or /v1/setup/admin, and matching the
		// bare prefix would hide a hypothetical future "/v1/setupdoh"
		// route if one were ever added.
		{"/v1/setup", false},
		{"/v1/billing/invoices", false},
		{"/v1/payments/webhooks/stripe", true},
		{"/v1/payments/charges", false},
		{"/metrics", true},
	}
	for _, c := range cases {
		if got := IsPublicRoute(c.path); got != c.want {
			t.Errorf("IsPublicRoute(%q) = %v, want %v", c.path, got, c.want)
		}
	}
}

// tracingStart wraps the handler so every request starts a fresh span,
// mirroring what the otelhttp instrumentation (or the server's own
// chi-router middleware) would do in production.
func tracingStart(tp *sdktrace.TracerProvider) func(http.Handler) http.Handler {
	tracer := tp.Tracer("test")
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx, span := tracer.Start(r.Context(), "HTTP "+r.URL.Path)
			defer span.End()
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// seedIdentity installs the four context values that the real auth
// middleware would populate, so TenantBaggage downstream can stamp
// them onto the span. Using the package-private constants keeps the
// test honest: if the keys rename, this file moves with them or
// breaks immediately.
func seedIdentity(tenantID, tenantKind, userID, userRole string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx := r.Context()
			ctx = context.WithValue(ctx, ctxauth.KeyTenantID, tenantID)
			ctx = context.WithValue(ctx, ctxauth.KeyTenantKind, tenantKind)
			ctx = context.WithValue(ctx, ctxauth.KeyUserUUID, userID)
			ctx = context.WithValue(ctx, ctxauth.KeySystemRole, userRole)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// attrMap collapses []attribute.KeyValue to a map for friendlier
// lookups in assertions.
func attrMap(kvs []attribute.KeyValue) map[string]string {
	out := make(map[string]string, len(kvs))
	for _, kv := range kvs {
		out[string(kv.Key)] = kv.Value.Emit()
	}
	return out
}

// compile-time signal that strings import stays live if the file is
// slimmed down later.
var _ = strings.HasPrefix
