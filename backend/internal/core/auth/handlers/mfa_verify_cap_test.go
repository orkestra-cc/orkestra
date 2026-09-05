package handlers

// M-3 (spec §4.3 D20): the AUTHENTICATED verify routes had no attempt cap
// of their own. `MFAChallengeService` bounds one challenge — five guesses
// and it is destroyed — but `/mfa/verify` and `/mfa/webauthn/verify/finish`
// take a bearer token and no challenge budget at all on the TOTP side, so a
// caller holding a stolen session could guess indefinitely, and on the
// passkey side could simply start a new challenge after every fifth try.
// The cap here is the OUTER bound: per (audience, user), across challenges.
//
// The fixtures (`stepUpUsers`, `stepUpMFA`, `newStepUpJWT`, `stepUpContext`,
// `statusOf`) are the ones step_up_session_identity_test.go already built
// for these two handlers — the failing variants below are the only new
// fakes, because a success-only fake cannot drive a failure counter.

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"sync/atomic"
	"testing"
	"time"

	gowebauthn "github.com/go-webauthn/webauthn/webauthn"

	authModels "github.com/orkestra/backend/internal/core/auth/models"
	"github.com/orkestra/backend/internal/core/auth/repository"
	"github.com/orkestra/backend/internal/core/auth/services"
	"github.com/orkestra/backend/pkg/sdk/iface"
)

// --- fakes ---------------------------------------------------------------

// refusingMFA answers every TOTP and backup-code attempt with the sentinel
// a wrong code produces (services/mfa_service.go returns ErrMFAInvalidCode
// for every "that is not the code" branch).
type refusingMFA struct{ services.MFAService }

func (refusingMFA) Verify(context.Context, string, string) error { return services.ErrMFAInvalidCode }
func (refusingMFA) VerifyBackupCode(context.Context, string, string) error {
	return services.ErrMFAInvalidCode
}

// notEnrolledMFA answers with a refusal that is NOT a rejected credential.
// Nothing was guessed, so nothing may be charged.
type notEnrolledMFA struct{ services.MFAService }

func (notEnrolledMFA) Verify(context.Context, string, string) error {
	return services.ErrMFANotEnrolled
}

// switchingMFA refuses until `ok` is set, so one test can drive failures
// and then a success against the same handler.
type switchingMFA struct {
	services.MFAService
	ok atomic.Bool
}

func (m *switchingMFA) Verify(context.Context, string, string) error {
	if m.ok.Load() {
		return nil
	}
	return services.ErrMFAInvalidCode
}

// refusingWebAuthn fails the assertion the way a mismatched signature does.
type refusingWebAuthn struct{ services.WebAuthnService }

func (refusingWebAuthn) FinishAssertion(context.Context, *iface.User, string, services.MFAChallengePurpose, []byte) error {
	return services.ErrWebAuthnAssertion
}

// backupCodeFactors serves one TOTP row carrying `n` backup-code hashes, so
// VerifyBackupCode really walks the list. Only FindByUserAndType is reached
// on an all-wrong attempt; every other method stays the embedded nil.
type backupCodeFactors struct {
	repository.MFAFactorRepository
	doc *authModels.MFAFactorDoc
}

func (f *backupCodeFactors) FindByUserAndType(context.Context, string, authModels.MFAFactorType) (*authModels.MFAFactorDoc, error) {
	return f.doc, nil
}

// countingPasswords counts hash comparisons so the backup-code test can
// assert the REAL number of comparisons alongside the single charge.
type countingPasswords struct {
	services.PasswordService
	verifies atomic.Int32
}

func (c *countingPasswords) Verify(plaintext, hash string) (bool, error) {
	c.verifies.Add(1)
	return c.PasswordService.Verify(plaintext, hash)
}

// --- helpers -------------------------------------------------------------

// countFor peeks a key without incrementing it. Limit{} has Threshold 0,
// which attempt_counter.go documents as UNSET — it never locks, so the
// peek reports the count and nothing else.
func countFor(t *testing.T, c services.AttemptCounter, key string) int64 {
	t.Helper()
	v, err := c.Locked(context.Background(), key, services.Limit{})
	if err != nil {
		t.Fatalf("peek %s: %v", key, err)
	}
	return v.Count
}

func newCappedMFAHandler(t *testing.T, mfa services.MFAService, aud services.PolicyAudience) (*MFAHandler, services.AttemptCounter, *iface.User) {
	t.Helper()
	user := &iface.User{UUID: "capped-user", Email: "capped@example.com", Role: "operator", IsActive: true}
	h := NewMFAHandler(mfa, nil, newStepUpJWT(t), &stepUpUsers{user: user}, nil, "", "", false)
	counter := services.NewMemoryAttemptCounter()
	h.SetVerifyAttemptCounter(counter, aud)
	return h, counter, user
}

func callVerify(t *testing.T, h *MFAHandler, userUUID, code string) int {
	t.Helper()
	req := &MFAVerifyRequest{}
	req.Body.Code = code
	_, err := h.Verify(stepUpContext(userUUID), req)
	if err == nil {
		return http.StatusOK
	}
	return statusOf(t, err)
}

func callVerifyBackup(t *testing.T, h *MFAHandler, userUUID, code string) int {
	t.Helper()
	req := &MFAVerifyRequest{}
	req.Body.Code = code
	req.Body.UseBackup = true
	_, err := h.Verify(stepUpContext(userUUID), req)
	if err == nil {
		return http.StatusOK
	}
	return statusOf(t, err)
}

func callVerifyFinish(t *testing.T, h *WebAuthnHandler, userUUID string) int {
	t.Helper()
	req := &webAuthnVerifyFinishRequest{}
	req.Body.ChallengeID = "challenge-capped"
	req.Body.AssertionResponse = json.RawMessage(`{"id":"credential"}`)
	_, err := h.VerifyFinish(stepUpContext(userUUID), req)
	if err == nil {
		return http.StatusOK
	}
	return statusOf(t, err)
}

// --- TOTP / backup code --------------------------------------------------

func TestMFAVerify_LocksAfterFiveFailures(t *testing.T) {
	h, counter, user := newCappedMFAHandler(t, refusingMFA{}, services.PolicyAudienceOperator)

	for i := 0; i < services.MFAMaxAttempts; i++ {
		if code := callVerify(t, h, user.UUID, "000000"); code != http.StatusUnauthorized {
			t.Fatalf("attempt %d = %d, want 401", i+1, code)
		}
	}
	if code := callVerify(t, h, user.UUID, "000000"); code != http.StatusTooManyRequests {
		t.Fatalf("attempt %d = %d, want 429 — the cap must survive a caller who keeps trying",
			services.MFAMaxAttempts+1, code)
	}
	key := services.AttemptKeyMFAVerify(services.PolicyAudienceOperator, user.UUID)
	if got := countFor(t, counter, key); got != int64(services.MFAMaxAttempts) {
		t.Fatalf("count = %d, want %d — the locked attempt must PEEK, not charge",
			got, services.MFAMaxAttempts)
	}
}

// The 429 has to carry Retry-After: a cap with no retry hint is a client
// that hot-loops. lockoutError is the single renderer both this and the
// password lockout use.
func TestMFAVerify_LockAnswersWithRetryAfter(t *testing.T) {
	h, _, user := newCappedMFAHandler(t, refusingMFA{}, services.PolicyAudienceOperator)
	for i := 0; i < services.MFAMaxAttempts; i++ {
		callVerify(t, h, user.UUID, "000000")
	}
	req := &MFAVerifyRequest{}
	req.Body.Code = "000000"
	_, err := h.Verify(stepUpContext(user.UUID), req)
	assertStatusAndCode(t, err, http.StatusTooManyRequests, "auth.too_many_attempts")
	if got := retryAfterOf(t, err); got == "" {
		t.Fatal("a 429 with no Retry-After is an invitation to hot-loop")
	}
}

func TestMFAVerify_SuccessResetsTheCounter(t *testing.T) {
	mfa := &switchingMFA{}
	h, counter, user := newCappedMFAHandler(t, mfa, services.PolicyAudienceOperator)
	key := services.AttemptKeyMFAVerify(services.PolicyAudienceOperator, user.UUID)

	for i := 0; i < 3; i++ {
		callVerify(t, h, user.UUID, "000000")
	}
	if got := countFor(t, counter, key); got != 3 {
		t.Fatalf("count = %d before the success, want 3", got)
	}
	mfa.ok.Store(true)
	if code := callVerify(t, h, user.UUID, "123456"); code != http.StatusOK {
		t.Fatalf("success = %d, want 200", code)
	}
	if got := countFor(t, counter, key); got != 0 {
		t.Fatalf("count = %d after a success, want 0", got)
	}
}

// D20 charges an invalid CODE. "You have no factor" is a refusal, not a
// guess: charging it would let a degraded or unenrolled account burn its
// own five-attempt budget without an attacker ever trying a code.
//
// ⚠️ BOUND, not a TDD driver — it was green before the cap existed (an
// absent counter also counts zero). It fails only if the charge is later
// widened to every verify error. Ruling R17.
func TestMFAVerify_ANonCredentialRefusalIsNotCharged(t *testing.T) {
	h, counter, user := newCappedMFAHandler(t, notEnrolledMFA{}, services.PolicyAudienceOperator)

	if code := callVerify(t, h, user.UUID, "000000"); code != http.StatusBadRequest {
		t.Fatalf("not-enrolled = %d, want 400", code)
	}
	key := services.AttemptKeyMFAVerify(services.PolicyAudienceOperator, user.UUID)
	if got := countFor(t, counter, key); got != 0 {
		t.Fatalf("count = %d, want 0 — no code was guessed", got)
	}
}

// The cap is per (audience, user). Two tiers wired to the same counter with
// the same user UUID must not share a lockout: AttemptKeyMFAVerify puts the
// audience in the key, and the handlers are wired per tier. Get this wrong
// and locking an operator locks the client account that happens to carry
// the same UUID.
func TestMFAVerify_TheTwoTiersDoNotShareALockout(t *testing.T) {
	counter := services.NewMemoryAttemptCounter()
	user := &iface.User{UUID: "same-uuid", Email: "both@example.com", Role: "operator", IsActive: true}

	operator := NewMFAHandler(refusingMFA{}, nil, newStepUpJWT(t), &stepUpUsers{user: user}, nil, "", "", false)
	operator.SetVerifyAttemptCounter(counter, services.PolicyAudienceOperator)
	client := NewMFAHandler(refusingMFA{}, nil, newStepUpJWT(t), &stepUpUsers{user: user}, nil, "", "", false)
	client.SetVerifyAttemptCounter(counter, services.PolicyAudienceClient)

	for i := 0; i < services.MFAMaxAttempts; i++ {
		callVerify(t, operator, user.UUID, "000000")
	}
	if code := callVerify(t, operator, user.UUID, "000000"); code != http.StatusTooManyRequests {
		t.Fatalf("operator = %d, want 429", code)
	}
	if code := callVerify(t, client, user.UUID, "000000"); code != http.StatusUnauthorized {
		t.Fatalf("client = %d, want 401 — the client tier must have its own budget", code)
	}
}

// An unavailable counter fails OPEN, the contract every AttemptCounter
// caller in this module shares: a Redis outage must not take step-up down
// for everyone. The inner per-challenge counter still bounds the passkey
// ceremony, and the durable half of the login lockout is unaffected.
//
// ⚠️ BOUND, not a TDD driver — green before the cap existed. It fails only
// if a later change makes the counter fail closed. Ruling R17.
func TestMFAVerify_CounterOutageFailsOpen(t *testing.T) {
	h, counter, user := newCappedMFAHandler(t, refusingMFA{}, services.PolicyAudienceOperator)
	counter.(*services.MemoryAttemptCounter).FailWith(errStoreDown)

	for i := 0; i < services.MFAMaxAttempts+2; i++ {
		if code := callVerify(t, h, user.UUID, "000000"); code != http.StatusUnauthorized {
			t.Fatalf("attempt %d = %d, want 401 — a counter outage must not answer 429", i+1, code)
		}
	}
}

// A handler with no counter wired behaves exactly as it did before D20.
// Production wires all four instances, but a fork's handler (and every
// pre-existing fixture in this package) must not panic.
//
// ⚠️ REGRESSION PIN, not a TDD driver — it pins the pre-D20 behaviour and
// was green all along. Ruling R17.
func TestMFAVerify_NoCounterWiredIsUncapped(t *testing.T) {
	user := &iface.User{UUID: "uncapped", Email: "u@example.com", Role: "operator", IsActive: true}
	h := NewMFAHandler(refusingMFA{}, nil, newStepUpJWT(t), &stepUpUsers{user: user}, nil, "", "", false)
	for i := 0; i < services.MFAMaxAttempts+1; i++ {
		if code := callVerify(t, h, user.UUID, "000000"); code != http.StatusUnauthorized {
			t.Fatalf("attempt %d = %d, want 401", i+1, code)
		}
	}
}

// The load-bearing half of D20's backup-code rule. VerifyBackupCode walks
// the whole hashed list and returns ONE ErrMFAInvalidCode, so the charge
// belongs at the request, not at the comparison. This drives a REAL
// mfaService over five stored hashes: a per-comparison charge would put the
// counter at 5 and lock the user out on their FIRST backup-code attempt.
func TestMFAVerify_BackupCodeAttemptCostsOne(t *testing.T) {
	passwords := services.NewPasswordService(nil, false)
	counted := &countingPasswords{PasswordService: passwords}
	const stored = 5
	hashes := make([]string, 0, stored)
	for i := 0; i < stored; i++ {
		h, err := passwords.Hash("AAAA-BBB" + string(rune('0'+i)))
		if err != nil {
			t.Fatalf("hash: %v", err)
		}
		hashes = append(hashes, h)
	}
	factors := &backupCodeFactors{doc: &authModels.MFAFactorDoc{
		UUID:              "factor-1",
		UserUUID:          "capped-user",
		Type:              authModels.MFAFactorTOTP,
		BackupCodesHashed: hashes,
	}}
	mfa := services.NewMFAService(factors, nil, counted, "Orkestra", nil)
	h, counter, user := newCappedMFAHandler(t, mfa, services.PolicyAudienceOperator)

	if code := callVerifyBackup(t, h, user.UUID, "ZZZZ-ZZZZ"); code != http.StatusUnauthorized {
		t.Fatalf("backup attempt = %d, want 401", code)
	}
	if got := counted.verifies.Load(); got != stored {
		t.Fatalf("hash comparisons = %d, want %d — the fixture must really walk the list", got, stored)
	}
	key := services.AttemptKeyMFAVerify(services.PolicyAudienceOperator, user.UUID)
	if got := countFor(t, counter, key); got != 1 {
		t.Fatalf("count = %d after ONE backup-code attempt (%d comparisons), want 1", got, stored)
	}
}

// --- passkey -------------------------------------------------------------

func newCappedWebAuthnHandler(t *testing.T, wa services.WebAuthnService, aud services.PolicyAudience) (*WebAuthnHandler, services.AttemptCounter, *iface.User) {
	t.Helper()
	user := &iface.User{UUID: "capped-user", Email: "capped@example.com", Role: "operator", IsActive: true}
	h := NewWebAuthnHandler(wa, nil, newStepUpJWT(t), &stepUpUsers{user: user}, nil, "", "", false)
	counter := services.NewMemoryAttemptCounter()
	h.SetVerifyAttemptCounter(counter, aud)
	return h, counter, user
}

func TestWebAuthnVerifyFinish_LocksAfterFiveFailures(t *testing.T) {
	h, counter, user := newCappedWebAuthnHandler(t, refusingWebAuthn{}, services.PolicyAudienceOperator)

	for i := 0; i < services.MFAMaxAttempts; i++ {
		if code := callVerifyFinish(t, h, user.UUID); code != http.StatusUnauthorized {
			t.Fatalf("attempt %d = %d, want 401", i+1, code)
		}
	}
	if code := callVerifyFinish(t, h, user.UUID); code != http.StatusTooManyRequests {
		t.Fatalf("attempt %d = %d, want 429 — starting a fresh challenge must not buy a fresh budget",
			services.MFAMaxAttempts+1, code)
	}
	key := services.AttemptKeyMFAVerify(services.PolicyAudienceOperator, user.UUID)
	if got := countFor(t, counter, key); got != int64(services.MFAMaxAttempts) {
		t.Fatalf("count = %d, want %d", got, services.MFAMaxAttempts)
	}
}

func TestWebAuthnVerifyFinish_SuccessResetsTheCounter(t *testing.T) {
	// stepUpWebAuthn accepts every assertion; refusingWebAuthn rejects
	// every one. Two handlers over ONE counter is the honest way to drive
	// "failures, then a success" without a stateful fake.
	counter := services.NewMemoryAttemptCounter()
	user := &iface.User{UUID: "capped-user", Email: "capped@example.com", Role: "operator", IsActive: true}
	key := services.AttemptKeyMFAVerify(services.PolicyAudienceOperator, user.UUID)

	failing := NewWebAuthnHandler(refusingWebAuthn{}, nil, newStepUpJWT(t), &stepUpUsers{user: user}, nil, "", "", false)
	failing.SetVerifyAttemptCounter(counter, services.PolicyAudienceOperator)
	for i := 0; i < 3; i++ {
		callVerifyFinish(t, failing, user.UUID)
	}
	if got := countFor(t, counter, key); got != 3 {
		t.Fatalf("count = %d before the success, want 3", got)
	}

	succeeding := NewWebAuthnHandler(stepUpWebAuthn{}, nil, newStepUpJWT(t), &stepUpUsers{user: user}, nil, "", "", false)
	succeeding.SetVerifyAttemptCounter(counter, services.PolicyAudienceOperator)
	if code := callVerifyFinish(t, succeeding, user.UUID); code != http.StatusOK {
		t.Fatalf("success = %d, want 200", code)
	}
	if got := countFor(t, counter, key); got != 0 {
		t.Fatalf("count = %d after a success, want 0", got)
	}
}

// Fix round 1, Important 1. On the passkey route ErrMFAInvalidCode is NOT
// the wrong-assertion sentinel — FinishAssertion returns it when
// `challenges.Peek` fails, and Peek collapses every store error (a Redis
// outage included) into ErrMFAChallengeNotFound. Charging it made one
// store's degradation spend the user's lockout budget in the other, which
// is exactly what the attempt counter's own fail-open contract exists to
// prevent (spec §5 edge case 2).
//
// The fixture fakes only the STORE, so the whole chain under test is real
// production code: store.Get errors → mfaChallengeService.Peek →
// ErrMFAChallengeNotFound → webAuthnService.FinishAssertion →
// ErrMFAInvalidCode → the handler. A fake WebAuthnService returning the
// sentinel directly would have asserted the handler's arm without proving
// the sentinel is what a store outage actually produces.
func TestWebAuthnVerifyFinish_ChallengeStoreOutageCostsNothing(t *testing.T) {
	rp, err := gowebauthn.New(&gowebauthn.Config{
		RPID:          "localhost",
		RPDisplayName: "Orkestra",
		RPOrigins:     []string{"http://localhost:8080"},
	})
	if err != nil {
		t.Fatalf("webauthn.New: %v", err)
	}
	user := &iface.User{UUID: "capped-user", Email: "capped@example.com", Role: "operator", IsActive: true}
	wa := services.NewWebAuthnService(rp, nil,
		services.NewMFAChallengeService(&downChallengeStore{}), silentTestLogger())
	h := NewWebAuthnHandler(wa, nil, newStepUpJWT(t), &stepUpUsers{user: user}, nil, "", "", false)
	counter := services.NewMemoryAttemptCounter()
	h.SetVerifyAttemptCounter(counter, services.PolicyAudienceOperator)

	// Well past the threshold: a charged outage would lock at five.
	for i := 0; i < services.MFAMaxAttempts+3; i++ {
		if code := callVerifyFinish(t, h, user.UUID); code != http.StatusUnauthorized {
			t.Fatalf("attempt %d = %d, want 401 — a store outage must never answer 429", i+1, code)
		}
	}
	key := services.AttemptKeyMFAVerify(services.PolicyAudienceOperator, user.UUID)
	if got := countFor(t, counter, key); got != 0 {
		t.Fatalf("count = %d, want 0 — a challenge-store outage is not a guess", got)
	}
}

// downChallengeStore is a services.OAuthStateStore whose reads always fail,
// standing in for an unreachable Redis. Only Get is exercised by Peek.
type downChallengeStore struct{}

func (downChallengeStore) Set(context.Context, string, []byte, time.Duration) error {
	return errStoreDown
}
func (downChallengeStore) Get(context.Context, string) ([]byte, error) {
	return nil, errStoreDown
}
func (downChallengeStore) Take(context.Context, string) ([]byte, error) { return nil, errStoreDown }
func (downChallengeStore) Delete(context.Context, string) error         { return errStoreDown }
func (downChallengeStore) DeleteByPattern(context.Context, string) error {
	return errStoreDown
}
func (downChallengeStore) Incr(context.Context, string, time.Duration) (int64, error) {
	return 0, errStoreDown
}

func silentTestLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// A locked caller is refused BEFORE the user lookup and before the
// assertion is parsed: the point of a cap is that a capped request costs
// less than an uncapped one.
func TestWebAuthnVerifyFinish_LockedCallerNeverReachesTheUserLookup(t *testing.T) {
	counter := services.NewMemoryAttemptCounter()
	users := &countingStepUpUsers{user: &iface.User{UUID: "capped-user", Email: "c@example.com", Role: "operator", IsActive: true}}
	h := NewWebAuthnHandler(refusingWebAuthn{}, nil, newStepUpJWT(t), users, nil, "", "", false)
	h.SetVerifyAttemptCounter(counter, services.PolicyAudienceOperator)

	for i := 0; i < services.MFAMaxAttempts; i++ {
		callVerifyFinish(t, h, "capped-user")
	}
	before := users.lookups.Load()
	if code := callVerifyFinish(t, h, "capped-user"); code != http.StatusTooManyRequests {
		t.Fatalf("locked = %d, want 429", code)
	}
	if got := users.lookups.Load(); got != before {
		t.Fatalf("user lookups %d → %d: a locked caller must not cost a read", before, got)
	}
}

type countingStepUpUsers struct {
	iface.UserProvider
	user    *iface.User
	lookups atomic.Int32
}

func (u *countingStepUpUsers) GetUserByID(context.Context, string) (*iface.User, error) {
	u.lookups.Add(1)
	return u.user, nil
}

// retryAfterOf pulls the Retry-After header off a headers-bearing error.
func retryAfterOf(t *testing.T, err error) string {
	t.Helper()
	type headerer interface{ GetHeaders() http.Header }
	h, ok := err.(headerer)
	if !ok {
		t.Fatalf("error %T carries no headers", err)
	}
	return h.GetHeaders().Get("Retry-After")
}

var errStoreDown = errors.New("redis unavailable")
