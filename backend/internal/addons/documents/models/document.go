package models

import (
	"time"

	"go.mongodb.org/mongo-driver/bson/primitive"
)

// GeneratedDocument represents a PDF document that was generated
type GeneratedDocument struct {
	ID           primitive.ObjectID `bson:"_id,omitempty" json:"-"`
	UUID         string             `bson:"uuid" json:"id"`
	SourceType   SourceType         `bson:"sourceType" json:"sourceType"`     // Type of source (invoice, offer, custom)
	SourceUUID   string             `bson:"sourceUuid" json:"sourceUuid"`     // UUID of source document (e.g., invoice UUID)
	TemplateUUID string             `bson:"templateUuid" json:"templateUuid"` // UUID of template used

	// File information
	FileName    string `bson:"fileName" json:"fileName"`
	FileSize    int64  `bson:"fileSize" json:"fileSize"`       // Size in bytes
	ContentType string `bson:"contentType" json:"contentType"` // MIME type (application/pdf)

	// PDF binary content (stored in MongoDB)
	PDFContent []byte `bson:"pdfContent" json:"-"` // Binary PDF data, not exposed in JSON

	// Generation metadata
	GeneratedAt time.Time  `bson:"generatedAt" json:"generatedAt"`
	GeneratedBy string     `bson:"generatedBy" json:"generatedBy"` // User UUID who generated it
	ExpiresAt   *time.Time `bson:"expiresAt,omitempty" json:"expiresAt,omitempty"`

	// Audit fields
	CreatedAt time.Time  `bson:"createdAt" json:"createdAt"`
	DeletedAt *time.Time `bson:"deletedAt,omitempty" json:"deletedAt,omitempty"`
}

// GeneratedDocumentMeta represents document metadata without the binary content
type GeneratedDocumentMeta struct {
	UUID         string     `json:"id"`
	SourceType   SourceType `json:"sourceType"`
	SourceUUID   string     `json:"sourceUuid"`
	TemplateUUID string     `json:"templateUuid"`
	FileName     string     `json:"fileName"`
	FileSize     int64      `json:"fileSize"`
	ContentType  string     `json:"contentType"`
	GeneratedAt  time.Time  `json:"generatedAt"`
	GeneratedBy  string     `json:"generatedBy"`
	ExpiresAt    *time.Time `json:"expiresAt,omitempty"`
	CreatedAt    time.Time  `json:"createdAt"`
}

// ToMeta converts a GeneratedDocument to GeneratedDocumentMeta (without binary content)
func (d *GeneratedDocument) ToMeta() GeneratedDocumentMeta {
	return GeneratedDocumentMeta{
		UUID:         d.UUID,
		SourceType:   d.SourceType,
		SourceUUID:   d.SourceUUID,
		TemplateUUID: d.TemplateUUID,
		FileName:     d.FileName,
		FileSize:     d.FileSize,
		ContentType:  d.ContentType,
		GeneratedAt:  d.GeneratedAt,
		GeneratedBy:  d.GeneratedBy,
		ExpiresAt:    d.ExpiresAt,
		CreatedAt:    d.CreatedAt,
	}
}
