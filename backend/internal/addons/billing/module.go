package billing

import (
	"context"
	"log/slog"
	"time"

	"github.com/danielgtaylor/huma/v2/adapters/humachi"
	"github.com/go-chi/chi/v5"
	billingConfig "github.com/orkestra/backend/internal/addons/billing/config"
	"github.com/orkestra/backend/internal/addons/billing/handlers"
	"github.com/orkestra/backend/internal/addons/billing/jobs"
	"github.com/orkestra/backend/internal/addons/billing/repository"
	"github.com/orkestra/backend/internal/addons/billing/services"
	"github.com/orkestra/backend/internal/shared/capability"
	"github.com/orkestra/backend/internal/shared/config"
	"github.com/orkestra/backend/internal/shared/iface"
	"github.com/orkestra/backend/internal/shared/middleware"
	"github.com/orkestra/backend/internal/shared/module"
)

type BillingModule struct {
	module.BaseModule
	invoiceHandler          *handlers.InvoiceHandler
	customerHandler         *handlers.CustomerHandler
	supplierHandler         *handlers.SupplierHandler
	companyHandler          *handlers.CompanyHandler
	notificationHandler     *handlers.NotificationHandler
	businessRegistryHandler *handlers.BusinessRegistryHandler
	syncHandler             *handlers.SyncHandler
	webhookHandler          *handlers.WebhookHandler

	pollingJob    *jobs.PollingJob
	openAPIClient services.OpenAPIClient
	cfg           *config.Config
	logger        *slog.Logger
}

func NewModule() *BillingModule { return &BillingModule{} }

func (m *BillingModule) Name() string                   { return "billing" }
func (m *BillingModule) DisplayName() string             { return "Fatturazione Elettronica" }
func (m *BillingModule) Description() string             { return "Italian electronic invoicing via FatturaPA/SDI" }
func (m *BillingModule) Category() module.ModuleCategory { return module.CategoryExternal }
func (m *BillingModule) Enabled(cfg *config.Config) bool {
	if cfg.Billing.OpenAPIBearerToken != "" {
		return true
	}
	return cfg.Billing.OpenAPIAccountEmail != "" && cfg.Billing.OpenAPIAPIKey != ""
}
func (m *BillingModule) Dependencies() []string          { return []string{"documents"} }
func (m *BillingModule) OptionalServices() []module.ServiceKey {
	// TenantProvider is optional from the billing module's POV: the
	// promote-tenant flow needs it but normal customer CRUD does not.
	// Listed as optional so the module still boots if tenant somehow
	// fails to init (it shouldn't — tenant is a core module).
	return []module.ServiceKey{module.ServicePDFService, module.ServiceTenantProvider}
}

// ProvidedServices declares the billing.tenant_customer_provider key the
// core/tenant aggregator endpoint resolves. ADR-0001 PR-4.
func (m *BillingModule) ProvidedServices() []module.ServiceKey {
	return []module.ServiceKey{module.ServiceTenantBillingCustomerProvider}
}

func (m *BillingModule) ConfigSchema() []module.ConfigField {
	return []module.ConfigField{
		{Key: "accountEmail", Label: "Account Email", Type: module.FieldString,
			Description: "Email of your OpenAPI.com account. Combined with API Key to mint short-lived JWT bearer tokens automatically.",
			EnvVar:      "OPENAPI_BILLING_ACCOUNT_EMAIL"},
		{Key: "apiKey", Label: "API Key", Type: module.FieldSecret,
			Description: "Long-lived API key from console.openapi.com. The module exchanges it for a JWT at the OAuth host.",
			EnvVar:      "OPENAPI_BILLING_API_KEY"},
		{Key: "oauthBaseURL", Label: "OAuth Base URL", Type: module.FieldString,
			Description: "OpenAPI.com OAuth host. Production: https://oauth.openapi.it · Sandbox: https://test.oauth.openapi.it",
			Default:     "https://oauth.openapi.it",
			EnvVar:      "OPENAPI_OAUTH_BASE_URL"},
		{Key: "bearerToken", Label: "Bearer Token (legacy fallback)", Type: module.FieldSecret,
			Description: "Optional. Static JWT used only when API Key is empty. Leave blank if you've set the API Key above.",
			EnvVar:      "OPENAPI_BILLING_BEARER_TOKEN"},
		{Key: "baseURL", Label: "SDI API Base URL", Type: module.FieldString, Default: "https://test.sdi.openapi.it", EnvVar: "OPENAPI_BILLING_BASE_URL"},
		{Key: "fiscalID", Label: "Codice Fiscale", Type: module.FieldString, Required: true, EnvVar: "OPENAPI_BILLING_FISCAL_ID"},
		{Key: "recipientCode", Label: "Codice Destinatario", Type: module.FieldString, Default: "JKKZDGR", EnvVar: "OPENAPI_BILLING_RECIPIENT_CODE"},
		{Key: "applySignature", Label: "Apply Digital Signature", Type: module.FieldBool, Default: "true", EnvVar: "OPENAPI_BILLING_APPLY_SIGNATURE"},
		{Key: "applyStorage", Label: "Apply Legal Storage", Type: module.FieldBool, Default: "true", EnvVar: "OPENAPI_BILLING_APPLY_STORAGE"},
		{Key: "timeout", Label: "HTTP Timeout", Type: module.FieldDuration, Default: "30s", EnvVar: "OPENAPI_BILLING_TIMEOUT"},
		{Key: "retryAttempts", Label: "Retry Attempts", Type: module.FieldInt, Default: "3", EnvVar: "OPENAPI_BILLING_RETRY_ATTEMPTS"},
		{Key: "sandboxMode", Label: "Sandbox Mode", Type: module.FieldBool, Default: "true", EnvVar: "OPENAPI_SANDBOX_MODE"},
		{Key: "pollingEnabled", Label: "Enable SDI Polling", Type: module.FieldBool, Default: "true", EnvVar: "OPENAPI_BILLING_POLLING_ENABLED"},
		{Key: "pollingInterval", Label: "Polling Interval", Type: module.FieldDuration, Default: "12h", EnvVar: "OPENAPI_BILLING_POLLING_INTERVAL"},
		{Key: "webhookURL", Label: "Webhook URL", Type: module.FieldString, EnvVar: "BILLING_WEBHOOK_URL"},
		{Key: "webhookSecret", Label: "Webhook Secret", Type: module.FieldSecret, EnvVar: "BILLING_WEBHOOK_SECRET"},
	}
}

func (m *BillingModule) HotReloadConfig() bool { return true }

func (m *BillingModule) Collections() []module.CollectionSpec {
	return []module.CollectionSpec{
		{Name: "billing_invoices", Indexes: []module.IndexSpec{{Keys: map[string]int{"uuid": 1}, Unique: true}}},
		{Name: "billing_customers", Indexes: []module.IndexSpec{
			{Keys: map[string]int{"uuid": 1}, Unique: true},
			// Sparse + unique: a tenant has at most one billing profile,
			// but most customers are not tenants. ADR-0001 PR-4.
			{Keys: map[string]int{"tenantUUID": 1}, Sparse: true, Unique: true},
		}},
		{Name: "billing_suppliers", Indexes: []module.IndexSpec{{Keys: map[string]int{"uuid": 1}, Unique: true}}},
		{Name: "billing_companies", Indexes: []module.IndexSpec{{Keys: map[string]int{"uuid": 1}, Unique: true}}},
		{Name: "billing_notifications"},
		{Name: "billing_polling_state"},
	}
}

func (m *BillingModule) NavItems() []module.NavItemSpec {
	return []module.NavItemSpec{{
		Realm: "business", Tier: "internal",
		Name: "Invoicing", Icon: "file-invoice", Path: "/billing",
		Active: true,
		Children: []module.NavItemSpec{
			{Name: "Dashboard", Icon: "chart-pie", Path: "/billing/dashboard", Active: true},
			{Name: "Invoices Issued", Icon: "paper-plane", Path: "/billing/invoices/issued", Active: true},
			{Name: "Invoices Received", Icon: "inbox", Path: "/billing/invoices/received", Active: true},
			{Name: "Customers", Icon: "users", Path: "/billing/customers", Active: true},
			{Name: "Suppliers", Icon: "truck", Path: "/billing/suppliers", Active: true},
			{Name: "SDI Notifications", Icon: "bell", Path: "/billing/notifications", Active: true},
			{Name: "Issuing Companies", Icon: "building", Path: "/billing/companies", Active: true},
		},
	}}
}

func (m *BillingModule) Init(deps *module.Dependencies) error {
	m.cfg = deps.Config
	m.logger = deps.Logger

	configLoader := func() *billingConfig.OpenAPIConfig {
		return &billingConfig.OpenAPIConfig{
			BaseURL:        deps.GetConfig("billing", "baseURL"),
			AccountEmail:   deps.GetConfig("billing", "accountEmail"),
			APIKey:         deps.GetSecret("billing", "apiKey"),
			OAuthBaseURL:   deps.GetConfig("billing", "oauthBaseURL"),
			BearerToken:    deps.GetSecret("billing", "bearerToken"),
			FiscalID:       deps.GetConfig("billing", "fiscalID"),
			RecipientCode:  deps.GetConfig("billing", "recipientCode"),
			ApplySignature: deps.GetConfigBool("billing", "applySignature", true),
			ApplyStorage:   deps.GetConfigBool("billing", "applyStorage", true),
			Timeout:        deps.GetConfigDuration("billing", "timeout", 30*time.Second),
			RetryAttempts:  deps.GetConfigInt("billing", "retryAttempts", 3),
			SandboxMode:    deps.GetConfigBool("billing", "sandboxMode", true),
		}
	}

	invoiceRepo := repository.NewInvoiceRepository(deps.DB)
	customerRepo := repository.NewCustomerRepository(deps.DB)
	supplierRepo := repository.NewSupplierRepository(deps.DB)
	companyRepo := repository.NewCompanyRepository(deps.DB)
	notificationRepo := repository.NewNotificationRepository(deps.DB)

	m.openAPIClient = services.NewOpenAPIClientWithCache(configLoader, deps.Logger, deps.RedisAdapter)
	xmlBuilder := services.NewXMLBuilder(configLoader)

	// Retrieve PDFService from ServiceRegistry (can be nil if documents module is disabled)
	pdfSvc, _ := module.GetTyped[iface.PDFProvider](deps.Services, module.ServicePDFService)

	// Resolve the tenant provider lazily — needed by the
	// PromoteTenantToCustomer flow (ADR-0001 PR-4). Tenant is a core
	// module so it always initializes before billing, but treat the
	// lookup as optional so a misconfigured deployment without tenant
	// still boots the rest of the billing surface.
	tenantProvider, _ := module.GetTyped[iface.TenantProvider](deps.Services, module.ServiceTenantProvider)

	invoiceSvc := services.NewInvoiceService(invoiceRepo, customerRepo, supplierRepo, companyRepo, m.openAPIClient, xmlBuilder, nil, pdfSvc, deps.Logger)
	customerSvc := services.NewCustomerService(customerRepo, tenantProvider, deps.Logger)
	supplierSvc := services.NewSupplierService(supplierRepo, deps.Logger)
	companySvc := services.NewCompanyService(companyRepo, m.openAPIClient, deps.Logger)
	notificationSvc := services.NewNotificationService(notificationRepo, deps.Logger)

	// Publish the tenant→customer aggregator adapter so core/tenant can
	// serve /v1/admin/tenants/{id}/billing-customer without importing
	// this module. ADR-0001 PR-4.
	deps.Services.Register(
		module.ServiceTenantBillingCustomerProvider,
		iface.TenantBillingCustomerProvider(services.NewTenantBillingCustomerAdapter(customerRepo, customerSvc)),
	)

	m.invoiceHandler = handlers.NewInvoiceHandler(invoiceSvc)
	m.customerHandler = handlers.NewCustomerHandler(customerSvc)
	m.supplierHandler = handlers.NewSupplierHandler(supplierSvc)
	m.companyHandler = handlers.NewCompanyHandler(companySvc)
	m.notificationHandler = handlers.NewNotificationHandler(notificationSvc)
	m.businessRegistryHandler = handlers.NewBusinessRegistryHandler(m.openAPIClient)

	m.pollingJob = jobs.NewPollingJob(
		m.openAPIClient,
		invoiceRepo,
		notificationRepo,
		deps.Logger,
		deps.GetConfigDuration("billing", "pollingInterval", 12*time.Hour),
	)

	m.syncHandler = handlers.NewSyncHandler(m.pollingJob)

	webhookURL := deps.GetConfig("billing", "webhookURL")
	if webhookURL != "" {
		m.webhookHandler = handlers.NewWebhookHandler(
			m.pollingJob,
			deps.GetSecret("billing", "webhookSecret"),
			deps.Logger,
		)
	}

	deps.Logger.Info("Billing module initialized",
		slog.String("baseURL", deps.GetConfig("billing", "baseURL")),
		slog.Bool("sandbox", deps.GetConfigBool("billing", "sandboxMode", true)),
	)
	return nil
}

func (m *BillingModule) Permissions() []iface.PermissionSpec {
	return []iface.PermissionSpec{
		{Key: "billing.invoice.read", Module: "billing", Description: "View invoices and notifications"},
		{Key: "billing.invoice.create", Module: "billing", Description: "Create and update invoices"},
		{Key: "billing.invoice.send", Module: "billing", Description: "Send invoices to SDI"},
		{Key: "billing.invoice.delete", Module: "billing", Description: "Delete draft invoices"},
		{Key: "billing.customer.manage", Module: "billing", Description: "Manage customers and suppliers"},
	}
}

func (m *BillingModule) Capabilities() []capability.Capability {
	return []capability.Capability{
		{
			ID:          "billing.access",
			Module:      "billing",
			Action:      "access",
			Title:       "Italian Electronic Invoicing",
			Description: "Issue, send, and receive FatturaPA invoices via the SDI (Sistema di Interscambio).",
			Published:   true,
		},
	}
}

func (m *BillingModule) RegisterRoutes(ri *module.RouteInfo) {
	// Protected billing routes: operator-only — FatturaPA and SDI are
	// internal-tenant concerns, so wrap with RequireInternalTenant to refuse
	// any external-tenant token. Rollout respects TENANT_KIND_ENFORCEMENT
	// (warn|enforce) so operators can probe traffic before the gate starts
	// returning 403.
	ri.Operator.ProtectedRouter.Group(func(r chi.Router) {
		r.Use(middleware.ModuleGate(ri.ConfigService, m.Name()))
		r.Use(ri.Operator.AuthMW.RequireInternalTenant())
		r.Use(ri.Operator.AuthMW.RequireCapability("billing.access"))
		r.Use(ri.Operator.AuthMW.RequirePermission("billing.invoice.read"))
		api := humachi.New(r, ri.APIConfig)
		RegisterRoutes(
			api,
			m.invoiceHandler,
			m.customerHandler,
			m.supplierHandler,
			m.companyHandler,
			m.notificationHandler,
			m.businessRegistryHandler,
			m.syncHandler,
		)
	})

	// Public webhook routes (no JWT, authenticated via webhook secret)
	if m.webhookHandler != nil {
		RegisterWebhookRoutes(ri.Operator.PublicAPI, m.webhookHandler)
		m.logger.Info("Billing webhook routes registered",
			slog.String("webhookURL", m.cfg.Billing.WebhookURL),
		)
	}
}

func (m *BillingModule) Start(_ context.Context) error {
	cfg := m.cfg

	// Start SDI notification polling job
	if m.pollingJob != nil && cfg.Billing.PollingEnabled {
		pollingCtx, pollingCancel := context.WithCancel(context.Background())
		_ = pollingCancel // Will be cancelled via Stop()

		go func() {
			m.logger.Info("Starting SDI notification polling job",
				slog.Duration("interval", cfg.Billing.PollingInterval),
			)
			m.pollingJob.Start(pollingCtx)
		}()
	} else if !cfg.Billing.PollingEnabled {
		m.logger.Info("SDI polling job disabled - use manual sync endpoints instead (POST /v1/billing/sync)")
	}

	// Auto-configure API callbacks on OpenAPI.it for webhook reception
	if cfg.Billing.WebhookURL != "" && m.openAPIClient != nil {
		go func() {
			configCtx, configCancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer configCancel()

			authHeader := "Bearer " + cfg.Billing.WebhookSecret
			err := m.openAPIClient.ConfigureAPICallbacks(configCtx, services.APICallbackConfig{
				FiscalID: cfg.Billing.OpenAPIFiscalID,
				Callbacks: []services.CallbackConfig{
					{Event: "supplier-invoice", URL: cfg.Billing.WebhookURL, AuthHeader: authHeader},
					{Event: "customer-notification", URL: cfg.Billing.WebhookURL, AuthHeader: authHeader},
					{Event: "legal-storage-receipt", URL: cfg.Billing.WebhookURL, AuthHeader: authHeader},
				},
			})
			if err != nil {
				m.logger.Error("Failed to configure API callbacks on OpenAPI.it",
					slog.String("error", err.Error()),
					slog.String("webhookURL", cfg.Billing.WebhookURL),
				)
			} else {
				m.logger.Info("API callbacks configured on OpenAPI.it",
					slog.String("webhookURL", cfg.Billing.WebhookURL),
					slog.String("fiscalID", cfg.Billing.OpenAPIFiscalID),
				)
			}
		}()
	}

	return nil
}

func (m *BillingModule) Stop(_ context.Context) error {
	if m.pollingJob != nil {
		m.logger.Info("Stopping SDI notification polling job")
		m.pollingJob.Stop()
	}
	return nil
}

func (m *BillingModule) HealthCheck(_ context.Context) error { return nil }
