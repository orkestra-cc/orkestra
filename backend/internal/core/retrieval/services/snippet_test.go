package services

import (
	"strings"
	"testing"
	"unicode/utf8"
)

func longText(n, at int, word string) string {
	var b strings.Builder
	for utf8.RuneCountInString(b.String()) < at {
		b.WriteString("lorem ")
	}
	b.WriteString(word + " ")
	for utf8.RuneCountInString(b.String()) < n {
		b.WriteString("ipsum ")
	}
	return b.String()
}

func TestSnippet_ShortTextCollapsed(t *testing.T) {
	assert.Equal(t, "a b c", Snippet("a  b\n c", "x", 300))
}

func TestSnippet_WindowAroundFirstMatch(t *testing.T) {
	s := Snippet(longText(1000, 600, "Responsabile"), "responsabile", 300)
	assert.Contains(t, s, "Responsabile")
	assert.Equal(t, true, strings.HasPrefix(s, "…") && strings.HasSuffix(s, "…"))
	assert.Equal(t, true, utf8.RuneCountInString(s) <= 300)
}

func TestSnippet_MatchNearStartHasNoLeadingEllipsis(t *testing.T) {
	s := Snippet(longText(1000, 20, "Responsabile"), "Responsabile", 300)
	assert.Equal(t, false, strings.HasPrefix(s, "…"))
	assert.Equal(t, true, strings.HasSuffix(s, "…"))
	assert.Contains(t, s, "Responsabile")
	assert.Equal(t, true, utf8.RuneCountInString(s) <= 300)
}

func TestSnippet_NoMatchTakesTheHead(t *testing.T) {
	s := Snippet(longText(1000, 600, "zzz"), "nothing here", 300)
	assert.Equal(t, true, strings.HasPrefix(s, "lorem"))
	assert.Equal(t, true, strings.HasSuffix(s, "…"))
	assert.Equal(t, true, utf8.RuneCountInString(s) <= 300)
}

func TestSnippet_CaseInsensitiveMultibyte(t *testing.T) {
	s := Snippet(longText(1000, 450, "CITTÀ"), "città", 300)
	assert.Contains(t, s, "CITTÀ")
	assert.Equal(t, true, utf8.ValidString(s))
	assert.Equal(t, true, utf8.RuneCountInString(s) <= 300)
}
