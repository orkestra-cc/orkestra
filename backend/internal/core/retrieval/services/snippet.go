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
	if len(r) <= max {
		return string(r)
	}

	start := 0
	if m := firstMatch(r, queryTerms(query)); m >= 0 {
		target := m - max/4
		if target > 0 {
			start = target
			for i := target; i >= 0; i-- {
				if r[i] == ' ' {
					start = i + 1
					break
				}
			}
		}
	}

	avail := max
	lead := start > 0
	if lead {
		avail--
	}
	var body []rune
	trail := start+avail < len(r)
	if trail {
		avail--
	}
	if avail < 0 {
		avail = 0
	}
	body = r[start : start+avail]
	if trail {
		body = []rune(strings.TrimRight(string(body), " "))
	}
	out := make([]rune, 0, len(body)+2)
	if lead {
		out = append(out, ellipsis)
	}
	out = append(out, body...)
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
// any term, or -1.
func firstMatch(r []rune, terms [][]rune) int {
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
				return i
			}
		}
	}
	return -1
}
