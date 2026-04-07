package module

import (
	"net/http"

	"github.com/danielgtaylor/huma/v2"
)

// RegisterAdminModuleRoutes registers the module management admin API.
// Should be called on a protected Huma API with administrator role guard.
func RegisterAdminModuleRoutes(api huma.API, handler *ModuleAdminHandler) {
	huma.Register(api, huma.Operation{
		OperationID: "list-modules",
		Method:      http.MethodGet,
		Path:        "/v1/admin/modules",
		Summary:     "List all modules",
		Description: "Returns all registered modules with their configuration and status",
		Tags:        []string{"Admin - Modules"},
		Security:    []map[string][]string{{"bearerAuth": {}}},
	}, handler.ListModules)

	huma.Register(api, huma.Operation{
		OperationID: "get-module",
		Method:      http.MethodGet,
		Path:        "/v1/admin/modules/{name}",
		Summary:     "Get module configuration",
		Description: "Returns a single module's configuration. Secrets are never returned, only indicators of whether values are stored.",
		Tags:        []string{"Admin - Modules"},
		Security:    []map[string][]string{{"bearerAuth": {}}},
	}, handler.GetModule)

	huma.Register(api, huma.Operation{
		OperationID: "update-module",
		Method:      http.MethodPatch,
		Path:        "/v1/admin/modules/{name}",
		Summary:     "Update module configuration",
		Description: "Enable/disable a module and update its configuration. Core modules cannot be disabled. Secret values are encrypted before storage.",
		Tags:        []string{"Admin - Modules"},
		Security:    []map[string][]string{{"bearerAuth": {}}},
	}, handler.UpdateModule)
}
