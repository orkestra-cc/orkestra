# Migration 0009 — Unique index on authz_bindings + dedup

One-shot `mongosh` migration that dedups `authz_bindings` on `(tenantId, userUUID, roleId)` and then creates that tuple as the collection's **first** unique index.

Written as the deploy prerequisite for the idempotent owner-role-binding ensure (`backend/internal/core/authz/repository/repository.go::EnsureBinding`), part of the tier-1 default-tenant-setup work that makes every tenant-provisioning side effect safe to replay after a lost response, a crashed executor, or an expired lease.

## The failure mode

`authz_bindings` has never carried a unique index — not even on `uuid`. `Repository.CreateBinding` is a plain `InsertOne` with no upsert and no duplicate handling. Nothing today stops the same `(tenantId, userUUID, roleId)` tuple from being inserted twice: replaying tenant creation (or `SetMemberRoles`, or `AttachMember`) re-grants the `org_owner` (or other) binding and a second row lands silently. `GetEffectivePermissions` unions every binding for the pair, so the duplicate has no visible effect on authorization *decisions* — which is exactly why it goes unnoticed. It is still a permanent leak of rows, and proof the grant path was never safe to retry.

## The fix

`(tenantId, userUUID, roleId)` as a unique compound index, **not sparse, no partial filter**. `tenantId == ""` rows are global/system-role grants (`super_admin`, `administrator`, `developer`, `manager`, `operator`, `guest`); the index deliberately covers them too — a user must not hold the same system role twice globally any more than they should hold the same tenant role twice in one tenant. Watch the field-name mismatch: the Go struct field is `RoleUUID`, but its bson tag — and the index key — is `roleId`.

MongoDB refuses to build a unique index over a collection that already contains duplicates, so step 1 dedups first: for every `(tenantId, userUUID, roleId)` tuple with more than one row, it keeps the earliest by `grantedAt` (ties broken by `_id`, which is monotonic) and deletes the rest. Step 2 then creates the index and re-reads it to confirm it landed unique — throwing rather than reporting false success if it didn't.

Declared spec: `backend/internal/core/authz/module.go` (`Collections()`, `CollBindings` block).

## Run order

`module.go`'s boot-time `Collections()` declaration only takes effect through `ensureCollections`, which is **create-only** — it does not retrofit an index onto an existing collection whose spec changed — **and deliberately non-fatal**: a failed `createIndex` (e.g. `E11000` from pre-existing duplicates) is logged as a WARN and the boot continues. On an installation with duplicate bindings already present, that means the unique constraint silently never exists while every health check stays green. Nothing else surfaces the gap.

**Run this migration against every environment before the deploy that ships the `CollBindings` index change — and, per the tier-1 setup-saga design, before the PR that enables the finalize route**, since that route is what replays provisioning steps and relies on this index to make the owner-binding ensure (`EnsureBinding`) actually safe to call twice.

```bash
set -a; . docker/.env; set +a
docker exec -i "${APP_NAME}-mongodb-${ENV}" mongosh --quiet \
  -u "$MONGO_ROOT_USERNAME" -p "$MONGO_ROOT_PASSWORD" \
  --authenticationDatabase admin "$MONGO_DATABASE" \
  < backend/migrations/20260823_authz_bindings_unique.js
```

Idempotent: a second run finds no tuple with more than one row (dedup is a no-op) and finds the index already present under its name (`createIndex` is skipped). The script throws rather than reporting false success if, after the create step, the index is missing or not unique.

## Verification

**Not yet executed against any environment.** Ruling R2 of the task that authored this migration scopes the work to writing the script and this document — running a migration against a live or staging database is an operations action outside that task's worktree. This section will be updated with a before/after index snapshot and duplicate-row count once the migration has actually run, the same way [0008](0008_payments_tx_partial_index.md) records its staging execution.

Before running against a real environment, confirm the duplicate count first with a dry-run read of the same aggregation the script uses:

```javascript
db.authz_bindings.aggregate([
  { $group: { _id: { t: "$tenantId", u: "$userUUID", r: "$roleId" }, n: { $sum: 1 } } },
  { $match: { n: { $gt: 1 } } },
  { $count: "duplicateTuples" },
])
```

A non-zero count is expected and handled — the migration removes exactly those extra rows before creating the index.
