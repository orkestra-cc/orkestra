package services

import (
	"context"
	"strings"
	"time"
)

// Absolute session lifetime. The refresh TTL is an IDLE timeout —
// rotation writes a fresh `now + refreshTTL` on every use, so seven days
// of inactivity ends a session but an active user is never asked to
// re-authenticate. These constants bound the total age of a session,
// measured from login, independently of activity. ADR-0017 D1.
const (
	// DefaultSessionAbsoluteTTL is 30 days, written in hours for
	// readability against time.Duration. The shared parser accepts "30d"
	// equally, so an operator may type either.
	DefaultSessionAbsoluteTTL = 720 * time.Hour
	MinSessionAbsoluteTTL     = time.Hour
	// MaxSessionAbsoluteTTL leaves SessionRetentionSafetyMargin below
	// AuthSessionRetention. Equality is not safe: at exactly the
	// retention boundary Mongo's TTL monitor may delete the session
	// document before the refresh path evaluates the cap, which would
	// present an expired session as a missing anchor.
	MaxSessionAbsoluteTTL = 89 * 24 * time.Hour
	// SessionRetentionSafetyMargin is the gap the invariant test pins.
	SessionRetentionSafetyMargin = 24 * time.Hour
)

// SessionAbsoluteTTL returns the maximum age a session may reach before
// the user must authenticate again, or 0 when the cap is disabled.
//
// This distinguishes three states via GetRawValue's presence flag, which
// GetValue cannot express (it returns "" for both an absent key and a
// cleared one — see ModuleConfigService.GetValue and the doc comment on
// GetRawValue):
//
//   - Key ABSENT — "never configured". Every module_configs document
//     seeded since this field was declared already carries "720h" (the
//     schema Default written by buildInitialConfig), so this state means
//     either a document older than this field or a test double with no
//     opinion. Takes the 30-day default: the base must be secure out of
//     the box, and a cap that shipped disabled would be adopted by nobody
//     who had not already identified the gap. A nil policy service takes
//     the same default.
//   - Key present, value empty/blank — the operator explicitly cleared
//     the field. DISABLED — the supported exit for a fork that does not
//     want the cap, without patching code — and skips the session query
//     entirely.
//   - Key present, non-empty value — parsed and clamped to
//     [MinSessionAbsoluteTTL, MaxSessionAbsoluteTTL] like every other
//     persisted auth duration.
//
// ADR-0017 D1.
func (s *AuthPolicyService) SessionAbsoluteTTL(ctx context.Context) time.Duration {
	if s == nil || s.cs == nil {
		return DefaultSessionAbsoluteTTL
	}
	raw, present := s.cs.GetRawValue(ctx, "auth", "sessionAbsoluteTTL")
	if !present {
		return DefaultSessionAbsoluteTTL
	}
	if strings.TrimSpace(raw) == "" {
		return 0
	}
	return clampPersistedDuration(raw, DefaultSessionAbsoluteTTL,
		MinSessionAbsoluteTTL, MaxSessionAbsoluteTTL, "sessionAbsoluteTTL", slogDefault())
}
