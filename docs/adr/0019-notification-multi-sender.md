---
title: ADR-0019 — Multi-sender email delivery (sender profiles, category routing, driver seam)
status: accepted
public: true
---

# ADR-0019 — Multi-sender email delivery (sender profiles, category routing, driver seam)

| Field | Value |
|---|---|
| **Status** | ✅ Accepted — adopted 2026-08-26 |
| **Date** | 2026-08-26 |
| **Authors** | @salvatore.balestrino |
| **Supersedes** | — |
| **Related** | [ADR-0012](0012-module-config-group-contract.md) (config group contract — `email.provider` became an enum there, and `recordList` is the primitive this ADR consumes); [ADR-0006](0006-collapse-to-core-only-base.md) (core-only base — §Consequences records where this ADR strains it) |

## Context

The `notification` module owns every outbound email in Orkestra and has exactly **one** transport configuration: a single `email.provider` (`noop` or `smtp`), one set of SMTP credentials, and one sender identity (`email.from_address`, `email.from_name`, `email.reply_to`). Every consumer — `auth` verification and password-reset mail, a fork's CRM campaign worker, forms notifications — funnels through it and comes out of the same relay, from the same address, on the same domain.

That single identity is the problem, and it is not a cosmetic one.

**Reputation is per-domain and per-IP, and the two workloads are incompatible.** Verification and password-reset mail must arrive: it is the product's access path. Marketing mail is sent in bulk, to recipients who did not ask for it this minute, and it accrues complaints. Putting both behind one sender means a campaign's complaint rate degrades the deliverability of password resets — the failure is silent, gradual, and discovered when users report they never received the email.

**Vendors enforce the same split contractually.** MailUp's SMTP+ transactional product carries an explicit usage restriction — *"do not use it for promotional emails"* — and it is not alone. An installation that wants both workloads cannot honour those terms with one configured sender, whatever its own preferences.

The pieces to fix it are already in the base and unused:

- **`FieldRecordList`** (ADR-0012's `ConfigField.Items`) is a complete SDK primitive — slug minting, per-element secrets at ordinary encrypted keys, revision/CAS, staged removal, an admin handler, and a rendered `RecordListField` in the console. **No core module declares one.** It was built for fork addons.
- **`EmailSender`** is already an interface (`IsConfigured` / `Send` / `ProviderName`) behind a `SettingsLoader` closure. The abstraction exists; what is missing is that there is one loader and one `EmailSettings` rather than many.
- **`dispatchEmail`** is a single chokepoint where the category, the type, the recipient and the rendered body coexist immediately before transport — the same chokepoint a fork's CRM already hooks for open/click tracking.
- **`HasConfigValidator`** (`ValidateConfig(ctx, mergedValues) error` → 422) already lets a module reject config at the PATCH boundary, and `HasConfigActivationValidator` does the same for environment activation.

So this is not a request for new platform capability. It is a request to stop hard-coding *one* of something the platform is already shaped to hold *many* of.

## Decision

Give the `notification` module **sender profiles**: a configured list of senders, each with its own transport and its own identity, selected per send by operator-declared rules.

### D1 — The routing key is the notification **category**, and `iface` does not change

A send is routed by matching its `Category` (`auth.verify_email`, `crm.campaign`, …) against patterns the operator declares on each profile. Precedence is **most-specific-match-wins**; `*` is the default and exactly one profile may claim it.

`iface.NotificationSender` and its two request DTOs are **unchanged**. No consumer learns that profiles exist, no caller is edited, and no fork's addon needs a code change on sync. Which sender carries which mail becomes an operator decision made on a screen, not a developer decision made in a deploy.

Rejected: adding a `SenderProfile` field to the DTOs. It changes a frozen v1 SDK surface (OpenAPI diff, every fork on next sync) to move an operational decision into code.

### D2 — A profile is transport **and** identity, declared as one `recordList`

One new config field, `email.senders` of type `FieldRecordList`. Each element carries `provider`, the pattern list it serves, its own `from_address` / `from_name` / `reply_to`, and the transport fields for its provider gated by `DependsOn`.

Identity travels with the profile because identity *is* the point: separate credentials behind a shared `From:` would isolate nothing.

The routing rules live **on the profile** rather than in a second list. One surface, one validation pass, and no ordering for the operator to maintain — specificity is computed, not declared.

### D3 — The driver seam is real and populated: `noop`, `smtp`, `mailup`

`EmailDriver` (`Name` / `Validate(profile)` / `Send(ctx, profile, msg)`) with a registry. The existing SMTP transport retires *inside* the `smtp` driver unchanged; only the source of its credentials moves. A `mailup` driver for MailUp's SMTP+ REST API ships in the base.

This is the decision that strains ADR-0006 most, and it was taken deliberately — see §Consequences.

### D4 — Profiles are per-installation, per-environment; the resolver already accepts a tenant

Profiles live in `module_configs` like every other module setting, so they inherit ADR-0012's sandbox/production profiles and AES-256-GCM secret encryption at no cost. No new collection, no new RBAC surface, no new GDPR cascade.

The resolver's input carries a `TenantID` that is **ignored today**. Per-tenant senders are a real future requirement; reserving the parameter costs one struct field now and avoids reopening the chokepoint later.

### D5 — Resolution failure is fail-closed, and the gate that makes that safe runs at save time

If no profile matches, or the matched profile's driver rejects it as incomplete, **the send fails**. It is never silently rerouted. Falling back to the default profile would push promotional mail through the transactional sender — violating the vendor terms that motivated this ADR and burning the reputation of the domain it exists to protect.

Fail-closed is only safe if misconfiguration is caught before it can strand a password reset, so `notification` implements `HasConfigValidator` and `HasConfigActivationValidator`: exactly one profile claims `*`, no pattern is claimed twice, every `provider` names a registered driver, and each driver's required non-secret fields are present. Violations are 422 with stable `notification.sender_*` codes.

**Known limit, accepted with eyes open.** The validator contract states that *"Secrets are never passed: a validator must not see decrypted secret material to do its job."* The gate therefore cannot verify that an SMTP password or a MailUp secret is populated — a profile missing only its secret saves cleanly and fails at send. Three existing mechanisms cover the gap rather than one new one: the rail's "N to fill" badge counts visible required fields (the console already receives per-element secret status), `POST /v1/notifications/test` with an explicit sender proves the profile for real, and activation validation re-checks structure before an environment is promoted.

### D6 — The legacy flat keys survive as the environment-bootstrap path

`ConfigItemField` has **no `EnvVar`, by construction** — record-list elements are never seeded from the process environment. A design that migrated the flat keys into a profile would therefore delete the only way to configure email from `docker-compose`.

So `email.provider`, `email.smtp.*` and `email.from_*` stay. When the `email.senders` roster is empty, the resolver synthesizes a `default` profile from them with `categories = *`. An installation that never opens the new screen behaves **identically to today**; a stack configured by `SMTP_HOST` keeps working. No data migration, no boot-time write, no rollback path to invent.

## Consequences

- **A fork gets workload separation without touching code.** Declaring two profiles and their patterns is enough; no consumer, no addon, and no `iface` implementation changes.
- **Deliverability becomes diagnosable.** Every delivery-log row already carries `provider`; it gains the sender slug, so "which sender sent this, and which one failed" is answerable per message instead of inferred.
- **The base now contains a vendor integration.** ADR-0006 collapsed Orkestra to a core-only base precisely to stop vendor verticals accumulating, and D3 puts an Italian ESP driver in the public base that every fork inherits. The alternative — the seam in core and the driver registered from a fork — was considered and rejected in favour of a single place to maintain and test. **This is a known tension, not an oversight.** If a second and third vendor driver follow, that is the signal to move drivers behind the fork seam after all, and this ADR should be revisited rather than extended.
- **Fail-closed can strand mail that a fallback would have delivered.** A profile whose secret is missing passes validation and fails at send (D5). The mitigation is procedural — use the test-send — not structural.
- **`IsConfigured(ctx)` changes meaning compatibly**: it reports whether the default (`*`) profile resolves and is valid, which is what consumers already mean when they call it to decide whether to degrade.
- **No SDK change.** `recordList`, `DependsOn`, per-element secrets, the validator seams and the console renderer all exist. The contract in `pkg/sdk` is untouched, so nothing here reaches a fork as a breaking sync.

## Alternatives considered

- **Route by `Type` (`transactional` / `marketing`) only** — two buckets, no routing config at all. Rejected: system mail and transactional mail are both `transactional` today, so a third bucket is inexpressible without editing callers — which was the cost this design set out to avoid.
- **Let the caller name the profile** (a `SenderProfile` field on the `iface` DTOs) — maximum explicitness. Rejected under D1: it breaks a frozen v1 surface and makes changing provider a deploy rather than a setting.
- **Multiple SMTP profiles with no driver seam** — SES, SendGrid, Postmark, Resend and MailUp all expose an SMTP relay, so this covers most vendors with no new dependency and no driver to maintain. Rejected in favour of D3 so that vendors whose API offers batch send or delivery webhooks are reachable later without reopening the chokepoint. Noted honestly: for MailUp specifically the vendor documents its REST API and its SMTP relay as *"virtually the same features"*, so this driver buys little beyond proving the seam.
- **Two record lists, profiles and routes, with roster-order precedence** — more explicit, and the SDK preserves roster order for free. Rejected under D2: a second list adds orphan rules, unreachable profiles, and an ordering the operator must curate, to express something specificity already decides.
- **Per-tenant sender profiles now** (D4) — rejected as a different and larger project: a tenant-scoped collection with its own RBAC, console surface and GDPR cascade. The resolver parameter keeps the door open.
- **Migrate the flat keys into a seeded `default` element at boot** — tidier end state, one shape instead of two. Rejected under D6: element sub-fields cannot carry `EnvVar`, so this would silently remove environment-variable configuration and require a data migration with a rollback story.
