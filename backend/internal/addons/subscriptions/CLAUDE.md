# Module: Subscriptions

_Path: `/backend/internal/addons/subscriptions`_
_Parent: [../../../CLAUDE.md](../../../CLAUDE.md)_

Recurring-revenue core: a catalog of AI services the operator sells and subscriptions binding Tier-2 external tenants to those services with cycle-based billing and an append-only activity log.

## Responsibility split

- **Subscriptions owns**: what's for sale, which tenant bought it, when the next charge is due, and an audit trail. No gateway code.
- **Payments owns**: Stripe (and later PayPal) API calls + webhook verification. Subscriptions consumes it through `iface.PaymentProvider`.
- **Billing (FatturaPA)** is NOT involved in v1 — subscription invoices are internal receipts (number format `SUB-YYYY-NNNNNN`), separate from fiscal compliance. Future integration with `billing` for Italian B2B clients is deferred.

## Cycle-free wiring with `payments`

Neither module declares the other in `Dependencies()`. `subscriptions` resolves `iface.PaymentProvider` lazily from `ServiceRegistry` on every charge (see `services/renewal_service.go:paymentProvider`). `payments` does the same for `iface.SubscriptionReconciler`. Because both registrations complete during `Init()` and lookups happen during `Start()` / HTTP / ticker, there is no init-time ordering constraint.

If `payments` is disabled, the renewal job still generates invoices but marks them `awaiting_manual_payment` and emits `manual_payment_required` activity — the workflow keeps running so manual bank transfers can be reconciled by editing the invoice status.

## State machine (`models.SubStatus`)

```
active ──charge_failed──▶ past_due ──retry_succeeded──▶ active
                              │
                              └──max_failures (3)──▶ suspended
active | past_due | suspended ──admin_cancel──▶ cancelled (terminal)
active ──cancel_at_period_end─▶ (ends at CurrentPeriodEnd)─▶ cancelled
```

Transitions happen in `services/subscription_service.go` (user actions) and `services/renewal_service.go` (charge outcomes). The reconciler (webhook path) transitions on async events and **is idempotent** — receiving the same Stripe event twice is a no-op.

## Renewal job

`jobs/renewal_job.go` tickles every `SUBSCRIPTIONS_RENEWAL_INTERVAL` (default 1h). Each tick: find subscriptions with `NextBillingAt <= now` AND `status ∈ {active, past_due}`, generate an invoice, attempt a charge, update state. Failed charges push `NextBillingAt` out by 1 day so we retry tomorrow, not every tick.

## Owner scope (post-onboarding refactor)

Subscriptions are owned by a polymorphic `iface.Owner{Kind, UUID}`:

- `Kind="user"` — self-registered clients hold subscriptions on their own user UUID. Default for the `POST /v1/me/subscriptions` self-service path.
- `Kind="tenant"` — admin-attached business members (or admin-created subscriptions) hold subscriptions on a tenant UUID. The legacy tenant-only model is now this case.

`SubscriptionService.Create(ctx, owner, serviceUUID, tierCode, actor)` is the operator-admin entry point; `CreateForOwner(ctx, owner, serviceCode, tierCode, actor)` is the self-service variant that resolves the service by its human-typed SKU code.

Operator-admin routes (service catalog, subscription admin, invoice/activity reads) sit behind `RequireInternalTenant()`. The self-service `POST /v1/me/subscriptions` accepts `ownerKind` (defaults to `"user"`); when `"tenant"`, the handler enforces ownership via `TenantProvider.ListUserMemberships` against the supplied `tenantUuid`.

A thin `TenantSubscriptionAdapter` (`services/tenant_provider.go`) implements `iface.TenantSubscriptionProvider` so `core/tenant` can serve `GET /v1/admin/tenants/{id}/subscriptions` — the adapter filters by `Owner{Kind:"tenant", UUID:tenantUUID}`.

**User-owner renewals (Phase 2):** `RenewalService.loadOwnerTenant` resolves user-owned subscriptions through the clientbilling addon's `iface.UserBillingCustomerProvider` and projects the user's profile onto a synthetic `*iface.Tenant` so the rest of the renewal pipeline stays owner-agnostic. The Stripe-customer-id write is dispatched per-owner-kind (tenants → `TenantProvider.SetTenantStripeCustomerID`, users → `UserBillingCustomerProvider.SetStripeCustomerID`). When the clientbilling addon is disabled, user-owner renewals fail fast with `"user billing provider not configured"`; tenant-owner renewals are unaffected.

## Collections

| Collection | Notes |
|---|---|
| `subscriptions_services` | Catalog items with pricing tiers. `code` is unique. |
| `subscriptions_subscriptions` | Cycle state. `(ownerKind, ownerUUID, status)` for owner-scoped reads, `(nextBillingAt, status)` for the renewal scan, `serviceUUID` for catalog joins. |
| `subscriptions_invoices` | Generated per cycle. `(subscriptionUUID, periodStart)` unique prevents double-billing the same cycle. Carries `(ownerKind, ownerUUID)` denormalized from the parent subscription for cross-owner listings. |
| `subscriptions_activity` | Append-only audit log. `(subscriptionUUID, createdAt desc)` indexed for the detail-page timeline. |

## Permissions

```
subscriptions.service.{view,manage}
subscriptions.subscription.{view,manage}
subscriptions.invoice.view
subscriptions.activity.view
```

## Tier-2 self-service routes (ADR-0003 client surface)

Mounted on `ri.Client.ProtectedRouter` behind `RequireGlobal()` via `RegisterSelfServiceRoutes`; each handler re-checks ownership against the caller's identity (user) and `TenantProvider.ListUserMemberships` (tenant). Reads fan out across every owner the caller may act under (always the calling user, plus every tenant they own); mutations target one subscription at a time.

```
POST /v1/me/subscriptions                          { ownerKind?, tenantUuid?, serviceCode, tierCode }
GET  /v1/me/subscriptions                          ?ownerKind&ownerUuid&status   (legacy ?tenantUuid alias)
GET  /v1/me/subscriptions/{id}
POST /v1/me/subscriptions/{id}/cancel              { atPeriodEnd }
POST /v1/me/subscriptions/{id}/reactivate
GET  /v1/me/subscriptions/{id}/invoices
GET  /v1/me/subscriptions/{id}/activity
```

`POST /v1/me/subscriptions` defaults `ownerKind` to `"user"` (the calling user); pass `"tenant"` plus a `tenantUuid` the caller owns to subscribe on the organization's behalf.

The module also publishes `iface.SelfServiceCheckoutPlanner` under `module.ServiceSelfServiceCheckoutPlanner`. Implementation in `services/checkout_planner.go` resolves a subscription UUID into the snapshot the payments client handler needs to open a Stripe Checkout session (`Owner` + pending invoice + tier price). Returns `iface.ErrCheckoutNoPendingInvoice` when nothing is due — the payments handler maps that to 409.

## Not in v1

Trials, proration, metered usage, multi-currency, FatturaPA issuance, dunning sequences beyond one failure email, org-scoped RBAC. Member invites + per-tenant role assignment also deferred — Tier-2 routes assume single-owner tenants.
