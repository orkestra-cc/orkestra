package llm

import (
	"net/http"

	"github.com/danielgtaylor/huma/v2"
	"github.com/orkestra/backend/internal/core/llm/handlers"
)

// The bearerAuth scopes below document the operator boundary in OpenAPI;
// nothing reads them. Authorization is the middleware Module.RegisterRoutes
// mounts around each group: RequireInternalTenant plus a permission (and
// RequireStepUp where noted).
var (
	adminScope = []map[string][]string{{"bearerAuth": {"administrator"}}}
	selfScope  = []map[string][]string{{"bearerAuth": {}}}
	adminTags  = []string{"LLM Admin"}
)

// RegisterAdminReadRoutes mounts every GET under /v1/admin/llm. Gated by
// llm.admin.read, so a read-only operator still sees the configuration.
func RegisterAdminReadRoutes(api huma.API, h *handlers.AdminHandler) {
	huma.Register(api, huma.Operation{OperationID: "admin-llm-credentials-list", Method: http.MethodGet, Path: "/v1/admin/llm/credentials",
		Summary: "List LLM credentials of the current organization (secrets redacted)", Tags: adminTags, Security: adminScope}, h.ListCredentials)
	huma.Register(api, huma.Operation{OperationID: "admin-llm-credentials-get", Method: http.MethodGet, Path: "/v1/admin/llm/credentials/{uuid}",
		Summary: "Get one LLM credential (secret redacted)", Tags: adminTags, Security: adminScope}, h.GetCredential)
	huma.Register(api, huma.Operation{OperationID: "admin-llm-models-list", Method: http.MethodGet, Path: "/v1/admin/llm/models",
		Summary: "List configured LLM models with their grants", Tags: adminTags, Security: adminScope}, h.ListModels)
	huma.Register(api, huma.Operation{OperationID: "admin-llm-models-get", Method: http.MethodGet, Path: "/v1/admin/llm/models/{uuid}",
		Summary: "Get one configured LLM model with its grants", Tags: adminTags, Security: adminScope}, h.GetModel)
}

// RegisterCredentialStepUpRoutes mounts the credential writes that touch the
// API key or remove the credential: create, rotate, delete. Gated by
// llm.credentials.admin plus RequireStepUp.
func RegisterCredentialStepUpRoutes(api huma.API, h *handlers.AdminHandler) {
	huma.Register(api, huma.Operation{OperationID: "admin-llm-credentials-create", Method: http.MethodPost, Path: "/v1/admin/llm/credentials",
		Summary: "Create an LLM credential (the API key is write-only)", Tags: adminTags, Security: adminScope, DefaultStatus: http.StatusCreated}, h.CreateCredential)
	huma.Register(api, huma.Operation{OperationID: "admin-llm-credentials-rotate", Method: http.MethodPost, Path: "/v1/admin/llm/credentials/{uuid}/rotate",
		Summary: "Replace the API key of a credential", Tags: adminTags, Security: adminScope}, h.RotateCredential)
	huma.Register(api, huma.Operation{OperationID: "admin-llm-credentials-delete", Method: http.MethodDelete, Path: "/v1/admin/llm/credentials/{uuid}",
		Summary: "Delete a credential (409 while an active model uses it)", Tags: adminTags, Security: adminScope, DefaultStatus: http.StatusNoContent}, h.DeleteCredential)
}

// RegisterCredentialPatchRoutes mounts the credential PATCH (rename,
// re-point, enable/disable; never the key). Gated by llm.credentials.admin,
// no step-up.
func RegisterCredentialPatchRoutes(api huma.API, h *handlers.AdminHandler) {
	huma.Register(api, huma.Operation{OperationID: "admin-llm-credentials-patch", Method: http.MethodPatch, Path: "/v1/admin/llm/credentials/{uuid}",
		Summary: "Rename, re-point or disable a credential", Tags: adminTags, Security: adminScope}, h.PatchCredential)
}

// RegisterModelWriteRoutes mounts model create, patch and delete. Gated by
// llm.models.admin, no step-up.
func RegisterModelWriteRoutes(api huma.API, h *handlers.AdminHandler) {
	huma.Register(api, huma.Operation{OperationID: "admin-llm-models-create", Method: http.MethodPost, Path: "/v1/admin/llm/models",
		Summary: "Configure a model", Tags: adminTags, Security: adminScope, DefaultStatus: http.StatusCreated}, h.CreateModel)
	huma.Register(api, huma.Operation{OperationID: "admin-llm-models-patch", Method: http.MethodPatch, Path: "/v1/admin/llm/models/{uuid}",
		Summary: "Update the provided fields of a configured model", Tags: adminTags, Security: adminScope}, h.PatchModel)
	huma.Register(api, huma.Operation{OperationID: "admin-llm-models-delete", Method: http.MethodDelete, Path: "/v1/admin/llm/models/{uuid}",
		Summary: "Delete a configured model and its grants", Tags: adminTags, Security: adminScope, DefaultStatus: http.StatusNoContent}, h.DeleteModel)
}

// RegisterGrantRoutes mounts the grant replacement. Gated by
// llm.grants.admin plus RequireStepUp.
func RegisterGrantRoutes(api huma.API, h *handlers.AdminHandler) {
	huma.Register(api, huma.Operation{OperationID: "admin-llm-models-grants-put", Method: http.MethodPut, Path: "/v1/admin/llm/models/{uuid}/grants",
		Summary: "Replace the users allowed to use a model", Tags: adminTags, Security: adminScope}, h.PutGrants)
}

// RegisterSelfRoutes mounts the current user's model list. Gated by the
// org permission llm.models.self.
func RegisterSelfRoutes(api huma.API, h *handlers.SelfHandler) {
	huma.Register(api, huma.Operation{OperationID: "llm-me-models", Method: http.MethodGet, Path: "/v1/llm/me/models",
		Summary: "Models the current user may use in the current organization", Tags: []string{"LLM"}, Security: selfScope}, h.MyModels)
}
