<div align="center">

# Orkestra SDK

**The public contract layer between the [Orkestra](https://github.com/orkestra-cc/orkestra) kernel and every module — core or addon. Implement one Go interface, get a boot kernel that does config, routing, RBAC, multi-tenant scoping, service discovery, and lifecycle for you.**

[![Go Reference](https://pkg.go.dev/badge/github.com/orkestra-cc/orkestra-sdk.svg)](https://pkg.go.dev/github.com/orkestra-cc/orkestra-sdk)
[![Go](https://img.shields.io/badge/Go-1.25-00ADD8?logo=go&logoColor=white&style=flat-square)](https://go.dev)
[![Module](https://img.shields.io/badge/module-github.com%2Forkestra--cc%2Forkestra--sdk-blue?style=flat-square)](https://github.com/orkestra-cc/orkestra-sdk)
[![Latest tag](https://img.shields.io/github/v/tag/orkestra-cc/orkestra-sdk?sort=semver&style=flat-square)](https://github.com/orkestra-cc/orkestra-sdk/tags)
[![License: Apache 2.0](https://img.shields.io/badge/License-Apache_2.0-blue.svg?style=flat-square)](LICENSE)

[Install](#install) · [The Module contract](#the-module-contract) · [Quick example](#quick-example) · [Package map](#package-map) · [Versioning](#versioning-policy) · [Monorepo](https://github.com/orkestra-cc/orkestra)

</div>

---

## What this is

The Orkestra backend is a Go modular monolith. **Every module — the six always-loaded core modules (`user`, `auth`, `authz`, `tenant`, `notification`, `navigation`) and every optional addon (billing, documents, payments, RAG, …) — implements the same `Module` interface defined in this SDK.** The kernel takes care of the rest: topo-sorting init order from declared dependencies, seeding config to MongoDB + caching in Redis, mounting Huma v2 routes, gating disabled modules at the middleware layer, stamping every Mongo write with the caller's tenant scope, and wiring cross-module services through a typed registry.

This module is the **public surface** of that contract. An addon hosted in its own GitHub repo `go get`s exactly the same packages the monorepo consumes — interfaces, DTOs, helpers — and produces a binary the upstream kernel can boot without any custom glue.

The SDK is intentionally narrow:

- **Three required methods** on `Module` (`Name`, `Category`, `Init`) — frozen at v1, will never grow.
- **Sixteen optional sub-interfaces** queried via type assertion — `HasConfigSchema`, `HasNavItems`, `HasCollections`, `Startable`, `HasInfraContainers`, … Modules opt into capabilities by implementing what they need.
- **Five third-party deps** (`huma/v2`, `chi/v5`, `google/uuid`, `prometheus/client_golang`, `mongo-driver`). No transitive auth library, no encoder family, no driver lock-in.
- **Zero imports from `backend/internal/`.** The SDK is self-contained — see [Self-containment invariant](#self-containment-invariant).

## Install

```bash
go get github.com/orkestra-cc/orkestra-sdk@latest
```

Requires Go 1.25 or newer.

```go
import (
    "github.com/orkestra-cc/orkestra-sdk/module"
    "github.com/orkestra-cc/orkestra-sdk/iface"
    "github.com/orkestra-cc/orkestra-sdk/ctxauth"
    "github.com/orkestra-cc/orkestra-sdk/tenantrepo"
)
```

## The Module contract

`module.Module` is the entire required surface — three methods, all called by the registry at boot:

```go
type Module interface {
    Name() string                       // "billing", "documents", "my-addon"
    Category() ModuleCategory           // CategoryCore | CategoryToggleable | CategoryExternal
    Init(deps *Dependencies) error      // wire repositories, services, handlers
}
```

Everything else is opt-in. Implement the sub-interface; the registry detects it and calls it.

| Sub-interface | When the registry uses it |
| --- | --- |
| `HasDisplayInfo` | `/admin/modules` listing |
| `HasConfigSchema` | Seeds defaults to MongoDB, renders admin form, decrypts secrets |
| `HasCollections` | Auto-creates MongoDB collections + declared indexes at boot |
| `HasNavItems` | Aggregates sidebar entries into the `/v1/navigation` response |
| `HasPermissions` | Upserts permissions into the central RBAC catalog |
| `HasCapabilities` | Registers subscription-gated capabilities |
| `HasDependencies` | Topo-sorts init order (your module inits after its dependencies) |
| `HasServiceContracts` | Validates `Provides` / `Requires` / `Optional` keys against the live `ServiceRegistry` |
| `HasDefaultEnabled` | Sets first-install activation state |
| `HasRoutes` | Mounts your Huma v2 / Chi routes (registry handles the disabled-module gate) |
| `Startable` | Starts background jobs, pollers, workers when the module is enabled |
| `Stoppable` | Cleanly shuts down those workers when the module is disabled or the server exits |
| `HealthCheckable` | Reports module health to `/v1/admin/modules/health` |
| `HasInfraContainers` | Declares Docker sidecars (e.g. Gotenberg, Memgraph) the kernel manages alongside the module |
| `HasInternalRoutes` | Service-to-service routes (e.g. the AI-sidecar split's `/v1/internal/*`) |
| `HasMigrations` | Per-module schema migrations the registry runs at boot |

Embed `module.BaseModule` to get sensible no-op defaults for every sub-interface and override only what you need.

## Quick example

A complete addon — `module.go` plus a single route — that uses the SDK end-to-end.

```go
package hello

import (
    "context"
    "errors"
    "fmt"

    "github.com/danielgtaylor/huma/v2"
    "github.com/orkestra-cc/orkestra-sdk/ctxauth"
    "github.com/orkestra-cc/orkestra-sdk/module"
)

// Settings is what `module:"key"` tags bind to. The registry decrypts
// secret fields automatically before unmarshaling.
type Settings struct {
    Greeting string `module:"greeting"`
    APIKey   string `module:"apiKey" secret:"true"`
}

type Module struct {
    module.BaseModule
    settings Settings
}

func New() module.Module { return &Module{} }

func (m *Module) Name() string                  { return "hello" }
func (m *Module) Category() module.ModuleCategory { return module.CategoryToggleable }

func (m *Module) ConfigSchema() []module.ConfigField {
    return []module.ConfigField{
        {Key: "greeting", Type: module.FieldString, Default: "hello", EnvVar: "HELLO_GREETING"},
        {Key: "apiKey", Type: module.FieldString, Secret: true, EnvVar: "HELLO_API_KEY"},
    }
}

func (m *Module) Init(deps *module.Dependencies) error {
    if err := module.UnmarshalModule(deps.ConfigService, m.Name(), &m.settings); err != nil {
        return fmt.Errorf("hello: load settings: %w", err)
    }
    return nil
}

// HasRoutes — mount whatever Huma routes your module exposes.
func (m *Module) RegisterRoutes(api huma.API) {
    huma.Get(api, "/v1/hello", func(ctx context.Context, _ *struct{}) (*GreetingOut, error) {
        uid, ok := ctxauth.GetUserUUID(ctx)
        if !ok {
            return nil, huma.Error401Unauthorized("not signed in")
        }
        return &GreetingOut{Body: GreetingBody{
            Message: fmt.Sprintf("%s, %s", m.settings.Greeting, uid),
        }}, nil
    })
}

type GreetingOut struct{ Body GreetingBody }
type GreetingBody struct{ Message string `json:"message"` }

var _ = errors.New // keep imports honest in the snippet
```

The same pattern works for billing, invoicing, payments — anything you want to plug into Orkestra. The kernel boots your module, mounts the route, gates it behind `/admin/modules` toggle state, and enforces tenant scope on every Mongo query you make through `tenantrepo`.

For the full new-developer walkthrough — `Dependencies` fields, `ConfigService` reads, `ServiceRegistry` typed getters, `tenantrepo.Scope` invariants, an end-to-end addon checklist — see [`docs/onboarding/orkestra-sdk.md`](https://github.com/orkestra-cc/orkestra/blob/main/docs/onboarding/orkestra-sdk.md) in the monorepo.

## Package map

| Package | Purpose |
| --- | --- |
| [`module`](module/) | The `Module` interface + 16 optional sub-interfaces, `BaseModule`, `ModuleRegistry`, `ServiceRegistry`, `ConfigService` (with AES-256-GCM secret helpers), `RouteInfo`, `RedisClient`, `Dependencies`, `PlatformInfo`. The boot kernel. |
| [`iface`](iface/) | Cross-module interfaces — `UserProvider`, `TenantProvider`, `AuthzProvider`, `NotificationSender`, `JWTProvider`, `PDFProvider`, `AIModelProvider`, `RAGQueryProvider`, `AuditSink`, `BillingTenantProvider`, `PaymentProvider`, … — plus their DTOs (`User`, `OAuthLink`, `Tenant`, `NotificationRequest`, …). |
| [`ctxauth`](ctxauth/) | Typed accessors over the request context: `GetUserUUID`, `GetTenantID`, `GetTenantRoles`, `GetClientIP`, `IsImpersonating`, `TenantKindFromContext`, plus the exported `Key*` constants the upstream `AuthMiddleware` writes against. |
| [`modulegate`](modulegate/) | `ModuleGate(checker, name)` HTTP middleware — returns 503 when its module is disabled — and the `ModuleEnabledChecker` interface. |
| [`tenantrepo`](tenantrepo/) | Fail-closed MongoDB helpers: `Scope`, `MustScope`, `StampInsert`, `StampInsertM`, `ScopeAggregate`, `RequireInternalTenant`, `RequireExternalTenant`, plus `ErrTenantScopeMissing` / `ErrTenantKindMismatch` sentinels. |
| [`capability`](capability/) | `Capability` struct + `Registry`. The unit a Tier-2 tenant subscribes to. |
| [`metrics`](metrics/) | Default Prometheus registry + a `Default` snapshot for module-level instrumentation. |

The `iface` package is intentionally additive — a new method on an existing interface is a breaking change for every external implementor, so new capabilities ship as new sub-interfaces (`HasFooProvider`) and the kernel probes them with `module.GetTyped[T]`.

## Versioning policy

The SDK follows [Semantic Versioning](https://semver.org). It is currently on the path to v1.0:

- **`v0.x`** — additive surface, breaking changes allowed at minor bumps when a kernel refactor demands it. Each release notes any breakage. Pin to a tag in production.
- **`v1.x`** — `Module` interface and every interface in `iface/` are frozen. New capabilities only via new optional sub-interfaces and new optional DTO fields (pointer types or `omitempty`).

The kernel guarantees:

- **The three `Module` methods are frozen forever** — `Name`, `Category`, `Init`. New module features go behind new optional sub-interfaces.
- **New DTO fields are additive** — old implementations keep compiling and the field is ignored at runtime.
- **No new third-party dependencies without a deliberate decision.** Every transitive dep is a forced transitive on every external addon. The current five are intentional.

See the upstream [CLAUDE.md](CLAUDE.md) for the full set of rules, and [`docs/plans/orkestra-sdk-split.md`](https://github.com/orkestra-cc/orkestra/blob/main/docs/plans/orkestra-sdk-split.md) in the monorepo for the rollout history.

## Self-containment invariant

**No file in this module may import anything under `backend/internal/` in the upstream monorepo.** Encryption helpers, Redis interfaces, error sentinels, user DTOs — everything the SDK needs lives inside this tree. The check is a single grep:

```bash
grep -rn '"github.com/orkestra/backend/internal/' .
```

A clean run shows zero hits. Any new import that would tie the SDK to backend-private code must instead be added as an interface in `iface/` (consumed via `ServiceRegistry`) or inlined into the relevant `pkg/sdk/` package.

This rule exists because the SDK is shipped to external addon repositories that have no access to `backend/internal/*` — Go's `internal/` visibility rule applies on the import path, regardless of source layout.

## Where the source lives

The SDK is developed in-tree at [`backend/pkg/sdk/`](https://github.com/orkestra-cc/orkestra/tree/main/backend/pkg/sdk) inside the [orkestra-cc/orkestra](https://github.com/orkestra-cc/orkestra) monorepo and mirrored to this standalone repository at every tagged release. The monorepo binds the in-tree path back to the public module via a `replace` directive plus a repo-root `go.work` file, so cross-cutting changes round-trip without a tag bump during development.

The upstream backend's `go.mod` looks roughly like this:

```go
require github.com/orkestra-cc/orkestra-sdk v0.2.0

replace github.com/orkestra-cc/orkestra-sdk => ./pkg/sdk
```

External addons drop the `replace` and consume the published version through the Go module proxy. Once cross-cutting churn settles, the upstream `replace` will be retired too.

## Used by

- [orkestra-cc/orkestra](https://github.com/orkestra-cc/orkestra) — the upstream monorepo. All 6 core modules and every optional addon implement this SDK.
- [orkestra-cc/orkestra-addon-documents](https://github.com/orkestra-cc/orkestra-addon-documents) — first addon extracted to its own repository (Phase 5a). PDF generation via Gotenberg.

External addons that want to be listed here — open a PR.

## Contributing

Issues and PRs against the SDK itself are welcome here, but most development happens in the [upstream monorepo](https://github.com/orkestra-cc/orkestra) where the SDK lives alongside the kernel and its consumers — so a change can be implemented, tested, and reviewed against real callers in one diff. See [CONTRIBUTING.md](https://github.com/orkestra-cc/orkestra/blob/main/CONTRIBUTING.md) in the monorepo for the contributor flow.

## License

Licensed under the [Apache License, Version 2.0](LICENSE).
