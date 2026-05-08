package payments

import (
	"context"
	"log/slog"

	"github.com/danielgtaylor/huma/v2/adapters/humachi"
	"github.com/go-chi/chi/v5"
	"github.com/orkestra/backend/internal/addons/payments/handlers"
	"github.com/orkestra/backend/internal/addons/payments/models"
	stripeProvider "github.com/orkestra/backend/internal/addons/payments/providers/stripe"
	"github.com/orkestra/backend/internal/addons/payments/repository"
	"github.com/orkestra/backend/internal/addons/payments/services"
	"github.com/orkestra/backend/internal/addons/payments/webhooks"
	"github.com/orkestra/backend/internal/shared/config"
	"github.com/orkestra/backend/internal/shared/iface"
	"github.com/orkestra/backend/internal/shared/middleware"
	"github.com/orkestra/backend/internal/shared/module"
)

type PaymentsModule struct {
	module.BaseModule

	payment       *services.PaymentService
	dispatcher    *services.Dispatcher
	txHandler     *handlers.TransactionHandler
	stripeHandler *webhooks.StripeHandler
	clientHandler *handlers.ClientHandler // nil when TenantProvider is unavailable

	hasStripe bool
	logger    *slog.Logger
}

func NewModule() *PaymentsModule { return &PaymentsModule{} }

func (m *PaymentsModule) Name() string                    { return "payments" }
func (m *PaymentsModule) DisplayName() string             { return "Pagamenti" }
func (m *PaymentsModule) Description() string             { return "Payment gateway integration (Stripe) — charges, refunds, webhooks" }
func (m *PaymentsModule) Category() module.ModuleCategory { return module.CategoryExternal }

// Enabled returns true whenever the module has been registered — it can be
// toggled on via the admin UI and will start accepting webhooks as soon as
// a Stripe API key is configured.
func (m *PaymentsModule) Enabled(_ *config.Config) bool { return true }
func (m *PaymentsModule) HotReloadConfig() bool         { return true }

// See subscriptions/module.go for the cycle-free wiring rationale — neither
// module declares the other in Dependencies().
func (m *PaymentsModule) Dependencies() []string { return nil }

func (m *PaymentsModule) ProvidedServices() []module.ServiceKey {
	return []module.ServiceKey{module.ServicePaymentProvider, module.ServiceTenantPaymentProvider}
}

func (m *PaymentsModule) OptionalServices() []module.ServiceKey {
	return []module.ServiceKey{
		module.ServiceSubscriptionReconciler,
		// Tenant + checkout planner power the Tier-2 client-surface
		// self-service routes. Both are optional — when missing, the
		// /v1/me/* routes are simply not mounted (logged at Init time).
		module.ServiceTenantProvider,
		module.ServiceSelfServiceCheckoutPlanner,
	}
}

func (m *PaymentsModule) ConfigSchema() []module.ConfigField {
	return []module.ConfigField{
		{Key: "defaultProvider", Label: "Default Provider", Type: module.FieldString, Default: "stripe"},
		{Key: "stripeApiKey", Label: "Stripe Secret Key", Type: module.FieldSecret, EnvVar: "STRIPE_API_KEY", Description: "Starts with sk_test_ or sk_live_"},
		{Key: "stripeWebhookSecret", Label: "Stripe Webhook Signing Secret", Type: module.FieldSecret, EnvVar: "STRIPE_WEBHOOK_SECRET", Description: "From the Stripe CLI or Dashboard webhook endpoint"},
		{Key: "stripeApiVersion", Label: "Stripe API Version (optional pin)", Type: module.FieldString, Default: "2024-12-18.acacia", EnvVar: "STRIPE_API_VERSION"},
	}
}

func (m *PaymentsModule) Collections() []module.CollectionSpec {
	return []module.CollectionSpec{
		{Name: models.TransactionsCollection, Indexes: []module.IndexSpec{
			{Keys: map[string]int{"uuid": 1}, Unique: true},
			{OrderedKeys: []module.IndexKey{{Field: "provider", Direction: 1}, {Field: "providerTxID", Direction: 1}}, Unique: true, Sparse: true},
			{Keys: map[string]int{"subscriptionUUID": 1, "createdAt": -1}},
			// Tenant-scoped lookups for self-service /v1/me/transactions
			// and the admin aggregator GET /v1/admin/tenants/{id}/payments.
			{OrderedKeys: []module.IndexKey{
				{Field: "tenantUUID", Direction: 1},
				{Field: "createdAt", Direction: -1},
			}},
			{Keys: map[string]int{"status": 1}},
		}},
		{Name: models.PaymentMethodsCollection, Indexes: []module.IndexSpec{
			{Keys: map[string]int{"uuid": 1}, Unique: true},
			{OrderedKeys: []module.IndexKey{
				{Field: "tenantUUID", Direction: 1},
				{Field: "provider", Direction: 1},
			}},
			{Keys: map[string]int{"providerMethodID": 1}, Unique: true, Sparse: true},
		}},
		{Name: models.WebhookEventsCollection, Indexes: []module.IndexSpec{
			{Keys: map[string]int{"uuid": 1}, Unique: true},
			{OrderedKeys: []module.IndexKey{{Field: "provider", Direction: 1}, {Field: "providerEventID", Direction: 1}}, Unique: true},
		}},
	}
}

func (m *PaymentsModule) NavItems() []module.NavItemSpec {
	return []module.NavItemSpec{{
		Realm: "business", Tier: "internal",
		Name: "Payments", Icon: "credit-card", Path: "/payments", Active: true,
		Children: []module.NavItemSpec{
			{Name: "Transactions", Icon: "exchange-alt", Path: "/payments/transactions", Active: true},
			{Name: "Payment Methods", Icon: "wallet", Path: "/payments/methods", Active: true},
			{Name: "Webhooks", Icon: "bell", Path: "/payments/webhooks", Active: true},
		},
	}}
}

func (m *PaymentsModule) Permissions() []iface.PermissionSpec {
	return []iface.PermissionSpec{
		{Key: "payments.transaction.view", Module: "payments", Description: "View payment transactions"},
		{Key: "payments.transaction.refund", Module: "payments", Description: "Issue refunds on transactions"},
		{Key: "payments.method.view", Module: "payments", Description: "View stored payment methods"},
		{Key: "payments.method.manage", Module: "payments", Description: "Attach/detach payment methods"},
		{Key: "payments.webhook.view", Module: "payments", Description: "View webhook event audit log"},
	}
}

func (m *PaymentsModule) Init(deps *module.Dependencies) error {
	m.logger = deps.Logger

	txRepo := repository.NewTransactionRepository(deps.DB)
	pmRepository := repository.NewPaymentMethodRepository(deps.DB)
	whRepo := repository.NewWebhookEventRepository(deps.DB)

	providers := map[models.ProviderName]iface.PaymentProvider{}
	apiKey := deps.GetSecret("payments", "stripeApiKey")
	if apiKey != "" {
		stripeProv, err := stripeProvider.New(stripeProvider.Config{
			APIKey:        apiKey,
			WebhookSecret: deps.GetSecret("payments", "stripeWebhookSecret"),
			APIVersion:    deps.GetConfig("payments", "stripeApiVersion"),
		}, deps.Logger)
		if err != nil {
			deps.Logger.Error("payments: stripe init failed", slog.String("error", err.Error()))
		} else {
			providers[models.ProviderStripe] = stripeProv
			m.hasStripe = true
		}
	}

	defaultProvider := models.ProviderName(deps.GetConfig("payments", "defaultProvider"))
	if defaultProvider == "" {
		defaultProvider = models.ProviderStripe
	}

	m.payment = services.NewPaymentService(defaultProvider, providers, txRepo, deps.Logger)
	m.dispatcher = services.NewDispatcher(whRepo, deps.Services, deps.Logger)
	m.txHandler = handlers.NewTransactionHandler(txRepo, pmRepository, whRepo, m.payment)
	m.stripeHandler = webhooks.NewStripeHandler(m.payment, m.dispatcher, deps.Logger)

	// Register the façade as the PaymentProvider for the subscriptions
	// module to consume. Even if no Stripe key is configured, we still
	// register so subscriptions sees a live provider (charge calls will
	// simply error out until a key is set via the admin UI).
	deps.Services.Register(module.ServicePaymentProvider, m.payment)

	// Publish the tenant-scoped payment read view so core/tenant's admin
	// aggregator endpoint can list a tenant's transactions without
	// importing this module. Matches the subscriptions adapter pattern.
	deps.Services.Register(module.ServiceTenantPaymentProvider, iface.TenantPaymentProvider(services.NewTenantPaymentAdapter(txRepo)))

	// Tier-2 self-service handler — built only when TenantProvider is wired
	// because every /v1/me/* route re-checks ownership against memberships
	// (and materializes the caller's personal tenant via EnsureTenantForUser).
	// SelfServiceCheckoutPlanner stays optional even when the handler is
	// built; its absence degrades the checkout-session route to 503.
	if tenants, ok := module.GetTyped[iface.TenantProvider](deps.Services, module.ServiceTenantProvider); ok {
		planner, _ := module.GetTyped[iface.SelfServiceCheckoutPlanner](deps.Services, module.ServiceSelfServiceCheckoutPlanner)
		m.clientHandler = handlers.NewClientHandler(m.payment, txRepo, pmRepository, tenants, planner)
	} else {
		deps.Logger.Warn("payments: tenant provider missing — Tier-2 self-service routes will not mount")
	}

	deps.Logger.Info("Payments module initialized",
		slog.String("defaultProvider", string(defaultProvider)),
		slog.Bool("stripeConfigured", m.hasStripe),
		slog.Bool("clientSelfService", m.clientHandler != nil),
	)
	return nil
}

func (m *PaymentsModule) RegisterRoutes(ri *module.RouteInfo) {
	// ADR-0003 PR-D D-7: payments is bucketed as a client-tier module
	// (Stripe billing for Tier-2 subscriptions). The Stripe webhook moves
	// to the client host (api.*) so the inbound POST lands on the audience
	// that owns the billing relationship; the URL needs to be reconfigured
	// in the Stripe dashboard / CLI to point at api.orkestra.com (dev:
	// api.localhost:3000).
	//
	// Operator-admin routes (transaction list/refund, payment-method view,
	// webhook audit log) stay on the operator host. They gate on
	// RequireInternalTenant() and serve Tier-1 oversight; moving them to
	// the client surface would make them unreachable (operator tokens fail
	// aud=client) with no replacement endpoint in PR-D's scope. The
	// /v1/admin/tenants/{id}/payments aggregator on core/tenant remains the
	// per-tenant operator read path and is unaffected.

	// --- Operator surface (console.*) ---
	//
	// Admin API — each permission bucket gets its own chi subgroup with a
	// dedicated RequirePermission middleware. Refunds sit behind the
	// distinct `payments.transaction.refund` grant so read access cannot
	// authorise a mutation.
	ri.Operator.ProtectedRouter.Group(func(gated chi.Router) {
		gated.Use(middleware.ModuleGate(ri.ConfigService, m.Name()))

		gated.Group(func(r chi.Router) {
			r.Use(ri.Operator.AuthMW.RequireInternalTenant())
			r.Use(ri.Operator.AuthMW.RequirePermission("payments.transaction.view"))
			api := humachi.New(r, ri.APIConfig)
			RegisterTransactionReadRoutes(api, m.txHandler)
		})

		gated.Group(func(r chi.Router) {
			r.Use(ri.Operator.AuthMW.RequireInternalTenant())
			r.Use(ri.Operator.AuthMW.RequirePermission("payments.transaction.refund"))
			api := humachi.New(r, ri.APIConfig)
			RegisterTransactionRefundRoutes(api, m.txHandler)
		})

		gated.Group(func(r chi.Router) {
			r.Use(ri.Operator.AuthMW.RequireInternalTenant())
			r.Use(ri.Operator.AuthMW.RequirePermission("payments.method.view"))
			api := humachi.New(r, ri.APIConfig)
			RegisterPaymentMethodReadRoutes(api, m.txHandler)
		})

		gated.Group(func(r chi.Router) {
			r.Use(ri.Operator.AuthMW.RequireInternalTenant())
			r.Use(ri.Operator.AuthMW.RequirePermission("payments.webhook.view"))
			api := humachi.New(r, ri.APIConfig)
			RegisterWebhookEventReadRoutes(api, m.txHandler)
		})
	})

	// --- Client surface (api.*) ---
	//
	// Tier-2 self-service routes (/v1/me/transactions, /v1/me/payment-
	// methods, /v1/me/payments/checkout-session, /v1/me/payments/
	// setup-checkout-session). Mounted on ri.Client.ProtectedRouter behind
	// RequireGlobal() — the handlers re-check ownership against
	// TenantProvider memberships, so no per-route RBAC permission is
	// required (mirrors the subscriptions self-subscribe pattern). Skipped
	// entirely when m.clientHandler is nil (tenant module disabled).
	if m.clientHandler != nil {
		ri.Client.ProtectedRouter.Group(func(gated chi.Router) {
			gated.Use(middleware.ModuleGate(ri.ConfigService, m.Name()))
			gated.Group(func(r chi.Router) {
				r.Use(ri.Client.AuthMW.RequireGlobal())
				api := humachi.New(r, ri.APIConfig)
				RegisterClientSelfServiceRoutes(api, m.clientHandler)
			})
		})
	}

	// Public Stripe webhook — no JWT, HMAC-verified inside the handler.
	// Mounted on the client root router so we can read the raw request
	// body (Huma's binding would have already consumed it). Mux-level
	// RequireAudience passes through bearer-less requests, so the webhook
	// remains anonymous.
	if ri.ClientRouter != nil {
		ri.ClientRouter.Post("/v1/payments/webhooks/stripe", m.stripeHandler.ServeHTTP)
	}
}

func (m *PaymentsModule) Start(_ context.Context) error  { return nil }
func (m *PaymentsModule) Stop(_ context.Context) error   { return nil }
func (m *PaymentsModule) HealthCheck(_ context.Context) error {
	return nil
}
