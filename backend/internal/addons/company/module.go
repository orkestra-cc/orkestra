package company

import (
	"context"
	"log/slog"
	"time"

	"github.com/danielgtaylor/huma/v2/adapters/humachi"
	"github.com/go-chi/chi/v5"
	"github.com/orkestra/backend/internal/addons/company/config"
	"github.com/orkestra/backend/internal/addons/company/handlers"
	"github.com/orkestra/backend/internal/addons/company/repository"
	"github.com/orkestra/backend/internal/addons/company/services"
	sharedConfig "github.com/orkestra/backend/internal/shared/config"
	"github.com/orkestra/backend/internal/shared/iface"
	"github.com/orkestra/backend/internal/shared/middleware"
	"github.com/orkestra/backend/internal/shared/module"
)

type CompanyModule struct {
	module.BaseModule
	handler *handlers.CompanyHandler
}

func NewModule() *CompanyModule { return &CompanyModule{} }

func (m *CompanyModule) Name() string                       { return "company" }
func (m *CompanyModule) DisplayName() string                 { return "Company Lookup" }
func (m *CompanyModule) Description() string                 { return "Italian company data lookup by CF/P.IVA via OpenAPI" }
func (m *CompanyModule) Category() module.ModuleCategory     { return module.CategoryExternal }
func (m *CompanyModule) Enabled(cfg *sharedConfig.Config) bool {
	if cfg.Company.BearerToken != "" {
		return true
	}
	return cfg.Company.AccountEmail != "" && cfg.Company.APIKey != ""
}

func (m *CompanyModule) ConfigSchema() []module.ConfigField {
	return []module.ConfigField{
		{Key: "accountEmail", Label: "Account Email", Type: module.FieldString,
			Description: "Email of your OpenAPI.com account. Combined with API Key to mint short-lived JWT bearer tokens automatically.",
			EnvVar:      "OPENAPI_COMPANY_ACCOUNT_EMAIL"},
		{Key: "apiKey", Label: "API Key", Type: module.FieldSecret,
			Description: "Long-lived API key from console.openapi.com. The module exchanges it for a JWT at the OAuth host.",
			EnvVar:      "OPENAPI_COMPANY_API_KEY"},
		{Key: "oauthBaseURL", Label: "OAuth Base URL", Type: module.FieldString,
			Description: "OpenAPI.com OAuth host. Production: https://oauth.openapi.it · Sandbox: https://test.oauth.openapi.it",
			Default:     "https://oauth.openapi.it",
			EnvVar:      "OPENAPI_OAUTH_BASE_URL"},
		{Key: "bearerToken", Label: "Bearer Token (legacy fallback)", Type: module.FieldSecret,
			Description: "Optional. Static JWT used only when API Key is empty. Leave blank if you've set the API Key above.",
			EnvVar:      "OPENAPI_COMPANY_BEARER_TOKEN"},
		{Key: "baseURL", Label: "Company API Base URL", Type: module.FieldString, Default: "https://company.openapi.com", EnvVar: "OPENAPI_COMPANY_BASE_URL"},
		{Key: "timeout", Label: "Request Timeout", Type: module.FieldDuration, Default: "15s", EnvVar: "OPENAPI_COMPANY_TIMEOUT"},
		{Key: "cacheTTL", Label: "Cache TTL", Type: module.FieldDuration, Default: "24h", EnvVar: "OPENAPI_COMPANY_CACHE_TTL"},
	}
}

func (m *CompanyModule) Collections() []module.CollectionSpec {
	return []module.CollectionSpec{
		{Name: "company_lookups", Indexes: []module.IndexSpec{
			{Keys: map[string]int{"uuid": 1}, Unique: true},
			{Keys: map[string]int{"taxCode": 1}, Unique: true},
		}},
	}
}

func (m *CompanyModule) NavItems() []module.NavItemSpec {
	return []module.NavItemSpec{
		{Realm: "shared", Section: "Tools", Name: "Company Registry", Icon: "building", Path: "/company",
			MinRole: "manager", Active: true,
			Children: []module.NavItemSpec{
				{Name: "Tax ID Lookup", Icon: "search", Path: "/company/lookup", MinRole: "manager", Active: true},
				{Name: "Advanced Search", Icon: "search-plus", Path: "/company/search", MinRole: "manager", Active: true},
			},
		},
	}
}

func (m *CompanyModule) Permissions() []iface.PermissionSpec {
	return []iface.PermissionSpec{
		{Key: "company.lookup.read", Module: "company", Description: "Search and view company lookups"},
		{Key: "company.lookup.enrich", Module: "company", Description: "Fetch enrichment data for a company"},
	}
}

func (m *CompanyModule) Init(deps *module.Dependencies) error {
	companyCfg := &config.CompanyAPIConfig{
		BaseURL:       deps.GetConfig("company", "baseURL"),
		AccountEmail:  deps.GetConfig("company", "accountEmail"),
		APIKey:        deps.GetSecret("company", "apiKey"),
		OAuthBaseURL:  deps.GetConfig("company", "oauthBaseURL"),
		BearerToken:   deps.GetSecret("company", "bearerToken"),
		Timeout:       deps.GetConfigDuration("company", "timeout", 15*time.Second),
		RetryAttempts: deps.GetConfigInt("company", "retryAttempts", 3),
		CacheTTL:      deps.GetConfigDuration("company", "cacheTTL", 24*time.Hour),
	}

	repo := repository.NewCompanyRepository(deps.DB)
	client := services.NewCompanyAPIClientWithCache(companyCfg, deps.Logger, deps.RedisAdapter)
	svc := services.NewCompanyService(repo, client, deps.Logger)
	m.handler = handlers.NewCompanyHandler(svc)

	deps.Logger.Info("Company lookup module initialized",
		slog.String("baseURL", companyCfg.BaseURL),
		slog.Bool("oauthMintingEnabled", companyCfg.HasOAuthCredentials()),
	)
	return nil
}

func (m *CompanyModule) RegisterRoutes(ri *module.RouteInfo) {
	// Company is a global lookup cache shared across tenants (public
	// business registry data), so routes require the permission but not
	// a plan entitlement.
	ri.Operator.ProtectedRouter.Group(func(r chi.Router) {
		r.Use(middleware.ModuleGate(ri.ConfigService, m.Name()))
		r.Use(ri.Operator.AuthMW.RequirePermission("company.lookup.read"))
		api := humachi.New(r, ri.APIConfig)
		RegisterRoutes(api, m.handler)
	})
}

func (m *CompanyModule) Start(_ context.Context) error      { return nil }
func (m *CompanyModule) Stop(_ context.Context) error       { return nil }
func (m *CompanyModule) HealthCheck(_ context.Context) error { return nil }
