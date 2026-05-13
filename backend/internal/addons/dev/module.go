package dev

import (
	"log/slog"
	"os"

	"github.com/orkestra-cc/orkestra-sdk/iface"
	"github.com/orkestra-cc/orkestra-sdk/module"
	"github.com/orkestra/backend/internal/addons/dev/handlers"
)

type DevModule struct {
	module.BaseModule
	handler *handlers.DevTokenHandler
	logger  *slog.Logger
}

func NewModule() *DevModule { return &DevModule{} }

func (m *DevModule) Name() string        { return "dev" }
func (m *DevModule) DisplayName() string { return "Development Tools" }
func (m *DevModule) Description() string { return "Dev token generation and testing utilities" }

// Enabled keeps dev disabled in production. Sourced directly from the ENV
// env var (matches shared/config's Server.Environment loader) so no
// shared/config dependency is needed for the SDK split.
func (m *DevModule) Enabled() bool { return os.Getenv("ENV") != "production" }

func (m *DevModule) Dependencies() []string { return []string{"auth"} }
func (m *DevModule) RequiredServices() []module.ServiceKey {
	// ADR-0003 PR-D D-10: dev token mints per-audience tokens so the
	// caller can exercise either host mux. Both per-tier JWT services
	// are required dependencies; the canonical key is no longer used.
	return []module.ServiceKey{module.ServiceOperatorJWTService, module.ServiceClientJWTService}
}

func (m *DevModule) Init(deps *module.Dependencies) error {
	operatorJWT := module.MustGetTyped[iface.JWTProvider](deps.Services, module.ServiceOperatorJWTService)
	clientJWT := module.MustGetTyped[iface.JWTProvider](deps.Services, module.ServiceClientJWTService)
	m.handler = handlers.NewDevTokenHandler(operatorJWT, clientJWT, deps.Platform)
	m.logger = deps.Logger
	return nil
}

func (m *DevModule) RegisterRoutes(ri *module.RouteInfo) {
	ri.Router.Post("/dev/token", m.handler.GenerateTokenHTTP)
	ri.Router.Get("/dev/token/roles", m.handler.ListRolesHTTP)
	m.logger.Info("Dev token routes registered")
}
