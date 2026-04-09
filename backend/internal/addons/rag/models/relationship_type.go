package models

import (
	"time"

	"go.mongodb.org/mongo-driver/bson/primitive"
)

// RelationshipTypeConfig defines a graph relationship type and which document categories it applies to.
type RelationshipTypeConfig struct {
	ID          primitive.ObjectID `bson:"_id,omitempty" json:"-"`
	UUID        string             `bson:"uuid" json:"uuid"`
	Name        string             `bson:"name" json:"name"`                 // e.g. "REFERENCES", "SIMILAR_TO"
	Description string             `bson:"description" json:"description"`   // human-readable description
	FromNode    string             `bson:"fromNode" json:"fromNode"`         // e.g. "RagChunk", "RagDocument"
	ToNode      string             `bson:"toNode" json:"toNode"`             // e.g. "RagSection", "RagChunk"
	Properties  []string           `bson:"properties" json:"properties"`     // edge properties, e.g. ["similarity"]
	Categories  map[string]bool    `bson:"categories" json:"categories"`     // {"iso": true, "law": false, ...}
	IsSystem    bool               `bson:"isSystem" json:"isSystem"`         // system rels cannot be deleted
	CreatedAt   time.Time          `bson:"createdAt" json:"createdAt"`
	UpdatedAt   time.Time          `bson:"updatedAt" json:"updatedAt"`
}

// DefaultRelationshipTypes returns the seed data for initial setup.
func DefaultRelationshipTypes() []RelationshipTypeConfig {
	allCats := map[string]bool{"iso": true, "law": true, "regulation": true, "generic": true}

	return []RelationshipTypeConfig{
		// System (structural) — always created, cannot be deleted
		{Name: "HAS_SECTION", Description: "Document contains a top-level section", FromNode: "RagDocument", ToNode: "RagSection", IsSystem: true, Categories: copyMap(allCats)},
		{Name: "CONTAINS", Description: "Parent section contains child section or chunk", FromNode: "RagSection", ToNode: "RagSection/RagChunk", IsSystem: true, Categories: copyMap(allCats)},
		{Name: "NEXT_SECTION", Description: "Sequential ordering between sibling sections", FromNode: "RagSection", ToNode: "RagSection", IsSystem: true, Categories: copyMap(allCats)},
		{Name: "NEXT", Description: "Sequential ordering between chunks", FromNode: "RagChunk", ToNode: "RagChunk", IsSystem: true, Categories: copyMap(allCats)},

		// Non-system — can be toggled per category or deleted
		{Name: "HAS_DEFINITION", Description: "Document defines a term in its definitions section", FromNode: "RagDocument", ToNode: "RagDefinition", IsSystem: false, Categories: copyMap(allCats)},
		{Name: "DEFINES", Description: "A definition term is used in a chunk", FromNode: "RagDefinition", ToNode: "RagChunk", IsSystem: false, Categories: copyMap(allCats)},
		{Name: "REFERENCES", Description: "Cross-reference from a chunk to another section (e.g. 'see 4.1.3')", FromNode: "RagChunk", ToNode: "RagSection", IsSystem: false, Categories: copyMap(allCats), Properties: []string{"referenceText"}},
		{Name: "SIMILAR_TO", Description: "Cosine similarity above threshold between non-adjacent chunks", FromNode: "RagChunk", ToNode: "RagChunk", IsSystem: false, Categories: copyMap(allCats), Properties: []string{"similarity"}},
		{Name: "SUPERSEDES", Description: "A newer document version replaces an older one", FromNode: "RagDocument", ToNode: "RagDocument", IsSystem: false, Categories: copyMap(allCats)},
	}
}

func copyMap(m map[string]bool) map[string]bool {
	c := make(map[string]bool, len(m))
	for k, v := range m {
		c[k] = v
	}
	return c
}
