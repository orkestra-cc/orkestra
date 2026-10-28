package services

import (
	"context"
	"reflect"
	"testing"
	"time"

	"github.com/orkestra/backend/internal/core/tenant/models"
	"github.com/orkestra/backend/pkg/sdk/iface"
)

func TestListActiveTenantIDs_ReturnsLiveActiveTenantsOfKind(t *testing.T) {
	db, cleanup := newSetupTenantTestDB(t)
	defer cleanup()
	repo, svc, _, _ := newSetupTenantService(db)
	ctx := context.Background()
	sfx := randSuffix(t)

	seed := func(id string, kind models.TenantKind, status models.TenantStatus, deleted bool) {
		tn := &models.Tenant{
			UUID:          id + sfx,
			Kind:          kind,
			Status:        status,
			Name:          "Tenant " + id + sfx,
			Slug:          id + sfx,
			OwnerUserUUID: "owner-" + sfx,
			Plan:          models.PlanEnterprise,
		}
		if deleted {
			now := time.Now()
			tn.DeletedAt = &now
		}
		seedRawTenant(t, repo, tn)
	}
	seed("i-b", models.TenantKindInternal, models.TenantStatusActive, false)
	seed("i-a", models.TenantKindInternal, models.TenantStatusActive, false)
	seed("i-s", models.TenantKindInternal, models.TenantStatusSuspended, false)
	seed("i-d", models.TenantKindInternal, models.TenantStatusActive, true)
	seed("e-a", models.TenantKindExternal, models.TenantStatusActive, false)

	got, err := svc.ListActiveTenantIDs(ctx, iface.TenantKindInternal)
	if err != nil {
		t.Fatalf("ListActiveTenantIDs internal: %v", err)
	}
	if want := []string{"i-a" + sfx, "i-b" + sfx}; !reflect.DeepEqual(got, want) {
		t.Fatalf("internal = %v, want %v", got, want)
	}

	got, err = svc.ListActiveTenantIDs(ctx, iface.TenantKindExternal)
	if err != nil {
		t.Fatalf("ListActiveTenantIDs external: %v", err)
	}
	if want := []string{"e-a" + sfx}; !reflect.DeepEqual(got, want) {
		t.Fatalf("external = %v, want %v", got, want)
	}

	if _, err := svc.ListActiveTenantIDs(ctx, "bogus"); err == nil {
		t.Fatal("expected an error for an unknown kind")
	}
}
