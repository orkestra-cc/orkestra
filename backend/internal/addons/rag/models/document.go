package models

import (
	"time"

	"go.mongodb.org/mongo-driver/bson/primitive"
)

// RagDocument represents an ingested document stored in MongoDB
type RagDocument struct {
	ID               primitive.ObjectID `bson:"_id,omitempty" json:"-"`
	UUID             string             `bson:"uuid" json:"uuid"`
	Title            string             `bson:"title" json:"title"`
	FileName         string             `bson:"fileName" json:"fileName"`
	FileSize         int64              `bson:"fileSize" json:"fileSize"`
	ISOStandard      string             `bson:"isoStandard,omitempty" json:"isoStandard,omitempty"`
	Version          string             `bson:"version,omitempty" json:"version,omitempty"`
	DocumentCategory string             `bson:"documentCategory,omitempty" json:"documentCategory,omitempty"` // "iso", "law", "regulation", "generic"
	DocType          string             `bson:"docType" json:"docType"`                                       // "pdf", "text"
	Status           string             `bson:"status" json:"status"`                                         // pending, processing, completed, failed
	Error            string             `bson:"error,omitempty" json:"error,omitempty"`
	ChunkCount       int                `bson:"chunkCount" json:"chunkCount"`
	ModelUUID        string             `bson:"modelUuid" json:"modelUuid"`
	LLMModelName     string             `bson:"llmModelName,omitempty" json:"llmModelName,omitempty"`
	ChunkSize        int                `bson:"chunkSize" json:"chunkSize"`
	ChunkOverlap     int                `bson:"chunkOverlap" json:"chunkOverlap"`
	CreatedAt        time.Time          `bson:"createdAt" json:"createdAt"`
	UpdatedAt        time.Time          `bson:"updatedAt" json:"updatedAt"`
	CompletedAt      *time.Time         `bson:"completedAt,omitempty" json:"completedAt,omitempty"`
}
