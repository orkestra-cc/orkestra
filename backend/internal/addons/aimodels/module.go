package aimodels

import (
	"context"
	"log/slog"
	"os"
	"strconv"

	"github.com/danielgtaylor/huma/v2/adapters/humachi"
	"github.com/go-chi/chi/v5"
	"github.com/orkestra-cc/orkestra-sdk/capability"
	"github.com/orkestra-cc/orkestra-sdk/iface"
	"github.com/orkestra-cc/orkestra-sdk/module"
	"github.com/orkestra-cc/orkestra-sdk/modulegate"
	"github.com/orkestra/backend/internal/addons/aimodels/handlers"
	"github.com/orkestra/backend/internal/addons/aimodels/repository"
	"github.com/orkestra/backend/internal/addons/aimodels/services"
)

// Settings mirrors the aimodels ConfigSchema 1:1. Unlike other addons, this
// module re-reads config on every model-service invocation (see configLoader
// closure in Init) so admin UI changes take effect without restart — Settings
// is therefore used both for the boot-time unmarshal and inside the closure.
type Settings struct {
	OllamaBaseURL string `module:"ollamaBaseURL"`
	OpenAIKey     string `module:"openaiKey"`
	AnthropicKey  string `module:"anthropicKey"`
	GeminiKey     string `module:"geminiKey"`
}

type AIModelsModule struct {
	module.BaseModule
	handler         *handlers.ModelHandler
	internalHandler *handlers.InternalHandler
	service         services.AIModelService
}

func NewModule() *AIModelsModule { return &AIModelsModule{} }

func (m *AIModelsModule) Name() string        { return "aimodels" }
func (m *AIModelsModule) DisplayName() string { return "AI Models" }
func (m *AIModelsModule) Description() string {
	return "LLM and embedding model management (Ollama, OpenAI, Anthropic, Gemini)"
}
func (m *AIModelsModule) Category() module.ModuleCategory { return module.CategoryToggleable }

// Enabled gates first-boot activation on AIMODELS_ENABLED.
func (m *AIModelsModule) Enabled() bool {
	v, _ := strconv.ParseBool(os.Getenv("AIMODELS_ENABLED"))
	return v
}

func (m *AIModelsModule) ProvidedServices() []module.ServiceKey {
	return []module.ServiceKey{module.ServiceAIModelProvider}
}

func (m *AIModelsModule) ConfigSchema() []module.ConfigField {
	return []module.ConfigField{
		{Key: "ollamaBaseURL", Label: "Ollama Base URL", Type: module.FieldString, Default: "http://host.docker.internal:11434", EnvVar: "OLLAMA_BASE_URL"},
		{Key: "openaiKey", Label: "OpenAI API Key", Type: module.FieldSecret, EnvVar: "OPENAI_API_KEY"},
		{Key: "anthropicKey", Label: "Anthropic API Key", Type: module.FieldSecret, EnvVar: "ANTHROPIC_API_KEY"},
		{Key: "geminiKey", Label: "Gemini API Key", Type: module.FieldSecret, EnvVar: "GEMINI_API_KEY"},
	}
}

func (m *AIModelsModule) Collections() []module.CollectionSpec {
	return []module.CollectionSpec{
		{Name: "ai_models", Indexes: []module.IndexSpec{
			{Keys: map[string]int{"uuid": 1}, Unique: true},
			{Keys: map[string]int{"isDefault": 1, "type": 1}},
		}},
	}
}

func (m *AIModelsModule) NavItems() []module.NavItemSpec {
	return nil
}

func (m *AIModelsModule) Permissions() []iface.PermissionSpec {
	return []iface.PermissionSpec{
		{Key: "aimodels.read", Module: "aimodels", Description: "List AI model configurations"},
		{Key: "aimodels.admin", Module: "aimodels", Description: "Create, update, test, and delete AI models"},
	}
}

func (m *AIModelsModule) Capabilities() []capability.Capability {
	return []capability.Capability{
		{
			ID:          "aimodels.access",
			Module:      "aimodels",
			Action:      "access",
			Title:       "AI Models",
			Description: "Configure and exercise LLM + embedding model providers (Ollama, OpenAI, Anthropic, Gemini).",
			Published:   true,
		},
	}
}

func (m *AIModelsModule) Init(deps *module.Dependencies) error {
	repo := repository.NewModelRepository(deps.DB)

	// configLoader returns the live config on every invocation so admin UI
	// changes apply without a restart. Each call costs one DB+cache read.
	configLoader := func() services.AIModelsConfig {
		var s Settings
		if err := deps.ConfigService.UnmarshalModule(context.Background(), m.Name(), &s); err != nil {
			deps.Logger.Warn("aimodels: config reload failed", slog.String("error", err.Error()))
			return services.AIModelsConfig{}
		}
		return services.AIModelsConfig{
			OllamaBaseURL: s.OllamaBaseURL,
			OpenAIAPIKey:  s.OpenAIKey,
			AnthropicKey:  s.AnthropicKey,
			GeminiKey:     s.GeminiKey,
		}
	}

	m.service = services.NewModelService(repo, configLoader, deps.Logger)

	m.handler = handlers.NewModelHandler(m.service)
	m.internalHandler = handlers.NewInternalHandler(m.service)

	// Register AIModelService for RAG and Sales module consumption
	deps.Services.Register(module.ServiceAIModelProvider, m.service)

	deps.Logger.Info("AI Models module initialized")
	return nil
}

func (m *AIModelsModule) RegisterRoutes(ri *module.RouteInfo) {
	ri.Operator.ProtectedRouter.Group(func(r chi.Router) {
		r.Use(modulegate.ModuleGate(ri.ConfigService, m.Name()))
		r.Use(ri.Operator.AuthMW.RequireCapability("aimodels.access"))
		r.Use(ri.Operator.AuthMW.RequirePermission("aimodels.admin"))
		api := humachi.New(r, ri.APIConfig)
		RegisterRoutes(api, m.handler)
		RegisterInternalRoutes(api, m.internalHandler)
	})
}

func (m *AIModelsModule) Start(ctx context.Context) error {
	// Seed default models on startup
	if m.service != nil {
		if err := m.service.SeedDefaults(ctx); err != nil {
			slog.Warn("Failed to seed default AI models", slog.String("error", err.Error()))
		}
	}
	return nil
}

func (m *AIModelsModule) Stop(_ context.Context) error        { return nil }
func (m *AIModelsModule) HealthCheck(_ context.Context) error { return nil }
