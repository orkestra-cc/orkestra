package services

import (
	"context"
	"log/slog"
	"testing"
	"time"

	"github.com/go-webauthn/webauthn/webauthn"
	"github.com/pquerna/otp/totp"

	"github.com/orkestra/backend/internal/core/auth/models"
	"github.com/orkestra/backend/pkg/sdk/iface"
)

// M-2 (spec §4.2 D13, §4.3 D16): removing or replacing a credential left
// every issued token MFA-satisfied, because the markers live in the token
// until it expires. The epoch closes the caller's CURRENT token; session
// revocation (asserted at the handler layer, mfa_credential_change_test.go
// in package handlers) closes the others.
//
// These are the SERVICE-level halves: what mfaService and webAuthnService
// owe on their own — the epoch bump, the device-trust revoke, the security
// event and the "a factor was added" mail. The session terminator is not
// here because neither service can learn the caller's session id (it lives
// on the request claims); see controller ruling R5.

// --- fakes ---------------------------------------------------------------
//
// recordingDeviceTrust (gates_test.go) and fakeFactorRepo
// (mfa_service_test.go) already exist in this package and are reused; the
// three below are new seams this task introduces.

// recordingEpochBumper is an iface.MFAEpochBumper that counts bumps per
// user. err, when set, makes every bump fail — the degradation path must
// still not fail the caller.
type recordingEpochBumper struct {
	bumps map[string]int
	err   error
}

func newRecordingEpochBumper() *recordingEpochBumper {
	return &recordingEpochBumper{bumps: map[string]int{}}
}

func (e *recordingEpochBumper) BumpMFAEpoch(_ context.Context, userUUID string) (int, error) {
	if e.err != nil {
		return 0, e.err
	}
	e.bumps[userUUID]++
	return e.bumps[userUUID], nil
}

func (e *recordingEpochBumper) bumpsFor(userUUID string) int { return e.bumps[userUUID] }

// recordingSecurityEvents is a SecurityEventSink that records every event
// type it is handed, in order.
type recordingSecurityEvents struct {
	types  []string
	fields []map[string]interface{}
}

func (r *recordingSecurityEvents) RecordSelfAuthEvent(_ context.Context, eventType, _ string, fields map[string]interface{}) {
	r.types = append(r.types, eventType)
	r.fields = append(r.fields, fields)
}

func (r *recordingSecurityEvents) saw(eventType string) bool {
	for _, t := range r.types {
		if t == eventType {
			return true
		}
	}
	return false
}

// recordingMailEnqueuer is a MailEnqueuer that records the template id of
// every enqueued job AND runs its Send closure inline. Running it is the
// point: it proves the closure really reaches SendTemplated with the
// template the assertion names, rather than merely that a job with the
// right label was queued.
type recordingMailEnqueuer struct {
	templates []string
}

func (m *recordingMailEnqueuer) Enqueue(job MailJob) bool {
	m.templates = append(m.templates, job.TemplateID)
	if job.Send != nil {
		_ = job.Send(context.Background())
	}
	return true
}

func (m *recordingMailEnqueuer) enqueuedTemplate(id string) bool {
	for _, t := range m.templates {
		if t == id {
			return true
		}
	}
	return false
}

// stubFactorOwner resolves any UUID to a user with an address, which is
// all the notifier needs.
type stubFactorOwner struct{}

func (stubFactorOwner) GetUserByID(_ context.Context, id string) (*iface.User, error) {
	return &iface.User{UUID: id, Email: id + "@example.com", FullName: "Test User"}, nil
}

// credentialChangeDeps bundles the observable seams so the tests read as
// assertions about behaviour rather than about wiring.
type credentialChangeDeps struct {
	factors  *fakeFactorRepo
	trust    *recordingDeviceTrust
	epoch    *recordingEpochBumper
	events   *recordingSecurityEvents
	mail     *recordingMailEnqueuer
	notifier *gateNotifier
}

func newCredentialChangeDeps() *credentialChangeDeps {
	return &credentialChangeDeps{
		factors:  newFakeFactorRepo(),
		trust:    &recordingDeviceTrust{},
		epoch:    newRecordingEpochBumper(),
		events:   &recordingSecurityEvents{},
		mail:     &recordingMailEnqueuer{},
		notifier: &gateNotifier{configured: true},
	}
}

// factorAddedNotifier builds the real notifier over the fake mail queue and
// the fake sender — the production type, not a stand-in.
func (d *credentialChangeDeps) factorAddedNotifier() *FactorAddedNotifier {
	return NewFactorAddedNotifier(d.notifier, d.mail, stubFactorOwner{}, "Orkestra", "help@example.com", slog.Default())
}

// newMFAServiceWithDeps builds an MFAService with every credential-change
// seam wired.
func newMFAServiceWithDeps(t *testing.T) (MFAService, *credentialChangeDeps) {
	t.Helper()
	t.Setenv("MFA_SECRET_ENCRYPTION_KEY", hex32())

	deps := newCredentialChangeDeps()
	svc := NewMFAService(deps.factors, NewMFAChallengeService(NewMemoryOAuthStateStore()),
		NewPasswordService(slog.Default(), false), "Orkestra", slog.Default())
	svc.SetDeviceTrust(deps.trust)
	svc.SetEpochBumper(deps.epoch)
	svc.SetAuditSink(deps.events)
	svc.SetFactorAddedNotifier(deps.factorAddedNotifier())
	return svc, deps
}

// newWebAuthnServiceWithDeps mirrors the above for passkeys. The
// registration validator is stubbed so the ceremony can complete without a
// real authenticator — the same seam webauthn_service_test.go uses.
func newWebAuthnServiceWithDeps(t *testing.T) (*webAuthnService, *credentialChangeDeps) {
	t.Helper()
	wa, err := webauthn.New(&webauthn.Config{
		RPDisplayName: "Orkestra Test",
		RPID:          "localhost",
		RPOrigins:     []string{"http://localhost:8080"},
	})
	if err != nil {
		t.Fatalf("init webauthn: %v", err)
	}
	deps := newCredentialChangeDeps()
	svcIface := NewWebAuthnService(wa, deps.factors, NewMFAChallengeService(NewMemoryOAuthStateStore()), slog.Default())
	svcIface.SetEpochBumper(deps.epoch)
	svcIface.SetAuditSink(deps.events)
	svcIface.SetDeviceTrust(deps.trust)
	svcIface.SetFactorAddedNotifier(deps.factorAddedNotifier())
	svc := svcIface.(*webAuthnService)
	svc.registrationValidator = func(context.Context, *iface.User, webauthn.SessionData, []byte) (*webauthn.Credential, error) {
		return &webauthn.Credential{ID: []byte("cred-1"), PublicKey: []byte("pk")}, nil
	}
	return svc, deps
}

// confirmEnrolment runs a full TOTP enrolment ceremony for userUUID and
// returns whether it replaced an existing factor.
func confirmEnrolment(t *testing.T, svc MFAService, userUUID string) bool {
	t.Helper()
	begin, err := svc.BeginEnrollment(context.Background(), &iface.User{UUID: userUUID, Email: userUUID + "@example.com"})
	if err != nil {
		t.Fatalf("BeginEnrollment: %v", err)
	}
	code, err := totp.GenerateCode(begin.SecretBase32, time.Now())
	if err != nil {
		t.Fatalf("GenerateCode: %v", err)
	}
	_, replaced, err := svc.ConfirmEnrollment(context.Background(), userUUID, begin.ChallengeID, code)
	if err != nil {
		t.Fatalf("ConfirmEnrollment: %v", err)
	}
	return replaced
}

// finishRegistration runs a passkey registration for user through the
// stubbed validator.
func finishRegistration(t *testing.T, svc *webAuthnService, userUUID string) {
	t.Helper()
	user := &iface.User{UUID: userUUID, Email: userUUID + "@example.com"}
	chID, _, err := svc.BeginRegistration(context.Background(), user)
	if err != nil {
		t.Fatalf("BeginRegistration: %v", err)
	}
	if _, err := svc.FinishRegistration(context.Background(), user, chID, "Yubikey", []byte(`{}`)); err != nil {
		t.Fatalf("FinishRegistration: %v", err)
	}
}

// --- TOTP replacement ----------------------------------------------------

// A TOTP replacement is a REMOVAL of the old secret, so it carries the same
// consequences: the epoch moves, and the device-trust rows whose amr
// annotation named the old factor go with it.
func TestConfirmEnrollment_ReplacementBumpsEpochAndRevokesTrust(t *testing.T) {
	svc, deps := newMFAServiceWithDeps(t)
	deps.factors.seedTOTP(t, "u-3")

	if replaced := confirmEnrolment(t, svc, "u-3"); !replaced {
		t.Fatal("ConfirmEnrollment over an existing TOTP row must report replaced=true")
	}
	if got := deps.epoch.bumpsFor("u-3"); got != 1 {
		t.Fatalf("epoch bumped %d times, want 1 — replacing a TOTP factor must bump it", got)
	}
	if got := deps.trust.lastReason(); got != models.DeviceTrustRevokedOnMFAReplace {
		t.Fatalf("device-trust reason = %q, want %q", got, models.DeviceTrustRevokedOnMFAReplace)
	}
	if !deps.events.saw("self_mfa_factor_replaced") {
		t.Fatalf("a replacement must emit self_mfa_factor_replaced; saw %v", deps.events.types)
	}
	if !deps.mail.enqueuedTemplate("auth.mfa_factor_added") {
		t.Fatal("a replacement is still an addition of a new secret and must be announced by email")
	}
}

// A FIRST enrolment adds authority; it must not invalidate authority proven
// by a factor that still exists.
func TestConfirmEnrollment_FirstEnrolmentDoesNotBumpEpoch(t *testing.T) {
	svc, deps := newMFAServiceWithDeps(t)

	if replaced := confirmEnrolment(t, svc, "u-4"); replaced {
		t.Fatal("a first enrolment must report replaced=false")
	}
	if got := deps.epoch.bumpsFor("u-4"); got != 0 {
		t.Fatalf("epoch bumped %d times, want 0 — an addition never bumps", got)
	}
	if deps.trust.revokeCalls() != 0 {
		t.Fatal("a first enrolment must not revoke device trust")
	}
	if !deps.events.saw("self_mfa_enrolled") {
		t.Fatalf("a first enrolment must emit self_mfa_enrolled; saw %v", deps.events.types)
	}
	if !deps.mail.enqueuedTemplate("auth.mfa_factor_added") {
		t.Fatal("a first enrolment must email the user")
	}
}

// --- passkeys ------------------------------------------------------------

func TestFinishRegistration_AddingAPasskeyDoesNotBumpEpoch(t *testing.T) {
	svc, deps := newWebAuthnServiceWithDeps(t)
	finishRegistration(t, svc, "u-5")

	if got := deps.epoch.bumpsFor("u-5"); got != 0 {
		t.Fatalf("epoch bumped %d times, want 0 — adding a passkey never bumps", got)
	}
	if !deps.events.saw("self_passkey_registered") {
		t.Fatalf("a passkey registration must emit self_passkey_registered; saw %v", deps.events.types)
	}
	if !deps.mail.enqueuedTemplate("auth.mfa_factor_added") {
		t.Fatal("a passkey registration must email the user")
	}
}

// v1.10: EVERY passkey removal, not only the last one. A removed credential
// is one the user no longer trusts — a lost or compromised device — and it
// may have CREATED sessions through the passkey login flow. Neither the
// session document nor amr records which credential minted a session, so
// the rule cannot be narrower.
func TestRemoveCredential_BumpsEpochEvenWhenFactorsRemain(t *testing.T) {
	svc, deps := newWebAuthnServiceWithDeps(t)
	deps.factors.seedTOTP(t, "u-2")
	deps.factors.seedWebAuthn(t, "u-2", 0)
	if err := deps.factors.AppendWebAuthnCredential(context.Background(), "u-2", models.WebAuthnCredential{CredentialID: []byte("cred-a")}); err != nil {
		t.Fatalf("seed credential a: %v", err)
	}
	if err := deps.factors.AppendWebAuthnCredential(context.Background(), "u-2", models.WebAuthnCredential{CredentialID: []byte("cred-b")}); err != nil {
		t.Fatalf("seed credential b: %v", err)
	}

	removed, err := svc.RemoveCredential(context.Background(), "u-2", []byte("cred-a"))
	if err != nil || !removed {
		t.Fatalf("RemoveCredential = (%v, %v), want (true, nil)", removed, err)
	}
	if got := deps.epoch.bumpsFor("u-2"); got != 1 {
		t.Fatalf("epoch bumped %d times, want 1 — removing one passkey bumps even when a TOTP factor and a second passkey remain", got)
	}
	if got := deps.trust.lastReason(); got != models.DeviceTrustRevokedOnMFARemove {
		t.Fatalf("device-trust reason = %q, want %q", got, models.DeviceTrustRevokedOnMFARemove)
	}
	if !deps.events.saw("self_passkey_removed") {
		t.Fatalf("a passkey removal must emit self_passkey_removed; saw %v", deps.events.types)
	}
	if deps.mail.enqueuedTemplate("auth.mfa_factor_added") {
		t.Fatal("a removal is not an addition — no factor-added mail")
	}
}

// A DELETE that matched nothing changed no credential set, so it must not
// move the epoch: otherwise a bearer could strip its own MFA authority (and
// everyone else's session) by looping on a nonexistent credential id.
func TestRemoveCredential_NoMatchDoesNotBumpEpoch(t *testing.T) {
	svc, deps := newWebAuthnServiceWithDeps(t)
	deps.factors.seedWebAuthn(t, "u-9", 0)

	removed, err := svc.RemoveCredential(context.Background(), "u-9", []byte("nothing-here"))
	if err != nil || removed {
		t.Fatalf("RemoveCredential = (%v, %v), want (false, nil)", removed, err)
	}
	if got := deps.epoch.bumpsFor("u-9"); got != 0 {
		t.Fatalf("epoch bumped %d times, want 0", got)
	}
	if deps.trust.revokeCalls() != 0 {
		t.Fatal("nothing was removed — device trust must be untouched")
	}
}

// --- removal -------------------------------------------------------------

func TestRemoveFactor_SelfRemovalBumpsEpochAndEmitsEvent(t *testing.T) {
	svc, deps := newMFAServiceWithDeps(t)
	deps.factors.seedTOTP(t, "u-1")

	if err := svc.RemoveFactor(context.Background(), "u-1", "u-1"); err != nil {
		t.Fatalf("RemoveFactor: %v", err)
	}
	if got := deps.epoch.bumpsFor("u-1"); got != 1 {
		t.Fatalf("epoch bumped %d times, want 1", got)
	}
	if !deps.events.saw("self_mfa_removed") {
		t.Fatalf("self removal must emit self_mfa_removed; saw %v", deps.events.types)
	}
}

// The admin path bumps the same counter — the target's authority has to end
// wherever it was minted — but the SELF event belongs to the self path: the
// admin action is audited as admin_mfa_reset by the handler, and emitting
// both would double-count one reset in the audit timeline.
func TestRemoveFactor_AdminResetBumpsEpochButEmitsNoSelfEvent(t *testing.T) {
	svc, deps := newMFAServiceWithDeps(t)
	deps.factors.seedTOTP(t, "target-1")

	if err := svc.RemoveFactor(context.Background(), "target-1", "admin-1"); err != nil {
		t.Fatalf("RemoveFactor: %v", err)
	}
	if got := deps.epoch.bumpsFor("target-1"); got != 1 {
		t.Fatalf("epoch bumped %d times, want 1", got)
	}
	if deps.events.saw("self_mfa_removed") {
		t.Fatal("an admin reset is not a self action — the handler records admin_mfa_reset instead")
	}
}

// Regression pin, not a TDD driver: the seam is optional by construction
// (nil is its zero value), so this passed before the epoch existed too. It
// is kept because the degradation is a stated contract — a fork's user
// provider may predate iface.MFAEpochBumper — and a later change that makes
// the bump mandatory would break that silently.
func TestEpochBumper_AbsentSeamDegradesInsteadOfFailing(t *testing.T) {
	svc, deps := newMFAServiceWithDeps(t)
	svc.SetEpochBumper(nil)
	deps.factors.seedTOTP(t, "u-6")

	if err := svc.RemoveFactor(context.Background(), "u-6", "u-6"); err != nil {
		t.Fatalf("RemoveFactor with no epoch bumper = %v, want nil", err)
	}
	if deps.factors.hasTOTP("u-6") {
		t.Error("the removal itself must still happen")
	}
}

// A failing bumper is the same contract as an absent one: log it, keep
// going. The removal has already happened and session revocation still
// runs; failing the request would leave the factor deleted and tell the
// caller it was not.
func TestEpochBumper_FailureDoesNotFailTheRemoval(t *testing.T) {
	svc, deps := newMFAServiceWithDeps(t)
	deps.epoch.err = context.DeadlineExceeded
	deps.factors.seedTOTP(t, "u-7")

	if err := svc.RemoveFactor(context.Background(), "u-7", "u-7"); err != nil {
		t.Fatalf("RemoveFactor with a failing epoch bumper = %v, want nil", err)
	}
}

// --- partial failures (fix round 1) --------------------------------------
//
// The consequences of a credential change follow the DESTRUCTION of a
// credential, not the success of the whole call. Getting that ordering
// wrong is M-2 surviving inside the fix for M-2: an account left with no
// factor while every live token still carries amr:["pwd","otp"].

// A replacement whose new secret never lands has still destroyed the old
// one, so the epoch must have moved and device trust must be gone. It also
// has to report replaced=true alongside the error, because that is the
// only signal the handler has to end the caller's other sessions.
func TestConfirmEnrollment_ReplacementThatFailsToPersistStillApplies(t *testing.T) {
	svc, deps := newMFAServiceWithDeps(t)
	deps.factors.seedTOTP(t, "u-10")

	begin, err := svc.BeginEnrollment(context.Background(), &iface.User{UUID: "u-10", Email: "u10@example.com"})
	if err != nil {
		t.Fatalf("BeginEnrollment: %v", err)
	}
	code, err := totp.GenerateCode(begin.SecretBase32, time.Now())
	if err != nil {
		t.Fatalf("GenerateCode: %v", err)
	}
	deps.factors.failInsert = true

	_, replaced, err := svc.ConfirmEnrollment(context.Background(), "u-10", begin.ChallengeID, code)
	if err == nil {
		t.Fatal("a failed persist must surface as an error")
	}
	if !replaced {
		t.Fatal("the old secret was destroyed — the caller must be told so it can end the other sessions")
	}
	if deps.factors.hasTOTP("u-10") {
		t.Fatal("precondition: the old row really was deleted")
	}
	if got := deps.epoch.bumpsFor("u-10"); got != 1 {
		t.Fatalf("epoch bumped %d times, want 1 — the destruction happened whether or not the new row landed", got)
	}
	if got := deps.trust.lastReason(); got != models.DeviceTrustRevokedOnMFAReplace {
		t.Fatalf("device-trust reason = %q, want %q", got, models.DeviceTrustRevokedOnMFAReplace)
	}
	if !deps.events.saw("self_mfa_factor_replaced") {
		t.Fatalf("the destruction must be audited; saw %v", deps.events.types)
	}
	// The mail announces an ADDITION, and no new secret exists.
	if deps.mail.enqueuedTemplate("auth.mfa_factor_added") {
		t.Fatal("nothing was added — no factor-added mail")
	}
}

// The mirror case: the delete itself failed, so nothing was destroyed and
// nothing may be applied. Without this, the test above would pass against
// an implementation that bumps unconditionally.
func TestConfirmEnrollment_FailedDeleteAppliesNothing(t *testing.T) {
	svc, deps := newMFAServiceWithDeps(t)
	deps.factors.seedTOTP(t, "u-11")
	deps.factors.failDeleteFor(models.MFAFactorTOTP)

	begin, err := svc.BeginEnrollment(context.Background(), &iface.User{UUID: "u-11", Email: "u11@example.com"})
	if err != nil {
		t.Fatalf("BeginEnrollment: %v", err)
	}
	code, _ := totp.GenerateCode(begin.SecretBase32, time.Now())

	_, replaced, err := svc.ConfirmEnrollment(context.Background(), "u-11", begin.ChallengeID, code)
	if err == nil {
		t.Fatal("a failed delete must surface as an error")
	}
	if replaced {
		t.Fatal("nothing was replaced — the old row is still there")
	}
	if got := deps.epoch.bumpsFor("u-11"); got != 0 {
		t.Fatalf("epoch bumped %d times, want 0", got)
	}
	if deps.trust.revokeCalls() != 0 {
		t.Fatal("nothing was destroyed — device trust must be untouched")
	}
}

// A half-completed removal: TOTP gone, WebAuthn delete failed. The caller
// gets the error (the account IS half-reset and an operator must know), but
// the destroyed TOTP factor's authority ends anyway. Before the fix this
// returned with the epoch unmoved and every session fully MFA-authorised.
func TestRemoveFactor_PartialDeletionStillAppliesConsequences(t *testing.T) {
	svc, deps := newMFAServiceWithDeps(t)
	deps.factors.seedTOTP(t, "target-9")
	deps.factors.seedWebAuthn(t, "target-9", 1)
	deps.factors.failDeleteFor(models.MFAFactorWebAuthn)

	if err := svc.RemoveFactor(context.Background(), "target-9", "admin-1"); err == nil {
		t.Fatal("a partial deletion must surface as an error")
	}
	if deps.factors.hasTOTP("target-9") {
		t.Fatal("precondition: the TOTP row really was deleted")
	}
	if got := deps.epoch.bumpsFor("target-9"); got != 1 {
		t.Fatalf("epoch bumped %d times, want 1 — a destroyed credential ends its own authority, success or not", got)
	}
	if got := deps.trust.lastReason(); got != models.DeviceTrustRevokedOnAdminReset {
		t.Fatalf("device-trust reason = %q, want the admin-reset reason", got)
	}
}

// The mirror case again: the FIRST delete failed, so nothing was destroyed.
// RemoveFactor fails fast, and the WebAuthn row is never touched.
func TestRemoveFactor_FirstDeleteFailureAppliesNothing(t *testing.T) {
	svc, deps := newMFAServiceWithDeps(t)
	deps.factors.seedTOTP(t, "u-12")
	deps.factors.seedWebAuthn(t, "u-12", 1)
	deps.factors.failDeleteFor(models.MFAFactorTOTP)

	if err := svc.RemoveFactor(context.Background(), "u-12", "u-12"); err == nil {
		t.Fatal("a failed delete must surface as an error")
	}
	if got := deps.epoch.bumpsFor("u-12"); got != 0 {
		t.Fatalf("epoch bumped %d times, want 0 — nothing was destroyed", got)
	}
	if deps.trust.revokeCalls() != 0 {
		t.Fatal("nothing was destroyed — device trust must be untouched")
	}
}
