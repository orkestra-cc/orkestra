package handlers

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"testing"
	"time"

	"github.com/orkestra/backend/internal/core/compliance/models"
	"github.com/orkestra/backend/internal/core/compliance/services"
	"github.com/orkestra/backend/internal/core/compliance/services/policytest"
	"github.com/orkestra/backend/internal/shared/errcode"
	"github.com/orkestra/backend/internal/testkit"
	"github.com/orkestra/backend/pkg/sdk/iface"
)

type policyFixture struct {
	h        *PolicyHandler
	tenant   *TenantPolicyHandler
	policies *services.PolicyService
	repo     *policytest.MemRepo
}

func newPolicyFixture(t *testing.T) *policyFixture {
	t.Helper()
	repo := policytest.NewMemRepo()
	logger := slog.New(slog.DiscardHandler)
	svc := services.NewPolicyService(repo, logger)
	tenants := policytest.Tenants{"t1": {UUID: "t1", Kind: iface.TenantKindExternal, Name: "Clinica"}}
	admin := services.NewPolicyAdminService(repo, svc, tenants, &policytest.Sink{}, func(context.Context) bool { return true }, logger)
	if err := admin.EnsurePlatformPolicy(context.Background()); err != nil {
		t.Fatal(err)
	}
	return &policyFixture{h: NewPolicyHandler(admin, svc), tenant: NewTenantPolicyHandler(svc), policies: svc, repo: repo}
}

func assertCode(t *testing.T, err error, status int, code string) {
	t.Helper()
	var ce *errcode.Error
	if !errors.As(err, &ce) || ce.Status != status || ce.Code != code {
		t.Fatalf("error = %v (%+v), want %d %s", err, ce, status, code)
	}
}

func cleanTenantPolicy(name string) models.PolicyInput {
	in := models.PolicyInput{
		Name:       name,
		LogContent: iface.DefaultLogContentPolicy(),
		Retention:  map[iface.RetentionClass]int{iface.RetentionPrivilegedChange: 730},
		Accountability: models.Accountability{Role: models.RoleProcessor, RoPARef: "RoPA-7", AssessmentRef: "DPIA-3",
			ReviewDueAt: time.Now().AddDate(0, 6, 0)},
	}
	in.LogContent.IPAddress = iface.IPAddressOmitted
	return in
}

func TestPolicyHandler_CreateStatuses(t *testing.T) {
	f := newPolicyFixture(t)
	alice := adminTenantCtx("alice", "internal-1")

	in := &CreatePolicyInput{}
	in.Body.Policy, in.Body.Reason = cleanTenantPolicy("Strict"), "contratto"
	out, err := f.h.Create(alice, in)
	if err != nil || out.Status != http.StatusCreated || !out.Body.Applied || out.Body.Policy == nil || out.Body.Warnings == nil {
		t.Fatalf("clean create = %+v, %v", out, err)
	}

	loose := &CreatePolicyInput{}
	loose.Body.Policy, loose.Body.Reason = cleanTenantPolicy("Loose"), "motivo"
	loose.Body.Policy.LogContent.IPAddress = iface.IPAddressFull
	_, err = f.h.Create(alice, loose)
	assertCode(t, err, http.StatusUnprocessableEntity, errcode.CompliancePolicyWarningsUnacknowledged)

	loose.Body.AcknowledgeWarnings = true
	out, err = f.h.Create(alice, loose)
	if err != nil || out.Status != http.StatusAccepted || out.Body.Applied || out.Body.ChangeRequest == nil {
		t.Fatalf("create with warnings = %+v, %v", out, err)
	}

	dup := &CreatePolicyInput{}
	dup.Body.Policy, dup.Body.Reason = cleanTenantPolicy("Strict"), "motivo"
	_, err = f.h.Create(alice, dup)
	assertCode(t, err, http.StatusConflict, errcode.CompliancePolicyNameTaken)

	noReason := &CreatePolicyInput{}
	noReason.Body.Policy = cleanTenantPolicy("Other")
	_, err = f.h.Create(alice, noReason)
	assertCode(t, err, http.StatusUnprocessableEntity, errcode.CompliancePolicyReasonRequired)
}

func TestPolicyHandler_UpdateConflictAndSelfApproval(t *testing.T) {
	f := newPolicyFixture(t)
	alice := adminTenantCtx("alice", "internal-1")
	c := &CreatePolicyInput{}
	c.Body.Policy, c.Body.Reason = cleanTenantPolicy("Strict"), "motivo"
	created, _ := f.h.Create(alice, c)

	up := &UpdatePolicyInput{ID: created.Body.Policy.UUID}
	up.Body.ExpectedVersion, up.Body.Policy, up.Body.Reason = 9, cleanTenantPolicy("Strict"), "motivo"
	_, err := f.h.Update(alice, up)
	assertCode(t, err, http.StatusConflict, errcode.CompliancePolicyVersionConflict)

	up.Body.ExpectedVersion = 1
	up.Body.Policy.LogContent.IPAddress = iface.IPAddressFull
	up.Body.AcknowledgeWarnings = true
	pending, err := f.h.Update(alice, up)
	if err != nil || pending.Status != http.StatusAccepted {
		t.Fatalf("update with warnings = %+v, %v", pending, err)
	}
	ap := &DecideChangeRequestInput{ID: pending.Body.ChangeRequest.UUID}
	ap.Body.Note = "ok"
	_, err = f.h.Approve(alice, ap)
	assertCode(t, err, http.StatusConflict, errcode.ComplianceChangeRequestSelfApproval)

	out, err := f.h.Approve(adminTenantCtx("bob", "internal-1"), ap)
	if err != nil || !out.Body.Applied || out.Body.Policy.Version != 2 {
		t.Fatalf("approve = %+v, %v", out, err)
	}
	_, err = f.h.Approve(adminTenantCtx("carol", "internal-1"), ap)
	assertCode(t, err, http.StatusConflict, errcode.ComplianceChangeRequestNotPending)
}

func TestPolicyHandler_ReadsAndValidate(t *testing.T) {
	f := newPolicyFixture(t)
	ctx := adminTenantCtx("alice", "internal-1")

	_, err := f.h.Effective(ctx, &EffectiveInput{TenantID: "t1"})
	assertCode(t, err, http.StatusServiceUnavailable, errcode.CompliancePolicyUnavailable)
	if err := f.policies.Refresh(ctx); err != nil {
		t.Fatal(err)
	}
	eff, err := f.h.Effective(ctx, &EffectiveInput{TenantID: "t1"})
	if err != nil || eff.Body.Source != services.EffectiveSourcePlatform || len(eff.Body.Retention) != 5 {
		t.Fatalf("effective = %+v, %v", eff, err)
	}

	v := &ValidatePolicyInput{}
	v.Body.Policy = cleanTenantPolicy("")
	res, err := f.h.Validate(ctx, v)
	if err != nil || len(res.Body.Errors) == 0 || res.Body.Warnings == nil {
		t.Fatalf("validate = %+v, %v", res, err)
	}

	classes, _ := f.h.RetentionClasses(ctx, &struct{}{})
	if len(classes.Body.Items) != 5 {
		t.Fatalf("classes = %d", len(classes.Body.Items))
	}
	list, err := f.h.List(ctx, &struct{}{})
	if err != nil || len(list.Body.Items) != 1 {
		t.Fatalf("list = %+v, %v", list, err)
	}
	_, err = f.h.Get(ctx, &PolicyIDInput{ID: "ghost"})
	assertCode(t, err, http.StatusNotFound, errcode.CompliancePolicyNotFound)
}

func TestPolicyHandler_AssignmentRoutes(t *testing.T) {
	f := newPolicyFixture(t)
	ctx := adminTenantCtx("alice", "internal-1")
	c := &CreatePolicyInput{}
	c.Body.Policy, c.Body.Reason = cleanTenantPolicy("Strict"), "motivo"
	created, _ := f.h.Create(ctx, c)

	ghost := &AssignPolicyInput{TenantID: "ghost"}
	ghost.Body.PolicyID, ghost.Body.Reason = created.Body.Policy.UUID, "motivo"
	_, err := f.h.Assign(ctx, ghost)
	assertCode(t, err, http.StatusNotFound, errcode.ComplianceTenantNotFound)

	as := &AssignPolicyInput{TenantID: "t1"}
	as.Body.PolicyID, as.Body.Reason = created.Body.Policy.UUID, "contratto sanità"
	out, err := f.h.Assign(ctx, as)
	if err != nil || out.Status != http.StatusOK || !out.Body.Applied {
		t.Fatalf("assign = %+v, %v", out, err)
	}
	pre := &ValidateAssignmentInput{TenantID: "t1"} // removal preview
	res, err := f.h.ValidateAssignment(ctx, pre)
	if err != nil || len(res.Body.Warnings) == 0 {
		t.Fatalf("removal preview = %+v, %v", res, err)
	}
	un := &UnassignPolicyInput{TenantID: "t1"}
	un.Body.Reason = "fine contratto"
	_, err = f.h.Unassign(ctx, un)
	assertCode(t, err, http.StatusUnprocessableEntity, errcode.CompliancePolicyWarningsUnacknowledged)
}

func TestTenantPolicyHandler_OwnTenantOnly(t *testing.T) {
	f := newPolicyFixture(t)
	if err := f.policies.Refresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	client := testkit.NewIdentity("client-user", "client@example.com", "").
		WithTenant("t1", []string{"org_viewer"}, true).
		ContextFor(context.Background(), "t1")
	_, err := f.tenant.Get(client, &ClientPolicyInput{TenantID: "t2"})
	assertCode(t, err, http.StatusNotFound, errcode.ComplianceTenantNotFound)

	out, err := f.tenant.Get(client, &ClientPolicyInput{TenantID: "t1"})
	if err != nil || out.Body.IPAddress != iface.IPAddressTruncated {
		t.Fatalf("summary = %+v, %v", out, err)
	}
	if len(out.Body.Retention) != 3 {
		t.Fatalf("retention classes shown to the client = %v, want only the three tenant-settable ones", out.Body.Retention)
	}
	if _, ok := out.Body.Retention[iface.RetentionComplianceEvidence]; ok {
		t.Fatal("a platform-only class leaked to the client summary")
	}
}

func TestMapPolicyError_UnknownIsAServerFault(t *testing.T) {
	err := mapPolicyError(context.Background(), errors.New("mongo: connection reset"))
	assertCode(t, err, http.StatusInternalServerError, errcode.CompliancePolicyPersistenceFailed)
	var ce *errcode.Error
	errors.As(err, &ce)
	if ce.Detail == "mongo: connection reset" {
		t.Fatal("the raw error text reached the response")
	}
}
