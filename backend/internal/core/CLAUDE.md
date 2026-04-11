# Core modules — always-loaded kernel

_Path: `/backend/internal/core`_
_Parent: [../../CLAUDE.md](../../CLAUDE.md)_

[← Backend](../../CLAUDE.md) | [Root](../../../CLAUDE.md)

## What this is

The six modules under `backend/internal/core/` are the always-loaded kernel of the Orkestra backend. Every deployment boots them — they provide identity, multi-tenancy, permissions, navigation, and outbound mail. Addons under `backend/internal/addons/` are opt-in via `MODULES` env var; core is not.

Load order is topologically sorted from each module's `Dependencies()` by `ModuleRegistry.InitAll` (`shared/module/registry.go:115-217`):

```
user → notification → tenant → authz → auth → navigation
```

The registry walks this DAG, constructs each module, auto-creates its MongoDB collections, seeds its module_configs document, collects its nav items, wires its services into the registry, and registers its routes behind a gate (core routes are not gated — the gate only applies to optional addons).

## Module map

| Module | Tagline | CLAUDE.md |
|---|---|---|
| **user** | Global user accounts, profiles, system role field, document expiry tracking | [user/CLAUDE.md](user/CLAUDE.md) |
| **notification** | Outbound email: SMTP transport, template rendering, preferences, suppressions, idempotency | [notification/CLAUDE.md](notification/CLAUDE.md) |
| **tenant** | Organizations, per-user memberships, plan-based feature entitlements | [tenant/CLAUDE.md](tenant/CLAUDE.md) |
| **authz** | Permission catalog, roles (system + custom), role bindings, evaluator with Redis cache | [authz/CLAUDE.md](authz/CLAUDE.md) |
| **auth** | Email/password + OAuth 2.1, JWT issuance, sessions-per-device, refresh rotation | [auth/CLAUDE.md](auth/CLAUDE.md) |
| **navigation** | Role-filtered sidebar menu aggregated from every module's `NavItems()` | [navigation/CLAUDE.md](navigation/CLAUDE.md) |

## Dependency graph

```
              ┌────────┐
              │  user  │ ◄── everything reads user profiles
              └───┬────┘
                  │
      ┌───────────┼───────────┐
      ▼           ▼           ▼
┌──────────┐ ┌────────┐  ┌────────┐
│  notif.  │ │ tenant │  │ authz  │
│(optional │ │        │  │        │
│  from    │ └───┬────┘  └───┬────┘
│  auth's  │     │           │
│  POV)    │     │           │
└─────┬────┘     │           │
      │          │           │
      └──────────▼───────────▼
                ┌──────┐
                │ auth │ ◄── required svcs: user, tenant
                └──┬───┘         optional svcs: notification
                   │
                   ▼
              ┌────────────┐
              │ navigation │ ◄── reads all NavItems() from the registry
              └────────────┘
```

Edges:
- **Hard dependency** (listed in `Dependencies()` → blocks boot if missing): `tenant → user`, `authz → user, tenant`, `auth → user, notification, tenant, authz`.
- **Soft runtime dependency** (`OptionalServices()` → graceful degradation): `auth → notification` (signup still works, but returns 503 when `AUTH_REQUIRE_EMAIL_VERIFICATION=true` and the sender is not configured).
- **Navigation** has no declared dependencies; it reads the already-populated `ServiceNavItems` at init time, so it transitively sees every other module.

## Service registry keys (core subset)

Constants live in `backend/internal/shared/module/services.go:13-32`. This is the table a reader searching for "who registers X" should hit first.

| `ServiceKey` constant | Registered by | Interface type | Notes |
|---|---|---|---|
| `ServiceNavItems` | registry (`registry.go:153`) | `[]module.NavItemSpec` | populated before any module's `Init` runs |
| `ServiceConfigService` | registry (`registry.go:140`) | `*ModuleConfigService` | provides `IsEnabled` + `GetValue`/`GetSecret` |
| `ServiceUserService` | user | `iface.UserProvider` | CRUD + auth lookup surface |
| `ServiceNotificationSender` | notification | `iface.NotificationSender` | optional from auth's POV |
| `ServiceTenantProvider` | tenant | `iface.TenantProvider` | org, membership, entitlement checks |
| `ServiceAuthzProvider` | authz | `iface.AuthzProvider` | `HasPermission`, `GetEffectivePermissions`, `RegisterPermissions` |
| `ServiceAuthService` | auth | `*services.AuthService` | concrete, consumed by handlers/main.go |
| `ServiceJWTService` | auth | `*services.JWTService` | consumed by middleware + dev module |
| `ServicePasswordService` | auth | `*services.PasswordService` | argon2id hashing |
| `ServicePasswordAuthService` | auth | `*services.PasswordAuthService` | consumed by setup wizard |
| `ServiceOAuthProviderFactory` | auth | `*services.OAuthProviderFactory` | |
| `ServiceOAuthStateService` | auth | `*services.OAuthStateService` | |
| `ServiceOAuthProviderRepo` | auth | `*repository.OAuthProviderRepository` | |

Type-safe accessors: `module.GetTyped[T]` for optional lookups, `module.MustGetTyped[T]` for required ones (panics if missing — only use after the dependent module has already initialized).

## Boot-time seeding — things that run once and then never again

Several pieces of state are written to MongoDB only during `InitAll`. If the collections are wiped at runtime, the affected features break until a restart — unless the module has a lazy-heal fallback. Current state:

| Seed | Where | Lazy-heal? |
|---|---|---|
| `module_configs` documents | `shared/module/config_service.go::SeedFromModules` | ✅ `GetConfig`/`GetAllConfigs` lazy-rebuild from the in-memory spec cache |
| Permissions catalog + system roles | `registry.go:183-211` → `authz.RegisterPermissions` + `authz.SeedSystemRoles` | ✅ `authz.Service.ListRoles`/`ListPermissions` call `ensureSeeded` |
| Notification templates | `notification` module's `Start` → `TemplateService.SeedDefaults` | ❌ templates re-seed only at next backend restart |
| Mongo collection auto-creation + index build | registry's `ensureCollections` | ❌ collections are re-created only at next restart |

## Where to put new code

| Concern | Answer |
|---|---|
| "It's a new cross-cutting capability everyone needs" → core or shared? | Core if it has state and exposes an interface to others (e.g. authz). Shared (`shared/iface`, `shared/middleware`) if it's stateless plumbing. |
| "It's a product feature that some deployments won't want" | **Addon** under `backend/internal/addons/`, opt-in via `MODULES`. Examples: billing, rag, graph. |
| "A core module needs something from an addon" | **Don't.** Core must not import addons. Invert the dependency: expose an interface in `shared/iface` and let the addon register as the implementation. |

## Rules

- **Core modules never depend on addons.** The dependency edge always points from addon to core.
- **Cross-module deps go through `shared/iface`.** Never import another module's `services/` or `repository/` package from a `module.go` wiring file.
- **Every new core module must implement the full `Module` interface** (`shared/module/module.go:30-70`) including `Collections()`, `Dependencies()`, `Permissions()`, `NavItems()` — no shortcuts via `BaseModule` except for genuinely empty methods.
- **Seeding code lives inside the module's own `Init` or `Start` method**, not in `cmd/server/main.go`. `main.go` should stay ~240 lines and contain zero domain logic.
- **If you add a new `ServiceKey` constant**, declare it in `shared/module/services.go`, not in the consuming module. The constants are the lingua franca of the registry.

## Related

- [`../shared/module/registry.go`](../shared/module/registry.go) — `ModuleRegistry.InitAll`, the single place modules get wired up
- [`../shared/module/module.go`](../shared/module/module.go) — the `Module` interface contract
- [`../shared/iface/interfaces.go`](../shared/iface/interfaces.go) — every cross-module interface lives in one file
- [`../../cmd/server/catalog.go`](../../cmd/server/catalog.go) — module catalog, core vs optional split
- [Root CLAUDE.md](../../../CLAUDE.md) — module map and overall architecture
- [Backend CLAUDE.md](../../CLAUDE.md) — project structure and dev workflow
