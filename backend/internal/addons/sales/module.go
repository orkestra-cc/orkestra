package sales

import (
	"context"
	"log/slog"
	"os"
	"strconv"
	"time"

	"github.com/danielgtaylor/huma/v2/adapters/humachi"
	"github.com/go-chi/chi/v5"
	"github.com/orkestra-cc/orkestra-sdk/capability"
	"github.com/orkestra-cc/orkestra-sdk/iface"
	"github.com/orkestra-cc/orkestra-sdk/module"
	"github.com/orkestra-cc/orkestra-sdk/modulegate"
	"github.com/orkestra/backend/internal/addons/sales/handlers"
	"github.com/orkestra/backend/internal/addons/sales/repository"
	"github.com/orkestra/backend/internal/addons/sales/services"
	"github.com/orkestra/backend/internal/shared/config"
)

// Settings mirrors the sales ConfigSchema 1:1. Init() unmarshals into a
// value of this type, then materializes a config.SalesConfig (from
// shared/config) so existing service constructors (NewScraper,
// NewOrchestrator, NewAgentExecutor) keep their signatures unchanged. The
// Enabled flag on SalesConfig is set true here — by the time Init runs, the
// registry has already gated activation on the module_configs document.
type Settings struct {
	MaxConcurrency  int           `module:"maxConcurrency"`
	DefaultLocale   string        `module:"defaultLocale"`
	SkillTimeout    time.Duration `module:"skillTimeout"`
	QuickTimeout    time.Duration `module:"quickTimeout"`
	FullTimeout     time.Duration `module:"fullTimeout"`
	ScraperTimeout  time.Duration `module:"scraperTimeout"`
	ScraperMaxDepth int           `module:"scraperMaxDepth"`
	MaxTokens       int           `module:"maxTokens"`
}

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

func (m *SalesModule) Name() string        { return "sales" }
func (m *SalesModule) DisplayName() string { return "Sales Intelligence" }
func (m *SalesModule) Description() string {
	return "AI-driven prospect analysis, scoring, and outreach"
}
func (m *SalesModule) Category() module.ModuleCategory { return module.CategoryToggleable }

// Enabled gates first-boot activation on SALES_ENABLED.
func (m *SalesModule) Enabled() bool {
	v, _ := strconv.ParseBool(os.Getenv("SALES_ENABLED"))
	return v
}

// Dependencies orders aimodels before sales so its AIModelProvider is
// registered by the time sales's Init runs. The provider is still read
// via GetTyped with graceful degradation, so sales can run without it.
func (m *SalesModule) Dependencies() []string { return []string{"aimodels"} }

func (m *SalesModule) OptionalServices() []module.ServiceKey {
	return []module.ServiceKey{module.ServiceAIModelProvider}
}

// ConfigSchema declares the per-module runtime tunables. Fields previously
// read from cfg.Sales (shared/config) move here so they can be adjusted
// through /admin/modules without a restart. Defaults match the env-var
// defaults in shared/config so existing deployments keep their behavior.
func (m *SalesModule) ConfigSchema() []module.ConfigField {
	return []module.ConfigField{
		{Key: "maxConcurrency", Label: "Max Parallel Agent Calls", Type: module.FieldInt, Default: "5", EnvVar: "SALES_MAX_CONCURRENCY"},
		{Key: "defaultLocale", Label: "Default Locale", Type: module.FieldString, Default: "it", EnvVar: "SALES_DEFAULT_LOCALE"},
		{Key: "skillTimeout", Label: "Skill Timeout", Type: module.FieldDuration, Default: "5m", EnvVar: "SALES_SKILL_TIMEOUT"},
		{Key: "quickTimeout", Label: "Quick Prospect Timeout", Type: module.FieldDuration, Default: "5m", EnvVar: "SALES_QUICK_TIMEOUT"},
		{Key: "fullTimeout", Label: "Full Prospect Pipeline Timeout", Type: module.FieldDuration, Default: "15m", EnvVar: "SALES_FULL_TIMEOUT"},
		{Key: "scraperTimeout", Label: "Scraper Request Timeout", Type: module.FieldDuration, Default: "30s", EnvVar: "SALES_SCRAPER_TIMEOUT"},
		{Key: "scraperMaxDepth", Label: "Scraper Max Subpage Depth", Type: module.FieldInt, Default: "3", EnvVar: "SALES_SCRAPER_MAX_DEPTH"},
		{Key: "maxTokens", Label: "Max LLM Output Tokens", Type: module.FieldInt, Default: "8192", EnvVar: "SALES_MAX_TOKENS"},
	}
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
		Realm: "shared", Tier: "internal",
		Name: "Sales", Icon: "chart-line", Path: "/sales",
		Active: true,
		Children: []module.NavItemSpec{
			{Name: "Prospect Analysis", Icon: "search", Path: "/sales/prospect", Active: true},
			{Name: "Jobs", Icon: "tasks", Path: "/sales/jobs", Active: true},
			{Name: "Reports", Icon: "file-alt", Path: "/sales/reports", Active: true},
			{Name: "Settings", Icon: "cog", Path: "/sales/settings", Active: true},
		},
	}}
}

func (m *SalesModule) Permissions() []iface.PermissionSpec {
	return []iface.PermissionSpec{
		{Key: "sales.job.read", Module: "sales", Description: "View sales jobs and reports"},
		{Key: "sales.job.run", Module: "sales", Description: "Run prospect analysis jobs"},
		{Key: "sales.admin", Module: "sales", Description: "Manage prompts and settings"},
	}
}

func (m *SalesModule) Capabilities() []capability.Capability {
	return []capability.Capability{
		{
			ID:          "sales.access",
			Module:      "sales",
			Action:      "access",
			Title:       "Sales Intelligence",
			Description: "Run AI-driven prospect analysis, scoring, and report generation.",
			Published:   true,
		},
	}
}

func (m *SalesModule) Init(deps *module.Dependencies) error {
	var settings Settings
	if err := deps.ConfigService.UnmarshalModule(context.Background(), m.Name(), &settings); err != nil {
		return err
	}
	if settings.MaxConcurrency <= 0 {
		settings.MaxConcurrency = 5
	}
	if settings.SkillTimeout <= 0 {
		settings.SkillTimeout = 5 * time.Minute
	}
	if settings.QuickTimeout <= 0 {
		settings.QuickTimeout = 5 * time.Minute
	}
	if settings.FullTimeout <= 0 {
		settings.FullTimeout = 15 * time.Minute
	}
	if settings.ScraperTimeout <= 0 {
		settings.ScraperTimeout = 30 * time.Second
	}
	if settings.ScraperMaxDepth <= 0 {
		settings.ScraperMaxDepth = 3
	}
	if settings.MaxTokens <= 0 {
		settings.MaxTokens = 8192
	}

	// Build the legacy SalesConfig (shared/config type) from Settings so
	// existing service constructors keep their signatures. Enabled is true
	// by construction — Init only runs when the module is enabled.
	salesCfg := config.SalesConfig{
		Enabled:         true,
		MaxConcurrency:  settings.MaxConcurrency,
		DefaultLocale:   settings.DefaultLocale,
		SkillTimeout:    settings.SkillTimeout,
		QuickTimeout:    settings.QuickTimeout,
		FullTimeout:     settings.FullTimeout,
		ScraperTimeout:  settings.ScraperTimeout,
		ScraperMaxDepth: settings.ScraperMaxDepth,
		MaxTokens:       settings.MaxTokens,
	}

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
	scraper := services.NewScraper(salesCfg, deps.Logger)
	agentExecutor := services.NewAgentExecutor(salesCfg.MaxConcurrency, salesCfg.MaxTokens, deps.Logger)
	scorer := services.NewScorer()

	salesSettingsRepo := repository.NewSettingsRepository(deps.DB)
	salesReportRepo := repository.NewReportRepository(deps.DB)
	reportGen := services.NewReportGenerator(salesReportRepo, deps.Logger)

	salesBatchRepo := repository.NewBatchRepository(deps.DB)

	orchestrator := services.NewOrchestrator(
		salesJobRepo, salesReportRepo, salesSettingsRepo, salesBatchRepo, salesModelProvider, promptLoader,
		scraper, agentExecutor, scorer, salesEnrichment, reportGen,
		salesCfg, deps.Logger,
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
	ri.Operator.ProtectedRouter.Group(func(r chi.Router) {
		r.Use(modulegate.ModuleGate(ri.ConfigService, m.Name()))
		r.Use(ri.Operator.AuthMW.RequireCapability("sales.access"))
		r.Use(ri.Operator.AuthMW.RequirePermission("sales.job.read"))
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
