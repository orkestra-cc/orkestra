package module

import (
	"errors"
	"fmt"
	"reflect"
	"testing"
)

func TestParseAndFormatRoster(t *testing.T) {
	v := map[string]string{"email.profiles.__items": "a,b-c"}
	if got := ParseRoster(v, "email.profiles"); !reflect.DeepEqual(got, []string{"a", "b-c"}) {
		t.Fatalf("ParseRoster = %v", got)
	}
	if got := ParseRoster(map[string]string{}, "email.profiles"); len(got) != 0 {
		t.Fatalf("absent roster should parse empty, got %v", got)
	}
	if got := FormatRoster([]string{"a", "b-c"}); got != "a,b-c" {
		t.Fatalf("FormatRoster = %q", got)
	}
}

func TestApplyMembershipPreconditions(t *testing.T) {
	stored := []string{"a"}

	if _, err := ApplyMembership(stored, []string{"a"}, nil); !errors.Is(err, ErrSlugExists) {
		t.Errorf("creating an existing slug: got %v, want ErrSlugExists", err)
	}
	if _, err := ApplyMembership(stored, nil, []string{"zz"}); !errors.Is(err, ErrSlugMissing) {
		t.Errorf("removing an absent slug: got %v, want ErrSlugMissing", err)
	}
	if _, err := ApplyMembership(stored, []string{"b"}, []string{"b"}); !errors.Is(err, ErrCreateRemoveOverlap) {
		t.Errorf("create ∩ remove: got %v, want ErrCreateRemoveOverlap", err)
	}
	if _, err := ApplyMembership(stored, []string{"NOPE"}, nil); err == nil {
		t.Error("malformed slug accepted")
	}

	got, err := ApplyMembership([]string{"a", "b"}, []string{"c"}, []string{"a"})
	if err != nil {
		t.Fatalf("valid membership change rejected: %v", err)
	}
	if !reflect.DeepEqual(got, []string{"b", "c"}) {
		t.Fatalf("target = %v, want [b c]", got)
	}
}

func TestRosterCeiling(t *testing.T) {
	stored := make([]string, 0, MaxRecordListItems)
	for i := 0; i < MaxRecordListItems; i++ {
		stored = append(stored, fmt.Sprintf("slug-%d", i))
	}
	if _, err := ApplyMembership(stored, []string{"one-more"}, nil); !errors.Is(err, ErrRosterFull) {
		t.Fatalf("ceiling not enforced: %v", err)
	}
	// Filling exactly to the ceiling is allowed — the bound is on the result.
	if _, err := ApplyMembership(stored[:MaxRecordListItems-1], []string{"one-more"}, nil); err != nil {
		t.Fatalf("a roster landing exactly on the ceiling was rejected: %v", err)
	}
}
