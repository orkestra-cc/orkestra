package models

// AlgorithmRequest represents a request to run a graph algorithm
type AlgorithmRequest struct {
	Algorithm string                 `json:"algorithm" doc:"Algorithm name (e.g., pageRank, louvain, wcc)"`
	Config    map[string]interface{} `json:"config,omitempty" doc:"Algorithm-specific configuration"`
}

// AlgorithmInfo describes a graph algorithm available via MAGE
type AlgorithmInfo struct {
	Name        string `json:"name" doc:"Algorithm name"`
	Category    string `json:"category" doc:"Category: centrality, community, similarity, pathfinding, embedding"`
	Procedure   string `json:"procedure" doc:"MAGE procedure name"`
	Description string `json:"description" doc:"Brief description"`
}

// VectorIndex represents a vector index in the graph database
type VectorIndex struct {
	Name       string `json:"name" doc:"Index name"`
	Label      string `json:"label" doc:"Node label"`
	Property   string `json:"property" doc:"Indexed property"`
	Dimensions int    `json:"dimensions" doc:"Vector dimensions"`
	Similarity string `json:"similarity" doc:"Similarity metric"`
	State      string `json:"state" doc:"Index state"`
}

// VectorSearchRequest contains parameters for vector similarity search
type VectorSearchRequest struct {
	IndexName   string    `json:"indexName" doc:"Name of the vector index"`
	QueryVector []float64 `json:"queryVector" doc:"Query vector for similarity search"`
	TopK        int       `json:"topK,omitempty" doc:"Number of results to return" default:"10"`
	MinScore    float64   `json:"minScore,omitempty" doc:"Minimum similarity score threshold"`
}

// CreateVectorIndexRequest contains parameters for creating a vector index
type CreateVectorIndexRequest struct {
	Name       string `json:"name" doc:"Index name"`
	Label      string `json:"label" doc:"Node label to index"`
	Property   string `json:"property" doc:"Property containing vectors"`
	Dimensions int    `json:"dimensions" doc:"Vector dimensions"`
	Capacity   int    `json:"capacity,omitempty" doc:"Maximum number of vectors (default: 10000)" default:"10000"`
	Similarity string `json:"similarity" doc:"Similarity metric: cos, l2sq, ip" enum:"cos,l2sq,ip" default:"cos"`
}
