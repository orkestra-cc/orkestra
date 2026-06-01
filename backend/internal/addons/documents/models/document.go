package models

import (
	"time"

	"github.com/orkestra/backend/pkg/sdk/iface"
)

// GeneratedDocument (and SourceType) live in shared/iface/document_types.go
// so the cross-module contract layer doesn't import this addon. Only the
// addon-internal "metadata-without-binary" projection stays here.

// GeneratedDocumentMeta represents document metadata without the binary content.
// Used by list endpoints that should not return PDF bytes.
type GeneratedDocumentMeta struct {
	UUID         string           `json:"id"`
	SourceType   iface.SourceType `json:"sourceType"`
	SourceUUID   string           `json:"sourceUuid"`
	TemplateUUID string           `json:"templateUuid"`
	FileName     string           `json:"fileName"`
	FileSize     int64            `json:"fileSize"`
	ContentType  string           `json:"contentType"`
	GeneratedAt  time.Time        `json:"generatedAt"`
	GeneratedBy  string           `json:"generatedBy"`
	ExpiresAt    *time.Time       `json:"expiresAt,omitempty"`
	CreatedAt    time.Time        `json:"createdAt"`
}

// MetaOf builds a GeneratedDocumentMeta from an iface.GeneratedDocument
// (the body without the binary). Replaces the previous d.ToMeta() method —
// since the underlying type now lives in iface, the projection lives here
// as a free function.
func MetaOf(d *iface.GeneratedDocument) GeneratedDocumentMeta {
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
