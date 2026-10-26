package erasuretest

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/orkestra/backend/internal/core/compliance/models"
	"github.com/orkestra/backend/internal/core/compliance/repository"
)

func tp(t time.Time) *time.Time { return &t }

func TestMemErasureListOpenNewestFirst(t *testing.T) {
	r := NewMemRepo()
	ctx := context.Background()
	t0 := time.Now().UTC().Add(-24 * time.Hour)
	r.Put(models.ErasureRequest{UUID: "r-old", UserUUID: "u-1", Status: models.ErasureRequestPending, RequestedAt: t0})
	r.Put(models.ErasureRequest{UUID: "r-new", UserUUID: "u-2", Status: models.ErasureRequestIncomplete, RequestedAt: t0.Add(time.Hour)})
	r.Put(models.ErasureRequest{UUID: "r-done", UserUUID: "u-3", Status: models.ErasureRequestCompleted, RequestedAt: t0.Add(2 * time.Hour)})

	got, err := r.ListByStatus(ctx, models.ErasureRequestPending, models.ErasureRequestIncomplete)
	if err != nil {
		t.Fatalf("ListByStatus: %v", err)
	}
	if len(got) != 2 || got[0].UUID != "r-new" || got[1].UUID != "r-old" {
		t.Fatalf("ListByStatus = %+v; want [r-new r-old]", got)
	}
	all, err := r.ListByStatus(ctx)
	if err != nil || len(all) != 3 || all[0].UUID != "r-done" {
		t.Fatalf("ListByStatus() = %+v, %v; want 3 rows newest first", all, err)
	}
}

func TestMemErasureClaimFromPendingSetsModeAndLease(t *testing.T) {
	r := NewMemRepo()
	ctx := context.Background()
	now := time.Now().UTC()
	r.Put(models.ErasureRequest{UUID: "r-1", UserUUID: "u-1", Status: models.ErasureRequestPending, RequestedAt: now})

	lease := now.Add(5 * time.Minute)
	got, err := r.Claim(ctx, "r-1", models.ErasureRequestPending, "anonymize", "admin-1", now, lease)
	if err != nil {
		t.Fatalf("Claim: %v", err)
	}
	if got.Status != models.ErasureRequestIncomplete || got.Mode != "anonymize" || got.ResolvedBy != "admin-1" {
		t.Fatalf("claimed = %+v", got)
	}
	if got.LeaseUntil == nil || !got.LeaseUntil.Equal(lease.Truncate(time.Millisecond)) {
		t.Fatalf("LeaseUntil = %v; want %v", got.LeaseUntil, lease.Truncate(time.Millisecond))
	}
	if _, err := r.Claim(ctx, "r-1", models.ErasureRequestIncomplete, "", "admin-2", now, lease); !errors.Is(err, repository.ErrErasureRequestBusy) {
		t.Fatalf("second Claim err = %v; want repository.ErrErasureRequestBusy", err)
	}
	if _, err := r.Claim(ctx, "r-1", models.ErasureRequestPending, "hard_delete", "admin-2", now, lease); !errors.Is(err, repository.ErrErasureRequestNotFound) {
		t.Fatalf("Claim pending on incomplete err = %v; want repository.ErrErasureRequestNotFound", err)
	}
}

func TestMemErasureClaimDueSkipsLiveLeaseAndFutureAttempts(t *testing.T) {
	r := NewMemRepo()
	ctx := context.Background()
	now := time.Now().UTC()
	inc := models.ErasureRequestIncomplete
	r.Put(models.ErasureRequest{UUID: "a", UserUUID: "u", Status: inc, RequestedAt: now, NextAttemptAt: tp(now.Add(-time.Minute))})
	r.Put(models.ErasureRequest{UUID: "b", UserUUID: "u", Status: inc, RequestedAt: now, NextAttemptAt: tp(now.Add(-2 * time.Minute)), LeaseUntil: tp(now.Add(time.Minute))})
	r.Put(models.ErasureRequest{UUID: "c", UserUUID: "u", Status: inc, RequestedAt: now, NextAttemptAt: tp(now.Add(time.Minute))})
	r.Put(models.ErasureRequest{UUID: "d", UserUUID: "u", Status: inc, RequestedAt: now})

	lease := now.Add(5 * time.Minute)
	got, err := r.ClaimDue(ctx, now, lease)
	if err != nil || got == nil || got.UUID != "a" {
		t.Fatalf("ClaimDue 1 = %+v, %v; want a", got, err)
	}
	got, err = r.ClaimDue(ctx, now, lease)
	if err != nil || got != nil {
		t.Fatalf("ClaimDue 2 = %+v, %v; want nil", got, err)
	}
	later := now.Add(2 * time.Minute)
	got, err = r.ClaimDue(ctx, later, later.Add(5*time.Minute))
	if err != nil || got == nil || got.UUID != "b" {
		t.Fatalf("ClaimDue after lease expiry = %+v, %v; want b", got, err)
	}
}

func TestMemErasureSettleIsFencedByLease(t *testing.T) {
	r := NewMemRepo()
	ctx := context.Background()
	now := time.Now().UTC()
	r.Put(models.ErasureRequest{UUID: "r-1", UserUUID: "u-1", Status: models.ErasureRequestPending, RequestedAt: now})

	l1 := now.Add(5 * time.Minute).Truncate(time.Millisecond)
	if _, err := r.Claim(ctx, "r-1", models.ErasureRequestPending, "anonymize", "admin-1", now, l1); err != nil {
		t.Fatalf("Claim: %v", err)
	}
	// The row needs a due attempt for the worker to take it over once L1 expires.
	row0 := r.Row("r-1")
	row0.NextAttemptAt = &now
	r.Put(row0)
	later := l1.Add(time.Second)
	l2 := later.Add(5 * time.Minute).Truncate(time.Millisecond)
	taken, err := r.ClaimDue(ctx, later, l2)
	if err != nil || taken == nil {
		t.Fatalf("ClaimDue = %+v, %v", taken, err)
	}

	done := repository.ErasureSettlement{Status: models.ErasureRequestCompleted, ResolvedAt: &later}
	if err := r.Settle(ctx, "r-1", l1, done); !errors.Is(err, repository.ErrErasureLeaseLost) {
		t.Fatalf("Settle with stale lease err = %v; want repository.ErrErasureLeaseLost", err)
	}
	row := r.Row("r-1")
	if row.Status != models.ErasureRequestIncomplete || row.LeaseUntil == nil {
		t.Fatalf("row changed by a lost lease: %+v", row)
	}
	if err := r.Settle(ctx, "r-1", l2, done); err != nil {
		t.Fatalf("Settle: %v", err)
	}
	row = r.Row("r-1")
	if row.Status != models.ErasureRequestCompleted || row.LeaseUntil != nil || row.ResolvedAt == nil {
		t.Fatalf("settled row = %+v", row)
	}
}

func TestMemErasureHasIncomplete(t *testing.T) {
	r := NewMemRepo()
	ctx := context.Background()
	now := time.Now().UTC()
	r.Put(models.ErasureRequest{UUID: "r-1", UserUUID: "u-1", Status: models.ErasureRequestIncomplete, RequestedAt: now})
	r.Put(models.ErasureRequest{UUID: "r-2", UserUUID: "u-2", Status: models.ErasureRequestCompleted, RequestedAt: now})
	if ok, err := r.HasIncomplete(ctx, "u-1"); err != nil || !ok {
		t.Fatalf("HasIncomplete u-1 = %v, %v; want true", ok, err)
	}
	if ok, err := r.HasIncomplete(ctx, "u-2"); err != nil || ok {
		t.Fatalf("HasIncomplete u-2 = %v, %v; want false", ok, err)
	}
}
