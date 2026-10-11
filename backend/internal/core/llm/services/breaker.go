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
// Allow hands the probe a ticket; only that ticket frees or settles the
// slot, so a call admitted before the circuit opened can never clear a
// newer probe and let a second one through.
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
	seq    uint64 // last probe ticket handed out
}

type breakerState struct {
	failures     int
	openUntil    time.Time
	lastErr      error
	probeStarted time.Time // zero = no probe in flight
	probe        uint64    // the in-flight probe's ticket
}

func NewBreaker(threshold int, cooldown time.Duration) *Breaker {
	if threshold < 1 {
		threshold = 1
	}
	return &Breaker{threshold: threshold, cooldown: cooldown, now: time.Now, states: map[string]*breakerState{}}
}

// Allow returns a nil error when a call on key may proceed, or an
// open-circuit error (see openErr). The ticket is non-zero only for the
// half-open probe; every call passes its ticket back to Record or
// Release, and a zero ticket never touches the probe slot.
func (b *Breaker) Allow(key string) (ticket uint64, err error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	s := b.states[key]
	if s == nil || s.failures < b.threshold {
		return 0, nil
	}
	now := b.now()
	if now.Before(s.openUntil) {
		return 0, openErr(s.lastErr)
	}
	if !s.probeStarted.IsZero() && now.Before(s.probeStarted.Add(b.cooldown)) {
		return 0, openErr(s.lastErr) // another caller is probing
	}
	b.seq++
	s.probeStarted, s.probe = now, b.seq
	return s.probe, nil
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
// caller left, or its own deadline fired) without counting anything. It
// frees the half-open probe slot only when ticket is that probe's.
func (b *Breaker) Release(key string, ticket uint64) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.freeProbe(key, ticket)
}

func (b *Breaker) freeProbe(key string, ticket uint64) {
	if s := b.states[key]; s != nil && ticket != 0 && s.probe == ticket {
		s.probeStarted, s.probe = time.Time{}, 0
	}
}

// Record reports the outcome of a call on key: nil closes the circuit,
// a provider fault counts towards opening it, anything else is ignored.
// ticket is the one Allow returned (see Release); once the circuit is
// open only the current probe's ticket is heard.
func (b *Breaker) Record(key string, ticket uint64, err error) {
	if err != nil && !providerFault(err) {
		b.Release(key, ticket)
		return
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	s := b.states[key]
	if s != nil && s.failures >= b.threshold && (ticket == 0 || ticket != s.probe) {
		// The circuit is open or half-open: only the current probe's
		// verdict settles it. A call admitted before it opened (ticket 0)
		// or an expired probe reports on a provider state that is stale.
		return
	}
	if err == nil {
		delete(b.states, key) // keeps the map bounded by the failing identities
		return
	}
	if s == nil {
		s = &breakerState{}
		b.states[key] = s
	}
	s.failures++
	b.freeProbe(key, ticket)
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
