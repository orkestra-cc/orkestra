package models

import "time"

// SalesSettings stores per-user configuration for the Sales Intelligence module
type SalesSettings struct {
	UUID        string    `bson:"uuid" json:"uuid"`
	UserUUID    string    `bson:"userUuid" json:"userUuid"`
	ModelUUID   string    `bson:"modelUuid,omitempty" json:"modelUuid,omitempty"`     // Selected LLM model UUID (empty = system default)
	Temperature float64   `bson:"temperature,omitempty" json:"temperature,omitempty"` // 0.0-1.0 (0 = use model default)
	MaxTokens   int       `bson:"maxTokens,omitempty" json:"maxTokens,omitempty"`     // 0 = use model default
	Locale      string    `bson:"locale,omitempty" json:"locale,omitempty"`           // "it" or "en" (empty = config default)
	BatchMode   bool      `bson:"batchMode,omitempty" json:"batchMode,omitempty"`     // Use batch API for prospect pipeline (50% savings)
	CreatedAt   time.Time `bson:"createdAt" json:"createdAt"`
	UpdatedAt   time.Time `bson:"updatedAt" json:"updatedAt"`
}

// --- DTOs ---

// GetSalesSettingsRequest is the Huma input for GET /v1/sales/settings
type GetSalesSettingsRequest struct{}

// GetSalesSettingsResponse is the Huma output for GET /v1/sales/settings
type GetSalesSettingsResponse struct {
	Body SalesSettings
}

// SalesSettingsUpdateBody is the request body for updating sales settings
type SalesSettingsUpdateBody struct {
	ModelUUID   *string  `json:"modelUuid,omitempty" doc:"LLM model UUID from /v1/ai/models (empty string = system default)"`
	Temperature *float64 `json:"temperature,omitempty" doc:"LLM temperature 0.0-1.0 (0 = model default)" minimum:"0" maximum:"1"`
	MaxTokens   *int     `json:"maxTokens,omitempty" doc:"Max output tokens (0 = model default)" minimum:"0" maximum:"128000"`
	Locale      *string  `json:"locale,omitempty" doc:"Default locale for prompts" enum:"it,en,"`
	BatchMode   *bool    `json:"batchMode,omitempty" doc:"Use batch API for prospect pipeline (50% cost savings, slower). Cloud LLMs only."`
}

// UpdateSalesSettingsRequest is the Huma input for PATCH /v1/sales/settings
type UpdateSalesSettingsRequest struct {
	Body SalesSettingsUpdateBody
}

// UpdateSalesSettingsResponse is the Huma output for PATCH /v1/sales/settings
type UpdateSalesSettingsResponse struct {
	Body SalesSettings
}
