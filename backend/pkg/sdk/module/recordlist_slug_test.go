package module

import (
	"strings"
	"testing"
)

func TestMintSlug(t *testing.T) {
	cases := map[string]string{
		"MailUp SMTP+":      "mailup-smtp",
		"  Città  Aperta  ": "citta-aperta",
		"SES — bulk (2026)": "ses-bulk-2026",
		"already-a-slug":    "already-a-slug",
		"___":               "",
		"🙂":                 "",
	}
	for in, want := range cases {
		if got := MintSlug(in); got != want {
			t.Errorf("MintSlug(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestMintSlugTruncatesAtSixtyFour(t *testing.T) {
	got := MintSlug(strings.Repeat("ab ", 40)) // far past the bound
	if len(got) != 64 {
		t.Fatalf("expected a 64-char slug, got %d (%q)", len(got), got)
	}
	if strings.HasSuffix(got, "-") {
		t.Fatalf("truncation left a trailing dash: %q", got)
	}
	if !ValidSlug(got) {
		t.Fatalf("truncated slug is not valid: %q", got)
	}
}

func TestValidSlug(t *testing.T) {
	ok := []string{"a", "a-b", "mailup-smtp", "a1-2b"}
	bad := []string{"", "-a", "a-", "a--b", "A", "a_b", "a.b", strings.Repeat("a", 65)}
	for _, s := range ok {
		if !ValidSlug(s) {
			t.Errorf("ValidSlug(%q) = false, want true", s)
		}
	}
	for _, s := range bad {
		if ValidSlug(s) {
			t.Errorf("ValidSlug(%q) = true, want false", s)
		}
	}
}

func TestValidateLabel(t *testing.T) {
	if err := ValidateLabel("  "); err == nil {
		t.Error("blank label accepted")
	}
	if err := ValidateLabel(strings.Repeat("x", 121)); err == nil {
		t.Error("over-long label accepted")
	}
	if err := ValidateLabel("MailUp SMTP+"); err != nil {
		t.Errorf("valid label rejected: %v", err)
	}
}
