# Tier-1 OAuth Initial-Password Enrollment Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Let a signed-in, OAuth-only Tier-1 human user add email/password sign-in to the existing operator account from the Security page, with fresh proof of presence, atomic create-only persistence, and no Tier-2 behavior change.

**Architecture:** Add a narrow `iface.InitialPasswordSetter` capability implemented by the Tier-1 user service and backed by a Mongo compare-and-set. Expose a dedicated operator-only Huma endpoint through `PasswordAuthService`; keep `ChangePassword` unchanged. Mount the endpoint under `RequireGlobal()` plus the existing five-minute enrollment-proof gate, then split `PasswordTab` into explicit enrollment, disabled, change, loading, and error states.

**Tech Stack:** Go 1.26.8, Huma v2, Chi, MongoDB 8, React 19, TypeScript 5.9, Redux Toolkit Query, React Hook Form, Yup, React Bootstrap, Vitest, Testing Library, MSW.

**Spec:** [`docs/superpowers/specs/2026-09-30-tier1-oauth-initial-password-design.md`](../specs/2026-09-30-tier1-oauth-initial-password-design.md)

## Global Constraints

- This is a Tier-1/operator feature only. Do not add a client API route, change `frontend-client`, or pass the capability into the client tier bundle.
- Do not weaken or overload `ChangePassword`; an existing password must still require the current password.
- The request body contains only `newPassword`. The authenticated context supplies the user UUID, and the existing account supplies the email.
- The route must be behind operator bearer validation, `RequireGlobal()`, and `operatorEnrolmentGate(5*time.Minute)`.
- Read `passwordLoginEnabledAdmin` strictly at request time. Break-glass does not enable enrollment.
- Keep `iface.UserProvider` unchanged. The new persistence seam is additive and must fail closed if unavailable.
- Never log, audit, return, or place in a URL the plaintext password, its hash, an OAuth subject, or a token.
- Preserve the current session after success; revoke other sessions and all device-trust grants best-effort with reason `password_added`.
- Reuse the live password-policy validator and Argon2id hasher.
- Follow `backend/internal/core/user/AGENTS.md`, `backend/internal/core/auth/AGENTS.md`, and the canonical auth flow at `docs/site/architecture/authentication-flow.mdx`.
- For `frontend-admin`, use the existing `PasswordTab.tsx` precedent and the React Bootstrap / React Hook Form / Yup primitives confirmed from `src/reference/components/forms/FormValidation.tsx` and `FormLayout.tsx`.
- Every task is test-first and ends with a focused commit. Preserve unrelated worktree changes.

## File Map

| File | Responsibility |
|---|---|
| `backend/pkg/sdk/iface/interfaces.go` | Add the source-compatible setter capability and stable already-set sentinel. |
| `backend/internal/core/user/repository/user_repository.go` | Implement the Mongo create-only compare-and-set. |
| `backend/internal/core/user/repository/initial_password_integration_test.go` | Prove absent/empty handling, no overwrite, deletion behavior, and concurrency. |
| `backend/internal/core/user/services/user_service.go` | Validate inputs and expose the capability. |
| `backend/internal/core/auth/services/password_auth_service.go` | Enforce policy/account eligibility, validate/hash, persist, tear down other sessions, and emit events. |
| `backend/internal/core/auth/services/password_initial_enrollment_test.go` | Cover success, all fail-closed branches, races, teardown, and secret-free metadata. |
| `backend/internal/core/auth/models/device_trust.go` | Add the `password_added` revocation reason. |
| `backend/internal/core/auth/tier_bundle.go` | Inject the operator setter and security-event sink. |
| `backend/internal/core/auth/services/auth_event_compliance_test.go` | Pin security-event to compliance-action mapping. |
| `backend/internal/core/auth/handlers/password_handler.go` | Add the request/response, handler, error mapping, and operator-only route. |
| `backend/internal/core/auth/module.go` | Resolve the operator capability and mount the route behind enrollment proof. |
| `backend/internal/shared/errcode/codes.go` | Add `auth.password_already_set`. |
| `backend/internal/core/compliance/models/audit_event.go` | Add `auth.password.added`. |
| `backend/internal/core/compliance/module.go` | Wire the compliance sink into the canonical auth service so mapped security events reach the audit log once. |
| `frontend-admin/src/store/api/authApi.ts` | Add the mutation and invalidate `SelfAuthMethods`. |
| `frontend-admin/src/pages/user/security/PasswordTab.tsx` | Render authoritative enrollment/change/disabled/loading/error modes. |
| `frontend-admin/src/pages/user/security/PasswordTab.test.tsx` | Test modes, exact payload, success transition, and race recovery. |
| `frontend-admin/src/locales/{en,it}.json` | Add paired localized copy. |
| `backend/internal/core/auth/AGENTS.md` and `docs/site/**` | Document invariants and the flow. |
| `backend/openapi/enterprise.json` | Regenerated Huma contract. |

## Task 1: Add the atomic initial-password capability to the user module

**Files:**

- Modify: `backend/pkg/sdk/iface/interfaces.go`
- Modify: `backend/internal/core/user/repository/user_repository.go`
- Create: `backend/internal/core/user/repository/initial_password_integration_test.go`
- Modify: `backend/internal/core/user/services/user_service.go`
- Modify: `backend/internal/core/user/services/user_service_test.go`
- Modify: `backend/internal/core/user/services/user_service_notfound_delegation_test.go`
- Modify: `backend/internal/core/user/handlers/user_handler_test.go`

- [ ] **Step 1: Write failing service tests**

Add tests for empty UUID/hash, successful delegation, repository failure, `iface.ErrUserNotFound`, and `iface.ErrPasswordAlreadySet`. Extend every `repository.UserRepository` fake with the new method.

~~~go
func TestUserService_SetPasswordHashIfUnset_PreservesConflict(t *testing.T) {
    repo := &fakeUserRepo{setPasswordHashIfUnsetErr: iface.ErrPasswordAlreadySet}
    svc := newTestUserService(repo)
    err := svc.SetPasswordHashIfUnset(context.Background(), "u-1", "$argon2id$v=19$hash")
    if !errors.Is(err, iface.ErrPasswordAlreadySet) {
        t.Fatalf("got %v, want ErrPasswordAlreadySet", err)
    }
}
~~~

Run:

~~~bash
cd backend && go test ./internal/core/user/services ./internal/core/user/handlers -run 'SetPasswordHashIfUnset' -count=1
~~~

Expected: FAIL because the method and sentinel do not exist.

- [ ] **Step 2: Add the additive SDK seam**

Place this beside the other narrow user capabilities, not inside `UserProvider`:

~~~go
type InitialPasswordSetter interface {
    SetPasswordHashIfUnset(ctx context.Context, userUUID, hash string) error
}

var ErrPasswordAlreadySet = errors.New("password already set")
~~~

- [ ] **Step 3: Implement the Mongo compare-and-set**

Add the method to the internal `UserRepository` interface and implement:

~~~go
func (r *mongoUserRepository) SetPasswordHashIfUnset(ctx context.Context, userUUID, hash string) error {
    now := time.Now()
    filter := bson.M{
        "uuid":      userUUID,
        "deletedAt": bson.M{"$exists": false},
        "$or": bson.A{
            bson.M{"passwordHash": bson.M{"$exists": false}},
            bson.M{"passwordHash": ""},
        },
    }
    update := bson.M{"$set": bson.M{
        "passwordHash":      hash,
        "passwordUpdatedAt": now,
        "updatedAt":         now,
    }}
    result, err := r.collection.UpdateOne(ctx, filter, update)
    if err != nil {
        return fmt.Errorf("set initial password hash: %w", err)
    }
    if result.MatchedCount == 1 {
        return nil
    }
    count, err := r.collection.CountDocuments(ctx, bson.M{
        "uuid":      userUUID,
        "deletedAt": bson.M{"$exists": false},
    }, options.Count().SetLimit(1))
    if err != nil {
        return fmt.Errorf("classify initial password conflict: %w", err)
    }
    if count == 0 {
        return ErrUserNotFound
    }
    return iface.ErrPasswordAlreadySet
}
~~~

Keep `UpdatePasswordHash` unchanged.

- [ ] **Step 4: Expose the capability through `userService`**

Add the method to the user module's concrete `UserService` interface as well as its implementation; this is an internal module interface, not the cross-module `iface.UserProvider` contract.

~~~go
func (s *userService) SetPasswordHashIfUnset(ctx context.Context, userUUID, hash string) error {
    if strings.TrimSpace(userUUID) == "" {
        return ErrInvalidUserID
    }
    if hash == "" {
        return errors.New("password hash is required")
    }
    return asUserNotFound(s.userRepo.SetPasswordHashIfUnset(ctx, userUUID, hash))
}

var _ iface.InitialPasswordSetter = (*userService)(nil)
~~~

- [ ] **Step 5: Add Mongo integration coverage**

Reuse `liveUserRepository` from the existing Mongo integration tests. Cover:

- absent `passwordHash` succeeds;
- empty `passwordHash` succeeds;
- a non-empty hash returns `iface.ErrPasswordAlreadySet` without changing hash/timestamps;
- missing or soft-deleted user returns `ErrUserNotFound`;
- two goroutines released from one start channel yield exactly one success and one conflict;
- success sets `passwordUpdatedAt` and `updatedAt`.

Core race assertion:

~~~go
if successes != 1 || conflicts != 1 {
    t.Fatalf("successes=%d conflicts=%d, want 1/1", successes, conflicts)
}
~~~

- [ ] **Step 6: Run and commit**

~~~bash
cd backend && go test ./internal/core/user/repository ./internal/core/user/services ./internal/core/user/handlers -run 'SetPasswordHashIfUnset|UpdatePasswordHash' -count=1
~~~

Expected: PASS. A missing test Mongo may SKIP only under the same explicit condition used by existing live-repository tests; rerun against development Mongo before final verification.

~~~bash
git add backend/pkg/sdk/iface/interfaces.go backend/internal/core/user/repository/user_repository.go backend/internal/core/user/repository/initial_password_integration_test.go backend/internal/core/user/services/user_service.go backend/internal/core/user/services/user_service_test.go backend/internal/core/user/services/user_service_notfound_delegation_test.go backend/internal/core/user/handlers/user_handler_test.go
git commit -m "feat(user): add atomic initial password setter"
~~~

## Task 2: Implement the auth service flow and teardown consequences

**Files:**

- Create: `backend/internal/core/auth/services/password_initial_enrollment_test.go`
- Modify: `backend/internal/core/auth/services/password_auth_service.go`
- Modify: `backend/internal/core/auth/services/gates_fakes_test.go`
- Modify: `backend/internal/core/auth/services/password_credential_revocation_test.go`
- Modify: `backend/internal/core/auth/models/device_trust.go`

- [ ] **Step 1: Make the shared user fake implement the atomic seam**

Use its mutex and stored user map:

~~~go
func (f *gateUserFake) SetPasswordHashIfUnset(_ context.Context, userUUID, hash string) error {
    f.mu.Lock()
    defer f.mu.Unlock()
    u := f.byUUID[userUUID]
    if u == nil {
        return iface.ErrUserNotFound
    }
    if u.PasswordHash != "" {
        return iface.ErrPasswordAlreadySet
    }
    u.PasswordHash = hash
    now := time.Now()
    u.PasswordUpdatedAt = &now
    return nil
}
~~~

- [ ] **Step 2: Write failing eligibility and policy tests**

Build an environment with a verified, active, OAuth-only human, a real `PasswordService`, operator policy enabled, the atomic setter, revocation fakes, and a recording security-event sink.

Cover these refusals and assert no hash, teardown, or success event:

| Case | Expected error |
|---|---|
| policy disabled | `ErrPasswordLoginDisabled` |
| policy unreadable or nil | `ErrAuthPolicyUnavailable` |
| inactive user | `ErrUserInactive` |
| service principal | `ErrUserInactive` |
| unverified account email | `ErrEmailNotVerified` |
| existing password | `iface.ErrPasswordAlreadySet` |
| setter missing | `ErrInitialPasswordUnavailable` |
| password policy rejection | existing policy sentinel |
| setter race | `iface.ErrPasswordAlreadySet` |
| repository outage | original storage error |

Run:

~~~bash
cd backend && go test ./internal/core/auth/services -run 'TestSetInitialPassword' -count=1
~~~

Expected: FAIL because the service contract is absent.

- [ ] **Step 3: Add service dependencies and inputs**

~~~go
var (
    ErrPasswordAlreadySet         = iface.ErrPasswordAlreadySet
    ErrInitialPasswordUnavailable = errors.New("initial password setter unavailable")
)

type SetInitialPasswordInput struct {
    UserUUID   string
    CurrentSID string
    New        string
}
~~~

Add `InitialPasswordSetter iface.InitialPasswordSetter` to `PasswordAuthConfig` and the service. Add an optional `securityEventSink SecurityEventSink` with:

~~~go
func (s *PasswordAuthService) SetSecurityEventSink(sink SecurityEventSink) {
    s.securityEventSink = sink
}
~~~

- [ ] **Step 4: Make teardown status observable without changing its best-effort semantics**

Change the private helper to return:

~~~go
type credentialRevocationResult struct {
    Revoked  int
    Complete bool
}
~~~

Initialize `Complete: true` and set it false for errors from session listing, refresh-token revocation, session termination, Redis SID revocation, by-user refresh revocation, or device-trust revocation. Continue after every failure. Update `ResetPassword` and `ChangePassword` to read `result.Revoked`; their external behavior stays unchanged.

Extend `password_credential_revocation_test.go` with an injected failure and assert `Complete == false` while later stages were attempted.

- [ ] **Step 5: Implement the fail-closed enrollment sequence**

Use this exact order: setter availability, strict operator policy, user read, active/human, verified email, no hash, live validation, hash, atomic setter, then post-write consequences.

~~~go
func (s *PasswordAuthService) SetInitialPassword(ctx context.Context, in SetInitialPasswordInput) error {
    if s.initialPasswordSetter == nil {
        return ErrInitialPasswordUnavailable
    }
    enabled, err := s.policy.PasswordLoginEnabled(ctx, s.audience)
    if err != nil {
        return ErrAuthPolicyUnavailable
    }
    if !enabled {
        return ErrPasswordLoginDisabled
    }
    user, err := s.userService.GetUserByID(ctx, in.UserUUID)
    if err != nil {
        return err
    }
    if !user.IsActive || user.Kind == iface.UserKindService {
        return ErrUserInactive
    }
    if !user.EmailVerified {
        return ErrEmailNotVerified
    }
    if user.PasswordHash != "" {
        return ErrPasswordAlreadySet
    }
    if err := s.passwordService.ValidatePolicy(ctx, in.New, user.Email); err != nil {
        return err
    }
    hash, err := s.passwordService.Hash(in.New)
    if err != nil {
        return err
    }
    if err := s.initialPasswordSetter.SetPasswordHashIfUnset(ctx, user.UUID, hash); err != nil {
        return err
    }

    _ = s.userService.ClearFailedLogins(ctx, user.UUID)
    s.resetLoginFailures(ctx, user.Email)
    teardown := s.revokeSessionsAfterCredentialChange(
        ctx, user.UUID, "password_added", in.CurrentSID, authModels.DeviceTrustRevokedOnPasswordAdded,
    )
    metadata := map[string]interface{}{
        "audience":             string(s.audience),
        "currentSessionId":     in.CurrentSID,
        "otherSessionsRevoked": teardown.Revoked,
        "teardownComplete":     teardown.Complete,
    }
    emitCredentialEvent(ctx, s.securityEventSink, s.logger, "self_password_added", user.UUID, metadata)
    return nil
}
~~~

Do not add the account email or any submitted credential to event metadata. The security-event sink derives the source IP from the request context; the handler must preserve the existing HTTP request context when calling the service.

- [ ] **Step 6: Add the device-trust reason and success tests**

~~~go
const DeviceTrustRevokedOnPasswordAdded = "password_added"
~~~

Success tests must prove:

- the stored Argon2id hash verifies;
- the current session survives;
- all other session, refresh-token, and SID paths are revoked;
- device trust is revoked with `password_added`;
- failed-login state is cleared;
- exactly one `self_password_added` security event is emitted by the password service; Task 3 proves that its mapping produces exactly one `auth.password.added` compliance event;
- metadata contains only bounded IDs/status/count/audience, while the event's dedicated IP field comes from request context; neither contains plaintext or hash;
- post-write teardown failure still returns success and records `teardownComplete: false`.

- [ ] **Step 7: Run and commit**

~~~bash
cd backend && go test ./internal/core/auth/services -run 'TestSetInitialPassword|TestCredentialRevocation|TestChangePassword|TestResetPassword' -count=1
~~~

Expected: PASS.

~~~bash
git add backend/internal/core/auth/services/password_initial_enrollment_test.go backend/internal/core/auth/services/password_auth_service.go backend/internal/core/auth/services/gates_fakes_test.go backend/internal/core/auth/services/password_credential_revocation_test.go backend/internal/core/auth/models/device_trust.go
git commit -m "feat(auth): implement initial password enrollment"
~~~

## Task 3: Wire tier dependencies and audit vocabularies

**Files:**

- Modify: `backend/internal/core/auth/services/auth_service.go`
- Create: `backend/internal/core/auth/services/auth_event_compliance_test.go`
- Modify: `backend/internal/core/compliance/models/audit_event.go`
- Modify: `backend/internal/core/compliance/module.go`
- Create: `backend/internal/core/compliance/module_audit_wiring_test.go`
- Modify: `backend/internal/core/auth/tier_bundle.go`
- Modify: `backend/internal/core/auth/tier_bundle_test.go`

- [ ] **Step 1: Write failing mapping and bundle tests**

Pin:

~~~go
{"self_password_added", "auth.password.added"},
~~~

Add a bundle test that passes an `iface.InitialPasswordSetter` to operator deps and proves the password service receives it and records through the bundle auth service. Construct the client bundle without the setter and prove construction still succeeds. Add a compliance-module test that registers an `iface.AuditSinkSetter` under `module.ServiceAuthService` and proves module initialization calls it exactly once.

Run:

~~~bash
cd backend && go test ./internal/core/auth/services ./internal/core/auth ./internal/core/compliance -run 'TestAuthEventComplianceAction|TestTierBundle.*InitialPassword|TestModule_WiresAuditSinkToAuthService' -count=1
~~~

Expected: FAIL.

- [ ] **Step 2: Add action constants and mapping**

~~~go
ActionAuthPasswordAdded = "auth.password.added"
~~~

~~~go
case "self_password_added":
    return "auth.password.added"
~~~

- [ ] **Step 3: Thread the setter and security-event sink through the bundle**

Add `initialPasswordSetter iface.InitialPasswordSetter` to `tierBundleDeps` and pass it into `PasswordAuthConfig`. After constructing `passSvc`:

~~~go
if sink, ok := authSvc.(services.SecurityEventSink); ok {
    passSvc.SetSecurityEventSink(sink)
}
~~~

Do not invent a client setter or client route.

- [ ] **Step 4: Wire compliance into the canonical auth security-event lane**

In the compliance module's existing audit-sink wiring block, add the additive interface lookup:

~~~go
if authAudit, ok := module.GetTyped[iface.AuditSinkSetter](deps.Services, module.ServiceAuthService); ok {
    authAudit.SetAuditSink(sink)
}
~~~

The initial-password service must emit only `self_password_added` through `SecurityEventSink`; `authEventComplianceAction` then mirrors it once as `auth.password.added`. Do not also call `PasswordAuthService.emitAudit` for this success path, or a fully wired deployment will receive duplicate audit rows.

- [ ] **Step 5: Run and commit**

~~~bash
cd backend && go test ./internal/core/auth/services ./internal/core/auth ./internal/core/compliance -run 'TestAuthEventComplianceAction|TestTierBundle.*InitialPassword|TestModule_WiresAuditSinkToAuthService' -count=1
~~~

Expected: PASS.

~~~bash
git add backend/internal/core/auth/services/auth_service.go backend/internal/core/auth/services/auth_event_compliance_test.go backend/internal/core/compliance/models/audit_event.go backend/internal/core/compliance/module.go backend/internal/core/compliance/module_audit_wiring_test.go backend/internal/core/auth/tier_bundle.go backend/internal/core/auth/tier_bundle_test.go
git commit -m "feat(auth): audit initial password enrollment"
~~~

## Task 4: Add the operator-only HTTP contract and stable errors

**Files:**

- Modify: `backend/internal/core/auth/handlers/password_handler.go`
- Create: `backend/internal/core/auth/handlers/initial_password_handler_test.go`
- Modify: `backend/internal/core/auth/handlers/route_mount_test.go`
- Modify: `backend/internal/core/auth/handlers/error_mapping_test.go`
- Modify: `backend/internal/shared/errcode/codes.go`
- Modify: `backend/internal/shared/errcode/codes_test.go`

- [ ] **Step 1: Write failing handler and OpenAPI tests**

Test that the handler derives `userUUID` and current SID from context, preserves the HTTP request context used by the event sink, returns 401 without a user, and passes only `newPassword` from the request.

Extend route tests:

~~~go
if _, ok := spec.Paths["/v1/auth/operator/me/password"]; !ok {
    t.Fatal("operator initial-password path missing")
}
if _, ok := spec.Paths["/v1/auth/client/me/password"]; ok {
    t.Fatal("initial-password path must not exist on the client surface")
}
~~~

Inspect the request schema and require `newPassword` as the only property. Explicitly reject `email`, `currentPassword`, `confirmPassword`, `userId`, and `audience`.

Add error-map cases for 409 `auth.password_already_set` and 503 `auth.unavailable`.

Run:

~~~bash
cd backend && go test ./internal/core/auth/handlers ./internal/shared/errcode -run 'TestSetInitialPassword|TestRouteMountsRegisterDistinctPaths|TestMapPasswordError|TestCodes' -count=1
~~~

Expected: FAIL.

- [ ] **Step 2: Add the error code**

~~~go
const AuthPasswordAlreadySet = "auth.password_already_set"
~~~

Add the exact name/value to the map in `codes_test.go`.

- [ ] **Step 3: Implement the request, handler, and hard-coded operator route**

~~~go
type SetInitialPasswordRequest struct {
    Body struct {
        NewPassword string `json:"newPassword" doc:"New password"`
    }
}

type SetInitialPasswordResponse struct {
    Body struct {
        Success bool   `json:"success"`
        Message string `json:"message"`
    }
}
~~~

Implement the handler:

~~~go
func (h *PasswordAuthHandler) SetInitialPassword(ctx context.Context, req *SetInitialPasswordRequest) (*SetInitialPasswordResponse, error) {
    userUUID, _ := ctx.Value("userUUID").(string)
    if userUUID == "" {
        return nil, huma.Error401Unauthorized("authentication required")
    }
    claims, _ := ctx.Value("claims").(*authModels.JWTClaims)
    currentSID := ""
    if claims != nil {
        currentSID = claims.SessionID
    }
    err := h.svc.SetInitialPassword(ctx, services.SetInitialPasswordInput{
        UserUUID: userUUID, CurrentSID: currentSID, New: req.Body.NewPassword,
    })
    if err != nil {
        return nil, mapPasswordError(err)
    }
    resp := &SetInitialPasswordResponse{}
    resp.Body.Success = true
    resp.Body.Message = "Password sign-in added."
    return resp, nil
}

func (h *PasswordAuthHandler) RegisterInitialPasswordRoute(api huma.API) {
    huma.Register(api, huma.Operation{
        OperationID: "operator-password-set-initial",
        Method:      http.MethodPost,
        Path:        "/v1/auth/operator/me/password",
        Summary:     "Add password sign-in to the current operator account",
        Tags:        []string{"Authentication", "Self-Service"},
        Security:    []map[string][]string{{"bearerAuth": {}}},
    }, h.SetInitialPassword)
}
~~~

Keep it out of mount-aware `RegisterProtectedRoutes`.

- [ ] **Step 4: Map conflicts and capability outages**

~~~go
case errors.Is(err, services.ErrPasswordAlreadySet):
    return errcode.Conflict(errcode.AuthPasswordAlreadySet,
        "This account already has a password. Use change password instead.")
case errors.Is(err, services.ErrInitialPasswordUnavailable):
    return errcode.ServiceUnavailable(errcode.AuthUnavailable,
        "Password enrollment is temporarily unavailable; try again shortly.")
~~~

Existing email-verification, policy, and password-policy mappings remain shared.

- [ ] **Step 5: Run and commit**

~~~bash
cd backend && go test ./internal/core/auth/handlers ./internal/shared/errcode -run 'TestSetInitialPassword|TestRouteMountsRegisterDistinctPaths|TestMapPasswordError|TestCodes' -count=1
~~~

Expected: PASS.

~~~bash
git add backend/internal/core/auth/handlers/password_handler.go backend/internal/core/auth/handlers/initial_password_handler_test.go backend/internal/core/auth/handlers/route_mount_test.go backend/internal/core/auth/handlers/error_mapping_test.go backend/internal/shared/errcode/codes.go backend/internal/shared/errcode/codes_test.go
git commit -m "feat(auth): expose operator initial password endpoint"
~~~

## Task 5: Resolve the operator capability and enforce enrollment proof

**Files:**

- Modify: `backend/internal/core/auth/module.go`
- Create: `backend/internal/core/auth/initial_password_route_test.go`

- [ ] **Step 1: Write a routing-boundary regression test**

Following existing auth structural tests, assert that `module.go`:

- type-asserts the operator provider to `iface.InitialPasswordSetter`;
- does not assign a setter to `clDeps`;
- registers `RegisterInitialPasswordRoute` exactly once;
- registers it in a group containing both `RequireGlobal()` and `operatorEnrolmentGate`.

Run:

~~~bash
cd backend && go test ./internal/core/auth -run 'TestInitialPasswordRoute' -count=1
~~~

Expected: FAIL.

- [ ] **Step 2: Resolve the additive capability for operator only**

~~~go
operatorInitialPasswordSetter, ok := operatorUser.(iface.InitialPasswordSetter)
if !ok {
    logger.Warn("auth: operator user provider lacks initial-password capability; enrollment will fail closed")
}
opDeps.initialPasswordSetter = operatorInitialPasswordSetter
~~~

Leave `clDeps.initialPasswordSetter` nil.

- [ ] **Step 3: Resolve the existing gate earlier and mount the endpoint**

Move the one `operatorEnrolmentGate` resolution above the password route groups and reuse it for password, MFA, and passkey enrollment:

~~~go
operatorEnrolmentGate := enrolmentGate(m.logger, "operator", ri.Operator.AuthMW, 5*time.Minute)

ri.Operator.ProtectedRouter.Group(func(r chi.Router) {
    r.Use(ri.Operator.AuthMW.RequireGlobal())
    r.Use(operatorEnrolmentGate)
    api := humachi.New(r, ri.APIConfig)
    m.operatorPasswordHandler.RegisterInitialPasswordRoute(api)
})
~~~

Do not add `RequireStepUp`; the enrollment gate already selects fresh MFA proof for enrolled users and recent `auth_time` for no-factor users.

- [ ] **Step 4: Run and commit**

~~~bash
cd backend && go test ./internal/core/auth -run 'TestInitialPasswordRoute|TestEnrolmentGate' -count=1
~~~

Expected: PASS, including fresh OAuth, stale reauthentication, and enrolled-factor step-up behavior already pinned by the gate tests.

~~~bash
git add backend/internal/core/auth/module.go backend/internal/core/auth/initial_password_route_test.go
git commit -m "feat(auth): gate initial password enrollment"
~~~

## Task 6: Add the frontend mutation and explicit Password tab modes

**Files:**

- Modify: `frontend-admin/src/store/api/authApi.ts`
- Modify: `frontend-admin/src/test/handlers.ts`
- Modify: `frontend-admin/src/pages/user/security/PasswordTab.tsx`
- Modify: `frontend-admin/src/pages/user/security/PasswordTab.test.tsx`
- Modify: `frontend-admin/src/locales/en.json`
- Modify: `frontend-admin/src/locales/it.json`

- [ ] **Step 1: Add test handlers and write failing mode tests**

Add `currentOperatorHandler` returning a complete `BackendUser` with email `oauth@example.com`. Test:

1. policy/auth-method/current-user loading: spinner, no form;
2. any query error: alert, no form;
3. no password + enabled: read-only email, new/confirm fields, no current field;
4. no password + disabled: policy explanation, no form;
5. password present: current/new/confirm change form, including when login is disabled.

- [ ] **Step 2: Write failing interaction tests**

Use `userEvent` and MSW to assert the enrollment request body equals exactly:

~~~ts
{ newPassword: 'Correct-Horse-42!' }
~~~

Return a first auth-method read with `hasPasswordSet: false` and a post-success read with `hasPasswordSet: true`; assert the current-password field appears after invalidation/refetch.

Add a 409 Huma response with `code: 'auth.password_already_set'` and assert it refetches, clears both secret inputs, transitions to change mode if the refetch reports a password, and shows localized non-destructive copy.

Run:

~~~bash
cd frontend-admin && npm test -- --run src/pages/user/security/PasswordTab.test.tsx
~~~

Expected: FAIL.

- [ ] **Step 3: Add the RTK Query mutation**

~~~ts
setInitialPassword: builder.mutation<
  SimpleMessageResponse,
  { newPassword: string }
>({
  query: body => ({
    url: 'v1/auth/operator/me/password',
    method: 'POST',
    body
  }),
  invalidatesTags: ['SelfAuthMethods']
}),
~~~

Export `useSetInitialPasswordMutation`.

- [ ] **Step 4: Refactor around authoritative resolved state**

Read policy, auth methods, and current user. Before choosing a mode, render loading or failure without a form. Then derive:

~~~ts
const hasPassword = methodsQuery.data.hasPasswordSet;
const methodEnabled = passwordUiVisible(policyQuery.data);
const mode = hasPassword ? 'change' : methodEnabled ? 'enroll' : 'disabled';
~~~

Behavior:

- `enroll`: show read-only existing account email, new password, confirmation, and an `orkestra-primary` “Add password” action; render no current-password input; submit only `newPassword`.
- `disabled`: show policy guidance and no form.
- `change`: preserve current behavior and the retained-password notice.
- require `oldPassword` only in change mode;
- clear secrets on success and already-set conflict;
- let `step_up_required` and `reauthentication_required` reach existing global handling without a duplicate local toast;
- preserve the existing `/user/security?tab=password` return target and never store form data in URL/storage.

- [ ] **Step 5: Add paired locale copy**

Add the same English and Italian keys for loading/failure, enrollment heading/explanation, account email label/help, add/submitting action, success toast, already-set conflict, and disabled policy. Keep existing change-password keys.

- [ ] **Step 6: Run and commit**

~~~bash
cd frontend-admin && npm test -- --run src/pages/user/security/PasswordTab.test.tsx
cd frontend-admin && npm run typecheck
cd frontend-admin && npm run lint -- src/pages/user/security/PasswordTab.tsx src/pages/user/security/PasswordTab.test.tsx src/store/api/authApi.ts src/test/handlers.ts
~~~

Expected: PASS. If the lint script does not accept paths, run unfiltered `npm run lint`.

~~~bash
git add frontend-admin/src/store/api/authApi.ts frontend-admin/src/test/handlers.ts frontend-admin/src/pages/user/security/PasswordTab.tsx frontend-admin/src/pages/user/security/PasswordTab.test.tsx frontend-admin/src/locales/en.json frontend-admin/src/locales/it.json
git commit -m "feat(frontend-admin): add initial password enrollment"
~~~

## Task 7: Regenerate OpenAPI and update canonical documentation

**Files:**

- Modify: `backend/openapi/enterprise.json`
- Modify: `backend/internal/core/auth/AGENTS.md`
- Modify: `docs/site/architecture/authentication-flow.mdx`
- Modify: `docs/site/modules/core/auth.mdx`

- [ ] **Step 1: Regenerate and inspect OpenAPI**

~~~bash
cd backend && make openapi-dump
rg -n 'operator-password-set-initial|/v1/auth/operator/me/password|/v1/auth/client/me/password' openapi/enterprise.json
~~~

Expected: one operator POST with bearer security and a request schema containing only required `newPassword`; no client path. Never hand-edit generated JSON.

- [ ] **Step 2: Update the auth module contract**

Document the Tier-1-only route, five-minute proof gate, strict policy/no break-glass rule, additive setter, atomic create-only persistence, current-session preservation, unconditional best-effort teardown, `auth.password.added` / `self_password_added` events, and Tier-2 non-goal.

- [ ] **Step 3: Update human-facing docs**

In `authentication-flow.mdx` add the OAuth-only operator enrollment sequence. In `core/auth.mdx` add the endpoint, body, and 200/403/409/503 behavior. Do not edit the drifted `docs/Authentication_flow.md`.

- [ ] **Step 4: Verify and commit**

~~~bash
cd backend && make openapi-check
git diff --check
~~~

Expected: PASS.

~~~bash
git add backend/openapi/enterprise.json backend/internal/core/auth/AGENTS.md docs/site/architecture/authentication-flow.mdx docs/site/modules/core/auth.mdx
git commit -m "docs(auth): document initial password enrollment"
~~~

## Task 8: Verify end to end and request review

- [ ] **Step 1: Run focused backend packages**

~~~bash
cd backend && go test ./internal/core/user/... ./internal/core/auth/... ./internal/core/compliance/models ./internal/shared/errcode -count=1
~~~

Expected: PASS, including all interface fakes.

- [ ] **Step 2: Run focused frontend checks**

~~~bash
cd frontend-admin && npm test -- --run src/pages/user/security/PasswordTab.test.tsx
cd frontend-admin && npm run typecheck
~~~

Expected: PASS.

- [ ] **Step 3: Run repository CI entry points**

~~~bash
make ci-backend
make ci-frontend-admin
~~~

Expected: PASS, including policy coverage, tenant scope, OpenAPI drift, ESLint, tests, audits, and builds.

- [ ] **Step 4: Scan scope and secrets**

~~~bash
rg -n 'SetInitialPassword|password_added|auth\.password\.added|self_password_added|/me/password' backend frontend-admin docs/site
rg -n '/v1/auth/client/me/password|clDeps\.initialPasswordSetter' backend frontend-client
rg -n 'newPassword.*(log|audit|metadata)|PasswordHash.*(log|audit|metadata)' backend/internal/core/auth
~~~

Expected: intended operator references only, no client route/UI, and no plaintext/hash logging or event metadata.

- [ ] **Step 5: Smoke-test on the existing Docker development stack**

Verify:

1. fresh OAuth-only operator sees enrollment mode and read-only email;
2. compliant password succeeds;
3. current tab survives while another session and device trust are revoked;
4. email/password and OAuth login both work afterward;
5. stale OAuth auth time reauthenticates and returns to `?tab=password`;
6. disabled `passwordLoginEnabledAdmin` hides/refuses enrollment;
7. two tabs racing converge to change mode after one 409.

Do not start servers manually; use the existing stack and `orkestra.sh` if lifecycle work is needed.

- [ ] **Step 6: Review the final diff**

~~~bash
git status --short
git diff --check
~~~

Expected: only planned auth/user/frontend-admin/docs/OpenAPI surfaces changed; no Tier-2 implementation or unrelated work.

- [ ] **Step 7: Request review**

Invoke `superpowers:requesting-code-review` and direct the reviewer to the five items below.

## Review Focus

1. **Concurrent tabs:** Mongo, not an in-memory check, admits exactly one write; the loser gets 409 and the UI refetches/clears secrets.
2. **Live policy and break-glass:** a policy flip after render prevents the write; policy outage is 503; break-glass never enables enrollment.
3. **Identity/email boundary:** no body email/user/audience override; unverified local email, service principal, inactive, and deleted users cannot write.
4. **Post-write partial failure:** teardown failure never rolls back the hash or claims it is absent; the current session survives; bounded metadata truthfully reports incomplete teardown.
5. **Tier/proof isolation:** only the operator route exists, under both `RequireGlobal()` and the five-minute enrollment gate; fresh no-factor OAuth may proceed, stale auth time reauthenticates, and enrolled MFA requires fresh proof.
