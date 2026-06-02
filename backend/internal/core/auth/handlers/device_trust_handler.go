// Package handlers — device-trust self-service endpoints.
//
// Section C item #3 of the 2026-04-24 auth roadmap. Endpoints let the
// signed-in user inspect and revoke their "remember this device 30d"
// grants. Trust is *granted* from the login-verify endpoints in
// mfa_handler.go / webauthn_handler.go (so the grant is gated behind
// a verified MFA proof); these handlers only read and revoke.
package handlers

import (
	"context"
	"net/http"
	"time"

	"github.com/danielgtaylor/huma/v2"

	"github.com/orkestra/backend/internal/core/auth/models"
	"github.com/orkestra/backend/internal/core/auth/services"
	"github.com/orkestra/backend/pkg/sdk/ctxauth"
)

// DeviceTrustHandler exposes the three /v1/auth/me/devices/trust
// endpoints: list, revoke-one, revoke-all. All three require an
// authenticated session (the parent router applies RequireGlobal).
type DeviceTrustHandler struct {
	svc services.DeviceTrustService
}

// NewDeviceTrustHandler builds the handler. A nil service is valid —
// each endpoint degrades to an empty list / 204 no-op so tests and
// minimal deploys that don't wire the service don't crash the router.
func NewDeviceTrustHandler(svc services.DeviceTrustService) *DeviceTrustHandler {
	return &DeviceTrustHandler{svc: svc}
}

type trustedDevicePublic struct {
	UUID         string    `json:"uuid"`
	DeviceID     string    `json:"deviceId"`
	DeviceName   string    `json:"deviceName,omitempty"`
	Platform     string    `json:"platform,omitempty"`
	IPAddress    string    `json:"ipAddress,omitempty"`
	GrantedAMR   string    `json:"grantedAmr,omitempty"`
	TrustedAt    time.Time `json:"trustedAt"`
	TrustedUntil time.Time `json:"trustedUntil"`
	LastUsedAt   time.Time `json:"lastUsedAt,omitempty"`
}

type listTrustedDevicesResponse struct {
	Body struct {
		Devices []trustedDevicePublic `json:"devices"`
	}
}

// List returns every active trust grant the current user holds.
func (h *DeviceTrustHandler) List(ctx context.Context, _ *struct{}) (*listTrustedDevicesResponse, error) {
	userUUID, ok := ctxauth.GetUserUUID(ctx)
	if !ok || userUUID == "" {
		return nil, huma.Error401Unauthorized("authentication required")
	}
	resp := &listTrustedDevicesResponse{}
	resp.Body.Devices = []trustedDevicePublic{}
	if h.svc == nil {
		return resp, nil
	}
	rows, err := h.svc.ListActive(ctx, userUUID)
	if err != nil {
		return nil, huma.Error500InternalServerError("failed to list trusted devices")
	}
	for _, row := range rows {
		resp.Body.Devices = append(resp.Body.Devices, trustedDeviceDoc(row))
	}
	return resp, nil
}

type revokeTrustedDeviceRequest struct {
	DeviceID string `path:"deviceId"`
}

type revokeTrustedDeviceResponse struct{}

// RevokeOne drops trust for one device. Idempotent — a 204 is returned
// even if the device was never trusted or has already been revoked, so
// the frontend can call this from a confirmation modal without
// checking the list first.
func (h *DeviceTrustHandler) RevokeOne(ctx context.Context, req *revokeTrustedDeviceRequest) (*revokeTrustedDeviceResponse, error) {
	userUUID, ok := ctxauth.GetUserUUID(ctx)
	if !ok || userUUID == "" {
		return nil, huma.Error401Unauthorized("authentication required")
	}
	if req.DeviceID == "" {
		return nil, huma.Error400BadRequest("deviceId is required")
	}
	if h.svc != nil {
		if err := h.svc.RevokeByDevice(ctx, userUUID, req.DeviceID, models.DeviceTrustRevokedByUser); err != nil {
			return nil, huma.Error500InternalServerError("failed to revoke trust")
		}
	}
	return &revokeTrustedDeviceResponse{}, nil
}

type revokeAllTrustedDevicesResponse struct{}

// RevokeAll drops every active trust grant the current user holds.
// Same idempotency contract as RevokeOne.
func (h *DeviceTrustHandler) RevokeAll(ctx context.Context, _ *struct{}) (*revokeAllTrustedDevicesResponse, error) {
	userUUID, ok := ctxauth.GetUserUUID(ctx)
	if !ok || userUUID == "" {
		return nil, huma.Error401Unauthorized("authentication required")
	}
	if h.svc != nil {
		if err := h.svc.RevokeAllByUser(ctx, userUUID, models.DeviceTrustRevokedByUser); err != nil {
			return nil, huma.Error500InternalServerError("failed to revoke trusts")
		}
	}
	return &revokeAllTrustedDevicesResponse{}, nil
}

func trustedDeviceDoc(d *models.DeviceTrustDoc) trustedDevicePublic {
	return trustedDevicePublic{
		UUID:         d.UUID,
		DeviceID:     d.DeviceID,
		DeviceName:   d.DeviceName,
		Platform:     d.Platform,
		IPAddress:    d.IPAddress,
		GrantedAMR:   d.GrantedAMR,
		TrustedAt:    d.TrustedAt,
		TrustedUntil: d.TrustedUntil,
		LastUsedAt:   d.LastUsedAt,
	}
}

// RegisterRoutes wires the three endpoints. All share the
// RequireGlobal() gate applied at the parent router level. mount controls
// the path/operation-ID prefix for the ADR-0003 audience-split surfaces.
func (h *DeviceTrustHandler) RegisterRoutes(api huma.API, mount RouteMount) {
	huma.Register(api, huma.Operation{
		OperationID: mount.OpIDPrefix + "list-trusted-devices",
		Method:      http.MethodGet,
		Path:        "/v1/auth" + mount.PathPrefix + "/me/devices/trust",
		Summary:     "List trusted devices",
		Description: "Returns the devices the caller has opted into skipping the MFA prompt on login. Expired grants are reaped by the backend and don't appear.",
		Tags:        []string{"Auth - Device Trust"},
		Security:    []map[string][]string{{"bearerAuth": {}}},
	}, h.List)

	huma.Register(api, huma.Operation{
		OperationID:   mount.OpIDPrefix + "revoke-trusted-device",
		Method:        http.MethodDelete,
		Path:          "/v1/auth" + mount.PathPrefix + "/me/devices/trust/{deviceId}",
		Summary:       "Revoke one trusted device",
		Description:   "Drops the trust grant for a single device. Idempotent — returns 204 even when the device was never trusted.",
		Tags:          []string{"Auth - Device Trust"},
		Security:      []map[string][]string{{"bearerAuth": {}}},
		DefaultStatus: http.StatusNoContent,
	}, h.RevokeOne)

	huma.Register(api, huma.Operation{
		OperationID:   mount.OpIDPrefix + "revoke-all-trusted-devices",
		Method:        http.MethodDelete,
		Path:          "/v1/auth" + mount.PathPrefix + "/me/devices/trust",
		Summary:       "Revoke all trusted devices",
		Description:   "Drops every active trust grant the caller holds. The next login from any device will require completing MFA.",
		Tags:          []string{"Auth - Device Trust"},
		Security:      []map[string][]string{{"bearerAuth": {}}},
		DefaultStatus: http.StatusNoContent,
	}, h.RevokeAll)
}
