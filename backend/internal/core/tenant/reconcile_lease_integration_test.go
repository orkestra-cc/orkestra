package tenant

// Lost-reconcile-lease behaviour. Reuses reconcile_integration_test.go's
// harness (newReconcileHarness / newReplica / seedTenant / record), and its
// MONGO_TEST_URI gate.

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/orkestra/backend/internal/core/tenant/models"
	"github.com/orkestra/backend/internal/shared/systeminit"
)

// lostFinishStore reports the first FinishReconcile as a CAS miss — the
// exact shape a replica sees when its reconcile lease expired mid-run and
// another replica took the lease over. Later calls delegate.
//
// It deliberately does NOT stamp the version behind the caller's back: the
// point of the test is the case where this replica genuinely cannot prove
// the reconciliation was ever recorded.
type lostFinishStore struct {
	systeminit.FinalizationStore
	mu    sync.Mutex
	calls int
}

func (s *lostFinishStore) FinishReconcile(ctx context.Context, version int, owner string) (bool, error) {
	s.mu.Lock()
	s.calls++
	first := s.calls == 1
	s.mu.Unlock()
	if first {
		return false, nil
	}
	return s.FinalizationStore.FinishReconcile(ctx, version, owner)
}

func (s *lostFinishStore) callCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.calls
}

// TestReconcile_LostLeaseBeforeFinish_DoesNotReportSuccessUnstamped pins the
// rule the saga's own executor already follows and reconciliation used to
// break: **completion is judged by the CAS, never by having held a lease.**
//
// FinishReconcile's bool was discarded, so a replica whose lease expired
// while runReconciliationV1 was still writing returned nil from Start and
// went on to serve traffic — having performed reconciliation writes
// WITHOUT the lease, concurrently with whichever replica took it over, and
// leaving the version unstamped so every later boot re-runs the migration.
//
// Not parallel: it shrinks the package's lease timings so the takeover
// window is milliseconds rather than a minute.
func TestReconcile_LostLeaseBeforeFinish_DoesNotReportSuccessUnstamped(t *testing.T) {
	h := newReconcileHarness(t)

	origTTL, origWait := reconcileLeaseTTL, reconcileWaitInterval
	reconcileLeaseTTL, reconcileWaitInterval = 150*time.Millisecond, 20*time.Millisecond
	t.Cleanup(func() { reconcileLeaseTTL, reconcileWaitInterval = origTTL, origWait })

	h.users.count = 5
	h.seedTenant(t, "t-only", models.TenantStatusActive, time.Now().Add(-96*time.Hour).UTC(), false)

	store := &lostFinishStore{FinalizationStore: h.store}
	m := h.newReplica(t)
	m.finalization = store

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	if err := m.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}

	rec := h.record(t)
	if rec == nil || rec.ReconciliationVersion != setupReconciliationVersion {
		t.Fatalf("Start reported success while the reconciliation version is unstamped (record=%+v): "+
			"a replica must not come up on the strength of work whose completion it could not record", rec)
	}
	if n := store.callCount(); n < 2 {
		t.Fatalf("FinishReconcile called %d time(s): a CAS miss must be re-derived from the record, not ignored", n)
	}
	if rec.ReconcileLeaseOwner != "" || rec.ReconcileLeaseUntil != nil {
		t.Errorf("reconcile lease not released: owner=%q until=%v", rec.ReconcileLeaseOwner, rec.ReconcileLeaseUntil)
	}
	// The retry re-ran an idempotent reconciliation: still exactly one
	// pointer, still naming the only operational tenant.
	if d := h.defaultPointer(t); d == nil || d.TenantUUID != "t-only" {
		t.Errorf("default pointer = %+v, want t-only", d)
	}
}

// TestReconcile_LostLeaseButVersionPublished_ReturnsWithoutRerunning covers
// the other half: when the replica that took the lease over has already
// published the version, the CAS miss must resolve on the very next read —
// no second reconciliation pass, no waiting out a lease.
func TestReconcile_LostLeaseButVersionPublished_ReturnsWithoutRerunning(t *testing.T) {
	h := newReconcileHarness(t)
	h.users.count = 5
	h.seedTenant(t, "t-only", models.TenantStatusActive, time.Now().Add(-96*time.Hour).UTC(), false)

	store := &rivalFinishStore{FinalizationStore: h.store}
	m := h.newReplica(t)
	m.finalization = store

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	if err := m.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}

	rec := h.record(t)
	if rec == nil || rec.ReconciliationVersion != setupReconciliationVersion {
		t.Fatalf("record = %+v, want reconciliationVersion %d", rec, setupReconciliationVersion)
	}
	if n := store.callCount(); n != 1 {
		t.Fatalf("FinishReconcile called %d time(s), want 1: an already-published version needs no second pass", n)
	}
}

// rivalFinishStore models the rival replica winning: the version really is
// published (by the delegate, under this owner — the harness has only one
// real lease holder), but the caller is told its own CAS missed.
type rivalFinishStore struct {
	systeminit.FinalizationStore
	mu    sync.Mutex
	calls int
}

func (s *rivalFinishStore) FinishReconcile(ctx context.Context, version int, owner string) (bool, error) {
	s.mu.Lock()
	s.calls++
	first := s.calls == 1
	s.mu.Unlock()
	if first {
		if _, err := s.FinalizationStore.FinishReconcile(ctx, version, owner); err != nil {
			return false, err
		}
		return false, nil
	}
	return s.FinalizationStore.FinishReconcile(ctx, version, owner)
}

func (s *rivalFinishStore) callCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.calls
}
