package models

import "time"

// BatchJob tracks a submitted LLM batch through its lifecycle
type BatchJob struct {
	UUID        string         `bson:"uuid" json:"uuid"`
	JobUUID     string         `bson:"jobUuid" json:"jobUuid"`         // Parent prospect job
	ModelUUID   string         `bson:"modelUuid" json:"modelUuid"`     // LLM model (for provider reconstruction)
	Provider    string         `bson:"provider" json:"provider"`       // "anthropic", "openai", "gemini"
	BatchID     string         `bson:"batchId" json:"batchId"`         // Provider's batch ID
	Status      string         `bson:"status" json:"status"`           // "submitted", "processing", "completed", "failed"
	RequestMap  map[string]int `bson:"requestMap" json:"requestMap"`   // customID → agent index
	Results     []BatchResultEntry `bson:"results,omitempty" json:"results,omitempty"`
	Error       string         `bson:"error,omitempty" json:"error,omitempty"`
	CreatedAt   time.Time      `bson:"createdAt" json:"createdAt"`
	UpdatedAt   time.Time      `bson:"updatedAt" json:"updatedAt"`
	CompletedAt *time.Time     `bson:"completedAt,omitempty" json:"completedAt,omitempty"`
}

// BatchResultEntry stores one agent's result from a batch
type BatchResultEntry struct {
	CustomID     string `bson:"customId" json:"customId"`
	Text         string `bson:"text" json:"text"`
	InputTokens  int    `bson:"inputTokens" json:"inputTokens"`
	OutputTokens int    `bson:"outputTokens" json:"outputTokens"`
	Error        string `bson:"error,omitempty" json:"error,omitempty"`
}
