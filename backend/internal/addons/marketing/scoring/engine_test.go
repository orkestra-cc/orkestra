package scoring

import (
	"math"
	"testing"
	"time"

	"github.com/orkestra-cc/orkestra-addon-marketing/models"
)

func f64Ptr(f float64) *float64 { return &f }

// makeEngine returns an engine with the supplied breakdown cap and
// nil logger — every test exercises pure logic, no log output needed.
func makeEngine(breakdownMax int) *Engine {
	return NewEngine(breakdownMax, nil)
}

// asOf2026 pins a stable clock for every engine test that doesn't
// otherwise care about the wall clock — eliminates flakiness from
// real-time calls slipping into table-driven tests.
var asOf2026 = time.Date(2026, 5, 21, 12, 0, 0, 0, time.UTC)

// TestComputeEmptyInputs covers the three degenerate cases where the
// engine returns the zero ScoreResult without iterating: nil profile,
// empty rules, empty activities.
func TestComputeEmptyInputs(t *testing.T) {
	cases := []struct {
		name       string
		activities []models.Activity
		profile    *models.ScoreProfile
	}{
		{"nil-profile", []models.Activity{{Kind: models.KindEmailOpened, OccurredAt: asOf2026}}, nil},
		{"empty-rules", []models.Activity{{Kind: models.KindEmailOpened, OccurredAt: asOf2026}}, &models.ScoreProfile{}},
		{"empty-activities", nil, &models.ScoreProfile{Rules: []models.ScoreRule{{ActivityKind: "*", Points: 1}}}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r := makeEngine(0).Compute(c.activities, c.profile, asOf2026)
			if r.Value != 0 || len(r.Breakdown) != 0 || r.ActivityCount != 0 || r.LastActivityAt != nil {
				t.Errorf("expected zero ScoreResult, got %+v", r)
			}
		})
	}
}

// TestComputeSingleRuleNoDecay confirms the simplest path: one rule,
// one matching activity, no decay → contribution = Points.
func TestComputeSingleRuleNoDecay(t *testing.T) {
	profile := &models.ScoreProfile{
		Rules: []models.ScoreRule{{ActivityKind: string(models.KindMeetingHeld), Points: 50}},
	}
	acts := []models.Activity{
		{
			UUID:       "a1",
			Kind:       models.KindMeetingHeld,
			OccurredAt: asOf2026.Add(-7 * 24 * time.Hour),
			Source:     models.ActivitySourceManual,
		},
	}
	r := makeEngine(0).Compute(acts, profile, asOf2026)
	if r.Value != 50 {
		t.Errorf("Value = %v, want 50", r.Value)
	}
	if r.ActivityCount != 1 {
		t.Errorf("ActivityCount = %v, want 1", r.ActivityCount)
	}
	if len(r.Breakdown) != 1 {
		t.Fatalf("Breakdown len = %d, want 1", len(r.Breakdown))
	}
	if r.Breakdown[0].PointsContributed != 50 {
		t.Errorf("Breakdown[0].PointsContributed = %v, want 50", r.Breakdown[0].PointsContributed)
	}
}

// TestComputeMultipleRulesAllMatch — wildcard + specific rule both
// fire on the same activity. Verifies the "every matching rule
// contributes" semantics documented on engine.go.
func TestComputeMultipleRulesAllMatch(t *testing.T) {
	profile := &models.ScoreProfile{
		Rules: []models.ScoreRule{
			{ActivityKind: string(models.KindFormSubmitted), Points: 30}, // specific
			{ActivityKind: "*", Points: 1},                               // baseline wildcard
		},
	}
	acts := []models.Activity{
		{UUID: "a1", Kind: models.KindFormSubmitted, OccurredAt: asOf2026},
	}
	r := makeEngine(0).Compute(acts, profile, asOf2026)
	if r.Value != 31 {
		t.Errorf("Value = %v, want 31 (30 specific + 1 wildcard)", r.Value)
	}
	if len(r.Breakdown) != 2 {
		t.Errorf("Breakdown len = %d, want 2", len(r.Breakdown))
	}
}

// TestComputeWildcardOnlyMatchesOnce — same wildcard rule, three
// activities of different kinds. Each fires the wildcard once.
func TestComputeWildcardOnlyMatchesOnce(t *testing.T) {
	profile := &models.ScoreProfile{
		Rules: []models.ScoreRule{{ActivityKind: "*", Points: 1}},
	}
	acts := []models.Activity{
		{UUID: "a1", Kind: models.KindEmailOpened, OccurredAt: asOf2026},
		{UUID: "a2", Kind: models.KindCallMade, OccurredAt: asOf2026},
		{UUID: "a3", Kind: models.KindFormSubmitted, OccurredAt: asOf2026},
	}
	r := makeEngine(0).Compute(acts, profile, asOf2026)
	if r.Value != 3 {
		t.Errorf("Value = %v, want 3 (3 activities × 1 wildcard point each)", r.Value)
	}
}

// TestComputeRuleCap verifies a rule's Cap clamps the sum and scales
// every contributing breakdown entry proportionally. The plan §4.5
// is explicit that Cap is per-rule, not per-snapshot.
func TestComputeRuleCap(t *testing.T) {
	profile := &models.ScoreProfile{
		Rules: []models.ScoreRule{
			{ActivityKind: string(models.KindEmailOpened), Points: 1, Cap: f64Ptr(10)},
		},
	}
	// 15 email_opened — sum would be 15, cap is 10
	acts := make([]models.Activity, 15)
	for i := range acts {
		acts[i] = models.Activity{
			UUID:       "a" + string(rune('A'+i)),
			Kind:       models.KindEmailOpened,
			OccurredAt: asOf2026.Add(-time.Duration(i) * time.Hour),
		}
	}
	r := makeEngine(0).Compute(acts, profile, asOf2026)
	if r.Value != 10 {
		t.Errorf("Value = %v, want 10 (capped)", r.Value)
	}
	// Each entry should be scaled by 10/15 ≈ 0.667
	var entriesSum float64
	for _, e := range r.Breakdown {
		entriesSum += e.PointsContributed
	}
	if math.Abs(entriesSum-10) > 1e-9 {
		t.Errorf("scaled entries sum = %v, want 10", entriesSum)
	}
	for _, e := range r.Breakdown {
		if math.Abs(e.PointsContributed-(10.0/15.0)) > 1e-9 {
			t.Errorf("entry PointsContributed = %v, want 10/15", e.PointsContributed)
		}
	}
}

// TestComputeFutureActivitiesExcluded — activities with OccurredAt
// past asOf are filtered out (time-travel queries can't see the
// future of their cutoff).
func TestComputeFutureActivitiesExcluded(t *testing.T) {
	profile := &models.ScoreProfile{
		Rules: []models.ScoreRule{{ActivityKind: "*", Points: 1}},
	}
	acts := []models.Activity{
		{UUID: "past", Kind: models.KindEmailOpened, OccurredAt: asOf2026.Add(-24 * time.Hour)},
		{UUID: "now", Kind: models.KindEmailOpened, OccurredAt: asOf2026},
		{UUID: "future", Kind: models.KindEmailOpened, OccurredAt: asOf2026.Add(24 * time.Hour)},
	}
	r := makeEngine(0).Compute(acts, profile, asOf2026)
	if r.Value != 2 {
		t.Errorf("Value = %v, want 2 (future activity excluded)", r.Value)
	}
	if r.ActivityCount != 2 {
		t.Errorf("ActivityCount = %v, want 2", r.ActivityCount)
	}
}

// TestComputeCorrectionDirect — a single corrected_by row removes its
// target from the sum.
func TestComputeCorrectionDirect(t *testing.T) {
	profile := &models.ScoreProfile{
		Rules: []models.ScoreRule{{ActivityKind: string(models.KindMeetingHeld), Points: 50}},
	}
	acts := []models.Activity{
		{UUID: "m1", Kind: models.KindMeetingHeld, OccurredAt: asOf2026.Add(-10 * 24 * time.Hour)},
		{
			UUID:       "c1",
			Kind:       models.KindCorrectedBy,
			OccurredAt: asOf2026.Add(-9 * 24 * time.Hour),
			Refs:       models.ActivityRefs{CorrectsActivityUUID: "m1"},
		},
	}
	r := makeEngine(0).Compute(acts, profile, asOf2026)
	if r.Value != 0 {
		t.Errorf("Value = %v, want 0 (m1 corrected out)", r.Value)
	}
	// The correction itself doesn't count as an activity for the
	// count — it's metadata, not an event.
	if r.ActivityCount != 0 {
		t.Errorf("ActivityCount = %v, want 0", r.ActivityCount)
	}
}

// TestComputeCorrectionChain — A ← corrected_by(B) ← corrected_by(C).
// Final state: A counted out, B counted out (it's a correction so
// 0 anyway), C counted out (also a correction). Value = 0.
//
// Then add a fresh untouched meeting to confirm the engine still
// counts surviving activities when corrections are present.
func TestComputeCorrectionChain(t *testing.T) {
	profile := &models.ScoreProfile{
		Rules: []models.ScoreRule{{ActivityKind: string(models.KindMeetingHeld), Points: 50}},
	}
	acts := []models.Activity{
		{UUID: "a", Kind: models.KindMeetingHeld, OccurredAt: asOf2026.Add(-30 * 24 * time.Hour)},
		{
			UUID:       "b",
			Kind:       models.KindCorrectedBy,
			OccurredAt: asOf2026.Add(-29 * 24 * time.Hour),
			Refs:       models.ActivityRefs{CorrectsActivityUUID: "a"},
		},
		{
			UUID:       "c",
			Kind:       models.KindCorrectedBy,
			OccurredAt: asOf2026.Add(-28 * 24 * time.Hour),
			Refs:       models.ActivityRefs{CorrectsActivityUUID: "b"},
		},
		// A fresh, uncorrected meeting that should still count.
		{UUID: "survivor", Kind: models.KindMeetingHeld, OccurredAt: asOf2026.Add(-1 * 24 * time.Hour)},
	}
	r := makeEngine(0).Compute(acts, profile, asOf2026)
	if r.Value != 50 {
		t.Errorf("Value = %v, want 50 (only survivor counted)", r.Value)
	}
	if r.ActivityCount != 1 {
		t.Errorf("ActivityCount = %v, want 1", r.ActivityCount)
	}
}

// TestComputeDecayApplied confirms decay actually attenuates
// contributions when configured at the rule level.
func TestComputeDecayApplied(t *testing.T) {
	profile := &models.ScoreProfile{
		Rules: []models.ScoreRule{
			{
				ActivityKind: string(models.KindEmailOpened),
				Points:       10,
				Decay:        &models.DecayFn{Fn: "linear", WindowDays: intPtr(100)},
			},
		},
	}
	acts := []models.Activity{
		{UUID: "fresh", Kind: models.KindEmailOpened, OccurredAt: asOf2026},                              // age 0 → 10
		{UUID: "midway", Kind: models.KindEmailOpened, OccurredAt: asOf2026.Add(-50 * 24 * time.Hour)},   // age 50d → 5
		{UUID: "expired", Kind: models.KindEmailOpened, OccurredAt: asOf2026.Add(-100 * 24 * time.Hour)}, // age 100d → 0
	}
	r := makeEngine(0).Compute(acts, profile, asOf2026)
	// 10 + 5 + 0 = 15
	if math.Abs(r.Value-15) > 1e-9 {
		t.Errorf("Value = %v, want ~15", r.Value)
	}
}

// TestComputeDefaultDecayInherited — a rule without its own decay
// picks up the profile's DefaultDecay.
func TestComputeDefaultDecayInherited(t *testing.T) {
	profile := &models.ScoreProfile{
		DefaultDecay: &models.DecayFn{Fn: "linear", WindowDays: intPtr(100)},
		Rules: []models.ScoreRule{
			{ActivityKind: string(models.KindEmailOpened), Points: 10},
		},
	}
	acts := []models.Activity{
		{UUID: "a", Kind: models.KindEmailOpened, OccurredAt: asOf2026.Add(-50 * 24 * time.Hour)},
	}
	r := makeEngine(0).Compute(acts, profile, asOf2026)
	if math.Abs(r.Value-5) > 1e-9 {
		t.Errorf("Value = %v, want ~5 (default decay applied)", r.Value)
	}
}

// TestComputeBreakdownCap — 200 activities, cap=100 → exactly 101
// entries (100 normal + 1 aggregate). The aggregate must absorb the
// dropped entries' point contributions.
func TestComputeBreakdownCap(t *testing.T) {
	profile := &models.ScoreProfile{
		Rules: []models.ScoreRule{{ActivityKind: "*", Points: 1}},
	}
	acts := make([]models.Activity, 200)
	for i := range acts {
		acts[i] = models.Activity{
			UUID:       "a" + string(rune('A'+i%26)) + string(rune('A'+(i/26)%26)),
			Kind:       models.KindEmailOpened,
			OccurredAt: asOf2026.Add(-time.Duration(i) * time.Minute),
		}
	}
	r := makeEngine(100).Compute(acts, profile, asOf2026)
	if r.Value != 200 {
		t.Errorf("Value = %v, want 200", r.Value)
	}
	if len(r.Breakdown) != 101 {
		t.Errorf("Breakdown len = %d, want 101 (100 + 1 aggregate)", len(r.Breakdown))
	}
	last := r.Breakdown[len(r.Breakdown)-1]
	if last.ActivityKind != aggregateKind {
		t.Errorf("last breakdown entry kind = %v, want %v", last.ActivityKind, aggregateKind)
	}
	if last.ActivityUUID != "" {
		t.Errorf("aggregate entry should have empty ActivityUUID, got %q", last.ActivityUUID)
	}
	// Aggregate captures the 100 dropped entries → 100 points (each
	// contributed 1).
	if math.Abs(last.PointsContributed-100) > 1e-9 {
		t.Errorf("aggregate PointsContributed = %v, want 100", last.PointsContributed)
	}
	// Sum of every breakdown entry should equal the total value.
	var sum float64
	for _, e := range r.Breakdown {
		sum += e.PointsContributed
	}
	if math.Abs(sum-r.Value) > 1e-9 {
		t.Errorf("breakdown sum %v != Value %v", sum, r.Value)
	}
}

// TestComputeBreakdownCapBelowMax — when the breakdown stays under
// the cap, no aggregate row is appended.
func TestComputeBreakdownCapBelowMax(t *testing.T) {
	profile := &models.ScoreProfile{
		Rules: []models.ScoreRule{{ActivityKind: "*", Points: 1}},
	}
	acts := make([]models.Activity, 50)
	for i := range acts {
		acts[i] = models.Activity{
			UUID:       "a" + string(rune('A'+i)),
			Kind:       models.KindEmailOpened,
			OccurredAt: asOf2026.Add(-time.Duration(i) * time.Minute),
		}
	}
	r := makeEngine(100).Compute(acts, profile, asOf2026)
	if len(r.Breakdown) != 50 {
		t.Errorf("Breakdown len = %d, want 50 (no aggregate)", len(r.Breakdown))
	}
	for _, e := range r.Breakdown {
		if e.ActivityKind == aggregateKind {
			t.Errorf("unexpected aggregate row in under-cap breakdown")
		}
	}
}

// TestComputeBreakdownSortedAscending — the breakdown sort invariant
// stated on engine.go: ascending by OccurredAt.
func TestComputeBreakdownSortedAscending(t *testing.T) {
	profile := &models.ScoreProfile{
		Rules: []models.ScoreRule{{ActivityKind: "*", Points: 1}},
	}
	// Activities inserted out of order.
	acts := []models.Activity{
		{UUID: "newest", Kind: models.KindEmailOpened, OccurredAt: asOf2026},
		{UUID: "oldest", Kind: models.KindEmailOpened, OccurredAt: asOf2026.Add(-100 * 24 * time.Hour)},
		{UUID: "middle", Kind: models.KindEmailOpened, OccurredAt: asOf2026.Add(-50 * 24 * time.Hour)},
	}
	r := makeEngine(0).Compute(acts, profile, asOf2026)
	if len(r.Breakdown) != 3 {
		t.Fatalf("Breakdown len = %d, want 3", len(r.Breakdown))
	}
	want := []string{"oldest", "middle", "newest"}
	for i, w := range want {
		if r.Breakdown[i].ActivityUUID != w {
			t.Errorf("Breakdown[%d].ActivityUUID = %q, want %q", i, r.Breakdown[i].ActivityUUID, w)
		}
	}
}

// TestComputeLastActivityAt pins the LastActivityAt pointer to the
// most-recent OccurredAt across surviving activities.
func TestComputeLastActivityAt(t *testing.T) {
	profile := &models.ScoreProfile{
		Rules: []models.ScoreRule{{ActivityKind: "*", Points: 1}},
	}
	newest := asOf2026.Add(-1 * 24 * time.Hour)
	acts := []models.Activity{
		{UUID: "a", Kind: models.KindEmailOpened, OccurredAt: asOf2026.Add(-10 * 24 * time.Hour)},
		{UUID: "b", Kind: models.KindEmailOpened, OccurredAt: newest},
		{UUID: "c", Kind: models.KindEmailOpened, OccurredAt: asOf2026.Add(-5 * 24 * time.Hour)},
	}
	r := makeEngine(0).Compute(acts, profile, asOf2026)
	if r.LastActivityAt == nil {
		t.Fatal("LastActivityAt is nil")
	}
	if !r.LastActivityAt.Equal(newest) {
		t.Errorf("LastActivityAt = %v, want %v", *r.LastActivityAt, newest)
	}
}

// TestComputeIsPure — confirms the engine doesn't mutate its inputs.
// A regression that started writing into the input slices would
// silently corrupt the activity log on subsequent recomputes.
func TestComputeIsPure(t *testing.T) {
	profile := &models.ScoreProfile{
		Rules: []models.ScoreRule{{ActivityKind: "*", Points: 1, Cap: f64Ptr(2)}},
	}
	// Take a copy to compare against post-Compute.
	origActs := []models.Activity{
		{UUID: "a", Kind: models.KindEmailOpened, OccurredAt: asOf2026, Payload: map[string]any{"k": "v"}},
		{UUID: "b", Kind: models.KindEmailOpened, OccurredAt: asOf2026.Add(-time.Hour)},
		{UUID: "c", Kind: models.KindEmailOpened, OccurredAt: asOf2026.Add(-2 * time.Hour)},
	}
	acts := make([]models.Activity, len(origActs))
	copy(acts, origActs)

	makeEngine(0).Compute(acts, profile, asOf2026)

	for i := range acts {
		if acts[i].UUID != origActs[i].UUID ||
			acts[i].Kind != origActs[i].Kind ||
			!acts[i].OccurredAt.Equal(origActs[i].OccurredAt) {
			t.Errorf("activities[%d] mutated by Compute: got %+v, orig %+v", i, acts[i], origActs[i])
		}
	}
}
