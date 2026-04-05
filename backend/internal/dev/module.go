package dev

import (
	"context"
	"log/slog"

	"github.com/orkestra/backend/internal/auth/services"
	"github.com/orkestra/backend/internal/dev/handlers"
	"github.com/orkestra/backend/internal/shared/config"
	"github.com/orkestra/backend/internal/shared/module"
)

type DevModule struct {
	handler *handlers.DevTokenHandler
	logger  *slog.Logger
}

func NewModule() *DevModule {
	return &DevModule{}
}

func (m *DevModule) Name() string { return "dev" }

func (m *DevModule) Enabled(cfg *config.Config) bool {
	return !cfg.IsProduction()
}

func (m *DevModule) Init(deps *module.Dependencies) error {
	jwtService := deps.Services.MustGet(module.ServiceJWTService).(services.JWTService)
	m.handler = handlers.NewDevTokenHandler(jwtService, deps.Config)
	m.logger = deps.Logger
	return nil
}

func (m *DevModule) RegisterRoutes(ri *module.RouteInfo) {
	// Dev routes are registered directly on the main router (not Huma, no auth)
	ri.Router.Post("/dev/token", m.handler.GenerateTokenHTTP)
	ri.Router.Get("/dev/token/roles", m.handler.ListRolesHTTP)
	m.logger.Info("Dev token routes registered",
		slog.String("note", "registered via module registry"),
	)
}

func (m *DevModule) Start(_ context.Context) error      { return nil }
func (m *DevModule) Stop(_ context.Context) error       { return nil }
func (m *DevModule) HealthCheck(_ context.Context) error { return nil }
