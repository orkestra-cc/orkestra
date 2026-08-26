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
| D6 | Legacy flat keys survive as the env-bootstrap path and as the synthesized `_legacy` profile, used until some profile declares a pattern. |
| D7 | Pre-flight checks become category-aware via an **optional companion interface**; `IsConfigured(ctx)` keeps its meaning. |

## Design

### Components

Four additions, all under `internal/core/notification/services/`.

| Component | Responsibility | Depends on |
| --- | --- | --- |
| `SenderProfile` | struct decoded from the record list: identity + credentials of one sender | — |
| `EmailDriver` + registry | `Name() / Requires() []ProfileRequirement / Send(ctx, SenderProfile, EmailMessage) error`; a package-level `ValidateProfile(driver, profile, view)` checks a profile against `Requires()` — `RuntimeView` (every requirement) for the send path and the pre-flight, `SaveTimeView` (non-secret requirements only) for the save-time gate. One declaration, two readers, so the two checks cannot drift; a driver that one day needs more than "field non-empty" adds a method then, with a recorded decision. Registry holds `noop`, `smtp`, `mailup` | `SenderProfile`, `EmailMessage` (+ `Category`) |
| `SenderResolver` | `{Category, Type, TenantID}` → `SenderProfile`; also backs `IsConfiguredFor` | the profile list |
| `dispatchEmail` (modified) | resolve → validate → `driver.Send` | the three above |

`EmailMessage` gains **one** field. It is `{To, ToName, Subject, BodyText, BodyHTML}` today, which is enough to *transmit* a message and not enough to let a driver say anything about it:

```go
type EmailMessage struct {
    To, ToName, Subject, BodyText, BodyHTML string

    Category string // routing category, e.g. "auth.verify_email"
}
```

`Category` already exists at the chokepoint — `dispatchEmail` holds it while it builds the log document — so this carries data that is present rather than computing anything new, and it is the field the `CampaignCode` mapping needs. `EmailMessage` and `EmailDriver` live in `notification/services`: core-private, not `iface`, not a wire type, so no fork surface changes.

> **Deliberately not added: `Type` and `MessageUUID`.** Both were in an earlier draft and both failed the only test that matters — nothing in this design reads them. `Type` would exist for a `mailup` driver that refuses `marketing` outright, a behaviour offered and not adopted. `MessageUUID` would exist for the delivery-feedback loop, which is out of scope. Because the struct is core-private, adding either later costs a compile of one package and nothing else, so there is no compatibility argument for reserving them now — and a field carried "for later" is one no test covers and no reader can tell is dead. If the `mailup` marketing refusal is wanted, it arrives with `Type`; if the webhook loop is built, it arrives with `MessageUUID`.

The `smtp` and `noop` drivers ignore `Category`, so D6's byte-identical promise is unaffected.

The current `emailService` is not deleted: it retires **inside** the `smtp` driver. `sendSMTP`, the TLS-mode handling and the quoted-printable encoding move unchanged; only the source of the credentials changes, from the global `SettingsLoader` to the profile.

### Config shape

One new field, `email.senders` (`FieldRecordList`), in its own group on the settings rail.

| Sub-field | Type | Condition |
| --- | --- | --- |
| `provider` | enum `noop \| smtp \| mailup` | required |
| `categories` | stringList | patterns served; `*` marks the default profile |
| `from_address` | string | `DependsOn provider in [smtp, mailup]`, required within it — **not** required for `noop`, which never uses it |
| `from_name`, `reply_to` | string | `DependsOn provider in [smtp, mailup]` — `noop` reads neither (D2: every field a driver does not read is gated off) |
| `smtp_host`, `smtp_port`, `smtp_tls_mode` | string / int / enum | `DependsOn provider in [smtp]`; `smtp_host` required |
| `smtp_username`, `smtp_password` | string / secret | `DependsOn provider in [smtp]`; **optional — an anonymous relay is a supported configuration** |
| `mailup_user`, `mailup_secret` | string / secret | `DependsOn provider in [mailup]`, both required within it |

**Every `Required` here is scoped by the same `DependsOn` that governs its driver**, and that is a correctness rule, not tidiness. `Required` is a hint the *server* does not enforce, but the console enforces it hard: `useModuleConfigForm`'s resolver skips hidden fields and then rejects a visible required field that is empty, which blocks Save. A field marked required outside the states where its driver needs it makes the profile unsavable through the only UI that configures it. `from_address` is the case that bites — a `noop` profile would demand a sender address it never reads.

`from_address` uses one condition with two values rather than two conditions: per ADR-0012, `In` is an OR **within** an entry while separate entries are ANDed, so `{Key: "provider", In: ["smtp","mailup"]}` is the correct shape and no `DependsOnMatch: "any"` is needed.

`mailup_secret` being marked required is a console-side hint only — the save-time validator cannot see secrets (D5) — but that hint is precisely one of the three mitigations D5 leans on: the rail badges it as unfilled.

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
       ├─ ValidateProfile(driver, profile, RuntimeView)
       │     incomplete                  → ErrSenderNotConfigured     (fail-closed)
       ├─ driver.Send(ctx, profile, msg)
       │     msg carries Category from this scope
       └─ logDoc.Provider   = profile.Provider
          logDoc.SenderSlug = profile.Slug     ← new field, omitempty
          logDoc.Error      = sanitize(err)    ← bounded; never a vendor body
```

Every fail-closed path writes a `NotificationDoc` with `Status=failed` and the reason, so the admin delivery log answers *which* profile failed and why. A fail-closed design that cannot be diagnosed is worse than a fallback.

`TenantID` is read from the request context (`ctxauth.GetTenantID`) at the chokepoint and rides in the resolver input, where it is ignored (D4) — the point is that per-tenant routing later touches the resolver, not `dispatchEmail`.

A fork's CRM email-tracking rewriter operates on the rendered HTML **before** this block and is untouched: open/click tracking behaves identically whichever profile carries the message.

### Matching rules

**Grammar.** A pattern is exactly one of:

| Form | Meaning | Matches | Does **not** match |
| --- | --- | --- | --- |
| `*` | the default | everything | — |
| `foo.bar` | exact | `foo.bar` only | `foo.bar.baz` |
| `foo.*` | prefix | any category beginning `foo.` with **at least one** further character, at any depth — `foo.bar`, `foo.bar.baz` | the bare `foo` |

Nothing else is legal: no `*` inside a token (`auth*`), no bare `*` mid-pattern (`auth.*.google`), no pattern that is only `.`, no empty token (`a..b`). A token is otherwise **any non-empty run of characters other than `.`, `*` and `,`** — the comma is the `stringList` separator and can never be stored — and `iface` does not restrict a fork's category charset (`crm/campaign`, `vendor:event`), so the pattern grammar does not either. Malformed patterns are rejected at save time with `notification.sender_pattern_invalid`.

The bare-`foo` exclusion is deliberate and matters in practice: a fork's CRM sends `Category: "marketing"`, a single token with no dot. Only the exact pattern `marketing` reaches it — `marketing.*` does not — so a category with no dot can never be captured by accident from a prefix rule written for a namespace.

**Normalization.** Patterns are operator-typed free text, so on read each entry is trimmed and lowercased, empty entries are dropped (`"auth.*,,crm.*"` yields two patterns, not three — an empty string must never become a pattern that matches everything), and duplicates **within one profile** are collapsed silently. A repeated pattern in the same list is redundant, not an error, and rejecting it would be pedantry the operator has to work around. The category is lowercased for comparison too — but **not trimmed**: an empty category, or one carrying leading or trailing whitespace, is malformed and the resolver rejects it **before matching**, so it cannot ride the `*` profile (which would otherwise match `""`). Matching is therefore case-insensitive in both directions and never repairs a value. **One declared exception:** while no profile declares a pattern the category is not inspected at all — an empty `Category` is legal today, and D6 promises byte-identical behaviour until a profile routes. The rule is a property of routing and starts with the routing map.

Deduplication happens **before** the cross-profile uniqueness check, so a within-profile repeat can never be reported as a conflict between profiles.

**Precedence.** Among the patterns that match a category, the winner is the one requiring the **longest literal**:

| Pattern | Literal it requires | Length |
| --- | --- | --- |
| `*` | — | 0 |
| `auth.*` | `auth.` | 5 |
| `auth.x` | `auth.x` (the whole category) | 6 |

So for the category `auth.x`, the exact pattern wins 6 to 5, and `*` loses to both. For `auth.oauth.google`, `auth.oauth.*` (11) beats `auth.*` (5).

**Ties cannot happen**, which is why "exact beats prefix" is a consequence here rather than a rule that has to be stated and defended:

- Two prefix patterns matching the same category are both prefixes of it; if their literals were the same length they would be the same string, and duplicate patterns are rejected.
- An exact pattern and a prefix pattern matching the same category can never tie, because the exact one requires the entire category while the prefix one requires strictly less — the `.` and at least one character have to follow it.

The remaining rules:

- At most one profile may declare `*`, and no pattern may be declared by two profiles.
- A profile that declares **no** patterns is never selected. This is a legitimate state, not a misconfiguration: it lets an operator create and test a profile before routing any traffic to it.

The uniqueness rules are enforced at save time, not at send time — but **only once a routing map actually exists**; see §Validation for the three states and why the distinction is load-bearing.

### Compatibility and environment bootstrap

`ConfigItemField` carries **no `EnvVar`, by construction** — `config_unmarshal.go` is explicit that element sub-fields resolve stored-value → default and that *"Nothing is read from the process environment."*

So the existing flat keys stay, and they are the environment-bootstrap path. **Until some profile declares a pattern** — the roster is empty, or holds only drafts — the resolver synthesizes:

```
legacy := SenderProfile{
    Slug: "_legacy", Provider: <email.provider>, Categories: []string{"*"},
    FromAddress: <email.from_address>, … , Host: <email.smtp.host>, …
}
```

An installation that never opens the new screen behaves exactly as today; a stack configured through `SMTP_HOST` keeps working. No migration, no boot-time write, no rollback path.

**Values and secrets are decoded from one snapshot.** Today's settings closure makes nine separate `GetConfig`/`GetSecret` reads per send, each its own `FindByName`; an environment activation landing between two of them pairs one environment's host with the other's password — which for SMTP means sending the new environment's credential to the old environment's relay. The loader therefore reads the module document **once** per send and decodes the legacy keys, the roster values and the roster secrets from that document's active environment (`ActiveConfigValues` + `ActiveEncryptedValues`, decrypted through the SDK's `UnmarshalConfig`); nothing is fetched by a second read. A read or decrypt failure fails closed.

The cutover is **the existence of a routing map, not roster emptiness** — the same predicate §Validation uses. Taken literally, "empty roster" would strand every send the moment the first pattern-less profile is saved (nothing matches, `Default` fails, `auth` refuses signups), which is the opposite of what the draft state is for. So creating a first draft, or removing the last pattern, never changes which sender carries mail; a draft stays reachable by slug for the explicit-sender test send, and the legacy slug resolves only while the legacy profile is the one carrying mail. That slug is `_legacy`: the leading underscore is outside the record-list slug grammar (`^[a-z0-9]+(-[a-z0-9]+)*$`), so no roster element — not even one an operator labels "Default" — can ever mint it, and the resolver and the delivery log cannot confuse the two.

This compatibility is only real if **validation** honours it too, in two places. With an empty roster the sender rules must not fire at all, or every PATCH on a legacy install would 422 on a routing map it never asked for (see §Validation). And the `smtp` driver's `Requires()` must list exactly what `isSMTPConfigured` requires today — host, port, from address, and **not** credentials — or an anonymous internal relay stops working on upgrade (see §Driver validation contract).

### Validation

Three layers, and the middle one has a limit worth stating rather than discovering.

**Declaration (test time).** `ValidateConfigDeclarations` already runs over the real catalog in `cmd/server`'s catalog test: `Items` present, no nested record list, no `__` prefix, `DependsOn` resolving to a sibling. A schema typo fails a test.

**Save time.** `notification` implements `HasConfigValidator`. `ValidateConfig` runs on **every** PATCH to the module — including a PATCH that touches only `app.name` — so the routing rules must be scoped to the states in which a routing map exists. There are three, and conflating them breaks either legacy installs or the migration path:

| Roster | Patterns declared | Routing rules |
| --- | --- | --- |
| empty | — | **do not apply** — a legacy installation configured entirely through the flat keys |
| non-empty | none | **do not apply** — every profile is a draft, and a pattern-less profile is never selected |
| non-empty | ≥ 1 | **all apply** |

In the third state, each check has its own scope — and **every per-profile check applies only to profiles that declare at least one pattern**:

| Check | Scope | Code |
| --- | --- | --- |
| pattern is well-formed | every profile declaring ≥1 pattern entry | `notification.sender_pattern_invalid` |
| exactly one profile declares `*` | the map as a whole | `notification.sender_no_default` / `_duplicate_default` |
| no pattern claimed by two profiles | across profiles declaring patterns | `notification.sender_pattern_conflict` |
| `provider` names a registered driver | **profiles declaring ≥1 pattern** | `notification.sender_unknown_driver` |
| driver's required **non-secret** fields present | **profiles declaring ≥1 pattern** | `notification.sender_incomplete` |

A draft sitting alongside live profiles is exempt from the last two for the same reason it is never selected: nothing can route to it, so nothing it gets wrong can affect a send. Rejecting it would repeat the empty-roster mistake at a smaller scale — blocking a PATCH the operator did not intend to be about that profile. The concrete case is not hypothetical: a profile can hold a `provider` naming a driver that no longer exists, and thanks to the enum-orphan fix the console now shows that stale value **selected and visible** rather than silently snapping to the first option. The operator can see the problem; they should not be blocked from unrelated work by it, and adding a pattern to that profile is exactly when it starts to matter and exactly when it is rejected.

Grammar is the deliberate exception. A malformed pattern is checked on any profile that declares one, because a profile *with* a pattern is not a draft — the operator has stated an intent to route — and a pattern that silently matches nothing is worse than a rejection. For this purpose a profile "declares a pattern" when its normalized list is non-empty, **whether or not the entries are well-formed**; otherwise a profile whose only pattern is malformed would classify itself as a draft and escape the very check aimed at it.

Failures return `*ConfigValidationError` → 422 with stable `notification.sender_*` codes.

Two consequences worth being explicit about, because a literal reading of "exactly one `*`" produces a broken module in both:

- **An empty roster must not be validated against the synthesized default.** `notification` implements no validator today, so its flat keys have never been checked. Enforcing driver completeness on the synthesized profile would newly reject PATCHes that are legal right now — setting `email.provider = smtp` before filling `email.smtp.host` is the natural order in which an operator fills a form. With an empty roster the rules are **vacuous, not lenient**: the flat keys keep exactly today's save behaviour.
- **The first profile must be creatable.** The console stages a create and writes the element with its sub-fields at their declared defaults, so the first save of the first profile carries no patterns. Requiring `*` at that moment would make the roster impossible to leave empty — an operator could neither stay legacy nor migrate. The pattern-less state is what makes the transition passable; the rules engage on the save that first declares a pattern, which is also the first save that could route anything.

`HasConfigActivationValidator` applies the same three-state logic to the target profile before an environment is promoted, so sandbox → production cannot activate a map that is broken in the third state.

> **Limit.** The seam contract states *"Secrets are never passed: a validator must not see decrypted secret material to do its job."* The gate therefore cannot check that a driver's secret is populated — but this only bites for drivers whose secret is **mandatory**, which today means `mailup` alone. `smtp` has no such requirement (see §Driver validation contract), so there is nothing to be blind about there. A `mailup` profile missing only its secret saves cleanly and fails at send. Covered by three existing mechanisms rather than a new one: the rail's "N to fill" badge over visible required fields (the console already receives per-element secret status), `POST /v1/notifications/test` with an explicit sender, and `IsConfiguredFor` at request time, which *can* read secrets (D7).

**Send time.** Fail-closed (D5), each error logged as above.

`IsConfigured(ctx)` keeps exactly its present meaning — the default (`*`) profile resolves and is valid — and is **no longer the right question for a category-specific pre-flight**. See §Pre-flight checks.

### Driver validation contract

`driver.Requires()` — read by `ValidateProfile` — decides whether a profile is usable, and for `smtp` it must reproduce today's `isSMTPConfigured` **exactly** — otherwise D6's compatibility promise is words rather than behaviour.

| Driver | `Requires()` | Explicitly does **not** require |
| --- | --- | --- |
| `noop` | nothing | `from_address` — and the schema must agree, or the profile cannot be saved |
| `smtp` | `host`, `port`, `from_address` | `username`, `password` |
| `mailup` | `from_address`, `mailup_user`, `mailup_secret` | — |

**Anonymous SMTP relay stays supported, and this is the concrete thing "byte-identical" has to mean.** `email_service.go:97` requires only host, port and from address; `sendSMTP` calls `client.Auth` **only when `Username != ""`**. An internal MTA on a private network with no authentication is a first-class path in a self-hosted product, and a `Requires()` that listed a password would break every such installation on upgrade — silently, since the config would still look complete.

For the same reason `smtp` does not require a password *when a username is set*. Today `smtp.PlainAuth` is attempted with whatever password is stored and the server decides; adding a local requirement would be a tightening, and tightening is not this design's job.

> **One deliberate tightening, declared.** `from_address` is required on the profile element **for `smtp` and `mailup`**, while the flat `email.from_address` is not marked required in today's schema — even though `isSMTPConfigured` has always demanded it at runtime. Schema and runtime already disagree upstream, and the profile element resolves the disagreement in favour of the runtime, scoped to the drivers that actually read the value. This changes what the console badges and rejects; it does not change which sends succeed.

### Driver error contract

Whatever a driver returns ends up in `NotificationDoc.Error`, and that field is **not** private: it is served by `GET /v1/notifications` to operators, and `notification_messages` is registered as an `iface.PIIProducer`, so it also rides the GDPR export path. A vendor response body is untrusted external content and must never be persisted verbatim.

Three concrete reasons, in increasing order of how bad they are:

- **Size.** A proxy or WAF in front of the vendor answers with an HTML page, not JSON. Storing it puts kilobytes-to-megabytes of markup into a document the admin list then returns.
- **PII.** An API that echoes the request back puts the recipient address, the subject and possibly the body into an error string — widening what a DSR export contains, for data the module already stores in its own typed fields.
- **Credentials.** MailUp carries the SMTP+ user and secret **in the request body's `User` field**. A vendor error that echoes the request would write that secret into an admin-visible log. This is the case that makes the rule non-negotiable rather than merely tidy.

**And what the chokepoint returns is bounded the same way.** `dispatchEmail` and the test send return only a `DispatchError` whose text is the stored reason and whose `errors.Is` answers for the trusted sentinels alone; the driver's own error is dropped, never wrapped. `auth` logs `err.Error()` of a failed send, so a raw `fmt.Errorf("vendor: %s", body)` from a future driver would otherwise reach the backend log by a second door.

So the chokepoint sanitizes before writing, once, for every driver — and it does so **structurally**: a driver returns a `SendError` whose fields are typed (the step that failed, a numeric reply code, a failure kind from a fixed set, an HTTP status, the vendor's status and code) and which has **no free-text field**. The diagnostic is rendered from those fields at the chokepoint, every string through the allowlist below, so no driver — ours or a fork's — can hand it a remote body even by mistake: there is no field to put one in that is not allowlisted on the way out.

**The read is bounded before the parse.** Everything above is about what gets *stored*; none of it helps if the client has already pulled an arbitrary number of bytes into memory to decide. A response body is attacker-shaped input from a host we do not control — a proxy or WAF in front of the vendor can answer with megabytes of HTML, and `io.ReadAll` or a bare `json.NewDecoder(resp.Body)` would take all of it before any rule here applies.

So every response, **success path included**, is read through `io.LimitReader(resp.Body, maxBody+1)` with `maxBody = 64 KiB` — generous for a JSON acknowledgement envelope and small enough to be harmless. Reading one byte past the limit is what distinguishes "exactly at the cap" from "over it": if the limited read returns `maxBody+1` bytes the response is rejected as `body=too_large` and **never handed to the decoder**, so an oversized body costs 64 KiB rather than an allocation the size of whatever was sent. That connection's body is closed without draining — losing keep-alive on one connection is the cheaper half of that trade.

`http.Client` carries an explicit `Timeout` for the same reason: Go's default client has none, so a vendor that accepts a connection and never answers would otherwise hold the send goroutine indefinitely. Bounding bytes without bounding time only moves the problem.

| Case | Recorded |
| --- | --- |
| vendor returned a parseable envelope | `http=<status> status=<Status> code=<Code>` — **no vendor free text** |
| body exceeded the read limit | `http=<status> body=too_large` — not parsed, no byte count (we stopped reading) |
| body did not parse, or was not JSON | `http=<status> body=unparseable bytes=<n> type=<content-type>` — no content; `n` is bounded by the limit above, and `content-type` is a remote header so it goes through the same allowlist and cap |
| SMTP server rejected the message | `smtp op=<auth\|mail_from\|rcpt_to\|data\|starttls> code=<nnn>` — the numeric code from `*textproto.Error` only; **`Msg` is dropped** |
| local or transport failure (dial, TLS, timeout, cancel) | `smtp op=<…> err=<dial\|tls\|timeout\|canceled\|io>` — a kind from a fixed set, never the Go error string |

**The vendor's `Message` is never persisted.** Truncation does not make free text safe: if MailUp echoes the request into its error message — and it carries the SMTP+ user and secret in the request body's `User` field — the credential can sit inside the first 200 characters, where every cap and every control-character filter passes it straight through.

Redacting it instead was considered and rejected. Redaction is a blocklist: it works only while the vendor echoes the secret in exactly the form we hold, and it fails **silently** against URL-encoding, JSON escaping, a partial echo, or any encoding added later. A rule that can only be verified by hunting for a string we hope to recognise is not a rule.

`Status` and `Code` are kept, but structurally rather than on trust: each is passed through a `[A-Za-z0-9._-]` allowlist and capped at 64 characters, with anything else replaced by a marker. `Content-Type` is a remote header and gets the same treatment, with `/` and `+` added to the allowlist so real media types survive. So even a vendor that one day returns a sentence in `Code` cannot turn it into a channel for echoed content.

What this costs is the vendor's human-readable reason, and the cost is small: `Code` is the machine-readable form of the same thing and is what their documentation is indexed by, the full message stays in MailUp's own reporting, and the log row already carries the UUID, timestamp and sender slug needed to find it there.

**The same rule binds SMTP, and an earlier draft of this section got that wrong.** It argued that an SMTP status line is safe because SMTP authenticates through `PlainAuth` rather than by echoing a body. That reasoning is about the *mechanism* of authentication, not about what a server is free to write in a rejection: a hostile or merely broken MTA can put anything in a 5xx line, and `PlainAuth` sends `base64(\0user\0pass)`, so echoing the AUTH argument echoes the credential. `net/smtp` surfaces that line as `*textproto.Error{Code, Msg}`, and today `logDoc.Error = sendErr.Error()` stores `Msg` verbatim — a pre-existing exposure this design inherits rather than creates, and now closes.

So the rule generalizes past "no vendor free text" to its real form: **no string that came from a remote peer is ever persisted.** For a server rejection that leaves the numeric code, which is the diagnostic that matters anyway — `auth` failing with `535` says more than the server's prose. For a local failure it leaves a kind from a fixed set. Both are assembled from constants we own, and the `op` naming which step failed is ours too, so the useful half of the diagnostic survives intact.

Every field is still capped, and the whole `Error` value is capped at 512 — the caps stay as a backstop for a bug in the assembly, not as the protection, which is now the allowlists and the fixed sets.

One rule holds by construction: **no driver ever puts its own request payload into an error.** That is ours to guarantee, and it is guaranteed by never passing the request to the error path — unlike anything a peer produces, which we can only refuse to keep.

The raw body is not written anywhere else either — not to stdout, not at debug level — because the credential risk does not care which sink it reaches. It is also not needed: the vendor keeps its own copy, and the log row already carries its own UUID, its timestamp, the sender slug and the vendor's own status and code — enough to find the corresponding entry in MailUp's reporting without duplicating the payload here.

This contract is not MailUp-specific. `smtp` already stores `sendErr.Error()` today, which can carry a server's response text — external content, usually short but not guaranteed. Defining the rule at the chokepoint fixes that too, and means a fork's future driver inherits it without being told.

### Pre-flight checks

`auth` calls `IsConfigured` eight times before sending — six in `password_auth_service.go`, two in `suspicious_login_notifier.go`, and every call is a pre-flight for **one** category — each followed within a few lines by the send that names it:

| Call site | n | Guards | Category it is about to send |
| --- | --- | --- | --- |
| `password_auth_service.go:256` | 1 | signup admission → `ErrNotificationDown` | `auth.verify_email` |
| `password_auth_service.go:969`, `:1839` | 2 | password reset | `auth.reset_password` |
| `password_auth_service.go:1280` | 1 | email verification | `auth.verify_email` |
| `password_auth_service.go:1660` | 1 | new-device notice | `auth.new_device_login` |
| `password_auth_service.go:1767` | 1 | admin invite | `auth.admin_invite` |
| `suspicious_login_notifier.go:216`, `:295` | 2 | suspicious-login notices | `auth.suspicious_login`, `auth.admin_suspicious_login` |
| **total** | **8** | | |

Six rows, eight call sites — two rows carry two each. Verify with `grep -c "IsConfigured(ctx)"` over the two files rather than counting rows.

Once routing is per-category, a single global boolean is wrong in both directions: a valid default with a broken `auth.*` returns `true` and the signup is accepted against mail that then fails; a broken default with a working `auth.*` refuses registrations that would have succeeded.

`pkg/sdk/iface` therefore gains an optional companion (D7), following `HasConfigGroups`/`ConfigGroupsOf`:

```go
type CategoryConfiguredChecker interface {
    IsConfiguredFor(ctx context.Context, category string) bool
}

func IsConfiguredForCategory(ctx context.Context, s NotificationSender, category string) bool
```

The accessor returns the exact answer when the sender implements the companion and falls back to `IsConfigured(ctx)` otherwise, so a fork's own `NotificationSender` needs no change.

`IsConfiguredFor` resolves the category — with the same `TenantID` the dispatch path passes — then reports whether the profile's driver is registered and `ValidateProfile` passes with secrets in view. **No match returns false** — consistent with fail-closed, and better than today for that case: the signup is refused up front rather than accepted against mail that cannot be sent.

Unlike `ValidateConfig`, this check runs inside the module against its own configuration and **can read secrets**, so it catches precisely the class of misconfiguration the save-time gate is forbidden to see.

Core `auth` migrates all eight guards to the accessor. This lands in **PR 2**, not PR 1: until the roster is read, every category resolves to the same synthesized profile and the distinction cannot bite.

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

`CampaignCode` — MailUp aggregates messages by it for statistics — is mapped from `EmailMessage.Category`, which is why that field is carried (see §Components). The effect is that the vendor's own reporting lines up with the routing this design introduces, instead of showing one undifferentiated stream.

> **Success is an allowlist, not the absence of an error.** MailUp's REST surface is WCF-derived (`.svc` endpoints, SOAP heritage), and that family routinely answers `200 OK` with an error envelope in the body rather than a 4xx. A driver that treats the status line as the verdict would record `Status=sent` on mail that was never accepted — under fail-closed the worst failure available, because it is silent and indistinguishable from success in the delivery log.
>
> So the driver does not look for failure; it requires success, and treats everything else as a failure including shapes nobody anticipated:
>
> ```
> success  ⇔  HTTP 2xx  ∧  body within the read limit  ∧  body parses
>              ∧  Status == "done"  ∧  Code == "0"
> ```
>
> Every other combination fails — non-2xx, unparseable body, 2xx with a different `Status`, a missing field, an empty body — and each records a **bounded diagnostic**, never the vendor's body (see §Driver error contract). Written the other way round — enumerate the errors, pass otherwise — a response shape the vendor adds later becomes a silent success.
>
> The two literals `"done"` and `"0"` come from review rather than from a page this design could fetch; the wiki is JS-rendered and does not yield its schema. **PR 3 confirms them against the response schema in the same pass that reads the request field names**, and the predicate above is what the test asserts either way — if the literals differ, only they change.

> **A success means accepted, not delivered.** The vendor is explicit that *"all methods used to send emails are asynchronous, hence a successful return code means that the request was correct."* So `Status=sent` in the delivery log records that MailUp accepted the message, and nothing about what happened afterwards. That is the same guarantee the SMTP driver gives when a relay returns 250, so the log stays consistent across drivers — but it is the reason the deferred delivery-feedback loop is the only thing that could ever make that column mean *delivered*.

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
| `SenderResolver` | table-driven, no infrastructure | longest-literal wins, `auth.x` vs `auth.*` (6 beats 5), `auth.oauth.*` vs `auth.*`, `*` last, no match, empty roster → synthesized legacy profile, **all-draft roster → still the legacy profile, and a draft reachable by slug**. Plus the grammar: `foo.*` must **not** match the bare `foo`, must match at any depth, and `"auth.*,,crm.*"` must yield two patterns — an empty entry becoming a match-everything pattern is the failure that would route the whole install to one profile |
| `ValidateConfig` scoping | table-driven | a draft profile with an unregistered `provider` alongside a valid live one must **save**; the same profile once it declares a pattern must be **rejected**; a profile whose only pattern is malformed must be rejected rather than counted as a draft |
| Schema/driver agreement | table-driven over the declared schema | for every driver, every field the schema marks `Required` **and** visible under that provider must be one the driver's `Requires()` actually lists, and vice versa (a field the driver requires must be visible and either required or defaulted). A `noop` profile with nothing but a slug and a label must be savable — that is the regression test for the console blocking Save on a field its driver never reads |
| Pattern normalization | table-driven | trim, lowercase both sides, drop empties, collapse within-profile duplicates **before** the cross-profile uniqueness check so a repeat is never reported as a conflict |
| Driver registry + `ValidateProfile` | table-driven | unknown driver; each driver against incomplete profiles, in both views |
| `mailup` driver | `httptest.Server` | asserts method, auth placement and body shape; **`CampaignCode` carries the category through**. Success is asserted as the full predicate, and the failure table is driven from its negation: non-2xx, unparseable body, **2xx with a non-`done` `Status`**, 2xx with a non-zero `Code`, missing field, empty body, timeout. No live calls |
| SMTP error sanitization | table-driven, fake SMTP server | a server whose `535` line **echoes the base64 AUTH argument** must leave `smtp op=auth code=535` and nothing else — the regression test for the credential path this design inherited from `sendErr.Error()`; a dial refusal ⇒ `err=dial`; a hung server ⇒ `err=timeout`, never the Go string |
| Error sanitization | table-driven | a hostile `SendError` with remote text in every string field must render the fixed shape with the marker in place of the text — the chokepoint guarantee; then assert the **shape**, not the absence of a string: for a parseable envelope the recorded value must match `^http=\d+ status=[A-Za-z0-9._-]{0,64} code=[A-Za-z0-9._-]{0,64}$` and nothing else. That makes "an envelope echoing the SMTP+ secret in `Message` leaves no trace" true by the assertion rather than by searching for a secret we would have to recognise. Plus: a 5 MB HTML error page ⇒ `body=too_large`, **and the test asserts at most 64 KiB+1 bytes were read from the reader**, not merely that the markup was not stored; a body just under the limit that is not JSON ⇒ `body=unparseable bytes=…`; a `Code` containing a sentence ⇒ replaced by the marker; every field capped |
| `smtp` / `noop` drivers vs the new field | the existing tests, unchanged | both ignore `Category` — the wire output must be byte-identical, which is what makes extending `EmailMessage` safe under D6 |
| `smtp` driver | the existing 233 lines, re-pointed | the credentials' source changes, the behaviour does not |
| `ValidateConfig` | table-driven over the merged map | the three states above are the point of the table: **an empty roster must accept a PATCH that touches only `app.name`** (the legacy-install regression), a roster of pattern-less drafts must save, and the save that first declares a pattern must demand a `*`. Plus a test that **documents the secret-blind limit** rather than leaving it implicit |
| `ValidateConfigActivation` | table-driven over the target profile | same three states; a map broken in the third must not be promotable |
| `IsConfiguredFor` | table-driven | valid default + broken `auth.*` ⇒ **false** for `auth.*` and true for the default (today's global boolean gets both wrong); unmatched category ⇒ false; a `mailup` profile with a secret-only gap ⇒ false, which `ValidateConfig` cannot see |
| `smtp` driver `Requires()` | table-driven | **an anonymous relay — host + port + from, no credentials — must validate**, as `isSMTPConfigured` does today; missing host, port or from must not. This is the regression test for D6 |
| `IsConfiguredForCategory` accessor | table-driven | a sender implementing the companion gets the exact answer; one that does not falls back to `IsConfigured` — the fork-compatibility guarantee |
| `auth` guards | existing auth tests, re-pointed | all eight call sites ask for the category they are about to send; the table above is the checklist |
| `dispatchEmail` | the existing 562 lines, unchanged | they are the compatibility safety net |
| Compatibility | new | empty roster + flat keys only ⇒ byte-identical send behaviour to today |

## Implementation shape

Six PRs against upstream, each green and mergeable alone. Per repo convention every code PR carries its own `CLAUDE.md` updates in the same commits; the one documentation PR at the end (PR 5) is the docs **site**, which lives in `docs/site/` and is rendered separately.

| PR | Content | Notes |
| --- | --- | --- |
| **0** | ADR-0019 + this spec + the implementation plan | docs only; merges before any code so PRs 1-5 can cite it |
| **1** | `SenderProfile`, `EmailDriver`, registry with `noop` + `smtp`, resolver that always returns the synthesized legacy profile; `dispatchEmail` routed through it; the single-snapshot loader | **no routing change** — the wire output is pinned byte for byte. Two declared behaviour changes ride with it because the SMTP driver is rewritten anyway: the send error contract (bounded diagnostics stored, returned and logged; no server text) and an SMTP exchange deadline (context or 30 s) where today a silent relay hangs the goroutine forever |
| **2** | `email.senders` record list, its rail group, `ValidateConfig`, `ValidateConfigActivation`; resolver starts reading the roster with the legacy fallback; **`CategoryConfiguredChecker` + accessor in `pkg/sdk/iface`, and `auth`'s eight guards migrated to it**; **`sender` on `POST /v1/notifications/test`** | first behavioural change, and everything D5 depends on has to be in it. The pre-flight must land in the same PR that lets profiles diverge or the guards are wrong for a merge window — and the explicit-sender test-send is the *only* mitigation that actually proves a profile, so shipping routing without it leaves fail-closed unverifiable. EN/IT i18n (parity test) |
| **3** | `mailup` driver against `POST https://send.mailup.com/API/v2.0/messages/sendmessage` | no spike — endpoint, auth model and method are settled; only the body field names are read from the vendor page. One file behind the registry, so a vendor API change touches nothing else |
| **4** | sender filter + `senderSlug` on `GET /v1/notifications`, and the delivery-log column | diagnostics, not mitigation: at PR 2 a failed send already names the profile in the log's reason string, so this improves triage rather than enabling it |
| **5** | `docs/site/modules/core/notification.mdx` and `docs/site/operating/notifications.mdx` | the operating page currently teaches pointing the single SMTP at SES/SendGrid and must be rewritten around profiles |

**Gates to remember.**

- **OpenAPI.** Declaring `email.senders` produces **no** spec diff: per [ADR-0012](../../adr/0012-module-config-group-contract.md), `ConfigField`/`ConfigGroup` are the frozen shapes, and declaring *data* against them is not a shape change. So in PR 2 `openapi-check` acts as a **guard that nothing moved**, and a diff there is a signal to stop and find out why. PR 2 still needs `make openapi-dump` — but for the `sender` field on the test endpoint, which *is* a wire change. PR 4 needs it for the new query parameter and response field.
- **Error codes.** `backend-errquality` runs against a baseline, so the new `notification.sender_*` codes must be accounted for.
- **Coverage.** The gate applies to every PR.
- **Docs site.** Render `docs/site/**` locally before merging PR 5 — nothing in this repo's CI builds the site.
