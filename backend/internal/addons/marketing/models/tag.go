package models

import "time"

// Tag is a hierarchical, renamable label applied to a Person or
// Organization. Tags carry materialized paths so prefix-scan queries
// (e.g. "every contact under /Industry/Manufacturing") run with an
// index instead of a graph traversal. The Slug is the stable
// machine-friendly identifier — Name is free to mutate without
// breaking references.
//
// Schema mirrors docs/plans/marketing-addon/schemas/marketing_tags.md.
type Tag struct {
	UUID     string `bson:"uuid" json:"uuid"`
	TenantID string `bson:"tenantId" json:"tenantId"`

	Name        string `bson:"name" json:"name"`
	Slug        string `bson:"slug" json:"slug"`
	Description string `bson:"description,omitempty" json:"description,omitempty"`
	Color       string `bson:"color,omitempty" json:"color,omitempty"`

	// ParentUUID references another marketing_tags row by UUID. Empty
	// string means root.
	ParentUUID string `bson:"parentUuid,omitempty" json:"parentUuid,omitempty"`

	// Path is the materialized lineage, e.g.
	// "/Industry/Manufacturing/Automotive". Rebuilt by the service
	// on parent change for the moved subtree.
	Path string `bson:"path" json:"path"`

	CreatedAt time.Time `bson:"createdAt" json:"createdAt"`
	UpdatedAt time.Time `bson:"updatedAt" json:"updatedAt"`
	CreatedBy string    `bson:"createdBy,omitempty" json:"createdBy,omitempty"`
}
