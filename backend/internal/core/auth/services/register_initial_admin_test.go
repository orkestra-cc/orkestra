package services

import (
	"context"
	"errors"
	"testing"
)

// A caller that loses the first-admin claim — the concurrent-first-install
// race, where both requests passed the setup service's user-count check —
// must get a named sentinel, so the setup handler can answer 409 like the
// sequential "a user already exists" case instead of a generic 400.
func TestRegisterInitialAdmin_LostFirstAdminClaim_ReturnsErrInitialAdminExists(t *testing.T) {
	env := newGatesEnv(t, PolicyAudienceOperator, nil, nil)
	env.claimer.claimed["winner-uuid"] = true // another request already holds the seat

	_, err := env.auth.RegisterInitialAdmin(context.Background(), "second@example.com", "a-long-enough-passphrase", "Second Admin", "10.0.0.2")

	if !errors.Is(err, ErrInitialAdminExists) {
		t.Fatalf("err = %v, want ErrInitialAdminExists", err)
	}
	if n := len(env.users.createdUsers); n != 0 {
		t.Errorf("created %d users after losing the claim, want 0", n)
	}
}
