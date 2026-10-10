package services

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/orkestra/backend/internal/core/llm/providers"
	"github.com/orkestra/backend/pkg/sdk/iface"
)

// Breaker is a per-identity circuit breaker for provider calls. The key is
// provider|baseUrl|modelId|credentialUuid: one bad key or endpoint never
// trips another. threshold consecutive provider faults open the circuit
// for cooldown; after it a single probe call is let through (half-open):
// a fault on it re-opens the circuit at once, a success closes it, and a
// probe that ends without a verdict (Release) frees the slot. A probe
// that never reports back stops holding the slot after one cooldown.
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
	failures     int
	openUntil    time.Time
	lastErr      error
	probeStarted time.Time // zero = no probe in flight
}

func NewBreaker(threshold int, cooldown time.Duration) *Breaker {
	if threshold < 1 {
		threshold = 1
	}
	return &Breaker{threshold: threshold, cooldown: cooldown, now: time.Now, states: map[string]*breakerState{}}
}

// Allow returns nil when a call on key may proceed (a nil from a
// half-open circuit makes the caller the probe: it must Record or
// Release), or an open-circuit error (see openErr).
func (b *Breaker) Allow(key string) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	s := b.states[key]
	if s == nil || s.failures < b.threshold {
		return nil
	}
	now := b.now()
	if now.Before(s.openUntil) {
		return openErr(s.lastErr)
	}
	if !s.probeStarted.IsZero() && now.Before(s.probeStarted.Add(b.cooldown)) {
		return openErr(s.lastErr) // another caller is probing
	}
	s.probeStarted = now
	return nil
}

// openErr wraps iface.ErrLLMProviderUnavailable and keeps only the cause
// classes the router acts on: an exhausted quota stays final (no
// fallback, plan_quota_exhausted) and a rejected credential keeps its
// credential_rejected code. Any other cause — a timeout above all — is
// carried as text only, so the open circuit stays fallbackable instead of
// inheriting context.DeadlineExceeded's finality.
func openErr(cause error) error {
	switch {
	case errors.Is(cause, iface.ErrLLMQuotaExhausted):
		return fmt.Errorf("%w: circuit open: %w", iface.ErrLLMProviderUnavailable, cause)
	case errors.Is(cause, providers.ErrUnauthorized):
		return fmt.Errorf("%w: circuit open: %w (%v)", iface.ErrLLMProviderUnavailable, providers.ErrUnauthorized, cause)
	}
	return fmt.Errorf("%w: circuit open: %v", iface.ErrLLMProviderUnavailable, cause)
}

// Release ends a call that produced no verdict on the provider (the
// caller left, or its own deadline fired): it frees a half-open probe
// slot without counting anything.
func (b *Breaker) Release(key string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if s := b.states[key]; s != nil {
		s.probeStarted = time.Time{}
	}
}

// Record reports the outcome of a call on key: nil closes the circuit,
// a provider fault counts towards opening it, anything else is ignored.
func (b *Breaker) Record(key string, err error) {
	if err != nil && !providerFault(err) {
		b.Release(key)
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
	s.probeStarted = time.Time{}
	if s.failures >= b.threshold {
		s.openUntil = b.now().Add(b.cooldown)
		s.lastErr = err
	}
}

// providerFault: the error describes the provider (or its credential),
// not the caller. A deadline counts because the router records one only
// when the module's own request_timeout fired: it never records a call
// whose caller context ended, nor one whose deadline the caller lowered
// (Options.Timeout) or the reservation window clamped. ErrLLMPartialOutput
// is listed only defensively: the router records the raw provider error,
// so a provider fault after partial output does count.
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
