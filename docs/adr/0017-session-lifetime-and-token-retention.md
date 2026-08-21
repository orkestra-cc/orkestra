---
title: ADR-0017 — Absolute session lifetime, single-sourced token TTL, and auth retention
status: accepted
public: true
---

# ADR-0017 — Absolute session lifetime, single-sourced token TTL, and auth retention

| Field | Value |
|---|---|
| **Status** | ✅ Accepted — adopted 2026-08-21 |
| **Date** | 2026-08-21 |
| **Authors** | @salvatore.balestrino |
| **Related** | [ADR-0003](0003-three-audience-host-split.md) (three-audience host split — the per-audience refresh cookie this decision bounds); [ADR-0012](0012-module-config-group-contract.md) (module config group contract — the admin surface the new field is declared against); [ADR-0014](0014-service-accounts-client-credentials.md) (service accounts — machine principals hold no refresh token and are outside this decision) |
| **Design** | [2026-08-21 session lifetime design](../superpowers/specs/2026-08-21-session-lifetime-design.md) |

## Context

Orkestra has never bounded the total age of an authenticated session. Refresh
tokens rotate on every use and each rotation writes a fresh expiry
(`now + refreshTTL`), so the 7-day refresh window functions as an **idle**
timeout: it ends a session only after seven days without a refresh. An active
user is never required to re-authenticate. Nothing else caps the session —
`RefreshTokensWithRiskAssessment` does not consult the session document, and the
refresh-family replay fence extends with the newest token in the family.

An audit of the install-time defaults surfaced this alongside six adjacent
defects in the same area, three of which are documentation drift and three of
which are latent bugs. Two findings bear on this decision directly.

**Token lifetime had two sources that could disagree.** The auth module
documents a three-level chain — admin `accessTokenTTL`, then
`JWT_ACCESS_TOKEN_EXPIRY`, then a 15-minute default — but the policy reader
substituted the 15-minute default for "unset" before the environment value could
be consulted, making the environment variable unreachable. Separately, the Redis
session-revocation denylist derived its entry TTL from that same environment
value **once at boot**, while access tokens took their lifetime from the admin
policy **live on every mint**. Raising `accessTokenTTL` through the admin UI
therefore produced access tokens that outlived their own revocation entries: once
the entry expired, tokens belonging to a revoked session were accepted again,
silently, for the remainder of their lifetime. Logout, change-password, and
administrative session termination are all served by that denylist.

**No process purged expired authentication state.** Cleanup functions for expired
and revoked refresh tokens and for expired sessions exist in the repositories and
have never had a caller, and neither the refresh-token nor the session
collections carry a TTL index. With rotation on every refresh, the refresh
collections gain one row per refresh and never shrink. The compliance module's
retention sweep covers user tombstones only.

The decision below is scoped to the session-lifetime semantics that forks
inherit. The remaining findings are corrections toward already-documented
contracts and are recorded in the design document rather than here.

## Decision

**D1 — A session has a maximum age, enforced by default at 30 days.** A new admin
field `sessionAbsoluteTTL` (module `auth`, group `login`, default `720h`, range
1h–89d) bounds the total lifetime of a session measured from login, independently
of activity. When it elapses the user must authenticate again. An empty value
disables the cap, which is the supported exit for a fork that does not want it.
The field applies to both audience tiers; the operator console and the client
surface share one value, following the precedent that per-tier splitting is added
only when a need appears.

**D2 — The anchor is `session.StartedAt`, not a new field.** Every path that
issues credentials already creates a session document, and a failure to persist
it rolls back the refresh token that was just minted — the invariant is that no
usable credential survives without its session record. The session UUID is
preserved across every rotation, the document is retained for 90 days, and it is
addressed by a unique index. The cap therefore needs no schema change, no
backfill, and no compatibility branch, and on upgrade it signs out sessions older
than the cap because that is what the existing data already records.

**D3 — The cap is enforced on both the rotating and the non-rotating refresh
path.** `/session` bootstrap mints an access token without rotating the refresh
cookie, a deliberate anti-replay split. Enforcing the cap only on the rotation
endpoint would let a client that calls solely the bootstrap endpoint hold a
session open indefinitely, so both paths consult the same helper.

**D4 — Reaching the cap performs a logout, not a denial.** The session's refresh
tokens are revoked, the session document is marked inactive, and the session id
is pushed onto the revocation denylist — the same three steps as an
administrative termination. A bare denial would leave the in-flight access token
valid until its natural expiry and would repeat the lookup on every subsequent
request. The event is recorded on the security-event log and counted as
`orkestra_auth_session_cap_expiries_total`.

**D5 — Token lifetime has exactly one resolution chain, and the revocation
denylist outlives every token that the policy can mint.** The policy reader
reports "unset" instead of substituting a default, restoring the documented
`admin → environment → 15m` fall-through. Every denylist entry lives for the
maximum permitted access-token lifetime (24 hours) plus one minute of clock
skew. Deriving a revocation TTL from the live policy value is insufficient: if
an operator lowers the policy after minting a long-lived token, the new shorter
value would let the denylist entry expire while the old token remained valid.
The fixed upper bound closes both the increase and decrease cases without
tracking every access-token expiry per session. The shipped
`.env.example` is aligned to the same 15-minute default so that repairing the
chain does not silently lengthen anyone's access-token lifetime. The bound also
applies to `JWT_ACCESS_TOKEN_EXPIRY` and to direct `NewJWTService` callers: an
environment or constructor value above 24 hours is clamped with a warning. The
environment fallback therefore cannot mint a token that outlives the fixed
denylist window.

**D6 — Admin-supplied durations that govern credentials are validated at the
auth configuration boundary and bounded defensively at read time.**
`accessTokenTTL` (1m–24h), `passwordResetTokenTTL` (5m–24h), and
`sessionAbsoluteTTL` (1h–89d) reject malformed or out-of-range non-empty PATCH
values with 422 before persistence. Empty has field-specific meaning:
`accessTokenTTL` falls through to the environment, `passwordResetTokenTTL` uses
its 30-minute default, and `sessionAbsoluteTTL` disables the cap. The readers
still clamp legacy/out-of-band persisted values and warn, so an upgraded
deployment cannot be locked out of the admin UI by old data. This two-layer
rule prevents the stored value shown by the UI from disagreeing with the
effective value while remaining safe against direct database edits. Enforcement
lives in the `auth` module through a new optional `HasConfigValidator` module
interface. The validator runs for both the active-config PATCH and the named
environment PATCH, before encryption or persistence; neither settings surface
can bypass it. Modules that do not implement it retain today's behaviour, so
forks' addons do not break. Generic interpretation of every `ConfigField`
constraint is still a separate SDK decision.

**D7 — Expired authentication state is purged, by the mechanism each collection's
semantics call for.** Session documents carry a retention deadline in
`expiresAt`, so they get a TTL index. Refresh-token rows, revoked or active, are
never deleted while their token could still pass temporal validation; expired
rows may be deleted because replaying them cannot mint credentials. The durable
family fence remains independent. Token rows use a bounded application sweep so
deletion progress is observable: every cycle deletes at most a fixed batch per
tier. The query fetches one row beyond the batch to decide whether more work
exists without counting the whole eligible range. A short Redis lease elects one
reaper across backend replicas; failure to acquire or renew it skips maintenance
without affecting authentication. `sessionAbsoluteTTL` is capped
one day below the 90-day session retention window. Equality is unsafe because
Mongo's TTL monitor could delete the anchor at the exact cap boundary before the
refresh path evaluates it.

**D8 — New metric labels are closed schema.** Token-sweep metrics carry only
`tier ∈ {operator,client}`. Session-anchor anomaly metrics carry only
`kind ∈ {missing,zero_timestamp}`. UUIDs, collection names, configuration values,
and error strings never become Prometheus labels. This decision extends the label
schema governed by ADR-0002.

## Consequences

- **Upgrading signs out sessions older than the cap.** On the first refresh after
  deployment, any session that began more than 30 days earlier is terminated and
  the user is returned to the login screen. This is a behaviour change inherited
  by every downstream fork at its next sync and is the reason this ADR exists.
  Deployments that need the previous semantics set `sessionAbsoluteTTL` to empty.
- **A missing session row fails open only during a measured compatibility window.** D2's invariant
  makes an absent session document impossible for credentials issued by current
  code, but invariants bind only the code written after them and older rows
  cannot be assumed to comply. Until a release cycle of telemetry confirms the
  case does not occur, an absent row permits the refresh and increments a
  `orkestra_auth_session_anchor_anomalies_total{kind="missing"}`; a row present
  with no usable timestamp uses `kind="zero_timestamp"`. Repository failures are
  not compatibility misses and fail closed. Tightening to fail-closed is required
  in the first minor release after at least 30 consecutive production days with
  zero increments in every supported environment; the implementing PR must create
  and link the tracked follow-up.
- **An environment variable begins taking effect that previously did not.**
  Deployments that set `JWT_ACCESS_TOKEN_EXPIRY` by hand have been running on the
  15-minute default regardless of its value. Repairing the chain activates their
  configured value, which may be longer than what they have actually been
  running. Operators must diff that key against the shipped example before
  upgrading.
- **The first sweeps after deployment drain accumulated history gradually.** On
  an installation that has never been pruned the backlog is proportional to its
  lifetime traffic. Mongo's TTL monitor batches session deletion and the
  application reaper caps each token deletion pass; backlog, duration, and
  deletion counts must be measured on staging before promotion. A session
  document with a zero `expiresAt` serialises as a year-1 date and would be
  deleted immediately by a bare TTL index; both session indexes therefore carry
  a `{expiresAt: {$gt: 2000-01-01}}` partial filter, which removes that class
  structurally rather than by pre-flight count. Counting such rows before a
  deploy remains a useful sanity check — they can no longer be reaped either —
  but it is not a release gate.
- **Session lifetime becomes an operator-visible policy rather than an
  implementation detail.** Three lifetimes — access token, idle window, and
  absolute cap — are now separately nameable, and the idle window is documented as
  being the refresh TTL rather than an independent control.

## Alternatives considered

- **Shipping the cap disabled by default.** Rejected: it would leave the base
  insecure out of the box and would be adopted by nobody who had not already
  identified the gap. The upgrade cost is one sign-out for sessions that are
  already older than any reasonable bound.
- **A new `familyIssuedAt` field carried forward on every rotation.** Rejected in
  favour of D2: it duplicates information the session document already holds
  durably, requires a backfill decision for existing rows, and would itself be at
  risk from the retention sweep introduced in D7.
- **Creating the refresh-family state row at login to serve as the anchor.**
  Rejected: the family collection is written only on revocation and carries a TTL
  fence of its own, so making it load-bearing for the cap would add a hot-path
  read and couple two lifetimes that are deliberately independent.
- **A TTL index on refresh-token rows instead of an application sweep.**
  Rejected: deletion at expiry is semantically safe, but Mongo's TTL monitor
  cannot provide the bounded per-cycle progress and backlog telemetry required
  for the first cleanup of an upgraded installation.
- **Read-time clamping as the only validation.** Rejected: it leaves the stored
  value shown in the admin UI different from the effective value. D6 adds an
  auth-specific 422 at the edit boundary while retaining defensive read-time
  handling for legacy or out-of-band data.
- **Separating the idle timeout from the refresh-token TTL.** Deferred: they are
  the same control today, and splitting them means carrying a second expiry on
  the refresh row without a demonstrated need.
