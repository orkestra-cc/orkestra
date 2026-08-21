package auth

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/orkestra/backend/internal/core/auth/services"
	"github.com/orkestra/backend/internal/shared/utils"
	"github.com/orkestra/backend/pkg/sdk/module"
)

var _ module.HasConfigValidator = (*AuthModule)(nil)

// durationBound is one admin-editable duration that governs credentials
// already in circulation, and therefore cannot be left unbounded.
type durationBound struct {
	key      string
	min, max time.Duration
	// why is appended to the operator-facing 422 message so the bound
	// reads as a reason rather than an arbitrary refusal.
	why string
}

// authDurationBounds intentionally omits accountLockoutDuration and
// accountLockoutThreshold: neither governs an already-issued credential,
// and an absurd value there is self-punishing rather than exploitable.
// ADR-0017 D6.
var authDurationBounds = []durationBound{
	{
		key: "accessTokenTTL",
		min: services.MinAccessTokenTTL, max: services.MaxAccessTokenTTL,
		why: "below a minute the SPA enters a refresh loop; above 24h a token would outlive its own revocation entry",
	},
	{
		key: "passwordResetTokenTTL",
		min: services.MinPasswordResetTokenTTL, max: services.MaxPasswordResetTokenTTL,
		why: "below five minutes the link dies before the mail is delivered",
	},
	{
		key: "sessionAbsoluteTTL",
		min: services.MinSessionAbsoluteTTL, max: services.MaxSessionAbsoluteTTL,
		why: "the maximum leaves a one-day margin below the 90-day session retention window, so retention can never delete the anchor of a session still inside the cap",
	},
}

// ValidateConfig rejects malformed or out-of-range duration values before
// they are persisted, so the value the admin UI displays is the value in
// force. It is the write half of ADR-0017 D6; the read half is the
// clamping in services.clampPersistedDuration, which stays as a defence
// for values written by an older release or directly to Mongo.
//
// An empty value is always accepted: emptiness is a decision with a
// field-specific meaning (accessTokenTTL falls through to the
// environment, passwordResetTokenTTL uses its 30-minute default,
// sessionAbsoluteTTL disables the cap), never an omission to reject.
func (m *AuthModule) ValidateConfig(_ context.Context, values map[string]string) error {
	for _, b := range authDurationBounds {
		raw, present := values[b.key]
		if !present {
			continue
		}
		raw = strings.TrimSpace(raw)
		if raw == "" {
			continue
		}
		d, ok := utils.ParseDuration(raw)
		if !ok || d <= 0 {
			return &module.ConfigValidationError{
				Field:   b.key,
				Message: fmt.Sprintf("must be a positive duration such as 15m, 2h or 30d (%s)", b.why),
			}
		}
		if d < b.min || d > b.max {
			return &module.ConfigValidationError{
				Field:   b.key,
				Message: fmt.Sprintf("must be between %s and %s (%s)", b.min, b.max, b.why),
			}
		}
	}
	return nil
}
