package repository

// Mongo-gated tests for the reconciler's side of UnsubscribeRepository:
// ListPending, RecordFailedAttempt and MarkDeadLettered. Same discipline as
// unsubscribe_claim_test.go — skip unless MONGO_TEST_URI is set, a per-run
// ephemeral database, a cleanup that drops only that database.
//
// These run against a real server on purpose: the scan's filter is the part
// that decides whether a row is retried for ever, and its two subtleties —
// "not dead-lettered" and "due now" both have to match documents where the
// field is absent — are exactly what an in-memory fake cannot prove.

import (
	"context"
	"testing"
	"time"

	"github.com/orkestra/backend/internal/core/notification/models"
)

// seedToken stores one token row shaped the way the claim would have left it.
func seedToken(t *testing.T, repo *unsubscribeRepository, doc *models.UnsubscribeTokenDoc) {
	t.Helper()
	if doc.CreatedAt.IsZero() {
		doc.CreatedAt = time.Now()
	}
	if doc.ExpiresAt.IsZero() {
		doc.ExpiresAt = time.Now().Add(time.Hour)
	}
	if err := repo.Create(context.Background(), doc); err != nil {
		t.Fatalf("create %s: %v", doc.UUID, err)
	}
}

func TestMongo_ListPendingReturnsOnlyDueUnfinishedRows(t *testing.T) {
	repo, done := newTestUnsubscribeRepo(t)
	defer done()
	ctx := context.Background()
	now := time.Now()
	used := now.Add(-time.Hour)
	deferred := now.Add(time.Hour)
	due := now.Add(-time.Minute)
	deadLettered := now.Add(-time.Minute)

	seedToken(t, repo, &models.UnsubscribeTokenDoc{UUID: "settled", TokenHash: "settled", Address: "a@x.it", UsedAt: &used})
	seedToken(t, repo, &models.UnsubscribeTokenDoc{UUID: "sink", TokenHash: "sink", Address: "b@x.it", UsedAt: &used, SinkPending: true})
	seedToken(t, repo, &models.UnsubscribeTokenDoc{UUID: "pref", TokenHash: "pref", Address: "c@x.it", UsedAt: &used, PrefPending: true})
	seedToken(t, repo, &models.UnsubscribeTokenDoc{UUID: "retried", TokenHash: "retried", Address: "d@x.it", UsedAt: &used, SinkPending: true, Attempts: 2, NextAttemptAt: &due})
	seedToken(t, repo, &models.UnsubscribeTokenDoc{UUID: "deferred", TokenHash: "deferred", Address: "e@x.it", UsedAt: &used, SinkPending: true, Attempts: 3, NextAttemptAt: &deferred})
	seedToken(t, repo, &models.UnsubscribeTokenDoc{UUID: "dead", TokenHash: "dead", Address: "f@x.it", UsedAt: &used, SinkPending: true, DeadLetteredAt: &deadLettered})

	rows, err := repo.ListPending(ctx, now, 100)
	if err != nil {
		t.Fatalf("ListPending: %v", err)
	}
	got := map[string]bool{}
	for _, r := range rows {
		got[r.UUID] = true
	}
	for _, want := range []string{"sink", "pref", "retried"} {
		if !got[want] {
			t.Fatalf("%q owes work and is due: the scan must return it, got %v", want, got)
		}
	}
	for _, unwanted := range []string{"settled", "deferred", "dead"} {
		if got[unwanted] {
			t.Fatalf("%q must not be scanned, got %v", unwanted, got)
		}
	}
	// The rows come back whole: the reconciler replays from them.
	for _, r := range rows {
		if r.TokenHash == "" || r.Address == "" {
			t.Fatalf("the scan must return the stored document, got %+v", r)
		}
	}
}

func TestMongo_ListPendingRespectsTheLimit(t *testing.T) {
	repo, done := newTestUnsubscribeRepo(t)
	defer done()
	ctx := context.Background()
	now := time.Now()
	used := now.Add(-time.Hour)
	for _, id := range []string{"u1", "u2", "u3"} {
		seedToken(t, repo, &models.UnsubscribeTokenDoc{UUID: id, TokenHash: id, Address: id + "@x.it", UsedAt: &used, SinkPending: true})
	}
	rows, err := repo.ListPending(ctx, now, 2)
	if err != nil {
		t.Fatalf("ListPending: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("a scan must stay bounded: got %d rows for a limit of 2", len(rows))
	}
}

func TestMongo_RecordFailedAttemptCountsAndDefers(t *testing.T) {
	repo, done := newTestUnsubscribeRepo(t)
	defer done()
	ctx := context.Background()
	now := time.Now()
	used := now.Add(-time.Hour)
	seedToken(t, repo, &models.UnsubscribeTokenDoc{UUID: "u1", TokenHash: "h1", Address: "a@x.it", UsedAt: &used, SinkPending: true})

	retryAt := now.Add(2 * time.Minute)
	if err := repo.RecordFailedAttempt(ctx, "h1", retryAt); err != nil {
		t.Fatalf("RecordFailedAttempt: %v", err)
	}
	stored, err := repo.GetByHash(ctx, "h1")
	if err != nil {
		t.Fatalf("GetByHash: %v", err)
	}
	if stored.Attempts != 1 {
		t.Fatalf("attempts = %d, want 1", stored.Attempts)
	}
	if stored.NextAttemptAt == nil || !stored.NextAttemptAt.After(now) {
		t.Fatalf("the row must be deferred, nextAttemptAt = %v", stored.NextAttemptAt)
	}
	if !stored.SinkPending {
		t.Fatal("a failed attempt must not lower the flag it failed on")
	}
	// The count is a $inc, not a write of a value the caller computed: two
	// reconcilers racing the same row must not lose an attempt.
	if err := repo.RecordFailedAttempt(ctx, "h1", now.Add(4*time.Minute)); err != nil {
		t.Fatalf("RecordFailedAttempt: %v", err)
	}
	stored, err = repo.GetByHash(ctx, "h1")
	if err != nil {
		t.Fatalf("GetByHash: %v", err)
	}
	if stored.Attempts != 2 {
		t.Fatalf("attempts = %d, want 2", stored.Attempts)
	}

	// And a deferred row is out of the scan until it is due again.
	rows, err := repo.ListPending(ctx, now, 10)
	if err != nil {
		t.Fatalf("ListPending: %v", err)
	}
	if len(rows) != 0 {
		t.Fatalf("a deferred row must not be scanned yet, got %d", len(rows))
	}
	rows, err = repo.ListPending(ctx, now.Add(10*time.Minute), 10)
	if err != nil {
		t.Fatalf("ListPending: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("once due the row must come back, got %d", len(rows))
	}
}

func TestMongo_MarkDeadLetteredEndsTheRow(t *testing.T) {
	repo, done := newTestUnsubscribeRepo(t)
	defer done()
	ctx := context.Background()
	now := time.Now()
	used := now.Add(-time.Hour)
	deferred := now.Add(-time.Minute)
	seedToken(t, repo, &models.UnsubscribeTokenDoc{
		UUID: "u1", TokenHash: "h1", Address: "a@x.it", UsedAt: &used,
		SinkPending: true, PrefPending: true, Attempts: 7, NextAttemptAt: &deferred,
	})

	if err := repo.MarkDeadLettered(ctx, "h1", now); err != nil {
		t.Fatalf("MarkDeadLettered: %v", err)
	}
	stored, err := repo.GetByHash(ctx, "h1")
	if err != nil {
		t.Fatalf("GetByHash: %v", err)
	}
	if stored.DeadLetteredAt == nil {
		t.Fatal("the row must carry when it was given up on")
	}
	if stored.SinkPending || stored.PrefPending {
		t.Fatalf("a dead-lettered row must not stay pending, it would be scanned for ever: %+v", stored)
	}
	if stored.Attempts != 8 {
		t.Fatalf("attempts = %d, want the failed attempt counted too", stored.Attempts)
	}
	rows, err := repo.ListPending(ctx, now.Add(time.Hour), 10)
	if err != nil {
		t.Fatalf("ListPending: %v", err)
	}
	if len(rows) != 0 {
		t.Fatalf("a dead-lettered row must never be scanned again, got %d", len(rows))
	}
}
