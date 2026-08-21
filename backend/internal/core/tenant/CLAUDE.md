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
| `module.go` | Module registration, collections, permissions, `ConfigSchema()` (provisioning policy), service wire-up |
| `handlers/handler.go` | HTTP handlers for org and membership CRUD + invites; provisioning manual-gate (`enforceManualGate`/`callerIsTenantAdmin`) + provisioning-policy read |
| `services/service.go` | Org lifecycle, membership sync, invite token issuance, provisioning policy (`ProvisioningMode`/`CountActiveByKind`/`ErrProvisioningLocked`), `iface.TenantProvider` implementation |
| `services/billing.go` | Unified-clients (Phase 1) — `SetItalianBillable`, `SetBillingIdentity`, `ResolveBillingParty`, `EnsureTenantForUser` (honours external provisioning mode), `iface.BillingTenantProvider` implementation |
| `services/entitlements.go` | Capability-entitlement projection (`iface.AccessProvider`) |
| `repository/repository.go` | MongoDB CRUD for orgs, memberships, invites; personal-tenant predicate lookup; `CountActiveByKind` (single-mode invariant) |
| `models/tenant.go` | `Tenant`, `TenantMembership`, `TenantInvite`, `TenantAncestor`, `FatturaPAProfile` structs + `TenantKind`/`TenantStatus`/plan/`ProvisioningMode*` constants |
| `models/entitlement.go` | Capability-entitlement projection row |

## MongoDB collections

Declared in `module.go::Collections()` (`module.go:77`).

| Collection | Indexes | TTL |
|---|---|---|
| `tenants` | `uuid` unique, `slug` unique sparse, `ownerUserUUID`, `kind`, `status`, `parentTenantUUID` sparse | — |
| `tenant_memberships` | compound `(userUUID, tenantId)` unique, `tenantId` | — |
| `tenant_invites` | `tokenHash` unique sparse, `tenantId`, `expiresAt` TTL(ExpireAt) | `expiresAt` is a TTL index with `expireAfterSeconds=0` so Mongo reaps the doc the moment the timestamp passes. |
| `tenant_ancestors` | compound `(descendantUUID, ancestorUUID)` unique, `ancestorUUID` | — — closure table for the tenant hierarchy (ADR-0001) |
| `tenant_entitlements` | `uuid` unique sparse, compound `(tenantUUID, capabilityId)`, `capabilityId`, `expiresAt` sparse | — — capability projection (at most one active row per capability, enforced in the service) |

Collection name constants live in `repository/repository.go` (`CollTenants`, `CollMemberships`, `CollInvites`, `CollAncestors`) and `repository/entitlements.go` (`CollEntitlements`).

## Dependencies

- **Modules**: `user` (`module.go::Dependencies`) — so user profiles exist before memberships reference them.
- **Required services**: none (the service does not currently look up users via the provider, it trusts the caller's auth context).
- **Optional services**: none. At request time the handler resolves `iface.AuthzProvider` lazily from the registry to enforce the provisioning `manual` gate (see [Provisioning policy](#provisioning-policy-admin-managed)).
- **Provides** (`module.go::ProvidedServices`): `ServiceTenantProvider` → `iface.TenantProvider`, `ServiceAccessProvider` → `iface.AccessProvider`, `ServiceTenantService` (concrete), `ServiceBillingTenantProvider` → `iface.BillingTenantProvider`.
- **Config** (`ConfigSchema()`): `provisioning.internal.mode` + `provisioning.external.mode` — admin-managed tenant-creation policy. See [Provisioning policy](#provisioning-policy-admin-managed).
- **Permissions contributed** (`module.go::Permissions`):

| Key | System? | Purpose |
|---|---|---|
| `tenant.read` | no | Read tenant details |
| `tenant.update` | no | Update tenant name, slug, settings (also gates identity IdP + SCIM admin CRUD) |
| `tenant.delete` | no | Archive the tenant |
| `tenant.plan.update` | no | Change plan and features |
| `tenant.member.read` | no | List tenant members |
| `tenant.member.invite` | no | Invite new members |
| `tenant.member.remove` | no | Remove members from the tenant |
| `system.tenants.admin` | **yes** | Administer every tenant platform-wide (powers `/v1/admin/tenants/*`) |

## Lifecycle

- **Init**: constructs the repository, builds the service, creates the handler, registers the service as `iface.TenantProvider` in the registry, and wires the `ProvisioningModeResolver` (a closure over `deps.ConfigService` reading the `provisioning.{internal,external}.mode` keys live).
- **Start / Stop / HealthCheck**: inherit from `BaseModule` (no-op).
- **Seeding**: the first-install setup flow creates **no** tenant. The initial admin is a super_admin (system role, tenant-independent); tenants are created deliberately afterward — the setup wizard's `OrgStep` is the first, skippable creation point (admin-only by default, since both provisioning modes default to `manual`). A fresh install may run with zero internal tenants.
- **GDPR/DSR** (`services/pii_producer.go`): registers an `iface.PIIProducer` (subject `"tenant"`) on `ServicePIIProducerRegistry` at Init. The subject's personal data here is their **tenant memberships** (which orgs, what roles) — the orgs/tenants themselves are not the subject's data and are left intact. Export returns the membership rows; purge deletes them (`tenant_memberships`) under **both** erase modes, since a membership row IS the user→org linkage with no anonymizable residue. Consumed by the [compliance module](../compliance/CLAUDE.md)'s DSR pipeline (ADR-0009).

## Provisioning policy (admin-managed)

`module.go::ConfigSchema()` exposes two enum config keys that govern **who may create tenants**, per tier. Read at request time by the service's `ProvisioningModeResolver` (a closure over `ModuleConfigService`, 30s Redis cache) — edits at `/admin/modules/tenant` take effect on the next creation with **no restart**.

| Key | Tier | Values | Default | EnvVar (first-boot seed) |
|---|---|---|---|---|
| `provisioning.internal.mode` | internal | `open` · `manual` · `single` | **`manual`** | `TENANT_PROVISIONING_INTERNAL_MODE` |
| `provisioning.external.mode` | external | `open` · `manual` | **`manual`** | `TENANT_PROVISIONING_EXTERNAL_MODE` |

- **open** — any authenticated user may create.
- **manual** (default both tiers) — only holders of `system.tenants.admin` may create.
- **single** — at most one active (non-deleted) tenant of that tier may exist (internal-only intent: lock the platform to a single internal org).

**Default posture (both `manual`):** a fresh install accepts no self-service tenant creation. The first internal tenant is created deliberately from the setup wizard's `OrgStep` or the admin UI — a fresh install may have zero internal tenants; afterwards an operator creates further internal tenants deliberately. External clients are **never auto-provisioned** and **cannot self-create** a tenant — only a platform admin creates a client tenant and assigns it to a Tier-2 user.

**Two-layer enforcement** (so every creation path is covered without scattering checks):

1. **`single` is a data invariant in the service.** `Service.CreateTenant` (`services/service.go`) counts active tenants of the kind via `repository.CountActiveByKind` and returns the sentinel `services.ErrProvisioningLocked` when one already exists. This covers **all** paths — `POST /v1/tenants`, divisions, and lazy provisioning — automatically. The first tenant on a fresh install counts 0 and passes.
2. **`manual` is a permission gate in the handlers.** `createTenant` / `createDivision` call `Handler.enforceManualGate`, which resolves `iface.AuthzProvider` from the registry and checks `system.tenants.admin` (empty org = system grant). Non-admins get 403; the admin `CreateTenantModal` hits the same `POST /v1/tenants` and passes. The admin division route already requires the permission at the route group, so the shared handler body is a no-op gate for it.
3. **Lazy provisioning honours `external.mode`.** `EnsureTenantForUser` (`services/billing.go`) returns an existing personal tenant unchanged but **refuses to auto-create** one (returns `ErrProvisioningLocked`) when `external.mode != open` — which is the default. A Tier-2 caller with no admin-assigned tenant therefore hits `resolveCallerTenant`, which maps the sentinel to **409** ("an administrator must create and assign one") rather than silently minting a tenant.

Handlers map `ErrProvisioningLocked` → 409. The read-only `GET /v1/admin/tenants/provisioning-policy` reports both modes + active counts and backs the policy card on the tenant management pages (the modes themselves are edited at `/admin/modules/tenant`).

`ConfigGroups()` splits the two fields onto the full-page rail along the tier boundary: `provisioning.internal` ("Internal provisioning (Tier-1)") holds `provisioning.internal.mode`, `provisioning.external` ("External provisioning (Tier-2)") holds `provisioning.external.mode` — one field per group, the minimum that promotes `/admin/modules/tenant` off the flat-form degradation path. Dropping `ConfigGroups()` entirely reverts the page to the flat single-card form with no other change required — `ConfigSchema()`'s `Group` tags become inert.

## HTTP endpoints

Three route groups, each with a different gate:

### Global — `RequireGlobal()`

| Method | Path | Purpose |
|---|---|---|
| GET | `/v1/tenants` | List the tenants the caller is a member of |
| POST | `/v1/tenants` | Create a new tenant — caller becomes owner (`org_owner`). Subject to the provisioning policy (see above): in `manual` mode a non-admin caller gets 403; in `single` mode a second tenant of that tier gets 409. |
| POST | `/v1/tenants/accept-invite` | Redeem an invite token and join the target tenant |

### Per-tenant — read (`RequirePermission("tenant.read")`)

| Method | Path | Purpose |
|---|---|---|
| GET | `/v1/tenants/{tenantId}` | Get tenant by id |
| GET | `/v1/tenants/{tenantId}/members` | List members |
| GET | `/v1/tenants/{tenantId}/divisions` | List direct children (depth=1) |

### Per-tenant — mutation (`RequirePermission("tenant.read")` + `RequireMFA()`)

Block B gates every tenant mutation behind an MFA step-up. Each can transfer ownership-adjacent data, change plan entitlements, or destroy the tenant — a pwd-only token fails with 401 `step_up_required` and the client steps up via `/v1/auth/mfa/verify` before retrying.

| Method | Path | Purpose |
|---|---|---|
| PATCH | `/v1/tenants/{tenantId}` | Update tenant name, slug, or settings |
| DELETE | `/v1/tenants/{tenantId}` | Soft-delete (owner only) |
| PATCH | `/v1/tenants/{tenantId}/plan` | Change plan and recompute features |
| DELETE | `/v1/tenants/{tenantId}/members/{userUUID}` | Remove a member |
| POST | `/v1/tenants/{tenantId}/invites` | Create an invite token |
| POST | `/v1/tenants/{tenantId}/divisions` | Create a division under this external tenant (also honours the external provisioning gate) |

### Platform admin — `RequireSystemPermission("system.tenants.admin")`

Gated globally by a system permission, not by per-org membership, so platform operators can manage every tenant without having to join each one. Powers the frontend `/admin/internal/tenants` (Tier-1) and `/admin/clients` (Tier-2) pages.

| Method | Path | Purpose |
|---|---|---|
| GET | `/v1/admin/tenants` | List every tenant. Query params: `?kind=internal\|external`, `?parentTenantUUID=<uuid>`, `?rootsOnly=true`, `?includeDeleted=true`, `?q=<text>`, `?includeDeletedUsers=true`. Response includes `memberCount` from a single `$group` aggregation. When `q` is set the handler routes to `repository.SearchTenantsByQ`, which $lookup-joins `tenant_memberships` → tier-appropriate user collection (`operator_users` for internal, `client_users` for external) and matches `q` (case-insensitive substring) against tenant `name`/`slug` plus member `email`/`fullName`/`username`. Each matching row includes a `matchedMembers` array (≤ `MaxMatchedMembersPerTenant=5`) so the frontend can render "matched: alice@x" chips. `includeDeletedUsers` opts soft-deleted users into the member-side join (default: live users only). |
| GET | `/v1/admin/tenants/{tenantId}` | Get any tenant |
| PATCH | `/v1/admin/tenants/{tenantId}` | Update any tenant (name, slug, settings) |
| DELETE | `/v1/admin/tenants/{tenantId}` | Soft-delete any tenant — bypasses the owner-only check |
| POST | `/v1/admin/tenants/{tenantId}/purge` | Crypto-shred a tenant (irreversible; see purge handler docs) |
| PATCH | `/v1/admin/tenants/{tenantId}/plan` | Change any tenant's plan + features |
| GET | `/v1/admin/tenants/{tenantId}/members` | List members |
| DELETE | `/v1/admin/tenants/{tenantId}/members/{userUUID}` | Remove a member |
| GET | `/v1/admin/tenants/{tenantId}/invites` | List invites. Defaults to pending-only; pass `?includeAccepted=true` to also see accepted rows. Raw tokens are scrubbed. |
| POST | `/v1/admin/tenants/{tenantId}/invites` | Create an invite. Raw token returned once, see Key invariants. |
| DELETE | `/v1/admin/tenants/{tenantId}/invites/{inviteId}` | Revoke a pending invite |
| GET | `/v1/admin/tenants/{tenantId}/divisions` | List direct children (depth=1) of an external tenant |
| POST | `/v1/admin/tenants/{tenantId}/divisions` | Create a division (Kind=external, ParentTenantUUID=this). Refuses internal parents. |
| PATCH | `/v1/admin/clients/{tenantId}/billing-identity` | Unified-clients — sets `IsCompany`, `LegalName`, VAT/fiscal codes, billing address, and the FatturaPA routing sub-document on a Tier-2 tenant. All body fields optional; nil leaves the existing value. The data this endpoint writes is what `iface.BillingTenantProvider.ResolveBillingParty` reads at invoice-send time (replaces the deleted `billing.Customer` row). |
| POST | `/v1/admin/clients/{tenantId}/italian-billable` | Unified-clients Phase 1 — flips `Tenant.IsItalianBillable`. Enabling requires a FatturaPA profile carrying `CodiceDestinatario` or `PECDestinatario` (422 otherwise); disabling is unconditional. Send-time validation enforces the same invariant a second time, so the toggle on its own is not load-bearing. |
| GET | `/v1/admin/tenants/provisioning-policy` | Read-only per-tier provisioning policy (`open`/`manual`/`single`) + active-tenant counts. Backs the policy card on the tenant pages; the modes are edited at `/admin/modules/tenant`. See [Provisioning policy](#provisioning-policy-admin-managed). |

### Tier-2 self-service — Client surface, `RequireGlobal()`

Mounted on `ri.Client.ProtectedRouter` so frontend-client tokens (`aud=client`) can manage their own tenant's billing identity without an admin sweep. Each handler resolves the caller's personal tenant via `EnsureTenantForUser` (lazy provisioning), then delegates to the same `SetBillingIdentity` / `SetItalianBillable` service methods the admin endpoints call. The Tier-2 caller never touches another tenant's row — the personal tenant is keyed by the authenticated `userUUID`.

| Method | Path | Purpose |
|---|---|---|
| GET | `/v1/me/billing-identity` | Read the caller's billing identity (lazy-provisions the personal tenant on first call). Returns `BillingIdentityDTO` (focused projection — does not leak operator-only fields). |
| PATCH | `/v1/me/billing-identity` | Update the caller's billing identity. All fields optional; nil leaves the existing value. FatturaPA is wholesale-replaced when present. |
| POST | `/v1/me/italian-billable` | Toggle `IsItalianBillable` on the caller's personal tenant. Same FatturaPA-routing precondition as the admin endpoint. |

Route registration and handler implementations in `handlers/handler.go`. The permission itself (`system.tenants.admin`) is declared with `System: true` in `module.go::Permissions()`, so `super_admin` / `administrator` / `developer` inherit it automatically via authz's system-role seeding — no bindings required.

## Navigation

`module.go::NavItems()` contributes **two** sidebar entries (ADR-0001 Phase 3 split — operator side vs client side stay distinct rows, but both live under the Administration realm). Both carry `Tier="internal"` so external Tier-2 callers never see them even when the menu is rendered for an admin user. They are declared consecutively (Internal first), so External Tenants renders directly below Internal Tenants in the same section:

```
Realm:   platform   (renders under "Administration")
Tier:    internal
Name:    Internal Tenants
Path:    /admin/internal/tenants
MinRole: administrator

Realm:   platform   (renders under "Administration")
Tier:    internal
Name:    External Tenants
Path:    /admin/clients
MinRole: administrator
```

Frontend routes: `/admin/internal/tenants` (+ `/:tenantId`) renders `frontend-admin/src/pages/admin/internal-tenants/`, `/admin/clients` (+ `/:clientId`) renders `frontend-admin/src/pages/admin/clients/`. Both are gated by `ProtectedRoute` with `super_admin | administrator | developer`. The legacy `/admin/tenants` route 301-redirects to `/admin/clients` since most historical traffic there was client-leaning.

## Service contract

`iface.TenantProvider` (`pkg/sdk/iface/interfaces.go`):

```go
GetTenant(ctx, tenantUUID) (*Tenant, error)
ListUserMemberships(ctx, userUUID) ([]TenantMembership, error)
IsMember(ctx, userUUID, tenantUUID) (bool, error)
ActivateTenant(ctx, tenantUUID) error
SetTenantStripeCustomerID(ctx, tenantUUID, stripeCustomerID) error
EnsureTenantForUser(ctx, userUUID) (*Tenant, error)
```

`iface.AccessProvider` (same file) — capability surface keyed by tenant UUID. The same concrete `*tenant/services.Service` implements both interfaces; registered separately under `module.ServiceAccessProvider` so consumers ask for the capability surface without the tenant CRUD surface:

```go
HasCapability(ctx, tenantUUID, capabilityID) (bool, error)
GrantCapability(ctx, GrantCapabilityInput) error            // GrantCapabilityInput.TenantUUID carries the scope
RevokeCapability(ctx, tenantUUID, capabilityID) error
ListCapabilityIDs(ctx, tenantUUID) ([]string, error)
```

The `tenant_entitlements` collection is keyed by `(tenantUUID, capabilityID)`. Self-registered clients ride on a personal Tenant aggregate (created lazily by `EnsureTenantForUser`) so every billable principal looks like a tenant.

`Tenant` exposes `UUID, Kind, ParentTenantUUID, Status, Name, Slug, Plan`. `TenantMembership` exposes `TenantUUID, TenantName, TenantSlug, TenantKind, Roles, IsOwner`. Both are intentionally trimmed — anything richer lives in `tenant/models` and is only reachable via the concrete service, not through the provider interface.

Typical consumers:
- **auth** — `ListUserMemberships` during JWT issuance so memberships are embedded in the access token's `memberships` claim (frontend reads them to build the org switcher without an extra round trip).
- **middleware** — `IsMember` on every protected request that resolves an `X-Tenant-ID` header; `RequireCapability` consumes `AccessProvider.HasCapability` against the request's resolved tenant UUID (`X-Tenant-ID`) and returns 402 on a miss. Capability gating without a tenant context is undefined post-Unified-Client-Aggregate.
- **authz (Cedar shadow evaluator)** — `AccessProvider.ListCapabilityIDs(tenantUUID)` populates `cedar.Principal.Capabilities` so the `capability_grants.cedar` forbid-unless-entitled rule can reason about entitlements.
- **subscriptions (entitlement syncer)** — `AccessProvider.GrantCapability/RevokeCapability` on every subscription lifecycle transition; the syncer hands `sub.TenantUUID` directly.
- **client signup (`/v1/auth/client/register`)** — creates a user only; the personal tenant is materialized lazily by `EnsureTenantForUser` on the first tenant-requiring action.
- **tenant handlers themselves** — use the concrete service for richer operations that don't fit on the interface.

## Key invariants

- **Tenant creation obeys the provisioning policy.** Every creation path funnels through `Service.CreateTenant`, which enforces the `single` count invariant as a data rule (so divisions and lazy provisioning are covered, not just `POST /v1/tenants`). The `manual` permission gate lives at the handler boundary. **Both tiers default to `manual`** (admin-only); a fresh install creates no tenant; the first internal tenant is created deliberately (setup wizard `OrgStep` or admin UI). Full semantics in [Provisioning policy](#provisioning-policy-admin-managed).
- **Plan names** (`models/tenant.go`): `free` (default), `pro`, `enterprise`. Plan is an **informational label only** — admin UI display, reporting. It does **not** drive feature access.
- **Capability entitlements drive access, not plan.** `RequireCapability(capID)` consults the `tenant_entitlements` projection via `AccessProvider.HasCapability`. The `subscriptions` module populates entitlements through the entitlement syncer on every lifecycle transition. Entitlement rows are keyed by polymorphic `(ownerKind, ownerUUID, capabilityID)` so a self-registered user holds capabilities directly without a tenant.
- **Internal-tenant bypass.** `HasCapability` short-circuits to `true` for `Owner{Kind:"tenant"}` whose tenant is `Kind == internal`. User-owned entitlements always consult the projection (no operator user is granted capabilities by tier). Internal (operator) tenants host the platform and don't consume via subscriptions — the capability gate is the external-client monetization seam.
- **Owner is auto-enrolled as `org_owner`** on tenant creation (`services/service.go::CreateTenant`) — the first membership is inserted with `Roles: ["org_owner"]` and `IsOwner: true`. The post-membership `OwnerRoleBinder` hook (wired by the authz module) creates a tenant-scoped authz binding so the owner has actual permissions. The org_owner role's permission set excludes everything tagged `System=true`, so the owner cannot manage modules, other tenants, or platform users via this binding. Pre-2026-04-24 tenants whose Roles still says `["administrator"]` are not auto-migrated — operators need a one-shot script that updates those memberships and creates the missing `org_owner` binding.
- **Slug uniqueness + auto-generation**. Unique sparse index on `slug`. `CreateTenant` falls back to `slugify(input.Name)` when no slug is provided (`services/service.go::CreateTenant`); the slugifier is `slugify` in `services/service.go` (~`:1080`).
- **Soft delete only on the tenant row.** `DeleteTenant` sets a `deletedAt` timestamp. Every read query filters these out at the Mongo layer unless `includeDeleted` is explicitly requested (admin list only). The plain `DeleteTenant` has no owner-check today — the platform-admin path reuses it directly.
- **Cascade-delete fans out via `RegisterPostDeleteHook`.** `DeleteTenant` and `PurgeTenant` build a `TenantPostDeleteContext` (kind, owner, "owner-has-other-tenants" flag, hard/soft) before mutation, then **hard-delete** memberships + closure-table rows (those have no soft-delete pattern), then run registered hooks best-effort. Today the authz module wires a hook that drops every tenant-scoped binding and flushes the permission cache, and the tenant module itself wires a hook that calls `iface.ClientUserProvider.SoftDeleteAndAliasEmail` for external owners with no other live memberships — so a Tier-2 self-serve user can re-register with the same email after their only org is deleted. Hook errors are logged via the audit sink (`tenant.cascade.hook_failed`) but do not abort subsequent hooks.
- **Invite tokens are stored as SHA-256 hashes, never plaintext.** `generateInviteToken` (services/service.go) produces 32 bytes of randomness → base64url → SHA-256 hex. The raw token is populated on `models.Invite.Token` (a `bson:"-"` transient field) and returned once on the create response; the database only holds `tokenHash`. `AcceptInvite` hashes the supplied token and looks up by `tokenHash`. This mirrors the email-token pattern in the auth module. Expired invites are auto-reaped by the `expiresAt` TTL index (`expireAfterSeconds=0`).
- **`Membership.Roles` is a denormalization.** It's an array of authz role names. When authz bindings change, the tenant service is **not automatically kept in sync** — there's no event hook yet. If you see a divergence between authz bindings and the tenant membership's `Roles`, the authz bindings are the source of truth.

## What this module does NOT do

- Role bindings, permission evaluation, cascade rules → **authz**
- User identity, profile, password, email verification → **user** / **auth**
- Billing/subscription state (Stripe, invoices) → belongs to a future billing-addon; this module only stores the plan *name*
- Usage metering, quotas, rate-based enforcement → not implemented; plan features are boolean flags only
- Org-level preferences beyond settings blob → settings is a free-form map today, not typed

## Rules

- **Platform-admin delete bypasses ownership.** The `/v1/admin/tenants/{tenantId}` DELETE route is gated by `system.tenants.admin` (system roles only). The plain per-tenant DELETE route at `/v1/tenants/{tenantId}` has no owner check today; adding one is pending the "transfer ownership" flow.
- **Never store a plaintext invite token.** Enforced: `models.Invite.Token` is `bson:"-"`; only `TokenHash` is persisted. If you add a second invite-like flow (e.g. a public "share link"), use the same pattern and never add a plaintext field to the document.
- **When you add a new paid capability**, declare it in the owning module's `Capabilities()` method and gate the relevant routes with `RequireCapability(capID)`. Wire a subscription tier that grants it via `PricingTier.Capabilities` so the entitlement syncer hands it out on checkout. Plan names are informational — do not scatter plan-name logic through the codebase.
- **If you add a new permission**, put it in `module.go::Permissions()` and gate the relevant handler in `module.go::RegisterRoutes` — don't scatter `RequirePermission` calls across the handlers package, keep them at the route-group boundary.
- **Do not keep `Membership.Roles` in sync with authz bindings by hand.** Long term this denormalization needs an event-based sync; until then, treat `Membership.Roles` as a hint the JWT can read quickly, and `authz.GetEffectivePermissions` as the source of truth.

## Org-scoping invariants

The system-wide invariants that govern tenant isolation live in [`../authz/CLAUDE.md`](../authz/CLAUDE.md#org-scoping-invariants-system-wide). Three of them are directly owned by this module:

- **Invariant #1** — every addon `collection.Find/Update/Delete/Aggregate` must derive its filter from `pkg/sdk/tenantrepo.Scope*`. Enforced at dev time by panic in the helper; CI-enforced in Phase 0 by the `tools/tenantscope` analyzer.
- **Invariant #2** — `X-Tenant-ID` header must match a membership in the JWT. Already enforced in `shared/middleware/auth.go::resolveCurrentTenant`, with one exception: holders of `system.tenants.admin` bypass the check via `tryImpersonationBypass` (operator admins can act in any tenant). Every impersonation emits an `admin.tenant.impersonate.{personal,business}` audit event through `iface.AuditSink` — split by the target's IsCompany+SignupChannel shape so SOC2 review can tell apart sensitive personal-tenant access from routine operator work. Personal targets (IsCompany=false + SignupChannel=self_serve) additionally require a fresh MFA-satisfied session: a pwd-only operator hits the standard 401 `step_up_required` envelope before the bypass applies. Handlers that want to refuse destructive self-targeted actions while impersonating can read `middleware.IsImpersonating(ctx)`.
- **Invariant #6** — `tenants.ownerUserUUID` is immutable without a two-step owner-transfer flow. **Not yet enforced** — Phase 2 work. Until then, platform admins changing `ownerUserUUID` directly is a known gap.

## Related

- [`../user/CLAUDE.md`](../user/CLAUDE.md) — hard dep; user accounts must exist before memberships
- [`../authz/CLAUDE.md`](../authz/CLAUDE.md) — provides the role-name vocabulary this module stores in `Membership.Roles`; owns the **Org-scoping invariants** table
- [`../auth/CLAUDE.md`](../auth/CLAUDE.md) — embeds memberships in JWT claims via `TenantProvider.ListUserMemberships`
- [`../../../pkg/sdk/iface/interfaces.go`](../../../pkg/sdk/iface/interfaces.go) — `TenantProvider` interface definition (`type TenantProvider interface` ~`:366`)
- [`../../../pkg/sdk/tenantrepo/`](../../../pkg/sdk/tenantrepo) — helpers for other modules that need to scope queries by tenant
