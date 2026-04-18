// Package tenant is the multi-tenancy core module. It owns organizations,
// per-user memberships, and plan-based entitlements. It implements
// iface.TenantProvider so the middleware and other modules can resolve
// the current org, check membership, and gate routes on plan features.
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
func (m *Module) DisplayName() string { return "Organizations" }
func (m *Module) Description() string { return "Multi-tenant organizations, memberships, and plan entitlements" }

func (m *Module) Dependencies() []string { return []string{"user"} }

func (m *Module) ProvidedServices() []module.ServiceKey {
	return []module.ServiceKey{module.ServiceTenantProvider}
}

func (m *Module) Collections() []module.CollectionSpec {
	return []module.CollectionSpec{
		{Name: repository.CollOrgs, Indexes: []module.IndexSpec{
			{Keys: map[string]int{"uuid": 1}, Unique: true},
			{Keys: map[string]int{"slug": 1}, Unique: true, Sparse: true},
			{Keys: map[string]int{"ownerUserUUID": 1}},
		}},
		{Name: repository.CollMemberships, Indexes: []module.IndexSpec{
			{OrderedKeys: []module.IndexKey{
				{Field: "userUUID", Direction: 1},
				{Field: "orgId", Direction: 1},
			}, Unique: true},
			{Keys: map[string]int{"orgId": 1}},
		}},
		{Name: repository.CollInvites, Indexes: []module.IndexSpec{
			// tokenHash is the lookup key on accept; unique stops two invites
			// from colliding on the same random token (near-impossible at 32
			// bytes of entropy, but the unique constraint also catches
			// programmer mistakes like forgetting to reroll on retry).
			// Sparse so the index build succeeds even when a legacy invite
			// document without tokenHash is lingering — new writes are still
			// uniqueness-checked. The service layer always populates the
			// field, so "sparse" does not weaken the invariant in practice.
			{Keys: map[string]int{"tokenHash": 1}, Unique: true, Sparse: true},
			{Keys: map[string]int{"orgId": 1}},
			// ExpireAt: Mongo reaps the doc when the expiresAt timestamp
			// itself passes. Without this, invites would accumulate forever
			// because the service only lazily filters expired rows on read.
			{Keys: map[string]int{"expiresAt": 1}, ExpireAt: true},
		}},
		// Closure table for the tenant hierarchy (ADR-0001). Supports
		// external multi-tenant clients (clients that are themselves
		// multi-tenant with sub-workspaces). Indexed both ways so
		// ancestor-of-X and descendants-of-X are O(1)/O(depth) respectively.
		{Name: repository.CollAncestors, Indexes: []module.IndexSpec{
			{OrderedKeys: []module.IndexKey{
				{Field: "descendantUUID", Direction: 1},
				{Field: "ancestorUUID", Direction: 1},
			}, Unique: true},
			{Keys: map[string]int{"ancestorUUID": 1}},
		}},
	}
}

func (m *Module) Permissions() []iface.PermissionSpec {
	return []iface.PermissionSpec{
		{Key: "tenant.org.read", Module: "tenant", Description: "Read org details"},
		{Key: "tenant.org.update", Module: "tenant", Description: "Update org name, slug, settings"},
		{Key: "tenant.org.delete", Module: "tenant", Description: "Soft-delete the org"},
		{Key: "tenant.plan.update", Module: "tenant", Description: "Change plan and features"},
		{Key: "tenant.member.read", Module: "tenant", Description: "List org members"},
		{Key: "tenant.member.invite", Module: "tenant", Description: "Invite new members"},
		{Key: "tenant.member.remove", Module: "tenant", Description: "Remove members from the org"},
		{Key: "system.tenants.admin", Module: "tenant", Description: "Administer all organizations platform-wide", System: true},
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
	return nil
}

func (m *Module) RegisterRoutes(ri *module.RouteInfo) {
	// Global routes: list/create orgs, accept invites. These need auth but
	// intentionally do not require a current-org context.
	ri.ProtectedRouter.Group(func(r chi.Router) {
		r.Use(ri.AuthMW.RequireGlobal())
		api := humachi.New(r, ri.APIConfig)
		m.handler.RegisterGlobalRoutes(api)
	})

	// Org-scoped routes: need the caller to be an administrator of the
	// org in X-Org-ID. tenant.* permissions are granted by the system
	// administrator role seeded by the authz module. Reads pass through
	// with just the permission; mutations additionally require an MFA
	// step-up (Block B) because they can transfer ownership data, change
	// plan entitlements, or destroy the org.
	ri.ProtectedRouter.Group(func(r chi.Router) {
		r.Use(ri.AuthMW.RequirePermission("tenant.org.read"))
		api := humachi.New(r, ri.APIConfig)
		m.handler.RegisterScopedReadRoutes(api)
	})
	ri.ProtectedRouter.Group(func(r chi.Router) {
		r.Use(ri.AuthMW.RequirePermission("tenant.org.read"))
		r.Use(ri.AuthMW.RequireMFA())
		api := humachi.New(r, ri.APIConfig)
		m.handler.RegisterScopedMutationRoutes(api)
	})

	// Platform-admin routes: visible to super_admin / administrator /
	// developer via the system.tenants.admin permission. These bypass
	// per-org membership so a platform operator can list and manage
	// every tenant without joining each one.
	ri.ProtectedRouter.Group(func(r chi.Router) {
		r.Use(ri.AuthMW.RequireSystemPermission("system.tenants.admin"))
		api := humachi.New(r, ri.APIConfig)
		m.handler.RegisterAdminRoutes(api)
	})
}

// Service returns the tenant service — exposed so the authz module can
// also consume it during its own initialization.
func (m *Module) Service() *services.Service { return m.svc }
