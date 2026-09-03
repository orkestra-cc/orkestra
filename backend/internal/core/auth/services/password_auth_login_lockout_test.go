package services

// The login lockout has to be indistinguishable between a known and an
// unknown address, in status code AND in cost. These tests measure both.
// They reuse the existing gateUserFake / fakePasswordService harness —
// read gates_fakes_test.go before editing.

import (
	"context"
	"errors"
	"testing"
	"time"
)

const lockoutTestThreshold = 3

// A known and an unknown email must produce the SAME status sequence
// over threshold+1 attempts. Answering 429 for one and 401 for the other
// is a free account-existence oracle.
func TestLogin_KnownAndUnknownEmailLockIdentically(t *testing.T) {
	seq := func(email string) []error {
		svc := newLockoutTestService(t, lockoutTestThreshold)
		var out []error
		for i := 0; i < lockoutTestThreshold+1; i++ {
			_, err := svc.Login(context.Background(), LoginInput{
				Email: email, Password: "wrong-password", IP: "203.0.113.10",
			})
			out = append(out, err)
		}
		return out
	}

	known := seq("known@example.com") // seeded by the fixture
	unknown := seq("nobody@example.com")

	if len(known) != len(unknown) {
		t.Fatalf("sequence lengths differ: %d vs %d", len(known), len(unknown))
	}
	for i := range known {
		if !sameSentinel(known[i], unknown[i]) {
			t.Fatalf("attempt %d: known=%v unknown=%v — the answers must be identical",
				i+1, known[i], unknown[i])
		}
	}
	// And the last one must be the lockout, not a 401.
	if !errors.Is(known[len(known)-1], ErrAccountLocked) {
		t.Fatalf("attempt %d for a known email = %v, want ErrAccountLocked",
			len(known), known[len(known)-1])
	}
}

// Every non-success branch must pay the argon2 cost. Today the inactive
// account and the service-principal branches return without one, which
// makes them measurably faster than a wrong password.
func TestLogin_EveryFailureBranchRunsAVerify(t *testing.T) {
	cases := []struct {
		name  string
		email string
	}{
		{"unknown user", "nobody@example.com"},
		{"inactive user", "inactive@example.com"},
		{"service principal", "svc@example.com"},
		{"no password hash", "oauthonly@example.com"},
		{"wrong password", "known@example.com"},
		{"durably locked", "locked@example.com"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			svc, pw := newLockoutTestServiceWithPassword(t, lockoutTestThreshold)
			before := pw.verifyCalls()
			_, err := svc.Login(context.Background(), LoginInput{
				Email: tc.email, Password: "whatever", IP: "203.0.113.11",
			})
			if err == nil {
				t.Fatal("want a failure")
			}
			if pw.verifyCalls() == before {
				t.Fatalf("%s returned without a password verify — a measurably cheaper branch is a timing oracle", tc.name)
			}
		})
	}
}

// A locked scope answers before the user lookup and records NOTHING —
// otherwise the lock extends itself forever under a running attack.
func TestLogin_LockedScopeRecordsNothing(t *testing.T) {
	svc, counter := newLockoutTestServiceWithCounter(t, 1)
	ctx := context.Background()

	_, _ = svc.Login(ctx, LoginInput{Email: "known@example.com", Password: "wrong", IP: "203.0.113.12"})
	v, _ := counter.Locked(ctx, AttemptKeyEmail(PolicyAudienceOperator, "known@example.com"), Limit{Threshold: 1, Window: time.Minute})
	countAfterFirst := v.Count

	for i := 0; i < 5; i++ {
		_, err := svc.Login(ctx, LoginInput{Email: "known@example.com", Password: "wrong", IP: "203.0.113.12"})
		if !errors.Is(err, ErrAccountLocked) {
			t.Fatalf("attempt %d = %v, want ErrAccountLocked", i, err)
		}
	}
	v, _ = counter.Locked(ctx, AttemptKeyEmail(PolicyAudienceOperator, "known@example.com"), Limit{Threshold: 1, Window: time.Minute})
	if v.Count != countAfterFirst {
		t.Fatalf("counter moved from %d to %d while locked — a lock must not extend itself", countAfterFirst, v.Count)
	}
}

// The durable lock is the SECOND line. With the counter unavailable it
// must still cap guessing against an existing account, and it must NOT
// invent a lock for an unknown one (there is no document to write).
func TestLogin_CounterUnavailableFallsBackToDurableRule(t *testing.T) {
	svc, users := newLockoutTestServiceWithFailingCounter(t, lockoutTestThreshold)
	ctx := context.Background()

	for i := 0; i < lockoutTestThreshold; i++ {
		_, err := svc.Login(ctx, LoginInput{Email: "known@example.com", Password: "wrong", IP: "203.0.113.13"})
		if !errors.Is(err, ErrInvalidCredentials) {
			t.Fatalf("attempt %d = %v, want ErrInvalidCredentials while the counter is down", i+1, err)
		}
	}
	if !users.lockedUntilSet("known@example.com") {
		t.Fatal("the durable rule must lock the account when the counter cannot")
	}

	// Documented fail-open: an unknown email is answered 401 throughout.
	for i := 0; i < lockoutTestThreshold+2; i++ {
		_, err := svc.Login(ctx, LoginInput{Email: "nobody@example.com", Password: "wrong", IP: "203.0.113.13"})
		if !errors.Is(err, ErrInvalidCredentials) {
			t.Fatalf("unknown-email attempt %d = %v, want ErrInvalidCredentials", i+1, err)
		}
	}
}

// An EXPIRED durable lock must be cleared BEFORE the verify. Today the
// counter it is compared against is never reset, so the first wrong
// password after a lock expires re-locks the account immediately.
func TestLogin_ExpiredDurableLockIsClearedBeforeVerify(t *testing.T) {
	svc, users := newLockoutTestServiceWithUsers(t, lockoutTestThreshold)
	ctx := context.Background()
	users.setExpiredLock("known@example.com", lockoutTestThreshold+9)

	_, err := svc.Login(ctx, LoginInput{Email: "known@example.com", Password: "wrong", IP: "203.0.113.14"})
	if !errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("first attempt after expiry = %v, want ErrInvalidCredentials, not an instant re-lock", err)
	}
	if !users.failedLoginsCleared("known@example.com") {
		t.Fatal("ClearFailedLogins must run before the verify when LockedUntil is in the past")
	}
	// The stale FailedLoginCount must not survive the clear either: had
	// it, the very next wrong password would compare 12+1 against the
	// threshold and re-lock the account on attempt one.
	if users.lockedUntilSet("known@example.com") {
		t.Fatal("one wrong password after an expiry must not re-lock the account")
	}
}

// A successful login clears the EMAIL scope only. Resetting the IP scope
// would let one correct login launder a stuffing run from that address.
func TestLogin_SuccessResetsEmailScopeNotIPScope(t *testing.T) {
	svc, counter := newLockoutTestServiceWithCounter(t, 10)
	ctx := context.Background()
	ip := "203.0.113.15"

	for i := 0; i < 3; i++ {
		_, _ = svc.Login(ctx, LoginInput{Email: "known@example.com", Password: "wrong", IP: ip})
	}
	if _, err := svc.Login(ctx, LoginInput{Email: "known@example.com", Password: correctTestPassword, IP: ip}); err != nil {
		t.Fatalf("successful login: %v", err)
	}

	emailV, _ := counter.Locked(ctx, AttemptKeyEmail(PolicyAudienceOperator, "known@example.com"), Limit{Threshold: 10, Window: time.Minute})
	if emailV.Count != 0 {
		t.Fatalf("email scope = %d after a successful login, want 0", emailV.Count)
	}
	ipV, _ := counter.Locked(ctx, AttemptKeyIP(ip), Limit{Threshold: 10, Window: time.Minute})
	if ipV.Count == 0 {
		t.Fatal("the IP scope must NOT be reset by a success — one correct login cannot launder a stuffing run")
	}
}

// Edge case 31: six wrong passwords for six different accounts from one
// office egress must lock none of them and not the address, because the
// address threshold is 100, not 5.
func TestLogin_SharedEgressDoesNotLockTheAddress(t *testing.T) {
	svc, counter := newLockoutTestServiceWithCounter(t, lockoutTestThreshold)
	ctx := context.Background()
	ip := "203.0.113.16"

	for i := 0; i < 6; i++ {
		email := "user" + string(rune('a'+i)) + "@example.com"
		_, err := svc.Login(ctx, LoginInput{Email: email, Password: "wrong", IP: ip})
		if errors.Is(err, ErrAccountLocked) {
			t.Fatalf("attempt %d locked; six failures across six accounts must not lock the address", i+1)
		}
	}
	v, _ := counter.Locked(ctx, AttemptKeyIP(ip), Limit{Threshold: 100, Window: 15 * time.Minute})
	if v.Locked {
		t.Fatal("the address must not be locked at 6 failures with a threshold of 100")
	}
}

// An unresolvable client IP must SKIP the address scope rather than
// share one key with every other such caller.
func TestLogin_EmptyIPSkipsTheAddressScope(t *testing.T) {
	svc, counter := newLockoutTestServiceWithCounter(t, 100)
	ctx := context.Background()

	for i := 0; i < 5; i++ {
		_, _ = svc.Login(ctx, LoginInput{Email: "known@example.com", Password: "wrong", IP: ""})
	}
	v, err := counter.Locked(ctx, AttemptKeyIP(""), Limit{Threshold: 100, Window: time.Minute})
	if err != nil {
		t.Fatalf("Locked: %v", err)
	}
	if v.Count != 0 {
		t.Fatalf("an empty IP produced a counter with count %d; it must produce none", v.Count)
	}
}
