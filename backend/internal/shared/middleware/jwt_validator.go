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
	"time"

	"github.com/golang-jwt/jwt/v5"
	authModels "github.com/orkestra/backend/internal/core/auth/models"
	"github.com/orkestra/backend/pkg/sdk/ctxauth"
	"github.com/orkestra/backend/pkg/sdk/iface"
)

// JWTValidator is a lightweight middleware that validates RS256 JWTs using
// only the public key. It does not require the full auth module — designed
// for the AI service sidecar where we only need to verify tokens, not issue
// them.
//
// It satisfies module.RoleMiddleware so it can be dropped in wherever the
// main monolith uses AuthMiddleware.
type JWTValidator struct {
	publicKey         *rsa.PublicKey
	expectedIssuer    string
	tenant            iface.TenantProvider
	access            iface.AccessProvider
	authz             iface.AuthzProvider
	sessionRevocation SessionRevocationChecker
}

// SessionRevocationChecker is the narrow contract the middleware needs to
// verify that the caller's JWT sid has not been revoked. Deliberately
// minimal so both the monolith's full SessionRevocationService and any
// lightweight sidecar-local implementation can satisfy it.
type SessionRevocationChecker interface {
	IsRevoked(ctx context.Context, sid string) (bool, error)
}

// NewJWTValidator creates a JWTValidator from a PEM-encoded RSA public key file.
// env is the deployment environment (e.g. "production"); validation rejects
// tokens whose iss claim doesn't match `orkestra.<env>`. This matches the
// stamp applied by auth/services.NewJWTService so the monolith and sidecar
// agree on which environment's tokens they accept.
func NewJWTValidator(publicKeyPath, env string) (*JWTValidator, error) {
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

	if env == "" {
		env = "development"
	}

	return &JWTValidator{
		publicKey:      rsaPub,
		expectedIssuer: "orkestra." + env,
	}, nil
}

// SetTenantProvider wires the tenant provider for entitlement checks.
func (v *JWTValidator) SetTenantProvider(t iface.TenantProvider) { v.tenant = t }

// SetAccessProvider wires the polymorphic-owner capability surface.
// RequireCapability uses it instead of TenantProvider.HasCapability.
func (v *JWTValidator) SetAccessProvider(a iface.AccessProvider) { v.access = a }

// SetAuthzProvider wires the authz provider for permission evaluation.
func (v *JWTValidator) SetAuthzProvider(a iface.AuthzProvider) { v.authz = a }

// SetSessionRevocation wires the revoked-session checker. Optional: the
// sidecar trusts the monolith's upstream revocation check when unset, but
// wiring it provides defense in depth for requests that skip the
// monolith (e.g. RAG streaming that goes directly from the frontend to
// the sidecar).
func (v *JWTValidator) SetSessionRevocation(s SessionRevocationChecker) { v.sessionRevocation = s }

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

		if iss := getStr(mapClaims, "iss"); iss != v.expectedIssuer {
			// Cross-environment replay guard: a token minted in staging must
			// not validate in production even if the signing key somehow
			// overlaps (test deploy, leaked key, misconfig). Matching the
			// monolith's check in jwt_service.validateTokenEnhanced.
			writeErr(w, http.StatusUnauthorized, "invalid or expired token")
			return
		}

		// ADR-0003 PR-D D-3: aud is mandatory post-cutover. The host-mux
		// RequireAudience middleware enforces a specific value upstream,
		// but defense-in-depth here catches any bypass (e.g. a sidecar
		// surface that legitimately serves multiple audiences and must
		// still reject v1 tokens).
		if getStr(mapClaims, "aud") == "" {
			writeErr(w, http.StatusUnauthorized, "invalid or expired token")
			return
		}

		claims := parseClaims(mapClaims)

		if v.sessionRevocation != nil && claims.SessionID != "" {
			if revoked, _ := v.sessionRevocation.IsRevoked(r.Context(), claims.SessionID); revoked {
				w.Header().Set("WWW-Authenticate", `Bearer error="session_revoked"`)
				writeErr(w, http.StatusUnauthorized, "session revoked")
				return
			}
		}

		ctx := r.Context()
		ctx = context.WithValue(ctx, ctxauth.KeyUserUUID, claims.UserUUID)
		ctx = context.WithValue(ctx, ctxauth.KeyUserEmail, claims.Email)
		ctx = context.WithValue(ctx, ctxauth.KeySystemRole, claims.SystemRole)
		ctx = context.WithValue(ctx, ctxClaims, claims)
		ctx = context.WithValue(ctx, ctxTenantMemberships, claims.Memberships)

		tenantID, roles, kind, resolved := resolveCurrentTenant(r, claims)
		if resolved {
			ctx = context.WithValue(ctx, ctxauth.KeyTenantID, tenantID)
			ctx = context.WithValue(ctx, ctxauth.KeyTenantRoles, roles)
			if kind != "" {
				ctx = context.WithValue(ctx, ctxauth.KeyTenantKind, kind)
			}
		}
		if h := r.Header.Get(TenantIDHeader); h != "" && !resolved {
			writeErr(w, http.StatusForbidden, "not a member of requested tenant")
			return
		}

		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func parseClaims(m jwt.MapClaims) *authModels.JWTClaims {
	c := &authModels.JWTClaims{
		UserUUID:         getStr(m, "sub"),
		Email:            getStr(m, "email"),
		SystemRole:       getStr(m, "srole"),
		TokenType:        getStr(m, "type"),
		DefaultTenantID:  getStr(m, "dtid"),
		ActingTenantID:   getStr(m, "acting_tenant_id"),
		ActingTenantKind: getStr(m, "acting_tenant_kind"),
		SessionID:        getStr(m, "sid"),
		DeviceID:         getStr(m, "did"),
	}
	if mbrs, ok := m["mbr"].([]interface{}); ok {
		for _, raw := range mbrs {
			obj, ok := raw.(map[string]any)
			if !ok {
				continue
			}
			mb := authModels.TenantMembership{
				TenantUUID: getStr(obj, "tid"),
				TenantKind: getStr(obj, "k"),
			}
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
	if amr, ok := m["amr"].([]interface{}); ok {
		for _, v := range amr {
			if s, ok := v.(string); ok {
				c.AMR = append(c.AMR, s)
			}
		}
	}
	if v, ok := m["last_otp_at"].(float64); ok {
		c.LastOTPAt = int64(v)
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
			userUUID, ok := ctxauth.GetUserUUID(r.Context())
			if !ok {
				writeErr(w, http.StatusUnauthorized, "authentication required")
				return
			}
			tenantID, hasTenant := ctxauth.GetTenantID(r.Context())
			if v.authz != nil {
				if !hasTenant {
					writeErr(w, http.StatusForbidden, "tenant context required")
					return
				}
				allowed, err := v.authz.HasPermission(r.Context(), userUUID, tenantID, permission)
				if err != nil || !allowed {
					writeErr(w, http.StatusForbidden, "insufficient permissions")
					return
				}
				next.ServeHTTP(w, r)
				return
			}
			// Fallback: JWT-only evaluation.
			if fallbackAllowedByRole(r.Context(), hasTenant) {
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
			userUUID, ok := ctxauth.GetUserUUID(r.Context())
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
			if role, _ := ctxauth.GetSystemRole(r.Context()); role == "super_admin" || role == "administrator" || role == "developer" {
				next.ServeHTTP(w, r)
				return
			}
			writeErr(w, http.StatusForbidden, "insufficient system permissions")
		})
	}
}

// RequireCapability mirrors AuthMiddleware.RequireCapability for the AI
// sidecar. When the sidecar has no AccessProvider wired the monolith's
// upstream enforcement is trusted and the request passes through. When a
// provider is wired, the capability projection is consulted directly with
// the request's polymorphic owner (tenant when X-Tenant-ID is present,
// otherwise the calling user) and a miss returns 402.
func (v *JWTValidator) RequireCapability(capabilityID string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if v.access == nil {
				next.ServeHTTP(w, r)
				return
			}
			tenantUUID, ok := capabilityTenantFromRequest(r)
			if !ok {
				writeErr(w, http.StatusForbidden, "tenant context required")
				return
			}
			allowed, err := v.access.HasCapability(r.Context(), tenantUUID, capabilityID)
			if err != nil || !allowed {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusPaymentRequired)
				_ = json.NewEncoder(w).Encode(map[string]any{
					"status":     http.StatusPaymentRequired,
					"title":      "capability required",
					"detail":     "tenant is not entitled to this capability",
					"capability": capabilityID,
					"tenantId":   tenantUUID,
					"code":       "capability_required",
				})
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// fallbackAllowedByRole implements a minimal role check for the AI sidecar:
// super_admin/administrator/developer system roles bypass the check;
// otherwise the user must hold one of those roles in the current tenant.
// Used only when no authz provider is wired.
func fallbackAllowedByRole(ctx context.Context, hasTenant bool) bool {
	if role, _ := ctxauth.GetSystemRole(ctx); role == "super_admin" || role == "administrator" || role == "developer" {
		return true
	}
	if !hasTenant {
		return false
	}
	roles, _ := ctxauth.GetTenantRoles(ctx)
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
			if _, ok := ctxauth.GetUserUUID(r.Context()); !ok {
				writeErr(w, http.StatusUnauthorized, "authentication required")
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// RequireMFA mirrors AuthMiddleware.RequireMFA for the AI sidecar. Parsing
// the amr claim here is cheap; the sidecar never needs to touch the MFA
// service or database — the monolith records amr at token issuance.
func (v *JWTValidator) RequireMFA() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			claims, ok := r.Context().Value(ctxClaims).(*authModels.JWTClaims)
			if !ok || claims == nil {
				writeErr(w, http.StatusUnauthorized, "authentication required")
				return
			}
			for _, v := range claims.AMR {
				if v == "otp" || v == "webauthn" || v == "mfa" {
					next.ServeHTTP(w, r)
					return
				}
			}
			w.Header().Set("WWW-Authenticate", `MFA error="step_up_required"`)
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusUnauthorized)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"status": http.StatusUnauthorized,
				"title":  "mfa required",
				"detail": "this action requires a second authentication factor",
				"code":   "step_up_required",
			})
		})
	}
}

// RequireStepUp mirrors AuthMiddleware.RequireStepUp for the AI sidecar.
// The sidecar has no step-up-sensitive endpoints today (AI chat / RAG
// query are gated by capability + permission, not freshness), but the
// interface must be satisfied so sidecar modules compile against the
// same RoleMiddleware contract.
func (v *JWTValidator) RequireStepUp(maxAge time.Duration) func(http.Handler) http.Handler {
	if maxAge <= 0 {
		maxAge = 5 * time.Minute
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			claims, ok := r.Context().Value(ctxClaims).(*authModels.JWTClaims)
			if !ok || claims == nil {
				writeErr(w, http.StatusUnauthorized, "authentication required")
				return
			}
			fresh := false
			for _, a := range claims.AMR {
				if a == "otp" || a == "webauthn" || a == "mfa" {
					fresh = true
					break
				}
			}
			if !fresh || claims.LastOTPAt == 0 ||
				time.Since(time.Unix(claims.LastOTPAt, 0)) > maxAge {
				w.Header().Set("WWW-Authenticate", `MFA error="step_up_required"`)
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusUnauthorized)
				_ = json.NewEncoder(w).Encode(map[string]any{
					"status":        http.StatusUnauthorized,
					"title":         "step-up authentication required",
					"detail":        "this action requires a fresh second-factor verification",
					"code":          "step_up_required",
					"maxAgeSeconds": int(maxAge.Seconds()),
				})
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// RequireInternalTenant and RequireExternalTenant on the JWTValidator are
// implemented as no-op pass-throughs for the AI sidecar: the sidecar trusts
// the monolith's kind enforcement on the public boundary and never performs
// independent kind-based routing. Implemented to satisfy the
// module.RoleMiddleware interface so sidecar modules compile against the
// same contract.
func (v *JWTValidator) RequireInternalTenant() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler { return next }
}

func (v *JWTValidator) RequireExternalTenant() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler { return next }
}

// RequireLowRisk on the JWTValidator is a pass-through. The AI sidecar
// has no access to the monolith's auth_sessions collection, so it
// cannot independently score the session. The monolith gates high-risk
// calls at its public boundary; any request that reaches the sidecar
// has already passed that gate. Implemented to satisfy the
// module.RoleMiddleware interface.
func (v *JWTValidator) RequireLowRisk(threshold float64) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler { return next }
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
