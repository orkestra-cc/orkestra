# Orkestra setup — first-install bootstrap and the finalization saga

_Path: `/backend/internal/shared/setup`_
_Parent: [../../../CLAUDE.md](../../../CLAUDE.md)_

In-tree package of the single backend Go module, imported as
`github.com/orkestra/backend/internal/shared/setup`. Not a `Module`: it is
shared bootstrap infrastructure wired directly in `cmd/server/main.go`,
before and outside the module registry's route pass.

It owns four endpoints and the resumable saga that finishes a fresh
install. Coordinator state lives in
[`../systeminit`](../systeminit), which is the sole owner of the
`system_init` collection.

## The four endpoints

| Route | Auth | Purpose |
| --- | --- | --- |
| `GET /v1/setup/status` | **public** | authoritative phase + SMTP hint |
| `POST /v1/setup/admin` | **public** | create the first administrator |
| `GET /v1/setup/finalization-access` | operator, `RequireAuth` | read-only "may I finalize?" probe |
| `POST /v1/setup/finalize` | operator, `RequireAuth` | the finalization saga |

Three persistent phases, in `Status`: `admin_required` (no users) →
`tenant_required` (users exist, coordinator not complete) → `complete`
(coordinator has `completedAt`). `SetupCompleted` on the wire is *derived*
from `Phase == complete`, never computed independently.

## Invariants

### Route registration is a security boundary, not a naming convention

`RegisterPublicRoutes` and `RegisterProtectedRoutes` are separate methods
on purpose, and `main.go` mounts them on **different** Huma APIs — the
protected one built on a router already behind `RequireAuth`.

**The `/v1/setup/` entry in `shared/middleware.PublicRoutes` is a
tenant-baggage / span-coverage exemption, NOT a statement that everything
under the prefix is anonymous.** It is correct because no tenant exists yet
during bootstrap, authenticated or not. Never add a route to the public
registrar because it shares the prefix. `routes_mount_test.go` pins this:
status/admin answer anonymously, the other two answer `401` anonymously
and reject a client-audience token, and the public registrar's generated
OpenAPI contains exactly two operations.

Finalization deliberately does **not** require step-up MFA: a
just-created administrator has no second factor enrolled yet. Its
authorization comes from the incomplete setup phase plus the coordinator's
bound administrator identity.

### Fail closed on every authoritative read

`Status` returns `(Status{}, error)` — never an inferred phase — when the
user count or the coordinator read fails; the handler maps that to
`503 setup.status_unavailable` with `Retry-After`. Same rule for the
finalizer state (`503 setup.finalizer_state_unavailable`). This is
load-bearing: "setup is incomplete" is exactly the state that unlocks the
anonymous bootstrap endpoints, so a database outage must never be able to
imply it. `SMTPConfigured` is the one field allowed to degrade to `false`
on its own read failure — it controls no phase, authorization, or creation.

### The two `system_init` records have separate lifecycles

- `key: first_admin` — the rollback-capable CAS for the first `super_admin`
  seat. `Release(userUUID)` can delete **only** this record after a failed
  user create.
- `key: setup_finalization` — the persistent coordinator: bound admin,
  reserved tenant UUID, normalized name/slug, mode, request hash, stage,
  revision, leases, result snapshot. **Nothing deletes it**, and
  `Release()` must never be able to reach it.

`CreateInitialAdmin` binds the coordinator from `TokenResponse.User.ID`
(`iface.UserManagementResponse` exposes the UUID as `id`) via
`InitializeFresh`, which is `$setOnInsert`-only so a legacy or recovered
record is never clobbered. It must **never** read the identity back from
the `first_admin` sentinel.

### Finalizer access and recovery

A bound `adminUUID` is **usable** only when the operator user exists, is
not soft-deleted, and is active. That classification comes from the narrow
`iface.UserLifecycleStateProvider` (`active|inactive|deleted|missing`),
type-asserted from the wired `UserProvider` at construction; a nil provider
fails closed exactly like a database error.

`evaluateAccess` is the single shared authorization seam for the probe and
the POST. It **never mutates** — only the POST performs the atomic claim.
Rules:

- usable binding → only that UUID may finalize;
- empty binding, or missing/deleted/inactive → an authenticated **active
  `super_admin`** may claim recovery, nobody else;
- a lookup error is never a lifecycle class and never opens recovery;
- the claim is a CAS on the previously observed `(adminUUID, revision)`. A
  loser re-evaluates access **once** rather than overwriting the winner;
- a legacy install can reach "may claim recovery" with **no record at
  all** — the claim path creates it (`EnsureRecord(legacy_recovery)`,
  `$setOnInsert`) and CASes against whatever exists.

Neither the probe response nor any error string may expose the bound
administrator's UUID, email, name, or lifecycle state.

### The saga: lease + CAS

Four idempotent stages driven by the coordinator record:

1. `StageConfig` — persist `provisioning.internal.mode` through
   `ModuleConfigService.UpdateConfig` (which runs the tenant module's
   validator, so a persisted mode is valid by construction);
2. `StageTenant` — `EnsureSetupTenant(reservedUUID, …)`;
3. `StageDefault` — `AssignDefaultTenant(reservedUUID, …, "setup")`;
4. `StageFinish` — `Complete` writes the result snapshot and `completedAt`.

Each execution takes an opaque owner UUID and claims **only the current
incomplete stage**, with a CAS filtered by key, request hash, stage,
revision, and an absent-or-expired lease; it advances with a second CAS on
the same owner, stage and revision.

**The four rules that must survive every edit:**

- **Stage completion is judged ONLY by the CAS-advanced record — never by
  having held a lease.** Every loop iteration re-reads the record and
  drives from `rec.Stage`. A lost claim, a lost renewal and a lost advance
  all fall back to the same place: reread and re-derive.
- **A CAS answers "did my filter match?", not "did the document
  change?"** — so `systeminit.RenewLease` returns `MatchedCount`, not
  `ModifiedCount`. MongoDB reports `modifiedCount=0` for a matched
  document whose `$set` stores byte-identical values, and the renewal is
  a pure refresh of `leaseUntil` + `updatedAt`: claim and renew inside one
  millisecond and the write changes nothing. Reading that as a lost lease
  made the executor loop, find its **own** live lease blocking the
  re-claim, and answer `202` in-progress to the request it was in the
  middle of completing — a flake that only appeared where the round trip
  was fast enough to fit the pair inside a millisecond. Pinned by
  `TestRenewLease_UnchangedWriteStillReportsOwnership`.
- **The lease prevents routine double execution; it is NOT an
  exactly-once boundary.** An external effect can succeed in the instant
  before its executor loses the lease or crashes, so every stage body must
  stay correct under overlap and at-least-once replay.
- **The lease TTL must exceed the timeout of any single external call**
  (currently 30s; the slowest stage is `StageTenant`, which fans out to
  tenant/KMS/closure/membership/authz writes). It is renewed between a
  stage's external effect and its advance CAS.

Request state table (after authentication and coordinator authorization):

| Phase | Reservation/hash | Result |
| --- | --- | --- |
| `tenant_required` | nothing reserved | reserve (pre-minted UUIDv7) + execute |
| `tenant_required` | matching hash, live lease | `202` in-progress + `Retry-After` |
| `tenant_required` | matching hash, no live lease | resume the first incomplete stage |
| `tenant_required` | different hash | `409 setup.finalization_already_started` |
| `complete` | matching hash, authorized caller | `200` from the persisted snapshot, zero side effects |
| `complete` | different or missing hash | `409 setup.already_completed` |

The hash is `sha256` over the **normalized** `(name, slug, mode)` tuple, so
a payload differing only in whitespace or slug case is the same request and
replays; the mode participates, so switching `manual`↔`single` is a
different request, not a retry. An install completed by **migration** has
no client request hash, so every later POST gets `already_completed`.
The bound-admin check runs **before** the hash check on the
post-reservation path: a reservation lost to a binding takeover leaves the
hash empty, where "a different request is already reserved" would be false.

### The 202 is a success, not an error

`{"state":"setup.finalization_in_progress"}` plus `Retry-After` is returned
as a **successful** `202` — the handler returns `(output, nil)` and Huma's
error path is never entered. That state string is deliberately **not** an
`errcode` constant and has **no** `goldenCodes` row: the
`{status,title,detail,code}` Problem Details shape stays exclusive to error
responses. Do not add a constant for it.

Because the operation hand-writes its `Responses` map to document the 202,
it must also hand-write the `default` → `application/problem+json` →
`ErrorModel` entry: huma's `defineErrors` only injects that automatically
when an operation has at most one response and no declared `Errors`, so a
populated map suppresses it.

### shared/setup imports no tenant package

The tenant service is reached only through the local structural seam
`SetupTenantEnsurer`. Mode strings, the module-config key, the default
assignment source and the internal tenant-kind literal are **local
constants pinned by value** to the tenant module's, with comments saying so
and a tripwire test.

Tenant failures cross the boundary as `*SeamError{Kind}`
(`slug_conflict` → `409 tenant.slug_already_in_use`, `provisioning_locked`
→ `409 tenant.provisioning_locked`, `identity_conflict` → `500`,
`remediation` → `409`). The translation lives in
`cmd/server/setup_tenant_adapter.go`, which may legally import both
packages and wraps the original error with a second `%w` so logs keep the
cause. **Never** match a tenant sentinel or its error text here.

`EnsureSetupTenant` takes a trailing **coordinator attestation**: the saga
states that the coordinator record for *this* reservation exists and is not
completed (derived from the record, not hardcoded). The tenant seam gates
its restore-a-soft-deleted-reserved-row branch on it — see
[`../../core/tenant/CLAUDE.md`](../../core/tenant/CLAUDE.md) for why the row
signature alone is not enough.

**The attestation is only half the proof, and it is worth being precise
about what it does NOT say.** It states that a reservation is *open*; it
says nothing about how the row came to be deleted. Since no backend
middleware gates the platform-admin routes during setup (the `SetupGate` is
a frontend component), an operator can delete the reserved tenant while the
reservation is still open — so the tenant seam additionally requires the
row's own persisted deletion provenance to be `provisioning_rollback`.
Do not "simplify" the seam by dropping either half.

### Wiring

`main.go` constructs one `systeminit.Repo` and registers it under both
`ServiceFirstAdminClaimer` and `ServiceSetupFinalizationStore`; the setup
service receives the `FinalizationStore` interface explicitly. Every seam
is resolved with `MustGetTyped` — **missing required wiring must fail
module initialization**, not degrade a bootstrap endpoint at runtime. The
audit sink is the single exception: it belongs to the compliance module and
is nil-tolerated (emits are skipped).

Tests replace `FinalizationStore` with a fake rather than constructing
Mongo; `finalize_integration_test.go` (gated on `MONGO_TEST_URI`) runs the
saga against the real repo for the concurrency and lease-expiry cases.

## Error codes

Declared in `shared/errcode` (each with its exact `goldenCodes` row):
`setup.status_unavailable` (503), `setup.finalizer_state_unavailable`
(503), `setup.finalizer_bound_to_another_admin` (403),
`setup.recovery_requires_super_admin` (403),
`setup.finalization_already_started` (409), `setup.already_completed`
(409), `setup.tenant_name_required` (422), `setup.tenant_slug_required`
(422). Both 503s carry `Retry-After` and `Cache-Control: no-store`; the
client retries the identical payload and the saga resumes where it stopped.

**The two 422s validate the NORMALIZED payload, and must stay in the
service rather than the schema.** `minLength:"1"` constrains the raw
string, so `"   "` satisfies it and `normalizeFinalize` then collapses it
to `""`; nothing downstream re-checks (`createTenantWithUUID` only
`TrimSpace`s what it is handed). Without the check setup completes against
a nameless Tier-1 organization — and because the reservation hash is
computed over the normalized tuple, every whitespace-only variant hashes
identically and replays as "the same request". The guard runs **before the
reservation**, so a rejected payload leaves no coordinator state behind.

Client-facing details are **fixed written sentences** — never `err.Error()`.
Stage failures return a server error **without** marking setup complete, and
the log names the failed stage without request payload or secrets.

## Audit

- `setup.completed` — emitted **only** by the executor that won the
  `Complete` CAS (so exactly once), actor = bound admin, metadata
  `{tenantUUID, mode}`.
- `setup.finalizer.recovered` — emitted only on a **won** recovery CAS,
  actor = the winning super_admin, metadata `{previousAdminUUID (omitted
  when the binding was empty), reason, newAdminUUID}` where reason ∈
  `{missing, deleted, inactive}` and is **never** a lookup error. Metadata
  stays minimal: no name, email, token, or profile snapshot.
