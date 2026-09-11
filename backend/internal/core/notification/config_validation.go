package notification

import (
	"context"
	"log/slog"
	"os"
	"strings"

	"github.com/orkestra/backend/internal/core/notification/services"
	"github.com/orkestra/backend/pkg/sdk/module"
)

var (
	_ module.HasConfigValidator           = (*NotificationModule)(nil)
	_ module.HasConfigActivationValidator = (*NotificationModule)(nil)
)

// requireOneClickKey holds the RFC 8058 one-click unsubscribe requirement.
// Declared in ConfigSchema with Default "true": on unless an operator says
// otherwise, and absent means on.
const requireOneClickKey = "require_one_click_unsubscribe"

// ValidateConfig rejects a broken sender routing map at the PATCH boundary
// (active-config and named-environment PATCH both funnel here). The rules
// are vacuous while no profile declares a pattern — see
// services.ValidateSenderConfig for the three states (ADR-0019 D5).
func (m *NotificationModule) ValidateConfig(_ context.Context, merged map[string]string) error {
	return services.ValidateSenderConfig(merged, m.driverRegistry(), m.oneClickPolicy(merged))
}

// ValidateConfigActivation applies the same policy to the whole target
// profile before an environment switch, so sandbox → production cannot
// activate a map that is broken in the third state.
func (m *NotificationModule) ValidateConfigActivation(_ context.Context, target map[string]string) error {
	return services.ValidateSenderConfig(target, m.driverRegistry(), m.oneClickPolicy(target))
}

// livePolicy is what the RUNNING service consults on every marketing
// decision (module.go wires it as Options.OneClickSource). It must be a live
// read, not a value captured in Init: this module declares
// HotReloadConfig() == true, so a config write leaves no restart-required
// flag, and a captured value would leave an operator who has just switched
// require_one_click_unsubscribe ON still sending marketing with no
// unsubscribe header — stale in the fail-OPEN direction.
//
// A read that fails yields an empty map, which oneClickPolicy resolves
// through the schema fallback to the "true" default: unreadable
// configuration means the requirement is in force, never that it is off.
// UnsubscribePageURL resolves the same way — empty on an unreadable
// configuration, which NotificationService.unsubscribeURL treats exactly
// like "not configured": the footer falls back to the API link, never a
// stale or broken page URL.
//
// Cost is one config read per marketing send for the one-click decision
// (Waived + PublicAPIBaseURL), and — since UnsubscribePageURL rides on this
// same struct — one additional read per TEMPLATED send of ANY type, because
// the footer link this field feeds renders on transactional templates too.
// A non-templated Send never calls this at all, matching the sender
// resolver's snapshot cost model.
func (m *NotificationModule) livePolicy(ctx context.Context) services.OneClickPolicy {
	var values map[string]string
	if m.configService != nil {
		if doc, err := m.configService.GetConfig(ctx, "notification"); err == nil && doc != nil {
			values = doc.ActiveConfigValues()
		}
	}
	return m.oneClickPolicy(values)
}

// oneClickPolicy reads the one-click unsubscribe requirement — and, riding
// along on the same struct, the optional hosted unsubscribe page URL — out
// of a config map the way the RUNTIME reads it. The same function serves
// the save-time validator and livePolicy above, so validation and dispatch
// cannot resolve the same keys differently. Refusing a save over a base URL
// the deployment supplies through NOTIFICATION_PUBLIC_API_BASE_URL would be
// a false alarm, and treating an absent require_one_click_unsubscribe as
// "off" would let a document written before the field existed slip past the
// rule it was added for.
//
// Waived is the inverse of the field, and only an explicit, recognisably
// FALSE value turns the requirement off — so anything unparseable, like a
// typo or the empty string, lands on "required" rather than "off". See
// oneClickWaived for why this one key does not use the platform's ordinary
// boolean parsing.
//
// UnsubscribePageURL has no such inversion or admissibility rule of its
// own — effectiveValue's ordinary stored→EnvVar→Default resolution is
// enough, because NotificationService.unsubscribeURL already treats
// anything that is not a bare https origin (including empty, and including
// a value that is whitespace-only after TrimSpace) as "not configured" and
// falls back to the API link.
func (m *NotificationModule) oneClickPolicy(values map[string]string) services.OneClickPolicy {
	return services.OneClickPolicy{
		Waived:             oneClickWaived(m.effectiveValue(values, requireOneClickKey)),
		PublicAPIBaseURL:   m.effectiveValue(values, services.PublicAPIBaseURLField),
		UnsubscribePageURL: m.effectiveValue(values, services.UnsubscribePageURLField),
	}
}

// effectiveValue resolves one scalar config key the way ModuleConfigService
// resolves it at runtime: the stored value, else the schema's EnvVar, else
// the schema's Default. The legacy validator hooks receive the raw stored
// map, so this fallback has to be applied here for validation and runtime to
// agree.
func (m *NotificationModule) effectiveValue(values map[string]string, key string) string {
	if v := strings.TrimSpace(values[key]); v != "" {
		return v
	}
	for _, f := range m.ConfigSchema() {
		if f.Key != key {
			continue
		}
		if f.EnvVar != "" {
			if v := os.Getenv(f.EnvVar); v != "" {
				return v
			}
		}
		return f.Default
	}
	return ""
}

// oneClickWaived reports whether a stored require_one_click_unsubscribe
// value turns the RFC 8058 requirement OFF.
//
// It deliberately does NOT mirror module.Dependencies.GetConfigBool, which
// tests for a recognisably TRUE value ("true", "1", "yes") and resolves
// everything else — "TRUE", "True", "on", "enabled", a typo — to false. For
// every other boolean on the platform that fallback is harmless: an
// unrecognised value leaves a feature off. Here "off" is the single outcome
// this whole requirement exists to prevent, because it means marketing
// leaves the building with no unsubscribe header, and a message already in
// a mailbox cannot be recalled. So the test is inverted: only an explicitly
// false-looking value waives the requirement, and anything else — a
// recognisably true value, an unrecognised one, or the empty string — keeps
// it in force. The false-looking set is the one the rest of the codebase
// already uses for config booleans (see readBool in the auth module), read
// case-insensitively because an operator who types "FALSE" into the admin
// form has plainly said off.
func oneClickWaived(v string) bool {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "false", "0", "no":
		return true
	}
	return false
}

// driverRegistry returns the registry Init built, or a default one for a
// module that was never initialized (declaration tests, validation before
// Init). Both carry the same driver names and requirements; only the
// logger differs.
func (m *NotificationModule) driverRegistry() *services.DriverRegistry {
	if m.drivers == nil {
		m.drivers = services.NewDriverRegistry(services.CoreDrivers(slog.Default())...)
	}
	return m.drivers
}
