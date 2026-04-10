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
	ErrInvalidCredentials = stderrors.New("invalid credentials")
	ErrEmailNotVerified   = stderrors.New("email not verified")
	ErrAccountLocked      = stderrors.New("account temporarily locked")
	ErrUserInactive       = stderrors.New("user account is not active")
	ErrPasswordReused     = stderrors.New("new password must differ from the current one")
	ErrNotificationDown   = stderrors.New("notifications disabled — cannot send email")
)

// PasswordAuthConfig configures the password auth service.
type PasswordAuthConfig struct {
	UserService              iface.UserProvider
	PasswordService          PasswordService
	JWTService               JWTService
	EmailTokenRepo           repository.EmailTokenRepository
	RefreshTokenRepo         repository.RefreshTokenRepository
	AuthSessionRepo          repository.AuthSessionRepository
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
	passwordService          PasswordService
	jwtService               JWTService
	emailTokenRepo           repository.EmailTokenRepository
	refreshTokenRepo         repository.RefreshTokenRepository
	authSessionRepo          repository.AuthSessionRepository
	notifier                 iface.NotificationSender
	rateLimiter              *sharederrors.RateLimiter
	frontendURL              string
	requireEmailVerification bool
	appName                  string
	supportEmail             string
	logger                   *slog.Logger
}

// NewPasswordAuthService builds a new password auth service.
func NewPasswordAuthService(cfg PasswordAuthConfig) *PasswordAuthService {
	return &PasswordAuthService{
		userService:              cfg.UserService,
		passwordService:          cfg.PasswordService,
		jwtService:               cfg.JWTService,
		emailTokenRepo:           cfg.EmailTokenRepo,
		refreshTokenRepo:         cfg.RefreshTokenRepo,
		authSessionRepo:          cfg.AuthSessionRepo,
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

	// First-user heuristic: if no users exist, assign developer role.
	userCount, _ := s.userService.GetUserCount(ctx, nil)
	role := "operator"
	if userCount == 0 {
		role = "developer"
	}

	user, err := s.userService.CreateUserWithPassword(ctx, &userModels.CreateUserInput{
		Email:        email,
		FullName:     in.FullName,
		PasswordHash: hash,
		Role:         role,
	})
	if err != nil {
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
// configured) and explicitly assigns the developer role rather than relying on
// the first-user heuristic in Register. Returns a full TokenResponse so the
// wizard can log the operator straight in.
//
// Callers must gate this by "no users exist yet." The unique index on
// users.email is the only protection against concurrent callers slipping
// through that gate at the same instant.
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

	user, err := s.userService.CreateUserWithPassword(ctx, &userModels.CreateUserInput{
		Email:        email,
		FullName:     fullName,
		PasswordHash: hash,
		Role:         "developer",
	})
	if err != nil {
		return nil, err
	}

	if err := s.userService.MarkEmailVerified(ctx, user.UUID); err != nil {
		s.logger.Warn("RegisterInitialAdmin: MarkEmailVerified failed",
			slog.String("user", user.UUID),
			slog.String("error", err.Error()),
		)
	}
	user.EmailVerified = true

	return s.issueTokens(ctx, user, LoginInput{IP: ip, Platform: "web"})
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
		return nil, ErrInvalidCredentials
	}

	if !user.IsActive {
		s.recordFailed(ctx, in.IP, email)
		return nil, ErrInvalidCredentials
	}
	if user.LockedUntil != nil && time.Now().Before(*user.LockedUntil) {
		return nil, ErrAccountLocked
	}
	if user.PasswordHash == "" {
		// Account exists but was created via OAuth — don't leak that fact.
		_, _ = s.passwordService.Verify(in.Password, s.passwordService.DummyHash())
		s.recordFailed(ctx, in.IP, email)
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
		return nil, ErrInvalidCredentials
	}

	if s.requireEmailVerification && !user.EmailVerified {
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

	return s.issueTokens(ctx, user, in)
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
	return s.emailTokenRepo.MarkUsed(ctx, doc.TokenHash)
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

func (s *PasswordAuthService) issueTokens(ctx context.Context, user *userModels.User, in LoginInput) (*authModels.TokenResponse, error) {
	deviceID := in.DeviceID
	if deviceID == "" {
		deviceID = "password-" + strings.ReplaceAll(uuid.NewString(), "-", "")[:12]
	}
	platform := in.Platform
	if platform == "" {
		platform = "web"
	}

	accessToken, err := s.jwtService.GenerateAccessToken(user)
	if err != nil {
		return nil, err
	}
	refreshToken, err := s.jwtService.GenerateRefreshToken(user)
	if err != nil {
		return nil, err
	}

	sessionID := uuid.New().String()
	now := time.Now()

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
