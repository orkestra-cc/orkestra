package services

// The per-challenge attempt cap is the only rate limit on a 6-digit
// code. It was a read-modify-write over Redis (Peek → ++ → Set), so
// concurrent verifies all read the same counter and each wrote back the
// same value: N parallel guesses cost 1 attempt instead of N. An
// attacker who fires requests in parallel rather than in series gets far
// more than MFAMaxAttempts tries out of a single challenge.

import (
	"context"
	"sync"
	"testing"
)

func TestIncrementAttempts_ConcurrentCallersEachConsumeOne(t *testing.T) {
	store := NewMemoryOAuthStateStore()
	svc := NewMFAChallengeService(store)
	ch, err := svc.Begin(context.Background(), "u-1", MFAPurposeEnroll, "")
	if err != nil {
		t.Fatalf("Begin: %v", err)
	}

	// Two concurrent guesses — fewer than the cap, so neither should
	// destroy the challenge, but the counter must land on 2.
	var wg sync.WaitGroup
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _ = svc.IncrementAttempts(context.Background(), ch.ID)
		}()
	}
	wg.Wait()

	after, err := svc.Peek(context.Background(), ch.ID)
	if err != nil {
		t.Fatalf("Peek after concurrent increments: %v", err)
	}
	if after.Attempts != 2 {
		t.Errorf("Attempts = %d, want 2 — concurrent increments were lost", after.Attempts)
	}
}

func TestIncrementAttempts_ExhaustionDeletesChallengeUnderConcurrency(t *testing.T) {
	store := NewMemoryOAuthStateStore()
	svc := NewMFAChallengeService(store)
	ch, err := svc.Begin(context.Background(), "u-2", MFAPurposeEnroll, "")
	if err != nil {
		t.Fatalf("Begin: %v", err)
	}

	// Fire well past the cap in parallel. However the increments
	// interleave, the challenge must end up gone.
	var wg sync.WaitGroup
	for i := 0; i < MFAMaxAttempts*4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _ = svc.IncrementAttempts(context.Background(), ch.ID)
		}()
	}
	wg.Wait()

	if _, err := svc.Peek(context.Background(), ch.ID); err == nil {
		t.Error("a challenge past MFAMaxAttempts must be destroyed regardless of interleaving")
	}
}

func TestIncrementAttempts_SerialCountingUnchanged(t *testing.T) {
	store := NewMemoryOAuthStateStore()
	svc := NewMFAChallengeService(store)
	ch, err := svc.Begin(context.Background(), "u-3", MFAPurposeEnroll, "")
	if err != nil {
		t.Fatalf("Begin: %v", err)
	}

	for want := 1; want < MFAMaxAttempts; want++ {
		got, err := svc.IncrementAttempts(context.Background(), ch.ID)
		if err != nil {
			t.Fatalf("IncrementAttempts #%d: %v", want, err)
		}
		if got != want {
			t.Fatalf("attempt %d reported %d", want, got)
		}
	}

	// The one that reaches the cap destroys the challenge.
	if _, err := svc.IncrementAttempts(context.Background(), ch.ID); err != nil {
		t.Fatalf("final IncrementAttempts: %v", err)
	}
	if _, err := svc.Peek(context.Background(), ch.ID); err == nil {
		t.Error("challenge must be gone once the cap is reached")
	}
}
