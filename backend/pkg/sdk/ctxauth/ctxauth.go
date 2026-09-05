// Package ctxauth carries the SDK-visible subset of authentication state
// that addons read from the request context. The backend's AuthMiddleware
// stamps these values onto the context after JWT validation; addon
// handlers consume them through the typed getters below.
//
// Keys are exported as untyped string constants so legacy handlers that
// read ctx.Value("userUUID") directly keep working through the migration.
// New code (and extracted addons) should use the typed getters.
//
// Note: the JWT-claims-derived signals (session ID, AMR, MFA enrolment)
// stay backend-private because they depend on the auth module's claims
// type. When the SDK split goes external, exposing them needs a small
// iface.ClaimsAccessor interface — out of scope for Phase 2b.
package ctxauth

import "context"

// Exported context keys. Untyped string constants so existing handlers
// that read ctx.Value("userUUID") directly continue to work — string-
// equality matches the value AuthMiddleware writes.
const (
	KeyUserUUID           = "userUUID"
	KeyUserEmail          = "userEmail"
	KeySystemRole         = "userRole" // legacy key name; carries the system role
	KeyTenantID           = "tenantID"
	KeyTenantRoles        = "tenantRoles"
	KeyClientIP           = "clientIP"
	KeyTenantKind         = "tenantKind"
	KeyTenantImpersonated = "tenantImpersonated"
)

// GetUserUUID extracts the authenticated user's UUID from ctx.
// Returns ok=false on unauthenticated routes.
func GetUserUUID(ctx context.Context) (string, bool) {
	v, ok := ctx.Value(KeyUserUUID).(string)
	return v, ok
}

// GetUserEmail extracts the authenticated user's email from ctx.
func GetUserEmail(ctx context.Context) (string, bool) {
	v, ok := ctx.Value(KeyUserEmail).(string)
	return v, ok
}

// GetSystemRole extracts the user's global system role
// (super_admin / administrator / developer / manager / operator / guest)
// from the request's `srole` JWT claim.
//
// NOT an authorization source. The claim is minted at login and is
// therefore up to one access-token lifetime stale — a role changed since
// then is not reflected here, which is precisely the window the
// role-change propagation exists to close (spec §4.6 D27/D28). Any guard
// that decides what a caller may *do* with their role must read it from
// the database instead; the operator-tier role guards
// (internal/core/user/handlers) and the setup recovery gate
// (internal/shared/setup) both do.
//
// Its remaining in-tree consumers fall into three groups, and only the
// first is genuinely free of authorization weight:
//
//   - Non-authorising. Request logging and tenant baggage (observability),
//     the navigation menu's role filter (shaping — every route behind a
//     menu entry re-checks permissions), and GET /v1/tenants/{id}/authz/me,
//     which echoes the claim back in its response body without deciding
//     anything with it. A stale value costs a mislabelled log line, a menu
//     entry that 403s when clicked, or one stale field in a read.
//   - Authorising, but triple-guarded. The dev-token fallbacks in
//     internal/core/authz/module.go and internal/core/user/handlers read
//     this claim to give a SYNTHETIC dev-token principal a role, because
//     such a principal has no database row by design. Safe only because
//     they additionally require a non-production(-like) deployment and the
//     `dev-` UUID prefix that only POST /dev/token mints. Describe these
//     as "authorising but guarded", never as non-authorising: it is the
//     guards, not the call site, that carry the safety argument.
//   - Authorising on the claim alone, and therefore NOT a pattern to copy:
//     internal/shared/middleware/jwt_validator.go's RequireSystemPermission
//     fallback and fallbackAllowedByRole, which admit
//     super_admin|administrator|developer straight off `srole` when no
//     authz provider is wired. Both are unreachable in this repository —
//     NewJWTValidator has no non-test caller, so that AI-sidecar path is
//     fork-only — but a fork that builds the sidecar inherits exactly the
//     staleness D28 closed elsewhere, and should wire an AccessProvider
//     rather than rely on the fallback.
func GetSystemRole(ctx context.Context) (string, bool) {
	v, ok := ctx.Value(KeySystemRole).(string)
	return v, ok
}

// GetTenantID extracts the current tenant UUID resolved from the
// X-Tenant-ID header (or single-membership fallback). Returns ok=false
// on global routes that don't carry a tenant scope.
func GetTenantID(ctx context.Context) (string, bool) {
	v, ok := ctx.Value(KeyTenantID).(string)
	if !ok || v == "" {
		return "", false
	}
	return v, true
}

// GetTenantRoles returns the role names the user holds in the current tenant.
func GetTenantRoles(ctx context.Context) ([]string, bool) {
	v, ok := ctx.Value(KeyTenantRoles).([]string)
	return v, ok
}

// GetClientIP returns the caller's source IP as resolved by the middleware
// boundary. Background jobs and service-to-service calls outside the HTTP
// stack return ok=false.
func GetClientIP(ctx context.Context) (string, bool) {
	v, ok := ctx.Value(KeyClientIP).(string)
	if !ok || v == "" {
		return "", false
	}
	return v, true
}

// TenantKindFromContext returns the tier of the tenant the current request
// is acting in: "internal" (operator-side) or "external" (client-side).
// Returns "" on routes without a resolved tenant.
func TenantKindFromContext(ctx context.Context) string {
	v, _ := ctx.Value(KeyTenantKind).(string)
	return v
}

// IsImpersonating reports whether the current tenant context was resolved
// via the operator-admin impersonation bypass rather than a real membership.
// Handlers that guard self-destructive actions consult this flag.
func IsImpersonating(ctx context.Context) bool {
	v, _ := ctx.Value(KeyTenantImpersonated).(bool)
	return v
}

// WithTenantKind stamps a tenant kind onto ctx so downstream gates can
// read it without re-resolving the tenant. Used by AuthMiddleware after
// the kind is computed from the active membership.
func WithTenantKind(ctx context.Context, kind string) context.Context {
	return context.WithValue(ctx, KeyTenantKind, kind)
}

// WithClientIP stamps a client IP onto ctx. Exposed so tests can exercise
// IP-gated policies without booting the middleware chain; production code
// paths receive the IP from utils.GetClientIP at the middleware boundary.
func WithClientIP(ctx context.Context, ip string) context.Context {
	return context.WithValue(ctx, KeyClientIP, ip)
}
