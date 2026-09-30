package services

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/orkestra/backend/internal/core/compliance/models"
	"github.com/orkestra/backend/internal/core/compliance/repository"
	"github.com/orkestra/backend/internal/core/compliance/services/policytest"
	"github.com/orkestra/backend/pkg/sdk/iface"
)

var (
	alice = Actor{UserID: "alice", TenantID: "internal-1", IP: "203.0.113.7"}
	bob   = Actor{UserID: "bob", TenantID: "internal-1", IP: "203.0.113.8"}
)

type adminFixture struct {
	admin    *PolicyAdminService
	policies *PolicyService
	repo     *policytest.MemRepo
	sink     *policytest.Sink
	fourEyes bool
	now      time.Time
}

func newAdminFixture(t *testing.T) *adminFixture {
	t.Helper()
	f := &adminFixture{repo: policytest.NewMemRepo(), sink: &policytest.Sink{}, fourEyes: true, now: testNow}
	logger := slog.New(slog.DiscardHandler)
	f.policies = NewPolicyService(f.repo, logger)
	f.policies.now = func() time.Time { return f.now }
	tenants := policytest.Tenants{
		"t1": {UUID: "t1", Kind: iface.TenantKindExternal, Name: "Clinica"},
		"t2": {UUID: "t2", Kind: iface.TenantKindInternal, Name: "Interno"},
	}
	f.admin = NewPolicyAdminService(f.repo, f.policies, tenants, f.sink, func(context.Context) bool { return f.fourEyes }, logger)
	f.admin.now = func() time.Time { return f.now }
	n := 0
	f.admin.newUUID = func() string { n++; return fmt.Sprintf("id-%d", n) }
	if err := f.admin.EnsurePlatformPolicy(context.Background()); err != nil {
		t.Fatal(err)
	}
	return f
}

func (f *adminFixture) platformUUID(t *testing.T) string {
	p, err := f.repo.GetPlatformPolicy(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	return p.UUID
}

// createStrict creates, without warnings, a tenant policy stricter than the
// platform: IP omitted, privileged_change at 730 days.
func (f *adminFixture) createStrict(t *testing.T, name string) *models.Policy {
	t.Helper()
	in := tenantInput()
	in.Name = name
	in.LogContent.IPAddress = iface.IPAddressOmitted
	res, err := f.admin.Create(context.Background(), alice, in, "richiesta del cliente", false)
	if err != nil || !res.Applied {
		t.Fatalf("Create(%s) = %+v, %v", name, res, err)
	}
	return res.Policy
}

func TestEnsurePlatformPolicy_Idempotent(t *testing.T) {
	f := newAdminFixture(t)
	if err := f.admin.EnsurePlatformPolicy(context.Background()); err != nil {
		t.Fatal(err)
	}
	ps, _ := f.repo.ListPolicies(context.Background())
	if len(ps) != 1 || !ps[0].IsPlatformDefault || ps[0].CreatedBy != models.SystemActor {
		t.Fatalf("policies = %+v", ps)
	}
	if v := f.repo.Versions(); len(v) != 1 || v[0].ChangeKind != models.VersionCreate {
		t.Fatalf("versions = %+v", v)
	}
	if got := f.sink.Actions(); !slices.Equal(got, []string{"compliance.policy.created"}) {
		t.Fatalf("audit = %v", got)
	}
}

func TestCreate_AppliesAChangeWithoutWarnings(t *testing.T) {
	f := newAdminFixture(t)
	p := f.createStrict(t, "Strict")
	if p.Version != 1 || p.CreatedBy != "alice" || p.Sinks != nil {
		t.Fatalf("policy = %+v", p)
	}
	ev := f.sink.Events()
	last := ev[len(ev)-1]
	if last.Action != "compliance.policy.created" || last.ActorUserID != "alice" || last.TenantID != "internal-1" ||
		last.IPAddress != "203.0.113.7" || last.Metadata["after"] == nil {
		t.Fatalf("audit event = %+v", last)
	}
}

func TestCreate_Refusals(t *testing.T) {
	f := newAdminFixture(t)
	ctx := context.Background()
	if _, err := f.admin.Create(ctx, alice, tenantInput(), "  ", false); !errors.Is(err, ErrReasonRequired) {
		t.Fatalf("empty reason: %v", err)
	}
	bad := tenantInput()
	bad.Retention[iface.RetentionAdminAccess] = 100
	var verr *ValidationError
	if _, err := f.admin.Create(ctx, alice, bad, "motivo", true); !errors.As(err, &verr) || !verr.Result.HasError(IssueClassBelowMinimum) {
		t.Fatalf("below the AdS floor: %v", err)
	}
	f.createStrict(t, "Strict")
	dup := tenantInput()
	dup.Name = "Strict"
	if _, err := f.admin.Create(ctx, alice, dup, "motivo", false); !errors.As(err, &verr) || !verr.Result.HasError(IssueNameTaken) {
		t.Fatalf("duplicate name: %v", err)
	}
}

func TestCreate_WarningsNeedAcknowledgementAndFourEyes(t *testing.T) {
	f := newAdminFixture(t)
	ctx := context.Background()
	in := tenantInput()
	in.LogContent.IPAddress = iface.IPAddressFull // ip_full + less_restrictive_than_platform
	var werr *WarningsError
	if _, err := f.admin.Create(ctx, alice, in, "motivo", false); !errors.As(err, &werr) || !slices.Contains(werr.Codes, WarnIPFull) {
		t.Fatalf("unacknowledged warnings: %v", err)
	}
	res, err := f.admin.Create(ctx, alice, in, "chiesto da mario.rossi@example.com", true)
	if err != nil || res.Applied || res.ChangeRequest == nil {
		t.Fatalf("four eyes: %+v, %v", res, err)
	}
	cr := res.ChangeRequest
	if cr.Status != models.ChangeStatusPending || cr.RequestedBy != "alice" || cr.Kind != models.ChangeCreate ||
		!slices.Contains(cr.Warnings, WarnIPFull) {
		t.Fatalf("change request = %+v", cr)
	}
	if strings.Contains(cr.Reason, "mario.rossi") || !strings.Contains(cr.Reason, "[EMAIL]") {
		t.Fatalf("reason not masked: %q", cr.Reason)
	}
	if ps, _ := f.repo.ListPolicies(ctx); len(ps) != 1 {
		t.Fatal("a change with warnings was applied under four eyes")
	}
	f.fourEyes = false
	in.Name = "Aperta"
	if res, err := f.admin.Create(ctx, alice, in, "motivo", true); err != nil || !res.Applied {
		t.Fatalf("four eyes off: %+v, %v", res, err)
	}
}

func TestApprove(t *testing.T) {
	f := newAdminFixture(t)
	ctx := context.Background()
	in := tenantInput()
	in.LogContent.IPAddress = iface.IPAddressFull
	res, _ := f.admin.Create(ctx, alice, in, "motivo", true)
	id := res.ChangeRequest.UUID

	if _, err := f.admin.Approve(ctx, alice, id, "ok"); !errors.Is(err, ErrSelfApproval) {
		t.Fatalf("self approval: %v", err)
	}
	if _, err := f.admin.Approve(ctx, bob, id, " "); !errors.Is(err, ErrReasonRequired) {
		t.Fatalf("empty note: %v", err)
	}
	out, err := f.admin.Approve(ctx, bob, id, "verificato con il DPO")
	if err != nil || !out.Applied || out.Policy == nil {
		t.Fatalf("Approve = %+v, %v", out, err)
	}
	if out.Policy.CreatedBy != "alice" {
		t.Fatalf("author = %s, want the requester", out.Policy.CreatedBy)
	}
	cr, _ := f.repo.GetChangeRequest(ctx, id)
	if cr.Status != models.ChangeStatusApproved || cr.DecidedBy != "bob" || cr.DecisionNote != "verificato con il DPO" {
		t.Fatalf("request = %+v", cr)
	}
	var v models.PolicyVersion
	for _, x := range f.repo.Versions() {
		if x.PolicyUUID == out.Policy.UUID {
			v = x
		}
	}
	if v.ApprovedBy != "bob" || v.ChangeRequestUUID != id || v.ChangedBy != "alice" {
		t.Fatalf("version = %+v", v)
	}
	actions := f.sink.Actions()
	if !slices.Contains(actions, "compliance.change_request.approved") {
		t.Fatalf("audit = %v", actions)
	}
	if _, err := f.admin.Approve(ctx, bob, id, "ancora"); !errors.Is(err, repository.ErrChangeRequestNotPending) {
		t.Fatalf("second approval: %v", err)
	}
}

func TestApprove_StaleRequestIsSuperseded(t *testing.T) {
	f := newAdminFixture(t)
	ctx := context.Background()
	p := f.createStrict(t, "Strict")
	looser := p.Input()
	looser.LogContent.IPAddress = iface.IPAddressFull
	res, err := f.admin.Update(ctx, alice, p.UUID, 1, looser, "motivo", true)
	if err != nil || res.ChangeRequest == nil {
		t.Fatalf("Update with warnings = %+v, %v", res, err)
	}
	// Meanwhile a change without warnings moves the policy to version 2.
	edit := p.Input()
	edit.Description = "nuova descrizione"
	if res, err := f.admin.Update(ctx, bob, p.UUID, 1, edit, "descrizione", false); err != nil || !res.Applied {
		t.Fatalf("direct update = %+v, %v", res, err)
	}
	if _, err := f.admin.Approve(ctx, bob, res.ChangeRequest.UUID, "ok"); !errors.Is(err, ErrChangeRequestSuperseded) {
		t.Fatalf("stale approval: %v", err)
	}
	cr, _ := f.repo.GetChangeRequest(ctx, res.ChangeRequest.UUID)
	cur, _ := f.repo.GetPolicy(ctx, p.UUID)
	if cr.Status != models.ChangeStatusSuperseded || cur.Version != 2 || cur.LogContent.IPAddress != iface.IPAddressOmitted {
		t.Fatalf("request %s, policy v%d ip %s", cr.Status, cur.Version, cur.LogContent.IPAddress)
	}
}

func TestUpdate_WeakeningAPolicyWarns(t *testing.T) {
	f := newAdminFixture(t)
	p := f.createStrict(t, "Strict")
	weaker := p.Input()
	weaker.LogContent.IPAddress = iface.IPAddressTruncated // still no looser than the platform
	r, err := f.admin.ValidateDraft(context.Background(), p.UUID, weaker)
	if err != nil || !slices.Contains(r.WarningCodes(), WarnLessRestrictiveThanCurrent) {
		t.Fatalf("ValidateDraft = %+v, %v", r, err)
	}
}

func TestAssignAndUnassign(t *testing.T) {
	f := newAdminFixture(t)
	ctx := context.Background()
	p := f.createStrict(t, "Strict")

	// A stricter policy: no warnings, applied at once, live in the resolver.
	res, err := f.admin.Assign(ctx, alice, "t1", p.UUID, "contratto sanità", false)
	if err != nil || !res.Applied || res.Assignment.TenantKind != iface.TenantKindExternal {
		t.Fatalf("Assign = %+v, %v", res, err)
	}
	if f.policies.LogContentFor("t1").IPAddress != iface.IPAddressOmitted {
		t.Fatal("the resolver did not pick up the assignment")
	}

	// Back to the platform: weaker, so it needs a second operator.
	if _, err := f.admin.Unassign(ctx, alice, "t1", "fine contratto", false); err == nil {
		t.Fatal("weakening unassignment applied without acknowledgement")
	}
	res, err = f.admin.Unassign(ctx, alice, "t1", "fine contratto", true)
	if err != nil || res.Applied || !slices.Contains(res.ChangeRequest.Warnings, WarnLessRestrictiveThanCurrent) {
		t.Fatalf("Unassign = %+v, %v", res, err)
	}
	if _, err := f.admin.Approve(ctx, bob, res.ChangeRequest.UUID, "ok"); err != nil {
		t.Fatal(err)
	}
	if _, err := f.repo.GetAssignment(ctx, "t1"); !errors.Is(err, repository.ErrAssignmentNotFound) {
		t.Fatalf("assignment still there: %v", err)
	}
	if f.policies.LogContentFor("t1").IPAddress != iface.IPAddressTruncated {
		t.Fatal("the resolver still serves the removed assignment")
	}
	h := f.repo.History()
	if len(h) != 2 || h[1].Action != models.AssignmentActionUnassign || h[1].PreviousPolicyUUID != p.UUID || h[1].ApprovedBy != "bob" {
		t.Fatalf("history = %+v", h)
	}
}

func TestAssign_Refusals(t *testing.T) {
	f := newAdminFixture(t)
	ctx := context.Background()
	p := f.createStrict(t, "Strict")
	if _, err := f.admin.Assign(ctx, alice, "t1", f.platformUUID(t), "motivo", true); !errors.Is(err, ErrPlatformProtected) {
		t.Fatalf("assigning the platform policy: %v", err)
	}
	if _, err := f.admin.Assign(ctx, alice, "ghost", p.UUID, "motivo", true); !errors.Is(err, ErrTenantNotFound) {
		t.Fatalf("unknown tenant: %v", err)
	}
	if _, err := f.admin.Unassign(ctx, alice, "t2", "motivo", true); !errors.Is(err, repository.ErrAssignmentNotFound) {
		t.Fatalf("unassigning a tenant without assignment: %v", err)
	}
}

func TestAssign_StaleWhenTheTenantMovedMeanwhile(t *testing.T) {
	f := newAdminFixture(t)
	ctx := context.Background()
	a := f.createStrict(t, "A")
	b := f.createStrict(t, "B")
	// A policy with warnings (review overdue) so the assignment needs approval.
	stale := tenantInput()
	stale.Name = "Scaduta"
	stale.Accountability.ReviewDueAt = testNow.AddDate(0, 0, -1)
	f.fourEyes = false
	c, err := f.admin.Create(ctx, alice, stale, "motivo", true)
	if err != nil {
		t.Fatal(err)
	}
	f.fourEyes = true
	res, err := f.admin.Assign(ctx, alice, "t1", c.Policy.UUID, "motivo", true)
	if err != nil || res.ChangeRequest == nil {
		t.Fatalf("Assign with warnings = %+v, %v", res, err)
	}
	if _, err := f.admin.Assign(ctx, bob, "t1", a.UUID, "motivo", false); err != nil {
		t.Fatal(err)
	}
	if _, err := f.admin.Approve(ctx, bob, res.ChangeRequest.UUID, "ok"); !errors.Is(err, ErrChangeRequestSuperseded) {
		t.Fatalf("approval after the tenant moved: %v", err)
	}
	if got, _ := f.repo.GetAssignment(ctx, "t1"); got.PolicyUUID != a.UUID {
		t.Fatalf("assignment = %s, want %s", got.PolicyUUID, a.UUID)
	}
	_ = b
}

func TestDelete(t *testing.T) {
	f := newAdminFixture(t)
	ctx := context.Background()
	p := f.createStrict(t, "Strict")
	if err := f.admin.Delete(ctx, alice, f.platformUUID(t), 1, "motivo"); !errors.Is(err, ErrPlatformProtected) {
		t.Fatalf("deleting the platform policy: %v", err)
	}
	_, _ = f.admin.Assign(ctx, alice, "t1", p.UUID, "motivo", false)
	if err := f.admin.Delete(ctx, alice, p.UUID, 1, "motivo"); !errors.Is(err, ErrPolicyInUse) {
		t.Fatalf("deleting an assigned policy: %v", err)
	}
	f.fourEyes = false
	if _, err := f.admin.Unassign(ctx, alice, "t1", "motivo", true); err != nil {
		t.Fatal(err)
	}
	if err := f.admin.Delete(ctx, alice, p.UUID, 3, "motivo"); !errors.Is(err, repository.ErrPolicyVersionConflict) {
		t.Fatalf("stale delete: %v", err)
	}
	if err := f.admin.Delete(ctx, alice, p.UUID, 1, "non più usata"); err != nil {
		t.Fatal(err)
	}
	vs, _ := f.repo.ListVersions(ctx, p.UUID)
	if len(vs) != 2 || vs[0].ChangeKind != models.VersionDelete || vs[0].Version != 2 {
		t.Fatalf("versions = %+v", vs)
	}
}

func TestReject(t *testing.T) {
	f := newAdminFixture(t)
	ctx := context.Background()
	in := tenantInput()
	in.LogContent.IPAddress = iface.IPAddressFull
	res, _ := f.admin.Create(ctx, alice, in, "motivo", true)
	if _, err := f.admin.Reject(ctx, alice, res.ChangeRequest.UUID, "no"); !errors.Is(err, ErrSelfApproval) {
		t.Fatalf("self rejection: %v", err)
	}
	cr, err := f.admin.Reject(ctx, bob, res.ChangeRequest.UUID, "IP intero non giustificato")
	if err != nil || cr.Status != models.ChangeStatusRejected || cr.DecidedBy != "bob" {
		t.Fatalf("Reject = %+v, %v", cr, err)
	}
	if !slices.Contains(f.sink.Actions(), "compliance.change_request.rejected") {
		t.Fatalf("audit = %v", f.sink.Actions())
	}
}

func TestExpireStale(t *testing.T) {
	f := newAdminFixture(t)
	ctx := context.Background()
	in := tenantInput()
	in.LogContent.IPAddress = iface.IPAddressFull
	old, _ := f.admin.Create(ctx, alice, in, "motivo", true)
	f.now = testNow.Add(2 * 24 * time.Hour)
	in.Name = "Recente"
	recent, _ := f.admin.Create(ctx, alice, in, "motivo", true)
	f.now = testNow.Add(15 * 24 * time.Hour)
	n, err := f.admin.ExpireStale(ctx)
	if err != nil || n != 1 {
		t.Fatalf("ExpireStale = %d, %v", n, err)
	}
	if cr, _ := f.repo.GetChangeRequest(ctx, old.ChangeRequest.UUID); cr.Status != models.ChangeStatusExpired {
		t.Fatalf("old request = %s", cr.Status)
	}
	if cr, _ := f.repo.GetChangeRequest(ctx, recent.ChangeRequest.UUID); cr.Status != models.ChangeStatusPending {
		t.Fatalf("recent request = %s", cr.Status)
	}
}

func TestListPolicies(t *testing.T) {
	f := newAdminFixture(t)
	ctx := context.Background()
	p := f.createStrict(t, "Strict")
	_, _ = f.admin.Assign(ctx, alice, "t1", p.UUID, "motivo", false)
	views, err := f.admin.ListPolicies(ctx)
	if err != nil || len(views) != 2 {
		t.Fatalf("ListPolicies = %+v, %v", views, err)
	}
	for _, v := range views {
		switch {
		case v.IsPlatformDefault:
			if v.LessRestrictiveFields == nil || len(v.LessRestrictiveFields) != 0 || v.ReviewOverdue {
				t.Fatalf("platform view = %+v", v)
			}
		default:
			if v.AssignedTenants != 1 {
				t.Fatalf("tenant view = %+v", v)
			}
		}
	}
	d, err := f.admin.GetPolicy(ctx, p.UUID)
	if err != nil || len(d.Assignments) != 1 || d.Assignments[0].TenantName != "Clinica" {
		t.Fatalf("GetPolicy = %+v, %v", d, err)
	}
}
