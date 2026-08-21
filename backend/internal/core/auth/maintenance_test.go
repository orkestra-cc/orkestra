package auth

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/orkestra/backend/internal/core/auth/repository"
	"github.com/orkestra/backend/internal/core/auth/services"
)

// --- fakes ---

// fakeSweepRepo implements only the two sweep methods; everything else
// panics, because a sweep that called anything else would be a bug worth
// failing loudly on.
type fakeSweepRepo struct {
	repository.RefreshTokenRepository // embedded nil: any other call panics

	mu           sync.Mutex
	expired      int64
	countCalls   int
	sweepCalls   int
	sweepErr     error
	deletedSoFar int64

	// swept receives one value per CleanupExpiredTokens call so a
	// loop-driven test can wait for a real pass instead of sleeping.
	// The send is non-blocking: a full (or nil) channel is skipped so
	// the fake never stalls the loop it is observing.
	swept chan struct{}
}

func (r *fakeSweepRepo) CleanupExpiredTokens(_ context.Context, limit int) (int64, bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.sweepCalls++
	select {
	case r.swept <- struct{}{}:
	default:
	}
	if r.sweepErr != nil {
		return 0, false, r.sweepErr
	}
	n := int64(limit)
	if r.expired < n {
		n = r.expired
	}
	r.expired -= n
	r.deletedSoFar += n
	return n, r.expired > 0, nil
}

func (r *fakeSweepRepo) CountExpiredTokens(context.Context) (int64, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.countCalls++
	return r.expired, nil
}

func (r *fakeSweepRepo) counts() (int64, int) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.deletedSoFar, r.countCalls
}

// sweeps reports how many batches were attempted — including failed and
// empty ones, which the deleted total cannot distinguish from "never
// called". A follower test that only checked the deleted count would
// pass against a loop that swept an empty collection unelected.
func (r *fakeSweepRepo) sweeps() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.sweepCalls
}

func newSweepModule(t *testing.T, operatorExpired, clientExpired int64) (*AuthModule, *fakeSweepRepo, *fakeSweepRepo) {
	t.Helper()
	op := &fakeSweepRepo{expired: operatorExpired, swept: make(chan struct{}, 16)}
	cl := &fakeSweepRepo{expired: clientExpired, swept: make(chan struct{}, 16)}
	m := &AuthModule{logger: slog.New(slog.DiscardHandler)}
	m.sweepTiers = []sweepTier{
		{name: "operator", repo: op},
		{name: "client", repo: cl},
	}
	return m, op, cl
}

// --- the adaptive mechanism ---

// A fixed 6-hour interval cannot drain: 5,000 rows per tier every six
// hours is 20,000 a day, so a one-million-row backlog takes fifty days.
// The cadence — not the batch — is what adapts.
func TestNextSweepInterval_AdaptsToBacklog(t *testing.T) {
	if got := nextSweepInterval(true); got != sweepDrainInterval {
		t.Errorf("with a backlog = %v, want %v", got, sweepDrainInterval)
	}
	if got := nextSweepInterval(false); got != sweepIdleInterval {
		t.Errorf("drained = %v, want %v", got, sweepIdleInterval)
	}
	if sweepDrainInterval >= sweepIdleInterval {
		t.Fatal("the drain cadence must be faster than the idle one, or the loop never catches up")
	}
}

func TestSweep_OneBatchPerTierPerCycle(t *testing.T) {
	m, op, cl := newSweepModule(t, 3*int64(repository.SweepBatchLimit), 10)
	ctx := context.Background()

	hasMore := m.sweepOneTier(ctx, &m.sweepTiers[0])
	if !hasMore {
		t.Error("operator tier reported no backlog with 3 batches queued")
	}
	m.sweepOneTier(ctx, &m.sweepTiers[1])

	if deleted, _ := op.counts(); deleted != int64(repository.SweepBatchLimit) {
		t.Errorf("operator deleted %d in one cycle, want exactly the batch bound %d", deleted, repository.SweepBatchLimit)
	}
	if deleted, _ := cl.counts(); deleted != 10 {
		t.Errorf("client deleted %d, want 10", deleted)
	}
}

func TestSweep_BacklogCountedOnceOnEntryToDrain(t *testing.T) {
	m, op, _ := newSweepModule(t, 3*int64(repository.SweepBatchLimit), 0)
	ctx := context.Background()
	tier := &m.sweepTiers[0]

	for m.sweepOneTier(ctx, tier) {
	}

	if _, calls := op.counts(); calls != 1 {
		t.Errorf("CountExpiredTokens called %d times, want 1 — at the five-minute cadence a per-cycle count would scan the whole eligible range 288 times a day", calls)
	}
	if tier.backlog != 0 {
		t.Errorf("backlog estimate = %d after draining, want 0 — operators watch this reach zero to see the drain finish", tier.backlog)
	}
	if tier.backlogCounted {
		t.Error("backlogCounted must reset so a later drain recounts")
	}
}

func TestSweep_RepositoryErrorReportsNoBacklogAndDoesNotPanic(t *testing.T) {
	m, op, _ := newSweepModule(t, 100, 0)
	op.sweepErr = errors.New("mongo unavailable")
	if m.sweepOneTier(context.Background(), &m.sweepTiers[0]) {
		t.Error("a failed batch must not claim a backlog and re-arm the drain cadence")
	}
}

// --- leadership ---

// TestSweep_NotTheLeaderDoesNothing drives the real loop against a lease
// another replica already holds. Asserting only that Acquire returns
// false would be vacuous: the guard that matters is in the loop, and a
// test that never runs the loop passes just as happily when the guard is
// deleted.
func TestSweep_NotTheLeaderDoesNothing(t *testing.T) {
	m, op, cl := newSweepModule(t, 100, 100)
	redis := newFakeLeaseRedis()
	incumbent := services.NewMaintenanceLease(redis, tokenSweepLeaseKey, slog.New(slog.DiscardHandler))
	if ok, _ := incumbent.Acquire(context.Background()); !ok {
		t.Fatal("incumbent failed to acquire")
	}
	<-redis.attempted // the incumbent's own SetNX; the wait below must observe the follower's
	m.sweepLease = services.NewMaintenanceLease(redis, tokenSweepLeaseKey, slog.New(slog.DiscardHandler))

	acquired, err := m.sweepLease.Acquire(context.Background())
	if err != nil || acquired {
		t.Fatalf("follower Acquire = (%v, %v), want (false, nil)", acquired, err)
	}
	<-redis.attempted // and its own probe above

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan struct{})
	go m.tokenSweepLoop(ctx, done, time.Millisecond, time.Hour)

	select {
	case <-redis.attempted:
		// The follower woke, tried, and was refused. A loop missing the
		// !acquired guard sweeps both tiers in this same iteration
		// before it can reach another select, so cancelling now still
		// catches it.
	case <-time.After(10 * time.Second):
		t.Fatal("the loop never attempted to acquire the lease")
	}
	cancel()
	<-done

	if op.sweeps() != 0 || cl.sweeps() != 0 {
		t.Errorf("a follower swept (operator %d batches, client %d); 5000 must be a cluster-wide bound, not a per-replica multiplier",
			op.sweeps(), cl.sweeps())
	}
	if deleted, _ := op.counts(); deleted != 0 {
		t.Errorf("a follower deleted %d operator rows", deleted)
	}
}

func TestSweep_RedisUnavailableIsNotLeadership(t *testing.T) {
	redis := newFakeLeaseRedis()
	redis.failEverything()
	lease := services.NewMaintenanceLease(redis, tokenSweepLeaseKey, slog.New(slog.DiscardHandler))
	acquired, err := lease.Acquire(context.Background())
	if acquired {
		t.Fatal("a Redis failure was read as having won the lease")
	}
	if err == nil {
		t.Error("the loop needs the error to log a bounded warning and skip maintenance — never authentication")
	}
}

// TestSweep_LosingTheLeaseStepsDownWithoutSweeping is the failover half
// of the leadership contract. A replica whose lease was taken over must
// stop sweeping and stop renewing — but it must NOT exit, because
// nothing calls Start again and a "not_owner" renewal is exactly what a
// Redis that restarted empty reports to the only replica there is.
// Demotion returns it to the follower state it was in at boot, which is
// the same steady state every other failure path lands in.
//
// The assertions are on renewals, not on a second sweep: after stepping
// down the next pass is a retry interval away, so "did not sweep again"
// alone would pass whether or not the guard exists. A leader keeps
// renewing every tick; a follower stops at `if !leader`. That is the
// difference this test can actually see.
func TestSweep_LosingTheLeaseStepsDownWithoutSweeping(t *testing.T) {
	m, op, _ := newSweepModule(t, 0, 0)
	redis := newFakeLeaseRedis()
	m.sweepLease = services.NewMaintenanceLease(redis, tokenSweepLeaseKey, slog.New(slog.DiscardHandler))

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan struct{})
	go m.tokenSweepLoop(ctx, done, time.Millisecond, 5*time.Millisecond)

	// Only proceed once this replica is genuinely the leader and has run
	// a pass — otherwise the takeover below would race the acquisition
	// and the test would prove nothing.
	select {
	case <-op.swept:
	case <-time.After(10 * time.Second):
		t.Fatal("the loop never took leadership and swept")
	}

	// The lease lapses and a second replica takes it: our next renew
	// finds a token that is no longer ours.
	redis.expireAll()
	successor := services.NewMaintenanceLease(redis, tokenSweepLeaseKey, slog.New(slog.DiscardHandler))
	if ok, _ := successor.Acquire(context.Background()); !ok {
		t.Fatal("successor failed to acquire the lapsed lease")
	}

	// ~20 renew ticks: long enough for the loss to be observed and the
	// replica to step down.
	time.Sleep(100 * time.Millisecond)
	renewsAtStepDown := redis.evals()
	time.Sleep(100 * time.Millisecond)
	if got := redis.evals(); got != renewsAtStepDown {
		t.Errorf("still renewing %d ticks after the takeover (%d -> %d): the replica never stepped down and is sweeping alongside the new leader",
			got-renewsAtStepDown, renewsAtStepDown, got)
	}
	if op.sweeps() != 1 {
		t.Errorf("swept %d times, want 1 — a demoted replica must re-acquire before it sweeps again", op.sweeps())
	}
	select {
	case <-done:
		t.Fatal("the loop exited on a lost election — nothing calls Start again, so this replica could never take over when the successor dies")
	default:
	}

	cancel()
	<-done
}

// TestSweep_RenewTicksDoNotStarveTheSweep pins the scheduling shape: the
// next pass is an absolute deadline, not a duration re-derived every
// time the loop wakes. Renewal fires every 30s in production against
// waits of 5m and 6h, so a loop that restarted its wait on each renew
// tick would never sweep at all — it would look perfectly healthy while
// deleting nothing.
func TestSweep_RenewTicksDoNotStarveTheSweep(t *testing.T) {
	m, op, _ := newSweepModule(t, 0, 0)
	redis := newFakeLeaseRedis()
	m.sweepLease = services.NewMaintenanceLease(redis, tokenSweepLeaseKey, slog.New(slog.DiscardHandler))

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan struct{})
	// The renew tick is 30x more frequent than the pass is due — the
	// same ratio as 30s renewal against a 5m drain wait.
	go m.tokenSweepLoop(ctx, done, 150*time.Millisecond, 5*time.Millisecond)

	select {
	case <-op.swept:
	case <-time.After(10 * time.Second):
		t.Fatal("no sweep ran: renew ticks reset the pass deadline, so the sweep is starved for as long as the loop lives")
	}

	// The tiers were empty, so this pass reported no backlog and the loop
	// is now in the six-hour idle wait — still holding the lease. Giving
	// it up between passes would let every follower's own retry timer win
	// an election against an idle cluster.
	other := services.NewMaintenanceLease(redis, tokenSweepLeaseKey, slog.New(slog.DiscardHandler))
	if ok, _ := other.Acquire(context.Background()); ok {
		t.Fatal("the lease was released for the idle wait — a follower would take over and re-enter the drain cadence against a drained cluster")
	}

	cancel()
	<-done
}

// TestSweep_DrainingHonoursTheDrainCadence pins the one line that keeps
// 5,000 rows per tier per cycle a bound at all: the reschedule after a
// pass. Delete it and the deadline never advances, so the loop re-enters
// CleanupExpiredTokens at whatever rate Mongo will answer — unbounded
// deletion pressure that no other assertion in this file notices.
func TestSweep_DrainingHonoursTheDrainCadence(t *testing.T) {
	m, op, _ := newSweepModule(t, 2*int64(repository.SweepBatchLimit), 0)
	redis := newFakeLeaseRedis()
	m.sweepLease = services.NewMaintenanceLease(redis, tokenSweepLeaseKey, slog.New(slog.DiscardHandler))

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan struct{})
	go m.tokenSweepLoop(ctx, done, time.Millisecond, 5*time.Millisecond)

	select {
	case <-op.swept:
	case <-time.After(10 * time.Second):
		t.Fatal("the loop never took leadership and swept")
	}

	// That pass reported hasMore, so the next one is a drain interval —
	// five minutes — away. Nothing may re-enter the batch before then.
	select {
	case <-op.swept:
		t.Fatal("swept again immediately — the pass deadline was not advanced; 5000/cycle stops being a bound and the loop spins CleanupExpiredTokens at full Mongo throughput")
	case <-time.After(100 * time.Millisecond):
	}

	// And the leader keeps the lease across that wait, so a second
	// replica cannot start a parallel drain.
	other := services.NewMaintenanceLease(redis, tokenSweepLeaseKey, slog.New(slog.DiscardHandler))
	if ok, _ := other.Acquire(context.Background()); ok {
		t.Fatal("a second replica acquired the lease between passes — 5000/tier/cycle would become a per-replica multiplier")
	}

	cancel()
	<-done
}

// TestSweep_TransientRedisErrorDoesNotEndRetention covers the difference
// between the two ways a renewal fails. Nothing calls Start again for the
// life of the process, so exiting the loop on a Redis error would end
// refresh-token retention until the next restart — one failover on a
// 30-second renew tick against a backlog that needs days of uninterrupted
// draining, with one Warn line to show for it.
func TestSweep_TransientRedisErrorDoesNotEndRetention(t *testing.T) {
	m, op, _ := newSweepModule(t, 0, 0)
	redis := newFakeLeaseRedis()
	m.sweepLease = services.NewMaintenanceLease(redis, tokenSweepLeaseKey, slog.New(slog.DiscardHandler))

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan struct{})
	go m.tokenSweepLoop(ctx, done, time.Millisecond, 5*time.Millisecond)

	select {
	case <-op.swept:
	case <-time.After(10 * time.Second):
		t.Fatal("the loop never took leadership and swept")
	}

	// A restart, failover or reset lands on a renew tick.
	redis.failEverything()

	select {
	case <-done:
		t.Fatal("a transient Redis error ended the sweep for the rest of the process's life — it must step down and re-contend, not exit")
	case <-time.After(200 * time.Millisecond):
		// ~40 renew ticks later the loop is still alive, holding no
		// leadership and sweeping nothing until it can re-acquire.
	}

	if op.sweeps() != 1 {
		t.Errorf("swept %d times, want 1 — a replica that stepped down must not sweep again before re-acquiring", op.sweeps())
	}

	cancel()
	<-done
}

// --- lifecycle ---

func TestModuleLifecycle_StartIdempotentStopExitsRestartable(t *testing.T) {
	m, _, _ := newSweepModule(t, 0, 0)
	m.sweepLease = services.NewMaintenanceLease(newFakeLeaseRedis(), tokenSweepLeaseKey, slog.New(slog.DiscardHandler))
	ctx := context.Background()

	if err := m.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	firstDone := m.sweepDone
	if err := m.Start(ctx); err != nil {
		t.Fatalf("second Start must be a no-op, got %v", err)
	}
	if m.sweepDone != firstDone {
		t.Fatal("a second Start created a second loop — a hot enable/disable cycle would leave two tickers")
	}

	stopCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err := m.Stop(stopCtx); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	select {
	case <-firstDone:
	default:
		t.Fatal("Stop returned before the loop closed done")
	}

	if err := m.Start(ctx); err != nil {
		t.Fatalf("a stopped module must start again: %v", err)
	}
	if m.sweepDone == firstDone {
		t.Fatal("restart reused the closed done channel")
	}
	_ = m.Stop(stopCtx)
}

func TestModuleLifecycle_NoLeaseMeansNoLoop(t *testing.T) {
	m, _, _ := newSweepModule(t, 0, 0)
	m.sweepLease = nil // Redis adapter did not satisfy LeaseRedisClient at Init
	if err := m.Start(context.Background()); err != nil {
		t.Fatalf("Start without a lease must be a silent no-op, got %v", err)
	}
	if m.sweepDone != nil {
		t.Error("a loop started without leadership; every replica would sweep unelected")
	}
}

func TestModuleLifecycle_StopWithoutStartIsNoop(t *testing.T) {
	m, _, _ := newSweepModule(t, 0, 0)
	if err := m.Stop(context.Background()); err != nil {
		t.Errorf("Stop before Start = %v, want nil", err)
	}
}

// fakeLeaseRedis is a mutex-guarded, in-memory services.LeaseRedisClient
// double, duplicated from the services package's own lease tests: a test
// file is not importable across packages, and a ~40-line fake is cheaper
// than putting a public test seam on production code. It carries one
// addition the services copy does not need — `attempted`, which lets a
// loop-driven test wait for a real acquisition attempt rather than sleep.
//
// Its Eval interprets the two lease scripts by matching on script text
// and applying the same compare-then-act semantics Redis would — an Eval
// that unconditionally reported success would let every ownership test
// pass without the implementation ever checking ownership.
type fakeLeaseRedis struct {
	mu        sync.Mutex
	values    map[string]string
	failing   bool
	evalCalls int

	// attempted receives one value per SetNX call. Buffered and sent to
	// non-blockingly, so it never stalls the loop under observation.
	attempted chan struct{}
}

func newFakeLeaseRedis() *fakeLeaseRedis {
	return &fakeLeaseRedis{values: make(map[string]string), attempted: make(chan struct{}, 16)}
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

// evals counts renew/release round-trips. A leader renews on every tick;
// a follower does not, so the count is how a test tells the two apart
// without a clock.
func (f *fakeLeaseRedis) evals() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.evalCalls
}

var errFakeLeaseRedisDown = errors.New("fakeLeaseRedis: simulated Redis outage")

// SetNX is the primitive Acquire needs: it must create the key only when
// absent, atomically, so two concurrent callers cannot both "win".
func (f *fakeLeaseRedis) SetNX(_ context.Context, key string, value interface{}, _ time.Duration) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	select {
	case f.attempted <- struct{}{}:
	default:
	}
	if f.failing {
		return false, errFakeLeaseRedisDown
	}
	if _, exists := f.values[key]; exists {
		return false, nil
	}
	f.values[key] = fmt.Sprintf("%v", value)
	return true, nil
}

// Eval applies the identical compare-then-act logic Redis's Lua engine
// would run atomically: only proceed when the stored value equals the
// caller-supplied owner token. "guarded" is derived from the script text
// itself rather than hardcoded, so stripping the comparison out of the
// production script makes this fake behave like the broken Redis it is
// standing in for.
func (f *fakeLeaseRedis) Eval(_ context.Context, script string, keys []string, args ...interface{}) (interface{}, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.evalCalls++
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
