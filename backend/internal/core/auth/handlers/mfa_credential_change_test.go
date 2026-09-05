package handlers

import (
	"context"
	"encoding/base64"
	"errors"
	"sync"
	"testing"
	"time"

	authModels "github.com/orkestra/backend/internal/core/auth/models"
	"github.com/orkestra/backend/internal/core/auth/services"
	"github.com/orkestra/backend/pkg/sdk/iface"
)

// M-2 (spec §4.3 D16): the HANDLER halves of the one rule. Only the handler
// can learn the caller's session id — it lives on the request claims — so
// session termination lives here and not on the services (ruling R5). The
// epoch bump, the device-trust revoke and the security events are asserted
// in package services (mfa_credential_change_test.go there).

// --- fakes ---------------------------------------------------------------

// recordingSessions is the services.AuthService slice these handlers use:
// the two revocation methods, recorded. Everything else is the embedded nil
// interface, so a handler that starts calling something new panics loudly
// instead of silently succeeding.
type recordingSessions struct {
	services.AuthService
	mu            sync.Mutex
	revokedExcept map[string]string
	terminated    map[string]int
	fail          error
}

func newRecordingSessions() *recordingSessions {
	return &recordingSessions{revokedExcept: map[string]string{}, terminated: map[string]int{}}
}

func (s *recordingSessions) RevokeAllUserSessionsExcept(_ context.Context, userUUID, currentSid string) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.fail != nil {
		return 0, s.fail
	}
	s.revokedExcept[userUUID] = currentSid
	return 1, nil
}

func (s *recordingSessions) TerminateAllSessionsByUUID(_ context.Context, userUUID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.fail != nil {
		return s.fail
	}
	s.terminated[userUUID]++
	return nil
}

func (s *recordingSessions) revokedExceptFor(userUUID string) (string, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	sid, ok := s.revokedExcept[userUUID]
	return sid, ok
}

func (s *recordingSessions) terminatedAll(userUUID string) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.terminated[userUUID]
}

// credChangeMFA is the MFAService slice the two changed handlers call.
type credChangeMFA struct {
	services.MFAService
	removeErr  error
	replaced   bool
	confirmErr error
	// totpEnrolled is what Status reports. It is the TOTP half of the
	// passkey path's "was that the last factor?" question.
	totpEnrolled bool
}

func (m *credChangeMFA) RemoveFactor(context.Context, string, string) error { return m.removeErr }

func (m *credChangeMFA) Status(context.Context, string) (*services.MFAStatusSnapshot, error) {
	if m.totpEnrolled {
		return &services.MFAStatusSnapshot{Status: authModels.MFAStatusEnrolled, Type: authModels.MFAFactorTOTP}, nil
	}
	return &services.MFAStatusSnapshot{Status: authModels.MFAStatusNotRequired}, nil
}

func (m *credChangeMFA) ConfirmEnrollment(context.Context, string, string, string) ([]string, bool, error) {
	if m.confirmErr != nil {
		return nil, m.replaced, m.confirmErr
	}
	return []string{"AAAA-BBBB"}, m.replaced, nil
}

// credChangeWebAuthn is the WebAuthnService slice the passkey DELETE calls.
type credChangeWebAuthn struct {
	services.WebAuthnService
	removed bool
	err     error
	// remaining is what HasCredentials reports AFTER the delete — the
	// passkey half of "was that the last factor?".
	remaining bool
}

func (w *credChangeWebAuthn) RemoveCredential(context.Context, string, []byte) (bool, error) {
	return w.removed, w.err
}

func (w *credChangeWebAuthn) HasCredentials(context.Context, string) (bool, error) {
	return w.remaining, nil
}

// credChangeUsers is the user store the grace clock lives in. It models the
// one field that matters — MFAGraceStartedAt — so a test can ask the real
// policy predicate whether the user would still be refused at login, rather
// than merely asserting that a method was called.
type credChangeUsers struct {
	iface.UserProvider
	mu    sync.Mutex
	users map[string]*iface.User
	calls map[string]int
}

func newCredChangeUsers() *credChangeUsers {
	return &credChangeUsers{users: map[string]*iface.User{}, calls: map[string]int{}}
}

// withGraceStartedAt seeds a privileged user whose enrolment grace clock
// started at the given instant — for a long-standing administrator that is
// their first-ever privileged login, which is why the window has lapsed.
func (u *credChangeUsers) withGraceStartedAt(userUUID string, started time.Time) *iface.User {
	u.mu.Lock()
	defer u.mu.Unlock()
	rec := &iface.User{UUID: userUUID, Role: services.SystemRoleAdministrator, MFAGraceStartedAt: &started}
	u.users[userUUID] = rec
	return rec
}

func (u *credChangeUsers) record(userUUID string) *iface.User {
	u.mu.Lock()
	defer u.mu.Unlock()
	return u.users[userUUID]
}

func (u *credChangeUsers) resetCalls(userUUID string) int {
	u.mu.Lock()
	defer u.mu.Unlock()
	return u.calls[userUUID]
}

func (u *credChangeUsers) ResetMFAGrace(_ context.Context, userUUID string) error {
	u.mu.Lock()
	defer u.mu.Unlock()
	u.calls[userUUID]++
	now := time.Now()
	if rec, ok := u.users[userUUID]; ok {
		rec.MFAGraceStartedAt = &now
	}
	return nil
}

// recordedAdminEvent is one admin_mfa_reset audit row as the handler
// produced it.
type recordedAdminEvent struct {
	eventType string
	target    string
	fields    map[string]interface{}
}

type recordingAdminAudit struct{ rows []recordedAdminEvent }

func (r *recordingAdminAudit) RecordAdminAuthEvent(_ context.Context, eventType, _, targetUUID string, fields map[string]interface{}) {
	r.rows = append(r.rows, recordedAdminEvent{eventType: eventType, target: targetUUID, fields: fields})
}

// rowFor returns the last recorded row for a target, if any.
func (r *recordingAdminAudit) rowFor(target string) (recordedAdminEvent, bool) {
	for i := len(r.rows) - 1; i >= 0; i-- {
		if r.rows[i].target == target {
			return r.rows[i], true
		}
	}
	return recordedAdminEvent{}, false
}

// credChangeCtx builds the context the auth middleware would have built:
// the caller's uuid under "userUUID" and the claims (carrying sid) under
// "claims" — the untyped keys AuthMiddleware.setUserContext writes.
func credChangeCtx(userUUID, sid string) context.Context {
	ctx := context.WithValue(context.Background(), "userUUID", userUUID) //nolint:staticcheck // the handler seam reads this untyped key
	return context.WithValue(ctx, "claims", &authModels.JWTClaims{       //nolint:staticcheck // ditto, middleware writes a plain string key
		UserUUID:  userUUID,
		SessionID: sid,
	})
}

func newCredChangeMFAHandler(mfa services.MFAService, sessions services.AuthService) (*MFAHandler, *recordingAdminAudit) {
	h, audit, _ := newCredChangeMFAHandlerWithUsers(mfa, sessions, newCredChangeUsers())
	return h, audit
}

func newCredChangeMFAHandlerWithUsers(mfa services.MFAService, sessions services.AuthService, users *credChangeUsers) (*MFAHandler, *recordingAdminAudit, *credChangeUsers) {
	h := NewMFAHandler(mfa, nil, nil, users, nil, "cookie", "", false)
	audit := &recordingAdminAudit{}
	h.SetAuditRecorder(audit)
	h.SetSessionTerminator(sessions)
	return h, audit, users
}

// --- admin reset ---------------------------------------------------------

func TestAdminReset_TerminatesEverySessionIncludingTheTargets(t *testing.T) {
	sessions := newRecordingSessions()
	h, audit := newCredChangeMFAHandler(&credChangeMFA{}, sessions)

	if _, err := h.AdminReset(credChangeCtx("admin-1", "sid-admin"), &MFAAdminResetRequest{UserID: "target-1"}); err != nil {
		t.Fatalf("AdminReset: %v", err)
	}
	if got := sessions.terminatedAll("target-1"); got != 1 {
		t.Fatalf("terminated %d times, want 1 — the admin path ends EVERY session, including the target's current one", got)
	}
	if _, ok := sessions.revokedExceptFor("target-1"); ok {
		t.Fatal("the admin path must not spare a session: the caller is not the target")
	}
	row, ok := audit.rowFor("target-1")
	if !ok || row.eventType != "admin_mfa_reset" {
		t.Fatalf("audit row = %+v, want admin_mfa_reset", row)
	}
	if row.fields["sessions_terminated"] != true {
		t.Fatalf("metadata sessions_terminated = %v, want true", row.fields["sessions_terminated"])
	}
}

// A failed termination must not fail the reset: the epoch has already ended
// MFA authority everywhere, so what is left is ordinary session access —
// the same exposure as any degraded revocation. But it must be visible.
func TestAdminReset_TerminationFailureIsRecordedNotFatal(t *testing.T) {
	sessions := newRecordingSessions()
	sessions.fail = errors.New("mongo down")
	h, audit := newCredChangeMFAHandler(&credChangeMFA{}, sessions)

	if _, err := h.AdminReset(credChangeCtx("admin-1", "sid-admin"), &MFAAdminResetRequest{UserID: "target-2"}); err != nil {
		t.Fatalf("AdminReset must still succeed: %v", err)
	}
	row, ok := audit.rowFor("target-2")
	if !ok {
		t.Fatal("a reset that degraded must still be audited")
	}
	if row.fields["sessions_terminated"] != false {
		t.Fatalf("metadata sessions_terminated = %v, want false", row.fields["sessions_terminated"])
	}
}

// R14: the partial-deletion error Task 4 introduced reaches the 500 branch,
// which wrote no audit row at all — a half-reset account left no trace.
func TestAdminReset_FailureIsAudited(t *testing.T) {
	sessions := newRecordingSessions()
	h, audit := newCredChangeMFAHandler(&credChangeMFA{removeErr: errors.New("half deleted")}, sessions)

	if _, err := h.AdminReset(credChangeCtx("admin-1", "sid-admin"), &MFAAdminResetRequest{UserID: "target-3"}); err == nil {
		t.Fatal("a failed removal must still surface as an error")
	}
	row, ok := audit.rowFor("target-3")
	if !ok {
		t.Fatal("a failed reset must leave an audit row — it may have half-applied")
	}
	// A DISTINCT type, not admin_mfa_reset with a metadata flag:
	// recordAuthEvent hardcodes the outcome to success for every auth
	// event, so a failure filed under the success type is indexed as a
	// successful reset and an evidence query never surfaces it.
	if row.eventType != "admin_mfa_reset_failed" {
		t.Fatalf("event type = %q, want admin_mfa_reset_failed", row.eventType)
	}
	if row.fields["outcome"] != "failed" {
		t.Fatalf("metadata outcome = %v, want \"failed\"", row.fields["outcome"])
	}
	// Metadata reaches GET /v1/admin/audit-events, so it carries a
	// classified kind and never the driver's own error text (which can
	// echo namespaces, filter fragments and server addresses).
	if row.fields["error_kind"] != "removal_failed" {
		t.Fatalf("metadata error_kind = %v, want \"removal_failed\"", row.fields["error_kind"])
	}
	if _, present := row.fields["error"]; present {
		t.Fatal("raw error text must not reach the audit metadata — it is serialised to an admin API response")
	}
	// M-1: the consequences follow the DESTRUCTION, not the success of the
	// call. A part-way removal (one factor row deleted, the other not) has
	// already bumped the epoch via the service's defer; leaving the
	// target's other sessions alive is the exact half-applied state this
	// branch's audit row exists to make visible.
	if sessions.terminatedAll("target-3") != 1 {
		t.Fatalf("terminated %d times, want 1 — a part-way removal destroyed a credential, so the sessions it authorised must still end", sessions.terminatedAll("target-3"))
	}
	if row.fields["sessions_terminated"] != true {
		t.Fatalf("metadata sessions_terminated = %v, want true", row.fields["sessions_terminated"])
	}
}

// "Nothing to reset" is not a failure and changes no state, so it stays a
// 404 with no audit row.
func TestAdminReset_NotEnrolledIs404AndNotAudited(t *testing.T) {
	sessions := newRecordingSessions()
	h, audit := newCredChangeMFAHandler(&credChangeMFA{removeErr: services.ErrMFANotEnrolled}, sessions)

	if _, err := h.AdminReset(credChangeCtx("admin-1", "sid-admin"), &MFAAdminResetRequest{UserID: "target-4"}); err == nil {
		t.Fatal("want 404")
	}
	if _, ok := audit.rowFor("target-4"); ok {
		t.Fatal("a no-op reset must not write an audit row")
	}
}

// --- self removal --------------------------------------------------------

// The self path spares the caller's own session — they are signed in and
// just proved themselves — but its MFA authority is gone with the next
// gated request, via the epoch.
func TestSelfRemove_RevokesEveryOtherSession(t *testing.T) {
	sessions := newRecordingSessions()
	h, _ := newCredChangeMFAHandler(&credChangeMFA{}, sessions)

	if _, err := h.Remove(credChangeCtx("u-1", "sid-current"), &MFARemoveRequest{}); err != nil {
		t.Fatalf("Remove: %v", err)
	}
	sid, ok := sessions.revokedExceptFor("u-1")
	if !ok {
		t.Fatal("self removal must revoke the user's other sessions")
	}
	if sid != "sid-current" {
		t.Fatalf("revokedExcept = %q, want the caller's own sid", sid)
	}
	if sessions.terminatedAll("u-1") != 0 {
		t.Fatal("the self path must not terminate the caller's own session")
	}
}

// ErrMFANotEnrolled — and ONLY that error — means nothing existed, so
// nothing was destroyed and no consequence may be applied. Every other
// failure is covered by its sibling below.
func TestSelfRemove_NotEnrolledRevokesNothing(t *testing.T) {
	sessions := newRecordingSessions()
	h, _ := newCredChangeMFAHandler(&credChangeMFA{removeErr: services.ErrMFANotEnrolled}, sessions)

	if _, err := h.Remove(credChangeCtx("u-2", "sid-current"), &MFARemoveRequest{}); err == nil {
		t.Fatal("want an error")
	}
	if _, ok := sessions.revokedExceptFor("u-2"); ok {
		t.Fatal("nothing was removed — no session may be revoked")
	}
}

// The sibling, and the half the admin path already pinned: a removal that
// fails for any OTHER reason may have destroyed a credential part-way
// (RemoveFactor deletes the TOTP row before the WebAuthn one and applies
// the epoch bump from a defer), so the sessions that credential authorised
// must still end — and the grace clock must still restart, or the caller
// can be left factor-less and locked out by a 500. Without this test the
// self path silently regresses to success-gating while every other test in
// the package stays green.
func TestSelfRemove_PartialFailureStillAppliesTheConsequences(t *testing.T) {
	ctx := credChangeCtx("u-partial", "sid-current")
	users := newCredChangeUsers()
	users.withGraceStartedAt("u-partial", time.Now().Add(-90*24*time.Hour))
	sessions := newRecordingSessions()
	h, _, _ := newCredChangeMFAHandlerWithUsers(&credChangeMFA{removeErr: errors.New("half deleted")}, sessions, users)

	if _, err := h.Remove(ctx, &MFARemoveRequest{}); err == nil {
		t.Fatal("the removal failure must still surface to the caller")
	}
	sid, ok := sessions.revokedExceptFor("u-partial")
	if !ok {
		t.Fatal("a part-way removal destroyed a credential: the sessions it authorised must still end")
	}
	if sid != "sid-current" {
		t.Fatalf("revokedExcept = %q, want the caller's own sid", sid)
	}
	if users.resetCalls("u-partial") != 1 {
		t.Fatalf("ResetMFAGrace called %d times, want 1 — a 500 must not leave the caller factor-less with a lapsed grace window", users.resetCalls("u-partial"))
	}
	if gate := graceGate(); gate.MFAGraceExpired(ctx, users.record("u-partial"), time.Now()) {
		t.Fatal("the grace window is still expired after a part-way removal — the same lockout, reached through the failure path")
	}
}

// --- TOTP replacement ----------------------------------------------------

// A replacement is a removal of the old secret, so the other sessions go
// with it. The service reports the replacement outward (it cannot see the
// caller's sid); the handler acts on it.
func TestEnrollConfirm_ReplacementRevokesEveryOtherSession(t *testing.T) {
	sessions := newRecordingSessions()
	h, _ := newCredChangeMFAHandler(&credChangeMFA{replaced: true}, sessions)

	req := &MFAEnrollConfirmRequest{}
	req.Body.ChallengeID, req.Body.Code = "ch-1", "123456"
	if _, err := h.EnrollConfirm(credChangeCtx("u-3", "sid-current"), req); err != nil {
		t.Fatalf("EnrollConfirm: %v", err)
	}
	sid, ok := sessions.revokedExceptFor("u-3")
	if !ok {
		t.Fatal("replacing a TOTP factor must revoke the caller's other sessions")
	}
	if sid != "sid-current" {
		t.Fatalf("revokedExcept = %q, want the caller's own sid", sid)
	}
}

func TestEnrollConfirm_FirstEnrolmentRevokesNothing(t *testing.T) {
	sessions := newRecordingSessions()
	h, _ := newCredChangeMFAHandler(&credChangeMFA{replaced: false}, sessions)

	req := &MFAEnrollConfirmRequest{}
	req.Body.ChallengeID, req.Body.Code = "ch-1", "123456"
	if _, err := h.EnrollConfirm(credChangeCtx("u-4", "sid-current"), req); err != nil {
		t.Fatalf("EnrollConfirm: %v", err)
	}
	if _, ok := sessions.revokedExceptFor("u-4"); ok {
		t.Fatal("an addition invalidates nothing — no session may be revoked")
	}
}

// --- passkey DELETE ------------------------------------------------------

func TestWebAuthnRemove_RevokesEveryOtherSession(t *testing.T) {
	sessions := newRecordingSessions()
	h := NewWebAuthnHandler(&credChangeWebAuthn{removed: true}, nil, nil, newCredChangeUsers(), nil, "cookie", "", false)
	h.SetSessionTerminator(sessions)

	req := &webAuthnRemoveRequest{CredentialID: base64.RawURLEncoding.EncodeToString([]byte("cred-a"))}
	if _, err := h.Remove(credChangeCtx("u-5", "sid-current"), req); err != nil {
		t.Fatalf("Remove: %v", err)
	}
	sid, ok := sessions.revokedExceptFor("u-5")
	if !ok {
		t.Fatal("removing a passkey must revoke the caller's other sessions, whatever else remains")
	}
	if sid != "sid-current" {
		t.Fatalf("revokedExcept = %q, want the caller's own sid", sid)
	}
}

// A 404 changed nothing, so it revokes nothing — otherwise any bearer could
// sign every other session out by looping on an unknown credential id.
func TestWebAuthnRemove_UnknownCredentialRevokesNothing(t *testing.T) {
	sessions := newRecordingSessions()
	h := NewWebAuthnHandler(&credChangeWebAuthn{removed: false}, nil, nil, newCredChangeUsers(), nil, "cookie", "", false)
	h.SetSessionTerminator(sessions)

	req := &webAuthnRemoveRequest{CredentialID: base64.RawURLEncoding.EncodeToString([]byte("nope"))}
	if _, err := h.Remove(credChangeCtx("u-6", "sid-current"), req); err == nil {
		t.Fatal("want 404")
	}
	if _, ok := sessions.revokedExceptFor("u-6"); ok {
		t.Fatal("nothing was removed — no session may be revoked")
	}
}

// The terminator is optional wiring; an unwired handler must still perform
// the removal rather than panicking on a nil interface.
func TestCredentialChange_UnwiredTerminatorDegrades(t *testing.T) {
	h, _ := newCredChangeMFAHandler(&credChangeMFA{}, nil)
	if _, err := h.Remove(credChangeCtx("u-7", "sid-current"), &MFARemoveRequest{}); err != nil {
		t.Fatalf("Remove with no session terminator = %v, want nil", err)
	}
	if _, err := h.AdminReset(credChangeCtx("admin-1", "sid-admin"), &MFAAdminResetRequest{UserID: "target-9"}); err != nil {
		t.Fatalf("AdminReset with no session terminator = %v, want nil", err)
	}
}

// The service reports replaced=true alongside an error when the old secret
// was destroyed but the new one failed to persist. Those sessions were
// authorised by a factor that no longer exists, so they must still end —
// checking `replaced` only after the error is mapped makes the value dead
// on exactly the path that needs it most.
func TestEnrollConfirm_FailedReplacementStillRevokes(t *testing.T) {
	sessions := newRecordingSessions()
	h, _ := newCredChangeMFAHandler(&credChangeMFA{
		replaced:   true,
		confirmErr: errors.New("persist failed"),
	}, sessions)

	req := &MFAEnrollConfirmRequest{}
	req.Body.ChallengeID, req.Body.Code = "ch-1", "123456"
	if _, err := h.EnrollConfirm(credChangeCtx("u-8", "sid-current"), req); err == nil {
		t.Fatal("the enrolment failure must still surface to the caller")
	}
	sid, ok := sessions.revokedExceptFor("u-8")
	if !ok {
		t.Fatal("the destroyed secret's other sessions must end even though the enrolment failed")
	}
	if sid != "sid-current" {
		t.Fatalf("revokedExcept = %q, want the caller's own sid", sid)
	}
}

// --- the enrolment grace clock (I-1) ---------------------------------------
//
// The branch that ends a removed factor's authority also made removal a
// ONE-WAY DOOR for anyone whose role obliges MFA — every administrator, by
// default. MFAGraceStartedAt is stamped at the first privileged login and
// nothing ever clears it, so for a long-standing admin the window lapsed
// long ago. Remove the last factor and the four ways back in close in
// order: enroll/begin answers reauthentication_required once auth_time goes
// stale, /me/password-confirm refuses an MFA-obliged caller (D19), the SPA
// sends them to sign in again — and completeLogin refuses THAT with
// mfa_enrollment_required, because they are privileged, factor-less, and
// out of grace. A sole administrator never gets back in.
//
// These tests walk that sequence to the point where it used to close: they
// remove the last factor and then ask the REAL policy predicate
// completeLogin consults whether the login would still be refused.

// graceGate is the predicate PasswordAuthService.completeLogin uses to
// decide "privileged, no factor, grace expired -> 403". Built from the
// production constructor with no config overrides, so it carries the
// shipped 7-day window.
func graceGate() *services.AuthPolicyService {
	return services.NewAuthPolicyServiceForTest(nil)
}

func TestSelfRemove_RestartsTheGraceClockSoEnrolmentStaysReachable(t *testing.T) {
	ctx := credChangeCtx("admin-sole", "sid-current")
	users := newCredChangeUsers()
	// A long-standing administrator: obliged to hold a factor, grace
	// clock started at their first privileged login 90 days ago.
	user := users.withGraceStartedAt("admin-sole", time.Now().Add(-90*24*time.Hour))
	gate := graceGate()

	if !gate.MFARequired(user, nil) {
		t.Fatal("fixture is wrong: the user must be MFA-obliged for this lockout to exist")
	}
	if !gate.MFAGraceExpired(ctx, user, time.Now()) {
		t.Fatal("fixture is wrong: the grace window must already have lapsed before the removal")
	}

	h, _, _ := newCredChangeMFAHandlerWithUsers(&credChangeMFA{}, newRecordingSessions(), users)
	if _, err := h.Remove(ctx, &MFARemoveRequest{}); err != nil {
		t.Fatalf("Remove: %v", err)
	}

	// The step that used to fail: their next sign-in.
	if gate.MFAGraceExpired(ctx, users.record("admin-sole"), time.Now()) {
		t.Fatal("removing the last factor left the grace window expired: the user\u2019s next login is refused with mfa_enrollment_required and enrolment is unreachable \u2014 for a sole administrator, permanently")
	}
}

// The passkey DELETE removes ONE credential, so the clock restarts only
// when that was the user's last factor. Restarting it for a user who still
// holds one would silently move a deadline they are already meeting.
func TestWebAuthnRemove_LastFactorRestartsTheGraceClock(t *testing.T) {
	ctx := credChangeCtx("admin-passkey", "sid-current")
	users := newCredChangeUsers()
	users.withGraceStartedAt("admin-passkey", time.Now().Add(-90*24*time.Hour))

	h := NewWebAuthnHandler(&credChangeWebAuthn{removed: true, remaining: false}, nil, nil, users, nil, "cookie", "", false)
	h.SetSessionTerminator(newRecordingSessions())
	h.SetMFAStatusReader(&credChangeMFA{totpEnrolled: false})

	req := &webAuthnRemoveRequest{CredentialID: base64.RawURLEncoding.EncodeToString([]byte("cred-last"))}
	if _, err := h.Remove(ctx, req); err != nil {
		t.Fatalf("Remove: %v", err)
	}
	if gate := graceGate(); gate.MFAGraceExpired(ctx, users.record("admin-passkey"), time.Now()) {
		t.Fatal("removing the last passkey left the grace window expired \u2014 the same lockout as the self path")
	}
}

func TestWebAuthnRemove_SurvivingTOTPLeavesTheGraceClockAlone(t *testing.T) {
	ctx := credChangeCtx("admin-totp", "sid-current")
	users := newCredChangeUsers()
	started := time.Now().Add(-3 * 24 * time.Hour)
	users.withGraceStartedAt("admin-totp", started)

	h := NewWebAuthnHandler(&credChangeWebAuthn{removed: true, remaining: false}, nil, nil, users, nil, "cookie", "", false)
	h.SetSessionTerminator(newRecordingSessions())
	h.SetMFAStatusReader(&credChangeMFA{totpEnrolled: true})

	req := &webAuthnRemoveRequest{CredentialID: base64.RawURLEncoding.EncodeToString([]byte("cred-a"))}
	if _, err := h.Remove(ctx, req); err != nil {
		t.Fatalf("Remove: %v", err)
	}
	if users.resetCalls("admin-totp") != 0 {
		t.Fatal("a TOTP factor survives the removal: restarting the clock would move a deadline the user is already meeting")
	}
	if got := users.record("admin-totp").MFAGraceStartedAt; got == nil || !got.Equal(started) {
		t.Fatalf("MFAGraceStartedAt = %v, want it untouched at %v", got, started)
	}
}

func TestWebAuthnRemove_SurvivingPasskeyLeavesTheGraceClockAlone(t *testing.T) {
	ctx := credChangeCtx("admin-two-keys", "sid-current")
	users := newCredChangeUsers()
	users.withGraceStartedAt("admin-two-keys", time.Now().Add(-3*24*time.Hour))

	h := NewWebAuthnHandler(&credChangeWebAuthn{removed: true, remaining: true}, nil, nil, users, nil, "cookie", "", false)
	h.SetSessionTerminator(newRecordingSessions())
	h.SetMFAStatusReader(&credChangeMFA{totpEnrolled: false})

	req := &webAuthnRemoveRequest{CredentialID: base64.RawURLEncoding.EncodeToString([]byte("cred-a"))}
	if _, err := h.Remove(ctx, req); err != nil {
		t.Fatalf("Remove: %v", err)
	}
	if users.resetCalls("admin-two-keys") != 0 {
		t.Fatal("another passkey survives the removal \u2014 the clock must not restart")
	}
}

// A DELETE that matched nothing changed no credential set, so it triggers
// none of the consequences \u2014 otherwise any bearer could restart their own
// grace clock by looping on an unknown credential id.
func TestWebAuthnRemove_UnknownCredentialLeavesTheGraceClockAlone(t *testing.T) {
	ctx := credChangeCtx("admin-nope", "sid-current")
	users := newCredChangeUsers()
	users.withGraceStartedAt("admin-nope", time.Now().Add(-90*24*time.Hour))

	h := NewWebAuthnHandler(&credChangeWebAuthn{removed: false}, nil, nil, users, nil, "cookie", "", false)
	h.SetSessionTerminator(newRecordingSessions())

	req := &webAuthnRemoveRequest{CredentialID: base64.RawURLEncoding.EncodeToString([]byte("nope"))}
	if _, err := h.Remove(ctx, req); err == nil {
		t.Fatal("want 404")
	}
	if users.resetCalls("admin-nope") != 0 {
		t.Fatal("nothing was removed \u2014 the grace clock must not restart")
	}
}

// "Nothing to remove" destroyed nothing, so it changes nothing.
func TestSelfRemove_NotEnrolledLeavesTheGraceClockAlone(t *testing.T) {
	ctx := credChangeCtx("u-none", "sid-current")
	users := newCredChangeUsers()
	users.withGraceStartedAt("u-none", time.Now().Add(-90*24*time.Hour))
	h, _, _ := newCredChangeMFAHandlerWithUsers(&credChangeMFA{removeErr: services.ErrMFANotEnrolled}, newRecordingSessions(), users)

	if _, err := h.Remove(ctx, &MFARemoveRequest{}); err == nil {
		t.Fatal("want an error")
	}
	if users.resetCalls("u-none") != 0 {
		t.Fatal("nothing was removed \u2014 no consequence may be applied")
	}
}

// The admin reset already restarted the clock on its success path. Its
// FAILURE path is the only pass that can still stamp it: the operator's
// retry answers 404 once the last factor row is gone.
func TestAdminReset_FailedRemovalStillRestartsTheGraceClock(t *testing.T) {
	ctx := credChangeCtx("admin-1", "sid-admin")
	users := newCredChangeUsers()
	users.withGraceStartedAt("target-half", time.Now().Add(-90*24*time.Hour))
	h, _, _ := newCredChangeMFAHandlerWithUsers(&credChangeMFA{removeErr: errors.New("half deleted")}, newRecordingSessions(), users)

	if _, err := h.AdminReset(ctx, &MFAAdminResetRequest{UserID: "target-half"}); err == nil {
		t.Fatal("a failed removal must still surface as an error")
	}
	if users.resetCalls("target-half") != 1 {
		t.Fatalf("ResetMFAGrace called %d times, want 1", users.resetCalls("target-half"))
	}
}
