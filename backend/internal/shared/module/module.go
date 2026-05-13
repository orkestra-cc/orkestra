package module

import (
	"context"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/go-chi/chi/v5"
	"github.com/orkestra/backend/internal/shared/capability"
	"github.com/orkestra/backend/internal/shared/config"
	"github.com/orkestra/backend/internal/shared/database"
	"github.com/orkestra/backend/internal/shared/iface"
	"go.mongodb.org/mongo-driver/mongo"
)

// Module defines the contract for a self-contained backend module.
// Each module declares everything it needs (DB, config, nav, services)
// and the registry wires it automatically.
// Module is the minimum contract every Orkestra module must satisfy. The
// registry can boot, seed, and route a module that implements ONLY these
// three methods. Every other capability — config schemas, nav items,
// permissions, lifecycle hooks, infra containers — lives behind an
// optional sub-interface below (HasConfigSchema, HasNavItems, Startable,
// etc.) and is invoked via type assertion.
//
// PR-1b reshape rationale: when this interface becomes the public SDK
// surface (Phase 4), every method here is frozen. Future capabilities
// must be added as new sub-interfaces; widening Module would break every
// addon published outside the monorepo.
//
// In-monorepo modules typically embed BaseModule, which implements every
// sub-interface with sensible defaults so the type assertions all
// succeed at runtime. Extracted addons may skip BaseModule and implement
// only the sub-interfaces they care about — the registry handles both.
type Module interface {
	// Name returns the unique identifier (e.g., "billing", "sales").
	Name() string
	// Category returns whether this module is core, toggleable, or external.
	// Drives registry decisions like "fatal on init failure for core".
	Category() ModuleCategory
	// Init initializes repositories, services, and handlers.
	Init(deps *Dependencies) error
}

// --- Optional sub-interfaces (queried via type assertion) ---

// HasDisplayInfo lets a module advertise a human-readable name and
// description for the admin UI. Modules that omit this are listed by
// their Name() alone.
type HasDisplayInfo interface {
	DisplayName() string
	Description() string
}

// HasConfigSchema lets a module declare its admin-editable configuration
// fields. The registry seeds DB defaults from the schema's EnvVar /
// Default values at first boot; the admin UI renders forms from it.
type HasConfigSchema interface {
	ConfigSchema() []ConfigField
}

// HasCollections lets a module declare the MongoDB collections it owns.
// The registry auto-creates them with the requested indexes on boot.
type HasCollections interface {
	Collections() []CollectionSpec
}

// HasNavItems lets a module contribute entries to the dynamic sidebar.
// The navigation module aggregates them from every enabled module.
type HasNavItems interface {
	NavItems() []NavItemSpec
}

// HasPermissions lets a module register authorization permissions in
// the central catalog. The registry collects these from every module at
// boot and upserts them so administrators can bind them to custom roles.
type HasPermissions interface {
	Permissions() []iface.PermissionSpec
}

// HasCapabilities lets a module declare entitlement-gated capabilities —
// the units tenants subscribe to. Middleware and Cedar policies gate
// routes by checking whether the current tenant holds an active
// entitlement to the capability ID.
type HasCapabilities interface {
	Capabilities() []capability.Capability
}

// HasDependencies lets a module declare other modules that must init
// before it. The registry's topological sort consumes this declaration.
type HasDependencies interface {
	Dependencies() []string
}

// HasServiceContracts lets a module declare its participation in the
// ServiceRegistry: what it produces, what it hard-requires, and what
// it would consume gracefully if present. The three lists are
// conceptually a unit — addons that have any will declare all three —
// hence the grouped interface.
type HasServiceContracts interface {
	ProvidedServices() []ServiceKey
	RequiredServices() []ServiceKey
	OptionalServices() []ServiceKey
}

// HasDefaultEnabled lets a module override the default first-install
// activation state. Consulted exactly once by buildInitialConfig() when
// seeding the module_configs collection; after first boot the persisted
// flag in the DB is authoritative. Modules that omit this default to
// enabled=true (BaseModule's behavior).
//
// Implementations must be self-contained — read env vars directly when
// the default depends on deployment configuration, do NOT reach into
// shared/config. Addons published outside the monorepo cannot import
// shared/config at all.
type HasDefaultEnabled interface {
	Enabled() bool
}

// HotReloadable indicates that a module reads its configuration lazily
// (typically via a ConfigLoader closure passed into services), so admin
// UI edits to its config take effect without a backend restart.
// Default (modules that omit it) is false: changes require a restart.
type HotReloadable interface {
	HotReloadConfig() bool
}

// Routable lets a module register HTTP routes with the host's router
// (and Huma adapter). Modules that expose no HTTP surface — pure
// background workers or service providers — can omit this entirely.
type Routable interface {
	RegisterRoutes(ri *RouteInfo)
}

// Startable lets a module launch background goroutines (polling jobs,
// queue workers, etc.) after Init. Called per-enable when modules are
// hot-toggled at runtime, not just at boot.
type Startable interface {
	Start(ctx context.Context) error
}

// Stoppable performs graceful shutdown of whatever Startable started.
// Called on module hot-disable and on host shutdown.
type Stoppable interface {
	Stop(ctx context.Context) error
}

// HealthCheckable verifies the module's runtime dependencies are
// healthy. Polled by the /admin/modules/health endpoint.
type HealthCheckable interface {
	HealthCheck(ctx context.Context) error
}

// HasInfraContainers lets a module declare Docker containers the
// registry should start before the module's Start() and stop after its
// Stop() — used by graph (Memgraph) and agents (Hindsight).
type HasInfraContainers interface {
	InfraContainers() []InfraContainerSpec
}

// HasPreflight lets a module validate runtime prerequisites that depend
// on mutable state (other modules' config, external services, etc.)
// before the registry brings up its infra containers. Returning an
// error here surfaces directly to the admin UI without side effects.
//
// Typical use: agents requires aimodels to have a default LLM
// configured; Preflight() returns a descriptive error otherwise.
type HasPreflight interface {
	Preflight(ctx context.Context) error
}

// InfraContainerSpec describes a Docker container whose lifecycle is bound to
// a module. The registry brings these up via the shared container.Manager
// when the module is started and tears them down when it is stopped.
type InfraContainerSpec struct {
	// Name is the stable Docker container name (used as the DNS name on
	// the orkestra-network too). Required.
	Name string
	// Image is the fully-qualified image reference. Required.
	Image string
	// Env maps environment variables passed to the container.
	Env map[string]string
	// Volumes lists named volumes to mount.
	Volumes []InfraVolumeMount
	// Ports lists host→container port bindings.
	Ports []InfraPortBinding
	// Network is the Docker network the container joins. Must exist
	// beforehand (e.g. orkestra-network, precreated by compose).
	Network string
	// HealthCheck is polled from the backend after ContainerStart. If nil,
	// the manager returns as soon as Docker reports the container running.
	HealthCheck *InfraHealthCheck
	// ReadyTimeout is the maximum total time spent waiting for the
	// container to report healthy. Defaults to 60s if zero.
	ReadyTimeout time.Duration
	// Labels are attached to the container for discovery/observability.
	Labels map[string]string
}

// InfraVolumeMount references a named Docker volume.
type InfraVolumeMount struct {
	Name   string // named volume (created on demand if missing)
	Target string // mount path inside the container
}

// InfraPortBinding publishes a container port on the host.
type InfraPortBinding struct {
	HostPort      int
	ContainerPort int
	Protocol      string // "tcp" (default) or "udp"
}

// InfraHealthCheck describes a readiness probe the container.Manager polls
// from the backend container after ContainerStart. Exactly one of HTTPPath
// or TCPPort should be set — HTTPPath performs an HTTP GET (used by
// services that expose a /health endpoint), TCPPort performs a raw TCP
// dial (used by databases like Memgraph whose native protocol isn't HTTP).
type InfraHealthCheck struct {
	HTTPPath string        // e.g. "/health" — if set, Port must also be set
	Port     int           // container port to probe (paired with HTTPPath)
	TCPPort  int           // container port to TCP-dial (alternative to HTTPPath)
	Interval time.Duration // between polls (default 2s)
	Retries  int           // max attempts (default 30)
	Timeout  time.Duration // per-request timeout (default 5s)
}

// ContainerManager is the registry-facing contract for managing the
// lifecycle of Docker containers declared by modules. Satisfied by
// shared/container.Manager. Declared here to avoid an import cycle
// (container depends on this package for InfraContainerSpec).
type ContainerManager interface {
	EnsureStarted(ctx context.Context, spec InfraContainerSpec) error
	EnsureStopped(ctx context.Context, name string, timeout time.Duration) error
	IsRunning(ctx context.Context, name string) (bool, error)
	Available() bool
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
func (BaseModule) Enabled() bool                         { return true }
func (BaseModule) ConfigSchema() []ConfigField           { return nil }
func (BaseModule) Collections() []CollectionSpec         { return nil }
func (BaseModule) NavItems() []NavItemSpec               { return nil }
func (BaseModule) Permissions() []iface.PermissionSpec   { return nil }
func (BaseModule) Capabilities() []capability.Capability { return nil }
func (BaseModule) Dependencies() []string                { return nil }
func (BaseModule) ProvidedServices() []ServiceKey        { return nil }
func (BaseModule) RequiredServices() []ServiceKey        { return nil }
func (BaseModule) OptionalServices() []ServiceKey        { return nil }
func (BaseModule) HotReloadConfig() bool                 { return false }
func (BaseModule) Start(_ context.Context) error         { return nil }
func (BaseModule) Stop(_ context.Context) error          { return nil }
func (BaseModule) HealthCheck(_ context.Context) error   { return nil }
func (BaseModule) InfraContainers() []InfraContainerSpec { return nil }
func (BaseModule) Preflight(_ context.Context) error     { return nil }

// --- Optional-capability accessors ---
//
// Each helper queries one optional sub-interface and falls back to a
// sensible default when the module doesn't implement it. The registry
// (and any other consumer of Module) should call these instead of
// reaching for methods on the interface directly — they keep the
// type-assertion logic in one place and let modules opt in piecemeal.

// DisplayNameOf returns the module's display name, falling back to the
// module's Name when HasDisplayInfo is not implemented.
func DisplayNameOf(m Module) string {
	if di, ok := m.(HasDisplayInfo); ok {
		if name := di.DisplayName(); name != "" {
			return name
		}
	}
	return m.Name()
}

// DescriptionOf returns the module's description, or empty if the
// module does not implement HasDisplayInfo.
func DescriptionOf(m Module) string {
	if di, ok := m.(HasDisplayInfo); ok {
		return di.Description()
	}
	return ""
}

// ConfigSchemaOf returns the module's config schema, or nil when the
// module declares no schema.
func ConfigSchemaOf(m Module) []ConfigField {
	if cs, ok := m.(HasConfigSchema); ok {
		return cs.ConfigSchema()
	}
	return nil
}

// CollectionsOf returns the MongoDB collections the module owns, or nil
// when the module declares none.
func CollectionsOf(m Module) []CollectionSpec {
	if c, ok := m.(HasCollections); ok {
		return c.Collections()
	}
	return nil
}

// NavItemsOf returns the module's nav items, or nil when none declared.
func NavItemsOf(m Module) []NavItemSpec {
	if n, ok := m.(HasNavItems); ok {
		return n.NavItems()
	}
	return nil
}

// PermissionsOf returns the module's permission declarations, or nil.
func PermissionsOf(m Module) []iface.PermissionSpec {
	if p, ok := m.(HasPermissions); ok {
		return p.Permissions()
	}
	return nil
}

// CapabilitiesOf returns the module's entitlement-gated capabilities, or nil.
func CapabilitiesOf(m Module) []capability.Capability {
	if c, ok := m.(HasCapabilities); ok {
		return c.Capabilities()
	}
	return nil
}

// DependenciesOf returns the names of modules that must init before m,
// or nil when m declares no dependencies.
func DependenciesOf(m Module) []string {
	if d, ok := m.(HasDependencies); ok {
		return d.Dependencies()
	}
	return nil
}

// ProvidedServicesOf returns the ServiceKeys m registers in the
// ServiceRegistry, or nil.
func ProvidedServicesOf(m Module) []ServiceKey {
	if s, ok := m.(HasServiceContracts); ok {
		return s.ProvidedServices()
	}
	return nil
}

// RequiredServicesOf returns the ServiceKeys m hard-requires, or nil.
func RequiredServicesOf(m Module) []ServiceKey {
	if s, ok := m.(HasServiceContracts); ok {
		return s.RequiredServices()
	}
	return nil
}

// OptionalServicesOf returns the ServiceKeys m can use gracefully, or nil.
func OptionalServicesOf(m Module) []ServiceKey {
	if s, ok := m.(HasServiceContracts); ok {
		return s.OptionalServices()
	}
	return nil
}

// EnabledByDefault returns the module's preferred first-install state.
// Defaults to true when the module does not implement HasDefaultEnabled.
func EnabledByDefault(m Module) bool {
	if e, ok := m.(HasDefaultEnabled); ok {
		return e.Enabled()
	}
	return true
}

// HotReloadsConfig reports whether the module reloads config without
// a backend restart. Defaults to false.
func HotReloadsConfig(m Module) bool {
	if h, ok := m.(HotReloadable); ok {
		return h.HotReloadConfig()
	}
	return false
}

// RegisterRoutes invokes m's Routable.RegisterRoutes when implemented;
// no-op otherwise. Modules that expose no HTTP surface simply omit it.
func RegisterRoutes(m Module, ri *RouteInfo) {
	if r, ok := m.(Routable); ok {
		r.RegisterRoutes(ri)
	}
}

// StartModule invokes Startable.Start when implemented; no-op otherwise.
func StartModule(ctx context.Context, m Module) error {
	if s, ok := m.(Startable); ok {
		return s.Start(ctx)
	}
	return nil
}

// StopModule invokes Stoppable.Stop when implemented; no-op otherwise.
func StopModule(ctx context.Context, m Module) error {
	if s, ok := m.(Stoppable); ok {
		return s.Stop(ctx)
	}
	return nil
}

// CheckHealth invokes HealthCheckable.HealthCheck when implemented;
// returns nil otherwise.
func CheckHealth(ctx context.Context, m Module) error {
	if h, ok := m.(HealthCheckable); ok {
		return h.HealthCheck(ctx)
	}
	return nil
}

// InfraContainersOf returns the docker containers m owns, or nil.
func InfraContainersOf(m Module) []InfraContainerSpec {
	if i, ok := m.(HasInfraContainers); ok {
		return i.InfraContainers()
	}
	return nil
}

// Preflight invokes HasPreflight.Preflight when implemented; returns
// nil otherwise.
func Preflight(ctx context.Context, m Module) error {
	if p, ok := m.(HasPreflight); ok {
		return p.Preflight(ctx)
	}
	return nil
}

// PlatformInfo is the SDK-visible subset of the backend's app config that
// addons may legitimately need: the runtime environment classification and
// the public frontend origin (used by notification links). Addons should
// reach into platform info through this interface and NOT through
// *config.Config — the latter is a backend-internal struct that extracted
// addons cannot import.
//
// *config.Config from internal/shared/config satisfies this interface; the
// monolith and ai-service binaries inject *config.Config as Dependencies.Platform.
type PlatformInfo interface {
	IsProduction() bool
	IsStaging() bool
	IsDevelopment() bool
	IsProductionLike() bool
	GetEnvironment() string
	FrontendURL() string
}

// Dependencies holds shared infrastructure injected into every module.
type Dependencies struct {
	DB           *mongo.Database
	RedisAdapter *database.RedisClientAdapter
	// Config is the legacy *config.Config handle. New code should use
	// Platform for environment + frontend URL info and ConfigService /
	// UnmarshalModule for per-module values. Auth core still reads from
	// this field heavily and is retired in Phase 1c.
	//
	// Deprecated: use Platform + ConfigService instead.
	Config        *config.Config
	Platform      PlatformInfo
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

// RoleMiddleware is the interface modules use for authorization route
// protection. Implemented by both AuthMiddleware (monolith) and JWTValidator
// (AI service sidecar). Permissions are checked against the current org in
// the request context (set from the X-Org-ID header at auth time).
type RoleMiddleware interface {
	// RequirePermission blocks the request unless the current user holds
	// the given permission in the current org (from X-Org-ID).
	RequirePermission(permission string) func(http.Handler) http.Handler

	// RequireSystemPermission blocks the request unless the user's system
	// role grants the given system-level permission (e.g. platform admin).
	// Does not need an org context.
	RequireSystemPermission(permission string) func(http.Handler) http.Handler

	// RequireCapability blocks the request unless the current tenant holds
	// an active entitlement to the given capability ID in the
	// tenant_entitlements projection (Phase 2 of ADR-0001). Returns 402
	// Payment Required — distinct from 403 Forbidden so the frontend can
	// route the error to a subscribe / upgrade flow rather than an
	// access-denied screen. Apply after RequirePermission so RBAC runs
	// first; both gates must pass.
	RequireCapability(capabilityID string) func(http.Handler) http.Handler

	// RequireGlobal allows the request without an org context. Use for
	// auth flows, self-service, and org-list endpoints that run before a
	// current org is selected.
	RequireGlobal() func(http.Handler) http.Handler

	// RequireMFA blocks the request unless the caller's access token records
	// that a second factor was completed for this session (amr claim
	// contains "otp" or "webauthn"). Applied to sensitive routes such as
	// role grant/revoke and module secret writes. Returns 401 with
	// code="step_up_required" — shared with RequireStepUp and RequireLowRisk
	// so the frontend's step-up modal handles every MFA-driven 401 the same
	// way: prompt for a TOTP code, call /v1/auth/mfa/verify, retry.
	RequireMFA() func(http.Handler) http.Handler

	// RequireStepUp blocks the request unless the caller completed a second
	// factor within maxAge of now. Stronger than RequireMFA, which is
	// satisfied for the lifetime of the access token — step-up demands a
	// *fresh* MFA proof for operations whose abuse is catastrophic or
	// irreversible (admin resetting another user's MFA, tenant deletion,
	// bulk PII export, role-ownership transfer). Returns 401 with
	// code="step_up_required" and a maxAge hint; the client is expected
	// to drive the user through /v1/auth/mfa/verify and retry.
	RequireStepUp(maxAge time.Duration) func(http.Handler) http.Handler

	// RequireLowRisk blocks the request when the current session's most
	// recent risk score meets or exceeds threshold. Reuses the
	// code="step_up_required" response envelope so the frontend's
	// step-up modal completes the action transparently. Fails open on
	// lookup errors or when the scorer is unwired — a degraded risk
	// signal must not lock privileged actions out. Apply after
	// RequireMFA on routes that already demand a second factor; the
	// combination means a stolen token from a high-risk session still
	// can't perform the action even if the token itself is MFA-stepped.
	// Section C item #2 of the 2026-04-24 auth roadmap.
	RequireLowRisk(threshold float64) func(http.Handler) http.Handler

	// RequireInternalTenant rejects requests whose resolved tenant is not
	// internal (Tier-1 operator). Use on operator-only routes: billing /
	// FatturaPA, subscription-admin, payments-admin. Warn-mode (set via
	// TENANT_KIND_ENFORCEMENT=warn) logs mismatches and passes through so
	// operators can stage the rollout before it starts returning 403.
	RequireInternalTenant() func(http.Handler) http.Handler

	// RequireExternalTenant rejects requests whose resolved tenant is not
	// external (Tier-2 client). Use on client-only routes: self-service
	// checkout, client portal. Honours the same warn/enforce env as
	// RequireInternalTenant.
	RequireExternalTenant() func(http.Handler) http.Handler
}

// Audience identifies which class of consumer an API surface serves. Per
// ADR-0003, Orkestra splits its HTTP surface into three audiences — operator
// (Tier-1 internal console), client (Tier-2 external clients), and service
// (internal sidecar-only). Each audience eventually owns its own host, JWT
// `aud` claim, OpenAPI document, CORS allowlist, and rate-limit policy.
type Audience string

const (
	// AudienceOperator is the Tier-1 operator-facing surface (admin console,
	// module management, internal billing/SDI, dev tooling).
	AudienceOperator Audience = "operator"
	// AudienceClient is the Tier-2 client-facing surface (subscriptions,
	// payments, onboarding, future AI-runtime endpoints).
	AudienceClient Audience = "client"
	// AudienceService is the internal-network-only surface used by the AI
	// sidecar over /v1/internal/* — never reachable from ingress.
	AudienceService Audience = "service"
)

// APISurface is the per-audience routing bundle a module receives at
// RegisterRoutes time. Modules that target one audience read from the
// matching surface; modules that legitimately serve both audiences read
// from each surface and may register the same handler twice.
//
// During PR-A of ADR-0003 the host mux does not yet exist: both the
// Operator and Client surfaces on a `RouteInfo` reference the same
// underlying huma.API / chi.Router / RoleMiddleware instances, so behavior
// is unchanged. PR-C is what gives each surface its own mux, JWT audience
// check, CORS allowlist, and rate-limit policy.
type APISurface struct {
	// Audience tags this surface so handlers can branch on it when a single
	// module legitimately serves both tiers (rare — `auth`, `user`, etc.).
	Audience Audience
	// PublicAPI is for unauthenticated endpoints scoped to this audience
	// (health, webhooks, OAuth callbacks, public catalog browse).
	PublicAPI huma.API
	// ProtectedRouter is the chi.Router with auth middleware applied,
	// scoped to this audience.
	ProtectedRouter chi.Router
	// AuthMW provides role-based middleware helpers for this audience.
	// Post-PR-D the middleware also enforces JWT `aud == Audience`.
	AuthMW RoleMiddleware
}

// RouteInfo provides the routing infrastructure for module route registration.
// Operator and Client are the per-audience surfaces; Router/APIConfig/
// ConfigService remain audience-agnostic (root router for SSE/dev/raw HTTP,
// shared Huma config, runtime enable/disable lookups).
type RouteInfo struct {
	// Operator is the Tier-1 operator-facing audience surface. Nil if a
	// module is not registered on operator (post-PR-A: client-only modules).
	Operator *APISurface
	// Client is the Tier-2 client-facing audience surface. Nil if a module
	// is not registered on client.
	Client *APISurface
	// Router is the root chi.Router for special cases (dev endpoints, SSE
	// streams, raw HTTP handlers that predate the audience split).
	// Operator-only — dev tokens, setup wizard, and the legacy raw HTTP
	// auth refresh/logout routes all land here.
	Router chi.Router
	// ClientRouter is the root chi.Router for client-tier raw HTTP
	// routes that don't fit the Huma API surface — i.e. the client-side
	// equivalents of the refresh / refresh-cookie / logout HTTP handlers
	// the auth module mounts on `Router` for the operator side.
	// ADR-0003 PR-D D-5 plumbs this so /v1/auth/client/{refresh,
	// refresh-cookie,logout} can mount on the client host mux. Nil when
	// no client surface exists (single-mux deployments).
	ClientRouter chi.Router
	// APIConfig is the shared Huma API configuration.
	APIConfig huma.Config
	// ConfigService provides runtime module enabled/disabled checks for gate middleware.
	ConfigService *ModuleConfigService
}
