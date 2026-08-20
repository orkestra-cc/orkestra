// Package logging is the core module that owns the runtime
// log-level configuration (ADR-0005 Phase F). It runs always-on,
// like every other module under internal/core/. The module:
//
//   - declares the log_levels MongoDB collection
//   - builds a LogLevelService backed by that collection
//   - loads the persisted snapshot at boot (falls back to env)
//   - registers the service under ServiceLogLevelResolver so
//     main.go can hot-swap the slog handler's resolver
//   - mounts permanent and diagnostic admin endpoints under
//     /v1/admin/observability/
package logging

import (
	"context"
	"log/slog"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/danielgtaylor/huma/v2/adapters/humachi"
	"github.com/go-chi/chi/v5"
	"github.com/orkestra/backend/internal/core/logging/handlers"
	"github.com/orkestra/backend/internal/core/logging/logquery"
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
	handler         *handlers.LogLevelHandler
	svc             *services.LogLevelService
	logger          *slog.Logger
	cleanupInterval time.Duration
	lifecycleMu     sync.Mutex
	cleanupCancel   context.CancelFunc
	cleanupDone     chan struct{}
}

func NewModule() *LoggingModule {
	return &LoggingModule{cleanupInterval: time.Minute}
}

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
	m.logger = deps.Logger
	if err := m.svc.Load(context.Background()); err != nil {
		deps.Logger.Warn("logging: load persisted levels failed; using env defaults",
			slog.String("error", err.Error()))
	}

	deps.Services.Register(module.ServiceLogLevelResolver, m.svc)
	provider := logquery.New(os.Getenv("LOKI_QUERY_URL"), moduleNames)
	m.handler = handlers.NewLogLevelHandler(m.svc, provider, normalizeExternalURL(os.Getenv("GRAFANA_URL")))
	return nil
}

// normalizeExternalURL validates the trusted process-configured Grafana base
// before exposing it as a browser link. Request data never reaches this path.
func normalizeExternalURL(raw string) string {
	if strings.TrimSpace(raw) == "" {
		return ""
	}
	parsed, err := url.Parse(raw)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" || parsed.User != nil {
		return ""
	}
	parsed.RawQuery = ""
	parsed.Fragment = ""
	parsed.Path = strings.TrimRight(parsed.Path, "/")
	return parsed.String()
}

// Start launches best-effort storage cleanup for expired diagnostics. Resolver
// correctness remains in LogLevelService.LevelFor; this loop only removes
// entries that no longer need to remain persisted.
func (m *LoggingModule) Start(ctx context.Context) error {
	if m.svc == nil {
		return nil
	}

	m.lifecycleMu.Lock()
	defer m.lifecycleMu.Unlock()
	if m.cleanupCancel != nil {
		select {
		case <-m.cleanupDone:
			m.cleanupCancel = nil
			m.cleanupDone = nil
		default:
			return nil
		}
	}

	interval := m.cleanupInterval
	if interval <= 0 {
		interval = time.Minute
	}
	cleanupCtx, cancel := context.WithCancel(ctx)
	done := make(chan struct{})
	m.cleanupCancel = cancel
	m.cleanupDone = done
	go m.cleanupLoop(cleanupCtx, done, interval)
	return nil
}

// Stop cancels the cleanup loop and waits for it to exit. It does not acquire
// the LogLevelService write mutex; an in-flight cleanup owns and releases that
// mutex itself before the loop closes done.
func (m *LoggingModule) Stop(ctx context.Context) error {
	m.lifecycleMu.Lock()
	cancel := m.cleanupCancel
	done := m.cleanupDone
	m.lifecycleMu.Unlock()
	if cancel == nil {
		return nil
	}

	cancel()
	select {
	case <-done:
		m.lifecycleMu.Lock()
		if m.cleanupDone == done {
			m.cleanupCancel = nil
			m.cleanupDone = nil
		}
		m.lifecycleMu.Unlock()
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (m *LoggingModule) cleanupLoop(ctx context.Context, done chan<- struct{}, interval time.Duration) {
	defer close(done)
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := m.svc.CleanupExpired(ctx); err != nil && m.logger != nil {
				m.logger.ErrorContext(ctx, "logging: cleanup expired diagnostics failed",
					slog.String("error", err.Error()))
			}
		}
	}
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
