package services

// These tests run the REAL Lua script: miniredis ships a Lua engine, so
// the atomicity, the PTTL read and the orphan healing are exercised
// rather than asserted. A pure fake would prove nothing about the one
// property that matters — that count, TTL and healing are a single step.

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"

	"github.com/orkestra/backend/internal/shared/database"
)

func newTestCounter(t *testing.T) (AttemptCounter, *miniredis.Miniredis) {
	t.Helper()
	mr := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = client.Close() })
	return NewRedisAttemptCounter(database.NewRedisClientAdapter(client), slog.Default()), mr
}

var testLimit = Limit{Threshold: 3, Window: 15 * time.Minute}

func TestAttemptCounter_LocksAtThresholdInsideWindow(t *testing.T) {
	c, _ := newTestCounter(t)
	ctx := context.Background()
	key := "auth:attempts:email:operator:a@example.com"

	for i := 1; i <= 2; i++ {
		v, err := c.RecordFailure(ctx, key, testLimit)
		if err != nil {
			t.Fatalf("RecordFailure %d: %v", i, err)
		}
		if v.Locked {
			t.Fatalf("locked at attempt %d, threshold is %d", i, testLimit.Threshold)
		}
	}

	v, err := c.RecordFailure(ctx, key, testLimit)
	if err != nil {
		t.Fatalf("RecordFailure 3: %v", err)
	}
	if !v.Locked || v.Count != 3 {
		t.Fatalf("Verdict = %+v, want Locked with Count 3", v)
	}
	if v.RetryAfter <= 0 || v.RetryAfter > testLimit.Window {
		t.Fatalf("RetryAfter = %v, want (0, %v]", v.RetryAfter, testLimit.Window)
	}
}

// A peek must never move the counter. IsBlocked's defect was exactly
// this: pre-checking cost a token, so a caller could trip its own
// lockout purely by asking.
func TestAttemptCounter_LockedNeverIncrements(t *testing.T) {
	c, _ := newTestCounter(t)
	ctx := context.Background()
	key := "auth:attempts:ip:203.0.113.7"

	for i := 0; i < 50; i++ {
		v, err := c.Locked(ctx, key, testLimit)
		if err != nil {
			t.Fatalf("Locked: %v", err)
		}
		if v.Locked || v.Count != 0 {
			t.Fatalf("peek %d moved the counter: %+v", i, v)
		}
	}

	if _, err := c.RecordFailure(ctx, key, testLimit); err != nil {
		t.Fatalf("RecordFailure: %v", err)
	}
	v, err := c.Locked(ctx, key, testLimit)
	if err != nil {
		t.Fatalf("Locked after record: %v", err)
	}
	if v.Count != 1 {
		t.Fatalf("Count = %d after 50 peeks + 1 record, want 1", v.Count)
	}
}

func TestAttemptCounter_WindowExpiryUnlocks(t *testing.T) {
	c, mr := newTestCounter(t)
	ctx := context.Background()
	key := "auth:attempts:email:operator:b@example.com"
	lim := Limit{Threshold: 2, Window: time.Minute}

	_, _ = c.RecordFailure(ctx, key, lim)
	v, _ := c.RecordFailure(ctx, key, lim)
	if !v.Locked {
		t.Fatal("want locked at threshold")
	}

	mr.FastForward(61 * time.Second)

	v, err := c.Locked(ctx, key, lim)
	if err != nil {
		t.Fatalf("Locked after expiry: %v", err)
	}
	if v.Locked || v.Count != 0 {
		t.Fatalf("Verdict = %+v after window expiry, want a clean slate", v)
	}
}

// Edge case 3: a threshold lowered while a window is open must lock
// immediately, and a raised one must unlock — because the comparison
// happens against the LIVE limit, not against a capacity frozen into a
// bucket at allocation time (the old getBucket defect).
func TestAttemptCounter_ThresholdChangeAppliesToOpenWindow(t *testing.T) {
	c, _ := newTestCounter(t)
	ctx := context.Background()
	key := "auth:attempts:email:operator:c@example.com"

	for i := 0; i < 3; i++ {
		_, _ = c.RecordFailure(ctx, key, Limit{Threshold: 10, Window: 15 * time.Minute})
	}

	tight, err := c.Locked(ctx, key, Limit{Threshold: 3, Window: 15 * time.Minute})
	if err != nil {
		t.Fatalf("Locked tight: %v", err)
	}
	if !tight.Locked {
		t.Fatal("lowering the threshold to 3 with a count of 3 must lock")
	}

	loose, err := c.Locked(ctx, key, Limit{Threshold: 99, Window: 15 * time.Minute})
	if err != nil {
		t.Fatalf("Locked loose: %v", err)
	}
	if loose.Locked {
		t.Fatal("raising the threshold above the count must unlock")
	}
}

// N concurrent failures must cost N. The RMW shape this replaces let N
// parallel guesses cost one.
func TestAttemptCounter_ConcurrentRecordFailureCountsAll(t *testing.T) {
	c, _ := newTestCounter(t)
	ctx := context.Background()
	key := "auth:attempts:ip:198.51.100.4"
	const n = 32

	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _ = c.RecordFailure(ctx, key, Limit{Threshold: 1000, Window: time.Minute})
		}()
	}
	wg.Wait()

	v, err := c.Locked(ctx, key, Limit{Threshold: 1000, Window: time.Minute})
	if err != nil {
		t.Fatalf("Locked: %v", err)
	}
	if v.Count != n {
		t.Fatalf("Count = %d, want %d — concurrent increments were lost", v.Count, n)
	}
}

// A counter key that exists with NO TTL is a permanent lockout: the
// INCR-then-EXPIRE shape leaves exactly this on a crash between the two
// commands. Both the peek and the increment must heal it.
func TestAttemptCounter_HealsOrphanKeyOnPeek(t *testing.T) {
	c, mr := newTestCounter(t)
	ctx := context.Background()
	key := "auth:attempts:email:operator:orphan@example.com"

	mr.Set(key, "9") // no TTL — the orphan
	if ttl := mr.TTL(key); ttl != 0 {
		t.Fatalf("precondition: TTL = %v, want none", ttl)
	}

	v, err := c.Locked(ctx, key, testLimit)
	if err != nil {
		t.Fatalf("Locked: %v", err)
	}
	if !v.Locked || v.Count != 9 {
		t.Fatalf("Verdict = %+v, want Locked with Count 9", v)
	}
	if mr.TTL(key) <= 0 {
		t.Fatal("the peek must have stamped a TTL — an orphan lockout can never expire")
	}

	mr.FastForward(testLimit.Window + time.Second)
	v, err = c.Locked(ctx, key, testLimit)
	if err != nil {
		t.Fatalf("Locked after healed window: %v", err)
	}
	if v.Locked {
		t.Fatal("a healed orphan must unlock on its own")
	}
}

func TestAttemptCounter_HealsOrphanKeyOnRecord(t *testing.T) {
	c, mr := newTestCounter(t)
	ctx := context.Background()
	key := "auth:attempts:ip:192.0.2.55"

	mr.Set(key, "1")
	if _, err := c.RecordFailure(ctx, key, testLimit); err != nil {
		t.Fatalf("RecordFailure: %v", err)
	}
	if mr.TTL(key) <= 0 {
		t.Fatal("the increment must have stamped a TTL on the orphan")
	}
}

func TestAttemptCounter_RetryAfterTracksPTTL(t *testing.T) {
	c, mr := newTestCounter(t)
	ctx := context.Background()
	key := "auth:attempts:email:operator:d@example.com"
	lim := Limit{Threshold: 1, Window: 10 * time.Minute}

	v, err := c.RecordFailure(ctx, key, lim)
	if err != nil {
		t.Fatalf("RecordFailure: %v", err)
	}
	if v.RetryAfter < 9*time.Minute || v.RetryAfter > 10*time.Minute {
		t.Fatalf("RetryAfter = %v, want ~10m", v.RetryAfter)
	}

	mr.FastForward(8 * time.Minute)
	v, err = c.Locked(ctx, key, lim)
	if err != nil {
		t.Fatalf("Locked: %v", err)
	}
	if v.RetryAfter > 2*time.Minute+time.Second || v.RetryAfter <= 0 {
		t.Fatalf("RetryAfter = %v after 8m of a 10m window, want ~2m", v.RetryAfter)
	}
}

func TestAttemptCounter_ResetClearsTheKey(t *testing.T) {
	c, _ := newTestCounter(t)
	ctx := context.Background()
	key := "auth:attempts:email:operator:e@example.com"

	for i := 0; i < 3; i++ {
		_, _ = c.RecordFailure(ctx, key, testLimit)
	}
	if err := c.Reset(ctx, key); err != nil {
		t.Fatalf("Reset: %v", err)
	}
	v, err := c.Locked(ctx, key, testLimit)
	if err != nil {
		t.Fatalf("Locked: %v", err)
	}
	if v.Count != 0 || v.Locked {
		t.Fatalf("Verdict = %+v after Reset, want a clean slate", v)
	}
}

// A store that cannot answer must say so. Absorbing the failure into
// "not locked" would make D4's fallback to the durable rule impossible
// to express — the caller would never know it had no verdict.
func TestAttemptCounter_StoreErrorIsReturned(t *testing.T) {
	c, mr := newTestCounter(t)
	ctx := context.Background()
	mr.Close() // every command from here on fails

	if _, err := c.Locked(ctx, "auth:attempts:ip:1.2.3.4", testLimit); err == nil {
		t.Error("Locked must return the store error, not absorb it")
	}
	if _, err := c.RecordFailure(ctx, "auth:attempts:ip:1.2.3.4", testLimit); err == nil {
		t.Error("RecordFailure must return the store error")
	}
	if err := c.Reset(ctx, "auth:attempts:ip:1.2.3.4"); err == nil {
		t.Error("Reset must return the store error")
	}
}

func TestAttemptCounter_ZeroThresholdNeverLocks(t *testing.T) {
	c, _ := newTestCounter(t)
	ctx := context.Background()
	key := "auth:attempts:ip:0.0.0.0"

	// A misedited policy that returns 0 must not turn the counter into a
	// deny-all — the same stance SetAuthFailedConfig took by ignoring
	// threshold < 1.
	v, err := c.RecordFailure(ctx, key, Limit{Threshold: 0, Window: time.Minute})
	if err != nil {
		t.Fatalf("RecordFailure: %v", err)
	}
	if v.Locked {
		t.Fatal("a threshold of 0 must be treated as unset, never as deny-all")
	}
}

// ===== key builders =====

func TestAttemptKeys_ShapeAndScoping(t *testing.T) {
	cases := []struct{ got, want string }{
		{AttemptKeyIP("203.0.113.1"), "auth:attempts:ip:203.0.113.1"},
		{AttemptKeyEmail(PolicyAudienceOperator, "A@Example.COM "), "auth:attempts:email:operator:a@example.com"},
		{AttemptKeyEmail(PolicyAudienceClient, "a@example.com"), "auth:attempts:email:client:a@example.com"},
		{AttemptKeyClient("svc-1"), "auth:attempts:client:svc-1"},
		{AttemptKeyResetEmail(PolicyAudienceOperator, "a@example.com"), "auth:attempts:reset-email:operator:a@example.com"},
		{AttemptKeyResetIP("203.0.113.1"), "auth:attempts:reset-ip:203.0.113.1"},
		{AttemptKeyVerifyEmail(PolicyAudienceClient, "a@example.com"), "auth:attempts:verify-email:client:a@example.com"},
		{AttemptKeyVerifyIP("203.0.113.1"), "auth:attempts:verify-ip:203.0.113.1"},
		{AttemptKeyMFAVerify(PolicyAudienceOperator, "u-1"), "auth:attempts:mfa-verify:operator:u-1"},
	}
	for _, c := range cases {
		if c.got != c.want {
			t.Errorf("key = %q, want %q", c.got, c.want)
		}
	}
}

// An empty IP must SKIP the scope, not share one bucket with every
// caller whose address could not be resolved — that shared bucket is
// what let one unresolvable client lock out all of them.
func TestAttemptKeyIP_EmptyYieldsNoKey(t *testing.T) {
	if k := AttemptKeyIP(""); k != "" {
		t.Fatalf("AttemptKeyIP(\"\") = %q, want \"\" so the caller skips the scope", k)
	}
	if k := AttemptKeyResetIP("  "); k != "" {
		t.Fatalf("AttemptKeyResetIP(blank) = %q, want \"\"", k)
	}
}

// ===== memory implementation =====

func TestMemoryAttemptCounter_MatchesRedisSemantics(t *testing.T) {
	c := NewMemoryAttemptCounter()
	ctx := context.Background()
	key := "auth:attempts:email:operator:mem@example.com"

	for i := 1; i <= 2; i++ {
		v, err := c.RecordFailure(ctx, key, testLimit)
		if err != nil {
			t.Fatalf("RecordFailure: %v", err)
		}
		if v.Locked {
			t.Fatalf("locked early at %d", i)
		}
	}
	v, _ := c.RecordFailure(ctx, key, testLimit)
	if !v.Locked || v.Count != 3 {
		t.Fatalf("Verdict = %+v, want Locked/3", v)
	}
	if v.RetryAfter <= 0 {
		t.Fatal("RetryAfter must be positive")
	}

	peek, _ := c.Locked(ctx, key, testLimit)
	if peek.Count != 3 {
		t.Fatalf("peek moved the counter: %+v", peek)
	}
	if err := c.Reset(ctx, key); err != nil {
		t.Fatalf("Reset: %v", err)
	}
	after, _ := c.Locked(ctx, key, testLimit)
	if after.Count != 0 {
		t.Fatalf("Count = %d after Reset, want 0", after.Count)
	}
}

func TestMemoryAttemptCounter_ErrorInjection(t *testing.T) {
	c := NewMemoryAttemptCounter().(*MemoryAttemptCounter)
	sentinel := errors.New("store down")
	c.FailWith(sentinel)

	if _, err := c.Locked(context.Background(), "k", testLimit); !errors.Is(err, sentinel) {
		t.Fatalf("Locked err = %v, want %v", err, sentinel)
	}
	if _, err := c.RecordFailure(context.Background(), "k", testLimit); !errors.Is(err, sentinel) {
		t.Fatalf("RecordFailure err = %v, want %v", err, sentinel)
	}
}
