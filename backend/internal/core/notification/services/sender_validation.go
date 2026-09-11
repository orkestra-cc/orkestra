package services

import (
	"fmt"
	"strings"

	"github.com/orkestra/backend/internal/core/notification/models"
	"github.com/orkestra/backend/internal/shared/errcode"
	"github.com/orkestra/backend/pkg/sdk/module"
)

// allowedSendTypes is the closed set a profile's allowed_types may declare
// (ADR-0021 D2). Compared lowercase, no repair: a value that is not exactly
// one of these after decode's normalization is rejected, not coerced.
var allowedSendTypes = map[string]bool{"marketing": true, "transactional": true}

// PublicAPIBaseURLField is the module-level config key the one-click
// unsubscribe header is built on. Named here because the save-time gate
// reports against it when a marketing profile has nothing to build a link
// from, and module.go declares it — one spelling, two readers.
const PublicAPIBaseURLField = "public_api_base_url"

// UnsubscribePageURLField is the module-level config key naming the optional
// hosted page a person reaches by clicking the unsubscribe link in an
// email's footer — as opposed to PublicAPIBaseURLField above, which only a
// mail client's own automatic one-click button ever talks to. module.go
// declares it in ConfigSchema; NotificationService.Options.UnsubscribePageURL
// carries the resolved value through to SendTemplated's
// {{.UnsubscribeURL}} footer.
const UnsubscribePageURLField = "unsubscribe_page_url"

// OneClickPolicy is the RFC 8058 one-click unsubscribe requirement as one
// value. The save-time gate and the dispatch chokepoint share it, so they
// agree on what "one-click is guaranteed" MEANS — but not on what they apply
// it to, and that asymmetry is deliberate rather than an oversight:
//
//	save     judges a profile's allowed_types, the only save-time
//	         declaration that says marketing may be sent through it;
//	dispatch judges the SEND, so a marketing message routed to a profile by
//	         category pattern — which declares no type, and which no
//	         save-time rule could have judged — is refused too.
//
// A routing-only profile therefore saves cleanly and still has its marketing
// sends refused. Closing that at save time would mean refusing every routing
// profile whenever the module-wide base URL is empty, which would lock an
// operator out of the very PATCH that sets it.
//
// Waived is stored INVERTED from the require_one_click_unsubscribe config
// field on purpose: the zero value must fail closed. A caller that builds
// this struct without thinking about the requirement gets "one-click is
// required", never "marketing may go out without an unsubscribe header" —
// the same reasoning that makes a nil opt-out repository a refusal rather
// than a skipped check at the dispatch chokepoint.
type OneClickPolicy struct {
	Waived           bool
	PublicAPIBaseURL string

	// UnsubscribePageURL rides along on this same struct purely so it
	// hot-reloads through the same mechanism as the two fields above
	// (module.go's livePolicy, read fresh on every call rather than
	// captured at Init) — it plays NO part in gap() or oneClickAdmissible()
	// below. The RFC 8058 header those compute is unrelated to whether a
	// hosted unsubscribe page is configured: this field is consulted only
	// by NotificationService.SendTemplated, to build the FOOTER link a
	// person clicks, never the header a mail client's one-click button
	// POSTs to — and, like the rest of this struct, only for a MARKETING
	// send; a transactional one never resolves this struct at all and
	// builds its footer from the static Options.UnsubscribePageURL field
	// instead.
	UnsubscribePageURL string
}

// oneClickGap names what stops a marketing message from carrying a one-click
// unsubscribe. The zero value is "nothing does", so a gap is always an
// explicit finding.
type oneClickGap int

const (
	oneClickSatisfied oneClickGap = iota
	// oneClickDriverCannot: the driver does not put arbitrary headers on the
	// wire, so no configuration of this platform can make the message carry
	// one.
	oneClickDriverCannot
	// oneClickNoPublicBaseURL: public_api_base_url is empty, or is not the
	// bare https origin oneClickBase requires, so there is no address to
	// point the header at.
	oneClickNoPublicBaseURL
)

// gap judges one driver under this policy. The driver is reported first
// because it is the profile's own choice and the operator fixes it on the
// profile; the base URL is module-wide and one setting repairs every profile
// at once.
func (p OneClickPolicy) gap(caps DriverCapabilities) oneClickGap {
	if p.Waived {
		return oneClickSatisfied
	}
	if !caps.ListUnsubscribeHeaders {
		return oneClickDriverCannot
	}
	if _, status := oneClickBase(p.PublicAPIBaseURL); status != baseURLUsable {
		return oneClickNoPublicBaseURL
	}
	return oneClickSatisfied
}

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
// oneClick is the RFC 8058 requirement in force. Within loadBearing it adds
// one rule to the two above: a profile whose allowed_types names marketing
// is refused when nothing could put a one-click unsubscribe on the mail it
// would carry. See oneClickAdmissible for why it judges allowed_types and
// not patterns.
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
func ValidateSenderConfig(values map[string]string, drivers *DriverRegistry, oneClick OneClickPolicy) error {
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
		if err := oneClickAdmissible(p, d, oneClick); err != nil {
			return err
		}
	}
	return nil
}

// oneClickAdmissible refuses a profile that declares marketing while nothing
// could put an RFC 8058 one-click unsubscribe on the messages it would carry.
// This is the point of the whole rule: the operator finds out while saving
// the profile, when a checkbox still fixes it, rather than after a campaign
// has gone out with no unsubscribe header and the mailbox providers have
// started treating the domain as a spammer.
//
// It judges allowed_types, and only allowed_types, because that is the one
// declaration at save time that says "marketing may be sent through this
// profile". Category patterns route by CATEGORY and carry no type, so no
// save-time rule can tell which of them a marketing campaign will use; the
// dispatch chokepoint's own check (see notification_service.go) is what
// covers a marketing send that arrives through pattern routing.
func oneClickAdmissible(p SenderProfile, d EmailDriver, policy OneClickPolicy) *module.ConfigValidationError {
	if !typeAllowed(p, models.TypeMarketing) {
		return nil
	}
	switch policy.gap(d.Capabilities()) {
	case oneClickDriverCannot:
		return &module.ConfigValidationError{
			Field: module.ItemKey(SendersField, p.Slug, SubProvider),
			Message: fmt.Sprintf("the %s provider cannot add the one-click unsubscribe headers that marketing email must carry, so this profile may not be used for marketing. "+
				"Choose a provider that can add them, remove marketing from this profile's send types, or turn off \"Require one-click unsubscribe\" to send marketing without it.", p.Provider),
			Code: errcode.NotificationSenderDriverNoOneClick,
		}
	case oneClickNoPublicBaseURL:
		return &module.ConfigValidationError{
			Field: PublicAPIBaseURLField,
			Message: fmt.Sprintf("a one-click unsubscribe link needs the public API base URL, and it is not set to this API's own https origin (for example https://api.example.com), so profile %q may not be used for marketing. "+
				"Set it, remove marketing from that profile's send types, or turn off \"Require one-click unsubscribe\" to send marketing without an unsubscribe link.", p.Slug),
			Code: errcode.NotificationPublicBaseURLMissing,
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
