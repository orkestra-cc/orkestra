package models

import "time"

// ConsentBasis tracks the GDPR legal basis on which marketing
// activity may be conducted toward a Person. The two values cover the
// dominant cases — additional bases (legal_obligation, vital_interests,
// public_task) can be added when the matching use case arrives.
type ConsentBasis string

const (
	ConsentBasisConsent            ConsentBasis = "consent"
	ConsentBasisLegitimateInterest ConsentBasis = "legitimate_interest"
)

// ConsentRecord captures the state of one consent channel
// (marketing_email, marketing_phone, profiling, …). RevokedAt is nil
// while the consent is active; setting it ends consent.
type ConsentRecord struct {
	Given     bool         `bson:"given" json:"given"`
	Basis     ConsentBasis `bson:"basis,omitempty" json:"basis,omitempty"`
	GivenAt   *time.Time   `bson:"givenAt,omitempty" json:"givenAt,omitempty"`
	Source    string       `bson:"source,omitempty" json:"source,omitempty"`
	RevokedAt *time.Time   `bson:"revokedAt,omitempty" json:"revokedAt,omitempty"`
}

// Consent is the typed consent bag attached to a Person. The map-typed
// alternative would be more open-ended, but the GDPR audit trail is
// easier to validate when the channels are enumerated. New channels
// arrive as new fields (additive change).
type Consent struct {
	MarketingEmail *ConsentRecord `bson:"marketingEmail,omitempty" json:"marketingEmail,omitempty"`
	MarketingPhone *ConsentRecord `bson:"marketingPhone,omitempty" json:"marketingPhone,omitempty"`
	Profiling      *ConsentRecord `bson:"profiling,omitempty" json:"profiling,omitempty"`
}

// Person is the anagrafica record for a physical individual. Multi-
// tenant scoped on TenantID. A Person can have N memberships to N
// organizations (via marketing_memberships), at most one of which is
// flagged primary for activity-org denormalization. Schema mirrors
// docs/plans/marketing-addon/schemas/marketing_persons.md.
//
// Identity-minimum constraint (per the schema doc): at least one of
// (FirstName + LastName) or one primary EmailEntry.Address must be
// non-empty. Enforced at the service layer; the repository takes the
// row as-is so importers can stage incomplete rows for review.
type Person struct {
	UUID     string `bson:"uuid" json:"uuid"`
	TenantID string `bson:"tenantId" json:"tenantId"`

	FirstName string `bson:"firstName,omitempty" json:"firstName,omitempty"`
	LastName  string `bson:"lastName,omitempty" json:"lastName,omitempty"`
	Title     string `bson:"title,omitempty" json:"title,omitempty"`

	// Emails are stored with the Address lowercased and trimmed.
	// Normalization happens at the repository entry point; do not rely
	// on a normalization step elsewhere.
	Emails []EmailEntry `bson:"emails,omitempty" json:"emails,omitempty"`
	Phones []PhoneEntry `bson:"phones,omitempty" json:"phones,omitempty"`

	// Language is a BCP-47 tag (e.g. "en", "it", "it-IT").
	Language  string     `bson:"language,omitempty" json:"language,omitempty"`
	Birthdate *time.Time `bson:"birthdate,omitempty" json:"birthdate,omitempty"`

	Tags         []string       `bson:"tags,omitempty" json:"tags,omitempty"`
	CustomFields map[string]any `bson:"customFields,omitempty" json:"customFields,omitempty"`
	Consent      *Consent       `bson:"consent,omitempty" json:"consent,omitempty"`

	// ActiveCardUUIDs is denormalized — the source of truth is
	// marketing_cards with status="active". Always written by the
	// card-issue/suspend/revoke pipeline (Phase 4); empty until then.
	// Declared from Phase 1 so the index lands at boot and Phase 4
	// does not need a write-blocking rebuild.
	ActiveCardUUIDs []string `bson:"activeCardUuids,omitempty" json:"activeCardUuids,omitempty"`

	Sources []ProvenanceSource `bson:"sources,omitempty" json:"sources,omitempty"`
	Notes   string             `bson:"notes,omitempty" json:"notes,omitempty"`

	CreatedAt time.Time `bson:"createdAt" json:"createdAt"`
	UpdatedAt time.Time `bson:"updatedAt" json:"updatedAt"`
	CreatedBy string    `bson:"createdBy,omitempty" json:"createdBy,omitempty"`
	UpdatedBy string    `bson:"updatedBy,omitempty" json:"updatedBy,omitempty"`
}

// HasMinimumIdentity reports whether the Person carries the minimum
// identity bits required by the schema's service-layer constraint:
// either a non-empty (first OR last) name, or at least one email with
// Primary=true.
func (p *Person) HasMinimumIdentity() bool {
	if p.FirstName != "" || p.LastName != "" {
		return true
	}
	for _, e := range p.Emails {
		if e.Primary && e.Address != "" {
			return true
		}
	}
	return false
}
