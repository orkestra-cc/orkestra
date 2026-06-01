package middleware

import (
	"net/http"

	"github.com/orkestra/backend/pkg/sdk/ctxauth"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

// TenantBaggage enriches the current OpenTelemetry span with the
// tenant + user context resolved by the auth middleware. Phase 4.4 of
// ADR-0001 makes these attributes mandatory on every request span so
// SOC2 auditors can correlate actions back to a principal + tenant
// without joining against the audit trail.
//
// Must be registered AFTER RequireAuth (or OptionalAuth) so the
// context values are populated. When the request is anonymous the
// middleware is a no-op — missing values are never stamped as empty
// strings because semconv-style filtering on the collector side
// treats an empty tenant.id as a wildcard, not a miss.
//
// The span is resolved from r.Context() via trace.SpanFromContext;
// when the tracer provider is a no-op (OTEL collector not wired) the
// SetAttributes calls are zero-cost, so the middleware runs
// unconditionally across environments.
func TenantBaggage(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		span := trace.SpanFromContext(r.Context())
		if tenantID, ok := ctxauth.GetTenantID(r.Context()); ok {
			span.SetAttributes(attribute.String("tenant.id", tenantID))
		}
		if kind := ctxauth.TenantKindFromContext(r.Context()); kind != "" {
			span.SetAttributes(attribute.String("tenant.kind", kind))
		}
		if userID, ok := ctxauth.GetUserUUID(r.Context()); ok {
			span.SetAttributes(attribute.String("user.id", userID))
		}
		if role, ok := ctxauth.GetSystemRole(r.Context()); ok {
			span.SetAttributes(attribute.String("user.role", role))
		}
		next.ServeHTTP(w, r)
	})
}
