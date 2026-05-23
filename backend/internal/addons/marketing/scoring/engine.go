package scoring

import (
	"log/slog"
	"math"
	"sort"
	"time"

	"github.com/orkestra-cc/orkestra-addon-marketing/models"
)

// DefaultBreakdownMax is the engine's fallback cap on per-snapshot
// breakdown entries. Operators override via the activityBreakdownMax
// module config (Phase 2 plan §2.4) and the module wiring passes the
// resolved value to NewEngine.
const DefaultBreakdownMax = 100

// aggregateKind is the synthetic ActivityKind used for the single
// breakdown row that absorbs entries dropped past the breakdown cap.
// Deliberately NOT in models.AllKinds — the handler validator would
// reject it on a POST, which is correct because it's never a stored
// activity, only a synthetic UI hint.
const aggregateKind models.ActivityKind = "aggregate"

// ScoreResult is the engine's pure output: the snapshot fields a
// caller would persist plus the diagnostic breakdown. The
// ScoreService combines this with a per-Person Applicable check
// (which lives outside the engine because it needs the Person record,
// not just activities).
type ScoreResult struct {
	Value          float64
	Breakdown      []models.BreakdownEntry
	ActivityCount  int
	LastActivityAt *time.Time
}

// Engine is the pure scorer. The only state it carries is the
// breakdown cap (per-call configurable via NewEngine). All other
// inputs flow through Compute as arguments.
//
// Purity invariants — verified by the engine tests:
//
//   - No DB calls. No HTTP. No file I/O.
//   - No time.Now() reads. The caller supplies asOf; if you want
//     "now" you pass time.Now().UTC() at the call site.
//   - Input slices and maps are treated as read-only — no mutation
//     leaks back to the caller.
//   - Deterministic output ordering: Breakdown is sorted by
//     OccurredAt ascending (with the aggregate row, when present,
//     appended last as a synthetic marker).
type Engine struct {
	breakdownMax int
	logger       *slog.Logger
}

// NewEngine constructs an engine with the given breakdown cap. A
// non-positive value falls back to DefaultBreakdownMax. The logger
// is optional — passing nil disables runtime warnings (e.g. when a
// profile references a kind that never fires).
func NewEngine(breakdownMax int, logger *slog.Logger) *Engine {
	if breakdownMax <= 0 {
		breakdownMax = DefaultBreakdownMax
	}
	return &Engine{breakdownMax: breakdownMax, logger: logger}
}

// Compute scores the given activities against the profile as of the
// supplied instant. It is a pure function of its inputs.
//
// Semantics (matches §4 of the Phase 2 plan):
//
//   - Activities with Kind == KindCorrectedBy contribute no points;
//     they only act as correction signals.
//   - Activities whose UUID is referenced by a correction's
//     Refs.CorrectsActivityUUID are excluded entirely (the
//     correction supersedes them).
//   - Activities with OccurredAt > asOf are excluded (the engine
//     respects the asOf cutoff for time-travel queries).
//   - For every surviving activity, every matching rule contributes
//     Points × decay(age). Decay falls back to ProfileDefaultDecay
//     when a rule does not declare one.
//   - Per-rule Cap is applied after summing all of that rule's
//     contributions; capped rules scale every contributing breakdown
//     entry proportionally so the per-entry numbers stay consistent
//     with the cap-adjusted total.
//   - Breakdown is then sorted ascending by OccurredAt and, if it
//     exceeds the engine's breakdownMax, the lowest-contribution
//     entries past the cap are collapsed into one synthetic
//     "aggregate" entry appended at the end.
func (e *Engine) Compute(activities []models.Activity, profile *models.ScoreProfile, asOf time.Time) ScoreResult {
	if profile == nil || len(profile.Rules) == 0 || len(activities) == 0 {
		return ScoreResult{}
	}

	corrected := buildCorrectedSet(activities)

	type ruleAccum struct {
		sum     float64
		indices []int // indices into the breakdown slice
	}
	perRule := make([]ruleAccum, len(profile.Rules))
	breakdown := make([]models.BreakdownEntry, 0, len(activities))

	var activityCount int
	var lastActivityAt time.Time

	for _, act := range activities {
		if act.Kind == models.KindCorrectedBy {
			continue
		}
		if _, isCorrected := corrected[act.UUID]; isCorrected {
			continue
		}
		if act.OccurredAt.After(asOf) {
			continue
		}

		activityCount++
		if act.OccurredAt.After(lastActivityAt) {
			lastActivityAt = act.OccurredAt
		}

		for idx, rule := range profile.Rules {
			if !ruleMatches(act, rule, asOf) {
				continue
			}
			d := rule.Decay
			if d == nil {
				d = profile.DefaultDecay
			}
			age := asOf.Sub(act.OccurredAt)
			appliedDecay := applyDecay(d, age)
			contribution := rule.Points * appliedDecay

			perRule[idx].sum += contribution
			perRule[idx].indices = append(perRule[idx].indices, len(breakdown))

			breakdown = append(breakdown, models.BreakdownEntry{
				ActivityUUID:      act.UUID,
				ActivityKind:      act.Kind,
				OccurredAt:        act.OccurredAt,
				RuleIndex:         idx,
				RawPoints:         rule.Points,
				AppliedDecay:      appliedDecay,
				PointsContributed: contribution,
			})
		}
	}

	var totalValue float64
	for idx, rule := range profile.Rules {
		accum := perRule[idx]
		if accum.sum == 0 {
			continue
		}
		if rule.Cap != nil && accum.sum > *rule.Cap {
			ratio := *rule.Cap / accum.sum
			for _, i := range accum.indices {
				breakdown[i].PointsContributed *= ratio
			}
			totalValue += *rule.Cap
		} else {
			totalValue += accum.sum
		}
	}

	// Deterministic ordering: oldest first so the UI timeline matches
	// the breakdown order top-to-bottom.
	sort.SliceStable(breakdown, func(i, j int) bool {
		if !breakdown[i].OccurredAt.Equal(breakdown[j].OccurredAt) {
			return breakdown[i].OccurredAt.Before(breakdown[j].OccurredAt)
		}
		// Stable secondary sort on rule index → predictable test
		// ordering for activities that fire multiple rules in the
		// same tick.
		return breakdown[i].RuleIndex < breakdown[j].RuleIndex
	})

	if len(breakdown) > e.breakdownMax {
		breakdown = capBreakdown(breakdown, e.breakdownMax)
	}

	result := ScoreResult{
		Value:         totalValue,
		Breakdown:     breakdown,
		ActivityCount: activityCount,
	}
	if !lastActivityAt.IsZero() {
		// Return a pointer so the snapshot model's optional
		// LastActivityAt field can be set verbatim by the caller.
		t := lastActivityAt
		result.LastActivityAt = &t
	}
	return result
}

// capBreakdown reduces the slice to max + 1 entries: the top `max`
// by absolute contribution, sorted ascending by OccurredAt, plus one
// synthetic aggregate row appended last.
//
// Absolute value drives the ranking because a future Phase-2.5
// `corrected_by` flow may emit negative contributions; we want to
// keep the most informative entries either way, not just the largest
// positives. Today contributions are non-negative so the rule
// degenerates to "top N by contribution", which is what the plan
// specifies.
func capBreakdown(entries []models.BreakdownEntry, max int) []models.BreakdownEntry {
	if len(entries) <= max {
		return entries
	}
	// Rank indices by descending |PointsContributed|.
	idx := make([]int, len(entries))
	for i := range idx {
		idx[i] = i
	}
	sort.SliceStable(idx, func(a, b int) bool {
		ai, bi := idx[a], idx[b]
		return math.Abs(entries[ai].PointsContributed) > math.Abs(entries[bi].PointsContributed)
	})

	keep := make(map[int]struct{}, max)
	for i := 0; i < max; i++ {
		keep[idx[i]] = struct{}{}
	}

	out := make([]models.BreakdownEntry, 0, max+1)
	var aggregate float64
	for i, e := range entries {
		if _, k := keep[i]; k {
			out = append(out, e)
		} else {
			aggregate += e.PointsContributed
		}
	}
	// Re-sort the kept entries by OccurredAt asc (the index-keyed
	// pass above scrambled the order).
	sort.SliceStable(out, func(i, j int) bool {
		if !out[i].OccurredAt.Equal(out[j].OccurredAt) {
			return out[i].OccurredAt.Before(out[j].OccurredAt)
		}
		return out[i].RuleIndex < out[j].RuleIndex
	})
	out = append(out, models.BreakdownEntry{
		ActivityKind:      aggregateKind,
		PointsContributed: aggregate,
	})
	return out
}
