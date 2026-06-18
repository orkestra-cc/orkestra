package services

import (
	"context"
	"errors"
	"testing"

	"github.com/orkestra/backend/internal/core/compliance/models"
	"github.com/orkestra/backend/internal/core/compliance/repository"
	"github.com/orkestra/backend/pkg/sdk/iface"
)

// fakeErasureRepo is a hand-rolled stub of erasureRequestRepo.
type fakeErasureRepo struct {
	inserted       *models.ErasureRequest
	insertErr      error
	getResult      *models.ErasureRequest
	getErr         error
	listResult     []models.ErasureRequest
	listErr        error
	resolveErr     error
	lastListStatus string
	resolveCalls   int
	lastResolve    struct{ uuid, status, resolvedBy, mode, note string }
}

func (f *fakeErasureRepo) Insert(_ context.Context, req *models.ErasureRequest) error {
	f.inserted = req
	return f.insertErr
}
func (f *fakeErasureRepo) Get(_ context.Context, _ string) (*models.ErasureRequest, error) {
	return f.getResult, f.getErr
}
func (f *fakeErasureRepo) ListByStatus(_ context.Context, status string) ([]models.ErasureRequest, error) {
	f.lastListStatus = status
	return f.listResult, f.listErr
}
func (f *fakeErasureRepo) Resolve(_ context.Context, uuid, status, resolvedBy, mode, note string) error {
	f.resolveCalls++
	f.lastResolve = struct{ uuid, status, resolvedBy, mode, note string }{uuid, status, resolvedBy, mode, note}
	return f.resolveErr
}

// TestLodgeCreatesPendingRequest pins the Lodge contract: the service stamps a
// UUID + RequestedAt, defaults the status to pending, and forwards the subject
// fields verbatim.
func TestLodgeCreatesPendingRequest(t *testing.T) {
	t.Parallel()

	repo := &fakeErasureRepo{}
	dsr, _ := newDSRService()
	svc := NewErasureRequestService(repo, dsr)

	got, err := svc.Lodge(context.Background(), "u-1", "t-1", "no longer a customer")
	if err != nil {
		t.Fatalf("Lodge: %v", err)
	}
	if repo.inserted == nil {
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

// TestListPendingFiltersByStatus pins that ListPending asks the repo for
// pending rows specifically.
func TestListPendingFiltersByStatus(t *testing.T) {
	t.Parallel()

	repo := &fakeErasureRepo{listResult: []models.ErasureRequest{{UUID: "r-1"}}}
	dsr, _ := newDSRService()
	svc := NewErasureRequestService(repo, dsr)

	got, err := svc.ListPending(context.Background())
	if err != nil {
		t.Fatalf("ListPending: %v", err)
	}
	if repo.lastListStatus != models.ErasureRequestPending {
		t.Fatalf("ListPending should query pending status; got %q", repo.lastListStatus)
	}
	if len(got) != 1 {
		t.Fatalf("expected 1 request, got %d", len(got))
	}
}

// TestExecuteRunsEraseAndResolves pins the happy path: a pending request runs
// the DSR erase, then the request is resolved to completed carrying the erase
// mode label.
func TestExecuteRunsEraseAndResolves(t *testing.T) {
	t.Parallel()

	repo := &fakeErasureRepo{getResult: &models.ErasureRequest{
		UUID:     "r-1",
		UserUUID: "u-9",
		Status:   models.ErasureRequestPending,
	}}
	producer := &stubProducer{subject: "user", purgeResult: iface.PurgeResult{RowsDeleted: 1}}
	dsr, _ := newDSRService(producer)
	svc := NewErasureRequestService(repo, dsr)

	res, err := svc.Execute(context.Background(), "r-1", iface.EraseHardDelete, "admin-1")
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if res == nil || res.Purged["user"].RowsDeleted != 1 {
		t.Fatalf("expected the DSR erase to run; got %+v", res)
	}
	if producer.purgeCalls != 1 {
		t.Fatalf("producer should be purged once; got %d", producer.purgeCalls)
	}
	if repo.resolveCalls != 1 {
		t.Fatalf("request should be resolved exactly once; got %d", repo.resolveCalls)
	}
	if repo.lastResolve.status != models.ErasureRequestCompleted {
		t.Fatalf("resolved status = %q; want completed", repo.lastResolve.status)
	}
	if repo.lastResolve.resolvedBy != "admin-1" || repo.lastResolve.mode != "hard_delete" {
		t.Fatalf("resolve metadata wrong: %+v", repo.lastResolve)
	}
}

// TestExecuteRejectsNonPending pins that executing an already-resolved request
// returns ErrErasureRequestNotFound and never runs the erase.
func TestExecuteRejectsNonPending(t *testing.T) {
	t.Parallel()

	repo := &fakeErasureRepo{getResult: &models.ErasureRequest{
		UUID:   "r-1",
		Status: models.ErasureRequestCompleted,
	}}
	producer := &stubProducer{subject: "user"}
	dsr, _ := newDSRService(producer)
	svc := NewErasureRequestService(repo, dsr)

	_, err := svc.Execute(context.Background(), "r-1", iface.EraseHardDelete, "admin-1")
	if !errors.Is(err, repository.ErrErasureRequestNotFound) {
		t.Fatalf("expected ErrErasureRequestNotFound, got %v", err)
	}
	if producer.purgeCalls != 0 || repo.resolveCalls != 0 {
		t.Fatalf("a non-pending request must not erase or resolve; purge=%d resolve=%d",
			producer.purgeCalls, repo.resolveCalls)
	}
}

// TestExecuteBlockedByLegalHold pins that a hold blocks execution: the erase
// errors with ErrLegalHoldActive and the request stays pending (not resolved).
func TestExecuteBlockedByLegalHold(t *testing.T) {
	t.Parallel()

	repo := &fakeErasureRepo{getResult: &models.ErasureRequest{
		UUID:     "r-1",
		UserUUID: "u-held",
		Status:   models.ErasureRequestPending,
	}}
	dsr, _ := newDSRService(&stubProducer{subject: "user"})
	dsr.SetLegalHoldChecker(&fakeHoldChecker{held: true})
	svc := NewErasureRequestService(repo, dsr)

	_, err := svc.Execute(context.Background(), "r-1", iface.EraseHardDelete, "admin-1")
	if !errors.Is(err, ErrLegalHoldActive) {
		t.Fatalf("expected ErrLegalHoldActive, got %v", err)
	}
	if repo.resolveCalls != 0 {
		t.Fatalf("a blocked request must stay pending (not resolved); resolve calls=%d", repo.resolveCalls)
	}
}

// TestRejectResolvesRejected pins that Reject closes the request as rejected
// carrying the operator note.
func TestRejectResolvesRejected(t *testing.T) {
	t.Parallel()

	repo := &fakeErasureRepo{}
	dsr, _ := newDSRService()
	svc := NewErasureRequestService(repo, dsr)

	if err := svc.Reject(context.Background(), "r-1", "admin-2", "insufficient identity proof"); err != nil {
		t.Fatalf("Reject: %v", err)
	}
	if repo.resolveCalls != 1 {
		t.Fatalf("Reject should resolve once; got %d", repo.resolveCalls)
	}
	if repo.lastResolve.status != models.ErasureRequestRejected {
		t.Fatalf("resolved status = %q; want rejected", repo.lastResolve.status)
	}
	if repo.lastResolve.note != "insufficient identity proof" || repo.lastResolve.resolvedBy != "admin-2" {
		t.Fatalf("reject metadata wrong: %+v", repo.lastResolve)
	}
}
