package handlers

import (
	"context"
	"encoding/base64"
	"errors"
	"sync"
	"testing"

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
	removeErr error
	replaced  bool
}

func (m *credChangeMFA) RemoveFactor(context.Context, string, string) error { return m.removeErr }

func (m *credChangeMFA) ConfirmEnrollment(context.Context, string, string, string) ([]string, bool, error) {
	return []string{"AAAA-BBBB"}, m.replaced, nil
}

// credChangeWebAuthn is the WebAuthnService slice the passkey DELETE calls.
type credChangeWebAuthn struct {
	services.WebAuthnService
	removed bool
	err     error
}

func (w *credChangeWebAuthn) RemoveCredential(context.Context, string, []byte) (bool, error) {
	return w.removed, w.err
}

// credChangeUsers answers the admin reset's grace restart.
type credChangeUsers struct{ iface.UserProvider }

func (credChangeUsers) ResetMFAGrace(context.Context, string) error { return nil }

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
	h := NewMFAHandler(mfa, nil, nil, credChangeUsers{}, nil, "cookie", "", false)
	audit := &recordingAdminAudit{}
	h.SetAuditRecorder(audit)
	h.SetSessionTerminator(sessions)
	return h, audit
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
	if row.fields["outcome"] != "failed" {
		t.Fatalf("metadata outcome = %v, want \"failed\"", row.fields["outcome"])
	}
	if sessions.terminatedAll("target-3") != 0 {
		t.Fatal("a failed removal must not go on to terminate sessions")
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

// A failed removal must not go on to revoke sessions — nothing changed.
func TestSelfRemove_FailedRemovalRevokesNothing(t *testing.T) {
	sessions := newRecordingSessions()
	h, _ := newCredChangeMFAHandler(&credChangeMFA{removeErr: services.ErrMFANotEnrolled}, sessions)

	if _, err := h.Remove(credChangeCtx("u-2", "sid-current"), &MFARemoveRequest{}); err == nil {
		t.Fatal("want an error")
	}
	if _, ok := sessions.revokedExceptFor("u-2"); ok {
		t.Fatal("nothing was removed — no session may be revoked")
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
	h := NewWebAuthnHandler(&credChangeWebAuthn{removed: true}, nil, nil, credChangeUsers{}, nil, "cookie", "", false)
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
	h := NewWebAuthnHandler(&credChangeWebAuthn{removed: false}, nil, nil, credChangeUsers{}, nil, "cookie", "", false)
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
