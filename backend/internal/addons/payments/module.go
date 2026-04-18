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
	return []module.ServiceKey{module.ServicePaymentProvider}
}

func (m *PaymentsModule) OptionalServices() []module.ServiceKey {
	return []module.ServiceKey{module.ServiceSubscriptionReconciler}
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
			{Keys: map[string]int{"status": 1}},
		}},
		{Name: models.PaymentMethodsCollection, Indexes: []module.IndexSpec{
			{Keys: map[string]int{"uuid": 1}, Unique: true},
			{Keys: map[string]int{"clientUUID": 1, "provider": 1}},
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
		Group: "Administration", Name: "Pagamenti", Icon: "credit-card", Path: "/payments", Active: true,
		Children: []module.NavItemSpec{
			{Name: "Transazioni", Icon: "exchange-alt", Path: "/payments/transactions", Active: true},
			{Name: "Metodi di Pagamento", Icon: "wallet", Path: "/payments/methods", Active: true},
			{Name: "Webhook", Icon: "bell", Path: "/payments/webhooks", Active: true},
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
	m.txHandler = handlers.NewTransactionHandler(txRepo, pmRepository, whRepo, m.payment, deps.Services)
	m.stripeHandler = webhooks.NewStripeHandler(m.payment, m.dispatcher, deps.Logger)

	// Register the façade as the PaymentProvider for the subscriptions
	// module to consume. Even if no Stripe key is configured, we still
	// register so subscriptions sees a live provider (charge calls will
	// simply error out until a key is set via the admin UI).
	deps.Services.Register(module.ServicePaymentProvider, m.payment)

	deps.Logger.Info("Payments module initialized",
		slog.String("defaultProvider", string(defaultProvider)),
		slog.Bool("stripeConfigured", m.hasStripe),
	)
	return nil
}

func (m *PaymentsModule) RegisterRoutes(ri *module.RouteInfo) {
	// Admin API — each permission bucket gets its own chi subgroup with a
	// dedicated RequirePermission middleware. Refunds sit behind the
	// distinct `payments.transaction.refund` grant so read access cannot
	// authorise a mutation.
	ri.ProtectedRouter.Group(func(gated chi.Router) {
		gated.Use(middleware.ModuleGate(ri.ConfigService, m.Name()))

		gated.Group(func(r chi.Router) {
			r.Use(ri.AuthMW.RequirePermission("payments.transaction.view"))
			api := humachi.New(r, ri.APIConfig)
			RegisterTransactionReadRoutes(api, m.txHandler)
		})

		gated.Group(func(r chi.Router) {
			r.Use(ri.AuthMW.RequirePermission("payments.transaction.refund"))
			api := humachi.New(r, ri.APIConfig)
			RegisterTransactionRefundRoutes(api, m.txHandler)
		})

		gated.Group(func(r chi.Router) {
			r.Use(ri.AuthMW.RequirePermission("payments.method.view"))
			api := humachi.New(r, ri.APIConfig)
			RegisterPaymentMethodReadRoutes(api, m.txHandler)
		})

		gated.Group(func(r chi.Router) {
			r.Use(ri.AuthMW.RequirePermission("payments.webhook.view"))
			api := humachi.New(r, ri.APIConfig)
			RegisterWebhookEventReadRoutes(api, m.txHandler)
		})
	})

	// Public webhook — no JWT, HMAC-verified inside the handler. Uses the
	// root chi router directly so we can read the raw request body (Huma's
	// binding would have already consumed it).
	ri.Router.Post("/v1/payments/webhooks/stripe", m.stripeHandler.ServeHTTP)
}

func (m *PaymentsModule) Start(_ context.Context) error  { return nil }
func (m *PaymentsModule) Stop(_ context.Context) error   { return nil }
func (m *PaymentsModule) HealthCheck(_ context.Context) error {
	return nil
}
