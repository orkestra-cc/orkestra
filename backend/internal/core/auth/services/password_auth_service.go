package services

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	stderrors "errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/google/uuid"
	authModels "github.com/orkestra/backend/internal/core/auth/models"
	"github.com/orkestra/backend/internal/core/auth/repository"
	notifModels "github.com/orkestra/backend/internal/core/notification/models"
	"github.com/orkestra/backend/internal/shared/blob"
	sharederrors "github.com/orkestra/backend/internal/shared/errors"
	"github.com/orkestra/backend/internal/shared/geoip"
	"github.com/orkestra/backend/pkg/sdk/iface"
)

var (
	ErrInvalidCredentials    = stderrors.New("invalid credentials")
	ErrEmailNotVerified      = stderrors.New("email not verified")
	ErrAccountLocked         = stderrors.New("account temporarily locked")
	ErrUserInactive          = stderrors.New("user account is not active")
	ErrPasswordReused        = stderrors.New("new password must differ from the current one")
	ErrNotificationDown      = stderrors.New("notifications disabled — cannot send email")
	ErrMFAEnrollmentRequired = stderrors.New("mfa enrollment required — grace period expired")
	ErrRegistrationDisabled  = stderrors.New("registration disabled for this surface")
	ErrEmailDomainNotAllowed = stderrors.New("email domain not allowed")
	ErrLoginDisabled         = stderrors.New("login disabled for this surface")
	// ErrCountryBlocked is returned when geoBlockCountries is configured
	// and the request IP resolves to a blocked country. Translated to
	// 403 auth.country_blocked at the handler boundary.
	ErrCountryBlocked = stderrors.New("country blocked by policy")
	// ErrPasswordConfirmUnavailable is returned by ConfirmPassword when
	// the user can't satisfy step-up via a password reconfirm — either
	// they have no password (pure-OAuth account) or they have at least
	// one MFA factor enrolled (and must use that stronger gate instead).
	// Translated to 409 auth.password_confirm_unavailable so the frontend
	// can nudge the user to the MFA path or a fresh OAuth flow.
	ErrPasswordConfirmUnavailable = stderrors.New("password reconfirm not available for this account")
	// ErrPasswordLoginDisabled is iface.ErrPasswordLoginDisabled (one
	// identity across the AdminAuthInviter boundary); the per-surface
	// method gates of spec §4.3 return it.
	ErrPasswordLoginDisabled = iface.ErrPasswordLoginDisabled
)

// FirstAdminClaimer is the contract the password auth service uses to
// atomically reserve the platform's super_admin seat on a fresh install.
// shared/systeminit.Repo satisfies it. Inlining the interface here keeps
// the auth module free of a hard import on shared/systeminit while still
// letting tests stub the claim behaviour.
type FirstAdminClaimer interface {
	ClaimFirstAdmin(ctx context.Context, userUUID string) (bool, error)
	Release(ctx context.Context, userUUID string) error
}

// PasswordAuthConfig configures the password auth service.
type PasswordAuthConfig struct {
	UserService              iface.UserProvider
	TenantProvider           iface.TenantProvider // required: drives RoleRequiresMFA check at login
	PasswordService          PasswordService
	JWTService               JWTService
	EmailTokenRepo           repository.EmailTokenRepository
	RefreshTokenRepo         repository.RefreshTokenRepository
	AuthSessionRepo          repository.AuthSessionRepository
	MFAFactorRepo            repository.MFAFactorRepository // required: decides partial vs full response
	MFAChallengeService      MFAChallengeService            // required: mints login-continuation challenges
	FirstAdminClaimer        FirstAdminClaimer              // required: atomic first-admin claim
	RiskAssessment           RiskAssessmentService          // nil → session gets zero-score; mandatory in prod
	DeviceTrust              DeviceTrustService             // nil → never skips MFA; Section C item #3
	SuspiciousLoginNotifier  SuspiciousLoginNotifier        // nil → no email on high-risk login; Section C item #5
	Notifier                 iface.NotificationSender
	RateLimiter              *sharederrors.RateLimiter
	FrontendURL              string
	RequireEmailVerification bool
	AppName                  string
	SupportEmail             string
	Logger                   *slog.Logger
	// Policy resolves admin-managed signup policy (registration on/off,
	// email-domain allowlist, default client role) at request time. Nil
	// is allowed: the service falls back to the legacy "always-on,
	// any domain, role=operator" behaviour.
	Policy *AuthPolicyService
	// Audience identifies which surface this service instance serves.
	// Empty defaults to operator semantics (preserves legacy behaviour
	// when the policy service is not wired).
	Audience PolicyAudience
	// GeoResolver resolves a request IP to an ISO-3166-1 country code so
	// the geoBlockCountries policy can reject login attempts. Nil
	// (geoip disabled) makes the geo-block half a no-op.
	GeoResolver geoip.Resolver
}

// PasswordAuthService handles the register / login / verify / reset / change
// password flows. It complements the existing OAuth-focused AuthService.
type PasswordAuthService struct {
	userService              iface.UserProvider
	tenantProvider           iface.TenantProvider
	passwordService          PasswordService
	jwtService               JWTService
	emailTokenRepo           repository.EmailTokenRepository
	refreshTokenRepo         repository.RefreshTokenRepository
	authSessionRepo          repository.AuthSessionRepository
	mfaFactorRepo            repository.MFAFactorRepository
	mfaChallengeService      MFAChallengeService
	firstAdminClaimer        FirstAdminClaimer
	riskAssessment           RiskAssessmentService
	deviceTrust              DeviceTrustService
	suspiciousLoginNotifier  SuspiciousLoginNotifier
	notifier                 iface.NotificationSender
	rateLimiter              *sharederrors.RateLimiter
	frontendURL              string
	requireEmailVerification bool
	appName                  string
	supportEmail             string
	logger                   *slog.Logger
	// policy resolves admin-managed signup policy. Nil = legacy fallback
	// (registration always allowed, any domain, role=operator).
	policy      *AuthPolicyService
	audience    PolicyAudience
	geoResolver geoip.Resolver
	// auditSink is wired post-construction via SetAuditSink by the compliance
	// module. Nil when compliance is disabled — emit* helpers tolerate that.
	auditSink iface.AuditSink
	// sessionRevocation pushes revoked sids into Redis so a credential
	// change invalidates access tokens immediately rather than after
	// their TTL. Wired post-construction; nil-tolerant.
	sessionRevocation SessionRevocationService
	// webauthnAvailability is the narrow checker the login flow consumes
	// to populate the partial response's WebAuthnAvailable field. Wired
	// post-construction because WebAuthn is built later in the same Init.
	// Nil when WebAuthn is disabled — completeLogin then reports false.
	webauthnAvailability HasWebAuthnCredentials
	// blobStore is consumed by every code path that builds a
	// UserManagementResponse (login, MFA partial response) so the
	// wire `avatar` field is the freshly-resolved URL, not the stale
	// stored value. Optional — nil leaves Avatar unchanged.
	blobStore blob.Store
}

// Session document retention lives in models.AuthSessionRetention — see
// its doc comment for why it's there rather than here.

// HasWebAuthnCredentials is the narrow contract login flows need from the
// WebAuthn service: a fast yes/no on whether the user has any passkey
// enrolled. Decoupled from WebAuthnService so password/OAuth services don't
// transitively pull the entire ceremony surface into their dependency graph.
type HasWebAuthnCredentials interface {
	HasWebAuthnCredentials(ctx context.Context, userUUID string) bool
}

// SetWebAuthnAvailability wires the optional checker. Safe to call before
// the first login since both are fully constructed during module Init.
func (s *PasswordAuthService) SetWebAuthnAvailability(c HasWebAuthnCredentials) {
	s.webauthnAvailability = c
}

// SetBlobStore wires the object-storage handle used to resolve the
// uploaded-avatar presigned URL on every login response. Without
// this, oauth_*/uploaded users see initials in the navbar because
// the raw User.Avatar field is empty.
func (s *PasswordAuthService) SetBlobStore(store blob.Store) {
	s.blobStore = store
}

// buildUserResponse converts a raw User to a wire response with
// Avatar resolved from AvatarSource. Mirrors authService.buildUserResponse
// — both services need the same shape on the wire.
func (s *PasswordAuthService) buildUserResponse(ctx context.Context, user *iface.User) *iface.UserManagementResponse {
	resp := user.ToResponse()
	if user.AvatarSource != "" {
		resp.Avatar = blob.ResolveAvatarURL(ctx, user, s.blobStore)
	}
	return resp
}

// NewPasswordAuthService builds a new password auth service.
func NewPasswordAuthService(cfg PasswordAuthConfig) *PasswordAuthService {
	return &PasswordAuthService{
		userService:              cfg.UserService,
		tenantProvider:           cfg.TenantProvider,
		passwordService:          cfg.PasswordService,
		jwtService:               cfg.JWTService,
		emailTokenRepo:           cfg.EmailTokenRepo,
		refreshTokenRepo:         cfg.RefreshTokenRepo,
		authSessionRepo:          cfg.AuthSessionRepo,
		mfaFactorRepo:            cfg.MFAFactorRepo,
		mfaChallengeService:      cfg.MFAChallengeService,
		firstAdminClaimer:        cfg.FirstAdminClaimer,
		riskAssessment:           cfg.RiskAssessment,
		deviceTrust:              cfg.DeviceTrust,
		suspiciousLoginNotifier:  cfg.SuspiciousLoginNotifier,
		notifier:                 cfg.Notifier,
		rateLimiter:              cfg.RateLimiter,
		frontendURL:              cfg.FrontendURL,
		requireEmailVerification: cfg.RequireEmailVerification,
		appName:                  cfg.AppName,
		supportEmail:             cfg.SupportEmail,
		logger:                   cfg.Logger,
		policy:                   cfg.Policy,
		audience:                 cfg.Audience,
		geoResolver:              cfg.GeoResolver,
	}
}

// RegisterInput is the payload for self-service signup.
type RegisterInput struct {
	Email    string
	Password string
	FullName string
	IP       string
}

// Register creates a new user with a password and sends a verification email.
func (s *PasswordAuthService) Register(ctx context.Context, in RegisterInput) (*iface.User, error) {
	email := strings.ToLower(strings.TrimSpace(in.Email))
	if email == "" || in.Password == "" || in.FullName == "" {
		return nil, fmt.Errorf("email, password and name are required")
	}

	// The first-user bootstrap bypass and the super_admin first-admin claim are
	// OPERATOR-ONLY. The first-admin sentinel is a single global document, but
	// GetUserCount is tier-scoped (client register counts only client_users), so
	// without this gate an anonymous POST /v1/auth/client/register on a fresh
	// install would see zero client users and (a) bypass the client registration
	// kill switch and (b) win the global super_admin seat — bricking the operator
	// bootstrap. A Tier-2 client is never the platform's first admin.
	isOperatorBootstrap := s.audience != PolicyAudienceClient

	// Admin-managed registration policy. Bypass for the very first operator
	// account on a fresh install — otherwise an operator who flips
	// "registrationEnabledAdmin=false" before any user exists locks themselves
	// out. Bypass detection: ask the user count; the firstAdminClaimer's atomic
	// claim later still races correctly.
	if s.policy != nil {
		isFirstUser := false
		if isOperatorBootstrap {
			if count, err := s.userService.GetUserCount(ctx, nil); err == nil && count == 0 {
				isFirstUser = true
			}
		}
		if !isFirstUser {
			if !s.policy.RegistrationAllowed(ctx, s.audience) {
				return nil, ErrRegistrationDisabled
			}
			if !s.policy.EmailDomainAllowed(ctx, s.audience, email) {
				return nil, ErrEmailDomainNotAllowed
			}
		}
	}

	// Reject signups up-front if verification is required but the
	// notification sender isn't configured.
	if s.requireEmailVerification && !iface.IsConfiguredForCategory(ctx, s.notifier, notifModels.CategoryAuthVerifyEmail) {
		return nil, ErrNotificationDown
	}

	if err := s.passwordService.ValidatePolicy(ctx, in.Password, email); err != nil {
		return nil, err
	}

	hash, err := s.passwordService.Hash(in.Password)
	if err != nil {
		return nil, fmt.Errorf("hash password: %w", err)
	}

	// Atomic first-admin claim. Previous implementation checked
	// GetUserCount()==0 and then created the user, racing with concurrent
	// signups. Now the first caller to upsert the system_init sentinel wins
	// super_admin; losers fall through to a tier-default role (operator
	// for tier-1, admin-policy-controlled for tier-2). The userUUID is
	// minted up front so we can hand it to both the claimer and the
	// user-create call.
	proposedUUID := uuid.New().String()
	// Tier-default role for a non-first signup. Operator-tier defaults to
	// "guest" (lowest system role) so a fresh password registration can't
	// grant itself elevated privileges; client-tier consults the
	// admin-configurable defaultRoleClient (falls back to "operator" when
	// unset). The first-admin claim below upgrades the very first account
	// to "super_admin" regardless of tier.
	role := "guest"
	if s.audience == PolicyAudienceClient && s.policy != nil {
		role = s.policy.DefaultClientRole(ctx)
	}
	claimed := false
	if isOperatorBootstrap && s.firstAdminClaimer != nil {
		claimed, err = s.firstAdminClaimer.ClaimFirstAdmin(ctx, proposedUUID)
		if err != nil {
			return nil, fmt.Errorf("claim first admin: %w", err)
		}
		if claimed {
			role = "super_admin"
		}
	}

	user, err := s.userService.CreateUserWithPassword(ctx, &iface.CreateUserInput{
		UUID:         proposedUUID,
		Email:        email,
		FullName:     in.FullName,
		PasswordHash: hash,
		Role:         role,
	})
	if err != nil {
		// Rollback the sentinel if we claimed it but failed to materialize
		// the user — otherwise the sentinel would block all future signups
		// from ever becoming super_admin.
		if claimed && s.firstAdminClaimer != nil {
			if relErr := s.firstAdminClaimer.Release(ctx, proposedUUID); relErr != nil {
				s.logger.Error("first-admin rollback failed — sentinel is now orphaned",
					slog.String("userUUID", proposedUUID),
					slog.String("error", relErr.Error()))
			}
		}
		return nil, err
	}

	// If verification is not required (dev), mark as verified immediately.
	if !s.requireEmailVerification {
		_ = s.userService.MarkEmailVerified(ctx, user.UUID)
		user.EmailVerified = true
		return user, nil
	}

	if err := s.sendVerificationEmail(ctx, user, in.IP); err != nil {
		s.logger.Warn("failed to send verification email",
			slog.String("user", user.UUID),
			slog.String("error", err.Error()),
		)
	}
	return user, nil
}

// RegisterInitialAdmin creates the first administrator during the first-install
// setup wizard. It bypasses email verification (the wizard runs before SMTP is
// configured) and explicitly assigns the super_admin role rather than relying
// on the first-user heuristic in Register. Returns a full TokenResponse so the
// wizard can log the operator straight in.
//
// Atomically claims the system_init first-admin sentinel before creating
// the user; returns ErrAlreadyCompleted-equivalent behaviour via the
// claimer if someone else has already taken the seat. The unique index on
// users.email is a secondary guard but no longer the primary race defense.
func (s *PasswordAuthService) RegisterInitialAdmin(ctx context.Context, email, password, fullName, ip string) (*authModels.TokenResponse, error) {
	email = strings.ToLower(strings.TrimSpace(email))
	if email == "" || password == "" || fullName == "" {
		return nil, fmt.Errorf("email, password and name are required")
	}

	if err := s.passwordService.ValidatePolicy(ctx, password, email); err != nil {
		return nil, err
	}

	hash, err := s.passwordService.Hash(password)
	if err != nil {
		return nil, fmt.Errorf("hash password: %w", err)
	}

	// Pre-mint the UUID so the sentinel references the user we're about
	// to create. Claim must succeed — this flow exists specifically to
	// promote super_admin.
	proposedUUID := uuid.New().String()
	if s.firstAdminClaimer != nil {
		claimed, err := s.firstAdminClaimer.ClaimFirstAdmin(ctx, proposedUUID)
		if err != nil {
			return nil, fmt.Errorf("claim first admin: %w", err)
		}
		if !claimed {
			return nil, fmt.Errorf("initial admin already exists")
		}
	}

	user, err := s.userService.CreateUserWithPassword(ctx, &iface.CreateUserInput{
		UUID:         proposedUUID,
		Email:        email,
		FullName:     fullName,
		PasswordHash: hash,
		Role:         "super_admin",
	})
	if err != nil {
		if s.firstAdminClaimer != nil {
			if relErr := s.firstAdminClaimer.Release(ctx, proposedUUID); relErr != nil {
				s.logger.Error("RegisterInitialAdmin: sentinel rollback failed",
					slog.String("userUUID", proposedUUID),
					slog.String("error", relErr.Error()))
			}
		}
		return nil, err
	}

	if err := s.userService.MarkEmailVerified(ctx, user.UUID); err != nil {
		s.logger.Warn("RegisterInitialAdmin: MarkEmailVerified failed",
			slog.String("user", user.UUID),
			slog.String("error", err.Error()),
		)
	}
	user.EmailVerified = true

	// Setup-mode bypass: the wizard immediately needs to PATCH
	// /v1/admin/modules/notification to configure SMTP, but that route is
	// gated by RequireMFA() — and a freshly-created admin has no factor
	// enrolled, creating a chicken-and-egg: MFA enrollment usually needs
	// working email. Mint the setup token with amr=["pwd","reauth"] +
	// LastOTPAt=now so RequireMFA and RequireStepUp(5m) both pass for the
	// duration of the wizard. This is the exact escape hatch
	// /v1/auth/me/password-confirm uses post-login — same threat model
	// (the user just typed their password into a trusted form), same
	// 5-minute window. Once they navigate away and log back in, the next
	// token carries amr=["pwd"] only and the standard MFA gate engages.
	return s.issueTokens(ctx, user, LoginInput{IP: ip, Platform: "web"}, []string{"pwd", "reauth"}, time.Now().Unix())
}

// LoginInput is the payload for email/password login.
type LoginInput struct {
	Email    string
	Password string
	IP       string
	DeviceID string
	Platform string
	// Fingerprint is the client-computed device fingerprint. Consumed
	// by the device-trust check (Section C item #3) so a returning
	// user on the same device + fingerprint can skip the MFA prompt.
	// Optional — web today doesn't compute a stable fingerprint, so
	// callers pass empty and device_trust reads only deviceID. Mobile
	// paths thread the real value from DeviceInfo.Fingerprint.
	Fingerprint string
	// UserAgent is recorded alongside a new trust grant so the
	// self-service "trusted devices" list can render something
	// human-readable. Purely informational.
	UserAgent string
}

// LoginTokenContext carries the authenticated login state that must be
// identical across JWT claims, refresh persistence, and the session row.
// It is used by MFA continuation so OAuth device/risk metadata is not
// replaced with password-flow placeholders at the final issuance step.
type LoginTokenContext struct {
	SessionID    string
	DeviceID     string
	DeviceType   string
	Platform     string
	IPAddress    string
	Fingerprint  string
	UserAgent    string
	LoginMethod  string
	RiskScore    float64
	RiskFactors  []string
	TrustLevel   string
	MFACompleted bool
}

// Login authenticates a user by email/password and returns a token pair.
// On any failure it returns ErrInvalidCredentials to avoid user enumeration.
// Rate limiting is applied against both the IP and the email address.
func (s *PasswordAuthService) Login(ctx context.Context, in LoginInput) (*authModels.TokenResponse, error) {
	email := strings.ToLower(strings.TrimSpace(in.Email))
	if email == "" || in.Password == "" {
		return nil, ErrInvalidCredentials
	}

	// Admin-managed kill switch — same shape as the registration check
	// in Register. Returns 403 to make the disabled state visible to
	// the caller (the maintenance UI relies on this to render a banner)
	// rather than silently failing as if credentials were wrong.
	if s.policy != nil && !s.policy.LoginAllowed(ctx, s.audience) {
		return nil, ErrLoginDisabled
	}

	// Geo block — checked before the rate-limiter and password lookup so
	// a blocked country never spends a token from the per-IP bucket and
	// never lights up the audit log with a noisy rejected-login row.
	// Skipped when the policy is unwired, the resolver is nil (geoip
	// disabled), or the IP can't be resolved (private/loopback) — the
	// gate must fail open on degraded geoip lookups so an outage can't
	// lock everyone out.
	if s.policy != nil && s.geoResolver != nil && in.IP != "" {
		if loc, err := s.geoResolver.Lookup(ctx, in.IP); err == nil && loc != nil && loc.Country != "" {
			if s.policy.CountryBlocked(ctx, loc.Country) {
				s.emitLoginFailed(ctx, email, "", in.IP, "country_blocked")
				return nil, ErrCountryBlocked
			}
		}
	}

	// Refresh the rate limiter's lockout config from the live policy so
	// admin edits take effect on the next login attempt without a
	// restart. New buckets pick up the updated values immediately;
	// already-running buckets ride out their existing window, which is
	// fine — the next refill cycle reflects the new policy.
	if s.policy != nil && s.rateLimiter != nil {
		s.rateLimiter.SetAuthFailedConfig(
			s.policy.LockoutThreshold(ctx),
			s.policy.LockoutDuration(ctx),
		)
	}

	// Combined IP + email bucket so that a single credential-stuffer rotating
	// IPs still trips the per-account lock.
	if s.rateLimiter != nil {
		if s.rateLimiter.IsBlocked(ctx, "ip:"+in.IP) || s.rateLimiter.IsBlocked(ctx, "email:"+email) {
			return nil, ErrAccountLocked
		}
	}

	user, err := s.userService.GetUserForAuth(ctx, email)
	if err != nil {
		// Run Verify against a dummy hash to keep timing constant whether
		// or not the user exists, foiling user enumeration via timing.
		_, _ = s.passwordService.Verify(in.Password, s.passwordService.DummyHash())
		s.recordFailed(ctx, in.IP, email)
		s.emitLoginFailed(ctx, email, "", in.IP, "unknown_user")
		return nil, ErrInvalidCredentials
	}

	// Inactive-account auto-disable: if the policy is configured and
	// the user's lastLogin is older than the threshold, flip
	// isActive=false before the IsActive check below so the next branch
	// returns ErrInvalidCredentials and emits the standard
	// user_inactive audit event. We skip users who never logged in at
	// all — disabling those would brick fresh signups whose initial
	// login hasn't completed yet.
	if days := s.inactiveAccountThresholdDays(ctx); days > 0 && user.LastLogin != nil {
		threshold := time.Now().Add(-time.Duration(days) * 24 * time.Hour)
		if user.LastLogin.Before(threshold) && user.IsActive {
			isActive := false
			if _, uerr := s.userService.UpdateUser(ctx, user.UUID, &iface.UpdateUserInput{IsActive: &isActive}); uerr == nil {
				user.IsActive = false
				s.emitAudit(ctx, iface.AuditEvent{
					ActorUserID: user.UUID,
					ActorEmail:  user.Email,
					ActorType:   "system",
					Action:      "auth.account.auto_disabled",
					Outcome:     "success",
					IPAddress:   in.IP,
					Metadata: map[string]any{
						"thresholdDays": days,
						"lastLogin":     user.LastLogin.UTC().Format(time.RFC3339),
					},
				})
			} else if s.logger != nil {
				s.logger.Warn("auth: failed to auto-disable inactive account",
					slog.String("user_uuid", user.UUID),
					slog.String("error", uerr.Error()))
			}
		}
	}

	if !user.IsActive {
		s.recordFailed(ctx, in.IP, email)
		s.emitLoginFailed(ctx, email, user.UUID, in.IP, "user_inactive")
		return nil, ErrInvalidCredentials
	}
	// Service principals authenticate only through the client-credentials
	// grant; every interactive surface is closed by construction.
	if user.Kind == iface.UserKindService {
		s.recordFailed(ctx, in.IP, email)
		s.emitLoginFailed(ctx, email, user.UUID, in.IP, "service_principal")
		return nil, ErrInvalidCredentials
	}
	if user.LockedUntil != nil && time.Now().Before(*user.LockedUntil) {
		s.emitLoginFailed(ctx, email, user.UUID, in.IP, "account_locked")
		return nil, ErrAccountLocked
	}
	if user.PasswordHash == "" {
		// Account exists but was created via OAuth — don't leak that fact.
		_, _ = s.passwordService.Verify(in.Password, s.passwordService.DummyHash())
		s.recordFailed(ctx, in.IP, email)
		s.emitLoginFailed(ctx, email, user.UUID, in.IP, "no_password")
		return nil, ErrInvalidCredentials
	}

	ok, err := s.passwordService.Verify(in.Password, user.PasswordHash)
	if err != nil || !ok {
		s.recordFailed(ctx, in.IP, email)
		// Threshold and duration come from the admin-managed policy —
		// the same values already plumbed into the rate limiter above.
		// This branch used to compare against a hardcoded 5, so an
		// operator who tightened accountLockoutThreshold got the new
		// value on the in-memory bucket but the old one on the
		// persisted User.LockedUntil.
		var lockUntil *time.Time
		if user.FailedLoginCount+1 >= s.policy.LockoutThreshold(ctx) {
			t := time.Now().Add(s.policy.LockoutDuration(ctx))
			lockUntil = &t
		}
		_ = s.userService.RecordFailedLogin(ctx, user.UUID, lockUntil)
		s.emitLoginFailed(ctx, email, user.UUID, in.IP, "bad_password")
		return nil, ErrInvalidCredentials
	}

	if s.requireEmailVerification && !user.EmailVerified {
		s.emitLoginFailed(ctx, email, user.UUID, in.IP, "email_unverified")
		return nil, ErrEmailNotVerified
	}

	// Upgrade the hash if parameters changed since it was stored.
	if s.passwordService.NeedsRehash(user.PasswordHash) {
		if newHash, err := s.passwordService.Hash(in.Password); err == nil {
			_ = s.userService.UpdatePasswordHash(ctx, user.UUID, newHash)
		}
	}

	// Successful login: clear the failed counter.
	_ = s.userService.ClearFailedLogins(ctx, user.UUID)

	s.emitAudit(ctx, iface.AuditEvent{
		ActorUserID: user.UUID,
		ActorEmail:  user.Email,
		ActorType:   "user",
		Action:      "auth.login.succeeded",
		Outcome:     "success",
		IPAddress:   in.IP,
		Metadata: map[string]any{
			"deviceId": in.DeviceID,
			"platform": in.Platform,
		},
	})

	return s.completeLogin(ctx, user, in, []string{"pwd"})
}

// emitLoginFailed is a terse helper for the many login-failure branches.
// Captures the rejection reason in metadata so auditors can distinguish
// credential stuffing from locked-account retries.
func (s *PasswordAuthService) emitLoginFailed(ctx context.Context, email, userUUID, ip, reason string) {
	actorType := "anonymous"
	if userUUID != "" {
		actorType = "user"
	}
	s.emitAudit(ctx, iface.AuditEvent{
		ActorUserID: userUUID,
		ActorEmail:  email,
		ActorType:   actorType,
		Action:      "auth.login.failed",
		Outcome:     "failure",
		IPAddress:   ip,
		Metadata:    map[string]any{"reason": reason},
	})
}

// completeLogin applies the MFA decision tree to a user who has already
// satisfied primary credentials. `sourceAMR` is the list of factors used so
// far (["pwd"] for password, ["oauth"] for OAuth). Returns one of:
//   - full TokenResponse (no MFA required, or grace window still open)
//   - partial TokenResponse with RequiresMFA=true (factor enrolled, client
//     must call /v1/auth/mfa/login/verify)
//   - ErrMFAEnrollmentRequired (privileged user, no factor, grace expired)
func (s *PasswordAuthService) completeLogin(ctx context.Context, user *iface.User, in LoginInput, sourceAMR []string) (*authModels.TokenResponse, error) {
	memberships := s.loadMembershipsAsAuthModel(ctx, user.UUID)
	requires := s.policy.MFARequired(user, memberships)
	if !requires {
		return s.issueTokens(ctx, user, in, sourceAMR, 0)
	}

	// Privileged user: check enrollment. We treat "has TOTP" OR "has at
	// least one passkey" as enrolled — either factor satisfies the partial
	// response. WebAuthnAvailable on the response tells the UI whether to
	// offer the passkey button alongside the code field.
	hasTOTP := false
	if s.mfaFactorRepo != nil {
		factor, err := s.mfaFactorRepo.FindByUserAndType(ctx, user.UUID, authModels.MFAFactorTOTP)
		if err == nil && factor != nil {
			hasTOTP = true
		} else if err != nil && !stderrors.Is(err, repository.ErrMFAFactorNotFound) {
			return nil, err
		}
	}
	hasWebAuthn := false
	if s.webauthnAvailability != nil {
		hasWebAuthn = s.webauthnAvailability.HasWebAuthnCredentials(ctx, user.UUID)
	}
	if hasTOTP || hasWebAuthn {
		// Device trust (Section C item #3): if the user has a live
		// "remember this device" grant for (deviceID, fingerprint),
		// skip the MFA prompt entirely and mint a full token pair.
		// The new token's amr carries the prior factor forward plus
		// a "device_trust" annotation so RequireMFA passes but
		// RequireStepUp (which needs a fresh LastOTPAt) still
		// prompts for catastrophic actions. LastOTPAt stays at 0.
		if s.deviceTrust != nil && in.DeviceID != "" {
			trusted, doc, err := s.deviceTrust.IsTrusted(ctx, user.UUID, in.DeviceID, in.Fingerprint)
			if err == nil && trusted && doc != nil {
				amr := append([]string(nil), sourceAMR...)
				if doc.GrantedAMR != "" {
					amr = append(amr, doc.GrantedAMR)
				}
				amr = append(amr, authModels.DeviceTrustAMR)
				return s.issueTokens(ctx, user, in, amr, 0)
			}
			if err != nil && s.logger != nil {
				s.logger.Warn("device_trust: lookup failed during login, falling through to MFA prompt",
					slog.String("user_uuid", user.UUID),
					slog.String("device_id", in.DeviceID),
					slog.String("error", err.Error()))
			}
		}
		if s.mfaChallengeService == nil {
			return nil, fmt.Errorf("mfa challenge service not wired")
		}
		ch, err := s.mfaChallengeService.BeginLogin(ctx, LoginChallengeInput{
			UserUUID:    user.UUID,
			SessionID:   uuid.NewString(),
			SourceAMR:   sourceAMR,
			DeviceID:    in.DeviceID,
			DeviceType:  "desktop",
			Platform:    in.Platform,
			IPAddress:   in.IP,
			Fingerprint: in.Fingerprint,
			UserAgent:   in.UserAgent,
			LoginMethod: "password",
		})
		if err != nil {
			return nil, err
		}
		return &authModels.TokenResponse{
			RequiresMFA:       true,
			MFAToken:          ch.ID,
			WebAuthnAvailable: hasWebAuthn,
			User:              s.buildUserResponse(ctx, user),
		}, nil
	}

	// Privileged, no factor → grace logic.
	now := time.Now()
	if s.policy.MFAGraceExpired(ctx, user, now) {
		return nil, ErrMFAEnrollmentRequired
	}
	if user.MFAGraceStartedAt == nil {
		_ = s.userService.StartMFAGraceIfUnset(ctx, user.UUID)
		// Re-read so the response carries the correct expiry.
		if fresh, err := s.userService.GetUserForAuth(ctx, user.Email); err == nil && fresh != nil {
			user = fresh
		}
	}
	resp, err := s.issueTokens(ctx, user, in, sourceAMR, 0)
	if err != nil {
		return nil, err
	}
	resp.MFAEnrollmentRequired = true
	if deadline := s.policy.MFAGraceExpiresAt(ctx, user); !deadline.IsZero() {
		resp.MFAGraceExpiresAt = &deadline
	}
	return resp, nil
}

// loadMembershipsAsAuthModel pulls the user's memberships from the tenant
// provider and converts them to the lightweight OrgMembership shape the
// policy helper consumes. Returns nil on error or when the provider is
// missing — RoleRequiresMFA then falls back to the system-role check.
func (s *PasswordAuthService) loadMembershipsAsAuthModel(ctx context.Context, userUUID string) []authModels.TenantMembership {
	if s.tenantProvider == nil {
		return nil
	}
	list, err := s.tenantProvider.ListUserMemberships(ctx, userUUID)
	if err != nil || len(list) == 0 {
		return nil
	}
	out := make([]authModels.TenantMembership, 0, len(list))
	for _, m := range list {
		out = append(out, authModels.TenantMembership{TenantUUID: m.TenantUUID, TenantKind: m.TenantKind, Roles: m.Roles})
	}
	return out
}

// SetAuditSink wires the compliance audit sink post-construction. The
// compliance module is optional, so auth's audit-emission helpers skip
// silently when sink is nil.
func (s *PasswordAuthService) SetAuditSink(sink iface.AuditSink) {
	s.auditSink = sink
}

// RefreshTokenTTL surfaces the JWT service's refresh lifetime so the
// handler can size the refresh cookie to the token it carries instead
// of a literal.
func (s *PasswordAuthService) RefreshTokenTTL() time.Duration {
	if s.jwtService == nil {
		return 7 * 24 * time.Hour
	}
	return s.jwtService.RefreshTokenTTL()
}

// SetSessionRevocation wires the Redis-backed sid revocation set so a
// credential change can kill in-flight access tokens instead of waiting
// out their TTL. Optional — nil degrades revocation to "refresh tokens
// and session docs only", which still stops new tokens from being
// minted.
func (s *PasswordAuthService) SetSessionRevocation(rev SessionRevocationService) {
	s.sessionRevocation = rev
}

// revokeSessionsAfterCredentialChange evicts every way the old password
// could still be in use, sparing only keepSID.
//
// A credential change has to close FOUR pathways, and closing three of
// them is worth very little:
//
//  1. refresh tokens — stops new access tokens from being minted;
//  2. session docs — stops the session showing as live in the UI and
//     cascades to anything keyed on the session;
//  3. the Redis sid revocation set — kills access tokens already in
//     flight, which otherwise stay valid for their full TTL;
//  4. device-trust grants — otherwise whoever holds the old password
//     still skips the MFA prompt on their next login, which is exactly
//     the property a compromised-account reset needs to destroy.
//
// keepSID spares one session: ChangePassword passes the caller's own sid
// because the caller just proved knowledge of the current password, so
// signing them out of the tab they are sitting in achieves nothing.
// ResetPassword passes "" — a reset is a recovery action and there is no
// session worth preserving. With keepSID empty we also sweep by user, so
// refresh rows that predate session docs (or whose session doc has gone)
// cannot survive.
//
// Best-effort throughout: the password is already changed by the time
// this runs, and failing the request would leave the caller believing
// the change did not happen. Failures are logged.
func (s *PasswordAuthService) revokeSessionsAfterCredentialChange(ctx context.Context, userUUID, reason, keepSID, trustReason string) int {
	revoked := 0

	if s.authSessionRepo != nil {
		sessions, err := s.authSessionRepo.GetActiveSessionsByUser(ctx, userUUID)
		if err != nil && s.logger != nil {
			s.logger.Warn("auth: could not list sessions for credential-change revocation",
				slog.String("user_uuid", userUUID),
				slog.String("error", err.Error()))
		}
		for _, sess := range sessions {
			if sess == nil || sess.UUID == "" || sess.UUID == keepSID {
				continue
			}
			if s.refreshTokenRepo != nil {
				if err := s.refreshTokenRepo.RevokeTokensBySession(ctx, sess.UUID, reason); err != nil && s.logger != nil {
					s.logger.Warn("auth: revoke refresh tokens by session failed",
						slog.String("session_uuid", sess.UUID),
						slog.String("error", err.Error()))
				}
			}
			if err := s.authSessionRepo.TerminateSession(ctx, sess.UUID); err != nil && s.logger != nil {
				s.logger.Warn("auth: terminate session doc failed",
					slog.String("session_uuid", sess.UUID),
					slog.String("error", err.Error()))
			}
			if s.sessionRevocation != nil {
				_ = s.sessionRevocation.Revoke(ctx, sess.UUID, reason)
			}
			revoked++
		}
	}

	// Full sweep only when nothing is being spared — RevokeTokensByUser
	// cannot exclude a session, so running it with a keepSID set would
	// sign the caller out along with everyone else.
	if keepSID == "" && s.refreshTokenRepo != nil {
		if err := s.refreshTokenRepo.RevokeTokensByUser(ctx, userUUID, reason); err != nil && s.logger != nil {
			s.logger.Warn("auth: revoke refresh tokens by user failed",
				slog.String("user_uuid", userUUID),
				slog.String("error", err.Error()))
		}
	}

	if s.deviceTrust != nil {
		if err := s.deviceTrust.RevokeAllByUser(ctx, userUUID, trustReason); err != nil && s.logger != nil {
			s.logger.Warn("device_trust: revoke on credential change failed",
				slog.String("user_uuid", userUUID),
				slog.String("error", err.Error()))
		}
	}

	return revoked
}

// emitAudit is a best-effort wrapper over auditSink.Emit that no-ops when
// the sink is unwired. Kept terse so callers can sprinkle emits through
// the login/password flows without noise.
func (s *PasswordAuthService) emitAudit(ctx context.Context, event iface.AuditEvent) {
	if s.auditSink == nil {
		return
	}
	s.auditSink.Emit(ctx, event)
}

// VerifyEmail consumes a verification token and marks the user verified.
func (s *PasswordAuthService) VerifyEmail(ctx context.Context, rawToken string) error {
	doc, err := s.lookupEmailToken(ctx, rawToken, authModels.EmailTokenPurposeVerifyEmail)
	if err != nil {
		return err
	}
	if err := s.userService.MarkEmailVerified(ctx, doc.UserUUID); err != nil {
		return err
	}
	if err := s.emailTokenRepo.MarkUsed(ctx, doc.TokenHash); err != nil {
		return err
	}
	s.emitAudit(ctx, iface.AuditEvent{
		ActorUserID:  doc.UserUUID,
		ActorType:    "user",
		Action:       "auth.email.verified",
		Outcome:      "success",
		ResourceType: "user",
		ResourceID:   doc.UserUUID,
	})
	return nil
}

// ResendVerification issues a new verification email.
//
// Always returns nil regardless of outcome so callers cannot distinguish
// "address unknown", "already verified", "rate-limited", or "sent" — the
// public-facing 200 response stays neutral. Rate limiting shares the
// same per-IP and per-email buckets as Login/ForgotPassword so an
// attacker can't bypass one surface by hitting another.
func (s *PasswordAuthService) ResendVerification(ctx context.Context, email, ip string) error {
	email = strings.ToLower(strings.TrimSpace(email))
	if s.rateLimiter != nil {
		if s.rateLimiter.IsBlocked(ctx, "ip:"+ip) || s.rateLimiter.IsBlocked(ctx, "email:"+email) {
			return nil
		}
	}
	user, err := s.userService.GetUserForAuth(ctx, email)
	if err != nil {
		return nil
	}
	if user.EmailVerified {
		return nil
	}
	_ = s.emailTokenRepo.InvalidateByUserAndPurpose(ctx, user.UUID, authModels.EmailTokenPurposeVerifyEmail)
	if err := s.sendVerificationEmail(ctx, user, ip); err != nil {
		return err
	}
	// Each successful send counts toward the shared limiter so a script
	// spamming the endpoint from one IP / against one address trips the
	// same lockout window that protects login.
	s.recordFailed(ctx, ip, email)
	return nil
}

// ForgotPassword issues a reset token and emails it. Always returns nil
// regardless of whether the email exists (prevents enumeration).
func (s *PasswordAuthService) ForgotPassword(ctx context.Context, email, ip string) error {
	email = strings.ToLower(strings.TrimSpace(email))
	user, err := s.userService.GetUserForAuth(ctx, email)
	if err != nil || user == nil {
		return nil
	}
	if !user.IsActive {
		return nil
	}
	_ = s.emailTokenRepo.InvalidateByUserAndPurpose(ctx, user.UUID, authModels.EmailTokenPurposeResetPassword)

	raw, hash, err := generateEmailToken()
	if err != nil {
		return nil
	}
	resetTTL := s.resetTokenTTL(ctx)
	doc := &authModels.EmailTokenDoc{
		UUID:      uuid.Must(uuid.NewV7()).String(),
		UserUUID:  user.UUID,
		TokenHash: hash,
		Purpose:   authModels.EmailTokenPurposeResetPassword,
		IP:        ip,
		CreatedAt: time.Now(),
		ExpiresAt: time.Now().Add(resetTTL),
	}
	if err := s.emailTokenRepo.Create(ctx, doc); err != nil {
		return nil
	}

	if !iface.IsConfiguredForCategory(ctx, s.notifier, notifModels.CategoryAuthResetPassword) {
		s.logger.Warn("forgot password: notifier not configured, cannot send email")
		return nil
	}

	resetURL := s.frontendURL + "/reset-password?token=" + raw
	_, err = s.notifier.SendTemplated(ctx, iface.TemplatedNotificationRequest{
		Channel:    "email",
		Type:       "transactional",
		Category:   notifModels.CategoryAuthResetPassword,
		TemplateID: "auth.reset_password",
		Recipients: []iface.Recipient{{
			UserUUID: user.UUID,
			Address:  user.Email,
			Name:     user.FullName,
		}},
		Data: map[string]any{
			"UserName":     coalesce(user.FullName, user.Email),
			"ResetURL":     resetURL,
			"ExpiresIn":    humanDuration(resetTTL),
			"RequestIP":    ip,
			"AppName":      s.appName,
			"SupportEmail": s.supportEmail,
		},
		IdempotencyKey: "reset:" + user.UUID + ":" + doc.UUID,
	})
	if err != nil {
		s.logger.Warn("forgot password: failed to send email", slog.String("error", err.Error()))
	}
	return nil
}

// ResetPassword consumes a reset token, updates the password, and
// invalidates all outstanding refresh tokens.
func (s *PasswordAuthService) ResetPassword(ctx context.Context, rawToken, newPassword string) error {
	doc, err := s.lookupEmailToken(ctx, rawToken, authModels.EmailTokenPurposeResetPassword)
	if err != nil {
		return err
	}

	user, err := s.userService.GetUserByID(ctx, doc.UserUUID)
	if err != nil {
		return err
	}
	if err := s.passwordService.ValidatePolicy(ctx, newPassword, user.Email); err != nil {
		return err
	}
	hash, err := s.passwordService.Hash(newPassword)
	if err != nil {
		return err
	}
	if err := s.userService.UpdatePasswordHash(ctx, user.UUID, hash); err != nil {
		return err
	}
	_ = s.emailTokenRepo.MarkUsed(ctx, doc.TokenHash)
	_ = s.userService.ClearFailedLogins(ctx, user.UUID)

	// A reset is the recovery action for a compromised account, so it
	// must evict EVERY session — including the attacker's in-flight
	// access token and their device-trust grant, which previously
	// survived a reset and let them keep skipping the MFA prompt.
	// keepSID is empty: nothing is spared, and the by-user refresh sweep
	// runs on top of the per-session teardown.
	revoked := s.revokeSessionsAfterCredentialChange(ctx, user.UUID,
		"password_reset", "", authModels.DeviceTrustRevokedOnPasswordReset)
	if s.logger != nil {
		s.logger.Info("auth: sessions revoked after password reset",
			slog.String("user_uuid", user.UUID),
			slog.Int("sessions_revoked", revoked))
	}
	s.emitAudit(ctx, iface.AuditEvent{
		ActorUserID:  user.UUID,
		ActorEmail:   user.Email,
		ActorType:    "user",
		Action:       "auth.password.reset_completed",
		Outcome:      "success",
		ResourceType: "user",
		ResourceID:   user.UUID,
	})
	return nil
}

// ChangePasswordInput bundles the arguments for a self-service password
// change. CurrentSID names the session the caller is making the request
// from so it can be spared when every other session is evicted — pass ""
// (no sid on the token) to evict everything.
type ChangePasswordInput struct {
	UserUUID   string
	CurrentSID string
	Current    string
	New        string
}

// ChangePassword updates the password for an authenticated user who
// supplied the current password.
func (s *PasswordAuthService) ChangePassword(ctx context.Context, in ChangePasswordInput) error {
	userUUID, current, next := in.UserUUID, in.Current, in.New
	user, err := s.userService.GetUserByID(ctx, userUUID)
	if err != nil {
		return err
	}
	if user.PasswordHash == "" {
		return ErrInvalidCredentials
	}
	ok, err := s.passwordService.Verify(current, user.PasswordHash)
	if err != nil || !ok {
		return ErrInvalidCredentials
	}
	if current == next {
		return ErrPasswordReused
	}
	if err := s.passwordService.ValidatePolicy(ctx, next, user.Email); err != nil {
		return err
	}
	hash, err := s.passwordService.Hash(next)
	if err != nil {
		return err
	}
	if err := s.userService.UpdatePasswordHash(ctx, user.UUID, hash); err != nil {
		return err
	}
	// A password change signs out every OTHER device — that is the point
	// of changing a password — and drops every "remember this device"
	// grant, so a stolen password cannot piggyback on a trust row the
	// legitimate owner created before the breach (Section C item #3).
	//
	// The caller's own session is spared: they just proved knowledge of
	// the current password, and signing them out of the tab they are
	// sitting in evicts the one person we know is legitimate. (The
	// previous behaviour did exactly that — revoked the caller's sid and
	// left every other device running.)
	//
	// Phase 8: gated on the revokeSessionsOnPasswordChange policy toggle
	// so admins can opt out for staged-rollout / migration workflows.
	// Best-effort — a revoke failure doesn't roll the password update back.
	if s.shouldRevokeOnPasswordChange(ctx) {
		revoked := s.revokeSessionsAfterCredentialChange(ctx, user.UUID,
			"password_change", in.CurrentSID, authModels.DeviceTrustRevokedOnPasswordChange)
		if s.logger != nil {
			s.logger.Info("auth: sessions revoked after password change",
				slog.String("user_uuid", user.UUID),
				slog.Int("sessions_revoked", revoked))
		}
	}
	s.emitAudit(ctx, iface.AuditEvent{
		ActorUserID:  user.UUID,
		ActorEmail:   user.Email,
		ActorType:    "user",
		Action:       "auth.password.changed",
		Outcome:      "success",
		ResourceType: "user",
		ResourceID:   user.UUID,
	})
	return nil
}

// ConfirmPasswordResult carries the stepped-up access token minted
// after a successful password reconfirm. RefreshToken is intentionally
// not rotated — the reconfirm only refreshes the bearer's MFA proof so
// the in-flight destructive request can replay. Session + refresh-token
// lineage stay untouched so the user keeps their other sessions.
type ConfirmPasswordResult struct {
	AccessToken string
	TokenType   string
	ExpiresIn   int64
}

// ConfirmPassword verifies the user's password and mints a stepped-up
// access token carrying the caller's existing session, amr=(priorAMR ∪
// {"reauth"}), and last_otp_at=now.
// Used by RequireStepUp's fallback path for users who can't satisfy the
// standard MFA gate because no factor is enrolled.
//
// Refuses with ErrPasswordConfirmUnavailable when:
//   - the user has no password set (pure-OAuth account); the caller is
//     expected to start a fresh OAuth flow to reauthenticate instead.
//   - the user has any MFA factor (TOTP or WebAuthn) enrolled; a
//     password reconfirm would defeat the stronger gate, so the caller
//     must use the MFA path.
//
// A wrong password returns ErrInvalidCredentials so the handler can
// emit 401 (and the IP/email failure counters tick the same way as a
// failed login). The 5-minute freshness window is enforced downstream
// by RequireStepUp comparing last_otp_at — this method always stamps now.
func (s *PasswordAuthService) ConfirmPassword(ctx context.Context, userUUID, password string, priorAMR []string, ip, sessionID, deviceID string) (*ConfirmPasswordResult, error) {
	return s.ConfirmPasswordWithSecurity(ctx, userUUID, password, priorAMR,
		&authModels.DeviceInfo{DeviceID: deviceID},
		&authModels.SecurityContext{SessionID: sessionID, IPAddress: ip, Timestamp: time.Now()})
}

// ConfirmPasswordWithSecurity preserves the verified access token's device,
// network, and risk binding while adding the fresh reauthentication proof.
func (s *PasswordAuthService) ConfirmPasswordWithSecurity(ctx context.Context, userUUID, password string, priorAMR []string, device *authModels.DeviceInfo, security *authModels.SecurityContext) (*ConfirmPasswordResult, error) {
	if userUUID == "" || password == "" || security == nil || security.SessionID == "" {
		return nil, ErrInvalidCredentials
	}
	if device == nil {
		device = &authModels.DeviceInfo{}
	}
	user, err := s.userService.GetUserByID(ctx, userUUID)
	if err != nil {
		return nil, err
	}
	if user == nil {
		return nil, ErrInvalidCredentials
	}
	if err := ValidateTokenEligibleUser(user); err != nil {
		return nil, ErrInvalidCredentials
	}
	if user.PasswordHash == "" {
		return nil, ErrPasswordConfirmUnavailable
	}
	// Refuse when the user already has a stronger factor. The frontend
	// should never reach this endpoint in that case (the middleware
	// emits step_up_required, not password_confirm_required), but the
	// check is defensive: a crafted direct call must not be able to
	// bypass MFA.
	if s.mfaFactorRepo != nil {
		if totp, err := s.mfaFactorRepo.FindByUserAndType(ctx, userUUID, authModels.MFAFactorTOTP); err == nil && totp != nil {
			return nil, ErrPasswordConfirmUnavailable
		} else if err != nil && !stderrors.Is(err, repository.ErrMFAFactorNotFound) {
			return nil, err
		}
		if wa, err := s.mfaFactorRepo.FindByUserAndType(ctx, userUUID, authModels.MFAFactorWebAuthn); err == nil && wa != nil && len(wa.WebAuthnCredentials) > 0 {
			return nil, ErrPasswordConfirmUnavailable
		} else if err != nil && !stderrors.Is(err, repository.ErrMFAFactorNotFound) {
			return nil, err
		}
	}
	ok, err := s.passwordService.Verify(password, user.PasswordHash)
	if err != nil || !ok {
		s.recordFailed(ctx, security.IPAddress, user.Email)
		return nil, ErrInvalidCredentials
	}
	// Mint the stepped-up token. amr is priorAMR ∪ {"reauth"} so the
	// authentication lineage stays inspectable (e.g. a token minted from
	// an oauth login will carry ["oauth","reauth"], not just ["reauth"]).
	amr := mergeAMRWithReauth(priorAMR)
	now := time.Now()
	securityCtx := *security
	securityCtx.Timestamp = now
	deviceInfo := *device
	token, err := s.jwtService.GenerateAccessTokenForSessionWithAMR(user, &deviceInfo, &securityCtx, amr, now.Unix())
	if err != nil {
		return nil, err
	}
	s.emitAudit(ctx, iface.AuditEvent{
		ActorUserID:  user.UUID,
		ActorEmail:   user.Email,
		ActorType:    "user",
		Action:       "auth.password.reconfirmed",
		Outcome:      "success",
		ResourceType: "user",
		ResourceID:   user.UUID,
	})
	return &ConfirmPasswordResult{
		AccessToken: token,
		TokenType:   "Bearer",
		ExpiresIn:   int64(s.jwtService.AccessTokenTTL(ctx).Seconds()),
	}, nil
}

// mergeAMRWithReauth returns priorAMR with "reauth" appended, deduplicated.
// Falls back to ["pwd","reauth"] when priorAMR is empty so the token still
// reflects that a password was just verified.
func mergeAMRWithReauth(prior []string) []string {
	out := make([]string, 0, len(prior)+1)
	seenReauth := false
	for _, v := range prior {
		out = append(out, v)
		if v == "reauth" {
			seenReauth = true
		}
	}
	if len(out) == 0 {
		out = append(out, "pwd")
	}
	if !seenReauth {
		out = append(out, "reauth")
	}
	return out
}

// --- internal helpers ---

func (s *PasswordAuthService) recordFailed(ctx context.Context, ip, email string) {
	if s.rateLimiter == nil {
		return
	}
	s.rateLimiter.RecordFailedAuth(ctx, "ip:"+ip)
	s.rateLimiter.RecordFailedAuth(ctx, "email:"+email)
}

func (s *PasswordAuthService) sendVerificationEmail(ctx context.Context, user *iface.User, ip string) error {
	raw, hash, err := generateEmailToken()
	if err != nil {
		return err
	}
	doc := &authModels.EmailTokenDoc{
		UUID:      uuid.Must(uuid.NewV7()).String(),
		UserUUID:  user.UUID,
		TokenHash: hash,
		Purpose:   authModels.EmailTokenPurposeVerifyEmail,
		IP:        ip,
		CreatedAt: time.Now(),
		ExpiresAt: time.Now().Add(24 * time.Hour),
	}
	if err := s.emailTokenRepo.Create(ctx, doc); err != nil {
		return err
	}

	if !iface.IsConfiguredForCategory(ctx, s.notifier, notifModels.CategoryAuthVerifyEmail) {
		return ErrNotificationDown
	}

	verifyURL := s.frontendURL + "/verify-email?token=" + raw
	_, err = s.notifier.SendTemplated(ctx, iface.TemplatedNotificationRequest{
		Channel:    "email",
		Type:       "transactional",
		Category:   notifModels.CategoryAuthVerifyEmail,
		TemplateID: "auth.verify_email",
		Recipients: []iface.Recipient{{
			UserUUID: user.UUID,
			Address:  user.Email,
			Name:     user.FullName,
		}},
		Data: map[string]any{
			"UserName":     coalesce(user.FullName, user.Email),
			"VerifyURL":    verifyURL,
			"ExpiresIn":    "24 hours",
			"AppName":      s.appName,
			"SupportEmail": s.supportEmail,
		},
		IdempotencyKey: "verify:" + user.UUID + ":" + doc.UUID,
	})
	return err
}

func (s *PasswordAuthService) lookupEmailToken(ctx context.Context, raw, purpose string) (*authModels.EmailTokenDoc, error) {
	if raw == "" {
		return nil, ErrInvalidCredentials
	}
	hash := hashEmailToken(raw)
	doc, err := s.emailTokenRepo.GetByHash(ctx, hash)
	if err != nil {
		return nil, ErrInvalidCredentials
	}
	if doc.Purpose != purpose {
		return nil, ErrInvalidCredentials
	}
	if doc.UsedAt != nil {
		return nil, ErrInvalidCredentials
	}
	if time.Now().After(doc.ExpiresAt) {
		return nil, ErrInvalidCredentials
	}
	return doc, nil
}

// IssueLoginTokens mints a full access + refresh pair for an already-
// authenticated user and persists the refresh token / session rows.
// Exposed for consumers that complete a login outside the password flow —
// currently the MFA login-verify handler, later the refresh rotation path.
// amr records which factors were completed; lastOTPAt is 0 when no OTP
// step has happened on this request. Rejects a service-principal user
// (ErrInvalidCredentials) at the shared issueTokens chokepoint — see that
// function's guard comment.
func (s *PasswordAuthService) IssueLoginTokens(ctx context.Context, user *iface.User, deviceID, platform, ip string, amr []string, lastOTPAt int64) (*authModels.TokenResponse, error) {
	return s.issueTokens(ctx, user, LoginInput{DeviceID: deviceID, Platform: platform, IP: ip}, amr, lastOTPAt)
}

// IssueLoginTokensForSession completes a partial login against the session
// UUID chosen before the MFA challenge was returned.
func (s *PasswordAuthService) IssueLoginTokensForSession(ctx context.Context, user *iface.User, in LoginTokenContext, amr []string, lastOTPAt int64) (*authModels.TokenResponse, error) {
	return s.issueTokensForSession(ctx, user, in, amr, lastOTPAt)
}

// IssueLoginTokensExternal is the iface.LoginTokenIssuer-shaped wrapper
// around IssueLoginTokens. Extracted addons (today: identity, for the
// OIDC bridge) consume it through the kernel's ServiceRegistry without
// importing this package's concrete types. Returns the SDK-canonical
// `iface.LoginTokens` shape instead of the auth-internal
// `authModels.TokenResponse`; only the five fields external callers
// need are carried over. `lastOTPAt` is fixed at 0 because federated
// flows don't satisfy the OTP step — when an SP that adds an MFA
// requirement on top of OIDC appears, the caller will mint via
// IssueLoginTokens directly.
func (s *PasswordAuthService) IssueLoginTokensExternal(ctx context.Context, user *iface.User, deviceID, platform, ip string, amr []string) (*iface.LoginTokens, error) {
	resp, err := s.IssueLoginTokens(ctx, user, deviceID, platform, ip, amr, 0)
	if err != nil {
		return nil, err
	}
	return &iface.LoginTokens{
		AccessToken:  resp.AccessToken,
		RefreshToken: resp.RefreshToken,
		TokenType:    resp.TokenType,
		ExpiresIn:    resp.ExpiresIn,
		User:         resp.User,
	}, nil
}

func (s *PasswordAuthService) issueTokens(ctx context.Context, user *iface.User, in LoginInput, amr []string, lastOTPAt int64) (*authModels.TokenResponse, error) {
	return s.issueTokensForSession(ctx, user, LoginTokenContext{
		SessionID: uuid.NewString(), DeviceID: in.DeviceID, DeviceType: "desktop",
		Platform: in.Platform, IPAddress: in.IP, Fingerprint: in.Fingerprint,
		UserAgent: in.UserAgent, LoginMethod: loginMethodFromAMR(amr),
		MFACompleted: hasMFAAMR(amr),
	}, amr, lastOTPAt)
}

func loginMethodFromAMR(amr []string) string {
	for _, method := range amr {
		if method == "oauth" {
			return "oauth"
		}
	}
	return "password"
}

func hasMFAAMR(amr []string) bool {
	for _, method := range amr {
		if method == "otp" || method == "webauthn" {
			return true
		}
	}
	return false
}

func (s *PasswordAuthService) issueTokensForSession(ctx context.Context, user *iface.User, in LoginTokenContext, amr []string, lastOTPAt int64) (*authModels.TokenResponse, error) {
	if err := ValidateTokenEligibleUser(user); err != nil || in.SessionID == "" {
		return nil, ErrInvalidCredentials
	}
	// Shared chokepoint for every interactive issuance path — Login's
	// completeLogin, IssueLoginTokens / IssueLoginTokensExternal (the
	// exported iface.LoginTokenIssuer seam), and the MFA login-verify
	// handler all funnel here. Login already screens out service
	// principals earlier, so this exists to close the direct-caller gap:
	// no consumer reaching token issuance without going through the
	// guarded Login may mint an interactive token pair for a machine
	// principal. Same sentinel the login guard uses.
	if user.Kind == iface.UserKindService {
		return nil, ErrInvalidCredentials
	}
	deviceID := in.DeviceID
	if deviceID == "" {
		deviceID = "password-" + strings.ReplaceAll(uuid.NewString(), "-", "")[:12]
	}
	platform := in.Platform
	if platform == "" {
		platform = "web"
	}
	deviceType := in.DeviceType
	if deviceType == "" {
		deviceType = "desktop"
	}
	loginMethod := in.LoginMethod
	if loginMethod == "" {
		loginMethod = loginMethodFromAMR(amr)
	}
	var assessment *authModels.RiskAssessment
	if s.riskAssessment != nil && in.RiskScore == 0 && len(in.RiskFactors) == 0 {
		a, assessErr := s.riskAssessment.AssessLoginRisk(ctx, user.UUID, &authModels.SecurityContext{
			IPAddress: in.IPAddress, Fingerprint: in.Fingerprint, Timestamp: time.Now(),
		})
		if assessErr != nil {
			if s.logger != nil {
				s.logger.Warn("risk: assess login risk failed, using default score",
					slog.String("user_uuid", user.UUID), slog.String("error", assessErr.Error()))
			}
		} else if a != nil {
			assessment = a
			in.RiskScore = a.Score
			in.RiskFactors = make([]string, 0, len(a.Factors))
			for _, factor := range a.Factors {
				in.RiskFactors = append(in.RiskFactors, factor.Type)
			}
		}
	}
	riskScore := in.RiskScore
	if riskScore == 0 {
		riskScore = 0.1
	}
	trustLevel := in.TrustLevel
	if trustLevel == "" {
		trustLevel = "medium"
		if riskScore >= 0.5 {
			trustLevel = "untrusted"
		}
	}

	now := time.Now()
	device := &authModels.DeviceInfo{
		DeviceID:    deviceID,
		DeviceType:  deviceType,
		Platform:    platform,
		Fingerprint: in.Fingerprint,
		UserAgent:   in.UserAgent,
	}
	security := &authModels.SecurityContext{
		SessionID:   in.SessionID,
		IPAddress:   in.IPAddress,
		RiskScore:   riskScore,
		RiskFactors: append([]string(nil), in.RiskFactors...),
		Fingerprint: in.Fingerprint,
		Timestamp:   now,
	}
	pair, err := s.jwtService.GenerateTokenPairWithAMR(user, device, security, amr, lastOTPAt)
	if err != nil {
		return nil, err
	}

	// Fresh login → fresh family. The MFA login-verify path also flows
	// through here (via IssueLoginTokens) so post-MFA token pairs get their
	// own family too — correct, because the prior partial login didn't
	// issue any refresh token.
	familyID := uuid.New().String()

	// Store the refresh token for rotation before returning it to the caller.
	if s.refreshTokenRepo == nil {
		return nil, stderrors.New("refresh token persistence is unavailable")
	}
	if err := s.refreshTokenRepo.CreateRefreshToken(ctx, &authModels.RefreshTokenDoc{
		UUID:         authModels.GenerateUUIDv7(),
		UserUUID:     user.UUID,
		Token:        pair.RefreshToken,
		SessionUUID:  in.SessionID,
		DeviceID:     deviceID,
		DeviceType:   deviceType,
		Platform:     platform,
		Fingerprint:  in.Fingerprint,
		IPAddress:    in.IPAddress,
		RiskScore:    riskScore,
		RiskFactors:  append([]string(nil), in.RiskFactors...),
		IssuedAt:     now,
		ExpiresAt:    now.Add(s.jwtService.RefreshTokenTTL()),
		LastActivity: now,
		IsRevoked:    false,
		CreatedAt:    now,
		UpdatedAt:    now,
		FamilyID:     familyID,
	}); err != nil {
		return nil, fmt.Errorf("persist refresh token: %w", err)
	}

	// Create the auth-session row before returning the pair. If this fails,
	// revoke the just-created refresh row by its canonical session id so no
	// usable credential survives without its session record.
	in.DeviceID = deviceID
	in.DeviceType = deviceType
	in.Platform = platform
	in.LoginMethod = loginMethod
	in.RiskScore = riskScore
	in.TrustLevel = trustLevel
	if err := s.createSessionDoc(ctx, user, in, assessment); err != nil {
		if revokeErr := s.refreshTokenRepo.RevokeTokensBySession(ctx, in.SessionID, authModels.RevokeReasonManualRevoke); revokeErr != nil && s.logger != nil {
			s.logger.Error("auth: refresh-token rollback after session persistence failure failed")
		}
		return nil, fmt.Errorf("persist auth session: %w", err)
	}

	// Update the last login timestamp.
	_ = s.userService.UpdateUserLastLogin(ctx, user.UUID)

	return &authModels.TokenResponse{
		AccessToken:  pair.AccessToken,
		RefreshToken: pair.RefreshToken,
		TokenType:    "Bearer",
		ExpiresIn:    int64(s.jwtService.AccessTokenTTL(ctx).Seconds()),
		SessionID:    in.SessionID,
		DeviceID:     deviceID,
		User:         s.buildUserResponse(ctx, user),
	}, nil
}

func (s *PasswordAuthService) createSessionDoc(ctx context.Context, user *iface.User, in LoginTokenContext, assessment *authModels.RiskAssessment) error {
	if s.authSessionRepo == nil {
		return stderrors.New("auth session persistence is unavailable")
	}
	now := time.Now()
	// Detect a never-before-seen (userUUID, deviceID) pair before
	// CreateSession so the just-inserted row doesn't count as its own
	// "prior history". A nil error + zero-length list means new — any
	// repo error degrades to "treat as known" so a flaky lookup never
	// spams the user with a false-positive new-device email.
	newDevice := false
	if in.DeviceID != "" {
		history, err := s.authSessionRepo.GetDeviceSessionHistory(ctx, user.UUID, in.DeviceID, 1)
		if err == nil && len(history) == 0 {
			newDevice = true
		}
	}
	// Risk was computed before JWT/refresh issuance so all three artifacts
	// carry the same value. Score/trust fall back to 0.1 / "medium" when
	// the scorer isn't wired.
	riskScore := in.RiskScore
	trustLevel := in.TrustLevel
	doc := &authModels.AuthSessionDoc{
		UUID:         in.SessionID,
		UserUUID:     user.UUID,
		DeviceID:     in.DeviceID,
		IsActive:     true,
		StartedAt:    now,
		LastActivity: now,
		ExpiresAt:    now.Add(authModels.AuthSessionRetention),
		LoginMethod:  in.LoginMethod,
		MFACompleted: in.MFACompleted,
		DeviceInfo: authModels.DeviceInfo{
			DeviceID:    in.DeviceID,
			DeviceType:  in.DeviceType,
			Platform:    in.Platform,
			Fingerprint: in.Fingerprint,
			UserAgent:   in.UserAgent,
		},
		IPAddress: in.IPAddress,
		RiskScore: riskScore,
		// RiskFactors on SecurityEventLog would be the ideal home, but
		// the session-level trustLevel + score is what downstream
		// middleware (C2 RequireLowRisk) reads today.
		TrustLevel: trustLevel,
		CreatedAt:  now,
		UpdatedAt:  now,
	}
	if err := s.authSessionRepo.CreateSession(ctx, doc); err != nil {
		return err
	}
	// Section C item #5: record the login on the user's security
	// timeline and, when the risk scorer flagged a high-bucket
	// anomaly, email the user. Best-effort — failures here don't
	// undo the just-created session.
	if s.suspiciousLoginNotifier != nil && assessment != nil {
		s.suspiciousLoginNotifier.OnLogin(ctx, SuspiciousLoginInput{
			User: &AuthUser{
				UUID:  user.UUID,
				Email: user.Email,
				Name:  user.FullName,
			},
			Session: &SessionSnapshot{
				UUID:      doc.UUID,
				CreatedAt: doc.CreatedAt,
			},
			Assessment: assessment,
			IPAddress:  in.IPAddress,
			Platform:   in.Platform,
			UserAgent:  in.UserAgent,
		})
	}
	// Phase 7: new-device-login email. Independent of the risk score
	// so even a low-risk login from a brand-new (deviceId, userUUID)
	// pair surfaces. Gated by the notifyUserOnNewDeviceLogin policy
	// toggle. Best-effort — a notification failure leaves the session
	// intact.
	if newDevice {
		s.notifyNewDeviceLogin(ctx, user, doc, in.DeviceID, in.Platform, in.IPAddress, in.UserAgent)
	}
	return nil
}

// inactiveAccountThresholdDays is a small wrapper that returns 0 when
// the policy is unwired, so callers don't have to repeat the nil-check.
func (s *PasswordAuthService) inactiveAccountThresholdDays(ctx context.Context) int {
	if s.policy == nil {
		return 0
	}
	return s.policy.InactiveAccountAutoDisableDays(ctx)
}

// ShouldRevokeOnPasswordChange exposes the live revokeSessionsOnPasswordChange
// policy. Public so the password handler can read it before deciding
// whether to revoke the caller's session id (the service handles the
// service-side half — device-trust grants — itself).
func (s *PasswordAuthService) ShouldRevokeOnPasswordChange(ctx context.Context) bool {
	return s.shouldRevokeOnPasswordChange(ctx)
}

func (s *PasswordAuthService) shouldRevokeOnPasswordChange(ctx context.Context) bool {
	if s.policy == nil {
		return true
	}
	return s.policy.RevokeSessionsOnPasswordChange(ctx)
}

// notifyNewDeviceLogin sends the auth.new_device_login template when
// notifyUserOnNewDeviceLogin is enabled and the notification module is
// wired. Idempotency key includes the session UUID so a retry of the
// same login can't dispatch duplicates.
func (s *PasswordAuthService) notifyNewDeviceLogin(
	ctx context.Context,
	user *iface.User,
	session *authModels.AuthSessionDoc,
	deviceID, platform, ip, userAgent string,
) {
	if !iface.IsConfiguredForCategory(ctx, s.notifier, notifModels.CategoryAuthNewDeviceLogin) {
		return
	}
	if s.policy != nil && !s.policy.NotifyUserOnNewDeviceLogin(ctx) {
		return
	}
	if user == nil || user.Email == "" || session == nil {
		return
	}
	deviceSummary := platform
	if deviceSummary == "" {
		deviceSummary = "Unknown device"
	}
	userName := user.FullName
	if userName == "" {
		userName = user.Email
	}
	loginAt := session.CreatedAt
	if loginAt.IsZero() {
		loginAt = time.Now()
	}
	accountActivityURL := strings.TrimRight(s.frontendURL, "/") + "/user/security?tab=sessions"
	vars := map[string]any{
		"AppName":            s.appName,
		"UserName":           userName,
		"LoginAt":            loginAt.UTC().Format("2006-01-02 15:04 UTC"),
		"LoginIP":            ip,
		"LoginDevice":        deviceSummary,
		"LoginLocation":      "",
		"AccountActivityURL": accountActivityURL,
		"SupportEmail":       s.supportEmail,
	}
	req := iface.TemplatedNotificationRequest{
		Channel:  notifModels.ChannelEmail,
		Type:     notifModels.TypeTransactional,
		Category: notifModels.CategoryAuthNewDeviceLogin,
		Recipients: []iface.Recipient{{
			UserUUID: user.UUID,
			Address:  user.Email,
			Name:     userName,
		}},
		TemplateID:     notifModels.CategoryAuthNewDeviceLogin,
		Data:           vars,
		IdempotencyKey: fmt.Sprintf("new-device:%s:%s:%s", user.UUID, deviceID, session.UUID),
	}
	if _, err := s.notifier.SendTemplated(ctx, req); err != nil && s.logger != nil {
		s.logger.Warn("auth: new-device-login email failed",
			slog.String("user_uuid", user.UUID),
			slog.String("session_uuid", session.UUID),
			slog.String("error", err.Error()))
	}
}

func generateEmailToken() (raw, hash string, err error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", "", err
	}
	raw = base64.RawURLEncoding.EncodeToString(buf)
	sum := sha256.Sum256([]byte(raw))
	hash = hex.EncodeToString(sum[:])
	return raw, hash, nil
}

func hashEmailToken(raw string) string {
	sum := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(sum[:])
}

// ---------------------------------------------------------------------------
// Admin-triggered flows (Phase 3 of the /admin/clients management surface)
//
// These complement the public-facing Resend/Forgot variants. They surface
// real errors instead of the enumeration-proof silent return — the admin
// already knows the user exists, so signalling "not found" or "notifier
// unavailable" is information they're entitled to.
// ---------------------------------------------------------------------------

// AdminSendInvite issues an admin_invite email-token for the given user
// and emails the auth.admin_invite template. Used both by the
// invite-create flow (when the user has no password yet) and as a
// "resend invite" affordance from the user-detail page. Invalidates any
// prior unused invite token for the same user before minting the new
// one. Returns ErrNotificationDown if the notifier is missing.
func (s *PasswordAuthService) AdminSendInvite(ctx context.Context, userUUID, inviterName string) error {
	user, err := s.userService.GetUserByID(ctx, userUUID)
	if err != nil {
		return err
	}
	_ = s.emailTokenRepo.InvalidateByUserAndPurpose(ctx, userUUID, authModels.EmailTokenPurposeAdminInvite)

	raw, hash, err := generateEmailToken()
	if err != nil {
		return err
	}
	doc := &authModels.EmailTokenDoc{
		UUID:      uuid.Must(uuid.NewV7()).String(),
		UserUUID:  userUUID,
		TokenHash: hash,
		Purpose:   authModels.EmailTokenPurposeAdminInvite,
		CreatedAt: time.Now(),
		ExpiresAt: time.Now().Add(7 * 24 * time.Hour),
	}
	if err := s.emailTokenRepo.Create(ctx, doc); err != nil {
		return err
	}

	if !iface.IsConfiguredForCategory(ctx, s.notifier, notifModels.CategoryAuthAdminInvite) {
		return ErrNotificationDown
	}

	inviteURL := s.frontendURL + "/accept-invite?token=" + raw
	_, err = s.notifier.SendTemplated(ctx, iface.TemplatedNotificationRequest{
		Channel:    "email",
		Type:       "transactional",
		Category:   notifModels.CategoryAuthAdminInvite,
		TemplateID: notifModels.CategoryAuthAdminInvite,
		Recipients: []iface.Recipient{{
			UserUUID: userUUID,
			Address:  user.Email,
			Name:     user.FullName,
		}},
		Data: map[string]any{
			"UserName":     coalesce(user.FullName, user.Email),
			"InviteURL":    inviteURL,
			"ExpiresIn":    "7 days",
			"InviterName":  inviterName,
			"AppName":      s.appName,
			"SupportEmail": s.supportEmail,
		},
		IdempotencyKey: "invite:" + userUUID + ":" + doc.UUID,
	})
	return err
}

// AdminResendVerification re-sends a verification email on behalf of an
// admin. Unlike the public ResendVerification it surfaces concrete
// errors and skips rate-limiting (the admin operator is implicitly
// trusted; the operator host is already privileged).
func (s *PasswordAuthService) AdminResendVerification(ctx context.Context, userUUID string) error {
	user, err := s.userService.GetUserByID(ctx, userUUID)
	if err != nil {
		return err
	}
	if user.EmailVerified {
		return nil
	}
	_ = s.emailTokenRepo.InvalidateByUserAndPurpose(ctx, user.UUID, authModels.EmailTokenPurposeVerifyEmail)
	return s.sendVerificationEmail(ctx, user, "")
}

// AdminTriggerPasswordReset emits a reset-password token + email on
// behalf of an admin. Surfaces real errors (404 on missing user,
// 503 ErrNotificationDown). The redemption path is the same
// /v1/auth/{tier}/reset-password the public flow uses.
func (s *PasswordAuthService) AdminTriggerPasswordReset(ctx context.Context, userUUID string) error {
	user, err := s.userService.GetUserByID(ctx, userUUID)
	if err != nil {
		return err
	}
	_ = s.emailTokenRepo.InvalidateByUserAndPurpose(ctx, userUUID, authModels.EmailTokenPurposeResetPassword)

	raw, hash, err := generateEmailToken()
	if err != nil {
		return err
	}
	resetTTL := s.resetTokenTTL(ctx)
	doc := &authModels.EmailTokenDoc{
		UUID:      uuid.Must(uuid.NewV7()).String(),
		UserUUID:  userUUID,
		TokenHash: hash,
		Purpose:   authModels.EmailTokenPurposeResetPassword,
		CreatedAt: time.Now(),
		ExpiresAt: time.Now().Add(resetTTL),
	}
	if err := s.emailTokenRepo.Create(ctx, doc); err != nil {
		return err
	}

	if !iface.IsConfiguredForCategory(ctx, s.notifier, notifModels.CategoryAuthResetPassword) {
		return ErrNotificationDown
	}

	resetURL := s.frontendURL + "/reset-password?token=" + raw
	_, err = s.notifier.SendTemplated(ctx, iface.TemplatedNotificationRequest{
		Channel:    "email",
		Type:       "transactional",
		Category:   notifModels.CategoryAuthResetPassword,
		TemplateID: notifModels.CategoryAuthResetPassword,
		Recipients: []iface.Recipient{{
			UserUUID: userUUID,
			Address:  user.Email,
			Name:     user.FullName,
		}},
		Data: map[string]any{
			"UserName":     coalesce(user.FullName, user.Email),
			"ResetURL":     resetURL,
			"ExpiresIn":    humanDuration(resetTTL),
			"RequestIP":    "(admin-triggered)",
			"AppName":      s.appName,
			"SupportEmail": s.supportEmail,
		},
		IdempotencyKey: "reset:" + userUUID + ":" + doc.UUID,
	})
	return err
}

// resetTokenTTL returns the admin-managed password-reset token lifetime
// when the policy reader is wired, falling back to the legacy hardcoded
// 30-minute value. Centralised so the public ForgotPassword and the
// AdminTriggerPasswordReset paths share the same resolution.
func (s *PasswordAuthService) resetTokenTTL(ctx context.Context) time.Duration {
	if s.policy != nil {
		if d := s.policy.PasswordResetTokenTTL(ctx); d > 0 {
			return d
		}
	}
	return 30 * time.Minute
}

// humanDuration renders a Duration as the human-friendly string the
// reset-password email template uses ("30 minutes", "2 hours", "1 hour
// 15 minutes"). Kept simple; the policy TTL is unlikely to need
// sub-minute precision.
func humanDuration(d time.Duration) string {
	if d <= 0 {
		return "0 minutes"
	}
	hours := int(d / time.Hour)
	mins := int((d % time.Hour) / time.Minute)
	switch {
	case hours == 0:
		return pluralUnit(mins, "minute")
	case mins == 0:
		return pluralUnit(hours, "hour")
	default:
		return pluralUnit(hours, "hour") + " " + pluralUnit(mins, "minute")
	}
}

func pluralUnit(n int, unit string) string {
	if n == 1 {
		return "1 " + unit
	}
	return fmt.Sprintf("%d %ss", n, unit)
}

// ConsumeInvite redeems an admin_invite token: validates the token,
// validates the new password against live policy, hashes it, sets it on
// the target user, and marks the email verified. Used by the
// /v1/auth/client/accept-invite redemption endpoint.
func (s *PasswordAuthService) ConsumeInvite(ctx context.Context, rawToken, newPassword string) error {
	doc, err := s.lookupEmailToken(ctx, rawToken, authModels.EmailTokenPurposeAdminInvite)
	if err != nil {
		return err
	}
	user, err := s.userService.GetUserByID(ctx, doc.UserUUID)
	if err != nil {
		return err
	}
	if err := s.passwordService.ValidatePolicy(ctx, newPassword, user.Email); err != nil {
		return err
	}
	hash, err := s.passwordService.Hash(newPassword)
	if err != nil {
		return err
	}
	if err := s.userService.UpdatePasswordHash(ctx, user.UUID, hash); err != nil {
		return err
	}
	// Admin vouched for the address by typing it — invite redemption
	// implies the recipient controls the inbox, so mark the email
	// verified in the same step.
	if err := s.userService.MarkEmailVerified(ctx, user.UUID); err != nil {
		// Non-fatal: password is set, user can log in. Log + continue
		// so a verify-mark hiccup doesn't strand a freshly-onboarded
		// account at "set password again".
		if s.logger != nil {
			s.logger.Warn("invite consume: mark email verified failed",
				slog.String("user_uuid", user.UUID),
				slog.String("error", err.Error()))
		}
	}
	_ = s.emailTokenRepo.MarkUsed(ctx, doc.TokenHash)
	_ = s.userService.ClearFailedLogins(ctx, user.UUID)
	return nil
}

func coalesce(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}
