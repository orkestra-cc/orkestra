package scoring

import (
	"testing"

	"github.com/orkestra-cc/orkestra-addon-marketing/models"
)

// TestBuildCorrectedSetEmpty: nothing to correct.
func TestBuildCorrectedSetEmpty(t *testing.T) {
	acts := []models.Activity{
		{UUID: "a", Kind: models.KindEmailOpened},
		{UUID: "b", Kind: models.KindCallMade},
	}
	got := buildCorrectedSet(acts)
	if len(got) != 0 {
		t.Errorf("expected empty set, got %v", got)
	}
}

// TestBuildCorrectedSetDirect: one correction points at one row.
func TestBuildCorrectedSetDirect(t *testing.T) {
	acts := []models.Activity{
		{UUID: "a", Kind: models.KindEmailOpened},
		{
			UUID: "b",
			Kind: models.KindCorrectedBy,
			Refs: models.ActivityRefs{CorrectsActivityUUID: "a"},
		},
	}
	got := buildCorrectedSet(acts)
	if len(got) != 1 {
		t.Fatalf("expected 1 corrected UUID, got %d", len(got))
	}
	if _, ok := got["a"]; !ok {
		t.Errorf("expected 'a' in corrected set, got %v", got)
	}
}

// TestBuildCorrectedSetChain: A ← corrected_by(B) ← corrected_by(C).
// The set contains A and B; C survives but is itself a correction
// row (no points anyway).
func TestBuildCorrectedSetChain(t *testing.T) {
	acts := []models.Activity{
		{UUID: "a", Kind: models.KindEmailOpened},
		{
			UUID: "b",
			Kind: models.KindCorrectedBy,
			Refs: models.ActivityRefs{CorrectsActivityUUID: "a"},
		},
		{
			UUID: "c",
			Kind: models.KindCorrectedBy,
			Refs: models.ActivityRefs{CorrectsActivityUUID: "b"},
		},
	}
	got := buildCorrectedSet(acts)
	if len(got) != 2 {
		t.Fatalf("expected 2 corrected UUIDs (a, b), got %d (%v)", len(got), got)
	}
	for _, uuid := range []string{"a", "b"} {
		if _, ok := got[uuid]; !ok {
			t.Errorf("expected %q in corrected set, got %v", uuid, got)
		}
	}
}

// TestBuildCorrectedSetMalformed: a correction without a target UUID
// is ignored — the engine doesn't error, it just doesn't remove
// anything.
func TestBuildCorrectedSetMalformed(t *testing.T) {
	acts := []models.Activity{
		{UUID: "a", Kind: models.KindEmailOpened},
		{
			UUID: "b",
			Kind: models.KindCorrectedBy,
			// No Refs populated
		},
	}
	got := buildCorrectedSet(acts)
	if len(got) != 0 {
		t.Errorf("expected empty set for malformed correction, got %v", got)
	}
}

// TestBuildCorrectedSetMultiple: several independent corrections at
// once each remove a different target.
func TestBuildCorrectedSetMultiple(t *testing.T) {
	acts := []models.Activity{
		{UUID: "x", Kind: models.KindEmailOpened},
		{UUID: "y", Kind: models.KindCallMade},
		{UUID: "z", Kind: models.KindMeetingHeld},
		{
			UUID: "c1",
			Kind: models.KindCorrectedBy,
			Refs: models.ActivityRefs{CorrectsActivityUUID: "x"},
		},
		{
			UUID: "c2",
			Kind: models.KindCorrectedBy,
			Refs: models.ActivityRefs{CorrectsActivityUUID: "z"},
		},
	}
	got := buildCorrectedSet(acts)
	if len(got) != 2 {
		t.Fatalf("expected 2 corrected UUIDs, got %d (%v)", len(got), got)
	}
	if _, ok := got["x"]; !ok {
		t.Errorf("missing 'x' in corrected set: %v", got)
	}
	if _, ok := got["z"]; !ok {
		t.Errorf("missing 'z' in corrected set: %v", got)
	}
	if _, ok := got["y"]; ok {
		t.Errorf("'y' should not be in corrected set: %v", got)
	}
}
