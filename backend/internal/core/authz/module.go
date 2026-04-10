// Package authz is the authorization core module. It owns the permission
// catalog, roles (system + custom per tenant), role bindings with optional
// expiration, and the evaluator that the middleware calls on every request.
// It implements iface.AuthzProvider.
package authz

import (
	"context"

	"github.com/danielgtaylor/huma/v2/adapters/humachi"
	"github.com/go-chi/chi/v5"
	"github.com/orkestra/backend/internal/core/authz/handlers"
	"github.com/orkestra/backend/internal/core/authz/repository"
	"github.com/orkestra/backend/internal/core/authz/services"
	"github.com/orkestra/backend/internal/shared/iface"
	"github.com/orkestra/backend/internal/shared/module"
)

type Module struct {
	module.BaseModule
	handler *handlers.Handler
	svc     *services.Service
}

func NewModule() *Module { return &Module{} }

func (m *Module) Name() string        { return "authz" }
func (m *Module) DisplayName() string { return "Authorization" }
func (m *Module) Description() string { return "Permissions catalog, roles, role bindings" }

func (m *Module) Dependencies() []string { return []string{"user", "tenant"} }

func (m *Module) RequiredServices() []module.ServiceKey {
	return []module.ServiceKey{module.ServiceUserService}
}

func (m *Module) ProvidedServices() []module.ServiceKey {
	return []module.ServiceKey{module.ServiceAuthzProvider}
}

func (m *Module) NavItems() []module.NavItemSpec {
	return []module.NavItemSpec{
		{Group: "System Administration", Name: "Role Management", Icon: "shield-alt", Path: "/admin/roles", Active: true},
	}
}

func (m *Module) Collections() []module.CollectionSpec {
	return []module.CollectionSpec{
		{Name: repository.CollPermissions, Indexes: []module.IndexSpec{
			{Keys: map[string]int{"key": 1}, Unique: true},
			{Keys: map[string]int{"module": 1}},
		}},
		{Name: repository.CollRoles, Indexes: []module.IndexSpec{
			{OrderedKeys: []module.IndexKey{
				{Field: "orgId", Direction: 1},
				{Field: "name", Direction: 1},
			}, Unique: true},
			{Keys: map[string]int{"uuid": 1}, Unique: true},
		}},
		{Name: repository.CollBindings, Indexes: []module.IndexSpec{
			{OrderedKeys: []module.IndexKey{
				{Field: "userUUID", Direction: 1},
				{Field: "orgId", Direction: 1},
			}},
			{Keys: map[string]int{"roleId": 1}},
			{Keys: map[string]int{"expiresAt": 1}},
		}},
	}
}

func (m *Module) Permissions() []iface.PermissionSpec {
	return []iface.PermissionSpec{
		{Key: "authz.role.read", Module: "authz", Description: "List roles"},
		{Key: "authz.role.create", Module: "authz", Description: "Create custom roles"},
		{Key: "authz.role.delete", Module: "authz", Description: "Delete custom roles"},
		{Key: "authz.binding.read", Module: "authz", Description: "List role bindings"},
		{Key: "authz.binding.create", Module: "authz", Description: "Grant roles to users"},
		{Key: "authz.binding.delete", Module: "authz", Description: "Revoke role bindings"},

		// Platform-level system permissions granted by the user's system role.
		{Key: "system.modules.admin", Module: "system", Description: "Manage module catalog and runtime state", System: true},
		{Key: "system.users.admin", Module: "system", Description: "Administer all user accounts", System: true},
	}
}

func (m *Module) Init(deps *module.Dependencies) error {
	repo := repository.New(deps.DB)

	// The evaluator needs to know each user's system role to honor the
	// developer/administrator shortcuts. We get it from UserProvider.
	userSvc := module.MustGetTyped[iface.UserProvider](deps.Services, module.ServiceUserService)
	lookup := func(ctx context.Context, userUUID string) (string, error) {
		u, err := userSvc.GetUserByID(ctx, userUUID)
		if err != nil {
			return "", err
		}
		return u.Role, nil
	}

	m.svc = services.New(services.Config{
		Repo:       repo,
		Redis:      deps.RedisAdapter,
		Logger:     deps.Logger,
		LookupUser: lookup,
	})
	m.handler = handlers.New(m.svc)

	deps.Services.Register(module.ServiceAuthzProvider, iface.AuthzProvider(m.svc))
	return nil
}

func (m *Module) RegisterRoutes(ri *module.RouteInfo) {
	// Catalog is global (not per-org).
	ri.ProtectedRouter.Group(func(r chi.Router) {
		r.Use(ri.AuthMW.RequireGlobal())
		api := humachi.New(r, ri.APIConfig)
		m.handler.RegisterGlobalRoutes(api)
	})

	// Scoped admin routes require authz.* permissions in the target org.
	ri.ProtectedRouter.Group(func(r chi.Router) {
		r.Use(ri.AuthMW.RequirePermission("authz.role.read"))
		api := humachi.New(r, ri.APIConfig)
		m.handler.RegisterScopedRoutes(api)
	})
}

// Service returns the authz service — exposed so the registry can trigger
// RegisterPermissions and SeedSystemRoles after all modules init.
func (m *Module) Service() *services.Service { return m.svc }
