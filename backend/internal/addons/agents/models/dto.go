package models

// --- Project DTOs ---

type CreateProjectRequest struct {
	Body struct {
		Name          string   `json:"name" doc:"Project name" required:"true" minLength:"1" maxLength:"200"`
		Description   string   `json:"description" doc:"Project description — used as the agent's mission" required:"true" minLength:"1"`
		DocumentUUIDs []string `json:"documentUuids,omitempty" doc:"RAG document UUIDs scoped to this project"`
		ISOStandards  []string `json:"isoStandards,omitempty" doc:"ISO standard filters (e.g. ISO 27001)"`
		Categories    []string `json:"categories,omitempty" doc:"Document category filters: iso, law, regulation, generic"`
	}
}

type CreateProjectResponse struct {
	Body Project
}

type ListProjectsRequest struct {
	Status string `query:"status" doc:"Filter by status: active or archived"`
}

type ListProjectsResponse struct {
	Body struct {
		Projects []Project `json:"projects" doc:"List of projects"`
	}
}

type GetProjectRequest struct {
	UUID string `path:"uuid" doc:"Project UUID"`
}

type GetProjectResponse struct {
	Body Project
}

type UpdateProjectRequest struct {
	UUID string `path:"uuid" doc:"Project UUID"`
	Body struct {
		Name        *string  `json:"name,omitempty" doc:"Project name"`
		Description *string  `json:"description,omitempty" doc:"Project description"`
		Status      *string  `json:"status,omitempty" doc:"Project status: active or archived"`
	}
}

type UpdateProjectResponse struct {
	Body Project
}

type DeleteProjectRequest struct {
	UUID string `path:"uuid" doc:"Project UUID"`
}

type DeleteProjectResponse struct {
	Body struct {
		Message string `json:"message" doc:"Confirmation message"`
	}
}

type AddDocumentsRequest struct {
	UUID string `path:"uuid" doc:"Project UUID"`
	Body struct {
		DocumentUUIDs []string `json:"documentUuids" doc:"RAG document UUIDs to add" required:"true"`
	}
}

type AddDocumentsResponse struct {
	Body Project
}

type RemoveDocumentsRequest struct {
	UUID string `path:"uuid" doc:"Project UUID"`
	Body struct {
		DocumentUUIDs []string `json:"documentUuids" doc:"RAG document UUIDs to remove" required:"true"`
	}
}

type RemoveDocumentsResponse struct {
	Body Project
}

type UpdateFiltersRequest struct {
	UUID string `path:"uuid" doc:"Project UUID"`
	Body struct {
		ISOStandards []string `json:"isoStandards,omitempty" doc:"ISO standard filters"`
		Categories   []string `json:"categories,omitempty" doc:"Document category filters"`
	}
}

type UpdateFiltersResponse struct {
	Body Project
}

// --- Agent Settings DTOs ---

type UpdateSettingsRequest struct {
	UUID string `path:"uuid" doc:"Project UUID"`
	Body struct {
		SystemPrompt *string  `json:"systemPrompt,omitempty" doc:"Custom system prompt prepended to every query"`
		Directives   []string `json:"directives,omitempty" doc:"Extra directives merged with persona defaults"`
		Skepticism   *int32   `json:"skepticism,omitempty" doc:"1=trusting, 5=strict to docs (0=persona default)"`
		Literalism   *int32   `json:"literalism,omitempty" doc:"1=creative, 5=strictly literal (0=persona default)"`
		Empathy      *int32   `json:"empathy,omitempty" doc:"1=detached, 5=helpful/warm (0=persona default)"`
		MaxTokens    *int32   `json:"maxTokens,omitempty" doc:"Max response tokens (0=persona default)"`
		Temperature  *string  `json:"temperature,omitempty" doc:"Response style: precise, balanced, creative"`
		Language     *string  `json:"language,omitempty" doc:"Force response language (e.g. en, it)"`
	}
}

type UpdateSettingsResponse struct {
	Body Project
}

type GetSettingsRequest struct {
	UUID string `path:"uuid" doc:"Project UUID"`
}

type GetSettingsResponse struct {
	Body struct {
		Settings *AgentSettings `json:"settings" doc:"Current agent settings"`
	}
}

// --- Agent Query DTOs ---

type AgentQueryRequest struct {
	UUID string `path:"uuid" doc:"Project UUID"`
	Body struct {
		Question       string  `json:"question" doc:"Question to ask the agent" required:"true" minLength:"1"`
		Persona        string  `json:"persona,omitempty" doc:"Query persona: developer, administrator, manager, auditor, guest"`
		ConversationID string  `json:"conversationId,omitempty" doc:"Existing conversation UUID to continue (omit to start new)"`
		TopK           int     `json:"topK,omitempty" doc:"Number of RAG chunks to retrieve"`
		MinScore       float64 `json:"minScore,omitempty" doc:"Minimum similarity score for RAG results"`
		RetrievalMode  string  `json:"retrievalMode,omitempty" doc:"RAG retrieval mode: vector, graph, hybrid"`
	}
}

type AgentQueryResponse struct {
	Body struct {
		Answer         string   `json:"answer" doc:"Agent's response"`
		Sources        []Source `json:"sources" doc:"RAG source citations"`
		ConversationID string   `json:"conversationId" doc:"Conversation UUID"`
		Metadata       MsgMeta  `json:"metadata" doc:"Timing and processing metadata"`
	}
}

// --- Conversation DTOs ---

type CreateConversationRequest struct {
	UUID string `path:"uuid" doc:"Project UUID"`
	Body struct {
		Persona string `json:"persona,omitempty" doc:"Query persona for this conversation"`
	}
}

type CreateConversationResponse struct {
	Body Conversation
}

type ListConversationsRequest struct {
	UUID   string `path:"uuid" doc:"Project UUID"`
	Limit  int    `query:"limit" doc:"Max conversations to return" default:"20"`
	Offset int    `query:"offset" doc:"Number of conversations to skip" default:"0"`
}

type ListConversationsResponse struct {
	Body struct {
		Conversations []Conversation `json:"conversations" doc:"List of conversations"`
		Total         int64          `json:"total" doc:"Total number of conversations"`
	}
}

type GetConversationRequest struct {
	UUID string `path:"uuid" doc:"Conversation UUID"`
}

type GetConversationResponse struct {
	Body Conversation
}

type DeleteConversationRequest struct {
	UUID string `path:"uuid" doc:"Conversation UUID"`
}

type DeleteConversationResponse struct {
	Body struct {
		Message string `json:"message" doc:"Confirmation message"`
	}
}

// --- Admin DTOs ---

type GetBankInfoRequest struct {
	UUID string `path:"uuid" doc:"Project UUID"`
}

type GetBankInfoResponse struct {
	Body struct {
		BankID    string `json:"bankId" doc:"Hindsight bank ID"`
		Status    string `json:"status" doc:"Bank status"`
		Memories  int    `json:"memories,omitempty" doc:"Number of stored memories"`
	}
}

type AgentHealthCheckResponse struct {
	Body struct {
		Hindsight string `json:"hindsight" doc:"Hindsight connection status: ok or degraded"`
	}
}
