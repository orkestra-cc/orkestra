package handlers

// The PUBLIC login-completion route had no outer attempt cap. The
// per-challenge counter destroys a challenge after five wrong codes, but a
// correct password mints a fresh one — and a successful password login
// clears the email lockout scope on the way — so a caller who holds the
// password could buy five new TOTP guesses per login, indefinitely. The
// cap pinned here is the OUTER bound D20 gave the authenticated routes:
// per (audience, user), across challenges, on its own key.

import (
	"context"
	"errors"
	"net/http"
	"testing"

	"github.com/orkestra/backend/internal/core/auth/services"
)

func newCappedLoginHandler(t *testing.T, mfa services.MFAService) (*MFAHandler, services.MFAChallengeService, services.AttemptCounter) {
	t.Helper()
	challenges := newChallengeService(t)
	h := NewMFAHandler(mfa, challenges, completionJWT{}, completionUsers{}, &completionIssuer{}, "cookie", "", false)
	counter := services.NewMemoryAttemptCounter()
	h.SetVerifyAttemptCounter(counter, services.PolicyAudienceOperator)
	return h, challenges, counter
}

// newOAuthLoginChallenge mints a challenge whose source is NOT the password,
// so the password-policy recheck stays out of the picture and the cap is
// the only thing under test.
func newOAuthLoginChallenge(t *testing.T, svc services.MFAChallengeService, userUUID string) *services.MFAChallenge {
	t.Helper()
	ch, err := svc.BeginLogin(context.Background(), services.LoginChallengeInput{
		UserUUID: userUUID, SessionID: "sid-cap", SourceAMR: []string{"oauth"}, LoginMethod: "oauth", Audience: "operator",
	})
	if err != nil {
		t.Fatalf("BeginLogin: %v", err)
	}
	return ch
}

func callLoginVerify(t *testing.T, h *MFAHandler, challengeID, code string) (int, error) {
	t.Helper()
	req := &MFALoginVerifyRequest{}
	req.Body.ChallengeID = challengeID
	req.Body.Code = code
	_, err := h.LoginVerify(context.Background(), req)
	if err == nil {
		return http.StatusOK, nil
	}
	return statusOf(t, err), err
}

func TestLoginVerifyCap_FreshChallengeDoesNotRenewTheBudget(t *testing.T) {
	h, challenges, counter := newCappedLoginHandler(t, refusingMFA{})
	const user = "u-login-cap"

	first := newOAuthLoginChallenge(t, challenges, user)
	for i := 0; i < services.MFAMaxAttempts; i++ {
		if status, _ := callLoginVerify(t, h, first.ID, "000000"); status != http.StatusUnauthorized {
			t.Fatalf("guess %d: status = %d, want 401", i+1, status)
		}
	}
	key := services.AttemptKeyMFALogin(services.PolicyAudienceOperator, user)
	if n := countFor(t, counter, key); n != int64(services.MFAMaxAttempts) {
		t.Fatalf("login scope count = %d, want %d — every wrong code must be charged", n, services.MFAMaxAttempts)
	}

	// The attacker logs in again with the correct password: a brand-new
	// challenge. Before the cap this was five more free guesses.
	second := newOAuthLoginChallenge(t, challenges, user)
	status, err := callLoginVerify(t, h, second.ID, "000000")
	if status != http.StatusTooManyRequests {
		t.Fatalf("status on a fresh challenge after %d failures = %d, want 429 (%v)", services.MFAMaxAttempts, status, err)
	}
	var hdr interface{ GetHeaders() http.Header }
	if !errors.As(err, &hdr) || hdr.GetHeaders().Get("Retry-After") == "" {
		t.Fatalf("429 must carry Retry-After, got %v", err)
	}
	if n := countFor(t, counter, key); n != int64(services.MFAMaxAttempts) {
		t.Fatalf("a locked caller must not extend its own lock: count = %d", n)
	}
	if _, perr := challenges.Peek(context.Background(), second.ID); perr != nil {
		t.Fatal("a locked caller must not spend the challenge's own budget either")
	}
}

func TestLoginVerifyCap_SuccessResetsTheBudget(t *testing.T) {
	mfa := &switchingMFA{}
	h, challenges, counter := newCappedLoginHandler(t, mfa)
	const user = "u-login-reset"
	ch := newOAuthLoginChallenge(t, challenges, user)

	callLoginVerify(t, h, ch.ID, "000000")
	callLoginVerify(t, h, ch.ID, "000000")
	key := services.AttemptKeyMFALogin(services.PolicyAudienceOperator, user)
	if n := countFor(t, counter, key); n != 2 {
		t.Fatalf("count after two failures = %d, want 2", n)
	}

	mfa.ok.Store(true)
	if status, err := callLoginVerify(t, h, ch.ID, "123456"); status != http.StatusOK {
		t.Fatalf("correct code: status = %d (%v)", status, err)
	}
	if n := countFor(t, counter, key); n != 0 {
		t.Fatalf("count after success = %d, want 0", n)
	}
}

func TestLoginVerifyCap_IsIndependentOfTheStepUpBudget(t *testing.T) {
	h, challenges, counter := newCappedLoginHandler(t, refusingMFA{})
	const user = "u-login-independent"
	ch := newOAuthLoginChallenge(t, challenges, user)
	callLoginVerify(t, h, ch.ID, "000000")

	if n := countFor(t, counter, services.AttemptKeyMFALogin(services.PolicyAudienceOperator, user)); n != 1 {
		t.Fatalf("login scope = %d, want 1", n)
	}
	if n := countFor(t, counter, services.AttemptKeyMFAVerify(services.PolicyAudienceOperator, user)); n != 0 {
		t.Fatalf("step-up scope = %d, want 0 — a login failure must never spend the step-up budget", n)
	}
	if n := countFor(t, counter, services.AttemptKeyMFAEnroll(services.PolicyAudienceOperator, user)); n != 0 {
		t.Fatalf("enrol scope = %d, want 0", n)
	}
}

func TestLoginVerifyCap_RefusalThatIsNotAGuessIsNotCharged(t *testing.T) {
	h, challenges, counter := newCappedLoginHandler(t, notEnrolledMFA{})
	const user = "u-login-notenrolled"
	ch := newOAuthLoginChallenge(t, challenges, user)
	callLoginVerify(t, h, ch.ID, "000000")

	if n := countFor(t, counter, services.AttemptKeyMFALogin(services.PolicyAudienceOperator, user)); n != 0 {
		t.Fatalf("count = %d, want 0 — 'not enrolled' is a refusal, nothing was guessed", n)
	}
}
