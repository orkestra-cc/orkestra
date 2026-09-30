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
