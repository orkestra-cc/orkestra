# Module: Subscriptions

_Path: `/backend/internal/addons/subscriptions`_
_Parent: [../../../CLAUDE.md](../../../CLAUDE.md)_

Recurring-revenue core: a catalog of AI services the operator sells, a registry of external clients (not tenants), and subscriptions linking the two with cycle-based billing and an append-only activity log.

## Responsibility split

- **Subscriptions owns**: what's for sale, who bought it, when the next charge is due, and an audit trail. No gateway code.
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

## Collections

| Collection | Notes |
|---|---|
| `subscriptions_services` | Catalog items with pricing tiers. `code` is unique. |
| `subscriptions_clients` | External buyers. `email` unique. `orgUUID` nullable link to tenant. |
| `subscriptions_subscriptions` | Client × service × tier with cycle state. `(nextBillingAt, status)` indexed for the renewal scan. |
| `subscriptions_invoices` | Generated per cycle. `(subscriptionUUID, periodStart)` unique prevents double-billing the same cycle. |
| `subscriptions_activity` | Append-only audit log. `(subscriptionUUID, createdAt desc)` indexed for the detail-page timeline. |

## Permissions

```
subscriptions.service.{view,manage}
subscriptions.client.{view,manage}
subscriptions.subscription.{view,manage}
subscriptions.invoice.view
subscriptions.activity.view
```

## Not in v1

Trials, proration, metered usage, multi-currency, FatturaPA issuance, self-service client portal, dunning sequences beyond one failure email, org-scoped RBAC.
