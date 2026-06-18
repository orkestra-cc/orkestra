package handlers

import (
	"context"
	"testing"

	"github.com/orkestra/backend/internal/core/compliance/models"
	"github.com/orkestra/backend/internal/core/compliance/repository"
	"github.com/orkestra/backend/internal/core/compliance/services"
	"github.com/orkestra/backend/pkg/sdk/iface"
)

// fakeErasureReqRepo structurally satisfies the services erasure-request seam.
type fakeErasureReqRepo struct {
	getResult  *models.ErasureRequest
	getErr     error
	listResult []models.ErasureRequest
	resolveErr error
}

func (f *fakeErasureReqRepo) Insert(_ context.Context, _ *models.ErasureRequest) error { return nil }
func (f *fakeErasureReqRepo) Get(_ context.Context, _ string) (*models.ErasureRequest, error) {
	return f.getResult, f.getErr
}
func (f *fakeErasureReqRepo) ListByStatus(_ context.Context, _ string) ([]models.ErasureRequest, error) {
	return f.listResult, nil
}
func (f *fakeErasureReqRepo) Resolve(_ context.Context, _, _, _, _, _ string) error {
	return f.resolveErr
}

func newErasureHandler(repo *fakeErasureReqRepo, dsr *services.DSRService) *ErasureRequestHandler {
	return NewErasureRequestHandler(services.NewErasureRequestService(repo, dsr))
}

func TestErasureLodgeRequiresAuth(t *testing.T) {
	t.Parallel()
	h := newErasureHandler(&fakeErasureReqRepo{}, newDSR())
	_, err := h.Lodge(context.Background(), &LodgeErasureRequestInput{})
	assertStatus(t, err, 401)
}

func TestErasureLodgeSuccess(t *testing.T) {
	t.Parallel()
	h := newErasureHandler(&fakeErasureReqRepo{}, newDSR())
	in := &LodgeErasureRequestInput{}
	in.Body.Reason = "leaving"
	out, err := h.Lodge(authedCtx("u-1"), in)
	if err != nil {
		t.Fatalf("Lodge: %v", err)
	}
	if out.Body.UserUUID != "u-1" || out.Body.Status != models.ErasureRequestPending {
		t.Fatalf("lodged request wrong: %+v", out.Body)
	}
}

func TestErasureListPending(t *testing.T) {
	t.Parallel()
	h := newErasureHandler(&fakeErasureReqRepo{listResult: []models.ErasureRequest{{UUID: "r-1"}}}, newDSR())
	out, err := h.ListPending(context.Background(), &struct{}{})
	if err != nil {
		t.Fatalf("ListPending: %v", err)
	}
	if len(out.Body.Items) != 1 {
		t.Fatalf("expected 1 request, got %d", len(out.Body.Items))
	}
}

func TestErasureExecuteSuccess(t *testing.T) {
	t.Parallel()
	repo := &fakeErasureReqRepo{getResult: &models.ErasureRequest{
		UUID: "r-1", UserUUID: "u-9", Status: models.ErasureRequestPending,
	}}
	dsr := newDSR(&fakeProducer{subject: "user", purgeResult: iface.PurgeResult{RowsDeleted: 1}})
	h := newErasureHandler(repo, dsr)

	in := &ExecuteErasureRequestInput{ID: "r-1"}
	in.Body.Mode = "hard_delete"
	out, err := h.Execute(authedCtx("admin-1"), in)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if out.Body.Purged["user"].RowsDeleted != 1 {
		t.Fatalf("expected the erase to run; got %+v", out.Body.Purged)
	}
}

func TestErasureExecuteBlockedByLegalHoldIs409(t *testing.T) {
	t.Parallel()
	repo := &fakeErasureReqRepo{getResult: &models.ErasureRequest{
		UUID: "r-1", UserUUID: "u-held", Status: models.ErasureRequestPending,
	}}
	dsr := newDSR(&fakeProducer{subject: "user"})
	dsr.SetLegalHoldChecker(&fakeHoldChecker{held: true})
	h := newErasureHandler(repo, dsr)

	_, err := h.Execute(authedCtx("admin-1"), &ExecuteErasureRequestInput{ID: "r-1"})
	assertStatus(t, err, 409)
}

func TestErasureExecuteNotFoundIs404(t *testing.T) {
	t.Parallel()
	// An already-resolved request is not re-executable → ErrErasureRequestNotFound.
	repo := &fakeErasureReqRepo{getResult: &models.ErasureRequest{
		UUID: "r-1", Status: models.ErasureRequestCompleted,
	}}
	h := newErasureHandler(repo, newDSR(&fakeProducer{subject: "user"}))
	_, err := h.Execute(authedCtx("admin-1"), &ExecuteErasureRequestInput{ID: "r-1"})
	assertStatus(t, err, 404)
}

func TestErasureRejectNotFoundIs404(t *testing.T) {
	t.Parallel()
	h := newErasureHandler(&fakeErasureReqRepo{resolveErr: repository.ErrErasureRequestNotFound}, newDSR())
	_, err := h.Reject(authedCtx("admin-1"), &RejectErasureRequestInput{ID: "missing"})
	assertStatus(t, err, 404)
}

func TestErasureRejectSuccess(t *testing.T) {
	t.Parallel()
	h := newErasureHandler(&fakeErasureReqRepo{}, newDSR())
	in := &RejectErasureRequestInput{ID: "r-1"}
	in.Body.Note = "no proof of identity"
	if _, err := h.Reject(authedCtx("admin-1"), in); err != nil {
		t.Fatalf("Reject: %v", err)
	}
}
