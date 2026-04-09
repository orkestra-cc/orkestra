package providers

// ModelConfig contains the fields needed to create a provider.
type ModelConfig struct {
	Provider    string // "ollama" | "openai" | "anthropic" | "gemini"
	ModelType   string // "embedding" | "llm"
	ModelName   string
	BaseURL     string
	APIKey      string
	Dimensions  int
	Temperature float64
	MaxTokens   int
}

// RemoteModel represents a model available on a remote provider
type RemoteModel struct {
	ID           string
	OwnedBy      string
	Capabilities string // comma-separated capabilities (e.g. "embedContent,generateContent")
}
