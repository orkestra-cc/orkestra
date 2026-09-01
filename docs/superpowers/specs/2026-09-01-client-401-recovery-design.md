# Client-tier 401 recovery — design

_Status: **DRAFT — round 1 answered, awaiting review round 2**_
_Issue: [#325](https://github.com/orkestra-cc/orkestra/issues/325)_
_Related: [ADR-0020](../../adr/0020-bearer-only-require-auth.md), [ADR-0017](../../adr/0017-session-lifetime-and-token-retention.md), [ADR-0003](../../adr/0003-three-audience-host-split.md)_

## 0. Revision log

| Rev | Date | Change |
| --- | ---- | ------ |
| v16 | 2026-09-01 | **Review round 14.** §4.5 asserted that every path installing a token supplies the duration; that was the *requirement* written as if it were the state. Today **none** of them do — `signIn` takes only a token, and both call sites drop the `expiresIn` their result already carries. §4.6 now carries the full migration inventory (`setAccessToken`, `AuthState.signIn`, `AuthProvider`, `LoginPage.complete` and its two callers, `OAuthCallbackPage`), and flags `auth.ts`'s `?? 900` fallback, which fabricates a lifetime rather than admitting an unknown one. |
| v15 | 2026-09-01 | **Review round 13 — blocking.** §4.1c's timer was cleared right after `await fetch`, which resolves on **headers**, leaving the body read unbounded — so round 6's "the lock is bounded transitively" argument was false, and a stalled body could hold the cross-tab lock forever. Reproduced locally: fetch resolved at 31 ms, timer cleared, body finished at 3029 ms against a 1000 ms timeout with no abort; moving `clearTimeout` past the body read aborts at 1006 ms. The timer now spans fetch + classification + body. Also documents the residual the timeout creates: an abort after the server rotated but before the `Set-Cookie` lands strands the successor, and the 409 retry cannot recover it. |
| v14 | 2026-09-01 | **Review round 12 — blocking, and N4 falls.** The refresh outcome was a denylist — everything that was not 2xx/409/503 meant "signed out" — so a 429 from the *global* rate limiter, a 5xx, a malformed 2xx, or a Mongo outage would log the user out. Worse, four infrastructure failures on the refresh path are wrapped generically and answered as a plain 401 (`auth_service.go`), which the client cannot tell from a genuinely dead refresh token. **G2/G3 are unreachable while N4 stands**, so N4 is replaced by a scoped backend change: infrastructure failures on the refresh path map to 503. The client rule inverts to an allowlist — sign out **only** on 401 — and defect C, §4.9, the new tests and O6 follow. |
| v13 | 2026-09-01 | **Review round 11 — blocking.** The replay guard had a 30-second hole. Branch 2 called a token "live" only above `now + SKEW`, but the server accepts it until the instant it expires: a `change-password` sent with 20 s left reaches the handler, the wrong password is counted, and the client — reading 20 s < SKEW — refreshed and **replayed it**. The margin *was* the defect, so it is gone: recovery is now permitted only when the token was **already expired when it was sent**, which is the one condition under which the handler provably never ran. `SKEW` leaves the 401 path entirely, an unreadable expiry now counts as **live** rather than expired (§4.5's earlier direction was unsafe under this rule), and §4.4's reachability argument — which was simply wrong — is restated. |
| v12 | 2026-09-01 | **Review round 10 (minor).** Manual procedure for two tabs crossing the TTL together written out in full (§6). The existing two-tab item only *reloaded* both tabs, which exercises `AuthProvider`'s mount refresh, not the 401-driven path — a different code path with a different expected shape. Writing it also corrected an expectation: two tabs produce **two** serialised rotations, not one, because branch 3 is per-tab and each tab needs its own access token. A lock-disabled variant is included, since the 409 retry is otherwise unreachable by hand. |
| v11 | 2026-09-01 | **Review round 9 (minor).** `jwtExp` test cases added for a non-numeric/non-finite `exp` and for unpadded base64url — both probed first, and both came back different from the assumption. `Infinity` **is** reachable through valid JSON (`1e400`), so `typeof === "number"` is not enough and §4.5 now requires `Number.isFinite`. Missing padding, by contrast, is *tolerated* by `atob` in this runtime; what actually throws is the base64url alphabet, so the test is specified with a fixture that provably contains `-`/`_` and asserts its own fixture. |
| v10 | 2026-09-01 | **Review round 8 (minor).** §3.C's description corrected: the generic `RequireAuth` 401 carries **no top-level `code`** — `sendErrorResponse` puts `appErr.Code` in `errors[0].value` (`auth.go:1258-1279`). Enumerating the emitters to write that correction falsified a second claim, mine: the middleware emits **at least seven** distinct top-level codes, several of them on 401s (`step_up_required`, `mfa_enrollment_required`, `password_confirm_required`, `audience_mismatch`), so a presence test on `code` would sign users out on four non-terminal conditions. `TERMINAL_CODES` is restated as a **membership** test with that as its justification, and §5.11 and the tests follow. |
| v9 | 2026-09-01 | **Review round 7 (minor).** Header merging specified concretely: build with `new Headers(init?.headers)` rather than object spread, which silently drops a `Headers` instance or an array of tuples — both legal `HeadersInit` shapes the helper's own signature accepts. Defaults set only when absent, `Authorization` set last. Latent today (every call site passes an object literal), which is why it needs pinning before the four wrappers become one. |
| v8 | 2026-09-01 | **Review round 6.** The timeout rule was already specified (§1's third gap, G2, §4.1c, the outcome table, the §6 case) — but checking it surfaced two real holes. (i) The prescribed mechanism, `AbortSignal.timeout`, **does not respect vitest's fake timers** (probed: `aborted` stays `false` after advancing 20 s), so §6's test could not have worked as written; §4.1c now specifies `AbortController` + `setTimeout`, which does. (ii) §4.1a's unbounded Web Lock and §4.1c's bounded fetch were stated separately and never connected — the lock is bounded *transitively* by the holder's timeout, which is the argument that makes an unbounded lock safe. |
| v7 | 2026-09-01 | **Review round 5.** Finding accepted. N2 ('keep `client.ts`'s middleware for whenever codegen lands') was wrong: that middleware is a second, *unsafe* 401 algorithm — it retries every 401 including `change-password`, re-sends an already-consumed `Request` body, gates on the marker, and clears the token without the marker. Keeping it contradicts G5 and arms a trap for whoever first imports `api`. **N2 is replaced: the dead client and both middlewares are deleted** (§4.8). Verified safe across the whole fork chain — zero importers of `api` anywhere. |
| v6 | 2026-09-01 | **Review round 4.** Finding accepted and verified. Branch 1 inspects the 401 body, which is a single-use stream: reading it directly would leave the caller's `readError` unable to see `detail`/`code`, and — because `readError` swallows the resulting `TypeError` — it would degrade **silently**. §4.3 now inspects a `res.clone()` throughout, and the retry's own 401 is inspected the same way for terminal codes only. §5.11, the body-readability test and the `WWW-Authenticate` note added. |
| v5 | 2026-09-01 | **Open questions ruled (O1, O2, O3, O5).** O1 replaces the wall-clock `exp` comparison with a duration derived from `expiresIn` at receipt, which is immune to clock skew rather than tolerant of it (§4.5 rewritten, §4.1 records it, §4.3 branch 2 restated, §5.9 corrected — v4 claimed the skew case was stuck until reload, which was wrong in the other direction too). O2: `dsr.ts` unified now, verified free. O3: the helper is its own file. O5: the marker repair stays. |
| v4 | 2026-09-01 | **Review round 3.** Finding accepted and verified. v3's branch 4 asserted that a `signed-out` outcome had already cleared the token; `refreshAccessToken` returns `signed-out` from its marker gate **without** clearing anything (`tokenStore.ts:117`), reachable via a throwing `localStorage` *and* via a sibling tab's sign-out. §4.1(d) adds an unconditional `refreshAfterUnauthorized`, §4.3 splits branch 4 by whether a bearer was sent, G3 is stated as a guarantee, §5.8 corrected (it claimed the opposite of what the code does), tests added, O5 raised. |
| v3 | 2026-09-01 | **Review round 2.** Finding accepted and verified. A second 401 answered *after* another request already rotated was handled wrongly in both possible readings of v2's branch order: judging by the token in the store returned the stale 401 to the caller, judging by the token that was sent rotated a second time. §4.3 now captures `sentToken` and reorders the branches — the replay guard now sits ahead of every recovery branch, and an already-rotated token is preferred over a new refresh. G8, §4.4's reachability argument, §5.1 and the delayed-401 test added; O4 answered. |
| v2 | 2026-09-01 | **Review round 1.** Finding accepted in full and verified against the tree. v1 claimed the existing coalescing made the refresh safe (old §4.2); it is per-tab only, and `performRefresh` collapses the backend's `409 refresh_rotation_raced` into `signed-out` — the exact outcome the 409 was created to prevent. Defect B added to §1; §4.1 (cross-tab lock, 409 retry, fetch timeout) is new and is now the foundation the helper sits on; G6/G7 added; §5.2–5.4 and §6 race tests added. |
| v1 | 2026-09-01 | First draft. |

## 1. Problem

Two defects. **A** is the reported regression; **B** is older, independent, and
already able to sign a user out today — and A's fix multiplies its trigger rate,
so shipping A alone would make B worse.

### Defect A — no 401 recovery (regression, v0.10.0)

`frontend-client` has **no recovery from a 401**. A Tier-2 session stops working
one access-token TTL after login and does not repair itself until reload.

1. **Something that looks like the recovery exists, and it is both dead and
   wrong.** `src/api/client.ts` defines `refreshMiddleware` — refresh, retry with
   `X-Retry: 1`, honour ADR-0017's `unavailable` — attached to the
   `openapi-fetch` client exported as `api`. It reads like the live request path,
   and the docs that shipped before `82f25252` said it was one.
   **Nothing imports `api`.** All six consumers import only `apiBaseURL`:

   ```
   $ grep -rh "import .* from \"@/api/client\"" src | sort -u
   import { apiBaseURL } from "@/api/client";
   ```

   Every request goes through a hand-typed wrapper calling `fetch` directly.
   None has a 401 branch. And the middleware would not have been the fix even if
   something did import it: it retries every 401 indiscriminately and re-sends a
   consumed request body, among three further defects catalogued in §4.8 — so
   this is not "wire up the code that is already there", and the code that is
   already there has to go.

2. **v0.10.0 removed the server-side net that hid this.** Before `18721162`,
   `RequireAuth`'s cookie branch called `RefreshTokensWithRiskAssessment` and then
   `m.setUserContext(w, r, claims, next)` — it *let the request through
   authenticated*. The SPA sends `credentials: 'include'` on every call to the API
   host, so an expired bearer plus a valid refresh cookie was transparently
   rescued. The SPA never needed its own recovery and never noticed it lacked one.

3. **ADR-0020 shipped on the opposite belief.** D2 says "Both in-tree SPAs already
   do the former"; Consequences says "In-tree clients need no change." Neither is
   true for `frontend-client`.

**Observable today:** after the TTL every authenticated call 401s. `AuthProvider`
still holds the stale token, so `isAuthenticated` stays `true` — the user is *not*
redirected to `/login`, the pages just fail. A reload fixes it, which makes it
read as intermittent. Reachable path: open `/account/billing`, leave the form past
the TTL, submit.

### Defect B — a lost rotation race signs the user out (pre-existing)

`performRefresh` classifies **any** non-2xx that is not a 503 as `signed-out`,
clearing both the token and the session marker:

```ts
// src/auth/tokenStore.ts:90-91
if (res.status === 503) return { status: "unavailable" };
if (!res.ok) return signedOut();
```

The backend has a **409** for exactly this situation, and its comment says what
the client must not do:

```go
// backend/.../auth_handler.go — writeRefreshErr
if errors.Is(err, services.ErrRefreshRotationRaced) {
    // A sibling tab won the rotation; the family is untouched and the
    // browser already holds the successor cookie. 409 keeps this off
    // the 401 path, where every client treats the answer as a sign-out.
```

`frontend-client` is that client. The 409 lands in `!res.ok` → `signedOut()` →
marker cleared. Clearing the **marker** makes it sticky: `refreshAccessToken`
short-circuits on the next cold load, so the session stays lost even though the
cookie in the jar is perfectly valid.

Two further gaps behind the same call:

- **The in-flight guard is per-tab.** `inflightRefresh` is a module-level variable
  in one JS context. Every tab shares a login, so their tokens expire at the same
  instant and each posts the same cookie. `frontend-admin` hit precisely this and
  solved it with a Web Lock (`REFRESH_LOCK_NAME`); the client SPA never got that
  treatment.
- **The refresh fetch has no timeout.** Today that only delays a background
  rehydrate. Under defect A's fix the refresh moves *into the request path*, where
  a connection that accepts and never answers stalls the request itself,
  indefinitely.
- **One `signed-out` does not mean what the other does.** `refreshAccessToken`
  answers `signed-out` straight from its marker gate — `if (!hasSessionMarker())
  return { status: "signed-out" }` (`tokenStore.ts:117`) — **without** calling
  `signedOut()`, so neither the token nor the marker is cleared. Every other
  `signed-out` in the store goes through `signedOut()` and clears both. Latent
  today (`AuthProvider` discards the outcome with `void`), but load-bearing the
  moment a 401 handler acts on it — see §4.3.

**Reproducible now, without any of this spec's changes:** open two client-SPA tabs
on a signed-in session and reload both together. Both `AuthProvider`s fire the
mount refresh, one wins the rotation CAS, the loser gets 409 → signed out.

### Defect C — an outage is indistinguishable from a dead session (pre-existing, both tiers)

ADR-0017 gave `session_enforcement_unavailable` its own 503 precisely so a storage
outage would not "train clients to discard a session that is still perfectly
valid". The refresh path has three more doors into the same failure, and they were
all left open.

**On the wire.** `/refresh-cookie` is mounted under the router's **global** rate
limiter — `router.Use(errorManager.GetRateLimiter().Middleware("api:general"))`
(`cmd/server/middleware.go:131`) — so it can answer **429** with a `Retry-After`
(`errors/rate_limiter.go:353`). A reverse proxy can answer 502/504, the endpoint
can 500, and a 2xx can arrive without a token. None of these says the session is
over; the client's denylist called every one of them a sign-out.

**In the service.** `RefreshTokensWithRiskAssessment` wraps **four** distinct
infrastructure failures in generic errors that `writeRefreshErr` then answers as a
plain 401 `Invalid refresh token`, with no `code`:

| Site | Wrapped as | What it really is |
| ---- | ---------- | ----------------- |
| `refreshTokenRepo.GetByTokenAny` | `failed to validate refresh token` | the store is unreachable |
| the user lookup | **`user not found`** | same — and this one *reads* as terminal |
| `mintTokenPair` | `failed to mint token pair` | signing/key failure |
| `refreshTokenRepo.RotateWithFamily` | `failed to rotate refresh token` | write failure |

A Mongo blip during a refresh therefore reaches the SPA as the same codeless 401 a
genuinely invalid refresh token produces. **No client-side rule can separate
them**, which is why this defect is what removes N4 rather than something the
helper can be made clever about.

## 2. Goals and non-goals

### Goals

- **G1** — An expired access token on an authenticated call recovers silently:
  refresh once, retry once; the caller never sees the 401.
- **G2** — **Only an explicit rejection ends a session.** A refresh that could not
  be *completed* — 429 from the rate limiter, 408, any 5xx, a transport failure, a
  timeout, a 2xx without a token, a twice-raced 409 — surfaces the original 401
  and **keeps token and marker**, per ADR-0017's reasoning. This is an allowlist:
  the sign-out case is enumerated (§4.1), and everything not on that list is
  transient by default. A denylist is what defect C was.
- **G3** — A genuinely dead session clears token **and** marker so `AuthProvider`
  re-renders and `RequireAuth` redirects, instead of leaving a broken page. This
  is a guarantee about the *observable state*, not about which function was
  called: no path may report `signed-out` to the 401 handler while leaving a
  token in memory.
- **G4** — A 401 that does **not** mean "the token is the problem" is passed
  through untouched: no refresh, no replay. See §4.4.
- **G5** — One helper owns the authenticated path — and it is the **only** 401
  algorithm in the tree. Today four near-copies of "attach bearer +
  `credentials: 'include'`" exist (`auth.ts::authedFetch`, `avatar.ts::authedJson`,
  `billingProfile.ts::authedJson`, `dsr.ts::postJson`) *plus* a fifth, unreachable
  and unsafe one in `client.ts` (§4.8). After this there is one, a new endpoint
  cannot forget the recovery, and there is no second implementation to drift from
  it or to be picked up by mistake.
- **G6** — **Concurrent tabs never sign each other out.** At most one rotation is
  in flight per origin, and a lost race is never a sign-out.
- **G7** — **`409 refresh_rotation_raced` is honoured as designed**: retried once,
  and if it races again reported as `unavailable` — never `signed-out`, and the
  marker survives either way.
- **G8** — **A 401 answered after a sibling request already rotated does not
  rotate again, and does not fail.** Whatever the delay between the request going
  out and its 401 coming back, a burst that shares one expired token produces
  **exactly one** rotation and every caller ends up served.

### Non-goals

- **N1 — Proactive rotation.** ADR-0020 D3 gives `frontend-admin` a
  refresh-before-expiry path. Not added here; §8 names it.
- **N2 — Waking up `openapi-fetch`.** `openapi.gen.ts` is still a stub, so routing
  the wrappers through `api` is unavailable, and this spec does not attempt it.
  The **dormant client and its middlewares are deleted** rather than left in place
  (§4.8) — keeping a second, unsafe 401 algorithm in the tree would contradict G5
  and arm a trap for whoever first imports `api`. Follow-up #2 re-adds the client
  wired to this spec's policy, which is three lines against a real generated type.
- **N3 — Fixing `frontend-admin`.** §7 records a real defect found there; own PR.
- **N4 (withdrawn in round 12) — a scoped backend change is now in scope.**
  Earlier revisions forbade touching the backend. Defect C shows that G2 and G3
  are then **unreachable**: four infrastructure failures on the refresh path are
  answered as a codeless 401, and no client-side rule can tell them from a dead
  refresh token. So N4 becomes a boundary rather than a prohibition — **the only
  backend work in scope is error *classification* on the refresh path** (§4.9):
  infrastructure failures answer 503 instead of 401. No route moves, no field is
  removed, nothing that already works changes shape. Anything wider — including
  §3.D's `access_token_expired` on `RequireAuth`, which touches every protected
  route — stays out, and **O6** asks whether to pull that one in while the same
  code is open.

## 3. Alternatives considered

### A — Fix `performRefresh`, then one shared authed-fetch helper with a token-state gate (chosen)

Defect B is fixed at the single choke point every refresh already goes through, so
the helper inherits correct outcomes rather than re-deriving them. The helper then
decides from **the token we hold** whether a refresh could help — closing §4.4's
replay hazard by construction rather than by remembering to list an endpoint.

### B — Path exclusion list, the `frontend-admin` shape

`baseApi.ts` carries `AUTH_ENDPOINT_PATHS` and skips the retry for those. It is the
in-house precedent — and it is the mechanism that **already has a hole**:
`change-password` is not on the list (§7). A hand-maintained allowlist fails open,
and it did. Rejected as the primary gate; the recursion-prevention part is kept in
§4.3 as defence-in-depth.

### C — Discriminate on the 401 body from the server

Rejected: there is no field that separates "your token expired" from "your
credentials were wrong". Three shapes reach the client, and none of them offers
one:

| Emitter | Body |
| ------- | ---- |
| `RequireAuth`'s generic path — `sendErrorResponse` (`auth.go:1258`) | `status`, `title`, `detail`, `type`, `errors[]`. **No top-level `code`**; `appErr.Code` is carried as `errors[0].value` — and for `errors.AuthenticationError` that is `CodeInvalidCredentials`, *the same value a wrong password produces*. |
| `jwt_validator.go::writeErr` | `status`, `title`, `detail`. Nothing else at all. |
| Session termination — `sendSessionRevoked` | `status`, `title`, `detail`, `type`, `errors[]`, **and a top-level `code`** of `session_revoked` or `session_max_age_reached`. |

**A top-level `code` is therefore not evidence of anything in particular.** The
middleware emits at least seven, and four of them ride on 401s that are emphatically
*not* a dead session:

```
$ grep -rn '"code":' backend/internal/shared/middleware/*.go | grep -o '"[a-z_]*"$' | sort -u
audience_mismatch   capability_required   mfa_enrollment_required
password_confirm_required   session_max_age_reached   session_revoked   step_up_required
```

This is why §4.3's `TERMINAL_CODES` is a **membership** test and not a presence
test. `if (body.code) → clear` reads as the obvious simplification and would sign
a user out for being asked to confirm a password, to enrol MFA, or to complete a
step-up — turning four recoverable prompts into a logout. The set is closed on
purpose; adding to it is a decision, not a maintenance chore.

Reading `body.code` is then both sufficient and safe: the generic paths answer "no
top-level code", which is the "not terminal" answer we want, and the one field that
*could* be mistaken for a discriminator — `errors[0].value`, which for
`AuthenticationError` is `CodeInvalidCredentials`, indistinguishable from a wrong
password — is deliberately not read.

Everything else — the actual expired-vs-wrong-credentials question — is answered
client-side from the token we sent (§4.3 branch 2), because the server does not
answer it.

### D — Have the backend say `access_token_expired` (the right answer, deferred)

The client is inferring something the server knows for certain. If `RequireAuth`'s
expired-bearer 401 carried a distinct top-level `code`, §4.3 branch 2 would stop
being an inference: refresh **iff** that code is present, and the replay hazard
disappears by construction rather than by an argument about when the handler runs.
It would also fix `frontend-admin`'s copy of the same hazard (§7) in one change,
and it composes with the closed set already read in branch 1.

Not taken here — but no longer because backend work is forbidden, since round 12
withdrew N4 for the refresh path (§4.9). The reason now is **blast radius**: this
touches `RequireAuth`, which answers every protected route on both tiers, days
after ADR-0020 reworked exactly that code, whereas §4.9 changes the classification
of four error returns on a single endpoint. §4.3's rule is safe without it — it
never replays; it only gives up auto-recovery for a token that expired *in flight*
(§4.4). **O6** asks whether to pull it in anyway. Recorded as follow-up #1 with a
sketch — it is the eventual
right answer, not a rejected idea.

## 4. Design

### 4.1 `tokenStore.performRefresh` — cross-tab, 409-aware, bounded

Three changes inside the existing `performRefresh`. Its public contract is
unchanged: still coalesced, still never rejects, still returns the same three
`RefreshOutcome` shapes.

**(a) Serialise across tabs.** Wrap the attempt in a Web Lock, mirroring
`frontend-admin`:

```ts
const REFRESH_LOCK_NAME = "orkestra:auth-refresh";

async function withRefreshLock<T>(run: () => Promise<T>): Promise<T> {
  const locks = typeof navigator !== "undefined" ? navigator.locks : undefined;
  if (!locks?.request) return run();      // see §5.5 — must be `?.`, not `typeof`
  return await locks.request(REFRESH_LOCK_NAME, run);
}
```

Web Locks is the only cross-tab primitive that releases automatically when the
holder navigates away or crashes. Where it is missing, run unguarded: the
backend's 10 s `RefreshRotationGrace` (`services/auth_service.go:1699`) plus (b)
still keep a lost race from ending the session.

The lock is **per-origin**, so the client SPA (`client.localhost`) and the operator
console (`console.localhost`) never contend even though they use the same lock
name — and by ADR-0003 D-9 they hold different refresh cookies anyway.

**The lock itself is not bounded** with an `AbortSignal`: that needs the
3-argument `request(name, {signal}, cb)` overload, and `frontend-admin`'s comment
records that switching shapes silently defeated its own test. Same decision, same
reason — **and it is safe only because of (c)**. A tab that takes the lock and
then hangs would otherwise block every other tab on the origin indefinitely; since
everything it does while holding the lock happens inside (c)'s bound, the lock is
bounded transitively. The two are one safety argument, not two unrelated choices:
**weakening (c) re-arms the lock.**

Round 13 showed how easily "weakening" happens without anyone intending it: (c)'s
first sketch cleared the timer immediately after `await fetch`, which bounds the
headers and nothing else, so a stalled body held the lock indefinitely and this
paragraph was simply false. The bound is only real while the timer covers the body
read — which is why (c) now spells out where `clearTimeout` may sit.

**(b) Classify 409 correctly, with exactly one retry.** Inside the lock:

```
first = attempt()
if first is raced (409):
    second = attempt()            // the successor cookie is already in the jar
    if second is raced (409): → unavailable   // NOT signed-out
    else: → second
else: → first
```

Rationale, unchanged from `frontend-admin`: in the case the 409 was designed for —
a **sibling** rotated first — the browser already holds the successor cookie, so a
second attempt lands. A race surviving two attempts is far more likely a live
session than a dead one, and guessing "dead" is the failure this removes. **The
marker is untouched on every 409 path.**

**That justification does not cover every 409.** If the rotation succeeded
server-side but its response never reached the browser (§5.12), the successor is
in nobody's jar, and the retry re-presents the same superseded cookie for a second
409. The design still degrades correctly — two 409s are `unavailable`, token and
marker kept — but the retry is a *hope* there, not a guarantee, and the spec should
not claim otherwise.

**(c) Bound the fetch.** `REFRESH_FETCH_TIMEOUT_MS = 10_000`, the value
`frontend-admin` settled on (`baseApi.ts:74`). A `/refresh-cookie` that accepts the
connection and never answers would otherwise hang the **original request**, since
§4.3 puts the refresh on its critical path — the failure this bound exists for.

The existing `catch` already returns `unavailable`, which is the correct
classification and must stay that way: **"no answer" is not "no"**. A bare
`signed-out` here would turn a slow network into a logout — a worse bug than the
hang it replaces. So: **timeout → `unavailable`, token and marker untouched**,
identical to the 503 and transport-failure paths.

**The timer must span the whole operation — fetch, classification *and* the body
read.** `fetch` resolves when the **headers** arrive, not when the body is
complete, so a `clearTimeout` placed straight after the `await` bounds almost
nothing: a server that sends headers and then stalls the body is unbounded, and
because `performRefresh` runs inside the Web Lock, it holds that lock for as long
as it stalls. Measured against a server that writes headers and half a JSON body,
then finishes 3 s later, with a 1 s timeout:

```
clearTimeout immediately after await fetch
  fetch resolved at   31 ms   → timer cleared
  body completed at 3029 ms   → no abort, 3× past the timeout

clearTimeout moved past the body read
  fetch resolved at   30 ms
  ABORT at          1006 ms   → AbortError, and the lock is released here
```

So the `finally` goes around everything, and nothing may be cleared early:

```ts
const ctrl = new AbortController();
const timer = setTimeout(() => ctrl.abort(), REFRESH_FETCH_TIMEOUT_MS);
try {
  res = await fetch(url, { …, signal: ctrl.signal });
  // status classification and the body read happen INSIDE the timer's scope —
  // aborting the signal rejects an in-flight res.json() too.
  …
  const body = await res.json();
  …
} finally {
  clearTimeout(timer);          // only here, on every path
}
```

An abort during the body read rejects, and that rejection classifies as
`unavailable` like any other "no answer" (§4.1's table) — it must **not** fall
through to the malformed-body reasoning, though under round 12's inverted table
both land on `unavailable` anyway, so the two agree rather than fight.

**Build the signal from an `AbortController` + `setTimeout`, not
`AbortSignal.timeout`** — the second reason for the same choice:

This is a **deliberate divergence from `frontend-admin`**, which uses
`AbortSignal.timeout`, and it is not stylistic. Probed in this repo's own harness:

```
vi.useFakeTimers();
const s = AbortSignal.timeout(10_000);
await vi.advanceTimersByTimeAsync(20_000);
s.aborted  // → false
```

`AbortSignal.timeout` runs on an internal timer that vitest's fake clock does not
control, so the §6 timeout case could not be written the way a reader would expect
— it would either hang for ten real seconds or never abort. The same probe with
`AbortController` + `setTimeout` aborts correctly, because that path goes through
the faked `globalThis.setTimeout`. `clearTimeout` also stops each refresh from
leaving a live 10 s timer behind, which `AbortSignal.timeout` cannot avoid.

**(d) Record the reported lifetime.** `performRefresh` already parses the body for
`accessToken`; it also reads `expiresIn` and installs the pair, so §4.5's
duration-based reckoning has its input on every rotation. A body without
`expiresIn` is **not** an error — the token is installed with the §4.5 fallback.

**(e) Export an unconditional path for the authenticated retry.** The marker gate
exists to spare *anonymous visitors* a guaranteed-401 round-trip on every cold
load. A 401 answering a request that carried a bearer is not that case: a bearer
in memory is proof a session existed, so the gate must not decide whether to try
the cookie. `refreshAccessToken` keeps its current shape and its current callers;
a sibling is added beside it:

```ts
// The AUTHENTICATED-RETRY path. Unlike refreshAccessToken it does NOT consult
// the session marker: the caller already sent a bearer, so the anonymous
// optimisation the gate exists for does not apply, and a marker that is absent
// for an unrelated reason (a throwing localStorage, a sibling tab's signOut)
// must not veto a cookie that is still valid. Every outcome therefore comes
// from performRefresh, which is the only place that decides them — so a
// signed-out here always clears BOTH token and marker. Never rejects.
export async function refreshAfterUnauthorized(
  apiBase: string,
): Promise<RefreshOutcome> {
  const outcome = await performRefresh(apiBase);
  if (outcome.status === "ok") setSessionMarker();   // see O5
  return outcome;
}
```

The `setSessionMarker()` on success is a **repair**, not a stamp-then-hope: the
refresh has just proved a cookie exists, so leaving the marker unset would keep
the store in a knowingly-false state and lose the session at the next cold load.
It is best-effort by construction (`setSessionMarker` swallows a throwing
storage). **O5** asks for a ruling on its one awkward case.

After (e), exactly one `signed-out` in the whole store does not clear: the marker
gate in `refreshAccessToken`, whose callers are the anonymous-safe ones. §4.3
routes around it.

**Outcome table after this change:**

**The rule inverts: `signed-out` is an allowlist of one.** Only a **401** clears
anything — the single status that means "the credential I presented was rejected".
Everything else that is not `ok` is `unavailable`, because it says something about
the *server* and nothing about the session (G2, defect C).

| Response | Outcome | Token | Marker |
| -------- | ------- | ----- | ------ |
| 2xx with a token | `ok` | installed | kept |
| **401** | **`signed-out`** | **cleared** | **cleared** |
| 409, then 2xx on retry | `ok` | installed | kept |
| 409 twice | `unavailable` | kept | kept |
| 503 | `unavailable` | kept | kept |
| **429** (the global rate limiter, `Retry-After`) | **`unavailable`** | **kept** | **kept** |
| **408, 5xx, 4xx other than 401** | **`unavailable`** | **kept** | **kept** |
| **2xx without a token** | **`unavailable`** | **kept** | **kept** |
| transport failure / timeout | `unavailable` | kept | kept |

Three rows changed in round 12, and each was a way to log a user out for something
that was not their session:

- **429** — reachable on every refresh, since the rate limiter is global
  (`cmd/server/middleware.go:131`). A burst of tabs rotating at once is exactly
  the traffic shape that trips it, so this row and §4.1a's lock are related: the
  lock makes 429 rarer, this row makes it harmless.
- **408 / 5xx / other 4xx** — a proxy timeout, a 502 during a deploy, a 404 from a
  misrouted host. None is a session ending.
- **2xx without a token** — v1 called this "a broken response, not an outage" and
  signed the user out. It is a broken response, which is the reason *not* to act on
  it: a server that answers 200 with no body has told us nothing about the session.

**This row set is only correct once §4.9 lands.** Until infrastructure failures
answer 503 instead of a codeless 401, the `401 → signed-out` row still swallows a
Mongo outage. That ordering is a hard dependency, not a nicety (§7).

### 4.2 The one helper

New `src/api/authedFetch.ts`, exporting `authedFetch(path, init)`. It:

1. merges headers per the rule below, ending with `Authorization: Bearer
   <in-memory token>` when one exists;
2. sets `credentials: 'include'`;
3. applies §4.3 on a 401.

**Header merging — `new Headers`, not object spread.** The helper's `init` is a
`RequestInit`, so `init.headers` is a `HeadersInit`: a plain object, **a `Headers`
instance, or an array of `[name, value]` tuples**. Object spread only handles the
first. Spreading a `Headers` yields `{}` — it has no own enumerable properties, so
every header the caller set is dropped **silently**; spreading a tuple array yields
`{0: [...], 1: [...]}`, which `fetch` then rejects or mangles. Today every call
site passes an object literal, so the four existing wrappers get away with it;
folding them into one helper that advertises `RequestInit` is exactly when that
stops being safe.

```ts
const headers = new Headers(init?.headers);      // normalises all three shapes
if (!headers.has("Accept")) headers.set("Accept", "application/json");
if (!headers.has("Content-Type") && jsonBody(init?.body)) {
  headers.set("Content-Type", "application/json");
}
if (token) headers.set("Authorization", `Bearer ${token}`);   // last, always wins
```

Three rules, each load-bearing:

- **Defaults only when absent** (`has` before `set`) — a call site that sets its
  own `Accept` or `Content-Type` keeps it, which object spread achieved only by
  accident of key order.
- **`Content-Type` only for a body we know is JSON.** A `FormData`, `Blob` or
  `URLSearchParams` body must set its own — forcing `application/json` on
  `FormData` destroys the multipart boundary. No call site does this today
  (`avatar.ts`'s blob PUT bypasses the helper entirely, §4.6), which is precisely
  why the guard belongs here now rather than being discovered later.
- **`Authorization` last, via `set`** — not appended, not conditional on absence.
  This is where §4.2's precedence decision below is actually enforced: a call site
  cannot override the bearer, whether it passed one in an object, a `Headers`, or
  a tuple.

The retry (§4.3) rebuilds the headers through the same construction with the fresh
token, so it can never carry the stale `Authorization` by inheriting a
half-mutated object.

It is the only 401 algorithm that survives this change: `client.ts`'s rival
implementation is deleted, not left dormant (§4.8).

`auth.ts::jsonFetch` (the **anonymous** path) is untouched and gains nothing: a 401
from `login`, `mfa/login/verify`, `register`, `forgot-password`, `reset-password`,
`accept-invite`, `policy`, `providers` or `oauth/login` means "those credentials
are wrong" or "not signed in", never "the token expired".

**Header precedence is unified.** `avatar.ts` currently lets the bearer win over
caller headers; `billingProfile.ts` lets caller headers win over the bearer — a
divergence that exists only because each wrapper chose its own spread order. The
helper takes `avatar.ts`'s order, enforced by the `set`-last rule above rather
than by key ordering.

### 4.3 The 401 branch — capture the sent token, then four outcomes in this order

The helper records the token it actually put on the wire **before** the request
leaves, and decides from it. This matters because `performRefresh` clears
`inflightRefresh` in a `finally` the moment the rotation resolves
(`tokenStore.ts:103`), so a 401 that comes back even slightly later finds no
in-flight promise to join. Neither naive reading of "the token" is correct on its
own:

- judging by **the store** — a sibling already installed a fresh token, so the
  expired-token test fails, the request falls through to "not a token problem",
  and the caller is handed the stale 401 even though a usable token exists;
- judging by **the sent token** alone — it is genuinely expired, so a second
  rotation fires for a token that has already been replaced.

The two are used for different questions. **The sent token decides whether it was
plausibly the cause; the store decides how to recover.**

The expiry is judged **at send time, with no margin**: the only question that
makes recovery safe is "was this token already dead when it left?" — see §4.4.

```ts
// Captured together, before the fetch: at 401 time the store's expiry may
// already belong to a token a sibling installed (§5.1), and `sentAt` is the
// instant the whole decision below turns on.
const sent = getAccessTokenSnapshot();    // { token, expiresAt }
const sentAt = Date.now();
const res = await doFetch(path, init, sent.token);
if (res.status !== 401) return res;

// A Response body is a single-use stream. Every inspection below reads a
// CLONE, so whatever we hand back is still unread (§5.11).
const terminal = await terminalCode(res.clone());
```

with the one place that knows the closed set:

```ts
const TERMINAL_CODES = new Set(["session_revoked", "session_max_age_reached"]);

// Reads a CLONE, never the response a caller will get. A body that is absent,
// not JSON, or carries no top-level `code` is simply "not terminal" — which is
// the ordinary case, not an error condition (§3.C: the generic paths emit no
// top-level `code`, keeping their internal one in `errors[0].value`, which we
// deliberately do not read).
async function terminalCode(clone: Response): Promise<string | null> {
  const body = (await clone.json().catch(() => ({}))) as { code?: string };
  return typeof body.code === "string" && TERMINAL_CODES.has(body.code)
    ? body.code
    : null;
}
```

**Why the clone is load-bearing, not hygiene.** Callers pass the response to
`readError` (`auth.ts`), which does `await res.json()` to build the `ApiError`
carrying `detail` and `code` — and wraps it in `.catch(() => ({}))`. On an
already-read body `json()` rejects with a `TypeError`, that catch swallows it, and
the caller silently gets the fallback message with **no code at all**. Pages branch
on `apiErrorCode(e)`, so the failure would not be a crash to debug but a set of
error branches that quietly stop matching.

**The `WWW-Authenticate` shortcut is not available.** `sendSessionRevoked` also
sets `WWW-Authenticate: Bearer error="<code>"`, which would give the terminal code
without touching the body — but it is not in the API's CORS `ExposedHeaders`
(`cmd/server/middleware.go:103`, which lists only `Link`, `X-Total-Count`, the two
rate-limit headers and `X-New-Access-Token`), and this SPA is cross-origin to the
API host. JS cannot read it. Do not "simplify" the clone away by reaching for it
without adding the header to that list first. §4.9 is the only backend change in
scope, and a CORS `ExposedHeaders` edit is not it.

| # | Condition | Action |
| - | --------- | ------ |
| 1 | `terminal !== null` — the cloned body carried a terminal `code` | Clear token + marker. **No refresh, no retry** — a token minted from the same cookie carries the same dead `sid`. Return the original response. |
| 2 | **NOT** (`sent.expiresAt !== null` **and** `sent.expiresAt <= sentAt`) — i.e. the token was *not* provably dead before it was sent, including when its expiry is unknown | Return the original response **unchanged**. The request may have reached the handler, so a retry could re-consume whatever it consumed: no refresh, no replay (**G4**, §4.4). |
| 3 | Store now holds a **different, non-null** token than `sent.token` | A sibling already rotated. Retry **once** with the store's token. **No refresh** (**G8**). |
| 4a | Otherwise, **and a bearer was sent** (`sent.token` non-null) | `refreshAfterUnauthorized(apiBaseURL)` (§4.1e) — **not** marker-gated. `ok`: retry **once** with the new bearer. `signed-out`: `performRefresh` has cleared token **and** marker (**G3**); return the original. `unavailable`: return the original, token and marker untouched (**G2**, **G7**). |
| 4b | Otherwise, **and no bearer was sent** (`sent.token` null) | `refreshAccessToken(apiBaseURL)` — marker-gated, as today. A true anonymous visitor short-circuits with no request, and there is nothing to clear because there is no token. A marker-holding visitor whose request raced `AuthProvider`'s mount refresh joins the coalesced attempt. |

**No margin, and that is the whole point.** v12 asked whether the token had more
than `SKEW` of life left, which quietly widened "safe to retry" to include tokens
the *server still accepts* — a 30-second window in which a `change-password`
rejection was replayed and double-counted. The condition is now a strict
"already expired at `sentAt`", so `SKEW` plays no part in the 401 path at all;
the constant survives only for follow-up #1, which introduces it when it needs it.

Branch 2 is the replay guard, and it sits **ahead of every recovery branch**
(branch 1 precedes it only because a dead `sid` makes recovery pointless).
Because branches 3 and 4 sit behind it, they only ever see a request whose token
was already dead — so the "a sibling rotated, reuse its token" retry is covered by
the same proof, not by a separate argument.
Ordering is load-bearing, not cosmetic: v2 had the guard last, and dropping the
new "the token changed" branch in front of it would have replayed a
`change-password` rejection — a 401 earned on its own merits — merely because a
sibling request happened to rotate in the meantime (§4.4).

**Why 4a and 4b are different functions.** Routing everything through
`refreshAccessToken` would have re-introduced the hole this split exists to close:
its marker gate answers `signed-out` **without clearing anything**
(`tokenStore.ts:117`), so a tab holding a live in-memory token but no marker —
reachable two ways, §5.8 — would have been told "signed out", returned the raw
401, and then *kept* `isAuthenticated === true`, leaving the user in exactly
defect A's broken state that this spec exists to end. Splitting on "did we send a
bearer?" is the honest question: it preserves the anonymous optimisation for the
only case it was written for, and never lets it veto a real session.

At most **one** retry per call, whichever branch produces it. **The retry's own
401 is inspected too** — same `terminalCode(retried.clone())` — but only for the
terminal set:

- terminal code → clear token **and** marker, then return the retried response.
  The session died between the refresh and the retry; leaving a token that the
  server rejects is defect A's broken state all over again (**G3**).
- anything else → return the retried response untouched.

**Never a second refresh, and never a second retry**, in either case. A codeless
401 on the retry stays ambiguous — it can be the endpoint's own answer, the §4.4
mirror case being one — and clearing on it would sign out a user whose session is
fine because they mistyped a password.

**The property this buys.** Two requests sharing one expired token, in either
timing:

- both 401s arrive before the rotation resolves → branch 4 twice, coalesced by
  `inflightRefresh` into one `/refresh-cookie`;
- the second 401 arrives after it resolves → branch 3, which does not refresh
  at all.

Either way: **one rotation, both callers served.** Combined with §4.1's Web Lock
the same holds across tabs, where the coalescing does not reach.

The refresh endpoint is called through `tokenStore`, never through the helper, so
recursion is structurally impossible.

### 4.4 Why branch 2 exists — the replay hazard

`change-password` is an **authenticated** endpoint that answers **401** when the
*current password in the body* is wrong: `mapPasswordError` maps
`services.ErrInvalidCredentials` → `huma.Error401Unauthorized`. A blanket
"401 → refresh → retry" therefore **re-sends a failed password attempt**, and the
backend counts it again: `ErrAccountLocked` → `429 Too many failed attempts`. A
user who mistypes twice would be locked out as though they had tried four times.

Branch 2 removes it structurally, and the boundary has to be drawn exactly where
the *server* draws it.

**The invariant.** A replay can only double-count a request that actually reached
the handler. Since ADR-0020, `RequireAuth` rejects an expired bearer *before* the
handler runs — but it accepts the token until the **instant** it expires, with no
grace of its own. So the one condition that proves the handler never ran is
"the token was already expired when the request left". Nothing weaker will do.

**Why v12's margin was a hole, in numbers.** It treated a token as live only above
`now + SKEW` (30 s). A `change-password` sent with **20 s** of life left:

| | |
| --- | --- |
| Server | token still valid → `RequireAuth` accepts → handler runs → `ErrInvalidCredentials` → the failed attempt **is counted** → 401 |
| Client (v12) | `expiresAt (sentAt + 20s) > sentAt + 30s` is **false** → not "live" → refresh → **retry** → the same wrong password is sent again and counted **twice** |

Two mistypes would have tripped `ErrAccountLocked` (429) as though there had been
four. The margin existed to absorb latency and clock error; §4.5 already removed
the clock-error need, and latency was never a reason to widen a *safety* boundary.
Removing it closes the class rather than narrowing it.

**What this costs, stated plainly.** A token that was live at `sentAt` and expired
*in flight* is not auto-recovered: the 401 is surfaced and the caller sees one
failed request. It self-heals immediately — by the user's next action that token
is expired at send, so the very next request refreshes and succeeds. The window is
one request latency out of a whole TTL, and the alternative is guessing on the one
question where a wrong guess locks an account.

The same reasoning covers an **unknown** expiry (§4.5's fallback exhausted):
unknown cannot prove the handler did not run, so it counts as live and passes
through. This reverses the direction §4.5 gave before this round — "treat an
unreadable expiry as expired" was chosen when the failure mode was a wasted
refresh; under this rule the failure mode would be a replay, so it flips.

Any future authenticated endpoint that answers 401 for a body credential inherits
the protection without being listed anywhere.

### 4.5 Knowing whether the token had expired — duration, not wall clock

The question branch 2 asks is "had the token we sent already expired?". Answering
it by comparing the JWT's `exp` against `Date.now()` compares a **server-issued
absolute timestamp** with the **client's wall clock**, so it is only as accurate
as the difference between the two. A client clock behind by X classifies the token
as expired X seconds late, and for that whole window every 401 is misread as "not
a token problem" and passed through (§5.9).

Derive the expiry from the **duration** the server reported instead:

```ts
// tokenStore — the token and what we know about its life are one fact.
let accessToken: string | null = null;
let accessTokenExpiresAt: number | null = null;   // Date.now() domain, local

export function getAccessTokenSnapshot(): {
  token: string | null;
  expiresAt: number | null;
} {
  return { token: accessToken, expiresAt: accessTokenExpiresAt };
}
```

`expiresAt = Date.now() + expiresIn * 1000`, recorded the moment the token is
installed. Both the write and the read are taken from the same clock, so a
constant offset **cancels**: the reckoning is immune to clock skew rather than
tolerant of it, and the failure mode §5.9 describes disappears instead of being
bounded. `frontend-admin` derives its `tokenExpiry` the same way
(`baseApi.ts:212`), so the two SPAs agree on the technique.

Every path that installs a token must supply the duration. **None does today** —
the value reaches the API layer and is dropped one call short of the store — so
this is a migration, itemised in §4.6, not a property the code already has.

**Fallback, not a hard requirement.** Where `expiresIn` is missing — an older
backend, a shape we have not met — fall back to `src/lib/jwtExp.ts`: base64url-decode
the payload, read `exp`, return `null` on anything malformed, **no signature
verification** (a scheduling hint, never a security decision; the backend stays
the only authority). If that is unreadable too, the expiry is `null` — and §4.3
branch 2 treats an unknown expiry as **live**, so the request passes through
without a retry. That direction is the opposite of what earlier revisions said,
and the reason is §4.4: an unknown expiry cannot prove the handler never ran, and
under a rule whose failure mode is a *replay* rather than a wasted refresh, "don't
know" has to fall on the safe side.

Two decoding details, both probed rather than assumed:

- **`Number.isFinite(exp)`, not `typeof exp === "number"`.** `Infinity` is
  reachable through entirely valid JSON — `JSON.parse('{"exp":1e400}')` yields
  `Infinity`, whose `typeof` is `"number"`. An infinite `exp` would read as a token
  that never expires, so branch 2 would pass through **every** 401 forever and the
  recovery would be silently disabled. `-1e400` gives the mirror case, refreshing
  on every 401. Both must read as `null` → expired.
- **Convert the base64url alphabet before `atob`** (`-` → `+`, `_` → `/`). This is
  the part that actually breaks: `atob` throws `InvalidCharacterError` on `-` and
  `_`, while **missing padding is tolerated** in both Node and happy-dom (probed at
  lengths ≡ 1, 2 and 3 mod 4). Re-padding to a multiple of 4 is therefore belt-and-braces
  here, and correct to keep — a stricter `atob` elsewhere is a runtime away, and
  the WHATWG forgiving-base64 algorithm does specify failure at length ≡ 1 mod 4.

Note this is deliberately softer than `frontend-admin`, which treats a response
without `expiresIn` as a failed refresh (`baseApi.ts:130`). Turning a valid
rotation into a sign-out over a missing optional field is the wrong trade here.

**There is no `SKEW` in this design.** Earlier revisions carried a 30 s margin;
round 11 showed the margin *was* the replay hole (§4.4), and with the expiry
derived from a duration there is nothing left for it to absorb. The 401 path
compares `expiresAt <= sentAt` exactly. A `PROACTIVE_REFRESH_SKEW_MS` belongs to
follow-up #1, which introduces it when it has a use for it — and must then hold
ADR-0020 D3's invariant that it stays **strictly below `MinAccessTokenTTL`
(60 s)**. Do not reintroduce it here: a margin on this comparison is, precisely,
the bug this round removed.

### 4.6 Call-site migration

| File | Today | After |
| ---- | ----- | ----- |
| `api/auth.ts` | `authedFetch` (local) | imports the shared helper; `jsonFetch` unchanged |
| `api/avatar.ts` | `authedJson` (local) | imports the shared helper; `putAvatarBlob` **untouched** — it PUTs to the presigned object-store URL with `credentials: 'omit'` and no bearer |
| `api/billingProfile.ts` | `authedJson` (local) | imports the shared helper |
| `api/dsr.ts` | `postJson` (local) | imports the shared helper; keeps its own body/error handling |

Bodies are re-sent from `init`, not from a consumed `Request` — every call site
passes a string or `undefined`. The helper documents that a streaming body is
unsupported (there are none, and a stream cannot be replayed).

### The token-lifetime migration (§4.5)

`expiresAt` is only as good as its propagation, and today the duration dies one
call short of the store. The API layer is already correct — `LoginResult` and
`MfaLoginVerifyResult` both **declare and return** `expiresIn` — so no backend and
no API-wrapper change is needed. The value is available at every call site and is
simply dropped:

| Site | Today | After |
| ---- | ----- | ----- |
| `tokenStore.setAccessToken` | `(token: string \| null)` | takes the lifetime alongside the token; records `expiresAt` (§4.5) |
| `tokenStore.clearAccessToken` | `setAccessToken(null)` | unchanged in meaning — clears both fields |
| `tokenStore.performRefresh` | installs `fresh` only | installs the pair from `body.expiresIn` (§4.1d) |
| `authContext.AuthState.signIn` | `(token: string) => void` | carries the lifetime |
| `AuthProvider.signIn` | `useCallback((next: string) => …)` | same signature change |
| `LoginPage.complete(token)` | takes a bare string | takes the result, which already has `expiresIn` |
| ↳ `LoginPage` password login | `complete(result.accessToken)` | passes the whole `LoginResult` |
| ↳ `LoginPage` MFA challenge | `complete(result.accessToken)` | passes the whole `MfaLoginVerifyResult` |
| `OAuthCallbackPage` MFA success | `signIn(result.accessToken)` | passes the lifetime too |

`MfaChallenge`'s `onSuccess` prop already hands over the whole
`MfaLoginVerifyResult`, so no component prop changes — the loss is entirely in the
`.accessToken` projections at the two `signIn` calls.

**One thing to fix rather than carry over.** `auth.ts`'s login mapper writes
`expiresIn: body.expiresIn ?? 900` — it **fabricates** a fifteen-minute lifetime
when the field is absent. Under §4.5 that is a lie the client would then act on: a
deployment running a 60 s TTL would record 900 s and, by §4.3 branch 2, read every
subsequent 401 as "not a token problem" for the rest of that quarter hour. It
never fires against the current backend, which always sends the field, and an
*unknown* lifetime lands on the same branch anyway — so this is not a correctness
change today. It is an honesty one, and it matters for follow-up #2, whose
proactive rotation would schedule against the fabricated number. Make it
`undefined` (unknown) and let §4.5's fallback chain decide.

`dsr.ts` throws a bare `Error("request failed (n)")` where the others throw the
shared `ApiError` carrying `status` + `code`. **Unified here (O2 ruling), verified
free:** `AccountDsrPage` has exactly two error branches, both `isError` with fixed
copy (lines 54 and 83), and neither reads the message, the status or the code;
there is no page test to update. Nothing visible changes, and G5's "one helper"
becomes literally true instead of true-with-an-exception.

_Noticed while checking, out of scope:_ those two strings are hard-coded English
rather than `t()` calls, against this SPA's own i18n rule. Left alone — a bug-fix
PR is the wrong place to change user-visible copy. §8 records it.

### 4.7 Documentation, same commit

Four documents, and the list is longer than earlier revisions assumed because
§4.9 makes this a backend change too.

**`frontend-client/CLAUDE.md`** currently documents the gap (`82f25252`): its
"Refresh choreography" section states that an expired token does *not* refresh, and
names this fix. Rewrite it to the shipped behaviour — §4.1's outcome table, §4.3's
branch table, and why **branch 2** is the replay guard. "How auth works" item 1 and
the `credentials` convention bullet also name the helper.

**`backend/internal/core/auth/CLAUDE.md`** — §4.9 adds `refresh_lookup_unavailable`
and moves four error returns from 401 to 503. That is documented surface: the
module contract must say which failures on the refresh path are authentication
answers and which are outages.

**`docs/site/architecture/authentication-flow.mdx`** — two edits. §4.9's status
change belongs beside the existing rotation description (line 149's grace/409
paragraph). And **line 226 carries the same false claim as ADR-0020 D2**: "A
browser client therefore recovers from an expired access token with `401 → POST
/v1/auth/{tier}/refresh-cookie → retry`. **Both SPAs implement it**" — then it
substantiates only `frontend-admin`, because only `frontend-admin` does. This is
the canonical published page, so unlike the ADR it is not a historical record; it
is a reference that is wrong today and becomes right the moment this ships.

**`docs/site/modules/core/auth.mdx`** — line 190 already explains why session
enforcement answers 503 rather than 401; `refresh_lookup_unavailable` is the same
argument applied to a sibling failure and belongs in the same place.

Per the user's ruling of 2026-09-01, **ADR-0020 is not edited**: once this ships,
D2's claim is simply true. The same reasoning is why line 226 above is corrected
*with* the implementation rather than ahead of it.

### 4.8 Deleting the second 401 algorithm

`client.ts`'s `refreshMiddleware` is not a harmless stub waiting for codegen. It
is a **complete and wrong** implementation of the thing this spec is specifying,
and every defect the rest of the document argues against is in it:

| `refreshMiddleware` does | Why it is wrong | Fixed here by |
| ------------------------ | --------------- | ------------- |
| Retries **any** 401, with no token-state gate | Replays a `change-password` rejection and double-counts it toward the 429 lockout | §4.3 branch 2 (§4.4) |
| `body: request.body` on the retry | `request` has already been sent, so its body stream is disturbed; a bodied retry throws instead of being re-sent | §4.6 (re-send from `init`) |
| Calls `refreshAccessToken` | Marker-gated: a live session with no marker is told "signed out" and nothing is cleared | §4.1e / §4.3 branch 4a |
| `clearAccessToken()` on a non-`ok` outcome | Clears the token but **not** the marker, leaving a marker that will short-circuit the next cold load | §4.1's `signedOut()` (G3) |
| Reads `response` for the retry decision, returns the same object | No `code` inspection at all, so a revoked session is retried instead of ending | §4.3 branch 1 (§5.12) |

None of it runs today, which is exactly what makes it dangerous: it looks
finished, it is named as the live path in the docs that shipped before
`82f25252`, and the first person to `import { api }` inherits all five defects at
once. That is the trap #325 was born from — code that reads as the request path
while nothing routes through it.

**Delete `api`, both middlewares, and the now-unused imports** (`createClient`,
the `paths` type, `getAccessToken`, `refreshAccessToken`, `clearAccessToken`).
`client.ts` keeps what it actually provides: the `window.__ORKESTRA_CONFIG__`
resolution and the `apiBaseURL` export every consumer already imports.

**Verified safe across the whole chain before proposing it** — the lesson from
`@stripe/stripe-js`, which is unused upstream but imported by commons, so removing
it would have broken the fork at the next sync. This is the opposite case:

```
$ grep -rn "import {[^}]*\bapi\b[^}]*} from \"@/api/client\"" src        # upstream, tests included
(no matches)
$ … the same over commons / gaterei / octolabs / hermes                     # 0, 0, 0, 0
```

`openapi-fetch` appears in every fork **only inside `client.ts` itself** — the
same inherited dead code — and gaterei's `publicForms.ts` even carries a comment
saying it is deliberately "NOT routed through the openapi-fetch client". Nobody
loses anything.

`openapi.gen.ts` **stays**: it is the committed codegen target the README
documents, and follow-up #2 needs it. The `openapi-fetch` dependency stays too,
for the same reason — with one honest consequence to accept: Dependabot will keep
proposing bumps for a package nothing imports, and per this repo's own rule those
are vacuous and should be closed rather than merged. Dropping the dependency is a
one-line follow-up if that noise outweighs the convenience; it is deliberately not
bundled into a bug-fix PR because a dependency removal propagates down the fork
chain on its own schedule.

### 4.9 Backend: classify infrastructure failures on the refresh path

The one backend change in scope, and it is a classification fix, not a new
feature: **an infrastructure failure must not be answered as an authentication
failure.** ADR-0017 already decided this for session enforcement and gave it a
503; the refresh path has four sibling sites that never got the same treatment
(§1 defect C).

In `RefreshTokensWithRiskAssessment` (`services/auth_service.go`), each of the
four generic wraps becomes a sentinel that `writeRefreshErr` answers as **503**:

| Site | Today | After |
| ---- | ----- | ----- |
| `refreshTokenRepo.GetByTokenAny` | `fmt.Errorf("failed to validate refresh token: %w")` → 401 | `ErrRefreshLookupUnavailable` → 503 |
| the user lookup | `fmt.Errorf("user not found: %w")` → 401 | same sentinel → 503 |
| `mintTokenPair` | `fmt.Errorf("failed to mint token pair: %w")` → 401 | same sentinel → 503 |
| `refreshTokenRepo.RotateWithFamily` | `fmt.Errorf("failed to rotate refresh token: %w")` → 401 | same sentinel → 503 |

The "user not found" wrap deserves naming: a store error there is reported with
the words of a terminal condition, which is how an outage acquires the appearance
of a deleted account. A genuine `nil` user stays `ErrInvalidRefreshToken` → 401;
only the *error* return changes.

`writeRefreshErr` gains one branch beside the `ErrSessionEnforcementUnavailable`
one it already has, emitting 503 with a **distinct** code —
`refresh_lookup_unavailable` — rather than reusing
`session_enforcement_unavailable`. Both are 503 and both clients treat 503
identically, so the distinction costs nothing on the wire and buys the thing
ADR-0017 D4 argued for elsewhere: whoever reads the support ticket can tell which
subsystem failed.

**Why this is safe to ship on its own.** It is additive in the only direction that
matters — a response that was 401 becomes 503 exactly when the server failed. Both
in-tree SPAs already treat 503 as transient (`frontend-admin`'s `refreshOnce`
returns `retry: true`; this SPA's `performRefresh` returns `unavailable`), so the
change makes today's operators stop being logged out by Mongo blips **before** any
client work lands. It is genuinely a fix on its own merits, not scaffolding.

**Explicitly out of scope:** `RequireAuth`'s expired-bearer 401 (§3.D). That is a
different code path, on every protected route rather than one endpoint. **O6.**

## 5. Edge cases

1. **A delayed second 401, same tab (G8).** Requests A and B go out together with
   the same expired token. A's 401 returns first, rotates, installs the new token
   — and `performRefresh`'s `finally` clears `inflightRefresh` immediately
   (`tokenStore.ts:103`), so B, whose 401 arrives later, finds nothing to join.
   B takes §4.3 branch 3: the store's token differs from B's `sentToken`, so it
   retries with it and **does not rotate again**. Deliberately delayed in the
   test suite (§6) rather than left to timing.
2. **Two tabs, same expiry (G6).** Both take a 401, both reach branch 4a and call
   `refreshAfterUnauthorized`.
   The Web Lock admits one; the other runs after it, presenting the successor
   cookie its predecessor left behind, and succeeds. Neither is signed out.
3. **Lock unavailable, race still lost (G7).** Without Web Locks both post at once;
   the loser gets 409, retries once inside `performRefresh`, and lands. A double
   409 is `unavailable` — the request's original 401 surfaces, the session stays.
4. **Grace window.** `RefreshRotationGrace` is 10 s: a cookie superseded moments
   ago is answered with a retry hint rather than a replay kill. For the sibling
   race the 409 retry therefore almost always succeeds, because the successor is in
   the jar; the double-409 path is the rare tail and fails safe. §5.12 covers the
   case where the successor is in *nobody's* jar, which the grace cannot rescue.
5. **`navigator.locks` is `null`, not absent, in happy-dom 20.9** (verified). Since
   `typeof null === "object"`, a guard written `typeof navigator.locks === "undefined"`
   would pass and then throw on `.request`. The guard must be `!locks?.request`.
   Tests that need to exercise the locked path must inject a stub.
6. **Anonymous 401** — no marker, no token: `refreshAccessToken` short-circuits to
   `signed-out` without a request. Nothing to clear; original 401 returned.
7. **A token in memory with no marker — two ways in, one behaviour.** This is the
   state v3 got wrong, and neither route is exotic:
   **(i) storage that throws** — `signIn` calls `setSessionMarker()` first, which
   swallows the failure by design ("acceptable degradation", `sessionMarker.ts:26`),
   then installs the token. Private mode or blocked site data leaves a live
   session with no marker. **(ii) a sibling tab signed out** — `localStorage` is
   shared across tabs, so `clearSessionMarker()` removes it for everyone, while
   the in-memory token is per-tab and survives.
   Under §4.3 both land in branch **4a**, which does not consult the marker: the
   cookie is presented once. Route (i) with a valid cookie recovers and repairs
   the marker (§4.1e). Route (ii) normally gets a 401 — the sign-out invalidated
   the cookie server-side — and `performRefresh`'s `signedOut()` clears token and
   marker, so `AuthProvider` re-renders and `RequireAuth` redirects. Either way the
   user ends in a coherent state instead of a page that silently fails.
8. **TanStack Query `retry: 1`** — a failed query retries at the React Query level,
   producing a second helper call with its own (coalesced) refresh. Acceptable; the
   suite must not assert "exactly one request" where this can fire.
9. **Clock skew — no longer a failure mode (§4.5).** Worth recording what it
   *would* have been, because v4 stated it wrongly in both directions. Comparing
   the JWT's absolute `exp` against a client clock behind by X does **not** strand
   the user "until reload": the client's own clock passes `exp` too, so it starts
   refreshing X seconds late (less any margin, back when there was one) and then
   recovers on its own. But it is not a one-off either — the window reopens **every TTL cycle**, so a badly set clock
   means an app that fails for minutes out of every fifteen. Deriving the expiry
   from `expiresIn` at receipt removes the class: both ends of the comparison come
   from the same clock and a constant offset cancels. What survives is a clock
   *jump* between installing a token and reading it, which self-corrects at the
   next rotation.
10. **No `X-Retry` header.** The deleted middleware marked its retry with one
    (§4.8); the helper holds retry state in a local variable instead and must
    **not** send it. Nothing on the backend reads it, so it was only ever a way
    for a stateless middleware to talk to itself — and after §4.8 no code in the
    tree emits it at all.
11. **MFA step-up 401 — a top-level `code` that must be ignored.** Both emitters
    (`jwt_validator.go:425` and `auth.go:1253`) answer 401 with a top-level
    `code: "step_up_required"`, and `mfa_enrollment_required` /
    `password_confirm_required` join it from the same family. They are **not** in
    `TERMINAL_CODES`, so `terminalCode()` returns `null`, and with a live token
    they land in branch 2 and are surfaced to the caller. Correct — and the reason
    branch 1 tests set membership rather than "has a code" (§3.C). The client SPA
    has no step-up flow yet, so the caller's own error handling is what the user
    sees; that is unchanged by this spec.
12. **A timeout can strand the rotation — and *where* it fires decides whether it
    matters.** The browser applies `Set-Cookie` while processing response
    **headers**, before `fetch` resolves and independently of whether our JS ever
    reads the body. That splits the timeout into two very different cases:

    - **Abort during the body read** (the common one now that §4.1c's timer spans
      the body): the headers were processed, so **the successor cookie is already
      in the jar**. The refresh is reported `unavailable`, the token and marker are
      kept, and the *next* attempt presents the successor and succeeds. Harmless —
      and it is the window round 13's fix moves aborts **into**.
    - **Abort before any headers arrive**, or a response lost on the wire: the
      server may have rotated anyway. The successor then exists only server-side,
      and the jar holds a cookie that is already superseded. The next attempt gets
      a 409 inside `RefreshRotationGrace` (10 s) — which cannot make progress,
      because `benignRotationRetry`'s own comment says progress "still requires the
      successor cookie, which only the legitimate client holds" — and once the
      grace elapses, presenting it again is **replay detection**: the family is
      revoked and the session really does end.

    This residual is **inherited, not introduced**: ADR-0020's Consequences already
    record that "a `/refresh-cookie` response lost client-side leaves the jar
    holding the superseded cookie … after it, presenting that cookie is a replay by
    design." The timeout adds one more way to reach it, and the client cannot
    repair it — it has no way to learn a token it never received. What it does do
    is fail safely at every step: `unavailable` keeps the session while there is
    any hope, and the eventual `refresh_token_replay` 401 is an explicit rejection,
    which §4.1's allowlist correctly treats as a sign-out.

    Note also that `REFRESH_FETCH_TIMEOUT_MS` (10 s) and `RefreshRotationGrace`
    (10 s) are **equal by coincidence, not by design**. Do not build on the idea
    that a retry after a timeout lands inside the grace: it may not, and even when
    it does, the grace cannot help a client that never received the successor.

13. **The response body is single-use, and so is the retry's.** Every branch that
    returns "the original response" must return one whose body no caller has
    touched — including the paths that never inspect anything, since branch 1's
    inspection happens before the branching. Hence the clone is taken once, up
    front, for *every* 401. The retried response gets the same treatment. What is
    handed back is always the real response, never the clone.

## 6. Testing

MSW + `renderWithProviders`, matching the existing suite (193 tests, 15 files;
`onUnhandledRequest: 'error'`).

**`src/auth/tokenStore.test.ts` — additions (§4.1).** No existing case asserts the
409 or the lock, so all of these are additive; no existing assertion is edited.

- **409 then 2xx → `ok`**, token installed, **marker still present** (G7);
- **409 twice → `unavailable`**, token **and marker** both kept — the regression
  test for defect B;
- 409 then 401 → `signed-out`, both cleared;
- a stubbed `navigator.locks.request` is called once with `orkestra:auth-refresh`,
  and the callback runs inside it;
- with `navigator.locks` **null** (happy-dom's default) the refresh still happens —
  the fallback path, and the §5.5 guard regression test;
- two concurrent refreshes → **one** `/refresh-cookie` request (the in-tab
  coalescing still holds after the lock is added). Run it **mixed** —
  `refreshAccessToken` and `refreshAfterUnauthorized` at once — since both now
  funnel through `performRefresh` and a regression that coalesced only within one
  entry point would otherwise pass;
- **the inverted rule, one case per changed row** (§4.1, defect C) — each asserts
  `unavailable` **and** that the token *and* marker are still there: **429** with a
  `Retry-After`, **500**, **502**, **504**, **408**, a **404**, and a **2xx with no
  token in the body**. Written as an `it.each` table so a future status is one row,
  not a new test;
- the mirror: **401 → `signed-out`, both cleared** — the allowlist's only member,
  so the table above cannot silently become "nothing ever signs out";
- **a fetch that never resolves → `unavailable` after `REFRESH_FETCH_TIMEOUT_MS`,
  with token *and* marker still present.** Deterministic under `vi.useFakeTimers()`
  **only** because §4.1c builds the signal from `AbortController` + `setTimeout`;
  if someone "simplifies" it back to `AbortSignal.timeout`, this case stops
  aborting under the fake clock and either hangs or fails — which is the tripwire
  that keeps the divergence honest. Advance past the timeout with
  `vi.advanceTimersByTimeAsync`;
- **a response whose headers arrive promptly but whose body stalls** → the refresh
  still ends `unavailable` at `REFRESH_FETCH_TIMEOUT_MS`, token and marker kept.
  This is the round-13 regression test and it must be written against a **streamed**
  body (MSW supports a `ReadableStream` response), not a delayed whole response:
  a delayed *response* is caught even by a timer cleared right after `await fetch`,
  so that shape would pass against the bug;
- **and the lock is released**: with a stubbed `navigator.locks`, a second refresh
  queued behind the stalled one runs once the timeout fires rather than hanging —
  the assertion that actually pins §4.1a's transitive bound;
- the timer is **cleared** on a refresh that answers normally: after a successful
  refresh, advancing the fake clock past the timeout aborts nothing and produces
  no further request.

**`refreshAfterUnauthorized` (§4.1e) — additions:**

- **no marker, token in memory, cookie valid** → a `/refresh-cookie` request *is*
  made (the assertion is on the request having happened, not just the outcome),
  the new token is installed, and the marker is **repaired**;
- **no marker, token in memory, cookie dead (401)** → `signed-out` **and both the
  token and the marker are cleared** — the G3 guarantee, and the direct
  regression test for v3's false assumption;
- `refreshAccessToken` with no marker still short-circuits with **no** request:
  the anonymous optimisation must survive this change untouched.

**`authedFetch` — additions for branch 4a/4b:**

- **expired token, marker absent, 401 → the refresh is attempted anyway** and the
  retry succeeds. Fails against a v3-shaped implementation, which would have
  returned the raw 401 and left the user signed-in-but-broken;
- same, but the refresh 401s → the caller gets the original 401 **and**
  `getAccessToken()` is now `null`, so `AuthProvider` can re-render;
- **no bearer sent, no marker** (anonymous) → **zero** `/refresh-cookie`
  requests, original 401 returned.

**New `src/api/authedFetch.test.ts`:**

- expired token → 401 → refresh 200 → retry carries the **new** bearer → caller
  sees the retry's 200; exactly one `/refresh-cookie`;
- expired token → 401 → refresh **503** → caller sees the original 401, token
  still in memory (G2);
- expired token → 401 → refresh **409 twice** → caller sees the original 401,
  **marker and token kept**, user not signed out (G7);
- expired token → 401 → refresh **401** → token and marker cleared (G3);
- **live token → 401 → no `/refresh-cookie` at all, response passed through**
  (G4 / §4.4 — the lockout-hazard regression test);
- **the round-11 case, and it must be written with a token that is close to
  expiry but not past it**: a `change-password` sent with **20 s** of remaining
  life, answered 401 → **zero** `/refresh-cookie` requests and **exactly one**
  request to `/change-password` for the whole call. A token with 20 minutes left
  passes this test against the broken implementation too, so the fixture's
  remaining life is the test: pick a value inside any plausible margin (20 s
  against the 30 s that used to be there) and assert the remaining life in the
  test itself, the way §6's base64url fixture asserts its own shape;
- the boundary either side of it: `expiresAt === sentAt` → treated as expired →
  refresh and retry; `expiresAt === sentAt + 1` → live → passed through. Pins that
  the comparison is `<=` at the exact instant and carries no hidden margin;
- **unknown expiry** (`expiresAt === null`, §4.5's fallback exhausted) on a POST →
  passed through, **no** refresh. This is the case whose direction flipped in
  round 11, so it needs a test that fails against the old direction;
- 401 with `code: session_revoked` on a live token → cleared, no refresh;
- the retry's 401 is returned and does not trigger a second refresh;
- a parallel burst of three 401s produces exactly one `/refresh-cookie`.

**Header merging (§4.2)** — the shapes are the point, so the cases are the shapes:

- `init.headers` as a **`Headers` instance** → its headers reach the wire. Fails
  against an object-spread implementation, which would send none of them;
- as an **array of tuples** → likewise;
- as a plain object → unchanged behaviour (the regression guard for the migration);
- a caller-supplied `Accept` / `Content-Type` **survives**, and one that is absent
  gets the JSON default;
- a caller-supplied `Authorization` is **overridden** by the in-memory bearer, in
  all three shapes — the precedence rule, tested where it can actually break;
- a `FormData` body gets **no** `Content-Type` from the helper;
- the retry carries the **new** bearer and no trace of the old one.

**Body integrity (§5.13)** — the test the finding asks for, and it must assert on
the *caller's* view, not on `bodyUsed`:

- a passed-through 401 (branch 2, live token) whose body carries `detail` and
  `code` → the caller's `readError(res, …)` still yields **both**. Written as
  "`apiErrorCode(err)` is `'…'` and the message is the backend's `detail`", so it
  fails against an implementation that read the body directly — where the
  swallowed `TypeError` would hand back the fallback message and `undefined`;
- the same for a 401 returned after an `unavailable` refresh (branch 4a) and for
  a **retried** 401 (both the terminal and the codeless case) — three more paths
  that return a response someone else will read;
- a 401 with a non-JSON body, and one with an empty body → treated as not
  terminal, no throw, and still readable downstream.

**Terminal codes:**

- 401 with `code: session_revoked` on a live token → cleared, **no**
  `/refresh-cookie` request at all;
- `session_max_age_reached` likewise — the second member of the set, since a
  `Set` with one entry and an `includes` on the wrong field both pass a
  single-code test;
- **a 401 carrying a non-terminal top-level `code`** — `step_up_required`, and
  `mfa_enrollment_required` as a second row — on a **live** token → passed through
  untouched: nothing cleared, no refresh, no retry. This is the test that fails
  against `if (body.code) → clear`, the simplification §3.C exists to forbid;
- a 401 whose `code` lives only at `errors[0].value` (the generic
  `sendErrorResponse` shape, where that value is `CodeInvalidCredentials`) → **not**
  terminal. Pins that the implementation reads the top level and not the array;
- expired token → refresh `ok` → **retry 401s with `session_revoked`** → token and
  marker cleared, exactly **one** `/refresh-cookie` for the whole call, and no
  second retry;
- same but the retry's 401 carries **no** code → nothing cleared, response
  returned as is. Guards against "any retry 401 means signed out", which would
  sign out the §4.4 mirror case for mistyping a password.

**The delayed-401 cases (G8, §4.3 branch 3, §5.1)** — the timing must be forced,
never left to the scheduler. The endpoint handler holds B's response behind a
deferred promise the test resolves only after A's `/refresh-cookie` has settled,
so "B's 401 comes back after the rotation" is a fact of the test, not a hope:

- **A and B share an expired token; B's 401 is released after A's rotation
  completes** → exactly **one** `/refresh-cookie` request; B's retry carries the
  **new** bearer; B's caller sees the retried 200, *not* the stale 401. This is
  the regression test for both wrong readings named in §4.3 — it fails with a
  second rotation under one, and with a returned 401 under the other.
- **Same setup, but B's request is a live-token 401** (the token it sent was
  refreshed before it was even sent, so it is not expired) → branch 2: response
  passed through, no retry, no rotation. Guards the ordering: a `change-password`
  rejection must not be replayed just because a sibling rotated meanwhile (§4.4).
- **B's sent token is expired and the store's token is `null`** (a sign-out
  landed mid-flight) → branch 4 with no marker → `signed-out` without a request;
  the original 401 surfaces and nothing is retried.

**Lifetime propagation (§4.6)** — the migration's own tests, because a dropped
`expiresIn` fails *silently*: the token still installs, and only the 401 path
misbehaves, hours later.

- after a **password login**, `getAccessTokenSnapshot().expiresAt` is set and
  matches the `expiresIn` the login response carried — not `null`, and not a
  fabricated 900;
- the same after the **MFA challenge** path in `LoginPage`, and after
  `OAuthCallbackPage`'s MFA success — the two `signIn` call sites that drop it
  today, so both need their own case;
- the same after a **refresh** (§4.1d);
- a login response **without** `expiresIn` records an **unknown** expiry, not 900
  (the `?? 900` fix) — and §4.3 branch 2 then passes a 401 through;
- an end-to-end guard worth its weight: sign in, advance the fake clock past the
  recorded lifetime, fire a request that 401s → the refresh **is** attempted. This
  is the test that fails if any single call site in §4.6's table is missed, since
  a dropped lifetime reads as "unknown" → "live" → no recovery.

**Expiry reckoning (§4.5)** — `src/auth/tokenStore.test.ts`:

- a refresh answering `expiresIn: 900` records `expiresAt ≈ Date.now() + 900_000`
  (fake timers, exact);
- **the skew-immunity test**: with the system clock moved **hours** off between
  installing the token and reading it back — but the *elapsed* time still under
  the lifetime — the token reads as **live**. This is the case the wall-clock
  design got wrong, and it fails against a `Date.now()`-vs-`exp` implementation;
- a refresh body **without** `expiresIn` still installs the token (not a
  sign-out — the deliberate divergence from `frontend-admin`) and falls back to
  the JWT's `exp`;
- neither present nor readable → the token counts as expired.

**`src/lib/jwtExp.test.ts`** (now only the fallback): a well-formed token, one
without `exp`, a non-base64 segment, a two-segment string, `null`/`""` — all `null`
but the first. Plus the two families below, which are the ones a naive
implementation passes.

**Non-numeric and non-finite `exp`** — every case must yield `null`, and the caller
must then treat the token as expired:

- `exp` as a **string** (`"1700000000"`), as `null`, as `true`, as an object, and
  absent — the `typeof` family;
- **`exp: 1e400` → `Infinity`**, and `-1e400` → `-Infinity`. These are the
  important two: they are valid JSON, they survive `JSON.parse`, and their `typeof`
  **is** `"number"`, so they slip past the obvious guard. Write them as raw JSON in
  the fixture (`{"exp":1e400}`), not as a JS `Infinity` literal, or the test proves
  something else — `JSON.stringify(Infinity)` emits `null` and the case collapses
  into the `typeof` family above.

**Unpadded base64url** — and the fixture has to be chosen, not assumed:

- a token whose payload segment is base64url **containing `-` and/or `_`**. The
  test must **assert its own fixture**: `expect(segment).toMatch(/[-_]/)` before
  asserting the decode succeeds. Without that guard the case is vacuous, because
  most ASCII JSON never produces those characters — a generator run over 2000
  candidate payloads found none. Two fixtures verified to work:

  | Payload | base64url segment |
  | ------- | ----------------- |
  | `{"exp":1700000000,"s":"?"}` | `eyJleHAiOjE3MDAwMDAwMDAsInMiOiI_In0` (`_`) |
  | `{"exp":1700000000,"s":"~~"}` | `eyJleHAiOjE3MDAwMDAwMDAsInMiOiJ-fiJ9` (`-`) |

  `?`, `>` and `~` are the ASCII characters that reach base64 indices 62/63 — a
  payload of ordinary words will not, which is why the fixture is chosen and
  asserted rather than generated;
- the same token segments with padding **stripped** at lengths ≡ 1, 2 and 3 mod 4.
  Note these pass even against a naive `atob`, since padding is tolerated here —
  they pin the behaviour against a stricter runtime rather than catching today's
  bug, and the comment should say so, so nobody reads a green run as proof the
  alphabet is handled.

**Backend (§4.9)** — Go tests beside the existing refresh-path ones, one per site,
each injecting a repository/infrastructure error and asserting the **status**, not
the message:

- `GetByTokenAny` returns an error → **503** with `code: refresh_lookup_unavailable`;
- the user lookup returns an error → **503** (not the 401 that "user not found"
  produces today — the case most likely to be written wrongly, because the current
  wording invites asserting a 401);
- `mintTokenPair` fails → 503; `RotateWithFamily` fails → 503;
- **the negatives, which are what stop this becoming a blanket 503**: a `nil` user,
  a `nil` token document and an expired token document each still answer **401**,
  and a superseded rotation still answers **409**. Regression-pin
  `ErrSessionEnforcementUnavailable`'s existing 503 too, so the new branch is proven
  to sit beside it rather than swallow it.

**The deletion (§4.8)** cannot be asserted by a unit test — there is nothing left
to call. It is verified the way an absence has to be:

- `grep` for any `api` import across `src/` returns nothing (the same check that
  justified the removal, re-run after it);
- `npm run typecheck` and `npm run build` pass, which is what catches an orphaned
  `paths` import or a now-unused `clearAccessToken`;
- `npm run lint` passes with `--max-warnings 0`, which is what catches the unused
  imports left behind in `client.ts`;
- the full suite stays green, proving nothing was routing through it after all.

**Regression:** `auth.test.ts`, `AuthProvider.test.tsx`, `OAuthCallbackPage.test.tsx`
and `App.test.tsx` must stay green unmodified. If one needs editing, that is a
signal the change is wider than this spec claims — raise it rather than adjust it.

**Manual.** Dev stack only — staging cannot serve the client tier at all while
`CLIENT_API_HOST=client-disabled.invalid` (§7).

Set `JWT_ACCESS_TOKEN_EXPIRY=60s` in `docker/.env` and restart the backend. **60 s
is the floor**, not an arbitrary choice: `NewJWTService` clamps anything smaller up
to `MinAccessTokenTTL` (`b3fdefee`), so a `10s` here silently behaves as 60 and
makes the wait look broken.

1. Sign in, wait past the TTL, act on `/account/security` → succeeds after exactly
   one `/refresh-cookie`.
2. Mistype the current password on change-password → **no** `/refresh-cookie`, and
   the attempt is sent once.
3. Two tabs **reloaded** together past the TTL → neither is signed out. This
   exercises `AuthProvider`'s mount refresh, *not* the 401 path — keep it, but it
   does not stand in for 4.

### 4. Two tabs crossing the TTL together (G6, G7, §4.1a)

The scenario the Web Lock exists for, and the one that signs users out today. It
has to be done by hand because it needs two real browsing contexts sharing one
cookie jar.

**Setup** — one browser profile, or the cookie jar is not shared and there is no
race to observe:

1. Sign in at `http://client.localhost:8081`.
2. Open a **second tab** on the same origin and navigate it to `/account/security`.
   Do this *before* the wait: `staleTime` is 30 s (`main.tsx:16`), so navigating
   after the wait would serve cache and fire nothing.
3. Open DevTools → Network in **both** tabs, filter on `refresh-cookie`, leave both
   recording. Note the value of `localStorage.orkestra_client_session_marker`.
4. Wait **> 60 s** touching neither tab. Switching between them is safe:
   `refetchOnWindowFocus` is `false` (`main.tsx:14`), so focus alone fires no query
   and rotates nothing while you set up.

**The race.** Trigger an authenticated request in both tabs as close together as
you can — a reload of the MFA status, or any mutation on the page. Sub-second is
enough; the whole rotation is tens of milliseconds, and the lock is what makes the
result deterministic rather than the timing.

**Expected — and note it is _two_ rotations, not one:**

- both requests succeed, and **neither tab redirects to `/login`**;
- the session marker is **still present** in `localStorage`;
- the two `/refresh-cookie` calls **do not overlap** in time — tab B's starts after
  tab A's has answered. That non-overlap *is* the Web Lock; it is the only directly
  observable evidence of it;
- **no 409 appears**, because the lock prevented the race from happening.

Two rotations is correct and worth understanding, because "exactly one" is the
tempting wrong expectation: §4.3 branch 3 compares against **this tab's** store,
and each tab has its own module-scoped token. Tab B cannot see the token tab A just
installed, so it legitimately needs its own — it simply must not be *signed out*
while getting it.

### 5. The same race with the lock removed (G7 — the 409 retry)

With Web Locks working the 409 path is unreachable by hand, so force the fallback.
In **tab B's console only**, before triggering:

```js
Object.defineProperty(navigator, "locks", { value: undefined, configurable: true });
```

`withRefreshLock` reads `navigator.locks` at call time (§4.1a), so this takes
effect immediately and only for that tab. Run the race again.

**Expected:** tab B's network panel shows a **409 `refresh_rotation_raced`
immediately followed by a second `/refresh-cookie` that succeeds**, and tab B stays
signed in. One 409 is the point of the test, not a fault.

**The failure signature to recognise:** tab B lands on `/login`, the marker is
**gone** from `localStorage`, and the network panel shows the 409 with **no**
follow-up request. That means the 409 is still collapsing into `signed-out`
(§1 defect B).

On **unmodified `dev`** this procedure produces no 409 at all — there is no 401
recovery, so nothing refreshes and the requests simply fail. Defect B's sign-out is
observable today only through the *mount-refresh* path: **step 3**, two tabs
reloaded together. Worth running that one first, before any code changes, to see
the bug this half of the spec is fixing — otherwise there is nothing to compare
step 5's "fixed" against.

## 7. Rollout and verification

- Gates: `make -C /home/tore/orkestra ci-frontend-client` **and** `ci-backend`
  (§4.9 is Go).
- **Ordering is a hard dependency, not a preference.** §4.1's `401 → signed-out`
  row is only sound once §4.9 has moved infrastructure failures off 401. Shipping
  the client half first would leave the Mongo-blip logout in place *and* strip the
  accidental cover it has today — with no 401 recovery there is no refresh, so no
  spurious sign-out from one. **§4.9 first, deployed, then the client work**, as
  two PRs or as one PR whose commits land in that order.
- §4.9 alone is a complete, shippable fix: both SPAs already treat 503 as
  transient, so it stops operators being logged out by storage blips before any
  client change exists.
- No migration, no config. A client that does not know `refresh_lookup_unavailable`
  sees a 503, which every current caller already handles.
- ⚠️ The staging round-trip is **blocked** — `docker/.env` has
  `CLIENT_API_HOST=client-disabled.invalid`, so every client-tier call 401s
  regardless. Same blocker as PR 4 §7. Verification is dev-stack + suite until that
  variable points at a real client API host.

**Fork-chain note.** `frontend-client/src/api/client.ts` is byte-identical across
upstream, commons, octolabs, gaterei and hermes, so §4.8's deletion will land on
all four at the next sync and conflict in none of them — but a fork that has
started importing `api` in the meantime would break. The check is one grep, and it
is worth re-running at sync time rather than trusting this measurement:
`grep -rn "from \"@/api/client\"" frontend-client/src` and confirm every hit
takes only `apiBaseURL`.

**Related defect, not fixed here.** `frontend-admin` has the same replay hazard:
`changePassword` (`src/store/api/authApi.ts:449`, `v1/auth/operator/change-password`)
goes through `baseApi` and is **not** in `AUTH_ENDPOINT_PATHS`, so a
wrong-current-password 401 triggers the silent refresh and re-sends the attempt,
double-counting toward the same lockout. It shares `mapPasswordError` with the
client route, and its hazard is **wider** than the one round 11 closed here: it
has no token-state gate at all, so the replay is not limited to a window near
expiry — every wrong-password 401 is re-sent. Own issue; the fix there is §4.3's
"already expired at send" gate, adding the path to `AUTH_ENDPOINT_PATHS`, or
follow-up #1, which removes the need for either.

## 8. Follow-ups (named, not started)

1. **Backend: a distinct `access_token_expired` code** (§3.D) — the robust answer
   to the replay hazard. `RequireAuth`'s expired-bearer 401 would carry it as a
   top-level `code`, alongside the ones already there (`session_revoked`,
   `step_up_required`, …), and §4.3 branch 2 would become "refresh iff the server
   said the token expired" instead of an inference from our own reckoning. It
   recovers the in-flight expiry §4.4 gives up, and closes `frontend-admin`'s
   copy of the same hazard (§7) at the same time. Cross-tier and touching auth
   middleware, so it deserves its own spec and its own tests rather than riding
   along with a client fix.
2. **Proactive rotation for the client SPA** (ADR-0020 D3 parity) — refresh before
   expiry instead of after a 401. Needs a trustworthy remaining-lifetime figure,
   which §4.5's `expiresAt` snapshot already provides — and provides *correctly*
   under clock skew, which is what makes a proactive scheme safe to build on. This
   is what introduces `PROACTIVE_REFRESH_SKEW_MS`; ADR-0020 D3's
   `SKEW < MinAccessTokenTTL` invariant applies to it, and it must not leak back
   into the 401 comparison (§4.5).
3. **Wake up `openapi-fetch`** — sharpen `openapi.gen.ts` against a real backend,
   re-add the typed client, and give it a middleware that **delegates to
   `authedFetch`'s policy** rather than restating it (§4.8 deleted the version
   that restated it, badly). Then migrate the wrappers and fold the two together.
4. **Drop the `openapi-fetch` runtime dependency** if the vacuous Dependabot bumps
   prove more annoying than the convenience of having it ready (§4.8).
5. **`frontend-admin`'s change-password replay** (§7) — subsumed by #1 if that lands first.
6. **`AccountDsrPage`'s hard-coded English error copy** (§4.6) — two strings that
   bypass `t()` against this SPA's own i18n rule. Not touched here: a bug-fix PR
   should not change user-visible copy.
7. **Align `frontend-admin`'s refresh timeout** with §4.1c's
   `AbortController` + `setTimeout`. Its `AbortSignal.timeout` is not controllable
   by fake timers either, so any test it grows around `REFRESH_FETCH_TIMEOUT_MS`
   inherits the same problem; the mechanism is a drop-in swap.
8. **`frontend-admin`'s 3-arg Web Lock test mock** — its own comment records that
   the existing test stays green while no longer exercising what it was written to
   exercise. Not this SPA's code, but the same primitive.

## Open questions — all ruled 2026-09-01

- **O1 — how to judge that the sent token had expired.** ✅ **Ruled: derive the
  expiry from `expiresIn` at receipt** (§4.5). Chosen over a 30 s tolerance on the
  JWT's `exp`, and over a GET-only backstop, because it removes the failure class
  instead of bounding it: both ends of the comparison come from the same clock.
  `jwtExp.ts` survives as the fallback for a response without `expiresIn`.
- **O2 — `dsr.ts`'s error shape.** ✅ **Ruled: unify now** (§4.6). Verified free —
  `AccountDsrPage` reads only `isError`.
- **O3 — where the helper lives.** ✅ **Ruled: its own `src/api/authedFetch.ts`.**
  The original argument was to keep the live path visibly apart from the dormant
  client in `client.ts`; round 5 deleted that client outright (§4.8), so what is
  left is the plainer reason — the file is named for what it does, and
  `client.ts` goes back to being what it actually is, the API-base resolver.
  Follow-up #2 brings a typed client back **delegating to** this helper's policy.
- **O6 (new — needs a ruling).** Round 12 withdrew N4 for the refresh path (§4.9),
  so §3.D's `access_token_expired` on `RequireAuth` is now *available*. It would
  turn §4.3 branch 2 from an inference into a fact, recover the in-flight expiry
  §4.4 gives up, and close `frontend-admin`'s wider copy of the replay hazard
  (§7). But it is a different code path — every protected route, not one endpoint
  — so it is not in scope merely because the neighbouring file is open. **Pull it
  into this work, or keep it as follow-up #1?**
- **O5 — repairing the session marker.** ✅ **Ruled: keep the repair** (§4.1e). A
  successful refresh has proved a cookie exists, so the marker is factually true;
  the accepted cost is that a sibling tab's sign-out whose `POST /logout` failed
  can be outlived across a reload. A `storage`-event listener would close that,
  and was declined as new behaviour inside a bug-fix PR.
- **O4 — macrotask vs synchronous clear of `inflightRefresh`.** ✅ **Answered by
  round 2: neither timing is a fix.** The macrotask only widens the coalescing
  window by one turn of the event loop, while the 401 this must survive can
  arrive seconds later; §4.3's sent-token comparison is what makes the late case
  correct, for *any* delay. **Keep the synchronous clear**, and document in
  `tokenStore.ts` that the window is deliberately not load-bearing, so nobody
  later "fixes" it by copying the macrotask and assumes the race is thereby closed.
