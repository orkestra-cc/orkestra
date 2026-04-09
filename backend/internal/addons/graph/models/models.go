package models

// GraphNode represents a Neo4j node
type GraphNode struct {
	ID         int64                  `json:"id" doc:"Internal Neo4j node ID"`
	ElementID  string                 `json:"elementId" doc:"Neo4j element ID"`
	Labels     []string               `json:"labels" doc:"Node labels"`
	Properties map[string]interface{} `json:"properties" doc:"Node properties"`
}

// GraphRelationship represents a Neo4j relationship
type GraphRelationship struct {
	ID            int64                  `json:"id" doc:"Internal Neo4j relationship ID"`
	ElementID     string                 `json:"elementId" doc:"Neo4j element ID"`
	Type          string                 `json:"type" doc:"Relationship type"`
	StartNodeID   int64                  `json:"startNodeId" doc:"Start node ID"`
	EndNodeID     int64                  `json:"endNodeId" doc:"End node ID"`
	StartElementID string               `json:"startElementId" doc:"Start node element ID"`
	EndElementID   string               `json:"endElementId" doc:"End node element ID"`
	Properties    map[string]interface{} `json:"properties" doc:"Relationship properties"`
}

// GraphData contains nodes and relationships for graph visualization
type GraphData struct {
	Nodes         []GraphNode         `json:"nodes" doc:"Graph nodes"`
	Relationships []GraphRelationship `json:"relationships" doc:"Graph relationships"`
}

// QueryResult represents the result of a Cypher query
type QueryResult struct {
	Columns   []string                 `json:"columns" doc:"Result column names"`
	Rows      []map[string]interface{} `json:"rows" doc:"Result rows"`
	Graph     *GraphData               `json:"graph,omitempty" doc:"Extracted graph data for visualization"`
	Metadata  QueryMetadata            `json:"metadata" doc:"Query execution metadata"`
}

// QueryMetadata contains execution statistics
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

// DatabaseInfo represents a Neo4j database
type DatabaseInfo struct {
	Name          string `json:"name" doc:"Database name"`
	Address       string `json:"address,omitempty" doc:"Database address"`
	CurrentStatus string `json:"currentStatus" doc:"Current status (online, offline, etc.)"`
	Default       bool   `json:"default" doc:"Whether this is the default database"`
	Home          bool   `json:"home" doc:"Whether this is the home database"`
}

// SchemaInfo represents the schema of a Neo4j database
type SchemaInfo struct {
	Labels            []LabelInfo      `json:"labels" doc:"Node labels with counts"`
	RelationshipTypes []RelTypeInfo    `json:"relationshipTypes" doc:"Relationship types with counts"`
	Indexes           []IndexInfo      `json:"indexes" doc:"Database indexes"`
	Constraints       []ConstraintInfo `json:"constraints" doc:"Database constraints"`
	NodeCount         int64            `json:"nodeCount" doc:"Total node count"`
	RelationshipCount int64            `json:"relationshipCount" doc:"Total relationship count"`
}

// LabelInfo represents a node label and its properties
type LabelInfo struct {
	Name       string   `json:"name" doc:"Label name"`
	Count      int64    `json:"count" doc:"Number of nodes with this label"`
	Properties []string `json:"properties" doc:"Properties on nodes with this label"`
}

// RelTypeInfo represents a relationship type and its properties
type RelTypeInfo struct {
	Name       string   `json:"name" doc:"Relationship type name"`
	Count      int64    `json:"count" doc:"Number of relationships of this type"`
	Properties []string `json:"properties" doc:"Properties on this relationship type"`
}

// IndexInfo represents a database index
type IndexInfo struct {
	Name       string   `json:"name" doc:"Index name"`
	Type       string   `json:"type" doc:"Index type (BTREE, FULLTEXT, VECTOR, etc.)"`
	EntityType string   `json:"entityType" doc:"NODE or RELATIONSHIP"`
	Labels     []string `json:"labels" doc:"Labels or types the index applies to"`
	Properties []string `json:"properties" doc:"Indexed properties"`
	State      string   `json:"state" doc:"Index state (ONLINE, POPULATING, etc.)"`
}

// ConstraintInfo represents a database constraint
type ConstraintInfo struct {
	Name       string   `json:"name" doc:"Constraint name"`
	Type       string   `json:"type" doc:"Constraint type (UNIQUENESS, EXISTS, etc.)"`
	EntityType string   `json:"entityType" doc:"NODE or RELATIONSHIP"`
	Labels     []string `json:"labels" doc:"Labels or types the constraint applies to"`
	Properties []string `json:"properties" doc:"Constrained properties"`
}
