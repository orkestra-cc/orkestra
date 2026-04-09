package module

import (
	"context"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/go-chi/chi/v5"
	"github.com/orkestra/backend/internal/shared/config"
	"github.com/orkestra/backend/internal/shared/database"
	"go.mongodb.org/mongo-driver/mongo"
)

// Module defines the contract for a self-contained backend module.
// Each module declares everything it needs (DB, config, nav, services)
// and the registry wires it automatically.
type Module interface {
	// --- Identity ---

	// Name returns the unique identifier (e.g., "billing", "sales").
	Name() string
	// DisplayName returns a human-readable name for the admin UI.
	DisplayName() string
	// Description returns a short description of the module's purpose.
	Description() string
	// Category returns whether this module is core, toggleable, or external.
	Category() ModuleCategory

	// --- Schema declarations ---

	// ConfigSchema returns the configurable fields for this module.
	// The admin UI renders forms from these, and the registry seeds DB defaults.
	ConfigSchema() []ConfigField
	// Collections returns the MongoDB collections this module owns.
	// The registry auto-creates collections and indexes on boot.
	Collections() []CollectionSpec
	// NavItems returns menu entries this module contributes to the navigation.
	// The navigation module aggregates these from all enabled modules.
	NavItems() []NavItemSpec
	// Dependencies returns names of modules that must init before this one.
	Dependencies() []string
	// ProvidedServices returns ServiceKeys this module registers in the ServiceRegistry.
	ProvidedServices() []ServiceKey
	// RequiredServices returns ServiceKeys this module needs (hard dependency — panics if missing).
	RequiredServices() []ServiceKey
	// OptionalServices returns ServiceKeys this module can use (graceful degradation if missing).
	OptionalServices() []ServiceKey

	// --- Activation ---

	// Enabled returns whether this module should be initialized.
	// During transition: checked by registry at boot. Will be replaced by DB-backed config.
	Enabled(cfg *config.Config) bool

	// --- Lifecycle ---

	// Init initializes repositories, services, and handlers.
	Init(deps *Dependencies) error
	// RegisterRoutes registers HTTP endpoints.
	RegisterRoutes(ri *RouteInfo)
	// Start launches background goroutines (polling jobs, workers).
	Start(ctx context.Context) error
	// Stop performs graceful shutdown of background goroutines.
	Stop(ctx context.Context) error
	// HealthCheck verifies runtime dependencies are healthy.
	HealthCheck(ctx context.Context) error
}

// BaseModule provides default implementations for all declarative methods.
// Embed this in your module struct to avoid implementing methods you don't need.
//
//	type MyModule struct {
//	    module.BaseModule
//	    handler *handlers.MyHandler
//	}
type BaseModule struct{}

func (BaseModule) DisplayName() string                   { return "" }
func (BaseModule) Description() string                   { return "" }
func (BaseModule) Category() ModuleCategory              { return CategoryCore }
func (BaseModule) Enabled(_ *config.Config) bool         { return true }
func (BaseModule) ConfigSchema() []ConfigField           { return nil }
func (BaseModule) Collections() []CollectionSpec         { return nil }
func (BaseModule) NavItems() []NavItemSpec               { return nil }
func (BaseModule) Dependencies() []string                { return nil }
func (BaseModule) ProvidedServices() []ServiceKey        { return nil }
func (BaseModule) RequiredServices() []ServiceKey        { return nil }
func (BaseModule) OptionalServices() []ServiceKey        { return nil }
func (BaseModule) Start(_ context.Context) error         { return nil }
func (BaseModule) Stop(_ context.Context) error          { return nil }
func (BaseModule) HealthCheck(_ context.Context) error   { return nil }

// Dependencies holds shared infrastructure injected into every module.
type Dependencies struct {
	DB            *mongo.Database
	RedisAdapter  *database.RedisClientAdapter
	Config        *config.Config
	Logger        *slog.Logger
	Services      *ServiceRegistry
	ConfigService *ModuleConfigService // set by registry before InitAll
}

// GetConfig returns a plain config value for a module. DB → env var → default.
func (d *Dependencies) GetConfig(module, key string) string {
	if d.ConfigService == nil {
		return ""
	}
	return d.ConfigService.GetValue(context.Background(), module, key)
}

// GetSecret returns a decrypted secret config value. DB → env var → default.
func (d *Dependencies) GetSecret(module, key string) string {
	if d.ConfigService == nil {
		return ""
	}
	return d.ConfigService.GetSecret(context.Background(), module, key)
}

// GetConfigBool returns a boolean config value with fallback.
func (d *Dependencies) GetConfigBool(module, key string, fallback bool) bool {
	v := d.GetConfig(module, key)
	if v == "" {
		return fallback
	}
	return v == "true" || v == "1" || v == "yes"
}

// GetConfigInt returns an integer config value with fallback.
func (d *Dependencies) GetConfigInt(module, key string, fallback int) int {
	v := d.GetConfig(module, key)
	if v == "" {
		return fallback
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return fallback
	}
	return n
}

// GetConfigDuration returns a time.Duration config value with fallback.
func (d *Dependencies) GetConfigDuration(module, key string, fallback time.Duration) time.Duration {
	v := d.GetConfig(module, key)
	if v == "" {
		return fallback
	}
	dur, err := time.ParseDuration(v)
	if err != nil {
		return fallback
	}
	return dur
}

// RoleMiddleware is the interface modules use for RBAC route protection.
// Implemented by both AuthMiddleware (monolith) and JWTValidator (AI service).
type RoleMiddleware interface {
	RequireHierarchicalRole(minRole string) func(http.Handler) http.Handler
}

// RouteInfo provides the routing infrastructure for module route registration.
type RouteInfo struct {
	// PublicAPI is for unauthenticated endpoints (health, webhooks, OAuth callbacks).
	PublicAPI huma.API
	// ProtectedRouter is the chi.Router with auth middleware applied.
	ProtectedRouter chi.Router
	// Router is the root chi.Router for special cases (dev endpoints, SSE streams).
	Router chi.Router
	// AuthMW provides role-based middleware helpers.
	AuthMW RoleMiddleware
	// APIConfig is the shared Huma API configuration.
	APIConfig huma.Config
	// ConfigService provides runtime module enabled/disabled checks for gate middleware.
	ConfigService *ModuleConfigService
}
