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

	// ReviewAt is when the hold is due for a review; it never releases the hold.
	ReviewAt *time.Time `bson:"reviewAt,omitempty" json:"reviewAt,omitempty"`
	// ExpiresAt releases the hold on its own once reached (the expiry job
	// records it; the hold stops counting at this instant either way).
	ExpiresAt *time.Time `bson:"expiresAt,omitempty" json:"expiresAt,omitempty"`
	// Notes are append-only operator notes (masked), never edited.
	Notes     []LegalHoldNote `bson:"notes,omitempty" json:"notes,omitempty"`
	UpdatedAt *time.Time      `bson:"updatedAt,omitempty" json:"updatedAt,omitempty"`
	// ReviewDue is computed on read: an active hold whose ReviewAt has passed.
	ReviewDue bool `bson:"-" json:"reviewDue"`
	// Expired is computed on read: still active in the row but past its
	// expiry, so no longer blocking; the expiry job has not released it yet.
	Expired bool `bson:"-" json:"expired"`
}

// LegalHoldExpiryActor is the ReleasedBy value of a hold the expiry job released.
const LegalHoldExpiryActor = "system:expiry"

// LegalHoldNote is one operator note on a hold.
type LegalHoldNote struct {
	ID   string    `bson:"id" json:"id"`
	By   string    `bson:"by" json:"by"`
	At   time.Time `bson:"at" json:"at"`
	Text string    `bson:"text" json:"text"`
}
