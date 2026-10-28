package iface

import "context"

// TenantPurgeHooks lets a module run a cleanup when a tenant is hard-purged.
// Implemented by the tenant module's concrete service; resolve it with
// module.GetTyped[iface.TenantPurgeHooks](reg, module.ServiceTenantService).
//
// The hook receives the purged tenant's UUID, even when its row could not be
// read. It runs after the tenant's own cascade and its post-delete hooks, only
// for a hard purge (never for a soft delete). A returned error is audited as
// tenant.cascade.hook_failed and never aborts the purge or later hooks, so a
// hook must be safe to re-run from a durable reconciliation path.
type TenantPurgeHooks interface {
	RegisterTenantPurgeHook(fn func(ctx context.Context, tenantUUID string) error)
}
