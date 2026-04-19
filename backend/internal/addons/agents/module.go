package agents

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/danielgtaylor/huma/v2/adapters/humachi"
	"github.com/go-chi/chi/v5"
	"github.com/orkestra/backend/internal/addons/agents/handlers"
	"github.com/orkestra/backend/internal/addons/agents/repository"
	"github.com/orkestra/backend/internal/addons/agents/services"
	"github.com/orkestra/backend/internal/shared/capability"
	"github.com/orkestra/backend/internal/shared/config"
	"github.com/orkestra/backend/internal/shared/iface"
	"github.com/orkestra/backend/internal/shared/middleware"
	"github.com/orkestra/backend/internal/shared/module"
)

// Default image reference for the Hindsight container. Overridable via the
// "hindsightImage" module config field.
const defaultHindsightImage = "ghcr.io/vectorize-io/hindsight:latest"

type AgentsModule struct {
	module.BaseModule
	projectHandler       *handlers.ProjectHandler
	agentHandler         *handlers.AgentHandler
	personalAgentHandler *handlers.PersonalAgentHandler

	// Retained so InfraContainers() and Preflight() can read live config
	// on every toggle — admins edit aimodels' default LLM separately and
	// we want to pick up the latest value on the next enable.
	deps *module.Dependencies
}

func NewModule() *AgentsModule { return &AgentsModule{} }

func (m *AgentsModule) Name() string                   { return "agents" }
func (m *AgentsModule) DisplayName() string             { return "AI Agents" }
func (m *AgentsModule) Description() string             { return "Hindsight-powered AI agents with RAG context" }
func (m *AgentsModule) Category() module.ModuleCategory { return module.CategoryToggleable }
func (m *AgentsModule) Enabled(cfg *config.Config) bool { return cfg.Agents.Enabled }

// Hard dependency on aimodels: the Hindsight container inherits its LLM
// provider/model/API key from aimodels' default LLM, so aimodels must be
// initialized first and cannot be disabled while agents is running.
func (m *AgentsModule) Dependencies() []string { return []string{"auth", "aimodels"} }

func (m *AgentsModule) RequiredServices() []module.ServiceKey {
	return []module.ServiceKey{module.ServiceAIModelProvider}
}

func (m *AgentsModule) OptionalServices() []module.ServiceKey {
	return []module.ServiceKey{module.ServiceRAGQuery}
}

func (m *AgentsModule) Permissions() []iface.PermissionSpec {
	return []iface.PermissionSpec{
		{Key: "agents.personal", Module: "agents", Description: "Use your personal AI agent"},
		{Key: "agents.project.read", Module: "agents", Description: "View agent projects and conversations"},
		{Key: "agents.project.manage", Module: "agents", Description: "Create and manage agent projects"},
		{Key: "agents.query", Module: "agents", Description: "Run queries against agents"},
		{Key: "agents.admin", Module: "agents", Description: "Administer Hindsight banks and health"},
	}
}

func (m *AgentsModule) Capabilities() []capability.Capability {
	return []capability.Capability{
		{
			ID:          "agents.access",
			Module:      "agents",
			Action:      "access",
			Title:       "AI Agents",
			Description: "Run Hindsight-powered AI agents with per-project memory and RAG context.",
			Published:   true,
		},
	}
}

func (m *AgentsModule) ConfigSchema() []module.ConfigField {
	return []module.ConfigField{
		{Key: "hindsightURL", Group: "Connection", Label: "Hindsight URL", Type: module.FieldString, Default: "http://orkestra-hindsight:8888", EnvVar: "HINDSIGHT_URL"},
		{Key: "hindsightNamespace", Group: "Connection", Label: "Hindsight Namespace", Type: module.FieldString, Default: "orkestra", EnvVar: "HINDSIGHT_NAMESPACE"},
		{Key: "hindsightImage", Group: "Container", Label: "Hindsight Image", Type: module.FieldString, Default: defaultHindsightImage, EnvVar: "HINDSIGHT_IMAGE",
			Description: "Docker image used when the backend manages the Hindsight container's lifecycle. LLM credentials come from the AI Models module's default LLM."},
	}
}

func (m *AgentsModule) Collections() []module.CollectionSpec {
	return []module.CollectionSpec{
		{Name: "agent_projects", Indexes: []module.IndexSpec{{Keys: map[string]int{"uuid": 1}, Unique: true}}},
		{Name: "agent_conversations", Indexes: []module.IndexSpec{{Keys: map[string]int{"uuid": 1}, Unique: true}}},
	}
}

func (m *AgentsModule) NavItems() []module.NavItemSpec {
	return []module.NavItemSpec{
		{Group: "AI", Name: "Personal Agent", Icon: "robot", Path: "/ai/personal-agent", Active: true},
		{Group: "AI", Name: "AI Agents", Icon: "users-cog", Path: "/ai/agents", Active: true},
	}
}

func (m *AgentsModule) Init(deps *module.Dependencies) error {
	projectRepo := repository.NewProjectRepository(deps.DB)
	conversationRepo := repository.NewConversationRepository(deps.DB)

	hindsightURL := deps.GetConfig("agents", "hindsightURL")
	hindsightNS := deps.GetConfig("agents", "hindsightNamespace")
	hsClient := services.NewHindsightClient(hindsightURL, deps.Logger)

	// Retain deps so Preflight() and InfraContainers() can resolve
	// aimodels on every toggle.
	m.deps = deps

	// Create RAG bridge if RAG query service is available
	var ragBridge services.RAGBridge
	if ragQuery, ok := module.GetTyped[iface.RAGQueryProvider](deps.Services, module.ServiceRAGQuery); ok {
		ragBridge = services.NewRAGBridge(ragQuery, deps.Config.RAG.DefaultTopK, deps.Logger)
	}

	projectService := services.NewProjectService(projectRepo, hsClient, hindsightNS, deps.Logger)
	agentService := services.NewAgentService(projectRepo, conversationRepo, hsClient, ragBridge, deps.Logger)

	m.projectHandler = handlers.NewProjectHandler(projectService)
	m.agentHandler = handlers.NewAgentHandler(agentService)

	personalAgentService := services.NewPersonalAgentService(projectRepo, agentService, hsClient, hindsightNS, deps.Logger)
	m.personalAgentHandler = handlers.NewPersonalAgentHandler(personalAgentService)

	deps.Logger.Info("Agents module initialized",
		slog.String("hindsightURL", hindsightURL),
	)
	return nil
}

func (m *AgentsModule) RegisterRoutes(ri *module.RouteInfo) {
	ri.ProtectedRouter.Group(func(gated chi.Router) {
		gated.Use(middleware.ModuleGate(ri.ConfigService, m.Name()))
		gated.Use(ri.AuthMW.RequireCapability("agents.access"))

		gated.Group(func(r chi.Router) {
			r.Use(ri.AuthMW.RequirePermission("agents.personal"))
			api := humachi.New(r, ri.APIConfig)
			RegisterPersonalAgentRoutes(api, m.personalAgentHandler)
		})

		gated.Group(func(r chi.Router) {
			r.Use(ri.AuthMW.RequirePermission("agents.project.manage"))
			api := humachi.New(r, ri.APIConfig)
			RegisterProjectRoutes(api, m.projectHandler)
		})

		gated.Group(func(r chi.Router) {
			r.Use(ri.AuthMW.RequirePermission("agents.query"))
			api := humachi.New(r, ri.APIConfig)
			RegisterQueryRoutes(api, m.agentHandler)
		})

		gated.Group(func(r chi.Router) {
			r.Use(ri.AuthMW.RequirePermission("agents.admin"))
			api := humachi.New(r, ri.APIConfig)
			RegisterAdminRoutes(api, m.agentHandler)
		})
	})
}

func (m *AgentsModule) Start(_ context.Context) error      { return nil }
func (m *AgentsModule) Stop(_ context.Context) error       { return nil }
func (m *AgentsModule) HealthCheck(_ context.Context) error { return nil }

// Preflight asserts that aimodels has a default LLM configured before the
// agents module — and its Hindsight container — is allowed to start.
// Without this the container boots, contacts the LLM provider with empty
// credentials, and exits with a confusing 401 before any query runs.
func (m *AgentsModule) Preflight(ctx context.Context) error {
	if _, _, err := m.resolveLLM(ctx); err != nil {
		return err
	}
	return nil
}

func (m *AgentsModule) InfraContainers() []module.InfraContainerSpec {
	if m.deps == nil {
		return nil
	}
	image := m.deps.GetConfig("agents", "hindsightImage")
	if image == "" {
		image = defaultHindsightImage
	}

	env := map[string]string{}
	// Resolve aimodels' default LLM and translate it into the env vars
	// Hindsight expects. On error we leave env empty; Preflight would
	// have already rejected the toggle in the normal code path, so this
	// is just a defensive fallback for out-of-band callers (tests etc.).
	if _, llm, err := m.resolveLLM(context.Background()); err == nil {
		hsProvider, hsModel, hsAPIKey, hsBaseURL := translateLLMForHindsight(llm)
		env["HINDSIGHT_API_LLM_PROVIDER"] = hsProvider
		env["HINDSIGHT_API_LLM_MODEL"] = hsModel
		env["HINDSIGHT_API_LLM_API_KEY"] = hsAPIKey
		if hsBaseURL != "" {
			env["HINDSIGHT_API_LLM_BASE_URL"] = hsBaseURL
		}
	}

	return []module.InfraContainerSpec{{
		Name:    "orkestra-hindsight",
		Image:   image,
		Env:     env,
		Volumes: []module.InfraVolumeMount{{Name: "orkestra-hindsight-data", Target: "/home/hindsight/.pg0"}},
		Ports:   []module.InfraPortBinding{{HostPort: 8888, ContainerPort: 8888, Protocol: "tcp"}},
		Network: "orkestra-network",
		HealthCheck: &module.InfraHealthCheck{
			HTTPPath: "/health",
			Port:     8888,
			Interval: 2 * time.Second,
			Retries:  30,
			Timeout:  5 * time.Second,
		},
		ReadyTimeout: 60 * time.Second,
		Labels:       map[string]string{"orkestra.managed": "true", "orkestra.module": "agents"},
	}}
}

// resolveLLM looks up the aimodels service and returns its default LLM
// config. It centralizes the error messages so Preflight() and
// InfraContainers() see the same validation.
func (m *AgentsModule) resolveLLM(ctx context.Context) (iface.AIModelProvider, iface.LLMConfig, error) {
	if m.deps == nil {
		return nil, iface.LLMConfig{}, fmt.Errorf("agents module not initialized")
	}
	// The service registry keeps providers registered even when the
	// owning module is disabled, so check enabled state explicitly
	// against the config service (DB/Redis) too.
	if m.deps.ConfigService != nil && !m.deps.ConfigService.IsEnabled(ctx, "aimodels") {
		return nil, iface.LLMConfig{}, fmt.Errorf("aimodels module must be enabled — go to /admin/modules and enable \"AI Models\"")
	}
	provider, ok := module.GetTyped[iface.AIModelProvider](m.deps.Services, module.ServiceAIModelProvider)
	if !ok {
		return nil, iface.LLMConfig{}, fmt.Errorf("aimodels module must be enabled — go to /admin/modules and enable \"AI Models\"")
	}
	llm, err := provider.GetDefaultLLMConfig(ctx)
	if err != nil {
		return provider, iface.LLMConfig{}, fmt.Errorf("no default LLM model configured — go to /admin/aimodels and set one as default: %w", err)
	}
	if llm.Model == "" || llm.Provider == "" {
		return provider, llm, fmt.Errorf("default LLM model is incomplete (provider=%q model=%q) — fix it at /admin/aimodels", llm.Provider, llm.Model)
	}
	// API key is required only when the model points at a cloud provider.
	// When BaseURL is set, the provider string (usually "openai") means
	// "OpenAI-compatible API" and targets a local engine like llama.cpp
	// or vLLM that accepts any token — don't block on an empty key.
	cloudProvider := llm.Provider == "openai" || llm.Provider == "anthropic" || llm.Provider == "gemini"
	if cloudProvider && llm.BaseURL == "" && llm.APIKey == "" {
		return provider, llm, fmt.Errorf("default LLM (%s/%s) is missing an API key — set it at /admin/aimodels", llm.Provider, llm.Model)
	}
	return provider, llm, nil
}

// translateLLMForHindsight maps an aimodels LLM config into the four env
// vars Hindsight expects. Hindsight itself only ships provider adapters
// for openai/anthropic/gemini; Ollama and other local engines are driven
// through the OpenAI-compatible path by pointing the provider at the
// local endpoint via BaseURL.
func translateLLMForHindsight(llm iface.LLMConfig) (provider, model, apiKey, baseURL string) {
	model = llm.Model
	apiKey = llm.APIKey
	baseURL = llm.BaseURL

	switch llm.Provider {
	case "ollama":
		provider = "openai" // reach Ollama via its /v1 OpenAI-compat endpoint
		if baseURL == "" {
			baseURL = "http://orkestra-ollama:11434/v1"
		}
	default:
		provider = llm.Provider
	}

	// Hindsight's OpenAI SDK client rejects an empty API key even when
	// the target doesn't require auth (local llama.cpp / Ollama). Fill a
	// placeholder so the client initializes; the local server ignores it.
	if apiKey == "" && baseURL != "" {
		apiKey = "sk-local"
	}
	return provider, model, apiKey, baseURL
}
