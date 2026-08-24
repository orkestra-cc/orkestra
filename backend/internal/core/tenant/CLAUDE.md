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
| `module.go` | Module registration, collections, permissions, `ConfigSchema()` (provisioning policy), service wire-up, `slotCount` seam (defaults to `svc.CountProvisioningSlotsByKind`, overridden in tests) |
| `config_validation.go` | `ValidateConfig`/`ValidateConfigActivation` — the shared Tier-1 provisioning-policy gate applied to every module-config surface. See [Provisioning policy](#provisioning-policy-admin-managed) |
| `reconcile.go` | `Module.Start` — versioned boot reconciliation: the reconcile-lease election, the record-absent rule, the Tier-1 `open`→`manual` rewrite, migration default assignment, setup-coordinator creation, and `setupReconciliationVersion`. See [Boot reconciliation](#boot-reconciliation) |
| `handlers/handler.go` | HTTP handlers for org and membership CRUD + invites; provisioning manual-gate (`enforceManualGate`/`callerIsTenantAdmin`) + provisioning-policy read; platform-admin routes split across `RegisterAdminRoutes` (reads + non-destructive mutations) and `RegisterAdminDestructiveRoutes` (default transfer, archive/delete, purge — MFA-gated, see [MFA boundary](#default-tenant-invariants)); derived `isDefault` stamping on `adminTenantListItem`, the admin get-tenant response, `membershipRow`, and `memberDTO` via `tenantSvc.DefaultTenantUUID` |
| `services/service.go` | Org lifecycle, membership sync, invite token issuance, provisioning policy (`ProvisioningMode`/`CountProvisioningSlotsByKind`/`ErrProvisioningLocked`), `iface.TenantProvider` implementation, platform default-tenant assign/transfer/read (`DefaultTenantUUID`/`GetDefaultTenant`/`AssignDefaultTenant`/`TransferDefaultTenant`) + the `RunDefaultGuarded` lifecycle guard wrapping `SuspendTenant`/`ArchiveTenant`/`DeleteTenant`/`PurgeTenant` — see [Default tenant invariants](#default-tenant-invariants); `createTenantWithUUID` (the absent-to-present creation primitive `CreateTenant` wraps) + `EnsureSetupTenant`/`reconcileSetupTenant` (the reserved-UUID reconciliation seam a resumable setup saga converges through) — see [Creation vs. reconciliation](#creation-vs-reconciliation) |
| `services/reconcile.go` | The service half of boot reconciliation: `OldestOperationalTenant`, `HasAnyTenant`, `TenantDefaultAssigned`, `DefaultTenant` (the non-operational-tolerant pointer read, distinct from the narrow `GetDefaultTenant` provider), and `AuditInternalOpenModeMigrated` — the audit sink lives on the service, so the emit does too |
| `services/billing.go` | Unified-clients (Phase 1) — `SetItalianBillable`, `SetBillingIdentity`, `ResolveBillingParty`, `EnsureTenantForUser` (honours external provisioning mode), `iface.BillingTenantProvider` implementation |
| `services/entitlements.go` | Capability-entitlement projection (`iface.AccessProvider`) |
| `repository/repository.go` | MongoDB CRUD for orgs, memberships, invites; personal-tenant predicate lookup; `CountProvisioningSlotsByKind` (single-mode invariant); `RestoreTenant` (reverses `SoftDeleteTenant`, used only by the setup-tenant reconciliation seam — see [Creation vs. reconciliation](#creation-vs-reconciliation)); `ListOperationalByKindOldestFirst` + `CountAllTenants` (boot reconciliation — see [Boot reconciliation](#boot-reconciliation)) |
| `repository/defaults.go` | `GetDefault`/`SetDefault`/`AssignDefault`/`RunDefaultGuarded` — the platform-global default-tenant pointer (`tenant_defaults`). `SetDefault`, `AssignDefault`, and `RunDefaultGuarded` share a `withTxn` helper and all run inside a MongoDB multi-document transaction (requires a replica-set deployment). `SetDefault` (requireExisting=true only — the admin-transfer path) always overwrites; `AssignDefault` (the setup/migration entry point) instead detects an assign-time conflict — an existing pointer naming a DIFFERENT tenant — INSIDE its own transaction via `ErrDefaultAlreadyAssigned`, so two concurrent assigns can never silently clobber one another the way a service-level pre-check-then-write would. Relies on `tenant_defaults`'s unique `kind` index (see [collections](#mongodb-collections)) for its write-conflict retry to actually fire on two concurrent from-scratch inserts |
| `models/tenant.go` | `Tenant`, `TenantMembership`, `TenantInvite`, `TenantAncestor`, `TenantDefault`, `FatturaPAProfile` structs + `TenantKind`/`TenantStatus`/plan/`ProvisioningMode*`/`DefaultUpdateSource*` constants |
| `models/entitlement.go` | Capability-entitlement projection row |

## MongoDB collections

Declared in `module.go::Collections()` (`module.go:98`).

| Collection | Indexes | TTL |
|---|---|---|
| `tenants` | `uuid` unique, `slug` unique sparse, `ownerUserUUID`, `kind`, `status`, `parentTenantUUID` sparse | — |
| `tenant_memberships` | compound `(userUUID, tenantId)` unique, `tenantId` | — |
| `tenant_invites` | `tokenHash` unique sparse, `tenantId`, `expiresAt` TTL(ExpireAt) | `expiresAt` is a TTL index with `expireAfterSeconds=0` so Mongo reaps the doc the moment the timestamp passes. |
| `tenant_ancestors` | compound `(descendantUUID, ancestorUUID)` unique, `ancestorUUID` | — — closure table for the tenant hierarchy (ADR-0001) |
| `tenant_entitlements` | `uuid` unique sparse, compound `(tenantUUID, capabilityId)`, `capabilityId`, `expiresAt` sparse | — — capability projection (at most one active row per capability, enforced in the service) |
| `tenant_defaults` | `kind` unique | — — **platform-global, no `tenantId`**: one row per `TenantKind`, replaced atomically. Holds the platform default Tier-1 tenant pointer that begins operator resolution; a tenant document never carries an `isDefault` flag — any DTO field of that name is derived from this row. Every read/write carries an inline `//tenantscope:allow` exemption (see [Org-scoping invariants](#org-scoping-invariants)) rather than a `tools/tenantscope/baseline.txt` entry. |

Collection name constants live in `repository/repository.go` (`CollTenants`, `CollMemberships`, `CollInvites`, `CollAncestors`), `repository/entitlements.go` (`CollEntitlements`), and `repository/defaults.go` (`CollDefaults`).

## Dependencies

- **Modules**: `user` (`module.go::Dependencies`) — so user profiles exist before memberships reference them.
- **Required services**: `module.ServiceSetupFinalizationStore` (`systeminit.FinalizationStore`) and `module.ServiceUserService` (narrowed to the `userCounter` slice of `iface.UserProvider`). Both are resolved in `Init` and a missing one **returns an error**; they exist solely for boot reconciliation in `Start` (see [Boot reconciliation](#boot-reconciliation)). Nothing on the request path consults them — tenant CRUD still trusts the caller's auth context and never looks a user up.
- **Optional services**: none. At request time the handler resolves `iface.AuthzProvider` lazily from the registry to enforce the provisioning `manual` gate (see [Provisioning policy](#provisioning-policy-admin-managed)).
- **Provides** (`module.go::ProvidedServices`): `ServiceTenantProvider` → `iface.TenantProvider`, `ServiceAccessProvider` → `iface.AccessProvider`, `ServiceTenantService` (concrete), `ServiceBillingTenantProvider` → `iface.BillingTenantProvider`, `ServiceDefaultTenantProvider` → `iface.DefaultTenantProvider` (narrow — see [Default tenant invariants](#default-tenant-invariants)).
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

**Terminology — two distinct predicates, not one fuzzy "active":**

- An **operational tenant** has `status == active` AND `deletedAt == nil`. Only an operational internal tenant is eligible to become the platform default.
- A tenant **occupies a provisioning slot** when `deletedAt == nil` AND its status is one of `provisioning`, `active`, or `suspended`. A suspended tenant is still part of the installation, so it keeps its slot; `archived` and `purged` tenants free their slot even if a legacy row was never soft-deleted.

`repository.CountProvisioningSlotsByKind` implements the second predicate and backs the `single` cardinality gate, config validation, and the admin provisioning-policy read. `repository.ListOperationalByKindOldestFirst` implements the first, and is what boot reconciliation selects the platform default from. A lifecycle check that needs an operational tenant uses an exact status predicate instead of the slot count — never conflate the two.

### Creation vs. reconciliation

**Every actual creation or restoration passes the same service-level provisioning guard; reconciliation of a reserved tenant that already occupies its slot is not a new creation.** This is the contract `CreateTenant` and `EnsureSetupTenant` (`services/service.go`) both honour, and it is why they are built from the same primitives but never trip the `single` gate against each other's work:

- **`createTenantWithUUID`** (`services/service.go`) is the shared absent-to-present primitive: it always inserts a brand-new row, so it always runs the `single`-mode count check first (see [Provisioning policy](#provisioning-policy-admin-managed)). `CreateTenant` is a thin wrapper that mints a fresh UUIDv7 and delegates to it. **`EnsureSetupTenant`** (Task 4.4) is the second-stage primitive a resumable setup saga (a later PR) calls, potentially many times, to converge a *reserved* tenant UUID into a fully provisioned, operational tenant. When no row exists yet at that UUID, `EnsureSetupTenant` calls the *same* `createTenantWithUUID` primitive — with an EXPLICIT `models.PlanEnterprise`, never `CreateTenant`'s empty-plan-defaults-to-`free` fallback — so this path is a genuine creation and the `single` gate applies exactly as it does for any other tenant.
- **`reconcileSetupTenant`** (`services/service.go`) is the reconciliation half: it only ever runs against a row that `GetTenantByUUIDIncludingDeleted` already found. A row that already occupies a provisioning slot (see the predicate above) is validated (immutable setup identity — kind, owner, normalized name, slug) and its dependents (plan, KMS key, closure self-row, owner membership, authz binding) are reconciled *without ever consulting the `single` gate* — the row is not being created, so it must never be counted against itself. This is the ordering the whole seam exists to get right: getting it wrong means a retry after a partial failure fails with `ErrProvisioningLocked` caused by its own tenant.
- **Restoring a soft-deleted reserved row requires the caller's coordinator attestation, and is the one reconciliation branch that DOES re-apply the gate** — the latter against OTHER occupants only. A prior attempt's partial-failure rollback (`createTenantWithUUID`'s own `SoftDeleteTenant` calls on a KMS/membership/bind-owner failure) can leave the reserved row soft-deleted; restoring it re-occupies a slot, which is a state transition serious enough to require the same cardinality check a genuine creation gets. Because the row's own `deletedAt != nil` already excludes it from `CountProvisioningSlotsByKind`'s count, the check can only ever see occupants other than itself.
  **Restoration requires TWO independent proofs, and neither alone is sufficient.** They answer different questions:

  1. **WHO is asking — `EnsureSetupTenant`'s trailing `coordinatorAttested bool`.** The caller's statement that the setup coordinator record (`systeminit`, `key: setup_finalization`) for *this* reservation exists and is not completed. Only the setup finalization saga holds the coordinator, so only it can attest (it derives the flag from the record it just CAS-claimed a stage on — `rec.RequestHash != "" && rec.CompletedAt == nil`); every other caller passes `false`. The tenant service deliberately does not resolve the coordinator itself — `systeminit` is not reachable from here, and a second source of truth for it would be worse than a documented caller contract.
  2. **WHAT deleted the row — its persisted `DeletedReason` (`models.TenantDeleteReason`), which must be `provisioning_rollback`.** The attestation proves a reservation is *open*, **not** that this row's deletion was that saga's own rollback — and an open reservation is precisely the window in which an operator can reach `DELETE /v1/admin/tenants/{id}`, because **there is no backend setup gate on it** (the gate is the frontend `SetupGate` component; `grep -rn "SetupRequired" backend/internal/` finds nothing, and the `setup` package registers no middleware). So a saga retry after a deliberate, MFA-gated deletion would arrive attested and silently resurrect the tenant. `SoftDeleteTenant` therefore takes the reason as a **required** parameter at every call site: `DeleteTenant` stamps `admin_action`, `createTenantWithUUID`'s own unwind stamps `provisioning_rollback`, and `RestoreTenant` clears the field so a later deletion cannot inherit it. An **empty** reason (a row predating the stamp) is not a rollback either — the check fails closed, because resurrecting a row whose deletion nobody can account for is exactly the failure it exists to prevent.

  The provenance check runs **before** the `single` slot check: whether a row is restorable at all precedes whether there is room for it. Anything that fails either proof returns `ErrSetupTenantRemediation`. Regression tests: `services/setup_tenant_integration_test.go` (`…_AdminDeletedRowIsRemediation`, `…_UnknownDeleteProvenanceIsRemediation`, `…_RollbackProvenanceStamped`).
- **A row that reached `Status == purged` (or carries `PurgedAt`) is never resurrected** by either path — `PurgeTenant` has already crypto-shredded its KMS key, so restoring it would be pointless and misleading. `reconcileSetupTenant` returns `ErrSetupTenantRemediation`; an operator must remediate (e.g. assign a fresh reservation) rather than have the saga retry against a dead row.
- **A row an operator explicitly archived (`ArchiveTenant`) is ALSO never resurrected — a distinct, non-restorable state from the seam's own soft-delete rollback.** `ArchiveTenant` sets `Status == archived` without ever touching `deletedAt`; `SoftDeleteTenant` (the primitive's own rollback, and `DeleteTenant`) sets `Status == archived` *together with* `deletedAt`. `reconcileSetupTenant` tells these apart precisely by `deletedAt`: `Status == archived && deletedAt == nil` is an admin's deliberate archive and returns `ErrSetupTenantRemediation` immediately, before the restore branch below ever runs; `deletedAt != nil` is the seam's own rollback signature and IS eligible for restoration. Losing this distinction would either silently resurrect a tenant an operator archived on purpose, or refuse to resume a legitimate retry — both wrong. This is why the two lifecycle predicates in [Lifecycle](#lifecycle) are never blurred into one fuzzy "not usable" bucket.
- **Identity mismatch is never silently adopted.** If the reserved UUID already names a row whose kind, owner, normalized name, or slug doesn't match what the caller supplied, `reconcileSetupTenant` returns `ErrSetupTenantConflict` rather than treating the row as "close enough". The same rule governs the slug: when an UNRELATED tenant holds the slug the reserved UUID was meant to get, `EnsureSetupTenant` never adopts it — `ErrSlugAlreadyInUse` propagates.
- **Every dependent reconciliation is idempotent per row, backed by a database uniqueness constraint** — never by "the caller said it's the only one running": the tenant row by its unique `uuid`/`slug`, the closure self-row by the unique `(descendantUUID, ancestorUUID)` index, the owner membership by the unique `(userUUID, tenantId)` index (a duplicate-key loser rereads the winner and VALIDATES its `IsOwner`/role/kind rather than trusting it), the KMS key by `iface.KMSProvider.CreateKey`'s concurrency contract (Task 4.1), and the authz binding by `EnsureBinding` (Task 4.2). A duplicate-key error surfaced anywhere in `createTenantWithUUID`'s own call chain — not just on the tenant row itself — is treated by `EnsureSetupTenant` as "a concurrent attempt progressed on this reserved UUID": it rereads and reconciles rather than failing, which is what lets a losing creator's own downstream duplicate-key race resolve through the identical recovery path a genuine concurrent reconciler uses.

- **Init**: constructs the repository, builds the service, creates the handler, registers the service as `iface.TenantProvider` in the registry, wires the `ProvisioningModeResolver` (a closure over `deps.ConfigService` reading the `provisioning.{internal,external}.mode` keys live), and resolves the two **required** boot-reconciliation seams — `systeminit.FinalizationStore` (`module.ServiceSetupFinalizationStore`) and the narrow `userCounter` slice of `iface.UserProvider` (`module.ServiceUserService`). Either one missing returns an error from `Init`: missing wiring fails module initialization loudly rather than degrading the upgrade path into a silent no-op.
- **Start**: versioned boot reconciliation — see [Boot reconciliation](#boot-reconciliation). **Stop / HealthCheck**: inherit from `BaseModule` (no-op).
- **Seeding**: the first-install setup flow provisions **exactly one** internal tenant, and it is **mandatory**. `POST /v1/setup/admin` still creates no tenant — the initial admin is a super_admin (system role, tenant-independent) — but the wizard's organization step is **not skippable**: it calls `POST /v1/setup/finalize`, whose resumable saga persists the Tier-1 provisioning mode, ensures that one internal tenant through `EnsureSetupTenant` (reserved UUID, `plan=enterprise`), assigns it as the **platform default**, and only then marks setup complete. So a finished install always has one internal tenant and one `tenant_defaults` pointer; a zero-internal-tenant install is an *unfinished* setup, not a supported steady state. Contract: [`internal/shared/setup/CLAUDE.md`](../../shared/setup/CLAUDE.md).
- **GDPR/DSR** (`services/pii_producer.go`): registers an `iface.PIIProducer` (subject `"tenant"`) on `ServicePIIProducerRegistry` at Init. The subject's personal data here is their **tenant memberships** (which orgs, what roles) — the orgs/tenants themselves are not the subject's data and are left intact. Export returns the membership rows; purge deletes them (`tenant_memberships`) under **both** erase modes, since a membership row IS the user→org linkage with no anonymizable residue. Consumed by the [compliance module](../compliance/CLAUDE.md)'s DSR pipeline (ADR-0009).

## Boot reconciliation

`tenant.Module.Start` (`reconcile.go`) runs **versioned setup reconciliation** — the upgrade path an existing installation takes the first time it boots this code. It is never triggered by an HTTP request.

**Why `Start` and not `Init`.** By the time `Start` runs, every core module has completed `Init`: the audit sink is wired into the tenant service, module configuration is readable, collection indexes exist. `StartAll` still runs before `ListenAndServe`, and tenant is a **core** module, so `StartAll` propagates its `Start` error and aborts backend startup rather than logging it and continuing (`pkg/sdk/module/registry.go::StartAll`). A replica can therefore never come up serving traffic having silently skipped the migration. A legitimate `admin_required` / `tenant_required` outcome is *persisted state*, not a startup failure — only a read or write failure returns an error.

**The three steps, each idempotent.** They reconcile **pointers and configuration only**: reconciliation never creates, renames, archives or purges a tenant, and never touches a membership.

1. **Tier-1 `open` → `manual` rewrite.** Every persisted `provisioning.internal.mode == "open"` is rewritten — the legacy top-level config *and* **every** environment profile, active or not, because switching profiles would otherwise re-introduce a value the Tier-1 schema no longer offers. Tier-2 `open` is a valid, supported mode and is never touched. Both write paths run the module's own provisioning-policy validator (see [Provisioning policy](#provisioning-policy-admin-managed)) and `manual` always passes it, so the rewrite cannot be rejected. **One `tenant.provisioning.internal_open_migrated` audit event per installation, not one per read** — the event fires only when a rewrite actually happened, and after the rewrite no `open` remains for a later run to find.
   **A legacy installation configured as `single` keeps `single`**, even while it holds several occupied Tier-1 provisioning slots and is therefore blocked from further Tier-1 creation. That is a remediation state for an administrator to resolve — archive or delete slot occupants until one remains, or explicitly choose `manual` — and silently loosening it here would discard operator intent.
2. **Platform-default assignment.** Skipped entirely when a pointer already exists (the RAW pointer, via `TenantDefaultAssigned` — an established default only moves through `TransferDefaultTenant`). Otherwise the oldest **operational** Tier-1 tenant becomes the default: `repository.ListOperationalByKindOldestFirst` applies the `status == active && deletedAt == nil` predicate and sorts `createdAt asc, uuid asc`. Suspended, archived, purged and soft-deleted tenants are never selected. **The total ordering is load-bearing**, not cosmetic — `createdAt` alone is not total, and two replicas must agree on the winner.
3. **Setup-coordinator creation**, when the record is missing: a `migration` record already marked complete (carrying the tenant snapshot + resolved Tier-1 mode) when a default can be assigned without interaction; a `legacy_recovery` record with an **empty** `adminUUID` at `StageConfig` when no operational Tier-1 tenant exists, which an authenticated active `super_admin` later claims. An **existing** record is left alone whatever state it is in — the finalizer-access recovery rules govern its claim, not this migration.

**Migration-sourced writes use a system actor.** Audit events carry `ActorType: "system"` with an **empty** `ActorUserID` — the literal string `system` never lands in an actor-UUID field — and the default pointer row records its automated origin through `updateSource: "migration"` while carrying **no** `updatedBy` key at all (`repository.AssignDefault`'s `$unset`).

**Concurrency: two leases, never confused.** Every replica reads `reconciliationVersion` on `system_init/{key: "setup_finalization"}` and returns immediately once it is current. Otherwise one replica wins `ClaimReconcileLease` (`reconcileLeaseOwner` / `reconcileLeaseUntil`, 60s TTL) and performs the writes; losers wait `reconcileWaitInterval` and re-check, taking over only after an expired lease. **The reconcile lease is a separate mechanism from the setup saga's per-stage lease** (`leaseOwner` / `leaseUntil`) on the same document — neither side ever reads or clears the other's fields, and they coordinate different work over different lifetimes.

**Record-absent rule.** `ClaimReconcileLease` is a CAS on an *existing* document, so a record must exist before any replica can be elected. `ensureReconcileCoordinator` is the one part of reconciliation that necessarily runs without the lease, and is therefore restricted to **reads plus a single insert-only `EnsureRecord`** — any number of replicas racing through it converge on one record without any of them performing a reconciliation write. Three outcomes:

- **Pristine** (no operator users **and** no tenant rows in any lifecycle state, soft-deleted included): only the config rewrite runs — idempotent and safe for concurrent replicas without coordination. **No record is created and no version is stamped.** The fresh path binds the coordinator through `InitializeFresh` after the initial administrator is created; the next boot re-runs this check, which costs one read. Stamping a version for work never performed would make the real migration be skipped forever on the boot where it finally matters — which is exactly why `runReconciliationV1` returns `(pristine bool, err error)` and the caller **skips the completion CAS** when it is true.
- **Not pristine, record absent**: create it first (insert-only), then loop back to the lease claim so exactly one replica performs the writes under it.
- **Record present, version incomplete**: claim the lease and reconcile.

**Bumping the version.** `setupReconciliationVersion` (`reconcile.go`) is bumped **only when the reconciliation work itself changes**, so an installation that already completed the previous version re-runs the new one. v1 is the three steps above. Bumping it re-runs *every* step on every installation, so each step must stay idempotent forever — a step that is not safe to repeat belongs in a new version's own guard, never in an existing one.

## Default tenant invariants

The platform default is a single global pointer (`tenant_defaults`, `kind=internal` only — see the [collections table](#mongodb-collections)) at the tenant used to begin operator resolution. `services/service.go` exposes it through four methods, all delegating to `repository/defaults.go`'s guarded transactions:

- **`DefaultTenantUUID(ctx) (string, error)`** — the raw pointer's `tenantUUID`, or `""` when unassigned. Not operational-filtered: it reflects the pointer exactly, which is what the lifecycle-guard pre-checks below need.
- **`GetDefaultTenant(ctx) (*iface.Tenant, error)`** — the `iface.DefaultTenantProvider` implementation, registered under `module.ServiceDefaultTenantProvider` (a narrow service key; the wider `iface.TenantProvider` is deliberately not widened — see the interface's doc comment). Returns `(nil, nil)` — **never an error** — both when no default is assigned and when the pointer names a tenant that is no longer operational (suspended, archived, purged, or soft-deleted). The provider never hands out a non-operational target, and it grants nothing by itself: membership validation, RBAC, audience checks, and `X-Tenant-ID` override all still apply downstream.
- **`AssignDefaultTenant(ctx, tenantUUID, actorUUID, source) error`** — the setup/migration entry point. Idempotent: re-assigning the UUID the pointer already names is a nil-error no-op (no repository write, no duplicate row, no re-emitted audit event). Assigning a *different* UUID while a default is already set returns `ErrDefaultAlreadyAssigned` rather than silently replacing it — an established default only moves via `TransferDefaultTenant`, never as a side effect of Assign. On a genuine first assignment, audits `tenant.default.assigned`. **The conflict decision is made transactionally**, by `repository.AssignDefault`, not by a service-level read-then-write: two concurrent `AssignDefaultTenant` calls targeting *different* tenants on an unassigned platform cannot both observe "unassigned" and both succeed — exactly one wins and the other gets `ErrDefaultAlreadyAssigned`, enforced by MongoDB's write-conflict retry against the `tenant_defaults` unique `kind` index (`TestAssignDefaultTenant_ConcurrentDifferentTargets_ExactlyOneWins` in `services/default_tenant_integration_test.go` is the regression test — an earlier version used `repo.GetDefault` as a pre-check before calling `repo.SetDefault`, which raced).
- **`TransferDefaultTenant(ctx, tenantUUID, actorUUID) error`** — the admin transfer path (`system.tenants.admin` + step-up MFA enforced at the HTTP layer, not here). Requires an existing pointer (`repository.SetDefault`'s `requireExisting=true`) and moves it to `tenantUUID`, which `SetDefault` validates as an operational internal tenant inside the same transaction. A target rejected as not operational (`repository.ErrDefaultTargetNotOperational`) is audited as a **denied** `tenant.default.transferred` event and the pointer is left untouched; success audits the same action with the previous and new tenant UUIDs.

Both Assign and Transfer resolve the audit actor via `resolveDefaultActor`: an explicit non-empty `actorUUID` param wins (`ActorType: "user"`); when empty, the request context is consulted (`actorFromContext`); when both are empty the caller is genuinely unattended — `ActorType: "system"`, `ActorUserID: ""`. The literal string `"system"` is never stored in an actor-UUID field, only in `ActorType`; a migration-sourced write also leaves the pointer row's `updatedBy` key **entirely absent** (`repository.SetDefault`'s `$unset`), never an empty-string sentinel.

**Lifecycle guard.** Every mutation that changes a tenant's `status` or `deletedAt` — `SuspendTenant`, `ArchiveTenant`, `DeleteTenant`, `PurgeTenant` — wraps its row write in `repository.RunDefaultGuarded(ctx, models.TenantKindInternal, tenantUUID, write)`. The guard lives in the *service*, not only at the HTTP handler boundary, because it is an invariant every caller must observe, including non-HTTP callers (background flows, later saga stages) that never pass through a handler at all:

- `SuspendTenant` / `ArchiveTenant` wrap their `UpdateTenantStatus` write directly in `RunDefaultGuarded`.
- `DeleteTenant` / `PurgeTenant` **run `cascadeTenantData` inside the guarded transaction**, alongside their row write (`SoftDeleteTenant` / `UpdateTenantStatus(purged)`). The membership/ancestor cascade is a hard delete of *every* membership and closure row for the tenant, so it must never run before the guard has confirmed the target is not the platform default: a denial (or any failure of the row write) now rolls the cascade back with it, and a `409 tenant.default_reassignment_required` genuinely means nothing happened. Because `session.WithTransaction` may replay the closure on a transient error, everything inside it stays idempotent.
  They additionally run a **cheap pre-check first** — compare `DefaultTenantUUID(ctx)` against the target and bail out without opening a transaction at all in the common denied case. That pre-check is only an optimization: it is racy (a concurrent transfer can move the default between the read and the guarded write), and the guarded write remains the actual invariant. **It fails closed:** a repository error from `DefaultTenantUUID` propagates wrapped (mapped to the handlers' fixed 5xx detail) instead of being read as "not the default" — an unreadable pointer cannot prove the target is safe to destroy, and the design fails closed on unreadable state everywhere else (setup status 503s rather than infer a phase; the finalizer access probe 503s rather than offer recovery).
- **The guard's revision bump UPSERTS, and that is load-bearing on a kind with no pointer yet.** `RunDefaultGuarded` opens by bumping `revision` on the `tenant_defaults` singleton for `kind` — with `upsert: true`, so an unassigned kind gets a **placeholder** row (`kind` + `revision`, no `tenantUUID`) it deletes again before committing. The write, not the read, is what serializes the guard against `AssignDefault`: `AssignDefault` only *reads* the tenant row it validates (`validateOperationalTarget`), and MongoDB transactions are snapshot-isolated with no read-write conflict detection, so nothing else would collide. Without the upsert the bump matched zero documents on the first-assignment path and wrote nothing at all — a setup assign and a concurrent suspend/archive/delete of that same tenant could then both commit, leaving the brand-new platform default pointing at a non-operational tenant. `SetDefault` (transfer) always writes the singleton, which is why only the *first* assignment was exposed. The placeholder is never observable outside its transaction (insert and delete commit atomically together), so the collection keeps its invariant: **a `tenant_defaults` row exists if and only if a default is assigned**. `AssignDefault` additionally treats an empty `tenantUUID` as "unassigned" rather than as a conflicting assignment — defense in depth against a leaked placeholder nobody could otherwise clear. Regression tests: `repository/defaults_first_assignment_integration_test.go` (`TestRunDefaultGuarded_WritesSingletonWhenUnassigned` pins the write deterministically; `TestAssignVersusLifecycle_Serialized` is the race).
- The guard only ever names an internal pointer (`models.TenantKindInternal`); external tenants pass through untouched because an external UUID can never match the internal pointer's target.
- On denial (`repository.ErrDefaultGuard`), the service returns `ErrDefaultReassignmentRequired` (maps to `409 tenant.default_reassignment_required`, `errcode.TenantDefaultReassignmentRequired`) and emits a **denied** audit event reusing the exact same action the method emits on success (`tenant.lifecycle.suspended` / `tenant.lifecycle.archived` / `tenant.deleted` / `tenant.lifecycle.purged`), with `Outcome: "denied"` and `Metadata{"code": errcode.TenantDefaultReassignmentRequired}` — the existing denied-event convention (same action, flipped outcome) rather than a separate "refused" action name.
- Replacing the platform default therefore always follows the same order: assign/create another operational Tier-1 tenant, `TransferDefaultTenant` to it, *then* suspend/archive/delete/purge the previous one. In `single` provisioning mode, replacement additionally requires switching to `manual` first (see [Provisioning policy](#provisioning-policy-admin-managed)).

**HTTP surface and the MFA boundary.** `TransferDefaultTenant` is exposed as `PUT /v1/admin/tenants/default` (operation `set-default-tenant-admin`, body `{"tenantId": "<uuid>"}`, handler `setDefaultTenant`). This route, plus the two admin lifecycle routes that can orphan the default (`DELETE /v1/admin/tenants/{tenantId}`, `POST /v1/admin/tenants/{tenantId}/purge`), are registered by `handlers.RegisterAdminDestructiveRoutes` and mounted in `module.go` behind `RequireSystemPermission("system.tenants.admin")` **plus** `RequireMFA()` — a second route group distinct from `RegisterAdminRoutes`, which covers every other platform-admin read and non-destructive mutation behind the permission gate alone. This closes what used to be a purge-handler TODO: the irreversible purge is no longer protected more weakly than default reassignment. The handler maps `repository.ErrDefaultTargetNotOperational` to `409` with a fixed detail ("the target tenant must be an operational internal tenant") and `services.ErrDefaultReassignmentRequired` to `409 tenant.default_reassignment_required` — the same mapping the plain `deleteTenant`/`purgeTenant` handlers apply for the lifecycle-guard case described above.

**Derived `isDefault`.** `adminTenantListItem` (`GET /v1/admin/tenants`), the admin get-tenant response (`GET /v1/admin/tenants/{tenantId}`, a dedicated `tenantAdminOutput` wrapper — the tenant-scoped self-view `get-tenant` does not carry this field), `membershipRow` (`GET .../members`, the admin attach-member response), and `memberDTO` (`GET /v1/tenants`, the caller's own memberships — this is what the operator console's tenant switcher reads, so it needs the flag without a second request) all carry an `isDefault bool` field. It is resolved **once per request** via `tenantSvc.DefaultTenantUUID`, never per row, and is never written to the tenant document: the canonical state stays the `tenant_defaults` pointer row (see the [collections table](#mongodb-collections)). A stored `isDefault` column was deliberately rejected — transfer would become a two-document mutation with either a no-default window or a unique-index collision.

## Provisioning policy (admin-managed)

`module.go::ConfigSchema()` exposes two enum config keys that govern **who may create tenants**, per tier. Read at request time by the service's `ProvisioningModeResolver` (a closure over `ModuleConfigService`, 30s Redis cache) — edits at `/admin/modules/tenant` take effect on the next creation with **no restart**.

| Key | Tier | Values | Default | EnvVar (first-boot seed) |
|---|---|---|---|---|
| `provisioning.internal.mode` | internal (Tier-1) | `manual` · `single` — **fail-closed** | **`manual`** | `TENANT_PROVISIONING_INTERNAL_MODE` |
| `provisioning.external.mode` | external (Tier-2) | `open` · `manual` — fail-open, unchanged | **`manual`** | `TENANT_PROVISIONING_EXTERNAL_MODE` |

**Tier-1 (internal) is fail-closed and never self-serve.** `open` is not a valid internal mode: `Service.ProvisioningMode` (`services/service.go`) resolves a missing, unknown, or legacy-stored `open` value to `manual` for internal — never to open. (A pre-existing install can still have `open` sitting in `module_configs` from `TENANT_PROVISIONING_INTERNAL_MODE=open`; runtime resolution silently normalises it, and boot reconciliation rewrites the stored value on the next start — see [Boot reconciliation](#boot-reconciliation).) **Every Tier-1 creation path requires `system.tenants.admin`, in both `manual` and `single` mode** — `single` only adds a cardinality constraint on top of `manual`, it never grants creation authority on its own.

**Tier-2 (external) keeps its historical fail-open behaviour, unchanged by this.** `open` remains a valid, self-serve external mode: any authenticated user may create, and lazy personal-tenant provisioning (`EnsureTenantForUser`) is allowed. `manual` is the default and requires `system.tenants.admin`, same as before.

- **open** — Tier-2 only: any authenticated user may create.
- **manual** (default both tiers) — only holders of `system.tenants.admin` may create. Every Tier-1 path requires this regardless of mode; Tier-2 requires it only when `external.mode == manual`.
- **single** — Tier-1 only: at most one Tier-1 tenant may occupy a provisioning slot (see [Lifecycle](#lifecycle) for the predicate). Adds cardinality on top of `manual`; does not by itself grant creation authority.

**Default posture (both `manual`):** a fresh install accepts no self-service tenant creation. The first internal tenant is not optional — it is created by the mandatory setup finalization saga (`POST /v1/setup/finalize`), which also writes the Tier-1 mode the operator chose there (`manual` or `single`); afterwards an operator creates further internal tenants deliberately from the admin UI, subject to that mode. External clients are **never auto-provisioned** and **cannot self-create** a tenant — only a platform admin creates a client tenant and assigns it to a Tier-2 user.

**Two-layer enforcement** (so every creation path is covered without scattering checks):

1. **`single` is a data invariant in the service.** `Service.CreateTenant` (`services/service.go`) counts tenants of the kind occupying a provisioning slot via `repository.CountProvisioningSlotsByKind` and returns the sentinel `services.ErrProvisioningLocked` when one already exists. This covers **all** paths — `POST /v1/tenants`, divisions, and lazy provisioning — automatically. The first tenant on a fresh install counts 0 and passes.
2. **The permission gate in the handlers is universal for Tier-1, conditional for Tier-2.** `createTenant` / `createDivision` call `Handler.enforceManualGate`, which resolves `iface.AuthzProvider` from the registry and checks `system.tenants.admin` (empty org = system grant). For `kind == internal` the gate always applies, independent of `ProvisioningMode`; for `kind == external` it applies only when the resolved mode is `manual`. Non-admins get 403; the admin `CreateTenantModal` hits the same `POST /v1/tenants` and passes. The admin division route already requires the permission at the route group, so the shared handler body is a no-op gate for it.
3. **Lazy provisioning honours `external.mode`.** `EnsureTenantForUser` (`services/billing.go`) returns an existing personal tenant unchanged but **refuses to auto-create** one (returns `ErrProvisioningLocked`) when `external.mode != open` — which is the default. A Tier-2 caller with no admin-assigned tenant therefore hits `resolveCallerTenant`, which maps the sentinel to **409** `tenant.provisioning_locked` rather than silently minting a tenant.

Handlers map `ErrProvisioningLocked` → **409** `tenant.provisioning_locked` (`errcode.TenantProvisioningLocked`). The read-only `GET /v1/admin/tenants/provisioning-policy` reports both modes + provisioning-slot counts and backs the policy card on the tenant management pages (the modes themselves are edited at `/admin/modules/tenant`).

**Config-time validation, one policy function on all three surfaces.** The two enforcement layers above run at tenant-*creation* time; `config_validation.go` additionally gates the *config write* itself, before a bad `provisioning.internal.mode` value can even be persisted. `Module` implements both `module.HasConfigValidator` (`ValidateConfig`) and `module.HasConfigActivationValidator` (`ValidateConfigActivation`), and both delegate to the single unexported `validateProvisioningPolicy` — so the active-config PATCH, the named-environment PATCH (which can write an inactive profile), and the active-environment switch (which can activate a profile stored earlier) all apply the identical rule. This closes the gap where a stored legacy `single` profile that is no longer satisfiable could be smuggled in by switching profiles instead of PATCHing the active one; a rejected activation leaves the previously active profile and `needsRestart` untouched (PR 1's `ModuleConfigService` contract).

The policy: `manual` or absent/empty is accepted (absent and legacy values normalise to `manual` at runtime — see above — so there is nothing to gate); `single` is accepted only when `slotCount` (defaults to `svc.CountProvisioningSlotsByKind`, overridden in tests) reports at most one Tier-1 tenant currently occupying a provisioning slot; any other value, **including `open`**, is rejected — `open` was removed from Tier-1 in Task 3.2 and is Tier-2-only. A failure to *count* propagates as a plain wrapped error, never a `*module.ConfigValidationError` — a database outage must not read as a 422 telling the operator their input was bad.

| Code | Condition | HTTP |
|---|---|---|
| `tenant.internal_mode_invalid` (`errcode.TenantInternalModeInvalid`) | `provisioning.internal.mode` is neither `manual`, empty, nor `single` (e.g. `open`, or an unknown value) | 422 |
| `tenant.single_mode_conflict` (`errcode.TenantSingleModeConflict`) | `provisioning.internal.mode == single` while more than one Tier-1 tenant occupies a provisioning slot | 422 |

`ConfigGroups()` splits the two fields onto the full-page rail along the tier boundary: `provisioning.internal` ("Internal provisioning (Tier-1)") holds `provisioning.internal.mode`, `provisioning.external` ("External provisioning (Tier-2)") holds `provisioning.external.mode` — one field per group, the minimum that promotes `/admin/modules/tenant` off the flat-form degradation path. Dropping `ConfigGroups()` entirely reverts the page to the flat single-card form with no other change required — `ConfigSchema()`'s `Group` tags become inert.

## HTTP endpoints

Three route groups, each with a different gate:

### Global — `RequireGlobal()`

| Method | Path | Purpose |
|---|---|---|
| GET | `/v1/tenants` | List the tenants the caller is a member of, each row (`memberDTO`) carrying a derived `isDefault` flag (see [Default tenant invariants](#default-tenant-invariants)) |
| POST | `/v1/tenants` | Create a new tenant — caller becomes owner (`org_owner`). Subject to the provisioning policy (see above): for `kind=internal` a non-admin caller always gets 403 (both modes); for `kind=external` only `manual` mode requires admin. `single` mode additionally 409s once a second Tier-1 tenant would occupy a provisioning slot. |
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

### Platform admin — reads + non-destructive mutations, `RequireSystemPermission("system.tenants.admin")`

Gated globally by a system permission, not by per-org membership, so platform operators can manage every tenant without having to join each one. Powers the frontend `/admin/internal/tenants` (Tier-1) and `/admin/clients` (Tier-2) pages. Registered by `handlers.RegisterAdminRoutes`. No MFA step-up — see the destructive group below for the routes that do require one.

| Method | Path | Purpose |
|---|---|---|
| GET | `/v1/admin/tenants` | List every tenant. Query params: `?kind=internal\|external`, `?parentTenantUUID=<uuid>`, `?rootsOnly=true`, `?includeDeleted=true`, `?q=<text>`, `?includeDeletedUsers=true`. Response includes `memberCount` from a single `$group` aggregation and a derived `isDefault` flag per row (see [Default tenant invariants](#default-tenant-invariants)). When `q` is set the handler routes to `repository.SearchTenantsByQ`, which $lookup-joins `tenant_memberships` → tier-appropriate user collection (`operator_users` for internal, `client_users` for external) and matches `q` (case-insensitive substring) against tenant `name`/`slug` plus member `email`/`fullName`/`username`. Each matching row includes a `matchedMembers` array (≤ `MaxMatchedMembersPerTenant=5`) so the frontend can render "matched: alice@x" chips. `includeDeletedUsers` opts soft-deleted users into the member-side join (default: live users only). |
| GET | `/v1/admin/tenants/{tenantId}` | Get any tenant, with a derived `isDefault` flag |
| PATCH | `/v1/admin/tenants/{tenantId}` | Update any tenant (name, slug, settings) |
| PATCH | `/v1/admin/tenants/{tenantId}/plan` | Change any tenant's plan + features |
| GET | `/v1/admin/tenants/{tenantId}/members` | List members, each row carrying a derived `isDefault` flag for the tenant they belong to |
| DELETE | `/v1/admin/tenants/{tenantId}/members/{userUUID}` | Remove a member |
| GET | `/v1/admin/tenants/{tenantId}/invites` | List invites. Defaults to pending-only; pass `?includeAccepted=true` to also see accepted rows. Raw tokens are scrubbed. |
| POST | `/v1/admin/tenants/{tenantId}/invites` | Create an invite. Raw token returned once, see Key invariants. |
| DELETE | `/v1/admin/tenants/{tenantId}/invites/{inviteId}` | Revoke a pending invite |
| GET | `/v1/admin/tenants/{tenantId}/divisions` | List direct children (depth=1) of an external tenant |
| POST | `/v1/admin/tenants/{tenantId}/divisions` | Create a division (Kind=external, ParentTenantUUID=this). Refuses internal parents. |
| PATCH | `/v1/admin/clients/{tenantId}/billing-identity` | Unified-clients — sets `IsCompany`, `LegalName`, VAT/fiscal codes, billing address, and the FatturaPA routing sub-document on a Tier-2 tenant. All body fields optional; nil leaves the existing value. The data this endpoint writes is what `iface.BillingTenantProvider.ResolveBillingParty` reads at invoice-send time (replaces the deleted `billing.Customer` row). |
| POST | `/v1/admin/clients/{tenantId}/italian-billable` | Unified-clients Phase 1 — flips `Tenant.IsItalianBillable`. Enabling requires a FatturaPA profile carrying `CodiceDestinatario` or `PECDestinatario` (422 otherwise); disabling is unconditional. Send-time validation enforces the same invariant a second time, so the toggle on its own is not load-bearing. |
| GET | `/v1/admin/tenants/provisioning-policy` | Read-only per-tier provisioning policy (internal: `manual`/`single`, fail-closed; external: `open`/`manual`) + provisioning-slot counts. Backs the policy card on the tenant pages; the modes are edited at `/admin/modules/tenant`. See [Provisioning policy](#provisioning-policy-admin-managed). |

### Platform admin — destructive / default-threatening, `RequireSystemPermission("system.tenants.admin")` + `RequireMFA()`

Registered by `handlers.RegisterAdminDestructiveRoutes` and mounted in a second `module.go` route group that additionally requires a session-long MFA step-up (Block B) — a pwd-only token gets 401 `step_up_required`. These are the only three platform-admin tenant operations that either destroy a tenant or move which tenant is the platform default; every other admin route above is a read or a non-destructive mutation and does not require MFA. See [Default tenant invariants](#default-tenant-invariants) for the full HTTP-boundary error mapping.

| Method | Path | Purpose |
|---|---|---|
| PUT | `/v1/admin/tenants/default` | Transfer the platform default Tier-1 tenant. Body `{"tenantId": "<uuid>"}`. 409 `tenant.default_reassignment_required` is not reachable on this route (that guard belongs to the lifecycle mutations below); a non-operational target 409s with a fixed detail instead. |
| DELETE | `/v1/admin/tenants/{tenantId}` | Soft-delete any tenant — bypasses the owner-only check. 409 `tenant.default_reassignment_required` if the target is the platform default. |
| POST | `/v1/admin/tenants/{tenantId}/purge` | Crypto-shred a tenant (irreversible; see purge handler docs). 409 `tenant.default_reassignment_required` if the target is the platform default. |

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

- **Tenant creation obeys the provisioning policy.** Every creation path funnels through `Service.CreateTenant`, which enforces the `single` count invariant as a data rule (so divisions and lazy provisioning are covered, not just `POST /v1/tenants`). The `manual` permission gate lives at the handler boundary. **Both tiers default to `manual`** (admin-only); the first internal tenant is created by the mandatory setup finalization saga (`POST /v1/setup/finalize`, which also persists the chosen Tier-1 mode), and further internal tenants are created deliberately from the admin UI. Full semantics in [Provisioning policy](#provisioning-policy-admin-managed).
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

- **Platform-admin delete bypasses ownership.** The `/v1/admin/tenants/{tenantId}` DELETE route is gated by `system.tenants.admin` (system roles only) plus MFA step-up (`RegisterAdminDestructiveRoutes`). The plain per-tenant DELETE route at `/v1/tenants/{tenantId}` has no owner check today; adding one is pending the "transfer ownership" flow.
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
