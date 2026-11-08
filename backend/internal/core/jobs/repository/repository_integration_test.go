package repository

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/orkestra/backend/internal/core/jobs/models"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

const mongoTestURIEnv = "MONGO_TEST_URI"

func newTestDB(t *testing.T) *mongo.Database {
	t.Helper()
	uri := os.Getenv(mongoTestURIEnv)
	if uri == "" {
		t.Skipf("skipping integration test: set %s to run", mongoTestURIEnv)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	client, err := mongo.Connect(ctx, options.Client().ApplyURI(uri))
	if err != nil {
		t.Fatalf("mongo connect: %v", err)
	}
	if err := client.Ping(ctx, nil); err != nil {
		t.Fatalf("mongo ping: %v", err)
	}
	suffix := make([]byte, 4)
	_, _ = rand.Read(suffix)
	db := client.Database("orkestra_test_jobs_" + hex.EncodeToString(suffix))
	t.Cleanup(func() {
		c, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = db.Drop(c)
		_ = client.Disconnect(c)
	})
	return db
}

func newStates(t *testing.T) StateRepository {
	return NewStateRepository(newTestDB(t).Collection(models.StatesCollection))
}

var t0 = time.Date(2026, 11, 8, 12, 0, 0, 0, time.UTC)

func tp(t time.Time) *time.Time { return &t }

func TestClaimIsExclusive(t *testing.T) {
	r := newStates(t)
	ctx := context.Background()
	if err := r.EnsureState(ctx, "a.job", "a", "fp", tp(t0), t0); err != nil {
		t.Fatal(err)
	}
	s, _ := r.Get(ctx, "a.job")
	var wg sync.WaitGroup
	results := make([]*models.State, 2)
	for i := range results {
		wg.Add(1)
		go func() {
			defer wg.Done()
			l := models.Lease{Owner: fmt.Sprint("r", i), RunID: fmt.Sprint("run", i), Until: t0.Add(time.Minute)}
			st, err := r.Claim(ctx, "a.job", s.Revision, l, t0)
			if err != nil {
				t.Error(err)
			}
			results[i] = st
		}()
	}
	wg.Wait()
	n := 0
	for _, st := range results {
		if st != nil {
			n++
		}
	}
	if n != 1 {
		t.Fatalf("expected exactly one winner, got %d", n)
	}
}

func TestClaimAfterLeaseExpiry(t *testing.T) {
	r := newStates(t)
	ctx := context.Background()
	_ = r.EnsureState(ctx, "k", "m", "fp", tp(t0), t0)
	s, _ := r.Get(ctx, "k")
	if st, err := r.Claim(ctx, "k", s.Revision, models.Lease{RunID: "old", Until: t0.Add(-time.Second)}, t0); err != nil || st == nil {
		t.Fatalf("first claim: %v %v", st, err)
	}
	s2, _ := r.Get(ctx, "k")
	pre, err := r.Claim(ctx, "k", s2.Revision, models.Lease{RunID: "new", Until: t0.Add(time.Minute)}, t0)
	if err != nil || pre == nil {
		t.Fatalf("expired lease must be claimable: %v %v", pre, err)
	}
	if pre.Lease == nil || pre.Lease.RunID != "old" {
		t.Fatalf("pre-image should carry old lease, got %+v", pre.Lease)
	}
	// live lease blocks
	s3, _ := r.Get(ctx, "k")
	if st, _ := r.Claim(ctx, "k", s3.Revision, models.Lease{RunID: "x", Until: t0.Add(time.Hour)}, t0); st != nil {
		t.Fatal("live lease must block claim")
	}
}

func TestCompleteKeepsOverride(t *testing.T) {
	r := newStates(t)
	ctx := context.Background()
	_ = r.EnsureState(ctx, "k", "m", "fp", tp(t0), t0)
	s, _ := r.Get(ctx, "k")
	_, err := r.Claim(ctx, "k", s.Revision, models.Lease{RunID: "r1", Until: t0.Add(time.Minute)}, t0)
	if err != nil {
		t.Fatal(err)
	}
	s, _ = r.Get(ctx, "k") // revision 1
	en := false
	if _, err := r.SetOverride(ctx, "k", s.Revision, &models.Override{Enabled: &en}, "fp2", nil, "op", t0); err != nil {
		t.Fatal(err)
	}
	ok, err := r.Complete(ctx, "k", "r1", Completion{
		LastRun:   models.LastRun{RunID: "r1", Status: models.StatusSucceeded, StartedAt: t0, FinishedAt: t0},
		SetNext:   true,
		NextRunAt: tp(t0.Add(time.Hour)),
	})
	if err != nil || !ok {
		t.Fatalf("complete: %v %v", ok, err)
	}
	got, _ := r.Get(ctx, "k")
	if got.Override == nil || got.Override.Enabled == nil || *got.Override.Enabled {
		t.Fatalf("override lost: %+v", got.Override)
	}
	if got.Fingerprint != "fp2" {
		t.Fatalf("fingerprint touched: %q", got.Fingerprint)
	}
	if got.Lease != nil {
		t.Fatal("lease should be cleared")
	}
	if got.Revision != 3 { // claim, override, complete
		t.Fatalf("revision = %d, want 3", got.Revision)
	}
	if got.LastRun == nil || got.LastRun.Status != models.StatusSucceeded {
		t.Fatalf("lastRun: %+v", got.LastRun)
	}
}

func TestCompleteLostLease(t *testing.T) {
	r := newStates(t)
	ctx := context.Background()
	_ = r.EnsureState(ctx, "k", "m", "fp", tp(t0), t0)
	s, _ := r.Get(ctx, "k")
	_, _ = r.Claim(ctx, "k", s.Revision, models.Lease{RunID: "r1", Until: t0.Add(time.Minute)}, t0)
	ok, err := r.Complete(ctx, "k", "other", Completion{})
	if err != nil || ok {
		t.Fatalf("wrong runID must return false: %v %v", ok, err)
	}
	if ok, _ := r.Renew(ctx, "k", "other", t0.Add(time.Hour)); ok {
		t.Fatal("renew with wrong runID must be false")
	}
	if ok, _ := r.Renew(ctx, "k", "r1", t0.Add(time.Hour)); !ok {
		t.Fatal("renew with right runID must be true")
	}
}

func TestRequestRunConflicts(t *testing.T) {
	r := newStates(t)
	ctx := context.Background()
	if _, err := r.RequestRun(ctx, "nope", "u", t0); !errors.Is(err, ErrNotFound) {
		t.Fatalf("unknown: %v", err)
	}
	_ = r.EnsureState(ctx, "k", "m", "fp", tp(t0.Add(time.Hour)), t0)
	st, err := r.RequestRun(ctx, "k", "alice", t0)
	if err != nil || st.RunRequestedAt == nil || st.RunRequestedBy != "alice" {
		t.Fatalf("request: %+v %v", st, err)
	}
	if _, err := r.RequestRun(ctx, "k", "bob", t0); !errors.Is(err, ErrAlreadyRequested) {
		t.Fatalf("second request: %v", err)
	}
	_ = r.EnsureState(ctx, "j", "m", "fp", tp(t0.Add(time.Hour)), t0)
	s, _ := r.Get(ctx, "j")
	_, _ = r.Claim(ctx, "j", s.Revision, models.Lease{RunID: "r", Until: t0.Add(time.Minute)}, t0)
	if _, err := r.RequestRun(ctx, "j", "u", t0); !errors.Is(err, ErrAlreadyRunning) {
		t.Fatalf("running: %v", err)
	}
}

func TestSetOverrideRevision(t *testing.T) {
	r := newStates(t)
	ctx := context.Background()
	if _, err := r.SetOverride(ctx, "nope", 0, nil, "fp", nil, "u", t0); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing: %v", err)
	}
	_ = r.EnsureState(ctx, "k", "m", "fp", tp(t0), t0)
	if _, err := r.SetOverride(ctx, "k", 99, nil, "fp", nil, "u", t0); !errors.Is(err, ErrRevisionConflict) {
		t.Fatalf("stale: %v", err)
	}
	st, err := r.SetOverride(ctx, "k", 0, nil, "fp", nil, "u", t0)
	if err != nil || st.NextRunAt != nil || st.Revision != 1 {
		t.Fatalf("override: %+v %v", st, err)
	}
	ok, err := r.Reschedule(ctx, "k", 1, "fp3", tp(t0.Add(time.Hour)), t0)
	if err != nil || !ok {
		t.Fatalf("reschedule: %v %v", ok, err)
	}
	if ok, _ := r.Reschedule(ctx, "k", 1, "fp4", nil, t0); ok {
		t.Fatal("stale reschedule must fail")
	}
}

func TestDueSelection(t *testing.T) {
	r := newStates(t)
	ctx := context.Background()
	_ = r.EnsureState(ctx, "disabled", "m", "fp", nil, t0)
	_ = r.EnsureState(ctx, "future", "m", "fp", tp(t0.Add(time.Hour)), t0)
	_ = r.EnsureState(ctx, "due", "m", "fp", tp(t0.Add(-time.Minute)), t0)
	_ = r.EnsureState(ctx, "requested", "m", "fp", nil, t0)
	_, _ = r.RequestRun(ctx, "requested", "u", t0)
	_ = r.EnsureState(ctx, "leased", "m", "fp", tp(t0.Add(-time.Minute)), t0)
	s, _ := r.Get(ctx, "leased")
	_, _ = r.Claim(ctx, "leased", s.Revision, models.Lease{RunID: "x", Until: t0.Add(time.Minute)}, t0)
	_ = r.EnsureState(ctx, "expired", "m", "fp", tp(t0.Add(-time.Minute)), t0)
	s, _ = r.Get(ctx, "expired")
	_, _ = r.Claim(ctx, "expired", s.Revision, models.Lease{RunID: "y", Until: t0.Add(-time.Second)}, t0)

	got, err := r.Due(ctx, t0, 10)
	if err != nil {
		t.Fatal(err)
	}
	var keys []string
	for _, s := range got {
		keys = append(keys, s.Key)
	}
	want := []string{"due", "expired", "requested"}
	if fmt.Sprint(keys) != fmt.Sprint(want) {
		t.Fatalf("due = %v, want %v", keys, want)
	}
	if lim, _ := r.Due(ctx, t0, 2); len(lim) != 2 {
		t.Fatalf("limit not applied: %d", len(lim))
	}
}

func TestRunListCursor(t *testing.T) {
	db := newTestDB(t)
	r := NewRunRepository(db.Collection(models.RunsCollection))
	ctx := context.Background()
	for i := 0; i < 5; i++ {
		st := models.StatusSucceeded
		if i%2 == 1 {
			st = models.StatusFailed
		}
		rec := &models.RunRecord{ID: fmt.Sprintf("run-%d", i), Key: "k", Module: "m", Status: st, StartedAt: t0.Add(time.Duration(i) * time.Minute)}
		if err := r.Insert(ctx, rec); err != nil {
			t.Fatal(err)
		}
	}
	_ = r.Insert(ctx, &models.RunRecord{ID: "other", Key: "z", StartedAt: t0})

	var ids []string
	var cur *RunCursor
	pages := 0
	for {
		items, next, err := r.List(ctx, "k", "", cur, 2)
		if err != nil {
			t.Fatal(err)
		}
		pages++
		for _, it := range items {
			ids = append(ids, it.ID)
		}
		if next == nil {
			break
		}
		// round-trip the wire form
		dec, err := DecodeRunCursor(EncodeRunCursor(next))
		if err != nil || !dec.StartedAt.Equal(next.StartedAt) || dec.ID != next.ID {
			t.Fatalf("cursor roundtrip: %+v %v", dec, err)
		}
		cur = dec
	}
	if pages != 3 || fmt.Sprint(ids) != "[run-4 run-3 run-2 run-1 run-0]" {
		t.Fatalf("pages=%d ids=%v", pages, ids)
	}
	items, _, _ := r.List(ctx, "k", models.StatusFailed, nil, 10)
	if len(items) != 2 {
		t.Fatalf("status filter: %d", len(items))
	}
	if _, err := DecodeRunCursor("!!!"); err == nil {
		t.Fatal("bad cursor must error")
	}
}

func TestRunFinishAndMark(t *testing.T) {
	db := newTestDB(t)
	r := NewRunRepository(db.Collection(models.RunsCollection))
	ctx := context.Background()
	_ = r.Insert(ctx, &models.RunRecord{ID: "a", Key: "k", Status: models.StatusRunning, StartedAt: t0})
	_ = r.Insert(ctx, &models.RunRecord{ID: "b", Key: "k", Status: models.StatusRunning, StartedAt: t0.Add(time.Second)})
	if err := r.Finish(ctx, "a", models.StatusFailed, "boom", t0.Add(time.Second), 1000, map[string]any{"n": 1}, 2); err != nil {
		t.Fatal(err)
	}
	if err := r.MarkIfRunning(ctx, "a", models.StatusAbandoned, t0); err != nil {
		t.Fatal(err)
	}
	if err := r.MarkIfRunning(ctx, "b", models.StatusInterrupted, t0); err != nil {
		t.Fatal(err)
	}
	items, _, _ := r.List(ctx, "k", "", nil, 10)
	by := map[string]models.RunRecord{}
	for _, i := range items {
		by[i.ID] = i
	}
	if by["a"].Status != models.StatusFailed || by["a"].Error != "boom" || by["a"].DroppedDetails != 2 || by["a"].DurationMs != 1000 {
		t.Fatalf("a: %+v", by["a"])
	}
	if by["b"].Status != models.StatusInterrupted || by["b"].FinishedAt == nil {
		t.Fatalf("b: %+v", by["b"])
	}
}

func TestDeleteOlderThan(t *testing.T) {
	db := newTestDB(t)
	r := NewRunRepository(db.Collection(models.RunsCollection))
	ctx := context.Background()
	_ = r.Insert(ctx, &models.RunRecord{ID: "old", Key: "k", StartedAt: t0.Add(-48 * time.Hour)})
	_ = r.Insert(ctx, &models.RunRecord{ID: "new", Key: "k", StartedAt: t0})
	n, err := r.DeleteOlderThan(ctx, t0.Add(-24*time.Hour))
	if err != nil || n != 1 {
		t.Fatalf("deleted %d %v", n, err)
	}
	items, _, _ := r.List(ctx, "k", "", nil, 10)
	if len(items) != 1 || items[0].ID != "new" {
		t.Fatalf("remaining: %+v", items)
	}
}

// An operator edit that lands while a run is in flight changes the stored
// fingerprint; the completion computed from the claim-time state must not
// overwrite the edited nextRunAt nor schedule a retry.
func TestCompleteRespectsConcurrentOverride(t *testing.T) {
	r := newStates(t)
	ctx := context.Background()
	claim := func(key string) {
		t.Helper()
		if err := r.EnsureState(ctx, key, "m", "fp", tp(t0), t0); err != nil {
			t.Fatal(err)
		}
		s, _ := r.Get(ctx, key)
		if st, err := r.Claim(ctx, key, s.Revision, models.Lease{RunID: "r-" + key, Until: t0.Add(time.Minute)}, t0); err != nil || st == nil {
			t.Fatalf("claim %s: %v %v", key, st, err)
		}
	}
	failed := func(key string) Completion {
		return Completion{
			LastRun:           models.LastRun{RunID: "r-" + key, Status: models.StatusFailed, StartedAt: t0, FinishedAt: t0},
			ExpectFingerprint: "fp",
			SetNext:           true,
			NextRunAt:         tp(t0.Add(time.Hour)),
			RetryAt:           tp(t0.Add(time.Minute)),
			RetrySlotAt:       tp(t0),
			Attempt:           1,
		}
	}

	// 1. Unchanged fingerprint: the completion applies in full.
	claim("same")
	if ok, err := r.Complete(ctx, "same", "r-same", failed("same")); err != nil || !ok {
		t.Fatalf("complete same: %v %v", ok, err)
	}
	got, _ := r.Get(ctx, "same")
	if got.NextRunAt == nil || !got.NextRunAt.Equal(t0.Add(time.Hour)) ||
		got.RetryAt == nil || !got.RetryAt.Equal(t0.Add(time.Minute)) ||
		got.RetrySlotAt == nil || !got.RetrySlotAt.Equal(t0) || got.Attempt != 1 {
		t.Fatalf("same fingerprint: %+v", got)
	}

	// 2. Disabled during the run: no next slot, no retry.
	claim("off")
	s, _ := r.Get(ctx, "off")
	off := false
	if _, err := r.SetOverride(ctx, "off", s.Revision, &models.Override{Enabled: &off}, "fp-off", nil, "op", t0); err != nil {
		t.Fatal(err)
	}
	if ok, err := r.Complete(ctx, "off", "r-off", failed("off")); err != nil || !ok {
		t.Fatalf("complete off: %v %v", ok, err)
	}
	got, _ = r.Get(ctx, "off")
	if got.NextRunAt != nil || got.RetryAt != nil || got.RetrySlotAt != nil || got.Attempt != 0 {
		t.Fatalf("disabled job got scheduled: next=%v retry=%v slot=%v attempt=%d", got.NextRunAt, got.RetryAt, got.RetrySlotAt, got.Attempt)
	}
	if got.Lease != nil || got.LastRun == nil || got.LastRun.Status != models.StatusFailed || got.Fingerprint != "fp-off" {
		t.Fatalf("run fields not applied: %+v", got)
	}

	// 3. Schedule changed during the run: the operator's nextRunAt survives.
	claim("moved")
	s, _ = r.Get(ctx, "moved")
	edited := t0.Add(5 * time.Hour)
	o := &models.Override{Schedule: &models.ScheduleSpec{Kind: "every", EverySeconds: 18000}}
	if _, err := r.SetOverride(ctx, "moved", s.Revision, o, "fp-moved", &edited, "op", t0); err != nil {
		t.Fatal(err)
	}
	if ok, err := r.Complete(ctx, "moved", "r-moved", failed("moved")); err != nil || !ok {
		t.Fatalf("complete moved: %v %v", ok, err)
	}
	got, _ = r.Get(ctx, "moved")
	if got.NextRunAt == nil || !got.NextRunAt.Equal(edited) {
		t.Fatalf("operator nextRunAt overwritten: %v", got.NextRunAt)
	}
	if got.RetryAt != nil || got.Attempt != 0 {
		t.Fatalf("retry scheduled after edit: %v %d", got.RetryAt, got.Attempt)
	}
	if got.Revision != 3 { // claim, override, complete
		t.Fatalf("revision = %d, want 3", got.Revision)
	}
}

// A run record that already reached a terminal status (abandoned by another
// replica, interrupted by Stop) is not overwritten by a late Finish.
func TestFinishKeepsTerminalStatus(t *testing.T) {
	db := newTestDB(t)
	r := NewRunRepository(db.Collection(models.RunsCollection))
	ctx := context.Background()
	_ = r.Insert(ctx, &models.RunRecord{ID: "a", Key: "k", Status: models.StatusRunning, StartedAt: t0})
	if err := r.MarkIfRunning(ctx, "a", models.StatusAbandoned, t0); err != nil {
		t.Fatal(err)
	}
	if err := r.Finish(ctx, "a", models.StatusSucceeded, "", t0.Add(time.Second), 1000, nil, 0); err != nil {
		t.Fatal(err)
	}
	items, _, _ := r.List(ctx, "k", "", nil, 10)
	if len(items) != 1 || items[0].Status != models.StatusAbandoned {
		t.Fatalf("terminal status overwritten: %+v", items)
	}
}
