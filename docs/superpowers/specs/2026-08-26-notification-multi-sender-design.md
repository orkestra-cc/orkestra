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
| D7 | Pre-flight checks become category-aware via an **optional companion interface**; `IsConfigured(ctx)` keeps its meaning. |

## Design

### Components

Four additions, all under `internal/core/notification/services/`.

| Component | Responsibility | Depends on |
| --- | --- | --- |
| `SenderProfile` | struct decoded from the record list: identity + credentials of one sender | — |
| `EmailDriver` + registry | `Name() / Validate(SenderProfile) error / Send(ctx, SenderProfile, EmailMessage) error`; registry holds `noop`, `smtp`, `mailup` | `SenderProfile` |
| `SenderResolver` | `{Category, Type, TenantID}` → `SenderProfile`; also backs `IsConfiguredFor` | the profile list |
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
| `smtp_host`, `smtp_port`, `smtp_tls_mode` | string / int / enum | `DependsOn provider in [smtp]`; `smtp_host` required |
| `smtp_username`, `smtp_password` | string / secret | `DependsOn provider in [smtp]`; **optional — an anonymous relay is a supported configuration** |
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

**Grammar.** A pattern is exactly one of:

| Form | Meaning | Matches | Does **not** match |
| --- | --- | --- | --- |
| `*` | the default | everything | — |
| `foo.bar` | exact | `foo.bar` only | `foo.bar.baz` |
| `foo.*` | prefix | any category beginning `foo.` with **at least one** further character, at any depth — `foo.bar`, `foo.bar.baz` | the bare `foo` |

Nothing else is legal: no `*` inside a token (`auth*`), no bare `*` mid-pattern (`auth.*.google`), no pattern that is only `.`. Malformed patterns are rejected at save time with `notification.sender_pattern_invalid`.

The bare-`foo` exclusion is deliberate and matters in practice: a fork's CRM sends `Category: "marketing"`, a single token with no dot. Only the exact pattern `marketing` reaches it — `marketing.*` does not — so a category with no dot can never be captured by accident from a prefix rule written for a namespace.

**Normalization.** Patterns are operator-typed free text, so on read each entry is trimmed and lowercased, empty entries are dropped (`"auth.*,,crm.*"` yields two patterns, not three — an empty string must never become a pattern that matches everything), and duplicates **within one profile** are collapsed silently. A repeated pattern in the same list is redundant, not an error, and rejecting it would be pedantry the operator has to work around. The category is lowercased for comparison too, so matching is effectively case-insensitive in both directions.

Deduplication happens **before** the cross-profile uniqueness check, so a within-profile repeat can never be reported as a conflict between profiles.

**Precedence.**

- **An exact match always beats a prefix match.** `auth.verify_email` and `auth.*` are the same length, so length alone does not order them; specificity does.
- Among prefix matches, the **longest** wins — `auth.oauth.*` beats `auth.*` for `auth.oauth.google`.
- `*` is treated as a zero-length prefix, so it wins only when nothing else matches.
- At most one profile may declare `*`, and no pattern may be declared by two profiles.
- A profile that declares **no** patterns is never selected. This is a legitimate state, not a misconfiguration: it lets an operator create and test a profile before routing any traffic to it.

The uniqueness rules are enforced at save time, not at send time — but **only once a routing map actually exists**; see §Validation for the three states and why the distinction is load-bearing.

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

This compatibility is only real if **validation** honours it too, in two places. With an empty roster the sender rules must not fire at all, or every PATCH on a legacy install would 422 on a routing map it never asked for (see §Validation). And `driver.Validate` for `smtp` must require exactly what `isSMTPConfigured` requires today — host, port, from address, and **not** credentials — or an anonymous internal relay stops working on upgrade (see §Driver validation contract).

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

> **Limit.** The seam contract states *"Secrets are never passed: a validator must not see decrypted secret material to do its job."* The gate therefore cannot check that a driver's secret is populated — but this only bites for drivers whose secret is **mandatory**, which today means `mailup` alone. `smtp` has no such requirement (see §Driver validation contract), so there is nothing to be blind about there. A `mailup` profile missing only its secret saves cleanly and fails at send. Covered by three existing mechanisms rather than a new one: the rail's "N to fill" badge over visible required fields (the console already receives per-element secret status), `POST /v1/notifications/test` with an explicit sender, and `IsConfiguredFor` at request time, which *can* read secrets (D7).

**Send time.** Fail-closed (D5), each error logged as above.

`IsConfigured(ctx)` keeps exactly its present meaning — the default (`*`) profile resolves and is valid — and is **no longer the right question for a category-specific pre-flight**. See §Pre-flight checks.

### Driver validation contract

`driver.Validate(profile)` decides whether a profile is usable, and for `smtp` it must reproduce today's `isSMTPConfigured` **exactly** — otherwise D6's compatibility promise is words rather than behaviour.

| Driver | `Validate` requires | Explicitly does **not** require |
| --- | --- | --- |
| `noop` | nothing | — |
| `smtp` | `host`, `port`, `from_address` | `username`, `password` |
| `mailup` | `from_address`, `mailup_user`, `mailup_secret` | — |

**Anonymous SMTP relay stays supported, and this is the concrete thing "byte-identical" has to mean.** `email_service.go:97` requires only host, port and from address; `sendSMTP` calls `client.Auth` **only when `Username != ""`**. An internal MTA on a private network with no authentication is a first-class path in a self-hosted product, and a `Validate` that demanded a password would break every such installation on upgrade — silently, since the config would still look complete.

For the same reason `smtp` does not require a password *when a username is set*. Today `smtp.PlainAuth` is attempted with whatever password is stored and the server decides; adding a local requirement would be a tightening, and tightening is not this design's job.

> **One deliberate tightening, declared.** `from_address` is `Required: true` on the profile element, while the flat `email.from_address` is not marked required in today's schema — even though `isSMTPConfigured` has always demanded it at runtime. So the schema and the runtime already disagree upstream, and the profile element resolves that disagreement in favour of the runtime. This changes what the console badges and rejects; it does not change which sends succeed.

### Pre-flight checks

`auth` calls `IsConfigured` seven times before sending, and every call is a pre-flight for **one** category — each followed within a few lines by the send that names it:

| Call site | Guards | Category it is about to send |
| --- | --- | --- |
| `password_auth_service.go:256` | signup admission → `ErrNotificationDown` | `auth.verify_email` |
| `password_auth_service.go:969`, `:1839` | password reset | `auth.reset_password` |
| `password_auth_service.go:1280` | email verification | `auth.verify_email` |
| `password_auth_service.go:1660` | new-device notice | `auth.new_device_login` |
| `password_auth_service.go:1767` | admin invite | `auth.admin_invite` |
| `suspicious_login_notifier.go:216`, `:295` | suspicious-login notices | `auth.suspicious_login`, `auth.admin_suspicious_login` |

Once routing is per-category, a single global boolean is wrong in both directions: a valid default with a broken `auth.*` returns `true` and the signup is accepted against mail that then fails; a broken default with a working `auth.*` refuses registrations that would have succeeded.

`pkg/sdk/iface` therefore gains an optional companion (D7), following `HasConfigGroups`/`ConfigGroupsOf`:

```go
type CategoryConfiguredChecker interface {
    IsConfiguredFor(ctx context.Context, category string) bool
}

func IsConfiguredForCategory(ctx context.Context, s NotificationSender, category string) bool
```

The accessor returns the exact answer when the sender implements the companion and falls back to `IsConfigured(ctx)` otherwise, so a fork's own `NotificationSender` needs no change.

`IsConfiguredFor` resolves the category, then reports whether the profile's driver is registered and `Validate` passes. **No match returns false** — consistent with fail-closed, and better than today for that case: the signup is refused up front rather than accepted against mail that cannot be sent.

Unlike `ValidateConfig`, this check runs inside the module against its own configuration and **can read secrets**, so it catches precisely the class of misconfiguration the save-time gate is forbidden to see.

Core `auth` migrates its seven guards to the accessor. This lands in **PR 2**, not PR 1: until the roster is read, every category resolves to the same synthesized profile and the distinction cannot bite.

### MailUp driver

| | |
| --- | --- |
| Send endpoint | `POST https://send.mailup.com/API/v2.0/messages/sendmessage` |
| Methods | `SendMessage` (fully distinct content per message — what this driver uses) and `SendTemplate` (a stored body with merge tags) |
| Authentication | the **SMTP+ credentials in the request body's `User` field**. OAuth 2 `Authorization: Bearer` belongs to the *management/console* APIs, not to sending |
| Prerequisite | an SMTP+ user (`sNNNNN_NN` username format), created from the console or the management API after authorizing a trusted sender |
| Payload | parameters that ride as headers over SMTP become fields of a JSON object |
| Rate limit | tied to the purchased hourly message volume |
| SMTP relay equivalent | `fast.smtpok.com`, which accepts an `X-SMTPAPI` JSON header for the same extras |

**Bounce and unsubscribe webhooks exist.** The product page states that *"With SMTP+, unsubscriptions and bounces are handled automatically, with notifications to the sender and WebHook to inform other applications or external databases."*

Read precisely, that is a property of the **SMTP+ product**, not of the API send path — it is available whichever way the message is submitted. So it is *not* an argument for the driver over the relay; it is what makes a suppression feedback loop genuinely buildable later (see §Out of scope).

`CampaignCode` is worth noting for the implementation: MailUp uses it to aggregate messages for statistics. The notification category is the natural value to map onto it, which would make the vendor's own reporting line up with the routing this design introduces.

> **HTTP 200 is not success.** MailUp's REST surface is WCF-derived (`.svc` endpoints, SOAP heritage), and that family routinely answers `200 OK` with an error envelope in the body — a `Status` of `"error"` alongside a message, rather than a 4xx. A driver that treats the status line as the verdict would record `Status=sent` on mail that was never accepted, which under fail-closed is the worst possible failure: silent, and indistinguishable from success in the delivery log. **The driver decides from the parsed body, and only then from the status code.** This is a test case, not a comment.

> **What still needs reading at implementation time** — not a spike, ordinary reference work. The exact JSON field names of the `SendMessage` body are not transcribed here; PR 3 reads them from the [vendor page](https://helpmailup.atlassian.net/wiki/spaces/mailupapi/pages/36342655/Transactional+Emails+using+APIs) and writes the struct against them. The endpoint, the auth model and the method choice above are settled.

## Out of scope

- **Per-tenant sender profiles.** The resolver parameter is reserved (D4); the tenant-scoped collection, RBAC surface, console UI and GDPR cascade are a separate project.
- **Bounce / complaint feedback loop.** `notification_suppressions` exists and MailUp's SMTP+ **does** emit webhooks for bounces and unsubscriptions, so this is buildable rather than blocked — but it is a separate design: a public signed-webhook endpoint, per-vendor payload mapping, and a suppression-write path with its own idempotency. Deliberately not folded in here; this ADR is about choosing a sender, not about consuming delivery feedback.
- **Batch / bulk send APIs.** The chokepoint is per-message by construction, and a fork's CRM depends on that for per-recipient idempotency and tracking. Batch sending would change that contract.
- **Retiring the legacy flat keys.** They are load-bearing for environment configuration (D6) until `ConfigItemField` grows an env convention, which is its own ADR.
- **Non-email channels.** The module is designed multi-channel; only email is implemented, and this design does not change that.

## Testing

The backend has a coverage gate (`make backend-coverage-gate`), so tests are part of every PR rather than a follow-up.

| Target | Approach | Why there |
| --- | --- | --- |
| `SenderResolver` | table-driven, no infrastructure | longest-match, exact-beats-prefix at equal length, `*`, no match, empty roster → synthesized legacy profile. Plus the grammar: `foo.*` must **not** match the bare `foo`, must match at any depth, and `"auth.*,,crm.*"` must yield two patterns — an empty entry becoming a match-everything pattern is the failure that would route the whole install to one profile |
| Pattern normalization | table-driven | trim, lowercase both sides, drop empties, collapse within-profile duplicates **before** the cross-profile uniqueness check so a repeat is never reported as a conflict |
| Driver registry + `Validate` | table-driven | unknown driver; each driver against incomplete profiles |
| `mailup` driver | `httptest.Server` | asserts method, auth placement and body shape; 4xx / 5xx / timeout — **and HTTP 200 carrying an error envelope** (see below). No live calls |
| `smtp` driver | the existing 233 lines, re-pointed | the credentials' source changes, the behaviour does not |
| `ValidateConfig` | table-driven over the merged map | the three states above are the point of the table: **an empty roster must accept a PATCH that touches only `app.name`** (the legacy-install regression), a roster of pattern-less drafts must save, and the save that first declares a pattern must demand a `*`. Plus a test that **documents the secret-blind limit** rather than leaving it implicit |
| `ValidateConfigActivation` | table-driven over the target profile | same three states; a map broken in the third must not be promotable |
| `IsConfiguredFor` | table-driven | valid default + broken `auth.*` ⇒ **false** for `auth.*` and true for the default (today's global boolean gets both wrong); unmatched category ⇒ false; a `mailup` profile with a secret-only gap ⇒ false, which `ValidateConfig` cannot see |
| `smtp` driver `Validate` | table-driven | **an anonymous relay — host + port + from, no credentials — must validate**, as `isSMTPConfigured` does today; missing host, port or from must not. This is the regression test for D6 |
| `IsConfiguredForCategory` accessor | table-driven | a sender implementing the companion gets the exact answer; one that does not falls back to `IsConfigured` — the fork-compatibility guarantee |
| `auth` guards | existing auth tests, re-pointed | the seven call sites ask for the category they are about to send |
| `dispatchEmail` | the existing 562 lines, unchanged | they are the compatibility safety net |
| Compatibility | new | empty roster + flat keys only ⇒ byte-identical send behaviour to today |

## Implementation shape

Six PRs against upstream, each green and mergeable alone. Per repo convention every PR carries its own `CLAUDE.md` updates; there is no documentation PR at the end.

| PR | Content | Notes |
| --- | --- | --- |
| **0** | ADR-0019 + this spec + the implementation plan | docs only; merges before any code so PRs 1-5 can cite it |
| **1** | `SenderProfile`, `EmailDriver`, registry with `noop` + `smtp`, resolver that always returns the synthesized legacy profile; `dispatchEmail` routed through it | **no behaviour change** — reviewable by confirming the existing tests still pass |
| **2** | `email.senders` record list, its rail group, `ValidateConfig`, `ValidateConfigActivation`; resolver starts reading the roster with the legacy fallback; **`CategoryConfiguredChecker` + accessor in `pkg/sdk/iface`, and `auth`'s seven guards migrated to it**; **`sender` on `POST /v1/notifications/test`** | first behavioural change, and everything D5 depends on has to be in it. The pre-flight must land in the same PR that lets profiles diverge or the guards are wrong for a merge window — and the explicit-sender test-send is the *only* mitigation that actually proves a profile, so shipping routing without it leaves fail-closed unverifiable. EN/IT i18n (parity test) |
| **3** | `mailup` driver against `POST https://send.mailup.com/API/v2.0/messages/sendmessage` | no spike — endpoint, auth model and method are settled; only the body field names are read from the vendor page. One file behind the registry, so a vendor API change touches nothing else |
| **4** | sender filter + `senderSlug` on `GET /v1/notifications`, and the delivery-log column | diagnostics, not mitigation: at PR 2 a failed send already names the profile in the log's reason string, so this improves triage rather than enabling it |
| **5** | `docs/site/modules/core/notification.mdx` and `docs/site/operating/notifications.mdx` | the operating page currently teaches pointing the single SMTP at SES/SendGrid and must be rewritten around profiles |

**Gates to remember.**

- **OpenAPI.** Declaring `email.senders` produces **no** spec diff: per [ADR-0012](../../adr/0012-module-config-group-contract.md), `ConfigField`/`ConfigGroup` are the frozen shapes, and declaring *data* against them is not a shape change. So in PR 2 `openapi-check` acts as a **guard that nothing moved**, and a diff there is a signal to stop and find out why. PR 2 still needs `make openapi-dump` — but for the `sender` field on the test endpoint, which *is* a wire change. PR 4 needs it for the new query parameter and response field.
- **Error codes.** `backend-errquality` runs against a baseline, so the new `notification.sender_*` codes must be accounted for.
- **Coverage.** The gate applies to every PR.
- **Docs site.** Render `docs/site/**` locally before merging PR 5 — nothing in this repo's CI builds the site.
