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

- **Init** (`module.go:87-111`): constructs the repository, builds the user-role lookup closure (calls `UserProvider.GetUserByID` and reads `.Role`), wires the service, registers it as `iface.AuthzProvider`. The lookup has a **dev-token fallback**: when the DB lookup fails, it falls back to the JWT context system role only if all three guards pass — (1) non-production environment, (2) UUID starts with `dev-`, (3) role is in the hardcoded `validDevRoles` allow-list. This lets synthetic dev-token users (which have no DB record) work with the authz evaluator.
- **Registry post-init** (`shared/module/registry.go:183-211`): after every module has run its `Init`, the registry calls `authz.RegisterPermissions` with the union of every module's `Permissions()`, then calls `authz.SeedSystemRoles` which derives the six roles' permission lists from the now-complete catalog.
- **Start / Stop / HealthCheck**: inherit from `BaseModule`.
- **Lazy-heal** (`services/service.go::ensureSeeded`): `ListRoles` and `ListPermissions` both call `ensureSeeded` before querying the repo. If the system-role count is zero, the service re-runs `RegisterPermissions` + `SeedSystemRoles` from an in-memory copy of the spec list (`cachedPermSpecs`). This is what makes `/admin/roles` self-heal after a live DB drop without a backend restart. See the notes in [Key invariants](#key-invariants) below — do not remove `cachedPermSpecs` or the `ensureSeeded` calls.

## HTTP endpoints

Two route groups (`module.go:113-127`):

### Global — `RequireGlobal()`

| Method | Path | Purpose |
|---|---|---|
| GET | `/v1/authz/permissions` | List the permission catalog (system-generated) |

### Per-org — read (`RequirePermission("authz.role.read")`)

| Method | Path | Purpose |
|---|---|---|
| GET | `/v1/orgs/{orgId}/authz/roles` | List roles (system + custom scoped to this org) |
| GET | `/v1/orgs/{orgId}/authz/bindings` | List role bindings in the org |
| GET | `/v1/orgs/{orgId}/authz/me` | Return the caller's effective permissions in this org |

### Per-org — mutation (`RequirePermission("authz.role.read")` + `RequireMFA()`)

Block B gates every mutation path behind an MFA step-up because each can grant or revoke effective permissions. A pwd-only or oauth-only token fails with 401 `step_up_required`; the client steps up via `/v1/auth/mfa/verify` then retries.

| Method | Path | Purpose |
|---|---|---|
| POST | `/v1/orgs/{orgId}/authz/roles` | Create a custom role |
| PATCH | `/v1/orgs/{orgId}/authz/roles/{roleId}` | Update role — custom: name/description/permissions/isActive; system: `isActive` only |
| DELETE | `/v1/orgs/{orgId}/authz/roles/{roleId}` | Delete custom role — cascades bindings |
| POST | `/v1/orgs/{orgId}/authz/bindings` | Grant a role with optional expiration |
| DELETE | `/v1/orgs/{orgId}/authz/bindings/{bindingId}` | Revoke a binding |

Route registration in `handlers/handler.go::RegisterGlobalRoutes`, `::RegisterScopedReadRoutes`, and `::RegisterScopedMutationRoutes`.

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
- **`CreateBinding` enforces a cascade rule** — caller's effective permissions in the binding's tenant scope must be a superset of the role's permission set, otherwise the call returns `ErrInsufficientPermissionsToGrant` (commit C of the org-role split, 2026-04-24). Wildcard `*` (super_admin) bypasses; the literal sentinel granter `"system"` bypasses for platform-issued auto-grants like the `OwnerRoleBinder` hook in `tenant.CreateTenant`.
- **`CreateBinding` enforces system / tenant separation** — platform system roles (the 6 named in `platformSystemRoleNames`) require `binding.tenantID == ""` (`ErrSystemRoleNotGrantableInTenant`); everything else (`org_*`, custom roles) requires `binding.tenantID != ""` (`ErrTenantRoleNotGrantableGlobally`).
- **`CreateBinding` eagerly starts the target's MFA enrollment grace clock** when the granted role is `super_admin`, `administrator`, `org_owner`, or `org_admin`. Implemented via the `MFAGraceStarter` callback (see `Config.StartMFAGrace`) so the service stays free of a direct user-module import. The callback is idempotent — repeated grants don't reset an already-running clock. This list must stay in sync with `auth/services/mfa_policy.go::RoleRequiresMFA`; if they drift a user could be gated at login without ever having had their grace window started.
- **Binding expiration is advisory, not TTL.** The `expiresAt` index exists, but it's not declared as a TTL index. Expired bindings are filtered out by `ListActiveBindingsForUser` but they stay in the collection. A background reaper is future work.
- **`DeleteRole` cascades bindings.** The service calls `DeleteBindingsByRoleUUID` before the role delete so nothing is left pointing at a nonexistent role. System roles refuse to delete regardless (via the repository's `isSystem=false` filter).
- **Tenant deletion cascades bindings.** The authz module registers a `TenantPostDeleteHook` on the tenant service (`module.go::Init`) that calls `RemoveBindingsByTenant` for every deleted/purged tenant and flushes the effective-permission cache. Without this, dropped tenants leave dangling `org_owner` / `org_admin` / custom-role rows the evaluator would still consult if the tenant UUID were re-used. Global bindings (`tenantId=""`) are untouched — those carry platform system roles that outlive any single tenant.

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

- **Permission domain tags** — tagging each permission as `system` / `technical` / `business` so the developer role can be seeded as "all system+technical permissions, read-only on business".
- **TTL on `authz_bindings.expiresAt`** — flip the plain index to a TTL index so expired contractor grants auto-reap.
- **Audit trail for role CRUD and binding CRUD** — part of future SOC2 work.

## Org-scoping invariants (system-wide)

These invariants apply across **every** module, not just authz. They are the enforceable contract the org-scoped RBAC plan ([memory](../../../../../home/tore/.claude/projects/-mnt-c-Users-tore-orkestra/memory/project_org_rbac_plan.md) / PR description) delivers. Where an invariant is not yet enforced, it's marked **planned**; implementation phase is noted.

| # | Invariant | Enforcement today | Full enforcement |
|---|---|---|---|
| 1 | Every non-global data read/write carries `ctxOrgID` | `tenantrepo.Scope()` helper exists; panics in dev when missing | **planned (Phase 0)**: CI linter `tools/tenantscope` flags raw `collection.Find/UpdateOne/...` in `internal/addons/**` when filter doesn't come from `tenantrepo.Scope*`. **Phase 4**: every addon repo migrated to use it. |
| 2 | `X-Tenant-ID` must match a membership carried in the JWT | ✅ `shared/middleware/auth.go::resolveCurrentTenant`, with one exception: holders of `system.tenants.admin` bypass the membership check (`tryImpersonationBypass`) so operator admins can act in any tenant. Every impersonation emits an `admin.tenant.impersonate.{personal,business}` audit event (split by target's IsCompany+SignupChannel shape) and sets `ctxTenantImpersonated=true` in context. Personal targets additionally require an MFA-satisfied session — pwd-only sessions hit a 401 step-up before the bypass applies. | Keep. |
| 3 | System roles (`super_admin`/`administrator`/`developer`/`manager`/`operator`/`guest`) are **platform-level**; org roles (`org_owner`/`org_admin`/`org_member`/`org_viewer`/`org_billing`) are **tenant-level**. Never mix. | ✅ org roles seeded globally (commit A 2026-04-24); `CreateBinding` enforces system-role-needs-global / tenant-role-needs-tenant separation (commit C 2026-04-24). | Keep. |
| 4 | Permission checks always run in a resolved org context unless the route uses `RequireGlobal()` or `RequireSystemPermission()` | ✅ middleware chain | Keep. |
| 5 | A user cannot grant a role whose permissions they themselves lack | ✅ `CreateBinding` cascade rule (commit C 2026-04-24) returns `ErrInsufficientPermissionsToGrant`. Wildcard `*` (super_admin) bypasses; the literal sentinel granter `"system"` bypasses for platform-issued auto-grants. | Keep. |
| 6 | Org owner is immutable without a transfer flow (`ownerUserUUID` cannot be directly reassigned) | ❌ — no transfer flow today | **planned (Phase 2)**: two-step `POST /v1/orgs/{id}/transfer-ownership` (initiate → accept); both parties emailed; audit logged. |
| 7 | All secrets AES-256-GCM encrypted at rest | ✅ `shared/module/config_service.go` | Keep. Phase 5 extends to per-org secrets via `(module, env, orgId)` key. |
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

**Cedar enforce mode:** the `CEDAR_ENFORCE_ACTIONS` env var (comma-separated permission keys) opts each listed action out of shadow mode and into Cedar-authoritative mode — for those actions Cedar's verdict overrides the role table, including the tier-aware forbids in `tenant_scope.cedar`. Unset (the default) keeps every action in pure shadow mode. Cedar-side failures during an enforced check fall back to the role-table verdict (logged at Error, counted as `fallback_role` on `orkestra_cedar_enforced_total`). Recommended starter list: `system.modules.admin,system.tenants.admin,system.users.admin,system.users.mfa_reset` — those four were the original `Action::` literals named in `tenant_scope.cedar`. The same file now also names `system.users.password_reset`, `system.users.email_verify_resend`, and `system.users.oauth_unlink` (admin-managed user-credential operations) as operator-only forbids; fold them into the enforced list as they roll out. Roll back is a single env-var change.

## Cedar ABAC attributes

Section B item #4 of the auth roadmap (2026-04-24) plumbs attribute-based signals through the shadow evaluator so policies can gate beyond role membership. Every attribute below is available on every evaluation — policies consume them via `policies/abac.cedar` (and extensions there).

| Entity    | Attribute         | Type         | Source |
|-----------|-------------------|--------------|--------|
| principal | `system_role`     | String       | user module, present when non-empty |
| principal | `tenant_roles`    | Set<String>  | TenantMembership.Roles, present when non-empty |
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

**Shipped ABAC policies** (all shadow-mode at landing — flip via `CEDAR_ENFORCE_ACTIONS`):

| @id | Shape | Fires when |
|-----|-------|------------|
| `abac.require_mfa_for_admin_suffix` | forbid | principal.system_role ∈ {super_admin, administrator} AND context.action_suffix == "admin" AND principal.mfa_enrolled == false |
| `abac.deny_system_actions_from_public_ip_in_prod` | forbid | action ∈ the 7 system.* literals (`system.modules.admin`, `system.tenants.admin`, `system.users.{admin,mfa_reset,password_reset,email_verify_resend,oauth_unlink}`) AND context.env == "production" AND context.ip_bucket == "public" |

**IP bucketing:** Cedar has no CIDR or netmask support, so `ip_bucket` is pre-classified by the engine. Policies never see the raw IP. If you need a new bucket (e.g. `allowlist` for a per-tenant CIDR config), extend `classifyIP` in `cedar/engine.go` and document the new value here — drift between policy literals and the classifier is a silent bug.

## Related

- [`../user/CLAUDE.md`](../user/CLAUDE.md) — provides the system role lookup
- [`../tenant/CLAUDE.md`](../tenant/CLAUDE.md) — provides `Membership.Roles`, which is a denormalized view of this module's bindings
- [`../auth/CLAUDE.md`](../auth/CLAUDE.md) — consumes `HasPermission` via middleware on every protected request
- [`../../shared/iface/interfaces.go:198-229`](../../shared/iface/interfaces.go) — `AuthzProvider` and `PermissionSpec` definitions
- [`../../shared/middleware/auth.go`](../../shared/middleware/auth.go) — `RequirePermission`, `RequireSystemPermission`
