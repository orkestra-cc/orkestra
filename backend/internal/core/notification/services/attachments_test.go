package services

import (
	"strings"
	"testing"
)

func TestSanitizeAttachmentFilename(t *testing.T) {
	cases := map[string]string{
		"Ricevuta — Iscrizione.pdf":       "Ricevuta — Iscrizione.pdf",
		"../../etc/passwd.pdf":            "passwd.pdf",
		"a\r\nBcc: x@y.pdf":               "aBcc x@y.pdf",
		`q"uote.pdf`:                      "quote.pdf",
		"":                                "attachment.pdf",
		strings.Repeat("a", 300) + ".pdf": strings.Repeat("a", 150) + ".pdf",
	}
	for in, want := range cases {
		if got := SanitizeAttachmentFilename(in); got != want {
			t.Errorf("SanitizeAttachmentFilename(%q) = %q, want %q", in, got, want)
		}
	}
}

// TestSanitizeAttachmentFilename_ExtensionCapped: an "extension" longer than
// maxAttachmentExtRunes is not an extension — it joins the stem, so the
// 150-rune cap bounds the whole name instead of letting a dotted tail through.
func TestSanitizeAttachmentFilename_ExtensionCapped(t *testing.T) {
	cases := map[string]string{
		strings.Repeat("a", 300) + ".abcdefghij":       strings.Repeat("a", 150) + ".abcdefghij",
		strings.Repeat("a", 300) + ".abcdefghijklmnop": strings.Repeat("a", 150),
		"a." + strings.Repeat("x", 300):                ("a." + strings.Repeat("x", 300))[:150],
	}
	for in, want := range cases {
		if got := SanitizeAttachmentFilename(in); got != want {
			t.Errorf("SanitizeAttachmentFilename(%q) = %q, want %q", in, got, want)
		}
	}
}

// TestSanitizeAttachmentFilename_DropsFormatChars: Unicode format characters
// (category Cf) are invisible — U+202E flips "fdp.exe" into a name that reads
// "exe.pdf", zero-width ones smuggle look-alike names — so none survives.
func TestSanitizeAttachmentFilename_DropsFormatChars(t *testing.T) {
	cases := map[string]string{
		"Ricevuta\u202Efdp.exe.pdf": "Ricevutafdp.exe.pdf",
		"\u200Bx\u200D\uFEFF.pdf":   "x.pdf",
		"a\u2066b\u2069.pdf":        "ab.pdf",
	}
	for in, want := range cases {
		if got := SanitizeAttachmentFilename(in); got != want {
			t.Errorf("SanitizeAttachmentFilename(%q) = %q, want %q", in, got, want)
		}
	}
}
