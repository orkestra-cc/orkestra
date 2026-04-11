# Module: Navigation — Dynamic menu aggregator

_Path: `/backend/internal/core/navigation`_
_Parent: [../CLAUDE.md](../CLAUDE.md)_

[← Core](../CLAUDE.md) | [☰ Backend](../../../CLAUDE.md) | [Root](../../../../CLAUDE.md)

## Purpose

Collects `NavItems()` from every registered module at boot, filters the result by the caller's system role and the current module enable/disable state, and returns a single JSON tree to the frontend. One endpoint, one responsibility: the frontend sidebar is backend-driven, so adding a menu entry means editing the owning module's `NavItems()`, not the frontend.

No persistence. Every request is computed from the same in-memory `[]NavItemSpec` the registry populated under `ServiceNavItems` at boot.

## What it owns

| File | Purpose |
|---|---|
| `module.go` | Module registration, nav-items + config-service lookup, handler wire-up |
| `routes.go` | Huma route registration (one endpoint) |
| `handlers/navigation_handler.go` | Extracts the system role from the request context and calls the service |
| `services/navigation_service.go` | Navigation service interface |
| `services/dynamic_navigation.go` | Role + enabled-module filtering implementation |
| `models/navigation.go` | `NavigationResponse` DTO returned to the frontend |

## MongoDB collections

None. This module is stateless.

## Dependencies

- **Modules**: none declared.
- **Required services**: none.
- **Optional services** (read at `Init`):
  - `ServiceNavItems` — `[]module.NavItemSpec` populated by the registry before any module's `Init` runs (`shared/module/registry.go:153`). Empty slice if no modules declared nav items.
  - `ServiceConfigService` — `*ModuleConfigService`, used as a `middleware.ModuleEnabledChecker` to skip items from disabled modules at request time.
- **Provides**: none (no module consumes navigation; the frontend talks to it directly).
- **Permissions contributed**: none.

## Lifecycle

`Init` (`module.go:22-38`) reads `ServiceNavItems` and `ServiceConfigService` from the registry, builds a `DynamicNavigationService`, and wraps it in a handler. `Start`, `Stop`, and `HealthCheck` are no-ops inherited from `BaseModule`.

There is no seeding. The nav-items list is assembled by the registry in `InitAll` (`shared/module/registry.go:146-153`) which walks every registered module's `NavItems()` method, stamps each item with the owning module name, and stores the aggregated slice under `ServiceNavItems` before any module gets its `Init` call — which is why navigation (which reads that key during its own init) always sees the full set.

## HTTP endpoints

| Method | Path | Gate | Purpose |
|---|---|---|---|
| GET | `/v1/navigation` | Bearer token (protected router) | Return the role-filtered menu tree for the current user |

Registered in `routes.go::RegisterRoutes`. The route is mounted on the protected router in `module.go:40-43`, so it requires a valid access token but no specific permission.

## Service contract

None. Navigation does not expose anything in `shared/iface` — no other backend module consumes it.

## Key invariants

- **Filter key is the user's system role.** `GetNavigation` (`handlers/navigation_handler.go:32-47`) reads `middleware.GetSystemRole(ctx)` and falls back to `"guest"` when the context has no role (`:37-39`). This is a coarse-grained filter — it does not know about per-org authz bindings or effective permissions.
- **Each `NavItemSpec` carries a `minRole` string.** Items are included if the user's system role is at or above `minRole` in the hierarchy. Hierarchy order matches `ROLE_HIERARCHY` on the frontend: `super_admin > administrator > developer > manager > operator > guest`.
- **Module enable/disable filtering.** If `ServiceConfigService` was present at init, the service uses it as a `ModuleEnabledChecker` and filters out items whose owning module is currently disabled (`module_configs.enabled == false`). The check is performed per request, cached through the config service's Redis layer — so toggling a module in `/admin/modules` takes effect on the next nav fetch without a backend restart.
- **Stateless.** No DB writes, no background jobs. All state is the in-memory slice built at boot.
- **Module-name stamping.** The registry sets `ModuleName` on every `NavItemSpec` and recursively on every child item before it hands them to navigation. This is what the enabled-module filter keys off.

## What this module does NOT do

- Persist user-specific preferences (favorites, collapsed groups, reordering) — not implemented
- Evaluate fine-grained permissions (e.g., "only show Invoices if the user has `billing.invoice.read` in this org") — filter is system-role-only today
- Return a route map or permission gates to the frontend — it only returns human-readable menu entries; the frontend's React Router still has to have the route registered
- Hide a menu item because its *target* module is failing its health check — only explicit disabled state is honored

## Rules

- **Never hardcode menu entries in this module.** Every item must come from some other module's `NavItems()`. If you find yourself adding items in `navigation_service.go`, the entry belongs in the owning module instead.
- **Never return the raw `NavItemSpec`** — wrap in `NavigationResponse` (`models/navigation.go`). The wrapper is the frontend contract; the spec is an internal detail.
- **Never fall back to the guest tier silently** for a user with a non-empty token but unresolvable role. Log it — it means the system-role field is corrupt or a rename migration was missed.
- **When the authz permission-domain-tag refactor lands**, replace the system-role filter with `authz.GetEffectivePermissions` and filter items by a `RequiredPermission` field on `NavItemSpec`. This is flagged explicitly as known limitation.

## Known limitation

Filtering by a single global system role means this module cannot express "show `Billing` in org A but not org B for the same user". The underlying `authz` module already has per-org effective permissions; the hook-up is future work tied to the permission-domain-tag refactor. Until then, the frontend compensates by hiding a per-org menu section when `authz.GetEffectivePermissions` returns an empty set for that org — but the returned tree still contains every item the role allows.

## Related

- [`../../shared/module/registry.go:146-153`](../../shared/module/registry.go) — where nav items are collected and stamped with owner
- [`../../shared/module/module.go`](../../shared/module/module.go) — `NavItemSpec` type definition
- [`../../shared/middleware/module_enabled.go`](../../shared/middleware/module_enabled.go) — `ModuleEnabledChecker` interface
- [`../authz/CLAUDE.md`](../authz/CLAUDE.md) — the provider the filter should eventually consult
- [`../../../../frontend/CLAUDE.md`](../../../../frontend/CLAUDE.md) — how the frontend consumes `/v1/navigation`
