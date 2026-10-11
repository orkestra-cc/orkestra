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
		b.Record(key, 0, iface.ErrLLMQuotaExhausted)
		if _, err := b.Allow(key); err != nil {
			t.Fatalf("open after %d failures: %v", i+1, err)
		}
	}
	b.Record(key, 0, iface.ErrLLMQuotaExhausted)
	_, err := b.Allow(key)
	if !errors.Is(err, iface.ErrLLMProviderUnavailable) || !errors.Is(err, iface.ErrLLMQuotaExhausted) {
		t.Fatalf("open circuit err = %v, want both sentinels", err)
	}
	// Distinct keys are independent.
	if _, err := b.Allow("other"); err != nil {
		t.Fatalf("other key: %v", err)
	}
	*now = now.Add(30 * time.Second)
	probe, err := b.Allow(key)
	if err != nil {
		t.Fatalf("after cooldown: %v", err)
	}
	// Half-open: one probe at a time.
	if _, err := b.Allow(key); !errors.Is(err, iface.ErrLLMProviderUnavailable) {
		t.Fatalf("second concurrent probe allowed: %v", err)
	}
	// The probe's failure re-opens at once.
	b.Record(key, probe, iface.ErrLLMProviderUnavailable)
	if _, err := b.Allow(key); !errors.Is(err, iface.ErrLLMProviderUnavailable) {
		t.Fatalf("half-open failure did not re-open: %v", err)
	}
}

func TestBreaker_SuccessResets(t *testing.T) {
	b, _ := newClockBreaker(3, 30*time.Second)
	b.Record("k", 0, iface.ErrLLMProviderUnavailable)
	b.Record("k", 0, iface.ErrLLMProviderUnavailable)
	b.Record("k", 0, nil)
	b.Record("k", 0, iface.ErrLLMProviderUnavailable)
	b.Record("k", 0, iface.ErrLLMProviderUnavailable)
	if _, err := b.Allow("k"); err != nil {
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
		b.Record("k", 0, e)
		if _, err := b.Allow("k"); err != nil {
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
		b.Record("k", 0, e)
		if _, err := b.Allow("k"); err == nil {
			t.Errorf("%v did not count as a provider fault", e)
		}
	}
}

func TestBreaker_ProbeReleasedOrExpires(t *testing.T) {
	b, now := newClockBreaker(1, 10*time.Second)
	b.Record("k", 0, iface.ErrLLMProviderUnavailable)
	*now = now.Add(10 * time.Second)
	ticket, err := b.Allow("k")
	if err != nil || ticket == 0 {
		t.Fatalf("probe ticket=%d err=%v", ticket, err)
	}
	// A probe that ended without a verdict (caller hang-up) frees the slot.
	b.Release("k", ticket)
	if _, err := b.Allow("k"); err != nil {
		t.Fatalf("released probe slot still held: %v", err)
	}
	// A probe that never reports back stops blocking after a cooldown.
	if _, err := b.Allow("k"); err == nil {
		t.Fatal("second probe allowed")
	}
	*now = now.Add(10 * time.Second)
	fresh, err := b.Allow("k")
	if err != nil {
		t.Fatalf("stale probe still blocks: %v", err)
	}
	b.Record("k", fresh, nil)
	if _, err := b.Allow("k"); err != nil {
		t.Fatal(err)
	}
}

// A call admitted while the circuit was still closed carries no ticket:
// its late Release or non-fault Record must not free a newer probe's slot.
func TestBreaker_OlderCallCannotFreeTheProbe(t *testing.T) {
	b, now := newClockBreaker(1, 10*time.Second)
	old, err := b.Allow("k")
	if err != nil || old != 0 {
		t.Fatalf("closed circuit ticket=%d err=%v", old, err)
	}
	b.Record("k", 0, iface.ErrLLMProviderUnavailable) // another call opens it
	*now = now.Add(10 * time.Second)
	probe, err := b.Allow("k")
	if err != nil || probe == 0 {
		t.Fatalf("probe ticket=%d err=%v", probe, err)
	}
	b.Release("k", old)
	b.Record("k", old, context.Canceled)
	if _, err := b.Allow("k"); !errors.Is(err, iface.ErrLLMProviderUnavailable) {
		t.Fatalf("an older call freed the live probe: %v", err)
	}
	// A stale ticket from an earlier probe does not free the new one either.
	b.Release("k", probe+1)
	if _, err := b.Allow("k"); err == nil {
		t.Fatal("a foreign ticket freed the live probe")
	}
	b.Release("k", probe)
	if _, err := b.Allow("k"); err != nil {
		t.Fatalf("the probe's own release did not free the slot: %v", err)
	}
}

// While the circuit is open only the current probe settles it: an older
// call's success does not close it under a live probe, and its fault does
// not push the cooldown out.
func TestBreaker_OnlyTheProbeSettlesAnOpenCircuit(t *testing.T) {
	b, now := newClockBreaker(1, 10*time.Second)
	b.Record("k", 0, iface.ErrLLMProviderUnavailable) // open
	*now = now.Add(10 * time.Second)
	probe, err := b.Allow("k")
	if err != nil || probe == 0 {
		t.Fatalf("probe ticket=%d err=%v", probe, err)
	}
	b.Record("k", 0, nil) // an older call that finished late
	if _, err := b.Allow("k"); err == nil {
		t.Fatal("an older call's success closed the circuit under a live probe")
	}
	b.Record("k", 0, iface.ErrLLMProviderUnavailable)
	b.Record("k", probe, nil)
	if _, err := b.Allow("k"); err != nil {
		t.Fatalf("the probe's success did not close the circuit: %v", err)
	}
}

// Only a quota cause stays final through an open circuit; a timeout or an
// outage must stay fallbackable, and a rejected credential keeps its code.
func TestBreaker_OpenCircuitErrorKeepsOnlyTheCauseClass(t *testing.T) {
	b, _ := newClockBreaker(1, time.Minute)
	b.Record("t", 0, fmt.Errorf("%w: %w", iface.ErrLLMProviderUnavailable, context.DeadlineExceeded))
	if _, err := b.Allow("t"); !errors.Is(err, iface.ErrLLMProviderUnavailable) || errors.Is(err, context.DeadlineExceeded) || !fallbackable(err) {
		t.Fatalf("timeout-opened circuit err = %v", err)
	}
	b.Record("q", 0, iface.ErrLLMQuotaExhausted)
	if _, err := b.Allow("q"); !errors.Is(err, iface.ErrLLMQuotaExhausted) || fallbackable(err) {
		t.Fatalf("quota-opened circuit err = %v", err)
	}
	b.Record("u", 0, fmt.Errorf("%w: %w", providers.ErrUnauthorized, iface.ErrLLMProviderUnavailable))
	if _, err := b.Allow("u"); !errors.Is(err, providers.ErrUnauthorized) || !fallbackable(err) {
		t.Fatalf("unauthorized-opened circuit err = %v", err)
	}
}
