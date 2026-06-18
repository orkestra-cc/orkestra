package models

import "time"

// LegalHoldsCollection backs active/released legal holds that block GDPR
// erasure for a data subject (litigation / investigation hold).
const LegalHoldsCollection = "compliance_legal_holds"

// LegalHold records a litigation/investigation hold on a data subject. While
// any hold is active for a subject, the DSR erase pipeline and the retention
// auto-cleanup job refuse to erase that subject.
type LegalHold struct {
	UUID          string     `bson:"uuid" json:"uuid"`
	UserUUID      string     `bson:"userUuid" json:"userUuid"`
	TenantID      string     `bson:"tenantId,omitempty" json:"tenantId,omitempty"`
	Reason        string     `bson:"reason" json:"reason"`
	CaseRef       string     `bson:"caseRef,omitempty" json:"caseRef,omitempty"`
	PlacedBy      string     `bson:"placedBy" json:"placedBy"`
	PlacedAt      time.Time  `bson:"placedAt" json:"placedAt"`
	Active        bool       `bson:"active" json:"active"`
	ReleasedAt    *time.Time `bson:"releasedAt,omitempty" json:"releasedAt,omitempty"`
	ReleasedBy    string     `bson:"releasedBy,omitempty" json:"releasedBy,omitempty"`
	ReleaseReason string     `bson:"releaseReason,omitempty" json:"releaseReason,omitempty"`
}
