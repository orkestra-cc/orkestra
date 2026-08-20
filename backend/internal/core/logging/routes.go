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
		Method:      http.MethodPost,
		Path:        "/v1/admin/observability/log-levels/logs",
		Summary:     "Preview recent logs for one registered module",
		Description: "Returns at most 100 minimized, redacted events from a closed Loki query. Free-text messages may still contain personal data.",
		Tags:        []string{"Observability"},
		Security:    []map[string][]string{{"bearerAuth": {"administrator"}}},
		// Runtime numeric decoding is deliberately handled by the endpoint so
		// malformed and overflowing values retain its stable 400 error code.
		SkipValidateBody: true,
	}, h.GetLogs)
	documentLogPreviewBody(api.OpenAPI().Paths["/v1/admin/observability/log-levels/logs"].Post)

	huma.Register(api, huma.Operation{
		OperationID: "admin-observability-loglevels-apply",
		Method:      http.MethodPut,
		Path:        "/v1/admin/observability/log-levels",
		Summary:     "Apply the complete permanent log-level configuration",
		Tags:        []string{"Observability"},
		Security:    []map[string][]string{{"bearerAuth": {"administrator"}}},
	}, h.Apply)
	documentApplyBody(api.OpenAPI().Paths["/v1/admin/observability/log-levels"].Put)

	huma.Register(api, huma.Operation{
		OperationID: "admin-observability-loglevels-set-global",
		Method:      http.MethodPut,
		Path:        "/v1/admin/observability/log-levels/global",
		Summary:     "Set the global log level",
		Tags:        []string{"Observability"},
		Security:    []map[string][]string{{"bearerAuth": {"administrator"}}},
	}, h.SetGlobal)
	documentLevelBody(api.OpenAPI().Paths["/v1/admin/observability/log-levels/global"].Put)

	huma.Register(api, huma.Operation{
		OperationID: "admin-observability-loglevels-set-module",
		Method:      http.MethodPut,
		Path:        "/v1/admin/observability/log-levels/{module}",
		Summary:     "Set a per-module log level override",
		Tags:        []string{"Observability"},
		Security:    []map[string][]string{{"bearerAuth": {"administrator"}}},
	}, h.SetModule)
	documentLevelBody(api.OpenAPI().Paths["/v1/admin/observability/log-levels/{module}"].Put)

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
	documentDiagnosticBody(api.OpenAPI().Paths["/v1/admin/observability/log-levels/{module}/diagnostic"].Put)

	huma.Register(api, huma.Operation{
		OperationID: "admin-observability-loglevels-stop-diagnostic",
		Method:      http.MethodDelete,
		Path:        "/v1/admin/observability/log-levels/{module}/diagnostic",
		Summary:     "Stop a module diagnostic override",
		Tags:        []string{"Observability"},
		Security:    []map[string][]string{{"bearerAuth": {"administrator"}}},
	}, h.StopDiagnostic)
}

// The helpers below replace only the published request schemas after Huma has
// compiled its runtime decoder/validator. This documents closed enums and
// bounds without changing established runtime error semantics.
func documentLogPreviewBody(operation *huma.Operation) {
	minLimit, maxLimit, maxSearchLength := float64(1), float64(100), 200
	setJSONBodySchema(operation, &huma.Schema{
		Type:     huma.TypeObject,
		Required: []string{"module", "windowMinutes"},
		Properties: map[string]*huma.Schema{
			"module":        {Type: huma.TypeString, Description: "Registered module name"},
			"windowMinutes": {Type: huma.TypeInteger, Enum: []any{5, 15, 60}, Description: "Closed preview window in minutes"},
			"level":         logLevelSchema("Optional exact level"),
			"q":             {Type: huma.TypeString, MaxLength: &maxSearchLength, Description: "Optional literal text filter"},
			"limit":         {Type: huma.TypeInteger, Default: 50, Minimum: &minLimit, Maximum: &maxLimit, Description: "Maximum returned events"},
		},
	})
}

func documentApplyBody(operation *huma.Operation) {
	minimum := float64(0)
	setJSONBodySchema(operation, &huma.Schema{
		Type:     huma.TypeObject,
		Required: []string{"global", "perModule", "expectedPermanentRevision"},
		Properties: map[string]*huma.Schema{
			"global": logLevelSchema("Global permanent threshold"),
			"perModule": {
				Type:                 huma.TypeObject,
				Description:          "Complete per-module permanent override map",
				AdditionalProperties: logLevelSchema("Per-module permanent threshold"),
			},
			"expectedPermanentRevision": {
				Type:        huma.TypeInteger,
				Minimum:     &minimum,
				Description: "Permanent revision from the edited snapshot",
			},
		},
	})
}

func documentLevelBody(operation *huma.Operation) {
	setJSONBodySchema(operation, &huma.Schema{
		Type:       huma.TypeObject,
		Required:   []string{"level"},
		Properties: map[string]*huma.Schema{"level": logLevelSchema("Permanent threshold")},
	})
}

func documentDiagnosticBody(operation *huma.Operation) {
	setJSONBodySchema(operation, &huma.Schema{
		Type:     huma.TypeObject,
		Required: []string{"level"},
		Properties: map[string]*huma.Schema{
			"level":           logLevelSchema("Temporary diagnostic threshold"),
			"durationMinutes": {Type: huma.TypeInteger, Enum: []any{15, 60, 240}, Description: "Omit for no expiry"},
		},
	})
}

func logLevelSchema(description string) *huma.Schema {
	return &huma.Schema{
		Type:        huma.TypeString,
		Description: description,
		Enum:        []any{"debug", "info", "warn", "error"},
	}
}

func setJSONBodySchema(operation *huma.Operation, schema *huma.Schema) {
	if operation == nil || operation.RequestBody == nil {
		return
	}
	media := operation.RequestBody.Content["application/json"]
	if media == nil {
		return
	}
	operation.RequestBody.Required = true
	media.Schema = schema
}
