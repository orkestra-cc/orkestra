package services

import (
	"context"
	"testing"
	"time"

	"github.com/orkestra/backend/internal/core/tenant/models"
	"github.com/orkestra/backend/internal/core/tenant/repository"
)

// IsMember must agree with ListUserMemberships: a membership past its
// ExpiresAt no longer counts, one without ExpiresAt or expiring later does.
func TestIsMember_HonoursExpiresAt(t *testing.T) {
	db, cleanup := newDefaultsTestDB(t)
	defer cleanup()
	ctx := context.Background()
	repo := repository.New(db)
	svc := New(repo)

	past := time.Now().Add(-time.Hour)
	future := time.Now().Add(time.Hour)
	for _, m := range []models.TenantMembership{
		{UUID: "m-open", UserUUID: "u1", TenantUUID: "t-open"},
		{UUID: "m-future", UserUUID: "u1", TenantUUID: "t-future", ExpiresAt: &future},
		{UUID: "m-past", UserUUID: "u1", TenantUUID: "t-past", ExpiresAt: &past},
	} {
		if err := repo.CreateMembership(ctx, &m); err != nil {
			t.Fatalf("CreateMembership(%s): %v", m.UUID, err)
		}
	}

	for tenant, want := range map[string]bool{
		"t-open":   true,
		"t-future": true,
		"t-past":   false,
		"t-none":   false,
	} {
		got, err := svc.IsMember(ctx, "u1", tenant)
		if err != nil {
			t.Fatalf("IsMember(%s): %v", tenant, err)
		}
		if got != want {
			t.Errorf("IsMember(%s) = %v, want %v", tenant, got, want)
		}
	}
}
