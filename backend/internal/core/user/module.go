package user

import (
	"github.com/danielgtaylor/huma/v2/adapters/humachi"
	"github.com/go-chi/chi/v5"
	authRepo "github.com/orkestra/backend/internal/core/auth/repository"
	"github.com/orkestra/backend/internal/core/user/handlers"
	"github.com/orkestra/backend/internal/core/user/repository"
	"github.com/orkestra/backend/internal/core/user/services"
	"github.com/orkestra/backend/internal/shared/iface"
	"github.com/orkestra/backend/internal/shared/module"
)

type UserModule struct {
	module.BaseModule
	handler *handlers.UserHandler
}

func NewModule() *UserModule { return &UserModule{} }

func (m *UserModule) Name() string        { return "user" }
func (m *UserModule) DisplayName() string  { return "User Management" }
func (m *UserModule) Description() string  { return "User accounts, profiles, and RBAC" }

func (m *UserModule) ProvidedServices() []module.ServiceKey {
	return []module.ServiceKey{module.ServiceUserService}
}

func (m *UserModule) Collections() []module.CollectionSpec {
	// Email uniqueness on the per-tier collections is scoped to that
	// collection — the same email address may legitimately exist as both
	// an operator user and a client user (the same human running an
	// internal staff account and an external client account). The legacy
	// `users` collection retains its global email uniqueness until the
	// migration script copies its rows into operator_users.
	tierUserIndexes := []module.IndexSpec{
		{Keys: map[string]int{"uuid": 1}, Unique: true},
		{Keys: map[string]int{"email": 1}, Unique: true},
		{Keys: map[string]int{"tier": 1}},
	}
	return []module.CollectionSpec{
		{Name: repository.UsersCollection, Indexes: []module.IndexSpec{
			{Keys: map[string]int{"uuid": 1}, Unique: true},
			{Keys: map[string]int{"email": 1}, Unique: true},
		}},
		// ADR-0003 PR-B: tier-split user collections. Created on every
		// boot so the migration script + tier-aware providers can rely
		// on them existing; queries against them return zero rows until
		// the migration populates operator_users.
		{Name: repository.OperatorUsersCollection, Indexes: tierUserIndexes},
		{Name: repository.ClientUsersCollection, Indexes: tierUserIndexes},
	}
}

func (m *UserModule) NavItems() []module.NavItemSpec {
	return []module.NavItemSpec{
		{Realm: "personal", Section: "My workspace", Name: "Dashboard", Icon: "chart-pie", Path: "/user/dashboard", Active: true},
		{Realm: "personal", Section: "My workspace", Name: "Profile", Icon: "user", Path: "/user/profile", Active: true},
		{Realm: "personal", Section: "My workspace", Name: "Calendar", Icon: "calendar-alt", Path: "/user/calendar", Active: true},
		{Realm: "platform", Section: "Admin", Tier: "internal", Name: "User Management", Icon: "users-cog", Path: "/admin/users", Active: true},
		{Realm: "platform", Section: "Admin", Tier: "internal", Name: "Module Management", Icon: "puzzle-piece", Path: "/admin/modules", Active: true},
	}
}

func (m *UserModule) Permissions() []iface.PermissionSpec {
	return []iface.PermissionSpec{
		{Key: "user.read", Module: "user", Description: "List users"},
		{Key: "user.update", Module: "user", Description: "Update user profiles"},
		{Key: "user.delete", Module: "user", Description: "Delete users"},
		{Key: "user.self", Module: "user", Description: "Edit your own profile"},
	}
}

func (m *UserModule) Init(deps *module.Dependencies) error {
	userRepo := repository.NewUserRepository(deps.DB)
	oauthProviderRepo := authRepo.NewOAuthProviderRepository(deps.DB)
	svc := services.NewUserService(userRepo, oauthProviderRepo)
	m.handler = handlers.NewUserHandler(svc)
	deps.Services.Register(module.ServiceUserService, svc)

	// ADR-0003 PR-B: tier-aware providers registered alongside the
	// legacy one. They share the legacy oauth_provider_repo at the
	// PR-B boundary because the auth-side tier split (separate
	// operator_oauth_providers / client_oauth_providers repos) lands
	// in PR-D — operator_users / client_users are also empty until
	// the migration script runs in B-3, so any handler that
	// accidentally calls these now would just get zero rows. Both
	// keys are populated regardless of USER_TIER_SPLIT_ENABLED so a
	// future MustGetTyped lookup never panics; the flag governs which
	// provider auth flows consume, not whether they exist.
	deps.Services.Register(
		module.ServiceOperatorUserProvider,
		services.NewUserService(repository.NewOperatorUserRepository(deps.DB), oauthProviderRepo),
	)
	deps.Services.Register(
		module.ServiceClientUserProvider,
		services.NewUserService(repository.NewClientUserRepository(deps.DB), oauthProviderRepo),
	)

	// Register the user PII producer with the DSR registry pre-created in
	// main.go. Missing registry means the platform was booted without
	// compliance infrastructure — tolerate and skip.
	if reg, ok := module.GetTyped[*iface.PIIProducerRegistry](deps.Services, module.ServicePIIProducerRegistry); ok {
		reg.Register(services.NewPIIProducer(userRepo))
	}
	return nil
}

func (m *UserModule) RegisterRoutes(ri *module.RouteInfo) {
	// User management is a platform-level concern: users are global, so
	// routes live on the system permission gate (administrators) rather
	// than per-org.
	ri.Operator.ProtectedRouter.Group(func(r chi.Router) {
		r.Use(ri.Operator.AuthMW.RequireSystemPermission("system.users.admin"))
		api := humachi.New(r, ri.APIConfig)
		RegisterRoutes(api, m.handler)
	})
}
