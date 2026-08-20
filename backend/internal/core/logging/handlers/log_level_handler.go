// Package handlers exposes the admin endpoints for the logging core
// module (ADR-0005 Phase F). All endpoints require administrator
// system permission — gated by RequireSystemPermission on the module's
// operator route group, not here, so this layer stays focused on translating
// between the HTTP envelope and the service interface.
package handlers

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/orkestra/backend/internal/core/logging/logquery"
	"github.com/orkestra/backend/internal/core/logging/models"
	"github.com/orkestra/backend/internal/core/logging/services"
	"github.com/orkestra/backend/internal/shared/errcode"
	"github.com/orkestra/backend/pkg/sdk/ctxauth"
)

// LogLevelHandler serves the /v1/admin/observability/log-levels surface.
// Construction takes the service that owns the atomic snapshot; this
// handler is stateless.
type LogLevelHandler struct {
	svc        *services.LogLevelService
	logs       logquery.Provider
	grafanaURL string
}

func NewLogLevelHandler(svc *services.LogLevelService, logs logquery.Provider, grafanaURL string) *LogLevelHandler {
	if logs == nil {
		logs = logquery.New("", nil)
	}
	return &LogLevelHandler{svc: svc, logs: logs, grafanaURL: grafanaURL}
}

// --- GET /v1/admin/observability/log-levels -----------------------------

type GetRequest struct{}

type GetResponse struct {
	Body models.AdminView `json:"-"`
}

func (h *LogLevelHandler) Get(ctx context.Context, _ *GetRequest) (*GetResponse, error) {
	return &GetResponse{Body: h.view(ctx)}, nil
}

// --- GET /v1/admin/observability/log-levels/logs -----------------------

type GetLogsRequest struct {
	// Validation lives in logquery.Client rather than Huma schema tags so every
	// rejected filter follows this endpoint's stable 400 error-code contract;
	// Huma's automatic schema failures use 422.
	Module        string `query:"module" doc:"Required registered module name"`
	WindowMinutes string `query:"windowMinutes" doc:"Required closed preview window: 5, 15, or 60 minutes"`
	Level         string `query:"level" doc:"Optional level: debug, info, warn, or error"`
	Q             string `query:"q" doc:"Optional literal text filter, at most 200 characters"`
	Limit         string `query:"limit" default:"50" doc:"Maximum events; values above 100 are clamped"`
}

type GetLogsResponse struct {
	Body struct {
		Events []models.LogEvent `json:"events"`
	}
}

func (h *LogLevelHandler) GetLogs(ctx context.Context, req *GetLogsRequest) (*GetLogsResponse, error) {
	windowMinutes, err := strconv.Atoi(req.WindowMinutes)
	if err != nil {
		return nil, invalidLogPreviewFilters()
	}
	limit := 0
	if req.Limit != "" {
		limit, err = strconv.Atoi(req.Limit)
		if err != nil {
			return nil, invalidLogPreviewFilters()
		}
	}
	if !h.logs.Status(ctx).Available {
		return nil, errcode.New(http.StatusServiceUnavailable, errcode.LoggingLogProviderUnavailable, "Log preview is unavailable on this deployment")
	}
	events, err := h.logs.Query(ctx, logquery.Query{
		Module:        req.Module,
		WindowMinutes: windowMinutes,
		Level:         req.Level,
		Text:          req.Q,
		Limit:         limit,
	})
	if err != nil {
		switch {
		case errors.Is(err, logquery.ErrUnavailable):
			return nil, errcode.New(http.StatusServiceUnavailable, errcode.LoggingLogProviderUnavailable, "Log preview is unavailable on this deployment")
		case errors.Is(err, logquery.ErrInvalidQuery):
			return nil, invalidLogPreviewFilters()
		case errors.Is(err, logquery.ErrTimeout):
			return nil, errcode.New(http.StatusGatewayTimeout, errcode.LoggingLogProviderTimeout, "Log provider timed out")
		default:
			return nil, errcode.New(http.StatusBadGateway, errcode.LoggingLogProviderFailed, "Log provider request failed")
		}
	}
	response := &GetLogsResponse{}
	response.Body.Events = events
	return response, nil
}

func invalidLogPreviewFilters() error {
	return errcode.New(http.StatusBadRequest, errcode.LoggingLogPreviewInvalid, "Invalid log preview filters")
}

// --- PUT /v1/admin/observability/log-levels ----------------------------

type ApplyRequest struct {
	Body struct {
		Global            string            `json:"global" doc:"Global log level: debug | info | warn | error" example:"info"`
		PerModule         map[string]string `json:"perModule" doc:"Complete desired map of per-module overrides"`
		ExpectedUpdatedAt time.Time         `json:"expectedUpdatedAt" doc:"updatedAt from the snapshot being replaced"`
	}
}

func (h *LogLevelHandler) Apply(ctx context.Context, req *ApplyRequest) (*GetResponse, error) {
	global, err := models.Parse(req.Body.Global)
	if err != nil {
		return nil, huma.Error400BadRequest("invalid global level", err)
	}

	perModule := make(map[string]models.LogLevel, len(req.Body.PerModule))
	for module, rawLevel := range req.Body.PerModule {
		if !h.knownModule(module) {
			return nil, huma.Error400BadRequest(fmt.Sprintf("unknown logging module %q", module))
		}
		level, err := models.Parse(rawLevel)
		if err != nil {
			return nil, huma.Error400BadRequest(fmt.Sprintf("invalid level for module %q", module), err)
		}
		perModule[module] = level
	}

	err = h.svc.ApplyPermanent(ctx, models.PermanentConfigInput{
		Global:            global,
		PerModule:         perModule,
		ExpectedUpdatedAt: req.Body.ExpectedUpdatedAt,
	}, actor(ctx))
	if errors.Is(err, services.ErrConfigConflict) {
		return nil, huma.Error409Conflict("logging configuration changed; reload before applying", err)
	}
	if err != nil {
		return nil, huma.Error500InternalServerError("persist failed", err)
	}
	return &GetResponse{Body: h.view(ctx)}, nil
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
	return &GetResponse{Body: h.view(ctx)}, nil
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
	return &GetResponse{Body: h.view(ctx)}, nil
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
	return &GetResponse{Body: h.view(ctx)}, nil
}

// --- POST /v1/admin/observability/log-levels/reset ---------------------

type ResetRequest struct{}

func (h *LogLevelHandler) Reset(ctx context.Context, _ *ResetRequest) (*GetResponse, error) {
	if err := h.svc.ResetToEnv(ctx, actor(ctx)); err != nil {
		return nil, huma.Error500InternalServerError("reset failed", err)
	}
	return &GetResponse{Body: h.view(ctx)}, nil
}

// --- PUT /v1/admin/observability/log-levels/{module}/diagnostic --------

type StartDiagnosticRequest struct {
	Module string `path:"module" doc:"Registered module name"`
	Body   struct {
		Level           string `json:"level" doc:"Temporary log level override" example:"debug"`
		DurationMinutes *int   `json:"durationMinutes,omitempty" doc:"Expiry duration: 15, 60, or 240 minutes; omit for no expiry"`
	}
}

func (h *LogLevelHandler) StartDiagnostic(ctx context.Context, req *StartDiagnosticRequest) (*GetResponse, error) {
	if !h.knownModule(req.Module) {
		return nil, huma.Error400BadRequest(fmt.Sprintf("unknown logging module %q", req.Module))
	}
	level, err := models.Parse(req.Body.Level)
	if err != nil {
		return nil, huma.Error400BadRequest("invalid diagnostic level", err)
	}
	expiresAt, err := diagnosticExpiry(req.Body.DurationMinutes)
	if err != nil {
		return nil, huma.Error400BadRequest("invalid diagnostic duration", err)
	}
	if err := h.svc.StartDiagnostic(ctx, req.Module, level, expiresAt, actor(ctx)); err != nil {
		return nil, huma.Error500InternalServerError("persist failed", err)
	}
	return &GetResponse{Body: h.view(ctx)}, nil
}

// --- DELETE /v1/admin/observability/log-levels/{module}/diagnostic -----

type StopDiagnosticRequest struct {
	Module string `path:"module" doc:"Registered module name"`
}

func (h *LogLevelHandler) StopDiagnostic(ctx context.Context, req *StopDiagnosticRequest) (*GetResponse, error) {
	if !h.knownModule(req.Module) {
		return nil, huma.Error400BadRequest(fmt.Sprintf("unknown logging module %q", req.Module))
	}
	if err := h.svc.StopDiagnostic(ctx, req.Module, actor(ctx)); err != nil {
		return nil, huma.Error500InternalServerError("persist failed", err)
	}
	return &GetResponse{Body: h.view(ctx)}, nil
}

func (h *LogLevelHandler) knownModule(name string) bool {
	if name == "" {
		return false
	}
	for _, entry := range h.svc.View().Modules {
		if entry.Name == name {
			return true
		}
	}
	return false
}

func (h *LogLevelHandler) view(ctx context.Context) models.AdminView {
	view := h.svc.View()
	view.LogProvider = models.LogProviderStatus{
		Available:  h.logs.Status(ctx).Available,
		GrafanaURL: h.grafanaURL,
	}
	return view
}

func diagnosticExpiry(durationMinutes *int) (*time.Time, error) {
	if durationMinutes == nil {
		return nil, nil
	}
	var duration time.Duration
	switch *durationMinutes {
	case 15:
		duration = 15 * time.Minute
	case 60:
		duration = time.Hour
	case 240:
		duration = 4 * time.Hour
	default:
		return nil, fmt.Errorf("durationMinutes must be 15, 60, 240, or omitted")
	}
	expiresAt := time.Now().UTC().Add(duration)
	return &expiresAt, nil
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
