package services

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestLease_OnlyOneAcquirerWins(t *testing.T) {
	redis := newFakeLeaseRedis()
	a := NewMaintenanceLease(redis, "auth:maintenance:token-sweep", slog.New(slog.DiscardHandler))
	b := NewMaintenanceLease(redis, "auth:maintenance:token-sweep", slog.New(slog.DiscardHandler))

	okA, err := a.Acquire(context.Background())
	if err != nil || !okA {
		t.Fatalf("first Acquire = %v, %v; want true, nil", okA, err)
	}
	okB, err := b.Acquire(context.Background())
	if err != nil {
		t.Fatalf("second Acquire error: %v", err)
	}
	if okB {
		t.Fatal("two replicas both hold the scheduler lease — 5000 would become a per-replica multiplier")
	}
}

func TestLease_NonOwnerCannotRenewOrRelease(t *testing.T) {
	redis := newFakeLeaseRedis()
	owner := NewMaintenanceLease(redis, "k", slog.New(slog.DiscardHandler))
	other := NewMaintenanceLease(redis, "k", slog.New(slog.DiscardHandler))
	if ok, _ := owner.Acquire(context.Background()); !ok {
		t.Fatal("owner failed to acquire")
	}

	if ok, _ := other.Renew(context.Background()); ok {
		t.Error("a non-owner renewed another replica's lease")
	}
	if err := other.Release(context.Background()); err != nil {
		t.Errorf("a non-owner Release must be a silent no-op, got %v", err)
	}
	if ok, _ := owner.Renew(context.Background()); !ok {
		t.Error("the owner's lease was released by a non-owner")
	}
}

func TestLease_ReleaseAllowsFailover(t *testing.T) {
	redis := newFakeLeaseRedis()
	first := NewMaintenanceLease(redis, "k", slog.New(slog.DiscardHandler))
	second := NewMaintenanceLease(redis, "k", slog.New(slog.DiscardHandler))
	_, _ = first.Acquire(context.Background())
	if err := first.Release(context.Background()); err != nil {
		t.Fatalf("Release: %v", err)
	}
	if ok, _ := second.Acquire(context.Background()); !ok {
		t.Fatal("a released lease must be immediately acquirable so a rolling restart does not stall maintenance")
	}
}

func TestLease_ExpiryAllowsFailover(t *testing.T) {
	redis := newFakeLeaseRedis()
	first := NewMaintenanceLease(redis, "k", slog.New(slog.DiscardHandler))
	second := NewMaintenanceLease(redis, "k", slog.New(slog.DiscardHandler))
	_, _ = first.Acquire(context.Background())
	redis.expireAll() // the leader died without releasing
	if ok, _ := second.Acquire(context.Background()); !ok {
		t.Fatal("a lost leader must fail over once the lease expires")
	}
}

// TestLease_StaleFormerOwnerCannotActOnCurrentLease is the scenario the
// Lua CAS scripts exist for: a replica that lost the lease still holds
// its own now-stale token in memory. Without a server-side compare, its
// Renew would resurrect a lease nobody scheduled it to hold, and its
// Release would silently delete the CURRENT leader's lease out from
// under it. Neither call may go through a local "do I have a cached
// token" guard alone — first genuinely dispatches to Eval here, and the
// fake's compare-then-act logic is what must reject it.
func TestLease_StaleFormerOwnerCannotActOnCurrentLease(t *testing.T) {
	redis := newFakeLeaseRedis()
	first := NewMaintenanceLease(redis, "k", slog.New(slog.DiscardHandler))
	second := NewMaintenanceLease(redis, "k", slog.New(slog.DiscardHandler))
	if ok, _ := first.Acquire(context.Background()); !ok {
		t.Fatal("first failed to acquire")
	}
	redis.expireAll() // the leader died without releasing
	if ok, _ := second.Acquire(context.Background()); !ok {
		t.Fatal("second failed to acquire the now-expired lease")
	}

	// first.token is still the old, stale value — a genuine former owner
	// whose lease was taken over, not an empty-token replica that never
	// held it. Both calls below must be rejected by the server-side CAS,
	// not by a local "token == """ short-circuit.
	if ok, _ := first.Renew(context.Background()); ok {
		t.Fatal("a stale former owner renewed the current leader's lease — the Lua CAS comparison did not run")
	}
	if err := first.Release(context.Background()); err != nil {
		t.Fatalf("Release: %v", err)
	}
	if ok, _ := second.Renew(context.Background()); !ok {
		t.Fatal("the stale former owner's failed release evicted the current leader's lease")
	}
}

func TestLease_RedisErrorIsNotLeadership(t *testing.T) {
	redis := newFakeLeaseRedis()
	redis.failEverything()
	l := NewMaintenanceLease(redis, "k", slog.New(slog.DiscardHandler))
	ok, err := l.Acquire(context.Background())
	if ok {
		t.Fatal("a Redis failure must never be read as having won the lease")
	}
	if err == nil {
		t.Error("the caller needs the error to log a bounded warning and skip maintenance")
	}
}

// fakeLeaseRedis is a mutex-guarded, in-memory LeaseRedisClient double.
// Its Eval interprets the two lease scripts by matching on script text
// and applying the same compare-then-act semantics Redis would — an
// Eval that unconditionally reports success would let every ownership
// test above pass without the implementation ever checking ownership.
type fakeLeaseRedis struct {
	mu      sync.Mutex
	values  map[string]string
	failing bool
}

func newFakeLeaseRedis() *fakeLeaseRedis {
	return &fakeLeaseRedis{values: make(map[string]string)}
}

// expireAll drops every key, standing in for every lease's TTL lapsing —
// the fake has no real clock, so a dead leader is simulated by removing
// its row directly rather than waiting out a TTL.
func (f *fakeLeaseRedis) expireAll() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.values = make(map[string]string)
}

// failEverything makes every subsequent call return an error, standing
// in for a Redis outage.
func (f *fakeLeaseRedis) failEverything() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.failing = true
}

var errFakeLeaseRedisDown = errors.New("fakeLeaseRedis: simulated Redis outage")

// SetNX is the primitive Acquire needs: it must create the key only when
// absent, atomically, so two concurrent callers cannot both "win".
func (f *fakeLeaseRedis) SetNX(_ context.Context, key string, value interface{}, _ time.Duration) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.failing {
		return false, errFakeLeaseRedisDown
	}
	if _, exists := f.values[key]; exists {
		return false, nil
	}
	f.values[key] = fmt.Sprintf("%v", value)
	return true, nil
}

// Eval interprets the renew and release scripts by matching on their
// literal text (they are the only two scripts the lease ever sends) and
// then applies the identical compare-then-act logic Redis's Lua engine
// would run atomically: only proceed when the stored value equals the
// caller-supplied owner token.
//
// "guarded" is deliberately derived from the script text itself, not
// hardcoded true — this is what makes the fake sensitive to a real
// production bug (someone strips the "GET == ARGV[1]" comparison out of
// leaseRenewScript/leaseReleaseScript so the script starts acting
// unconditionally). An earlier version of this fake always enforced
// ownership in Go regardless of what the script argument actually said,
// which meant it could never fail a test that mutated the script text —
// a broken production CAS and a correct one produced identical fake
// behaviour. Matching only on "PEXPIRE"/"DEL" tells the fake WHICH
// operation this is; checking for the comparison substring tells it
// WHETHER that operation is guarded, exactly as a real Lua interpreter
// would behave for either script.
func (f *fakeLeaseRedis) Eval(_ context.Context, script string, keys []string, args ...interface{}) (interface{}, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.failing {
		return nil, errFakeLeaseRedisDown
	}
	if len(keys) != 1 {
		return nil, fmt.Errorf("fakeLeaseRedis: Eval expects exactly one key, got %d", len(keys))
	}
	if len(args) < 1 {
		return nil, errors.New("fakeLeaseRedis: Eval expects an owner token argument")
	}
	key := keys[0]
	token := fmt.Sprintf("%v", args[0])
	current, exists := f.values[key]
	owns := exists && current == token
	guarded := strings.Contains(script, "== ARGV[1]")

	switch {
	case strings.Contains(script, "PEXPIRE"):
		if guarded && !owns {
			return int64(0), nil
		}
		// Renewal keeps the row alive; the fake has no TTL clock beyond
		// expireAll(), so "renewed" is simply "still present".
		return int64(1), nil
	case strings.Contains(script, "DEL"):
		if guarded && !owns {
			return int64(0), nil
		}
		delete(f.values, key)
		return int64(1), nil
	default:
		return nil, fmt.Errorf("fakeLeaseRedis: unrecognized script: %s", script)
	}
}

// The remaining methods satisfy services.RedisClient (embedded in
// LeaseRedisClient) but are not exercised by the lease itself.

func (f *fakeLeaseRedis) Get(_ context.Context, key string) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.failing {
		return "", errFakeLeaseRedisDown
	}
	return f.values[key], nil
}

func (f *fakeLeaseRedis) Set(_ context.Context, key string, value interface{}, _ time.Duration) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.failing {
		return errFakeLeaseRedisDown
	}
	f.values[key] = fmt.Sprintf("%v", value)
	return nil
}

func (f *fakeLeaseRedis) Del(_ context.Context, keys ...string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.failing {
		return errFakeLeaseRedisDown
	}
	for _, k := range keys {
		delete(f.values, k)
	}
	return nil
}

func (f *fakeLeaseRedis) Keys(_ context.Context, _ string) ([]string, error) {
	return nil, errors.New("fakeLeaseRedis: Keys not implemented")
}

func (f *fakeLeaseRedis) Incr(_ context.Context, _ string) (int64, error) {
	return 0, errors.New("fakeLeaseRedis: Incr not implemented")
}

func (f *fakeLeaseRedis) Expire(_ context.Context, _ string, _ time.Duration) error {
	return errors.New("fakeLeaseRedis: Expire not implemented")
}
