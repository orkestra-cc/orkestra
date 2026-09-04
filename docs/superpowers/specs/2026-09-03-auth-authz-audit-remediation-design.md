# Auth/authz audit remediation — design

| Field | Value |
|---|---|
| **Date** | 2026-09-03 |
| **Last review** | — |
| **Status** | v1.13 — PR A implemented and merged; D4 amended to match what shipped |
| **Scope** | `backend/internal/core/auth` (services, handlers, module wiring), `backend/internal/core/authz` (services, handlers, Cedar policies), `backend/internal/core/user` (role-change handlers), `backend/internal/shared/{middleware,errors,errcode,systeminit,database}`, `backend/pkg/sdk/iface` (one additive interface), `backend/migrations` (one new migration), `frontend-client` (MFA enrolment page), notification templates, `docs/site`, module `CLAUDE.md`s |
| **Source** | Security audit of 2026-09-03 on `dev` @ `a242e6bd` — report: https://claude.ai/code/artifact/5e5c5406-19bd-4cb1-8cd1-86cd9440b245. Finding IDs below (H-n, M-n, L-n) are the report's |
| **ADR** | None. Every SDK change is additive (§4.3 adds the `iface.MFAEpochBumper` sub-interface and one `iface.User` field, §4.6 adds `iface.AuthzCacheInvalidator`, §4.7 adds `iface.SystemRoleHolderFinder`; `module.RedisClient` is untouched). Lockout moves from an in-memory token bucket to Redis counters, which changes an operational property, not a contract: the two admin-managed keys keep their names and gain the meaning their UI text already promises |

## 0. Revision log

v1 (2026-09-03) — first draft. Perimeter agreed with the architect: the seven
High findings plus the Medium findings that share a file or a mechanism with
them (M-1, M-2, M-3, M-5, M-6, M-7, M-8, M-13, M-18, M-20). Four approach
choices were taken before writing (§3): Redis fixed-window counters for
lockout; a "fresh proof only when a factor already exists" gate on enrolment;
session termination on every system-role change; the provider-side OAuth
collection as the single source of truth. Two small items outside the agreed
list are included because they live in the exact lines being edited and are
flagged as such: the client-user PATCH role guard (M-17, §4.6 D29) and the
degraded-lookup fail-open in the OAuth callback (L-30, §4.8 D33). The
architect can strike either.

v1.1 (2026-09-03) answers review round 1, finding 1: v1's enrolment gate let
a session with no factor add one with no proof, which contradicted goal 2
("a stolen session-only bearer cannot add … a second factor") and left an
account-lockout attack open. D11 now demands a fresh proof on **every**
enrolment: with a factor, the step-up proof; without one, a recent
interactive login carried by a new `auth_time` claim (or a fresh password
reconfirm where D19 still issues one), answered otherwise with a new 401
`reauthentication_required` that both SPAs turn into a re-login with return.
§3.2 records why freshness was preferred to a one-shot capability, D12/D14/D17
follow the rename and the new claim, edge cases 8–9 and §6 are updated.

v1.2 (2026-09-03) answers review round 1, finding 2: v1.1's D16 kept the
caller's session alive after a self-removal and let its MFA markers live
until the next refresh, so the token could still pass `RequireMFA` for the
access-token lifetime and `RequireStepUp` for five minutes — contradicting
goal 3, and leaving the admin reset dependent on a revocation write that
D16 itself allowed to fail. D16 now adds an **MFA epoch**: a per-user
counter bumped on every credential removal or replacement, carried in the
token (`mfae`) and compared on every request that consumes an MFA marker, so
authority minted under a removed factor dies immediately in every session
(current one included) without logging the user out. D17's refresh check,
D18's predicate, D11's factor branch and D13's replacement path use it;
edge cases 10 and 12, §6 and the ADR row follow.

v1.3 (2026-09-03) answers review round 1, finding 3: v1's counter relied on
`OAuthStateStore.Incr`, which issues `INCR` and `EXPIRE` as two commands — a
failure between them leaves a counter with no TTL, i.e. a permanent lockout
— and its interface absorbed store errors into `(0, false)`, which made D4's
"fall back to the durable rule when the store is unavailable" impossible to
express. D1 now runs one Lua script (`INCR`/`GET` + `PTTL` + `PEXPIRE`,
healing any key found without a TTL, peek included), returns a `Verdict` plus
an explicit `error`, requires `EVAL` at boot as GETDEL is required today, and
moves the MFA per-challenge cap onto the same script. D3 and D4 name the
error explicitly; §6 adds the atomicity, orphan-healing and outage tests.

v1.4 (2026-09-03) answers review round 1, finding 4: v1's D32 declared the
provider collection authoritative yet kept `CreateOAuthProvider` best-effort,
so a session could be minted for an identity the store never recorded as the
caller's — and a duplicate-key hit, which under the new unique index means
"someone else owns this", would have been logged and ignored. D32 item 5 now
writes ownership first on every link path and mints nothing without it:
duplicate key → re-read, same owner proceeds, other owner is refused with
`oauth_identity_conflict`; outage → `oauth_store_unavailable`. Signup
reserves the identity with the future user's UUID, then claims the sentinel,
then creates the user, and compensates backwards on failure; a new item 8
heals an orphan reservation on the next callback. D30, edge cases 27–28 and
§6 follow.

v1.5 (2026-09-03) answers review round 1, finding 5: v1's migration kept the
earliest-linked row among duplicates of one `(provider, providerId)` — an
automatic answer to a question that has none, since the existing
`(userUuid, provider)` index makes every such duplicate a claim by two
different users. D32 item 1 now makes the migration report and refuse,
accepts only an explicit per-identity resolution map, and creates the index
only at zero conflicts; as a flagged addition, the auth module checks the
index at `Start` and degrades OAuth to `oauth_store_unavailable` when it is
missing, so a deploy that skipped the migration cannot run the
ownership-first flow without its constraint. Edge case 23, §6 and §7 follow.

v1.6 (2026-09-03) answers review round 1, important finding 1: goal 6
promised a role change "on the next request" while D26/D27 made cache
invalidation and session termination best-effort with a 60 s fallback. The
authz cache becomes generation-keyed (per-user and global counters folded
into the key, invalidation = one atomic `INCR`, no `KEYS` scan — closing
L-11), and every role or binding mutation runs pre-invalidate → write →
post-invalidate: a pre-invalidation Redis cannot perform refuses the change
with 503, the post step retires any entry repopulated during the write.
Goal 6 is restated to exactly that, with the sub-second residual named in
D27 and edge cases 29–30; §6 adds the generation and ordering tests.

v1.7 (2026-09-03) answers review round 1, important finding 2: v1's mobile
nonce travelled with the ID token, so whoever exfiltrated the token had the
nonce too and the replay counter only decided who presented first. D35 is
now a two-step native flow: the backend issues and stores the nonce at
`begin` against a client-committed PKCE challenge, takes the record
atomically at completion and checks the client's verifier, so a token
stolen on its own is worthless and a replay finds no record; the residual —
an attacker inside the completion channel — is written down as the same
boundary the web cookie binding has. Goal 10, edge cases 25–26 and §6
follow; the v1 replay counter is dropped.

v1.8 (2026-09-03) answers review round 1, important finding 3: D2 gave the
IP scope the account threshold, which would have locked a whole NAT or VPN
for the lockout window after five wrong passwords among its users — worse
than today's 1 token/s bucket. The IP scope gets its own admin-managed pair
(`ipLockoutThreshold` default 100, `ipLockoutDuration` default 15m) with a
write-time rule that keeps it at or above the account threshold; the
per-address request caps rise to 60 per window; edge case 31 and §6 follow.

v1.9 (2026-09-03) answers review round 1, important finding 4: D31 leaned on
a `UserProvider` listing method that does not exist (the interface exposes
only `GetUserCount`). The backfill now has its own additive seam,
`iface.SystemRoleHolderFinder` (one deterministic query: oldest non-deleted
holder of a role), with a `GetUserCount`-based placeholder claim when a
provider lacks it; §6 and the ADR row follow.

v1.10 (2026-09-03) answers review round 1, important finding 5: D16 revoked
the other sessions on a passkey removal only when it was the last factor. A
removed credential is one the user no longer trusts and may have created
sessions; since no session records which credential minted it, the rule is
now uniform for every credential removal or replacement — epoch bump,
device-trust revocation, every other session ended — and D13's TOTP
replacement follows it; §6 adds the cases.

v1.11 (2026-09-03) answers review round 1, important finding 6: a rollback
to today's binary after the first tombstone would ignore `unlinkedAt` and
re-enable every unlinked identity. PR D is split expand/contract: D1 makes
every reader tombstone-aware and ships everything else in §4.7–§4.10
without writing a tombstone; D2 enables the tombstone-writing unlink and
sets the hard rollback floor at D1. §7 gains a rollback-floor table for
every data-shape change in the spec; D32 item 2 names the split.

v1.12 (2026-09-03) answers review round 1, important finding 7: D5's "a
semaphore of 16 concurrent sends" did not say where it was acquired, and an
acquire inside the goroutine would have let goroutines pile up under a
flood. D5 now specifies a bounded dispatcher — a 256-job queue drained by 16
workers started and stopped with the module, non-blocking enqueue that
drops with a metric when the queue is full — and §6 tests the bound
directly (concurrency, queue capacity, goroutine count, enqueue latency,
drain on stop). D13 and edge case 7 follow.

v1.13 (2026-09-04) amends D4 after PR A shipped. As written, D4 reached the
durable `FailedLoginCount+1 >= threshold` rule only when the counter store
errored, which made the fixed-window counter the sole decider whenever Redis
was healthy — and a fixed window has no cumulative bound, so an attacker
pacing `threshold-1` guesses per window was capped by neither mechanism. The
pre-remediation code did cap that case, so this was a regression introduced
by the move to counters and caught by the whole-branch review, not by any
single task's diff. D4 now ORs the two rules and evaluates the durable one on
every failure; the expired-lock clear in D3 is what makes an unconditional
cumulative check safe. The amendment is recorded here rather than silently:
the shipped code is the OR, and D4 as published described something else.
D4 also now carries the M-7 residual, which the spec had nowhere else: D3
claims the counter closes the 429-versus-401 oracle, which holds only while
the counter is the thing answering. Closing the residual changes D9's wire
contract, so it stays an open decision.

## 1. Problem

The audit found the authentication and authorization stack sound in its
foundations (RS256 with `iss`/`aud`/`type` checks, permissions never in the
token, atomic refresh rotation with family replay fences, argon2id, one
`RealIP` chokepoint, step-up on destructive routes) and seven defects that
undo parts of it. Each is confirmed against the tree at `a242e6bd`:

| ID | Defect | Where |
|---|---|---|
| H-1 | `RateLimiter.Check` / `IsLockedOut` / `Middleware` read `rl.configs` without a lock while `SetAuthFailedConfig` writes it on **every** login and service-account grant. Reproduced with `go test -race`. A collision is a fatal runtime error, not a recoverable panic | `shared/errors/rate_limiter.go:119,122,225,229,349,358,359` vs `:190-192`; callers `password_auth_service.go:524`, `service_account_service.go:729` |
| H-2 | TOTP enrolment is mounted under `RequireGlobal()` only and `ConfirmEnrollment` deletes an existing factor after validating a code for the *new* secret | `auth/module.go:1615-1619, 1742-1746`; `mfa_service.go:224-229` |
| H-3 | Passkey registration is mounted under `RequireGlobal()` only | `auth/module.go:1693-1697, 1755-1759`; `webauthn_handler.go:554-570` |
| H-4 | `UpdateRole` / `CreateRole` take no actor and replace `permissions` verbatim; no catalog check, no cascade | `authz/services/service.go:840-854, 891-896` |
| H-5 | Every `tenant_roles.*` Cedar permit fires on `system.*` actions for internal tenants; `shadowEvaluate` stamps JWT tenant roles even on global (`tenantID == ""`) checks; under `CEDAR_ENFORCE_ACTIONS` an `org_owner` gets `system.users.admin` | `cedar/policies/tenant_roles.cedar`; `service.go:396`; `engine.go:277` |
| H-6 | OAuth signup calls `ClaimFirstAdmin` with no audience guard; the password path has one. The claimer is wired into the client bundle. No boot-time seeding of the sentinel exists, so upgraded installs have it unclaimed | `auth_service.go:2178-2185` vs `password_auth_service.go:234,307`; `tier_bundle.go:138,166` |
| H-7 | Login resolves identity from `*_oauth_providers`; signup and auto-link write only that collection; unlink `$pull`s only `user.oauthLinks`. An unlinked identity keeps signing in; a login-created link is invisible to auth-methods and cannot be unlinked | `auth_service.go:2111, 2370, 676, 763`; `user_service.go:704-712` |

The coupled Mediums: M-1 (`reauth` satisfies `RequireMFA` and password-confirm
ignores `RoleRequiresMFA`), M-2 (MFA reset and self-removal leave sessions
MFA-satisfied; refresh copies `amr` forward; passkeys survive a reset), M-3
(no attempt cap on `/mfa/verify`), M-5 (forgot-password: no throttle,
synchronous send, token invalidated per call), M-6 (`IsBlocked` consumes a
token, so `verify-email/resend` lets an anonymous caller keep any email in
429), M-7 (window ignored, durable lock only for existing accounts, counter
never resets after lock expiry), M-8 (change-password and password-confirm
bypass lockout), M-13 (role change: no cache flush, no session termination,
JWT `srole` trusted by tier guards), M-18 (PKCE never sent), M-20 (mobile ID
tokens: no nonce, no `iss`, `aud` skipped when config read fails).

## 2. Goals and non-goals

**Goals**

1. No anonymous request can crash the process (H-1).
2. A stolen session-only bearer cannot add, replace or remove a second factor,
   and cannot mint an MFA-satisfied token by any route (H-2, H-3, M-1, M-3).
3. Removing or resetting a factor ends what that factor authorised (M-2).
4. A custom role can never carry more than its editor holds, nor any
   platform permission (H-4).
5. `system.*` actions require a platform system role under both evaluators,
   shadow and enforce (H-5).
6. A system-role change — and every role or binding mutation — is honoured
   by the next authorization decision in every session the user holds; a
   change whose effect cannot be guaranteed is refused, not applied (M-13).
7. The global `super_admin` seat can be claimed only by the operator tier,
   and never on an install that already has one (H-6).
8. Unlinking an OAuth identity refuses that identity at the next callback;
   every linked identity is visible and unlinkable (H-7).
9. Lockout means what the admin UI says: N failures per window, per IP and
   per email, across replicas, without an existence oracle (M-5..M-8).
10. PKCE on every provider that accepts it; mobile ID tokens are bound to
    a server-issued challenge and a client-held verifier, single-use and
    issuer-checked (M-18, M-20).

**Non-goals**

- Flipping `CEDAR_ENFORCE_ACTIONS` on. This spec makes it safe to flip; §7
  keeps the flip a separate operational step.
- A queue or worker for outbound mail. §4.1 D5 detaches the send with a
  bounded goroutine; a real queue is a notification-module concern.
- Admin "unlock account" UI, CAPTCHA, progressive delays.
- The remaining audit findings (M-4 invites, M-9 refresh `jti`, M-10..M-12,
  M-14..M-17 except the one guard in D29, M-19, every Low/Info). Listed in §8.
- Changing `mfaEnabled`'s default or the grace-window model.

## 3. Alternatives considered

Four choices were put to the architect before writing; the chosen option is
first in each block.

### 3.1 Lockout mechanism (H-1, M-5..M-8)

- **Redis fixed-window counters (chosen).** `INCR` + `EXPIRE` on the seam
  the MFA attempt cap already uses. Honours threshold *and* window, is shared
  across replicas, survives restarts, and lets pre-checks peek. The in-memory
  bucket keeps only the per-IP `api:general` role.
- Minimal in-memory fix: lock the map, honour the window in the bucket,
  peek in pre-checks. Stays per-process; thresholds multiply by replica count
  and reset on every deploy.
- Race-only fix: leaves M-5..M-8 open.

### 3.2 Enrolment gate (H-2, H-3)

- **A fresh proof on every enrolment, in two shapes (chosen, v1.1).** A
  dedicated gate that consults the MFA-enrolment lookup: with a factor, the
  same proof `RequireStepUp` demands; without one, a recent interactive login
  (`auth_time` within five minutes) or a fresh password reconfirm where the
  policy still issues one. Every enrolment is also announced (security event
  + email). v1 let a no-factor session enrol with no proof at all; review
  round 1 showed that contradicts goal 2 and enables a takeover-by-lockout.
- A one-shot "enrolment capability" minted at login and consumed by the
  first `enroll/begin`: the same guarantee (the proof is bound to a recent
  interactive authentication a stolen bearer never had) but with Redis
  state, a consume step and a new endpoint; `auth_time` + max-age is the
  OIDC-standard shape of exactly this check and costs one claim.
- `RequireStepUp(5m)` unconditionally: deadlocks an MFA-obligated user in
  their grace window — `dispatchStepUpFailure` answers
  `mfa_enrollment_required` on the very route they need to enrol with.
- Require the current TOTP code inside `ConfirmEnrollment`: covers only TOTP
  replacement, not passkeys, and needs a new SPA field.

### 3.3 Session handling on system-role change (M-13)

- **Terminate every session on any change (chosen).** One invariant: a role
  change closes the sessions minted under the old role. Promotions cost one
  re-login and show the new role immediately.
- Terminate on demotion only: two code paths, and a promoted user keeps a
  stale `srole` until refresh.
- Cache flush only: `srole` stays stale for the access-token lifetime and
  `canAssignRole` keeps trusting the claim.

### 3.4 OAuth identity store (H-7)

- **Provider-side collection authoritative, `user.oauthLinks` a derived
  read-model (chosen).** Login already keys on it; listing and unlink move to
  it; the embedded slice is written symmetrically and repaired lazily. No
  user-module DTO changes.
- Drop `user.oauthLinks`: touches user DTOs, DSR export, both SPAs.
- Dual-write without repair: history stays inconsistent.

## 4. Design

Decisions are numbered D1… so review rounds can cite them.

### 4.1 Attempt counters and lockout (H-1, M-5, M-6, M-7, M-8)

**D1 — One counter service, fixed windows, one atomic Redis script.**
New file `auth/services/attempt_counter.go`:

```go
// Limit is "at most Threshold events per Window".
type Limit struct {
    Threshold int
    Window    time.Duration
}

// Verdict is what the store said about a key. Zero when err != nil.
type Verdict struct {
    Count      int64
    Locked     bool          // Count >= Threshold
    RetryAfter time.Duration // remaining life of the window, from PTTL
}

type AttemptCounter interface {
    // Locked peeks. It never increments. A non-nil error means the store
    // could not answer; the caller decides what that means (D3, D4).
    Locked(ctx context.Context, key string, limit Limit) (Verdict, error)
    // RecordFailure increments the key and reports the resulting state.
    RecordFailure(ctx context.Context, key string, limit Limit) (Verdict, error)
    // Reset deletes the key (a successful login clears the email scope).
    Reset(ctx context.Context, key string) error
}
```

Both reads run one Lua script (`EVAL`, one round trip), so the count, the
TTL and the healing of an orphan are a single atomic step:

```lua
-- KEYS[1] counter key; ARGV[1] window in ms; ARGV[2] "1" to increment, "0" to peek
local n
if ARGV[2] == "1" then
  n = redis.call('INCR', KEYS[1])
else
  n = tonumber(redis.call('GET', KEYS[1]) or '0')
end
if n == 0 then return {0, -2} end
local ttl = redis.call('PTTL', KEYS[1])
if ttl < 0 then            -- fresh key (INCR just created it) or an orphan with no expiry
  redis.call('PEXPIRE', KEYS[1], ARGV[1])
  ttl = tonumber(ARGV[1])
end
return {n, ttl}
```

The window starts at the first failure and ends `Window` later — fixed, not
sliding; the threshold is exact within it. A key that exists without a TTL
(`PTTL == -1`) gets one on the next touch, peek included, so a lockout can
never outlive its window. Today's `RedisOAuthStateStore.Incr`
(`oauth_state_service.go:343-354`) sends `INCR` and `EXPIRE` as two commands,
and a crash or error between them leaves exactly such a key — for a lockout
counter that would be a permanent 429 until someone ran `DEL`. `RetryAfter`
comes from the same script's `PTTL`, so no optional TTL interface and no
adapter change are needed.

The script needs `EVAL`; `database.RedisClientAdapter` already exposes it
(`redis.go:166`, consumed through `services.LeaseRedisClient` by the
maintenance lease). The counter takes a
`ScriptRedisClient interface { RedisClient; Eval(ctx context.Context, script string, keys []string, args ...any) (any, error) }`
and the auth module refuses to boot without it — the same stance it takes
for GETDEL (`auth/module.go:897-899`). `OAuthStateStore.Incr`, which backs
the MFA per-challenge cap (`mfa_challenge_service.go:250-273`), switches to
the same script so `IncrementAttempts` gains the atomic TTL as well; the
in-memory store the tests use is atomic by construction.

**Errors are returned, never absorbed.** The implementation records
`orkestra_auth_attempt_store_failures_total{operation}` and a throttled WARN
(`sessionRevocationWarningInterval` pattern,
`session_revocation_service.go:172-181`) on every error, then returns it.
What an unavailable store *means* is decided by each caller and is the same
everywhere: **fail open for the counters, fall back to the durable rule** —
a `Locked` error is treated as not locked; a `RecordFailure` error yields no
verdict and D4 switches to the `FailedLoginCount` rule for that attempt. A
fail-closed counter would turn a Redis outage into a platform-wide login
outage, which the session-revocation design already rejected (ADR-0017 D5
rationale, `auth/CLAUDE.md:1050`); the durable per-account lock is the
second line, and the explicit error is what lets D4 know when to be it.

**D2 — Keys, scopes and limits.** Keys are `auth:attempts:<scope>:<id>`.
Email is lower-cased and trimmed exactly as `Login` does (`password_auth_service.go:476`);
an empty IP **skips** the IP scope instead of sharing the `ip:` key every
caller with no resolvable address shares today (`password_handler.go:398-403`).

| Scope | Key | Limit | Who records |
|---|---|---|---|
| `ip` | `ip:<ip>` | policy `ipLockoutThreshold` / `ipLockoutDuration` (new keys, defaults **100** / 15m) | login, change-password, password-confirm, service-account grant |
| `email` | `email:<audience>:<email>` | policy `accountLockoutThreshold` / `accountLockoutDuration` (defaults 5 / 15m) | login, change-password, password-confirm |
| `client` | `client:<clientId>` | the account pair (a client ID is an account) | service-account grant |
| `reset-email` | `reset-email:<audience>:<email>` | 3 / 15m (`ResetRequestsPerEmail`) | forgot-password (every accepted request) |
| `reset-ip` | `reset-ip:<ip>` | 60 / 15m (`ResetRequestsPerIP`) | forgot-password |
| `verify-email` / `verify-ip` | as above | 3 / 15m and 60 / 15m (`VerifyRequestsPerEmail`, `VerifyRequestsPerIP`) | verify-email/resend |
| `mfa-verify` | `mfa-verify:<audience>:<userUUID>` | `MFAMaxAttempts` (5) / `MFAChallengeTTL` (5m) | authenticated `/mfa/verify`, `/mfa/webauthn/verify/finish` (§4.3 D20) |

The account pair is read per call through the existing accessors
(`auth_policy_service.go:289-304, 400-414`); the `SetAuthFailedConfig`
round-trip on every attempt disappears with the method.

**The IP scope has its own, much higher, threshold** (v1.8). An egress
address is not an account: a corporate NAT or VPN carries hundreds of people,
and five wrong passwords among them must not lock the office out for
fifteen minutes — which is exactly what sharing the account pair would do,
and worse than today's bucket, whose 1 token/s refill never held an IP for
more than seconds. Two admin-managed keys join the `login` group next to the
account pair (`auth/module.go:421-429`): `ipLockoutThreshold` (`FieldInt`,
default `100`, "Failed attempts from one source address before that address
is locked — one office egress can be hundreds of people") and
`ipLockoutDuration` (`FieldDuration`, default `15m`). Read through new
`IPLockoutThreshold`/`IPLockoutDuration` accessors with the same shape as
the account ones (nil-tolerant, default on empty or malformed). A third rule
joins `ValidateConfigSnapshot` (`config_validation.go`): `ipLockoutThreshold`
must be ≥ `accountLockoutThreshold`, refused with 422
`auth.ip_threshold_below_account` — an IP lock tighter than the account lock
turns the shared address into the account's oracle. `config_groups_test.go`'s
per-group field count and `cmd/server/config_declarations_test.go` are
updated deliberately (the contract asks for that, `auth/CLAUDE.md:161-168`).
The per-address request caps (`reset-ip`, `verify-ip`) are raised to 60 per
window for the same reason; they stay named constants in
`attempt_counter.go`, not admin config — a request cap has no legitimate
reason to be tuned per install today, and adding schema keys is a separate
decision. The per-address scopes are shared across audiences and with the
service-account grant, as the single `ip:` bucket is today.

**D3 — Login order and answers.** `PasswordAuthService.Login`
(`password_auth_service.go:475-666`) becomes:

1. Policy gates (login enabled, password method, geo) — unchanged.
2. **Peek** `ip` and `email` (D1). Locked → run the dummy argon2 verify,
   emit `auth.login.failed` with `reason=rate_limited`, return
   `ErrAccountLocked`. Nothing is recorded. A store error reads as not
   locked.
3. `GetUserForAuth`. Unknown → dummy verify, `RecordFailure(ip, email)`,
   `ErrInvalidCredentials` (as today).
4. Inactive, service principal, no password hash → **dummy verify** (new for
   the first two — today `:581-592` return without it), `RecordFailure`,
   `ErrInvalidCredentials`.
5. Durable lock. `LockedUntil` in the future → dummy verify (new), audit
   `reason=account_locked`, `ErrAccountLocked`; not recorded. `LockedUntil`
   in the past → `ClearFailedLogins` **before** verifying, closing the
   "one wrong password after expiry re-locks" defect (`:605-622` compares
   against a counter nothing ever resets, `user_repository.go:1055-1073`).
6. Password verify. Failure → `RecordFailure(ip)` and `v, err :=
   RecordFailure(email)`, then the durable write of D4 with
   `counterAvailable := err == nil`, audit `bad_password`,
   `ErrInvalidCredentials`.
7. Success → `ClearFailedLogins` (as today) **and** `Reset(email)`. The IP
   scope is not reset: one correct login must not launder a stuffing run.

`ErrAccountLocked` maps to **429 `auth.too_many_attempts`** with a
`Retry-After` header (D9) on every route that can raise it. Both an unknown
and a known email lock at the same attempt under the same window and answer
the same 429, and every non-success branch pays the argon2 cost, which closes
the 429-versus-401 oracle and the timing oracle (M-7, L-1).

**D4 — The durable lock ORs with the counter (amended in v1.13).**
`User.LockedUntil` is set when `RecordFailure(email)` returned
`Verdict.Locked` **or** when `FailedLoginCount+1 >= threshold` — the two are
ORed and the durable rule is evaluated on **every** failure, not only when
the store was unavailable.

`FailedLoginCount` is therefore load-bearing, not merely operator
visibility: it is the **cumulative** cap. The counter is a *fixed* window
stamped on the first failure of that window, so making it the sole decider
while Redis is healthy would leave an attacker pacing `threshold-1` guesses
per window bounded by nothing — the guarantee the pre-remediation code had,
lost as a side effect of moving login onto the counter. ORed, the counter
bounds bursts and the durable count bounds the long run.

This is safe in a way the pre-remediation rule was not, because the
expired-lock clear (D3) zeroes `FailedLoginCount` when a lock lapses: an
account gets a fresh budget after each lockout instead of re-locking on
every later failure for life.

So the two mechanisms lock at the same attempt for a *burst*; a slow run
trips the durable rule alone; and with Redis down the durable rule still
caps guessing against existing accounts.

Consequence for M-7, recorded here because the spec has no other place for
it: D3 above says the counter closes the 429-versus-401 oracle, and that is
true only while the counter is the thing answering. Whenever a durable lock
outlives its counter window — Redis down, the fixed window expired under a
live lock, or now a paced attacker who never fills a single window — a known
email answers 429 where an unknown one answers 401. The residual is
described in full in `backend/internal/core/auth/CLAUDE.md` and at the
durable-lock branch itself; closing it changes D9's wire contract, so it is
an open decision, not a defect. Admin
visibility and the setup of an admin "unlock" affordance stay as they are
(§8).

**D5 — Forgot-password and resend-verification: request caps and detached
send.** `ForgotPassword` (`:1016-1091`) and `ResendVerification` (`:982-1006`):

1. Peek `reset-ip`/`reset-email` (resp. `verify-*`). Over the cap → return
   the generic success with no side effect.
2. `RecordFailure` on both request scopes (the scope name says "request";
   the counter is the same type). Recorded **before** the user lookup so the
   cost is identical for known and unknown addresses.
3. Lookup, token issue (previous token still invalidated — inside the cap an
   attacker can force at most three re-issues per address per window).
4. The email is handed to a **bounded dispatcher** and the handler answers
   immediately (v1.12 makes the bound explicit). `auth/services/mail_dispatcher.go`:
   - a buffered channel of `MailQueueCapacity = 256` jobs and
     `MailWorkers = 16` worker goroutines, started by the auth module's
     `Start` and stopped by `Stop` (drain what is queued for up to 10 s,
     then abandon; each job runs under `context.WithoutCancel` with a 60 s
     deadline and a `recover`);
   - **enqueue is non-blocking** (`select` with `default`): a full queue
     drops the job, logs WARN with the template id and the request id
     (never the address) and increments
     `orkestra_auth_mail_dropped_total{template}`. Nothing waits and no
     goroutine is created per request, so memory is bounded by the queue,
     concurrency by the workers, and the handler's latency by neither —
     which also keeps the response time independent of whether an account
     existed (a blocking acquire would have reintroduced the oracle for
     known addresses);
   - a drop is a lost mail, recoverable by the user's next request inside
     the caps of D2; the metric is the alerting signal.
   Delivery failures are already swallowed today (`:1086-1089`); they keep
   going to the log and to the notification log row.

`ResendVerification` stops calling `IsBlocked`/`recordFailed` on the login
scopes (M-6): a verification request is not a login failure and must never
be able to lock a login.

**D6 — Change-password and password-confirm join the lockout.**
`ChangePasswordInput` (`:1145-1150`) gains `IP string`; the handler fills it
from `clientIPFromCtx`. Both `ChangePassword` (`:1154-1214`) and
`ConfirmPasswordWithSecurity` (`:1251-1333`, IP from `security.IPAddress`):

- peek durable `LockedUntil` and the `ip`/`email` counters first; locked →
  `ErrAccountLocked` (429, D9), after a dummy verify;
- wrong password → `RecordFailure(ip, email)` and the durable write of D4,
  plus an audit row `auth.password.change_failed` /
  `auth.password.reconfirm_failed` (today `:1163-1166` records nothing);
- success → `Reset(email)`.

`RequireStepUp` is **not** added to `/change-password` (the audit's optional
suggestion): the route already verifies the current password, and the SPA
treats it as a plain form.

**D7 — Service accounts use the same counters.** `service_account_service.go`
replaces the limiter (`:722-734, 783-793`) with peeks on `ip`/`client` and
`RecordFailure` on the same failure branches; `ErrClientRateLimited` maps to
429 `auth.too_many_attempts` + `Retry-After`. `TestGrantSuccessiveSuccessesNotRateLimited`
stays green by construction (peeks never consume).

**D8 — The in-memory `RateLimiter` shrinks to what it serves.**
`shared/errors/rate_limiter.go` keeps `Check`, `Middleware` and the
`api:general` config; `SetAuthFailedConfig`, `IsBlocked`, `IsLockedOut`,
`RecordFailedAuth`, `CheckMultiple`, the unmounted `AuthMiddleware` and the
four dead configs (`auth:login`, `auth:refresh`, `security:sensitive`,
`global:ip`) are deleted. Every remaining read of `rl.configs` takes
`rl.mu.RLock()`; `Check` reads `bucket.tokens` under `tb.mu` (the second race
the fact-finding surfaced at `:129`). The auth module no longer constructs a
`RateLimiter` (`auth/module.go:956`); `PasswordAuthConfig.RateLimiter` and
`tierBundleDeps.rateLimiter` become `AttemptCounter`. `errors_test.go:122-148`
(`TestSetAuthFailedConfig`) and `rate_limiter_test.go:11-107` are replaced by
counter tests (§6).

**D9 — Wire shape of a lockout.** New constant
`errcode.AuthTooManyAttempts = "auth.too_many_attempts"` (429). `errcode.Error`
gains an optional `Headers http.Header` (`json:"-"`), a `WithHeader(k, v)`
builder and a `GetHeaders()` method, which makes it satisfy
`huma.HeadersError` (`huma/v2@v2.39.1/error.go:153-177`) so Huma copies the
header onto the response. `Retry-After` is the integer seconds of
`retryAfter` from D1 (or of `LockedUntil - now` for the durable branch),
never below 1. The golden test `codes_test.go` gains the constant; the
`error_mapping_test.go:50` row changes from an empty slug to the code.

**D10 — Telemetry.** `orkestra_auth_lockouts_total{scope}` on every
`RecordFailure` that reports `locked`; the store-failure counter of D1.

### 4.2 Enrolment and passkey registration gate (H-2, H-3)

**D11 — A dedicated gate: `RequireEnrolmentProof(maxAge)`.** New method on
`AuthMiddleware` (`shared/middleware/auth.go`), mirroring `RequireStepUp`'s
shape. It answers one question — *has the account holder proven presence
within `maxAge`?* — and accepts two shapes of proof:

- **a fresh second factor**: `amrSatisfiesStepUp(claims.AMR) && LastOTPAt
  within maxAge`, exactly `RequireStepUp`'s predicate (D18), with the MFA
  markers read through D16's epoch resolver. This includes a fresh `reauth`
  from password-confirm for the users that endpoint still serves
  (non-MFA-obligated password users, D19);
- **a fresh primary authentication**: a new JWT claim `auth_time` (the OIDC
  name; `JWTClaims.AuthTime int64`, wire key `auth_time`), the Unix time of
  the interactive authentication that created the session, within `maxAge`.

Decision table:

1. No claims → 401 `authentication required` (as every gate).
2. `hasFactor, err := m.mfaEnrollment(ctx, claims.Audience, claims.UserUUID)`.
   Lookup unwired or `err != nil` → **fail closed**: `sendStepUpRequired`.
   (A degraded Mongo must not let a factor be added without proof.)
3. `hasFactor` → a fresh second factor → pass; else `sendStepUpRequired`.
   Never `password_confirm_required`, never `mfa_enrollment_required`: a
   user with a factor has exactly one right answer.
4. `!hasFactor` → a fresh second factor **or** a fresh `auth_time` → pass;
   else a new envelope, **401 `reauthentication_required`**
   (`WWW-Authenticate: Bearer error="reauthentication_required"`, body
   `maxAgeSeconds` and `authTime`, written by `writeCodedError` and pinned by
   the golden test). Not `password_confirm_required`: the users most in need
   of a first enrolment — MFA-obligated accounts in their grace window — are
   the ones password-confirm refuses (D19), and OAuth-only accounts have no
   password to reconfirm. A re-login is the one answer that works for
   everyone, and D14 makes both SPAs return the user to where they were.

`auth_time` is stamped by every path that creates a session and by nothing
else: `issueTokensForSession` (password login, `password_auth_service.go:1575`),
MFA and passkey login completion (`IssueLoginTokensForSession`), the OAuth
callback's token pair (`auth_service.go:1356`, `:2377-2388`) and the
client-tier relay completion, the setup wizard's initial admin (`:423-430`),
the dev-token minter. Refresh carries it unchanged (D17); password-confirm
and step-up mints keep the session's value — they prove a factor, they do
not create a session. `claimsToMap` writes it when non-zero, `mapToClaims`
reads it (`jwt_service.go:613-732`); the sidecar `parseClaims` mirrors it.
A token minted before this ships has no `auth_time`, reads as stale, and a
first enrolment from it needs a re-login: safe by construction, and
invisible to users who already own a factor.

Residual window: a bearer stolen inside the `maxAge` minutes after the
victim's login can still enrol — the same window every freshness proof has,
including a stolen `reauth` token's — and D13's email and audit row make it
visible; the victim recovers through the admin reset (D15/D16). A bearer
stolen later cannot: the attacker has neither the password nor the identity
provider to re-authenticate with.

`MFAEnrollmentLookup` already reports true for a TOTP row or a WebAuthn row
with at least one credential (`auth/module.go:1391-1423`), so "has a factor"
covers both types.

**D12 — Mounting.** `mfa/enroll/begin`, `mfa/enroll/confirm`,
`mfa/webauthn/register/begin`, `mfa/webauthn/register/finish` move out of
`RegisterProtectedRoutes` into a new `RegisterEnrolmentRoutes` on both
handlers, mounted under `RequireGlobal()` + `RequireEnrolmentProof(5m)` on
both tiers (`auth/module.go:1615-1619, 1693-1697, 1742-1746, 1755-1759`).
`/me/mfa`, `/mfa/verify`, `/me/mfa/webauthn/credentials`,
`/mfa/webauthn/verify/*` stay where they are. Both halves of each ceremony
are gated because the factor set can change between `begin` and `confirm`.

**D13 — Every factor change is announced.** `ConfirmEnrollment` keeps
replacing an existing TOTP row (the gate now guarantees a fresh proof) but,
when it does, bumps the MFA epoch, revokes the caller's other sessions
(`RevokeAllUserSessionsExcept`, D16's uniform rule) and revokes device
trust with a new reason
`models.DeviceTrustRevokedOnMFAReplace = "mfa_factor_replaced"`. The
`SecurityEventSink` (`mfa_service.go:105-107`) receives:

| Event type | Compliance action (`authEventComplianceAction`, `auth_service.go:2034-2054`) | When |
|---|---|---|
| `self_mfa_enrolled` | `auth.mfa.enrolled` | first TOTP confirm |
| `self_mfa_factor_replaced` | `auth.mfa.replaced` | TOTP confirm with an existing row |
| `self_passkey_registered` | `auth.mfa.passkey_registered` | `FinishRegistration` |
| `self_passkey_removed` | `auth.mfa.passkey_removed` | `RemoveCredential` |
| `self_mfa_removed` | `auth.mfa.removed` | self `RemoveFactor` |

Today only backup-code regeneration emits anything (`mfa_service.go:395-407`).
The WebAuthn service gains the same `SetAuditSink` seam.

The user is also emailed on `self_mfa_enrolled`, `self_mfa_factor_replaced`
and `self_passkey_registered` through a new notification template
`auth.mfa_factor_added` (category `auth.security`), seeded and localised
exactly like `auth.new_device_login` (`notifyNewDeviceLogin`,
`password_auth_service.go:1754-1810`) and sent through D5's bounded
dispatcher. Data:
`FactorType` (`totp` | `passkey`), `Replaced` (bool), `RequestIP`, `At`.

**D14 — SPAs.** Operator console: `baseApi.ts:536-565` already routes
`step_up_required` to `StepUpModal` and replays the request, and the settings
page never offers enrolment to an enrolled user (`MfaSettings.tsx:57-79`).
It gains one branch: a 401 `reauthentication_required` clears the session
and navigates to `/login?next=<current path>` through the existing
`sanitizeReturnTo` (`utils/returnTo.ts:64-98`); after the login the user is
back on the security page with a fresh `auth_time` and retries. No modal:
the answer is the same for password and OAuth accounts. `MfaEnrollWizard`
and `WebAuthnEnrollDialog` let that code through to the interceptor before
their generic error copy.

Client SPA: `authedFetch.ts:23-33` has no step-up handling and stays
modal-free. `MfaEnrolPage.tsx` reads `GET /v1/auth/client/me/mfa` first and
renders an "already enrolled" state instead of the wizard; a 401
`reauthentication_required` becomes a redirect to
`/login?next=/account/security/mfa` through `sanitizeNext`
(`lib/safeNext.ts:82-111`); a 401 `step_up_required` (an enrolled client
user reaching `enroll/begin` by URL) renders the existing error copy. The
client tier keeps first-enrolment-only as its supported flow; replacing a
factor on the client tier goes through the operator's admin reset (§4.3)
until the client SPA grows a step-up modal (§8).

### 4.3 Reset, removal, `reauth` and the step-up attempt cap (M-1, M-2, M-3)

**D15 — Removal removes every factor.** `MFAService.RemoveFactor`
(`mfa_service.go:333-367`) deletes the TOTP row **and** the WebAuthn row;
`ErrMFANotEnrolled` only when neither exists. Device-trust revocation is
unchanged. `AdminReset` (`mfa_handler.go:485-522`) therefore no longer answers
404 for a passkey-only target, and `Status` keeps reporting the passkey count
the handler already adds (`mfa_handler.go:313-323`).

**D16 — Reset and removal end sessions *and* end MFA authority now.** Two
mechanisms, because they answer two different questions.

*The MFA epoch* (v1.2) answers "is the MFA proof in this token still backed
by a factor set the user holds?" on every request that consumes it, so
removing a factor takes effect on the caller's **current** token as well,
without waiting for a refresh or for a revocation write to succeed:

- `iface.User` gains `MFAEpoch int` (`bson:"mfaEpoch,omitempty" json:"-"`,
  zero for every existing document). A new additive sub-interface
  `iface.MFAEpochBumper { BumpMFAEpoch(ctx, userUUID string) (int, error) }`
  is implemented by the user service as a `$inc` and resolved by the auth
  module with `module.GetTyped` against the tier's user provider.
- Every access token carries the epoch it was minted under, claim `mfae`
  (`JWTClaims.MFAEpoch`), taken from the `iface.User` the minting path
  already holds; absent claim reads as 0.
- The epoch is bumped by every **removal or replacement** of a credential:
  `RemoveFactor` (self and admin), `RemoveCredential` (any passkey, not only
  the last), and `ConfirmEnrollment` when it replaces a TOTP row (D13).
  Additions do not bump it: authority proven by a factor that still exists
  stays valid.
- One per-request resolver in the middleware, `mfaAuthority(ctx)`, memoised
  in the request context, is the only reader of the markers: when
  `claims.AMR` carries `otp`/`webauthn`/`mfa`/`device_trust`, it loads the
  user through the already-wired `m.users` (`SetUserProvider`,
  `auth.go:194`) and honours the markers only if `claims.MFAEpoch ==
  user.MFAEpoch`; a lookup error is "not current" (fail closed). Tokens
  without MFA markers cost nothing. Its consumers are the five places that
  decide on MFA authority: `RequireMFA`, `RequireStepUp`,
  `RequireEnrolmentProof` (factor branch), the impersonation bypass, and
  `IsMFAEnrolled` (Cedar's `principal.mfa_enrolled`). A stale epoch answers
  exactly as a token with no markers would: `step_up_required` from the
  gates, `false` to Cedar.

*Session termination* answers "may the other bearers of this account keep
using it?". `MFAHandler` gains a per-tier `sessions iface.SessionTerminator`
dependency (the operator handler gets the operator `*authService`, the
client handler the client one — the same values the user module resolves,
`interfaces.go:947-951`).

- `AdminReset` → after `RemoveFactor` (which bumped the epoch),
  `TerminateAllSessionsByUUID(target)`. A failure is logged and reported in
  the audit row's metadata (`sessions_terminated: false`), and the reset
  still succeeds: the epoch has already ended the target's MFA authority in
  every session, so a failed termination leaves only ordinary session access
  behind — the same exposure as any degraded revocation.
- Self `Remove` (`/me/mfa/remove`) → `RevokeAllUserSessionsExcept(user, currentSid)`
  (`auth_service.go:1223-1253`). The caller's own session stays signed in but
  its MFA authority is gone with the next gated request; its markers are
  dropped from the token at the next refresh (D17).
- `DELETE /me/mfa/webauthn/credentials/{id}` → bumps the epoch **and**
  `RevokeAllUserSessionsExcept(user, currentSid)`, whether or not other
  factors remain (v1.10). A removed passkey is a credential the user no
  longer trusts — a lost or compromised device — and it may have *created*
  sessions through the passkey login flow; the epoch ends their MFA
  authority, only revocation ends the sessions, and neither the session
  document nor `amr` records which credential minted a session, so the rule
  cannot be narrower than "every other session".

One rule, then, for every credential removal or replacement — TOTP removal,
TOTP replacement (D13), any passkey removal, admin reset: bump the epoch,
revoke device trust, and end every session but the caller's own (all of
them on the admin path). What differs between the paths is only who the
caller is.

**D17 — Refresh recomputes the MFA markers.** `RefreshTokensWithRiskAssessment`
(`auth_service.go:1574`) and `MintAccessTokenFromRefresh` (`:1780`) stop copying
`claims.AMR` / `claims.LastOTPAt` verbatim. New helper
`carryAMR(prior []string, tokenEpoch, userEpoch int) (amr []string, lastOTPAt int64)`:

- `reauth` is always dropped (a password reconfirm is a five-minute proof,
  never a session property);
- `otp`, `webauthn`, `mfa`, `device_trust` are kept **only if**
  `tokenEpoch == userEpoch` (D16) — the user the refresh already loads
  (`auth_service.go:1530`) supplies `userEpoch`, so the check costs no extra
  read; the new token is minted under the current epoch;
- `LastOTPAt` is carried only when at least one MFA marker survives, else 0;
- the base markers (`pwd`, `oauth`) are kept;
- `auth_time` (D11) is carried unchanged: it describes the session's origin,
  not the token, and a refresh is not an authentication.

This keeps the intended "MFA is session-long" semantic for users who still
own a factor and closes the M-2 window for those who do not. The stale
comment at `jwt_service.go:491-492` ("refresh tokens don't carry amr") is
corrected: they do, and this is where they are recomputed.

**D18 — Two predicates, one boundary.** `amrSatisfiesMFA` (`auth.go:1308-1315`)
becomes strict — `otp | webauthn | mfa` — and, evaluated through D16's
`mfaAuthority(ctx)` so a stale epoch reads as no marker, is what `RequireMFA`,
`IsMFAEnrolled` (Cedar's `principal.mfa_enrolled`) and the impersonation
bypass use. A new `amrSatisfiesStepUp` adds `reauth` and is used by
`RequireStepUp` and D11's gate only. The sidecar `JWTValidator.RequireMFA`
already has the strict list (`jwt_validator.go:366-391`); the drift closes.

`RequireMFA` behaviour for a no-factor user is unchanged in practice: such a
user was already blocked unless they had obtained `reauth` elsewhere — the
route's intended answer is "enrol". The setup wizard still mints
`["pwd","reauth"]` for the initial admin (`password_auth_service.go:423-430`);
that token now satisfies `RequireStepUp` but not `RequireMFA`, which a fresh
install has switched off (`mfaEnabled` seeds `false`, `auth/module.go:573-574`).

**D19 — Password-confirm refuses MFA-obligated users.**
`ConfirmPasswordWithSecurity` adds, after the enrolled-factor refusal
(`:1281-1297`): `if s.policy.MFARequired(user, memberships) { return nil,
ErrPasswordConfirmEnrollmentRequired }`, with memberships resolved the way
`completeLogin` resolves them for its own MFA decision. The handler maps the
new sentinel to **403** with the middleware's `mfa_enrollment_required`
envelope shape (code, title, detail) so the SPA's per-page handling
(`SessionsTab.tsx:59-60`) sees one code for one situation.

**D20 — Attempt cap on authenticated verification.** `MFAHandler.Verify`
(`mfa_handler.go:405-447`) and `WebAuthnHandler.VerifyFinish`
(`webauthn_handler.go:282-312`) use the `mfa-verify` scope of D2: peek before
verifying (locked → 429 `auth.too_many_attempts`), `RecordFailure` on every
invalid code / assertion (a backup-code attempt is one failure regardless of
how many hashes it compares), `Reset` on success. The per-challenge counter
inside `FinishAssertion` stays as the inner bound.

### 4.4 Custom-role cascade and catalog validation (H-4)

**D21 — Roles are validated like bindings.** `CreateRole(ctx, tenantID,
actor string, input)` and `UpdateRole(ctx, tenantID, roleUUID, actor string,
input)` gain an actor; the handlers pass `ctxauth.GetUserUUID(ctx)` exactly
as `createBinding` does (`handler.go:298`). A shared
`validateCustomRolePermissions(ctx, tenantID, actor string, keys []string) ([]string, error)`
runs whenever `Permissions` is supplied:

1. trim and de-duplicate; empty → `ErrRolePermissionsRequired` (400);
2. every key must exist in `allPermissionSet` (populated by
   `RegisterPermissions`, `service.go:667-695`, and unused until now) →
   `ErrUnknownPermission` (422 `authz.permission_unknown`, detail names the
   key);
3. no key may be `"*"` or a member of `systemPermissionSet` →
   `ErrSystemPermissionInCustomRole` (422 `authz.system_permission_forbidden`);
4. `actor == ""` → `ErrGranterRequired` (400); `actor != granterSystem` →
   `validateBindingCascade(&models.Role{Permissions: keys},
   GetEffectivePermissions(ctx, actor, tenantID))` (`service.go:1268-1291`)
   → `ErrInsufficientPermissionsToGrant` (403, existing message).

`Name` is trimmed and must be non-empty (`ErrRoleNameRequired`, 400) — the
two `errors.New` paths that surface as 500 today (`handler.go:248-264`) are
replaced. `allPermissionSet` is complete because the registry calls
`RegisterPermissions` once with the union of every module's `Permissions()`
and `ensureSeeded` replays the cached specs; the spec relies on that and the
test in §6 asserts it. `DeleteRole` is unchanged (already scoped and
cascade-free by design: deleting a role removes access).

**D22 — Tenant-scoped bindings never contribute platform permissions.**
`GetEffectivePermissions` skips keys in `systemPermissionSet` when unioning
tenant-scoped bindings (`service.go:642-657`). This makes the documented
evaluator rule 4 (`authz/CLAUDE.md:204`) true instead of incidental, and
closes the audit's L-9 at zero cost. Global bindings are untouched.

### 4.5 Cedar: system actions require a platform role (H-5)

**D23 — A forbid that no permit can outrank.** New file
`cedar/policies/system_actions.cedar`:

```cedar
// system_actions.cedar — platform-reserved actions need a platform role.
//
// Every system.* permission is declared System: true and gated with
// RequireSystemPermission (tenantID == ""). Tenant-scoped role permits in
// tenant_roles.cedar are written for tenant resources and must never be
// the reason a system.* action is allowed: Cedar forbids win over permits,
// so this single rule closes every present and future tenant-role permit.
@id("system_actions.require_platform_role")
forbid (
    principal,
    action,
    resource
) when {
    context has action_module &&
    context.action_module == "system" &&
    !(principal has system_role &&
      (principal.system_role == "super_admin" ||
       principal.system_role == "administrator" ||
       principal.system_role == "developer"))
};
```

`developer` stays in the exempt set because the role table already bounds it
to read-only in production and `platform.developer.prod_readonly` mirrors
that; the forbid is about *who may hold* system actions, the permits about
*which*. `policycoverage` regex-scans the file and needs no baseline change
(`tools/policycoverage/scanner.go:300-319`).

**D24 — Global checks carry no tenant roles.** `shadowEvaluate`
(`service.go:396`) stamps `principal.tenant_roles` only when `tenantID != ""`.
`RequireSystemPermission` and the impersonation pre-check
(`auth.go:549, 967`) are the callers with an empty tenant; for them a
membership role in whatever tenant the request happened to resolve is not an
input to the decision.

**D25 — Not flipped here.** `CEDAR_ENFORCE_ACTIONS` stays unset by this spec.
§7 lists the flip as a separate operational step once §6's regression tests
are green on staging. The legacy `tenant_roles.administrator.all_in_tenant`
permit and the impersonation stamp that feeds it (M-14) are out of scope;
D23 already keeps that pair away from `system.*`.

### 4.6 System-role change propagation (M-13, plus M-17)

**D26 — One additive SDK seam.** `pkg/sdk/iface/interfaces.go` gains:

```go
// AuthzCacheInvalidator — consumed by: the user module after a system-role
// change. Narrow on purpose (the UserLifecycleStateProvider precedent):
// AuthzProvider is implemented by forks and stays additive-only. Resolve it
// with module.GetTyped against ServiceAuthzProvider; a missing value means
// the cached verdict expires on its own TTL (60 s) — degrade, do not fail.
type AuthzCacheInvalidator interface {
    InvalidateUserPermissions(ctx context.Context, userUUID string) error
}
```

`*authz/services.Service` implements it — not by exporting today's
`KEYS authz:cache:<user>:*` + `DEL` (`service.go:1232-1241`), which scans,
can partially fail and races a concurrent repopulation, but with a cache
that is **generation-keyed** (v1.6):

- two counters live in Redis, `authz:gen` (global) and `authz:gen:<userUUID>`
  (per user); every cache read fetches both with one `MGET` and folds them
  into the key: `authz:cache:<globalGen>:<userUUID>:<userGen>:<tenant|->`;
- `InvalidateUserPermissions` is `INCR authz:gen:<userUUID>`; the global
  `flushCache` (role update/delete, binding delete, tenant cascades) is
  `INCR authz:gen`. One atomic command each: no scan, no glob built from a
  request body (this closes the audit's L-11);
- an entry written under an older generation can never be read again and
  dies with its own 60 s TTL; a missing counter reads as 0;
- an `MGET` failure is a cache miss — the evaluator goes to Mongo, which is
  the fresh answer; an `INCR` failure is returned to the caller, who decides
  (D27).

`module.GetTyped[T]` asserts the
stored value (`iface.AuthzProvider(m.svc)`, `authz/module.go:228`) to `T`,
which succeeds because the dynamic type is `*Service`. `pkg/sdk/CLAUDE.md`
records the interface as additive; no versioning exception is needed.

**D27 — The role branch does what the deactivate branch does, in an order
that cannot leave a stale verdict.** `UpdateUser` (`user_handler.go:209-309`)
and the client-user PATCH (`admin_client_handler.go:242-267`) wrap a role
change as follows (v1.6):

1. **Pre-invalidate**: `InvalidateUserPermissions` (one `INCR`). Failure →
   the change is **refused** with 503 `user.role_change_unavailable` (new
   errcode) and nothing is written: a change whose effect cannot be
   guaranteed is not applied. An invalidator absent from the registry is
   treated the same way for role changes — the "degrade, do not fail" note
   on the interface applies to consumers that only *read* verdicts.
2. Write the role (`userService.UpdateUser`).
3. **Post-invalidate**: `INCR` again. This retires the only race left: a
   request that repopulated the cache between steps 1 and 2 wrote the *old*
   role under the *new* generation. A failure here is logged at ERROR,
   counted (`orkestra_authz_cache_invalidation_failures_total`) and recorded
   in the audit row (`cache_invalidated: false`); the change stands.
4. `terminateSessions` (best-effort, exactly as deactivation does today,
   `:62-75`), `sessions_terminated` in the audit row; the existing
   `user.role.changed` event carries both flags.

Residual: a stale verdict outlives the change, for at most 60 s, only when
Redis accepted step 1, served a repopulating read inside the milliseconds
before step 2, and then refused step 3 — a partial failure in a sub-second
window, not an operating mode. The same pre/write/post shape wraps every
authz mutation that changes effective permissions (`UpdateRole`,
`DeleteRole`, `CreateBinding`/`EnsureBinding`, `DeleteBinding`, the tenant
cascades) through one service-side helper, `withGeneration(ctx, scope,
mutate)`, so the cascade-checked role edits of D21 carry the same guarantee;
a refused pre-invalidation there is 503 `authz.cache_unavailable`. Redis
being unavailable already stops sessions, MFA challenges and OAuth state;
refusing a permission change in that state is consistent, and the retry is
the admin's.

**D28 — Tier guards read the database.** `canAssignRole`'s caller role
(`user_handler.go:121, 211`) comes from `h.userService.GetUser(ctx, actorUUID)`;
a lookup error is a 500 (`errcode.Internal`), never a fallback to the claim.
The setup finalization gate (`setup/service.go:363-369`) compares the role
returned by the `userLifecycleState` lookup it already performs, not the
`srole` parameter, which is removed from `evaluateAccess`/`Finalize`.
`GetSystemRole` keeps its remaining consumers (logging, navigation shaping,
the dev-token fallback) — none of them authorises.

**D29 — Client-user PATCH gains the role guards (M-17, addition).**
`UpdateClientUserAdmin` runs `canAssignRole` (with D28's DB role) and
`serviceAccountRoleAllowed`, emitting the same `user.update.refused` audit
row as the operator handler. The last-admin quorum stays operator-only (a
client user is never a platform administrator). Flagged as outside the agreed
perimeter; strike it if unwanted.

### 4.7 OAuth first-admin claim and sentinel backfill (H-6)

**D30 — The OAuth path mirrors the password path.** In
`HandleOAuthCallbackWithLinking`'s signup branch (`auth_service.go:2178-2185`):

```go
claimed := false
if s.audience != PolicyAudienceClient && s.firstAdminClaimer != nil {
    c, err := s.firstAdminClaimer.ClaimFirstAdmin(ctx, newUUID)
    if err != nil {
        return nil, fmt.Errorf("claim first admin: %w", err)   // fatal, as password Register
    }
    if c { claimed = true; role = "super_admin" }
}
```

A claim error is fatal, as at `password_auth_service.go:310`, because a
swallowed error is how a lost race becomes a silent `guest`. The claim moves
between the identity reservation and the user creation, and `Release` joins
the compensation D32 item 5 describes. `CreateUserFromOAuth`
additionally honours `input.UUID` (`user_service.go:694` ignores it today), so
the sentinel and the created user carry the same UUID on both paths.

**D31 — The sentinel is seeded on installs that already have an
administrator.** `iface.UserProvider` exposes `GetUserCount(ctx, filters)`
(`interfaces.go:37` of the interface body) but no listing, so the backfill
gets a dedicated, additive seam (v1.9), in the shape of
`UserLifecycleStateProvider` (`interfaces.go:118-139`):

```go
// SystemRoleHolderFinder — consumed by: the auth module's first-admin
// sentinel backfill (D31). Narrow on purpose: one deterministic answer,
// no paging, no DTO. Returns the UUID of the oldest non-deleted user
// holding role (by createdAt, then uuid), or found=false when none.
type SystemRoleHolderFinder interface {
    FindOldestUserWithRole(ctx context.Context, role string) (userUUID string, found bool, err error)
}
```

`*userService` implements it with one repository query — the existing
`buildFilter` (role, `deletedAt` excluded; `isActive` deliberately not
filtered: a deactivated super_admin still proves the install was
bootstrapped) sorted `createdAt asc, uuid asc`, limit 1 — so every replica
picks the same user. The auth module resolves it with
`module.GetTyped[iface.SystemRoleHolderFinder]` against
`ServiceOperatorUserProvider`.

In the auth module's `Start` (`maintenance.go`, operator bundle): when a
claimer is wired, `FindOldestUserWithRole(ctx, "super_admin")` answers
`found` → `ClaimFirstAdmin(ctx, thatUUID)`. When the seam is absent (a fork's
user provider that predates it) the backfill still runs on the interface
that exists: `GetUserCount(ctx, &iface.UserFilters{Role: "super_admin"}) > 0`
→ `ClaimFirstAdmin(ctx, "legacy-backfill")`. The placeholder is safe by the
sentinel's own contract: nothing reads its `userUUID` back
(`setup/service.go:226-245`), and `Release` deletes only a matching UUID
(`firstadmin.go:49-62`), so no signup rollback can ever remove it. Both
paths log INFO `first-admin sentinel backfilled` with the UUID or the
placeholder and `source=backfill`; a lookup or claim error logs ERROR and
the module still starts — the guard of D30 already closes the client tier,
and the next boot retries. `ClaimFirstAdmin` is `$setOnInsert`
(`firstadmin.go:22-43`), so a present sentinel is a no-op and concurrent
replicas converge. No migration script: one idempotent query and one upsert
per boot, before any request can reach the callback because `Start` precedes
`ListenAndServe`.

### 4.8 OAuth identity store: one source of truth (H-7, plus L-30)

**D32 — `*_oauth_providers` is authoritative; `user.oauthLinks` is derived.**

1. **Index, with human-decided conflicts** (v1.5). Both provider
   collections gain a unique index on `(provider, providerId)`
   (`auth/module.go:742-747`). Because `ensureCollections` is create-only and
   non-fatal, migration
   `backend/migrations/20260903_oauth_provider_identity_unique.js` (+ `.test.js`,
   `docs/migrations/0010_oauth_provider_identity_unique.md`) runs before the
   deploy that ships PR D (§7), following `20260823_authz_bindings_unique.js`
   in shape but not in policy:
   - it **never decides ownership**. The existing unique index on
     `(userUuid, provider)` already makes two rows of one user for one
     provider impossible, so every duplicate on `(provider, providerId)` is
     an identity claimed by two *different* users — a conflict no rule can
     settle. The migration lists each group (`provider`, `providerId`,
     every `userUuid` with its `linkedAt`, `lastUsed`, `tokenStatus`) and
     **exits non-zero without touching data**;
   - reconciliation is explicit: the operator re-runs it with a resolution
     map naming, per identity, the `userUuid` that keeps it
     (`--eval 'var RESOLVE={"google:1234":"<userUuid>"}'`); only rows of
     listed identities are deleted, only the losers, each deletion is
     printed, and the runbook asks for that output to be filed with the
     change. Unlisted groups still block;
   - with zero groups left it creates the index and verifies it; a re-run
     is a no-op.
   The auth module additionally verifies at `Start` that the unique index
   exists on both collections (list indexes): if it is missing, the module's
   health check reports degraded and every OAuth login and link path answers
   `oauth_store_unavailable` until it is created — a deploy that skipped the
   migration must not run the ownership-first flow of item 5 without the
   constraint it relies on. (Addition beyond the finding, flagged.)
2. **Tombstone instead of delete** — read from release D1, written only
   from D2 (§7). `models.OAuthProviderDoc` gains
   `UnlinkedAt *time.Time` (`bson:"unlinkedAt,omitempty"`). Admin and self
   unlink (`auth_service.go:616-684, 724-770`) resolve the `providerID` from
   `GetByUserUUID` (not from `user.OAuthLinks`), then call a new repo method
   `MarkUnlinked(ctx, userUUID, provider, now)`; only when that succeeds do
   they `$pull` the embedded link (best-effort, WARN). A failed tombstone is
   an error: nothing is removed, the identity stays valid, the caller sees
   503 `auth.oauth_store_unavailable`.
3. **A tombstoned identity is refused at the callback.** In
   `HandleOAuthCallbackWithLinking`, a `GetByProviderAndID` hit with
   `UnlinkedAt != nil` returns `ErrOAuthIdentityUnlinked`, mapped to the
   closed redirect contract's new allowlisted code
   `error=oauth_identity_unlinked` (`oauth_callback_redirect.go`; SPA copy:
   "This sign-in method was unlinked from your account. Sign in another way
   and re-link it from Security"). Without the tombstone a hard delete would
   be undone by the next callback: the unlinked branch auto-links by verified
   email (`:2206-2217`) and the operator would have removed nothing.
4. **Re-linking revives.** `SelfLinkOAuthFromCallback` (`:772-904`) treats a
   tombstoned doc owned by the same user as "revive": clear `UnlinkedAt`,
   refresh `LinkedAt`, re-add the embedded link. A tombstoned doc owned by a
   *different* user is deleted and replaced (the identity moved). The
   provider-doc write becomes the primary write (an error fails the link);
   the embedded write stays best-effort.
5. **Ownership is written first, and nothing is minted without it** (v1.4).
   `CreateOAuthProvider` stops being best-effort on the login path
   (`:2370-2373` today ignores its error and mints). The unique index of
   item 1 is what detects a conflict, so the write is the *first* durable
   step of every link path and its outcome decides the flow:
   - **success** → continue;
   - **duplicate key on `(provider, providerId)`** → re-read the row: owned
     by the same user (a benign double callback, two tabs) → continue with
     it; owned by another user → `ErrOAuthIdentityClaimedByOther`, callback
     code `oauth_identity_conflict`, no session;
   - **any other error** → `ErrOAuthStoreUnavailable`, callback code
     `oauth_store_unavailable` (D33), no session.

   *Email auto-link* (existing user, `:2206-2217`): insert the doc for that
   user, apply the rule above, then `AddOAuthLinkToUser` (read-model,
   best-effort WARN), then mint.

   *Signup* (`:2146-2205`), where two writes in two modules cannot share a
   transaction, becomes a reservation with compensation:
   1. insert the identity doc with `userUuid = newUUID` (the UUID the user
      will be created with, D30) — the rule above applies; a conflict here
      means no user is ever created for a lost race;
   2. claim the first-admin sentinel when the tier allows it (D30);
   3. `CreateUserFromOAuth` with that UUID;
   4. on failure of 2 or 3: delete the identity doc, `Release` the sentinel
      if claimed, return the error (no session). A compensation failure is
      logged at Error with `orkestra_auth_oauth_compensation_failures_total`
      and item 8 heals it.
   Only after 3 does the branch add the embedded link and mint.

   *Existing-link login* (doc found) is unchanged: token refresh, last-used
   and metadata updates (`:2265-2295`) stay best-effort — they are not
   ownership.
6. **Readers move.** `GetUserAuthMethods` (`:918-1008`), `GetOAuthLinks`
   (`:569-595`) and `wouldLockOutOAuthUnlink` (`:686-722`) read the
   non-tombstoned provider docs (`Email`, `LinkedAt`, `LastUsed`,
   `IsPrimary`); "active" means "no tombstone".
7. **Lazy repair of the read-model.** On a login through an existing
   provider doc, if the user's embedded links lack that `(provider,
   providerId)`, `AddOAuthLinkToUser` runs best-effort. No full backfill: the
   embedded slice no longer decides anything.
8. **An orphan reservation heals itself** (v1.4). A provider doc whose
   `userUuid` names a user that does not exist (a crash between steps 1 and
   3 of the signup, or a failed compensation) is not a linked identity: when
   the callback finds such a doc and `GetUserByID` answers *not found* (an
   outage answers `oauth_store_unavailable`, D33), the doc is deleted and the
   flow continues as unlinked — the same person completes the signup on
   their next attempt. Today that state is terminal (`:2118-2125` treats any
   lookup error as fatal).

**D33 — Degraded lookups fail closed (L-30, addition).** In the callback,
a non-nil error from `GetByProviderAndID` (`:2111-2114`) or a non-not-found
error from `GetUserByEmail` (`:2146`) returns `ErrOAuthStoreUnavailable`
(callback code `oauth_store_unavailable`, a retryable outage) instead of
falling into the auto-link/signup branch. `GetByProviderAndID` already
returns `(nil, nil)` on no-documents (`oauth_provider_repository.go:113-130`),
so the string comparison at `:2112` is deleted. Flagged as an addition
because it is the same ten lines D32 rewrites.

### 4.9 PKCE (M-18)

**D34 — Send what the providers already accept.** The start endpoints
(`auth_handler.go:513-545, 621-642`) mint a verifier with
`utils.GenerateCodeVerifier()`, derive the S256 challenge, store the verifier
in the state row (`StoreOAuthStateRequest.CodeVerifier`, already persisted at
`oauth_state_service.go:172-173`) and pass the challenge to `GetAuthURL`. The
`oauthExchange` closure (`oauth_callback_flow.go:34-37`) gains a
`codeVerifier string` argument, filled from `res.info.CodeVerifier` at
`:219`, and both closures set `CodeExchangeRequest.CodeVerifier`. Every
provider already emits `code_challenge`/`S256` and `code_verifier` when
non-empty (`google_oauth_service.go:56-58,75-77`; `apple:97-99,122-124`;
`github:51-53,69-71`; `discord:51-53,68-70`).

A new `SupportsPKCE() bool` on `OAuthProviderInterface` decides per provider
whether the challenge is sent: **Google and Discord `true`; GitHub and Apple
`false` until the staging round-trip in §7 confirms their token endpoints
accept `code_verifier`**, after which flipping the constant is a one-line
change. A provider that returns `false` gets an empty challenge and an empty
verifier, exactly today's behaviour. The dead `ValidateOAuthCallback`
(`oauth_state_service.go:453-483`) and the duplicate PKCE helpers in
`shared/utils/crypto.go:95-118` are deleted; `utils/pkce.go` gets its test.
The state row stays plaintext JSON: a reader of the Redis row already holds
everything else in it, and the verifier is useless without the
provider-issued `code`.

### 4.10 Mobile ID tokens: nonce and issuer (M-20)

**D35 — A server-issued challenge, consumed once, bound to a client-held
verifier; issuer-checked, audience-mandatory** (v1.7).

*Two-step contract.* The mobile flow becomes `begin` + `complete`:

- `POST /v1/auth/{tier}/{google,apple}/mobile/begin` — body
  `{ code_challenge }` (S256 of a verifier the app generates and keeps in
  memory). The backend mints a 32-byte nonce, stores
  `{provider, tier, codeChallenge, createdAt}` under
  `oauth:mobile:nonce:<sha256(nonce)>` in the `OAuthStateStore` with the
  10-minute TTL the web state uses, and returns `{ nonce }`. The app hands
  the nonce to the provider SDK (Google passes it through; Apple's SDK
  takes `sha256(nonce)` and the ID token carries that hash).
- `POST /v1/auth/{tier}/{google,apple}/mobile` — body
  `{ id_token, code_verifier }`; the client-supplied `access_token` is
  gone (`:1310-1315` stored it unverified). Order of checks:
  1. token validation (below);
  2. the `nonce` claim is required; the record key is derived from it
     per provider (Google: `sha256(claim)`; Apple: `claim`) and **taken**
     with `OAuthStateStore.Take` (GETDEL, `oauth_state_service.go:329-335`)
     — one presenter wins, the record is gone;
  3. `record.provider` and `record.tier` must match the route, and
     `sha256(code_verifier)` must equal `record.codeChallenge`
     (constant-time compare);
  4. only then `HandleOAuthCallbackWithLinking` (D32 rules apply).
  Any miss is one opaque 401 `invalid id token`; the record, if taken, is
  not restored — a failed completion burns the challenge, as the web
  relay does (`oauth_callback_flow.go:306-318`).

The separate replay counter on `sha256(id_token)` of v1 is dropped: the
one-shot record is the replay guard.

*Token validation.* `IDTokenValidationRequest`
(`oauth_provider_interface.go:47-54`) gains `Issuers []string` and
`ExpectedNonce string` (empty on the web Apple exchange, which is unchanged
and keeps the state-cookie binding). Both validators
(`google_oauth_service.go:190-263`, `apple_oauth_service.go:256-325`) check,
in order: signature (as today), `jwt.WithExpirationRequired()`,
`iss ∈ Issuers` (Google: `https://accounts.google.com`, `accounts.google.com`;
Apple: `https://appleid.apple.com`), `aud` **always** — an empty
`request.Audience` is an error, no longer a skip (`:234`, `:303`), so
`MobileAudience` returning `""` fails the login instead of disabling the
check — and, when `ExpectedNonce != ""`, the `nonce` claim must equal it.
The Google handler resolves the platform from `deviceInfo.Platform` like the
Apple one (`:1284` hardcodes `"android"`). Errors returned to the caller
drop the wrapped `err` (`:1279, 1294, 1390, 1411`); details go to the log
(L-27, same lines).

*Threat model, stated.* What this defeats: an ID token exfiltrated on its
own (SDK logs, crash reports, a leaked debug build) — worthless without the
verifier and useless after one presentation; a token minted for another
app or flow — its nonce has no record; a replay — the record is gone. What
it does not defeat: an attacker who observes the completion request itself
(a compromised device, a broken TLS channel) holds token and verifier and
can race the legitimate completion. No request-level control closes that;
it is the same boundary the web flow's cookie binding has, and the platform
accepts it there. "Attacker first" is therefore possible only from inside
that boundary, never from token theft alone.

The in-tree Flutter app does not call these routes (`mobile/lib` has no
OAuth code), so the two-step contract breaks nothing shipped; the mobile
`README` and `CLAUDE.md` gain the new sequence.

### 4.11 Documentation (same commit as the code it describes)

Each PR updates the contract and the reference for what it touches. The
sentences known to be wrong after this spec:

- `auth/CLAUDE.md` — `:28, :116, :185, :341, :1071, :1091` (limiter shared with
  Register/ForgotPassword, "only protection"); `:832-833, :839-840` (enrolment
  gates); `:890, :894, :902` (`reauth` "can never bypass"); `:892` (step-up
  list); `:837-838` (reset: sessions, passkeys); `:184, :1040` (first-user
  heuristic, OAuth sentinel); `:38, :49, :847, :856, :947, :1065` (identity
  store, index, unlink, auth-methods source); `:41, :766, :805-806` (PKCE,
  mobile nonce/iss); new sections: attempt counters, enrolment gate and the
  `auth_time` claim, refresh AMR recomputation, tombstones.
- `authz/CLAUDE.md` — `:90` (stale gate heading), `:179` (cache key and
  invalidation are now generation-based), `:97, :146-153, :157-162,
  :177, :181, :204, :222` (cascade now covers role edits; rule 4 now
  enforced), `:250, :261` (enforce note, `tenant_roles` source), new
  `system_actions.cedar` row.
- `user/CLAUDE.md` — `:121-122` (role change terminates sessions +
  invalidates), `:125` ("the two are synced" → derived read-model), new
  `mfaEpoch` field and the `MFAEpochBumper` seam the auth module consumes.
- `docs/site/modules/core/auth.mdx` (`:13` "OAuth 2.1", `:22, :126, :131`,
  new lockout paragraph), `authz.mdx` (`:12-14, :51-58, :62-67, :78, :91`),
  `user.mdx` (`:15, :50`), `docs/site/architecture/authentication-flow.mdx`
  (`:120` claims table gains `auth_time`; `:167, :174-177, :191, :199-203,
  :275, :280, :304, :318, :332`).
- `backend/CLAUDE.md` "Error-code contract" example list gains
  `auth.too_many_attempts`; `pkg/sdk/CLAUDE.md` records
  `AuthzCacheInvalidator`, `MFAEpochBumper`, `SystemRoleHolderFinder` and
  the `User.MFAEpoch` field;
  the JWT claims table in `authentication-flow.mdx` gains `auth_time` and
  `mfae`; `docs/migrations/0010_*.md` is new.
- Admin-UI field text at `auth/module.go:422-428` is now true and stays;
  the two new IP keys are documented in the `auth/CLAUDE.md` policy table
  ("Login & Sessions" row) and in `auth.mdx`.

## 5. Edge cases

1. **Redis down during login (D1).** Counters answer "not locked" and
   record nothing; the durable lock (D4) still engages for existing accounts
   after `threshold` cumulative failures. Unknown-email guessing is
   unthrottled beyond `api:general` for the duration of the outage — the same
   exposure the platform accepts for session revocation. WARN + metric.
2. **Redis down during password-confirm / MFA verify (D6, D20).** Same
   fail-open; the per-challenge Redis counter in `FinishAssertion` fails
   closed already (it destroys the challenge).
3. **Threshold lowered while a window is open (D2).** The next `Locked`
   peek compares the current count with the new threshold: a lower threshold
   locks immediately, a higher one unlocks. No stale bucket capacity (the
   old `getBucket` froze it).
4. **Same email on both tiers (D2).** The email scope is per audience;
   operator and client accounts with the same address lock independently. The
   IP scope is shared, as it is today.
5. **Successful login while the IP scope is locked (D3).** Impossible: the
   peek runs first and answers 429 for the IP regardless of credentials. This
   is intended — a locked IP is locked.
6. **Forgot-password over the per-email cap (D5).** Generic 200, no token,
   no mail. The victim's last valid token is preserved because the attacker's
   fourth request no longer invalidates it.
7. **Queued sends at process shutdown (D5).** `Stop` gives the dispatcher
   10 s to drain the queue, then abandons what is left; an in-flight job runs
   under a detached 60 s context. A restart may lose a queued or in-flight
   reset mail; the user retries. A queue that is full when a request
   arrives drops that request's mail with a metric, never a wait.
8. **First enrolment with a stolen bearer (D11).** A bearer whose session
   began more than `maxAge` ago is refused with `reauthentication_required`,
   and the attacker cannot re-authenticate (no password, no identity
   provider). A bearer stolen inside the window after the victim's own login
   can still enrol — the residual every freshness proof shares — and D13
   makes it visible; the victim recovers through the admin reset (D15/D16),
   which also ends the attacker's sessions.
9. **Enrolment gate with lookup outage (D11).** `step_up_required` for
   everyone: a user with a factor can satisfy it, a user without one cannot
   enrol until Mongo recovers. Fail closed on purpose. A pre-deploy session
   (no `auth_time`) is the other "cannot enrol without a re-login" case.
10. **Passkey-only user, self `Remove` (D15/D16).** `RemoveFactor` now
    deletes the WebAuthn row too and bumps the epoch; sessions except the
    current are revoked; the current session's next gated request already
    answers `step_up_required`, and its markers leave the token at the next
    refresh (D17).
11. **Admin resets their own MFA through the admin route (D16).**
    `TerminateAllSessionsByUUID` includes the admin's current session; the
    reset still succeeds and the admin is logged out. Acceptable and audited.
12. **Gate or refresh cannot read the epoch (D16/D17).** The gates treat
    the markers as absent (`step_up_required`); a refresh that cannot load the
    user already fails as a whole (`auth_service.go:1530`). Never fail open.
    A user document without `mfaEpoch` (every document before this ships)
    reads as 0 and matches every pre-deploy token, so nothing is downgraded
    by the deploy itself; the first removal on that account bumps it to 1.
13. **Custom role that already holds a system key (D21/D22).** Existing
    data: the evaluator ignores the key from tenant scope (D22); the next
    `UpdateRole` that supplies `Permissions` must drop it or is refused with
    422 naming the key. `IsActive`-only patches do not re-validate.
14. **Actor is `super_admin` (D21).** Wildcard covers everything in the
    cascade; the catalog and system-key checks still apply — a super_admin
    cannot put `system.users.admin` into a tenant role either.
15. **`developer` in production under enforce (D23).** The forbid exempts
    the role; the permit `platform.developer.prod_readonly` and the role
    table both bound it to read-only. Unchanged outcome, now explicit.
16. **Role change on oneself (D27).** The actor's sessions are terminated;
    the response is still 200 and the audit row is written first.
17. **Role change when the auth module is not wired (tests) (D27).**
    Both helpers degrade to no-op with a WARN, as `terminateSessions` does.
18. **Sentinel backfill on a fresh install (D31).** Zero super_admins →
    no claim; bootstrap paths unchanged.
19. **Backfill races a live first signup (D31).** `$setOnInsert`: whoever
    upserts first wins; the loser is a no-op. A signup that loses to the
    backfill gets `guest`, which is correct — a super_admin already existed.
20. **Unlink, then the same identity signs in (D32).** Callback refused
    with `oauth_identity_unlinked`; the account is unaffected.
21. **Unlink, then the same user re-links from Security (D32).** Revived in
    place; audit `self_oauth_link`.
22. **Identity migrates between users (D32).** B links an identity A had
    unlinked: A's tombstone is deleted, B's doc created. A never sees it
    again. If A had *not* unlinked, B gets `ErrOAuthLinkClaimedByOther` as
    today.
23. **Duplicate `(provider, providerId)` rows at migration time (D32).**
    Every such group is two users claiming one identity; the migration
    reports and refuses, the operator names the keeper, and only then is the
    index built. The deploy is blocked until the migration has exited zero
    (§7); a deploy that bypasses it degrades OAuth to `oauth_store_unavailable`
    at boot rather than running without the constraint.
24. **Provider that ignores `code_challenge` but rejects `code_verifier`
    (D34).** Prevented by `SupportsPKCE`; only Google and Discord send it
    until staging proves the others.
25. **Mobile completion without a live challenge (D35).** A token whose
    `nonce` claim has no record (never begun, expired after 10 minutes,
    already taken, minted for another app) or a `code_verifier` that does
    not hash to the recorded challenge → one opaque 401; the record, if it
    existed, is consumed.
26. **Same ID token presented twice within its lifetime (D35).** The
    second presentation finds no record and answers 401; the first session
    is unaffected. Two presentations racing: GETDEL gives the record to
    exactly one.
27. **Two first callbacks for one identity race (D32 item 5).** Both miss
    the lookup; the first insert reserves the identity; the second gets the
    duplicate key, re-reads, finds its own user (auto-link: same email) and
    proceeds, or finds another user (a signup race that lost) and is refused
    with `oauth_identity_conflict` before any user row exists. No session is
    ever minted for an identity the store does not record as the caller's.
28. **Crash between reservation and user creation (D32 items 5 and 8).**
    The next callback finds the orphan doc, its user is *not found*, the doc
    is deleted and the signup runs again from scratch. A claimed sentinel
    with no user behind it is the same window the password path has
    (`firstadmin.go:13-21`) and is unchanged by this spec.
29. **Redis refuses the pre-invalidation (D27).** The role change, or the
    authz mutation, answers 503 and nothing is written; the admin retries
    once Redis is back. Consistent with every other Redis-dependent write
    on the platform.
30. **Pre-invalidation succeeds, post-invalidation fails (D27).** Only a
    cache entry written in the sub-second window between the two can carry
    the old verdict; it dies within 60 s; ERROR log, metric and audit
    metadata say so. This window is the whole residual behind goal 6.
31. **Shared egress (D2).** An office NAT produces wrong passwords from many
    people; each *account* still locks at 5 per window, the *address* only at
    100. When the address does trip, every login through it answers 429 with
    `Retry-After` until the window ends and `orkestra_auth_lockouts_total{scope="ip"}`
    moves — a stuffing signal worth alerting on. An operator who knows their
    egress is large raises `ipLockoutThreshold`; the validator refuses to
    set it below the account threshold.

## 6. Testing

Recipe for every PR (from `docs/superpowers` playbook): live Mongo for
guarded tests, `MONGO_TEST_URI='mongodb://127.0.0.1:28017/?directConnection=true'`;
gates via `make -C /home/tore/orkestra ci-backend` (and `ci-frontend-client`
for D14); `git diff --check`. New tests:

**PR A — counters**
- `attempt_counter_test.go` (miniredis, whose Lua engine runs the real
  script): threshold reached inside window → locked; window expiry →
  unlocked; concurrent `RecordFailure` × N counts N (the
  `mfa_attempts_atomic_test.go` shape); a key pre-seeded **without a TTL**
  (`SET` outside the script) is healed to `Window` by the next peek and by
  the next increment, so a locked orphan unlocks on its own; `RetryAfter`
  equals the key's PTTL; `Reset` clears; a store error is returned to the
  caller, the metric increments and the WARN is throttled; a client without
  `Eval` makes the auth module refuse to boot.
- `mfa_challenge_service_test.go`: `IncrementAttempts` on a counter key
  without TTL heals it (today such a key is orphaned forever).
- `password_auth_service`: with a counter that returns an error, the
  durable rule alone locks an existing account at `threshold` and unknown
  emails are answered 401 (documented fail-open).
- `mail_dispatcher_test.go`: with a fake sender that blocks until released,
  16 workers are busy and the 17th job waits in the queue (the fake counts
  concurrent calls and never sees 17); with all workers blocked and the
  queue full, the 257th enqueue returns at once, is dropped, and the metric
  and WARN fire; `runtime.NumGoroutine` before and after 10 000 enqueues
  differs by zero; `Stop` drains queued jobs when the sender is released
  within 10 s and abandons them otherwise; enqueue latency is the same for
  a queue with 0 and with 255 jobs.
- `password_auth_service`: known and unknown email produce identical
  status/code sequences over `threshold+1` attempts and identical dummy-verify
  calls (extend `gateUserFake`/`fakePasswordService` with a verify counter);
  durable lock set only when the counter reports locked, or when the store
  is unavailable; expired `LockedUntil` is cleared before verify; success
  resets the email scope but not the IP scope; forgot-password over the
  per-email cap issues no token and sends nothing; resend never touches the
  login scopes; change-password and password-confirm answer 429 when locked
  and record failures; `TestForgotPassword_PasswordMethodGate`,
  `TestConfirmPassword_*`, `TestChangePassword_*` keep passing.
- `service_account_grant_test.go`: `TestGrantRateLimited`,
  `TestGrantSuccessiveSuccessesNotRateLimited` ported to counters.
- `rate_limiter_test.go`: a `-race` test with concurrent `Check` +
  `Middleware` calls (the throwaway probe from the audit, kept).
- `error_mapping_test.go`: `ErrAccountLocked` → 429 `auth.too_many_attempts`
  with `Retry-After`; `codes_test.go` golden updated.
- IP pair (D2): `auth_policy_service_test.go` covers the two accessors
  (empty, malformed, `< 1`, valid); `config_validation_test.go` refuses
  `ipLockoutThreshold < accountLockoutThreshold` with the new code and
  accepts equality; `config_groups_test.go` field count for `login` updated;
  a login test with 6 wrong passwords for 6 different emails from one IP
  locks none of the addresses and not the IP.

**PR B — MFA**
- `step_up_test.go`: `RequireEnrolmentProof` — factor + fresh proof passes;
  factor + stale/missing proof → `step_up_required`; no factor + fresh
  `auth_time` passes; no factor + fresh `reauth` passes; no factor + stale
  `auth_time` → `reauthentication_required` with `maxAgeSeconds`/`authTime`;
  no factor + missing `auth_time` (pre-deploy token) → `reauthentication_required`;
  lookup error → `step_up_required`; `RequireMFA` rejects a `reauth`-only
  token (new); `RequireStepUp` still accepts it
  (`TestRequireStepUp_ReauthAMRSatisfiesGate` unchanged);
  `coded_error_golden_test.go` gains the new envelope.
- `jwt_service_amr_test.go`: `auth_time` round-trips through
  `claimsToMap`/`mapToClaims` and is omitted when zero; login-path tests
  (password, MFA completion, passkey completion, OAuth callback, relay, setup
  wizard, dev token) assert it is stamped at issuance; refresh and
  password-confirm tests assert it is carried unchanged.
- `frontend-admin`: `baseApi` turns `reauthentication_required` into a
  `/login?next=` navigation with the sanitised current path;
  `frontend-client`: `authedFetch` does the same for `/account/security/mfa`.
- `mfa_service_test.go`: `TestEnrollmentIdempotentReset` unchanged;
  replacement revokes device trust and emits `self_mfa_factor_replaced`;
  `RemoveFactor` deletes both rows; passkey-only removal succeeds.
- Handler tests (new file `mfa_handler_reset_test.go`): admin reset calls
  the terminator and records `sessions_terminated`; self remove calls
  `RevokeAllUserSessionsExcept` with the caller's sid; removing one passkey
  while a TOTP factor and a second passkey remain still revokes the other
  sessions and bumps the epoch; TOTP replacement revokes the other sessions;
  `/mfa/verify` locks after 5 failures and answers 429; success resets.
- `auth_service` refresh tests: markers kept when the token epoch matches
  the user's, dropped when stale; `reauth` always dropped; `LastOTPAt`
  follows the markers; the new token carries the current epoch.
- MFA epoch: `jwt_service_amr_test.go` round-trips `mfae` (omitted when
  zero); `mfa_service_test.go` asserts a bump on self remove, admin remove,
  passkey removal and TOTP replacement, and no bump on first enrolment or
  passkey addition; `step_up_test.go` asserts that a token whose epoch is
  behind the user's answers `step_up_required` on `RequireMFA`,
  `RequireStepUp` and `RequireEnrolmentProof`, that `IsMFAEnrolled` reads
  false for it, that the impersonation bypass demands MFA for it, and that a
  user lookup error is treated as stale; a handler test removes the factor
  with a stepped-up token and shows the **same** token refused on the very
  next step-up route.
- `password_confirm_test.go`: MFA-obligated user with no factor → 403
  `mfa_enrollment_required`.
- Notification: template `auth.mfa_factor_added` seeded and rendered
  (`template_parity_test.go` in `cmd/server` covers seeding parity).
- `frontend-client`: `MfaEnrolPage` renders the enrolled state when
  `/me/mfa` reports `enrolled`.

**PR C — authz**
- `tier1_crud_test.go`: `TestUpdateRole_CustomRolePermissionsUpdate` gains an
  actor and registered keys; new: unknown key → `ErrUnknownPermission`;
  system key / `*` → `ErrSystemPermissionInCustomRole`; actor lacking a key →
  `ErrInsufficientPermissionsToGrant`; `granterSystem` bypasses the cascade
  only; empty actor → `ErrGranterRequired`; the H-4 probe sequence
  (create → bind → update with `tenant.delete`) now fails at the update.
- `tier1_test.go`: a tenant-scoped binding holding a system key does not
  grant it (D22).
- `engine_test.go`: `org_owner` + internal tenant + `system.users.admin` →
  deny; `org_admin` + `system.modules.admin` → deny; `super_admin` /
  `administrator` internal → allow (existing); `developer` non-prod → allow;
  `TestPolicyLoadingNonEmpty` count updated.
- `service_test.go`: `shadowEvaluate` with ctx tenant roles and
  `tenantID == ""` stamps none; under `EnforceActions: ["system.users.admin"]`
  the role-table deny is not overridden (the H-5 probe, inverted).
- Handler tests (new): create/update role map the new sentinels to
  400/422/403.
- Generation cache (D26), `cache_test.go`: the key carries both
  generations; `InvalidateUserPermissions` is one `INCR` and no `KEYS` is
  issued (miniredis command log); an entry written under the previous
  generation is not read after the bump; the global flush retires every
  user's entries; an `MGET` failure reads as a miss; retired entries expire
  on their own TTL.
- Role-change ordering (D27): pre-invalidate failure → 503 and
  `UpdateUser` never called; post-invalidate failure → change persisted,
  audit `cache_invalidated: false`, metric incremented; a repopulation
  injected between pre-invalidate and write is retired by the post step
  (hook on the fake invalidator); the same three cases for `UpdateRole`
  through `withGeneration`.
- `user/handlers`: role change calls the invalidator and the terminator
  (extend `recordingTerminator` with a recording invalidator); lookup failure
  in `canAssignRole` → 500; client PATCH refuses escalation (D29);
  `setup/service_test.go`: recovery gate uses the DB role.

**PR D — OAuth**
- `gates_test.go`: `TestOAuthCallback_ClientFirstUser_NeverClaimsSuperAdmin`
  (twin of `:301`); operator OAuth first user still claims; claim error is
  fatal.
- Sentinel backfill: unit test with `gateClaimer` + a user fake implementing
  `SystemRoleHolderFinder` → claimed once with that UUID, idempotent on a
  second run; a fake **without** the seam but counting one super_admin →
  claimed with `legacy-backfill`; zero super_admins → no claim; finder error
  → no claim, ERROR logged, `Start` returns nil. `user_service_test`:
  `FindOldestUserWithRole` picks the earliest `createdAt`, ignores deleted
  users, includes inactive ones, ties broken by uuid.
- `auth_service_admin_unlink_test.go` / `_self_unlink_test.go`: services
  built with `fakeOAuthProviderRepo` (extended with `unlinked` map); unlink
  tombstones the doc then pulls the link; tombstone failure removes nothing;
  the "unlink → callback with same providerID → `ErrOAuthIdentityUnlinked`"
  regression; re-link revives; identity moves between users.
- `oauth_inactive_user_test.go` fixture gains the tombstone field; the
  "logs in regardless of the bit" carve-out test is kept for live docs.
- Degraded lookups (D33): repo error → `ErrOAuthStoreUnavailable`, never the
  signup branch.
- Ownership-first writes (D32 item 5): `fakeOAuthProviderRepo` gains an
  injectable `createErr` and duplicate-key simulation; duplicate on
  auto-link with the same owner → session minted once, one doc; duplicate
  with another owner → `ErrOAuthIdentityClaimedByOther`, no session, no
  `AddOAuthLinkToUser`; store error → `ErrOAuthStoreUnavailable`, no session;
  signup with a duplicate at step 1 → no `CreateUserFromOAuth` call, sentinel
  never claimed; signup with `CreateUserFromOAuth` failing → identity doc
  deleted and sentinel released (`gateClaimer.released`); orphan doc + user
  not found → doc deleted, signup proceeds; orphan doc + user lookup outage →
  `oauth_store_unavailable`. `oauth_callback_redirect.go`'s allowlist test
  gains `oauth_identity_conflict` and `oauth_identity_unlinked`.
- `oauth_callback_flow_test.go`: `fakeProvider` records
  `CodeExchangeRequest.CodeVerifier`; start endpoint stores a verifier and a
  non-empty challenge for a `SupportsPKCE` provider, empty for the others;
  the verifier reaches the exchange.
- `utils/pkce_test.go`: verifier length/charset, S256 round-trip.
- Mobile: table tests for both validators with a locally-signed RSA key
  (wrong `iss`, wrong `aud`, empty `aud`, missing `exp`, nonce match, nonce
  mismatch, nonce missing); handler tests through `begin` + complete with
  a fake store: happy path takes the record exactly once; second
  presentation → 401; wrong `code_verifier` → 401 and the record is
  consumed; record from the other provider or tier → 401; expired record →
  401; concurrent completions → one winner (the
  `TestMFAChallengeConsume_AllowsExactlyOneConcurrentWinner` shape); no
  `access_token` is persisted.
- Migration `.test.js`: a cross-user duplicate group → non-zero exit, no
  document changed, group printed with both users; the same run with a
  `RESOLVE` entry → only the losing rows of that identity deleted and
  printed, unresolved groups still block; no groups → index created and
  verified; re-run → no-op. Boot check: with the index absent the auth
  health check is degraded and the callback answers
  `oauth_store_unavailable`; with it present the flow runs.

## 7. Rollout and verification

Five deliverables (PR D is two releases), each independently shippable, in
this order:

| PR | Sections | Depends on | Notes |
|---|---|---|---|
| **A** | §4.1 | — | First: removes an anonymous DoS. Verify on staging: 6 wrong passwords from one IP → 429 with `Retry-After`; known and unknown email identical; forgot-password 4× → third mail is the last; `orkestra_auth_lockouts_total` moves |
| **B** | §4.2, §4.3 | A (D20 uses the counters) | Staging drill: enrol TOTP, call `enroll/begin` again with a plain session → `step_up_required`; admin reset → target's other tab is signed out; passkey-only reset succeeds |
| **C** | §4.4, §4.5, §4.6 | — | Can run in parallel with B. Staging drill: the H-4 probe sequence against the API returns 403 at the update; demote a test admin → their session ends |
| **D1** (expand) | §4.7, §4.8 items 1, 3–8 (readers and writers, **no tombstone writes**), §4.9, §4.10 | migration 0010 exited zero on every environment **before** deploy — a conflict report stops the rollout until an operator names the keeper per identity | Login refuses a doc with `unlinkedAt`, listings exclude it, ownership-first writes, boot index check; the unlink routes keep today's `$pull`-only behaviour. Staging drill: seed one provider doc with `unlinkedAt` by hand → its sign-in answers `oauth_identity_unlinked` and it is absent from auth-methods; a normal unlink still behaves as today; PKCE and mobile drills as listed below |
| **D2** (contract) | §4.8 item 2 and the revive/move rules of item 4 | D1 deployed and verified on **every** environment; rollback floor becomes D1 the moment the first tombstone exists | Unlink writes `unlinkedAt`. Staging drill: unlink Google, sign in with Google → `oauth_identity_unlinked`; re-link from Security → works; roll back to D1 on staging and confirm the unlinked identity is still refused |

PKCE and mobile drills (D1): PKCE round-trip per provider — Google and
Discord must succeed; GitHub and Apple are exercised with `SupportsPKCE`
flipped on a staging branch and the constants promoted only if they
succeed; mobile `begin` + complete with a test app or `curl` against a
locally-signed token.

**Rollback floors** (v1.11). Every change here is expand-before-contract,
and the floors are:

| Change | Old binary sees | Rollback |
|---|---|---|
| `unlinkedAt` on provider docs (D32 item 2) | an ordinary linked identity — H-7 reopens for every account unlinked since D2 | **Hard floor: D1** once any tombstone exists. D1 reads tombstones and writes none, so D2 → D1 is always safe; going below D1 requires either zero tombstones or an explicit decision to re-link those identities |
| Unique index on `(provider, providerId)` (item 1) | a duplicate-key error on a lost race, which it already ignores | Safe; the index stays and only removes a duplicate the old code would have created |
| `auth_time`, `mfae` claims (D11, D16), `User.MFAEpoch` | unknown claims and field, ignored | Safe; the protections revert to claims-only for the rollback's duration, no data harm |
| Redis counters and generations (D1, D26) | unknown keys, ignored; the old in-memory limiter resumes | Safe; keys expire on their own TTL |
| Sentinel backfill (D31) | a claimed sentinel | Safe; that is the state a fresh install has |
| Mobile two-step (D35) | the one-step contract | Safe in-tree (no shipped caller); a shipped app must ship against D1 or later |

The release notes of D2 name the floor; the promote playbook's pre-flight
gains one line: "does the target environment hold tombstones? then the
rollback target is D1 or later".

After D2: the docs pipeline publishes on the `main` push; verify the three
touched pages at the destination (memory: a green sender proves nothing).

Separately, after PR C is on staging for one release cycle with
`orkestra_cedar_shadow_divergence_total` quiet for `system.*` suffixes:
set `CEDAR_ENFORCE_ACTIONS` to the four keys `authz/CLAUDE.md:250` recommends.
Not part of this spec's PRs.

## 8. Follow-ups (named, not started)

From the audit, outside this spec's perimeter:

- **M-4** invite flow: validate `Roles`, cascade-check the inviter, bind on
  accept, require acceptor email == invited email; `SetMemberRoles` ordering.
- **M-9** refresh-token `jti` + unique index on `token`.
- **M-10** `/v1/me/dsr/erase` behind `RequireStepUp(5m)` + last-super_admin
  guard. **M-11** notification template writes behind
  `notification.template.manage`. **M-12** service-account disable → random
  `sid` + revocation. **M-14** impersonation marker instead of the
  `administrator` tenant role; MFA for business tenants. **M-15/M-16**
  staging posture and `/metrics`. **M-19** Apple form_post continuation.
- **L-2** duplicate-email register → 409; **L-3/L-4** email-token
  invalidation and atomic single-use; **L-5** refresh cookie path;
  **L-6** argon2 concurrency bound; **L-7** IP-gate malformed CIDR warning;
  **L-8** `IsProductionLike` in the authz dev fallback and the error
  sanitizer; **L-13/L-14/L-15** config validation; **L-17** device cookie
  `Secure`; **L-18** clone-warning enforcement; **L-19** login challenge
  binding; **L-28** JWKS refetch throttle; **L-29** is closed by D32's index.
- Client SPA step-up modal (so factor replacement works on the client tier
  without an admin reset).
- Admin "unlock account" affordance (clears `LockedUntil` and the email
  scope) and a `sessions_terminated` badge in the admin user timeline.
