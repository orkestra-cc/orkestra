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

## Collections

| Collection | Key indexes |
|---|---|
| `payments_transactions` | `(provider, providerTxID)` unique, `(subscriptionUUID, createdAt desc)`, `status` |
| `payments_payment_methods` | `(clientUUID, provider)`, `providerMethodID` unique |
| `payments_webhook_events` | `(provider, providerEventID)` **unique** — the idempotency guard |

## Config (encrypted secrets)

```
stripeApiKey          secret   STRIPE_API_KEY            sk_test_… / sk_live_…
stripeWebhookSecret   secret   STRIPE_WEBHOOK_SECRET     whsec_… from Stripe CLI / Dashboard
stripeApiVersion      string   STRIPE_API_VERSION        pinned: 2024-12-18.acacia
defaultProvider       string                             "stripe" in v1
```

## Not in v1

PayPal, SEPA Direct Debit, bank transfers, hosted checkout, client-side payment element (Stripe Elements) endpoints — those come with the self-service client portal in v2.
