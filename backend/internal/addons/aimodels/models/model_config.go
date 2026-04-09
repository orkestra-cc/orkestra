package models

import (
	"time"

	"go.mongodb.org/mongo-driver/bson/primitive"
)

// ModelConfig represents an AI model configuration stored in MongoDB
type ModelConfig struct {
	ID               primitive.ObjectID `bson:"_id,omitempty" json:"-"`
	UUID             string             `bson:"uuid" json:"uuid"`
	Name             string             `bson:"name" json:"name"`
	Provider         string             `bson:"provider" json:"provider"`                 // "ollama" | "openai" | "anthropic" | "gemini"
	ProviderCategory string             `bson:"providerCategory" json:"providerCategory"` // "local" | "cloud"
	ModelType        string             `bson:"modelType" json:"modelType"`               // "embedding" | "llm"
	ModelName        string             `bson:"modelName" json:"modelName"`               // e.g. "nomic-embed-text", "gpt-4o"
	BaseURL          string             `bson:"baseUrl,omitempty" json:"baseUrl,omitempty"`
	APIKey           string             `bson:"apiKey,omitempty" json:"-"` // Never exposed in JSON
	Dimensions       int                `bson:"dimensions,omitempty" json:"dimensions,omitempty"`
	Temperature      float64            `bson:"temperature,omitempty" json:"temperature,omitempty"`
	MaxTokens        int                `bson:"maxTokens,omitempty" json:"maxTokens,omitempty"`
	IsDefault        bool               `bson:"isDefault" json:"isDefault"`
	IsActive         bool               `bson:"isActive" json:"isActive"`
	LastTestedAt     *time.Time         `bson:"lastTestedAt,omitempty" json:"lastTestedAt,omitempty"`
	LastTestStatus   string             `bson:"lastTestStatus,omitempty" json:"lastTestStatus,omitempty"` // "ok" | "error" | ""
	CreatedAt        time.Time          `bson:"createdAt" json:"createdAt"`
	UpdatedAt        time.Time          `bson:"updatedAt" json:"updatedAt"`
}

// HasAPIKey returns whether the model has an API key configured (without exposing it)
func (m *ModelConfig) HasAPIKey() bool {
	return m.APIKey != ""
}

// ProviderCategoryFor derives the category from a provider name
func ProviderCategoryFor(provider string) string {
	switch provider {
	case "ollama":
		return "local"
	case "openai":
		// OpenAI provider can be local (llama.cpp, vLLM) or cloud, but we default to "cloud"
		// since when used with a custom baseURL it's typically still a server endpoint
		return "cloud"
	case "anthropic", "gemini":
		return "cloud"
	default:
		return "cloud"
	}
}
