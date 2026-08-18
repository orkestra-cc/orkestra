package middleware

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"

	"github.com/golang-jwt/jwt/v5"
	"github.com/orkestra/backend/internal/shared/config"
)

// cookieDomainForAudience returns the refresh-cookie Domain attribute the
// middleware should mint for a request acting in the given audience.
// ADR-0003 PR-D D-9 split: operator requests get cfg.Auth.Cookie.OperatorDomain,
// client requests get ClientDomain. Anything else (or an unset per-tier
// value) returns "" — the cookie is minted without a Domain attribute,
// scoped to whatever host served the request.
func cookieDomainForAudience(cfg *config.Config, audience string) string {
	if cfg == nil {
		return ""
	}
	switch audience {
	case "operator":
		return cfg.Auth.Cookie.OperatorDomain
	case "client":
		return cfg.Auth.Cookie.ClientDomain
	}
	return ""
}

// AudienceContextKey holds the resolved JWT audience for the current
// request. Stamped by RequireAudience after a successful match.
type audienceCtxKey struct{}

// AudienceContextKey is exported so handlers can read the resolved
// audience without importing the unexported key type.
var AudienceContextKey = audienceCtxKey{}

// AudienceFromContext returns the audience stamped by RequireAudience, or
// the empty string when no audience was resolved (anonymous request, or
// the route is not audience-gated).
func AudienceFromContext(ctx context.Context) string {
	if v, ok := ctx.Value(AudienceContextKey).(string); ok {
		return v
	}
	return ""
}

// RequireAudience returns chi middleware that rejects requests whose
// bearer token carries a JWT `aud` claim not in the expected set. It is
// designed to be mounted on an audience-scoped chi.Mux at construction
// time so every route on that mux fails closed if a token from a
// different audience reaches it — defense in depth above per-route RBAC.
//
// expected is variadic: passing more than one audience turns the check
// into set membership rather than equality (e.g. the operator mux
// accepting both "operator" and "service"). The context value stamped on
// a match is the token's own matched `aud` element, not the gate's
// allowlist — so a caller downstream still sees which audience the
// caller actually presented.
//
// Behaviour (ADR-0003 PR-D D-3 hard cutover — no transition compat):
//
//   - Third-party webhook path        → pass through untouched (see below).
//   - No bearer token                 → pass through (public route or
//     downstream auth middleware will
//     enforce). Audience is not stamped.
//   - Token with `aud` in expected    → pass through, matched audience
//     stamped.
//   - Token with no `aud` claim       → 401 with code "audience_mismatch"
//     (v1 token rejected per PR-D cutover).
//   - Token with no aud in expected   → 401 with code "audience_mismatch".
//
// The webhook carve-out exists because a webhook's Authorization header
// belongs to the originating service, not to us: the SDI callback sends
// `Bearer <static webhook secret>`, which is not a JWT and so has no
// `aud` to match. Gating those paths here 401'd every authentic delivery
// at the mux, before ModuleGate, the poll throttle, the replay dedup, or
// the handler's own constant-time secret check ever ran. Those routes
// are not weakened by skipping this gate — their credential is checked
// by the handler; this gate could only ever have rejected them.
//
// The unverified claim parse here is intentionally cheap (no key, no
// signature check). The downstream auth middleware (RequireAuth /
// JWTValidator) re-parses and verifies the signature. A forged token
// with a legitimate-looking `aud` will satisfy this gate but still be
// rejected at the signature check, so the trust boundary is preserved.
func RequireAudience(expected ...string) func(http.Handler) http.Handler {
	allowed := make(map[string]struct{}, len(expected))
	for _, a := range expected {
		if a != "" {
			allowed[a] = struct{}{}
		}
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Fail-closed by construction: only the prefixes
			// WebhookRoutes names skip the gate — every other path,
			// including the rest of IsPublicRoute, keeps the hard-cutover
			// behaviour below. No audience is stamped: a webhook request
			// has none.
			if IsWebhookRoute(r.URL.Path) {
				next.ServeHTTP(w, r)
				return
			}

			token := extractBearerForAudience(r)
			if token == "" {
				next.ServeHTTP(w, r)
				return
			}

			for _, aud := range readUnverifiedAudiences(token) {
				if _, ok := allowed[aud]; ok {
					ctx := context.WithValue(r.Context(), AudienceContextKey, aud)
					next.ServeHTTP(w, r.WithContext(ctx))
					return
				}
			}

			w.Header().Set("Content-Type", "application/json")
			w.Header().Set("WWW-Authenticate", `Bearer error="audience_mismatch"`)
			w.WriteHeader(http.StatusUnauthorized)
			_ = json.NewEncoder(w).Encode(map[string]string{
				"error": "audience_mismatch",
				"code":  "audience_mismatch",
			})
		})
	}
}

// extractBearerForAudience parses an `Authorization: Bearer <token>` header.
// Mirrors the helper in auth.go but is duplicated locally so the audience
// middleware does not depend on the auth middleware (which imports the
// auth services package and would create a heavier dependency graph than
// this file warrants).
func extractBearerForAudience(r *http.Request) string {
	h := r.Header.Get("Authorization")
	if h == "" {
		return ""
	}
	parts := strings.Split(h, " ")
	if len(parts) != 2 || !strings.EqualFold(parts[0], "bearer") {
		return ""
	}
	return parts[1]
}

// readUnverifiedAudiences returns every string element of the JWT `aud`
// claim without verifying the signature. Returns nil on any parse failure
// or a missing claim.
//
// That nil/empty result is NOT a pass-through, despite what the name
// might suggest: RequireAudience's match loop has nothing to match
// against a nil slice, so the request falls straight through to the 401
// audience_mismatch response — the same outcome as a token whose `aud`
// names an audience outside the gate's allowlist. A bearer token with an
// unparseable or missing `aud` claim is rejected at THIS gate; it never
// reaches RequireAuth. (The only genuine pass-through in RequireAudience
// is a request carrying no bearer token at all, handled earlier by
// checking for an empty token string before this function is ever
// called.)
//
// MapClaims#GetAudience returns a list (RFC 7519 allows aud to be a string
// or string array). At PR-C every monolith-issued token carries a single
// string audience; a multi-valued `aud` claim (whether from a token that
// legitimately targets more than one surface, or any other shape a caller
// sends) is handled the same way now that RequireAudience checks set
// membership instead of equality. Every non-empty string element is
// returned so the gate can match against any of them.
func readUnverifiedAudiences(tokenString string) []string {
	t, _, err := jwt.NewParser().ParseUnverified(tokenString, jwt.MapClaims{})
	if err != nil {
		return nil
	}
	mc, ok := t.Claims.(jwt.MapClaims)
	if !ok {
		return nil
	}
	switch v := mc["aud"].(type) {
	case string:
		if v != "" {
			return []string{v}
		}
	case []any:
		var auds []string
		for _, item := range v {
			if s, ok := item.(string); ok && s != "" {
				auds = append(auds, s)
			}
		}
		return auds
	case []string:
		var auds []string
		for _, s := range v {
			if s != "" {
				auds = append(auds, s)
			}
		}
		return auds
	}
	return nil
}
