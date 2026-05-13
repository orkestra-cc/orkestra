package scim

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"strings"

	"github.com/orkestra-cc/orkestra-sdk/iface"
	"github.com/orkestra/backend/internal/addons/identity/repository"
)

// ctxTenantIDKey mirrors the middleware.ctxTenantID string literal so
// downstream code reading with ctxauth.GetTenantID (and transitively
// tenantrepo.Scope) sees the tenant this SCIM request is acting in. The
// auth middleware uses the untyped string "tenantID" as its key; we
// match it by value, not by exporting a constant from the middleware
// package (which would require a deeper refactor).
const (
	ctxTenantIDKey   = "tenantID"
	ctxTenantKindKey = "tenantKind"
)

// BearerMiddleware validates the Bearer token on incoming SCIM requests
// and injects the resolved tenant into the request context. Any request
// lacking a valid Authorization header is rejected with a SCIM-shaped
// 401 error body (not the generic authentication error shape the other
// middleware uses — SCIM clients parse the error envelope).
func BearerMiddleware(tokens *repository.ScimTokenRepository, tenant iface.TenantProvider) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			raw := extractBearer(r.Header.Get("Authorization"))
			if raw == "" {
				writeScimError(w, http.StatusUnauthorized, "missing Authorization header")
				return
			}
			tokenHash := sha256Hex(raw)
			row, err := tokens.FindActiveByHash(r.Context(), tokenHash)
			if err != nil {
				writeScimError(w, http.StatusUnauthorized, "invalid SCIM bearer token")
				return
			}

			// Resolve tenant kind so downstream helpers (tenantrepo.
			// RequireExternalTenant) can gate if they ever need to. The
			// provider lookup is one Mongo read — acceptable overhead for
			// a SCIM request, which is typically low-RPS bulk sync traffic.
			kind := ""
			if tenant != nil {
				if t, err := tenant.GetTenant(r.Context(), row.TenantID); err == nil && t != nil {
					kind = t.Kind
				}
			}

			ctx := context.WithValue(r.Context(), ctxKey(ctxTenantIDKey), row.TenantID)
			if kind != "" {
				ctx = context.WithValue(ctx, ctxKey(ctxTenantKindKey), kind)
			}
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// ctxKey matches the untyped-string key the auth middleware uses. A
// dedicated newtype would be cleaner but would stop `tenantrepo.Scope`
// from finding the value — keep them string-literal-equivalent until a
// central context-key refactor lands.
func ctxKey(s string) any { return s }

// extractBearer parses the value of an Authorization header. Returns
// empty string for unparseable or non-Bearer values so the caller emits
// a single 401 rather than enumerating failure modes.
func extractBearer(header string) string {
	if header == "" {
		return ""
	}
	const prefix = "Bearer "
	if !strings.HasPrefix(header, prefix) {
		return ""
	}
	return strings.TrimSpace(header[len(prefix):])
}

func sha256Hex(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}

// writeScimError emits an RFC-7644 error envelope with the SCIM schema
// URI and the caller-supplied status/detail. Always sets the
// application/scim+json content type per the spec.
func writeScimError(w http.ResponseWriter, status int, detail string) {
	w.Header().Set("Content-Type", "application/scim+json")
	w.WriteHeader(status)
	body := NewError(statusText(status), detail)
	_ = json.NewEncoder(w).Encode(body)
}

func statusText(code int) string {
	switch code {
	case http.StatusUnauthorized:
		return "401"
	case http.StatusForbidden:
		return "403"
	case http.StatusNotFound:
		return "404"
	case http.StatusNotImplemented:
		return "501"
	default:
		if code >= 400 && code < 600 {
			return http.StatusText(code)
		}
		return ""
	}
}

// ScimNotImplemented writes a 501 SCIM error envelope. Used by every
// mutation endpoint in v1.
func ScimNotImplemented(w http.ResponseWriter, detail string) {
	writeScimError(w, http.StatusNotImplemented, detail)
}

// ScimJSON writes the given payload as application/scim+json with the
// supplied HTTP status.
func ScimJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/scim+json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}
