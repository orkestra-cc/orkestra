package handlers

import (
	"context"
	"errors"
	"log/slog"
	"net/http"

	"github.com/danielgtaylor/huma/v2"
	"github.com/orkestra/backend/internal/core/auth/services"
	"github.com/orkestra/backend/internal/shared/utils"
)

// PasswordAuthHandler wraps the PasswordAuthService with HTTP bindings.
type PasswordAuthHandler struct {
	svc               *services.PasswordAuthService
	cookieName        string
	cookieDomain      string
	cookieSecure      bool
	sessionRevocation services.SessionRevocationService
}

func NewPasswordAuthHandler(svc *services.PasswordAuthService, cookieName, cookieDomain string, cookieSecure bool) *PasswordAuthHandler {
	if cookieName == "" {
		cookieName = "access_token"
	}
	return &PasswordAuthHandler{
		svc:          svc,
		cookieName:   cookieName,
		cookieDomain: cookieDomain,
		cookieSecure: cookieSecure,
	}
}

// SetSessionRevocation wires the revoked-session store so change-password
// can invalidate the current session's access token in addition to the
// refresh-token wipe the service already performs.
func (h *PasswordAuthHandler) SetSessionRevocation(s services.SessionRevocationService) {
	h.sessionRevocation = s
}

// --- Register ---

type RegisterRequest struct {
	Body struct {
		Email    string `json:"email" doc:"Email address" format:"email"`
		Password string `json:"password" doc:"New password (min 10 chars)"`
		FullName string `json:"fullName" doc:"Full name"`
	}
}

type RegisterResponse struct {
	Body struct {
		Success             bool   `json:"success"`
		UserUUID            string `json:"userUuid"`
		Message             string `json:"message"`
		RequiresVerification bool  `json:"requiresVerification"`
	}
}

func (h *PasswordAuthHandler) Register(ctx context.Context, req *RegisterRequest) (*RegisterResponse, error) {
	ip := clientIPFromCtx(ctx)
	user, err := h.svc.Register(ctx, services.RegisterInput{
		Email:    req.Body.Email,
		Password: req.Body.Password,
		FullName: req.Body.FullName,
		IP:       ip,
	})
	if err != nil {
		return nil, mapPasswordError(err)
	}
	resp := &RegisterResponse{}
	resp.Body.Success = true
	resp.Body.UserUUID = user.UUID
	resp.Body.RequiresVerification = !user.EmailVerified
	if user.EmailVerified {
		resp.Body.Message = "Account created. You can now sign in."
	} else {
		resp.Body.Message = "Account created. Please check your email to verify your address."
	}
	return resp, nil
}

// --- Login ---

type PasswordLoginRequest struct {
	Body struct {
		Email    string `json:"email" doc:"Email address" format:"email"`
		Password string `json:"password" doc:"Password"`
	}
}

type PasswordLoginResponse struct {
	SetCookie string `header:"Set-Cookie"`
	Body      struct {
		Success     bool        `json:"success"`
		AccessToken string      `json:"accessToken,omitempty"`
		TokenType   string      `json:"tokenType,omitempty"`
		ExpiresIn   int64       `json:"expiresIn,omitempty"`
		User        interface{} `json:"user,omitempty"`
		// MFA fields: when RequiresMFA is true, AccessToken is empty and the
		// client must call POST /v1/auth/mfa/login/verify (TOTP/backup code)
		// or POST /v1/auth/mfa/webauthn/login/{begin,finish} (passkey) with
		// MFAToken. WebAuthnAvailable lets the UI offer the passkey button.
		RequiresMFA           bool        `json:"requiresMfa,omitempty"`
		MFAToken              string      `json:"mfaToken,omitempty"`
		WebAuthnAvailable     bool        `json:"webauthnAvailable,omitempty"`
		MFAEnrollmentRequired bool        `json:"mfaEnrollmentRequired,omitempty"`
		MFAGraceExpiresAt     interface{} `json:"mfaGraceExpiresAt,omitempty"`
	}
}

func (h *PasswordAuthHandler) Login(ctx context.Context, req *PasswordLoginRequest) (*PasswordLoginResponse, error) {
	ip := clientIPFromCtx(ctx)
	tokens, err := h.svc.Login(ctx, services.LoginInput{
		Email:    req.Body.Email,
		Password: req.Body.Password,
		IP:       ip,
		Platform: "web",
	})
	if err != nil {
		return nil, mapPasswordError(err)
	}

	resp := &PasswordLoginResponse{}
	resp.Body.Success = true

	// Partial response — caller must complete MFA before receiving tokens.
	if tokens.RequiresMFA {
		resp.Body.RequiresMFA = true
		resp.Body.MFAToken = tokens.MFAToken
		resp.Body.WebAuthnAvailable = tokens.WebAuthnAvailable
		resp.Body.User = tokens.User
		return resp, nil
	}

	resp.SetCookie = buildRefreshCookie(h.cookieName, tokens.RefreshToken, h.cookieDomain, h.cookieSecure)
	resp.Body.AccessToken = tokens.AccessToken
	resp.Body.TokenType = tokens.TokenType
	resp.Body.ExpiresIn = tokens.ExpiresIn
	resp.Body.User = tokens.User
	if tokens.MFAEnrollmentRequired {
		resp.Body.MFAEnrollmentRequired = true
		resp.Body.MFAGraceExpiresAt = tokens.MFAGraceExpiresAt
	}
	return resp, nil
}

// --- Verify email ---

type VerifyEmailRequest struct {
	Body struct {
		Token string `json:"token" doc:"Verification token from email"`
	}
}

type VerifyEmailResponse struct {
	Body struct {
		Success bool   `json:"success"`
		Message string `json:"message"`
	}
}

func (h *PasswordAuthHandler) VerifyEmail(ctx context.Context, req *VerifyEmailRequest) (*VerifyEmailResponse, error) {
	if err := h.svc.VerifyEmail(ctx, req.Body.Token); err != nil {
		return nil, huma.Error400BadRequest("Invalid or expired verification token")
	}
	resp := &VerifyEmailResponse{}
	resp.Body.Success = true
	resp.Body.Message = "Email verified. You can now sign in."
	return resp, nil
}

type ResendVerificationRequest struct {
	Body struct {
		Email string `json:"email" doc:"Email address" format:"email"`
	}
}

func (h *PasswordAuthHandler) ResendVerification(ctx context.Context, req *ResendVerificationRequest) (*VerifyEmailResponse, error) {
	ip := clientIPFromCtx(ctx)
	_ = h.svc.ResendVerification(ctx, req.Body.Email, ip)
	resp := &VerifyEmailResponse{}
	resp.Body.Success = true
	resp.Body.Message = "If an account with that email exists and is unverified, a new verification email has been sent."
	return resp, nil
}

// --- Forgot / reset password ---

type ForgotPasswordRequest struct {
	Body struct {
		Email string `json:"email" doc:"Email address" format:"email"`
	}
}

type ForgotPasswordResponse struct {
	Body struct {
		Success bool   `json:"success"`
		Message string `json:"message"`
	}
}

func (h *PasswordAuthHandler) ForgotPassword(ctx context.Context, req *ForgotPasswordRequest) (*ForgotPasswordResponse, error) {
	ip := clientIPFromCtx(ctx)
	_ = h.svc.ForgotPassword(ctx, req.Body.Email, ip)
	resp := &ForgotPasswordResponse{}
	resp.Body.Success = true
	resp.Body.Message = "If an account with that email exists, a password reset email has been sent."
	return resp, nil
}

type ResetPasswordRequest struct {
	Body struct {
		Token       string `json:"token" doc:"Reset token from email"`
		NewPassword string `json:"newPassword" doc:"New password"`
	}
}

func (h *PasswordAuthHandler) ResetPassword(ctx context.Context, req *ResetPasswordRequest) (*ForgotPasswordResponse, error) {
	if err := h.svc.ResetPassword(ctx, req.Body.Token, req.Body.NewPassword); err != nil {
		return nil, mapPasswordError(err)
	}
	resp := &ForgotPasswordResponse{}
	resp.Body.Success = true
	resp.Body.Message = "Password updated. You can now sign in with your new password."
	return resp, nil
}

// --- Change password (authenticated) ---

type ChangePasswordRequest struct {
	Body struct {
		CurrentPassword string `json:"currentPassword" doc:"Current password"`
		NewPassword     string `json:"newPassword" doc:"New password"`
	}
}

func (h *PasswordAuthHandler) ChangePassword(ctx context.Context, req *ChangePasswordRequest) (*ForgotPasswordResponse, error) {
	userUUID, _ := ctx.Value("userUUID").(string)
	if userUUID == "" {
		return nil, huma.Error401Unauthorized("authentication required")
	}
	if err := h.svc.ChangePassword(ctx, userUUID, req.Body.CurrentPassword, req.Body.NewPassword); err != nil {
		return nil, mapPasswordError(err)
	}
	// Phase 8: revoke the caller's session id so the old bearer stops
	// working immediately instead of staying valid until its 15-minute
	// TTL. Gated on the live revokeSessionsOnPasswordChange policy so
	// admins can opt out (e.g. for migration workflows where preserving
	// existing sessions matters more than the immediate cutover).
	// Default-on preserves the historical behaviour.
	if h.sessionRevocation != nil && h.svc.ShouldRevokeOnPasswordChange(ctx) {
		if sid := currentSessionID(ctx); sid != "" {
			_ = h.sessionRevocation.Revoke(ctx, sid, "password_change")
		}
	}
	resp := &ForgotPasswordResponse{}
	resp.Body.Success = true
	resp.Body.Message = "Password updated."
	return resp, nil
}

// --- helpers ---

func clientIPFromCtx(ctx context.Context) string {
	if r, ok := ctx.Value("http_request").(*http.Request); ok && r != nil {
		return utils.GetClientIP(r)
	}
	return ""
}

// buildRefreshCookie assembles a Set-Cookie header value for the refresh
// token cookie with secure defaults.
func buildRefreshCookie(name, value, domain string, secure bool) string {
	maxAge := 7 * 24 * 3600
	c := &http.Cookie{
		Name:     name,
		Value:    value,
		Path:     "/",
		Domain:   domain,
		MaxAge:   maxAge,
		HttpOnly: true,
		Secure:   secure,
		SameSite: http.SameSiteLaxMode,
	}
	return c.String()
}

// codedError is a huma.StatusError that adds a stable machine-readable
// `code` field on top of the standard problem+json shape, so SPAs can
// discriminate specific error cases without parsing the localized
// `detail` text. Mirrors the convention used by the step_up_required
// 401 envelope in shared/middleware/auth.go.
type codedError struct {
	Status int    `json:"status"`
	Title  string `json:"title,omitempty"`
	Detail string `json:"detail"`
	Code   string `json:"code,omitempty"`
}

func (e *codedError) Error() string  { return e.Detail }
func (e *codedError) GetStatus() int { return e.Status }

func mapPasswordError(err error) error {
	switch {
	case errors.Is(err, services.ErrInvalidCredentials):
		return huma.Error401Unauthorized("Invalid email or password")
	case errors.Is(err, services.ErrEmailNotVerified):
		return &codedError{
			Status: http.StatusForbidden,
			Title:  "Forbidden",
			Detail: "Email address not verified. Please check your inbox for the verification email.",
			Code:   "email_not_verified",
		}
	case errors.Is(err, services.ErrAccountLocked):
		return huma.Error429TooManyRequests("Too many failed attempts. Please try again later.")
	case errors.Is(err, services.ErrUserInactive):
		return huma.Error403Forbidden("Account is not active")
	case errors.Is(err, services.ErrPasswordReused):
		return huma.Error400BadRequest("New password must differ from the current one")
	case errors.Is(err, services.ErrNotificationDown):
		return huma.Error503ServiceUnavailable("Email delivery is not configured — signups are temporarily unavailable. Please contact an administrator.")
	case errors.Is(err, services.ErrMFAEnrollmentRequired):
		return huma.Error403Forbidden("MFA enrollment required — the grace period for this account has expired. Please complete MFA setup via an admin before signing in.")
	case errors.Is(err, services.ErrRegistrationDisabled):
		return &codedError{
			Status: http.StatusForbidden,
			Title:  "Forbidden",
			Detail: "Self-service registration is disabled for this surface. Contact an administrator.",
			Code:   "registration_disabled",
		}
	case errors.Is(err, services.ErrEmailDomainNotAllowed):
		return &codedError{
			Status: http.StatusForbidden,
			Title:  "Forbidden",
			Detail: "Registrations from this email domain are not allowed.",
			Code:   "email_domain_not_allowed",
		}
	case errors.Is(err, services.ErrLoginDisabled):
		return &codedError{
			Status: http.StatusForbidden,
			Title:  "Forbidden",
			Detail: "Login is temporarily disabled for this surface. Contact an administrator.",
			Code:   "login_disabled",
		}
	case errors.Is(err, services.ErrCountryBlocked):
		return &codedError{
			Status: http.StatusForbidden,
			Title:  "Forbidden",
			Detail: "Access from your country is not permitted.",
			Code:   "country_blocked",
		}
	case errors.Is(err, services.ErrPasswordTooShort),
		errors.Is(err, services.ErrPasswordTooLong),
		errors.Is(err, services.ErrPasswordContainsEmail),
		errors.Is(err, services.ErrPasswordBreached),
		errors.Is(err, services.ErrPasswordMissingUpper),
		errors.Is(err, services.ErrPasswordMissingLower),
		errors.Is(err, services.ErrPasswordMissingDigit),
		errors.Is(err, services.ErrPasswordMissingSymbol):
		return huma.Error400BadRequest(err.Error())
	default:
		slog.Default().Warn("password auth error", slog.String("error", err.Error()))
		return huma.Error400BadRequest("Request failed")
	}
}

// RegisterPublicRoutes registers the endpoints that don't require auth.
// mount controls the path + operation-ID prefix; LegacyMount preserves
// the pre-PR-D /v1/auth/... layout while OperatorMount/ClientMount mount
// the same handlers under /v1/auth/operator/... and /v1/auth/client/...
// for the ADR-0003 audience-split surfaces.
func (h *PasswordAuthHandler) RegisterPublicRoutes(api huma.API, mount RouteMount) {
	huma.Register(api, huma.Operation{
		OperationID: mount.OpIDPrefix + "password-register",
		Method:      http.MethodPost,
		Path:        "/v1/auth" + mount.PathPrefix + "/register",
		Summary:     "Register a new account with email and password",
		Tags:        []string{"Authentication"},
	}, h.Register)

	huma.Register(api, huma.Operation{
		OperationID: mount.OpIDPrefix + "password-login",
		Method:      http.MethodPost,
		Path:        "/v1/auth" + mount.PathPrefix + "/login",
		Summary:     "Log in with email and password",
		Tags:        []string{"Authentication"},
	}, h.Login)

	huma.Register(api, huma.Operation{
		OperationID: mount.OpIDPrefix + "password-verify-email",
		Method:      http.MethodPost,
		Path:        "/v1/auth" + mount.PathPrefix + "/verify-email",
		Summary:     "Verify an email address with a token",
		Tags:        []string{"Authentication"},
	}, h.VerifyEmail)

	huma.Register(api, huma.Operation{
		OperationID: mount.OpIDPrefix + "password-resend-verification",
		Method:      http.MethodPost,
		Path:        "/v1/auth" + mount.PathPrefix + "/verify-email/resend",
		Summary:     "Resend email verification",
		Tags:        []string{"Authentication"},
	}, h.ResendVerification)

	huma.Register(api, huma.Operation{
		OperationID: mount.OpIDPrefix + "password-forgot",
		Method:      http.MethodPost,
		Path:        "/v1/auth" + mount.PathPrefix + "/forgot-password",
		Summary:     "Request a password reset email",
		Tags:        []string{"Authentication"},
	}, h.ForgotPassword)

	huma.Register(api, huma.Operation{
		OperationID: mount.OpIDPrefix + "password-reset",
		Method:      http.MethodPost,
		Path:        "/v1/auth" + mount.PathPrefix + "/reset-password",
		Summary:     "Reset a password with a token",
		Tags:        []string{"Authentication"},
	}, h.ResetPassword)
}

// RegisterProtectedRoutes registers the change-password endpoint. See
// RegisterPublicRoutes for the mount semantics.
func (h *PasswordAuthHandler) RegisterProtectedRoutes(api huma.API, mount RouteMount) {
	huma.Register(api, huma.Operation{
		OperationID: mount.OpIDPrefix + "password-change",
		Method:      http.MethodPost,
		Path:        "/v1/auth" + mount.PathPrefix + "/change-password",
		Summary:     "Change the current user's password",
		Tags:        []string{"Authentication"},
		Security:    []map[string][]string{{"bearerAuth": {}}},
	}, h.ChangePassword)
}

