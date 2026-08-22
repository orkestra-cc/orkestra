package services

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"time"

	"github.com/orkestra/backend/internal/core/auth/models"
	"github.com/orkestra/backend/pkg/sdk/metrics"
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
// It returns ErrSessionEnforcementUnavailable when the configuration
// could not be READ at all. That is not the same as "no value stored":
// an unreadable module_configs document says nothing about whether the
// operator disabled the cap, and answering with the 30-day default in
// that state would silently re-arm a cap a deployment had deliberately
// turned off — irreversibly signing out every session older than 30 days
// that happens to refresh during the outage. Every other failure on this
// path fails closed to 503; this one used to be the single exception that
// substituted a different policy instead. Callers must propagate the
// error rather than treating a zero duration as "disabled".
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
func (s *AuthPolicyService) SessionAbsoluteTTL(ctx context.Context) (time.Duration, error) {
	if s == nil || s.cs == nil {
		return DefaultSessionAbsoluteTTL, nil
	}
	raw, present, err := s.cs.GetRawValue(ctx, "auth", "sessionAbsoluteTTL")
	if err != nil {
		slogDefault().ErrorContext(ctx, "session cap: policy read failed",
			slog.String("outcome", "fail_closed"),
			slog.String("key", "sessionAbsoluteTTL"),
			slog.String("error", err.Error()))
		return 0, ErrSessionEnforcementUnavailable
	}
	if !present {
		return DefaultSessionAbsoluteTTL, nil
	}
	if strings.TrimSpace(raw) == "" {
		return 0, nil
	}
	return clampPersistedDuration(raw, DefaultSessionAbsoluteTTL,
		MinSessionAbsoluteTTL, MaxSessionAbsoluteTTL, "sessionAbsoluteTTL", slogDefault()), nil
}

// ErrSessionMaxAgeReached means the session hit its configured absolute
// lifetime and has been terminated. It is a LOGOUT, not a denial: by the
// time a caller sees this sentinel the session's refresh tokens are
// revoked, the session document is inactive, and the sid is on the
// revocation denylist. ADR-0017 D4.
var ErrSessionMaxAgeReached = errors.New("session maximum age reached")

// ErrSessionEnforcementUnavailable means the cap could not be evaluated
// or could not be applied because durable storage failed. It fails
// CLOSED — no credentials are minted — and maps to 503, never to a 401.
// Reporting an outage as an authentication failure would train clients to
// discard a session that is still perfectly valid.
var ErrSessionEnforcementUnavailable = errors.New("session enforcement unavailable")

// sessionCapAnchor resolves the instant the cap is measured from, or a
// non-empty anomaly kind when the row cannot supply one.
//
// StartedAt is the anchor: CreateSession stamps it unconditionally and
// rotation preserves the session UUID, so it is the login time and it
// survives every refresh. CreatedAt is the compatibility fallback for
// rows written before that guarantee, and using it is NOT an anomaly —
// counting a row that has a usable anchor would poison the 30-day
// observation window that gates the fail-closed change.
func sessionCapAnchor(sess *models.AuthSessionDoc) (time.Time, string) {
	if sess == nil {
		return time.Time{}, "missing"
	}
	if !sess.StartedAt.IsZero() {
		return sess.StartedAt, ""
	}
	if !sess.CreatedAt.IsZero() {
		return sess.CreatedAt, ""
	}
	return time.Time{}, "zero_timestamp"
}

// sessionWithinAbsoluteCap returns nil while the session may keep
// refreshing, ErrSessionMaxAgeReached once it has been terminated for
// age, or ErrSessionEnforcementUnavailable when durable state failed.
//
// Failure precedence is explicit and load-bearing:
//   - a repository ERROR loading the session, or revoking durable
//     refresh/session state, fails closed and never mints credentials;
//   - only a clean (nil, nil) lookup follows the temporary compatibility
//     rule below;
//   - a Redis denylist failure AFTER durable revocation returns
//     SessionRevocationDegradedError — durable logout happened, so the
//     caller must still clear the cookie, but the response must not claim
//     a completely recorded cap expiry;
//   - the cap event and counter are emitted only after durable state is
//     terminated, and only by the caller that won the transition.
//
// COMPATIBILITY WINDOW — ADR-0017, remove in the first minor release
// after at least 30 consecutive production days with
// orkestra_auth_session_anchor_anomalies_total at zero in every supported
// environment. Tracking issue: orkestra-cc/orkestra#277. D2's
// invariant makes an absent session document impossible for credentials
// issued by current code, but invariants bind only the code written after
// them and older rows cannot be assumed to comply. If the counter moves,
// classify and repair the data cause before restarting the window.
func (s *authService) sessionWithinAbsoluteCap(ctx context.Context, sessionUUID string) error {
	// No session repository is a wiring shape, not a data anomaly. An
	// empty session UUID is not one either: requireSessionContext in
	// jwt_service.go already refuses to mint from a row without one, so
	// such a row can never yield credentials — counting it here would
	// put permanent noise in the very counter that gates tightening the
	// compatibility rule below to fail-closed.
	if s.authSessionRepo == nil || sessionUUID == "" {
		return nil
	}
	// A failed policy read is an outage, not a configuration. Falling
	// through to the default here would re-arm a cap an operator had
	// disabled and log out every session older than it — see
	// SessionAbsoluteTTL. Fail closed, exactly like an unreadable session
	// store two statements below.
	maxAge, err := s.policy.SessionAbsoluteTTL(ctx)
	if err != nil {
		return ErrSessionEnforcementUnavailable
	}
	if maxAge <= 0 {
		// Disabled: skip the query entirely. This is the exit for a fork
		// that does not want the cap, and it must cost nothing.
		return nil
	}

	sess, err := s.authSessionRepo.GetByUUID(ctx, sessionUUID)
	if err != nil {
		slogDefault().ErrorContext(ctx, "session cap: anchor lookup failed",
			slog.String("outcome", "fail_closed"),
			slog.String("error", err.Error()))
		return ErrSessionEnforcementUnavailable
	}

	anchor, anomaly := sessionCapAnchor(sess)
	if anomaly != "" {
		metrics.Default().RecordSessionAnchorAnomaly(anomaly)
		slogDefault().WarnContext(ctx, "session cap: no usable anchor, permitting refresh under the ADR-0017 compatibility window",
			slog.String("kind", anomaly))
		return nil
	}
	if time.Since(anchor) < maxAge {
		return nil
	}
	return s.expireSessionForMaxAge(ctx, sess)
}

// expireSessionForMaxAge performs the same three durable steps as an
// administrative termination — revoke the session's refresh tokens, flip
// the session document inactive, push the sid onto the denylist — and
// records the event exactly once. Revoking refresh rows is idempotent, so
// only the isActive transition needs to name a winner.
func (s *authService) expireSessionForMaxAge(ctx context.Context, sess *models.AuthSessionDoc) error {
	if s.refreshTokenRepo != nil {
		if err := s.refreshTokenRepo.RevokeTokensBySession(ctx, sess.UUID, models.RevokeReasonSessionMaxAge); err != nil {
			slogDefault().ErrorContext(ctx, "session cap: refresh revocation failed",
				slog.String("outcome", "fail_closed"),
				slog.String("error", err.Error()))
			return ErrSessionEnforcementUnavailable
		}
	}

	won, err := s.authSessionRepo.ExpireSessionForMaxAge(ctx, sess.UUID)
	if err != nil {
		slogDefault().ErrorContext(ctx, "session cap: session termination failed",
			slog.String("outcome", "fail_closed"),
			slog.String("error", err.Error()))
		return ErrSessionEnforcementUnavailable
	}

	var degraded error
	if s.sessionRevocation != nil {
		if err := s.sessionRevocation.Revoke(ctx, sess.UUID, models.RevokeReasonSessionMaxAge); err != nil {
			degraded = &SessionRevocationDegradedError{Cause: err}
		}
	}

	if won {
		metrics.Default().RecordSessionCapExpiry()
		s.recordSessionCapEvent(ctx, sess.UserUUID)
	}
	if degraded != nil {
		// Durable logout completed; only the short-lived denylist is
		// behind. The caller still clears the cookie, but must not report
		// a cleanly recorded cap expiry.
		return degraded
	}
	return ErrSessionMaxAgeReached
}

// recordSessionCapEvent writes the security-event row. A failure here
// cannot restore credentials — durable state is already terminated — so
// it increments a counter and logs without PII rather than propagating.
func (s *authService) recordSessionCapEvent(ctx context.Context, userUUID string) {
	if s.securityEventRepo == nil || userUUID == "" {
		return
	}
	ip, _ := ipFromCtx(ctx)
	event := &models.SecurityEvent{
		UserUUID:  userUUID,
		EventType: "session_max_age_reached",
		IPAddress: ip,
		Success:   true,
		Timestamp: time.Now().UTC(),
	}
	if err := s.securityEventRepo.Insert(ctx, event); err != nil {
		metrics.Default().RecordSessionCapEventFailure()
		slogDefault().WarnContext(ctx, "session cap: security event persist failed",
			slog.String("error", err.Error()))
	}
}
