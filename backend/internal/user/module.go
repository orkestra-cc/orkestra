package user

import (
	"context"

	"github.com/danielgtaylor/huma/v2/adapters/humachi"
	"github.com/go-chi/chi/v5"
	"github.com/orkestra/backend/internal/shared/config"
	"github.com/orkestra/backend/internal/shared/module"
	"github.com/orkestra/backend/internal/user/handlers"
	"github.com/orkestra/backend/internal/user/repository"
	"github.com/orkestra/backend/internal/user/services"

	authRepo "github.com/orkestra/backend/internal/auth/repository"
)

type UserModule struct {
	handler *handlers.UserHandler
}

func NewModule() *UserModule {
	return &UserModule{}
}

func (m *UserModule) Name() string { return "user" }

func (m *UserModule) Enabled(_ *config.Config) bool { return true }

func (m *UserModule) Init(deps *module.Dependencies) error {
	userRepo := repository.NewUserRepository(deps.DB)

	// OAuthProviderRepo is needed by UserService for managing OAuth links
	oauthProviderRepo := authRepo.NewOAuthProviderRepository(deps.DB)

	svc := services.NewUserService(userRepo, oauthProviderRepo)
	m.handler = handlers.NewUserHandler(svc)

	// Register UserService for auth module consumption
	deps.Services.Register(module.ServiceUserService, svc)

	return nil
}

func (m *UserModule) RegisterRoutes(ri *module.RouteInfo) {
	// User management: administrator role and above
	ri.ProtectedRouter.Group(func(r chi.Router) {
		r.Use(ri.AuthMW.RequireHierarchicalRole("administrator"))
		api := humachi.New(r, ri.APIConfig)
		RegisterRoutes(api, m.handler)
	})
}

func (m *UserModule) Start(_ context.Context) error      { return nil }
func (m *UserModule) Stop(_ context.Context) error       { return nil }
func (m *UserModule) HealthCheck(_ context.Context) error { return nil }
