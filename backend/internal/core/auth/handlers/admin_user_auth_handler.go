package handlers

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strings"

	"github.com/danielgtaylor/huma/v2"

	authModels "github.com/orkestra/backend/internal/core/auth/models"
	"github.com/orkestra/backend/internal/core/auth/services"
	userModels "github.com/orkestra/backend/internal/core/user/models"
	"github.com/orkestra/backend/internal/shared/iface"
)

// AdminUserAuthHandler hosts the operator-tier admin endpoints that
// inspect and manage another user's authentication methods (password,
// MFA, OAuth, email verification). All routes mount under
// /v1/admin/users/{userId}/... on the operator host mux. Each route is
// gated by the per-action system permission contributed by the auth
// module — see auth/module.go::Permissions(). The OAuth-unlink path
// additionally requires a fresh MFA step-up because it removes a
// credential.
type AdminUserAuthHandler struct {
	auth    services.AuthService
	inviter iface.AdminAuthInviter
}

// NewAdminUserAuthHandler wires the dependencies. Both must be the
// operator-tier instances — AdminUserAuthHandler operates on Tier-1
// internal users only. The inviter satisfies iface.AdminAuthInviter
// via structural typing on *services.PasswordAuthService.
func NewAdminUserAuthHandler(auth services.AuthService, inviter iface.AdminAuthInviter) *AdminUserAuthHandler {
	return &AdminUserAuthHandler{auth: auth, inviter: inviter}
}

// --- GET auth-methods ---

// AdminAuthMethodsRequest is path-only.
type AdminAuthMethodsRequest struct {
	UserID string `path:"userId" doc:"UUID of the user to inspect"`
}

type AdminAuthMethodsResponse struct {
	Body authModels.AuthMethodsView
}

// GetAuthMethods returns the aggregated auth state of the target user.
// Read-only; no mutation. The route gate is
// RequireSystemPermission("system.users.admin") — anyone who can
// administer users can see what auth methods they have.
func (h *AdminUserAuthHandler) GetAuthMethods(ctx context.Context, req *AdminAuthMethodsRequest) (*AdminAuthMethodsResponse, error) {
	if req.UserID == "" {
		return nil, huma.Error400BadRequest("userId is required")
	}
	view, err := h.auth.GetUserAuthMethods(ctx, req.UserID)
	if err != nil {
		return nil, mapAdminUserAuthError(err)
	}
	return &AdminAuthMethodsResponse{Body: *view}, nil
}

// --- POST send-password-reset ---

type AdminSendPasswordResetRequest struct {
	UserID string `path:"userId" doc:"UUID of the user to email"`
}

type AdminSimpleResponse struct {
	Body struct {
		Success bool   `json:"success"`
		Message string `json:"message"`
	}
}

// SendPasswordReset triggers the standard password-reset email flow on
// behalf of the admin. Idempotent at the service layer — a missing
// notifier surfaces as 503; an unknown user as 404.
func (h *AdminUserAuthHandler) SendPasswordReset(ctx context.Context, req *AdminSendPasswordResetRequest) (*AdminSimpleResponse, error) {
	if h.inviter == nil {
		return nil, huma.Error503ServiceUnavailable("auth service unavailable")
	}
	if req.UserID == "" {
		return nil, huma.Error400BadRequest("userId is required")
	}
	actorUUID, _ := ctx.Value("userUUID").(string)
	if err := h.inviter.AdminTriggerPasswordReset(ctx, req.UserID); err != nil {
		return nil, mapAdminInviterError(err, "failed to send password reset email")
	}
	// Audit lane — captures every successful admin-on-user action so
	// operator log streams have a record. Replace this with
	// AuthService.RecordSecurityEvent once the persistent audit pipeline
	// (auth_security_events repo + tier-aware writes) lands; tracked as
	// a follow-up in the plan file.
	slog.Info("admin_auth_action",
		"event", "admin_password_reset_sent",
		"actorUUID", actorUUID,
		"targetUUID", req.UserID,
	)
	out := &AdminSimpleResponse{}
	out.Body.Success = true
	out.Body.Message = "password reset email sent"
	return out, nil
}

// --- POST resend-verification ---

type AdminResendVerificationRequest struct {
	UserID string `path:"userId" doc:"UUID of the user to email"`
}

// ResendVerification re-emits the email-verification message. The
// service layer no-ops if the user is already verified, so the
// response is always 200/success — the operator gets a friendly toast
// either way.
func (h *AdminUserAuthHandler) ResendVerification(ctx context.Context, req *AdminResendVerificationRequest) (*AdminSimpleResponse, error) {
	if h.inviter == nil {
		return nil, huma.Error503ServiceUnavailable("auth service unavailable")
	}
	if req.UserID == "" {
		return nil, huma.Error400BadRequest("userId is required")
	}
	actorUUID, _ := ctx.Value("userUUID").(string)
	if err := h.inviter.AdminResendVerification(ctx, req.UserID); err != nil {
		return nil, mapAdminInviterError(err, "failed to resend verification email")
	}
	slog.Info("admin_auth_action",
		"event", "admin_verification_resent",
		"actorUUID", actorUUID,
		"targetUUID", req.UserID,
	)
	out := &AdminSimpleResponse{}
	out.Body.Success = true
	out.Body.Message = "verification email re-sent"
	return out, nil
}

// --- DELETE oauth/{provider} ---

type AdminUnlinkOAuthRequest struct {
	UserID   string `path:"userId" doc:"UUID of the user whose OAuth identity should be unlinked"`
	Provider string `path:"provider" doc:"OAuth provider to unlink" enum:"google,apple,github,discord"`
}

type AdminUnlinkOAuthResponse struct {
	Body struct {
		Success bool `json:"success"`
	}
}

// UnlinkOAuth removes a linked OAuth identity from the target user.
// Self-action and last-credential safeguards live in the service —
// the handler only translates errors to HTTP statuses.
func (h *AdminUserAuthHandler) UnlinkOAuth(ctx context.Context, req *AdminUnlinkOAuthRequest) (*AdminUnlinkOAuthResponse, error) {
	actorUUID, _ := ctx.Value("userUUID").(string)
	if actorUUID == "" {
		return nil, huma.Error401Unauthorized("authentication required")
	}
	if req.UserID == "" {
		return nil, huma.Error400BadRequest("userId is required")
	}
	provider := strings.ToLower(req.Provider)
	switch provider {
	case "google", "apple", "github", "discord":
	default:
		return nil, huma.Error400BadRequest("unsupported provider")
	}
	if err := h.auth.AdminUnlinkOAuth(ctx, actorUUID, req.UserID, userModels.OAuthProvider(provider)); err != nil {
		return nil, mapAdminUserAuthError(err)
	}
	out := &AdminUnlinkOAuthResponse{}
	out.Body.Success = true
	return out, nil
}

// --- error mapping ---

// mapAdminUserAuthError translates the auth service's typed errors
// into Huma HTTP errors. Body codes match the plan: 409 last_credential
// for the lock-out safeguard, 409 self_action for actor==target, 404
// for unknown user / unlinked provider. Anything else surfaces as 500.
func mapAdminUserAuthError(err error) error {
	if err == nil {
		return nil
	}
	switch {
	case errors.Is(err, services.ErrLastCredentialRemoval):
		return huma.NewError(http.StatusConflict, "last_credential",
			&huma.ErrorDetail{Message: "user has no other login method — send a password reset first"})
	case errors.Is(err, services.ErrAdminSelfAction):
		return huma.NewError(http.StatusConflict, "self_action",
			&huma.ErrorDetail{Message: "use the self-service flow for actions on your own account"})
	case errors.Is(err, services.ErrOAuthLinkNotFound):
		return huma.Error404NotFound("provider not linked or user not found")
	}
	// Treat the generic "user not found" sentinel surfaced by the user
	// service as 404 too — the inviter helper has its own mapping but
	// the aggregator path uses GetUserByID directly.
	if msg := err.Error(); msg == "user not found" {
		return huma.Error404NotFound("user not found")
	}
	return huma.Error500InternalServerError("admin auth action failed", err)
}

// mapAdminInviterError mirrors the user module's mapInviteErr helper
// for the inviter-backed routes. Kept local to avoid an import cycle
// (user/handlers ← auth/handlers).
func mapAdminInviterError(err error, generic string) error {
	if err == nil {
		return nil
	}
	if msg := err.Error(); msg == "user not found" {
		return huma.Error404NotFound("user not found")
	}
	if msg := err.Error(); msg == "notifications disabled — cannot send email" {
		return huma.Error503ServiceUnavailable("notifications disabled")
	}
	return huma.Error500InternalServerError(generic, err)
}

// --- registration ---

// RegisterReadAuthMethodsRoute mounts the GET aggregator endpoint. The
// caller wires RequireSystemPermission("system.users.admin") around
// this API instance — see auth/module.go::RegisterRoutes.
func (h *AdminUserAuthHandler) RegisterReadAuthMethodsRoute(api huma.API) {
	huma.Register(api, huma.Operation{
		OperationID: "admin-user-auth-methods",
		Method:      http.MethodGet,
		Path:        "/v1/admin/users/{userId}/auth-methods",
		Summary:     "Admin: aggregate password / MFA / OAuth state of a user",
		Tags:        []string{"Administration", "Authentication"},
		Security:    []map[string][]string{{"bearerAuth": {}}},
	}, h.GetAuthMethods)
}

// RegisterPasswordResetRoute mounts the send-password-reset endpoint.
// Caller wires RequireSystemPermission("system.users.password_reset").
func (h *AdminUserAuthHandler) RegisterPasswordResetRoute(api huma.API) {
	huma.Register(api, huma.Operation{
		OperationID: "admin-user-send-password-reset",
		Method:      http.MethodPost,
		Path:        "/v1/admin/users/{userId}/send-password-reset",
		Summary:     "Admin: trigger a password-reset email for a user",
		Tags:        []string{"Administration", "Authentication"},
		Security:    []map[string][]string{{"bearerAuth": {}}},
	}, h.SendPasswordReset)
}

// RegisterResendVerificationRoute mounts the resend-verification
// endpoint. Caller wires
// RequireSystemPermission("system.users.email_verify_resend").
func (h *AdminUserAuthHandler) RegisterResendVerificationRoute(api huma.API) {
	huma.Register(api, huma.Operation{
		OperationID: "admin-user-resend-verification",
		Method:      http.MethodPost,
		Path:        "/v1/admin/users/{userId}/resend-verification",
		Summary:     "Admin: resend the email-verification message for a user",
		Tags:        []string{"Administration", "Authentication"},
		Security:    []map[string][]string{{"bearerAuth": {}}},
	}, h.ResendVerification)
}

// RegisterOAuthUnlinkRoute mounts the OAuth-unlink endpoint. Caller
// wires RequireSystemPermission("system.users.oauth_unlink") AND
// RequireStepUp(5m) — the action removes a credential.
func (h *AdminUserAuthHandler) RegisterOAuthUnlinkRoute(api huma.API) {
	huma.Register(api, huma.Operation{
		OperationID: "admin-user-oauth-unlink",
		Method:      http.MethodDelete,
		Path:        "/v1/admin/users/{userId}/oauth/{provider}",
		Summary:     "Admin: unlink an OAuth identity from a user",
		Tags:        []string{"Administration", "Authentication"},
		Security:    []map[string][]string{{"bearerAuth": {}}},
	}, h.UnlinkOAuth)
}
