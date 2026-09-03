package services

// Both ChangePassword and ConfirmPasswordWithSecurity verify a password.
// Leaving them unthrottled makes them the unthrottled back door around
// the login lockout (M-8) — see the "Attempt counters" section of this
// module's CLAUDE.md. Reuses the login-lockout fixture from
// gates_fakes_test.go — read that file before editing.

import (
	"context"
	"errors"
	"testing"
	"time"

	authModels "github.com/orkestra/backend/internal/core/auth/models"
)

func TestChangePassword_LocksAfterThreshold(t *testing.T) {
	svc, _ := newLockoutTestServiceWithPassword(t, lockoutTestThreshold)
	ctx := context.Background()

	for i := 0; i < lockoutTestThreshold; i++ {
		err := svc.ChangePassword(ctx, ChangePasswordInput{
			UserUUID: knownTestUserUUID, Current: "wrong", New: "NewPassw0rd!x", IP: "203.0.113.30",
		})
		if !errors.Is(err, ErrInvalidCredentials) {
			t.Fatalf("attempt %d = %v, want ErrInvalidCredentials", i+1, err)
		}
	}
	err := svc.ChangePassword(ctx, ChangePasswordInput{
		UserUUID: knownTestUserUUID, Current: "wrong", New: "NewPassw0rd!x", IP: "203.0.113.30",
	})
	if !errors.Is(err, ErrAccountLocked) {
		t.Fatalf("attempt %d = %v, want ErrAccountLocked", lockoutTestThreshold+1, err)
	}
}

func TestConfirmPassword_LocksAfterThreshold(t *testing.T) {
	svc, _ := newLockoutTestServiceWithPassword(t, lockoutTestThreshold)
	ctx := context.Background()
	sec := &authModels.SecurityContext{SessionID: "sid-1", IPAddress: "203.0.113.31"}

	for i := 0; i < lockoutTestThreshold; i++ {
		_, err := svc.ConfirmPasswordWithSecurity(ctx, knownTestUserUUID, "wrong", []string{"pwd"}, nil, sec)
		if !errors.Is(err, ErrInvalidCredentials) {
			t.Fatalf("attempt %d = %v", i+1, err)
		}
	}
	_, err := svc.ConfirmPasswordWithSecurity(ctx, knownTestUserUUID, "wrong", []string{"pwd"}, nil, sec)
	if !errors.Is(err, ErrAccountLocked) {
		t.Fatalf("attempt %d = %v, want ErrAccountLocked", lockoutTestThreshold+1, err)
	}
}

// A lockout earned at LOGIN must be honoured here too, or the lock is
// worth nothing.
func TestChangePassword_HonoursALoginEarnedLock(t *testing.T) {
	svc, _ := newLockoutTestServiceWithPassword(t, lockoutTestThreshold)
	ctx := context.Background()

	for i := 0; i < lockoutTestThreshold; i++ {
		_, _ = svc.Login(ctx, LoginInput{Email: "known@example.com", Password: "wrong", IP: "203.0.113.32"})
	}
	err := svc.ChangePassword(ctx, ChangePasswordInput{
		UserUUID: knownTestUserUUID, Current: "wrong", New: "NewPassw0rd!x", IP: "203.0.113.32",
	})
	if !errors.Is(err, ErrAccountLocked) {
		t.Fatalf("change-password after a login lockout = %v, want ErrAccountLocked", err)
	}
}

// A failure must leave an audit row. Today ChangePassword records
// nothing at all on a wrong current password.
func TestChangePassword_FailureIsAudited(t *testing.T) {
	svc, audit := newLockoutTestServiceWithAudit(t, lockoutTestThreshold)
	ctx := context.Background()

	_ = svc.ChangePassword(ctx, ChangePasswordInput{
		UserUUID: knownTestUserUUID, Current: "wrong", New: "NewPassw0rd!x", IP: "203.0.113.33",
	})
	if !audit.sawAction("auth.password.change_failed") {
		t.Fatal("a wrong current password must leave an audit row")
	}
}

func TestConfirmPassword_FailureIsAudited(t *testing.T) {
	svc, audit := newLockoutTestServiceWithAudit(t, lockoutTestThreshold)
	ctx := context.Background()
	sec := &authModels.SecurityContext{SessionID: "sid-1", IPAddress: "203.0.113.34"}

	_, _ = svc.ConfirmPasswordWithSecurity(ctx, knownTestUserUUID, "wrong", []string{"pwd"}, nil, sec)
	if !audit.sawAction("auth.password.reconfirm_failed") {
		t.Fatal("a failed reconfirm must leave an audit row")
	}
}

// A success clears the email scope, so a user who mistyped twice and
// then succeeded is not one attempt from a lockout. Review round 1,
// Finding 2: a success must ALSO clear the durable FailedLoginCount —
// resetLoginFailures alone only touches the AttemptCounter's email
// scope, so a mistype here used to accumulate durably forever (only a
// later LOGIN success, or the lock naturally expiring, would clear it).
func TestChangePassword_SuccessResetsTheEmailScope(t *testing.T) {
	svc, users := newLockoutTestServiceWithUsers(t, 10)
	ctx := context.Background()

	for i := 0; i < 3; i++ {
		_ = svc.ChangePassword(ctx, ChangePasswordInput{
			UserUUID: knownTestUserUUID, Current: "wrong", New: "NewPassw0rd!x", IP: "203.0.113.35",
		})
	}
	if err := svc.ChangePassword(ctx, ChangePasswordInput{
		UserUUID: knownTestUserUUID, Current: correctTestPassword, New: "NewPassw0rd!x", IP: "203.0.113.35",
	}); err != nil {
		t.Fatalf("successful change: %v", err)
	}
	v, _ := svc.attempts.Locked(ctx, AttemptKeyEmail(PolicyAudienceOperator, "known@example.com"), Limit{Threshold: 10, Window: time.Minute})
	if v.Count != 0 {
		t.Fatalf("email scope = %d after a successful change, want 0", v.Count)
	}
	if !users.failedLoginsCleared("known@example.com") {
		t.Fatal("a successful change must also clear the durable FailedLoginCount (ClearFailedLogins)")
	}
}

// ConfirmPasswordWithSecurity's success must clear both scopes too —
// the same Finding 2 gap, on the reconfirm route.
func TestConfirmPassword_SuccessClearsBothScopes(t *testing.T) {
	svc, users := newLockoutTestServiceWithUsers(t, 10)
	ctx := context.Background()
	sec := &authModels.SecurityContext{SessionID: "sid-1", IPAddress: "203.0.113.38"}

	for i := 0; i < 3; i++ {
		_, _ = svc.ConfirmPasswordWithSecurity(ctx, knownTestUserUUID, "wrong", []string{"pwd"}, nil, sec)
	}
	if _, err := svc.ConfirmPasswordWithSecurity(ctx, knownTestUserUUID, correctTestPassword, []string{"pwd"}, nil, sec); err != nil {
		t.Fatalf("successful reconfirm: %v", err)
	}
	v, _ := svc.attempts.Locked(ctx, AttemptKeyEmail(PolicyAudienceOperator, "known@example.com"), Limit{Threshold: 10, Window: time.Minute})
	if v.Count != 0 {
		t.Fatalf("email scope = %d after a successful reconfirm, want 0", v.Count)
	}
	if !users.failedLoginsCleared("known@example.com") {
		t.Fatal("a successful reconfirm must also clear the durable FailedLoginCount (ClearFailedLogins)")
	}
}

// Review round 1, Finding 1: neither gate cleared an EXPIRED durable
// lock before verifying. On the fail-open path (attempt-counter store
// down) the durable rule falls back to comparing FailedLoginCount+1
// against the threshold — against the STALE count an expired lock left
// behind — so a legitimate user's first wrong password after the lock's
// natural expiry re-locked them for a full new duration. Login was
// already fixed for this; these two didn't share the fix until it was
// extracted into durableLockOrClear.
func TestChangePassword_ExpiredDurableLockDoesNotInstantlyRelockUnderCounterOutage(t *testing.T) {
	svc, users := newLockoutTestServiceWithFailingCounter(t, lockoutTestThreshold)
	ctx := context.Background()
	users.setExpiredLock("known@example.com", lockoutTestThreshold+9)

	err := svc.ChangePassword(ctx, ChangePasswordInput{
		UserUUID: knownTestUserUUID, Current: "wrong", New: "NewPassw0rd!x", IP: "203.0.113.39",
	})
	if !errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("first attempt after expiry (counter down) = %v, want ErrInvalidCredentials", err)
	}
	if users.lockedUntilSet("known@example.com") {
		t.Fatal("a stale FailedLoginCount must not re-lock the account on the first attempt after its lock expired")
	}
}

func TestConfirmPassword_ExpiredDurableLockDoesNotInstantlyRelockUnderCounterOutage(t *testing.T) {
	svc, users := newLockoutTestServiceWithFailingCounter(t, lockoutTestThreshold)
	ctx := context.Background()
	users.setExpiredLock("known@example.com", lockoutTestThreshold+9)
	sec := &authModels.SecurityContext{SessionID: "sid-1", IPAddress: "203.0.113.40"}

	_, err := svc.ConfirmPasswordWithSecurity(ctx, knownTestUserUUID, "wrong", []string{"pwd"}, nil, sec)
	if !errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("first attempt after expiry (counter down) = %v, want ErrInvalidCredentials", err)
	}
	if users.lockedUntilSet("known@example.com") {
		t.Fatal("a stale FailedLoginCount must not re-lock the account on the first attempt after its lock expired")
	}
}
