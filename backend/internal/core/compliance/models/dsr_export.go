package models

import "time"

// DSRExportsCollection holds one row per stored DSR export. The bytes live in
// the object store under ObjectKey; the row is the index the cleanup job and
// the erasure producer walk.
const DSRExportsCollection = "compliance_dsr_exports"

// DSRExport is a right-of-access export an operator generated on behalf of a
// data subject and stored as a blob that expires at ExpiresAt.
type DSRExport struct {
	UUID     string `bson:"uuid" json:"uuid"`
	UserUUID string `bson:"userUuid" json:"userUuid"`
	// Format is "json" or "csv".
	Format string `bson:"format" json:"format"`
	// RequestedBy is the operator who generated it.
	RequestedBy string `bson:"requestedBy" json:"requestedBy"`
	// Reason is the operator's masked note.
	Reason    string    `bson:"reason" json:"reason"`
	CreatedAt time.Time `bson:"createdAt" json:"createdAt"`
	ExpiresAt time.Time `bson:"expiresAt" json:"expiresAt"`
	SizeBytes int64     `bson:"sizeBytes" json:"sizeBytes"`
	// Producers are the producer keys that answered; ErrorKeys those that failed
	// (keys only, never error text).
	Producers []string `bson:"producers,omitempty" json:"producers,omitempty"`
	ErrorKeys []string `bson:"errorKeys,omitempty" json:"errorKeys,omitempty"`
	// ObjectKey is the key in the exports bucket; never sent to a client.
	ObjectKey string `bson:"objectKey" json:"-"`
}

// DSRExportErasuresCollection holds, per subject, the time the last erasure
// started deleting that subject's stored exports. An export being created
// checks it after it lands, so one that raced an erasure removes itself. The
// row carries a UUID and a time only; the cleanup job drops it after
// export_retention_days.
const DSRExportErasuresCollection = "compliance_dsr_export_erasures"

// DSRExportErasure is the marker row.
type DSRExportErasure struct {
	UserUUID string    `bson:"userUuid"`
	ErasedAt time.Time `bson:"erasedAt"`
}
