# Client-tier 401 recovery — design

_Status: **RULED — v22.** Batch 3 amendment: new §4.11 (proactive rotation for the client SPA), §4.9 takes the three refresh-path validation sites (thirteen in all), §4.10 answers `ErrJWTKeysNotLoaded` with a 503, §7 gives the console refresh-without-replay, and §8's follow-ups 2, 4, 6, 7, 8, 13, 14, 15, 16, 17 and 18 are ruled in for it._
_Issue: [#325](https://github.com/orkestra-cc/orkestra/issues/325)_
_Related: [ADR-0020](../../adr/0020-bearer-only-require-auth.md), [ADR-0017](../../adr/0017-session-lifetime-and-token-retention.md), [ADR-0003](../../adr/0003-three-audience-host-split.md)_

## 0. Revision log

| Rev | Date | Change |
| --- | ---- | ------ |
| v22 | 2026-09-02 | **Batch-3 amendment — the contracts the batch implements, and the four N3 sentences that outlived N3.** (i) **New §4.11: proactive rotation for the client SPA** (#2, ADR-0020 D3 parity). `PROACTIVE_REFRESH_SKEW_MS = 30_000` is exported from `authedFetch.ts`; `authedFetch` snapshots the store, rotates once through the marker-gated `refreshAccessToken` when the bearer it holds expires inside that window, **re-snapshots**, and sends whatever the store then holds. The outcome is deliberately not inspected — each of the three is already handled where it is decided — and the skew **never** enters §4.3 branch 2's margin-free comparison, which is stated as an invariant and pinned by reading the module's own source. The existing 401 suite migrates with it: a seed inside the window now rotates before the request, so those cases answer their proactive attempt 503 and count the two kinds of rotation apart. (ii) **§7 gains the console's refresh-without-replay** (#14). On a **codeless** 401 that went out with a live bearer the console runs `performRefresh` **once** and hands the caller the ORIGINAL 401 unchanged — no replay, so **G4** holds — collapsing a window of up to `TTL − 30 s` to a single request. The two `baseApi.replayGuard.test.ts` assertions that flip are named, and so are the two beside them that must not. (iii) **§4.9 takes the three `ErrJWTKeysNotLoaded` validation sites** (#15, first half): the `ValidateRefreshToken` call that opens `RefreshTokensWithRiskAssessment`, `PeekRefreshToken` and `MintAccessTokenFromRefresh` splits the sentinel off to 503 and leaves every other validation error exactly as it is. Ten sites become **thirteen**, the sentinel's own doc comment moves with them, and the test hook is a new `breakVerifyingKey` twin of `breakSigningKey`. (iv) **§4.10 gains the same sentinel's `RequireAuth` half** (#15, second half): a **503** carrying `token_verification_unavailable`, one emitter, modelled on `sendPolicyUnavailable` — because a boot with no verifying key is the server saying it cannot authenticate anyone, not that this session is over. (v) **The four N3 sentences are corrected.** §2's non-goal, N4's "still out" clause, §3.D's "what it buys" and §4.10's closing paragraph all still said N3 stands, and three of them still prescribed the one-line `code === "access_token_expired"` gate that §7 and §8 #5 record as refuted. N3 was discharged in batch 2; the sentences say so, and N4's boundary is restated for the batch-3 backend work. (vi) **§8**: #2, #4, #6, #7, #8, #13, #14, #15 and #16 are ruled in for batch 3, #3 is **resolved as #4**, and two entries are new — **#17**, the service-account gate that answers a store failure as a 404, and **#18**, the cleanups. (vii) **Three paragraphs that promised `openapi-fetch` a future are corrected** with it — N2's pointer, §4.8's "the dependency stays too" and O3's closing line all assumed follow-up #3, which #4 now supersedes — and §8's heading stops claiming its entries are "named, not started", which after this revision none of the eighteen is. |
| v21 | 2026-09-02 | **Batch-2 final fix wave — docs truth, no design change.** #1's closing clause still said the console fix "is still #5 below, and N3 still stands" while #5 and §7 both record it as done: corrected to "that landed as #5 in batch 2". #10's "the operator console is unaffected" is qualified with "only at the shipped entry point (see #13)", which is what #13 actually says. Three citations re-derived against HEAD: `REFRESH_FETCH_TIMEOUT_MS` is `baseApi.ts:98`; the `tokenExpiry` derivation is the **write** site (`authSlice.ts:179-181`'s `setAccessToken`), not `baseApi.ts:226`, which is the comparison that reads it; `VITE_API_URL` is `docker-compose.dev.yml:205`. §8 gains **14** (the F6 residual: a live-bearer codeless 401 is un-recovered for up to `TTL − 30 s`; mitigation is refresh-**without**-replay), **15** (`ErrJWTKeysNotLoaded` answered as a codeless 401 on all three refresh endpoints — §4.9's class, boot-time rather than a blip) and **16** (existing dev checkouts need three `.env` keys migrated; the note lands in `docker/CLAUDE.md` with this wave, the `env-validate.sh` hostname-equality guard does not). |
| v20 | 2026-09-02 | **Batch 2 amendment — the residual §4.9 named but never classified, plus three corrections the shipped code exposed.** (i) **`MintAccessTokenFromRefresh` joins §4.9.** The read-only mint that `GET /v1/auth/session` performs *after* the picker carries three generic wraps of its own — its `GetByTokenAny` (`auth_service.go:1673`), its `GetUserByID` (`:1690`) and `GenerateAccessTokenForSessionWithAMR` (`:1726`) — so a store failure opening between the picker's read and the mint's own answers Peek-OK → Mint-fail → codeless 401 on the console's boot path: v18's finding, one call later. The site table gains three rows and the count goes from seven to **ten**; the user lookup takes the same not-found-first split as the rotation's, because a blanket 503 there would strand a deleted account in the permanent loop R2 describes. **No handler change is needed** — `GetSessionHTTP` already hands the mint's error to `writeRefreshErr`, whose `ErrRefreshLookupUnavailable` branch answers 503 `refresh_lookup_unavailable`; `refreshFailureOutcome` already has the `lookup_unavailable` arm; and the cookie-clear allowlist already excludes the sentinel. (ii) **§6 claimed "no existing assertion is edited"** — two were, and they are one rule: a **2xx without a token** answers `unavailable` with the marker kept, where D15 made it `signed-out`. The third delayed-401 bullet asserted that a sign-out landing mid-flight refreshes nothing, which contradicts §4.3 4a's split on the **sent** bearer, and is replaced by the case that pins that split; and "unknown expiry → expired" survived from before round 11 in §4.5's list while §4.3 treats unknown as **live**. (iii) **§7's `frontend-admin` paragraph named the wrong endpoint and the wrong gate.** The provable lockout double-count is on `/me/password-confirm` (`recordFailed`, `password_auth_service.go:1300`), not `change-password`; and a strict `code === "access_token_expired"` gate would switch the console's reactive path off in almost every real case, because `prepareHeaders` withholds a locally expired bearer and the resulting 401 is codeless — so follow-up 5's gate is §4.3's **disjunction**. Also recorded: §4.3 4b is unreachable by construction and kept for symmetry; §4.1a's helper is `RefreshOutcome`-typed because a rejected lock *acquisition* is `unavailable`; §4.5 gains the clock-**ahead** residual on the `jwtExp` fallback path. §8 gains follow-ups **9-12** — 9, 10 and 11 are ruled in for batch 2 and land in the waves this amendment authorises; 12 is docs-only and lands with it. |
| v19 | 2026-09-01 | **Plan review round 1, finding #2 — high, accepted and verified.** Two more infrastructure reads sit inside the rotation-race classification, and both fail *destructively*: `benignRotationRetry` turns a `FamilyRevoked` error into `false` (`auth_service.go:1718-1723`, with a comment saying it "keeps the pre-existing replay behaviour"), and the post-CAS re-read discards its own error (`:1541`). Both callers then run `handleRefreshReplay` → `RevokeFamily`. So a Mongo blip during a **legitimate multi-tab race** revokes the family the winner has just renewed — every tab signed out, which is precisely the outcome `ErrRefreshRotationRaced` exists to prevent, and strictly worse than the 401s of finding #1 because it is persisted. The plan's `RotateCASLoss_IsNotUnavailable` test consecrated a lost CAS as "never an outage" regardless of whether the family state was readable, contradicting G2 and, during a race, G6/G7. **Fixed in §4.9:** `benignRotationRetry` returns `(benign, err)`; a failed family read or a failed re-read answers 503 **without** calling `handleRefreshReplay`; replay fires only on a family state that was actually read. Fail-closed denies the *current request* — it does not convert an unavailability into a presumed revocation. |
| v18 | 2026-09-01 | **Plan review round 1, finding #1 — blocking, accepted and verified.** §4.9 enumerated four infrastructure sites inside `RefreshTokensWithRiskAssessment`, but the browser's `/refresh-cookie` never reaches that function under an outage: `RefreshTokensHTTP` first classifies every cookie candidate through `PeekRefreshToken` (`auth_service.go:1578`), whose repository error is wrapped generically; `pickRefreshCandidate` discards **any** error as an invalid candidate (`auth_handler.go:1015-1018`); with no candidate left the handler synthesises `ErrInvalidRefreshToken` (`auth_handler.go:1459-1461`); `writeRefreshErr` answers 401; and §4.1's allowlist makes that 401 the one status that signs out. So defect C's headline scenario — Mongo unreachable during a cookie refresh — survived v17 intact, and §6's tests could not see it because they drove the service and `writeRefreshErr` directly, never `RefreshTokensHTTP`. **Fixed in §4.9:** `PeekRefreshToken` classifies the lookup with the same sentinel; the picker reports an infrastructure failure instead of swallowing it; all three cookie-iteration handlers answer 503 when no candidate is valid *and* a lookup failed — and **never fire the replay fallback** on that input, since an unclassifiable candidate may be the valid successor. §1 gains the fifth site, §6 gains picker unit tests and HTTP-level tests through the real handlers. |
| v17 | 2026-09-01 | **Rulings R1 and R2 — the last open question closes and §4.9 gains a site it could not have classified.** **R1: O6 is ruled IN.** `access_token_expired` on `RequireAuth` ships with this work (new §4.10), so §4.3 branch 2 stops being a pure inference: it becomes an **OR of two independent proofs** — the server's own code, or v16's "already expired at send" retained as the fallback for a backend that has not shipped it. Either alone proves the handler never ran, so §4.4's guarantee is untouched and the in-flight expiry §4.4 gave up is recovered whenever the code is present. §4.5's duration machinery and the §4.6 migration survive intact — they *are* the second proof. **R2: §4.9's user-lookup site was unimplementable as written.** It said "a genuine `nil` user stays `ErrInvalidRefreshToken` → 401"; verified against the tree, that `nil` never occurs — `userService.GetUserByID` returns the sentinel `ErrUserNotFound` for a deleted account (`user_service.go:592`), so a blanket 503 would strand an erased account in a **permanent 503 loop**: token and marker kept, `isAuthenticated` true, every request 401 — defect A's broken state, forever. `internal/core/auth/services` cannot import the user module, and no shared sentinel existed, so one is added to the SDK. |
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

**And on the way in — the site that actually answers the browser.** Those four
are inside the rotation. The cookie endpoint does not reach the rotation until it
has *classified* every cookie the browser sent, and that classification has its own
lookup with its own generic wrap, feeding a picker that cannot tell "invalid" from
"could not look":

| Site | What happens | Where |
| ---- | ------------ | ----- |
| `PeekRefreshToken` → `GetByTokenAny` | repository error wrapped as `failed to look up refresh token` | `auth_service.go:1578-1581` |
| `pickRefreshCandidate` | `if err != nil \|\| doc == nil { continue }` — **any** error is "not a candidate" | `auth_handler.go:1015-1018` |
| `RefreshTokensHTTP` | no candidate left → `lastErr = ErrInvalidRefreshToken` | `auth_handler.go:1459-1461` |
| `writeRefreshErr` | → **401**, no code | |

The same shape sits in `RefreshTokensWithHeaderHTTP` (`/refresh`) and
`GetSessionHTTP` (`/session`, the operator console's boot path). So under a Mongo
outage the browser's `/refresh-cookie` **never calls
`RefreshTokensWithRiskAssessment` at all** — fixing the four sites inside it fixes
the JSON-body path and leaves the cookie path, which is the only one either SPA
uses, exactly where it was. This was missed through v17 and caught in plan review
(v18): a test that drives the service directly cannot see it, because the bug is in
the handler *in front of* the service.

**And during a race — where the failure is not a status code but a revocation.**
The rotation-race classification (§5.4's grace window) is itself two more store
reads, and both fail in the worst possible direction:

| Site | What happens | Where |
| ---- | ------------ | ----- |
| `benignRotationRetry` → `FamilyRevoked` | error → `return false` ("treating rotation as replay", by its own log line) | `auth_service.go:1718-1723` |
| the post-CAS re-read → `GetByTokenAny` | `rerr == nil && …` — an error falls through | `auth_service.go:1541` |
| both callers | → `handleRefreshReplay` → **`RevokeFamily`** → `ErrRefreshTokenReplay` → 401 | `:1456-1460`, `:1542-1546` |

The input this misclassifies is the **benign** one: two tabs of one login rotate
together, the loser presents the superseded cookie inside the grace window, and
the family is healthy — the case the 409 was built for. If the family read fails
at that instant, the loser is answered "replay", the family is revoked, and the
winner's freshly minted successor dies with it. Every tab signs out for a store
hiccup. Unlike the five sites above this is not a transient 401 the next request
can recover from: it is persisted.

**And after the picker — the read-only mint (v20).** `GET /v1/auth/session`
classifies its cookies through that same picker and then mints *without* rotating,
and the mint has three generic wraps of its own:

| Site | Wrapped as | What it really is |
| ---- | ---------- | ----------------- |
| `MintAccessTokenFromRefresh` → `GetByTokenAny` | `failed to look up refresh token` | the store is unreachable |
| its user lookup | **`user not found`** | same — and, as above, it *reads* as terminal |
| `GenerateAccessTokenForSessionWithAMR` | `failed to mint access token` | signing/key failure |

Peek and Mint issue **separate** reads, so a blip that opens between them answers
Peek-OK → Mint-fail → a codeless 401 on the operator console's boot path. It is
v18's finding one call later, and it survived v18 for the same reason v18's own
site survived v17: the fix was aimed at the function the tests could reach.

A Mongo blip during a refresh therefore reaches the SPA as the same codeless 401 a
genuinely invalid refresh token produces — through ten sites, not four, and two
of them revoke the session rather than merely rejecting the request. **No
client-side rule can separate them**, which is why this defect is what removes N4
rather than something the helper can be made clever about.

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
  and arm a trap for whoever first imports `api`. Re-adding the client wired to
  this spec's policy was follow-up #3; batch 3 **resolves #3 as #4** and takes the
  dependency and the generated stub with it, so a typed client — if one is ever
  wanted — arrives with its own pinned dependency and a middleware that delegates
  to §4.3 rather than finding materials lying around.
- **N3 (discharged in batch 2) — fixing `frontend-admin`.** §7 records a real
  defect found there, and this spec's own PR did not touch it. It shipped
  separately, as §8 #5's own commits with its own tests — and **not** as the
  one-line gate earlier revisions promised, but as §4.3's two-proof disjunction
  (§7 says why). What is left of it is follow-up **#14**, ruled in for batch 3.
- **N4 (withdrawn in round 12, widened in round 15) — backend work is in scope,
  and it is exactly two changes.** Earlier revisions forbade touching the backend.
  Defect C shows that G2 and G3 are then **unreachable**: four infrastructure
  failures on the refresh path are answered as a codeless 401, and no client-side
  rule can tell them from a dead refresh token. N4 is therefore a boundary, not a
  prohibition, and **R1** moved the boundary once more. In scope:

  1. **§4.9** — error *classification* on the refresh path: infrastructure
     failures answer 503 instead of 401. No route moves, no field is removed,
     nothing that already works changes shape.
  2. **§4.10** (new, R1/O6) — `RequireAuth`'s expired-bearer 401 carries a
     distinct top-level `code`. It touches every protected route on both tiers,
     which is why it needed a ruling rather than an assumption; it is a **split
     of a branch that already exists** (`auth.go:218` already compares against
     `services.ErrTokenExpired`), changes nothing about what is *accepted*, and
     leaves `RequireAuth` bearer-only.

  Still out **of the batch this paragraph was written for**: anything else. The
  boundary has since moved once more and only within the same class — batch 3
  applies §4.9's classification to the three refresh-path validation sites and
  §4.10's to `RequireAuth`, both for `ErrJWTKeysNotLoaded` (§8 #15), and to one
  sibling outside the refresh path (§8 #17). **N3 no longer stands**: §4.10 was
  expected to make `frontend-admin`'s copy of the replay hazard (§7) a one-line
  fix and it did not — the console withholds a locally expired bearer, so the 401
  that follows carries no code at all. What shipped there in batch 2 is §4.3's
  disjunction.

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

**Round 15 adds one code that IS read positively, and it does not weaken any of
the above.** §4.10 gives `RequireAuth`'s expired-bearer 401 a distinct
`access_token_expired`. It is not in `TERMINAL_CODES` and never will be — it means
the opposite, "this is recoverable" — so branch 1's closed set is untouched and
`if (body.code) → clear` stays exactly as forbidden as it was. Two disjoint
membership tests are read off the same field: one closed set that ends a session,
one single value that permits a recovery, and every other code — the four
non-terminal 401s enumerated above among them — matches neither and falls through
to branch 2.

Everything else — the expired-vs-wrong-credentials question for a backend that has
**not** shipped §4.10 — is still answered client-side from the token we sent (§4.3
branch 2's second proof), because that server does not answer it.

### D — Have the backend say `access_token_expired` (adopted, round 15 — §4.10)

The client was inferring something the server knows for certain. `RequireAuth`'s
expired-bearer 401 now carries a distinct top-level `code`, so §4.3 branch 2 stops
being purely an inference: it refreshes when that code is present, and the replay
hazard disappears by construction rather than by an argument about when the handler
runs. It composes with the closed set already read in branch 1 — same field, two
disjoint tests, one clone.

**Why it was deferred through fourteen rounds, and what changed.** The objection
was never that backend work is forbidden — round 12 withdrew N4 for the refresh
path. It was **blast radius**: this touches `RequireAuth`, which answers every
protected route on both tiers, days after ADR-0020 reworked exactly that code,
whereas §4.9 changes the classification of four error returns on one endpoint.
Reading the code settled it (**O6**, ruled 2026-09-01): the branch **already
exists**. `validateTokenEnhanced` returns the unwrapped sentinel
`services.ErrTokenExpired` (`jwt_service.go:544-546`) and `RequireAuth` already
compares against it (`auth.go:218`) — it merely merges that case with
`ErrInvalidToken` into one codeless 401 on the way out. Expiry is also checked by
`jwt.Parse` **before** the audience, type and issuer checks, so an expired token
never reaches those branches and cannot be mislabelled by this one. What ships is a
split of an existing branch and a second emitter modelled line-for-line on
`sendSessionRevoked`. Nothing about what is *accepted* changes; `RequireAuth` stays
bearer-only.

**What it buys, concretely.** §4.4's one acknowledged cost — a token that was live
at `sentAt` and expired *in flight* is not auto-recovered — is recovered whenever
the server sends the code. It was also expected to make `frontend-admin`'s wider
copy of the same hazard (§7) a one-line fix. **It did not, and this section
applies no fix there**: `prepareHeaders` withholds a locally expired bearer, so the
console's 401 usually arrives codeless and a strict gate on the code would have
switched its recovery off in almost every real case. §8 #5 shipped §4.3's
disjunction instead, in batch 2.

**It does not replace §4.3 branch 2's client-side reckoning.** See §4.3: the two
are OR-ed, because a client can reach a backend that has not shipped this — a
rolling deploy, a fork on an older base, a stale API container — and the recovery
must not be dead against one.

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

**The shipped helper is `RefreshOutcome`-typed, not generic `<T>`.** A lock the
manager refuses to *grant* (an `InvalidStateError` on a document that is not fully
active, an implementation that throws) says nothing about the session, so by §4.1's
allowlist it is `unavailable` — and it must not propagate either, since
`AuthProvider`'s mount call is `void refreshAccessToken(…)` and a rejection there
lands as an unhandled one. That means the helper needs a fallback **value**, and
the value is outcome-specific: a `<T>` signature has nothing it could return. The
`catch` is scoped to the acquisition alone (a `granted` flag), because a rejection
raised *after* the callback started is a programming error and is rethrown.

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
`frontend-admin` settled on (`baseApi.ts:98`). A `/refresh-cookie` that accepts the
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
// CLONE, so whatever we hand back is still unread (§5.11). The code is read
// ONCE and two disjoint tests are applied to it (§3.C).
const code = await read401Code(res.clone());
```

with the one place that knows both sets:

```ts
// The CLOSED set that ends a session. A MEMBERSHIP test, never a presence
// test (§3.C): the middleware emits at least seven top-level codes and four
// of them ride on 401s that are emphatically not a dead session.
const TERMINAL_CODES = new Set(["session_revoked", "session_max_age_reached"]);

// The single value that PERMITS a recovery — §4.10. It means the opposite of
// terminal, so it is never a member of the set above.
const CODE_ACCESS_TOKEN_EXPIRED = "access_token_expired";

// Reads a CLONE, never the response a caller will get. A body that is absent,
// not JSON, or carries no top-level `code` simply yields null — the ordinary
// case, not an error condition (§3.C: the generic paths emit no top-level
// `code`, keeping their internal one in `errors[0].value`, which we
// deliberately do not read).
async function read401Code(clone: Response): Promise<string | null> {
  const body = (await clone.json().catch(() => ({}))) as { code?: unknown };
  return typeof body.code === "string" ? body.code : null;
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
without adding the header to that list first. §4.10 sets the same header for
`access_token_expired`, and for the same reason the client cannot read that one
either — the backend work in scope is §4.9 and §4.10, and a CORS `ExposedHeaders`
edit is neither.

| # | Condition | Action |
| - | --------- | ------ |
| 1 | `code !== null` **and** `TERMINAL_CODES.has(code)` | Clear token + marker. **No refresh, no retry** — a token minted from the same cookie carries the same dead `sid`. Return the original response. |
| 2 | **NEITHER PROOF HOLDS** — i.e. **NOT** (`code === CODE_ACCESS_TOKEN_EXPIRED`) **and NOT** (`sent.expiresAt !== null` **and** `sent.expiresAt <= sentAt`). An unknown expiry with no code lands here. | Return the original response **unchanged**. The request may have reached the handler, so a retry could re-consume whatever it consumed: no refresh, no replay (**G4**, §4.4). |
| 3 | Store now holds a **different, non-null** token than `sent.token` | A sibling already rotated. Retry **once** with the store's token. **No refresh** (**G8**). |
| 4a | Otherwise, **and a bearer was sent** (`sent.token` non-null) | `refreshAfterUnauthorized(apiBaseURL)` (§4.1e) — **not** marker-gated. `ok`: retry **once** with the new bearer. `signed-out`: `performRefresh` has cleared token **and** marker (**G3**); return the original. `unavailable`: return the original, token and marker untouched (**G2**, **G7**). |
| 4b | Otherwise, **and no bearer was sent** (`sent.token` null) | `refreshAccessToken(apiBaseURL)` — marker-gated, as today. A true anonymous visitor short-circuits with no request, and there is nothing to clear because there is no token. A marker-holding visitor whose request raced `AuthProvider`'s mount refresh joins the coalesced attempt. **Unreachable by construction under this order** — see below. |

**4b is unreachable, and the row stays anyway.** With no bearer sent, `expiresAt`
is `null` (the store writes the pair together, and `setAccessToken(null)` nulls
both), so proof (b) cannot hold; and a missing-bearer 401 from `RequireAuth` is
**codeless** (§4.10 emits the code only for a well-formed, correctly signed,
expired bearer), so proof (a) cannot hold either — branch 2 has therefore already
returned. The one way in is a caller that supplies its **own** `Authorization`
header while the store is empty, which `doFetch` permits: the in-memory bearer is
set only `if (token)`. The row is kept as the honest expression of the split rather
than as live code — the rule `authedFetch.ts` records as **P23**: do not delete it
as dead, and do not reorder the branches to make it reachable, because branch 2
sitting in front of every recovery *is* the replay guard.

**Two independent proofs, OR-ed — and that is not a weakening.** Branch 2 permits
recovery only on evidence that the request never reached its handler, because a
retry re-sends whatever it consumed. Round 15 gives it a second source of that
evidence:

1. **`code === "access_token_expired"`** — the server states it rejected the
   bearer *before dispatch* (§4.10). This is the stronger proof: it is the
   server's own account of its own control flow, and it covers a token that was
   live at `sentAt` and expired **in flight**, which no client-side reckoning can.
2. **`sent.expiresAt !== null && sent.expiresAt <= sentAt`** — v16's rule,
   retained as the **fallback**. `RequireAuth` accepts a token until the instant
   it expires, with no grace of its own, so "already expired when it left" is the
   weakest client-side condition that still proves the handler never ran.

Each is sufficient on its own, so their disjunction is too: §4.4's guarantee is
unchanged, and the guard remains a **negative** — no proof, pass the 401 through.
(2) exists because a client can reach a backend that has not shipped (1) — a
rolling deploy, a fork on an older base, a stale API container — and the recovery
must not be dead against one. It also means §4.5's duration machinery and the
§4.6 migration are **not** made redundant by §4.10: they *are* proof (2), and
follow-up #2 needs them regardless.

**And still no margin.** v12 asked whether the token had more than `SKEW` of life
left, which quietly widened "safe to retry" to include tokens the *server still
accepts* — a 30-second window in which a `change-password` rejection was replayed
and double-counted. Proof (2) is a strict "already expired at `sentAt`", so `SKEW`
plays no part in the 401 path at all; the constant belongs to follow-up **#2** —
**§4.11**, which introduces it for a decision taken *before* the request goes out,
and states as an invariant that it never comes back here.

Branch 2 is the replay guard, and it sits **ahead of every recovery branch**
(branch 1 precedes it only because a dead `sid` makes recovery pointless).
Because branches 3 and 4 sit behind it, they only ever see a request that one of
the two proofs has cleared — so the "a sibling rotated, reuse its token" retry is
covered by the same argument, not by a separate one.
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
401 is inspected too** — same `read401Code(retried.clone())` — but only for the
terminal set. `access_token_expired` on a retry is deliberately **not** acted on:
a second refresh is forbidden regardless (see below), so reading it would change
nothing and would invite exactly the second rotation this design exists to avoid.

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
"the token was already expired when the request left". Nothing weaker will do —
*client-side*. §4.10 gives the server a way to state the same fact directly, which
is a **stronger** proof of the same invariant, not a weaker one: the middleware
rejects before dispatch, so a 401 carrying `access_token_expired` provably never
reached a handler. The two are OR-ed in §4.3 branch 2 for that reason.

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

**What this costs, stated plainly — and what round 15 refunds.** A token that was
live at `sentAt` and expired *in flight* cannot be recovered by the client-side
proof: the 401 is surfaced and the caller sees one failed request. It self-heals
immediately — by the user's next action that token is expired at send, so the very
next request refreshes and succeeds. The window is one request latency out of a
whole TTL, and the alternative is guessing on the one question where a wrong guess
locks an account.

**Against a backend that has shipped §4.10 this cost is gone**: that 401 carries
`access_token_expired`, branch 2's first proof holds, and the request is recovered.
The cost above is therefore the behaviour against an *older* backend — which is
precisely why the client-side proof is kept rather than replaced.

The same reasoning covers an **unknown** expiry (§4.5's fallback exhausted) *when
the server said nothing*: unknown cannot prove the handler did not run, so it
counts as live and passes through. This reverses the direction §4.5 gave before
round 11 — "treat an unreadable expiry as expired" was chosen when the failure mode
was a wasted refresh; under this rule the failure mode would be a replay, so it
flips. Note the two combine cleanly: an unknown expiry **plus**
`access_token_expired` still recovers, because proof (1) does not consult the
expiry at all.

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
bounded. `frontend-admin` derives its `tokenExpiry` the same way — in the
`setAccessToken` reducer, `Date.now() + expiresIn * 1000` at the moment the token
is installed (`frontend-admin/src/store/slices/authSlice.ts:179-181`), dispatched
from `baseApi.ts` after every refresh — so the two SPAs agree on the technique.
`baseApi.ts:226` is the *comparison*, and it reads that same snapshot.

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

**Clock ahead — the residual the fallback keeps.** The immunity above belongs to
the duration path. On the `jwtExp` fallback, `expiresAt` is the token's **absolute**
`exp` and branch 2 compares it against `sentAt`, a reading of the client's own wall
clock — so a client clock running **ahead** by X calls the token expired X seconds
early, and inside that window a 401 that the handler *did* answer is refreshed and
replayed: a replay window of X per TTL, not a permanent one. A clock **behind** by
X is the harmless direction (§5.9): recovery is late, never wrong. The window is
reachable only against a backend that reports no `expiresIn` — which, since both
ship in this work, is also a backend without §4.10, so proof (1) cannot cover it
either. Accepted as a residual rather than absorbed with a margin: a margin on this
comparison is precisely the round-11 hole (§4.4), and the thing that actually
removes the window is the fix already specified — report the duration.

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
without `expiresIn` as a failed refresh (`baseApi.ts:144`). Turning a valid
rotation into a sign-out over a missing optional field is the wrong trade here.

**There is no `SKEW` in this comparison.** Earlier revisions carried a 30 s margin;
round 11 showed the margin *was* the replay hole (§4.4), and with the expiry
derived from a duration there is nothing left for it to absorb. The 401 path
compares `expiresAt <= sentAt` exactly. `PROACTIVE_REFRESH_SKEW_MS` belongs to
follow-up **#2** — **§4.11**, which asks a different question about a request that
has not gone out yet, and which holds ADR-0020 D3's invariant that the constant
stays **strictly below `MinAccessTokenTTL` (60 s)**. Do not reintroduce it here: a
margin on this comparison is, precisely, the bug round 11 removed, and §4.11 pins
its absence rather than trusting this paragraph.

**§4.10 does not retire any of this section.** It is tempting to read "the server
now tells us the token expired" as "the client no longer needs to know when its
token expires", and it is wrong twice over: this reckoning is §4.3 branch 2's
second proof, which is what keeps the recovery alive against a backend without
§4.10, and follow-up #2 cannot exist without a trustworthy remaining-lifetime
figure. The §4.6 lifetime migration is required either way.

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
§4.9 and §4.10 make this a backend change too.

**`frontend-client/CLAUDE.md`** currently documents the gap (`82f25252`): its
"Refresh choreography" section states that an expired token does *not* refresh, and
names this fix. Rewrite it to the shipped behaviour — §4.1's outcome table, §4.3's
branch table, and why **branch 2** is the replay guard. "How auth works" item 1 and
the `credentials` convention bullet also name the helper.

**`backend/internal/core/auth/CLAUDE.md`** — two additions. §4.9 adds
`refresh_lookup_unavailable` and moves ten error returns from 401 to 503. That is
documented surface: the module contract must say which failures on the refresh
path are authentication answers and which are outages. §4.10 adds
`access_token_expired` to the enumeration of codes
`shared/middleware.AuthMiddleware` emits — the file already carries that list, and
this is the only non-terminal one on the 401 path that a client should act on by
refreshing.

**`docs/site/architecture/authentication-flow.mdx`** — three edits. §4.9's status
change belongs beside the existing rotation description (line 149's grace/409
paragraph). §4.10's new code belongs in the "Access-token expiry and refresh"
section (line ~224), immediately after the "bearer-only" sentence, stated as what
it is: the signal that tells a client the request never reached its handler and is
therefore safe to retry. And **line 226 carries the same false claim as ADR-0020
D2**: "A
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
*with* the implementation rather than ahead of it. §4.10 is a refinement of
ADR-0020's rejection path, not a reversal of any of its decisions — `RequireAuth`
stays bearer-only, never reads the cookie and never rotates — so it is documented
on the canonical reference page rather than by amending the record.

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

`openapi.gen.ts` **stayed** when this section was written: it is the committed
codegen target the README documents, and a future typed client would need it. The
`openapi-fetch` dependency stayed too, for the same reason — with one honest
consequence: Dependabot keeps proposing bumps for a package nothing imports, and
per this repo's own rule those are vacuous and get closed rather than merged.

**Batch 3 reverses that** (§8 #4, which resolves #3 with it). Vacuous by
construction is the condition under which a dependency is *dropped*, not the
condition under which it is kept — and keeping the generated types and the runtime
package keeps the **materials** for exactly the second 401 algorithm this section
deleted, with only prose telling the next person to delegate. The dependency, the
`openapi-typescript` devDependency, the `codegen` script and the stub go together,
because dropping the runtime package alone leaves a generator with no consumer.
That it propagates down the fork chain on its own schedule is why it is its own
wave and why #4 makes the both-ways dependency-set diff at the next sync a
condition of the change rather than an afterthought.

### 4.9 Backend: classify infrastructure failures on the refresh path

The first of the two backend changes in scope (§4.10 is the other), and it is a
classification fix, not a new feature: **an infrastructure failure must not be
answered as an authentication failure.** ADR-0017 already decided this for session
enforcement and gave it a 503; the refresh path has thirteen sibling sites that
never got the same treatment (§1 defect C).

Each generic wrap becomes a sentinel that `writeRefreshErr` answers as **503** —
four inside `RefreshTokensWithRiskAssessment` (`services/auth_service.go`), one in
the `PeekRefreshToken` classification that runs in front of it, two in the
rotation-race classifier, three in `MintAccessTokenFromRefresh`, the read-only
mint `GET /v1/auth/session` performs after the picker, and three at the
`ValidateRefreshToken` call that opens each of those three functions (batch 3,
§8 #15):

| Site | Today | After |
| ---- | ----- | ----- |
| `refreshTokenRepo.GetByTokenAny` | `fmt.Errorf("failed to validate refresh token: %w")` → 401 | `ErrRefreshLookupUnavailable` → 503 |
| the user lookup | `fmt.Errorf("user not found: %w")` → 401 | **split** — `iface.ErrUserNotFound` → `ErrInvalidRefreshToken` → 401; anything else → same sentinel → 503 |
| `mintTokenPair` | `fmt.Errorf("failed to mint token pair: %w")` → 401 | same sentinel → 503 |
| `refreshTokenRepo.RotateWithFamily` | `fmt.Errorf("failed to rotate refresh token: %w")` → 401 | same sentinel → 503 |
| **`PeekRefreshToken` → `GetByTokenAny`** (v18) | `fmt.Errorf("failed to look up refresh token: %w")` → swallowed by the picker → 401 | same sentinel → **reported by the picker** → 503 |
| **`benignRotationRetry` → `FamilyRevoked`** (v19) | error → `false` → `handleRefreshReplay` → **family revoked** → 401 | same sentinel → 503, **replay not fired** |
| **post-CAS re-read → `GetByTokenAny`** (v19) | error ignored → `handleRefreshReplay` → **family revoked** → 401 | same sentinel → 503, **replay not fired** |
| **`MintAccessTokenFromRefresh` → `GetByTokenAny`** (v20) | `fmt.Errorf("failed to look up refresh token: %w")` → 401 | same sentinel → 503 |
| **`MintAccessTokenFromRefresh` → the user lookup** (v20) | `fmt.Errorf("user not found: %w")` → 401 for *both* a deleted account and an outage | **split**, exactly as the rotation's row: `iface.ErrUserNotFound` → `ErrInvalidRefreshToken` → 401; anything else → same sentinel → 503 |
| **`MintAccessTokenFromRefresh` → `GenerateAccessTokenForSessionWithAMR`** (v20) | `fmt.Errorf("failed to mint access token: %w")` → 401 | same sentinel → 503 |
| **`RefreshTokensWithRiskAssessment` → `ValidateRefreshToken`** (v22) | `fmt.Errorf("invalid refresh token: %w")` → 401 for **every** validation failure, a missing verifying key included | **split** — `ErrJWTKeysNotLoaded` → same sentinel → 503; every other validation error keeps today's wrap → 401 |
| **`PeekRefreshToken` → `ValidateRefreshToken`** (v22) | same wrap, then swallowed by the picker → 401 | same split, and the 503 half is reported by the picker exactly as its `GetByTokenAny` row is |
| **`MintAccessTokenFromRefresh` → `ValidateRefreshToken`** (v22) | same wrap → 401 on the session-bootstrap path | same split |

**The user-lookup site needs more than a re-wrap, and R2 is why.** A store error
there is reported with the words of a terminal condition, which is how an outage
acquires the appearance of a deleted account — that much was right. What was wrong
was the escape hatch: v16 said "a genuine `nil` user stays `ErrInvalidRefreshToken`
→ 401", and **that `nil` never occurs**. `userService.GetUserByID` returns the
sentinel `ErrUserNotFound` for a deleted account and wraps anything else
(`user_service.go:586-600`); it never answers `(nil, nil)`. Mapping every error
there to 503 would therefore strand a genuinely deleted or GDPR-erased account in
a **permanent 503 loop** — token and marker kept by §4.1's table, `isAuthenticated`
still `true`, every request 401ing — which is defect A's broken state made
permanent, and strictly worse than the bug this section fixes.

So the site must *classify*, and it cannot do so with what is in the tree:
`internal/core/auth/services` must not import `internal/core/user/services` (root
`CLAUDE.md`: "Never import cross-module service/repository packages"), and no
shared not-found sentinel exists. **One is added to the SDK:**

```go
// pkg/sdk/iface — beside the existing ErrPasswordLoginDisabled /
// ErrAuthPolicyUnavailable sentinels.
var ErrUserNotFound = errors.New("user not found")
```

and `user/services.ErrUserNotFound` becomes an **alias** of it. That is the whole
migration: every existing `return nil, ErrUserNotFound` site becomes
`errors.Is`-classifiable from outside the module, the message is byte-identical, and
every `err == ErrUserNotFound` comparison still holds because it is the same value.
The refresh path then reads:

```go
userModel, err := s.userService.GetUserByID(ctx, claims.UserUUID)
if err != nil {
    // Gone is terminal — 401, or the client holds a token for a deleted
    // account forever. Anything else is the store, and answering THAT 401 is
    // how an outage acquires the appearance of a deleted account.
    if errors.Is(err, iface.ErrUserNotFound) {
        return nil, ErrInvalidRefreshToken
    }
    return nil, fmt.Errorf("user lookup failed: %w", ErrRefreshLookupUnavailable)
}
```

The SDK addition propagates down the fork chain like any other: a fork with its own
`UserProvider` keeps compiling — but until its `GetUserByID` returns or wraps
`iface.ErrUserNotFound`, a **deleted account is classified as an outage**. Both the
rotation and the read-only mint then answer **503** where they should answer 401,
and the client keeps retrying a session that is never coming back. Returning the
sentinel is therefore an obligation on a fork's `UserProvider`, not an optional
upgrade, and it is stated as one in `pkg/sdk/CLAUDE.md`.

`writeRefreshErr` gains one branch beside the `ErrSessionEnforcementUnavailable`
one it already has, emitting 503 with a **distinct** code —
`refresh_lookup_unavailable` — rather than reusing
`session_enforcement_unavailable`. Both are 503 and both clients treat 503
identically, so the distinction costs nothing on the wire and buys the thing
ADR-0017 D4 argued for elsewhere: whoever reads the support ticket can tell which
subsystem failed.

**The picker in front of the rotation (v18).** The fifth row is the one that
answers the browser, and it needs more than a re-wrap because the failure is not
in the service but in the handler that *consumes* it. `RefreshTokensHTTP`,
`RefreshTokensWithHeaderHTTP` and `GetSessionHTTP` all run every cookie candidate
through `pickRefreshCandidate` before any mutating call — the PR-D D-9 fix that
keeps a stale parent-domain cookie from poisoning a valid family — and the picker
treats **every** `PeekRefreshToken` error as "not a candidate". That is right for
a malformed JWT and wrong for an unreachable store, and the two are
indistinguishable to it today. Three changes, one sentinel:

1. **`PeekRefreshToken`** classifies its `GetByTokenAny` error as
   `ErrRefreshLookupUnavailable`. Its JWT-validation error and its `nil`-doc
   `ErrInvalidRefreshToken` are untouched — those *are* "not a candidate". Its
   user lookup stays deliberately error-tolerant (the existing comment: "Peek
   stays a pure classification read"); the mutating path enforces that one.

2. **The picker reports what it could not classify.** It gains a third return:

   ```go
   func pickRefreshCandidate(ctx, peek, candidates) (chosen, fallbackRotated string, lookupErr error)
   ```

   A Peek error that `errors.Is` the sentinel is recorded and the loop continues
   — another candidate may still be valid, and a valid candidate is proof enough
   on its own (the rotation it leads to will 503 by itself if the store is really
   down). A Peek error that is *not* the sentinel is skipped exactly as today.
   At the end:
   - a valid `chosen` wins regardless of what else was seen;
   - otherwise, if any lookup failed, return `("", "", lookupErr)` — **and the
     rotated fallback is suppressed**. The fallback exists to fire genuine replay
     detection when the *only* thing the browser holds is a rotated token; a
     candidate that could not be classified may have been the valid successor,
     and revoking a family on incomplete information is the PR-D D-9 regression
     wearing a new face;
   - otherwise, today's `("", fallbackRotated, nil)`.

3. **The three handlers put the new arm between "chosen" and "fallback".**
   `chosen != ""` → rotate; `lookupErr != nil` → `lastErr = lookupErr`;
   `fallbackRotated != ""` → fire replay; else `ErrInvalidRefreshToken`.
   `writeRefreshErr` then answers the same 503 `refresh_lookup_unavailable` it
   answers for the four sites inside the rotation. `GetSessionHTTP` has no
   fallback arm and gains only the middle one — and that is the *only* handler
   work the session path needs, the mint rows below included: its `chosen != ""`
   arm already passes the mint's error to the same `writeRefreshErr`.

Two facts checked while writing this, both of which the plan pins with a test
rather than trusting: `clearRefreshCookieOnTerminalRefreshErr` is an
**allowlist** (`ErrSessionMaxAgeReached` and the degraded logout only), so the
sentinel never expires the cookie — a cleared cookie on an outage would be
unrecoverable, strictly worse than the 401 this replaces; and
`refreshFailureOutcome`, the log classifier, falls through to `"invalid_token"`
for anything it does not name, so it gains a `"lookup_unavailable"` arm or the
new 503 is *logged* as the very misreading it fixes.

The JSON-body path in `RefreshTokensHTTP` runs after the cookie path and can
overwrite `lastErr`; neither SPA sends a body, and under a real outage the body
path 503s through the four in-rotation sites anyway. Recorded, not changed.

**Infrastructure failure during a rotation race (v19).** The last two rows are
the ones where "fail closed" was implemented as "assume the worst and act on it",
which is not the same thing. The race classifier has three honest answers — the
family is healthy (409), the family is revoked or the window has passed (replay),
**or it could not tell** — and today the third is silently folded into the second,
which is the one that mutates. The fix makes the third answer explicit:

```go
// benignRotationRetry reports whether a rotated row presented inside the
// grace window belongs to a HEALTHY family. The error return is the third
// state: the family could not be read, so neither "retry" nor "replay" is
// justified. A caller must answer 503 on it and must NOT revoke — fail closed
// denies the current request; it does not invent a verdict and persist it.
func (s *authService) benignRotationRetry(ctx context.Context, doc *models.RefreshTokenDoc) (bool, error)
```

The two structural checks (`doc == nil`, not rotated, outside the grace window)
still answer `(false, nil)` — those are determinate "not benign" verdicts and keep
routing to replay. Only a `FamilyRevoked` error becomes `(false, err)`, wrapped as
`ErrRefreshLookupUnavailable`. Both callers gain the same arm ahead of replay:

- **step 3/4** (a row already marked rotated): `benign, err :=`; `err != nil` →
  return the sentinel; `benign` → `ErrRefreshRotationRaced`; else
  `handleRefreshReplay` + `ErrRefreshTokenReplay`, exactly as today;
- **the `ErrTokenAlreadyRotated` branch**: the re-read's own error is no longer
  discarded — `rerr != nil` → return the sentinel *before* the family check; then
  the same three-way split on `benignRotationRetry(current)`.

`handleRefreshReplay` is reached only when the family state was **actually read**
and said "not benign". Its own `RevokeFamily` error stays as it is — logged, with
the 401 still returned — because by then the verdict *was* a replay and denying
the request is right even if the revocation did not persist; that is a genuine
fail-closed, not a misclassification.

What this does and does not change, stated exactly: a CAS lost to a sibling whose
family **could be read** still answers 409 or replay by that state (G7 unchanged,
and the plan's test for it is narrowed to say so — "a lost CAS is not an outage"
was true only with that qualifier). A CAS lost, or a rotated row presented, while
the family **cannot be read** answers 503 and touches nothing, so the sibling that
won keeps its successor and both tabs recover on the next attempt (G6, G2). The
benign race is exactly the input this used to destroy.

**The read-only mint on the bootstrap path (v20).** The last three rows are the
residual v18 left one call short. `GetSessionHTTP` classifies every cookie through
the picker and then calls `MintAccessTokenFromRefresh`, which reads the row again,
loads the user and signs — three infrastructure sites, all wrapped generically, all
answered as a codeless 401. Peek and Mint are **separate reads**, so the reachable
input is a blip that opens between them: Peek-OK → Mint-fail → the console decides
its session is over on boot. The three wraps take the same treatment as their
rotation-path twins, and the user lookup takes the same not-found-first split for
the same R2 reason — a blanket 503 there is a permanent loop for an account that
really is gone.

Two things this does **not** touch. The absolute-cap return
(`sessionWithinAbsoluteCap`) stays **bare and unwrapped**: it already propagates
`ErrSessionMaxAgeReached` (401), `ErrSessionEnforcementUnavailable` (503) and the
degraded-revocation error verbatim, ADR-0017 owns those, and wrapping it in this
sentinel would put it on the wrong side of `writeRefreshErr`'s branch order. And
the handler: `GetSessionHTTP` hands `lastErr` to `writeRefreshErr` unmodified, so
the 503 `refresh_lookup_unavailable` and the `lookup_unavailable` log outcome both
arrive with **no edit to `auth_handler.go`**, and `clearRefreshCookieOnTerminalRefreshErr`
— an allowlist — still leaves the cookie alone. What the operator console does with
that 503 on boot is a separate question and not this section's claim: it surfaces
as an RTK Query error with a "Server error" toast and a redirect to `/login`, which
is the same visible outcome as today's 401 with a truthful cause behind it. Making
the console *survive* the blip means giving `getSession`'s custom `queryFn` a retry
arm; that is not required for the classification to be correct.

**A missing verifying key is not a bad token (v22, §8 #15).** The last three rows
are a different route to the same misclassification, and they are why the count
moved a third time. `validateTokenEnhanced` returns `ErrJWTKeysNotLoaded` when
`s.publicKey` is `nil`, and all three service entry points wrap **every**
validation failure into one opaque string — so `/refresh`, `/refresh-cookie` and
`/session` answer a server that cannot verify anything with the same codeless 401
they answer a dead session with. The split is the narrowest one available, and it
is the same shape at all three sites:

```go
claims, err := s.jwtService.ValidateRefreshToken(refreshToken)
if err != nil {
    if errors.Is(err, ErrJWTKeysNotLoaded) {
        return nil, fmt.Errorf("refresh token validation unavailable: %w: %w",
            ErrRefreshLookupUnavailable, err)
    }
    return nil, fmt.Errorf("invalid refresh token: %w", err)
}
```

A malformed JWT, a bad signature, a wrong token type, a wrong audience and an
expired refresh row all keep the wrap and the 401 they have today: those are
verdicts, and this section's discipline is that only the server's own failures
move. **No handler change** — `writeRefreshErr` already answers the sentinel with
503 `refresh_lookup_unavailable`, `refreshFailureOutcome` already has the
`lookup_unavailable` arm, and `clearRefreshCookieOnTerminalRefreshErr` is an
allowlist that already excludes it. The **signing** sites need nothing either:
`GenerateEnhancedAccessToken` and its two siblings return the same sentinel, but
their callers are the mint wraps this table already classified, which
`refresh_infra_classification_test.go` already pins. This is a validation-path-only
gap, which is exactly why it survived batch 2.

**The test hook does not exist yet, and it is eight lines.**
`refresh_orchestration_test.go`'s `breakSigningKey()` nils `privateKey`, which
reaches the mint sites and nothing else. Its public-key twin — `breakVerifyingKey()`,
same package, same `*jwtService` type assertion, setting `svc.publicKey = nil` —
is what forces the validation sites, and the ordering makes the test clean: with
`publicKey` nil, `validateTokenEnhanced` returns before `jwt.Parse`, so a test can
seed a perfectly valid refresh row and still get the sentinel back. That is the
positive case at each of the three sites; the negative is a genuinely invalid token
at the same site still answering 401.

**The sentinel's own doc comment moves in the same commit.** `auth_service.go`
enumerates the sites above `ErrRefreshLookupUnavailable` and says **TEN**. It says
thirteen and names the three new ones — the same discipline §8 #9 applied when it
struck `SEVEN`.

**Why this is safe to ship on its own.** It is additive in the only direction that
matters — a response that was 401 becomes 503 exactly when the server failed. Both
in-tree SPAs already treat 503 as transient (`frontend-admin`'s `refreshOnce`
returns `retry: true`; this SPA's `performRefresh` returns `unavailable`), so the
change makes today's operators stop being logged out by Mongo blips **before** any
client work lands. It is genuinely a fix on its own merits, not scaffolding.

**The negatives are what stop this becoming a blanket 503**, and they carry as much
weight as the positives: a `nil` token document, an expired token document, a
deleted user and a non-rotation revocation each still answer **401**, a superseded
rotation still answers **409**, and `ErrSessionEnforcementUnavailable` keeps its own
503 beside the new one. The mint's own terminals are the same list read on the
bootstrap path — `doc == nil`, an expired row, a row revoked for *any* reason
(including `rotated`, which it deliberately never turns into replay detection), an
ineligible user and a service principal all keep their 401 — plus its JWT-envelope
rejection, which is not a store call at all. A rule that never lets a dead session
end is not an improvement on one that ends live sessions.

**`RequireAuth`'s expired-bearer 401 is no longer out of scope** — O6 was ruled in
on 2026-09-01 and it is §4.10. It remains a *separate* change with its own
justification: this section changes the classification of thirteen error returns on the
refresh path; that one splits an existing branch in middleware that answers every
protected route.

### 4.10 Backend: `RequireAuth` says when the access token expired (R1 / O6)

The second and last backend change. §3.D argues why it is the right answer; this
is what ships.

**It is a split of a branch that already exists.** `validateTokenEnhanced` returns
the unwrapped sentinel `services.ErrTokenExpired` when `jwt.Parse` reports an
expired token (`jwt_service.go:544-546`), and `RequireAuth` already compares
against it — it merely merges that case with `ErrInvalidToken` into one codeless
401 on the way out (`auth.go:216-229`):

```go
claims, err := m.jwtService.ValidateAccessToken(token)
if err != nil {
    if err == services.ErrTokenExpired {
        m.sendAccessTokenExpired(w)          // NEW — the only new behaviour
        return
    }
    if err == services.ErrInvalidToken {
        m.sendErrorResponse(w, r, errors.AuthenticationError("authentication required").
            WithOperation("require_auth").Build())
        return
    }
    m.sendErrorResponse(w, r, errors.TokenInvalidError().
        WithOperation("require_auth").WithInternal(err).Build())
    return
}
```

`sendAccessTokenExpired` is modelled line-for-line on the existing
`sendSessionRevoked`: `Content-Type`, `WWW-Authenticate: Bearer
error="access_token_expired"`, 401, and a body carrying a top-level
`"code": "access_token_expired"` alongside `status` / `title` / `detail` /
`type` / `errors[]`.

**The bound on the new code — it must mean EXPIRED and nothing else.** Three
properties make that true, and all three are load-bearing:

- **Only one emitter.** No other site in `internal/` writes the string. If a
  second one appears, §4.3 branch 2's first proof stops being a proof.
- **Expiry is decided before every other check.** `jwt.Parse` reports
  `jwt.ErrTokenExpired` before `validateTokenEnhanced` reaches its own type,
  issuer and audience checks, so an expired token never reaches those branches and
  cannot be mislabelled by this one — and, conversely, an audience mismatch or a
  wrong token type can never acquire this code.
- **It is emitted before dispatch.** That is the whole reason a client may act on
  it: a request answered here provably never reached its handler, so a retry
  cannot re-consume anything (§4.4).

**What does *not* change.** `RequireAuth` stays bearer-only (ADR-0020): it never
reads the refresh cookie, never rotates, emits no `Set-Cookie` and no
`X-New-Access-Token`. Nothing about what is *accepted* changes — only what a
rejection says about itself. Every other 401 the middleware emits keeps exactly
the body it has today, including the codeless generic path §3.C describes. The
three `_NeverRotates` tests and the two structural reintroduction guards in
`require_auth_test.go` must stay green untouched; if either AST guard goes red the
change has strayed and the change is wrong, not the test.

**Why the client still cannot read the header.** `WWW-Authenticate` is set for
symmetry with `sendSessionRevoked` and for non-browser consumers, but it is not in
the API's CORS `ExposedHeaders` (`cmd/server/middleware.go:103`) and this SPA is
cross-origin to the API host. The client reads the **body**, on a clone, exactly as
§4.3 specifies. Adding the header to that list is not in scope.

**Batch 3 adds a second arm to the same branch (§8 #15).** `validateTokenEnhanced`
returns `ErrJWTKeysNotLoaded` when no verifying key is loaded, and that sentinel is
neither `ErrTokenExpired` nor `ErrInvalidToken` — so it falls through to
`errors.TokenInvalidError()` (`auth.go:244-247`) and **every protected route on
both tiers** answers a boot misconfiguration with a codeless 401. That is §4.9's
class one layer up, with one difference that decides the status: this is a
boot-time state rather than a blip, so no client-side retry can help and no client
should read it as its own session ending. `RequireAuth` gains one comparison ahead
of the fall-through:

```go
if err == services.ErrJWTKeysNotLoaded {
    m.sendTokenVerificationUnavailable(w)   // 503
    return
}
```

**`==`, not `errors.Is`, and that is not laziness.** `validateTokenEnhanced`
returns the sentinel **unwrapped**, exactly as it returns `ErrTokenExpired`, so the
new comparison is the same shape as the two it joins — and in this file the
identifier `errors` is the **shared** `internal/shared/errors` package
(`errors.TokenInvalidError()` a few lines below), with the standard library's
`errors` not imported at all. Reaching for `errors.Is` here does not compile, and
"fixing" that with an import alias would be a change to the file's conventions
smuggled in by a one-line branch. If the sentinel ever starts arriving wrapped,
that is the moment to change all three comparisons together.

`sendTokenVerificationUnavailable` is modelled on `sendPolicyUnavailable`, the
middleware's one existing 503: `Content-Type`, `WriteHeader(503)`, and a body of
`status` / `title` / `detail` / `type: "about:blank"` / a top-level
`"code": "token_verification_unavailable"`. **No `WWW-Authenticate`** and **no
`errors[]`** — the header names a scheme the caller should retry with and there is
nothing to retry with, and `sendPolicyUnavailable` omits both for the same reason.
The "only one emitter" bound stated above for `access_token_expired` applies
verbatim to this string too: if a second site ever writes it, it stops meaning what
it says.

**What does not change, again.** The accepted set: a server with no verifying key
accepted nothing before and accepts nothing now — only the account it gives of
itself changes. `RequireAuth` stays bearer-only, the three `_NeverRotates` tests
and the two AST reintroduction guards stay green untouched, and every other 401 the
middleware emits keeps its exact body. Downstream, neither SPA needs a change to
handle it: §4.1's outcome table is an allowlist in which **only 401** signs a user
out, so the client SPA reads a 503 as `unavailable` and keeps its token, and the
console's `baseApi` never reaches its 401 branch at all — a `>= 500` response
raises the "Server error. Please try again later." toast (`baseApi.ts:598-599`)
and nothing is cleared.

**Not applied to `frontend-admin` by this section.** The console's own fix is
§8 #5 and it landed in batch 2, in its own commits — and it is **not** the one-line
`code === "access_token_expired"` gate this paragraph once promised.
`prepareHeaders` withholds a locally expired bearer, so the 401 that follows is
codeless and that gate would have switched the console's reactive path off in
almost every real case; what shipped is §4.3's two-proof disjunction (§7). Batch 3
adds the one arm that disjunction deliberately left open, §8 #14.

### 4.11 Client: rotate before expiry, not only after a 401 (§8 #2)

Batch 3, and the last piece of ADR-0020 D3 parity the client tier is missing. §4.3
is a **recovery**: it costs a 401 round-trip and it may only fire on proof that the
request never reached its handler. A rotation taken *before* the request costs no
round-trip and needs no proof, because there is no request to replay.

**The constant, and why it is not the one §4.3 refuses to have.**

```ts
// src/api/authedFetch.ts, beside TERMINAL_CODES and CODE_ACCESS_TOKEN_EXPIRED.
//
// INVARIANT (ADR-0020 D3): strictly below the backend's MinAccessTokenTTL
// (60 s, backend/internal/core/auth/services/auth_duration_bounds.go). At or
// above the floor, a token minted at the minimum TTL is already inside this
// window the moment it arrives, so every request would rotate again — a
// refresh loop.
export const PROACTIVE_REFRESH_SKEW_MS = 30_000;
```

**The arm.** It sits between the snapshot and `doFetch` — the two statements §4.3
takes together — and it **re-snapshots**, because `sent` and `sentAt` have to
describe the request that actually goes out:

```ts
let sent = getAccessTokenSnapshot();
if (
  sent.token !== null &&
  sent.expiresAt !== null &&
  sent.expiresAt - Date.now() < PROACTIVE_REFRESH_SKEW_MS
) {
  await refreshAccessToken(apiBaseURL);
  sent = getAccessTokenSnapshot();
}
const sentAt = Date.now();
const res = await doFetch(path, init, sent.token);
```

**Which refresh function, and why the outcome is not inspected.**
`refreshAccessToken` — the marker-gated **automatic** path — not
`refreshAfterUnauthorized`. A proactive rotation is automatic by definition, and
the marker gate is a correct optimisation here rather than the hole §4.3 4a routes
around: a visitor with no session has no `expiresAt` either, so the branch cannot
fire for them at all. The one input where the gate does bite is §5.8's tab — a live
in-memory token with **no** marker, reachable through a throwing `localStorage` or
a sibling's sign-out — and there the proactive attempt is simply a no-op, so that
tab keeps the reactive path it has today and branch 4a, which is deliberately *not*
marker-gated, recovers it. One extra round-trip, never a wrong sign-out, which is
the same trade §4.3's 4a/4b split already makes. `refreshAccessToken` never
rejects, is coalesced in-tab, serialised across tabs by §4.1a's Web Lock and
bounded by §4.1c's timeout; the arm inherits all four properties instead of
restating any of them.

The outcome is then **ignored**, and that is a design decision rather than a
shortcut — each of the three is already handled where it is decided:

- **`ok`** — `performRefresh` has already installed the new token in the store, so
  the re-snapshot picks it up and the request carries the fresh bearer. There is
  nothing for the arm to dispatch.
- **`unavailable`** — token and marker untouched (§4.1's allowlist), so the request
  goes out with the old bearer and §4.3 owns whatever 401 follows. A failed
  proactive rotation costs one round-trip and changes nothing else.
- **`signed-out`** — from `performRefresh` that means the refresh itself was
  answered 401, and it has already cleared token **and** marker (**G3**); the
  re-snapshot yields `null`, the request goes out anonymous and its 401 is passed
  through by branch 2 — the same end state as a sign-out anywhere else. From
  `refreshAccessToken`'s **marker gate** it means no marker was present and no
  request was made: a no-op that clears nothing, which is exactly right for a
  rotation nobody asked for.

The arm therefore introduces **no state transition of its own**, which is the
property that keeps §4.1's outcome table the single owner of the sign-out decision.

**`expiresAt === null` is not a reason to rotate.** An unknown expiry counts as
**live** everywhere in this design (§4.3 branch 2, §4.5), and it counts as live
here too: rotating on "we cannot tell" would rotate on every request made with a
token whose lifetime was never learned, which is the refresh loop the D3 bound
exists to prevent, arrived at from the other side. No bearer at all is likewise no
attempt.

**The skew NEVER enters the 401 branch — invariant.** §4.3 branch 2's proof (2) is
`sent.expiresAt <= sentAt` with **no margin**, and the margin is precisely the
round-11 replay hole (v13): a token with 20 s of life is still accepted by the
server, so the handler *did* run. Two constants, two predicates, one file — the
discipline `baseApi.ts` documents for `tokenNeedsRefresh` versus `liveBearer`, and
the reason those two must never be merged. It is pinned mechanically rather than
argued: `PROACTIVE_REFRESH_SKEW_MS` appears in `authedFetch.ts` **only** in its own
declaration and in the arm above — never below the `if (res.status !== 401) return
res;` line — and a test asserts that by reading the module's own source and cutting
it at that line. A behavioural test cannot express this one, because the two
predicates agree on almost every input and differ only where the difference is the
bug.

**No endpoint exclusion list, deliberately.** `frontend-admin` excludes
`AUTH_ENDPOINT_PATHS` and `/session` from its proactive check because its refresh
goes through the same `baseQuery`. This SPA's does not: `performRefresh` calls
`/v1/auth/client/refresh-cookie` with a raw `fetch` from `tokenStore`, and every
`authedFetch` call site is a protected resource route. Recursion is structurally
impossible, and the rule that keeps it so is already at the top of
`authedFetch.ts` — the refresh endpoint is never called through this helper.

**Rolling deploys are unaffected, and that is worth stating because §4.10 made it a
question.** This arm asks the backend for nothing new: it is a client-side
scheduling decision taken from a duration the client derived itself at receipt
(§4.5). §4.3 branch 2's proof (2) still exists for a backend that has not shipped
§4.10, and §4.11 neither strengthens nor weakens it.

**What it costs.** Never more than one rotation per request. Over a session it
normally *moves* a rotation earlier rather than adding one — the request that would
have 401'd and then refreshed now refreshes and then succeeds. The one shape that
genuinely costs an extra rotation is a session whose **last** request lands inside
the window: it rotates a token nothing will use. That is the price of never
spending a request to discover an expiry, and it is the same trade ADR-0020 D3 made
for the console.

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
    `TERMINAL_CODES` and are not `access_token_expired` either, so `read401Code()`
    returns a string that matches **neither** membership test, and with a live
    token they land in branch 2 and are surfaced to the caller. Correct — and the
    reason branch 1 tests set membership rather than "has a code" (§3.C). Round 15
    makes this sharper rather than softer: there are now two positive tests on the
    same field, and a code belonging to neither is still simply passed through.
    The client SPA has no step-up flow yet, so the caller's own error handling is
    what the user sees; that is unchanged by this spec.
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
409 or the lock, so all of those are additive — but **exactly two existing
assertions are inverted**, and they are one rule seen twice: a **2xx without a
token** now resolves `unavailable` with the session marker **kept**, where D15 made
it `signed-out` and cleared the marker. The two are `bootstrapFromRefreshCookie`'s
case and `refreshAccessToken`'s, one each. They are edited rather than added
because the rule they pinned is the rule §4.1's table replaced: a broken response
is precisely the input on which the server has said nothing about the session.

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
- **unknown expiry** (`expiresAt === null`, §4.5's fallback exhausted) on a POST,
  **and no code from the server** → passed through, **no** refresh. This is the
  case whose direction flipped in round 11, so it needs a test that fails against
  the old direction;
- **the R1 case, and it is the one that fails against a reckoning-only
  implementation**: a **live** token (say 900 s of life left) answered 401 with
  `code: access_token_expired` → the refresh **is** attempted and the retry
  carries the new bearer. This is the in-flight expiry §4.4 gives up without the
  server's help, so a test that used an already-expired token would pass either
  way and prove nothing — the fixture's *remaining life* is the test, exactly as
  in the round-11 case above;
- the mirror, which is what keeps R1 from widening branch 2 into a blanket retry:
  the same **live** token answered 401 with **no** code → passed through, zero
  refreshes. Together the two pin that the code is read as a *positive* signal and
  that its absence is not one;
- **unknown expiry plus `access_token_expired`** → recovered. Proof (1) does not
  consult the expiry at all, and this is the case where the two proofs visibly
  compose rather than merely coexist;
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
  against `if (body.code) → clear`, the simplification §3.C exists to forbid. Add
  `access_token_expired` to the *terminal* half of this assertion too — on a live
  token it must **not** clear anything, only refresh — since the two membership
  tests now read the same field and a single `if/else` chain written in the wrong
  order would collapse them;
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
- **B's sent token is expired and the store's token is `null`** (a sign-out landed
  mid-flight, clearing both token and marker) → branch 3 is skipped, because the
  store holds `null` rather than a *different* token, and **branch 4a still runs on
  the strength of the sent bearer**: exactly **one** `/refresh-cookie`, and B's
  retry carries the new one. This is the case that pins 4a's split on the **sent**
  bearer rather than on the store — an earlier draft of this bullet asserted the
  opposite ("`signed-out` with no request"), which is what routing 4a through the
  marker-gated `refreshAccessToken` would produce and is exactly the hole §4.3
  splits the two functions to close.

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
- neither present nor readable → the expiry is recorded as **unknown** (`null`),
  and §4.3 branch 2 then treats unknown as **live**: the 401 is passed through with
  no refresh. Round 11 flipped this direction and §4.5 states it — an unknown expiry
  cannot prove the handler never ran, and on a rule whose failure mode is a *replay*
  rather than a wasted refresh, "don't know" falls on the safe side.

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
- the user lookup returns an **infrastructure** error → **503** (not the 401 that
  "user not found" produces today — the case most likely to be written wrongly,
  because the current wording invites asserting a 401);
- **the user lookup returns `iface.ErrUserNotFound` → still 401** (R2). This is the
  pair that has to be written together, and the *only* one where the two inputs are
  both errors: the fake must return the sentinel for a deleted account and a plain
  error for an unreachable store, or the split is untested and a 503 loop for an
  erased account ships silently. Pin the alias too —
  `errors.Is(userservices.ErrUserNotFound, iface.ErrUserNotFound)` — since the whole
  classification rests on it;
- `mintTokenPair` fails → 503; `RotateWithFamily` fails → 503;
- **the negatives, which are what stop this becoming a blanket 503**: a `nil` token
  document, an expired token document and a non-rotation revocation each still
  answer **401**, a superseded rotation still answers **409**, and
  `repository.ErrTokenAlreadyRotated` from the write is **not** classified as an
  outage. Regression-pin `ErrSessionEnforcementUnavailable`'s existing 503 too, so
  the new branch is proven to sit beside it rather than swallow it.

⚠️ **The existing auth-service test fake lies about this and must be fixed first.**
`gates_fakes_test.go`'s `errNotFound` is a bare error whose own comment says
"callers don't introspect the specific error type, so a plain error string is
enough" — true until R2, false after it. Left as is, the deleted-account case would
pass while production classified the same input the other way. It must wrap
`iface.ErrUserNotFound`.

**Backend (§4.9, the picker — v18).** The four cases above drive the service; the
finding that produced v18 is that the browser never reaches the service under an
outage, so the tests below drive the **handler**, or they prove nothing about the
path either SPA uses:

- `PeekRefreshToken` with a failing `GetByTokenAny` → `errors.Is(err,
  ErrRefreshLookupUnavailable)`. Its two existing negatives stay: an unknown token
  is still `ErrInvalidRefreshToken`, a malformed JWT is still a plain error —
  neither may become the sentinel;
- **picker unit tests** (`refresh_picker_test.go`, whose fake `peek` already takes
  an error per row): an infrastructure error on the **only** candidate →
  `("", "", err)`; an infrastructure error on one candidate and a **valid** sibling
  in either order → the sibling is chosen; an infrastructure error on one and a
  **rotated** sibling → `("", "", err)` and **no** fallback — the case that stops
  incomplete classification from firing family revocation; a **non**-sentinel error
  (`errors.New("invalid")`, the existing fixture) → still skipped silently, no
  `lookupErr`. The five existing picker tests are updated for the third return
  value and must keep passing unchanged in meaning;
- **HTTP-level, through the real handlers**, with an `AuthHandler` built the way
  `logout_identity_test.go` builds one (`&AuthHandler{authService: fake, config:
  cfg}`, `cfg.Auth.Cookie.Name` set) and a fake that **embeds**
  `services.AuthService` and overrides only `PeekRefreshToken` — every other call
  hits the nil embed and panics, which is the assertion that nothing else was
  reached:
  - `POST /refresh-cookie` with a cookie and a Peek that fails with the sentinel →
    **503** `refresh_lookup_unavailable`, **no `Set-Cookie`** (the cookie was not
    cleared), and `RefreshTokensWithRiskAssessment` was **never called** — override
    it to record the call and return `ErrRefreshTokenReplay`, the wrong answer, so
    a replay fallback that fires shows up as a 401 with the wrong code rather than
    as a silently green run;
  - the same through `GET /session` (`GetSessionHTTP`, the operator console's boot
    path) and `POST /refresh` (`RefreshTokensWithHeaderHTTP`);
  - the negative: a Peek that fails with a **non**-sentinel error still answers
    **401** — the picker's "invalid candidate" meaning survives;
- `refreshFailureOutcome(ErrRefreshLookupUnavailable)` → `"lookup_unavailable"`,
  not `"invalid_token"`.

**Backend (§4.9, the race — v19).** These sit beside `refresh_rotation_grace_test.go`,
which already has the vocabulary (`rotateOnce`, `FamilyRevoked`,
`activeFamilyMembers`, `backdateRevocation`). Each positive asserts three things:
the sentinel, that the family was **not** revoked (`activeFamilyMembers` unchanged
**and** `RevokeFamily` never called — the second is what proves the classifier did
not merely fail to persist a revocation it had decided on), and that no credentials
were issued:

- **the benign race under a blip (G6/G7)**: seed, `rotateOnce`, then make
  `FamilyRevoked` fail and present the superseded token inside the grace window →
  `ErrRefreshLookupUnavailable`, the winner's successor still active, `RevokeFamily`
  not called. This is the input that used to sign every tab out;
- **CAS lost, re-read fails** → the sentinel, nothing revoked, replay not fired.
  Needs a fake whose `GetByTokenAny` succeeds on the first call and fails on the
  second — the read-then-re-read shape is the point, so a fake that fails every
  call tests the wrong site (it never reaches the CAS);
- **CAS lost, re-read succeeds and shows a rotated-in-grace row, `FamilyRevoked`
  fails** → the sentinel, nothing revoked. The re-read must return a *different*
  row than the first read — that is what a race is — so the fake intercepts the
  second call and returns the row marked rotated;
- **the negatives, which keep replay detection intact**: CAS lost with a readable
  family state → 409 inside the window with a healthy family, replay outside it or
  with a revoked family — the four existing `TestRefreshGrace_*` cases, unchanged;
  and the plan's `RotateCASLoss` case **narrowed** to "with a readable family
  state, a lost CAS is replay by state, not an outage", asserting `RevokeFamily`
  *was* called — the unqualified version was the assertion finding #2 objected to;
- `handleRefreshReplay` with a failing `RevokeFamily` still returns
  `ErrRefreshTokenReplay` (401): a verdict that was actually reached is denied
  even when the revocation did not persist. Pins the boundary between "could not
  decide" (503) and "decided, could not persist" (401).

**Backend (§4.9, the read-only mint — v20).** These join
`refresh_infra_classification_test.go`, which already carries the shape:
`errStoreDown`, and positives that assert **both** the sentinel and the underlying
cause. One positive per site, and the negative is the one that must not be skipped
— without it a blanket 503 for a deleted account passes:

- `MintAccessTokenFromRefresh` against `setGetByTokenAnyErr(errStoreDown)` →
  `errors.Is(err, ErrRefreshLookupUnavailable)` **and** `errors.Is(err,
  errStoreDown)`, and a **nil** `TokenResponse` — the mint must never hand back
  credentials it could not verify;
- the same with `setGetByIDErr(errStoreDown)` on the user fake → the sentinel and
  the cause;
- the same with `breakSigningKey()` — a nil private key, so the generator returns
  `ErrJWTKeysNotLoaded` **without touching any repository**, which is what isolates
  the mint site from the two store sites;
- **the negative: a refresh row whose user was never seeded.** The fake's
  `errNotFound` already wraps `iface.ErrUserNotFound`, so this is one line of setup
  — and the assertion is `ErrInvalidRefreshToken`, **not** the sentinel. A deleted
  account must still end its session; this is R2's permanent-503 loop in the mint's
  own words;
- **HTTP, through the real handler**: `GET /v1/auth/session` with a cookie whose
  **Peek succeeds** and whose **mint fails** with the sentinel → **503**
  `refresh_lookup_unavailable` and **no `Set-Cookie`** (`assertOutage503` asserts
  both already). It needs a settable `mintErr` on the existing
  `outagePeekAuthService` plus a Peek that returns a live row — a few lines on the
  fixture, not a new harness. The existing
  `TestGetSessionHTTP_CookieLookupOutage_Is503` covers the *picker* case and
  asserts `mintCalled == false`, so it cannot see this one.

**Backend (§4.10)** — beside the existing `require_auth_test.go` cases, whose
fixture already has `mintExpiredAccessToken` (and which already asserts its own
precondition against `services.ErrTokenExpired`):

- an expired bearer → **401** carrying top-level `code: access_token_expired` and
  `WWW-Authenticate: Bearer error="access_token_expired"`, and the downstream
  handler is **not** reached — the second half is what makes a client retry safe;
- **the bound**: a missing bearer, a malformed one and a tampered signature each
  still answer 401 **without** that code. Without these the code could quietly come
  to mean "401", and §4.3 branch 2's first proof would stop being a proof;
- a **revoked** session still answers `session_revoked`, not the new code — the new
  branch must not shadow the terminal one;
- the three existing `_NeverRotates` cases and the two structural guards
  (`TestAuthMiddleware_Fields_CannotReintroduceCookieRotation`,
  `TestAuthGo_ContainsNoCookieRead`) stay green **unmodified**. They parse
  `auth.go`'s AST; if either goes red the emitter has strayed into cookie territory
  and the change is wrong, not the test;
- one grep, as a test or as a review step: `access_token_expired` has **exactly one**
  production emitter in `internal/`.

**The deletion (§4.8)** cannot be asserted by a unit test — there is nothing left
to call. It is verified the way an absence has to be:

- `grep` for any `api` import across `src/` returns nothing (the same check that
  justified the removal, re-run after it);
- `npm run typecheck` and `npm run build` pass, which is what catches an orphaned
  `paths` import or a now-unused `clearAccessToken`;
- `npm run lint` passes with `--max-warnings 0`, which is what catches the unused
  imports left behind in `client.ts`;
- the full suite stays green, proving nothing was routing through it after all.

**`src/api/authedFetch.test.ts` — proactive rotation (§4.11, batch 3).** A new
`describe("authedFetch proactive rotation (§4.11)")` block, plus a migration of the
existing 401 suite that is not optional — the arm sits *before* the request, so it
changes what those cases send.

New cases:

- **near expiry** — `seedToken("at-near", 20)`: one `/refresh-cookie` **before** the
  request, and the request's `Authorization` header carries the **new** bearer.
  Zero 401s, which is the whole point: no request is spent discovering the expiry;
- **far from expiry** — `seedToken("at-1", 900)`: no refresh, and the request
  carries the seeded bearer;
- **unknown expiry** — `setAccessToken("opaque-not-a-jwt")`, so `expiresAt === null`:
  **no** refresh. Unknown is live here exactly as it is in §4.3 branch 2;
- **no bearer** — an empty store: no refresh, no attempt;
- **the proactive rotation is `unavailable`** — `/refresh-cookie` answers 503: the
  request still goes out, with the **old** bearer, and token and marker both
  survive;
- **already expired at send** — `seedExpiredToken()` with the proactive attempt
  answered 503: the request goes out with the dead bearer, 401s, and §4.3 branch
  2's proof (2) recovers it exactly as it does today. This is the case that proves
  §4.11 cannot strand the 401 path;
- **the D3 bound** — `expect(PROACTIVE_REFRESH_SKEW_MS).toBeLessThan(60_000)` with
  the comment naming `MinAccessTokenTTL`, plus the behavioural twin ADR-0020 D3
  prescribes: a token installed with `expiresIn: 60` does not rotate on the next
  request (`baseApi.proactiveRefresh.test.ts`'s "does not loop on a token minted at
  the backend minimum TTL (60s)" is the model);
- **the no-leak invariant** — the constant is referenced only above the
  `if (res.status !== 401) return res;` line in `authedFetch.ts`, asserted by
  reading the module's own source and cutting it there (§4.11 says why a
  behavioural test cannot express it).

Migration of the existing 401 cases, which follows mechanically from where the arm
sits:

- **a seed outside the window is untouched.** Every `seedToken(…, 900)` case, both
  unknown-expiry cases and the no-token case keep their exact counts;
- **a seed inside the window now rotates first.** Every `seedExpiredToken()` case
  answers its **first** `/refresh-cookie` hit with a **503**, so the proactive
  attempt is `unavailable`, the request goes out with the seeded token, and the
  case exercises §4.3 exactly as before — with its `refresh.hits()` expectation
  raised by one. `countRefresh`'s responder already receives the hit index, so this
  is a fixture change, not a helper change;
- **`"a live token's 401 is passed through — no refresh, no replay"` changes its
  seed**, from `seedToken("at-live", 20)` to `seedToken("at-live", 300)`, and its
  self-asserting premise from 15/25 s to 290/310 s. Twenty seconds was never the
  load-bearing part — "alive, so the handler ran" was — and twenty seconds is now
  inside the window;
- **`"expiresAt === sentAt counts as expired; sentAt + 1 counts as live"`** is the
  no-margin pin, and both of its halves are inside the window by construction. It
  keeps its shape by counting the two kinds of rotation apart: `/refresh-cookie`
  answers **503 on odd hits** (the proactive attempts) and 200 on the even one, so
  the boundary token adds a **second** rotation and the `+1 ms` token adds
  **none**. Same property, read as a difference;
- **`"a burst of three 401s produces exactly one /refresh-cookie"` moves its title
  too.** With the proactive attempt answered 503 the three concurrent calls
  coalesce **twice** — one proactive rotation shared by the burst, then one
  reactive rotation shared by the three 401s — so `refresh.hits()` is **2** and the
  title becomes `a burst of three 401s produces exactly one reactive
  /refresh-cookie`. The coalescing the case exists to pin is now pinned twice over,
  which is the point rather than a dilution of it;
- **`"expired token with NO marker still attempts the refresh (branch 4a)"` is
  untouched, and it is worth knowing why**: the arm calls the marker-gated
  `refreshAccessToken`, which answers `signed-out` from its gate **without making a
  request and without clearing anything**, so the case reaches branch 4a with its
  count unchanged. It is also the only existing case that exercises that gate from
  the proactive side.

**Regression:** `auth.test.ts`, `AuthProvider.test.tsx`, `OAuthCallbackPage.test.tsx`
and `App.test.tsx` must stay green unmodified. If one needs editing, that is a
signal the change is wider than this spec claims — raise it rather than adjust it.

**Manual.** Dev stack only — staging cannot serve the client tier at all while
`CLIENT_API_HOST=client-disabled.invalid` (§7).

Shorten the access-token TTL to its floor — and it has to be done through the
**admin config key**, not the env var: `accessTokenTTL: "1m"` at `/admin/modules/auth`
or by `PATCH /v1/admin/modules/auth`. `JWT_ACCESS_TOKEN_EXPIRY=60s` in `docker/.env`
changes nothing, because the auth schema declares `accessTokenTTL` with `Default:
"15m"` and no `EnvVar`, so the seed (and `GetValue`'s schema fallback) supplies
`15m` and the admin value wins — §8 #12, measured that way during verification.
**60 s is the floor**, not an arbitrary choice — but the PATCH does not clamp to
it: `1m` is the shortest value it accepts, and a `10s` is **refused with a 422
naming the field** (`validateAuthDurations`, `config_validation.go:84-89`). The
silent clamp *up* to 60 s belongs to the two levels that cannot surface a 422:
`JWT_ACCESS_TOKEN_EXPIRY` through `NewJWTService` (`b3fdefee`), and a value written
into the DB out of band through `clampPersistedDuration` on read. So a `10s` typed
into the admin field fails loudly; a `10s` that reaches the service any other way
behaves as 60 and makes the wait look broken.

1. Sign in, wait past the TTL, act on `/account/security` → succeeds after exactly
   one `/refresh-cookie`. §4.11 changes this scenario's **shape**, not its outcome:
   with proactive rotation in place the refresh *precedes* the request, so the
   network panel shows **one `/refresh-cookie` and zero 401s** where today it shows
   a 401, then the refresh, then the replay. Run it before and after that wave —
   the disappearance of the 401 is the only directly observable evidence the arm is
   live.
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
  (§4.9 and §4.10 are Go). Expect `errquality` to have an opinion about the new
  emitters — if it does, fix the emitter rather than baseline it: this work exists
  to *improve* error classification, so a complaint from that gate is signal.
- **Ordering is a hard dependency, not a preference.** §4.1's `401 → signed-out`
  row is only sound once §4.9 has moved infrastructure failures off 401. Shipping
  the client half first would leave the Mongo-blip logout in place *and* strip the
  accidental cover it has today — with no 401 recovery there is no refresh, so no
  spurious sign-out from one. **§4.9 first, deployed, then the client work**, as
  two PRs or as one PR whose commits land in that order.
- **§4.10's ordering is a preference, not a dependency** — deliberately, and it is
  worth knowing which is which. The client half works against a backend without it
  (that is what §4.3 branch 2's second proof is for), and §4.10 works against a
  client that ignores the code (it is an added field on an existing 401). It ships
  with the backend commits only because that keeps the deploy story simple; if it
  slipped, nothing breaks and the in-flight-expiry recovery simply waits.
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

**The same defect in `frontend-admin` — closed in this branch (follow-up 5, §8
#5), not left open.** The console carried the same replay hazard and a wider one:
its reactive branch gated on two path tests — not an auth endpoint, not the
session endpoint — and on nothing else. No token-state gate, no body inspection.
**Every** 401 on a non-auth endpoint was refreshed and the original request
re-sent, whatever the 401 was about, and four console routes answer 401 as a
**verdict on the request** with none of them in `AUTH_ENDPOINT_PATHS`:

| route | what the replay cost |
| ----- | -------------------- |
| `me/password-confirm` (`authApi.ts:475`) | the **provable** one: `ConfirmPasswordWithSecurity` calls `recordFailed` (`password_auth_service.go:1300`), so a replay double-counts the lockout budget under both the IP and the email key |
| `change-password` (`authApi.ts:454`) | a second argon2id verify of the same wrong password and a second audit-relevant failure — but **no counter**: `ChangePassword` does not call `recordFailed` |
| `mfa/verify`, `mfa/enroll/confirm` | a replayed TOTP burns the replay guard or consumes a backup code; enrolment confirmation burns one of `MFAMaxAttempts` (5), and the fifth deletes the challenge |
| WebAuthn `*/finish` | a consumed challenge |

Earlier revisions of this paragraph put the lockout double-count on
`change-password`. It belongs to `/me/password-confirm`; `change-password` is
replayed too, and its harm is real, but it is not countable. Both routes share
`mapPasswordError` with the client tier.

**The fix was not the one-liner an earlier revision promised.** Gating the retry on
`code === "access_token_expired"` alone would have switched the console's reactive
path off in almost every real case: `prepareHeaders` (`baseApi.ts:298-305`)
**withholds** the bearer once the console's own recorded expiry has passed, the
request then arrives with no `Authorization` at all, `RequireAuth` takes its
missing-token branch — and that 401 is **codeless**, because §4.10 emits the code
only for a well-formed, correctly signed, *expired* bearer. The code therefore
reaches the console only in the narrow expired-in-flight window, and ADR-0020 D3
frames the reactive path as the fallback for a **failed proactive rotation**, which
is exactly the shape that arrives without one. So what shipped is §4.3's
**disjunction**, in `baseQueryWithRetry`'s reactive branch (`baseApi.ts:507`):
refresh and replay only when the 401 body carries `code: access_token_expired`
**or** the request went out without a live bearer — decided by `liveBearer()`
(`baseApi.ts:279`), the one predicate `prepareHeaders` uses to withhold a bearer
(`:301`), read from a snapshot captured before the fetch (`:383`) so a sibling
tab's rotation or sign-out cannot rewrite the answer. Neither proof and the 401 is
returned to the caller untouched — no refresh, no replay, no sign-out. Rollout was
backend-first: §4.10 shipped the code, and the second disjunct is what keeps the
console recovering against a backend that has not.

Two replays at `:445-473` are **correct** and survive the gate — `step_up_required`
and `password_confirm_required` re-send `args` deliberately, after the user has
re-authenticated, and both sit ahead of the branch.

**And `AUTH_ENDPOINT_PATHS` turned out to be part of the guard, not merely loop
avoidance.** The second disjunct reasons that a bearer-less request was rejected by
`RequireAuth` before dispatch — true for a **protected** route, false for a
**public** one, which runs its handler with no bearer by design. The console calls
two public routes that answer 401 as a verdict and were not in the list:
`mfa/webauthn/login/begin` and `.../finish` (`mfaApi.ts:295,307`;
`WebAuthnHandler.RegisterPublicRoutes`), where `LoginFinish` calls
`IncrementAttempts` *before* returning its 401 — so under the new gate a paused
passkey login, whose store holds no access token, satisfied proof (b) and spent two
of `MFAMaxAttempts` (5) per typo. One substring entry,
`v1/auth/operator/mfa/webauthn/login/`, covers both halves of the ceremony; the
TOTP twin `mfa/login/verify` was already listed. Adding the four **protected**
verdict-401 routes to that allowlist remains the alternative to the gate, and
remains worse: it is the hand-maintained list which **already failed open once**,
which is how this defect existed (§3.B) — and it failed open the same way a second
time, on the passkey pair, which is why the two mechanisms are documented together
in `baseApi.ts`. N3 is discharged; the tests are in
`frontend-admin/src/store/api/baseApi.replayGuard.test.ts` (6 cases) plus the
fixture audit of the four pre-existing `baseApi.*.test.ts` suites.

**Batch 3 closes the window that gate deliberately leaves open (§8 #14).**
Everything above describes what shipped: neither proof, and the 401 goes back
untouched. That is right for a verdict and wrong for the one input in that shape
which is not one — a JWT signing-key rotation, or a restart with new key material,
after which every unexpired bearer validates as plain "invalid" and `RequireAuth`
answers a **codeless** 401. Against that the console today does nothing at all: no
refresh, no toast, no sign-out, every request failing silently until the proactive
check fires at `expiry − PROACTIVE_REFRESH_SKEW_MS`. Batch 3 adds a third outcome
to the same `handlerNeverRan` decision, sitting between it and the existing replay
path. Everything ahead of it is unchanged: the branch is reached only for a 401
outside `AUTH_ENDPOINT_PATHS` and not on the session endpoint, and the
terminal-code, step-up and password-confirm checks have already returned by then.

| The 401, at that point | Action |
| ---------------------- | ------ |
| `code === "access_token_expired"`, **or** no live bearer was sent (`sentBearer === null`) | unchanged — refresh **and** replay, the path §7 describes above |
| a live bearer was sent **and** the body carries **no top-level code at all** (`errorData?.code === undefined`) | **NEW** — `performRefresh(runtimeConfig.apiUrl)` **once**, then return the **ORIGINAL 401 unchanged**. No replay |
| a live bearer was sent and the body **does** carry a code | unchanged — return the original 401 untouched |

The refresh outcome is handled in the console's own vocabulary, and it is the only
place the arm branches:

- **`ok`** → `api.dispatch(setAccessToken({ accessToken, expiresIn }))`, then return
  the original 401. **No replay** — **G4** holds, and the request that earned the
  401 is never sent twice. The *next* request carries the fresh bearer, which is
  what collapses the window to a single request;
- **`retry` or `raced`** → return the original 401, token and expiry untouched;
- **a bare `{ ok: false }`** → the refresh itself was refused, which is the
  session's own death: `clearAccessToken()` and then the existing sign-out path
  below, exactly as the replay branch already does on the same outcome.

**Codeless, not "anything but `access_token_expired`".** The narrower predicate is
the correct one, and the difference is not hypothetical: a 401 that names itself
has been explained by the server, and a new token minted from the same cookie
cannot change the answer. `audience_mismatch` is the live example — a coded 401
emitted by `RequireAudience`, unhandled by every branch ahead of this one, and
carrying the same audience after any rotation.

**What it costs.** One serialised rotation per verdict 401 — a mistyped current
password now rotates the refresh cookie once. That is harmless: the cookie rotates
by design, the family is untouched, and `performRefresh` coalesces in-tab and takes
the cross-tab lock, so a burst costs one rotation, not one each.

**The two assertions that flip, and the two beside them that must not**, both in
`frontend-admin/src/store/api/baseApi.replayGuard.test.ts`:

- case 1, `does not refresh or replay a wrong-current-password change-password 401`
  → retitled `rotates once but does not replay a wrong-current-password
  change-password 401`. `refreshAttempts` **0 → 1**, and the store's token
  `'seed-access-token'` → `'fresh-token'`. **`changeAttempts` stays 1** — that is
  the invariant #14 must preserve and the assertion that keeps it honest;
- case 4, `passes a codeless 401 through when the store lost its bearer mid-flight`
  → retitled `rotates once and still passes the codeless 401 through when the store
  lost its bearer mid-flight`. `refreshAttempts` **0 → 1**, and the refresh
  fixture's `'must-not-be-fetched'` token becomes `'rotated-token'`, because it now
  is fetched. **`resourceAttempts` stays 1**, and the caller still receives the 401.

Cases 2, 3 and 6 take the existing replay path and are untouched. Case 5 — the
failed passkey assertion — is on a public route excluded by `AUTH_ENDPOINT_PATHS`
and never reaches the branch at all, which is the same reason that allowlist is
part of the guard rather than loop avoidance. The terminal-code, step-up and
password-confirm checks all stay **ahead** of the new arm, and the arm itself must
dispatch no `clearAccessToken()` and call no `navigateToLogin` on its own two
returning outcomes.

## 8. Follow-ups (each carries its own status)

1. ~~**Backend: a distinct `access_token_expired` code**~~ — ✅ **done in this
   work** (O6 ruled in, round 15). It is §4.10. The number is kept rather than
   reclaimed so the cross-references in §4.5, §7 and elsewhere stay meaningful.
   What it does **not** do is apply the resulting fix to
   `frontend-admin` — that landed as #5 in batch 2.
2. **Proactive rotation for the client SPA** (ADR-0020 D3 parity) —
   **ruled in for this branch (batch 3)**, and it is **§4.11**. Refresh before
   expiry instead of after a 401. It needs a trustworthy remaining-lifetime
   figure, which §4.5's `expiresAt` snapshot already provides — and provides
   *correctly* under clock skew, which is what makes a proactive scheme safe to
   build on. This is what introduces `PROACTIVE_REFRESH_SKEW_MS`; ADR-0020 D3's
   `SKEW < MinAccessTokenTTL` invariant applies to it, and it must not leak back
   into the 401 comparison (§4.3 branch 2, §4.5) — §4.11 states that as an
   invariant and pins it by reading the module's own source rather than by
   argument.
3. **Wake up `openapi-fetch`** — **resolved as #4.** The dependency and the
   generated stub go rather than stay warm, so there is nothing left to wake. If a
   typed client is ever wanted, it re-adds a pinned dependency in the same PR that
   writes the middleware, against a real generated type rather than a stub — and
   that middleware must **delegate to `authedFetch`'s policy** rather than restate
   it (§4.8 deleted the version that restated it, badly). Nothing about that is
   made harder by #4; the only thing #3 was preserving was the *materials* for a
   second 401 algorithm, which is what §4.8 argues against.
4. **Drop the `openapi-fetch` runtime dependency** —
   **ruled in for this branch (batch 3)**, and it subsumes #3. Nothing imports
   it: `src/api/client.ts` is the API-base resolver and exports only
   `apiBaseURL`, and `src/api/openapi.gen.ts` is a stub nothing reads — so its
   Dependabot bumps are vacuous *by construction*, which is the condition under
   which a bump gets closed, not the condition under which a dependency is kept.
   Five artefacts go, and they go together: `openapi-fetch` from `dependencies`,
   `openapi-typescript` from `devDependencies`, the `codegen` npm script,
   `src/api/openapi.gen.ts` itself, and the prose —
   `frontend-client/README.md`'s stack bullet, its whole `## OpenAPI codegen`
   section and its layout tree, plus the `frontend-client/CLAUDE.md` regions
   that name them. Dropping the runtime dependency while keeping the generator
   leaves a generator with no consumer, which is the same trap one layer down.
   **The fork-chain check is not optional:** all four forks still carry both
   dependencies and still import them from `src/api/client.ts` — an
   upstream-owned file none of them has edited, so the next sync replaces it and
   the imports leave with it. Diff the dependency sets **both ways** at that
   sync and treat `vite build` as load-bearing; a dropped dependency has passed
   `tsc` and `eslint` and broken a build before.
5. **`frontend-admin`'s reactive replay** (§7) — **done in this branch
   (batch 2).** Not the one-liner an earlier revision promised: a strict
   `code === "access_token_expired"` gate would switch the console's reactive path
   off in almost every real case, because `prepareHeaders` withholds a locally
   expired bearer and the resulting 401 is codeless. The gate is §4.3's
   **disjunction** — the code, **or** a request sent without a live bearer by the
   console's own expiry predicate. Its hazard is *wider* than the one closed here
   (no token-state gate at all, so every wrong-password 401 is re-sent, not just
   those near expiry), and the fixtures move with it: no existing test emits
   `access_token_expired`, and every `{}`-bodied 401 in the four `baseApi.*.test.ts`
   files becomes a "must NOT refresh" assertion. Its own commits and its own tests
   rather than riding along. N3.
6. **`AccountDsrPage`'s hard-coded English copy** (§4.6) —
   **ruled in for this branch (batch 3)**, and it is larger than the two error
   strings §4.6 noticed while reading the error path. The page has **no
   `useTranslation` import and no `t()` call at all**, so every string on it is
   hard-coded: the heading, the intro, both button labels and both pending
   states, the reason placeholder, the submitted confirmation and the two
   failure messages. The whole page moves behind `t()` under a new top-level
   **`dsr`** key block in `src/locales/en.json` and `it.json` — a *key*, not an
   i18next namespace: this SPA calls `useTranslation()` with no argument and has
   one default namespace, unlike `frontend-admin`'s per-addon namespaces
   (ADR-0007). `locales.test.ts` is a key-parity test, so a one-sided addition
   fails CI: both locale files or neither.
7. **Align `frontend-admin`'s refresh timeout** with §4.1c —
   **ruled in for this branch (batch 3)**, and it is **both halves of the client
   model, not the timer swap alone.** (a) `refreshOnce` bounds its fetch with
   `AbortSignal.timeout(REFRESH_FETCH_TIMEOUT_MS)`; that becomes an
   `AbortController` + `setTimeout`, with `clearTimeout` in a `finally` and nowhere
   else. (b) `fetch` resolves on **headers**, so the timer has to span the body read
   too: the `res.json()` promise is raced against an abort rejection, exactly as
   `tokenStore.attemptRefresh` does. Without (b) a server that sends headers and
   stalls the body holds the cross-tab Web Lock for as long as it stalls — the
   defect v15 found in this spec's own §4.1c, still live in the console. The two
   tests that monkey-patch the global today (`baseApi.proactiveRefresh.test.ts`'s
   "sends the request anyway when /refresh-cookie never answers…" and
   `baseApi.rotationRace.test.ts`'s "does not sign the user out when
   /refresh-cookie never answers") lose their `vi.spyOn(AbortSignal, 'timeout')`
   and their `mockRestore()`, and drive the timeout through a **test-only setter**
   for the constant rather than through fake timers: `performRefresh` schedules its
   own `setTimeout(…, 0)` and every `baseApi.*.test.ts` file drains it on a real
   timer in `afterEach`, so a file-wide `vi.useFakeTimers()` hangs those drains. A
   new case becomes expressible and should be added — headers sent, body stalled,
   and the lock released.
8. **`frontend-admin`'s Web Lock test mock** —
   **ruled in for this branch (batch 3)**, as a hardening rather than a bug fix.
   The call site and the mock are both **two-argument** today, so the test
   passes for the right reason; what is wrong is that it would keep passing for
   the wrong one. Its assertions read only the call count and the lock name, so
   a switch to the three-argument `request(name, { signal }, callback)` overload
   — which #7's sibling concern, bounding the lock, is exactly what would
   motivate — binds the mock's callback parameter to the options object, throws
   inside it, and has that throw swallowed by `performRefresh`'s own `.catch`.
   The mock asserts **arity 2** and that the second argument is a **function**,
   and the eight-line apology comment on `withRefreshLock` goes, replaced by one
   line naming the test that now guards it.
9. **`MintAccessTokenFromRefresh`'s three unclassified wraps** — **done in this
   branch (batch 2)**, and it is the §4.9 amendment above rather than a new
   section: three rows on the site table, the not-found-first split on its user
   lookup, and the tests in §6. It gets a number even though it ships here because
   it never had one — the residual was tracked in prose, in four documents that
   each said "not yet classified" with nothing to strike. **No handler change**:
   `GetSessionHTTP` already routes the mint's error to `writeRefreshErr`, which
   answers 503 `refresh_lookup_unavailable` for the sentinel, and the log outcome
   and cookie-clear allowlist are already right. The prose that flips **with the
   code, in the same commit**: `docs/site/architecture/authentication-flow.mdx`
   (the "not yet classified" clause), `docs/site/modules/core/auth.mdx` (the whole
   paragraph, deletable), `backend/internal/core/auth/CLAUDE.md`'s "Scope,
   precisely" block — including its explicit "do not fix it opportunistically",
   which this amendment is the authorisation to lift — and the `SEVEN sites`
   enumeration in `auth_service.go`'s sentinel comment.
10. **The dev host layout cannot carry the client-tier cookies** — **done in this
    branch (batch 2)**; dev-only, and config-only. `client.localhost` and
    `api.localhost` are **different sites** to Chromium: `localhost` is not in the
    Public Suffix List, so `SchemefulSite` falls back to `scheme://host` (ports are
    irrelevant to site). Every client-tier cookie is minted `SameSite=Lax` with an
    empty `Domain` — the refresh cookie (`password_handler.go:411-424`,
    `utils/http.go:53-64`), the OAuth state cookie (`oauth_state_binding.go:56,71`)
    and the device cookie (`middleware/device.go:61-69`) — so a cross-site
    `fetch(…, {credentials:"include"})` neither stores nor sends them. Measured on
    Chrome 151: after a successful client login the refresh cookie is simply **absent**
    from the jar and `/refresh-cookie` answers 401 "No refresh token provided";
    moving only the API's hostname to `client.localhost` makes it appear and every
    scenario pass. Neither knob helps — `CLIENT_COOKIE_DOMAIN` cannot, because
    SameSite is computed from the request's site and not from the cookie's `Domain`,
    and `COOKIE_SAME_SITE` is read into config and **never consumed** (every mint
    path writes `http.SameSiteLaxMode` literally), so there is no configuration path
    to `SameSite=None` at all. **The fix is same-site by configuration** —
    `CLIENT_API_HOST` / `CLIENT_API_URL` / `VITE_CLIENT_API_BASE` on
    `client.localhost:3000` — plus the docs that prescribe the broken triple. No
    code, no `SameSite=None`. The operator console is unaffected because it calls
    `localhost:3000` from port 8080: same host, different port, same site — but
    only at the shipped entry point (see #13).
11. **The client SPA's route guard redirects before the bootstrap resolves** —
    **done in this branch (batch 2)**. `RequireAuth` is synchronous and has no
    in-flight state, and `AuthProvider` exposes no readiness flag, so on a cold load
    `token` is `null` and the **first render** returns
    `<Navigate to="/login?next=…" replace />` — before the mount effect's
    `refreshAccessToken` has even left. When it lands, nothing navigates back:
    `LoginPage` never reads `isAuthenticated`, so a signed-in user gets a login form
    under a signed-in header, and the only way out is to log in again. Measured on
    the mitigated stack of #10, so the cookie defect is excluded as the cause:
    `/refresh-cookie` and `/me` both answer 200 **after** the redirect. The fix is a
    bootstrap flag in `AuthProvider` that `RequireAuth` waits on, plus `LoginPage`
    honouring `next` for a visitor who is already authenticated. Pre-existing and
    untouched by this branch, and untested for a structural reason: `App.test.tsx`
    enters only at `/auth/callback`, the one route immune to it because
    `OAuthCallbackPage` *awaits* the bootstrap before it navigates — which is why
    295 green tests coexist with it.
12. **The access-token TTL is the admin key, not the env var** — docs only, done in
    this branch (batch 2). The auth module's `ConfigSchema` declares `accessTokenTTL`
    with `Default: "15m"` and **no `EnvVar`**, so first boot seeds `module_configs`
    with `15m` and `GetValue` returns that same schema default whenever the key is
    empty — `AuthPolicyService.AccessTokenTTL` then answers with a positive
    duration and `jwtService` does not fall through to `JWT_ACCESS_TOKEN_EXPIRY`.
    Two states still reach the env level, and an operator can produce neither
    through the admin API: a missing or unreadable `module_configs` document, and a
    **persisted value the parser rejects** — `clampPersistedDuration` returns its
    `0` fallback, which is precisely the "warn, fall through to env" row of
    `auth/CLAUDE.md`'s ADR-0017 D6 table, and reaching it takes an out-of-band
    write because the PATCH validator refuses a malformed value. Observed during
    verification: `JWT_ACCESS_TOKEN_EXPIRY=60s`
    changed nothing until `accessTokenTTL` was PATCHed to `1m`. ADR-0017 D5 repaired
    this chain one layer up and it re-formed one layer down, at the schema default.
    The docs now say which level governs and how to change it; the schema and the
    resolution order are **not** touched — that would be a behaviour change, and this
    is a note about what the code does.
13. **The operator console is same-site only by default** —
    **ruled in for this branch (batch 3)**, as **convention A: `localhost` for both
    the console and its API.**
    #10's fix gave the client tier a dedicated `CLIENT_API_HOST`; the operator tier
    has no equivalent, so the console's origin and `VITE_API_URL` (default
    `http://localhost:3000`, `docker-compose.dev.yml:205`) have to agree by
    convention. They do at the shipped entry point `http://localhost:8080`, and they
    do not the moment anyone opens the same console at `http://console.localhost:8080`
    — every call carries `credentials: 'include'`
    (`frontend-admin/src/store/api/baseApi.ts:295-297`), so
    `POST /v1/auth/operator/refresh-cookie` becomes cross-site and drops the
    `SameSite=Lax` cookie exactly as the client tier did. The operator OAuth recipe
    has the tighter constraint: the `orkestra_oauth_state` cookie is host-only
    (`oauth_state_binding.go:48-58`), so the login-POST host and the callback host
    must match too, which `docs/site/operating/oauth-providers.mdx` and
    `docs/Multi-Environment-Setup.md:489` do not agree on today. Batch-2 wave W4
    recorded the condition in the docs under ruling F8 and changed **no**
    operator-tier config.

    **Why A and not B.** Convention B — `console.localhost` end to end — would need
    seven `.env` keys changed in two files, one of them a key `docker/.env.example`
    does not ship at all, plus the Vite `allowedHosts` list, two compiled backend
    defaults and a resolver entry on every contributor's box: an `.env` migration
    for everyone, which is #16's failure mode one tier over. A needs **no** env key,
    **no** allow-list entry and **no** backend registration — `localhost:3000`
    already reaches the operator mux through the dev fallthrough in
    `cmd/server/hostmux.go`, and `orkestra.sh`'s `wiz_urls` already writes a
    convention-A pairing (`VITE_API_URL` defaults to `BACKEND_URL`, both
    `localhost`). What A needs is four things that are **wrong today**, independently
    of it:

    - `frontend-admin/public/config.example.js`'s `apiUrl` / `wsUrl`, which hard-code
      `console.localhost:3000` while the documented host-side `npm run dev` serves
      the SPA on `localhost` — cross-site out of the box, for the one path the file
      exists to support. Both become `localhost:3000` / `ws://localhost:3000/ws`;
    - `frontend-admin/src/config/environment.ts`'s two code fallbacks, which say
      `console.localhost:3000` while the runtime config compose writes says
      `localhost:3000`. The code default stops disagreeing with the shipped one;
    - the **compiled OAuth redirect defaults** — `internal/shared/config/config.go`'s
      four `OAUTH_*_REDIRECT_URL` fallbacks and the `AllowedRedirectURIs` list in
      `internal/core/auth/utils/redirect_validation.go`. Their host is already
      `localhost:3000`, which is right under A, but their **path is the pre-`/v1`
      one** (`/auth/oauth/{provider}/callback`) and no such route is mounted: the
      handlers register `/v1/auth/oauth/{provider}/callback`. They gain the `/v1`,
      with a test that asserts every compiled default is a path the router actually
      serves — this is the tighter constraint the entry above names, because
      `orkestra_oauth_state` is host-only and `SameSite=Lax`, so the login-POST host
      and the callback host must be the **same host**, not merely the same site;
    - the ~18 documentation lines that prescribe `console.localhost` as the operator
      convention — the rule sentences in `docs/site/getting-started/installation.mdx`,
      `docs/site/architecture/authentication-flow.mdx`, `docker/CLAUDE.md` and
      `docs/site/operating/oauth-providers.mdx`, the OAuth origin/callback recipes in
      that page and in `docs/site/operating/troubleshooting.mdx`, and the table row
      in `docs/Multi-Environment-Setup.md`.

    `CONSOLE_HOST` keeps its `console.localhost:3000` default and simply becomes what
    it already is in practice — a staging/production knob. Nothing stops a
    contributor putting the console on `console.localhost` end to end; A decides only
    what ships, what the docs prescribe, and what the compiled defaults agree with.
14. **A live-bearer codeless 401 strands the console for up to `TTL − 30 s`** —
    **ruled in for this branch (batch 3)**; the contract is the third arm §7
    specifies, down to the two `baseApi.replayGuard.test.ts` assertions that flip
    and the two beside them that must not. §4.3's gate is deliberate and stays:
    a 401 that carries
    no terminal code on a request that *did* go out with a live bearer is a verdict
    from the handler, and replaying it is the defect §7 closed. The residue is the
    one input in that shape which is **not** a verdict — a JWT signing-key rotation
    or a restart with new keys, after which every unexpired bearer validates as
    plain "invalid" and `RequireAuth` answers a codeless 401
    (`backend/internal/shared/middleware/auth.go:198-203`). Against that the console
    now does nothing at all: no refresh, no toast, no sign-out, every request
    failing silently until the *proactive* check fires at
    `expiry − PROACTIVE_REFRESH_SKEW_MS`, i.e. up to `TTL − 30 s` — **≈ 14.5 min**
    at the 15 m default and **30 s** at the `MinAccessTokenTTL` floor of 1 m
    (`auth_duration_bounds.go:30`). A page reload recovers immediately, because
    `/session` mints from the cookie. The mitigation is to run `performRefresh`
    **without** the replay on that input: dispatch the new token and return the
    original 401 untouched. The next request then carries a fresh bearer, so the
    window collapses to one request, and a genuinely dead session reaches the
    sign-out branch instead of failing quietly for a quarter of an hour. Cost: one
    serialised rotation per verdict 401 — a wrong-password attempt would rotate the
    refresh cookie, which is harmless. The adjacent case belongs with it: after a
    proof-(b) refresh, a *replay* that itself 401s still falls through to
    `clearAccessToken` + "Session expired" (`baseApi.ts:550-554` → `:562-563`,
    `:573-582`), which is the same misreading of a verdict as a dead session,
    narrowed but not introduced by this branch — and **not** closed by batch 3's
    arm, which returns before the replay path and never reaches it. It stays named
    here.
15. **`ErrJWTKeysNotLoaded` reaches the browser as a codeless 401** —
    **ruled in for this branch (batch 3)**, in **both** halves: the refresh path is
    three new rows on §4.9's site table, and `RequireAuth` is the second arm §4.10
    now specifies.
    `validateTokenEnhanced` returns the sentinel when no public key is
    loaded (`jwt_service.go:534-536`), and all three service entry points wrap
    *every* validation failure into the same opaque string —
    `RefreshTokensWithRiskAssessment` (`auth_service.go:1461-1464`),
    `PeekRefreshToken` (`:1635-1637`) and `MintAccessTokenFromRefresh`
    (`:1672-1675`). `writeRefreshErr`'s default arm is a 401 with **no** `code`
    (`auth_handler.go:2049-2064`), so `/refresh`, `/refresh-cookie` and `/session`
    all answer a boot misconfiguration the way they answer a dead session. That is
    §4.9's class — infrastructure answered as an auth verdict — with the one
    difference that kept it out of batch 2: this is a boot-time state, not a blip,
    so every session is dead until an operator fixes the keys and no client-side
    retry can help. That is exactly why the answer is a **503** rather than another
    401 code: "the server cannot authenticate anyone" is the true statement, and
    both SPAs already read 503 as transient-and-keep-the-token.

    The refresh half is a three-way split at the one call each of those functions
    opens with, and it needs **no handler change** — `writeRefreshErr` already
    answers `ErrRefreshLookupUnavailable` with 503 `refresh_lookup_unavailable`.
    The `RequireAuth` half is the one with reach, because it is what a browser hits
    first and it covers every protected route on both tiers; §4.10 names its status,
    its code, its single emitter and the envelope it copies. The **signing** sites
    are already classified and are not touched. The test hook — a `breakVerifyingKey`
    twin of the existing `breakSigningKey` — is new, and §4.9 says why the ordering
    inside `validateTokenEnhanced` makes it clean.
16. **Existing dev checkouts need three `.env` keys migrated** — the note is
    **done in this branch (batch 2)**, and the guard is
    **ruled in for this branch (batch 3)**. A `docker/.env` written before #10
    carries `CLIENT_API_HOST=api.localhost`,
    `CLIENT_API_URL=http://api.localhost:3000` and
    `CLIENT_FRONTEND_URL=http://localhost:8081`, and an existing `.env` value beats
    a compose default — so only the SPA moves, because `VITE_CLIENT_API_BASE` is
    not a key `.env.example` ships and therefore takes the new compose default
    `http://client.localhost:3000` while the client mux still listens on
    `api.localhost`. The failure is not a connection error and not the old cookie
    bug: the unmatched Host hits the dev fallthrough (`cmd/server/hostmux.go:86-89`),
    lands on the operator mux, which mounts no `/v1/auth/client/*` routes
    (`auth/module.go:1722-1740`), and every client-tier call answers **404**. The
    three keys to migrate are documented in `docker/CLAUDE.md` → "Client tier: the
    SPA and the client API must be same-site".

    **The guard is two things, because either alone does nothing.** *The rule*, in
    `scripts/env-validate.sh`: with scheme and port stripped,
    `host(CLIENT_API_HOST)`, `host(CLIENT_API_URL)` and `host(CLIENT_FRONTEND_URL)`
    must agree, and `host(VITE_API_URL)` must equal `host(FRONTEND_URL)` — the
    operator twin, which convention A (#13) satisfies on the shipped
    `docker/.env.example` unchanged. Each group is checked only when its keys are
    **set**, in **every** `ENV` rather than under a `development` gate — the same
    constraint is stated for staging and production in `.env.example` itself — and a
    mismatch is an **error**, not a warning, carrying the three-key migration message
    this entry documents. The script never sources `.env`; every read is a `grep`,
    so the rule brings two small helpers of its own: a value extractor in the shape
    of the existing `ENV` read, and a hostname extractor that strips scheme,
    userinfo, path and **port**. The port stripping is load-bearing, not tidiness:
    `.env.example` and the setup wizard write these hosts **bare** while the compose
    defaults write them **ported**, and the host mux tolerates both.
    *The wiring*: today `env-validate.sh` runs from exactly one place —
    `orkestra.sh`'s `init` wizard — so a guard added to the script alone would fire
    only for someone regenerating their `.env`, which is precisely not the person it
    is for. It is wired into the **deploy preflight** as well, ahead of the compose
    up, as a hard stop. And `wiz_urls` gains the write it is missing: it sets
    `CLIENT_API_HOST` and never `CLIENT_API_URL`, so a wizard-generated `.env` has
    no such key at all and leans on the backend deriving one — right today, wrong
    the moment a proxy changes the port or the scheme, and exactly the desync this
    rule would then report.

17. **A service-account lookup answers a store failure as a 404** —
    **ruled in for this branch (batch 3)**. `requireServiceAccount` is the gate
    every `ServiceAccountService` lifecycle method runs first, so that a human
    user's UUID can never be targeted by these endpoints. It collapses **every**
    error from `iface.UserProvider.GetUserByID` into `ErrServiceAccountNotFound`:

    ```go
    user, err := s.users.GetUserByID(ctx, userID)
    if err != nil || user.Kind != iface.UserKindService {
        return nil, ErrServiceAccountNotFound
    }
    ```

    `mapServiceAccountAdminError` answers that sentinel with
    **404 `"service account not found"`**, so a Mongo outage tells an operator their
    service account has been deleted. Four routes gate on it:
    `GET /v1/admin/service-accounts/{id}`,
    `PATCH /v1/admin/service-accounts/{id}`,
    `POST /v1/admin/service-accounts/{id}/credentials` and
    `DELETE /v1/admin/service-accounts/{id}/credentials/{credentialId}`.
    It is §4.9's class exactly — an infrastructure failure reported as a verdict —
    one module over, and it is a follow-up rather than part of §4.9 only because it
    is not on the refresh path and cannot sign anyone out.

    **The contract.** The gate classifies instead of collapsing, and the not-found
    sentinel it classifies against is the SDK's own **`iface.ErrUserNotFound`** —
    added by §4.9, and already what `user/services.ErrUserNotFound` aliases, so it is
    what a conforming `UserProvider` returns for a deleted account:

    ```go
    // beside ErrServiceAccountNotFound in services/service_account_service.go
    ErrServiceAccountLookupUnavailable = errors.New("service account lookup unavailable")

    user, err := s.users.GetUserByID(ctx, userID)
    if err != nil {
        if errors.Is(err, iface.ErrUserNotFound) {
            return nil, ErrServiceAccountNotFound
        }
        return nil, fmt.Errorf("service account lookup failed: %w: %w",
            ErrServiceAccountLookupUnavailable, err)
    }
    if user == nil || user.Kind != iface.UserKindService {
        return nil, ErrServiceAccountNotFound
    }
    ```

    Both wrapped verbs, `%w: %w`, exactly as §4.9's sentinel requires of its own
    sites, so the classification holds and the cause survives for whoever reads the
    log. The `user == nil` test is written out rather than left to `||`'s
    short-circuit: splitting the error arm off moves the `user.Kind` dereference into
    a statement of its own, and a not-found is the right answer for that shape
    anyway.

    **At the handler**, one new arm in `mapServiceAccountAdminError`, ahead of its
    `default`, in the shape this tree already uses for a machine-readable code on a
    `huma.Register`ed route — `user/handlers/avatar_handler.go`'s
    `huma.NewError(status, "<snake_case_code>", &huma.ErrorDetail{Message: …})`,
    where the token lands in `detail`:

    ```go
    case errors.Is(err, services.ErrServiceAccountLookupUnavailable):
        return huma.NewError(http.StatusServiceUnavailable,
            "service_account_lookup_unavailable",
            &huma.ErrorDetail{Message: "the service-account directory could not be read; try again shortly"})
    ```

    Huma's `ErrorModel` has no top-level `code` field, so `detail` is where the token
    goes; the hand-built envelope `writeRefreshErr` and the middleware emitters use
    is not available on a huma route, and giving this family one is not in scope.

    **Tests, one each way.** Positive: `saUserFake.GetUserByID` returning a store
    error makes all four lifecycle methods answer
    `ErrServiceAccountLookupUnavailable`, and `TestMapServiceAccountAdminError` gains
    the `{services.ErrServiceAccountLookupUnavailable, http.StatusServiceUnavailable}`
    row. Negative: an **unknown id** still answers `ErrServiceAccountNotFound` — and
    that case only holds once the fake's `errSAUserNotFound` wraps the sentinel
    (`fmt.Errorf("service account test: user not found: %w", iface.ErrUserNotFound)`).
    Today it is a free-standing `errors.New` whose identity nothing depends on; under
    this split it becomes load-bearing, and leaving it alone would quietly turn every
    unknown-id case into a 503. `TestLifecycleRefusesHumanTargets` seeds a real human
    user, so its lookup *succeeds* and it keeps its 404 either way — which is the
    check that the split did not move the human-target refusal.

    **Named so it is not folded in by mistake:** the Grant path's own user lookup
    collapses the same two conditions into `ErrInvalidClientCredentials` (401). That
    collapse is deliberate — the grant refuses to let a caller learn *why* it was
    rejected — so changing it is a security-shaped decision rather than a
    classification one, and it belongs in its own follow-up.
18. **Cleanups** — **ruled in for this branch (batch 3)**, five of them, each small
    and each already diagnosed:

    **(a) `handleOAuthCallback` and `useHandleOAuthCallbackMutation` are deleted**
    (`frontend-admin/src/store/api/authApi.ts`). Zero consumers — the definition and
    the exported hook name are the only two hits in the repo — and it is *wrong*,
    not merely unused: it POSTs `v1/auth/oauth/{provider}/callback`, which the
    backend mounts as a **GET** for Google, Discord and GitHub, so any caller would
    get a 405. It is the dormant, wrong second implementation §4.8 deleted on the
    client tier, one tier over.

    **(b) `frontend-client/src/components/Layout.tsx` waits for the bootstrap.**
    `isAuthenticated` is `token !== null`, which is `false` for the whole cold-load
    window, so a signed-in user sees "Sign in / Sign up" flash in the header and the
    `enabled: !isAuthenticated` policy query fires a request that is pure waste for
    them — the #11 defect, in the header rather than the route guard. The flag
    already exists: `AuthProvider` exposes `isBootstrapping` and `RequireAuth`
    already consumes it. Three lines — read the flag, add `&& !isBootstrapping` to
    the query gate, and render **nothing in the auth slot** while it is true. Not a
    spinner: the window is one `/refresh-cookie` round-trip and a spinner in a header
    reads as breakage. The logo, the language switcher and the footer stay, so the
    layout does not shift.

    **(c) Three `err.Error() == "user not found"` string compares become
    `errors.Is(err, iface.ErrUserNotFound)`** — twice in
    `handlers/admin_user_auth_handler.go` (the aggregator path and
    `mapAdminInviterError`) and once in `handlers/self_user_auth_handler.go`. They
    match today only because the sentinel's message is literally `"user not found"`
    and `user_service` returns it unwrapped, so the next `fmt.Errorf("...: %w", …)`
    anywhere on the path would silently turn a 404 into a 500. **Paired, in the same
    commit**, with `auth_service.go`'s `SelfLinkOAuthFromCallback`, whose nil-user
    path returns a *fresh* `fmt.Errorf("user not found")` — a different error value
    that `errors.Is` does **not** match — so it must return the sentinel, or a wrap
    of it, or that 404 regresses to a 500 the moment the compare goes. The identical
    `"notifications disabled — cannot send email"` compare beside it has the same
    shape and belongs in the same commit.

    **(d) A `writeCodedError` helper behind the nine hand-built coded envelopes in
    `internal/shared/middleware/auth.go`, byte-identical output.** Nine, not the six
    a first reading suggests and not the eight a second one does:
    `sendSessionRevoked`, `sendAccessTokenExpired`, `sendRiskStepUp`,
    `sendStepUpRequired`, `sendPasswordConfirmRequired`, `sendPolicyUnavailable`,
    `sendMFAEnrollmentRequired`, `sendMFARequired` and
    `sendCapabilityRequiredResponse`. (`sendErrorResponse` is not one of them — it
    routes through `errorManager` and emits **no** top-level `code` at all, which is
    §3.C's whole point and must stay that way.) The invariant core is
    `Content-Type`, `status`, `title`, `detail`, `type: "about:blank"` and `code`;
    the variance is four-dimensional and every axis has to be a parameter.
    **Status**: 401 (×6), 403 (×1), **402** (×1, the capability emitter), 503 (×1).
    **Auth scheme**: `Bearer` (×4), `MFA` (×3), **absent** (×2 —
    `sendPolicyUnavailable` and `sendCapabilityRequiredResponse`). **`errors[]`**:
    present with a per-site `location` and `value` (×8), absent (×1,
    `sendPolicyUnavailable`) — and the absence must be an explicit opt-out, because a
    helper that supplies one is making a **wire change** to that response. **Extra
    top-level fields**: none (×5), `maxAgeSeconds` (×2), `riskScore` +
    `riskThreshold` (×1), `capability` + `tenantId` (×1). `value` cannot be derived
    from `code` — it is `strings.ToUpper(code)` in some cases and something else
    entirely in `sendRiskStepUp` and `sendMFARequired` — so it stays a parameter too.
    §4.10's `sendAccessTokenExpired` and #15's new 503 go behind the same helper; the
    middleware tests that pin each envelope are what keep "byte-identical" honest,
    and none of them may be edited.

    **(e) The refresh-cookie name stops disagreeing three ways.** The compiled
    default (`config.go`'s `COOKIE_NAME_REFRESH` fallback) and **all three** compose
    files say `orkestra_cookie`; `docker/.env.example` says `orkestra_cookie_refresh`
    and `docs/site/operating/cookie-hardening-cross-tier.mdx`'s two sample
    `set-cookie` lines follow `.env.example`. Three of the four sources agree, so the
    two that do not move to **`orkestra_cookie`**. The doc is only exposing a config
    inconsistency; fixing the config is what makes the doc true.

## Open questions — all ruled 2026-09-01 (O6 last, in round 15)

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
  A typed client, if one ever comes back, **delegates to** this helper's policy —
  and batch 3's #4 removed the dependency and the generated stub as well, so it
  would arrive with its own pinned dependency rather than an inherited one.
- **O6 — `access_token_expired` on `RequireAuth`.** ✅ **Ruled 2026-09-01: pull it
  in** (R1). It is **§4.10**. The blast-radius objection that deferred it through
  fourteen rounds was answered by reading the code rather than by re-weighing the
  risk: the branch already exists (`auth.go:218` already compares against
  `services.ErrTokenExpired`), expiry is decided by `jwt.Parse` before every other
  check so no other rejection can acquire the code, and what ships is a split of
  that branch plus an emitter modelled on `sendSessionRevoked`. Nothing about what
  is accepted changes and `RequireAuth` stays bearer-only.

  Two consequences worth stating, because both are easy to get wrong: §4.3 branch
  2 becomes an **OR** of the server's code and the client's own reckoning rather
  than a replacement of one by the other — the recovery must not be dead against a
  backend that has not shipped §4.10 — and §4.5's duration machinery plus the §4.6
  migration are therefore **still required**, not retired.
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
