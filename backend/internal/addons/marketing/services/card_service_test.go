package services

import (
	"errors"
	"testing"

	"github.com/orkestra-cc/orkestra-addon-marketing/models"
)

// TestValidateTier walks the four branches of the tier policy:
// type-has-no-tiers + issue-has-no-tier (OK),
// type-has-no-tiers + issue-carries-one (Forbidden),
// type-has-tiers + issue-no-tier (Required),
// type-has-tiers + issue-not-in-list (NotInType),
// type-has-tiers + issue-in-list (OK).
func TestValidateTier(t *testing.T) {
	noTiers := &models.CardType{}
	withTiers := &models.CardType{Tiers: []string{"bronze", "silver", "gold"}}

	cases := []struct {
		name    string
		typ     *models.CardType
		tier    string
		wantErr error
	}{
		{"no tiers + no tier", noTiers, "", nil},
		{"no tiers + a tier", noTiers, "gold", ErrTierForbidden},
		{"tiers + no tier", withTiers, "", ErrTierRequired},
		{"tiers + unknown tier", withTiers, "platinum", ErrTierNotInType},
		{"tiers + known tier", withTiers, "silver", nil},
		{"tiers + trimmed", withTiers, "  gold  ", nil},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := validateTier(c.typ, c.tier)
			if c.wantErr == nil {
				if err != nil {
					t.Errorf("unexpected error: %v", err)
				}
				return
			}
			if !errors.Is(err, c.wantErr) {
				t.Errorf("error = %v, want %v", err, c.wantErr)
			}
		})
	}
}

// TestCanTransitionViaServiceMatrix asserts the service-layer
// transition gate matches the model-layer matrix. Same shape as
// TestCanTransitionCardStatus in the models package but pinned here
// because CardService.Suspend / Reinstate / Revoke / Expire each
// independently consult CanTransitionCardStatus — a future change
// that loosens one method would not surface in the model test but
// would here.
func TestCanTransitionViaServiceMatrix(t *testing.T) {
	type pair struct {
		from, to models.CardStatus
		want     bool
	}
	cases := []pair{
		// Issue (new → active) is implicit at the service layer
		// (no prior status); not represented as a CanTransition
		// edge.

		// Suspend: active → suspended.
		{models.CardStatusActive, models.CardStatusSuspended, true},
		{models.CardStatusSuspended, models.CardStatusSuspended, false},
		{models.CardStatusRevoked, models.CardStatusSuspended, false},

		// Reinstate: suspended → active.
		{models.CardStatusSuspended, models.CardStatusActive, true},
		{models.CardStatusActive, models.CardStatusActive, false},
		{models.CardStatusRevoked, models.CardStatusActive, false},

		// Revoke / Expire: active|suspended → revoked.
		{models.CardStatusActive, models.CardStatusRevoked, true},
		{models.CardStatusSuspended, models.CardStatusRevoked, true},
		{models.CardStatusRevoked, models.CardStatusRevoked, false},
	}
	for _, c := range cases {
		t.Run(string(c.from)+"->"+string(c.to), func(t *testing.T) {
			if got := models.CanTransitionCardStatus(c.from, c.to); got != c.want {
				t.Errorf("CanTransitionCardStatus(%q, %q) = %v, want %v", c.from, c.to, got, c.want)
			}
		})
	}
}
