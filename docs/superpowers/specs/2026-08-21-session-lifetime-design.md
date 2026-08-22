# Session lifetime, token TTL sourcing, and auth retention — design

| Field | Value |
|---|---|
| **Date** | 2026-08-21 |
| **Status** | Approved — ready for implementation planning |
| **Scope** | `backend/internal/core/auth`, `backend/internal/shared/{config,utils}`, `backend/pkg/sdk/{module,metrics}`, `frontend-admin`, `docker/` |
| **ADR** | [ADR-0017](../../adr/0017-session-lifetime-and-token-retention.md) records the one decision that changes inherited behaviour (the absolute session cap) |

## Problem

An audit of "how often is a user forced to log in again" on a fresh Orkestra
install found seven defects. Every claim below was verified against the code at
`80959962`; file:line references are to that commit.

**Effective defaults today:** access token 15m, refresh token 7d, rotation
sliding (every refresh restarts the 7 days), no absolute cap, no idle timeout
distinct from the refresh TTL. An active user is never forced to re-authenticate;
an inactive one is logged out after 7 days.

### ① `JWT_ACCESS_TOKEN_EXPIRY` is unreachable, and that silently breaks revocation

`jwtService.accessTokenLifetime()` (`services/jwt_service.go:104`) uses the admin
policy value when `> 0` and otherwise falls back to the env-derived
`s.accessExpiry`. But `AuthPolicyService.AccessTokenTTL`
(`services/auth_policy_service.go:179`) never returns `0` — on an unset or
invalid value it returns `defaultAccessTokenTTL` (15m). The fall-through is dead,
so the env var has no effect on any deployment with the policy wired, which is
every deployment.

This contradicts the documented contract in
`backend/internal/core/auth/CLAUDE.md:451`, which states the chain is
`admin accessTokenTTL → JWT_ACCESS_TOKEN_EXPIRY → 15m`. It is a regression against
a written contract, not a design choice.

The consequence is a security defect. `module.go:805-808` constructs the Redis
revocation denylist with `cfg.Auth.JWT.AccessTokenExpiry` — the env value, read
**once at boot** — and stores entries with `ttl = accessTokenTTL + 1m`
(`services/session_revocation_service.go:82`). The access token's real lifetime
comes from the policy, read live on every mint. The two are unconnected:

> An operator who raises `accessTokenTTL` to `4h` at `/admin/modules` gets 4-hour
> access tokens, while the `auth:revoked:session:<sid>` entry still expires after
> 16 minutes. Once it expires the sid is no longer considered revoked and **access
> tokens belonging to a revoked session become valid again** for the remaining
> hours. There is no error and no log. Logout, change-password, and
> `TerminateAllSessions` are all served by that denylist
> (`shared/middleware/auth.go:285`, `shared/middleware/jwt_validator.go:152`).

Compounding it, `docker/.env.example:137` ships `JWT_ACCESS_TOKEN_EXPIRY=1h`
while every compose file and `config.go:271` default to `15m` — three values, none
of them in force.

### ② No absolute session cap

The idle timeout is not missing: it **is** the refresh TTL. Rotation writes
`ExpiresAt: now + RefreshTokenTTL()` (`services/auth_service.go:1401`) and only
happens when the user is active, so 7 days of inactivity ends the session.

What is genuinely absent is an absolute cap. Nothing bounds total session age:
`RefreshTokensWithRiskAssessment` never consults the session document, and the
refresh-family fence extends with the newest token
(`repository/refresh_token_repository.go:499`).

**The anchor already exists.** Every credential-issuing path creates a session
document, and failure to persist it rolls back the just-minted refresh token —
`password_auth_service.go:1527-1531` ("no usable credential survives without its
session record") and `auth_service.go:1248-1270`. Path coverage: password, MFA
verify, WebAuthn, and the setup wizard funnel through `issueTokensForSession`;
OAuth goes through `GenerateEnhancedTokenPair`; service accounts receive no
refresh token at all. So `session.StartedAt` is the login time, it survives
rotation (`newSessionID := tokenDoc.SessionUUID`, `auth_service.go:1370`), it is
retained 90 days, and it is a point query on a unique index.

Related nuance to document rather than fix: `/session` mints an access token
without rotating (`handlers/auth_handler.go:1751`, a deliberate anti-replay
split). A user who never keeps a session open past the access-token TTL therefore
never rotates and is logged out at the idle boundary despite using the app.

### ③ The "30 days" claim is stale in three places

`handlers/oauth_state_binding.go:158`, `auth/CLAUDE.md:182`, and
`auth/CLAUDE.md:451` all state that `JWT_REFRESH_TOKEN_EXPIRY` defaults to 30
days. It is `7d` (`config.go:272`, all three compose files, `.env.example:144`).
The `refreshTTL <= 0 → 30d` guard in `NewJWTService` (`jwt_service.go:135`) is
real but unreachable through configuration, because `getEnvAsDuration` never
returns zero for that key.

### ④ `COOKIE_MAX_AGE` is dead configuration

`config.go:299` parses it into `CookieConfig.MaxAge` (`config.go:152`); nothing
reads that field. The real cookie `Max-Age` comes from `refreshCookieMaxAge(jwt)`
(`handlers/oauth_state_binding.go:161`). It is nonetheless declared in all three
compose files and `.env.example:127`, and its comment (`// 24 hours in
milliseconds`) is wrong twice over — `http.Cookie.MaxAge` is in seconds, so the
value would mean roughly 1000 days. An operator reading `.env` believes it
controls session duration.

### ⑤ Nothing purges expired auth rows

`CleanupExpiredTokens`, `CleanupRevokedTokens`, and `TerminateExpiredSessions`
exist and have no callers. `operator_refresh_tokens` / `client_refresh_tokens`
and `operator_sessions` / `client_sessions` carry no TTL index
(`auth/module.go:670`, `:688`) — only the family-fence collections do. The
compliance module does not cover them: `compliance/services/retention.go:23`
sweeps `operator_users` / `client_users` tombstones only. With rotation on every
refresh, the refresh collections grow by one row per refresh and never shrink.

The fix is constrained by a documented rule (`auth/CLAUDE.md:432`): revoked rows
are currently documented as retained for one refresh TTL after revocation. The
safe invariant is narrower and stronger: no row is deleted while its token could
still pass temporal validation, regardless of revocation state.

### ⑥ No server-side validation of any module config value

`ConfigField` carries `Min` / `Max` / `Pattern` (`pkg/sdk/module/types.go:119-121`),
but `ValidateConfigDeclarations` (`pkg/sdk/module/config_validate.go:34`) only
checks that the **schema declaration** is coherent. `UpdateConfig`
(`config_service.go:391`) never compares submitted values against the schema.
Enforcement exists solely client-side in
`frontend-admin/src/pages/admin/modules/ModuleConfigFields.tsx`. `accessTokenTTL`
declares no bounds at all, so an operator can set `9999h` through the API.

`Min` / `Max` are `*int` and cannot express a bound on a duration field.

### ⑦ Session retention is inconsistent and unenforced

Mis-framed in the original audit: `session.ExpiresAt` is a deliberate **retention**
marker, not an auth gate. `services/password_auth_service.go:143-149` says so
explicitly — the row is audit and device history that the risk scorer reads, and
"nothing authenticates off this row." `AuthSessionRetention = 90 * 24h`, used by
both creation sites (`auth_service.go:1259`, `password_auth_service.go:1577`).

The real defects are smaller: `repository/auth_session_repository.go:161` falls
back to **30** days, contradicting the 90 its callers write; and nothing deletes
at either boundary, which folds into ⑤.

### Cross-cutting: divergent duration parsers

`config.parseDuration` (`config.go:508`) accepts a `d` (day) suffix.
`AuthPolicyService` uses bare `time.ParseDuration` at `:187`, `:266`, `:283`, as
does `auth/module.go:1336`. An operator typing `30d` into the admin UI silently
gets the default, and `AUTH_DEVICE_TRUST_DURATION=30d` has never worked.

## Decisions

| # | Decision | Rationale |
|---|---|---|
| A | Restore the documented three-level chain `admin → env → 15m` | Honours `CLAUDE.md:451`; keeps infrastructure-level control for immutable environments and first boot |
| B | Absolute session cap **on by default at 30 days**, both tiers, one config field | Secure out of the box; `session.StartedAt` makes it free of migration code |
| C | Add an optional SDK validation seam, implemented only by `auth` | Avoids an SDK→auth special-case; modules that omit the optional interface retain today's behaviour |
| D | Auth PATCHes reject malformed/out-of-range durations; readers clamp legacy/out-of-band values | Stored and effective settings must agree, while old database state must not make the admin surface unusable |
| E | Missing session row on refresh: **temporarily fail open** only for a clean not-found result; repository errors fail closed | Compatibility telemetry is not permission to accept refreshes during a database failure |
| F | Keep the unreachable `refreshTTL <= 0 → 30d` guard in `NewJWTService` | Behaviour change for direct callers is not worth it; correct the comments instead |
| G | Ship as one ADR plus three sequenced PRs | Three different risk profiles, independent reverts, and a changelog that separates the breaking change from cleanup |

## Design

### PR 1 — Single source of truth for token TTL

**1.1 — Policy signals "unset".** `AuthPolicyService.AccessTokenTTL` returns `0`
when the value is absent or unparsable, instead of substituting 15m. The consuming
code is already correct and does not change: `accessTokenLifetime()` falls through
to `s.accessExpiry`, which `NewJWTService` guarantees is non-zero. There is exactly
one call site (`jwt_service.go:106`), so the blast radius is one line plus the test
that pins the wrong behaviour today (`auth_policy_service_test.go:221`).

`defaultAccessTokenTTL` remains as the guard inside `NewJWTService`, not inside the
policy.

**1.2 — `.env.example` aligns to `15m`.** Without this, 1.1 would silently *lengthen*
the effective access-token TTL from 15 minutes to 1 hour on every install that
copied the shipped example. This is why it cannot be deferred to a later PR.

**1.3 — Denylist TTL covers policy decreases as well as increases.** A provider
of the current policy value is not sufficient. Consider a token minted at 4h,
then an admin lowering the policy to 15m before logout: a denylist entry derived
from the new value would expire after 16m while the old token remained valid.

The service therefore uses the maximum access-token TTL accepted by the auth
policy plus the existing one-minute skew buffer:

```go
const maxAccessTokenTTL = 24 * time.Hour
const sessionRevocationTTL = maxAccessTokenTTL + time.Minute
```

`NewSessionRevocationService` keeps its current signature so forks calling it
directly still compile, but the duration argument becomes deprecated and is not
used to shorten the security window. Its documentation explains the compatibility
choice. A constructor test pins the 24h + 1m TTL. This bounded extra Redis
retention is preferable to storing the latest access-token `exp` per session and
covers every live-policy transition by construction.

The 24-hour ceiling is enforced on the effective lifetime, not only on the admin
field. `NewJWTService` clamps any positive `accessTTL` above
`maxAccessTokenTTL` and logs the source-independent warning; this covers
`JWT_ACCESS_TOKEN_EXPIRY` and direct constructor callers. Zero and negative
values still take the 15-minute constructor default. Consequently the
admin-empty → environment fallback cannot produce a token longer than the
denylist window.

**1.4 — Shared day-aware duration parser.** `parseDuration` moves from
`internal/shared/config` to `internal/shared/utils` as exported `ParseDuration`,
and replaces bare `time.ParseDuration` at `auth_policy_service.go:187,266,283` and
`auth/module.go:1336`. `30d` then works identically in env vars, the admin UI, and
`AUTH_DEVICE_TRUST_DURATION`.

**1.5 — Validate on write, defend on read.**

```go
func clampPersistedDuration(raw string, empty, min, max time.Duration, key string, log *slog.Logger) time.Duration
```

The SDK adds an optional extension seam, following its existing sub-interface
pattern:

```go
type HasConfigValidator interface {
    ValidateConfig(ctx context.Context, mergedValues map[string]string) error
}
```

The registry gives `ModuleConfigService` the validators of registered modules.
Both `UpdateConfig` and `UpdateEnvironmentConfig` invoke the matching validator
before encryption or persistence and map its typed field errors to 422. The
validator receives the merged non-secret values, so later cross-field rules
cannot be bypassed with partial PATCHes. `SetActiveEnvironment` does not reject a
legacy invalid profile: the defensive readers keep it operable and the next edit
must repair it. There is no import or name-based special case from
`pkg/sdk/module` to `internal/core/auth`. Modules that omit the interface retain
current behaviour, so existing fork addons continue to compile and accept the
same values as before.

`AuthModule` implements the interface with a key-specific validator. A malformed
or out-of-range non-empty value returns 422 and is not persisted. The read helper
remains a second line of defence for
values already persisted by an older release or written directly to Mongo: it
saturates numeric out-of-range values, substitutes the field's fallback for
unparsable values, and logs at `Warn` with the key and discarded value.

| Field | Range | Reason for the bound |
|---|---|---|
| `accessTokenTTL` | 1m – 24h | Below a minute the SPA enters a refresh loop; above 24h the Redis denylist grows without useful bound |
| `passwordResetTokenTTL` | 5m – 24h | Below 5 minutes the link dies before the mail is delivered |
| `sessionAbsoluteTTL` (PR 2) | 1h – 89d | Leaves a one-day margin below `AuthSessionRetention`; equality races Mongo TTL deletion at the cap boundary |

The input contract is exhaustive:

| Input | `accessTokenTTL` | `passwordResetTokenTTL` | `sessionAbsoluteTTL` |
|---|---|---|---|
| empty | unset: fall through to env, then 15m | 30m default | 0: cap disabled |
| malformed non-empty PATCH | 422, not persisted | 422, not persisted | 422, not persisted |
| malformed value already in DB | warn, fall through to env | warn, use 30m | warn, use default 720h |
| below minimum PATCH | 422, not persisted | 422, not persisted | 422, not persisted |
| above maximum PATCH | 422, not persisted | 422, not persisted | 422, not persisted |
| out of range already in DB | warn and clamp | warn and clamp | warn and clamp |
| env/direct constructor above maximum | warn and clamp to 24h | n/a | n/a |

Deliberately excluded: `accountLockoutDuration` and `accountLockoutThreshold`.
They do not govern already-issued credentials, and an absurd value there is
self-punishing rather than exploitable.

`Pattern: "^[0-9]+(s|m|h|d)$"` is added to the three duration fields so the
existing client-side guard gives feedback before save. That is a UX aid only —
the enforcement is the auth-specific server-side PATCH validator; generic
`UpdateConfig` still validates nothing.

**1.6 — Tests.**

- `TestAccessTokenTTL_Unset_ReturnsZero` — the policy no longer masks absence
- `TestAccessTokenLifetime_FallsBackToEnvWhenPolicyUnset` — the three-level chain; its absence is why the regression went unnoticed
- `TestRevocationTTL_OutlivesTokensMintedBeforePolicyDecrease` — mint at 4h, lower policy to 15m, revoke, and assert the Redis entry still covers the old token
- `TestSessionRevocationTTL_UsesPolicyMaximum` — pins 24h + 1m independently of the deprecated constructor argument
- `TestAccessTokenTTL_EnvironmentAndConstructorClampToMaximum` — a 48h env/direct constructor value mints for 24h, warns, and remains covered by the denylist
- `TestAuthDurationPatchValidation` — table-driven across all rows of the input contract; rejected values never reach persistence
- `TestConfigUpdate_ModuleValidatorOptional` — modules without the seam are unchanged; auth errors map to 422 before persistence
- `TestEnvironmentConfigUpdate_InvokesModuleValidator` — named profiles cannot bypass validation and invalid input is never persisted
- `TestSetActiveEnvironment_LegacyInvalidValueUsesDefensiveReader` — activation remains recoverable until the operator repairs the stored value
- `TestClampPersistedDuration_SaturatesAndWarns` — legacy/out-of-band values only
- `TestParseDuration_DaySuffix` — parity between env and admin paths

**1.7 — Side effects.** The effective access-token TTL changes on no installation:
15m before (policy), 15m after (aligned example, 15m env default, 15m policy
default). What changes is *which* line of configuration decides it, and that the
env var now works.

An operator who has set `JWT_ACCESS_TOKEN_EXPIRY` by hand in their live
`docker/.env` **will see that value take effect for the first time**. That is the
intent, but it is a real behaviour change for them and the changelog must say so
explicitly, with an instruction to diff that key before upgrading.

### PR 2 — Absolute session cap

**2.1 — Anchor.** `session.StartedAt`. No new field, no backfill, no compatibility
branch. On upgrade, anyone who logged in more than 30 days ago is signed out at
their next refresh — which is what the existing data already says, not something
the code has to arrange.

**2.2 — Enforcement points.** One helper, called from both paths:

```go
func (s *authService) sessionWithinAbsoluteCap(ctx context.Context, sessionUUID string) error
```

- `RefreshTokensWithRiskAssessment` — after the revocation/expiry checks on the row, before `GenerateTokenPairWithAMR` (currently `auth_service.go:1383`)
- `MintAccessTokenFromRefresh` — at the equivalent position (currently `auth_service.go:1514`)

The second is not optional: `/session` mints access tokens without rotating, so a
client calling only that endpoint would bypass the cap indefinitely. This is the
easiest mistake to make in this PR and gets a dedicated test.

**2.3 — Behaviour at expiry.** A real logout, not a bare denial: reuse
`revokeSessionInternal(ctx, sessionUUID, "session_max_age")`
(`auth_service.go:1113`), which already revokes the session's refresh tokens, flips
`isActive=false`, and pushes the sid onto the denylist together. Without it, every
subsequent request would repeat the lookup and the in-flight access token would
stay valid until its natural expiry.

Plus a `SecurityEvent` of type `session_max_age_reached` and a counter
`orkestra_auth_session_cap_expiries_total` on the `Collector` in
`pkg/sdk/metrics/metrics.go`, following `RecordSessionRevocationStoreFailure`. In
production this distinguishes a cap that works from a cap that is signing out too
many people.

The transition is idempotent under concurrent refreshes. Add a repository
operation that changes `isActive:true` to `false` and reports whether this caller
won the transition. Only the winner records the security event and increments the
counter; later callers return the same sentinel without double-counting. Revoking
refresh rows is safe to repeat.

Failure precedence is explicit:

- a repository error while loading the session or revoking durable refresh/session
  state fails closed, never mints credentials, and maps to 503
  `session_enforcement_unavailable` without exposing internals. The cookie is not
  cleared because durable logout is not known to have completed and the client may
  retry when storage recovers;
- only a clean `(nil, nil)` session lookup follows the temporary compatibility
  rule in 2.6;
- a Redis denylist failure after durable revocation returns
  `SessionRevocationDegradedError`, emits the existing failure metric, and still
  clears the refresh cookie; the response uses a generic logout 401 rather than
  claiming a completely recorded cap expiry;
- the cap event/counter is emitted only after durable state is terminated. An
  event-write failure increments
  `orkestra_auth_session_cap_event_failures_total`, logs the operation without PII,
  and cannot restore credentials.

**2.4 — Configuration.**

```go
{
    Key: "sessionAbsoluteTTL", Label: "Maximum session age", Group: "login",
    Description: "Maximum lifetime of a session from login, independent of activity. " +
                 "When it elapses the user must authenticate again. Range 1h–89d; " +
                 "empty disables the cap. Default 720h (30 days).",
    Type: module.FieldDuration, Default: "720h",
    Pattern: "^[0-9]+(s|m|h|d)$",
}
```

One field for both tiers. The default is written `720h` rather than `30d` for
readability against `time.Duration`, though PR 1's shared parser makes `30d` equally
valid input.

The 89-day maximum leaves a full-day margin below `AuthSessionRetention`. Equality
is not safe: at exactly 90 days Mongo's TTL monitor may delete the session before
the refresh path checks the cap, turning an expired session into a compatibility
miss. A test asserts
`maxSessionAbsoluteTTL+sessionRetentionSafetyMargin <= AuthSessionRetention` so
changing either constant breaks the build.

An empty value disables the cap and skips the query entirely — the exit for a fork
that does not want it, without patching code.

**2.5 — Error surface.** New sentinel `ErrSessionMaxAgeReached`, mapped in
`writeRefreshErr` (`handlers/auth_handler.go:2560`) to
`code: "session_max_age_reached"` alongside `refresh_token_replay`.
The same mapper handles the 503 enforcement-unavailable case separately from
credential failures; it must not turn repository outages into misleading 401s.

`frontend-admin/src/store/api/baseApi.ts:189` already intercepts `session_revoked`,
skips the silent-refresh retry, clears state, and redirects. That branch is extended
to the new code, sharing the logic and differing only in the message — "revoked" is
inaccurate for a session that simply reached its maximum age, and the distinction
matters to whoever reads the support ticket. The existing branch uses a literal
English string, so the new one matches that local precedent rather than introducing
i18n into a file that has none.

Both refresh handlers clear the current refresh cookie on
`session_max_age_reached` and on a partially degraded cap logout. Redux cleanup is
not a substitute for expiring the HttpOnly credential; handler tests assert the
expired `Set-Cookie` header.

`frontend-client` needs **no change**: `src/api/client.ts:46` already treats a failed
refresh as logout. `mobile` has no refresh logic and is unaffected.

**2.6 — Missing session row (decision E).** A clean repository not-found fails
open, with a `Warn` log and
`orkestra_auth_session_anchor_anomalies_total{kind="missing"}`, until the first
minor release after ADR-0017. A row whose `StartedAt` and `CreatedAt` are both zero
uses `kind="zero_timestamp"` and follows the same temporary rule. The label is a
closed allowlist with exactly those two values. Any repository/Mongo error fails
closed and uses ordinary error telemetry; it is never collapsed into not-found.

The removal gate is concrete: after at least 30 consecutive production days with
zero increments across every supported environment, change both anomaly cases to
fail closed in the next minor release. The implementation comment names ADR-0017,
the target release, the 30-day criterion, and the tracking issue created by the
implementing PR. If the counter moves, the owner classifies and repairs the data
before restarting the observation window.

A row with `StartedAt` zero but a usable `CreatedAt` uses `CreatedAt` as its
compatibility anchor and increments no anomaly counter.

**2.7 — Tests.**

- `TestRefresh_DeniedPastAbsoluteCap` / `TestMintAccessToken_DeniedPastAbsoluteCap` — the two paths, separately
- `TestRefresh_AllowedWithinAbsoluteCap` — rotation must not extend the cap: two rotations, the second past the boundary, must fail even with a freshly-minted token
- `TestSessionCapExpiry_RevokesFamilyAndSession` — expiry is a logout, not a denial
- `TestSessionCap_DisabledWhenUnset` — empty means no query
- `TestSessionAbsoluteTTLLeavesRetentionMargin` — the strict invariant between cap, one-day safety margin, and retention
- `TestSessionCap_MissingSessionRow` — pins decision E so changing it is deliberate
- `TestSessionCap_RepositoryErrorFailsClosed` — a database failure is not a compatibility miss
- `TestSessionCap_ConcurrentExpiryCountedOnce` — two refreshes produce one transition, event, and metric increment
- `TestSessionCap_RedisDegradedClearsCookie` — durable logout survives denylist degradation and expires the HttpOnly cookie
- boundary cases exactly before, at, and after the cap use an injected clock and no wall-clock sleeps

**2.8 — Side effects.** One indexed point query per refresh, roughly one per active
user per 15 minutes: negligible, and skipped entirely when the cap is disabled.

The interaction with PR 3 is the delicate one and the reason for the ordering:
retention must never be able to delete the anchor of a session still inside the cap.
The strict `sessionAbsoluteTTL + 24h <= AuthSessionRetention` bound guarantees it
and the test enforces it.

Forks inherit the change at their next sync. ADR-0017 is what communicates it; the
changelog marks the release minor with a note about sessions older than 30 days being
signed out.

### PR 3 — Retention and hygiene

**3.1 — Two mechanisms, because the semantics differ.**

For **sessions**, `expiresAt` *is* the retention deadline (`now +
AuthSessionRetention`). A TTL index on that field expresses the intent exactly.

For **refresh tokens**, no row is deleted while its token could still pass temporal
validation, regardless of whether it is active, rotated, or otherwise revoked.
Once `expiresAt` is past, replaying it cannot mint credentials and the row may be
deleted. This replaces the over-broad old wording "one refresh TTL after
revocation" and remains correct even if `JWT_REFRESH_TOKEN_EXPIRY` changes between
restarts. The durable family fence survives independently with its own TTL index.

A TTL index on token `expiresAt` still cannot express the combined rule or report
bounded deletion progress, so refresh-token rows use an application reaper.

**3.2 — The bounded sweep.** `AuthModule` implements `Startable` / `Stoppable`
(`pkg/sdk/module/module.go:151,157`), which it does not today — it embeds
`BaseModule`. The loop follows `logging/module.go:205-227`: ticker, `done` channel,
exit on `ctx.Done()`. One loop covering both tiers by calling the two repositories,
which are separate instances.

`CleanupExpiredTokens` becomes a bounded method accepting `limit` and returning
`(deleted, hasMore, error)`. It selects at most 5,001 UUIDs where
`expiresAt < now`, ordered by `(expiresAt, uuid)`, deletes only the first 5,000,
and derives `hasMore` from the extra row. It never runs `CountDocuments` on the
hot drain path, so deciding the next cadence is bounded by the same batch limit.
The maintenance path has no unbounded `DeleteMany(cutoff)`. One cycle performs
one batch per tier. `CleanupRevokedTokens` is removed rather than wired:
revocation age alone is never a safe deletion criterion, especially after
refresh-TTL changes between restarts. An `(expiresAt, uuid)` index is declared
for both tier collections.

Counts, backlog and duration are logged, and the collector records:

- `orkestra_auth_token_sweep_deleted_total{tier}`;
- `orkestra_auth_token_sweep_backlog_estimate{tier}`;
- `orkestra_auth_token_sweep_duration_seconds{tier}`.

The backlog estimate is initialised with one indexed `CountDocuments` when an
idle pass first discovers `hasMore`, then decremented by successful deletions;
it is reset to zero when `hasMore=false` and recomputed if leadership changes.
It is explicitly an estimate because rows can become eligible during a drain.
The exact count is never recomputed every five minutes. The label set is closed:
`tier ∈ {operator,client}`. ADR-0017 D8 is the schema decision required by
ADR-0002; collection names, UUIDs, configuration values and error strings never
become labels.

The cadence adapts to the backlog rather than the batch doing so. A fixed 6-hour
interval would not drain: 5,000 rows per tier every six hours is 20,000 a day, so a
one-million-row backlog takes fifty days and a five-million-row one takes over eight
months. The loop therefore reschedules on the `hasMore` bit the previous batch
reported — 5 minutes while true, 6 hours once false:

```go
const (
    sweepIdleInterval  = 6 * time.Hour
    sweepDrainInterval = 5 * time.Minute
)
```

At the drain cadence that is 5,000 × 288 cycles ≈ 1.4M rows per tier per day, so the
same million-row backlog clears in under a day and a five-million-row one in under
four — a sustained ~17 deletes per second, which is unremarkable load — while every
individual pass stays bounded at 5,000 and the loop returns to the idle cadence on
its own.

**Cluster ownership.** The lease elects the scheduler leader, not merely the owner
of one pass. On `Start`, the module attempts
`auth:maintenance:token-sweep` with `SET NX`, a random owner token, and a two-minute
TTL. The leader retains it across drain and idle waits, renews every 30 seconds with
compare-and-expire, and releases on `Stop` with compare-and-delete; both operations
use Lua so one replica cannot renew or release another's lease. Loss of renewal
cancels both the current database context and the scheduler loop. Followers retry
leadership after five minutes. Redis unavailability logs a bounded warning and
skips maintenance, never authentication. Holding leadership during the six-hour
idle interval prevents follower retries from accidentally turning an idle cluster
into a five-minute sweep loop, while leader loss still fails over after the lease
expires. Thus 5,000 is the cluster-wide batch bound rather than a per-replica
multiplier.

Adapting the cadence is also what makes an operator-triggered sweep unnecessary.
Any design that drains too slowly has to tell operators to "run extra cycles during
a maintenance window", which requires an admin endpoint or a documented manual
procedure — surface that has to be built, secured, and documented. Self-draining
avoids all of it.

First pass delayed a few minutes after boot so it does not compete with startup.
Still no config field: the two intervals are maintenance constants, not policy, and a
knob here is surface to document and get wrong.

`Start` and `Stop` copy the logging module's lifecycle mutex/cancel/done pattern:
`Start` is idempotent, a stopped module can start again, `Stop` waits for the loop
or its own context, and no second ticker survives a hot enable/disable cycle.

**3.3 — Sessions.** `AuthSessionRetention` moves from `services` to `models`, because
the repository cannot import `services` and both need it. The fallback at
`repository/auth_session_repository.go:161` changes from `30 * 24 * time.Hour` to
`models.AuthSessionRetention`, closing the contradiction with the two callers that
already write 90 days. A TTL index on `expiresAt` is added to `operator_sessions` and
`client_sessions`.

`TerminateExpiredSessions` is removed: it is dead, and under retention semantics it is
also wrong — it flips `isActive=false` at 90 days, when the session has been
irrelevant for months. It is internal to the repository; no fork implements it.

Operational risk: a document with a zero `expiresAt` serialises as a year-1 BSON date
and a TTL index deletes it **immediately**. The write path has always set the field,
but "should not" is not sufficient warrant for an irreversible delete. A pre-flight
count (`countDocuments({expiresAt: {$lt: ISODate("2000-01-01")}})`) on staging and
production is a rollout gate, not a suggestion.

**3.4 — Stale claims (point ③).** Corrected in `handlers/oauth_state_binding.go:158`,
`auth/CLAUDE.md:182`, and `auth/CLAUDE.md:451`. Per decision F the `refreshTTL <= 0 →
30d` guard in `NewJWTService` stays as-is; its comment is rewritten to say it is
unreachable through configuration and applies only to callers constructing the
service with a zero value, in practice tests.

**3.5 — `COOKIE_MAX_AGE` (point ④).** Removed from `config.go:299`, from the
`CookieConfig.MaxAge` field (`config.go:152`), from all three compose files, and from
`.env.example:127`. Verified that `CookieOptions` in `shared/utils/http.go` is fed only
by literals, never by config, so no real cookie is affected. An operator with the
variable in their live `docker/.env` is not broken: it was ignored before and is
ignored after.

**3.6 — Documentation.** Part of the work, not its tail — half of the seven findings
*are* documentation defects, and this repo requires docs to move in the same commit as
the code they describe.

`backend/internal/core/auth/CLAUDE.md` is the most affected: lines 46-47 and 55 on
collection TTLs, 181-182 on the env-var table, 321 on the fixed-upper-bound denylist, 451 on
the lifetime chain, and 479, whose wording conflates invalidation with retention and
must be rewritten with the distinction.

Two further lines state the rule D7 supersedes and must be rewritten in the same
commit, or the file will contradict the shipped behaviour:

- **432** — "Revoked rows must stay in the collection for at least the refresh TTL — do not shorten `CleanupRevokedTokens`'s `olderThan` below that." It names a function PR 3 deletes. Replace with the temporal-validity invariant.
- **438** — "Retain rotated and revoked refresh rows for at least one refresh-token TTL so replay detection remains effective," in the legacy-SID migration note. Same replacement; the migration advice itself still holds.

Then `docs/site/modules/core/auth.mdx` and
`docs/site/architecture/authentication-flow.mdx`, the canonical copy.
`docs/Authentication_flow.md` is **not** touched — the root `CLAUDE.md` marks it as the
drifted duplicate.

Two constraints from the docs pipeline: nothing in this repo's CI builds the site, so
the pages must be rendered locally before merge (recipe in `docs/site/README.md`); and
only a push to `main` publishes.

**3.7 — Tests.**

- Sweep never deletes an unexpired row, regardless of revocation age
- Expired rows may be deleted regardless of revocation state
- A backlog larger than 5,000 deletes no more than one batch per tier and returns `hasMore=true` using the 5,001st row, without `CountDocuments`
- `hasMore=true` schedules the next pass on the drain interval, and false returns to the idle interval — asserted on an injected clock, not by waiting
- Backlog estimation counts once on entry to drain mode, decrements locally, resets at completion, and is not consulted for scheduling
- Two module instances racing for scheduler leadership produce one loop; non-owner renew/release fails, and renewal loss cancels the database context and loop
- The leader retains its lease during idle waits; a follower retries after five minutes and takes over only after leader loss/lease expiry
- TTL index declared on the session collections, modelled on `module_refresh_family_ttl_test.go`, which already asserts the family one
- Retention fallback equals `AuthSessionRetention`
- `Start` is idempotent, `Stop` exits on cancellation, and Start→Stop→Start creates exactly one live loop

**3.8 — Side effects.** Session TTL deletion may still be substantial on the first
boot and must be measured on staging. Refresh-token history drains in bounded
5,000-row batches per tier on the 5-minute drain cadence — roughly 1.4M rows per
tier per day — so even a large accumulated backlog clears in days, after which the
loop settles back to its 6-hour idle interval. Operators watch
`orkestra_auth_token_sweep_backlog_estimate{tier}` together with `hasMore`/cadence
logs to see the drain finish; no manual
intervention or interval change is expected.

New queries need the `//tenantscope:allow` annotations used by the existing queries in
the same repository, or `make ci-backend` fails the tenantscope check. No routes
change, so `openapi-check` is unaffected.

## Sequencing

PR 1 → PR 2 → PR 3, and the order is load-bearing in both directions.

PR 1 must precede PR 2 because the cap reads the TTL from the source PR 1 unifies;
reversed, the cap would be built on top of the denylist split-brain.

PR 3 must follow PR 2 because retention must not be able to delete an anchor the cap
still depends on. The strict
`sessionAbsoluteTTL + sessionRetentionSafetyMargin <= AuthSessionRetention`
bound is what makes the pair safe, and it is introduced in PR 2.

## Rollout

1. Count zero/near-zero `expiresAt` session documents and the eligible token-sweep backlog on staging **and** production before deploying PR 3.
2. Time the `(expiresAt, uuid)` index build on a production-sized copy before deploying PR 3. Building it on a multi-million-row collection is itself an operational event: Mongo 8 builds with minimal locking, but on a busy replica set the duration and its replication lag belong in the maintenance window, not discovered mid-deploy.
3. Deploy PR 1 to staging; verify a revoked session stays revoked after raising `accessTokenTTL` above the old denylist TTL. This is the regression that motivated the work and must be confirmed against a running stack, not only in unit tests.
4. Diff the live `docker/.env` against `.env.example` for `JWT_ACCESS_TOKEN_EXPIRY` before each environment's upgrade — the value may activate for the first time.
5. Deploy PR 2 to staging; confirm sessions older than the cap are signed out and that `orkestra_auth_session_cap_expiries_total` moves, then settles.
6. Deploy PR 3 to staging; verify each application batch stays at or below 5,000, confirm the loop switches from the drain interval back to the idle one when the backlog reaches zero, and measure duration and drain time before promoting.
7. Review `orkestra_auth_session_anchor_anomalies_total` for at least 30 consecutive production days. If it remains zero everywhere, implement the already-filed fail-closed follow-up in the next minor release; otherwise repair the data cause and restart the window.

## Out of scope

- **Generic schema-driven config validation in the SDK.** Decision C adds only an optional callback seam and keeps policy inside `auth`. Teaching `UpdateConfig` to interpret every schema constraint — including new `MinDuration` / `MaxDuration` fields — remains a broader contract change and deserves its own ADR.
- **Per-tier session caps.** One field covers both tiers. Splitting it follows the `loginEnabledAdmin` / `loginEnabledClient` precedent if a need appears.
- **Idle timeout configurable independently of the refresh TTL.** They are the same control today, and separating them means a second expiry on the refresh row with no demonstrated demand.
- **`/session` extending the idle window.** The non-rotating bootstrap is a deliberate anti-replay split; changing it would reopen the race it was built to close.
- **Lockout duration/threshold bounds.** Excluded from the auth-specific duration validation per PR 1.5.
