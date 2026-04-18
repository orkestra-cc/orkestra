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
	ctxUserUUID       = "userUUID"
	ctxUserEmail      = "userEmail"
	ctxSystemRole     = "userRole" // legacy key name: "userRole" still holds the system role
	ctxClaims         = "claims"
	ctxOrgID          = "orgID"
	ctxOrgMemberships = "orgMemberships"
	ctxOrgRoles       = "orgRoles"
	// ctxTenantKind carries the tier ("internal" | "external") of the tenant
	// the current request is acting in. Populated from claims.ActingTenantKind
	// or resolved from the matched membership. See ADR-0001.
	ctxTenantKind = "tenantKind"
)

// OrgIDHeader is the HTTP header clients use to pick the current tenant
// for every request. The value must be an org UUID that the user is a
// member of, otherwise the request is rejected with 403.
const OrgIDHeader = "X-Org-ID"

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

// setUserContext injects user identity and the resolved current-org context
// into the request. Org resolution order:
//  1. X-Org-ID header, if present — must match one of the claims.Memberships
//  2. claims.DefaultOrgID — falls back to the user's default org
//  3. empty — only allowed on RequireGlobal() routes
func (m *AuthMiddleware) setUserContext(w http.ResponseWriter, r *http.Request, claims *models.JWTClaims, next http.Handler) {
	ctx := r.Context()
	ctx = context.WithValue(ctx, ctxUserUUID, claims.UserUUID)
	ctx = context.WithValue(ctx, ctxUserEmail, claims.Email)
	ctx = context.WithValue(ctx, ctxSystemRole, claims.SystemRole)
	ctx = context.WithValue(ctx, ctxClaims, claims)
	ctx = context.WithValue(ctx, ctxOrgMemberships, claims.Memberships)

	orgID, roles, kind, ok := resolveCurrentOrg(r, claims)
	if ok {
		ctx = context.WithValue(ctx, ctxOrgID, orgID)
		ctx = context.WithValue(ctx, ctxOrgRoles, roles)
		if kind != "" {
			ctx = context.WithValue(ctx, ctxTenantKind, kind)
		}
	}

	// If the client sent X-Org-ID but it doesn't match any membership,
	// reject immediately so a stale header can't leak data from another
	// tenant. Missing header is fine — downstream middleware decides.
	if h := r.Header.Get(OrgIDHeader); h != "" && !ok {
		m.sendErrorResponse(w, r, errors.AuthorizationError("not a member of requested organization").
			WithOperation("resolve_org").
			WithDetail("orgId", h).
			Build())
		return
	}

	next.ServeHTTP(w, r.WithContext(ctx))
}

// resolveCurrentOrg picks the current org for this request. Returns the
// resolved orgID, the user's roles in that org, the tenant kind
// ("internal" | "external" or empty if not known), and ok=false when no
// org can be resolved.
//
// Tier resolution order (ADR-0001):
//  1. claims.ActingTenantID + ActingTenantKind when set by the issuer — the
//     JWT was minted for a specific tenant, nothing else can override it.
//  2. X-Org-ID header when the user is a member of that org.
//  3. claims.DefaultOrgID.
func resolveCurrentOrg(r *http.Request, claims *models.JWTClaims) (string, []string, string, bool) {
	// Stamped-in tenant on the JWT itself: client-portal tokens in Phase 3
	// will always take this path. The header is ignored.
	if claims.ActingTenantID != "" {
		for _, mbr := range claims.Memberships {
			if mbr.OrgUUID == claims.ActingTenantID {
				kind := claims.ActingTenantKind
				if kind == "" {
					kind = mbr.TenantKind
				}
				return mbr.OrgUUID, mbr.Roles, kind, true
			}
		}
	}
	requested := r.Header.Get(OrgIDHeader)
	if requested != "" {
		for _, mbr := range claims.Memberships {
			if mbr.OrgUUID == requested {
				return mbr.OrgUUID, mbr.Roles, mbr.TenantKind, true
			}
		}
		return "", nil, "", false
	}
	if claims.DefaultOrgID != "" {
		for _, mbr := range claims.Memberships {
			if mbr.OrgUUID == claims.DefaultOrgID {
				return mbr.OrgUUID, mbr.Roles, mbr.TenantKind, true
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
			ctx = context.WithValue(ctx, ctxOrgMemberships, claims.Memberships)
			if orgID, roles, kind, ok := resolveCurrentOrg(r, claims); ok {
				ctx = context.WithValue(ctx, ctxOrgID, orgID)
				ctx = context.WithValue(ctx, ctxOrgRoles, roles)
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

// GetOrgID extracts the current org UUID from the request context.
// Returns ok=false when the request has no resolved org (global routes).
func GetOrgID(ctx context.Context) (string, bool) {
	orgID, ok := ctx.Value(ctxOrgID).(string)
	if !ok || orgID == "" {
		return "", false
	}
	return orgID, true
}

// GetOrgRoles extracts the user's roles in the current org.
func GetOrgRoles(ctx context.Context) ([]string, bool) {
	roles, ok := ctx.Value(ctxOrgRoles).([]string)
	return roles, ok
}

// GetMemberships returns all org memberships the user has.
func GetMemberships(ctx context.Context) ([]models.OrgMembership, bool) {
	mbrs, ok := ctx.Value(ctxOrgMemberships).([]models.OrgMembership)
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
			orgID, hasOrg := GetOrgID(r.Context())
			if !hasOrg {
				m.sendErrorResponse(w, r, errors.AuthorizationError("org context required").
					WithOperation("require_permission").
					WithDetail("permission", permission).Build())
				return
			}
			allowed, err := m.authz.HasPermission(r.Context(), userUUID, orgID, permission)
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
					WithDetail("orgId", orgID).Build())
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
			orgID, ok := GetOrgID(r.Context())
			if !ok {
				m.sendErrorResponse(w, r, errors.AuthorizationError("org context required").
					WithOperation("require_entitlement").
					WithDetail("feature", feature).Build())
				return
			}
			allowed, err := m.tenant.HasEntitlement(r.Context(), orgID, feature)
			if err != nil {
				m.sendErrorResponse(w, r, errors.InternalError("entitlement check failed").
					WithOperation("require_entitlement").
					WithInternal(err).Build())
				return
			}
			if !allowed {
				m.sendPlanLimitResponse(w, r, feature, orgID)
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
func (m *AuthMiddleware) sendPlanLimitResponse(w http.ResponseWriter, r *http.Request, feature, orgID string) {
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
		"feature": feature,
		"orgId":   orgID,
	})
}
