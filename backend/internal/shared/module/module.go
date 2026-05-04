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
	// Permissions returns the authorization permissions this module exposes.
	// The registry collects these from every module at boot and upserts them
	// into the authz catalog so administrators can bind them to custom roles.
	Permissions() []iface.PermissionSpec
	// Capabilities returns the entitlement-gated capabilities this module
	// exposes. Capabilities are the units a tenant subscribes to — middleware
	// and Cedar policies gate routes by checking whether the current tenant
	// holds an active entitlement to the capability ID. The registry collects
	// these at boot and makes them available via ServiceCapabilityRegistry.
	Capabilities() []capability.Capability
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

	// --- Runtime capabilities ---

	// HotReloadConfig returns true when the module reads its configuration
	// lazily (via a ConfigLoader closure) so that admin-UI changes take
	// effect without a backend restart.  Modules that still read config
	// once during Init should return false (the default in BaseModule).
	HotReloadConfig() bool

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

	// InfraContainers declares Docker containers the registry should start
	// before this module's Start() and stop after its Stop(). Returns nil
	// for modules that don't own external infrastructure.
	InfraContainers() []InfraContainerSpec

	// Preflight validates runtime prerequisites that depend on mutable
	// state (other modules' config, external services, etc.) and must be
	// checked at each toggle-on rather than at Init. The registry calls
	// it before touching infra containers, so a failure here prevents
	// any side effects. BaseModule returns nil by default.
	//
	// Typical use: a module that requires another module's configuration
	// (e.g. agents requires aimodels to have a default LLM set) checks
	// that invariant here and returns a descriptive error that surfaces
	// directly to the admin UI.
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

func (BaseModule) DisplayName() string                 { return "" }
func (BaseModule) Description() string                 { return "" }
func (BaseModule) Category() ModuleCategory            { return CategoryCore }
func (BaseModule) Enabled(_ *config.Config) bool       { return true }
func (BaseModule) ConfigSchema() []ConfigField         { return nil }
func (BaseModule) Collections() []CollectionSpec       { return nil }
func (BaseModule) NavItems() []NavItemSpec             { return nil }
func (BaseModule) Permissions() []iface.PermissionSpec { return nil }
func (BaseModule) Capabilities() []capability.Capability { return nil }
func (BaseModule) Dependencies() []string              { return nil }
func (BaseModule) ProvidedServices() []ServiceKey      { return nil }
func (BaseModule) RequiredServices() []ServiceKey      { return nil }
func (BaseModule) OptionalServices() []ServiceKey      { return nil }
func (BaseModule) HotReloadConfig() bool                { return false }
func (BaseModule) Start(_ context.Context) error       { return nil }
func (BaseModule) Stop(_ context.Context) error        { return nil }
func (BaseModule) HealthCheck(_ context.Context) error { return nil }
func (BaseModule) InfraContainers() []InfraContainerSpec { return nil }
func (BaseModule) Preflight(_ context.Context) error     { return nil }

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
	// error="mfa_required" so the frontend can prompt for a TOTP code and
	// retry against /v1/auth/mfa/verify.
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
