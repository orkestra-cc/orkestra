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

A send is routed by matching its `Category` (`auth.verify_email`, `crm.campaign`, …) against patterns the operator declares on each profile. Precedence is **most-specific-match-wins**; `*` is the default, and at most one profile may claim it (D5 states when that becomes *exactly* one).

`iface.NotificationSender` and its two request DTOs are **unchanged**. No consumer learns that profiles exist, no fork's addon needs a code change on sync, and no *routing* decision reaches a caller. (D7 adds an **optional companion interface** for pre-flight checks — the `NotificationSender` contract itself is still untouched, and a sender that does not implement the companion keeps working.) Which sender carries which mail becomes an operator decision made on a screen, not a developer decision made in a deploy.

Rejected: adding a `SenderProfile` field to the DTOs. The mechanical cost is small — `iface`'s request structs are in-process Go, not wire types, so there is no OpenAPI diff, and adding a field to a struct is source-compatible. The cost is semantic, and larger: it puts the choice of sender in the calling code, so changing which vendor carries password resets becomes a code edit and a deploy instead of a setting, and every fork's addon has to be taught the profile vocabulary to route anything.

### D2 — A profile is transport **and** identity, declared as one `recordList`

One new config field, `email.senders` of type `FieldRecordList`. Each element carries `provider`, the pattern list it serves, its own `from_address` / `from_name` / `reply_to`, and the transport fields for its provider. Every field a given driver does not read is gated off by `DependsOn` — including `from_address`, which `noop` never uses. That is not tidiness: the console rejects an empty visible required field, so a `Required` declared outside the states where its driver needs it makes the profile unsavable through the only UI that configures profiles.

Identity travels with the profile because identity *is* the point: separate credentials behind a shared `From:` would isolate nothing.

The routing rules live **on the profile** rather than in a second list. One surface, one validation pass, and no ordering for the operator to maintain — specificity is computed, not declared.

### D3 — The driver seam is real and populated: `noop`, `smtp`, `mailup`

`EmailDriver` (`Name` / `Validate(profile)` / `Send(ctx, profile, msg)`) with a registry. The existing SMTP transport retires *inside* the `smtp` driver unchanged; only the source of its credentials moves. A `mailup` driver for MailUp's SMTP+ REST API ships in the base.

`Validate` is where D6's compatibility promise is either kept or quietly broken, so it is pinned per driver. **`smtp` requires host, port and from address — and not credentials**, reproducing `isSMTPConfigured` exactly: today `sendSMTP` authenticates only when a username is set, so an unauthenticated internal MTA is a supported configuration and not an edge case in a self-hosted product. A driver that demanded a password would break every such installation on upgrade, and would break it silently, because the configuration would still look complete. `mailup` genuinely requires its user and secret — its API cannot function without them.

`EmailMessage`, the struct `Send` receives, grows by exactly one field: the routing category, which already exists at the chokepoint and is what MailUp's `CampaignCode` needs. The struct is core-private — not `iface`, not a wire type — so extending it changes no fork surface, and that same property is the reason nothing further is reserved now: a field added when a requirement appears costs one package recompile, while a field carried in advance is one no test covers and no reader can tell is dead.

This is the decision that strains ADR-0006 most, and it was taken deliberately — see §Consequences.

### D4 — Profiles are per-installation, per-environment; the resolver already accepts a tenant

Profiles live in `module_configs` like every other module setting, so they inherit ADR-0012's sandbox/production profiles and AES-256-GCM secret encryption at no cost. No new collection, no new RBAC surface, no new GDPR cascade.

The resolver's input carries a `TenantID` that is **ignored today**. Per-tenant senders are a real future requirement; reserving the parameter costs one struct field now and avoids reopening the chokepoint later.

### D5 — Resolution failure is fail-closed, and the gate that makes that safe runs at save time

If no profile matches, or the matched profile's driver rejects it as incomplete, **the send fails**. It is never silently rerouted. Falling back to the default profile would push promotional mail through the transactional sender — violating the vendor terms that motivated this ADR and burning the reputation of the domain it exists to protect.

Fail-closed is only safe if misconfiguration is caught before it can strand a password reset, so `notification` implements `HasConfigValidator` and `HasConfigActivationValidator`. Against a **configured routing map** they require that exactly one profile claims `*`, that no pattern is claimed twice, and — **for each profile that declares at least one pattern** — that its `provider` names a registered driver and that driver's required non-secret fields are present. Violations are 422 with stable `notification.sender_*` codes.

The per-profile scope is deliberate. A profile with no patterns is a draft: nothing can route to it, so nothing it gets wrong can reach a send, and rejecting it would block a PATCH the operator did not intend to be about that profile. Pattern *grammar* is the exception, checked wherever a pattern is declared at all — a profile with a pattern has stated an intent to route, and a pattern that silently matches nothing is worse than a rejection.

**Those rules apply only once a routing map exists.** `ValidateConfig` runs on every PATCH to the module, so an unconditional "exactly one `*`" would reject a legacy installation whose roster is empty (it has no profiles at all) and would also reject the first save of a first profile (the console writes a new element with its sub-fields at their defaults, so it carries no patterns yet). Either would make the roster impossible to leave empty *and* impossible to leave — an operator could neither stay on the flat keys nor migrate off them. The rules therefore engage only when the roster is non-empty **and** at least one profile declares a pattern; below that threshold they are vacuous, and the flat keys keep exactly the save behaviour they have today. See the spec's §Validation for the three states.

**Known limit, accepted with eyes open.** The validator contract states that *"Secrets are never passed: a validator must not see decrypted secret material to do its job."* The gate therefore cannot verify that a driver's secret is populated — a profile missing only its secret saves cleanly and fails at send. The blind spot is narrower than it first looks: it bites only where a secret is **mandatory**, which today means `mailup`. `smtp` requires no credentials at all (D3), so for the driver most installations use there is nothing to be blind about. Three existing mechanisms cover the gap rather than one new one: the rail's "N to fill" badge counts visible required fields (the console already receives per-element secret status), `POST /v1/notifications/test` with an explicit sender proves the profile for real, and `IsConfiguredFor` (D7) runs at request time inside the module, where it **can** read secrets. Activation validation re-checks the structural rules on the promotion path, but it is bound by the same secret blindness and is not a fourth answer to this.

### D6 — The legacy flat keys survive as the environment-bootstrap path

`ConfigItemField` has **no `EnvVar`, by construction** — record-list elements are never seeded from the process environment. A design that migrated the flat keys into a profile would therefore delete the only way to configure email from `docker-compose`.

So `email.provider`, `email.smtp.*` and `email.from_*` stay. When the `email.senders` roster is empty, the resolver synthesizes a `default` profile from them with `categories = *`. An installation that never opens the new screen behaves **identically to today**; a stack configured by `SMTP_HOST` keeps working. No data migration, no boot-time write, no rollback path to invent.

### D7 — Pre-flight checks become category-aware through an optional companion interface

`IsConfigured(ctx) bool` is a single boolean over a single transport. Under D1 it stops answering the question its callers ask. Every one of the eight call sites in `auth` — six in `password_auth_service.go`, two in `suspicious_login_notifier.go` — — signup admission (`ErrNotificationDown`), password reset, email verification, new-device and suspicious-login notices, admin invite — is a **pre-flight for one specific category**, and each is followed within a few lines by a send that names it.

With profiles, a global boolean is wrong in both directions. A valid default and a broken `auth.*` profile returns `true`, the signup is accepted, and the verification mail then fails — leaving a user who cannot get in and an account that cannot be completed. A broken default and a working `auth.*` returns `false` and refuses registrations that would have succeeded.

Redefining `IsConfigured` as *"every routed profile is valid"* is worse than either: a misconfigured campaign sender would block signups, coupling two workloads that this ADR exists to separate.

So the check becomes category-aware, using the house idiom for extending a frozen contract ([ADR-0012](0012-module-config-group-contract.md)'s `HasConfigGroups` / `ConfigGroupsOf`, mirrored on `HasServiceContracts` / `RequiredServicesOf`): an **optional companion interface** in `pkg/sdk/iface` with a resolving accessor.

```go
type CategoryConfiguredChecker interface {
    IsConfiguredFor(ctx context.Context, category string) bool
}

// Exact answer when the sender implements the companion; the coarse
// answer otherwise. A fork's own sender needs no change to keep working.
func IsConfiguredForCategory(ctx context.Context, s NotificationSender, category string) bool
```

`IsConfiguredFor` resolves the category, then reports whether its profile's driver is registered and `Validate` passes. A category that matches no profile returns **false** — consistent with D5, and strictly better than today for that case: the signup is refused up front rather than accepted against mail that cannot be sent.

`IsConfigured(ctx)` keeps its present meaning — the default (`*`) profile resolves and is valid — so no existing caller changes behaviour or breaks. Core's `auth` migrates all eight guards to the accessor.

One useful asymmetry falls out. The save-time gate cannot see secrets (D5), but this runtime check runs inside the module against its own configuration and **can**. So `IsConfiguredFor` catches exactly the class of misconfiguration `ValidateConfig` is forbidden to see — a `mailup` profile missing only its API secret — at the last moment before it would matter.

## Consequences

- **A fork gets workload separation without touching code.** Declaring two profiles and their patterns is enough: no addon changes, no caller of `iface.NotificationSender` changes, and a fork's own sender implementation keeps compiling — the D7 companion is optional and falls back. **Core is not in that position**: `auth`'s eight pre-flight guards migrate to the category-aware accessor (D7), and `pkg/sdk/iface` gains one optional interface. The promise is that the cost stays upstream, not that there is none.
- **Driver errors are bounded and sanitized before they are stored.** `NotificationDoc.Error` is served by the admin delivery log and rides the GDPR export path, so no driver's raw vendor response is ever persisted: the chokepoint records the HTTP status, the vendor's status and code, and a truncated message, with control characters stripped and the request payload excluded by construction. That last exclusion is load-bearing rather than cautious — MailUp carries its SMTP+ secret in the request body, so an API that echoed the request back would otherwise write a credential into an operator-visible field. The rule is defined once for every driver, which also bounds the SMTP error text the module stores today.
- **Deliverability becomes diagnosable.** Every delivery-log row already carries `provider`; it gains the sender slug, so "which sender sent this, and which one failed" is answerable per message instead of inferred.
- **The base now contains a vendor integration.** ADR-0006 collapsed Orkestra to a core-only base precisely to stop vendor verticals accumulating, and D3 puts an Italian ESP driver in the public base that every fork inherits. The alternative — the seam in core and the driver registered from a fork — was considered and rejected in favour of a single place to maintain and test. **This is a known tension, not an oversight.** If a second and third vendor driver follow, that is the signal to move drivers behind the fork seam after all, and this ADR should be revisited rather than extended.
- **Fail-closed can strand mail that a fallback would have delivered.** A profile whose secret is missing passes validation and fails at send (D5). The mitigation is procedural — use the test-send — not structural.
- **`IsConfigured` stops silently lying, and starts catching what the save gate cannot.** Under D7 a pre-flight answers for the category it is about to send, and — unlike the save-time validator — it can see secrets, so a profile missing only its API key is caught before the mail is attempted rather than after.
- **One additive interface in `pkg/sdk/iface`.** Everything else this design needs already exists: `recordList`, `DependsOn`, per-element secrets, both validator seams, the console renderer. The `NotificationSender` contract is untouched and the new companion is optional, so nothing reaches a fork as a breaking sync — a fork's own sender simply keeps getting the coarse answer.

## Alternatives considered

- **Route by `Type` (`transactional` / `marketing`) only** — two buckets, no routing config at all. Rejected: system mail and transactional mail are both `transactional` today, so a third bucket is inexpressible without editing callers — which was the cost this design set out to avoid.
- **Let the caller name the profile** (a `SenderProfile` field on the `iface` DTOs) — maximum explicitness. Rejected under D1, and **not** for the mechanical reason it is tempting to give: the request structs are in-process Go rather than wire types, so there is no spec diff, and adding a field to a struct keeps every fork compiling. The objection is semantic. It moves the choice of sender into the calling code, so changing which vendor carries password resets becomes an edit and a deploy instead of a setting, and every addon that wants to route has to learn the profile vocabulary.
- **Multiple SMTP profiles with no driver seam** — SES, SendGrid, Postmark, Resend and MailUp all expose an SMTP relay, so this covers most vendors with no new dependency and no driver to maintain. Rejected in favour of D3 so that vendors whose API offers batch send or richer per-message control are reachable later without reopening the chokepoint. Noted honestly: for MailUp specifically the vendor documents its REST API and its SMTP relay as *"virtually the same features"*, and its bounce/unsubscribe webhooks are a property of the SMTP+ **product** — available on either path — so this first driver buys little beyond proving the seam on a real vendor. That was accepted knowingly when D3 was taken.
- **Two record lists, profiles and routes, with roster-order precedence** — more explicit, and the SDK preserves roster order for free. Rejected under D2: a second list adds orphan rules, unreachable profiles, and an ordering the operator must curate, to express something specificity already decides.
- **Redefine `IsConfigured` as "every routed profile is valid"** (D7) — no new interface, no caller edits. Rejected: a broken campaign sender would then block signups, re-coupling the two workloads this ADR exists to separate — the same failure mode, wearing the opposite sign.
- **Change `IsConfigured`'s signature to take a category** — semantically exact with no companion interface. Rejected: it breaks every implementor and every caller in every fork on the next sync, to avoid one optional interface the codebase already has an idiom for.
- **Per-tenant sender profiles now** (D4) — rejected as a different and larger project: a tenant-scoped collection with its own RBAC, console surface and GDPR cascade. The resolver parameter keeps the door open.
- **Migrate the flat keys into a seeded `default` element at boot** — tidier end state, one shape instead of two. Rejected under D6: element sub-fields cannot carry `EnvVar`, so this would silently remove environment-variable configuration and require a data migration with a rollback story.
