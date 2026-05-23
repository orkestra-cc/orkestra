package models

import "time"

// OrganizationKind enumerates the institution flavours an organization
// can have. The list is intentionally short — it captures gross category
// for filtering, with finer-grained classification handled via tags and
// custom fields.
type OrganizationKind string

const (
	OrgKindCompany              OrganizationKind = "company"
	OrgKindPublicAdministration OrganizationKind = "public_administration"
	OrgKindFoundation           OrganizationKind = "foundation"
	OrgKindAssociation          OrganizationKind = "association"
	OrgKindOther                OrganizationKind = "other"
)

// Organization is the anagrafica record for an institution (company,
// PA, foundation, association, ...). Multi-tenant scoped on TenantID
// per Orkestra's two-tier tenancy. Schema mirrors the per-collection
// design at docs/plans/marketing-addon/schemas/marketing_organizations.md
// — adjust both together when fields evolve.
//
// Identity convention: the externally-visible primary key is UUID
// (stable string), not the auto-generated Mongo _id ObjectID. This
// matches every other Orkestra module — references between marketing
// collections also carry UUIDs, not ObjectIDs. The schema doc shows
// ObjectIDs in examples; that's documentation shorthand for "the Mongo
// PK", which here happens to be UUID.
type Organization struct {
	UUID     string `bson:"uuid" json:"uuid"`
	TenantID string `bson:"tenantId" json:"tenantId"`

	LegalName   string `bson:"legalName" json:"legalName"`
	DisplayName string `bson:"displayName,omitempty" json:"displayName,omitempty"`

	// VAT and TaxCode are stored normalized (uppercase, whitespace
	// stripped). The repository's Create / Update entry points apply
	// normalization; do not rely on a normalization step elsewhere.
	VAT     string `bson:"vat,omitempty" json:"vat,omitempty"`
	TaxCode string `bson:"taxCode,omitempty" json:"taxCode,omitempty"`

	Kind OrganizationKind `bson:"kind" json:"kind"`

	Website   string          `bson:"website,omitempty" json:"website,omitempty"`
	Emails    []EmailEntry    `bson:"emails,omitempty" json:"emails,omitempty"`
	Phones    []PhoneEntry    `bson:"phones,omitempty" json:"phones,omitempty"`
	Addresses []PostalAddress `bson:"addresses,omitempty" json:"addresses,omitempty"`

	// Tags references marketing_tags by UUID. Set-union merged on
	// import auto-merge (decision §5.6).
	Tags []string `bson:"tags,omitempty" json:"tags,omitempty"`

	// CustomFields is the per-tenant typed bag validated against
	// marketing_custom_field_schemas with target = "organizations".
	CustomFields map[string]any `bson:"customFields,omitempty" json:"customFields,omitempty"`

	Sources []ProvenanceSource `bson:"sources,omitempty" json:"sources,omitempty"`
	Notes   string             `bson:"notes,omitempty" json:"notes,omitempty"`

	// LegalNameNormalized is the lower-cased + whitespace-collapsed
	// form of LegalName, populated by the repository on every Create /
	// Update. Backs the soft-match dedup pass that runs after VAT /
	// TaxCode strict-miss; the importer never auto-merges on this
	// signal — every soft-match hit parks in marketing_conflict_reviews.
	LegalNameNormalized string `bson:"legalNameNormalized,omitempty" json:"-"`

	CreatedAt time.Time `bson:"createdAt" json:"createdAt"`
	UpdatedAt time.Time `bson:"updatedAt" json:"updatedAt"`
	CreatedBy string    `bson:"createdBy,omitempty" json:"createdBy,omitempty"`
	UpdatedBy string    `bson:"updatedBy,omitempty" json:"updatedBy,omitempty"`
}
