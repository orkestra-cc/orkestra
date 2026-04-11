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
| `tenant_org_invites` | `token` unique, `orgId`, `expiresAt` | `expiresAt` index (Mongo will reap when you add a TTL; today it's a plain index — see Rules) |

Collection name constants live in `repository/repository.go` as `CollOrgs`, `CollMemberships`, `CollInvites`.

## Dependencies

- **Modules**: `user` (`module.go:29`) — so user profiles exist before memberships reference them.
- **Required services**: none (the service does not currently look up users via the provider, it trusts the caller's auth context).
- **Optional services**: none.
- **Provides**: `ServiceTenantProvider` → `iface.TenantProvider` (`module.go:31-33`).
- **Permissions contributed** (`module.go:58-68`):

| Key | Purpose |
|---|---|
| `tenant.org.read` | Read org details |
| `tenant.org.update` | Update org name, slug, settings |
| `tenant.org.delete` | Soft-delete the org |
| `tenant.plan.update` | Change plan and features |
| `tenant.member.read` | List org members |
| `tenant.member.invite` | Invite new members |
| `tenant.member.remove` | Remove members from the org |

## Lifecycle

- **Init** (`module.go:70-76`): constructs the repository, builds the service, creates the handler, and registers the service as `iface.TenantProvider` in the registry.
- **Start / Stop / HealthCheck**: inherit from `BaseModule` (no-op).
- **Seeding**: none. Orgs are created by users via the setup wizard or `POST /v1/orgs`.

## HTTP endpoints

Two route groups, each with a different gate:

### Global — `RequireGlobal()` (`module.go:81-85`)

| Method | Path | Purpose |
|---|---|---|
| GET | `/v1/orgs` | List the orgs the caller is a member of |
| POST | `/v1/orgs` | Create a new org — caller becomes owner + administrator |
| POST | `/v1/orgs/accept-invite` | Redeem an invite token and join the target org |

### Per-org — `RequirePermission("tenant.org.read")` (`module.go:88-94`)

These read the target org from the `{orgId}` path and check that the caller's bindings in that org include `tenant.org.read`. Further permissions (e.g. `tenant.org.update`) are enforced inside the handler/service layer when needed.

| Method | Path | Purpose |
|---|---|---|
| GET | `/v1/orgs/{orgId}` | Get org by id |
| PATCH | `/v1/orgs/{orgId}` | Update org name, slug, or settings |
| DELETE | `/v1/orgs/{orgId}` | Soft-delete (owner only) |
| PATCH | `/v1/orgs/{orgId}/plan` | Change plan and recompute features |
| GET | `/v1/orgs/{orgId}/members` | List members |
| DELETE | `/v1/orgs/{orgId}/members/{userUUID}` | Remove a member |
| POST | `/v1/orgs/{orgId}/invites` | Create an invite token |

Route registration in `handlers/handler.go:79-165`.

## Service contract

`iface.TenantProvider` (`shared/iface/interfaces.go:191-196`):

```go
GetOrg(ctx, orgUUID) (*Org, error)
ListUserMemberships(ctx, userUUID) ([]Membership, error)
IsMember(ctx, userUUID, orgUUID) (bool, error)
HasEntitlement(ctx, orgUUID, feature string) (bool, error)
```

`Org` exposes `UUID, Name, Slug, Plan, Features`. `Membership` exposes `OrgUUID, OrgName, OrgSlug, Roles, IsOwner`. Both are intentionally trimmed — anything richer lives in `tenant/models` and is only reachable via the concrete service, not through the provider interface.

Typical consumers:
- **auth** — `ListUserMemberships` during JWT issuance so memberships are embedded in the access token's `memberships` claim (frontend reads them to build the org switcher without an extra round trip).
- **middleware** — `IsMember` on every protected request that resolves an `X-Org-ID` header; `HasEntitlement` on routes gated by a plan feature.
- **tenant handlers themselves** — use the concrete service for richer operations that don't fit on the interface.

## Key invariants

- **Plan names** (`models/org.go:12-14`): `free` (default), `pro`, `enterprise`. Custom plan strings are allowed and fall through to the `free` default feature list unless the caller supplies `Features` explicitly.
- **Default features per plan** (`services/service.go:227-236`):
  - `enterprise` → `["*"]` (wildcard, `HasFeature` short-circuits)
  - `pro` → `["billing", "documents", "company", "sales", "agents"]`
  - default (`free` or unknown) → `["billing", "documents"]`
- **Owner is auto-enrolled as administrator** on org creation (`services/service.go:115-122`) — the first membership is inserted with `Roles: ["administrator"]` and `IsOwner: true`. The `administrator` string must match an authz role name.
- **Slug uniqueness + auto-generation**. Unique sparse index on `slug`. `CreateOrg` falls back to `slugify(input.Name)` when no slug is provided (`services/service.go:88-94`); the slugifier is in `services/service.go:238-255`.
- **Soft delete only.** `DeleteOrg` sets a `deletedAt` timestamp. Every read query should filter these out — the repository does this at the Mongo layer. Note: **owner-only** is enforced in the service, not via a permission check.
- **Invite tokens are SHA256-hashed base32** (`services/service.go:257-261`). The raw token is returned to the inviter exactly once via the API response; only the hash lives in the DB. Invites have an `expiresAt` index but **not** a TTL — expired invites hang around until you explicitly reap them. Worth fixing, but don't assume it's a TTL today.
- **`Membership.Roles` is a denormalization.** It's an array of authz role names. When authz bindings change, the tenant service is **not automatically kept in sync** — there's no event hook yet. If you see a divergence between authz bindings and the tenant membership's `Roles`, the authz bindings are the source of truth.
- **`HasEntitlement` treats `"*"` as "yes"** (`models/org.go::HasFeature`). This is how enterprise plans bypass the per-feature gate.

## What this module does NOT do

- Role bindings, permission evaluation, cascade rules → **authz**
- User identity, profile, password, email verification → **user** / **auth**
- Billing/subscription state (Stripe, invoices) → belongs to a future billing-addon; this module only stores the plan *name*
- Usage metering, quotas, rate-based enforcement → not implemented; plan features are boolean flags only
- Org-level preferences beyond settings blob → settings is a free-form map today, not typed

## Rules

- **Owner check is owner-only, permission-independent.** `DeleteOrg` checks `IsOwner`, not a permission. Do not move this to a permission check without also adding a "transfer ownership" flow.
- **Never store a plaintext invite token.** Always hash on write and compare hashes on accept.
- **When you add a new feature flag**, update `defaultFeaturesForPlan` in `services/service.go:227-236` and document the string in a Plans/Features section above. Frontend code reads these strings.
- **If you add a new permission**, put it in `module.go::Permissions()` and gate the relevant handler in `module.go::RegisterRoutes` — don't scatter `RequirePermission` calls across the handlers package, keep them at the route-group boundary.
- **Do not keep `Membership.Roles` in sync with authz bindings by hand.** Long term this denormalization needs an event-based sync; until then, treat `Membership.Roles` as a hint the JWT can read quickly, and `authz.GetEffectivePermissions` as the source of truth.

## Related

- [`../user/CLAUDE.md`](../user/CLAUDE.md) — hard dep; user accounts must exist before memberships
- [`../authz/CLAUDE.md`](../authz/CLAUDE.md) — provides the role-name vocabulary this module stores in `Membership.Roles`
- [`../auth/CLAUDE.md`](../auth/CLAUDE.md) — embeds memberships in JWT claims via `TenantProvider.ListUserMemberships`
- [`../../shared/iface/interfaces.go:175-196`](../../shared/iface/interfaces.go) — `TenantProvider` interface definition
- [`../../shared/tenantrepo/`](../../shared/tenantrepo) — helpers for other modules that need to scope queries by org
