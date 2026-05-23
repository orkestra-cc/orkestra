package models

// CardStatus is the lifecycle state of a marketing_cards row.
//
//	active    — issued and currently valid. Default for newly emitted
//	            cards. The card appears in marketing_persons.activeCardUuids.
//	suspended — temporarily disabled by the staff. Reversible via
//	            CardService.Reinstate. Suspended cards remain in
//	            activeCardUuids — see the §3.4 note in
//	            docs/plans/marketing-addon/IMPLEMENTATION_PLAN_PHASE_4.md
//	            for the rationale (activeCardUuids tracks "issued and
//	            not revoked"; "currently usable" is a separate filter
//	            on Cards.status == active).
//	revoked   — terminal. Set by manual revoke or by the expiration
//	            scheduler (with revoke_reason "expired"). The card is
//	            pulled from activeCardUuids atomically. Once revoked
//	            the card cannot be reinstated; a new card record must
//	            be issued instead.
type CardStatus string

const (
	CardStatusActive    CardStatus = "active"
	CardStatusSuspended CardStatus = "suspended"
	CardStatusRevoked   CardStatus = "revoked"
)

// AllCardStatuses lists every declared CardStatus value. Consumed by
// the handler boundary to reject unknown payloads and by the state-
// machine test which walks every pair.
var AllCardStatuses = []CardStatus{
	CardStatusActive, CardStatusSuspended, CardStatusRevoked,
}

// IsKnownCardStatus reports whether s matches one of the declared
// CardStatus constants.
func IsKnownCardStatus(s CardStatus) bool {
	for _, known := range AllCardStatuses {
		if known == s {
			return true
		}
	}
	return false
}

// CanTransitionCardStatus reports whether moving from `from` to `to`
// is a legal transition per the §3.6 matrix:
//
//	from \ to | active | suspended | revoked
//	----------+--------+-----------+--------
//	active    |   —    |     ✓     |   ✓
//	suspended |   ✓    |     —     |   ✓
//	revoked   |   ✗    |     ✗     |   ✗     (terminal)
//
// Self-transitions (from == to) are rejected — there is no operator
// story for "suspend a suspended card" or "revoke a revoked card".
func CanTransitionCardStatus(from, to CardStatus) bool {
	if from == to {
		return false
	}
	switch from {
	case CardStatusActive:
		return to == CardStatusSuspended || to == CardStatusRevoked
	case CardStatusSuspended:
		return to == CardStatusActive || to == CardStatusRevoked
	case CardStatusRevoked:
		return false
	}
	return false
}
