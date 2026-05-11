# Migration 0003 — Billing Customer → Tenant.FatturaPA

One-shot Mongo migration that finishes Unified-Client Aggregate **Phase 5**: every `billing_customers` row is folded into the matching Tier-2 `Tenant` aggregate (FatturaPA sub-document + `IsItalianBillable` + identity patch), and `billing_invoices.tenantUUID` is backfilled from the per-row `customerId` lookup so the rewired invoice send path (`iface.BillingTenantProvider.ResolveBillingParty`) can resolve a CessionarioCommittente without the deleted `billing.Customer` Go surface.

The same release deletes the `clientbilling` addon entirely and the `billing.Customer` Go model + repo + service + handler + routes. Sources of truth for FatturaPA recipient identity are now the tenant aggregate fields populated by `PATCH /v1/admin/clients/{tenantUUID}/billing-identity`.

## Binary

`backend/cmd/migrations/0003_billing_customer_to_tenant/`

```bash
# inside the running backend container, or any host with Mongo reachability
go run ./cmd/migrations/0003_billing_customer_to_tenant --dry-run
go run ./cmd/migrations/0003_billing_customer_to_tenant
```

Connection: reads `MONGO_URI` and `MONGO_DATABASE` from the environment, or accepts `--mongo-uri` / `--mongo-db` flags. No other config is loaded — the binary stays independent of the full `config.Load()` validation chain.

## What the migration does

Per-row sentinels in `migrations_applied` (`{migration: "0003_billing_customer_to_tenant", customerUUID: <uuid>}`) gate each customer's processing — the migrator resumes deterministically from any partial run.

For every row in `billing_customers` (sorted by `createdAt` asc, soft-deleted rows skipped):

1. **Resolve the tenant.**
   - `customer.tenantUUID` set → load that tenant.
   - `customer.tenantUUID` empty → mint a fresh admin-flagged external tenant from the customer row (`Kind=external`, `Status=active`, `IsCompany=customer.IsCompany`, identity + address pre-filled, `memberCount=0`). The new tenant has no membership; operators attach a user via the admin UI later if needed.
2. **Patch identity.** Additive only: never overwrites a populated tenant field with empty. Propagates `LegalName`, `VATNumber` (only when `customer.fiscalIdCountry=IT`), `FiscalCode` (codice fiscale), `primaryContact.email`, `billingAddress.{line1,city,province,postalCode,country}`. Promotes `IsCompany` `false → true` when the source row is a corporate entity (the reverse direction is never applied).
3. **Install FatturaPA.** When the customer row carries at least one routing handle (`CodiceDestinatario` or `PECDestinatario`), the migrator stamps the full FatturaPA sub-document onto the tenant and flips `IsItalianBillable=true`. Re-runs heal partial state — the FatturaPA write is unconditional, not "only-when-empty".
4. **Backfill invoice tenantUUID.** `db.billing_invoices.updateMany({customerId: <uuid>, tenantUUID: <empty>}, {$set: {tenantUUID: <resolved>}})`. The filter is empty-tenantUUID so re-runs are no-ops.

The sentinel records the resolved tenantUUID + invoice count so an auditor can reconstruct what each row produced.

## Order of operations (run book)

```
┌─ Pre-flight ────────────────────────────────────────────────────────────────┐
│ 1. Snapshot Mongo. Phase 5 deletes the billing.Customer Go surface; the    │
│    only rollback past this migration is restoring the snapshot.             │
│ 2. Confirm Phases 1–4 sentinels are present in migrations_applied:          │
│      0001_unify_clients (per-row sentinels)                                 │
│      0002_collapse_owner_to_tenant_uuid (single sentinel)                   │
│    Running 0003 against a fleet that hasn't completed earlier phases is     │
│    undefined — Tenant.FatturaPA / IsItalianBillable / ParentTenantUUID      │
│    fields the migrator inspects only land in their final shape after       │
│    Phase 1.                                                                 │
│ 3. Verify the backend binary already includes the Phase 5 Go changes      │
│    (invoice service consuming iface.BillingTenantProvider, billing.        │
│    Customer + clientbilling addon deleted). The migration is harmless      │
│    against the old binary, but the new invoice send path needs the         │
│    backfilled tenantUUID + populated Tenant.FatturaPA before it can         │
│    successfully snapshot a CessionarioCommittente.                         │
└─────────────────────────────────────────────────────────────────────────────┘

┌─ Staging ───────────────────────────────────────────────────────────────────┐
│ 4. docker compose -f docker-compose.staging.yml exec backend                │
│      go run ./cmd/migrations/0003_billing_customer_to_tenant --dry-run      │
│ 5. Inspect the JSON log lines: rows count should equal                      │
│    db.billing_customers.countDocuments({deletedAt: null}). TenantsCreated   │
│    is the count of customers with no tenantUUID link — for fleets that      │
│    seeded customers exclusively via the Tier-2 self-onboarding path that    │
│    number should be 0.                                                       │
│ 6. Run for real:                                                            │
│      docker compose -f docker-compose.staging.yml exec backend              │
│        go run ./cmd/migrations/0003_billing_customer_to_tenant              │
│ 7. Spot-check a few tenants. Pick one that previously had a linked          │
│    customer:                                                                │
│      mongosh "$MONGO_URI/$MONGO_DATABASE"                                   │
│        db.tenants.findOne({uuid: "<tenantUUID>"},                           │
│          {fatturaPA: 1, isItalianBillable: 1, legalName: 1, isCompany: 1}) │
│        db.billing_invoices.countDocuments({tenantUUID: "<tenantUUID>"})    │
│ 8. End-to-end smoke: open the issued-invoices admin page, create a draft   │
│    invoice picking the same Tier-2 client, click "Send to SDI". The        │
│    request should succeed (in sandbox) — proof the new send path resolves   │
│    a CessionarioCommittente from the tenant.                                │
└─────────────────────────────────────────────────────────────────────────────┘

┌─ Production ────────────────────────────────────────────────────────────────┐
│ 9. Schedule a maintenance window. Operators creating new invoices against  │
│    not-yet-migrated customers will hit ErrTenantNotBillable (HTTP 422)      │
│    until the migration completes — accept that brief degradation.           │
│10. Snapshot Mongo (mandatory; the only rollback path).                      │
│11. Re-run the dry-run once more and compare counts to staging.              │
│12. Run for real. Wall time scales linearly with billing_customers row       │
│    count; typical fleets complete in under 60 seconds.                      │
│13. Restart the backend. Verify GET /healthz then create + send a smoke-    │
│    test invoice against a real customer-to-tenant pair.                     │
│14. Watch sales/CS channels for ErrTenantNotBillable surfacing in the SPA   │
│    over the next 24h — that signals a customer row that didn't carry a      │
│    routing handle and operators need to fill the FatturaPA profile via     │
│    /admin/clients/:tenantUUID/billing-identity.                             │
└─────────────────────────────────────────────────────────────────────────────┘
```

## Rollback

There is no automatic rollback. If a problem surfaces:

1. Stop the backend.
2. Restore the pre-migration Mongo snapshot.
3. Re-deploy the previous backend binary (Phase 4 era) so `billing.Customer` is back and the invoice service expects `customerId` instead of `tenantUUID`.
4. Investigate the issue against staging before retrying.

The reverse rewrite is technically possible (clear `tenant.fatturaPA`, clear `tenant.isItalianBillable`, clear `billing_invoices.tenantUUID`) but recovering the exact pre-migration tenant state — including which IsCompany value and which identity fields existed beforehand — is not possible without the snapshot. Always snapshot first.

## After

Once Phase 5 has soaked in production for at least one full billing cycle (typically 1 week is sufficient — long enough for monthly subscriptions to renew and for any operator to flag billing-identity issues):

- Run a follow-up migration to drop the `billing_customers` and `clientbilling_customers` collections entirely. Both are unused at this point — Phase 1's `0001_unify_clients` already pivoted clientbilling, and Phase 5's `0003` folded billing customers into tenants. The collections are kept around as a forensics safety belt for one phase only.
- Drop the legacy `customerId` field from `billing.Invoice` (BSON `customerId`) — it remains as a deprecated read-only field on the model after Phase 5 to support forensics. Once the cleanup migration runs the backend stops writing to the field; a subsequent Mongo `$unset` strips it from existing rows.
- Phase 6 picks up next: frontend admin URL merge (collapses `/admin/clients/:userId` and `/admin/external-tenants/:tenantId` into the unified `/admin/clients/:tenantUUID`).
