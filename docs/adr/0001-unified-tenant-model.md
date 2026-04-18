# ADR-0001 — Unified `Tenant` model for two-tier multi-tenancy

| Field | Value |
|---|---|
| **Status** | Accepted |
| **Date** | 2026-04-18 |
| **Authors** | @salvatore.balestrino |
| **Supersedes** | — |
| **Related** | Phase 0 of the [SOTA tenancy plan v2](../../../../../home/tore/.claude/projects/-mnt-c-Users-tore-orkestra/memory/project_tenancy_plan_v2.md) |

## Context

Orkestra is a modular orchestrator platform with **two distinct tenant tiers**:

- **Tier 1 — Internal tenants (operator side):** the companies that run Orkestra.
- **Tier 2 — External client tenants (customer side):** external clients that register, may themselves be multi-tenant (sub-workspaces), and subscribe to services Orkestra exposes.

A 2026-04-18 audit of the codebase against this stated goal found the architecture is **mostly flat**:

- `backend/internal/core/tenant/models/org.go` — `Org` aggregate with no tier discriminator.
- `backend/internal/addons/subscriptions/models/client.go` — separate `Client` collection for external buyers, non-login, with a nullable `OrgUUID` hook that is essentially dead code.
- `backend/internal/shared/module/config_service.go` — `module_configs` is a single global collection; module enablement is not per-tenant.
- `backend/internal/core/authz/models/authz.go` — `Role` has `OrgID` but only system roles are seeded; no tier-aware assignment validator.
- `backend/internal/core/auth/services/jwt_service.go` — JWT claims carry `memberships` but no `acting_tenant_id` or `tenant_kind`, so middleware cannot dispatch on tier.

The intent of "external clients register and subscribe" is documented in comments (e.g. `subscriptions/models/client.go:17`, `"Clients do not log in to Orkestra in v1"`) but not expressed in the schema or enforcement code.

We are still in a pre-GA development phase and have explicit permission to make breaking schema and API changes.

## Decision

Collapse the current `Org` aggregate and the `subscriptions.Client` collection into a **single `Tenant` aggregate** with an explicit tier discriminator.

### Shape

```go
type Tenant struct {
    UUID             string
    Kind             TenantKind       // internal | external
    ParentTenantUUID *string          // nil for root tenants; set for sub-tenants of an external client
    Status           TenantStatus     // provisioning | active | suspended | archived | purged
    Slug             string           // unique within (Kind)
    DisplayName      string
    LegalName        string           // for billing / FatturaPA
    PrimaryContact   Contact          // email + phone; replaces subscriptions.Client fields
    BillingAddress   Address          // replaces subscriptions.ClientAddress
    VATNumber        string
    FiscalCode       string
    SignupChannel    string           // self_serve | sales_assisted | seeded | invite
    IdPConfigUUID    *string          // BYO identity provider (Phase 3)
    RetentionPolicyID *string         // per-tenant data retention (Phase 4)
    KMSKeyID         *string          // per-tenant envelope-encryption key (Phase 4)
    Region           string           // reserved for multi-region routing; default "eu-west"
    StripeCustomerID string           // replaces Client.StripeCustomerID
    Metadata         map[string]string
    CreatedAt        time.Time
    UpdatedAt        time.Time
    ArchivedAt       *time.Time
    PurgedAt         *time.Time
}

type TenantKind string
const (
    TenantKindInternal TenantKind = "internal"
    TenantKindExternal TenantKind = "external"
)

type TenantStatus string
const (
    TenantStatusProvisioning TenantStatus = "provisioning"
    TenantStatusActive       TenantStatus = "active"
    TenantStatusSuspended    TenantStatus = "suspended"
    TenantStatusArchived     TenantStatus = "archived"
    TenantStatusPurged       TenantStatus = "purged"
)
```

### Hierarchy — closure table

`ParentTenantUUID` supports nesting (external clients that are themselves multi-tenant). To answer "is A an ancestor of B?" in a single indexed lookup, we materialize the transitive closure in `tenant_ancestors`:

```
tenant_ancestors(descendantUUID, ancestorUUID, depth)
  unique index (descendantUUID, ancestorUUID)
  index (ancestorUUID)
```

Every tenant has a self-row with `depth=0` and a row per ancestor up to the root. `Status=archived|purged` is propagated to descendants via a domain event, not a cascade.

### What we remove

1. `backend/internal/core/tenant/models/org.go` — replaced by `models/tenant.go`.
2. `backend/internal/addons/subscriptions/models/client.go::Client` — external buyers *are* `Tenant{Kind=external}`. Subscriptions own `TenantUUID` directly.
3. `subscriptions_clients` MongoDB collection — data re-seeded into `tenants` during rewrite (dev phase, no prod data).
4. `Plan`/`Features` fields on tenant — superseded by capability entitlements in Phase 2. Drop now; re-introduce via the subscriptions module.
5. JWT claim `mbr[].r` (role list per membership) — replaced by `acting_tenant_id` + lazy role lookup (prevents JWT bloat for multi-tenant users).

### Tenant-scoping across addons

Every tenant-owned collection gains a non-null indexed `tenantUUID` field (Phase 0 task 6). The `tools/tenantscope` analyzer is upgraded to fail the build if a repository method on a tenant-owned collection does not take `tenantUUID` as the first argument (Phase 0 task 5).

### JWT claim redesign

Existing shape (simplified):
```json
{
  "sub": "<userUUID>",
  "srole": "administrator",
  "dorg": "<defaultOrgUUID>",
  "mbr": [{"oid": "<orgUUID>", "r": ["administrator"]}]
}
```

New shape:
```json
{
  "sub": "<userUUID>",
  "srole": "administrator",
  "acting_tenant_id": "<tenantUUID>",
  "acting_tenant_kind": "external",
  "memberships": [
    {"tid": "<tenantUUID>", "kind": "external"}
  ]
}
```

Rules:
- `acting_tenant_id` is **required** on every access token. Tokens without it are rejected.
- `memberships` carries only `(tenant_id, kind)`. Role lookup happens per-request via authz — consistent with the existing "permissions are not embedded in the JWT" invariant, extended to roles too.
- Tier-aware middleware dispatches on `acting_tenant_kind`: routes tagged `TierRequired=internal` reject external acting tenants; routes tagged `TierRequired=external` reject internal; untagged routes are tier-agnostic.

## Consequences

### Positive

- Tier becomes a first-class, queryable, indexable property — no more "is it an operator or a client" guesswork.
- External clients become first-class actors: they can have users, subscriptions, and sub-tenants without the `Client`-vs-`Org` impedance mismatch.
- Subscriptions wire directly to `Tenant{Kind=external}`, unblocking the Tier-1 → Tier-2 consumption model.
- The closure table answers hierarchy queries (e.g. "find every sub-tenant that inherits this subscription") in O(depth) reads with a single indexed lookup.
- Tier-aware authorization (Phase 1 Cedar policies) has a clean principal shape to match on.
- JWT payload shrinks for users with many memberships — eliminates the "JWT bloat" risk noted in the plan.

### Negative / costs

- Breaking change to the JWT shape — every issued token is invalidated at the rollout boundary. Acceptable in dev phase.
- Breaking change to `subscriptions_clients` — dev data re-seeded. Acceptable.
- `subscriptions/handlers`, `subscriptions/services`, `payments/services` all need refactoring to swap `ClientUUID` for `TenantUUID`.
- Every addon repository gains a `tenantUUID` parameter on its CRUD methods, enforced by `tenantscope`.
- Per-tenant `Plan` goes away; until Phase 2 ships the capability+entitlement model, module enablement remains platform-global (temporary regression in functional scope).

### Neutral

- `administrator`, `manager`, etc. system roles are unchanged by this ADR — Phase 1 will rework RBAC with Cedar and tier-label them.
- `Membership` (user × org × roles) survives but is renamed `TenantMembership` and gains `tenant_kind`.
- Per-tenant encryption keys (`KMSKeyID`) are reserved now but not implemented until Phase 4.

## Alternatives considered

### A. Keep `Org` + `Client` separate, add a tier field to `Org` only

Rejected. Leaves the `Client`-vs-`Tenant`-promotion ambiguity unresolved. Subscriptions code would still have to dispatch on "is this a Client or a promoted Org" — we've already seen this produce dead-code hooks (`Client.OrgUUID`).

### B. Two separate collections: `internal_tenants` + `external_tenants`

Rejected. Duplicates schema, makes cross-tier queries (platform-wide tenant count, audit across tiers) require unions. Kind-discriminator on a single collection is the standard pattern in B2B SaaS tenancy and is friendlier to the static analyzer.

### C. Materialized path (`/root/parent/self`) instead of closure table

Rejected. Materialized path is simpler for shallow reads but terrible for "find all descendants of X" (the prefix scan is large and non-selective in MongoDB). Closure table gives O(1) ancestor lookup, O(descendants) bounded descendant lookup, and cleanly supports propagating status down. The extra write cost on tenant create is negligible at expected volumes (~thousands).

### D. Flat design — single tier, assume external clients "are just orgs"

Rejected outright. That *is* today's design. It is precisely what the 2026-04-18 audit found does not match product intent. The CLAUDE.md Tenancy Model section makes the two-tier distinction a first-class contributor rule.

## Rollout

1. Land this ADR (current commit).
2. Phase 0 implementation (per [plan](../../../../../home/tore/.claude/projects/-mnt-c-Users-tore-orkestra/memory/project_tenancy_plan_v2.md)): new `Tenant` model, closure table, `tenantUUID` propagation, JWT redesign, `tenantscope` upgrade, legacy removal.
3. Verify: all modules build, integration tests pass, dev docker stack boots.
4. Phase 1+ proceed on top of the new foundation.

No staged rollout, no feature flag. Dev phase, breaking change, one cut.
