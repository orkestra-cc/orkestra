package policytest

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/orkestra/backend/internal/core/compliance/models"
	"github.com/orkestra/backend/internal/core/compliance/repository"
)

func TestMemRepoMirrorsTheRepository(t *testing.T) {
	m := NewMemRepo()
	ctx := context.Background()
	p := models.NewPlatformPolicy("plat", time.Now())
	if err := m.InsertPolicy(ctx, &p); err != nil {
		t.Fatal(err)
	}
	q := p
	q.UUID, q.Name = "plat2", "Other"
	if err := m.InsertPolicy(ctx, &q); !errors.Is(err, repository.ErrPlatformPolicyExists) {
		t.Fatalf("second platform: %v", err)
	}
	next := p
	next.Version = 2
	if err := m.ReplacePolicy(ctx, &next, 5); !errors.Is(err, repository.ErrPolicyVersionConflict) {
		t.Fatalf("stale replace: %v", err)
	}
	got, _ := m.GetPolicy(ctx, "plat")
	got.LogContent.PIIKeys[0] = "mutated"
	if again, _ := m.GetPolicy(ctx, "plat"); again.LogContent.PIIKeys[0] == "mutated" {
		t.Fatal("GetPolicy returns shared state")
	}
	cr := models.PolicyChangeRequest{UUID: "cr", Status: models.ChangeStatusPending}
	_ = m.InsertChangeRequest(ctx, &cr)
	if err := m.DecideChangeRequest(ctx, "cr", models.ChangeStatusApproved, "b", "ok", time.Now()); err != nil {
		t.Fatal(err)
	}
	if err := m.DecideChangeRequest(ctx, "cr", models.ChangeStatusApproved, "c", "ok", time.Now()); !errors.Is(err, repository.ErrChangeRequestNotPending) {
		t.Fatalf("second decision: %v", err)
	}
}

// The real repository returns [] (never null) when nothing matches: the
// console receives an empty array, so the double must do the same.
func TestMemRepoEmptyListsAreNonNil(t *testing.T) {
	m := NewMemRepo()
	ctx := context.Background()
	policies, err := m.ListPolicies(ctx)
	if err != nil || policies == nil {
		t.Errorf("ListPolicies = %#v, %v; want non-nil empty", policies, err)
	}
	versions, err := m.ListVersions(ctx, "none")
	if err != nil || versions == nil {
		t.Errorf("ListVersions = %#v, %v; want non-nil empty", versions, err)
	}
	assignments, err := m.ListAssignments(ctx)
	if err != nil || assignments == nil {
		t.Errorf("ListAssignments = %#v, %v; want non-nil empty", assignments, err)
	}
	requests, err := m.ListChangeRequests(ctx, "")
	if err != nil || requests == nil {
		t.Errorf("ListChangeRequests = %#v, %v; want non-nil empty", requests, err)
	}
	pending, err := m.ListPendingBefore(ctx, time.Now())
	if err != nil || pending == nil {
		t.Errorf("ListPendingBefore = %#v, %v; want non-nil empty", pending, err)
	}
	if reqs := m.Requests(); reqs == nil {
		t.Error("Requests() = nil, want non-nil empty")
	}
}
