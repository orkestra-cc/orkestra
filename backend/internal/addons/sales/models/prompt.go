package models

import "time"

// SalesPrompt is an editable prompt template used by the sales intelligence pipeline
type SalesPrompt struct {
	UUID        string    `bson:"uuid" json:"uuid"`
	Category    string    `bson:"category" json:"category"`       // "agents" or "skills"
	Name        string    `bson:"name" json:"name"`               // e.g. "company-research", "qualify"
	DisplayName string    `bson:"displayName" json:"displayName"` // e.g. "Company Research"
	Description string    `bson:"description" json:"description"` // Short description of what the prompt does
	Content     string    `bson:"content" json:"content"`         // Markdown template with Go {{.Var}} placeholders
	IsCustom    bool      `bson:"isCustom" json:"isCustom"`       // true if user has edited the default
	CreatedAt   time.Time `bson:"createdAt" json:"createdAt"`
	UpdatedAt   time.Time `bson:"updatedAt" json:"updatedAt"`
}

// --- DTOs ---

type ListPromptsRequest struct {
	Category string `query:"category,omitempty" doc:"Filter by category: agents or skills"`
}

type ListPromptsResponse struct {
	Body struct {
		Prompts []SalesPrompt `json:"prompts"`
	}
}

type GetPromptRequest struct {
	UUID string `path:"uuid" doc:"Prompt UUID"`
}

type GetPromptResponse struct {
	Body SalesPrompt
}

type UpdatePromptRequest struct {
	UUID string `path:"uuid" doc:"Prompt UUID"`
	Body struct {
		Content     string `json:"content" doc:"Markdown template content" minLength:"1"`
		DisplayName string `json:"displayName,omitempty" doc:"Display name"`
		Description string `json:"description,omitempty" doc:"Short description"`
	}
}

type UpdatePromptResponse struct {
	Body SalesPrompt
}

type ResetPromptRequest struct {
	UUID string `path:"uuid" doc:"Prompt UUID"`
}

type ResetPromptResponse struct {
	Body SalesPrompt
}
