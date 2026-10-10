package llm

import (
	"net/http"

	"github.com/danielgtaylor/huma/v2"
	"github.com/orkestra/backend/internal/core/llm/handlers"
)

// The bearerAuth scopes below document the operator boundary in OpenAPI;
// nothing reads them. Authorization is the middleware Module.RegisterRoutes
// mounts around each group: RequireInternalTenant plus a permission (and
// RequireMFA / RequireStepUp where noted).
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

// RegisterCredentialWriteRoutes mounts every credential write: create,
// patch, rotate, delete. Gated by llm.credentials.admin plus RequireStepUp:
// patch included, because re-pointing baseUrl sends the stored API key to
// another host.
func RegisterCredentialWriteRoutes(api huma.API, h *handlers.AdminHandler) {
	huma.Register(api, huma.Operation{OperationID: "admin-llm-credentials-create", Method: http.MethodPost, Path: "/v1/admin/llm/credentials",
		Summary: "Create an LLM credential (the API key is write-only)", Tags: adminTags, Security: adminScope, DefaultStatus: http.StatusCreated}, h.CreateCredential)
	huma.Register(api, huma.Operation{OperationID: "admin-llm-credentials-patch", Method: http.MethodPatch, Path: "/v1/admin/llm/credentials/{uuid}",
		Summary: "Rename, re-point or disable a credential", Tags: adminTags, Security: adminScope}, h.PatchCredential)
	huma.Register(api, huma.Operation{OperationID: "admin-llm-credentials-rotate", Method: http.MethodPost, Path: "/v1/admin/llm/credentials/{uuid}/rotate",
		Summary: "Replace the API key of a credential", Tags: adminTags, Security: adminScope}, h.RotateCredential)
	huma.Register(api, huma.Operation{OperationID: "admin-llm-credentials-delete", Method: http.MethodDelete, Path: "/v1/admin/llm/credentials/{uuid}",
		Summary: "Delete a credential (409 while an active model uses it)", Tags: adminTags, Security: adminScope, DefaultStatus: http.StatusNoContent}, h.DeleteCredential)
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
		Summary: "Set who may use a model: access and the complete grant list", Tags: adminTags, Security: adminScope}, h.PutGrants)
}

// RegisterSelfRoutes mounts the current user's model list. Gated by the
// org permission llm.models.self.
func RegisterSelfRoutes(api huma.API, h *handlers.SelfHandler) {
	huma.Register(api, huma.Operation{OperationID: "llm-me-models", Method: http.MethodGet, Path: "/v1/llm/me/models",
		Summary: "Models the current user may use in the current organization", Tags: []string{"LLM"}, Security: selfScope}, h.MyModels)
}

// RegisterCredentialOpsRoutes mounts the credential connectivity test and
// model discovery. Gated by llm.credentials.admin + MFA, no step-up: they
// change no configuration (the test records its outcome). Rate-limited to
// 10 a minute per org and credential, each.
func RegisterCredentialOpsRoutes(api huma.API, h *handlers.OpsHandler) {
	huma.Register(api, huma.Operation{OperationID: "admin-llm-credentials-test", Method: http.MethodPost, Path: "/v1/admin/llm/credentials/{uuid}/test",
		Summary: "Test a credential against its provider and record the outcome", Tags: adminTags, Security: adminScope}, h.TestCredential)
	huma.Register(api, huma.Operation{OperationID: "admin-llm-credentials-models", Method: http.MethodPost, Path: "/v1/admin/llm/credentials/{uuid}/models",
		Summary: "List the model ids the credential's provider serves", Tags: adminTags, Security: adminScope}, h.DiscoverModels)
}

// RegisterPromptRoute mounts the admin prompt test. Gated by
// llm.models.admin + MFA; ignores grants; 10 a minute per org and user.
func RegisterPromptRoute(api huma.API, h *handlers.OpsHandler) {
	huma.Register(api, huma.Operation{OperationID: "admin-llm-models-prompt", Method: http.MethodPost, Path: "/v1/admin/llm/models/{uuid}/prompt",
		Summary: "Send a test prompt to a model (ignores grants, ledgered as llm.admin_test)", Tags: adminTags, Security: adminScope}, h.PromptTest)
}

// RegisterUsageReadRoutes mounts the usage report. Gated by llm.usage.read,
// which therefore also reads the budget (the report carries it): both are
// read-only System permissions, and the budget alone stays readable with
// llm.admin.read.
func RegisterUsageReadRoutes(api huma.API, h *handlers.UsageHandler) {
	huma.Register(api, huma.Operation{OperationID: "admin-llm-usage-get", Method: http.MethodGet, Path: "/v1/admin/llm/usage",
		Summary:     "Usage of the current organization by model, user or caller, with the month's rollup and the budget",
		Description: "Requires llm.usage.read, which also grants reading the budget embedded in the report (both read-only).", Tags: adminTags, Security: adminScope}, h.GetUsage)
}

// RegisterBudgetReadRoutes mounts the budget read. Gated by llm.admin.read.
func RegisterBudgetReadRoutes(api huma.API, h *handlers.UsageHandler) {
	huma.Register(api, huma.Operation{OperationID: "admin-llm-budget-get", Method: http.MethodGet, Path: "/v1/admin/llm/budget",
		Summary: "The organization's monthly LLM budget (204 when it has none)", Tags: adminTags, Security: adminScope}, h.GetBudget)
}

// RegisterBudgetWriteRoutes mounts the budget write. Gated by
// llm.budgets.admin + MFA + step-up.
func RegisterBudgetWriteRoutes(api huma.API, h *handlers.UsageHandler) {
	huma.Register(api, huma.Operation{OperationID: "admin-llm-budget-put", Method: http.MethodPut, Path: "/v1/admin/llm/budget",
		Summary: "Create or replace the organization's monthly LLM budget", Tags: adminTags, Security: adminScope}, h.PutBudget)
}

// RegisterSelfUsageRoutes mounts the current user's usage. Gated by the org
// permission llm.usage.self.
func RegisterSelfUsageRoutes(api huma.API, h *handlers.UsageHandler) {
	huma.Register(api, huma.Operation{OperationID: "llm-me-usage", Method: http.MethodGet, Path: "/v1/llm/me/usage",
		Summary: "The current user's LLM usage this month, by who pays", Tags: []string{"LLM"}, Security: selfScope}, h.MyUsage)
}
