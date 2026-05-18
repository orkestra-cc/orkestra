package repository

import "testing"

// TestNormalizeVAT pins the canonical form used both at write-time
// and during importer dedup. The underlying invariant: two strings
// that should be the "same VAT" by human judgment must hash to the
// same normalized value.
func TestNormalizeVAT(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"IT01234567890", "IT01234567890"},
		{"  IT01234567890  ", "IT01234567890"},
		{"IT 0123 4567 890", "IT01234567890"},
		{"it01234567890", "IT01234567890"},
		// Leading zeros are significant for some jurisdictions and
		// must not be stripped.
		{"00123", "00123"},
		{"", ""},
		{"   ", ""},
	}
	for _, c := range cases {
		if got := NormalizeVAT(c.in); got != c.want {
			t.Errorf("NormalizeVAT(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

// TestNormalizeTaxCode mirrors TestNormalizeVAT for tax codes — same
// rules apply.
func TestNormalizeTaxCode(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"RSSMRA80A01H501U", "RSSMRA80A01H501U"},
		{"rssmra80a01h501u", "RSSMRA80A01H501U"},
		{"  RSSMRA80A01H501U  ", "RSSMRA80A01H501U"},
		{"RSSM RA80 A01H 501U", "RSSMRA80A01H501U"},
		{"", ""},
	}
	for _, c := range cases {
		if got := NormalizeTaxCode(c.in); got != c.want {
			t.Errorf("NormalizeTaxCode(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

// TestNormalizeEmail pins the lowercased+trimmed form. Gmail-style
// dot/+ aliases are intentionally NOT normalised — that is a
// marketing-domain decision left to the importer when the operator
// requests it.
func TestNormalizeEmail(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"Jane.Doe@Example.com", "jane.doe@example.com"},
		{"  jane@example.com  ", "jane@example.com"},
		{"JANE@EXAMPLE.COM", "jane@example.com"},
		{"", ""},
		// Plus-aliases and gmail dots are preserved — they may
		// legitimately route to different inboxes for some ESPs.
		{"jane+marketing@example.com", "jane+marketing@example.com"},
		{"j.a.n.e@gmail.com", "j.a.n.e@gmail.com"},
	}
	for _, c := range cases {
		if got := NormalizeEmail(c.in); got != c.want {
			t.Errorf("NormalizeEmail(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}
