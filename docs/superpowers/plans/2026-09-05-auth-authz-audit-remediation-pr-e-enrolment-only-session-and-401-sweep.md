# Auth/Authz Audit Remediation — PR E: Enrolment-Only Session and the 401 Sweep — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make an obligation to enrol a second factor incapable of becoming an inability to enrol one, and make every 401 that is a verdict on the request name itself so no client can mistake it for a dead credential.

**Architecture:** Two independent mechanisms. **(1)** A privileged account whose MFA grace window has lapsed stops being refused a login and instead receives an *enrolment-only session* — an access token with no refresh cookie, a lifetime equal to the enrolment gate's `maxAge`, and a new `enroll_only` claim that `RequireAuth` refuses on every route except the four the enrolment mount already owns. Enrolling clears the grace stamp; the user then signs in normally, because a user who holds a factor must prove it. **(2)** Every bare `huma.Error401Unauthorized` in the tree is given an error code, and a new `errquality` rule R4 makes a bare one a build failure — with one deliberate exception, `RequireAuth`'s own rejection of an unparseable bearer, which the operator console's key-rotation recovery depends on being codeless.

**Tech Stack:** Go 1.26.8, Huma v2, chi routers, RS256 JWT (`jwt_service.go` claim map), MongoDB 8, `go/analysis` (the in-tree `tools/errquality` analyzer), React 19 + RTK Query (`frontend-admin`), React 19 + `authedFetch` (`frontend-client`), Vitest.

**Spec:** `docs/superpowers/specs/2026-09-03-auth-authz-audit-remediation-design.md` **v1.16** — this plan implements the **PR E** row of §7: §4.12 (**D37–D42**) and §4.13 (**D43–D46**) in full, plus the §4.11 documentation lines for PR E, the §5 edge cases 32–37 and the §6 "PR E" test list.

**Depends on:** **PR B must be merged** (it is — `6e00409c9` on `dev`). D39's allowlist is the mount D12 created; D38's TTL is D11's `maxAge`; the sweep's premise is the `4cfa2531b` fix. Nothing here depends on PR C or PR D.

## 🔴 Before Task 1: run the pre-flight scan

**PR B's plan had 15 blocking defects and PR C's had 22.** Both were written from the same spec by the same process as this one. Dispatch a read-only opus subagent to produce the five tables (cross-task interfaces · intra-task self-consistency · **anchor verification** · **test fixtures** · risks/rubric conflicts) against the tree at `origin/dev`, and resolve every BLOCKING row before Task 1. Tables 3 and 4 are what implementers actually trip on. Worked example: `~/orkestra-archives/pr-b-handoff-2026-09-05/preflight-scan.md`.

Anchors in this plan were verified against `origin/dev` @ `38c8c9b4e` on 2026-09-05, but **line numbers move**: find prose and code by content, never by the `file:line` written here.

## Global Constraints

- **The gate is never relaxed.** No branch added here lets a factor-less privileged caller hold ordinary authority. D37 grants a *session*, never permissions. A change that would make `RoleRequiresMFA` return false, or exempt a user from `RequireEnrolmentProof`, is out of scope and wrong.
- **Deny by default.** `RequireAuth` refuses an `enroll_only` bearer on every route not in the derived allowlist. When in doubt about a route, it is denied.
- **The allowlist is derived, never hand-written.** One exported list feeds both `RegisterEnrolmentRoutes` and `RequireAuth`, with a drift test. A hand-maintained path allowlist is what failed open in the console (`AUTH_ENDPOINT_PATHS`).
- **The enrolment-only mint creates no refresh token and no family.** If a step makes one, the escalation D38 exists to prevent has been reintroduced.
- **Grace-expired only.** A user inside an open grace window keeps today's behaviour exactly: a full session, `mfaEnrollmentRequired: true`, and `mfaGraceExpiresAt`. A diff that changes the open-window branch is out of scope.
- **`RequireAuth`'s codeless 401 stays codeless (D44).** It is not a defect. Coding it removes the console's recovery from a signing-key rotation and restores a ≈14.5-minute window of silently failing requests.
- **R4 is 401-only.** Bare `huma.Error400BadRequest` / `Error403Forbidden` are out of scope; only 401 drives the console's rotation arm.
- **Every new `errcode` const needs a `goldenCodes` row** in `internal/shared/errcode/codes_test.go`, in the same commit — `TestEveryConstSnapshotted` fails otherwise.
- **`TokenResponse` gains a field, so the OpenAPI dump moves.** Run `make -C /home/tore/orkestra openapi-dump` in the task that changes it or `backend-openapi-check` fails.
- **A new Mongo call needs `//tenantscope:allow`,** and inserted lines shift every baselined entry for the file — regenerate the tenantscope baseline when the gate says so.
- **Docs move in the same commit as the code they describe** (repo rule): `backend/internal/core/auth/CLAUDE.md`, `backend/CLAUDE.md`, `backend/tools/errquality/CLAUDE.md`, `docs/site/architecture/authentication-flow.mdx`, `docs/site/sdk/build-your-first-addon.mdx`.
- **Test commands** (from `/home/tore/orkestra/backend` unless stated):
  - `go test ./internal/core/auth/... ./internal/shared/middleware/... -count=1`
  - `go test ./tools/errquality/... -count=1`
  - `go vet ./...` before every commit
  - frontend: `cd frontend-admin && npx vitest run && npm run typecheck && npm run lint`; same for `frontend-client`
  - full gate before every commit: `make -C /home/tore/orkestra ci-backend` (and `ci-frontend-admin` / `ci-frontend-client` for the SPA tasks) — **binding**
  - live Mongo where guarded: `MONGO_TEST_URI='mongodb://127.0.0.1:28017/?directConnection=true'`
- **Never start servers manually.** Backend and frontend run in Docker with hot reload.
- **Never two `make ci-backend` at once** (golangci-lint takes a global lock) and never two vitest runs at once (`coverage/.tmp` contention).
- **Commit trailer, typed literally, via `git commit -F <file>`** — a heredoc shipped a literal `${CLAUDE_SESSION}` in 4 of 9 tasks on PR B:

  `Claude-Session: https://claude.ai/code/session_0155Lt3QwR9zX15oGNKrn1T1`

## Declared deviations from the spec (read before executing)

1. 🔴 **D40's "mints a full pair" is NOT implemented. A successful enrolment clears the grace stamp and the user signs in again.** The spec says `enroll/confirm` from an enrolment-only session returns a full token pair. Implementing that produces a session shape the login path can never produce — **an enrolled user holding `amr: ["pwd"]`** — because the user has just created a factor they have not used. `RequireMFA()` gates the authz role routes (`authz/module.go:325,331,337,343,349`) and the tenant routes (`tenant/module.go:359,365`), so that session would answer `mfa_required` on exactly the pages an administrator needs, i.e. a console that looks signed in and is half broken. The alternatives are worse: synthesising an MFA marker the user never proved would be a lie in the token. Signing in again is one extra login, it produces a consistent session, and it verifies the freshly enrolled factor at the only moment when failing is cheap — the backup codes are still on screen. **The SPA must display and acknowledge the backup codes before it signs the user out** (Task 6). If the architect rejects this deviation, D40 needs the token-issuer wiring and a ruling on what `amr` such a pair carries; nothing else in the plan changes.
2. **`MFAEnrolmentProofMaxAge` is a new exported constant** in `internal/core/auth/services/mfa_policy.go`, next to `MFAEnrollmentGraceWindow`. The spec cites the gate's `maxAge` as `5*time.Minute` at two literals (`auth/module.go:1784` and `:1924`); D38 needs the same value inside `password_auth_service`, and a third literal would be the drift the spec warns about elsewhere. Both mount sites are changed to use the constant.
3. **The access-token lifetime override rides on `SecurityContext`,** as `Lifetime time.Duration` (zero = the policy default), beside `AMR`, `LastOTPAt` and `AuthTime`. The spec names the TTL but not the seam; `SecurityContext` is already the documented transport for per-mint claim data ("the mint seams take a SecurityContext, not a claims struct"), so this follows the established pattern rather than adding a JWTService method.
4. **The R4 baseline is populated in Task 8 and deleted in Task 10.** The spec says R4 ships with an empty baseline, and it does — at the end of the PR. Freezing the 74 known sites when the rule lands means the rule is live against *new* code for the whole sweep, which is the repo's own stated pattern for `errquality`. Task 10's final step asserts no `:R4` line survives.
5. **`GenerateAccessTokenForSessionWithAMR` is not reused for the enrolment mint.** It exists and mints access-only, but it takes an already-created session; the enrolment mint must also create the session document (edge case 32: these sessions count against the ADR-0017 cap). The mint therefore branches inside `issueTokensForSession`, which already owns session creation, rather than adding a second session-creating path.

## File Structure

**Backend — `backend/internal/core/auth/`**

| File | Responsibility | Task |
|---|---|---|
| `models/token.go` | + `JWTClaims.EnrollOnly` (`enroll_only`) | 1 |
| `models/security.go` | + `SecurityContext.EnrollOnly`, `SecurityContext.Lifetime` (Task 1); + `TokenResponse.EnrollmentOnly` (Task 3) — both structs live here | 1, 3 |
| `services/mfa_policy.go` | + `MFAEnrolmentProofMaxAge` | 1 |
| `services/jwt_service.go` | `claimsToMap` / `mapToClaims` for `enroll_only`; lifetime override in `GenerateEnhancedAccessToken` | 1 |
| `services/password_auth_service.go` | `LoginTokenContext.EnrolmentOnly`; the no-refresh branch of `issueTokensForSession`; D37's `completeLogin` branch | 3 |
| `services/auth_service.go` | `evaluateMFAForOAuth` D37 twin + its caller | 4 |
| `services/mfa_service.go`, `services/webauthn_service.go` | `ClearMFAGrace` on successful enrolment (D40) | 5 |
| `handlers/mfa_handler.go`, `handlers/webauthn_handler.go` | `EnrolmentPaths`; register from it | 2 |
| `module.go` | both mounts use `MFAEnrolmentProofMaxAge` | 1 |
| `CLAUDE.md` | the enrolment-only session, end to end | 5 |

**Backend — `backend/internal/shared/`**

| File | Responsibility | Task |
|---|---|---|
| `middleware/auth.go` | the `enroll_only` deny in `RequireAuth`; `sendMFAEnrollmentRequired` reuse | 2 |
| `middleware/jwt_validator.go` | sidecar `parseClaims` mirrors `enroll_only` (`:183`, beside `auth_time` at `:230`) | 1 |
| `errcode/codes.go` + `codes_test.go` | the new 401 codes + their `goldenCodes` rows | 9 |

**Backend — `backend/tools/errquality/`**

| File | Responsibility | Task |
|---|---|---|
| `analyzer.go` | rule R4 | 8 |
| `analyzer_test.go` | R4 unit tests | 8 |
| `baseline.txt` | the 74 frozen, then burned down | 8, 9, 10 |
| `CLAUDE.md` | R4 documented | 8 |

**Frontend**

| File | Responsibility | Task |
|---|---|---|
| `frontend-admin/src/store/api/authApi.ts` | `enrollmentOnly` on the login response type | 6 |
| `frontend-admin/src/hooks/auth/useAuthRTK.ts` | route on `enrollmentOnly` (`:127` handles `requiresMfa` today) | 6 |
| `frontend-admin/src/components/authentication/Login.tsx` | the enrolment-only destination | 6 |
| `frontend-admin/src/pages/user/security/…` (MFA wizard) | backup-code acknowledgement, then sign out | 6 |
| `frontend-admin/src/utils/oauthCallbackParams.ts` | carry `enrollmentOnly` through the OAuth fragment | 4 |
| `frontend-client/src/api/auth.ts`, `MfaEnrolPage.tsx` | the same routing on the client tier | 7 |

---

### Task 1: The `enroll_only` claim, its transport, and the shared max-age

**Files:**
- Modify: `backend/internal/core/auth/models/token.go` (`JWTClaims`, beside `AuthTime`)
- Modify: `backend/internal/core/auth/models/security.go` (`SecurityContext`, beside `AuthTime`)
- Modify: `backend/internal/core/auth/services/mfa_policy.go` (new constant)
- Modify: `backend/internal/core/auth/services/jwt_service.go` (`claimsToMap`, `mapToClaims`, `GenerateEnhancedAccessToken`)
- Modify: `backend/internal/shared/middleware/jwt_validator.go` (`parseClaims`)
- Modify: `backend/internal/core/auth/module.go` (two `5*time.Minute` literals)
- Test: `backend/internal/core/auth/services/jwt_service_amr_test.go` (the file that already round-trips `auth_time`)

**Interfaces:**
- Consumes: nothing.
- Produces: `models.JWTClaims.EnrollOnly bool`; `models.SecurityContext.EnrollOnly bool`; `models.SecurityContext.Lifetime time.Duration`; `services.MFAEnrolmentProofMaxAge time.Duration`. Tasks 2, 3, 4 and 5 all read these exact names.

- [ ] **Step 1: Write the failing round-trip test**

Add to `jwt_service_amr_test.go`:

```go
func TestEnrollOnlyClaimRoundTrips(t *testing.T) {
	svc := newTestJWTService(t) // same helper the auth_time tests use
	user := &iface.User{UUID: "u-enrol", Email: "e@example.com", Role: "administrator"}
	sec := &authModels.SecurityContext{
		SessionID:  "s-1",
		Timestamp:  time.Now(),
		AuthTime:   time.Now().Unix(),
		EnrollOnly: true,
		Lifetime:   5 * time.Minute,
	}
	tok, err := svc.GenerateAccessTokenForSessionWithAMR(user, &authModels.DeviceInfo{DeviceID: "d1"}, sec, []string{"pwd"}, 0)
	if err != nil {
		t.Fatalf("mint: %v", err)
	}
	claims, err := svc.ValidateAccessToken(tok)
	if err != nil {
		t.Fatalf("validate: %v", err)
	}
	if !claims.EnrollOnly {
		t.Error("enroll_only did not survive the round trip")
	}
	// Lifetime override wins over the policy default.
	if got := time.Until(time.Unix(claims.ExpiresAt, 0)); got > 6*time.Minute {
		t.Errorf("lifetime override ignored: token lives %v, want <= ~5m", got)
	}
}

func TestEnrollOnlyOmittedWhenFalse(t *testing.T) {
	svc := newTestJWTService(t)
	user := &iface.User{UUID: "u-plain", Email: "p@example.com", Role: "administrator"}
	sec := &authModels.SecurityContext{SessionID: "s-2", Timestamp: time.Now()}
	tok, err := svc.GenerateAccessTokenForSessionWithAMR(user, &authModels.DeviceInfo{DeviceID: "d2"}, sec, []string{"pwd"}, 0)
	if err != nil {
		t.Fatalf("mint: %v", err)
	}
	raw, err := svc.ParseUnverifiedClaims(tok)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if raw.EnrollOnly {
		t.Error("enroll_only set on an ordinary token")
	}
}
```

Read the existing `auth_time` tests in this file first and reuse their service-construction helper verbatim — do not invent `newTestJWTService` if it is named something else.

- [ ] **Step 2: Run it and watch it fail**

Run: `go test ./internal/core/auth/services/ -run TestEnrollOnly -count=1`
Expected: FAIL to compile — `EnrollOnly` and `Lifetime` are not fields of those structs.

- [ ] **Step 3: Add the two struct fields**

`models/token.go`, immediately after `AuthTime`:

```go
	// EnrollOnly (claim "enroll_only") marks a session minted solely so a
	// privileged account whose MFA grace window has lapsed can enrol a
	// first factor. RequireAuth refuses it on every route outside the
	// enrolment mount, so it carries no ordinary authority. Absent reads
	// as false, which is every token minted before this shipped.
	EnrollOnly bool `json:"enroll_only,omitempty"`
```

`models/security.go`, after `AuthTime`:

```go
	// EnrollOnly is the transport for JWTClaims.EnrollOnly, for the same
	// reason AMR, LastOTPAt and AuthTime are transported here: the mint
	// seams take a SecurityContext, not a claims struct.
	EnrollOnly bool `json:"enrollOnly,omitempty" bson:"-"`
	// Lifetime overrides the access token's lifetime for this mint only.
	// Zero means "use the admin policy / env default". The enrolment-only
	// mint sets it to MFAEnrolmentProofMaxAge so the session and the
	// enrolment proof lapse as one event rather than two.
	Lifetime time.Duration `json:"-" bson:"-"`
```

- [ ] **Step 4: Add the shared constant and remove the two literals**

`services/mfa_policy.go`, below `MFAEnrollmentGraceWindow`:

```go
// MFAEnrolmentProofMaxAge is how recent a proof of presence must be to
// create or replace a credential (spec §4.2 D11). It is also the lifetime
// of an enrolment-only session (§4.12 D38), so that "your session expired"
// and "your proof went stale" are one event with one message instead of
// two clocks lapsing minutes apart.
const MFAEnrolmentProofMaxAge = 5 * time.Minute
```

In `auth/module.go`, replace the literal in **both** gate constructions (operator and client — find them by content, `enrolmentGate(m.logger, "operator"` and `enrolmentGate(m.logger, "client"`):

```go
	operatorEnrolmentGate := enrolmentGate(m.logger, "operator", ri.Operator.AuthMW, services.MFAEnrolmentProofMaxAge)
```

- [ ] **Step 5: Thread the claim through both parsers and the lifetime through the mint**

`jwt_service.go`, in `claimsToMap` (beside the `auth_time` write):

```go
	if claims.EnrollOnly {
		m["enroll_only"] = true
	}
```

in `mapToClaims` (beside the `auth_time` read):

```go
	claims.EnrollOnly, _ = m["enroll_only"].(bool)
```

in `GenerateEnhancedAccessToken`, replace the `expiresAt` assignment:

```go
	lifetime := s.accessTokenLifetime(context.Background())
	if securityCtx != nil && securityCtx.Lifetime > 0 {
		lifetime = securityCtx.Lifetime
	}
	expiresAt := now.Add(lifetime)
```

and set the claim from the context where the other `securityCtx` fields are copied into `claims`:

```go
		EnrollOnly: securityCtx.EnrollOnly,
```

`internal/shared/middleware/jwt_validator.go`, in `parseClaims` beside the `auth_time` block:

```go
	if v, ok := m["enroll_only"].(bool); ok {
		c.EnrollOnly = v
	}
```

- [ ] **Step 6: Run the tests**

Run: `go test ./internal/core/auth/services/ ./internal/shared/middleware/ -count=1`
Expected: PASS, including the two new tests.

- [ ] **Step 7: Full gate, then commit**

Run: `make -C /home/tore/orkestra ci-backend` — expected EXIT 0. Then `git diff --check`.

```bash
git add backend/internal/core/auth/models/token.go backend/internal/core/auth/models/security.go \
        backend/internal/core/auth/services/mfa_policy.go backend/internal/core/auth/services/jwt_service.go \
        backend/internal/core/auth/services/jwt_service_amr_test.go \
        backend/internal/shared/middleware/jwt_validator.go backend/internal/core/auth/module.go
git commit -F .git/COMMIT_E1
```

`.git/COMMIT_E1` contains:

```
feat(auth): add the enroll_only claim and one shared enrolment max-age

Spec v1.16 §4.12 D38/D39. The claim rides SecurityContext like AMR,
LastOTPAt and auth_time do, and carries an optional per-mint lifetime
override so an enrolment-only session and its enrolment proof lapse as
one event. MFAEnrolmentProofMaxAge replaces the two 5*time.Minute
literals at the gate mounts; the value now has one home.

No behaviour changes yet: nothing sets EnrollOnly.

Claude-Session: https://claude.ai/code/session_0155Lt3QwR9zX15oGNKrn1T1
```

---

### Task 2: Deny by default in `RequireAuth`, with a derived allowlist

**Files:**
- Modify: `backend/internal/core/auth/handlers/mfa_handler.go` (`EnrolmentPaths`, `RegisterEnrolmentRoutes`)
- Modify: `backend/internal/core/auth/handlers/webauthn_handler.go` (`RegisterEnrolmentRoutes`)
- Modify: `backend/internal/shared/middleware/auth.go` (`RequireAuth`)
- Test: `backend/internal/core/auth/handlers/enrolment_paths_test.go` (new), `backend/internal/shared/middleware/enroll_only_test.go` (new)

**Interfaces:**
- Consumes: `models.JWTClaims.EnrollOnly` (Task 1).
- Produces: `handlers.EnrolmentPaths(mount RouteMount) []string` — the four enrolment paths, absolute, in registration order. `RequireAuth` denies an `enroll_only` bearer outside this list plus two support paths.

- [ ] **Step 1: Write the failing drift test**

Create `backend/internal/core/auth/handlers/enrolment_paths_test.go`:

```go
package handlers

import (
	"net/http"
	"sort"
	"testing"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/humatest"
)

// The allowlist RequireAuth consults and the routes RegisterEnrolmentRoutes
// mounts must be the same set. A route added to the mount without being
// added to the list would be denied to the very sessions that exist to reach
// it; a path listed without being mounted would open a hole.
func TestEnrolmentPathsMatchRegisteredRoutes(t *testing.T) {
	for _, mount := range []RouteMount{OperatorMount, ClientMount} {
		_, api := humatest.New(t, huma.DefaultConfig("t", "1"))
		(&MFAHandler{}).RegisterEnrolmentRoutes(api, mount)
		(&WebAuthnHandler{}).RegisterEnrolmentRoutes(api, mount)

		var registered []string
		for _, op := range api.OpenAPI().Paths {
			_ = op
		}
		for path := range api.OpenAPI().Paths {
			registered = append(registered, path)
		}
		want := EnrolmentPaths(mount)
		sort.Strings(registered)
		sorted := append([]string(nil), want...)
		sort.Strings(sorted)
		if len(registered) != len(sorted) {
			t.Fatalf("mount %q: registered %v, EnrolmentPaths %v", mount.PathPrefix, registered, sorted)
		}
		for i := range sorted {
			if registered[i] != sorted[i] {
				t.Errorf("mount %q: registered %v, EnrolmentPaths %v", mount.PathPrefix, registered, sorted)
				break
			}
		}
		_ = http.MethodPost
	}
}
```

If `humatest` cannot register against bare zero-value handlers, build them with the same nil-tolerant constructors the existing handler tests use — read `mfa_handler_reset_test.go` for the pattern before writing this.

- [ ] **Step 2: Run it and watch it fail**

Run: `go test ./internal/core/auth/handlers/ -run TestEnrolmentPathsMatch -count=1`
Expected: FAIL to compile — `EnrolmentPaths` is undefined.

- [ ] **Step 3: Add `EnrolmentPaths` and register from it**

In `mfa_handler.go`, above `RegisterEnrolmentRoutes`:

```go
// EnrolmentPaths is the single source of truth for which routes an
// enrolment-only session may reach (spec §4.12 D39). RegisterEnrolmentRoutes
// on both handlers registers exactly these, and RequireAuth allows exactly
// these; TestEnrolmentPathsMatchRegisteredRoutes fails if the two drift.
// Adding a route to the enrolment mount therefore allows it automatically,
// and adding one anywhere else denies it automatically.
func EnrolmentPaths(mount RouteMount) []string {
	base := "/v1/auth" + mount.PathPrefix
	return []string{
		base + "/mfa/enroll/begin",
		base + "/mfa/enroll/confirm",
		base + "/mfa/webauthn/register/begin",
		base + "/mfa/webauthn/register/finish",
	}
}
```

Change both `RegisterEnrolmentRoutes` bodies to take their `Path` from this list — index 0 and 1 in `mfa_handler.go`, index 2 and 3 in `webauthn_handler.go` — so a path can only be changed in one place:

```go
func (h *MFAHandler) RegisterEnrolmentRoutes(api huma.API, mount RouteMount) {
	paths := EnrolmentPaths(mount)
	huma.Register(api, huma.Operation{
		OperationID: mount.OpIDPrefix + "mfa-enroll-begin",
		Method:      http.MethodPost,
		Path:        paths[0],
		Summary:     "Begin MFA (TOTP) enrollment",
		Tags:        []string{"Authentication", "MFA"},
		Security:    []map[string][]string{{"bearerAuth": {}}},
	}, h.EnrollBegin)
	// … confirm uses paths[1]
}
```

- [ ] **Step 4: Run the drift test**

Run: `go test ./internal/core/auth/handlers/ -run TestEnrolmentPathsMatch -count=1`
Expected: PASS.

- [ ] **Step 5: Mutation-check the drift test — it must be able to fail**

Temporarily delete `base + "/mfa/enroll/confirm"` from `EnrolmentPaths` and re-run. Expected: FAIL. **Restore it.** A drift test that cannot fail is one of the three patterns that produced eight falsely-passing tests on PR C; prove this one bites before moving on.

- [ ] **Step 6: Write the failing middleware test**

Create `backend/internal/shared/middleware/enroll_only_test.go`. Reuse the harness the existing `RequireStepUp` / `step_up_test.go` tests use for building an `AuthMiddleware` with a stub JWT service.

```go
// An enrolment-only bearer reaches the enrolment mount and nothing else.
func TestRequireAuth_EnrollOnly_AllowsEnrolmentPaths(t *testing.T) {
	for _, p := range handlers.EnrolmentPaths(handlers.OperatorMount) {
		rec := doRequest(t, enrollOnlyBearer(t), p)
		if rec.Code == http.StatusForbidden {
			t.Errorf("%s: enrolment-only session was denied its own route", p)
		}
	}
}

func TestRequireAuth_EnrollOnly_AllowsSupportPaths(t *testing.T) {
	for _, p := range []string{"/v1/auth/operator/me", "/v1/auth/operator/me/mfa"} {
		rec := doRequest(t, enrollOnlyBearer(t), p)
		if rec.Code == http.StatusForbidden {
			t.Errorf("%s: denied, but the console needs it to boot and read factor status", p)
		}
	}
}

// The case RequireGlobal would have missed: a route gated only by
// RequireSystemPermission. RequireAuth is router-level, so it catches it.
func TestRequireAuth_EnrollOnly_DeniesSystemPermissionRoute(t *testing.T) {
	rec := doRequest(t, enrollOnlyBearer(t), "/v1/admin/users/u1/mfa/reset")
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", rec.Code)
	}
	if code := bodyCode(t, rec); code != "mfa_enrollment_required" {
		t.Errorf("code = %q, want mfa_enrollment_required", code)
	}
}

func TestRequireAuth_OrdinaryBearer_Unaffected(t *testing.T) {
	rec := doRequest(t, ordinaryBearer(t), "/v1/admin/users/u1/mfa/reset")
	if rec.Code == http.StatusForbidden {
		t.Error("an ordinary session was denied by the enroll_only gate")
	}
}
```

- [ ] **Step 7: Run it and watch it fail**

Run: `go test ./internal/shared/middleware/ -run TestRequireAuth_EnrollOnly -count=1`
Expected: FAIL — the deny does not exist, so the system-permission route is not 403.

- [ ] **Step 8: Add the deny to `RequireAuth`**

In `auth.go`, in `RequireAuth`, **after** the `sessionRevocationState` check and **before** `m.setUserContext(...)`:

```go
		// An enrolment-only session (spec §4.12 D38) exists so a privileged
		// account whose grace window lapsed can create a first factor. It
		// carries no other authority, and the check lives HERE rather than
		// in a per-route gate because RequireAuth is the only middleware
		// every protected route provably passes through: RequireGlobal is
		// not universal (the admin MFA-reset group mounts under
		// RequireSystemPermission alone), so a per-gate check would be one
		// omission away from a privileged factor-less session.
		if claims.EnrollOnly && !enrolmentOnlyMayReach(r.URL.Path) {
			m.sendMFAEnrollmentRequired(w, r)
			return
		}
```

and, next to it:

```go
// enrolmentOnlyMayReach reports whether an enrolment-only session may reach
// this path. The enrolment routes come from handlers.EnrolmentPaths so the
// list cannot drift from the mount; the two support paths are enumerated
// with their reasons because they are NOT part of that mount.
func enrolmentOnlyMayReach(path string) bool {
	for _, mount := range []handlers.RouteMount{handlers.OperatorMount, handlers.ClientMount} {
		for _, p := range handlers.EnrolmentPaths(mount) {
			if path == p {
				return true
			}
		}
		base := "/v1/auth" + mount.PathPrefix
		// GET /me — the console's boot call; without it the SPA cannot
		// render the shell that hosts the enrolment wizard.
		// GET /me/mfa — the wizard reads factor status before offering
		// enrolment.
		if path == base+"/me" || path == base+"/me/mfa" {
			return true
		}
	}
	return false
}
```

**If `internal/shared/middleware` cannot import `internal/core/auth/handlers` without an import cycle** — check with `go build ./...` before writing the deny — move `EnrolmentPaths` and `RouteMount` to a leaf package both can import (`internal/shared/authroutes`) and re-export from `handlers` so the drift test and the registrars are unchanged. Resolve this in the pre-flight scan, not mid-task.

- [ ] **Step 9: Run the tests**

Run: `go test ./internal/shared/middleware/ ./internal/core/auth/... -count=1`
Expected: PASS.

- [ ] **Step 10: Full gate, then commit**

Run: `make -C /home/tore/orkestra ci-backend` — expected EXIT 0.

```bash
git add backend/internal/core/auth/handlers/mfa_handler.go backend/internal/core/auth/handlers/webauthn_handler.go \
        backend/internal/core/auth/handlers/enrolment_paths_test.go \
        backend/internal/shared/middleware/auth.go backend/internal/shared/middleware/enroll_only_test.go
git commit -F .git/COMMIT_E2
```

```
feat(auth): refuse an enroll_only session everywhere but the enrolment mount

Spec v1.16 §4.12 D39. The check is in RequireAuth, not a per-route gate,
because RequireAuth is the only middleware every protected route passes
through — RequireGlobal is not universal, and the admin MFA-reset group
proves it. The allowlist is derived from handlers.EnrolmentPaths, which
RegisterEnrolmentRoutes now registers from, so the two cannot drift; the
drift test was mutation-checked. Two support paths (GET /me, GET /me/mfa)
are enumerated with their reasons.

Still inert: nothing mints a token with the claim set.

Claude-Session: https://claude.ai/code/session_0155Lt3QwR9zX15oGNKrn1T1
```

---

### Task 3: The enrolment-only mint, and D37 on the password login path

**Files:**
- Modify: `backend/internal/core/auth/models/security.go` — add `TokenResponse.EnrollmentOnly` (`TokenResponse` shares this file with `SecurityContext`, which Task 1 also edits: expect the two commits to touch it)
- Modify: `backend/internal/core/auth/services/password_auth_service.go` (`LoginTokenContext`, `issueTokensForSession`, `completeLogin`)
- Modify: `backend/internal/core/auth/handlers/password_handler.go` (surface the flag; retire the dead 403 mapping)
- Test: `backend/internal/core/auth/services/password_auth_service_enrolment_test.go` (new)

**Interfaces:**
- Consumes: `SecurityContext.EnrollOnly`, `SecurityContext.Lifetime`, `services.MFAEnrolmentProofMaxAge` (Task 1).
- Produces: `authModels.TokenResponse.EnrollmentOnly bool` (JSON `enrollmentOnly`), read by Tasks 4, 6 and 7; `LoginTokenContext.EnrolmentOnly bool`.

- [ ] **Step 1: Write the failing tests — both branches, because the open window must not move**

Create `password_auth_service_enrolment_test.go`. Reuse the existing service-construction helper from `password_auth_service_test.go`; seed a privileged user with **no** factor.

```go
// D37: the deadline has passed. Today this is a refusal; it must become a
// session that can do exactly one thing.
func TestCompleteLogin_GraceExpired_IssuesEnrolmentOnlySession(t *testing.T) {
	svc, users := newLoginTestService(t)
	stamped := time.Now().Add(-60 * 24 * time.Hour) // the staging shape
	users.seed(&iface.User{UUID: "u1", Email: "a@x.io", Role: services.SystemRoleSuperAdmin, MFAGraceStartedAt: &stamped})

	resp, err := svc.Login(context.Background(), LoginInput{Email: "a@x.io", Password: "correct-horse"})
	if err != nil {
		t.Fatalf("login refused a factor-less admin past the deadline: %v", err)
	}
	if resp.AccessToken == "" {
		t.Fatal("no access token: the account still cannot reach the enrolment gate")
	}
	if resp.RefreshToken != "" {
		t.Error("a refresh token was issued — D38 forbids it; a rotation could upgrade the session")
	}
	if !resp.EnrollmentOnly {
		t.Error("enrollmentOnly not set: the SPA cannot tell this from an ordinary login")
	}
	if !resp.MFAEnrollmentRequired {
		t.Error("mfaEnrollmentRequired not set")
	}
	if resp.ExpiresIn > int64(services.MFAEnrolmentProofMaxAge.Seconds()) {
		t.Errorf("ExpiresIn = %d, want <= %v", resp.ExpiresIn, services.MFAEnrolmentProofMaxAge)
	}
}

// The scope boundary the architect set: an OPEN window is untouched.
func TestCompleteLogin_GraceOpen_Unchanged(t *testing.T) {
	svc, users := newLoginTestService(t)
	stamped := time.Now().Add(-24 * time.Hour)
	users.seed(&iface.User{UUID: "u2", Email: "b@x.io", Role: services.SystemRoleAdministrator, MFAGraceStartedAt: &stamped})

	resp, err := svc.Login(context.Background(), LoginInput{Email: "b@x.io", Password: "correct-horse"})
	if err != nil {
		t.Fatalf("login: %v", err)
	}
	if resp.RefreshToken == "" {
		t.Error("an open grace window must still yield a FULL session")
	}
	if resp.EnrollmentOnly {
		t.Error("enrollmentOnly set inside an open window — scope creep past D37")
	}
	if resp.MFAGraceExpiresAt == nil {
		t.Error("the deadline stopped being reported")
	}
}

// No factor, no obligation: an unprivileged user is not touched at all.
func TestCompleteLogin_UnprivilegedNoFactor_Unchanged(t *testing.T) {
	svc, users := newLoginTestService(t)
	users.seed(&iface.User{UUID: "u3", Email: "c@x.io", Role: "user"})

	resp, err := svc.Login(context.Background(), LoginInput{Email: "c@x.io", Password: "correct-horse"})
	if err != nil {
		t.Fatalf("login: %v", err)
	}
	if resp.EnrollmentOnly || resp.MFAEnrollmentRequired {
		t.Error("an unprivileged user was pulled into the enrolment flow")
	}
}
```

- [ ] **Step 2: Run them and watch them fail**

Run: `go test ./internal/core/auth/services/ -run TestCompleteLogin_ -count=1`
Expected: the grace-expired test FAILS (login returns `ErrMFAEnrollmentRequired`); the other two PASS already — that is the point, they are the regression fence.

- [ ] **Step 3: Add the response field**

In the `TokenResponse` struct, after `MFAGraceExpiresAt`:

```go
	// EnrollmentOnly marks a session minted past the grace deadline: it
	// reaches the enrolment endpoints and nothing else, carries no refresh
	// token, and lives MFAEnrolmentProofMaxAge. Clients route straight to
	// the enrolment flow; every other request will answer 403
	// mfa_enrollment_required.
	EnrollmentOnly bool `json:"enrollmentOnly,omitempty"`
```

- [ ] **Step 4: Add the mint branch**

Add to `LoginTokenContext`:

```go
	// EnrolmentOnly mints the restricted session of spec §4.12 D38: no
	// refresh token, no family, and a lifetime equal to the enrolment
	// gate's max age.
	EnrolmentOnly bool
```

In `issueTokensForSession`, set the claim on the security context where `AuthTime` is set:

```go
		AuthTime:   now.Unix(),
		EnrollOnly: in.EnrolmentOnly,
		Lifetime:   enrolmentLifetime(in.EnrolmentOnly),
```

with

```go
// enrolmentLifetime returns the per-mint lifetime override: the enrolment
// gate's max age for a restricted session, zero (meaning "policy default")
// for every other mint.
func enrolmentLifetime(enrolmentOnly bool) time.Duration {
	if enrolmentOnly {
		return MFAEnrolmentProofMaxAge
	}
	return 0
}
```

Then make the refresh half conditional. Replace the `GenerateTokenPairWithAMR` call and the `CreateRefreshToken` block with:

```go
	var accessToken, refreshToken string
	if in.EnrolmentOnly {
		// D38: no refresh token and no family. There is nothing to rotate,
		// so no path exists by which this session becomes a full one, and
		// nothing for a client's rotation logic to race.
		accessToken, err = s.jwtService.GenerateAccessTokenForSessionWithAMR(user, device, security, amr, lastOTPAt)
		if err != nil {
			return nil, err
		}
	} else {
		pair, perr := s.jwtService.GenerateTokenPairWithAMR(user, device, security, amr, lastOTPAt)
		if perr != nil {
			return nil, perr
		}
		accessToken, refreshToken = pair.AccessToken, pair.RefreshToken
		familyID := uuid.New().String()
		if s.refreshTokenRepo == nil {
			return nil, stderrors.New("refresh token persistence is unavailable")
		}
		if err := s.refreshTokenRepo.CreateRefreshToken(ctx, &authModels.RefreshTokenDoc{ /* … unchanged … */ }); err != nil {
			return nil, fmt.Errorf("persist refresh token: %w", err)
		}
	}
```

The session document is still created in **both** branches (edge case 32: these sessions count against the ADR-0017 cap, and `sid` must exist for revocation). The rollback path after `createSessionDoc` fails calls `RevokeTokensBySession`, which is a no-op when no refresh row exists — leave it.

Return:

```go
	return &authModels.TokenResponse{
		AccessToken:  accessToken,
		RefreshToken: refreshToken,
		TokenType:    "Bearer",
		ExpiresIn:    int64(effectiveAccessTTL(ctx, s.jwtService, in.EnrolmentOnly).Seconds()),
		SessionID:    in.SessionID,
		DeviceID:     deviceID,
		User:         s.buildUserResponse(ctx, user),
	}, nil
```

```go
func effectiveAccessTTL(ctx context.Context, j JWTService, enrolmentOnly bool) time.Duration {
	if enrolmentOnly {
		return MFAEnrolmentProofMaxAge
	}
	return j.AccessTokenTTL(ctx)
}
```

- [ ] **Step 5: Replace D37's refusal**

In `completeLogin`, the grace-expired branch:

```go
	// Privileged, no factor → grace logic.
	now := time.Now()
	if s.policy.MFAGraceExpired(ctx, user, now) {
		// D37: the deadline withholds AUTHORITY, not the session. Refusing
		// the login left an account whose grace lapsed with no route back
		// at all — every path to enrolment needs a session, and a sole
		// super_admin has no admin to reset them. The gate (D11 case 4)
		// admits a factor-less caller on a fresh auth_time; this mint is
		// what gives them one to present.
		resp, err := s.issueTokensForSession(ctx, user, LoginTokenContext{
			SessionID: uuid.NewString(), DeviceID: in.DeviceID, DeviceType: "desktop",
			Platform: in.Platform, IPAddress: in.IP, Fingerprint: in.Fingerprint,
			UserAgent: in.UserAgent, LoginMethod: loginMethodFromAMR(sourceAMR),
			MFACompleted: false, EnrolmentOnly: true,
		}, sourceAMR, 0)
		if err != nil {
			return nil, err
		}
		resp.MFAEnrollmentRequired = true
		resp.EnrollmentOnly = true
		return resp, nil
	}
```

Note what is **not** here: no device-trust consultation. The trusted-device shortcut sits in the `hasTOTP || hasWebAuthn` branch above and is unreachable from here, which is correct — a trusted device must not skip an enrolment that has not happened. Do not add one.

- [ ] **Step 6: Retire the dead mapping**

`ErrMFAEnrollmentRequired` now has one producer left, `ValidatePasswordConfirm` (D19). In `password_handler.go`, delete the `case errors.Is(err, services.ErrMFAEnrollmentRequired):` arm that returns the bare 403 — it is unreachable from login and its detail text ("complete MFA setup via an admin") is now false advice. Keep the `ErrPasswordConfirmEnrollmentRequired` arm exactly as it is.

Grep first: `grep -rn "ErrMFAEnrollmentRequired" backend/ --include=*.go`. If any producer other than `password_auth_service.go:1438` survives, stop and resolve it before deleting the arm.

- [ ] **Step 7: Run the tests**

Run: `go test ./internal/core/auth/... -count=1`
Expected: PASS, all three new tests included.

- [ ] **Step 8: Refresh the OpenAPI dump**

Run: `make -C /home/tore/orkestra openapi-dump`
Expected: `backend/openapi/*.json` gains `enrollmentOnly`. `backend-openapi-check` fails the build otherwise.

- [ ] **Step 9: Full gate, then commit**

Run: `make -C /home/tore/orkestra ci-backend` — expected EXIT 0.

```bash
git add backend/internal/core/auth/ backend/openapi/
git commit -F .git/COMMIT_E3
```

```
feat(auth): mint an enrolment-only session instead of refusing the login

Spec v1.16 §4.12 D37/D38. A privileged account with no factor whose grace
window lapsed was refused a token, and every route back to enrolment needs
one — for a sole super_admin there was no way back at all. It now receives
an access token with no refresh cookie and a lifetime equal to the
enrolment gate's max age, so session expiry and proof staleness are one
event, and no rotation can turn it into a full session.

An OPEN grace window is untouched, and two regression tests fence that.
Device trust is not consulted on this path. The login arm mapping
ErrMFAEnrollmentRequired to a bare 403 is deleted with its last producer.

Claude-Session: https://claude.ai/code/session_0155Lt3QwR9zX15oGNKrn1T1
```

---

### Task 4: The OAuth twin

**Files:**
- Modify: `backend/internal/core/auth/services/auth_service.go` (`evaluateMFAForOAuth` and its one caller)
- Modify: `frontend-admin/src/utils/oauthCallbackParams.ts` (carry the flag through the fragment)
- Test: `backend/internal/core/auth/services/auth_service_oauth_enrolment_test.go` (new); `frontend-admin/src/utils/oauthCallbackParams.test.ts`

**Interfaces:**
- Consumes: `TokenResponse.EnrollmentOnly`, `LoginTokenContext.EnrolmentOnly` (Task 3).
- Produces: nothing new; the OAuth path reaches the same response shape as the password path.

- [ ] **Step 1: Write the failing test**

```go
// The OAuth path had the same refusal as the password path and needs the
// same answer — an OAuth-only administrator has no password to fall back on,
// so it is the population MOST exposed to the lockout.
func TestEvaluateMFAForOAuth_GraceExpired_DoesNotRefuse(t *testing.T) {
	svc, users := newOAuthTestService(t)
	stamped := time.Now().Add(-60 * 24 * time.Hour)
	user := &iface.User{UUID: "o1", Email: "o@x.io", Role: services.SystemRoleAdministrator, MFAGraceStartedAt: &stamped}
	users.seed(user)

	resp, handled, err := svc.evaluateMFAForOAuth(context.Background(), user, &authModels.DeviceInfo{DeviceID: "d"}, &authModels.SecurityContext{SessionID: "s"})
	if errors.Is(err, services.ErrMFAEnrollmentRequired) {
		t.Fatal("the OAuth path still refuses a factor-less admin past the deadline")
	}
	if err != nil {
		t.Fatalf("evaluate: %v", err)
	}
	if !handled || resp == nil || !resp.EnrollmentOnly {
		t.Fatalf("handled=%v resp=%+v, want an enrolment-only response", handled, resp)
	}
	if resp.RefreshToken != "" {
		t.Error("a refresh token was issued on the OAuth enrolment-only path")
	}
}
```

- [ ] **Step 2: Run it and watch it fail**

Run: `go test ./internal/core/auth/services/ -run TestEvaluateMFAForOAuth_GraceExpired -count=1`
Expected: FAIL — `ErrMFAEnrollmentRequired`.

- [ ] **Step 3: Change the branch**

In `evaluateMFAForOAuth`, replace `return nil, true, ErrMFAEnrollmentRequired` with a mint that mirrors Task 3's, using the OAuth `sourceAMR` (`[]string{"oauth"}`) and returning `handled=true` with the enrolment-only response. `evaluateMFAForOAuth` does not currently issue tokens — the caller at `auth_service.go:1303` does — so resolve **in the pre-flight scan** whether the mint belongs here (returning `handled=true` with a response) or at the caller (returning a new `enrolmentOnly=true` signal). Prefer whichever keeps `evaluateMFAForOAuth` a decision function if the surrounding code reads that way; the observable behaviour is identical and the test above passes either way once the response reaches the caller's return.

- [ ] **Step 4: Carry the flag to the SPA through the fragment**

`oauthCallbackParams.ts` has an `MFA_KEYS` allowlist (`['requiresMfa', 'mfaToken', 'webauthnAvailable']`) and a strict shape check. Add an enrolment shape beside the MFA one: fragment `enrollmentOnly=true` with an access token, query empty. Read the file's existing shape validation and mirror it exactly — this file exists because a permissive parser is a redirect-injection surface, so do not loosen the strictness.

Add the mirror test in `oauthCallbackParams.test.ts`: a fragment carrying `enrollmentOnly=true` parses; one carrying extra unexpected keys is rejected.

- [ ] **Step 5: Run both suites**

Run: `go test ./internal/core/auth/... -count=1` and `cd frontend-admin && npx vitest run src/utils/oauthCallbackParams.test.ts`
Expected: PASS.

- [ ] **Step 6: Gates, then commit**

Run: `make -C /home/tore/orkestra ci-backend`, then `make -C /home/tore/orkestra ci-frontend-admin`. Both EXIT 0.

```bash
git add backend/internal/core/auth/services/ frontend-admin/src/utils/
git commit -F .git/COMMIT_E4
```

```
feat(auth): give the OAuth login the same enrolment-only answer

Spec v1.16 §4.12 D37. evaluateMFAForOAuth carried the identical refusal,
and an OAuth-only administrator is the population most exposed to it —
there is no password to fall back on. The callback fragment carries
enrollmentOnly through the same strict shape check the MFA keys use.

Claude-Session: https://claude.ai/code/session_0155Lt3QwR9zX15oGNKrn1T1
```

---

### Task 5: Clear the grace stamp on enrolment, announce the login, document the mechanism

**Files:**
- Modify: `backend/internal/core/auth/services/mfa_service.go` (`ConfirmEnrollment`)
- Modify: `backend/internal/core/auth/services/webauthn_service.go` (`FinishRegistration`)
- Modify: `backend/internal/core/auth/services/password_auth_service.go` (the `login_enrollment_only` security event)
- Modify: `backend/internal/core/auth/CLAUDE.md`
- Modify: `docs/site/architecture/authentication-flow.mdx` (claims table)
- Test: `backend/internal/core/auth/services/mfa_service_test.go`, `webauthn_service_test.go`

**Interfaces:**
- Consumes: `iface.UserProvider.ClearMFAGrace(ctx, userUUID) error` — already declared at `pkg/sdk/iface/interfaces.go:114` and implemented at `user_service.go:971`, with **zero callers**. This task is its first.
- Produces: nothing consumed downstream.

- [ ] **Step 1: Write the failing tests**

```go
// D40. The stamp is set on a factor-less privileged login and restarted on
// every removal (I-1). Nothing ever cleared it, so "enrolled" and
// "factor-less" both left a clock running — and a long-enrolled admin who
// removed a factor inherited a deadline that had already passed.
func TestConfirmEnrollment_ClearsGraceStamp(t *testing.T) {
	svc, users := newMFATestService(t)
	stamped := time.Now().Add(-60 * 24 * time.Hour)
	users.seed(&iface.User{UUID: "u1", Role: services.SystemRoleAdministrator, MFAGraceStartedAt: &stamped})

	if _, err := svc.ConfirmEnrollment(context.Background(), "u1", validTOTPCode(t)); err != nil {
		t.Fatalf("confirm: %v", err)
	}
	if got := users.record("u1").MFAGraceStartedAt; got != nil {
		t.Errorf("grace stamp survived a successful enrolment: %v", got)
	}
}

func TestConfirmEnrollment_FailedCode_LeavesStamp(t *testing.T) {
	svc, users := newMFATestService(t)
	stamped := time.Now().Add(-60 * 24 * time.Hour)
	users.seed(&iface.User{UUID: "u2", Role: services.SystemRoleAdministrator, MFAGraceStartedAt: &stamped})

	if _, err := svc.ConfirmEnrollment(context.Background(), "u2", "000000"); err == nil {
		t.Fatal("a wrong code was accepted")
	}
	if users.record("u2").MFAGraceStartedAt == nil {
		t.Error("a FAILED enrolment cleared the deadline")
	}
}

func TestFinishRegistration_ClearsGraceStamp(t *testing.T) { /* the passkey twin of the first test */ }
```

- [ ] **Step 2: Run and watch fail**

Run: `go test ./internal/core/auth/services/ -run "ClearsGraceStamp|LeavesStamp" -count=1`
Expected: the two "clears" tests FAIL; "LeavesStamp" passes already.

- [ ] **Step 3: Call `ClearMFAGrace`**

In `ConfirmEnrollment`, **after** the code has verified and the factor row is persisted, and in `FinishRegistration` after the credential is stored:

```go
	// D40: enrolled means no clock. I-1 restarts the clock on every removal,
	// so clearing it here is what closes the state machine — without it a
	// user who enrols keeps a deadline that already passed, and the next
	// removal inherits it instead of getting a fresh window.
	if s.users != nil {
		if err := s.users.ClearMFAGrace(ctx, userUUID); err != nil && s.logger != nil {
			s.logger.Warn("mfa: clearing the enrolment grace stamp failed; the user keeps a stale deadline",
				slog.String("user_uuid", userUUID), slog.String("error", err.Error()))
		}
	}
```

Best-effort, not fatal: the factor exists at this point, and failing the enrolment because a bookkeeping write failed would put the user back in the hole this PR is digging them out of. The WARN is the signal.

- [ ] **Step 4: Emit `login_enrollment_only`**

In the D37 branch added in Task 3, emit a security event through the same sink the other login events use (find the emit helper by content — `recordSecurityEvent` / `SecurityEventSink` in `password_auth_service.go`). Type `login_enrollment_only`, metadata `{"audience": …, "sessionId": …, "graceStartedAt": …}`. Add its row to `authEventComplianceAction` if that mapping is exhaustive — check, do not assume.

- [ ] **Step 5: Run the tests**

Run: `go test ./internal/core/auth/... -count=1`
Expected: PASS.

- [ ] **Step 6: Documentation, same commit**

`backend/internal/core/auth/CLAUDE.md` — a new section, placed with the enrolment-gate material, covering: what an enrolment-only session is; that it has no refresh token and why; the derived allowlist and where to add a route so it is reachable; that enrolling clears the stamp and the user then signs in again (**and why a full pair is not minted** — an enrolled user holding `amr: ["pwd"]` fails `RequireMFA` on the authz and tenant routes); and that grace-expired is the only trigger.

`docs/site/architecture/authentication-flow.mdx` — the JWT claims table gains `enroll_only` beside `auth_time` and `mfae`.

Both must be true **of the final tree**, not of this commit — doc sentences falsified by a later task were the dominant defect class on PR B (three occurrences). Task 6 changes SPA behaviour these sentences describe; write them for the end state.

- [ ] **Step 7: Full gate, then commit**

Run: `make -C /home/tore/orkestra ci-backend` — EXIT 0. Note: if the security-event write is a new Mongo call site, `backend-tenantscope` will demand a `//tenantscope:allow` and a baseline regeneration; the gate tells you.

```bash
git add backend/internal/core/auth/ docs/site/architecture/authentication-flow.mdx
git commit -F .git/COMMIT_E5
```

```
feat(auth): clear the grace stamp on enrolment and announce the restricted login

Spec v1.16 §4.12 D40/D41. ClearMFAGrace had zero callers despite its doc
comment; it now has one on each enrolment path, best-effort, so "enrolled"
means no clock and the next removal gets a fresh window instead of an
inherited expired one. A failed enrolment leaves the stamp, and a test
fences that. The restricted login emits login_enrollment_only so the
operator timeline shows the unusual shape rather than an ordinary sign-in.

auth/CLAUDE.md documents the mechanism end to end, including why a
successful enrolment does NOT mint a full pair.

Claude-Session: https://claude.ai/code/session_0155Lt3QwR9zX15oGNKrn1T1
```

---

### Task 6: Operator console — route the restricted login, and finish it honestly

**Files:**
- Modify: `frontend-admin/src/store/api/authApi.ts` (login response type, `:95-104`)
- Modify: `frontend-admin/src/hooks/auth/useAuthRTK.ts` (`:127` handles `requiresMfa` today)
- Modify: `frontend-admin/src/components/authentication/Login.tsx`
- Modify: the MFA enrolment wizard (find by content: `MfaEnrollWizard`), backup-code step
- Test: `frontend-admin/src/hooks/auth/useAuthRTK.test.ts(x)`, the wizard's test file

**Interfaces:**
- Consumes: `enrollmentOnly` on the login response (Task 3), `403 mfa_enrollment_required` from any other call (Task 2).
- Produces: nothing consumed by later tasks.

- [ ] **Step 1: Write the failing tests**

```ts
it('routes an enrollmentOnly login to the MFA enrolment flow, not the dashboard', async () => {
  server.use(http.post('*/v1/auth/operator/login', () => HttpResponse.json({
    accessToken: 'a.b.c', tokenType: 'Bearer', expiresIn: 300,
    mfaEnrollmentRequired: true, enrollmentOnly: true,
    user: { uuid: 'u1', email: 'a@x.io', role: 'super_admin' }
  })));
  // …render the login form, submit…
  await waitFor(() => expect(navigate).toHaveBeenCalledWith(expect.stringContaining('/mfa')));
  expect(navigate).not.toHaveBeenCalledWith('/dashboard');
});

it('does not sign the operator out when another call answers 403 mfa_enrollment_required', async () => {
  // A 403 is not a 401; the session is alive and the user is mid-enrolment.
  // Regression fence: signing out here would recreate the lockout in the UI.
});

it('shows the backup codes and requires acknowledgement before signing out', async () => {
  // …confirm succeeds in an enrolment-only session…
  expect(screen.getByTestId('backup-codes')).toBeInTheDocument();
  expect(navigate).not.toHaveBeenCalledWith(expect.stringContaining('/login'));
  await user.click(screen.getByRole('button', { name: /saved (them|these)/i }));
  await waitFor(() => expect(navigate).toHaveBeenCalledWith(expect.stringContaining('/login')));
});
```

- [ ] **Step 2: Run and watch fail**

Run: `cd frontend-admin && npx vitest run src/hooks/auth`
Expected: FAIL — nothing reads `enrollmentOnly`; the console currently declares `mfaEnrollmentRequired` on the type at `authApi.ts:103` and acts on neither.

- [ ] **Step 3: Add the field to the response type**

In `authApi.ts`, beside `mfaEnrollmentRequired` and `mfaGraceExpiresAt`:

```ts
  /**
   * The grace deadline has passed: this session reaches the MFA enrolment
   * endpoints and nothing else, carries no refresh cookie, and expires in
   * ~5 minutes. Route straight to enrolment — every other request will
   * answer 403 mfa_enrollment_required.
   */
  enrollmentOnly?: boolean;
```

- [ ] **Step 4: Route on it**

In `useAuthRTK.ts`, beside the existing `if (result.requiresMfa)` branch at `:127`, add an `enrollmentOnly` branch that stores the access token and navigates to the enrolment route rather than the post-login destination. `requiresMfa` and `enrollmentOnly` are mutually exclusive — a user with a factor never gets the second — but assert that ordering explicitly rather than relying on it.

The enrolment screen must **suppress the navigation shell**: every other request 403s, so a rendered sidebar produces a wall of failed queries and error toasts. Reuse whatever bare layout the login and MFA-verify screens already use — read `LoginMfaVerify.tsx` for the precedent before inventing one.

- [ ] **Step 5: Finish the flow honestly**

After a successful `enroll/confirm` **in an enrolment-only session** (deviation 1): show the backup codes, require an explicit acknowledgement, then clear the session and navigate to `/login` with a message along the lines of "Second factor enrolled. Sign in with it to continue." Do **not** skip the acknowledgement — the backup codes are shown once and the sign-out would discard them.

An ordinary session's enrolment is unchanged: no sign-out, no redirect.

- [ ] **Step 6: Run the tests**

Run: `cd frontend-admin && npx vitest run && npm run typecheck && npm run lint`
Expected: PASS / 0 errors.

- [ ] **Step 7: Gate, then commit**

Run: `make -C /home/tore/orkestra ci-frontend-admin` — EXIT 0. (Never alongside another vitest run.)

```bash
git add frontend-admin/src
git commit -F .git/COMMIT_E6
```

```
feat(admin): route an enrolment-only login to the MFA wizard

Spec v1.16 §4.12 D42. The login response's enrollmentOnly flag sends the
operator straight to enrolment with the nav shell suppressed, because every
other request answers 403 mfa_enrollment_required by design. A 403 is not a
sign-out and a test fences that.

Per the plan's deviation 1, a successful enrolment does not mint a full
pair: the wizard shows the backup codes, waits for acknowledgement, then
sends the user to sign in with the factor they just created — which is also
the cheapest possible moment to discover a mis-scanned secret.

Claude-Session: https://claude.ai/code/session_0155Lt3QwR9zX15oGNKrn1T1
```

---

### Task 7: Client SPA — the same routing on the Tier-2 surface

**Files:**
- Modify: `frontend-client/src/api/auth.ts` (`:177-178`, `:196-197`, `:232-233` already carry `mfaEnrollmentRequired` / `mfaGraceExpiresAt`)
- Modify: `frontend-client/src/pages/…/MfaEnrolPage.tsx`
- Test: the corresponding vitest files

**Interfaces:**
- Consumes: `enrollmentOnly` (Task 3). A client user reaches this path when a membership gives them `org_owner` or `org_admin`, which `RoleRequiresMFA` treats as privileged.
- Produces: nothing.

- [ ] **Step 1: Write the failing test**

Mirror Task 6's first test against the client login helper: an `enrollmentOnly` response routes to `/account/security/mfa` and not to the account home.

- [ ] **Step 2: Run and watch fail**

Run: `cd frontend-client && npx vitest run`
Expected: FAIL.

- [ ] **Step 3: Thread the field**

Add `enrollmentOnly?: boolean` to both response shapes in `api/auth.ts` and to the mapping at `:232-233`, then route on it. The client SPA is deliberately modal-free (`authedFetch.ts:23-33`) — do not add a modal; a redirect is the whole mechanism.

- [ ] **Step 4: Same finish as the console**

Backup codes, acknowledgement, sign out, return to login.

- [ ] **Step 5: Run, gate, commit**

Run: `cd frontend-client && npx vitest run && npm run typecheck && npm run lint`, then `make -C /home/tore/orkestra ci-frontend-client` — EXIT 0.

```bash
git add frontend-client/src
git commit -F .git/COMMIT_E7
```

```
feat(client): route an enrolment-only login to the MFA enrolment page

Spec v1.16 §4.12 D42, client tier. A client user reaches this path through
an org_owner or org_admin membership, which RoleRequiresMFA treats as
privileged. Redirect only — the client SPA stays modal-free by design.

Claude-Session: https://claude.ai/code/session_0155Lt3QwR9zX15oGNKrn1T1
```

---

### Task 8: `errquality` R4 — the rule, its tests, and the backlog frozen

**Files:**
- Modify: `backend/tools/errquality/analyzer.go`
- Modify: `backend/tools/errquality/analyzer_test.go`
- Modify: `backend/tools/errquality/baseline.txt`
- Modify: `backend/tools/errquality/CLAUDE.md`
- Create: `backend/tools/errquality/testdata/src/r4/r4.go` (follow the layout the existing rule tests use — read `analyzer_test.go` first)

**Interfaces:**
- Consumes: nothing.
- Produces: rule `R4`, reported at `huma.Error401Unauthorized` call sites. Tasks 9 and 10 burn its baseline down.

- [ ] **Step 1: Write the failing analyzer tests**

```go
func TestR4_FlagsBareHuma401(t *testing.T) {
	// want: "[R4] a 401 that is a verdict on the request carries no code"
}

func TestR4_AcceptsErrcodeUnauthorized(t *testing.T) {
	// errcode.Unauthorized(errcode.AuthInvalidCredentials, "…") → no diagnostic
}

func TestR4_AllowCommentSilences(t *testing.T) {
	// //errquality:allow RequireAuth's codeless 401 is load-bearing (D44)
}

func TestR4_BaselineSilences(t *testing.T) {
	// a relpath:line:R4 entry suppresses exactly that site
}

// The scope boundary, pinned: only 401 drives the console's rotation arm.
func TestR4_DoesNotFlag400Or403(t *testing.T) {
	// huma.Error400BadRequest / huma.Error403Forbidden → no diagnostic
}
```

- [ ] **Step 2: Run and watch fail**

Run: `go test ./tools/errquality/ -run TestR4 -count=1`
Expected: FAIL — no R4 diagnostics are produced.

- [ ] **Step 3: Add the rule**

In `analyzer.go`, extend the package doc comment with R4 (matching the R1–R3 style), then add the predicate inside `inspectFile` beside the R1/R2 reports:

```go
	// R4 — a 401 built with the bare huma constructor carries no top-level
	// code, and a client cannot tell a verdict on the request from a dead
	// credential. frontend-admin's 401 handler rotates the refresh cookie on
	// a codeless 401 because that is what a JWT signing-key rotation looks
	// like; 26 codeless 401s in 13 s once cost an operator their session.
	// Build it with errcode.Unauthorized(code, detail).
	//
	// 401 ONLY. A bare 400 or 403 is a legibility problem, not a session
	// hazard, and widening this rule would produce a baseline nobody reads.
	if isHumaCall(call, "Error401Unauthorized") {
		report(call.Pos(), "R4",
			"a 401 that is a verdict on the request carries no code — build it with errcode.Unauthorized(code, detail); see backend/CLAUDE.md#error-code-contract")
	}
```

Reuse the existing helper that recognises `huma.ErrorNNN…` calls if there is one; R3 already needs to tell 4xx builders from 5xx ones (`analyzer.go:70`), so read that first rather than writing a second matcher.

- [ ] **Step 4: Run the rule tests**

Run: `go test ./tools/errquality/ -run TestR4 -count=1`
Expected: PASS.

- [ ] **Step 5: Freeze the 74**

Generate the baseline rows with the recipe the file's own header documents, filtered to R4, and append them:

```bash
cd backend && go run ./tools/errquality/cmd/errquality ./internal/... 2>&1 \
  | sed -n 's#^.*/\(internal/[^:]*\):\([0-9]*\):[0-9]*: \[\(R4\)\].*#\1:\2:\3#p' \
  | sort -u >> tools/errquality/baseline.txt
```

Sanity-check the count: **74 R4 rows**, no fewer. If the number differs, the rule's predicate is wrong — find out why before continuing, because a rule that misses sites is worse than no rule.

- [ ] **Step 6: Document R4**

`backend/tools/errquality/CLAUDE.md` gains R4 with its rationale and the 401-only scope. State plainly that the baseline rows are temporary and Tasks 9–10 delete them.

- [ ] **Step 7: Full gate, then commit**

Run: `make -C /home/tore/orkestra ci-backend` — EXIT 0 (the baseline is what keeps it green).

```bash
git add backend/tools/errquality/
git commit -F .git/COMMIT_E8
```

```
feat(errquality): add R4 — a 401 that is a verdict must carry a code

Spec v1.16 §4.13 D45. backend/CLAUDE.md has mandated this since the
error-code contract was written, and 74 bare huma.Error401Unauthorized
sites accumulated under it, because a convention is not a control. R4 is
one predicate in the analyzer that already owns error quality, reusing its
baseline and its //errquality:allow escape.

401 only: a bare 400 or 403 is a legibility problem, not a session hazard,
and a test pins that boundary. The 74 known sites are frozen here so the
rule is live against NEW code for the whole sweep; the next two commits
burn the list down to nothing.

Claude-Session: https://claude.ai/code/session_0155Lt3QwR9zX15oGNKrn1T1
```

---

### Task 9: Name the 24 verdicts

**Files:**
- Modify: `backend/internal/shared/errcode/codes.go` and `codes_test.go` (`goldenCodes`)
- Modify: `backend/internal/core/auth/handlers/{mfa_handler,webauthn_handler,auth_handler,service_token_handler}.go`
- Modify: `backend/tools/errquality/baseline.txt` (remove the rows fixed here)
- Test: `backend/internal/core/auth/handlers/*_test.go`

**Interfaces:**
- Consumes: R4 (Task 8).
- Produces: seven new `errcode` consts, listed below. Task 10 adds one more.

- [ ] **Step 1: Write the failing handler tests**

The one that matters most, because it is the closest surviving twin of the defect PR B fixed — `/mfa/verify` is reachable with a live bearer and is **not** in the console's `AUTH_ENDPOINT_PATHS` (that list holds `mfa/login/verify`, which does not match it):

```go
func TestMFAVerify_UserMissing_CarriesCode(t *testing.T) {
	// claims for a user the repo no longer resolves
	rec := call(t, h.Verify, deletedUserClaims(t))
	if rec.Status != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Status)
	}
	if rec.Code != errcode.AuthSessionUserMissing {
		t.Errorf("code = %q, want %q — a codeless 401 here makes the console rotate the refresh cookie", rec.Code, errcode.AuthSessionUserMissing)
	}
}
```

Add the same shape for: the MFA challenge verdicts, the WebAuthn login-challenge verdicts, the three refresh verdicts, the two ID-token verdicts, and the service-token credential verdict.

- [ ] **Step 2: Run and watch fail**

Run: `go test ./internal/core/auth/handlers/ -count=1`
Expected: FAIL — the codes are empty.

- [ ] **Step 3: Declare the codes**

In `codes.go`, each with the doc-comment style its neighbours use:

```go
const AuthSessionUserMissing = "auth.session_user_missing"
const AuthMFAChallengeInvalid = "auth.mfa_challenge_invalid"
const AuthRefreshTokenMissing = "auth.refresh_token_missing"
const AuthRefreshTokenInvalid = "auth.refresh_token_invalid"
const AuthRefreshTokenReplay = "auth.refresh_token_replay"
const AuthIDTokenInvalid = "auth.id_token_invalid"
const AuthClientCredentialsInvalid = "auth.client_credentials_invalid"
```

**Add a `goldenCodes` row for each in `codes_test.go` in this same commit** — `TestEveryConstSnapshotted` fails otherwise, and that failure is the gate doing its job.

`"invalid or expired login challenge"` (WebAuthn, 4 sites) reuses the **existing** `AuthWebAuthnChallengeInvalid`; do not declare a second code for it.

- [ ] **Step 4: Convert the 24 sites**

Replace `huma.Error401Unauthorized(detail)` with `errcode.Unauthorized(code, detail)`, keeping each detail sentence as written unless it is misleading.

⚠️ **Three sites pass a wrapped error as a second argument** — `huma.Error401Unauthorized("Invalid refresh token", err)`, the replay one, and the two ID-token ones. `errcode.Unauthorized` takes no error. Do **not** fold the error text into the detail (that is R1, and it leaks internals). Log it instead, at the level the surrounding code uses, and let the client see only the sentence.

`auth_handler.go:303` returns a dynamic `response.humaDetail`. Read what produces it before assigning a code; if it can carry more than one situation, either give the producer the code or leave the site with an `//errquality:allow` naming the reason — and say which in the commit message.

- [ ] **Step 5: Drop the fixed rows from the baseline**

Remove exactly the R4 rows for the files touched here. Do not regenerate the whole file — a regeneration would silently re-freeze anything newly broken, which is what the baseline header warns against.

- [ ] **Step 6: Run everything**

Run: `go test ./internal/... ./tools/errquality/... -count=1`
Expected: PASS.

- [ ] **Step 7: Full gate, then commit**

Run: `make -C /home/tore/orkestra ci-backend` — EXIT 0.

```bash
git add backend/internal/shared/errcode/ backend/internal/core/auth/handlers/ backend/tools/errquality/baseline.txt
git commit -F .git/COMMIT_E9
```

```
fix(auth): give every verdict 401 a code

Spec v1.16 §4.13 D43. Twenty-four call sites answered 401 as a verdict on
the request with no top-level code, which is indistinguishable to the
operator console from a JWT signing-key rotation — the collision that cost
an operator their session on staging. /mfa/verify is the sharpest of them:
reachable with a live bearer and absent from the console's
AUTH_ENDPOINT_PATHS, i.e. the closest surviving twin of the defect PR B
fixed.

The three sites that wrapped an error now log it instead of shipping it to
the client; folding it into the detail would trade one defect for an R1.

Claude-Session: https://claude.ai/code/session_0155Lt3QwR9zX15oGNKrn1T1
```

---

### Task 10: Name the 50 guards, empty the baseline, finish the docs

**Files:**
- Modify: `backend/internal/shared/errcode/codes.go` + `codes_test.go` (one more const)
- Modify: the remaining files with bare 401s — `setup/routes.go`, `user/handlers/avatar_handler.go`, `authz/handlers/handler.go`, `tenant/handlers/handler.go`, `compliance/handlers/{me_handler,erasure_request_handler}.go`, the rest of `auth/handlers/*`
- Modify: `backend/tools/errquality/baseline.txt` (empty of R4)
- Modify: `backend/CLAUDE.md`, `docs/site/sdk/build-your-first-addon.mdx`

**Interfaces:**
- Consumes: R4 (Task 8), the code style of Task 9.
- Produces: `errcode.AuthAuthenticationRequired`; a baseline with zero R4 rows.

- [ ] **Step 1: Write the failing test**

```go
// The rule is only a control once nothing is exempt from it.
func TestBaselineHasNoR4Rows(t *testing.T) {
	b, err := os.ReadFile("baseline.txt")
	if err != nil {
		t.Fatal(err)
	}
	for i, line := range strings.Split(string(b), "\n") {
		if strings.HasSuffix(strings.TrimSpace(line), ":R4") {
			t.Errorf("baseline.txt:%d still exempts a bare 401: %s", i+1, line)
		}
	}
}
```

Put it in `tools/errquality/analyzer_test.go`.

- [ ] **Step 2: Run and watch fail**

Run: `go test ./tools/errquality/ -run TestBaselineHasNoR4 -count=1`
Expected: FAIL with ~50 rows still listed.

- [ ] **Step 3: Declare the guard code**

```go
// AuthAuthenticationRequired is the answer when a handler finds no
// authentication context. On a route behind RequireAuth this is
// unreachable — RequireAuth rejected before dispatch — but the code costs
// nothing and is correct on the optional-auth mounts where the guard does
// fire. Converting these to 500 was considered and rejected: it needs
// per-site proof of unreachability across 50 call sites in seven packages.
const AuthAuthenticationRequired = "auth.authentication_required"
```

Plus its `goldenCodes` row, same commit.

- [ ] **Step 4: Convert the ~50 guards**

Mechanical: `huma.Error401Unauthorized("authentication required")` → `errcode.Unauthorized(errcode.AuthAuthenticationRequired, "Authentication required")`, and likewise for `"not authenticated"` and `"Invalid authentication context"`. Normalise the detail sentence while you are there; three spellings of one situation is how the triage predicate had to exist at all.

Do **not** touch `internal/shared/middleware/auth.go`'s `errors.AuthenticationError(...)` calls. Those are the middleware envelope path, R4 does not see them, and `RequireAuth`'s codeless 401 is load-bearing (D44).

- [ ] **Step 5: Empty the R4 baseline**

Delete every remaining `:R4` row. The R1/R3 rows stay.

- [ ] **Step 6: Pin D44**

Add, in `internal/shared/middleware/` alongside the other middleware tests:

```go
// D44. RequireAuth's rejection of an unparseable bearer must stay CODELESS:
// frontend-admin's third 401 arm rotates the token on a codeless 401
// precisely because that is what a signing-key rotation looks like, and
// coding this would restore a ~14.5-minute window of silently failing
// requests after any key change. Asserting an ABSENCE is deliberate — this
// is the invariant a well-meaning future sweep breaks.
func TestRequireAuth_InvalidBearer_StaysCodeless(t *testing.T) {
	rec := doRequest(t, "Bearer not-a-jwt", "/v1/admin/users")
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
	if code := bodyCode(t, rec); code != "" {
		t.Errorf("code = %q, want none — see spec v1.16 §4.13 D44", code)
	}
}

// …and the expired case still names itself, which is the other half.
func TestRequireAuth_ExpiredBearer_CarriesAccessTokenExpired(t *testing.T) { /* … */ }
```

- [ ] **Step 7: Documentation, same commit**

`backend/CLAUDE.md` — the error-code contract section records that R4 enforces the 401 rule mechanically **and** that `RequireAuth`'s codeless 401 is the deliberate exception, with the reason. Without that sentence the next reader files the exception as a bug.

`docs/site/sdk/build-your-first-addon.mdx` — one paragraph: a handler's 401 needs an errcode, and CI enforces it, so a fork author meets R4 in review rather than in a red pipeline.

- [ ] **Step 8: Verify the sweep is actually complete**

```bash
cd backend && grep -rn "huma.Error401Unauthorized" --include=*.go . | grep -v _test.go | wc -l
```

Expected: **0** outside test files. If not zero, every survivor must carry an `//errquality:allow` with a written reason, and the commit message must name them.

- [ ] **Step 9: Full gate, then commit**

Run: `make -C /home/tore/orkestra ci-backend` — EXIT 0.

```bash
git add backend/ docs/site/sdk/build-your-first-addon.mdx
git commit -F .git/COMMIT_E10
```

```
fix(auth): code the remaining 401 guards and empty the R4 baseline

Spec v1.16 §4.13 D43/D44/D46. The 50 pre-dispatch guards were never the
hazard — they fire only when RequireAuth already rejected, which the
console reads correctly — but a code is correct either way and it lets the
R4 baseline reach zero, which is what turns the rule from a backlog into a
control. A test now fails if any :R4 row reappears.

RequireAuth's own codeless 401 is untouched and is now pinned by a test
that asserts the ABSENCE of a code, because it is exactly the invariant a
future sweep would "fix". backend/CLAUDE.md records why.

Claude-Session: https://claude.ai/code/session_0155Lt3QwR9zX15oGNKrn1T1
```

---

## Final verification (before opening the PR)

- [ ] `make -C /home/tore/orkestra ci` — every changed surface, EXIT 0
- [ ] `grep -rn "huma.Error401Unauthorized" backend --include=*.go | grep -v _test.go | wc -l` → `0`
- [ ] `grep -c ":R4" backend/tools/errquality/baseline.txt` → `0`
- [ ] `git diff --check origin/dev..HEAD`
- [ ] **Whole-branch review question, asked explicitly:** is any sentence in any `CLAUDE.md` or `docs/site` page false *of the final tree*? Doc truth falsified by a later task was the dominant defect class on PR B, three times over.
- [ ] **Staging drill** (spec §7, PR E row), against a rebuilt binary — AIR will not rebuild for you, and staging silently served a two-day-old binary once:
  1. take an operator with zero factors, set `mfaGraceStartedAt` 60 days back
  2. sign in → an enrolment-only session, **not** a 403
  3. any admin call on that session → 403 `mfa_enrollment_required`
  4. `enroll/begin` + `confirm` → backup codes, then the sign-out
  5. sign in again → an ordinary MFA login, and the stamp is gone from Mongo
  6. 26 wrong MFA codes in 13 s against `/mfa/verify` → **zero** `/refresh-cookie` calls, session survives (the measurement PR B's fix was verified with)
  7. clean up every document the drill created — the PR C drill left 23 across 8 collections

## Self-review notes (done; recorded so a reviewer can check the same things)

- **Spec coverage.** D37 → Task 3 (+4 for OAuth); D38 → Tasks 1, 3; D39 → Task 2; D40 → Task 5 *with deviation 1 on the "full pair" half*; D41 → Tasks 3, 5; D42 → Tasks 6, 7; D43 → Tasks 9, 10; D44 → Task 10 step 6 (+ the R4 scope in Task 8); D45 → Task 8; D46 → Task 10 step 7. Edge cases 32–37: 32 (session doc created in both branches, Task 3 step 4), 33 (Task 2 drift test), 34 (Task 1 lifetime override), 35 (Task 1 omitted-when-false test), 36 (Task 10 step 6), 37 (Task 8 allow-comment test).
- **Type consistency.** `EnrollOnly` is the Go field on both `JWTClaims` and `SecurityContext`; `enroll_only` is the JWT claim; `EnrolmentOnly` is the `LoginTokenContext` field and the plan's prose spelling; `EnrollmentOnly` / `enrollmentOnly` is the API response field and the TypeScript name. The mixed spelling is deliberate — Go claim structs in this repo use `Enroll…`, the spec prose uses "enrolment" — but it is exactly the kind of thing that drifts, so every occurrence above is intentional and should be checked, not normalised on sight.
- **Known open question for the pre-flight scan:** whether `internal/shared/middleware` may import `internal/core/auth/handlers` for `EnrolmentPaths` without an import cycle (Task 2 step 8 carries the fallback), and whether Task 4's mint belongs in `evaluateMFAForOAuth` or at its caller.
