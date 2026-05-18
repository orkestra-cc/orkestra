package services

import "testing"

// TestDeriveSlug pins the canonical slug shape the rest of Orkestra
// uses — lowercase, hyphen-separated, ASCII-safe. Stability across
// cosmetic name changes (case, punctuation) is the load-bearing
// property.
func TestDeriveSlug(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"Manufacturing", "manufacturing"},
		{"Industry / Manufacturing", "industry-manufacturing"},
		{"  Spaced  ", "spaced"},
		{"Acme & Sons, Ltd.", "acme-sons-ltd"},
		{"UPPER CASE", "upper-case"},
		// Empty / punctuation-only inputs yield empty slug — the
		// TagService catches this case and rejects the create.
		{"", ""},
		{"---", ""},
		{"???", ""},
	}
	for _, c := range cases {
		if got := DeriveSlug(c.in); got != c.want {
			t.Errorf("DeriveSlug(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

// TestJoinPath covers root vs nested cases and the parent-path-with-
// trailing-slash defensive trim.
func TestJoinPath(t *testing.T) {
	cases := []struct {
		parent, name, want string
	}{
		{"", "Industry", "/Industry"},
		{"/Industry", "Manufacturing", "/Industry/Manufacturing"},
		{"/Industry/Manufacturing", "Automotive", "/Industry/Manufacturing/Automotive"},
		// Defensive: trim trailing slash on parent if a caller passed
		// "/Industry/" — still produce "/Industry/Manufacturing".
		{"/Industry/", "Manufacturing", "/Industry/Manufacturing"},
		// Names are trimmed.
		{"/Industry", "  Manufacturing  ", "/Industry/Manufacturing"},
	}
	for _, c := range cases {
		if got := JoinPath(c.parent, c.name); got != c.want {
			t.Errorf("JoinPath(%q, %q) = %q, want %q", c.parent, c.name, got, c.want)
		}
	}
}
