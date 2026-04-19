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
	sharederrors "github.com/orkestra/backend/internal/shared/errors"
	"github.com/orkestra/backend/internal/shared/iface"
	userModels "github.com/orkestra/backend/internal/core/user/models"
)

var (
	ErrInvalidCredentials     = stderrors.New("invalid credentials")
	ErrEmailNotVerified       = stderrors.New("email not verified")
	ErrAccountLocked          = stderrors.New("account temporarily locked")
	ErrUserInactive           = stderrors.New("user account is not active")
	ErrPasswordReused         = stderrors.New("new password must differ from the current one")
	ErrNotificationDown       = stderrors.New("notifications disabled — cannot send email")
	ErrMFAEnrollmentRequired  = stderrors.New("mfa enrollment required — grace period expired")
)

// OnboardingActivator is invoked after a successful email verification so
// onboarding-adjacent side effects (flipping the owner's tenant from
// provisioning → active) can run without the auth module importing
// onboarding or tenant. Set via SetOnboardingActivator at boot — nil by
// default so flows that do not run the onboarding module (setup wizard,
// admin-created users) are unaffected.
//
// The callback must be best-effort: a failure here is logged but does not
// roll back the email verification itself. The owner has already proven
// they control the inbox; we don't want to force them to reclick because
// of a transient tenant-side failure.
type OnboardingActivator func(ctx context.Context, userUUID string) error

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
	MFAFactorRepo            repository.MFAFactorRepository  // required: decides partial vs full response
	MFAChallengeService      MFAChallengeService             // required: mints login-continuation challenges
	FirstAdminClaimer        FirstAdminClaimer               // required: atomic first-admin claim
	Notifier                 iface.NotificationSender
	RateLimiter              *sharederrors.RateLimiter
	FrontendURL              string
	RequireEmailVerification bool
	AppName                  string
	SupportEmail             string
	Logger                   *slog.Logger
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
	notifier                 iface.NotificationSender
	rateLimiter              *sharederrors.RateLimiter
	frontendURL              string
	requireEmailVerification bool
	appName                  string
	supportEmail             string
	logger                   *slog.Logger
	// onboardingActivator is wired post-construction via SetOnboardingActivator
	// because the onboarding module initializes after auth. Accessed only
	// from VerifyEmail — no locking needed as the setter runs once at boot
	// before the first HTTP request is served.
	onboardingActivator OnboardingActivator
	// auditSink is wired post-construction via SetAuditSink by the compliance
	// module. Nil when compliance is disabled — emit* helpers tolerate that.
	auditSink iface.AuditSink
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
		notifier:                 cfg.Notifier,
		rateLimiter:              cfg.RateLimiter,
		frontendURL:              cfg.FrontendURL,
		requireEmailVerification: cfg.RequireEmailVerification,
		appName:                  cfg.AppName,
		supportEmail:             cfg.SupportEmail,
		logger:                   cfg.Logger,
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
func (s *PasswordAuthService) Register(ctx context.Context, in RegisterInput) (*userModels.User, error) {
	email := strings.ToLower(strings.TrimSpace(in.Email))
	if email == "" || in.Password == "" || in.FullName == "" {
		return nil, fmt.Errorf("email, password and name are required")
	}

	// Reject signups up-front if verification is required but the
	// notification sender isn't configured.
	if s.requireEmailVerification && (s.notifier == nil || !s.notifier.IsConfigured(ctx)) {
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
	// super_admin; losers fall through to operator. The userUUID is minted
	// up front so we can hand it to both the claimer and the user-create
	// call.
	proposedUUID := uuid.New().String()
	role := "operator"
	claimed := false
	if s.firstAdminClaimer != nil {
		claimed, err = s.firstAdminClaimer.ClaimFirstAdmin(ctx, proposedUUID)
		if err != nil {
			return nil, fmt.Errorf("claim first admin: %w", err)
		}
		if claimed {
			role = "super_admin"
		}
	}

	user, err := s.userService.CreateUserWithPassword(ctx, &userModels.CreateUserInput{
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

	user, err := s.userService.CreateUserWithPassword(ctx, &userModels.CreateUserInput{
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

	return s.issueTokens(ctx, user, LoginInput{IP: ip, Platform: "web"}, []string{"pwd"}, 0)
}

// LoginInput is the payload for email/password login.
type LoginInput struct {
	Email    string
	Password string
	IP       string
	DeviceID string
	Platform string
}

// Login authenticates a user by email/password and returns a token pair.
// On any failure it returns ErrInvalidCredentials to avoid user enumeration.
// Rate limiting is applied against both the IP and the email address.
func (s *PasswordAuthService) Login(ctx context.Context, in LoginInput) (*authModels.TokenResponse, error) {
	email := strings.ToLower(strings.TrimSpace(in.Email))
	if email == "" || in.Password == "" {
		return nil, ErrInvalidCredentials
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

	if !user.IsActive {
		s.recordFailed(ctx, in.IP, email)
		s.emitLoginFailed(ctx, email, user.UUID, in.IP, "user_inactive")
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
		var lockUntil *time.Time
		if user.FailedLoginCount+1 >= 5 {
			t := time.Now().Add(15 * time.Minute)
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
func (s *PasswordAuthService) completeLogin(ctx context.Context, user *userModels.User, in LoginInput, sourceAMR []string) (*authModels.TokenResponse, error) {
	memberships := s.loadMembershipsAsAuthModel(ctx, user.UUID)
	requires := RoleRequiresMFA(user, memberships)
	if !requires {
		return s.issueTokens(ctx, user, in, sourceAMR, 0)
	}

	// Privileged user: check enrollment.
	if s.mfaFactorRepo != nil {
		factor, err := s.mfaFactorRepo.FindByUserAndType(ctx, user.UUID, authModels.MFAFactorTOTP)
		if err == nil && factor != nil {
			// Factor enrolled — partial response; client must complete MFA.
			if s.mfaChallengeService == nil {
				return nil, fmt.Errorf("mfa challenge service not wired")
			}
			ch, err := s.mfaChallengeService.BeginLogin(ctx, LoginChallengeInput{
				UserUUID:  user.UUID,
				SourceAMR: sourceAMR,
				DeviceID:  in.DeviceID,
				Platform:  in.Platform,
				IPAddress: in.IP,
			})
			if err != nil {
				return nil, err
			}
			return &authModels.TokenResponse{
				RequiresMFA: true,
				MFAToken:    ch.ID,
				User:        user.ToResponse(),
			}, nil
		}
		// FindByUserAndType returned ErrMFAFactorNotFound or another error.
		// Treat not-found as "no factor" and fall through to grace logic;
		// any other error is surfaced.
		if err != nil && !stderrors.Is(err, repository.ErrMFAFactorNotFound) {
			return nil, err
		}
	}

	// Privileged, no factor → grace logic.
	now := time.Now()
	if GraceExpired(user, now) {
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
	if user.MFAGraceStartedAt != nil {
		deadline := user.MFAGraceStartedAt.Add(MFAEnrollmentGraceWindow)
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

// SetOnboardingActivator wires the activate-on-verify hook. Called once
// from the onboarding module's Init, after both auth and tenant are up.
// Passing nil clears the hook — used by tests that want the bare flow.
func (s *PasswordAuthService) SetOnboardingActivator(fn OnboardingActivator) {
	s.onboardingActivator = fn
}

// SetAuditSink wires the compliance audit sink post-construction. The
// compliance module is optional, so auth's audit-emission helpers skip
// silently when sink is nil.
func (s *PasswordAuthService) SetAuditSink(sink iface.AuditSink) {
	s.auditSink = sink
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
	// Best-effort onboarding activation. A transient tenant-side failure
	// here leaves the tenant in `provisioning`; the operator can flip it
	// manually via /admin/tenants. Do not surface the error to the caller —
	// the email has been verified, which is the user-visible contract.
	if s.onboardingActivator != nil {
		if err := s.onboardingActivator(ctx, doc.UserUUID); err != nil {
			s.logger.Warn("verify-email: onboarding activator failed",
				slog.String("userUUID", doc.UserUUID),
				slog.String("error", err.Error()),
			)
		}
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
func (s *PasswordAuthService) ResendVerification(ctx context.Context, email, ip string) error {
	email = strings.ToLower(strings.TrimSpace(email))
	user, err := s.userService.GetUserForAuth(ctx, email)
	if err != nil {
		// Always return success to avoid enumeration.
		return nil
	}
	if user.EmailVerified {
		return nil
	}
	_ = s.emailTokenRepo.InvalidateByUserAndPurpose(ctx, user.UUID, authModels.EmailTokenPurposeVerifyEmail)
	return s.sendVerificationEmail(ctx, user, ip)
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
	doc := &authModels.EmailTokenDoc{
		UUID:      uuid.Must(uuid.NewV7()).String(),
		UserUUID:  user.UUID,
		TokenHash: hash,
		Purpose:   authModels.EmailTokenPurposeResetPassword,
		IP:        ip,
		CreatedAt: time.Now(),
		ExpiresAt: time.Now().Add(30 * time.Minute),
	}
	if err := s.emailTokenRepo.Create(ctx, doc); err != nil {
		return nil
	}

	if s.notifier == nil || !s.notifier.IsConfigured(ctx) {
		s.logger.Warn("forgot password: notifier not configured, cannot send email")
		return nil
	}

	resetURL := s.frontendURL + "/reset-password?token=" + raw
	_, err = s.notifier.SendTemplated(ctx, iface.TemplatedNotificationRequest{
		Channel:    "email",
		Type:       "transactional",
		Category:   authModels.EmailTokenPurposeResetPassword,
		TemplateID: "auth.reset_password",
		Recipients: []iface.Recipient{{
			UserUUID: user.UUID,
			Address:  user.Email,
			Name:     user.FullName,
		}},
		Data: map[string]any{
			"UserName":     coalesce(user.FullName, user.Email),
			"ResetURL":     resetURL,
			"ExpiresIn":    "30 minutes",
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

	// Revoke all refresh tokens — the user re-authenticates everywhere.
	if s.refreshTokenRepo != nil {
		_ = s.refreshTokenRepo.RevokeTokensByUser(ctx, user.UUID, "password_reset")
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

// ChangePassword updates the password for an authenticated user who
// supplied the current password.
func (s *PasswordAuthService) ChangePassword(ctx context.Context, userUUID, current, next string) error {
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

// --- internal helpers ---

func (s *PasswordAuthService) recordFailed(ctx context.Context, ip, email string) {
	if s.rateLimiter == nil {
		return
	}
	s.rateLimiter.RecordFailedAuth(ctx, "ip:"+ip)
	s.rateLimiter.RecordFailedAuth(ctx, "email:"+email)
}

func (s *PasswordAuthService) sendVerificationEmail(ctx context.Context, user *userModels.User, ip string) error {
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

	if s.notifier == nil || !s.notifier.IsConfigured(ctx) {
		return ErrNotificationDown
	}

	verifyURL := s.frontendURL + "/verify-email?token=" + raw
	_, err = s.notifier.SendTemplated(ctx, iface.TemplatedNotificationRequest{
		Channel:    "email",
		Type:       "transactional",
		Category:   authModels.EmailTokenPurposeVerifyEmail,
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
// step has happened on this request.
func (s *PasswordAuthService) IssueLoginTokens(ctx context.Context, user *userModels.User, deviceID, platform, ip string, amr []string, lastOTPAt int64) (*authModels.TokenResponse, error) {
	return s.issueTokens(ctx, user, LoginInput{DeviceID: deviceID, Platform: platform, IP: ip}, amr, lastOTPAt)
}

func (s *PasswordAuthService) issueTokens(ctx context.Context, user *userModels.User, in LoginInput, amr []string, lastOTPAt int64) (*authModels.TokenResponse, error) {
	deviceID := in.DeviceID
	if deviceID == "" {
		deviceID = "password-" + strings.ReplaceAll(uuid.NewString(), "-", "")[:12]
	}
	platform := in.Platform
	if platform == "" {
		platform = "web"
	}

	accessToken, err := s.jwtService.GenerateAccessTokenWithAMR(user, amr, lastOTPAt)
	if err != nil {
		return nil, err
	}
	refreshToken, err := s.jwtService.GenerateRefreshToken(user)
	if err != nil {
		return nil, err
	}

	sessionID := uuid.New().String()
	now := time.Now()

	// Fresh login → fresh family. The MFA login-verify path also flows
	// through here (via IssueLoginTokens) so post-MFA token pairs get their
	// own family too — correct, because the prior partial login didn't
	// issue any refresh token.
	familyID := uuid.New().String()

	// Store the refresh token for rotation.
	_ = s.refreshTokenRepo.CreateRefreshToken(ctx, &authModels.RefreshTokenDoc{
		UUID:         authModels.GenerateUUIDv7(),
		UserUUID:     user.UUID,
		Token:        refreshToken,
		SessionUUID:  sessionID,
		DeviceID:     deviceID,
		DeviceType:   "web",
		Platform:     platform,
		Fingerprint:  "password-login",
		IPAddress:    in.IP,
		RiskScore:    0.1,
		IssuedAt:     now,
		ExpiresAt:    now.Add(7 * 24 * time.Hour),
		LastActivity: now,
		IsRevoked:    false,
		CreatedAt:    now,
		UpdatedAt:    now,
		FamilyID:     familyID,
	})

	// Create an auth session doc for audit trail.
	_ = s.createSessionDoc(ctx, user, sessionID, deviceID, platform, in.IP)

	// Update the last login timestamp.
	_ = s.userService.UpdateUserLastLogin(ctx, user.UUID)

	return &authModels.TokenResponse{
		AccessToken:  accessToken,
		RefreshToken: refreshToken,
		TokenType:    "Bearer",
		ExpiresIn:    900,
		SessionID:    sessionID,
		DeviceID:     deviceID,
		User:         user.ToResponse(),
	}, nil
}

func (s *PasswordAuthService) createSessionDoc(ctx context.Context, user *userModels.User, sessionID, deviceID, platform, ip string) error {
	if s.authSessionRepo == nil {
		return nil
	}
	now := time.Now()
	doc := &authModels.AuthSessionDoc{
		UUID:         sessionID,
		UserUUID:     user.UUID,
		DeviceID:     deviceID,
		IsActive:     true,
		StartedAt:    now,
		LastActivity: now,
		ExpiresAt:    now.Add(90 * 24 * time.Hour),
		LoginMethod:  "password",
		MFACompleted: false,
		DeviceInfo: authModels.DeviceInfo{
			DeviceID: deviceID,
			Platform: platform,
		},
		IPAddress:  ip,
		RiskScore:  0.1,
		TrustLevel: "medium",
		CreatedAt:  now,
		UpdatedAt:  now,
	}
	return s.authSessionRepo.CreateSession(ctx, doc)
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

func coalesce(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}
