# Authentication and Authorization Hardening Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Eliminate auth secret/PII logging, reject inactive users on every token-issuance path, make JWT and persisted session identifiers identical, and expose degraded Redis revocation safely.

**Architecture:** Keep the existing auth, authz, MongoDB, Redis, and middleware boundaries. Centralize full-token eligibility in the auth service and require production token issuance to receive one caller-generated session UUID through `SecurityContext`; refresh and step-up preserve that UUID. Remove unconditional debug output, add bounded revocation-store telemetry, and document the required Apple-key incident response.

**Tech Stack:** Go 1.25.13, Huma v2, Chi, MongoDB 8.0, Redis 8.2, `golang-jwt/jwt/v5`, Prometheus client, `testify`

**Spec:** `docs/superpowers/specs/2026-08-18-authentication-authorization-hardening-design.md`

## Global Constraints

- Preserve RS256 issuer/type/expiry/audience validation and the operator/client host split.
- Preserve refresh-token hashing, atomic CAS rotation, family replay detection, and tier-specific repositories.
- Do not weaken `RequireAuth`, tenant-kind, permission, MFA, or step-up middleware.
- Do not add MongoDB reads to every authenticated request.
- Never log credentials, PEM data, OAuth tokens, PII, provider IDs, session IDs, device IDs, fingerprints, or raw provider metadata.
- Redis revocation lookup remains fail-open, bounded by the access-token TTL, but failures must be observable.
- Add no dependency and no analyzer baseline exemption.
- Every commit in this commons branch must contain exactly one `Prop: upstream` trailer.

---

### Task 1: Remove secret and PII debug logging

**Files:**
- Create: `backend/internal/core/auth/services/logging_safety_test.go`
- Modify: `backend/internal/core/auth/services/apple_oauth_service.go`
- Modify: `backend/internal/core/auth/services/auth_service.go`
- Modify: `backend/internal/core/auth/repository/oauth_provider_repository.go`

**Interfaces:**
- Consumes: module-scoped `*slog.Logger` already available to auth construction.
- Produces: unchanged provider/repository constructor signatures and production auth packages with no direct `fmt.Print*` calls or debug markers.

- [ ] **Step 1: Write the source-scan regression test**

Create `logging_safety_test.go` in package `services_test`. Resolve `../` from the test file, walk production `.go` files in `services` and `repository`, skip `_test.go`, and fail on these patterns:

```go
var forbiddenAuthLogPatterns = []*regexp.Regexp{
	regexp.MustCompile(`\[(?:APPLE|AUTH|REPO)_DEBUG\]`),
	regexp.MustCompile(`fmt\.(?:Print|Printf|Println)\s*\(`),
}

func TestProductionAuthCodeContainsNoDirectDebugPrinting(t *testing.T) {
	// filepath.WalkDir over services/ and ../repository/.
	// For each production Go file, assert every pattern has no match.
}
```

- [ ] **Step 2: Run the test and confirm the current leak is detected**

Run:

```bash
cd backend
go test ./internal/core/auth/services -run TestProductionAuthCodeContainsNoDirectDebugPrinting -count=1
```

Expected: FAIL naming `apple_oauth_service.go`, `auth_service.go`, and `oauth_provider_repository.go`.

- [ ] **Step 3: Remove direct debug printing and sanitize useful events**

Delete every `APPLE_DEBUG`, `AUTH_DEBUG`, and `REPO_DEBUG` call and remove now-unused `fmt` imports where formatting is not otherwise required. Do not replace per-record diagnostic chatter. Preserve only security-relevant outcome events through an injected logger, for example:

```go
logger.WarnContext(ctx, "oauth provider operation failed",
	slog.String("provider", string(provider)),
	slog.String("operation", "link"),
	slog.String("error_category", "repository_unavailable"),
)
```

Never attach `email`, `providerID`, UUIDs, device fields, tokens, ciphertext, `AdditionalConfig`, PEM strings, secret paths, raw metadata, or raw HTTP response bodies. Apple key parsing and the OAuth repository do not need replacement log statements; their callers already receive errors.

- [ ] **Step 4: Add a focused Apple-construction capture test**

Construct an Apple provider with a recognizable invalid secret such as `DO-NOT-LOG-APPLE-PRIVATE-KEY`, temporarily capture `os.Stdout` through a pipe, and assert the marker and `private_key` key name are absent. The constructor must still return the existing parse error. Restore `os.Stdout` with `t.Cleanup` even when the assertion fails.

- [ ] **Step 5: Run focused tests**

Run:

```bash
cd backend
go test ./internal/core/auth/services ./internal/core/auth/repository -run 'TestProductionAuthCodeContainsNoDirectDebugPrinting|TestApple.*DoesNotLog' -count=1
```

Expected: PASS.

- [ ] **Step 6: Commit the logging fix**

```bash
git add backend/internal/core/auth/services/logging_safety_test.go \
  backend/internal/core/auth/services/apple_oauth_service.go \
  backend/internal/core/auth/services/auth_service.go \
  backend/internal/core/auth/repository/oauth_provider_repository.go
git commit -m "fix(auth): remove sensitive debug logging" -m "Prop: upstream"
```

### Task 2: Centralize inactive-account token eligibility

**Files:**
- Create: `backend/internal/core/auth/services/oauth_inactive_user_test.go`
- Modify: `backend/internal/core/auth/services/auth_service.go`
- Modify: `backend/internal/core/auth/handlers/error_mapping_test.go`
- Modify: `backend/internal/core/auth/handlers/auth_handler.go`

**Interfaces:**
- Consumes: `*iface.User` loaded by password, OAuth, mobile, MFA, and refresh flows.
- Produces: unexported `validateTokenEligibleUser(user *iface.User) error` returning `ErrInvalidCredentials` for nil, empty-UUID, or inactive users; OAuth handlers map it to their existing neutral authentication failure.

- [ ] **Step 1: Write failing service tests for both OAuth lookup branches**

Use the existing fakes from `gates_fakes_test.go` to cover:

```go
func TestHandleOAuthCallbackWithLinking_RejectsInactiveLinkedUser(t *testing.T) { /* provider link resolves inactive user; expect ErrInvalidCredentials; assert no refresh row */ }
func TestHandleOAuthCallbackWithLinking_RejectsInactiveEmailMatchedUser(t *testing.T) { /* no link, email lookup resolves inactive user; expect ErrInvalidCredentials; assert no provider write and no refresh row */ }
```

Also add a table test that calls `GenerateEnhancedTokenPair` with nil, empty UUID, and inactive users and asserts no JWT method or persistence fake was invoked.

- [ ] **Step 2: Run the tests and verify token issuance currently succeeds**

Run:

```bash
cd backend
go test ./internal/core/auth/services -run 'TestHandleOAuthCallbackWithLinking_RejectsInactive|TestGenerateEnhancedTokenPair_RejectsIneligible' -count=1
```

Expected: FAIL because the inactive user reaches token generation.

- [ ] **Step 3: Add the central eligibility guard**

Add near the auth-service issuance code:

```go
func validateTokenEligibleUser(user *iface.User) error {
	if user == nil || user.UUID == "" || !user.IsActive {
		return ErrInvalidCredentials
	}
	return nil
}
```

Call it at the first line of `GenerateEnhancedTokenPair`, before MFA evaluation, token signing, provider updates, session writes, or refresh writes. Keep the existing password and refresh checks for flow-specific audit behavior. Call the guard before any full-token minting helper that can be invoked without `GenerateEnhancedTokenPair`, including read-only access minting from refresh and MFA completion.

- [ ] **Step 4: Preserve a neutral OAuth response**

Update the existing handler mapping so `ErrInvalidCredentials` from OAuth web or mobile returns the same status/code/detail as invalid OAuth authentication. Add a table row to `error_mapping_test.go` and assert the body does not contain `inactive`, the email, or the user UUID.

- [ ] **Step 5: Run service and handler tests**

Run:

```bash
cd backend
go test ./internal/core/auth/services ./internal/core/auth/handlers -run 'Inactive|Ineligible|ErrorMapping' -count=1
```

Expected: PASS.

- [ ] **Step 6: Commit the eligibility invariant**

```bash
git add backend/internal/core/auth/services/oauth_inactive_user_test.go \
  backend/internal/core/auth/services/auth_service.go \
  backend/internal/core/auth/handlers/error_mapping_test.go \
  backend/internal/core/auth/handlers/auth_handler.go
git commit -m "fix(auth): reject inactive OAuth users" -m "Prop: upstream"
```

### Task 3: Require explicit session identity in JWT issuance

**Files:**
- Modify: `backend/internal/core/auth/services/jwt_service.go`
- Modify: `backend/internal/core/auth/services/jwt_helpers_test.go`
- Modify: `backend/internal/core/auth/services/password_auth_service.go`
- Create: `backend/internal/core/auth/services/session_identity_test.go`

**Interfaces:**
- Consumes: a non-empty `models.SecurityContext.SessionID` created by the login service.
- Produces: `GenerateAccessTokenForSessionWithAMR(user *iface.User, deviceInfo *models.DeviceInfo, securityCtx *models.SecurityContext, amr []string, lastOTPAt int64) (string, error)` and existing `GenerateTokenPairWithAMR` with explicit session preservation.

- [ ] **Step 1: Write failing JWT claim tests**

Add tests that construct `SecurityContext{SessionID: "session-canonical"}` and assert:

```go
pair, err := svc.GenerateTokenPairWithAMR(user, device, security, []string{"pwd"}, 0)
require.NoError(t, err)
accessClaims, err := svc.ValidateAccessToken(pair.AccessToken)
require.NoError(t, err)
refreshClaims, err := svc.ValidateRefreshToken(pair.RefreshToken)
require.NoError(t, err)
assert.Equal(t, "session-canonical", accessClaims.SessionID)
assert.Equal(t, accessClaims.SessionID, refreshClaims.SessionID)
```

Add negative cases for nil security context and empty `SessionID`; production session-aware methods must return an error instead of inventing `session_<unix>`.

- [ ] **Step 2: Run the JWT tests and confirm the missing validation**

Run:

```bash
cd backend
go test ./internal/core/auth/services -run 'TestGenerateTokenPairWithAMR_RequiresSession|TestGenerateTokenPairWithAMR_PreservesSession' -count=1
```

Expected: at least the empty-session case FAILS.

- [ ] **Step 3: Implement the explicit-session API**

Add an internal validator:

```go
func requireSessionContext(securityCtx *models.SecurityContext) error {
	if securityCtx == nil || securityCtx.SessionID == "" {
		return errors.New("session id is required for token issuance")
	}
	return nil
}
```

Apply it to `GenerateTokenPair` and `GenerateTokenPairWithAMR`. Implement `GenerateAccessTokenForSessionWithAMR` by copying the supplied security context, setting only `AMR` and `LastOTPAt`, and calling `GenerateEnhancedAccessToken`. Do not generate a timestamp-derived SID in this method.

- [ ] **Step 4: Refactor password issuance to generate the UUID first**

In `PasswordAuthService.issueTokens`, generate `sessionID := uuid.NewString()` before signing. Build the actual device and security contexts, then call `GenerateTokenPairWithAMR` once:

```go
sessionID := uuid.NewString()
device := &authModels.DeviceInfo{DeviceID: deviceID, DeviceType: "web", Platform: platform}
security := &authModels.SecurityContext{SessionID: sessionID, IPAddress: in.IP, Timestamp: time.Now()}
pair, err := s.jwtService.GenerateTokenPairWithAMR(user, device, security, amr, lastOTPAt)
```

Persist `pair.RefreshToken` with the same `sessionID`, create the session document with the same value, and return `pair.AccessToken`, `pair.RefreshToken`, and `sessionID`. Stop ignoring refresh/session persistence errors: if refresh creation fails, return no tokens; if session creation fails after refresh creation, revoke tokens by that session before returning the error.

- [ ] **Step 5: Add password end-to-end SID assertions**

In `session_identity_test.go`, issue password login tokens with real test RSA keys, decode both tokens, inspect the captured refresh row and captured session document, and assert all five values equal the response `SessionID`.

- [ ] **Step 6: Run JWT and password tests**

Run:

```bash
cd backend
go test ./internal/core/auth/services -run 'Session|TokenPair|IssueTokens' -count=1
```

Expected: PASS.

- [ ] **Step 7: Commit canonical password/JWT sessions**

```bash
git add backend/internal/core/auth/services/jwt_service.go \
  backend/internal/core/auth/services/jwt_helpers_test.go \
  backend/internal/core/auth/services/password_auth_service.go \
  backend/internal/core/auth/services/session_identity_test.go
git commit -m "fix(auth): require canonical token session ids" -m "Prop: upstream"
```

### Task 4: Propagate canonical sessions through OAuth, refresh, MFA, and step-up

**Files:**
- Modify: `backend/internal/core/auth/services/auth_service.go`
- Modify: `backend/internal/core/auth/services/refresh_orchestration_test.go`
- Modify: `backend/internal/core/auth/services/auth_service_sessions_test.go`
- Modify: `backend/internal/core/auth/handlers/mfa_handler.go`
- Modify: `backend/internal/core/auth/handlers/webauthn_handler.go`
- Modify: `backend/internal/core/auth/handlers/self_user_auth_handler.go`
- Create: `backend/internal/core/auth/services/oauth_session_identity_test.go`
- Create: `backend/internal/core/auth/handlers/step_up_session_identity_test.go`

**Interfaces:**
- Consumes: Task 3's explicit-session JWT methods and the current JWT `sid` from middleware context.
- Produces: OAuth login and every refresh/step-up token carrying the original persisted session UUID.

- [ ] **Step 1: Write failing OAuth and refresh SID tests**

Add an OAuth service test with real signing keys and captured repositories. After `HandleOAuthCallbackWithLinking`, assert access claim `sid`, refresh claim `sid`, response `SessionID`, refresh row `SessionUUID`, and auth-session `UUID` are identical.

Extend `refresh_orchestration_test.go` to decode the returned JWTs after one rotation and a second rotation:

```go
assert.Equal(t, oldDoc.SessionUUID, accessClaims.SessionID)
assert.Equal(t, oldDoc.SessionUUID, refreshClaims.SessionID)
assert.Equal(t, oldDoc.SessionUUID, successor.SessionUUID)
```

- [ ] **Step 2: Run the tests and confirm the synthetic SID mismatch**

Run:

```bash
cd backend
go test ./internal/core/auth/services -run 'OAuthSessionIdentity|Refresh.*PreservesJWTSession' -count=1
```

Expected: FAIL because JWT `sid` currently comes from `session_<unix>`.

- [ ] **Step 3: Refactor OAuth full-token issuance**

In `GenerateEnhancedTokenPair`, validate the user, normalize device data, generate one random session UUID, and call `GenerateTokenPairWithAMR` with that UUID before persistence. Persist the refresh row and an `AuthSessionDoc` using the same UUID and `LoginMethod: "oauth"`. Return no tokens when either persistence operation fails; revoke by session on a session-write failure after the refresh row exists.

Remove all calls from production OAuth issuance to `GenerateAccessTokenWithAMR` and `GenerateRefreshToken` convenience methods.

- [ ] **Step 4: Preserve SID during refresh rotation**

In `RefreshTokensWithRiskAssessment`, create:

```go
device := &models.DeviceInfo{DeviceID: tokenDoc.DeviceID, DeviceType: tokenDoc.DeviceType, Platform: tokenDoc.Platform, Fingerprint: tokenDoc.Fingerprint}
security := &models.SecurityContext{SessionID: tokenDoc.SessionUUID, IPAddress: nonEmpty(securityCtxIP(securityCtx), tokenDoc.IPAddress), Timestamp: time.Now()}
pair, err := s.jwtService.GenerateTokenPairWithAMR(user, device, security, claims.AMR, claims.LastOTPAt)
```

Use `pair.AccessToken` and `pair.RefreshToken` for the successor row and response. Never derive a new SID during rotation.

- [ ] **Step 5: Write failing step-up preservation tests**

For TOTP verify, WebAuthn verify, and password-confirm handlers, seed an authenticated context whose claims contain `SessionID: "session-step-up"`. Decode the replacement access token and assert its `sid` remains `session-step-up` while `amr`/`last_otp_at` change.

- [ ] **Step 6: Refactor step-up token issuance**

Replace handler calls to the synthetic `GenerateAccessTokenWithAMR` with `GenerateAccessTokenForSessionWithAMR`. Obtain the current SID and device claim through the existing claims accessor/middleware context; fail closed with 401 if the session claim is missing. MFA partial-login completion must carry its pending session UUID in the server-side challenge and use that UUID when issuing the first full token pair.

- [ ] **Step 7: Verify revocation with a real issued access token**

Extend `auth_service_sessions_test.go`: issue a session, call `RevokeUserSession` with the response session ID, pass its access token through `AuthMiddleware.RequireAuth`, and expect `401` with `code=session_revoked`. Issue a second session for the same user and assert it remains accepted.

- [ ] **Step 8: Run auth service and handler tests**

Run:

```bash
cd backend
go test ./internal/core/auth/services ./internal/core/auth/handlers ./internal/shared/middleware -run 'SessionIdentity|PreservesJWTSession|StepUp|SessionRevoked' -count=1
```

Expected: PASS.

- [ ] **Step 9: Commit end-to-end session identity**

```bash
git add backend/internal/core/auth/services/auth_service.go \
  backend/internal/core/auth/services/refresh_orchestration_test.go \
  backend/internal/core/auth/services/auth_service_sessions_test.go \
  backend/internal/core/auth/services/oauth_session_identity_test.go \
  backend/internal/core/auth/handlers/mfa_handler.go \
  backend/internal/core/auth/handlers/webauthn_handler.go \
  backend/internal/core/auth/handlers/self_user_auth_handler.go \
  backend/internal/core/auth/handlers/step_up_session_identity_test.go
git commit -m "fix(auth): preserve session identity across token renewal" -m "Prop: upstream"
```

### Task 5: Add bounded telemetry for degraded revocation

**Files:**
- Modify: `backend/pkg/sdk/metrics/metrics.go`
- Modify: `backend/pkg/sdk/metrics/metrics_test.go`
- Modify: `backend/internal/core/auth/services/session_revocation_service.go`
- Modify: `backend/internal/core/auth/services/session_revocation_service_test.go`

**Interfaces:**
- Consumes: the existing singleton `metrics.Default()` and Redis `Get` errors.
- Produces: `Collector.RecordSessionRevocationStoreFailure(operation string)` with operation allowlisted to `lookup` or `write`, plus rate-limited sanitized warnings.

- [ ] **Step 1: Write a failing metric registration test**

Add a counter to the collector contract and assert its exposition after recording a lookup failure:

```go
collector.RecordSessionRevocationStoreFailure("lookup")
body := scrapeCollector(t, collector)
assert.Contains(t, body, `orkestra_auth_session_revocation_store_failures_total{operation="lookup"} 1`)
```

The only accepted operation labels are `lookup` and `write`; unknown input must collapse to `unknown` so cardinality remains bounded.

- [ ] **Step 2: Run metrics tests and confirm the metric is absent**

Run:

```bash
cd backend
go test ./pkg/sdk/metrics -run TestSessionRevocationStoreFailure -count=1
```

Expected: FAIL because the method and metric do not exist.

- [ ] **Step 3: Implement and register the bounded counter**

Add `sessionRevocationStoreFailures *prometheus.CounterVec` to `Collector`, build and register:

```go
prometheus.NewCounterVec(prometheus.CounterOpts{
	Namespace: "orkestra",
	Subsystem: "auth",
	Name:      "session_revocation_store_failures_total",
	Help:      "Count of Redis failures while reading or writing revoked session identifiers.",
}, []string{"operation"})
```

Implement `RecordSessionRevocationStoreFailure` with the fixed allowlist.

- [ ] **Step 4: Write revocation-service degradation tests**

Using the existing Redis fake, return a transport error from `Get`. Assert `IsRevoked` remains `(false, nil)`, the metric increments once, and captured logs contain neither the SID nor Redis error text. Make repeated calls within the rate-limit window and assert at most one warning is emitted. Add a write-failure test that records `operation="write"` and returns the original error to the caller.

- [ ] **Step 5: Add rate-limited sanitized logging**

Give `redisSessionRevocationService` a mutex-protected `lastWarning time.Time` and a fixed one-minute warning interval. On non-`redis.Nil` lookup failure, record the metric and log only:

```go
s.log.WarnContext(ctx, "session revocation store unavailable",
	slog.String("operation", "lookup"),
)
```

Do not log SID or raw error. In `Revoke`, record the `write` metric when `Set` returns an error and return that error unchanged so existing callers can audit degradation.

- [ ] **Step 6: Run metrics and revocation tests**

Run:

```bash
cd backend
go test ./pkg/sdk/metrics ./internal/core/auth/services -run 'SessionRevocationStore|SessionRevocation' -count=1
```

Expected: PASS.

- [ ] **Step 7: Commit revocation telemetry**

```bash
git add backend/pkg/sdk/metrics/metrics.go \
  backend/pkg/sdk/metrics/metrics_test.go \
  backend/internal/core/auth/services/session_revocation_service.go \
  backend/internal/core/auth/services/session_revocation_service_test.go
git commit -m "feat(auth): expose revocation store degradation" -m "Prop: upstream"
```

### Task 6: Document incident response and session migration

**Files:**
- Modify: `backend/internal/core/auth/CLAUDE.md`
- Modify: `docs/Authentication_flow.md`
- Modify: `docs/site/architecture/authentication-flow.mdx`
- Modify: `docs/site/operating/oauth-providers.mdx`

**Interfaces:**
- Consumes: completed behavior from Tasks 1–5.
- Produces: operator guidance that matches actual code and contains no real secret, internal hostname, or environment-specific value.

- [ ] **Step 1: Update the auth invariants documentation**

Document these exact invariants:

- one random session UUID is generated before token signing;
- access JWT `sid`, refresh JWT `sid`, refresh row, and session document match;
- rotation and step-up preserve the SID;
- inactive users cannot receive credentials through password, OAuth, mobile, MFA completion, refresh, or session bootstrap;
- revocation Redis failures remain fail-open until access-token expiry and increment the new metric.

- [ ] **Step 2: Add the Apple-key response runbook**

Add an operator subsection with ordered actions: identify affected environments, restrict/sanitize retained local and OTLP logs, rotate and invalidate the Apple key in Apple Developer, update the encrypted `auth` module configuration, restart no service unless required by the live config behavior, force reauthentication, and monitor OAuth failures. Do not claim the application can rotate an external key.

- [ ] **Step 3: Document legacy SID migration**

State that deployments upgrading from affected builds must revoke all active refresh rows and allow existing access tokens to expire within the configured access-token TTL. Retain rotated/revoked refresh rows for at least one refresh-token TTL so replay detection remains effective.

- [ ] **Step 4: Verify documentation references and formatting**

Run:

```bash
rg -n 'session_<|APPLE_DEBUG|AUTH_DEBUG|REPO_DEBUG' backend/internal/core/auth/CLAUDE.md docs/Authentication_flow.md docs/site
git diff --check
```

Expected: no documentation recommends timestamp-derived sessions or debug logging; `git diff --check` exits 0.

- [ ] **Step 5: Commit documentation**

```bash
git add backend/internal/core/auth/CLAUDE.md docs/Authentication_flow.md docs/site
git commit -m "docs(auth): add hardening and key rotation guidance" -m "Prop: upstream"
```

### Task 7: Run complete security verification

**Files:**
- Modify only files required to fix failures directly caused by Tasks 1–6.
- Regenerate `backend/openapi/enterprise.json` only if a response schema or endpoint contract changed.

**Interfaces:**
- Consumes: all preceding task deliverables.
- Produces: a green backend security and build gate without new baselines or ignored failures.

- [ ] **Step 1: Run focused auth, authz, and middleware tests**

```bash
cd backend
go test ./internal/core/auth/... ./internal/core/authz/... ./internal/shared/middleware/... -count=1
```

Expected: PASS for every package.

- [ ] **Step 2: Run race-sensitive session tests repeatedly**

```bash
cd backend
go test -race ./internal/core/auth/services ./internal/shared/middleware -run 'Session|Refresh|Revocation|OAuth' -count=5
```

Expected: PASS with no race report.

- [ ] **Step 3: Run repository backend gates**

```bash
make backend-lint
make backend-tenantscope
make backend-policycoverage
make backend-piiscan
make backend-vulncheck
make backend-test-ci
make backend-build-ci
make backend-openapi-check
```

Expected: every command exits 0; policy coverage reports 0 errors and 0 warnings; no new baseline entry is added.

- [ ] **Step 4: Confirm secrets and debug markers are absent**

```bash
rg -n 'APPLE_DEBUG|AUTH_DEBUG|REPO_DEBUG|First 50 chars|Last 50 chars|Config keys available' backend/internal/core/auth
rg -n 'fmt\.(Print|Printf|Println)\(' backend/internal/core/auth --glob '!**/*_test.go'
```

Expected: both commands produce no output.

- [ ] **Step 5: Inspect the final diff and propagation trailers**

```bash
git diff --check
git status --short
git log --format='%h %s | %(trailers:key=Prop,valueonly)' dev..HEAD
```

Expected: clean formatting; every implementation commit has exactly `upstream` in the trailer column. If verification finds a defect, return to the task that owns the failing behavior, add a regression test there, and repeat that task's test and commit steps before rerunning Task 7.
