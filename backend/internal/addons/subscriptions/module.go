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
	subscriptionHandler *handlers.SubscriptionHandler

	renewalJob *jobs.RenewalJob
	logger     *slog.Logger
}

func NewModule() *SubscriptionsModule { return &SubscriptionsModule{} }

func (m *SubscriptionsModule) Name() string                    { return "subscriptions" }
func (m *SubscriptionsModule) DisplayName() string             { return "Sottoscrizioni" }
func (m *SubscriptionsModule) Description() string             { return "AI services catalog, recurring tenant subscriptions, and activity logs" }
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
		module.ServiceSubscriptionService,
		module.ServiceTenantSubscriptionProvider,
		module.ServiceSelfServiceCheckoutPlanner,
	}
}

func (m *SubscriptionsModule) OptionalServices() []module.ServiceKey {
	return []module.ServiceKey{
		module.ServicePaymentProvider,
		module.ServiceNotificationSender,
		module.ServicePDFService,
		// User billing provider drives renewal of user-owned subscriptions
		// (Phase 2). Optional: when missing, user-owner renewals fail fast
		// while tenant-owner renewals continue unchanged.
		module.ServiceUserBillingCustomerProvider,
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
		{Name: models.SubscriptionsCollection, Indexes: []module.IndexSpec{
			{Keys: map[string]int{"uuid": 1}, Unique: true},
			// Owner-scoped lookups for self-service /v1/me/subscriptions and
			// the admin aggregator GET /v1/admin/tenants/{id}/subscriptions.
			// Owners are polymorphic: Kind="user" for self-registered clients,
			// Kind="tenant" for admin-attached business members.
			{OrderedKeys: []module.IndexKey{
				{Field: "ownerKind", Direction: 1},
				{Field: "ownerUUID", Direction: 1},
				{Field: "status", Direction: 1},
			}},
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
		Realm: "business", Tier: "internal",
		Name: "Subscriptions", Icon: "repeat", Path: "/subscriptions", Active: true,
		Children: []module.NavItemSpec{
			{Name: "Services", Icon: "layer-group", Path: "/subscriptions/services", Active: true},
			{Name: "All Subscriptions", Icon: "list", Path: "/subscriptions/subscriptions", Active: true},
		},
	}}
}

func (m *SubscriptionsModule) Permissions() []iface.PermissionSpec {
	return []iface.PermissionSpec{
		{Key: "subscriptions.service.view", Module: "subscriptions", Description: "View service catalog"},
		{Key: "subscriptions.service.manage", Module: "subscriptions", Description: "Create, update, delete catalog services"},
		{Key: "subscriptions.subscription.view", Module: "subscriptions", Description: "View subscriptions"},
		{Key: "subscriptions.subscription.manage", Module: "subscriptions", Description: "Create, cancel, reactivate subscriptions, retry charges"},
		{Key: "subscriptions.invoice.view", Module: "subscriptions", Description: "View generated subscription invoices"},
		{Key: "subscriptions.activity.view", Module: "subscriptions", Description: "View subscription activity logs"},
	}
}

func (m *SubscriptionsModule) Init(deps *module.Dependencies) error {
	m.logger = deps.Logger

	serviceRepo := repository.NewServiceRepository(deps.DB)
	subRepo := repository.NewSubscriptionRepository(deps.DB)
	invoiceRepo := repository.NewInvoiceRepository(deps.DB)
	activityRepo := repository.NewActivityRepository(deps.DB)

	activitySvc := services.NewActivityService(activityRepo, deps.Logger)
	serviceSvc := services.NewServiceService(serviceRepo, deps.Logger)

	// Resolve the tenant provider for capability entitlement sync and for
	// the renewal service's tenant lookup. Core modules init before addons,
	// so this is always present in a full deployment. Any alternative
	// wiring that runs subscriptions without the tenant module gets a nil
	// provider — the entitlement syncer degrades to no-ops and the renewal
	// flow fails fast on missing tenant data.
	tenantProvider, _ := module.GetTyped[iface.TenantProvider](deps.Services, module.ServiceTenantProvider)
	accessProvider, _ := module.GetTyped[iface.AccessProvider](deps.Services, module.ServiceAccessProvider)
	entitlementSyncer := services.NewEntitlementSyncer(serviceRepo, accessProvider, tenantProvider, deps.Logger)

	subscriptionSvc := services.NewSubscriptionService(subRepo, serviceRepo, activitySvc, entitlementSyncer, tenantProvider, deps.Logger)

	notifier, _ := module.GetTyped[iface.NotificationSender](deps.Services, module.ServiceNotificationSender)
	userBilling, _ := module.GetTyped[iface.UserBillingCustomerProvider](deps.Services, module.ServiceUserBillingCustomerProvider)

	renewalSvc := services.NewRenewalService(
		subRepo, serviceRepo, invoiceRepo,
		activitySvc, entitlementSyncer, tenantProvider, userBilling, notifier, deps.Services, deps.Logger,
	)
	reconciler := services.NewReconciler(invoiceRepo, subRepo, activitySvc, entitlementSyncer, deps.Logger)

	m.serviceHandler = handlers.NewServiceHandler(serviceSvc)
	m.subscriptionHandler = handlers.NewSubscriptionHandler(subscriptionSvc, renewalSvc, invoiceRepo, activitySvc, tenantProvider)

	interval := deps.GetConfigDuration("subscriptions", "renewalInterval", time.Hour)
	m.renewalJob = jobs.NewRenewalJob(renewalSvc, interval, deps.Logger)

	// Publish the reconciler so the payments module can call into us on webhooks.
	deps.Services.Register(module.ServiceSubscriptionReconciler, reconciler)

	// Publish the concrete subscription service so the compliance module
	// can wire its audit sink via SetAuditSink. The interface is the
	// ServiceRegistry's generic `any`; compliance type-asserts on retrieval.
	deps.Services.Register(module.ServiceSubscriptionService, subscriptionSvc)

	// Publish the tenant-scoped subscription read view so core/tenant's
	// admin aggregator endpoint can list a tenant's subscriptions without
	// importing this module. iface.TenantSubscriptionProvider is the
	// contract — consumers resolve it via module.GetTyped.
	deps.Services.Register(module.ServiceTenantSubscriptionProvider, iface.TenantSubscriptionProvider(services.NewTenantSubscriptionAdapter(subRepo)))

	// Publish the self-service checkout planner so the payments client-
	// surface handler can resolve a subscription UUID into a payable
	// snapshot (tenant + pending invoice + tier price) without importing
	// the subscriptions package directly. Optional dependency on payments
	// side — the route returns 503 when this key is missing.
	deps.Services.Register(module.ServiceSelfServiceCheckoutPlanner, iface.SelfServiceCheckoutPlanner(services.NewCheckoutPlanner(subRepo, serviceRepo, invoiceRepo)))

	deps.Logger.Info("Subscriptions module initialized",
		slog.Duration("renewalInterval", interval),
	)
	return nil
}

func (m *SubscriptionsModule) RegisterRoutes(ri *module.RouteInfo) {
	// ADR-0003 PR-D D-7: subscriptions is bucketed as a client-tier module
	// (Tier-2 catalog browse, self-service subscribe, view activity). The
	// Tier-2-facing routes — anonymous catalog and POST /v1/me/subscriptions
	// — move to the client host (api.*) so operator JWTs (aud=operator) no
	// longer satisfy the mux-level RequireAudience gate on those paths.
	//
	// Operator-admin routes (catalog CRUD, subscription admin, invoice /
	// activity reads) stay on the operator host. They gate on
	// RequireInternalTenant() and serve Tier-1 oversight of Tier-2 billing;
	// moving them to the client surface would make them unreachable
	// (operator tokens fail aud=client) with no replacement endpoint in
	// PR-D's scope. Future work can fold them into core/tenant's
	// /v1/admin/tenants/{id}/* aggregator surface or a dedicated admin
	// module once the catalog-management UI moves off the console.

	// --- Client surface (api.*) ---
	//
	// Public catalog — no auth, no gate. Mounted on ri.Client.PublicAPI so
	// the Tier-2 signup UI can render pricing before any credentials exist.
	// Disabling the subscriptions module via /admin/modules does NOT detach
	// this route at runtime (same caveat as onboarding.RegisterRoutes) — to
	// fully stop public exposure, restart with the module disabled before
	// boot (delete its row in module_configs or mark enabled=false).
	RegisterPublicCatalogRoutes(ri.Client.PublicAPI, m.serviceHandler)

	// Self-service subscribe — RequireGlobal() so callers don't need a
	// tenant-scoping header, then the handler enforces ownership via
	// TenantProvider.ListUserMemberships. The mux-level RequireAudience
	// gate already restricts callers to aud=client tokens, so the Tier-2
	// scope is enforced at the surface boundary.
	ri.Client.ProtectedRouter.Group(func(gated chi.Router) {
		gated.Use(middleware.ModuleGate(ri.ConfigService, m.Name()))
		gated.Group(func(r chi.Router) {
			r.Use(ri.Client.AuthMW.RequireGlobal())
			api := humachi.New(r, ri.APIConfig)
			RegisterSelfServiceRoutes(api, m.subscriptionHandler)
		})
	})

	// --- Operator surface (console.*) ---
	//
	// Each permission bucket gets its own chi subgroup. Mutations (POST,
	// PATCH, DELETE) live behind the `.manage` grants so view-level users
	// cannot create subscriptions, change pricing, cancel subscriptions, etc.
	// All admin routes require an internal tenant.
	ri.Operator.ProtectedRouter.Group(func(gated chi.Router) {
		gated.Use(middleware.ModuleGate(ri.ConfigService, m.Name()))

		// Services (catalog) — operator admin only.
		gated.Group(func(r chi.Router) {
			r.Use(ri.Operator.AuthMW.RequireInternalTenant())
			r.Use(ri.Operator.AuthMW.RequirePermission("subscriptions.service.view"))
			api := humachi.New(r, ri.APIConfig)
			RegisterServiceReadRoutes(api, m.serviceHandler)
		})
		gated.Group(func(r chi.Router) {
			r.Use(ri.Operator.AuthMW.RequireInternalTenant())
			r.Use(ri.Operator.AuthMW.RequirePermission("subscriptions.service.manage"))
			api := humachi.New(r, ri.APIConfig)
			RegisterServiceWriteRoutes(api, m.serviceHandler)
		})

		// Subscriptions — operator admin (not the external self-subscribe).
		gated.Group(func(r chi.Router) {
			r.Use(ri.Operator.AuthMW.RequireInternalTenant())
			r.Use(ri.Operator.AuthMW.RequirePermission("subscriptions.subscription.view"))
			api := humachi.New(r, ri.APIConfig)
			RegisterSubscriptionReadRoutes(api, m.subscriptionHandler)
		})
		gated.Group(func(r chi.Router) {
			r.Use(ri.Operator.AuthMW.RequireInternalTenant())
			r.Use(ri.Operator.AuthMW.RequirePermission("subscriptions.subscription.manage"))
			api := humachi.New(r, ri.APIConfig)
			RegisterSubscriptionWriteRoutes(api, m.subscriptionHandler)
		})

		// Nested reads — operator admin only.
		gated.Group(func(r chi.Router) {
			r.Use(ri.Operator.AuthMW.RequireInternalTenant())
			r.Use(ri.Operator.AuthMW.RequirePermission("subscriptions.invoice.view"))
			api := humachi.New(r, ri.APIConfig)
			RegisterInvoiceReadRoutes(api, m.subscriptionHandler)
		})
		gated.Group(func(r chi.Router) {
			r.Use(ri.Operator.AuthMW.RequireInternalTenant())
			r.Use(ri.Operator.AuthMW.RequirePermission("subscriptions.activity.view"))
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
