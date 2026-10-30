package handlers

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/danielgtaylor/huma/v2"
	authModels "github.com/orkestra/backend/internal/core/auth/models"
	"github.com/orkestra/backend/internal/core/auth/services"
	"github.com/orkestra/backend/internal/shared/errcode"
	"github.com/orkestra/backend/internal/shared/middleware"
	"github.com/orkestra/backend/pkg/sdk/ctxauth"
	"github.com/orkestra/backend/pkg/sdk/iface"
)

// PINHandler serves the PIN sign-in method of one audience tier:
// the public sign-in and device enrollment, the
// caller's own PIN, and the administrator's routes on another user's PIN.
// Route gates are applied by module.go: RequireGlobal for reads,
// RequireStepUp for credential changes, the enrolment-proof gate for a
// self-issued enrollment code, user.update + RequireStepUp for admins.
type PINHandler struct {
	svc          *services.PINAuthService
	cookieName   string
	cookieDomain string
	cookieSecure bool
}

// NewPINHandler binds the handler to one tier's PIN service and cookie.
func NewPINHandler(svc *services.PINAuthService, cookieName, cookieDomain string, cookieSecure bool) *PINHandler {
	return &PINHandler{svc: svc, cookieName: cookieName, cookieDomain: cookieDomain, cookieSecure: cookieSecure}
}

// --- public: sign-in ---

// PINLoginRequest is POST /v1/auth/{tier}/login/pin.
type PINLoginRequest struct {
	Body struct {
		Username     string `json:"username" minLength:"1" maxLength:"50" doc:"Username (case-sensitive)"`
		PIN          string `json:"pin" minLength:"1" maxLength:"16" doc:"Numeric PIN"`
		DeviceID     string `json:"deviceId,omitempty" maxLength:"128" doc:"Stable per-installation device id; required when PIN sign-in is bound to enrolled devices"`
		DeviceSecret string `json:"deviceSecret,omitempty" maxLength:"128" doc:"Secret returned once by device enrollment"`
		Platform     string `json:"platform,omitempty" enum:"ios,android,web" doc:"Client platform; ios and android receive the refresh token in the body"`
	}
}

// PINLoginResponse mirrors the password login response. Mobile platforms
// (ios, android) also get the refresh token in the body, as the mobile
// OAuth routes do; web gets it only as the HttpOnly cookie.
type PINLoginResponse struct {
	SetCookie string `header:"Set-Cookie"`
	Body      struct {
		Success      bool        `json:"success"`
		AccessToken  string      `json:"accessToken"`
		RefreshToken string      `json:"refreshToken,omitempty"`
		TokenType    string      `json:"tokenType"`
		ExpiresIn    int64       `json:"expiresIn"`
		User         interface{} `json:"user,omitempty"`
	}
}

// Login handles POST /v1/auth/{tier}/login/pin.
func (h *PINHandler) Login(ctx context.Context, req *PINLoginRequest) (*PINLoginResponse, error) {
	tokens, err := h.svc.Login(ctx, services.PINLoginInput{
		Username:     req.Body.Username,
		PIN:          req.Body.PIN,
		DeviceID:     req.Body.DeviceID,
		DeviceSecret: req.Body.DeviceSecret,
		Platform:     req.Body.Platform,
		IP:           clientIPFromCtx(ctx),
		UserAgent:    userAgentFromCtx(ctx),
	})
	if err != nil {
		if errors.Is(err, services.ErrPINNotAllowed) {
			// The PIN was right; the account may not use one.
			return nil, errcode.Forbidden(errcode.AuthPINNotAllowed,
				"This account cannot sign in with a PIN. Use your password or another sign-in method.")
		}
		return nil, mapPINError(err)
	}
	resp := &PINLoginResponse{}
	resp.SetCookie = buildRefreshCookie(h.cookieName, tokens.RefreshToken, h.cookieDomain, h.cookieSecure, int(tokens.RefreshExpiresIn))
	resp.Body.Success = true
	resp.Body.AccessToken = tokens.AccessToken
	resp.Body.TokenType = tokens.TokenType
	resp.Body.ExpiresIn = tokens.ExpiresIn
	resp.Body.User = tokens.User
	if p := strings.ToLower(req.Body.Platform); p == "ios" || p == "android" {
		resp.Body.RefreshToken = tokens.RefreshToken
	}
	return resp, nil
}

// PINDeviceEnrollRequest is POST /v1/auth/{tier}/login/pin/device.
type PINDeviceEnrollRequest struct {
	Body struct {
		Username   string `json:"username" minLength:"1" maxLength:"50"`
		Code       string `json:"code" minLength:"1" maxLength:"16" doc:"One-time enrollment code (XXXX-XXXX) issued by an administrator or by the user"`
		DeviceID   string `json:"deviceId" minLength:"1" maxLength:"128"`
		DeviceName string `json:"deviceName,omitempty" maxLength:"100"`
		Platform   string `json:"platform,omitempty" enum:"ios,android,web"`
	}
}

// PINDeviceEnrollResponse carries the device secret, returned exactly once.
type PINDeviceEnrollResponse struct {
	Body struct {
		DeviceSecret string `json:"deviceSecret" doc:"Store it in the device's secure storage; it is never shown again"`
	}
}

// EnrollDevice handles POST /v1/auth/{tier}/login/pin/device.
func (h *PINHandler) EnrollDevice(ctx context.Context, req *PINDeviceEnrollRequest) (*PINDeviceEnrollResponse, error) {
	secret, err := h.svc.EnrollDevice(ctx, services.PINDeviceEnrollInput{
		Username: req.Body.Username, Code: req.Body.Code, DeviceID: req.Body.DeviceID,
		DeviceName: req.Body.DeviceName, Platform: req.Body.Platform, IP: clientIPFromCtx(ctx),
	})
	if err != nil {
		return nil, mapPINError(err)
	}
	resp := &PINDeviceEnrollResponse{}
	resp.Body.DeviceSecret = secret
	return resp, nil
}

// --- self ---

type pinViewResponse struct {
	Body authModels.PINMethodView
}

// GetOwnPIN handles GET /v1/auth/{tier}/me/pin.
func (h *PINHandler) GetOwnPIN(ctx context.Context, _ *struct{}) (*pinViewResponse, error) {
	userUUID, err := callerUUID(ctx)
	if err != nil {
		return nil, err
	}
	view := h.svc.ViewByUUID(ctx, userUUID)
	if view == nil {
		return nil, huma.Error404NotFound("user not found")
	}
	return &pinViewResponse{Body: *view}, nil
}

// SetPINRequest is the body of the two PIN-setting routes.
type SetPINRequest struct {
	Body struct {
		PIN string `json:"pin" maxLength:"16" doc:"New PIN: exactly auth.pinLength digits, not a repeated digit or a sequence (422 auth.pin_invalid / auth.pin_too_weak)"`
	}
}

// ChangeOwnPIN handles PUT /v1/auth/{tier}/me/pin.
func (h *PINHandler) ChangeOwnPIN(ctx context.Context, req *SetPINRequest) (*struct{}, error) {
	userUUID, err := callerUUID(ctx)
	if err != nil {
		return nil, err
	}
	amr, _ := middleware.GetAMR(ctx)
	if err := h.svc.ChangeOwnPIN(ctx, userUUID, amr, req.Body.PIN); err != nil {
		return nil, mapPINError(err)
	}
	return nil, nil
}

// RemoveOwnPIN handles DELETE /v1/auth/{tier}/me/pin.
func (h *PINHandler) RemoveOwnPIN(ctx context.Context, _ *struct{}) (*struct{}, error) {
	userUUID, err := callerUUID(ctx)
	if err != nil {
		return nil, err
	}
	amr, _ := middleware.GetAMR(ctx)
	if err := h.svc.RemoveOwnPIN(ctx, userUUID, amr); err != nil {
		return nil, mapPINError(err)
	}
	return nil, nil
}

// PINEnrollmentCodeResponse returns a one-time device enrollment code.
type PINEnrollmentCodeResponse struct {
	Body struct {
		Code      string    `json:"code" doc:"Type it on the device within the validity window; single use"`
		ExpiresAt time.Time `json:"expiresAt"`
	}
}

// IssueOwnEnrollmentCode handles POST /v1/auth/{tier}/me/pin/enrollment-code.
func (h *PINHandler) IssueOwnEnrollmentCode(ctx context.Context, _ *struct{}) (*PINEnrollmentCodeResponse, error) {
	userUUID, err := callerUUID(ctx)
	if err != nil {
		return nil, err
	}
	amr, _ := middleware.GetAMR(ctx)
	code, expires, err := h.svc.IssueEnrollmentCode(ctx, userUUID, userUUID, true, amr)
	if err != nil {
		return nil, mapPINError(err)
	}
	resp := &PINEnrollmentCodeResponse{}
	resp.Body.Code, resp.Body.ExpiresAt = code, expires
	return resp, nil
}

// PINDevicePath addresses one enrolled device of the caller.
type PINDevicePath struct {
	DeviceID string `path:"deviceId" maxLength:"128"`
}

// RevokeOwnDevice handles DELETE /v1/auth/{tier}/me/pin/devices/{deviceId}.
func (h *PINHandler) RevokeOwnDevice(ctx context.Context, req *PINDevicePath) (*struct{}, error) {
	userUUID, err := callerUUID(ctx)
	if err != nil {
		return nil, err
	}
	if err := h.svc.RevokeDevice(ctx, userUUID, userUUID, req.DeviceID, true); err != nil {
		return nil, mapPINError(err)
	}
	return nil, nil
}

// --- admin ---

// AdminPINPath addresses the target user.
type AdminPINPath struct {
	UserID string `path:"userId"`
}

// AdminGetPIN handles GET {base}/auth-methods/pin.
func (h *PINHandler) AdminGetPIN(ctx context.Context, req *AdminPINPath) (*pinViewResponse, error) {
	view := h.svc.ViewByUUID(ctx, req.UserID)
	if view == nil {
		return nil, huma.Error404NotFound("user not found")
	}
	return &pinViewResponse{Body: *view}, nil
}

// AdminSetPINRequest is PUT {base}/auth-methods/pin.
type AdminSetPINRequest struct {
	UserID string `path:"userId"`
	SetPINRequest
}

// AdminSetPIN handles PUT {base}/auth-methods/pin.
func (h *PINHandler) AdminSetPIN(ctx context.Context, req *AdminSetPINRequest) (*struct{}, error) {
	actor, err := callerUUID(ctx)
	if err != nil {
		return nil, err
	}
	if err := h.svc.AdminSetPIN(ctx, actor, req.UserID, req.Body.PIN); err != nil {
		return nil, mapPINError(err)
	}
	return nil, nil
}

// AdminRemovePIN handles DELETE {base}/auth-methods/pin.
func (h *PINHandler) AdminRemovePIN(ctx context.Context, req *AdminPINPath) (*struct{}, error) {
	actor, err := callerUUID(ctx)
	if err != nil {
		return nil, err
	}
	if err := h.svc.AdminRemovePIN(ctx, actor, req.UserID); err != nil {
		return nil, mapPINError(err)
	}
	return nil, nil
}

// AdminIssueEnrollmentCode handles POST {base}/auth-methods/pin/enrollment-code.
func (h *PINHandler) AdminIssueEnrollmentCode(ctx context.Context, req *AdminPINPath) (*PINEnrollmentCodeResponse, error) {
	actor, err := callerUUID(ctx)
	if err != nil {
		return nil, err
	}
	code, expires, err := h.svc.IssueEnrollmentCode(ctx, actor, req.UserID, false, nil)
	if err != nil {
		return nil, mapPINError(err)
	}
	resp := &PINEnrollmentCodeResponse{}
	resp.Body.Code, resp.Body.ExpiresAt = code, expires
	return resp, nil
}

// AdminPINDevicePath addresses one device of the target user.
type AdminPINDevicePath struct {
	UserID   string `path:"userId"`
	DeviceID string `path:"deviceId" maxLength:"128"`
}

// AdminRevokeDevice handles DELETE {base}/auth-methods/pin/devices/{deviceId}.
func (h *PINHandler) AdminRevokeDevice(ctx context.Context, req *AdminPINDevicePath) (*struct{}, error) {
	actor, err := callerUUID(ctx)
	if err != nil {
		return nil, err
	}
	if err := h.svc.RevokeDevice(ctx, actor, req.UserID, req.DeviceID, false); err != nil {
		return nil, mapPINError(err)
	}
	return nil, nil
}

// --- registration ---

// RegisterPublicRoutes mounts the sign-in and device-enrollment routes.
func (h *PINHandler) RegisterPublicRoutes(api huma.API, mount RouteMount) {
	huma.Register(api, huma.Operation{
		OperationID: mount.OpIDPrefix + "pin-login",
		Method:      http.MethodPost,
		Path:        "/v1/auth" + mount.PathPrefix + "/login/pin",
		Summary:     "Sign in with username and PIN",
		Description: "Weak, opt-in method (auth.pinLoginEnabled{Admin,Client}). The session carries amr [\"pin\"]: it never satisfies MFA or step-up and cannot manage credentials. With auth.pinRequiresTrustedDevice on, deviceId and deviceSecret of an enrolled device are required.",
		Tags:        []string{"Authentication"},
	}, h.Login)
	huma.Register(api, huma.Operation{
		OperationID: mount.OpIDPrefix + "pin-device-enroll",
		Method:      http.MethodPost,
		Path:        "/v1/auth" + mount.PathPrefix + "/login/pin/device",
		Summary:     "Enroll a device for PIN sign-in with a one-time code",
		Tags:        []string{"Authentication"},
	}, h.EnrollDevice)
}

// RegisterSelfReadRoutes mounts GET /me/pin (RequireGlobal).
func (h *PINHandler) RegisterSelfReadRoutes(api huma.API, mount RouteMount) {
	huma.Register(api, huma.Operation{
		OperationID: mount.OpIDPrefix + "self-pin",
		Method:      http.MethodGet,
		Path:        "/v1/auth" + mount.PathPrefix + "/me/pin",
		Summary:     "Read your own PIN sign-in state",
		Tags:        []string{"Authentication", "Self-Service"},
		Security:    []map[string][]string{{"bearerAuth": {}}},
	}, h.GetOwnPIN)
}

// RegisterSelfSetRoute mounts PUT /me/pin (caller wires RequireGlobal +
// RequireStepUp + RequireSystemPermission("auth.pin_login")).
func (h *PINHandler) RegisterSelfSetRoute(api huma.API, mount RouteMount) {
	huma.Register(api, huma.Operation{
		OperationID:   mount.OpIDPrefix + "self-pin-set",
		Method:        http.MethodPut,
		Path:          "/v1/auth" + mount.PathPrefix + "/me/pin",
		Summary:       "Set or change your own PIN",
		Description:   "Refused from a PIN session (403 auth.pin_session_forbidden). Ends every PIN session of the account.",
		Tags:          []string{"Authentication", "Self-Service"},
		Security:      []map[string][]string{{"bearerAuth": {}}},
		DefaultStatus: http.StatusNoContent,
	}, h.ChangeOwnPIN)
}

// RegisterSelfRemoveRoutes mounts the caller's own removals (caller wires
// RequireGlobal + RequireStepUp). No permission gate: a user whose
// auth.pin_login was revoked may still remove what is left.
func (h *PINHandler) RegisterSelfRemoveRoutes(api huma.API, mount RouteMount) {
	huma.Register(api, huma.Operation{
		OperationID:   mount.OpIDPrefix + "self-pin-remove",
		Method:        http.MethodDelete,
		Path:          "/v1/auth" + mount.PathPrefix + "/me/pin",
		Summary:       "Remove your own PIN and its enrolled devices",
		Tags:          []string{"Authentication", "Self-Service"},
		Security:      []map[string][]string{{"bearerAuth": {}}},
		DefaultStatus: http.StatusNoContent,
	}, h.RemoveOwnPIN)
	huma.Register(api, huma.Operation{
		OperationID:   mount.OpIDPrefix + "self-pin-device-revoke",
		Method:        http.MethodDelete,
		Path:          "/v1/auth" + mount.PathPrefix + "/me/pin/devices/{deviceId}",
		Summary:       "Revoke one of your PIN devices",
		Tags:          []string{"Authentication", "Self-Service"},
		Security:      []map[string][]string{{"bearerAuth": {}}},
		DefaultStatus: http.StatusNoContent,
	}, h.RevokeOwnDevice)
}

// RegisterSelfEnrolmentRoutes mounts the self-issued enrollment code
// (RequireGlobal + the enrolment-proof gate: a fresh login or a fresh
// second factor, and never a PIN session).
func (h *PINHandler) RegisterSelfEnrolmentRoutes(api huma.API, mount RouteMount) {
	huma.Register(api, huma.Operation{
		OperationID: mount.OpIDPrefix + "self-pin-enrollment-code",
		Method:      http.MethodPost,
		Path:        "/v1/auth" + mount.PathPrefix + "/me/pin/enrollment-code",
		Summary:     "Issue a one-time code to enroll a device for your PIN",
		Tags:        []string{"Authentication", "Self-Service"},
		Security:    []map[string][]string{{"bearerAuth": {}}},
	}, h.IssueOwnEnrollmentCode)
}

// AdminPINBase is the user collection an admin route addresses.
type AdminPINBase struct {
	// Path is "/v1/admin/users/{userId}" or "/v1/admin/client-users/{userId}".
	Path string
	// OpIDPrefix keeps the two surfaces' operation ids distinct.
	OpIDPrefix string
}

// AdminPINOperatorUsers / AdminPINClientUsers are the two admin surfaces.
var (
	AdminPINOperatorUsers = AdminPINBase{Path: "/v1/admin/users/{userId}", OpIDPrefix: "admin-user-"}
	AdminPINClientUsers   = AdminPINBase{Path: "/v1/admin/client-users/{userId}", OpIDPrefix: "admin-client-user-"}
)

// RegisterAdminReadRoutes mounts GET {base}/auth-methods/pin.
func (h *PINHandler) RegisterAdminReadRoutes(api huma.API, base AdminPINBase) {
	huma.Register(api, huma.Operation{
		OperationID: base.OpIDPrefix + "pin",
		Method:      http.MethodGet,
		Path:        base.Path + "/auth-methods/pin",
		Summary:     "Admin: read a user's PIN sign-in state",
		Tags:        []string{"Administration", "Authentication"},
		Security:    []map[string][]string{{"bearerAuth": {}}},
	}, h.AdminGetPIN)
}

// RegisterAdminManageRoutes mounts the admin credential routes (caller
// wires user.update + RequireStepUp).
func (h *PINHandler) RegisterAdminManageRoutes(api huma.API, base AdminPINBase) {
	huma.Register(api, huma.Operation{
		OperationID:   base.OpIDPrefix + "pin-set",
		Method:        http.MethodPut,
		Path:          base.Path + "/auth-methods/pin",
		Summary:       "Admin: set or replace a user's PIN",
		Description:   "Refused for privileged roles, service principals, users without a unique username or without auth.pin_login (409), and for a target whose role is above the caller's (403 user.target_tier_forbidden). Ends the user's PIN sessions.",
		Tags:          []string{"Administration", "Authentication"},
		Security:      []map[string][]string{{"bearerAuth": {}}},
		DefaultStatus: http.StatusNoContent,
	}, h.AdminSetPIN)
	huma.Register(api, huma.Operation{
		OperationID:   base.OpIDPrefix + "pin-remove",
		Method:        http.MethodDelete,
		Path:          base.Path + "/auth-methods/pin",
		Summary:       "Admin: remove a user's PIN and enrolled devices",
		Tags:          []string{"Administration", "Authentication"},
		Security:      []map[string][]string{{"bearerAuth": {}}},
		DefaultStatus: http.StatusNoContent,
	}, h.AdminRemovePIN)
	huma.Register(api, huma.Operation{
		OperationID: base.OpIDPrefix + "pin-enrollment-code",
		Method:      http.MethodPost,
		Path:        base.Path + "/auth-methods/pin/enrollment-code",
		Summary:     "Admin: issue a one-time device enrollment code for a user's PIN",
		Tags:        []string{"Administration", "Authentication"},
		Security:    []map[string][]string{{"bearerAuth": {}}},
	}, h.AdminIssueEnrollmentCode)
	huma.Register(api, huma.Operation{
		OperationID:   base.OpIDPrefix + "pin-device-revoke",
		Method:        http.MethodDelete,
		Path:          base.Path + "/auth-methods/pin/devices/{deviceId}",
		Summary:       "Admin: revoke one of a user's PIN devices",
		Tags:          []string{"Administration", "Authentication"},
		Security:      []map[string][]string{{"bearerAuth": {}}},
		DefaultStatus: http.StatusNoContent,
	}, h.AdminRevokeDevice)
}

// --- helpers ---

func callerUUID(ctx context.Context) (string, error) {
	userUUID, ok := ctxauth.GetUserUUID(ctx)
	if !ok || userUUID == "" {
		return "", huma.Error401Unauthorized("authentication required")
	}
	return userUUID, nil
}

func userAgentFromCtx(ctx context.Context) string {
	if r, ok := ctx.Value("http_request").(*http.Request); ok && r != nil {
		return r.UserAgent()
	}
	return ""
}

func mapPINError(err error) error {
	switch {
	case errors.Is(err, services.ErrPINLoginDisabled):
		return errcode.Forbidden(errcode.AuthPINLoginDisabled, "PIN sign-in is not available on this surface.")
	case errors.Is(err, services.ErrPINInvalid):
		return errcode.UnprocessableEntity(errcode.AuthPINInvalid, "The PIN must be exactly the configured number of digits.")
	case errors.Is(err, services.ErrPINTooWeak):
		return errcode.UnprocessableEntity(errcode.AuthPINTooWeak, "Choose a PIN that is not a repeated digit or a sequence.")
	case errors.Is(err, services.ErrPINUsernameRequired):
		return errcode.Conflict(errcode.AuthPINUsernameRequired, "The user needs a username before a PIN can be set: the username is the PIN sign-in identifier.")
	case errors.Is(err, services.ErrPINUsernameNotUnique):
		return errcode.Conflict(errcode.AuthPINUsernameNotUnique, "Another user has the same username; give this user a unique username before setting a PIN.")
	case errors.Is(err, services.ErrPINNotAllowed):
		return pinNotAllowed()
	case errors.Is(err, services.ErrPINNotSet):
		return errcode.Conflict(errcode.AuthPINNotSet, "The user has no PIN.")
	case errors.Is(err, services.ErrPINDeviceNotFound):
		return errcode.NotFound(errcode.AuthPINDeviceNotFound, "No enrolled PIN device with that id.")
	case errors.Is(err, services.ErrPINSessionForbidden):
		return errcode.Forbidden(errcode.AuthPINSessionForbidden, "Sign in with your password or another strong method to manage your PIN.")
	case errors.Is(err, services.ErrPINTargetAboveCaller):
		return errcode.Forbidden(errcode.UserTargetTierForbidden, "You cannot modify a user whose role is higher than your own")
	case errors.Is(err, services.ErrInvalidCredentials):
		return errcode.Unauthorized(errcode.AuthInvalidCredentials, "Invalid username, PIN or device")
	case errors.Is(err, services.ErrLoginDisabled):
		return errcode.Forbidden(errcode.AuthLoginDisabled, "Sign-in is temporarily disabled.")
	case errors.Is(err, services.ErrCountryBlocked):
		return errcode.Forbidden(errcode.AuthCountryBlocked, "Access from your country is not permitted.")
	case errors.Is(err, services.ErrAccountLocked):
		return lockoutError(services.RetryAfterFor(err))
	case errors.Is(err, services.ErrAuthPolicyUnavailable):
		return errcode.ServiceUnavailable(errcode.AuthPolicyUnavailable, "Sign-in policy is temporarily unavailable; try again shortly.")
	case errors.Is(err, iface.ErrUserNotFound):
		return huma.Error404NotFound("user not found")
	default:
		slog.Default().Error("unmapped pin auth error", slog.String("error", err.Error()))
		return errcode.Internal(errcode.AuthUnavailable, "The PIN operation failed for an unexpected reason. The failure has been logged for an administrator.")
	}
}

// pinNotAllowed is one answer for every eligibility refusal: on the
// management routes it is a 409 about the target, at sign-in (after a
// correct PIN) a 403 about the caller. The status is chosen by the route
// family, the code is the same.
func pinNotAllowed() error {
	return errcode.Conflict(errcode.AuthPINNotAllowed,
		"This user cannot have a PIN: privileged roles, service accounts and users without the auth.pin_login permission sign in with stronger methods.")
}
