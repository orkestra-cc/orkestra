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

	"github.com/orkestra/backend/internal/core/auth/models"
	"github.com/orkestra/backend/internal/core/auth/services"
	"github.com/orkestra/backend/internal/shared/errors"
	"github.com/orkestra/backend/internal/shared/utils"
	"github.com/orkestra/backend/pkg/sdk/ctxauth"
	"github.com/orkestra/backend/pkg/sdk/iface"
	"github.com/orkestra/backend/pkg/sdk/metrics"
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
// substitute a fake. Nil-tolerant where an answer can be defaulted safely
// (MFARequired / MFAEnabled), but NOT on the password-reauth branch: with
// no policy wired the gate cannot know whether the password is still an
// accepted method, so RequireStepUp answers 503 auth.policy_unavailable
// rather than offering a reconfirm the endpoint would only refuse.
type StepUpPolicy interface {
	MFARequired(user *iface.User, memberships []models.OrgMembership) bool
	// MFAEnabled reports the master MFA switch (auth module's mfaEnabled).
	// When off, the RequireMFA route gate passes through: a deployment that
	// has globally disabled MFA must still be able to perform MFA-gated
	// admin writes (module enable/disable, secret writes, role/tenant
	// mutations) — otherwise a never-enrolled operator can never turn MFA
	// back on. Mirrors AuthPolicyService.MFARequired, which already
	// short-circuits to false when the switch is off. Nil-safe on the impl.
	MFAEnabled(ctx context.Context) bool
	// PasswordReauthAllowed reports whether a password may serve as the
	// re-authentication proof for the token's audience ("operator" |
	// "client"). False means the per-surface method is administratively
	// disabled (auth module passwordLoginEnabled{Admin,Client}); an error
	// means the policy could not be evaluated and the caller must answer
	// a retryable 503 auth.policy_unavailable — never mfa_enrollment_
	// required, which would misreport an outage as a user obligation.
	// The operator break-glass is deliberately invisible here: a
	// temporary override must not look like a durable login method.
	PasswordReauthAllowed(ctx context.Context, audience string) (bool, error)
}

type AuthMiddleware struct {
	jwtService        services.JWTService
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
		errorManager: errorManager,
	}
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

// RequireAuth is the bearer-only perimeter (ADR-0020, #317). A missing or
// invalid bearer is a plain codeless 401; an EXPIRED one is a 401 carrying
// code "access_token_expired" and WWW-Authenticate: Bearer
// error="access_token_expired" (sendAccessTokenExpired), which is what tells
// a client the request never reached its handler and a refresh-then-retry is
// safe. Either way the refresh cookie is never consulted here. Rotation
// happens only where a client explicitly asks for it — POST
// /v1/auth/{tier}/refresh-cookie or /refresh — and the read-only mint lives
// in GET /v1/auth/session; a client recovers from an expired access token
// with 401 → refresh-cookie → retry. A middleware that rotated on the
// caller's behalf raced that path and signed users out mid-session.
func (m *AuthMiddleware) RequireAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token := m.extractBearerToken(r)
		if token == "" {
			m.sendErrorResponse(w, r, errors.AuthenticationError("authentication required").
				WithOperation("require_auth").
				Build())
			return
		}

		claims, err := m.jwtService.ValidateAccessToken(token)
		if err != nil {
			// An EXPIRED token gets its own code. It is the one rejection that
			// tells a client something actionable: the credential was well
			// formed and correctly signed, it simply aged out, and the
			// sanctioned recovery (POST /v1/auth/{tier}/refresh-cookie, then
			// retry — ADR-0020) will work. Every other rejection here says
			// "this token is not ours", where a refresh is at best wasted.
			//
			// It is also the boundary a client needs to retry SAFELY: this
			// branch runs before dispatch, so a request rejected here provably
			// never reached the handler and cannot have consumed anything.
			// Without the code, frontend-client has to infer that from its own
			// reckoning of the token's lifetime and must give up the token
			// that expired in flight.
			if err == services.ErrTokenExpired {
				m.sendAccessTokenExpired(w)
				return
			}
			// No verifying key loaded is not a verdict on this token — it
			// is this server admitting it cannot authenticate ANYONE until
			// an operator fixes the key material. RequireAuth guards every
			// protected route on both tiers, so the codeless 401 this used
			// to fall through to told every client its session had ended.
			// A boot-time state is not a blip: no client-side retry helps,
			// which is exactly why the honest answer is a 503 rather than
			// another 401 code. == rather than errors.Is because
			// validateTokenEnhanced returns the sentinel unwrapped, like the
			// two comparisons it joins, and because in this file `errors` is
			// the shared errors package (see errors.TokenInvalidError below).
			if err == services.ErrJWTKeysNotLoaded {
				m.sendTokenVerificationUnavailable(w)
				return
			}
			if err == services.ErrInvalidToken {
				m.sendErrorResponse(w, r, errors.AuthenticationError("authentication required").
					WithOperation("require_auth").
					Build())
				return
			}
			m.sendErrorResponse(w, r, errors.TokenInvalidError().
				WithOperation("require_auth").
				WithInternal(err).
				Build())
			return
		}

		if revoked, reason := m.sessionRevocationState(r, claims); revoked {
			m.sendSessionRevoked(w, r, reason)
			return
		}
		m.setUserContext(w, r, claims, next)
	})
}

// sessionRevocationState reports whether the token's sid is on the denylist
// and, when the wired service can supply it, WHY it was put there. Errors are
// treated as "not revoked" by the service (fail-open on Redis outage) — see
// SessionRevocationService's type comment.
//
// The reason is read through the optional SessionRevocationReasonReader
// extension, so a fork's own SessionRevocationService implementation keeps
// compiling and simply gets the generic wording. The lookup is not an extra
// round-trip: Revoke stores the reason as the Redis value, so the GET that
// answers "revoked?" already carries it.
func (m *AuthMiddleware) sessionRevocationState(r *http.Request, claims *models.JWTClaims) (bool, string) {
	if m.sessionRevocation == nil || claims == nil || claims.SessionID == "" {
		return false, ""
	}
	if reader, ok := m.sessionRevocation.(services.SessionRevocationReasonReader); ok {
		reason, revoked := reader.RevocationReason(r.Context(), claims.SessionID)
		return revoked, reason
	}
	revoked, _ := m.sessionRevocation.IsRevoked(r.Context(), claims.SessionID)
	return revoked, ""
}

// schemeBearer and schemeMFA are the only two WWW-Authenticate schemes
// this middleware challenges with. A coded envelope that names neither
// sends no WWW-Authenticate at all — see codedError.scheme.
const (
	schemeBearer = "Bearer"
	schemeMFA    = "MFA"
)

// codedErrorItem is the single errors[] entry a coded envelope carries.
//
// `value` is a parameter rather than something derived from the code: it is
// strings.ToUpper(code) for every emitter but sendRiskStepUp, whose value is
// HIGH_RISK_SESSION against a step_up_required code — and one counter-example
// is enough to make derivation wrong.
type codedErrorItem struct {
	message  string
	location string
	value    string
}

// codedError describes one of this package's hand-built coded error
// envelopes — the responses that carry a FLAT top-level `code`, which is
// what a client branches on. sendErrorResponse is deliberately not one of
// them: it routes through errorManager and emits no top-level code at all,
// putting its value in errors[0].value instead, and moving it behind this
// writer would be a wire change.
//
// The invariant core is Content-Type, status, title, detail,
// type: "about:blank" and code. Everything that varies is a field here:
//
//   - status — 401, 402, 403 or 503;
//   - scheme — schemeBearer, schemeMFA, or "" for no WWW-Authenticate.
//     The header's error token is always the `code`, which is true of all
//     seven challenging emitters and pinned by the golden test;
//   - item — the single errors[] entry, or nil for the emitters that carry
//     none. The zero value is the SAFE one: a caller that forgets the field
//     omits errors[], it does not invent one. Adding an errors[] to a
//     response that has none is a wire change (spec §8 #18(d)), so the two
//     emitters that omit it say so at their call site;
//   - extra — additional TOP-LEVEL body fields (maxAgeSeconds; riskScore +
//     riskThreshold; capability + tenantId). Keys outside the invariant core
//     only — a key that collides with one would overwrite it.
type codedError struct {
	status int
	code   string
	title  string
	detail string
	scheme string
	item   *codedErrorItem
	extra  map[string]any
}

// writeCodedError writes one coded error envelope. It is the single place
// AuthMiddleware builds that wire shape in THIS FILE; every send* below is a
// thin wrapper that names its own envelope and calls this. It is NOT
// package-wide: jwt_validator.go and audience.go still build four coded
// envelopes inline, in shapes that differ from these ones (no errors[], and
// in audience.go's case no status/title/detail/type either), so routing them
// through this writer would change their wire output. They are enumerated in
// the SCOPE note on TestCodedErrorEnvelopes_Golden.
//
// The output is byte-for-byte what the ten emitters that existed at the
// refactor wrote when each built its own map (there are eleven now —
// sendReauthenticationRequired was written against this helper rather than
// migrated onto it) — json.Encoder sorts map keys, so field order here is
// irrelevant, and Encode's trailing newline is part of the contract.
// TestCodedErrorEnvelopes_Golden pins every byte against literals captured
// before this helper existed.
func writeCodedError(w http.ResponseWriter, e codedError) {
	w.Header().Set("Content-Type", "application/json")
	if e.scheme != "" {
		w.Header().Set("WWW-Authenticate", e.scheme+` error="`+e.code+`"`)
	}
	w.WriteHeader(e.status)

	body := map[string]any{
		"status": e.status,
		"title":  e.title,
		"detail": e.detail,
		"type":   "about:blank",
		"code":   e.code,
	}
	if e.item != nil {
		body["errors"] = []map[string]any{{
			"message":  e.item.message,
			"location": e.item.location,
			"value":    e.item.value,
		}}
	}
	for k, v := range e.extra {
		body[k] = v
	}
	_ = json.NewEncoder(w).Encode(body)
}

// sendSessionRevoked emits the structured 401 that tells the client to
// drop its access token and re-authenticate. The code is distinct from the
// generic `authentication required` path so the frontend can choose a
// cleaner UX than the token-expired toast.
//
// A session that simply reached its configured maximum age gets its OWN code.
// The cap writes models.RevokeReasonSessionMaxAge onto the denylist, and
// ADR-0017 D4 is explicit that reporting that as "revoked" is inaccurate and
// that "the distinction matters to whoever reads the support ticket". Before
// this, `session_max_age_reached` was emitted only by the two refresh
// endpoints — neither of which surfaces it to a user (one is read by a raw
// fetch that discards the body, the other is classified as an auth check and
// suppressed) — so in practice a capped-out user was always told "revoked".
// This is the path that actually reaches them.
func (m *AuthMiddleware) sendSessionRevoked(w http.ResponseWriter, r *http.Request, reason string) {
	code, title, detail := "session_revoked", "session revoked", "this session has been revoked; please sign in again"
	if reason == models.RevokeReasonSessionMaxAge {
		code = "session_max_age_reached"
		title = "session maximum age reached"
		detail = "this session reached its maximum age; please sign in again"
	}
	writeCodedError(w, codedError{
		status: http.StatusUnauthorized,
		code:   code,
		title:  title,
		detail: detail,
		scheme: schemeBearer,
		item:   &codedErrorItem{message: title, location: "require_auth", value: strings.ToUpper(code)},
	})
}

// sendAccessTokenExpired emits the 401 for a well-formed, correctly signed
// access token that has simply aged out.
//
// It is deliberately distinct from the generic `authentication required` 401,
// which carries NO top-level code (sendErrorResponse puts appErr.Code in
// errors[0].value, and for an AuthenticationError that value is
// CodeInvalidCredentials — the same value a wrong password produces, so it
// discriminates nothing). A client cannot otherwise tell "your token expired"
// from "your credentials were wrong", and guessing wrong in one direction
// replays a rejected request while guessing wrong in the other leaves a
// working session broken until reload.
//
// The code means EXPIRED and nothing else, and it may only ever have ONE
// emitter: validateTokenEnhanced maps jwt.ErrTokenExpired to
// services.ErrTokenExpired straight off jwt.Parse, BEFORE its own token-type,
// issuer and audience checks, so a wrong audience or a wrong token type can
// never acquire this code — and a client that refreshes on it never refreshes
// for a forged token.
//
// RequireAuth stays bearer-only (ADR-0020): this rejects, it does not rotate,
// and it emits no Set-Cookie and no minted token. Recovery remains the
// client's explicit POST to /v1/auth/{tier}/refresh-cookie.
//
// The request is deliberately not a parameter: nothing here reads it (the body
// says only what the token's own state is), and sendPolicyUnavailable sets the
// same precedent in this file.
func (m *AuthMiddleware) sendAccessTokenExpired(w http.ResponseWriter) {
	const code = "access_token_expired"
	writeCodedError(w, codedError{
		status: http.StatusUnauthorized,
		code:   code,
		title:  "access token expired",
		detail: "the access token has expired; refresh it and retry",
		scheme: schemeBearer,
		item:   &codedErrorItem{message: "access token expired", location: "require_auth", value: strings.ToUpper(code)},
	})
}

// setUserContext injects user identity and the resolved current-tenant
// context into the request. Tenant resolution order:
//  1. claims.ActingTenantID — a JWT stamped for a specific tenant.
//  2. X-Tenant-ID header when the user is a member of that tenant.
//  3. claims.TenantFallbackID.
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
// Tier resolution order (ADR-0001, amended):
//  1. X-Tenant-ID header, for OPERATOR-audience tokens only, when it names a
//     tenant the user is a member of — this is the operator org switcher.
//     Operator tokens stamp ActingTenantID = TenantFallbackID merely as a
//     default, so the header must be free to override it. A header naming a
//     non-member tenant falls through (admin impersonation is handled by
//     setUserContext when resolution yields ok=false).
//  2. claims.ActingTenantID + ActingTenantKind when set by the issuer — a
//     client-portal token is minted pinned to one tenant; the header cannot
//     override it, so a Tier-2 session can never hop tenants.
//  3. X-Tenant-ID header when the user is a member of that tenant.
//  4. claims.TenantFallbackID.
//
// SPEC INVARIANT (Task 6.2): this function — and every other file under
// internal/shared/middleware — never resolves the platform-wide default
// Tier-1 tenant pointer (owned by the tenant module, tenant module PR 3;
// its resolver type is declared in pkg/sdk/iface). The platform default may
// influence a request only indirectly, by having already been folded into
// TenantFallbackID at operator-audience token ISSUANCE time
// (auth/services/jwt_service.go::loadMemberships), which only happens when
// the default names one of the user's own valid memberships. By the time a
// claim reaches this function, TenantFallbackID is an ordinary per-token
// membership-validated value indistinguishable from any other fallback —
// resolution here does not know or care whether it came from the platform
// default or the owner-first rule. Resolving the pointer directly from
// middleware would let a request influence itself with something that was
// never membership-checked against the CALLER. Enforced by a package
// hygiene test alongside this file (see the middleware isolation test).
func resolveCurrentTenant(r *http.Request, claims *models.JWTClaims) (string, []string, string, bool) {
	requested := r.Header.Get(TenantIDHeader)

	// Operator org switcher: an operator-audience token may act in any tenant
	// it belongs to by sending X-Tenant-ID, overriding the default
	// ActingTenantID the issuer stamped. Client-portal tokens are excluded so
	// their issuer-pinned ActingTenantID stays authoritative (Phase 3). A
	// header naming a non-member tenant is left to the flow below / the admin
	// impersonation bypass, preserving existing behaviour.
	if requested != "" && claims.Audience == services.AudienceOperator {
		for _, mbr := range claims.Memberships {
			if mbr.TenantUUID == requested {
				return mbr.TenantUUID, mbr.Roles, mbr.TenantKind, true
			}
		}
	}

	// Stamped-in tenant on the JWT itself: client-portal tokens in Phase 3
	// always take this path. The header is ignored for them.
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
	if requested != "" {
		for _, mbr := range claims.Memberships {
			if mbr.TenantUUID == requested {
				return mbr.TenantUUID, mbr.Roles, mbr.TenantKind, true
			}
		}
		return "", nil, "", false
	}
	if claims.TenantFallbackID != "" {
		for _, mbr := range claims.Memberships {
			if mbr.TenantUUID == claims.TenantFallbackID {
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
			// Master switch off → no second factor is required to exist, so
			// demanding an MFA proof here would deadlock a never-enrolled
			// operator out of the very admin writes (module enable, secret
			// writes) they'd use to configure the platform — the bootstrap
			// trap #78 fixed at the policy layer but not at this route gate.
			// RequireStepUp keeps its stricter fresh-proof requirement; this
			// only relaxes the session-long RequireMFA gate.
			if m.stepUpPolicy != nil && !m.stepUpPolicy.MFAEnabled(r.Context()) {
				next.ServeHTTP(w, r)
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
// The gate has four failure shapes so the frontend can pick the right
// modal without a second round-trip:
//
//   - code="step_up_required" — user has a factor; ask for an OTP/passkey
//     (the legacy path, unchanged).
//   - code="password_confirm_required" — user has no factor enrolled AND
//     the policy doesn't require them to AND the password is still an
//     accepted credential for the token's audience. Frontend collects a
//     password reconfirm via POST /v1/auth/{tier}/me/password-confirm
//     and replays.
//   - code="mfa_enrollment_required" — user's role requires MFA but they
//     haven't enrolled, or the password method is disabled for this
//     surface so no reconfirm can be offered. Frontend nudges them to
//     /user/settings to enroll.
//   - code="auth.policy_unavailable" (503) — the sign-in policy could not
//     be evaluated. Retryable; the frontend must not open a modal.
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
			// Fresh proof — pass through. The predicate accepts the
			// "reauth" marker minted by the password-confirm endpoint; that
			// endpoint refuses to issue a reauth token for users with an
			// enrolled factor, so the marker can never be used to bypass
			// MFA-required scenarios.
			if m.hasFreshSecondFactor(r, claims, maxAge) {
				next.ServeHTTP(w, r)
				return
			}
			// No fresh proof. Branch on enrollment + policy.
			m.dispatchStepUpFailure(w, r, claims, maxAge)
		})
	}
}

// hasFreshSecondFactor is RequireStepUp's freshness predicate, extracted
// so RequireStepUp and RequireEnrolmentProof cannot drift apart: they ask
// the same question ("did this caller present a second factor, or a
// password reconfirm, inside maxAge") and must keep answering it the same
// way.
//
// The request is a parameter although nothing here reads it yet: the
// epoch-aware version resolves the caller's MFA authority off the context
// built by setUserContext (R12) rather than trusting claims.AMR
// literally, and threading it in now keeps that change to the body.
func (m *AuthMiddleware) hasFreshSecondFactor(_ *http.Request, claims *models.JWTClaims, maxAge time.Duration) bool {
	return amrSatisfiesStepUp(claims.AMR) && claims.LastOTPAt > 0 &&
		time.Since(time.Unix(claims.LastOTPAt, 0)) <= maxAge
}

// RequireEnrolmentProof gates the four enrolment endpoints — TOTP enroll
// begin/confirm and passkey register begin/finish — on a fresh proof of
// presence.
//
// H-2 and H-3: these were mounted under RequireGlobal() alone, so a stolen
// session-only bearer could register a passkey on the victim's account, or
// REPLACE their TOTP secret (ConfirmEnrollment deletes the existing factor
// after validating a code for the NEW one), and then own the account
// outright.
//
// Two shapes of proof, because the two populations have different ones
// available:
//
//   - a user WITH a factor proves presence exactly as RequireStepUp
//     demands: a fresh second factor. There is one right answer for them,
//     so this branch never emits password_confirm_required (they have
//     something stronger) or mfa_enrollment_required (they are enrolled).
//   - a user WITHOUT a factor proves it with a recent interactive login
//     (auth_time within maxAge) or a fresh reauth. The answer when they
//     cannot is reauthentication_required, NOT password_confirm_required:
//     the users most in need of a first enrolment are MFA-obligated
//     accounts inside their grace window, whom the reconfirm endpoint
//     refuses (D19), and an OAuth-only account has no password to
//     reconfirm. A re-login is the one answer that works for everyone, and
//     both SPAs return the user to where they were.
//
// The lookup fails CLOSED: nil or erroring → step_up_required. A degraded
// Mongo must never be the reason a factor can be added without proof. It
// is consulted only AFTER the fresh-second-factor check, because that
// proof is carried in the signed token and needs no database at all — so
// the outage answer stays one an enrolled caller can actually satisfy by
// stepping up (spec §5 edge case 9), while auth_time, which is only
// acceptable for a caller with no factor, stays behind a successful read.
//
// Unlike RequireMFA this gate deliberately does NOT consult the mfaEnabled
// master switch. That switch exists to avoid a bootstrap deadlock — a
// never-enrolled operator must still be able to perform the admin writes
// that turn MFA on — and no such deadlock exists here: "did you prove
// presence" is answerable, and meaningful, whether or not MFA is enforced.
// The one cost is that a setup-wizard admin (amr ["pwd","reauth"],
// last_otp_at=now) must enrol within maxAge of the wizard or re-login
// first; they hold a password, so they can.
//
// Residual: the honest bound is a party holding the REFRESH COOKIE within
// maxAge of the victim's own interactive login — MintAccessTokenFromRefresh
// is reachable with that cookie alone and carries auth_time forward
// unchanged. That is inherent to a refresh cookie being a session
// credential, and no worse than any other session-bound action, but it is
// wider than "a bearer stolen inside the window". D13's email and audit row
// make an enrolment visible and the admin reset (D15/D16) recovers.
func (m *AuthMiddleware) RequireEnrolmentProof(maxAge time.Duration) func(http.Handler) http.Handler {
	if maxAge <= 0 {
		maxAge = 5 * time.Minute
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			claims, ok := r.Context().Value(ctxClaims).(*models.JWTClaims)
			if !ok || claims == nil {
				m.sendErrorResponse(w, r, errors.AuthenticationError("authentication required").
					WithOperation("require_enrolment_proof").Build())
				return
			}

			// The strong proof is carried IN THE TOKEN, so it needs no
			// read and is checked before the read that can fail. On a
			// healthy lookup this is behaviour-neutral — the branch below
			// passes on this input regardless of hasFactor — and during an
			// outage it is what makes the step_up_required answer
			// satisfiable rather than a challenge the caller provably
			// cannot act on (spec §5 edge case 9).
			if m.hasFreshSecondFactor(r, claims, maxAge) {
				next.ServeHTTP(w, r)
				return
			}

			// Everything past here needs to know whether a factor exists,
			// because auth_time — the weaker proof — may only be accepted
			// for a caller who has none. That question is unanswerable
			// without the lookup, so an unwired or failing one refuses.
			if m.mfaEnrollment == nil {
				m.sendStepUpRequired(w, r, maxAge)
				return
			}
			hasFactor, err := m.mfaEnrollment(r.Context(), claims.Audience, claims.UserUUID)
			if err != nil {
				m.sendStepUpRequired(w, r, maxAge)
				return
			}

			if hasFactor {
				// One right answer for an enrolled user. A fresh auth_time
				// is deliberately NOT accepted here: they have a stronger
				// proof available and replacing a factor is the attack this
				// gate exists to stop.
				m.sendStepUpRequired(w, r, maxAge)
				return
			}
			if claims.AuthTime > 0 && time.Since(time.Unix(claims.AuthTime, 0)) <= maxAge {
				next.ServeHTTP(w, r)
				return
			}
			m.sendReauthenticationRequired(w, r, maxAge, claims.AuthTime)
		})
	}
}

// RefuseEnrolmentProof is the fail-closed stand-in a caller mounts when its
// module.RoleMiddleware does not implement module.EnrolmentProofGate. Every
// request is refused with the same step_up_required envelope
// sendStepUpRequired writes.
//
// A missing gate must not become an open enrolment endpoint: the whole
// point of H-2/H-3 is that creating or replacing a second factor needs a
// proof this middleware is the only thing that can check. Refusing is the
// safe reading of "we cannot check", and it is the same answer the real
// gate gives when factor presence is unresolvable.
//
// It delegates to sendStepUpRequired on a zero-value AuthMiddleware rather
// than rebuilding the envelope, so the two can never drift: that method
// reads nothing from its receiver (TestCodedErrorEnvelopes_Golden pins its
// bytes off a zero value for the same reason). Keeping it here, in the
// package that owns writeCodedError, is what stops a second hand-built
// step_up_required envelope appearing in a consumer package.
func RefuseEnrolmentProof(maxAge time.Duration) func(http.Handler) http.Handler {
	if maxAge <= 0 {
		maxAge = 5 * time.Minute
	}
	var m AuthMiddleware
	return func(http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			m.sendStepUpRequired(w, r, maxAge)
		})
	}
}

// dispatchStepUpFailure picks the failure envelope that lets the
// frontend run the right recovery UX. Four outcomes:
//
//  1. enrollment lookup unavailable, or it reports the user has at
//     least one factor → step_up_required (legacy path).
//  2. no factor + role requires MFA → mfa_enrollment_required (the user
//     must enroll before they can use the destructive surface).
//  3. no factor + role does not require MFA + the password is a method
//     this audience still accepts → password_confirm_required (the
//     password reconfirm path). A method the surface refuses answers
//     mfa_enrollment_required instead — the reconfirm endpoint would
//     only 409 it (PR 3 §4.6).
//  4. the password-method policy cannot be read (no StepUpPolicy wired,
//     or the read failed) → 503 auth.policy_unavailable. An outage is
//     reported as an outage, never dressed up as a user obligation.
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
	// the policy. Otherwise the password-confirm fallback is offered ONLY
	// when the password is an accepted credential for this audience
	// (PR 3 §4.6): a disabled method also answers "enroll first", and an
	// unanswerable policy is a 503, never a fabricated obligation.
	if m.roleRequiresMFA(r.Context(), claims) {
		m.sendMFAEnrollmentRequired(w, r)
		return
	}
	if m.stepUpPolicy == nil {
		m.sendPolicyUnavailable(w)
		return
	}
	allowed, err := m.stepUpPolicy.PasswordReauthAllowed(r.Context(), claims.Audience)
	if err != nil {
		m.sendPolicyUnavailable(w)
		return
	}
	if !allowed {
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
	return m.stepUpPolicy.MFARequired(&iface.User{Role: claims.SystemRole}, claims.Memberships)
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
	writeCodedError(w, codedError{
		status: http.StatusUnauthorized,
		code:   "step_up_required",
		title:  "step-up authentication required",
		detail: "this action requires a fresh second-factor verification due to elevated session risk",
		scheme: schemeMFA,
		item:   &codedErrorItem{message: "step-up required — high risk session", location: "require_low_risk", value: "HIGH_RISK_SESSION"},
		extra:  map[string]any{"riskScore": score, "riskThreshold": threshold},
	})
}

// sendStepUpRequired emits the structured 401 the frontend looks for to
// drive a re-MFA prompt. Mirrors sendMFARequired's shape so clients can
// reuse most of the handler, branching only on the "code" field.
func (m *AuthMiddleware) sendStepUpRequired(w http.ResponseWriter, r *http.Request, maxAge time.Duration) {
	writeCodedError(w, codedError{
		status: http.StatusUnauthorized,
		code:   "step_up_required",
		title:  "step-up authentication required",
		detail: "this action requires a fresh second-factor verification",
		scheme: schemeMFA,
		item:   &codedErrorItem{message: "step-up required", location: "require_step_up", value: "STEP_UP_REQUIRED"},
		extra:  map[string]any{"maxAgeSeconds": int(maxAge.Seconds())},
	})
}

// sendReauthenticationRequired emits the 401 that tells a client "sign in
// again, then come back". It is the no-factor branch of
// RequireEnrolmentProof, and the only gate answer that every population
// can actually satisfy: an MFA-obligated account inside its grace window
// is refused by the password-reconfirm endpoint (D19), and an OAuth-only
// account has no password to reconfirm at all.
//
// The body carries authTime so the SPA can say how stale the session is —
// zero for a token minted before the claim shipped — and maxAgeSeconds so
// it knows the bar it has to clear. The request is deliberately not a
// parameter: nothing here reads it, as with sendPolicyUnavailable.
func (m *AuthMiddleware) sendReauthenticationRequired(w http.ResponseWriter, _ *http.Request, maxAge time.Duration, authTime int64) {
	writeCodedError(w, codedError{
		status: http.StatusUnauthorized,
		code:   "reauthentication_required",
		title:  "reauthentication required",
		detail: "adding a second factor requires a recent sign-in; please sign in again and retry",
		scheme: schemeBearer,
		item:   &codedErrorItem{message: "reauthentication required", location: "require_enrolment_proof", value: "REAUTHENTICATION_REQUIRED"},
		extra: map[string]any{
			"maxAgeSeconds": int(maxAge.Seconds()),
			"authTime":      authTime,
		},
	})
}

// amrSatisfiesStepUp is amrSatisfiesMFA PLUS "reauth". Used by
// RequireStepUp and RequireEnrolmentProof only, through
// hasFreshSecondFactor.
//
// The two lists are identical TODAY — amrSatisfiesMFA still accepts
// "reauth" — so this is a seam, not yet a behaviour change. The split
// exists because "reauth", a fresh password reconfirm, is a presence proof
// but NOT a second factor: letting it satisfy RequireMFA (which it does
// today) means a session-long gate meant to demand a second factor accepts
// a password the caller already typed once, which is audit finding M-1.
// RequireStepUp is different — it asks "did you prove presence in the last
// five minutes", and a reconfirm answers that. Narrowing amrSatisfiesMFA
// is M-1's job; this function is what makes that narrowing possible
// without collaterally breaking the freshness gates.
func amrSatisfiesStepUp(amr []string) bool {
	for _, v := range amr {
		if v == "otp" || v == "webauthn" || v == "mfa" || v == "reauth" {
			return true
		}
	}
	return false
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
	writeCodedError(w, codedError{
		status: http.StatusUnauthorized,
		code:   "password_confirm_required",
		title:  "password reconfirm required",
		detail: "this action requires a fresh password reconfirm because no second factor is enrolled",
		scheme: schemeBearer,
		item:   &codedErrorItem{message: "password confirm required", location: "require_step_up", value: "PASSWORD_CONFIRM_REQUIRED"},
		extra:  map[string]any{"maxAgeSeconds": int(maxAge.Seconds())},
	})
}

// sendPolicyUnavailable emits the 503 envelope for a sign-in policy that
// could not be evaluated (nil StepUpPolicy or a failed read). Retryable;
// deliberately NOT one of the step-up prompts — the frontend must show
// "try again shortly", not open a password modal or an enrollment nudge.
func (m *AuthMiddleware) sendPolicyUnavailable(w http.ResponseWriter) {
	// No scheme and no item, both deliberately: there is no credential to
	// re-present, and adding an errors[] here would be a wire change.
	writeCodedError(w, codedError{
		status: http.StatusServiceUnavailable,
		code:   "auth.policy_unavailable",
		title:  "sign-in policy unavailable",
		detail: "the sign-in policy could not be evaluated; try again shortly",
	})
}

// sendTokenVerificationUnavailable emits the 503 envelope for a server that
// holds no verifying key, so no bearer on any protected route can be checked
// (services.ErrJWTKeysNotLoaded). Modelled on sendPolicyUnavailable, the
// middleware's other 503, down to omitting both WWW-Authenticate and errors[]:
// the header names a scheme the caller should retry with and there is nothing
// to retry with. The code is flat rather than dotted — every code this family
// has added reads that way, and the model's punctuation is a lone survivor.
//
// Nothing about what is ACCEPTED changes: a server with no verifying key
// accepted nothing before and accepts nothing now. Only the account it gives
// of itself does. As with sendAccessTokenExpired, the request is deliberately
// not a parameter — nothing here reads it — and the code must keep exactly
// ONE emitter in backend/, or it stops meaning what it says.
func (m *AuthMiddleware) sendTokenVerificationUnavailable(w http.ResponseWriter) {
	const code = "token_verification_unavailable"
	// No scheme and no item, for the same reason as sendPolicyUnavailable.
	writeCodedError(w, codedError{
		status: http.StatusServiceUnavailable,
		code:   code,
		title:  "token verification unavailable",
		detail: "access tokens cannot be verified right now; try again shortly",
	})
}

// sendMFAEnrollmentRequired emits the 403 envelope used when the caller's
// role obligates MFA but they have no factor. Distinguished from
// password_confirm_required so the frontend nudges to /user/settings
// instead of opening the password modal.
func (m *AuthMiddleware) sendMFAEnrollmentRequired(w http.ResponseWriter, _ *http.Request) {
	writeCodedError(w, codedError{
		status: http.StatusForbidden,
		code:   "mfa_enrollment_required",
		title:  "mfa enrollment required",
		detail: "your role requires a second factor; enroll one before performing this action",
		scheme: schemeBearer,
		item:   &codedErrorItem{message: "mfa enrollment required", location: "require_step_up", value: "MFA_ENROLLMENT_REQUIRED"},
	})
}

// sendMFARequired emits the structured 401 the frontend looks for to trigger
// a step-up prompt. Shares the `step_up_required` code with sendStepUpRequired
// and sendRiskStepUp so the client switches on a single value to drive the
// MFA modal regardless of whether the gate is session-MFA, freshness, or
// risk-based.
func (m *AuthMiddleware) sendMFARequired(w http.ResponseWriter, r *http.Request) {
	writeCodedError(w, codedError{
		status: http.StatusUnauthorized,
		code:   "step_up_required",
		title:  "mfa required",
		detail: "this action requires a second authentication factor",
		scheme: schemeMFA,
		item:   &codedErrorItem{message: "mfa required", location: "require_mfa", value: "STEP_UP_REQUIRED"},
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
	// No scheme: 402 is a payment/entitlement verdict, not an
	// authentication challenge, so there is nothing to re-present.
	writeCodedError(w, codedError{
		status: http.StatusPaymentRequired,
		code:   "capability_required",
		title:  "capability required",
		detail: "tenant is not entitled to this capability",
		item:   &codedErrorItem{message: "capability required", location: "require_capability", value: "CAPABILITY_REQUIRED"},
		extra:  map[string]any{"capability": capabilityID, "tenantId": tenantID},
	})
}
