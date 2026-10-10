package services

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/orkestra/backend/internal/core/llm/providers"
	"github.com/orkestra/backend/pkg/sdk/iface"
)

func newClockBreaker(threshold int, cooldown time.Duration) (*Breaker, *time.Time) {
	b := NewBreaker(threshold, cooldown)
	now := time.Date(2026, 10, 10, 12, 0, 0, 0, time.UTC)
	b.now = func() time.Time { return now }
	return b, &now
}

func TestBreaker_OpensAfterThresholdAndWrapsCause(t *testing.T) {
	b, now := newClockBreaker(3, 30*time.Second)
	const key = "openai|https://api.openai.com|gpt|c1"
	for i := 0; i < 2; i++ {
		b.Record(key, iface.ErrLLMQuotaExhausted)
		if err := b.Allow(key); err != nil {
			t.Fatalf("open after %d failures: %v", i+1, err)
		}
	}
	b.Record(key, iface.ErrLLMQuotaExhausted)
	err := b.Allow(key)
	if !errors.Is(err, iface.ErrLLMProviderUnavailable) || !errors.Is(err, iface.ErrLLMQuotaExhausted) {
		t.Fatalf("open circuit err = %v, want both sentinels", err)
	}
	// Distinct keys are independent.
	if err := b.Allow("other"); err != nil {
		t.Fatalf("other key: %v", err)
	}
	*now = now.Add(30 * time.Second)
	if err := b.Allow(key); err != nil {
		t.Fatalf("after cooldown: %v", err)
	}
	// Half-open: the first failure after the cooldown re-opens at once.
	b.Record(key, iface.ErrLLMProviderUnavailable)
	if err := b.Allow(key); !errors.Is(err, iface.ErrLLMProviderUnavailable) {
		t.Fatalf("half-open failure did not re-open: %v", err)
	}
}

func TestBreaker_SuccessResets(t *testing.T) {
	b, _ := newClockBreaker(3, 30*time.Second)
	b.Record("k", iface.ErrLLMProviderUnavailable)
	b.Record("k", iface.ErrLLMProviderUnavailable)
	b.Record("k", nil)
	b.Record("k", iface.ErrLLMProviderUnavailable)
	b.Record("k", iface.ErrLLMProviderUnavailable)
	if err := b.Allow("k"); err != nil {
		t.Fatalf("success did not reset the count: %v", err)
	}
}

// Ruling 5.2: only provider faults count. A caller hanging up, a malformed
// request, a capability gap, budget or user-account errors never open the
// circuit for everyone else.
func TestBreaker_CountsOnlyProviderFaults(t *testing.T) {
	notFaults := []error{
		context.Canceled,
		fmt.Errorf("llm providers: request canceled: %w", context.Canceled),
		fmt.Errorf("%w: bad field", iface.ErrLLMInvalidRequest),
		iface.ErrLLMCapabilityMismatch,
		iface.ErrLLMBudgetExceeded,
		iface.ErrLLMUserAccountRequired,
		iface.ErrLLMUserAccountReauth,
		iface.ErrLLMPlanUsageDisabled,
		iface.ErrLLMPartialOutput,
		errors.New("sink closed"),
	}
	for _, e := range notFaults {
		b, _ := newClockBreaker(1, time.Minute)
		b.Record("k", e)
		if err := b.Allow("k"); err != nil {
			t.Errorf("%v opened the circuit: %v", e, err)
		}
	}
	faults := []error{
		iface.ErrLLMProviderUnavailable,
		iface.ErrLLMQuotaExhausted,
		fmt.Errorf("%w: %w", iface.ErrLLMProviderUnavailable, context.DeadlineExceeded),
		context.DeadlineExceeded, // the router's own per-attempt deadline
		fmt.Errorf("%w: %w (status 401)", providers.ErrUnauthorized, iface.ErrLLMProviderUnavailable),
	}
	for _, e := range faults {
		b, _ := newClockBreaker(1, time.Minute)
		b.Record("k", e)
		if err := b.Allow("k"); err == nil {
			t.Errorf("%v did not count as a provider fault", e)
		}
	}
}
