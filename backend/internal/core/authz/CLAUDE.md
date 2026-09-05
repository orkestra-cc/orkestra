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
| `services/service.go` | Evaluator, seeding, lazy-heal, generation-keyed cache + the D27 invalidation contract |
| `services/role_validation.go` | The D21 custom-role permission gate (catalog / platform-key / cascade) + `OffendingPermissionKey` |
| `cedar/policies/*.cedar` | Embedded Cedar policy set — see [Shipped Cedar policies](#cedar-abac-attributes) |
| `repository/repository.go` | MongoDB CRUD for permissions, roles, bindings |
| `models/authz.go` | `Permission`, `Role`, `Binding` structs + DTOs |

## MongoDB collections

Declared in `module.go:47-69`. Constants: `CollPermissions`, `CollRoles`, `CollBindings`.

| Collection | Indexes | TTL |
|---|---|---|
| `authz_permissions` | `key` unique, `module` | — |
| `authz_roles` | compound `(orgId, name)` unique, `uuid` unique | — |
| `authz_bindings` | compound `(userUUID, orgId)`, `roleId`, `expiresAt`, compound `(tenantId, userUUID, roleId)` **unique** | `expiresAt` plain index — **not** a TTL; expired bindings are filtered out by queries but must be explicitly reaped |

### `authz_bindings` uniqueness constraint

`(tenantId, userUUID, roleId)` is this collection's **first** uniqueness constraint — before it shipped, nothing stopped the same tuple from being inserted twice, and `Repository.CreateBinding` was (and still is, for the non-ensure path) a plain `InsertOne` with no dedup. **NOT sparse, NO partial filter**: `tenantId == ""` rows are global/system-role grants (`super_admin`, `administrator`, `developer`, `manager`, `operator`, `guest`), and the index deliberately covers them too — a user must not hold the same system role twice globally any more than they should hold the same tenant role twice in one tenant. This is intentional, not an oversight; do not narrow the index to `tenantId != ""` or make it sparse/partial.

Watch the field-name mismatch when touching this index or its filters: the Go struct field is `RoleUUID` (`models.Binding.RoleUUID`), but its bson tag — and the index key — is `roleId`.

**Deploy prerequisite:** `module.go`'s `Collections()` declaration only takes effect through the registry's `ensureCollections`, which is **create-only** (it will not retrofit this index onto an existing collection) **and deliberately non-fatal on failure** — a failed `createIndex` (e.g. pre-existing duplicate rows) is logged as a WARN and boot continues, silently leaving the constraint absent while every health check stays green. `backend/migrations/20260823_authz_bindings_unique.js` (companion doc: `docs/migrations/0009_authz_bindings_unique.md`, companion test: `20260823_authz_bindings_unique.test.js`) dedups first and then creates the index with verification that throws on failure. **The dedup keeps the grant conferring the most access, not the oldest one** — permanent beats expiring, furthest-future expiry beats nearer, `grantedAt` only breaks ties. Keeping the oldest row silently revokes privileges whenever a trial grant was later made permanent; see the migration doc for the full rule and the `_perm` sort-key trap. **Run it against every environment before the deploy that ships this index change** — and, per the tier-1 setup-saga design, before the deploy that enables the provisioning-finalize route, since that route is what replays the owner-binding grant and depends on this index to make `EnsureBinding` actually safe to call twice. As of this change the migration has been written but not executed anywhere (see the doc's Verification section).

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

- **Init** (`module.go:87-111`): constructs the repository, builds the user-role lookup closure (calls `UserProvider.GetUserByID` and reads `.Role`), wires the service, registers it as `iface.AuthzProvider`. The lookup has a **dev-token fallback**: when the DB lookup fails, it falls back to the JWT context system role only if all three guards pass — (1) non-production environment, (2) UUID starts with `dev-`, (3) role is in the hardcoded `validDevRoles` allow-list. This lets synthetic dev-token users (which have no DB record) work with the authz evaluator.
- **Registry post-init** (`pkg/sdk/module/registry.go:183-211`): after every module has run its `Init`, the registry calls `authz.RegisterPermissions` with the union of every module's `Permissions()`, then calls `authz.SeedSystemRoles` which derives the six roles' permission lists from the now-complete catalog.
- **Start / Stop / HealthCheck**: inherit from `BaseModule`.
- **Lazy-heal** (`services/service.go::ensureSeeded`): `ListRoles` and `ListPermissions` both call `ensureSeeded` before querying the repo. If the system-role count is zero, the service re-runs `RegisterPermissions` + `SeedSystemRoles` from an in-memory copy of the spec list (`cachedPermSpecs`). This is what makes `/admin/roles` self-heal after a live DB drop without a backend restart. See the notes in [Key invariants](#key-invariants) below — do not remove `cachedPermSpecs` or the `ensureSeeded` calls.
- **GDPR/DSR** (`services/pii_producer.go`): registers an `iface.PIIProducer` (subject `"authz"`) on `ServicePIIProducerRegistry` at Init. The subject's personal data here is their **role bindings** (which roles, in which tenants, plus global/system bindings) — the role and permission catalogs are platform metadata, not the subject's data, and are left intact. Export returns the binding rows; purge deletes them (`authz_bindings`) under **both** erase modes, since a binding row IS the user→role linkage with no anonymizable residue. Consumed by the [compliance module](../compliance/CLAUDE.md)'s DSR pipeline (ADR-0009).

## HTTP endpoints

Two route groups (`module.go:113-127`):

### Global — `RequireGlobal()`

| Method | Path | Purpose |
|---|---|---|
| GET | `/v1/authz/permissions` | List the permission catalog (system-generated) |

### Per-org — read (`RequirePermission("authz.role.read")`)

| Method | Path | Purpose |
|---|---|---|
| GET | `/v1/tenants/{tenantId}/authz/roles` | List roles (system + custom scoped to this org) |
| GET | `/v1/tenants/{tenantId}/authz/bindings` | List role bindings in the org |
| GET | `/v1/tenants/{tenantId}/authz/me` | Return the caller's effective permissions in this org |

### Per-org — mutation (per-route `RequirePermission` + `RequireMFA()`)

Each mutation carries its own key — `authz.role.create` / `authz.role.update` / `authz.role.delete` / `authz.binding.create` / `authz.binding.delete` (`module.go::RegisterRoutes`), not the read key. Block B gates every mutation path behind an MFA step-up because each can grant or revoke effective permissions. A pwd-only or oauth-only token fails with 401 `step_up_required`; the client steps up via `/v1/auth/mfa/verify` then retries.

| Method | Path | Purpose |
|---|---|---|
| POST | `/v1/tenants/{tenantId}/authz/roles` | Create a custom role |
| PATCH | `/v1/tenants/{tenantId}/authz/roles/{roleId}` | Update role — custom: name/description/permissions/isActive; system: `isActive` only |
| DELETE | `/v1/tenants/{tenantId}/authz/roles/{roleId}` | Delete custom role — cascades bindings |
| POST | `/v1/tenants/{tenantId}/authz/bindings` | Grant a role with optional expiration |
| DELETE | `/v1/tenants/{tenantId}/authz/bindings/{bindingId}` | Revoke a binding |

Route registration in `handlers/handler.go::RegisterGlobalRoutes`, `::RegisterScopedReadRoutes`, and `::RegisterScopedMutationRoutes`.

**503 `authz.cache_unavailable`** is a possible answer on the mutations that *grant* access and can change an existing verdict: `PATCH .../roles/{roleId}` for a patch that takes no permission away, and `POST .../bindings`. Those pre-invalidate the effective-permission cache before they write and refuse the write when they cannot (D27, see [Key invariants](#key-invariants)). Nothing else answers 503 — the mutations that *take access away* (role delete, binding delete, a role patch that drops a permission) write first and report a failed invalidation through logs and metrics, and `POST .../roles` is deliberately not wrapped at all because a brand-new role has no bindings and so changes nobody's effective permissions. Reason about it by **direction**, not by HTTP method: which verb sits on which side of this rule has already changed twice.

The two custom-role permission validations answer **422**: `authz.permission_unknown` (a key no module registered) and `authz.system_permission_forbidden` (a platform-reserved key, or `*`, inside a tenant role). Both name the offending key in `detail`.

## Service contract

`iface.AuthzProvider` (`pkg/sdk/iface/interfaces.go:216-229`):

```go
HasPermission(ctx, userUUID, orgUUID, permission string) (bool, error)
GetEffectivePermissions(ctx, userUUID, orgUUID string) ([]string, error)
RegisterPermissions(ctx, specs []PermissionSpec) error
```

The service **additionally** satisfies `iface.AuthzCacheInvalidator` (`InvalidateUserPermissions(ctx, userUUID) error`), resolved with `module.GetTyped[iface.AuthzCacheInvalidator]` against `ServiceAuthzProvider`. It is a separate interface rather than a third method on `AuthzProvider` because `AuthzProvider` is additive-only for forks that implement it. Its only in-tree consumer is the **user** module, after a system-role change.

Consumers:
- **`shared/middleware/auth.go`** — `RequirePermission`, `RequireSystemPermission` call `HasPermission` on every request; the result is compared against the route's required permission.
- **The authz module itself** — calls `RegisterPermissions` from the registry post-init hook.
- **Lazy heal** — `ensureSeeded` also calls `RegisterPermissions` + the internal `SeedSystemRoles` when the catalog is empty at query time.
- **The user module** — calls `InvalidateUserPermissions` when `PUT /v1/users/{id}` changes a user's system role, so the next decision uses the new role.

## System and org roles

Seeded by `services/service.go::SeedSystemRoles`. All 11 are stored as global rows (`tenantId=""`, `IsSystem=true`); the platform-vs-tenant distinction comes from how each role is granted, not how it's stored. Permissions are computed from the catalog at seed time:

**Platform-level (granted via global bindings — `binding.tenantId == ""`):**

| Role | Permissions | Notes |
|---|---|---|
| `super_admin` | `["*"]` (wildcard) | Overrides every check in `HasPermission`; the only holder of `*` by design. |
| `administrator` | `allKeys` (every registered permission) | Full platform permissions. Cannot elevate peers to administrator (cascade rule blocks granting roles whose perms exceed caller's). |
| `developer` | `allKeys` in dev/staging; `.read`/`.view`/`.self` in production | Environment-gated (D9). Permission-set distinction from `administrator` outside production is **not yet enforced** — only the name differs. |
| `manager` | `allKeys` filtered to exclude `.delete` and `.admin` suffixes | Read + create + update across the catalog. |
| `operator` | `allKeys` filtered to suffix `.read` or `.self` | Read everything plus self-service. |
| `guest` | `allKeys` filtered to suffix `.read` | Read-only. |

**Tenant-level (granted via tenant-scoped bindings — `binding.tenantId != ""`):**

| Role | Permissions | Notes |
|---|---|---|
| `org_owner` | every non-system permission (`!System`) | Full tenant control. Cannot manage modules, other tenants, or platform users. |
| `org_admin` | `org_owner` set minus `.delete` suffixes | Manages tenant resources but cannot remove them. |
| `org_member` | non-system filtered to `.read`/`.view`/`.self`/`.own` | Read across the tenant plus self/own scopes. |
| `org_billing` | non-system filtered to `billing.*` / `payments.*` / `subscriptions.*` | Finance-surface only. Cedar dispatches via `context.action_module`. |
| `org_viewer` | non-system filtered to `.read`/`.view` | Read-only across the tenant. |

The `binding.tenantId` discipline (system roles only via global bindings, org roles only via tenant bindings) is enforced by `CreateBinding`'s separation rule.

### Binding-creation rules (enforced by `CreateBinding`)

Two rejections fire before any insert. Both apply to manual grants from authenticated users; the platform-issued sentinel granter `"system"` (used by the `OwnerRoleBinder` hook in `tenant.CreateTenant`) bypasses the cascade rule but still respects the separation rule.

1. **System / tenant separation** — platform system roles (`super_admin`, `administrator`, `developer`, `manager`, `operator`, `guest`) require `binding.tenantID == ""`; everything else (`org_*`, custom) requires `binding.tenantID != ""`. Returns `ErrSystemRoleNotGrantableInTenant` or `ErrTenantRoleNotGrantableGlobally`.
2. **Cascade** — caller's effective permissions in the binding's tenant scope must be a superset of the role's permission set. Wildcard `"*"` (held only by `super_admin`) is treated as a universal cover; a role asking for `"*"` requires the caller to also hold `"*"`. Returns `ErrInsufficientPermissionsToGrant`.

A missing `grantedBy` returns `ErrGranterRequired` rather than silently waiving the cascade. Handlers populate `grantedBy` from `middleware.GetUserUUID(ctx)`; the route gates ensure that field is always set in production.

**Duplicate grants:** `authz_bindings` carries a unique `(tenantId, userUUID, roleId)` index (see [MongoDB collections](#authz_bindings-uniqueness-constraint) above). `CreateBinding` still does a plain insert — replaying it against an existing tuple now surfaces the driver's E11000 rather than silently doubling the row, mapped to `ErrBindingExists` (client-facing: `POST /v1/tenants/{tenantId}/authz/bindings` → 409). Callers that want "grant if absent, otherwise return the existing row" semantics call `EnsureBinding` instead — see [Idempotent binding grants](#idempotent-binding-grants-ensurebinding) below.

### Custom-role permission rules (enforced by `CreateRole` / `UpdateRole`)

Both take an `actor` and route every supplied permission list through `validateCustomRolePermissions` (`services/role_validation.go`). Before this existed, `UpdateRole` wrote `permissions` verbatim — any string, including a platform key or `"*"` — and the caller could then bind themselves to the role, which made the role editor the way around `CreateBinding`'s cascade (the audit's H-4).

Four checks, in this order, because each names a different refusal:

1. **Non-empty** after trimming and de-duplication → `ErrRolePermissionsRequired` (400). A `nil` `Permissions` field is a patch that does not touch permissions and is not validated at all; a supplied-but-empty one is.
2. **In the catalog** — every key must be a registered permission → `ErrUnknownPermission` (422 `authz.permission_unknown`, naming the key).
3. **Not platform-reserved** — no key may be `"*"` or carry `System: true` → `ErrSystemPermissionInCustomRole` (422 `authz.system_permission_forbidden`, naming the key). This one binds **everyone**, `super_admin` included: it is about what a *tenant role* may carry, not about what the actor may grant.
4. **Cascade** — the actor's effective permissions in the role's tenant scope must cover every key → `ErrInsufficientPermissionsToGrant` (403). Same `validateBindingCascade` helper `CreateBinding` uses, so a role and a binding cannot drift on what "the caller already holds" means.

An empty actor is `ErrGranterRequired` (400) rather than a silent waiver. The literal sentinel `"system"` bypasses **check 4 only** — and only from in-process callers: the handler refuses a request whose authenticated subject spells the sentinel (`services.IsReservedActor`), so a token can never choose the waiver.

Consequence worth knowing before deploying: a role holding a key from a module a fork removed can still be renamed or disabled, but **its permission list can no longer be saved** until the stale key is dropped — and because the admin UI round-trips the whole list, adding one permission to such a role returns a 422 naming a key the operator never typed.

Permission-evaluation rules (implemented in `services/service.go::GetEffectivePermissions`):

1. If the user's system role is `super_admin`, grant `"*"` and short-circuit.
2. If the system role is `administrator` or `developer`, inherit every permission in the in-memory `systemPermissionSet` (everything marked `System: true` in any module's `Permissions()`).
3. Otherwise, union every active binding for `(userUUID, orgID)` with every global binding (`orgID=""`).
4. System permissions — those declared with `System: true` — and the wildcard `"*"` require a **global** grant (either by system role or by a binding with empty `orgID`), not a per-org binding. The two are one class here because D21 refuses them as one class, and because `"*"` is the maximal case: `HasPermission` short-circuits on it. **This is enforced, not incidental** — the tenant-scoped branch skips such keys (the audit's L-9); before that it held only because no seeded tenant role happens to carry one. A skipped key is **ignored, not removed**: it stays stored and role reads still show it. The **global** branch is deliberately not filtered — that is where a platform role legitimately conveys a platform key (the seeded `administrator` role carries every registered key), and filtering it would strip the operator console's own permissions.

## Idempotent binding grants (`EnsureBinding`)

`Repository.EnsureBinding` / `Service.EnsureBinding` grant a `(tenantID, userUUID, roleID)` tuple **if it does not already exist**, and otherwise return the existing row untouched. This is what makes granting a role safe to replay after a lost response, a crashed executor, or an expired lease — part of the tier-1 default-tenant-setup work whose provisioning saga (a later PR) can re-execute any stage more than once.

- **The service pipeline is identical to `CreateBinding`'s.** `EnsureBinding` runs the same role-active check, system/tenant separation rule, and (for non-`"system"` granters) the cascade rule as `CreateBinding` — both call the shared `validateBindingGrant` helper so the two entry points cannot drift on what a grant must satisfy. It also runs the same post-persist side effect — the MFA-grace hook via `afterBindingGrant` — unconditionally, including on the reused-existing-row path, because `StartMFAGraceIfUnset` no-ops once the clock is already running. **Cache invalidation is no longer one of those side effects**: it is the `withGeneration` wrapper *around* the persist, so for an actor-issued grant it is a precondition of the write rather than a consequence of it (D27). It too runs on both paths, for a different reason — the upsert cannot report in advance whether it will insert.
- **The repository layer is a `$setOnInsert` upsert against the unique compound index**, not a find-then-insert — so two callers racing the same tuple both converge on one persisted row rather than one winning a TOCTOU gap. The loser of the race (an upsert that raced another insert between its find and insert phases) rereads the tuple rather than erroring.
- **An EXPIRED incumbent is not a winner.** "Already exists" means *already granted*, and an expired row grants nothing. `EnsureBinding` reaps it and replays the upsert once, so the caller always ends up with a live binding — see the corresponding invariant under [Key invariants](#key-invariants). Everything below about preserving the winner applies to live rows only.
- **The winner's fields are never overwritten.** A replay with a different `UUID`, `GrantedBy`, or `ExpiresAt` than the original grant returns the *original* row's values — `uuid`, `grantedBy`, `grantedAt`, and `expiresAt` all belong to whoever's insert won. The inserting caller's own `ExpiresAt` **is** honored — `$setOnInsert` sets `expiresAt` for the winning insert exactly like `CreateBinding` does (nil is a legitimate value, persisted as BSON null); only a *losing* caller's `ExpiresAt` is discarded, along with the rest of its payload.
- **The `OwnerRoleBinder` hook (`module.go`) uses `EnsureBinding`, not `CreateBinding`.** All three call sites — `tenant.CreateTenant`, `SetMemberRoles`, `AttachMember` — share this one closure, so all three became replay-safe in the same change. `SetMemberRoles` deletes the old binding and then calls the (now-idempotent) binder; that delete-then-ensure sequence is still not atomic — a crash between the two leaves the member with no binding until the next call — but is no worse than before this change.
- **Deploy prerequisite:** the unique index this depends on must exist before `EnsureBinding` is exercised in an environment with pre-existing duplicate bindings — see the migration note under [MongoDB collections](#authz_bindings-uniqueness-constraint).

## Key invariants

- **System roles are immutable on name/description/permissions.** `UpdateRole` on an `IsSystem=true` row returns `ErrSystemRoleImmutable` (`services/service.go::ErrSystemRoleImmutable`) if the caller touches `Name`, `Description`, or `Permissions`. Only `IsActive` is toggleable. The UI enforces this too, but the service is the authoritative gate.
- **Role seeding preserves UUIDs.** `SeedSystemRoles` (`services/service.go::SeedSystemRoles`) calls `GetRoleByName` before upsert and copies the existing UUID into the new row, so bindings pointing at the old document keep working across boots and across lazy-heal runs.
- **A role name is unique within its tenant, and `CreateRole` may never rewrite an existing row.** The two writes are deliberately different: `Repository.UpsertRole` is keyed on `(tenantId, name)` and belongs to the **seeder**, where converging on one row per system role across boots is the point; `Repository.InsertRole` is a plain insert and belongs to **`CreateRole`**, where the unique `(tenantId, name)` index turns a taken name into `repository.ErrRoleExists` → **409**. Do not merge them. When `CreateRole` went through the upsert, a create naming an existing role replaced that row's `uuid` and `permissions` in place: every binding on the old UUID dangled permanently and its holders lost the role's access silently — no error, and no invalidation, because `CreateRole` bumps nothing. It handed anyone holding only `authz.role.create` a power the catalog reserves to `authz.role.update`/`authz.role.delete`. The index is what makes this race-free; there is no read-then-write window. **Renaming** a custom role into a taken name violates the same index inside `UpdateRoleFields`, whose duplicate-key error is *not* translated and still surfaces as a 500 — recorded as a follow-up, not fixed.
- **Effective-permission cache: 60s in Redis**, keyed on `authz:cache:<globalGen>:<userUUID>:<userGen>:<orgID|"-">`. The two generation counters (`authz:gen` for everyone, `authz:gen:<userUUID>` for one user) are read in **one `MGET`** on every cache read, and invalidation is a single `INCR` of one of them — an entry written under an older generation simply becomes unreachable and dies on its own TTL. Nothing has to *find* a key to retire it, so the old `KEYS <glob>` + `DEL` (which enumerated keys on the hot path, could partially fail, raced a concurrent repopulating read, and built its glob from a request body — the audit's L-11) is gone, along with `cacheInvalidate`. The seams are `InvalidateUserPermissions` (one user, also exported as `iface.AuthzCacheInvalidator`) and `flushCache` (everyone), both reached through `bumpGeneration`.
  - **`MGET` is a narrow optional extension** (`services.MultiGetRedisClient`), type-asserted once at construction, **not** a new method on `module.RedisClient` — that is an SDK contract a fork's own client may implement, so widening it would break every fork. A client without `MGET` bypasses the cache on this replica (every check resolves from Mongo — slower, never wrong) but **still issues the bumps**, because a peer replica may hold entries this mutation must retire.
  - **"No cache configured" is not "cache unavailable".** With no Redis wired, a bump is a truthful no-op *success*: there is no cached verdict to retire. With Redis wired but unreachable, the bump returns an error and the direction rule below decides what that means.
  - 🔴 **Deployment requirement: the Redis backing authz must not evict the generation counters.** `authz:gen` and `authz:gen:<userUUID>` carry **no TTL** — they are the retirement mechanism, not cached data — and a counter that is gone reads back as `0`. That does not fail closed: it *resurrects* every entry written under generation 0 that is still inside its 60s TTL, so a revocation silently undoes itself. Run the instance with `maxmemory-policy noeviction` (evicting nothing) or a `volatile-*` policy (the counters carry no TTL, so `volatile-*` never selects them). **Every `allkeys-*` policy is unsafe here**, because those are the ones that can select a key carrying no TTL. The bundled compose is safe and verified so — it sets no `maxmemory`, so Redis's own defaults apply, and the running stack reports `maxmemory 0` / `maxmemory-policy noeviction`. A managed instance inherits none of that: check its policy before pointing authz at it, because the failure is silent (a revocation that quietly comes back) rather than an error. See also `docker/CLAUDE.md`.
- **Invalidation is a gate for grants and a report for revocations (D27, as amended).** A stale verdict after a *grant* is a harmless late DENY; after a *revocation* it is a live ALLOW. So the two directions get opposite shapes, and neither is "atomic and total":
  - **Grants** — an `UpdateRole` patch that only ADDS permissions or sets `isActive: true`, and `CreateBinding`/`EnsureBinding` from a real actor — run pre-invalidate → write → post-invalidate (`withGeneration`). A failed **pre**-bump refuses the whole change with `ErrAuthzCacheUnavailable` → **503 `authz.cache_unavailable`**, and nothing is written. A failed **post**-bump is logged and counted but never fatal: the write landed.
  - **Revocations and platform-issued grants** — `DeleteRole`, `DeleteBinding`, `RemoveBindingsByTenant`, `RemoveBindingsByUserAndTenant`, an `UpdateRole` patch that drops a permission **or sets `isActive: false`**, and any grant from the `"system"` sentinel — write first and then invalidate best-effort (`writeThenInvalidate`). A failed bump is logged and counted, **never** a refusal: refusing a revocation would leave the privilege granted indefinitely, where writing it leaves at most a 60s stale allow.
    - **`isActive: false` is the largest revocation `UpdateRole` can make**, and the permission set-difference cannot see it: the stored list does not change, but the evaluator skips every binding whose role is inactive, so the patch drops every permission the role carries from every holder — platform-wide for a system row. It was routed through the gate until the whole-branch review of PR C; with the cache store down the operator could not disable a role at all and every holder kept the access, which is the exact inversion the direction rule exists to prevent. The console's `RolesTable.onToggleActive` sends precisely this patch. **Enabling** a role is the other direction and keeps the gate.
  - **A patch that can change no verdict retires nothing at all** — a `name` and/or `description` edit, and nothing else. The cache stores permission KEYS (`GetEffectivePermissions` is its only writer) and no evaluator path reads a role's name, so retiring on a rename cost every user on the platform two cold-cache waves for a change none of them could observe. `bindings.roleName` is a denorm written at grant time and read by nothing in evaluation, so it does not make a rename verdict-affecting either (it does go stale — pre-existing, unrelated to the cache).
  - **`CreateRole` bumps nothing, in either shape.** A role it creates has no bindings, so it cannot change anybody's effective permissions. That premise is *enforced*: `CreateRole` writes through `InsertRole`, so a name already taken in the tenant is a **409**, not a silent rewrite of the incumbent (see the role-name invariant below). `mapRoleWriteError` carries the 503 row for `UpdateRole`'s sake only — do not "fix" the asymmetry by wrapping `CreateRole`.
  - The bump is applied **inside** each method, after its own validation and only on the path that writes. Wrapping a whole method would let a request that is about to be refused 403/404 flush the cache — a remotely triggerable flush at request rate.
  - Two metric families, two different alerts: `orkestra_authz_cache_invalidation_failures_total` (the change landed, a stale verdict may survive its TTL) and `orkestra_authz_cache_invalidation_refusals_total` (nothing was written).
  - Residual, deliberate: a reader that resolves Mongo across the whole pre/write/post window can still publish a stale entry, bounded by the 60s TTL.
- **Lazy-heal uses an in-memory spec cache.** `RegisterPermissions` stores the full `[]PermissionSpec` under the service's mutex as `cachedPermSpecs`. `ensureSeeded` reuses it to rebuild the catalog if the DB is dropped at runtime. If you touch `RegisterPermissions`, make sure the cache is still populated — otherwise the first admin who drops the DB in dev will get an empty `/admin/roles` and no automatic recovery.
- **`CreateBinding` enforces a cascade rule** — caller's effective permissions in the binding's tenant scope must be a superset of the role's permission set, otherwise the call returns `ErrInsufficientPermissionsToGrant` (commit C of the org-role split, 2026-04-24). Wildcard `*` (super_admin) bypasses; the literal sentinel granter `"system"` bypasses for platform-issued auto-grants like the `OwnerRoleBinder` hook in `tenant.CreateTenant`.
- **`CreateBinding` enforces system / tenant separation** — platform system roles (the 6 named in `platformSystemRoleNames`) require `binding.tenantID == ""` (`ErrSystemRoleNotGrantableInTenant`); everything else (`org_*`, custom roles) requires `binding.tenantID != ""` (`ErrTenantRoleNotGrantableGlobally`).
- **`CreateBinding` eagerly starts the target's MFA enrollment grace clock** when the granted role is `super_admin`, `administrator`, `org_owner`, or `org_admin`. Implemented via the `MFAGraceStarter` callback (see `Config.StartMFAGrace`) so the service stays free of a direct user-module import. The callback is idempotent — repeated grants don't reset an already-running clock. This list must stay in sync with `auth/services/mfa_policy.go::RoleRequiresMFA`; if they drift a user could be gated at login without ever having had their grace window started.
- **Binding expiration is advisory, not TTL.** The `expiresAt` index exists, but it's not declared as a TTL index. Expired bindings are filtered out by `ListActiveBindingsForUser` but they stay in the collection. A background reaper is future work.
- **An expired binding never blocks a re-grant.** This is the corollary of the previous invariant once the unique `(tenantId, userUUID, roleId)` index exists: a dead row occupies the tuple forever, so without this rule an expired contractor grant would make the role permanently un-re-grantable — `CreateBinding` answering 409 `ErrBindingExists` about a grant conferring nothing, and `EnsureBinding` returning the dead row while reporting success (a silent no-op on all three `OwnerRoleBinder` call sites, which discard the returned row). Both repository methods therefore **reap the tuple's own expired row and retry exactly once** (`reapExpiredBinding`). Only a genuinely expired incumbent is displaced — a live grant, permanent or expiring in the future, still produces the duplicate-key error. **The reap filter must keep its `$type: "date"` clause**: BSON's canonical type ordering sorts null *below* dates, so a bare `{"expiresAt": {"$lte": now}}` also matches permanent grants and would reap the very rows that must never be touched. Regression tests: `repository/binding_expiry_integration_test.go`.
- **`DeleteRole` cascades bindings.** The service calls `DeleteBindingsByRoleUUID` before the role delete so nothing is left pointing at a nonexistent role. System roles refuse to delete regardless (via the repository's `isSystem=false` filter).
- **Tenant deletion cascades bindings.** The authz module registers a `TenantPostDeleteHook` on the tenant service (`module.go::Init`) that calls `RemoveBindingsByTenant` for every deleted/purged tenant and then flushes the effective-permission cache — best-effort, and only when rows were actually removed (`TestRemoveBindingsByTenant_NoMatch_DoesNotFlush` pins the second half). **A failed flush never fails the hook**: the rows are gone, and an error there would surface as `tenant.cascade.hook_failed` — a false signal for a cascade that succeeded and only failed to retire a cache. The member-unbind hook (`RemoveBindingsByUserAndTenant`) is the same shape, with one more reason: `tenant.SetMemberRoles` calls it and then re-binds, so aborting between the two would leave the member unbound while the membership denorm still shows the role. Without this, dropped tenants leave dangling `org_owner` / `org_admin` / custom-role rows the evaluator would still consult if the tenant UUID were re-used. Global bindings (`tenantId=""`) are untouched — those carry platform system roles that outlive any single tenant.

## What this module does NOT do

- User identity and system-role storage → **user** module (`User.Role` field)
- Org ownership, membership, plan entitlements → **tenant** module
- Middleware enforcement → **`shared/middleware/auth.go`** consumes `HasPermission`
- JWT claims — permissions are never embedded; only the system role and memberships are
- Audit logging of role changes — there is no audit stream today; future work

## Rules

- **Never hardcode a role name outside of `SeedSystemRoles`.** Role renames must be a single-file change. Middleware code that needs to special-case `super_admin` / `administrator` / `developer` belongs in the evaluator, not in the handler layer.
- **Never remove `ensureSeeded` from `ListRoles` / `ListPermissions`.** It's the only thing making the admin UI self-heal after a dev DB wipe. If you optimize it, still keep the empty-collection branch.
- **Never bypass `CanDeliver`-style checks for system roles** — a super_admin should see every permission on `/v1/tenants/{tenantId}/authz/me`, which means the wildcard must be preserved in the cache serialization.
- **When adding a new permission**, always declare it in the owning module's `Permissions()` — never write directly to the `authz_permissions` collection. The registry-collected list is the single source of truth and drives the computed role sets at seed time.
- **Custom roles are scoped to one org.** Their `orgId` field is non-empty. Never copy custom-role UUIDs across orgs; always resolve by `(orgId, name)` or UUID within the target org.
- **System permissions need a global grant.** Do not try to "grant `system.modules.admin` in org X" — the evaluator requires `orgID=""` for system permissions. Bindings with a non-empty `orgId` holding system permissions will silently not match in the evaluator — that is now the tenant branch's explicit filter (evaluation rule 4 above), not a happy accident of the seeded role sets.

## What's planned but not done

- **Permission domain tags** — tagging each permission as `system` / `technical` / `business` so the developer role can be seeded as "all system+technical permissions, read-only on business".
- **TTL on `authz_bindings.expiresAt`** — flip the plain index to a TTL index so expired contractor grants auto-reap.
- **Audit trail for role CRUD and binding CRUD** — part of future SOC2 work.

## Org-scoping invariants (system-wide)

These invariants apply across **every** module, not just authz. They are the enforceable contract the org-scoped RBAC plan delivers. Where an invariant is not yet enforced, it's marked **planned**; implementation phase is noted.

| # | Invariant | Enforcement today | Full enforcement |
|---|---|---|---|
| 1 | Every non-global data read/write carries `ctxOrgID` | `tenantrepo.Scope()` helper exists; panics in dev when missing | CI linter `tools/tenantscope` (`make backend-tenantscope`) flags raw `collection.Find/UpdateOne/...` across `internal/**` when the filter doesn't come from `tenantrepo.Scope*`. |
| 2 | `X-Tenant-ID` must match a membership carried in the JWT | ✅ `shared/middleware/auth.go::resolveCurrentTenant`, with one exception: holders of `system.tenants.admin` bypass the membership check (`tryImpersonationBypass`) so operator admins can act in any tenant. Every impersonation emits an `admin.tenant.impersonate.{personal,business}` audit event (split by target's IsCompany+SignupChannel shape) and sets `ctxTenantImpersonated=true` in context. Personal targets additionally require an MFA-satisfied session — pwd-only sessions hit a 401 step-up before the bypass applies. | Keep. |
| 3 | System roles (`super_admin`/`administrator`/`developer`/`manager`/`operator`/`guest`) are **platform-level**; org roles (`org_owner`/`org_admin`/`org_member`/`org_viewer`/`org_billing`) are **tenant-level**. Never mix. | ✅ org roles seeded globally (commit A 2026-04-24); `CreateBinding` enforces system-role-needs-global / tenant-role-needs-tenant separation (commit C 2026-04-24). | Keep. |
| 4 | Permission checks always run in a resolved org context unless the route uses `RequireGlobal()` or `RequireSystemPermission()` | ✅ middleware chain | Keep. |
| 5 | A user cannot grant a role whose permissions they themselves lack | ✅ `CreateBinding` cascade rule (commit C 2026-04-24) **and, since D21, `CreateRole`/`UpdateRole`** return `ErrInsufficientPermissionsToGrant`. Wildcard `*` (super_admin) bypasses; the literal sentinel granter `"system"` bypasses for platform-issued auto-grants. Both entry points share `validateBindingCascade`, so the role editor is no longer the way around the binding rule. | Keep. |
| 6 | Org owner is immutable without a transfer flow (`ownerUserUUID` cannot be directly reassigned) | ❌ — no transfer flow today | **planned (Phase 2)**: two-step `POST /v1/tenants/{id}/transfer-ownership` (initiate → accept); both parties emailed; audit logged. |
| 7 | All secrets AES-256-GCM encrypted at rest | ✅ `pkg/sdk/module/config_service.go` | Keep. Phase 5 extends to per-org secrets via `(module, env, orgId)` key. |
| 8 | All mutations audited to an append-only, tamper-evident log | ❌ — only session events logged in `auth_sessions.securityEvents` | **planned (Phase 3)**: `audit_events` collection (hash-chained, insert-only role) → Loki (90d observability) → S3 Object Lock EU (7y WORM) dual-sink. SOC2 requirement. |
| 9 | Every side effect references both actor (userUUID) and principal (orgID), including background jobs | ❌ — jobs today carry no identity context | **planned (Phase 3)**: audit middleware populates both; background jobs run under a synthetic "system" actor with logged justification. |

Violating one of these is a security bug. When reviewing a PR that touches any `addons/**` module, walk the 9 invariants as a checklist.

## Permission naming convention

Target form: `<module>.<resource>.<action>[.<scope>]`

- **Actions**: `read`, `create`, `update`, `delete`, `admin` — plus a small set of domain-verbs where the operation is specific enough that the generic action would mislead: `send` (billing), `refund` (payments), `ingest` (rag), `query` (rag/agents/graph), `generate` (documents), `enrich` (company), `run` (sales), `test` (notification).
- **Scopes** (optional suffix): `self` (actor's own row), `own` (rows where `createdBy == actor`). Unscoped = every row the role reaches within the resolved org.

Current catalog status (snapshot as of this doc update — the live catalog is in each module's `Permissions()` method):

| Form | Count | Examples | Status |
|---|---|---|---|
| `<mod>.<resource>.<action>` | ~30 | `billing.invoice.read`, `tenant.plan.update` | ✅ compliant |
| `<mod>.<action>` (no resource, action is the full capability) | ~10 | `rag.query`, `agents.admin`, `auth.self` | ✅ acceptable where the module has one main resource |
| `system.<resource>.admin` | 3 | `system.modules.admin`, `system.users.admin`, `system.tenants.admin` | ✅ platform-level reserved prefix |
| `<mod>.<resource>.view` | 13 | `subscriptions.client.view`, `payments.transaction.view` | ⚠️ **rename to `read`** in Phase 4 along with collection migration (avoid touching `authz_permissions` twice) |
| `<mod>.<resource>.manage` | 8 | `billing.customer.manage`, `documents.template.manage` | ⚠️ semantically "create+update+delete" — leave as-is for v1; split into `.update` + `.delete` only when `.own` scope introduces asymmetry |
| `<mod>.<resource>.<action>.own` | 0 | — | planned (Phase 4) for per-module self-service (`billing.invoice.read.own`) |

**When adding a new permission:** declare it in the owning module's `Permissions()` only. Never write directly to `authz_permissions`. Include `System: true` only for platform-level operations that system roles inherit without a binding. Then ensure Cedar coverage — either name it as `Action::"<key>"` in a `cedar/policies/*.cedar` file, or use a suffix already covered by `context.action_suffix == "X"` (today: `read`, `view`, `self`); otherwise the `policycoverage` CI gate will fail with `permission.cedar.unreferenced`.

**Cedar enforce mode:** the `CEDAR_ENFORCE_ACTIONS` env var (comma-separated permission keys) opts each listed action out of shadow mode and into Cedar-authoritative mode — for those actions Cedar's verdict overrides the role table, including the tier-aware forbids in `tenant_scope.cedar`. Unset (the default) keeps every action in pure shadow mode. Cedar-side failures during an enforced check fall back to the role-table verdict (logged at Error, counted as `fallback_role` on `orkestra_cedar_enforced_total`). Recommended starter list: `system.modules.admin,system.tenants.admin,system.users.admin,system.users.mfa_reset` — those four were the original `Action::` literals named in `tenant_scope.cedar`. The same file now also names `system.users.password_reset`, `system.users.email_verify_resend`, and `system.users.oauth_unlink` (admin-managed user-credential operations) plus `system.compliance.legalhold.manage` and `system.compliance.dsr.manage` (operator-side GDPR legal-hold / right-to-erasure management, [ADR-0009](../../../../docs/adr/0009-core-compliance-module.md)) as operator-only forbids; fold them into the enforced list as they roll out. Roll back is a single env-var change.

`system_actions.require_platform_role` now also constrains **who** may hold those keys, not only which of them exist — and it is shadow-only until the divergence window below is quiet. 🔴 **That window starts after BOTH the forbid and the D24 stamp gate have landed, not at the first of them:** the forbid produces divergence precisely on the global-check path D24 fixes, so measuring from the forbid alone counts noise D24 removes and trains an operator to dismiss real divergence as expected. Expect telemetry to move for `manager` / `operator` / `guest` when it does: those roles have no `platform.*` permit in Cedar, and `manager` is seeded with `allKeys` minus `.delete`/`.admin` — which includes `auth.service_accounts.manage` and `notification.log.read`. A user holding one globally *and* an org role in the resolved tenant previously had the divergence masked by the `org_owner` permit; it now surfaces as `role_only`. That is a pre-existing Cedar/role-table gap the stamp gate exposes, not a new one.

**Divergence severity is deployment-dependent.** Every shadow evaluation logs `cedar: agree` at Debug; a disagreement logs `cedar: divergence` and increments `orkestra_cedar_shadow_divergence_total` (labelled `role_only` / `cedar_only` / `neither`). The line is **Warn outside production and Error when `ENV=production`** (`Config.IsProduction()`, the same flag that restricts the developer role to read-only). Below production a divergence is the expected output of policy work in progress and a permanent Error would train operators to ignore it; in production it is a disagreement about a real authorization decision that no one is watching a rollout for, so it has to reach whatever alerts on Error.

## Cedar ABAC attributes

Section B item #4 of the auth roadmap (2026-04-24) plumbs attribute-based signals through the shadow evaluator so policies can gate beyond role membership. Every attribute below is available on every evaluation — policies consume them via `policies/abac.cedar` (and extensions there).

| Entity    | Attribute         | Type         | Source |
|-----------|-------------------|--------------|--------|
| principal | `system_role`     | String       | user module, present when non-empty |
| principal | `tenant_roles`    | Set<String>  | TenantMembership.Roles, present when non-empty **and the check is tenant-scoped**. A global check (`tenantID == ""` — every `RequireSystemPermission` route, and the impersonation pre-check) stamps it not at all, so a tenant-role permit cannot fire on a decision whose resource is not that tenant (D24). |
| principal | `capabilities`    | Set<String>  | AccessProvider.ListCapabilityIDs(TenantOwner(tenantUUID)), present when non-empty |
| principal | `mfa_enrolled`    | Bool         | JWT `amr` claim → middleware.IsMFAEnrolled — always stamped |
| principal | `amr`             | Set<String>  | JWT `amr` claim, present when non-empty |
| resource  | `kind`            | String       | TenantProvider.GetTenant.Kind (internal/external) |
| resource  | `status`          | String       | TenantProvider.GetTenant.Status (live since Commit A; was hardcoded "active" before) |
| context   | `env`             | String       | deployment env |
| context   | `action_key`      | String       | full permission key |
| context   | `action_module`   | String       | substring before first `.` |
| context   | `action_suffix`   | String       | substring after last `.` |
| context   | `hour_utc`        | Long (0–23)  | injectable engine clock, defaults to UTC wall time |
| context   | `weekday`         | String       | `mon`…`sun` from the same clock |
| context   | `ip_bucket`       | String       | `loopback` / `private` / `public` / `unknown` — pre-classified via `net.ParseIP`, no CIDR in Cedar |
| context   | `requires_capability` | String   | only present when `RequiredCapability` is set on the request |

**Shipped Cedar policies** (all shadow-mode at landing — flip via `CEDAR_ENFORCE_ACTIONS`):

| @id | Shape | Fires when |
|-----|-------|------------|
| `abac.require_mfa_for_admin_suffix` | forbid | principal.system_role ∈ {super_admin, administrator} AND context.action_suffix == "admin" AND principal.mfa_enrolled == false |
| `abac.deny_system_actions_from_public_ip_in_prod` | forbid | action ∈ the 7 system.* literals (`system.modules.admin`, `system.tenants.admin`, `system.users.{admin,mfa_reset,password_reset,email_verify_resend,oauth_unlink}`) AND context.env == "production" AND context.ip_bucket == "public" |
| `system_actions.require_platform_role` | forbid | `context.action_module == "system"` AND the principal does **not** hold one of `{super_admin, administrator, developer}` in `system_role` — including a principal carrying no `system_role` attribute at all. Closes H-5: every `tenant_roles.*` permit is written for a tenant resource with no action constraint, so on an internal tenant they fire on `system.*` too and an `org_owner` would hold `system.users.admin` under enforce. Module-keyed rather than action-keyed so future `system.*` keys and future tenant-role permits are both covered without an edit. |

**Two caveats on that last row, both recorded in the policy file's own header:**

- **`policycoverage` cannot tell a permit from a forbid.** `scanCedar` reads the `context.action_module == "system"` clause as *coverage* for every permission under module `system`. That is inert today (all ten `system.*` keys are already named as `Action::"…"` literals in `tenant_scope.cedar` / `abac.cedar`), but a future `system.*` key landing with no permit naming it would have its `permission.cedar.unreferenced` diagnostic masked by this forbid.
- **The forbid keys on the module prefix; the platform reservation is the `System: true` flag.** They are not the same set: fifteen keys are `System: true`, ten carry the `system.` prefix, five do not (`auth.service_accounts.read`, `auth.service_accounts.manage`, `notification.log.read`, `notification.template.manage`, `notification.test`). The first three are live and gated by `RequireSystemPermission`; what closes them is **D24**, not this forbid — `RequireSystemPermission` checks with an empty tenantID, and `shadowEvaluate` no longer stamps the caller's `tenant_roles` on a check with no tenant, so no tenant-role permit can fire there. The last two gate no route (a pre-existing catalog artefact). Keying the forbid on the flag would need a `context.action_is_system` stamp from `shadowEvaluate` — a change to the rule itself, left to the spec owner.

**IP bucketing:** Cedar has no CIDR or netmask support, so `ip_bucket` is pre-classified by the engine. Policies never see the raw IP. If you need a new bucket (e.g. `allowlist` for a per-tenant CIDR config), extend `classifyIP` in `cedar/engine.go` and document the new value here — drift between policy literals and the classifier is a silent bug.

## Related

- [`../user/CLAUDE.md`](../user/CLAUDE.md) — provides the system role lookup
- [`../tenant/CLAUDE.md`](../tenant/CLAUDE.md) — provides `Membership.Roles`, which is a denormalized view of this module's bindings
- [`../auth/CLAUDE.md`](../auth/CLAUDE.md) — consumes `HasPermission` via middleware on every protected request
- [`../../../pkg/sdk/iface/interfaces.go:198-229`](../../../pkg/sdk/iface/interfaces.go) — `AuthzProvider` and `PermissionSpec` definitions
- [`../../shared/middleware/auth.go`](../../shared/middleware/auth.go) — `RequirePermission`, `RequireSystemPermission`
