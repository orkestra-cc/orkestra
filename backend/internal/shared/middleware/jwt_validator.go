package middleware

import (
	"context"
	"crypto/rsa"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"net/http"
	"os"
	"strings"

	"github.com/golang-jwt/jwt/v5"
	authModels "github.com/orkestra/backend/internal/core/auth/models"
	"github.com/orkestra/backend/internal/shared/iface"
)

// JWTValidator is a lightweight middleware that validates RS256 JWTs using
// only the public key. It does not require the full auth module — designed
// for the AI service sidecar where we only need to verify tokens, not issue
// them.
//
// It satisfies module.RoleMiddleware so it can be dropped in wherever the
// main monolith uses AuthMiddleware.
type JWTValidator struct {
	publicKey *rsa.PublicKey
	tenant    iface.TenantProvider
	authz     iface.AuthzProvider
}

// NewJWTValidator creates a JWTValidator from a PEM-encoded RSA public key file.
func NewJWTValidator(publicKeyPath string) (*JWTValidator, error) {
	keyData, err := os.ReadFile(publicKeyPath)
	if err != nil {
		return nil, fmt.Errorf("jwt_validator: read public key %s: %w", publicKeyPath, err)
	}

	block, _ := pem.Decode(keyData)
	if block == nil {
		return nil, fmt.Errorf("jwt_validator: no PEM block found in %s", publicKeyPath)
	}

	pub, err := x509.ParsePKIXPublicKey(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("jwt_validator: parse public key: %w", err)
	}

	rsaPub, ok := pub.(*rsa.PublicKey)
	if !ok {
		return nil, fmt.Errorf("jwt_validator: key is not RSA")
	}

	return &JWTValidator{publicKey: rsaPub}, nil
}

// SetTenantProvider wires the tenant provider for entitlement checks.
func (v *JWTValidator) SetTenantProvider(t iface.TenantProvider) { v.tenant = t }

// SetAuthzProvider wires the authz provider for permission evaluation.
func (v *JWTValidator) SetAuthzProvider(a iface.AuthzProvider) { v.authz = a }

// RequireAuth validates the Bearer token and populates the request context
// with user identity plus the resolved current org (X-Org-ID header).
func (v *JWTValidator) RequireAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		tokenStr := extractBearer(r)
		if tokenStr == "" {
			writeErr(w, http.StatusUnauthorized, "missing bearer token")
			return
		}

		token, err := jwt.Parse(tokenStr, func(t *jwt.Token) (interface{}, error) {
			if _, ok := t.Method.(*jwt.SigningMethodRSA); !ok {
				return nil, fmt.Errorf("unexpected signing method: %v", t.Header["alg"])
			}
			return v.publicKey, nil
		})
		if err != nil || !token.Valid {
			writeErr(w, http.StatusUnauthorized, "invalid or expired token")
			return
		}

		mapClaims, ok := token.Claims.(jwt.MapClaims)
		if !ok {
			writeErr(w, http.StatusUnauthorized, "invalid claims")
			return
		}

		if getStr(mapClaims, "type") == "refresh" {
			writeErr(w, http.StatusUnauthorized, "refresh tokens not accepted")
			return
		}

		claims := parseClaims(mapClaims)

		ctx := r.Context()
		ctx = context.WithValue(ctx, ctxUserUUID, claims.UserUUID)
		ctx = context.WithValue(ctx, ctxUserEmail, claims.Email)
		ctx = context.WithValue(ctx, ctxSystemRole, claims.SystemRole)
		ctx = context.WithValue(ctx, ctxClaims, claims)
		ctx = context.WithValue(ctx, ctxOrgMemberships, claims.Memberships)

		orgID, roles, resolved := resolveCurrentOrg(r, claims)
		if resolved {
			ctx = context.WithValue(ctx, ctxOrgID, orgID)
			ctx = context.WithValue(ctx, ctxOrgRoles, roles)
		}
		if h := r.Header.Get(OrgIDHeader); h != "" && !resolved {
			writeErr(w, http.StatusForbidden, "not a member of requested organization")
			return
		}

		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func parseClaims(m jwt.MapClaims) *authModels.JWTClaims {
	c := &authModels.JWTClaims{
		UserUUID:     getStr(m, "sub"),
		Email:        getStr(m, "email"),
		SystemRole:   getStr(m, "srole"),
		TokenType:    getStr(m, "type"),
		DefaultOrgID: getStr(m, "dorg"),
	}
	if mbrs, ok := m["mbr"].([]interface{}); ok {
		for _, raw := range mbrs {
			obj, ok := raw.(map[string]any)
			if !ok {
				continue
			}
			mb := authModels.OrgMembership{OrgUUID: getStr(obj, "oid")}
			if roles, ok := obj["r"].([]interface{}); ok {
				for _, r := range roles {
					if s, ok := r.(string); ok {
						mb.Roles = append(mb.Roles, s)
					}
				}
			}
			c.Memberships = append(c.Memberships, mb)
		}
	}
	return c
}

// --- RoleMiddleware implementation ---

// RequirePermission evaluates a permission against the authz provider if
// one is wired, otherwise falls back to a role-only check suitable for the
// AI sidecar: the caller must either have a developer/administrator system
// role or the "administrator" role on the current org (from the JWT).
func (v *JWTValidator) RequirePermission(permission string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			userUUID, ok := GetUserUUID(r.Context())
			if !ok {
				writeErr(w, http.StatusUnauthorized, "authentication required")
				return
			}
			orgID, hasOrg := GetOrgID(r.Context())
			if v.authz != nil {
				if !hasOrg {
					writeErr(w, http.StatusForbidden, "org context required")
					return
				}
				allowed, err := v.authz.HasPermission(r.Context(), userUUID, orgID, permission)
				if err != nil || !allowed {
					writeErr(w, http.StatusForbidden, "insufficient permissions")
					return
				}
				next.ServeHTTP(w, r)
				return
			}
			// Fallback: JWT-only evaluation.
			if fallbackAllowedByRole(r.Context(), hasOrg) {
				next.ServeHTTP(w, r)
				return
			}
			writeErr(w, http.StatusForbidden, "insufficient permissions")
		})
	}
}

// RequireSystemPermission falls back to checking that the caller has a
// developer or administrator system role when authz is not wired.
func (v *JWTValidator) RequireSystemPermission(permission string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			userUUID, ok := GetUserUUID(r.Context())
			if !ok {
				writeErr(w, http.StatusUnauthorized, "authentication required")
				return
			}
			if v.authz != nil {
				allowed, err := v.authz.HasPermission(r.Context(), userUUID, "", permission)
				if err != nil || !allowed {
					writeErr(w, http.StatusForbidden, "insufficient system permissions")
					return
				}
				next.ServeHTTP(w, r)
				return
			}
			if role, _ := GetSystemRole(r.Context()); role == "super_admin" || role == "administrator" || role == "developer" {
				next.ServeHTTP(w, r)
				return
			}
			writeErr(w, http.StatusForbidden, "insufficient system permissions")
		})
	}
}

// RequireEntitlement is a pass-through when the sidecar has no tenant
// provider wired — the monolith that issued the JWT already enforced plan
// limits upstream, and the sidecar trusts valid tokens.
func (v *JWTValidator) RequireEntitlement(feature string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if v.tenant == nil {
				next.ServeHTTP(w, r)
				return
			}
			orgID, ok := GetOrgID(r.Context())
			if !ok {
				writeErr(w, http.StatusForbidden, "org context required")
				return
			}
			allowed, err := v.tenant.HasEntitlement(r.Context(), orgID, feature)
			if err != nil || !allowed {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusPaymentRequired)
				_ = json.NewEncoder(w).Encode(map[string]any{
					"status":  http.StatusPaymentRequired,
					"title":   "plan limit",
					"detail":  "feature not included in plan",
					"feature": feature,
					"orgId":   orgID,
				})
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// fallbackAllowedByRole implements a minimal role check for the AI sidecar:
// super_admin/administrator/developer system roles bypass the check; otherwise
// the user must hold one of those roles in the current org. Used only when no
// authz provider is wired.
func fallbackAllowedByRole(ctx context.Context, hasOrg bool) bool {
	if role, _ := GetSystemRole(ctx); role == "super_admin" || role == "administrator" || role == "developer" {
		return true
	}
	if !hasOrg {
		return false
	}
	roles, _ := GetOrgRoles(ctx)
	for _, r := range roles {
		if r == "super_admin" || r == "administrator" || r == "developer" {
			return true
		}
	}
	return false
}

func (v *JWTValidator) RequireGlobal() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if _, ok := GetUserUUID(r.Context()); !ok {
				writeErr(w, http.StatusUnauthorized, "authentication required")
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

func extractBearer(r *http.Request) string {
	h := r.Header.Get("Authorization")
	if h == "" {
		return ""
	}
	parts := strings.SplitN(h, " ", 2)
	if len(parts) == 2 && strings.EqualFold(parts[0], "bearer") {
		return parts[1]
	}
	return ""
}

func getStr(m jwt.MapClaims, key string) string {
	if v, ok := m[key]; ok {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return ""
}

func writeErr(w http.ResponseWriter, status int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"status": status,
		"title":  http.StatusText(status),
		"detail": msg,
	})
}
