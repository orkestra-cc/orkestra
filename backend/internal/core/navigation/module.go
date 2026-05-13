package navigation

import (
	"github.com/danielgtaylor/huma/v2/adapters/humachi"
	"github.com/orkestra-cc/orkestra-sdk/module"
	"github.com/orkestra-cc/orkestra-sdk/modulegate"
	"github.com/orkestra/backend/internal/core/navigation/handlers"
	"github.com/orkestra/backend/internal/core/navigation/services"
)

type NavigationModule struct {
	module.BaseModule
	handler *handlers.NavigationHandler
}

func NewModule() *NavigationModule { return &NavigationModule{} }

func (m *NavigationModule) Name() string        { return "navigation" }
func (m *NavigationModule) DisplayName() string { return "Navigation" }
func (m *NavigationModule) Description() string { return "Dynamic navigation menu aggregator" }

func (m *NavigationModule) Init(deps *module.Dependencies) error {
	// Collect NavItems from all registered modules (put in ServiceRegistry by the registry).
	var navItems []module.NavItemSpec
	if items := deps.Services.Get(module.ServiceNavItems); items != nil {
		navItems = items.([]module.NavItemSpec)
	}

	// Get config service for runtime enabled/disabled checks.
	var enabledChecker modulegate.ModuleEnabledChecker
	if svc := deps.Services.Get(module.ServiceConfigService); svc != nil {
		enabledChecker = svc.(modulegate.ModuleEnabledChecker)
	}

	svc := services.NewDynamicNavigationService(navItems, enabledChecker)
	m.handler = handlers.NewNavigationHandler(svc)
	return nil
}

func (m *NavigationModule) RegisterRoutes(ri *module.RouteInfo) {
	api := humachi.New(ri.Operator.ProtectedRouter, ri.APIConfig)
	RegisterRoutes(api, m.handler)
}
