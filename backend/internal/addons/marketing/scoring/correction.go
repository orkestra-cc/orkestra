package scoring

import "github.com/orkestra-cc/orkestra-addon-marketing/models"

// buildCorrectedSet returns the set of activity UUIDs that have been
// superseded by a correction event (Kind == KindCorrectedBy with a
// populated Refs.CorrectsActivityUUID). The engine consults this set
// to skip corrected rows when summing the score.
//
// The correction signal itself (the KindCorrectedBy row) is NOT in
// the returned set — the engine skips it via the Kind check, not via
// the corrected-set lookup. Keeping the two signals separate lets a
// future debug endpoint surface "this row was a correction for X"
// without having to re-derive the relationship.
//
// Chained corrections work transitively because each correction in
// the chain adds the previous activity's UUID to the set: if
// A ← corrected_by(B) ← corrected_by(C), the set is {A, B} and only
// C survives the engine's filter. C is itself a KindCorrectedBy row
// so it contributes 0 points either way — the chain effectively
// erases the original A.
//
// If a correction's CorrectsActivityUUID is empty (operator entered
// a note without targeting a row) the correction is ignored — the
// engine doesn't error on malformed references, it just doesn't
// remove anything.
func buildCorrectedSet(activities []models.Activity) map[string]struct{} {
	out := map[string]struct{}{}
	for _, a := range activities {
		if a.Kind != models.KindCorrectedBy {
			continue
		}
		target := a.Refs.CorrectsActivityUUID
		if target == "" {
			continue
		}
		out[target] = struct{}{}
	}
	return out
}
