// Package logging is the core module that owns the runtime
// log-level configuration (ADR-0005 Phase F). It runs always-on,
// like every other module under internal/core/. The module:
//
//   - declares the log_levels MongoDB collection
//   - builds a LogLevelService backed by that collection
//   - loads the persisted snapshot at boot (falls back to env)
//   - registers the service under ServiceLogLevelResolver so
//     main.go can hot-swap the slog handler's resolver
//   - mounts five admin endpoints under /v1/admin/observability/
package logging

import (
	"context"
	"log/slog"

	"github.com/danielgtaylor/huma/v2/adapters/humachi"
	"github.com/go-chi/chi/v5"
	"github.com/orkestra/backend/internal/core/logging/handlers"
	"github.com/orkestra/backend/internal/core/logging/repository"
	"github.com/orkestra/backend/internal/core/logging/services"
	"github.com/orkestra/backend/internal/shared/utils"
	"github.com/orkestra/backend/pkg/sdk/module"
)

// LoggingModule wires the LogLevelService and exposes its admin
// surface. Implements module.Module + the optional sub-interfaces
// for Collections, NavItems, Dependencies, and RegisterRoutes.
type LoggingModule struct {
	module.BaseModule
	handler *handlers.LogLevelHandler
	svc     *services.LogLevelService
}

func NewModule() *LoggingModule { return &LoggingModule{} }

func (m *LoggingModule) Name() string        { return "logging" }
func (m *LoggingModule) DisplayName() string { return "Logging" }
func (m *LoggingModule) Description() string { return "Runtime log-level admin (ADR-0005 Phase F)" }
func (m *LoggingModule) Category() module.ModuleCategory {
	return module.CategoryCore
}

// Collections declares the log_levels Mongo collection. Single
// document keyed on _id="default"; the unique-on-_id is implicit
// since Mongo enforces it for the primary key.
func (m *LoggingModule) Collections() []module.CollectionSpec {
	return []module.CollectionSpec{
		{
			Name:    "log_levels",
			Indexes: []module.IndexSpec{
				// Empty — _id is enough. Kept here so this module
				// follows the same Collections() shape as every
				// other module's declaration.
			},
		},
	}
}

// Dependencies — none. The logging module needs only the registry's
// shared services (Mongo, logger). It must init early so subsequent
// modules' deps.Logger picks up the live resolver.
func (m *LoggingModule) Dependencies() []string { return nil }

// Init builds the repository + service, loads the persisted
// snapshot, registers the service for main.go to hot-swap, and
// constructs the handler. The actual handler-to-resolver swap
// happens outside (main.go calls utils.SwapLevelResolver) because
// the per-module logger.With chain is set up by the registry, not
// by this module.
func (m *LoggingModule) Init(deps *module.Dependencies) error {
	repo := repository.NewMongoRepository(deps.DB.Collection("log_levels"))

	// Build the env-driven seed by re-reading what SetupLogger
	// captured. Done by hand here (rather than threading the
	// StaticLevelResolver through deps) so the module stays a
	// drop-in addition with no signature changes to the SDK or to
	// SetupLogger. The values match what utils.SetupLogger did at
	// boot — same LOG_LEVEL / LOG_LEVEL_<MODULE> parsing.
	envGlobal := utils.GlobalLevelFromEnv()
	envPerMod := utils.LoadPerModuleLevels()

	// The module-name catalog is populated by main.go via
	// ServiceLogLevelModuleNames AFTER the registry's InitAll loop
	// finishes — at this point only earlier-init modules are known.
	// Service.View() consults the registered slice at call time so
	// new modules registered later still show up.
	var moduleNames []string
	if v := deps.Services.Get(module.ServiceLogLevelModuleNames); v != nil {
		if ns, ok := v.([]string); ok {
			moduleNames = ns
		}
	}

	m.svc = services.NewLogLevelService(repo, deps.Logger, envGlobal, envPerMod, moduleNames)
	if err := m.svc.Load(context.Background()); err != nil {
		deps.Logger.Warn("logging: load persisted levels failed; using env defaults",
			slog.String("error", err.Error()))
	}

	deps.Services.Register(module.ServiceLogLevelResolver, m.svc)
	m.handler = handlers.NewLogLevelHandler(m.svc)
	return nil
}

// RegisterRoutes mounts the admin endpoints on the operator-protected router
// behind an explicit system-permission gate. Previously they were mounted with
// no role middleware at all — the operator-protected mux only enforces
// RequireAuth, and the OpenAPI `Security:{administrator}` scope on each
// operation is inert documentation (nothing reads it), so any authenticated
// operator token could rewrite global log levels. system.modules.admin is a
// System permission held only by super_admin/administrator/developer.
func (m *LoggingModule) RegisterRoutes(ri *module.RouteInfo) {
	ri.Operator.ProtectedRouter.Group(func(r chi.Router) {
		r.Use(ri.Operator.AuthMW.RequireSystemPermission("system.modules.admin"))
		api := humachi.New(r, ri.APIConfig)
		RegisterRoutes(api, m.handler)
	})
}

// NavItems puts a single entry directly under the platform realm
// (the "Administration" group). Matches the shape every other core /
// addon admin item uses — no Section, Tier=internal, Active=true,
// MinRole=administrator. sliders-h captures "adjust per-module
// thresholds" better than a chart icon would.
func (m *LoggingModule) NavItems() []module.NavItemSpec {
	return []module.NavItemSpec{
		{
			Realm:   "platform",
			Tier:    "internal",
			Name:    "Log levels",
			Icon:    "sliders-h",
			Path:    "/admin/observability/log-levels",
			MinRole: "administrator",
			Active:  true,
		},
	}
}
