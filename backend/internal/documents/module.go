package documents

import (
	"context"
	"log/slog"
	"time"

	"github.com/danielgtaylor/huma/v2/adapters/humachi"
	"github.com/go-chi/chi/v5"
	"github.com/orkestra/backend/internal/documents/config"
	"github.com/orkestra/backend/internal/documents/handlers"
	"github.com/orkestra/backend/internal/documents/repository"
	"github.com/orkestra/backend/internal/documents/services"
	sharedConfig "github.com/orkestra/backend/internal/shared/config"
	"github.com/orkestra/backend/internal/shared/middleware"
	"github.com/orkestra/backend/internal/shared/module"
)

type DocumentsModule struct {
	module.BaseModule
	templateHandler *handlers.TemplateHandler
	documentHandler *handlers.DocumentHandler
	templateSvc     services.TemplateService
}

func NewModule() *DocumentsModule { return &DocumentsModule{} }

func (m *DocumentsModule) Name() string                   { return "documents" }
func (m *DocumentsModule) DisplayName() string             { return "Document Templates" }
func (m *DocumentsModule) Description() string             { return "PDF generation with Gotenberg and customizable HTML templates" }
func (m *DocumentsModule) Category() module.ModuleCategory { return module.CategoryExternal }
func (m *DocumentsModule) Enabled(cfg *sharedConfig.Config) bool { return cfg.Documents.GotenbergURL != "" }

func (m *DocumentsModule) ProvidedServices() []module.ServiceKey {
	return []module.ServiceKey{module.ServicePDFService}
}

func (m *DocumentsModule) ConfigSchema() []module.ConfigField {
	return []module.ConfigField{
		{Key: "gotenbergURL", Label: "Gotenberg URL", Type: module.FieldString, Required: true, Default: "http://gotenberg:3000", EnvVar: "GOTENBERG_URL"},
		{Key: "timeout", Label: "Request Timeout", Type: module.FieldDuration, Default: "60s", EnvVar: "GOTENBERG_TIMEOUT"},
		{Key: "retryAttempts", Label: "Retry Attempts", Type: module.FieldInt, Default: "3", EnvVar: "GOTENBERG_RETRY_ATTEMPTS"},
	}
}

func (m *DocumentsModule) Collections() []module.CollectionSpec {
	return []module.CollectionSpec{
		{Name: "document_templates", Indexes: []module.IndexSpec{{Keys: map[string]int{"uuid": 1}, Unique: true}}},
		{Name: "generated_documents", Indexes: []module.IndexSpec{{Keys: map[string]int{"uuid": 1}, Unique: true}}},
	}
}

func (m *DocumentsModule) NavItems() []module.NavItemSpec {
	return []module.NavItemSpec{
		{Group: "Administration", Name: "Template Documenti", Icon: "file-alt", Path: "/admin/templates", MinRole: "manager", Active: true},
	}
}

func (m *DocumentsModule) Init(deps *module.Dependencies) error {
	docsConfig := &config.Config{
		GotenbergURL:  deps.GetConfig("documents", "gotenbergURL"),
		Timeout:       deps.GetConfigDuration("documents", "timeout", 60*time.Second),
		RetryAttempts: deps.GetConfigInt("documents", "retryAttempts", 3),
		DefaultMargins: config.PDFMargins{
			Top:    20, Bottom: 20, Left: 20, Right: 20,
		},
	}

	templateRepo := repository.NewTemplateRepository(deps.DB)
	documentRepo := repository.NewDocumentRepository(deps.DB)

	gotenbergClient := services.NewGotenbergClient(docsConfig, deps.Logger)
	templateEngine := services.NewTemplateEngine()
	m.templateSvc = services.NewTemplateService(templateRepo, templateEngine, deps.Logger)
	pdfSvc := services.NewPDFService(templateRepo, documentRepo, gotenbergClient, templateEngine, deps.Logger)

	m.templateHandler = handlers.NewTemplateHandler(m.templateSvc)
	m.documentHandler = handlers.NewDocumentHandler(pdfSvc)

	// Register PDFService for billing module consumption
	deps.Services.Register(module.ServicePDFService, pdfSvc)

	deps.Logger.Info("Documents module initialized",
		slog.String("gotenbergURL", docsConfig.GotenbergURL),
	)
	return nil
}

func (m *DocumentsModule) RegisterRoutes(ri *module.RouteInfo) {
	ri.ProtectedRouter.Group(func(r chi.Router) {
		r.Use(middleware.ModuleGate(ri.ConfigService, m.Name()))
		r.Use(ri.AuthMW.RequireHierarchicalRole("manager"))
		api := humachi.New(r, ri.APIConfig)
		RegisterRoutes(api, m.templateHandler, m.documentHandler)
	})
}

func (m *DocumentsModule) Start(ctx context.Context) error {
	// Seed built-in templates on startup
	if m.templateSvc != nil {
		if err := m.templateSvc.SeedBuiltInTemplates(ctx); err != nil {
			// Non-fatal: log warning and continue
			slog.Warn("Failed to seed built-in templates", slog.String("error", err.Error()))
		}
	}
	return nil
}

func (m *DocumentsModule) Stop(_ context.Context) error       { return nil }
func (m *DocumentsModule) HealthCheck(_ context.Context) error { return nil }
