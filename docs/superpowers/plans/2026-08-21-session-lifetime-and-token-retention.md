# Session Lifetime, Token TTL Sourcing, and Auth Retention Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Bound the total age of an authenticated session at 30 days by default, collapse access-token lifetime onto one resolution chain whose revocation denylist outlives every token it can mint, and purge expired authentication state with mechanisms matched to each collection's semantics.

**Architecture:** Three sequenced PRs against `backend/internal/core/auth`. PR 1 repairs the `admin → env → 15m` chain, pins the Redis revocation TTL to a fixed 24h+1m upper bound, and adds an *optional* SDK config-validation seam that only `auth` implements. PR 2 enforces an absolute cap anchored on the existing `session.StartedAt` — no schema change, no backfill — on both the rotating and non-rotating refresh paths, terminating the session rather than denying the request. PR 3 gives session documents a TTL index and refresh-token rows a bounded, Redis-lease-elected application reaper with backlog telemetry.

**Tech Stack:** Go 1.25.13, Huma v2, MongoDB 8 (TTL indexes, `UpdateOne` CAS), Redis 8.2 (`SET NX` + Lua compare-and-swap lease), Prometheus `client_golang`, React 19 + RTK Query, Vitest/MSW.

**Spec:** `docs/superpowers/specs/2026-08-21-session-lifetime-design.md`
**ADR:** `docs/adr/0017-session-lifetime-and-token-retention.md`

## Global Constraints

- **Sequencing is load-bearing in both directions.** PR 1 → PR 2 → PR 3. PR 2 must not start before PR 1 lands (the cap reads the TTL source PR 1 unifies); PR 3 must not start before PR 2 lands (retention must never delete an anchor the cap still needs, and the `sessionAbsoluteTTL + sessionRetentionSafetyMargin <= AuthSessionRetention` bound is introduced in PR 2).
- **Three branches off `dev`**, one per PR: `feat/adr-0017-pr1-token-ttl-source`, `feat/adr-0017-pr2-session-cap`, `feat/adr-0017-pr3-auth-retention`. Each PR is independently revertible.
- **Docs move in the same commit as the code they describe** — every task that changes behaviour also updates `backend/internal/core/auth/CLAUDE.md` (and, in PR 3, the `docs/site` pages) in that same commit.
- **Exact bounds, copied verbatim from the spec:** `accessTokenTTL` 1m–24h; `passwordResetTokenTTL` 5m–24h; `sessionAbsoluteTTL` 1h–89d, default `720h`, empty disables; denylist entry TTL `24h + 1m`; sweep batch 5,000 rows per tier per cycle (select 5,001 to derive `hasMore`); drain interval 5m, idle interval 6h; lease TTL 2m, renew every 30s, follower retry 5m; session retention 90d, safety margin 24h.
- **Closed metric label schema (ADR-0017 D8, extends ADR-0002):** token-sweep metrics carry only `tier ∈ {operator,client}`; anchor-anomaly metrics carry only `kind ∈ {missing,zero_timestamp}`. UUIDs, collection names, configuration values, and error strings never become labels.
- **New Mongo queries need `//tenantscope:allow`** with the same rationale comment as the existing queries in that repository, or `make ci-backend` fails the tenantscope check.
- **No new `go.mod` / `replace` / `go.work`** (ADR-0006 forbidden pattern).
- **Never expose internals in API responses.** Repository failures map to a 503 with a fixed code, never a wrapped error string.
- Run `cd backend && go test ./internal/core/auth/... ./pkg/sdk/module/... ./internal/shared/... -count=1` before each commit; `make ci-backend` before opening each PR.

## File Structure

**PR 1**

| File | Responsibility |
|---|---|
| `backend/internal/shared/utils/duration.go` (create) | Exported day-aware `ParseDuration`, the single parser env vars *and* the admin UI share |
| `backend/internal/shared/config/config.go` (modify) | `parseDuration` delegates to `utils.ParseDuration`; no behaviour change |
| `backend/internal/core/auth/services/auth_policy_service.go` (modify) | `AccessTokenTTL` reports "unset" as `0`; all duration reads go through the day-aware parser and a defensive clamp |
| `backend/internal/core/auth/services/auth_duration_bounds.go` (create) | `MaxAccessTokenTTL`, per-key bounds, `clampPersistedDuration` — the read-time second line of defence |
| `backend/internal/core/auth/services/jwt_service.go` (modify) | Clamp any positive `accessTTL` above `MaxAccessTokenTTL` at construction |
| `backend/internal/core/auth/services/session_revocation_service.go` (modify) | Fixed `24h + 1m` entry TTL; constructor duration argument deprecated |
| `backend/pkg/sdk/module/config_validator.go` (create) | `HasConfigValidator` optional interface + `ConfigValidationError` typed field error |
| `backend/pkg/sdk/module/config_service.go` (modify) | Invoke the module validator on merged non-secret values before encryption/persistence, on both update paths |
| `backend/pkg/sdk/module/handler.go` (modify) | Map `*ConfigValidationError` to 422 on both PATCH surfaces |
| `backend/internal/core/auth/config_validation.go` (create) | `AuthModule.ValidateConfig` — the auth-specific PATCH-boundary bounds |
| `backend/internal/core/auth/module.go` (modify) | `Pattern` on the duration config fields; revocation service constructed without a policy-derived TTL |
| `docker/.env.example` (modify) | `JWT_ACCESS_TOKEN_EXPIRY` aligned to `15m` |

**PR 2**

| File | Responsibility |
|---|---|
| `backend/internal/core/auth/services/session_cap.go` (create) | Cap constants, `SessionAbsoluteTTL` policy accessor, the anchor resolver, the cap helper and its sentinels |
| `backend/internal/core/auth/repository/auth_session_repository.go` (modify) | `ExpireSessionForMaxAge` — the idempotent `isActive:true → false` transition that names one winner |
| `backend/internal/core/auth/models/collections.go` (modify) | `RevokeReasonSessionMaxAge` |
| `backend/pkg/sdk/metrics/metrics.go` (modify) | Three new families: cap expiries, cap event failures, anchor anomalies |
| `backend/internal/core/auth/services/auth_service.go` (modify) | Call the helper from both refresh paths |
| `backend/internal/core/auth/handlers/auth_handler.go` (modify) | 401 `session_max_age_reached` / 503 `session_enforcement_unavailable`; expire the HttpOnly cookie on terminal cap failures |
| `frontend-admin/src/store/api/baseApi.ts` (modify) | Extend the existing `session_revoked` branch to the new code with its own message |

**PR 3**

| File | Responsibility |
|---|---|
| `backend/internal/core/auth/models/collections.go` (modify) | `AuthSessionRetention` moves here so the repository can reference it |
| `backend/internal/core/auth/repository/auth_session_repository.go` (modify) | Retention fallback = 90d; `TerminateExpiredSessions` removed |
| `backend/internal/core/auth/repository/refresh_token_repository.go` (modify) | Bounded `CleanupExpiredTokens(limit)`; `CountExpiredTokens`; `CleanupRevokedTokens` removed |
| `backend/internal/shared/database/redis.go` (modify) | `SetNX` + `Eval` on the adapter |
| `backend/internal/core/auth/services/maintenance_lease.go` (create) | `LeaseRedisClient` narrow extension + the compare-and-swap Redis lease |
| `backend/internal/core/auth/maintenance.go` (create) | `AuthModule.Start`/`Stop` + the adaptive-cadence sweep scheduler |
| `backend/pkg/sdk/metrics/metrics.go` (modify) | Token-sweep deleted / backlog-estimate / duration families |
| `backend/internal/shared/config/config.go`, `docker/*.yml`, `docker/.env.example` (modify) | `COOKIE_MAX_AGE` removed |
| `backend/internal/core/auth/CLAUDE.md`, `docs/site/modules/core/auth.mdx`, `docs/site/architecture/authentication-flow.mdx` (modify) | The three documentation defects + the superseded retention rules |

---

# PR 1 — Single source of truth for token TTL

Branch: `feat/adr-0017-pr1-token-ttl-source` off `dev`.

### Task 1: Shared day-aware duration parser

`config.parseDuration` accepts a `d` suffix; `AuthPolicyService` and `auth/module.go` use bare `time.ParseDuration`. So `30d` typed into the admin UI silently falls back to a default, and `AUTH_DEVICE_TRUST_DURATION=30d` has never worked. One exported parser fixes both.

`internal/shared/utils` imports nothing from `internal/` today, so moving the parser there creates no cycle.

**Files:**
- Create: `backend/internal/shared/utils/duration.go`
- Create: `backend/internal/shared/utils/duration_test.go`
- Modify: `backend/internal/shared/config/config.go:508-524` (the `parseDuration` body)
- Modify: `backend/internal/core/auth/services/auth_policy_service.go` (`:187`, `:266`, `:283`)
- Modify: `backend/internal/core/auth/module.go:1330-1344` (`parseDurationEnv`)

**Interfaces:**
- Produces: `utils.ParseDuration(raw string) (time.Duration, bool)` — every later task in every PR parses admin-supplied durations with this.

- [ ] **Step 1: Write the failing test**

Create `backend/internal/shared/utils/duration_test.go`:

```go
package utils

import (
	"testing"
	"time"
)

func TestParseDuration_DaySuffix(t *testing.T) {
	cases := []struct {
		raw  string
		want time.Duration
		ok   bool
	}{
		{"15m", 15 * time.Minute, true},
		{"1h", time.Hour, true},
		{"30d", 30 * 24 * time.Hour, true},
		{"720h", 720 * time.Hour, true},
		{"0.5d", 12 * time.Hour, true},
		{"1d12h", 0, false}, // compound day forms stay unsupported, not half-supported
		{"forever", 0, false},
		{"", 0, false},
		{"   ", 0, false},
	}
	for _, tc := range cases {
		t.Run(tc.raw, func(t *testing.T) {
			got, ok := ParseDuration(tc.raw)
			if ok != tc.ok {
				t.Fatalf("ParseDuration(%q) ok = %v, want %v", tc.raw, ok, tc.ok)
			}
			if ok && got != tc.want {
				t.Errorf("ParseDuration(%q) = %v, want %v", tc.raw, got, tc.want)
			}
		})
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd backend && go test ./internal/shared/utils/ -run TestParseDuration_DaySuffix -count=1`
Expected: FAIL — `undefined: ParseDuration`

- [ ] **Step 3: Write the parser**

Create `backend/internal/shared/utils/duration.go`:

```go
package utils

import (
	"strconv"
	"strings"
	"time"
)

// ParseDuration accepts everything time.ParseDuration does, plus a
// trailing "d" for days. Only a bare "<number>d" is special-cased;
// compound forms ("1d12h") stay unsupported rather than half-supported,
// so a value either parses exactly or is rejected.
//
// This is the single parser for durations that reach Orkestra from a
// human: environment variables, module config values typed into the
// admin UI, and defaults declared in ConfigSchema. Before ADR-0017 the
// env path accepted "30d" and the admin path did not, so the same
// string meant two different things depending on where it was typed.
func ParseDuration(raw string) (time.Duration, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return 0, false
	}
	if days, ok := strings.CutSuffix(raw, "d"); ok {
		n, err := strconv.ParseFloat(days, 64)
		if err != nil {
			return 0, false
		}
		return time.Duration(n * float64(24*time.Hour)), true
	}
	d, err := time.ParseDuration(raw)
	if err != nil {
		return 0, false
	}
	return d, true
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd backend && go test ./internal/shared/utils/ -run TestParseDuration_DaySuffix -count=1`
Expected: PASS

- [ ] **Step 5: Delegate the config parser to it**

In `backend/internal/shared/config/config.go`, replace the whole `parseDuration` function body with a delegation, and add the import:

```go
// parseDuration delegates to utils.ParseDuration so environment
// variables and admin-UI values are read by one parser. See ADR-0017.
func parseDuration(raw string) (time.Duration, bool) {
	return utils.ParseDuration(raw)
}
```

Add `"github.com/orkestra/backend/internal/shared/utils"` to the import block. Delete the now-unused `strconv`/`strings` imports only if nothing else in the file uses them (they are used elsewhere — check with `go build` rather than by eye).

- [ ] **Step 6: Adopt it in the auth policy service and module env reader**

In `backend/internal/core/auth/services/auth_policy_service.go`, replace each of the three `time.ParseDuration(v)` calls (in `AccessTokenTTL`, `PasswordResetTokenTTL`, `LockoutDuration`) with:

```go
	d, ok := utils.ParseDuration(v)
	if !ok || d <= 0 {
		return defaultLockoutDuration // the field's own default
	}
	return d
```

Add `"github.com/orkestra/backend/internal/shared/utils"` to the imports. (Task 2 rewrites `AccessTokenTTL` again — this step only removes the parser divergence.)

In `backend/internal/core/auth/module.go`, `parseDurationEnv`:

```go
	d, ok := utils.ParseDuration(raw)
	if !ok || d <= 0 {
		slog.Default().Warn("auth: malformed duration env var, using default",
			slog.String("key", key),
			slog.String("value", raw),
			slog.String("default", fallback.String()))
		return fallback
	}
	return d
```

`internal/shared/utils` is already imported by the auth handlers; add it to `module.go`'s import block.

- [ ] **Step 7: Verify nothing regressed**

Run: `cd backend && go build ./... && go test ./internal/shared/... ./internal/core/auth/... -count=1`
Expected: PASS

- [ ] **Step 8: Update the module contract doc**

In `backend/internal/core/auth/CLAUDE.md`, in the env-var table row for `AUTH_DEVICE_TRUST_DURATION` (and anywhere durations are described), note that duration values accept a bare `d` suffix (`30d`) in both env vars and the admin UI, parsed by `internal/shared/utils.ParseDuration`.

- [ ] **Step 9: Commit**

```bash
cd /home/tore/orkestra
git add backend/internal/shared/utils/duration.go backend/internal/shared/utils/duration_test.go \
        backend/internal/shared/config/config.go \
        backend/internal/core/auth/services/auth_policy_service.go \
        backend/internal/core/auth/module.go \
        backend/internal/core/auth/CLAUDE.md
git commit -m "refactor(auth): single day-aware duration parser for env and admin values

ADR-0017. 30d typed into the admin UI silently fell back to a default
and AUTH_DEVICE_TRUST_DURATION=30d never worked, because config and the
auth policy used two different parsers."
```

---

### Task 2: Policy reports "unset", readers clamp defensively

`AuthPolicyService.AccessTokenTTL` never returns `0`, so `jwtService.accessTokenLifetime`'s documented fall-through to `JWT_ACCESS_TOKEN_EXPIRY` is dead code and the env var has no effect on any deployment with the policy wired — which is every deployment. Returning `0` for "unset" restores the chain. The consuming code at `jwt_service.go:106` is already correct and does not change.

The same task adds the read-time clamp, because once the env var becomes reachable it must not be able to mint a token that outlives the fixed denylist window Task 3 introduces.

**Files:**
- Create: `backend/internal/core/auth/services/auth_duration_bounds.go`
- Create: `backend/internal/core/auth/services/auth_duration_bounds_test.go`
- Modify: `backend/internal/core/auth/services/auth_policy_service.go` (`AccessTokenTTL`, `PasswordResetTokenTTL`)
- Modify: `backend/internal/core/auth/services/auth_policy_service_test.go:196-226` (the cases that pin today's wrong behaviour)
- Modify: `backend/internal/core/auth/services/jwt_service.go:130-150` (`NewJWTService`)
- Create: `backend/internal/core/auth/services/token_ttl_chain_test.go`

**Interfaces:**
- Consumes: `utils.ParseDuration` (Task 1).
- Produces: `services.MaxAccessTokenTTL = 24 * time.Hour` (exported — Task 3 and Task 5 both bound against it).
- Produces: `services.minAccessTokenTTL`, `services.minPasswordResetTokenTTL`, `services.maxPasswordResetTokenTTL` (package-private).
- Produces: `clampPersistedDuration(raw string, fallback, min, max time.Duration, key string, log *slog.Logger) time.Duration` — Task 7 reuses it for `sessionAbsoluteTTL`.
- Produces: `AuthPolicyService.AccessTokenTTL` now returns `0` when unset. `jwtService.accessTokenLifetime` is the only call site.

- [ ] **Step 1: Write the failing tests**

Create `backend/internal/core/auth/services/auth_duration_bounds_test.go`:

```go
package services

import (
	"log/slog"
	"testing"
	"time"
)

func TestClampPersistedDuration_SaturatesAndWarns(t *testing.T) {
	log := slog.New(slog.DiscardHandler)
	cases := []struct {
		name     string
		raw      string
		fallback time.Duration
		min, max time.Duration
		want     time.Duration
	}{
		{"in range passes through", "2h", 0, time.Minute, 24 * time.Hour, 2 * time.Hour},
		{"day suffix parses", "1d", 0, time.Minute, 24 * time.Hour, 24 * time.Hour},
		{"below min saturates up", "30s", 0, time.Minute, 24 * time.Hour, time.Minute},
		{"above max saturates down", "9999h", 0, time.Minute, 24 * time.Hour, 24 * time.Hour},
		{"malformed uses fallback", "forever", 30 * time.Minute, 5 * time.Minute, 24 * time.Hour, 30 * time.Minute},
		{"zero uses fallback", "0s", 30 * time.Minute, 5 * time.Minute, 24 * time.Hour, 30 * time.Minute},
		{"negative uses fallback", "-5m", 30 * time.Minute, 5 * time.Minute, 24 * time.Hour, 30 * time.Minute},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := clampPersistedDuration(tc.raw, tc.fallback, tc.min, tc.max, "testKey", log)
			if got != tc.want {
				t.Errorf("clampPersistedDuration(%q) = %v, want %v", tc.raw, got, tc.want)
			}
		})
	}
}
```

Create `backend/internal/core/auth/services/token_ttl_chain_test.go`:

```go
package services

import (
	"context"
	"testing"
	"time"
)

// The three-level chain is admin accessTokenTTL → JWT_ACCESS_TOKEN_EXPIRY
// → 15m. Its middle level was unreachable because the policy substituted
// the 15m default for "unset", so the fall-through in accessTokenLifetime
// could never fire. The absence of this test is why the regression went
// unnoticed. ADR-0017 D5.
func TestAccessTokenTTL_Unset_ReturnsZero(t *testing.T) {
	for _, raw := range []string{"", "   ", "forever", "0s", "-5m"} {
		p := newPolicy(map[string]string{"accessTokenTTL": raw})
		if got := p.AccessTokenTTL(context.Background()); got != 0 {
			t.Errorf("accessTokenTTL=%q → %v, want 0 (unset, so the env level can be consulted)", raw, got)
		}
	}
	if got := newPolicy(nil).AccessTokenTTL(context.Background()); got != 0 {
		t.Errorf("absent key → %v, want 0", got)
	}
}

func TestAccessTokenLifetime_FallsBackToEnvWhenPolicyUnset(t *testing.T) {
	priv := testRSAKey()
	const envTTL = 42 * time.Minute
	svc := NewJWTService(priv, &priv.PublicKey, "test", envTTL, 7*24*time.Hour)

	// No policy wired at all.
	if got := svc.AccessTokenTTL(context.Background()); got != envTTL {
		t.Errorf("no policy: %v, want the env-derived %v", got, envTTL)
	}

	// Policy wired but the key is unset — the env value must still win.
	svc.(*jwtService).SetPolicy(newPolicy(map[string]string{"accessTokenTTL": ""}))
	if got := svc.AccessTokenTTL(context.Background()); got != envTTL {
		t.Errorf("policy unset: %v, want the env-derived %v", got, envTTL)
	}

	// Policy set — it wins over the env value.
	svc.(*jwtService).SetPolicy(newPolicy(map[string]string{"accessTokenTTL": "5m"}))
	if got := svc.AccessTokenTTL(context.Background()); got != 5*time.Minute {
		t.Errorf("policy set: %v, want 5m", got)
	}
}

func TestAccessTokenTTL_EnvironmentAndConstructorClampToMaximum(t *testing.T) {
	priv := testRSAKey()
	svc := NewJWTService(priv, &priv.PublicKey, "test", 48*time.Hour, 7*24*time.Hour)
	if got := svc.AccessTokenTTL(context.Background()); got != MaxAccessTokenTTL {
		t.Errorf("48h constructor value = %v, want it clamped to %v so the denylist window still covers it", got, MaxAccessTokenTTL)
	}
}

func TestAccessTokenTTL_PersistedOutOfRangeIsClamped(t *testing.T) {
	if got := newPolicy(map[string]string{"accessTokenTTL": "9999h"}).AccessTokenTTL(context.Background()); got != MaxAccessTokenTTL {
		t.Errorf("legacy 9999h = %v, want %v", got, MaxAccessTokenTTL)
	}
	if got := newPolicy(map[string]string{"accessTokenTTL": "10s"}).AccessTokenTTL(context.Background()); got != minAccessTokenTTL {
		t.Errorf("legacy 10s = %v, want %v", got, minAccessTokenTTL)
	}
}
```

- [ ] **Step 2: Run to verify failure**

Run: `cd backend && go test ./internal/core/auth/services/ -run 'TestClampPersistedDuration|TestAccessTokenTTL|TestAccessTokenLifetime' -count=1`
Expected: FAIL — `undefined: clampPersistedDuration`, `undefined: MaxAccessTokenTTL`, `undefined: minAccessTokenTTL`

- [ ] **Step 3: Add the bounds and the defensive reader**

Create `backend/internal/core/auth/services/auth_duration_bounds.go`:

```go
package services

import (
	"log/slog"
	"time"

	"github.com/orkestra/backend/internal/shared/utils"
)

// Bounds on the admin-managed durations that govern already-issued
// credentials. ADR-0017 D6 enforces these at the PATCH boundary (422);
// the readers below are the second line of defence for values persisted
// by an older release or written directly to Mongo, where rejecting the
// value would lock the operator out of the admin UI instead of fixing it.
const (
	// MaxAccessTokenTTL is the ceiling on the EFFECTIVE access-token
	// lifetime — not merely on the admin field. The Redis revocation
	// denylist stores every entry for this value plus a clock-skew
	// minute, so a token that could outlive it would be accepted again
	// after its own revocation entry expired. The bound therefore also
	// applies to JWT_ACCESS_TOKEN_EXPIRY and to direct NewJWTService
	// callers. ADR-0017 D5.
	MaxAccessTokenTTL = 24 * time.Hour
	// minAccessTokenTTL: below a minute the SPA enters a refresh loop.
	minAccessTokenTTL = time.Minute

	// minPasswordResetTokenTTL: below five minutes the link dies before
	// the mail is delivered.
	minPasswordResetTokenTTL = 5 * time.Minute
	maxPasswordResetTokenTTL = 24 * time.Hour
)

// clampPersistedDuration resolves a duration that is ALREADY persisted.
// `fallback` is returned when raw is non-empty but unparsable or
// non-positive; values outside [min,max] saturate to the nearest bound.
// Every correction logs at Warn with the key and the discarded value so
// an operator can find and repair the stored data.
//
// Empty input is deliberately the CALLER's business: the three auth
// duration keys give emptiness three different meanings (accessTokenTTL
// falls through to the environment, passwordResetTokenTTL uses 30m,
// sessionAbsoluteTTL disables the cap), and folding that into one
// signature would hide the distinction the input contract turns on.
func clampPersistedDuration(raw string, fallback, min, max time.Duration, key string, log *slog.Logger) time.Duration {
	if log == nil {
		log = slog.Default()
	}
	d, ok := utils.ParseDuration(raw)
	if !ok || d <= 0 {
		log.Warn("auth: unusable persisted duration, using default",
			slog.String("key", key),
			slog.String("value", raw),
			slog.String("using", fallback.String()))
		return fallback
	}
	if d < min {
		log.Warn("auth: persisted duration below minimum, clamping",
			slog.String("key", key),
			slog.String("value", raw),
			slog.String("using", min.String()))
		return min
	}
	if d > max {
		log.Warn("auth: persisted duration above maximum, clamping",
			slog.String("key", key),
			slog.String("value", raw),
			slog.String("using", max.String()))
		return max
	}
	return d
}
```

- [ ] **Step 4: Make the policy report "unset"**

In `backend/internal/core/auth/services/auth_policy_service.go`, replace `AccessTokenTTL` and `PasswordResetTokenTTL` entirely:

```go
// AccessTokenTTL returns the admin-managed access-token lifetime, or 0
// when the value is absent. Zero means UNSET, not "use the default":
// jwtService.accessTokenLifetime falls through to the env-derived
// s.accessExpiry on zero, which is the documented
// `admin → JWT_ACCESS_TOKEN_EXPIRY → 15m` chain. Substituting the 15m
// default here is what made the middle level unreachable. The 15m guard
// lives in NewJWTService, which is the level that owns it. ADR-0017 D5.
func (s *AuthPolicyService) AccessTokenTTL(ctx context.Context) time.Duration {
	if s == nil || s.cs == nil {
		return 0
	}
	v := strings.TrimSpace(s.cs.GetValue(ctx, "auth", "accessTokenTTL"))
	if v == "" {
		return 0
	}
	return clampPersistedDuration(v, 0, minAccessTokenTTL, MaxAccessTokenTTL, "accessTokenTTL", slogDefault())
}

// PasswordResetTokenTTL returns the admin-managed lifetime of the
// reset-password email token. Empty means "use the 30m default"; a
// persisted out-of-range or unparsable value is clamped and warned
// rather than rejected, so old data cannot make the admin UI unusable.
func (s *AuthPolicyService) PasswordResetTokenTTL(ctx context.Context) time.Duration {
	if s == nil || s.cs == nil {
		return defaultPasswordResetTokenTTL
	}
	v := strings.TrimSpace(s.cs.GetValue(ctx, "auth", "passwordResetTokenTTL"))
	if v == "" {
		return defaultPasswordResetTokenTTL
	}
	return clampPersistedDuration(v, defaultPasswordResetTokenTTL, minPasswordResetTokenTTL, maxPasswordResetTokenTTL, "passwordResetTokenTTL", slogDefault())
}
```

`clampPersistedDuration` returns `0` for `accessTokenTTL` only when the stored value is unusable — the `fallback` argument is `0`, which is exactly "unset", so a corrupted stored value falls through to the environment level rather than to a hardcoded 15m.

- [ ] **Step 5: Clamp the constructor level**

In `backend/internal/core/auth/services/jwt_service.go`, in `NewJWTService`, replace the `accessTTL` guard:

```go
	if accessTTL <= 0 {
		accessTTL = 15 * time.Minute
	}
	if accessTTL > MaxAccessTokenTTL {
		// The Redis revocation denylist stores entries for
		// MaxAccessTokenTTL + 1m. A longer token would outlive its own
		// revocation entry and become valid again after logout. Clamp
		// rather than reject: this level is fed by JWT_ACCESS_TOKEN_EXPIRY
		// and by direct callers, neither of which can surface a 422.
		// ADR-0017 D5.
		slog.Default().Warn("auth: access-token lifetime above maximum, clamping",
			slog.String("value", accessTTL.String()),
			slog.String("using", MaxAccessTokenTTL.String()))
		accessTTL = MaxAccessTokenTTL
	}
```

Leave the `refreshTTL <= 0 → 30d` guard exactly as it is (decision F) but rewrite its comment in Task 6.
`NewJWTServiceWithAudience` delegates the same guards — verify it does; if it duplicates them, apply the clamp there too.

- [ ] **Step 6: Fix the test that pins today's behaviour**

In `backend/internal/core/auth/services/auth_policy_service_test.go`, the `TestAccessTokenTTL` table asserts `15 * time.Minute` for unset/empty/malformed/zero/negative. Change those five `want` values to `0` and rename the cases (`"unset reports zero"`, …). Change `TestAccessTokenTTL_NilService_LegacyDefault` to assert `0` and rename it `TestAccessTokenTTL_NilService_ReportsUnset`. Leave `TestPasswordResetTokenTTL` alone except for adding two rows: `{"below minimum clamps", map[string]string{"passwordResetTokenTTL": "1m"}, 5 * time.Minute}` and `{"above maximum clamps", map[string]string{"passwordResetTokenTTL": "72h"}, 24 * time.Hour}`.

- [ ] **Step 7: Run to verify pass**

Run: `cd backend && go test ./internal/core/auth/services/ -count=1`
Expected: PASS

- [ ] **Step 8: Commit**

```bash
cd /home/tore/orkestra
git add backend/internal/core/auth/services/
git commit -m "fix(auth): restore the admin -> env -> 15m access-token chain

AuthPolicyService.AccessTokenTTL substituted the 15m default for 'unset',
so accessTokenLifetime's documented fall-through to JWT_ACCESS_TOKEN_EXPIRY
was dead and the env var had no effect on any deployment. It now reports 0
for unset. Effective lifetimes are clamped to 24h at every level so no
token can outlive the revocation denylist window. ADR-0017 D5/D6."
```

---

### Task 3: The revocation denylist outlives every token the policy can mint

`module.go:805-808` builds the denylist with `cfg.Auth.JWT.AccessTokenExpiry` — the env value, read **once at boot** — while access tokens take their lifetime from the admin policy, read live on every mint. Raising `accessTokenTTL` to `4h` in the admin UI produces 4-hour tokens whose `auth:revoked:session:<sid>` entry still expires after 16 minutes; once it does, tokens belonging to a revoked session are accepted again, silently. Logout, change-password, and `TerminateAllSessions` are all served by that denylist.

A provider of the *current* policy value is not sufficient: a token minted at 4h, followed by an admin lowering the policy to 15m before logout, would get a 16-minute entry while the 4-hour token is still valid. The fixed upper bound closes the increase and the decrease case at once.

**Files:**
- Modify: `backend/internal/core/auth/services/session_revocation_service.go:53-85`
- Modify: `backend/internal/core/auth/services/session_revocation_service_test.go`
- Modify: `backend/internal/core/auth/module.go:803-809`

**Interfaces:**
- Consumes: `services.MaxAccessTokenTTL` (Task 2).
- Produces: `services.sessionRevocationTTL = MaxAccessTokenTTL + time.Minute`. `NewSessionRevocationService` keeps its exact signature; the duration argument is deprecated and ignored.

- [ ] **Step 1: Write the failing tests**

Append to `backend/internal/core/auth/services/session_revocation_service_test.go`:

```go
// The denylist entry must outlive every access token the policy is
// permitted to mint, in BOTH directions of a policy change. Deriving the
// entry TTL from the live policy value fails the decrease case: a token
// minted at 4h, then a policy lowered to 15m, then a logout, would store
// a 16-minute entry while the 4-hour token is still valid. ADR-0017 D5.
func TestSessionRevocationTTL_UsesPolicyMaximum(t *testing.T) {
	for _, constructorArg := range []time.Duration{0, time.Minute, 15 * time.Minute, 4 * time.Hour, 48 * time.Hour} {
		fake := newFakeRevocationRedis()
		svc := newSessionRevocationService(fake, constructorArg, slog.New(slog.DiscardHandler), nil)
		if err := svc.Revoke(context.Background(), "sid-1", "logout"); err != nil {
			t.Fatalf("Revoke: %v", err)
		}
		want := MaxAccessTokenTTL + time.Minute
		if got := fake.ttlFor("auth:revoked:session:sid-1"); got != want {
			t.Errorf("constructor arg %v produced entry TTL %v, want the fixed %v", constructorArg, got, want)
		}
	}
}

func TestRevocationTTL_OutlivesTokensMintedBeforePolicyDecrease(t *testing.T) {
	// Mint at 4h, lower the policy to 15m, then revoke. The entry must
	// still cover the 4h token that is out there.
	fake := newFakeRevocationRedis()
	svc := newSessionRevocationService(fake, 4*time.Hour, slog.New(slog.DiscardHandler), nil)
	// (Policy lowered to 15m — the service must not consult it at all.)
	if err := svc.Revoke(context.Background(), "sid-2", "logout"); err != nil {
		t.Fatalf("Revoke: %v", err)
	}
	if got := fake.ttlFor("auth:revoked:session:sid-2"); got < 4*time.Hour {
		t.Fatalf("entry TTL %v does not cover the already-minted 4h token", got)
	}
}
```

Read the existing test file first: if it has no Redis fake exposing the stored TTL, add one:

```go
type fakeRevocationRedis struct {
	mu   sync.Mutex
	ttls map[string]time.Duration
}

func newFakeRevocationRedis() *fakeRevocationRedis {
	return &fakeRevocationRedis{ttls: map[string]time.Duration{}}
}

func (f *fakeRevocationRedis) Set(_ context.Context, key string, _ interface{}, ttl time.Duration) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.ttls[key] = ttl
	return nil
}
func (f *fakeRevocationRedis) Get(_ context.Context, key string) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if _, ok := f.ttls[key]; ok {
		return "revoked", nil
	}
	return "", redis.Nil
}
func (f *fakeRevocationRedis) Del(context.Context, ...string) error            { return nil }
func (f *fakeRevocationRedis) Keys(context.Context, string) ([]string, error)  { return nil, nil }
func (f *fakeRevocationRedis) Incr(context.Context, string) (int64, error)     { return 0, nil }
func (f *fakeRevocationRedis) Expire(context.Context, string, time.Duration) error { return nil }

func (f *fakeRevocationRedis) ttlFor(key string) time.Duration {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.ttls[key]
}
```

- [ ] **Step 2: Run to verify failure**

Run: `cd backend && go test ./internal/core/auth/services/ -run TestSessionRevocationTTL -count=1`
Expected: FAIL — entry TTL is `constructorArg + 1m`, not the fixed 24h+1m

- [ ] **Step 3: Pin the TTL**

In `backend/internal/core/auth/services/session_revocation_service.go`, add below the existing `sessionRevocationWarningInterval`:

```go
// sessionRevocationTTL is how long a revoked sid stays on the denylist.
// It is FIXED at the maximum access-token lifetime the platform permits
// plus a clock-skew minute — deliberately not derived from the live
// policy value.
//
// Deriving it from the current value is unsafe in both directions. If an
// operator raises accessTokenTTL, an entry sized from the old value
// expires while the new longer tokens are still valid. If they lower it,
// an entry sized from the new value expires while tokens minted under the
// old one are still valid. Because NewJWTService clamps every effective
// lifetime to MaxAccessTokenTTL, no token can outlive this window.
// The alternative — tracking each session's newest access-token exp —
// costs a write per mint to save bounded Redis retention. ADR-0017 D5.
const sessionRevocationTTL = MaxAccessTokenTTL + time.Minute
```

Rewrite the constructor pair:

```go
// NewSessionRevocationService builds a Redis-backed revocation store.
//
// Deprecated argument: accessTokenTTL is ignored. It is retained so forks
// calling this constructor directly keep compiling; the entry TTL is the
// fixed sessionRevocationTTL. Passing a shorter value cannot shorten the
// security window. ADR-0017 D5.
func NewSessionRevocationService(client RedisClient, accessTokenTTL time.Duration, log *slog.Logger) SessionRevocationService {
	_ = accessTokenTTL
	return newSessionRevocationService(client, accessTokenTTL, log, metrics.Default())
}

func newSessionRevocationService(client RedisClient, _ time.Duration, log *slog.Logger, recorder sessionRevocationStoreFailureRecorder) SessionRevocationService {
	if log == nil {
		log = slog.Default()
	}
	if recorder == nil {
		recorder = metrics.Default()
	}
	return &redisSessionRevocationService{
		client:  client,
		ttl:     sessionRevocationTTL,
		log:     log,
		metrics: recorder,
	}
}
```

Update the type doc comment on `SessionRevocationService` — the sentence "Entries auto-expire after the access-token TTL plus a small clock-skew buffer: a token older than that is already rejected by signature validation" is now wrong. Replace with: "Entries auto-expire after `sessionRevocationTTL` — the maximum access-token lifetime the platform permits, plus a clock-skew minute. Sizing this from the live policy value would let a policy change strand tokens outside their own revocation entry."

- [ ] **Step 4: Stop threading the boot-time env value**

In `backend/internal/core/auth/module.go:803-809`, the argument is now inert but keep the call shape readable:

```go
	// Session revocation list (Block D): Redis-backed set of revoked
	// `sid` claims checked on every authenticated request. Single
	// instance shared across both tiers since the sid namespace is
	// global. The TTL argument is deprecated and ignored — entries live
	// for the fixed maximum access-token lifetime plus clock skew, which
	// is the only window that survives a live policy change in either
	// direction (ADR-0017 D5).
	sessionRevocationSvc := services.NewSessionRevocationService(
		deps.RedisAdapter,
		0,
		logger,
	)
```

- [ ] **Step 5: Run to verify pass**

Run: `cd backend && go test ./internal/core/auth/... -count=1`
Expected: PASS

- [ ] **Step 6: Update the module contract doc**

In `backend/internal/core/auth/CLAUDE.md`, the "Session revocation list" bullet (~line 321) currently reads "Entries auto-expire after the access-token TTL + 1min buffer." Replace with:

> Entries auto-expire after a **fixed** 24h + 1min — the maximum access-token lifetime the platform permits, plus clock skew — never a value derived from the live `accessTokenTTL`. Sizing the entry from the current policy value strands tokens on both sides of a policy change: raising the TTL leaves long tokens uncovered, lowering it expires the entry while tokens minted under the old value are still valid. `NewJWTService` clamps every effective access-token lifetime to 24h so the window is always sufficient. ADR-0017 D5.

- [ ] **Step 7: Commit**

```bash
cd /home/tore/orkestra
git add backend/internal/core/auth/services/session_revocation_service.go \
        backend/internal/core/auth/services/session_revocation_service_test.go \
        backend/internal/core/auth/module.go backend/internal/core/auth/CLAUDE.md
git commit -m "fix(auth): pin the session-revocation denylist TTL to the policy maximum

The denylist derived its entry TTL from JWT_ACCESS_TOKEN_EXPIRY read once
at boot, while access tokens took their lifetime from the admin policy read
live on every mint. Raising accessTokenTTL made revoked sessions valid again
once the shorter entry expired, silently. ADR-0017 D5."
```

---

### Task 4: Optional SDK config-validation seam

`ConfigField` carries `Min`/`Max`/`Pattern`, but `ValidateConfigDeclarations` only checks that the *schema declaration* is coherent — `UpdateConfig` never compares submitted values against it. Enforcement exists solely client-side, so an operator can PATCH `accessTokenTTL: "9999h"` through the API. `Min`/`Max` are `*int` and cannot express a bound on a duration field anyway.

This task adds only the **callback seam**. Teaching `UpdateConfig` to interpret every schema constraint is a separate contract change with its own ADR (spec: Out of scope). Modules that omit the interface behave exactly as today, so forks' addons keep compiling and keep accepting the same values.

**Files:**
- Create: `backend/pkg/sdk/module/config_validator.go`
- Create: `backend/pkg/sdk/module/config_validator_test.go`
- Modify: `backend/pkg/sdk/module/config_service.go:391-436` (`UpdateConfig`), `:456-500` (`UpdateEnvironmentConfig`)
- Modify: `backend/pkg/sdk/module/handler.go:277` and `:394`
- Modify: `backend/pkg/sdk/CLAUDE.md`

**Interfaces:**
- Produces: `module.HasConfigValidator { ValidateConfig(ctx context.Context, mergedValues map[string]string) error }` — Task 5 implements it on `AuthModule`.
- Produces: `module.ConfigValidationError{Field, Message string}` with `Error() string` — Task 5 returns it; the handler maps it to 422.

- [ ] **Step 1: Write the failing tests**

Create `backend/pkg/sdk/module/config_validator_test.go`:

```go
package module

import (
	"context"
	"errors"
	"strings"
	"testing"
)

// validatingModule implements the optional seam and rejects one key.
type validatingModule struct {
	BaseModule
	seen map[string]string
}

func (m *validatingModule) Name() string { return "validating" }
func (m *validatingModule) Init(*Dependencies) error { return nil }
func (m *validatingModule) ValidateConfig(_ context.Context, values map[string]string) error {
	m.seen = values
	if values["strict"] == "bad" {
		return &ConfigValidationError{Field: "strict", Message: "must not be bad"}
	}
	return nil
}

// plainModule omits the seam entirely — it must be unaffected.
type plainModule struct{ BaseModule }

func (plainModule) Name() string          { return "plain" }
func (plainModule) Init(*Dependencies) error { return nil }

func TestConfigValidationError_MessageIncludesField(t *testing.T) {
	err := error(&ConfigValidationError{Field: "accessTokenTTL", Message: "must be between 1m0s and 24h0m0s"})
	if !strings.Contains(err.Error(), "accessTokenTTL") {
		t.Errorf("Error() = %q, want it to name the field so the operator knows which input to fix", err.Error())
	}
	var typed *ConfigValidationError
	if !errors.As(err, &typed) {
		t.Fatal("ConfigValidationError must be recoverable with errors.As so the handler can map it to 422")
	}
}
```

Add, in the same file, a test that the service consults the seam. Model it on the existing `config_service` tests — read `backend/pkg/sdk/module/config_service_test.go` first for the in-memory repo/redis fakes it already uses and reuse them rather than inventing new ones:

```go
func TestConfigUpdate_ModuleValidatorOptional(t *testing.T) {
	ctx := context.Background()

	// A module WITHOUT the seam is unaffected — any value persists.
	svcPlain, repoPlain := newTestConfigService(t)   // helper from config_service_test.go
	svcPlain.RegisterKnownModules([]Module{plainModule{}})
	seedModuleDoc(t, repoPlain, "plain", map[string]string{"anything": "old"})
	if err := svcPlain.UpdateConfig(ctx, "plain", map[string]string{"anything": "whatever"}, nil); err != nil {
		t.Fatalf("module without validator must keep today's behaviour: %v", err)
	}

	// A module WITH the seam rejects before persistence.
	svc, repo := newTestConfigService(t)
	vm := &validatingModule{}
	svc.RegisterKnownModules([]Module{vm})
	seedModuleDoc(t, repo, "validating", map[string]string{"strict": "good", "other": "keep"})

	err := svc.UpdateConfig(ctx, "validating", map[string]string{"strict": "bad"}, nil)
	var typed *ConfigValidationError
	if !errors.As(err, &typed) {
		t.Fatalf("UpdateConfig = %v, want *ConfigValidationError", err)
	}
	doc, _ := repo.FindByName(ctx, "validating")
	if doc.ConfigValues["strict"] != "good" {
		t.Errorf("rejected value reached persistence: %q", doc.ConfigValues["strict"])
	}

	// The validator sees the MERGED values, not just the PATCH body, so a
	// later cross-field rule cannot be bypassed with a partial PATCH.
	_ = svc.UpdateConfig(ctx, "validating", map[string]string{"strict": "fine"}, nil)
	if vm.seen["other"] != "keep" {
		t.Errorf("validator saw %v, want the merged document including untouched keys", vm.seen)
	}
}

func TestEnvironmentConfigUpdate_InvokesModuleValidator(t *testing.T) {
	ctx := context.Background()
	svc, repo := newTestConfigService(t)
	svc.RegisterKnownModules([]Module{&validatingModule{}})
	seedModuleDocWithEnv(t, repo, "validating", "sandbox", map[string]string{"strict": "good"})

	err := svc.UpdateEnvironmentConfig(ctx, "validating", "sandbox", map[string]string{"strict": "bad"}, nil)
	var typed *ConfigValidationError
	if !errors.As(err, &typed) {
		t.Fatalf("named-environment PATCH = %v, want *ConfigValidationError — the profile surface must not be a bypass", err)
	}
	doc, _ := repo.FindByName(ctx, "validating")
	if doc.Environments["sandbox"].ConfigValues["strict"] != "good" {
		t.Error("rejected value reached the environment profile")
	}
}

func TestSetActiveEnvironment_LegacyInvalidValueUsesDefensiveReader(t *testing.T) {
	// Activation must NOT reject a legacy invalid profile: the defensive
	// readers keep the deployment operable and the next edit repairs it.
	ctx := context.Background()
	svc, repo := newTestConfigService(t)
	svc.RegisterKnownModules([]Module{&validatingModule{}})
	seedModuleDocWithEnv(t, repo, "validating", "sandbox", map[string]string{"strict": "bad"})
	if err := svc.SetActiveEnvironment(ctx, "validating", "sandbox"); err != nil {
		t.Fatalf("SetActiveEnvironment must stay recoverable on legacy data: %v", err)
	}
}
```

If `newTestConfigService` / `seedModuleDoc` / `seedModuleDocWithEnv` do not exist, write them in this file against the same fakes the existing config-service tests use.

- [ ] **Step 2: Run to verify failure**

Run: `cd backend && go test ./pkg/sdk/module/ -run 'TestConfigValidationError|TestConfigUpdate_ModuleValidator|TestEnvironmentConfigUpdate_InvokesModuleValidator|TestSetActiveEnvironment_Legacy' -count=1`
Expected: FAIL — `undefined: ConfigValidationError`, `undefined: HasConfigValidator`

- [ ] **Step 3: Add the seam**

Create `backend/pkg/sdk/module/config_validator.go`:

```go
package module

import (
	"context"
	"fmt"
)

// HasConfigValidator lets a module reject config values at the PATCH
// boundary, before encryption or persistence. It is OPTIONAL: modules
// that omit it retain the pre-ADR-0017 behaviour, in which UpdateConfig
// persists whatever it is given.
//
// The seam exists because ConfigField's Min/Max are *int and cannot
// express a bound on a duration, and because teaching UpdateConfig to
// interpret every schema constraint generically is a broader contract
// change that deserves its own ADR. Policy therefore stays inside the
// module that owns it, and pkg/sdk/module needs no import of, or
// name-based special case for, any particular module. ADR-0017 D6.
//
// mergedValues is the module's stored non-secret configuration with the
// PATCH overlaid — not just the submitted keys — so a cross-field rule
// added later cannot be bypassed by PATCHing one half of a pair.
// Secrets are never passed: a validator must not see decrypted secret
// material to do its job.
//
// Return a *ConfigValidationError to produce a 422 naming the offending
// field. Any other error propagates as an ordinary failure.
type HasConfigValidator interface {
	ValidateConfig(ctx context.Context, mergedValues map[string]string) error
}

// ConfigValidationError reports one rejected config field. The admin API
// maps it to 422 Unprocessable Entity; the message is operator-facing, so
// it must describe the accepted range and must never quote internal state.
type ConfigValidationError struct {
	Field   string
	Message string
}

func (e *ConfigValidationError) Error() string {
	return fmt.Sprintf("%s: %s", e.Field, e.Message)
}
```

- [ ] **Step 4: Invoke it on both update paths**

In `backend/pkg/sdk/module/config_service.go`, add the lookup helper:

```go
// validateModuleConfig runs the module's optional ValidateConfig hook
// against exactly the non-secret values the update would persist.
// Modules that are unknown to this service, or that omit the seam, are
// accepted unchanged. ADR-0017 D6.
func (s *ModuleConfigService) validateModuleConfig(ctx context.Context, name string, merged map[string]string) error {
	m, ok := s.knownModules[name]
	if !ok {
		return nil
	}
	v, ok := m.(HasConfigValidator)
	if !ok {
		return nil
	}
	return v.ValidateConfig(ctx, merged)
}
```

In `UpdateConfig`, move the `FindByName` load **above** the encryption loop and validate between them:

```go
func (s *ModuleConfigService) UpdateConfig(ctx context.Context, name string, values map[string]string, secrets map[string]string) error {
	// Load first: the module validator must see the merged document, and
	// nothing may be encrypted or written before it has accepted it.
	existing, err := s.repo.FindByName(ctx, name)
	if err != nil {
		return err
	}
	if existing == nil {
		return fmt.Errorf("module %q not found", name)
	}
	mergedValues := mergeStringMaps(existing.ConfigValues, values)
	if err := s.validateModuleConfig(ctx, name, mergedValues); err != nil {
		return err
	}

	encrypted := make(map[string]string, len(secrets))
	for k, v := range secrets {
		enc, err := encryptSecret(v)
		if err != nil {
			return fmt.Errorf("encrypt secret %q: %w", k, err)
		}
		encrypted[k] = enc
	}

	// Update legacy top-level fields for backward compat.
	if err := s.repo.UpdateConfigValues(
		ctx, name,
		mergedValues,
		mergeStringMaps(existing.EncryptedValues, encrypted),
	); err != nil {
		return err
	}
	// ... the environment-sync block and InvalidateCache stay as they are,
	// but reuse mergedValues where it recomputed mergeStringMaps(existing.ConfigValues, values).
```

In `UpdateEnvironmentConfig`, validate right after `existingEnv := doc.Environments[envName]` and before the encryption loop:

```go
	existingEnv := doc.Environments[envName]
	mergedValues := mergeStringMaps(existingEnv.ConfigValues, values)
	if err := s.validateModuleConfig(ctx, name, mergedValues); err != nil {
		return err
	}
```

then drop the later duplicate `mergedValues :=` assignment.

`SetActiveEnvironment` is deliberately left unvalidated — a legacy invalid profile must stay activatable, and the defensive readers keep the deployment operable until the operator repairs the value.

- [ ] **Step 5: Map it to 422 on both PATCH surfaces**

In `backend/pkg/sdk/module/handler.go`, `UpdateModule` (~line 277):

```go
		if err := h.configService.UpdateConfig(ctx, input.Name, input.Body.Config, input.Body.Secrets); err != nil {
			var invalid *ConfigValidationError
			if errors.As(err, &invalid) {
				return nil, huma.Error422UnprocessableEntity(invalid.Error())
			}
			return nil, err
		}
```

and `UpdateEnvironment` (~line 394):

```go
	if err := h.configService.UpdateEnvironmentConfig(ctx, input.Name, input.Env, input.Body.Config, input.Body.Secrets); err != nil {
		var invalid *ConfigValidationError
		if errors.As(err, &invalid) {
			return nil, huma.Error422UnprocessableEntity(invalid.Error())
		}
		return nil, huma.Error400BadRequest(err.Error())
	}
```

Add `"errors"` to `handler.go`'s import block if it is not already there.

- [ ] **Step 6: Run to verify pass**

Run: `cd backend && go test ./pkg/sdk/... -count=1`
Expected: PASS

- [ ] **Step 7: Document the seam**

In `backend/pkg/sdk/CLAUDE.md`, add `HasConfigValidator` to the list of optional module sub-interfaces, stating: it runs on both `PATCH /v1/admin/modules/{name}` and `PATCH /v1/admin/modules/{name}/environments/{env}`, before encryption or persistence; it receives merged non-secret values; returning `*ConfigValidationError` produces a 422 naming the field; omitting the interface preserves today's behaviour; and `SetActiveEnvironment` deliberately does not invoke it.

- [ ] **Step 8: Commit**

```bash
cd /home/tore/orkestra
git add backend/pkg/sdk/module/config_validator.go backend/pkg/sdk/module/config_validator_test.go \
        backend/pkg/sdk/module/config_service.go backend/pkg/sdk/module/handler.go backend/pkg/sdk/CLAUDE.md
git commit -m "feat(sdk): optional HasConfigValidator seam on module config updates

UpdateConfig never compared submitted values against the schema, so
enforcement was client-side only. Adds an opt-in per-module validator
invoked before encryption on both the active-config and named-environment
PATCH paths, mapped to 422. Modules that omit it are unchanged. ADR-0017 D6."
```

---

### Task 5: Auth implements the validator

Bounds are enforced twice on purpose: 422 at the edit boundary so the stored value the admin UI shows cannot disagree with the effective value, and the Task 2 read-time clamp for legacy or out-of-band data. Read-time clamping alone was rejected precisely because it leaves the two disagreeing.

`accountLockoutDuration` and `accountLockoutThreshold` are deliberately excluded: they do not govern already-issued credentials, and an absurd value there is self-punishing rather than exploitable.

**Files:**
- Create: `backend/internal/core/auth/config_validation.go`
- Create: `backend/internal/core/auth/config_validation_test.go`
- Modify: `backend/internal/core/auth/module.go:403-412` (the two duration `ConfigField`s)

**Interfaces:**
- Consumes: `module.HasConfigValidator`, `module.ConfigValidationError` (Task 4); `services.MaxAccessTokenTTL`, `services.MinAccessTokenTTL` (Task 2 — export `minAccessTokenTTL` as `MinAccessTokenTTL` and the two password-reset bounds as `MinPasswordResetTokenTTL` / `MaxPasswordResetTokenTTL` in this task, since `internal/core/auth` and `internal/core/auth/services` are different packages).
- Produces: `(*AuthModule).ValidateConfig(ctx, map[string]string) error`; `authDurationBounds` — Task 7 appends the `sessionAbsoluteTTL` row to it.

- [ ] **Step 1: Export the bounds Task 2 kept private**

In `backend/internal/core/auth/services/auth_duration_bounds.go`, rename `minAccessTokenTTL` → `MinAccessTokenTTL`, `minPasswordResetTokenTTL` → `MinPasswordResetTokenTTL`, `maxPasswordResetTokenTTL` → `MaxPasswordResetTokenTTL`, and update the three references in `auth_policy_service.go` plus the assertions in `token_ttl_chain_test.go`.

Run: `cd backend && go build ./... && go test ./internal/core/auth/services/ -count=1`
Expected: PASS

- [ ] **Step 2: Write the failing test**

Create `backend/internal/core/auth/config_validation_test.go`:

```go
package auth

import (
	"context"
	"errors"
	"testing"

	"github.com/orkestra/backend/pkg/sdk/module"
)

// The input contract from the ADR-0017 design, exhaustively. Rows marked
// wantField must be rejected with 422 and must never reach persistence;
// the empty rows are legal because emptiness is field-specific meaning,
// not absence of a decision.
func TestAuthDurationPatchValidation(t *testing.T) {
	cases := []struct {
		name      string
		values    map[string]string
		wantField string
	}{
		{"absent key", map[string]string{}, ""},
		{"accessTokenTTL empty falls through to env", map[string]string{"accessTokenTTL": ""}, ""},
		{"accessTokenTTL blank is empty", map[string]string{"accessTokenTTL": "   "}, ""},
		{"accessTokenTTL at minimum", map[string]string{"accessTokenTTL": "1m"}, ""},
		{"accessTokenTTL at maximum", map[string]string{"accessTokenTTL": "24h"}, ""},
		{"accessTokenTTL day suffix", map[string]string{"accessTokenTTL": "1d"}, ""},
		{"accessTokenTTL below minimum", map[string]string{"accessTokenTTL": "30s"}, "accessTokenTTL"},
		{"accessTokenTTL above maximum", map[string]string{"accessTokenTTL": "9999h"}, "accessTokenTTL"},
		{"accessTokenTTL malformed", map[string]string{"accessTokenTTL": "forever"}, "accessTokenTTL"},
		{"accessTokenTTL zero", map[string]string{"accessTokenTTL": "0s"}, "accessTokenTTL"},
		{"accessTokenTTL negative", map[string]string{"accessTokenTTL": "-5m"}, "accessTokenTTL"},
		{"passwordResetTokenTTL empty uses default", map[string]string{"passwordResetTokenTTL": ""}, ""},
		{"passwordResetTokenTTL at minimum", map[string]string{"passwordResetTokenTTL": "5m"}, ""},
		{"passwordResetTokenTTL below minimum", map[string]string{"passwordResetTokenTTL": "1m"}, "passwordResetTokenTTL"},
		{"passwordResetTokenTTL above maximum", map[string]string{"passwordResetTokenTTL": "72h"}, "passwordResetTokenTTL"},
		// Deliberately unbounded: neither governs an already-issued credential.
		{"lockout duration absurd but accepted", map[string]string{"accountLockoutDuration": "9999h"}, ""},
		{"lockout threshold absurd but accepted", map[string]string{"accountLockoutThreshold": "999999"}, ""},
	}
	m := &AuthModule{}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := m.ValidateConfig(context.Background(), tc.values)
			if tc.wantField == "" {
				if err != nil {
					t.Fatalf("ValidateConfig(%v) = %v, want nil", tc.values, err)
				}
				return
			}
			var typed *module.ConfigValidationError
			if !errors.As(err, &typed) {
				t.Fatalf("ValidateConfig(%v) = %v, want *ConfigValidationError", tc.values, err)
			}
			if typed.Field != tc.wantField {
				t.Errorf("Field = %q, want %q", typed.Field, tc.wantField)
			}
		})
	}
}

func TestAuthModuleImplementsConfigValidator(t *testing.T) {
	var _ module.HasConfigValidator = (*AuthModule)(nil)
}
```

- [ ] **Step 3: Run to verify failure**

Run: `cd backend && go test ./internal/core/auth/ -run TestAuthDurationPatchValidation -count=1`
Expected: FAIL — `(*AuthModule).ValidateConfig` undefined

- [ ] **Step 4: Implement the validator**

Create `backend/internal/core/auth/config_validation.go`:

```go
package auth

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/orkestra/backend/internal/core/auth/services"
	"github.com/orkestra/backend/internal/shared/utils"
	"github.com/orkestra/backend/pkg/sdk/module"
)

var _ module.HasConfigValidator = (*AuthModule)(nil)

// durationBound is one admin-editable duration that governs credentials
// already in circulation, and therefore cannot be left unbounded.
type durationBound struct {
	key      string
	min, max time.Duration
	// why is appended to the operator-facing 422 message so the bound
	// reads as a reason rather than an arbitrary refusal.
	why string
}

// authDurationBounds intentionally omits accountLockoutDuration and
// accountLockoutThreshold: neither governs an already-issued credential,
// and an absurd value there is self-punishing rather than exploitable.
// ADR-0017 D6.
var authDurationBounds = []durationBound{
	{
		key: "accessTokenTTL",
		min: services.MinAccessTokenTTL, max: services.MaxAccessTokenTTL,
		why: "below a minute the SPA enters a refresh loop; above 24h a token would outlive its own revocation entry",
	},
	{
		key: "passwordResetTokenTTL",
		min: services.MinPasswordResetTokenTTL, max: services.MaxPasswordResetTokenTTL,
		why: "below five minutes the link dies before the mail is delivered",
	},
}

// ValidateConfig rejects malformed or out-of-range duration values before
// they are persisted, so the value the admin UI displays is the value in
// force. It is the write half of ADR-0017 D6; the read half is the
// clamping in services.clampPersistedDuration, which stays as a defence
// for values written by an older release or directly to Mongo.
//
// An empty value is always accepted: emptiness is a decision with a
// field-specific meaning (accessTokenTTL falls through to the
// environment, passwordResetTokenTTL uses its 30-minute default,
// sessionAbsoluteTTL disables the cap), never an omission to reject.
func (m *AuthModule) ValidateConfig(_ context.Context, values map[string]string) error {
	for _, b := range authDurationBounds {
		raw, present := values[b.key]
		if !present {
			continue
		}
		raw = strings.TrimSpace(raw)
		if raw == "" {
			continue
		}
		d, ok := utils.ParseDuration(raw)
		if !ok || d <= 0 {
			return &module.ConfigValidationError{
				Field:   b.key,
				Message: fmt.Sprintf("must be a positive duration such as 15m, 2h or 30d (%s)", b.why),
			}
		}
		if d < b.min || d > b.max {
			return &module.ConfigValidationError{
				Field:   b.key,
				Message: fmt.Sprintf("must be between %s and %s (%s)", b.min, b.max, b.why),
			}
		}
	}
	return nil
}
```

- [ ] **Step 5: Add the client-side pattern hint**

In `backend/internal/core/auth/module.go`, add `Pattern` to the two duration fields:

```go
		{
			Key: "accessTokenTTL", Label: "Access token lifetime", Group: "login",
			Description: "Go duration string — how long an issued access token stays valid. Shorter = tighter security but more refresh round-trips. Range 1m–24h. Default 15m.",
			Type:        module.FieldDuration, Default: "15m",
			// UX aid only: ModuleConfigFields.tsx gives feedback before
			// save. Enforcement is the server-side ValidateConfig above —
			// this pattern is deliberately stricter than the parser (it
			// rejects compound forms like 1h30m that the server accepts),
			// which is acceptable for a hint but must never be treated as
			// the contract.
			Pattern: "^[0-9]+(s|m|h|d)$",
		},
		{
			Key: "passwordResetTokenTTL", Label: "Password reset link lifetime", Group: "login",
			Description: "Go duration string — how long the link in the reset-password email stays valid. Range 5m–24h. Default 30m.",
			Type:        module.FieldDuration, Default: "30m",
			Pattern:     "^[0-9]+(s|m|h|d)$",
		},
```

- [ ] **Step 6: Run to verify pass**

Run: `cd backend && go test ./internal/core/auth/... ./pkg/sdk/... -count=1`
Expected: PASS

- [ ] **Step 7: Document the contract**

In `backend/internal/core/auth/CLAUDE.md`, under the config section, add a table with the exact input contract:

| Input | `accessTokenTTL` | `passwordResetTokenTTL` |
|---|---|---|
| empty | unset: falls through to `JWT_ACCESS_TOKEN_EXPIRY`, then 15m | 30m default |
| malformed or out-of-range PATCH | 422, not persisted | 422, not persisted |
| malformed value already in DB | warn, fall through to env | warn, use 30m |
| out of range already in DB | warn and clamp | warn and clamp |
| env / direct constructor above 24h | warn and clamp to 24h | n/a |

State that enforcement is `AuthModule.ValidateConfig` (the `module.HasConfigValidator` seam), that generic `UpdateConfig` still validates nothing, and that `accountLockoutDuration`/`accountLockoutThreshold` are deliberately unbounded.

- [ ] **Step 8: Commit**

```bash
cd /home/tore/orkestra
git add backend/internal/core/auth/config_validation.go backend/internal/core/auth/config_validation_test.go \
        backend/internal/core/auth/module.go backend/internal/core/auth/services/ backend/internal/core/auth/CLAUDE.md
git commit -m "feat(auth): validate credential-governing durations at the config boundary

accessTokenTTL declared no bounds, so an operator could PATCH 9999h through
the API. Auth now implements the optional SDK validator and rejects
malformed or out-of-range non-empty values with 422 before persistence,
while the readers keep clamping legacy data. ADR-0017 D6."
```

---

### Task 6: Align the shipped example and correct the lifetime comments

`docker/.env.example` ships `JWT_ACCESS_TOKEN_EXPIRY=1h` while every compose file and `config.go` default to `15m` — three values, none of them in force. Without this alignment, Task 2 would silently **lengthen** the effective access-token TTL from 15 minutes to 1 hour on every install that copied the shipped example. That is why it cannot be deferred to a later PR.

**Files:**
- Modify: `docker/.env.example:137`
- Modify: `backend/internal/core/auth/services/jwt_service.go` (the `refreshTTL <= 0` guard comment, and the `NewJWTService` doc block)
- Modify: `CHANGELOG.md` — no; the changelog is generated by git-cliff at release. Put the operator instruction in the PR description and in `backend/internal/core/auth/CLAUDE.md` instead.

**Interfaces:** none — this task ships no new symbol.

- [ ] **Step 1: Align the example**

In `docker/.env.example`, change line 137 and expand its comment:

```sh
# Access-token lifetime. Resolution order is: the admin `accessTokenTTL`
# at /admin/modules → this variable → 15m. Before ADR-0017 the middle
# level was unreachable, so this key had no effect on any deployment;
# it does now. Range 1m–24h; a longer value is clamped to 24h with a
# warning, because the Redis revocation denylist stores entries for
# 24h + 1m and a token may never outlive its own revocation entry.
JWT_ACCESS_TOKEN_EXPIRY=15m
```

- [ ] **Step 2: Verify the three sources now agree**

Run:
```bash
cd /home/tore/orkestra
grep -n "JWT_ACCESS_TOKEN_EXPIRY" docker/.env.example docker/docker-compose.*.yml backend/internal/shared/config/config.go
```
Expected: `.env.example` `15m`, all three compose defaults `15m`, `config.go` default `"15m"` — four occurrences, one value.

- [ ] **Step 3: Correct the unreachable-guard comment (decision F)**

In `backend/internal/core/auth/services/jwt_service.go`, above the refresh guard in `NewJWTService`:

```go
	// Unreachable through configuration: getEnvAsDuration never returns
	// zero for JWT_REFRESH_TOKEN_EXPIRY (it falls back to the shipped
	// "7d"), so this branch fires only for callers constructing the
	// service with an explicit zero — in practice, tests. Kept as-is per
	// ADR-0017 decision F: changing the value would be a silent behaviour
	// change for direct callers with no benefit. The refresh-token TTL an
	// actual deployment runs is 7d, not the 30d this line suggests.
	if refreshTTL <= 0 {
		refreshTTL = 30 * 24 * time.Hour
	}
```

Also fix the `NewJWTService` doc block: "Zero or negative values fall back to safe defaults" is now half-true for `accessTTL`, which is additionally clamped from above. Say so.

- [ ] **Step 4: Record the operator instruction**

In `backend/internal/core/auth/CLAUDE.md`, in the `JWT_ACCESS_TOKEN_EXPIRY` env-var table row, replace the current text with:

> Access-token TTL. **Level 2 of `admin accessTokenTTL → JWT_ACCESS_TOKEN_EXPIRY → 15m`.** Before ADR-0017 this level was unreachable — the policy substituted its 15m default for "unset" — so any deployment that set this by hand has been running on 15m regardless of the value. Repairing the chain activates their configured value, which may be longer than what they have actually been running: **diff this key against `docker/.env.example` before upgrading.** Effective values above 24h are clamped with a warning.

- [ ] **Step 5: Full backend gate**

Run: `make ci-backend`
Expected: PASS (lint, tenantscope, policycoverage, piiscan, vuln, tests, build, openapi-check)

- [ ] **Step 6: Commit and open PR 1**

```bash
cd /home/tore/orkestra
git add docker/.env.example backend/internal/core/auth/services/jwt_service.go backend/internal/core/auth/CLAUDE.md
git commit -m "docs(auth): align .env.example to 15m and correct the lifetime comments

Repairing the TTL chain would otherwise lengthen the effective access-token
TTL from 15m to 1h on every install that copied the shipped example. Also
corrects the unreachable refreshTTL<=0 guard comment per ADR-0017 F."
git push -u origin feat/adr-0017-pr1-token-ttl-source
gh pr create --base dev --title "fix(auth): single source of truth for token TTL (ADR-0017 PR 1)" --body "$(cat <<'BODY'
Implements PR 1 of docs/superpowers/specs/2026-08-21-session-lifetime-design.md (ADR-0017 D5/D6).

## Operator action required before upgrading

`JWT_ACCESS_TOKEN_EXPIRY` **begins taking effect for the first time.** Deployments
that set it by hand have been running on the 15-minute default regardless of its
value. Diff that key in your live `docker/.env` against `docker/.env.example`
before upgrading — the configured value may be longer than what you have actually
been running. Effective values above 24h are clamped to 24h with a warning.

## What changes

- `AuthPolicyService.AccessTokenTTL` reports `0` for unset, restoring the
  documented `admin → env → 15m` chain (one call site).
- The Redis revocation denylist stores entries for a **fixed** 24h + 1m instead of
  a value derived from a boot-time env read. Raising `accessTokenTTL` used to
  produce tokens that outlived their own revocation entries — revoked sessions
  became valid again, silently, with no log.
- Optional `module.HasConfigValidator` SDK seam; only `auth` implements it.
  Modules that omit it are unchanged, so fork addons keep compiling.
- One day-aware duration parser for env vars and admin values (`30d` now works
  in both, and `AUTH_DEVICE_TRUST_DURATION=30d` works for the first time).
- `.env.example` aligned to `15m`.

## Verification

`make ci-backend` green. Staging check before promotion: raise `accessTokenTTL`
above the old denylist TTL and confirm a revoked session stays revoked.
BODY
)"
```

---

# PR 2 — Absolute session cap

Branch: `feat/adr-0017-pr2-session-cap` off `dev`, **after PR 1 is merged.**

This is the PR that changes inherited behaviour. On the first refresh after deployment, any session that began more than 30 days earlier is terminated and the user returns to the login screen. That is what the existing data already records, not something the code has to arrange.

### Task 7: Cap configuration, constants, and the retention-margin invariant

The anchor is `session.StartedAt` — no new field, no backfill, no compatibility branch. Every credential-issuing path creates a session document, and a failure to persist it rolls back the just-minted refresh token, so no usable credential survives without its session record. `CreateSession` stamps `StartedAt = now` unconditionally, the UUID is preserved across every rotation (`newSessionID := tokenDoc.SessionUUID`), and the row is addressed by a unique index.

The 89-day maximum leaves a full-day margin below the 90-day `AuthSessionRetention`. Equality is unsafe: at exactly 90 days Mongo's TTL monitor may delete the session before the refresh path evaluates the cap, turning an expired session into a compatibility miss.

**Files:**
- Create: `backend/internal/core/auth/services/session_cap.go`
- Create: `backend/internal/core/auth/services/session_cap_test.go`
- Modify: `backend/internal/core/auth/module.go` (ConfigSchema `login` group)
- Modify: `backend/internal/core/auth/config_validation.go` (append the bound)
- Modify: `backend/internal/core/auth/config_validation_test.go`

**Interfaces:**
- Produces: `services.MaxSessionAbsoluteTTL = 89 * 24 * time.Hour`, `services.MinSessionAbsoluteTTL = time.Hour`, `services.DefaultSessionAbsoluteTTL = 720 * time.Hour`, `services.SessionRetentionSafetyMargin = 24 * time.Hour`.
- Produces: `(*AuthPolicyService).SessionAbsoluteTTL(ctx) time.Duration` — returns `0` when the cap is disabled. Task 10 consumes it.

- [ ] **Step 1: Write the failing tests**

Create `backend/internal/core/auth/services/session_cap_test.go`:

```go
package services

import (
	"context"
	"testing"
	"time"
)

// The cap and the session-retention window are coupled: retention must
// never be able to delete the anchor of a session still inside the cap.
// A strict inequality is required — at equality Mongo's TTL monitor can
// delete the row at the exact boundary, before the refresh path reads it,
// turning an expired session into a compatibility miss. Changing either
// constant must break the build. ADR-0017 D7.
func TestSessionAbsoluteTTLLeavesRetentionMargin(t *testing.T) {
	if MaxSessionAbsoluteTTL+SessionRetentionSafetyMargin > AuthSessionRetention {
		t.Fatalf("cap %v + margin %v exceeds retention %v — retention could delete a live session's anchor",
			MaxSessionAbsoluteTTL, SessionRetentionSafetyMargin, AuthSessionRetention)
	}
	if SessionRetentionSafetyMargin <= 0 {
		t.Fatal("the margin must be positive; equality races Mongo's TTL monitor at the cap boundary")
	}
	if DefaultSessionAbsoluteTTL > MaxSessionAbsoluteTTL || DefaultSessionAbsoluteTTL < MinSessionAbsoluteTTL {
		t.Fatalf("default %v outside the accepted range [%v, %v]", DefaultSessionAbsoluteTTL, MinSessionAbsoluteTTL, MaxSessionAbsoluteTTL)
	}
}

func TestSessionAbsoluteTTL_PolicyResolution(t *testing.T) {
	cases := []struct {
		name string
		set  map[string]string
		want time.Duration
	}{
		{"absent uses the 30-day default", nil, DefaultSessionAbsoluteTTL},
		{"empty disables the cap", map[string]string{"sessionAbsoluteTTL": ""}, 0},
		{"blank disables the cap", map[string]string{"sessionAbsoluteTTL": "   "}, 0},
		{"explicit value wins", map[string]string{"sessionAbsoluteTTL": "48h"}, 48 * time.Hour},
		{"day suffix parses", map[string]string{"sessionAbsoluteTTL": "7d"}, 7 * 24 * time.Hour},
		{"legacy malformed uses the default", map[string]string{"sessionAbsoluteTTL": "forever"}, DefaultSessionAbsoluteTTL},
		{"legacy below minimum clamps up", map[string]string{"sessionAbsoluteTTL": "5m"}, MinSessionAbsoluteTTL},
		{"legacy above maximum clamps down", map[string]string{"sessionAbsoluteTTL": "365d"}, MaxSessionAbsoluteTTL},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := newPolicy(tc.set).SessionAbsoluteTTL(context.Background()); got != tc.want {
				t.Errorf("got %v, want %v", got, tc.want)
			}
		})
	}
}

func TestSessionAbsoluteTTL_NilPolicyUsesDefault(t *testing.T) {
	var p *AuthPolicyService
	if got := p.SessionAbsoluteTTL(context.Background()); got != DefaultSessionAbsoluteTTL {
		t.Errorf("nil policy = %v, want the secure-by-default %v", got, DefaultSessionAbsoluteTTL)
	}
}
```

- [ ] **Step 2: Run to verify failure**

Run: `cd backend && go test ./internal/core/auth/services/ -run TestSessionAbsoluteTTL -count=1`
Expected: FAIL — `undefined: MaxSessionAbsoluteTTL`, `undefined: SessionAbsoluteTTL`

- [ ] **Step 3: Add the constants and the accessor**

Create `backend/internal/core/auth/services/session_cap.go`:

```go
package services

import (
	"context"
	"strings"
	"time"
)

// Absolute session lifetime. The refresh TTL is an IDLE timeout —
// rotation writes a fresh `now + refreshTTL` on every use, so seven days
// of inactivity ends a session but an active user is never asked to
// re-authenticate. These constants bound the total age of a session,
// measured from login, independently of activity. ADR-0017 D1.
const (
	// DefaultSessionAbsoluteTTL is 30 days, written in hours for
	// readability against time.Duration. The shared parser accepts "30d"
	// equally, so an operator may type either.
	DefaultSessionAbsoluteTTL = 720 * time.Hour
	MinSessionAbsoluteTTL     = time.Hour
	// MaxSessionAbsoluteTTL leaves SessionRetentionSafetyMargin below
	// AuthSessionRetention. Equality is not safe: at exactly the
	// retention boundary Mongo's TTL monitor may delete the session
	// document before the refresh path evaluates the cap, which would
	// present an expired session as a missing anchor.
	MaxSessionAbsoluteTTL = 89 * 24 * time.Hour
	// SessionRetentionSafetyMargin is the gap the invariant test pins.
	SessionRetentionSafetyMargin = 24 * time.Hour
)

// SessionAbsoluteTTL returns the maximum age a session may reach before
// the user must authenticate again, or 0 when the cap is disabled.
//
// Empty means DISABLED — the supported exit for a fork that does not want
// the cap, without patching code — and skips the session query entirely.
// An absent key, by contrast, means "never configured", which takes the
// 30-day default: the base must be secure out of the box, and a cap that
// shipped disabled would be adopted by nobody who had not already
// identified the gap. A nil policy service takes the same default.
// ADR-0017 D1.
func (s *AuthPolicyService) SessionAbsoluteTTL(ctx context.Context) time.Duration {
	if s == nil || s.cs == nil {
		return DefaultSessionAbsoluteTTL
	}
	raw := s.cs.GetValue(ctx, "auth", "sessionAbsoluteTTL")
	if strings.TrimSpace(raw) == "" {
		// Distinguish "the key is absent from the document" from "the
		// operator cleared it". GetValue returns "" for both, so fall
		// back to the schema default only when the document has no
		// opinion at all — see the ConfigService seeding contract, which
		// writes the declared Default on first boot. A seeded document
		// therefore holds "720h" and an operator-cleared one holds "".
		return DefaultSessionAbsoluteTTL
	}
	return clampPersistedDuration(raw, DefaultSessionAbsoluteTTL,
		MinSessionAbsoluteTTL, MaxSessionAbsoluteTTL, "sessionAbsoluteTTL", slogDefault())
}
```

**Read this before implementing:** the comment above describes a distinction `GetValue` cannot make — it returns `""` for both an absent key and a cleared one. Confirm the actual seeding behaviour by reading `buildInitialConfig` in `backend/pkg/sdk/module/config_service.go` and `GetValue`'s fall-back to `ConfigSchema().Default`. If `GetValue` already falls back to the declared `Default` for an absent key, then an explicitly-cleared value arrives as `""` and the code above must return `0` for empty, with the default supplied by the schema. Adjust the function and the two test rows (`"absent uses the 30-day default"`, `"empty disables the cap"`) to match the real behaviour, and state the resolved rule in the doc comment. **Do not guess** — this single branch decides whether a fork's "clear the field" exit works.

- [ ] **Step 4: Declare the config field**

In `backend/internal/core/auth/module.go`, after `passwordResetTokenTTL` in the `login` group:

```go
		// ADR-0017 D1 — absolute session cap. One field for both audience
		// tiers: the operator console and the client surface share one
		// value, following the loginEnabledAdmin/loginEnabledClient
		// precedent that per-tier splitting is added only when a need
		// appears. Anchored on session.StartedAt, so enabling it needs no
		// migration and on upgrade it signs out sessions older than the
		// cap because that is what the existing data already records.
		{
			Key: "sessionAbsoluteTTL", Label: "Maximum session age", Group: "login",
			Description: "Maximum lifetime of a session from login, independent of activity. " +
				"When it elapses the user must authenticate again. Range 1h–89d; " +
				"empty disables the cap. Default 720h (30 days).",
			Type: module.FieldDuration, Default: "720h",
			Pattern: "^[0-9]+(s|m|h|d)$",
		},
```

- [ ] **Step 5: Bound it at the PATCH boundary**

In `backend/internal/core/auth/config_validation.go`, append to `authDurationBounds`:

```go
	{
		key: "sessionAbsoluteTTL",
		min: services.MinSessionAbsoluteTTL, max: services.MaxSessionAbsoluteTTL,
		why: "the maximum leaves a one-day margin below the 90-day session retention window, so retention can never delete the anchor of a session still inside the cap",
	},
```

Add to the `TestAuthDurationPatchValidation` table in `config_validation_test.go`:

```go
		{"sessionAbsoluteTTL empty disables the cap", map[string]string{"sessionAbsoluteTTL": ""}, ""},
		{"sessionAbsoluteTTL at minimum", map[string]string{"sessionAbsoluteTTL": "1h"}, ""},
		{"sessionAbsoluteTTL at maximum", map[string]string{"sessionAbsoluteTTL": "89d"}, ""},
		{"sessionAbsoluteTTL default", map[string]string{"sessionAbsoluteTTL": "720h"}, ""},
		{"sessionAbsoluteTTL below minimum", map[string]string{"sessionAbsoluteTTL": "30m"}, "sessionAbsoluteTTL"},
		{"sessionAbsoluteTTL above maximum", map[string]string{"sessionAbsoluteTTL": "90d"}, "sessionAbsoluteTTL"},
		{"sessionAbsoluteTTL malformed", map[string]string{"sessionAbsoluteTTL": "forever"}, "sessionAbsoluteTTL"},
```

- [ ] **Step 6: Run to verify pass**

Run: `cd backend && go test ./internal/core/auth/... -count=1`
Expected: PASS

- [ ] **Step 7: Commit**

```bash
cd /home/tore/orkestra
git add backend/internal/core/auth/services/session_cap.go backend/internal/core/auth/services/session_cap_test.go \
        backend/internal/core/auth/module.go backend/internal/core/auth/config_validation.go \
        backend/internal/core/auth/config_validation_test.go
git commit -m "feat(auth): declare sessionAbsoluteTTL and its retention-margin invariant

ADR-0017 D1. One field for both tiers, default 720h, range 1h-89d, empty
disables. The 89-day maximum leaves a full day below AuthSessionRetention;
a build-breaking test pins the strict inequality, because equality races
Mongo's TTL monitor at the cap boundary."
```

---

### Task 8: The idempotent expiry transition

Two concurrent refreshes on a capped session must both terminate it but produce exactly one security event and one metric increment. A CAS on `isActive: true → false` names the winner. Revoking refresh rows is safe to repeat, so only the transition needs to be raced.

**Files:**
- Modify: `backend/internal/core/auth/repository/auth_session_repository.go` (interface + impl, near `TerminateSession` at `:330`)
- Modify: `backend/internal/core/auth/models/collections.go:110-115`
- Create: `backend/internal/core/auth/repository/auth_session_cap_test.go`

**Interfaces:**
- Produces: `AuthSessionRepository.ExpireSessionForMaxAge(ctx context.Context, uuid string) (won bool, err error)` — Task 10 consumes it.
- Produces: `models.RevokeReasonSessionMaxAge = "session_max_age"`.
- Note for Task 10: `gateSessionRepo` in `services/gates_fakes_test.go` must gain this method (it currently panics on unused methods) — and its `GetByUUID`, which today panics `"not used"`, must start serving seeded rows.

- [ ] **Step 1: Write the failing test**

This package already has a live-Mongo pattern in `refresh_token_repository_concurrency_test.go` (`liveRefreshRepository`, which skips unless `MONGO_TEST_URI` or `MONGO_URI` is set and drops a per-run database afterwards). There is no session equivalent yet, so add one in the same shape.

Create `backend/internal/core/auth/repository/auth_session_cap_test.go`:

```go
package repository

import (
	"context"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/orkestra/backend/internal/core/auth/models"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// liveSessionRepository mirrors liveRefreshRepository: same env vars, same
// per-run database, same cleanup. Split rather than generalised because
// the two set up different indexes.
func liveSessionRepository(t *testing.T) (*authSessionRepository, func()) {
	t.Helper()
	uri := os.Getenv("MONGO_TEST_URI")
	if uri == "" {
		uri = os.Getenv("MONGO_URI")
	}
	if uri == "" {
		t.Skip("set MONGO_TEST_URI or MONGO_URI to run live session repository tests")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	client, err := mongo.Connect(ctx, options.Client().ApplyURI(uri))
	if err != nil {
		t.Fatalf("mongo.Connect: %v", err)
	}
	if err := client.Ping(ctx, nil); err != nil {
		_ = client.Disconnect(context.Background())
		t.Fatalf("mongo.Ping: %v", err)
	}
	db := client.Database("auth_session_cap_" + uuid.NewString())
	repo := NewOperatorAuthSessionRepository(db).(*authSessionRepository)
	return repo, func() {
		_ = db.Drop(context.Background())
		_ = client.Disconnect(context.Background())
	}
}

func seedActiveSession(t *testing.T, repo *authSessionRepository, uuidStr string) {
	t.Helper()
	err := repo.CreateSession(context.Background(), &models.AuthSessionDoc{
		UUID: uuidStr, UserUUID: "cap-user", DeviceID: "cap-device",
	})
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
}

// Exactly one caller may win the transition. Without the isActive:true
// predicate every concurrent refresh reports itself the winner, and the
// cap security event and metric fire once per racing request instead of
// once per session. ADR-0017 D4.
func TestExpireSessionForMaxAge_NamesExactlyOneWinner(t *testing.T) {
	repo, cleanup := liveSessionRepository(t)
	defer cleanup()
	seedActiveSession(t, repo, "sess-race")

	const racers = 8
	var (
		wg   sync.WaitGroup
		mu   sync.Mutex
		wins int
	)
	wg.Add(racers)
	for i := 0; i < racers; i++ {
		go func() {
			defer wg.Done()
			won, err := repo.ExpireSessionForMaxAge(context.Background(), "sess-race")
			if err != nil {
				t.Errorf("ExpireSessionForMaxAge: %v", err)
				return
			}
			if won {
				mu.Lock()
				wins++
				mu.Unlock()
			}
		}()
	}
	wg.Wait()

	if wins != 1 {
		t.Fatalf("winners = %d, want exactly 1", wins)
	}
	sess, err := repo.GetByUUID(context.Background(), "sess-race")
	if err != nil || sess == nil {
		t.Fatalf("GetByUUID: %v", err)
	}
	if sess.IsActive {
		t.Error("every racer must still terminate the session, whether or not it won")
	}
}

// Losing the race is not an error — the caller must still receive the cap
// sentinel, not a 503.
func TestExpireSessionForMaxAge_AlreadyInactiveIsNotAnError(t *testing.T) {
	repo, cleanup := liveSessionRepository(t)
	defer cleanup()
	seedActiveSession(t, repo, "sess-done")
	if _, err := repo.ExpireSessionForMaxAge(context.Background(), "sess-done"); err != nil {
		t.Fatalf("first call: %v", err)
	}

	won, err := repo.ExpireSessionForMaxAge(context.Background(), "sess-done")
	if err != nil {
		t.Fatalf("second call returned an error: %v", err)
	}
	if won {
		t.Error("the second caller must not report itself the winner")
	}
}

// A UUID with no row is the same shape as the losing side of a race.
func TestExpireSessionForMaxAge_UnknownUUIDIsNotAnError(t *testing.T) {
	repo, cleanup := liveSessionRepository(t)
	defer cleanup()
	won, err := repo.ExpireSessionForMaxAge(context.Background(), "sess-absent")
	if err != nil || won {
		t.Fatalf("got (%v, %v), want (false, nil)", won, err)
	}
}
```

- [ ] **Step 2: Add the revoke reason**

In `backend/internal/core/auth/models/collections.go`, next to the other reasons:

```go
	// RevokeReasonSessionMaxAge marks rows revoked because the session
	// reached its configured absolute lifetime, as opposed to a user or
	// admin action. Distinct from RevokeReasonManualRevoke so a support
	// query can tell "we signed you out on a timer" from "someone
	// terminated your session". ADR-0017 D4.
	RevokeReasonSessionMaxAge = "session_max_age"
```

- [ ] **Step 3: Add the repository operation**

In `backend/internal/core/auth/repository/auth_session_repository.go`, add to the `AuthSessionRepository` interface under "Session termination":

```go
	// ExpireSessionForMaxAge flips an ACTIVE session to inactive and
	// reports whether this caller performed the transition. Concurrent
	// refreshes on a capped session all terminate it, but only the winner
	// gets true — so the security event and the cap metric are emitted
	// exactly once per session rather than once per racing request.
	// A session already inactive returns (false, nil), not an error.
	// ADR-0017 D4.
	ExpireSessionForMaxAge(ctx context.Context, uuid string) (bool, error)
```

and the implementation next to `TerminateSession`:

```go
func (r *authSessionRepository) ExpireSessionForMaxAge(ctx context.Context, uuid string) (bool, error) {
	filter := bson.M{"uuid": uuid, "isActive": true}
	update := bson.M{
		"$set": bson.M{
			"isActive":  false,
			"updatedAt": time.Now(),
		},
	}

	result, err := r.collection.UpdateOne(ctx, filter, update)
	if err != nil {
		return false, fmt.Errorf("failed to expire session for max age: %w", err)
	}
	// MatchedCount==0 means the row was already inactive (or gone). That
	// is not an error — it is the losing side of the race, and the caller
	// must still return the same cap sentinel to it.
	return result.ModifiedCount == 1, nil
}
```

- [ ] **Step 4: Run to verify build and tests**

Run: `cd backend && go build ./... && go test ./internal/core/auth/... -count=1`
Expected: PASS — every implementer of `AuthSessionRepository` compiles. If a test fake in `services/` fails to compile, that is Task 10's `gateSessionRepo` work; add the method there now with a real implementation rather than a panic:

```go
func (r *gateSessionRepo) ExpireSessionForMaxAge(_ context.Context, uuid string) (bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, d := range r.created {
		if d.UUID == uuid && d.IsActive {
			d.IsActive = false
			return true, nil
		}
	}
	return false, nil
}
```

- [ ] **Step 5: Commit**

```bash
cd /home/tore/orkestra
git add backend/internal/core/auth/repository/ backend/internal/core/auth/models/collections.go \
        backend/internal/core/auth/services/gates_fakes_test.go
git commit -m "feat(auth): idempotent session-expiry transition for the absolute cap

CAS on isActive:true so concurrent refreshes on a capped session all
terminate it but exactly one caller records the security event and the
metric. ADR-0017 D4."
```

---

### Task 9: Cap telemetry

Three counters, all with closed label schemas (ADR-0017 D8, extending ADR-0002). In production the expiry counter distinguishes a cap that works from a cap that is signing out too many people; the anomaly counter is the gate on tightening the compatibility rule to fail-closed.

**Files:**
- Modify: `backend/pkg/sdk/metrics/metrics.go` (package doc, `Collector` struct, `buildMetrics`, `Register`, recorders)
- Create: `backend/pkg/sdk/metrics/session_cap_test.go`

**Interfaces:**
- Produces: `(*Collector).RecordSessionCapExpiry()`, `(*Collector).RecordSessionCapEventFailure()`, `(*Collector).RecordSessionAnchorAnomaly(kind string)` — Task 10 consumes all three.

- [ ] **Step 1: Write the failing test**

Create `backend/pkg/sdk/metrics/session_cap_test.go`:

```go
package metrics

import (
	"testing"

	"github.com/prometheus/client_golang/prometheus/testutil"
)

// ADR-0017 D8 freezes these label sets. The anomaly kind is a closed
// allowlist of exactly two values: anything else collapses to "unknown"
// rather than minting a new time series, because an unbounded label is
// how a Prometheus cardinality explosion starts and history breaks when
// labels change.
func TestSessionAnchorAnomalyLabelsAreClosed(t *testing.T) {
	c := NewCollector()
	if err := c.Register(); err != nil {
		t.Fatalf("Register: %v", err)
	}
	c.RecordSessionAnchorAnomaly("missing")
	c.RecordSessionAnchorAnomaly("zero_timestamp")
	c.RecordSessionAnchorAnomaly("sess-9f3a-uuid-leak")
	c.RecordSessionAnchorAnomaly("")

	for kind, want := range map[string]float64{"missing": 1, "zero_timestamp": 1, "unknown": 2} {
		got := testutil.ToFloat64(c.sessionAnchorAnomalies.WithLabelValues(kind))
		if got != want {
			t.Errorf("kind=%q counter = %v, want %v", kind, got, want)
		}
	}
}

func TestSessionCapCountersAreUnlabelled(t *testing.T) {
	c := NewCollector()
	if err := c.Register(); err != nil {
		t.Fatalf("Register: %v", err)
	}
	c.RecordSessionCapExpiry()
	c.RecordSessionCapExpiry()
	c.RecordSessionCapEventFailure()

	if got := testutil.ToFloat64(c.sessionCapExpiries); got != 2 {
		t.Errorf("cap expiries = %v, want 2", got)
	}
	if got := testutil.ToFloat64(c.sessionCapEventFailures); got != 1 {
		t.Errorf("cap event failures = %v, want 1", got)
	}
}

func TestNilCollectorRecordersAreSafe(t *testing.T) {
	var c *Collector
	c.RecordSessionCapExpiry()
	c.RecordSessionCapEventFailure()
	c.RecordSessionAnchorAnomaly("missing")
}
```

- [ ] **Step 2: Run to verify failure**

Run: `cd backend && go test ./pkg/sdk/metrics/ -count=1`
Expected: FAIL — undefined fields and methods

- [ ] **Step 3: Add the families**

In `backend/pkg/sdk/metrics/metrics.go`, add to the `Collector` struct next to `sessionRevocationStoreFailures`:

```go
	sessionCapExpiries      prometheus.Counter
	sessionCapEventFailures prometheus.Counter
	sessionAnchorAnomalies  *prometheus.CounterVec
```

In `buildMetrics`:

```go
	c.sessionCapExpiries = prometheus.NewCounter(
		prometheus.CounterOpts{
			Namespace: "orkestra",
			Subsystem: "auth",
			Name:      "session_cap_expiries_total",
			Help:      "Count of sessions terminated because they reached the configured absolute maximum age. Unlabelled by design (ADR-0017 D8): the value distinguishes a cap that works from one signing out too many people, and no dimension of it is bounded.",
		},
	)

	c.sessionCapEventFailures = prometheus.NewCounter(
		prometheus.CounterOpts{
			Namespace: "orkestra",
			Subsystem: "auth",
			Name:      "session_cap_event_failures_total",
			Help:      "Count of cap expiries whose security-event write failed. Credentials are already terminated when this increments — the failure is observational, never restorative.",
		},
	)

	c.sessionAnchorAnomalies = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: "orkestra",
			Subsystem: "auth",
			Name:      "session_anchor_anomalies_total",
			Help:      "Count of refreshes that could not read a session-cap anchor and were permitted under the ADR-0017 compatibility window. Zero for 30 consecutive production days is the gate on tightening this to fail-closed.",
		},
		// kind is a closed allowlist: "missing" (clean not-found) or
		// "zero_timestamp" (row present, no usable StartedAt/CreatedAt).
		// Repository errors are NOT anomalies — they fail closed and use
		// ordinary error telemetry. ADR-0017 D8.
		[]string{"kind"},
	)
```

Extend the `Register` slice:

```go
	for _, m := range []prometheus.Collector{c.cedarDivergence, c.cedarEnforced, c.capabilityDenied, c.sessionRevocationStoreFailures, c.sessionCapExpiries, c.sessionCapEventFailures, c.sessionAnchorAnomalies, c.entitlementLag, c.httpDuration} {
```

Add the recorders next to `RecordSessionRevocationStoreFailure`:

```go
// RecordSessionCapExpiry counts one session terminated for reaching its
// configured maximum age. Emitted only by the caller that won the
// isActive transition, so concurrent refreshes on the same session count
// once. ADR-0017 D4.
func (c *Collector) RecordSessionCapExpiry() {
	if c == nil || c.sessionCapExpiries == nil {
		return
	}
	c.sessionCapExpiries.Inc()
}

// RecordSessionCapEventFailure counts a cap expiry whose security-event
// write failed. Durable state is already terminated at that point.
func (c *Collector) RecordSessionCapEventFailure() {
	if c == nil || c.sessionCapEventFailures == nil {
		return
	}
	c.sessionCapEventFailures.Inc()
}

// RecordSessionAnchorAnomaly counts a refresh permitted because the cap
// could not read an anchor. kind is limited to "missing" and
// "zero_timestamp"; anything else collapses to "unknown" so a caller bug
// cannot turn a session UUID into a Prometheus label. ADR-0017 D8.
func (c *Collector) RecordSessionAnchorAnomaly(kind string) {
	if c == nil || c.sessionAnchorAnomalies == nil {
		return
	}
	if kind != "missing" && kind != "zero_timestamp" {
		kind = "unknown"
	}
	c.sessionAnchorAnomalies.WithLabelValues(kind).Inc()
}
```

Add the three families to the package doc block, in the style of the existing entries.

- [ ] **Step 4: Run to verify pass**

Run: `cd backend && go test ./pkg/sdk/metrics/ -count=1`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
cd /home/tore/orkestra
git add backend/pkg/sdk/metrics/
git commit -m "feat(metrics): session-cap expiry, event-failure and anchor-anomaly counters

Closed label schemas per ADR-0017 D8: the anomaly kind is a two-value
allowlist and everything else collapses to unknown, so a caller bug cannot
turn a session UUID into a time series."
```

---

### Task 10: Enforce the cap on both refresh paths

Enforcing only on the rotation endpoint would leave a hole: `/session` mints an access token **without** rotating the refresh cookie — a deliberate anti-replay split — so a client calling solely that endpoint would hold a session open indefinitely. This is the easiest mistake to make in this PR and gets a dedicated test.

Reaching the cap performs a **logout**, not a denial. A bare denial would leave the in-flight access token valid until its natural expiry and would repeat the lookup on every subsequent request.

**Files:**
- Modify: `backend/internal/core/auth/services/session_cap.go` (helper, sentinels, anchor resolver)
- Modify: `backend/internal/core/auth/services/auth_service.go` (both call sites)
- Modify: `backend/internal/core/auth/services/gates_fakes_test.go` (`gateSessionRepo.GetByUUID`)
- Create: `backend/internal/core/auth/services/session_cap_enforcement_test.go`

**Interfaces:**
- Consumes: `(*AuthPolicyService).SessionAbsoluteTTL` (Task 7); `AuthSessionRepository.ExpireSessionForMaxAge`, `models.RevokeReasonSessionMaxAge` (Task 8); the three recorders (Task 9).
- Produces: `services.ErrSessionMaxAgeReached`, `services.ErrSessionEnforcementUnavailable` — Task 11 maps both.
- Produces: `(*authService).sessionWithinAbsoluteCap(ctx context.Context, sessionUUID string) error`.

- [ ] **Step 1: Make the session fake serve rows**

In `backend/internal/core/auth/services/gates_fakes_test.go`, replace the panicking `GetByUUID`:

```go
// GetByUUID serves rows the fake was told about via CreateSession or
// seedSession. The absolute-cap helper reads this on every refresh, so a
// panic here would make every cap test a panic test.
func (r *gateSessionRepo) GetByUUID(_ context.Context, uuid string) (*authModels.AuthSessionDoc, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.getErr != nil {
		return nil, r.getErr
	}
	for _, d := range r.created {
		if d.UUID == uuid {
			c := *d
			return &c, nil
		}
	}
	return nil, nil
}

// seedSession inserts a row directly, so a test can set StartedAt in the
// past without going through CreateSession (which stamps time.Now()).
func (r *gateSessionRepo) seedSession(doc *authModels.AuthSessionDoc) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.created = append(r.created, doc)
}
```

Add `getErr error` to the `gateSessionRepo` struct so a test can force a repository failure.

- [ ] **Step 2: Write the failing tests**

Create `backend/internal/core/auth/services/session_cap_enforcement_test.go`:

```go
package services

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	authModels "github.com/orkestra/backend/internal/core/auth/models"
	"github.com/orkestra/backend/pkg/sdk/iface"
)

// capEnv wires an orchestration env whose session repo is reachable and
// whose policy carries an explicit cap.
type capEnv struct {
	*orchestrationEnv
	sessions *gateSessionRepo
}

func newCapEnv(t *testing.T, capValue string) *capEnv {
	t.Helper()
	base := newOrchestrationEnv(t)
	sessions := newGateSessionRepo()
	svc, err := NewAuthService(&AuthConfig{
		UserService:       base.users,
		TenantProvider:    gateTenantProvider{},
		OAuthProviderRepo: base.oauth,
		RefreshTokenRepo:  base.refresh,
		AuthSessionRepo:   sessions,
		JWTService:        base.jwt,
		FirstAdminClaimer: newGateClaimer(),
	})
	if err != nil {
		t.Fatalf("NewAuthService: %v", err)
	}
	svc.SetPolicy(newPolicy(map[string]string{"sessionAbsoluteTTL": capValue}))
	base.auth = svc
	return &capEnv{orchestrationEnv: base, sessions: sessions}
}

func (e *capEnv) seedUserAndSession(t *testing.T, startedAt time.Time) (*iface.User, string) {
	t.Helper()
	user := &iface.User{UUID: "u-cap", Email: "cap@example.com", Role: "operator", IsActive: true}
	e.users.seed(user)
	e.sessions.seedSession(&authModels.AuthSessionDoc{
		UUID: "sess-A", UserUUID: user.UUID, DeviceID: "dev-A",
		IsActive: true, StartedAt: startedAt, CreatedAt: startedAt,
	})
	token, _ := e.issueAndSeedRefresh(user, "fam-cap")
	return user, token
}

func TestRefresh_DeniedPastAbsoluteCap(t *testing.T) {
	env := newCapEnv(t, "24h")
	_, token := env.seedUserAndSession(t, time.Now().Add(-25*time.Hour))

	_, err := env.auth.RefreshTokensWithRiskAssessment(context.Background(), token, nil)
	if !errors.Is(err, ErrSessionMaxAgeReached) {
		t.Fatalf("refresh past the cap = %v, want ErrSessionMaxAgeReached", err)
	}
}

// The non-rotating path is NOT optional. /session mints access tokens
// without rotating the refresh cookie, so a client that calls only that
// endpoint would hold a session open forever if the cap lived on the
// rotation endpoint alone. ADR-0017 D3.
func TestMintAccessToken_DeniedPastAbsoluteCap(t *testing.T) {
	env := newCapEnv(t, "24h")
	_, token := env.seedUserAndSession(t, time.Now().Add(-25*time.Hour))

	_, err := env.auth.MintAccessTokenFromRefresh(context.Background(), token, &authModels.SecurityContext{})
	if !errors.Is(err, ErrSessionMaxAgeReached) {
		t.Fatalf("bootstrap past the cap = %v, want ErrSessionMaxAgeReached — /session must not be a bypass", err)
	}
}

// Rotation must not extend the cap. Two rotations, the second past the
// boundary, must fail even though the token presented was minted seconds
// earlier: the anchor is the session, not the token.
func TestRefresh_AllowedWithinAbsoluteCap(t *testing.T) {
	env := newCapEnv(t, "24h")
	_, token := env.seedUserAndSession(t, time.Now().Add(-23*time.Hour))

	resp, err := env.auth.RefreshTokensWithRiskAssessment(context.Background(), token, nil)
	if err != nil {
		t.Fatalf("refresh inside the cap: %v", err)
	}
	// Age the session past the cap and present the FRESH token.
	env.sessions.ageSession(t, "sess-A", -25*time.Hour)
	if _, err := env.auth.RefreshTokensWithRiskAssessment(context.Background(), resp.RefreshToken, nil); !errors.Is(err, ErrSessionMaxAgeReached) {
		t.Fatalf("second refresh = %v, want ErrSessionMaxAgeReached — rotation must not restart the clock", err)
	}
}

func TestSessionCapExpiry_RevokesFamilyAndSession(t *testing.T) {
	env := newCapEnv(t, "24h")
	_, token := env.seedUserAndSession(t, time.Now().Add(-25*time.Hour))

	_, _ = env.auth.RefreshTokensWithRiskAssessment(context.Background(), token, nil)

	sess, _ := env.sessions.GetByUUID(context.Background(), "sess-A")
	if sess == nil || sess.IsActive {
		t.Errorf("session still active after cap expiry: %+v — reaching the cap is a logout, not a denial")
	}
	doc, _ := env.refresh.GetByTokenAny(context.Background(), hashOf(token))
	if doc == nil || !doc.IsRevoked {
		t.Errorf("refresh row not revoked after cap expiry: %+v", doc)
	}
}

func TestSessionCap_DisabledWhenUnset(t *testing.T) {
	env := newCapEnv(t, "")
	_, token := env.seedUserAndSession(t, time.Now().Add(-10*365*24*time.Hour))
	env.sessions.failEveryGet(t) // an empty cap must skip the query entirely

	if _, err := env.auth.RefreshTokensWithRiskAssessment(context.Background(), token, nil); err != nil {
		t.Fatalf("cap disabled must not query the session repo or block the refresh: %v", err)
	}
}

func TestSessionCap_MissingSessionRow(t *testing.T) {
	// Pins ADR-0017's temporary compatibility rule so changing it is
	// deliberate. A clean not-found PERMITS the refresh and increments
	// the anomaly counter; it must be tightened to fail-closed in the
	// first minor release after 30 consecutive production days at zero.
	env := newCapEnv(t, "24h")
	user := &iface.User{UUID: "u-orphan", Email: "orphan@example.com", Role: "operator", IsActive: true}
	env.users.seed(user)
	token, _ := env.issueAndSeedRefresh(user, "fam-orphan") // no session row seeded

	if _, err := env.auth.RefreshTokensWithRiskAssessment(context.Background(), token, nil); err != nil {
		t.Fatalf("missing session row must fail OPEN during the compatibility window: %v", err)
	}
}

func TestSessionCap_ZeroTimestampRowFallsBackToCreatedAt(t *testing.T) {
	// StartedAt zero but CreatedAt usable is NOT an anomaly — it has a
	// perfectly good anchor, and counting it would poison the 30-day
	// observation window that gates the fail-closed change.
	env := newCapEnv(t, "24h")
	user := &iface.User{UUID: "u-cap", Email: "cap@example.com", Role: "operator", IsActive: true}
	env.users.seed(user)
	env.sessions.seedSession(&authModels.AuthSessionDoc{
		UUID: "sess-A", UserUUID: user.UUID, DeviceID: "dev-A", IsActive: true,
		CreatedAt: time.Now().Add(-25 * time.Hour), // StartedAt deliberately zero
	})
	token, _ := env.issueAndSeedRefresh(user, "fam-cap")

	if _, err := env.auth.RefreshTokensWithRiskAssessment(context.Background(), token, nil); !errors.Is(err, ErrSessionMaxAgeReached) {
		t.Fatalf("CreatedAt must serve as the compatibility anchor: %v", err)
	}
}

func TestSessionCap_RepositoryErrorFailsClosed(t *testing.T) {
	// A database failure is not a compatibility miss. Compatibility
	// telemetry is not permission to accept refreshes during an outage.
	env := newCapEnv(t, "24h")
	_, token := env.seedUserAndSession(t, time.Now().Add(-1*time.Hour))
	env.sessions.failEveryGet(t)

	_, err := env.auth.RefreshTokensWithRiskAssessment(context.Background(), token, nil)
	if !errors.Is(err, ErrSessionEnforcementUnavailable) {
		t.Fatalf("repository error = %v, want ErrSessionEnforcementUnavailable (fail closed)", err)
	}
	if errors.Is(err, ErrSessionMaxAgeReached) {
		t.Fatal("an outage must never be reported as a cap expiry")
	}
}

func TestSessionCap_ConcurrentExpiryCountedOnce(t *testing.T) {
	env := newCapEnv(t, "24h")
	user, token := env.seedUserAndSession(t, time.Now().Add(-25*time.Hour))
	_ = user
	second, _ := env.issueAndSeedRefresh(seededCapUser(env), "fam-cap-2")

	var wg sync.WaitGroup
	wg.Add(2)
	for _, tk := range []string{token, second} {
		go func(tk string) {
			defer wg.Done()
			_, _ = env.auth.RefreshTokensWithRiskAssessment(context.Background(), tk, nil)
		}(tk)
	}
	wg.Wait()

	if n := env.sessions.expiryTransitions("sess-A"); n != 1 {
		t.Fatalf("winning transitions = %d, want exactly 1 — two refreshes must not double-count the event or the metric", n)
	}
}
```

Add the three fake helpers this test needs (`ageSession`, `failEveryGet`, `expiryTransitions`, `seededCapUser`) to `gates_fakes_test.go` alongside `seedSession`. `hashOf` is `utils.HashRefreshToken` — import it or reuse the existing helper in `refresh_orchestration_test.go`.

- [ ] **Step 3: Run to verify failure**

Run: `cd backend && go test ./internal/core/auth/services/ -run TestSessionCap -count=1`
Expected: FAIL — `undefined: ErrSessionMaxAgeReached`

- [ ] **Step 4: Implement the helper**

Append to `backend/internal/core/auth/services/session_cap.go`:

```go
// ErrSessionMaxAgeReached means the session hit its configured absolute
// lifetime and has been terminated. It is a LOGOUT, not a denial: by the
// time a caller sees this sentinel the session's refresh tokens are
// revoked, the session document is inactive, and the sid is on the
// revocation denylist. ADR-0017 D4.
var ErrSessionMaxAgeReached = errors.New("session maximum age reached")

// ErrSessionEnforcementUnavailable means the cap could not be evaluated
// or could not be applied because durable storage failed. It fails
// CLOSED — no credentials are minted — and maps to 503, never to a 401.
// Reporting an outage as an authentication failure would train clients to
// discard a session that is still perfectly valid.
var ErrSessionEnforcementUnavailable = errors.New("session enforcement unavailable")

// sessionCapAnchor resolves the instant the cap is measured from, or a
// non-empty anomaly kind when the row cannot supply one.
//
// StartedAt is the anchor: CreateSession stamps it unconditionally and
// rotation preserves the session UUID, so it is the login time and it
// survives every refresh. CreatedAt is the compatibility fallback for
// rows written before that guarantee, and using it is NOT an anomaly —
// counting a row that has a usable anchor would poison the 30-day
// observation window that gates the fail-closed change.
func sessionCapAnchor(sess *models.AuthSessionDoc) (time.Time, string) {
	if sess == nil {
		return time.Time{}, "missing"
	}
	if !sess.StartedAt.IsZero() {
		return sess.StartedAt, ""
	}
	if !sess.CreatedAt.IsZero() {
		return sess.CreatedAt, ""
	}
	return time.Time{}, "zero_timestamp"
}

// sessionWithinAbsoluteCap returns nil while the session may keep
// refreshing, ErrSessionMaxAgeReached once it has been terminated for
// age, or ErrSessionEnforcementUnavailable when durable state failed.
//
// Failure precedence is explicit and load-bearing:
//   - a repository ERROR loading the session, or revoking durable
//     refresh/session state, fails closed and never mints credentials;
//   - only a clean (nil, nil) lookup follows the temporary compatibility
//     rule below;
//   - a Redis denylist failure AFTER durable revocation returns
//     SessionRevocationDegradedError — durable logout happened, so the
//     caller must still clear the cookie, but the response must not claim
//     a completely recorded cap expiry;
//   - the cap event and counter are emitted only after durable state is
//     terminated, and only by the caller that won the transition.
//
// COMPATIBILITY WINDOW — ADR-0017, remove in the first minor release
// after at least 30 consecutive production days with
// orkestra_auth_session_anchor_anomalies_total at zero in every supported
// environment. Tracking issue: <filled in by the implementing PR>. D2's
// invariant makes an absent session document impossible for credentials
// issued by current code, but invariants bind only the code written after
// them and older rows cannot be assumed to comply. If the counter moves,
// classify and repair the data cause before restarting the window.
func (s *authService) sessionWithinAbsoluteCap(ctx context.Context, sessionUUID string) error {
	if s.authSessionRepo == nil || sessionUUID == "" {
		return nil
	}
	maxAge := s.policy.SessionAbsoluteTTL(ctx)
	if maxAge <= 0 {
		// Disabled: skip the query entirely. This is the exit for a fork
		// that does not want the cap, and it must cost nothing.
		return nil
	}

	sess, err := s.authSessionRepo.GetByUUID(ctx, sessionUUID)
	if err != nil {
		slogDefault().ErrorContext(ctx, "session cap: anchor lookup failed",
			slog.String("outcome", "fail_closed"),
			slog.String("error", err.Error()))
		return ErrSessionEnforcementUnavailable
	}

	anchor, anomaly := sessionCapAnchor(sess)
	if anomaly != "" {
		metrics.Default().RecordSessionAnchorAnomaly(anomaly)
		slogDefault().WarnContext(ctx, "session cap: no usable anchor, permitting refresh under the ADR-0017 compatibility window",
			slog.String("kind", anomaly))
		return nil
	}
	if time.Since(anchor) < maxAge {
		return nil
	}
	return s.expireSessionForMaxAge(ctx, sess)
}

// expireSessionForMaxAge performs the same three durable steps as an
// administrative termination — revoke the session's refresh tokens, flip
// the session document inactive, push the sid onto the denylist — and
// records the event exactly once. Revoking refresh rows is idempotent, so
// only the isActive transition needs to name a winner.
func (s *authService) expireSessionForMaxAge(ctx context.Context, sess *models.AuthSessionDoc) error {
	if s.refreshTokenRepo != nil {
		if err := s.refreshTokenRepo.RevokeTokensBySession(ctx, sess.UUID, models.RevokeReasonSessionMaxAge); err != nil {
			slogDefault().ErrorContext(ctx, "session cap: refresh revocation failed",
				slog.String("outcome", "fail_closed"),
				slog.String("error", err.Error()))
			return ErrSessionEnforcementUnavailable
		}
	}

	won, err := s.authSessionRepo.ExpireSessionForMaxAge(ctx, sess.UUID)
	if err != nil {
		slogDefault().ErrorContext(ctx, "session cap: session termination failed",
			slog.String("outcome", "fail_closed"),
			slog.String("error", err.Error()))
		return ErrSessionEnforcementUnavailable
	}

	var degraded error
	if s.sessionRevocation != nil {
		if err := s.sessionRevocation.Revoke(ctx, sess.UUID, models.RevokeReasonSessionMaxAge); err != nil {
			degraded = &SessionRevocationDegradedError{Cause: err}
		}
	}

	if won {
		metrics.Default().RecordSessionCapExpiry()
		s.recordSessionCapEvent(ctx, sess.UserUUID)
	}
	if degraded != nil {
		// Durable logout completed; only the short-lived denylist is
		// behind. The caller still clears the cookie, but must not report
		// a cleanly recorded cap expiry.
		return degraded
	}
	return ErrSessionMaxAgeReached
}

// recordSessionCapEvent writes the security-event row. A failure here
// cannot restore credentials — durable state is already terminated — so
// it increments a counter and logs without PII rather than propagating.
func (s *authService) recordSessionCapEvent(ctx context.Context, userUUID string) {
	if s.securityEventRepo == nil || userUUID == "" {
		return
	}
	ip, _ := ipFromCtx(ctx)
	event := &models.SecurityEvent{
		UserUUID:  userUUID,
		EventType: "session_max_age_reached",
		IPAddress: ip,
		Success:   true,
		Timestamp: time.Now().UTC(),
	}
	if err := s.securityEventRepo.Insert(ctx, event); err != nil {
		metrics.Default().RecordSessionCapEventFailure()
		slogDefault().WarnContext(ctx, "session cap: security event persist failed",
			slog.String("error", err.Error()))
	}
}
```

Imports for `session_cap.go`: `context`, `errors`, `log/slog`, `strings`, `time`, `authModels "…/internal/core/auth/models"` (aliased as `models` to match the file's use — check how `auth_service.go` names it and match), and `"…/pkg/sdk/metrics"`.

Note the nil-policy path: `s.policy.SessionAbsoluteTTL(ctx)` is called on a possibly-nil `*AuthPolicyService`, which is safe — the accessor has a nil receiver guard and returns the secure default. That is deliberate: a deployment that forgot to wire the policy still gets the cap.

- [ ] **Step 5: Call it from both paths**

In `RefreshTokensWithRiskAssessment`, immediately before `pair, err := s.jwtService.GenerateTokenPairWithAMR(...)` (after `newSessionID`, `device` and `security` are built):

```go
	// Absolute session cap. Placed after the row's revocation/expiry
	// checks and before the mint, so a capped session never receives a
	// token pair. ADR-0017 D3.
	if err := s.sessionWithinAbsoluteCap(ctx, newSessionID); err != nil {
		return nil, err
	}
```

In `MintAccessTokenFromRefresh`, immediately before `access, err := s.jwtService.GenerateAccessTokenForSessionWithAMR(...)`:

```go
	// The same cap on the non-rotating path. /session mints without
	// rotating, so omitting this would let a client that calls only the
	// bootstrap endpoint hold a session open indefinitely. ADR-0017 D3.
	if err := s.sessionWithinAbsoluteCap(ctx, doc.SessionUUID); err != nil {
		return nil, err
	}
```

- [ ] **Step 6: Run to verify pass**

Run: `cd backend && go test ./internal/core/auth/... -count=1`
Expected: PASS. If `TestSessionCap_DisabledWhenUnset` fails because the fake was queried, the `maxAge <= 0` early return is in the wrong place — fix that, not the test.

- [ ] **Step 7: File the fail-closed follow-up and link it**

```bash
cd /home/tore/orkestra
gh issue create --title "ADR-0017: tighten session-cap anchor anomalies to fail-closed" --body "$(cat <<'BODY'
ADR-0017 permits a refresh when the session-cap anchor cannot be read, for a
measured compatibility window only.

**Gate:** at least 30 consecutive production days with
`orkestra_auth_session_anchor_anomalies_total` at zero across every supported
environment (both `kind="missing"` and `kind="zero_timestamp"`).

**Action when the gate is met:** in the next minor release, change both anomaly
cases in `sessionWithinAbsoluteCap` (backend/internal/core/auth/services/session_cap.go)
to return `ErrSessionEnforcementUnavailable` instead of nil, and delete the
compatibility comment block.

**If the counter moves:** classify and repair the data cause, then restart the
30-day window. Do not extend the window silently.
BODY
)"
```

Replace `<filled in by the implementing PR>` in the compatibility comment with the issue URL the command prints.

- [ ] **Step 8: Update the module contract doc**

In `backend/internal/core/auth/CLAUDE.md`, add an invariant bullet next to "A session has one canonical SID":

> **A session has a maximum age, and both refresh paths enforce it.** `sessionAbsoluteTTL` (default 30d, empty disables) bounds total session age from `session.StartedAt`, independently of activity — the refresh TTL is the *idle* timeout, not the cap. `RefreshTokensWithRiskAssessment` **and** `MintAccessTokenFromRefresh` both call `sessionWithinAbsoluteCap`: `/session` mints without rotating, so enforcing on the rotation endpoint alone would let a bootstrap-only client hold a session open forever. Reaching the cap is a **logout** — refresh tokens revoked, session inactive, sid denylisted — not a denial, because a denial would leave the in-flight access token valid until its natural expiry. Repository failures fail closed to 503 `session_enforcement_unavailable`; only a clean not-found fails open, under a measured compatibility window counted by `orkestra_auth_session_anchor_anomalies_total`.

- [ ] **Step 9: Commit**

```bash
cd /home/tore/orkestra
git add backend/internal/core/auth/services/ backend/internal/core/auth/CLAUDE.md
git commit -m "feat(auth): enforce an absolute session cap on both refresh paths

ADR-0017 D1/D3/D4. Anchored on session.StartedAt, so no schema change and no
backfill. Reaching the cap revokes the session's refresh tokens, marks the
session inactive and denylists the sid. Repository failures fail closed; only
a clean not-found fails open, under a metered compatibility window."
```

---

### Task 11: Surface the cap to clients

Two distinct outcomes must not collapse into one: a 401 for "your session reached its maximum age" and a 503 for "we could not evaluate the cap right now". Turning a repository outage into a 401 would train clients to discard a session that is still valid.

Redux cleanup is not a substitute for expiring the HttpOnly credential — the handler must send the expiring `Set-Cookie`.

**Files:**
- Modify: `backend/internal/core/auth/handlers/auth_handler.go` (`writeRefreshErr` at `:2560`, `refreshFailureOutcome` at `:2231`, the three `writeRefreshErr` call sites at `:1593`, `:1774`, `:2118`)
- Create: `backend/internal/core/auth/handlers/session_cap_response_test.go`
- Modify: `frontend-admin/src/store/api/baseApi.ts:182-200`
- Create: `frontend-admin/src/store/api/baseApi.sessionEnded.test.ts`

**Interfaces:**
- Consumes: `services.ErrSessionMaxAgeReached`, `services.ErrSessionEnforcementUnavailable`, `*services.SessionRevocationDegradedError` (Task 10).
- Produces: response codes `session_max_age_reached` (401) and `session_enforcement_unavailable` (503).
- `frontend-client` needs **no change** — `src/api/client.ts:46` already treats a failed refresh as logout. `mobile` has no refresh logic and is unaffected.

- [ ] **Step 1: Write the failing backend test**

Create `backend/internal/core/auth/handlers/session_cap_response_test.go`:

```go
package handlers

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/orkestra/backend/internal/core/auth/services"
)

func decodeBody(t *testing.T, rec *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var body map[string]any
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	return body
}

func TestWriteRefreshErr_SessionMaxAgeReached(t *testing.T) {
	rec := httptest.NewRecorder()
	writeRefreshErr(rec, services.ErrSessionMaxAgeReached)
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", rec.Code)
	}
	if got := decodeBody(t, rec)["code"]; got != "session_max_age_reached" {
		t.Errorf("code = %v, want session_max_age_reached — 'revoked' is inaccurate for a session that simply aged out, and the distinction matters to whoever reads the support ticket", got)
	}
}

func TestWriteRefreshErr_EnforcementUnavailableIs503(t *testing.T) {
	rec := httptest.NewRecorder()
	writeRefreshErr(rec, services.ErrSessionEnforcementUnavailable)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503 — a storage outage must not be reported as an authentication failure", rec.Code)
	}
	body := decodeBody(t, rec)
	if body["code"] != "session_enforcement_unavailable" {
		t.Errorf("code = %v, want session_enforcement_unavailable", body["code"])
	}
	for _, v := range body {
		if s, ok := v.(string); ok && strings.Contains(strings.ToLower(s), "mongo") {
			t.Errorf("response leaks internals: %q", s)
		}
	}
}

func TestWriteRefreshErr_DegradedIsGenericLogout(t *testing.T) {
	rec := httptest.NewRecorder()
	writeRefreshErr(rec, &services.SessionRevocationDegradedError{})
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", rec.Code)
	}
	if code, present := decodeBody(t, rec)["code"]; present {
		t.Errorf("code = %v, want none — a partially degraded cap logout must not claim a completely recorded cap expiry", code)
	}
}

func TestWriteRefreshErr_ReplayUnchanged(t *testing.T) {
	rec := httptest.NewRecorder()
	writeRefreshErr(rec, services.ErrRefreshTokenReplay)
	if got := decodeBody(t, rec)["code"]; got != "refresh_token_replay" {
		t.Errorf("code = %v, want refresh_token_replay", got)
	}
}
```

Add the cookie-expiry test in the same file. It exercises the helper directly rather than the whole HTTP handler — the helper *is* the decision, and `logout_identity_test.go` already establishes that an `AuthHandler` can be built from a literal with just the fields a unit needs:

```go
// Redux state cleanup is not a substitute for expiring the HttpOnly
// credential: the browser would keep presenting the dead cookie on every
// subsequent request. Enforcement unavailable is deliberately excluded —
// durable logout is not known to have completed there, and the client may
// legitimately retry once storage recovers.
func TestClearRefreshCookieOnTerminalRefreshErr(t *testing.T) {
	cases := []struct {
		name      string
		err       error
		wantClear bool
	}{
		{"cap expiry clears", services.ErrSessionMaxAgeReached, true},
		{"degraded cap logout clears", &services.SessionRevocationDegradedError{}, true},
		{"enforcement unavailable keeps the cookie", services.ErrSessionEnforcementUnavailable, false},
		{"replay keeps today's behaviour", services.ErrRefreshTokenReplay, false},
		{"invalid token keeps today's behaviour", services.ErrInvalidRefreshToken, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := &config.Config{}
			cfg.Auth.Cookie.Name = logoutTestCookieName
			h := &AuthHandler{config: cfg}

			rec := httptest.NewRecorder()
			h.clearRefreshCookieOnTerminalRefreshErr(rec, logoutTestCookieName, tc.err)

			var cleared bool
			for _, c := range rec.Result().Cookies() {
				if c.Name == logoutTestCookieName && c.MaxAge < 0 {
					cleared = true
				}
			}
			if cleared != tc.wantClear {
				t.Errorf("cookie cleared = %v, want %v", cleared, tc.wantClear)
			}
		})
	}
}
```

`services.ErrInvalidRefreshToken` and `logoutTestCookieName` already exist; add `"net/http/httptest"` and the config import if this new file does not have them.

- [ ] **Step 2: Run to verify failure**

Run: `cd backend && go test ./internal/core/auth/handlers/ -run 'TestWriteRefreshErr|TestRefreshHandler_ClearsCookie' -count=1`
Expected: FAIL — 401 with no code / no `Set-Cookie`

- [ ] **Step 3: Extend the error writer**

Replace `writeRefreshErr` in `backend/internal/core/auth/handlers/auth_handler.go`:

```go
// writeRefreshErr writes the JSON error for a refresh-flow failure.
//
// Four outcomes are deliberately distinct:
//   - 503 session_enforcement_unavailable — the cap could not be
//     evaluated or applied because storage failed. NOT a 401: reporting
//     an outage as an authentication failure would train clients to
//     discard a session that is still perfectly valid, and the caller may
//     retry once storage recovers.
//   - 401 session_max_age_reached — the session hit its configured
//     maximum age and has been logged out. "Revoked" is inaccurate for a
//     session that simply aged out, and the distinction matters to
//     whoever reads the support ticket.
//   - 401 refresh_token_replay — reuse detected, family killed.
//   - 401 with no code — everything else, including a partially degraded
//     cap logout, which must not claim a completely recorded cap expiry.
func writeRefreshErr(w http.ResponseWriter, err error) {
	if errors.Is(err, services.ErrSessionEnforcementUnavailable) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusServiceUnavailable)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"status": http.StatusServiceUnavailable,
			"title":  "Service Unavailable",
			"detail": "session enforcement is temporarily unavailable — please retry",
			"code":   "session_enforcement_unavailable",
		})
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusUnauthorized)
	body := map[string]any{
		"status": http.StatusUnauthorized,
		"title":  "Unauthorized",
		"detail": "Invalid refresh token",
	}
	switch {
	case errors.Is(err, services.ErrSessionMaxAgeReached):
		body["code"] = "session_max_age_reached"
		body["detail"] = "session reached its maximum age — please sign in again"
	case errors.Is(err, services.ErrRefreshTokenReplay):
		body["code"] = "refresh_token_replay"
		body["detail"] = "refresh token reuse detected — session revoked"
	}
	_ = json.NewEncoder(w).Encode(body)
}
```

Extend `refreshFailureOutcome` so the structured log distinguishes them:

```go
func refreshFailureOutcome(err error) string {
	switch {
	case errors.Is(err, services.ErrRefreshTokenReplay):
		return "replay_detected"
	case errors.Is(err, services.ErrSessionMaxAgeReached):
		return "session_max_age"
	case errors.Is(err, services.ErrSessionEnforcementUnavailable):
		return "enforcement_unavailable"
	}
	var degraded *services.SessionRevocationDegradedError
	if errors.As(err, &degraded) {
		return "session_max_age_revocation_degraded"
	}
	return "invalid_token"
}
```

- [ ] **Step 4: Clear the cookie on terminal cap failures**

Add near `clearStaleParentDomainCookies`:

```go
// clearRefreshCookieOnTerminalRefreshErr expires the HttpOnly refresh
// cookie when the failure means the session is durably gone: a cap
// expiry, or a cap logout whose only incomplete step was the short-lived
// Redis denylist. Redux state cleanup is not a substitute — the browser
// would keep presenting the dead cookie on every subsequent request.
//
// Deliberately NOT cleared on ErrSessionEnforcementUnavailable: durable
// logout is not known to have completed, and the client may legitimately
// retry when storage recovers.
func (h *AuthHandler) clearRefreshCookieOnTerminalRefreshErr(w http.ResponseWriter, cookieName string, err error) {
	var degraded *services.SessionRevocationDegradedError
	if errors.Is(err, services.ErrSessionMaxAgeReached) || errors.As(err, &degraded) {
		utils.ClearRefreshTokenCookie(w, cookieName, h.cookieDomain, h.config.Auth.Cookie.Secure)
	}
}
```

Call it immediately before each of the three `writeRefreshErr(w, …)` sites (`:1593`, `:1774`, `:2118`) — headers must be written before `writeRefreshErr` calls `WriteHeader`:

```go
		h.clearRefreshCookieOnTerminalRefreshErr(w, cookieName, lastErr)
		writeRefreshErr(w, lastErr)
		return
```

At `:1593` the variable is named `err`, not `lastErr` — check each site. At `:1593` confirm `cookieName` is in scope; if not, use `h.config.Auth.Cookie.Name`.

- [ ] **Step 5: Run to verify pass**

Run: `cd backend && go test ./internal/core/auth/... -count=1 && make ci-backend`
Expected: PASS

- [ ] **Step 6: Extend the frontend interception**

In `frontend-admin/src/store/api/baseApi.ts`, generalise the existing `session_revoked` branch. The existing branch uses a literal English string, so the new one matches that local precedent rather than introducing i18n into a file that has none:

```ts
    // Server-side session termination sets a code on the 401 body. Skip the
    // silent-refresh retry in both cases — a new access token minted from
    // the same refresh cookie would carry the same dead sid and just fail
    // again. The two codes share the logic and differ only in the message:
    // "revoked" is inaccurate for a session that simply reached its maximum
    // age, and the distinction matters to whoever reads the support ticket.
    const errorData = (result.error as { data?: { code?: string } }).data;
    const sessionEndedMessages: Record<string, string> = {
      session_revoked: 'Your session has been revoked. Please sign in again.',
      session_max_age_reached:
        'Your session reached its maximum age. Please sign in again.'
    };
    const sessionEndedMessage = errorData?.code
      ? sessionEndedMessages[errorData.code]
      : undefined;
    if (sessionEndedMessage) {
      api.dispatch(clearAccessToken());
      if (!isAuthCheck) {
        toast.error(sessionEndedMessage, {
          toastId: errorData!.code,
          autoClose: 5000
        });
      }
      if (navigateToLogin) {
        navigateToLogin(window.location.pathname);
      }
      return result;
    }
```

- [ ] **Step 7: Write the frontend test**

Create `frontend-admin/src/store/api/baseApi.sessionEnded.test.ts`, modelled on `baseApi.tenantHeader.test.ts` (same `server`/`setupStore`/`injectEndpoints` harness). **Every request the test fires must have an MSW handler** — vitest exits non-zero on an unhandled MSW request even with every assertion passing.

```ts
import { describe, it, expect, beforeEach, vi } from 'vitest';
import { http, HttpResponse } from 'msw';
import { server } from 'test/server';
import { setupStore } from 'test/render';
import { baseApi } from './baseApi';

const probeApi = baseApi.injectEndpoints({
  endpoints: builder => ({
    sessionEndedProbe: builder.query<unknown, string>({
      query: url => ({ url, method: 'GET' })
    })
  }),
  overrideExisting: true
});

const respondWith = (code: string) => {
  let refreshAttempts = 0;
  server.use(
    http.get('*/v1/some/resource', () =>
      HttpResponse.json({ code }, { status: 401 })
    ),
    // If the silent-refresh retry fires, this counts it. It must not.
    http.post('*/refresh*', () => {
      refreshAttempts += 1;
      return HttpResponse.json({}, { status: 200 });
    })
  );
  return () => refreshAttempts;
};

describe('baseApi session-ended interception', () => {
  beforeEach(() => {
    setupStore().dispatch(baseApi.util.resetApiState());
  });

  it.each(['session_revoked', 'session_max_age_reached'])(
    'clears state and skips the silent refresh on %s',
    async code => {
      const attempts = respondWith(code);
      const store = setupStore();
      await store.dispatch(
        probeApi.endpoints.sessionEndedProbe.initiate('/v1/some/resource')
      );
      expect(attempts()).toBe(0);
      expect(store.getState().auth.accessToken).toBeFalsy();
    }
  );
});
```

Adjust the state selector to the real slice shape (`clearAccessToken`'s reducer) — read `frontend-admin/src/store/slices/authSlice.ts` first.

- [ ] **Step 8: Run the frontend gate**

Run: `cd frontend-admin && npm run typecheck && npx vitest run src/store/api/baseApi.sessionEnded.test.ts && npm run lint`
Expected: PASS, exit code 0 (a non-zero exit with passing assertions means an unhandled MSW request — add the missing handler)

- [ ] **Step 9: Commit and open PR 2**

```bash
cd /home/tore/orkestra
git add backend/internal/core/auth/handlers/ frontend-admin/src/store/api/
git commit -m "feat(auth): surface session-cap expiry as its own code and clear the cookie

401 session_max_age_reached with the HttpOnly refresh cookie expired, and
503 session_enforcement_unavailable kept distinct from credential failures so
a storage outage is never reported as an authentication failure. The admin SPA
reuses its session_revoked branch with a message that says 'aged out' rather
than 'revoked'. ADR-0017 D4."
git push -u origin feat/adr-0017-pr2-session-cap
gh pr create --base dev --title "feat(auth): absolute session lifetime cap (ADR-0017 PR 2)" --body "$(cat <<'BODY'
Implements PR 2 of docs/superpowers/specs/2026-08-21-session-lifetime-design.md (ADR-0017 D1–D4, D8).

## ⚠️ Behaviour change inherited by every downstream fork

**Upgrading signs out sessions older than 30 days.** On the first refresh after
deployment, any session that began more than `sessionAbsoluteTTL` ago is
terminated and the user returns to the login screen. That is what the existing
data already records, not something this code arranges.

Deployments that need the previous semantics set `sessionAbsoluteTTL` to empty
at `/admin/modules` — that disables the cap and skips the query entirely.

## What changes

- New `sessionAbsoluteTTL` admin field (`auth` / `login` group, default `720h`,
  range 1h–89d, empty disables). One field for both audience tiers.
- Anchored on the existing `session.StartedAt` — **no schema change, no backfill,
  no compatibility branch.**
- Enforced on `RefreshTokensWithRiskAssessment` **and** `MintAccessTokenFromRefresh`.
  `/session` mints without rotating, so the second is not optional.
- Reaching the cap is a logout, not a denial: refresh tokens revoked, session
  inactive, sid denylisted — the same three steps as an administrative termination.
- New metrics: `orkestra_auth_session_cap_expiries_total`,
  `orkestra_auth_session_cap_event_failures_total`,
  `orkestra_auth_session_anchor_anomalies_total{kind}`.

## Compatibility window

A clean session-row not-found permits the refresh and increments the anomaly
counter. Follow-up filed to tighten this to fail-closed after 30 consecutive
production days at zero: <issue link>.

## Staging verification before promotion

Confirm sessions older than the cap are signed out and that
`orkestra_auth_session_cap_expiries_total` moves, then settles.
BODY
)"
```

---

# PR 3 — Retention and hygiene

Branch: `feat/adr-0017-pr3-auth-retention` off `dev`, **after PR 2 is merged.** Retention must never be able to delete an anchor the cap still depends on; the strict `MaxSessionAbsoluteTTL + SessionRetentionSafetyMargin <= AuthSessionRetention` bound introduced in PR 2 is what makes the pair safe.

Two mechanisms, because the semantics differ. For **sessions**, `expiresAt` *is* the retention deadline, so a TTL index expresses the intent exactly. For **refresh tokens**, no row may be deleted while its token could still pass temporal validation — regardless of whether it is active, rotated, or otherwise revoked — and a TTL index can express neither that combined rule nor the bounded per-cycle progress the first cleanup of an upgraded installation needs.

### Task 12: Session retention — one deadline, one index

`repository/auth_session_repository.go` falls back to **30** days while both callers write 90 (`AuthSessionRetention`), and nothing deletes at either boundary.

`TerminateExpiredSessions` is removed rather than wired: it is dead, and under retention semantics it is also wrong — it flips `isActive=false` at 90 days, when the session has been irrelevant for months. It is internal to the repository; no fork implements it.

**Files:**
- Modify: `backend/internal/core/auth/models/collections.go` (add `AuthSessionRetention`)
- Modify: `backend/internal/core/auth/services/password_auth_service.go:143-149` (remove the const, keep the explanation as a pointer)
- Modify: `backend/internal/core/auth/services/auth_service.go:1259`, `password_auth_service.go:1577` (reference `models.AuthSessionRetention`)
- Modify: `backend/internal/core/auth/services/session_cap_test.go` (the invariant test's reference)
- Modify: `backend/internal/core/auth/repository/auth_session_repository.go:161-165` (fallback), interface + impl (remove `TerminateExpiredSessions`)
- Modify: `backend/internal/core/auth/module.go:688-694` (session collection indexes)
- Create: `backend/internal/core/auth/module_session_ttl_test.go`

**Interfaces:**
- Produces: `models.AuthSessionRetention = 90 * 24 * time.Hour` — `services.AuthSessionRetention` ceases to exist; every reference moves.
- Removes: `AuthSessionRepository.TerminateExpiredSessions`.

- [ ] **Step 1: Write the failing tests**

Create `backend/internal/core/auth/module_session_ttl_test.go`, modelled on the existing `module_refresh_family_ttl_test.go`:

```go
package auth

import (
	"testing"

	"github.com/orkestra/backend/internal/core/auth/models"
)

// session.expiresAt IS the retention deadline (now + AuthSessionRetention),
// so a TTL index expresses the intent exactly — unlike refresh-token rows,
// whose deletion rule a TTL index cannot express. ADR-0017 D7.
func TestSessionCollectionsHaveRetentionTTLIndex(t *testing.T) {
	m := &AuthModule{}
	want := map[string]bool{
		models.OperatorSessionsCollection: false,
		models.ClientSessionsCollection:   false,
	}
	for _, collection := range m.Collections() {
		if _, ok := want[collection.Name]; !ok {
			continue
		}
		for _, index := range collection.Indexes {
			if index.ExpireAt && index.Keys["expiresAt"] == 1 {
				want[collection.Name] = true
			}
		}
	}
	for name, found := range want {
		if !found {
			t.Errorf("%s missing expiresAt ExpireAt index — session documents would accumulate forever", name)
		}
	}
}
```

Create `backend/internal/core/auth/repository/auth_session_retention_test.go`:

```go
package repository

import (
	"testing"
	"time"

	"github.com/orkestra/backend/internal/core/auth/models"
)

// The repository defaulted to 30 days while both service call sites wrote
// 90. With a TTL index on the field that discrepancy stops being cosmetic:
// a row created through the fallback would be deleted two months early.
func TestSessionRetentionFallbackMatchesCallers(t *testing.T) {
	if models.AuthSessionRetention != 90*24*time.Hour {
		t.Fatalf("AuthSessionRetention = %v, want 90d", models.AuthSessionRetention)
	}
	if sessionRetentionFallback != models.AuthSessionRetention {
		t.Errorf("repository fallback %v disagrees with the callers' %v", sessionRetentionFallback, models.AuthSessionRetention)
	}
}
```

- [ ] **Step 2: Run to verify failure**

Run: `cd backend && go test ./internal/core/auth/ ./internal/core/auth/repository/ -run 'TestSessionCollectionsHaveRetentionTTLIndex|TestSessionRetentionFallback' -count=1`
Expected: FAIL — no TTL index; `models.AuthSessionRetention` undefined

- [ ] **Step 3: Move the constant to models**

In `backend/internal/core/auth/models/collections.go`, next to the `AuthSessionDoc` type:

```go
// AuthSessionRetention is how long a session DOCUMENT is kept. It is a
// RETENTION deadline, not an auth gate: the row is audit and device
// history that the risk scorer reads, and nothing authenticates off it.
// The session's authentication lifetime is bounded separately by
// sessionAbsoluteTTL (ADR-0017 D1), which is capped a full day below this
// value so retention can never delete the anchor of a live session.
//
// It lives in models rather than services because the repository writes
// the same deadline the services do and cannot import services.
const AuthSessionRetention = 90 * 24 * time.Hour
```

Delete the const from `password_auth_service.go` and replace its comment block with a one-line pointer to `models.AuthSessionRetention`. Update the two write sites (`auth_service.go:1259`, `password_auth_service.go:1577`) to `now.Add(models.AuthSessionRetention)` and the PR 2 invariant test to `models.AuthSessionRetention`.

- [ ] **Step 4: Fix the repository fallback and drop the dead sweeper**

In `backend/internal/core/auth/repository/auth_session_repository.go`:

```go
// sessionRetentionFallback backstops CreateSession when the caller left
// ExpiresAt zero. It must equal the value the callers write: with a TTL
// index on the field, a disagreement deletes rows early rather than
// merely reading oddly.
const sessionRetentionFallback = models.AuthSessionRetention
```

and in `CreateSession`:

```go
	if session.ExpiresAt.IsZero() {
		session.ExpiresAt = now.Add(sessionRetentionFallback)
	}
```

Remove `TerminateExpiredSessions` from the interface and delete its implementation (lines ~392-410).

- [ ] **Step 5: Declare the TTL indexes**

In `backend/internal/core/auth/module.go`:

```go
		{Name: models.OperatorSessionsCollection, Indexes: []module.IndexSpec{
			{Keys: map[string]int{"uuid": 1}, Unique: true},
			// expiresAt is the retention deadline, so ExpireAt (delete AT
			// the timestamp) is the exact expression of the intent —
			// not TTL, which would add a second offset on top. ADR-0017 D7.
			{Keys: map[string]int{"expiresAt": 1}, ExpireAt: true},
		}},
		{Name: models.ClientSessionsCollection, Indexes: []module.IndexSpec{
			{Keys: map[string]int{"uuid": 1}, Unique: true},
			{Keys: map[string]int{"expiresAt": 1}, ExpireAt: true},
		}},
```

- [ ] **Step 6: Run to verify pass**

Run: `cd backend && go build ./... && go test ./internal/core/auth/... -count=1`
Expected: PASS

- [ ] **Step 7: Write down the rollout gate**

In `backend/internal/core/auth/CLAUDE.md`, in the collections table, change the sessions row's TTL column from `—` to `Yes — expiresAt is the 90-day retention deadline (models.AuthSessionRetention)`. Then correct the sentence below the table ("Refresh-token rows, sessions, and MFA factor rows are rotated/invalidated explicitly in the service layer") and the "Every new auth-adjacent collection needs a deliberate TTL decision" bullet, which currently gives sessions as the example of "no TTL because they're invalidated explicitly".

Add an operational warning:

> **Rollout gate.** A session document with a zero `expiresAt` serialises as a year-1 BSON date, and a TTL index deletes it **immediately**. The write path has always set the field, but "should not happen" is not sufficient warrant for an irreversible delete. Count them on staging *and* production before deploying: `db.operator_sessions.countDocuments({expiresAt: {$lt: ISODate("2000-01-01")}})` and the same for `client_sessions`. A non-zero count blocks the deploy.

- [ ] **Step 8: Commit**

```bash
cd /home/tore/orkestra
git add backend/internal/core/auth/models/collections.go backend/internal/core/auth/repository/ \
        backend/internal/core/auth/services/ backend/internal/core/auth/module.go \
        backend/internal/core/auth/module_session_ttl_test.go backend/internal/core/auth/CLAUDE.md
git commit -m "feat(auth): TTL index on session documents, retention fallback set to 90d

The repository defaulted to 30 days while both callers wrote 90; with a TTL
index on the field that would delete rows two months early. Removes the dead
TerminateExpiredSessions, which under retention semantics flipped isActive at
90 days on a session irrelevant for months. ADR-0017 D7."
```

---

### Task 13: Bounded refresh-token sweep

The safe invariant is narrower and stronger than the rule it replaces (`auth/CLAUDE.md:432`, "revoked rows are retained for one refresh TTL after revocation"): **no row is deleted while its token could still pass temporal validation, regardless of revocation state.** Once `expiresAt` is past, replaying the token cannot mint credentials and the row may be deleted. That stays correct even if `JWT_REFRESH_TOKEN_EXPIRY` changes between restarts, which the old rule did not. The durable family fence survives independently with its own TTL index.

`CleanupRevokedTokens` is **removed**, not wired: revocation age alone is never a safe deletion criterion.

The batch never runs `CountDocuments` on the hot drain path — it fetches one row beyond the batch to decide whether more work exists.

**Files:**
- Modify: `backend/internal/core/auth/repository/refresh_token_repository.go` (interface `:63-73`, impl `:708-736`)
- Modify: `backend/internal/core/auth/module.go` (refresh-token collection indexes, `:670-680`)
- Create: `backend/internal/core/auth/repository/refresh_token_sweep_test.go`

**Interfaces:**
- Produces: `RefreshTokenRepository.CleanupExpiredTokens(ctx context.Context, limit int) (deleted int64, hasMore bool, err error)` — **signature change**, the old `(int64, error)` form is gone.
- Produces: `RefreshTokenRepository.CountExpiredTokens(ctx context.Context) (int64, error)` — Task 16 calls it once on entry to drain mode.
- Removes: `RefreshTokenRepository.CleanupRevokedTokens`.
- Produces: `repository.SweepBatchLimit = 5000` — Task 16 passes it.

- [ ] **Step 1: Write the failing tests**

Create `backend/internal/core/auth/repository/refresh_token_sweep_test.go`, using the package's existing `liveRefreshRepository(t)` helper from `refresh_token_repository_concurrency_test.go` (it skips unless `MONGO_TEST_URI`/`MONGO_URI` is set and drops a per-run database):

```go
package repository

import (
	"context"
	"testing"
	"time"

	"github.com/orkestra/backend/internal/core/auth/models"
	"go.mongodb.org/mongo-driver/bson"
)

func insertSweepRow(t *testing.T, repo *refreshTokenRepository, uuidStr string, expiresIn time.Duration, revokedAgo time.Duration) {
	t.Helper()
	doc := &models.RefreshTokenDoc{
		UUID: uuidStr, UserUUID: "sweep-user", Token: "hash-" + uuidStr,
		SessionUUID: "sweep-session", DeviceID: "sweep-device",
		IssuedAt: time.Now(), CreatedAt: time.Now(),
		ExpiresAt: time.Now().Add(expiresIn),
	}
	if revokedAgo > 0 {
		revokedAt := time.Now().Add(-revokedAgo)
		doc.IsRevoked = true
		doc.RevokedAt = &revokedAt
		doc.RevokedReason = models.RevokeReasonRotated
	}
	if _, err := repo.collection.InsertOne(context.Background(), doc); err != nil {
		t.Fatalf("insert %s: %v", uuidStr, err)
	}
}

func rowExists(t *testing.T, repo *refreshTokenRepository, uuidStr string) bool {
	t.Helper()
	n, err := repo.collection.CountDocuments(context.Background(), bson.M{"uuid": uuidStr})
	if err != nil {
		t.Fatalf("count %s: %v", uuidStr, err)
	}
	return n == 1
}

// The deletion rule: a row may be deleted only once its token can no
// longer pass temporal validation. Revocation state is irrelevant in both
// directions — an unexpired rotated row must survive, because that row is
// exactly what replay detection matches a reused token against; an
// expired active row may go, because replaying it cannot mint anything.
// This is strictly narrower than the old "one refresh TTL after
// revocation" rule and stays correct across a JWT_REFRESH_TOKEN_EXPIRY
// change between restarts. ADR-0017 D7.
func TestSweep_NeverDeletesAnUnexpiredRow(t *testing.T) {
	repo, cleanup := liveRefreshRepository(t)
	defer cleanup()

	insertSweepRow(t, repo, "keep-recently-revoked", 6*24*time.Hour, time.Hour)
	insertSweepRow(t, repo, "keep-long-revoked", 24*time.Hour, 30*24*time.Hour)
	insertSweepRow(t, repo, "delete-expired-active", -time.Hour, 0)
	insertSweepRow(t, repo, "delete-expired-revoked", -time.Hour, 30*24*time.Hour)

	deleted, hasMore, err := repo.CleanupExpiredTokens(context.Background(), SweepBatchLimit)
	if err != nil {
		t.Fatalf("CleanupExpiredTokens: %v", err)
	}
	if deleted != 2 {
		t.Errorf("deleted = %d, want 2", deleted)
	}
	if hasMore {
		t.Error("hasMore = true with only 2 eligible rows")
	}
	for _, id := range []string{"keep-recently-revoked", "keep-long-revoked"} {
		if !rowExists(t, repo, id) {
			t.Errorf("%s was deleted while its token could still pass temporal validation", id)
		}
	}
	for _, id := range []string{"delete-expired-active", "delete-expired-revoked"} {
		if rowExists(t, repo, id) {
			t.Errorf("%s survived despite being expired", id)
		}
	}
}

// One cycle deletes at most the batch bound and reports hasMore from the
// (limit+1)-th SELECTED row — never from CountDocuments, which on the
// five-minute drain cadence would scan the whole eligible range 288 times
// a day to answer a yes/no question.
func TestSweep_BatchIsBoundedAndReportsHasMore(t *testing.T) {
	repo, cleanup := liveRefreshRepository(t)
	defer cleanup()

	// A small limit keeps the test fast while exercising the same code
	// path; SweepBatchLimit itself is a constant, not behaviour.
	const limit = 10
	for i := 0; i < limit+5; i++ {
		insertSweepRow(t, repo, "expired-"+string(rune('a'+i)), -time.Hour, 0)
	}

	deleted, hasMore, err := repo.CleanupExpiredTokens(context.Background(), limit)
	if err != nil {
		t.Fatalf("CleanupExpiredTokens: %v", err)
	}
	if deleted != limit {
		t.Errorf("deleted = %d, want exactly the batch bound %d", deleted, limit)
	}
	if !hasMore {
		t.Error("hasMore = false with 5 rows left; the scheduler would drop to the 6-hour idle cadence mid-drain")
	}

	deleted, hasMore, err = repo.CleanupExpiredTokens(context.Background(), limit)
	if err != nil {
		t.Fatalf("second batch: %v", err)
	}
	if deleted != 5 || hasMore {
		t.Errorf("second batch = (%d, %v), want (5, false)", deleted, hasMore)
	}
}

func TestCountExpiredTokens_CountsOnlyEligibleRows(t *testing.T) {
	repo, cleanup := liveRefreshRepository(t)
	defer cleanup()
	insertSweepRow(t, repo, "live", 24*time.Hour, 0)
	insertSweepRow(t, repo, "expired-1", -time.Hour, 0)
	insertSweepRow(t, repo, "expired-2", -time.Hour, 30*24*time.Hour)

	n, err := repo.CountExpiredTokens(context.Background())
	if err != nil {
		t.Fatalf("CountExpiredTokens: %v", err)
	}
	if n != 2 {
		t.Errorf("count = %d, want 2", n)
	}
}
```

Confirm `repo.collection` is reachable from the test (the helper returns the concrete `*refreshTokenRepository`, so it is). Run these with `MONGO_TEST_URI` pointing at the infra stack: `docker compose -f docker/docker-compose.infra.yml up -d`, then `MONGO_TEST_URI=mongodb://localhost:27017 go test ./internal/core/auth/repository/ -run TestSweep -count=1`.

- [ ] **Step 2: Rewrite the cleanup operation**

In `backend/internal/core/auth/repository/refresh_token_repository.go`, replace the two cleanup declarations in the interface:

```go
	// Cleanup operations
	//
	// The invariant: no row is deleted while its token could still pass
	// temporal validation, regardless of whether it is active, rotated,
	// or otherwise revoked. Once expiresAt is past, replaying the token
	// cannot mint credentials, so the row may go. This supersedes the old
	// "retain revoked rows for one refresh TTL after revocation" rule,
	// which was both over-broad and wrong across a JWT_REFRESH_TOKEN_EXPIRY
	// change between restarts. The durable family fence has its own TTL
	// index and is unaffected. ADR-0017 D7.

	// CleanupExpiredTokens deletes at most `limit` expired rows and
	// reports whether more remain. hasMore is derived from selecting one
	// row beyond the batch, NOT from CountDocuments: the drain path runs
	// every five minutes and must not scan the whole eligible range to
	// decide its next cadence.
	CleanupExpiredTokens(ctx context.Context, limit int) (deleted int64, hasMore bool, err error)
	// CountExpiredTokens is the ONE indexed count taken on entry to drain
	// mode to seed the backlog gauge. It is never called per cycle.
	CountExpiredTokens(ctx context.Context) (int64, error)
```

Delete the `CleanupRevokedTokens` declaration entirely.

Replace the implementations:

```go
// SweepBatchLimit is the cluster-wide per-cycle deletion bound per tier.
// It is a cluster bound rather than a per-replica multiplier because the
// Redis lease elects exactly one scheduler.
const SweepBatchLimit = 5000

func (r *refreshTokenRepository) CleanupExpiredTokens(ctx context.Context, limit int) (int64, bool, error) {
	if limit <= 0 {
		return 0, false, nil
	}
	filter := bson.M{"expiresAt": bson.M{"$lt": time.Now()}}
	opts := options.Find().
		SetSort(bson.D{{Key: "expiresAt", Value: 1}, {Key: "uuid", Value: 1}}).
		SetLimit(int64(limit) + 1).
		SetProjection(bson.M{"uuid": 1, "_id": 0})

	//tenantscope:allow Refresh-token state is audience-tier scoped, not org scoped; this repository is bound to one tier collection.
	cursor, err := r.collection.Find(ctx, filter, opts)
	if err != nil {
		return 0, false, fmt.Errorf("failed to select expired tokens: %w", err)
	}
	var rows []struct {
		UUID string `bson:"uuid"`
	}
	if err := cursor.All(ctx, &rows); err != nil {
		return 0, false, fmt.Errorf("failed to read expired tokens: %w", err)
	}

	// The (limit+1)-th row is the whole point: it answers "is there more
	// work?" for the cost of one extra index entry, so the scheduler can
	// pick its next cadence without counting the backlog every cycle.
	hasMore := len(rows) > limit
	if hasMore {
		rows = rows[:limit]
	}
	if len(rows) == 0 {
		return 0, false, nil
	}

	uuids := make([]string, 0, len(rows))
	for _, row := range rows {
		uuids = append(uuids, row.UUID)
	}
	//tenantscope:allow Refresh-token state is audience-tier scoped, not org scoped; this repository is bound to one tier collection.
	res, err := r.collection.DeleteMany(ctx, bson.M{"uuid": bson.M{"$in": uuids}})
	if err != nil {
		return 0, hasMore, fmt.Errorf("failed to delete expired tokens: %w", err)
	}
	return res.DeletedCount, hasMore, nil
}

func (r *refreshTokenRepository) CountExpiredTokens(ctx context.Context) (int64, error) {
	//tenantscope:allow Refresh-token state is audience-tier scoped, not org scoped; this repository is bound to one tier collection.
	n, err := r.collection.CountDocuments(ctx, bson.M{"expiresAt": bson.M{"$lt": time.Now()}})
	if err != nil {
		return 0, fmt.Errorf("failed to count expired tokens: %w", err)
	}
	return n, nil
}
```

Delete the `CleanupRevokedTokens` implementation.

- [ ] **Step 3: Declare the supporting index**

In `backend/internal/core/auth/module.go`, add to both refresh-token collections. `Keys` is a map, so its iteration order is not deterministic — a compound index **must** use `OrderedKeys`:

```go
		{Name: models.OperatorRefreshTokensCollection, Indexes: []module.IndexSpec{
			{Keys: map[string]int{"uuid": 1}, Unique: true},
			{Keys: map[string]int{"userUuid": 1}},
			{Keys: map[string]int{"familyId": 1}},
			// Serves the sweep's sorted, limited selection. Deliberately
			// NOT a TTL index: deletion at expiry is semantically safe,
			// but Mongo's TTL monitor cannot provide the bounded
			// per-cycle progress and backlog telemetry the first cleanup
			// of an upgraded installation requires. ADR-0017 D7.
			{OrderedKeys: []module.IndexKey{{Field: "expiresAt", Direction: 1}, {Field: "uuid", Direction: 1}}},
		}},
```

and the identical block for `models.ClientRefreshTokensCollection`.

- [ ] **Step 4: Fix every caller and fake**

Run: `cd backend && go build ./... 2>&1 | head -40`

Fix each compile error: test fakes implementing `RefreshTokenRepository` need the new signatures and must drop `CleanupRevokedTokens`. In `services/gates_fakes_test.go` and any other fake, implement them for real rather than panicking — Task 16's tests drive the sweep through them:

```go
func (r *gateRefreshRepo) CleanupExpiredTokens(_ context.Context, limit int) (int64, bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.sweepErr != nil {
		return 0, false, r.sweepErr
	}
	now := time.Now()
	var expired []string
	for hash, d := range r.byHash {
		if d.ExpiresAt.Before(now) {
			expired = append(expired, hash)
		}
	}
	sort.Strings(expired) // deterministic batching
	hasMore := len(expired) > limit
	if hasMore {
		expired = expired[:limit]
	}
	for _, h := range expired {
		delete(r.byHash, h)
	}
	return int64(len(expired)), hasMore, nil
}

func (r *gateRefreshRepo) CountExpiredTokens(_ context.Context) (int64, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	var n int64
	now := time.Now()
	for _, d := range r.byHash {
		if d.ExpiresAt.Before(now) {
			n++
		}
	}
	return n, nil
}
```

- [ ] **Step 5: Run to verify pass**

Run: `cd backend && go build ./... && go test ./internal/core/auth/... -count=1`
Expected: PASS

- [ ] **Step 6: Rewrite the two superseded documentation rules**

Both of these name the rule D7 replaces and one names a function this task deletes. Leaving either makes `backend/internal/core/auth/CLAUDE.md` contradict the shipped behaviour.

Line ~432, in the refresh-rotation bullet — replace:
> Revoked rows must stay in the collection for at least the refresh TTL — do not shorten `CleanupRevokedTokens`'s `olderThan` below that.

with:
> No refresh row may be deleted while its token could still pass temporal validation, regardless of revocation state — an unexpired rotated row is exactly what replay detection matches against. Once `expiresAt` is past, replaying it cannot mint credentials and the row may be swept. `CleanupRevokedTokens` was deleted in ADR-0017 D7: revocation age alone is never a safe deletion criterion, and it was wrong across a `JWT_REFRESH_TOKEN_EXPIRY` change between restarts.

Line ~438, in the legacy-SID migration note — replace "Retain rotated and revoked refresh rows for at least one refresh-token TTL so replay detection remains effective" with the same temporal-validity invariant. The migration advice itself still holds; only the retention sentence changes.

- [ ] **Step 7: Commit**

```bash
cd /home/tore/orkestra
git add backend/internal/core/auth/repository/ backend/internal/core/auth/module.go \
        backend/internal/core/auth/services/gates_fakes_test.go backend/internal/core/auth/CLAUDE.md
git commit -m "feat(auth): bounded expired-refresh-token sweep with a hasMore probe

CleanupExpiredTokens becomes (limit) -> (deleted, hasMore, error), selecting
one row beyond the batch so the scheduler picks its cadence without counting
the backlog every cycle. CleanupRevokedTokens is removed: revocation age alone
is never a safe deletion criterion. ADR-0017 D7."
```

---

### Task 14: Sweep telemetry

The backlog is an **estimate** by construction — rows become eligible during a drain — so it is counted once on entry to drain mode, decremented by successful deletions, reset to zero when `hasMore=false`, and recomputed if leadership changes. The exact count is never recomputed every five minutes.

**Files:**
- Modify: `backend/pkg/sdk/metrics/metrics.go`
- Create: `backend/pkg/sdk/metrics/token_sweep_test.go`

**Interfaces:**
- Produces: `(*Collector).RecordTokenSweep(tier string, deleted int64, duration time.Duration)`, `(*Collector).SetTokenSweepBacklog(tier string, remaining int64)` — Task 16 consumes both.

- [ ] **Step 1: Write the failing test**

Create `backend/pkg/sdk/metrics/token_sweep_test.go`:

```go
package metrics

import (
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus/testutil"
)

// ADR-0017 D8: tier is the ONLY label. Collection names, UUIDs,
// configuration values and error strings never become labels.
func TestTokenSweepLabelsAreClosed(t *testing.T) {
	c := NewCollector()
	if err := c.Register(); err != nil {
		t.Fatalf("Register: %v", err)
	}
	c.RecordTokenSweep("operator", 5000, 900*time.Millisecond)
	c.RecordTokenSweep("client", 12, 3*time.Millisecond)
	c.RecordTokenSweep("operator_refresh_tokens", 1, time.Millisecond) // a collection name is not a tier
	c.RecordTokenSweep("", 1, time.Millisecond)

	for tier, want := range map[string]float64{"operator": 5001, "client": 12, "unknown": 1} {
		if got := testutil.ToFloat64(c.tokenSweepDeleted.WithLabelValues(tier)); got != want {
			t.Errorf("tier=%q deleted = %v, want %v", tier, got, want)
		}
	}
}

func TestTokenSweepBacklogGauge(t *testing.T) {
	c := NewCollector()
	if err := c.Register(); err != nil {
		t.Fatalf("Register: %v", err)
	}
	c.SetTokenSweepBacklog("operator", 1_200_000)
	if got := testutil.ToFloat64(c.tokenSweepBacklog.WithLabelValues("operator")); got != 1_200_000 {
		t.Errorf("backlog = %v, want 1200000", got)
	}
	c.SetTokenSweepBacklog("operator", 0)
	if got := testutil.ToFloat64(c.tokenSweepBacklog.WithLabelValues("operator")); got != 0 {
		t.Errorf("backlog after drain = %v, want 0 — operators watch this reach zero to know the drain finished", got)
	}
}

func TestNilCollectorSweepRecordersAreSafe(t *testing.T) {
	var c *Collector
	c.RecordTokenSweep("operator", 1, time.Second)
	c.SetTokenSweepBacklog("operator", 1)
}
```

- [ ] **Step 2: Run to verify failure**

Run: `cd backend && go test ./pkg/sdk/metrics/ -count=1`
Expected: FAIL — undefined fields and methods

- [ ] **Step 3: Add the families**

In the `Collector` struct:

```go
	tokenSweepDeleted  *prometheus.CounterVec
	tokenSweepBacklog  *prometheus.GaugeVec
	tokenSweepDuration *prometheus.HistogramVec
```

In `buildMetrics`:

```go
	c.tokenSweepDeleted = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: "orkestra",
			Subsystem: "auth",
			Name:      "token_sweep_deleted_total",
			Help:      "Refresh-token rows deleted by the retention sweep, per audience tier.",
		},
		// Closed label set: tier ∈ {operator, client}. ADR-0017 D8.
		[]string{"tier"},
	)

	c.tokenSweepBacklog = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Namespace: "orkestra",
			Subsystem: "auth",
			Name:      "token_sweep_backlog_estimate",
			Help:      "Estimated refresh-token rows still eligible for deletion, per tier. An ESTIMATE: seeded by one indexed count on entry to drain mode, then decremented locally, because rows become eligible during a drain. Operators watch it reach zero to see the drain finish.",
		},
		[]string{"tier"},
	)

	c.tokenSweepDuration = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Namespace: "orkestra",
			Subsystem: "auth",
			Name:      "token_sweep_duration_seconds",
			Help:      "Wall time of one refresh-token sweep batch, per tier. Measured before promotion so the first cleanup of an upgraded installation is an observed event rather than a discovered one.",
			Buckets:   prometheus.DefBuckets,
		},
		[]string{"tier"},
	)
```

Add all three to the `Register` slice and to the package doc block.

Add the recorders with the same tier-normalising discipline as `RecordSessionAnchorAnomaly`:

```go
// normaliseTier keeps the sweep label set closed at {operator, client}.
// Anything else — a collection name, an empty string, a caller bug —
// collapses to "unknown" rather than minting a new time series.
func normaliseTier(tier string) string {
	if tier != "operator" && tier != "client" {
		return "unknown"
	}
	return tier
}

// RecordTokenSweep records one completed sweep batch for a tier.
func (c *Collector) RecordTokenSweep(tier string, deleted int64, duration time.Duration) {
	if c == nil || c.tokenSweepDeleted == nil {
		return
	}
	t := normaliseTier(tier)
	if deleted > 0 {
		c.tokenSweepDeleted.WithLabelValues(t).Add(float64(deleted))
	}
	c.tokenSweepDuration.WithLabelValues(t).Observe(duration.Seconds())
}

// SetTokenSweepBacklog publishes the current backlog estimate for a tier.
func (c *Collector) SetTokenSweepBacklog(tier string, remaining int64) {
	if c == nil || c.tokenSweepBacklog == nil {
		return
	}
	if remaining < 0 {
		remaining = 0
	}
	c.tokenSweepBacklog.WithLabelValues(normaliseTier(tier)).Set(float64(remaining))
}
```

- [ ] **Step 4: Run to verify pass**

Run: `cd backend && go test ./pkg/sdk/metrics/ -count=1`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
cd /home/tore/orkestra
git add backend/pkg/sdk/metrics/
git commit -m "feat(metrics): refresh-token sweep deleted/backlog/duration families

Closed label set at tier in {operator,client} per ADR-0017 D8. Backlog is an
explicit estimate: counted once on entry to drain mode, decremented locally,
reset at completion — never recounted every five minutes."
```

---

### Task 15: Single-scheduler Redis lease

The lease elects the **scheduler leader**, not merely the owner of one pass. Holding it across the six-hour idle wait is what prevents follower retries from accidentally turning an idle cluster into a five-minute sweep loop, while leader loss still fails over after the lease expires. 5,000 is therefore a cluster-wide bound, not a per-replica multiplier.

Renew and release use Lua so one replica cannot renew or release another's lease.

**Files:**
- Modify: `backend/internal/shared/database/redis.go` (add `SetNX`, `Eval`)
- Create: `backend/internal/core/auth/services/maintenance_lease.go`
- Create: `backend/internal/core/auth/services/maintenance_lease_test.go`

**Interfaces:**
- Produces: `services.LeaseRedisClient` (narrow extension of `services.RedisClient`, mirroring the existing `AtomicTakeRedisClient`/`GetDel` precedent).
- Produces: `services.NewMaintenanceLease(client LeaseRedisClient, key string, log *slog.Logger) *MaintenanceLease` with `Acquire(ctx) (bool, error)`, `Renew(ctx) (bool, error)`, `Release(ctx) error` — Task 16 drives all three.
- Produces: `services.LeaseTTL = 2 * time.Minute`, `services.LeaseRenewInterval = 30 * time.Second`, `services.LeaseRetryInterval = 5 * time.Minute`.

- [ ] **Step 1: Write the failing test**

Create `backend/internal/core/auth/services/maintenance_lease_test.go`:

```go
package services

import (
	"context"
	"log/slog"
	"testing"
)

func TestLease_OnlyOneAcquirerWins(t *testing.T) {
	redis := newFakeLeaseRedis()
	a := NewMaintenanceLease(redis, "auth:maintenance:token-sweep", slog.New(slog.DiscardHandler))
	b := NewMaintenanceLease(redis, "auth:maintenance:token-sweep", slog.New(slog.DiscardHandler))

	okA, err := a.Acquire(context.Background())
	if err != nil || !okA {
		t.Fatalf("first Acquire = %v, %v; want true, nil", okA, err)
	}
	okB, err := b.Acquire(context.Background())
	if err != nil {
		t.Fatalf("second Acquire error: %v", err)
	}
	if okB {
		t.Fatal("two replicas both hold the scheduler lease — 5000 would become a per-replica multiplier")
	}
}

func TestLease_NonOwnerCannotRenewOrRelease(t *testing.T) {
	redis := newFakeLeaseRedis()
	owner := NewMaintenanceLease(redis, "k", slog.New(slog.DiscardHandler))
	other := NewMaintenanceLease(redis, "k", slog.New(slog.DiscardHandler))
	if ok, _ := owner.Acquire(context.Background()); !ok {
		t.Fatal("owner failed to acquire")
	}

	if ok, _ := other.Renew(context.Background()); ok {
		t.Error("a non-owner renewed another replica's lease")
	}
	if err := other.Release(context.Background()); err != nil {
		t.Errorf("a non-owner Release must be a silent no-op, got %v", err)
	}
	if ok, _ := owner.Renew(context.Background()); !ok {
		t.Error("the owner's lease was released by a non-owner")
	}
}

func TestLease_ReleaseAllowsFailover(t *testing.T) {
	redis := newFakeLeaseRedis()
	first := NewMaintenanceLease(redis, "k", slog.New(slog.DiscardHandler))
	second := NewMaintenanceLease(redis, "k", slog.New(slog.DiscardHandler))
	_, _ = first.Acquire(context.Background())
	if err := first.Release(context.Background()); err != nil {
		t.Fatalf("Release: %v", err)
	}
	if ok, _ := second.Acquire(context.Background()); !ok {
		t.Fatal("a released lease must be immediately acquirable so a rolling restart does not stall maintenance")
	}
}

func TestLease_ExpiryAllowsFailover(t *testing.T) {
	redis := newFakeLeaseRedis()
	first := NewMaintenanceLease(redis, "k", slog.New(slog.DiscardHandler))
	second := NewMaintenanceLease(redis, "k", slog.New(slog.DiscardHandler))
	_, _ = first.Acquire(context.Background())
	redis.expireAll() // the leader died without releasing
	if ok, _ := second.Acquire(context.Background()); !ok {
		t.Fatal("a lost leader must fail over once the lease expires")
	}
}

func TestLease_RedisErrorIsNotLeadership(t *testing.T) {
	redis := newFakeLeaseRedis()
	redis.failEverything()
	l := NewMaintenanceLease(redis, "k", slog.New(slog.DiscardHandler))
	ok, err := l.Acquire(context.Background())
	if ok {
		t.Fatal("a Redis failure must never be read as having won the lease")
	}
	if err == nil {
		t.Error("the caller needs the error to log a bounded warning and skip maintenance")
	}
}
```

Write `fakeLeaseRedis` in the same file: a mutex-guarded `map[string]string` plus per-key expiry flags, implementing `LeaseRedisClient`. Its `Eval` must interpret the two scripts by matching on the script text and applying the same compare-then-act semantics Redis would.

- [ ] **Step 2: Run to verify failure**

Run: `cd backend && go test ./internal/core/auth/services/ -run TestLease -count=1`
Expected: FAIL — `undefined: NewMaintenanceLease`

- [ ] **Step 3: Extend the Redis adapter**

In `backend/internal/shared/database/redis.go`, next to `GetDel`:

```go
// SetNX sets the key only if it does not exist, reporting whether this
// caller created it. The primitive behind the maintenance-lease election.
func (r *RedisClientAdapter) SetNX(ctx context.Context, key string, value interface{}, expiration time.Duration) (bool, error) {
	result := r.client.SetNX(ctx, key, value, expiration)
	if result.Err() != nil {
		return false, result.Err()
	}
	return result.Val(), nil
}

// Eval runs a Lua script server-side. The maintenance lease uses it so
// compare-and-expire and compare-and-delete are atomic: without it, one
// replica could renew or delete a lease another replica owns in the gap
// between reading the owner token and acting on it.
func (r *RedisClientAdapter) Eval(ctx context.Context, script string, keys []string, args ...interface{}) (interface{}, error) {
	result := r.client.Eval(ctx, script, keys, args...)
	if result.Err() != nil {
		return nil, result.Err()
	}
	return result.Val(), nil
}
```

Do **not** add these to `module.RedisClient` — widening that interface would break every fork implementation of it. The narrow extension below follows the existing `AtomicTakeRedisClient` precedent.

- [ ] **Step 4: Implement the lease**

Create `backend/internal/core/auth/services/maintenance_lease.go`:

```go
package services

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"log/slog"
	"sync"
	"time"
)

// Lease timings. The TTL is generous relative to the renew interval so a
// brief Redis blip does not hand the scheduler to a second replica; the
// follower retry is long because losing a sweep cycle costs nothing and
// a tight retry loop against a healthy leader costs Redis traffic.
const (
	LeaseTTL           = 2 * time.Minute
	LeaseRenewInterval = 30 * time.Second
	LeaseRetryInterval = 5 * time.Minute
)

// LeaseRedisClient is the narrow extension the maintenance lease needs.
// Defined here rather than widening module.RedisClient, which every fork
// implementation would have to grow. Mirrors AtomicTakeRedisClient.
type LeaseRedisClient interface {
	RedisClient
	SetNX(ctx context.Context, key string, value interface{}, expiration time.Duration) (bool, error)
	Eval(ctx context.Context, script string, keys []string, args ...interface{}) (interface{}, error)
}

// Compare-and-expire / compare-and-delete. Both run server-side so a
// replica can only act on a lease whose owner token it holds — the
// read-then-act version has a window in which the lease expires, another
// replica acquires it, and the first replica then renews or deletes a
// lease it no longer owns.
const (
	leaseRenewScript = `
if redis.call("GET", KEYS[1]) == ARGV[1] then
  return redis.call("PEXPIRE", KEYS[1], ARGV[2])
else
  return 0
end`
	leaseReleaseScript = `
if redis.call("GET", KEYS[1]) == ARGV[1] then
  return redis.call("DEL", KEYS[1])
else
  return 0
end`
)

// MaintenanceLease elects one scheduler leader across backend replicas.
//
// It guards the SCHEDULER, not one pass: the leader retains the lease
// across both the drain wait and the six-hour idle wait. Guarding only
// the pass would let every follower wake on its own retry timer and
// re-enter drain mode against an idle cluster, and would turn the
// per-cycle batch bound into a per-replica multiplier.
//
// Redis unavailability is never leadership. A failed acquire or a failed
// renew skips maintenance — it must never affect authentication.
type MaintenanceLease struct {
	client LeaseRedisClient
	key    string
	log    *slog.Logger

	mu    sync.Mutex
	token string
}

func NewMaintenanceLease(client LeaseRedisClient, key string, log *slog.Logger) *MaintenanceLease {
	if log == nil {
		log = slog.Default()
	}
	return &MaintenanceLease{client: client, key: key, log: log}
}

// Acquire attempts leadership. Returns (false, nil) when another replica
// holds it and (false, err) when Redis could not answer — the caller must
// treat both as "not the leader" but only log the second.
func (l *MaintenanceLease) Acquire(ctx context.Context) (bool, error) {
	if l == nil || l.client == nil {
		return false, nil
	}
	token, err := randomLeaseToken()
	if err != nil {
		return false, err
	}
	ok, err := l.client.SetNX(ctx, l.key, token, LeaseTTL)
	if err != nil || !ok {
		return false, err
	}
	l.mu.Lock()
	l.token = token
	l.mu.Unlock()
	return true, nil
}

// Renew extends the lease only while this replica still owns it.
// A false return means leadership was lost: the caller must cancel the
// in-flight database context and the scheduler loop rather than carry on.
func (l *MaintenanceLease) Renew(ctx context.Context) (bool, error) {
	if l == nil || l.client == nil {
		return false, nil
	}
	l.mu.Lock()
	token := l.token
	l.mu.Unlock()
	if token == "" {
		return false, nil
	}
	res, err := l.client.Eval(ctx, leaseRenewScript, []string{l.key}, token, LeaseTTL.Milliseconds())
	if err != nil {
		return false, err
	}
	return leaseScriptSucceeded(res), nil
}

// Release drops the lease if this replica owns it. A non-owner Release is
// a silent no-op so a shutdown race cannot hand the next leader's lease
// away.
func (l *MaintenanceLease) Release(ctx context.Context) error {
	if l == nil || l.client == nil {
		return nil
	}
	l.mu.Lock()
	token := l.token
	l.token = ""
	l.mu.Unlock()
	if token == "" {
		return nil
	}
	_, err := l.client.Eval(ctx, leaseReleaseScript, []string{l.key}, token)
	return err
}

func leaseScriptSucceeded(res interface{}) bool {
	switch v := res.(type) {
	case int64:
		return v == 1
	case int:
		return v == 1
	default:
		return false
	}
}

func randomLeaseToken() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}
```

- [ ] **Step 5: Run to verify pass**

Run: `cd backend && go test ./internal/core/auth/services/ -run TestLease -count=1 && go build ./...`
Expected: PASS

- [ ] **Step 6: Commit**

```bash
cd /home/tore/orkestra
git add backend/internal/shared/database/redis.go backend/internal/core/auth/services/maintenance_lease.go \
        backend/internal/core/auth/services/maintenance_lease_test.go
git commit -m "feat(auth): Redis lease electing one maintenance scheduler across replicas

Guards the scheduler, not one pass: the leader holds it across the idle wait
too, so followers cannot turn an idle cluster into a five-minute sweep loop
and 5000 stays a cluster-wide bound. Renew and release are Lua compare-and-act
so no replica can act on another's lease. ADR-0017 D7."
```

---

### Task 16: The adaptive-cadence sweep scheduler

**The cadence adapts, not the batch.** A fixed 6-hour interval would not drain: 5,000 rows per tier every six hours is 20,000 a day, so a one-million-row backlog takes fifty days and a five-million-row one over eight months. Rescheduling on the `hasMore` bit the previous batch reported — 5 minutes while true, 6 hours once false — gives 5,000 × 288 ≈ 1.4M rows per tier per day, so the same million-row backlog clears in under a day and a five-million-row one in under four, at a sustained ~17 deletes per second. Every individual pass stays bounded at 5,000 and the loop returns to the idle cadence on its own.

Self-draining is also what makes an operator-triggered sweep unnecessary. Any design that drains too slowly has to tell operators to "run extra cycles during a maintenance window", which needs an admin endpoint or a documented manual procedure — surface that has to be built, secured, and documented.

No config field: the two intervals are maintenance constants, not policy, and a knob here is surface to document and get wrong.

**Files:**
- Create: `backend/internal/core/auth/maintenance.go`
- Create: `backend/internal/core/auth/maintenance_test.go`
- Modify: `backend/internal/core/auth/module.go` (`AuthModule` struct fields; capture the two refresh repos and the lease during `Init`)

**Interfaces:**
- Consumes: `repository.SweepBatchLimit`, `CleanupExpiredTokens`, `CountExpiredTokens` (Task 13); `metrics.RecordTokenSweep`, `SetTokenSweepBacklog` (Task 14); `services.MaintenanceLease` and its three interval constants (Task 15).
- Produces: `(*AuthModule).Start(ctx) error`, `(*AuthModule).Stop(ctx) error` — shadowing `BaseModule`'s no-ops.

- [ ] **Step 1: Write the failing tests**

Create `backend/internal/core/auth/maintenance_test.go`. Rather than injecting a clock into the loop, the adaptive decision is extracted into a pure function and the batch work into a method — both assertable directly, with no waiting anywhere. The loop itself is then thin enough that the lifecycle test covers it.

```go
package auth

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/orkestra/backend/internal/core/auth/models"
	"github.com/orkestra/backend/internal/core/auth/repository"
)

// --- fakes ---

// fakeSweepRepo implements only the two sweep methods; everything else
// panics, because a sweep that called anything else would be a bug worth
// failing loudly on.
type fakeSweepRepo struct {
	repository.RefreshTokenRepository // embedded nil: any other call panics

	mu          sync.Mutex
	expired     int64
	countCalls  int
	sweepErr    error
	deletedSoFar int64
}

func (r *fakeSweepRepo) CleanupExpiredTokens(_ context.Context, limit int) (int64, bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.sweepErr != nil {
		return 0, false, r.sweepErr
	}
	n := int64(limit)
	if r.expired < n {
		n = r.expired
	}
	r.expired -= n
	r.deletedSoFar += n
	return n, r.expired > 0, nil
}

func (r *fakeSweepRepo) CountExpiredTokens(context.Context) (int64, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.countCalls++
	return r.expired, nil
}

func (r *fakeSweepRepo) counts() (int64, int) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.deletedSoFar, r.countCalls
}

func newSweepModule(t *testing.T, operatorExpired, clientExpired int64) (*AuthModule, *fakeSweepRepo, *fakeSweepRepo) {
	t.Helper()
	op := &fakeSweepRepo{expired: operatorExpired}
	cl := &fakeSweepRepo{expired: clientExpired}
	m := &AuthModule{logger: slog.New(slog.DiscardHandler)}
	m.sweepTiers = []sweepTier{
		{name: "operator", repo: op},
		{name: "client", repo: cl},
	}
	return m, op, cl
}

// --- the adaptive mechanism ---

// A fixed 6-hour interval cannot drain: 5,000 rows per tier every six
// hours is 20,000 a day, so a one-million-row backlog takes fifty days.
// The cadence — not the batch — is what adapts.
func TestNextSweepInterval_AdaptsToBacklog(t *testing.T) {
	if got := nextSweepInterval(true); got != sweepDrainInterval {
		t.Errorf("with a backlog = %v, want %v", got, sweepDrainInterval)
	}
	if got := nextSweepInterval(false); got != sweepIdleInterval {
		t.Errorf("drained = %v, want %v", got, sweepIdleInterval)
	}
	if sweepDrainInterval >= sweepIdleInterval {
		t.Fatal("the drain cadence must be faster than the idle one, or the loop never catches up")
	}
}

func TestSweep_OneBatchPerTierPerCycle(t *testing.T) {
	m, op, cl := newSweepModule(t, 3*int64(repository.SweepBatchLimit), 10)
	ctx := context.Background()

	hasMore := m.sweepOneTier(ctx, &m.sweepTiers[0])
	if !hasMore {
		t.Error("operator tier reported no backlog with 3 batches queued")
	}
	m.sweepOneTier(ctx, &m.sweepTiers[1])

	if deleted, _ := op.counts(); deleted != int64(repository.SweepBatchLimit) {
		t.Errorf("operator deleted %d in one cycle, want exactly the batch bound %d", deleted, repository.SweepBatchLimit)
	}
	if deleted, _ := cl.counts(); deleted != 10 {
		t.Errorf("client deleted %d, want 10", deleted)
	}
}

func TestSweep_BacklogCountedOnceOnEntryToDrain(t *testing.T) {
	m, op, _ := newSweepModule(t, 3*int64(repository.SweepBatchLimit), 0)
	ctx := context.Background()
	tier := &m.sweepTiers[0]

	for m.sweepOneTier(ctx, tier) {
	}

	if _, calls := op.counts(); calls != 1 {
		t.Errorf("CountExpiredTokens called %d times, want 1 — at the five-minute cadence a per-cycle count would scan the whole eligible range 288 times a day", calls)
	}
	if tier.backlog != 0 {
		t.Errorf("backlog estimate = %d after draining, want 0 — operators watch this reach zero to see the drain finish", tier.backlog)
	}
	if tier.backlogCounted {
		t.Error("backlogCounted must reset so a later drain recounts")
	}
}

func TestSweep_RepositoryErrorReportsNoBacklogAndDoesNotPanic(t *testing.T) {
	m, op, _ := newSweepModule(t, 100, 0)
	op.sweepErr = errors.New("mongo unavailable")
	if m.sweepOneTier(context.Background(), &m.sweepTiers[0]) {
		t.Error("a failed batch must not claim a backlog and re-arm the drain cadence")
	}
}

// --- leadership ---

func TestSweep_NotTheLeaderDoesNothing(t *testing.T) {
	m, op, _ := newSweepModule(t, 100, 0)
	redis := newFakeLeaseRedis()
	incumbent := services.NewMaintenanceLease(redis, tokenSweepLeaseKey, slog.New(slog.DiscardHandler))
	if ok, _ := incumbent.Acquire(context.Background()); !ok {
		t.Fatal("incumbent failed to acquire")
	}
	m.sweepLease = services.NewMaintenanceLease(redis, tokenSweepLeaseKey, slog.New(slog.DiscardHandler))

	acquired, err := m.sweepLease.Acquire(context.Background())
	if err != nil || acquired {
		t.Fatalf("follower Acquire = (%v, %v), want (false, nil)", acquired, err)
	}
	if deleted, _ := op.counts(); deleted != 0 {
		t.Error("a follower swept; 5000 must be a cluster-wide bound, not a per-replica multiplier")
	}
}

func TestSweep_RedisUnavailableIsNotLeadership(t *testing.T) {
	redis := newFakeLeaseRedis()
	redis.failEverything()
	lease := services.NewMaintenanceLease(redis, tokenSweepLeaseKey, slog.New(slog.DiscardHandler))
	acquired, err := lease.Acquire(context.Background())
	if acquired {
		t.Fatal("a Redis failure was read as having won the lease")
	}
	if err == nil {
		t.Error("the loop needs the error to log a bounded warning and skip maintenance — never authentication")
	}
}

// --- lifecycle ---

func TestModuleLifecycle_StartIdempotentStopExitsRestartable(t *testing.T) {
	m, _, _ := newSweepModule(t, 0, 0)
	m.sweepLease = services.NewMaintenanceLease(newFakeLeaseRedis(), tokenSweepLeaseKey, slog.New(slog.DiscardHandler))
	ctx := context.Background()

	if err := m.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	firstDone := m.sweepDone
	if err := m.Start(ctx); err != nil {
		t.Fatalf("second Start must be a no-op, got %v", err)
	}
	if m.sweepDone != firstDone {
		t.Fatal("a second Start created a second loop — a hot enable/disable cycle would leave two tickers")
	}

	stopCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err := m.Stop(stopCtx); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	select {
	case <-firstDone:
	default:
		t.Fatal("Stop returned before the loop closed done")
	}

	if err := m.Start(ctx); err != nil {
		t.Fatalf("a stopped module must start again: %v", err)
	}
	if m.sweepDone == firstDone {
		t.Fatal("restart reused the closed done channel")
	}
	_ = m.Stop(stopCtx)
}

func TestModuleLifecycle_NoLeaseMeansNoLoop(t *testing.T) {
	m, _, _ := newSweepModule(t, 0, 0)
	m.sweepLease = nil // Redis adapter did not satisfy LeaseRedisClient at Init
	if err := m.Start(context.Background()); err != nil {
		t.Fatalf("Start without a lease must be a silent no-op, got %v", err)
	}
	if m.sweepDone != nil {
		t.Error("a loop started without leadership; every replica would sweep unelected")
	}
}

func TestModuleLifecycle_StopWithoutStartIsNoop(t *testing.T) {
	m, _, _ := newSweepModule(t, 0, 0)
	if err := m.Stop(context.Background()); err != nil {
		t.Errorf("Stop before Start = %v, want nil", err)
	}
}
```

Duplicate the small `fakeLeaseRedis` from Task 15 into this file — package `auth` cannot see package `services`'s test files, and a ~40-line fake is cheaper than putting a public test seam on production code. Add the `services` import.

The lifecycle tests run fast despite `sweepStartupDelay`: `Start` returns immediately and the goroutine is parked on its first timer, which `Stop`'s cancel unblocks.

- [ ] **Step 2: Run to verify failure**

Run: `cd backend && go test ./internal/core/auth/ -run 'TestSweep|TestModuleLifecycle' -count=1`
Expected: FAIL — undefined sweeper and lifecycle symbols

- [ ] **Step 3: Implement the scheduler**

Create `backend/internal/core/auth/maintenance.go`:

```go
package auth

import (
	"context"
	"log/slog"
	"time"

	"github.com/orkestra/backend/internal/core/auth/repository"
	"github.com/orkestra/backend/internal/core/auth/services"
	"github.com/orkestra/backend/pkg/sdk/metrics"
)

// Sweep cadence. The CADENCE adapts to the backlog; the batch never does.
//
// A fixed 6-hour interval cannot drain an upgraded installation: 5,000
// rows per tier every six hours is 20,000 a day, so a one-million-row
// backlog takes fifty days and a five-million-row one over eight months.
// At the drain cadence the same batch clears ~1.4M rows per tier per day
// — a sustained ~17 deletes per second, unremarkable load — so a
// million-row backlog clears in under a day and a five-million-row one in
// under four, while every individual pass stays bounded and the loop
// returns to idle on its own.
//
// Self-draining is also what removes the need for an operator-triggered
// sweep: a design that drains too slowly has to tell operators to run
// extra cycles in a maintenance window, which means an admin endpoint or
// a manual procedure to build, secure and document.
//
// Deliberately not config fields: these are maintenance constants, not
// policy, and a knob here is surface to document and get wrong.
const (
	sweepIdleInterval  = 6 * time.Hour
	sweepDrainInterval = 5 * time.Minute
	// sweepStartupDelay keeps the first pass off the boot path so index
	// builds and module initialisation are not competing with a drain.
	sweepStartupDelay = 3 * time.Minute
	// tokenSweepLeaseKey names the cluster-wide scheduler lease.
	tokenSweepLeaseKey = "auth:maintenance:token-sweep"
)

// sweepTier pairs a repository with the label its metrics carry.
type sweepTier struct {
	name string // "operator" | "client" — the closed metric label
	repo repository.RefreshTokenRepository
	// backlog is the local estimate: seeded by one indexed count on entry
	// to drain mode, decremented by successful deletions, reset when the
	// tier reports hasMore=false, recomputed if leadership changes.
	backlog        int64
	backlogCounted bool
}

// Start launches the refresh-token retention sweep. Idempotent, and a
// stopped module can start again — the registry calls these per hot
// enable/disable, not only at boot. Mirrors the logging module's
// lifecycle mutex/cancel/done pattern so no second ticker survives a
// toggle cycle.
func (m *AuthModule) Start(ctx context.Context) error {
	if len(m.sweepTiers) == 0 || m.sweepLease == nil {
		// Nothing to sweep, or Redis was unavailable at Init. Maintenance
		// is skipped; authentication is untouched.
		return nil
	}

	m.lifecycleMu.Lock()
	defer m.lifecycleMu.Unlock()
	if m.sweepCancel != nil {
		select {
		case <-m.sweepDone:
			m.sweepCancel = nil
			m.sweepDone = nil
		default:
			return nil // already running
		}
	}

	sweepCtx, cancel := context.WithCancel(ctx)
	done := make(chan struct{})
	m.sweepCancel = cancel
	m.sweepDone = done
	go m.tokenSweepLoop(sweepCtx, done)
	return nil
}

// Stop cancels the sweep loop and waits for it to exit, releasing the
// scheduler lease so another replica can take over immediately rather
// than after the lease TTL.
func (m *AuthModule) Stop(ctx context.Context) error {
	m.lifecycleMu.Lock()
	cancel := m.sweepCancel
	done := m.sweepDone
	m.lifecycleMu.Unlock()
	if cancel == nil {
		return nil
	}

	cancel()
	select {
	case <-done:
		m.lifecycleMu.Lock()
		if m.sweepDone == done {
			m.sweepCancel = nil
			m.sweepDone = nil
		}
		m.lifecycleMu.Unlock()
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (m *AuthModule) tokenSweepLoop(ctx context.Context, done chan<- struct{}) {
	defer close(done)

	leader := false
	defer func() {
		if leader {
			// Best-effort release on a context that is already cancelled
			// would be a no-op, so use a fresh short one.
			releaseCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			if err := m.sweepLease.Release(releaseCtx); err != nil {
				m.logger.Warn("auth: failed to release token-sweep lease", slog.String("error", err.Error()))
			}
		}
	}()

	wait := sweepStartupDelay
	renewTicker := time.NewTicker(services.LeaseRenewInterval)
	defer renewTicker.Stop()

	for {
		timer := time.NewTimer(wait)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-renewTicker.C:
			timer.Stop()
			if !leader {
				continue
			}
			ok, err := m.sweepLease.Renew(ctx)
			if err != nil || !ok {
				// Leadership lost. Cancelling the loop is the point: a
				// replica that keeps sweeping without the lease turns the
				// cluster-wide batch bound into a per-replica multiplier.
				m.logger.Warn("auth: token-sweep leadership lost, stopping maintenance",
					slog.String("outcome", errOutcome(err)))
				leader = false
				return
			}
			continue
		case <-timer.C:
		}

		if !leader {
			acquired, err := m.sweepLease.Acquire(ctx)
			if err != nil {
				// Redis unavailable is never leadership. Bounded warning,
				// skip maintenance, never touch authentication.
				m.logger.Warn("auth: token-sweep lease unavailable, skipping maintenance",
					slog.String("error", err.Error()))
				wait = services.LeaseRetryInterval
				continue
			}
			if !acquired {
				wait = services.LeaseRetryInterval
				continue
			}
			leader = true
			// Leadership changed: the previous leader's backlog estimate
			// is not ours, so recompute on the next drain entry.
			for i := range m.sweepTiers {
				m.sweepTiers[i].backlogCounted = false
			}
		}

		anyHasMore := false
		for i := range m.sweepTiers {
			if m.sweepOneTier(ctx, &m.sweepTiers[i]) {
				anyHasMore = true
			}
		}

		// Reschedule on the hasMore bit the batch just reported.
		wait = nextSweepInterval(anyHasMore)
	}
}

// nextSweepInterval is the whole adaptive mechanism, isolated so it can be
// asserted without a clock: while there is a backlog the loop runs every
// five minutes; once there is not, it costs one query per tier every six
// hours and nothing else.
func nextSweepInterval(anyHasMore bool) time.Duration {
	if anyHasMore {
		return sweepDrainInterval
	}
	return sweepIdleInterval
}

// sweepOneTier performs at most one bounded batch and returns hasMore.
func (m *AuthModule) sweepOneTier(ctx context.Context, tier *sweepTier) bool {
	started := time.Now()
	deleted, hasMore, err := tier.repo.CleanupExpiredTokens(ctx, repository.SweepBatchLimit)
	elapsed := time.Since(started)
	if err != nil {
		m.logger.Error("auth: token sweep batch failed",
			slog.String("tier", tier.name),
			slog.String("error", err.Error()))
		return false
	}

	metrics.Default().RecordTokenSweep(tier.name, deleted, elapsed)

	switch {
	case hasMore && !tier.backlogCounted:
		// The ONE indexed count, taken on entry to drain mode. Not per
		// cycle: at the five-minute cadence that would scan the whole
		// eligible range 288 times a day to publish a number that is an
		// estimate either way.
		if total, cErr := tier.repo.CountExpiredTokens(ctx); cErr == nil {
			tier.backlog = total
			tier.backlogCounted = true
		} else {
			m.logger.Warn("auth: token-sweep backlog count failed, gauge will lag",
				slog.String("tier", tier.name),
				slog.String("error", cErr.Error()))
		}
	case hasMore:
		tier.backlog -= deleted
		if tier.backlog < 0 {
			tier.backlog = 0
		}
	default:
		tier.backlog = 0
		tier.backlogCounted = false
	}
	metrics.Default().SetTokenSweepBacklog(tier.name, tier.backlog)

	m.logger.Info("auth: token sweep batch",
		slog.String("tier", tier.name),
		slog.Int64("deleted", deleted),
		slog.Bool("hasMore", hasMore),
		slog.Int64("backlogEstimate", tier.backlog),
		slog.Duration("duration", elapsed))
	return hasMore
}

func errOutcome(err error) string {
	if err != nil {
		return "renew_error"
	}
	return "not_owner"
}
```

- [ ] **Step 4: Wire the module fields**

In `backend/internal/core/auth/module.go`, add to the `AuthModule` struct:

```go
	// Refresh-token retention sweep (ADR-0017 D7). One loop covering both
	// tiers by calling the two repositories, which are separate
	// instances. lifecycleMu/sweepCancel/sweepDone copy the logging
	// module's pattern so Start is idempotent, a stopped module can start
	// again, and no second ticker survives a hot enable/disable cycle.
	lifecycleMu sync.Mutex
	sweepCancel context.CancelFunc
	sweepDone   chan struct{}
	sweepTiers  []sweepTier
	sweepLease  *services.MaintenanceLease
	logger      *slog.Logger
```

(If `m.logger` already exists on the struct, do not redeclare it.)

At the end of `Init`, after both tier bundles are built:

```go
	// Maintenance wiring. A Redis adapter that cannot satisfy the lease
	// contract disables the sweep rather than running it unelected —
	// every replica sweeping would make the per-cycle bound meaningless.
	if lease, ok := deps.RedisAdapter.(services.LeaseRedisClient); ok {
		m.sweepLease = services.NewMaintenanceLease(lease, tokenSweepLeaseKey, logger)
		m.sweepTiers = []sweepTier{
			{name: "operator", repo: opBundle.refreshTokenRepo},
			{name: "client", repo: clBundle.refreshTokenRepo},
		}
	} else {
		logger.Warn("auth: Redis adapter does not support the maintenance lease; refresh-token sweep disabled")
	}
	m.logger = logger
```

Add `"sync"` and `"log/slog"` to the imports if absent.

- [ ] **Step 5: Run to verify pass**

Run: `cd backend && go build ./... && go test ./internal/core/auth/... -count=1 -race`
Expected: PASS, no data races

- [ ] **Step 6: Document the sweep**

In `backend/internal/core/auth/CLAUDE.md`, in the collections table, change the refresh-token row's TTL column from the current parenthetical to:

> — (application sweep, not a TTL index: bounded per-cycle progress and backlog telemetry are required for the first cleanup of an upgraded install, and a TTL index provides neither)

Add an operations bullet:

> **Refresh-token retention is an elected, self-draining sweep.** `AuthModule.Start` runs one loop covering both tiers. A Redis lease (`auth:maintenance:token-sweep`, 2m TTL, renewed every 30s with Lua compare-and-expire) elects **one scheduler across replicas** — held across the idle wait too, so 5,000 rows/tier/cycle is a cluster-wide bound, not a per-replica multiplier. The cadence adapts to the `hasMore` bit the previous batch reported: 5 minutes while draining, 6 hours once dry. Watch `orkestra_auth_token_sweep_backlog_estimate{tier}` reach zero; no manual intervention or interval change is expected. Redis unavailability logs a bounded warning and skips maintenance — never authentication.

- [ ] **Step 7: Commit**

```bash
cd /home/tore/orkestra
git add backend/internal/core/auth/maintenance.go backend/internal/core/auth/maintenance_test.go \
        backend/internal/core/auth/module.go backend/internal/core/auth/CLAUDE.md
git commit -m "feat(auth): elected self-draining refresh-token retention sweep

AuthModule gains Start/Stop. The cadence adapts to the backlog (5m while
hasMore, 6h once dry) rather than the batch: at a fixed 6h interval a
million-row backlog would take fifty days. One Redis-elected scheduler keeps
5000/tier/cycle a cluster-wide bound. ADR-0017 D7/D8."
```

---

### Task 17: Remove dead configuration and the stale 30-day claims

`COOKIE_MAX_AGE` is parsed into `CookieConfig.MaxAge` and **nothing reads that field** — the real cookie `Max-Age` comes from `refreshCookieMaxAge(jwt)`. Its comment (`// 24 hours in milliseconds`) is wrong twice over: `http.Cookie.MaxAge` is in seconds, so the shipped value would mean roughly 1000 days. An operator reading `.env` believes it controls session duration.

Three places state that `JWT_REFRESH_TOKEN_EXPIRY` defaults to 30 days. It is `7d` everywhere that matters.

**Files:**
- Modify: `backend/internal/shared/config/config.go:152` (the field), `:299` (the parse)
- Modify: `docker/docker-compose.dev.yml:68`, `docker/docker-compose.staging.yml:81`, `docker/docker-compose.prod.yml:56`
- Modify: `docker/.env.example:127`
- Modify: `backend/internal/core/auth/handlers/oauth_state_binding.go:155-160`
- Modify: `backend/internal/core/auth/CLAUDE.md` (lines ~181-182)
- Modify: `docker/CLAUDE.md` if it lists `COOKIE_MAX_AGE`

**Interfaces:** removes `config.CookieConfig.MaxAge`.

- [ ] **Step 1: Prove the field is unread before deleting it**

Run:
```bash
cd /home/tore/orkestra/backend
grep -rn "Cookie.MaxAge\|CookieConfig{" --include=*.go . | grep -v _test
grep -rn "MaxAge" --include=*.go internal/shared/utils/http.go
```
Expected: `CookieConfig.MaxAge` is written only in `config.go`; `utils.CookieOptions.MaxAge` is fed only by literals and by `refreshCookieMaxAge(jwt)`, never by config. If that is not what you see, **stop** — the field is live and this step is wrong.

- [ ] **Step 2: Delete the field, the parse, and the env declarations**

Remove `MaxAge int` from `CookieConfig` (`config.go:152`) and the `MaxAge: getEnvAsInt("COOKIE_MAX_AGE", 86400000),` line (`:299`).

Remove the `COOKIE_MAX_AGE:` line from all three compose files and from `.env.example`.

An operator with the variable still in their live `docker/.env` is not broken: it was ignored before and is ignored after.

- [ ] **Step 3: Correct the stale 30-day claims**

`backend/internal/core/auth/handlers/oauth_state_binding.go`, the `refreshCookieMaxAge` comment:

```go
// refreshCookieMaxAge keeps the browser cookie and the refresh token it
// carries on the same clock. They used to disagree: the cookie was
// pinned at 7 days while the token's own TTL came from
// JWT_REFRESH_TOKEN_EXPIRY. Note that the shipped default for that key is
// 7d, not the 30d this comment used to claim — the 30d figure is the
// unreachable NewJWTService zero-guard, never a configured value.
```

`backend/internal/core/auth/CLAUDE.md`, the env-var table row:

| `JWT_REFRESH_TOKEN_EXPIRY` | Refresh-token TTL — and, because rotation writes `now + this` on every use, the **idle** timeout: this many days without a refresh ends the session. The absolute cap is the separate `sessionAbsoluteTTL`. The `refreshTTL <= 0 → 720h` guard in `NewJWTService` is unreachable through configuration. | `7d` |

- [ ] **Step 4: Verify and commit**

Run: `cd backend && go build ./... && go test ./internal/shared/... -count=1 && cd .. && grep -rn "COOKIE_MAX_AGE" backend/ docker/ | grep -v '\.env$'`
Expected: build and tests PASS; the grep returns nothing.

```bash
cd /home/tore/orkestra
git add backend/internal/shared/config/config.go docker/ backend/internal/core/auth/
git commit -m "chore(auth): remove dead COOKIE_MAX_AGE and correct the 30-day refresh claims

Nothing read CookieConfig.MaxAge — the real cookie Max-Age comes from
refreshCookieMaxAge(jwt) — while its comment said 'milliseconds' for a field
Go measures in seconds, so the shipped value would have meant ~1000 days. An
operator reading .env believed it controlled session duration."
```

---

### Task 18: Publish the documentation

Half of the seven findings **are** documentation defects, so this is part of the work, not its tail. `docs/Authentication_flow.md` is deliberately **not** touched — the root `CLAUDE.md` marks it as the drifted duplicate.

Two constraints from the docs pipeline: **nothing in this repo's CI builds the site**, so the pages must be rendered locally before merge; and only a push to `main` publishes.

**Files:**
- Modify: `docs/site/modules/core/auth.mdx`
- Modify: `docs/site/architecture/authentication-flow.mdx:121`
- Verify: `backend/internal/core/auth/CLAUDE.md` (every edit from Tasks 1–17 landed)

**Interfaces:** none.

- [ ] **Step 1: Reconcile the lifetime-chain bullet**

`backend/internal/core/auth/CLAUDE.md` line ~451 is the one bullet that states all three lifetimes at once, and every PR in this plan changed part of it. It currently reads `JWTService.RefreshTokenTTL()` (`JWT_REFRESH_TOKEN_EXPIRY` → 30d) — wrong default — and does not mention the cap at all. Rewrite it as:

> **Token lifetimes come from config, never from literals.** `JWTService.AccessTokenTTL(ctx)` resolves `admin accessTokenTTL → JWT_ACCESS_TOKEN_EXPIRY → 15m` — all three levels reachable since ADR-0017, with the effective value clamped to 24h so it can never outlive its revocation-denylist entry. `JWTService.RefreshTokenTTL()` resolves `JWT_REFRESH_TOKEN_EXPIRY → 7d` (the unreachable 720h zero-guard in `NewJWTService` is not a configured default). They drive every `expiresIn` in a response, the `expiresAt` on each persisted refresh row, and the `Max-Age` on every refresh cookie. Because rotation rewrites the refresh row's expiry on every use, the refresh TTL is the session's **idle** timeout, not its total lifetime; the total is bounded separately by `sessionAbsoluteTTL` (ADR-0017 D1). The lifetime deliberately kept separate from all three is `models.AuthSessionRetention` (90d): the session **document** is audit and device history that the risk scorer reads, and nothing authenticates off it.

- [ ] **Step 2: Audit that the contract doc is complete**

Run:
```bash
cd /home/tore/orkestra
grep -n "30 days\|30d\|CleanupRevokedTokens\|COOKIE_MAX_AGE\|access-token TTL + 1min" backend/internal/core/auth/CLAUDE.md
```
Expected: no hits except where the text explicitly frames them as superseded. Anything else is a Task 1–17 edit that did not land — fix it now, in this branch.

- [ ] **Step 3: Update the docs-site module page**

In `docs/site/modules/core/auth.mdx`, add a "Session lifetime" section that names the three lifetimes separately, since the point of ADR-0017 is that they stop being one undifferentiated thing:

```mdx
## Session lifetime

Three separate lifetimes govern how long a user stays signed in. Before
ADR-0017 only the first two existed and the second was undocumented as a
timeout at all.

| Lifetime | Controlled by | Default | What it means |
|---|---|---|---|
| Access token | admin `accessTokenTTL` → `JWT_ACCESS_TOKEN_EXPIRY` → 15m | 15m | How long a minted access token is accepted. Range 1m–24h; longer values are clamped, because the Redis revocation denylist stores entries for 24h + 1m and a token must never outlive its own revocation entry. |
| Idle window | `JWT_REFRESH_TOKEN_EXPIRY` | 7d | **This is the idle timeout.** Rotation writes a fresh `now + this` on every refresh, so it ends a session only after this long *without* activity. It is not a separate control. |
| Absolute cap | admin `sessionAbsoluteTTL` | 30d (`720h`) | The maximum total age of a session, measured from login, independent of activity. When it elapses the user must sign in again. Range 1h–89d; **empty disables the cap.** |

Reaching the absolute cap is a logout, not a denial: the session's refresh
tokens are revoked, the session document is marked inactive, and the session id
is pushed onto the revocation denylist — the same three steps as an
administrative termination. The client receives a 401 with
`code: "session_max_age_reached"`.

Both refresh paths enforce the cap. `GET /v1/auth/session` mints an access token
without rotating the refresh cookie (a deliberate anti-replay split), so a
client calling only that endpoint would otherwise hold a session open forever.

### Upgrading

Deploying the absolute cap signs out sessions that began more than
`sessionAbsoluteTTL` ago, on their next refresh. Set the field to empty at
`/admin/modules` to keep the previous unbounded behaviour.

### Retention

Session documents are audit and device history — nothing authenticates off them —
and are kept 90 days by a TTL index on `expiresAt`. Refresh-token rows are
deleted by a bounded background sweep once their token can no longer pass
temporal validation, regardless of revocation state; an unexpired rotated row is
never deleted, because that is exactly what replay detection matches against.
```

- [ ] **Step 4: Fix the canonical authentication-flow page**

`docs/site/architecture/authentication-flow.mdx:121` says the refresh expiry defaults to 30d. Replace with:

> Same shape, `type: "refresh"`, longer expiry (`JWT_REFRESH_TOKEN_EXPIRY`, default `7d`). Because rotation rewrites the expiry on every use, this value is the session's **idle** timeout; its absolute lifetime is bounded separately by `sessionAbsoluteTTL` (see [ADR-0017](/adr/0017-session-lifetime-and-token-retention)). Refresh tokens carry the **same `aud` claim** as the access token they paired with, so a refresh token issued for the operator host cannot be redeemed on the client host.

- [ ] **Step 5: Render the site locally**

Nothing in this repo's CI builds the site, so a broken page merges silently. Follow the recipe in `docs/site/README.md` and confirm both edited pages render, including the new table and the ADR link.

Run: the commands `docs/site/README.md` specifies.
Expected: a clean build; open `/modules/core/auth` and `/architecture/authentication-flow` and read them.

- [ ] **Step 6: Full gate**

Run: `make ci-backend && make ci-frontend-admin`
Expected: PASS. `openapi-check` is unaffected — no routes changed.

- [ ] **Step 7: Commit and open PR 3**

```bash
cd /home/tore/orkestra
git add docs/site/ backend/internal/core/auth/CLAUDE.md
git commit -m "docs(auth): document the three session lifetimes and the retention rules

Half of the ADR-0017 findings were documentation defects: the refresh TTL was
never described as the idle timeout, the 30-day refresh default was stated in
three places and is 7d, and the collections table claimed sessions need no TTL."
git push -u origin feat/adr-0017-pr3-auth-retention
gh pr create --base dev --title "feat(auth): session retention and hygiene (ADR-0017 PR 3)" --body "$(cat <<'BODY'
Implements PR 3 of docs/superpowers/specs/2026-08-21-session-lifetime-design.md (ADR-0017 D7/D8).

## ⛔ Rollout gates — do these before deploying, not during

1. **Count zero/near-zero `expiresAt` session documents** on staging **and**
   production. A zero value serialises as a year-1 BSON date and the new TTL
   index deletes those rows **immediately**:
   `db.operator_sessions.countDocuments({expiresAt: {$lt: ISODate("2000-01-01")}})`
   (and `client_sessions`). A non-zero count blocks the deploy.
2. **Count the eligible token-sweep backlog** on both environments.
3. **Time the `(expiresAt, uuid)` index build on a production-sized copy.**
   Mongo 8 builds with minimal locking, but on a busy replica set the duration
   and its replication lag belong in the maintenance window, not discovered
   mid-deploy.

## What changes

- TTL index on `operator_sessions` / `client_sessions` `expiresAt`; the
  repository retention fallback corrected from 30 to 90 days to match both
  callers; the dead `TerminateExpiredSessions` removed.
- `CleanupExpiredTokens` becomes bounded — `(limit) → (deleted, hasMore, error)`,
  5,000 rows per tier per cycle, `hasMore` from the 5,001st selected row so the
  drain path never runs `CountDocuments`. `CleanupRevokedTokens` removed:
  revocation age alone is never a safe deletion criterion.
- A Redis-lease-elected scheduler whose **cadence** adapts to the backlog
  (5 minutes while draining, 6 hours once dry). At a fixed 6-hour interval a
  one-million-row backlog would take fifty days; adaptively it clears in under
  a day.
- New metrics: `orkestra_auth_token_sweep_{deleted_total,backlog_estimate,duration_seconds}{tier}`.
- `COOKIE_MAX_AGE` removed — nothing read it, and its comment was wrong twice over.

## Staging verification before promotion

Confirm each batch stays at or below 5,000, that the loop switches from the
drain interval back to idle when the backlog reaches zero, and measure duration
and total drain time.

## Docs

`docs/site` pages rendered locally (this repo's CI does not build the site).
BODY
)"
```

---

## Rollout

Ordered, and every item is a gate rather than a suggestion.

- [ ] 1. Count zero/near-zero `expiresAt` session documents **and** the eligible token-sweep backlog on staging **and** production, before deploying PR 3.
- [ ] 2. Time the `(expiresAt, uuid)` index build on a production-sized copy before deploying PR 3.
- [ ] 3. Deploy PR 1 to staging. **Verify a revoked session stays revoked after raising `accessTokenTTL` above the old denylist TTL** — this is the regression that motivated the work and must be confirmed against a running stack, not only in unit tests.
- [ ] 4. Diff the live `docker/.env` against `.env.example` for `JWT_ACCESS_TOKEN_EXPIRY` before each environment's upgrade. The value may activate for the first time.
- [ ] 5. Deploy PR 2 to staging. Confirm sessions older than the cap are signed out and that `orkestra_auth_session_cap_expiries_total` moves, then settles.
- [ ] 6. Deploy PR 3 to staging. Verify each batch stays at or below 5,000, confirm the loop returns from the drain interval to the idle one when the backlog reaches zero, and measure duration and drain time before promoting.
- [ ] 7. Review `orkestra_auth_session_anchor_anomalies_total` for at least 30 consecutive production days. If it remains zero everywhere, implement the already-filed fail-closed follow-up in the next minor release; otherwise repair the data cause and restart the window.

## Out of scope

Recorded here so a reviewer does not read these as omissions:

- **Generic schema-driven config validation in the SDK.** The `HasConfigValidator` seam is a callback only; teaching `UpdateConfig` to interpret every `ConfigField` constraint — including new `MinDuration`/`MaxDuration` fields — is a broader contract change deserving its own ADR.
- **Per-tier session caps.** One field covers both tiers; splitting follows the `loginEnabledAdmin`/`loginEnabledClient` precedent if a need appears.
- **An idle timeout configurable independently of the refresh TTL.** They are the same control today, and separating them means a second expiry on the refresh row with no demonstrated demand.
- **`/session` extending the idle window.** The non-rotating bootstrap is a deliberate anti-replay split; changing it would reopen the race it was built to close.
- **Bounds on `accountLockoutDuration` / `accountLockoutThreshold`.** Neither governs an already-issued credential.
