package handlers

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/danielgtaylor/huma/v2"

	authModels "github.com/orkestra/backend/internal/core/auth/models"
	"github.com/orkestra/backend/internal/core/auth/repository"
	"github.com/orkestra/backend/internal/core/auth/services"
	"github.com/orkestra/backend/internal/shared/errcode"
	"github.com/orkestra/backend/pkg/sdk/iface"
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
	// eventRepo backs the GET /security-events endpoint. Nil falls
	// back to "no rows yet" so a build without the audit pipeline
	// returns an empty page instead of a 500.
	eventRepo repository.SecurityEventRepository
}

// NewAdminUserAuthHandler wires the dependencies. auth + inviter must
// be the operator-tier instances — AdminUserAuthHandler operates on
// Tier-1 internal users only. The inviter satisfies
// iface.AdminAuthInviter via structural typing on
// *services.PasswordAuthService. eventRepo is optional; pass the
// shared (non-tier-split) instance built in module.go.
func NewAdminUserAuthHandler(auth services.AuthService, inviter iface.AdminAuthInviter, eventRepo repository.SecurityEventRepository) *AdminUserAuthHandler {
	return &AdminUserAuthHandler{auth: auth, inviter: inviter, eventRepo: eventRepo}
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
	// Audit lane: persistent + slog via AuthService — Phase 2.2 of the
	// core-completion epic. Both halves are best-effort so a degraded
	// audit pipeline never blocks the user-facing action.
	h.auth.RecordAdminAuthEvent(ctx, "admin_password_reset_sent", actorUUID, req.UserID, nil)
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
	h.auth.RecordAdminAuthEvent(ctx, "admin_verification_resent", actorUUID, req.UserID, nil)
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
	if err := h.auth.AdminUnlinkOAuth(ctx, actorUUID, req.UserID, iface.OAuthProvider(provider)); err != nil {
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
	case errors.Is(err, services.ErrAuthPolicyUnavailable):
		return errcode.ServiceUnavailable(errcode.AuthPolicyUnavailable,
			"Sign-in policy is temporarily unavailable; try again shortly.")
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
	if errors.Is(err, services.ErrPasswordLoginDisabled) {
		return errcode.Conflict(errcode.AuthPasswordLoginDisabled,
			"Email/password sign-in is disabled on this user's surface; a reset link would mint a credential the surface refuses. Re-enable the method first.")
	}
	if errors.Is(err, services.ErrAuthPolicyUnavailable) {
		return errcode.ServiceUnavailable(errcode.AuthPolicyUnavailable,
			"Sign-in policy is temporarily unavailable; try again shortly.")
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

// --- GET security-events ---

// AdminSecurityEventsRequest is the path + query envelope for the
// per-user audit timeline.
type AdminSecurityEventsRequest struct {
	UserID string `path:"userId" doc:"UUID of the user whose audit timeline to fetch"`
	Limit  int    `query:"limit" doc:"Max rows to return (clamped to 500, default 100)" example:"100"`
	Offset int    `query:"offset" doc:"Pagination offset (default 0)" example:"0"`
	// SinceDays restricts to events from the last N days. 0 = no
	// filter. Useful for "show me last 7 days of activity" without
	// the client computing a timestamp.
	SinceDays int `query:"sinceDays" doc:"Restrict to events from the last N days (0 = unbounded)" example:"30"`
}

// AdminSecurityEventsResponse mirrors the page-of-rows shape every
// admin list endpoint uses. Total is the total matching the filter,
// not the page size, so the UI can render "Showing 100 of 1,243".
type AdminSecurityEventsResponse struct {
	Body struct {
		Events []*authModels.SecurityEvent `json:"events"`
		Total  int64                       `json:"total"`
		Offset int                         `json:"offset"`
		Limit  int                         `json:"limit"`
	}
}

// GetSecurityEvents returns the audit timeline for one user, newest
// first. Gated by system.users.admin (incidental to user
// administration — same gate as the auth-methods aggregator).
//
// Reads route directly through SecurityEventRepository.ListByUserPaged
// rather than the AuthService surface so the response carries `total`
// for pagination without a second round trip.
func (h *AdminUserAuthHandler) GetSecurityEvents(ctx context.Context, req *AdminSecurityEventsRequest) (*AdminSecurityEventsResponse, error) {
	if req.UserID == "" {
		return nil, huma.Error400BadRequest("userId is required")
	}
	if h.eventRepo == nil {
		// Degraded build with the persistent audit pipeline unwired.
		// Return an empty page rather than 500 so the SPA renders the
		// empty-state instead of an error toast.
		out := &AdminSecurityEventsResponse{}
		out.Body.Events = []*authModels.SecurityEvent{}
		out.Body.Limit = req.Limit
		return out, nil
	}
	var since *time.Time
	if req.SinceDays > 0 {
		t := time.Now().UTC().Add(-time.Duration(req.SinceDays) * 24 * time.Hour)
		since = &t
	}
	events, total, err := h.eventRepo.ListByUserPaged(ctx, req.UserID, req.Offset, req.Limit, since)
	if err != nil {
		return nil, huma.Error500InternalServerError("failed to list security events", err)
	}
	out := &AdminSecurityEventsResponse{}
	out.Body.Events = events
	out.Body.Total = total
	out.Body.Offset = req.Offset
	out.Body.Limit = req.Limit
	return out, nil
}

// RegisterSecurityEventsRoute mounts the audit-timeline endpoint.
// Caller wires RequireSystemPermission("system.users.admin") — the
// same gate the auth-methods aggregator uses (reading audit rows is
// incidental to user administration).
func (h *AdminUserAuthHandler) RegisterSecurityEventsRoute(api huma.API) {
	huma.Register(api, huma.Operation{
		OperationID: "admin-user-security-events",
		Method:      http.MethodGet,
		Path:        "/v1/admin/users/{userId}/security-events",
		Summary:     "Admin: list the audit timeline for a user",
		Tags:        []string{"Administration", "Authentication"},
		Security:    []map[string][]string{{"bearerAuth": {}}},
	}, h.GetSecurityEvents)
}
