package logging

import (
	"net/http"

	"github.com/danielgtaylor/huma/v2"
	"github.com/orkestra/backend/internal/core/logging/handlers"
)

// RegisterRoutes mounts the admin observability endpoints. LoggingModule calls
// it only inside the Tier-1 operator group protected by
// RequireSystemPermission("system.modules.admin"). The bearerAuth
// administrator scope below documents that boundary in OpenAPI.
//
// ADR-0005 Phase F — existing single-setting operations remain available
// while the workspace uses atomic batch updates and diagnostic overrides.
func RegisterRoutes(api huma.API, h *handlers.LogLevelHandler) {
	huma.Register(api, huma.Operation{
		OperationID: "admin-observability-loglevels-get",
		Method:      http.MethodGet,
		Path:        "/v1/admin/observability/log-levels",
		Summary:     "Get effective log levels",
		Description: "Returns the global threshold + per-module overrides + the catalog of known modules. ADR-0005 Phase F.",
		Tags:        []string{"Observability"},
		Security:    []map[string][]string{{"bearerAuth": {"administrator"}}},
	}, h.Get)

	huma.Register(api, huma.Operation{
		OperationID: "admin-observability-loglevels-preview",
		Method:      http.MethodGet,
		Path:        "/v1/admin/observability/log-levels/logs",
		Summary:     "Preview recent logs for one registered module",
		Description: "Returns at most 100 minimized, redacted events from a closed Loki query. Free-text messages may still contain personal data.",
		Tags:        []string{"Observability"},
		Security:    []map[string][]string{{"bearerAuth": {"administrator"}}},
	}, h.GetLogs)
	documentLogPreviewParameters(api.OpenAPI().Paths["/v1/admin/observability/log-levels/logs"].Get)

	huma.Register(api, huma.Operation{
		OperationID: "admin-observability-loglevels-apply",
		Method:      http.MethodPut,
		Path:        "/v1/admin/observability/log-levels",
		Summary:     "Apply the complete permanent log-level configuration",
		Tags:        []string{"Observability"},
		Security:    []map[string][]string{{"bearerAuth": {"administrator"}}},
	}, h.Apply)

	huma.Register(api, huma.Operation{
		OperationID: "admin-observability-loglevels-set-global",
		Method:      http.MethodPut,
		Path:        "/v1/admin/observability/log-levels/global",
		Summary:     "Set the global log level",
		Tags:        []string{"Observability"},
		Security:    []map[string][]string{{"bearerAuth": {"administrator"}}},
	}, h.SetGlobal)

	huma.Register(api, huma.Operation{
		OperationID: "admin-observability-loglevels-set-module",
		Method:      http.MethodPut,
		Path:        "/v1/admin/observability/log-levels/{module}",
		Summary:     "Set a per-module log level override",
		Tags:        []string{"Observability"},
		Security:    []map[string][]string{{"bearerAuth": {"administrator"}}},
	}, h.SetModule)

	huma.Register(api, huma.Operation{
		OperationID: "admin-observability-loglevels-unset-module",
		Method:      http.MethodDelete,
		Path:        "/v1/admin/observability/log-levels/{module}",
		Summary:     "Remove a per-module override (fall back to global)",
		Tags:        []string{"Observability"},
		Security:    []map[string][]string{{"bearerAuth": {"administrator"}}},
	}, h.UnsetModule)

	huma.Register(api, huma.Operation{
		OperationID: "admin-observability-loglevels-reset",
		Method:      http.MethodPost,
		Path:        "/v1/admin/observability/log-levels/reset",
		Summary:     "Revert global + per-module to boot env defaults",
		Tags:        []string{"Observability"},
		Security:    []map[string][]string{{"bearerAuth": {"administrator"}}},
	}, h.Reset)

	huma.Register(api, huma.Operation{
		OperationID: "admin-observability-loglevels-start-diagnostic",
		Method:      http.MethodPut,
		Path:        "/v1/admin/observability/log-levels/{module}/diagnostic",
		Summary:     "Start or replace a module diagnostic override",
		Tags:        []string{"Observability"},
		Security:    []map[string][]string{{"bearerAuth": {"administrator"}}},
	}, h.StartDiagnostic)

	huma.Register(api, huma.Operation{
		OperationID: "admin-observability-loglevels-stop-diagnostic",
		Method:      http.MethodDelete,
		Path:        "/v1/admin/observability/log-levels/{module}/diagnostic",
		Summary:     "Stop a module diagnostic override",
		Tags:        []string{"Observability"},
		Security:    []map[string][]string{{"bearerAuth": {"administrator"}}},
	}, h.StopDiagnostic)
}

// documentLogPreviewParameters replaces only the OpenAPI parameter schemas.
// Runtime decoding intentionally remains string-based so malformed or
// overflowing integers reach GetLogs and use its stable 400 error contract.
func documentLogPreviewParameters(operation *huma.Operation) {
	minLimit, maxLimit, maxSearchLength := float64(1), float64(100), 200
	for _, parameter := range operation.Parameters {
		description := parameter.Schema.Description
		switch parameter.Name {
		case "module":
			parameter.Required = true
			parameter.Schema = &huma.Schema{Type: huma.TypeString, Description: description}
		case "windowMinutes":
			parameter.Required = true
			parameter.Schema = &huma.Schema{
				Type:        huma.TypeInteger,
				Description: description,
				Enum:        []any{5, 15, 60},
			}
		case "level":
			parameter.Schema = &huma.Schema{
				Type:        huma.TypeString,
				Description: description,
				Enum:        []any{"debug", "info", "warn", "error"},
			}
		case "q":
			parameter.Schema = &huma.Schema{
				Type:        huma.TypeString,
				Description: description,
				MaxLength:   &maxSearchLength,
			}
		case "limit":
			parameter.Schema = &huma.Schema{
				Type:        huma.TypeInteger,
				Description: description,
				Default:     50,
				Minimum:     &minLimit,
				Maximum:     &maxLimit,
			}
		}
	}
}
