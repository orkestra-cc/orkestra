# Module: Notification — Email delivery, templates, preferences

_Path: `/backend/internal/core/notification`_
_Parent: [../../../CLAUDE.md](../../../CLAUDE.md)_

<!-- Navigation -->

[← Backend](../../../CLAUDE.md) | [☰ Module Map](../../../../CLAUDE.md#module-map)

<!-- /Navigation -->

## Purpose

Core module that owns all outbound email for Orkestra. Exposes a single narrow interface (`iface.NotificationSender`) so any other module can deliver mail without caring about transport, rendering, preferences or suppressions.

Primary consumer today is the auth module (verification, password reset). Designed multi-channel from the ground up so SMS, push and webhook channels can slot in later — only email is implemented in v1.

## What it owns

| Concern                | Where                                      |
| ---------------------- | ------------------------------------------ |
| Sender profiles + resolver | `services/sender_profile.go`, `services/sender_resolver.go`, `services/sender_loader.go` |
| Driver seam + registry (`noop`, `smtp`) | `services/email_driver.go`, `services/driver_noop.go`, `services/driver_smtp.go` |
| Send error contract     | `services/send_error.go`                   |
| Template rendering     | `services/template_service.go`             |
| Default system templates | `services/default_templates.go`           |
| Per-user preferences   | `services/preference_service.go`           |
| Unsubscribe tokens     | `services/unsubscribe_service.go`          |
| Orchestration + idempotency | `services/notification_service.go`   |
| Delivery log           | `repository/notification_repository.go`    |
| HTTP endpoints         | `handlers/notification_handler.go`         |

## MongoDB collections

Declared in `module.go::Collections()` and auto-created on boot:

| Collection                          | Indexes                                           | TTL  |
| ----------------------------------- | ------------------------------------------------- | ---- |
| `notification_messages`             | `uuid` unique, `recipientUserUuid`, `category`, `idempotencyKey`, `senderSlug` (sparse) | 90 days on `createdAt` |
| `notification_templates`            | `uuid` unique, compound `templateId+locale` unique | — |
| `notification_preferences`          | compound `userUuid+category+channel` unique       | — |
| `notification_suppressions`         | `address` unique                                  | — |
| `notification_unsubscribe_tokens`   | `uuid` unique, `tokenHash` unique                 | 30 days on `expiresAt` |

## Lifecycle

- **Init**: constructs repositories, builds the `SnapshotLoader` over `ConfigService.GetConfig` (**one** document read per send, so values and secrets always come from the same active environment — see [ADR-0019](../../../../docs/adr/0019-notification-multi-sender.md) D4 — while admin UI changes still propagate without a restart), registers the core drivers (`noop`, `smtp`) and the resolver, wires the `NotificationService` and registers it as `ServiceNotificationSender`.
- **Start**: calls `TemplateService.SeedDefaults(ctx)` which inserts every `auth.*` system template (`verify_email`, `reset_password`, `suspicious_login`, `new_device_login`, `admin_suspicious_login`, `admin_invite`) into the DB if they are missing. Source strings live in `services/default_templates.go` as Go constants.
- **Stop / HealthCheck**: inherit base no-op from `BaseModule`.
- **GDPR/DSR** (`services/pii_producer.go`): registers an `iface.PIIProducer` (subject `"notification"`) on `ServicePIIProducerRegistry` at Init. Exports the subject's delivered-message history (`notification_messages`) + per-category delivery preferences (`notification_preferences`); purge deletes both under **either** erase mode. Suppressions are keyed by email address (not `userUUID`), so they ride the auth/email erasure path rather than this producer. Consumed by the [compliance module](../compliance/CLAUDE.md)'s DSR pipeline (ADR-0009).

## Settings (loaded lazily per send)

All settings live in the `module_configs` collection under the `notification` module name (AES-256-GCM for `email.smtp.password`). Env vars act as bootstrap fallbacks.

| Config key                    | Env var                        | Default   |
| ----------------------------- | ------------------------------ | --------- |
| `email.provider`              | `NOTIFICATION_EMAIL_PROVIDER`  | `noop`    |
| `email.from_address`          | `NOTIFICATION_EMAIL_FROM`      | —         |
| `email.from_name`             | `NOTIFICATION_EMAIL_FROM_NAME` | `Orkestra` |
| `email.reply_to`              | `NOTIFICATION_EMAIL_REPLY_TO`  | —         |
| `email.senders`               | — *(record list, see Sender profiles)* | —         |
| `email.senders.<slug>.mailup_user`   | — *(element sub-field)*        | —         |
| `email.senders.<slug>.mailup_secret` | — *(element sub-field, secret)* | —        |
| `email.smtp.host`             | `SMTP_HOST`                    | — *(required when provider is `smtp`)* |
| `email.smtp.port`             | `SMTP_PORT`                    | `587`     |
| `email.smtp.username`         | `SMTP_USERNAME`                | —         |
| `email.smtp.password`         | `SMTP_PASSWORD` *(secret)*     | —         |
| `email.smtp.tls_mode`         | `SMTP_TLS_MODE`                | `starttls` (options: `starttls`, `tls`, `none`) |
| `app.name`                    | `APP_NAME`                     | `Orkestra` |
| `app.support_email`           | `SUPPORT_EMAIL`                | —         |
| `app.default_locale`          | `NOTIFICATION_DEFAULT_LOCALE`  | `en` (options: `en`, `it`) |

`app.default_locale` is the fallback for callers that pass no `Locale` — it must name a locale that has seeded templates, because `Get(templateID, locale)` has no fallback and a miss only logs. Callers that resolve a locale per recipient and pass it explicitly are unaffected by it.

`/admin/modules/notification` renders as a three-group rail declared via `ConfigGroups()`: **Delivery** (`email.provider` + the five `email.smtp.*` fields), **Sender** (`email.from_address`, `email.from_name`, `email.reply_to`), **Branding & templates** (`app.name`, `app.support_email`, `app.default_locale`). `email.provider`, `email.smtp.tls_mode` and `app.default_locale` are `FieldEnum` — selects, not free text. The five `email.smtp.*` fields carry `DependsOn: email.provider in [smtp]`, so a default `noop` install shows **one** visible Delivery field (`Email provider`) until it's switched to `smtp`, which reveals the SMTP connection settings.

The `noop` provider logs rendered mail to the backend stdout instead of dialing an SMTP server — use it in dev and CI. The module reports `IsConfigured() = true` for `noop` so consumers can still make send calls without failing.

## Sender profiles and drivers (ADR-0019)

`dispatchEmail` no longer talks to a single transport. Per send it runs
**resolve → validate → send**:

1. `SenderResolver.Resolve({Category, Type, TenantID})` picks a `SenderProfile`
   (transport **and** identity). `TenantID` is read from the request context and
   passed through; the resolver ignores it (D4).
2. `DriverRegistry.Get(profile.Provider)` → `EmailDriver`; `ValidateProfile(driver,
   profile, RuntimeView)` checks the driver's `Requires()`.
3. `driver.Send(ctx, profile, msg)`; `msg.Category` carries the routing category.

Each row records the profile that carried it: `logDoc.SenderSlug = profile.Slug`
(empty when no profile resolved).

Every failure is **fail-closed** and still writes a `failed` log row whose `error`
names the profile and the reason (`sender=default driver=smtp err=not_configured
missing=smtp_host`). Nothing is silently rerouted.

Profiles are declared in the `email.senders` record list (`FieldRecordList`,
group **Sender profiles**). Storage is the flat map every setting uses:
`email.senders.__items` (roster), `email.senders.<slug>.provider`,
`.categories`, `.from_address`, `.from_name`, `.reply_to`, `.smtp_host`,
`.smtp_port`, `.smtp_tls_mode`, `.smtp_username`, `.smtp_password` (secret,
AES-256-GCM at its ordinary key), `.mailup_user`, `.mailup_secret` (secret),
`.allowed_types` (ADR-0021, below). Element sub-fields carry **no `EnvVar`** by
construction, so the flat `email.*` keys stay as the environment-bootstrap
path: **until some profile declares a pattern** — the roster is empty, or holds
only drafts — the resolver synthesizes `slug=_legacy`, pattern `*`, from them
(D6). The leading underscore is outside the slug grammar, so a roster profile
named "Default" can never collide with it; `BySlug` searches the roster first
and answers `_legacy` only while the legacy profile carries mail. Creating a first draft, or removing the last pattern, therefore never
strands mail; the cutover is the existence of a routing map, the same predicate
the validator uses. No migration, no boot-time write.

**Routing.** A pattern is exactly `*`, an exact category `foo.bar`, or a prefix
`foo.*` (matches `foo.` + ≥1 char at any depth, never bare `foo`). Entries are
trimmed, lowercased, empties dropped, within-profile duplicates collapsed. The
most specific match (longest required literal) wins; `*` last. No match ⇒ the
send **fails closed** (`ErrNoSenderForCategory`). A profile with no patterns is a
draft: never selected, never validated beyond grammar. Once a routing map
exists the category is inspected strictly: an empty or untrimmed category
matches nothing, not even `*`.

**Validation** (`config_validation.go` → `services.ValidateSenderConfig`, both
`HasConfigValidator` and `HasConfigActivationValidator`). Rules apply only once
the roster is non-empty **and** ≥1 profile declares a pattern; below that they
are vacuous (a legacy install's PATCH to `app.name` must pass; the first save of
the first profile carries no patterns). Then: every declared pattern is
well-formed (`notification.sender_pattern_invalid`), exactly one profile declares
`*` (`_no_default` / `_duplicate_default`), no pattern is claimed twice
(`_pattern_conflict`), and — for profiles declaring ≥1 pattern only — the
provider is a registered driver (`_unknown_driver`) and its **non-secret**
requirements are present (`_incomplete`). The gate never sees secrets; a `mailup`
profile missing only its secret saves and fails at send — `IsConfiguredFor`
and the explicit-sender test send cover that gap.

**Driver requirements** (`Requires()`): `noop` nothing; `smtp` `smtp_host`,
`smtp_port`, `from_address` and **never credentials** — an anonymous relay is a
supported configuration; `sendSMTP` authenticates only when a username is set.
`mailup` `from_address`, `mailup_user`, `mailup_secret` (secret). Sends
`POST https://send.mailup.com/API/v2.0/messages/sendmessage` with the SMTP+
credentials in the body's `User` field — never an `Authorization` header — and
`CampaignCode = Category` (empty falls back to the SMTP+ user's console
default). **Success ⇔ 2xx ∧ body ≤ 64 KiB ∧ parses ∧ `Status=="done"` ∧
`Code=="0"`** — anything else fails with `http=<n> status=<tok> code=<tok>`,
`body=too_large`, or `body=unparseable bytes=<n> type=<media>`; the vendor's
`Message` is never read. A success means *accepted*, not delivered. A
`reply_to` differing from `from_address` must be enabled on the account by
MailUp support, or the vendor rejects the message.

**Error contract.** `NotificationDoc.Error` is served to operators and rides the
GDPR export, so **no string produced by a remote peer is ever persisted or
logged**. Drivers return `*SendError`; it has no free-text field, and the diagnostic is rendered from its typed fields with
allowlisted tokens (`[A-Za-z0-9._-]`, ≤64 chars; ≤512 overall). An SMTP
rejection keeps only `smtp op=<step> code=<nnn>` — the server's text is dropped
because a broken MTA can echo the `AUTH` argument (`base64(\0user\0pass)`) into
its 5xx line. A local failure keeps a kind from a fixed set
(`dial|tls|timeout|canceled|io`). An error of unknown shape (a fork's driver
returning `fmt.Errorf` with a vendor body) is recorded as `err=unknown`.
The SMTP driver bounds the exchange with the context deadline or 30 s.

`IsConfigured(ctx)` means exactly what it did: the default (`*`) profile resolves
and its driver accepts it.

## Explicit sender selection (ADR-0021)

Category routing answers *"which sender serves this workload class"*. It cannot
answer *"which of the several senders eligible for `marketing` should **this**
campaign use"* without collapsing them into one pattern or growing an unbounded
per-campaign routing table. ADR-0021 amends ADR-0019 **D1 only** — a caller's Go
code still must not hardcode which sender carries a category — and adds a path
for an *operator* choosing, per send, from a policy-bounded list.

**`allowed_types` is the policy.** A profile's `allowed_types` (`FieldStringList`,
constrained to `{marketing, transactional}`) declares which send types may name
it **by slug**. Empty — the default, and every profile that predates this — means
**never explicitly selectable**: the profile still serves category routing exactly
as before, and no caller can pick it. An unknown value is rejected at save with
`notification.sender_bad_allowed_type`.

A **non-empty** `allowed_types` makes the profile load-bearing at save time: the
same completeness check ADR-0019 D5 runs for a pattern-routed profile (registered
driver, required non-secret fields present) now also runs for a selectable one.
This is why `ValidateSenderConfig` is split into two scopes — routing rules
(`_no_default`, `_duplicate_default`, `_pattern_conflict`, pattern grammar) stay
gated on `hasRoutingMap`, while driver/completeness rules run for **any** profile
that either mechanism can reach. A roster of one selectable, pattern-less profile
does **not** trip `sender_no_default`: nothing routes, so legacy still carries mail.

**Dispatch re-enforces; it never trusts the caller.** `NotificationRequest` and
`TemplatedNotificationRequest` gained `Sender string`. Empty is byte-identical to
today. Non-empty runs, in order: slug grammar + a ≤64-char bound → `BySlug` →
`Type ∈ AllowedTypes` → the resolved driver reports usable. This is enforcement,
not a second opinion on `PreflightDelivery` — a caller that skipped pre-flight, or
whose list went stale between pre-flight and send, gets the same guarantee.

**`AttemptedSenderSlug` on the failed row.** `NotificationDoc` gained it: the
validated slug verbatim when the guard passed, and the **constant marker
`"invalid"`** when it did not. Deliberately not a hash of the input — an unsalted
digest of attacker-controlled text names no profile and buys no correlation the
bounded failure reason does not already give, while risking exactly the free-text
leak the driver-error contract closed.

**`SenderDirectory`** is an *optional companion*, asserted from the same
registered `ServiceNotificationSender` object — the `CategoryConfiguredChecker`
idiom of ADR-0019 D7, no new `ServiceKey`:

```go
ListEligibleSenders(ctx, typ) ([]iface.SenderInfo, error) // identity only + Ready
PreflightDelivery(ctx, sender, category, typ) error       // nil or an iface sentinel
```

Both evaluate the **whole** delivery path, so one answer covers "would this send
go out" whichever path it takes: `sender == ""` preflights category routing
(`ErrNoSenderForCategory` when nothing matches), non-empty preflights the explicit
chain. `SenderInfo` is `Slug, Label, Provider, FromAddress, Ready` — **never** a
secret, host, or username; a picker must not receive the transport side of a
profile. A read that cannot reach config returns `ErrSenderUnavailable`, never an
empty list, which a caller would misread as "no senders configured".

**Six sentinels** live in `pkg/sdk/iface` so a consumer matches them with
`errors.Is` without importing this module: `ErrSenderInvalid` (grammar/length),
`ErrSenderNotFound`, `ErrSenderNotEligible` (`Type ∉ AllowedTypes`),
`ErrSenderNotConfigured` (driver unregistered or fields incomplete),
`ErrNoSenderForCategory` (empty sender, nothing routes), `ErrSenderUnavailable`
(the config plane itself is unreadable — nothing about the choice was evaluated).

**Tenancy (D8).** Profiles stay installation-global; the resolver's `TenantID` is
still reserved and ignored. Explicit selection widens the *choice* and the
*visibility* (label, `From:`) available to a campaign sender — it grants no send
capability category routing did not already grant implicitly. **A
multi-internal-tenant deployment that needs identity segregation MUST NOT set
`allowed_types` on a profile that needs segregating** until the named
`allowed_tenants` follow-up lands; leaving it empty keeps the profile reachable
only through category routing, which is today's behavior.

## Templates

System templates live as Go string constants in `services/default_templates.go`. On first boot they are seeded into the DB; afterwards the DB is the source of truth. Admins can override them via `PUT /v1/notifications/templates/{id}` which flips `isSystem` to `false`. Deleting an override with `DELETE` calls `SeedDefaults` again and the default comes back.

Rendering uses Go's `text/template` for the subject and plain-text body, and `html/template` for the HTML body (contextual escaping). The orchestrator automatically injects three variables into every templated send:

- `{{.UnsubscribeURL}}` — absolute URL to `/v1/notifications/unsubscribe?token=<raw>` with a fresh per-send token
- `{{.PreferencesURL}}` — absolute URL to `/account/notifications`
- `{{.AppName}}`, `{{.SupportEmail}}` — from module config, if not already provided by the caller

Each system template documents its expected variables in the `variables` array of the seeded document. For `auth.verify_email`: `AppName`, `UserName`, `VerifyURL`, `ExpiresIn`, `SupportEmail`, `UnsubscribeURL`, `PreferencesURL`. For `auth.reset_password`: the same set plus `ResetURL` and `RequestIP`. For `auth.admin_invite`: `AppName`, `UserName`, `InviteURL`, `ExpiresIn`, `InviterName` (optional), `SupportEmail`, `UnsubscribeURL`, `PreferencesURL`. For `auth.mfa_factor_added`: `AppName`, `UserName`, `FactorType` (`totp`|`passkey`), `Replaced` (bool), `RequestIP`, `At`, `SupportEmail` — and **not** `UnsubscribeURL`/`PreferencesURL`, unlike its six siblings: the orchestrator still injects them, but neither body renders one, because a security notice cannot be turned off and offering the link would say otherwise.

`auth.mfa_factor_added` is the one seeded template whose **id is not its category**. Every other one doubles as its own category constant; this one is sent under `models.CategoryAuthSecurity` (`auth.security`) because the category is the routing family — sender-profile patterns like `auth.*` match it — while the template is one specific notice inside it. Both constants live in `models/collections.go`; use `models.TemplateAuthMFAFactorAdded` for the `TemplateID` and `models.CategoryAuthSecurity` for the `Category`, never one for the other.

## Preferences and transactional mail

`PreferenceService.CanDeliver(userUUID, category, channel, type)` returns `true` unconditionally when `type == "transactional"`. Marketing mail respects the opt-out stored in `notification_preferences`, defaulting to opted-in when no preference exists.

This is deliberate: verification and password-reset mail are required for the product to function and cannot legitimately be opted out of. The unsubscribe footer still links to the preferences page where the user can opt out of *marketing* categories, with a clear note that security mail will continue to arrive.

## Idempotency

Every `Send` and `SendTemplated` call accepts an `IdempotencyKey`. Before dispatching, the orchestrator looks up the `notifications` collection for a row with the same key created within the last hour (configurable via `Options.IdempotencyTTL`). If found, the prior result is returned unchanged — no duplicate send, no duplicate log row. Auth uses keys like `verify:<user_uuid>:<token_uuid>` and `reset:<user_uuid>:<token_uuid>` so retries are safe.

## HTTP endpoints

Registered in three groups with different middleware:

### Public (no auth)

- `GET /v1/notifications/unsubscribe?token=<raw>` — consumes an unsubscribe token and opts the user out of the bound category (or `marketing` if the token has no category). Always returns a generic success message.

### User (`guest`+ role)

- `GET /v1/notifications/preferences` — list current user's preferences
- `PUT /v1/notifications/preferences` — update one `{category, channel, optedIn}` tuple

### Admin (`administrator` role)

- `GET /v1/notifications` — paginated delivery log; filters: `category`, `status`, `sender` (profile slug). Every row carries `provider` and `senderSlug`, so *which* profile sent or refused a message is answerable per row.
- `POST /v1/notifications/test` — `{to, subject?, bodyText?, sender?}`; `sender` names a profile slug (default: the `*` profile). Bypasses preferences, idempotency and the delivery log. 404 `notification.sender_not_found`, 422 `notification.sender_incomplete` (driver unknown or a required field — secret included — missing), 502 `notification.send_failed` with the bounded diagnostic. This is the only way to prove a profile whose gap is a secret.
- `GET /v1/notifications/templates` — list all templates
- `GET /v1/notifications/templates/{templateId}?locale=en` — fetch a single template
- `PUT /v1/notifications/templates/{templateId}` — override a template (sets `isSystem=false`)
- `DELETE /v1/notifications/templates/{templateId}?locale=en` — delete an override; next `Start()` reseeds the system default

## Service contract for consumers

Consumers depend on `iface.NotificationSender` from `pkg/sdk/iface`, not on any package inside this module. The interface has three methods:

```go
IsConfigured(ctx) bool
Send(ctx, NotificationRequest) (*NotificationResult, error)
SendTemplated(ctx, TemplatedNotificationRequest) (*NotificationResult, error)
```

Get the service via `module.GetTyped[iface.NotificationSender](deps.Services, module.ServiceNotificationSender)`. A consumer that needs to list or pre-flight senders (ADR-0021) type-asserts `iface.SenderDirectory` off that **same object** — there is no second `ServiceKey`, and a fork whose `NotificationSender` does not implement it simply gets `ok == false`. Auth treats it as optional — if the lookup returns `(nil, false)`, the auth module still works but signup returns `503` when `AUTH_REQUIRE_EMAIL_VERIFICATION=true`.

## Email-tracking rewriter seam (module extension point)

`*NotificationService` exposes a nil-by-default **`iface.EmailTrackingRewriter`**
seam so an optional module can rewrite the rendered HTML of an outbound email just
before transport — **without this core module importing it** (the same
`SetAuditSink`/`SetKMSProvider` setter precedent the compliance module uses).

- `iface.EmailTrackingRewriter.RewriteOutboundEmail(ctx, OutboundEmail) string`
  receives the rendered `BodyHTML`, the recipient address, the per-send
  `MessageUUID` (= the delivery-log `logDoc.UUID`), and an **opaque** `ContactRef`
  the producer set on the request; it returns the (possibly) modified HTML.
- `NotificationRequest` and `TemplatedNotificationRequest` carry an optional
  `TrackingContactRef string` — opaque to core; ignored when no rewriter is wired.
- `SetEmailTrackingRewriter(r)` wires the rewriter post-construction (satisfying
  `iface.EmailTrackingRewriterSetter`). `dispatchEmail` invokes it **only when** a
  rewriter is set **and** the body is HTML **and** the ref is non-empty, inside a
  `recover()` guard — so a nil rewriter / empty ref / a panicking rewriter all
  leave the sent body byte-identical to the un-rewritten path.

The typical implementation injects an open-pixel and rewrites click links for
consenting recipients of marketing mail. The base ships **no** rewriter; the seam
is inert until one is registered.

## Unsubscribe context seam (module extension point)

`NotificationRequest` and `TemplatedNotificationRequest` carry an optional opaque
`UnsubscribeContext string` — ignored by core, passed through to the unsubscribe
token store. `UnsubscribeTokenDoc` gained a matching `Context string` (omitempty).
`UnsubscribeService.IssueToken` accepts `context string` and stores it on the doc at
mint; on consume the handler hands `doc.Context` to the
`MarketingUnsubscribeSink` (see below). The field is opaque to core: a producer
encodes whatever attribution payload it needs (e.g. a reference carrying the
contact, campaign and run ids) and decodes it in its own sink impl. Same
`SetAuditSink`/`SetKMSProvider` / email-tracking-rewriter precedent: core stores and
forwards an opaque string; the producer owns the codec. URL length is unaffected — the
context lives server-side on the token doc, not in the unsubscribe link.

## Marketing unsubscribe sink seam (module extension point)

`pkg/sdk/iface` gains `MarketingUnsubscribeSink` + `MarketingUnsubscribeSinkSetter`:

```go
type MarketingUnsubscribeSink interface {
    OnMarketingUnsubscribe(ctx context.Context, address, category, context string) error
}
```

`*NotificationService` exposes `SetMarketingUnsubscribeSink(s)` (satisfying
`MarketingUnsubscribeSinkSetter`) and `FireMarketingUnsubscribe(ctx, address,
category, refContext) error`, which invokes the sink `recover()`-guarded and
**reports the outcome**: a nil sink is `nil` ("nothing to mirror"), a returned
error is passed through, and a panic is recovered *and* converted into an error
rather than swallowed. A panicking sink therefore still cannot break an
unsubscribe, but its failure is no longer invisible — the caller needs it to
decide whether the opt-out must be replayed downstream. The gate for whether a given category
warrants a marketing consent revocation lives in the sink's impl, not in a
category-string test in core. The base ships **no** sink; the seam is inert until a
module registers one — the intended use is mirroring the opt-out into a consent
store and attributing it to the run that sent the mail.

## Template read/write capability

`*NotificationService` also exposes `UpsertTemplate(ctx, templateID, locale, subject,
bodyHTML, bodyText) error` and `GetTemplate(ctx, templateID, locale) (*iface.TemplateView,
error)`, wrapping `TemplateService.Upsert` / `TemplateService.Get`. An empty locale
falls back to the configured `app.default_locale`, and an upsert always writes
`IsSystem: false` — operator content, never a system default.

There is **no** separate service key: the concrete service is already registered
under `module.ServiceNotificationSender`, so a consumer resolves that key and
**type-asserts** to an interface it declares itself — the same "resolve
`ServiceNotificationSender`, then narrow to the capability you need" pattern the
rewriter and unsubscribe-sink seams use. `iface.TemplateView` is the read
projection (`TemplateID`, `Locale`, `Subject`, `BodyHTML`, `BodyText`). This lets a
module compose and preview notification templates without importing this module's
`services/` package.

## What's NOT in this module

- SMS, push, webhook channels — interface is designed for them, no implementations yet
- Async delivery via NATS JetStream — all sends are synchronous in v1; `NotificationResult.Status` reserves `"queued"` for a future async upgrade
- Marketing automation, segmentation, A/B testing — transactional only
- Bounce and complaint ingestion — suppressions must be added manually via the repository until the SMTP provider offers a webhook
- Digital signature or DKIM — relies on the configured SMTP relay to handle signing

## Rules

- **Never bypass the orchestrator.** Don't call `EmailDriver.Send` directly from another module — always go through `NotificationSender.Send` or `SendTemplated` so preferences, suppressions, idempotency and the delivery log all fire.
- **Never hardcode templates in consumers.** Add a new `TemplateID` constant, a seed entry in `default_templates.go`, and document the variable contract. Consumers pass a `map[string]any` and the module renders.
- **Secrets stay encrypted.** `email.smtp.password` is a `FieldSecret` — ConfigService encrypts it at rest with AES-256-GCM. Never read it from plain env after bootstrap; always go through `deps.GetSecret`.
- **Transactional and marketing are different types.** Set `Type: "transactional"` for mail the user cannot opt out of (auth flows, invoices, legal notices). Marketing mail must set `Type: "marketing"` or preferences won't be honored.
- **Idempotency keys are the caller's responsibility.** Include one whenever a retry could legitimately happen — it's the only protection against duplicate sends.
- **`Sender` is an operator's choice, never a caller's constant.** Setting
  `Sender` from a value a *person* selected (a campaign's `senderSlug`) is the
  supported use. Hardcoding a slug in Go to route a category is exactly what
  ADR-0019 D1 forbids and ADR-0021 did **not** reopen — route it with a pattern.
- **Never pre-check and skip the guard.** `PreflightDelivery` is advisory: it can
  go stale between the check and the send. Dispatch re-validates in full, and a
  consumer must treat a send failure as authoritative even after a green pre-flight.

## Related

- [Root CLAUDE.md](../../../../CLAUDE.md) — module map and architecture
- [`pkg/sdk/iface/interfaces.go`](../../../pkg/sdk/iface/interfaces.go) — `NotificationSender` + `SenderDirectory` interface definitions and the sender sentinels
- [ADR-0019](../../../../docs/adr/0019-notification-multi-sender.md) — sender profiles, category routing, the driver seam
- [ADR-0021](../../../../docs/adr/0021-explicit-sender-selection.md) — explicit sender selection under operator policy
- [`docs/site/architecture/authentication-flow.mdx`](../../../../docs/site/architecture/authentication-flow.mdx) — how auth consumes this module
