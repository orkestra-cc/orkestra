package models

// Graph query types (GraphNode, GraphRelationship, GraphData, QueryResult,
// QueryMetadata) live in shared/iface so the iface contract layer doesn't
// have to import this addon package — see iface/graph_types.go. Schema
// introspection types stay here because they are graph-internal and never
// cross the iface boundary.

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
