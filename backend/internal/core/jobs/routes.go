package jobs

import (
	"net/http"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humachi"
	"github.com/go-chi/chi/v5"

	"github.com/orkestra/backend/internal/core/jobs/handlers"
	"github.com/orkestra/backend/pkg/sdk/module"
)

// Permission keys. System permissions: administrator (and super_admin,
// developer) hold them through the authz seed; the system_actions Cedar
// forbid covers the "system." prefix. The gates and Permissions() spell the
// keys as literals so policycoverage can match them.
const (
	PermRead   = "system.jobs.read"
	PermManage = "system.jobs.manage"
)

var adminSecurity = []map[string][]string{{"bearerAuth": {"administrator"}}}

// RegisterRoutes mounts the Tier-1 admin API on the operator-protected router
// in two groups: reads (and the schedule preview, which changes nothing)
// behind system.jobs.read, changes behind system.jobs.manage. The OpenAPI
// administrator scope documents that boundary; the middleware enforces it.
func (m *Module) RegisterRoutes(ri *module.RouteInfo) {
	if ri == nil || ri.Operator == nil {
		return
	}
	h := m.handler
	if h == nil {
		// Not initialised (OpenAPI generation, tests): mount the routes
		// over an empty service so the document does not depend on Init.
		h = handlers.New(nil, nil)
	}
	ri.Operator.ProtectedRouter.Group(func(r chi.Router) {
		r.Use(ri.Operator.AuthMW.RequireSystemPermission("system.jobs.read"))
		registerReadRoutes(humachi.New(r, ri.APIConfig), h)
	})
	ri.Operator.ProtectedRouter.Group(func(r chi.Router) {
		r.Use(ri.Operator.AuthMW.RequireSystemPermission("system.jobs.manage"))
		registerManageRoutes(humachi.New(r, ri.APIConfig), h)
	})
}

func registerReadRoutes(api huma.API, h *handlers.Handler) {
	huma.Register(api, huma.Operation{
		OperationID: "admin-jobs-list",
		Method:      http.MethodGet,
		Path:        "/v1/admin/jobs",
		Summary:     "List background jobs",
		Description: "Every registered job with its declared and effective schedule, state and last run.",
		Tags:        []string{"Jobs"},
		Security:    adminSecurity,
	}, h.List)

	huma.Register(api, huma.Operation{
		OperationID: "admin-jobs-get",
		Method:      http.MethodGet,
		Path:        "/v1/admin/jobs/{key}",
		Summary:     "Get a background job",
		Tags:        []string{"Jobs"},
		Security:    adminSecurity,
	}, h.Get)

	huma.Register(api, huma.Operation{
		OperationID: "admin-jobs-runs",
		Method:      http.MethodGet,
		Path:        "/v1/admin/jobs/{key}/runs",
		Summary:     "List a job's runs",
		Description: "Run history, newest first, keyset-paginated with nextCursor. Details carry no personal data.",
		Tags:        []string{"Jobs"},
		Security:    adminSecurity,
	}, h.Runs)

	huma.Register(api, huma.Operation{
		OperationID: "admin-jobs-schedule-preview",
		Method:      http.MethodPost,
		Path:        "/v1/admin/jobs/schedule-preview",
		Summary:     "Preview a schedule",
		Description: "The next five due times of an every or cron schedule, computed by the backend's parser, and a one-line description.",
		Tags:        []string{"Jobs"},
		Security:    adminSecurity,
	}, h.Preview)
}

func registerManageRoutes(api huma.API, h *handlers.Handler) {
	huma.Register(api, huma.Operation{
		OperationID: "admin-jobs-update",
		Method:      http.MethodPatch,
		Path:        "/v1/admin/jobs/{key}",
		Summary:     "Override a job's schedule or enable it",
		Description: "Changes the operator override (enabled, schedule, timezone) with optimistic concurrency on revision. A change of the effective schedule reschedules the job from now.",
		Tags:        []string{"Jobs"},
		Security:    adminSecurity,
	}, h.Update)

	huma.Register(api, huma.Operation{
		OperationID: "admin-jobs-clear-override",
		Method:      http.MethodDelete,
		Path:        "/v1/admin/jobs/{key}/override",
		Summary:     "Restore a job's defaults",
		Description: "Removes the operator override so the job follows the schedule and enabled state its module declares.",
		Tags:        []string{"Jobs"},
		Security:    adminSecurity,
	}, h.ClearOverride)

	huma.Register(api, huma.Operation{
		OperationID:   "admin-jobs-run",
		Method:        http.MethodPost,
		Path:          "/v1/admin/jobs/{key}/run",
		Summary:       "Run a job now",
		Description:   "Requests one run; the first free replica takes it under the job's lease. Allowed on a disabled job, not on a job of a stopped module.",
		Tags:          []string{"Jobs"},
		Security:      adminSecurity,
		DefaultStatus: http.StatusAccepted,
	}, h.RequestRun)
}
