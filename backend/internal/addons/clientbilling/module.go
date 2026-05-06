// Package clientbilling owns the user-level billing projection introduced
// in the post-onboarding refactor (Phase 2). A self-registered Tier-2
// client's billing identity (legal name, VAT, fiscal code, country, email)
// lives here so payment + subscription flows can drive Stripe customer
// creation and subscription renewals when the owner is `Kind="user"`.
//
// The addon publishes iface.UserBillingCustomerProvider under
// module.ServiceUserBillingCustomerProvider — payments and subscriptions
// resolve it lazily from the ServiceRegistry. Tenant-owner billing is
// untouched by this module; that path is still served through the existing
// TenantProvider + billing addon seams.
package clientbilling

import (
	"github.com/danielgtaylor/huma/v2/adapters/humachi"
	"github.com/go-chi/chi/v5"

	"github.com/orkestra/backend/internal/addons/clientbilling/handlers"
	"github.com/orkestra/backend/internal/addons/clientbilling/models"
	"github.com/orkestra/backend/internal/addons/clientbilling/repository"
	"github.com/orkestra/backend/internal/addons/clientbilling/services"
	"github.com/orkestra/backend/internal/shared/iface"
	"github.com/orkestra/backend/internal/shared/middleware"
	"github.com/orkestra/backend/internal/shared/module"
)

// ClientBillingModule is the addon entry point. Single collection, one
// cross-module service registration, two HTTP routes on the client surface.
type ClientBillingModule struct {
	module.BaseModule

	handler *handlers.MeHandler
}

// NewModule constructs the module.
func NewModule() *ClientBillingModule { return &ClientBillingModule{} }

func (m *ClientBillingModule) Name() string                    { return "clientbilling" }
func (m *ClientBillingModule) DisplayName() string             { return "Client Billing Profile" }
func (m *ClientBillingModule) Description() string             { return "User-level billing profile for self-registered Tier-2 clients (Phase 2 of polymorphic-owner refactor)" }
func (m *ClientBillingModule) Category() module.ModuleCategory { return module.CategoryToggleable }
func (m *ClientBillingModule) HotReloadConfig() bool           { return true }

// Dependencies is empty: the addon owns its own collection and exposes a
// service for downstream modules to pick up via the ServiceRegistry. The
// payments and subscriptions modules already resolve their cross-module
// dependencies lazily, so no init-time ordering is required.
func (m *ClientBillingModule) Dependencies() []string { return nil }

func (m *ClientBillingModule) ProvidedServices() []module.ServiceKey {
	return []module.ServiceKey{module.ServiceUserBillingCustomerProvider}
}

func (m *ClientBillingModule) Collections() []module.CollectionSpec {
	return []module.CollectionSpec{
		{Name: models.CustomersCollection, Indexes: []module.IndexSpec{
			{Keys: map[string]int{"userUUID": 1}, Unique: true},
		}},
	}
}

func (m *ClientBillingModule) Init(deps *module.Dependencies) error {
	repo := repository.NewCustomerRepository(deps.DB)
	svc := services.NewCustomerService(repo, deps.Logger)
	m.handler = handlers.NewMeHandler(svc)

	// Publish the cross-module provider. Structural typing — *CustomerService
	// already implements iface.UserBillingCustomerProvider, so no separate
	// adapter is needed.
	deps.Services.Register(module.ServiceUserBillingCustomerProvider, iface.UserBillingCustomerProvider(svc))

	deps.Logger.Info("Client billing module initialized")
	return nil
}

func (m *ClientBillingModule) RegisterRoutes(ri *module.RouteInfo) {
	if ri.Client == nil {
		// Single-mux deployments don't have a separate client surface; the
		// route still mounts on the only available router via the operator
		// surface in those topologies. We intentionally skip mounting in
		// that case — billing-profile is a Tier-2 surface and operator
		// tokens have no use for it.
		return
	}
	ri.Client.ProtectedRouter.Group(func(gated chi.Router) {
		gated.Use(middleware.ModuleGate(ri.ConfigService, m.Name()))
		gated.Group(func(r chi.Router) {
			r.Use(ri.Client.AuthMW.RequireGlobal())
			api := humachi.New(r, ri.APIConfig)
			RegisterMeRoutes(api, m.handler)
		})
	})
}
