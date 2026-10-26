package services

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/orkestra/backend/internal/core/compliance/models"
	"github.com/orkestra/backend/internal/core/compliance/repository"
	"github.com/orkestra/backend/internal/core/compliance/services/erasuretest"
	"github.com/orkestra/backend/pkg/sdk/iface"
)

var erasureT0 = time.Date(2026, 10, 26, 12, 0, 0, 0, time.UTC)

func newErasureSvc(repo *erasuretest.MemRepo, producers ...iface.PIIProducer) (*ErasureRequestService, *DSRService) {
	dsr, _ := newDSRService(producers...)
	svc := NewErasureRequestService(repo, dsr, nil, nil)
	svc.now = func() time.Time { return erasureT0 }
	return svc, dsr
}

func done(key string) models.ErasureProducerProgress {
	return models.ErasureProducerProgress{Key: key, Subject: key, Outcome: models.ProducerOutcomeDone, Attempts: 1}
}

func failed(key string) models.ErasureProducerProgress {
	return models.ErasureProducerProgress{Key: key, Subject: key, Outcome: models.ProducerOutcomeFailed, Attempts: 1}
}

func ptrTime(t time.Time) *time.Time { return &t }

// TestLodgeCreatesPendingRequest pins the Lodge contract: the service stamps a
// UUID + RequestedAt, defaults the status to pending, and forwards the subject
// fields verbatim.
func TestLodgeCreatesPendingRequest(t *testing.T) {
	t.Parallel()

	repo := erasuretest.NewMemRepo()
	dsr, _ := newDSRService()
	svc := NewErasureRequestService(repo, dsr, nil, nil)

	got, err := svc.Lodge(context.Background(), "u-1", "t-1", "no longer a customer")
	if err != nil {
		t.Fatalf("Lodge: %v", err)
	}
	if repo.Row(got.UUID).UUID != got.UUID {
		t.Fatal("Lodge should insert a request")
	}
	if got.UUID == "" || got.RequestedAt.IsZero() {
		t.Fatalf("Lodge should stamp UUID + RequestedAt: %+v", got)
	}
	if got.Status != models.ErasureRequestPending {
		t.Fatalf("new request status = %q; want pending", got.Status)
	}
	if got.UserUUID != "u-1" || got.TenantID != "t-1" || got.Reason != "no longer a customer" {
		t.Fatalf("request fields not forwarded: %+v", got)
	}
}

func TestErasureRetryBackoff(t *testing.T) {
	t.Parallel()
	cases := map[int]time.Duration{
		1: time.Minute, 2: 2 * time.Minute, 9: 256 * time.Minute, 10: 6 * time.Hour, 20: 6 * time.Hour,
	}
	for attempts, want := range cases {
		if got := erasureRetryBackoff(attempts); got != want {
			t.Errorf("erasureRetryBackoff(%d) = %v; want %v", attempts, got, want)
		}
	}
}

func TestExecuteCompletesWhenEveryProducerDone(t *testing.T) {
	t.Parallel()
	repo := erasuretest.NewMemRepo()
	repo.Put(models.ErasureRequest{UUID: "r-1", UserUUID: "u-9", Status: models.ErasureRequestPending, RequestedAt: erasureT0})
	user := &stubProducer{subject: "user", purgeResult: iface.PurgeResult{RowsDeleted: 1}}
	auth := &stubProducer{subject: "auth"}
	svc, _ := newErasureSvc(repo, user, auth)

	out, err := svc.Execute(context.Background(), "r-1", iface.EraseHardDelete, "admin-1")
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if out.Request.Status != models.ErasureRequestCompleted || out.Run.Purged["user"].RowsDeleted != 1 {
		t.Fatalf("outcome wrong: %+v / %+v", out.Request, out.Run)
	}
	row := repo.Row("r-1")
	if row.Status != models.ErasureRequestCompleted || row.ResolvedAt == nil || row.ResolvedBy != "admin-1" ||
		row.Mode != "hard_delete" || len(row.Progress) != 2 || row.LeaseUntil != nil || row.NextAttemptAt != nil {
		t.Fatalf("settled row wrong: %+v", row)
	}
}

func TestExecutePendingProducerLeavesIncomplete(t *testing.T) {
	t.Parallel()
	repo := erasuretest.NewMemRepo()
	repo.Put(models.ErasureRequest{UUID: "r-1", UserUUID: "u-9", Status: models.ErasureRequestPending})
	svc, _ := newErasureSvc(repo,
		&stubProducer{subject: "user"},
		&stubProducer{subject: "retrieval", purgeResult: iface.PurgeResult{Pending: true}})

	out, err := svc.Execute(context.Background(), "r-1", iface.EraseHardDelete, "admin-1")
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	row := repo.Row("r-1")
	if out.Request.Status != models.ErasureRequestIncomplete || row.Status != models.ErasureRequestIncomplete ||
		row.Attempts != 1 || row.LastError != "" || row.ResolvedAt != nil ||
		row.NextAttemptAt == nil || !row.NextAttemptAt.Equal(erasureT0.Add(time.Minute)) {
		t.Fatalf("row wrong: %+v", row)
	}
}

func TestExecutePendingUnderLegalHoldStaysPending(t *testing.T) {
	t.Parallel()
	repo := erasuretest.NewMemRepo()
	repo.Put(models.ErasureRequest{UUID: "r-1", UserUUID: "u-held", Status: models.ErasureRequestPending})
	user := &stubProducer{subject: "user"}
	svc, dsr := newErasureSvc(repo, user)
	dsr.SetLegalHoldChecker(&fakeHoldChecker{held: true})

	_, err := svc.Execute(context.Background(), "r-1", iface.EraseHardDelete, "admin-1")
	if !errors.Is(err, ErrLegalHoldActive) {
		t.Fatalf("expected ErrLegalHoldActive, got %v", err)
	}
	if repo.Row("r-1").Status != models.ErasureRequestPending || user.purgeCalls != 0 {
		t.Fatalf("held request must stay pending untouched: %+v purge=%d", repo.Row("r-1"), user.purgeCalls)
	}
}

func TestExecuteRejectsResolvedRequests(t *testing.T) {
	t.Parallel()
	for _, status := range []string{models.ErasureRequestCompleted, models.ErasureRequestRejected} {
		repo := erasuretest.NewMemRepo()
		repo.Put(models.ErasureRequest{UUID: "r-1", Status: status})
		user := &stubProducer{subject: "user"}
		svc, _ := newErasureSvc(repo, user)
		_, err := svc.Execute(context.Background(), "r-1", iface.EraseHardDelete, "admin-1")
		if !errors.Is(err, repository.ErrErasureRequestNotFound) || user.purgeCalls != 0 {
			t.Fatalf("%s: err=%v purge=%d", status, err, user.purgeCalls)
		}
	}
}

func TestExecuteIncompleteRunsOnlyRemainingWithStoredMode(t *testing.T) {
	t.Parallel()
	repo := erasuretest.NewMemRepo()
	repo.Put(models.ErasureRequest{
		UUID: "r-1", UserUUID: "u-9", Status: models.ErasureRequestIncomplete, Mode: "anonymize",
		Progress: []models.ErasureProducerProgress{done("user"), failed("auth")},
	})
	user := &stubProducer{subject: "user"}
	auth := &stubProducer{subject: "auth"}
	svc, _ := newErasureSvc(repo, user, auth)

	out, err := svc.Execute(context.Background(), "r-1", iface.EraseHardDelete, "admin-2")
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if user.purgeCalls != 0 || auth.purgeCalls != 1 || auth.lastMode != iface.EraseAnonymize {
		t.Fatalf("user=%d auth=%d mode=%v", user.purgeCalls, auth.purgeCalls, auth.lastMode)
	}
	if out.Request.Status != models.ErasureRequestCompleted || repo.Row("r-1").Mode != "anonymize" {
		t.Fatalf("row wrong: %+v", repo.Row("r-1"))
	}
}

func TestExecuteIncompleteResetsAttempts(t *testing.T) {
	t.Parallel()
	repo := erasuretest.NewMemRepo()
	repo.Put(models.ErasureRequest{
		UUID: "r-1", UserUUID: "u-9", Status: models.ErasureRequestIncomplete, Mode: "hard_delete",
		Attempts: 20, Progress: []models.ErasureProducerProgress{failed("user")},
	})
	svc, _ := newErasureSvc(repo, &stubProducer{subject: "user", purgeErr: errors.New("down")})

	if _, err := svc.Execute(context.Background(), "r-1", iface.EraseHardDelete, "admin-2"); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	row := repo.Row("r-1")
	if row.Attempts != 1 || row.NextAttemptAt == nil || !row.NextAttemptAt.Equal(erasureT0.Add(time.Minute)) {
		t.Fatalf("attempt budget not restarted: %+v", row)
	}
}

func TestExecuteBusyWhileLeaseHeld(t *testing.T) {
	t.Parallel()
	repo := erasuretest.NewMemRepo()
	repo.Put(models.ErasureRequest{
		UUID: "r-1", UserUUID: "u-9", Status: models.ErasureRequestIncomplete,
		LeaseUntil: ptrTime(erasureT0.Add(time.Minute)),
	})
	user := &stubProducer{subject: "user"}
	svc, _ := newErasureSvc(repo, user)

	_, err := svc.Execute(context.Background(), "r-1", iface.EraseHardDelete, "admin-1")
	if !errors.Is(err, repository.ErrErasureRequestBusy) || user.purgeCalls != 0 {
		t.Fatalf("err=%v purge=%d", err, user.purgeCalls)
	}
}

func TestRejectIncompleteIsRefused(t *testing.T) {
	t.Parallel()
	repo := erasuretest.NewMemRepo()
	repo.Put(models.ErasureRequest{UUID: "r-1", Status: models.ErasureRequestIncomplete})
	svc, _ := newErasureSvc(repo)

	err := svc.Reject(context.Background(), "r-1", "admin-2", "no")
	if !errors.Is(err, ErrErasureRequestStarted) || repo.Row("r-1").Status != models.ErasureRequestIncomplete {
		t.Fatalf("err=%v row=%+v", err, repo.Row("r-1"))
	}
}

func TestRejectResolvesRejected(t *testing.T) {
	t.Parallel()
	repo := erasuretest.NewMemRepo()
	repo.Put(models.ErasureRequest{UUID: "r-1", Status: models.ErasureRequestPending})
	svc, _ := newErasureSvc(repo)

	if err := svc.Reject(context.Background(), "r-1", "admin-2", "insufficient identity proof"); err != nil {
		t.Fatalf("Reject: %v", err)
	}
	row := repo.Row("r-1")
	if row.Status != models.ErasureRequestRejected || row.ResolutionNote != "insufficient identity proof" || row.ResolvedBy != "admin-2" {
		t.Fatalf("row wrong: %+v", row)
	}
}

func TestListOpenReturnsPendingAndIncomplete(t *testing.T) {
	t.Parallel()
	repo := erasuretest.NewMemRepo()
	for i, st := range []string{"pending", "incomplete", "completed", "rejected"} {
		repo.Put(models.ErasureRequest{UUID: "r-" + st, Status: st, RequestedAt: erasureT0.Add(time.Duration(i) * time.Second)})
	}
	svc, _ := newErasureSvc(repo)
	got, err := svc.ListOpen(context.Background())
	if err != nil || len(got) != 2 {
		t.Fatalf("ListOpen = %d items, err %v", len(got), err)
	}
}

func TestEraseNowRecordsTheRunBeforeProducers(t *testing.T) {
	t.Parallel()
	repo := erasuretest.NewMemRepo()
	user := &stubProducer{subject: "user"}
	svc, _ := newErasureSvc(repo, user)
	var seen []models.ErasureRequest
	user.onPurge = func() {
		seen, _ = repo.ListByStatus(context.Background(), models.ErasureRequestIncomplete)
	}

	out, err := svc.EraseNow(context.Background(), "u-1", "t-1", models.ErasureSourceSelfService, "u-1", iface.EraseHardDelete)
	if err != nil {
		t.Fatalf("EraseNow: %v", err)
	}
	if len(seen) != 1 || seen[0].UserUUID != "u-1" || seen[0].Source != "self_service" ||
		seen[0].LeaseUntil == nil || !seen[0].LeaseUntil.After(erasureT0) || seen[0].Attempts != 1 {
		t.Fatalf("row not recorded before the producer ran: %+v", seen)
	}
	row := repo.Row(out.Request.UUID)
	if row.Status != models.ErasureRequestCompleted || row.Source != "self_service" || row.ResolvedBy != "u-1" || row.TenantID != "t-1" {
		t.Fatalf("final row wrong: %+v", row)
	}
}

func TestEraseNowInsertFailureErasesNothing(t *testing.T) {
	t.Parallel()
	repo := erasuretest.NewMemRepo()
	repo.FailNextInsert(errors.New("mongo down"))
	user := &stubProducer{subject: "user"}
	svc, _ := newErasureSvc(repo, user)

	if _, err := svc.EraseNow(context.Background(), "u-1", "t-1", "self_service", "u-1", iface.EraseHardDelete); err == nil {
		t.Fatal("expected the insert error")
	}
	if user.purgeCalls != 0 {
		t.Fatalf("nothing may be erased; purge=%d", user.purgeCalls)
	}
}

func TestEraseNowUnderLegalHoldRecordsNothing(t *testing.T) {
	t.Parallel()
	repo := erasuretest.NewMemRepo()
	svc, dsr := newErasureSvc(repo, &stubProducer{subject: "user"})
	dsr.SetLegalHoldChecker(&fakeHoldChecker{held: true})

	_, err := svc.EraseNow(context.Background(), "u-held", "t-1", "self_service", "u-held", iface.EraseHardDelete)
	if !errors.Is(err, ErrLegalHoldActive) {
		t.Fatalf("expected ErrLegalHoldActive, got %v", err)
	}
	if rows, _ := repo.ListByStatus(context.Background()); len(rows) != 0 {
		t.Fatalf("a held subject must record nothing: %+v", rows)
	}
}

func TestEraseNowSettleFailureIsRecoveredByRetry(t *testing.T) {
	t.Parallel()
	repo := erasuretest.NewMemRepo()
	repo.FailNextSettle(errors.New("mongo blip"))
	user := &stubProducer{subject: "user"}
	auth := &stubProducer{subject: "auth", purgeErrs: []error{errors.New("boom")}}
	svc, _ := newErasureSvc(repo, user, auth)

	out, err := svc.EraseNow(context.Background(), "u-1", "t-1", "self_service", "u-1", iface.EraseHardDelete)
	if err != nil {
		t.Fatalf("EraseNow: %v", err)
	}
	if out.Run.Errors["auth"] == "" {
		t.Fatalf("run should report the auth error: %+v", out.Run)
	}
	row := repo.Row(out.Request.UUID)
	if row.Status != models.ErasureRequestIncomplete || row.LeaseUntil == nil {
		t.Fatalf("row should stay incomplete under its lease: %+v", row)
	}
	if ok, err := svc.RetryDue(context.Background()); ok || err != nil {
		t.Fatalf("RetryDue while the lease is live = %v, %v", ok, err)
	}
	svc.now = func() time.Time { return erasureT0.Add(erasureRunLease + time.Second) }
	if ok, err := svc.RetryDue(context.Background()); !ok || err != nil {
		t.Fatalf("RetryDue after the lease = %v, %v", ok, err)
	}
	if got := repo.Row(out.Request.UUID); got.Status != models.ErasureRequestCompleted {
		t.Fatalf("row should complete: %+v", got)
	}
}

func TestRetryDueReclaimsExpiredLeaseAndRunsOnlyRemaining(t *testing.T) {
	t.Parallel()
	repo := erasuretest.NewMemRepo()
	repo.Put(models.ErasureRequest{
		UUID: "r-1", UserUUID: "u-9", Status: models.ErasureRequestIncomplete, Mode: "hard_delete",
		LeaseUntil: ptrTime(erasureT0.Add(-time.Second)), NextAttemptAt: ptrTime(erasureT0.Add(-time.Second)),
		Progress: []models.ErasureProducerProgress{done("user"), failed("auth.client")},
	})
	user := &stubProducer{subject: "user"}
	client := &stubProducer{subject: "auth"}
	svc, _ := newErasureSvc(repo, user, keyedStub{client, "auth.client"})

	ok, err := svc.RetryDue(context.Background())
	if !ok || err != nil {
		t.Fatalf("RetryDue = %v, %v", ok, err)
	}
	if user.purgeCalls != 0 || client.purgeCalls != 1 || repo.Row("r-1").Status != models.ErasureRequestCompleted {
		t.Fatalf("user=%d client=%d row=%+v", user.purgeCalls, client.purgeCalls, repo.Row("r-1"))
	}
}

func TestRetryDueStopsAtMaxAttemptsAndStaysIncomplete(t *testing.T) {
	t.Parallel()
	repo := erasuretest.NewMemRepo()
	repo.Put(models.ErasureRequest{
		UUID: "r-1", UserUUID: "u-9", Status: models.ErasureRequestIncomplete, Mode: "hard_delete",
		Attempts: ErasureRetryMaxAttempts - 1, NextAttemptAt: ptrTime(erasureT0.Add(-time.Second)),
	})
	svc, _ := newErasureSvc(repo, &stubProducer{subject: "user", purgeResult: iface.PurgeResult{Pending: true}})

	if ok, err := svc.RetryDue(context.Background()); !ok || err != nil {
		t.Fatalf("RetryDue = %v, %v", ok, err)
	}
	row := repo.Row("r-1")
	if row.Status != models.ErasureRequestIncomplete || row.Attempts != ErasureRetryMaxAttempts || row.NextAttemptAt != nil || row.ResolvedAt != nil {
		t.Fatalf("row wrong: %+v", row)
	}
	if ok, _ := svc.RetryDue(context.Background()); ok {
		t.Fatal("an exhausted request must not be retried automatically")
	}
	open, _ := svc.ListOpen(context.Background())
	if len(open) != 1 || open[0].UUID != "r-1" {
		t.Fatalf("ListOpen = %+v", open)
	}
}

func TestRetryDueUnderLegalHoldStaysIncomplete(t *testing.T) {
	t.Parallel()
	repo := erasuretest.NewMemRepo()
	prog := []models.ErasureProducerProgress{failed("user")}
	repo.Put(models.ErasureRequest{
		UUID: "r-1", UserUUID: "u-held", Status: models.ErasureRequestIncomplete, Mode: "hard_delete",
		Attempts: 2, NextAttemptAt: ptrTime(erasureT0.Add(-time.Second)), Progress: prog,
	})
	user := &stubProducer{subject: "user"}
	svc, dsr := newErasureSvc(repo, user)
	dsr.SetLegalHoldChecker(&fakeHoldChecker{held: true})

	if ok, err := svc.RetryDue(context.Background()); !ok || err != nil {
		t.Fatalf("RetryDue = %v, %v", ok, err)
	}
	row := repo.Row("r-1")
	if user.purgeCalls != 0 || row.Status != models.ErasureRequestIncomplete || row.LastError != models.ErasureLastErrorLegalHold ||
		row.Attempts != 3 || row.NextAttemptAt == nil || !row.NextAttemptAt.Equal(erasureT0.Add(4*time.Minute)) ||
		len(row.Progress) != 1 || row.Progress[0].Outcome != models.ProducerOutcomeFailed {
		t.Fatalf("row wrong: %+v purge=%d", row, user.purgeCalls)
	}
}

func newCloseSvc(repo *erasuretest.MemRepo, producers ...iface.PIIProducer) (*ErasureRequestService, *capturingAuditSink) {
	dsr, _ := newDSRService(producers...)
	sink := &capturingAuditSink{}
	svc := NewErasureRequestService(repo, dsr, sink, nil)
	svc.now = func() time.Time { return erasureT0 }
	return svc, sink
}

func pendingProgress(key string) models.ErasureProducerProgress {
	return models.ErasureProducerProgress{Key: key, Subject: key, Outcome: models.ProducerOutcomePending, Attempts: 1}
}

func TestAbandonedKeys(t *testing.T) {
	t.Parallel()
	reg := func(keys ...string) map[string]bool {
		m := map[string]bool{}
		for _, k := range keys {
			m[k] = true
		}
		return m
	}
	cases := []struct {
		name     string
		progress []models.ErasureProducerProgress
		reg      map[string]bool
		keys     []string
		ok       bool
	}{
		{"registered not done", []models.ErasureProducerProgress{done("user"), failed("gone"), pendingProgress("auth.client")}, reg("user", "auth.client"), []string{"gone"}, false},
		{"only unregistered not done", []models.ErasureProducerProgress{done("user"), failed("gone")}, reg("user"), []string{"gone"}, true},
		{"all done", []models.ErasureProducerProgress{done("user")}, reg("user"), nil, false},
	}
	for _, c := range cases {
		keys, ok := abandonedKeys(c.progress, c.reg)
		if ok != c.ok || len(keys) != len(c.keys) || (len(keys) == 1 && keys[0] != c.keys[0]) {
			t.Errorf("%s: abandonedKeys = %v, %v; want %v, %v", c.name, keys, ok, c.keys, c.ok)
		}
	}
}

func TestClosePartialRecordsAbandonedProducersAndAudits(t *testing.T) {
	t.Parallel()
	repo := erasuretest.NewMemRepo()
	repo.Put(models.ErasureRequest{UUID: "r-1", UserUUID: "u-9", Mode: "anonymize", Status: models.ErasureRequestIncomplete,
		Progress: []models.ErasureProducerProgress{done("user"), failed("gone")}, RequestedAt: erasureT0})
	svc, sink := newCloseSvc(repo, &stubProducer{subject: "user"})

	got, err := svc.ClosePartial(context.Background(), "r-1", "admin-1", "  module removed in v2  ")
	if err != nil {
		t.Fatalf("ClosePartial: %v", err)
	}
	if got.Status != models.ErasureRequestClosedPartial || len(got.Abandoned) != 1 || got.Abandoned[0] != "gone" || got.ResolutionNote != "module removed in v2" {
		t.Fatalf("returned row = %+v", got)
	}
	if len(sink.events) != 1 {
		t.Fatalf("events = %+v; want one", sink.events)
	}
	ev := sink.events[0]
	ab, _ := ev.Metadata["abandoned"].([]string)
	if ev.Action != "gdpr.dsr.erasure_closed_partial" || ev.ResourceID != "r-1" || ev.ActorUserID != "admin-1" ||
		len(ab) != 1 || ab[0] != "gone" || ev.Metadata["subjectUserUuid"] != "u-9" || ev.Metadata["mode"] != "anonymize" {
		t.Fatalf("event = %+v", ev)
	}
	if _, has := ev.Metadata["note"]; has {
		t.Fatal("audit metadata must not carry the note")
	}
}

func TestClosePartialRefusedWhileARegisteredProducerIsNotDone(t *testing.T) {
	t.Parallel()
	repo := erasuretest.NewMemRepo()
	repo.Put(models.ErasureRequest{UUID: "r-1", UserUUID: "u-9", Status: models.ErasureRequestIncomplete,
		Progress: []models.ErasureProducerProgress{done("user"), failed("gone"), failed("auth.client")}, RequestedAt: erasureT0})
	svc, sink := newCloseSvc(repo, &stubProducer{subject: "user"}, &stubProducer{subject: "auth.client"})

	if _, err := svc.ClosePartial(context.Background(), "r-1", "admin-1", "note"); !errors.Is(err, ErrErasureRequestNotClosable) {
		t.Fatalf("err = %v; want ErrErasureRequestNotClosable", err)
	}
	if repo.Row("r-1").Status != models.ErasureRequestIncomplete || len(sink.events) != 0 {
		t.Fatalf("row changed or audited: %+v %+v", repo.Row("r-1"), sink.events)
	}
}

func TestClosePartialRefusesPendingAndResolved(t *testing.T) {
	t.Parallel()
	repo := erasuretest.NewMemRepo()
	repo.Put(models.ErasureRequest{UUID: "r-p", Status: models.ErasureRequestPending, RequestedAt: erasureT0})
	repo.Put(models.ErasureRequest{UUID: "r-c", Status: models.ErasureRequestCompleted, RequestedAt: erasureT0})
	svc, _ := newCloseSvc(repo)
	if _, err := svc.ClosePartial(context.Background(), "r-p", "admin-1", "note"); !errors.Is(err, ErrErasureRequestNotClosable) {
		t.Fatalf("pending err = %v", err)
	}
	if _, err := svc.ClosePartial(context.Background(), "r-c", "admin-1", "note"); !errors.Is(err, repository.ErrErasureRequestNotFound) {
		t.Fatalf("completed err = %v", err)
	}
}

func TestClosePartialMasksAndRequiresNote(t *testing.T) {
	t.Parallel()
	repo := erasuretest.NewMemRepo()
	repo.Put(models.ErasureRequest{UUID: "r-1", UserUUID: "u-9", Status: models.ErasureRequestIncomplete,
		Progress: []models.ErasureProducerProgress{failed("gone")}, RequestedAt: erasureT0})
	svc, _ := newCloseSvc(repo)
	for _, n := range []string{"", strings.Repeat("x", 501)} {
		if _, err := svc.ClosePartial(context.Background(), "r-1", "admin-1", n); !errors.Is(err, ErrErasureCloseNoteRequired) {
			t.Fatalf("note len %d err = %v; want ErrErasureCloseNoteRequired", len(n), err)
		}
	}
	got, err := svc.ClosePartial(context.Background(), "r-1", "admin-1", "asked by mario.rossi@example.com")
	if err != nil {
		t.Fatalf("ClosePartial: %v", err)
	}
	if strings.Contains(got.ResolutionNote, "mario.rossi@example.com") || strings.Contains(repo.Row("r-1").ResolutionNote, "mario.rossi@example.com") {
		t.Fatalf("note not masked: %q", got.ResolutionNote)
	}
}

func TestListOpenMarksClosableRows(t *testing.T) {
	t.Parallel()
	repo := erasuretest.NewMemRepo()
	repo.Put(models.ErasureRequest{UUID: "r-a", Status: models.ErasureRequestIncomplete, RequestedAt: erasureT0,
		Progress: []models.ErasureProducerProgress{done("user"), failed("gone")}})
	repo.Put(models.ErasureRequest{UUID: "r-b", Status: models.ErasureRequestIncomplete, RequestedAt: erasureT0.Add(time.Second),
		Progress: []models.ErasureProducerProgress{failed("user")}})
	repo.Put(models.ErasureRequest{UUID: "r-c", Status: models.ErasureRequestPending, RequestedAt: erasureT0.Add(2 * time.Second)})
	svc, _ := newCloseSvc(repo, &stubProducer{subject: "user"})
	got, err := svc.ListOpen(context.Background())
	if err != nil || len(got) != 3 {
		t.Fatalf("ListOpen = %d, %v", len(got), err)
	}
	want := map[string]bool{"r-a": true, "r-b": false, "r-c": false}
	for _, r := range got {
		if r.Closable != want[r.UUID] {
			t.Errorf("%s Closable = %v; want %v", r.UUID, r.Closable, want[r.UUID])
		}
	}
}

func TestExecuteCountsTheAttemptWhenClaimed(t *testing.T) {
	t.Parallel()
	repo := erasuretest.NewMemRepo()
	repo.Put(models.ErasureRequest{UUID: "r-1", UserUUID: "u-9", Status: models.ErasureRequestPending})
	user := &stubProducer{subject: "user", purgeResult: iface.PurgeResult{Pending: true}}
	svc, _ := newErasureSvc(repo, user)
	var during int
	user.onPurge = func() { during = repo.Row("r-1").Attempts }

	if _, err := svc.Execute(context.Background(), "r-1", iface.EraseHardDelete, "admin-1"); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if during != 1 || repo.Row("r-1").Attempts != 1 {
		t.Fatalf("attempts during run = %d, after = %d; want 1 and 1", during, repo.Row("r-1").Attempts)
	}
}

func TestRetryDuePanickingProducerFailsAloneAndSiblingsFinish(t *testing.T) {
	t.Parallel()
	repo := erasuretest.NewMemRepo()
	repo.Put(models.ErasureRequest{
		UUID: "r-1", UserUUID: "u-9", Status: models.ErasureRequestIncomplete, Mode: "hard_delete",
		NextAttemptAt: ptrTime(erasureT0.Add(-time.Second)),
	})
	user := &stubProducer{subject: "user"}
	crm := &stubProducer{subject: "crm", purgePanic: "boom"}
	auth := &stubProducer{subject: "auth"}
	svc, _ := newErasureSvc(repo, user, crm, auth)

	if ok, err := svc.RetryDue(context.Background()); !ok || err != nil {
		t.Fatalf("RetryDue = %v, %v", ok, err)
	}
	row := repo.Row("r-1")
	if row.Status != models.ErasureRequestIncomplete || row.LeaseUntil != nil || row.LastError != "" || len(row.Progress) != 3 ||
		row.Progress[0].Outcome != models.ProducerOutcomeDone || row.Progress[1].Outcome != models.ProducerOutcomeFailed ||
		row.Progress[2].Outcome != models.ProducerOutcomeDone || row.NextAttemptAt == nil {
		t.Fatalf("row wrong: %+v", row)
	}
}

func TestRunSettleIsBoundedByTheLease(t *testing.T) {
	t.Parallel()
	repo := erasuretest.NewMemRepo()
	repo.Put(models.ErasureRequest{UUID: "r-1", UserUUID: "u-9", Status: models.ErasureRequestPending})
	user := &stubProducer{subject: "user"}
	svc, _ := newErasureSvc(repo, user)
	svc.settleFloor = 50 * time.Millisecond
	// The producers use up the lease: only 1ms of it is left at settle time.
	user.onPurge = func() { svc.now = func() time.Time { return erasureT0.Add(erasureRunLease - time.Millisecond) } }
	var hadDeadline bool
	repo.OnSettle(func(ctx context.Context) error {
		_, hadDeadline = ctx.Deadline()
		<-ctx.Done()
		return ctx.Err()
	})

	done := make(chan *ErasureOutcome, 1)
	go func() {
		out, err := svc.Execute(context.Background(), "r-1", iface.EraseHardDelete, "admin-1")
		if err != nil {
			t.Errorf("Execute: %v", err)
		}
		done <- out
	}()
	select {
	case out := <-done:
		if !hadDeadline {
			t.Fatal("Settle ran without a deadline")
		}
		if out == nil || out.Request.Status != models.ErasureRequestIncomplete {
			t.Fatalf("an unsettled run must not report its intended state: %+v", out)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Settle was not bounded by the lease")
	}
}

func TestRunSettleFailureReportsIncomplete(t *testing.T) {
	t.Parallel()
	repo := erasuretest.NewMemRepo()
	repo.Put(models.ErasureRequest{UUID: "r-1", UserUUID: "u-9", Status: models.ErasureRequestPending})
	repo.FailNextSettle(errors.New("mongo blip"))
	svc, _ := newErasureSvc(repo, &stubProducer{subject: "user"})

	out, err := svc.Execute(context.Background(), "r-1", iface.EraseHardDelete, "admin-1")
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if out.Request.Status != models.ErasureRequestIncomplete || out.Request.ResolvedAt != nil {
		t.Fatalf("returned %+v; want incomplete, as stored", out.Request)
	}
	if !out.Run.Complete() {
		t.Fatalf("the run itself did every producer: %+v", out.Run)
	}
	if row := repo.Row("r-1"); row.Status != models.ErasureRequestIncomplete || row.LeaseUntil == nil {
		t.Fatalf("row = %+v; want incomplete under its lease", row)
	}
}
