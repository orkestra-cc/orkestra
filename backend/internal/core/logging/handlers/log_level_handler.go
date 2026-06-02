// Package handlers exposes the admin endpoints for the logging core
// module (ADR-0005 Phase F). All endpoints require administrator
// system role — gated by RequireRole on the route registration, not
// here, so this layer stays focused on translating between the HTTP
// envelope and the service interface.
package handlers

import (
	"context"

	"github.com/danielgtaylor/huma/v2"
	"github.com/orkestra/backend/internal/core/logging/models"
	"github.com/orkestra/backend/internal/core/logging/services"
	"github.com/orkestra/backend/pkg/sdk/ctxauth"
)

// LogLevelHandler serves the /v1/admin/observability/log-levels surface.
// Construction takes the service that owns the atomic snapshot; this
// handler is stateless.
type LogLevelHandler struct {
	svc *services.LogLevelService
}

func NewLogLevelHandler(svc *services.LogLevelService) *LogLevelHandler {
	return &LogLevelHandler{svc: svc}
}

// --- GET /v1/admin/observability/log-levels -----------------------------

type GetRequest struct{}

type GetResponse struct {
	Body models.AdminView `json:"-"`
}

func (h *LogLevelHandler) Get(_ context.Context, _ *GetRequest) (*GetResponse, error) {
	return &GetResponse{Body: h.svc.View()}, nil
}

// --- PUT /v1/admin/observability/log-levels/global ---------------------

type SetGlobalRequest struct {
	Body struct {
		Level string `json:"level" doc:"Global log level: debug | info | warn | error" example:"info"`
	}
}

func (h *LogLevelHandler) SetGlobal(ctx context.Context, req *SetGlobalRequest) (*GetResponse, error) {
	lvl, err := models.Parse(req.Body.Level)
	if err != nil {
		return nil, huma.Error400BadRequest("invalid level", err)
	}
	if err := h.svc.SetGlobal(ctx, lvl, actor(ctx)); err != nil {
		return nil, huma.Error500InternalServerError("persist failed", err)
	}
	return &GetResponse{Body: h.svc.View()}, nil
}

// --- PUT /v1/admin/observability/log-levels/{module} -------------------

type SetModuleRequest struct {
	Module string `path:"module" doc:"Module name (lowercase, matches deps.Logger module attribute)"`
	Body   struct {
		Level string `json:"level" doc:"Per-module log level override"`
	}
}

func (h *LogLevelHandler) SetModule(ctx context.Context, req *SetModuleRequest) (*GetResponse, error) {
	if req.Module == "" {
		return nil, huma.Error400BadRequest("module name required")
	}
	lvl, err := models.Parse(req.Body.Level)
	if err != nil {
		return nil, huma.Error400BadRequest("invalid level", err)
	}
	if err := h.svc.SetModule(ctx, req.Module, lvl, actor(ctx)); err != nil {
		return nil, huma.Error500InternalServerError("persist failed", err)
	}
	return &GetResponse{Body: h.svc.View()}, nil
}

// --- DELETE /v1/admin/observability/log-levels/{module} ----------------

type UnsetModuleRequest struct {
	Module string `path:"module"`
}

func (h *LogLevelHandler) UnsetModule(ctx context.Context, req *UnsetModuleRequest) (*GetResponse, error) {
	if req.Module == "" {
		return nil, huma.Error400BadRequest("module name required")
	}
	if err := h.svc.UnsetModule(ctx, req.Module, actor(ctx)); err != nil {
		return nil, huma.Error500InternalServerError("persist failed", err)
	}
	return &GetResponse{Body: h.svc.View()}, nil
}

// --- POST /v1/admin/observability/log-levels/reset ---------------------

type ResetRequest struct{}

func (h *LogLevelHandler) Reset(ctx context.Context, _ *ResetRequest) (*GetResponse, error) {
	if err := h.svc.ResetToEnv(ctx, actor(ctx)); err != nil {
		return nil, huma.Error500InternalServerError("reset failed", err)
	}
	return &GetResponse{Body: h.svc.View()}, nil
}

// actor pulls the acting user identifier from the request context.
// Falls back to "unknown" when the call originates outside the auth
// flow (test, internal, dev-token endpoint).
func actor(ctx context.Context) string {
	if uuid, ok := ctxauth.GetUserUUID(ctx); ok && uuid != "" {
		return uuid
	}
	return "unknown"
}
