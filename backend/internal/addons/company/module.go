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
func (m *CompanyModule) Enabled(cfg *sharedConfig.Config) bool { return cfg.Company.BearerToken != "" }

func (m *CompanyModule) ConfigSchema() []module.ConfigField {
	return []module.ConfigField{
		{Key: "bearerToken", Label: "Bearer Token", Type: module.FieldSecret, Required: true, EnvVar: "OPENAPI_COMPANY_BEARER_TOKEN"},
		{Key: "baseURL", Label: "API Base URL", Type: module.FieldString, Default: "https://company.openapi.com", EnvVar: "OPENAPI_COMPANY_BASE_URL"},
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
		{Group: "Administration", Name: "Aziende", Icon: "building", Path: "/company",
			MinRole: "manager", Active: true,
			Children: []module.NavItemSpec{
				{Name: "Ricerca CF/P.IVA", Icon: "search", Path: "/company/lookup", Active: true},
				{Name: "Ricerca Avanzata", Icon: "search-plus", Path: "/company/search", Active: true},
			},
		},
	}
}

func (m *CompanyModule) Init(deps *module.Dependencies) error {
	companyCfg := &config.CompanyAPIConfig{
		BaseURL:       deps.GetConfig("company", "baseURL"),
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
	)
	return nil
}

func (m *CompanyModule) RegisterRoutes(ri *module.RouteInfo) {
	// Company routes: manager role and above
	ri.ProtectedRouter.Group(func(r chi.Router) {
		r.Use(middleware.ModuleGate(ri.ConfigService, m.Name()))
		r.Use(ri.AuthMW.RequireHierarchicalRole("manager"))
		api := humachi.New(r, ri.APIConfig)
		RegisterRoutes(api, m.handler)
	})
}

func (m *CompanyModule) Start(_ context.Context) error      { return nil }
func (m *CompanyModule) Stop(_ context.Context) error       { return nil }
func (m *CompanyModule) HealthCheck(_ context.Context) error { return nil }
