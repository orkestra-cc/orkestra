package scoring

import (
	"testing"
	"time"

	"github.com/orkestra-cc/orkestra-addon-marketing/models"
)

// TestMatchKindSelectorShapes exhausts every accepted shape of the
// rule's ActivityKind selector. Failure of any case means the BSON-
// roundtrip path drops contributions silently.
func TestMatchKindSelectorShapes(t *testing.T) {
	cases := []struct {
		name     string
		kind     models.ActivityKind
		selector any
		want     bool
	}{
		// String forms
		{"string-exact-match", models.KindEmailOpened, "email_opened", true},
		{"string-mismatch", models.KindEmailOpened, "email_clicked", false},
		{"string-wildcard", models.KindEmailOpened, "*", true},
		// Typed ActivityKind forms
		{"typed-exact-match", models.KindEmailOpened, models.KindEmailOpened, true},
		{"typed-mismatch", models.KindEmailOpened, models.KindEmailClicked, false},
		{"typed-wildcard", models.KindEmailOpened, models.ActivityKind("*"), true},
		// []string list
		{"string-list-hit", models.KindEmailOpened, []string{"email_clicked", "email_opened"}, true},
		{"string-list-miss", models.KindCallMade, []string{"email_clicked", "email_opened"}, false},
		{"string-list-wildcard", models.KindCallMade, []string{"*"}, true},
		// []ActivityKind list
		{"typed-list-hit", models.KindEmailOpened, []models.ActivityKind{models.KindEmailOpened}, true},
		{"typed-list-miss", models.KindCallMade, []models.ActivityKind{models.KindEmailOpened}, false},
		// []any list (BSON unmarshal shape)
		{"any-list-hit", models.KindEmailOpened, []any{"email_clicked", "email_opened"}, true},
		{"any-list-miss", models.KindCallMade, []any{"email_clicked", "email_opened"}, false},
		// Malformed inputs
		{"nil-selector", models.KindEmailOpened, nil, false},
		{"int-selector", models.KindEmailOpened, 42, false},
		{"map-selector", models.KindEmailOpened, map[string]any{"foo": "bar"}, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := matchKindSelector(c.kind, c.selector); got != c.want {
				t.Errorf("matchKindSelector(%v, %v) = %v, want %v",
					c.kind, c.selector, got, c.want)
			}
		})
	}
}

// TestMatchPayloadImplicitEq pins the default operator: a plain
// scalar in the rule filter means equality.
func TestMatchPayloadImplicitEq(t *testing.T) {
	cases := []struct {
		name    string
		payload map[string]any
		filter  map[string]any
		want    bool
	}{
		{
			name:    "match",
			payload: map[string]any{"location": "Milano"},
			filter:  map[string]any{"location": "Milano"},
			want:    true,
		},
		{
			name:    "miss",
			payload: map[string]any{"location": "Roma"},
			filter:  map[string]any{"location": "Milano"},
			want:    false,
		},
		{
			name:    "missing-key",
			payload: map[string]any{"other": "x"},
			filter:  map[string]any{"location": "Milano"},
			want:    false,
		},
		{
			name:    "multi-key-all-match",
			payload: map[string]any{"location": "Milano", "tier": "gold"},
			filter:  map[string]any{"location": "Milano", "tier": "gold"},
			want:    true,
		},
		{
			name:    "multi-key-one-miss",
			payload: map[string]any{"location": "Milano", "tier": "silver"},
			filter:  map[string]any{"location": "Milano", "tier": "gold"},
			want:    false,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := matchPayload(c.payload, c.filter); got != c.want {
				t.Errorf("matchPayload: got %v, want %v", got, c.want)
			}
		})
	}
}

// TestMatchPayloadOperators walks every supported Mongo-style operator.
func TestMatchPayloadOperators(t *testing.T) {
	cases := []struct {
		name    string
		payload map[string]any
		filter  map[string]any
		want    bool
	}{
		// $eq — same as implicit but explicit
		{
			"$eq-match",
			map[string]any{"k": "v"},
			map[string]any{"k": map[string]any{"$eq": "v"}},
			true,
		},
		{
			"$eq-miss",
			map[string]any{"k": "v"},
			map[string]any{"k": map[string]any{"$eq": "w"}},
			false,
		},
		// $ne — true when value differs OR key absent
		{
			"$ne-different",
			map[string]any{"k": "v"},
			map[string]any{"k": map[string]any{"$ne": "w"}},
			true,
		},
		{
			"$ne-same",
			map[string]any{"k": "v"},
			map[string]any{"k": map[string]any{"$ne": "v"}},
			false,
		},
		{
			"$ne-absent",
			map[string]any{},
			map[string]any{"k": map[string]any{"$ne": "v"}},
			true,
		},
		// $in
		{
			"$in-hit-string",
			map[string]any{"k": "b"},
			map[string]any{"k": map[string]any{"$in": []any{"a", "b", "c"}}},
			true,
		},
		{
			"$in-miss-string",
			map[string]any{"k": "z"},
			map[string]any{"k": map[string]any{"$in": []any{"a", "b", "c"}}},
			false,
		},
		{
			"$in-absent",
			map[string]any{},
			map[string]any{"k": map[string]any{"$in": []any{"a", "b"}}},
			false,
		},
		// $exists
		{
			"$exists-true-present",
			map[string]any{"k": nil},
			map[string]any{"k": map[string]any{"$exists": true}},
			true,
		},
		{
			"$exists-true-absent",
			map[string]any{},
			map[string]any{"k": map[string]any{"$exists": true}},
			false,
		},
		{
			"$exists-false-absent",
			map[string]any{},
			map[string]any{"k": map[string]any{"$exists": false}},
			true,
		},
		{
			"$exists-false-present",
			map[string]any{"k": "v"},
			map[string]any{"k": map[string]any{"$exists": false}},
			false,
		},
		// Unknown operator — fail closed
		{
			"unknown-op",
			map[string]any{"k": "v"},
			map[string]any{"k": map[string]any{"$mod": []int{2, 0}}},
			false,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := matchPayload(c.payload, c.filter); got != c.want {
				t.Errorf("matchPayload: got %v, want %v", got, c.want)
			}
		})
	}
}

// TestMatchPayloadInListShapes — $in args can arrive as []any (raw
// BSON unmarshal), []string (typed), []float64 (typed numeric), or
// []int (untyped numeric). All must be supported because rule
// filters are persisted and round-tripped through bson.
func TestMatchPayloadInListShapes(t *testing.T) {
	payload := map[string]any{"k": "b"}
	cases := []struct {
		name   string
		filter map[string]any
		want   bool
	}{
		{"any-slice", map[string]any{"k": map[string]any{"$in": []any{"a", "b", "c"}}}, true},
		{"string-slice", map[string]any{"k": map[string]any{"$in": []string{"a", "b", "c"}}}, true},
		{"any-slice-miss", map[string]any{"k": map[string]any{"$in": []any{"x", "y"}}}, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := matchPayload(payload, c.filter); got != c.want {
				t.Errorf("matchPayload: got %v, want %v", got, c.want)
			}
		})
	}

	// Numeric $in
	if !matchPayload(map[string]any{"n": int64(5)}, map[string]any{"n": map[string]any{"$in": []float64{1, 5, 10}}}) {
		t.Errorf("$in with []float64 numeric list should match int payload")
	}
	if !matchPayload(map[string]any{"n": int(5)}, map[string]any{"n": map[string]any{"$in": []int{1, 5, 10}}}) {
		t.Errorf("$in with []int numeric list should match")
	}
}

// TestMatchPayloadExistsArgShapes — $exists accepts bool, "true"/"1"
// strings, and non-zero numbers because BSON callers sometimes pass
// strings literally. Mirrors Mongo's permissive coercion.
func TestMatchPayloadExistsArgShapes(t *testing.T) {
	payload := map[string]any{"k": "v"}
	truthyShapes := []any{true, "true", "1", int(1), int64(1), float64(1)}
	for _, shape := range truthyShapes {
		got := matchPayload(payload, map[string]any{"k": map[string]any{"$exists": shape}})
		if !got {
			t.Errorf("$exists with truthy arg %v (%T) should match present field", shape, shape)
		}
	}
	falsyShapes := []any{false, "false", "0", int(0), float64(0), "unrelated-string"}
	for _, shape := range falsyShapes {
		got := matchPayload(map[string]any{}, map[string]any{"k": map[string]any{"$exists": shape}})
		if !got {
			t.Errorf("$exists with falsy arg %v (%T) should match absent field", shape, shape)
		}
	}
}

// TestMatchPayloadNumericCoercion confirms that ints / floats compare
// equal across BSON-unmarshal shapes.
func TestMatchPayloadNumericCoercion(t *testing.T) {
	cases := []struct {
		name    string
		payload map[string]any
		filter  map[string]any
		want    bool
	}{
		{"int-vs-int64", map[string]any{"n": int(25)}, map[string]any{"n": int64(25)}, true},
		{"int64-vs-float64", map[string]any{"n": int64(25)}, map[string]any{"n": float64(25)}, true},
		{"int32-vs-float64", map[string]any{"n": int32(25)}, map[string]any{"n": float64(25)}, true},
		{"int-mismatch", map[string]any{"n": int(25)}, map[string]any{"n": int(26)}, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := matchPayload(c.payload, c.filter); got != c.want {
				t.Errorf("matchPayload: got %v, want %v", got, c.want)
			}
		})
	}
}

// TestRuleMatchesWindowDays pins the rule-level age cutoff. The
// matcher returns true within the window and false outside.
func TestRuleMatchesWindowDays(t *testing.T) {
	asOf := time.Date(2026, 5, 21, 12, 0, 0, 0, time.UTC)
	rule := models.ScoreRule{
		ActivityKind: "*",
		Points:       1,
		WindowDays:   intPtr(30),
	}
	cases := []struct {
		name string
		when time.Time
		want bool
	}{
		{"same-day", asOf, true},
		{"15-days-ago", asOf.Add(-15 * 24 * time.Hour), true},
		{"30-days-ago", asOf.Add(-30 * 24 * time.Hour), true},
		{"31-days-ago", asOf.Add(-31 * 24 * time.Hour), false},
		{"365-days-ago", asOf.Add(-365 * 24 * time.Hour), false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			act := models.Activity{
				Kind:       models.KindEmailOpened,
				OccurredAt: c.when,
			}
			if got := ruleMatches(act, rule, asOf); got != c.want {
				t.Errorf("ruleMatches(%v): got %v, want %v", c.name, got, c.want)
			}
		})
	}
}

// TestRuleMatchesComposite combines kind + payload + window in one
// rule so a regression dropping any of the three fails loudly.
func TestRuleMatchesComposite(t *testing.T) {
	asOf := time.Date(2026, 5, 21, 12, 0, 0, 0, time.UTC)
	rule := models.ScoreRule{
		ActivityKind: string(models.KindEventAttended),
		MatchPayload: map[string]any{"location": "Milano"},
		Points:       25,
		WindowDays:   intPtr(365),
	}
	cases := []struct {
		name string
		act  models.Activity
		want bool
	}{
		{
			"full-match",
			models.Activity{
				Kind:       models.KindEventAttended,
				OccurredAt: asOf.Add(-30 * 24 * time.Hour),
				Payload:    map[string]any{"location": "Milano"},
			},
			true,
		},
		{
			"wrong-kind",
			models.Activity{
				Kind:       models.KindCallMade,
				OccurredAt: asOf.Add(-30 * 24 * time.Hour),
				Payload:    map[string]any{"location": "Milano"},
			},
			false,
		},
		{
			"wrong-payload",
			models.Activity{
				Kind:       models.KindEventAttended,
				OccurredAt: asOf.Add(-30 * 24 * time.Hour),
				Payload:    map[string]any{"location": "Roma"},
			},
			false,
		},
		{
			"out-of-window",
			models.Activity{
				Kind:       models.KindEventAttended,
				OccurredAt: asOf.Add(-400 * 24 * time.Hour),
				Payload:    map[string]any{"location": "Milano"},
			},
			false,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := ruleMatches(c.act, rule, asOf); got != c.want {
				t.Errorf("ruleMatches(%v): got %v, want %v", c.name, got, c.want)
			}
		})
	}
}
