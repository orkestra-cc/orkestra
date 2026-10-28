package services

import (
	"testing"

	"github.com/orkestra/backend/pkg/sdk/iface"
)

func TestService_ImplementsTenantPurgeHooks(t *testing.T) {
	var _ iface.TenantPurgeHooks = (*Service)(nil)
}

func TestService_ImplementsPurgedTenantIDLister(t *testing.T) {
	var _ iface.PurgedTenantIDLister = (*Service)(nil)
}

func TestRegisterTenantPurgeHook_NilIgnored(t *testing.T) {
	svc := New(nil)
	svc.RegisterTenantPurgeHook(nil)
	if len(svc.purgeHooks) != 0 {
		t.Fatalf("purgeHooks = %d, want 0", len(svc.purgeHooks))
	}
}
