package iface

import (
	"time"

	"go.mongodb.org/mongo-driver/bson/primitive"
)

// Document contract types live here (rather than in addons/documents/models)
// so that the iface package — the cross-module contract layer — does not
// import any addon package. The documents addon imports these from iface
// like every other consumer.

// SourceType identifies what business document a generated PDF was rendered
// from. Producers (billing, sales, custom-document flows) tag each PDF so
// downstream lookups (`GetBySource`, archival) can find it.
type SourceType string

const (
	// SourceTypeInvoice marks a PDF generated from an invoice (FatturaPA).
	SourceTypeInvoice SourceType = "invoice"
	// SourceTypeOffer marks a PDF generated from a quote/offer (preventivo).
	SourceTypeOffer SourceType = "offer"
	// SourceTypeCustom marks a PDF generated from a free-form template.
	SourceTypeCustom SourceType = "custom"
)

// IsValid checks if the source type is one of the recognized values.
func (s SourceType) IsValid() bool {
	switch s {
	case SourceTypeInvoice, SourceTypeOffer, SourceTypeCustom:
		return true
	}
	return false
}

// String returns the string representation.
func (s SourceType) String() string {
	return string(s)
}

// GeneratedDocument represents a PDF document that was generated. Returned
// by PDFProvider.GenerateInvoicePDF and stored in the documents addon's
// `document_outputs` collection.
type GeneratedDocument struct {
	ID           primitive.ObjectID `bson:"_id,omitempty" json:"-"`
	UUID         string             `bson:"uuid" json:"id"`
	SourceType   SourceType         `bson:"sourceType" json:"sourceType"`
	SourceUUID   string             `bson:"sourceUuid" json:"sourceUuid"`
	TemplateUUID string             `bson:"templateUuid" json:"templateUuid"`

	// File information
	FileName    string `bson:"fileName" json:"fileName"`
	FileSize    int64  `bson:"fileSize" json:"fileSize"`
	ContentType string `bson:"contentType" json:"contentType"`

	// PDF binary content (stored in MongoDB)
	PDFContent []byte `bson:"pdfContent" json:"-"`

	// Generation metadata
	GeneratedAt time.Time  `bson:"generatedAt" json:"generatedAt"`
	GeneratedBy string     `bson:"generatedBy" json:"generatedBy"`
	ExpiresAt   *time.Time `bson:"expiresAt,omitempty" json:"expiresAt,omitempty"`

	// Audit fields
	CreatedAt time.Time  `bson:"createdAt" json:"createdAt"`
	DeletedAt *time.Time `bson:"deletedAt,omitempty" json:"deletedAt,omitempty"`
}
