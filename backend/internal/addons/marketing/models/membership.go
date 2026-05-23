package models

import "time"

// Membership relates a Person to an Organization with role + period
// metadata. The data model is intentionally history-preserving: when a
// person changes role or company, the existing membership is closed
// (Until set, Active=false) rather than mutated, and a new membership
// row is created. Activity rollups over historical roles depend on
// this convention.
//
// Schema mirrors docs/plans/marketing-addon/schemas/marketing_memberships.md.
//
// Invariant: at most one Active=true membership per (tenantId,
// personId, orgId) tuple, and exactly one Active+Primary=true
// membership per (tenantId, personId). The unique-partial index
// expression that would enforce these at the DB level is not yet
// expressible through CollectionSpec/IndexSpec — the SDK does not
// surface PartialFilterExpression — so the service layer enforces
// them via repository helpers. The plain compound indexes declared
// in module.go::Collections() back the lookup paths.
type Membership struct {
	UUID     string `bson:"uuid" json:"uuid"`
	TenantID string `bson:"tenantId" json:"tenantId"`

	PersonUUID string `bson:"personUuid" json:"personUuid"`
	OrgUUID    string `bson:"orgUuid" json:"orgUuid"`

	Role       string `bson:"role,omitempty" json:"role,omitempty"`
	Department string `bson:"department,omitempty" json:"department,omitempty"`

	Since *time.Time `bson:"since,omitempty" json:"since,omitempty"`
	Until *time.Time `bson:"until,omitempty" json:"until,omitempty"`

	// Active is a convenience flag — equivalent to
	// (Until == nil || Until > now). Maintained by the service when
	// closing a membership.
	Active  bool `bson:"active" json:"active"`
	Primary bool `bson:"primary" json:"primary"`

	Notes string `bson:"notes,omitempty" json:"notes,omitempty"`

	CreatedAt time.Time `bson:"createdAt" json:"createdAt"`
	UpdatedAt time.Time `bson:"updatedAt" json:"updatedAt"`
	CreatedBy string    `bson:"createdBy,omitempty" json:"createdBy,omitempty"`
}
