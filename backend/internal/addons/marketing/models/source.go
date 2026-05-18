package models

import "time"

// Source captures one provenance entry for an Organization or Person.
// The array on each record grows monotonically — every importer run and
// every manual create appends an entry rather than overwriting prior
// ones, so audit, re-import idempotence, and dedup debugging all work
// off a complete history of where the row came from. Design decision
// D06 in docs/plans/marketing-addon/Orkestra_marketing_addon.md.
type ProvenanceSource struct {
	// Importer names the channel that produced this record. The canonical
	// values are "csv", "excel", "odoo", "manual", "form". Future
	// importers append values without breaking existing rows.
	Importer string `bson:"importer" json:"importer"`

	// JobUUID points at the marketing_import_jobs row that produced this
	// source entry. Empty for "manual" / "form" provenances. Stored as
	// the import job's UUID string (not its Mongo ObjectID) so external
	// audit dumps remain stable across re-indexings.
	JobUUID string `bson:"jobUuid,omitempty" json:"jobUuid,omitempty"`

	// ExternalID is the row identity in the source system — CSV row
	// number, Odoo res.partner.id, etc. Used by the importer pipeline
	// to detect re-imports of the same row.
	ExternalID string `bson:"externalId,omitempty" json:"externalId,omitempty"`

	// ImportedAt is when the record was ingested.
	ImportedAt time.Time `bson:"importedAt" json:"importedAt"`

	// RawPayloadRef is an opaque pointer (object-storage key, log line
	// reference, etc.) to the original payload as received from the
	// source, when the deployment elects to archive raw imports. Empty
	// when raw archival is not configured.
	RawPayloadRef string `bson:"rawPayloadRef,omitempty" json:"rawPayloadRef,omitempty"`
}

// EmailEntry models one email address attached to an Organization or
// Person. The schema is slightly richer than Organization needs (which
// only uses Address/Label/Primary) because Person rows also track
// verification + opt-in state per address — collapsing both into one
// type keeps the importer pipeline trivially homogeneous.
type EmailEntry struct {
	Address string `bson:"address" json:"address"`
	Label   string `bson:"label,omitempty" json:"label,omitempty"`
	Primary bool   `bson:"primary,omitempty" json:"primary,omitempty"`

	// Person-only fields. Marshalled with omitempty so Organization
	// rows do not emit useless zero values.
	Verified    bool       `bson:"verified,omitempty" json:"verified,omitempty"`
	OptIn       bool       `bson:"optIn,omitempty" json:"optIn,omitempty"`
	OptInAt     *time.Time `bson:"optInAt,omitempty" json:"optInAt,omitempty"`
	OptInSource string     `bson:"optInSource,omitempty" json:"optInSource,omitempty"`
}

// PhoneEntry models one phone number attached to an Organization or
// Person. Number is stored as written, services responsible for
// normalization (E.164 conversion) live at the importer + handler
// layer rather than in the model.
type PhoneEntry struct {
	Number  string `bson:"number" json:"number"`
	Label   string `bson:"label,omitempty" json:"label,omitempty"`
	Primary bool   `bson:"primary,omitempty" json:"primary,omitempty"`
}

// PostalAddress models one postal address attached to an Organization.
// Person rows do not carry addresses in Phase 1 — they relate to
// organizations via marketing_memberships, and addresses live on the
// organization. Named PostalAddress rather than Address to avoid an
// OpenAPI schema-name collision with the company addon's models.Address;
// the huma schema registry resolves types by unqualified name across
// every addon compiled into the enterprise build.
type PostalAddress struct {
	Street     string `bson:"street,omitempty" json:"street,omitempty"`
	City       string `bson:"city,omitempty" json:"city,omitempty"`
	Province   string `bson:"province,omitempty" json:"province,omitempty"`
	PostalCode string `bson:"postalCode,omitempty" json:"postalCode,omitempty"`
	Country    string `bson:"country,omitempty" json:"country,omitempty"`
	Label      string `bson:"label,omitempty" json:"label,omitempty"`
	Primary    bool   `bson:"primary,omitempty" json:"primary,omitempty"`
}
