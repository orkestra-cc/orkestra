package user

import (
	"github.com/danielgtaylor/huma/v2/adapters/humachi"
	"github.com/go-chi/chi/v5"
	authRepo "github.com/orkestra/backend/internal/core/auth/repository"
	"github.com/orkestra/backend/internal/shared/module"
	"github.com/orkestra/backend/internal/core/user/handlers"
	"github.com/orkestra/backend/internal/core/user/repository"
	"github.com/orkestra/backend/internal/core/user/services"
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
	return []module.CollectionSpec{
		{Name: "users", Indexes: []module.IndexSpec{
			{Keys: map[string]int{"uuid": 1}, Unique: true},
			{Keys: map[string]int{"email": 1}, Unique: true},
		}},
	}
}

func (m *UserModule) NavItems() []module.NavItemSpec {
	return []module.NavItemSpec{
		{Group: "Operators", Name: "Dashboard", Icon: "chart-pie", Path: "/user/dashboard", MinRole: "guest", Active: true},
		{Group: "Operators", Name: "Profile", Icon: "user", Path: "/user/profile", MinRole: "guest", Active: true},
		{Group: "Operators", Name: "Calendar", Icon: "calendar-alt", Path: "/user/calendar", MinRole: "guest", Active: true},
		{Group: "System Administration", Name: "User Management", Icon: "users-cog", Path: "/admin/users", MinRole: "administrator", Active: true},
		{Group: "System Administration", Name: "Module Management", Icon: "puzzle-piece", Path: "/admin/modules", MinRole: "administrator", Active: true},
	}
}

func (m *UserModule) Init(deps *module.Dependencies) error {
	userRepo := repository.NewUserRepository(deps.DB)
	oauthProviderRepo := authRepo.NewOAuthProviderRepository(deps.DB)
	svc := services.NewUserService(userRepo, oauthProviderRepo)
	m.handler = handlers.NewUserHandler(svc)
	deps.Services.Register(module.ServiceUserService, svc)
	return nil
}

func (m *UserModule) RegisterRoutes(ri *module.RouteInfo) {
	ri.ProtectedRouter.Group(func(r chi.Router) {
		r.Use(ri.AuthMW.RequireHierarchicalRole("administrator"))
		api := humachi.New(r, ri.APIConfig)
		RegisterRoutes(api, m.handler)
	})
}
