package agents

import (
	"net/http"

	"github.com/danielgtaylor/huma/v2"

	"github.com/orkestra/backend/internal/addons/agents/handlers"
)

// RegisterProjectRoutes registers project management routes (requires manager role)
func RegisterProjectRoutes(api huma.API, handler *handlers.ProjectHandler) {
	huma.Register(api, huma.Operation{
		OperationID: "create-agent-project",
		Method:      http.MethodPost,
		Path:        "/v1/agents/projects",
		Summary:     "Create project",
		Description: "Creates a new agent project with an associated Hindsight memory bank.",
		Tags:        []string{"Agent Projects"},
		Security:    []map[string][]string{{"bearerAuth": {}}},
	}, handler.CreateProject)

	huma.Register(api, huma.Operation{
		OperationID: "list-agent-projects",
		Method:      http.MethodGet,
		Path:        "/v1/agents/projects",
		Summary:     "List projects",
		Description: "Lists all agent projects, optionally filtered by status.",
		Tags:        []string{"Agent Projects"},
		Security:    []map[string][]string{{"bearerAuth": {}}},
	}, handler.ListProjects)

	huma.Register(api, huma.Operation{
		OperationID: "get-agent-project",
		Method:      http.MethodGet,
		Path:        "/v1/agents/projects/{uuid}",
		Summary:     "Get project",
		Description: "Returns a specific agent project with its configuration.",
		Tags:        []string{"Agent Projects"},
		Security:    []map[string][]string{{"bearerAuth": {}}},
	}, handler.GetProject)

	huma.Register(api, huma.Operation{
		OperationID: "update-agent-project",
		Method:      http.MethodPatch,
		Path:        "/v1/agents/projects/{uuid}",
		Summary:     "Update project",
		Description: "Updates project name, description, or status. Syncs Hindsight bank mission on description change.",
		Tags:        []string{"Agent Projects"},
		Security:    []map[string][]string{{"bearerAuth": {}}},
	}, handler.UpdateProject)

	huma.Register(api, huma.Operation{
		OperationID: "delete-agent-project",
		Method:      http.MethodDelete,
		Path:        "/v1/agents/projects/{uuid}",
		Summary:     "Delete project",
		Description: "Deletes a project and its associated Hindsight memory bank.",
		Tags:        []string{"Agent Projects"},
		Security:    []map[string][]string{{"bearerAuth": {}}},
	}, handler.DeleteProject)

	huma.Register(api, huma.Operation{
		OperationID: "add-agent-project-documents",
		Method:      http.MethodPost,
		Path:        "/v1/agents/projects/{uuid}/documents",
		Summary:     "Add documents to project",
		Description: "Adds RAG documents to the project scope. Deduplicates automatically.",
		Tags:        []string{"Agent Projects"},
		Security:    []map[string][]string{{"bearerAuth": {}}},
	}, handler.AddDocuments)

	huma.Register(api, huma.Operation{
		OperationID: "remove-agent-project-documents",
		Method:      http.MethodDelete,
		Path:        "/v1/agents/projects/{uuid}/documents",
		Summary:     "Remove documents from project",
		Description: "Removes RAG documents from the project scope.",
		Tags:        []string{"Agent Projects"},
		Security:    []map[string][]string{{"bearerAuth": {}}},
	}, handler.RemoveDocuments)

	huma.Register(api, huma.Operation{
		OperationID: "update-agent-project-filters",
		Method:      http.MethodPatch,
		Path:        "/v1/agents/projects/{uuid}/filters",
		Summary:     "Update project filters",
		Description: "Updates ISO standard and document category filters for the project.",
		Tags:        []string{"Agent Projects"},
		Security:    []map[string][]string{{"bearerAuth": {}}},
	}, handler.UpdateFilters)

	huma.Register(api, huma.Operation{
		OperationID: "update-agent-project-settings",
		Method:      http.MethodPatch,
		Path:        "/v1/agents/projects/{uuid}/settings",
		Summary:     "Update agent settings",
		Description: "Tune agent behavior: system prompt, directives, disposition traits, response style, and language.",
		Tags:        []string{"Agent Projects"},
		Security:    []map[string][]string{{"bearerAuth": {}}},
	}, handler.UpdateSettings)

	huma.Register(api, huma.Operation{
		OperationID: "get-agent-project-settings",
		Method:      http.MethodGet,
		Path:        "/v1/agents/projects/{uuid}/settings",
		Summary:     "Get agent settings",
		Description: "Returns the current agent behavior settings for a project.",
		Tags:        []string{"Agent Projects"},
		Security:    []map[string][]string{{"bearerAuth": {}}},
	}, handler.GetSettings)
}

// RegisterQueryRoutes registers agent query and conversation routes (requires operator role)
func RegisterQueryRoutes(api huma.API, handler *handlers.AgentHandler) {
	huma.Register(api, huma.Operation{
		OperationID: "agent-query",
		Method:      http.MethodPost,
		Path:        "/v1/agents/projects/{uuid}/query",
		Summary:     "Query project agent",
		Description: "Sends a question to the project's AI agent. Combines RAG document context with Hindsight persistent memory.",
		Tags:        []string{"Agent Query"},
		Security:    []map[string][]string{{"bearerAuth": {}}},
	}, handler.Query)

	huma.Register(api, huma.Operation{
		OperationID: "create-agent-conversation",
		Method:      http.MethodPost,
		Path:        "/v1/agents/projects/{uuid}/conversations",
		Summary:     "Start new conversation",
		Description: "Creates a new conversation session with the project agent.",
		Tags:        []string{"Agent Conversations"},
		Security:    []map[string][]string{{"bearerAuth": {}}},
	}, handler.CreateConversation)

	huma.Register(api, huma.Operation{
		OperationID: "list-agent-conversations",
		Method:      http.MethodGet,
		Path:        "/v1/agents/projects/{uuid}/conversations",
		Summary:     "List conversations",
		Description: "Lists conversation sessions for a project, sorted by most recently updated.",
		Tags:        []string{"Agent Conversations"},
		Security:    []map[string][]string{{"bearerAuth": {}}},
	}, handler.ListConversations)

	huma.Register(api, huma.Operation{
		OperationID: "get-agent-conversation",
		Method:      http.MethodGet,
		Path:        "/v1/agents/conversations/{uuid}",
		Summary:     "Get conversation",
		Description: "Returns a conversation with all messages, sources, and metadata.",
		Tags:        []string{"Agent Conversations"},
		Security:    []map[string][]string{{"bearerAuth": {}}},
	}, handler.GetConversation)

	huma.Register(api, huma.Operation{
		OperationID: "delete-agent-conversation",
		Method:      http.MethodDelete,
		Path:        "/v1/agents/conversations/{uuid}",
		Summary:     "Delete conversation",
		Description: "Deletes a conversation and all its messages.",
		Tags:        []string{"Agent Conversations"},
		Security:    []map[string][]string{{"bearerAuth": {}}},
	}, handler.DeleteConversation)
}

// RegisterPersonalAgentRoutes registers personal agent routes (any authenticated user)
func RegisterPersonalAgentRoutes(api huma.API, handler *handlers.PersonalAgentHandler) {
	huma.Register(api, huma.Operation{
		OperationID: "get-personal-agent",
		Method:      http.MethodGet,
		Path:        "/v1/agents/personal",
		Summary:     "Get personal agent",
		Description: "Returns the current user's personal agent project, auto-creating it on first access.",
		Tags:        []string{"Personal Agent"},
		Security:    []map[string][]string{{"bearerAuth": {}}},
	}, handler.GetOrCreate)

	huma.Register(api, huma.Operation{
		OperationID: "personal-agent-query",
		Method:      http.MethodPost,
		Path:        "/v1/agents/personal/query",
		Summary:     "Query personal agent",
		Description: "Sends a question to your personal AI agent. Combines RAG document context with Hindsight persistent memory.",
		Tags:        []string{"Personal Agent"},
		Security:    []map[string][]string{{"bearerAuth": {}}},
	}, handler.Query)

	huma.Register(api, huma.Operation{
		OperationID: "personal-agent-add-documents",
		Method:      http.MethodPost,
		Path:        "/v1/agents/personal/documents",
		Summary:     "Add documents to personal agent",
		Description: "Adds RAG documents to your personal agent's scope. Deduplicates automatically.",
		Tags:        []string{"Personal Agent"},
		Security:    []map[string][]string{{"bearerAuth": {}}},
	}, handler.AddDocuments)

	huma.Register(api, huma.Operation{
		OperationID: "personal-agent-remove-documents",
		Method:      http.MethodDelete,
		Path:        "/v1/agents/personal/documents",
		Summary:     "Remove documents from personal agent",
		Description: "Removes RAG documents from your personal agent's scope.",
		Tags:        []string{"Personal Agent"},
		Security:    []map[string][]string{{"bearerAuth": {}}},
	}, handler.RemoveDocuments)

	huma.Register(api, huma.Operation{
		OperationID: "personal-agent-update-settings",
		Method:      http.MethodPatch,
		Path:        "/v1/agents/personal/settings",
		Summary:     "Update personal agent settings",
		Description: "Tune your personal agent's behavior: system prompt, directives, disposition traits, response style, and language.",
		Tags:        []string{"Personal Agent"},
		Security:    []map[string][]string{{"bearerAuth": {}}},
	}, handler.UpdateSettings)

	huma.Register(api, huma.Operation{
		OperationID: "personal-agent-get-settings",
		Method:      http.MethodGet,
		Path:        "/v1/agents/personal/settings",
		Summary:     "Get personal agent settings",
		Description: "Returns the current behavior settings for your personal agent.",
		Tags:        []string{"Personal Agent"},
		Security:    []map[string][]string{{"bearerAuth": {}}},
	}, handler.GetSettings)

	huma.Register(api, huma.Operation{
		OperationID: "personal-agent-list-conversations",
		Method:      http.MethodGet,
		Path:        "/v1/agents/personal/conversations",
		Summary:     "List personal conversations",
		Description: "Lists your conversation sessions with the personal agent.",
		Tags:        []string{"Personal Agent"},
		Security:    []map[string][]string{{"bearerAuth": {}}},
	}, handler.ListConversations)

	huma.Register(api, huma.Operation{
		OperationID: "personal-agent-get-conversation",
		Method:      http.MethodGet,
		Path:        "/v1/agents/personal/conversations/{uuid}",
		Summary:     "Get personal conversation",
		Description: "Returns a personal conversation with all messages, sources, and metadata.",
		Tags:        []string{"Personal Agent"},
		Security:    []map[string][]string{{"bearerAuth": {}}},
	}, handler.GetConversation)

	huma.Register(api, huma.Operation{
		OperationID: "personal-agent-delete-conversation",
		Method:      http.MethodDelete,
		Path:        "/v1/agents/personal/conversations/{uuid}",
		Summary:     "Delete personal conversation",
		Description: "Deletes a personal conversation and all its messages.",
		Tags:        []string{"Personal Agent"},
		Security:    []map[string][]string{{"bearerAuth": {}}},
	}, handler.DeleteConversation)
}

// RegisterAdminRoutes registers agent admin routes (requires administrator role)
func RegisterAdminRoutes(api huma.API, handler *handlers.AgentHandler) {
	huma.Register(api, huma.Operation{
		OperationID: "get-agent-bank-info",
		Method:      http.MethodGet,
		Path:        "/v1/agents/projects/{uuid}/bank",
		Summary:     "Get Hindsight bank info",
		Description: "Returns information about the project's Hindsight memory bank.",
		Tags:        []string{"Agent Admin"},
		Security:    []map[string][]string{{"bearerAuth": {}}},
	}, handler.GetBankInfo)

	huma.Register(api, huma.Operation{
		OperationID: "agent-health-check",
		Method:      http.MethodGet,
		Path:        "/v1/agents/health",
		Summary:     "Hindsight health check",
		Description: "Checks connectivity to the Hindsight memory service.",
		Tags:        []string{"Agent Admin"},
		Security:    []map[string][]string{{"bearerAuth": {}}},
	}, handler.HealthCheck)
}
