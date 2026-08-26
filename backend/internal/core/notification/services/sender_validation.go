package services

import (
	"fmt"
	"strings"

	"github.com/orkestra/backend/internal/shared/errcode"
	"github.com/orkestra/backend/pkg/sdk/module"
)

// ValidateSenderConfig is the save-time and activation-time gate (ADR-0019
// D5). It sees the module's merged non-secret map — every PATCH, including
// one that touches only app.name — so the routing rules are scoped to the
// states in which a routing map exists:
//
//	roster empty                     → vacuous (a legacy install)
//	roster non-empty, no patterns    → vacuous (every profile is a draft)
//	≥1 profile declares a pattern    → all rules apply
//
// In the third state every per-profile rule applies only to profiles that
// declare at least one pattern: a draft cannot route, so nothing it gets
// wrong can reach a send, and rejecting it would block a PATCH the operator
// did not intend to be about that profile. Grammar is the exception —
// checked wherever a pattern is declared at all, well-formed or not.
func ValidateSenderConfig(values map[string]string, drivers *DriverRegistry) error {
	profiles, err := DecodeSenderProfiles(values, nil) // save-time view: no secrets, so no decrypt can fail
	if err != nil {
		return fmt.Errorf("notification: decode sender profiles: %w", err)
	}
	if !hasRoutingMap(profiles) {
		return nil // states 1 and 2: no routing map, nothing to judge
	}
	var routing []SenderProfile
	for _, p := range profiles {
		if len(p.Categories) > 0 {
			routing = append(routing, p)
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
	if defaults == 0 {
		return &module.ConfigValidationError{
			Field:   SendersField,
			Message: "one sender profile must declare * so every category has a sender",
			Code:    errcode.NotificationSenderNoDefault,
		}
	}
	return nil
}
