# Module: Payments

_Path: `/backend/internal/addons/payments`_
_Parent: [../../../CLAUDE.md](../../../CLAUDE.md)_

Gateway-agnostic payment processing. Implements `iface.PaymentProvider` so the `subscriptions` module can charge cards, issue refunds, and react to webhook events without importing any Stripe/PayPal code.

## v1 providers

- **Stripe** — supported. Uses `stripe-go/v76`, off-session PaymentIntents with automatic payment methods (cards).
- **PayPal** — stub only. The `ProviderName` enum reserves `"paypal"` so subscriptions can pick it per charge in v2.

## How the façade works

- `services/payment_service.go` holds a `map[ProviderName]iface.PaymentProvider`.
- `deps.Services.Register(module.ServicePaymentProvider, paymentService)` publishes the façade so subscriptions can lookup.
- Each call routes by the `Customer.Provider` field on `SubscriptionCharge`, falling back to `defaultProvider` from config.
- Every call persists a `payments_transactions` document for the admin history view, regardless of success/failure.

## Webhook pipeline

```
POST /v1/payments/webhooks/stripe  (public, no JWT)
          │
   raw body + Stripe-Signature
          │
  stripeHandler.ServeHTTP → provider.VerifyWebhook (HMAC via stripe-go)
          │                                             │
          │                                         invalid → 401
          ▼
  services.Dispatcher.Handle
      1. Insert into payments_webhook_events
         (unique (provider, providerEventID) → dedupe guard)
         duplicate → 200 {"status":"duplicate"}
      2. Lookup iface.SubscriptionReconciler from ServiceRegistry
      3. Call MarkInvoicePaid | MarkInvoiceFailed | RecordRefund
      4. MarkProcessed on the webhook event
```

All reconciler methods are idempotent, so a replayed webhook (same `evt.id`) is double-safe: the dedupe insert fails first, but even if it didn't, the reconciler's own `if already == Paid { return nil }` short-circuits.

## Metadata contract with subscriptions

`PaymentIntent.metadata` must carry `subscriptionUUID` + `invoiceUUID` — those are how webhook handlers find the right subscription/invoice to update. Set in `renewal_service.go:chargeInvoice` and echoed back by Stripe on every related event (succeeded, failed, refunded).

## Owner scope (post-onboarding refactor)

Every operator-admin route under the module sits behind `RequireInternalTenant()` — external-tenant tokens cannot hit transaction reads, refunds, payment-method reads, or the webhook audit log. The Stripe webhook endpoint stays public (HMAC-verified inside the handler).

Transaction and PaymentMethod rows are owner-scoped via polymorphic `(ownerKind, ownerUUID)` — `Kind="user"` for self-registered clients, `Kind="tenant"` for admin-attached business members. `PaymentService.ChargeSubscription` reads the owner from the charge struct's `Owner` field (preferred) or from `ownerKind`/`ownerUUID` metadata stamps populated by the subscriptions renewal service. A thin `TenantPaymentAdapter` (`services/tenant_provider.go`) implements `iface.TenantPaymentProvider` so `core/tenant` can serve `GET /v1/admin/tenants/{id}/payments` — the adapter filters by `Owner{Kind:"tenant", UUID:tenantUUID}`.

User-owner checkout (Phase 2) reads the caller's profile through `iface.UserBillingCustomerProvider` (wired by the `clientbilling` addon). On the first charge / setup session the handler creates the Stripe customer and persists the id back onto the profile via `SetStripeCustomerID`, mirroring the tenant-owner flow against `Tenant.StripeCustomerID`. When the clientbilling addon is disabled the user-owner branch returns 503 `"user billing profile not configured"`; when the addon is enabled but the user has not filled out their profile yet, the handler returns 409 `"complete your billing profile before checkout"` so the SPA can route them to `PUT /v1/me/billing-profile` before retrying.

## Collections

| Collection | Key indexes |
|---|---|
| `payments_transactions` | `(provider, providerTxID)` unique, `(subscriptionUUID, createdAt desc)`, `(ownerKind, ownerUUID, createdAt desc)` for self-service `/v1/me/transactions` and the admin aggregator, `status` |
| `payments_payment_methods` | `(ownerKind, ownerUUID, provider)`, `providerMethodID` unique |
| `payments_webhook_events` | `(provider, providerEventID)` **unique** — the idempotency guard |

## Config (encrypted secrets)

```
stripeApiKey          secret   STRIPE_API_KEY            sk_test_… / sk_live_…
stripeWebhookSecret   secret   STRIPE_WEBHOOK_SECRET     whsec_… from Stripe CLI / Dashboard
stripeApiVersion      string   STRIPE_API_VERSION        pinned: 2024-12-18.acacia
defaultProvider       string                             "stripe" in v1
```

## Tier-2 self-service routes (ADR-0003 client surface)

Mounted on `ri.Client.ProtectedRouter` behind `RequireGlobal()`; each handler re-checks ownership against the calling user's identity and `TenantProvider.ListUserMemberships`. Reads fan out across every owner the caller may act under (always the calling user, plus every tenant they own). The handler is built unconditionally — tenant-owner flows degrade to 503 when the tenant module is missing, user-owner flows still work.

```
GET  /v1/me/transactions                      ?ownerKind&ownerUuid&subscriptionUuid&status   (legacy ?tenantUuid alias)
GET  /v1/me/payment-methods                   ?ownerKind&ownerUuid                            (legacy ?tenantUuid alias)
POST /v1/me/payments/checkout-session         { subscriptionUuid, successUrl, cancelUrl }
POST /v1/me/payments/setup-checkout-session   { ownerKind?, ownerUuid?, successUrl, cancelUrl }   (legacy tenantUuid alias)
```

`checkout-session` opens a Stripe Checkout in `mode=payment` for the subscription's most recent pending invoice, with `setup_future_usage=off_session` so the card is saved for next-cycle renewals. Stamps `subscriptionUUID/invoiceUUID/ownerKind/ownerUUID` into the resulting PaymentIntent metadata so the existing webhook reconciler picks it up — no new reconciliation path. Returns 409 when the subscription has no pending invoice. The owner is resolved from the planner's `CheckoutPlan.Owner` and re-checked against the caller's owner set.

`setup-checkout-session` opens Stripe Checkout in `mode=setup` (card-only, no charge) so a Tier-2 user can save a card from the account page. Owner defaults to the calling user; pass `ownerKind:"tenant"` + `ownerUuid` to save a card on an owned tenant.

## Not in v1

PayPal, SEPA Direct Debit, bank transfers, embedded Stripe Elements (hosted Checkout only). Anonymous-checkout (no auth, sessionless) is also out — every `/v1/me/*` route requires an authenticated client session.
