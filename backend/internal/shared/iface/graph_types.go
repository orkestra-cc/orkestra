package iface

// Graph query types live here (rather than in addons/graph/models) so that
// the iface package — the cross-module contract layer — does not import
// any addon package. The graph addon imports these from iface like every
// other consumer; build profiles that omit `addon_graph` no longer keep
// the addon's models package linked just to satisfy this contract.

// GraphNode represents a Memgraph/Neo4j node returned by a Cypher query.
type GraphNode struct {
	ID         int64                  `json:"id" doc:"Internal Neo4j node ID"`
	ElementID  string                 `json:"elementId" doc:"Neo4j element ID"`
	Labels     []string               `json:"labels" doc:"Node labels"`
	Properties map[string]interface{} `json:"properties" doc:"Node properties"`
}

// GraphRelationship represents a Memgraph/Neo4j relationship returned by a Cypher query.
type GraphRelationship struct {
	ID             int64                  `json:"id" doc:"Internal Neo4j relationship ID"`
	ElementID      string                 `json:"elementId" doc:"Neo4j element ID"`
	Type           string                 `json:"type" doc:"Relationship type"`
	StartNodeID    int64                  `json:"startNodeId" doc:"Start node ID"`
	EndNodeID      int64                  `json:"endNodeId" doc:"End node ID"`
	StartElementID string                 `json:"startElementId" doc:"Start node element ID"`
	EndElementID   string                 `json:"endElementId" doc:"End node element ID"`
	Properties     map[string]interface{} `json:"properties" doc:"Relationship properties"`
}

// GraphData contains nodes and relationships extracted from a Cypher result
// for visualization. Populated by the graph repository when query rows yield
// node/relationship/path values.
type GraphData struct {
	Nodes         []GraphNode         `json:"nodes" doc:"Graph nodes"`
	Relationships []GraphRelationship `json:"relationships" doc:"Graph relationships"`
}

// QueryResult represents the result of a Cypher query — the contract returned
// by GraphProvider.ExecuteRead / ExecuteWrite.
type QueryResult struct {
	Columns  []string                 `json:"columns" doc:"Result column names"`
	Rows     []map[string]interface{} `json:"rows" doc:"Result rows"`
	Graph    *GraphData               `json:"graph,omitempty" doc:"Extracted graph data for visualization"`
	Metadata QueryMetadata            `json:"metadata" doc:"Query execution metadata"`
}

// QueryMetadata contains Cypher execution statistics returned alongside
// the query result.
type QueryMetadata struct {
	ExecutionTimeMs      int64 `json:"executionTimeMs" doc:"Query execution time in milliseconds"`
	ResultCount          int   `json:"resultCount" doc:"Number of result rows"`
	NodesCreated         int   `json:"nodesCreated,omitempty" doc:"Nodes created"`
	NodesDeleted         int   `json:"nodesDeleted,omitempty" doc:"Nodes deleted"`
	RelationshipsCreated int   `json:"relationshipsCreated,omitempty" doc:"Relationships created"`
	RelationshipsDeleted int   `json:"relationshipsDeleted,omitempty" doc:"Relationships deleted"`
	PropertiesSet        int   `json:"propertiesSet,omitempty" doc:"Properties set"`
	LabelsAdded          int   `json:"labelsAdded,omitempty" doc:"Labels added"`
	LabelsRemoved        int   `json:"labelsRemoved,omitempty" doc:"Labels removed"`
	ContainsUpdates      bool  `json:"containsUpdates,omitempty" doc:"Whether the query modified data"`
}
