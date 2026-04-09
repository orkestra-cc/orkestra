package agents

import (
	"context"
	"log/slog"

	"github.com/danielgtaylor/huma/v2/adapters/humachi"
	"github.com/go-chi/chi/v5"
	"github.com/orkestra/backend/internal/addons/agents/handlers"
	"github.com/orkestra/backend/internal/addons/agents/repository"
	"github.com/orkestra/backend/internal/addons/agents/services"
	"github.com/orkestra/backend/internal/shared/config"
	"github.com/orkestra/backend/internal/shared/iface"
	"github.com/orkestra/backend/internal/shared/middleware"
	"github.com/orkestra/backend/internal/shared/module"
)

type AgentsModule struct {
	module.BaseModule
	projectHandler       *handlers.ProjectHandler
	agentHandler         *handlers.AgentHandler
	personalAgentHandler *handlers.PersonalAgentHandler
}

func NewModule() *AgentsModule { return &AgentsModule{} }

func (m *AgentsModule) Name() string                   { return "agents" }
func (m *AgentsModule) DisplayName() string             { return "AI Agents" }
func (m *AgentsModule) Description() string             { return "Hindsight-powered AI agents with RAG context" }
func (m *AgentsModule) Category() module.ModuleCategory { return module.CategoryToggleable }
func (m *AgentsModule) Enabled(cfg *config.Config) bool { return cfg.Agents.Enabled }
func (m *AgentsModule) Dependencies() []string          { return []string{"auth"} }
func (m *AgentsModule) OptionalServices() []module.ServiceKey {
	return []module.ServiceKey{module.ServiceRAGQuery}
}

func (m *AgentsModule) ConfigSchema() []module.ConfigField {
	return []module.ConfigField{
		{Key: "hindsightURL", Label: "Hindsight URL", Type: module.FieldString, Default: "http://orkestra-hindsight:8888", EnvVar: "HINDSIGHT_URL"},
		{Key: "hindsightNamespace", Label: "Hindsight Namespace", Type: module.FieldString, Default: "orkestra", EnvVar: "HINDSIGHT_NAMESPACE"},
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
		{Group: "AI", Name: "Personal Agent", Icon: "robot", Path: "/ai/personal-agent", MinRole: "guest", Active: true},
		{Group: "AI", Name: "AI Agents", Icon: "users-cog", Path: "/ai/agents", MinRole: "manager", Active: true},
	}
}

func (m *AgentsModule) Init(deps *module.Dependencies) error {
	projectRepo := repository.NewProjectRepository(deps.DB)
	conversationRepo := repository.NewConversationRepository(deps.DB)

	hindsightURL := deps.GetConfig("agents", "hindsightURL")
	hindsightNS := deps.GetConfig("agents", "hindsightNamespace")
	hsClient := services.NewHindsightClient(hindsightURL, deps.Logger)

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

		// Personal agent: any authenticated user (guest and above)
		gated.Group(func(r chi.Router) {
			r.Use(ri.AuthMW.RequireHierarchicalRole("guest"))
			api := humachi.New(r, ri.APIConfig)
			RegisterPersonalAgentRoutes(api, m.personalAgentHandler)
		})

		// Project management: manager role and above
		gated.Group(func(r chi.Router) {
			r.Use(ri.AuthMW.RequireHierarchicalRole("manager"))
			api := humachi.New(r, ri.APIConfig)
			RegisterProjectRoutes(api, m.projectHandler)
		})

		// Agent querying and conversations: operator role and above
		gated.Group(func(r chi.Router) {
			r.Use(ri.AuthMW.RequireHierarchicalRole("operator"))
			api := humachi.New(r, ri.APIConfig)
			RegisterQueryRoutes(api, m.agentHandler)
		})

		// Agent admin: administrator role and above
		gated.Group(func(r chi.Router) {
			r.Use(ri.AuthMW.RequireHierarchicalRole("administrator"))
			api := humachi.New(r, ri.APIConfig)
			RegisterAdminRoutes(api, m.agentHandler)
		})
	})
}

func (m *AgentsModule) Start(_ context.Context) error      { return nil }
func (m *AgentsModule) Stop(_ context.Context) error       { return nil }
func (m *AgentsModule) HealthCheck(_ context.Context) error { return nil }
