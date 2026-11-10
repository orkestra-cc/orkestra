package handlers

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/danielgtaylor/huma/v2/humatest"

	"github.com/orkestra/backend/internal/core/compliance/models"
	"github.com/orkestra/backend/internal/core/compliance/repository"
	"github.com/orkestra/backend/internal/core/compliance/services"
	"github.com/orkestra/backend/internal/core/compliance/services/erasuretest"
	"github.com/orkestra/backend/internal/shared/errcode"
)

func approvalHandler(repo *erasuretest.MemRepo, dsr *services.DSRService, required int) *ErasureRequestHandler {
	svc := services.NewErasureRequestService(repo, dsr, nil, nil)
	svc.SetRequiredApprovals(func(context.Context) int { return required })
	return NewErasureRequestHandler(svc)
}

func openInput(user, reason, mode string) *OpenErasureRequestInput {
	in := &OpenErasureRequestInput{}
	in.Body.UserUUID, in.Body.Reason, in.Body.Mode = user, reason, mode
	return in
}

func extension(t *testing.T, ce *errcode.Error, key string) any {
	t.Helper()
	body, _ := json.Marshal(ce)
	var m map[string]any
	if err := json.Unmarshal(body, &m); err != nil {
		t.Fatal(err)
	}
	return m[key]
}

func TestOpenCreatesAdminRequest(t *testing.T) {
	t.Parallel()
	repo := erasuretest.NewMemRepo()
	h := approvalHandler(repo, newDSR(&fakeProducer{subject: "user"}), 2)
	out, err := h.Open(authedCtx("op-a"), openInput("u-1", "contract ended", "anonymize"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	b := out.Body
	if b.Source != models.ErasureSourceAdmin || b.RequestedBy != "op-a" || b.Mode != "anonymize" || b.RequiredApprovals != 2 || len(b.Approvals) != 1 {
		t.Fatalf("body = %+v", b)
	}
}

func TestOpenRouteDeclares201(t *testing.T) {
	t.Parallel()
	h := approvalHandler(erasuretest.NewMemRepo(), newDSR(), 1)
	_, api := humatest.New(t)
	RegisterErasureRequestAdminWriteRoutes(api, h)
	op := api.OpenAPI().Paths["/v1/admin/compliance/erasure-requests"].Post
	if op == nil || op.Responses["201"] == nil {
		t.Fatalf("POST /v1/admin/compliance/erasure-requests must answer 201: %+v", op)
	}
	if api.OpenAPI().Paths["/v1/admin/compliance/erasure-requests/{id}/approve"].Post == nil {
		t.Fatal("approve route missing")
	}
}

func TestOpenErrors(t *testing.T) {
	t.Parallel()
	repo := erasuretest.NewMemRepo()
	dsr := newDSR(&fakeProducer{subject: "user"})
	h := approvalHandler(repo, dsr, 1)

	_, err := h.Open(authedCtx("op-a"), openInput("u-1", "  ", "anonymize"))
	assertCoded(t, err, 422, errcode.ComplianceReasonRequired)

	first, err := h.Open(authedCtx("op-a"), openInput("u-1", "why", "anonymize"))
	if err != nil {
		t.Fatal(err)
	}
	_, err = h.Open(authedCtx("op-b"), openInput("u-1", "again", "anonymize"))
	ce := assertCoded(t, err, 409, errcode.ComplianceErasureRequestExists)
	if got := extension(t, ce, "requestId"); got != first.Body.UUID {
		t.Fatalf("requestId = %v; want %s", got, first.Body.UUID)
	}

	_, err = h.Open(authedCtx("u-2"), openInput("u-2", "mine", "anonymize"))
	assertCoded(t, err, 409, errcode.ComplianceErasureSelfApproval)

	dsr.SetLegalHoldChecker(&fakeHoldChecker{held: true})
	_, err = h.Open(authedCtx("op-a"), openInput("u-3", "held", "anonymize"))
	assertCoded(t, err, 409, errcode.ComplianceLegalHoldActive)
}

func TestOpenHardDeleteInsideRetentionIs409(t *testing.T) {
	t.Parallel()
	until := time.Now().UTC().AddDate(3, 0, 0)
	h := approvalHandler(erasuretest.NewMemRepo(), gatedDSR(&retainingProducer{fakeProducer: fakeProducer{subject: "billing"}, until: until}), 1)
	_, err := h.Open(authedCtx("op-a"), openInput("u-1", "why", "hard_delete"))
	assertCoded(t, err, 409, errcode.ComplianceRetentionMinimumNotMet)
}

func TestApproveAndExecuteErrors(t *testing.T) {
	t.Parallel()
	repo := seeded(models.ErasureRequest{UUID: "r-1", UserUUID: "u-1", Status: models.ErasureRequestPending, RequiredApprovals: 2})
	h := approvalHandler(repo, newDSR(&fakeProducer{subject: "user"}), 2)
	appr := func(actor, mode, note string) (*ErasureRequestOutput, error) {
		in := &ApproveErasureRequestInput{ID: "r-1"}
		in.Body.Mode, in.Body.Note = mode, note
		return h.Approve(authedCtx(actor), in)
	}

	_, err := h.Execute(authedCtx("op-a"), &ExecuteErasureRequestInput{ID: "r-1"})
	ce := assertCoded(t, err, 409, errcode.ComplianceErasureApprovalsMissing)
	if extension(t, ce, "have") != float64(1) || extension(t, ce, "need") != float64(2) {
		t.Fatalf("have/need = %v/%v", extension(t, ce, "have"), extension(t, ce, "need"))
	}

	_, err = appr("op-a", "anonymize", " ")
	assertCoded(t, err, 422, errcode.ComplianceReasonRequired)
	_, err = appr("u-1", "anonymize", "mine")
	assertCoded(t, err, 409, errcode.ComplianceErasureSelfApproval)
	out, err := appr("op-a", "anonymize", "ok")
	if err != nil || len(out.Body.Approvals) != 1 || out.Body.Mode != "anonymize" {
		t.Fatalf("Approve = %+v, %v", out, err)
	}
	_, err = appr("op-a", "anonymize", "again")
	assertCoded(t, err, 409, errcode.ComplianceErasureAlreadyApproved)
	_, err = appr("op-b", "hard_delete", "other")
	assertCoded(t, err, 409, errcode.ComplianceErasureModeMismatch)

	in := &ExecuteErasureRequestInput{ID: "r-1"}
	in.Body.Mode = "hard_delete"
	_, err = h.Execute(authedCtx("op-b"), in)
	assertCoded(t, err, 409, errcode.ComplianceErasureModeMismatch)
	_, err = h.Execute(authedCtx("u-1"), &ExecuteErasureRequestInput{ID: "r-1"})
	assertCoded(t, err, 409, errcode.ComplianceErasureSelfApproval)

	in.Body.Mode = "" // omitted: the locked mode
	if _, err := h.Execute(authedCtx("op-b"), in); err != nil {
		t.Fatalf("Execute by B: %v", err)
	}
}

func TestApproveUnknownIs404(t *testing.T) {
	t.Parallel()
	h := approvalHandler(erasuretest.NewMemRepo(), newDSR(), 1)
	in := &ApproveErasureRequestInput{ID: "nope"}
	in.Body.Mode, in.Body.Note = "anonymize", "ok"
	_, err := h.Approve(authedCtx("op-a"), in)
	assertStatus(t, err, 404)
}

func TestExecuteWithoutModeRunsTheLockedModeInsideRetention(t *testing.T) {
	t.Parallel()
	until := time.Now().UTC().AddDate(3, 0, 0)
	repo := seeded(models.ErasureRequest{UUID: "r-1", UserUUID: "u-1", Status: models.ErasureRequestPending, Mode: "anonymize"})
	h := approvalHandler(repo, gatedDSR(&retainingProducer{fakeProducer: fakeProducer{subject: "billing"}, until: until}), 1)

	in := &ExecuteErasureRequestInput{ID: "r-1"}
	in.Body.Mode = "hard_delete"
	_, err := h.Execute(authedCtx("op-b"), in)
	assertCoded(t, err, 409, errcode.ComplianceErasureModeMismatch)
	if repo.Row("r-1").Status != models.ErasureRequestPending {
		t.Fatal("request must stay pending")
	}

	out, err := h.Execute(authedCtx("op-b"), &ExecuteErasureRequestInput{ID: "r-1"})
	if err != nil || out.Body.Status != models.ErasureRequestCompleted {
		t.Fatalf("Execute without mode = %+v, %v; want the locked anonymize to run", out, err)
	}
	if repo.Row("r-1").Mode != "anonymize" {
		t.Fatalf("mode = %q", repo.Row("r-1").Mode)
	}
}

func TestExecuteRequiresAnActor(t *testing.T) {
	t.Parallel()
	h := approvalHandler(seeded(models.ErasureRequest{UUID: "r-1", UserUUID: "u-1", Status: models.ErasureRequestPending}), newDSR(), 1)
	_, err := h.Execute(context.Background(), &ExecuteErasureRequestInput{ID: "r-1"})
	assertStatus(t, err, 401)
}

type conflictRepo struct{ *erasuretest.MemRepo }

func (conflictRepo) AddApproval(context.Context, string, string, models.ErasureApproval) (*models.ErasureRequest, error) {
	return nil, repository.ErrErasureApprovalConflict
}

func TestApproveCASConflictWithoutReasonIsBusy(t *testing.T) {
	t.Parallel()
	repo := conflictRepo{seeded(models.ErasureRequest{UUID: "r-1", UserUUID: "u-1", Status: models.ErasureRequestPending})}
	svc := services.NewErasureRequestService(repo, newDSR(), nil, nil)
	in := &ApproveErasureRequestInput{ID: "r-1"}
	in.Body.Mode, in.Body.Note = "anonymize", "ok"
	_, err := NewErasureRequestHandler(svc).Approve(authedCtx("op-a"), in)
	assertCoded(t, err, 409, errcode.ComplianceErasureRequestBusy)
}
