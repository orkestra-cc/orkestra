# Module: Authz — Permissions catalog, roles, role bindings

_Path: `/backend/internal/core/authz`_
_Parent: [../CLAUDE.md](../CLAUDE.md)_

[← Core](../CLAUDE.md) | [☰ Backend](../../../CLAUDE.md) | [Root](../../../../CLAUDE.md)

## Purpose

Owns the permission catalog (auto-populated by every module at boot), the system roles (seeded from that catalog), org-scoped custom roles, role bindings (with optional expiration), and the evaluator the middleware calls on every protected request. Implements `iface.AuthzProvider`.

Permissions are **not embedded in JWTs** — they are resolved per request via Redis-backed caching so revocation is instant. The JWT only carries the user's global system role plus their list of org memberships.

## What it owns

| File | Purpose |
|---|---|
| `module.go` | Module registration, collections, permissions, service wiring |
| `handlers/handler.go` | HTTP routes for permission catalog, roles, bindings, effective-permissions |
| `services/service.go` | Evaluator, seeding, lazy-heal, cache management |
| `repository/repository.go` | MongoDB CRUD for permissions, roles, bindings |
| `models/authz.go` | `Permission`, `Role`, `Binding` structs + DTOs |

## MongoDB collections

Declared in `module.go:47-69`. Constants: `CollPermissions`, `CollRoles`, `CollBindings`.

| Collection | Indexes | TTL |
|---|---|---|
| `authz_permissions` | `key` unique, `module` | — |
| `authz_roles` | compound `(orgId, name)` unique, `uuid` unique | — |
| `authz_bindings` | compound `(userUUID, orgId)`, `roleId`, `expiresAt` | `expiresAt` plain index — **not** a TTL; expired bindings are filtered out by queries but must be explicitly reaped |

## Dependencies

- **Modules**: `user`, `tenant` (`module.go:31`).
- **Required services**: `ServiceUserService` — the evaluator calls `UserProvider.GetUserByID` to resolve each user's global system role (super_admin / administrator / developer → shortcut paths).
- **Optional services**: none.
- **Provides**: `ServiceAuthzProvider` → `iface.AuthzProvider` (`module.go:37-39`).
- **Permissions contributed** (`module.go:71-85`):

| Key | System? | Purpose |
|---|---|---|
| `authz.role.read` | no | List roles |
| `authz.role.create` | no | Create custom roles |
| `authz.role.update` | no | Update custom roles; toggle any role's active state |
| `authz.role.delete` | no | Delete custom roles |
| `authz.binding.read` | no | List role bindings |
| `authz.binding.create` | no | Grant roles to users |
| `authz.binding.delete` | no | Revoke role bindings |
| `system.modules.admin` | **yes** | Manage module catalog + runtime state |
| `system.users.admin` | **yes** | Administer all user accounts |

The two `system.*` permissions are contributed here even though they gate other modules, because authz owns the concept of "system permission" and the seeding of system roles that inherit them.

## Lifecycle

- **Init** (`module.go:87-111`): constructs the repository, builds the user-role lookup closure (calls `UserProvider.GetUserByID` and reads `.Role`), wires the service, registers it as `iface.AuthzProvider`.
- **Registry post-init** (`shared/module/registry.go:183-211`): after every module has run its `Init`, the registry calls `authz.RegisterPermissions` with the union of every module's `Permissions()`, then calls `authz.SeedSystemRoles` which derives the six roles' permission lists from the now-complete catalog.
- **Start / Stop / HealthCheck**: inherit from `BaseModule`.
- **Lazy-heal** (`services/service.go::ensureSeeded`): `ListRoles` and `ListPermissions` both call `ensureSeeded` before querying the repo. If the system-role count is zero, the service re-runs `RegisterPermissions` + `SeedSystemRoles` from an in-memory copy of the spec list (`cachedPermSpecs`). This is what makes `/admin/roles` self-heal after a live DB drop without a backend restart. See the notes in [Key invariants](#key-invariants) below — do not remove `cachedPermSpecs` or the `ensureSeeded` calls.

## HTTP endpoints

Two route groups (`module.go:113-127`):

### Global — `RequireGlobal()`

| Method | Path | Purpose |
|---|---|---|
| GET | `/v1/authz/permissions` | List the permission catalog (system-generated) |

### Per-org — `RequirePermission("authz.role.read")`

| Method | Path | Purpose |
|---|---|---|
| GET | `/v1/orgs/{orgId}/authz/roles` | List roles (system + custom scoped to this org) |
| POST | `/v1/orgs/{orgId}/authz/roles` | Create a custom role |
| PATCH | `/v1/orgs/{orgId}/authz/roles/{roleId}` | Update role — custom: name/description/permissions/isActive; system: `isActive` only |
| DELETE | `/v1/orgs/{orgId}/authz/roles/{roleId}` | Delete custom role — cascades bindings |
| GET | `/v1/orgs/{orgId}/authz/bindings` | List role bindings in the org |
| POST | `/v1/orgs/{orgId}/authz/bindings` | Grant a role with optional expiration |
| DELETE | `/v1/orgs/{orgId}/authz/bindings/{bindingId}` | Revoke a binding |
| GET | `/v1/orgs/{orgId}/authz/me` | Return the caller's effective permissions in this org |

Route registration in `handlers/handler.go::RegisterGlobalRoutes` and `::RegisterScopedRoutes`.

## Service contract

`iface.AuthzProvider` (`shared/iface/interfaces.go:216-229`):

```go
HasPermission(ctx, userUUID, orgUUID, permission string) (bool, error)
GetEffectivePermissions(ctx, userUUID, orgUUID string) ([]string, error)
RegisterPermissions(ctx, specs []PermissionSpec) error
```

Consumers:
- **`shared/middleware/auth.go`** — `RequirePermission`, `RequireSystemPermission` call `HasPermission` on every request; the result is compared against the route's required permission.
- **The authz module itself** — calls `RegisterPermissions` from the registry post-init hook.
- **Lazy heal** — `ensureSeeded` also calls `RegisterPermissions` + the internal `SeedSystemRoles` when the catalog is empty at query time.

## System roles

Seeded by `services/service.go::SeedSystemRoles`. Permissions are computed from the catalog at seed time:

| Role | Permissions | Notes |
|---|---|---|
| `super_admin` | `["*"]` (wildcard) | Overrides every check in `HasPermission`; the only holder of `*` by design. |
| `administrator` | `allKeys` (every registered permission) | Full org permissions. Cannot elevate peers to administrator (future cascade rule). |
| `developer` | `allKeys` (same set as administrator) | Conceptually the mid-tier technical role; permission-set distinction from `administrator` is **not yet enforced** — only the name differs. |
| `manager` | `allKeys` filtered to exclude `.delete` and `.admin` suffixes | Read + create + update across the catalog. |
| `operator` | `allKeys` filtered to suffix `.read` or `.self` | Read everything plus self-service. |
| `guest` | `allKeys` filtered to suffix `.read` | Read-only. |

Permission-evaluation rules (`services/service.go:31-44`, implemented in `GetEffectivePermissions`):

1. If the user's system role is `super_admin`, grant `"*"` and short-circuit.
2. If the system role is `administrator` or `developer`, inherit every permission in the in-memory `systemPermissionSet` (everything marked `System: true` in any module's `Permissions()`).
3. Otherwise, union every active binding for `(userUUID, orgID)` with every global binding (`orgID=""`).
4. System permissions — those declared with `System: true` — require a **global** grant (either by system role or by a binding with empty `orgID`), not a per-org binding.

## Key invariants

- **System roles are immutable on name/description/permissions.** `UpdateRole` on an `IsSystem=true` row returns `ErrSystemRoleImmutable` (`services/service.go::ErrSystemRoleImmutable`) if the caller touches `Name`, `Description`, or `Permissions`. Only `IsActive` is toggleable. The UI enforces this too, but the service is the authoritative gate.
- **Role seeding preserves UUIDs.** `SeedSystemRoles` (`services/service.go::SeedSystemRoles`) calls `GetRoleByName` before upsert and copies the existing UUID into the new row, so bindings pointing at the old document keep working across boots and across lazy-heal runs.
- **Effective-permission cache: 60s in Redis**, keyed on `authz:cache:<userUUID>:<orgID>`. Cache is invalidated on binding create/delete and on role update/delete via `flushCache` / `cacheInvalidate`.
- **Lazy-heal uses an in-memory spec cache.** `RegisterPermissions` stores the full `[]PermissionSpec` under the service's mutex as `cachedPermSpecs`. `ensureSeeded` reuses it to rebuild the catalog if the DB is dropped at runtime. If you touch `RegisterPermissions`, make sure the cache is still populated — otherwise the first admin who drops the DB in dev will get an empty `/admin/roles` and no automatic recovery.
- **`CreateBinding` does not currently enforce a cascade rule.** An administrator can grant any role to any user, including another administrator. Client-side role-management UI filters the picker, but the server accepts it. Cascade enforcement ("you can only assign roles strictly below your own level") is planned but not yet implemented — see the out-of-scope note below.
- **Binding expiration is advisory, not TTL.** The `expiresAt` index exists, but it's not declared as a TTL index. Expired bindings are filtered out by `ListActiveBindingsForUser` but they stay in the collection. A background reaper is future work.
- **`DeleteRole` cascades bindings.** The service calls `DeleteBindingsByRoleUUID` before the role delete so nothing is left pointing at a nonexistent role. System roles refuse to delete regardless (via the repository's `isSystem=false` filter).

## What this module does NOT do

- User identity and system-role storage → **user** module (`User.Role` field)
- Org ownership, membership, plan entitlements → **tenant** module
- Middleware enforcement → **`shared/middleware/auth.go`** consumes `HasPermission`
- JWT claims — permissions are never embedded; only the system role and memberships are
- Audit logging of role changes — there is no audit stream today; future work

## Rules

- **Never hardcode a role name outside of `SeedSystemRoles`.** Role renames must be a single-file change. Middleware code that needs to special-case `super_admin` / `administrator` / `developer` belongs in the evaluator, not in the handler layer.
- **Never remove `ensureSeeded` from `ListRoles` / `ListPermissions`.** It's the only thing making the admin UI self-heal after a dev DB wipe. If you optimize it, still keep the empty-collection branch.
- **Never bypass `CanDeliver`-style checks for system roles** — a super_admin should see every permission on `/v1/orgs/{orgId}/authz/me`, which means the wildcard must be preserved in the cache serialization.
- **When adding a new permission**, always declare it in the owning module's `Permissions()` — never write directly to the `authz_permissions` collection. The registry-collected list is the single source of truth and drives the computed role sets at seed time.
- **Custom roles are scoped to one org.** Their `orgId` field is non-empty. Never copy custom-role UUIDs across orgs; always resolve by `(orgId, name)` or UUID within the target org.
- **System permissions need a global grant.** Do not try to "grant `system.modules.admin` in org X" — the evaluator requires `orgID=""` for system permissions. Bindings with a non-empty `orgId` holding system permissions will silently not match in the evaluator.

## What's planned but not done

- **Assignment cascade rule** — administrator can't elevate peers to administrator; developer can't manage administrators or super_admin. Today this is UI-only, server enforcement pending.
- **Permission domain tags** — tagging each permission as `system` / `technical` / `business` so the developer role can be seeded as "all system+technical permissions, read-only on business".
- **TTL on `authz_bindings.expiresAt`** — flip the plain index to a TTL index so expired contractor grants auto-reap.
- **Audit trail for role CRUD and binding CRUD** — part of future SOC2 work.

## Related

- [`../user/CLAUDE.md`](../user/CLAUDE.md) — provides the system role lookup
- [`../tenant/CLAUDE.md`](../tenant/CLAUDE.md) — provides `Membership.Roles`, which is a denormalized view of this module's bindings
- [`../auth/CLAUDE.md`](../auth/CLAUDE.md) — consumes `HasPermission` via middleware on every protected request
- [`../../shared/iface/interfaces.go:198-229`](../../shared/iface/interfaces.go) — `AuthzProvider` and `PermissionSpec` definitions
- [`../../shared/middleware/auth.go`](../../shared/middleware/auth.go) — `RequirePermission`, `RequireSystemPermission`
