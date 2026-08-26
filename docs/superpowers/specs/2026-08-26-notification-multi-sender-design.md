# Notification multi-sender: sender profiles, category routing, driver seam

**Date:** 2026-08-26
**Status:** Approved, ready for implementation planning
**Decision record:** [ADR-0019](../../adr/0019-notification-multi-sender.md)

## Problem

`notification` has one transport configuration and one sender identity. Every consumer — `auth` verification and password-reset mail, a fork's CRM campaign worker, forms notifications — leaves through the same relay, from the same address, on the same domain.

Email reputation is scored per domain and per IP. Bulk marketing accrues complaints; verification and password-reset mail must arrive or users cannot get in. Sharing one sender means the first workload silently degrades the second, and the failure is discovered from support tickets rather than from a log line.

Vendors enforce the same split in their terms. MailUp's SMTP+ transactional product states *"do not use it for promotional emails"*; an installation running both workloads cannot honour that with a single configured sender.

### What already exists

Nothing here needs new platform capability. Four pieces are in the base and unused or under-used:

| Piece | Where | State |
| --- | --- | --- |
| `FieldRecordList` — repeatable config records, per-element secrets, slug minting, CAS, staged removal, console renderer | `pkg/sdk/module` (17 files), `frontend-admin/.../recordList/` | complete; **no core module declares one** |
| `EmailSender` interface + `SettingsLoader` closure | `notification/services/email_service.go:63` | one loader, one `EmailSettings` |
| `dispatchEmail` — the one place category, type, recipient and rendered body coexist before transport | `notification/services/notification_service.go:193` | already the hook point a fork's CRM uses for tracking |
| `HasConfigValidator` / `HasConfigActivationValidator` — module-owned rejection at PATCH and at environment activation | `pkg/sdk/module/config_validator.go` | unused by `notification` |

`NotificationDoc.Provider` is already persisted per message, so the delivery log can attribute a send without a schema change.

## Decisions

Recorded in full, with rationale and rejected alternatives, in [ADR-0019](../../adr/0019-notification-multi-sender.md). Summary:

| # | Decision |
| --- | --- |
| D1 | Route by notification **category**, most-specific-match-wins, `*` as default. `iface.NotificationSender` unchanged. |
| D2 | A profile is transport **and** identity, declared as one `recordList` with the routing patterns on the profile. |
| D3 | Driver seam populated: `noop`, `smtp`, `mailup`. |
| D4 | Per-installation and per-environment; the resolver accepts a `TenantID` it ignores today. |
| D5 | Fail-closed on resolution failure; structural validation at save time and at environment activation. |
| D6 | Legacy flat keys survive as the env-bootstrap path and as the synthesized `default` profile. |

## Design

### Components

Four additions, all under `internal/core/notification/services/`.

| Component | Responsibility | Depends on |
| --- | --- | --- |
| `SenderProfile` | struct decoded from the record list: identity + credentials of one sender | — |
| `EmailDriver` + registry | `Name() / Validate(SenderProfile) error / Send(ctx, SenderProfile, EmailMessage) error`; registry holds `noop`, `smtp`, `mailup` | `SenderProfile` |
| `SenderResolver` | `{Category, Type, TenantID}` → `SenderProfile` | the profile list |
| `dispatchEmail` (modified) | resolve → validate → `driver.Send` | the three above |

The current `emailService` is not deleted: it retires **inside** the `smtp` driver. `sendSMTP`, the TLS-mode handling and the quoted-printable encoding move unchanged; only the source of the credentials changes, from the global `SettingsLoader` to the profile.

### Config shape

One new field, `email.senders` (`FieldRecordList`), in its own group on the settings rail.

| Sub-field | Type | Condition |
| --- | --- | --- |
| `provider` | enum `noop \| smtp \| mailup` | required |
| `categories` | stringList | patterns served; `*` marks the default profile |
| `from_address` | string | required |
| `from_name`, `reply_to` | string | — |
| `smtp_host`, `smtp_port`, `smtp_username`, `smtp_password`, `smtp_tls_mode` | string / int / string / secret / enum | `DependsOn provider in [smtp]` |
| `mailup_user`, `mailup_secret` | string / secret | `DependsOn provider in [mailup]` |

Storage stays flat, on the same key/value map every other setting uses:

```
email.senders.__items                        → "mailup-sistema,esp-campagne"
email.senders.mailup-sistema.provider        → "mailup"
email.senders.mailup-sistema.categories      → "*"
email.senders.esp-campagne.smtp_password     → (AES-256-GCM at an ordinary key)
```

Per-element secrets are ordinary encrypted values at ordinary keys, so there is no new secret handling to build or audit.

### Send flow

```
Send / SendTemplated                          ← unchanged
  └─ dispatchEmail
       ├─ prefService.CanDeliver(...)          ← unchanged
       ├─ resolver.Resolve({Category, Type, TenantID})
       │     most-specific match on categories; "*" is the default
       │     no match                    → ErrNoSenderForCategory     (fail-closed)
       ├─ driver, ok := registry[profile.Provider]
       │     unknown                     → ErrUnknownDriver           (fail-closed)
       ├─ driver.Validate(profile)
       │     incomplete                  → ErrSenderNotConfigured     (fail-closed)
       ├─ driver.Send(ctx, profile, msg)
       └─ logDoc.Provider   = profile.Provider
          logDoc.SenderSlug = profile.Slug     ← new field, omitempty
```

Every fail-closed path writes a `NotificationDoc` with `Status=failed` and the reason, so the admin delivery log answers *which* profile failed and why. A fail-closed design that cannot be diagnosed is worse than a fallback.

`TenantID` rides in the resolver input and is ignored (D4).

A fork's CRM email-tracking rewriter operates on the rendered HTML **before** this block and is untouched: open/click tracking behaves identically whichever profile carries the message.

### Matching rules

- A pattern is either `*`, an exact category, or a dotted prefix ending in `.*` (`auth.*`, `crm.campaign.*`).
- **An exact match always beats a prefix match.** `auth.verify_email` and `auth.*` are the same length, so length alone does not order them; specificity does.
- Among prefix matches, the **longest** wins. `*` is treated as a zero-length prefix, so it wins only when nothing else matches.
- At most one profile may declare `*`, and no pattern may be declared by two profiles.
- A profile that declares **no** patterns is never selected. This is a legitimate state, not a misconfiguration: it lets an operator create and test a profile before routing any traffic to it.

These two rules are enforced at save time, not at send time — but **only once a routing map actually exists**; see §Validation for the three states and why the distinction is load-bearing.

### Compatibility and environment bootstrap

`ConfigItemField` carries **no `EnvVar`, by construction** — `config_unmarshal.go` is explicit that element sub-fields resolve stored-value → default and that *"Nothing is read from the process environment."*

So the existing flat keys stay, and they are the environment-bootstrap path. When the `email.senders` roster is empty, the resolver synthesizes:

```
default := SenderProfile{
    Slug: "default", Provider: <email.provider>, Categories: []string{"*"},
    FromAddress: <email.from_address>, … , Host: <email.smtp.host>, …
}
```

An installation that never opens the new screen behaves exactly as today; a stack configured through `SMTP_HOST` keeps working. No migration, no boot-time write, no rollback path.

This compatibility is only real if **validation** honours it too: with an empty roster the sender rules must not fire at all, or every PATCH on a legacy install would 422 on a routing map it never asked for. See §Validation.

### Validation

Three layers, and the middle one has a limit worth stating rather than discovering.

**Declaration (test time).** `ValidateConfigDeclarations` already runs over the real catalog in `cmd/server`'s catalog test: `Items` present, no nested record list, no `__` prefix, `DependsOn` resolving to a sibling. A schema typo fails a test.

**Save time.** `notification` implements `HasConfigValidator`. `ValidateConfig` runs on **every** PATCH to the module — including a PATCH that touches only `app.name` — so the routing rules must be scoped to the states in which a routing map exists. There are three, and conflating them breaks either legacy installs or the migration path:

| Roster | Patterns declared | Routing rules |
| --- | --- | --- |
| empty | — | **do not apply** — a legacy installation configured entirely through the flat keys |
| non-empty | none | **do not apply** — every profile is a draft, and a pattern-less profile is never selected |
| non-empty | ≥ 1 | **all apply** |

In the third state: exactly one profile declares `*`; no pattern is claimed twice; every `provider` names a registered driver; and each driver's required **non-secret** fields are present **on the profiles that declare at least one pattern** — a draft profile alongside live ones is not held to completeness, for the same reason it is never selected. Failures return `*ConfigValidationError` → 422 with stable `notification.sender_*` codes.

Two consequences worth being explicit about, because a literal reading of "exactly one `*`" produces a broken module in both:

- **An empty roster must not be validated against the synthesized default.** `notification` implements no validator today, so its flat keys have never been checked. Enforcing driver completeness on the synthesized profile would newly reject PATCHes that are legal right now — setting `email.provider = smtp` before filling `email.smtp.host` is the natural order in which an operator fills a form. With an empty roster the rules are **vacuous, not lenient**: the flat keys keep exactly today's save behaviour.
- **The first profile must be creatable.** The console stages a create and writes the element with its sub-fields at their declared defaults, so the first save of the first profile carries no patterns. Requiring `*` at that moment would make the roster impossible to leave empty — an operator could neither stay legacy nor migrate. The pattern-less state is what makes the transition passable; the rules engage on the save that first declares a pattern, which is also the first save that could route anything.

`HasConfigActivationValidator` applies the same three-state logic to the target profile before an environment is promoted, so sandbox → production cannot activate a map that is broken in the third state.

> **Limit.** The seam contract states *"Secrets are never passed: a validator must not see decrypted secret material to do its job."* The gate therefore cannot check that `smtp_password` or `mailup_secret` is populated. A profile missing only its secret saves cleanly and fails at send. Covered by three existing mechanisms rather than a new one: the rail's "N to fill" badge over visible required fields (the console already receives per-element secret status), `POST /v1/notifications/test` with an explicit sender, and activation validation for the promotion path.

**Send time.** Fail-closed (D5), each error logged as above.

`IsConfigured(ctx)` is redefined compatibly: true when the default (`*`) profile resolves and is valid.

### MailUp driver

Verified from vendor documentation:

- Transactional sending is the **SMTP+** product; it requires an SMTP+ user (`sNNNNN_NN` username format) created from the console or the REST API.
- Authentication is either `Authorization: Bearer <access_token>` (OAuth 2) **or** the SMTP+ username and password carried in the request body's `User` parameter. **This design uses the second** — it keeps an OAuth token lifecycle out of the notification module.
- Parameters that travel as headers over SMTP become fields of a JSON object over the API.
- Rate limiting is tied to the purchased hourly message volume.
- No delivery/bounce/complaint webhook is documented; statistics are pull-only.

> **Open item for implementation.** The exact endpoint path and request body schema could not be verified: MailUp's documentation is a JS-rendered wiki that does not yield its content to a plain fetch. **PR 3 opens with a short spike** against the live documentation or an account, and the driver is written against what it finds. Nothing else in this design depends on the answer — the driver is one file behind the registry.

## Out of scope

- **Per-tenant sender profiles.** The resolver parameter is reserved (D4); the tenant-scoped collection, RBAC surface, console UI and GDPR cascade are a separate project.
- **Bounce / complaint feedback loop.** `notification_suppressions` exists and could be fed by delivery webhooks, but no driver in this design provides them (MailUp documents none). Adding a vendor that does is the moment to design this.
- **Batch / bulk send APIs.** The chokepoint is per-message by construction, and a fork's CRM depends on that for per-recipient idempotency and tracking. Batch sending would change that contract.
- **Retiring the legacy flat keys.** They are load-bearing for environment configuration (D6) until `ConfigItemField` grows an env convention, which is its own ADR.
- **Non-email channels.** The module is designed multi-channel; only email is implemented, and this design does not change that.

## Testing

The backend has a coverage gate (`make backend-coverage-gate`), so tests are part of every PR rather than a follow-up.

| Target | Approach | Why there |
| --- | --- | --- |
| `SenderResolver` | table-driven, no infrastructure | longest-match, `*`, no match, conflicting patterns, empty roster → synthesized legacy profile. The edge cases concentrate here |
| Driver registry + `Validate` | table-driven | unknown driver; each driver against incomplete profiles |
| `mailup` driver | `httptest.Server` | asserts method, headers and body shape; 4xx / 5xx / timeout. No live calls |
| `smtp` driver | the existing 233 lines, re-pointed | the credentials' source changes, the behaviour does not |
| `ValidateConfig` | table-driven over the merged map | the three states above are the point of the table: **an empty roster must accept a PATCH that touches only `app.name`** (the legacy-install regression), a roster of pattern-less drafts must save, and the save that first declares a pattern must demand a `*`. Plus a test that **documents the secret-blind limit** rather than leaving it implicit |
| `ValidateConfigActivation` | table-driven over the target profile | same three states; a map broken in the third must not be promotable |
| `dispatchEmail` | the existing 562 lines, unchanged | they are the compatibility safety net |
| Compatibility | new | empty roster + flat keys only ⇒ byte-identical send behaviour to today |

## Implementation shape

Six PRs against upstream, each green and mergeable alone. Per repo convention every PR carries its own `CLAUDE.md` updates; there is no documentation PR at the end.

| PR | Content | Notes |
| --- | --- | --- |
| **0** | ADR-0019 + this spec + the implementation plan | docs only; merges before any code so PRs 1-5 can cite it |
| **1** | `SenderProfile`, `EmailDriver`, registry with `noop` + `smtp`, resolver that always returns the synthesized legacy profile; `dispatchEmail` routed through it | **no behaviour change** — reviewable by confirming the existing tests still pass |
| **2** | `email.senders` record list, its rail group, `ValidateConfig`, `ValidateConfigActivation`; resolver starts reading the roster with the legacy fallback | first behavioural change; EN/IT i18n (parity test) and the OpenAPI diff |
| **3** | `mailup` driver | opens with the endpoint spike; one file behind the registry |
| **4** | `sender` on `POST /v1/notifications/test`; sender filter + column on `GET /v1/notifications` | OpenAPI diff |
| **5** | `docs/site/modules/core/notification.mdx` and `docs/site/operating/notifications.mdx` | the operating page currently teaches pointing the single SMTP at SES/SendGrid and must be rewritten around profiles |

**Gates to remember:** `make openapi-dump` after PRs 2 and 4; `backend-errquality` runs against a baseline, so the new `notification.sender_*` codes must be accounted for; the coverage gate applies to every PR. Render `docs/site/**` locally before merging PR 5 — nothing in this repo's CI builds the site.
