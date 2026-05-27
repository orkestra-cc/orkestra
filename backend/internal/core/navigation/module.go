package navigation

import (
	"github.com/danielgtaylor/huma/v2/adapters/humachi"
	"github.com/orkestra-cc/orkestra-sdk/module"
	"github.com/orkestra-cc/orkestra-sdk/modulegate"
	"github.com/orkestra/backend/internal/core/navigation/handlers"
	"github.com/orkestra/backend/internal/core/navigation/repository"
	"github.com/orkestra/backend/internal/core/navigation/services"
)

// navOverridesCollection is the Mongo collection that stores per-parent
// ordering overrides. Single collection on this module, so no module
// prefix is required by the collection-naming convention.
const navOverridesCollection = "navigation_overrides"

type NavigationModule struct {
	module.BaseModule
	handler      *handlers.NavigationHandler
	adminHandler *handlers.AdminNavigationHandler
}

func NewModule() *NavigationModule { return &NavigationModule{} }

func (m *NavigationModule) Name() string        { return "navigation" }
func (m *NavigationModule) DisplayName() string { return "Navigation" }
func (m *NavigationModule) Description() string { return "Dynamic navigation menu aggregator" }

// Collections declares the navigation_overrides Mongo collection.
// One document per parent menu node, keyed by _id=parentKey. No
// secondary indexes — _id is the only lookup key the repo uses.
func (m *NavigationModule) Collections() []module.CollectionSpec {
	return []module.CollectionSpec{
		{Name: navOverridesCollection},
	}
}

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

	// Persisted ordering overrides (Phase 2). Repository auto-created
	// by the registry from Collections(). The override service self-
	// heals stale keys on every read; failure to load degrades to
	// declared order rather than serving nothing.
	overrideRepo := repository.NewMongoRepository(deps.DB.Collection(navOverridesCollection))
	overrideSvc := services.NewOverrideService(overrideRepo, deps.Logger)

	svc := services.NewDynamicNavigationService(navItems, enabledChecker, overrideSvc)
	m.handler = handlers.NewNavigationHandler(svc)

	adminSvc := services.NewAdminNavigationService(navItems, enabledChecker, overrideSvc)
	itemsIdx := services.NewNavItemsIndexAccessor(navItems)
	m.adminHandler = handlers.NewAdminNavigationHandler(adminSvc, overrideSvc, itemsIdx)
	return nil
}

func (m *NavigationModule) RegisterRoutes(ri *module.RouteInfo) {
	api := humachi.New(ri.Operator.ProtectedRouter, ri.APIConfig)
	RegisterRoutes(api, m.handler)
	RegisterAdminRoutes(api, m.adminHandler)
}

// NavItems adds a single entry in the platform realm so operators can
// reach the navigation admin page from the sidebar. Kept here rather
// than on a fake "Modules" parent so it shows up beside Log levels under
// Administration.
func (m *NavigationModule) NavItems() []module.NavItemSpec {
	return []module.NavItemSpec{
		{
			Realm:   "platform",
			Tier:    "internal",
			Name:    "Navigation",
			Icon:    "list-ul",
			Path:    "/admin/modules/navigation",
			MinRole: "administrator",
			Active:  true,
		},
	}
}
