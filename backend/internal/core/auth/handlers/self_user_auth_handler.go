package handlers

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"github.com/danielgtaylor/huma/v2"

	"github.com/orkestra-cc/orkestra-sdk/ctxauth"
	authModels "github.com/orkestra/backend/internal/core/auth/models"
	"github.com/orkestra/backend/internal/core/auth/services"
	userModels "github.com/orkestra/backend/internal/core/user/models"
	"github.com/orkestra/backend/internal/shared/middleware"
)

// SelfUserAuthHandler hosts the self-service security-center endpoints
// — what an end user can do for **their own** account from
// `/user/security`. Mirrors the structure of AdminUserAuthHandler:
// each route gets its own Register* method so the caller can apply
// the right gate (RequireGlobal for reads, RequireGlobal +
// RequireStepUp(5m) for credential / session removal).
//
// The handler is tier-stateless — the same instance can be mounted
// under OperatorMount or ClientMount. Per-tier service binding lives
// in module.go (each tier's authService and mfaService is scoped to
// its own _* collections).
type SelfUserAuthHandler struct {
	auth services.AuthService
	mfa  services.MFAService
}

// NewSelfUserAuthHandler wires the dependencies. Both must be the
// **same tier** as the route mount — mounting an operator handler
// under ClientMount (or vice versa) would route a client user's
// request through the wrong tier's collections.
func NewSelfUserAuthHandler(auth services.AuthService, mfa services.MFAService) *SelfUserAuthHandler {
	return &SelfUserAuthHandler{auth: auth, mfa: mfa}
}

// --- GET auth-methods (read-only aggregator) ---

type selfAuthMethodsResponse struct {
	Body authModels.AuthMethodsView
}

// GetAuthMethods returns the caller's own auth state — same shape as
// the admin endpoint. Reused so the frontend can drive the page
// header without a parallel schema. The HTTP gate is RequireGlobal
// only (any signed-in user can read their own).
func (h *SelfUserAuthHandler) GetAuthMethods(ctx context.Context, _ *struct{}) (*selfAuthMethodsResponse, error) {
	userUUID, ok := ctxauth.GetUserUUID(ctx)
	if !ok || userUUID == "" {
		return nil, huma.Error401Unauthorized("authentication required")
	}
	view, err := h.auth.GetUserAuthMethods(ctx, userUUID)
	if err != nil {
		return nil, mapSelfAuthError(err)
	}
	return &selfAuthMethodsResponse{Body: *view}, nil
}

// --- GET sessions (active list, IsCurrent stamped from JWT sid) ---

type selfSessionsResponse struct {
	Body authModels.SessionsResponse
}

// ListSessions returns the caller's active sessions, sorted
// most-recent-first by repo contract. The IsCurrent flag is stamped
// here (not in the service) so the JWT sid lives strictly at the
// HTTP boundary — service tests can drive the same code path
// without manufacturing a context.
func (h *SelfUserAuthHandler) ListSessions(ctx context.Context, _ *struct{}) (*selfSessionsResponse, error) {
	userUUID, ok := ctxauth.GetUserUUID(ctx)
	if !ok || userUUID == "" {
		return nil, huma.Error401Unauthorized("authentication required")
	}
	resp, err := h.auth.ListUserSessions(ctx, userUUID)
	if err != nil {
		return nil, mapSelfAuthError(err)
	}
	if resp == nil {
		resp = &authModels.SessionsResponse{Sessions: []authModels.SessionInfo{}}
	}
	currentSid, _ := middleware.GetSessionID(ctx)
	if currentSid != "" {
		for i := range resp.Sessions {
			if resp.Sessions[i].SessionID == currentSid {
				resp.Sessions[i].IsCurrent = true
				resp.CurrentDevice = resp.Sessions[i].DeviceID
			}
		}
	}
	return &selfSessionsResponse{Body: *resp}, nil
}

// --- DELETE oauth/{provider} (self-unlink) ---

type selfUnlinkOAuthRequest struct {
	Provider string `path:"provider" doc:"OAuth provider to unlink" enum:"google,apple,github,discord"`
}

type selfSimpleResponse struct {
	Body struct {
		Success bool `json:"success"`
	}
}

// UnlinkOAuth removes one OAuth identity from the caller's own
// account. The service layer enforces the last-credential safeguard
// — a user with no password and a single OAuth link is refused with
// 409 last_credential.
func (h *SelfUserAuthHandler) UnlinkOAuth(ctx context.Context, req *selfUnlinkOAuthRequest) (*selfSimpleResponse, error) {
	userUUID, ok := ctxauth.GetUserUUID(ctx)
	if !ok || userUUID == "" {
		return nil, huma.Error401Unauthorized("authentication required")
	}
	provider := strings.ToLower(req.Provider)
	switch provider {
	case "google", "apple", "github", "discord":
	default:
		return nil, huma.Error400BadRequest("unsupported provider")
	}
	if err := h.auth.SelfUnlinkOAuth(ctx, userUUID, userModels.OAuthProvider(provider)); err != nil {
		return nil, mapSelfAuthError(err)
	}
	// Audit lane: service layer already emits self_auth_action with
	// event=self_oauth_unlink. Don't duplicate here — the admin
	// surface follows the same one-layer-per-action pattern.
	out := &selfSimpleResponse{}
	out.Body.Success = true
	return out, nil
}

// --- DELETE sessions/{sessionId} (revoke one) ---

type selfRevokeSessionRequest struct {
	SessionID string `path:"sessionId" doc:"UUID of the session to revoke"`
}

type selfRevokeSessionResponse struct{}

// RevokeSession revokes one session by UUID. The service rejects
// revoke-current with ErrCannotRevokeCurrent so the caller's HTTP
// response can complete; logout is the right tool for self-revoke.
func (h *SelfUserAuthHandler) RevokeSession(ctx context.Context, req *selfRevokeSessionRequest) (*selfRevokeSessionResponse, error) {
	userUUID, ok := ctxauth.GetUserUUID(ctx)
	if !ok || userUUID == "" {
		return nil, huma.Error401Unauthorized("authentication required")
	}
	if req.SessionID == "" {
		return nil, huma.Error400BadRequest("sessionId is required")
	}
	currentSid, _ := middleware.GetSessionID(ctx)
	if err := h.auth.RevokeUserSession(ctx, userUUID, req.SessionID, currentSid); err != nil {
		return nil, mapSelfAuthError(err)
	}
	return &selfRevokeSessionResponse{}, nil
}

// --- DELETE sessions (revoke all-except-current) ---

type selfRevokeAllSessionsResponse struct {
	Body struct {
		Revoked int `json:"revoked"`
	}
}

// RevokeAllSessions revokes every active session except the one the
// request is coming from. The current session is left alive so the
// HTTP response can complete and the user keeps working without a
// reauth round-trip.
func (h *SelfUserAuthHandler) RevokeAllSessions(ctx context.Context, _ *struct{}) (*selfRevokeAllSessionsResponse, error) {
	userUUID, ok := ctxauth.GetUserUUID(ctx)
	if !ok || userUUID == "" {
		return nil, huma.Error401Unauthorized("authentication required")
	}
	currentSid, _ := middleware.GetSessionID(ctx)
	count, err := h.auth.RevokeAllUserSessionsExcept(ctx, userUUID, currentSid)
	if err != nil {
		return nil, mapSelfAuthError(err)
	}
	out := &selfRevokeAllSessionsResponse{}
	out.Body.Revoked = count
	return out, nil
}

// --- error mapping ---

// mapSelfAuthError translates the auth service's typed errors into
// Huma HTTP responses. Body codes follow the plan: 409 last_credential
// for the lockout safeguard, 409 cannot_revoke_current for the revoke-
// current guard, 404 for unknown session / unlinked provider.
func mapSelfAuthError(err error) error {
	if err == nil {
		return nil
	}
	switch {
	case errors.Is(err, services.ErrLastCredentialRemoval):
		return huma.NewError(http.StatusConflict, "last_credential",
			&huma.ErrorDetail{Message: "you have no other login method — set a password before unlinking this provider"})
	case errors.Is(err, services.ErrCannotRevokeCurrent):
		return huma.NewError(http.StatusConflict, "cannot_revoke_current",
			&huma.ErrorDetail{Message: "use the logout endpoint to end the current session"})
	case errors.Is(err, services.ErrSessionNotFound):
		return huma.Error404NotFound("session not found")
	case errors.Is(err, services.ErrOAuthLinkNotFound):
		return huma.Error404NotFound("provider not linked")
	case errors.Is(err, services.ErrMFANotEnrolled):
		return huma.Error400BadRequest("mfa is not enrolled — enroll a TOTP factor before regenerating backup codes")
	}
	if msg := err.Error(); msg == "user not found" {
		return huma.Error404NotFound("user not found")
	}
	return huma.Error500InternalServerError("self auth action failed", err)
}

// --- registration ---

// RegisterReadRoutes mounts the GET aggregator + GET sessions
// endpoints. Caller wires `RequireGlobal()` around this API
// instance — any signed-in user can read their own auth state.
func (h *SelfUserAuthHandler) RegisterReadRoutes(api huma.API, mount RouteMount) {
	huma.Register(api, huma.Operation{
		OperationID: mount.OpIDPrefix + "self-auth-methods",
		Method:      http.MethodGet,
		Path:        "/v1/auth" + mount.PathPrefix + "/me/auth-methods",
		Summary:     "Return the current user's authentication state (password / MFA / OAuth)",
		Tags:        []string{"Authentication", "Self-Service"},
		Security:    []map[string][]string{{"bearerAuth": {}}},
	}, h.GetAuthMethods)

	huma.Register(api, huma.Operation{
		OperationID: mount.OpIDPrefix + "self-list-sessions",
		Method:      http.MethodGet,
		Path:        "/v1/auth" + mount.PathPrefix + "/me/sessions",
		Summary:     "List the current user's active sessions",
		Description: "Returns isActive && !expired sessions for the caller. The IsCurrent flag is stamped from the JWT sid so the UI can disable revoke on the row the request came from.",
		Tags:        []string{"Authentication", "Self-Service"},
		Security:    []map[string][]string{{"bearerAuth": {}}},
	}, h.ListSessions)
}

// RegisterStepUpRoutes mounts the destructive endpoints — credential
// removal and session revocation. Caller wires `RequireGlobal()` +
// `RequireStepUp(5m)` so a fresh MFA proof is required.
func (h *SelfUserAuthHandler) RegisterStepUpRoutes(api huma.API, mount RouteMount) {
	huma.Register(api, huma.Operation{
		OperationID: mount.OpIDPrefix + "self-unlink-oauth",
		Method:      http.MethodDelete,
		Path:        "/v1/auth" + mount.PathPrefix + "/me/oauth/{provider}",
		Summary:     "Unlink one OAuth identity from the current user",
		Tags:        []string{"Authentication", "Self-Service"},
		Security:    []map[string][]string{{"bearerAuth": {}}},
	}, h.UnlinkOAuth)

	huma.Register(api, huma.Operation{
		OperationID: mount.OpIDPrefix + "self-revoke-session",
		Method:      http.MethodDelete,
		Path:        "/v1/auth" + mount.PathPrefix + "/me/sessions/{sessionId}",
		Summary:     "Revoke one of the current user's sessions",
		Description: "Returns 409 cannot_revoke_current when the target is the calling session — use logout instead.",
		Tags:        []string{"Authentication", "Self-Service"},
		Security:    []map[string][]string{{"bearerAuth": {}}},
	}, h.RevokeSession)

	huma.Register(api, huma.Operation{
		OperationID: mount.OpIDPrefix + "self-revoke-all-sessions",
		Method:      http.MethodDelete,
		Path:        "/v1/auth" + mount.PathPrefix + "/me/sessions",
		Summary:     "Revoke all of the current user's sessions except the calling one",
		Tags:        []string{"Authentication", "Self-Service"},
		Security:    []map[string][]string{{"bearerAuth": {}}},
	}, h.RevokeAllSessions)
}
