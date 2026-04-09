package models

import "time"

// Conversation stores a multi-turn chat session with a project's agent
type Conversation struct {
	UUID        string    `bson:"uuid" json:"uuid"`
	ProjectUUID string    `bson:"projectUuid" json:"projectUuid"`
	UserUUID    string    `bson:"userUuid" json:"userUuid"`
	Persona     string    `bson:"persona" json:"persona"`
	Title       string    `bson:"title,omitempty" json:"title,omitempty"`
	Messages    []Message `bson:"messages" json:"messages"`
	CreatedAt   time.Time `bson:"createdAt" json:"createdAt"`
	UpdatedAt   time.Time `bson:"updatedAt" json:"updatedAt"`
}

// Message represents a single user or assistant turn in a conversation
type Message struct {
	Role      string    `bson:"role" json:"role"` // user, assistant
	Content   string    `bson:"content" json:"content"`
	Sources   []Source  `bson:"sources,omitempty" json:"sources,omitempty"`
	Metadata  MsgMeta   `bson:"metadata,omitempty" json:"metadata,omitempty"`
	CreatedAt time.Time `bson:"createdAt" json:"createdAt"`
}

// Source references a RAG chunk used to generate an answer
type Source struct {
	DocumentUUID     string  `bson:"documentUuid" json:"documentUuid"`
	DocumentTitle    string  `bson:"documentTitle" json:"documentTitle"`
	ChunkText        string  `bson:"chunkText" json:"chunkText"`
	FullPath         string  `bson:"fullPath" json:"fullPath"`
	RequirementLevel string  `bson:"requirementLevel,omitempty" json:"requirementLevel,omitempty"`
	Score            float64 `bson:"score" json:"score"`
}

// MsgMeta holds timing and processing metadata for an assistant message
type MsgMeta struct {
	RAGTimeMs       int64  `bson:"ragTimeMs,omitempty" json:"ragTimeMs,omitempty"`
	ReflectTimeMs   int64  `bson:"reflectTimeMs,omitempty" json:"reflectTimeMs,omitempty"`
	TotalTimeMs     int64  `bson:"totalTimeMs,omitempty" json:"totalTimeMs,omitempty"`
	ChunksRetrieved int    `bson:"chunksRetrieved,omitempty" json:"chunksRetrieved,omitempty"`
	ModelUsed       string `bson:"modelUsed,omitempty" json:"modelUsed,omitempty"`
	// Token usage from Hindsight reflect
	InputTokens  int32 `bson:"inputTokens,omitempty" json:"inputTokens,omitempty"`
	OutputTokens int32 `bson:"outputTokens,omitempty" json:"outputTokens,omitempty"`
	TotalTokens  int32 `bson:"totalTokens,omitempty" json:"totalTokens,omitempty"`
}
