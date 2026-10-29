package retrieval

import (
	"net/http"

	"github.com/danielgtaylor/huma/v2"
	"github.com/orkestra/backend/internal/core/retrieval/handlers"
)

// The bearerAuth scope documents the operator boundary in OpenAPI; nothing
// reads it. Authorization is the middleware Module.RegisterRoutes mounts
// around each group.
var (
	adminScope = []map[string][]string{{"bearerAuth": {"administrator"}}}
	adminTags  = []string{"Retrieval Admin"}
)

// RegisterCorpusReadRoutes mounts the corpus report. Gated by
// retrieval.corpus.read.
func RegisterCorpusReadRoutes(api huma.API, h *handlers.AdminHandler) {
	huma.Register(api, huma.Operation{OperationID: "admin-retrieval-corpora-list", Method: http.MethodGet, Path: "/v1/admin/retrieval/corpora",
		Summary: "List corpora with their state, engine and inference health (never content or scores)", Tags: adminTags, Security: adminScope}, h.ListCorpora)
}

// RegisterCorpusAdminRoutes mounts the diagnostic search and the orphan
// drop. Gated by retrieval.corpus.admin, MFA and a fresh step-up.
func RegisterCorpusAdminRoutes(api huma.API, h *handlers.AdminHandler) {
	huma.Register(api, huma.Operation{OperationID: "admin-retrieval-corpora-diagnostics", Method: http.MethodPost, Path: "/v1/admin/retrieval/corpora/{corpus}/diagnostics",
		Summary: "Run one search and report per-stage scores (audited)", Tags: adminTags, Security: adminScope}, h.Diagnose)
	huma.Register(api, huma.Operation{OperationID: "admin-retrieval-corpora-drop", Method: http.MethodDelete, Path: "/v1/admin/retrieval/corpora/{corpus}",
		Summary: "Drop the state and collections of an unregistered corpus (audited)", Tags: adminTags, Security: adminScope, DefaultStatus: http.StatusNoContent}, h.DropCorpus)
}

// RegisterTenantReadRoutes mounts the organization's indexing state. Gated
// by the organization permission retrieval.tenant.read.
func RegisterTenantReadRoutes(api huma.API, h *handlers.AdminHandler) {
	huma.Register(api, huma.Operation{OperationID: "admin-retrieval-tenant-indexing-get", Method: http.MethodGet, Path: "/v1/admin/retrieval/tenant/indexing",
		Summary: "Indexing state and backlog of the current organization", Tags: adminTags, Security: adminScope}, h.GetTenantIndexing)
}

// RegisterSearchLogRoutes mounts the search-log read. Gated by
// retrieval.searchlog.admin, MFA and a fresh step-up.
func RegisterSearchLogRoutes(api huma.API, h *handlers.AdminHandler) {
	huma.Register(api, huma.Operation{OperationID: "admin-retrieval-search-log-list", Method: http.MethodGet, Path: "/v1/admin/retrieval/search-log",
		Summary: "Page through the organization's audited searches (audited)", Tags: adminTags, Security: adminScope}, h.ListSearchLog)
}
