# Orkestra SDK — Developer Onboarding

_Audience: backend developers writing or modifying Orkestra modules (core or addons)._
_Last refresh: 2026-05-13, after Phase 3 of the SDK split._

## What the SDK is

The Orkestra SDK is the **contract layer** between the backend kernel and
every module — the six core modules (`user`, `auth`, `authz`, `tenant`,
`notification`, `navigation`) plus every optional addon (`billing`,
`rag`, `payments`, …). It lives at `backend/pkg/sdk/` as its own Go
module:

```
module github.com/orkestra-cc/orkestra-sdk
```

A repo-root `go.work` file binds it to the main backend module so both
build together. The Phase-4 plan ([docs/plans/orkestra-sdk-split.md][1])
publishes this same package to its own GitHub repo so addons can move
out of the monolith without breaking; from your perspective as a
contributor, the API is the same whether you're in-tree or extracted.

If you're writing a module — **anything that boots, registers routes,
and owns config or data** — this document is the one to read.

[1]: ../plans/orkestra-sdk-split.md

## Why it exists

Three problems the SDK solves:

1. **Cross-module references** without import cycles. Auth needs the user
   provider, the navigation module needs every other module's nav items,
   and so on. Instead of each module importing every other's `services/`
   package, they share a single `iface` package of plain Go interfaces
   that every module imports and every provider satisfies via Go's
   structural typing.

2. **A stable boundary so addons can be extracted.** Today every addon
   lives under `backend/internal/addons/`. The SDK boundary is the
   contract that lets us pull one out into its own repo (e.g.
   `orkestra-cc/addon-billing`) and have it still compile against the
   monolith via `go get`. Anything inside the SDK is **frozen public
   API**; anything outside is backend-private and can change at any
   time.

3. **A minimum-viable module contract.** A module must implement only
   three methods (`Name`, `Category`, `Init`); every other capability —
   config schemas, nav items, lifecycle hooks, infra containers — lives
   behind an optional sub-interface queried via type assertion. New
   capabilities can be added as new sub-interfaces without breaking any
   existing module.

## Where the SDK sits in the project

```
orkestra/
├── go.work                            ← binds the two Go modules below
├── backend/
│   ├── go.mod                         module github.com/orkestra/backend
│   ├── cmd/                           binaries: cmd/server, cmd/ai-service
│   ├── internal/
│   │   ├── core/                      6 always-loaded modules
│   │   │   ├── user/  auth/  authz/  tenant/  notification/  navigation/
│   │   └── addons/                    13 optional addons (billing, rag, ...)
│   │   └── shared/                    backend-PRIVATE infra (config, errors, db, ...)
│   │
│   └── pkg/sdk/                       ← THIS IS THE SDK
│       ├── go.mod                     module github.com/orkestra-cc/orkestra-sdk
│       ├── capability/                Capability type (entitlement IDs)
│       ├── ctxauth/                   request-context getters (GetUserUUID, ...)
│       ├── iface/                     cross-module interfaces + DTOs
│       ├── metrics/                   prometheus registry
│       ├── module/                    Module interface, Registry, ConfigService
│       ├── modulegate/                ModuleGate HTTP middleware
│       └── tenantrepo/                tenant-scoping helpers for MongoDB queries
```

**Self-containment invariant:** no file inside `pkg/sdk/` imports
anything from `backend/internal/`. CI doesn't enforce this explicitly
yet — verify with `grep -rn "internal/" backend/pkg/sdk/ --include="*.go"`
before any PR that touches SDK code (the few hits should all be in
doc-comments, not code).

## The seven SDK packages

| Package | Purpose | What you'll use most |
| --- | --- | --- |
| `module` | Module interface + lifecycle, ServiceRegistry, ConfigService, BaseModule, Dependencies | Almost every file in your module |
| `iface` | Cross-module interfaces and DTOs (UserProvider, TenantProvider, AuditSink, User, OAuthLink, …) | Whenever you need data or behaviour from another module |
| `ctxauth` | Request-context getters: `GetUserUUID(ctx)`, `GetTenantID(ctx)`, `IsImpersonating(ctx)` | Every HTTP handler |
| `modulegate` | The 503-on-disabled middleware that wraps every non-core module's routes | `RegisterRoutes` |
| `tenantrepo` | Tenant-scoped MongoDB query helpers (`Scope`, `StampInsert`, `RequireInternalTenant`) | Every repository method |
| `capability` | The Capability type used by tenant entitlements | When declaring `Capabilities()` |
| `metrics` | Default Prometheus registry | Background workers, polling jobs |

## The Module contract

The full interface is in `pkg/sdk/module/module.go`:

```go
// Required (every module must implement these three)
type Module interface {
    Name() string                   // unique identifier — "billing", "rag", ...
    Category() ModuleCategory       // CategoryCore | CategoryToggleable | CategoryExternal
    Init(deps *Dependencies) error  // wire repos, services, handlers
}
```

That's it for the required surface. Everything else is **optional**,
declared via a sub-interface the registry queries with a type assertion:

| Sub-interface | What it gives you |
| --- | --- |
| `HasDisplayInfo` | `DisplayName()` + `Description()` shown in the admin UI |
| `HasConfigSchema` | `ConfigSchema()` — admin-editable config fields (auto-seeded from env vars on first boot) |
| `HasCollections` | `Collections()` — MongoDB collections + indexes the registry auto-creates |
| `HasNavItems` | `NavItems()` — entries added to the dynamic sidebar |
| `HasPermissions` | `Permissions()` — declared into the authz catalog |
| `HasCapabilities` | `Capabilities()` — entitlement IDs tenants subscribe to |
| `HasDependencies` | `Dependencies()` — names of modules that must init before yours |
| `HasServiceContracts` | `ProvidedServices` + `RequiredServices` + `OptionalServices` |
| `HasDefaultEnabled` | `Enabled() bool` — first-boot activation default (true if not implemented) |
| `HotReloadable` | `HotReloadConfig() bool` — read config lazily so admin edits take effect without restart |
| `Routable` | `RegisterRoutes(ri *RouteInfo)` — register HTTP endpoints |
| `Startable` | `Start(ctx)` — background goroutines, polling jobs |
| `Stoppable` | `Stop(ctx)` — graceful shutdown of whatever `Start` started |
| `HealthCheckable` | `HealthCheck(ctx) error` — polled by `/admin/modules/health` |
| `HasInfraContainers` | `InfraContainers()` — Docker containers the registry brings up alongside your module (Memgraph, Hindsight) |
| `HasPreflight` | `Preflight(ctx) error` — pre-Start validation that can refuse activation |

You won't implement all 16. Most addons embed `BaseModule` (next
section) and override only the ones they actually need.

### BaseModule — the ergonomic helper

```go
type MyModule struct {
    module.BaseModule              // ← embed this
    handler *handlers.MyHandler
}
```

`BaseModule` implements every sub-interface with a sensible default
(empty slices, `nil` errors, `true` for `Enabled`). You override
whichever methods you need; the registry sees the embedded defaults via
type assertion for everything else. The 14 in-tree modules all use this
pattern.

When the SDK extracts to its own repo, an addon can choose to skip
`BaseModule` and implement only the interfaces it actually uses — the
registry handles both shapes identically.

## The lifecycle, end to end

```
                              ┌─────────────────────────┐
                              │ cmd/server/main.go      │
                              └──────────┬──────────────┘
                                         │
                              modRegistry.Register(yourModule)
                                         │
                                         ▼
            ┌─────────────────── modRegistry.InitAll(deps) ─────────────────────┐
            │                                                                  │
            │  1. ensureCollections (auto-creates MongoDB collections           │
            │     declared by HasCollections, with their indexes)               │
            │                                                                  │
            │  2. SeedFromModules → buildInitialConfig for each module:        │
            │       reads ConfigSchema, populates env-var defaults,            │
            │       writes module_configs document on first boot               │
            │       (calls m.Enabled() for the initial enabled flag)          │
            │                                                                  │
            │  3. Collects HasNavItems → ServiceNavItems                       │
            │                                                                  │
            │  4. Topologically sorts by HasDependencies                       │
            │                                                                  │
            │  5. For each module in dep order:                                │
            │       err := m.Init(deps)                                         │
            │       (you wire repos, services, handlers here)                  │
            │                                                                  │
            │  6. Collects HasPermissions → authz catalog upsert               │
            │                                                                  │
            │  7. Collects HasCapabilities → capability registry               │
            └──────────────────────────────┬───────────────────────────────────┘
                                           │
                              RegisterAllRoutes(ri):
                                           │
                                           ▼
                              for each module:
                                  if module implements Routable:
                                      module.RegisterRoutes(ri)
                                           │
                                           ▼
                              StartAll(ctx, startSet):
                                           │
                                           ▼
                              for each enabled module:
                                  if HasPreflight: Preflight(ctx)         ← bail before side effects
                                  if HasInfraContainers: spin them up      ← Docker containers
                                  if Startable: Start(ctx)                 ← background workers

                              ┌─ admin enables/disables a module at runtime ─┐
                              │ POST /v1/admin/modules/billing               │
                              │   { "enabled": true }                        │
                              │                                              │
                              │ → registry.StartModule("billing")            │
                              │   runs Preflight + InfraContainers + Start   │
                              │                                              │
                              │ → registry.StopModule("billing")             │
                              │   runs Stop + tears down containers          │
                              └──────────────────────────────────────────────┘
```

Three key invariants:

- **`Init` runs once at boot. `Start` / `Stop` may run repeatedly** as
  admins toggle the module on/off without restarting the backend.
- **A failed `Init` for a non-core module is non-fatal** — the registry
  logs and gates the routes behind `ModuleGate` so they return 503. A
  failed `Init` for a core module is fatal.
- **`RegisterRoutes` registers routes regardless of enabled state.**
  `ModuleGate` middleware is what turns disabled-module requests into
  503s. This is why an admin can flip a module on/off without touching
  routing.

## `Init(deps *Dependencies)` — what you get

```go
type Dependencies struct {
    DB            *mongo.Database
    RedisAdapter  RedisClient           // SDK interface, satisfied by *database.RedisClientAdapter
    Platform      PlatformInfo          // IsProduction / IsStaging / FrontendURL / GetEnvironment
    Logger        *slog.Logger
    Services      *ServiceRegistry      // cross-module service handles
    ConfigService *ModuleConfigService  // module config, UnmarshalModule, etc.
}
```

What to use what for:

- **`deps.DB`** — your MongoDB handle. Pass to your repository
  constructors.
- **`deps.RedisAdapter`** — the `RedisClient` interface (Get/Set/Del/Keys).
  Pass to anything that caches.
- **`deps.Platform`** — environment classification and the SPA URL.
  Anything that branches on prod-vs-dev or builds verification links
  uses this.
- **`deps.Logger`** — your structured logger.
- **`deps.Services`** — the typed service registry. Pull cross-module
  providers from here (see "Talking to other modules" below).
- **`deps.ConfigService`** — read your own module's runtime config via
  `UnmarshalModule` (see next section).

There's no `Dependencies.Config *config.Config` field. The auth core
module is the only consumer of the backend's app-wide config, and it
takes `cfg` via its own `NewModule(cfg *config.Config)` constructor —
see `cmd/server/catalog.go` for how `main.go`'s cfg threads through
the factory closure. Addons that need a piece of backend config they
can't get from `Platform` should request a typed service via
`deps.Services`.

## Module configuration

Every module that needs runtime-editable settings declares a
`ConfigSchema`:

```go
func (m *BillingModule) ConfigSchema() []module.ConfigField {
    return []module.ConfigField{
        {Key: "apiKey", Label: "API Key", Type: module.FieldSecret,  EnvVar: "OPENAPI_BILLING_API_KEY"},
        {Key: "timeout", Label: "Timeout", Type: module.FieldDuration, Default: "30s", EnvVar: "OPENAPI_BILLING_TIMEOUT"},
        {Key: "pollingEnabled", Label: "Polling", Type: module.FieldBool, Default: "true"},
        // ...
    }
}
```

Field types: `FieldString`, `FieldBool`, `FieldInt`, `FieldDuration`,
`FieldSecret` (AES-256-GCM at rest), `FieldEnum`, `FieldStringList`.

The registry seeds these into the `module_configs` MongoDB collection on
first boot — env-var values become the initial state, then the admin UI
at `/admin/modules/<name>` is authoritative. The Redis layer caches
`IsEnabled(name)` for 30s.

### Reading config: the `UnmarshalModule` pattern

Inside `Init`, declare a typed `Settings` struct that mirrors your
schema and unmarshal:

```go
type Settings struct {
    APIKey         string        `module:"apiKey"`
    Timeout        time.Duration `module:"timeout"`
    PollingEnabled bool          `module:"pollingEnabled"`
}

func (m *BillingModule) Init(deps *module.Dependencies) error {
    var s Settings
    if err := deps.ConfigService.UnmarshalModule(context.Background(), m.Name(), &s); err != nil {
        return err
    }
    if s.Timeout <= 0 {
        s.Timeout = 30 * time.Second  // defensive guard when schema default is missing
    }

    // ... use s.APIKey, s.Timeout, etc.
}
```

`UnmarshalModule` resolves each field with the same precedence the
admin UI shows: **active-environment DB value → schema `Default` →
schema `EnvVar`**. `FieldSecret` fields are decrypted with the
`OAUTH_TOKEN_ENCRYPTION_KEY`. Decryption failures return an error from
`Init`, which is the right outcome — silently using empty secrets is
worse than a loud boot failure.

The struct-tag convention: `module:"keyName"` matches the `Key` in
`ConfigField`. Without a tag the field name's first rune is lowercased
(e.g. `APIKey` → `aPIKey`) — explicit tags are clearer.

### Dynamic reload (hot-reloadable modules)

For modules that need admin edits to take effect without a restart
(billing, aimodels), call `UnmarshalModule` inside a closure that the
service captures:

```go
configLoader := func() *config.OpenAPIConfig {
    var s Settings
    if err := deps.ConfigService.UnmarshalModule(context.Background(), m.Name(), &s); err != nil {
        deps.Logger.Warn("config reload failed", "error", err)
        return &config.OpenAPIConfig{}  // graceful degradation
    }
    return &config.OpenAPIConfig{ APIKey: s.APIKey, /* ... */ }
}

m.client = services.NewOpenAPIClientWithCache(configLoader, /* ... */)
```

Then declare `HotReloadConfig() bool { return true }` so the admin UI
labels the module accordingly.

## Talking to other modules: the ServiceRegistry

Modules never import each other's packages directly. Instead they
publish + consume **typed services** through `deps.Services`:

```go
// In tenant's Init():
deps.Services.Register(module.ServiceTenantProvider, m.svc)

// In billing's Init(), pulling tenant's provider:
tenantProvider, ok := module.GetTyped[iface.TenantProvider](deps.Services, module.ServiceTenantProvider)
if !ok { /* missing required dependency */ }
```

Two flavors of lookup:

- **`module.GetTyped[T]`** — optional. Returns `(T, false)` if absent.
  Graceful-degradation path.
- **`module.MustGetTyped[T]`** — required. Panics if missing.

Service keys are declared once in `pkg/sdk/module/services.go` (e.g.
`ServiceUserService`, `ServiceTenantProvider`, `ServiceAuditSink`,
`ServiceAIModelProvider`). When you publish a new service add the key
constant there.

The interfaces themselves live in `pkg/sdk/iface/`. If you need to
expose a new provider from your module, add the interface to `iface/`
first, then have your module's service satisfy it via structural typing.

## HTTP routes

```go
func (m *BillingModule) RegisterRoutes(ri *module.RouteInfo) {
    ri.Operator.ProtectedRouter.Group(func(r chi.Router) {
        r.Use(modulegate.ModuleGate(ri.ConfigService, m.Name()))     // 503 when disabled
        r.Use(ri.Operator.AuthMW.RequireInternalTenant())            // tier gate
        r.Use(ri.Operator.AuthMW.RequirePermission("billing.invoice.read"))
        api := humachi.New(r, ri.APIConfig)
        RegisterRoutes(api, m.invoiceHandler, /* ... */)
    })
}
```

`RouteInfo` exposes two audiences:

- **`ri.Operator`** — operator console (Tier-1, `console.*` host). Has
  `AuthMW` (full `AuthMiddleware`), `PublicAPI`, `ProtectedRouter`.
- **`ri.Client`** — Tier-2 customer-facing surface (`api.*` host). Same
  shape.

The three middleware patterns you'll combine:

| Middleware | What it gates |
| --- | --- |
| `modulegate.ModuleGate(cs, name)` | Module enabled at runtime |
| `ri.Operator.AuthMW.RequireInternalTenant()` | Caller acts in a Tier-1 tenant |
| `ri.Operator.AuthMW.RequirePermission("x.y.z")` | RBAC check via authz |

Reading the request context inside handlers — use `ctxauth`, not raw
context keys:

```go
import "github.com/orkestra-cc/orkestra-sdk/ctxauth"

func (h *Handler) Get(ctx context.Context, req *Req) (*Resp, error) {
    userUUID, ok := ctxauth.GetUserUUID(ctx)
    if !ok { /* unauthenticated */ }
    tenantID, _ := ctxauth.GetTenantID(ctx)
    if ctxauth.IsImpersonating(ctx) {
        // refuse self-destructive actions while admin is acting-as someone
    }
    // ...
}
```

Full list of `ctxauth` getters: `GetUserUUID`, `GetUserEmail`,
`GetSystemRole`, `GetTenantID`, `GetTenantRoles`, `GetClientIP`,
`IsImpersonating`, `TenantKindFromContext`. Plus setters
`WithTenantKind`, `WithClientIP` for tests.

The claim-derived signals (`GetSessionID`, `GetAMR`, `IsMFAEnrolled`)
stay in `internal/shared/middleware` — they depend on the auth
module's claims type, which doesn't belong in the SDK. A future
refactor lifts them behind an `iface.ClaimsAccessor` contract if
an extracted addon genuinely needs them; today the only consumers
are backend-internal (middleware itself + authz).

## Repository pattern: tenant scoping is mandatory

Every MongoDB read or write from a non-core module **must** scope by
tenant. The `tenantrepo` package makes this fail-closed:

```go
import "github.com/orkestra-cc/orkestra-sdk/tenantrepo"

func (r *InvoiceRepository) Find(ctx context.Context, status string) ([]Invoice, error) {
    filter, err := tenantrepo.Scope(ctx, bson.M{"status": status})
    if err != nil {
        return nil, err  // missing tenant context → 403 in production, panic in dev
    }
    cur, err := r.coll.Find(ctx, filter)
    // ...
}

func (r *InvoiceRepository) Insert(ctx context.Context, doc bson.M) error {
    doc, err := tenantrepo.StampInsertM(ctx, doc)
    if err != nil {
        return err
    }
    _, err = r.coll.InsertOne(ctx, doc)
    return err
}
```

Other helpers: `MustScope` (panics on missing context — for routes
statically known to be authenticated), `ScopeAggregate` (prepends a
`$match` stage to an aggregation pipeline), `RequireInternalTenant` /
`RequireExternalTenant` (refuses cross-tier operations).

A CI gate enforces this. The `tenantscope` static analyzer
(`backend/tools/tenantscope/`) scans every `*.go` file under
`backend/internal/` and flags any `collection.Find/Update/Delete` whose
filter doesn't trace back to a `tenantrepo` helper. Skipping the helper
is the most common cause of cross-tenant data leaks, so the check is
non-negotiable. Suppress only with an explicit comment:
`//tenantscope:allow webhook: stripe event keyed by provider-id`.

## Cross-module contracts: `iface`

`pkg/sdk/iface/interfaces.go` is the single source of truth for the
interfaces every module shares. The biggest ones you'll encounter:

| Interface | Implemented by | Used by |
| --- | --- | --- |
| `UserProvider` | `user` | auth, authz, tenant, every admin page |
| `TenantProvider` | `tenant` | auth, billing, payments, subscriptions, every tenant-scoped route |
| `AuthzProvider` | `authz` | auth middleware on every protected request |
| `NotificationSender` | `notification` | auth (verification + reset), subscriptions (dunning) |
| `JWTProvider` | `auth` | dev token generator |
| `PDFProvider` | `documents` | billing (invoice rendering) |
| `AIModelProvider` | `aimodels` | rag, sales, agents |
| `RAGQueryProvider` | `rag` | agents |
| `AuditSink` | `compliance` | every module that emits audit events |
| `BillingTenantProvider` | `tenant` | billing (CessionarioCommittente snapshot) |
| `PaymentProvider` | `payments` | subscriptions |

DTOs (e.g. `User`, `OAuthLink`, `Tenant`, `NotificationRequest`,
`GeneratedDocument`) also live in `iface/` so signatures can reference
them without each consumer importing the producer's package.

If you find yourself wanting to import another module's `services/`
package — stop. Add an interface to `iface/`, have the producer satisfy
it, and consume through `deps.Services`. There's exactly one allowed
shape for cross-module communication.

## Adding a new addon — checklist

1. **Create the module directory:** `backend/internal/addons/<name>/`.
   Sub-folders: `handlers/`, `services/`, `repository/`, `models/`,
   `config/` if you have a complex local config type.

2. **Implement the module struct in `module.go`:**
   ```go
   package widgets

   type Module struct {
       module.BaseModule              // ergonomic helper
       handler *handlers.Handler
   }

   func NewModule() *Module { return &Module{} }

   func (m *Module) Name() string                    { return "widgets" }
   func (m *Module) Category() module.ModuleCategory { return module.CategoryToggleable }
   ```

3. **Declare what you need.** Override only the sub-interfaces you use:
   - `ConfigSchema()` if you have runtime config
   - `Collections()` if you own MongoDB collections (and want indexes
     auto-created)
   - `NavItems()` to add a sidebar entry — `Realm` + `Section` + `Tier`
     drive the dynamic menu
   - `Permissions()` to register RBAC permissions
   - `Dependencies()` if you require another module to init first
   - `Init(deps)` to wire your repos, services, handlers
   - `RegisterRoutes(ri)` to register HTTP endpoints
   - `Start(ctx)` / `Stop(ctx)` for background workers
   - `Enabled() bool` if your default first-boot state isn't `true`

4. **Register the factory in the catalog:** create
   `backend/cmd/server/catalog_<name>.go`:
   ```go
   //go:build !no_addons || addon_widgets

   package main

   import (
       "github.com/orkestra/backend/internal/addons/widgets"
       "github.com/orkestra-cc/orkestra-sdk/module"
   )

   func init() {
       optionalModules["widgets"] = func() module.Module { return widgets.NewModule() }
   }
   ```

5. **Add the optional name** to `allOptionalModuleNames` in
   `cmd/server/main.go` so the registry sees it.

6. **Add the frontend slice** if you have UI — see
   `frontend-admin/CLAUDE.md` for the module pattern there. Backend
   declares the nav item, frontend declares the route + RTK Query slice.

7. **Test:** `make backend-test` runs the full backend + SDK suite. Tests
   for your module's services/handlers go in `internal/addons/<name>/services/*_test.go`
   etc. — same package as the code, plain `testing` package.

## Reading list

If you're going to spend serious time in the backend, in priority order:

1. **`backend/CLAUDE.md`** — module system + project structure
2. **`backend/internal/addons/billing/CLAUDE.md`** — most complex addon,
   exercises every capability (config schema, infra containers,
   dependencies, hot reload, dynamic config closures, webhooks)
3. **`backend/internal/addons/subscriptions/CLAUDE.md`** — cleanest
   service-publishing pattern + cycle-free wiring with payments
4. **`backend/internal/core/auth/CLAUDE.md`** — authentication +
   middleware chain
5. **`backend/internal/core/authz/CLAUDE.md`** — RBAC + Cedar ABAC + the
   9 org-scoping invariants every module must respect
6. **[docs/plans/orkestra-sdk-split.md][1]** — full multi-phase plan for
   how the SDK reached its current shape and where it's going

## Where the SDK is

The SDK is published at
[`github.com/orkestra-cc/orkestra-sdk`](https://github.com/orkestra-cc/orkestra-sdk)
since `v0.1.0` (Phase 4). The in-tree source at `backend/pkg/sdk/`
remains the canonical home — `backend/go.mod` carries a `replace`
directive pointing at it so monorepo development uses live source, and
the same code is mirrored to the public repo via a tagged release.
External addons (and Phase-5 addon extractions) `go get` the SDK from
the module proxy and never see the in-tree path.

When the cross-cutting churn settles (no per-PR changes spanning both
repos), the next refactor will drop the `replace` directive and the
backend will fetch the SDK like any other public module. Until then,
write code against the SDK as if it were already external — every
import path is the public path, the contract is the contract, and
the source location is a detail.

## Quick reference

| Need to… | Use |
| --- | --- |
| Read current user from request | `ctxauth.GetUserUUID(ctx)` |
| Read current tenant from request | `ctxauth.GetTenantID(ctx)` |
| Scope a Mongo query by tenant | `tenantrepo.Scope(ctx, filter)` |
| Stamp tenant on insert | `tenantrepo.StampInsertM(ctx, doc)` |
| Reject cross-tier operations | `tenantrepo.RequireInternalTenant(ctx)` |
| Read your module's config | `deps.ConfigService.UnmarshalModule(ctx, m.Name(), &s)` |
| Read another module's published service | `module.MustGetTyped[T](deps.Services, key)` |
| Publish your service for other modules | `deps.Services.Register(key, svc)` |
| Send an email | `iface.NotificationSender.SendTemplated(ctx, req)` |
| Check if running in prod | `deps.Platform.IsProduction()` |
| Gate a route on module-enabled | `modulegate.ModuleGate(ri.ConfigService, m.Name())` |
| Gate a route on auth | `ri.Operator.AuthMW.RequireAuth` (or `RequireGlobal`) |
| Gate a route on tier | `ri.Operator.AuthMW.RequireInternalTenant()` |
| Gate a route on permission | `ri.Operator.AuthMW.RequirePermission("x.y.z")` |
| Run before module starts | implement `HasPreflight.Preflight(ctx)` |
| Run on every module enable | implement `Startable.Start(ctx)` |

## Troubleshooting

**"My module doesn't show up in the admin UI."**
The `module_configs` document is created on the first boot that sees
your module registered. Drop the document (Mongo: `db.module_configs.deleteOne({moduleName: "yours"})`)
and restart — the seeder will recreate it from your `ConfigSchema`.

**"My `Init` panics on `MustGetTyped[T]`."**
You're consuming a required service that didn't register. Either declare
the producer in `Dependencies()` so the topological sort runs it first,
or switch to `GetTyped[T]` and degrade gracefully.

**"My MongoDB insert succeeds in tests but my filter returns nothing in prod."**
You forgot to scope. Production wraps your filter against an empty
`tenantId`. Use `tenantrepo.Scope` / `MustScope` on the read and
`StampInsertM` on the write.

**"`UnmarshalModule` doesn't pick up my new env var."**
First-boot seeding is one-shot — env vars seed the DB document only when
the document doesn't already exist. After that, change values through
`/admin/modules`. To re-seed in dev, drop the row in `module_configs`.

**"Admin enables my module but routes still return 404."**
You didn't wrap your route group in `modulegate.ModuleGate(...)` — the
route is registered unconditionally and Chi serves it whether the
module is enabled or not. Wrap it.

**"How do I run only the SDK tests?"**
`make sdk-test` or `cd backend/pkg/sdk && go test ./...`. The repo-root
`go.work` resolves the workspace; both modules can be tested in
isolation.

---

If something here doesn't match what you see in the code, the code is
right and this doc is stale — file a quick PR to fix it. The SDK shape
is settled (Phase 3 is final until Phase 4 extracts it), so drift
should be rare.
