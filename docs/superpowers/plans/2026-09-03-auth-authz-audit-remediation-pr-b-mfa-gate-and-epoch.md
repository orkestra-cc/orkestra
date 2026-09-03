# Auth/Authz Audit Remediation — PR B: Enrolment Gate and MFA Epoch — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make a stolen session-only bearer unable to add, replace or remove a second factor, or to mint an MFA-satisfied token by any route (H-2, H-3, M-1, M-3), and make the removal or reset of a factor end what that factor authorised — immediately, in every session, including the caller's own (M-2).

**Architecture:** Two mechanisms answering two different questions. A new gate, `RequireEnrolmentProof(maxAge)`, demands a **fresh proof on every enrolment**: the step-up proof for a user who already has a factor, or a recent interactive login — carried by a new `auth_time` JWT claim — for a user who does not; anything else is a new 401 `reauthentication_required` that both SPAs turn into a re-login with return. Separately, an **MFA epoch** (`User.MFAEpoch`, claim `mfae`) is bumped by every credential removal or replacement and compared on every request that consumes an MFA marker, so authority minted under a removed factor dies at once without logging the user out; refresh recomputes the markers against it rather than copying them forward. `RemoveFactor` finally removes *both* factor rows, every credential change is announced (security event + email), and the authenticated MFA-verify routes get the attempt cap PR A built.

**Tech Stack:** Go 1.26.8, Huma v2.39.1, MongoDB 8 (`$inc` on the user document), RS256 JWT (`jwt_service.go` claim map), React 19 + RTK Query (`frontend-admin/src/services/baseApi.ts`), React 19 + `authedFetch` (`frontend-client`), Vitest + RTL + MSW.

**Spec:** `docs/superpowers/specs/2026-09-03-auth-authz-audit-remediation-design.md` **v1.12** — this plan implements the **PR B** row of §7, i.e. §4.2 (**D11–D14**) and §4.3 (**D15–D20**) in full, plus the §4.11 documentation lines that describe them and the §6 "PR B — MFA" test list.

**Depends on:** **PR A must be merged first** — D20's attempt cap consumes `AttemptCounter`, `AttemptKeyMFAVerify` and `MFAVerifyLimit`, all of which PR A ships.

## Global Constraints

- **Fresh proof on EVERY enrolment.** No branch of `RequireEnrolmentProof` lets a session with no proof add a factor. The v1 design (no factor → no proof needed) was rejected in spec review round 1: it contradicts goal 2 and enables takeover-by-lockout.
- **The gate fails CLOSED on a degraded lookup.** `mfaEnrollment == nil` or a lookup error → `sendStepUpRequired`. A degraded Mongo must never let a factor be added without proof.
- **A user WITH a factor gets exactly one answer.** `step_up_required` — never `password_confirm_required`, never `mfa_enrollment_required`.
- **A user WITHOUT a factor and without freshness gets `reauthentication_required`,** not `password_confirm_required`: the users most in need of a first enrolment are MFA-obligated accounts in their grace window, whom password-confirm refuses (D19), and OAuth-only accounts have no password to reconfirm.
- **`auth_time` is stamped by every path that creates a session and by nothing else.** Refresh carries it unchanged; password-confirm and step-up mints keep the session's value. A refresh is not an authentication.
- **`mfae` is bumped by removal and replacement only — never by an addition.** Authority proven by a factor that still exists stays valid.
- **A stale epoch reads exactly as "no MFA markers".** `step_up_required` from the gates, `false` to Cedar. Never fail open; a user-lookup error is "not current".
- **One rule for every credential removal or replacement** — TOTP removal, TOTP replacement, any passkey removal, admin reset: bump the epoch, revoke device trust, end every session but the caller's own (all of them on the admin path). What differs between paths is only who the caller is.
- **Both halves of each enrolment ceremony are gated** (`begin` *and* `confirm`/`finish`): the factor set can change between them.
- **Every SDK change is additive.** `iface.User` gains one field; `iface.MFAEpochBumper` is a new sub-interface resolved with `module.GetTyped`. `iface.UserProvider` is **not** widened.
- **Docs move in the same commit as the code:** `backend/internal/core/auth/CLAUDE.md`, `backend/internal/core/user/CLAUDE.md`, `backend/pkg/sdk/CLAUDE.md`, `docs/site/modules/core/auth.mdx`, `docs/site/architecture/authentication-flow.mdx` (claims table gains `auth_time` **and** `mfae`).
- **Test commands** (from `/home/tore/orkestra/backend` unless stated):
  - `go test ./internal/core/auth/... ./internal/shared/middleware/... -count=1`
  - `go vet ./...` before every commit
  - frontend: `cd frontend-admin && npx vitest run src/services && npm run typecheck && npm run lint`; `cd frontend-client && npx vitest run && npm run typecheck && npm run lint`
  - full gate: `make -C /home/tore/orkestra ci-backend` and `make -C /home/tore/orkestra ci-frontend-client`
  - live Mongo where guarded: `MONGO_TEST_URI='mongodb://127.0.0.1:28017/?directConnection=true'`
- **Never start servers manually.** **Commit trailer:** `Claude-Session: https://claude.ai/code/session_0155Lt3QwR9zX15oGNKrn1T1`

## Declared deviations from the spec (read before executing)

1. **`mfaAuthority(ctx)` is a method on `AuthMiddleware`, memoised in the request context under a new unexported context key.** The spec names the resolver and its memoisation but not where the key lives; putting it beside `ctxClaims` in `auth.go` keeps it unexported and untestable from outside the package, which is correct for a cache.
2. **The epoch check is skipped entirely for tokens with no MFA markers.** The spec says "tokens without MFA markers cost nothing"; this plan makes that explicit as the first line of the resolver, so the common request path adds zero database reads.
3. **`amrSatisfiesStepUp` lives next to `amrSatisfiesMFA` in `auth.go`,** not in a new file. Two four-line predicates that must be read together belong together.
4. **The notification template `auth.mfa_factor_added` is seeded through the same path as `auth.new_device_login`.** The spec says "seeded and localised exactly like"; this plan names `cmd/server`'s seeding parity test as the gate, because that is what actually fails when a template is added without its EN/IT pair.
5. **`RemoveFactor`'s signature is unchanged.** It gains the WebAuthn deletion and the epoch bump internally; the spec does not ask for a new parameter and every caller already passes `actorUUID`.

## File Structure

**Backend — `backend/pkg/sdk/iface/`**

| File | Responsibility | Task |
|---|---|---|
| `user_types.go` | + `User.MFAEpoch int` | 1 |
| `interfaces.go` | + `MFAEpochBumper` | 1 |

**Backend — `backend/internal/core/user/`**

| File | Responsibility | Task |
|---|---|---|
| `repository/user_repository.go` | + `BumpMFAEpoch` (`$inc`) | 1 |
| `services/user_service.go` | implements `iface.MFAEpochBumper` | 1 |
| `CLAUDE.md` | `mfaEpoch` field + the seam | 1 |

**Backend — `backend/internal/core/auth/`**

| File | Responsibility | Task |
|---|---|---|
| `models/token.go` | + `JWTClaims.AuthTime`, `JWTClaims.MFAEpoch` | 2 |
| `services/jwt_service.go` | `claimsToMap` / `mapToClaims` for `auth_time` + `mfae`; the stale `:491-492` comment | 2, 6 |
| `services/jwt_validator.go` | sidecar `parseClaims` mirrors both | 2 |
| `services/password_auth_service.go` | stamp `auth_time` at `issueTokensForSession` + the setup wizard | 2 |
| `services/auth_service.go` | stamp on OAuth + relay; `carryAMR` on both refresh paths | 2, 6 |
| `services/mfa_service.go` | `RemoveFactor` deletes both rows; epoch bumps; replacement side effects; security events | 4, 5 |
| `services/webauthn_service.go` | `SetAuditSink`; epoch bump on `RemoveCredential` | 4, 5 |
| `handlers/mfa_handler.go` | `RegisterEnrolmentRoutes`; `sessions` dependency; `AdminReset`; `Verify` attempt cap | 3, 5, 8 |
| `handlers/webauthn_handler.go` | `RegisterEnrolmentRoutes`; `VerifyFinish` attempt cap | 3, 8 |
| `module.go` | mount enrolment routes under the new gate; resolve `MFAEpochBumper`; wire `sessions` | 3, 5 |
| `CLAUDE.md` | enrolment gate, `auth_time`, epoch, refresh recomputation, reset semantics | 3, 6, 9 |

**Backend — `backend/internal/shared/middleware/`**

| File | Responsibility | Task |
|---|---|---|
| `auth.go` | `RequireEnrolmentProof`, `mfaAuthority`, `amrSatisfiesStepUp`, strict `amrSatisfiesMFA`, `sendReauthenticationRequired` | 3, 6 |
| `coded_error_golden_test.go` | the new envelope | 3 |

**Frontend**

| File | Responsibility | Task |
|---|---|---|
| `frontend-admin/src/services/baseApi.ts` | `reauthentication_required` → `/login?next=` | 7 |
| `frontend-client/src/lib/authedFetch.ts` | same, via `sanitizeNext` | 7 |
| `frontend-client/src/pages/MfaEnrolPage.tsx` | read `/me/mfa` first; enrolled state; 401 branches | 7 |

**Notification**

| File | Responsibility | Task |
|---|---|---|
| `backend/internal/core/notification/…/templates` | `auth.mfa_factor_added` EN + IT | 5 |

---

## Task 1: The MFA epoch's storage and its SDK seam (D16, first half)

One additive field, one additive interface, one `$inc`. No behaviour yet.

**Files:**
- Modify: `backend/pkg/sdk/iface/user_types.go`
- Modify: `backend/pkg/sdk/iface/interfaces.go`
- Modify: `backend/internal/core/user/repository/user_repository.go`
- Modify: `backend/internal/core/user/services/user_service.go`
- Modify: `backend/pkg/sdk/CLAUDE.md`, `backend/internal/core/user/CLAUDE.md`
- Test: `backend/internal/core/user/services/mfa_epoch_test.go` (new)

**Interfaces:**
- Consumes: nothing.
- Produces:
  - `iface.User.MFAEpoch int` (`bson:"mfaEpoch,omitempty" json:"-"`)
  - `iface.MFAEpochBumper interface { BumpMFAEpoch(ctx context.Context, userUUID string) (int, error) }`
  - `repository.BumpMFAEpoch(ctx, userUUID string) (int, error)`
- Later tasks rely on: `module.GetTyped[iface.MFAEpochBumper]` against the tier's user provider (Task 5); `user.MFAEpoch` read at every mint (Task 2).

- [ ] **Step 1: Write the failing test**

Create `backend/internal/core/user/services/mfa_epoch_test.go`:

```go
package services

import (
	"context"
	"testing"

	"github.com/orkestra/backend/pkg/sdk/iface"
)

// The epoch is what makes a factor removal take effect on the CALLER's
// current token, without waiting for a refresh and without depending on
// a revocation write succeeding. It must be monotone and it must start
// at zero for every document that predates it.
func TestBumpMFAEpoch_IsMonotone(t *testing.T) {
	svc, repo := newUserServiceForEpochTest(t)
	ctx := context.Background()
	uuid := repo.seedUser(t, "u-1")

	got, err := svc.BumpMFAEpoch(ctx, uuid)
	if err != nil {
		t.Fatalf("BumpMFAEpoch: %v", err)
	}
	if got != 1 {
		t.Fatalf("first bump = %d, want 1 (an absent field reads as 0)", got)
	}
	got, err = svc.BumpMFAEpoch(ctx, uuid)
	if err != nil {
		t.Fatalf("BumpMFAEpoch: %v", err)
	}
	if got != 2 {
		t.Fatalf("second bump = %d, want 2", got)
	}

	user, err := svc.GetUserByID(ctx, uuid)
	if err != nil {
		t.Fatalf("GetUserByID: %v", err)
	}
	if user.MFAEpoch != 2 {
		t.Fatalf("persisted MFAEpoch = %d, want 2", user.MFAEpoch)
	}
}

// Every document written before this ships has no mfaEpoch. It must
// read as 0 and match every pre-deploy token, so the deploy itself
// downgrades nobody (edge case 12).
func TestUser_MissingMFAEpochReadsAsZero(t *testing.T) {
	svc, repo := newUserServiceForEpochTest(t)
	uuid := repo.seedUserWithoutEpochField(t, "u-legacy")

	user, err := svc.GetUserByID(context.Background(), uuid)
	if err != nil {
		t.Fatalf("GetUserByID: %v", err)
	}
	if user.MFAEpoch != 0 {
		t.Fatalf("MFAEpoch = %d for a legacy document, want 0", user.MFAEpoch)
	}
}

func TestBumpMFAEpoch_UnknownUserIsAnError(t *testing.T) {
	svc, _ := newUserServiceForEpochTest(t)
	if _, err := svc.BumpMFAEpoch(context.Background(), "does-not-exist"); err == nil {
		t.Fatal("bumping a user that does not exist must be an error, never a silent 0")
	}
}

// The seam is what the auth module resolves; a compile-time assertion is
// cheaper than discovering the mismatch at boot.
func TestUserService_ImplementsMFAEpochBumper(t *testing.T) {
	var _ iface.MFAEpochBumper = (*userService)(nil)
}
```

> `newUserServiceForEpochTest` follows whatever harness the neighbouring
> user-service tests use. If they are `MONGO_TEST_URI`-guarded integration
> tests, guard this file the same way and skip when the variable is
> unset — do not invent a second fake repository.

- [ ] **Step 2: Run it to verify it fails**

Run: `MONGO_TEST_URI='mongodb://127.0.0.1:28017/?directConnection=true' go test ./internal/core/user/services/ -run MFAEpoch -count=1`
Expected: FAIL — `svc.BumpMFAEpoch undefined`, `user.MFAEpoch undefined`.

- [ ] **Step 3: Add the field**

In `backend/pkg/sdk/iface/user_types.go`, next to `MFAGraceStartedAt`:

```go
	// MFAEpoch is bumped by every REMOVAL or REPLACEMENT of an MFA
	// credential (TOTP removal, TOTP replacement, any passkey removal,
	// admin reset) and never by an addition. Every access token carries
	// the epoch it was minted under in the "mfae" claim; a request whose
	// token epoch is behind this value has its MFA markers ignored, so
	// authority proven by a factor the user no longer holds dies
	// immediately in EVERY session — the caller's included — without
	// waiting for a refresh and without depending on a revocation write
	// succeeding.
	//
	// Absent on every document written before this shipped, which reads
	// as 0 and matches every pre-deploy token: the deploy downgrades
	// nobody, and the first removal on such an account moves it to 1.
	MFAEpoch int `bson:"mfaEpoch,omitempty" json:"-"`
```

- [ ] **Step 4: Add the seam**

In `backend/pkg/sdk/iface/interfaces.go`, next to `UserLifecycleStateProvider`:

```go
// ---------------------------------------------------------------------------
// MFAEpochBumper — consumed by: the auth module's MFA service and WebAuthn
// service, on every credential removal or replacement. Narrow on purpose
// (the UserLifecycleStateProvider precedent): UserProvider is implemented by
// forks and stays additive-only, and this is one monotone counter, not
// profile data.
//
// Resolve it with module.GetTyped against the tier's user provider
// (ServiceOperatorUserProvider / ServiceClientUserProvider). A missing value
// means the epoch never moves, so the platform degrades to the pre-epoch
// behaviour — session revocation alone — which is why the removal paths log
// at WARN when the seam is absent rather than failing.
// ---------------------------------------------------------------------------

type MFAEpochBumper interface {
	// BumpMFAEpoch increments the user's MFA epoch and returns the new
	// value. It is a single atomic $inc: concurrent removals converge and
	// neither can observe the other's value as its own.
	BumpMFAEpoch(ctx context.Context, userUUID string) (int, error)
}
```

- [ ] **Step 5: Implement the repository method**

In `backend/internal/core/user/repository/user_repository.go`, following the shape of the neighbouring single-field updates:

```go
// BumpMFAEpoch increments mfaEpoch and returns the new value in one
// round trip (FindOneAndUpdate with ReturnDocument: After). A read-then-
// write would let two concurrent removals both write the same value,
// which would leave one of the two removals' tokens still valid.
func (r *UserRepository) BumpMFAEpoch(ctx context.Context, userUUID string) (int, error) {
	var out struct {
		MFAEpoch int `bson:"mfaEpoch"`
	}
	opts := options.FindOneAndUpdate().
		SetReturnDocument(options.After).
		SetProjection(bson.M{"mfaEpoch": 1})
	err := r.collection.FindOneAndUpdate(ctx,
		bson.M{"uuid": userUUID, "deletedAt": bson.M{"$exists": false}},
		bson.M{"$inc": bson.M{"mfaEpoch": 1}, "$set": bson.M{"updatedAt": time.Now()}},
		opts,
	).Decode(&out)
	if err != nil {
		return 0, err
	}
	return out.MFAEpoch, nil
}
```

> Match the collection field name and the soft-delete predicate the file
> already uses — read two neighbouring methods before writing this one.

- [ ] **Step 6: Implement the service method**

In `backend/internal/core/user/services/user_service.go`:

```go
// Compile-time proof the auth module's module.GetTyped resolution will
// succeed. A missing method would otherwise surface as a silent
// "seam absent" WARN at the first factor removal.
var _ iface.MFAEpochBumper = (*userService)(nil)

// BumpMFAEpoch implements iface.MFAEpochBumper.
func (s *userService) BumpMFAEpoch(ctx context.Context, userUUID string) (int, error) {
	if userUUID == "" {
		return 0, fmt.Errorf("user uuid is required")
	}
	return s.repo.BumpMFAEpoch(ctx, userUUID)
}
```

- [ ] **Step 7: Run the tests**

Run: `MONGO_TEST_URI='mongodb://127.0.0.1:28017/?directConnection=true' go test ./internal/core/user/... -count=1`
Expected: PASS.

- [ ] **Step 8: Document the seam**

- `backend/pkg/sdk/CLAUDE.md`: record `MFAEpochBumper` and `User.MFAEpoch` as additive, in the table the other narrow seams sit in.
- `backend/internal/core/user/CLAUDE.md`: add the `mfaEpoch` field to the user-document description and state that the auth module consumes the bumper.

- [ ] **Step 9: Vet and commit**

```bash
go vet ./... && go test ./internal/core/user/... ./pkg/sdk/... -count=1
cd /home/tore/orkestra && git add backend/pkg/sdk backend/internal/core/user
git commit -m "$(cat <<'EOF'
feat(user): add the MFA epoch field and its additive SDK seam

iface.User gains MFAEpoch and iface gains MFAEpochBumper, a one-method
sub-interface implemented by the user service as a single atomic $inc.
UserProvider is not widened — forks implement it and it stays
additive-only, the UserLifecycleStateProvider precedent.

The epoch is what will let a factor removal end MFA authority in the
caller's CURRENT token, rather than waiting for a refresh or depending
on a revocation write succeeding. Nothing bumps it yet.

An absent field reads as 0 and matches every pre-deploy token, so the
deploy itself downgrades nobody.

Spec §4.3 D16.

Claude-Session: https://claude.ai/code/session_0155Lt3QwR9zX15oGNKrn1T1
EOF
)"
```

---

## Task 2: The two new claims (D11 `auth_time`, D16 `mfae`)

Both claims are carried by the token, both are written by exactly the mints that own them, both read as zero when absent. Still no gate.

**Files:**
- Modify: `backend/internal/core/auth/models/token.go`
- Modify: `backend/internal/core/auth/services/jwt_service.go` (`claimsToMap` `:675-678`, `mapToClaims` `:709-712`)
- Modify: `backend/internal/core/auth/services/jwt_validator.go` (`parseClaims`)
- Modify: `backend/internal/core/auth/services/password_auth_service.go` (`issueTokensForSession`, the setup wizard's mint `:423-430`)
- Modify: `backend/internal/core/auth/services/auth_service.go` (OAuth callback `:1356`, `:2377-2388`; relay completion)
- Modify: `backend/internal/shared/devtoken/…` (the dev-token minter)
- Test: `backend/internal/core/auth/services/jwt_service_amr_test.go`

**Interfaces:**
- Consumes: `iface.User.MFAEpoch` (Task 1).
- Produces:
  - `models.JWTClaims.AuthTime int64` (wire `auth_time`)
  - `models.JWTClaims.MFAEpoch int` (wire `mfae`)
- Later tasks rely on: `claims.AuthTime` (Task 3's gate), `claims.MFAEpoch` (Task 6's resolver and `carryAMR`).

- [ ] **Step 1: Write the failing round-trip tests**

Append to `backend/internal/core/auth/services/jwt_service_amr_test.go`:

```go
// auth_time is the OIDC name for "when the interactive authentication
// that created this session happened". It is what lets a user with NO
// factor prove freshness for a first enrolment; a refresh must carry it
// unchanged, because a refresh is not an authentication.
func TestClaims_AuthTimeRoundTrips(t *testing.T) {
	now := time.Now().Unix()
	in := &models.JWTClaims{UserUUID: "u-1", AuthTime: now}

	m := claimsToMap(in)
	if got, ok := m["auth_time"]; !ok || int64(got.(int64)) != now {
		t.Fatalf("auth_time = %v (present=%v), want %d", m["auth_time"], ok, now)
	}
	out := mapToClaims(m)
	if out.AuthTime != now {
		t.Fatalf("AuthTime = %d after round trip, want %d", out.AuthTime, now)
	}
}

// Zero must be OMITTED, not written as 0 — a pre-deploy token has no
// auth_time and a freshly minted one carrying a literal zero would be
// indistinguishable from it in a log.
func TestClaims_AuthTimeOmittedWhenZero(t *testing.T) {
	m := claimsToMap(&models.JWTClaims{UserUUID: "u-1"})
	if _, present := m["auth_time"]; present {
		t.Fatal("auth_time must be omitted when zero")
	}
}

func TestClaims_MFAEpochRoundTrips(t *testing.T) {
	in := &models.JWTClaims{UserUUID: "u-1", MFAEpoch: 7}
	m := claimsToMap(in)
	out := mapToClaims(m)
	if out.MFAEpoch != 7 {
		t.Fatalf("MFAEpoch = %d after round trip, want 7", out.MFAEpoch)
	}
}

func TestClaims_MFAEpochOmittedWhenZero(t *testing.T) {
	m := claimsToMap(&models.JWTClaims{UserUUID: "u-1"})
	if _, present := m["mfae"]; present {
		t.Fatal("mfae must be omitted when zero")
	}
}

// An absent claim reads as 0, which is what matches every pre-deploy
// token against a user document that has no mfaEpoch either.
func TestClaims_AbsentClaimsReadAsZero(t *testing.T) {
	out := mapToClaims(map[string]interface{}{"sub": "u-1"})
	if out.AuthTime != 0 || out.MFAEpoch != 0 {
		t.Fatalf("AuthTime=%d MFAEpoch=%d, want 0/0", out.AuthTime, out.MFAEpoch)
	}
}

// The sidecar validator parses claims independently; drift between the
// two parsers is how a gate ends up reading a claim the minter writes
// under a different key.
func TestJWTValidator_ParsesAuthTimeAndMFAEpoch(t *testing.T) {
	raw := map[string]interface{}{
		"sub": "u-1", "auth_time": float64(1735689600), "mfae": float64(3),
	}
	c := parseClaims(raw)
	if c.AuthTime != 1735689600 {
		t.Errorf("AuthTime = %d", c.AuthTime)
	}
	if c.MFAEpoch != 3 {
		t.Errorf("MFAEpoch = %d", c.MFAEpoch)
	}
}
```

And a stamping test per mint path:

```go
// auth_time is stamped by every path that CREATES a session, and by
// nothing else. A gap here is a user who cannot enrol a first factor
// without an unexplained re-login.
func TestAuthTime_StampedByEverySessionCreatingMint(t *testing.T) {
	cases := []struct {
		name string
		mint func(t *testing.T) *models.JWTClaims
	}{
		{"password login", mintViaPasswordLogin},
		{"mfa login completion", mintViaMFACompletion},
		{"passkey login completion", mintViaPasskeyCompletion},
		{"oauth callback", mintViaOAuthCallback},
		{"client relay completion", mintViaRelayCompletion},
		{"setup wizard initial admin", mintViaSetupWizard},
		{"dev token", mintViaDevToken},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			claims := tc.mint(t)
			if claims.AuthTime == 0 {
				t.Fatal("auth_time must be stamped at session creation")
			}
			if delta := time.Since(time.Unix(claims.AuthTime, 0)); delta > time.Minute || delta < -time.Minute {
				t.Fatalf("auth_time is %v away from now", delta)
			}
		})
	}
}

// A refresh CARRIES auth_time: it describes the session's origin, not
// the token's. Password-confirm and step-up mints keep the session's
// value too — they prove a factor, they do not create a session.
func TestAuthTime_CarriedUnchangedByRefreshAndStepUp(t *testing.T) {
	origin := time.Now().Add(-2 * time.Hour).Unix()

	refreshed := mintViaRefresh(t, &models.JWTClaims{UserUUID: "u-1", AuthTime: origin})
	if refreshed.AuthTime != origin {
		t.Fatalf("refresh AuthTime = %d, want the session's original %d", refreshed.AuthTime, origin)
	}

	stepped := mintViaPasswordConfirm(t, &models.JWTClaims{UserUUID: "u-1", AuthTime: origin})
	if stepped.AuthTime != origin {
		t.Fatalf("password-confirm AuthTime = %d, want %d", stepped.AuthTime, origin)
	}
}
```

> The `mintVia*` helpers wrap the existing per-path test harnesses in this
> package. Where a path has no harness yet, build the smallest one that
> reaches the mint and returns the parsed claims — do not assert on the
> raw token string.

- [ ] **Step 2: Run it to verify it fails**

Run: `go test ./internal/core/auth/services/ -run 'Claims_AuthTime|Claims_MFAEpoch|AuthTime_|ParsesAuthTime' -count=1`
Expected: FAIL — `AuthTime` / `MFAEpoch` undefined on `JWTClaims`.

- [ ] **Step 3: Add the claims**

In `backend/internal/core/auth/models/token.go`, after `LastOTPAt`:

```go
	// AuthTime (OIDC "auth_time") is the unix time of the INTERACTIVE
	// authentication that created this session. Stamped by every path
	// that creates a session and by nothing else; a refresh carries it
	// unchanged, because a refresh is not an authentication, and so do
	// the password-confirm and step-up mints, which prove a factor
	// rather than creating a session.
	//
	// RequireEnrolmentProof reads it: a user with no factor proves
	// presence for a first enrolment with a recent auth_time. Absent on
	// every token minted before this shipped, which reads as stale and
	// costs such a user one re-login.
	AuthTime int64 `json:"auth_time,omitempty"`

	// MFAEpoch (claim "mfae") is the value of User.MFAEpoch when this
	// token was minted. Every request that CONSUMES an MFA marker
	// compares it against the user's current epoch; behind means the
	// markers are ignored, so authority proven by a removed factor dies
	// at once in every session. Absent reads as 0, which matches every
	// document that has no mfaEpoch.
	MFAEpoch int `json:"mfae,omitempty"`
```

- [ ] **Step 4: Serialise them**

In `jwt_service.go`'s `claimsToMap`, next to the `amr` / `last_otp_at` block:

```go
	if claims.AuthTime > 0 {
		m["auth_time"] = claims.AuthTime
	}
	if claims.MFAEpoch > 0 {
		m["mfae"] = claims.MFAEpoch
	}
```

and in `mapToClaims`:

```go
	claims.AuthTime = int64(getFloatClaim(m, "auth_time"))
	claims.MFAEpoch = int(getFloatClaim(m, "mfae"))
```

Mirror both in `jwt_validator.go`'s `parseClaims`.

- [ ] **Step 5: Stamp `auth_time` at every session-creating mint**

At each of these sites, set `AuthTime: time.Now().Unix()` on the claims being minted:

- `password_auth_service.go` — `issueTokensForSession` (`~:1575`) and the setup wizard's initial-admin mint (`:423-430`);
- `auth_service.go` — MFA login completion and passkey login completion (`IssueLoginTokensForSession`), the OAuth callback's token pair (`:1356`, `:2377-2388`), and the client-tier relay completion;
- the dev-token minter in `internal/shared/devtoken/`.

And confirm the *carrying* paths do **not** overwrite it: `RefreshTokensWithRiskAssessment`, `MintAccessTokenFromRefresh`, `ConfirmPasswordWithSecurity`, and the step-up mint all copy `claims.AuthTime` through.

- [ ] **Step 6: Stamp `mfae` at every mint**

Every path that mints an access token already holds the `*iface.User` (or can read it). Set `MFAEpoch: user.MFAEpoch` on the claims. Do **not** try to read it in the middleware at mint time — the value must be the one that was current when the token was created.

- [ ] **Step 7: Run the tests**

Run: `go test ./internal/core/auth/... ./internal/shared/devtoken/... -count=1`
Expected: PASS.

- [ ] **Step 8: Document the claims**

In `docs/site/architecture/authentication-flow.mdx`, add `auth_time` and `mfae` rows to the JWT claims table at `:120`, each with the "written by / carried by / read by" triple.

- [ ] **Step 9: Vet and commit**

```bash
go vet ./... && go test ./internal/core/auth/... -count=1
git add backend/internal/core/auth backend/internal/shared/devtoken docs/site
git commit -m "$(cat <<'EOF'
feat(auth): add the auth_time and mfae claims

auth_time (the OIDC name) records the interactive authentication that
created the session; it is stamped by every session-creating mint and
carried unchanged by refresh, password-confirm and step-up, because
none of those is an authentication. mfae records the MFA epoch the token
was minted under.

Both are omitted when zero and read as zero when absent, so a
pre-deploy token is well defined under both: its epoch matches a user
document that has no mfaEpoch, and its missing auth_time reads as stale.

The sidecar validator parses both, closing the drift that would
otherwise let a gate read a claim under a key the minter never writes.
Nothing consumes either claim yet.

Spec §4.2 D11, §4.3 D16.

Claude-Session: https://claude.ai/code/session_0155Lt3QwR9zX15oGNKrn1T1
EOF
)"
```

---

## Task 3: `RequireEnrolmentProof` and the enrolment route move (D11, D12)

The H-2 / H-3 fix. Both ceremonies move out of the plain protected group into a gated one.

**Files:**
- Modify: `backend/internal/shared/middleware/auth.go`
- Modify: `backend/internal/core/auth/handlers/mfa_handler.go` (`RegisterProtectedRoutes` `:704-740` → new `RegisterEnrolmentRoutes`)
- Modify: `backend/internal/core/auth/handlers/webauthn_handler.go`
- Modify: `backend/internal/core/auth/module.go` (`:1615-1619`, `:1693-1697`, `:1742-1746`, `:1755-1759`)
- Test: `backend/internal/shared/middleware/step_up_test.go`, `coded_error_golden_test.go`

**Interfaces:**
- Consumes: `claims.AuthTime` (Task 2), `MFAEnrollmentLookup` (`auth.go:59`), `amrSatisfiesStepUp` (introduced here, made epoch-aware in Task 6).
- Produces:
  - `func (m *AuthMiddleware) RequireEnrolmentProof(maxAge time.Duration) func(http.Handler) http.Handler`
  - `func (m *AuthMiddleware) sendReauthenticationRequired(w http.ResponseWriter, r *http.Request, maxAge time.Duration, authTime int64)`
  - `func amrSatisfiesStepUp(amr []string) bool`
  - `MFAHandler.RegisterEnrolmentRoutes(api huma.API, mount RouteMount)`
  - `WebAuthnHandler.RegisterEnrolmentRoutes(api huma.API, mount RouteMount)`
- Later tasks rely on: the gate's factor branch is rewritten to use `mfaAuthority` in Task 6; the SPAs consume `reauthentication_required` in Task 7.

- [ ] **Step 1: Write the failing gate tests**

Append to `backend/internal/shared/middleware/step_up_test.go`:

```go
// H-2 / H-3: enrolment was mounted under RequireGlobal() alone, so a
// stolen session-only bearer could add a passkey — or REPLACE the
// victim's TOTP secret, since ConfirmEnrollment deletes the existing
// factor after validating a code for the NEW one. Every enrolment now
// demands a fresh proof.

func TestRequireEnrolmentProof_NoClaimsIs401(t *testing.T) {
	rec := runEnrolmentGate(t, nil, enrolmentLookupFactor(false), 5*time.Minute)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
}

// A user WITH a factor has exactly one right answer: step_up_required.
// Never password_confirm_required (they have a stronger factor), never
// mfa_enrollment_required (they are already enrolled).
func TestRequireEnrolmentProof_WithFactorFreshProofPasses(t *testing.T) {
	claims := &models.JWTClaims{
		UserUUID: "u-1", AMR: []string{"pwd", "otp"}, LastOTPAt: time.Now().Unix(),
	}
	rec := runEnrolmentGate(t, claims, enrolmentLookupFactor(true), 5*time.Minute)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 — a fresh second factor is the proof", rec.Code)
	}
}

func TestRequireEnrolmentProof_WithFactorStaleProofIsStepUp(t *testing.T) {
	claims := &models.JWTClaims{
		UserUUID: "u-1", AMR: []string{"pwd", "otp"},
		LastOTPAt: time.Now().Add(-10 * time.Minute).Unix(),
	}
	rec := runEnrolmentGate(t, claims, enrolmentLookupFactor(true), 5*time.Minute)
	assertCodedError(t, rec, http.StatusUnauthorized, "step_up_required")
}

func TestRequireEnrolmentProof_WithFactorNoProofIsStepUp(t *testing.T) {
	claims := &models.JWTClaims{UserUUID: "u-1", AMR: []string{"pwd"}}
	rec := runEnrolmentGate(t, claims, enrolmentLookupFactor(true), 5*time.Minute)
	assertCodedError(t, rec, http.StatusUnauthorized, "step_up_required")
}

// A user WITHOUT a factor proves presence with a recent interactive
// login. This is the branch that lets an MFA-obligated account in its
// grace window enrol at all — password-confirm refuses those (D19).
func TestRequireEnrolmentProof_NoFactorFreshAuthTimePasses(t *testing.T) {
	claims := &models.JWTClaims{
		UserUUID: "u-1", AMR: []string{"pwd"}, AuthTime: time.Now().Add(-time.Minute).Unix(),
	}
	rec := runEnrolmentGate(t, claims, enrolmentLookupFactor(false), 5*time.Minute)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
}

// A fresh reauth (password reconfirm) is also a fresh proof, for the
// users that endpoint still serves.
func TestRequireEnrolmentProof_NoFactorFreshReauthPasses(t *testing.T) {
	claims := &models.JWTClaims{
		UserUUID: "u-1", AMR: []string{"pwd", "reauth"}, LastOTPAt: time.Now().Unix(),
	}
	rec := runEnrolmentGate(t, claims, enrolmentLookupFactor(false), 5*time.Minute)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
}

func TestRequireEnrolmentProof_NoFactorStaleAuthTimeIsReauth(t *testing.T) {
	authTime := time.Now().Add(-30 * time.Minute).Unix()
	claims := &models.JWTClaims{UserUUID: "u-1", AMR: []string{"pwd"}, AuthTime: authTime}
	rec := runEnrolmentGate(t, claims, enrolmentLookupFactor(false), 5*time.Minute)

	body := assertCodedError(t, rec, http.StatusUnauthorized, "reauthentication_required")
	if body["maxAgeSeconds"] != float64(300) {
		t.Errorf("maxAgeSeconds = %v, want 300", body["maxAgeSeconds"])
	}
	if body["authTime"] != float64(authTime) {
		t.Errorf("authTime = %v, want %d", body["authTime"], authTime)
	}
	if got := rec.Header().Get("WWW-Authenticate"); got != `Bearer error="reauthentication_required"` {
		t.Errorf("WWW-Authenticate = %q", got)
	}
}

// A token minted before this shipped has no auth_time. It reads as
// stale and costs one re-login — safe by construction (edge case 9).
func TestRequireEnrolmentProof_PreDeployTokenIsReauth(t *testing.T) {
	claims := &models.JWTClaims{UserUUID: "u-1", AMR: []string{"pwd"}}
	rec := runEnrolmentGate(t, claims, enrolmentLookupFactor(false), 5*time.Minute)
	assertCodedError(t, rec, http.StatusUnauthorized, "reauthentication_required")
}

// FAIL CLOSED. A degraded Mongo must not let a factor be added without
// proof, so an unavailable lookup answers step_up_required for
// everyone: a user with a factor can satisfy it, a user without one
// cannot enrol until it recovers.
func TestRequireEnrolmentProof_LookupErrorFailsClosed(t *testing.T) {
	claims := &models.JWTClaims{
		UserUUID: "u-1", AMR: []string{"pwd"}, AuthTime: time.Now().Unix(),
	}
	rec := runEnrolmentGate(t, claims, enrolmentLookupErr(errors.New("mongo down")), 5*time.Minute)
	assertCodedError(t, rec, http.StatusUnauthorized, "step_up_required")
}

func TestRequireEnrolmentProof_NilLookupFailsClosed(t *testing.T) {
	claims := &models.JWTClaims{
		UserUUID: "u-1", AMR: []string{"pwd"}, AuthTime: time.Now().Unix(),
	}
	rec := runEnrolmentGate(t, claims, nil, 5*time.Minute)
	assertCodedError(t, rec, http.StatusUnauthorized, "step_up_required")
}
```

> `runEnrolmentGate(t, claims, lookup, maxAge) *httptest.ResponseRecorder`
> builds an `AuthMiddleware`, calls `SetMFAEnrollmentLookup`, wraps a
> 200 handler in `RequireEnrolmentProof(maxAge)` and serves a request
> whose context carries `claims`. `assertCodedError(t, rec, status, code)
> map[string]any` decodes the envelope and asserts status + `code`.
> Both belong next to the existing step-up helpers in this file.

- [ ] **Step 2: Run it to verify it fails**

Run: `go test ./internal/shared/middleware/ -run RequireEnrolmentProof -count=1`
Expected: FAIL — `m.RequireEnrolmentProof undefined`.

- [ ] **Step 3: Add the step-up predicate**

In `backend/internal/shared/middleware/auth.go`, next to `amrSatisfiesMFA`:

```go
// amrSatisfiesStepUp is amrSatisfiesMFA PLUS "reauth". Used by
// RequireStepUp and RequireEnrolmentProof only.
//
// The split exists because "reauth" — a fresh password reconfirm — is a
// presence proof but NOT a second factor. Letting it satisfy RequireMFA
// (which it does today) means a session-long gate meant to demand a
// second factor accepts a password the caller already typed once, which
// is M-1. RequireStepUp is different: it asks "did you prove presence in
// the last five minutes", and a reconfirm answers that.
func amrSatisfiesStepUp(amr []string) bool {
	for _, v := range amr {
		if v == "otp" || v == "webauthn" || v == "mfa" || v == "reauth" {
			return true
		}
	}
	return false
}
```

- [ ] **Step 4: Add the gate**

```go
// RequireEnrolmentProof gates the four enrolment endpoints — TOTP
// enroll begin/confirm and passkey register begin/finish — on a fresh
// proof of presence.
//
// H-2 and H-3: these were mounted under RequireGlobal() alone, so a
// stolen session-only bearer could register a passkey on the victim's
// account, or REPLACE their TOTP secret (ConfirmEnrollment deletes the
// existing factor after validating a code for the NEW one), and then
// own the account outright.
//
// Two shapes of proof, because the two populations have different ones
// available:
//
//   - a user WITH a factor proves presence exactly as RequireStepUp
//     demands: a fresh second factor. There is one right answer for
//     them, so this branch never emits password_confirm_required (they
//     have something stronger) or mfa_enrollment_required (they are
//     enrolled).
//   - a user WITHOUT a factor proves it with a recent interactive
//     login (auth_time within maxAge) or a fresh reauth. The answer
//     when they cannot is reauthentication_required, NOT
//     password_confirm_required: the users most in need of a first
//     enrolment are MFA-obligated accounts inside their grace window,
//     whom the reconfirm endpoint refuses (D19), and an OAuth-only
//     account has no password to reconfirm. A re-login is the one
//     answer that works for everyone, and both SPAs return the user to
//     where they were.
//
// The lookup fails CLOSED: nil or erroring → step_up_required. A
// degraded Mongo must never be the reason a factor can be added
// without proof.
//
// Residual: a bearer stolen inside the maxAge window after the victim's
// own login can still enrol — the window every freshness proof has,
// including a stolen reauth token's. D13's email and audit row make it
// visible and the admin reset (D15/D16) recovers.
func (m *AuthMiddleware) RequireEnrolmentProof(maxAge time.Duration) func(http.Handler) http.Handler {
	if maxAge <= 0 {
		maxAge = 5 * time.Minute
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			claims, ok := r.Context().Value(ctxClaims).(*models.JWTClaims)
			if !ok || claims == nil {
				m.sendErrorResponse(w, r, errors.AuthenticationError("authentication required").
					WithOperation("require_enrolment_proof").Build())
				return
			}

			if m.mfaEnrollment == nil {
				m.sendStepUpRequired(w, r, maxAge)
				return
			}
			hasFactor, err := m.mfaEnrollment(r.Context(), claims.Audience, claims.UserUUID)
			if err != nil {
				m.sendStepUpRequired(w, r, maxAge)
				return
			}

			if m.hasFreshSecondFactor(r, claims, maxAge) {
				next.ServeHTTP(w, r)
				return
			}
			if hasFactor {
				// One right answer for an enrolled user.
				m.sendStepUpRequired(w, r, maxAge)
				return
			}
			if claims.AuthTime > 0 && time.Since(time.Unix(claims.AuthTime, 0)) <= maxAge {
				next.ServeHTTP(w, r)
				return
			}
			m.sendReauthenticationRequired(w, r, maxAge, claims.AuthTime)
		})
	}
}

// hasFreshSecondFactor is RequireStepUp's predicate, extracted so the
// two gates cannot drift. Task 6 makes it epoch-aware.
func (m *AuthMiddleware) hasFreshSecondFactor(r *http.Request, claims *models.JWTClaims, maxAge time.Duration) bool {
	return amrSatisfiesStepUp(claims.AMR) && claims.LastOTPAt > 0 &&
		time.Since(time.Unix(claims.LastOTPAt, 0)) <= maxAge
}

// sendReauthenticationRequired emits the 401 that tells a client "sign
// in again, then come back". It carries authTime so the SPA can explain
// how stale the session is, and maxAgeSeconds so it knows the bar.
func (m *AuthMiddleware) sendReauthenticationRequired(w http.ResponseWriter, _ *http.Request, maxAge time.Duration, authTime int64) {
	writeCodedError(w, codedError{
		status: http.StatusUnauthorized,
		code:   "reauthentication_required",
		title:  "reauthentication required",
		detail: "adding a second factor requires a recent sign-in; please sign in again and retry",
		scheme: schemeBearer,
		item:   &codedErrorItem{message: "reauthentication required", location: "require_enrolment_proof", value: "REAUTHENTICATION_REQUIRED"},
		extra: map[string]any{
			"maxAgeSeconds": int(maxAge.Seconds()),
			"authTime":      authTime,
		},
	})
}
```

Point `RequireStepUp` at the extracted predicate so the two cannot drift:

```go
			if m.hasFreshSecondFactor(r, claims, maxAge) {
				next.ServeHTTP(w, r)
				return
			}
```

- [ ] **Step 5: Pin the new envelope in the golden test**

Run `go test ./internal/shared/middleware/ -run CodedErrorEnvelopes_Golden -count=1`, read the failure, and add the `reauthentication_required` literal to the golden table byte-for-byte. Do not relax the golden.

- [ ] **Step 6: Split the enrolment routes out of the protected group**

In `backend/internal/core/auth/handlers/mfa_handler.go`, move `mfa-enroll-begin` and `mfa-enroll-confirm` out of `RegisterProtectedRoutes` into:

```go
// RegisterEnrolmentRoutes mounts the endpoints that CREATE or REPLACE a
// second factor. The caller wires RequireEnrolmentProof(5m) around this
// API instance — see auth/module.go. Both halves of the ceremony are
// gated, not just the first: the factor set can change between begin
// and confirm.
func (h *MFAHandler) RegisterEnrolmentRoutes(api huma.API, mount RouteMount) {
	huma.Register(api, huma.Operation{
		OperationID: mount.OpIDPrefix + "mfa-enroll-begin",
		Method:      http.MethodPost,
		Path:        "/v1/auth" + mount.PathPrefix + "/mfa/enroll/begin",
		Summary:     "Begin MFA (TOTP) enrollment",
		Tags:        []string{"Authentication", "MFA"},
		Security:    []map[string][]string{{"bearerAuth": {}}},
	}, h.EnrollBegin)

	huma.Register(api, huma.Operation{
		OperationID: mount.OpIDPrefix + "mfa-enroll-confirm",
		Method:      http.MethodPost,
		Path:        "/v1/auth" + mount.PathPrefix + "/mfa/enroll/confirm",
		Summary:     "Confirm MFA enrollment and receive backup codes",
		Tags:        []string{"Authentication", "MFA"},
		Security:    []map[string][]string{{"bearerAuth": {}}},
	}, h.EnrollConfirm)
}
```

`mfa-status` (`/me/mfa`) and `mfa-verify` **stay** in `RegisterProtectedRoutes`.

Do the same in `webauthn_handler.go` for `register/begin` and `register/finish`; `/me/mfa/webauthn/credentials` and `webauthn/verify/*` stay.

- [ ] **Step 7: Mount them under the gate**

In `backend/internal/core/auth/module.go`, after each existing protected group, add a gated one — operator (`:1615-1619` neighbourhood):

```go
	// Enrolment CREATES or REPLACES a credential, so it demands a fresh
	// proof of presence, not merely a valid bearer (H-2, H-3).
	ri.Operator.ProtectedRouter.Group(func(r chi.Router) {
		r.Use(ri.Operator.AuthMW.RequireGlobal())
		r.Use(ri.Operator.AuthMW.RequireEnrolmentProof(5 * time.Minute))
		api := humachi.New(r, ri.APIConfig)
		m.operatorMFAHandler.RegisterEnrolmentRoutes(api, handlers.OperatorMount)
	})
```

and the same for `m.operatorWebAuthnHandler` (`:1693-1697`), `m.clientMFAHandler` (`:1742-1746`) and `m.clientWebAuthnHandler` (`:1755-1759`), using the client router and mount.

Also fix the stale comment block at `:1605-1613`, which says "protected (no step-up): enroll / status / verify".

- [ ] **Step 8: Run everything and regenerate OpenAPI**

Run:
```
go vet ./... && go test ./internal/shared/middleware/... ./internal/core/auth/... -count=1
make -C /home/tore/orkestra openapi-dump
```
Expected: PASS. The route paths are unchanged, so the dump should differ only if a security scheme annotation changed — commit whatever it produces.

- [ ] **Step 9: Document the gate**

`backend/internal/core/auth/CLAUDE.md` `:832-833`, `:839-840` describe the enrolment gates and are now wrong. Rewrite them and add a section covering: the two proof shapes, the fail-closed lookup, why the no-factor branch answers `reauthentication_required` rather than `password_confirm_required`, and the residual window.

- [ ] **Step 10: Commit**

```bash
git add backend/internal/shared/middleware backend/internal/core/auth backend/openapi
git commit -m "$(cat <<'EOF'
fix(auth): demand a fresh proof for every MFA enrolment (H-2, H-3)

TOTP enrolment and passkey registration were mounted under
RequireGlobal() alone, so a stolen session-only bearer could register a
passkey on the victim's account — or REPLACE their TOTP secret, since
ConfirmEnrollment deletes the existing factor after validating a code
for the NEW one — and then own the account outright.

All four endpoints move behind RequireEnrolmentProof(5m). A user with a
factor proves presence with a fresh second factor and gets exactly one
answer when they cannot: step_up_required. A user without one proves it
with a recent interactive login (the new auth_time claim) or a fresh
reauth, and gets a new 401 reauthentication_required otherwise — not
password_confirm_required, because the users most in need of a first
enrolment are MFA-obligated accounts in their grace window, whom the
reconfirm endpoint refuses, and an OAuth-only account has no password.

The enrolment lookup fails closed: a degraded Mongo must never be the
reason a factor can be added without proof. Both halves of each
ceremony are gated, because the factor set can change between them.

Spec §4.2 D11, D12.

Claude-Session: https://claude.ai/code/session_0155Lt3QwR9zX15oGNKrn1T1
EOF
)"
```

---

## Task 4: Removal removes every factor (D15)

`RemoveFactor` deletes the TOTP row and nothing else, so an admin reset 404s for a passkey-only target and a "remove MFA" leaves the passkeys standing.

**Files:**
- Modify: `backend/internal/core/auth/services/mfa_service.go` (`RemoveFactor` `:333-367`)
- Test: `backend/internal/core/auth/services/mfa_service_test.go`

**Interfaces:**
- Consumes: `repository.MFAFactorRepository.FindByUserAndType`, `.Delete`.
- Produces: `RemoveFactor` now returns `ErrMFANotEnrolled` only when **neither** row exists.

- [ ] **Step 1: Write the failing tests**

Append to `backend/internal/core/auth/services/mfa_service_test.go`:

```go
// "Remove MFA" that leaves the passkeys standing is not a removal. The
// admin reset path depends on this too: it 404s today for a passkey-only
// target, so an operator cannot recover such an account at all.
func TestRemoveFactor_DeletesBothRows(t *testing.T) {
	svc, repo := newMFAServiceForTest(t)
	ctx := context.Background()
	repo.seedTOTP(t, "u-1")
	repo.seedWebAuthn(t, "u-1", 2 /* credentials */)

	if err := svc.RemoveFactor(ctx, "u-1", "u-1"); err != nil {
		t.Fatalf("RemoveFactor: %v", err)
	}
	if repo.hasTOTP("u-1") {
		t.Error("the TOTP row must be gone")
	}
	if repo.hasWebAuthn("u-1") {
		t.Error("the WebAuthn row must be gone too")
	}
}

func TestRemoveFactor_PasskeyOnlyUserSucceeds(t *testing.T) {
	svc, repo := newMFAServiceForTest(t)
	repo.seedWebAuthn(t, "u-2", 1)

	if err := svc.RemoveFactor(context.Background(), "u-2", "admin-1"); err != nil {
		t.Fatalf("RemoveFactor for a passkey-only user = %v, want nil", err)
	}
	if repo.hasWebAuthn("u-2") {
		t.Error("the WebAuthn row must be gone")
	}
}

func TestRemoveFactor_TOTPOnlyUserStillSucceeds(t *testing.T) {
	svc, repo := newMFAServiceForTest(t)
	repo.seedTOTP(t, "u-3")
	if err := svc.RemoveFactor(context.Background(), "u-3", "u-3"); err != nil {
		t.Fatalf("RemoveFactor: %v", err)
	}
}

func TestRemoveFactor_NoFactorsAtAllIsNotEnrolled(t *testing.T) {
	svc, _ := newMFAServiceForTest(t)
	if err := svc.RemoveFactor(context.Background(), "u-4", "u-4"); !errors.Is(err, ErrMFANotEnrolled) {
		t.Fatalf("err = %v, want ErrMFANotEnrolled only when NEITHER row exists", err)
	}
}

// A WebAuthn row with zero credentials is not a factor — the enrolment
// lookup already treats it that way, and the two must agree.
func TestRemoveFactor_EmptyWebAuthnRowIsNotAFactor(t *testing.T) {
	svc, repo := newMFAServiceForTest(t)
	repo.seedWebAuthn(t, "u-5", 0)
	if err := svc.RemoveFactor(context.Background(), "u-5", "u-5"); !errors.Is(err, ErrMFANotEnrolled) {
		t.Fatalf("err = %v, want ErrMFANotEnrolled", err)
	}
}

// A partial failure must not report success: if one row deletes and the
// other errors, the caller has to know the account is half-reset.
func TestRemoveFactor_PartialDeletionIsAnError(t *testing.T) {
	svc, repo := newMFAServiceForTest(t)
	repo.seedTOTP(t, "u-6")
	repo.seedWebAuthn(t, "u-6", 1)
	repo.failDeleteFor(models.MFAFactorWebAuthn)

	if err := svc.RemoveFactor(context.Background(), "u-6", "u-6"); err == nil {
		t.Fatal("a failed deletion of one row must surface as an error")
	}
}

// Device trust is revoked exactly once regardless of how many rows were
// deleted, with the reason the actor implies.
func TestRemoveFactor_RevokesDeviceTrustOnce(t *testing.T) {
	svc, repo := newMFAServiceForTest(t)
	trust := repo.deviceTrust
	repo.seedTOTP(t, "u-7")
	repo.seedWebAuthn(t, "u-7", 1)

	if err := svc.RemoveFactor(context.Background(), "u-7", "admin-9"); err != nil {
		t.Fatalf("RemoveFactor: %v", err)
	}
	if trust.revokeCalls() != 1 {
		t.Fatalf("device trust revoked %d times, want 1", trust.revokeCalls())
	}
	if trust.lastReason() != models.DeviceTrustRevokedOnAdminReset {
		t.Fatalf("reason = %q, want the admin-reset reason", trust.lastReason())
	}
}
```

> `newMFAServiceForTest` extends the existing fixture in this file
> with `seedTOTP` / `seedWebAuthn(n)` / `hasTOTP` / `hasWebAuthn` /
> `failDeleteFor` and a recording device-trust fake.

- [ ] **Step 2: Run it to verify it fails**

Run: `go test ./internal/core/auth/services/ -run RemoveFactor -count=1`
Expected: FAIL — the WebAuthn row survives, and a passkey-only user gets `ErrMFANotEnrolled`.

- [ ] **Step 3: Rewrite `RemoveFactor`**

```go
// RemoveFactor deletes EVERY MFA credential the user holds — the TOTP
// row and the WebAuthn row — and returns ErrMFANotEnrolled only when
// neither exists.
//
// It used to delete the TOTP row alone, which made "remove MFA" a lie
// for anyone with a passkey and made the admin reset answer 404 for a
// passkey-only target: an operator could not recover such an account at
// all. A WebAuthn row with no credentials is not a factor, matching
// what MFAEnrollmentLookup already reports.
func (s *mfaService) RemoveFactor(ctx context.Context, userUUID, actorUUID string) error {
	totp, err := s.factors.FindByUserAndType(ctx, userUUID, models.MFAFactorTOTP)
	if err != nil && !errors.Is(err, repository.ErrMFAFactorNotFound) {
		return err
	}
	wa, err := s.factors.FindByUserAndType(ctx, userUUID, models.MFAFactorWebAuthn)
	if err != nil && !errors.Is(err, repository.ErrMFAFactorNotFound) {
		return err
	}
	hasWebAuthn := wa != nil && len(wa.WebAuthnCredentials) > 0
	if totp == nil && !hasWebAuthn {
		return ErrMFANotEnrolled
	}

	// Both deletions are attempted, and a failure of either is returned:
	// reporting success on a half-reset account would tell an operator
	// the target is recoverable when it is not.
	if totp != nil {
		if err := s.factors.Delete(ctx, totp.UUID); err != nil {
			return err
		}
	}
	if hasWebAuthn {
		if err := s.factors.Delete(ctx, wa.UUID); err != nil {
			return err
		}
	}

	// Section C item #3: a trust row carries an amr annotation claiming
	// a factor was verified. Once no factor remains, that annotation is
	// a lie. Revoked once, with the reason the actor implies.
	if s.deviceTrust != nil {
		reason := models.DeviceTrustRevokedOnMFARemove
		if actorUUID != "" && actorUUID != userUUID {
			reason = models.DeviceTrustRevokedOnAdminReset
		}
		if err := s.deviceTrust.RevokeAllByUser(ctx, userUUID, reason); err != nil {
			s.logger.Warn("device_trust: revoke on factor removal failed",
				slog.String("user_uuid", userUUID),
				slog.String("error", err.Error()))
		}
	}

	s.logger.Info("mfa factors removed",
		slog.String("userUUID", userUUID),
		slog.String("actorUUID", actorUUID),
		slog.Bool("totp", totp != nil),
		slog.Bool("webauthn", hasWebAuthn),
	)
	return nil
}
```

- [ ] **Step 4: Run and check the handler assumptions**

Run: `go test ./internal/core/auth/... -count=1`
Expected: PASS. `AdminReset` (`mfa_handler.go:485-522`) no longer needs its passkey-only 404 carve-out — read it and remove any branch that exists only to explain that case.

- [ ] **Step 5: Commit**

```bash
go vet ./... && go test ./internal/core/auth/... -count=1
git add backend/internal/core/auth
git commit -m "$(cat <<'EOF'
fix(auth): make RemoveFactor remove every factor, not just TOTP

RemoveFactor deleted the TOTP row alone, so "remove MFA" left the user's
passkeys standing and the admin reset answered 404 for a passkey-only
target — an operator could not recover such an account at all.

It now deletes both rows, returns ErrMFANotEnrolled only when neither
exists, treats a WebAuthn row with no credentials as no factor (matching
MFAEnrollmentLookup), and surfaces a partial deletion as an error rather
than reporting a half-reset account as recovered.

Spec §4.3 D15.

Claude-Session: https://claude.ai/code/session_0155Lt3QwR9zX15oGNKrn1T1
EOF
)"
```

---

## Task 5: One rule for every credential change (D13, D16 second half)

Bump the epoch, revoke device trust, end every other session, announce it. TOTP removal, TOTP replacement, any passkey removal, admin reset — the same rule, differing only in who the caller is.

**Files:**
- Modify: `backend/internal/core/auth/services/mfa_service.go` (`RemoveFactor`, `ConfirmEnrollment` `:224-229`)
- Modify: `backend/internal/core/auth/services/webauthn_service.go` (`RemoveCredential`, `FinishRegistration`, `SetAuditSink`)
- Modify: `backend/internal/core/auth/handlers/mfa_handler.go` (`sessions` dependency, `AdminReset`, self `Remove`)
- Modify: `backend/internal/core/auth/handlers/webauthn_handler.go` (credential DELETE)
- Modify: `backend/internal/core/auth/module.go` (resolve `MFAEpochBumper`, wire `sessions`)
- Modify: `backend/internal/core/auth/models/device_trust.go` (+ `DeviceTrustRevokedOnMFAReplace`)
- Create: notification template `auth.mfa_factor_added` (EN + IT)
- Test: `backend/internal/core/auth/handlers/mfa_handler_reset_test.go` (new), `mfa_service_test.go`

**Interfaces:**
- Consumes: `iface.MFAEpochBumper` (Task 1), `iface.SessionTerminator` (`interfaces.go:955`), `authService.RevokeAllUserSessionsExcept` (`auth_service.go:1223`), `TerminateAllSessionsByUUID` (`:1158`), `SecurityEventSink` (`mfa_service.go:105-107`), `MailDispatcher` (PR A Task 6).
- Produces:
  - `models.DeviceTrustRevokedOnMFAReplace = "mfa_factor_replaced"`
  - `mfaService.SetEpochBumper(iface.MFAEpochBumper)`, same on `webAuthnService`
  - `MFAHandler.sessions iface.SessionTerminator` (per tier)
  - security event types `self_mfa_enrolled`, `self_mfa_factor_replaced`, `self_passkey_registered`, `self_passkey_removed`, `self_mfa_removed`
  - notification template id `auth.mfa_factor_added`

- [ ] **Step 1: Write the failing tests**

Create `backend/internal/core/auth/handlers/mfa_handler_reset_test.go`:

```go
package handlers

// M-2: removing or resetting a factor left every session MFA-satisfied.
// The epoch closes the caller's CURRENT token; revocation closes the
// others. These tests assert both, on all four paths.

import (
	"context"
	"testing"
)

func TestAdminReset_BumpsEpochAndTerminatesEverySession(t *testing.T) {
	h, deps := newMFAHandlerForTest(t)
	deps.factors.seedTOTP(t, "target-1")

	if err := callAdminReset(t, h, "admin-1", "target-1"); err != nil {
		t.Fatalf("AdminReset: %v", err)
	}
	if deps.epoch.bumpsFor("target-1") != 1 {
		t.Fatalf("epoch bumped %d times, want 1", deps.epoch.bumpsFor("target-1"))
	}
	if deps.sessions.terminatedAll("target-1") != 1 {
		t.Fatal("the admin path must terminate EVERY session, including the target's current one")
	}
}

// A failed termination must not fail the reset: the epoch has already
// ended MFA authority everywhere, so what is left is ordinary session
// access — the same exposure as any degraded revocation.
func TestAdminReset_TerminationFailureIsRecordedNotFatal(t *testing.T) {
	h, deps := newMFAHandlerForTest(t)
	deps.factors.seedTOTP(t, "target-2")
	deps.sessions.failNext()

	if err := callAdminReset(t, h, "admin-1", "target-2"); err != nil {
		t.Fatalf("AdminReset must still succeed: %v", err)
	}
	if deps.epoch.bumpsFor("target-2") != 1 {
		t.Fatal("the epoch bump must happen regardless")
	}
	if !deps.audit.metadataFalse("target-2", "sessions_terminated") {
		t.Fatal("the audit row must record sessions_terminated: false")
	}
}

// The self path spares the caller's own session — they are signed in
// and just proved themselves — but its MFA authority is gone with the
// next gated request, via the epoch.
func TestSelfRemove_RevokesEveryOtherSessionAndBumpsEpoch(t *testing.T) {
	h, deps := newMFAHandlerForTest(t)
	deps.factors.seedTOTP(t, "u-1")

	if err := callSelfRemove(t, h, "u-1", "sid-current"); err != nil {
		t.Fatalf("Remove: %v", err)
	}
	if deps.epoch.bumpsFor("u-1") != 1 {
		t.Fatal("self removal must bump the epoch")
	}
	if got := deps.sessions.revokedExcept("u-1"); got != "sid-current" {
		t.Fatalf("revokedExcept = %q, want the caller's own sid", got)
	}
}

// v1.10: EVERY passkey removal, not only the last one. A removed
// credential is one the user no longer trusts — a lost or compromised
// device — and it may have CREATED sessions through the passkey login
// flow. Neither the session document nor amr records which credential
// minted a session, so the rule cannot be narrower.
func TestRemoveCredential_RevokesOtherSessionsEvenWhenFactorsRemain(t *testing.T) {
	h, deps := newWebAuthnHandlerForTest(t)
	deps.factors.seedTOTP(t, "u-2")
	deps.factors.seedWebAuthn(t, "u-2", 2)

	if err := callRemoveCredential(t, h, "u-2", "cred-1", "sid-current"); err != nil {
		t.Fatalf("RemoveCredential: %v", err)
	}
	if deps.epoch.bumpsFor("u-2") != 1 {
		t.Fatal("removing one passkey must bump the epoch even when a TOTP factor and a second passkey remain")
	}
	if got := deps.sessions.revokedExcept("u-2"); got != "sid-current" {
		t.Fatalf("revokedExcept = %q, want the caller's own sid", got)
	}
}

// A TOTP replacement is a removal of the old secret, so it carries the
// same consequences.
func TestConfirmEnrollment_ReplacementBumpsEpochAndRevokes(t *testing.T) {
	svc, deps := newMFAServiceWithDeps(t)
	deps.factors.seedTOTP(t, "u-3")

	if _, err := confirmEnrolment(t, svc, "u-3"); err != nil {
		t.Fatalf("ConfirmEnrollment: %v", err)
	}
	if deps.epoch.bumpsFor("u-3") != 1 {
		t.Fatal("replacing a TOTP factor must bump the epoch")
	}
	if deps.trust.lastReason() != models.DeviceTrustRevokedOnMFAReplace {
		t.Fatalf("device-trust reason = %q, want mfa_factor_replaced", deps.trust.lastReason())
	}
	if !deps.events.saw("self_mfa_factor_replaced") {
		t.Fatal("a replacement must emit self_mfa_factor_replaced")
	}
}

// A FIRST enrolment adds authority; it must not invalidate authority
// proven by a factor that still exists.
func TestConfirmEnrollment_FirstEnrolmentDoesNotBumpEpoch(t *testing.T) {
	svc, deps := newMFAServiceWithDeps(t)
	if _, err := confirmEnrolment(t, svc, "u-4"); err != nil {
		t.Fatalf("ConfirmEnrollment: %v", err)
	}
	if deps.epoch.bumpsFor("u-4") != 0 {
		t.Fatal("a first enrolment must NOT bump the epoch")
	}
	if !deps.events.saw("self_mfa_enrolled") {
		t.Fatal("a first enrolment must emit self_mfa_enrolled")
	}
	if !deps.mail.enqueuedTemplate("auth.mfa_factor_added") {
		t.Fatal("a first enrolment must email the user")
	}
}

func TestFinishRegistration_AddingAPasskeyDoesNotBumpEpoch(t *testing.T) {
	svc, deps := newWebAuthnServiceWithDeps(t)
	if err := finishRegistration(t, svc, "u-5"); err != nil {
		t.Fatalf("FinishRegistration: %v", err)
	}
	if deps.epoch.bumpsFor("u-5") != 0 {
		t.Fatal("adding a passkey must NOT bump the epoch")
	}
	if !deps.events.saw("self_passkey_registered") {
		t.Fatal("a passkey registration must emit self_passkey_registered")
	}
	if !deps.mail.enqueuedTemplate("auth.mfa_factor_added") {
		t.Fatal("a passkey registration must email the user")
	}
}

// Every credential change leaves a security event. Today only
// backup-code regeneration emits anything at all.
func TestCredentialChanges_EmitSecurityEvents(t *testing.T) {
	for _, want := range []string{
		"self_mfa_enrolled", "self_mfa_factor_replaced",
		"self_passkey_registered", "self_passkey_removed", "self_mfa_removed",
	} {
		t.Run(want, func(t *testing.T) {
			if !securityEventTypeIsWired(t, want) {
				t.Fatalf("%s is not emitted by any path", want)
			}
		})
	}
}

// The seam being absent must degrade, not fail: a fork's user provider
// may predate it.
func TestEpochBumper_AbsentSeamDegradesWithWarn(t *testing.T) {
	svc, deps := newMFAServiceWithDeps(t)
	deps.epoch = nil
	deps.factors.seedTOTP(t, "u-6")

	if err := svc.RemoveFactor(context.Background(), "u-6", "u-6"); err != nil {
		t.Fatalf("RemoveFactor with no epoch bumper = %v, want nil", err)
	}
}
```

- [ ] **Step 2: Run it to verify it fails**

Run: `go test ./internal/core/auth/... -run 'AdminReset|SelfRemove|RemoveCredential|ConfirmEnrollment_|FinishRegistration_|CredentialChanges|EpochBumper' -count=1`
Expected: FAIL — no epoch bumper, no `sessions` on the handler, no security events, no template.

- [ ] **Step 3: Add the device-trust reason**

In `backend/internal/core/auth/models/device_trust.go`, next to the other reasons:

```go
// DeviceTrustRevokedOnMFAReplace marks trust rows dropped because the
// user replaced their TOTP secret. A replacement is a removal of the old
// factor, so the trust rows annotated with it are annotated with a lie.
const DeviceTrustRevokedOnMFAReplace = "mfa_factor_replaced"
```

- [ ] **Step 4: Add the epoch seam to both services**

In `mfa_service.go` and `webauthn_service.go`:

```go
// SetEpochBumper wires the MFA epoch counter. Optional: a fork's user
// provider may predate the seam, in which case the epoch never moves
// and the platform degrades to session revocation alone — which is what
// it had before. Never fatal.
func (s *mfaService) SetEpochBumper(b iface.MFAEpochBumper) { s.epoch = b }

// bumpEpoch is the single place either service moves the counter. It is
// called by every REMOVAL or REPLACEMENT and by no addition: authority
// proven by a factor that still exists stays valid.
func (s *mfaService) bumpEpoch(ctx context.Context, userUUID string) {
	if s.epoch == nil {
		s.logger.Warn("mfa: epoch bumper not wired; a removed factor's authority will survive until the token expires",
			slog.String("user_uuid", userUUID))
		return
	}
	if _, err := s.epoch.BumpMFAEpoch(ctx, userUUID); err != nil {
		s.logger.Error("mfa: failed to bump the MFA epoch",
			slog.String("user_uuid", userUUID),
			slog.String("error", err.Error()))
	}
}
```

Call `s.bumpEpoch(ctx, userUUID)` from: `RemoveFactor` (after the deletions), `ConfirmEnrollment` **only** on the replacement branch, and `webAuthnService.RemoveCredential` (always, whatever remains).

Also give `webAuthnService` the `SetAuditSink` seam the MFA service already has (`mfa_service.go:105-107`), and resolve `module.GetTyped[iface.MFAEpochBumper]` against each tier's user provider in `module.go`, calling `SetEpochBumper` on both services per tier.

- [ ] **Step 5: Announce every factor change**

In `ConfirmEnrollment`, replace the silent replacement (`:224-229`):

```go
	// A replacement is a REMOVAL of the old secret, so it carries the
	// same consequences: the epoch moves, the device-trust rows whose
	// amr annotation named the old factor are revoked, and every other
	// session ends. The enrolment gate has already guaranteed a fresh
	// proof, so this is a deliberate act by the account holder.
	replaced := false
	if existing, err := s.factors.FindByUserAndType(ctx, userUUID, models.MFAFactorTOTP); err == nil && existing != nil {
		if err := s.factors.Delete(ctx, existing.UUID); err != nil {
			return nil, err
		}
		replaced = true
	}
```

and after the new factor is persisted:

```go
	eventType := "self_mfa_enrolled"
	if replaced {
		eventType = "self_mfa_factor_replaced"
		s.bumpEpoch(ctx, userUUID)
		if s.deviceTrust != nil {
			if err := s.deviceTrust.RevokeAllByUser(ctx, userUUID, models.DeviceTrustRevokedOnMFAReplace); err != nil {
				s.logger.Warn("device_trust: revoke on factor replacement failed",
					slog.String("user_uuid", userUUID), slog.String("error", err.Error()))
			}
		}
		s.revokeOtherSessions(ctx, userUUID)
	}
	s.emitSecurityEvent(ctx, userUUID, eventType)
	s.notifyFactorAdded(ctx, userUUID, "totp", replaced)
```

Add the event/notify helpers, and the parallel calls in `webAuthnService.FinishRegistration` (`self_passkey_registered`, `notifyFactorAdded(..., "passkey", false)`) and `RemoveCredential` (`self_passkey_removed`, no mail), plus `self_mfa_removed` in `RemoveFactor` on the self path.

The compliance-action mapping (`authEventComplianceAction`, `auth_service.go:2034-2054`) gains:

| Event type | Action |
|---|---|
| `self_mfa_enrolled` | `auth.mfa.enrolled` |
| `self_mfa_factor_replaced` | `auth.mfa.replaced` |
| `self_passkey_registered` | `auth.mfa.passkey_registered` |
| `self_passkey_removed` | `auth.mfa.passkey_removed` |
| `self_mfa_removed` | `auth.mfa.removed` |

- [ ] **Step 6: Add the notification template**

Create `auth.mfa_factor_added` (category `auth.security`) in EN and IT, seeded exactly as `auth.new_device_login` is. Data fields: `FactorType` (`totp` | `passkey`), `Replaced` (bool), `RequestIP`, `At`, plus the `AppName` / `SupportEmail` the sibling templates carry. Send it through PR A's `MailDispatcher`, never inline.

Run `go test ./cmd/server/ -run TemplateParity -count=1` and fix whatever it reports — that test is the gate on the EN/IT pair being complete.

- [ ] **Step 7: Give the MFA handler a session terminator**

`MFAHandler` gains a per-tier `sessions iface.SessionTerminator`: the operator handler gets the operator `*authService`, the client handler the client one — the same values the user module resolves (`interfaces.go:947-951`). Then:

- `AdminReset` → after `RemoveFactor`, `TerminateAllSessionsByUUID(target)`; a failure is logged and recorded as `sessions_terminated: false` in the audit metadata, and the reset still succeeds.
- self `Remove` (`/me/mfa/remove`) → `RevokeAllUserSessionsExcept(user, currentSid)`.
- `DELETE /me/mfa/webauthn/credentials/{id}` → the same, on **every** removal.

- [ ] **Step 8: Run everything**

Run: `go vet ./... && go test ./internal/core/auth/... ./cmd/server/... -count=1`
Expected: PASS.

- [ ] **Step 9: Document and commit**

Update `backend/internal/core/auth/CLAUDE.md` `:837-838` (reset semantics: sessions **and** passkeys) and add the epoch section.

```bash
git add backend/internal/core/auth backend/internal/core/notification
git commit -m "$(cat <<'EOF'
fix(auth): end what a removed factor authorised, everywhere, at once

An MFA reset or self-removal left every session MFA-satisfied: the
markers lived in the token until it expired, and the caller's own token
kept passing RequireMFA for its whole lifetime and RequireStepUp for
five minutes.

One rule now covers every credential removal or replacement — TOTP
removal, TOTP replacement, any passkey removal, admin reset: bump the
MFA epoch, revoke device trust, and end every session but the caller's
own (all of them on the admin path). The epoch is what closes the
caller's CURRENT token, so the reset no longer depends on a revocation
write succeeding; a failed termination is recorded in the audit row and
the reset still stands.

Passkey removal is uniform rather than last-factor-only: a removed
credential is one the user no longer trusts, it may have created
sessions through the passkey login flow, and nothing records which
credential minted which session.

Every factor change now emits a security event and, for additions, an
auth.mfa_factor_added email — today only backup-code regeneration emits
anything at all, so a stolen-bearer enrolment was invisible.

Spec §4.2 D13, §4.3 D16. Closes M-2.

Claude-Session: https://claude.ai/code/session_0155Lt3QwR9zX15oGNKrn1T1
EOF
)"
```

---

## Task 6: The epoch is enforced, and refresh stops copying markers forward (D16 resolver, D17, D18)

The bumps from Task 5 mean nothing until something compares them. This task adds the single reader, makes the two predicates strict, and stops refresh laundering a dead marker into a fresh token.

**Files:**
- Modify: `backend/internal/shared/middleware/auth.go` (`mfaAuthority`, `amrSatisfiesMFA`, `RequireMFA`, `RequireStepUp`, `RequireEnrolmentProof`, `IsMFAEnrolled`, the impersonation bypass at `:559`)
- Modify: `backend/internal/core/auth/services/auth_service.go` (`RefreshTokensWithRiskAssessment` `:1574`, `MintAccessTokenFromRefresh` `:1780`)
- Modify: `backend/internal/core/auth/services/jwt_service.go` (the stale comment at `:491-492`)
- Test: `backend/internal/shared/middleware/step_up_test.go`, `backend/internal/core/auth/services/auth_service_refresh_test.go`

**Interfaces:**
- Consumes: `claims.MFAEpoch` (Task 2), `m.users iface.UserProvider` (already wired, `auth.go:194`).
- Produces:
  - `func (m *AuthMiddleware) mfaAuthority(r *http.Request, claims *models.JWTClaims) []string` — the markers that still count
  - `func carryAMR(prior []string, tokenEpoch, userEpoch int) (amr []string, lastOTPAt int64)`
  - `ctxMFAAuthority` context key (memoisation)

- [ ] **Step 1: Write the failing tests**

Append to `backend/internal/shared/middleware/step_up_test.go`:

```go
// The epoch is the whole point of D16: a token minted under a factor
// the user has since removed must lose its MFA authority IMMEDIATELY,
// in the session it was minted for, without a refresh.
func TestMFAEpoch_StaleTokenLosesEveryGate(t *testing.T) {
	claims := &models.JWTClaims{
		UserUUID: "u-1", AMR: []string{"pwd", "otp"},
		LastOTPAt: time.Now().Unix(), MFAEpoch: 1,
	}
	users := userFakeWithEpoch("u-1", 2) // the user has moved on

	t.Run("RequireMFA", func(t *testing.T) {
		rec := runMFAGate(t, claims, users)
		assertCodedError(t, rec, http.StatusUnauthorized, "mfa_required")
	})
	t.Run("RequireStepUp", func(t *testing.T) {
		rec := runStepUpGate(t, claims, users, 5*time.Minute)
		assertCodedError(t, rec, http.StatusUnauthorized, "step_up_required")
	})
	t.Run("RequireEnrolmentProof", func(t *testing.T) {
		rec := runEnrolmentGateWithUsers(t, claims, enrolmentLookupFactor(true), users, 5*time.Minute)
		assertCodedError(t, rec, http.StatusUnauthorized, "step_up_required")
	})
	t.Run("IsMFAEnrolled", func(t *testing.T) {
		ctx := ctxWithClaimsAndUsers(t, claims, users)
		if IsMFAEnrolled(ctx) {
			t.Fatal("Cedar's principal.mfa_enrolled must read false for a stale epoch")
		}
	})
}

func TestMFAEpoch_CurrentTokenPassesEveryGate(t *testing.T) {
	claims := &models.JWTClaims{
		UserUUID: "u-1", AMR: []string{"pwd", "otp"},
		LastOTPAt: time.Now().Unix(), MFAEpoch: 2,
	}
	users := userFakeWithEpoch("u-1", 2)
	if rec := runMFAGate(t, claims, users); rec.Code != http.StatusOK {
		t.Fatalf("RequireMFA = %d, want 200", rec.Code)
	}
	if rec := runStepUpGate(t, claims, users, 5*time.Minute); rec.Code != http.StatusOK {
		t.Fatalf("RequireStepUp = %d, want 200", rec.Code)
	}
}

// Edge case 12: every user document that predates the field reads as 0
// and matches every pre-deploy token, so the deploy downgrades nobody.
func TestMFAEpoch_ZeroOnBothSidesMatches(t *testing.T) {
	claims := &models.JWTClaims{
		UserUUID: "u-1", AMR: []string{"pwd", "otp"}, LastOTPAt: time.Now().Unix(),
	}
	if rec := runMFAGate(t, claims, userFakeWithEpoch("u-1", 0)); rec.Code != http.StatusOK {
		t.Fatalf("a pre-deploy token against a pre-deploy user = %d, want 200", rec.Code)
	}
}

// FAIL CLOSED on a lookup error: "not current" is the safe reading.
func TestMFAEpoch_LookupErrorReadsAsStale(t *testing.T) {
	claims := &models.JWTClaims{
		UserUUID: "u-1", AMR: []string{"pwd", "otp"},
		LastOTPAt: time.Now().Unix(), MFAEpoch: 1,
	}
	rec := runMFAGate(t, claims, userFakeErroring())
	assertCodedError(t, rec, http.StatusUnauthorized, "mfa_required")
}

// A token with NO MFA markers must cost no database read at all — this
// is the common request path.
func TestMFAEpoch_NoMarkersCostsNoLookup(t *testing.T) {
	claims := &models.JWTClaims{UserUUID: "u-1", AMR: []string{"pwd"}}
	users := countingUserFake("u-1", 0)
	_ = runStepUpGate(t, claims, users, 5*time.Minute)
	if users.lookups() != 0 {
		t.Fatalf("%d user lookups for a token with no MFA markers, want 0", users.lookups())
	}
}

// The resolver is memoised per request: five gates on one route must not
// mean five reads.
func TestMFAEpoch_ResolverIsMemoisedPerRequest(t *testing.T) {
	claims := &models.JWTClaims{
		UserUUID: "u-1", AMR: []string{"pwd", "otp"},
		LastOTPAt: time.Now().Unix(), MFAEpoch: 1,
	}
	users := countingUserFake("u-1", 1)
	runChainedGates(t, claims, users, 3 /* gates */)
	if users.lookups() != 1 {
		t.Fatalf("%d lookups across 3 chained gates, want 1", users.lookups())
	}
}

// M-1: "reauth" is a password reconfirm, not a second factor. It must
// satisfy RequireStepUp (a presence proof) and NOT RequireMFA (a
// second-factor gate).
func TestRequireMFA_RejectsAReauthOnlyToken(t *testing.T) {
	claims := &models.JWTClaims{UserUUID: "u-1", AMR: []string{"pwd", "reauth"}, LastOTPAt: time.Now().Unix()}
	rec := runMFAGate(t, claims, userFakeWithEpoch("u-1", 0))
	assertCodedError(t, rec, http.StatusUnauthorized, "mfa_required")
}

// …and the existing TestRequireStepUp_ReauthAMRSatisfiesGate must stay
// green, unchanged.

// The impersonation bypass consumes an MFA marker, so it reads through
// the resolver too.
func TestImpersonationBypass_DemandsMFAForAStaleEpoch(t *testing.T) {
	claims := &models.JWTClaims{
		UserUUID: "admin-1", AMR: []string{"pwd", "otp"},
		LastOTPAt: time.Now().Unix(), MFAEpoch: 1,
	}
	if impersonationBypassAllowed(t, claims, userFakeWithEpoch("admin-1", 4)) {
		t.Fatal("a stale epoch must not satisfy the impersonation bypass")
	}
}

// The handler-level proof: remove the factor with a stepped-up token,
// and the SAME token is refused on the very next step-up route.
func TestEpoch_SteppedUpTokenDiesOnTheNextRequestAfterRemoval(t *testing.T) {
	env := newLiveEpochEnv(t) // handler + real epoch bumper + real middleware
	token := env.mintSteppedUpToken(t, "u-1")

	if code := env.callStepUpRoute(t, token); code != http.StatusOK {
		t.Fatalf("precondition: step-up route = %d, want 200", code)
	}
	env.removeFactor(t, "u-1", token)
	if code := env.callStepUpRoute(t, token); code != http.StatusUnauthorized {
		t.Fatalf("the same token after removal = %d, want 401", code)
	}
}
```

And for refresh, append to `backend/internal/core/auth/services/auth_service_refresh_test.go` (create if absent):

```go
// M-2, second half: refresh copied amr and last_otp_at forward
// verbatim, so a session whose factor was removed kept minting
// MFA-satisfied tokens forever.
func TestCarryAMR_KeepsMarkersOnlyWhenTheEpochMatches(t *testing.T) {
	amr, lastOTP := carryAMR([]string{"pwd", "otp"}, 1700000000, 3, 3)
	if !contains(amr, "otp") {
		t.Fatal("a matching epoch must keep the otp marker")
	}
	if lastOTP == 0 {
		t.Fatal("LastOTPAt must be carried when a marker survives")
	}

	amr, lastOTP = carryAMR([]string{"pwd", "otp"}, 1700000000, 3, 4)
	if contains(amr, "otp") {
		t.Fatal("a stale epoch must drop the otp marker")
	}
	if !contains(amr, "pwd") {
		t.Fatal("base markers are kept regardless — they describe how the session began")
	}
	if lastOTP != 0 {
		t.Fatal("LastOTPAt must be zeroed when no MFA marker survives")
	}
}

// "reauth" is a five-minute proof, never a session property. It is
// ALWAYS dropped, whatever the epoch says.
func TestCarryAMR_AlwaysDropsReauth(t *testing.T) {
	amr, _ := carryAMR([]string{"pwd", "reauth"}, 1700000000, 0, 0)
	if contains(amr, "reauth") {
		t.Fatal("reauth must never survive a refresh")
	}
}

func TestCarryAMR_KeepsBaseMarkers(t *testing.T) {
	amr, _ := carryAMR([]string{"oauth"}, 1700000000, 1, 9)
	if !contains(amr, "oauth") {
		t.Fatal("oauth describes how the session began and always survives")
	}
}

func TestCarryAMR_DeviceTrustFollowsTheEpoch(t *testing.T) {
	amr, _ := carryAMR([]string{"pwd", "device_trust"}, 1700000000, 1, 2)
	if contains(amr, "device_trust") {
		t.Fatal("device_trust is an MFA marker and dies with a stale epoch")
	}
}

// The refreshed token must carry the CURRENT epoch, not the old one, or
// it would be stale the moment it was minted.
func TestRefresh_MintsUnderTheCurrentEpoch(t *testing.T) {
	svc, users := newRefreshTestService(t)
	users.setEpoch("u-1", 5)

	claims, err := refreshAndParse(t, svc, &models.JWTClaims{UserUUID: "u-1", MFAEpoch: 2, AMR: []string{"pwd", "otp"}})
	if err != nil {
		t.Fatalf("refresh: %v", err)
	}
	if claims.MFAEpoch != 5 {
		t.Fatalf("refreshed MFAEpoch = %d, want the user's current 5", claims.MFAEpoch)
	}
	if contains(claims.AMR, "otp") {
		t.Fatal("the otp marker was minted under epoch 2 and must not survive into epoch 5")
	}
}
```

- [ ] **Step 2: Run it to verify it fails**

Run: `go test ./internal/shared/middleware/ ./internal/core/auth/services/ -run 'MFAEpoch|CarryAMR|RequireMFA_Rejects|Impersonation|Refresh_Mints' -count=1`
Expected: FAIL — `carryAMR` undefined, no epoch comparison anywhere, `RequireMFA` accepts `reauth`.

- [ ] **Step 3: Make `amrSatisfiesMFA` strict**

```go
// amrSatisfiesMFA reports a genuine SECOND FACTOR. "reauth" is
// deliberately absent: a password reconfirm proves presence, not a
// second factor, and accepting it here let a session-long MFA gate be
// satisfied by a password the caller had already typed (M-1). The
// sidecar JWTValidator.RequireMFA has always used this strict list
// (jwt_validator.go:366-391); the drift closes here.
//
// Callers must read markers through AuthMiddleware.mfaAuthority, never
// from claims.AMR directly, so a stale MFA epoch reads as no marker.
func amrSatisfiesMFA(amr []string) bool {
	for _, v := range amr {
		if v == "otp" || v == "webauthn" || v == "mfa" {
			return true
		}
	}
	return false
}
```

- [ ] **Step 4: Add the resolver**

```go
// ctxMFAAuthority memoises mfaAuthority's answer for one request, so a
// route wearing several MFA-aware gates costs one user lookup.
type mfaAuthorityKey struct{}

// mfaAuthority returns the subset of claims.AMR that is still backed by
// a factor set the user actually holds.
//
// A token minted under MFA epoch N carries "mfae": N. When the user's
// current epoch has moved past it — they removed a factor, replaced one,
// or an admin reset them — every MFA marker on that token is authority
// derived from a credential that no longer exists, so it is dropped.
// The result is that a removal takes effect on the CALLER's current
// token, immediately, without waiting for a refresh and without
// depending on a revocation write succeeding.
//
// Three properties matter:
//   - a token with no MFA markers costs NO lookup (the common path);
//   - the answer is memoised per request;
//   - a lookup error reads as "not current" — fail closed.
func (m *AuthMiddleware) mfaAuthority(r *http.Request, claims *models.JWTClaims) []string {
	if !hasAnyMFAMarker(claims.AMR) {
		return claims.AMR
	}
	ctx := r.Context()
	if cached, ok := ctx.Value(mfaAuthorityKey{}).([]string); ok {
		return cached
	}

	current := true
	if m.users == nil {
		// No provider wired (tests, minimal setups): the epoch cannot be
		// checked, so behave as before the epoch existed.
		current = true
	} else {
		user, err := m.users.GetUserByID(ctx, claims.UserUUID)
		if err != nil || user == nil {
			current = false // fail closed
		} else {
			current = claims.MFAEpoch == user.MFAEpoch
		}
	}

	out := claims.AMR
	if !current {
		out = withoutMFAMarkers(claims.AMR)
	}
	*r = *r.WithContext(context.WithValue(ctx, mfaAuthorityKey{}, out))
	return out
}

func hasAnyMFAMarker(amr []string) bool {
	for _, v := range amr {
		if v == "otp" || v == "webauthn" || v == "mfa" || v == models.DeviceTrustAMR {
			return true
		}
	}
	return false
}

func withoutMFAMarkers(amr []string) []string {
	out := make([]string, 0, len(amr))
	for _, v := range amr {
		if v == "otp" || v == "webauthn" || v == "mfa" || v == models.DeviceTrustAMR {
			continue
		}
		out = append(out, v)
	}
	return out
}
```

> **Implementation note:** `*r = *r.WithContext(...)` mutates the request
> the handler chain holds. If the surrounding code style forbids that,
> thread the derived request into `next.ServeHTTP` instead — each gate
> already has the `next` handler in scope. Pick one and use it in all
> five call sites; do **not** mix them, or the memoisation silently
> stops working.

- [ ] **Step 5: Route the five consumers through it**

`RequireMFA`, `RequireStepUp`, `RequireEnrolmentProof`'s factor branch, the impersonation bypass (`auth.go:559`) and `IsMFAEnrolled` all read `m.mfaAuthority(r, claims)` instead of `claims.AMR`.

`IsMFAEnrolled` is a package function with only a context, so give the middleware a place to stash the resolved authority and have `IsMFAEnrolled` read that, falling back to `GetAMR` when nothing was stashed (unauthenticated routes, tests using `WithAMR`).

- [ ] **Step 6: Add `carryAMR` and use it on both refresh paths**

In `auth_service.go`:

```go
// carryAMR recomputes the authentication-method markers for a refreshed
// token instead of copying them forward.
//
// Refresh used to copy claims.AMR and claims.LastOTPAt verbatim, so a
// session whose factor had been removed kept minting MFA-satisfied
// tokens for as long as it lived — the second half of M-2.
//
//   - "reauth" is ALWAYS dropped: a password reconfirm is a five-minute
//     proof, never a session property.
//   - the MFA markers (otp, webauthn, mfa, device_trust) survive only
//     when the token's epoch still matches the user's. The refresh
//     already loads the user (auth_service.go:1530), so this costs no
//     extra read.
//   - LastOTPAt is carried only when at least one MFA marker survived;
//     a freshness stamp with nothing to be fresh about is misleading.
//   - the base markers (pwd, oauth) always survive: they describe how
//     the session began, which a refresh does not change.
func carryAMR(prior []string, priorLastOTPAt int64, tokenEpoch, userEpoch int) ([]string, int64) {
	current := tokenEpoch == userEpoch
	out := make([]string, 0, len(prior))
	kept := false
	for _, v := range prior {
		switch v {
		case "reauth":
			continue
		case "otp", "webauthn", "mfa", models.DeviceTrustAMR:
			if current {
				out = append(out, v)
				kept = true
			}
		default:
			out = append(out, v)
		}
	}
	if !kept {
		return out, 0
	}
	return out, priorLastOTPAt
}
```

at both call sites:

```go
	amr, lastOTPAt := carryAMR(claims.AMR, claims.LastOTPAt, claims.MFAEpoch, user.MFAEpoch)
	pair, err := s.jwtService.GenerateTokenPairWithAMR(user, device, security, amr, lastOTPAt)
```

Apply at `:1574` (`RefreshTokensWithRiskAssessment`) and `:1780` (`MintAccessTokenFromRefresh`). Both mints stamp `MFAEpoch: user.MFAEpoch` (the current one) and carry `AuthTime` unchanged.

- [ ] **Step 7: Fix the stale comment**

`jwt_service.go:491-492` says refresh tokens do not carry `amr`. They do, and this is where they are recomputed. Rewrite it to point at `carryAMR`.

- [ ] **Step 8: Run everything**

Run: `go vet ./... && go test ./internal/shared/middleware/... ./internal/core/auth/... -count=1`
Expected: PASS, including `TestRequireStepUp_ReauthAMRSatisfiesGate` unchanged.

- [ ] **Step 9: Document and commit**

`backend/internal/core/auth/CLAUDE.md` `:890`, `:894`, `:902` claim `reauth` "can never bypass"; `:892` lists the step-up predicate. Correct all four and describe the epoch + `carryAMR`.

```bash
git add backend/internal/shared/middleware backend/internal/core/auth
git commit -m "$(cat <<'EOF'
fix(auth): enforce the MFA epoch and recompute markers on refresh

Five places consume MFA authority — RequireMFA, RequireStepUp, the
enrolment gate, the impersonation bypass and Cedar's
principal.mfa_enrolled — and all five now read it through one resolver
that ignores the markers when the token's epoch is behind the user's. A
token with no MFA markers costs no lookup, the answer is memoised per
request, and a lookup error reads as stale.

Refresh stops copying amr and last_otp_at forward verbatim, which is
what let a session whose factor had been removed keep minting
MFA-satisfied tokens for its whole life. reauth is always dropped (a
five-minute proof is not a session property), the MFA markers survive
only under a matching epoch, LastOTPAt follows them, and the base
markers describing how the session began always survive.

amrSatisfiesMFA becomes strict: reauth proves presence, not a second
factor, and accepting it in a session-long MFA gate was M-1. The
sidecar validator has always used the strict list; the drift closes.

Spec §4.3 D16, D17, D18. Closes M-1 and M-2.

Claude-Session: https://claude.ai/code/session_0155Lt3QwR9zX15oGNKrn1T1
EOF
)"
```

---

## Task 7: The two SPAs (D14)

One new 401 code to handle, and one client-side page that must stop offering enrolment to an already-enrolled user.

**Files:**
- Modify: `frontend-admin/src/services/baseApi.ts` (`:536-565`)
- Modify: `frontend-admin/src/pages/.../MfaEnrollWizard.tsx`, `WebAuthnEnrollDialog.tsx` (let the code through)
- Modify: `frontend-client/src/lib/authedFetch.ts` (`:23-33`)
- Modify: `frontend-client/src/pages/.../MfaEnrolPage.tsx`
- Test: `frontend-admin/src/services/baseApi.test.ts`, `frontend-client/src/lib/authedFetch.test.ts`, `frontend-client/src/pages/MfaEnrolPage.test.tsx`

**Interfaces:**
- Consumes: the `reauthentication_required` envelope (Task 3) — `{ code, maxAgeSeconds, authTime }`.
- Produces: no new exports; `sanitizeReturnTo` (`frontend-admin/src/utils/returnTo.ts:64-98`) and `sanitizeNext` (`frontend-client/src/lib/safeNext.ts:82-111`) are reused as-is.

- [ ] **Step 1: Write the failing admin test**

Append to `frontend-admin/src/services/baseApi.test.ts`:

```ts
// The operator console already routes step_up_required to StepUpModal and
// replays. reauthentication_required has no modal answer: the user must
// sign in again, and the answer is the same for a password account and
// an OAuth one.
it('turns reauthentication_required into a login redirect carrying the current path', async () => {
  const navigate = vi.fn()
  installNavigate(navigate)
  window.history.pushState({}, '', '/user/security?tab=mfa')

  server.use(
    http.post('*/v1/auth/operator/mfa/enroll/begin', () =>
      HttpResponse.json(
        { status: 401, code: 'reauthentication_required', maxAgeSeconds: 300, authTime: 0 },
        { status: 401 },
      ),
    ),
  )

  await store.dispatch(api.endpoints.mfaEnrollBegin.initiate())

  expect(navigate).toHaveBeenCalledWith('/login?next=%2Fuser%2Fsecurity%3Ftab%3Dmfa')
  expect(sessionIsCleared()).toBe(true)
})

// The existing step-up path must be untouched: it still opens the modal
// and replays, it does NOT redirect.
it('leaves step_up_required on the modal path', async () => {
  const navigate = vi.fn()
  installNavigate(navigate)
  server.use(
    http.post('*/v1/auth/operator/mfa/enroll/begin', () =>
      HttpResponse.json({ status: 401, code: 'step_up_required', maxAgeSeconds: 300 }, { status: 401 }),
    ),
  )

  await store.dispatch(api.endpoints.mfaEnrollBegin.initiate())

  expect(navigate).not.toHaveBeenCalled()
  expect(stepUpModalOpened()).toBe(true)
})

// The return path is sanitised — an open redirect here would be handed
// an attacker-controlled destination on every stale enrolment attempt.
it('sanitises a hostile current path before putting it in next', async () => {
  const navigate = vi.fn()
  installNavigate(navigate)
  window.history.pushState({}, '', '//evil.example.com/steal')

  server.use(
    http.post('*/v1/auth/operator/mfa/enroll/begin', () =>
      HttpResponse.json({ status: 401, code: 'reauthentication_required' }, { status: 401 }),
    ),
  )
  await store.dispatch(api.endpoints.mfaEnrollBegin.initiate())

  expect(navigate).toHaveBeenCalledWith('/login')
})
```

- [ ] **Step 2: Run it to verify it fails**

Run: `cd frontend-admin && npx vitest run src/services/baseApi.test.ts`
Expected: FAIL — the code falls through to the generic 401 handling.

> **Watch:** vitest exits non-zero on an *unhandled MSW request* even
> when every assertion passes (`project_frontend_admin_i18n_phase4`).
> If the run is red with no failing assertion, a request escaped the
> handlers — add it.

- [ ] **Step 3: Add the admin branch**

In `frontend-admin/src/services/baseApi.ts`, in the existing 401 dispatch beside `step_up_required`:

```ts
      // reauthentication_required has no modal answer: the session is too
      // old to add a second factor and the only fix is a fresh sign-in.
      // Deliberately uniform for password and OAuth accounts — a
      // password-confirm prompt would be wrong for both an OAuth-only
      // user (no password) and an MFA-obligated one in their grace
      // window (the reconfirm endpoint refuses them).
      if (code === 'reauthentication_required') {
        clearSession()
        const next = sanitizeReturnTo(
          window.location.pathname + window.location.search,
        )
        navigate(next ? `/login?next=${encodeURIComponent(next)}` : '/login')
        return result
      }
```

And in `MfaEnrollWizard.tsx` / `WebAuthnEnrollDialog.tsx`, let a 401 whose code is `reauthentication_required` reach the interceptor rather than rendering their generic error copy.

- [ ] **Step 4: Write the failing client tests**

`frontend-client/src/lib/authedFetch.test.ts`:

```ts
// The client SPA has no step-up modal and stays modal-free (spec §4.2
// D14): reauthentication_required is a redirect, step_up_required keeps
// its existing inline error copy.
it('redirects to login with next on reauthentication_required', async () => {
  const res = await authedFetchWith(
    jsonResponse(401, { code: 'reauthentication_required' }),
    '/v1/auth/client/mfa/enroll/begin',
  )
  expect(locationAssign).toHaveBeenCalledWith('/login?next=%2Faccount%2Fsecurity%2Fmfa')
  expect(res.status).toBe(401)
})

it('does not redirect on step_up_required', async () => {
  await authedFetchWith(jsonResponse(401, { code: 'step_up_required' }), '/v1/auth/client/mfa/enroll/begin')
  expect(locationAssign).not.toHaveBeenCalled()
})
```

`frontend-client/src/pages/MfaEnrolPage.test.tsx`:

```tsx
// An enrolled client user reaching this page must see their state, not
// a wizard that will 401 them — the client tier's supported flow is
// first-enrolment-only, and replacement goes through the operator's
// admin reset until the SPA grows a step-up modal.
it('renders the enrolled state instead of the wizard when /me/mfa reports enrolled', async () => {
  server.use(
    http.get('*/v1/auth/client/me/mfa', () =>
      HttpResponse.json({ enrolled: true, methods: ['totp'] }),
    ),
  )
  render(<MfaEnrolPage />)
  expect(await screen.findByText(/already set up/i)).toBeInTheDocument()
  expect(screen.queryByRole('button', { name: /start/i })).not.toBeInTheDocument()
})

it('renders the wizard when /me/mfa reports not enrolled', async () => {
  server.use(
    http.get('*/v1/auth/client/me/mfa', () => HttpResponse.json({ enrolled: false, methods: [] })),
  )
  render(<MfaEnrolPage />)
  expect(await screen.findByRole('button', { name: /start/i })).toBeInTheDocument()
})
```

- [ ] **Step 5: Implement the client side**

`authedFetch.ts` gains the same branch via `sanitizeNext`; `MfaEnrolPage.tsx` reads `GET /v1/auth/client/me/mfa` first and branches on `enrolled`, and maps a 401 `step_up_required` onto the existing error copy.

- [ ] **Step 6: Run both frontends**

```bash
cd /home/tore/orkestra/frontend-admin && npx vitest run && npm run typecheck && npm run lint
cd /home/tore/orkestra/frontend-client && npx vitest run && npm run typecheck && npm run lint
```
Expected: PASS.

- [ ] **Step 7: Commit**

```bash
git add frontend-admin frontend-client
git commit -m "$(cat <<'EOF'
feat(spa): handle reauthentication_required on both consoles

Both SPAs turn the new 401 into a sign-in with a sanitised return path,
so a user whose session is too old to add a second factor lands back
where they were with a fresh auth_time. No modal: the answer is the same
for a password account and an OAuth one, and a password-confirm prompt
would be wrong for both an OAuth-only user and an MFA-obligated one in
their grace window.

step_up_required keeps its existing behaviour on both surfaces — modal
and replay on the operator console, inline copy on the client SPA.

MfaEnrolPage reads /me/mfa first and renders an enrolled state instead
of a wizard that would 401: the client tier's supported flow is
first-enrolment-only until it grows a step-up modal.

Spec §4.2 D14.

Claude-Session: https://claude.ai/code/session_0155Lt3QwR9zX15oGNKrn1T1
EOF
)"
```

---

## Task 8: Password-confirm refuses MFA-obligated users, and MFA verify gets its cap (D19, D20)

**Files:**
- Modify: `backend/internal/core/auth/services/password_auth_service.go` (`ConfirmPasswordWithSecurity`, after `:1281-1297`)
- Modify: `backend/internal/core/auth/handlers/password_handler.go` (map the new sentinel)
- Modify: `backend/internal/core/auth/handlers/mfa_handler.go` (`Verify` `:405-447`)
- Modify: `backend/internal/core/auth/handlers/webauthn_handler.go` (`VerifyFinish` `:282-312`)
- Test: `backend/internal/core/auth/services/password_confirm_test.go`, `mfa_handler_reset_test.go`

**Interfaces:**
- Consumes: `AttemptCounter`, `AttemptKeyMFAVerify`, `MFAVerifyLimit`, `lockoutError`, `LockedAfter` (all PR A); `policy.MFARequired` (`auth_policy_service.go:522`).
- Produces: `services.ErrPasswordConfirmEnrollmentRequired`.

- [ ] **Step 1: Write the failing tests**

```go
// M-1: password-confirm ignored RoleRequiresMFA, so an MFA-obligated
// user with no factor could mint a reauth token and satisfy step-up
// with a password alone — exactly what the obligation exists to stop.
func TestConfirmPassword_RefusesAnMFAObligatedUser(t *testing.T) {
	svc := newConfirmTestService(t, withMFARequired(true))
	_, err := svc.ConfirmPasswordWithSecurity(context.Background(), "u-1", correctTestPassword,
		[]string{"pwd"}, nil, &authModels.SecurityContext{SessionID: "sid-1"})
	if !errors.Is(err, ErrPasswordConfirmEnrollmentRequired) {
		t.Fatalf("err = %v, want ErrPasswordConfirmEnrollmentRequired", err)
	}
}

func TestConfirmPassword_AllowsANonObligatedUser(t *testing.T) {
	svc := newConfirmTestService(t, withMFARequired(false))
	if _, err := svc.ConfirmPasswordWithSecurity(context.Background(), "u-1", correctTestPassword,
		[]string{"pwd"}, nil, &authModels.SecurityContext{SessionID: "sid-1"}); err != nil {
		t.Fatalf("a non-obligated password user must still be served: %v", err)
	}
}

// The handler maps it to 403 with the middleware's own
// mfa_enrollment_required envelope shape, so the SPA's per-page
// handling sees ONE code for one situation.
func TestMapPasswordError_EnrollmentRequiredIs403(t *testing.T) {
	err := mapPasswordError(services.ErrPasswordConfirmEnrollmentRequired)
	assertStatusAndCode(t, err, http.StatusForbidden, "mfa_enrollment_required")
}

// M-3: /mfa/verify had NO attempt cap for an authenticated caller —
// the per-challenge counter bounds one challenge, not a caller who
// keeps starting new ones.
func TestMFAVerify_LocksAfterFiveFailures(t *testing.T) {
	h, deps := newMFAHandlerForTest(t)
	deps.factors.seedTOTP(t, "u-1")

	for i := 0; i < 5; i++ {
		if code := callVerify(t, h, "u-1", "000000"); code != http.StatusUnauthorized {
			t.Fatalf("attempt %d = %d, want 401", i+1, code)
		}
	}
	if code := callVerify(t, h, "u-1", "000000"); code != http.StatusTooManyRequests {
		t.Fatalf("attempt 6 = %d, want 429", code)
	}
}

// A backup-code attempt is ONE failure however many hashes it compares.
func TestMFAVerify_BackupCodeAttemptCostsOne(t *testing.T) {
	h, deps := newMFAHandlerForTest(t)
	deps.factors.seedTOTPWithBackupCodes(t, "u-1", 10)

	callVerify(t, h, "u-1", "not-a-real-backup-code")
	if got := deps.counter.countFor(services.AttemptKeyMFAVerify(services.PolicyAudienceOperator, "u-1")); got != 1 {
		t.Fatalf("count = %d after one backup-code attempt, want 1", got)
	}
}

func TestMFAVerify_SuccessResetsTheCounter(t *testing.T) {
	h, deps := newMFAHandlerForTest(t)
	deps.factors.seedTOTP(t, "u-1")
	callVerify(t, h, "u-1", "000000")
	callVerify(t, h, "u-1", deps.factors.currentTOTP(t, "u-1"))

	if got := deps.counter.countFor(services.AttemptKeyMFAVerify(services.PolicyAudienceOperator, "u-1")); got != 0 {
		t.Fatalf("count = %d after a success, want 0", got)
	}
}

func TestWebAuthnVerifyFinish_LocksAfterFiveFailures(t *testing.T) {
	h, deps := newWebAuthnHandlerForTest(t)
	deps.factors.seedWebAuthn(t, "u-1", 1)
	for i := 0; i < 5; i++ {
		callVerifyFinish(t, h, "u-1", badAssertion())
	}
	if code := callVerifyFinish(t, h, "u-1", badAssertion()); code != http.StatusTooManyRequests {
		t.Fatalf("attempt 6 = %d, want 429", code)
	}
}
```

- [ ] **Step 2: Run it to verify it fails**

Run: `go test ./internal/core/auth/... -run 'ConfirmPassword_Refuses|ConfirmPassword_Allows|EnrollmentRequiredIs403|MFAVerify_|WebAuthnVerifyFinish_' -count=1`
Expected: FAIL — the sentinel does not exist and neither verify route is capped.

- [ ] **Step 3: Add the refusal**

In `ConfirmPasswordWithSecurity`, after the enrolled-factor refusal:

```go
	// The obligation, not just the enrolment. A user whose role REQUIRES
	// MFA and has not yet enrolled must not be able to satisfy step-up
	// with a password: that is exactly what the obligation exists to
	// prevent, and the enrolled-factor check above does not catch them
	// (they have no factor yet). Memberships are resolved the way
	// completeLogin resolves them for its own MFA decision.
	if s.policy != nil {
		memberships, err := s.loadMemberships(ctx, user)
		if err != nil {
			return nil, err
		}
		if s.policy.MFARequired(user, memberships) {
			return nil, ErrPasswordConfirmEnrollmentRequired
		}
	}
```

with the sentinel:

```go
// ErrPasswordConfirmEnrollmentRequired — the caller's role obliges them
// to hold a second factor and they have not enrolled one. The handler
// maps it to 403 with the middleware's mfa_enrollment_required envelope,
// so the SPA sees one code for one situation
// (SessionsTab.tsx:59-60 already handles it).
var ErrPasswordConfirmEnrollmentRequired = stderrors.New("mfa enrollment required before password reconfirm")
```

- [ ] **Step 4: Cap the two verify routes**

Give both handlers the `AttemptCounter` and, in `MFAHandler.Verify` and `WebAuthnHandler.VerifyFinish`:

```go
	key := services.AttemptKeyMFAVerify(h.audience, userUUID)
	if v, err := h.counter.Locked(ctx, key, services.MFAVerifyLimit); err == nil && v.Locked {
		return nil, lockoutError(v.RetryAfter)
	}
	// … verify …
	// on an invalid code / assertion — ONE failure, whatever the
	// verification compared internally:
	_, _ = h.counter.RecordFailure(ctx, key, services.MFAVerifyLimit)
	// on success:
	_ = h.counter.Reset(ctx, key)
```

The per-challenge counter inside `FinishAssertion` stays as the inner bound; this is the outer one, and it survives a caller who keeps starting new challenges.

- [ ] **Step 5: Run and commit**

```bash
go vet ./... && go test ./internal/core/auth/... -count=1
git add backend/internal/core/auth
git commit -m "$(cat <<'EOF'
fix(auth): refuse password-confirm for MFA-obligated users; cap MFA verify

Password-confirm checked whether a factor was ENROLLED but not whether
the caller's role OBLIGES one, so an MFA-obligated user with no factor
could mint a reauth token and satisfy step-up with a password alone —
exactly what the obligation exists to prevent. It now answers 403 with
the mfa_enrollment_required envelope the SPA already handles.

/mfa/verify and /mfa/webauthn/verify/finish had no attempt cap for an
authenticated caller: the per-challenge counter bounds one challenge,
not a caller who keeps starting new ones. Both now peek and record on
the mfa-verify scope, answering 429 with Retry-After at five failures,
resetting on success, and charging a backup-code attempt exactly once
however many hashes it compared.

Spec §4.3 D19, D20. Closes M-1's second half and M-3.

Claude-Session: https://claude.ai/code/session_0155Lt3QwR9zX15oGNKrn1T1
EOF
)"
```

---

## Task 9: Documentation, OpenAPI and the staging drill

- [ ] **Step 1: Finish the docs sweep**

- `backend/internal/core/auth/CLAUDE.md` — new sections for the enrolment gate + `auth_time`, the MFA epoch, refresh AMR recomputation; corrected `:832-833`, `:837-840`, `:890-902`.
- `backend/internal/core/user/CLAUDE.md` — `mfaEpoch` and the bumper seam.
- `backend/pkg/sdk/CLAUDE.md` — `MFAEpochBumper`, `User.MFAEpoch`.
- `docs/site/modules/core/auth.mdx` — enrolment gate and reset semantics.
- `docs/site/architecture/authentication-flow.mdx` — `:120` claims table (`auth_time`, `mfae`), plus `:167`, `:174-177`, `:191`, `:199-203`.

- [ ] **Step 2: Regenerate and gate**

```bash
make -C /home/tore/orkestra openapi-dump
make -C /home/tore/orkestra ci-backend
make -C /home/tore/orkestra ci-frontend-client
cd frontend-admin && npm run typecheck && npm run lint
git diff --check
```

- [ ] **Step 3: Commit and open the PR**

```bash
cd /home/tore/orkestra && git add docs backend
git commit -m "$(cat <<'EOF'
docs(auth): document the enrolment gate, auth_time and the MFA epoch

Spec §4.11. Regenerates the OpenAPI dump.

Claude-Session: https://claude.ai/code/session_0155Lt3QwR9zX15oGNKrn1T1
EOF
)"
git push -u origin feat/auth-authz-audit-remediation-pr-b
gh pr create --base dev --title "PR B: enrolment gate and MFA epoch (auth/authz audit remediation)" --body "$(cat <<'EOF'
Implements §4.2 (D11–D14) and §4.3 (D15–D20) of `docs/superpowers/specs/2026-09-03-auth-authz-audit-remediation-design.md`, the second of five deliverables in §7. **Depends on PR A** (D20 consumes its attempt counters).

**Closes:** H-2, H-3, M-1, M-2, M-3.

Plan: `docs/superpowers/plans/2026-09-03-auth-authz-audit-remediation-pr-b-mfa-gate-and-epoch.md`

https://claude.ai/code/session_0155Lt3QwR9zX15oGNKrn1T1
EOF
)"
```

- [ ] **Step 4: Staging drill (spec §7, PR B row)**

1. Enrol TOTP, then call `enroll/begin` again from a plain session → **`step_up_required`**.
2. Sign in, wait past five minutes, call `enroll/begin` with no factor → **`reauthentication_required`**, and the console lands you on `/login?next=…` and back on the security page after signing in.
3. Admin-reset a target with a second tab open → the tab is signed out, and the target's own stepped-up token stops passing step-up **on its next request**, not at its expiry.
4. Reset a **passkey-only** target → succeeds (it 404s today).
5. Remove one passkey while a TOTP factor and a second passkey remain → the other sessions end and the epoch moves.
6. Fail `/mfa/verify` five times → the sixth answers **429** with `Retry-After`; a correct code afterwards (past the window) works and resets.
7. Confirm the `auth.mfa_factor_added` email arrives on a first enrolment and on a passkey registration, in EN and IT.

---

## Self-review

**Spec coverage (§4.2 D11–D14, §4.3 D15–D20 + §6 "PR B — MFA"):**

| Spec item | Task |
|---|---|
| D11 `RequireEnrolmentProof`, two proof shapes, fail-closed, `reauthentication_required` | 3 |
| D11 `auth_time` claim, stamped at every session-creating mint, carried by refresh | 2 |
| D12 route move to `RegisterEnrolmentRoutes`, both halves gated, all four mount sites | 3 |
| D13 replacement side effects, five security events, `auth.mfa_factor_added` email | 5 |
| D14 both SPAs | 7 |
| D15 `RemoveFactor` deletes both rows | 4 |
| D16 epoch storage + SDK seam | 1 |
| D16 `mfae` claim | 2 |
| D16 bumps on every removal/replacement; session termination per path | 5 |
| D16 `mfaAuthority` resolver, five consumers, memoisation, fail-closed | 6 |
| D17 `carryAMR` on both refresh paths, `reauth` always dropped | 6 |
| D18 strict `amrSatisfiesMFA` + `amrSatisfiesStepUp` | 3 (predicate), 6 (strictness) |
| D19 password-confirm refuses MFA-obligated users | 8 |
| D20 attempt cap on both authenticated verify routes | 8 |
| §4.11 docs | 1, 3, 5, 6, 9 |

**Placeholder scan:** none. Three tasks name an existing fixture to extend rather than reproducing it (`newMFAServiceForTest`, `newMFAHandlerForTest`, the `mintVia*` helpers) and each states exactly what to add.

**Type consistency:** `iface.MFAEpochBumper.BumpMFAEpoch` has the same signature in Tasks 1 and 5. `claims.AuthTime` / `claims.MFAEpoch` are named identically in Tasks 2, 3 and 6. `carryAMR(prior []string, priorLastOTPAt int64, tokenEpoch, userEpoch int)` is used with that exact signature in Task 6's tests and implementation. `RegisterEnrolmentRoutes` is the same name on both handlers. `amrSatisfiesStepUp` is introduced in Task 3 and its consumers listed in Task 6.

**One risk worth naming:** Task 6's `mfaAuthority` mutates the request to memoise. The plan says to pick one of the two mechanisms and use it at all five call sites; mixing them makes the memoisation silently degrade to one lookup per gate. A reviewer should check that specifically.
