package models

import "time"

// Project represents a project with an associated Hindsight memory bank
type Project struct {
	UUID            string `bson:"uuid" json:"uuid"`
	Name            string `bson:"name" json:"name"`
	Description     string `bson:"description" json:"description"`
	HindsightBankID string `bson:"hindsightBankId" json:"hindsightBankId"`

	// Document scoping - individual documents
	DocumentUUIDs []string `bson:"documentUuids" json:"documentUuids"`

	// Document scoping - category/standard filters
	ISOStandards []string `bson:"isoStandards,omitempty" json:"isoStandards,omitempty"`
	Categories   []string `bson:"categories,omitempty" json:"categories,omitempty"` // iso, law, regulation, generic

	// Agent behavior tuning — overrides persona defaults
	Settings *AgentSettings `bson:"settings,omitempty" json:"settings,omitempty"`

	// Personal agent fields — set when this project backs a per-user personal agent
	IsPersonal       bool   `bson:"isPersonal,omitempty" json:"isPersonal,omitempty"`
	PersonalUserUUID string `bson:"personalUserUuid,omitempty" json:"personalUserUuid,omitempty"`

	Status    string    `bson:"status" json:"status"` // active, archived
	CreatedBy string    `bson:"createdBy" json:"createdBy"`
	CreatedAt time.Time `bson:"createdAt" json:"createdAt"`
	UpdatedAt time.Time `bson:"updatedAt" json:"updatedAt"`
}

// AgentSettings holds per-project tuning for agent behavior.
// All fields are optional — unset fields fall back to persona defaults.
type AgentSettings struct {
	// Custom system prompt prepended to every query
	SystemPrompt string `bson:"systemPrompt,omitempty" json:"systemPrompt,omitempty"`

	// Extra directives merged with persona directives
	Directives []string `bson:"directives,omitempty" json:"directives,omitempty"`

	// Disposition overrides (1-5 scale, 0 = use persona default)
	Skepticism int32 `bson:"skepticism,omitempty" json:"skepticism,omitempty"` // 1=trusting, 5=strict to docs
	Literalism int32 `bson:"literalism,omitempty" json:"literalism,omitempty"` // 1=creative, 5=literal
	Empathy    int32 `bson:"empathy,omitempty" json:"empathy,omitempty"`       // 1=detached, 5=helpful/warm

	// Response style
	MaxTokens   int32  `bson:"maxTokens,omitempty" json:"maxTokens,omitempty"`     // 0 = use persona default
	Temperature string `bson:"temperature,omitempty" json:"temperature,omitempty"` // "precise", "balanced", "creative"
	Language    string `bson:"language,omitempty" json:"language,omitempty"`       // e.g. "en", "it" — forces response language
}

const (
	ProjectStatusActive   = "active"
	ProjectStatusArchived = "archived"
)
