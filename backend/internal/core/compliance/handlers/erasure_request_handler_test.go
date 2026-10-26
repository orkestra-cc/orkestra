package handlers

import (
	"context"
	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humachi"
	"github.com/go-chi/chi/v5"
	"net/http"
	"slices"
	"testing"
	"time"

	"github.com/orkestra/backend/internal/core/compliance/models"
	"github.com/orkestra/backend/internal/core/compliance/services"
	"github.com/orkestra/backend/internal/core/compliance/services/erasuretest"
	"github.com/orkestra/backend/internal/shared/errcode"
	"github.com/orkestra/backend/pkg/sdk/iface"
)

func seeded(rows ...models.ErasureRequest) *erasuretest.MemRepo {
	repo := erasuretest.NewMemRepo()
	for _, r := range rows {
		repo.Put(r)
	}
	return repo
}

func newErasureHandler(repo *erasuretest.MemRepo, dsr *services.DSRService) *ErasureRequestHandler {
	return NewErasureRequestHandler(services.NewErasureRequestService(repo, dsr, nil, nil))
}

func TestErasureLodgeRequiresAuth(t *testing.T) {
	t.Parallel()
	h := newErasureHandler(erasuretest.NewMemRepo(), newDSR())
	_, err := h.Lodge(context.Background(), &LodgeErasureRequestInput{})
	assertStatus(t, err, 401)
}

func TestErasureLodgeSuccess(t *testing.T) {
	t.Parallel()
	h := newErasureHandler(erasuretest.NewMemRepo(), newDSR())
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

func TestErasureListOpen(t *testing.T) {
	t.Parallel()
	h := newErasureHandler(seeded(models.ErasureRequest{UUID: "r-1", Status: models.ErasureRequestPending}), newDSR())
	out, err := h.ListOpen(context.Background(), &struct{}{})
	if err != nil {
		t.Fatalf("ListOpen: %v", err)
	}
	if len(out.Body.Items) != 1 {
		t.Fatalf("expected 1 request, got %d", len(out.Body.Items))
	}
}

func TestErasureExecuteSuccess(t *testing.T) {
	t.Parallel()
	repo := seeded(models.ErasureRequest{
		UUID: "r-1", UserUUID: "u-9", Status: models.ErasureRequestPending,
	})
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
	repo := seeded(models.ErasureRequest{
		UUID: "r-1", UserUUID: "u-held", Status: models.ErasureRequestPending,
	})
	dsr := newDSR(&fakeProducer{subject: "user"})
	dsr.SetLegalHoldChecker(&fakeHoldChecker{held: true})
	h := newErasureHandler(repo, dsr)

	_, err := h.Execute(authedCtx("admin-1"), &ExecuteErasureRequestInput{ID: "r-1"})
	assertStatus(t, err, 409)
}

func TestErasureExecuteNotFoundIs404(t *testing.T) {
	t.Parallel()
	// An already-resolved request is not re-executable → ErrErasureRequestNotFound.
	repo := seeded(models.ErasureRequest{
		UUID: "r-1", Status: models.ErasureRequestCompleted,
	})
	h := newErasureHandler(repo, newDSR(&fakeProducer{subject: "user"}))
	_, err := h.Execute(authedCtx("admin-1"), &ExecuteErasureRequestInput{ID: "r-1"})
	assertStatus(t, err, 404)
}

func TestErasureRejectNotFoundIs404(t *testing.T) {
	t.Parallel()
	h := newErasureHandler(erasuretest.NewMemRepo(), newDSR())
	_, err := h.Reject(authedCtx("admin-1"), &RejectErasureRequestInput{ID: "missing"})
	assertStatus(t, err, 404)
}

func TestErasureRejectSuccess(t *testing.T) {
	t.Parallel()
	h := newErasureHandler(seeded(models.ErasureRequest{UUID: "r-1", Status: models.ErasureRequestPending}), newDSR())
	in := &RejectErasureRequestInput{ID: "r-1"}
	in.Body.Note = "no proof of identity"
	if _, err := h.Reject(authedCtx("admin-1"), in); err != nil {
		t.Fatalf("Reject: %v", err)
	}
}

func incompleteRow(progress ...models.ErasureProducerProgress) models.ErasureRequest {
	return models.ErasureRequest{
		UUID: "r-1", UserUUID: "u-9", Status: models.ErasureRequestIncomplete,
		Mode: "hard_delete", Progress: progress,
	}
}

func TestErasureListOpenIncludesIncomplete(t *testing.T) {
	t.Parallel()
	h := newErasureHandler(seeded(
		models.ErasureRequest{UUID: "r-1", Status: models.ErasureRequestPending},
		models.ErasureRequest{UUID: "r-2", Status: models.ErasureRequestIncomplete},
		models.ErasureRequest{UUID: "r-3", Status: models.ErasureRequestCompleted},
	), newDSR())
	out, err := h.ListOpen(context.Background(), &struct{}{})
	if err != nil {
		t.Fatalf("ListOpen: %v", err)
	}
	if len(out.Body.Items) != 2 {
		t.Fatalf("items = %d, want 2", len(out.Body.Items))
	}
}

func TestErasureExecuteIncompleteReturnsProgress(t *testing.T) {
	t.Parallel()
	repo := seeded(incompleteRow(
		models.ErasureProducerProgress{Key: "user", Subject: "user", Outcome: models.ProducerOutcomeDone},
		models.ErasureProducerProgress{Key: "retrieval", Subject: "retrieval", Outcome: models.ProducerOutcomePending},
	))
	h := newErasureHandler(repo, newDSR(&fakeProducer{subject: "user"}, &fakeProducer{subject: "retrieval"}))
	out, err := h.Execute(authedCtx("admin-1"), &ExecuteErasureRequestInput{ID: "r-1"})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if out.Body.Status != "completed" || len(out.Body.Progress) != 2 {
		t.Fatalf("status=%q progress=%+v", out.Body.Status, out.Body.Progress)
	}
}

func TestErasureExecuteBusyIs409(t *testing.T) {
	t.Parallel()
	lease := time.Now().Add(time.Hour)
	row := incompleteRow()
	row.LeaseUntil = &lease
	h := newErasureHandler(seeded(row), newDSR(&fakeProducer{subject: "user"}))
	_, err := h.Execute(authedCtx("admin-1"), &ExecuteErasureRequestInput{ID: "r-1"})
	assertCode(t, err, http.StatusConflict, errcode.ComplianceErasureRequestBusy)
}

func TestErasureRejectIncompleteIs409(t *testing.T) {
	t.Parallel()
	h := newErasureHandler(seeded(incompleteRow()), newDSR())
	_, err := h.Reject(authedCtx("admin-1"), &RejectErasureRequestInput{ID: "r-1"})
	assertCode(t, err, http.StatusConflict, errcode.ComplianceErasureRequestStarted)
}

func TestErasureCloseUnregisteredProducer(t *testing.T) {
	t.Parallel()
	repo := seeded(incompleteRow(
		models.ErasureProducerProgress{Key: "user", Subject: "user", Outcome: models.ProducerOutcomeDone},
		models.ErasureProducerProgress{Key: "gone", Subject: "gone", Outcome: models.ProducerOutcomeFailed},
	))
	h := newErasureHandler(repo, newDSR(&fakeProducer{subject: "user"}))
	in := &CloseErasureRequestInput{ID: "r-1"}
	in.Body.Note = "module removed"
	out, err := h.Close(authedCtx("admin-1"), in)
	if err != nil {
		t.Fatalf("Close: %v", err)
	}
	if out.Body.Status != models.ErasureRequestClosedPartial || len(out.Body.Abandoned) != 1 ||
		out.Body.Abandoned[0] != "gone" || out.Body.ResolvedBy != "admin-1" {
		t.Fatalf("closed = %+v", out.Body)
	}
}

func TestErasureCloseWithRegisteredProducerIs409(t *testing.T) {
	t.Parallel()
	repo := seeded(incompleteRow(
		models.ErasureProducerProgress{Key: "user", Subject: "user", Outcome: models.ProducerOutcomeFailed},
	))
	h := newErasureHandler(repo, newDSR(&fakeProducer{subject: "user"}))
	in := &CloseErasureRequestInput{ID: "r-1"}
	in.Body.Note = "x"
	_, err := h.Close(authedCtx("admin-1"), in)
	assertCode(t, err, http.StatusConflict, errcode.ComplianceErasureRequestNotClosable)
}

func TestErasureCloseWithoutNoteIs422(t *testing.T) {
	t.Parallel()
	h := newErasureHandler(seeded(incompleteRow()), newDSR())
	_, err := h.Close(authedCtx("admin-1"), &CloseErasureRequestInput{ID: "r-1"})
	assertCode(t, err, http.StatusUnprocessableEntity, errcode.ComplianceErasureCloseNoteRequired)
}

func TestErasureCloseNotFoundIs404(t *testing.T) {
	t.Parallel()
	h := newErasureHandler(erasuretest.NewMemRepo(), newDSR())
	in := &CloseErasureRequestInput{ID: "missing"}
	in.Body.Note = "x"
	_, err := h.Close(authedCtx("admin-1"), in)
	assertStatus(t, err, 404)
}

func TestErasureCloseRouteIsBehindDSRManageAndStepUp(t *testing.T) {
	t.Parallel()
	const closePath = "POST /v1/admin/compliance/erasure-requests/{id}/close"
	mount := func(register func(huma.API, *ErasureRequestHandler)) []string {
		api := humachi.New(chi.NewRouter(), huma.DefaultConfig("test", "1.0.0"))
		register(api, &ErasureRequestHandler{})
		var out []string
		for path, item := range api.OpenAPI().Paths {
			if item.Post != nil {
				out = append(out, "POST "+path)
			}
			if item.Get != nil {
				out = append(out, "GET "+path)
			}
		}
		return out
	}
	if !slices.Contains(mount(RegisterErasureRequestAdminWriteRoutes), closePath) {
		t.Fatal("close must be registered by the write (dsr.manage + step-up) group")
	}
	if slices.Contains(mount(RegisterErasureRequestAdminReadRoutes), closePath) {
		t.Fatal("close must not be registered by the read group")
	}
}
