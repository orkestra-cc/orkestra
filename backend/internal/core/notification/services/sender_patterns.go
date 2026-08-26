package services

import (
	"errors"
	"fmt"
	"strings"
)

// ErrPatternInvalid: a category pattern outside the grammar.
var ErrPatternInvalid = errors.New("notification: sender pattern is not valid")

// NormalizePatterns trims and lowercases each entry, drops empties and
// collapses duplicates within one list (first occurrence wins, order kept).
// Dedup happens here, BEFORE the cross-profile uniqueness check, so a
// within-profile repeat can never be reported as a conflict.
func NormalizePatterns(raw []string) []string {
	seen := make(map[string]bool, len(raw))
	out := make([]string, 0, len(raw))
	for _, r := range raw {
		p := strings.ToLower(strings.TrimSpace(r))
		if p == "" || seen[p] {
			continue
		}
		seen[p] = true
		out = append(out, p)
	}
	return out
}

// ValidatePattern accepts exactly one of: "*", an exact category "foo.bar",
// or a prefix "foo.*". Nothing else: no "*" inside a token, no "*" mid-
// pattern, no empty token. A token is otherwise unrestricted — iface does
// not constrain a fork's category charset, so the pattern does not either.
// Call on normalized input.
func ValidatePattern(p string) error {
	if p == "*" {
		return nil
	}
	lit := strings.TrimSuffix(p, ".*")
	if lit == "" {
		return fmt.Errorf("%w: %q", ErrPatternInvalid, p)
	}
	for _, tok := range strings.Split(lit, ".") {
		// "," is the FieldStringList separator, so it can never be part of a
		// stored pattern; rejecting it makes the impossibility explicit.
		if tok == "" || strings.ContainsAny(tok, "*,") {
			return fmt.Errorf("%w: %q", ErrPatternInvalid, p)
		}
	}
	return nil
}

// MatchPattern reports whether pattern matches category and the length of
// the literal the pattern requires — the precedence key: among matching
// patterns the longest literal wins, and ties are impossible (two prefixes
// of one category with equal literals are the same string; an exact match
// requires the whole category while a prefix requires strictly less).
func MatchPattern(pattern, category string) (matched bool, literal int) {
	switch {
	case pattern == "*":
		return true, 0
	case strings.HasSuffix(pattern, ".*"):
		prefix := pattern[:len(pattern)-1] // keep the dot: "auth."
		return len(category) > len(prefix) && strings.HasPrefix(category, prefix), len(prefix)
	default:
		return category == pattern, len(pattern)
	}
}
