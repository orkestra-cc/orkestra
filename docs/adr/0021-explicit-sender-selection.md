---
title: ADR-0021 — Explicit per-send sender selection under operator policy
status: accepted
public: true
---

# ADR-0021 — Explicit per-send sender selection under operator policy

| Field | Value |
|---|---|
| **Status** | ✅ Accepted — adopted 2026-09-11 |
| **Date** | 2026-09-11 |
| **Authors** | @salvatore.balestrino |
| **Amends** | [ADR-0019](0019-notification-multi-sender.md) D1 only — narrows its "`iface` does not change" rejection to the case D1 was actually arguing against. D2–D7 (profile shape, driver seam, per-installation/per-environment scope, fail-closed validation, legacy-key bootstrap, category-aware pre-flight) are unchanged; this ADR builds directly on all of them. |
| **Related** | [ADR-0019](0019-notification-multi-sender.md) (sender profiles, category routing, the driver seam, and the companion-interface idiom this ADR reuses); [ADR-0012](0012-module-config-group-contract.md) (`recordList` and the config-field contract `allowed_types` is declared through) |

## Context

ADR-0019 gave the `notification` module sender profiles and category-based
routing. Its D1 considered and rejected a sender field on the `iface` request
DTOs (`NotificationRequest`, `TemplatedNotificationRequest`): *"it puts the
choice of sender in the calling code, so changing which vendor carries password
resets becomes a code edit and a deploy instead of a setting, and every fork's
addon has to be taught the profile vocabulary to route anything."* That
objection is correct for the decision D1 was actually protecting: **which sender
carries a category of system mail** — `auth.verify_email`,
`auth.reset_password` — must stay an operator setting, never a value a module's
Go code hard-codes and redeploys to change.

There is a second decision D1's routing model cannot express at all.

An installation may configure **several profiles eligible for the same workload
class**. Two marketing senders on different domains, with different `From:`
labels, different reputations, and different purposes, are not a hypothetical:
an operator who runs both an events list and a newsletter wants them
separable — separately warmed, separately damaged, separately identifiable in
the recipient's inbox. Category routing answers *"which sender serves the
`marketing` workload class"*. It has no mechanism to answer *"which of the
several senders eligible for `marketing` should **this** send use"*.

Forcing that second answer through category patterns costs one of two things,
and neither is acceptable:

- **Collapse the eligible profiles into a single pattern**, which defeats the
  entire reason to have configured more than one.
- **Grow the routing table with one pattern per workload instance** — an
  unbounded, operator-authored, constantly-changing index that just restates
  what the calling object already records, and that nobody can keep correct.

The choice belongs to the person composing the send, made once, from a list of
senders the operator has already declared eligible — not to code, and not once
per deploy.

D1's rationale targets a caller's *code* choosing a sender at compile time. An
operator picking a sender, per send, from a policy-bounded list the
`notification` module still validates and can refuse, is a different shape of
decision. This ADR adds that shape without reopening any of D1's actual
concerns: no existing `NotificationSender` implementation is required to change
(the field defaults to empty, preserving today's routing exactly), and the
vocabulary a caller must learn in order to use it is one string it already
has — a slug the directory in D6 hands back.

## Decision

### D1 — `Sender` joins the `iface` request DTOs; empty is byte-identical to today

`Sender string` is added to both `NotificationRequest` and
`TemplatedNotificationRequest`. `Sender == ""` takes exactly today's
category-routed path — no consumer that leaves the field unset (every existing
caller) changes behavior. A non-empty value names a profile slug explicitly,
and the dispatch chokepoint re-validates it in full (D3); it is never trusted
as pre-checked, however the caller obtained it.

### D2 — Eligibility is declared per profile: `allowed_types`

A profile gains `allowed_types`, a `FieldStringList` config field constrained
to `{marketing, transactional}`. An unknown value is rejected at save with
`notification.sender_bad_allowed_type`.

**Empty `allowed_types` means the profile is never explicitly selectable** — it
still serves category routing exactly as before, but no caller may name it by
slug. That is the default, and it is what every profile that predates this ADR
carries, so the new field cannot change the behavior of an existing
installation until an operator fills it in.

A **non-empty** `allowed_types` makes the profile **load-bearing non-secret
config at save time**: the same completeness check ADR-0019 D5 already runs for
a pattern-routed profile (registered driver, required non-secret fields
present) now also runs for an explicitly-selectable one. The rule behind both
is one rule — a profile that *nothing* routes to and *nothing* can pick by name
is inert and need not be sound, and a profile that either mechanism can reach
must be save-time sound the same way. Secrets stay outside the save-time gate
(D5's known limit is unchanged) and are still verified only at send or via
test-send.

This is why `ValidateSenderConfig` splits into two scopes rather than one.
Routing rules (`sender_no_default`, `sender_duplicate_default`,
`sender_pattern_conflict`, pattern grammar) stay gated on a non-empty routing
map, exactly as ADR-0019 D5 requires; driver and completeness rules now run for
any profile either mechanism can reach. A roster of one selectable,
pattern-less profile therefore does **not** trip `sender_no_default`: nothing
routes, so the legacy bootstrap path (ADR-0019 D6) still carries the mail.

### D3 — Dispatch re-enforces eligibility; it never trusts the caller

When `Sender != ""`, the chokepoint runs, in order: slug grammar (the
`FieldRecordList` grammar) and a ≤64-character bound → `BySlug` → `Type ∈
AllowedTypes` → the resolved driver reports usable. Any step failing writes the
failed delivery row through the same bounded-error contract ADR-0019's
Consequences already established: no vendor free text, no host, no credential,
ever persisted.

This is enforcement, not a second opinion on `SenderDirectory.PreflightDelivery`
(D6). A caller that skipped pre-flight, or whose eligible list went stale
between pre-flight and send, gets the same guarantee as one that never
pre-flighted at all.

### D4 — Malformed input records as the constant marker `invalid`, never a hash

`NotificationDoc` gains `AttemptedSenderSlug`. On a request that passes the
grammar/length guard it is the validated slug verbatim. On a request that fails
the guard — malformed, attacker-controlled, or simply too long —
`AttemptedSenderSlug` is the **constant marker `"invalid"`**, not a hash of the
input.

An unsalted digest of attacker-controlled text is not worth the correlation it
would buy: it names no profile (there isn't one), and the bounded failure
reason already on the row says why the attempt failed. Recording anything
derived from the raw input would risk exactly the free-text leak ADR-0019's
Consequences closed for driver errors.

### D5 — Six sentinel errors name every way a sender choice can fail

`pkg/sdk/iface` gains six sentinel errors, additive, so a consumer can match
them with `errors.Is` **without importing `internal/core/notification`**:

- `ErrSenderInvalid` — fails grammar or length, before any lookup.
- `ErrSenderNotFound` — grammar-valid slug, no matching profile.
- `ErrSenderNotEligible` — profile exists, `Type ∉ AllowedTypes`.
- `ErrSenderNotConfigured` — profile exists and is eligible, but its driver is
  unregistered or its required fields are incomplete.
- `ErrNoSenderForCategory` — empty `Sender` (category routing) resolves to no
  profile at all.
- `ErrSenderUnavailable` — the directory/config plane itself could not be read.
  Distinct from every error above, none of which was actually evaluated.

The module's own sentinels stay unexported from this seam: the explicit-sender
arm of dispatch maps its internal error to one of these six before failing, so
the value a consumer sees across the module boundary is always one it can name.

How a consumer projects these onto its own error codes and HTTP statuses is the
consumer's decision. This ADR fixes the sentinel set that such a mapping is
built from, not the mapping.

### D6 — `SenderDirectory`: an optional companion for listing and pre-flighting

Mirroring ADR-0019 D7's `CategoryConfiguredChecker` idiom — an optional
companion interface asserted from the same registered
`ServiceNotificationSender`, no new `ServiceKey` — `notification` exposes:

```go
type SenderInfo struct {
	Slug, Label, Provider, FromAddress string
	Ready                              bool // preflight passes now
}

type SenderDirectory interface {
	// All profiles whose allowed_types contains typ, Ready computed.
	// Identity only — no secrets, hosts, usernames.
	ListEligibleSenders(ctx context.Context, typ string) ([]SenderInfo, error)

	// Preflights the whole delivery path a send would take:
	//   sender == "": category routing — Resolve(category, typ) → usable
	//                 driver (ErrNoSenderForCategory when nothing routes it);
	//   sender != "": grammar → BySlug → allowed_types → usable driver.
	// Returns nil or one of the iface Err* sentinels above.
	PreflightDelivery(ctx context.Context, sender, category, typ string) error
}
```

`Ready` and `PreflightDelivery` both evaluate the **whole** delivery path —
explicit sender or category routing — not just slug lookup, so a caller gets
one answer to "would this send actually go out" regardless of which path the
send takes.

`SenderInfo` is deliberately identity-only. A picker rendered to an operator
needs a label and a `From:` address to make the choice meaningful; it has no
business receiving the transport side of a profile, and a companion that
returned one would make every consumer a place a credential can leak from.

A directory read that cannot reach config returns `ErrSenderUnavailable`. It
never degrades to an empty list, which a caller would read as "no senders
configured" rather than "the question could not be answered".

### D7 — A consumer that holds a sender across a long-running send snapshots the slug, not a fingerprint

Some consumers choose a sender once and then send to many recipients over
minutes or hours. What such a consumer persists at the moment of the choice is
a decision with two defensible answers, and this ADR picks one:

- **Chosen — slug only.** Editing the profile to another *valid* configuration
  (a new `From:` name, rotated credentials, a different provider on the same
  slug) applies to the run's remaining recipients. This is the same semantics a
  template edit already has mid-send today: the run follows the identity's
  current definition, not a copy frozen at the start. Deleting the profile, or
  editing it into ineligibility (removing the type from `allowed_types`,
  breaking its required fields), **fails closed per recipient** — each
  remaining send re-validates through D3, and the recipient's delivery row
  records the bounded reason. Recipients already sent are unaffected, and
  nothing is silently rerouted.
- **Rejected — fingerprint or version snapshot.** Capturing a content hash or
  revision of the profile at the start, so a mid-run edit — valid or not —
  never affects recipients still in flight. This is YAGNI for the problem this
  ADR solves, and it multiplies the states a run can be in against a profile
  that has since changed. It is recorded here as the explicit revisit point: if
  per-run immutability of sender identity ever becomes a real requirement, this
  is the ADR to amend, not a decision to make silently inside a worker.

### D8 — Tenancy: profiles stay installation-global; segregation is a named follow-up

Sender profiles remain **installation-global**, matching `notification`'s
posture unchanged since ADR-0019 D4: the resolver's `TenantID` parameter is
reserved and **ignored**, so category-routed mail from every internal tenant
already leaves through these same global identities today. Explicit selection
widens the *choice* and the *visibility* (label, `From:` address) available to
a caller — it does not create a send capability, or a visibility, that category
routing did not already grant implicitly. Nothing about D7's snapshot
semantics, D3's enforcement, or D5's sentinel set assumes single-tenancy.

**Normatively:** a deployment with several internal tenants that requires
identity segregation between them **MUST NOT** set `allowed_types` on a profile
that needs segregating until the follow-up below lands. Leaving `allowed_types`
empty on such a profile keeps it reachable only through category routing —
today's behavior, unaffected by this ADR. Setting it makes the profile
explicitly selectable, and its label and `From:` address visible, to every
internal tenant that can name a sender. That is the exposure this rule closes
off until the mechanism exists to scope it.

**The named follow-up** is an `allowed_tenants` profile field, mirroring
`allowed_types` in shape, enforced in three places: `ListEligibleSenders`
(never listed to an ineligible tenant), `PreflightDelivery` (never passes for
one), and at dispatch itself via the request context's tenant (never sent for
one — closing the gap a UI-only restriction would leave open to a direct API
call). That is the point at which ADR-0019 D4's reserved-but-ignored `TenantID`
finally gets consumed.

It is not built now — there is no deployment in the base exercising it, and an
unexercised authorization boundary is a boundary no test can prove correct. It
is recorded as a decision rather than left as an accident a multi-tenant
adopter would have to discover by reading code.

### D9 — Scope boundary

This ADR covers **synchronous delivery only**. `NotificationResult.Status ==
"queued"` and any asynchronous delivery flow are out of scope; nothing here
proposes to change how a consumer treats a status it does not handle.

Data-protection duties triggered by an operator changing which vendor a given
workload's mail routes through are **operational, not architectural**. This ADR
makes the choice possible, bounded, and auditable — a consumer that records the
change through the audit sink ([ADR-0009](0009-core-compliance-module.md)) gets
a durable trail of who switched what and when. Updating a Data Processing
Agreement or a Record of Processing Activities when an operator switches
providers is a process the operator runs, not a behavior the platform enforces.

## Consequences

- **A consumer gets explicit sender choice without importing `notification`.**
  Every new surface — the DTO field, the six sentinels, the `SenderDirectory`
  companion — lives in `pkg/sdk/iface`; the module boundary ADR-0006 drew stays
  intact, and a fork's optional module consumes it through the same seam the
  core does.
- **No existing caller changes behavior.** `Sender` defaults to empty on every
  DTO; `auth`'s eight pre-flight guards (ADR-0019 D7) and every other consumer
  of `NotificationSender` are unaffected.
- **A profile author now makes two independent declarations, not one.**
  `allowed_types` (explicit selectability) is orthogonal to a profile's routing
  patterns (category eligibility, ADR-0019 D2) — a profile can serve either,
  both, or neither. Getting both right is part of the save-time gate rather
  than something discovered at send.
- **A selectable, pattern-less profile is a supported shape.** It intercepts no
  category, so it cannot take mail from the profile that carries `*`, and the
  "exactly one `*`" rule does not apply to it. This is the shape a second
  sender for the same workload class takes.
- **Mid-send edits keep template-edit parity, at the cost of per-run
  immutability** (D7). An operator fixing a broken profile mid-flight fixes the
  remaining sends without restarting; an operator breaking one fails the
  remaining recipients closed, per recipient, with a bounded reason.
- **Global profiles mean explicit selection is a visibility change, not a new
  capability** — for as long as a deployment has one internal tenant. The
  moment it has several and needs them segregated, D8's constraint is binding
  until D8's follow-up is built. This ADR does not defer the *decision*, only
  the *implementation*.
- **A pre-flight is advisory, and callers must be told so.** `PreflightDelivery`
  can go stale between the check and the send. Dispatch re-validates in full,
  and a consumer must treat a send failure as authoritative even after a green
  pre-flight.

## Alternatives considered

- **Reopen ADR-0019 D1 wholesale** — let any caller name any profile, with no
  eligibility gate. Rejected: this is exactly the risk D1 was written to
  prevent. A caller-named sender with no operator-declared boundary would let a
  caller route through a profile provisioned for transactional mail,
  reintroducing the reputation-mixing problem ADR-0019 exists to solve.
  `allowed_types` (D2) is the boundary that makes explicit selection safe to
  reopen at all.
- **Express per-send choice as more category patterns** — one pattern per
  workload instance, or per sender-per-class. Rejected in Context: it turns the
  routing table into an operator-authored index of the caller's own objects, an
  unbounded and constantly-changing config surface duplicating what the caller
  already records, for a decision that belongs on the calling object rather
  than in `notification`'s config.
- **A second `ServiceKey` for the directory** instead of an optional companion
  asserted off `ServiceNotificationSender`. Rejected for the reason ADR-0019 D7
  gives for `CategoryConfiguredChecker`: a second key is a second thing to
  register, a second thing to miss, and a second failure mode for a capability
  that is a narrowing of one object, not a different object.
- **Fingerprint or version snapshot for mid-send sender identity** (D7).
  Rejected as YAGNI against the current requirement; recorded as the named
  revisit point rather than silently decided in a worker implementation.
- **Build `allowed_tenants` now, alongside `allowed_types`** (D8). Rejected:
  with no deployment in the base exercising a multi-internal-tenant boundary,
  it would be unexercised authorization code that no test can prove correct
  against a real boundary. The normative constraint in D8 stands in for it
  until a deployment that needs it builds it.
