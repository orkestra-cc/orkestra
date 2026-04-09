package dev

import (
	"log/slog"

	"github.com/orkestra/backend/internal/addons/dev/handlers"
	"github.com/orkestra/backend/internal/shared/config"
	"github.com/orkestra/backend/internal/shared/iface"
	"github.com/orkestra/backend/internal/shared/module"
)

type DevModule struct {
	module.BaseModule
	handler *handlers.DevTokenHandler
	logger  *slog.Logger
}

func NewModule() *DevModule { return &DevModule{} }

func (m *DevModule) Name() string        { return "dev" }
func (m *DevModule) DisplayName() string  { return "Development Tools" }
func (m *DevModule) Description() string  { return "Dev token generation and testing utilities" }

func (m *DevModule) Enabled(cfg *config.Config) bool { return !cfg.IsProduction() }

func (m *DevModule) Dependencies() []string           { return []string{"auth"} }
func (m *DevModule) RequiredServices() []module.ServiceKey { return []module.ServiceKey{module.ServiceJWTService} }

func (m *DevModule) Init(deps *module.Dependencies) error {
	jwtService := module.MustGetTyped[iface.JWTProvider](deps.Services, module.ServiceJWTService)
	m.handler = handlers.NewDevTokenHandler(jwtService, deps.Config)
	m.logger = deps.Logger
	return nil
}

func (m *DevModule) RegisterRoutes(ri *module.RouteInfo) {
	ri.Router.Post("/dev/token", m.handler.GenerateTokenHTTP)
	ri.Router.Get("/dev/token/roles", m.handler.ListRolesHTTP)
	m.logger.Info("Dev token routes registered")
}
