// Package handlers exposes the admin endpoints for the logging core
// module (ADR-0005 Phase F). All endpoints require administrator
// system permission — gated by RequireSystemPermission on the module's
// operator route group, not here, so this layer stays focused on translating
// between the HTTP envelope and the service interface.
package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"time"

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
	logger     *slog.Logger
}

func NewLogLevelHandler(svc *services.LogLevelService, logs logquery.Provider, grafanaURL string, logger *slog.Logger) *LogLevelHandler {
	if logs == nil {
		logs = logquery.New("", nil)
	}
	if logger == nil {
		logger = slog.Default()
	}
	return &LogLevelHandler{svc: svc, logs: logs, grafanaURL: grafanaURL, logger: logger}
}

// --- GET /v1/admin/observability/log-levels -----------------------------

type GetRequest struct{}

type GetResponse struct {
	Body models.AdminView `json:"-"`
}

func (h *LogLevelHandler) Get(ctx context.Context, _ *GetRequest) (*GetResponse, error) {
	if err := h.svc.Refresh(ctx); err != nil {
		return nil, h.mutationError(ctx, "load levels", err)
	}
	return &GetResponse{Body: h.view(ctx)}, nil
}

// --- POST /v1/admin/observability/log-levels/logs ----------------------

type GetLogsRequest struct {
	Body struct {
		// Numeric fields remain RawMessage so malformed and overflowing JSON
		// numbers reach the endpoint's stable 400 contract instead of Huma's
		// generic decoder response. routes.go supplies the OpenAPI-only schema.
		Module        string          `json:"module"`
		WindowMinutes json.RawMessage `json:"windowMinutes"`
		Level         string          `json:"level,omitempty"`
		Q             string          `json:"q,omitempty"`
		Limit         json.RawMessage `json:"limit,omitempty"`
	}
}

type GetLogsResponse struct {
	CacheControl string `header:"Cache-Control"`
	Body         struct {
		Events []models.LogEvent `json:"events"`
	}
}

func (h *LogLevelHandler) GetLogs(ctx context.Context, req *GetLogsRequest) (*GetLogsResponse, error) {
	windowMinutes, err := parseJSONInteger(req.Body.WindowMinutes, true)
	if err != nil {
		return nil, invalidLogPreviewFilters()
	}
	limit, err := parseJSONInteger(req.Body.Limit, false)
	if err != nil {
		return nil, invalidLogPreviewFilters()
	}
	if !h.logs.Status(ctx).Available {
		return nil, errcode.New(http.StatusServiceUnavailable, errcode.LoggingLogProviderUnavailable, "Log preview is unavailable on this deployment")
	}
	events, err := h.logs.Query(ctx, logquery.Query{
		Module:        req.Body.Module,
		WindowMinutes: windowMinutes,
		Level:         req.Body.Level,
		Text:          req.Body.Q,
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
	response := &GetLogsResponse{CacheControl: "private, no-store"}
	response.Body.Events = events
	return response, nil
}

func parseJSONInteger(raw json.RawMessage, required bool) (int, error) {
	if len(raw) == 0 || string(raw) == "null" {
		if required {
			return 0, errors.New("required integer missing")
		}
		return 0, nil
	}
	var value int
	if err := json.Unmarshal(raw, &value); err != nil {
		return 0, err
	}
	return value, nil
}

func invalidLogPreviewFilters() error {
	return errcode.New(http.StatusBadRequest, errcode.LoggingLogPreviewInvalid, "Invalid log preview filters")
}

// --- PUT /v1/admin/observability/log-levels ----------------------------

type ApplyRequest struct {
	Body struct {
		Global                    string            `json:"global" doc:"Global log level: debug | info | warn | error" example:"info"`
		PerModule                 map[string]string `json:"perModule" doc:"Complete desired map of per-module overrides"`
		ExpectedPermanentRevision int64             `json:"expectedPermanentRevision" doc:"permanentRevision from the snapshot being replaced"`
	}
}

func (h *LogLevelHandler) Apply(ctx context.Context, req *ApplyRequest) (*GetResponse, error) {
	global, err := models.Parse(req.Body.Global)
	if err != nil {
		return nil, invalidMutation()
	}
	if req.Body.ExpectedPermanentRevision < 0 {
		return nil, invalidMutation()
	}

	perModule := make(map[string]models.LogLevel, len(req.Body.PerModule))
	for module, rawLevel := range req.Body.PerModule {
		if !h.knownModule(module) {
			return nil, invalidMutation()
		}
		level, err := models.Parse(rawLevel)
		if err != nil {
			return nil, invalidMutation()
		}
		perModule[module] = level
	}

	err = h.svc.ApplyPermanent(ctx, models.PermanentConfigInput{
		Global:                    global,
		PerModule:                 perModule,
		ExpectedPermanentRevision: req.Body.ExpectedPermanentRevision,
	}, actor(ctx))
	if err != nil {
		return nil, h.mutationError(ctx, "apply permanent levels", err)
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
		return nil, invalidMutation()
	}
	if err := h.svc.SetGlobal(ctx, lvl, actor(ctx)); err != nil {
		return nil, h.mutationError(ctx, "set global level", err)
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
		return nil, invalidMutation()
	}
	if !h.knownModule(req.Module) {
		return nil, invalidMutation()
	}
	lvl, err := models.Parse(req.Body.Level)
	if err != nil {
		return nil, invalidMutation()
	}
	if err := h.svc.SetModule(ctx, req.Module, lvl, actor(ctx)); err != nil {
		return nil, h.mutationError(ctx, "set module level", err)
	}
	return &GetResponse{Body: h.view(ctx)}, nil
}

// --- DELETE /v1/admin/observability/log-levels/{module} ----------------

type UnsetModuleRequest struct {
	Module string `path:"module"`
}

func (h *LogLevelHandler) UnsetModule(ctx context.Context, req *UnsetModuleRequest) (*GetResponse, error) {
	if !h.knownModule(req.Module) {
		return nil, invalidMutation()
	}
	if err := h.svc.UnsetModule(ctx, req.Module, actor(ctx)); err != nil {
		return nil, h.mutationError(ctx, "unset module level", err)
	}
	return &GetResponse{Body: h.view(ctx)}, nil
}

// --- POST /v1/admin/observability/log-levels/reset ---------------------

type ResetRequest struct{}

func (h *LogLevelHandler) Reset(ctx context.Context, _ *ResetRequest) (*GetResponse, error) {
	if err := h.svc.ResetToEnv(ctx, actor(ctx)); err != nil {
		return nil, h.mutationError(ctx, "reset levels", err)
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
		return nil, invalidMutation()
	}
	level, err := models.Parse(req.Body.Level)
	if err != nil {
		return nil, invalidMutation()
	}
	expiresAt, err := diagnosticExpiry(req.Body.DurationMinutes)
	if err != nil {
		return nil, invalidMutation()
	}
	if err := h.svc.StartDiagnostic(ctx, req.Module, level, expiresAt, actor(ctx)); err != nil {
		return nil, h.mutationError(ctx, "start diagnostic", err)
	}
	return &GetResponse{Body: h.view(ctx)}, nil
}

// --- DELETE /v1/admin/observability/log-levels/{module}/diagnostic -----

type StopDiagnosticRequest struct {
	Module string `path:"module" doc:"Registered module name"`
}

func (h *LogLevelHandler) StopDiagnostic(ctx context.Context, req *StopDiagnosticRequest) (*GetResponse, error) {
	if !h.knownModule(req.Module) {
		return nil, invalidMutation()
	}
	if err := h.svc.StopDiagnostic(ctx, req.Module, actor(ctx)); err != nil {
		return nil, h.mutationError(ctx, "stop diagnostic", err)
	}
	return &GetResponse{Body: h.view(ctx)}, nil
}

func invalidMutation() error {
	return errcode.New(http.StatusBadRequest, errcode.LoggingMutationInvalid, "Invalid logging operation")
}

func (h *LogLevelHandler) mutationError(ctx context.Context, operation string, err error) error {
	if errors.Is(err, services.ErrConfigConflict) || errors.Is(err, services.ErrWriteConflict) {
		return errcode.New(http.StatusConflict, errcode.LoggingConfigConflict, "Logging configuration changed; reload and retry")
	}
	h.logger.ErrorContext(ctx, "logging mutation failed",
		slog.String("operation", operation),
		slog.String("error", err.Error()))
	return errcode.New(http.StatusInternalServerError, errcode.LoggingPersistenceFailed, "Logging configuration update failed")
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
