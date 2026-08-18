package services

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/go-webauthn/webauthn/webauthn"

	authModels "github.com/orkestra/backend/internal/core/auth/models"
	"github.com/orkestra/backend/pkg/sdk/iface"
)

// newTestWebAuthn builds a WebAuthnService against the in-memory factor
// repo + an in-memory challenge store. The RP config matches what the
// dev compose stack uses so the library accepts the synthetic origins.
func newTestWebAuthn(t *testing.T) (WebAuthnService, *fakeFactorRepo, MFAChallengeService) {
	t.Helper()
	wa, err := webauthn.New(&webauthn.Config{
		RPDisplayName: "Orkestra Test",
		RPID:          "localhost",
		RPOrigins:     []string{"http://localhost:8080"},
	})
	if err != nil {
		t.Fatalf("init webauthn: %v", err)
	}
	repo := newFakeFactorRepo()
	challenges := NewMFAChallengeService(NewMemoryOAuthStateStore())
	svc := NewWebAuthnService(wa, repo, challenges, slog.Default())
	return svc, repo, challenges
}

func testWebAuthnUser() *iface.User { return testWebAuthnUserDoc() }

func testWebAuthnUserDoc() *iface.User {
	return &iface.User{
		UUID:     "user-uuid-1",
		Email:    "passkey-tester@example.com",
		FullName: "Passkey Tester",
	}
}

// TestWebAuthnNilServiceDisabled ensures that a service constructed with a
// nil *webauthn.WebAuthn (the "passkeys disabled" path) reports a clean
// error instead of panicking when the handler still calls into it.
func TestWebAuthnNilServiceDisabled(t *testing.T) {
	repo := newFakeFactorRepo()
	challenges := NewMFAChallengeService(NewMemoryOAuthStateStore())
	svc := NewWebAuthnService(nil, repo, challenges, slog.Default())

	if _, _, err := svc.BeginRegistration(context.Background(), testWebAuthnUser()); err == nil {
		t.Fatal("expected error when webauthn not configured")
	}
	if _, _, err := svc.BeginAssertion(context.Background(), testWebAuthnUser(), MFAPurposeWebAuthnVerify); err == nil {
		t.Fatal("expected error when webauthn not configured")
	}
}

// TestWebAuthnBeginRegistrationPersistsChallenge exercises the full begin
// flow: the library produces creation options + session data, and the
// service stashes the session JSON in the challenge store under the
// register purpose. The returned options are valid JSON the browser can
// hand to navigator.credentials.create().
func TestWebAuthnBeginRegistrationPersistsChallenge(t *testing.T) {
	svc, _, challenges := newTestWebAuthn(t)
	ctx := context.Background()

	chID, options, err := svc.BeginRegistration(ctx, testWebAuthnUser())
	if err != nil {
		t.Fatalf("begin registration: %v", err)
	}
	if chID == "" {
		t.Fatal("expected non-empty challengeId")
	}
	if options == nil || options.Response.RelyingParty.ID != "localhost" {
		t.Fatalf("unexpected options: %+v", options)
	}
	// Must round-trip through JSON cleanly — the handler hands raw JSON to
	// the browser, so a regression here would silently break enrollment.
	if _, err := json.Marshal(options.Response); err != nil {
		t.Fatalf("options not JSON-serialisable: %v", err)
	}
	// Challenge must be retrievable under the register purpose so finish
	// can reload the SessionData. Wrong purpose fails the test.
	ch, err := challenges.Peek(ctx, chID)
	if err != nil {
		t.Fatalf("peek challenge: %v", err)
	}
	if ch.Purpose != MFAPurposeWebAuthnRegister {
		t.Fatalf("expected purpose %q, got %q", MFAPurposeWebAuthnRegister, ch.Purpose)
	}
	if ch.PendingSecret == "" {
		t.Fatal("expected webauthn session JSON in PendingSecret")
	}
	var session webauthn.SessionData
	if err := json.Unmarshal([]byte(ch.PendingSecret), &session); err != nil {
		t.Fatalf("session JSON not decodable: %v", err)
	}
}

// TestWebAuthnFinishRegistrationRejectsBadChallenge guards the failure
// path — a typo or replayed challengeId must produce ErrMFAInvalidCode,
// not a panic or 500.
func TestWebAuthnFinishRegistrationRejectsBadChallenge(t *testing.T) {
	svc, _, _ := newTestWebAuthn(t)
	_, err := svc.FinishRegistration(context.Background(), testWebAuthnUser(), "nonexistent-challenge", "Yubikey", []byte(`{"id":"x","rawId":"x","type":"public-key","response":{"attestationObject":"","clientDataJSON":""}}`))
	if !errors.Is(err, ErrMFAInvalidCode) {
		t.Fatalf("expected ErrMFAInvalidCode, got %v", err)
	}
}

// TestWebAuthnFinishRegistrationPurposeMismatch ensures a verify
// challenge cannot satisfy a registration finish (and vice-versa).
// Same user, same store — only the purpose tag differs.
func TestWebAuthnFinishRegistrationPurposeMismatch(t *testing.T) {
	svc, _, challenges := newTestWebAuthn(t)
	ctx := context.Background()

	// Mint a challenge under the wrong purpose.
	ch, err := challenges.Begin(ctx, testWebAuthnUser().UUID, MFAPurposeWebAuthnVerify, "{}")
	if err != nil {
		t.Fatalf("seed challenge: %v", err)
	}
	_, err = svc.FinishRegistration(ctx, testWebAuthnUser(), ch.ID, "", []byte(`{}`))
	if !errors.Is(err, ErrMFAChallengeMismatch) {
		t.Fatalf("expected ErrMFAChallengeMismatch, got %v", err)
	}
}

// TestWebAuthnHasCredentialsRoundTrip exercises the credential CRUD path
// directly via the repo to confirm the service's HasCredentials reflects
// what's stored. Avoids the need for a full ceremony round-trip.
func TestWebAuthnHasCredentialsRoundTrip(t *testing.T) {
	svc, repo, _ := newTestWebAuthn(t)
	ctx := context.Background()
	user := testWebAuthnUser()

	if has, _ := svc.HasCredentials(ctx, user.UUID); has {
		t.Fatal("expected no credentials before append")
	}

	cred := authModels.WebAuthnCredential{
		CredentialID: []byte{0x01, 0x02, 0x03},
		PublicKey:    []byte{0x04, 0x05},
		Name:         "Yubikey",
		SignCount:    0,
	}
	if err := repo.AppendWebAuthnCredential(ctx, user.UUID, cred); err != nil {
		t.Fatalf("append: %v", err)
	}
	if has, _ := svc.HasCredentials(ctx, user.UUID); !has {
		t.Fatal("expected credentials after append")
	}
	creds, err := svc.ListCredentials(ctx, user.UUID)
	if err != nil || len(creds) != 1 || creds[0].Name != "Yubikey" {
		t.Fatalf("list mismatch: %v / %v", err, creds)
	}
}

// TestWebAuthnRemoveCredentialClearsRow verifies that removing the last
// credential drops the underlying factor row so MFAStatus reverts to
// not-enrolled — important for the policy gate at next login.
func TestWebAuthnRemoveCredentialClearsRow(t *testing.T) {
	svc, repo, _ := newTestWebAuthn(t)
	ctx := context.Background()
	user := testWebAuthnUser()

	cred := authModels.WebAuthnCredential{
		CredentialID: []byte{0xAA, 0xBB},
		PublicKey:    []byte{0xCC},
		Name:         "iPhone",
	}
	if err := repo.AppendWebAuthnCredential(ctx, user.UUID, cred); err != nil {
		t.Fatalf("append: %v", err)
	}

	removed, err := svc.RemoveCredential(ctx, user.UUID, cred.CredentialID)
	if err != nil || !removed {
		t.Fatalf("remove: removed=%v err=%v", removed, err)
	}
	if has, _ := svc.HasCredentials(ctx, user.UUID); has {
		t.Fatal("expected no credentials after remove")
	}

	// Removing again is a no-op — silent (false, nil), not an error.
	removed, err = svc.RemoveCredential(ctx, user.UUID, cred.CredentialID)
	if err != nil || removed {
		t.Fatalf("idempotent remove: removed=%v err=%v", removed, err)
	}
}

// TestWebAuthnBeginAssertionRequiresCredentials ensures begin-assertion
// fails fast with ErrWebAuthnNoCredentials when the user has no enrolled
// passkeys. Without the guard the library would surface a less helpful
// "no credentials" wrapped error.
func TestWebAuthnBeginAssertionRequiresCredentials(t *testing.T) {
	svc, _, _ := newTestWebAuthn(t)
	_, _, err := svc.BeginAssertion(context.Background(), testWebAuthnUser(), MFAPurposeWebAuthnVerify)
	if !errors.Is(err, ErrWebAuthnNoCredentials) {
		t.Fatalf("expected ErrWebAuthnNoCredentials, got %v", err)
	}
}

// TestWebAuthnBeginAssertionRejectsBadPurpose blocks an attacker from
// requesting an assertion under an unsupported purpose tag. Only the
// register/verify/login purposes are valid; everything else is a coding
// bug we want to catch loudly.
func TestWebAuthnBeginAssertionRejectsBadPurpose(t *testing.T) {
	svc, repo, _ := newTestWebAuthn(t)
	ctx := context.Background()
	// Seed a credential so we don't hit the no-credentials guard first.
	_ = repo.AppendWebAuthnCredential(ctx, testWebAuthnUser().UUID, authModels.WebAuthnCredential{
		CredentialID: []byte{0x01},
		PublicKey:    []byte{0x02},
		Name:         "test",
	})
	_, _, err := svc.BeginAssertion(ctx, testWebAuthnUser(), MFAChallengePurpose("totally-bogus"))
	if !errors.Is(err, ErrMFAChallengeMismatch) {
		t.Fatalf("expected ErrMFAChallengeMismatch, got %v", err)
	}
}

func assertionServiceForConsumeTests(t *testing.T) (*webAuthnService, MFAChallengeService) {
	t.Helper()
	svcIface, repo, challenges := newTestWebAuthn(t)
	svc := svcIface.(*webAuthnService)
	credential := authModels.WebAuthnCredential{
		CredentialID: []byte("credential-one-winner"), PublicKey: []byte("public-key"), Name: "test",
	}
	if err := repo.AppendWebAuthnCredential(context.Background(), testWebAuthnUser().UUID, credential); err != nil {
		t.Fatalf("AppendWebAuthnCredential: %v", err)
	}
	return svc, challenges
}

func TestWebAuthnFinishAssertion_ConcurrentVerifiedCallersHaveOneWinner(t *testing.T) {
	svc, challenges := assertionServiceForConsumeTests(t)
	challenge, err := challenges.Begin(context.Background(), testWebAuthnUser().UUID, MFAPurposeWebAuthnLogin, `{}`)
	if err != nil {
		t.Fatalf("Begin: %v", err)
	}
	svc.assertionValidator = func(context.Context, *iface.User, webauthn.SessionData, []byte) (*webauthn.Credential, error) {
		return &webauthn.Credential{ID: []byte("credential-one-winner")}, nil
	}

	const callers = 24
	start := make(chan struct{})
	var winners atomic.Int32
	var wg sync.WaitGroup
	for i := 0; i < callers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			if err := svc.FinishAssertion(context.Background(), testWebAuthnUser(), challenge.ID, MFAPurposeWebAuthnLogin, []byte("verified-by-test-seam")); err == nil {
				winners.Add(1)
			}
		}()
	}
	close(start)
	wg.Wait()
	if got := winners.Load(); got != 1 {
		t.Fatalf("successful assertions = %d, want exactly 1", got)
	}
}

func TestWebAuthnFinishAssertion_InvalidAssertionDoesNotBurnChallenge(t *testing.T) {
	svc, challenges := assertionServiceForConsumeTests(t)
	challenge, err := challenges.Begin(context.Background(), testWebAuthnUser().UUID, MFAPurposeWebAuthnVerify, `{}`)
	if err != nil {
		t.Fatalf("Begin: %v", err)
	}
	var validations atomic.Int32
	svc.assertionValidator = func(context.Context, *iface.User, webauthn.SessionData, []byte) (*webauthn.Credential, error) {
		if validations.Add(1) == 1 {
			return nil, ErrWebAuthnAssertion
		}
		return &webauthn.Credential{ID: []byte("credential-one-winner")}, nil
	}

	if err := svc.FinishAssertion(context.Background(), testWebAuthnUser(), challenge.ID, MFAPurposeWebAuthnVerify, []byte("invalid")); !errors.Is(err, ErrWebAuthnAssertion) {
		t.Fatalf("invalid assertion = %v, want ErrWebAuthnAssertion", err)
	}
	if err := svc.FinishAssertion(context.Background(), testWebAuthnUser(), challenge.ID, MFAPurposeWebAuthnVerify, []byte("valid")); err != nil {
		t.Fatalf("retry after invalid assertion: %v", err)
	}
	if err := svc.FinishAssertion(context.Background(), testWebAuthnUser(), challenge.ID, MFAPurposeWebAuthnVerify, []byte("replay")); !errors.Is(err, ErrMFAInvalidCode) {
		t.Fatalf("replayed assertion = %v, want ErrMFAInvalidCode", err)
	}
}
