# Client-tier 401 recovery — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Give `frontend-client` a single, correct authenticated-fetch path that silently recovers from an expired access token, stop a lost rotation race or an infrastructure outage from signing anyone out, and make the backend say *which* of those happened.

**Architecture:** Three backend changes land first and are independently shippable — an SDK not-found sentinel, error **classification** on `/refresh-cookie` (infrastructure failure → 503, not 401), and a distinct `access_token_expired` code on `RequireAuth`'s expired-bearer 401. Then the client: `performRefresh` becomes cross-tab-serialised, 409-aware and time-bounded with an *allowlist* outcome table (only a 401 signs out); the token store records a skew-immune `expiresAt`; and one new `src/api/authedFetch.ts` owns the bearer, the header merge and the 401 branch for every authenticated call. The four hand-rolled wrappers fold into it and the dead, wrong `openapi-fetch` client is deleted.

**Tech Stack:** Go 1.25.13 + Huma v2 (backend); React 19 / TypeScript 5.9 / Vite 7 / TanStack Query v5 / vitest + happy-dom + MSW (frontend-client).

**Spec:** [`docs/superpowers/specs/2026-09-01-client-401-recovery-design.md`](../specs/2026-09-01-client-401-recovery-design.md) — **v19**, status `RULED`. **Read it first** — this plan argues from it and does not restate its reasoning.

**Issue:** [#325](https://github.com/orkestra-cc/orkestra/issues/325). **Related:** ADR-0020, ADR-0017, ADR-0003.

---

## Rulings R1–R3 — what changed between spec v16 and v17

Three decisions were taken on 2026-09-01, after v16 was written. **R1 and R2
are already folded into the spec (v17)** — §4.10 is new, §3.D, §4.3, §4.4,
§4.5, §4.9, §6, §8 and O6 were rewritten around them. They are restated here
because they are what an executor most needs in view, and because R3 shapes
this plan rather than the spec. Where the two ever disagree, **the spec wins**.

### R1 — O6 is ruled **IN**: `access_token_expired` ships with this work (spec §4.10)

Spec §3.D / O6 asked whether to pull `RequireAuth`'s distinct expired-bearer
code into scope. **Ruled: yes.** Task 3 implements it. Consequences:

- **§4.3 branch 2 changes shape.** v16 says: recover only when the sent token
  was *provably expired at send*. It becomes an **OR of two independent
  proofs**, either of which alone shows the handler never ran:
  1. the 401 body carries top-level `code: "access_token_expired"` — the
     server states it rejected the bearer before dispatch (spec §4.10); **or**
  2. `sent.expiresAt !== null && sent.expiresAt <= sentAt` — v16's rule,
     retained as the **fallback** for a backend that has not shipped (1) yet:
     a rolling deploy, a fork on an older base, or the SPA reaching a
     stale API container.

  OR-ing two proofs is a proof, so §4.4's replay guarantee is untouched, and
  (1) additionally recovers the in-flight-expiry case §4.4 explicitly gave up.
  The guard is still a **negative**: no proof → pass the 401 through.
- **§4.5's `expiresAt` machinery survives in full.** It is proof (2), it is
  what makes follow-up #2 possible, and dropping it would leave recovery dead
  against any backend without (1). The §4.6 lifetime migration stays as
  written.
- **§4.4's "what this costs" paragraph shrinks but does not vanish**: the
  in-flight-expiry request is now recovered whenever the server sends the
  code, and only surfaces as one failed request against a backend that does
  not.
- **N3 is unchanged.** `frontend-admin` is *not* fixed here. Task 3 makes
  follow-up #5 a one-line change later; it does not make it.
- **ADR-0020 is still not edited** (user ruling, 2026-09-01). §4.10 refines its
  rejection path rather than reversing any of its decisions — `RequireAuth`
  stays bearer-only, never reads the cookie, never rotates — so the new code is
  documented in `docs/site/architecture/authentication-flow.mdx` and
  `backend/internal/core/auth/CLAUDE.md`.

### R2 — §4.9's user-lookup site needs a sentinel; blanket 503 is wrong (spec §4.9, rewritten)

Spec §4.9 maps all four infrastructure sites to 503 and says "a genuine `nil`
user stays `ErrInvalidRefreshToken` → 401". **Verified against the tree: that
`nil` never happens.** `userService.GetUserByID` returns the sentinel
`services.ErrUserNotFound` for a deleted user
(`internal/core/user/services/user_service.go:592-594`), never `(nil, nil)`.
A blanket 503 would therefore put a genuinely deleted or erased account into a
**permanent 503 loop**: token and marker kept, `isAuthenticated` stays `true`,
every request 401s — precisely defect A's broken state, forever.

`internal/core/auth/services` cannot import the user module (root CLAUDE.md:
"Never import cross-module service/repository packages"), and no shared
not-found sentinel exists today. **Ruled: add one to the SDK** (now written into
spec §4.9). Task 1 adds
`iface.ErrUserNotFound` and aliases the user service's existing sentinel to
it — every existing `return nil, ErrUserNotFound` site becomes
`errors.Is`-classifiable with no other edit and no message change. Task 2 then
splits the user-lookup site: not-found → `ErrInvalidRefreshToken` (401),
anything else → `ErrRefreshLookupUnavailable` (503).

### Plan review round 1 (2026-09-01) — finding #1, blocking, accepted

The reviewer showed that Task 2's four sites are **unreachable from the
browser's `/refresh-cookie` under an outage**: `RefreshTokensHTTP` classifies
every cookie through `PeekRefreshToken` first, `pickRefreshCandidate` discards
any error as an invalid candidate, the handler synthesises
`ErrInvalidRefreshToken`, and `writeRefreshErr` answers 401 — which §4.1's
allowlist turns into a sign-out. Verified line by line (`auth_service.go:1578`,
`auth_handler.go:1015-1018`, `auth_handler.go:1459-1461`). Task 2's tests could
not see it because they drove the service and `writeRefreshErr` directly.
**Task 2 now carries the fifth site, the picker change, the three handler arms,
and HTTP-level tests through the real handlers.** Spec v18 §1 / §4.9 / §6 record
the same.

### Plan review round 1 (2026-09-01) — finding #2, high, accepted

Two more infrastructure reads sit inside the rotation-race classifier, and both
fail **destructively**: `benignRotationRetry` turns a `FamilyRevoked` error into
`false` (`auth_service.go:1718-1723`) and the post-CAS re-read discards its
error (`:1541`); both callers then run `handleRefreshReplay` → `RevokeFamily`.
A Mongo blip during a *legitimate* multi-tab race revokes the family the
winner just renewed — every tab signed out, persisted. The plan's original
`RotateCASLoss_IsNotUnavailable` test consecrated "a lost CAS is never an
outage" without the qualifier that makes it true. **New Task 2b** makes the
classifier's third state explicit — `(benign, err)` — and answers 503 without
revoking whenever the family state could not be read. Spec v19 §1 / §4.9 / §6.

### R3 — one plan, backend tasks first

Spec §7 makes the ordering a hard dependency. This plan is a **single branch**
whose commits land backend-first: Tasks 1–3 are shippable on their own merits
(they stop today's operators being logged out by Mongo blips before any client
change exists), Tasks 4–10 are the client half.

---

## Global Constraints

Every task's requirements implicitly include this section.

- **Branch:** `feat/client-401-recovery` off `dev`. Never commit to `dev`
  directly.
- **Gates** (run from the repo root, **always with `-C`** — a `cd backend &&`
  loop leaves later calls in the wrong directory; shell state does not persist
  between Bash tool calls):
  - `make -C /home/tore/orkestra ci-backend` — Tasks 1, 2, 3, and the final gate.
  - `make -C /home/tore/orkestra ci-frontend-client` — Tasks 4–10.
  - Never run two vitest runs concurrently (`coverage/.tmp` contention). A
    background `ci-backend` alongside a frontend task is safe.
- **Live Mongo for guarded Go tests:**
  `MONGO_TEST_URI='mongodb://127.0.0.1:28017/?directConnection=true'`
  (`directConnection` is required — it is a replica set advertising a docker
  hostname). With it `make ci-backend` runs **0 SKIP**. Live tests must run,
  not skip.
- **Commit trailer.** `export CLAUDE_SESSION=…` **inside every commit block** —
  shell state does not survive between tool calls — and guard it so a commit
  can never carry an empty trailer:
  ```bash
  export CLAUDE_SESSION="https://claude.ai/code/session_01QBHr35WPNoZZ1r2oNY7fDE"
  git commit -m "$(printf '%s\n\n%s\n' "<subject>" "Claude-Session: ${CLAUDE_SESSION:?set CLAUDE_SESSION first}")"
  ```
- **Stage by path.** `git add <explicit paths>`, never `git add -A`. Never
  `--amend`.
- **Doc hygiene is per-commit.** Every commit updates the `CLAUDE.md` /
  docs-site page for the paths it touches, **in the same commit**. The docs
  edits are written into the tasks below; do not defer them to the end.
- **Exact constants** (copy verbatim; do not re-derive):
  - `REFRESH_LOCK_NAME = "orkestra:auth-refresh"`
  - `REFRESH_FETCH_TIMEOUT_MS = 10_000`
  - `TERMINAL_CODES = new Set(["session_revoked", "session_max_age_reached"])`
  - `CODE_ACCESS_TOKEN_EXPIRED = "access_token_expired"`
  - backend code string: `refresh_lookup_unavailable`
  - **There is NO `SKEW` in this design.** Do not add a margin to the
    `expiresAt <= sentAt` comparison; the margin *was* the round-11 replay
    hole. `PROACTIVE_REFRESH_SKEW_MS` belongs to follow-up #2 only.
- **Never `AbortSignal.timeout`** in `tokenStore.ts`. It is not controlled by
  vitest's fake clock (probed: `aborted` stays `false` after advancing 20 s).
  Use `AbortController` + `setTimeout`, and put `clearTimeout` **only** in a
  `finally` that wraps the fetch, the classification *and* the body read.
- **Never spread `init.headers`.** Use `new Headers(init?.headers)` — a
  `Headers` instance spreads to `{}` and silently drops every header.
- **Every 401 body inspection reads `res.clone()`**, never the response a
  caller will get. `readError` swallows the `TypeError` from a consumed body,
  so a direct read degrades *silently* into "no code, fallback message".
- **Fakes must be honest.** A fake that returns a generic error where
  production returns a classifiable sentinel makes the test pass vacuously —
  Task 1 fixes exactly that in `gates_fakes_test.go`.
- **Regression files that must stay green unmodified:** `auth.test.ts`,
  `AuthProvider.test.tsx`, `OAuthCallbackPage.test.tsx`, `LoginPage.test.tsx`,
  `App.test.tsx`. If one needs editing, stop and raise it — that is a signal
  the change is wider than the spec claims.
- **`onUnhandledRequest: 'error'`** is on. Every endpoint a test touches must
  be stubbed, or the run is red even with all assertions passing.
- The pinned prettier (3.1.0) may reformat untouched lines. Accept the churn;
  never `--no-verify` for that.

---

## File Structure

| File | Task | Responsibility |
| ---- | ---- | -------------- |
| `backend/pkg/sdk/iface/interfaces.go` | 1 | **Modify.** Add `ErrUserNotFound` beside the existing `ErrPasswordLoginDisabled` / `ErrAuthPolicyUnavailable` sentinels. |
| `backend/internal/core/user/services/user_service.go` | 1 | **Modify** (line 22). Alias the module sentinel to the SDK one. |
| `backend/internal/core/user/services/user_not_found_sentinel_test.go` | 1 | **Create.** Pins the alias both ways. |
| `backend/internal/core/auth/services/gates_fakes_test.go` | 1 | **Modify.** Make `errNotFound` honest; add a `getByIDErr` injection hook. |
| `backend/internal/core/auth/services/auth_service.go` | 2 | **Modify.** New `ErrRefreshLookupUnavailable`; classify four sites in `RefreshTokensWithRiskAssessment` **and** the lookup in `PeekRefreshToken`. |
| `backend/internal/core/auth/handlers/auth_handler.go` | 2 | **Modify.** `writeRefreshErr` 503 branch; `pickRefreshCandidate` third return; the three cookie-iteration handlers' new arm; `refreshFailureOutcome` arm. |
| `backend/internal/core/auth/handlers/refresh_picker_test.go` | 2 | **Modify.** Third return value; four new cases. |
| `backend/internal/core/auth/handlers/refresh_outage_http_test.go` | 2 | **Create.** HTTP-level: the real handlers answer 503 and never fire replay. |
| `backend/internal/core/auth/services/refresh_infra_classification_test.go` | 2 | **Create.** One case per site + the negatives. |
| `backend/internal/core/auth/services/auth_service.go` | 2b | **Modify.** `benignRotationRetry` → `(bool, error)`; both race callers answer the sentinel before replay. |
| `backend/internal/core/auth/services/refresh_race_outage_test.go` | 2b | **Create.** The benign race under a blip; CAS lost + failed re-read; CAS lost + failed family read; the narrowed negative. |
| `backend/internal/core/auth/handlers/session_cap_response_test.go` | 2 | **Modify.** Add the `writeRefreshErr` branch case. |
| `backend/internal/shared/middleware/auth.go` | 3 | **Modify** (`RequireAuth`, lines 216-228, spec §4.10). Split the expired branch; new `sendAccessTokenExpired`. |
| `backend/internal/shared/middleware/require_auth_test.go` | 3 | **Modify.** Add the code + non-regression cases. |
| `frontend-client/src/lib/jwtExp.ts` | 4 | **Create.** Signature-free `exp` decode; the §4.5 fallback. |
| `frontend-client/src/lib/jwtExp.test.ts` | 4 | **Create.** |
| `frontend-client/src/auth/tokenStore.ts` | 5, 6, 7 | **Modify.** Expiry snapshot (5); lock + 409 + timeout + allowlist (6); `refreshAfterUnauthorized` (7). |
| `frontend-client/src/auth/authContext.ts` | 5 | **Modify.** `signIn` carries the lifetime. |
| `frontend-client/src/auth/AuthProvider.tsx` | 5 | **Modify.** Same signature change. |
| `frontend-client/src/pages/LoginPage.tsx` | 5 | **Modify.** `complete()` takes the result, not the bare token. |
| `frontend-client/src/pages/OAuthCallbackPage.tsx` | 5 | **Modify.** MFA success passes the lifetime. |
| `frontend-client/src/api/auth.ts` | 5, 9 | **Modify.** Drop the `?? 900` fabrication (5); import the shared helper (9). |
| `frontend-client/src/auth/tokenStore.test.ts` | 5, 6, 7 | **Modify.** Additive only — no existing assertion is edited. |
| `frontend-client/src/api/authedFetch.ts` | 8 | **Create.** The one helper: headers, bearer, `credentials`, the 401 branch. |
| `frontend-client/src/api/authedFetch.test.ts` | 8 | **Create.** |
| `frontend-client/src/api/avatar.ts`, `billingProfile.ts`, `dsr.ts` | 9 | **Modify.** Fold the local wrappers into the helper. |
| `frontend-client/src/api/client.ts` | 9 | **Modify.** Delete `api` + both middlewares; keep the base-URL resolver. |
| `frontend-client/CLAUDE.md` | 9, 10 | **Modify.** "How auth works" item 1 + the Refresh-choreography rewrite. |
| `backend/internal/core/auth/CLAUDE.md` | 2, 3 | **Modify.** The two new codes. |
| `docs/site/modules/core/auth.mdx` | 2 | **Modify** (~line 190). `refresh_lookup_unavailable` beside its sibling. |
| `docs/site/architecture/authentication-flow.mdx` | 2, 3, 10 | **Modify.** Rotation paragraph (~149), the expiry section (~226), and the false "Both SPAs implement it" claim. |

---

## Task 1: The `iface.ErrUserNotFound` sentinel (R2)

**Why first:** Task 2 cannot tell a deleted account from an unreachable store
without it, and the existing auth-service test fake lies about which it
returns — so Task 2's tests would pass vacuously against it.

**Files:**
- Modify: `backend/pkg/sdk/iface/interfaces.go` (beside `ErrAuthPolicyUnavailable`, ~line 248)
- Modify: `backend/internal/core/user/services/user_service.go:22`
- Create: `backend/internal/core/user/services/user_not_found_sentinel_test.go`
- Modify: `backend/internal/core/auth/services/gates_fakes_test.go` (~line 70-95, ~line 277-285)

**Interfaces:**
- Produces: `iface.ErrUserNotFound` (an `error` value); `userservices.ErrUserNotFound` is now `errors.Is`-equal to it; `gateUserFake.setGetByIDErr(err error)` for Task 2's tests.

- [ ] **Step 1: Write the failing test**

Create `backend/internal/core/user/services/user_not_found_sentinel_test.go`:

```go
package services

// The user module's not-found sentinel must be classifiable from OUTSIDE the
// module. internal/core/auth/services cannot import this package (root
// CLAUDE.md forbids cross-module service imports), but it must distinguish
// "this account is gone" (a terminal 401 on the refresh path) from "the store
// is unreachable" (a 503). Aliasing the module sentinel to the SDK one is what
// makes errors.Is work across that boundary without an import.

import (
	"errors"
	"testing"

	"github.com/orkestra/backend/pkg/sdk/iface"
)

func TestErrUserNotFound_IsTheSDKSentinel(t *testing.T) {
	if !errors.Is(ErrUserNotFound, iface.ErrUserNotFound) {
		t.Fatalf("errors.Is(ErrUserNotFound, iface.ErrUserNotFound) = false — a consumer outside this module cannot classify a deleted account, and §4.9 would answer 503 forever for one")
	}
}

// A wrapped return — what GetUserByID's siblings produce — must stay
// classifiable, so the seam survives a caller that adds context.
func TestErrUserNotFound_SurvivesWrapping(t *testing.T) {
	wrapped := errors.Join(errors.New("get user by id"), ErrUserNotFound)
	if !errors.Is(wrapped, iface.ErrUserNotFound) {
		t.Fatal("a wrapped not-found stopped matching the SDK sentinel")
	}
}

// The message must not change: it is asserted verbatim by existing callers and
// appears in operator-facing logs.
func TestErrUserNotFound_MessageUnchanged(t *testing.T) {
	if got := ErrUserNotFound.Error(); got != "user not found" {
		t.Fatalf("ErrUserNotFound.Error() = %q, want %q", got, "user not found")
	}
}
```

- [ ] **Step 2: Run it and verify it fails**

```bash
cd /home/tore/orkestra/backend && go test ./internal/core/user/services/ -run TestErrUserNotFound -v
```

Expected: **compile failure** — `undefined: iface.ErrUserNotFound`. That is the
RED signal here; Go reports a missing package-level identifier at build time,
not as a failing assertion.

- [ ] **Step 3: Add the SDK sentinel**

In `backend/pkg/sdk/iface/interfaces.go`, immediately after
`var ErrAuthPolicyUnavailable = errors.New("auth policy unavailable")`:

```go
// ErrUserNotFound is the cross-module "this user does not exist" sentinel.
//
// It exists because a consumer outside the user module — auth's refresh path,
// which must answer 401 for a deleted account and 503 for an unreachable store
// — cannot import internal/core/user/services to compare against its sentinel,
// and MUST NOT: reporting a storage outage as an authentication failure is
// exactly what ADR-0017 gave session enforcement its own 503 to prevent.
// user/services.ErrUserNotFound is an alias of this value, so every existing
// `return nil, ErrUserNotFound` classifies through errors.Is with no other
// change and no change to the message.
var ErrUserNotFound = errors.New("user not found")
```

- [ ] **Step 4: Alias the module sentinel**

In `backend/internal/core/user/services/user_service.go`, replace line 22:

```go
	// Aliased to the SDK sentinel so consumers outside this module (auth's
	// refresh path) can classify it with errors.Is without importing this
	// package. Same value, same message — every existing return site and every
	// `err == ErrUserNotFound` comparison is unaffected.
	ErrUserNotFound = iface.ErrUserNotFound
```

Note the `var (...)` block mixes `=` initialisers already; `iface` is already
imported by this file. Verify with `grep -n '"github.com/orkestra/backend/pkg/sdk/iface"' internal/core/user/services/user_service.go`.

- [ ] **Step 5: Run the test to verify it passes**

```bash
cd /home/tore/orkestra/backend && go test ./internal/core/user/services/ -run TestErrUserNotFound -v
```

Expected: three PASS.

- [ ] **Step 6: Make the auth-service fakes honest**

`gates_fakes_test.go`'s `errNotFound` carries a comment that is about to become
false — "callers don't introspect the specific error type, so a plain error
string is enough". After Task 2 they do. Replace the declaration
(~line 277-285):

```go
// errNotFound mirrors the "not found" sentinel the user service returns when an
// email/uuid is unknown. It WRAPS iface.ErrUserNotFound because callers now
// introspect it: RefreshTokensWithRiskAssessment must answer 401 for a deleted
// account and 503 for an unreachable store, and a fake that returned a bare
// error would make that test pass while production classified it the other way.
var errNotFound = fmt.Errorf("fake user store: %w", iface.ErrUserNotFound)
```

Delete the now-unused `fakeNotFoundErr` type and its `Error()` method. Add
`"fmt"` and `"github.com/orkestra/backend/pkg/sdk/iface"` to the imports if
they are not already there (`iface` almost certainly is — the fake returns
`*iface.User`).

- [ ] **Step 7: Add the infrastructure-error injection hook**

Task 2 needs a `GetUserByID` that fails for a reason that is *not* not-found.
In `gates_fakes_test.go`, add a field to `gateUserFake` and honour it:

```go
// getByIDErr, when set, is returned by GetUserByID instead of consulting the
// seeded map — the "Mongo is unreachable" input. Distinct from errNotFound on
// purpose: the whole point of §4.9 is that those two produce different
// statuses.
func (f *gateUserFake) setGetByIDErr(err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.getByIDErr = err
}
```

Add `getByIDErr error` to the `gateUserFake` struct, and make `GetUserByID`
(~line 70) consult it first:

```go
func (f *gateUserFake) GetUserByID(_ context.Context, id string) (*iface.User, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.getByIDErr != nil {
		return nil, f.getByIDErr
	}
	if u, ok := f.byUUID[id]; ok {
		return u, nil
	}
	return nil, errNotFound
}
```

- [ ] **Step 8: Run the affected packages**

```bash
cd /home/tore/orkestra/backend && go test ./internal/core/user/... ./internal/core/auth/... 2>&1 | tail -30
```

Expected: all PASS. `gates_test.go` and `auth_service_safety_net_test.go` both
lean on `errNotFound`; nothing there introspects it, so they are unaffected —
if one goes red, the fake changed behaviour rather than shape and that must be
understood, not patched over.

- [ ] **Step 9: Full backend gate**

```bash
make -C /home/tore/orkestra ci-backend 2>&1 | tail -25
```

Expected: `Backend CI: OK`. (Run with `MONGO_TEST_URI` exported so nothing
skips.)

- [ ] **Step 10: Commit**

```bash
cd /home/tore/orkestra
export CLAUDE_SESSION="https://claude.ai/code/session_01QBHr35WPNoZZ1r2oNY7fDE"
git add backend/pkg/sdk/iface/interfaces.go \
        backend/internal/core/user/services/user_service.go \
        backend/internal/core/user/services/user_not_found_sentinel_test.go \
        backend/internal/core/auth/services/gates_fakes_test.go
git commit -m "$(printf '%s\n\n%s\n\n%s\n' \
  "feat(sdk): add iface.ErrUserNotFound so auth can classify a deleted account" \
  "The refresh path must answer 401 for an account that is gone and 503 for a store it could not read. It cannot import internal/core/user/services to tell them apart, so the module sentinel is aliased to a new SDK one and every existing return site classifies through errors.Is unchanged. The auth-service fake wraps it too — it previously returned a bare error whose own comment said callers do not introspect it, which stops being true in the next commit." \
  "Claude-Session: ${CLAUDE_SESSION:?set CLAUDE_SESSION first}")"
```

---

## Task 2: Backend §4.9 — infrastructure failures on the refresh path answer 503

**Read spec §4.9 in full, including "The picker in front of the rotation".** The
browser's `/refresh-cookie` never reaches `RefreshTokensWithRiskAssessment` under
an outage; it dies in the picker first. Steps 1-5 fix the four sites inside the
rotation, Steps 6-9 fix the one in front of it, and Steps 10-12 are the
handler-level proof. All one commit: without the picker half, the rest is
unreachable from either SPA.

**Files:**
- Modify: `backend/internal/core/auth/services/auth_service.go` (sentinel block ~line 25-50; `RefreshTokensWithRiskAssessment` ~line 1430-1545; `PeekRefreshToken` ~line 1573-1596)
- Modify: `backend/internal/core/auth/handlers/auth_handler.go` (`writeRefreshErr` ~line 1950; `pickRefreshCandidate` ~line 1003-1030; its three call sites ~line 913, ~line 1134, ~line 1452; `refreshFailureOutcome` ~line 1606)
- Create: `backend/internal/core/auth/services/refresh_infra_classification_test.go`
- Modify: `backend/internal/core/auth/handlers/refresh_picker_test.go`
- Create: `backend/internal/core/auth/handlers/refresh_outage_http_test.go`
- Modify: `backend/internal/core/auth/handlers/session_cap_response_test.go`
- Modify: `backend/internal/core/auth/CLAUDE.md`
- Modify: `docs/site/modules/core/auth.mdx` (~line 190)
- Modify: `docs/site/architecture/authentication-flow.mdx` (~line 149, the rotation paragraph)

**Interfaces:**
- Consumes: `iface.ErrUserNotFound`, `gateUserFake.setGetByIDErr` (Task 1).
- Produces: `services.ErrRefreshLookupUnavailable`; the wire code `refresh_lookup_unavailable` on a 503 from `POST /v1/auth/{tier}/refresh-cookie`.

**Anchor on code, not line numbers** — Task 1 did not move these, but always
re-grep before editing.

- [ ] **Step 1: Write the failing tests**

Create `backend/internal/core/auth/services/refresh_infra_classification_test.go`:

```go
package services

// §4.9 / defect C. Four infrastructure failures on the rotation path were
// wrapped in generic errors that writeRefreshErr answered as a plain, codeless
// 401 — the same answer a genuinely dead refresh token produces. A Mongo blip
// therefore reached the SPA as a sign-out, and no client-side rule could tell
// the two apart. ADR-0017 already decided this question for session
// enforcement and gave it a 503; these are its siblings.
//
// The negatives matter as much as the positives: this must not become a
// blanket 503, or a genuinely dead session never ends.

import (
	"context"
	"errors"
	"testing"
	"time"

	authModels "github.com/orkestra/backend/internal/core/auth/models"
	"github.com/orkestra/backend/internal/core/auth/repository"
	"github.com/orkestra/backend/pkg/sdk/iface"
)

var errStoreDown = errors.New("mongo: no reachable servers")

func TestRefreshInfra_TokenLookupFailure_IsUnavailable(t *testing.T) {
	env := newOrchestrationEnv(t)
	user := seededUser()
	env.users.seed(user)
	raw, _ := env.issueAndSeedRefresh(user, "fam-infra-lookup")
	env.refresh.setGetByTokenAnyErr(errStoreDown)

	_, err := env.auth.RefreshTokensWithRiskAssessment(context.Background(), raw, &authModels.SecurityContext{})
	if !errors.Is(err, ErrRefreshLookupUnavailable) {
		t.Fatalf("err = %v, want ErrRefreshLookupUnavailable — an unreachable store answered as an authentication failure trains clients to discard a valid session", err)
	}
}

func TestRefreshInfra_UserLookupFailure_IsUnavailable(t *testing.T) {
	env := newOrchestrationEnv(t)
	user := seededUser()
	env.users.seed(user)
	raw, _ := env.issueAndSeedRefresh(user, "fam-infra-user")
	env.users.setGetByIDErr(errStoreDown)

	_, err := env.auth.RefreshTokensWithRiskAssessment(context.Background(), raw, &authModels.SecurityContext{})
	if !errors.Is(err, ErrRefreshLookupUnavailable) {
		t.Fatalf("err = %v, want ErrRefreshLookupUnavailable — this is the site whose wording ('user not found') makes an outage read as a deleted account", err)
	}
}

// The negative that keeps the site honest: an account that is genuinely gone
// is terminal, and must stay a 401 or the client never signs out.
func TestRefreshInfra_UserGenuinelyDeleted_IsInvalidToken(t *testing.T) {
	env := newOrchestrationEnv(t)
	user := seededUser()
	env.users.seed(user)
	raw, _ := env.issueAndSeedRefresh(user, "fam-infra-deleted")
	env.users.setGetByIDErr(iface.ErrUserNotFound)

	_, err := env.auth.RefreshTokensWithRiskAssessment(context.Background(), raw, &authModels.SecurityContext{})
	if !errors.Is(err, ErrInvalidRefreshToken) {
		t.Fatalf("err = %v, want ErrInvalidRefreshToken — a deleted account answered 503 leaves the SPA holding a token forever, never signed out", err)
	}
	if errors.Is(err, ErrRefreshLookupUnavailable) {
		t.Fatal("a deleted account must not be reported as an outage")
	}
}

func TestRefreshInfra_MintFailure_IsUnavailable(t *testing.T) {
	env := newOrchestrationEnv(t)
	user := seededUser()
	env.users.seed(user)
	raw, _ := env.issueAndSeedRefresh(user, "fam-infra-mint")
	env.breakSigningKey()

	_, err := env.auth.RefreshTokensWithRiskAssessment(context.Background(), raw, &authModels.SecurityContext{})
	if !errors.Is(err, ErrRefreshLookupUnavailable) {
		t.Fatalf("err = %v, want ErrRefreshLookupUnavailable — a signing/key failure is ours, not the caller's", err)
	}
}

func TestRefreshInfra_RotateFailure_IsUnavailable(t *testing.T) {
	env := newOrchestrationEnv(t)
	user := seededUser()
	env.users.seed(user)
	raw, _ := env.issueAndSeedRefresh(user, "fam-infra-rotate")
	env.refresh.setRotateErr(errStoreDown)

	_, err := env.auth.RefreshTokensWithRiskAssessment(context.Background(), raw, &authModels.SecurityContext{})
	if !errors.Is(err, ErrRefreshLookupUnavailable) {
		t.Fatalf("err = %v, want ErrRefreshLookupUnavailable — a failed write is an outage", err)
	}
}

// A write that fails with the CAS sentinel is NOT an outage — PROVIDED the
// family state could then be read. Here it can: the re-read succeeds and shows
// a row that is not rotated, so the verdict is replay BY STATE, and RevokeFamily
// runs. The qualifier is the point (plan review finding #2): the unqualified
// "a lost CAS is never an outage" is exactly what let a failed family read be
// answered with a revocation. Task 2b holds the cases where the read fails.
func TestRefreshInfra_RotateCASLoss_WithReadableState_IsReplayByState(t *testing.T) {
	env := newOrchestrationEnv(t)
	user := seededUser()
	env.users.seed(user)
	raw, _ := env.issueAndSeedRefresh(user, "fam-infra-cas")
	env.refresh.setRotateErr(repository.ErrTokenAlreadyRotated)

	_, err := env.auth.RefreshTokensWithRiskAssessment(context.Background(), raw, &authModels.SecurityContext{})
	if errors.Is(err, ErrRefreshLookupUnavailable) {
		t.Fatalf("a lost CAS with a READABLE family state was classified as an outage: %v", err)
	}
	if !errors.Is(err, ErrRefreshTokenReplay) {
		t.Fatalf("err = %v, want ErrRefreshTokenReplay — the re-read showed a live row, so the lone presented token is a replay signature", err)
	}
	if env.refresh.revokeFamilyCalled() != 1 {
		t.Fatalf("RevokeFamily calls = %d, want 1 — replay by state must still revoke", env.refresh.revokeFamilyCalled())
	}
}

// The remaining negatives, table-driven: each is a genuinely dead credential
// and each must still be a 401. This is what stops §4.9 from turning into a
// blanket 503.
func TestRefreshInfra_TerminalCasesStay401(t *testing.T) {
	cases := []struct {
		name  string
		setup func(t *testing.T, env *orchestrationEnv) string
	}{
		{"no row at all", func(t *testing.T, env *orchestrationEnv) string {
			user := seededUser()
			env.users.seed(user)
			raw, err := env.jwt.GenerateRefreshToken(user)
			if err != nil {
				t.Fatalf("GenerateRefreshToken: %v", err)
			}
			return raw // never seeded → GetByTokenAny returns (nil, nil)
		}},
		{"expired row", func(t *testing.T, env *orchestrationEnv) string {
			user := seededUser()
			env.users.seed(user)
			raw, _ := env.issueAndSeedRefresh(user, "fam-expired", func(d *authModels.RefreshTokenDoc) {
				d.ExpiresAt = time.Now().Add(-time.Hour)
			})
			return raw
		}},
		{"revoked for logout", func(t *testing.T, env *orchestrationEnv) string {
			user := seededUser()
			env.users.seed(user)
			now := time.Now()
			raw, _ := env.issueAndSeedRefresh(user, "fam-logout", func(d *authModels.RefreshTokenDoc) {
				d.IsRevoked = true
				d.RevokedAt = &now
				d.RevokedReason = "logout"
			})
			return raw
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			env := newOrchestrationEnv(t)
			raw := tc.setup(t, env)
			_, err := env.auth.RefreshTokensWithRiskAssessment(context.Background(), raw, &authModels.SecurityContext{})
			if !errors.Is(err, ErrInvalidRefreshToken) {
				t.Fatalf("err = %v, want ErrInvalidRefreshToken", err)
			}
		})
	}
}
```

- [ ] **Step 2: Run and verify RED**

```bash
cd /home/tore/orkestra/backend && go test ./internal/core/auth/services/ -run TestRefreshInfra -v 2>&1 | tail -30
```

Expected: **compile failure** — `undefined: ErrRefreshLookupUnavailable`,
`env.refresh.setGetByTokenAnyErr undefined`, `env.refresh.setRotateErr
undefined`, `env.breakSigningKey undefined`. Add those fake hooks in Step 3,
then the run must fail on *assertions* before Step 4 makes it pass.

- [ ] **Step 3: Add the fake hooks (they are test-only plumbing)**

In `backend/internal/core/auth/services/gates_fakes_test.go`, add three hooks to
`gateRefreshRepo`. The read hook is an **interceptor keyed on the call number**,
not a bare error field, because Task 2b needs "the first read succeeds, the
re-read fails / returns a changed row" — the read-then-re-read shape *is* the
race, and a fake that fails every call never reaches the CAS:

```go
// onGetByTokenAny, when set, intercepts every GetByTokenAny: it receives the
// 1-based call number and the COPY the map would have returned (possibly nil)
// and decides what the caller sees. Models "the store failed" (Task 2) and
// "the row changed / the store failed BETWEEN the read and the re-read"
// (Task 2b). Deliberately not the same input as "no such row" (nil, nil) or
// repository.ErrTokenAlreadyRotated: §4.9 exists precisely because those
// used to produce one indistinguishable answer.
onGetByTokenAny    func(call int, doc *authModels.RefreshTokenDoc) (*authModels.RefreshTokenDoc, error)
getByTokenAnyCalls int
// rotateErr short-circuits RotateWithFamily with the given error, no mutation.
rotateErr error
// familyRevokedErr makes FamilyRevoked fail — the read the race classifier
// turns a family revocation on.
familyRevokedErr error
// revokeFamilyCalls counts RevokeFamily invocations, so a test can assert the
// classifier did not DECIDE to revoke — activeFamilyMembers alone cannot tell
// "never called" from "called and failed".
revokeFamilyCalls int
```

with setters `setGetByTokenAnyErr(err)` (a convenience:
`onGetByTokenAny = func(int, *Doc) (*Doc, error) { return nil, err }`),
`setOnGetByTokenAny(fn)`, `setRotateErr(err)`, `setFamilyRevokedErr(err)`, and a
getter `revokeFamilyCalled() int`. Wire them in: `GetByTokenAny` increments the
counter and, when the interceptor is set, returns whatever it returns for the
copy it would have produced; `RotateWithFamily` returns `rotateErr` first when
set; `FamilyRevoked` returns `(false, familyRevokedErr)` first when set;
`RevokeFamily` increments the counter before doing anything else (including
before honouring the existing `revokeFamilyErr`).

And in `refresh_orchestration_test.go`, on `orchestrationEnv`:

```go
// breakSigningKey makes GenerateTokenPairWithAMR fail without touching any
// repository, isolating the mint site from the store sites.
func (e *orchestrationEnv) breakSigningKey() {
	e.t.Helper()
	svc, ok := e.jwt.(*jwtService)
	if !ok {
		e.t.Fatalf("breakSigningKey: jwt is %T, not *jwtService", e.jwt)
	}
	svc.privateKey = nil
}
```

> Verify the field name and the failure it produces before writing this:
> `grep -n "privateKey" internal/core/auth/services/jwt_service.go | head`. If
> a nil private key panics rather than erroring, use a key whose `Sign` fails
> instead, or drive the mint failure through the existing
> `ErrJWTKeysNotLoaded` path. Do **not** leave a test that passes because the
> mint site was never reached.

- [ ] **Step 4: Add the sentinel and classify the four sites**

In `backend/internal/core/auth/services/auth_service.go`, in the `var (...)`
block beside `ErrRefreshRotationRaced`:

```go
	// ErrRefreshLookupUnavailable signals that the rotation could not be
	// COMPLETED because infrastructure failed — the token store was
	// unreachable, the user store was unreachable, signing failed, or the
	// rotating write failed. It says nothing about whether the session is
	// alive, so it must never be answered as an authentication failure:
	// ADR-0017 gave session enforcement its own 503 precisely so a storage
	// outage would not "train clients to discard a session that is still
	// perfectly valid", and these are its siblings. Translated to
	// 503 refresh_lookup_unavailable at the handler boundary — a code
	// DISTINCT from session_enforcement_unavailable, because both clients
	// treat 503 identically so the distinction is free on the wire and tells
	// whoever reads the support ticket which subsystem failed.
	ErrRefreshLookupUnavailable = errors.New("refresh path infrastructure unavailable")
```

Then, in `RefreshTokensWithRiskAssessment`:

```go
	// 2. Look up the row — unfiltered so replay detection can see rotated rows.
	hashedToken := utils.HashRefreshToken(refreshToken)
	tokenDoc, err := s.refreshTokenRepo.GetByTokenAny(ctx, hashedToken)
	if err != nil {
		return nil, fmt.Errorf("refresh token lookup failed: %w", ErrRefreshLookupUnavailable)
	}
```

```go
	// Load the user for JWT claim population. A user that is genuinely GONE is
	// terminal — 401, or the client holds a token for a deleted account
	// forever. Anything else is the store being unreachable, and answering
	// that 401 is how an outage acquires the appearance of a deleted account.
	userModel, err := s.userService.GetUserByID(ctx, claims.UserUUID)
	if err != nil {
		if errors.Is(err, iface.ErrUserNotFound) {
			return nil, ErrInvalidRefreshToken
		}
		return nil, fmt.Errorf("user lookup failed: %w", ErrRefreshLookupUnavailable)
	}
	if userModel == nil {
		return nil, ErrInvalidRefreshToken
	}
```

```go
	pair, err := s.jwtService.GenerateTokenPairWithAMR(user, device, security, claims.AMR, claims.LastOTPAt)
	if err != nil {
		return nil, fmt.Errorf("token mint failed: %w", ErrRefreshLookupUnavailable)
	}
```

```go
		return nil, fmt.Errorf("refresh token rotation write failed: %w", ErrRefreshLookupUnavailable)
```

(the last one replaces the trailing `return nil, fmt.Errorf("failed to rotate
refresh token: %w", err)` **after** the `repository.ErrTokenAlreadyRotated`
branch — that branch is untouched.)

`iface` is already imported by `auth_service.go`; confirm with
`grep -n 'pkg/sdk/iface' internal/core/auth/services/auth_service.go`.

- [ ] **Step 5: Run the service tests to verify they pass**

```bash
cd /home/tore/orkestra/backend && go test ./internal/core/auth/services/ -run 'TestRefreshInfra|TestRefreshGrace' -v 2>&1 | tail -40
```

Expected: all PASS, including the four pre-existing `TestRefreshGrace_*` cases
— the rotation-race routing must be untouched.

- [ ] **Step 6: Write the failing Peek + picker tests**

Append to `backend/internal/core/auth/services/refresh_infra_classification_test.go`:

```go
// The FIFTH site (spec v18), and the one that answers the browser: the cookie
// handlers classify every candidate through Peek BEFORE the rotation, so a
// lookup failure here never reaches RefreshTokensWithRiskAssessment at all.
func TestRefreshInfra_PeekLookupFailure_IsUnavailable(t *testing.T) {
	env := newOrchestrationEnv(t)
	user := seededUser()
	env.users.seed(user)
	raw, _ := env.issueAndSeedRefresh(user, "fam-infra-peek")
	env.refresh.setGetByTokenAnyErr(errStoreDown)

	_, err := env.auth.PeekRefreshToken(context.Background(), raw)
	if !errors.Is(err, ErrRefreshLookupUnavailable) {
		t.Fatalf("err = %v, want ErrRefreshLookupUnavailable — the picker treats every other error as 'not a candidate'", err)
	}
}
```

The two existing negatives — `TestPeekRefreshToken_UnknownToken_ReturnsInvalid`
and `TestPeekRefreshToken_MalformedJWT_Rejected` in
`refresh_orchestration_test.go` — must keep passing **unchanged**: an unknown
token is `ErrInvalidRefreshToken`, a malformed JWT is a plain error, and neither
may become the sentinel.

Then in `backend/internal/core/auth/handlers/refresh_picker_test.go`, add the
sentinel to the fixture vocabulary and four cases. The existing `peekRow` fake
already takes an error per row, so nothing structural changes:

```go
var errPeekOutage = fmt.Errorf("mongo down: %w", services.ErrRefreshLookupUnavailable)

// An outage on the ONLY candidate is not "no candidate": it is "could not
// look", and the handler must say so instead of inventing a 401.
func TestPickRefreshCandidate_LookupOutage_OnlyCandidate_ReportsError(t *testing.T) {
	peek := peekerFromTable(map[string]peekRow{"only": {err: errPeekOutage}})
	chosen, fallback, lookupErr := pickRefreshCandidate(context.Background(), peek, []string{"only"})
	if chosen != "" || fallback != "" {
		t.Errorf("chosen=%q fallback=%q, want both empty", chosen, fallback)
	}
	if !errors.Is(lookupErr, services.ErrRefreshLookupUnavailable) {
		t.Fatalf("lookupErr = %v, want ErrRefreshLookupUnavailable", lookupErr)
	}
}

// A valid sibling is proof enough on its own, in either order: the rotation it
// leads to will 503 by itself if the store is really down.
func TestPickRefreshCandidate_LookupOutage_ValidSiblingStillWins(t *testing.T) {
	peek := peekerFromTable(map[string]peekRow{
		"broken":  {err: errPeekOutage},
		"current": {doc: freshDoc()},
	})
	for _, order := range [][]string{{"broken", "current"}, {"current", "broken"}} {
		chosen, fallback, lookupErr := pickRefreshCandidate(context.Background(), peek, order)
		if chosen != "current" || fallback != "" || lookupErr != nil {
			t.Errorf("order %v: chosen=%q fallback=%q err=%v, want current/\"\"/nil", order, chosen, fallback, lookupErr)
		}
	}
}

// THE case that keeps incomplete classification from revoking a family: the
// candidate we could not read may have been the valid successor. No fallback.
func TestPickRefreshCandidate_LookupOutage_SuppressesRotatedFallback(t *testing.T) {
	peek := peekerFromTable(map[string]peekRow{
		"broken":  {err: errPeekOutage},
		"rotated": {doc: rotatedDoc()},
	})
	for _, order := range [][]string{{"broken", "rotated"}, {"rotated", "broken"}} {
		chosen, fallback, lookupErr := pickRefreshCandidate(context.Background(), peek, order)
		if chosen != "" {
			t.Errorf("order %v: chosen=%q, want empty", order, chosen)
		}
		if fallback != "" {
			t.Errorf("order %v: fallback=%q — replay detection would fire on a family whose successor we could not read", order, fallback)
		}
		if !errors.Is(lookupErr, services.ErrRefreshLookupUnavailable) {
			t.Errorf("order %v: lookupErr=%v, want the sentinel", order, lookupErr)
		}
	}
}

// The existing meaning survives: a NON-sentinel error is an invalid candidate,
// skipped silently, and produces no lookupErr.
func TestPickRefreshCandidate_NonSentinelError_StillSkippedSilently(t *testing.T) {
	peek := peekerFromTable(map[string]peekRow{
		"bad-jwt": {err: errors.New("invalid refresh token: signature")},
		"current": {doc: freshDoc()},
	})
	chosen, fallback, lookupErr := pickRefreshCandidate(context.Background(), peek, []string{"bad-jwt", "current"})
	if chosen != "current" || fallback != "" || lookupErr != nil {
		t.Errorf("chosen=%q fallback=%q err=%v, want current/\"\"/nil", chosen, fallback, lookupErr)
	}
}
```

Update the five existing picker tests for the third return value (`chosen,
fallback, _ :=` is not acceptable — assert `lookupErr == nil` in each, or a
regression that starts reporting errors for valid input passes unseen). Add
`"fmt"` and the `services` import.

- [ ] **Step 7: Run and verify RED**

```bash
cd /home/tore/orkestra/backend && go test ./internal/core/auth/services/ -run TestRefreshInfra_PeekLookupFailure -v 2>&1 | tail -8
cd /home/tore/orkestra/backend && go test ./internal/core/auth/handlers/ -run TestPickRefreshCandidate 2>&1 | tail -8
```

Expected: the Peek case FAILS (`want ErrRefreshLookupUnavailable`); the handler
package **fails to compile** (`assignment mismatch: 3 variables but
pickRefreshCandidate returns 2 values`).

- [ ] **Step 8: Classify Peek's lookup and make the picker report**

In `PeekRefreshToken` (`auth_service.go`):

```go
	hashedToken := utils.HashRefreshToken(refreshToken)
	doc, err := s.refreshTokenRepo.GetByTokenAny(ctx, hashedToken)
	if err != nil {
		// An unreachable store is NOT "not a candidate". The picker in front
		// of the rotation reads this sentinel and answers 503; every other
		// error from this function is an invalid candidate and is skipped.
		return nil, fmt.Errorf("refresh token lookup failed: %w", ErrRefreshLookupUnavailable)
	}
```

Leave the JWT-validation wrap, the `nil`-doc `ErrInvalidRefreshToken` and the
error-tolerant user lookup exactly as they are.

Replace the free-function picker in `auth_handler.go`:

```go
// pickRefreshCandidate is the free-function form of the picker, with the Peek
// dependency injected so unit tests don't need a full AuthService.
//
// The third return is what keeps an OUTAGE from being answered as a sign-out.
// Peek used to fail for exactly one reason the picker cared about — "this is
// not a usable candidate" — so every error was skipped. An unreachable store
// fails the same call, and skipping it left the handler with "no candidate"
// and a synthesised 401, which is the one status the client treats as the end
// of the session. So: a Peek error that IS services.ErrRefreshLookupUnavailable
// is recorded and the loop continues (a valid sibling is proof enough on its
// own); at the end, a valid chosen wins regardless, and otherwise a recorded
// lookup failure is returned INSTEAD of the rotated fallback — a candidate we
// could not classify may have been the valid successor, and firing replay
// detection on that family is the PR-D D-9 regression in a new shape.
func pickRefreshCandidate(
	ctx context.Context,
	peek func(context.Context, string) (*models.RefreshTokenDoc, error),
	candidates []string,
) (chosen, fallbackRotated string, lookupErr error) {
	now := time.Now()
	for _, c := range candidates {
		doc, err := peek(ctx, c)
		if err != nil {
			if errors.Is(err, services.ErrRefreshLookupUnavailable) {
				lookupErr = err
			}
			continue
		}
		if doc == nil {
			continue
		}
		if now.After(doc.ExpiresAt) {
			continue
		}
		if doc.IsRevoked {
			if doc.RevokedReason == models.RevokeReasonRotated && fallbackRotated == "" {
				fallbackRotated = c
			}
			continue
		}
		return c, "", nil
	}
	if lookupErr != nil {
		return "", "", lookupErr
	}
	return "", fallbackRotated, nil
}
```

and the method wrapper's signature to match. Then the **three** call sites:

`RefreshTokensHTTP` (~line 1452) and `RefreshTokensWithHeaderHTTP` (~line 913):

```go
		chosen, fallbackRotated, pickErr := h.pickRefreshCandidate(ctx, candidates)
		switch {
		case chosen != "":
			tokenResponse, lastErr = h.authService.RefreshTokensWithRiskAssessment(ctx, chosen, securityCtx)
			if tokenResponse != nil {
				tokenSource = "cookie"
			}
		case pickErr != nil:
			// The store could not classify what the browser sent. Not a
			// sign-out — and NOT the replay fallback, which the picker has
			// already suppressed for this input.
			lastErr = pickErr
		case fallbackRotated != "":
			_, lastErr = h.authService.RefreshTokensWithRiskAssessment(ctx, fallbackRotated, securityCtx)
		default:
			lastErr = services.ErrInvalidRefreshToken
		}
```

(`RefreshTokensWithHeaderHTTP` names its variable `err`, not `lastErr` — keep
its name.) `GetSessionHTTP` (~line 1134) has no fallback arm:

```go
	chosen, _, pickErr := h.pickRefreshCandidate(ctx, candidates)
	var tokenResponse *models.TokenResponse
	var lastErr error
	switch {
	case chosen != "":
		tokenResponse, lastErr = h.authService.MintAccessTokenFromRefresh(ctx, chosen, securityCtx)
	case pickErr != nil:
		lastErr = pickErr
	default:
		lastErr = services.ErrInvalidRefreshToken
	}
```

And `refreshFailureOutcome` (~line 1606) gains an arm beside
`enforcement_unavailable`, or the new 503 is **logged** as `invalid_token` — the
exact misreading this task removes:

```go
	case errors.Is(err, services.ErrRefreshLookupUnavailable):
		return "lookup_unavailable"
```

- [ ] **Step 9: Run the service + picker tests to verify they pass**

```bash
cd /home/tore/orkestra/backend && go test ./internal/core/auth/services/ -run 'TestRefreshInfra|TestRefreshGrace|TestPeekRefreshToken' -v 2>&1 | tail -30
cd /home/tore/orkestra/backend && go test ./internal/core/auth/handlers/ -run TestPickRefreshCandidate -v 2>&1 | tail -20
```

Expected: all PASS, the pre-existing `TestPeekRefreshToken_*` and
`TestPickRefreshCandidate_*` cases included.

- [ ] **Step 10: Write the failing handler tests — `writeRefreshErr` and the real HTTP path**

Append to `backend/internal/core/auth/handlers/session_cap_response_test.go`,
matching the file's existing style:

```go
// §4.9. The refresh-path outage code sits BESIDE session_enforcement_unavailable
// rather than reusing it: both are 503 and every client treats 503 identically,
// so the distinction costs nothing on the wire and buys the thing ADR-0017 D4
// argued for — whoever reads the support ticket can tell which subsystem failed.
func TestWriteRefreshErr_LookupUnavailable_Is503WithDistinctCode(t *testing.T) {
	rec := httptest.NewRecorder()
	writeRefreshErr(rec, services.ErrRefreshLookupUnavailable)

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503 — a 401 here is the Mongo-blip logout this change removes", rec.Code)
	}
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("body is not JSON: %v (%s)", err, rec.Body.String())
	}
	if body["code"] != "refresh_lookup_unavailable" {
		t.Fatalf("code = %v, want refresh_lookup_unavailable", body["code"])
	}
}

// The neighbouring branch must not be swallowed by the new one.
func TestWriteRefreshErr_SessionEnforcementUnavailable_KeepsItsOwnCode(t *testing.T) {
	rec := httptest.NewRecorder()
	writeRefreshErr(rec, services.ErrSessionEnforcementUnavailable)

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", rec.Code)
	}
	var body map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &body)
	if body["code"] != "session_enforcement_unavailable" {
		t.Fatalf("code = %v, want session_enforcement_unavailable — the new branch must sit beside this one, not replace it", body["code"])
	}
}

func TestRefreshFailureOutcome_LookupUnavailable_IsNotInvalidToken(t *testing.T) {
	if got := refreshFailureOutcome(services.ErrRefreshLookupUnavailable); got != "lookup_unavailable" {
		t.Fatalf("refreshFailureOutcome = %q, want lookup_unavailable — logging an outage as invalid_token is the misreading this change removes", got)
	}
}
```

Then create `backend/internal/core/auth/handlers/refresh_outage_http_test.go`.
This is the test the plan-review finding asked for — through the **real**
handlers, with a cookie, with the picker in the path:

```go
package handlers

// Spec v18 / plan review round 1. The four in-rotation sites are unreachable
// from the browser's /refresh-cookie under an outage: RefreshTokensHTTP
// classifies every cookie through PeekRefreshToken FIRST, and a picker that
// swallowed the lookup error left the handler synthesising a 401 — the one
// status the client treats as the end of the session. These tests drive the
// real handlers, because a test that drives the service cannot see the bug.
//
// The fake EMBEDS services.AuthService with a nil value: every method the
// handler is not supposed to reach panics, which is the assertion that
// nothing else was reached.

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/orkestra/backend/internal/core/auth/models"
	"github.com/orkestra/backend/internal/core/auth/services"
	"github.com/orkestra/backend/internal/shared/config"
)

type outagePeekAuthService struct {
	services.AuthService // nil: anything not overridden panics
	peekErr      error
	rotateCalled bool
	mintCalled   bool
}

func (s *outagePeekAuthService) PeekRefreshToken(context.Context, string) (*models.RefreshTokenDoc, error) {
	return nil, s.peekErr
}

// The WRONG answer on purpose: if the replay fallback fires, the test sees a
// 401 refresh_token_replay instead of a silently green run.
func (s *outagePeekAuthService) RefreshTokensWithRiskAssessment(context.Context, string, *models.SecurityContext) (*models.TokenResponse, error) {
	s.rotateCalled = true
	return nil, services.ErrRefreshTokenReplay
}

func (s *outagePeekAuthService) MintAccessTokenFromRefresh(context.Context, string, *models.SecurityContext) (*models.TokenResponse, error) {
	s.mintCalled = true
	return nil, services.ErrInvalidRefreshToken
}

func outageHandler(peekErr error) (*AuthHandler, *outagePeekAuthService) {
	cfg := &config.Config{}
	cfg.Auth.Cookie.Name = logoutTestCookieName
	svc := &outagePeekAuthService{peekErr: peekErr}
	return &AuthHandler{authService: svc, config: cfg}, svc
}

func withCookie(method, path string) *http.Request {
	req := httptest.NewRequest(method, path, nil)
	req.AddCookie(&http.Cookie{Name: logoutTestCookieName, Value: "any-cookie-value"})
	return req
}

func assertOutage503(t *testing.T, rec *httptest.ResponseRecorder) {
	t.Helper()
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503 — this is the Mongo-blip sign-out, reached through the real handler (%s)", rec.Code, rec.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("body is not JSON: %v", err)
	}
	if body["code"] != "refresh_lookup_unavailable" {
		t.Fatalf("code = %v, want refresh_lookup_unavailable", body["code"])
	}
	if sc := rec.Header().Get("Set-Cookie"); sc != "" {
		t.Fatalf("Set-Cookie = %q — the cookie must NOT be cleared on an outage; that would be unrecoverable", sc)
	}
}

var errOutage = fmt.Errorf("mongo: no reachable servers: %w", services.ErrRefreshLookupUnavailable)

func TestRefreshTokensHTTP_CookieLookupOutage_Is503_NeverFiresReplay(t *testing.T) {
	h, svc := outageHandler(errOutage)
	rec := httptest.NewRecorder()
	h.RefreshTokensHTTP(rec, withCookie(http.MethodPost, "/v1/auth/client/refresh-cookie"))
	assertOutage503(t, rec)
	if svc.rotateCalled {
		t.Fatal("the replay fallback fired on a candidate the store could not classify")
	}
}

func TestRefreshTokensWithHeaderHTTP_CookieLookupOutage_Is503(t *testing.T) {
	h, svc := outageHandler(errOutage)
	rec := httptest.NewRecorder()
	h.RefreshTokensWithHeaderHTTP(rec, withCookie(http.MethodPost, "/v1/auth/client/refresh"))
	assertOutage503(t, rec)
	if svc.rotateCalled {
		t.Fatal("the replay fallback fired on a candidate the store could not classify")
	}
}

// The operator console's boot path.
func TestGetSessionHTTP_CookieLookupOutage_Is503(t *testing.T) {
	h, svc := outageHandler(errOutage)
	rec := httptest.NewRecorder()
	h.GetSessionHTTP(rec, withCookie(http.MethodGet, "/v1/auth/operator/session"))
	assertOutage503(t, rec)
	if svc.mintCalled {
		t.Fatal("MintAccessTokenFromRefresh was reached with no classified candidate")
	}
}

// The negative that keeps the picker's existing meaning: a Peek error that is
// NOT the sentinel is an invalid candidate and still answers 401.
func TestRefreshTokensHTTP_CookieInvalid_Still401(t *testing.T) {
	h, svc := outageHandler(fmt.Errorf("invalid refresh token: bad signature"))
	rec := httptest.NewRecorder()
	h.RefreshTokensHTTP(rec, withCookie(http.MethodPost, "/v1/auth/client/refresh-cookie"))
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401 for an invalid candidate", rec.Code)
	}
	if svc.rotateCalled {
		t.Fatal("rotation reached with no valid candidate")
	}
}
```

> If any of the three handlers touches a field of `AuthHandler` this fixture
> leaves nil before it reaches the picker (`jwtService`, `sessionRevocation`),
> the panic will say which; set that field the way `logoutTestHandler` does
> rather than widening the fake. Do **not** stub `RefreshTokensWithRiskAssessment`
> to return success — the point of the wrong answer is that a wrong path is loud.

- [ ] **Step 11: Run and verify RED, then PASS**

```bash
cd /home/tore/orkestra/backend && go test ./internal/core/auth/handlers/ -run 'TestWriteRefreshErr|TestRefreshFailureOutcome|TestRefreshTokensHTTP_|TestRefreshTokensWithHeaderHTTP_|TestGetSessionHTTP_' -v 2>&1 | tail -30
```

If Step 8 is already in place these pass on the first run — that is acceptable
here **only** because Steps 6-7 already produced the RED for the same change at
the picker level. If you want a handler-level RED for the record, temporarily
revert the `case pickErr != nil` arm in `RefreshTokensHTTP` and watch
`TestRefreshTokensHTTP_CookieLookupOutage_Is503_NeverFiresReplay` fail with
`status = 401` **and** `the replay fallback fired` — both halves of the finding.
Then restore it.

- [ ] **Step 12: Add the `writeRefreshErr` branch**

In `writeRefreshErr` (`auth_handler.go`), immediately after the
`ErrSessionEnforcementUnavailable` block:

```go
	if errors.Is(err, services.ErrRefreshLookupUnavailable) {
		// §4.9 / defect C: the rotation could not be COMPLETED because
		// infrastructure failed — inside the rotation, or in the picker that
		// classifies cookies in front of it. Distinct from
		// session_enforcement_unavailable so the failing subsystem is legible
		// in a support ticket; identical on the wire (503) so every existing
		// client already handles it.
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusServiceUnavailable)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"status": http.StatusServiceUnavailable,
			"title":  "Service Unavailable",
			"detail": "the refresh path is temporarily unavailable — please retry",
			"code":   "refresh_lookup_unavailable",
		})
		return
	}
```

Extend the function's doc comment: it currently says "Four outcomes are
deliberately distinct" — make it five and describe the new one.

- [ ] **Step 13: Pin the cookie-clear rule**

`clearRefreshCookieOnTerminalRefreshErr` (`auth_handler.go:1075`) is an
**allowlist** — `ErrSessionMaxAgeReached` and `SessionRevocationDegradedError`
only — so the sentinel never expires the cookie. Verified while planning; the
HTTP tests in Step 10 assert `Set-Cookie` is absent, which pins it. No code
change. If a future edit turns it into a denylist, those tests are what catch
it.

- [ ] **Step 14: Run the handler package**

```bash
cd /home/tore/orkestra/backend && go test ./internal/core/auth/handlers/ 2>&1 | tail -20
```

Expected: PASS.

- [ ] **Step 15: Documentation (same commit)**

**`backend/internal/core/auth/CLAUDE.md`** — in the "three cap outcomes surface
as four distinct HTTP responses" paragraph (~line 448), make it five and add:

> `ErrRefreshLookupUnavailable` is **503** `refresh_lookup_unavailable` — the
> rotation could not be *completed* because infrastructure failed: the token
> store or user store was unreachable, signing failed, the rotating write
> failed, **or the picker in front of the rotation could not classify a cookie
> candidate** (`PeekRefreshToken`'s lookup). Before this, all five were wrapped
> generically and answered as a codeless 401, so a Mongo blip during a refresh
> reached the SPA as the same answer a dead refresh token produces and no
> client-side rule could separate them. The picker case is the one that
> actually answers a browser: the cookie handlers never reach the rotation
> until every candidate is classified, so `pickRefreshCandidate` now returns a
> lookup error as a third value, and on that input the handlers answer 503
> **and suppress the rotated-token replay fallback** — a candidate the store
> could not read may have been the valid successor, and revoking that family
> is the PR-D D-9 regression in a new shape. A user who is genuinely **gone**
> still answers 401 (`errors.Is(err, iface.ErrUserNotFound)`) — the
> distinction is what stops this becoming a blanket 503 that never lets a dead
> session end. The cookie is never expired on this outcome
> (`clearRefreshCookieOnTerminalRefreshErr` is an allowlist).

Also update the `pickRefreshCandidate` doc comment's "Returns:" list in the
code itself (the handler file is its own documentation for that function).

**`docs/site/modules/core/auth.mdx`** — after the existing
`session_enforcement_unavailable` paragraph (~line 190):

> The same argument covers the rotation itself. When the refresh path cannot
> *complete* — a cookie candidate cannot be looked up, the token store or the
> user store is unreachable, signing fails, or the rotating write fails — the
> endpoint answers **503 `refresh_lookup_unavailable`** rather than 401. It is a
> separate code from `session_enforcement_unavailable` only so the failing
> subsystem is legible in a support ticket; both are 503 and every client
> already treats 503 as transient. An account that has genuinely been deleted
> still answers 401 — otherwise a dead session could never end.

**`docs/site/architecture/authentication-flow.mdx`** — in the "Refresh token"
rotation paragraph (~line 149), after the 409 sentence:

> A refresh that fails for an *infrastructure* reason answers **503
> `refresh_lookup_unavailable`**, never 401: reporting an outage as an
> authentication failure is what trains a client to discard a session that is
> still perfectly valid.

- [ ] **Step 16: Full backend gate**

```bash
make -C /home/tore/orkestra ci-backend 2>&1 | tail -25
```

Expected: `Backend CI: OK`. If **errquality** flags the new emitter, fix the
emitter — do **not** add a baseline entry: this change exists to *improve* error
classification, so a complaint from that gate is a real signal.
`openapi-check` should be unchanged (`writeRefreshErr` writes raw JSON outside
the Huma operation shapes, exactly as its 503 sibling already does); if the
dump drifts, run `make -C /home/tore/orkestra/backend openapi-dump` and commit
the result with this task.

- [ ] **Step 17: Commit**

```bash
cd /home/tore/orkestra
export CLAUDE_SESSION="https://claude.ai/code/session_01QBHr35WPNoZZ1r2oNY7fDE"
git add backend/internal/core/auth/services/auth_service.go \
        backend/internal/core/auth/services/refresh_infra_classification_test.go \
        backend/internal/core/auth/services/gates_fakes_test.go \
        backend/internal/core/auth/services/refresh_orchestration_test.go \
        backend/internal/core/auth/handlers/auth_handler.go \
        backend/internal/core/auth/handlers/refresh_picker_test.go \
        backend/internal/core/auth/handlers/refresh_outage_http_test.go \
        backend/internal/core/auth/handlers/session_cap_response_test.go \
        backend/internal/core/auth/CLAUDE.md \
        docs/site/modules/core/auth.mdx \
        docs/site/architecture/authentication-flow.mdx
git commit -m "$(printf '%s\n\n%s\n\n%s\n' \
  "fix(auth): answer 503 refresh_lookup_unavailable when the refresh path cannot complete" \
  "Five infrastructure failures on the refresh path were answered as a codeless 401 — indistinguishable from a dead refresh token, so a Mongo blip logged the user out. Four are inside RefreshTokensWithRiskAssessment; the fifth is PeekRefreshToken's lookup, whose error the cookie picker swallowed as 'not a candidate' so the browser's /refresh-cookie never reached the rotation at all. The picker now reports a lookup failure as a third return, the three cookie handlers answer 503 on it and suppress the rotated-token replay fallback — an unclassifiable candidate may be the valid successor. A genuinely deleted account still answers 401 via iface.ErrUserNotFound. Ships on its own merits: both SPAs already treat 503 as transient." \
  "Claude-Session: ${CLAUDE_SESSION:?set CLAUDE_SESSION first}")"
```

---

## Task 2b: §4.9 (v19) — an unreadable family state answers 503, never a revocation

**Why its own task.** Task 2 changes *status codes*. This changes **when replay
detection fires** — a security decision a reviewer must be able to reject on
its own. Today `benignRotationRetry` folds "could not read the family" into
"the family is revoked" and both callers then revoke it. During a legitimate
multi-tab race that destroys the session the winner just renewed.

**Files:**
- Modify: `backend/internal/core/auth/services/auth_service.go` (`benignRotationRetry` ~line 1711; the two callers ~line 1450-1462 and ~line 1533-1548)
- Create: `backend/internal/core/auth/services/refresh_race_outage_test.go`
- Modify: `backend/internal/core/auth/CLAUDE.md` (the same paragraph Task 2 edited)

**Interfaces:**
- Consumes: `ErrRefreshLookupUnavailable` (Task 2); the fake hooks `setOnGetByTokenAny`, `setRotateErr`, `setFamilyRevokedErr`, `revokeFamilyCalled` (Task 2 Step 3).
- Produces: `benignRotationRetry(ctx, doc) (benign bool, err error)` — the error is the third state.

- [ ] **Step 1: Write the failing tests**

Create `backend/internal/core/auth/services/refresh_race_outage_test.go`:

```go
package services

// Spec §4.9 (v19) / plan review finding #2. The rotation-race classifier has
// three honest answers — healthy family (409), revoked family or window passed
// (replay), or COULD NOT TELL — and the third used to be folded into the
// second, which is the one that mutates. A Mongo blip during a legitimate
// multi-tab race therefore revoked the family the winner had just renewed.
//
// Every positive asserts three things: the sentinel, that the family was NOT
// revoked (active members unchanged AND RevokeFamily never called — the
// second is what proves the classifier did not merely fail to persist a
// verdict it had reached), and that no credentials were issued.

import (
	"context"
	"errors"
	"testing"
	"time"

	authModels "github.com/orkestra/backend/internal/core/auth/models"
	"github.com/orkestra/backend/internal/core/auth/repository"
)

func assertNothingRevoked(t *testing.T, env *orchestrationEnv, family string, wantActive int, resp *authModels.TokenResponse) {
	t.Helper()
	if n := env.refresh.revokeFamilyCalled(); n != 0 {
		t.Fatalf("RevokeFamily called %d times — the classifier acted on a family state it could not read", n)
	}
	if active := env.refresh.activeFamilyMembers(family); active != wantActive {
		t.Fatalf("active family members = %d, want %d — a sibling's successor died for a store error", active, wantActive)
	}
	if resp != nil {
		t.Fatalf("credentials issued on an unclassifiable race: %+v", resp)
	}
}

// THE case: the benign multi-tab race, with the family read failing at the
// instant the loser presents the superseded cookie. This used to sign every
// tab out.
func TestRaceOutage_BenignRace_FamilyReadFails_Is503_NothingRevoked(t *testing.T) {
	env := newOrchestrationEnv(t)
	user := seededUser()
	env.users.seed(user)
	raw, doc := env.issueAndSeedRefresh(user, "fam-race-blip")
	rotateOnce(t, env, raw) // the sibling won; its successor is the 1 active member
	env.refresh.setFamilyRevokedErr(errStoreDown)

	resp, err := env.auth.RefreshTokensWithRiskAssessment(context.Background(), raw, &authModels.SecurityContext{})
	if !errors.Is(err, ErrRefreshLookupUnavailable) {
		t.Fatalf("err = %v, want ErrRefreshLookupUnavailable — 'could not read the family' is not 'the family is revoked'", err)
	}
	assertNothingRevoked(t, env, doc.FamilyID, 1, resp)
}

// CAS lost, and the re-read that would classify it fails. The shape matters:
// the FIRST read must succeed (so the code reaches the CAS at all) and the
// SECOND must fail — a fake that fails every read tests Task 2's site, not
// this one.
func TestRaceOutage_CASLost_ReReadFails_Is503_NothingRevoked(t *testing.T) {
	env := newOrchestrationEnv(t)
	user := seededUser()
	env.users.seed(user)
	raw, doc := env.issueAndSeedRefresh(user, "fam-race-reread")
	env.refresh.setRotateErr(repository.ErrTokenAlreadyRotated)
	env.refresh.setOnGetByTokenAny(func(call int, d *authModels.RefreshTokenDoc) (*authModels.RefreshTokenDoc, error) {
		if call >= 2 {
			return nil, errStoreDown
		}
		return d, nil
	})

	resp, err := env.auth.RefreshTokensWithRiskAssessment(context.Background(), raw, &authModels.SecurityContext{})
	if !errors.Is(err, ErrRefreshLookupUnavailable) {
		t.Fatalf("err = %v, want ErrRefreshLookupUnavailable", err)
	}
	assertNothingRevoked(t, env, doc.FamilyID, 1, resp)
}

// CAS lost, the re-read succeeds and shows the row a sibling rotated inside
// the grace window — a race, by construction — and THEN the family read
// fails. The re-read must return a DIFFERENT row than the first read; that is
// what a race is.
func TestRaceOutage_CASLost_ReReadRotated_FamilyReadFails_Is503_NothingRevoked(t *testing.T) {
	env := newOrchestrationEnv(t)
	user := seededUser()
	env.users.seed(user)
	raw, doc := env.issueAndSeedRefresh(user, "fam-race-family")
	env.refresh.setRotateErr(repository.ErrTokenAlreadyRotated)
	env.refresh.setOnGetByTokenAny(func(call int, d *authModels.RefreshTokenDoc) (*authModels.RefreshTokenDoc, error) {
		if call >= 2 && d != nil {
			now := time.Now()
			d.IsRevoked = true
			d.RevokedAt = &now
			d.RevokedReason = authModels.RevokeReasonRotated
		}
		return d, nil
	})
	env.refresh.setFamilyRevokedErr(errStoreDown)

	resp, err := env.auth.RefreshTokensWithRiskAssessment(context.Background(), raw, &authModels.SecurityContext{})
	if !errors.Is(err, ErrRefreshLookupUnavailable) {
		t.Fatalf("err = %v, want ErrRefreshLookupUnavailable", err)
	}
	assertNothingRevoked(t, env, doc.FamilyID, 1, resp)
}

// The boundary between "could not decide" (503) and "decided, could not
// persist" (401): a verdict that WAS reached is denied even when RevokeFamily
// fails. Pins that Task 2b did not soften genuine replay detection.
func TestRaceOutage_GenuineReplay_RevokeFails_Still401Replay(t *testing.T) {
	env := newOrchestrationEnv(t)
	user := seededUser()
	env.users.seed(user)
	raw, _ := env.issueAndSeedRefresh(user, "fam-race-persist")
	hash := rotateOnce(t, env, raw)
	env.refresh.backdateRevocation(hash, RefreshRotationGrace+time.Second) // outside the window: replay by state
	env.refresh.revokeFamilyErr = errStoreDown

	_, err := env.auth.RefreshTokensWithRiskAssessment(context.Background(), raw, &authModels.SecurityContext{})
	if !errors.Is(err, ErrRefreshTokenReplay) {
		t.Fatalf("err = %v, want ErrRefreshTokenReplay — the verdict was reached; failing to persist it must not downgrade it", err)
	}
	if errors.Is(err, ErrRefreshLookupUnavailable) {
		t.Fatal("a reached replay verdict was reported as an outage")
	}
}
```

`errStoreDown` is declared in Task 2's `refresh_infra_classification_test.go`
(same package). `revokeFamilyErr` is an existing field on the fake.

- [ ] **Step 2: Run and verify RED**

```bash
cd /home/tore/orkestra/backend && go test ./internal/core/auth/services/ -run TestRaceOutage -v 2>&1 | tail -30
```

Expected: the first three FAIL — `err = refresh token replay detected — session
revoked, want ErrRefreshLookupUnavailable`, and where the run gets that far,
`RevokeFamily called 1 times`. The fourth passes already (it is the guard that
must *stay* green). If the first three do not fail with a **replay** error, the
fake hooks are not driving the race sites — stop and check the call numbering
before changing production code.

- [ ] **Step 3: Make the third state explicit**

Replace `benignRotationRetry` in `auth_service.go`:

```go
// benignRotationRetry reports whether a rotated row presented inside the
// grace window belongs to a HEALTHY family — the multi-tab race the 409
// exists for — as opposed to a replay that already tripped detection or a
// rotation that lost to a concurrent family revocation.
//
// The error return is the THIRD state: the family could not be read, so
// neither "retry" nor "replay" is justified. A caller must answer 503 on it
// and must NOT revoke. This used to return false here, with a log line
// saying "treating rotation as replay" — and both callers then revoked the
// family, so a store hiccup during a legitimate two-tab race signed every
// tab out and killed the successor the winner had just minted. Fail closed
// denies the CURRENT request; it does not invent a verdict and persist it.
func (s *authService) benignRotationRetry(ctx context.Context, doc *models.RefreshTokenDoc) (bool, error) {
	if doc == nil || !doc.IsRevoked || doc.RevokedReason != models.RevokeReasonRotated {
		return false, nil
	}
	if doc.RevokedAt == nil || time.Since(*doc.RevokedAt) > RefreshRotationGrace {
		return false, nil
	}
	revoked, err := s.refreshTokenRepo.FamilyRevoked(ctx, doc.FamilyID)
	if err != nil {
		slogDefault().WarnContext(ctx, "refresh: family-state read failed, answering unavailable",
			"outcome", errorOutcome(err))
		return false, fmt.Errorf("family state read failed: %w", ErrRefreshLookupUnavailable)
	}
	return !revoked, nil
}
```

Then the **step 3/4** caller (a row already marked rotated, ~line 1450):

```go
	if tokenDoc.IsRevoked {
		if tokenDoc.RevokedReason == models.RevokeReasonRotated {
			// Three answers, not two. Inside the grace window against a
			// healthy family this is the multi-tab race — "retry". Against
			// a revoked family or outside the window it is a replay. And if
			// the family could not be READ, it is neither: answer 503 and
			// touch nothing, because the sibling that won this race is
			// holding a live successor that a revocation here would kill.
			benign, err := s.benignRotationRetry(ctx, tokenDoc)
			if err != nil {
				return nil, err
			}
			if benign {
				return nil, ErrRefreshRotationRaced
			}
			s.handleRefreshReplay(ctx, tokenDoc, securityCtx, "rotated_token_reused")
			return nil, ErrRefreshTokenReplay
		}
		return nil, ErrInvalidRefreshToken
	}
```

And the **`ErrTokenAlreadyRotated`** branch (~line 1533):

```go
	if err := s.refreshTokenRepo.RotateWithFamily(ctx, hashedToken, newDoc); err != nil {
		if errors.Is(err, repository.ErrTokenAlreadyRotated) {
			// Concurrency: another caller rotated between our Get and our
			// CAS, or the client retried. Re-read the row to tell the two
			// apart — our stale copy still says isRevoked:false. The
			// re-read's OWN failure is an outage, not evidence: answer 503
			// before the family check, and never fire replay on it.
			current, rerr := s.refreshTokenRepo.GetByTokenAny(ctx, hashedToken)
			if rerr != nil {
				return nil, fmt.Errorf("post-CAS re-read failed: %w", ErrRefreshLookupUnavailable)
			}
			benign, berr := s.benignRotationRetry(ctx, current)
			if berr != nil {
				return nil, berr
			}
			if benign {
				return nil, ErrRefreshRotationRaced
			}
			// A sibling that won the CAS within the grace window leaves a
			// healthy family and gets "retry" above; anything else is a
			// replay and the family dies. This also covers our own CAS
			// having succeeded while the family was revoked underneath us
			// (RotateWithFamily reports that as ErrTokenAlreadyRotated too)
			// — FamilyRevoked sees the fence and routes it here.
			s.handleRefreshReplay(ctx, tokenDoc, securityCtx, "rotation_cas_lost")
			return nil, ErrRefreshTokenReplay
		}
		return nil, fmt.Errorf("refresh token rotation write failed: %w", ErrRefreshLookupUnavailable)
	}
```

(the last line is Task 2's; keep it.) `handleRefreshReplay` is **unchanged** —
its `RevokeFamily` error stays logged with the 401 still returned, because by
then the verdict *was* reached.

- [ ] **Step 4: Run to verify PASS — including every existing race test**

```bash
cd /home/tore/orkestra/backend && go test ./internal/core/auth/services/ -run 'TestRaceOutage|TestRefreshGrace|TestRefreshInfra|TestRefreshTokensWithRiskAssessment|TestPeekRefreshToken' -v 2>&1 | tail -40
```

Expected: all PASS. The four `TestRefreshGrace_*` cases are the proof that
replay detection with a *readable* family state is untouched; if any of them
moves, the split is in the wrong place.

- [ ] **Step 5: Documentation (same commit)**

**`backend/internal/core/auth/CLAUDE.md`** — extend the paragraph Task 2 wrote,
after "the picker in front of the rotation could not classify a cookie
candidate":

> …or **the rotation-race classifier could not read the family state**
> (`benignRotationRetry`'s `FamilyRevoked`, or the post-CAS re-read). That one is
> the destructive case: both used to fold "could not read" into "revoked" and run
> `handleRefreshReplay`, so a store hiccup during a legitimate multi-tab race
> revoked the family the winner had just renewed and signed every tab out.
> `benignRotationRetry` now returns `(benign, err)` — three states, not two —
> and on the error both callers answer 503 **without** revoking. A replay verdict
> that *was* reached still answers 401 even if its `RevokeFamily` fails: fail
> closed denies the current request, it does not invent a verdict and persist
> it.

Also update the `benignRotationRetry` doc comment in the code (Step 3 did) and
the function-level comment on `RefreshTokensWithRiskAssessment` whose step 5
still says "A CAS failure means another caller beat us to the rotation — treat
that as replay too" — make it "…re-read to tell a race from a replay; an
unreadable state is neither".

- [ ] **Step 6: Full backend gate and commit**

```bash
make -C /home/tore/orkestra ci-backend 2>&1 | tail -25
cd /home/tore/orkestra
export CLAUDE_SESSION="https://claude.ai/code/session_01QBHr35WPNoZZ1r2oNY7fDE"
git add backend/internal/core/auth/services/auth_service.go \
        backend/internal/core/auth/services/refresh_race_outage_test.go \
        backend/internal/core/auth/CLAUDE.md
git commit -m "$(printf '%s\n\n%s\n\n%s\n' \
  "fix(auth): an unreadable family state during a rotation race answers 503, never a revocation" \
  "benignRotationRetry folded a FamilyRevoked error into 'the family is revoked' and both callers then ran handleRefreshReplay, so a store hiccup during a legitimate multi-tab race revoked the family the winner had just renewed — every tab signed out, persisted. The classifier now has an explicit third state: a failed family read or a failed post-CAS re-read answers refresh_lookup_unavailable and touches nothing. Replay detection with a readable family state is unchanged, and a reached replay verdict still answers 401 even if its revocation fails to persist — fail closed denies the current request; it does not invent a verdict." \
  "Claude-Session: ${CLAUDE_SESSION:?set CLAUDE_SESSION first}")"
```

---

## Task 3: Backend §4.10 (R1/O6) — `access_token_expired` on `RequireAuth`

**Why it is safe to do surgically** (spec §4.10's three bounds): `RequireAuth`
already *has* the branch.
`jwtService.validateTokenEnhanced` returns the unwrapped sentinel
`services.ErrTokenExpired` for an expired token
(`jwt_service.go:544-546`), and `auth.go:218` already compares
`err == services.ErrTokenExpired`. Today it merges that case with
`ErrInvalidToken` into one codeless 401. This task splits the two. **No new
validation, no new lookup, no change to what is accepted** — only what the
rejection says about itself.

Expiry is checked by `jwt.Parse` **before** the audience, type and issuer
checks, so an expired token never reaches those branches and can never be
mislabelled by this one.

Spec §4.10 is the design; §3.D is the argument for it.

**Files:**
- Modify: `backend/internal/shared/middleware/auth.go` (`RequireAuth`, lines 216-229; new emitter beside `sendSessionRevoked` ~line 275)
- Modify: `backend/internal/shared/middleware/require_auth_test.go`
- Modify: `backend/internal/core/auth/CLAUDE.md`
- Modify: `docs/site/architecture/authentication-flow.mdx` (~line 224, "Access-token expiry and refresh")

**Interfaces:**
- Produces: a 401 from `RequireAuth` carrying top-level `"code": "access_token_expired"` and `WWW-Authenticate: Bearer error="access_token_expired"`, consumed by Task 8's branch 2 (proof 1).

- [ ] **Step 1: Write the failing tests**

Append to `backend/internal/shared/middleware/require_auth_test.go`. The
fixture already has everything needed — `mintExpiredAccessToken` (line 834)
even asserts its own precondition against `services.ErrTokenExpired`.

```go
// §3.D. The client is otherwise inferring, from its own reckoning of when the
// token it sent expired, something the server knows for certain. Saying it
// turns frontend-client's 401 branch from an inference into a fact and lets it
// recover a token that expired IN FLIGHT — the one case the client-side rule
// has to give up, because "already expired at send" is the only condition that
// proves the handler never ran.
func TestRequireAuth_ExpiredBearer_ReturnsAccessTokenExpiredCode(t *testing.T) {
	f := newRequireAuthFixture(t)
	dh := &downstreamHandler{}
	srv := httptest.NewServer(f.mw.RequireAuth(dh.handler()))
	defer srv.Close()

	req, _ := http.NewRequest(http.MethodGet, srv.URL+"/protected", nil)
	req.Header.Set("Authorization", "Bearer "+f.mintExpiredAccessToken("u-expired"))
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("Do: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", resp.StatusCode)
	}
	if dh.called {
		t.Error("an expired bearer must NOT reach downstream — that is what makes a client retry safe")
	}
	body, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(body), `"code":"access_token_expired"`) {
		t.Errorf("body = %s, want a top-level code access_token_expired", body)
	}
	if wa := resp.Header.Get("WWW-Authenticate"); wa != `Bearer error="access_token_expired"` {
		t.Errorf("WWW-Authenticate = %q, want access_token_expired", wa)
	}
}

// The bound on the new code: it must mean EXPIRED, nothing else. A client that
// refreshes on it would otherwise refresh for a forged token, and — worse —
// the same code on a wrong-credentials answer would re-arm the replay hazard
// this whole design exists to close.
func TestRequireAuth_NonExpiredRejections_CarryNoExpiredCode(t *testing.T) {
	tok := func(f *requireAuthFixture) string {
		valid := f.issueTokenForUser("u-tamper", "operator")
		return valid[:len(valid)-8] + "AAAAAAAA"
	}
	cases := []struct {
		name   string
		bearer func(f *requireAuthFixture) string
	}{
		{"no bearer at all", func(*requireAuthFixture) string { return "" }},
		{"malformed", func(*requireAuthFixture) string { return "not-a-jwt" }},
		{"tampered signature", tok},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newRequireAuthFixture(t)
			dh := &downstreamHandler{}
			srv := httptest.NewServer(f.mw.RequireAuth(dh.handler()))
			defer srv.Close()

			req, _ := http.NewRequest(http.MethodGet, srv.URL+"/protected", nil)
			if b := tc.bearer(f); b != "" {
				req.Header.Set("Authorization", "Bearer "+b)
			}
			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				t.Fatalf("Do: %v", err)
			}
			defer resp.Body.Close()

			if resp.StatusCode != http.StatusUnauthorized {
				t.Errorf("status = %d, want 401", resp.StatusCode)
			}
			body, _ := io.ReadAll(resp.Body)
			if strings.Contains(string(body), "access_token_expired") {
				t.Errorf("body = %s — only an EXPIRED token may carry that code", body)
			}
		})
	}
}

// A revoked session is terminal and keeps its own code: a token minted from the
// same cookie would carry the same dead sid, so the client must clear rather
// than refresh. The new branch must not shadow it.
func TestRequireAuth_RevokedSession_StillReportsRevokedNotExpired(t *testing.T) {
	f := newRequireAuthFixture(t)
	dh := &downstreamHandler{}
	srv := httptest.NewServer(f.mw.RequireAuth(dh.handler()))
	defer srv.Close()

	tok := f.issueTokenWithSID("u-rev", "sess-rev-not-exp")
	_ = f.revocation.Revoke(context.Background(), "sess-rev-not-exp", "logout")

	req, _ := http.NewRequest(http.MethodGet, srv.URL+"/protected", nil)
	req.Header.Set("Authorization", "Bearer "+tok)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("Do: %v", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(body), `"code":"session_revoked"`) {
		t.Errorf("body = %s, want session_revoked", body)
	}
	if strings.Contains(string(body), "access_token_expired") {
		t.Errorf("body = %s — a revoked session must not invite a refresh", body)
	}
}
```

- [ ] **Step 2: Run and verify RED**

```bash
cd /home/tore/orkestra/backend && go test ./internal/shared/middleware/ -run 'TestRequireAuth_ExpiredBearer_ReturnsAccessTokenExpiredCode|TestRequireAuth_NonExpiredRejections|TestRequireAuth_RevokedSession_StillReports' -v 2>&1 | tail -30
```

Expected: the first FAILS (`want a top-level code access_token_expired`); the
other two PASS already — they are the guards that must *stay* green through
Step 3, not new behaviour.

- [ ] **Step 3: Split the branch**

In `backend/internal/shared/middleware/auth.go`, replace lines 216-229 of
`RequireAuth`:

```go
		claims, err := m.jwtService.ValidateAccessToken(token)
		if err != nil {
			// An EXPIRED token gets its own code. It is the one rejection that
			// tells a client something actionable: the credential was well
			// formed and correctly signed, it simply aged out, and the
			// sanctioned recovery (POST /v1/auth/{tier}/refresh-cookie, then
			// retry — ADR-0020 D2) will work. Every other rejection here says
			// "this token is not ours", where a refresh is at best wasted.
			//
			// It is also the boundary a client needs to retry SAFELY: this
			// branch runs before dispatch, so a request rejected here provably
			// never reached the handler and cannot have consumed anything.
			// Without the code, frontend-client has to infer that from its own
			// reckoning of the token's lifetime and must give up the token
			// that expired in flight.
			if err == services.ErrTokenExpired {
				m.sendAccessTokenExpired(w, r)
				return
			}
			if err == services.ErrInvalidToken {
				m.sendErrorResponse(w, r, errors.AuthenticationError("authentication required").
					WithOperation("require_auth").
					Build())
				return
			}
			m.sendErrorResponse(w, r, errors.TokenInvalidError().
				WithOperation("require_auth").
				WithInternal(err).
				Build())
			return
		}
```

And add the emitter beside `sendSessionRevoked` (~line 275), modelled on it:

```go
// sendAccessTokenExpired emits the 401 for a well-formed, correctly signed
// access token that has simply aged out.
//
// It is deliberately distinct from the generic `authentication required` 401,
// which carries NO top-level code (sendErrorResponse puts appErr.Code in
// errors[0].value, and for an AuthenticationError that value is
// CodeInvalidCredentials — the same value a wrong password produces, so it
// discriminates nothing). A client cannot otherwise tell "your token expired"
// from "your credentials were wrong", and guessing wrong in one direction
// replays a rejected request while guessing wrong in the other leaves a
// working session broken until reload.
//
// RequireAuth stays bearer-only (ADR-0020): this rejects, it does not rotate,
// and it emits no Set-Cookie and no minted token. Recovery remains the
// client's explicit POST to /v1/auth/{tier}/refresh-cookie.
func (m *AuthMiddleware) sendAccessTokenExpired(w http.ResponseWriter, r *http.Request) {
	const code = "access_token_expired"
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("WWW-Authenticate", `Bearer error="`+code+`"`)
	w.WriteHeader(http.StatusUnauthorized)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"status": http.StatusUnauthorized,
		"title":  "access token expired",
		"detail": "the access token has expired; refresh it and retry",
		"type":   "about:blank",
		"errors": []map[string]any{{
			"message":  "access token expired",
			"location": "require_auth",
			"value":    strings.ToUpper(code),
		}},
		"code": code,
	})
}
```

`r` is unused in the body — keep the parameter for symmetry with
`sendSessionRevoked` only if the linter allows it; if `golangci-lint` flags an
unused parameter, drop it and adjust the call site.

- [ ] **Step 4: Run the tests to verify they pass**

```bash
cd /home/tore/orkestra/backend && go test ./internal/shared/middleware/ -v 2>&1 | tail -40
```

Expected: all PASS, **including** the three `_NeverRotates` tests and the two
structural reintroduction guards (`TestAuthMiddleware_Fields_CannotReintroduceCookieRotation`,
`TestAuthGo_ContainsNoCookieRead`). The last two parse `auth.go`'s AST — if
either goes red, the new emitter has strayed into cookie territory and the
change is wrong, not the test.

- [ ] **Step 5: Confirm no other emitter claims the string**

```bash
cd /home/tore/orkestra/backend && grep -rn "access_token_expired" internal/ | grep -v _test.go
```

Expected: exactly one production hit, in `auth.go`. The code must be unique to
this branch or Task 8's proof (1) stops being a proof.

- [ ] **Step 6: Documentation (same commit)**

**`docs/site/architecture/authentication-flow.mdx`**, "Access-token expiry and
refresh" (~line 224) — after the "bearer-only" sentence:

> A rejection that is specifically an **expired** access token carries a
> distinct top-level `code: "access_token_expired"` (and
> `WWW-Authenticate: Bearer error="access_token_expired"`); every other 401
> from the middleware — missing, malformed, forged, wrong audience — does not.
> That is the signal a client uses to decide it is safe to refresh and retry:
> the middleware rejects before dispatch, so a request answered with this code
> provably never reached its handler and cannot have consumed anything. Without
> it a client has to infer the same fact from its own reckoning of the token's
> lifetime, which cannot cover a token that expired in flight and which — if
> the inference is drawn loosely — replays a rejected request such as a
> wrong-current-password `change-password`.

**`backend/internal/core/auth/CLAUDE.md`** — in the block-quote about the codes
`shared/middleware.AuthMiddleware` emits, add `access_token_expired` to the
enumeration with the same one-line rationale, and note that it is the only
non-terminal code on the 401 path that a client should act on by refreshing.

- [ ] **Step 7: Full backend gate**

```bash
make -C /home/tore/orkestra ci-backend 2>&1 | tail -25
```

Expected: `Backend CI: OK`.

- [ ] **Step 8: Commit**

```bash
cd /home/tore/orkestra
export CLAUDE_SESSION="https://claude.ai/code/session_01QBHr35WPNoZZ1r2oNY7fDE"
git add backend/internal/shared/middleware/auth.go \
        backend/internal/shared/middleware/require_auth_test.go \
        backend/internal/core/auth/CLAUDE.md \
        docs/site/architecture/authentication-flow.mdx
git commit -m "$(printf '%s\n\n%s\n\n%s\n' \
  "feat(auth): RequireAuth answers an expired bearer with code access_token_expired" \
  "The middleware already distinguished services.ErrTokenExpired; it merged it into the generic codeless 401 on the way out, so a client could not tell 'your token expired' from 'your credentials were wrong'. It now gets its own code and WWW-Authenticate, which is the proof a client needs that the request never reached its handler and is therefore safe to retry. Nothing about what is ACCEPTED changes, RequireAuth stays bearer-only, and every other rejection keeps the codeless answer it had." \
  "Claude-Session: ${CLAUDE_SESSION:?set CLAUDE_SESSION first}")"
```

---

## Task 4: `src/lib/jwtExp.ts` — the expiry fallback

Pure, dependency-free, and first in the client half because Task 5 imports it.

**Files:**
- Create: `frontend-client/src/lib/jwtExp.ts`
- Create: `frontend-client/src/lib/jwtExp.test.ts`

**Interfaces:**
- Produces: `jwtExp(token: string | null | undefined): number | null` — the expiry in the **`Date.now()` domain (milliseconds)**, or `null` for anything unreadable. Consumed by `tokenStore.setAccessToken` (Task 5).

- [ ] **Step 1: Write the failing tests**

Create `frontend-client/src/lib/jwtExp.test.ts`:

```ts
import { describe, expect, it } from "vitest";

import { jwtExp } from "@/lib/jwtExp";

// Builds a JWT-shaped string whose payload segment is EXACTLY the given JSON.
// Written to take raw JSON text rather than an object because two of the cases
// below cannot survive JSON.stringify: `{"exp":1e400}` parses to Infinity, and
// JSON.stringify(Infinity) emits `null`, which would collapse the case into a
// different one and prove nothing.
const tokenWithPayloadJSON = (json: string): string => {
  const seg = Buffer.from(json, "utf8").toString("base64url");
  return `h.${seg}.s`;
};

describe("jwtExp", () => {
  it("reads exp and returns it in the Date.now() domain (milliseconds)", () => {
    expect(jwtExp(tokenWithPayloadJSON('{"exp":1700000000}'))).toBe(
      1700000000 * 1000,
    );
  });

  it("returns null for a token with no exp", () => {
    expect(jwtExp(tokenWithPayloadJSON('{"sub":"u-1"}'))).toBeNull();
  });

  it.each([
    ["null", null],
    ["empty string", ""],
    ["undefined", undefined],
    ["two segments", "h.e"],
    ["four segments", "h.e.s.x"],
    ["non-base64 payload segment", "h.!!!!.s"],
    ["payload that is valid base64 but not JSON", `h.${Buffer.from("not json").toString("base64url")}.s`],
    ["payload that is JSON but not an object", `h.${Buffer.from("42").toString("base64url")}.s`],
  ])("returns null for %s", (_label, input) => {
    expect(jwtExp(input as string | null | undefined)).toBeNull();
  });

  // The typeof family: everything here has a non-number exp and must read as
  // "unknown", which §4.3 branch 2 then treats as LIVE — an unknown expiry
  // cannot prove the handler never ran.
  it.each([
    ['a string', '{"exp":"1700000000"}'],
    ["null", '{"exp":null}'],
    ["a boolean", '{"exp":true}'],
    ["an object", '{"exp":{"n":1}}'],
    ["an array", '{"exp":[1700000000]}'],
  ])("returns null when exp is %s", (_label, json) => {
    expect(jwtExp(tokenWithPayloadJSON(json))).toBeNull();
  });

  // The two that slip past `typeof exp === "number"`. They are valid JSON,
  // they survive JSON.parse, and their typeof IS "number". An infinite exp
  // would read as a token that never expires, so branch 2 would pass through
  // EVERY 401 forever and the recovery would be silently disabled; -1e400 is
  // the mirror, refreshing on every 401.
  it.each([
    ["1e400 (Infinity)", '{"exp":1e400}'],
    ["-1e400 (-Infinity)", '{"exp":-1e400}'],
  ])("returns null for a non-finite exp: %s", (_label, json) => {
    // The fixture asserts itself: if this ever stops being a non-finite
    // number, the case is testing something else.
    const parsed = JSON.parse(json) as { exp: number };
    expect(typeof parsed.exp).toBe("number");
    expect(Number.isFinite(parsed.exp)).toBe(false);
    expect(jwtExp(tokenWithPayloadJSON(json))).toBeNull();
  });

  // base64url: `-` and `_` are NOT in base64's alphabet and atob throws
  // InvalidCharacterError on them. Most ASCII JSON never produces those
  // characters — a sweep of 2000 candidate payloads found none — so the
  // fixture is CHOSEN and then asserts its own shape, or the case is vacuous.
  it.each([
    ["a payload containing `_`", '{"exp":1700000000,"s":"?"}'],
    ["a payload containing `-`", '{"exp":1700000000,"s":"~~"}'],
  ])("decodes the base64url alphabet: %s", (_label, json) => {
    const seg = Buffer.from(json, "utf8").toString("base64url");
    expect(seg).toMatch(/[-_]/); // the guard that makes this case real
    expect(jwtExp(`h.${seg}.s`)).toBe(1700000000 * 1000);
  });

  // Padding is TOLERATED by atob in both Node and happy-dom (probed at lengths
  // ≡ 1, 2 and 3 mod 4), so these pass even against a naive implementation.
  // They pin the behaviour against a stricter runtime — the WHATWG
  // forgiving-base64 algorithm does specify failure at length ≡ 1 mod 4 — and
  // a green run here is NOT evidence that the alphabet is handled. The two
  // cases above are.
  it.each([
    ['{"exp":1700000000}'],
    ['{"exp":1700000000,"s":"a"}'],
    ['{"exp":1700000000,"s":"ab"}'],
  ])("tolerates a stripped-padding segment: %s", (json) => {
    const seg = Buffer.from(json, "utf8").toString("base64").replace(/=+$/, "");
    expect(jwtExp(`h.${seg}.s`)).toBe(1700000000 * 1000);
  });
});
```

- [ ] **Step 2: Run and verify RED**

```bash
npx vitest run src/lib/jwtExp.test.ts 2>&1 | tail -25
```

Expected: every case fails. Note the shape: vitest/esbuild resolves a missing
module export to `undefined`, so RED reads `TypeError: jwtExp is not a
function`, **not** a module-load `SyntaxError`.

- [ ] **Step 3: Write the implementation**

Create `frontend-client/src/lib/jwtExp.ts`:

```ts
// Reads a JWT's `exp` claim WITHOUT verifying the signature.
//
// This is a scheduling hint and never a security decision — the backend
// remains the only authority on whether a token is valid. It exists purely as
// the FALLBACK for §4.5: the expiry the store actually reckons with is derived
// from the `expiresIn` DURATION the server reported at receipt, because both
// ends of that comparison then come from the same clock and a constant offset
// cancels. Comparing a server-issued absolute `exp` against the browser's wall
// clock is only as accurate as the difference between the two, and a badly set
// clock reopens that window every TTL cycle. This function is reached only when
// a response carried no `expiresIn` at all.
//
// Returns the expiry in the Date.now() domain (milliseconds), or null for
// anything unreadable. A null expiry means UNKNOWN, and §4.3 branch 2 treats
// unknown as LIVE: an unknown expiry cannot prove the request never reached
// its handler, and under a rule whose failure mode is a REPLAY rather than a
// wasted refresh, "don't know" has to fall on the safe side.
export function jwtExp(token: string | null | undefined): number | null {
  if (!token) return null;
  const parts = token.split(".");
  if (parts.length !== 3) return null;

  let json: string;
  try {
    // atob throws InvalidCharacterError on `-` and `_`: they are the base64URL
    // alphabet's substitutions for `+` and `/` and are not valid base64. This
    // is the part that actually breaks. Re-padding to a multiple of 4 is
    // belt-and-braces — atob tolerates missing padding in both Node and
    // happy-dom today — but the WHATWG forgiving-base64 algorithm specifies
    // failure at length ≡ 1 mod 4, so a stricter runtime is one upgrade away.
    const b64 = parts[1].replace(/-/g, "+").replace(/_/g, "/");
    json = atob(b64 + "=".repeat((4 - (b64.length % 4)) % 4));
  } catch {
    return null;
  }

  let payload: unknown;
  try {
    payload = JSON.parse(json);
  } catch {
    return null;
  }
  if (typeof payload !== "object" || payload === null) return null;

  const exp = (payload as { exp?: unknown }).exp;
  // Number.isFinite, NOT `typeof exp === "number"`. `{"exp":1e400}` is entirely
  // valid JSON, parses to Infinity, and its typeof IS "number" — an infinite
  // exp would read as a token that never expires and would silently disable
  // the 401 recovery for the life of the tab. `-1e400` is the mirror case.
  if (!Number.isFinite(exp)) return null;
  return (exp as number) * 1000;
}
```

- [ ] **Step 4: Run the tests to verify they pass**

```bash
cd /home/tore/orkestra/frontend-client && npx vitest run src/lib/jwtExp.test.ts 2>&1 | tail -15
```

Expected: PASS, ~24 cases.

- [ ] **Step 5: Gate**

```bash
make -C /home/tore/orkestra ci-frontend-client 2>&1 | tail -20
```

Expected: `Frontend-client CI: OK`.

- [ ] **Step 6: Commit**

```bash
cd /home/tore/orkestra
export CLAUDE_SESSION="https://claude.ai/code/session_01QBHr35WPNoZZ1r2oNY7fDE"
git add frontend-client/src/lib/jwtExp.ts frontend-client/src/lib/jwtExp.test.ts
git commit -m "$(printf '%s\n\n%s\n\n%s\n' \
  "feat(client): add jwtExp, the signature-free expiry fallback" \
  "Used only when a token arrives without an expiresIn duration; the store's primary reckoning is duration-based and immune to clock skew. Number.isFinite rather than typeof: {\"exp\":1e400} is valid JSON whose typeof is \"number\", and an infinite expiry would silently disable the 401 recovery. The base64url alphabet is converted before atob, which throws on - and _; the padding cases are pinned against a stricter runtime, not against today's." \
  "Claude-Session: ${CLAUDE_SESSION:?set CLAUDE_SESSION first}")"
```

---

## Task 5: §4.5 + §4.6 — the token store records a skew-immune `expiresAt`, and every install supplies it

**The trap this task exists for:** a dropped lifetime fails **silently**. The
token still installs and everything works; only the 401 path misbehaves, hours
later, by reading an unknown expiry as "live" and never recovering. So every
one of the six call sites gets its own test, plus an end-to-end guard that
fails if any single one is missed.

**Files:**
- Modify: `frontend-client/src/auth/tokenStore.ts`
- Modify: `frontend-client/src/auth/authContext.ts`
- Modify: `frontend-client/src/auth/AuthProvider.tsx`
- Modify: `frontend-client/src/pages/LoginPage.tsx`
- Modify: `frontend-client/src/pages/OAuthCallbackPage.tsx`
- Modify: `frontend-client/src/api/auth.ts` (the `?? 900` fabrication only)
- Modify: `frontend-client/src/auth/tokenStore.test.ts` (additive)
- Modify: `frontend-client/CLAUDE.md` ("How auth works" item 1)

**Interfaces:**
- Consumes: `jwtExp` (Task 4).
- Produces:
  - `setAccessToken(token: string | null, expiresInSeconds?: number): void`
  - `getAccessTokenSnapshot(): { token: string | null; expiresAt: number | null }`
  - `AuthState.signIn: (token: string, expiresInSeconds?: number) => void`
  - `LoginResult['expiresIn']` becomes `number | undefined` (was `number`)

- [ ] **Step 1: Write the failing store tests**

Append to `frontend-client/src/auth/tokenStore.test.ts`:

```ts
describe("access-token expiry reckoning (§4.5)", () => {
  it("records expiresAt from the reported duration, in the local clock domain", () => {
    vi.useFakeTimers();
    try {
      vi.setSystemTime(new Date("2026-09-01T12:00:00.000Z"));
      setAccessToken("at-1", 900);
      expect(getAccessTokenSnapshot()).toEqual({
        token: "at-1",
        expiresAt: Date.now() + 900_000,
      });
    } finally {
      vi.useRealTimers();
    }
  });

  // The reason the duration is used at all. Both ends of the comparison are
  // taken from the same clock, so a constant offset cancels: a client whose
  // clock is hours off still reads a token installed 60s ago, with a 900s
  // life, as live. A Date.now()-vs-`exp` implementation fails this.
  it("is immune to a wall-clock offset between install and read", () => {
    vi.useFakeTimers();
    try {
      vi.setSystemTime(new Date("2026-09-01T12:00:00.000Z"));
      setAccessToken("at-skew", 900);
      const at = getAccessTokenSnapshot().expiresAt!;
      // The clock jumps hours forward AND the elapsed time is only 60s: this
      // is the shape a badly set clock produces on the NEXT read.
      vi.advanceTimersByTime(60_000);
      expect(at).toBeGreaterThan(Date.now());
    } finally {
      vi.useRealTimers();
    }
  });

  it("falls back to the JWT exp when no duration is supplied", () => {
    const seg = Buffer.from('{"exp":1700000000}', "utf8").toString("base64url");
    setAccessToken(`h.${seg}.s`);
    expect(getAccessTokenSnapshot().expiresAt).toBe(1700000000 * 1000);
  });

  it("records an UNKNOWN expiry when neither is available", () => {
    setAccessToken("opaque-not-a-jwt");
    expect(getAccessTokenSnapshot()).toEqual({
      token: "opaque-not-a-jwt",
      expiresAt: null,
    });
  });

  it("clearing the token clears the expiry too", () => {
    setAccessToken("at-1", 900);
    clearAccessToken();
    expect(getAccessTokenSnapshot()).toEqual({ token: null, expiresAt: null });
  });

  it("a refresh installs the pair from the response body", async () => {
    vi.useFakeTimers();
    try {
      setSessionMarker();
      server.use(
        http.post(REFRESH, () =>
          HttpResponse.json({ accessToken: "at-r", expiresIn: 120 }),
        ),
      );
      await refreshAccessToken(API);
      expect(getAccessTokenSnapshot()).toEqual({
        token: "at-r",
        expiresAt: Date.now() + 120_000,
      });
    } finally {
      vi.useRealTimers();
    }
  });

  // The deliberate divergence from frontend-admin, which treats a response
  // without expiresIn as a FAILED refresh. Turning a valid rotation into a
  // sign-out over a missing optional field is the wrong trade.
  it("a refresh WITHOUT expiresIn still installs the token", async () => {
    setSessionMarker();
    server.use(
      http.post(REFRESH, () => HttpResponse.json({ accessToken: "at-noexp" })),
    );
    await expect(refreshAccessToken(API)).resolves.toEqual({
      status: "ok",
      accessToken: "at-noexp",
    });
    expect(getAccessToken()).toBe("at-noexp");
  });
});
```

Add `clearAccessToken`, `getAccessTokenSnapshot` to the file's import from
`@/auth/tokenStore`.

- [ ] **Step 2: Run and verify RED**

```bash
npx vitest run src/auth/tokenStore.test.ts 2>&1 | tail -25
```

Expected: the new block fails (`getAccessTokenSnapshot is not a function`). The
**existing** 198 lines must still pass — they call `setAccessToken(token)` with
one argument, which stays valid.

- [ ] **Step 3: Implement the store change**

In `frontend-client/src/auth/tokenStore.ts`, replace the token state block:

```ts
import { jwtExp } from "@/lib/jwtExp";

let accessToken: string | null = null;
// The moment the current token expires, in the Date.now() domain — recorded
// from the DURATION the server reported at receipt, not from the token's
// absolute `exp`. Both ends of the eventual comparison then come from the same
// clock, so a constant offset cancels and clock skew stops being a failure
// mode rather than merely being tolerated (§4.5). `null` means UNKNOWN, which
// §4.3 branch 2 treats as LIVE.
let accessTokenExpiresAt: number | null = null;
const subscribers = new Set<(token: string | null) => void>();

export function getAccessToken(): string | null {
  return accessToken;
}

// The token and what we know about its life are ONE fact and must be read
// together: a 401 handler that read them separately could compare a token it
// sent against an expiry a sibling request installed moments later.
export function getAccessTokenSnapshot(): {
  token: string | null;
  expiresAt: number | null;
} {
  return { token: accessToken, expiresAt: accessTokenExpiresAt };
}

// expiresInSeconds is the server's own figure (LoginResult.expiresIn,
// MfaLoginVerifyResult.expiresIn, the refresh body). It is OPTIONAL, and an
// absent one is not an error: the JWT's own `exp` is tried next, and an
// unreadable one leaves the expiry unknown.
export function setAccessToken(
  token: string | null,
  expiresInSeconds?: number,
): void {
  accessToken = token;
  accessTokenExpiresAt = resolveExpiresAt(token, expiresInSeconds);
  for (const fn of subscribers) fn(token);
}

function resolveExpiresAt(
  token: string | null,
  expiresInSeconds?: number,
): number | null {
  if (token === null) return null;
  // Number.isFinite guards the same hazard jwtExp does: a body carrying
  // `expiresIn: 1e400` would otherwise record a token that never expires.
  if (typeof expiresInSeconds === "number" && Number.isFinite(expiresInSeconds)) {
    return Date.now() + expiresInSeconds * 1000;
  }
  return jwtExp(token);
}
```

`clearAccessToken` is unchanged in source (`setAccessToken(null)`) and now
clears both fields through `resolveExpiresAt`'s `null` branch.

In `performRefresh`, pass the duration through — read `expiresIn` off the body
alongside the token:

```ts
      const body = (await res.json().catch(() => ({}))) as {
        accessToken?: string;
        token?: string;
        expiresIn?: number;
      };
      const fresh = body.accessToken ?? body.token ?? null;
      if (!fresh) return signedOut();
      setAccessToken(fresh, body.expiresIn);
```

(Task 6 restructures this function; the change survives that restructure — do
not skip it here on the grounds that it is about to move.)

- [ ] **Step 4: Run the store tests to verify they pass**

```bash
cd /home/tore/orkestra/frontend-client && npx vitest run src/auth/tokenStore.test.ts 2>&1 | tail -15
```

Expected: PASS, existing cases included.

- [ ] **Step 5: Write the failing propagation tests**

The store is correct; nothing feeds it yet. Append to
`frontend-client/src/pages/LoginPage.test.tsx` and
`frontend-client/src/pages/OAuthCallbackPage.test.tsx` — **additive cases
only**; do not edit an existing assertion.

In `LoginPage.test.tsx`:

```ts
  it("records the token lifetime the login response carried (§4.6)", async () => {
    vi.useFakeTimers({ shouldAdvanceTime: true });
    try {
      server.use(
        clientPolicyHandler(),
        providersHandler([]),
        http.post(url("/v1/auth/client/login"), () =>
          HttpResponse.json({
            success: true,
            accessToken: "at-login",
            tokenType: "Bearer",
            expiresIn: 300,
          }),
        ),
      );
      const { queryClient } = renderWithProviders(<LoginPage />);
      await waitForQuerySettled(queryClient, ["authPolicy"]);
      await userEvent.type(screen.getByLabelText(/email/i), "a@b.c");
      await userEvent.type(screen.getByLabelText(/password/i), "hunter2hunter2");
      await userEvent.click(screen.getByRole("button", { name: /sign in/i }));

      await waitFor(() =>
        expect(getAccessTokenSnapshot().token).toBe("at-login"),
      );
      // Not null (the lifetime was dropped) and not the fabricated 900.
      expect(getAccessTokenSnapshot().expiresAt).toBe(Date.now() + 300_000);
    } finally {
      vi.useRealTimers();
    }
  });

  it("records the lifetime from the MFA challenge path too", async () => {
    // Same shape: /login answers requiresMfa, then
    // /v1/auth/client/mfa/login/verify answers {accessToken, expiresIn: 240}.
    // Assert getAccessTokenSnapshot().expiresAt === Date.now() + 240_000.
    // This is the second of the two `signIn` call sites that drop it today,
    // so it needs its own case — the password case passes without it.
  });

  it("records an UNKNOWN expiry when the login response omits expiresIn", async () => {
    // Same as the first case but the body has no expiresIn.
    // expect(getAccessTokenSnapshot().expiresAt).toBeNull();
    // This is the `?? 900` fix: a fabricated 900 here would make §4.3 branch 2
    // read every 401 as "not a token problem" for a quarter of an hour on a
    // deployment running a 60s TTL.
  });
```

> The second and third cases are written out as comments **deliberately**: fill
> them in from the first case's shape and from the existing MFA-path test
> already in this file (`grep -n "requiresMfa" src/pages/LoginPage.test.tsx`),
> which supplies the exact handler bodies and the challenge-form interaction.
> Do not invent a new interaction — reuse that test's.

In `OAuthCallbackPage.test.tsx`, add the mirror case for the MFA-success
`signIn` at line 117: assert `getAccessTokenSnapshot().expiresAt` is
`Date.now() + <expiresIn>*1000` after the challenge completes.

And the end-to-end guard, in `src/auth/tokenStore.test.ts`:

```ts
  // The test that fails if ANY single call site in the §4.6 table is missed:
  // a dropped lifetime reads as "unknown", which reads as "live", which means
  // the recovery never fires. Sign in, cross the recorded lifetime, and the
  // expiry must have gone from "in the future" to "in the past".
  it("a recorded lifetime actually elapses", () => {
    vi.useFakeTimers();
    try {
      setAccessToken("at-e2e", 60);
      const { expiresAt } = getAccessTokenSnapshot();
      expect(expiresAt).toBeGreaterThan(Date.now());
      vi.advanceTimersByTime(61_000);
      expect(getAccessTokenSnapshot().expiresAt).toBeLessThanOrEqual(Date.now());
    } finally {
      vi.useRealTimers();
    }
  });
```

- [ ] **Step 6: Run and verify RED**

```bash
cd /home/tore/orkestra/frontend-client && npx vitest run src/pages/LoginPage.test.tsx src/pages/OAuthCallbackPage.test.tsx 2>&1 | tail -25
```

Expected: the new cases fail on `expiresAt` being `null` (the lifetime is
dropped) or `Date.now() + 900_000` (the fabrication). ⚠️ A case asserting a
**new field of an existing call** can pass at RED — if any of these is green
before Step 7, widen the fixture until it is not, or the case proves nothing.

- [ ] **Step 7: Thread the lifetime through the six sites**

**`src/api/auth.ts`** — two edits.

```ts
      // `expiresIn` is OPTIONAL on the wire. It used to default to 900, which
      // FABRICATES a fifteen-minute lifetime the server never promised: on a
      // deployment running a 60s TTL the store would then read every 401 as
      // "not a token problem" for the rest of that quarter hour. An unknown
      // lifetime is a fact the store knows how to handle (§4.5's fallback
      // chain); a wrong one is not.
      expiresIn: body.expiresIn,
```

and widen the discriminated union's field:

```ts
      expiresIn?: number;
```

`MfaLoginVerifyResult.expiresIn` should get the same optional treatment for
the same reason — check what `mfaLoginVerify` does with the body (it casts
straight through, so the type is the only change).

**`src/auth/authContext.ts`:**

```ts
  // The lifetime travels WITH the token: the store derives the expiry from
  // the duration at receipt (§4.5), and a caller that drops it leaves the
  // store with an unknown expiry, which reads as "live", which disables the
  // 401 recovery for that session.
  signIn: (token: string, expiresInSeconds?: number) => void;
```

**`src/auth/AuthProvider.tsx`:**

```ts
  const signIn = useCallback((next: string, expiresInSeconds?: number) => {
    setSessionMarker();
    setAccessToken(next, expiresInSeconds);
  }, []);
```

**`src/pages/LoginPage.tsx`** — `complete` takes the result, not a bare token,
so a future field cannot be dropped by a projection at the call site:

```ts
  // Takes the whole result rather than `result.accessToken`: the lifetime is
  // already on it, and the two `.accessToken` projections below are exactly
  // where it used to be lost.
  function complete(result: { accessToken: string; expiresIn?: number }) {
    signIn(result.accessToken, result.expiresIn);
    navigate(destination, { replace: true });
  }
```

Both call sites become `complete(result)` — the mutation's `onSuccess` (after
the `mfa_required` early return, where TypeScript has already narrowed the
union to the token branch) and `MfaChallenge`'s `onSuccess={(result) =>
complete(result)}`.

**`src/pages/OAuthCallbackPage.tsx`** line 117:

```ts
            signIn(result.accessToken, result.expiresIn);
```

- [ ] **Step 8: Run the full suite to verify it passes**

```bash
cd /home/tore/orkestra/frontend-client && npx vitest run 2>&1 | tail -20
```

Expected: PASS. `auth.test.ts` asserts `login`'s mapped result — if it pinned
`expiresIn: 900` for a body without the field, **that assertion is the bug the
`?? 900` fix removes**: change it to `undefined` and say so in the commit body.
That is the one sanctioned edit to a regression file in this plan; anything
else that needs editing must be raised, not adjusted.

- [ ] **Step 9: Documentation (same commit)**

**`frontend-client/CLAUDE.md`**, "How auth works" item 1 — replace the
description of the in-memory token:

> 1. **In-memory access token** — `src/auth/tokenStore.ts` holds the RS256 JWT
>    in a module-scoped variable, **together with the moment it expires**.
>    Never localStorage, never sessionStorage. The expiry is derived from the
>    `expiresIn` **duration** the server reported at receipt, not from the
>    token's absolute `exp`: both ends of the eventual comparison then come
>    from the same clock, so a badly set client clock cancels out instead of
>    reopening a broken window every TTL cycle. Every path that installs a
>    token must pass the lifetime alongside it (`setAccessToken(token,
>    expiresInSeconds)`); a dropped one records an **unknown** expiry, which
>    reads as "live" and silently disables the 401 recovery. A response
>    without `expiresIn` falls back to `src/lib/jwtExp.ts` (no signature
>    verification — a scheduling hint, never a security decision), and an
>    unreadable one leaves the expiry unknown. Read the pair through
>    `getAccessTokenSnapshot()`, never the two separately.

- [ ] **Step 10: Gate and commit**

```bash
make -C /home/tore/orkestra ci-frontend-client 2>&1 | tail -20
cd /home/tore/orkestra
export CLAUDE_SESSION="https://claude.ai/code/session_01QBHr35WPNoZZ1r2oNY7fDE"
git add frontend-client/src/auth/tokenStore.ts \
        frontend-client/src/auth/tokenStore.test.ts \
        frontend-client/src/auth/authContext.ts \
        frontend-client/src/auth/AuthProvider.tsx \
        frontend-client/src/pages/LoginPage.tsx \
        frontend-client/src/pages/LoginPage.test.tsx \
        frontend-client/src/pages/OAuthCallbackPage.tsx \
        frontend-client/src/pages/OAuthCallbackPage.test.tsx \
        frontend-client/src/api/auth.ts \
        frontend-client/src/api/auth.test.ts \
        frontend-client/CLAUDE.md
git commit -m "$(printf '%s\n\n%s\n\n%s\n' \
  "feat(client): record the access token's expiry from the reported duration" \
  "The expiry is derived from expiresIn at receipt rather than from the JWT's absolute exp, so both ends of the comparison come from one clock and skew cancels instead of reopening a broken window every TTL cycle. The duration reached the API layer and died one call short of the store; all six install sites now carry it. auth.ts stops fabricating 900s for an absent expiresIn — an unknown lifetime is a fact the store handles, a wrong one is a lie it would act on." \
  "Claude-Session: ${CLAUDE_SESSION:?set CLAUDE_SESSION first}")"
```

---

## Task 6: §4.1a/b/c — `performRefresh` becomes cross-tab, 409-aware, bounded, and its outcome table inverts

This is defect B, and it must land **before** the helper puts the refresh on
the request path.

**The three changes are one safety argument, not three choices.** The Web Lock
is deliberately *not* bounded with an `AbortSignal` (that needs the 3-argument
`request(name, {signal}, cb)` overload, and `frontend-admin`'s own comment
records that switching shapes silently defeated its test). It is safe only
because everything done while holding it happens inside the fetch timeout —
the lock is bounded **transitively**. **Weakening the timeout re-arms the
lock.**

**Files:**
- Modify: `frontend-client/src/auth/tokenStore.ts`
- Modify: `frontend-client/src/auth/tokenStore.test.ts` (additive)
- Modify: `frontend-client/CLAUDE.md` (refresh choreography — the outcome table)

**Interfaces:**
- Consumes: `setAccessToken(token, expiresInSeconds?)` (Task 5).
- Produces: `REFRESH_LOCK_NAME`, `REFRESH_FETCH_TIMEOUT_MS` (both exported, for the tests); `performRefresh`'s public contract is **unchanged** — still coalesced, still never rejects, still the same three `RefreshOutcome` shapes.

- [ ] **Step 1: Write the failing tests — the inverted table first**

Append to `frontend-client/src/auth/tokenStore.test.ts`:

```ts
// A helper that seeds a live-looking session, so every assertion below can
// check that BOTH the token and the marker survived.
const seedSession = () => {
  setSessionMarker();
  setAccessToken("at-live", 900);
};

describe("performRefresh outcome allowlist (§4.1, defect C)", () => {
  // The rule INVERTS: signed-out is an allowlist of exactly one status. Only a
  // 401 says "the credential I presented was rejected"; every other non-2xx
  // says something about the SERVER and nothing about the session. A denylist
  // is what defect C was — and 429 is not hypothetical, /refresh-cookie is
  // mounted under the router's GLOBAL rate limiter
  // (cmd/server/middleware.go:131), and a burst of tabs rotating at once is
  // exactly the traffic shape that trips it.
  it.each([
    ["429 from the global rate limiter", 429, { "Retry-After": "30" }],
    ["500", 500, {}],
    ["502 from a proxy during a deploy", 502, {}],
    ["504", 504, {}],
    ["408", 408, {}],
    ["404 from a misrouted host", 404, {}],
  ])("%s is unavailable and keeps token AND marker", async (_l, status, headers) => {
    seedSession();
    server.use(
      http.post(REFRESH, () =>
        HttpResponse.json({ detail: "nope" }, { status, headers }),
      ),
    );
    await expect(refreshAccessToken(API)).resolves.toEqual({
      status: "unavailable",
    });
    expect(getAccessToken()).toBe("at-live");
    expect(hasSessionMarker()).toBe(true);
  });

  // v1 called this "a broken response, not an outage" and signed the user out.
  // It IS a broken response, which is the reason not to act on it: a server
  // that answers 200 with no token has told us nothing about the session.
  it("a 2xx with no token is unavailable, not a sign-out", async () => {
    seedSession();
    server.use(http.post(REFRESH, () => HttpResponse.json({ ok: true })));
    await expect(refreshAccessToken(API)).resolves.toEqual({
      status: "unavailable",
    });
    expect(getAccessToken()).toBe("at-live");
    expect(hasSessionMarker()).toBe(true);
  });

  // The allowlist's only member. Without this the table above could silently
  // become "nothing ever signs out".
  it("401 is the ONLY status that signs out, and clears both", async () => {
    seedSession();
    server.use(
      http.post(REFRESH, () => new HttpResponse(null, { status: 401 })),
    );
    await expect(refreshAccessToken(API)).resolves.toEqual({
      status: "signed-out",
    });
    expect(getAccessToken()).toBeNull();
    expect(hasSessionMarker()).toBe(false);
  });
});

describe("performRefresh 409 handling (§4.1b, G7)", () => {
  it("409 then 2xx is ok, and the marker survives", async () => {
    seedSession();
    let hits = 0;
    server.use(
      http.post(REFRESH, () => {
        hits++;
        return hits === 1
          ? HttpResponse.json({ code: "refresh_rotation_raced" }, { status: 409 })
          : HttpResponse.json({ accessToken: "at-2", expiresIn: 900 });
      }),
    );
    await expect(refreshAccessToken(API)).resolves.toEqual({
      status: "ok",
      accessToken: "at-2",
    });
    expect(hits).toBe(2);
    expect(hasSessionMarker()).toBe(true);
  });

  // THE regression test for defect B: a lost rotation race used to fall into
  // !res.ok → signedOut(), and clearing the MARKER made it sticky, so the
  // session stayed lost across a cold load even though the cookie in the jar
  // was perfectly valid.
  it("409 twice is unavailable — token and marker both kept", async () => {
    seedSession();
    let hits = 0;
    server.use(
      http.post(REFRESH, () => {
        hits++;
        return HttpResponse.json(
          { code: "refresh_rotation_raced" },
          { status: 409 },
        );
      }),
    );
    await expect(refreshAccessToken(API)).resolves.toEqual({
      status: "unavailable",
    });
    expect(hits).toBe(2); // exactly one retry, never a loop
    expect(getAccessToken()).toBe("at-live");
    expect(hasSessionMarker()).toBe(true);
  });

  it("409 then 401 is signed-out, and clears both", async () => {
    seedSession();
    let hits = 0;
    server.use(
      http.post(REFRESH, () => {
        hits++;
        return hits === 1
          ? HttpResponse.json({ code: "refresh_rotation_raced" }, { status: 409 })
          : new HttpResponse(null, { status: 401 });
      }),
    );
    await expect(refreshAccessToken(API)).resolves.toEqual({
      status: "signed-out",
    });
    expect(getAccessToken()).toBeNull();
    expect(hasSessionMarker()).toBe(false);
  });
});

describe("performRefresh cross-tab lock (§4.1a, G6)", () => {
  // happy-dom 20.9 sets navigator.locks to NULL, not undefined — and
  // `typeof null === "object"`, so a guard written
  // `typeof navigator.locks === "undefined"` passes and then throws on
  // `.request`. The guard must be `!locks?.request`.
  it("runs unguarded when navigator.locks is null (happy-dom's default)", async () => {
    expect(navigator.locks).toBeNull(); // asserts the premise, not the code
    setSessionMarker();
    const refresh = countingRefresh(() => HttpResponse.json(okBody));
    await expect(refreshAccessToken(API)).resolves.toEqual({
      status: "ok",
      accessToken: "at-1",
    });
    expect(refresh.hits()).toBe(1);
  });

  it("takes the named lock and runs the refresh inside it", async () => {
    const calls: string[] = [];
    let ranInside = false;
    vi.stubGlobal("navigator", {
      locks: {
        request: async (name: string, cb: () => Promise<unknown>) => {
          calls.push(name);
          const out = await cb();
          ranInside = true;
          return out;
        },
      },
    });
    setSessionMarker();
    server.use(http.post(REFRESH, () => HttpResponse.json(okBody)));
    await refreshAccessToken(API);
    expect(calls).toEqual(["orkestra:auth-refresh"]);
    expect(ranInside).toBe(true);
  });

  // The in-tab coalescing must survive the lock — and it must be exercised
  // MIXED, because both entry points now funnel through performRefresh and a
  // regression that coalesced only within one of them would otherwise pass.
  it("coalesces concurrent callers across BOTH entry points into one request", async () => {
    setSessionMarker();
    const refresh = countingRefresh(() => HttpResponse.json(okBody));
    await Promise.all([
      refreshAccessToken(API),
      refreshAfterUnauthorized(API),
    ]);
    expect(refresh.hits()).toBe(1);
  });
});
```

> `refreshAfterUnauthorized` arrives in Task 7. Until then, write that last
> case with two `refreshAccessToken` calls and **widen it in Task 7** — the
> task brief for 7 must say so, or the mixed case is quietly never written.

- [ ] **Step 2: Write the failing timeout tests**

```ts
describe("performRefresh is bounded (§4.1c)", () => {
  // ⚠️ AbortSignal.timeout is NOT controlled by vitest's fake clock (probed:
  // `aborted` stays false after advancing 20s). If someone "simplifies"
  // AbortController + setTimeout back to it, these cases stop aborting and
  // either hang for ten real seconds or fail — which is the tripwire that
  // keeps the divergence from frontend-admin honest.
  it("a fetch that never resolves is unavailable at the timeout, keeping both", async () => {
    vi.useFakeTimers();
    try {
      seedSession();
      server.use(http.post(REFRESH, () => new Promise<never>(() => {})));
      const p = refreshAccessToken(API);
      await vi.advanceTimersByTimeAsync(REFRESH_FETCH_TIMEOUT_MS + 10);
      await expect(p).resolves.toEqual({ status: "unavailable" });
      expect(getAccessToken()).toBe("at-live");
      expect(hasSessionMarker()).toBe(true);
    } finally {
      vi.useRealTimers();
    }
  });

  // The round-13 regression test. It MUST use a streamed body: `fetch`
  // resolves on HEADERS, so a delayed whole response is caught even by a
  // clearTimeout placed right after `await fetch` — that shape would pass
  // against the bug. Measured against the buggy version: fetch resolved at
  // 31ms, the timer was cleared, and the body finished 3s later with no abort.
  it("a response whose HEADERS arrive but whose BODY stalls still times out", async () => {
    vi.useFakeTimers();
    try {
      seedSession();
      server.use(
        http.post(REFRESH, () => {
          const stream = new ReadableStream({
            start(controller) {
              controller.enqueue(new TextEncoder().encode('{"accessToken":'));
              // never closed — the body stalls after the headers
            },
          });
          return new HttpResponse(stream, {
            headers: { "Content-Type": "application/json" },
          });
        }),
      );
      const p = refreshAccessToken(API);
      await vi.advanceTimersByTimeAsync(REFRESH_FETCH_TIMEOUT_MS + 10);
      await expect(p).resolves.toEqual({ status: "unavailable" });
      expect(getAccessToken()).toBe("at-live");
      expect(hasSessionMarker()).toBe(true);
    } finally {
      vi.useRealTimers();
    }
  });

  // The assertion that actually pins §4.1a's transitive bound: a contender
  // queued behind a stalled holder must get the lock when the timeout fires,
  // not wait forever. Without this, "the lock is bounded transitively" is a
  // claim rather than a tested property — which is exactly what round 13
  // found it to be.
  it("releases the lock when the timeout fires", async () => {
    vi.useFakeTimers();
    try {
      let chain: Promise<unknown> = Promise.resolve();
      const locks = {
        request: (_name: string, cb: () => Promise<unknown>) => {
          const run = chain.then(() => cb());
          chain = run.catch(() => undefined);
          return run;
        },
      };
      vi.stubGlobal("navigator", { locks });
      seedSession();
      server.use(http.post(REFRESH, () => new Promise<never>(() => {})));

      const holder = refreshAccessToken(API);
      let contenderRan = false;
      const contender = locks.request("orkestra:auth-refresh", async () => {
        contenderRan = true;
      });

      await vi.advanceTimersByTimeAsync(REFRESH_FETCH_TIMEOUT_MS + 10);
      await holder;
      await contender;
      expect(contenderRan).toBe(true);
    } finally {
      vi.useRealTimers();
    }
  });

  // A refresh that answers normally must not leave a live 10s timer behind —
  // which is also why clearTimeout beats AbortSignal.timeout, since that one
  // cannot be cancelled at all.
  it("clears the timer on a normal answer", async () => {
    vi.useFakeTimers();
    try {
      setSessionMarker();
      const refresh = countingRefresh(() => HttpResponse.json(okBody));
      await refreshAccessToken(API);
      await vi.advanceTimersByTimeAsync(REFRESH_FETCH_TIMEOUT_MS * 2);
      expect(refresh.hits()).toBe(1); // nothing re-fired, nothing aborted
    } finally {
      vi.useRealTimers();
    }
  });
});
```

> **If MSW misbehaves under the fake clock**, narrow what is faked rather than
> abandoning the case: `vi.useFakeTimers({ toFake: ["setTimeout",
> "clearTimeout", "Date"] })`. Those three are all the implementation and the
> assertions need. Record which form you used in the commit body.

- [ ] **Step 3: Run and verify RED**

```bash
npx vitest run src/auth/tokenStore.test.ts 2>&1 | tail -40
```

Expected: the 429/500/502/504/408/404 rows FAIL with `{status: "signed-out"}`
(the old denylist), the 409 rows FAIL, and the timeout cases hang or fail.
`REFRESH_FETCH_TIMEOUT_MS` is not exported yet — add the import and let that be
part of RED.

- [ ] **Step 4: Implement**

Replace `performRefresh` in `frontend-client/src/auth/tokenStore.ts`:

```ts
// The cross-tab rotation lock. Web Locks is the only cross-tab primitive that
// releases automatically when the holder navigates away or crashes. The name is
// shared with the operator console, but locks are PER-ORIGIN, so client.* and
// console.* never contend — and by ADR-0003 D-9 they hold different refresh
// cookies anyway.
export const REFRESH_LOCK_NAME = "orkestra:auth-refresh";

// The value frontend-admin settled on (baseApi.ts:74). This bound is what makes
// the unbounded Web Lock above safe: everything done while holding the lock
// happens inside it, so the lock is bounded TRANSITIVELY. Weakening this
// re-arms the lock.
export const REFRESH_FETCH_TIMEOUT_MS = 10_000;

async function withRefreshLock<T>(run: () => Promise<T>): Promise<T> {
  const locks = typeof navigator !== "undefined" ? navigator.locks : undefined;
  // `!locks?.request`, NOT `typeof locks === "undefined"`: happy-dom 20.9 sets
  // navigator.locks to NULL, and `typeof null === "object"`, so the typeof
  // guard passes and then throws on `.request`.
  if (!locks?.request) return run();
  // Deliberately the 2-argument overload. Bounding the LOCK needs
  // request(name, {signal}, cb), and frontend-admin's own comment records that
  // switching shapes silently defeated its test. The bound comes from the
  // fetch timeout instead.
  return await locks.request(REFRESH_LOCK_NAME, run);
}

type RefreshAttempt =
  | { kind: "ok"; accessToken: string; expiresIn?: number }
  | { kind: "raced" }
  | { kind: "signed-out" }
  | { kind: "unavailable" };

// One presentation of the refresh cookie.
//
// The outcome rule is an ALLOWLIST: 401 is the only status that means "the
// credential I presented was rejected". Everything else that is not a usable
// 2xx says something about the SERVER and nothing about the session, so it is
// `unavailable` and nothing is cleared (G2). A denylist is what defect C was —
// and /refresh-cookie sits under the router's GLOBAL rate limiter, so 429 is
// reachable on every refresh and a burst of tabs is exactly what trips it.
async function attemptRefresh(apiBase: string): Promise<RefreshAttempt> {
  const ctrl = new AbortController();
  // AbortController + setTimeout, NOT AbortSignal.timeout: the latter runs on
  // an internal timer vitest's fake clock does not control, so the timeout
  // case could not be tested the way a reader would expect. It also cannot be
  // cancelled, leaving a live 10s timer behind after every refresh.
  const timer = setTimeout(() => ctrl.abort(), REFRESH_FETCH_TIMEOUT_MS);
  try {
    const res = await fetch(`${apiBase}/v1/auth/client/refresh-cookie`, {
      method: "POST",
      credentials: "include",
      signal: ctrl.signal,
    });
    if (res.status === 409) return { kind: "raced" };
    if (res.status === 401) return { kind: "signed-out" };
    if (!res.ok) return { kind: "unavailable" };
    const body = (await res.json().catch(() => ({}))) as {
      accessToken?: string;
      token?: string;
      expiresIn?: number;
    };
    const fresh = body.accessToken ?? body.token ?? null;
    // A 2xx with no token is a BROKEN RESPONSE, which is the reason not to act
    // on it: it has told us nothing about the session.
    if (!fresh) return { kind: "unavailable" };
    return { kind: "ok", accessToken: fresh, expiresIn: body.expiresIn };
  } catch {
    // A transport failure, or the abort — including one that fires during the
    // body read, which is why clearTimeout is in the `finally` and NOWHERE
    // else. `fetch` resolves on HEADERS, so clearing the timer straight after
    // the await bounds almost nothing: a server that sends headers and stalls
    // the body would hold the Web Lock for as long as it stalls.
    // "No answer" is not "no".
    return { kind: "unavailable" };
  } finally {
    clearTimeout(timer);
  }
}

async function performRefresh(apiBase: string): Promise<RefreshOutcome> {
  if (inflightRefresh) return inflightRefresh;
  inflightRefresh = (async (): Promise<RefreshOutcome> => {
    try {
      return await withRefreshLock(async (): Promise<RefreshOutcome> => {
        let attempt = await attemptRefresh(apiBase);
        if (attempt.kind === "raced") {
          // A sibling won the CAS, so the browser already holds the successor
          // cookie and a second attempt lands. Exactly ONE retry: a race
          // surviving two attempts is far more likely a live session than a
          // dead one, and guessing "dead" is the failure this removes. The
          // marker is untouched on every 409 path.
          attempt = await attemptRefresh(apiBase);
          if (attempt.kind === "raced") return { status: "unavailable" };
        }
        if (attempt.kind === "signed-out") return signedOut();
        if (attempt.kind === "unavailable") return { status: "unavailable" };
        setAccessToken(attempt.accessToken, attempt.expiresIn);
        return { status: "ok", accessToken: attempt.accessToken };
      });
    } finally {
      // Cleared SYNCHRONOUSLY, and the window it leaves is deliberately not
      // load-bearing: a 401 answered after this point is handled by
      // authedFetch's sent-token comparison (§4.3 branch 3), which is correct
      // for ANY delay. Do not "fix" this by deferring it to a macrotask and
      // conclude the race is thereby closed — it only widens the window by one
      // turn of the event loop, while the 401 it must survive can arrive
      // seconds later.
      inflightRefresh = null;
    }
  })();
  return inflightRefresh;
}
```

Update `performRefresh`'s doc comment to the new table, and update
`RefreshOutcome`'s comment: `unavailable` now covers 503 **and** 429, 408,
every other 5xx and 4xx, a 2xx with no token, a transport failure and a
timeout.

- [ ] **Step 5: Run the tests to verify they pass**

```bash
cd /home/tore/orkestra/frontend-client && npx vitest run src/auth/tokenStore.test.ts 2>&1 | tail -20
```

Expected: PASS, including the original 198 lines. One existing case may assert
that a non-503 non-2xx is `signed-out` — **that is the defect-B behaviour**, and
it is the one place this task legitimately changes an existing assertion.
Rewrite it as the new expectation and say so in the commit body; do not delete
it.

- [ ] **Step 6: Documentation (same commit)**

**`frontend-client/CLAUDE.md`**, "Refresh choreography" — replace the outcome
lines in the code block and add the table:

~~~markdown
        → POST /v1/auth/client/refresh-cookie   (serialised across tabs by a Web Lock;
                                                 bounded at 10s; one 409 retry)
        → ok           → in-memory access token + expiry installed
        → signed-out   → marker + token cleared   (401 ONLY)
        → unavailable  → both kept, caller may retry
~~~

> **`signed-out` is an allowlist of exactly one status.** Only a **401** clears
> anything — it is the one answer that means "the credential I presented was
> rejected". A **429** (the endpoint sits under the router's global rate
> limiter, and a burst of tabs rotating together is what trips it), a 408, any
> 5xx, any other 4xx, a 2xx with no token, a transport failure and the 10 s
> timeout are all `unavailable`: they say something about the server and
> nothing about the session, so the token *and* the marker survive. A denylist
> here is how a Mongo blip became a logout. A **409
> `refresh_rotation_raced`** is retried exactly once — the sibling that won the
> CAS left the successor cookie in the jar — and a second 409 is `unavailable`,
> never a sign-out.
>
> The Web Lock is deliberately **not** bounded with an `AbortSignal`; it is
> bounded *transitively*, because everything done while holding it happens
> inside the 10 s fetch timeout. The timer spans the fetch, the classification
> **and the body read** — `fetch` resolves on headers, so a `clearTimeout`
> placed right after the `await` would leave a stalled body holding the lock
> indefinitely. Do not weaken the timeout: it is what makes the lock safe.

- [ ] **Step 7: Gate and commit**

```bash
make -C /home/tore/orkestra ci-frontend-client 2>&1 | tail -20
cd /home/tore/orkestra
export CLAUDE_SESSION="https://claude.ai/code/session_01QBHr35WPNoZZ1r2oNY7fDE"
git add frontend-client/src/auth/tokenStore.ts \
        frontend-client/src/auth/tokenStore.test.ts \
        frontend-client/CLAUDE.md
git commit -m "$(printf '%s\n\n%s\n\n%s\n' \
  "fix(client): serialise, bound and correctly classify the refresh (defect B)" \
  "A lost rotation race used to fall into !res.ok and sign the user out, clearing the session marker and making it stick across the next cold load — the exact outcome the backend created its 409 to prevent. The outcome rule inverts to an allowlist: only a 401 clears anything, so a 429 from the global rate limiter, a proxy 5xx or a 2xx without a token keep the session. A Web Lock serialises rotation across tabs, and a 10s AbortController timeout spanning the fetch, the classification and the body read is what bounds the lock transitively." \
  "Claude-Session: ${CLAUDE_SESSION:?set CLAUDE_SESSION first}")"
```

---

## Task 7: §4.1e — `refreshAfterUnauthorized`, the unconditional authenticated-retry path

**Why it is a separate function.** `refreshAccessToken` answers `signed-out`
straight from its marker gate (`if (!hasSessionMarker()) return {status:
"signed-out"}`) **without clearing anything** — unlike every other `signed-out`
in the store, which goes through `signedOut()`. Latent today, because
`AuthProvider` discards the outcome with `void`. The moment a 401 handler acts
on that outcome it becomes load-bearing: a tab holding a live in-memory token
but no marker would be told "signed out", handed the raw 401, and left with
`isAuthenticated === true` — defect A's broken state, which is what this whole
spec exists to end.

The marker gate exists to spare **anonymous visitors** a guaranteed-401
round-trip on every cold load. A 401 answering a request that carried a bearer
is not that case: a bearer in memory is proof a session existed.

**Files:**
- Modify: `frontend-client/src/auth/tokenStore.ts`
- Modify: `frontend-client/src/auth/tokenStore.test.ts`

**Interfaces:**
- Produces: `refreshAfterUnauthorized(apiBase: string): Promise<RefreshOutcome>` — never rejects; every outcome comes from `performRefresh`, so a `signed-out` here always clears **both** token and marker (**G3**).

- [ ] **Step 1: Write the failing tests**

```ts
describe("refreshAfterUnauthorized (§4.1e)", () => {
  // Route (i) into "a token in memory with no marker": signIn calls
  // setSessionMarker() first, which swallows a throwing storage by design
  // ("acceptable degradation"), then installs the token. Private mode or
  // blocked site data leaves a live session with no marker.
  it("presents the cookie with NO marker, and repairs the marker on success", async () => {
    setAccessToken("at-stale", 900);
    expect(hasSessionMarker()).toBe(false);
    const refresh = countingRefresh(() =>
      HttpResponse.json({ accessToken: "at-new", expiresIn: 900 }),
    );
    await expect(refreshAfterUnauthorized(API)).resolves.toEqual({
      status: "ok",
      accessToken: "at-new",
    });
    // The assertion is on the REQUEST having happened, not just the outcome —
    // a marker-gated implementation short-circuits with no request at all.
    expect(refresh.hits()).toBe(1);
    expect(getAccessToken()).toBe("at-new");
    // A repair, not a stamp-then-hope: the refresh has just PROVED a cookie
    // exists, so leaving the marker unset would keep the store in a
    // knowingly-false state and lose the session at the next cold load.
    expect(hasSessionMarker()).toBe(true);
  });

  // Route (ii): a sibling tab signed out. localStorage is shared across tabs,
  // so clearSessionMarker() removed it for everyone while the in-memory token
  // — which is per-tab — survived. The cookie is normally dead server-side.
  // THE direct regression test for the v3 assumption that a signed-out outcome
  // had already cleared the token: it had not.
  it("a dead cookie clears BOTH the token and the marker (G3)", async () => {
    setAccessToken("at-stale", 900);
    setSessionMarker();
    server.use(
      http.post(REFRESH, () => new HttpResponse(null, { status: 401 })),
    );
    await expect(refreshAfterUnauthorized(API)).resolves.toEqual({
      status: "signed-out",
    });
    expect(getAccessToken()).toBeNull();
    expect(hasSessionMarker()).toBe(false);
  });

  it("keeps token and marker on unavailable", async () => {
    setAccessToken("at-stale", 900);
    setSessionMarker();
    server.use(
      http.post(REFRESH, () => new HttpResponse(null, { status: 503 })),
    );
    await expect(refreshAfterUnauthorized(API)).resolves.toEqual({
      status: "unavailable",
    });
    expect(getAccessToken()).toBe("at-stale");
    expect(hasSessionMarker()).toBe(true);
  });

  // The anonymous optimisation must survive this change UNTOUCHED.
  it("refreshAccessToken with no marker still short-circuits with NO request", async () => {
    const refresh = countingRefresh(() => HttpResponse.json(okBody));
    await expect(refreshAccessToken(API)).resolves.toEqual({
      status: "signed-out",
    });
    expect(refresh.hits()).toBe(0);
  });
});
```

Also **widen Task 6's coalescing case now** — it was written with two
`refreshAccessToken` calls because this function did not exist yet:

```ts
    await Promise.all([refreshAccessToken(API), refreshAfterUnauthorized(API)]);
    expect(refresh.hits()).toBe(1);
```

(and `setSessionMarker()` before it, so the gated entry point actually
proceeds).

- [ ] **Step 2: Run and verify RED**

```bash
cd /home/tore/orkestra/frontend-client && npx vitest run src/auth/tokenStore.test.ts 2>&1 | tail -25
```

Expected: `TypeError: refreshAfterUnauthorized is not a function`.

- [ ] **Step 3: Implement**

Add to `frontend-client/src/auth/tokenStore.ts`, beside `refreshAccessToken`:

```ts
// refreshAfterUnauthorized — the AUTHENTICATED-RETRY path.
//
// Unlike refreshAccessToken it does NOT consult the session marker. The gate
// exists to spare an anonymous visitor a guaranteed-401 round-trip on every
// cold load; a 401 answering a request that carried a bearer is not that case,
// because a bearer in memory is proof a session existed. A marker that is
// absent for an unrelated reason — a throwing localStorage, a sibling tab's
// signOut — must not veto a cookie that is still valid.
//
// Every outcome therefore comes from performRefresh, which is the only place
// that decides them, so a `signed-out` here ALWAYS clears both token and
// marker (G3). refreshAccessToken's marker gate is the one `signed-out` in
// this store that clears nothing, and this function is how the 401 path routes
// around it. Never rejects.
export async function refreshAfterUnauthorized(
  apiBase: string,
): Promise<RefreshOutcome> {
  const outcome = await performRefresh(apiBase);
  // A repair, not a stamp-then-hope: the refresh has just proved a cookie
  // exists, so leaving the marker unset would keep the store in a
  // knowingly-false state and lose the session at the next cold load.
  // Best-effort by construction — setSessionMarker swallows a throwing
  // storage. The accepted cost (O5) is that a sibling tab's sign-out whose
  // POST /logout failed can be outlived across a reload.
  if (outcome.status === "ok") setSessionMarker();
  return outcome;
}
```

- [ ] **Step 4: Run to verify PASS**

```bash
cd /home/tore/orkestra/frontend-client && npx vitest run src/auth/tokenStore.test.ts 2>&1 | tail -15
```

- [ ] **Step 5: Gate and commit**

```bash
make -C /home/tore/orkestra ci-frontend-client 2>&1 | tail -20
cd /home/tore/orkestra
export CLAUDE_SESSION="https://claude.ai/code/session_01QBHr35WPNoZZ1r2oNY7fDE"
git add frontend-client/src/auth/tokenStore.ts frontend-client/src/auth/tokenStore.test.ts
git commit -m "$(printf '%s\n\n%s\n\n%s\n' \
  "feat(client): add refreshAfterUnauthorized, the un-gated authenticated retry" \
  "refreshAccessToken answers signed-out from its marker gate without clearing anything, unlike every other signed-out in the store. Harmless while AuthProvider voids the outcome; the moment a 401 handler acts on it, a tab with a live token and no marker would be told 'signed out' and left signed-in-but-broken. The marker gate is an anonymous-visitor optimisation and has no business vetoing a cookie when a bearer was actually sent, so the 401 path routes around it and repairs the marker when the cookie proves out." \
  "Claude-Session: ${CLAUDE_SESSION:?set CLAUDE_SESSION first}")"
```

---

## Task 8: §4.2 + §4.3 — `src/api/authedFetch.ts`, the one helper and the only 401 algorithm

**Files:**
- Create: `frontend-client/src/api/authedFetch.ts`
- Create: `frontend-client/src/api/authedFetch.test.ts`
- Modify: `frontend-client/src/auth/tokenStore.ts` (export `clearSessionLocally`)

**Interfaces:**
- Consumes: `getAccessTokenSnapshot`, `refreshAccessToken`, `refreshAfterUnauthorized`, `clearSessionLocally` (tokenStore); `apiBaseURL` (client.ts).
- Produces: `authedFetch(path: string, init?: RequestInit): Promise<Response>` — Tasks 9's four wrappers call exactly this.

**The branch order is load-bearing, not cosmetic.** The replay guard sits ahead
of every recovery branch. v2 had it last, and putting the "a sibling rotated"
branch in front of it would have replayed a `change-password` rejection — a 401
earned on its own merits — merely because a sibling request happened to rotate
in the meantime.

- [ ] **Step 1: Export the one way to end a session locally**

In `frontend-client/src/auth/tokenStore.ts`:

```ts
// clearSessionLocally is the ONLY sanctioned way for code outside this module
// to end a session's local state, and it clears BOTH the token and the marker.
// Exported because §4.3 branch 1 (a 401 carrying a terminal code) and the
// terminal retry must end the session without a refresh. Clearing only one of
// the two is exactly the defect the deleted client.ts middleware had: it
// cleared the token and left a marker that then short-circuited the next cold
// load.
export function clearSessionLocally(): void {
  clearSessionMarker();
  clearAccessToken();
}
```

and make the module-internal `signedOut` use it, so there is one implementation:

```ts
const signedOut = (): RefreshOutcome => {
  clearSessionLocally();
  return { status: "signed-out" };
};
```

- [ ] **Step 2: Write the failing header-merge tests**

Create `frontend-client/src/api/authedFetch.test.ts`. **The shapes are the
point, so the cases are the shapes:**

```ts
import { describe, expect, it, vi } from "vitest";
import { http, HttpResponse } from "msw";

import { authedFetch } from "@/api/authedFetch";
import {
  getAccessToken,
  getAccessTokenSnapshot,
  setAccessToken,
} from "@/auth/tokenStore";
import { hasSessionMarker, setSessionMarker } from "@/auth/sessionMarker";
import { url } from "@/test/handlers";
import { server } from "@/test/server";

const REFRESH = url("/v1/auth/client/refresh-cookie");
const THING = url("/v1/me/thing");

// Records what actually reached the wire for /v1/me/thing.
const recordRequests = (respond: (hit: number, req: Request) => Response) => {
  const seen: Request[] = [];
  server.use(
    http.all(THING, ({ request }) => {
      seen.push(request.clone());
      return respond(seen.length, request);
    }),
  );
  return {
    seen,
    header: (i: number, name: string) => seen[i]?.headers.get(name) ?? null,
  };
};

const countRefresh = (respond: (hit: number) => Response) => {
  let hits = 0;
  server.use(
    http.post(REFRESH, () => {
      hits++;
      return respond(hits);
    }),
  );
  return { hits: () => hits };
};

// A token whose life is under our control. The lifetime is what the tests
// actually manipulate; the string itself never has to be a real JWT because
// setAccessToken's duration argument short-circuits the jwtExp fallback.
const seedToken = (token: string, expiresInSeconds: number) => {
  setSessionMarker();
  setAccessToken(token, expiresInSeconds);
};
const seedExpiredToken = (token = "at-old") => {
  setSessionMarker();
  setAccessToken(token, -1); // expiresAt is already in the past
};

describe("authedFetch header merging (§4.2)", () => {
  it("sends headers given as a Headers instance", async () => {
    const rec = recordRequests(() => HttpResponse.json({ ok: true }));
    seedToken("at-1", 900);
    await authedFetch("/v1/me/thing", {
      headers: new Headers({ "X-Custom": "kept" }),
    });
    // Fails against an object-spread implementation: spreading a Headers
    // instance yields {} — it has no own enumerable properties — so every
    // header the caller set is dropped SILENTLY.
    expect(rec.header(0, "X-Custom")).toBe("kept");
  });

  it("sends headers given as an array of tuples", async () => {
    const rec = recordRequests(() => HttpResponse.json({ ok: true }));
    seedToken("at-1", 900);
    await authedFetch("/v1/me/thing", { headers: [["X-Custom", "kept"]] });
    expect(rec.header(0, "X-Custom")).toBe("kept");
  });

  it("sends headers given as a plain object (the migration's regression guard)", async () => {
    const rec = recordRequests(() => HttpResponse.json({ ok: true }));
    seedToken("at-1", 900);
    await authedFetch("/v1/me/thing", { headers: { "X-Custom": "kept" } });
    expect(rec.header(0, "X-Custom")).toBe("kept");
  });

  it("defaults Accept and Content-Type only when absent", async () => {
    const rec = recordRequests(() => HttpResponse.json({ ok: true }));
    seedToken("at-1", 900);
    await authedFetch("/v1/me/thing", {
      method: "POST",
      body: JSON.stringify({ a: 1 }),
    });
    expect(rec.header(0, "Accept")).toBe("application/json");
    expect(rec.header(0, "Content-Type")).toBe("application/json");

    await authedFetch("/v1/me/thing", {
      method: "POST",
      body: JSON.stringify({ a: 1 }),
      headers: { Accept: "text/csv", "Content-Type": "application/merge-patch+json" },
    });
    expect(rec.header(1, "Accept")).toBe("text/csv");
    expect(rec.header(1, "Content-Type")).toBe("application/merge-patch+json");
  });

  it("does NOT set Content-Type for a FormData body", async () => {
    const rec = recordRequests(() => HttpResponse.json({ ok: true }));
    seedToken("at-1", 900);
    const fd = new FormData();
    fd.append("f", "v");
    await authedFetch("/v1/me/thing", { method: "POST", body: fd });
    // Forcing application/json on FormData destroys the multipart boundary.
    // fetch sets its own with the boundary; assert it is NOT ours.
    expect(rec.header(0, "Content-Type")).not.toBe("application/json");
  });

  it.each([
    ["a plain object", { Authorization: "Bearer caller" }],
    ["a Headers instance", new Headers({ Authorization: "Bearer caller" })],
    ["an array of tuples", [["Authorization", "Bearer caller"]] as [string, string][]],
  ])("overrides a caller-supplied Authorization given as %s", async (_l, headers) => {
    const rec = recordRequests(() => HttpResponse.json({ ok: true }));
    seedToken("at-1", 900);
    await authedFetch("/v1/me/thing", { headers });
    // The precedence decision, tested where it can actually break. avatar.ts
    // let the bearer win, billingProfile.ts let caller headers win — a
    // divergence that existed only because each wrapper chose its own spread
    // order. `set` last, always.
    expect(rec.header(0, "Authorization")).toBe("Bearer at-1");
  });
});
```

- [ ] **Step 3: Write the failing 401-branch tests**

```ts
describe("authedFetch 401 recovery (§4.3)", () => {
  it("expired token → 401 → refresh → retry carries the NEW bearer", async () => {
    const refresh = countRefresh(() =>
      HttpResponse.json({ accessToken: "at-new", expiresIn: 900 }),
    );
    const rec = recordRequests((hit) =>
      hit === 1
        ? new HttpResponse(null, { status: 401 })
        : HttpResponse.json({ ok: true }),
    );
    seedExpiredToken("at-old");

    const res = await authedFetch("/v1/me/thing");
    expect(res.status).toBe(200);
    expect(refresh.hits()).toBe(1);
    expect(rec.header(0, "Authorization")).toBe("Bearer at-old");
    expect(rec.header(1, "Authorization")).toBe("Bearer at-new");
  });

  // THE lockout-hazard regression test (§4.4). change-password is an
  // AUTHENTICATED endpoint that answers 401 when the CURRENT PASSWORD IN THE
  // BODY is wrong; a blanket "401 → refresh → retry" re-sends the failed
  // attempt and the backend counts it again, so a user who mistypes twice is
  // locked out as though they had tried four times.
  //
  // The fixture's remaining life IS the test: a token with 20 minutes left
  // passes against the broken implementation too. 20s is inside any plausible
  // margin — it is precisely the value the removed 30s SKEW would have
  // mis-classified as "not live".
  it("a live token's 401 is passed through — no refresh, no replay", async () => {
    const refresh = countRefresh(() => HttpResponse.json({ accessToken: "x" }));
    const rec = recordRequests(() =>
      HttpResponse.json({ detail: "Invalid email or password" }, { status: 401 }),
    );
    seedToken("at-live", 20);
    // The fixture asserts its own premise: 20s of life, which the server still
    // accepts, so the handler DID run and counted the failed attempt.
    const { expiresAt } = getAccessTokenSnapshot();
    expect(expiresAt! - Date.now()).toBeGreaterThan(15_000);
    expect(expiresAt! - Date.now()).toBeLessThan(25_000);

    const res = await authedFetch("/v1/auth/client/change-password", {
      method: "POST",
      body: JSON.stringify({ currentPassword: "wrong", newPassword: "x" }),
    });
    expect(res.status).toBe(401);
    expect(refresh.hits()).toBe(0);
    expect(rec.seen.length).toBe(0); // it went to change-password, not /thing
  });

  // Pins that the comparison is `<=` at the exact instant and carries no
  // hidden margin. A margin here IS the round-11 replay hole.
  it("expiresAt === sentAt counts as expired; sentAt + 1 counts as live", async () => {
    vi.useFakeTimers();
    try {
      const refresh = countRefresh(() =>
        HttpResponse.json({ accessToken: "at-new", expiresIn: 900 }),
      );
      recordRequests((hit) =>
        hit % 2 === 1
          ? new HttpResponse(null, { status: 401 })
          : HttpResponse.json({ ok: true }),
      );
      setSessionMarker();

      setAccessToken("at-boundary", 0); // expiresAt === Date.now()
      await authedFetch("/v1/me/thing");
      expect(refresh.hits()).toBe(1);

      setAccessToken("at-live", 0.001); // expiresAt === Date.now() + 1ms
      await authedFetch("/v1/me/thing");
      expect(refresh.hits()).toBe(1); // unchanged — passed through
    } finally {
      vi.useRealTimers();
    }
  });

  // The direction that FLIPPED in round 11. An unknown expiry cannot prove the
  // handler never ran, and under a rule whose failure mode is a REPLAY rather
  // than a wasted refresh, "don't know" falls on the safe side.
  it("an UNKNOWN expiry is treated as live — passed through, no refresh", async () => {
    const refresh = countRefresh(() => HttpResponse.json({ accessToken: "x" }));
    recordRequests(() => new HttpResponse(null, { status: 401 }));
    setSessionMarker();
    setAccessToken("opaque-not-a-jwt"); // no duration, unreadable exp
    expect(getAccessTokenSnapshot().expiresAt).toBeNull();

    const res = await authedFetch("/v1/me/thing", { method: "POST", body: "{}" });
    expect(res.status).toBe(401);
    expect(refresh.hits()).toBe(0);
  });

  // R1 / §3.D. The server states it rejected the bearer BEFORE dispatch, so
  // the request provably never reached its handler — proof enough on its own,
  // and it recovers the token that expired IN FLIGHT, which the client-side
  // reckoning has to give up. Fails against a reckoning-only implementation.
  it("recovers a LIVE token's 401 when the server says access_token_expired", async () => {
    const refresh = countRefresh(() =>
      HttpResponse.json({ accessToken: "at-new", expiresIn: 900 }),
    );
    const rec = recordRequests((hit) =>
      hit === 1
        ? HttpResponse.json(
            { status: 401, title: "access token expired", code: "access_token_expired" },
            { status: 401 },
          )
        : HttpResponse.json({ ok: true }),
    );
    seedToken("at-inflight", 900); // still live by our own reckoning

    const res = await authedFetch("/v1/me/thing");
    expect(res.status).toBe(200);
    expect(refresh.hits()).toBe(1);
    expect(rec.header(1, "Authorization")).toBe("Bearer at-new");
  });

  it.each([
    ["503", 503],
    ["429", 429],
  ])("refresh %s → the caller sees the original 401 and the token survives (G2)", async (_l, status) => {
    countRefresh(() => new HttpResponse(null, { status }));
    recordRequests(() => new HttpResponse(null, { status: 401 }));
    seedExpiredToken("at-old");

    const res = await authedFetch("/v1/me/thing");
    expect(res.status).toBe(401);
    expect(getAccessToken()).toBe("at-old");
    expect(hasSessionMarker()).toBe(true);
  });

  it("refresh 409 twice → original 401, marker and token KEPT (G7)", async () => {
    const refresh = countRefresh(() =>
      HttpResponse.json({ code: "refresh_rotation_raced" }, { status: 409 }),
    );
    recordRequests(() => new HttpResponse(null, { status: 401 }));
    seedExpiredToken("at-old");

    const res = await authedFetch("/v1/me/thing");
    expect(res.status).toBe(401);
    expect(refresh.hits()).toBe(2);
    expect(getAccessToken()).toBe("at-old");
    expect(hasSessionMarker()).toBe(true);
  });

  it("refresh 401 → token and marker cleared so AuthProvider can re-render (G3)", async () => {
    countRefresh(() => new HttpResponse(null, { status: 401 }));
    recordRequests(() => new HttpResponse(null, { status: 401 }));
    seedExpiredToken("at-old");

    const res = await authedFetch("/v1/me/thing");
    expect(res.status).toBe(401);
    expect(getAccessToken()).toBeNull();
    expect(hasSessionMarker()).toBe(false);
  });

  // Fails against a v3-shaped implementation, which routed everything through
  // the marker-gated refreshAccessToken, returned the raw 401 and left the
  // user signed-in-but-broken.
  it("expired token with NO marker still attempts the refresh (branch 4a)", async () => {
    const refresh = countRefresh(() =>
      HttpResponse.json({ accessToken: "at-new", expiresIn: 900 }),
    );
    const rec = recordRequests((hit) =>
      hit === 1 ? new HttpResponse(null, { status: 401 }) : HttpResponse.json({ ok: true }),
    );
    setAccessToken("at-old", -1); // NO setSessionMarker
    expect(hasSessionMarker()).toBe(false);

    const res = await authedFetch("/v1/me/thing");
    expect(res.status).toBe(200);
    expect(refresh.hits()).toBe(1);
    expect(rec.header(1, "Authorization")).toBe("Bearer at-new");
  });

  it("no bearer and no marker (anonymous) → ZERO refresh requests (branch 4b)", async () => {
    const refresh = countRefresh(() => HttpResponse.json({ accessToken: "x" }));
    recordRequests(() => new HttpResponse(null, { status: 401 }));
    // no token, no marker

    const res = await authedFetch("/v1/me/thing");
    expect(res.status).toBe(401);
    expect(refresh.hits()).toBe(0);
  });

  it("a burst of three 401s produces exactly one /refresh-cookie", async () => {
    const refresh = countRefresh(() =>
      HttpResponse.json({ accessToken: "at-new", expiresIn: 900 }),
    );
    let hits = 0;
    server.use(
      http.get(THING, ({ request }) => {
        hits++;
        return request.headers.get("Authorization") === "Bearer at-new"
          ? HttpResponse.json({ ok: true })
          : new HttpResponse(null, { status: 401 });
      }),
    );
    seedExpiredToken("at-old");

    const all = await Promise.all([
      authedFetch("/v1/me/thing"),
      authedFetch("/v1/me/thing"),
      authedFetch("/v1/me/thing"),
    ]);
    expect(all.map((r) => r.status)).toEqual([200, 200, 200]);
    expect(refresh.hits()).toBe(1);
    void hits;
  });
});
```

- [ ] **Step 4: Write the failing terminal-code and body-integrity tests**

```ts
describe("authedFetch terminal codes (§4.3 branch 1, §3.C)", () => {
  it.each([["session_revoked"], ["session_max_age_reached"]])(
    "%s on a LIVE token clears everything with no refresh",
    async (code) => {
      const refresh = countRefresh(() => HttpResponse.json({ accessToken: "x" }));
      recordRequests(() => HttpResponse.json({ code }, { status: 401 }));
      seedToken("at-live", 900);

      const res = await authedFetch("/v1/me/thing");
      expect(res.status).toBe(401);
      // A token minted from the same cookie carries the same dead sid, so a
      // refresh is pointless — not merely wasteful.
      expect(refresh.hits()).toBe(0);
      expect(getAccessToken()).toBeNull();
      expect(hasSessionMarker()).toBe(false);
    },
  );

  // THE test that fails against `if (body.code) → clear`, the simplification
  // §3.C exists to forbid. The middleware emits at least seven top-level
  // codes and four of them ride on 401s that are emphatically not a dead
  // session — turning four recoverable prompts into a logout.
  it.each([
    ["step_up_required"],
    ["mfa_enrollment_required"],
    ["password_confirm_required"],
  ])("a non-terminal code (%s) on a live token is passed through untouched", async (code) => {
    const refresh = countRefresh(() => HttpResponse.json({ accessToken: "x" }));
    recordRequests(() => HttpResponse.json({ code }, { status: 401 }));
    seedToken("at-live", 900);

    const res = await authedFetch("/v1/me/thing");
    expect(res.status).toBe(401);
    expect(refresh.hits()).toBe(0);
    expect(getAccessToken()).toBe("at-live");
    expect(hasSessionMarker()).toBe(true);
  });

  // Pins that the implementation reads the TOP LEVEL and not errors[0].value.
  // The generic sendErrorResponse shape puts appErr.Code there, and for an
  // AuthenticationError that value is CodeInvalidCredentials — the same value
  // a wrong password produces, so it discriminates nothing.
  it("a code that lives only at errors[0].value is NOT terminal", async () => {
    recordRequests(() =>
      HttpResponse.json(
        {
          status: 401,
          title: "Unauthorized",
          detail: "authentication required",
          errors: [{ message: "x", location: "require_auth", value: "INVALID_CREDENTIALS" }],
        },
        { status: 401 },
      ),
    );
    seedToken("at-live", 900);
    const res = await authedFetch("/v1/me/thing");
    expect(res.status).toBe(401);
    expect(getAccessToken()).toBe("at-live");
  });

  it("a retry that 401s with a terminal code clears, with exactly one refresh and no second retry", async () => {
    const refresh = countRefresh(() =>
      HttpResponse.json({ accessToken: "at-new", expiresIn: 900 }),
    );
    const rec = recordRequests(() =>
      HttpResponse.json({ code: "session_revoked" }, { status: 401 }),
    );
    seedExpiredToken("at-old");

    const res = await authedFetch("/v1/me/thing");
    expect(res.status).toBe(401);
    expect(refresh.hits()).toBe(1);
    expect(rec.seen.length).toBe(2); // original + ONE retry
    expect(getAccessToken()).toBeNull();
    expect(hasSessionMarker()).toBe(false);
  });

  // Guards against "any retry 401 means signed out", which would sign out the
  // §4.4 mirror case for mistyping a password.
  it("a retry that 401s with NO code changes nothing", async () => {
    countRefresh(() => HttpResponse.json({ accessToken: "at-new", expiresIn: 900 }));
    const rec = recordRequests(() => new HttpResponse(null, { status: 401 }));
    seedExpiredToken("at-old");

    const res = await authedFetch("/v1/me/thing");
    expect(res.status).toBe(401);
    expect(rec.seen.length).toBe(2);
    expect(getAccessToken()).toBe("at-new"); // the refresh's token survives
    expect(hasSessionMarker()).toBe(true);
  });
});

describe("authedFetch body integrity (§5.13)", () => {
  // The assertion is on the CALLER'S view, not on bodyUsed. readError does
  // `await res.json()` wrapped in `.catch(() => ({}))`, so on an already-read
  // body the TypeError is SWALLOWED and the caller silently gets the fallback
  // message with no code at all — error branches quietly stop matching rather
  // than crashing.
  const readCallerView = async (res: Response) => {
    const body = (await res.json().catch(() => ({}))) as {
      detail?: string;
      code?: string;
    };
    return body;
  };

  it("a passed-through 401 (branch 2) is still readable downstream", async () => {
    recordRequests(() =>
      HttpResponse.json({ detail: "Invalid email or password", code: "auth.bad" }, { status: 401 }),
    );
    seedToken("at-live", 900);
    const res = await authedFetch("/v1/me/thing");
    expect(await readCallerView(res)).toEqual({
      detail: "Invalid email or password",
      code: "auth.bad",
    });
  });

  it("a 401 returned after an unavailable refresh (branch 4a) is still readable", async () => {
    countRefresh(() => new HttpResponse(null, { status: 503 }));
    recordRequests(() =>
      HttpResponse.json({ detail: "nope", code: "auth.bad" }, { status: 401 }),
    );
    seedExpiredToken("at-old");
    const res = await authedFetch("/v1/me/thing");
    expect(await readCallerView(res)).toEqual({ detail: "nope", code: "auth.bad" });
  });

  it("a RETRIED 401 is still readable, terminal or not", async () => {
    countRefresh(() => HttpResponse.json({ accessToken: "at-new", expiresIn: 900 }));
    recordRequests(() =>
      HttpResponse.json({ detail: "still no", code: "session_revoked" }, { status: 401 }),
    );
    seedExpiredToken("at-old");
    const res = await authedFetch("/v1/me/thing");
    expect(await readCallerView(res)).toEqual({
      detail: "still no",
      code: "session_revoked",
    });
  });

  it.each([
    ["a non-JSON body", new HttpResponse("<html>gateway</html>", { status: 401 })],
    ["an empty body", new HttpResponse(null, { status: 401 })],
  ])("%s is not terminal, does not throw, and stays readable", async (_l, response) => {
    recordRequests(() => response.clone());
    seedToken("at-live", 900);
    const res = await authedFetch("/v1/me/thing");
    expect(res.status).toBe(401);
    expect(getAccessToken()).toBe("at-live");
    await expect(res.text()).resolves.toBeDefined();
  });
});
```

- [ ] **Step 5: Write the failing delayed-401 tests (G8, branch 3)**

```ts
// The timing must be FORCED, never left to the scheduler: performRefresh
// clears inflightRefresh in a `finally` the moment the rotation resolves, so a
// 401 that comes back even slightly later finds no in-flight promise to join.
describe("a 401 answered after a sibling already rotated (§5.1, G8)", () => {
  it("takes branch 3: retries with the store's token and does NOT rotate again", async () => {
    const refresh = countRefresh(() =>
      HttpResponse.json({ accessToken: "at-new", expiresIn: 900 }),
    );
    let releaseB!: () => void;
    const bHeld = new Promise<void>((r) => {
      releaseB = r;
    });
    const seen: string[] = [];
    server.use(
      http.get(THING, async ({ request }) => {
        const which = request.headers.get("X-Which")!;
        const auth = request.headers.get("Authorization");
        seen.push(`${which}:${auth}`);
        if (which === "B" && auth === "Bearer at-old") await bHeld;
        return auth === "Bearer at-new"
          ? HttpResponse.json({ ok: true })
          : new HttpResponse(null, { status: 401 });
      }),
    );
    seedExpiredToken("at-old");

    const a = authedFetch("/v1/me/thing", { headers: { "X-Which": "A" } });
    const b = authedFetch("/v1/me/thing", { headers: { "X-Which": "B" } });
    // A completes its whole recovery first — including the rotation — and only
    // THEN is B's 401 released. "B's 401 comes back after the rotation" is a
    // fact of the test, not a hope.
    expect((await a).status).toBe(200);
    releaseB();
    const resB = await b;

    expect(resB.status).toBe(200); // NOT the stale 401
    expect(refresh.hits()).toBe(1); // NOT a second rotation
    expect(seen).toContain("B:Bearer at-new");
  });

  // Guards the ORDERING: a change-password rejection must not be replayed just
  // because a sibling rotated meanwhile. Branch 3 sits BEHIND the replay guard.
  it("a live-token 401 in the same situation is still passed through", async () => {
    const refresh = countRefresh(() =>
      HttpResponse.json({ accessToken: "at-new", expiresIn: 900 }),
    );
    recordRequests(() => new HttpResponse(null, { status: 401 }));
    setSessionMarker();
    setAccessToken("at-live", 900);
    // A sibling installs a different token while our request is in flight.
    const res = await authedFetch("/v1/me/thing", { method: "POST", body: "{}" });
    setAccessToken("at-sibling", 900);
    expect(res.status).toBe(401);
    expect(refresh.hits()).toBe(0);
  });

  it("an expired sent token with a null store token → branch 4, signed-out with no request", async () => {
    const refresh = countRefresh(() => HttpResponse.json({ accessToken: "x" }));
    recordRequests(() => new HttpResponse(null, { status: 401 }));
    // A sign-out landed mid-flight: no token, no marker.
    setAccessToken("at-old", -1);
    const p = authedFetch("/v1/me/thing");
    const res = await p;
    expect(res.status).toBe(401);
    void refresh;
  });
});
```

> The third case above is the hardest to force and the least valuable of the
> three. If a deterministic version cannot be written without reaching into
> module internals, **drop it and say so in the commit body** — do not ship a
> case whose timing is a hope. The first two carry the guarantee.

- [ ] **Step 6: Run and verify RED**

```bash
npx vitest run src/api/authedFetch.test.ts 2>&1 | tail -30
```

Expected: `TypeError: authedFetch is not a function` across the file.

- [ ] **Step 7: Implement the helper**

Create `frontend-client/src/api/authedFetch.ts`:

```ts
// The ONE authenticated request path, and the ONLY 401 algorithm in the tree.
//
// Before this, four near-copies of "attach bearer + credentials:'include'"
// existed (auth.ts::authedFetch, avatar.ts::authedJson,
// billingProfile.ts::authedJson, dsr.ts::postJson) plus a fifth, unreachable
// and unsafe one in client.ts. After it there is one, a new endpoint cannot
// forget the recovery, and there is no second implementation to drift from it
// or to be picked up by mistake.
//
// The refresh endpoint is called through tokenStore, never through this
// helper, so recursion is structurally impossible.
//
// A streaming body is unsupported: the retry re-sends from `init`, and a
// stream cannot be replayed. No call site has one.
import { apiBaseURL } from "@/api/client";
import {
  clearSessionLocally,
  getAccessTokenSnapshot,
  refreshAccessToken,
  refreshAfterUnauthorized,
} from "@/auth/tokenStore";

// The CLOSED set of 401 codes that mean the session itself is over. It is a
// MEMBERSHIP test and deliberately not a presence test: the middleware emits at
// least seven distinct top-level codes and four of them ride on 401s that are
// emphatically not a dead session (step_up_required, mfa_enrollment_required,
// password_confirm_required, audience_mismatch). `if (body.code) → clear` reads
// as the obvious simplification and would sign a user out for being asked to
// confirm a password. Adding to this set is a decision, not a chore.
const TERMINAL_CODES = new Set(["session_revoked", "session_max_age_reached"]);

// The server's own statement that it rejected the bearer BEFORE dispatch
// (RequireAuth, §3.D). It is proof the request never reached its handler, so
// it is safe to refresh and retry — including for a token that expired in
// flight, which our own reckoning cannot cover.
const CODE_ACCESS_TOKEN_EXPIRED = "access_token_expired";

// Reads a CLONE, never the response a caller will get. A body that is absent,
// not JSON, or carries no top-level `code` simply yields null — the ordinary
// case, not an error condition: the generic paths emit no top-level code,
// keeping their internal one in errors[0].value, which we deliberately do not
// read (it is CodeInvalidCredentials for an AuthenticationError, the same value
// a wrong password produces).
//
// The WWW-Authenticate shortcut is NOT available: that header is not in the
// API's CORS ExposedHeaders (cmd/server/middleware.go:103) and this SPA is
// cross-origin to the API host, so JS cannot read it. Do not "simplify" the
// clone away by reaching for it without adding the header to that list first.
async function read401Code(clone: Response): Promise<string | null> {
  const body = (await clone.json().catch(() => ({}))) as { code?: unknown };
  return typeof body.code === "string" ? body.code : null;
}

const isJsonBody = (body: BodyInit | null | undefined): boolean =>
  typeof body === "string";

function doFetch(
  path: string,
  init: RequestInit | undefined,
  token: string | null,
): Promise<Response> {
  // new Headers, NOT object spread. `init.headers` is a HeadersInit: a plain
  // object, a Headers instance, or an array of tuples. Spreading a Headers
  // yields {} — it has no own enumerable properties — so every header the
  // caller set is dropped SILENTLY; spreading a tuple array yields
  // {0: [...], 1: [...]}, which fetch then rejects or mangles.
  const headers = new Headers(init?.headers);
  if (!headers.has("Accept")) headers.set("Accept", "application/json");
  // Only for a body we KNOW is JSON. Forcing application/json on FormData
  // destroys the multipart boundary.
  if (!headers.has("Content-Type") && isJsonBody(init?.body)) {
    headers.set("Content-Type", "application/json");
  }
  // Last, via `set`: not appended, not conditional on absence. This is where
  // the precedence decision is enforced — a call site cannot override the
  // bearer, whatever shape it passed its headers in.
  if (token) headers.set("Authorization", `Bearer ${token}`);
  return fetch(`${apiBaseURL}${path}`, {
    ...init,
    // After the spread, so it always wins: the httpOnly refresh cookie is
    // Domain-scoped to the API host (ADR-0003 D-9) and only attaches when
    // credentials are explicitly included.
    credentials: "include",
    headers,
  });
}

// At most ONE retry per call, whichever branch produced it. The retry's own
// 401 is inspected too — but only for the terminal set. A codeless 401 there
// stays ambiguous (it can be the endpoint's own answer, the §4.4 mirror case
// being one), and clearing on it would sign out a user whose session is fine
// because they mistyped a password.
async function retryOnce(
  path: string,
  init: RequestInit | undefined,
  token: string,
): Promise<Response> {
  const retried = await doFetch(path, init, token);
  if (retried.status === 401) {
    const code = await read401Code(retried.clone());
    if (code !== null && TERMINAL_CODES.has(code)) {
      // The session died between the refresh and the retry. Leaving a token
      // the server rejects is defect A's broken state all over again (G3).
      clearSessionLocally();
    }
  }
  return retried;
}

export async function authedFetch(
  path: string,
  init?: RequestInit,
): Promise<Response> {
  // Captured together, BEFORE the fetch: at 401 time the store's expiry may
  // already belong to a token a sibling installed, and `sentAt` is the instant
  // the whole decision below turns on.
  const sent = getAccessTokenSnapshot();
  const sentAt = Date.now();
  const res = await doFetch(path, init, sent.token);
  if (res.status !== 401) return res;

  // A Response body is a single-use stream. Every inspection reads a CLONE, so
  // whatever we hand back is still unread — the caller's readError does
  // `await res.json()` inside a `.catch(() => ({}))`, so reading it here would
  // degrade SILENTLY into "fallback message, no code".
  const code = await read401Code(res.clone());

  // 1. Terminal: a token minted from the same cookie carries the same dead
  //    sid, so there is nothing to recover. No refresh, no retry.
  if (code !== null && TERMINAL_CODES.has(code)) {
    clearSessionLocally();
    return res;
  }

  // 2. THE REPLAY GUARD, and it sits ahead of every recovery branch.
  //
  //    Recovery is permitted only on PROOF that the request never reached its
  //    handler — otherwise a retry re-sends whatever it consumed. The
  //    motivating case: change-password answers 401 when the CURRENT PASSWORD
  //    IN THE BODY is wrong, and a replayed attempt is counted again, so two
  //    mistypes trip the lockout as though there had been four.
  //
  //    Two independent proofs, either sufficient:
  //      (a) the server says it rejected the bearer before dispatch (§3.D);
  //      (b) the token was ALREADY EXPIRED when it left — RequireAuth accepts
  //          a token until the instant it expires, with no grace, so this is
  //          the weakest client-side condition that still proves it.
  //    (b) is the fallback for a backend that has not shipped (a) yet.
  //
  //    NO MARGIN. A `SKEW` here is precisely the round-11 hole: a token with
  //    20s of life is still accepted by the server, so the handler DID run.
  //    An UNKNOWN expiry counts as live for the same reason — it cannot prove
  //    the handler did not run.
  const serverSaysExpired = code === CODE_ACCESS_TOKEN_EXPIRED;
  const provablyExpiredAtSend =
    sent.expiresAt !== null && sent.expiresAt <= sentAt;
  if (!serverSaysExpired && !provablyExpiredAtSend) return res;

  // 3. A sibling already rotated: prefer its token over a second rotation
  //    (G8). Safe by the same proof as branches 4a/4b, because this sits
  //    behind the guard above.
  const current = getAccessTokenSnapshot();
  if (current.token !== null && current.token !== sent.token) {
    return retryOnce(path, init, current.token);
  }

  // 4a/4b. Split on "did we send a bearer?", which is the honest question. A
  //    bearer in memory is proof a session existed, so refreshAccessToken's
  //    marker gate — which answers `signed-out` while clearing NOTHING — must
  //    not get to veto the cookie. A true anonymous visitor keeps the
  //    optimisation the gate was written for.
  const outcome =
    sent.token !== null
      ? await refreshAfterUnauthorized(apiBaseURL)
      : await refreshAccessToken(apiBaseURL);
  // `signed-out`: performRefresh has already cleared token AND marker (G3).
  // `unavailable`: token and marker untouched (G2, G7). Either way the caller
  // gets the original response, unread.
  if (outcome.status !== "ok") return res;
  return retryOnce(path, init, outcome.accessToken);
}
```

- [ ] **Step 8: Run the tests to verify they pass**

```bash
cd /home/tore/orkestra/frontend-client && npx vitest run src/api/authedFetch.test.ts 2>&1 | tail -25
```

Expected: PASS. Note **§5.8**: TanStack Query's `retry: 1` can produce a second
helper call with its own (coalesced) refresh — do not assert "exactly one
request" in any case where a query is involved. Every case above calls
`authedFetch` directly for that reason.

- [ ] **Step 9: Gate and commit**

```bash
make -C /home/tore/orkestra ci-frontend-client 2>&1 | tail -20
cd /home/tore/orkestra
export CLAUDE_SESSION="https://claude.ai/code/session_01QBHr35WPNoZZ1r2oNY7fDE"
git add frontend-client/src/api/authedFetch.ts \
        frontend-client/src/api/authedFetch.test.ts \
        frontend-client/src/auth/tokenStore.ts
git commit -m "$(printf '%s\n\n%s\n\n%s\n' \
  "feat(client): add authedFetch — one authenticated path, one 401 algorithm" \
  "Recovery is permitted only on proof the request never reached its handler: the server's own access_token_expired code, or a token that was already expired when it left. No margin — a margin is what let a wrong-current-password 401 be replayed and double-counted toward the lockout. A sibling's fresh token is preferred over a second rotation, a terminal code ends the session without a pointless refresh, and every body inspection reads a clone so the caller's readError still sees detail and code." \
  "Claude-Session: ${CLAUDE_SESSION:?set CLAUDE_SESSION first}")"
```

---

## Task 9: §4.6 call-site migration + §4.8 deleting the second 401 algorithm

`client.ts`'s `refreshMiddleware` is not a harmless stub waiting for codegen.
It is a **complete and wrong** implementation of what this spec specifies, and
every defect the document argues against is in it: it retries **any** 401 with
no token-state gate (replaying a `change-password` rejection); it re-sends
`request.body` from an already-sent `Request`, whose stream is disturbed; it
calls the marker-gated `refreshAccessToken`; it calls `clearAccessToken()`
without clearing the marker; and it inspects no code at all. None of it runs
today, which is exactly what makes it dangerous — it looks finished, the docs
that shipped before `82f25252` named it as the live path, and the first person
to `import { api }` inherits all five at once. That is the trap #325 was born
from.

**Files:**
- Modify: `frontend-client/src/api/auth.ts`, `avatar.ts`, `billingProfile.ts`, `dsr.ts`
- Modify: `frontend-client/src/api/client.ts`
- Modify: `frontend-client/CLAUDE.md`
- Modify: `docs/site/architecture/authentication-flow.mdx` (~line 226)

- [ ] **Step 1: Re-run the deletion's safety check across the whole fork chain**

The `@stripe/stripe-js` lesson: unused upstream, imported by commons, so
removing it would have broken the fork at the next sync. This is the opposite
case, but the measurement is one grep and is worth re-running rather than
trusting.

```bash
for repo in /home/tore/orkestra /home/tore/orkestra-commons /home/tore/orkestra-octolabs /home/tore/orkestra-gaterei /home/tore/orkestra-hermes; do
  [ -d "$repo/frontend-client/src" ] || { echo "$repo: no frontend-client"; continue; }
  echo "--- $repo"
  grep -rn 'from "@/api/client"' "$repo/frontend-client/src" | grep -v "apiBaseURL" || echo "  (only apiBaseURL — safe)"
done
```

Expected: every hit takes only `apiBaseURL`. **If any fork imports `api`, stop
and raise it** — the deletion becomes a coordinated change, not a local one.

- [ ] **Step 2: Migrate `src/api/auth.ts`**

Delete the local `authedFetch` (lines ~61-73) and import the shared helper.
`jsonFetch` — the **anonymous** path — is untouched and gains nothing: a 401
from `login`, `mfa/login/verify`, `register`, `forgot-password`,
`reset-password`, `accept-invite`, `policy`, `providers` or `oauth/login` means
"those credentials are wrong" or "not signed in", never "the token expired".

```ts
import { authedFetch } from "@/api/authedFetch";
```

and drop the now-unused `getAccessToken` import. Every existing
`authedFetch("/v1/auth/client/…", …)` call site keeps its exact shape.

Add a one-line comment above the import saying why `jsonFetch` stays separate,
so the next reader does not "finish the job".

- [ ] **Step 3: Migrate `src/api/avatar.ts`**

Delete `authedJson` (lines ~48-61) and replace its three call sites with
`authedFetch`. **`putAvatarBlob` is untouched** — it PUTs to the presigned
object-store URL with `credentials: 'omit'` and no bearer, which is
deliberate, not an oversight. Add a comment saying so at that function, since
it is now the only raw `fetch` left in the file.

- [ ] **Step 4: Migrate `src/api/billingProfile.ts`**

Delete `authedJson` (lines ~66-80) and replace its four call sites. Note this
file's local helper let **caller headers win over the bearer**, the opposite of
`avatar.ts`; the shared helper takes `avatar.ts`'s order. No call site passes
an `Authorization`, so nothing observable changes — but say it in the commit
body, because it is a behaviour unification, not a pure refactor.

- [ ] **Step 5: Migrate `src/api/dsr.ts` and unify its error shape**

```ts
import { authedFetch } from "@/api/authedFetch";

// DSR self-service calls (GDPR Art. 15 / 17). These hit core compliance
// endpoints that are not in the generated `paths` type, so they are
// hand-typed — but they go through the same authedFetch as every other
// authenticated call, so they inherit the 401 recovery rather than
// re-deriving it.
async function postJson<T>(path: string, body?: unknown): Promise<T> {
  const res = await authedFetch(path, {
    method: "POST",
    body: body !== undefined ? JSON.stringify(body) : undefined,
  });
  if (!res.ok) throw await readError(res, "Request failed");
  return (await res.json()) as T;
}
```

with the same `ApiError` / `readError` pair the siblings use (copy from
`auth.ts` or import it if it is exported — check, and prefer importing over a
fifth copy). **Verified free (O2 ruling):** `AccountDsrPage` has exactly two
error branches, both `isError` with fixed copy (lines 54 and 83), and neither
reads the message, the status or the code; there is no page test to update.

> _Out of scope, recorded in §8 as follow-up #6:_ those two strings are
> hard-coded English rather than `t()` calls, against this SPA's own i18n rule.
> A bug-fix PR is the wrong place to change user-visible copy.

- [ ] **Step 6: Delete the second 401 algorithm**

In `frontend-client/src/api/client.ts`, delete `authMiddleware`,
`refreshMiddleware`, the `api` export, both `api.use(...)` calls, and the
now-unused imports (`createClient`, `type Middleware`, `type paths`,
`getAccessToken`, `refreshAccessToken`, `clearAccessToken`). What remains is
what the file actually provides: the `window.__ORKESTRA_CONFIG__` resolution
and the `apiBaseURL` export every consumer already imports.

Add, at the top of the file:

```ts
// The API base-URL resolver, and nothing else.
//
// This file used to export an openapi-fetch client `api` with a bearer
// middleware and a 401 refresh-and-retry middleware. Nothing ever imported it,
// and it was WRONG in five ways at once — it retried every 401 including a
// wrong-password change-password, re-sent an already-consumed request body,
// used the marker-gated refresh, cleared the token but not the session marker,
// and inspected no error code. It read as the live request path (the docs said
// so until 82f25252) while nothing routed through it, which is the trap #325
// was born from, so it was deleted rather than left dormant.
//
// The live authenticated path is src/api/authedFetch.ts, and it is the ONLY
// 401 algorithm in this tree. openapi.gen.ts and the openapi-fetch dependency
// both stay — the codegen target the README documents, and what a future typed
// client will be built on; that client must DELEGATE to authedFetch's policy
// rather than restate it.
```

**`openapi.gen.ts` stays** and so does the `openapi-fetch` dependency, with one
honest consequence: Dependabot will keep proposing bumps for a package nothing
imports, and per this repo's own rule those are vacuous and should be closed
rather than merged. Dropping the dependency is a one-line follow-up (§8 #4); it
is deliberately not bundled here because a dependency removal propagates down
the fork chain on its own schedule.

- [ ] **Step 7: Verify the absence — the only way an absence can be verified**

```bash
cd /home/tore/orkestra/frontend-client
grep -rn '\bapi\b' src --include=*.ts --include=*.tsx | grep "@/api/client"   # expect: nothing
grep -rn "openapi-fetch" src                                                  # expect: nothing
npm run typecheck        # catches an orphaned `paths` import
npm run lint -- --max-warnings 0   # catches the unused imports left in client.ts
npm run build
npx vitest run           # the whole suite, proving nothing was routing through it
```

Expected: all clean, all green. `openapi-fetch` must remain in
`package.json` — only its *usage* is gone.

- [ ] **Step 8: Documentation (same commit) — the claim becomes true here**

**`frontend-client/CLAUDE.md`** — replace the "An expired token on an ordinary
call does _not_ silently refresh" paragraph (~line 112) entirely:

> **An expired access token on an authenticated call recovers silently.**
> Every authenticated request goes through `src/api/authedFetch.ts`, which
> attaches the bearer, sets `credentials:'include'`, and on a **401** decides
> in this order:
>
> | # | Condition | Action |
> | - | --------- | ------ |
> | 1 | the body carries a **terminal** top-level `code` (`session_revoked`, `session_max_age_reached`) | clear token **and** marker; no refresh, no retry — a token minted from the same cookie carries the same dead `sid` |
> | 2 | **no proof the handler never ran** — neither `code: access_token_expired` from the server nor a token that was already expired when it was sent | return the 401 **unchanged**: no refresh, no replay |
> | 3 | the store now holds a **different** token | a sibling already rotated — retry once with it, no refresh |
> | 4 | otherwise | refresh (un-gated when a bearer was sent, marker-gated when none was) and retry **once** |
>
> **Branch 2 is the replay guard and it sits ahead of every recovery branch.**
> `change-password` is an authenticated endpoint that answers **401** when the
> *current password in the body* is wrong, and the backend counts the failed
> attempt: a blanket "401 → refresh → retry" re-sends it, so two mistypes trip
> the lockout as though there had been four. Recovery is therefore permitted
> only on proof the request never reached its handler — the server's own
> `access_token_expired` code, or a token that was **already expired at send**.
> There is deliberately **no margin**: a token with 20 s of life left is still
> accepted by the server, so the handler ran. An **unknown** expiry counts as
> live for the same reason. Any future authenticated endpoint that answers 401
> for a body credential inherits the protection without being listed anywhere.
>
> At most **one** retry per call. The retry's own 401 is inspected for terminal
> codes only — a codeless 401 there stays ambiguous, and clearing on it would
> sign out a user whose session is fine because they mistyped a password.
> Every body inspection reads a `res.clone()`: the caller's `readError`
> swallows the `TypeError` from a consumed body, so reading the original would
> degrade *silently* into "fallback message, no code".
>
> `jsonFetch` (the **anonymous** path in `auth.ts`) is deliberately untouched —
> a 401 from `login`, `register`, `forgot-password`, `policy` or `providers`
> means "those credentials are wrong", never "the token expired".

Also update **item 1** and the `credentials` convention bullet in "How auth
works" to name `authedFetch` as the single path, and delete the sentence "The
middleware in `client.ts` reads it the same way, but nothing routes through
that client today."

**`docs/site/architecture/authentication-flow.mdx`** (~line 226) — the claim
"**Both SPAs implement it**" was **false when written**; it becomes true here.
Replace the `frontend-admin`-only substantiation with both, and add:

> `frontend-client` implements the same recovery in `src/api/authedFetch.ts`:
> it too serialises rotation across tabs with a Web Lock, coalesces concurrent
> 401s onto one in-flight refresh, and tolerates one `409
> refresh_rotation_raced` retry. It differs in two deliberate ways. It **only**
> refreshes on proof the request never reached its handler — the
> `access_token_expired` code, or a token already expired when sent — because
> an unconditional retry re-sends a wrong-current-password `change-password`
> and double-counts it toward the lockout. And **only a 401 from
> `/refresh-cookie` ends the session**: a 429, a 5xx, a timeout or a 2xx
> without a token keep both the access token and the session marker, since none
> of them says anything about the session.

- [ ] **Step 9: Gate and commit**

```bash
make -C /home/tore/orkestra ci-frontend-client 2>&1 | tail -20
cd /home/tore/orkestra
export CLAUDE_SESSION="https://claude.ai/code/session_01QBHr35WPNoZZ1r2oNY7fDE"
git add frontend-client/src/api/auth.ts \
        frontend-client/src/api/avatar.ts \
        frontend-client/src/api/billingProfile.ts \
        frontend-client/src/api/dsr.ts \
        frontend-client/src/api/client.ts \
        frontend-client/CLAUDE.md \
        docs/site/architecture/authentication-flow.mdx
git commit -m "$(printf '%s\n\n%s\n\n%s\n' \
  "refactor(client): route every authenticated call through authedFetch and delete the dead one" \
  "The four hand-rolled wrappers become one, so a new endpoint cannot forget the 401 recovery, and the unreachable openapi-fetch client in client.ts is deleted rather than left dormant: it was a complete and WRONG second 401 algorithm — retrying every 401, re-sending a consumed body, marker-gated, clearing the token but not the marker, inspecting no code — that read as the live path while nothing routed through it. Verified across upstream, commons, octolabs, gaterei and hermes: nobody imports it. openapi.gen.ts and the dependency stay for the typed client that will delegate to this policy." \
  "Claude-Session: ${CLAUDE_SESSION:?set CLAUDE_SESSION first}")"
```

---

## Task 10: Whole-branch verification

No production code. This is where the branch is proven, and where the two
things a unit suite cannot show are shown by hand.

- [ ] **Step 1: Full gate, both surfaces**

```bash
make -C /home/tore/orkestra ci-backend 2>&1 | tail -25
make -C /home/tore/orkestra ci-frontend-client 2>&1 | tail -20
```

Run them **sequentially** if a frontend-admin vitest run is in flight
elsewhere; never two vitest runs at once. Expected: `Backend CI: OK` and
`Frontend-client CI: OK`.

- [ ] **Step 2: Whitespace and diff hygiene**

```bash
cd /home/tore/orkestra
git diff --check $(git merge-base HEAD origin/dev)..HEAD
git log --oneline $(git merge-base HEAD origin/dev)..HEAD
```

Expected: no output from the first; the commits from Tasks 1–9 in
backend-then-client order from the second. **That ordering is a hard
dependency**, not a preference: §4.1's `401 → signed-out` row is only sound
once §4.9 has moved infrastructure failures off 401.

- [ ] **Step 3: Confirm the regression files were not edited**

```bash
cd /home/tore/orkestra
git diff --stat $(git merge-base HEAD origin/dev)..HEAD -- \
  frontend-client/src/App.test.tsx \
  frontend-client/src/auth/AuthProvider.test.tsx
```

Expected: **empty**. `LoginPage.test.tsx`, `OAuthCallbackPage.test.tsx` and
`auth.test.ts` are expected to have grown (Task 5's propagation cases) but must
show **additions only** for the first two; `auth.test.ts` may carry the one
sanctioned `?? 900` assertion change. Verify by reading the diff, not by
trusting the stat.

- [ ] **Step 4: Docs-site render**

```bash
df -h /tmp   # /tmp is a 16 GB tmpfs; stale orkestra-docs clones from earlier
             # sessions are 1-2 GB each and have caused ENOSPC mid-task.
cd "$(mktemp -d /tmp/orkestra-docs-XXXX)" && git clone --depth 1 https://github.com/orkestra-cc/orkestra-docs . \
  && npm ci \
  && MONOREPO_LOCAL_PATH=/home/tore/orkestra npm run sync \
  && CI=true npm run build
```

The **full** `npm run sync`, not `sync:site` — and note `sync:openapi` /
`sync:adrs` ignore `MONOREPO_LOCAL_PATH` and pull from `main`, so
`openapi-check` in `ci-backend` is what covers the dump. Expected: a clean
build. Nothing in this repo's CI builds the site, so this render is the only
gate on the three `docs/site/**` pages this branch touches.

- [ ] **Step 5: Manual verification — dev stack**

⚠️ **Staging cannot serve the client tier at all** while
`CLIENT_API_HOST=client-disabled.invalid` in `docker/.env` — every client-tier
call 401s regardless. Dev stack only.

Set `JWT_ACCESS_TOKEN_EXPIRY=60s` in `docker/.env` and restart the backend.
**60 s is the floor, not an arbitrary choice**: `NewJWTService` clamps anything
smaller up to `MinAccessTokenTTL` (`b3fdefee`), so a `10s` here silently
behaves as 60 and makes the wait look broken.

1. Sign in, wait past the TTL, act on `/account/security` → succeeds after
   exactly one `/refresh-cookie`.
2. Mistype the current password on change-password → **no** `/refresh-cookie`,
   and the attempt is sent **once**.
3. Two tabs **reloaded** together past the TTL → neither is signed out. This
   exercises `AuthProvider`'s mount refresh, *not* the 401 path — keep it, but
   it does not stand in for 4. **Run this one on unmodified `dev` first**, so
   there is something to compare against: it is the only way defect B's
   sign-out is observable today.

- [ ] **Step 6: Manual — two tabs crossing the TTL together (G6, G7, §4.1a)**

The scenario the Web Lock exists for. It needs two real browsing contexts
sharing one cookie jar, so it cannot be automated here.

1. Sign in at `http://client.localhost:8081` in **one browser profile** — a
   second profile does not share the jar and there is no race to observe.
2. Open a **second tab** on the same origin and navigate it to
   `/account/security` **before** the wait: `staleTime` is a 30 s global
   QueryClient default (`main.tsx:16`), so navigating after the wait serves
   cache and fires nothing.
3. DevTools → Network in **both** tabs, filter `refresh-cookie`, both
   recording. Note `localStorage.orkestra_client_session_marker`.
4. Wait **> 60 s** touching neither tab. Switching between them is safe:
   `refetchOnWindowFocus` is `false` (`main.tsx:14`).
5. Trigger an authenticated request in both tabs as close together as you can.
   Sub-second is enough — the lock is what makes the result deterministic, not
   the timing.

**Expected — and it is _two_ rotations, not one:**

- both requests succeed and **neither tab redirects to `/login`**;
- the session marker is **still present**;
- the two `/refresh-cookie` calls **do not overlap** — tab B's starts after tab
  A's has answered. That non-overlap *is* the Web Lock, and it is the only
  directly observable evidence of it;
- **no 409 appears**, because the lock prevented the race.

"Exactly one" is the tempting wrong expectation: branch 3 compares against
**this tab's** store, and each tab has its own module-scoped token, so tab B
legitimately needs its own. It simply must not be *signed out* while getting it.

- [ ] **Step 7: Manual — the same race with the lock removed (G7, the 409 retry)**

With Web Locks working the 409 path is unreachable by hand. In **tab B's
console only**, before triggering:

```js
Object.defineProperty(navigator, "locks", { value: undefined, configurable: true });
```

`withRefreshLock` reads `navigator.locks` at call time, so this takes effect
immediately and only for that tab. Run the race again.

**Expected:** tab B shows a **409 `refresh_rotation_raced` immediately followed
by a second `/refresh-cookie` that succeeds**, and stays signed in. One 409 is
the point of the test, not a fault.

**The failure signature to recognise:** tab B lands on `/login`, the marker is
**gone**, and there is **no** follow-up request after the 409. That means the
409 is still collapsing into `signed-out` — defect B.

- [ ] **Step 8: Record the residual that is inherited, not introduced**

Confirm the team knows §5.12's residual survives this work: a
`/refresh-cookie` response lost **before any headers arrive** may leave the
server rotated and the jar holding a superseded cookie, which after
`RefreshRotationGrace` (10 s) is replay detection and really does end the
session. ADR-0020's Consequences already record it; the timeout adds one more
way to reach it and the client cannot repair it — it has no way to learn a
token it never received. What it does do is fail safely at every step. Note
also that `REFRESH_FETCH_TIMEOUT_MS` (10 s) and `RefreshRotationGrace` (10 s)
are **equal by coincidence, not by design** — do not build on a retry landing
inside the grace.

- [ ] **Step 9: Final commit if anything moved**

If Steps 1–8 produced no changes, there is nothing to commit and the branch is
ready for review. If a doc render or a gate forced an edit, commit it with the
same trailer discipline and re-run Step 1.

---

## Follow-ups this branch does NOT do (spec §8, as amended in v17)

1. ~~Backend `access_token_expired`~~ — **done here** (Task 3, spec §4.10). The
   number is kept rather than reclaimed so the spec's cross-references stay
   meaningful.
2. **Proactive rotation for the client SPA** (ADR-0020 D3 parity). §4.5's
   `expiresAt` snapshot is the trustworthy remaining-lifetime figure it needs,
   and provides it *correctly under clock skew*, which is what makes a
   proactive scheme safe to build on. This is what introduces
   `PROACTIVE_REFRESH_SKEW_MS`; ADR-0020 D3's `SKEW < MinAccessTokenTTL` (60 s)
   invariant applies to it, and **it must not leak back into the 401
   comparison**.
3. **Wake up `openapi-fetch`** — sharpen `openapi.gen.ts` against a real
   backend, re-add the typed client with a middleware that **delegates to
   `authedFetch`'s policy** rather than restating it.
4. **Drop the `openapi-fetch` runtime dependency** if the vacuous Dependabot
   bumps outweigh the convenience.
5. **`frontend-admin`'s change-password replay** (§7). `changePassword`
   (`src/store/api/authApi.ts:449`) goes through `baseApi` and is **not** in
   `AUTH_ENDPOINT_PATHS`, so every wrong-password 401 triggers the silent
   refresh and re-sends the attempt. Its hazard is **wider** than the one
   closed here — it has no token-state gate at all, so the replay is not
   limited to a window near expiry. Task 3 makes the fix a one-liner (gate on
   `code === "access_token_expired"`); **it does not apply it.** Own PR (N3).
6. **`AccountDsrPage`'s hard-coded English error copy** — two strings that
   bypass `t()`.
7. **Align `frontend-admin`'s refresh timeout** with `AbortController` +
   `setTimeout` — its `AbortSignal.timeout` is not fake-timer controllable
   either, so any test it grows around a timeout inherits the same problem.
8. **`frontend-admin`'s 3-arg Web Lock test mock** — its own comment records
   that the test stays green while no longer exercising what it was written to
   exercise.

## Fork-chain note

`frontend-client/src/api/client.ts` is byte-identical across upstream, commons,
octolabs, gaterei and hermes, so Task 9's deletion will land on all four at the
next sync and conflict in none of them — **provided** Task 9 Step 1's grep is
re-run at sync time rather than trusted from today. `iface.ErrUserNotFound`
(Task 1) is an SDK addition and propagates the same way; a fork with its own
`UserProvider` implementation keeps compiling, and simply does not get the
not-found classification until it opts in.

---

## Self-review notes

Run against the spec after the plan was written, per the writing-plans skill.

**Spec coverage.** §4.1a/b/c/d/e → Tasks 5, 6, 7. §4.2 → Task 8 Step 2/7.
§4.3 (all four branches + the retry's own 401) → Task 8. §4.4 → Task 8's
live-token, boundary, unknown-expiry and 20 s cases. §4.5 → Tasks 4, 5. §4.6
(both tables) → Tasks 5, 9. §4.7 → distributed across Tasks 2, 3, 5, 6, 9 per
the same-commit rule, verified in Task 10 Step 4. §4.8 → Task 9. §4.9 → Tasks
1, 2, 2b — **including the picker** (spec v18), which is what makes Task 2
reachable from the browser at all (its proof is the HTTP-level tests in Task 2
Step 10, not the service tests), **and the race classifier** (spec v19), whose
proof is `revokeFamilyCalled() == 0`, not the status alone. §5 edge cases 1–13 → each has a test or an explicit note (5.5 in Task 6's
null-locks case; 5.8 as a warning in Task 8 Step 8; 5.10's "no `X-Retry`" is
satisfied by construction — the helper holds retry state in a local variable
and Task 9 deletes the only emitter; 5.12 is Task 10 Step 8). §6 → the tests
are transcribed into Tasks 4–8; §6's manual procedure → Task 10 Steps 5–7.
§7 → Task 10 Steps 1–3 and the fork-chain note.

**Known gaps, deliberate.**
- §6 asks for a `jwtExp` case on "a two-segment string" and `null`/`""` — all
  present. It also implies a `SKEW` constant survives for follow-up #1; **it
  does not exist in this plan at all**, because R1 makes follow-up #1 land here
  and the skew belongs to follow-up #2. Do not add it.
- Task 8's third delayed-401 case is marked droppable if it cannot be forced
  deterministically. That is a deliberate instruction, not a placeholder.
- Task 5's second and third LoginPage cases are written as commented skeletons
  pointing at an existing test in the same file for their exact shape. Fill
  them from that test; do not invent a new interaction.

**Type consistency.** `setAccessToken(token, expiresInSeconds?)`,
`getAccessTokenSnapshot(): {token, expiresAt}`, `signIn(token,
expiresInSeconds?)`, `refreshAfterUnauthorized(apiBase)`,
`clearSessionLocally()`, `authedFetch(path, init?)`,
`jwtExp(token): number | null` — each is defined once and used with the same
signature everywhere it appears. `expiresAt` is always in the `Date.now()`
domain (ms); `expiresIn`/`expiresInSeconds` is always **seconds**. Backend:
`iface.ErrUserNotFound`, `services.ErrRefreshLookupUnavailable`,
`refresh_lookup_unavailable`, `access_token_expired`.
