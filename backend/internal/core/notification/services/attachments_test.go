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
