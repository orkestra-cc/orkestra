package documents

import (
	"context"
	"log/slog"
	"os"
	"time"

	"github.com/danielgtaylor/huma/v2/adapters/humachi"
	"github.com/go-chi/chi/v5"
	"github.com/orkestra/backend/internal/addons/documents/config"
	"github.com/orkestra/backend/internal/addons/documents/handlers"
	"github.com/orkestra/backend/internal/addons/documents/models"
	"github.com/orkestra/backend/internal/addons/documents/repository"
	"github.com/orkestra/backend/internal/addons/documents/services"
	"github.com/orkestra/backend/pkg/sdk/capability"
	"github.com/orkestra/backend/pkg/sdk/iface"
	"github.com/orkestra/backend/pkg/sdk/module"
	"github.com/orkestra/backend/pkg/sdk/modulegate"
)

// Settings mirrors the documents ConfigSchema 1:1 and is the typed surface
// Init() consumes module configuration through. Keep field order/names in
// sync with ConfigSchema() — UnmarshalModule maps via the `module:` tag.
type Settings struct {
	GotenbergURL  string        `module:"gotenbergURL"`
	Timeout       time.Duration `module:"timeout"`
	RetryAttempts int           `module:"retryAttempts"`
}

type DocumentsModule struct {
	module.BaseModule
	templateHandler *handlers.TemplateHandler
	documentHandler *handlers.DocumentHandler
	templateSvc     services.TemplateService
}

func NewModule() *DocumentsModule { return &DocumentsModule{} }

func (m *DocumentsModule) Name() string        { return "documents" }
func (m *DocumentsModule) DisplayName() string { return "Document Templates" }
func (m *DocumentsModule) Description() string {
	return "PDF generation with Gotenberg and customizable HTML templates"
}
func (m *DocumentsModule) Category() module.ModuleCategory { return module.CategoryExternal }

// Enabled gates first-boot activation on a Gotenberg URL being configured.
// Reads the env var directly to keep this method shared/config-free.
func (m *DocumentsModule) Enabled() bool {
	return os.Getenv("GOTENBERG_URL") != ""
}

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
		{Name: models.DocumentTemplatesCollection, Indexes: []module.IndexSpec{{Keys: map[string]int{"uuid": 1}, Unique: true}}},
		{Name: models.DocumentOutputsCollection, Indexes: []module.IndexSpec{{Keys: map[string]int{"uuid": 1}, Unique: true}}},
	}
}

func (m *DocumentsModule) NavItems() []module.NavItemSpec {
	return []module.NavItemSpec{
		{Realm: "shared", Section: "Tools", Name: "Document Templates", Icon: "file-alt", Path: "/documents/templates", MinRole: "manager", Active: true},
	}
}

func (m *DocumentsModule) Permissions() []iface.PermissionSpec {
	return []iface.PermissionSpec{
		{Key: "documents.template.read", Module: "documents", Description: "View document templates"},
		{Key: "documents.template.manage", Module: "documents", Description: "Create, update, and delete templates"},
		{Key: "documents.generate", Module: "documents", Description: "Generate PDFs from templates"},
	}
}

func (m *DocumentsModule) Capabilities() []capability.Capability {
	return []capability.Capability{
		{
			ID:          "documents.access",
			Module:      "documents",
			Action:      "access",
			Title:       "Documents",
			Description: "Generate PDFs from HTML/CSS templates (invoices, offers, receipts, custom).",
			Published:   true,
		},
	}
}

func (m *DocumentsModule) Init(deps *module.Dependencies) error {
	var settings Settings
	if err := deps.ConfigService.UnmarshalModule(context.Background(), m.Name(), &settings); err != nil {
		return err
	}
	if settings.Timeout <= 0 {
		settings.Timeout = 60 * time.Second
	}
	if settings.RetryAttempts <= 0 {
		settings.RetryAttempts = 3
	}

	docsConfig := &config.Config{
		GotenbergURL:  settings.GotenbergURL,
		Timeout:       settings.Timeout,
		RetryAttempts: settings.RetryAttempts,
		DefaultMargins: config.PDFMargins{
			Top: 20, Bottom: 20, Left: 20, Right: 20,
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
	ri.Operator.ProtectedRouter.Group(func(r chi.Router) {
		r.Use(modulegate.ModuleGate(ri.ConfigService, m.Name()))
		r.Use(ri.Operator.AuthMW.RequireCapability("documents.access"))
		r.Use(ri.Operator.AuthMW.RequirePermission("documents.template.read"))
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

func (m *DocumentsModule) Stop(_ context.Context) error        { return nil }
func (m *DocumentsModule) HealthCheck(_ context.Context) error { return nil }
