# Module: Navigation — Dynamic menu aggregator

_Path: `/backend/internal/core/navigation`_
_Parent: [../CLAUDE.md](../CLAUDE.md)_

[← Core](../CLAUDE.md) | [☰ Backend](../../../CLAUDE.md) | [Root](../../../../CLAUDE.md)

## Purpose

Collects `NavItems()` from every registered module at boot, filters the result by the caller's system role and the current module enable/disable state, and returns a single JSON tree to the frontend. Two surfaces: the public role-filtered tree at `/v1/navigation` (consumed by the sidebar) and the unfiltered admin tree at `/v1/admin/navigation` (consumed by `/admin/modules/navigation` for operator audit + reorder). Adding a menu entry still means editing the owning module's `NavItems()`, not the frontend.

Both trees are computed per-request from the in-memory `[]NavItemSpec` the registry populated under `ServiceNavItems` at boot. Persisted ordering overrides (`navigation_overrides` collection) are applied after the public filter pipeline and after the unfiltered admin pass, so operator reorder propagates to the live sidebar without a restart.

## What it owns

| File | Purpose |
|---|---|
| `module.go` | Module registration, nav-items + config-service lookup, handler wire-up, declares `navigation_overrides` collection. Also adds the module's own `/admin/modules/navigation` sidebar entry. |
| `routes.go` | Huma route registration for the public route + the three admin routes (GET tree, PATCH order, DELETE order) |
| `handlers/navigation_handler.go` | Extracts the system role from the request context and calls the public service |
| `handlers/admin_navigation_handler.go` | GetAdminNavigation / PatchOrder / DeleteOrder; gated upstream by the `administrator` security scope |
| `services/navigation_service.go` | Navigation service interface |
| `services/dynamic_navigation.go` | Role + enabled-module filtering implementation (public surface) — applies persisted overrides after the filter pipeline |
| `services/admin_navigation_service.go` | Unfiltered tree builder with MinRole/Tier/ModuleEnabled metadata; stamps `EffectiveOrder` + `Overridden` from the override layer |
| `services/override_service.go` | Loads + validates + persists ordering overrides; owns `sectionRootKey` synthetic-key shape; self-heals stale entries |
| `repository/override_repository.go` | Mongo upsert/list/delete over the `navigation_overrides` collection |
| `models/navigation.go` | `NavigationResponse` (public) + `AdminNavigationResponse` / `AdminNavItem` (admin) DTOs |
| `models/override.go` | `NavOverride` BSON model — one document per parent menu node |

## MongoDB collections

| Collection | Shape | Notes |
|---|---|---|
| `navigation_overrides` | one document per parent menu node — `{_id: parentKey, orderedChildren: []string, updatedAt, updatedBy}` | `parentKey` is one of: an `NavItemSpec.ItemKey` (nested reorder), the synthetic `__root.<realm>.<slug(section)>` (top-level items inside one realm/section bucket), or the sentinel `__realms__` (reorder the realm cards themselves). Single-collection module, so no module prefix per the naming rule. |

`parentKey` is the BSON `_id` — Mongo enforces uniqueness for free. Self-healing on read silently drops entries that reference stale ItemKeys (e.g. after a module rename) and logs a `slog.Warn`.

## Dependencies

- **Modules**: none declared.
- **Required services**: none.
- **Optional services** (read at `Init`):
  - `ServiceNavItems` — `[]module.NavItemSpec` populated by the registry before any module's `Init` runs (`shared/module/registry.go:153`). Empty slice if no modules declared nav items.
  - `ServiceConfigService` — `*ModuleConfigService`, used as a `middleware.ModuleEnabledChecker` to skip items from disabled modules at request time.
- **Provides**: none (no module consumes navigation; the frontend talks to it directly).
- **Permissions contributed**: none.

## Lifecycle

`Init` reads `ServiceNavItems` and `ServiceConfigService` from the registry, opens the `navigation_overrides` collection through `repository.NewMongoRepository`, builds the `OverrideService`, and constructs both the public `DynamicNavigationService` and the `AdminNavigationService` over the same NavItem slice. `Start`, `Stop`, and `HealthCheck` are no-ops inherited from `BaseModule`.

There is no seeding. The nav-items list is assembled by the registry in `InitAll` which walks every registered module's `NavItems()` method, stamps each item with the owning module name and a default `ItemKey` (slugified, `<module>.<parent-key>.<slug(name)>` when the module didn't pin one explicitly), and stores the aggregated slice under `ServiceNavItems` before any module gets its `Init` call — which is why navigation (which reads that key during its own init) always sees the full set.

## HTTP endpoints

| Method | Path | Gate | Purpose |
|---|---|---|---|
| GET    | `/v1/navigation` | Bearer token (protected router) | Return the role-filtered menu tree for the current user (overrides applied) |
| GET    | `/v1/admin/navigation` | `administrator` security scope on the operator router | Return the full unfiltered tree + per-item metadata (ItemKey, ModuleName, ModuleEnabled, MinRole, Tier, DeclaredOrder/EffectiveOrder/Overridden) plus `realmsParentKey` (the `__realms__` sentinel) and `realmsOverridden` (true when the realm order diverges from canonical) |
| PATCH  | `/v1/admin/navigation/order` | `administrator` | Upsert the ordering override for one parent. Body: `{parentKey, orderedChildren[]}`. `parentKey` may be an `ItemKey`, a `__root.<realm>.<slug(section)>` synthetic key, or the `__realms__` sentinel. Items missing from `orderedChildren` keep their declared order and append after. |
| DELETE | `/v1/admin/navigation/order` | `administrator` | Clear the override for `?parentKey=...` (idempotent — missing doc is OK). |

Registered in `routes.go::RegisterRoutes` / `RegisterAdminRoutes`. All four routes share the operator-protected mux; the admin variants additionally declare `administrator` in their security scope so the upstream `RequireRole` middleware rejects non-admin callers.

Error codes (see `internal/shared/errcode/codes.go`):
- `navigation.override_unknown_parent` (400 missing / 404 unknown)
- `navigation.override_child_not_found` (422)
- `navigation.override_duplicate_child` (400)

## Service contract

None. Navigation does not expose anything in `shared/iface` — no other backend module consumes it.

## Key invariants

- **Three filters run per request**, in order, in `services/dynamic_navigation.go::convert`:
  1. **Module enabled.** If `ServiceConfigService` was present at init, items whose owning module is disabled (`module_configs.enabled == false`) are skipped. The check is per-request, cached through the config service's Redis layer — toggling a module in `/admin/modules` takes effect on the next nav fetch without a restart.
  2. **Tier vs tenant kind.** Items with `Tier="internal"` are hidden from callers acting in an external tenant; `Tier="external"` items are hidden from internal callers; empty `Tier` is visible to both. Tenant kind is read from `middleware.TenantKindFromContext(ctx)` (populated by the auth middleware from the JWT's resolved acting-tenant).
  3. **MinRole.** Items are included when the caller's system role is at or above `MinRole` in the hierarchy `super_admin > administrator > developer > manager > operator > guest`. Empty `MinRole` = no restriction. The hierarchy matches `ROLE_HIERARCHY` on the frontend.
- **Parent items with no path collapse when every child is filtered out.** A parent acts as a menu group if it has `Children` and no `Path`; if all children are hidden, the parent vanishes too.
- **Override application is last.** After the three filters and after `buildRealms`, the override layer reorders siblings within each parent (keyed by `parentKey`) and reorders the realm slice itself (keyed by `__realms__`). Override-load failures degrade to declared order — the menu must always render, even when Mongo is unavailable.
- **Override self-heal.** Unknown `parentKey` or unknown child `ItemKey` are dropped on read with a `slog.Warn`. Module renames or removals therefore auto-heal stale overrides without an admin migration.
- **Dual response shape.** `NavigationResponse` emits **both** the legacy `groups []RouteGroup` (flat `section → items`, v1) and the new `realms []NavRealm` (nested `realm → section → items`, v2). Realms use canonical keys `personal | platform | business | shared` with canonical labels `My workspace | Administration | Business | Tools`. The key `platform` stays fixed (internal identifier); the label is free to evolve. Without an override, realms are emitted in canonical order; unknown realm keys append in discovery order. The dual shape exists so the frontend can migrate to v2 without a synchronized deploy.
- **`TenantKind` is surfaced in the response.** `NavigationResponse.TenantKind` echoes the filter context so the frontend can cache per-tier menus without re-reading the JWT. Cache key includes the tenant kind: `nav:<role>:<kind>` (falls back to `nav:<role>` when no tenant is resolved).
- **Registry stamping.** The registry sets `ModuleName` on every `NavItemSpec` (and recursively on every child) before handing them to navigation — that's what the enabled-module filter keys off. It also fills in `ItemKey` for every spec the module didn't pin explicitly, using a deterministic `<module>.<parent>.<slug(name)>` fallback so persisted overrides have a stable handle.

## What this module does NOT do

- Persist user-specific preferences (favorites, collapsed groups) — only operator-set ordering overrides are persisted (`navigation_overrides`)
- Evaluate per-org fine-grained permissions (e.g., "only show Invoices if the user has `billing.invoice.read` in this org"). The tier filter is tenant-kind-scoped, not per-org-binding-scoped — per-org filtering waits on the authz permission-domain-tag refactor.
- Return a route map or permission gates to the frontend — it only returns human-readable menu entries; the frontend's React Router still has to have the route registered
- Hide a menu item because its *target* module is failing its health check — only explicit disabled state is honored

## Rules

- **Never hardcode menu entries in this module.** Every item must come from some other module's `NavItems()`. If you find yourself adding items in `navigation_service.go`, the entry belongs in the owning module instead.
- **Never return the raw `NavItemSpec`** — wrap in `NavigationResponse` (`models/navigation.go`). The wrapper is the frontend contract; the spec is an internal detail.
- **Never fall back to the guest tier silently** for a user with a non-empty token but unresolvable role. Log it — it means the system-role field is corrupt or a rename migration was missed.
- **Prefer `Realm` + `Section` over the legacy `Group` field** in new modules. The aggregator maps items into the v2 shape by realm key first, section label second; `Group` is kept only so pre-migration modules still render.
- **When the authz permission-domain-tag refactor lands**, replace the tenant-kind+role filter with `authz.GetEffectivePermissions` and filter items by a `RequiredPermission` field on `NavItemSpec`.

## Known limitation

Filtering by tenant kind + global system role still can't express per-org scope (e.g., "show `Billing` in org A but not org B for the same user"). The underlying `authz` module already has per-org effective permissions; the hook-up is future work tied to the permission-domain-tag refactor. Until then, the frontend compensates by hiding a per-org menu section when `authz.GetEffectivePermissions` returns an empty set for that org — but the returned tree still contains every item the role+tier allows.

## Related

- [`../../shared/module/registry.go:146-153`](../../shared/module/registry.go) — where nav items are collected and stamped with owner
- [`../../shared/module/module.go`](../../shared/module/module.go) — `NavItemSpec` type definition
- [`../../shared/middleware/module_enabled.go`](../../shared/middleware/module_enabled.go) — `ModuleEnabledChecker` interface
- [`../authz/CLAUDE.md`](../authz/CLAUDE.md) — the provider the filter should eventually consult
- [`../../../../frontend-admin/CLAUDE.md`](../../../../frontend-admin/CLAUDE.md) — how the frontend consumes `/v1/navigation`
