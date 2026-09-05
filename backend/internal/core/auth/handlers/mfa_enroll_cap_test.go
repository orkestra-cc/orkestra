package handlers

// Defect (b), found in live testing on staging 2026-09-04: `/mfa/enroll/
// confirm` had NO attempt cap. Spec D20 capped `MFAHandler.Verify` and
// `WebAuthnHandler.VerifyFinish` and missed this route, whose only bound was
// the per-challenge `MFAMaxAttempts` — and that one DELETES the challenge at
// five, after which every further attempt fails the challenge lookup instead
// of stopping. An operator mistyping enrolment codes got 26 answers in 13
// seconds; each was a codeless 401, each rotated the console's refresh
// cookie, and the family's reuse detection ended the session.
//
// The cap here is the OUTER bound, per (audience, user), across challenges —
// and it keys into `mfa-enroll`, NOT the `mfa-verify` scope the step-up
// routes use. Sharing would be a circular lockout: a user fumbling their
// enrolment codes would spend the step-up budget, and step-up is exactly
// what a user who already holds a factor must pass in order to re-enrol.
//
// The fixtures drive the REAL `mfaService` over a real (or deliberately
// broken) challenge store rather than a fake that returns sentinels
// directly, for the reason
// TestWebAuthnVerifyFinish_ChallengeStoreOutageCostsNothing gives: a fake
// asserts the handler's arm without proving the sentinel is what the service
// actually produces, and the whole charge decision here turns on telling two
// productions of ErrMFAInvalidCode apart.

import (
	"context"
	"net/http"
	"testing"

	authModels "github.com/orkestra/backend/internal/core/auth/models"
	"github.com/orkestra/backend/internal/core/auth/repository"
	"github.com/orkestra/backend/internal/core/auth/services"
	"github.com/orkestra/backend/internal/shared/errcode"
	"github.com/orkestra/backend/pkg/sdk/iface"
)

// --- fixtures ------------------------------------------------------------

// emptyFactors is an MFAFactorRepository that holds nothing. Only the
// REPLACEMENT branch of ConfirmEnrollment reads it, and no test here ever
// gets past a rejected code, so returning "no row" is the whole contract.
type emptyFactors struct{ repository.MFAFactorRepository }

func (emptyFactors) FindByUserAndType(context.Context, string, authModels.MFAFactorType) (*authModels.MFAFactorDoc, error) {
	return nil, nil
}

// newEnrolHandler builds an MFAHandler over a real mfaService whose
// challenge store is `store`, wired to `counter` on `aud`. Passing
// downChallengeStore{} models an unreachable Redis; the memory store models
// a healthy one.
func newEnrolHandler(t *testing.T, store services.OAuthStateStore, counter services.AttemptCounter, aud services.PolicyAudience, user *iface.User) *MFAHandler {
	t.Helper()
	svc := services.NewMFAService(
		emptyFactors{},
		services.NewMFAChallengeService(store),
		services.NewPasswordService(nil, false),
		"Orkestra",
		silentTestLogger(),
	)
	h := NewMFAHandler(svc, nil, newStepUpJWT(t), &stepUpUsers{user: user}, nil, "", "", false)
	h.SetVerifyAttemptCounter(counter, aud)
	return h
}

// beginEnrolment issues a real enrolment challenge through the handler, so
// the challenge the confirm tests submit against is one the service itself
// created (with a real pending secret behind it).
func beginEnrolment(t *testing.T, h *MFAHandler, userUUID string) string {
	t.Helper()
	resp, err := h.EnrollBegin(stepUpContext(userUUID), &struct{}{})
	if err != nil {
		t.Fatalf("EnrollBegin: %v", err)
	}
	return resp.Body.ChallengeID
}

// callEnrolConfirm submits `code` against `challengeID` and reports the
// status. "000000" is never a live TOTP code for a freshly generated secret
// (the odds are 1e-6 and the secret is random per test), which is what makes
// it a wrong CODE rather than a lost challenge.
func callEnrolConfirm(t *testing.T, h *MFAHandler, userUUID, challengeID, code string) int {
	t.Helper()
	_, err := h.EnrollConfirm(stepUpContext(userUUID), enrolConfirmRequest(challengeID, code))
	if err == nil {
		return http.StatusOK
	}
	return statusOf(t, err)
}

func enrolConfirmRequest(challengeID, code string) *MFAEnrollConfirmRequest {
	req := &MFAEnrollConfirmRequest{}
	req.Body.ChallengeID = challengeID
	req.Body.Code = code
	return req
}

func enrolUser() *iface.User {
	return &iface.User{UUID: "enrolling-user", Email: "enrolling@example.com", Role: "operator", IsActive: true}
}

// --- the cap -------------------------------------------------------------

// The RED test for defect (b). Before the cap this loop answered 401 for as
// long as the caller kept typing.
func TestMFAEnrollConfirm_LocksAfterFiveFailures(t *testing.T) {
	counter := services.NewMemoryAttemptCounter()
	user := enrolUser()
	h := newEnrolHandler(t, services.NewMemoryOAuthStateStore(), counter, services.PolicyAudienceOperator, user)
	challengeID := beginEnrolment(t, h, user.UUID)

	for i := 0; i < services.MFAMaxAttempts; i++ {
		if code := callEnrolConfirm(t, h, user.UUID, challengeID, "000000"); code != http.StatusUnauthorized {
			t.Fatalf("attempt %d = %d, want 401", i+1, code)
		}
	}
	// The per-challenge counter has now destroyed the challenge, which is
	// precisely why it is not a cap: without the outer bound the caller
	// simply keeps going and every further attempt answers 401 again.
	if code := callEnrolConfirm(t, h, user.UUID, challengeID, "000000"); code != http.StatusTooManyRequests {
		t.Fatalf("attempt %d = %d, want 429 — /mfa/enroll/confirm must stop, not keep answering",
			services.MFAMaxAttempts+1, code)
	}
	key := services.AttemptKeyMFAEnroll(services.PolicyAudienceOperator, user.UUID)
	if got := countFor(t, counter, key); got != int64(services.MFAMaxAttempts) {
		t.Fatalf("count = %d, want %d — the locked attempt must PEEK, not charge",
			got, services.MFAMaxAttempts)
	}
}

// A cap with no retry hint is a client that hot-loops. lockoutError is the
// single renderer this shares with the password lockout and the verify cap.
func TestMFAEnrollConfirm_LockAnswersWithRetryAfter(t *testing.T) {
	counter := services.NewMemoryAttemptCounter()
	user := enrolUser()
	h := newEnrolHandler(t, services.NewMemoryOAuthStateStore(), counter, services.PolicyAudienceOperator, user)
	challengeID := beginEnrolment(t, h, user.UUID)

	for i := 0; i < services.MFAMaxAttempts; i++ {
		callEnrolConfirm(t, h, user.UUID, challengeID, "000000")
	}
	_, err := h.EnrollConfirm(stepUpContext(user.UUID), enrolConfirmRequest(challengeID, "000000"))
	assertStatusAndCode(t, err, http.StatusTooManyRequests, errcode.AuthTooManyAttempts)
	if got := retryAfterOf(t, err); got == "" {
		t.Fatal("a 429 with no Retry-After is an invitation to hot-loop")
	}
}

// 🔴 The whole point of the separate key. Exhausting either budget must
// leave the other untouched — a user who fumbles enrolment must still be
// able to step up, because step-up is what a user with an existing factor
// needs in order to re-enrol at all.
func TestMFAEnrollAndVerifyBudgetsAreIndependent(t *testing.T) {
	t.Run("a spent enrolment budget leaves step-up open", func(t *testing.T) {
		counter := services.NewMemoryAttemptCounter()
		user := enrolUser()
		enrol := newEnrolHandler(t, services.NewMemoryOAuthStateStore(), counter, services.PolicyAudienceOperator, user)
		verify := NewMFAHandler(refusingMFA{}, nil, newStepUpJWT(t), &stepUpUsers{user: user}, nil, "", "", false)
		verify.SetVerifyAttemptCounter(counter, services.PolicyAudienceOperator)

		challengeID := beginEnrolment(t, enrol, user.UUID)
		for i := 0; i < services.MFAMaxAttempts; i++ {
			callEnrolConfirm(t, enrol, user.UUID, challengeID, "000000")
		}
		if code := callEnrolConfirm(t, enrol, user.UUID, challengeID, "000000"); code != http.StatusTooManyRequests {
			t.Fatalf("enrolment = %d, want 429 — the fixture must really have spent that budget", code)
		}
		if code := callVerify(t, verify, user.UUID, "000000"); code != http.StatusUnauthorized {
			t.Fatalf("step-up = %d, want 401 — a fumbled enrolment must not lock the door it needs", code)
		}
	})

	t.Run("a spent step-up budget leaves enrolment open", func(t *testing.T) {
		counter := services.NewMemoryAttemptCounter()
		user := enrolUser()
		enrol := newEnrolHandler(t, services.NewMemoryOAuthStateStore(), counter, services.PolicyAudienceOperator, user)
		verify := NewMFAHandler(refusingMFA{}, nil, newStepUpJWT(t), &stepUpUsers{user: user}, nil, "", "", false)
		verify.SetVerifyAttemptCounter(counter, services.PolicyAudienceOperator)

		for i := 0; i < services.MFAMaxAttempts; i++ {
			callVerify(t, verify, user.UUID, "000000")
		}
		if code := callVerify(t, verify, user.UUID, "000000"); code != http.StatusTooManyRequests {
			t.Fatalf("step-up = %d, want 429 — the fixture must really have spent that budget", code)
		}
		challengeID := beginEnrolment(t, enrol, user.UUID)
		if code := callEnrolConfirm(t, enrol, user.UUID, challengeID, "000000"); code != http.StatusUnauthorized {
			t.Fatalf("enrolment = %d, want 401 — a spent step-up budget must not block enrolling", code)
		}
	})
}

// Only a rejected CODE is charged. `ConfirmEnrollment` answers with
// ErrMFAInvalidCode both for a wrong code and for a challenge it could not
// read — and `Peek` collapses EVERY store failure, a Redis outage included,
// into that second case. Charging it would let a degraded store spend the
// user's enrolment budget, which is the trade the AttemptCounter contract
// exists to refuse (spec §5 edge case 2) and the defect already fixed on the
// passkey route (ebed86f1a).
//
// The fixture fakes only the STORE, so everything under test is production
// code: store.Get errors → Peek → ErrMFAChallengeNotFound → ConfirmEnrollment
// → the tagged ErrMFAInvalidCode → the handler's charge decision.
func TestMFAEnrollConfirm_ChallengeStoreOutageCostsNothing(t *testing.T) {
	counter := services.NewMemoryAttemptCounter()
	user := enrolUser()
	h := newEnrolHandler(t, downChallengeStore{}, counter, services.PolicyAudienceOperator, user)

	// Well past the threshold: a charged outage would lock at five.
	for i := 0; i < services.MFAMaxAttempts+3; i++ {
		if code := callEnrolConfirm(t, h, user.UUID, "challenge-lost", "000000"); code != http.StatusUnauthorized {
			t.Fatalf("attempt %d = %d, want 401 — a store outage must never answer 429", i+1, code)
		}
	}
	key := services.AttemptKeyMFAEnroll(services.PolicyAudienceOperator, user.UUID)
	if got := countFor(t, counter, key); got != 0 {
		t.Fatalf("count = %d, want 0 — an unreadable challenge is not a guess", got)
	}
}

// An unavailable counter fails OPEN, the contract every AttemptCounter
// caller in this module shares. The per-challenge counter still bounds one
// ceremony.
//
// ⚠️ BOUND, not a TDD driver. It fails only if a later change makes this
// counter fail closed — which would turn a Redis blip into "nobody can
// enrol", the worst possible outage for an MFA-obligated fleet.
func TestMFAEnrollConfirm_CounterOutageFailsOpen(t *testing.T) {
	counter := services.NewMemoryAttemptCounter()
	user := enrolUser()
	h := newEnrolHandler(t, services.NewMemoryOAuthStateStore(), counter, services.PolicyAudienceOperator, user)
	challengeID := beginEnrolment(t, h, user.UUID)
	counter.(*services.MemoryAttemptCounter).FailWith(errStoreDown)

	for i := 0; i < services.MFAMaxAttempts+2; i++ {
		if code := callEnrolConfirm(t, h, user.UUID, challengeID, "000000"); code != http.StatusUnauthorized {
			t.Fatalf("attempt %d = %d, want 401 — a counter outage must not answer 429", i+1, code)
		}
	}
}

// A handler with no counter wired behaves exactly as it did before. Every
// production instance is wired by module.go, but a fork's handler (and every
// pre-existing fixture in this package) must not panic.
//
// ⚠️ REGRESSION PIN, not a TDD driver.
func TestMFAEnrollConfirm_NoCounterWiredIsUncapped(t *testing.T) {
	user := enrolUser()
	svc := services.NewMFAService(
		emptyFactors{},
		services.NewMFAChallengeService(services.NewMemoryOAuthStateStore()),
		services.NewPasswordService(nil, false),
		"Orkestra",
		silentTestLogger(),
	)
	h := NewMFAHandler(svc, nil, newStepUpJWT(t), &stepUpUsers{user: user}, nil, "", "", false)
	challengeID := beginEnrolment(t, h, user.UUID)

	for i := 0; i < services.MFAMaxAttempts+1; i++ {
		if code := callEnrolConfirm(t, h, user.UUID, challengeID, "000000"); code != http.StatusUnauthorized {
			t.Fatalf("attempt %d = %d, want 401", i+1, code)
		}
	}
}

// The cap is per (audience, user), like its verify sibling: two tiers over
// one counter with the same user UUID must not share a lockout.
func TestMFAEnrollConfirm_TheTwoTiersDoNotShareALockout(t *testing.T) {
	counter := services.NewMemoryAttemptCounter()
	user := enrolUser()
	operator := newEnrolHandler(t, services.NewMemoryOAuthStateStore(), counter, services.PolicyAudienceOperator, user)
	client := newEnrolHandler(t, services.NewMemoryOAuthStateStore(), counter, services.PolicyAudienceClient, user)

	opChallenge := beginEnrolment(t, operator, user.UUID)
	for i := 0; i < services.MFAMaxAttempts; i++ {
		callEnrolConfirm(t, operator, user.UUID, opChallenge, "000000")
	}
	if code := callEnrolConfirm(t, operator, user.UUID, opChallenge, "000000"); code != http.StatusTooManyRequests {
		t.Fatalf("operator = %d, want 429", code)
	}
	clientChallenge := beginEnrolment(t, client, user.UUID)
	if code := callEnrolConfirm(t, client, user.UUID, clientChallenge, "000000"); code != http.StatusUnauthorized {
		t.Fatalf("client = %d, want 401 — the client tier must have its own budget", code)
	}
}
