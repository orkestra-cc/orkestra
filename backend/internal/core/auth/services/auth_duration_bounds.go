package services

import (
	"log/slog"
	"time"

	"github.com/orkestra/backend/internal/shared/utils"
)

// Bounds on the admin-managed durations that govern already-issued
// credentials. ADR-0017 D6 enforces these at the PATCH boundary (422);
// the readers below are the second line of defence for values persisted
// by an older release or written directly to Mongo, where rejecting the
// value would lock the operator out of the admin UI instead of fixing it.
const (
	// MaxAccessTokenTTL is the ceiling on the EFFECTIVE access-token
	// lifetime — not merely on the admin field. The Redis revocation
	// denylist stores every entry for this value plus a clock-skew
	// minute, so a token that could outlive it would be accepted again
	// after its own revocation entry expired. The bound therefore also
	// applies to JWT_ACCESS_TOKEN_EXPIRY and to direct NewJWTService
	// callers. ADR-0017 D5.
	MaxAccessTokenTTL = 24 * time.Hour
	// MinAccessTokenTTL: below a minute the SPA enters a refresh loop.
	MinAccessTokenTTL = time.Minute

	// MinPasswordResetTokenTTL: below five minutes the link dies before
	// the mail is delivered.
	MinPasswordResetTokenTTL = 5 * time.Minute
	MaxPasswordResetTokenTTL = 24 * time.Hour
)

// clampPersistedDuration resolves a duration that is ALREADY persisted.
// `fallback` is returned when raw is non-empty but unparsable or
// non-positive; values outside [min,max] saturate to the nearest bound.
// Every correction logs at Warn with the key and the discarded value so
// an operator can find and repair the stored data.
//
// Empty input is deliberately the CALLER's business: the three auth
// duration keys give emptiness three different meanings (accessTokenTTL
// falls through to the environment, passwordResetTokenTTL uses 30m,
// sessionAbsoluteTTL disables the cap), and folding that into one
// signature would hide the distinction the input contract turns on.
func clampPersistedDuration(raw string, fallback, min, max time.Duration, key string, log *slog.Logger) time.Duration {
	if log == nil {
		log = slog.Default()
	}
	d, ok := utils.ParseDuration(raw)
	if !ok || d <= 0 {
		log.Warn("auth: unusable persisted duration, using default",
			slog.String("key", key),
			slog.String("value", raw),
			slog.String("using", fallback.String()))
		return fallback
	}
	if d < min {
		log.Warn("auth: persisted duration below minimum, clamping",
			slog.String("key", key),
			slog.String("value", raw),
			slog.String("using", min.String()))
		return min
	}
	if d > max {
		log.Warn("auth: persisted duration above maximum, clamping",
			slog.String("key", key),
			slog.String("value", raw),
			slog.String("using", max.String()))
		return max
	}
	return d
}
