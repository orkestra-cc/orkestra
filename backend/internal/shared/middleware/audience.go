package middleware

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"

	"github.com/golang-jwt/jwt/v5"
)

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

// legacyAudience is the JWT `aud` value monolith-issued tokens carried
// before ADR-0003 PR-C. RequireAudience accepts it as equivalent to a
// missing `aud` claim so in-flight sessions survive the PR-C deploy
// without being forced to re-login. PR-D ("JWT v2 hard cutover") removes
// this transition path and starts rejecting both missing and legacy aud.
const legacyAudience = "orkestra-api"

// RequireAudience returns chi middleware that rejects requests whose
// bearer token carries a JWT `aud` claim not equal to expected. It is
// designed to be mounted on an audience-scoped chi.Mux at construction
// time so every route on that mux fails closed if a token from a
// different audience reaches it — defense in depth above per-route RBAC.
//
// Behaviour summary:
//
//   - No bearer token              → pass through (public route or
//                                     downstream auth middleware will
//                                     enforce). Audience is not stamped.
//   - Token with no `aud` claim    → pass through (legacy v1 token from
//                                     before PR-C). Audience not stamped.
//   - Token with `aud == legacy`   → pass through, transition compat.
//   - Token with `aud == expected` → pass through, audience stamped.
//   - Token with mismatched aud    → 401 with code "audience_mismatch".
//
// The unverified claim parse here is intentionally cheap (no key, no
// signature check). The downstream auth middleware (RequireAuth /
// JWTValidator) re-parses and verifies the signature. A forged token
// with a legitimate-looking `aud` will satisfy this gate but still be
// rejected at the signature check, so the trust boundary is preserved.
func RequireAudience(expected string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			token := extractBearerForAudience(r)
			if token == "" {
				next.ServeHTTP(w, r)
				return
			}

			aud := readUnverifiedAudience(token)
			switch aud {
			case "", legacyAudience:
				next.ServeHTTP(w, r)
				return
			case expected:
				ctx := context.WithValue(r.Context(), AudienceContextKey, aud)
				next.ServeHTTP(w, r.WithContext(ctx))
				return
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

// readUnverifiedAudience returns the JWT `aud` claim without verifying
// the signature. Returns "" on any parse failure — the middleware treats
// that the same as a missing claim and passes through, deferring rejection
// to RequireAuth where signature verification happens.
//
// MapClaims#GetAudience returns a list (RFC 7519 allows aud to be a string
// or string array). At PR-C every monolith-issued token carries a single
// string audience; PR-D may extend this to multi-aud once the client
// path is online. We accept any element matching expected.
func readUnverifiedAudience(tokenString string) string {
	t, _, err := jwt.NewParser().ParseUnverified(tokenString, jwt.MapClaims{})
	if err != nil {
		return ""
	}
	mc, ok := t.Claims.(jwt.MapClaims)
	if !ok {
		return ""
	}
	switch v := mc["aud"].(type) {
	case string:
		return v
	case []any:
		for _, item := range v {
			if s, ok := item.(string); ok && s != "" {
				return s
			}
		}
	case []string:
		for _, s := range v {
			if s != "" {
				return s
			}
		}
	}
	return ""
}
