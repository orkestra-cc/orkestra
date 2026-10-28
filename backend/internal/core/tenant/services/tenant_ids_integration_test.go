package services

import (
	"context"
	"reflect"
	"testing"
	"time"

	"github.com/orkestra/backend/internal/core/tenant/models"
	"github.com/orkestra/backend/internal/core/tenant/repository"
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

func TestListPurgedTenantIDs_IncludesSoftDeletedPurged(t *testing.T) {
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
	seed("a-active", models.TenantKindInternal, models.TenantStatusActive, false)
	seed("b-archived", models.TenantKindExternal, models.TenantStatusArchived, false)
	seed("z-purged", models.TenantKindInternal, models.TenantStatusPurged, false)
	seed("m-purged-deleted", models.TenantKindExternal, models.TenantStatusPurged, true)

	got, err := svc.ListPurgedTenantIDs(ctx)
	if err != nil {
		t.Fatalf("ListPurgedTenantIDs: %v", err)
	}
	if want := []string{"m-purged-deleted" + sfx, "z-purged" + sfx}; !reflect.DeepEqual(got, want) {
		t.Fatalf("purged = %v, want %v", got, want)
	}
}

func TestPurgeTenant_IsListedAsPurgedWithoutAnyHook(t *testing.T) {
	db, cleanup := newDefaultsTestDB(t)
	defer cleanup()
	repo := repository.New(db)
	svc := New(repo)
	ctx := context.Background()
	tn := seedDefaultsTenant(t, repo, nil)

	if err := svc.PurgeTenant(ctx, tn.UUID); err != nil {
		t.Fatalf("PurgeTenant: %v", err)
	}
	got, err := svc.ListPurgedTenantIDs(ctx)
	if err != nil {
		t.Fatalf("ListPurgedTenantIDs: %v", err)
	}
	if want := []string{tn.UUID}; !reflect.DeepEqual(got, want) {
		t.Fatalf("purged = %v, want %v", got, want)
	}
}
