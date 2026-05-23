package scoring

import (
	"reflect"
	"time"

	"github.com/orkestra-cc/orkestra-addon-marketing/models"
)

// ruleMatches reports whether the rule applies to the activity. Three
// checks, all of which must pass:
//
//  1. ActivityKind selector — "*" wildcard, single kind, or kind list.
//  2. MatchPayload — every key/value in the rule's payload filter
//     must satisfy against the activity's payload (Mongo-style
//     operators).
//  3. WindowDays — activity must be within rule.WindowDays of asOf.
//
// The activity's Kind being KindCorrectedBy is NOT excluded here —
// that's the engine's job; this matcher is purely structural so it
// stays usable by future tooling (e.g. a "would this rule have
// matched?" debug endpoint).
func ruleMatches(act models.Activity, rule models.ScoreRule, asOf time.Time) bool {
	if !matchKindSelector(act.Kind, rule.ActivityKind) {
		return false
	}
	if rule.MatchPayload != nil {
		if !matchPayload(act.Payload, rule.MatchPayload) {
			return false
		}
	}
	if rule.WindowDays != nil && *rule.WindowDays > 0 {
		window := time.Duration(*rule.WindowDays) * 24 * time.Hour
		age := asOf.Sub(act.OccurredAt)
		if age < 0 {
			age = 0
		}
		if age > window {
			return false
		}
	}
	return true
}

// matchKindSelector resolves the polymorphic ActivityKind field on
// ScoreRule. Allowed shapes (post BSON-unmarshal):
//
//	string                   — single kind or "*" wildcard
//	models.ActivityKind      — same as string, typed
//	[]string                 — list of kinds
//	[]models.ActivityKind    — typed list
//	[]any (bson.A)           — list mode after raw BSON unmarshal
//
// Any other shape returns false (the matcher fails closed — better to
// drop a contribution than to apply rules with a corrupted selector).
func matchKindSelector(kind models.ActivityKind, selector any) bool {
	if selector == nil {
		return false
	}
	switch sel := selector.(type) {
	case string:
		if sel == "*" {
			return true
		}
		return sel == string(kind)
	case models.ActivityKind:
		if sel == "*" {
			return true
		}
		return sel == kind
	case []string:
		for _, s := range sel {
			if s == "*" || s == string(kind) {
				return true
			}
		}
		return false
	case []models.ActivityKind:
		for _, s := range sel {
			if s == "*" || s == kind {
				return true
			}
		}
		return false
	case []any:
		for _, item := range sel {
			if matchKindSelector(kind, item) {
				return true
			}
		}
		return false
	default:
		return false
	}
}

// MatchPayload is the exported wrapper around the internal payload
// matcher. ScoreService re-uses it to evaluate
// ProfileFilter.CustomFieldFilters against a Person's customFields
// bag — the matching semantics (implicit equality, $eq/$ne/$in/$exists,
// numeric coercion) are identical to the engine's rule-level
// payload matcher, so the same primitive serves both call sites.
func MatchPayload(payload, filter map[string]any) bool {
	return matchPayload(payload, filter)
}

// matchPayload reports whether actPayload satisfies filter. Filter
// keys map to one of:
//
//	scalar value             — implicit $eq
//	map[string]any with $    — operator-based comparison
//
// Supported operators: $eq, $ne, $in, $exists.
func matchPayload(actPayload, filter map[string]any) bool {
	for key, expected := range filter {
		actVal, present := actPayload[key]
		if !matchValue(actVal, present, expected) {
			return false
		}
	}
	return true
}

// matchValue applies one filter clause to an activity payload value.
// `present` is the existence bit — needed because $exists distinguishes
// "field is absent" from "field is nil".
func matchValue(actVal any, present bool, expected any) bool {
	// Operator map?
	if filterMap, ok := expected.(map[string]any); ok && isOperatorMap(filterMap) {
		for op, opArg := range filterMap {
			if !applyOperator(actVal, present, op, opArg) {
				return false
			}
		}
		return true
	}
	// Implicit equality.
	if !present {
		return false
	}
	return bsonEqual(actVal, expected)
}

// isOperatorMap recognises a filter clause as operator-based by
// detecting at least one $-prefixed key. A mixed map with both $
// keys and plain keys is rejected as malformed — Mongo's own filter
// language has the same restriction.
func isOperatorMap(m map[string]any) bool {
	for k := range m {
		if len(k) > 0 && k[0] == '$' {
			return true
		}
	}
	return false
}

// applyOperator dispatches one operator clause. Unknown operators
// fail the match closed (same defensive choice as the kind matcher).
func applyOperator(actVal any, present bool, op string, opArg any) bool {
	switch op {
	case "$eq":
		return present && bsonEqual(actVal, opArg)
	case "$ne":
		// $ne is true when either the field is absent OR the values
		// differ. Mirrors Mongo's behaviour: matching documents
		// without the field is the historical default.
		if !present {
			return true
		}
		return !bsonEqual(actVal, opArg)
	case "$in":
		if !present {
			return false
		}
		list := toAnySlice(opArg)
		for _, item := range list {
			if bsonEqual(actVal, item) {
				return true
			}
		}
		return false
	case "$exists":
		want := truthy(opArg)
		return present == want
	default:
		return false
	}
}

// toAnySlice normalises the various BSON-unmarshal shapes of a list
// into []any. Returns an empty slice for non-list inputs (so $in with
// a malformed arg matches nothing rather than panicking).
func toAnySlice(v any) []any {
	switch x := v.(type) {
	case []any:
		return x
	case []string:
		out := make([]any, len(x))
		for i, s := range x {
			out[i] = s
		}
		return out
	case []float64:
		out := make([]any, len(x))
		for i, f := range x {
			out[i] = f
		}
		return out
	case []int:
		out := make([]any, len(x))
		for i, n := range x {
			out[i] = n
		}
		return out
	default:
		return nil
	}
}

// truthy collapses Mongo's $exists arg shapes — bool, string "true",
// number nonzero — into a Go bool.
func truthy(v any) bool {
	switch x := v.(type) {
	case bool:
		return x
	case string:
		return x == "true" || x == "1"
	case int:
		return x != 0
	case int32:
		return x != 0
	case int64:
		return x != 0
	case float32:
		return x != 0
	case float64:
		return x != 0
	default:
		return false
	}
}

// bsonEqual compares two values for the purposes of payload matching.
// Numeric types are normalised to float64 because BSON unmarshal may
// produce int32 / int64 / float64 depending on storage — without
// normalisation, the same logical value compares as unequal across
// storage paths.
//
// Slices and maps fall back to reflect.DeepEqual; callers can
// approximate Mongo's deep-match semantics by passing structured
// values that came from the same unmarshal path.
func bsonEqual(a, b any) bool {
	an, aok := asFloat(a)
	bn, bok := asFloat(b)
	if aok && bok {
		return an == bn
	}
	return reflect.DeepEqual(a, b)
}

// asFloat tries to coerce common numeric BSON-unmarshal types into
// float64 for comparison. Returns (0, false) for non-numeric input.
func asFloat(v any) (float64, bool) {
	switch x := v.(type) {
	case int:
		return float64(x), true
	case int32:
		return float64(x), true
	case int64:
		return float64(x), true
	case float32:
		return float64(x), true
	case float64:
		return x, true
	default:
		return 0, false
	}
}
