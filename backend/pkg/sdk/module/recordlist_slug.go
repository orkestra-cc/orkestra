package module

import (
	"errors"
	"regexp"
	"strings"
	"unicode"

	"golang.org/x/text/runes"
	"golang.org/x/text/transform"
	"golang.org/x/text/unicode/norm"
)

const (
	// MaxSlugLength bounds an element's immutable key segment. A slug rides
	// inside every one of that element's dotted config keys, so an unbounded
	// one would push those keys past what is comfortable to store and index.
	MaxSlugLength = 64
	// MaxLabelLength bounds an element's editable display label.
	MaxLabelLength = 120
)

var (
	ErrEmptySlug     = errors.New("recordlist: name does not produce a slug")
	ErrLabelRequired = errors.New("recordlist: label is required")
	ErrLabelTooLong  = errors.New("recordlist: label exceeds 120 characters")

	slugPattern    = regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`)
	nonAlphaNumRun = regexp.MustCompile(`[^a-z0-9]+`)
)

// MintSlug derives an element's immutable key segment from its display label.
// Returns "" when the label carries nothing a slug can be built from; callers
// reject that rather than inventing one.
//
// The frontend mirrors this algorithm to preview the slug as the operator
// types. The two must agree — the backend is the authority, and a preview
// that disagrees with what is minted is worse than no preview.
func MintSlug(label string) string {
	t := transform.Chain(norm.NFKD, runes.Remove(runes.In(unicode.Mn)), norm.NFC)
	folded, _, err := transform.String(t, label)
	if err != nil {
		folded = label
	}
	s := nonAlphaNumRun.ReplaceAllString(strings.ToLower(folded), "-")
	s = strings.Trim(s, "-")
	if len(s) > MaxSlugLength {
		s = strings.TrimRight(s[:MaxSlugLength], "-")
	}
	return s
}

// ValidSlug reports whether s obeys the slug grammar. Every slug that reaches
// storage passes through here, minted or client-supplied.
func ValidSlug(s string) bool {
	return s != "" && len(s) <= MaxSlugLength && slugPattern.MatchString(s)
}

// ValidateLabel rejects a label that is blank once trimmed or longer than the
// bound. The label is what the operator reads in the element card; a blank one
// leaves a card no one can identify.
func ValidateLabel(label string) error {
	trimmed := strings.TrimSpace(label)
	if trimmed == "" {
		return ErrLabelRequired
	}
	if len(trimmed) > MaxLabelLength {
		return ErrLabelTooLong
	}
	return nil
}
