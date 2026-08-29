---
title: ADR-0020 — RequireAuth is bearer-only; refresh-token rotation only through explicit refresh endpoints
status: accepted
public: true
---

# ADR-0020 — RequireAuth is bearer-only; refresh-token rotation only through explicit refresh endpoints

| Field | Value |
|---|---|
| **Status** | ✅ Accepted — adopted 2026-08-29 |
| **Date** | 2026-08-29 |
| **Authors** | @salvatore.balestrino |
| **Supersedes** | — |
| **Related** | [ADR-0003](0003-three-audience-host-split.md) (per-audience refresh cookie); [ADR-0017](0017-session-lifetime-and-token-retention.md) (session cap enforced on both refresh paths — this ADR removes the *third*, undocumented one) |

## Context

Refresh-token rotation in Orkestra is meant to happen only where a client
**explicitly asks for it**: `POST /v1/auth/{tier}/refresh-cookie` (the HttpOnly
cookie path both SPAs use — `frontend-admin` serialises it across tabs with a
Web Lock and an in-flight promise, and tolerates one `409
refresh_rotation_raced` retry) and `POST /v1/auth/{tier}/refresh` (token
from the cookie, falling back to a JSON-body `refreshToken`; answered in
`X-New-Access-Token`). The read-only mint,
`GET /v1/auth/session`, deliberately does not rotate.

A third path did not fit that model: `shared/middleware.AuthMiddleware.RequireAuth`
rotated the refresh cookie **implicitly**, on **any** protected request whose
bearer was missing, expired or invalid (`RefreshTokensWithRiskAssessment`),
wrote the successor with `Set-Cookie`, and returned the new access token in
`X-New-Access-Token` / `X-Token-Refreshed`. Not serialised. Any failure
collapsed to the generic `401 authentication required`.

No client consumed the headers. `frontend-admin` withholds an expired bearer
(`prepareHeaders`), so after every 15-minute access-token window **every**
request fell into path 2 and rotated the cookie again; a parallel burst then
had exactly one winner, the losers' `/refresh-cookie` retries could meet a
superseded cookie, and the operator was signed out six hours into a session
whose idle and absolute limits were days away
([#317](https://github.com/orkestra-cc/orkestra/issues/317)). The middleware
path had no test coverage.

## Decision

### D1 — `RequireAuth` authenticates a bearer and nothing else

A missing, expired or invalid bearer is a plain `401`. The refresh cookie is
never read in the middleware. **`RequireAuth` never rotates; rotation happens
only through the explicit refresh endpoints** (`/refresh-cookie`, `/refresh`),
and the read-only mint lives only in `GET /v1/auth/session`. The `authService`
dependency, `NewAuthMiddlewareWithConfig`, `SetAuthService` and the cookie
helpers are removed so the implicit path cannot silently return.

### D2 — The middleware stops emitting `X-New-Access-Token` / `X-Token-Refreshed`

`X-Token-Refreshed` had no other emitter and leaves the contract (dropped from
CORS `ExposedHeaders`). `X-New-Access-Token` **stays** exposed: it is still the
response channel of `POST /v1/auth/{tier}/refresh`; only its *implicit*
emission on ordinary protected responses ends. A client recovers from an
expired access token with `401 → POST /v1/auth/{tier}/refresh-cookie → retry`
(or by calling `/refresh` explicitly). Both in-tree SPAs already do the former;
`frontend-admin`'s path is serialised. Removing `/refresh` itself is a
separate decision, not taken here.

### D3 — `frontend-admin` rotates before expiry, not after

`baseQueryWithRetry` calls the serialised `performRefresh` **before** any
non-auth request whose access token expires within `PROACTIVE_REFRESH_SKEW_MS`
(30 s). The burst-at-expiry case then awaits one in-flight rotation and goes
out with the fresh bearer; the 401 branch remains the only owner of the
sign-out decision. **Invariant: `PROACTIVE_REFRESH_SKEW_MS` < the backend's
`MinAccessTokenTTL` (60 s, `services/auth_duration_bounds.go`).** A skew at or
above the floor means a freshly minted floor-length token is *already* inside
the window, and every request rotates again — a refresh loop. Pinned by a
test that refreshes with `expiresIn: 60` and asserts the next request does
not refresh.

### D4 — Nothing else moves

`/session` stays mint-only, `/refresh-cookie` and `/refresh` keep their
behaviour, the 10-second `RefreshRotationGrace` and the `409` contract are
unchanged, and ADR-0017's cap is still enforced on every surviving path.

## Consequences

- **Breaking for custom clients** that relied on the transparent refresh on
  ordinary protected responses: they must implement
  `401 → refresh-cookie → retry` (or call `/refresh`). In-tree clients need
  no change.
- One extra round-trip per access-token expiry for a client without proactive
  refresh (`401`, refresh, retry). `frontend-admin` pays it only when the
  proactive rotation itself fails.
- A protected route can no longer be authenticated by the `SameSite=Lax`
  refresh cookie alone — every state-changing request now requires the
  bearer, which shrinks the CSRF-shaped surface.
- Residual: a `/refresh-cookie` **response** lost client-side leaves the jar
  holding the superseded cookie. The grace window absorbs the immediate
  retry; after it, presenting that cookie is a replay by design. Unchanged
  from before.

## Alternatives considered

- **Mint-only middleware** (`MintAccessTokenFromRefresh` in `RequireAuth`).
  Closes the race but keeps a second path with a hidden per-request cost
  (refresh-row read + user read + JWT mint on every no-bearer request), keeps
  cookie-only auth on mutating routes, and keeps a header nobody reads.
- **Consume `X-New-Access-Token` in the SPA.** Fixes one client; the race
  stays for every other.
- **Serialise the middleware server-side** (per-family lock). Complexity for a
  path that should not exist.
