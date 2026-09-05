# Auth/Authz Audit Remediation — PR A: Attempt Counters and Lockout — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace the process-local, unsynchronised `shared/errors.RateLimiter` lockout with Redis fixed-window counters driven by one atomic Lua script, so that an anonymous request can no longer race the config map into a fatal runtime error (H-1) and so that lockout finally means what the admin UI says — N failures per window, per IP and per email, across replicas, with no existence oracle (M-5, M-6, M-7, M-8).

**Architecture:** One new service, `AttemptCounter`, owns every "N per window" decision in the auth module. Both of its reads (`Locked` peek, `RecordFailure` increment) execute the *same* Lua script in one round trip, so count, TTL and orphan-healing are a single atomic step; errors are returned, never absorbed, and every caller fails **open** to the durable `User.LockedUntil` rule. `PasswordAuthService.Login` gains a peek-first / record-on-failure order in which every non-success branch pays the argon2 cost, which is what closes the 429-versus-401 and timing oracles. Forgot-password and resend-verification move to their own request-cap scopes and hand the mail to a bounded dispatcher (256-job queue, 16 workers) so the response time no longer depends on whether an account existed. The in-memory limiter shrinks to the single `api:general` per-IP role it still serves, and every remaining read of its config map takes the lock.

**Tech Stack:** Go 1.26.8, Huma v2.39.1 (`huma.HeadersError` for `Retry-After`), Redis 8.2 via `database.RedisClientAdapter.Eval`, `github.com/alicebob/miniredis/v2 v2.38.0` (its Lua engine runs the real script), `pkg/sdk/metrics` Prometheus collector, MongoDB 8 for the durable lock.

**Spec:** `docs/superpowers/specs/2026-09-03-auth-authz-audit-remediation-design.md` **v1.12** — this plan implements the **PR A** row of §7, i.e. §4.1 decisions **D1–D10** in full, plus the §4.11 documentation lines that describe them and the §6 "PR A — counters" test list. PRs B, C, D1 and D2 get their own plans. Read the spec alongside this plan: every task cites the decision it implements, and the spec carries the rationale this plan does not repeat.

## Global Constraints

- **Branch:** `feat/auth-authz-audit-remediation` (carries the spec + this plan), branched from `dev` @ `a242e6bd`. PR A targets `dev`. PR B branches from `dev` after PR A merges (D20 uses these counters); PR C is independent and may run in parallel.
- **Redis counters fail OPEN; the durable lock is the second line.** A `Locked` error reads as *not locked*. A `RecordFailure` error yields no verdict, and the durable write falls back to the existing `FailedLoginCount+1 >= threshold` rule for that attempt. A fail-closed counter would turn a Redis outage into a platform-wide login outage — the stance ADR-0017 D5 already took for session revocation (`backend/internal/core/auth/CLAUDE.md:1050`).
- **Errors are returned, never absorbed.** Every `AttemptCounter` method returns `(Verdict, error)` / `error`. The implementation records `orkestra_auth_attempt_store_failures_total{operation}` and a throttled WARN, then returns the error. No method may swallow one into a `(0, false)`.
- **One round trip per counter read.** `Locked` and `RecordFailure` both `EVAL` the same script. No path may issue `INCR` and `EXPIRE` as two commands — that is the orphan-key defect (`oauth_state_service.go:343-354`) this PR removes, and for a lockout counter an orphan is a permanent 429 until someone runs `DEL`.
- **The IP scope has its own, much higher, threshold.** `ipLockoutThreshold` default **100**, `ipLockoutDuration` default **15m**, admin-managed, validated `>= accountLockoutThreshold`. Sharing the account pair would lock a whole office NAT for fifteen minutes after five wrong passwords among its users — worse than today's 1 token/s bucket.
- **Every non-success login branch pays the argon2 cost.** Inactive account, service principal, durable-lock hit and counter-lock hit each run `s.passwordService.Verify(in.Password, s.passwordService.DummyHash())` before returning. Today `password_auth_service.go:581-592, 593-596` return without it.
- **`ErrAccountLocked` and `ErrClientRateLimited` map to 429 `auth.too_many_attempts` with a `Retry-After` header**, never below 1 second, on every route that can raise them.
- **The `mfa-verify` scope is defined in this PR but consumed in PR B (D20).** Ship the constant and the key builder here; do not wire `/mfa/verify` to it.
- **`CEDAR_ENFORCE_ACTIONS` is not touched.** Not this PR, not this spec (§4.5 D25).
- **Docs move in the same commit as the code** (repo rule, `feedback_commit_doc_hygiene`): `backend/internal/core/auth/CLAUDE.md`, `docs/site/modules/core/auth.mdx`, `backend/CLAUDE.md` error-code list, as each task touches them.
- **Test commands** (run from `/home/tore/orkestra/backend` unless stated):
  - after every backend step: `go test ./internal/core/auth/... -count=1`
  - before every commit: `go vet ./...` (a `go build` does **not** compile `_test.go` — see `project_fork_sync_v037`)
  - race probe: `go test ./internal/shared/errors/ -race -count=1`
  - full gate: `make -C /home/tore/orkestra ci-backend`
  - live-Mongo guarded tests: `MONGO_TEST_URI='mongodb://127.0.0.1:28017/?directConnection=true'`
- **Never start servers manually** (Docker + AIR own them). **Never `git push --tags`.**
- **Commit trailer:** every commit message ends with
  `Claude-Session: https://claude.ai/code/session_0155Lt3QwR9zX15oGNKrn1T1`
  Bash starts a fresh shell per call — export nothing, paste the trailer literally into each `git commit` heredoc.

## Declared deviations from the spec (read before executing)

1. **The metric families land in Task 2, before the counter that emits them.** The spec lists telemetry as D10, last in §4.1. `pkg/sdk/metrics` additions are standalone with their own test, and putting them first means Task 3's counter compiles against a real collector rather than a stub.
2. **`ScriptRedisClient` lives in `attempt_counter.go`, not next to `LeaseRedisClient`.** Both are narrow `RedisClient` extensions; the spec names the shape but not the file. Keeping it with its only consumer matches how `AtomicTakeRedisClient` sits in `oauth_state_service.go` with the state store.
3. **A `MemoryAttemptCounter` ships alongside the Redis one.** The spec says "the in-memory store the tests use is atomic by construction" about the *state* store; the counter needs the same affordance so handler-level tests in PRs B and D do not each stand up a miniredis. It is test-support code in a non-`_test.go` file for the same reason `NewMemoryOAuthStateStore` is.
4. **`Verdict.Locked` is computed by the caller-side wrapper, not inside Lua.** The script returns `{count, ttlMillis}`; the Go side compares against `limit.Threshold`. This is what makes edge case 3 (threshold lowered mid-window) work without a script change, and keeps the script free of policy.
5. **`ChangePasswordInput` gains `IP string`** (spec D6) **and the handler at `password_handler.go` fills it from `clientIPFromCtx`.** The spec names the field; this plan names the fill site explicitly because the input struct is constructed in more than one handler.

## File Structure

**Backend — `backend/internal/core/auth/services/` (package `services`)**

| File | Responsibility | Task |
|---|---|---|
| `attempt_counter.go` (new) | `Limit`, `Verdict`, `AttemptCounter`, `ScriptRedisClient`, `attemptScript`, `redisAttemptCounter`, `MemoryAttemptCounter`, key builders, scope constants, request-cap constants | 3 |
| `attempt_counter_test.go` (new) | miniredis script behaviour: threshold, window expiry, concurrency, orphan healing, `RetryAfter`, `Reset`, store-error propagation | 3 |
| `oauth_state_service.go` | `RedisOAuthStateStore.Incr` switches to `attemptScript`; `RedisClient` unchanged, `NewRedisOAuthStateStore` takes a client that also has `Eval` | 5 |
| `mfa_challenge_service.go` | unchanged code, new test coverage for TTL healing | 5 |
| `mail_dispatcher.go` (new) | `MailJob`, `MailDispatcher`, `MailQueueCapacity = 256`, `MailWorkers = 16`, non-blocking `Enqueue`, `Start`/`Stop` with 10 s drain | 6 |
| `mail_dispatcher_test.go` (new) | concurrency bound, queue capacity, goroutine count, enqueue latency parity, drain-on-stop | 6 |
| `auth_policy_service.go` | + `IPLockoutThreshold`, `IPLockoutDuration` accessors + `defaultIPLockoutThreshold`, `defaultIPLockoutDuration` | 4 |
| `password_auth_service.go` | `RateLimiter` → `AttemptCounter` in config + struct; `Login` reorder (D3/D4); `ForgotPassword`/`ResendVerification` (D5); `ChangePassword`/`ConfirmPasswordWithSecurity` (D6); `recordFailed` deleted | 7, 8, 9 |
| `service_account_service.go` | limiter → counter on `Grant` + `recordFailed` (D7) | 10 |

**Backend — elsewhere**

| File | Responsibility | Task |
|---|---|---|
| `internal/shared/errcode/errcode.go` | `Error.Headers`, `WithHeader`, `GetHeaders` (satisfies `huma.HeadersError`) | 1 |
| `internal/shared/errcode/codes.go` | + `AuthTooManyAttempts = "auth.too_many_attempts"` | 1 |
| `internal/shared/errcode/codes_test.go` | golden row for the new code | 1 |
| `internal/core/auth/handlers/password_handler.go` | `ErrAccountLocked` → coded 429 + `Retry-After`; `ChangePasswordInput.IP` fill | 1, 9 |
| `internal/core/auth/handlers/service_token_handler.go` | `ErrClientRateLimited` → coded 429 + `Retry-After` | 10 |
| `internal/core/auth/handlers/error_mapping_test.go` | row `AccountLocked → 429` gains the slug | 1 |
| `pkg/sdk/metrics/metrics.go` | 3 new families + `Record*` methods | 2 |
| `pkg/sdk/metrics/metrics_test.go` | registration + bounded-label tests | 2 |
| `internal/core/auth/module.go` | `ScriptRedisClient` boot requirement; counter construction; `ipLockout*` schema fields; `rateLimiter` construction removed | 3, 4, 11 |
| `internal/core/auth/config_validation.go` | `ipLockoutThreshold >= accountLockoutThreshold` rule | 4 |
| `internal/core/auth/config_validation_test.go` | the new rule | 4 |
| `internal/core/auth/tier_bundle.go` | `rateLimiter` → `attemptCounter` in `tierBundleDeps` | 3, 11 |
| `internal/shared/errors/rate_limiter.go` | shrink to `Check` + `Middleware` + `api:general`; lock every config read; lock `bucket.tokens` read | 11 |
| `internal/shared/errors/rate_limiter_test.go` | replaced by a `-race` concurrency probe | 11 |
| `internal/shared/errors/errors_test.go` | `TestSetAuthFailedConfig` deleted | 11 |
| `cmd/server/config_declarations_test.go` | field-count / declaration assertions for the 2 new keys | 4 |
| `internal/core/auth/config_groups_test.go` | `login` group field count | 4 |

**Docs**

| File | Responsibility | Task |
|---|---|---|
| `backend/internal/core/auth/CLAUDE.md` | `:28, :116, :185, :341, :1071, :1091` limiter sentences; new "Attempt counters" section; policy table gains the IP pair | 4, 7, 11, 12 |
| `docs/site/modules/core/auth.mdx` | `:22, :126, :131` + new lockout paragraph | 12 |
| `backend/CLAUDE.md` | error-code contract example list gains `auth.too_many_attempts` | 1 |

---

## Task 1: The wire shape of a lockout (D9)

Nothing about counters yet — just the 429 envelope every later task returns. Independently shippable and independently rejectable.

**Files:**
- Modify: `backend/internal/shared/errcode/errcode.go`
- Modify: `backend/internal/shared/errcode/codes.go`
- Modify: `backend/internal/shared/errcode/codes_test.go`
- Modify: `backend/internal/core/auth/handlers/password_handler.go:433`
- Modify: `backend/internal/core/auth/handlers/error_mapping_test.go:50`
- Modify: `backend/CLAUDE.md`
- Test: `backend/internal/shared/errcode/errcode_headers_test.go` (new)

**Interfaces:**
- Consumes: nothing.
- Produces:
  - `errcode.AuthTooManyAttempts` (const string `"auth.too_many_attempts"`)
  - `func (e *Error) WithHeader(k, v string) *Error`
  - `func (e *Error) GetHeaders() http.Header`
  - `func TooManyRequests(code, detail string) *Error` — 429 builder
  - helper in the auth handlers: `func lockoutError(retryAfter time.Duration) error`

- [ ] **Step 1: Write the failing header test**

Create `backend/internal/shared/errcode/errcode_headers_test.go`:

```go
package errcode

import (
	"net/http"
	"testing"

	"github.com/danielgtaylor/huma/v2"
)

// The 429 envelope is useless to a client that cannot tell when to come
// back, and Huma only copies headers off an error that satisfies
// huma.HeadersError. Pin both halves.
func TestError_SatisfiesHumaHeadersError(t *testing.T) {
	var _ huma.HeadersError = (*Error)(nil)
}

func TestWithHeader_SetsAndAccumulates(t *testing.T) {
	e := TooManyRequests(AuthTooManyAttempts, "Too many failed attempts.").
		WithHeader("Retry-After", "42")

	if e.Status != http.StatusTooManyRequests {
		t.Fatalf("Status = %d, want 429", e.Status)
	}
	if e.Code != "auth.too_many_attempts" {
		t.Fatalf("Code = %q, want auth.too_many_attempts", e.Code)
	}
	got := e.GetHeaders().Get("Retry-After")
	if got != "42" {
		t.Fatalf("Retry-After = %q, want 42", got)
	}
}

// Headers must never reach the JSON body — the envelope is a frozen
// wire contract ({status,title,detail,code}).
func TestHeaders_NotSerialised(t *testing.T) {
	e := TooManyRequests(AuthTooManyAttempts, "d").WithHeader("Retry-After", "1")
	b, err := json.Marshal(e)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if strings.Contains(string(b), "Retry-After") || strings.Contains(string(b), "Headers") {
		t.Fatalf("headers leaked into the body: %s", b)
	}
}

func TestGetHeaders_NilSafeOnUnadornedError(t *testing.T) {
	e := New(http.StatusTooManyRequests, AuthTooManyAttempts, "d")
	if h := e.GetHeaders(); h == nil {
		t.Fatal("GetHeaders must return a non-nil (possibly empty) Header")
	}
}
```

Add `encoding/json` and `strings` to that file's imports.

- [ ] **Step 2: Run it to verify it fails**

Run: `go test ./internal/shared/errcode/ -run 'Headers|TooManyRequests' -count=1`
Expected: FAIL — `undefined: TooManyRequests`, `undefined: AuthTooManyAttempts`, `e.GetHeaders undefined`.

- [ ] **Step 3: Add the code constant**

In `backend/internal/shared/errcode/codes.go`, next to the other `Auth*` constants:

```go
// AuthTooManyAttempts signals that a per-IP, per-email or per-client
// attempt counter reached its threshold inside its window, or that the
// account carries a durable lock that has not yet expired. Always 429,
// always accompanied by a Retry-After header carrying the remaining
// life of the window (never below 1 second).
//
// It deliberately covers BOTH the unknown-address and the known-address
// case: answering 429 for one and 401 for the other would be an
// existence oracle, which is the defect (M-7) the counters close.
const AuthTooManyAttempts = "auth.too_many_attempts"
```

- [ ] **Step 4: Add the golden row**

In `backend/internal/shared/errcode/codes_test.go`, inside `goldenCodes`, next to the other auth rows:

```go
	"AuthTooManyAttempts":                "auth.too_many_attempts",
```

- [ ] **Step 5: Add headers to `Error`**

In `backend/internal/shared/errcode/errcode.go`, change the struct and add the builders:

```go
type Error struct {
	Status int    `json:"status"`
	Title  string `json:"title,omitempty"`
	Detail string `json:"detail"`
	Code   string `json:"code,omitempty"`

	// Headers are copied onto the HTTP response by Huma via the
	// huma.HeadersError interface and are NEVER serialised into the
	// body — the {status,title,detail,code} envelope is a frozen wire
	// contract. Retry-After on a 429 is the motivating case.
	Headers http.Header `json:"-"`
}

// GetHeaders implements huma.HeadersError. Returns a non-nil, possibly
// empty Header so callers never have to nil-check.
func (e *Error) GetHeaders() http.Header {
	if e.Headers == nil {
		return http.Header{}
	}
	return e.Headers
}

// WithHeader attaches one response header and returns the receiver so it
// composes with the named builders:
//
//	errcode.TooManyRequests(errcode.AuthTooManyAttempts, detail).
//	    WithHeader("Retry-After", "15")
func (e *Error) WithHeader(k, v string) *Error {
	if e.Headers == nil {
		e.Headers = http.Header{}
	}
	e.Headers.Set(k, v)
	return e
}

// TooManyRequests returns a 429 with the given code + detail.
func TooManyRequests(code, detail string) *Error {
	return New(http.StatusTooManyRequests, code, detail)
}
```

- [ ] **Step 6: Run the errcode tests**

Run: `go test ./internal/shared/errcode/ -count=1`
Expected: PASS — including `TestCodesMatchGoldenSnapshot` and `TestEveryConstSnapshotted`.

- [ ] **Step 7: Write the failing handler-mapping test**

In `backend/internal/core/auth/handlers/error_mapping_test.go`, change the `AccountLocked` row:

```go
		{"AccountLocked → 429 auth.too_many_attempts", services.ErrAccountLocked, http.StatusTooManyRequests, errcode.AuthTooManyAttempts},
```

and append a new test to the same file:

```go
// A 429 with no Retry-After tells the caller to guess. Every lockout
// answer carries one, and it is never below 1 second (a "come back in
// 0 seconds" is an invitation to hot-loop).
func TestMapPasswordError_AccountLockedCarriesRetryAfter(t *testing.T) {
	err := mapPasswordError(services.ErrAccountLocked)

	var ce *errcode.Error
	if !errors.As(err, &ce) {
		t.Fatalf("want *errcode.Error, got %T", err)
	}
	ra := ce.GetHeaders().Get("Retry-After")
	if ra == "" {
		t.Fatal("Retry-After missing")
	}
	n, convErr := strconv.Atoi(ra)
	if convErr != nil || n < 1 {
		t.Fatalf("Retry-After = %q, want an integer >= 1", ra)
	}
}
```

Add `strconv` to that file's imports.

- [ ] **Step 8: Run it to verify it fails**

Run: `go test ./internal/core/auth/handlers/ -run TestMapPasswordError -count=1`
Expected: FAIL — the current mapping returns `huma.Error429TooManyRequests`, which is not an `*errcode.Error` and carries no headers.

- [ ] **Step 9: Map the sentinel**

In `backend/internal/core/auth/handlers/password_handler.go`, add the helper next to `clientIPFromCtx`:

```go
// lockoutRetryAfterFallback is what a lockout answers when the caller
// could not supply a window remainder — the counter was unavailable and
// the durable rule fired, or the sentinel arrived through a path that
// does not carry a verdict. One minute is short enough to be a real
// retry hint and long enough not to be an invitation to hot-loop.
const lockoutRetryAfterFallback = time.Minute

// lockoutError renders the single 429 answer every attempt-counter and
// durable-lock branch returns. retryAfter is rounded UP to whole
// seconds and floored at 1: a "Retry-After: 0" is worse than none.
func lockoutError(retryAfter time.Duration) error {
	if retryAfter <= 0 {
		retryAfter = lockoutRetryAfterFallback
	}
	secs := int(math.Ceil(retryAfter.Seconds()))
	if secs < 1 {
		secs = 1
	}
	return errcode.TooManyRequests(errcode.AuthTooManyAttempts,
		"Too many failed attempts. Please try again later.").
		WithHeader("Retry-After", strconv.Itoa(secs))
}
```

Add `math` and `strconv` to the imports. Then replace the mapping arm:

```go
	case errors.Is(err, services.ErrAccountLocked):
		return lockoutError(services.RetryAfterFor(err))
```

and add to `backend/internal/core/auth/services/password_auth_service.go`, next to the sentinel declarations:

```go
// lockedError carries the remaining life of the window alongside
// ErrAccountLocked so the handler can render Retry-After without a
// second Redis read. errors.Is(err, ErrAccountLocked) still matches.
type lockedError struct{ retryAfter time.Duration }

func (e *lockedError) Error() string { return ErrAccountLocked.Error() }
func (e *lockedError) Is(target error) bool { return target == ErrAccountLocked }

// LockedAfter wraps ErrAccountLocked with a retry hint.
func LockedAfter(d time.Duration) error { return &lockedError{retryAfter: d} }

// RetryAfterFor extracts the retry hint from an error produced by
// LockedAfter, or 0 when the error carries none.
func RetryAfterFor(err error) time.Duration {
	var le *lockedError
	if stderrors.As(err, &le) {
		return le.retryAfter
	}
	return 0
}
```

- [ ] **Step 10: Run the handler tests**

Run: `go test ./internal/core/auth/handlers/ -count=1`
Expected: PASS.

- [ ] **Step 11: Update the backend error-code contract doc**

In `backend/CLAUDE.md`, find the "Error-code contract" example list and add a line in the same style as its neighbours:

```
- `auth.too_many_attempts` — 429; an attempt counter reached its threshold inside its window, or the account carries an unexpired durable lock. Always carries `Retry-After` (integer seconds, never below 1).
```

- [ ] **Step 12: Vet and commit**

```bash
go vet ./... && go test ./internal/shared/errcode/ ./internal/core/auth/handlers/ -count=1
cd /home/tore/orkestra && git add backend/internal/shared/errcode backend/internal/core/auth/handlers backend/internal/core/auth/services/password_auth_service.go backend/CLAUDE.md
git commit -m "$(cat <<'EOF'
feat(auth): add the auth.too_many_attempts wire shape with Retry-After

errcode.Error gains an optional Headers field, a WithHeader builder and
GetHeaders, which makes it satisfy huma.HeadersError so Huma copies the
header onto the response. ErrAccountLocked now maps to a coded 429
carrying Retry-After instead of an anonymous huma.Error429TooManyRequests.

Spec §4.1 D9. No behaviour change yet: the counters that produce a real
window remainder land in the next commits; until then the mapping uses
the one-minute fallback.

Claude-Session: https://claude.ai/code/session_0155Lt3QwR9zX15oGNKrn1T1
EOF
)"
```

---

## Task 2: Telemetry families (D10, and the D5 drop counter)

Three new counter families on the SDK collector. ADR-0002 freezes the *label schema*, so every label here is a closed, caller-collapsed set, exactly as `RecordSessionRevocationStoreFailure` and `RecordSessionAnchorAnomaly` do — a caller bug must never be able to turn a user UUID, an email or an IP into a Prometheus label.

**Files:**
- Modify: `backend/pkg/sdk/metrics/metrics.go`
- Test: `backend/pkg/sdk/metrics/attempt_counter_metrics_test.go` (new)

**Interfaces:**
- Consumes: nothing.
- Produces:
  - `func (c *Collector) RecordAuthAttemptStoreFailure(operation string)` — `operation ∈ {peek, record, reset}`, else `unknown`
  - `func (c *Collector) RecordAuthLockout(scope string)` — `scope ∈ {ip, email, client, reset-email, reset-ip, verify-email, verify-ip, mfa-verify}`, else `unknown`
  - `func (c *Collector) RecordAuthMailDropped(template string)` — `template ∈ {auth.reset_password, auth.verify_email, auth.mfa_factor_added}`, else `unknown`

- [ ] **Step 1: Write the failing test**

Create `backend/pkg/sdk/metrics/attempt_counter_metrics_test.go`:

```go
package metrics

import (
	"strings"
	"testing"

	"github.com/prometheus/client_golang/prometheus/testutil"
)

func TestRecordAuthAttemptStoreFailure_CountsByOperation(t *testing.T) {
	c := NewCollector()
	if err := c.Register(); err != nil {
		t.Fatalf("Register: %v", err)
	}
	c.RecordAuthAttemptStoreFailure("peek")
	c.RecordAuthAttemptStoreFailure("peek")
	c.RecordAuthAttemptStoreFailure("record")

	if got := testutil.ToFloat64(c.attemptStoreFailures.WithLabelValues("peek")); got != 2 {
		t.Errorf("peek = %v, want 2", got)
	}
	if got := testutil.ToFloat64(c.attemptStoreFailures.WithLabelValues("record")); got != 1 {
		t.Errorf("record = %v, want 1", got)
	}
}

// ADR-0002: the label schema is frozen and cardinality is bounded by the
// caller collapsing anything unexpected. An email address or an IP must
// never become a time series.
func TestRecordAuthAttemptStoreFailure_CollapsesUnknownOperation(t *testing.T) {
	c := NewCollector()
	c.RecordAuthAttemptStoreFailure("victim@example.com")
	if got := testutil.ToFloat64(c.attemptStoreFailures.WithLabelValues("unknown")); got != 1 {
		t.Errorf("unknown = %v, want 1 — an unexpected operation must collapse", got)
	}
}

func TestRecordAuthLockout_CollapsesUnknownScope(t *testing.T) {
	c := NewCollector()
	c.RecordAuthLockout("ip")
	c.RecordAuthLockout("203.0.113.9")
	if got := testutil.ToFloat64(c.authLockouts.WithLabelValues("ip")); got != 1 {
		t.Errorf("ip = %v, want 1", got)
	}
	if got := testutil.ToFloat64(c.authLockouts.WithLabelValues("unknown")); got != 1 {
		t.Errorf("unknown = %v, want 1", got)
	}
}

func TestRecordAuthMailDropped_CollapsesUnknownTemplate(t *testing.T) {
	c := NewCollector()
	c.RecordAuthMailDropped("auth.reset_password")
	c.RecordAuthMailDropped("marketing.blast")
	if got := testutil.ToFloat64(c.authMailDropped.WithLabelValues("auth.reset_password")); got != 1 {
		t.Errorf("reset = %v, want 1", got)
	}
	if got := testutil.ToFloat64(c.authMailDropped.WithLabelValues("unknown")); got != 1 {
		t.Errorf("unknown = %v, want 1", got)
	}
}

// A nil collector is the "metrics not wired" case every Record* method
// already tolerates; the new three must not be the ones that panic.
func TestNewAuthRecorders_NilSafe(t *testing.T) {
	var c *Collector
	c.RecordAuthAttemptStoreFailure("peek")
	c.RecordAuthLockout("ip")
	c.RecordAuthMailDropped("auth.reset_password")
}

// Register must include the new families or they never leave the process.
func TestRegister_IncludesAuthAttemptFamilies(t *testing.T) {
	c := NewCollector()
	if err := c.Register(); err != nil {
		t.Fatalf("Register: %v", err)
	}
	c.RecordAuthLockout("email")
	families, err := c.registry.Gather()
	if err != nil {
		t.Fatalf("Gather: %v", err)
	}
	var names []string
	for _, f := range families {
		names = append(names, f.GetName())
	}
	joined := strings.Join(names, ",")
	for _, want := range []string{
		"orkestra_auth_attempt_store_failures_total",
		"orkestra_auth_lockouts_total",
		"orkestra_auth_mail_dropped_total",
	} {
		if !strings.Contains(joined, want) {
			t.Errorf("%s not registered; got %s", want, joined)
		}
	}
}
```

- [ ] **Step 2: Run it to verify it fails**

Run: `go test ./pkg/sdk/metrics/ -run 'AuthAttempt|AuthLockout|AuthMail|AuthRecorders' -count=1`
Expected: FAIL — `c.attemptStoreFailures undefined`, `c.RecordAuthLockout undefined`.

- [ ] **Step 3: Add the fields**

In `backend/pkg/sdk/metrics/metrics.go`, inside `type Collector struct`, after `sessionAnchorAnomalies`:

```go
	attemptStoreFailures *prometheus.CounterVec
	authLockouts         *prometheus.CounterVec
	authMailDropped      *prometheus.CounterVec
```

- [ ] **Step 4: Build them**

In `buildMetrics`, after the `sessionAnchorAnomalies` block:

```go
	c.attemptStoreFailures = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: "orkestra",
			Subsystem: "auth",
			Name:      "attempt_store_failures_total",
			Help:      "Count of Redis attempt-counter operations that could not be answered. Every one of these fails OPEN — the durable per-account lock is the second line — so a sustained non-zero rate means brute-force throttling is degraded, not that logins are failing.",
		},
		// operation is one of peek, record, reset; anything else
		// collapses to unknown (ADR-0002 cardinality bound).
		[]string{"operation"},
	)

	c.authLockouts = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: "orkestra",
			Subsystem: "auth",
			Name:      "lockouts_total",
			Help:      "Count of attempt-counter increments that reached their threshold. scope=\"ip\" moving is the credential-stuffing signal worth alerting on; scope=\"email\" is ordinary user error at low rates.",
		},
		// scope is the closed set declared in attempt_counter.go;
		// anything else collapses to unknown. NEVER the identifier
		// itself — that would make every attacked address a time series.
		[]string{"scope"},
	)

	c.authMailDropped = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: "orkestra",
			Subsystem: "auth",
			Name:      "mail_dropped_total",
			Help:      "Count of transactional auth emails dropped because the bounded dispatcher's queue was full. A drop is a lost mail the user can re-request inside the per-address caps; a non-zero rate means the queue or the worker count is undersized.",
		},
		// template is the notification template id — a finite set
		// declared in-tree; anything else collapses to unknown.
		[]string{"template"},
	)
```

- [ ] **Step 5: Register them**

In `Register`, extend the slice literal with the three new vectors:

```go
	for _, m := range []prometheus.Collector{c.cedarDivergence, c.cedarEnforced, c.capabilityDenied, c.sessionRevocationStoreFailures, c.sessionCapExpiries, c.sessionCapEventFailures, c.sessionAnchorAnomalies, c.tokenSweepDeleted, c.tokenSweepBacklog, c.tokenSweepDuration, c.entitlementLag, c.httpDuration, c.attemptStoreFailures, c.authLockouts, c.authMailDropped} {
```

- [ ] **Step 6: Add the Record methods**

After `RecordSessionAnchorAnomaly`:

```go
// attemptStoreOperations / lockoutScopes / droppedTemplates are the
// closed label sets. Anything outside them collapses to "unknown" so a
// caller bug cannot turn an email address, an IP or a user UUID into a
// Prometheus time series (ADR-0002).
var (
	attemptStoreOperations = map[string]struct{}{"peek": {}, "record": {}, "reset": {}}
	lockoutScopes          = map[string]struct{}{
		"ip": {}, "email": {}, "client": {},
		"reset-email": {}, "reset-ip": {},
		"verify-email": {}, "verify-ip": {},
		"mfa-verify": {},
	}
	droppedTemplates = map[string]struct{}{
		"auth.reset_password":  {},
		"auth.verify_email":    {},
		"auth.mfa_factor_added": {},
	}
)

func collapse(v string, allowed map[string]struct{}) string {
	if _, ok := allowed[v]; ok {
		return v
	}
	return "unknown"
}

// RecordAuthAttemptStoreFailure counts one Redis attempt-counter
// operation that could not be answered. The caller has already failed
// open; this is the signal that throttling is degraded.
func (c *Collector) RecordAuthAttemptStoreFailure(operation string) {
	if c == nil || c.attemptStoreFailures == nil {
		return
	}
	c.attemptStoreFailures.WithLabelValues(collapse(operation, attemptStoreOperations)).Inc()
}

// RecordAuthLockout counts one increment that reached its threshold.
func (c *Collector) RecordAuthLockout(scope string) {
	if c == nil || c.authLockouts == nil {
		return
	}
	c.authLockouts.WithLabelValues(collapse(scope, lockoutScopes)).Inc()
}

// RecordAuthMailDropped counts one transactional auth email dropped by a
// full dispatcher queue.
func (c *Collector) RecordAuthMailDropped(template string) {
	if c == nil || c.authMailDropped == nil {
		return
	}
	c.authMailDropped.WithLabelValues(collapse(template, droppedTemplates)).Inc()
}
```

- [ ] **Step 7: Document the families in the package doc**

In the `package metrics` doc comment, after the ADR-0017 sweep block and before the ADR-0002 paragraph, add:

```go
// The auth attempt counters (spec 2026-09-03 §4.1 D10) add three more:
//
//   - orkestra_auth_attempt_store_failures_total — Redis attempt-counter
//     operations that could not be answered, labelled by operation
//     (peek / record / reset). Every one fails OPEN to the durable
//     per-account lock, so this measures degraded throttling, not
//     failed logins.
//   - orkestra_auth_lockouts_total — increments that reached their
//     threshold, labelled by scope (ip / email / client / reset-* /
//     verify-* / mfa-verify). scope="ip" is the stuffing alert.
//   - orkestra_auth_mail_dropped_total — transactional auth emails
//     dropped by a full dispatcher queue, labelled by template id.
```

- [ ] **Step 8: Run the tests**

Run: `go test ./pkg/sdk/metrics/ -count=1`
Expected: PASS.

- [ ] **Step 9: Vet and commit**

```bash
go vet ./... && go test ./pkg/sdk/metrics/ -count=1
cd /home/tore/orkestra && git add backend/pkg/sdk/metrics
git commit -m "$(cat <<'EOF'
feat(metrics): add auth attempt-counter, lockout and mail-drop families

Three counter families for the Redis attempt counters landing next:
orkestra_auth_attempt_store_failures_total{operation},
orkestra_auth_lockouts_total{scope} and
orkestra_auth_mail_dropped_total{template}.

Every label is a closed set collapsed by the recorder, so a caller bug
cannot turn an email, an IP or a user UUID into a time series — the
ADR-0002 cardinality bound, same shape as RecordSessionAnchorAnomaly.

Spec §4.1 D10 (and the D5 drop counter).

Claude-Session: https://claude.ai/code/session_0155Lt3QwR9zX15oGNKrn1T1
EOF
)"
```

---

## Task 3: The AttemptCounter service and its atomic script (D1, D2 keys)

The substrate. One type, one script, one boot requirement. No caller changes yet — the counter is constructed and threaded into both tier bundles **alongside** the existing `rateLimiter`, so this task compiles and every existing test stays green (expand before contract; the limiter is removed in Task 11).

**Files:**
- Create: `backend/internal/core/auth/services/attempt_counter.go`
- Create: `backend/internal/core/auth/services/attempt_counter_test.go`
- Modify: `backend/internal/core/auth/module.go` (boot requirement + construction + bundle wiring)
- Modify: `backend/internal/core/auth/tier_bundle.go` (`attemptCounter` field)

**Interfaces:**
- Consumes: `metrics.Default().RecordAuthAttemptStoreFailure`, `RecordAuthLockout` (Task 2); `services.RedisClient` (`oauth_state_service.go:291-301`); `database.RedisClientAdapter.Eval` (`internal/shared/database/redis.go:166`).
- Produces:
  - `type Limit struct { Threshold int; Window time.Duration }`
  - `type Verdict struct { Count int64; Locked bool; RetryAfter time.Duration }`
  - `type AttemptCounter interface { Locked(ctx, key string, limit Limit) (Verdict, error); RecordFailure(ctx, key string, limit Limit) (Verdict, error); Reset(ctx, key string) error }`
  - `type ScriptRedisClient interface { RedisClient; Eval(ctx context.Context, script string, keys []string, args ...interface{}) (interface{}, error) }`
  - `func NewRedisAttemptCounter(client ScriptRedisClient, log *slog.Logger) AttemptCounter`
  - `func NewMemoryAttemptCounter() AttemptCounter`
  - key builders: `AttemptKeyIP(ip)`, `AttemptKeyEmail(aud PolicyAudience, email)`, `AttemptKeyClient(clientID)`, `AttemptKeyResetEmail(aud, email)`, `AttemptKeyResetIP(ip)`, `AttemptKeyVerifyEmail(aud, email)`, `AttemptKeyVerifyIP(ip)`, `AttemptKeyMFAVerify(aud, userUUID)`
  - scope constants: `ScopeIP`, `ScopeEmail`, `ScopeClient`, `ScopeResetEmail`, `ScopeResetIP`, `ScopeVerifyEmail`, `ScopeVerifyIP`, `ScopeMFAVerify`
  - request-cap limits: `ResetRequestsPerEmail`, `ResetRequestsPerIP`, `VerifyRequestsPerEmail`, `VerifyRequestsPerIP` (all `Limit` values)
  - `attemptScript` (the Lua source, exported to package tests only via the file)
- Later tasks rely on: `d.attemptCounter` on `tierBundleDeps`; `PasswordAuthConfig.AttemptCounter`; `ServiceAccountService`'s `counter` field.

- [ ] **Step 1: Write the failing counter tests**

Create `backend/internal/core/auth/services/attempt_counter_test.go`:

```go
package services

// These tests run the REAL Lua script: miniredis ships a Lua engine, so
// the atomicity, the PTTL read and the orphan healing are exercised
// rather than asserted. A pure fake would prove nothing about the one
// property that matters — that count, TTL and healing are a single step.

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"

	"github.com/orkestra/backend/internal/shared/database"
)

func newTestCounter(t *testing.T) (AttemptCounter, *miniredis.Miniredis) {
	t.Helper()
	mr := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = client.Close() })
	return NewRedisAttemptCounter(database.NewRedisClientAdapter(client), slog.Default()), mr
}

var testLimit = Limit{Threshold: 3, Window: 15 * time.Minute}

func TestAttemptCounter_LocksAtThresholdInsideWindow(t *testing.T) {
	c, _ := newTestCounter(t)
	ctx := context.Background()
	key := "auth:attempts:email:operator:a@example.com"

	for i := 1; i <= 2; i++ {
		v, err := c.RecordFailure(ctx, key, testLimit)
		if err != nil {
			t.Fatalf("RecordFailure %d: %v", i, err)
		}
		if v.Locked {
			t.Fatalf("locked at attempt %d, threshold is %d", i, testLimit.Threshold)
		}
	}

	v, err := c.RecordFailure(ctx, key, testLimit)
	if err != nil {
		t.Fatalf("RecordFailure 3: %v", err)
	}
	if !v.Locked || v.Count != 3 {
		t.Fatalf("Verdict = %+v, want Locked with Count 3", v)
	}
	if v.RetryAfter <= 0 || v.RetryAfter > testLimit.Window {
		t.Fatalf("RetryAfter = %v, want (0, %v]", v.RetryAfter, testLimit.Window)
	}
}

// A peek must never move the counter. IsBlocked's defect was exactly
// this: pre-checking cost a token, so a caller could trip its own
// lockout purely by asking.
func TestAttemptCounter_LockedNeverIncrements(t *testing.T) {
	c, _ := newTestCounter(t)
	ctx := context.Background()
	key := "auth:attempts:ip:203.0.113.7"

	for i := 0; i < 50; i++ {
		v, err := c.Locked(ctx, key, testLimit)
		if err != nil {
			t.Fatalf("Locked: %v", err)
		}
		if v.Locked || v.Count != 0 {
			t.Fatalf("peek %d moved the counter: %+v", i, v)
		}
	}

	if _, err := c.RecordFailure(ctx, key, testLimit); err != nil {
		t.Fatalf("RecordFailure: %v", err)
	}
	v, err := c.Locked(ctx, key, testLimit)
	if err != nil {
		t.Fatalf("Locked after record: %v", err)
	}
	if v.Count != 1 {
		t.Fatalf("Count = %d after 50 peeks + 1 record, want 1", v.Count)
	}
}

func TestAttemptCounter_WindowExpiryUnlocks(t *testing.T) {
	c, mr := newTestCounter(t)
	ctx := context.Background()
	key := "auth:attempts:email:operator:b@example.com"
	lim := Limit{Threshold: 2, Window: time.Minute}

	_, _ = c.RecordFailure(ctx, key, lim)
	v, _ := c.RecordFailure(ctx, key, lim)
	if !v.Locked {
		t.Fatal("want locked at threshold")
	}

	mr.FastForward(61 * time.Second)

	v, err := c.Locked(ctx, key, lim)
	if err != nil {
		t.Fatalf("Locked after expiry: %v", err)
	}
	if v.Locked || v.Count != 0 {
		t.Fatalf("Verdict = %+v after window expiry, want a clean slate", v)
	}
}

// Edge case 3: a threshold lowered while a window is open must lock
// immediately, and a raised one must unlock — because the comparison
// happens against the LIVE limit, not against a capacity frozen into a
// bucket at allocation time (the old getBucket defect).
func TestAttemptCounter_ThresholdChangeAppliesToOpenWindow(t *testing.T) {
	c, _ := newTestCounter(t)
	ctx := context.Background()
	key := "auth:attempts:email:operator:c@example.com"

	for i := 0; i < 3; i++ {
		_, _ = c.RecordFailure(ctx, key, Limit{Threshold: 10, Window: 15 * time.Minute})
	}

	tight, err := c.Locked(ctx, key, Limit{Threshold: 3, Window: 15 * time.Minute})
	if err != nil {
		t.Fatalf("Locked tight: %v", err)
	}
	if !tight.Locked {
		t.Fatal("lowering the threshold to 3 with a count of 3 must lock")
	}

	loose, err := c.Locked(ctx, key, Limit{Threshold: 99, Window: 15 * time.Minute})
	if err != nil {
		t.Fatalf("Locked loose: %v", err)
	}
	if loose.Locked {
		t.Fatal("raising the threshold above the count must unlock")
	}
}

// N concurrent failures must cost N. The RMW shape this replaces let N
// parallel guesses cost one.
func TestAttemptCounter_ConcurrentRecordFailureCountsAll(t *testing.T) {
	c, _ := newTestCounter(t)
	ctx := context.Background()
	key := "auth:attempts:ip:198.51.100.4"
	const n = 32

	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _ = c.RecordFailure(ctx, key, Limit{Threshold: 1000, Window: time.Minute})
		}()
	}
	wg.Wait()

	v, err := c.Locked(ctx, key, Limit{Threshold: 1000, Window: time.Minute})
	if err != nil {
		t.Fatalf("Locked: %v", err)
	}
	if v.Count != n {
		t.Fatalf("Count = %d, want %d — concurrent increments were lost", v.Count, n)
	}
}

// A counter key that exists with NO TTL is a permanent lockout: the
// INCR-then-EXPIRE shape leaves exactly this on a crash between the two
// commands. Both the peek and the increment must heal it.
func TestAttemptCounter_HealsOrphanKeyOnPeek(t *testing.T) {
	c, mr := newTestCounter(t)
	ctx := context.Background()
	key := "auth:attempts:email:operator:orphan@example.com"

	mr.Set(key, "9") // no TTL — the orphan
	if ttl := mr.TTL(key); ttl != 0 {
		t.Fatalf("precondition: TTL = %v, want none", ttl)
	}

	v, err := c.Locked(ctx, key, testLimit)
	if err != nil {
		t.Fatalf("Locked: %v", err)
	}
	if !v.Locked || v.Count != 9 {
		t.Fatalf("Verdict = %+v, want Locked with Count 9", v)
	}
	if mr.TTL(key) <= 0 {
		t.Fatal("the peek must have stamped a TTL — an orphan lockout can never expire")
	}

	mr.FastForward(testLimit.Window + time.Second)
	v, err = c.Locked(ctx, key, testLimit)
	if err != nil {
		t.Fatalf("Locked after healed window: %v", err)
	}
	if v.Locked {
		t.Fatal("a healed orphan must unlock on its own")
	}
}

func TestAttemptCounter_HealsOrphanKeyOnRecord(t *testing.T) {
	c, mr := newTestCounter(t)
	ctx := context.Background()
	key := "auth:attempts:ip:192.0.2.55"

	mr.Set(key, "1")
	if _, err := c.RecordFailure(ctx, key, testLimit); err != nil {
		t.Fatalf("RecordFailure: %v", err)
	}
	if mr.TTL(key) <= 0 {
		t.Fatal("the increment must have stamped a TTL on the orphan")
	}
}

func TestAttemptCounter_RetryAfterTracksPTTL(t *testing.T) {
	c, mr := newTestCounter(t)
	ctx := context.Background()
	key := "auth:attempts:email:operator:d@example.com"
	lim := Limit{Threshold: 1, Window: 10 * time.Minute}

	v, err := c.RecordFailure(ctx, key, lim)
	if err != nil {
		t.Fatalf("RecordFailure: %v", err)
	}
	if v.RetryAfter < 9*time.Minute || v.RetryAfter > 10*time.Minute {
		t.Fatalf("RetryAfter = %v, want ~10m", v.RetryAfter)
	}

	mr.FastForward(8 * time.Minute)
	v, err = c.Locked(ctx, key, lim)
	if err != nil {
		t.Fatalf("Locked: %v", err)
	}
	if v.RetryAfter > 2*time.Minute+time.Second || v.RetryAfter <= 0 {
		t.Fatalf("RetryAfter = %v after 8m of a 10m window, want ~2m", v.RetryAfter)
	}
}

func TestAttemptCounter_ResetClearsTheKey(t *testing.T) {
	c, _ := newTestCounter(t)
	ctx := context.Background()
	key := "auth:attempts:email:operator:e@example.com"

	for i := 0; i < 3; i++ {
		_, _ = c.RecordFailure(ctx, key, testLimit)
	}
	if err := c.Reset(ctx, key); err != nil {
		t.Fatalf("Reset: %v", err)
	}
	v, err := c.Locked(ctx, key, testLimit)
	if err != nil {
		t.Fatalf("Locked: %v", err)
	}
	if v.Count != 0 || v.Locked {
		t.Fatalf("Verdict = %+v after Reset, want a clean slate", v)
	}
}

// A store that cannot answer must say so. Absorbing the failure into
// "not locked" would make D4's fallback to the durable rule impossible
// to express — the caller would never know it had no verdict.
func TestAttemptCounter_StoreErrorIsReturned(t *testing.T) {
	c, mr := newTestCounter(t)
	ctx := context.Background()
	mr.Close() // every command from here on fails

	if _, err := c.Locked(ctx, "auth:attempts:ip:1.2.3.4", testLimit); err == nil {
		t.Error("Locked must return the store error, not absorb it")
	}
	if _, err := c.RecordFailure(ctx, "auth:attempts:ip:1.2.3.4", testLimit); err == nil {
		t.Error("RecordFailure must return the store error")
	}
	if err := c.Reset(ctx, "auth:attempts:ip:1.2.3.4"); err == nil {
		t.Error("Reset must return the store error")
	}
}

func TestAttemptCounter_ZeroThresholdNeverLocks(t *testing.T) {
	c, _ := newTestCounter(t)
	ctx := context.Background()
	key := "auth:attempts:ip:0.0.0.0"

	// A misedited policy that returns 0 must not turn the counter into a
	// deny-all — the same stance SetAuthFailedConfig took by ignoring
	// threshold < 1.
	v, err := c.RecordFailure(ctx, key, Limit{Threshold: 0, Window: time.Minute})
	if err != nil {
		t.Fatalf("RecordFailure: %v", err)
	}
	if v.Locked {
		t.Fatal("a threshold of 0 must be treated as unset, never as deny-all")
	}
}

// ===== key builders =====

func TestAttemptKeys_ShapeAndScoping(t *testing.T) {
	cases := []struct{ got, want string }{
		{AttemptKeyIP("203.0.113.1"), "auth:attempts:ip:203.0.113.1"},
		{AttemptKeyEmail(PolicyAudienceOperator, "A@Example.COM "), "auth:attempts:email:operator:a@example.com"},
		{AttemptKeyEmail(PolicyAudienceClient, "a@example.com"), "auth:attempts:email:client:a@example.com"},
		{AttemptKeyClient("svc-1"), "auth:attempts:client:svc-1"},
		{AttemptKeyResetEmail(PolicyAudienceOperator, "a@example.com"), "auth:attempts:reset-email:operator:a@example.com"},
		{AttemptKeyResetIP("203.0.113.1"), "auth:attempts:reset-ip:203.0.113.1"},
		{AttemptKeyVerifyEmail(PolicyAudienceClient, "a@example.com"), "auth:attempts:verify-email:client:a@example.com"},
		{AttemptKeyVerifyIP("203.0.113.1"), "auth:attempts:verify-ip:203.0.113.1"},
		{AttemptKeyMFAVerify(PolicyAudienceOperator, "u-1"), "auth:attempts:mfa-verify:operator:u-1"},
	}
	for _, c := range cases {
		if c.got != c.want {
			t.Errorf("key = %q, want %q", c.got, c.want)
		}
	}
}

// An empty IP must SKIP the scope, not share one bucket with every
// caller whose address could not be resolved — that shared bucket is
// what let one unresolvable client lock out all of them.
func TestAttemptKeyIP_EmptyYieldsNoKey(t *testing.T) {
	if k := AttemptKeyIP(""); k != "" {
		t.Fatalf("AttemptKeyIP(\"\") = %q, want \"\" so the caller skips the scope", k)
	}
	if k := AttemptKeyResetIP("  "); k != "" {
		t.Fatalf("AttemptKeyResetIP(blank) = %q, want \"\"", k)
	}
}

// ===== memory implementation =====

func TestMemoryAttemptCounter_MatchesRedisSemantics(t *testing.T) {
	c := NewMemoryAttemptCounter()
	ctx := context.Background()
	key := "auth:attempts:email:operator:mem@example.com"

	for i := 1; i <= 2; i++ {
		v, err := c.RecordFailure(ctx, key, testLimit)
		if err != nil {
			t.Fatalf("RecordFailure: %v", err)
		}
		if v.Locked {
			t.Fatalf("locked early at %d", i)
		}
	}
	v, _ := c.RecordFailure(ctx, key, testLimit)
	if !v.Locked || v.Count != 3 {
		t.Fatalf("Verdict = %+v, want Locked/3", v)
	}
	if v.RetryAfter <= 0 {
		t.Fatal("RetryAfter must be positive")
	}

	peek, _ := c.Locked(ctx, key, testLimit)
	if peek.Count != 3 {
		t.Fatalf("peek moved the counter: %+v", peek)
	}
	if err := c.Reset(ctx, key); err != nil {
		t.Fatalf("Reset: %v", err)
	}
	after, _ := c.Locked(ctx, key, testLimit)
	if after.Count != 0 {
		t.Fatalf("Count = %d after Reset, want 0", after.Count)
	}
}

func TestMemoryAttemptCounter_ErrorInjection(t *testing.T) {
	c := NewMemoryAttemptCounter().(*MemoryAttemptCounter)
	sentinel := errors.New("store down")
	c.FailWith(sentinel)

	if _, err := c.Locked(context.Background(), "k", testLimit); !errors.Is(err, sentinel) {
		t.Fatalf("Locked err = %v, want %v", err, sentinel)
	}
	if _, err := c.RecordFailure(context.Background(), "k", testLimit); !errors.Is(err, sentinel) {
		t.Fatalf("RecordFailure err = %v, want %v", err, sentinel)
	}
}
```

- [ ] **Step 2: Run it to verify it fails**

Run: `go test ./internal/core/auth/services/ -run 'AttemptCounter|AttemptKey|MemoryAttempt' -count=1`
Expected: FAIL to compile — `undefined: NewRedisAttemptCounter`, `undefined: Limit`, `undefined: AttemptKeyIP`, …

- [ ] **Step 3: Write the counter**

Create `backend/internal/core/auth/services/attempt_counter.go`:

```go
package services

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/orkestra/backend/pkg/sdk/metrics"
)

// Limit is "at most Threshold events per Window". Threshold < 1 means
// UNSET and never locks — a misedited policy must not become a
// deny-all, the same stance the limiter's SetAuthFailedConfig took.
type Limit struct {
	Threshold int
	Window    time.Duration
}

// Verdict is what the store said about a key. The zero Verdict is what
// a caller gets alongside a non-nil error, and it reads as "not locked"
// so a caller that ignores the error still fails open.
type Verdict struct {
	Count      int64
	Locked     bool
	RetryAfter time.Duration
}

// AttemptCounter is the single "N events per window" primitive of the
// auth module: login lockout, password-reset request caps,
// verification-resend caps, service-account grant lockout and (PR B) the
// authenticated MFA-verify cap all key into it.
//
// Every method returns its error. What an unavailable store MEANS is
// decided by each caller, and the decision is the same everywhere:
// FAIL OPEN and fall back to the durable rule. A Locked error reads as
// not locked; a RecordFailure error yields no verdict and the caller
// switches to User.FailedLoginCount for that attempt. A fail-closed
// counter would turn a Redis outage into a platform-wide login outage —
// the trade ADR-0017 D5 already rejected for session revocation.
type AttemptCounter interface {
	// Locked peeks. It never increments. A non-nil error means the
	// store could not answer.
	Locked(ctx context.Context, key string, limit Limit) (Verdict, error)
	// RecordFailure increments the key and reports the resulting state.
	RecordFailure(ctx context.Context, key string, limit Limit) (Verdict, error)
	// Reset deletes the key. A successful login clears the email scope.
	Reset(ctx context.Context, key string) error
}

// ScriptRedisClient is the narrow RedisClient extension the counter
// needs. database.RedisClientAdapter satisfies it (redis.go:166); the
// auth module refuses to boot without it, exactly as it does for the
// GETDEL contract (module.go:897-899).
type ScriptRedisClient interface {
	RedisClient
	Eval(ctx context.Context, script string, keys []string, args ...interface{}) (interface{}, error)
}

// attemptScript is BOTH reads. Running peek and increment through one
// script is what makes count, TTL and orphan-healing a single atomic
// step; the two-command INCR-then-EXPIRE shape it replaces
// (oauth_state_service.go Incr) leaves a key with no TTL when it fails
// between the commands — for a lockout counter that is a permanent 429
// until someone runs DEL by hand.
//
// KEYS[1] counter key
// ARGV[1] window in milliseconds
// ARGV[2] "1" to increment, "0" to peek
// returns {count, ttlMillis}; {0, -2} when the key does not exist.
//
// The threshold is deliberately NOT in here: Verdict.Locked is computed
// Go-side against the LIVE limit, so lowering accountLockoutThreshold
// mid-window locks immediately and raising it unlocks (edge case 3),
// with no capacity frozen into the key.
const attemptScript = `
local n
if ARGV[2] == "1" then
  n = redis.call('INCR', KEYS[1])
else
  n = tonumber(redis.call('GET', KEYS[1]) or '0')
end
if n == 0 then return {0, -2} end
local ttl = redis.call('PTTL', KEYS[1])
if ttl < 0 then
  redis.call('PEXPIRE', KEYS[1], ARGV[1])
  ttl = tonumber(ARGV[1])
end
return {n, ttl}
`

// Scope names. These are also the bounded label values of
// orkestra_auth_lockouts_total — keep the two lists in step.
const (
	ScopeIP          = "ip"
	ScopeEmail       = "email"
	ScopeClient      = "client"
	ScopeResetEmail  = "reset-email"
	ScopeResetIP     = "reset-ip"
	ScopeVerifyEmail = "verify-email"
	ScopeVerifyIP    = "verify-ip"
	ScopeMFAVerify   = "mfa-verify"
)

const attemptKeyPrefix = "auth:attempts:"

// Request caps stay named constants rather than admin config: a request
// cap has no legitimate reason to be tuned per install today, and adding
// schema keys is a separate decision (spec §4.1 D2).
//
// The per-ADDRESS caps are 60 per window, not 3. An egress address is
// not an account: a corporate NAT or VPN carries hundreds of people, and
// three reset requests among them must not silence the endpoint for
// everyone behind it.
var (
	ResetRequestsPerEmail  = Limit{Threshold: 3, Window: 15 * time.Minute}
	ResetRequestsPerIP     = Limit{Threshold: 60, Window: 15 * time.Minute}
	VerifyRequestsPerEmail = Limit{Threshold: 3, Window: 15 * time.Minute}
	VerifyRequestsPerIP    = Limit{Threshold: 60, Window: 15 * time.Minute}
)

// MFAVerifyLimit is the authenticated MFA-verify cap (spec D20). Defined
// here in PR A; wired to the handlers in PR B.
var MFAVerifyLimit = Limit{Threshold: MFAMaxAttempts, Window: MFAChallengeTTL}

// normaliseEmail applies the SAME normalisation Login does
// (password_auth_service.go: strings.ToLower(strings.TrimSpace(...))),
// so a counter keyed at signup and a counter keyed at login are the
// same counter.
func normaliseEmail(email string) string {
	return strings.ToLower(strings.TrimSpace(email))
}

// AttemptKeyIP returns "" for an unresolvable address so the caller
// SKIPS the IP scope. Sharing one "ip:" key among every caller with no
// resolvable address — today's behaviour — lets one such client lock out
// all of them.
func AttemptKeyIP(ip string) string { return addressKey(ScopeIP, ip) }

// AttemptKeyResetIP / AttemptKeyVerifyIP have the same empty-skips rule.
func AttemptKeyResetIP(ip string) string  { return addressKey(ScopeResetIP, ip) }
func AttemptKeyVerifyIP(ip string) string { return addressKey(ScopeVerifyIP, ip) }

func addressKey(scope, ip string) string {
	ip = strings.TrimSpace(ip)
	if ip == "" {
		return ""
	}
	return attemptKeyPrefix + scope + ":" + ip
}

// The email scopes are per audience: an operator and a client account
// sharing an address lock independently (edge case 4).
func AttemptKeyEmail(aud PolicyAudience, email string) string {
	return emailKey(ScopeEmail, aud, email)
}
func AttemptKeyResetEmail(aud PolicyAudience, email string) string {
	return emailKey(ScopeResetEmail, aud, email)
}
func AttemptKeyVerifyEmail(aud PolicyAudience, email string) string {
	return emailKey(ScopeVerifyEmail, aud, email)
}

func emailKey(scope string, aud PolicyAudience, email string) string {
	email = normaliseEmail(email)
	if email == "" {
		return ""
	}
	return attemptKeyPrefix + scope + ":" + string(aud) + ":" + email
}

// AttemptKeyClient — a client ID IS an account, so it carries the
// account pair, not the address pair.
func AttemptKeyClient(clientID string) string {
	clientID = strings.TrimSpace(clientID)
	if clientID == "" {
		return ""
	}
	return attemptKeyPrefix + ScopeClient + ":" + clientID
}

// AttemptKeyMFAVerify is per audience and per user UUID (never per
// email: the caller is already authenticated).
func AttemptKeyMFAVerify(aud PolicyAudience, userUUID string) string {
	userUUID = strings.TrimSpace(userUUID)
	if userUUID == "" {
		return ""
	}
	return attemptKeyPrefix + ScopeMFAVerify + ":" + string(aud) + ":" + userUUID
}

// ScopeOfKey recovers the scope label from a key so the metric does not
// need a second argument at every call site. Unknown shapes collapse to
// "unknown" in the recorder.
func ScopeOfKey(key string) string {
	rest := strings.TrimPrefix(key, attemptKeyPrefix)
	if i := strings.IndexByte(rest, ':'); i > 0 {
		return rest[:i]
	}
	return ""
}

// attemptWarningInterval throttles the store-unavailable WARN so a
// Redis outage produces one line a minute, not one per request — the
// sessionRevocationWarningInterval pattern
// (session_revocation_service.go:14, :172-181).
const attemptWarningInterval = time.Minute

type redisAttemptCounter struct {
	client ScriptRedisClient
	log    *slog.Logger

	mu          sync.Mutex
	lastWarning time.Time
}

// NewRedisAttemptCounter builds the production counter. client must
// support EVAL; the auth module verifies that at boot.
func NewRedisAttemptCounter(client ScriptRedisClient, log *slog.Logger) AttemptCounter {
	if log == nil {
		log = slog.Default()
	}
	return &redisAttemptCounter{client: client, log: log}
}

func (c *redisAttemptCounter) Locked(ctx context.Context, key string, limit Limit) (Verdict, error) {
	return c.eval(ctx, "peek", key, limit, false)
}

func (c *redisAttemptCounter) RecordFailure(ctx context.Context, key string, limit Limit) (Verdict, error) {
	v, err := c.eval(ctx, "record", key, limit, true)
	if err == nil && v.Locked {
		metrics.Default().RecordAuthLockout(ScopeOfKey(key))
	}
	return v, err
}

func (c *redisAttemptCounter) Reset(ctx context.Context, key string) error {
	if key == "" {
		return nil
	}
	if err := c.client.Del(ctx, key); err != nil {
		c.report(ctx, "reset", err)
		return fmt.Errorf("attempt counter reset: %w", err)
	}
	return nil
}

func (c *redisAttemptCounter) eval(ctx context.Context, op, key string, limit Limit, incr bool) (Verdict, error) {
	// An empty key means the caller resolved no identifier for this
	// scope (no client IP, no email). Skipping is the whole point: it
	// must not share a bucket with every other such caller.
	if key == "" {
		return Verdict{}, nil
	}
	window := limit.Window
	if window <= 0 {
		window = 15 * time.Minute
	}
	flag := "0"
	if incr {
		flag = "1"
	}

	raw, err := c.client.Eval(ctx, attemptScript, []string{key}, window.Milliseconds(), flag)
	if err != nil {
		c.report(ctx, op, err)
		return Verdict{}, fmt.Errorf("attempt counter %s: %w", op, err)
	}

	count, ttlMillis, err := parseAttemptResult(raw)
	if err != nil {
		c.report(ctx, op, err)
		return Verdict{}, fmt.Errorf("attempt counter %s: %w", op, err)
	}

	v := Verdict{Count: count}
	// Threshold < 1 is UNSET, never deny-all.
	if limit.Threshold >= 1 && count >= int64(limit.Threshold) {
		v.Locked = true
	}
	if ttlMillis > 0 {
		v.RetryAfter = time.Duration(ttlMillis) * time.Millisecond
	}
	return v, nil
}

// parseAttemptResult reads the {count, ttlMillis} pair. go-redis decodes
// a Lua table of integers as []interface{} of int64.
func parseAttemptResult(raw interface{}) (count, ttlMillis int64, err error) {
	arr, ok := raw.([]interface{})
	if !ok || len(arr) != 2 {
		return 0, 0, fmt.Errorf("unexpected script result %T", raw)
	}
	for i, v := range arr {
		n, ok := v.(int64)
		if !ok {
			return 0, 0, fmt.Errorf("unexpected script result element %d: %T", i, v)
		}
		if i == 0 {
			count = n
		} else {
			ttlMillis = n
		}
	}
	return count, ttlMillis, nil
}

// report increments the failure metric on every error and logs at most
// one WARN per attemptWarningInterval. The error is always returned to
// the caller regardless — the metric is the alerting signal, the return
// is what lets D4 know to fall back to the durable rule.
func (c *redisAttemptCounter) report(ctx context.Context, op string, err error) {
	metrics.Default().RecordAuthAttemptStoreFailure(op)

	c.mu.Lock()
	throttled := !c.lastWarning.IsZero() && time.Since(c.lastWarning) < attemptWarningInterval
	if !throttled {
		c.lastWarning = time.Now()
	}
	c.mu.Unlock()
	if throttled {
		return
	}
	c.log.WarnContext(ctx, "auth attempt counter store unavailable",
		slog.String("operation", op),
		slog.String("error", err.Error()),
	)
}

// ===== in-memory implementation =====

// MemoryAttemptCounter is the test / no-Redis stand-in, atomic by
// construction under one mutex. It ships in a non-_test.go file for the
// same reason NewMemoryOAuthStateStore does: handler tests across
// several packages need it without each standing up a miniredis.
type MemoryAttemptCounter struct {
	mu      sync.Mutex
	counts  map[string]int64
	expires map[string]time.Time
	failErr error
	now     func() time.Time
}

func NewMemoryAttemptCounter() AttemptCounter {
	return &MemoryAttemptCounter{
		counts:  map[string]int64{},
		expires: map[string]time.Time{},
		now:     time.Now,
	}
}

// FailWith makes every subsequent call return err, so a test can drive
// the documented fail-open path without killing a real store.
func (m *MemoryAttemptCounter) FailWith(err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.failErr = err
}

func (m *MemoryAttemptCounter) Locked(_ context.Context, key string, limit Limit) (Verdict, error) {
	return m.touch(key, limit, false)
}

func (m *MemoryAttemptCounter) RecordFailure(_ context.Context, key string, limit Limit) (Verdict, error) {
	return m.touch(key, limit, true)
}

func (m *MemoryAttemptCounter) Reset(_ context.Context, key string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.failErr != nil {
		return m.failErr
	}
	delete(m.counts, key)
	delete(m.expires, key)
	return nil
}

func (m *MemoryAttemptCounter) touch(key string, limit Limit, incr bool) (Verdict, error) {
	if key == "" {
		return Verdict{}, nil
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.failErr != nil {
		return Verdict{}, m.failErr
	}
	now := m.now()
	if exp, ok := m.expires[key]; ok && !now.Before(exp) {
		delete(m.counts, key)
		delete(m.expires, key)
	}
	window := limit.Window
	if window <= 0 {
		window = 15 * time.Minute
	}
	if incr {
		m.counts[key]++
		if _, ok := m.expires[key]; !ok {
			m.expires[key] = now.Add(window)
		}
	}
	n := m.counts[key]
	if n == 0 {
		return Verdict{}, nil
	}
	if _, ok := m.expires[key]; !ok {
		m.expires[key] = now.Add(window) // heal the orphan, as the script does
	}
	v := Verdict{Count: n, RetryAfter: time.Until(m.expires[key])}
	if limit.Threshold >= 1 && n >= int64(limit.Threshold) {
		v.Locked = true
	}
	if v.RetryAfter < 0 {
		v.RetryAfter = 0
	}
	return v, nil
}
```

- [ ] **Step 4: Run the counter tests**

Run: `go test ./internal/core/auth/services/ -run 'AttemptCounter|AttemptKey|MemoryAttempt' -count=1`
Expected: PASS (all 15).

- [ ] **Step 5: Require EVAL at boot and construct the counter**

In `backend/internal/core/auth/module.go`, immediately after the existing GETDEL check (`:897-899`):

```go
	// EVAL is required for the same reason GETDEL is: the attempt
	// counters are the only brute-force bound on every anonymous auth
	// surface, and the script is what makes their count/TTL/healing
	// atomic. A client that cannot run it would leave the platform with
	// no lockout at all, which must be a boot failure, not a silent
	// degradation.
	scriptRedis, ok := deps.RedisAdapter.(services.ScriptRedisClient)
	if !ok {
		return fmt.Errorf("auth: Redis adapter lacks EVAL support")
	}
	attemptCounter := services.NewRedisAttemptCounter(scriptRedis, logger)
```

- [ ] **Step 6: Thread it into both bundles**

In `backend/internal/core/auth/tier_bundle.go`, add to `tierBundleDeps` next to `rateLimiter`:

```go
	// attemptCounter is the Redis fixed-window counter behind every
	// lockout and request cap. rateLimiter is on its way out (spec D8)
	// and both are carried only while the callers migrate.
	attemptCounter services.AttemptCounter
```

and in `buildAuthTierBundle`'s `services.PasswordAuthConfig{...}` literal, next to `RateLimiter:`:

```go
		AttemptCounter:           d.attemptCounter,
```

In `backend/internal/core/auth/services/password_auth_service.go`, add the field to `PasswordAuthConfig` (next to `RateLimiter *sharederrors.RateLimiter`) and to the struct, and assign it in the constructor:

```go
	// PasswordAuthConfig
	AttemptCounter AttemptCounter

	// PasswordAuthService
	attempts AttemptCounter

	// NewPasswordAuthService
	attempts: cfg.AttemptCounter,
```

Finally, at both `buildAuthTierBundle` call sites in `module.go`, pass `attemptCounter: attemptCounter,` alongside the existing `rateLimiter: rateLimiter,`.

- [ ] **Step 7: Build, vet, run the whole auth suite**

Run:
```
go vet ./... && go test ./internal/core/auth/... -count=1
```
Expected: PASS — nothing consumes `s.attempts` yet, so no behaviour changed.

- [ ] **Step 8: Commit**

```bash
cd /home/tore/orkestra && git add backend/internal/core/auth
git commit -m "$(cat <<'EOF'
feat(auth): add the Redis attempt counter and its atomic script

One AttemptCounter primitive backs every "N events per window" decision
in the auth module. Both reads — the peek and the increment — run the
same Lua script, so count, PTTL and the healing of a key found without a
TTL are a single atomic step; the INCR-then-EXPIRE shape it replaces
leaves a permanent lockout behind on a failure between the commands.

Errors are returned, never absorbed: the metric is the alerting signal,
the return is what lets a caller fall back to the durable per-account
rule. The threshold is compared Go-side against the live limit, so an
admin lowering accountLockoutThreshold locks an open window immediately
instead of waiting for a bucket to be evicted.

The auth module now refuses to boot without EVAL, exactly as it does for
GETDEL. Nothing consumes the counter yet — callers migrate next.

Spec §4.1 D1, D2.

Claude-Session: https://claude.ai/code/session_0155Lt3QwR9zX15oGNKrn1T1
EOF
)"
```

---

## Task 4: The IP lockout policy pair and its validator rule (D2, second half)

Two admin-managed keys, two accessors, one cross-field validation rule. An IP threshold *tighter* than the account threshold turns a shared address into the account's existence oracle, so the validator refuses it.

**Files:**
- Modify: `backend/internal/core/auth/module.go` (schema, `login` group)
- Modify: `backend/internal/core/auth/services/auth_policy_service.go`
- Modify: `backend/internal/core/auth/config_validation.go`
- Modify: `backend/internal/shared/errcode/codes.go` + `codes_test.go`
- Modify: `backend/internal/core/auth/config_groups_test.go`
- Modify: `backend/cmd/server/config_declarations_test.go`
- Modify: `backend/internal/core/auth/CLAUDE.md` (policy table, "Login & Sessions" row)
- Test: `backend/internal/core/auth/services/auth_policy_service_test.go`, `backend/internal/core/auth/config_validation_test.go`

**Interfaces:**
- Consumes: `module.FieldInt`, `module.FieldDuration`, `module.ConfigValidationSnapshot`, `errcode` codes.
- Produces:
  - `func (s *AuthPolicyService) IPLockoutThreshold(ctx context.Context) int` (default 100)
  - `func (s *AuthPolicyService) IPLockoutDuration(ctx context.Context) time.Duration` (default 15m)
  - `errcode.AuthIPThresholdBelowAccount = "auth.ip_threshold_below_account"`
- Later tasks rely on: Task 7 reads both accessors to build the `ip` scope `Limit`.

- [ ] **Step 1: Write the failing accessor tests**

Append to `backend/internal/core/auth/services/auth_policy_service_test.go`:

```go
// The IP scope carries its OWN pair, and it is much looser than the
// account pair: an egress address is not an account. Five wrong
// passwords among the hundreds of people behind one office NAT must not
// lock the office out for fifteen minutes.
func TestIPLockoutThreshold(t *testing.T) {
	cases := []struct {
		name string
		val  string
		want int
	}{
		{"absent", "", 100},
		{"blank", "   ", 100},
		{"malformed", "many", 100},
		{"zero", "0", 100},
		{"negative", "-4", 100},
		{"valid", "250", 250},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			svc := newPolicyServiceWithValues(t, map[string]string{"ipLockoutThreshold": tc.val})
			if got := svc.IPLockoutThreshold(context.Background()); got != tc.want {
				t.Errorf("IPLockoutThreshold = %d, want %d", got, tc.want)
			}
		})
	}
}

func TestIPLockoutDuration(t *testing.T) {
	cases := []struct {
		name string
		val  string
		want time.Duration
	}{
		{"absent", "", 15 * time.Minute},
		{"malformed", "soon", 15 * time.Minute},
		{"zero", "0s", 15 * time.Minute},
		{"valid", "1h", time.Hour},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			svc := newPolicyServiceWithValues(t, map[string]string{"ipLockoutDuration": tc.val})
			if got := svc.IPLockoutDuration(context.Background()); got != tc.want {
				t.Errorf("IPLockoutDuration = %v, want %v", got, tc.want)
			}
		})
	}
}

// A nil service / nil config service must answer the defaults, not
// panic — the same nil-tolerance every other accessor has.
func TestIPLockoutAccessors_NilTolerant(t *testing.T) {
	var svc *AuthPolicyService
	if got := svc.IPLockoutThreshold(context.Background()); got != 100 {
		t.Errorf("threshold on nil service = %d, want 100", got)
	}
	if got := svc.IPLockoutDuration(context.Background()); got != 15*time.Minute {
		t.Errorf("duration on nil service = %v, want 15m", got)
	}
}
```

> If `newPolicyServiceWithValues` does not exist in that file, read the
> existing `TestLockoutThreshold` / `TestLockoutDuration` tests and reuse
> whatever fixture they use verbatim; do not invent a second harness.

- [ ] **Step 2: Run it to verify it fails**

Run: `go test ./internal/core/auth/services/ -run 'IPLockout' -count=1`
Expected: FAIL — `svc.IPLockoutThreshold undefined`.

- [ ] **Step 3: Add the accessors**

In `backend/internal/core/auth/services/auth_policy_service.go`, next to `defaultLockoutThreshold`:

```go
	// The IP pair is deliberately an order of magnitude looser than the
	// account pair. An egress address is not an account: one corporate
	// NAT or VPN carries hundreds of people, so the address threshold
	// exists to catch a stuffing run, not ordinary user error.
	defaultIPLockoutThreshold = 100
	defaultIPLockoutDuration  = 15 * time.Minute
```

and after `LockoutDuration`:

```go
// IPLockoutThreshold returns the number of failed attempts from one
// source address before that address is locked. Falls back to 100 when
// unset or invalid. Enforced >= LockoutThreshold at config-write time
// (config_validation.go): an IP lock tighter than the account lock turns
// a shared address into the account's existence oracle.
func (s *AuthPolicyService) IPLockoutThreshold(ctx context.Context) int {
	if s == nil || s.cs == nil {
		return defaultIPLockoutThreshold
	}
	v := strings.TrimSpace(s.cs.GetValue(ctx, "auth", "ipLockoutThreshold"))
	if v == "" {
		return defaultIPLockoutThreshold
	}
	n, err := strconv.Atoi(v)
	if err != nil || n < 1 {
		return defaultIPLockoutThreshold
	}
	return n
}

// IPLockoutDuration returns how long a source address stays locked.
// Falls back to 15m when unset or invalid.
func (s *AuthPolicyService) IPLockoutDuration(ctx context.Context) time.Duration {
	if s == nil || s.cs == nil {
		return defaultIPLockoutDuration
	}
	v := strings.TrimSpace(s.cs.GetValue(ctx, "auth", "ipLockoutDuration"))
	if v == "" {
		return defaultIPLockoutDuration
	}
	d, ok := utils.ParseDuration(v)
	if !ok || d <= 0 {
		return defaultIPLockoutDuration
	}
	return d
}
```

- [ ] **Step 4: Run the accessor tests**

Run: `go test ./internal/core/auth/services/ -run 'IPLockout' -count=1`
Expected: PASS.

- [ ] **Step 5: Write the failing validator test**

Append to `backend/internal/core/auth/config_validation_test.go`:

```go
// An IP threshold BELOW the account threshold makes the shared address
// lock before the account does, which turns "did this address get 429'd
// early?" into an oracle for whether an account exists behind it.
func TestValidateConfigSnapshot_RefusesIPThresholdBelowAccount(t *testing.T) {
	m := &AuthModule{}
	snap := module.ConfigValidationSnapshot{
		Values: map[string]string{
			"accountLockoutThreshold": "5",
			"ipLockoutThreshold":      "3",
		},
	}
	err := m.ValidateConfigSnapshot(context.Background(), snap)
	if err == nil {
		t.Fatal("want a validation error")
	}
	var ve *module.ConfigValidationError
	if !errors.As(err, &ve) {
		t.Fatalf("want *module.ConfigValidationError, got %T", err)
	}
	if ve.Field != "ipLockoutThreshold" {
		t.Errorf("Field = %q, want ipLockoutThreshold", ve.Field)
	}
	if ve.Code != errcode.AuthIPThresholdBelowAccount {
		t.Errorf("Code = %q, want %q", ve.Code, errcode.AuthIPThresholdBelowAccount)
	}
}

func TestValidateConfigSnapshot_AcceptsIPThresholdEqualToAccount(t *testing.T) {
	m := &AuthModule{}
	snap := module.ConfigValidationSnapshot{
		Values: map[string]string{
			"accountLockoutThreshold": "5",
			"ipLockoutThreshold":      "5",
		},
	}
	if err := m.ValidateConfigSnapshot(context.Background(), snap); err != nil {
		t.Fatalf("equality must be accepted: %v", err)
	}
}

// Absent keys mean "use the defaults" (5 and 100), which satisfy the
// rule — an operator who never touched either must not be refused.
func TestValidateConfigSnapshot_IPThresholdDefaultsPass(t *testing.T) {
	m := &AuthModule{}
	if err := m.ValidateConfigSnapshot(context.Background(), module.ConfigValidationSnapshot{
		Values: map[string]string{},
	}); err != nil {
		t.Fatalf("defaults must validate: %v", err)
	}
}

// A malformed value is not this rule's business — the field type
// already rejects it — so the rule must skip rather than mis-compare.
func TestValidateConfigSnapshot_IPThresholdMalformedSkipsRule(t *testing.T) {
	m := &AuthModule{}
	if err := m.ValidateConfigSnapshot(context.Background(), module.ConfigValidationSnapshot{
		Values: map[string]string{"ipLockoutThreshold": "lots", "accountLockoutThreshold": "5"},
	}); err != nil {
		t.Fatalf("a malformed value must not surface as the cross-field error: %v", err)
	}
}
```

- [ ] **Step 6: Run it to verify it fails**

Run: `go test ./internal/core/auth/ -run 'IPThreshold' -count=1`
Expected: FAIL — `undefined: errcode.AuthIPThresholdBelowAccount`, and no error returned.

- [ ] **Step 7: Add the code and the rule**

In `backend/internal/shared/errcode/codes.go`:

```go
// AuthIPThresholdBelowAccount signals a refused module-config write:
// ipLockoutThreshold must be >= accountLockoutThreshold. A source
// address that locks BEFORE the account does turns a shared egress into
// an existence oracle — the caller learns "an account is being attacked
// behind this NAT" from the early 429.
const AuthIPThresholdBelowAccount = "auth.ip_threshold_below_account"
```

and the golden row in `codes_test.go`:

```go
	"AuthIPThresholdBelowAccount":        "auth.ip_threshold_below_account",
```

In `backend/internal/core/auth/config_validation.go`, extend the entry point and add the rule:

```go
func (m *AuthModule) ValidateConfigSnapshot(_ context.Context, snap module.ConfigValidationSnapshot) error {
	if err := validateAuthDurations(snap.Values); err != nil {
		return err
	}
	if err := validateLockoutThresholdOrder(snap.Values); err != nil {
		return err
	}
	return validateLoginMethodInvariant(snap, services.ReadableNonEmptyFile)
}

// validateLockoutThresholdOrder enforces
// ipLockoutThreshold >= accountLockoutThreshold on the TARGET snapshot.
//
// Absent keys resolve to their schema defaults (5 and 100), so an
// operator who never touched either passes. A malformed value is left
// to the field-type check — mis-comparing it here would surface the
// wrong error for the wrong field.
func validateLockoutThresholdOrder(values map[string]string) error {
	account, ok := snapshotInt(values, "accountLockoutThreshold", 5)
	if !ok {
		return nil
	}
	ip, ok := snapshotInt(values, "ipLockoutThreshold", 100)
	if !ok {
		return nil
	}
	if ip >= account {
		return nil
	}
	return &module.ConfigValidationError{
		Field: "ipLockoutThreshold",
		Code:  errcode.AuthIPThresholdBelowAccount,
		Message: fmt.Sprintf(
			"must be at least the account threshold (%d): an address that locks before the account does turns a shared office or VPN egress into an oracle for which accounts exist behind it",
			account),
	}
}

// snapshotInt reads a positive integer from the snapshot. Returns
// ok=false for a malformed or non-positive value so the caller skips
// its rule rather than comparing against a guess.
func snapshotInt(values map[string]string, key string, def int) (int, bool) {
	raw, present := values[key]
	if !present {
		return def, true
	}
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return def, true
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n < 1 {
		return 0, false
	}
	return n, true
}
```

Add `strconv` to the file's imports.

- [ ] **Step 8: Run the validator tests**

Run: `go test ./internal/core/auth/ -run 'IPThreshold' -count=1 && go test ./internal/shared/errcode/ -count=1`
Expected: PASS.

- [ ] **Step 9: Declare the two schema fields**

In `backend/internal/core/auth/module.go`, in the `login` group immediately after `accountLockoutDuration`:

```go
		{
			Key: "ipLockoutThreshold", Label: "Failed attempts from one address before lockout", Group: "login",
			Description: "Number of failed attempts from a single source address before that address is temporarily locked. Deliberately much higher than the per-account threshold — one office egress or VPN can be hundreds of people, and locking the address on five wrong passwords among them would take the whole office offline. Must be at least the per-account threshold. Default 100.",
			Type:        module.FieldInt, Default: "100",
		},
		{
			Key: "ipLockoutDuration", Label: "Address lockout duration", Group: "login",
			Description: "Go duration string (e.g. 15m, 1h) — how long a source address stays locked after exceeding its threshold. Default 15m.",
			Type:        module.FieldDuration, Default: "15m",
		},
```

Also fix the stale comment above the group (`module.go:397-400`), which describes a mechanism this PR removes:

```go
		// Login & Sessions — per-surface kill switches + lockout policy.
		// Read at request time by AuthPolicyService. The account pair and
		// the address pair are read per attempt by the Redis attempt
		// counters (services/attempt_counter.go), so an admin edit takes
		// effect on the very next attempt, including one already inside
		// an open window.
```

- [ ] **Step 10: Update the two declaration tests**

Run `go test ./internal/core/auth/ -run ConfigGroups -count=1` and `go test ./cmd/server/ -run ConfigDeclarations -count=1`, read the failure messages, and update the expected `login`-group field count (+2) and any declaration snapshot in `cmd/server/config_declarations_test.go`. The contract asks for this deliberately (`backend/internal/core/auth/CLAUDE.md:161-168`) — do not loosen the assertions, update the numbers.

- [ ] **Step 11: Document the pair**

In `backend/internal/core/auth/CLAUDE.md`, in the policy table's "Login & Sessions" row, add the two keys next to `accountLockoutThreshold` / `accountLockoutDuration`, with the "address ≠ account" reason and the `>=` rule.

- [ ] **Step 12: Full gate and commit**

```bash
go vet ./... && go test ./internal/core/auth/... ./cmd/server/... ./internal/shared/errcode/ -count=1
git add backend/internal/core/auth backend/internal/shared/errcode backend/cmd/server
git commit -m "$(cat <<'EOF'
feat(auth): give the IP lockout scope its own admin-managed pair

ipLockoutThreshold (default 100) and ipLockoutDuration (default 15m)
join the login config group with IPLockoutThreshold/IPLockoutDuration
accessors, and ValidateConfigSnapshot refuses ipLockoutThreshold below
accountLockoutThreshold with 422 auth.ip_threshold_below_account.

An egress address is not an account: sharing the account pair would lock
a whole office NAT for fifteen minutes after five wrong passwords among
its users — worse than the 1 token/s bucket this replaces. An address
that locks BEFORE the account does is also an existence oracle, which is
what the ordering rule refuses.

Spec §4.1 D2.

Claude-Session: https://claude.ai/code/session_0155Lt3QwR9zX15oGNKrn1T1
EOF
)"
```

---

## Task 5: Move the MFA per-challenge cap onto the same script (D1, second half)

`RedisOAuthStateStore.Incr` is the other two-command counter in the tree, and it backs the MFA per-challenge cap. Today a failure between its `INCR` and its `EXPIRE` orphans the key forever; a recycled challenge id then inherits a used-up budget.

**Files:**
- Modify: `backend/internal/core/auth/services/oauth_state_service.go`
- Modify: `backend/internal/core/auth/module.go` (the store now needs `Eval` too)
- Test: `backend/internal/core/auth/services/mfa_challenge_service_test.go`

**Interfaces:**
- Consumes: `attemptScript` (Task 3), `ScriptRedisClient` (Task 3).
- Produces: `AtomicTakeRedisClient` gains `Eval`; `NewRedisOAuthStateStore` signature unchanged in shape but its parameter now also requires `Eval`.

- [ ] **Step 1: Write the failing healing test**

Append to `backend/internal/core/auth/services/mfa_challenge_service_test.go`:

```go
// A counter key with no TTL is an orphan: the challenge expires, its id
// space is reused, and the next holder inherits a spent budget. INCR +
// EXPIRE as two commands leaves exactly that behind on a failure
// between them; one script cannot.
func TestIncrementAttempts_HealsCounterKeyWithoutTTL(t *testing.T) {
	mr := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = client.Close() })
	store := NewRedisOAuthStateStore(database.NewRedisClientAdapter(client))
	svc := NewMFAChallengeService(store)

	ch, err := svc.Begin(context.Background(), "u-heal", MFAPurposeEnroll, "")
	if err != nil {
		t.Fatalf("Begin: %v", err)
	}

	// Simulate the orphan the two-command shape leaves behind.
	counterKey := buildMFAAttemptsKey(ch.ID)
	mr.Set(counterKey, "1")
	if ttl := mr.TTL(counterKey); ttl != 0 {
		t.Fatalf("precondition: TTL = %v, want none", ttl)
	}

	if _, err := svc.IncrementAttempts(context.Background(), ch.ID); err != nil {
		t.Fatalf("IncrementAttempts: %v", err)
	}
	if mr.TTL(counterKey) <= 0 {
		t.Fatal("the increment must stamp a TTL on an orphaned counter key")
	}
}
```

Add the miniredis / go-redis / `database` imports to that file if absent.

- [ ] **Step 2: Run it to verify it fails**

Run: `go test ./internal/core/auth/services/ -run TestIncrementAttempts_HealsCounterKeyWithoutTTL -count=1`
Expected: FAIL — the key keeps no TTL, because `Incr` only sets one when `n == 1`.

- [ ] **Step 3: Rewrite `Incr` on the script**

In `backend/internal/core/auth/services/oauth_state_service.go`:

```go
// AtomicTakeRedisClient is the narrow extension required by the state
// store: GETDEL for one-winner takes, EVAL for the attempt script.
type AtomicTakeRedisClient interface {
	RedisClient
	GetDel(ctx context.Context, key string) (string, error)
	Eval(ctx context.Context, script string, keys []string, args ...interface{}) (interface{}, error)
}

// Incr increments the counter and stamps the TTL in ONE round trip,
// through the same script the attempt counters use. The previous shape
// sent INCR and EXPIRE separately and only on the creating call, so a
// failure between them — or a key created by any other path — left a
// counter with no expiry. For the MFA per-challenge cap that is a
// budget a recycled challenge id inherits; for a lockout counter it
// would be a permanent 429.
func (r *RedisOAuthStateStore) Incr(ctx context.Context, key string, expiry time.Duration) (int64, error) {
	if expiry <= 0 {
		expiry = MFAChallengeTTL
	}
	raw, err := r.client.Eval(ctx, attemptScript, []string{key}, expiry.Milliseconds(), "1")
	if err != nil {
		return 0, err
	}
	n, _, err := parseAttemptResult(raw)
	if err != nil {
		return 0, err
	}
	return n, nil
}
```

Delete the now-unused `Incr`/`Expire` members from the `RedisClient` interface **only if** nothing else consumes them — check with
`grep -rn "\.Incr(ctx\|\.Expire(ctx" --include="*.go" internal/ | grep -v _test`
and leave them if any other caller remains. (The `RedisClient` contract is consumed by fork code; removing a method is not additive. If in doubt, keep them and note it in the commit body.)

- [ ] **Step 4: Run the MFA challenge tests**

Run: `go test ./internal/core/auth/services/ -run 'MFAChallenge|IncrementAttempts' -count=1`
Expected: PASS — including the existing `TestIncrementAttempts_ConcurrentCallersEachConsumeOne` and `TestIncrementAttempts_ExhaustionDeletesChallengeUnderConcurrency`, which run against `MemoryOAuthStateStore` and are unaffected.

- [ ] **Step 5: Vet, full auth suite, commit**

```bash
go vet ./... && go test ./internal/core/auth/... -count=1
cd /home/tore/orkestra && git add backend/internal/core/auth
git commit -m "$(cat <<'EOF'
fix(auth): make the MFA per-challenge counter atomic with its TTL

RedisOAuthStateStore.Incr sent INCR and EXPIRE as two commands and only
stamped the TTL on the call that created the key, so a failure between
them left a counter that never expires. For the MFA per-challenge cap
that is a spent budget a recycled challenge id inherits.

It now runs the same one-round-trip script the attempt counters use,
which also heals any key it finds without a TTL.

Spec §4.1 D1.

Claude-Session: https://claude.ai/code/session_0155Lt3QwR9zX15oGNKrn1T1
EOF
)"
```

---

## Task 6: The bounded mail dispatcher (D5, dispatcher half)

A synchronous `SendTemplated` inside `ForgotPassword` makes the response time depend on whether an account existed. Detaching it with a bare goroutine would let a flood create unbounded goroutines; a blocking semaphore acquire would reintroduce the very oracle the detach removes. So: a fixed queue, fixed workers, and a **non-blocking** enqueue that drops with a metric.

**Files:**
- Create: `backend/internal/core/auth/services/mail_dispatcher.go`
- Create: `backend/internal/core/auth/services/mail_dispatcher_test.go`
- Modify: `backend/internal/core/auth/module.go` (construct, start, stop)
- Modify: `backend/internal/core/auth/maintenance.go` (`Start` / `Stop` hooks)
- Modify: `backend/internal/core/auth/tier_bundle.go` (`mailDispatcher` field)

**Interfaces:**
- Consumes: `iface.NotificationSender`, `metrics.Default().RecordAuthMailDropped` (Task 2).
- Produces:
  - `const MailQueueCapacity = 256`, `const MailWorkers = 16`, `const mailDrainTimeout = 10 * time.Second`, `const mailJobTimeout = 60 * time.Second`
  - `type MailJob struct { TemplateID string; RequestID string; Send func(ctx context.Context) error }`
  - `type MailDispatcher struct{…}` with `NewMailDispatcher(log *slog.Logger) *MailDispatcher`, `Start()`, `Stop(ctx context.Context)`, `Enqueue(job MailJob) bool`
- Later tasks rely on: `PasswordAuthConfig.MailDispatcher`; `s.mail.Enqueue(...)` in Tasks 8 and (PR B) D13.

- [ ] **Step 1: Write the failing dispatcher tests**

Create `backend/internal/core/auth/services/mail_dispatcher_test.go`:

```go
package services

// The dispatcher's whole job is to be BOUNDED. These tests measure the
// bounds rather than asserting them in a comment: concurrency, queue
// capacity, goroutine count, enqueue latency and drain behaviour.

import (
	"context"
	"errors"
	"log/slog"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// blockingSender counts how many sends are in flight at once and holds
// each one until release is closed.
type blockingSender struct {
	release  chan struct{}
	inFlight atomic.Int64
	maxSeen  atomic.Int64
	started  chan struct{}
	done     atomic.Int64
}

func newBlockingSender() *blockingSender {
	return &blockingSender{release: make(chan struct{}), started: make(chan struct{}, 1024)}
}

func (b *blockingSender) send(ctx context.Context) error {
	n := b.inFlight.Add(1)
	for {
		m := b.maxSeen.Load()
		if n <= m || b.maxSeen.CompareAndSwap(m, n) {
			break
		}
	}
	select {
	case b.started <- struct{}{}:
	default:
	}
	<-b.release
	b.inFlight.Add(-1)
	b.done.Add(1)
	return nil
}

func (b *blockingSender) job() MailJob {
	return MailJob{TemplateID: "auth.reset_password", Send: b.send}
}

func TestMailDispatcher_ConcurrencyIsBoundedByWorkers(t *testing.T) {
	d := NewMailDispatcher(slog.Default())
	d.Start()
	sender := newBlockingSender()
	t.Cleanup(func() { close(sender.release); d.Stop(context.Background()) })

	// Enqueue more than the worker count; only MailWorkers may run.
	for i := 0; i < MailWorkers+8; i++ {
		if !d.Enqueue(sender.job()) {
			t.Fatalf("enqueue %d dropped while the queue has room", i)
		}
	}
	// Wait for the workers to pick their jobs up.
	deadline := time.After(2 * time.Second)
	for sender.inFlight.Load() < int64(MailWorkers) {
		select {
		case <-deadline:
			t.Fatalf("only %d workers started, want %d", sender.inFlight.Load(), MailWorkers)
		case <-time.After(time.Millisecond):
		}
	}
	time.Sleep(50 * time.Millisecond) // give a 17th worker a chance to appear
	if got := sender.maxSeen.Load(); got != int64(MailWorkers) {
		t.Fatalf("max concurrent sends = %d, want exactly %d", got, MailWorkers)
	}
}

// A full queue must DROP, immediately. A blocking enqueue would make the
// handler's latency depend on the mail backlog — and, for
// forgot-password, on whether the account existed.
func TestMailDispatcher_FullQueueDropsWithoutBlocking(t *testing.T) {
	d := NewMailDispatcher(slog.Default())
	d.Start()
	sender := newBlockingSender()
	t.Cleanup(func() { close(sender.release); d.Stop(context.Background()) })

	// Fill every worker and every queue slot.
	accepted := 0
	for i := 0; i < MailWorkers+MailQueueCapacity; i++ {
		if d.Enqueue(sender.job()) {
			accepted++
		}
	}
	if accepted < MailQueueCapacity {
		t.Fatalf("accepted only %d, want at least the queue capacity %d", accepted, MailQueueCapacity)
	}

	start := time.Now()
	ok := d.Enqueue(sender.job())
	elapsed := time.Since(start)
	if ok {
		t.Fatal("an enqueue past capacity must be dropped")
	}
	if elapsed > 50*time.Millisecond {
		t.Fatalf("a dropped enqueue took %v — it must return at once", elapsed)
	}
	if got := d.Dropped(); got == 0 {
		t.Fatal("the drop must be counted")
	}
}

// No goroutine per request. This is the property a bare `go send(...)`
// would violate, and the one a flood would exploit.
func TestMailDispatcher_NoGoroutinePerEnqueue(t *testing.T) {
	d := NewMailDispatcher(slog.Default())
	d.Start()
	sender := newBlockingSender()
	t.Cleanup(func() { close(sender.release); d.Stop(context.Background()) })

	runtime.GC()
	before := runtime.NumGoroutine()
	for i := 0; i < 10000; i++ {
		d.Enqueue(sender.job())
	}
	runtime.GC()
	after := runtime.NumGoroutine()
	if after-before > 2 { // slack for the runtime's own bookkeeping
		t.Fatalf("goroutines grew by %d over 10000 enqueues; the dispatcher must create none", after-before)
	}
}

// Enqueue latency must not depend on how full the queue is: that
// difference is exactly the timing oracle the detach exists to remove.
func TestMailDispatcher_EnqueueLatencyIndependentOfBacklog(t *testing.T) {
	d := NewMailDispatcher(slog.Default())
	d.Start()
	sender := newBlockingSender()
	t.Cleanup(func() { close(sender.release); d.Stop(context.Background()) })

	empty := time.Now()
	d.Enqueue(sender.job())
	emptyCost := time.Since(empty)

	for i := 0; i < MailQueueCapacity-2; i++ {
		d.Enqueue(sender.job())
	}

	full := time.Now()
	d.Enqueue(sender.job())
	fullCost := time.Since(full)

	if fullCost > 10*time.Millisecond || emptyCost > 10*time.Millisecond {
		t.Fatalf("enqueue costs: empty %v, near-full %v — both must be sub-millisecond in practice", emptyCost, fullCost)
	}
}

func TestMailDispatcher_StopDrainsQueuedJobs(t *testing.T) {
	d := NewMailDispatcher(slog.Default())
	d.Start()

	var sent atomic.Int64
	for i := 0; i < 32; i++ {
		d.Enqueue(MailJob{TemplateID: "auth.reset_password", Send: func(context.Context) error {
			sent.Add(1)
			return nil
		}})
	}
	d.Stop(context.Background())
	if got := sent.Load(); got != 32 {
		t.Fatalf("sent %d of 32 queued jobs; Stop must drain", got)
	}
}

func TestMailDispatcher_StopAbandonsAfterTimeout(t *testing.T) {
	d := NewMailDispatcher(slog.Default())
	d.Start()
	sender := newBlockingSender()

	for i := 0; i < MailWorkers*2; i++ {
		d.Enqueue(sender.job())
	}
	// Wait for the workers to be busy, then Stop with a context that is
	// already done so the drain gives up at once.
	<-sender.started

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	start := time.Now()
	d.Stop(ctx)
	if elapsed := time.Since(start); elapsed > 2*time.Second {
		t.Fatalf("Stop took %v with a cancelled context; it must abandon", elapsed)
	}
	close(sender.release)
}

// A send that panics must not take a worker — or the process — with it.
func TestMailDispatcher_RecoversFromPanickingSend(t *testing.T) {
	d := NewMailDispatcher(slog.Default())
	d.Start()

	var after atomic.Int64
	var wg sync.WaitGroup
	wg.Add(1)
	d.Enqueue(MailJob{TemplateID: "auth.reset_password", Send: func(context.Context) error {
		panic("sender exploded")
	}})
	d.Enqueue(MailJob{TemplateID: "auth.reset_password", Send: func(context.Context) error {
		after.Add(1)
		wg.Done()
		return nil
	}})
	wg.Wait()
	d.Stop(context.Background())
	if after.Load() != 1 {
		t.Fatal("a panicking job must not stop the pool serving the next one")
	}
}

// A job whose Send returns an error is a lost mail, logged, not a crash.
func TestMailDispatcher_SendErrorIsSwallowed(t *testing.T) {
	d := NewMailDispatcher(slog.Default())
	d.Start()
	var wg sync.WaitGroup
	wg.Add(1)
	d.Enqueue(MailJob{TemplateID: "auth.reset_password", Send: func(context.Context) error {
		defer wg.Done()
		return errors.New("smtp down")
	}})
	wg.Wait()
	d.Stop(context.Background())
}

// Enqueue on a dispatcher that was never started, or already stopped,
// must not panic on a closed channel — a module toggled off mid-request
// is an ordinary state.
func TestMailDispatcher_EnqueueAfterStopIsSafe(t *testing.T) {
	d := NewMailDispatcher(slog.Default())
	d.Start()
	d.Stop(context.Background())
	if d.Enqueue(MailJob{TemplateID: "auth.reset_password", Send: func(context.Context) error { return nil }}) {
		t.Fatal("Enqueue after Stop must report the job as not accepted")
	}

	var never *MailDispatcher
	if never.Enqueue(MailJob{}) {
		t.Fatal("Enqueue on a nil dispatcher must be a safe no-op")
	}
}
```

- [ ] **Step 2: Run it to verify it fails**

Run: `go test ./internal/core/auth/services/ -run MailDispatcher -count=1`
Expected: FAIL to compile — `undefined: NewMailDispatcher`, `undefined: MailJob`, `undefined: MailWorkers`.

- [ ] **Step 3: Write the dispatcher**

Create `backend/internal/core/auth/services/mail_dispatcher.go`:

```go
package services

import (
	"context"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"github.com/orkestra/backend/pkg/sdk/metrics"
)

const (
	// MailQueueCapacity bounds MEMORY: at most this many jobs wait.
	MailQueueCapacity = 256
	// MailWorkers bounds CONCURRENCY against the SMTP relay.
	MailWorkers = 16
	// mailDrainTimeout is how long Stop waits for queued work before
	// abandoning it. A restart may lose a queued or in-flight reset
	// mail; the user retries inside the caps of D2.
	mailDrainTimeout = 10 * time.Second
	// mailJobTimeout bounds one detached send.
	mailJobTimeout = 60 * time.Second
)

// MailJob is one detached transactional send.
//
// Send closes over everything the delivery needs (recipient, template
// data, notifier) so the dispatcher stays free of notification types and
// of anything the caller would otherwise have to keep alive.
//
// TemplateID is the metric label for a drop and the only identifier that
// reaches a log line. RequestID correlates the drop with the request that
// caused it. The recipient's ADDRESS is never logged — a WARN naming who
// did not get their password-reset mail is an enumeration oracle in the
// log pipeline.
type MailJob struct {
	TemplateID string
	RequestID  string
	Send       func(ctx context.Context) error
}

// MailDispatcher detaches transactional auth mail from the request that
// triggered it, with hard bounds in three dimensions:
//
//   - memory: MailQueueCapacity queued jobs, no more;
//   - concurrency: MailWorkers goroutines, started once at module Start;
//   - request latency: enqueue is non-blocking, so the handler's
//     response time depends on neither the SMTP relay nor the backlog.
//
// That last bound is a security property, not just a performance one:
// ForgotPassword must cost the same whether or not the address exists,
// and a blocking acquire would have reintroduced the difference for
// known addresses exactly when the queue is contended.
//
// A full queue DROPS the job with a metric. A drop is a lost mail the
// user recovers by asking again inside the D2 caps;
// orkestra_auth_mail_dropped_total is the alerting signal that the
// queue or the worker count is undersized.
type MailDispatcher struct {
	log  *slog.Logger
	jobs chan MailJob
	wg   sync.WaitGroup

	mu      sync.Mutex
	started bool
	stopped bool

	dropped atomic.Int64
}

func NewMailDispatcher(log *slog.Logger) *MailDispatcher {
	if log == nil {
		log = slog.Default()
	}
	return &MailDispatcher{
		log:  log,
		jobs: make(chan MailJob, MailQueueCapacity),
	}
}

// Start launches the worker pool. Idempotent.
func (d *MailDispatcher) Start() {
	if d == nil {
		return
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.started || d.stopped {
		return
	}
	d.started = true
	for i := 0; i < MailWorkers; i++ {
		d.wg.Add(1)
		go d.worker()
	}
}

// Stop closes the queue and waits up to mailDrainTimeout (or until ctx
// is done, whichever comes first) for the workers to finish what is
// queued. Idempotent.
func (d *MailDispatcher) Stop(ctx context.Context) {
	if d == nil {
		return
	}
	d.mu.Lock()
	if d.stopped || !d.started {
		d.stopped = true
		d.mu.Unlock()
		return
	}
	d.stopped = true
	close(d.jobs)
	d.mu.Unlock()

	done := make(chan struct{})
	go func() {
		d.wg.Wait()
		close(done)
	}()

	timer := time.NewTimer(mailDrainTimeout)
	defer timer.Stop()
	select {
	case <-done:
	case <-timer.C:
		d.log.Warn("auth: mail dispatcher abandoned queued sends at shutdown",
			slog.Duration("drain_timeout", mailDrainTimeout))
	case <-ctx.Done():
		d.log.Warn("auth: mail dispatcher shutdown context expired; queued sends abandoned")
	}
}

// Enqueue hands a job to the pool and returns whether it was accepted.
// It NEVER blocks and NEVER creates a goroutine.
func (d *MailDispatcher) Enqueue(job MailJob) bool {
	if d == nil || job.Send == nil {
		return false
	}
	d.mu.Lock()
	stopped := d.stopped || !d.started
	d.mu.Unlock()
	if stopped {
		return false
	}

	select {
	case d.jobs <- job:
		return true
	default:
		d.dropped.Add(1)
		metrics.Default().RecordAuthMailDropped(job.TemplateID)
		// Template id and request id only — never the address.
		d.log.Warn("auth: mail dropped, dispatcher queue full",
			slog.String("template", job.TemplateID),
			slog.String("request_id", job.RequestID),
			slog.Int("queue_capacity", MailQueueCapacity),
		)
		return false
	}
}

// Dropped reports the process-local drop count (tests and diagnostics;
// the metric is the operational signal).
func (d *MailDispatcher) Dropped() int64 {
	if d == nil {
		return 0
	}
	return d.dropped.Load()
}

func (d *MailDispatcher) worker() {
	defer d.wg.Done()
	for job := range d.jobs {
		d.run(job)
	}
}

func (d *MailDispatcher) run(job MailJob) {
	// WithoutCancel: the request that queued this job is long gone by
	// the time a worker picks it up, and a send tied to that context
	// would be cancelled before it ever left the process.
	ctx, cancel := context.WithTimeout(context.WithoutCancel(context.Background()), mailJobTimeout)
	defer cancel()

	defer func() {
		if r := recover(); r != nil {
			d.log.Error("auth: mail send panicked",
				slog.String("template", job.TemplateID),
				slog.Any("panic", r))
		}
	}()

	if err := job.Send(ctx); err != nil {
		// Delivery failures were already swallowed on this path; they
		// keep going to the log and to the notification log row.
		d.log.Warn("auth: mail send failed",
			slog.String("template", job.TemplateID),
			slog.String("error", err.Error()))
	}
}
```

- [ ] **Step 4: Run the dispatcher tests**

Run: `go test ./internal/core/auth/services/ -run MailDispatcher -count=1 -race`
Expected: PASS, no race.

- [ ] **Step 5: Wire it into the module lifecycle**

In `backend/internal/core/auth/module.go`, near the counter construction:

```go
	mailDispatcher := services.NewMailDispatcher(logger)
```

store it on the module (`m.mailDispatcher = mailDispatcher`), pass `mailDispatcher: mailDispatcher,` into both `buildAuthTierBundle` calls, and add the field to `tierBundleDeps` plus `MailDispatcher: d.mailDispatcher,` to the `PasswordAuthConfig` literal (and the matching `PasswordAuthConfig` field + service field + constructor assignment, mirroring Task 3 Step 6).

In `backend/internal/core/auth/maintenance.go`, `Start` — **before** the `len(m.sweepTiers) == 0` early return, because the dispatcher must run even on a deployment with nothing to sweep:

```go
	// The mail dispatcher is not maintenance: it serves live requests,
	// so it starts regardless of whether this replica has sweep tiers
	// or won the lease.
	m.mailDispatcher.Start()
```

and in `Stop`, before reading `m.sweepCancel`:

```go
	m.mailDispatcher.Stop(ctx)
```

- [ ] **Step 6: Vet, run, commit**

```bash
go vet ./... && go test ./internal/core/auth/... -count=1
git add backend/internal/core/auth
git commit -m "$(cat <<'EOF'
feat(auth): add a bounded dispatcher for transactional auth mail

A fixed 256-job queue drained by 16 workers, started and stopped with
the module. Enqueue is non-blocking: a full queue drops the job, counts
orkestra_auth_mail_dropped_total{template} and logs the template and
request id — never the recipient address.

The non-blocking property is a security one. ForgotPassword must cost
the same whether or not the address exists; a blocking acquire would
have reintroduced exactly that difference for known addresses whenever
the queue is contended, and a bare goroutine per request would let a
flood grow the process without bound.

Nothing enqueues yet — the callers move in the next commit.

Spec §4.1 D5.

Claude-Session: https://claude.ai/code/session_0155Lt3QwR9zX15oGNKrn1T1
EOF
)"
```

---

## Task 7: Login moves to the counters (D3, D4)

The order matters more than the mechanism. Peek before anything; record only on a real failure; pay the argon2 cost on **every** non-success branch; clear an expired durable lock *before* verifying. That combination is what closes the 429-vs-401 oracle (M-7), the timing oracle (L-1) and the "one wrong password after expiry re-locks you" defect at once.

**Files:**
- Modify: `backend/internal/core/auth/services/password_auth_service.go` (`Login`, `:475-666`)
- Modify: `backend/internal/core/auth/CLAUDE.md`
- Test: `backend/internal/core/auth/services/password_auth_login_lockout_test.go` (new)

**Interfaces:**
- Consumes: `AttemptCounter`, `AttemptKeyIP`, `AttemptKeyEmail`, `Limit`, `LockedAfter` (Tasks 1, 3); `IPLockoutThreshold`/`IPLockoutDuration` (Task 4).
- Produces:
  - `func (s *PasswordAuthService) accountLimit(ctx context.Context) Limit`
  - `func (s *PasswordAuthService) addressLimit(ctx context.Context) Limit`
  - `func (s *PasswordAuthService) peekLockout(ctx context.Context, ip, email string) (Verdict, bool)` — second return is "locked"
  - `func (s *PasswordAuthService) recordLoginFailure(ctx context.Context, ip, email string) (emailVerdict Verdict, counterAvailable bool)`
- Later tasks rely on: `recordLoginFailure` (Task 9 reuses it for change-password / password-confirm).

- [ ] **Step 1: Write the failing lockout tests**

Create `backend/internal/core/auth/services/password_auth_login_lockout_test.go`:

```go
package services

// The login lockout has to be indistinguishable between a known and an
// unknown address, in status code AND in cost. These tests measure both.
// They reuse the existing gateUserFake / fakePasswordService harness —
// read gates_fakes_test.go before editing.

import (
	"context"
	"errors"
	"testing"
	"time"
)

const lockoutTestThreshold = 3

// A known and an unknown email must produce the SAME status sequence
// over threshold+1 attempts. Answering 429 for one and 401 for the other
// is a free account-existence oracle.
func TestLogin_KnownAndUnknownEmailLockIdentically(t *testing.T) {
	seq := func(email string) []error {
		svc := newLockoutTestService(t, lockoutTestThreshold)
		var out []error
		for i := 0; i < lockoutTestThreshold+1; i++ {
			_, err := svc.Login(context.Background(), LoginInput{
				Email: email, Password: "wrong-password", IP: "203.0.113.10",
			})
			out = append(out, err)
		}
		return out
	}

	known := seq("known@example.com")   // seeded by the fixture
	unknown := seq("nobody@example.com")

	if len(known) != len(unknown) {
		t.Fatalf("sequence lengths differ: %d vs %d", len(known), len(unknown))
	}
	for i := range known {
		if !sameSentinel(known[i], unknown[i]) {
			t.Fatalf("attempt %d: known=%v unknown=%v — the answers must be identical",
				i+1, known[i], unknown[i])
		}
	}
	// And the last one must be the lockout, not a 401.
	if !errors.Is(known[len(known)-1], ErrAccountLocked) {
		t.Fatalf("attempt %d for a known email = %v, want ErrAccountLocked",
			len(known), known[len(known)-1])
	}
}

// Every non-success branch must pay the argon2 cost. Today the inactive
// account and the service-principal branches return without one, which
// makes them measurably faster than a wrong password.
func TestLogin_EveryFailureBranchRunsAVerify(t *testing.T) {
	cases := []struct {
		name  string
		email string
	}{
		{"unknown user", "nobody@example.com"},
		{"inactive user", "inactive@example.com"},
		{"service principal", "svc@example.com"},
		{"no password hash", "oauthonly@example.com"},
		{"wrong password", "known@example.com"},
		{"durably locked", "locked@example.com"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			svc, pw := newLockoutTestServiceWithPassword(t, lockoutTestThreshold)
			before := pw.verifyCalls()
			_, err := svc.Login(context.Background(), LoginInput{
				Email: tc.email, Password: "whatever", IP: "203.0.113.11",
			})
			if err == nil {
				t.Fatal("want a failure")
			}
			if pw.verifyCalls() == before {
				t.Fatalf("%s returned without a password verify — a measurably cheaper branch is a timing oracle", tc.name)
			}
		})
	}
}

// A locked scope answers before the user lookup and records NOTHING —
// otherwise the lock extends itself forever under a running attack.
func TestLogin_LockedScopeRecordsNothing(t *testing.T) {
	svc, counter := newLockoutTestServiceWithCounter(t, 1)
	ctx := context.Background()

	_, _ = svc.Login(ctx, LoginInput{Email: "known@example.com", Password: "wrong", IP: "203.0.113.12"})
	v, _ := counter.Locked(ctx, AttemptKeyEmail(PolicyAudienceOperator, "known@example.com"), Limit{Threshold: 1, Window: time.Minute})
	countAfterFirst := v.Count

	for i := 0; i < 5; i++ {
		_, err := svc.Login(ctx, LoginInput{Email: "known@example.com", Password: "wrong", IP: "203.0.113.12"})
		if !errors.Is(err, ErrAccountLocked) {
			t.Fatalf("attempt %d = %v, want ErrAccountLocked", i, err)
		}
	}
	v, _ = counter.Locked(ctx, AttemptKeyEmail(PolicyAudienceOperator, "known@example.com"), Limit{Threshold: 1, Window: time.Minute})
	if v.Count != countAfterFirst {
		t.Fatalf("counter moved from %d to %d while locked — a lock must not extend itself", countAfterFirst, v.Count)
	}
}

// The durable lock is the SECOND line. With the counter unavailable it
// must still cap guessing against an existing account, and it must NOT
// invent a lock for an unknown one (there is no document to write).
func TestLogin_CounterUnavailableFallsBackToDurableRule(t *testing.T) {
	svc, users := newLockoutTestServiceWithFailingCounter(t, lockoutTestThreshold)
	ctx := context.Background()

	for i := 0; i < lockoutTestThreshold; i++ {
		_, err := svc.Login(ctx, LoginInput{Email: "known@example.com", Password: "wrong", IP: "203.0.113.13"})
		if !errors.Is(err, ErrInvalidCredentials) {
			t.Fatalf("attempt %d = %v, want ErrInvalidCredentials while the counter is down", i+1, err)
		}
	}
	if !users.lockedUntilSet("known@example.com") {
		t.Fatal("the durable rule must lock the account when the counter cannot")
	}

	// Documented fail-open: an unknown email is answered 401 throughout.
	for i := 0; i < lockoutTestThreshold+2; i++ {
		_, err := svc.Login(ctx, LoginInput{Email: "nobody@example.com", Password: "wrong", IP: "203.0.113.13"})
		if !errors.Is(err, ErrInvalidCredentials) {
			t.Fatalf("unknown-email attempt %d = %v, want ErrInvalidCredentials", i+1, err)
		}
	}
}

// An EXPIRED durable lock must be cleared BEFORE the verify. Today the
// counter it is compared against is never reset, so the first wrong
// password after a lock expires re-locks the account immediately.
func TestLogin_ExpiredDurableLockIsClearedBeforeVerify(t *testing.T) {
	svc, users := newLockoutTestServiceWithUsers(t, lockoutTestThreshold)
	ctx := context.Background()
	users.setExpiredLock("known@example.com", lockoutTestThreshold+9)

	_, err := svc.Login(ctx, LoginInput{Email: "known@example.com", Password: "wrong", IP: "203.0.113.14"})
	if !errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("first attempt after expiry = %v, want ErrInvalidCredentials, not an instant re-lock", err)
	}
	if !users.failedLoginsCleared("known@example.com") {
		t.Fatal("ClearFailedLogins must run before the verify when LockedUntil is in the past")
	}
}

// A successful login clears the EMAIL scope only. Resetting the IP scope
// would let one correct login launder a stuffing run from that address.
func TestLogin_SuccessResetsEmailScopeNotIPScope(t *testing.T) {
	svc, counter := newLockoutTestServiceWithCounter(t, 10)
	ctx := context.Background()
	ip := "203.0.113.15"

	for i := 0; i < 3; i++ {
		_, _ = svc.Login(ctx, LoginInput{Email: "known@example.com", Password: "wrong", IP: ip})
	}
	if _, err := svc.Login(ctx, LoginInput{Email: "known@example.com", Password: correctTestPassword, IP: ip}); err != nil {
		t.Fatalf("successful login: %v", err)
	}

	emailV, _ := counter.Locked(ctx, AttemptKeyEmail(PolicyAudienceOperator, "known@example.com"), Limit{Threshold: 10, Window: time.Minute})
	if emailV.Count != 0 {
		t.Fatalf("email scope = %d after a successful login, want 0", emailV.Count)
	}
	ipV, _ := counter.Locked(ctx, AttemptKeyIP(ip), Limit{Threshold: 10, Window: time.Minute})
	if ipV.Count == 0 {
		t.Fatal("the IP scope must NOT be reset by a success — one correct login cannot launder a stuffing run")
	}
}

// Edge case 31: six wrong passwords for six different accounts from one
// office egress must lock none of them and not the address, because the
// address threshold is 100, not 5.
func TestLogin_SharedEgressDoesNotLockTheAddress(t *testing.T) {
	svc, counter := newLockoutTestServiceWithCounter(t, lockoutTestThreshold)
	ctx := context.Background()
	ip := "203.0.113.16"

	for i := 0; i < 6; i++ {
		email := "user" + string(rune('a'+i)) + "@example.com"
		_, err := svc.Login(ctx, LoginInput{Email: email, Password: "wrong", IP: ip})
		if errors.Is(err, ErrAccountLocked) {
			t.Fatalf("attempt %d locked; six failures across six accounts must not lock the address", i+1)
		}
	}
	v, _ := counter.Locked(ctx, AttemptKeyIP(ip), Limit{Threshold: 100, Window: 15 * time.Minute})
	if v.Locked {
		t.Fatal("the address must not be locked at 6 failures with a threshold of 100")
	}
}

// An unresolvable client IP must SKIP the address scope rather than
// share one key with every other such caller.
func TestLogin_EmptyIPSkipsTheAddressScope(t *testing.T) {
	svc, counter := newLockoutTestServiceWithCounter(t, 100)
	ctx := context.Background()

	for i := 0; i < 5; i++ {
		_, _ = svc.Login(ctx, LoginInput{Email: "known@example.com", Password: "wrong", IP: ""})
	}
	v, err := counter.Locked(ctx, AttemptKeyIP(""), Limit{Threshold: 100, Window: time.Minute})
	if err != nil {
		t.Fatalf("Locked: %v", err)
	}
	if v.Count != 0 {
		t.Fatalf("an empty IP produced a counter with count %d; it must produce none", v.Count)
	}
}
```

> **Fixtures:** `newLockoutTestService*`, `sameSentinel`, `correctTestPassword`,
> and the `users` fake's `lockedUntilSet` / `setExpiredLock` /
> `failedLoginsCleared` / `verifyCalls` helpers must be added to the
> existing `gates_fakes_test.go` alongside the current `gateUserFake` and
> `fakePasswordService` — extend those types, do not fork them. Seed the
> user fake with: `known@example.com` (active, hashed `correctTestPassword`),
> `inactive@example.com` (`IsActive: false`), `svc@example.com`
> (`Kind: iface.UserKindService`), `oauthonly@example.com`
> (`PasswordHash: ""`), `locked@example.com` (`LockedUntil` in the future).
> `sameSentinel(a, b)` returns true when both are nil, or when
> `errors.Is(a, b) || errors.Is(b, a)`.

- [ ] **Step 2: Run it to verify it fails**

Run: `go test ./internal/core/auth/services/ -run 'TestLogin_' -count=1`
Expected: FAIL — helpers undefined; and once the fixtures exist, the
identical-sequence, verify-on-every-branch and expired-lock tests fail
against today's `Login`.

- [ ] **Step 3: Add the limit and counter helpers**

In `backend/internal/core/auth/services/password_auth_service.go`, replacing `recordFailed` (`:1354-1360`):

```go
// accountLimit is the per-email / per-client pair. Read per call, so an
// admin edit takes effect on the very next attempt — including one
// inside an already-open window, since the threshold is compared live
// (attempt_counter.go), not frozen into a bucket.
func (s *PasswordAuthService) accountLimit(ctx context.Context) Limit {
	return Limit{
		Threshold: s.policy.LockoutThreshold(ctx),
		Window:    s.policy.LockoutDuration(ctx),
	}
}

// addressLimit is the per-IP pair, an order of magnitude looser: an
// egress address is not an account (spec D2, edge case 31).
func (s *PasswordAuthService) addressLimit(ctx context.Context) Limit {
	return Limit{
		Threshold: s.policy.IPLockoutThreshold(ctx),
		Window:    s.policy.IPLockoutDuration(ctx),
	}
}

// peekLockout reads both scopes WITHOUT moving either. A store error
// reads as "not locked": the counters fail open and the durable lock is
// the second line (spec D1). Returns the verdict that produced the lock
// so the caller can render Retry-After.
func (s *PasswordAuthService) peekLockout(ctx context.Context, ip, email string) (Verdict, bool) {
	if s.attempts == nil {
		return Verdict{}, false
	}
	if v, err := s.attempts.Locked(ctx, AttemptKeyIP(ip), s.addressLimit(ctx)); err == nil && v.Locked {
		return v, true
	}
	if v, err := s.attempts.Locked(ctx, AttemptKeyEmail(s.audience, email), s.accountLimit(ctx)); err == nil && v.Locked {
		return v, true
	}
	return Verdict{}, false
}

// recordLoginFailure charges one failure against the address and the
// account. counterAvailable is false when the EMAIL scope could not be
// recorded — that is the signal for the durable branch (D4) to fall
// back to the FailedLoginCount rule for this attempt.
func (s *PasswordAuthService) recordLoginFailure(ctx context.Context, ip, email string) (Verdict, bool) {
	if s.attempts == nil {
		return Verdict{}, false
	}
	_, _ = s.attempts.RecordFailure(ctx, AttemptKeyIP(ip), s.addressLimit(ctx))
	v, err := s.attempts.RecordFailure(ctx, AttemptKeyEmail(s.audience, email), s.accountLimit(ctx))
	return v, err == nil
}

// resetLoginFailures clears the EMAIL scope after a success. The address
// scope is deliberately NOT reset: one correct login must not launder a
// credential-stuffing run coming from the same address.
func (s *PasswordAuthService) resetLoginFailures(ctx context.Context, email string) {
	if s.attempts == nil {
		return
	}
	_ = s.attempts.Reset(ctx, AttemptKeyEmail(s.audience, email))
}

// dummyVerify burns one argon2 verification so a branch that returns
// early costs the same wall-clock time as a wrong password. Every
// non-success branch of Login calls it.
func (s *PasswordAuthService) dummyVerify(password string) {
	_, _ = s.passwordService.Verify(password, s.passwordService.DummyHash())
}
```

- [ ] **Step 4: Rewrite `Login`'s lockout order**

In `Login`, delete the `SetAuthFailedConfig` block (`:519-529`) and the `IsBlocked` block (`:531-536`) and put in their place:

```go
	// Peek both scopes before touching the database. Nothing is
	// recorded: a lock that extends itself on every probe never expires
	// under a running attack. A store error reads as not locked — the
	// counters fail open and the durable lock below is the second line.
	if v, locked := s.peekLockout(ctx, in.IP, email); locked {
		s.dummyVerify(in.Password)
		s.emitLoginFailed(ctx, email, "", in.IP, "rate_limited")
		return nil, LockedAfter(v.RetryAfter)
	}
```

Then, branch by branch:

```go
	user, err := s.userService.GetUserForAuth(ctx, email)
	if err != nil {
		s.dummyVerify(in.Password)
		s.recordLoginFailure(ctx, in.IP, email)
		s.emitLoginFailed(ctx, email, "", in.IP, "unknown_user")
		return nil, ErrInvalidCredentials
	}
```

```go
	if !user.IsActive {
		s.dummyVerify(in.Password) // NEW: this branch used to be measurably cheaper
		s.recordLoginFailure(ctx, in.IP, email)
		s.emitLoginFailed(ctx, email, user.UUID, in.IP, "user_inactive")
		return nil, ErrInvalidCredentials
	}
	if user.Kind == iface.UserKindService {
		s.dummyVerify(in.Password) // NEW
		s.recordLoginFailure(ctx, in.IP, email)
		s.emitLoginFailed(ctx, email, user.UUID, in.IP, "service_principal")
		return nil, ErrInvalidCredentials
	}
```

```go
	if user.LockedUntil != nil && time.Now().Before(*user.LockedUntil) {
		s.dummyVerify(in.Password) // NEW
		s.emitLoginFailed(ctx, email, user.UUID, in.IP, "account_locked")
		// Not recorded: a durable lock, like a counter lock, must not
		// extend itself.
		return nil, LockedAfter(time.Until(*user.LockedUntil))
	}
	if user.LockedUntil != nil {
		// The lock has EXPIRED. Clear the durable counter before the
		// verify: the old code compared FailedLoginCount against the
		// threshold without anything ever resetting it, so the first
		// wrong password after an expiry re-locked the account at once.
		if err := s.userService.ClearFailedLogins(ctx, user.UUID); err != nil && s.logger != nil {
			s.logger.Warn("auth: failed to clear an expired account lock",
				slog.String("user_uuid", user.UUID),
				slog.String("error", err.Error()))
		}
		user.FailedLoginCount = 0
		user.LockedUntil = nil
	}
```

```go
	if user.PasswordHash == "" {
		s.dummyVerify(in.Password)
		s.recordLoginFailure(ctx, in.IP, email)
		s.emitLoginFailed(ctx, email, user.UUID, in.IP, "no_password")
		return nil, ErrInvalidCredentials
	}
```

And the verify-failure branch (D4):

```go
	ok, err := s.passwordService.Verify(in.Password, user.PasswordHash)
	if err != nil || !ok {
		emailVerdict, counterAvailable := s.recordLoginFailure(ctx, in.IP, email)

		// The durable lock MIRRORS the counter. With a healthy Redis the
		// two lock at the same attempt; with Redis down the durable rule
		// alone still caps guessing against an existing account.
		var lockUntil *time.Time
		lock := emailVerdict.Locked
		if !counterAvailable {
			lock = user.FailedLoginCount+1 >= s.policy.LockoutThreshold(ctx)
		}
		if lock {
			t := time.Now().Add(s.policy.LockoutDuration(ctx))
			lockUntil = &t
		}
		// FailedLoginCount keeps being incremented for operator
		// visibility even when the counter is the one deciding.
		_ = s.userService.RecordFailedLogin(ctx, user.UUID, lockUntil)
		s.emitLoginFailed(ctx, email, user.UUID, in.IP, "bad_password")
		return nil, ErrInvalidCredentials
	}
```

Finally, at the success site (`:626`), next to the existing `ClearFailedLogins`:

```go
	_ = s.userService.ClearFailedLogins(ctx, user.UUID)
	s.resetLoginFailures(ctx, email)
```

- [ ] **Step 5: Run the login tests**

Run: `go test ./internal/core/auth/services/ -run 'TestLogin' -count=1`
Expected: PASS — the new file plus every pre-existing `TestLogin*`.

- [ ] **Step 6: Run the whole auth suite**

Run: `go test ./internal/core/auth/... -count=1`
Expected: PASS. If a pre-existing test asserted a *401* where a lockout now answers 429, read it: the change is intended (M-7), so update the expectation and say so in the commit body.

- [ ] **Step 7: Correct the CLAUDE.md sentences this task falsifies**

In `backend/internal/core/auth/CLAUDE.md`, fix the limiter claims at `:28`, `:116`, `:185`, `:341`, `:1071`, `:1091` (each says or implies the shared in-memory bucket is the lockout, or "the only protection"), and add a new **Attempt counters** section stating: the scopes and their keys, that both reads run one script, that they fail open to the durable lock, that the address pair is separate and looser, and that a success resets the email scope only.

- [ ] **Step 8: Vet and commit**

```bash
go vet ./... && go test ./internal/core/auth/... -count=1
git add backend/internal/core/auth
git commit -m "$(cat <<'EOF'
fix(auth): move login lockout onto the Redis counters, closing the oracles

Login now peeks both scopes before the user lookup, records only on a
real failure, and pays the argon2 cost on EVERY non-success branch —
the inactive-account, service-principal and locked-account branches used
to return without one, which made them measurably cheaper than a wrong
password.

A known and an unknown email now lock at the same attempt, inside the
same window, and answer the same 429 with Retry-After, so the
429-versus-401 difference is no longer an account-existence oracle. An
expired durable lock is cleared BEFORE the verify, so the first wrong
password after a lockout expires no longer re-locks the account against
a counter nothing ever reset.

The durable lock mirrors the counter and takes over on its own when
Redis is unavailable; a successful login clears the email scope but not
the address scope, so one correct login cannot launder a stuffing run.

Spec §4.1 D3, D4. Closes H-1's remaining call site and M-7.

Claude-Session: https://claude.ai/code/session_0155Lt3QwR9zX15oGNKrn1T1
EOF
)"
```

---

## Task 8: Forgot-password and resend-verification (D5, caller half)

Two defects here. `ResendVerification` calls `IsBlocked`, which *consumes* a token, so an anonymous caller can keep any address permanently rate-limited on the **login** scopes (M-6). And `ForgotPassword` invalidates the previous token on every call with no throttle, so an attacker can destroy the victim's live reset link at will (M-5).

**Files:**
- Modify: `backend/internal/core/auth/services/password_auth_service.go` (`ResendVerification` `:982-1006`, `ForgotPassword` `:1016-1091`)
- Test: `backend/internal/core/auth/services/password_auth_request_caps_test.go` (new)

**Interfaces:**
- Consumes: `AttemptCounter`, the reset/verify key builders and `Reset*/Verify*` limits (Task 3), `MailDispatcher` (Task 6).
- Produces: `func (s *PasswordAuthService) overRequestCap(ctx context.Context, ipKey, emailKey string, ipLimit, emailLimit Limit) bool`

- [ ] **Step 1: Write the failing tests**

Create `backend/internal/core/auth/services/password_auth_request_caps_test.go`:

```go
package services

import (
	"context"
	"testing"
	"time"
)

// Inside the cap an attacker can force at most three re-issues per
// address per window; the fourth request must leave the victim's live
// token alone. Today every call invalidates it, so a script can keep a
// reset link permanently dead.
func TestForgotPassword_OverEmailCapIssuesNothing(t *testing.T) {
	svc, tokens, mail := newRequestCapTestService(t)
	ctx := context.Background()

	for i := 0; i < 3; i++ {
		if err := svc.ForgotPassword(ctx, "known@example.com", "203.0.113.20"); err != nil {
			t.Fatalf("request %d: %v", i+1, err)
		}
	}
	issuedAfterCap := tokens.createCount()
	invalidatedAfterCap := tokens.invalidateCount()
	mailedAfterCap := mail.enqueued()

	// The fourth is over the cap: generic success, and NOTHING happens.
	if err := svc.ForgotPassword(ctx, "known@example.com", "203.0.113.20"); err != nil {
		t.Fatalf("over-cap request must still answer generically: %v", err)
	}
	if tokens.createCount() != issuedAfterCap {
		t.Error("an over-cap request must not mint a token")
	}
	if tokens.invalidateCount() != invalidatedAfterCap {
		t.Error("an over-cap request must not invalidate the victim's live token")
	}
	if mail.enqueued() != mailedAfterCap {
		t.Error("an over-cap request must not send mail")
	}
}

// The cost must be identical for a known and an unknown address, so the
// counter is charged BEFORE the lookup.
func TestForgotPassword_RecordsBeforeTheLookup(t *testing.T) {
	svc, _, _ := newRequestCapTestService(t)
	counter := svc.attempts
	ctx := context.Background()

	_ = svc.ForgotPassword(ctx, "nobody@example.com", "203.0.113.21")
	v, err := counter.Locked(ctx, AttemptKeyResetEmail(PolicyAudienceOperator, "nobody@example.com"), ResetRequestsPerEmail)
	if err != nil {
		t.Fatalf("Locked: %v", err)
	}
	if v.Count != 1 {
		t.Fatalf("count for an UNKNOWN address = %d, want 1 — the cap must be charged before the lookup", v.Count)
	}
}

// The handler must not wait on the relay. With a sender that blocks, the
// call still returns promptly.
func TestForgotPassword_DoesNotWaitOnDelivery(t *testing.T) {
	svc, _, mail := newRequestCapTestServiceBlockingSender(t)
	ctx := context.Background()

	start := time.Now()
	if err := svc.ForgotPassword(ctx, "known@example.com", "203.0.113.22"); err != nil {
		t.Fatalf("ForgotPassword: %v", err)
	}
	if elapsed := time.Since(start); elapsed > 200*time.Millisecond {
		t.Fatalf("ForgotPassword took %v with a blocking sender; the send must be detached", elapsed)
	}
	if mail.enqueued() != 1 {
		t.Fatalf("enqueued %d jobs, want 1", mail.enqueued())
	}
}

// M-6: a verification resend is not a login failure and must never be
// able to lock a login.
func TestResendVerification_NeverTouchesTheLoginScopes(t *testing.T) {
	svc, _, _ := newRequestCapTestService(t)
	counter := svc.attempts
	ctx := context.Background()

	for i := 0; i < 10; i++ {
		_ = svc.ResendVerification(ctx, "known@example.com", "203.0.113.23")
	}

	emailV, _ := counter.Locked(ctx, AttemptKeyEmail(PolicyAudienceOperator, "known@example.com"), Limit{Threshold: 5, Window: time.Minute})
	if emailV.Count != 0 {
		t.Fatalf("login email scope = %d after 10 resends, want 0", emailV.Count)
	}
	ipV, _ := counter.Locked(ctx, AttemptKeyIP("203.0.113.23"), Limit{Threshold: 100, Window: time.Minute})
	if ipV.Count != 0 {
		t.Fatalf("login IP scope = %d after 10 resends, want 0", ipV.Count)
	}

	// It DOES charge its own scope.
	verifyV, _ := counter.Locked(ctx, AttemptKeyVerifyEmail(PolicyAudienceOperator, "known@example.com"), VerifyRequestsPerEmail)
	if verifyV.Count == 0 {
		t.Fatal("the resend must charge its own verify-email scope")
	}
}

// The peek must not consume: the old IsBlocked pre-check spent a token
// on every call, which is how an anonymous caller could pin any address
// at 429 forever without ever failing anything.
func TestResendVerification_PeekDoesNotConsume(t *testing.T) {
	svc, tokens, _ := newRequestCapTestService(t)
	ctx := context.Background()

	// Three accepted requests are the cap; the counter must read exactly
	// 3, not 6 (peek + record on each).
	for i := 0; i < 3; i++ {
		_ = svc.ResendVerification(ctx, "unverified@example.com", "203.0.113.24")
	}
	v, _ := svc.attempts.Locked(ctx, AttemptKeyVerifyEmail(PolicyAudienceOperator, "unverified@example.com"), VerifyRequestsPerEmail)
	if v.Count != 3 {
		t.Fatalf("verify-email count = %d after 3 requests, want exactly 3", v.Count)
	}
	if tokens.createCount() != 3 {
		t.Fatalf("issued %d tokens for 3 in-cap requests, want 3", tokens.createCount())
	}
}

// A request cap must never surface as an error; the endpoint's single
// generic answer is what makes it non-enumerable.
func TestRequestCaps_AlwaysAnswerGenerically(t *testing.T) {
	svc, _, _ := newRequestCapTestService(t)
	ctx := context.Background()
	for i := 0; i < 10; i++ {
		if err := svc.ForgotPassword(ctx, "known@example.com", "203.0.113.25"); err != nil {
			t.Fatalf("ForgotPassword %d returned %v; only the method gate may return an error", i, err)
		}
		if err := svc.ResendVerification(ctx, "known@example.com", "203.0.113.25"); err != nil {
			t.Fatalf("ResendVerification %d returned %v", i, err)
		}
	}
}
```

> **Fixtures:** `newRequestCapTestService` builds a `PasswordAuthService`
> with `NewMemoryAttemptCounter()`, a recording `emailTokenRepo` fake
> exposing `createCount()` / `invalidateCount()`, a recording
> `MailDispatcher` stand-in exposing `enqueued()`, and the same
> `gateUserFake` seeded in Task 7 plus `unverified@example.com`
> (`EmailVerified: false`). For the dispatcher, give `MailDispatcher` a
> tiny test seam rather than mocking it: construct a real one, start it,
> and count through a `Send` closure the fixture supplies.

- [ ] **Step 2: Run it to verify it fails**

Run: `go test ./internal/core/auth/services/ -run 'ForgotPassword_|ResendVerification_|RequestCaps' -count=1`
Expected: FAIL — fixtures undefined; then, once they exist, the over-cap, before-the-lookup, detached-send and login-scope tests fail against today's code.

- [ ] **Step 3: Add the shared cap helper**

In `password_auth_service.go`, next to `peekLockout`:

```go
// overRequestCap peeks both request scopes. A request cap is not a
// lockout: it never produces an error, never records on the login
// scopes, and the caller's answer stays the endpoint's single generic
// success. A store error reads as "not over" (fail open, spec D1).
func (s *PasswordAuthService) overRequestCap(ctx context.Context, ipKey, emailKey string, ipLimit, emailLimit Limit) bool {
	if s.attempts == nil {
		return false
	}
	if v, err := s.attempts.Locked(ctx, ipKey, ipLimit); err == nil && v.Locked {
		return true
	}
	if v, err := s.attempts.Locked(ctx, emailKey, emailLimit); err == nil && v.Locked {
		return true
	}
	return false
}

// chargeRequestCap records one accepted request on both scopes. Called
// BEFORE the user lookup so the cost is identical for a known and an
// unknown address.
func (s *PasswordAuthService) chargeRequestCap(ctx context.Context, ipKey, emailKey string, ipLimit, emailLimit Limit) {
	if s.attempts == nil {
		return
	}
	_, _ = s.attempts.RecordFailure(ctx, ipKey, ipLimit)
	_, _ = s.attempts.RecordFailure(ctx, emailKey, emailLimit)
}
```

- [ ] **Step 4: Rewrite `ResendVerification`**

```go
// ResendVerification issues a new verification email.
//
// Always returns nil regardless of outcome so callers cannot distinguish
// "address unknown", "already verified", "over the cap" or "sent".
//
// It has its OWN request scopes. It used to pre-check IsBlocked on the
// LOGIN scopes — and IsBlocked's underlying Check consumes a token on
// every call — so an anonymous caller could pin any address at 429
// indefinitely without ever failing an authentication (M-6). A
// verification request is not a login failure and must never be able to
// lock a login.
func (s *PasswordAuthService) ResendVerification(ctx context.Context, email, ip string) error {
	email = strings.ToLower(strings.TrimSpace(email))
	ipKey := AttemptKeyVerifyIP(ip)
	emailKey := AttemptKeyVerifyEmail(s.audience, email)

	if s.overRequestCap(ctx, ipKey, emailKey, VerifyRequestsPerIP, VerifyRequestsPerEmail) {
		return nil
	}
	// Charged before the lookup: same cost for a known and an unknown
	// address.
	s.chargeRequestCap(ctx, ipKey, emailKey, VerifyRequestsPerIP, VerifyRequestsPerEmail)

	user, err := s.userService.GetUserForAuth(ctx, email)
	if err != nil {
		return nil
	}
	if user.EmailVerified {
		return nil
	}
	_ = s.emailTokenRepo.InvalidateByUserAndPurpose(ctx, user.UUID, authModels.EmailTokenPurposeVerifyEmail)
	if err := s.sendVerificationEmail(ctx, user, ip); err != nil {
		return err
	}
	return nil
}
```

- [ ] **Step 5: Rewrite `ForgotPassword`'s cap and send**

Immediately after the method gate (`enabled`/`ErrPasswordLoginDisabled` block), before the user lookup:

```go
	ipKey := AttemptKeyResetIP(ip)
	emailKey := AttemptKeyResetEmail(s.audience, email)
	if s.overRequestCap(ctx, ipKey, emailKey, ResetRequestsPerIP, ResetRequestsPerEmail) {
		// Generic success, no token, no mail — and, crucially, the
		// victim's last valid token is NOT invalidated: an attacker's
		// fourth request can no longer destroy a live reset link.
		return nil
	}
	s.chargeRequestCap(ctx, ipKey, emailKey, ResetRequestsPerIP, ResetRequestsPerEmail)
```

and replace the synchronous `SendTemplated` tail (`:1075-1090`) with:

```go
	req := iface.TemplatedNotificationRequest{
		Channel:    "email",
		Type:       "transactional",
		Category:   notifModels.CategoryAuthResetPassword,
		TemplateID: "auth.reset_password",
		Recipients: []iface.Recipient{{
			UserUUID: user.UUID,
			Address:  user.Email,
			Name:     user.FullName,
		}},
		Data: map[string]any{
			"UserName":     coalesce(user.FullName, user.Email),
			"ResetURL":     resetURL,
			"ExpiresIn":    humanDuration(resetTTL),
			"RequestIP":    ip,
			"AppName":      s.appName,
			"SupportEmail": s.supportEmail,
		},
		IdempotencyKey: "reset:" + user.UUID + ":" + doc.UUID,
	}
	// Detached: the handler must not wait on the relay, or its latency
	// would depend on whether the account existed. A full queue drops
	// the mail with a metric; the user retries inside the caps above.
	notifier := s.notifier
	s.mail.Enqueue(MailJob{
		TemplateID: "auth.reset_password",
		Send: func(sendCtx context.Context) error {
			_, err := notifier.SendTemplated(sendCtx, req)
			return err
		},
	})
	return nil
```

Add `mail *MailDispatcher` to the service struct, `MailDispatcher *MailDispatcher` to `PasswordAuthConfig`, and `mail: cfg.MailDispatcher,` to the constructor (Task 6 Step 5 already threads the value through the bundle).

- [ ] **Step 6: Run the tests**

Run: `go test ./internal/core/auth/services/ -run 'ForgotPassword|ResendVerification|RequestCaps' -count=1`
Expected: PASS, including the pre-existing `TestForgotPassword_PasswordMethodGate`.

- [ ] **Step 7: Vet, full suite, commit**

```bash
go vet ./... && go test ./internal/core/auth/... -count=1
git add backend/internal/core/auth
git commit -m "$(cat <<'EOF'
fix(auth): give reset and resend their own request caps and a detached send

ResendVerification pre-checked IsBlocked on the LOGIN scopes, and
IsBlocked consumes a token on every call — so an anonymous caller could
pin any address at 429 indefinitely without ever failing an
authentication, and a verification request could lock a login. Both
endpoints now use their own reset-*/verify-* scopes, peeked without
consuming and charged BEFORE the user lookup so a known and an unknown
address cost the same.

ForgotPassword invalidated the previous token on every call with no
throttle, which let an attacker destroy a victim's live reset link at
will. Over the cap it now does nothing at all — no token, no
invalidation, no mail — behind the same generic success.

The reset mail is handed to the bounded dispatcher instead of being sent
inline, so the response time no longer depends on the relay or on
whether the account existed.

Spec §4.1 D5. Closes M-5 and M-6.

Claude-Session: https://claude.ai/code/session_0155Lt3QwR9zX15oGNKrn1T1
EOF
)"
```

---

## Task 9: Change-password and password-confirm join the lockout (D6)

Both verify a password and neither is throttled or audited on failure, so they are the unthrottled back door around the login lockout (M-8).

**Files:**
- Modify: `backend/internal/core/auth/services/password_auth_service.go` (`ChangePasswordInput`, `ChangePassword` `:1154-1214`, `ConfirmPasswordWithSecurity` `:1251-1333`)
- Modify: `backend/internal/core/auth/handlers/password_handler.go` (fill `ChangePasswordInput.IP`; map the sentinel on both routes)
- Test: `backend/internal/core/auth/services/password_auth_reconfirm_lockout_test.go` (new)

**Interfaces:**
- Consumes: `peekLockout`, `recordLoginFailure`, `resetLoginFailures`, `dummyVerify`, `LockedAfter` (Tasks 1, 7).
- Produces: `ChangePasswordInput.IP string`.

- [ ] **Step 1: Write the failing tests**

Create `backend/internal/core/auth/services/password_auth_reconfirm_lockout_test.go`:

```go
package services

import (
	"context"
	"errors"
	"testing"
)

// Both endpoints verify a password. Leaving them unthrottled makes them
// the back door around the login lockout.
func TestChangePassword_LocksAfterThreshold(t *testing.T) {
	svc, _ := newLockoutTestServiceWithPassword(t, lockoutTestThreshold)
	ctx := context.Background()

	for i := 0; i < lockoutTestThreshold; i++ {
		err := svc.ChangePassword(ctx, ChangePasswordInput{
			UserUUID: knownTestUserUUID, Current: "wrong", New: "NewPassw0rd!x", IP: "203.0.113.30",
		})
		if !errors.Is(err, ErrInvalidCredentials) {
			t.Fatalf("attempt %d = %v, want ErrInvalidCredentials", i+1, err)
		}
	}
	err := svc.ChangePassword(ctx, ChangePasswordInput{
		UserUUID: knownTestUserUUID, Current: "wrong", New: "NewPassw0rd!x", IP: "203.0.113.30",
	})
	if !errors.Is(err, ErrAccountLocked) {
		t.Fatalf("attempt %d = %v, want ErrAccountLocked", lockoutTestThreshold+1, err)
	}
}

func TestConfirmPassword_LocksAfterThreshold(t *testing.T) {
	svc, _ := newLockoutTestServiceWithPassword(t, lockoutTestThreshold)
	ctx := context.Background()
	sec := &authModels.SecurityContext{SessionID: "sid-1", IPAddress: "203.0.113.31"}

	for i := 0; i < lockoutTestThreshold; i++ {
		_, err := svc.ConfirmPasswordWithSecurity(ctx, knownTestUserUUID, "wrong", []string{"pwd"}, nil, sec)
		if !errors.Is(err, ErrInvalidCredentials) {
			t.Fatalf("attempt %d = %v", i+1, err)
		}
	}
	_, err := svc.ConfirmPasswordWithSecurity(ctx, knownTestUserUUID, "wrong", []string{"pwd"}, nil, sec)
	if !errors.Is(err, ErrAccountLocked) {
		t.Fatalf("attempt %d = %v, want ErrAccountLocked", lockoutTestThreshold+1, err)
	}
}

// A lockout earned at LOGIN must be honoured here too, or the lock is
// worth nothing.
func TestChangePassword_HonoursALoginEarnedLock(t *testing.T) {
	svc, _ := newLockoutTestServiceWithPassword(t, lockoutTestThreshold)
	ctx := context.Background()

	for i := 0; i < lockoutTestThreshold; i++ {
		_, _ = svc.Login(ctx, LoginInput{Email: "known@example.com", Password: "wrong", IP: "203.0.113.32"})
	}
	err := svc.ChangePassword(ctx, ChangePasswordInput{
		UserUUID: knownTestUserUUID, Current: "wrong", New: "NewPassw0rd!x", IP: "203.0.113.32",
	})
	if !errors.Is(err, ErrAccountLocked) {
		t.Fatalf("change-password after a login lockout = %v, want ErrAccountLocked", err)
	}
}

// A failure must leave an audit row. Today ChangePassword records
// nothing at all on a wrong current password.
func TestChangePassword_FailureIsAudited(t *testing.T) {
	svc, audit := newLockoutTestServiceWithAudit(t, lockoutTestThreshold)
	ctx := context.Background()

	_ = svc.ChangePassword(ctx, ChangePasswordInput{
		UserUUID: knownTestUserUUID, Current: "wrong", New: "NewPassw0rd!x", IP: "203.0.113.33",
	})
	if !audit.sawAction("auth.password.change_failed") {
		t.Fatal("a wrong current password must leave an audit row")
	}
}

func TestConfirmPassword_FailureIsAudited(t *testing.T) {
	svc, audit := newLockoutTestServiceWithAudit(t, lockoutTestThreshold)
	ctx := context.Background()
	sec := &authModels.SecurityContext{SessionID: "sid-1", IPAddress: "203.0.113.34"}

	_, _ = svc.ConfirmPasswordWithSecurity(ctx, knownTestUserUUID, "wrong", []string{"pwd"}, nil, sec)
	if !audit.sawAction("auth.password.reconfirm_failed") {
		t.Fatal("a failed reconfirm must leave an audit row")
	}
}

// A success clears the email scope, so a user who mistyped twice and
// then succeeded is not one attempt from a lockout.
func TestChangePassword_SuccessResetsTheEmailScope(t *testing.T) {
	svc, _ := newLockoutTestServiceWithPassword(t, 10)
	ctx := context.Background()

	for i := 0; i < 3; i++ {
		_ = svc.ChangePassword(ctx, ChangePasswordInput{
			UserUUID: knownTestUserUUID, Current: "wrong", New: "NewPassw0rd!x", IP: "203.0.113.35",
		})
	}
	if err := svc.ChangePassword(ctx, ChangePasswordInput{
		UserUUID: knownTestUserUUID, Current: correctTestPassword, New: "NewPassw0rd!x", IP: "203.0.113.35",
	}); err != nil {
		t.Fatalf("successful change: %v", err)
	}
	v, _ := svc.attempts.Locked(ctx, AttemptKeyEmail(PolicyAudienceOperator, "known@example.com"), Limit{Threshold: 10, Window: time.Minute})
	if v.Count != 0 {
		t.Fatalf("email scope = %d after a successful change, want 0", v.Count)
	}
}
```

Add the `authModels` and `time` imports. `knownTestUserUUID` is the UUID
the Task 7 fixture assigns to `known@example.com`; `newLockoutTestServiceWithAudit`
returns a recording `emitAudit` sink exposing `sawAction(string) bool`.

- [ ] **Step 2: Run it to verify it fails**

Run: `go test ./internal/core/auth/services/ -run 'ChangePassword_|ConfirmPassword_Lock|ConfirmPassword_Failure' -count=1`
Expected: FAIL — `ChangePasswordInput` has no `IP`, neither method throttles, neither audits a failure.

- [ ] **Step 3: Add `IP` to the input**

```go
type ChangePasswordInput struct {
	UserUUID   string
	CurrentSID string
	Current    string
	New        string
	// IP is the caller's resolved address, used for the attempt
	// counters and the audit row. Empty skips the address scope (the
	// counters' empty-key rule) rather than sharing a bucket.
	IP string
}
```

- [ ] **Step 4: Gate `ChangePassword`**

Immediately after the `GetUserByID` / `PasswordHash == ""` checks and **before** the verify:

```go
	// This route verifies a password, so it is subject to the same
	// lockout as login — otherwise it is the unthrottled back door
	// around it (M-8). Durable lock first, then the counters.
	if user.LockedUntil != nil && time.Now().Before(*user.LockedUntil) {
		s.dummyVerify(current)
		return LockedAfter(time.Until(*user.LockedUntil))
	}
	if v, locked := s.peekLockout(ctx, in.IP, user.Email); locked {
		s.dummyVerify(current)
		return LockedAfter(v.RetryAfter)
	}
```

and replace the wrong-password branch:

```go
	ok, err := s.passwordService.Verify(current, user.PasswordHash)
	if err != nil || !ok {
		emailVerdict, counterAvailable := s.recordLoginFailure(ctx, in.IP, user.Email)
		var lockUntil *time.Time
		lock := emailVerdict.Locked
		if !counterAvailable {
			lock = user.FailedLoginCount+1 >= s.policy.LockoutThreshold(ctx)
		}
		if lock {
			t := time.Now().Add(s.policy.LockoutDuration(ctx))
			lockUntil = &t
		}
		_ = s.userService.RecordFailedLogin(ctx, user.UUID, lockUntil)
		s.emitAudit(ctx, iface.AuditEvent{
			ActorUserID:  user.UUID,
			ActorEmail:   user.Email,
			ActorType:    "user",
			Action:       "auth.password.change_failed",
			Outcome:      "failure",
			IPAddress:    in.IP,
			ResourceType: "user",
			ResourceID:   user.UUID,
		})
		return ErrInvalidCredentials
	}
```

and at the success site, next to the existing side effects:

```go
	s.resetLoginFailures(ctx, user.Email)
```

> `RequireStepUp` is deliberately **not** added to `/change-password`
> (the audit's optional suggestion): the route already verifies the
> current password and the SPA treats it as a plain form.

- [ ] **Step 5: Gate `ConfirmPasswordWithSecurity`**

Same shape, with the IP taken from `security.IPAddress`. Place the peek after the existing eligibility refusals (`ValidateTokenEligibleUser`, no-hash, method gate, enrolled-factor) and before the verify; replace the wrong-password branch with the `recordLoginFailure` + durable-write + `auth.password.reconfirm_failed` audit version, and add `s.resetLoginFailures(ctx, user.Email)` on success.

- [ ] **Step 6: Fill `IP` at the handler**

In `backend/internal/core/auth/handlers/password_handler.go`, at every construction of `services.ChangePasswordInput`, add:

```go
		IP: clientIPFromCtx(ctx),
```

and confirm both the change-password and the password-confirm routes run
their error through `mapPasswordError` (so `ErrAccountLocked` renders the
Task 1 coded 429 + `Retry-After`). If either has its own switch, add the
`lockoutError(services.RetryAfterFor(err))` arm there too.

- [ ] **Step 7: Run the tests**

Run: `go test ./internal/core/auth/... -count=1`
Expected: PASS, including the pre-existing `TestConfirmPassword_*` and `TestChangePassword_*`.

- [ ] **Step 8: Vet and commit**

```bash
go vet ./... && go test ./internal/core/auth/... -count=1
git add backend/internal/core/auth
git commit -m "$(cat <<'EOF'
fix(auth): put change-password and password-confirm behind the lockout

Both verify a password and neither was throttled or audited on failure,
which made them the unthrottled back door around the login lockout: an
attacker holding a session could guess the current password without
limit, and a lock earned at login was not honoured on either route.

Both now peek the durable lock and the counters first (answering the
coded 429 with Retry-After), record a failure on the same scopes login
uses, mirror the durable lock, leave an auth.password.change_failed /
auth.password.reconfirm_failed audit row, and reset the email scope on
success.

Spec §4.1 D6. Closes M-8.

Claude-Session: https://claude.ai/code/session_0155Lt3QwR9zX15oGNKrn1T1
EOF
)"
```

---

## Task 10: Service accounts use the same counters (D7)

**Files:**
- Modify: `backend/internal/core/auth/services/service_account_service.go` (`Grant` `:719-780`, `recordFailed` `:783-793`, the `limiter` field `:72`, `:101`)
- Modify: `backend/internal/core/auth/handlers/service_token_handler.go:70`
- Modify: `backend/internal/core/auth/module.go` (constructor argument)
- Test: `backend/internal/core/auth/services/service_account_grant_test.go`

**Interfaces:**
- Consumes: `AttemptCounter`, `AttemptKeyIP`, `AttemptKeyClient` (Task 3); `LockedAfter`, `lockoutError` (Task 1).
- Produces: `ServiceAccountService.counter AttemptCounter` replacing `limiter *sharederrors.RateLimiter`.

- [ ] **Step 1: Port the existing tests**

In `backend/internal/core/auth/services/service_account_grant_test.go`, change the construction of the service under test to pass `NewMemoryAttemptCounter()` where it passes a `*sharederrors.RateLimiter` today, and keep both existing tests as they are:

- `TestGrantRateLimited` — must still lock after `threshold` bad secrets.
- `TestGrantSuccessiveSuccessesNotRateLimited` — stays green **by construction**: peeks never consume, so back-to-back legitimate grants can no longer lock themselves out (that was the whole reason `IsLockedOut` existed).

Add one new test:

```go
// A client ID IS an account, so it carries the account pair; the
// address it grants from carries the much looser address pair. One
// build server hammering with a bad secret must lock the CLIENT, not
// every service account behind that egress.
func TestGrant_ClientLocksBeforeTheAddress(t *testing.T) {
	svc, counter := newGrantTestServiceWithCounter(t, 3 /* account */, 100 /* address */)
	ctx := context.Background()

	for i := 0; i < 3; i++ {
		_, _ = svc.Grant(ctx, GrantInput{
			GrantType: "client_credentials", ClientID: "svc-a", ClientSecret: "wrong", IP: "203.0.113.40",
		})
	}
	if _, err := svc.Grant(ctx, GrantInput{
		GrantType: "client_credentials", ClientID: "svc-a", ClientSecret: "wrong", IP: "203.0.113.40",
	}); !errors.Is(err, ErrClientRateLimited) {
		t.Fatalf("client svc-a should be locked, got %v", err)
	}
	// A DIFFERENT client from the same address is unaffected.
	if _, err := svc.Grant(ctx, GrantInput{
		GrantType: "client_credentials", ClientID: "svc-b", ClientSecret: "wrong", IP: "203.0.113.40",
	}); errors.Is(err, ErrClientRateLimited) {
		t.Fatal("a second client from the same address must not inherit the first one's lock")
	}
	_ = counter
}
```

- [ ] **Step 2: Run it to verify it fails**

Run: `go test ./internal/core/auth/services/ -run Grant -count=1`
Expected: FAIL to compile — the constructor still wants a `*RateLimiter`.

- [ ] **Step 3: Swap the dependency**

In `service_account_service.go`: change the struct field and the constructor parameter from `limiter *sharederrors.RateLimiter` to `counter AttemptCounter`, drop the `sharederrors` import if it becomes unused, and rewrite the two sites:

```go
	// The lockout pre-check PEEKS. The limiter this replaces had to
	// expose a separate IsLockedOut for exactly this reason: its Check
	// consumed a token on every call, so gating every Grant on it let
	// back-to-back legitimate calls lock themselves out. Peeks are free
	// by construction now.
	if s.counter != nil {
		accountLim := Limit{Threshold: s.policy.LockoutThreshold(ctx), Window: s.policy.LockoutDuration(ctx)}
		addressLim := Limit{Threshold: s.policy.IPLockoutThreshold(ctx), Window: s.policy.IPLockoutDuration(ctx)}
		if v, err := s.counter.Locked(ctx, AttemptKeyIP(in.IP), addressLim); err == nil && v.Locked {
			return nil, LockedAfter(v.RetryAfter)
		}
		if v, err := s.counter.Locked(ctx, AttemptKeyClient(in.ClientID), accountLim); err == nil && v.Locked {
			return nil, LockedAfter(v.RetryAfter)
		}
	}
```

Delete the `SetAuthFailedConfig` block above it — the limits are read per call now — and rewrite `recordFailed`:

```go
// recordFailed charges one failed attempt against the caller's address
// and the targeted clientId, so a distributed attacker (many IPs, one
// clientId) and a credential-stuffing attacker (one IP, many clientIds)
// are both throttled. A client ID IS an account and carries the account
// pair; the address carries the looser address pair.
func (s *ServiceAccountService) recordFailed(ctx context.Context, in GrantInput) {
	if s.counter == nil {
		return
	}
	_, _ = s.counter.RecordFailure(ctx, AttemptKeyIP(in.IP),
		Limit{Threshold: s.policy.IPLockoutThreshold(ctx), Window: s.policy.IPLockoutDuration(ctx)})
	_, _ = s.counter.RecordFailure(ctx, AttemptKeyClient(in.ClientID),
		Limit{Threshold: s.policy.LockoutThreshold(ctx), Window: s.policy.LockoutDuration(ctx)})
}
```

`ErrClientRateLimited` keeps its identity for the log line: return it unchanged when no verdict is at hand, and `LockedAfter(v.RetryAfter)` when one is. Both sentinels are mapped to the same wire answer in the next step — do **not** try to make one `errors.Is` the other.

- [ ] **Step 4: Map the sentinel on the wire**

In `backend/internal/core/auth/handlers/service_token_handler.go:70`:

```go
	case errors.Is(err, services.ErrClientRateLimited), errors.Is(err, services.ErrAccountLocked):
		return lockoutError(services.RetryAfterFor(err))
```

- [ ] **Step 5: Update the module wiring**

In `backend/internal/core/auth/module.go`, pass `attemptCounter` where the `ServiceAccountService` constructor takes the limiter today.

- [ ] **Step 6: Run and commit**

```bash
go vet ./... && go test ./internal/core/auth/... -count=1
git add backend/internal/core/auth
git commit -m "$(cat <<'EOF'
fix(auth): move the service-account grant onto the attempt counters

The client-credentials grant shared the in-memory limiter's config map,
which is one of the two writers in the H-1 race, and it needed a
bespoke IsLockedOut peek because the ordinary Check consumed a token on
every call. Both problems go away with the counters: peeks are free by
construction, the limits are read per call, and the lockout is shared
across replicas.

A client ID is an account and carries the account pair; the address
carries the much looser address pair, so one build server with a stale
secret locks its own client rather than every service account behind
that egress. ErrClientRateLimited now answers 429 auth.too_many_attempts
with Retry-After like every other lockout.

Spec §4.1 D7.

Claude-Session: https://claude.ai/code/session_0155Lt3QwR9zX15oGNKrn1T1
EOF
)"
```

---

## Task 11: Shrink the in-memory limiter to what it still serves (D8) — the H-1 fix

Every consumer has moved. What remains is the per-IP `api:general` middleware mounted at `cmd/server/middleware.go:131`. Deleting the auth-facing surface removes the **writer** in the H-1 race; locking the remaining reads removes the race itself, including the second one the audit found at `rate_limiter.go:129` (`bucket.tokens` read outside `tb.mu`).

**Files:**
- Modify: `backend/internal/shared/errors/rate_limiter.go`
- Delete tests: `backend/internal/shared/errors/rate_limiter_test.go:11-107`, `errors_test.go:122-148` (`TestSetAuthFailedConfig`)
- Create: `backend/internal/shared/errors/rate_limiter_race_test.go`
- Modify: `backend/internal/core/auth/module.go` (drop `rateLimiter := sharederrors.NewRateLimiter()`)
- Modify: `backend/internal/core/auth/tier_bundle.go` (drop the `rateLimiter` field)
- Modify: `backend/internal/core/auth/services/password_auth_service.go` (drop `RateLimiter` from config + struct)
- Modify: `backend/internal/core/auth/CLAUDE.md`

**Interfaces:**
- Consumes: nothing new.
- Produces: `RateLimiter` reduced to `NewRateLimiter`, `Check`, `Middleware`, `Close`, `cleanup`, `cleanupOldBuckets`, `getBucket`, and the single `api:general` config.

- [ ] **Step 1: Write the failing race probe**

Create `backend/internal/shared/errors/rate_limiter_race_test.go`:

```go
package errors

// H-1: RateLimiter.Check read rl.configs without a lock while
// SetAuthFailedConfig wrote it on every login and every service-account
// grant. A concurrent map read and write is a FATAL runtime error — not
// a recoverable panic — so any anonymous caller could stop the process.
//
// This probe is kept permanently: it is cheap, and it is the regression
// test for a defect whose blast radius is the whole process.

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync"
	"testing"
)

func TestRateLimiter_ConcurrentCheckAndMiddlewareIsRaceFree(t *testing.T) {
	rl := NewRateLimiter()
	t.Cleanup(rl.Close)

	handler := rl.Middleware("api:general")(http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) }))

	var wg sync.WaitGroup
	for i := 0; i < 64; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				rl.Check(context.Background(), "ip:"+strconv.Itoa(i), "api:general")

				req := httptest.NewRequest(http.MethodGet, "/", nil)
				req.RemoteAddr = "203.0.113." + strconv.Itoa(i%256) + ":1234"
				handler.ServeHTTP(httptest.NewRecorder(), req)
			}
		}(i)
	}
	wg.Wait()
}

// An unknown config name must fall back to api:general WITHOUT reading
// the map twice outside the lock.
func TestRateLimiter_UnknownConfigFallsBackUnderLock(t *testing.T) {
	rl := NewRateLimiter()
	t.Cleanup(rl.Close)

	res := rl.Check(context.Background(), "k", "does:not:exist")
	if res == nil {
		t.Fatal("Check must never return nil")
	}
}

// The auth-facing surface is GONE. These identifiers must not come back:
// they are what made an anonymous request able to write a shared map.
func TestRateLimiter_AuthSurfaceRemoved(t *testing.T) {
	// Compile-time assertions live in the type; this test documents the
	// contract for a reader. If any of the following exists again, the
	// H-1 writer is back and the counters have been bypassed:
	//   SetAuthFailedConfig, IsBlocked, IsLockedOut, RecordFailedAuth,
	//   CheckMultiple, AuthMiddleware
	// and the configs auth:login, auth:refresh, auth:failed,
	// security:sensitive, global:ip.
	rl := NewRateLimiter()
	t.Cleanup(rl.Close)
	if len(rl.configs) != 1 {
		t.Fatalf("configs = %d, want exactly 1 (api:general)", len(rl.configs))
	}
	if _, ok := rl.configs["api:general"]; !ok {
		t.Fatal("api:general must be the surviving config")
	}
}
```

- [ ] **Step 2: Run it to verify it fails**

Run: `go test ./internal/shared/errors/ -race -count=1 -run RateLimiter`
Expected: `TestRateLimiter_AuthSurfaceRemoved` FAILS (6 configs). The race probe may pass today because `SetAuthFailedConfig` is not among the concurrent callers — that is fine; the probe's job is to stay green after the shrink.

- [ ] **Step 3: Delete the auth-facing surface**

From `backend/internal/shared/errors/rate_limiter.go`, delete:
`SetAuthFailedConfig`, `IsBlocked`, `IsLockedOut`, `RecordFailedAuth`, `CheckMultiple`, `AuthMiddleware`, the `RateLimitCheck` type if it becomes unused, `TokenBucket.peek` if it becomes unused, and the four dead configs (`auth:login`, `auth:refresh`, `security:sensitive`, `global:ip`) plus `auth:failed` from `setDefaultConfigs`.

Rewrite the doc comment on the type:

```go
// RateLimiter is the per-IP request bound behind the api:general
// middleware, and nothing else.
//
// It used to carry the auth lockout too, through a config map that
// SetAuthFailedConfig rewrote on EVERY login and service-account grant
// while Check, IsBlocked and IsLockedOut read it without a lock. A
// concurrent map read and write is a fatal runtime error, so any
// anonymous caller could stop the process (H-1). Lockout now lives in
// the Redis attempt counters
// (internal/core/auth/services/attempt_counter.go), which are shared
// across replicas, honour the admin-managed window, and survive a
// restart — none of which a per-process token bucket could do.
type RateLimiter struct {
```

- [ ] **Step 4: Lock every remaining read**

```go
func (rl *RateLimiter) Check(ctx context.Context, key string, configName string) *RateLimitResult {
	config := rl.configFor(configName)

	bucketKey := fmt.Sprintf("%s:%s", configName, key)
	bucket := rl.getBucket(bucketKey, config)

	allowed := bucket.consume(1)
	// remaining is read under the bucket's own mutex: consume mutates
	// tokens, and a second goroutine's consume races this read.
	remaining := bucket.remaining()

	result := &RateLimitResult{
		Allowed:   allowed,
		Remaining: remaining,
		ResetTime: time.Now().Add(config.Window),
	}
	if !allowed {
		result.RetryAfter = time.Duration(float64(time.Second) / float64(config.RefillRate))
	}
	return result
}

// configFor resolves a config under the read lock, falling back to
// api:general. One lookup, one lock acquisition — the previous shape
// read the map twice, unlocked, and the fallback read could observe a
// map mid-write.
func (rl *RateLimiter) configFor(name string) *RateLimitConfig {
	rl.mu.RLock()
	defer rl.mu.RUnlock()
	if c, ok := rl.configs[name]; ok {
		return c
	}
	return rl.configs["api:general"]
}

// remaining reads the token count under the bucket's mutex.
func (tb *TokenBucket) remaining() int {
	tb.mu.Lock()
	defer tb.mu.Unlock()
	return int(tb.tokens)
}
```

And in `Middleware`, replace the three unlocked `rl.configs[configName]` reads (`:349`, `:358`, `:359`) with one `cfg := rl.configFor(configName)` hoisted above them.

- [ ] **Step 5: Run the race probe**

Run: `go test ./internal/shared/errors/ -race -count=1`
Expected: PASS, no race reported.

- [ ] **Step 6: Delete the dead tests**

Remove `TestSetAuthFailedConfig` from `errors_test.go:122-148` and the `IsBlocked`/`IsLockedOut`/`RecordFailedAuth` tests from `rate_limiter_test.go:11-107`. Keep any test that exercises `Check` or `Middleware`.

- [ ] **Step 7: Drop the auth-module wiring**

- `backend/internal/core/auth/module.go`: delete `rateLimiter := sharederrors.NewRateLimiter()` and both `rateLimiter: rateLimiter,` arguments; drop the `sharederrors` import if unused.
- `backend/internal/core/auth/tier_bundle.go`: delete the `rateLimiter` field and the `RateLimiter:` line in the `PasswordAuthConfig` literal.
- `backend/internal/core/auth/services/password_auth_service.go`: delete `RateLimiter` from `PasswordAuthConfig`, `rateLimiter` from the struct, and its constructor assignment.

- [ ] **Step 8: Build the whole tree and run everything**

Run:
```
go vet ./... && go test ./... -count=1
```
Expected: PASS. `go vet` is what catches an orphaned import or an unused field that `go build` would tolerate in a `_test.go`-free build.

- [ ] **Step 9: Correct the CLAUDE.md sentences**

`backend/internal/core/auth/CLAUDE.md` `:28`, `:116`, `:185`, `:341`, `:1071`, `:1091` — the limiter is no longer "shared with Register/ForgotPassword" and is no longer "the only protection". Point each at the attempt-counter section written in Task 7.

- [ ] **Step 10: Commit**

```bash
git add backend/internal/shared/errors backend/internal/core/auth
git commit -m "$(cat <<'EOF'
fix(auth): remove the anonymous-request process crash (H-1)

RateLimiter.Check, IsBlocked, IsLockedOut and Middleware read rl.configs
without a lock while SetAuthFailedConfig wrote it on EVERY login and
every service-account grant. A concurrent map read and write is a fatal
runtime error, not a recoverable panic, so any anonymous caller could
stop the process; go test -race reproduces it.

The writer is gone: lockout lives in the Redis attempt counters now, so
the auth-facing surface (SetAuthFailedConfig, IsBlocked, IsLockedOut,
RecordFailedAuth, CheckMultiple, the unmounted AuthMiddleware and the
four dead configs) is deleted. What remains is the per-IP api:general
middleware, whose every config read now takes the lock and whose token
count is read under the bucket's own mutex — the second race the
fact-finding surfaced.

A permanent -race probe replaces the tests of the deleted surface.

Spec §4.1 D8. Closes H-1.

Claude-Session: https://claude.ai/code/session_0155Lt3QwR9zX15oGNKrn1T1
EOF
)"
```

---

## Task 12: Documentation, OpenAPI and the staging drill

**Files:**
- Modify: `docs/site/modules/core/auth.mdx` (`:22`, `:126`, `:131` + a new lockout paragraph)
- Modify: `backend/internal/core/auth/CLAUDE.md` (final pass)
- Modify: `backend/openapi/enterprise.json` (regenerated)

- [ ] **Step 1: Regenerate the OpenAPI dump**

Run: `make -C /home/tore/orkestra openapi-dump` (or the target `make ci-help` lists for it). The 429 responses gained a `code` field and a `Retry-After` header, so the dump changes; `openapi-check` in `ci-backend` fails otherwise.

- [ ] **Step 2: Write the docs-site lockout paragraph**

In `docs/site/modules/core/auth.mdx`, add under the login section:

```mdx
### Lockout

Failed attempts are counted in Redis in fixed windows, so the limits hold
across every replica and survive a restart.

| Scope | Counted on | Limit |
|---|---|---|
| Email (per surface) | login, change-password, password-confirm | `accountLockoutThreshold` / `accountLockoutDuration` (5 / 15m) |
| Source address | the same three, plus the service-account grant | `ipLockoutThreshold` / `ipLockoutDuration` (100 / 15m) |
| Client ID | service-account grant | the account pair |
| Reset / verification requests | forgot-password, resend-verification | 3 per address per 15m, 60 per source address |

An address threshold **must** be at least the account threshold: an
address that locks first would tell an attacker which accounts exist
behind a shared office or VPN egress. The console refuses a lower value
with `auth.ip_threshold_below_account`.

A locked scope answers **429** with a `Retry-After` header, and answers
it identically for an address that exists and one that does not. A
successful login clears the email counter; the address counter is left
alone, so one correct sign-in cannot launder a credential-stuffing run.

If Redis is unavailable the counters fail open and the durable
per-account lock takes over, which still caps guessing against an
existing account. `orkestra_auth_attempt_store_failures_total` is the
signal that this is happening.
```

Fix `:22`, `:126` and `:131`, which describe the in-memory bucket.

- [ ] **Step 3: Run the full gate**

```bash
make -C /home/tore/orkestra ci-backend
git diff --check
```
Expected: green, no trailing whitespace.

- [ ] **Step 4: Commit and open the PR**

```bash
git add docs/site backend/openapi backend/internal/core/auth
git commit -m "$(cat <<'EOF'
docs(auth): document the Redis attempt counters and the lockout contract

Spec §4.11. Regenerates the OpenAPI dump for the 429 code + Retry-After.

Claude-Session: https://claude.ai/code/session_0155Lt3QwR9zX15oGNKrn1T1
EOF
)"
git push -u origin feat/auth-authz-audit-remediation
gh pr create --base dev --title "PR A: attempt counters and lockout (auth/authz audit remediation)" --body "$(cat <<'EOF'
Implements §4.1 (D1–D10) of `docs/superpowers/specs/2026-09-03-auth-authz-audit-remediation-design.md`, the first of five deliverables in §7.

**Closes:** H-1 (anonymous-request process crash), M-5, M-6, M-7, M-8.

Plan: `docs/superpowers/plans/2026-09-03-auth-authz-audit-remediation-pr-a-attempt-counters.md`

https://claude.ai/code/session_0155Lt3QwR9zX15oGNKrn1T1
EOF
)"
```

- [ ] **Step 5: Staging drill (spec §7, PR A row)**

Read the live stack first — `grep "^ENV=" docker/.env` and `sed -n 's/^HOST_BIND_ADDRESS=//p' docker/.env` (`~/orkestra` runs `orkestra-public-*-staging`, not a dev stack). Then verify, in order:

1. Six wrong passwords for one account from one address → the sixth answers **429** with `Retry-After`.
2. The same six against an address that does not exist → **identical** status sequence.
3. `forgot-password` four times for one address → the third mail is the last; the victim's live token still works.
4. `curl` `/metrics` and confirm `orkestra_auth_lockouts_total{scope="email"}` moved and `orkestra_auth_attempt_store_failures_total` is zero.
5. Stop Redis, attempt six logins against an **existing** account → the durable lock still engages; `orkestra_auth_attempt_store_failures_total` moves; the process stays up. Restart Redis.
6. Lower `accountLockoutThreshold` in `/admin/modules` while a window is open → the next attempt locks immediately (edge case 3). Try to set `ipLockoutThreshold` below it → 422 `auth.ip_threshold_below_account`.

---

## Self-review

**Spec coverage (§4.1 D1–D10 + §6 "PR A — counters"):**

| Spec item | Task |
|---|---|
| D1 counter service, Lua script, `EVAL` boot requirement, errors returned, throttled WARN | 3 |
| D1 `OAuthStateStore.Incr` on the same script | 5 |
| D2 keys, scopes, limits, empty-IP skip, per-audience email | 3 |
| D2 IP pair, accessors, `ValidateConfigSnapshot` rule, config-group tests | 4 |
| D3 login order, dummy verify on every branch, identical answers | 7 |
| D4 durable lock mirrors the counter, `counterAvailable` fallback | 7 |
| D5 request caps, charge-before-lookup, `IsBlocked` removal from resend | 8 |
| D5 bounded dispatcher (queue, workers, non-blocking, drain) | 6 |
| D6 change-password + password-confirm | 9 |
| D7 service accounts | 10 |
| D8 limiter shrink + locking | 11 |
| D9 429 wire shape + `Retry-After` | 1 |
| D10 telemetry | 2 |
| §4.11 docs | 4, 7, 11, 12 |
| §6 `attempt_counter_test.go`, `mail_dispatcher_test.go`, `mfa_challenge_service_test.go` TTL healing, password-service parity, service-account port, `rate_limiter` race probe, `error_mapping_test.go`, `codes_test.go`, IP-pair tests | 3, 6, 5, 7/8/9, 10, 11, 1, 4 |

**Deliberately deferred to PR B:** the `mfa-verify` scope is *defined* in Task 3 and *wired* in PR B (D20) — the spec assigns it there.

**Placeholder scan:** no "TBD", no "add error handling", no "similar to Task N". Two places name a fixture the executor must extend rather than showing it in full — `gates_fakes_test.go` (Task 7 Step 1) and `newRequestCapTestService` (Task 8 Step 1) — because the existing fakes are the authority on their own shape and forking them would be the worse outcome. Each says exactly what to seed and what accessors to add.

**Type consistency:** `AttemptCounter` / `Limit` / `Verdict` are used with the same names and signatures in Tasks 3, 7, 8, 9, 10. `LockedAfter` / `RetryAfterFor` are introduced in Task 1 and consumed in 7, 9, 10. `MailJob` / `MailDispatcher.Enqueue` are introduced in Task 6 and consumed in Task 8. `peekLockout` / `recordLoginFailure` / `resetLoginFailures` / `dummyVerify` are introduced in Task 7 and reused in Task 9. `AttemptKeyIP` returns `""` for an empty address in Task 3 and every caller relies on that rather than branching.
