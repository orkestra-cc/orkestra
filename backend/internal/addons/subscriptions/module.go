package subscriptions

import (
	"context"
	"log/slog"
	"time"

	"github.com/danielgtaylor/huma/v2/adapters/humachi"
	"github.com/go-chi/chi/v5"
	"github.com/orkestra/backend/internal/addons/subscriptions/handlers"
	"github.com/orkestra/backend/internal/addons/subscriptions/jobs"
	"github.com/orkestra/backend/internal/addons/subscriptions/models"
	"github.com/orkestra/backend/internal/addons/subscriptions/repository"
	"github.com/orkestra/backend/internal/addons/subscriptions/services"
	"github.com/orkestra/backend/internal/shared/config"
	"github.com/orkestra/backend/internal/shared/iface"
	"github.com/orkestra/backend/internal/shared/middleware"
	"github.com/orkestra/backend/internal/shared/module"
)

type SubscriptionsModule struct {
	module.BaseModule

	serviceHandler      *handlers.ServiceHandler
	clientHandler       *handlers.ClientHandler
	subscriptionHandler *handlers.SubscriptionHandler

	renewalJob *jobs.RenewalJob
	logger     *slog.Logger
}

func NewModule() *SubscriptionsModule { return &SubscriptionsModule{} }

func (m *SubscriptionsModule) Name() string                    { return "subscriptions" }
func (m *SubscriptionsModule) DisplayName() string             { return "Sottoscrizioni" }
func (m *SubscriptionsModule) Description() string             { return "AI services catalog, recurring client subscriptions, and activity logs" }
func (m *SubscriptionsModule) Category() module.ModuleCategory { return module.CategoryToggleable }
func (m *SubscriptionsModule) Enabled(_ *config.Config) bool   { return true }
func (m *SubscriptionsModule) HotReloadConfig() bool           { return true }

// Intentionally empty: the payments module is consumed via the
// ServiceRegistry lazily (paymentProvider() in RenewalService), so it must
// NOT appear in Dependencies() — that would force a cycle if payments ever
// needed something from subscriptions.
func (m *SubscriptionsModule) Dependencies() []string { return nil }

func (m *SubscriptionsModule) ProvidedServices() []module.ServiceKey {
	return []module.ServiceKey{
		module.ServiceSubscriptionReconciler,
		module.ServiceClientOwnership,
		module.ServiceSubscriptionService,
	}
}

func (m *SubscriptionsModule) OptionalServices() []module.ServiceKey {
	return []module.ServiceKey{
		module.ServicePaymentProvider,
		module.ServiceNotificationSender,
		module.ServicePDFService,
	}
}

func (m *SubscriptionsModule) ConfigSchema() []module.ConfigField {
	return []module.ConfigField{
		{Key: "renewalInterval", Label: "Renewal Job Interval", Type: module.FieldDuration, Default: "1h", EnvVar: "SUBSCRIPTIONS_RENEWAL_INTERVAL"},
		{Key: "defaultVATRate", Label: "Default VAT Rate (percent)", Type: module.FieldString, Default: "22", EnvVar: "SUBSCRIPTIONS_VAT_RATE"},
	}
}

func (m *SubscriptionsModule) Collections() []module.CollectionSpec {
	return []module.CollectionSpec{
		{Name: models.ServicesCollection, Indexes: []module.IndexSpec{
			{Keys: map[string]int{"uuid": 1}, Unique: true},
			{Keys: map[string]int{"code": 1}, Unique: true},
			{Keys: map[string]int{"active": 1, "category": 1}},
		}},
		{Name: models.ClientsCollection, Indexes: []module.IndexSpec{
			{Keys: map[string]int{"uuid": 1}, Unique: true},
			{Keys: map[string]int{"email": 1}, Unique: true},
			{Keys: map[string]int{"vatNumber": 1}, Sparse: true},
			{Keys: map[string]int{"orgUUID": 1}, Sparse: true},
		}},
		{Name: models.SubscriptionsCollection, Indexes: []module.IndexSpec{
			{Keys: map[string]int{"uuid": 1}, Unique: true},
			{Keys: map[string]int{"clientUUID": 1, "status": 1}},
			{Keys: map[string]int{"nextBillingAt": 1, "status": 1}},
			{Keys: map[string]int{"serviceUUID": 1}},
		}},
		{Name: models.InvoicesCollection, Indexes: []module.IndexSpec{
			{Keys: map[string]int{"uuid": 1}, Unique: true},
			{Keys: map[string]int{"number": 1}, Unique: true},
			{Keys: map[string]int{"subscriptionUUID": 1, "periodStart": 1}, Unique: true},
			{Keys: map[string]int{"status": 1, "dueAt": 1}},
		}},
		{Name: models.ActivityCollection, Indexes: []module.IndexSpec{
			{Keys: map[string]int{"subscriptionUUID": 1, "createdAt": -1}},
		}},
	}
}

func (m *SubscriptionsModule) NavItems() []module.NavItemSpec {
	return []module.NavItemSpec{{
		Group: "Administration", Name: "Sottoscrizioni", Icon: "repeat", Path: "/subscriptions", Active: true,
		Children: []module.NavItemSpec{
			{Name: "Servizi", Icon: "layer-group", Path: "/subscriptions/services", Active: true},
			{Name: "Clienti", Icon: "users", Path: "/subscriptions/clients", Active: true},
			{Name: "Sottoscrizioni", Icon: "list", Path: "/subscriptions/subscriptions", Active: true},
		},
	}}
}

func (m *SubscriptionsModule) Permissions() []iface.PermissionSpec {
	return []iface.PermissionSpec{
		{Key: "subscriptions.service.view", Module: "subscriptions", Description: "View service catalog"},
		{Key: "subscriptions.service.manage", Module: "subscriptions", Description: "Create, update, delete catalog services"},
		{Key: "subscriptions.client.view", Module: "subscriptions", Description: "View subscription clients"},
		{Key: "subscriptions.client.manage", Module: "subscriptions", Description: "Create, update, archive clients"},
		{Key: "subscriptions.subscription.view", Module: "subscriptions", Description: "View subscriptions"},
		{Key: "subscriptions.subscription.manage", Module: "subscriptions", Description: "Create, cancel, reactivate subscriptions, retry charges"},
		{Key: "subscriptions.invoice.view", Module: "subscriptions", Description: "View generated subscription invoices"},
		{Key: "subscriptions.activity.view", Module: "subscriptions", Description: "View subscription activity logs"},
	}
}

func (m *SubscriptionsModule) Init(deps *module.Dependencies) error {
	m.logger = deps.Logger

	serviceRepo := repository.NewServiceRepository(deps.DB)
	clientRepo := repository.NewClientRepository(deps.DB)
	subRepo := repository.NewSubscriptionRepository(deps.DB)
	invoiceRepo := repository.NewInvoiceRepository(deps.DB)
	activityRepo := repository.NewActivityRepository(deps.DB)

	activitySvc := services.NewActivityService(activityRepo, deps.Logger)
	serviceSvc := services.NewServiceService(serviceRepo, deps.Logger)
	clientSvc := services.NewClientService(clientRepo, deps.Logger)

	// Resolve the tenant provider for capability entitlement sync. Core
	// modules init before addons, so this is always present in a full
	// deployment. Any alternative wiring that runs subscriptions without
	// the tenant module gets a nil provider and the syncer degrades to
	// no-ops — subscription state machines still work.
	tenantProvider, _ := module.GetTyped[iface.TenantProvider](deps.Services, module.ServiceTenantProvider)
	entitlementSyncer := services.NewEntitlementSyncer(serviceRepo, tenantProvider, deps.Logger)

	subscriptionSvc := services.NewSubscriptionService(subRepo, clientRepo, serviceRepo, activitySvc, entitlementSyncer, deps.Logger)

	notifier, _ := module.GetTyped[iface.NotificationSender](deps.Services, module.ServiceNotificationSender)

	renewalSvc := services.NewRenewalService(
		subRepo, clientRepo, serviceRepo, invoiceRepo,
		activitySvc, entitlementSyncer, notifier, deps.Services, deps.Logger,
	)
	reconciler := services.NewReconciler(invoiceRepo, subRepo, activitySvc, entitlementSyncer, deps.Logger)

	ownershipSvc := services.NewClientOwnershipService(clientRepo)

	m.serviceHandler = handlers.NewServiceHandler(serviceSvc)
	m.clientHandler = handlers.NewClientHandler(clientSvc)
	m.subscriptionHandler = handlers.NewSubscriptionHandler(subscriptionSvc, renewalSvc, invoiceRepo, activitySvc, ownershipSvc, tenantProvider)

	interval := deps.GetConfigDuration("subscriptions", "renewalInterval", time.Hour)
	m.renewalJob = jobs.NewRenewalJob(renewalSvc, interval, deps.Logger)

	// Publish the reconciler so the payments module can call into us on webhooks.
	deps.Services.Register(module.ServiceSubscriptionReconciler, reconciler)

	// Publish a client-ownership resolver so cross-module handlers (payments)
	// can gate by-id reads/mutations against the requesting user's org.
	deps.Services.Register(module.ServiceClientOwnership, ownershipSvc)

	// Publish the concrete subscription service so the compliance module
	// can wire its audit sink via SetAuditSink. The interface is the
	// ServiceRegistry's generic `any`; compliance type-asserts on retrieval.
	deps.Services.Register(module.ServiceSubscriptionService, subscriptionSvc)

	deps.Logger.Info("Subscriptions module initialized",
		slog.Duration("renewalInterval", interval),
	)
	return nil
}

func (m *SubscriptionsModule) RegisterRoutes(ri *module.RouteInfo) {
	// Public catalog — no auth, no gate. Mounted on ri.PublicAPI so
	// anonymous signup UIs can render pricing before login. Disabling the
	// subscriptions module via /admin/modules does NOT detach this route
	// at runtime (same caveat as onboarding.RegisterRoutes in commit 3.1) —
	// operators restart with MODULES= to fully stop public exposure.
	RegisterPublicCatalogRoutes(ri.PublicAPI, m.serviceHandler)

	// Each permission bucket gets its own chi subgroup. Mutations (POST,
	// PATCH, DELETE) live behind the `.manage` grants so view-level users
	// cannot create clients, change pricing, cancel subscriptions, etc.
	ri.ProtectedRouter.Group(func(gated chi.Router) {
		gated.Use(middleware.ModuleGate(ri.ConfigService, m.Name()))

		// Services (catalog)
		gated.Group(func(r chi.Router) {
			r.Use(ri.AuthMW.RequirePermission("subscriptions.service.view"))
			api := humachi.New(r, ri.APIConfig)
			RegisterServiceReadRoutes(api, m.serviceHandler)
		})
		gated.Group(func(r chi.Router) {
			r.Use(ri.AuthMW.RequirePermission("subscriptions.service.manage"))
			api := humachi.New(r, ri.APIConfig)
			RegisterServiceWriteRoutes(api, m.serviceHandler)
		})

		// Clients
		gated.Group(func(r chi.Router) {
			r.Use(ri.AuthMW.RequirePermission("subscriptions.client.view"))
			api := humachi.New(r, ri.APIConfig)
			RegisterClientReadRoutes(api, m.clientHandler)
		})
		gated.Group(func(r chi.Router) {
			r.Use(ri.AuthMW.RequirePermission("subscriptions.client.manage"))
			api := humachi.New(r, ri.APIConfig)
			RegisterClientWriteRoutes(api, m.clientHandler)
		})

		// Subscriptions
		gated.Group(func(r chi.Router) {
			r.Use(ri.AuthMW.RequirePermission("subscriptions.subscription.view"))
			api := humachi.New(r, ri.APIConfig)
			RegisterSubscriptionReadRoutes(api, m.subscriptionHandler)
		})
		gated.Group(func(r chi.Router) {
			r.Use(ri.AuthMW.RequirePermission("subscriptions.subscription.manage"))
			api := humachi.New(r, ri.APIConfig)
			RegisterSubscriptionWriteRoutes(api, m.subscriptionHandler)
		})

		// Self-service subscribe — RequireGlobal() so callers don't need a
		// tenant-scoping header, then the handler enforces ownership via
		// TenantProvider.ListUserMemberships. No permission grant is
		// required; tenant owners are an implicit role here.
		gated.Group(func(r chi.Router) {
			r.Use(ri.AuthMW.RequireGlobal())
			api := humachi.New(r, ri.APIConfig)
			RegisterSelfServiceRoutes(api, m.subscriptionHandler)
		})

		// Nested reads
		gated.Group(func(r chi.Router) {
			r.Use(ri.AuthMW.RequirePermission("subscriptions.invoice.view"))
			api := humachi.New(r, ri.APIConfig)
			RegisterInvoiceReadRoutes(api, m.subscriptionHandler)
		})
		gated.Group(func(r chi.Router) {
			r.Use(ri.AuthMW.RequirePermission("subscriptions.activity.view"))
			api := humachi.New(r, ri.APIConfig)
			RegisterActivityReadRoutes(api, m.subscriptionHandler)
		})
	})
}

func (m *SubscriptionsModule) Start(_ context.Context) error {
	if m.renewalJob != nil {
		ctx, cancel := context.WithCancel(context.Background())
		_ = cancel // cancelled via Stop()
		go m.renewalJob.Start(ctx)
	}
	return nil
}

func (m *SubscriptionsModule) Stop(_ context.Context) error {
	if m.renewalJob != nil {
		m.renewalJob.Stop()
	}
	return nil
}

func (m *SubscriptionsModule) HealthCheck(_ context.Context) error { return nil }
