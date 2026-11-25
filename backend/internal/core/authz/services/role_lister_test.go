package services

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/orkestra/backend/pkg/sdk/iface"
	"github.com/orkestra/backend/pkg/sdk/module"
)

func TestService_ImplementsAuthzRoleLister(t *testing.T) {
	var _ iface.AuthzRoleLister = (*Service)(nil)
}

func TestAuthzRoleLister_ResolvesFromServiceAuthzProvider(t *testing.T) {
	svc, _ := newTier1Service(t, staticRoleLookup("operator"))
	reg := module.NewServiceRegistry()
	reg.Register(module.ServiceAuthzProvider, iface.AuthzProvider(svc))
	if _, ok := module.GetTyped[iface.AuthzRoleLister](reg, module.ServiceAuthzProvider); !ok {
		t.Fatal("AuthzRoleLister not resolvable from ServiceAuthzProvider")
	}
}

func TestActiveRoleIDs_ListsActiveRolesOfLiveBindingsInTheTenant(t *testing.T) {
	svc, repo := newTier1Service(t, staticRoleLookup("operator"))
	repo.seedRole("r-sys", "org_member", true, nil, "")
	repo.seedRole("r-a", "sales", false, nil, "tenant-X")
	repo.seedRole("r-off", "old", false, nil, "tenant-X")
	repo.seedRole("r-exp", "trial", false, nil, "tenant-X")
	repo.seedRole("r-y", "ops", false, nil, "tenant-Y")
	repo.seedRole("r-g", "auditor", false, nil, "")
	repo.seedBinding("b1", "u-1", "tenant-X", "r-sys")
	repo.seedBinding("b2", "u-1", "tenant-X", "r-a")
	repo.seedBinding("b2dup", "u-1", "tenant-X", "r-a")
	repo.seedBinding("b3", "u-1", "tenant-X", "r-off")
	repo.seedBinding("b4", "u-1", "tenant-X", "r-exp")
	repo.seedBinding("b5", "u-1", "tenant-Y", "r-y")
	repo.seedBinding("b6", "u-1", "", "r-g")
	off := repo.roles["r-off"]
	off.IsActive = false
	repo.roles["r-off"] = off
	past := time.Now().Add(-time.Hour)
	b := repo.bindings["b4"]
	b.ExpiresAt = &past
	repo.bindings["b4"] = b

	got, err := svc.ActiveRoleIDs(context.Background(), "u-1", "tenant-X")
	if err != nil {
		t.Fatalf("ActiveRoleIDs: %v", err)
	}
	if want := []string{"r-a", "r-sys"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func TestActiveRoleIDs_RefusesEmptyTenant(t *testing.T) {
	svc, repo := newTier1Service(t, staticRoleLookup("operator"))
	repo.listBindingsErr = errors.New("repo must not be called")
	_, err := svc.ActiveRoleIDs(context.Background(), "u-1", "")
	if !errors.Is(err, ErrRoleListTenantRequired) {
		t.Fatalf("err = %v, want ErrRoleListTenantRequired", err)
	}
}

func TestActiveRoleIDs_RefusesEmptyUser(t *testing.T) {
	svc, _ := newTier1Service(t, staticRoleLookup("operator"))
	if _, err := svc.ActiveRoleIDs(context.Background(), "", "tenant-X"); err == nil {
		t.Fatal("want error for empty user")
	}
}

func TestActiveRoleIDs_NoBindingsIsEmptyNotNil(t *testing.T) {
	svc, _ := newTier1Service(t, staticRoleLookup("operator"))
	got, err := svc.ActiveRoleIDs(context.Background(), "u-1", "tenant-X")
	if err != nil || got == nil || len(got) != 0 {
		t.Fatalf("got %#v, %v; want empty non-nil, nil", got, err)
	}
}

func TestActiveRoleIDs_DanglingBindingIsSkipped(t *testing.T) {
	svc, repo := newTier1Service(t, staticRoleLookup("operator"))
	repo.seedBinding("b1", "u-1", "tenant-X", "r-gone")
	got, err := svc.ActiveRoleIDs(context.Background(), "u-1", "tenant-X")
	if err != nil || got == nil || len(got) != 0 {
		t.Fatalf("got %#v, %v; want empty non-nil, nil", got, err)
	}
}

func TestActiveRoleIDs_CustomRoleOfAnotherTenantIsSkipped(t *testing.T) {
	svc, repo := newTier1Service(t, staticRoleLookup("operator"))
	repo.seedRole("r-y", "ops", false, nil, "tenant-Y")
	repo.seedBinding("b1", "u-1", "tenant-X", "r-y")
	got, err := svc.ActiveRoleIDs(context.Background(), "u-1", "tenant-X")
	if err != nil || len(got) != 0 {
		t.Fatalf("got %#v, %v; want empty", got, err)
	}
}

func TestActiveRoleIDs_RepositoryErrorsFailClosed(t *testing.T) {
	svc, repo := newTier1Service(t, staticRoleLookup("operator"))
	repo.seedRole("r-a", "sales", false, nil, "tenant-X")
	repo.seedBinding("b1", "u-1", "tenant-X", "r-a")

	repo.listBindingsErr = errors.New("boom")
	if got, err := svc.ActiveRoleIDs(context.Background(), "u-1", "tenant-X"); err == nil || got != nil {
		t.Fatalf("list error: got %v, %v", got, err)
	}
	repo.listBindingsErr = nil
	repo.getRoleErr = errors.New("boom")
	if got, err := svc.ActiveRoleIDs(context.Background(), "u-1", "tenant-X"); err == nil || got != nil {
		t.Fatalf("role error: got %v, %v", got, err)
	}
}
