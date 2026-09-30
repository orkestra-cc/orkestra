package services

import (
	"context"
	"errors"
	"log/slog"
	"slices"
	"sync/atomic"
	"testing"
	"time"

	"github.com/orkestra/backend/internal/core/compliance/models"
	"github.com/orkestra/backend/internal/core/compliance/services/policytest"
	"github.com/orkestra/backend/pkg/sdk/iface"
)

func newPolicyService(t *testing.T) (*PolicyService, *policytest.MemRepo) {
	t.Helper()
	repo := policytest.NewMemRepo()
	plat := models.NewPlatformPolicy("platform", testNow)
	if err := repo.InsertPolicy(context.Background(), &plat); err != nil {
		t.Fatal(err)
	}
	s := NewPolicyService(repo, slog.New(slog.DiscardHandler))
	s.now = func() time.Time { return testNow }
	return s, repo
}

// strictPolicy omits IPs, adds a PII key and shortens privileged_change.
func strictPolicy() models.Policy {
	p := models.Policy{UUID: "strict", Name: "Strict", Version: 1, LogContent: iface.DefaultLogContentPolicy(),
		Retention: map[iface.RetentionClass]int{iface.RetentionPrivilegedChange: 400}}
	p.LogContent.IPAddress = iface.IPAddressOmitted
	p.LogContent.PIIKeys = append(p.LogContent.PIIKeys, "badge")
	return p
}

func assign(t *testing.T, repo *policytest.MemRepo, tenant, policy string) {
	t.Helper()
	if err := repo.UpsertAssignment(context.Background(), &models.PolicyAssignment{TenantID: tenant, PolicyUUID: policy, AssignedAt: testNow}); err != nil {
		t.Fatal(err)
	}
}

func TestPolicyService_BeforeFirstRefresh(t *testing.T) {
	s, _ := newPolicyService(t)
	if s.LogContentFor("t1") != &staticDefaultContent || s.LogContentFor("") != &staticDefaultContent {
		t.Fatal("static defaults not served before the first snapshot")
	}
	d := s.RetentionFor("t1", iface.RetentionAdminAccess)
	if d.Days != 365 || d.PolicyUUID != "" {
		t.Fatalf("RetentionFor = %+v", d)
	}
	if _, ok := s.Effective("t1"); ok {
		t.Fatal("Effective answered without a snapshot")
	}
}

func TestPolicyService_Resolution(t *testing.T) {
	s, repo := newPolicyService(t)
	strict := strictPolicy()
	// A platform-only class on a tenant policy (validation forbids it) must
	// still be ignored by the resolver.
	strict.Retention[iface.RetentionComplianceEvidence] = 10
	_ = repo.InsertPolicy(context.Background(), &strict)
	assign(t, repo, "t1", "strict")
	if err := s.Refresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := s.LogContentFor("t1"); got.IPAddress != iface.IPAddressOmitted {
		t.Fatalf("t1 ip = %s", got.IPAddress)
	}
	if got := s.LogContentFor("t2"); got.IPAddress != iface.IPAddressTruncated {
		t.Fatalf("unassigned tenant ip = %s", got.IPAddress)
	}
	strictest := s.LogContentFor("")
	if strictest.IPAddress != iface.IPAddressOmitted || !slices.Contains(strictest.PIIKeys, "badge") || !slices.Contains(strictest.PIIKeys, "email") {
		t.Fatalf("strictest = %+v", strictest)
	}
	cases := []struct {
		tenant string
		class  iface.RetentionClass
		want   iface.RetentionDecision
	}{
		{"t1", iface.RetentionPrivilegedChange, iface.RetentionDecision{Class: iface.RetentionPrivilegedChange, PolicyUUID: "strict", PolicyVersion: 1, Days: 400}},
		{"t1", iface.RetentionComplianceEvidence, iface.RetentionDecision{Class: iface.RetentionComplianceEvidence, PolicyUUID: "platform", PolicyVersion: 1, Days: 1825}},
		{"t1", iface.RetentionAdminAccess, iface.RetentionDecision{Class: iface.RetentionAdminAccess, PolicyUUID: "platform", PolicyVersion: 1, Days: 365}},
		{"t2", iface.RetentionPrivilegedChange, iface.RetentionDecision{Class: iface.RetentionPrivilegedChange, PolicyUUID: "platform", PolicyVersion: 1, Days: 730}},
		// An unknown class falls on privileged_change, never on 0 days.
		{"t2", "bogus", iface.RetentionDecision{Class: iface.RetentionPrivilegedChange, PolicyUUID: "platform", PolicyVersion: 1, Days: 730}},
	}
	for _, c := range cases {
		if got := s.RetentionFor(c.tenant, c.class); got != c.want {
			t.Errorf("RetentionFor(%s, %s) = %+v, want %+v", c.tenant, c.class, got, c.want)
		}
	}
	eff, ok := s.Effective("t1")
	if !ok || eff.Source != EffectiveSourceAssigned || eff.Policy.UUID != "strict" || eff.Assignment == nil || len(eff.Retention) != 5 {
		t.Fatalf("Effective(t1) = %+v", eff)
	}
	if eff, _ := s.Effective("t2"); eff.Source != EffectiveSourcePlatform || eff.Assignment != nil {
		t.Fatalf("Effective(t2) = %+v", eff)
	}
}

func TestPolicyService_PointersStableUntilTheVersionChanges(t *testing.T) {
	s, repo := newPolicyService(t)
	strict := strictPolicy()
	_ = repo.InsertPolicy(context.Background(), &strict)
	assign(t, repo, "t1", "strict")
	_ = s.Refresh(context.Background())
	t1, t2, all := s.LogContentFor("t1"), s.LogContentFor("t2"), s.LogContentFor("")
	_ = s.Refresh(context.Background())
	if s.LogContentFor("t1") != t1 || s.LogContentFor("t2") != t2 || s.LogContentFor("") != all {
		t.Fatal("pointers changed on a refresh without changes")
	}
	next := strict
	next.Version = 2
	next.LogContent.IPAddress = iface.IPAddressHashed
	if err := repo.ReplacePolicy(context.Background(), &next, 1); err != nil {
		t.Fatal(err)
	}
	_ = s.Refresh(context.Background())
	if s.LogContentFor("t1") == t1 || s.LogContentFor("t1").IPAddress != iface.IPAddressHashed {
		t.Fatal("t1 did not get version 2")
	}
	if s.LogContentFor("t2") != t2 {
		t.Fatal("platform pointer changed although the platform policy did not")
	}
}

type countingHandler struct{ n *atomic.Int32 }

func (h countingHandler) Enabled(context.Context, slog.Level) bool  { return true }
func (h countingHandler) Handle(context.Context, slog.Record) error { h.n.Add(1); return nil }
func (h countingHandler) WithAttrs([]slog.Attr) slog.Handler        { return h }
func (h countingHandler) WithGroup(string) slog.Handler             { return h }

func TestPolicyService_RefreshFailureKeepsTheSnapshot(t *testing.T) {
	s, repo := newPolicyService(t)
	var warns atomic.Int32
	s.logger = slog.New(countingHandler{n: &warns})
	strict := strictPolicy()
	_ = repo.InsertPolicy(context.Background(), &strict)
	assign(t, repo, "t1", "strict")
	_ = s.Refresh(context.Background())
	t1 := s.LogContentFor("t1")

	repo.SetListErr(errors.New("mongo down"))
	for range 3 {
		if err := s.Refresh(context.Background()); err == nil {
			t.Fatal("Refresh hid the failure")
		}
	}
	if s.LogContentFor("t1") != t1 {
		t.Fatal("a failed refresh replaced the snapshot")
	}
	if warns.Load() != 1 {
		t.Fatalf("%d warnings in one minute, want 1", warns.Load())
	}
	s.now = func() time.Time { return testNow.Add(61 * time.Second) }
	_ = s.Refresh(context.Background())
	if warns.Load() != 2 {
		t.Fatalf("%d warnings after a minute, want 2", warns.Load())
	}
	if age, ok := s.SnapshotAge(); !ok || age != 61*time.Second {
		t.Fatalf("SnapshotAge = %v, %v", age, ok)
	}
}

func TestPolicyService_NoPlatformPolicy(t *testing.T) {
	repo := policytest.NewMemRepo()
	s := NewPolicyService(repo, slog.New(slog.DiscardHandler))
	if err := s.Refresh(context.Background()); err == nil {
		t.Fatal("a catalog without the platform policy was accepted")
	}
	if s.LogContentFor("t1") != &staticDefaultContent {
		t.Fatal("static defaults not served")
	}
}

func TestPolicyService_DanglingAssignmentGetsTheStrictest(t *testing.T) {
	s, repo := newPolicyService(t)
	strict := strictPolicy()
	_ = repo.InsertPolicy(context.Background(), &strict)
	assign(t, repo, "t1", "strict")
	assign(t, repo, "t9", "ghost")
	_ = s.Refresh(context.Background())
	if s.LogContentFor("t9") != s.LogContentFor("") {
		t.Fatal("a tenant assigned to a missing policy is not on the strictest policy")
	}
}

func TestPolicyService_SnapshotAgeHook(t *testing.T) {
	s, _ := newPolicyService(t)
	var got float64 = -1
	s.SetSnapshotAgeHook(func(v float64) { got = v })
	_ = s.Refresh(context.Background())
	s.now = func() time.Time { return testNow.Add(7 * time.Second) }
	s.reportAge()
	if got != 7 {
		t.Fatalf("age hook = %v, want 7", got)
	}
}

func TestPolicyService_SnapshotAgeBeforeTheFirstLoad(t *testing.T) {
	// No platform policy: Refresh always fails, so no snapshot ever loads.
	s := NewPolicyService(policytest.NewMemRepo(), slog.New(slog.DiscardHandler))
	var got float64 = -1
	s.SetSnapshotAgeHook(func(v float64) { got = v })
	s.startedAt = testNow
	s.now = func() time.Time { return testNow.Add(90 * time.Second) }
	if err := s.Refresh(context.Background()); err == nil {
		t.Fatal("a catalog without the platform policy was accepted")
	}
	s.reportAge()
	if got != 90 {
		t.Fatalf("age hook = %v, want 90 (time since start, no snapshot yet)", got)
	}
	if _, ok := s.SnapshotAge(); ok {
		t.Fatal("SnapshotAge must keep reporting false without a snapshot")
	}
}
