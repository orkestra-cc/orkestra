package models

import "github.com/orkestra/backend/pkg/sdk/iface"

// --- Internal Service-to-Service DTOs ---
// Used by the monolith's RemoteRAGQueryProvider to call the AI service.

// InternalRAGQueryRequest extends the public query with documentUUIDs scoping.
// This matches the full signature of RAGQueryProvider.Query().
type InternalRAGQueryRequest struct {
	Body struct {
		Question         string   `json:"question" doc:"Natural language question" required:"true"`
		TopK             int      `json:"topK,omitempty" doc:"Number of chunks to retrieve" default:"10"`
		MinScore         float64  `json:"minScore,omitempty" doc:"Minimum similarity score" default:"0.3"`
		ISOStandard      string   `json:"isoStandard,omitempty" doc:"Filter by ISO standard"`
		LLMOverrideUUID  string   `json:"llmOverrideUuid,omitempty" doc:"Override default LLM model"`
		RequirementLevel string   `json:"requirementLevel,omitempty" doc:"Filter by requirement level: SHALL, SHOULD, MAY"`
		NodeType         string   `json:"nodeType,omitempty" doc:"Filter by node type"`
		RetrievalMode    string   `json:"retrievalMode,omitempty" doc:"Retrieval mode: vector, graph" default:"vector"`
		DocumentUUIDs    []string `json:"documentUuids,omitempty" doc:"Scope to specific documents"`
	}
}

// InternalRAGQueryResponse reuses the standard RAGQueryResponse shape.
// Defined as a type alias for clarity in the internal routes.
type InternalRAGQueryResponse = iface.RAGQueryResponse
