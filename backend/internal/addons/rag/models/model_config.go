package models

import (
	"time"

	"go.mongodb.org/mongo-driver/bson/primitive"
)

// ModelConfig represents an AI model configuration stored in MongoDB
type ModelConfig struct {
	ID          primitive.ObjectID `bson:"_id,omitempty" json:"-"`
	UUID        string             `bson:"uuid" json:"uuid"`
	Name        string             `bson:"name" json:"name"`
	Provider    string             `bson:"provider" json:"provider"`     // "ollama" | "openai"
	ModelType   string             `bson:"modelType" json:"modelType"`   // "embedding" | "llm"
	ModelName   string             `bson:"modelName" json:"modelName"`   // e.g. "nomic-embed-text", "gpt-4o"
	BaseURL     string             `bson:"baseUrl,omitempty" json:"baseUrl,omitempty"`
	APIKey      string             `bson:"apiKey,omitempty" json:"-"` // Never exposed in JSON
	Dimensions  int                `bson:"dimensions,omitempty" json:"dimensions,omitempty"`
	Temperature float64            `bson:"temperature,omitempty" json:"temperature,omitempty"`
	MaxTokens   int                `bson:"maxTokens,omitempty" json:"maxTokens,omitempty"`
	IsDefault   bool               `bson:"isDefault" json:"isDefault"`
	IsActive    bool               `bson:"isActive" json:"isActive"`
	CreatedAt   time.Time          `bson:"createdAt" json:"createdAt"`
	UpdatedAt   time.Time          `bson:"updatedAt" json:"updatedAt"`
}

// HasAPIKey returns whether the model has an API key configured (without exposing it)
func (m *ModelConfig) HasAPIKey() bool {
	return m.APIKey != ""
}
