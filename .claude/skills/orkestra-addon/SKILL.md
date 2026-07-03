---
name: orkestra-addon
description: "Rules for writing a correct Orkestra addon (optional module) end-to-end — backend internal/addons/<name>/ + its frontend-admin pages. Covers the load-bearing module-name convention (collections, permissions, error codes, i18n namespace, catalog), the Module/iface/ServiceRegistry contract, tenant-scoping, RBAC + tier, per-addon i18n (ADR-0007), and the ADR-0006 forbidden patterns. Use PROACTIVELY when creating, scaffolding, or reviewing an addon or any of its files."
---

# Writing an Orkestra Addon

Orkestra is a **core-only base** ([ADR-0006](../../../docs/adr/0006-collapse-to-core-only-base.md)). A fork adds a vertical as an **addon** — an optional module built against the in-tree SDK, through the same `Module` seam the 8 core modules use. The base ships none; you add yours in-tree.

This skill is the **spine**: the end-to-end recipe + the binding rules unique to addons. For depth, defer to the cross-referenced skills/docs rather than duplicating them — they are the source of truth and must not drift.

## The one rule that drives all the others: the module name is load-bearing

Pick the module name **once** (lowercase, singular, no hyphens — e.g. `test`). The **same string** propagates to ~13 places, and most conventions are just "use that name". An addon named `test`:

| # | Where | Must be |
|---|---|---|
| 1 | Backend dir | `backend/internal/addons/test/` |
| 2 | Catalog file | `backend/cmd/server/catalog_test.go` — one `init()` registering into `optionalModules["test"]` |
| 3 | `Module.Name()` | returns `"test"` |
| 4 | Mongo collections (if ≥2) | every name prefixed `test_` (`test_things`, `test_audit`) |
| 5 | Permissions | `test.read`, `test.create`, `system.test.admin` |
| 6 | Error codes | `test.<situation>` (snake_case) |
| 7 | Config keys / secrets | `ConfigSchema` keys; `EnvVar: TEST_*` |
| 8 | Nav items | `NavItemSpec{Realm, Section, Tier, MinRole}` |
| 9 | Frontend pages | `frontend-admin/src/pages/test/` |
| 10 | RTK Query slice | `frontend-admin/src/store/api/testApi.ts` (extends `baseApi`) |
| 11 | Frontend manifest | `frontend-admin/src/modules/test.tsx` → `moduleCatalog["test"]` |
| 12 | i18n namespace | `test` → `t('test:…')`, bundles `pages/test/locales/{en,it}.json` |
| 13 | Frontend types | `frontend-admin/src/types/test.ts` |

If you ever find the addon's name diverging across these (e.g. dir `test` but collection `things`), that's the bug.

---

## Backend rules

Deep reference: **`orkestra-go` skill**, [`backend/CLAUDE.md`](../../../backend/CLAUDE.md), [`backend/pkg/sdk/CLAUDE.md`](../../../backend/pkg/sdk/CLAUDE.md).

1. **Implement the `Module` interface** (`pkg/sdk/module/module.go` — `Name() / Category() / Init(*Dependencies)`), embedding `module.BaseModule` for defaults. Add the optional sub-interfaces you need: `Routable` (RegisterRoutes), `HasCollections`, `HasNavItems`, `HasConfigSchema`, `HasPermissions`, `HasDependencies`, `Startable`/`Stoppable`, `HealthCheckable`. `Category()` returns `CategoryToggleable` (no external creds) or `CategoryExternal` (needs API keys).

2. **Register via a per-addon catalog file.** `cmd/server/catalog_test.go` with a single `init()` adding `optionalModules["test"] = func() module.Module { return test.NewModule() }`. No build tags — the single binary compiles every addon; enable/disable is runtime at `/admin/modules`. The registry topo-sorts by `Dependencies()`.

3. **Cross-module deps go through `pkg/sdk/iface` + `ServiceRegistry` only.** Resolve with `module.GetTyped[T]` (optional, nil-safe) / `module.MustGetTyped[T]` (required, panics). **Never** import another module's `services/` or `repository/` package from `module.go`. Declare `Dependencies()`, `RequiredServices()`, `OptionalServices()` honestly. Failure: import cycle / panic at init.

4. **Tenant-scope every Mongo query.** Use `tenantrepo.Scope` / `MustScope` / `StampInsert(M)` / `ScopeAggregate` (`pkg/sdk/tenantrepo/scope.go`) — never a raw `bson.M` filter. Legitimate exceptions carry an inline `//tenantscope:allow <reason>` (or `//tenantscope:allow-until=YYYY-MM-DD`). Failure: CI `tenantscope` job fails; dev panics, prod 403. See `backend/internal/<module>/CLAUDE.md` org-scoping invariants.

5. **Collection naming.** A module owning ≥2 collections prefixes every one with `test_`. Single-collection modules may keep any name. → **`orkestra-mongo-collection-naming` skill** is the authority.

6. **RBAC on every endpoint + declare the tier.** In `RegisterRoutes`, mount on `ri.Operator` (Tier-1) and/or `ri.Client` (Tier-2, may be nil — check). Gate every authed route with the audience's `AuthMW` (`RequirePermission` / `RequireSystemPermission` / `RequireCapability` / `RequireStepUp` …). Declare the permission catalog in `Permissions()` (`iface.PermissionSpec`). Tier filtering of routes/nav is via `Tier: "internal" | "external" | ""`. Non-core routes are auto-wrapped in `ModuleGate` → **503** (`module_disabled`) when disabled. Every new endpoint must state which tier it serves (CLAUDE.md mandate).

7. **Config & secrets.** Declare admin-editable fields in `ConfigSchema()` (`ConfigField{Key,Type,EnvVar,…}`); use `Type: FieldSecret` for credentials — encrypted at rest (AES-256-GCM) via `ConfigService`. Read with `deps.GetConfig/GetSecret/…`. Never log or return secrets.

8. **Error codes** live in `internal/shared/errcode/codes.go`, named `test.<situation>` (snake_case), returned through the `errcode` builders. They are wire contracts — stable, snake_case, module-namespaced.

9. **Regenerate OpenAPI** after any route change (`make openapi-…`, commit `backend/openapi/enterprise.json`); CI `openapi-check` gates it. Pass `make ci-backend` (lint, tenantscope, policycoverage, vuln, tests, build).

---

## Frontend-admin rules

Deep reference: **`frontend-design` skill**, [`frontend-admin/CLAUDE.md`](../../../frontend-admin/CLAUDE.md), [`frontend-admin/src/modules/_template/README.md`](../../../frontend-admin/src/modules/_template/README.md) (the canonical scaffold — copy it).

10. **Module manifest** `src/modules/test.tsx` exporting a `ModuleManifest` with `name: 'test'`, lazy `routes()` each wrapped in `<ModuleGate module="test">` + `<ProtectedRoute>` + `<Suspense>`, `injectApi`, and `injectI18n`. Register it in `src/modules/index.ts` `moduleCatalog`.

11. **Data fetching** through RTK Query only: `src/store/api/testApi.ts` extends `baseApi` via `injectEndpoints`; declare new cache tags in `baseApi.ts` `tagTypes`; types in `src/types/test.ts`. No axios / custom fetch.

12. **Navigation comes from the backend** (`NavItems()` → `/v1/navigation`) — never hardcode sidebar entries for addon (production) features.

13. **i18n is a per-addon namespace ([ADR-0007](../../../docs/adr/0007-per-addon-i18n-namespaces.md)).** This is the rule most likely to be violated. An addon **must not** edit the core `src/locales/{en,it}.json`, `src/i18n-types.d.ts`, or `src/locales/parity.test.ts`. Instead:
    - Ship `src/pages/test/locales/{en,it}.json` (all supported languages).
    - Register them via the manifest's `injectI18n` (the boot hook `useModuleI18nInjection` does `addResourceBundle(lng, 'test', …)` ungated by auth/enabled-state).
    - Add `src/pages/test/i18n.d.ts` augmenting `CustomTypeOptions['resources']` with the `test` namespace (typed `t()` without touching core types).
    - Consume with `useTranslation('test')` / `t('test:key')`.
    - Ship a parity test reusing `src/locales/parityCheck.ts`.
    - For localized **error codes**, ship them as `errors.<rest>` in the namespace bundle and render via `helpers/resolveErrorMessage` (`test:errors.<rest>` → core `errors.<code>` → backend `detail`).
    - Adding a brand-new **language** (e.g. `fr`) is a core change to `src/i18n.ts`, not an addon operation.

14. **Tabs must be URL-synced** → **`url-tabs` skill**.

---

## Forbidden (ADR-0006 / ADR-0007)

- ❌ A satellite `go.mod`, `go.work`, or `replace` directive. The backend is one Go module; the addon lives in-tree under `internal/addons/<name>/`.
- ❌ A core module importing anything under `internal/addons/`. Dependencies point inward only (addon → core via `iface`).
- ❌ An addon writing keys into the core frontend locale files / `i18n-types.d.ts` / `parity.test.ts`.
- ❌ Hardcoding addon sidebar entries in the frontend.
- ❌ Unscoped Mongo queries, un-gated endpoints, logged/returned secrets.

---

## Checklist (review an addon against this)

**Backend**
- [ ] `internal/addons/test/` implements `Module`; `Name()=="test"`; `Category()` correct
- [ ] `cmd/server/catalog_test.go` registers `optionalModules["test"]` via `init()`; name matches dir
- [ ] No import of other modules' `services/`/`repository/`; cross-module via `iface` + `ServiceRegistry`; `Dependencies()`/`RequiredServices()`/`OptionalServices()` declared
- [ ] Every Mongo query scoped (`tenantrepo.*`) or carries `//tenantscope:allow`
- [ ] Collections prefixed `test_` (if ≥2)
- [ ] Every endpoint gated by `AuthMW` + declares its tier; `Permissions()` declared
- [ ] Secrets `FieldSecret`; never logged/returned
- [ ] Error codes `test.<situation>` in `errcode/codes.go`
- [ ] OpenAPI regenerated; `make ci-backend` green

**Frontend-admin**
- [ ] `src/modules/test.tsx` manifest with `injectApi` + `injectI18n`; routes in `ModuleGate`+`ProtectedRoute`+`Suspense`; registered in `moduleCatalog`
- [ ] `testApi.ts` extends `baseApi`; tags in `baseApi.ts`; types in `types/test.ts`
- [ ] **Core locale files untouched**; addon strings under `pages/test/locales/` as namespace `test`; `i18n.d.ts` + parity test shipped
- [ ] No hardcoded nav; tabs URL-synced
- [ ] `npm run typecheck && lint && test && build` green

**Cross-cutting**
- [ ] No `go.mod`/`replace`/`go.work`; no core→addon import
- [ ] Adding **and removing** the addon touches only addon files (zero diff to any core file)

## Cross-references

- **`orkestra-go`** — backend module mechanics, iface, tenancy, Huma APIs (authoritative for Go)
- **`orkestra-mongo-collection-naming`** — the `test_` prefix rule (authoritative)
- **`frontend-design`** — frontend-admin components/pages/forms/tables (authoritative for UI)
- **`url-tabs`** — URL-synced tabs
- [ADR-0006](../../../docs/adr/0006-collapse-to-core-only-base.md) (core-only base), [ADR-0007](../../../docs/adr/0007-per-addon-i18n-namespaces.md) (per-addon i18n)
- [`frontend-admin/src/modules/_template/README.md`](../../../frontend-admin/src/modules/_template/README.md) — copy-paste scaffold (the worked `widgets` example)
