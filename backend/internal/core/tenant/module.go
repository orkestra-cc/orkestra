// Package tenant is the multi-tenancy core module. It owns the unified
// Tenant aggregate (two-tier: internal | external, hierarchical via
// ParentTenantUUID), per-user memberships, and plan-based entitlements.
// Implements iface.TenantProvider so the middleware and other modules can
// resolve the current tenant, check membership, and gate routes.
//
// See docs/adr/0001-unified-tenant-model.md for the two-tier design.
package tenant

import (
	"github.com/danielgtaylor/huma/v2/adapters/humachi"
	"github.com/go-chi/chi/v5"
	"github.com/orkestra/backend/internal/core/tenant/handlers"
	"github.com/orkestra/backend/internal/core/tenant/repository"
	"github.com/orkestra/backend/internal/core/tenant/services"
	"github.com/orkestra/backend/internal/shared/iface"
	"github.com/orkestra/backend/internal/shared/module"
)

type Module struct {
	module.BaseModule
	handler *handlers.Handler
	svc     *services.Service
}

func NewModule() *Module { return &Module{} }

func (m *Module) Name() string        { return "tenant" }
func (m *Module) DisplayName() string { return "Tenants" }
func (m *Module) Description() string {
	return "Unified two-tier tenancy: internal operator tenants + external client tenants, memberships, plan entitlements"
}

func (m *Module) Dependencies() []string { return []string{"user"} }

func (m *Module) ProvidedServices() []module.ServiceKey {
	return []module.ServiceKey{module.ServiceTenantProvider, module.ServiceTenantService}
}

func (m *Module) Collections() []module.CollectionSpec {
	return []module.CollectionSpec{
		{Name: repository.CollTenants, Indexes: []module.IndexSpec{
			{Keys: map[string]int{"uuid": 1}, Unique: true},
			{Keys: map[string]int{"slug": 1}, Unique: true, Sparse: true},
			{Keys: map[string]int{"ownerUserUUID": 1}},
			{Keys: map[string]int{"kind": 1}},
			{Keys: map[string]int{"status": 1}},
		}},
		{Name: repository.CollMemberships, Indexes: []module.IndexSpec{
			{OrderedKeys: []module.IndexKey{
				{Field: "userUUID", Direction: 1},
				{Field: "tenantId", Direction: 1},
			}, Unique: true},
			{Keys: map[string]int{"tenantId": 1}},
		}},
		{Name: repository.CollInvites, Indexes: []module.IndexSpec{
			// tokenHash is the lookup key on accept; unique stops two invites
			// from colliding on the same random token. Sparse so the index
			// build succeeds across transitional docs.
			{Keys: map[string]int{"tokenHash": 1}, Unique: true, Sparse: true},
			{Keys: map[string]int{"tenantId": 1}},
			{Keys: map[string]int{"expiresAt": 1}, ExpireAt: true},
		}},
		// Closure table for the tenant hierarchy (ADR-0001). Supports external
		// multi-tenant clients (clients that are themselves multi-tenant with
		// sub-workspaces). Indexed both ways so ancestor-of-X and descendants-
		// of-X are O(1)/O(depth) respectively.
		{Name: repository.CollAncestors, Indexes: []module.IndexSpec{
			{OrderedKeys: []module.IndexKey{
				{Field: "descendantUUID", Direction: 1},
				{Field: "ancestorUUID", Direction: 1},
			}, Unique: true},
			{Keys: map[string]int{"ancestorUUID": 1}},
		}},
		// Capability entitlements projection (Phase 2). Tenants may hold
		// many historical rows per capability (revoked + re-granted, or
		// expired); at most one is active at a time — that invariant is
		// enforced in the service (Grant revokes any existing active row
		// before inserting). Indexes here accelerate the common reads.
		{Name: repository.CollEntitlements, Indexes: []module.IndexSpec{
			{Keys: map[string]int{"uuid": 1}, Unique: true, Sparse: true},
			{OrderedKeys: []module.IndexKey{
				{Field: "tenantId", Direction: 1},
				{Field: "capabilityId", Direction: 1},
			}},
			{Keys: map[string]int{"capabilityId": 1}},
			{Keys: map[string]int{"expiresAt": 1}, Sparse: true},
		}},
	}
}

func (m *Module) Permissions() []iface.PermissionSpec {
	return []iface.PermissionSpec{
		{Key: "tenant.read", Module: "tenant", Description: "Read tenant details"},
		{Key: "tenant.update", Module: "tenant", Description: "Update tenant name, slug, settings"},
		{Key: "tenant.delete", Module: "tenant", Description: "Archive the tenant"},
		{Key: "tenant.plan.update", Module: "tenant", Description: "Change plan and features"},
		{Key: "tenant.member.read", Module: "tenant", Description: "List tenant members"},
		{Key: "tenant.member.invite", Module: "tenant", Description: "Invite new members"},
		{Key: "tenant.member.remove", Module: "tenant", Description: "Remove members from the tenant"},
		{Key: "system.tenants.admin", Module: "tenant", Description: "Administer all tenants platform-wide", System: true},
	}
}

func (m *Module) NavItems() []module.NavItemSpec {
	return []module.NavItemSpec{
		{Group: "System Administration", Name: "Tenant Management", Icon: "building", Path: "/admin/tenants", MinRole: "administrator", Active: true},
	}
}

func (m *Module) Init(deps *module.Dependencies) error {
	repo := repository.New(deps.DB)
	m.svc = services.New(repo)
	m.handler = handlers.New(m.svc)
	deps.Services.Register(module.ServiceTenantProvider, iface.TenantProvider(m.svc))
	// Also publish the concrete service so addon modules (compliance) that
	// need to drive post-init setters can resolve it without importing the
	// tenant package from a second location.
	deps.Services.Register(module.ServiceTenantService, m.svc)
	return nil
}

func (m *Module) RegisterRoutes(ri *module.RouteInfo) {
	// Global routes: list/create tenants, accept invites. These need auth
	// but intentionally do not require a current-tenant context.
	ri.ProtectedRouter.Group(func(r chi.Router) {
		r.Use(ri.AuthMW.RequireGlobal())
		api := humachi.New(r, ri.APIConfig)
		m.handler.RegisterGlobalRoutes(api)
	})

	// Tenant-scoped routes: need the caller to have the tenant.read permission
	// in X-Tenant-ID. tenant.* permissions are granted by the system
	// administrator role seeded by the authz module. Reads pass through
	// with just the permission; mutations additionally require an MFA
	// step-up (Block B) because they can transfer ownership data, change
	// plan entitlements, or destroy the tenant.
	ri.ProtectedRouter.Group(func(r chi.Router) {
		r.Use(ri.AuthMW.RequirePermission("tenant.read"))
		api := humachi.New(r, ri.APIConfig)
		m.handler.RegisterScopedReadRoutes(api)
	})
	ri.ProtectedRouter.Group(func(r chi.Router) {
		r.Use(ri.AuthMW.RequirePermission("tenant.read"))
		r.Use(ri.AuthMW.RequireMFA())
		api := humachi.New(r, ri.APIConfig)
		m.handler.RegisterScopedMutationRoutes(api)
	})

	// Platform-admin routes: visible to super_admin / administrator /
	// developer via the system.tenants.admin permission. These bypass
	// per-tenant membership so a platform operator can manage every tenant
	// without joining each one.
	ri.ProtectedRouter.Group(func(r chi.Router) {
		r.Use(ri.AuthMW.RequireSystemPermission("system.tenants.admin"))
		api := humachi.New(r, ri.APIConfig)
		m.handler.RegisterAdminRoutes(api)
	})
}

// Service returns the tenant service — exposed so the authz module can
// also consume it during its own initialization.
func (m *Module) Service() *services.Service { return m.svc }
