package errors

import (
	"context"
	"testing"
	"time"
)

// TestIsLockedOutNotLockedInitially verifies a fresh identifier that has
// never failed is not reported as locked out.
func TestIsLockedOutNotLockedInitially(t *testing.T) {
	rl := NewRateLimiter()
	rl.SetAuthFailedConfig(1, time.Minute)
	ctx := context.Background()

	if rl.IsLockedOut(ctx, "probe-fresh") {
		t.Fatalf("IsLockedOut on a fresh identifier: got true, want false")
	}
}

// TestIsLockedOutRepeatedCallsDoNotConsume is the core non-consuming
// property: unlike IsBlocked (which calls Check, and Check's
// TokenBucket.consume deducts a token on every call), IsLockedOut must
// be a pure peek. Calling it many times in a row must never itself tip
// an identifier into lockout.
func TestIsLockedOutRepeatedCallsDoNotConsume(t *testing.T) {
	rl := NewRateLimiter()
	rl.SetAuthFailedConfig(1, time.Minute)
	ctx := context.Background()

	for i := 0; i < 50; i++ {
		if rl.IsLockedOut(ctx, "probe-repeated") {
			t.Fatalf("IsLockedOut call %d: got true, want false — repeated peeks must not themselves cause lockout", i)
		}
	}
}

// TestIsLockedOutAfterFailuresReachThreshold verifies IsLockedOut does
// observe real failures recorded via RecordFailedAuth (the only thing
// that should be able to spend budget).
func TestIsLockedOutAfterFailuresReachThreshold(t *testing.T) {
	rl := NewRateLimiter()
	rl.SetAuthFailedConfig(1, time.Minute)
	ctx := context.Background()

	rl.RecordFailedAuth(ctx, "probe-failed")

	if !rl.IsLockedOut(ctx, "probe-failed") {
		t.Fatalf("IsLockedOut after RecordFailedAuth reached threshold: got false, want true")
	}
}

// TestIsLockedOutStaysLockedAcrossRepeatedChecks confirms IsLockedOut
// itself never contributes to (or relieves) an already-tripped lockout:
// once locked, repeated peeks must keep reporting locked.
func TestIsLockedOutStaysLockedAcrossRepeatedChecks(t *testing.T) {
	rl := NewRateLimiter()
	rl.SetAuthFailedConfig(1, time.Minute)
	ctx := context.Background()
	rl.RecordFailedAuth(ctx, "probe-stays-locked")

	for i := 0; i < 10; i++ {
		if !rl.IsLockedOut(ctx, "probe-stays-locked") {
			t.Fatalf("IsLockedOut call %d after lockout: got false, want true", i)
		}
	}
}

// TestIsLockedOutDoesNotSpendBudgetForSubsequentFailures uses a wider
// capacity (2) to prove peeking never eats into the failure budget that
// real RecordFailedAuth calls are supposed to spend: many peeks first,
// then exactly two real failures should be required to trip lockout —
// not fewer.
func TestIsLockedOutDoesNotSpendBudgetForSubsequentFailures(t *testing.T) {
	rl := NewRateLimiter()
	rl.SetAuthFailedConfig(2, time.Minute)
	ctx := context.Background()
	const id = "probe-budget"

	for i := 0; i < 20; i++ {
		if rl.IsLockedOut(ctx, id) {
			t.Fatalf("IsLockedOut call %d: got true, want false (peeking must not spend budget)", i)
		}
	}

	rl.RecordFailedAuth(ctx, id)
	if rl.IsLockedOut(ctx, id) {
		t.Fatalf("IsLockedOut after 1st failure (capacity 2): got true, want false")
	}

	rl.RecordFailedAuth(ctx, id)
	if !rl.IsLockedOut(ctx, id) {
		t.Fatalf("IsLockedOut after 2nd failure (capacity 2): got false, want true")
	}
}

// Note: an "unlocked after the refill window elapses" case is
// deliberately not covered here. SetAuthFailedConfig hardcodes
// RefillRate to 1 token/sec regardless of the configured window, so
// exercising a real refill needs a wall-clock sleep — timing-dependent
// and prone to flake under load. The non-consuming property
// (TestIsLockedOutRepeatedCallsDoNotConsume /
// TestIsLockedOutDoesNotSpendBudgetForSubsequentFailures) and the
// locked-after-failures property above are the load-bearing behaviors
// IsLockedOut adds; refill accounting itself is inherited unchanged
// from TokenBucket.consume's existing math, already exercised
// indirectly by every other RateLimiter caller.
