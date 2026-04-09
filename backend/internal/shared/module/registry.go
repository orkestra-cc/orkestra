package module

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/orkestra/backend/internal/shared/config"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// ModuleRegistry manages the lifecycle of all application modules.
// Use Register() for ordered insertion, or RegisterAll() for automatic
// dependency-based sorting via each module's Dependencies() declaration.
type ModuleRegistry struct {
	modules       []Module
	enabled       []Module           // populated by InitAll
	failed        map[string]error   // non-core modules that failed Init
	configService *ModuleConfigService
	logger        *slog.Logger
}

// NewModuleRegistry creates a new registry.
func NewModuleRegistry(logger *slog.Logger) *ModuleRegistry {
	return &ModuleRegistry{
		logger: logger,
		failed: make(map[string]error),
	}
}

// SetConfigService configures the registry to use a ModuleConfigService
// for seeding configs and checking module enabled state. Must be called before InitAll.
func (r *ModuleRegistry) SetConfigService(cs *ModuleConfigService) {
	r.configService = cs
}

// Register adds a module to the registry. Registration order determines
// initialization order — register producers before consumers.
func (r *ModuleRegistry) Register(m Module) {
	r.modules = append(r.modules, m)
}

// RegisterAll adds multiple modules and sorts them by Dependencies()
// so that producers always initialize before consumers. Callers don't
// need to worry about registration order.
func (r *ModuleRegistry) RegisterAll(modules []Module) error {
	sorted, err := topoSort(modules)
	if err != nil {
		return err
	}
	r.modules = append(r.modules, sorted...)
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
		for _, dep := range m.Dependencies() {
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

// InitAll initializes all enabled modules in registration order.
// Before initialization, it auto-creates MongoDB collections and seeds module configs.
// Core modules that fail Init are fatal; non-core modules are skipped with a warning.
func (r *ModuleRegistry) InitAll(cfg *config.Config, deps *Dependencies) error {
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
			context.Background(), r.modules, cfg,
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
		for _, item := range m.NavItems() {
			item.ModuleName = m.Name()
			stampChildren(item.Children, m.Name())
			allNavItems = append(allNavItems, item)
		}
	}
	deps.Services.Register(ServiceNavItems, allNavItems)

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

		r.enabled = append(r.enabled, m)
		r.logger.Info(fmt.Sprintf("Module %s initialized", m.Name()))
	}
	return nil
}

// RegisterAllRoutes calls RegisterRoutes on ALL modules (not just enabled).
// Non-core modules are gated by ModuleGate middleware which checks DB enabled status.
func (r *ModuleRegistry) RegisterAllRoutes(ri *RouteInfo) {
	for _, m := range r.modules {
		m.RegisterRoutes(ri)
	}
}

// StartAll starts background jobs for every enabled module.
func (r *ModuleRegistry) StartAll(ctx context.Context) error {
	for _, m := range r.enabled {
		if err := m.Start(ctx); err != nil {
			return fmt.Errorf("module %s start: %w", m.Name(), err)
		}
	}
	return nil
}

// StopAll shuts down all enabled modules in reverse registration order.
func (r *ModuleRegistry) StopAll(ctx context.Context) {
	for i := len(r.enabled) - 1; i >= 0; i-- {
		m := r.enabled[i]
		if err := m.Stop(ctx); err != nil {
			r.logger.Error(fmt.Sprintf("Module %s stop failed", m.Name()),
				slog.String("error", err.Error()))
		}
	}
}

// HealthCheckAll runs health checks on all enabled modules.
// Returns the first error encountered.
func (r *ModuleRegistry) HealthCheckAll(ctx context.Context) error {
	for _, m := range r.enabled {
		if err := m.HealthCheck(ctx); err != nil {
			return fmt.Errorf("module %s health: %w", m.Name(), err)
		}
	}
	return nil
}

// AllModules returns all registered modules (enabled, failed, and disabled).
func (r *ModuleRegistry) AllModules() []Module {
	return r.modules
}

// EnabledModules returns the names of all enabled modules.
func (r *ModuleRegistry) EnabledModules() []string {
	names := make([]string, len(r.enabled))
	for i, m := range r.enabled {
		names[i] = m.Name()
	}
	return names
}

// FailedModules returns modules that failed initialization with their errors.
func (r *ModuleRegistry) FailedModules() map[string]error {
	return r.failed
}

// CollectNavItems aggregates NavItemSpec from all enabled modules.
// Used by the navigation module to build the dynamic menu.
func (r *ModuleRegistry) CollectNavItems() []NavItemSpec {
	var items []NavItemSpec
	for _, m := range r.enabled {
		items = append(items, m.NavItems()...)
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
		for _, coll := range m.Collections() {
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
func ensureCollection(ctx context.Context, db *mongo.Database, coll CollectionSpec, moduleName string, logger *slog.Logger) error {
	// Create collection — ignore "already exists" error (MongoDB code 48).
	err := db.CreateCollection(ctx, coll.Name)
	if err != nil && !isCollectionAlreadyExists(err) {
		return fmt.Errorf("create collection %q: %w", coll.Name, err)
	}

	// Ensure indexes.
	if len(coll.Indexes) > 0 {
		indexModels := buildIndexModels(coll.Indexes)
		if _, err := db.Collection(coll.Name).Indexes().CreateMany(ctx, indexModels); err != nil {
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
