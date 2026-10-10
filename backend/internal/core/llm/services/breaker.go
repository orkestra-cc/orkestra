package services

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/orkestra/backend/pkg/sdk/iface"
)

// Breaker is a per-identity circuit breaker for provider calls. The key is
// provider|baseUrl|modelId|credentialUuid: one bad key or endpoint never
// trips another. threshold consecutive provider faults open the circuit
// for cooldown; after it the next call is let through, and a fault on it
// re-opens the circuit at once (half-open), a success closes it.
//
// Only provider faults count (ruling 5.2): unavailability, provider-side
// timeouts, rejected credentials and exhausted quota. A caller hanging
// up, a malformed request, a capability gap, the org budget or the
// user's linked account say nothing about the provider's health and are
// ignored (neither counted nor treated as a success).
type Breaker struct {
	threshold int
	cooldown  time.Duration
	now       func() time.Time

	mu     sync.Mutex
	states map[string]*breakerState
}

type breakerState struct {
	failures  int
	openUntil time.Time
	lastErr   error
}

func NewBreaker(threshold int, cooldown time.Duration) *Breaker {
	if threshold < 1 {
		threshold = 1
	}
	return &Breaker{threshold: threshold, cooldown: cooldown, now: time.Now, states: map[string]*breakerState{}}
}

// Allow returns nil when a call on key may proceed, or an error wrapping
// both iface.ErrLLMProviderUnavailable and the fault that opened the
// circuit.
func (b *Breaker) Allow(key string) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	s := b.states[key]
	if s == nil || !b.now().Before(s.openUntil) {
		return nil
	}
	return fmt.Errorf("%w: circuit open: %w", iface.ErrLLMProviderUnavailable, s.lastErr)
}

// Record reports the outcome of a call on key: nil closes the circuit,
// a provider fault counts towards opening it, anything else is ignored.
func (b *Breaker) Record(key string, err error) {
	if err != nil && !providerFault(err) {
		return
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if err == nil {
		delete(b.states, key) // keeps the map bounded by the failing identities
		return
	}
	s := b.states[key]
	if s == nil {
		s = &breakerState{}
		b.states[key] = s
	}
	s.failures++
	if s.failures >= b.threshold {
		s.openUntil = b.now().Add(b.cooldown)
		s.lastErr = err
	}
}

// providerFault: the error describes the provider (or its credential),
// not the caller. context.DeadlineExceeded alone is the router's
// per-attempt deadline; the router never records a call whose caller
// context ended, so a caller's own deadline does not reach here.
func providerFault(err error) bool {
	for _, notFault := range []error{context.Canceled, iface.ErrLLMInvalidRequest, iface.ErrLLMCapabilityMismatch,
		iface.ErrLLMBudgetExceeded, iface.ErrLLMUserAccountRequired, iface.ErrLLMUserAccountReauth,
		iface.ErrLLMPlanUsageDisabled, iface.ErrLLMPartialOutput} {
		if errors.Is(err, notFault) {
			return false
		}
	}
	return errors.Is(err, iface.ErrLLMProviderUnavailable) || errors.Is(err, iface.ErrLLMQuotaExhausted) ||
		errors.Is(err, context.DeadlineExceeded)
}
