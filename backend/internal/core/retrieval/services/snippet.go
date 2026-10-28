package services

import (
	"strings"
	"unicode"
)

// SnippetMaxRunes is the display length of a hit snippet.
const SnippetMaxRunes = 300

const ellipsis = '…'

// Snippet returns at most max runes of text for display: whitespace collapsed,
// windowed around the earliest query-term match, with an ellipsis on each cut
// side (the ellipses count toward max). It works on runes, so the result is
// valid UTF-8 whenever text is.
func Snippet(text, query string, max int) string {
	r := []rune(strings.Join(strings.Fields(text), " "))
	if max <= 0 {
		return ""
	}
	n := len(r)
	if n <= max {
		return string(r)
	}
	if max < 3 {
		// No room for an ellipsis plus content on a cut side.
		return string(r[:max])
	}

	m, tl := firstMatch(r, queryTerms(query))
	start := 0
	if m >= 0 {
		if target := m - max/4; target > 0 {
			start = target
			for i := target; i >= 0; i-- {
				if r[i] == ' ' {
					start = i + 1
					break
				}
			}
		}
	}

	// layout returns the body bounds [s, e) for a window beginning at start,
	// clamped to the text; a short tail pulls start back so the window fills.
	layout := func(start int) (s, e int, lead, trail bool) {
		avail := max
		if start > 0 {
			avail--
		}
		if start+avail >= n {
			return n - (max - 1), n, true, false
		}
		return start, start + avail - 1, start > 0, true
	}
	s, e, lead, trail := layout(start)
	if !trail && s > 0 && m >= 0 {
		// Tail window: begin at a word boundary when that keeps the match.
		for i := s; i < n && i < s+20 && i <= m; i++ {
			if r[i] == ' ' {
				s = i + 1
				break
			}
		}
	}
	if m >= 0 && tl <= max-2 && (m < s || m+tl > e) {
		s, e, lead, trail = layout(m)
	}

	out := make([]rune, 0, max)
	if lead {
		out = append(out, ellipsis)
	}
	out = append(out, []rune(strings.TrimSpace(string(r[s:e])))...)
	if trail {
		out = append(out, ellipsis)
	}
	return string(out)
}

// queryTerms splits on runes that are neither letters nor digits and keeps
// lower-cased terms of at least two runes.
func queryTerms(q string) [][]rune {
	parts := strings.FieldsFunc(q, func(c rune) bool { return !unicode.IsLetter(c) && !unicode.IsDigit(c) })
	var terms [][]rune
	for _, p := range parts {
		t := []rune(p)
		if len(t) < 2 {
			continue
		}
		for i := range t {
			t[i] = unicode.ToLower(t[i])
		}
		terms = append(terms, t)
	}
	return terms
}

// firstMatch returns the rune index of the earliest case-insensitive match of
// any term, and length, or -1.
func firstMatch(r []rune, terms [][]rune) (int, int) {
	for i := range r {
		for _, t := range terms {
			if i+len(t) > len(r) {
				continue
			}
			ok := true
			for j, c := range t {
				if unicode.ToLower(r[i+j]) != c {
					ok = false
					break
				}
			}
			if ok {
				return i, len(t)
			}
		}
	}
	return -1, 0
}
