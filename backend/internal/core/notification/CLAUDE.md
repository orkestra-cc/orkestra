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
| `notification_messages`             | `uuid` unique, `recipientUserUuid`, `category`, `idempotencyKey` | 90 days on `createdAt` |
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
| `email.smtp.host`             | `SMTP_HOST`                    | — *(required when provider is `smtp`)* |
| `email.smtp.port`             | `SMTP_PORT`                    | `587`     |
| `email.smtp.username`         | `SMTP_USERNAME`                | —         |
| `email.smtp.password`         | `SMTP_PASSWORD` *(secret)*     | —         |
| `email.smtp.tls_mode`         | `SMTP_TLS_MODE`                | `starttls` (options: `starttls`, `tls`, `none`) |
| `app.name`                    | `APP_NAME`                     | `Orkestra` |
| `app.support_email`           | `SUPPORT_EMAIL`                | —         |

`/admin/modules/notification` renders as a three-group rail declared via `ConfigGroups()`: **Delivery** (`email.provider` + the five `email.smtp.*` fields), **Sender** (`email.from_address`, `email.from_name`, `email.reply_to`), **Branding & templates** (`app.name`, `app.support_email`). `email.provider` and `email.smtp.tls_mode` are `FieldEnum` — selects, not free text. The five `email.smtp.*` fields carry `DependsOn: email.provider in [smtp]`, so a default `noop` install shows **one** visible Delivery field (`Email provider`) until it's switched to `smtp`, which reveals the SMTP connection settings.

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

Every failure is **fail-closed** and still writes a `failed` log row whose `error`
names the profile and the reason (`sender=default driver=smtp err=not_configured
missing=smtp_host`). Nothing is silently rerouted.

Profiles are declared in the `email.senders` record list (`FieldRecordList`,
group **Sender profiles**). Storage is the flat map every setting uses:
`email.senders.__items` (roster), `email.senders.<slug>.provider`,
`.categories`, `.from_address`, `.from_name`, `.reply_to`, `.smtp_host`,
`.smtp_port`, `.smtp_tls_mode`, `.smtp_username`, `.smtp_password` (secret,
AES-256-GCM at its ordinary key). Element sub-fields carry **no `EnvVar`** by
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

## Templates

System templates live as Go string constants in `services/default_templates.go`. On first boot they are seeded into the DB; afterwards the DB is the source of truth. Admins can override them via `PUT /v1/notifications/templates/{id}` which flips `isSystem` to `false`. Deleting an override with `DELETE` calls `SeedDefaults` again and the default comes back.

Rendering uses Go's `text/template` for the subject and plain-text body, and `html/template` for the HTML body (contextual escaping). The orchestrator automatically injects three variables into every templated send:

- `{{.UnsubscribeURL}}` — absolute URL to `/v1/notifications/unsubscribe?token=<raw>` with a fresh per-send token
- `{{.PreferencesURL}}` — absolute URL to `/account/notifications`
- `{{.AppName}}`, `{{.SupportEmail}}` — from module config, if not already provided by the caller

Each system template documents its expected variables in the `variables` array of the seeded document. For `auth.verify_email`: `AppName`, `UserName`, `VerifyURL`, `ExpiresIn`, `SupportEmail`, `UnsubscribeURL`, `PreferencesURL`. For `auth.reset_password`: the same set plus `ResetURL` and `RequestIP`. For `auth.admin_invite`: `AppName`, `UserName`, `InviteURL`, `ExpiresIn`, `InviterName` (optional), `SupportEmail`, `UnsubscribeURL`, `PreferencesURL`.

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

- `GET /v1/notifications` — paginated delivery log with filters by category / status / channel
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

Get the service via `module.GetTyped[iface.NotificationSender](deps.Services, module.ServiceNotificationSender)`. Auth treats it as optional — if the lookup returns `(nil, false)`, the auth module still works but signup returns `503` when `AUTH_REQUIRE_EMAIL_VERIFICATION=true`.

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

## Related

- [Root CLAUDE.md](../../../../CLAUDE.md) — module map and architecture
- [`pkg/sdk/iface/interfaces.go`](../../../pkg/sdk/iface/interfaces.go) — `NotificationSender` interface definition
- [`docs/site/architecture/authentication-flow.mdx`](../../../../docs/site/architecture/authentication-flow.mdx) — how auth consumes this module
