package models

// --- Personal Agent DTOs ---

// GET /v1/agents/personal
type GetPersonalAgentResponse struct {
	Body Project
}

// POST /v1/agents/personal/query
type PersonalQueryRequest struct {
	Body struct {
		Question       string  `json:"question" doc:"Question to ask your personal agent" required:"true" minLength:"1"`
		Persona        string  `json:"persona,omitempty" doc:"Query persona: developer, administrator, manager, auditor, guest"`
		ConversationID string  `json:"conversationId,omitempty" doc:"Existing conversation UUID to continue"`
		TopK           int     `json:"topK,omitempty" doc:"Number of RAG chunks to retrieve"`
		MinScore       float64 `json:"minScore,omitempty" doc:"Minimum similarity score for RAG results"`
		RetrievalMode  string  `json:"retrievalMode,omitempty" doc:"RAG retrieval mode: vector, graph, hybrid"`
	}
}

type PersonalQueryResponse struct {
	Body struct {
		Answer         string   `json:"answer" doc:"Agent's response"`
		Sources        []Source `json:"sources" doc:"RAG source citations"`
		ConversationID string   `json:"conversationId" doc:"Conversation UUID"`
		Metadata       MsgMeta  `json:"metadata" doc:"Timing and processing metadata"`
	}
}

// POST /v1/agents/personal/documents
type PersonalAddDocumentsRequest struct {
	Body struct {
		DocumentUUIDs []string `json:"documentUuids" doc:"RAG document UUIDs to add" required:"true"`
	}
}

type PersonalAddDocumentsResponse struct {
	Body Project
}

// DELETE /v1/agents/personal/documents
type PersonalRemoveDocumentsRequest struct {
	Body struct {
		DocumentUUIDs []string `json:"documentUuids" doc:"RAG document UUIDs to remove" required:"true"`
	}
}

type PersonalRemoveDocumentsResponse struct {
	Body Project
}

// PATCH /v1/agents/personal/settings
type PersonalUpdateSettingsRequest struct {
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

type PersonalUpdateSettingsResponse struct {
	Body Project
}

// GET /v1/agents/personal/settings
type PersonalGetSettingsResponse struct {
	Body struct {
		Settings *AgentSettings `json:"settings" doc:"Current agent settings"`
	}
}

// GET /v1/agents/personal/conversations
type PersonalListConversationsRequest struct {
	Limit  int `query:"limit" doc:"Max conversations to return" default:"20"`
	Offset int `query:"offset" doc:"Number of conversations to skip" default:"0"`
}

type PersonalListConversationsResponse struct {
	Body struct {
		Conversations []Conversation `json:"conversations" doc:"List of conversations"`
		Total         int64          `json:"total" doc:"Total number of conversations"`
	}
}

// GET /v1/agents/personal/conversations/{uuid}
type PersonalGetConversationRequest struct {
	UUID string `path:"uuid" doc:"Conversation UUID"`
}

type PersonalGetConversationResponse struct {
	Body Conversation
}

// DELETE /v1/agents/personal/conversations/{uuid}
type PersonalDeleteConversationRequest struct {
	UUID string `path:"uuid" doc:"Conversation UUID"`
}

type PersonalDeleteConversationResponse struct {
	Body struct {
		Message string `json:"message" doc:"Confirmation message"`
	}
}
