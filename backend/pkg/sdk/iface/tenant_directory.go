package iface

import (
	"context"
	"time"
)

// TenantMemberSummary is the SDK-boundary projection of one tenant
// membership: who the member is, their tenant roles, ownership and join time.
// It deliberately carries no persistence model of the tenant module.
type TenantMemberSummary struct {
	UserUUID string    `json:"user_id"`
	Roles    []string  `json:"roles"`
	IsOwner  bool      `json:"is_owner"`
	JoinedAt time.Time `json:"joined_at"`
}

// TenantDirectoryReader exposes tenant-scoped membership summaries without
// leaking the tenant module's persistence models across the SDK boundary.
// Registered by the tenant module under module.ServiceTenantDirectoryReader.
type TenantDirectoryReader interface {
	ListTenantMembers(ctx context.Context, tenantUUID string) ([]TenantMemberSummary, error)
}

// TenantIDLister lists tenant identifiers by tier without exposing the tenant
// module's persistence models. Implemented by the tenant module's concrete
// service; resolve it with
// module.GetTyped[iface.TenantIDLister](reg, module.ServiceTenantService).
type TenantIDLister interface {
	// ListActiveTenantIDs returns the UUIDs of the tenants of kind
	// (TenantKindInternal | TenantKindExternal) that are not soft-deleted and
	// whose status is TenantStatusActive, sorted ascending. Any other kind is
	// refused.
	ListActiveTenantIDs(ctx context.Context, kind string) ([]string, error)
}
