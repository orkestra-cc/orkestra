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
	// internal staff account and an external client account).
	tierUserIndexes := []module.IndexSpec{
		{Keys: map[string]int{"uuid": 1}, Unique: true},
		{Keys: map[string]int{"email": 1}, Unique: true},
		{Keys: map[string]int{"tier": 1}},
	}
	return []module.CollectionSpec{
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
	// ADR-0003 PR-D D-8: operator-tier user provider is the canonical
	// ServiceUserService. Both tier providers are registered under
	// their dedicated keys so audience-specific consumers (onboarding
	// → client, setup wizard → operator) can pick the right one
	// directly rather than going through the canonical key.
	operatorOAuthRepo := authRepo.NewOperatorOAuthProviderRepository(deps.DB)
	clientOAuthRepo := authRepo.NewClientOAuthProviderRepository(deps.DB)

	operatorRepo := repository.NewOperatorUserRepository(deps.DB)
	operatorSvc := services.NewUserService(operatorRepo, operatorOAuthRepo)

	clientRepo := repository.NewClientUserRepository(deps.DB)
	clientSvc := services.NewUserService(clientRepo, clientOAuthRepo)

	deps.Services.Register(module.ServiceOperatorUserProvider, operatorSvc)
	deps.Services.Register(module.ServiceClientUserProvider, clientSvc)

	canonical := operatorSvc
	canonicalRepo := operatorRepo
	m.handler = handlers.NewUserHandler(canonical)
	deps.Services.Register(module.ServiceUserService, canonical)

	// Register the user PII producer with the DSR registry pre-created in
	// main.go. Missing registry means the platform was booted without
	// compliance infrastructure — tolerate and skip.
	if reg, ok := module.GetTyped[*iface.PIIProducerRegistry](deps.Services, module.ServicePIIProducerRegistry); ok {
		reg.Register(services.NewPIIProducer(canonicalRepo))
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
