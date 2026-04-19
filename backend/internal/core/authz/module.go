// Package authz is the authorization core module. It owns the permission
// catalog, roles (system + custom per tenant), role bindings with optional
// expiration, and the evaluator that the middleware calls on every request.
// It implements iface.AuthzProvider.
package authz

import (
	"context"
	"strings"

	"github.com/danielgtaylor/huma/v2/adapters/humachi"
	"github.com/go-chi/chi/v5"
	"github.com/orkestra/backend/internal/core/authz/handlers"
	"github.com/orkestra/backend/internal/core/authz/repository"
	"github.com/orkestra/backend/internal/core/authz/services"
	"github.com/orkestra/backend/internal/shared/iface"
	"github.com/orkestra/backend/internal/shared/middleware"
	"github.com/orkestra/backend/internal/shared/module"
)

// validDevRoles is the set of system roles that synthetic dev-token users
// may carry. Must match the dev module's ValidRoles. Declared here to avoid
// an import dependency on the dev addon.
var validDevRoles = map[string]struct{}{
	"super_admin": {}, "administrator": {}, "developer": {},
	"manager": {}, "operator": {}, "guest": {},
}

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
				{Field: "tenantId", Direction: 1},
				{Field: "name", Direction: 1},
			}, Unique: true},
			{Keys: map[string]int{"uuid": 1}, Unique: true},
		}},
		{Name: repository.CollBindings, Indexes: []module.IndexSpec{
			{OrderedKeys: []module.IndexKey{
				{Field: "userUUID", Direction: 1},
				{Field: "tenantId", Direction: 1},
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
		{Key: "authz.role.update", Module: "authz", Description: "Update custom roles and toggle any role's active state"},
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

	// Optional TenantProvider handle: used by the Cedar shadow-mode
	// evaluator to stamp Principal.Capabilities so capability_grants.cedar
	// has something to reason about when a caller opts into capability
	// enforcement. The lookup is nil-safe — when the tenant module is not
	// yet registered (boot ordering) the Cedar principal simply has an
	// empty capability set and the defense-in-depth rule is inert.
	tenantProvider, _ := module.GetTyped[iface.TenantProvider](deps.Services, module.ServiceTenantProvider)
	var lookupCaps services.TenantCapabilityLookup
	if tenantProvider != nil {
		lookupCaps = tenantProvider.ListCapabilityIDs
	}

	// The evaluator needs to know each user's system role to honor the
	// developer/administrator shortcuts. We get it from UserProvider.
	userSvc := module.MustGetTyped[iface.UserProvider](deps.Services, module.ServiceUserService)
	lookup := func(ctx context.Context, userUUID string) (string, error) {
		u, err := userSvc.GetUserByID(ctx, userUUID)
		if err == nil {
			return u.Role, nil
		}
		// Dev-token fallback: synthetic users have no DB record.
		// Three guards: non-production + dev- UUID prefix + valid role in JWT.
		if !deps.Config.IsProduction() && strings.HasPrefix(userUUID, "dev-") {
			if role, ok := middleware.GetSystemRole(ctx); ok {
				if _, valid := validDevRoles[role]; valid {
					return role, nil
				}
			}
		}
		return "", err
	}

	// Post-binding grace hook. Binding creates that land a privileged role
	// eagerly start the target user's MFA enrollment clock so the 7-day
	// window begins at promotion rather than at their next login. We pass
	// a thin closure so the authz service stays free of a direct user
	// module import.
	startMFAGrace := func(ctx context.Context, userUUID string) error {
		return userSvc.StartMFAGraceIfUnset(ctx, userUUID)
	}

	m.svc = services.New(services.Config{
		Repo:          repo,
		Redis:         deps.RedisAdapter,
		Logger:        deps.Logger,
		LookupUser:    lookup,
		LookupCaps:    lookupCaps,
		StartMFAGrace: startMFAGrace,
		// Production flag restricts the developer system role to read-only
		// semantics (D9): dev/staging developers debug freely; prod
		// developers cannot mutate data or read secrets even if their
		// token is valid. Mirrors the role-seed output in SeedSystemRoles.
		Production: deps.Config.IsProduction(),
		// Environment is fed to the Cedar shadow-mode engine so
		// platform.cedar can branch on prod vs non-prod developer rules.
		Environment: deps.Config.GetEnvironment(),
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

	// Scoped admin routes split by mutation vs read: reads need the base
	// permission, mutations additionally require a second factor (Block B)
	// so a stolen pwd-only token cannot silently grant a role or bump
	// permissions. The caller must step up via /v1/auth/mfa/verify first.
	ri.ProtectedRouter.Group(func(r chi.Router) {
		r.Use(ri.AuthMW.RequirePermission("authz.role.read"))
		api := humachi.New(r, ri.APIConfig)
		m.handler.RegisterScopedReadRoutes(api)
	})
	ri.ProtectedRouter.Group(func(r chi.Router) {
		r.Use(ri.AuthMW.RequirePermission("authz.role.read"))
		r.Use(ri.AuthMW.RequireMFA())
		api := humachi.New(r, ri.APIConfig)
		m.handler.RegisterScopedMutationRoutes(api)
	})
}

// Service returns the authz service — exposed so the registry can trigger
// RegisterPermissions and SeedSystemRoles after all modules init.
func (m *Module) Service() *services.Service { return m.svc }
