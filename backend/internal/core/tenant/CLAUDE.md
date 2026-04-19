# Module: Tenant — Organizations, memberships, plan entitlements

_Path: `/backend/internal/core/tenant`_
_Parent: [../CLAUDE.md](../CLAUDE.md)_

[← Core](../CLAUDE.md) | [☰ Backend](../../../CLAUDE.md) | [Root](../../../../CLAUDE.md)

## Purpose

Owns the multi-tenant layer: organizations, per-user memberships, plan-based feature entitlements, and the invite lifecycle. Implements `iface.TenantProvider` so the auth module can embed memberships in JWTs, middleware can resolve the current org, and any module that needs "does this user belong to this org?" or "does this plan include this feature?" can do it with a single call.

Does not own org-scoped roles or permissions — those are authz role bindings. The `Membership.Roles` field is a denormalized list of authz role names for fast reads.

## What it owns

| File | Purpose |
|---|---|
| `module.go` | Module registration, collections, permissions, service wire-up |
| `handlers/handler.go` | HTTP handlers for org and membership CRUD + invites |
| `services/service.go` | Org lifecycle, membership sync, invite token issuance, `iface.TenantProvider` implementation |
| `repository/repository.go` | MongoDB CRUD for orgs, memberships, invites |
| `models/org.go` | `Org`, `Membership`, `Invite` structs + plan/feature constants |

## MongoDB collections

Declared in `module.go:35-55`.

| Collection | Indexes | TTL |
|---|---|---|
| `tenant_orgs` | `uuid` unique, `slug` unique sparse, `ownerUserUUID` | — |
| `tenant_memberships` | compound `(userUUID, orgId)` unique, `orgId` | — |
| `tenant_org_invites` | `tokenHash` unique, `orgId`, `expiresAt` TTL(ExpireAt) | `expiresAt` is a TTL index with `expireAfterSeconds=0` so Mongo reaps the doc the moment the timestamp passes. |

Collection name constants live in `repository/repository.go` as `CollOrgs`, `CollMemberships`, `CollInvites`.

## Dependencies

- **Modules**: `user` (`module.go:29`) — so user profiles exist before memberships reference them.
- **Required services**: none (the service does not currently look up users via the provider, it trusts the caller's auth context).
- **Optional services**: none.
- **Provides**: `ServiceTenantProvider` → `iface.TenantProvider` (`module.go:31-33`).
- **Permissions contributed** (`module.go:58-68`):

| Key | System? | Purpose |
|---|---|---|
| `tenant.org.read` | no | Read org details |
| `tenant.org.update` | no | Update org name, slug, settings |
| `tenant.org.delete` | no | Soft-delete the org |
| `tenant.plan.update` | no | Change plan and features |
| `tenant.member.read` | no | List org members |
| `tenant.member.invite` | no | Invite new members |
| `tenant.member.remove` | no | Remove members from the org |
| `system.tenants.admin` | **yes** | Administer every tenant platform-wide (powers `/v1/admin/orgs/*`) |

## Lifecycle

- **Init** (`module.go:70-76`): constructs the repository, builds the service, creates the handler, and registers the service as `iface.TenantProvider` in the registry.
- **Start / Stop / HealthCheck**: inherit from `BaseModule` (no-op).
- **Seeding**: none. Orgs are created by users via the setup wizard or `POST /v1/orgs`.

## HTTP endpoints

Three route groups, each with a different gate:

### Global — `RequireGlobal()`

| Method | Path | Purpose |
|---|---|---|
| GET | `/v1/orgs` | List the orgs the caller is a member of |
| POST | `/v1/orgs` | Create a new org — caller becomes owner + administrator |
| POST | `/v1/orgs/accept-invite` | Redeem an invite token and join the target org |

### Per-org — read (`RequirePermission("tenant.org.read")`)

| Method | Path | Purpose |
|---|---|---|
| GET | `/v1/orgs/{orgId}` | Get org by id |
| GET | `/v1/orgs/{orgId}/members` | List members |

### Per-org — mutation (`RequirePermission("tenant.org.read")` + `RequireMFA()`)

Block B gates every tenant mutation behind an MFA step-up. Each can transfer ownership-adjacent data, change plan entitlements, or destroy the org — a pwd-only token fails with 401 `mfa_required` and the client steps up via `/v1/auth/mfa/verify` before retrying.

| Method | Path | Purpose |
|---|---|---|
| PATCH | `/v1/orgs/{orgId}` | Update org name, slug, or settings |
| DELETE | `/v1/orgs/{orgId}` | Soft-delete (owner only) |
| PATCH | `/v1/orgs/{orgId}/plan` | Change plan and recompute features |
| DELETE | `/v1/orgs/{orgId}/members/{userUUID}` | Remove a member |
| POST | `/v1/orgs/{orgId}/invites` | Create an invite token |

### Platform admin — `RequireSystemPermission("system.tenants.admin")`

Gated globally by a system permission, not by per-org membership, so platform operators can manage every tenant without having to join each one. Powers the frontend `/admin/tenants` page.

| Method | Path | Purpose |
|---|---|---|
| GET | `/v1/admin/orgs` | List every tenant. `?includeDeleted=true` to include soft-deleted rows. Response includes `memberCount` resolved via a single `$group` aggregation. |
| GET | `/v1/admin/orgs/{orgId}` | Get any tenant |
| PATCH | `/v1/admin/orgs/{orgId}` | Update any tenant (name, slug, settings) |
| DELETE | `/v1/admin/orgs/{orgId}` | Soft-delete any tenant — bypasses the owner-only check |
| PATCH | `/v1/admin/orgs/{orgId}/plan` | Change any tenant's plan + features |
| GET | `/v1/admin/orgs/{orgId}/members` | List members |
| DELETE | `/v1/admin/orgs/{orgId}/members/{userUUID}` | Remove a member |
| GET | `/v1/admin/orgs/{orgId}/invites` | List invites. Defaults to pending-only; pass `?includeAccepted=true` to also see accepted rows. Raw tokens are scrubbed. |
| POST | `/v1/admin/orgs/{orgId}/invites` | Create an invite. Raw token returned once, see Key invariants. |
| DELETE | `/v1/admin/orgs/{orgId}/invites/{inviteId}` | Revoke a pending invite |

Route registration and handler implementations in `handlers/handler.go`. The permission itself (`system.tenants.admin`) is declared with `System: true` in `module.go::Permissions()`, so `super_admin` / `administrator` / `developer` inherit it automatically via authz's system-role seeding — no bindings required.

## Navigation

`module.go::NavItems()` contributes a single entry:

```
Group:   System Administration
Name:    Tenant Management
Path:    /admin/tenants
MinRole: administrator
```

The frontend route is registered at `frontend/src/pages/admin/tenants/` and wired in `frontend/src/routes/index.tsx` behind `ProtectedRoute` with `super_admin | administrator | developer`.

## Service contract

`iface.TenantProvider` (`shared/iface/interfaces.go`):

```go
GetTenant(ctx, tenantUUID) (*Tenant, error)
ListUserMemberships(ctx, userUUID) ([]TenantMembership, error)
IsMember(ctx, userUUID, tenantUUID) (bool, error)
HasCapability(ctx, tenantUUID, capabilityID) (bool, error)
GrantCapability(ctx, GrantCapabilityInput) error
RevokeCapability(ctx, tenantUUID, capabilityID) error
ListCapabilityIDs(ctx, tenantUUID) ([]string, error)
```

`Tenant` exposes `UUID, Kind, ParentTenantUUID, Status, Name, Slug, Plan`. `TenantMembership` exposes `TenantUUID, TenantName, TenantSlug, TenantKind, Roles, IsOwner`. Both are intentionally trimmed — anything richer lives in `tenant/models` and is only reachable via the concrete service, not through the provider interface.

Typical consumers:
- **auth** — `ListUserMemberships` during JWT issuance so memberships are embedded in the access token's `memberships` claim (frontend reads them to build the org switcher without an extra round trip).
- **middleware** — `IsMember` on every protected request that resolves an `X-Org-ID` header; `HasCapability` on routes gated by `RequireCapability(capID)` (returns 402 on a miss).
- **authz (Cedar shadow evaluator)** — `ListCapabilityIDs` populates `cedar.Principal.Capabilities` so the `capability_grants.cedar` forbid-unless-entitled rule can reason about entitlements.
- **subscriptions (entitlement syncer)** — `GrantCapability` / `RevokeCapability` on every subscription lifecycle transition so paid capabilities appear on the tenant's projection.
- **tenant handlers themselves** — use the concrete service for richer operations that don't fit on the interface.

## Key invariants

- **Plan names** (`models/tenant.go`): `free` (default), `pro`, `enterprise`. Plan is an **informational label only** — admin UI display, reporting. It does **not** drive feature access.
- **Capability entitlements drive access, not plan.** `RequireCapability(capID)` consults `tenant_entitlements` via `HasCapability`. The `subscriptions` module populates entitlements through the entitlement syncer on every lifecycle transition.
- **Internal-tenant bypass.** `HasCapability` short-circuits to `true` for tenants with `Kind == internal`. Internal (operator) tenants host the platform and don't consume via subscriptions — the capability gate is the external-client monetization seam. External tenants always consult the projection.
- **Owner is auto-enrolled as administrator** on tenant creation (`services/service.go::CreateTenant`) — the first membership is inserted with `Roles: ["administrator"]` and `IsOwner: true`. The `administrator` string must match an authz role name.
- **Slug uniqueness + auto-generation**. Unique sparse index on `slug`. `CreateOrg` falls back to `slugify(input.Name)` when no slug is provided (`services/service.go:88-94`); the slugifier is in `services/service.go:238-255`.
- **Soft delete only.** `DeleteOrg` sets a `deletedAt` timestamp. Every read query filters these out at the Mongo layer unless `includeDeleted` is explicitly requested (admin list only). The plain `DeleteOrg` has no owner-check today — the platform-admin path reuses it directly.
- **Invite tokens are stored as SHA-256 hashes, never plaintext.** `generateInviteToken` (services/service.go) produces 32 bytes of randomness → base64url → SHA-256 hex. The raw token is populated on `models.Invite.Token` (a `bson:"-"` transient field) and returned once on the create response; the database only holds `tokenHash`. `AcceptInvite` hashes the supplied token and looks up by `tokenHash`. This mirrors the email-token pattern in the auth module. Expired invites are auto-reaped by the `expiresAt` TTL index (`expireAfterSeconds=0`).
- **`Membership.Roles` is a denormalization.** It's an array of authz role names. When authz bindings change, the tenant service is **not automatically kept in sync** — there's no event hook yet. If you see a divergence between authz bindings and the tenant membership's `Roles`, the authz bindings are the source of truth.

## What this module does NOT do

- Role bindings, permission evaluation, cascade rules → **authz**
- User identity, profile, password, email verification → **user** / **auth**
- Billing/subscription state (Stripe, invoices) → belongs to a future billing-addon; this module only stores the plan *name*
- Usage metering, quotas, rate-based enforcement → not implemented; plan features are boolean flags only
- Org-level preferences beyond settings blob → settings is a free-form map today, not typed

## Rules

- **Platform-admin delete bypasses ownership.** The `/v1/admin/orgs/{orgId}` DELETE route is gated by `system.tenants.admin` (system roles only). The plain per-org DELETE route at `/v1/orgs/{orgId}` has no owner check today; adding one is pending the "transfer ownership" flow.
- **Never store a plaintext invite token.** Enforced: `models.Invite.Token` is `bson:"-"`; only `TokenHash` is persisted. If you add a second invite-like flow (e.g. a public "share link"), use the same pattern and never add a plaintext field to the document.
- **When you add a new paid capability**, declare it in the owning module's `Capabilities()` method and gate the relevant routes with `RequireCapability(capID)`. Wire a subscription tier that grants it via `PricingTier.Capabilities` so the entitlement syncer hands it out on checkout. Plan names are informational — do not scatter plan-name logic through the codebase.
- **If you add a new permission**, put it in `module.go::Permissions()` and gate the relevant handler in `module.go::RegisterRoutes` — don't scatter `RequirePermission` calls across the handlers package, keep them at the route-group boundary.
- **Do not keep `Membership.Roles` in sync with authz bindings by hand.** Long term this denormalization needs an event-based sync; until then, treat `Membership.Roles` as a hint the JWT can read quickly, and `authz.GetEffectivePermissions` as the source of truth.

## Org-scoping invariants

The system-wide invariants that govern tenant isolation live in [`../authz/CLAUDE.md`](../authz/CLAUDE.md#org-scoping-invariants-system-wide). Three of them are directly owned by this module:

- **Invariant #1** — every addon `collection.Find/Update/Delete/Aggregate` must derive its filter from `shared/tenantrepo.Scope*`. Enforced at dev time by panic in the helper; CI-enforced in Phase 0 by the `tools/tenantscope` analyzer.
- **Invariant #2** — `X-Org-ID` header must match a membership in the JWT. Already enforced in `shared/middleware/auth.go::resolveCurrentOrg`.
- **Invariant #6** — `tenant_orgs.ownerUserUUID` is immutable without a two-step owner-transfer flow. **Not yet enforced** — Phase 2 work. Until then, platform admins changing `ownerUserUUID` directly is a known gap.

## Related

- [`../user/CLAUDE.md`](../user/CLAUDE.md) — hard dep; user accounts must exist before memberships
- [`../authz/CLAUDE.md`](../authz/CLAUDE.md) — provides the role-name vocabulary this module stores in `Membership.Roles`; owns the **Org-scoping invariants** table
- [`../auth/CLAUDE.md`](../auth/CLAUDE.md) — embeds memberships in JWT claims via `TenantProvider.ListUserMemberships`
- [`../../shared/iface/interfaces.go:175-196`](../../shared/iface/interfaces.go) — `TenantProvider` interface definition
- [`../../shared/tenantrepo/`](../../shared/tenantrepo) — helpers for other modules that need to scope queries by org
