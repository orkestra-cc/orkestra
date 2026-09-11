package services

import (
	"fmt"
	"strings"

	"github.com/orkestra/backend/internal/shared/errcode"
	"github.com/orkestra/backend/pkg/sdk/module"
)

// allowedSendTypes is the closed set a profile's allowed_types may declare
// (ADR-0021 D2). Compared lowercase, no repair: a value that is not exactly
// one of these after decode's normalization is rejected, not coerced.
var allowedSendTypes = map[string]bool{"marketing": true, "transactional": true}

// ValidateSenderConfig is the save-time and activation-time gate (ADR-0019
// D5, ADR-0021 D2). It sees the module's merged non-secret map — every
// PATCH, including one that touches only app.name — so its rules are scoped
// to two distinct populations, judged by different rules:
//
//	routing      = declares ≥1 pattern      → pattern grammar/duplicate/
//	               default rules, and load-bearing
//	loadBearing  = routing ∪ {allowed_types non-empty} → driver known +
//	               ValidateProfile(SaveTimeView)
//
// The allowed_types VALUE check (marketing|transactional) applies wherever
// the field is non-empty, unconditionally — independent of both
// populations above.
//
// Early return only when NEITHER population exists (roster empty, or every
// profile is a bare draft with no pattern and no allowed_types — nothing
// for either mechanism to reach). The defaults==0 (sender_no_default) rule
// stays scoped to the routing population: a selectable profile with no
// patterns must not force a * to exist, because nothing routes to it —
// legacy flat-key mail still carries every category.
//
// Within the routing population every per-pattern rule applies only to
// profiles that declare at least one pattern: a draft cannot route, so
// nothing it gets wrong can reach a send, and rejecting it would block a
// PATCH the operator did not intend to be about that profile. Grammar is
// the exception — checked wherever a pattern is declared at all,
// well-formed or not.
func ValidateSenderConfig(values map[string]string, drivers *DriverRegistry) error {
	profiles, err := DecodeSenderProfiles(values, nil) // save-time view: no secrets, so no decrypt can fail
	if err != nil {
		return fmt.Errorf("notification: decode sender profiles: %w", err)
	}
	if !hasRoutingMap(profiles) && !hasSelectable(profiles) {
		return nil // neither population exists: nothing to judge
	}
	var routing, loadBearing []SenderProfile
	for _, p := range profiles {
		if len(p.Categories) > 0 {
			routing = append(routing, p)
		}
		if len(p.Categories) > 0 || len(p.AllowedTypes) > 0 {
			loadBearing = append(loadBearing, p)
		}
	}

	claimedBy := make(map[string]string) // pattern → slug
	defaults := 0
	for _, p := range routing {
		catField := module.ItemKey(SendersField, p.Slug, SubCategories)
		for _, pat := range p.Categories {
			if err := ValidatePattern(pat); err != nil {
				return &module.ConfigValidationError{
					Field:   catField,
					Message: fmt.Sprintf("%q is not a valid pattern: use an exact category (auth.verify_email), a prefix (auth.*), or *", capString(pat, maxTokenLen)),
					Code:    errcode.NotificationSenderPatternInvalid,
				}
			}
			if pat == "*" {
				defaults++
				if defaults > 1 {
					return &module.ConfigValidationError{
						Field:   catField,
						Message: "only one sender profile may declare * as its pattern",
						Code:    errcode.NotificationSenderDuplicateDefault,
					}
				}
			}
			if other, dup := claimedBy[pat]; dup {
				return &module.ConfigValidationError{
					Field:   catField,
					Message: fmt.Sprintf("pattern %q is already declared by profile %q", pat, other),
					Code:    errcode.NotificationSenderPatternConflict,
				}
			}
			claimedBy[pat] = p.Slug
		}
	}
	if len(routing) > 0 && defaults == 0 {
		return &module.ConfigValidationError{
			Field:   SendersField,
			Message: "one sender profile must declare * so every category has a sender",
			Code:    errcode.NotificationSenderNoDefault,
		}
	}

	for _, p := range profiles {
		for _, t := range p.AllowedTypes {
			if !allowedSendTypes[t] {
				return &module.ConfigValidationError{
					Field:   module.ItemKey(SendersField, p.Slug, SubAllowedTypes),
					Message: fmt.Sprintf("%q is not a valid send type: allowed values are marketing, transactional", capString(t, maxTokenLen)),
					Code:    errcode.NotificationSenderBadAllowedType,
				}
			}
		}
	}

	for _, p := range loadBearing {
		d, ok := drivers.Get(p.Provider)
		if !ok {
			return &module.ConfigValidationError{
				Field:   module.ItemKey(SendersField, p.Slug, SubProvider),
				Message: "provider must be one of: " + strings.Join(drivers.Names(), ", "),
				Code:    errcode.NotificationSenderUnknownDriver,
			}
		}
		if err := ValidateProfile(d, p, SaveTimeView); err != nil {
			inc := err.(*ProfileIncompleteError)
			return &module.ConfigValidationError{
				Field:   module.ItemKey(SendersField, p.Slug, inc.Missing[0]),
				Message: fmt.Sprintf("the %s provider needs: %s", p.Provider, strings.Join(inc.Missing, ", ")),
				Code:    errcode.NotificationSenderIncomplete,
			}
		}
	}
	return nil
}

// hasSelectable reports whether any profile declares a non-empty
// allowed_types. Mirrors hasRoutingMap over AllowedTypes instead of
// Categories — the same shape of predicate for the second population
// ValidateSenderConfig judges.
func hasSelectable(profiles []SenderProfile) bool {
	for _, p := range profiles {
		if len(p.AllowedTypes) > 0 {
			return true
		}
	}
	return false
}
