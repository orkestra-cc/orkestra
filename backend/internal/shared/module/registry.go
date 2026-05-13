package module

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/orkestra/backend/internal/shared/capability"
	"github.com/orkestra/backend/internal/shared/iface"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// ModuleRegistry manages the lifecycle of all application modules.
// Use Register() for ordered insertion, or RegisterAll() for automatic
// dependency-based sorting via each module's Dependencies() declaration.
type ModuleRegistry struct {
	modules       []Module
	initialized   []Module          // populated by InitAll — modules that passed Init
	failed        map[string]error  // non-core modules that failed Init
	started       map[string]bool   // modules that have had Start() called
	moduleByName  map[string]Module // fast lookup by name
	configService *ModuleConfigService
	deps          *Dependencies    // stored during InitAll for RetryInit
	containerMgr  ContainerManager // may be nil; Start/Stop skip infra-container handling when unset
	logger        *slog.Logger
	mu            sync.RWMutex // protects started and failed maps for runtime changes
}

// NewModuleRegistry creates a new registry.
func NewModuleRegistry(logger *slog.Logger) *ModuleRegistry {
	return &ModuleRegistry{
		logger:       logger,
		failed:       make(map[string]error),
		started:      make(map[string]bool),
		moduleByName: make(map[string]Module),
	}
}

// SetConfigService configures the registry to use a ModuleConfigService
// for seeding configs and checking module enabled state. Must be called before InitAll.
func (r *ModuleRegistry) SetConfigService(cs *ModuleConfigService) {
	r.configService = cs
}

// SetContainerManager wires a ContainerManager the registry uses to start
// and stop Docker containers declared by modules via InfraContainers().
// Must be called before StartAll / StartModule if infra containers matter.
func (r *ModuleRegistry) SetContainerManager(cm ContainerManager) {
	r.containerMgr = cm
}

// ContainerManager returns the configured container manager (possibly nil).
// Handlers use it to surface per-module infrastructure status.
func (r *ModuleRegistry) ContainerManager() ContainerManager {
	return r.containerMgr
}

// Register adds a module to the registry. Registration order determines
// initialization order — register producers before consumers.
func (r *ModuleRegistry) Register(m Module) {
	r.modules = append(r.modules, m)
	r.moduleByName[m.Name()] = m
}

// RegisterAll adds multiple modules and sorts them by Dependencies()
// so that producers always initialize before consumers. Callers don't
// need to worry about registration order.
func (r *ModuleRegistry) RegisterAll(modules []Module) error {
	sorted, err := topoSort(modules)
	if err != nil {
		return err
	}
	for _, m := range sorted {
		r.modules = append(r.modules, m)
		r.moduleByName[m.Name()] = m
	}
	return nil
}

// topoSort performs a topological sort on modules using Dependencies().
// Returns an error on dependency cycles.
func topoSort(modules []Module) ([]Module, error) {
	byName := make(map[string]Module, len(modules))
	for _, m := range modules {
		byName[m.Name()] = m
	}

	// Kahn's algorithm
	inDegree := make(map[string]int, len(modules))
	dependents := make(map[string][]string) // dep → modules that depend on it
	for _, m := range modules {
		name := m.Name()
		if _, exists := inDegree[name]; !exists {
			inDegree[name] = 0
		}
		for _, dep := range DependenciesOf(m) {
			// Only count dependencies that are in this batch.
			// Dependencies on modules outside this batch (e.g., core modules
			// already registered) are assumed satisfied.
			if _, inBatch := byName[dep]; inBatch {
				inDegree[name]++
				dependents[dep] = append(dependents[dep], name)
			}
		}
	}

	// Seed queue with modules that have no in-batch dependencies
	var queue []string
	for _, m := range modules {
		if inDegree[m.Name()] == 0 {
			queue = append(queue, m.Name())
		}
	}

	var sorted []Module
	for len(queue) > 0 {
		name := queue[0]
		queue = queue[1:]
		sorted = append(sorted, byName[name])

		for _, dependent := range dependents[name] {
			inDegree[dependent]--
			if inDegree[dependent] == 0 {
				queue = append(queue, dependent)
			}
		}
	}

	if len(sorted) != len(modules) {
		return nil, fmt.Errorf("module dependency cycle detected (sorted %d of %d modules)", len(sorted), len(modules))
	}
	return sorted, nil
}

// InitAll initializes all modules in registration order.
// Before initialization, it auto-creates MongoDB collections and seeds module configs.
// Core modules that fail Init are fatal; non-core modules are skipped with a warning.
func (r *ModuleRegistry) InitAll(deps *Dependencies) error {
	// Store deps for RetryInit use later.
	r.deps = deps

	// Make config service available to modules via deps.GetConfig/GetSecret.
	deps.ConfigService = r.configService

	// Phase 2.3: Auto-create MongoDB collections + indexes from module declarations.
	if err := r.ensureCollections(deps.DB); err != nil {
		r.logger.Error("Failed to ensure collections (non-fatal)",
			slog.String("error", err.Error()),
		)
	}

	// Phase 2.2: Seed module configs from ConfigSchema on first boot.
	if r.configService != nil {
		if err := r.configService.SeedFromModules(
			context.Background(), r.modules,
		); err != nil {
			r.logger.Error("Failed to seed module configs (non-fatal)",
				slog.String("error", err.Error()),
			)
		}

		// Register config service so navigation module can filter by enabled status.
		deps.Services.Register(ServiceConfigService, r.configService)
	}

	// Collect ALL NavItems from ALL modules (not just enabled) upfront.
	// Stamp each item with its owning module name for enabled filtering at request time.
	var allNavItems []NavItemSpec
	for _, m := range r.modules {
		for _, item := range NavItemsOf(m) {
			item.ModuleName = m.Name()
			stampChildren(item.Children, m.Name())
			allNavItems = append(allNavItems, item)
		}
	}
	deps.Services.Register(ServiceNavItems, allNavItems)

	// Register an empty capability registry so modules can resolve the
	// pointer during their Init if they want to hold a reference. It's
	// populated in registerCapabilities after all Init calls complete —
	// modules typically declare capabilities statically, but config-driven
	// Capabilities() calls must happen after Init configures the module.
	capReg := capability.NewRegistry()
	deps.Services.Register(ServiceCapabilityRegistry, capReg)

	// Initialize ALL modules in registration order.
	// Core modules: fatal on error. Non-core: attempt Init and track failures.
	// All modules get routes registered (gated by ModuleGate middleware for non-core).
	for _, m := range r.modules {
		if err := m.Init(deps); err != nil {
			if m.Category() == CategoryCore {
				return fmt.Errorf("core module %s init: %w", m.Name(), err)
			}

			// Non-core failure: log and track — routes still registered behind gate.
			r.logger.Warn("Module init failed (routes will be gated)",
				slog.String("module", m.Name()),
				slog.String("category", string(m.Category())),
				slog.String("error", err.Error()),
			)
			r.failed[m.Name()] = err
			continue
		}

		r.initialized = append(r.initialized, m)
		r.logger.Info(fmt.Sprintf("Module %s initialized", m.Name()))
	}

	// Phase 4: Collect Permissions() from every initialized module and hand
	// them to the authz provider. This seeds the catalog that admins see
	// in the role editor and that the evaluator checks against. Running
	// after InitAll ensures addons have had a chance to declare their
	// permissions too.
	r.registerPermissions()

	// Collect Capabilities() from every initialized module and populate the
	// shared capability registry. Runs after Init so modules that compute
	// their capability list from config have had a chance to configure
	// themselves. Tenants hold entitlements to capability IDs; middleware
	// and Cedar policies use this catalog to validate that grants refer to
	// a known capability and to surface the catalog in the admin UI.
	r.registerCapabilities(capReg)

	return nil
}

// registerCapabilities collects Capabilities() from every initialized module
// into the shared registry. Module-level collisions with different bodies are
// logged as warnings — the first declaration wins. Empty declarations are
// normal: not every module owns a capability.
func (r *ModuleRegistry) registerCapabilities(reg *capability.Registry) {
	count := 0
	for _, m := range r.initialized {
		caps := CapabilitiesOf(m)
		if len(caps) == 0 {
			continue
		}
		if err := reg.Register(caps...); err != nil {
			r.logger.Warn("capability registration collision",
				slog.String("module", m.Name()),
				slog.String("error", err.Error()),
			)
			continue
		}
		count += len(caps)
	}
	r.logger.Info("Registered capability catalog",
		slog.Int("count", reg.Count()),
		slog.Int("added_from_this_run", count),
	)
}

// registerPermissions collects Permissions() from all initialized modules
// and registers them with the authz provider.
func (r *ModuleRegistry) registerPermissions() {
	authz, ok := GetTyped[iface.AuthzProvider](r.deps.Services, ServiceAuthzProvider)
	if !ok {
		r.logger.Warn("authz provider not registered — permission catalog not seeded")
		return
	}

	var allPerms []iface.PermissionSpec
	for _, m := range r.initialized {
		allPerms = append(allPerms, PermissionsOf(m)...)
	}
	if err := authz.RegisterPermissions(context.Background(), allPerms); err != nil {
		r.logger.Warn("Failed to register permissions catalog",
			slog.String("error", err.Error()),
		)
	} else {
		r.logger.Info("Registered permissions catalog",
			slog.Int("count", len(allPerms)),
		)
	}
	// Seed the six system roles using the now-complete catalog.
	if seeder, ok := authz.(interface {
		SeedSystemRoles(ctx context.Context) error
	}); ok {
		if err := seeder.SeedSystemRoles(context.Background()); err != nil {
			r.logger.Warn("Failed to seed system roles",
				slog.String("error", err.Error()),
			)
		} else {
			r.logger.Info("Seeded system roles")
		}
	} else {
		r.logger.Warn("authz provider does not implement SeedSystemRoles",
			slog.String("dynamic_type", fmt.Sprintf("%T", authz)))
	}
}

// RegisterAllRoutes calls RegisterRoutes on ALL modules (not just enabled).
// Non-core modules are gated by ModuleGate middleware which checks DB enabled status.
// Modules that don't implement Routable contribute no routes.
func (r *ModuleRegistry) RegisterAllRoutes(ri *RouteInfo) {
	for _, m := range r.modules {
		RegisterRoutes(m, ri)
	}
}

// StartAll starts background jobs for modules in the startSet.
// Modules not in the startSet are skipped. Non-core modules that fail Start
// are logged and tracked in the failed map rather than being fatal.
func (r *ModuleRegistry) StartAll(ctx context.Context, startSet map[string]bool) error {
	for _, m := range r.initialized {
		if !startSet[m.Name()] {
			continue
		}
		if err := Preflight(ctx, m); err != nil {
			if m.Category() == CategoryCore {
				return fmt.Errorf("module %s preflight: %w", m.Name(), err)
			}
			r.logger.Warn("Module preflight failed",
				slog.String("module", m.Name()),
				slog.String("error", err.Error()),
			)
			r.mu.Lock()
			r.failed[m.Name()] = fmt.Errorf("preflight: %w", err)
			r.mu.Unlock()
			continue
		}
		if err := r.startInfraContainers(ctx, m); err != nil {
			if m.Category() == CategoryCore {
				return fmt.Errorf("module %s infra start: %w", m.Name(), err)
			}
			r.logger.Warn("Module infra container start failed",
				slog.String("module", m.Name()),
				slog.String("error", err.Error()),
			)
			r.mu.Lock()
			r.failed[m.Name()] = fmt.Errorf("infra: %w", err)
			r.mu.Unlock()
			continue
		}
		if err := StartModule(ctx, m); err != nil {
			if m.Category() == CategoryCore {
				return fmt.Errorf("module %s start: %w", m.Name(), err)
			}
			r.logger.Warn("Module start failed",
				slog.String("module", m.Name()),
				slog.String("error", err.Error()),
			)
			r.mu.Lock()
			r.failed[m.Name()] = fmt.Errorf("start: %w", err)
			r.mu.Unlock()
			continue
		}
		r.mu.Lock()
		r.started[m.Name()] = true
		r.mu.Unlock()
	}
	return nil
}

// startInfraContainers brings up every container declared by the module.
// Returns on the first failure; earlier containers stay running so the
// caller can retry or tear them down explicitly.
//
// A detached context is used so that short HTTP request timeouts on the
// admin toggle endpoint don't cancel the health-wait for containers that
// legitimately need ~60s to become ready. The ReadyTimeout on the spec
// still caps the wait internally.
func (r *ModuleRegistry) startInfraContainers(ctx context.Context, m Module) error {
	specs := InfraContainersOf(m)
	if len(specs) == 0 || r.containerMgr == nil {
		return nil
	}
	for _, spec := range specs {
		ready := spec.ReadyTimeout
		if ready <= 0 {
			ready = 60 * time.Second
		}
		// +30s headroom for image pull + container create before the
		// readiness wait starts ticking against ReadyTimeout.
		opCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), ready+30*time.Second)
		err := r.containerMgr.EnsureStarted(opCtx, spec)
		cancel()
		if err != nil {
			return fmt.Errorf("container %q: %w", spec.Name, err)
		}
	}
	return nil
}

// stopInfraContainers tears down every container declared by the module.
// Errors are logged but do not interrupt the stop sequence so a partially
// broken container state never blocks module disabling.
func (r *ModuleRegistry) stopInfraContainers(ctx context.Context, m Module) {
	specs := InfraContainersOf(m)
	if len(specs) == 0 || r.containerMgr == nil {
		return
	}
	const stopTimeout = 10 * time.Second
	for _, spec := range specs {
		if err := r.containerMgr.EnsureStopped(ctx, spec.Name, stopTimeout); err != nil {
			r.logger.Warn("Infra container stop failed",
				slog.String("module", m.Name()),
				slog.String("container", spec.Name),
				slog.String("error", err.Error()),
			)
		}
	}
}

// StartModule starts a single module by name at runtime.
// Idempotent: returns nil if already started.
func (r *ModuleRegistry) StartModule(ctx context.Context, name string) error {
	r.mu.Lock()
	if r.started[name] {
		r.mu.Unlock()
		return nil
	}
	r.mu.Unlock()

	m, ok := r.moduleByName[name]
	if !ok {
		return fmt.Errorf("module %q not registered", name)
	}

	// Preflight runs before any side effects so we never spin up
	// containers for a module whose prerequisites aren't met.
	if err := Preflight(ctx, m); err != nil {
		return fmt.Errorf("module %s preflight: %w", name, err)
	}

	if err := r.startInfraContainers(ctx, m); err != nil {
		return fmt.Errorf("module %s infra: %w", name, err)
	}

	if err := StartModule(ctx, m); err != nil {
		// Try to roll back infra so the next retry isn't hampered by a
		// half-started dependency. Best-effort — errors just get logged.
		r.stopInfraContainers(ctx, m)
		return fmt.Errorf("module %s start: %w", name, err)
	}

	r.mu.Lock()
	r.started[name] = true
	delete(r.failed, name) // clear any previous start failure
	r.mu.Unlock()

	r.logger.Info("Module started at runtime", slog.String("module", name))
	return nil
}

// StopModule stops a single module by name at runtime. Also tears down any
// declared infra containers even if the module never finished starting
// (e.g. it's in the failed map because its infra health check timed out),
// so disabling always leaves the system clean.
func (r *ModuleRegistry) StopModule(ctx context.Context, name string) error {
	m, ok := r.moduleByName[name]
	if !ok {
		return fmt.Errorf("module %q not registered", name)
	}

	r.mu.Lock()
	wasStarted := r.started[name]
	r.mu.Unlock()

	if wasStarted {
		if err := StopModule(ctx, m); err != nil {
			return fmt.Errorf("module %s stop: %w", name, err)
		}
	}

	// Tear down declared infra containers whether the module was started
	// or not — a failed module may still have a half-started container.
	r.stopInfraContainers(ctx, m)

	r.mu.Lock()
	delete(r.started, name)
	delete(r.failed, name) // disabling clears failure state; re-enable will retry cleanly
	r.mu.Unlock()

	r.logger.Info("Module stopped at runtime", slog.String("module", name), slog.Bool("wasStarted", wasStarted))
	return nil
}

// IsStarted returns true if the named module has been started.
func (r *ModuleRegistry) IsStarted(name string) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.started[name]
}

// CheckCanDisable verifies that no currently-started module depends on the
// named module. Returns an error describing the blocker if found.
func (r *ModuleRegistry) CheckCanDisable(name string) error {
	r.mu.RLock()
	defer r.mu.RUnlock()

	for modName, isStarted := range r.started {
		if !isStarted || modName == name {
			continue
		}
		m := r.moduleByName[modName]
		for _, dep := range DependenciesOf(m) {
			if dep == name {
				return fmt.Errorf("cannot disable %q: module %q depends on it and is currently running", name, modName)
			}
		}
	}
	return nil
}

// RetryInit re-attempts Init() for a module that previously failed.
// On success, the module is moved from the failed map to the initialized list.
// Uses ServiceRegistry.Replace() so re-registration doesn't panic.
func (r *ModuleRegistry) RetryInit(name string) error {
	r.mu.Lock()
	_, isFailed := r.failed[name]
	r.mu.Unlock()

	if !isFailed {
		return fmt.Errorf("module %q is not in failed state", name)
	}

	m, ok := r.moduleByName[name]
	if !ok {
		return fmt.Errorf("module %q not registered", name)
	}

	// Enable replacement mode so the module can re-register its services.
	r.deps.Services.SetReplaceMode(true)
	defer r.deps.Services.SetReplaceMode(false)

	if err := m.Init(r.deps); err != nil {
		r.mu.Lock()
		r.failed[name] = err
		r.mu.Unlock()
		return fmt.Errorf("module %s retry init: %w", name, err)
	}

	r.mu.Lock()
	delete(r.failed, name)
	r.mu.Unlock()

	r.initialized = append(r.initialized, m)
	r.logger.Info("Module re-initialized after retry", slog.String("module", name))

	// Re-register permissions to include the newly initialized module.
	r.registerPermissions()

	return nil
}

// StopAll shuts down all started modules in reverse initialization order.
func (r *ModuleRegistry) StopAll(ctx context.Context) {
	r.mu.RLock()
	// Build a list of started modules in init order, then reverse.
	var toStop []Module
	for i := len(r.initialized) - 1; i >= 0; i-- {
		m := r.initialized[i]
		if r.started[m.Name()] {
			toStop = append(toStop, m)
		}
	}
	r.mu.RUnlock()

	for _, m := range toStop {
		if err := StopModule(ctx, m); err != nil {
			r.logger.Error(fmt.Sprintf("Module %s stop failed", m.Name()),
				slog.String("error", err.Error()))
		}
		r.stopInfraContainers(ctx, m)
	}
}

// HealthCheckAll runs health checks on all started modules.
// Returns the first error encountered.
func (r *ModuleRegistry) HealthCheckAll(ctx context.Context) error {
	r.mu.RLock()
	var toCheck []Module
	for _, m := range r.initialized {
		if r.started[m.Name()] {
			toCheck = append(toCheck, m)
		}
	}
	r.mu.RUnlock()

	for _, m := range toCheck {
		if err := CheckHealth(ctx, m); err != nil {
			return fmt.Errorf("module %s health: %w", m.Name(), err)
		}
	}
	return nil
}

// AllModules returns all registered modules (initialized, failed, and disabled).
func (r *ModuleRegistry) AllModules() []Module {
	return r.modules
}

// EnabledModules returns the names of all started modules.
func (r *ModuleRegistry) EnabledModules() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()

	var names []string
	for name, isStarted := range r.started {
		if isStarted {
			names = append(names, name)
		}
	}
	return names
}

// FailedModules returns modules that failed initialization with their errors.
func (r *ModuleRegistry) FailedModules() map[string]error {
	r.mu.RLock()
	defer r.mu.RUnlock()

	// Return a copy to avoid data races.
	cp := make(map[string]error, len(r.failed))
	for k, v := range r.failed {
		cp[k] = v
	}
	return cp
}

// SupportsHotReload returns true if the named module is loaded and reports
// that it reads config lazily (HotReloadConfig() == true).
func (r *ModuleRegistry) SupportsHotReload(name string) bool {
	if m, ok := r.moduleByName[name]; ok {
		return HotReloadsConfig(m)
	}
	return false
}

// CollectNavItems aggregates NavItemSpec from all initialized modules.
// Used by the navigation module to build the dynamic menu.
func (r *ModuleRegistry) CollectNavItems() []NavItemSpec {
	items := make([]NavItemSpec, 0, len(r.initialized))
	for _, m := range r.initialized {
		items = append(items, NavItemsOf(m)...)
	}
	return items
}

// --- Collection auto-setup ---

// ensureCollections creates MongoDB collections and indexes declared by all registered modules.
// Errors are logged as warnings — collection creation is idempotent and non-fatal.
func (r *ModuleRegistry) ensureCollections(db *mongo.Database) error {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	for _, m := range r.modules {
		for _, coll := range CollectionsOf(m) {
			if err := ensureCollection(ctx, db, coll, m.Name(), r.logger); err != nil {
				r.logger.Warn("Failed to ensure collection",
					slog.String("module", m.Name()),
					slog.String("collection", coll.Name),
					slog.String("error", err.Error()),
				)
			}
		}
	}
	return nil
}

// ensureCollection creates a single collection and its indexes if they don't exist.
// Each step is retried on transient auth errors because the Mongo driver's
// background monitoring connections re-authenticate for several seconds after
// a container first boots, and they can briefly poison the connection pool
// even after the main client has completed a successful auth probe.
func ensureCollection(ctx context.Context, db *mongo.Database, coll CollectionSpec, moduleName string, logger *slog.Logger) error {
	if err := retryTransient(ctx, logger, "create collection "+coll.Name, func(opCtx context.Context) error {
		err := db.CreateCollection(opCtx, coll.Name)
		if err != nil && !isCollectionAlreadyExists(err) {
			return err
		}
		return nil
	}); err != nil {
		return fmt.Errorf("create collection %q: %w", coll.Name, err)
	}

	if len(coll.Indexes) > 0 {
		indexModels := buildIndexModels(coll.Indexes)
		if err := retryTransient(ctx, logger, "create indexes "+coll.Name, func(opCtx context.Context) error {
			_, err := db.Collection(coll.Name).Indexes().CreateMany(opCtx, indexModels)
			return err
		}); err != nil {
			return fmt.Errorf("create indexes for %q: %w", coll.Name, err)
		}
	}

	logger.Debug("Collection ensured",
		slog.String("module", moduleName),
		slog.String("collection", coll.Name),
		slog.Int("indexes", len(coll.Indexes)),
	)
	return nil
}

// retryTransient runs op with exponential backoff on transient errors
// (notably "connection pool cleared" caused by the Mongo driver's
// background auth reconnection during container first-boot).
func retryTransient(ctx context.Context, logger *slog.Logger, label string, op func(context.Context) error) error {
	const maxAttempts = 10
	var lastErr error
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		opCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
		err := op(opCtx)
		cancel()
		if err == nil {
			return nil
		}
		lastErr = err
		if !isTransientMongoError(err) || attempt == maxAttempts {
			return err
		}
		backoff := time.Duration(1<<uint(attempt-1)) * 200 * time.Millisecond
		if backoff > 3*time.Second {
			backoff = 3 * time.Second
		}
		logger.Debug("Transient mongo error, retrying",
			slog.String("op", label),
			slog.Int("attempt", attempt),
			slog.Duration("backoff", backoff),
			slog.String("error", err.Error()),
		)
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(backoff):
		}
	}
	return lastErr
}

// isTransientMongoError matches errors worth retrying at startup:
// pool cleared from a background auth handshake failure, or the
// AuthenticationFailed (code 18) from a command hitting the window
// before SCRAM users are fully provisioned.
func isTransientMongoError(err error) bool {
	if err == nil {
		return false
	}
	var cmdErr mongo.CommandError
	if errors.As(err, &cmdErr) && cmdErr.Code == 18 {
		return true
	}
	msg := err.Error()
	return containsAny(msg,
		"connection pool",
		"AuthenticationFailed",
		"SCRAM",
		"server selection",
	)
}

func containsAny(s string, needles ...string) bool {
	for _, n := range needles {
		for i := 0; i+len(n) <= len(s); i++ {
			if s[i:i+len(n)] == n {
				return true
			}
		}
	}
	return false
}

// isCollectionAlreadyExists checks for MongoDB error code 48 (NamespaceExists).
func isCollectionAlreadyExists(err error) bool {
	var cmdErr mongo.CommandError
	if errors.As(err, &cmdErr) {
		return cmdErr.Code == 48
	}
	return false
}

// buildIndexModels converts module IndexSpec declarations to MongoDB index models.
func buildIndexModels(specs []IndexSpec) []mongo.IndexModel {
	models := make([]mongo.IndexModel, 0, len(specs))
	for _, spec := range specs {
		var keys bson.D

		if spec.Text {
			// Text index: all keys become "text" type.
			if len(spec.OrderedKeys) > 0 {
				for _, k := range spec.OrderedKeys {
					keys = append(keys, bson.E{Key: k.Field, Value: "text"})
				}
			} else {
				for field := range spec.Keys {
					keys = append(keys, bson.E{Key: field, Value: "text"})
				}
			}
		} else if len(spec.OrderedKeys) > 0 {
			// Compound index with deterministic field order.
			for _, k := range spec.OrderedKeys {
				keys = append(keys, bson.E{Key: k.Field, Value: k.Direction})
			}
		} else {
			// Single-field index (map is fine for one key).
			for field, direction := range spec.Keys {
				keys = append(keys, bson.E{Key: field, Value: direction})
			}
		}

		opts := options.Index()
		if spec.Unique {
			opts.SetUnique(true)
		}
		if spec.Sparse {
			opts.SetSparse(true)
		}
		if spec.TTL > 0 {
			opts.SetExpireAfterSeconds(int32(spec.TTL.Seconds()))
		} else if spec.ExpireAt {
			// ExpireAfterSeconds=0 on a date-typed field means "reap the doc
			// when the field's timestamp passes". Used for absolute-expiry
			// indexes like tenant_org_invites.expiresAt.
			opts.SetExpireAfterSeconds(0)
		}

		models = append(models, mongo.IndexModel{Keys: keys, Options: opts})
	}
	return models
}

// stampChildren recursively sets ModuleName on child NavItemSpecs.
func stampChildren(children []NavItemSpec, moduleName string) {
	for i := range children {
		children[i].ModuleName = moduleName
		stampChildren(children[i].Children, moduleName)
	}
}
