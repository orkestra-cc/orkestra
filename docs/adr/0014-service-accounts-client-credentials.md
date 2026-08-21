---
title: ADR-0014 — Service accounts via OAuth2 client-credentials grant
status: accepted
public: true
---

# ADR-0014 — Service accounts via OAuth2 client-credentials grant

| Field | Value |
|---|---|
| **Status** | ✅ Accepted — adopted 2026-08-18 |
| **Date** | 2026-08-18 |
| **Authors** | @salvatore.balestrino |
| **Related** | [ADR-0003](0003-three-audience-host-split.md) (three-audience host split — reserved the `service` audience this ADR finally mints); [ADR-0006](0006-collapse-to-core-only-base.md) (core-only base — this feature ships inside the core `auth` module, not an addon) |

## Context

The core `auth` module has no machine-to-machine credential. Every token-minting
path is either interactive (email/password, OAuth 2.1) or the development-only
`/dev/token` endpoint, which is disabled outside local development. Integrations,
CI jobs, and other automated callers that need to reach the operator API surface
have had no production-shaped identity to authenticate as.

Relevant groundwork already in place before this decision:

- Access tokens are RS256 JWTs carrying `sub`, `email`, `srole`, and
  `memberships[]` (embedded at issue time from the caller's live tenant
  memberships). Permissions are **not** embedded — middleware resolves them
  fresh per request via the authz evaluator, so revocation is instant
  regardless of a token's remaining lifetime.
- `AudienceService = "service"` was reserved on the JWT validator
  (ADR-0003 PR-D) and accepted by token validation, but nothing minted it.
- `RequireAudience` performed an exact single-audience match per host mux, so
  an `aud: service` token would have been rejected on every existing mux.
- Authz already supports org-scoped custom roles with explicit permission
  sets, bound per tenant — an automated caller can be granted exactly the
  permissions it needs without any new authorization primitive.

A machine identity needed to be revocable, individually least-privilege, and
built without inventing a parallel authorization model.

## Decision

**D1 — A service account is a Tier-1 operator user row, not a new entity.**
`iface.User` gains an additive `Kind` field (`""` = human, `"service"` =
machine principal). A service account is created with a synthetic,
undeliverable email (`sa-<slug>@service.invalid`, using the RFC 2606 reserved
test TLD), `Role: "guest"` (the minimum system role — real capability comes
from tenant-scoped custom-role bindings on top), and no password hash. Because
it **is** a user, tenant membership, custom roles, authz bindings, Cedar
evaluation, audit provenance, and JWT membership embedding all work with zero
changes to the tenant, authz, or any downstream module. `Kind` is immutable
after creation — it is not part of the user-update input shape, so no update
path can retarget a human account into a service account or vice versa.

**D2 — Credentials are a dedicated collection, argon2id-hashed, capped at two
active per account.** A new `service_account_credentials` collection stores
`{uuid, userUuid, clientId, secretHash, label, createdAt, lastUsedAt,
revokedAt}`. `clientId` is an opaque `sa_`-prefixed identifier distinct from
the user UUID; the plaintext client secret (`sas_`-prefixed, 32 random bytes,
base64url-encoded) is generated server-side, hashed with the existing
argon2id password service, and returned to the caller **exactly once** — at
creation or rotation time — never persisted or logged in plaintext. At most
two active credentials may exist per account at a time, enabling
zero-downtime rotation (issue new → migrate the caller → revoke old) while
capping the blast radius of a leaked or forgotten credential. Revocation is
not idempotent — revoking an already-revoked credential surfaces the same
not-found outcome as revoking an unknown one, so there is no distinguishing
oracle between "already revoked" and "never existed."

**D3 — A dedicated OAuth2 client-credentials grant endpoint, no refresh
token.** `POST /v1/auth/token` is a public, rate-limited route accepting
`{grantType: "client_credentials", clientId, clientSecret}` and returning
`{accessToken, tokenType: "Bearer", expiresIn}` — JSON with camelCase field
names rather than RFC 6749's form-encoded `grant_type`/`access_token` shape,
matching the platform's JSON-first API convention throughout, so an
off-the-shelf OAuth2 client library needs a thin adapter (or a hand-rolled
request) rather than working against this endpoint out of the box. No
refresh token is issued —
a service account re-authenticates with its client credentials when the
access token expires, mirroring how the credential itself is already a
long-lived secret. Every rejection reason (unknown `clientId`, revoked
credential, wrong secret, disabled account, non-service-account user)
collapses to a single indistinguishable error so a caller can never learn
which check failed. An unknown or revoked `clientId` still burns a dummy
hash-verify call so that path costs the same wall-clock time as a
wrong-secret rejection, closing a timing side channel. Failed attempts are
throttled per source IP and per targeted `clientId` through the existing
rate limiter's lockout bucket, checked via a non-consuming peek so a burst of
legitimate grants cannot trip its own lockout merely by asking.

**D4 — Tokens carry `aud: "service"`; `RequireAudience` becomes
set-membership.** The grant is minted by a dedicated JWT-service instance
configured with the reserved `AudienceService` constant — the same RS256 key
pair and TTL policy as every other token, only the audience claim differs.
`RequireAudience` changes from an exact-match check to a variadic
set-membership check (`RequireAudience(expected ...string)`), and the
operator host mux is configured to accept `{operator, service}` while the
client host mux is unchanged (`{client}`). A service token therefore reaches
the operator API surface and is rejected outright on the client surface, and
an operator- or client-audience token is rejected at the grant endpoint's
downstream consumers by the same mechanism in reverse.

*Rejected alternative: minting `aud: "operator"` for service tokens.* This
would have required no mux change, but it leaves the already-reserved
`service` audience permanently dead code and makes service-issued tokens
indistinguishable from human operator tokens at every point downstream that
inspects the audience claim (logging, future audience-scoped policy, replay
analysis). The one-line mux change was judged cheaper than that permanent
ambiguity.

**D5 — A gated admin REST surface owns the account and credential
lifecycle.** Six endpoints under `/v1/admin/service-accounts` cover create,
list, get, update (rename/enable/disable), issue/rotate credential, and
revoke credential. Read endpoints require `auth.service_accounts.read`;
every mutating endpoint requires `auth.service_accounts.manage` plus a fresh
(under 5 minutes) step-up MFA proof, matching the step-up bar already applied to
every other secret-revealing or destructive admin mutation in this module.
Both permission keys are system-flagged. This ADR covers the admin REST
surface; a corresponding admin console page is a separate, later delivery
phase and is not implied to exist by this decision.

## Consequences

- **Fail-closed on every interactive and refresh path.** Password login,
  every OAuth flow, and all read paths in the refresh-token machinery reject
  a principal with `Kind == "service"`. The client-credentials grant is the
  only path that can mint a token for a service account. Privileged system
  roles (`super_admin`, `administrator`) can never be assigned to a service
  account, and that guard fails closed even when the pre-read needed to
  classify the target account is unavailable — an assignment is refused
  rather than risked.
- **Disabling an account has bounded, documented latency.** Disabling a
  service account stops new grants instantly, but a token already minted
  before the disable remains technically valid for up to its access-token
  TTL (the platform default is 15 minutes). This is accepted because
  permissions are never embedded in the token — they are resolved fresh per
  request by the authz evaluator — so unbinding a service account's roles or
  disabling it takes effect on its *authorization* immediately, regardless
  of how long the bearer token itself remains cryptographically valid.
- **Rotation is a documented best-effort cap, not an atomic guarantee.** The
  max-two-active-credentials check is a count-then-insert operation, not an
  atomic conditional insert. Two concurrent issue calls against the same
  account can both observe fewer than two active credentials and both
  succeed, briefly exceeding the cap. This is accepted as operational
  hygiene rather than a security boundary: credentials remain individually
  revocable, and a momentary over-cap grants no access a correctly-capped
  account would not already have.
- **The feature composes with, and does not duplicate, any existing
  subsystem.** Because a service account is a user row, no new tenancy,
  RBAC, Cedar, or audit model was introduced. This also means the design has
  no dependency on anything outside the core `auth`, `user`, and `authz`
  modules, so it carries no obstacle to being contributed back to the public
  base.

## Alternatives considered

- **Minting `aud: "operator"` for service tokens** — rejected; see D4.
- **Static long-lived API keys / personal access tokens (PATs)** — rejected
  for this phase: client-credentials is the single machine-authentication
  mechanism. A static key has no equivalent to a short-lived access token's
  natural expiry and would need its own revocation-latency story built from
  scratch.
- **Refresh tokens for service principals** — rejected: the client
  credential is already a long-lived secret: issuing a refresh token on top
  of it would add a second long-lived artifact to protect for no
  corresponding benefit. A service account simply repeats the grant when its
  access token expires.
- **Tier-2 (client-surface) machine credentials** — out of scope for this
  decision. Service accounts operate on the Tier-1 operator surface only;
  the client host mux's audience set is unchanged. A Tier-2 equivalent, if a
  need materializes, is a separate design.
