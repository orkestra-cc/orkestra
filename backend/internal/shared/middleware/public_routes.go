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
var PublicRoutes = append([]string{
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
}, WebhookRoutes...)

// WebhookRoutes enumerates the third-party webhook prefixes. The
// principal is the originating service and the credential is that
// service's own scheme — never an Orkestra JWT: billing/SDI sends
// `Authorization: Bearer <static webhook secret>`, payments/Stripe signs
// the body into its own `Stripe-Signature` header.
//
// Split out of PublicRoutes (which still contains it — see the append
// above, so the two can never drift) because a second gate needs exactly
// this subset and not the rest: RequireAudience must let a non-JWT
// bearer through here, while the other public prefixes stay under the
// ADR-0003 PR-D D-3 hard cutover. Keep the trailing slash: it is what
// stops the prefix from matching a sibling route.
var WebhookRoutes = []string{
	"/v1/payments/webhooks/",
	"/v1/billing/webhooks/",
}

// IsPublicRoute reports whether the given request path is covered by the
// PublicRoutes prefix set. Callers should normalize the path to its URL
// form (leading slash, no query string) before calling.
func IsPublicRoute(path string) bool {
	return hasAnyPrefix(path, PublicRoutes)
}

// IsWebhookRoute reports whether the given request path is a third-party
// webhook endpoint, i.e. covered by the WebhookRoutes subset of
// PublicRoutes. Same normalization expectation as IsPublicRoute.
func IsWebhookRoute(path string) bool {
	return hasAnyPrefix(path, WebhookRoutes)
}

func hasAnyPrefix(path string, prefixes []string) bool {
	for _, prefix := range prefixes {
		if strings.HasPrefix(path, prefix) {
			return true
		}
	}
	return false
}
