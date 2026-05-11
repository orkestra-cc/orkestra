# Migration 0002 — Collapse Owner→TenantUUID

One-shot Mongo migration that finishes Unified-Client Aggregate **Phase 4**: the polymorphic `(ownerKind, ownerUUID)` field pair is renamed to a single `tenantUUID` across the six owner-scoped collections, completing the Go-struct rename that landed in the same release.

After Phase 3, every persisted row already carried `ownerKind="tenant"` (user-owned rows were pivoted to point at the personal tenant). This migration drops the now-redundant kind discriminator and renames the value field, lining the on-disk shape up with the Go models so the backend boots cleanly.

## Binary

`backend/cmd/migrations/0002_collapse_owner_to_tenant_uuid/`

```bash
# inside the running backend container, or any host with Mongo reachability
go run ./cmd/migrations/0002_collapse_owner_to_tenant_uuid --dry-run
go run ./cmd/migrations/0002_collapse_owner_to_tenant_uuid
```

Connection: reads `MONGO_URI` and `MONGO_DATABASE` from the environment, or accepts `--mongo-uri` / `--mongo-db` flags. No other config is loaded — the binary stays independent of the full `config.Load()` validation chain.

## What the migration does

A single sentinel record in `migrations_applied` (`{migration: "0002_collapse_owner_to_tenant_uuid"}`) gates the run. When the sentinel is missing, the binary iterates the six target collections in this order:

1. `subscriptions_subscriptions`
2. `subscriptions_invoices`
3. `subscriptions_activity`
4. `payments_transactions`
5. `payments_payment_methods`
6. `tenant_entitlements`

For each collection, the rewrite is a single `updateMany` call:

```js
db.<collection>.updateMany(
  { ownerUUID: { $exists: true } },
  [
    { $set:   { tenantUUID: "$ownerUUID" } },
    { $unset: ["ownerKind", "ownerUUID"] }
  ]
)
```

The aggregation-pipeline form lets `$set` and `$unset` run atomically per document. The `ownerUUID: {$exists: true}` filter naturally skips already-renamed rows, so a re-run after a partial failure is safe — the loop short-circuits at the first error and the sentinel stays unstamped, so the next invocation resumes cleanly.

Once every collection finishes without error, the binary stamps `migrations_applied` with the per-collection matched/modified counts. The sentinel record carries the audit trail; future invocations short-circuit on first lookup.

## Order of operations (run book)

```
┌─ Pre-flight ────────────────────────────────────────────────────────────────┐
│ 1. Snapshot Mongo. The rename is destructive — Phase 3 already pivoted     │
│    user→tenant ownership, so a rollback past Phase 4 means restoring the    │
│    snapshot.                                                                │
│ 2. Confirm Phase 3 (0001_unify_clients) sentinel is present in              │
│    migrations_applied. 0002 expects every row already carries               │
│    ownerKind="tenant" + ownerUUID=<tenantUUID>; running it before 0001      │
│    completes will silently rewrite user-owned ownerUUIDs as tenantUUIDs,    │
│    breaking the entitlement projection.                                     │
│ 3. Verify the backend binary already includes the Phase 4 Go-struct        │
│    rename (Subscription.TenantUUID, Transaction.TenantUUID, etc). The       │
│    backend will refuse to read renamed rows from the old struct shape, so   │
│    the migration must land alongside (or after) the deploy.                 │
└─────────────────────────────────────────────────────────────────────────────┘

┌─ Staging ───────────────────────────────────────────────────────────────────┐
│ 4. docker compose -f docker-compose.staging.yml exec backend                │
│      go run ./cmd/migrations/0002_collapse_owner_to_tenant_uuid --dry-run   │
│ 5. Inspect the JSON log line per collection. Counts should match the row    │
│    counts you grepped pre-flight.                                           │
│ 6. Run for real:                                                            │
│      docker compose -f docker-compose.staging.yml exec backend              │
│        go run ./cmd/migrations/0002_collapse_owner_to_tenant_uuid           │
│ 7. Spot-check one row per collection:                                       │
│      mongosh "$MONGO_URI/$MONGO_DATABASE"                                   │
│        db.subscriptions_subscriptions.findOne({ tenantUUID: { $exists: true }})    │
│        db.subscriptions_subscriptions.findOne({ ownerUUID: { $exists: true }})     │
│    The first should return a hit; the second should return null.            │
│ 8. Restart the backend so Mongo's index builder re-runs against the new     │
│    Collections() spec. The (tenantUUID, status) and (tenantUUID,            │
│    capabilityId) indexes are auto-created by the registry.                  │
└─────────────────────────────────────────────────────────────────────────────┘

┌─ Production ────────────────────────────────────────────────────────────────┐
│ 9. Maintenance window: subscription renewals run on a 1h tick — schedule    │
│    the migration in the gap between ticks so a renewal is never racing the  │
│    rename.                                                                  │
│10. Snapshot Mongo (mandatory; the only rollback path).                      │
│11. Run the dry-run once more and compare counts to staging to flag drift.   │
│12. Run for real. Total wall time scales with the smallest of the six        │
│    collections — typical fleet sizes complete in under 30 seconds.          │
│13. Restart the backend. Verify boot health: GET /healthz, then a smoke      │
│    test against /v1/me/subscriptions on a real Tier-2 user.                 │
│14. Drop the sentinel only when you're certain Phase 4 is permanent. The     │
│    record is small and serves as the only durable proof the rename ran.    │
└─────────────────────────────────────────────────────────────────────────────┘
```

## Rollback

The migration writes via aggregation pipeline — there is no automatic rollback. If a problem surfaces:

1. Stop the backend.
2. Restore the pre-migration Mongo snapshot.
3. Re-deploy the previous backend binary (Phase 3 era) so the Go structs match the pre-rename on-disk shape.
4. Investigate the issue against staging before retrying.

A targeted reverse rewrite is possible (`$set: {ownerUUID: "$tenantUUID", ownerKind: "tenant"}, $unset: ["tenantUUID"]`) but only if Phase 3 data is fully intact — there is no separate sentinel to detect partial Phase 4 progress in that direction. Snapshot restore is the recommended path.

## After

Once Phase 4 has soaked in production for at least one renewal cycle (24h is typical):

- Drop the unused secondary indexes the old Collections() spec built (`(ownerKind, ownerUUID, status)` etc) — the registry only re-creates the new indexes on boot, but it does not auto-drop the old ones.
- Phase 5 picks up next: it deletes the `clientbilling` addon entirely and migrates `billing.Customer` rows onto `Tenant.FatturaPA`. See `0003_billing_customer_to_tenant.md` (when written).
