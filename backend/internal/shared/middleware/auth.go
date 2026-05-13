package middleware

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/orkestra-cc/orkestra-sdk/ctxauth"
	"github.com/orkestra-cc/orkestra-sdk/iface"
	"github.com/orkestra-cc/orkestra-sdk/metrics"
	"github.com/orkestra/backend/internal/core/auth/models"
	"github.com/orkestra/backend/internal/core/auth/services"
	userModels "github.com/orkestra/backend/internal/core/user/models"
	"github.com/orkestra/backend/internal/shared/config"
	"github.com/orkestra/backend/internal/shared/errors"
	"github.com/orkestra/backend/internal/shared/utils"
)

// slogString is a terse wrapper for slog.String so the warn-mode log site
// stays readable. Kept unexported — adopt slog.String directly if another
// file in the package starts using it.
func slogString(key, value string) slog.Attr { return slog.String(key, value) }

// Context keys for auth-internal values that did NOT move to pkg/sdk/ctxauth.
// The values that DID move (userUUID/userEmail/systemRole/tenantID/tenantRoles/
// clientIP/tenantKind/tenantImpersonated) are written using the exported
// ctxauth.Key* constants. Search for "ctxauth.Key" to find the stamping
// sites; legacy handlers that read ctx.Value("userUUID") directly still
// work because the SDK key constants are untyped strings with the same
// values.
const (
	ctxClaims            = "claims"
	ctxTenantMemberships = "tenantMemberships"
)

// TenantIDHeader is the HTTP header clients use to pick the current tenant
// for every request. The value must be a tenant UUID that the user is a
// member of, otherwise the request is rejected with 403.
const TenantIDHeader = "X-Tenant-ID"

// SessionRiskLookup resolves the most recent risk score for a session
// UUID. Implementations typically read auth_sessions by sid. A nil
// error with score == 0 is legitimate (session absent or scorer not
// yet wired) — callers treat it as zero risk and pass the request
// through.
type SessionRiskLookup func(ctx context.Context, sessionID string) (float64, error)

// MFAEnrollmentLookup resolves whether a user has any MFA factor (TOTP
// or WebAuthn) enrolled. The audience argument lets the implementation
// dispatch to the operator vs client mfa_factors collection without the
// middleware having to know about tiering. Returns (false, nil) when no
// factor is enrolled — a non-nil error is a lookup failure (e.g. Mongo
// outage), which the gate treats as "presence unknown" and fails closed
// to the legacy step-up path so a degraded DB never silently weakens the
// gate.
type MFAEnrollmentLookup func(ctx context.Context, audience, userUUID string) (hasFactor bool, err error)

// StepUpPolicy is the subset of *services.AuthPolicyService the middleware
// needs to decide between password-reconfirm and mfa-enrollment-required
// when the user has no factor. The interface is declared here (not in
// shared/iface) to avoid a package cycle — AuthMiddleware already imports
// auth/services, but the policy reader is parameter-shaped so a test can
// substitute a fake. Nil-tolerant: when unset, the gate falls back to the
// legacy "always emit step_up_required" behaviour.
type StepUpPolicy interface {
	MFARequired(user *userModels.User, memberships []models.OrgMembership) bool
}

type AuthMiddleware struct {
	jwtService        services.JWTService
	authService       services.AuthService
	tenant            iface.TenantProvider
	access            iface.AccessProvider
	authz             iface.AuthzProvider
	auditSink         iface.AuditSink
	sessionRevocation services.SessionRevocationService
	sessionRiskLookup SessionRiskLookup
	mfaEnrollment     MFAEnrollmentLookup
	stepUpPolicy      StepUpPolicy
	users             iface.UserProvider
	errorManager      *errors.Manager
	cookieName        string
	config            *config.Config

	// impersonationDedupe throttles the admin.tenant.impersonate audit
	// event to one emit per (actorUserUUID|targetTenantID) every
	// impersonationDedupeTTL so a page that fires dozens of requests
	// generates a single audit row. nil when auditSink is unset.
	impersonationDedupe    sync.Map
	impersonationDedupeTTL time.Duration
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

// SetAccessProvider wires the polymorphic-owner capability surface.
// RequireCapability uses it to evaluate entitlements for either a tenant
// (X-Tenant-ID present) or the calling user (no tenant context). Called
// from main.go after the tenant module initializes.
func (m *AuthMiddleware) SetAccessProvider(a iface.AccessProvider) {
	m.access = a
}

// SetAuthzProvider wires the authz provider for permission evaluation.
// Called from main.go after all modules initialize.
func (m *AuthMiddleware) SetAuthzProvider(a iface.AuthzProvider) {
	m.authz = a
}

// SetAuditSink wires the compliance audit sink for impersonation event
// emission. Optional — if the compliance module is disabled the middleware
// falls back to the structured request logger. Called from main.go after
// InitAll.
func (m *AuthMiddleware) SetAuditSink(sink iface.AuditSink) {
	m.auditSink = sink
	if m.impersonationDedupeTTL == 0 {
		m.impersonationDedupeTTL = 60 * time.Second
	}
}

// SetSessionRevocation wires the Redis-backed revoked-session checker.
// When set, every authenticated request verifies the token's sid claim is
// not present in the revocation set before running the handler, so logout
// and admin-kill take effect instantly rather than after the access-token
// TTL. Optional — when unset, revocation falls back to access-token TTL.
func (m *AuthMiddleware) SetSessionRevocation(s services.SessionRevocationService) {
	m.sessionRevocation = s
}

// SetSessionRiskLookup wires the per-sid risk-score resolver consumed
// by RequireLowRisk. Typically bound post-InitAll in main.go so the
// lookup can read the same auth_sessions collection the scorer writes.
// Optional — when unset, RequireLowRisk is a pass-through.
func (m *AuthMiddleware) SetSessionRiskLookup(lookup SessionRiskLookup) {
	m.sessionRiskLookup = lookup
}

// SetMFAEnrollmentLookup wires the per-tier MFA factor presence resolver
// consumed by RequireStepUp. When set, a request that fails the freshness
// check is split into two paths: users with no factor enrolled (and no
// policy requirement) receive a `password_confirm_required` envelope so
// the frontend can collect a password reconfirm instead of asking for an
// MFA code they can't produce. Optional — nil falls back to legacy
// behaviour (every step-up failure → step_up_required).
func (m *AuthMiddleware) SetMFAEnrollmentLookup(lookup MFAEnrollmentLookup) {
	m.mfaEnrollment = lookup
}

// SetStepUpPolicy wires the policy reader RequireStepUp uses to decide
// whether a no-factor user must enroll first (`mfa_enrollment_required`)
// or may bypass with a password reconfirm. Optional — nil receivers
// default to "no role requires MFA", which keeps the gate permissive in
// test setups that don't wire the auth policy.
func (m *AuthMiddleware) SetStepUpPolicy(p StepUpPolicy) {
	m.stepUpPolicy = p
}

// SetUserProvider wires the user lookup so RequireStepUp can resolve the
// caller's record (role + memberships) when deciding between the
// password-reconfirm and mfa-enroll branches. Optional — nil falls back
// to claims-only reasoning (system role from `srole`, memberships from
// `mbr`), which is sufficient for the role-based MFA requirement check.
func (m *AuthMiddleware) SetUserProvider(u iface.UserProvider) {
	m.users = u
}

func (m *AuthMiddleware) RequireAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token := m.extractBearerToken(r)

		if token != "" {
			claims, err := m.jwtService.ValidateAccessToken(token)
			if err == nil {
				if m.isSessionRevoked(r, claims) {
					m.sendSessionRevoked(w, r)
					return
				}
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
					// ADR-0003 PR-D D-9: write the rotated refresh cookie
					// scoped to the request's audience so an operator
					// silent-refresh stays on `console.*` and a client
					// silent-refresh stays on `api.*`. Falls back to the
					// legacy single-host Domain when the per-tier value
					// is empty (single-host deployments).
					cookieDomain := cookieDomainForAudience(m.config, AudienceFromContext(r.Context()))
					isSecure := m.config.Auth.Cookie.Secure
					utils.SetRefreshTokenCookie(w, m.cookieName, tokenResponse.RefreshToken, 7*24*3600, cookieDomain, isSecure)

					w.Header().Set("X-New-Access-Token", tokenResponse.AccessToken)
					w.Header().Set("X-Token-Refreshed", "true")

					claims, err := m.jwtService.ValidateAccessToken(tokenResponse.AccessToken)
					if err == nil {
						if m.isSessionRevoked(r, claims) {
							m.sendSessionRevoked(w, r)
							return
						}
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

// isSessionRevoked returns true when the revocation checker is wired and
// reports the token's sid as revoked. Errors are treated as "not revoked"
// by the service (fail-open on Redis outage) — see
// SessionRevocationService's type comment.
func (m *AuthMiddleware) isSessionRevoked(r *http.Request, claims *models.JWTClaims) bool {
	if m.sessionRevocation == nil || claims == nil || claims.SessionID == "" {
		return false
	}
	revoked, _ := m.sessionRevocation.IsRevoked(r.Context(), claims.SessionID)
	return revoked
}

// sendSessionRevoked emits the structured 401 that tells the client to
// drop its access token and re-authenticate. The `session_revoked` code
// is distinct from the generic `authentication required` path so the
// frontend can choose a cleaner UX than the token-expired toast.
func (m *AuthMiddleware) sendSessionRevoked(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("WWW-Authenticate", `Bearer error="session_revoked"`)
	w.WriteHeader(http.StatusUnauthorized)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"status": http.StatusUnauthorized,
		"title":  "session revoked",
		"detail": "this session has been revoked; please sign in again",
		"type":   "about:blank",
		"errors": []map[string]any{{
			"message":  "session revoked",
			"location": "require_auth",
			"value":    "SESSION_REVOKED",
		}},
		"code": "session_revoked",
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
	ctx = context.WithValue(ctx, ctxauth.KeyUserUUID, claims.UserUUID)
	ctx = context.WithValue(ctx, ctxauth.KeyUserEmail, claims.Email)
	ctx = context.WithValue(ctx, ctxauth.KeySystemRole, claims.SystemRole)
	ctx = context.WithValue(ctx, ctxClaims, claims)
	ctx = context.WithValue(ctx, ctxTenantMemberships, claims.Memberships)
	if ip := utils.GetClientIP(r); ip != "" {
		ctx = context.WithValue(ctx, ctxauth.KeyClientIP, ip)
	}

	tenantID, roles, kind, ok := resolveCurrentTenant(r, claims)
	if ok {
		ctx = context.WithValue(ctx, ctxauth.KeyTenantID, tenantID)
		ctx = context.WithValue(ctx, ctxauth.KeyTenantRoles, roles)
		if kind != "" {
			ctx = context.WithValue(ctx, ctxauth.KeyTenantKind, kind)
		}
	}

	// If the client sent X-Tenant-ID but it doesn't match any membership,
	// try the operator-admin impersonation bypass: holders of
	// system.tenants.admin can act in any tenant. Falls through to 403 for
	// non-admins so a stale header can't leak data from another tenant.
	if h := r.Header.Get(TenantIDHeader); h != "" && !ok {
		impCtx, _, decision := m.tryImpersonationBypass(ctx, r, claims, h)
		switch decision {
		case impersonationBypassMFARequired:
			m.sendMFARequired(w, r)
			return
		case impersonationBypassDenied:
			m.sendErrorResponse(w, r, errors.AuthorizationError("not a member of requested tenant").
				WithOperation("resolve_tenant").
				WithDetail("tenantId", h).
				Build())
			return
		case impersonationBypassAllowed:
			ctx = impCtx
		}
	}

	next.ServeHTTP(w, r.WithContext(ctx))
}

// impersonationBypassDecision is the tri-state result tryImpersonationBypass
// returns so the caller can distinguish "you're not allowed" (403) from
// "you'd be allowed but need a fresh second factor first" (401 step-up).
// The middleware emits a different response for each — folding them into a
// single ok=false would have hidden the MFA branch from the client.
type impersonationBypassDecision int

const (
	impersonationBypassDenied impersonationBypassDecision = iota
	impersonationBypassAllowed
	impersonationBypassMFARequired
)

// Audit action names for the impersonation event, split by target shape so
// SOC2/security review can tell apart sensitive personal-tenant access from
// routine business-tenant operator work. See Phase 7 of the Unified Client
// Aggregate plan.
const (
	auditActionImpersonatePersonal = "admin.tenant.impersonate.personal"
	auditActionImpersonateBusiness = "admin.tenant.impersonate.business"
)

// tryImpersonationBypass resolves a non-member X-Tenant-ID when the caller
// holds system.tenants.admin. On success it returns an enriched context
// stamping tenantID + looked-up kind + synthetic administrator roles + an
// impersonation flag, and emits one de-duped admin.tenant.impersonate.*
// audit event per (actor, tenant, TTL). The decision discriminates:
//   - Denied: missing services, no admin permission, tenant lookup failed.
//   - MFARequired: target is a personal tenant (IsCompany=false +
//     SignupChannel=self_serve) and the actor's session has not completed
//     a second factor — caller surfaces 401 step_up_required.
//   - Allowed: bypass applied; caller adopts the enriched context.
func (m *AuthMiddleware) tryImpersonationBypass(
	ctx context.Context,
	r *http.Request,
	claims *models.JWTClaims,
	requestedTenantID string,
) (context.Context, string, impersonationBypassDecision) {
	if m.authz == nil || m.tenant == nil {
		return ctx, "", impersonationBypassDenied
	}
	allowed, err := m.authz.HasPermission(ctx, claims.UserUUID, "", "system.tenants.admin")
	if err != nil || !allowed {
		return ctx, "", impersonationBypassDenied
	}
	target, err := m.tenant.GetTenant(ctx, requestedTenantID)
	if err != nil || target == nil {
		return ctx, "", impersonationBypassDenied
	}

	personal := isPersonalTenant(target)
	if personal && !amrSatisfiesMFA(claims.AMR) {
		return ctx, target.Kind, impersonationBypassMFARequired
	}

	ctx = context.WithValue(ctx, ctxauth.KeyTenantID, target.UUID)
	ctx = context.WithValue(ctx, ctxauth.KeyTenantRoles, []string{"administrator"})
	if target.Kind != "" {
		ctx = context.WithValue(ctx, ctxauth.KeyTenantKind, target.Kind)
	}
	ctx = context.WithValue(ctx, ctxauth.KeyTenantImpersonated, true)

	action := auditActionImpersonateBusiness
	if personal {
		action = auditActionImpersonatePersonal
	}
	m.recordImpersonationAudit(ctx, r, claims, target, action)
	return ctx, target.Kind, impersonationBypassAllowed
}

// isPersonalTenant matches the canonical "personal tenant" predicate from
// the Unified Client Aggregate plan: a Tier-2 self-serve signup that has
// not been promoted to a business entity. Anything else (companies,
// sales-assisted onboarding, seeded ops tenants) is treated as business.
func isPersonalTenant(t *iface.Tenant) bool {
	return t != nil && !t.IsCompany && t.SignupChannel == iface.SignupChannelSelfServe
}

// recordImpersonationAudit emits a dedupe'd audit event so a single page
// load that fires many XHRs produces one audit row per minute. action is
// the split admin.tenant.impersonate.{personal,business} variant chosen by
// the caller based on the target's IsCompany+SignupChannel shape. When the
// compliance sink is not registered, the event is silently dropped — the
// impersonation still works, it just isn't recorded.
func (m *AuthMiddleware) recordImpersonationAudit(
	ctx context.Context,
	r *http.Request,
	claims *models.JWTClaims,
	target *iface.Tenant,
	action string,
) {
	if m.auditSink == nil {
		return
	}
	key := claims.UserUUID + "|" + target.UUID
	now := time.Now()
	if last, ok := m.impersonationDedupe.Load(key); ok {
		if lastTime, ok := last.(time.Time); ok && now.Sub(lastTime) < m.impersonationDedupeTTL {
			return
		}
	}
	m.impersonationDedupe.Store(key, now)

	m.auditSink.Emit(ctx, iface.AuditEvent{
		TenantID:     target.UUID,
		TenantKind:   target.Kind,
		ActorUserID:  claims.UserUUID,
		ActorEmail:   claims.Email,
		ActorType:    "user",
		Action:       action,
		ResourceType: "tenant",
		ResourceID:   target.UUID,
		Outcome:      "success",
		IPAddress:    utils.GetClientIP(r),
		UserAgent:    r.UserAgent(),
		Metadata: map[string]any{
			"targetTenantSlug":    target.Slug,
			"targetTenantName":    target.Name,
			"targetIsCompany":     target.IsCompany,
			"targetSignupChannel": target.SignupChannel,
			"requestPath":         r.URL.Path,
		},
	})
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

// tenantKindEnforcementMode returns "warn" when TENANT_KIND_ENFORCEMENT=warn,
// otherwise "enforce". Read once per invocation rather than cached so an
// operator flipping the env var on a hot-reloaded process takes effect on the
// next request. Default is strict enforcement — warn-mode is an opt-in for
// staged rollouts where operators want telemetry on mismatched kinds before
// the gate starts returning 403.
func tenantKindEnforcementMode() string {
	if os.Getenv("TENANT_KIND_ENFORCEMENT") == "warn" {
		return "warn"
	}
	return "enforce"
}

// RequireTenantKind rejects any request whose resolved tenant is not of the
// expected kind. Use to gate Tier-1-only or Tier-2-only endpoints. Routes
// without a resolved tenant (global routes) are also rejected — callers that
// want tier-agnostic behavior simply don't use this middleware.
//
// Enforcement honours TENANT_KIND_ENFORCEMENT: "warn" logs mismatches and
// passes through; anything else (default) returns 403. Missing-tenant is
// always blocked regardless of mode — a route gated by kind cannot
// meaningfully run without a tenant context, so the error still needs to
// surface.
func (m *AuthMiddleware) RequireTenantKind(expected string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			kind := ctxauth.TenantKindFromContext(r.Context())
			if kind == "" {
				m.sendErrorResponse(w, r, errors.AuthorizationError("tenant context required").
					WithOperation("require_tenant_kind").
					WithDetail("expected", expected).
					Build())
				return
			}
			if kind != expected {
				if tenantKindEnforcementMode() == "warn" {
					slog.Default().Warn("tenant-kind mismatch (warn-mode, request allowed)",
						slogString("expected", expected),
						slogString("actual", kind),
						slogString("path", r.URL.Path),
						slogString("method", r.Method),
					)
					next.ServeHTTP(w, r)
					return
				}
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
			ctx = context.WithValue(ctx, ctxauth.KeyUserUUID, claims.UserUUID)
			ctx = context.WithValue(ctx, ctxauth.KeyUserEmail, claims.Email)
			ctx = context.WithValue(ctx, ctxauth.KeySystemRole, claims.SystemRole)
			ctx = context.WithValue(ctx, ctxClaims, claims)
			ctx = context.WithValue(ctx, ctxTenantMemberships, claims.Memberships)
			if ip := utils.GetClientIP(r); ip != "" {
				ctx = context.WithValue(ctx, ctxauth.KeyClientIP, ip)
			}
			if tenantID, roles, kind, ok := resolveCurrentTenant(r, claims); ok {
				ctx = context.WithValue(ctx, ctxauth.KeyTenantID, tenantID)
				ctx = context.WithValue(ctx, ctxauth.KeyTenantRoles, roles)
				if kind != "" {
					ctx = context.WithValue(ctx, ctxauth.KeyTenantKind, kind)
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

// --- Context accessors (auth-internal, JWT-claims-dependent) ---
//
// Plain getters that don't touch claims (GetUserUUID, GetUserEmail,
// GetSystemRole, GetTenantID, GetTenantRoles, GetClientIP, IsImpersonating,
// TenantKindFromContext, WithTenantKind, WithClientIP) live in
// pkg/sdk/ctxauth so extracted addons can read them without importing
// backend internals. The accessors below stay here because they read
// *models.JWTClaims, which is auth-module-internal — Phase 1c exposes
// them through an iface.ClaimsAccessor SDK contract.

// GetSessionID extracts the JWT sid claim (session identifier) from the
// request context. Used by the logout handler to revoke the current
// session and by any handler that needs to correlate requests against a
// specific session. Returns ok=false when the context has no claims
// (unauthenticated routes) or the claims predate sid stamping.
func GetSessionID(ctx context.Context) (string, bool) {
	claims, ok := ctx.Value(ctxClaims).(*models.JWTClaims)
	if !ok || claims == nil || claims.SessionID == "" {
		return "", false
	}
	return claims.SessionID, true
}

// GetMemberships returns all tenant memberships the user has.
func GetMemberships(ctx context.Context) ([]models.TenantMembership, bool) {
	mbrs, ok := ctx.Value(ctxTenantMemberships).([]models.TenantMembership)
	return mbrs, ok
}

// GetAMR returns the authentication-method-references slice (RFC 8176)
// from the current request's JWT claims. Used by the authz module to
// stamp the Cedar principal with MFA context. Returns ok=false when no
// claims are on the context (unauthenticated routes, service-to-service
// calls without a session).
func GetAMR(ctx context.Context) ([]string, bool) {
	claims, ok := ctx.Value(ctxClaims).(*models.JWTClaims)
	if !ok || claims == nil {
		return nil, false
	}
	return claims.AMR, true
}

// IsMFAEnrolled reports whether the active session was completed with a
// verified second factor. One source of truth for both RequireMFA
// middleware and Cedar's principal.mfa_enrolled attribute — drift here
// would let policies gate on a signal that never fires.
func IsMFAEnrolled(ctx context.Context) bool {
	amr, ok := GetAMR(ctx)
	if !ok {
		return false
	}
	return amrSatisfiesMFA(amr)
}

// WithAMR stamps an amr slice onto the request's JWT claims so tests can
// exercise MFA-gated ABAC policies without booting the middleware chain.
// Wraps (or creates) a minimal JWTClaims so GetAMR / IsMFAEnrolled read
// the same value they would from a real token. Production code paths
// populate AMR through JWT issuance, not this helper.
func WithAMR(ctx context.Context, amr []string) context.Context {
	existing, _ := ctx.Value(ctxClaims).(*models.JWTClaims)
	var next models.JWTClaims
	if existing != nil {
		next = *existing
	}
	next.AMR = amr
	return context.WithValue(ctx, ctxClaims, &next)
}

// WithSessionID stamps a session UUID onto the request's JWT claims so
// tests can exercise sid-gated signals (session revocation, risk
// lookup, Cedar principal.risk_score) without booting the middleware
// chain. Production code paths populate sid at token issuance.
func WithSessionID(ctx context.Context, sid string) context.Context {
	existing, _ := ctx.Value(ctxClaims).(*models.JWTClaims)
	var next models.JWTClaims
	if existing != nil {
		next = *existing
	}
	next.SessionID = sid
	return context.WithValue(ctx, ctxClaims, &next)
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
			userUUID, ok := ctxauth.GetUserUUID(r.Context())
			if !ok {
				m.sendErrorResponse(w, r, errors.AuthenticationError("authentication required").
					WithOperation("require_permission").Build())
				return
			}
			tenantID, hasTenant := ctxauth.GetTenantID(r.Context())
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
			userUUID, ok := ctxauth.GetUserUUID(r.Context())
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

// RequireCapability blocks the request unless the current owner holds an
// active entitlement to the given capability ID in the tenant_entitlements
// projection. Returns 402 Payment Required — distinct from 403 Forbidden —
// so the frontend can branch on the error and surface a subscription /
// upgrade prompt rather than an access-denied screen.
//
// Owner resolution: when X-Tenant-ID is present (and the caller is a member
// per the existing membership check), the owner is the tenant. Otherwise it
// is the calling user — self-registered clients hold entitlements on their
// own user UUID after the post-onboarding refactor.
//
// Typical use: apply this AFTER RequirePermission so RBAC runs first and
// a 403 wins over a 402 when neither gate passes. The order does not affect
// correctness (both must permit the request), only which error the caller
// sees when multiple gates fail.
func (m *AuthMiddleware) RequireCapability(capabilityID string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if m.access == nil {
				m.sendErrorResponse(w, r, errors.InternalError("access service not ready").
					WithOperation("require_capability").Build())
				return
			}
			tenantUUID, ok := capabilityTenantFromRequest(r)
			if !ok {
				m.sendErrorResponse(w, r, errors.AuthorizationError("tenant context required").
					WithOperation("require_capability").
					WithDetail("capability", capabilityID).Build())
				return
			}
			allowed, err := m.access.HasCapability(r.Context(), tenantUUID, capabilityID)
			if err != nil {
				m.sendErrorResponse(w, r, errors.InternalError("capability check failed").
					WithOperation("require_capability").
					WithInternal(err).Build())
				return
			}
			if !allowed {
				// Count every 402 so operators can see which capabilities
				// generate the most tenant friction. Label is the
				// capability ID (bounded by the Capabilities() catalog
				// cardinality).
				metrics.Default().RecordCapabilityDenied(capabilityID)
				m.sendCapabilityRequiredResponse(w, r, capabilityID, tenantUUID)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// capabilityTenantFromRequest returns the tenant UUID the capability gate
// evaluates against — the request's resolved X-Tenant-ID. Returns false
// when no tenant context is set; capability gating without a tenant is
// undefined post Unified Client Aggregate (every billable principal is a
// tenant).
func capabilityTenantFromRequest(r *http.Request) (string, bool) {
	if tenantID, ok := ctxauth.GetTenantID(r.Context()); ok && tenantID != "" {
		return tenantID, true
	}
	return "", false
}

// RequireGlobal is a pass-through for routes that don't need an org context
// (auth flows, org listing, user self-service). It just verifies the request
// is authenticated; RequireAuth on the parent router already handles that.
func (m *AuthMiddleware) RequireGlobal() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if _, ok := ctxauth.GetUserUUID(r.Context()); !ok {
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

// RequireStepUp blocks the request unless a second factor (or a fresh
// password reconfirm) was completed within maxAge of now. Used for
// catastrophic / irreversible operations where a session-long MFA proof
// (what RequireMFA accepts) would leave too wide a window between
// authentication and action.
//
// The gate has three failure shapes so the frontend can pick the right
// modal without a second round-trip:
//
//   - code="step_up_required" — user has a factor; ask for an OTP/passkey
//     (the legacy path, unchanged).
//   - code="password_confirm_required" — user has no factor enrolled AND
//     the policy doesn't require them to. Frontend collects a password
//     reconfirm via POST /v1/auth/{tier}/me/password-confirm and replays.
//   - code="mfa_enrollment_required" — user's role requires MFA but they
//     haven't enrolled. Frontend nudges them to /user/settings to enroll.
//
// The split is gated by the MFAEnrollmentLookup setter. When the lookup
// isn't wired (legacy tests, sidecar fallback) every step-up failure
// emits the legacy step_up_required envelope.
func (m *AuthMiddleware) RequireStepUp(maxAge time.Duration) func(http.Handler) http.Handler {
	if maxAge <= 0 {
		maxAge = 5 * time.Minute
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			claims, ok := r.Context().Value(ctxClaims).(*models.JWTClaims)
			if !ok || claims == nil {
				m.sendErrorResponse(w, r, errors.AuthenticationError("authentication required").
					WithOperation("require_step_up").Build())
				return
			}
			// Fresh proof — pass through. amrSatisfiesMFA also accepts the
			// "reauth" marker minted by the password-confirm endpoint; that
			// endpoint refuses to issue a reauth token for users with an
			// enrolled factor, so the marker can never be used to bypass
			// MFA-required scenarios.
			if amrSatisfiesMFA(claims.AMR) && claims.LastOTPAt > 0 &&
				time.Since(time.Unix(claims.LastOTPAt, 0)) <= maxAge {
				next.ServeHTTP(w, r)
				return
			}
			// No fresh proof. Branch on enrollment + policy.
			m.dispatchStepUpFailure(w, r, claims, maxAge)
		})
	}
}

// dispatchStepUpFailure picks the failure envelope that lets the
// frontend run the right recovery UX. Three outcomes:
//
//  1. enrollment lookup unavailable, or it reports the user has at
//     least one factor → step_up_required (legacy path).
//  2. no factor + role requires MFA → mfa_enrollment_required (the user
//     must enroll before they can use the destructive surface).
//  3. no factor + role does not require MFA → password_confirm_required
//     (the password reconfirm path).
//
// The branching is deliberately defensive: any error from the lookup
// falls through to the legacy step_up_required path. A degraded Mongo
// must never silently weaken the gate (e.g. trick the frontend into
// asking for a password when the user actually has TOTP enrolled).
func (m *AuthMiddleware) dispatchStepUpFailure(w http.ResponseWriter, r *http.Request, claims *models.JWTClaims, maxAge time.Duration) {
	if m.mfaEnrollment == nil {
		m.sendStepUpRequired(w, r, maxAge)
		return
	}
	hasFactor, err := m.mfaEnrollment(r.Context(), claims.Audience, claims.UserUUID)
	if err != nil || hasFactor {
		m.sendStepUpRequired(w, r, maxAge)
		return
	}
	// No factor enrolled. If the role requires MFA, the right answer is
	// "enroll first" — letting them bypass with a password would defeat
	// the policy. Otherwise emit the password-confirm envelope.
	if m.roleRequiresMFA(r.Context(), claims) {
		m.sendMFAEnrollmentRequired(w, r)
		return
	}
	m.sendPasswordConfirmRequired(w, r, maxAge)
}

// roleRequiresMFA resolves whether the caller's current role + memberships
// trip the MFA-required policy. Prefers a fresh User lookup (so a role
// change applied since the JWT was minted is honoured) and falls back to
// reasoning from the claims when the user provider isn't wired.
func (m *AuthMiddleware) roleRequiresMFA(ctx context.Context, claims *models.JWTClaims) bool {
	if m.stepUpPolicy == nil {
		return false
	}
	if m.users != nil {
		if user, err := m.users.GetUserByID(ctx, claims.UserUUID); err == nil && user != nil {
			return m.stepUpPolicy.MFARequired(user, claims.Memberships)
		}
	}
	// Claims-only fallback — synthesize a minimal user from srole. The
	// policy reader only reads user.Role and the membership roles, so
	// this is sufficient for the role-based MFA gate.
	return m.stepUpPolicy.MFARequired(&userModels.User{Role: claims.SystemRole}, claims.Memberships)
}

// RequireLowRisk blocks the request when the current session's risk
// score meets or exceeds threshold. Reuses the step_up_required 401
// envelope so the frontend's existing MFA step-up modal can resolve
// the block transparently. Fails open in three cases: (1) no JWT
// claims on the context, (2) lookup callback not wired, (3) lookup
// errors — a degraded risk signal must never lock privileged actions
// out. Blocking decisions emit a Warn log so operators can alert on
// risk-driven denials.
func (m *AuthMiddleware) RequireLowRisk(threshold float64) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			claims, ok := r.Context().Value(ctxClaims).(*models.JWTClaims)
			if !ok || claims == nil || claims.SessionID == "" {
				next.ServeHTTP(w, r)
				return
			}
			if m.sessionRiskLookup == nil {
				next.ServeHTTP(w, r)
				return
			}
			score, err := m.sessionRiskLookup(r.Context(), claims.SessionID)
			if err != nil {
				slog.Default().Warn("risk: session-risk lookup failed; passing through",
					slogString("sid", claims.SessionID),
					slogString("error", err.Error()))
				next.ServeHTTP(w, r)
				return
			}
			if score >= threshold {
				slog.Default().Warn("risk: blocking action, session score exceeds threshold",
					slogString("sid", claims.SessionID),
					slogString("path", r.URL.Path),
					slogString("method", r.Method))
				m.sendRiskStepUp(w, r, threshold, score)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// sendRiskStepUp emits the same code="step_up_required" 401 envelope
// RequireStepUp uses, annotated with the observed risk score and
// threshold. The frontend's step-up modal branches on `code` alone, so
// reusing the string means risk-driven and freshness-driven step-ups
// share the UI path.
func (m *AuthMiddleware) sendRiskStepUp(w http.ResponseWriter, r *http.Request, threshold, score float64) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("WWW-Authenticate", `MFA error="step_up_required"`)
	w.WriteHeader(http.StatusUnauthorized)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"status": http.StatusUnauthorized,
		"title":  "step-up authentication required",
		"detail": "this action requires a fresh second-factor verification due to elevated session risk",
		"type":   "about:blank",
		"errors": []map[string]any{{
			"message":  "step-up required — high risk session",
			"location": "require_low_risk",
			"value":    "HIGH_RISK_SESSION",
		}},
		"code":          "step_up_required",
		"riskScore":     score,
		"riskThreshold": threshold,
	})
}

// sendStepUpRequired emits the structured 401 the frontend looks for to
// drive a re-MFA prompt. Mirrors sendMFARequired's shape so clients can
// reuse most of the handler, branching only on the "code" field.
func (m *AuthMiddleware) sendStepUpRequired(w http.ResponseWriter, r *http.Request, maxAge time.Duration) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("WWW-Authenticate", `MFA error="step_up_required"`)
	w.WriteHeader(http.StatusUnauthorized)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"status": http.StatusUnauthorized,
		"title":  "step-up authentication required",
		"detail": "this action requires a fresh second-factor verification",
		"type":   "about:blank",
		"errors": []map[string]any{{
			"message":  "step-up required",
			"location": "require_step_up",
			"value":    "STEP_UP_REQUIRED",
		}},
		"code":          "step_up_required",
		"maxAgeSeconds": int(maxAge.Seconds()),
	})
}

// amrSatisfiesMFA checks whether any second-factor method (or a fresh
// password reconfirm) is recorded on the token. Method names follow
// RFC 8176 with one local extension:
//
//   - "reauth" — a fresh password reconfirm minted by the
//     /v1/auth/{tier}/me/password-confirm endpoint. The endpoint refuses
//     to mint a "reauth" token for a user with any MFA factor enrolled,
//     so accepting it here cannot weaken the gate for an
//     MFA-required user.
func amrSatisfiesMFA(amr []string) bool {
	for _, v := range amr {
		if v == "otp" || v == "webauthn" || v == "mfa" || v == "reauth" {
			return true
		}
	}
	return false
}

// sendPasswordConfirmRequired emits the 401 envelope that tells the
// frontend the user has no MFA factor and may bypass step-up with a
// password reconfirm. Same outer shape as sendStepUpRequired so the
// frontend's RTK Query base branch can switch on `code` alone.
func (m *AuthMiddleware) sendPasswordConfirmRequired(w http.ResponseWriter, _ *http.Request, maxAge time.Duration) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("WWW-Authenticate", `Bearer error="password_confirm_required"`)
	w.WriteHeader(http.StatusUnauthorized)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"status": http.StatusUnauthorized,
		"title":  "password reconfirm required",
		"detail": "this action requires a fresh password reconfirm because no second factor is enrolled",
		"type":   "about:blank",
		"errors": []map[string]any{{
			"message":  "password confirm required",
			"location": "require_step_up",
			"value":    "PASSWORD_CONFIRM_REQUIRED",
		}},
		"code":          "password_confirm_required",
		"maxAgeSeconds": int(maxAge.Seconds()),
	})
}

// sendMFAEnrollmentRequired emits the 403 envelope used when the caller's
// role obligates MFA but they have no factor. Distinguished from
// password_confirm_required so the frontend nudges to /user/settings
// instead of opening the password modal.
func (m *AuthMiddleware) sendMFAEnrollmentRequired(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("WWW-Authenticate", `Bearer error="mfa_enrollment_required"`)
	w.WriteHeader(http.StatusForbidden)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"status": http.StatusForbidden,
		"title":  "mfa enrollment required",
		"detail": "your role requires a second factor; enroll one before performing this action",
		"type":   "about:blank",
		"errors": []map[string]any{{
			"message":  "mfa enrollment required",
			"location": "require_step_up",
			"value":    "MFA_ENROLLMENT_REQUIRED",
		}},
		"code": "mfa_enrollment_required",
	})
}

// sendMFARequired emits the structured 401 the frontend looks for to trigger
// a step-up prompt. Shares the `step_up_required` code with sendStepUpRequired
// and sendRiskStepUp so the client switches on a single value to drive the
// MFA modal regardless of whether the gate is session-MFA, freshness, or
// risk-based.
func (m *AuthMiddleware) sendMFARequired(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("WWW-Authenticate", `MFA error="step_up_required"`)
	w.WriteHeader(http.StatusUnauthorized)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"status": http.StatusUnauthorized,
		"title":  "mfa required",
		"detail": "this action requires a second authentication factor",
		"type":   "about:blank",
		"errors": []map[string]any{{
			"message":  "mfa required",
			"location": "require_mfa",
			"value":    "STEP_UP_REQUIRED",
		}},
		"code": "step_up_required",
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
