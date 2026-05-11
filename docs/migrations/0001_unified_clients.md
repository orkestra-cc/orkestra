# Migration 0001 — Unified Clients

One-shot Mongo migration that finishes Unified-Client Aggregate **Phase 3**: every `clientbilling_customers` row is folded into the matching personal `Tenant`, ownership of subscriptions / invoices / transactions / payment methods / entitlements is pivoted from `(ownerKind="user")` to `(ownerKind="tenant")`, and the lazy-tenant feature flag is flipped on by default.

The `clientbilling_customers` collection is **not** dropped here — Phase 5 retires the entire `clientbilling` addon once the new shape has soaked. This runbook covers Phase 3 only.

## Binary

`backend/cmd/migrations/0001_unify_clients/`

```bash
# inside the running backend container, or any host with Mongo reachability
go run ./cmd/migrations/0001_unify_clients --dry-run
go run ./cmd/migrations/0001_unify_clients
```

Connection: reads `MONGO_URI` and `MONGO_DATABASE` from the environment, or accepts `--mongo-uri` / `--mongo-db` flags. No other config is loaded — the binary does not require the full `config.Load()` validation chain to pass.

## What the migration does, per row

For every document in `clientbilling_customers`, in `createdAt asc` order:

1. **Sentinel check.** `migrations_applied` is consulted for `{migration: "0001_unify_clients", sourceID: <hex>}`. Hit → skip (idempotent re-runs).
2. **Find the personal tenant.** Predicate is `(kind=external, isCompany=false, signupChannel=self_serve, ownerUserUUID=<userUUID>, deletedAt=null)`. This is the same predicate `EnsureTenantForUser` uses, so any tenant the lazy provisioner already created is reused.
3. **Create one if missing.** The new tenant carries `kind=external, status=active, isCompany=false, signupChannel=self_serve, plan=free, region=eu-west`. Slug is `personal-<userUUID[:8]>-<tenantUUID[:8]>` to avoid collisions with hand-curated tenants. A `tenant_ancestors` self-row at depth 0 is inserted in the same step.
4. **Patch billing identity.** `legalName, vatNumber, fiscalCode, primaryContact.email, stripeCustomerID`, plus the `billingAddress` sub-document, are copied from the source row **only when the tenant value is empty**. We never overwrite a populated tenant field with an empty source value, and we never overwrite a populated tenant field with a different non-empty source value (operator-curated wins).
5. **Ensure membership.** A `tenant_memberships` row is inserted for the owner with `roles=["org_owner"], isOwner=true`. Idempotent: a re-run after a partial failure that already wrote the membership is a no-op.
6. **Pivot owner rows.** `updateMany({ownerKind:"user", ownerUUID:<userUUID>}, {$set:{ownerKind:"tenant", ownerUUID:<tenantUUID>}})` is run against:
   - `subscriptions_subscriptions`
   - `subscriptions_invoices`
   - `payments_transactions`
   - `payments_payment_methods`
   - `tenant_entitlements`
7. **Stamp sentinel.** `migrations_applied` is upserted with the per-collection pivot counts.

`isCompany=true` source rows are intentionally migrated as `isCompany=false` — the personal-tenant predicate fixes that bit. Operators promote a personal tenant to a company through `PATCH /v1/admin/clients/{tenantUUID}/billing-identity` after the migration has soaked.

## Order of operations (run book)

```
┌─ Pre-flight ────────────────────────────────────────────────────────────────┐
│ 1. Snapshot Mongo. Phase 3 is reversible up to the point the sentinel       │
│    table is purged, but a snapshot is the only safety belt for unrelated    │
│    rows that get touched by future operator activity during the cutover.    │
│ 2. Confirm Phase 1 + Phase 2 are deployed:                                  │
│    grep -n "Phase 1" backend/internal/core/tenant/services/billing.go       │
│    grep -n "lazyTenantProvisioning" backend/internal/addons/payments/...    │
│ 3. Verify nothing is currently writing clientbilling rows during the cut.   │
│    The renewal job re-reads clientbilling on each tick, so a quiet window   │
│    of ~5 minutes is enough; no DB lock is needed.                           │
└─────────────────────────────────────────────────────────────────────────────┘

┌─ Staging ───────────────────────────────────────────────────────────────────┐
│ 4. docker compose -f docker-compose.staging.yml exec backend                 │
│      go run ./cmd/migrations/0001_unify_clients --dry-run                   │
│ 5. Inspect the JSON log: rows / tenantsCreated / pivots fields make sense?  │
│ 6. Run for real:                                                             │
│      docker compose -f docker-compose.staging.yml exec backend               │
│        go run ./cmd/migrations/0001_unify_clients                           │
│ 7. Spot-check 2–3 users:                                                    │
│      db.tenants.findOne({ ownerUserUUID: "...", isCompany: false })          │
│      db.subscriptions_subscriptions.find({ ownerUUID: "<tenantUUID>" })      │
│      db.migrations_applied.find({ migration: "0001_unify_clients" })         │
│ 8. Soak 24h. Renewal job + checkout flows continue against the new shape.   │
└─────────────────────────────────────────────────────────────────────────────┘

┌─ Production ────────────────────────────────────────────────────────────────┐
│ 9. Snapshot prod Mongo.                                                      │
│ 10. Repeat steps 4–7 against prod.                                          │
│ 11. Confirm the lazy-tenant flag default is in effect (true) by reading    │
│     the deployed config — no env override should set it to false.           │
└─────────────────────────────────────────────────────────────────────────────┘
```

## Verification queries

```javascript
// Source rows minus completed sentinels — should be 0 after a clean run.
db.clientbilling_customers.aggregate([
  {$lookup: {
    from: "migrations_applied",
    let: { sid: { $toString: "$_id" } },
    pipeline: [{ $match: { $expr: {
      $and: [
        { $eq: ["$migration", "0001_unify_clients"] },
        { $eq: ["$sourceID", "$$sid"] }
      ]}}}],
    as: "done"
  }},
  {$match: { "done.0": { $exists: false }}},
  {$count: "uncompleted"}
])

// Any leftover user-owner rows in the five collections — should be 0.
for (const c of ["subscriptions_subscriptions","subscriptions_invoices",
                  "payments_transactions","payments_payment_methods",
                  "tenant_entitlements"]) {
  print(c, db.getCollection(c).countDocuments({ ownerKind: "user" }))
}
```

## Rollback

This phase is reversible only by restoring the Mongo snapshot. The migration **does not delete** the source `clientbilling_customers` rows — they remain readable for the duration of Phase 3+4 so a partial revert (re-pointing ownerKind back to `user`) is possible by inverting the same `updateMany` shape against the sentinel table.

`UNIFIED_CLIENTS_LAZY_TENANT_ENABLED=false` reverts the runtime feature flag without touching data. Combined with a snapshot restore that's the full revert path.

## Notes for Phase 4 / 5

- Phase 4 collapses `Owner{Kind, UUID}` into bare `tenantUUID`. After Phase 4 ships, the `ownerKind` field disappears entirely — no source rows remain to migrate, so the verification queries above stop applying.
- Phase 5 deletes the `clientbilling` addon, drops `clientbilling_customers` and `billing_customers`, and decommissions this binary. Drop the binary with the same PR that drops the addon — the two have to land together.
