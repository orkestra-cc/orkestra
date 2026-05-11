package middleware

import "strings"

// PublicRoutes enumerates the route prefixes that run without a resolved
// tenant context by design. Phase 5.2 of the tenancy plan uses this list
// as the known-quantity baseline for the baggage-coverage test — any
// request whose path does not match one of these prefixes is expected to
// carry tenant.id on its span.
//
// Additions must come with a reason: each entry covers a path that is
// either unauthenticated (setup, onboarding, OAuth callbacks, webhooks)
// or deliberately global (health checks, OpenAPI docs). Adding a
// business-logic route here silently opts it out of tenant-span
// enforcement, which is a security-visibility regression — do not.
var PublicRoutes = []string{
	"/health",
	"/metrics",
	"/docs",
	"/openapi.json",

	// Setup wizard — bootstraps the first administrator before any
	// tenant exists.
	"/v1/setup/",

	// OAuth / OIDC callback endpoints. The callback itself is
	// anonymous (the IdP is the principal until we mint a session).
	"/v1/auth/oauth/callback/",
	"/v1/auth/github/callback",
	"/v1/identity/oidc/callback",

	// Email preference endpoints — the unsubscribe token is the
	// authentication mechanism.
	"/v1/notification/unsubscribe",

	// Third-party webhooks. Principal is the originating service;
	// auth is via webhook-secret header, not JWT.
	"/v1/payments/webhooks/",
	"/v1/billing/webhooks/",
}

// IsPublicRoute reports whether the given request path is covered by the
// PublicRoutes prefix set. Callers should normalize the path to its URL
// form (leading slash, no query string) before calling.
func IsPublicRoute(path string) bool {
	for _, prefix := range PublicRoutes {
		if strings.HasPrefix(path, prefix) {
			return true
		}
	}
	return false
}
