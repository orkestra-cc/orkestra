package module

import (
	"reflect"
	"sort"
	"testing"
)

func TestKeyComposition(t *testing.T) {
	if got := RosterKey("email.profiles"); got != "email.profiles.__items" {
		t.Errorf("RosterKey = %q", got)
	}
	if got := LabelKey("email.profiles", "a"); got != "email.profiles.a.__label" {
		t.Errorf("LabelKey = %q", got)
	}
	if got := ItemKey("email.profiles", "a", "host"); got != "email.profiles.a.host" {
		t.Errorf("ItemKey = %q", got)
	}
	if got := ItemPrefix("email.profiles", "a"); got != "email.profiles.a." {
		t.Errorf("ItemPrefix must carry the trailing separator, got %q", got)
	}
}

// The boundary is the whole point: "a" and "a-b" are ordinary siblings under
// the slug grammar, and a prefix match without the trailing dot swallows both.
func TestKeysUnderElementRespectsTheBoundary(t *testing.T) {
	keys := []string{
		"email.profiles.a.host",
		"email.profiles.a.password",
		"email.profiles.a.__label",
		"email.profiles.a-b.host",
		"email.profiles.ab.host",
		"email.profiles.__items",
		"unrelated.key",
	}
	got := KeysUnderElement(keys, "email.profiles", "a")
	sort.Strings(got)
	want := []string{"email.profiles.a.__label", "email.profiles.a.host", "email.profiles.a.password"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("KeysUnderElement = %v, want %v", got, want)
	}
}
