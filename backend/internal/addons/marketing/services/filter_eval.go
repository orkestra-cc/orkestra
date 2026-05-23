package services

import (
	"github.com/orkestra-cc/orkestra-addon-marketing/models"
	"github.com/orkestra-cc/orkestra-addon-marketing/scoring"
)

// EvaluatePersonFilter reports whether the person passes the profile
// filter, i.e. whether the engine should compute a non-zero
// Applicable score for them. Pure function — no DB calls, fully
// testable in isolation. Used by ScoreService.recomputeOne to set
// snapshot.Applicable before deciding whether to call the engine.
//
// Semantics:
//
//   - filter == nil → every person passes (no filter declared).
//   - TagsInclude (any-of): the person must carry at least one tag
//     in the list. Empty list disables the check.
//   - TagsExclude (none-of): the person must not carry any tag in
//     the list.
//   - CustomFieldFilters: Mongo-style match against the person's
//     CustomFields bag, reusing the same matcher as the scoring
//     engine's rule-level MatchPayload (scoring.MatchPayload).
//
// Order is documented but order-independent — every clause must pass
// for the person to be considered applicable.
func EvaluatePersonFilter(filter *models.ProfileFilter, person *models.Person) bool {
	if filter == nil {
		return true
	}
	if person == nil {
		// Defensive — a deleted person should never reach the engine,
		// but a stale snapshot reference must fail safely.
		return false
	}

	if len(filter.TagsInclude) > 0 {
		if !hasAnyTag(person.Tags, filter.TagsInclude) {
			return false
		}
	}

	if len(filter.TagsExclude) > 0 {
		if hasAnyTag(person.Tags, filter.TagsExclude) {
			return false
		}
	}

	if len(filter.CustomFieldFilters) > 0 {
		if !scoring.MatchPayload(person.CustomFields, filter.CustomFieldFilters) {
			return false
		}
	}

	return true
}

// hasAnyTag reports whether at least one element of want appears in
// have. Small O(n*m) implementation is fine — tag lists are typically
// short on both sides (≤10 tags per person, ≤5 tags per filter clause).
func hasAnyTag(have, want []string) bool {
	for _, w := range want {
		for _, h := range have {
			if h == w {
				return true
			}
		}
	}
	return false
}
