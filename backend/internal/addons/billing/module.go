package billing

import (
	"context"
	"log/slog"
	"os"
	"time"

	"github.com/danielgtaylor/huma/v2/adapters/humachi"
	"github.com/go-chi/chi/v5"
	"github.com/orkestra-cc/orkestra-sdk/capability"
	"github.com/orkestra-cc/orkestra-sdk/iface"
	"github.com/orkestra-cc/orkestra-sdk/module"
	"github.com/orkestra-cc/orkestra-sdk/modulegate"
	billingConfig "github.com/orkestra/backend/internal/addons/billing/config"
	"github.com/orkestra/backend/internal/addons/billing/handlers"
	"github.com/orkestra/backend/internal/addons/billing/jobs"
	"github.com/orkestra/backend/internal/addons/billing/repository"
	"github.com/orkestra/backend/internal/addons/billing/services"
)

// Settings mirrors the billing ConfigSchema 1:1. Captured once at Init for
// the static reads (Start, RegisterRoutes) and re-resolved per invocation
// inside the configLoader closure for the dynamic-reload path used by the
// OpenAPI client + XML builder.
type Settings struct {
	AccountEmail    string        `module:"accountEmail"`
	APIKey          string        `module:"apiKey"`
	OAuthBaseURL    string        `module:"oauthBaseURL"`
	BearerToken     string        `module:"bearerToken"`
	BaseURL         string        `module:"baseURL"`
	FiscalID        string        `module:"fiscalID"`
	RecipientCode   string        `module:"recipientCode"`
	ApplySignature  bool          `module:"applySignature"`
	ApplyStorage    bool          `module:"applyStorage"`
	Timeout         time.Duration `module:"timeout"`
	RetryAttempts   int           `module:"retryAttempts"`
	SandboxMode     bool          `module:"sandboxMode"`
	PollingEnabled  bool          `module:"pollingEnabled"`
	PollingInterval time.Duration `module:"pollingInterval"`
	WebhookURL      string        `module:"webhookURL"`
	WebhookSecret   string        `module:"webhookSecret"`
}

type BillingModule struct {
	module.BaseModule
	invoiceHandler          *handlers.InvoiceHandler
	supplierHandler         *handlers.SupplierHandler
	companyHandler          *handlers.CompanyHandler
	notificationHandler     *handlers.NotificationHandler
	businessRegistryHandler *handlers.BusinessRegistryHandler
	syncHandler             *handlers.SyncHandler
	webhookHandler          *handlers.WebhookHandler

	pollingJob    *jobs.PollingJob
	openAPIClient services.OpenAPIClient
	settings      Settings
	logger        *slog.Logger
}

func NewModule() *BillingModule { return &BillingModule{} }

func (m *BillingModule) Name() string                    { return "billing" }
func (m *BillingModule) DisplayName() string             { return "Fatturazione Elettronica" }
func (m *BillingModule) Description() string             { return "Italian electronic invoicing via FatturaPA/SDI" }
func (m *BillingModule) Category() module.ModuleCategory { return module.CategoryExternal }

// Enabled gates first-boot activation on OpenAPI credentials being
// present. Reads the env vars directly (same source the schema seeder
// uses for these keys) so this method has no shared/config dependency.
func (m *BillingModule) Enabled() bool {
	if os.Getenv("OPENAPI_BILLING_BEARER_TOKEN") != "" {
		return true
	}
	return os.Getenv("OPENAPI_BILLING_ACCOUNT_EMAIL") != "" && os.Getenv("OPENAPI_BILLING_API_KEY") != ""
}
func (m *BillingModule) Dependencies() []string { return []string{"documents"} }
func (m *BillingModule) OptionalServices() []module.ServiceKey {
	// BillingTenantProvider drives the CessionarioCommittente snapshot at
	// invoice creation time (Unified Client Aggregate, Phase 5). PDFService
	// renders the invoice PDF when documents is enabled. Both are optional
	// so the module still boots when those producers are absent.
	return []module.ServiceKey{module.ServicePDFService, module.ServiceBillingTenantProvider}
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
	// billing_customers is no longer registered for auto-creation (Phase 5
	// of the Unified Client Aggregate refactor — Customer was deleted in
	// favour of Tenant.FatturaPA). The collection itself survives in Mongo
	// as a one-phase safety belt for forensics; a follow-up migration drops
	// it after invoice send is verified working in production.
	return []module.CollectionSpec{
		{Name: "billing_invoices", Indexes: []module.IndexSpec{{Keys: map[string]int{"uuid": 1}, Unique: true}}},
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
			{Name: "Suppliers", Icon: "truck", Path: "/billing/suppliers", Active: true},
			{Name: "SDI Notifications", Icon: "bell", Path: "/billing/notifications", Active: true},
			{Name: "Issuing Companies", Icon: "building", Path: "/billing/companies", Active: true},
		},
	}}
}

func (m *BillingModule) Init(deps *module.Dependencies) error {
	m.logger = deps.Logger

	if err := deps.ConfigService.UnmarshalModule(context.Background(), m.Name(), &m.settings); err != nil {
		return err
	}
	if m.settings.PollingInterval <= 0 {
		m.settings.PollingInterval = 12 * time.Hour
	}

	// configLoader returns the live OpenAPI config on every invocation so
	// admin UI edits (key rotation, sandbox toggle, signature/storage flags)
	// apply without a restart. Each call costs one DB+cache read — same as
	// the previous N-call closure, just consolidated. Unmarshal failure
	// degrades to empty config and logs a warning, matching the legacy
	// GetSecret behaviour where a decrypt failure returned "" silently.
	configLoader := func() *billingConfig.OpenAPIConfig {
		var s Settings
		if err := deps.ConfigService.UnmarshalModule(context.Background(), m.Name(), &s); err != nil {
			deps.Logger.Warn("billing: config reload failed", slog.String("error", err.Error()))
			return &billingConfig.OpenAPIConfig{}
		}
		return &billingConfig.OpenAPIConfig{
			BaseURL:        s.BaseURL,
			AccountEmail:   s.AccountEmail,
			APIKey:         s.APIKey,
			OAuthBaseURL:   s.OAuthBaseURL,
			BearerToken:    s.BearerToken,
			FiscalID:       s.FiscalID,
			RecipientCode:  s.RecipientCode,
			ApplySignature: s.ApplySignature,
			ApplyStorage:   s.ApplyStorage,
			Timeout:        s.Timeout,
			RetryAttempts:  s.RetryAttempts,
			SandboxMode:    s.SandboxMode,
		}
	}

	invoiceRepo := repository.NewInvoiceRepository(deps.DB)
	supplierRepo := repository.NewSupplierRepository(deps.DB)
	companyRepo := repository.NewCompanyRepository(deps.DB)
	notificationRepo := repository.NewNotificationRepository(deps.DB)

	m.openAPIClient = services.NewOpenAPIClientWithCache(configLoader, deps.Logger, deps.RedisAdapter)
	xmlBuilder := services.NewXMLBuilder(configLoader)

	// Retrieve PDFService from ServiceRegistry (can be nil if documents module is disabled)
	pdfSvc, _ := module.GetTyped[iface.PDFProvider](deps.Services, module.ServicePDFService)

	// Resolve the unified-client billing-party seam (Phase 5). Tenant is a
	// core module so the provider is always available in production; treat
	// it as optional so a misconfigured deployment still boots the rest of
	// the billing surface — the invoice service surfaces ErrTenantNotBillable
	// when an invoice carries a tenantUUID and the resolver is missing.
	billingTenants, _ := module.GetTyped[iface.BillingTenantProvider](deps.Services, module.ServiceBillingTenantProvider)

	invoiceSvc := services.NewInvoiceService(invoiceRepo, supplierRepo, companyRepo, billingTenants, m.openAPIClient, xmlBuilder, nil, pdfSvc, deps.Logger)
	supplierSvc := services.NewSupplierService(supplierRepo, deps.Logger)
	companySvc := services.NewCompanyService(companyRepo, m.openAPIClient, deps.Logger)
	notificationSvc := services.NewNotificationService(notificationRepo, deps.Logger)

	m.invoiceHandler = handlers.NewInvoiceHandler(invoiceSvc)
	m.supplierHandler = handlers.NewSupplierHandler(supplierSvc)
	m.companyHandler = handlers.NewCompanyHandler(companySvc)
	m.notificationHandler = handlers.NewNotificationHandler(notificationSvc)
	m.businessRegistryHandler = handlers.NewBusinessRegistryHandler(m.openAPIClient)

	m.pollingJob = jobs.NewPollingJob(
		m.openAPIClient,
		invoiceRepo,
		notificationRepo,
		deps.Logger,
		m.settings.PollingInterval,
	)

	m.syncHandler = handlers.NewSyncHandler(m.pollingJob)

	if m.settings.WebhookURL != "" {
		m.webhookHandler = handlers.NewWebhookHandler(
			m.pollingJob,
			m.settings.WebhookSecret,
			deps.Logger,
		)
	}

	deps.Logger.Info("Billing module initialized",
		slog.String("baseURL", m.settings.BaseURL),
		slog.Bool("sandbox", m.settings.SandboxMode),
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
		r.Use(modulegate.ModuleGate(ri.ConfigService, m.Name()))
		r.Use(ri.Operator.AuthMW.RequireInternalTenant())
		r.Use(ri.Operator.AuthMW.RequireCapability("billing.access"))
		r.Use(ri.Operator.AuthMW.RequirePermission("billing.invoice.read"))
		api := humachi.New(r, ri.APIConfig)
		RegisterRoutes(
			api,
			m.invoiceHandler,
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
			slog.String("webhookURL", m.settings.WebhookURL),
		)
	}
}

func (m *BillingModule) Start(_ context.Context) error {
	// Start SDI notification polling job
	if m.pollingJob != nil && m.settings.PollingEnabled {
		pollingCtx, pollingCancel := context.WithCancel(context.Background())
		_ = pollingCancel // Will be cancelled via Stop()

		go func() {
			m.logger.Info("Starting SDI notification polling job",
				slog.Duration("interval", m.settings.PollingInterval),
			)
			m.pollingJob.Start(pollingCtx)
		}()
	} else if !m.settings.PollingEnabled {
		m.logger.Info("SDI polling job disabled - use manual sync endpoints instead (POST /v1/billing/sync)")
	}

	// Auto-configure API callbacks on OpenAPI.it for webhook reception
	if m.settings.WebhookURL != "" && m.openAPIClient != nil {
		go func() {
			configCtx, configCancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer configCancel()

			authHeader := "Bearer " + m.settings.WebhookSecret
			err := m.openAPIClient.ConfigureAPICallbacks(configCtx, services.APICallbackConfig{
				FiscalID: m.settings.FiscalID,
				Callbacks: []services.CallbackConfig{
					{Event: "supplier-invoice", URL: m.settings.WebhookURL, AuthHeader: authHeader},
					{Event: "customer-notification", URL: m.settings.WebhookURL, AuthHeader: authHeader},
					{Event: "legal-storage-receipt", URL: m.settings.WebhookURL, AuthHeader: authHeader},
				},
			})
			if err != nil {
				m.logger.Error("Failed to configure API callbacks on OpenAPI.it",
					slog.String("error", err.Error()),
					slog.String("webhookURL", m.settings.WebhookURL),
				)
			} else {
				m.logger.Info("API callbacks configured on OpenAPI.it",
					slog.String("webhookURL", m.settings.WebhookURL),
					slog.String("fiscalID", m.settings.FiscalID),
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
