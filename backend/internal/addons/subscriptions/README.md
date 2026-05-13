<div align="center">

# Orkestra Subscriptions addon

**Recurring-revenue core for the [Orkestra](https://github.com/orkestra-cc/orkestra) modular monolith. Owns the catalog of AI services the operator sells, the subscription records that bind Tier-2 external tenants to those services, cycle-based invoice generation, an append-only activity log, and an hourly renewal job that walks the `nextBillingAt` index. Stays cycle-free with the [`payments`](https://github.com/orkestra-cc/orkestra-addon-payments) addon by resolving `iface.PaymentProvider` lazily on every charge.**

[![Go Reference](https://pkg.go.dev/badge/github.com/orkestra-cc/orkestra-addon-subscriptions.svg)](https://pkg.go.dev/github.com/orkestra-cc/orkestra-addon-subscriptions)
[![Go](https://img.shields.io/badge/Go-1.25-00ADD8?logo=go&logoColor=white&style=flat-square)](https://go.dev)
[![Module](https://img.shields.io/badge/module-github.com%2Forkestra--cc%2Forkestra--addon--subscriptions-blue?style=flat-square)](https://github.com/orkestra-cc/orkestra-addon-subscriptions)
[![Latest tag](https://img.shields.io/github/v/tag/orkestra-cc/orkestra-addon-subscriptions?sort=semver&style=flat-square)](https://github.com/orkestra-cc/orkestra-addon-subscriptions/tags)
[![License: Apache 2.0](https://img.shields.io/badge/License-Apache_2.0-blue.svg?style=flat-square)](LICENSE)

[SDK](https://github.com/orkestra-cc/orkestra-sdk) · [Monorepo](https://github.com/orkestra-cc/orkestra) · [Module docs](CLAUDE.md)

</div>

---

## What this is

A self-contained Orkestra addon implementing the `Module` interface from [`orkestra-cc/orkestra-sdk`](https://github.com/orkestra-cc/orkestra-sdk). Owns four MongoDB collections — `subscriptions_services`, `subscriptions_subscriptions`, `subscriptions_invoices`, `subscriptions_activity` — and exposes both an operator-side admin surface and a Tier-2 client self-service surface under `/v1/me/subscriptions`.

**State machine:**

```
active ──charge_failed──▶ past_due ──retry_succeeded──▶ active
                              │
                              └──max_failures (3)──▶ suspended
active | past_due | suspended ──admin_cancel──▶ cancelled (terminal)
active ──cancel_at_period_end─▶ (ends at CurrentPeriodEnd)─▶ cancelled
```

Transitions happen in `services/subscription_service.go` (user actions) and `services/renewal_service.go` (charge outcomes). The webhook reconciler is **idempotent** — receiving the same Stripe event twice is a no-op.

**Renewal job** (`jobs/renewal_job.go`) tickles every `SUBSCRIPTIONS_RENEWAL_INTERVAL` (default 1h). Each tick: find subscriptions with `NextBillingAt <= now` AND `status ∈ {active, past_due}`, generate an invoice, attempt a charge, update state. Failed charges push `NextBillingAt` out by 1 day so we retry tomorrow, not every tick.

## Cycle-free wiring with `payments`

Neither module declares the other in `Dependencies()`. `subscriptions` resolves `iface.PaymentProvider` lazily from the kernel's `ServiceRegistry` on every charge; `payments` does the same for `iface.SubscriptionReconciler`. Because both registrations complete during `Init()` and lookups happen at runtime, there is no init-time ordering constraint.

If `payments` is disabled, the renewal job still generates invoices but marks them `awaiting_manual_payment` and emits `manual_payment_required` activity — the workflow keeps running so manual bank transfers can be reconciled by editing the invoice status.

## How it ships

The same source tree lives in two places:

- **In-tree** at [`backend/internal/addons/subscriptions/`](https://github.com/orkestra-cc/orkestra/tree/main/backend/internal/addons/subscriptions) inside the [orkestra-cc/orkestra](https://github.com/orkestra-cc/orkestra) monorepo, where cross-module development happens.
- **Standalone** at this repository, tagged from `v0.1.0`, consumed via the Go module proxy by anything outside the monorepo.

```go
require github.com/orkestra-cc/orkestra-addon-subscriptions v0.1.0
replace github.com/orkestra-cc/orkestra-addon-subscriptions => ./internal/addons/subscriptions
```

The `replace` will retire once cross-cutting addon churn settles.

## Install

```bash
go get github.com/orkestra-cc/orkestra-addon-subscriptions@latest
```

Requires Go 1.25.10 or newer.

```go
import subscriptions "github.com/orkestra-cc/orkestra-addon-subscriptions"
```

## Boot in a host

```go
import (
    subscriptions "github.com/orkestra-cc/orkestra-addon-subscriptions"
    "github.com/orkestra-cc/orkestra-sdk/module"
)

reg := module.NewModuleRegistry(logger) // your kernel's *slog.Logger
reg.Register(subscriptions.NewModule())
```

The registry takes care of init order, Mongo collection creation, route mounting (both operator-side admin and Tier-2 client surfaces), RBAC enforcement (`subscriptions.service.*`, `subscriptions.subscription.*`, `subscriptions.invoice.view`, `subscriptions.activity.view`), and module-gate middleware for runtime enable/disable via `/admin/modules`.

The addon publishes three services into the kernel's `ServiceRegistry`:

- `iface.SubscriptionReconciler` — consumed by `payments` to apply webhook outcomes.
- `iface.SelfServiceCheckoutPlanner` — consumed by `payments` to resolve a subscription UUID into the snapshot the client checkout handler needs to open a Stripe Checkout session.
- `iface.TenantSubscriptionProvider` — consumed by `core/tenant` to serve `GET /v1/admin/tenants/{id}/subscriptions`.

## Endpoints

### Operator-side (admin)

| Method | Path | Purpose |
|---|---|---|
| `GET/POST/PATCH/DELETE` | `/v1/subscriptions/services[/{id}]` | Catalog management |
| `GET/POST/PATCH/DELETE` | `/v1/subscriptions/subscriptions[/{id}]` | Subscription lifecycle (operator-attached) |
| `GET` | `/v1/subscriptions/subscriptions/{id}/{invoices,activity}` | Per-subscription detail |

### Client-side (Tier-2 self-service, ADR-0003)

Mounted on `ri.Client.ProtectedRouter` behind `RequireGlobal()`. Each handler builds a "caller tenant set" from the personal tenant plus every owned tenant; reads fan out across that set, mutations target one subscription at a time.

| Method | Path | Body |
|---|---|---|
| `POST` | `/v1/me/subscriptions` | `{ tenantUuid?, serviceCode, tierCode }` |
| `GET` | `/v1/me/subscriptions` | `?tenantUuid&status` |
| `GET` | `/v1/me/subscriptions/{id}` | — |
| `POST` | `/v1/me/subscriptions/{id}/cancel` | `{ atPeriodEnd }` |
| `POST` | `/v1/me/subscriptions/{id}/reactivate` | — |
| `GET` | `/v1/me/subscriptions/{id}/{invoices,activity}` | — |

`POST /v1/me/subscriptions` defaults the scope to the caller's personal tenant; pass `tenantUuid` to subscribe under a tenant the caller owns.

## Configuration

| Env var | Purpose | Default |
|---|---|---|
| `SUBSCRIPTIONS_RENEWAL_INTERVAL` | How often the renewal job runs | `1h` |
| `SUBSCRIPTIONS_INVOICE_PREFIX` | Invoice number prefix (e.g. `SUB`) | `SUB` |

See [`CLAUDE.md`](CLAUDE.md) for the responsibility split with payments + billing, MongoDB index layout, the full state-machine transition table, and the not-in-v1 list (trials, proration, metered usage, multi-currency, FatturaPA issuance).

## Versioning

Standard Go semver. `v0.x` allows breaking changes (rare); `v1.x` will freeze the public surface alongside the rest of the Orkestra addon ecosystem.

## Contributing

Most development happens in the [upstream monorepo](https://github.com/orkestra-cc/orkestra) where this addon lives alongside the kernel and its consumers — so a change can be implemented, tested, and reviewed against real callers in one diff. PRs against this standalone repo are welcome for addon-only changes.

See [CONTRIBUTING.md](https://github.com/orkestra-cc/orkestra/blob/main/CONTRIBUTING.md) in the monorepo for the contributor flow.

## License

Licensed under the [Apache License, Version 2.0](LICENSE).
