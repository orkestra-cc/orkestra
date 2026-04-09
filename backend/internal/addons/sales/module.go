package sales

import (
	"context"
	"log/slog"
	"time"

	"github.com/danielgtaylor/huma/v2/adapters/humachi"
	"github.com/go-chi/chi/v5"
	"github.com/orkestra/backend/internal/addons/sales/handlers"
	"github.com/orkestra/backend/internal/addons/sales/repository"
	"github.com/orkestra/backend/internal/addons/sales/services"
	"github.com/orkestra/backend/internal/shared/config"
	"github.com/orkestra/backend/internal/shared/iface"
	"github.com/orkestra/backend/internal/shared/middleware"
	"github.com/orkestra/backend/internal/shared/module"
)

type SalesModule struct {
	module.BaseModule
	skillHandler    *handlers.SkillHandler
	prospectHandler *handlers.ProspectHandler
	jobHandler      *handlers.JobHandler
	reportHandler   *handlers.ReportHandler
	promptHandler   *handlers.PromptHandler
	settingsHandler *handlers.SettingsHandler
	batchPoller     *services.BatchPoller
}

func NewModule() *SalesModule { return &SalesModule{} }

func (m *SalesModule) Name() string                   { return "sales" }
func (m *SalesModule) DisplayName() string             { return "Sales Intelligence" }
func (m *SalesModule) Description() string             { return "AI-driven prospect analysis, scoring, and outreach" }
func (m *SalesModule) Category() module.ModuleCategory { return module.CategoryToggleable }
func (m *SalesModule) Enabled(cfg *config.Config) bool { return cfg.Sales.Enabled }
func (m *SalesModule) OptionalServices() []module.ServiceKey {
	return []module.ServiceKey{module.ServiceAIModelProvider}
}

func (m *SalesModule) Collections() []module.CollectionSpec {
	return []module.CollectionSpec{
		{Name: "sales_jobs", Indexes: []module.IndexSpec{{Keys: map[string]int{"uuid": 1}, Unique: true}}},
		{Name: "sales_reports", Indexes: []module.IndexSpec{{Keys: map[string]int{"uuid": 1}, Unique: true}}},
		{Name: "sales_prompts"},
		{Name: "sales_settings"},
		{Name: "sales_batches"},
	}
}

func (m *SalesModule) NavItems() []module.NavItemSpec {
	return []module.NavItemSpec{{
		Group: "Sales Intelligence", Name: "Sales", Icon: "chart-line", Path: "/sales",
		MinRole: "manager", Active: true,
		Children: []module.NavItemSpec{
			{Name: "Prospect Analysis", Icon: "search", Path: "/sales/prospect", Active: true},
			{Name: "Jobs", Icon: "tasks", Path: "/sales/jobs", Active: true},
			{Name: "Reports", Icon: "file-alt", Path: "/sales/reports", Active: true},
			{Name: "Settings", Icon: "cog", Path: "/sales/settings", Active: true},
		},
	}}
}

func (m *SalesModule) Init(deps *module.Dependencies) error {
	cfg := deps.Config

	salesJobRepo := repository.NewJobRepository(deps.DB)

	// Use AI model provider if available
	var salesModelProvider services.AIModelProvider
	if mp, ok := module.GetTyped[iface.AIModelProvider](deps.Services, module.ServiceAIModelProvider); ok {
		salesModelProvider = mp
	}

	// Optional: company enrichment for Italian business registry
	var salesEnrichment services.CompanyEnrichmentService
	// TODO: wire companyService adapter when ready

	salesPromptRepo := repository.NewPromptRepository(deps.DB)
	promptLoader := services.NewPromptLoader(salesPromptRepo, deps.Logger)

	// Seed default prompts from embedded files on first run
	if err := promptLoader.SeedDefaults(context.Background()); err != nil {
		deps.Logger.Warn("Failed to seed sales prompts", slog.String("error", err.Error()))
	}
	scraper := services.NewScraper(cfg.Sales, deps.Logger)
	agentExecutor := services.NewAgentExecutor(cfg.Sales.MaxConcurrency, deps.Logger)
	scorer := services.NewScorer()

	salesSettingsRepo := repository.NewSettingsRepository(deps.DB)
	salesReportRepo := repository.NewReportRepository(deps.DB)
	reportGen := services.NewReportGenerator(salesReportRepo, deps.Logger)

	salesBatchRepo := repository.NewBatchRepository(deps.DB)

	orchestrator := services.NewOrchestrator(
		salesJobRepo, salesReportRepo, salesSettingsRepo, salesBatchRepo, salesModelProvider, promptLoader,
		scraper, agentExecutor, scorer, salesEnrichment, reportGen,
		cfg.Sales, deps.Logger,
	)

	// Create batch poller for async LLM batch results
	m.batchPoller = services.NewBatchPoller(
		salesBatchRepo, salesJobRepo, salesReportRepo,
		salesModelProvider, scorer, reportGen,
		30*time.Second, deps.Logger,
	)

	skillStore := services.NewSkillStore()
	m.skillHandler = handlers.NewSkillHandler(orchestrator, skillStore, deps.Logger)
	m.prospectHandler = handlers.NewProspectHandler(orchestrator)
	m.jobHandler = handlers.NewJobHandler(orchestrator)
	m.reportHandler = handlers.NewReportHandler(salesReportRepo, salesJobRepo, reportGen)
	m.promptHandler = handlers.NewPromptHandler(salesPromptRepo, promptLoader)
	m.settingsHandler = handlers.NewSettingsHandler(salesSettingsRepo)

	// Mark incomplete jobs from previous runs as failed
	if err := salesJobRepo.MarkStaleJobsFailed(context.Background()); err != nil {
		deps.Logger.Warn("Failed to mark stale sales jobs", slog.String("error", err.Error()))
	}

	deps.Logger.Info("Sales Intelligence module initialized")
	return nil
}

func (m *SalesModule) RegisterRoutes(ri *module.RouteInfo) {
	ri.ProtectedRouter.Group(func(r chi.Router) {
		r.Use(middleware.ModuleGate(ri.ConfigService, m.Name()))
		r.Use(ri.AuthMW.RequireHierarchicalRole("manager"))
		api := humachi.New(r, ri.APIConfig)
		RegisterSkillRoutes(api, m.skillHandler)
		RegisterProspectRoutes(api, m.prospectHandler)
		RegisterJobRoutes(api, m.jobHandler)
		RegisterReportRoutes(api, m.reportHandler)
		RegisterPromptRoutes(api, m.promptHandler)
		RegisterReportDownloadRoute(r, m.reportHandler)
		RegisterSettingsRoutes(api, m.settingsHandler)
	})
}

func (m *SalesModule) Start(_ context.Context) error {
	if m.batchPoller != nil {
		m.batchPoller.Start()
	}
	return nil
}

func (m *SalesModule) Stop(_ context.Context) error {
	// BatchPoller doesn't have an explicit Stop — it runs until context cancellation
	return nil
}

func (m *SalesModule) HealthCheck(_ context.Context) error { return nil }
