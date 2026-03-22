package models

// --- Query DTOs ---

type ExecuteQueryRequest struct {
	Body struct {
		Cypher   string                 `json:"cypher" doc:"Cypher query to execute" required:"true"`
		Params   map[string]interface{} `json:"params,omitempty" doc:"Query parameters"`
		Database string                 `json:"database,omitempty" doc:"Target database (uses default if empty)"`
		ReadOnly bool                   `json:"readOnly,omitempty" doc:"Execute as read-only transaction"`
	}
}

type ExecuteQueryResponse struct {
	Body QueryResult
}

// --- Database DTOs ---

type ListDatabasesResponse struct {
	Body struct {
		Databases []DatabaseInfo `json:"databases" doc:"Available databases"`
	}
}

// --- Schema DTOs ---

type GetSchemaRequest struct {
	Database string `query:"database" doc:"Target database (uses default if empty)"`
}

type GetSchemaResponse struct {
	Body SchemaInfo
}

// --- Browse DTOs ---

type BrowseNodesRequest struct {
	Database string   `query:"database" doc:"Target database"`
	Labels   []string `query:"labels" doc:"Filter by labels"`
	Limit    int      `query:"limit" doc:"Maximum nodes to return" default:"50"`
	Skip     int      `query:"skip" doc:"Nodes to skip for pagination" default:"0"`
}

type BrowseNodesResponse struct {
	Body QueryResult
}

type BrowseRelationshipsRequest struct {
	Database string   `query:"database" doc:"Target database"`
	Types    []string `query:"types" doc:"Filter by relationship types"`
	Limit    int      `query:"limit" doc:"Maximum relationships to return" default:"50"`
	Skip     int      `query:"skip" doc:"Relationships to skip for pagination" default:"0"`
}

type BrowseRelationshipsResponse struct {
	Body QueryResult
}

type GetNodeNeighborsRequest struct {
	NodeID   int64  `path:"nodeId" doc:"Node ID to expand"`
	Database string `query:"database" doc:"Target database"`
	Depth    int    `query:"depth" doc:"Expansion depth" default:"1"`
	Limit    int    `query:"limit" doc:"Maximum neighbors to return" default:"50"`
}

type GetNodeNeighborsResponse struct {
	Body GraphData
}

// --- Algorithm DTOs ---

type RunAlgorithmRequestDTO struct {
	Body struct {
		AlgorithmRequest
		Database string `json:"database,omitempty" doc:"Target database"`
	}
}

type RunAlgorithmResponse struct {
	Body QueryResult
}

type ListAlgorithmsResponse struct {
	Body struct {
		Algorithms []AlgorithmInfo `json:"algorithms" doc:"Available graph algorithms"`
	}
}

// --- Vector DTOs ---

type VectorSearchRequestDTO struct {
	Body struct {
		VectorSearchRequest
		Database string `json:"database,omitempty" doc:"Target database"`
	}
}

type VectorSearchResponse struct {
	Body QueryResult
}

type ListVectorIndexesRequest struct {
	Database string `query:"database" doc:"Target database"`
}

type ListVectorIndexesResponse struct {
	Body struct {
		Indexes []VectorIndex `json:"indexes" doc:"Vector indexes"`
	}
}

type CreateVectorIndexRequestDTO struct {
	Body struct {
		CreateVectorIndexRequest
		Database string `json:"database,omitempty" doc:"Target database"`
	}
}

type CreateVectorIndexResponse struct {
	Body struct {
		Message string `json:"message" doc:"Confirmation message"`
	}
}

type DropVectorIndexRequest struct {
	Name     string `path:"name" doc:"Index name"`
	Database string `query:"database" doc:"Target database"`
}

type DropVectorIndexResponse struct {
	Body struct {
		Message string `json:"message" doc:"Confirmation message"`
	}
}

// --- Delete DTOs ---

type DeleteNodeRequest struct {
	NodeID   int64  `path:"nodeId" doc:"ID of the node to delete"`
	Database string `query:"database" doc:"Target database (uses default if empty)"`
}

type DeleteNodeResponse struct {
	Body struct {
		Message              string `json:"message" doc:"Confirmation message"`
		NodesDeleted         int    `json:"nodesDeleted" doc:"Number of nodes deleted"`
		RelationshipsDeleted int    `json:"relationshipsDeleted" doc:"Number of relationships deleted"`
	}
}

type DeleteRelationshipRequest struct {
	RelationshipID int64  `path:"relationshipId" doc:"ID of the relationship to delete"`
	Database       string `query:"database" doc:"Target database (uses default if empty)"`
}

type DeleteRelationshipResponse struct {
	Body struct {
		Message              string `json:"message" doc:"Confirmation message"`
		RelationshipsDeleted int    `json:"relationshipsDeleted" doc:"Number of relationships deleted"`
	}
}

// --- Health DTOs ---

type HealthCheckResponse struct {
	Body struct {
		Status string `json:"status" doc:"Connection status"`
		URI    string `json:"uri" doc:"Graph database URI"`
	}
}
