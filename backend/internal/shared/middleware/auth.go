package middleware

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/orkestra/backend/internal/core/auth/models"
	"github.com/orkestra/backend/internal/core/auth/services"
	"github.com/orkestra/backend/internal/shared/config"
	"github.com/orkestra/backend/internal/shared/errors"
	"github.com/orkestra/backend/internal/shared/iface"
	"github.com/orkestra/backend/internal/shared/utils"
)

// Context keys used by the auth middleware to carry identity and the
// resolved current-org context into downstream handlers. Kept as plain
// string constants so existing handlers that read ctx.Value("userUUID")
// directly keep working — they can migrate to the typed helpers below
// incrementally.
const (
	ctxUserUUID          = "userUUID"
	ctxUserEmail         = "userEmail"
	ctxSystemRole        = "userRole" // legacy key name: "userRole" still holds the system role
	ctxClaims            = "claims"
	ctxTenantID          = "tenantID"
	ctxTenantMemberships = "tenantMemberships"
	ctxTenantRoles       = "tenantRoles"
	// ctxTenantKind carries the tier ("internal" | "external") of the tenant
	// the current request is acting in. Populated from claims.ActingTenantKind
	// or resolved from the matched membership. See ADR-0001.
	ctxTenantKind = "tenantKind"
)

// TenantIDHeader is the HTTP header clients use to pick the current tenant
// for every request. The value must be a tenant UUID that the user is a
// member of, otherwise the request is rejected with 403.
const TenantIDHeader = "X-Tenant-ID"

type AuthMiddleware struct {
	jwtService   services.JWTService
	authService  services.AuthService
	tenant       iface.TenantProvider
	authz        iface.AuthzProvider
	errorManager *errors.Manager
	cookieName   string
	config       *config.Config
}

func NewAuthMiddleware(jwtService services.JWTService, errorManager *errors.Manager) *AuthMiddleware {
	return &AuthMiddleware{
		jwtService:   jwtService,
		authService:  nil,
		errorManager: errorManager,
		cookieName:   "access_token",
		config:       nil,
	}
}

func NewAuthMiddlewareWithConfig(jwtService services.JWTService, errorManager *errors.Manager, cfg *config.Config) *AuthMiddleware {
	cookieName := cfg.Auth.Cookie.Name
	if cookieName == "" {
		cookieName = "access_token"
	}
	return &AuthMiddleware{
		jwtService:   jwtService,
		authService:  nil,
		errorManager: errorManager,
		cookieName:   cookieName,
		config:       cfg,
	}
}

// SetAuthService sets the auth service for auto-refresh functionality.
func (m *AuthMiddleware) SetAuthService(authService services.AuthService) {
	m.authService = authService
}

// SetTenantProvider wires the tenant provider for org membership verification
// and entitlement checks. Called from main.go after all modules initialize.
func (m *AuthMiddleware) SetTenantProvider(t iface.TenantProvider) {
	m.tenant = t
}

// SetAuthzProvider wires the authz provider for permission evaluation.
// Called from main.go after all modules initialize.
func (m *AuthMiddleware) SetAuthzProvider(a iface.AuthzProvider) {
	m.authz = a
}

func (m *AuthMiddleware) RequireAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token := m.extractBearerToken(r)

		if token != "" {
			claims, err := m.jwtService.ValidateAccessToken(token)
			if err == nil {
				m.setUserContext(w, r, claims, next)
				return
			}

			if err != services.ErrTokenExpired && err != services.ErrInvalidToken {
				m.sendErrorResponse(w, r, errors.TokenInvalidError().
					WithOperation("require_auth").
					WithInternal(err).
					Build())
				return
			}
		}

		if m.authService != nil && m.config != nil && !isLogoutRequest(r) && !isOAuthCallbackRequest(r) {
			refreshToken := m.extractRefreshTokenFromCookie(r)
			if refreshToken != "" {
				securityCtx := &models.SecurityContext{
					IPAddress: utils.GetClientIP(r),
					Timestamp: time.Now(),
				}

				tokenResponse, err := m.authService.RefreshTokensWithRiskAssessment(r.Context(), refreshToken, securityCtx)
				if err == nil {
					cookieDomain := m.config.Auth.Cookie.Domain
					isSecure := m.config.Auth.Cookie.Secure
					utils.SetRefreshTokenCookie(w, m.cookieName, tokenResponse.RefreshToken, 7*24*3600, cookieDomain, isSecure)

					w.Header().Set("X-New-Access-Token", tokenResponse.AccessToken)
					w.Header().Set("X-Token-Refreshed", "true")

					claims, err := m.jwtService.ValidateAccessToken(tokenResponse.AccessToken)
					if err == nil {
						m.setUserContext(w, r, claims, next)
						return
					}
				}
			}
		}

		m.sendErrorResponse(w, r, errors.AuthenticationError("authentication required").
			WithOperation("require_auth").
			Build())
	})
}

// setUserContext injects user identity and the resolved current-tenant
// context into the request. Tenant resolution order:
//  1. claims.ActingTenantID — a JWT stamped for a specific tenant.
//  2. X-Tenant-ID header when the user is a member of that tenant.
//  3. claims.DefaultTenantID.
//  4. empty — only allowed on RequireGlobal() routes.
func (m *AuthMiddleware) setUserContext(w http.ResponseWriter, r *http.Request, claims *models.JWTClaims, next http.Handler) {
	ctx := r.Context()
	ctx = context.WithValue(ctx, ctxUserUUID, claims.UserUUID)
	ctx = context.WithValue(ctx, ctxUserEmail, claims.Email)
	ctx = context.WithValue(ctx, ctxSystemRole, claims.SystemRole)
	ctx = context.WithValue(ctx, ctxClaims, claims)
	ctx = context.WithValue(ctx, ctxTenantMemberships, claims.Memberships)

	tenantID, roles, kind, ok := resolveCurrentTenant(r, claims)
	if ok {
		ctx = context.WithValue(ctx, ctxTenantID, tenantID)
		ctx = context.WithValue(ctx, ctxTenantRoles, roles)
		if kind != "" {
			ctx = context.WithValue(ctx, ctxTenantKind, kind)
		}
	}

	// If the client sent X-Tenant-ID but it doesn't match any membership,
	// reject immediately so a stale header can't leak data from another
	// tenant. Missing header is fine — downstream middleware decides.
	if h := r.Header.Get(TenantIDHeader); h != "" && !ok {
		m.sendErrorResponse(w, r, errors.AuthorizationError("not a member of requested tenant").
			WithOperation("resolve_tenant").
			WithDetail("tenantId", h).
			Build())
		return
	}

	next.ServeHTTP(w, r.WithContext(ctx))
}

// resolveCurrentTenant picks the current tenant for this request. Returns
// the resolved tenantID, the user's roles in that tenant, the tenant kind
// ("internal" | "external" or empty if not known), and ok=false when no
// tenant can be resolved.
//
// Tier resolution order (ADR-0001):
//  1. claims.ActingTenantID + ActingTenantKind when set by the issuer — the
//     JWT was minted for a specific tenant, nothing else can override it.
//  2. X-Tenant-ID header when the user is a member of that tenant.
//  3. claims.DefaultTenantID.
func resolveCurrentTenant(r *http.Request, claims *models.JWTClaims) (string, []string, string, bool) {
	// Stamped-in tenant on the JWT itself: client-portal tokens in Phase 3
	// will always take this path. The header is ignored.
	if claims.ActingTenantID != "" {
		for _, mbr := range claims.Memberships {
			if mbr.TenantUUID == claims.ActingTenantID {
				kind := claims.ActingTenantKind
				if kind == "" {
					kind = mbr.TenantKind
				}
				return mbr.TenantUUID, mbr.Roles, kind, true
			}
		}
	}
	requested := r.Header.Get(TenantIDHeader)
	if requested != "" {
		for _, mbr := range claims.Memberships {
			if mbr.TenantUUID == requested {
				return mbr.TenantUUID, mbr.Roles, mbr.TenantKind, true
			}
		}
		return "", nil, "", false
	}
	if claims.DefaultTenantID != "" {
		for _, mbr := range claims.Memberships {
			if mbr.TenantUUID == claims.DefaultTenantID {
				return mbr.TenantUUID, mbr.Roles, mbr.TenantKind, true
			}
		}
	}
	return "", nil, "", false
}

// TenantKindFromContext returns the tier ("internal" | "external") for the
// tenant this request is acting in, or empty when no tier is known (global
// routes, or pre-ADR-0001 tokens). See ADR-0001.
func TenantKindFromContext(ctx context.Context) string {
	if v, ok := ctx.Value(ctxTenantKind).(string); ok {
		return v
	}
	return ""
}

// RequireTenantKind rejects any request whose resolved tenant is not of the
// expected kind. Use to gate Tier-1-only or Tier-2-only endpoints. Routes
// without a resolved tenant (global routes) are also rejected — callers that
// want tier-agnostic behavior simply don't use this middleware.
func (m *AuthMiddleware) RequireTenantKind(expected string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			kind := TenantKindFromContext(r.Context())
			if kind == "" {
				m.sendErrorResponse(w, r, errors.AuthorizationError("tenant context required").
					WithOperation("require_tenant_kind").
					WithDetail("expected", expected).
					Build())
				return
			}
			if kind != expected {
				m.sendErrorResponse(w, r, errors.AuthorizationError("wrong tenant tier for this route").
					WithOperation("require_tenant_kind").
					WithDetail("expected", expected).
					WithDetail("actual", kind).
					Build())
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// RequireInternalTenant is the convenience wrapper for operator-only routes.
// Equivalent to RequireTenantKind(iface.TenantKindInternal).
func (m *AuthMiddleware) RequireInternalTenant() func(http.Handler) http.Handler {
	return m.RequireTenantKind(iface.TenantKindInternal)
}

// RequireExternalTenant is the convenience wrapper for client-only routes.
// Equivalent to RequireTenantKind(iface.TenantKindExternal).
func (m *AuthMiddleware) RequireExternalTenant() func(http.Handler) http.Handler {
	return m.RequireTenantKind(iface.TenantKindExternal)
}

// Extract Bearer token from Authorization header.
func (m *AuthMiddleware) extractBearerToken(r *http.Request) string {
	authHeader := r.Header.Get("Authorization")
	if authHeader != "" {
		parts := strings.Split(authHeader, " ")
		if len(parts) == 2 && strings.ToLower(parts[0]) == "bearer" {
			return parts[1]
		}
	}
	return ""
}

// Extract refresh token from cookie.
func (m *AuthMiddleware) extractRefreshTokenFromCookie(r *http.Request) string {
	cookie, err := r.Cookie(m.cookieName)
	if err == nil && cookie.Value != "" {
		claims, err := m.jwtService.ParseUnverifiedClaims(cookie.Value)
		if err == nil && claims.TokenType == "refresh" {
			return cookie.Value
		}
	}
	return ""
}

func (m *AuthMiddleware) OptionalAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token := m.extractBearerToken(r)
		if token == "" {
			next.ServeHTTP(w, r)
			return
		}

		claims, err := m.jwtService.ValidateAccessToken(token)
		if err == nil {
			ctx := r.Context()
			ctx = context.WithValue(ctx, ctxUserUUID, claims.UserUUID)
			ctx = context.WithValue(ctx, ctxUserEmail, claims.Email)
			ctx = context.WithValue(ctx, ctxSystemRole, claims.SystemRole)
			ctx = context.WithValue(ctx, ctxClaims, claims)
			ctx = context.WithValue(ctx, ctxTenantMemberships, claims.Memberships)
			if tenantID, roles, kind, ok := resolveCurrentTenant(r, claims); ok {
				ctx = context.WithValue(ctx, ctxTenantID, tenantID)
				ctx = context.WithValue(ctx, ctxTenantRoles, roles)
				if kind != "" {
					ctx = context.WithValue(ctx, ctxTenantKind, kind)
				}
			}
			r = r.WithContext(ctx)
		}

		next.ServeHTTP(w, r)
	})
}

// isLogoutRequest checks if the current request is a logout request.
func isLogoutRequest(r *http.Request) bool {
	return r.Method == "POST" && (r.URL.Path == "/v1/auth/logout" || r.URL.Path == "/auth/logout")
}

// isOAuthCallbackRequest checks if the current request is an OAuth callback.
func isOAuthCallbackRequest(r *http.Request) bool {
	return r.Method == "GET" && (strings.Contains(r.URL.Path, "/auth/callback") ||
		strings.Contains(r.URL.Path, "/oauth/callback") ||
		r.URL.Path == "/v1/auth/google/callback" ||
		r.URL.Path == "/v1/auth/apple/callback" ||
		r.URL.Path == "/v1/auth/discord/callback" ||
		r.URL.Path == "/v1/auth/github/callback")
}

// --- Context accessors ---

// GetUserUUID extracts the user UUID from the request context.
func GetUserUUID(ctx context.Context) (string, bool) {
	userUUID, ok := ctx.Value(ctxUserUUID).(string)
	return userUUID, ok
}

// GetUserEmail extracts the user email from the request context.
func GetUserEmail(ctx context.Context) (string, bool) {
	email, ok := ctx.Value(ctxUserEmail).(string)
	return email, ok
}

// GetSystemRole extracts the user's global system role from the context.
func GetSystemRole(ctx context.Context) (string, bool) {
	role, ok := ctx.Value(ctxSystemRole).(string)
	return role, ok
}

// GetTenantID extracts the current tenant UUID from the request context.
// Returns ok=false when the request has no resolved tenant (global routes).
func GetTenantID(ctx context.Context) (string, bool) {
	tenantID, ok := ctx.Value(ctxTenantID).(string)
	if !ok || tenantID == "" {
		return "", false
	}
	return tenantID, true
}

// GetTenantRoles extracts the user's roles in the current tenant.
func GetTenantRoles(ctx context.Context) ([]string, bool) {
	roles, ok := ctx.Value(ctxTenantRoles).([]string)
	return roles, ok
}

// GetMemberships returns all tenant memberships the user has.
func GetMemberships(ctx context.Context) ([]models.TenantMembership, bool) {
	mbrs, ok := ctx.Value(ctxTenantMemberships).([]models.TenantMembership)
	return mbrs, ok
}

// --- RoleMiddleware implementation ---

func (m *AuthMiddleware) RequirePermission(permission string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if m.authz == nil {
				m.sendErrorResponse(w, r, errors.InternalError("authorization service not ready").
					WithOperation("require_permission").Build())
				return
			}
			userUUID, ok := GetUserUUID(r.Context())
			if !ok {
				m.sendErrorResponse(w, r, errors.AuthenticationError("authentication required").
					WithOperation("require_permission").Build())
				return
			}
			tenantID, hasTenant := GetTenantID(r.Context())
			if !hasTenant {
				m.sendErrorResponse(w, r, errors.AuthorizationError("tenant context required").
					WithOperation("require_permission").
					WithDetail("permission", permission).Build())
				return
			}
			allowed, err := m.authz.HasPermission(r.Context(), userUUID, tenantID, permission)
			if err != nil {
				m.sendErrorResponse(w, r, errors.InternalError("permission check failed").
					WithOperation("require_permission").
					WithInternal(err).Build())
				return
			}
			if !allowed {
				m.sendErrorResponse(w, r, errors.AuthorizationError("insufficient permissions").
					WithOperation("require_permission").
					WithDetail("permission", permission).
					WithDetail("tenantId", tenantID).Build())
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

func (m *AuthMiddleware) RequireSystemPermission(permission string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if m.authz == nil {
				m.sendErrorResponse(w, r, errors.InternalError("authorization service not ready").
					WithOperation("require_system_permission").Build())
				return
			}
			userUUID, ok := GetUserUUID(r.Context())
			if !ok {
				m.sendErrorResponse(w, r, errors.AuthenticationError("authentication required").
					WithOperation("require_system_permission").Build())
				return
			}
			allowed, err := m.authz.HasPermission(r.Context(), userUUID, "", permission)
			if err != nil {
				m.sendErrorResponse(w, r, errors.InternalError("permission check failed").
					WithOperation("require_system_permission").
					WithInternal(err).Build())
				return
			}
			if !allowed {
				m.sendErrorResponse(w, r, errors.AuthorizationError("insufficient system permissions").
					WithOperation("require_system_permission").
					WithDetail("permission", permission).Build())
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

func (m *AuthMiddleware) RequireEntitlement(feature string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if m.tenant == nil {
				m.sendErrorResponse(w, r, errors.InternalError("tenant service not ready").
					WithOperation("require_entitlement").Build())
				return
			}
			tenantID, ok := GetTenantID(r.Context())
			if !ok {
				m.sendErrorResponse(w, r, errors.AuthorizationError("tenant context required").
					WithOperation("require_entitlement").
					WithDetail("feature", feature).Build())
				return
			}
			allowed, err := m.tenant.HasEntitlement(r.Context(), tenantID, feature)
			if err != nil {
				m.sendErrorResponse(w, r, errors.InternalError("entitlement check failed").
					WithOperation("require_entitlement").
					WithInternal(err).Build())
				return
			}
			if !allowed {
				m.sendPlanLimitResponse(w, r, feature, tenantID)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// RequireCapability blocks the request unless the current tenant holds an
// active entitlement to the given capability ID in the tenant_entitlements
// projection (Phase 2). Returns 402 Payment Required — distinct from 403
// Forbidden — so the frontend can branch on the error and surface a
// subscription / upgrade prompt rather than an access-denied screen.
//
// Typical use: apply this AFTER RequirePermission so RBAC runs first and
// a 403 wins over a 402 when neither gate passes. The order does not affect
// correctness (both must permit the request), only which error the caller
// sees when multiple gates fail.
func (m *AuthMiddleware) RequireCapability(capabilityID string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if m.tenant == nil {
				m.sendErrorResponse(w, r, errors.InternalError("tenant service not ready").
					WithOperation("require_capability").Build())
				return
			}
			tenantID, ok := GetTenantID(r.Context())
			if !ok {
				m.sendErrorResponse(w, r, errors.AuthorizationError("tenant context required").
					WithOperation("require_capability").
					WithDetail("capability", capabilityID).Build())
				return
			}
			allowed, err := m.tenant.HasCapability(r.Context(), tenantID, capabilityID)
			if err != nil {
				m.sendErrorResponse(w, r, errors.InternalError("capability check failed").
					WithOperation("require_capability").
					WithInternal(err).Build())
				return
			}
			if !allowed {
				m.sendCapabilityRequiredResponse(w, r, capabilityID, tenantID)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// RequireGlobal is a pass-through for routes that don't need an org context
// (auth flows, org listing, user self-service). It just verifies the request
// is authenticated; RequireAuth on the parent router already handles that.
func (m *AuthMiddleware) RequireGlobal() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if _, ok := GetUserUUID(r.Context()); !ok {
				m.sendErrorResponse(w, r, errors.AuthenticationError("authentication required").
					WithOperation("require_global").Build())
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// RequireMFA blocks the request unless the access token records that MFA
// was completed for this session (amr contains "otp" or "webauthn").
// Returns 401 so the frontend can catch it, prompt for a code, and call
// /v1/auth/mfa/verify to obtain a stepped-up token.
func (m *AuthMiddleware) RequireMFA() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			claims, ok := r.Context().Value(ctxClaims).(*models.JWTClaims)
			if !ok || claims == nil {
				m.sendErrorResponse(w, r, errors.AuthenticationError("authentication required").
					WithOperation("require_mfa").Build())
				return
			}
			if !amrSatisfiesMFA(claims.AMR) {
				m.sendMFARequired(w, r)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// amrSatisfiesMFA checks whether any second-factor method is recorded on the
// token. Method names follow RFC 8176.
func amrSatisfiesMFA(amr []string) bool {
	for _, v := range amr {
		if v == "otp" || v == "webauthn" || v == "mfa" {
			return true
		}
	}
	return false
}

// sendMFARequired emits the structured 401 the frontend looks for to trigger
// a step-up prompt. The body mirrors sendErrorResponse's shape but adds the
// stable `code` field so the client can switch on it without parsing prose.
func (m *AuthMiddleware) sendMFARequired(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("WWW-Authenticate", `MFA error="mfa_required"`)
	w.WriteHeader(http.StatusUnauthorized)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"status": http.StatusUnauthorized,
		"title":  "mfa required",
		"detail": "this action requires a second authentication factor",
		"type":   "about:blank",
		"errors": []map[string]any{{
			"message":  "mfa required",
			"location": "require_mfa",
			"value":    "MFA_REQUIRED",
		}},
		"code": "mfa_required",
	})
}

// sendErrorResponse sends a structured error response using the error manager.
func (m *AuthMiddleware) sendErrorResponse(w http.ResponseWriter, r *http.Request, appErr *errors.AppError) {
	if correlationID := errors.GetCorrelationID(r.Context()); correlationID != "" {
		appErr.CorrelationID = correlationID
	}

	humaErr := m.errorManager.HandleError(r.Context(), appErr)

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(humaErr.GetStatus())

	response := map[string]interface{}{
		"status": humaErr.GetStatus(),
		"title":  appErr.Message,
		"detail": appErr.Message,
		"type":   "about:blank",
		"errors": []map[string]interface{}{
			{
				"message":  appErr.Message,
				"location": appErr.Operation,
				"value":    string(appErr.Code),
			},
		},
	}

	json.NewEncoder(w).Encode(response)
}

// sendPlanLimitResponse returns a 402 Payment Required when a feature isn't
// included in the tenant's plan. This is a first-class error distinct from
// 403 Forbidden so the frontend can surface an "upgrade your plan" UI.
func (m *AuthMiddleware) sendPlanLimitResponse(w http.ResponseWriter, r *http.Request, feature, tenantID string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusPaymentRequired)
	json.NewEncoder(w).Encode(map[string]interface{}{
		"status": http.StatusPaymentRequired,
		"title":  "plan limit",
		"detail": "feature not included in plan",
		"type":   "about:blank",
		"errors": []map[string]interface{}{{
			"message":  "feature not included in plan",
			"location": "require_entitlement",
			"value":    "PLAN_LIMIT",
		}},
		"feature":  feature,
		"tenantId": tenantID,
	})
}

// sendCapabilityRequiredResponse returns a 402 Payment Required when a
// capability-gated route is hit by a tenant that does not hold an active
// entitlement to that capability. Separate from sendPlanLimitResponse so
// the frontend can distinguish plan-feature misses from capability misses
// and surface the right flow (catalog subscribe vs plan upgrade).
func (m *AuthMiddleware) sendCapabilityRequiredResponse(w http.ResponseWriter, r *http.Request, capabilityID, tenantID string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusPaymentRequired)
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"status": http.StatusPaymentRequired,
		"title":  "capability required",
		"detail": "tenant is not entitled to this capability",
		"type":   "about:blank",
		"errors": []map[string]interface{}{{
			"message":  "capability required",
			"location": "require_capability",
			"value":    "CAPABILITY_REQUIRED",
		}},
		"capability": capabilityID,
		"tenantId":   tenantID,
		"code":       "capability_required",
	})
}
