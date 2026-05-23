// Package match implements soft-match dedup helpers per design §5.5.
//
// Soft-match is a SECOND pass that runs only when the strict-match
// pass (email for Person, VAT/TaxCode for Organization) returns no
// candidate. The contract is intentionally narrow: a soft-match hit
// always parks the row in marketing_conflict_reviews — never
// auto-merges — because the false-positive rate on first+last+phone
// (and on legal_name exact-after-normalize) is too high to commit
// without operator review.
//
// Phase 5 is expected to introduce fuzzy matching (Levenshtein /
// token-set ratios). Today's helpers are exact-after-normalize so the
// behavior is fully deterministic and easy to reason about.
package match

import (
	"strings"
	"unicode"

	"github.com/orkestra-cc/orkestra-addon-marketing/models"
)

// SoftMatchPerson returns true when the incoming person looks like
// the existing person on the (firstName + lastName + any phone)
// signal. Empty values disable the check — a record with no first
// name + last name can never soft-match.
func SoftMatchPerson(incoming, existing *models.Person) bool {
	if incoming == nil || existing == nil {
		return false
	}
	if !equalsCI(incoming.FirstName, existing.FirstName) {
		return false
	}
	if !equalsCI(incoming.LastName, existing.LastName) {
		return false
	}
	if incoming.FirstName == "" || incoming.LastName == "" {
		return false
	}
	return phonesOverlap(incoming.Phones, existing.Phones)
}

// SoftMatchOrganization returns true when incoming.LegalName equals
// existing.LegalName after lower(trim(collapseWhitespace)) normalization.
// Empty legal_name disables the check.
func SoftMatchOrganization(incoming, existing *models.Organization) bool {
	if incoming == nil || existing == nil {
		return false
	}
	a := NormalizeLegalName(incoming.LegalName)
	b := NormalizeLegalName(existing.LegalName)
	if a == "" || b == "" {
		return false
	}
	return a == b
}

// NormalizeLegalName is the canonical form used by SoftMatchOrganization.
// Exposed so the pipeline can compare against existing-tenant data
// using the same transform.
func NormalizeLegalName(s string) string {
	if s == "" {
		return ""
	}
	s = strings.ToLower(strings.TrimSpace(s))
	// Collapse internal whitespace runs to a single space — "ACME S P A"
	// and "ACME  SPA" both end up as "acme s p a" / "acme spa", which
	// remain distinct (we don't strip internal punctuation), but
	// whitespace volume doesn't perturb the match.
	var b strings.Builder
	b.Grow(len(s))
	prevSpace := false
	for _, r := range s {
		if unicode.IsSpace(r) {
			if !prevSpace {
				b.WriteByte(' ')
				prevSpace = true
			}
			continue
		}
		b.WriteRune(r)
		prevSpace = false
	}
	return b.String()
}

// NormalizePhone strips everything that isn't a digit and returns the
// last 10 digits. Phone numbers in marketing data come from CSV
// exports / Odoo / hand-typed forms — all forms collapse to a
// "country-agnostic last-10" comparison because the operator's data
// is rarely fully E.164.
//
// Returns "" when fewer than 7 digits survive (numbers too short to
// be a real phone — typically import-row noise like extension codes).
func NormalizePhone(s string) string {
	if s == "" {
		return ""
	}
	digits := make([]byte, 0, len(s))
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c >= '0' && c <= '9' {
			digits = append(digits, c)
		}
	}
	if len(digits) < 7 {
		return ""
	}
	if len(digits) > 10 {
		digits = digits[len(digits)-10:]
	}
	return string(digits)
}

func equalsCI(a, b string) bool {
	if a == "" && b == "" {
		return true
	}
	return strings.EqualFold(strings.TrimSpace(a), strings.TrimSpace(b))
}

func phonesOverlap(a, b []models.PhoneEntry) bool {
	if len(a) == 0 || len(b) == 0 {
		return false
	}
	seen := make(map[string]struct{}, len(a))
	for _, p := range a {
		n := NormalizePhone(p.Number)
		if n != "" {
			seen[n] = struct{}{}
		}
	}
	for _, p := range b {
		n := NormalizePhone(p.Number)
		if n == "" {
			continue
		}
		if _, ok := seen[n]; ok {
			return true
		}
	}
	return false
}
