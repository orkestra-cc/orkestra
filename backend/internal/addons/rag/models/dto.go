package models

// --- Model DTOs ---

type CreateModelRequest struct {
	Body struct {
		Name        string  `json:"name" doc:"Display name" required:"true"`
		Provider    string  `json:"provider" doc:"Provider: ollama or openai" required:"true" enum:"ollama,openai"`
		ModelType   string  `json:"modelType" doc:"Model type: embedding or llm" required:"true" enum:"embedding,llm"`
		ModelName   string  `json:"modelName" doc:"Model identifier (e.g. nomic-embed-text, gpt-4o)" required:"true"`
		BaseURL     string  `json:"baseUrl,omitempty" doc:"Provider base URL (required for Ollama)"`
		APIKey      string  `json:"apiKey,omitempty" doc:"API key (required for OpenAI)"`
		Dimensions  int     `json:"dimensions,omitempty" doc:"Embedding dimensions (embedding models only)"`
		Temperature float64 `json:"temperature,omitempty" doc:"Generation temperature (LLM models only)"`
		MaxTokens   int     `json:"maxTokens,omitempty" doc:"Max tokens (LLM models only)"`
	}
}

type CreateModelResponse struct {
	Body ModelConfig
}

type ListModelsRequest struct {
	Type string `query:"type" doc:"Filter by model type: embedding or llm"`
}

type ListModelsResponse struct {
	Body struct {
		Models []ModelConfig `json:"models" doc:"List of model configurations"`
	}
}

type GetModelRequest struct {
	UUID string `path:"uuid" doc:"Model UUID"`
}

type GetModelResponse struct {
	Body ModelConfig
}

type UpdateModelRequest struct {
	UUID string `path:"uuid" doc:"Model UUID"`
	Body struct {
		Name        *string  `json:"name,omitempty" doc:"Display name"`
		BaseURL     *string  `json:"baseUrl,omitempty" doc:"Provider base URL"`
		APIKey      *string  `json:"apiKey,omitempty" doc:"API key"`
		Dimensions  *int     `json:"dimensions,omitempty" doc:"Embedding dimensions"`
		Temperature *float64 `json:"temperature,omitempty" doc:"Generation temperature"`
		MaxTokens   *int     `json:"maxTokens,omitempty" doc:"Max tokens"`
		IsActive    *bool    `json:"isActive,omitempty" doc:"Active status"`
	}
}

type UpdateModelResponse struct {
	Body ModelConfig
}

type DeleteModelRequest struct {
	UUID string `path:"uuid" doc:"Model UUID"`
}

type DeleteModelResponse struct {
	Body struct {
		Message string `json:"message" doc:"Confirmation message"`
	}
}

type SetDefaultModelRequest struct {
	UUID string `path:"uuid" doc:"Model UUID"`
}

type SetDefaultModelResponse struct {
	Body struct {
		Message string `json:"message" doc:"Confirmation message"`
	}
}

type TestModelRequest struct {
	UUID string `path:"uuid" doc:"Model UUID"`
}

type TestModelResponse struct {
	Body struct {
		Status  string `json:"status" doc:"Connection status"`
		Message string `json:"message" doc:"Status message"`
	}
}

// AvailableModel represents a model available on a provider
type AvailableModel struct {
	ID      string `json:"id" doc:"Model identifier"`
	OwnedBy string `json:"ownedBy,omitempty" doc:"Model owner"`
}

type FetchModelsRequest struct {
	Body struct {
		Provider string `json:"provider" doc:"Provider: ollama or openai" required:"true" enum:"ollama,openai"`
		BaseURL  string `json:"baseUrl" doc:"Provider base URL" required:"true"`
		APIKey   string `json:"apiKey,omitempty" doc:"API key (for OpenAI cloud)"`
	}
}

type FetchModelsResponse struct {
	Body struct {
		Models []AvailableModel `json:"models" doc:"Available models on the provider"`
	}
}

// --- Document DTOs ---

type ListDocumentsRequest struct {
	Status      string `query:"status" doc:"Filter by status"`
	ISOStandard string `query:"isoStandard" doc:"Filter by ISO standard"`
}

type ListDocumentsResponse struct {
	Body struct {
		Documents []RagDocument `json:"documents" doc:"List of documents"`
	}
}

type GetDocumentRequest struct {
	UUID string `path:"uuid" doc:"Document UUID"`
}

type GetDocumentResponse struct {
	Body RagDocument
}

type DeleteDocumentRequest struct {
	UUID string `path:"uuid" doc:"Document UUID"`
}

type DeleteDocumentResponse struct {
	Body struct {
		Message string `json:"message" doc:"Confirmation message"`
	}
}

type UpdateDocumentRequest struct {
	UUID string `path:"uuid" doc:"Document UUID"`
	Body struct {
		Title       *string `json:"title,omitempty" doc:"Document title"`
		ISOStandard *string `json:"isoStandard,omitempty" doc:"ISO standard"`
		Version     *string `json:"version,omitempty" doc:"Document version"`
	}
}

type UpdateDocumentResponse struct {
	Body RagDocument
}

type GetDocumentChunksRequest struct {
	UUID string `path:"uuid" doc:"Document UUID"`
}

type RagChunk struct {
	UUID             string `json:"uuid"`
	DocumentUUID     string `json:"documentUuid"`
	Text             string `json:"text"`
	Position         int    `json:"position"`
	FullPath         string `json:"fullPath,omitempty"`
	NodeType         string `json:"nodeType,omitempty"`
	Numbering        string `json:"numbering,omitempty"`
	RequirementLevel string `json:"requirementLevel,omitempty"`
	Depth            int    `json:"depth,omitempty"`
}

// RagSection represents a structural section of a document
type RagSection struct {
	UUID         string `json:"uuid"`
	DocumentUUID string `json:"documentUuid"`
	NodeType     string `json:"nodeType"`
	Numbering    string `json:"numbering,omitempty"`
	Title        string `json:"title,omitempty"`
	Depth        int    `json:"depth"`
	FullPath     string `json:"fullPath,omitempty"`
	Position     int    `json:"position"`
}

type GetDocumentSectionsRequest struct {
	UUID string `path:"uuid" doc:"Document UUID"`
}

type GetDocumentSectionsResponse struct {
	Body struct {
		Sections []RagSection `json:"sections" doc:"Document sections ordered by position"`
	}
}

type GetDocumentChunksResponse struct {
	Body struct {
		Chunks []RagChunk `json:"chunks" doc:"Document chunks ordered by position"`
	}
}

type ReprocessDocumentRequest struct {
	UUID string `path:"uuid" doc:"Document UUID"`
}

type ReprocessDocumentResponse struct {
	Body struct {
		Message string `json:"message" doc:"Status message"`
	}
}

// --- Cross-Document Relationship DTOs ---

type GetDocumentRelationsRequest struct {
	UUID string `path:"uuid" doc:"Document UUID"`
}

// CrossDocLink represents a similarity link between chunks from two different documents
type CrossDocLink struct {
	SourceChunkUUID  string  `json:"sourceChunkUuid"`
	SourceFullPath   string  `json:"sourceFullPath"`
	SourceText       string  `json:"sourceText"`
	TargetChunkUUID  string  `json:"targetChunkUuid"`
	TargetFullPath   string  `json:"targetFullPath"`
	TargetText       string  `json:"targetText"`
	TargetDocUUID    string  `json:"targetDocUuid"`
	TargetDocTitle   string  `json:"targetDocTitle"`
	Similarity       float64 `json:"similarity"`
}

// RelatedDocSummary summarizes cross-doc relationships to another document
type RelatedDocSummary struct {
	DocumentUUID  string  `json:"documentUuid"`
	DocumentTitle string  `json:"documentTitle"`
	ISOStandard   string  `json:"isoStandard,omitempty"`
	LinkCount     int     `json:"linkCount"`
	AvgSimilarity float64 `json:"avgSimilarity"`
	MaxSimilarity float64 `json:"maxSimilarity"`
}

type GetDocumentRelationsResponse struct {
	Body struct {
		RelatedDocuments []RelatedDocSummary `json:"relatedDocuments" doc:"Summary of related documents"`
		Links            []CrossDocLink      `json:"links" doc:"Individual cross-document similarity links"`
		TotalLinks       int                 `json:"totalLinks" doc:"Total number of cross-document links"`
	}
}

// --- RAG Query DTOs ---

type RAGQueryRequest struct {
	Body struct {
		Question         string  `json:"question" doc:"Natural language question" required:"true"`
		TopK             int     `json:"topK,omitempty" doc:"Number of chunks to retrieve" default:"10"`
		MinScore         float64 `json:"minScore,omitempty" doc:"Minimum similarity score" default:"0.3"`
		ISOStandard      string  `json:"isoStandard,omitempty" doc:"Filter by ISO standard"`
		ModelUUID        string  `json:"modelUuid,omitempty" doc:"Override default LLM model"`
		RequirementLevel string  `json:"requirementLevel,omitempty" doc:"Filter by requirement level: SHALL, SHOULD, MAY"`
		NodeType         string  `json:"nodeType,omitempty" doc:"Filter by node type: requirement, definition, note, etc."`
		RetrievalMode    string  `json:"retrievalMode,omitempty" doc:"Retrieval mode: vector (default), graph, hybrid" default:"vector"`
	}
}

// RAG query contract types (SourceRef, QueryMeta, RAGQueryResponse) live
// in shared/iface/rag_types.go so the iface contract layer doesn't import
// this addon package. Consumers — including this addon — reference them
// as iface.SourceRef, iface.QueryMeta, iface.RAGQueryResponse.

// --- Relationship Type DTOs ---

type ListRelationshipTypesRequest struct{}

type ListRelationshipTypesResponse struct {
	Body struct {
		RelationshipTypes []RelationshipTypeConfig `json:"relationshipTypes" doc:"Configured relationship types"`
	}
}

type CreateRelationshipTypeRequest struct {
	Body struct {
		Name        string          `json:"name" doc:"Relationship type name (e.g. REFERENCES)" required:"true"`
		Description string          `json:"description,omitempty" doc:"Human-readable description"`
		FromNode    string          `json:"fromNode" doc:"Source node label" required:"true"`
		ToNode      string          `json:"toNode" doc:"Target node label" required:"true"`
		Properties  []string        `json:"properties,omitempty" doc:"Edge property names"`
		Categories  map[string]bool `json:"categories" doc:"Category toggles" required:"true"`
	}
}

type CreateRelationshipTypeResponse struct {
	Body RelationshipTypeConfig
}

type UpdateRelationshipTypeRequest struct {
	UUID string `path:"uuid" doc:"Relationship type UUID"`
	Body struct {
		Description *string          `json:"description,omitempty" doc:"Human-readable description"`
		Properties  *[]string        `json:"properties,omitempty" doc:"Edge property names"`
		Categories  *map[string]bool `json:"categories,omitempty" doc:"Category toggles"`
	}
}

type UpdateRelationshipTypeResponse struct {
	Body RelationshipTypeConfig
}

type DeleteRelationshipTypeRequest struct {
	UUID string `path:"uuid" doc:"Relationship type UUID"`
}

type DeleteRelationshipTypeResponse struct {
	Body struct {
		Message string `json:"message" doc:"Confirmation message"`
	}
}
