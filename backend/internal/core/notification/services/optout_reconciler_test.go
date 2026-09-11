package services

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/orkestra/backend/internal/core/notification/models"
)

// ---------------------------------------------------------------------------
// Fakes for the reconciler. Same style as unsubscribe_consume_test.go: small
// in-memory structs, assertions on the state they persisted rather than on
// call bookkeeping — plus the one counter the budget tests need.
// ---------------------------------------------------------------------------

// fixedNow is the clock every reconciler test runs on. Time is injected so the
// backoff is asserted by reading the instant the store was told to wait for,
// never by sleeping.
func fixedNow() time.Time { return time.Date(2026, 9, 11, 12, 0, 0, 0, time.UTC) }

// reconcileStore is an in-memory ReconcileTokenStore. Its ListPending mirrors
// the repository filter exactly — not dead-lettered, something still pending,
// and due now — because the reconciler leans on that contract: every row the
// scan hands back is a row it is going to act on.
type reconcileStore struct {
	mu   sync.Mutex
	docs []models.UnsubscribeTokenDoc

	listErr       error
	clearSinkErr  error
	clearPrefErr  error
	recordErr     error
	deadLetterErr error

	listCalls int
}

// newReconcileStore seeds the store. A document with no TokenHash gets a
// synthetic one, because the reconciler addresses rows by hash and the tests
// care about the uuid.
func newReconcileStore(docs ...models.UnsubscribeTokenDoc) *reconcileStore {
	seeded := make([]models.UnsubscribeTokenDoc, 0, len(docs))
	for _, d := range docs {
		if d.TokenHash == "" {
			d.TokenHash = "hash-" + d.UUID
		}
		seeded = append(seeded, d)
	}
	return &reconcileStore{docs: seeded}
}

func (s *reconcileStore) ListPending(_ context.Context, now time.Time, limit int) ([]models.UnsubscribeTokenDoc, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.listCalls++
	if s.listErr != nil {
		return nil, s.listErr
	}
	var out []models.UnsubscribeTokenDoc
	for _, d := range s.docs {
		if d.DeadLetteredAt != nil {
			continue
		}
		if !d.SinkPending && !d.PrefPending {
			continue
		}
		if d.NextAttemptAt != nil && d.NextAttemptAt.After(now) {
			continue
		}
		out = append(out, d)
		if limit > 0 && len(out) == limit {
			break
		}
	}
	return out, nil
}

func (s *reconcileStore) ClearSinkPending(_ context.Context, hash string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.clearSinkErr != nil {
		return s.clearSinkErr
	}
	if d := s.find(hash); d != nil {
		d.SinkPending = false
	}
	return nil
}

func (s *reconcileStore) ClearPrefPending(_ context.Context, hash string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.clearPrefErr != nil {
		return s.clearPrefErr
	}
	if d := s.find(hash); d != nil {
		d.PrefPending = false
	}
	return nil
}

func (s *reconcileStore) RecordFailedAttempt(_ context.Context, hash string, retryAt time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.recordErr != nil {
		return s.recordErr
	}
	if d := s.find(hash); d != nil {
		d.Attempts++
		at := retryAt
		d.NextAttemptAt = &at
	}
	return nil
}

func (s *reconcileStore) MarkDeadLettered(_ context.Context, hash string, now time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.deadLetterErr != nil {
		return s.deadLetterErr
	}
	if d := s.find(hash); d != nil {
		at := now
		d.DeadLetteredAt = &at
		d.Attempts++
		d.SinkPending = false
		d.PrefPending = false
		d.NextAttemptAt = nil
	}
	return nil
}

// find must be called with the lock held.
func (s *reconcileStore) find(hash string) *models.UnsubscribeTokenDoc {
	for i := range s.docs {
		if s.docs[i].TokenHash == hash {
			return &s.docs[i]
		}
	}
	return nil
}

func (s *reconcileStore) byUUID(uuid string) models.UnsubscribeTokenDoc {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, d := range s.docs {
		if d.UUID == uuid {
			return d
		}
	}
	return models.UnsubscribeTokenDoc{}
}

func (s *reconcileStore) scans() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.listCalls
}

// failingSink is the downstream mirror. err nil means it accepts.
type failingSink struct {
	mu    sync.Mutex
	err   error
	calls int

	addresses  []string
	categories []string
	refs       []string
}

func (s *failingSink) OnMarketingUnsubscribe(_ context.Context, address, category, refContext string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.calls++
	s.addresses = append(s.addresses, address)
	s.categories = append(s.categories, category)
	s.refs = append(s.refs, refContext)
	return s.err
}

func (s *failingSink) count() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.calls
}

// reconcilePrefs is the preference seam.
type reconcilePrefs struct {
	err error

	n                           int
	lastUser, lastCat, lastChan string
	lastOptedIn                 bool
}

func (f *reconcilePrefs) CanDeliver(context.Context, string, string, string, string) (bool, error) {
	return true, nil
}

func (f *reconcilePrefs) List(context.Context, string) ([]*models.PreferenceDoc, error) {
	return nil, nil
}

func (f *reconcilePrefs) Set(_ context.Context, user, category, channel string, optedIn bool) error {
	f.n++
	f.lastUser, f.lastCat, f.lastChan, f.lastOptedIn = user, category, channel, optedIn
	return f.err
}

// ---------------------------------------------------------------------------
// The budget
// ---------------------------------------------------------------------------

func TestReconciler_DeadLettersAfterTheBudget(t *testing.T) {
	store := newReconcileStore(
		models.UnsubscribeTokenDoc{UUID: "u1", Address: "ada@example.test", SinkPending: true, Attempts: ReconcileMaxAttempts - 1},
	)
	sink := &failingSink{err: errors.New("sink still down")}
	r := NewOptoutReconciler(OptoutReconcilerDeps{Tokens: store, Sink: sink, Now: fixedNow})

	if _, err := r.RunOnce(context.Background()); err != nil {
		t.Fatalf("RunOnce: %v", err)
	}
	got := store.byUUID("u1")
	if got.DeadLetteredAt == nil {
		t.Fatal("past the budget the row goes to dead letter, it is not retried forever")
	}
	if got.SinkPending {
		t.Fatal("a dead-lettered row must not stay pending: the reconciler would pick it up forever")
	}

	// A second pass must not touch it.
	sink.calls = 0
	if _, err := r.RunOnce(context.Background()); err != nil {
		t.Fatalf("RunOnce: %v", err)
	}
	if sink.calls != 0 {
		t.Fatalf("a dead-lettered row must not be retried, calls=%d", sink.calls)
	}
}

func TestReconciler_DeadLetterIsLoggedAtError(t *testing.T) {
	var buf bytes.Buffer
	store := newReconcileStore(
		models.UnsubscribeTokenDoc{UUID: "u1", Address: "ada@example.test", SinkPending: true, Attempts: ReconcileMaxAttempts - 1},
	)
	r := NewOptoutReconciler(OptoutReconcilerDeps{
		Tokens: store,
		Sink:   &failingSink{err: errors.New("sink still down")},
		Now:    fixedNow,
		Logger: slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})),
	})
	stats, err := r.RunOnce(context.Background())
	if err != nil {
		t.Fatalf("RunOnce: %v", err)
	}
	if stats.DeadLettered != 1 {
		t.Fatalf("deadLettered = %d, want 1", stats.DeadLettered)
	}
	out := buf.String()
	if !strings.Contains(out, "level=ERROR") {
		t.Fatalf("giving up on a recipient's opt-out is an error, not a warning: %s", out)
	}
	if !strings.Contains(out, "u1") {
		t.Fatalf("the dead-letter line must name the token uuid so an operator can find the row: %s", out)
	}
}

func TestReconciler_SinkFailureCountsTowardTheBudget(t *testing.T) {
	store := newReconcileStore(
		models.UnsubscribeTokenDoc{UUID: "u1", Address: "ada@example.test", SinkPending: true},
	)
	sink := &failingSink{err: errors.New("sink still down")}
	r := NewOptoutReconciler(OptoutReconcilerDeps{Tokens: store, Sink: sink, Now: fixedNow})

	stats, err := r.RunOnce(context.Background())
	if err != nil {
		t.Fatalf("RunOnce: %v", err)
	}
	if stats.Retried != 1 || stats.DeadLettered != 0 {
		t.Fatalf("stats = %+v, want one retry and no dead letter", stats)
	}
	got := store.byUUID("u1")
	if got.Attempts != 1 {
		t.Fatalf("attempts = %d, want 1: a failed row must move toward its budget", got.Attempts)
	}
	if !got.SinkPending {
		t.Fatal("a failed sink must stay pending: clearing it would drop the opt-out downstream")
	}
	if got.DeadLetteredAt != nil {
		t.Fatal("one failure is not the budget")
	}
	if got.NextAttemptAt == nil || !got.NextAttemptAt.After(fixedNow()) {
		t.Fatalf("a failed row must be deferred, nextAttemptAt = %v", got.NextAttemptAt)
	}
}

func TestReconciler_RetriesUntilTheBudgetThenStops(t *testing.T) {
	// The whole machine, one pass per attempt: eight failures and no more.
	store := newReconcileStore(
		models.UnsubscribeTokenDoc{UUID: "u1", Address: "ada@example.test", SinkPending: true},
	)
	sink := &failingSink{err: errors.New("sink still down")}
	now := fixedNow()
	r := NewOptoutReconciler(OptoutReconcilerDeps{Tokens: store, Sink: sink, Now: func() time.Time { return now }})

	for pass := 0; pass < ReconcileMaxAttempts+4; pass++ {
		if _, err := r.RunOnce(context.Background()); err != nil {
			t.Fatalf("RunOnce pass %d: %v", pass, err)
		}
		// Jump past whatever backoff the row was given, so every pass is due.
		now = now.Add(24 * time.Hour)
	}
	if sink.count() != ReconcileMaxAttempts {
		t.Fatalf("the sink was called %d times, want exactly the %d-attempt budget", sink.count(), ReconcileMaxAttempts)
	}
	got := store.byUUID("u1")
	if got.DeadLetteredAt == nil || got.SinkPending {
		t.Fatalf("the row must end dead-lettered and not pending: %+v", got)
	}
}

func TestReconcileBackoff_GrowsWithAttemptsAndIsCapped(t *testing.T) {
	prev := time.Duration(0)
	for attempts := 1; attempts <= ReconcileMaxAttempts; attempts++ {
		d := reconcileBackoff(attempts)
		if d <= 0 {
			t.Fatalf("backoff(%d) = %v, must be positive", attempts, d)
		}
		if d < prev {
			t.Fatalf("backoff must not shrink: backoff(%d) = %v after %v", attempts, d, prev)
		}
		if d > reconcileBackoffMax {
			t.Fatalf("backoff(%d) = %v, above the cap %v", attempts, d, reconcileBackoffMax)
		}
		prev = d
	}
	if reconcileBackoff(1) >= reconcileBackoff(ReconcileMaxAttempts) {
		t.Fatal("the backoff must actually grow across the budget, otherwise it is a fixed delay")
	}
	// A corrupt counter must not shift the base into a negative duration.
	if d := reconcileBackoff(1 << 20); d != reconcileBackoffMax {
		t.Fatalf("backoff(huge) = %v, want the cap %v", d, reconcileBackoffMax)
	}
	if d := reconcileBackoff(0); d <= 0 {
		t.Fatalf("backoff(0) = %v, must still be a real delay", d)
	}
}

// ---------------------------------------------------------------------------
// The sink flag
// ---------------------------------------------------------------------------

func TestReconciler_SinkSuccessClearsSinkPending(t *testing.T) {
	store := newReconcileStore(
		models.UnsubscribeTokenDoc{UUID: "u1", Address: "ada@example.test", Category: "marketing.newsletter", Context: "ref-9", SinkPending: true},
	)
	sink := &failingSink{}
	r := NewOptoutReconciler(OptoutReconcilerDeps{Tokens: store, Sink: sink, Now: fixedNow})

	stats, err := r.RunOnce(context.Background())
	if err != nil {
		t.Fatalf("RunOnce: %v", err)
	}
	if stats.Sinks != 1 || stats.Retried != 0 {
		t.Fatalf("stats = %+v, want one sink and no retry", stats)
	}
	got := store.byUUID("u1")
	if got.SinkPending {
		t.Fatal("a sink that accepted must lower its flag, or the row is retried forever")
	}
	if got.Attempts != 0 {
		t.Fatalf("attempts = %d, want 0: a success does not consume the budget", got.Attempts)
	}
	if sink.count() != 1 {
		t.Fatalf("sink calls = %d, want 1", sink.count())
	}
	if sink.addresses[0] != "ada@example.test" || sink.categories[0] != "marketing.newsletter" || sink.refs[0] != "ref-9" {
		t.Fatalf("the sink must receive the row's own address, category and context: %q %q %q",
			sink.addresses[0], sink.categories[0], sink.refs[0])
	}

	// Settled: the next pass has nothing to scan.
	if _, err := r.RunOnce(context.Background()); err != nil {
		t.Fatalf("RunOnce: %v", err)
	}
	if sink.count() != 1 {
		t.Fatalf("a settled row must not be replayed, calls=%d", sink.count())
	}
}

func TestReconciler_EmptyCategoryDefaultsToMarketing(t *testing.T) {
	store := newReconcileStore(
		models.UnsubscribeTokenDoc{UUID: "u1", Address: "ada@example.test", SinkPending: true},
	)
	sink := &failingSink{}
	r := NewOptoutReconciler(OptoutReconcilerDeps{Tokens: store, Sink: sink, Now: fixedNow})
	if _, err := r.RunOnce(context.Background()); err != nil {
		t.Fatalf("RunOnce: %v", err)
	}
	if sink.categories[0] != models.TypeMarketing {
		t.Fatalf("category = %q, want the same default the consume path applies (%q)",
			sink.categories[0], models.TypeMarketing)
	}
}

func TestReconciler_WithoutASinkNothingStaysPending(t *testing.T) {
	// The base registers no sink at all — the normal case. Those rows must
	// settle on the first pass instead of piling up against a consumer that
	// does not exist.
	store := newReconcileStore(
		models.UnsubscribeTokenDoc{UUID: "u1", Address: "ada@example.test", SinkPending: true},
	)
	r := NewOptoutReconciler(OptoutReconcilerDeps{Tokens: store, Now: fixedNow})

	stats, err := r.RunOnce(context.Background())
	if err != nil {
		t.Fatalf("RunOnce: %v", err)
	}
	got := store.byUUID("u1")
	if got.SinkPending {
		t.Fatal("with no sink registered there is nothing to mirror: the flag must come down")
	}
	if got.Attempts != 0 || got.DeadLetteredAt != nil {
		t.Fatalf("a missing sink is not a failure: %+v", got)
	}
	if stats.Sinks != 1 {
		t.Fatalf("stats = %+v, want the row counted as settled", stats)
	}
}

func TestReconciler_ClearFailureCountsTowardTheBudget(t *testing.T) {
	// The sink accepted but the flag could not be lowered: the row comes back
	// on the next scan, so it has to move toward the budget. Otherwise it
	// costs the downstream system one call per tick, forever.
	store := newReconcileStore(
		models.UnsubscribeTokenDoc{UUID: "u1", Address: "ada@example.test", SinkPending: true},
	)
	store.clearSinkErr = errors.New("write concern not satisfied")
	sink := &failingSink{}
	r := NewOptoutReconciler(OptoutReconcilerDeps{Tokens: store, Sink: sink, Now: fixedNow})

	if _, err := r.RunOnce(context.Background()); err != nil {
		t.Fatalf("RunOnce: %v", err)
	}
	got := store.byUUID("u1")
	if got.Attempts != 1 {
		t.Fatalf("attempts = %d, want 1: a flag that would not come down is not progress", got.Attempts)
	}
}

func TestReconciler_FailedDeferralWriteIsNotCountedAsARetry(t *testing.T) {
	// The row could not actually be deferred — RecordFailedAttempt itself
	// failed — so it must not be reported as a retry: the pass log is the
	// only operator-visible signal for what a pass really did, and this row
	// did not move at all.
	store := newReconcileStore(
		models.UnsubscribeTokenDoc{UUID: "u1", Address: "ada@example.test", SinkPending: true},
	)
	store.recordErr = errors.New("write concern not satisfied")
	r := NewOptoutReconciler(OptoutReconcilerDeps{
		Tokens: store,
		Sink:   &failingSink{err: errors.New("sink still down")},
		Now:    fixedNow,
	})

	stats, err := r.RunOnce(context.Background())
	if err != nil {
		t.Fatalf("RunOnce: %v", err)
	}
	if stats.Retried != 0 || stats.DeadLettered != 0 {
		t.Fatalf("stats = %+v, want neither counter moved: the row was not deferred", stats)
	}
}

func TestReconciler_DeadLetterFallbackFailureCountsNeitherStat(t *testing.T) {
	// Both the dead-letter write and its deferral fallback fail: the row is
	// untouched, so it must be reported as neither a retry nor a dead letter.
	store := newReconcileStore(
		models.UnsubscribeTokenDoc{UUID: "u1", Address: "ada@example.test", SinkPending: true, Attempts: ReconcileMaxAttempts - 1},
	)
	store.deadLetterErr = errors.New("write concern not satisfied")
	store.recordErr = errors.New("write concern not satisfied")
	r := NewOptoutReconciler(OptoutReconcilerDeps{
		Tokens: store,
		Sink:   &failingSink{err: errors.New("sink still down")},
		Now:    fixedNow,
	})

	stats, err := r.RunOnce(context.Background())
	if err != nil {
		t.Fatalf("RunOnce: %v", err)
	}
	if stats.Retried != 0 || stats.DeadLettered != 0 {
		t.Fatalf("stats = %+v, want neither counter moved: neither write landed", stats)
	}
}

// ---------------------------------------------------------------------------
// The preference flag — the same machine, independently
// ---------------------------------------------------------------------------

func TestReconciler_PreferenceSuccessClearsPrefPending(t *testing.T) {
	store := newReconcileStore(
		models.UnsubscribeTokenDoc{UUID: "u1", Address: "ada@example.test", UserUUID: "user-1", Category: "marketing.newsletter", PrefPending: true},
	)
	prefs := &reconcilePrefs{}
	r := NewOptoutReconciler(OptoutReconcilerDeps{Tokens: store, Prefs: prefs, Now: fixedNow})

	stats, err := r.RunOnce(context.Background())
	if err != nil {
		t.Fatalf("RunOnce: %v", err)
	}
	if stats.Preferences != 1 {
		t.Fatalf("stats = %+v, want one preference written", stats)
	}
	got := store.byUUID("u1")
	if got.PrefPending {
		t.Fatal("a preference that was written must lower its flag")
	}
	if got.Attempts != 0 {
		t.Fatalf("attempts = %d, want 0", got.Attempts)
	}
	if prefs.n != 1 || prefs.lastUser != "user-1" || prefs.lastCat != "marketing.newsletter" ||
		prefs.lastChan != models.ChannelEmail || prefs.lastOptedIn {
		t.Fatalf("the preference must opt the token's user out of its own category on email: %+v", prefs)
	}
}

func TestReconciler_PreferenceFailureCountsTowardTheBudget(t *testing.T) {
	store := newReconcileStore(
		models.UnsubscribeTokenDoc{UUID: "u1", Address: "ada@example.test", UserUUID: "user-1", PrefPending: true},
	)
	r := NewOptoutReconciler(OptoutReconcilerDeps{
		Tokens: store,
		Prefs:  &reconcilePrefs{err: errors.New("preference store down")},
		Now:    fixedNow,
	})

	if _, err := r.RunOnce(context.Background()); err != nil {
		t.Fatalf("RunOnce: %v", err)
	}
	got := store.byUUID("u1")
	if !got.PrefPending {
		t.Fatal("a preference that did not write must stay pending")
	}
	if got.Attempts != 1 {
		t.Fatalf("attempts = %d, want 1", got.Attempts)
	}
}

func TestReconciler_PreferenceAndSinkAreIndependent(t *testing.T) {
	// A row can owe both. The preference succeeding says nothing about the
	// sink, so one flag comes down and the other does not.
	store := newReconcileStore(
		models.UnsubscribeTokenDoc{UUID: "u1", Address: "ada@example.test", UserUUID: "user-1", PrefPending: true, SinkPending: true},
	)
	r := NewOptoutReconciler(OptoutReconcilerDeps{
		Tokens: store,
		Prefs:  &reconcilePrefs{},
		Sink:   &failingSink{err: errors.New("sink still down")},
		Now:    fixedNow,
	})

	if _, err := r.RunOnce(context.Background()); err != nil {
		t.Fatalf("RunOnce: %v", err)
	}
	got := store.byUUID("u1")
	if got.PrefPending {
		t.Fatal("the preference was written: its flag must come down even though the sink failed")
	}
	if !got.SinkPending {
		t.Fatal("the sink failed: its flag must stay up even though the preference was written")
	}
	if got.Attempts != 1 {
		t.Fatalf("attempts = %d, want 1: the row still owes the sink", got.Attempts)
	}
}

func TestReconciler_PrefPendingWithoutAUserIsCleared(t *testing.T) {
	// The claim only raises prefPending for a token that names a user, so this
	// row is malformed. There is no preference row to write, and leaving the
	// flag up would have the scan return it on every tick for ever.
	store := newReconcileStore(
		models.UnsubscribeTokenDoc{UUID: "u1", Address: "ada@example.test", PrefPending: true},
	)
	prefs := &reconcilePrefs{}
	r := NewOptoutReconciler(OptoutReconcilerDeps{Tokens: store, Prefs: prefs, Now: fixedNow})

	if _, err := r.RunOnce(context.Background()); err != nil {
		t.Fatalf("RunOnce: %v", err)
	}
	got := store.byUUID("u1")
	if got.PrefPending {
		t.Fatal("a row with no user has no preference to write: the flag must not survive the pass")
	}
	if prefs.n != 0 {
		t.Fatalf("the preference service must not be called without a user, calls=%d", prefs.n)
	}
}

func TestReconciler_WithoutThePreferenceSeamCountsTowardTheBudget(t *testing.T) {
	// An unwired preference service is work left undone, not work that cannot
	// exist: the flag stays up and the row consumes its budget rather than
	// being silently dropped.
	store := newReconcileStore(
		models.UnsubscribeTokenDoc{UUID: "u1", Address: "ada@example.test", UserUUID: "user-1", PrefPending: true},
	)
	r := NewOptoutReconciler(OptoutReconcilerDeps{Tokens: store, Now: fixedNow})

	if _, err := r.RunOnce(context.Background()); err != nil {
		t.Fatalf("RunOnce: %v", err)
	}
	got := store.byUUID("u1")
	if !got.PrefPending {
		t.Fatal("without a preference service the work is undone, not done")
	}
	if got.Attempts != 1 {
		t.Fatalf("attempts = %d, want 1", got.Attempts)
	}
}

// ---------------------------------------------------------------------------
// Rows the scan must leave alone, and rows it can never finish
// ---------------------------------------------------------------------------

func TestReconciler_RowWithNothingPendingIsNotTouched(t *testing.T) {
	store := newReconcileStore(
		models.UnsubscribeTokenDoc{UUID: "settled", Address: "settled@example.test"},
		models.UnsubscribeTokenDoc{UUID: "u1", Address: "ada@example.test", SinkPending: true},
	)
	sink := &failingSink{}
	r := NewOptoutReconciler(OptoutReconcilerDeps{Tokens: store, Sink: sink, Now: fixedNow})

	stats, err := r.RunOnce(context.Background())
	if err != nil {
		t.Fatalf("RunOnce: %v", err)
	}
	if stats.Scanned != 1 {
		t.Fatalf("scanned = %d, want only the pending row", stats.Scanned)
	}
	settled := store.byUUID("settled")
	if settled.Attempts != 0 || settled.DeadLetteredAt != nil {
		t.Fatalf("a row with nothing pending must not be touched: %+v", settled)
	}
	if sink.count() != 1 || sink.addresses[0] != "ada@example.test" {
		t.Fatalf("only the pending row's address goes downstream: %v", sink.addresses)
	}
}

func TestReconciler_DeferredRowIsNotRetriedBeforeItIsDue(t *testing.T) {
	soon := fixedNow().Add(time.Hour)
	store := newReconcileStore(
		models.UnsubscribeTokenDoc{UUID: "u1", Address: "ada@example.test", SinkPending: true, Attempts: 1, NextAttemptAt: &soon},
	)
	sink := &failingSink{}
	r := NewOptoutReconciler(OptoutReconcilerDeps{Tokens: store, Sink: sink, Now: fixedNow})

	stats, err := r.RunOnce(context.Background())
	if err != nil {
		t.Fatalf("RunOnce: %v", err)
	}
	if stats.Scanned != 0 || sink.count() != 0 {
		t.Fatalf("the backoff is the scan's job: scanned=%d calls=%d", stats.Scanned, sink.count())
	}
}

func TestReconciler_RowWithoutAnAddressIsDeadLettered(t *testing.T) {
	// Nothing can be mirrored for an address that is not there, so retrying
	// it eight times would only delay the same conclusion.
	store := newReconcileStore(
		models.UnsubscribeTokenDoc{UUID: "u1", SinkPending: true},
	)
	sink := &failingSink{}
	r := NewOptoutReconciler(OptoutReconcilerDeps{Tokens: store, Sink: sink, Now: fixedNow})

	if _, err := r.RunOnce(context.Background()); err != nil {
		t.Fatalf("RunOnce: %v", err)
	}
	if sink.count() != 0 {
		t.Fatalf("a row with no address must not reach the sink, calls=%d", sink.count())
	}
	got := store.byUUID("u1")
	if got.DeadLetteredAt == nil || got.SinkPending {
		t.Fatalf("an unprocessable row must end, not loop: %+v", got)
	}
}

func TestReconciler_DeadLetterWriteFailureStillDefersTheRow(t *testing.T) {
	// If the terminal state cannot be written the row is still pending, so it
	// must at least be pushed out — otherwise the scan returns it on every
	// tick and the sink is called on every tick.
	store := newReconcileStore(
		models.UnsubscribeTokenDoc{UUID: "u1", Address: "ada@example.test", SinkPending: true, Attempts: ReconcileMaxAttempts - 1},
	)
	store.deadLetterErr = errors.New("write concern not satisfied")
	r := NewOptoutReconciler(OptoutReconcilerDeps{
		Tokens: store,
		Sink:   &failingSink{err: errors.New("sink still down")},
		Now:    fixedNow,
	})

	stats, err := r.RunOnce(context.Background())
	if err != nil {
		t.Fatalf("RunOnce: %v", err)
	}
	got := store.byUUID("u1")
	if got.NextAttemptAt == nil || !got.NextAttemptAt.After(fixedNow()) {
		t.Fatalf("a row that could not be closed must still be deferred: %+v", got)
	}
	// The row was not closed, so it must not be reported as a dead letter —
	// and it WAS deferred by the fallback write, so that has to be visible in
	// the pass stats the same way an ordinary retry is, not silently dropped.
	if stats.DeadLettered != 0 || stats.Retried != 1 {
		t.Fatalf("stats = %+v, want zero dead letters and one retry", stats)
	}
}

func TestReconciler_ScanErrorIsReturned(t *testing.T) {
	store := newReconcileStore()
	store.listErr = errors.New("no reachable server")
	r := NewOptoutReconciler(OptoutReconcilerDeps{Tokens: store, Now: fixedNow})

	if _, err := r.RunOnce(context.Background()); err == nil {
		t.Fatal("a scan that failed is the one thing RunOnce reports: the pass did not happen")
	}
}

func TestReconciler_WithoutATokenStoreIsAnError(t *testing.T) {
	r := NewOptoutReconciler(OptoutReconcilerDeps{Now: fixedNow})
	if _, err := r.RunOnce(context.Background()); err == nil {
		t.Fatal("an unwired reconciler must say so rather than report a clean pass")
	}
}

// ---------------------------------------------------------------------------
// What may never reach a log
// ---------------------------------------------------------------------------

func TestReconciler_NeverLogsTheAddressOrTheTokenHash(t *testing.T) {
	var buf bytes.Buffer
	store := newReconcileStore(models.UnsubscribeTokenDoc{
		UUID: "u1", TokenHash: "d3adb33fd3adb33f", Address: "ada@example.test",
		UserUUID: "user-1", PrefPending: true, SinkPending: true,
	})
	r := NewOptoutReconciler(OptoutReconcilerDeps{
		Tokens: store,
		Prefs:  &reconcilePrefs{err: errors.New("write failed for ada@example.test")},
		Sink:   &failingSink{err: errors.New("failed to upsert contact ada@example.test: timeout")},
		Now:    fixedNow,
		Logger: slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})),
	})

	if _, err := r.RunOnce(context.Background()); err != nil {
		t.Fatalf("RunOnce: %v", err)
	}
	out := buf.String()
	if strings.Contains(out, "ada@example.test") {
		t.Fatalf("the recipient's address reached the log: %s", out)
	}
	if !strings.Contains(out, "[address]") {
		t.Fatalf("the diagnostic must survive the scrub, only the address goes: %s", out)
	}
	if strings.Contains(out, "d3adb33f") {
		t.Fatalf("the token hash identifies the token as well as the token does; log the uuid: %s", out)
	}
	if !strings.Contains(out, "u1") {
		t.Fatalf("the token uuid is what makes a line actionable: %s", out)
	}
}

func TestReconciler_NeverLogsTheAddressWhenAWriteFails(t *testing.T) {
	// The other four log sites: the three bookkeeping writes and the
	// dead-letter fallback. A MongoDB write error quotes the document it
	// failed on, and that document is the token — which carries the address.
	writeErr := errors.New("validation failed for ada@example.test")
	for _, tc := range []struct {
		name string
		// sinkErr nil means the sink accepts, which is the only way the
		// bookkeeping write after it is reached at all.
		sinkErr error
		setup   func(*reconcileStore)
		doc     models.UnsubscribeTokenDoc
	}{
		{
			name:  "the flag would not come down",
			setup: func(s *reconcileStore) { s.clearSinkErr = writeErr },
			doc:   models.UnsubscribeTokenDoc{UUID: "u1", Address: "ada@example.test", SinkPending: true},
		},
		{
			name:    "the failed attempt would not record",
			sinkErr: errors.New("sink still down"),
			setup:   func(s *reconcileStore) { s.recordErr = writeErr },
			doc:     models.UnsubscribeTokenDoc{UUID: "u1", Address: "ada@example.test", SinkPending: true},
		},
		{
			name:    "the row would not dead-letter",
			sinkErr: errors.New("sink still down"),
			setup: func(s *reconcileStore) {
				s.deadLetterErr = writeErr
				s.recordErr = writeErr
			},
			doc: models.UnsubscribeTokenDoc{UUID: "u1", Address: "ada@example.test", SinkPending: true, Attempts: ReconcileMaxAttempts - 1},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var buf bytes.Buffer
			store := newReconcileStore(tc.doc)
			tc.setup(store)
			r := NewOptoutReconciler(OptoutReconcilerDeps{
				Tokens: store,
				Sink:   &failingSink{err: tc.sinkErr},
				Now:    fixedNow,
				Logger: slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})),
			})
			if _, err := r.RunOnce(context.Background()); err != nil {
				t.Fatalf("RunOnce: %v", err)
			}
			out := buf.String()
			if strings.Contains(out, "ada@example.test") {
				t.Fatalf("the recipient's address reached the log: %s", out)
			}
			if !strings.Contains(out, "[address]") {
				t.Fatalf("the diagnostic must survive the scrub: %s", out)
			}
		})
	}
}

func TestReconciler_RowWithNothingPendingIsReportedNotStamped(t *testing.T) {
	// The scan filters these out, so this only happens when the query and the
	// loop disagree. The row owes nothing: it must not be given a terminal
	// state, and the mismatch must not pass in silence.
	var buf bytes.Buffer
	store := newReconcileStore(models.UnsubscribeTokenDoc{UUID: "u1", Address: "ada@example.test"})
	r := NewOptoutReconciler(OptoutReconcilerDeps{
		Tokens: store,
		Now:    fixedNow,
		Logger: slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})),
	})
	stats := ReconcileStats{}
	doc := store.byUUID("u1")
	r.replay(context.Background(), &doc, fixedNow(), &stats)

	got := store.byUUID("u1")
	if got.DeadLetteredAt != nil || got.Attempts != 0 {
		t.Fatalf("a finished row must not be stamped: %+v", got)
	}
	if !strings.Contains(buf.String(), "nothing pending") {
		t.Fatalf("the mismatch must be reported: %s", buf.String())
	}
}

// ---------------------------------------------------------------------------
// Lifecycle
// ---------------------------------------------------------------------------

func TestReconciler_StopBeforeStartDoesNotPanic(t *testing.T) {
	r := NewOptoutReconciler(OptoutReconcilerDeps{Tokens: newReconcileStore(), Now: fixedNow})
	r.Stop() // must be a no-op, not a close of a nil channel
	r.Stop()

	// And the reconciler is still usable afterwards: a Stop that arrived
	// before the goroutine must not leave it permanently dead.
	r.Start(context.Background())
	defer r.Stop()
	if !waitFor(func() bool { return r.isRunning() }) {
		t.Fatal("Start after an early Stop must still bring the loop up")
	}
}

func TestReconciler_StopIsIdempotent(t *testing.T) {
	store := newReconcileStore()
	r := NewOptoutReconciler(OptoutReconcilerDeps{Tokens: store, Now: fixedNow, Interval: time.Millisecond})
	r.Start(context.Background())
	if !waitFor(func() bool { return store.scans() > 0 }) {
		t.Fatal("the ticker never ran a pass")
	}
	r.Stop()
	r.Stop()
	if !waitFor(func() bool { return !r.isRunning() }) {
		t.Fatal("Stop must bring the loop down")
	}
}

func TestReconciler_StopHaltsTheTicker(t *testing.T) {
	store := newReconcileStore()
	r := NewOptoutReconciler(OptoutReconcilerDeps{Tokens: store, Now: fixedNow, Interval: time.Millisecond})
	r.Start(context.Background())
	if !waitFor(func() bool { return store.scans() > 0 }) {
		t.Fatal("the ticker never ran a pass")
	}
	r.Stop()
	if !waitFor(func() bool { return !r.isRunning() }) {
		t.Fatal("Stop must bring the loop down")
	}
	settled := store.scans()
	time.Sleep(20 * time.Millisecond) // twenty ticks' worth
	if store.scans() != settled {
		t.Fatalf("scans kept coming after Stop: %d then %d", settled, store.scans())
	}
}

func TestReconciler_StartTwiceRunsOneLoop(t *testing.T) {
	store := newReconcileStore()
	r := NewOptoutReconciler(OptoutReconcilerDeps{Tokens: store, Now: fixedNow, Interval: time.Millisecond})
	r.Start(context.Background())
	r.Start(context.Background())
	if !waitFor(func() bool { return store.scans() > 0 }) {
		t.Fatal("the ticker never ran a pass")
	}
	r.Stop()
	if !waitFor(func() bool { return !r.isRunning() }) {
		t.Fatal("one Stop must bring the loop down — a second Start would have left a goroutine behind")
	}
	settled := store.scans()
	time.Sleep(20 * time.Millisecond)
	if store.scans() != settled {
		t.Fatalf("a second loop survived the Stop: %d then %d", settled, store.scans())
	}
}

func TestReconciler_CancelledContextStopsTheLoop(t *testing.T) {
	store := newReconcileStore()
	ctx, cancel := context.WithCancel(context.Background())
	r := NewOptoutReconciler(OptoutReconcilerDeps{Tokens: store, Now: fixedNow, Interval: time.Millisecond})
	r.Start(ctx)
	if !waitFor(func() bool { return store.scans() > 0 }) {
		t.Fatal("the ticker never ran a pass")
	}
	cancel()
	if !waitFor(func() bool { return !r.isRunning() }) {
		t.Fatal("a cancelled host context must bring the loop down")
	}
	r.Stop() // still safe after the loop exited on its own
}

// TestReconciler_StartWaitsOutARaceWithAFinishingStop reproduces, without
// relying on goroutine scheduling, the exact window a fast Stop followed by a
// Start can land in: `stopped` is set and the stop channel is closed, but the
// loop goroutine's own defer — the only place `running` is cleared — has not
// run yet. A Start arriving here must wait for that generation's `done`
// channel rather than reading the still-`true` `running` flag as "already
// started" and silently declining to launch a replacement.
func TestReconciler_StartWaitsOutARaceWithAFinishingStop(t *testing.T) {
	store := newReconcileStore()
	r := NewOptoutReconciler(OptoutReconcilerDeps{Tokens: store, Now: fixedNow, Interval: time.Millisecond})

	// Hand-place the reconciler in the race window itself, rather than
	// hoping a real Start/Stop pair lands in it.
	done := make(chan struct{})
	r.mu.Lock()
	r.stopCh = make(chan struct{})
	r.done = done
	r.stopped = true
	r.running = true
	r.mu.Unlock()

	started := make(chan struct{})
	go func() {
		r.Start(context.Background())
		close(started)
	}()

	select {
	case <-started:
		t.Fatal("Start returned before the previous generation actually finished")
	case <-time.After(20 * time.Millisecond):
	}

	// Let the "previous generation" finish, the way the loop's own defer
	// would: clear the fields, then close done.
	r.mu.Lock()
	r.running, r.stopCh, r.stopped, r.done = false, nil, false, nil
	r.mu.Unlock()
	close(done)

	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("Start never returned after the previous generation's done channel closed")
	}
	defer r.Stop()
	if !waitFor(func() bool { return r.isRunning() }) {
		t.Fatal("Start must bring a fresh generation up once the previous one is gone")
	}
	if !waitFor(func() bool { return store.scans() > 0 }) {
		t.Fatal("the new generation's ticker never ran a pass")
	}
}

// waitFor polls a condition for up to a second. The reconciler's loop runs on
// a goroutine, so its lifecycle is the one thing here that cannot be asserted
// off an injected clock.
func waitFor(cond func() bool) bool {
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return true
		}
		time.Sleep(time.Millisecond)
	}
	return cond()
}
