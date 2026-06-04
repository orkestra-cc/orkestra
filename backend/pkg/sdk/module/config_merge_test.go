package module

import "testing"

// TestMergeStringMaps_PreservesUnmentionedKeys is the regression guard for the
// config-only-update secret wipe: merging an empty overlay (a PATCH that
// flips a toggle but carries no secrets) must leave every existing key intact.
func TestMergeStringMaps_PreservesUnmentionedKeys(t *testing.T) {
	base := map[string]string{
		"googleClientSecret": "cipher-google",
		"githubClientSecret": "cipher-github",
	}

	got := mergeStringMaps(base, map[string]string{})

	if len(got) != 2 || got["googleClientSecret"] != "cipher-google" || got["githubClientSecret"] != "cipher-github" {
		t.Fatalf("empty overlay must preserve all base keys, got %+v", got)
	}
}

// TestMergeStringMaps_OverlayWins verifies overlay keys add to / overwrite base
// without touching the keys overlay does not mention.
func TestMergeStringMaps_OverlayWins(t *testing.T) {
	base := map[string]string{"a": "1", "b": "2"}
	overlay := map[string]string{"b": "20", "c": "3"}

	got := mergeStringMaps(base, overlay)

	want := map[string]string{"a": "1", "b": "20", "c": "3"}
	if len(got) != len(want) {
		t.Fatalf("expected %d keys, got %d: %+v", len(want), len(got), got)
	}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("key %q: want %q, got %q", k, v, got[k])
		}
	}
}

// TestMergeStringMaps_DoesNotMutateBase guards against the in-place mutation the
// old call sites relied on — the merge must return a fresh map.
func TestMergeStringMaps_DoesNotMutateBase(t *testing.T) {
	base := map[string]string{"a": "1"}

	got := mergeStringMaps(base, map[string]string{"a": "2", "b": "3"})

	if base["a"] != "1" || len(base) != 1 {
		t.Errorf("base must not be mutated, got %+v", base)
	}
	if got["a"] != "2" || got["b"] != "3" {
		t.Errorf("returned map missing overlay, got %+v", got)
	}
}

// TestMergeStringMaps_NilInputs covers the degenerate cases — a never-seeded
// module (nil base) and a values-only update (nil overlay).
func TestMergeStringMaps_NilInputs(t *testing.T) {
	if got := mergeStringMaps(nil, map[string]string{"a": "1"}); len(got) != 1 || got["a"] != "1" {
		t.Errorf("nil base should yield overlay, got %+v", got)
	}
	if got := mergeStringMaps(map[string]string{"a": "1"}, nil); len(got) != 1 || got["a"] != "1" {
		t.Errorf("nil overlay should yield base, got %+v", got)
	}
	if got := mergeStringMaps(nil, nil); len(got) != 0 {
		t.Errorf("nil+nil should yield empty, got %+v", got)
	}
}
