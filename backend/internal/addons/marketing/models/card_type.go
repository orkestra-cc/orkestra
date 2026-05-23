package models

import (
	"errors"
	"strings"
	"time"
)

// CardType is a per-tenant template that defines a kind of card. A
// CardType declares the display name shown in the operator UI, the
// list of tiers a card of this type may carry, the code-format
// template that drives unique-code generation, the default benefits
// snapshotted onto new instances, and whether one Person may hold
// multiple active cards of this type simultaneously.
//
// Schema mirrors docs/plans/marketing-addon/schemas/marketing_card_types.md.
type CardType struct {
	UUID     string `bson:"uuid" json:"uuid"`
	TenantID string `bson:"tenantId" json:"-"`

	// Key is the stable machine-friendly slug used for programmatic
	// reference (e.g. "premium_member"). Lowercase, unique per tenant.
	// DisplayName is the human-readable label the operator types
	// during create — it is free to mutate without breaking card
	// instances (those reference the type by UUID, not by key or
	// display name).
	Key         string `bson:"key" json:"key"`
	DisplayName string `bson:"displayName" json:"displayName"`
	Description string `bson:"description,omitempty" json:"description,omitempty"`

	// Tiers enumerates the optional tier strings a card of this type
	// may carry (e.g. ["bronze", "silver", "gold"]). Empty slice means
	// the type has no tier dimension; CardService rejects an Issue
	// that carries a tier when Tiers is empty, or one whose tier is
	// not in the list. Case is preserved — "Gold" and "gold" are
	// distinct values.
	Tiers []string `bson:"tiers,omitempty" json:"tiers,omitempty"`

	// CodeFormat is the template the card-code generator renders at
	// emit time. Grammar:
	//
	//   {YYYY} {YY} {MM} {DD}  — components of the current UTC date
	//   {seq:N}                — zero-padded counter, per (tenant, type)
	//   {rand:N}               — N Crockford-Base32 random characters
	//
	// Literal text between placeholders passes through verbatim. Parse
	// + validate via services.ParseCardCodeFormat at create / update;
	// the unique index on (tenantId, code) is the fail-safe against
	// collisions.
	CodeFormat string `bson:"codeFormat" json:"codeFormat"`

	// DefaultBenefits are copied onto Card.Benefits at emit time.
	// Mutations here do NOT propagate to existing cards.
	DefaultBenefits []string `bson:"defaultBenefits,omitempty" json:"defaultBenefits,omitempty"`

	// AllowMultiplePerPerson opens the door to a Person holding more
	// than one active card of this type at the same time. When false
	// (the default), CardService rejects an Issue whose
	// (personUuid, cardTypeUuid) pair already has a non-revoked card.
	AllowMultiplePerPerson bool `bson:"allowMultiplePerPerson" json:"allowMultiplePerPerson"`

	// Active disables future issuance of this type without affecting
	// the cards already in flight. Flipping to false leaves existing
	// instances untouched; flipping back to true re-opens issuance.
	Active bool `bson:"active" json:"active"`

	CreatedAt time.Time `bson:"createdAt" json:"createdAt"`
	UpdatedAt time.Time `bson:"updatedAt" json:"updatedAt"`
	CreatedBy string    `bson:"createdBy,omitempty" json:"createdBy,omitempty"`
	UpdatedBy string    `bson:"updatedBy,omitempty" json:"updatedBy,omitempty"`
}

// ErrInvalidCardType signals a malformed CardType payload. Repository
// Create / Update call Validate at the boundary; the handler also
// surfaces Validate errors as 400 bad-request.
var ErrInvalidCardType = errors.New("marketing: invalid card type")

// Validate enforces the structural invariants captured in the schema
// doc. The code-format grammar itself is enforced by the parser in
// services.ParseCardCodeFormat — Validate here only checks presence.
func (c *CardType) Validate() error {
	if c == nil {
		return ErrInvalidCardType
	}
	if strings.TrimSpace(c.Key) == "" {
		return errors.Join(ErrInvalidCardType, errors.New("missing key"))
	}
	if strings.TrimSpace(c.DisplayName) == "" {
		return errors.Join(ErrInvalidCardType, errors.New("missing displayName"))
	}
	if strings.TrimSpace(c.CodeFormat) == "" {
		return errors.Join(ErrInvalidCardType, errors.New("missing codeFormat"))
	}
	// Key must be a slug: lowercase letters, digits, underscores. We
	// accept the same character set the tag slug helper produces, so
	// operators can paste a familiar form without a re-normalisation
	// surprise.
	for _, r := range c.Key {
		switch {
		case r >= 'a' && r <= 'z':
		case r >= '0' && r <= '9':
		case r == '_':
		default:
			return errors.Join(ErrInvalidCardType,
				errors.New("key must contain only lowercase letters, digits, and underscores"))
		}
	}
	// Reject blank-string tiers — they would collide with the
	// "no tier on this card" branch in CardService.Issue.
	for _, t := range c.Tiers {
		if strings.TrimSpace(t) == "" {
			return errors.Join(ErrInvalidCardType, errors.New("empty tier in tiers list"))
		}
	}
	return nil
}
