package models

import (
	"errors"
	"strings"
	"time"
)

// Card is a concrete card emitted to a Person. Cards are staff-issued
// assets, not user self-registration — the workflow lives in
// CardService (Issue / Suspend / Reinstate / Revoke / Expire). Each
// transition emits a marketing_activities row (card_issued on emit;
// card_status_changed on every subsequent transition) so the timeline
// preserves a full audit trail even if the card record is later
// hard-deleted as part of a GDPR DSR cascade.
//
// Schema mirrors docs/plans/marketing-addon/schemas/marketing_cards.md.
type Card struct {
	UUID     string `bson:"uuid" json:"uuid"`
	TenantID string `bson:"tenantId" json:"-"`

	CardTypeUUID string `bson:"cardTypeUuid" json:"cardTypeUuid"`

	// Code is the operator-facing unique identifier rendered from
	// CardType.CodeFormat at emit time. Unique per (tenantId, code).
	Code string `bson:"code" json:"code"`

	PersonUUID string `bson:"personUuid" json:"personUuid"`

	// Tier is required when the type's Tiers list is non-empty; the
	// value must appear in Tiers. Optional otherwise.
	Tier string `bson:"tier,omitempty" json:"tier,omitempty"`

	Status CardStatus `bson:"status" json:"status"`

	// Benefits is a per-instance snapshot copied from
	// CardType.DefaultBenefits at emit time. Operators may edit.
	Benefits []string `bson:"benefits,omitempty" json:"benefits,omitempty"`

	Notes string `bson:"notes,omitempty" json:"notes,omitempty"`

	// ExpiresAt is optional. When set and reached, the card
	// expiration scheduler auto-revokes the card with
	// revoke_reason "expired".
	ExpiresAt *time.Time `bson:"expiresAt,omitempty" json:"expiresAt,omitempty"`

	IssuedAt time.Time `bson:"issuedAt" json:"issuedAt"`
	IssuedBy string    `bson:"issuedBy" json:"issuedBy"`

	SuspendedAt   *time.Time `bson:"suspendedAt,omitempty" json:"suspendedAt,omitempty"`
	SuspendedBy   string     `bson:"suspendedBy,omitempty" json:"suspendedBy,omitempty"`
	SuspendReason string     `bson:"suspendReason,omitempty" json:"suspendReason,omitempty"`

	RevokedAt    *time.Time `bson:"revokedAt,omitempty" json:"revokedAt,omitempty"`
	RevokedBy    string     `bson:"revokedBy,omitempty" json:"revokedBy,omitempty"`
	RevokeReason string     `bson:"revokeReason,omitempty" json:"revokeReason,omitempty"`

	UpdatedAt time.Time `bson:"updatedAt" json:"updatedAt"`
}

// ErrInvalidCard signals a malformed Card payload — emitted by
// Validate when required fields are missing or enums are off-list.
var ErrInvalidCard = errors.New("marketing: invalid card")

// Validate enforces the structural invariants captured in the schema
// doc. The tier-against-type-Tiers cross-check lives at the service
// layer (CardService.Issue) because Validate has no view of the
// associated CardType.
func (c *Card) Validate() error {
	if c == nil {
		return ErrInvalidCard
	}
	if strings.TrimSpace(c.CardTypeUUID) == "" {
		return errors.Join(ErrInvalidCard, errors.New("missing cardTypeUuid"))
	}
	if strings.TrimSpace(c.Code) == "" {
		return errors.Join(ErrInvalidCard, errors.New("missing code"))
	}
	if strings.TrimSpace(c.PersonUUID) == "" {
		return errors.Join(ErrInvalidCard, errors.New("missing personUuid"))
	}
	if !IsKnownCardStatus(c.Status) {
		return errors.Join(ErrInvalidCard, errors.New("invalid status"))
	}
	if c.IssuedAt.IsZero() {
		return errors.Join(ErrInvalidCard, errors.New("missing issuedAt"))
	}
	if strings.TrimSpace(c.IssuedBy) == "" {
		return errors.Join(ErrInvalidCard, errors.New("missing issuedBy"))
	}
	return nil
}
