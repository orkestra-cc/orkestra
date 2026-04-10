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
| SMTP transport         | `services/email_service.go`                |
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
| `notifications`                     | `uuid` unique, `recipientUserUuid`, `category`, `idempotencyKey` | 90 days on `createdAt` |
| `notification_templates`            | `uuid` unique, compound `templateId+locale` unique | — |
| `notification_preferences`          | compound `userUuid+category+channel` unique       | — |
| `notification_suppressions`         | `address` unique                                  | — |
| `notification_unsubscribe_tokens`   | `uuid` unique, `tokenHash` unique                 | 30 days on `expiresAt` |

## Lifecycle

- **Init**: constructs repositories, loads email settings via a closure over `ConfigService` (so admin UI changes propagate without restart), wires the `NotificationService` and registers it as `ServiceNotificationSender`.
- **Start**: calls `TemplateService.SeedDefaults(ctx)` which inserts the `auth.verify_email` and `auth.reset_password` system templates into the DB if they are missing. Source strings live in `services/default_templates.go` as Go constants.
- **Stop / HealthCheck**: inherit base no-op from `BaseModule`.

## Settings (loaded lazily per send)

All settings live in the `module_configs` collection under the `notification` module name (AES-256-GCM for `email.smtp.password`). Env vars act as bootstrap fallbacks.

| Config key                    | Env var                        | Default   |
| ----------------------------- | ------------------------------ | --------- |
| `email.provider`              | `NOTIFICATION_EMAIL_PROVIDER`  | `noop`    |
| `email.from_address`          | `NOTIFICATION_EMAIL_FROM`      | —         |
| `email.from_name`             | `NOTIFICATION_EMAIL_FROM_NAME` | `Orkestra` |
| `email.reply_to`              | `NOTIFICATION_EMAIL_REPLY_TO`  | —         |
| `email.smtp.host`             | `SMTP_HOST`                    | —         |
| `email.smtp.port`             | `SMTP_PORT`                    | `587`     |
| `email.smtp.username`         | `SMTP_USERNAME`                | —         |
| `email.smtp.password`         | `SMTP_PASSWORD` *(secret)*     | —         |
| `email.smtp.tls_mode`         | `SMTP_TLS_MODE`                | `starttls` (options: `starttls`, `tls`, `none`) |
| `app.name`                    | `APP_NAME`                     | `Orkestra` |
| `app.support_email`           | `SUPPORT_EMAIL`                | —         |

The `noop` provider logs rendered mail to the backend stdout instead of dialing an SMTP server — use it in dev and CI. The module reports `IsConfigured() = true` for `noop` so consumers can still make send calls without failing.

## Templates

System templates live as Go string constants in `services/default_templates.go`. On first boot they are seeded into the DB; afterwards the DB is the source of truth. Admins can override them via `PUT /v1/notifications/templates/{id}` which flips `isSystem` to `false`. Deleting an override with `DELETE` calls `SeedDefaults` again and the default comes back.

Rendering uses Go's `text/template` for the subject and plain-text body, and `html/template` for the HTML body (contextual escaping). The orchestrator automatically injects three variables into every templated send:

- `{{.UnsubscribeURL}}` — absolute URL to `/v1/notifications/unsubscribe?token=<raw>` with a fresh per-send token
- `{{.PreferencesURL}}` — absolute URL to `/account/notifications`
- `{{.AppName}}`, `{{.SupportEmail}}` — from module config, if not already provided by the caller

Each system template documents its expected variables in the `variables` array of the seeded document. For `auth.verify_email`: `AppName`, `UserName`, `VerifyURL`, `ExpiresIn`, `SupportEmail`, `UnsubscribeURL`, `PreferencesURL`. For `auth.reset_password`: the same set plus `ResetURL` and `RequestIP`.

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
- `POST /v1/notifications/test` — send a test email using the current SMTP settings; useful for verifying the admin config before trusting it with real auth mail
- `GET /v1/notifications/templates` — list all templates
- `GET /v1/notifications/templates/{templateId}?locale=en` — fetch a single template
- `PUT /v1/notifications/templates/{templateId}` — override a template (sets `isSystem=false`)
- `DELETE /v1/notifications/templates/{templateId}?locale=en` — delete an override; next `Start()` reseeds the system default

## Service contract for consumers

Consumers depend on `iface.NotificationSender` from `shared/iface`, not on any package inside this module. The interface has three methods:

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

- **Never bypass the orchestrator.** Don't call `EmailSender.Send` directly from another module — always go through `NotificationSender.Send` or `SendTemplated` so preferences, suppressions, idempotency and the delivery log all fire.
- **Never hardcode templates in consumers.** Add a new `TemplateID` constant, a seed entry in `default_templates.go`, and document the variable contract. Consumers pass a `map[string]any` and the module renders.
- **Secrets stay encrypted.** `email.smtp.password` is a `FieldSecret` — ConfigService encrypts it at rest with AES-256-GCM. Never read it from plain env after bootstrap; always go through `deps.GetSecret`.
- **Transactional and marketing are different types.** Set `Type: "transactional"` for mail the user cannot opt out of (auth flows, invoices, legal notices). Marketing mail must set `Type: "marketing"` or preferences won't be honored.
- **Idempotency keys are the caller's responsibility.** Include one whenever a retry could legitimately happen — it's the only protection against duplicate sends.

## Related

- [Root CLAUDE.md](../../../../CLAUDE.md) — module map and architecture
- [`shared/iface/interfaces.go`](../../shared/iface/interfaces.go) — `NotificationSender` interface definition
- [`docs/Authentication_flow.md`](../../../../docs/Authentication_flow.md) — how auth consumes this module
