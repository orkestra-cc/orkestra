package module

import "testing"

// TestFilterKnown_DropsOrphans verifies that documents for modules no longer
// registered with the binary (orphans left over from the ADR-0006 core-only
// collapse, or any addon a fork removes) are excluded from the listing, while
// registered modules pass through and are reported present.
func TestFilterKnown_DropsOrphans(t *testing.T) {
	docs := []ModuleConfig{
		{ModuleName: "auth"},
		{ModuleName: "billing"}, // orphan — not in known
		{ModuleName: "user"},
		{ModuleName: "marketing"}, // orphan — not in known
	}
	known := map[string]Module{
		"auth": minimalModule{name: "auth"},
		"user": minimalModule{name: "user"},
	}

	got, present := filterKnown(docs, known)

	if len(got) != 2 {
		t.Fatalf("expected 2 kept docs, got %d: %+v", len(got), got)
	}
	for _, d := range got {
		if d.ModuleName == "billing" || d.ModuleName == "marketing" {
			t.Errorf("orphan %q leaked into result", d.ModuleName)
		}
	}
	if !present["auth"] || !present["user"] {
		t.Errorf("expected auth and user marked present, got %+v", present)
	}
	if present["billing"] || present["marketing"] {
		t.Errorf("orphan should not be marked present, got %+v", present)
	}
}

// TestFilterKnown_Empty verifies the degenerate inputs behave sanely.
func TestFilterKnown_Empty(t *testing.T) {
	got, present := filterKnown(nil, map[string]Module{"auth": minimalModule{name: "auth"}})
	if len(got) != 0 || len(present) != 0 {
		t.Errorf("empty docs should yield empty result, got %+v / %+v", got, present)
	}

	docs := []ModuleConfig{{ModuleName: "auth"}}
	got, present = filterKnown(docs, map[string]Module{})
	if len(got) != 0 || len(present) != 0 {
		t.Errorf("empty known should drop everything, got %+v / %+v", got, present)
	}
}
