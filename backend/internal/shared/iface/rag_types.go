package iface

// RAG contract types live here (rather than in addons/rag/models) so that
// the iface package — the cross-module contract layer — does not import
// any addon package. The rag addon imports these from iface like every
// other consumer.

// SourceRef points to a chunk that contributed to a RAG answer. Returned
// alongside the answer so consumers can render citations or follow the
// provenance back to the source document.
type SourceRef struct {
	DocumentUUID     string  `json:"documentUuid"`
	DocumentTitle    string  `json:"documentTitle"`
	ISOStandard      string  `json:"isoStandard,omitempty"`
	ChunkUUID        string  `json:"chunkUuid"`
	ChunkText        string  `json:"chunkText"`
	FullPath         string  `json:"fullPath,omitempty"`
	NodeType         string  `json:"nodeType,omitempty"`
	RequirementLevel string  `json:"requirementLevel,omitempty"`
	Score            float64 `json:"score"`
	Position         int     `json:"position"`
}

// QueryMeta carries timing/sizing breakdowns for a RAG query — useful for
// observability dashboards and debugging slow requests.
type QueryMeta struct {
	EmbeddingTimeMs int64  `json:"embeddingTimeMs"`
	SearchTimeMs    int64  `json:"searchTimeMs"`
	LLMTimeMs       int64  `json:"llmTimeMs"`
	TotalTimeMs     int64  `json:"totalTimeMs"`
	ChunksRetrieved int    `json:"chunksRetrieved"`
	ModelUsed       string `json:"modelUsed"`
}

// RAGQueryResponse is the contract returned by RAGQueryProvider.Query.
// The Body wrapper matches Huma's response-DTO shape so the rag addon's
// query handler can return the value directly. Consumers reading the
// answer should access resp.Body.Answer / Sources / Metadata.
type RAGQueryResponse struct {
	Body struct {
		Answer   string      `json:"answer" doc:"Generated answer"`
		Sources  []SourceRef `json:"sources" doc:"Source references"`
		Metadata QueryMeta   `json:"metadata" doc:"Query timing metadata"`
	}
}
