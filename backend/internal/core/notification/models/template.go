package models

import (
	"time"

	"go.mongodb.org/mongo-driver/bson/primitive"
)

// TemplateDoc is a notification template stored in the DB.
// System templates are seeded from Go source constants on Start() with IsSystem=true.
// Admin overrides update the body and flip IsSystem=false; a reset endpoint deletes
// the override and the next Start() reseeds the default.
type TemplateDoc struct {
	ID   primitive.ObjectID `bson:"_id,omitempty" json:"-"`
	UUID string             `bson:"uuid" json:"id"`
	// OwnerTenantID is the tenant that owns this document; "" (or absent on
	// legacy rows) marks a SYSTEM template. Owned documents are the per-tenant
	// templates a consuming module writes through the notification service's
	// owner-scoped template methods and are only reachable from a ctx of the
	// same tenant.
	OwnerTenantID string    `bson:"ownerTenantId,omitempty" json:"ownerTenantId,omitempty"`
	TemplateID    string    `bson:"templateId" json:"templateId"` // stable key, e.g. "auth.verify_email"
	Locale        string    `bson:"locale" json:"locale"`         // "en", "it", ...
	Channel       string    `bson:"channel" json:"channel"`       // "email"
	Subject       string    `bson:"subject" json:"subject"`
	BodyText      string    `bson:"bodyText" json:"bodyText"`
	BodyHTML      string    `bson:"bodyHtml" json:"bodyHtml"`
	Description   string    `bson:"description,omitempty" json:"description,omitempty"`
	Variables     []string  `bson:"variables,omitempty" json:"variables,omitempty"` // documented variable names
	IsSystem      bool      `bson:"isSystem" json:"isSystem"`
	Version       int       `bson:"version" json:"version"`
	CreatedAt     time.Time `bson:"createdAt" json:"createdAt"`
	UpdatedAt     time.Time `bson:"updatedAt" json:"updatedAt"`
}
