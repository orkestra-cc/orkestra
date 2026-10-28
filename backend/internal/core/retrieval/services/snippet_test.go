package services

import (
	"math/rand"
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

func TestSnippet_MatchInLastRunes(t *testing.T) {
	txt := longText(1000, 960, "Responsabile")
	s := Snippet(txt, "responsabile", 300)
	assert.Contains(t, s, "Responsabile")
	assert.Equal(t, true, strings.HasPrefix(s, "…"))
	assert.Equal(t, true, utf8.RuneCountInString(s) <= 300)
	assert.Equal(t, false, strings.ContainsRune(s, 0))
}

func TestSnippet_TinyMax(t *testing.T) {
	txt := longText(200, 150, "Responsabile")
	assert.Equal(t, "", Snippet(txt, "responsabile", 0))
	assert.Equal(t, "l", Snippet(txt, "responsabile", 1))
	assert.Equal(t, "lo", Snippet(txt, "responsabile", 2))
	for m := 3; m < 12; m++ { // smaller than the word
		s := Snippet(txt, "responsabile", m)
		assert.Equal(t, true, utf8.RuneCountInString(s) <= m)
	}
}

func TestSnippet_EmptyAndBlank(t *testing.T) {
	assert.Equal(t, "", Snippet("", "x", 300))
	assert.Equal(t, "", Snippet(" \n\t ", "x", 300))
}

func TestSnippet_MultibyteBoundaries(t *testing.T) {
	txt := strings.Repeat("àèìòù ", 200) + "CITTÀ"
	for _, max := range []int{3, 4, 5, 10, 50} {
		s := Snippet(txt, "città", max)
		assert.Equal(t, true, utf8.ValidString(s))
		assert.Equal(t, true, utf8.RuneCountInString(s) <= max)
	}
	assert.Contains(t, Snippet(txt, "città", 50), "CITTÀ")
}

func TestSnippet_Property(t *testing.T) {
	rng := rand.New(rand.NewSource(42))
	alphabet := []rune("abcdeàèÀ  \n,x9ÇçΩ")
	rs := func(n int) string {
		b := make([]rune, n)
		for i := range b {
			b[i] = alphabet[rng.Intn(len(alphabet))]
		}
		return string(b)
	}
	for i := 0; i < 3000; i++ {
		text := rs(rng.Intn(600))
		query := rs(rng.Intn(6))
		max := rng.Intn(80)
		if rng.Intn(3) == 0 {
			// plant a known term somewhere
			w := "Zeta"
			if cut := rng.Intn(len(text) + 1); true {
				text = text[:cut] + " " + w + " " + text[cut:]
			}
			query = "zeta"
		}
		s := Snippet(text, query, max)
		if !utf8.ValidString(s) || strings.ContainsRune(s, 0) || utf8.RuneCountInString(s) > max {
			t.Fatalf("bad snippet %q (max %d) from %q / %q", s, max, text, query)
		}
		if strings.Contains(query, "zeta") && max >= len("Zeta")+2 && strings.Contains(text, "Zeta") {
			// the earliest match of any term may be another one; only
			// assert containment when "zeta" is the sole term.
			if strings.TrimSpace(query) == "zeta" && !strings.Contains(s, "Zeta") &&
				!strings.Contains(strings.ToLower(s), "zeta") {
				t.Fatalf("match lost %q (max %d) from %q", s, max, text)
			}
		}
	}
}
