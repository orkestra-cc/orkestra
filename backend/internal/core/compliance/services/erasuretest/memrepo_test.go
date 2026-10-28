package erasuretest

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/orkestra/backend/internal/core/compliance/models"
	"github.com/orkestra/backend/internal/core/compliance/repository"
)

const maxAttempts = 20 // services.ErasureRetryMaxAttempts

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
	if got.Status != models.ErasureRequestIncomplete || got.Mode != "anonymize" || got.ResolvedBy != "admin-1" || got.Attempts != 1 {
		t.Fatalf("claimed = %+v; want incomplete, anonymize, admin-1, attempts 1", got)
	}
	if got.LeaseUntil == nil || !got.LeaseUntil.Equal(lease.Truncate(time.Millisecond)) {
		t.Fatalf("LeaseUntil = %v; want %v", got.LeaseUntil, lease.Truncate(time.Millisecond))
	}
	if got.NextAttemptAt == nil || !got.NextAttemptAt.Equal(*got.LeaseUntil) {
		t.Fatalf("NextAttemptAt = %v; want the lease %v", got.NextAttemptAt, got.LeaseUntil)
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
	got, err := r.ClaimDue(ctx, now, lease, maxAttempts)
	if err != nil || got == nil || got.UUID != "a" {
		t.Fatalf("ClaimDue 1 = %+v, %v; want a", got, err)
	}
	got, err = r.ClaimDue(ctx, now, lease, maxAttempts)
	if err != nil || got != nil {
		t.Fatalf("ClaimDue 2 = %+v, %v; want nil", got, err)
	}
	later := now.Add(2 * time.Minute)
	got, err = r.ClaimDue(ctx, later, later.Add(5*time.Minute), maxAttempts)
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
	later := l1.Add(time.Second)
	l2 := later.Add(5 * time.Minute).Truncate(time.Millisecond)
	taken, err := r.ClaimDue(ctx, later, l2, maxAttempts)
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

func TestMemErasureSettleIncompleteThenCompleted(t *testing.T) {
	r := NewMemRepo()
	ctx := context.Background()
	now := time.Now().UTC()
	r.Put(models.ErasureRequest{UUID: "r-1", UserUUID: "u-1", Status: models.ErasureRequestPending, RequestedAt: now})
	lease := now.Add(5 * time.Minute)
	if _, err := r.Claim(ctx, "r-1", models.ErasureRequestPending, "anonymize", "admin-1", now, lease); err != nil {
		t.Fatalf("Claim: %v", err)
	}
	next := now.Add(time.Minute)
	prog := []models.ErasureProducerProgress{{Key: "auth", Subject: "auth", Outcome: models.ProducerOutcomePending, RowsDeleted: 3, Attempts: 1, UpdatedAt: now}}
	err := r.Settle(ctx, "r-1", lease, repository.ErasureSettlement{Status: models.ErasureRequestIncomplete, Progress: prog, Attempts: 1, NextAttemptAt: &next, LastError: models.ErasureLastErrorRun})
	if err != nil {
		t.Fatalf("Settle incomplete: %v", err)
	}
	row := r.Row("r-1")
	if row.Status != models.ErasureRequestIncomplete || len(row.Progress) != 1 || row.Progress[0].RowsDeleted != 3 || row.Attempts != 1 ||
		row.NextAttemptAt == nil || !row.NextAttemptAt.Equal(next.Truncate(time.Millisecond)) || row.LastError != models.ErasureLastErrorRun || row.LeaseUntil != nil {
		t.Fatalf("incomplete row = %+v", row)
	}

	l2 := next.Add(5 * time.Minute)
	if got, err := r.ClaimDue(ctx, next.Add(time.Second), l2, maxAttempts); err != nil || got == nil {
		t.Fatalf("ClaimDue = %+v, %v", got, err)
	}
	if err := r.Settle(ctx, "r-1", l2, repository.ErasureSettlement{Status: models.ErasureRequestCompleted, Progress: prog, Attempts: 2, ResolvedAt: &next}); err != nil {
		t.Fatalf("Settle completed: %v", err)
	}
	row = r.Row("r-1")
	if row.Status != models.ErasureRequestCompleted || row.NextAttemptAt != nil || row.LastError != "" || row.ResolvedAt == nil || row.LeaseUntil != nil {
		t.Fatalf("completed row = %+v", row)
	}
}

func TestMemErasureClaimFromIncompleteKeepsModeAndResets(t *testing.T) {
	r := NewMemRepo()
	ctx := context.Background()
	now := time.Now().UTC()
	r.Put(models.ErasureRequest{UUID: "r-1", UserUUID: "u-1", Status: models.ErasureRequestIncomplete, RequestedAt: now,
		Mode: "anonymize", ResolvedBy: "admin-1", Attempts: 20, LastError: models.ErasureLastErrorRun, LeaseUntil: tp(now.Add(-time.Minute))})
	got, err := r.Claim(ctx, "r-1", models.ErasureRequestIncomplete, "ignored", "admin-2", now, now.Add(5*time.Minute))
	if err != nil {
		t.Fatalf("Claim: %v", err)
	}
	if got.Mode != "anonymize" || got.ResolvedBy != "admin-1" || got.Attempts != 1 || got.LastError != "" || got.Status != models.ErasureRequestIncomplete {
		t.Fatalf("claimed = %+v", got)
	}
}

func TestMemErasureInsertDuplicateAndInjectedFailure(t *testing.T) {
	r := NewMemRepo()
	ctx := context.Background()
	req := models.ErasureRequest{UUID: "r-1", UserUUID: "u-1", Status: models.ErasureRequestPending}
	if err := r.Insert(ctx, &req); err != nil {
		t.Fatalf("Insert: %v", err)
	}
	if err := r.Insert(ctx, &req); err == nil {
		t.Fatal("duplicate Insert succeeded; want an error like the unique uuid index")
	}
	boom := errors.New("boom")
	r.FailNextInsert(boom)
	other := models.ErasureRequest{UUID: "r-2"}
	if err := r.Insert(ctx, &other); !errors.Is(err, boom) {
		t.Fatalf("Insert err = %v; want boom", err)
	}
	if err := r.Insert(ctx, &other); err != nil {
		t.Fatalf("Insert after consumed failure: %v", err)
	}
}

func TestMemErasureClosePartialOnlyIncompleteAndLeaseFree(t *testing.T) {
	r := NewMemRepo()
	ctx := context.Background()
	now := time.Now().UTC()
	next := now.Add(time.Hour)
	r.Put(models.ErasureRequest{UUID: "r-1", UserUUID: "u-1", Status: models.ErasureRequestIncomplete, RequestedAt: now, NextAttemptAt: &next})
	r.Put(models.ErasureRequest{UUID: "r-2", UserUUID: "u-2", Status: models.ErasureRequestIncomplete, RequestedAt: now, LeaseUntil: tp(now.Add(time.Minute))})
	r.Put(models.ErasureRequest{UUID: "r-3", UserUUID: "u-3", Status: models.ErasureRequestPending, RequestedAt: now})

	if err := r.ClosePartial(ctx, "r-1", now, []string{"gone"}, "admin-1", "module removed"); err != nil {
		t.Fatalf("ClosePartial: %v", err)
	}
	row := r.Row("r-1")
	if row.Status != models.ErasureRequestClosedPartial || len(row.Abandoned) != 1 || row.Abandoned[0] != "gone" ||
		row.ResolvedBy != "admin-1" || row.ResolutionNote != "module removed" || row.NextAttemptAt != nil || row.ResolvedAt == nil {
		t.Fatalf("closed row = %+v", row)
	}
	if err := r.ClosePartial(ctx, "r-2", now, []string{"gone"}, "admin-1", "n"); !errors.Is(err, repository.ErrErasureRequestBusy) {
		t.Fatalf("leased err = %v; want ErrErasureRequestBusy", err)
	}
	if err := r.ClosePartial(ctx, "r-3", now, []string{"gone"}, "admin-1", "n"); !errors.Is(err, repository.ErrErasureRequestNotFound) {
		t.Fatalf("pending err = %v; want ErrErasureRequestNotFound", err)
	}
	if err := r.ClosePartial(ctx, "r-1", now, []string{"gone"}, "admin-1", "n"); !errors.Is(err, repository.ErrErasureRequestNotFound) {
		t.Fatalf("already closed err = %v; want ErrErasureRequestNotFound", err)
	}
}

func TestMemErasureClaimDueCountsAttemptsAndStopsAtMax(t *testing.T) {
	r := NewMemRepo()
	ctx := context.Background()
	now := time.Now().UTC()
	r.Put(models.ErasureRequest{UUID: "r-1", UserUUID: "u-1", Status: models.ErasureRequestIncomplete, RequestedAt: now, NextAttemptAt: tp(now.Add(-time.Second))})

	// A run that dies before Settle still spends its attempt: every claim counts.
	for i := 1; i <= maxAttempts; i++ {
		got, err := r.ClaimDue(ctx, now, now.Add(5*time.Minute), maxAttempts)
		if err != nil || got == nil || got.Attempts != i {
			t.Fatalf("ClaimDue %d = %+v, %v; want attempts %d", i, got, err, i)
		}
		now = now.Add(5*time.Minute + time.Second) // the lease expires without a Settle
	}
	got, err := r.ClaimDue(ctx, now, now.Add(5*time.Minute), maxAttempts)
	if err != nil || got != nil {
		t.Fatalf("ClaimDue after %d unsettled claims = %+v, %v; want nil", maxAttempts, got, err)
	}
	row := r.Row("r-1")
	if row.Status != models.ErasureRequestIncomplete || row.Attempts != maxAttempts {
		t.Fatalf("exhausted row = %+v", row)
	}
}

func TestMemErasureClosePartialReleasesLeaseAndRefusesStaleSettle(t *testing.T) {
	r := NewMemRepo()
	ctx := context.Background()
	now := time.Now().UTC()
	r.Put(models.ErasureRequest{UUID: "r-1", UserUUID: "u-1", Status: models.ErasureRequestPending, RequestedAt: now})
	lease := now.Add(5 * time.Minute)
	if _, err := r.Claim(ctx, "r-1", models.ErasureRequestPending, "anonymize", "admin-1", now, lease); err != nil {
		t.Fatalf("Claim: %v", err)
	}
	later := lease.Add(time.Second) // the run died; its lease expired
	if err := r.ClosePartial(ctx, "r-1", later, []string{"gone"}, "admin-1", "module removed"); err != nil {
		t.Fatalf("ClosePartial: %v", err)
	}
	row := r.Row("r-1")
	if row.Status != models.ErasureRequestClosedPartial || row.LeaseUntil != nil {
		t.Fatalf("closed row = %+v; want closed_partial without a lease", row)
	}
	stale := repository.ErasureSettlement{Status: models.ErasureRequestCompleted, Attempts: 1, ResolvedAt: &later}
	if err := r.Settle(ctx, "r-1", lease, stale); !errors.Is(err, repository.ErrErasureLeaseLost) {
		t.Fatalf("stale Settle err = %v; want ErrErasureLeaseLost", err)
	}
	row = r.Row("r-1")
	if row.Status != models.ErasureRequestClosedPartial {
		t.Fatalf("a stale Settle reopened the row: %+v", row)
	}
}

func TestMemErasureRecordProgressIsFencedByLeaseAndKeepsIt(t *testing.T) {
	r := NewMemRepo()
	ctx := context.Background()
	now := time.Now().UTC()
	r.Put(models.ErasureRequest{UUID: "r-1", UserUUID: "u-1", Status: models.ErasureRequestPending, RequestedAt: now})
	plan := []models.ErasureProducerProgress{{Key: "crm", Subject: "crm", Outcome: models.ProducerOutcomePending}}

	l1 := now.Add(5 * time.Minute).Truncate(time.Millisecond)
	if err := r.RecordProgress(ctx, "r-1", l1, plan); !errors.Is(err, repository.ErrErasureLeaseLost) {
		t.Fatalf("RecordProgress on a pending row err = %v; want ErrErasureLeaseLost", err)
	}
	if _, err := r.Claim(ctx, "r-1", models.ErasureRequestPending, "anonymize", "admin-1", now, l1); err != nil {
		t.Fatalf("Claim: %v", err)
	}
	if err := r.RecordProgress(ctx, "r-1", l1.Add(time.Second), plan); !errors.Is(err, repository.ErrErasureLeaseLost) {
		t.Fatalf("RecordProgress with another lease err = %v; want ErrErasureLeaseLost", err)
	}
	if err := r.RecordProgress(ctx, "r-1", l1, plan); err != nil {
		t.Fatalf("RecordProgress: %v", err)
	}
	if row := r.Row("r-1"); row.LeaseUntil == nil || !row.LeaseUntil.Equal(l1) || len(row.Progress) != 1 || row.Progress[0].Key != "crm" {
		t.Fatalf("row = %+v; want crm pending under the same lease", row)
	}
}

func TestMemErasureInsertOneActiveRetentionRequestPerSubject(t *testing.T) {
	r := NewMemRepo()
	ctx := context.Background()
	retention := func(id string) *models.ErasureRequest {
		return &models.ErasureRequest{UUID: id, UserUUID: "u-1", Status: models.ErasureRequestIncomplete, Source: models.ErasureSourceRetention}
	}
	if err := r.Insert(ctx, retention("a")); err != nil {
		t.Fatalf("Insert a: %v", err)
	}
	if err := r.Insert(ctx, retention("b")); !errors.Is(err, repository.ErrErasureRequestActive) {
		t.Fatalf("second retention Insert err = %v; want ErrErasureRequestActive", err)
	}
	other := &models.ErasureRequest{UUID: "c", UserUUID: "u-1", Status: models.ErasureRequestIncomplete, Source: models.ErasureSourceSelfService}
	if err := r.Insert(ctx, other); err != nil {
		t.Fatalf("self-service Insert next to a retention request: %v", err)
	}
	r.Put(models.ErasureRequest{UUID: "a", UserUUID: "u-1", Status: models.ErasureRequestCompleted, Source: models.ErasureSourceRetention})
	if err := r.Insert(ctx, retention("d")); err != nil {
		t.Fatalf("retention Insert after the first completed: %v", err)
	}
}
