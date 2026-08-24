package module

import (
	"errors"
	"fmt"
	"strings"
)

// MaxRecordListItems bounds a single list regardless of the field's Max, so
// neither the config document nor the generated form can grow without limit.
const MaxRecordListItems = 50

var (
	ErrSlugExists          = errors.New("recordlist: slug already exists")
	ErrSlugMissing         = errors.New("recordlist: slug does not exist")
	ErrCreateRemoveOverlap = errors.New("recordlist: a slug cannot be created and removed in one request")
	ErrRosterFull          = errors.New("recordlist: list is full")
)

// ParseRoster reads a record list's membership out of the flat value map.
// An absent roster is an empty list, not an error: a module can declare a
// record list long before an operator adds anything to it.
func ParseRoster(values map[string]string, field string) []string {
	raw := strings.TrimSpace(values[RosterKey(field)])
	if raw == "" {
		return nil
	}
	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

// FormatRoster renders a roster back into its stored form. Order is preserved
// — it is the order the admin form renders elements in.
func FormatRoster(slugs []string) string { return strings.Join(slugs, ",") }

// ApplyMembership computes the target roster from the STORED one and the two
// explicit intent lists. Preconditions are checked against stored — checking
// them against the result would be circular, since the result is derived from
// them. Callers re-invoke this on every CAS attempt, because stored moves.
func ApplyMembership(stored, create, remove []string) ([]string, error) {
	// Order matters, and it follows the HTTP mapping. Create ∩ Remove ≠ ∅ is a
	// MALFORMED request (422) — true no matter what is stored — so it is
	// decided before any check against stored state, which yields 409s. Ask
	// "does this slug exist?" first and a contradictory request reports a
	// stale-roster conflict the client cannot act on instead of the
	// contradiction it can.
	removing := make(map[string]bool, len(remove))
	for _, s := range remove {
		removing[s] = true
	}
	for _, s := range create {
		if removing[s] {
			return nil, fmt.Errorf("%w: %q", ErrCreateRemoveOverlap, s)
		}
	}

	in := make(map[string]bool, len(stored))
	for _, s := range stored {
		in[s] = true
	}
	for _, s := range remove {
		if !ValidSlug(s) || !in[s] {
			return nil, fmt.Errorf("%w: %q", ErrSlugMissing, s)
		}
	}
	for _, s := range create {
		if !ValidSlug(s) {
			return nil, fmt.Errorf("recordlist: malformed slug %q", s)
		}
		if in[s] {
			return nil, fmt.Errorf("%w: %q", ErrSlugExists, s)
		}
	}

	out := make([]string, 0, len(stored)+len(create))
	for _, s := range stored {
		if !removing[s] {
			out = append(out, s)
		}
	}
	out = append(out, create...)
	if len(out) > MaxRecordListItems {
		return nil, fmt.Errorf("%w: %d exceeds %d", ErrRosterFull, len(out), MaxRecordListItems)
	}
	return out, nil
}
