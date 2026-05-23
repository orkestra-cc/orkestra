package services

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/orkestra-cc/orkestra-sdk/ctxauth"
)

// ctxWithTenant stamps a tenantID onto the background context so
// the lock-key derivation in ScoreService.withPersonLock returns
// the expected scope. Mirrors the subscriptions authedCtx helper.
func ctxWithTenant(tid string) context.Context {
	return context.WithValue(context.Background(), ctxauth.KeyTenantID, tid)
}

// trackMaxConcurrent observes how many goroutines hold a critical
// section simultaneously and returns the peak. The wait-loop on
// CompareAndSwap is a textbook way to atomically push a high-water
// mark without contention.
func trackMaxConcurrent(t *testing.T, current, peak *int32) {
	t.Helper()
	c := atomic.AddInt32(current, 1)
	for {
		max := atomic.LoadInt32(peak)
		if c <= max {
			break
		}
		if atomic.CompareAndSwapInt32(peak, max, c) {
			break
		}
	}
}

// TestWithPersonLockSerializesSamePerson verifies that two
// concurrent recomputes for the same (tenant, person) pair never
// run their critical section simultaneously. The per-(tenant, person)
// mutex is what stops two activity inserts on the same person from
// racing on the snapshot upsert.
func TestWithPersonLockSerializesSamePerson(t *testing.T) {
	s := &ScoreService{}
	ctx := ctxWithTenant("t-1")

	const goroutines = 10
	var current, peak int32
	var counter int32

	var wg sync.WaitGroup
	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = s.withPersonLock(ctx, "p-1", func() error {
				trackMaxConcurrent(t, &current, &peak)
				atomic.AddInt32(&counter, 1)
				// Sleep makes a race visible — without serialisation
				// the test would observe peak > 1 within a few
				// goroutine schedules.
				time.Sleep(2 * time.Millisecond)
				atomic.AddInt32(&current, -1)
				return nil
			})
		}()
	}
	wg.Wait()

	if peak > 1 {
		t.Errorf("withPersonLock should serialise same (tenant, person); peak concurrent = %d", peak)
	}
	if counter != goroutines {
		t.Errorf("counter = %d, want %d (some goroutines failed to acquire)", counter, goroutines)
	}
}

// TestWithPersonLockAllowsDifferentPersons — different persons of
// the same tenant must be able to recompute in parallel. Otherwise
// a busy tenant would serialise every snapshot upsert through one
// global mutex.
func TestWithPersonLockAllowsDifferentPersons(t *testing.T) {
	s := &ScoreService{}
	ctx := ctxWithTenant("t-1")

	const goroutines = 8
	var current, peak int32

	var wg sync.WaitGroup
	for i := 0; i < goroutines; i++ {
		person := fmt.Sprintf("p-%d", i)
		wg.Add(1)
		go func(p string) {
			defer wg.Done()
			_ = s.withPersonLock(ctx, p, func() error {
				trackMaxConcurrent(t, &current, &peak)
				time.Sleep(20 * time.Millisecond)
				atomic.AddInt32(&current, -1)
				return nil
			})
		}(person)
	}
	wg.Wait()

	if peak < 2 {
		t.Errorf("withPersonLock should allow different persons to run concurrently; peak = %d (expected ≥2)", peak)
	}
}

// TestWithPersonLockSeparatesTenants — same personUUID under two
// different tenant contexts must NOT share a lock. Otherwise a
// busy tenant A would stall tenant B's recomputes.
func TestWithPersonLockSeparatesTenants(t *testing.T) {
	s := &ScoreService{}
	const goroutines = 6
	var current, peak int32

	var wg sync.WaitGroup
	for i := 0; i < goroutines; i++ {
		tenant := fmt.Sprintf("t-%d", i)
		wg.Add(1)
		go func(tid string) {
			defer wg.Done()
			ctx := ctxWithTenant(tid)
			_ = s.withPersonLock(ctx, "p-shared", func() error {
				trackMaxConcurrent(t, &current, &peak)
				time.Sleep(20 * time.Millisecond)
				atomic.AddInt32(&current, -1)
				return nil
			})
		}(tenant)
	}
	wg.Wait()

	if peak < 2 {
		t.Errorf("withPersonLock should isolate tenants; peak = %d (expected ≥2)", peak)
	}
}

// TestLockKeyComposition pins the key format. The null byte separator
// avoids ambiguity between (tenant="ab", person="c") and
// (tenant="a", person="bc"), which a plain join would conflate.
func TestLockKeyComposition(t *testing.T) {
	ctxA := ctxWithTenant("ab")
	ctxB := ctxWithTenant("a")
	if lockKey(ctxA, "c") == lockKey(ctxB, "bc") {
		t.Errorf("lockKey must distinguish (\"ab\",\"c\") from (\"a\",\"bc\")")
	}
}
