package middleware

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	authModels "github.com/orkestra/backend/internal/core/auth/models"
	"github.com/orkestra/backend/pkg/sdk/iface"
)

// runStepUp wires a minimal AuthMiddleware, seeds the request context with
// the given claims, and invokes RequireStepUp(maxAge). Returns
// (downstreamRan, httpStatus, body) so the tests stay terse.
func runStepUp(t *testing.T, maxAge time.Duration, claims *authModels.JWTClaims) (bool, int, map[string]any) {
	t.Helper()
	m := newTestMiddleware(&fakeAuthz{}, &fakeTenantProvider{}, nil)

	req := httptest.NewRequest(http.MethodPost, "/v1/admin/anything", nil)
	if claims != nil {
		req = req.WithContext(context.WithValue(req.Context(), ctxClaims, claims))
	}
	rec := httptest.NewRecorder()

	called := false
	handler := m.RequireStepUp(maxAge)(http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {
		called = true
	}))
	handler.ServeHTTP(rec, req)

	var body map[string]any
	if rec.Body.Len() > 0 {
		_ = json.Unmarshal(rec.Body.Bytes(), &body)
	}
	return called, rec.Code, body
}

func TestRequireStepUp_FreshMFAProofPasses(t *testing.T) {
	claims := &authModels.JWTClaims{
		UserUUID:  "u-1",
		AMR:       []string{"pwd", "otp"},
		LastOTPAt: time.Now().Add(-30 * time.Second).Unix(),
	}
	called, status, _ := runStepUp(t, 5*time.Minute, claims)
	if !called {
		t.Errorf("fresh MFA must pass through; downstream not called (status %d)", status)
	}
	if status != http.StatusOK {
		t.Errorf("status = %d, want 200", status)
	}
}

func TestRequireStepUp_StaleMFAProofRejected(t *testing.T) {
	// last_otp_at older than maxAge → step up.
	claims := &authModels.JWTClaims{
		UserUUID:  "u-1",
		AMR:       []string{"pwd", "otp"},
		LastOTPAt: time.Now().Add(-10 * time.Minute).Unix(),
	}
	called, status, body := runStepUp(t, 5*time.Minute, claims)
	if called {
		t.Error("stale MFA must block downstream")
	}
	if status != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", status)
	}
	if code, _ := body["code"].(string); code != "step_up_required" {
		t.Errorf("body.code = %q, want step_up_required", code)
	}
}

func TestRequireStepUp_MissingAMRRejected(t *testing.T) {
	// amr without MFA marker → step up required even if LastOTPAt is set.
	claims := &authModels.JWTClaims{
		UserUUID:  "u-1",
		AMR:       []string{"pwd"},
		LastOTPAt: time.Now().Unix(),
	}
	called, status, body := runStepUp(t, 5*time.Minute, claims)
	if called {
		t.Error("non-MFA amr must block downstream")
	}
	if status != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", status)
	}
	if code, _ := body["code"].(string); code != "step_up_required" {
		t.Errorf("body.code = %q, want step_up_required", code)
	}
}

func TestRequireStepUp_MissingLastOTPAtRejected(t *testing.T) {
	// amr has otp but LastOTPAt is zero — we can't confirm freshness so
	// the middleware must reject. Pre-Block-A tokens land here.
	claims := &authModels.JWTClaims{
		UserUUID: "u-1",
		AMR:      []string{"pwd", "otp"},
	}
	called, status, _ := runStepUp(t, 5*time.Minute, claims)
	if called {
		t.Error("zero LastOTPAt must block downstream")
	}
	if status != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", status)
	}
}

func TestRequireStepUp_NoClaimsRejectedAsUnauth(t *testing.T) {
	called, status, _ := runStepUp(t, 5*time.Minute, nil)
	if called {
		t.Error("missing claims must block downstream")
	}
	if status != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", status)
	}
}

func TestRequireStepUp_DefaultMaxAgeWhenZero(t *testing.T) {
	// Zero maxAge defaults to 5min. A 2-minute-old OTP proof must pass.
	claims := &authModels.JWTClaims{
		UserUUID:  "u-1",
		AMR:       []string{"pwd", "otp"},
		LastOTPAt: time.Now().Add(-2 * time.Minute).Unix(),
	}
	called, status, _ := runStepUp(t, 0, claims)
	if !called {
		t.Errorf("2-min-old proof under default 5min window must pass; status %d", status)
	}
}

// fakeStepUpPolicy is the in-package StepUpPolicy double. Every knob
// defaults to the pre-PR-3 answer so a zero value keeps the behaviour
// every existing step-up test was written against.
type fakeStepUpPolicy struct {
	required bool
	// mfaDisabled flips the master MFA switch off. Defaults false so the
	// zero value reports the switch ON — every existing step-up test keeps
	// its behaviour without being updated.
	mfaDisabled bool
	// reauthDisabled / reauthErr drive the PR 3 PasswordReauthAllowed
	// branch: zero values report (true, nil) so pre-existing tests that
	// reach the password-confirm envelope keep passing unchanged.
	reauthDisabled bool
	reauthErr      error
}

func (f *fakeStepUpPolicy) MFARequired(_ *iface.User, _ []authModels.OrgMembership) bool {
	return f.required
}

func (f *fakeStepUpPolicy) MFAEnabled(_ context.Context) bool {
	return !f.mfaDisabled
}

func (f *fakeStepUpPolicy) PasswordReauthAllowed(_ context.Context, _ string) (bool, error) {
	if f.reauthErr != nil {
		return false, f.reauthErr
	}
	return !f.reauthDisabled, nil
}

// runStepUpThrough drives one request with claims on the context through
// RequireStepUp(5m) on the given middleware and decodes the JSON body.
// Extracted from runStepUpWithEnrollment so the PR 3 policy matrix can
// wire its own StepUpPolicy (or none) before driving the gate.
func runStepUpThrough(t *testing.T, m *AuthMiddleware, claims *authModels.JWTClaims) (bool, int, map[string]any) {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/v1/admin/anything", nil)
	if claims != nil {
		req = req.WithContext(context.WithValue(req.Context(), ctxClaims, claims))
	}
	rec := httptest.NewRecorder()
	called := false
	handler := m.RequireStepUp(5 * time.Minute)(http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {
		called = true
	}))
	handler.ServeHTTP(rec, req)

	var body map[string]any
	if rec.Body.Len() > 0 {
		_ = json.Unmarshal(rec.Body.Bytes(), &body)
	}
	return called, rec.Code, body
}

// runStepUpWithPolicy is runStepUpWithEnrollment with an explicit policy
// (nil = SetStepUpPolicy never called), for the PR 3 branch matrix.
func runStepUpWithPolicy(t *testing.T, claims *authModels.JWTClaims, hasFactor bool, lookupErr error, policy StepUpPolicy) (bool, int, map[string]any) {
	t.Helper()
	m := newTestMiddleware(&fakeAuthz{}, &fakeTenantProvider{}, nil)
	m.SetMFAEnrollmentLookup(func(_ context.Context, _, _ string) (bool, error) {
		return hasFactor, lookupErr
	})
	if policy != nil {
		m.SetStepUpPolicy(policy)
	}
	return runStepUpThrough(t, m, claims)
}

// runStepUpWithEnrollment is the pre-PR-3 harness shape: enrollment
// lookup + a policy that always answers. Kept as a wrapper over
// runStepUpWithPolicy so its many existing callers stay untouched.
func runStepUpWithEnrollment(t *testing.T, claims *authModels.JWTClaims, hasFactor bool, lookupErr error, mfaRequired bool) (bool, int, map[string]any) {
	t.Helper()
	return runStepUpWithPolicy(t, claims, hasFactor, lookupErr, &fakeStepUpPolicy{required: mfaRequired})
}

func TestRequireStepUp_NoFactorNonPrivilegedEmitsPasswordConfirm(t *testing.T) {
	// guest user with no MFA factor → password_confirm_required (401).
	claims := &authModels.JWTClaims{
		UserUUID:   "u-1",
		SystemRole: "guest",
		AMR:        []string{"pwd"},
	}
	called, status, body := runStepUpWithEnrollment(t, claims, false, nil, false)
	if called {
		t.Error("downstream must not run on step-up failure")
	}
	if status != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", status)
	}
	if code, _ := body["code"].(string); code != "password_confirm_required" {
		t.Errorf("body.code = %q, want password_confirm_required", code)
	}
}

func TestRequireStepUp_NoFactorPrivilegedEmitsEnrollmentRequired(t *testing.T) {
	// administrator with no factor and policy requiring MFA →
	// mfa_enrollment_required (403). Password reconfirm is not the
	// right exit here — they must enroll first.
	claims := &authModels.JWTClaims{
		UserUUID:   "u-2",
		SystemRole: "administrator",
		AMR:        []string{"pwd"},
	}
	called, status, body := runStepUpWithEnrollment(t, claims, false, nil, true)
	if called {
		t.Error("downstream must not run when enrollment is required")
	}
	if status != http.StatusForbidden {
		t.Errorf("status = %d, want 403", status)
	}
	if code, _ := body["code"].(string); code != "mfa_enrollment_required" {
		t.Errorf("body.code = %q, want mfa_enrollment_required", code)
	}
}

func TestRequireStepUp_HasFactorEmitsStepUpRequired(t *testing.T) {
	// User has TOTP enrolled but no fresh OTP proof → legacy
	// step_up_required so the frontend prompts for the code.
	claims := &authModels.JWTClaims{
		UserUUID:   "u-3",
		SystemRole: "guest",
		AMR:        []string{"pwd"},
	}
	called, status, body := runStepUpWithEnrollment(t, claims, true, nil, false)
	if called {
		t.Error("downstream must not run without fresh proof")
	}
	if status != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", status)
	}
	if code, _ := body["code"].(string); code != "step_up_required" {
		t.Errorf("body.code = %q, want step_up_required", code)
	}
}

func TestRequireStepUp_LookupErrorFailsClosedToStepUpRequired(t *testing.T) {
	// Mongo outage / unknown error from the enrollment lookup must NOT
	// silently weaken the gate. We emit step_up_required so the user
	// can still satisfy it with MFA (if they have it) and a privileged
	// account is never tricked into the password-only path.
	claims := &authModels.JWTClaims{
		UserUUID:   "u-4",
		SystemRole: "guest",
		AMR:        []string{"pwd"},
	}
	called, status, body := runStepUpWithEnrollment(t, claims, false, context.DeadlineExceeded, false)
	if called {
		t.Error("downstream must not run on lookup error")
	}
	if status != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", status)
	}
	if code, _ := body["code"].(string); code != "step_up_required" {
		t.Errorf("body.code = %q, want step_up_required", code)
	}
}

func TestRequireStepUp_ReauthAMRSatisfiesGate(t *testing.T) {
	// A token minted by /me/password-confirm carries amr=[pwd,reauth] +
	// last_otp_at=now. RequireStepUp must treat it as a satisfied proof.
	claims := &authModels.JWTClaims{
		UserUUID:  "u-5",
		AMR:       []string{"pwd", "reauth"},
		LastOTPAt: time.Now().Add(-30 * time.Second).Unix(),
	}
	called, status, _ := runStepUp(t, 5*time.Minute, claims)
	if !called {
		t.Errorf("reauth proof must pass; status %d", status)
	}
}

// PR 3 §4.6: the password-confirm fallback is offered only when the
// password is an accepted credential for the token's audience.
func TestRequireStepUp_PasswordReauthDisabledBecomesEnrollmentRequired(t *testing.T) {
	claims := &authModels.JWTClaims{UserUUID: "u-1", Audience: "operator", AMR: []string{"pwd"}}
	called, status, body := runStepUpWithPolicy(t, claims, false /*hasFactor*/, nil, &fakeStepUpPolicy{reauthDisabled: true})
	if called {
		t.Fatal("downstream must not run")
	}
	if status != http.StatusForbidden || body["code"] != "mfa_enrollment_required" {
		t.Fatalf("want 403 mfa_enrollment_required, got %d %v", status, body["code"])
	}
}

func TestRequireStepUp_PolicyErrorIs503NotEnrollment(t *testing.T) {
	claims := &authModels.JWTClaims{UserUUID: "u-1", Audience: "operator", AMR: []string{"pwd"}}
	called, status, body := runStepUpWithPolicy(t, claims, false, nil, &fakeStepUpPolicy{reauthErr: errors.New("mongo down")})
	if called {
		t.Fatal("downstream must not run")
	}
	if status != http.StatusServiceUnavailable || body["code"] != "auth.policy_unavailable" {
		t.Fatalf("an outage must be reported as an outage, got %d %v", status, body["code"])
	}
}

func TestRequireStepUp_MissingPolicyIs503OnPasswordConfirmBranch(t *testing.T) {
	claims := &authModels.JWTClaims{UserUUID: "u-1", Audience: "operator", AMR: []string{"pwd"}}
	called, status, body := runStepUpWithPolicy(t, claims, false, nil, nil /*no StepUpPolicy*/)
	if called {
		t.Fatal("downstream must not run")
	}
	if status != http.StatusServiceUnavailable || body["code"] != "auth.policy_unavailable" {
		t.Fatalf("missing wiring must fail closed, got %d %v", status, body["code"])
	}
}

func TestRequireStepUp_ReauthAllowedKeepsPasswordConfirm(t *testing.T) {
	claims := &authModels.JWTClaims{UserUUID: "u-1", Audience: "operator", AMR: []string{"pwd"}}
	called, status, body := runStepUpWithPolicy(t, claims, false, nil, &fakeStepUpPolicy{})
	if called {
		t.Fatal("downstream must not run")
	}
	if status != http.StatusUnauthorized || body["code"] != "password_confirm_required" {
		t.Fatalf("allowed method keeps today's envelope, got %d %v", status, body["code"])
	}
}

// runRequireMFA wires a minimal AuthMiddleware, optionally sets a step-up
// policy (so RequireMFA can consult the master switch), seeds the request
// with claims, and invokes RequireMFA(). Returns (downstreamRan, status, body).
func runRequireMFA(t *testing.T, claims *authModels.JWTClaims, policy StepUpPolicy) (bool, int, map[string]any) {
	t.Helper()
	m := newTestMiddleware(&fakeAuthz{}, &fakeTenantProvider{}, nil)
	if policy != nil {
		m.SetStepUpPolicy(policy)
	}

	req := httptest.NewRequest(http.MethodPatch, "/v1/admin/modules/marketing", nil)
	if claims != nil {
		req = req.WithContext(context.WithValue(req.Context(), ctxClaims, claims))
	}
	rec := httptest.NewRecorder()

	called := false
	handler := m.RequireMFA()(http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {
		called = true
	}))
	handler.ServeHTTP(rec, req)

	var body map[string]any
	if rec.Body.Len() > 0 {
		_ = json.Unmarshal(rec.Body.Bytes(), &body)
	}
	return called, rec.Code, body
}

func TestRequireMFA_NonMFAAmrBlockedWhenSwitchOn(t *testing.T) {
	// Master switch on (policy reports MFAEnabled=true) and a password-only
	// token → the gate must block. This is the pre-fix behaviour preserved.
	claims := &authModels.JWTClaims{UserUUID: "u-1", AMR: []string{"pwd"}}
	called, status, body := runRequireMFA(t, claims, &fakeStepUpPolicy{})
	if called {
		t.Error("password-only token must be blocked when MFA master switch is on")
	}
	if status != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", status)
	}
	if code, _ := body["code"].(string); code != "step_up_required" {
		t.Errorf("body.code = %q, want step_up_required", code)
	}
}

func TestRequireMFA_NonMFAAmrPassesWhenSwitchOff(t *testing.T) {
	// Master switch OFF (policy reports MFAEnabled=false). A never-enrolled
	// operator with a password-only token must be allowed through — turning
	// MFA off globally must not wall the operator out of MFA-gated admin
	// writes (the bug: module enable demanded an MFA proof that can't exist).
	claims := &authModels.JWTClaims{UserUUID: "u-1", AMR: []string{"pwd"}}
	called, status, _ := runRequireMFA(t, claims, &fakeStepUpPolicy{mfaDisabled: true})
	if !called {
		t.Errorf("master switch off must pass password-only token through; status %d", status)
	}
	if status != http.StatusOK {
		t.Errorf("status = %d, want 200", status)
	}
}

func TestRequireMFA_MFAAmrPassesWhenSwitchOn(t *testing.T) {
	// A token carrying a real second-factor proof passes regardless.
	claims := &authModels.JWTClaims{UserUUID: "u-1", AMR: []string{"pwd", "otp"}}
	called, status, _ := runRequireMFA(t, claims, &fakeStepUpPolicy{})
	if !called {
		t.Errorf("MFA-proof token must pass when switch on; status %d", status)
	}
	if status != http.StatusOK {
		t.Errorf("status = %d, want 200", status)
	}
}

func TestRequireMFA_NoPolicyFallsBackToLegacyGate(t *testing.T) {
	// When no step-up policy is wired (legacy/degraded), the gate must keep
	// its original unconditional behaviour: password-only token is blocked.
	claims := &authModels.JWTClaims{UserUUID: "u-1", AMR: []string{"pwd"}}
	called, status, _ := runRequireMFA(t, claims, nil)
	if called {
		t.Error("with no policy wired, password-only token must still be blocked")
	}
	if status != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", status)
	}
}

// --- RequireEnrolmentProof (H-2 / H-3) ---
//
// Enrolment was mounted under RequireGlobal() alone, so a stolen
// session-only bearer could add a passkey on the victim's account — or
// REPLACE their TOTP secret, since ConfirmEnrollment deletes the existing
// factor after validating a code for the NEW one — and then own the
// account outright. Every enrolment now demands a fresh proof.
//
// These helpers are a NEW family, deliberately not an extension of the
// runStepUp* trio above: those return (downstreamRan, status, body) and
// have many callers, while these assert on headers too and so need the
// recorder itself.

// enrolmentLookupFactor is the MFAEnrollmentLookup double that answers a
// fixed "has a factor" verdict with no error.
func enrolmentLookupFactor(hasFactor bool) MFAEnrollmentLookup {
	return func(_ context.Context, _, _ string) (bool, error) { return hasFactor, nil }
}

// enrolmentLookupErr is the degraded-Mongo double: it answers an error,
// which the gate must treat as "presence unknown" and fail closed on.
func enrolmentLookupErr(err error) MFAEnrollmentLookup {
	return func(_ context.Context, _, _ string) (bool, error) { return false, err }
}

// runEnrolmentGate builds a minimal AuthMiddleware, wires the given
// enrolment lookup (nil = SetMFAEnrollmentLookup never called), seeds the
// request context with claims, and drives RequireEnrolmentProof(maxAge)
// around a handler that writes 204 — so a pass case asserts a status the
// downstream handler alone can produce, never the recorder's zero value.
func runEnrolmentGate(t *testing.T, claims *authModels.JWTClaims, lookup MFAEnrollmentLookup, maxAge time.Duration) *httptest.ResponseRecorder {
	t.Helper()
	m := newTestMiddleware(&fakeAuthz{}, &fakeTenantProvider{}, nil)
	if lookup != nil {
		m.SetMFAEnrollmentLookup(lookup)
	}

	req := httptest.NewRequest(http.MethodPost, "/v1/auth/operator/mfa/enroll/begin", nil)
	if claims != nil {
		req = req.WithContext(context.WithValue(req.Context(), ctxClaims, claims))
	}
	rec := httptest.NewRecorder()
	// 204, not the recorder's default 200: a gate that neither called next
	// nor wrote anything would leave the zero value behind and satisfy a
	// 200 assertion, so every pass case here asserts a status only the
	// downstream handler can produce.
	m.RequireEnrolmentProof(maxAge)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})).ServeHTTP(rec, req)
	return rec
}

// assertCodedError decodes the coded envelope and asserts status + the
// flat top-level `code` a client branches on, returning the body so a
// caller can assert the extra fields.
func assertCodedError(t *testing.T, rec *httptest.ResponseRecorder, wantStatus int, wantCode string) map[string]any {
	t.Helper()
	if rec.Code != wantStatus {
		t.Fatalf("status = %d, want %d (body %q)", rec.Code, wantStatus, rec.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode body %q: %v", rec.Body.String(), err)
	}
	if code, _ := body["code"].(string); code != wantCode {
		t.Fatalf("body.code = %q, want %q", code, wantCode)
	}
	return body
}

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
	claims := &authModels.JWTClaims{
		UserUUID: "u-1", AMR: []string{"pwd", "otp"}, LastOTPAt: time.Now().Unix(),
	}
	rec := runEnrolmentGate(t, claims, enrolmentLookupFactor(true), 5*time.Minute)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204 — a fresh second factor is the proof", rec.Code)
	}
}

func TestRequireEnrolmentProof_WithFactorStaleProofIsStepUp(t *testing.T) {
	claims := &authModels.JWTClaims{
		UserUUID: "u-1", AMR: []string{"pwd", "otp"},
		LastOTPAt: time.Now().Add(-10 * time.Minute).Unix(),
	}
	rec := runEnrolmentGate(t, claims, enrolmentLookupFactor(true), 5*time.Minute)
	assertCodedError(t, rec, http.StatusUnauthorized, "step_up_required")
}

func TestRequireEnrolmentProof_WithFactorNoProofIsStepUp(t *testing.T) {
	claims := &authModels.JWTClaims{UserUUID: "u-1", AMR: []string{"pwd"}}
	rec := runEnrolmentGate(t, claims, enrolmentLookupFactor(true), 5*time.Minute)
	assertCodedError(t, rec, http.StatusUnauthorized, "step_up_required")
}

// A user WITH a factor and a fresh auth_time is STILL step_up_required:
// the recent login is not the proof their branch asks for, so auth_time
// must not leak across the factor branch.
func TestRequireEnrolmentProof_WithFactorFreshAuthTimeIsStillStepUp(t *testing.T) {
	claims := &authModels.JWTClaims{
		UserUUID: "u-1", AMR: []string{"pwd"}, AuthTime: time.Now().Unix(),
	}
	rec := runEnrolmentGate(t, claims, enrolmentLookupFactor(true), 5*time.Minute)
	assertCodedError(t, rec, http.StatusUnauthorized, "step_up_required")
}

// A user WITHOUT a factor proves presence with a recent interactive
// login. This is the branch that lets an MFA-obligated account in its
// grace window enrol at all — password-confirm refuses those (D19).
func TestRequireEnrolmentProof_NoFactorFreshAuthTimePasses(t *testing.T) {
	claims := &authModels.JWTClaims{
		UserUUID: "u-1", AMR: []string{"pwd"}, AuthTime: time.Now().Add(-time.Minute).Unix(),
	}
	rec := runEnrolmentGate(t, claims, enrolmentLookupFactor(false), 5*time.Minute)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204", rec.Code)
	}
}

// A fresh reauth (password reconfirm) is also a fresh proof, for the
// users that endpoint still serves.
func TestRequireEnrolmentProof_NoFactorFreshReauthPasses(t *testing.T) {
	claims := &authModels.JWTClaims{
		UserUUID: "u-1", AMR: []string{"pwd", "reauth"}, LastOTPAt: time.Now().Unix(),
	}
	rec := runEnrolmentGate(t, claims, enrolmentLookupFactor(false), 5*time.Minute)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204", rec.Code)
	}
}

func TestRequireEnrolmentProof_NoFactorStaleAuthTimeIsReauth(t *testing.T) {
	authTime := time.Now().Add(-30 * time.Minute).Unix()
	claims := &authModels.JWTClaims{UserUUID: "u-1", AMR: []string{"pwd"}, AuthTime: authTime}
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
	claims := &authModels.JWTClaims{UserUUID: "u-1", AMR: []string{"pwd"}}
	rec := runEnrolmentGate(t, claims, enrolmentLookupFactor(false), 5*time.Minute)
	assertCodedError(t, rec, http.StatusUnauthorized, "reauthentication_required")
}

// A dev token is exactly the pre-deploy shape (no amr, no last_otp_at,
// and by R3 no auth_time), so the four enrolment endpoints are
// unreachable with one. Pinned so the consequence stays deliberate.
func TestRequireEnrolmentProof_DevTokenShapeIsReauth(t *testing.T) {
	claims := &authModels.JWTClaims{UserUUID: "u-1", SystemRole: "administrator"}
	rec := runEnrolmentGate(t, claims, enrolmentLookupFactor(false), 5*time.Minute)
	assertCodedError(t, rec, http.StatusUnauthorized, "reauthentication_required")
}

// FAIL CLOSED. A degraded Mongo must not let a factor be added without
// proof, so an unavailable lookup refuses every caller who has not
// ALREADY presented a fresh second factor, with step_up_required. That
// answer is satisfiable: the proof it asks for lives in the signed token,
// so an enrolled caller can step up and come back (the test below), while
// a caller with no factor cannot enrol until the lookup recovers — spec
// §5 edge case 9. auth_time is not accepted on this path, because
// "is auth_time enough?" is only answerable once we know the caller has
// no factor, and that is the very question the lookup failed.
func TestRequireEnrolmentProof_LookupErrorFailsClosed(t *testing.T) {
	claims := &authModels.JWTClaims{
		UserUUID: "u-1", AMR: []string{"pwd"}, AuthTime: time.Now().Unix(),
	}
	rec := runEnrolmentGate(t, claims, enrolmentLookupErr(errors.New("mongo down")), 5*time.Minute)
	assertCodedError(t, rec, http.StatusUnauthorized, "step_up_required")
}

func TestRequireEnrolmentProof_NilLookupFailsClosed(t *testing.T) {
	claims := &authModels.JWTClaims{
		UserUUID: "u-1", AMR: []string{"pwd"}, AuthTime: time.Now().Unix(),
	}
	rec := runEnrolmentGate(t, claims, nil, 5*time.Minute)
	assertCodedError(t, rec, http.StatusUnauthorized, "step_up_required")
}

// The gate does NOT honour the mfaEnabled master switch, unlike
// RequireMFA: asking "did you prove presence" is meaningful whether or
// not MFA is enforced, and no bootstrap deadlock exists here (a fresh
// login satisfies the no-factor branch).
func TestRequireEnrolmentProof_MasterSwitchOffStillGates(t *testing.T) {
	m := newTestMiddleware(&fakeAuthz{}, &fakeTenantProvider{}, nil)
	m.SetMFAEnrollmentLookup(enrolmentLookupFactor(false))
	m.SetStepUpPolicy(&fakeStepUpPolicy{mfaDisabled: true})

	req := httptest.NewRequest(http.MethodPost, "/v1/auth/operator/mfa/enroll/begin", nil)
	req = req.WithContext(context.WithValue(req.Context(), ctxClaims,
		&authModels.JWTClaims{UserUUID: "u-1", AMR: []string{"pwd"}}))
	rec := httptest.NewRecorder()
	m.RequireEnrolmentProof(5*time.Minute)(http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {
	})).ServeHTTP(rec, req)

	assertCodedError(t, rec, http.StatusUnauthorized, "reauthentication_required")
}

// Zero maxAge defaults to 5 minutes, matching RequireStepUp.
func TestRequireEnrolmentProof_DefaultMaxAgeWhenZero(t *testing.T) {
	claims := &authModels.JWTClaims{
		UserUUID: "u-1", AMR: []string{"pwd"}, AuthTime: time.Now().Add(-2 * time.Minute).Unix(),
	}
	rec := runEnrolmentGate(t, claims, enrolmentLookupFactor(false), 0)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204 — 2-min-old login under the default 5min window", rec.Code)
	}
}

// The other half of edge case 9: the step_up_required an outage hands back
// must be an answer the caller can actually act on. A caller who steps up
// and retries carries the proof IN THE TOKEN, so the gate can honour it
// without the lookup that is still down. Before the freshness check was
// hoisted above the lookup, this returned step_up_required forever — a
// challenge the caller had already satisfied.
func TestRequireEnrolmentProof_LookupErrorFreshFactorPasses(t *testing.T) {
	claims := &authModels.JWTClaims{
		UserUUID: "u-1", AMR: []string{"pwd", "otp"}, LastOTPAt: time.Now().Unix(),
	}
	rec := runEnrolmentGate(t, claims, enrolmentLookupErr(errors.New("mongo down")), 5*time.Minute)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204 \u2014 a fresh factor needs no lookup", rec.Code)
	}
}

// Same, with the lookup never wired at all.
func TestRequireEnrolmentProof_NilLookupFreshFactorPasses(t *testing.T) {
	claims := &authModels.JWTClaims{
		UserUUID: "u-1", AMR: []string{"pwd", "webauthn"}, LastOTPAt: time.Now().Unix(),
	}
	rec := runEnrolmentGate(t, claims, nil, 5*time.Minute)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204", rec.Code)
	}
}

// M-1/M-4: the two predicates now differ on exactly one marker, "reauth",
// and agree on every other. This replaces the seam-era test that asserted
// they agreed everywhere: that one existed to catch an ACCIDENTAL
// divergence while the lists were duplicated literals, and the divergence
// it guarded against is the deliberate change M-1 asks for.
//
// The table is exhaustive over the marker vocabulary, so it also pins the
// answers themselves \u2014 an edit to models.IsSecondFactorAMR that quietly
// admitted device_trust, say, would fail here rather than only in an
// integration test.
func TestAMRPredicates_DifferOnlyOnReauth(t *testing.T) {
	for _, tc := range []struct {
		amr            []string
		wantMFA        bool
		wantStepUp     bool
		wantEpochBound bool
	}{
		{nil, false, false, false},
		{[]string{}, false, false, false},
		{[]string{"pwd"}, false, false, false},
		{[]string{"oauth"}, false, false, false},
		{[]string{"otp"}, true, true, true},
		{[]string{"webauthn"}, true, true, true},
		{[]string{"mfa"}, true, true, true},
		// The one divergence: a password reconfirm proves presence for a
		// step-up but is not a second factor, and the MFA epoch does not
		// govern it (a password is not an MFA credential).
		{[]string{"reauth"}, false, true, false},
		{[]string{"pwd", "reauth"}, false, true, false},
		// device_trust is epoch-governed \u2014 the trust was granted on the
		// strength of a factor \u2014 but never a second factor on its own.
		{[]string{"device_trust"}, false, false, true},
		{[]string{"pwd", "otp"}, true, true, true},
		{[]string{"oauth", "webauthn"}, true, true, true},
		{[]string{"pwd", "otp", "device_trust"}, true, true, true},
	} {
		if got := amrSatisfiesMFA(tc.amr); got != tc.wantMFA {
			t.Errorf("amr %v: amrSatisfiesMFA = %v, want %v", tc.amr, got, tc.wantMFA)
		}
		if got := amrSatisfiesStepUp(tc.amr); got != tc.wantStepUp {
			t.Errorf("amr %v: amrSatisfiesStepUp = %v, want %v", tc.amr, got, tc.wantStepUp)
		}
		if got := authModels.HasEpochBoundAMR(tc.amr); got != tc.wantEpochBound {
			t.Errorf("amr %v: HasEpochBoundAMR = %v, want %v", tc.amr, got, tc.wantEpochBound)
		}
	}
}
