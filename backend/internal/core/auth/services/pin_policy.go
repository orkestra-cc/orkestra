package services

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// PIN sign-in policy.
const (
	// MinPINLength / MaxPINLength bound auth.pinLength. A 4-digit PIN
	// is 10⁴ combinations: the lockout, the device binding and the
	// consecutive-failure cap are what make that tolerable.
	MinPINLength     = 4
	MaxPINLength     = 8
	DefaultPINLength = 5

	// DefaultPINAccessTokenTTL is the access-token lifetime of a PIN
	// session when auth.pinAccessTokenTTL is empty.
	DefaultPINAccessTokenTTL = 15 * time.Minute

	// DefaultPINRefreshTokenTTL is the idle timeout of a PIN session (one
	// shift) when auth.pinRefreshTokenTTL is empty.
	DefaultPINRefreshTokenTTL = 12 * time.Hour
	// MinPINRefreshTokenTTL / MaxPINRefreshTokenTTL bound the idle timeout:
	// below 15 minutes a field app would sign the worker out mid-task;
	// above 7 days a PIN session would outlive a password one
	// (JWT_REFRESH_TOKEN_EXPIRY defaults to 7d).
	MinPINRefreshTokenTTL = 15 * time.Minute
	MaxPINRefreshTokenTTL = 7 * 24 * time.Hour

	// PINMaxConsecutiveFailures removes the PIN outright after this many
	// wrong guesses with no successful PIN sign-in in between. The counter
	// lock alone lets a paced attacker keep guessing forever; ten guesses
	// in a 10⁴ space is a 0.1% chance, and an administrator (or the user,
	// with a stronger credential) sets a new PIN.
	PINMaxConsecutiveFailures = 10

	// PINEnrollmentCodeTTL is how long a device enrollment code stays
	// redeemable.
	PINEnrollmentCodeTTL = 15 * time.Minute
)

func pinLoginKeyFor(audience PolicyAudience) (string, error) {
	switch audience {
	case PolicyAudienceOperator:
		return "pinLoginEnabledAdmin", nil
	case PolicyAudienceClient:
		return "pinLoginEnabledClient", nil
	}
	return "", fmt.Errorf("%w: unknown policy audience %q", ErrAuthPolicyUnavailable, string(audience))
}

// PINLoginEnabled is the strict per-surface toggle of the PIN method.
// Unlike the password toggle an ABSENT key means false: the method is
// opt-in per tenant and surface (spec §2.3). A missing document, a
// malformed value or an unwired reader is ErrAuthPolicyUnavailable.
func (s *AuthPolicyService) PINLoginEnabled(ctx context.Context, audience PolicyAudience) (bool, error) {
	if s == nil || s.cs == nil {
		return false, fmt.Errorf("%w: policy service not wired", ErrAuthPolicyUnavailable)
	}
	key, err := pinLoginKeyFor(audience)
	if err != nil {
		return false, err
	}
	return s.strictBoolKey(ctx, key, false)
}

// PINRequiresTrustedDevice is the strict device-binding toggle:
// absent means true, malformed or unreadable is an error so a degraded
// config never silently drops the binding.
func (s *AuthPolicyService) PINRequiresTrustedDevice(ctx context.Context) (bool, error) {
	if s == nil || s.cs == nil {
		return false, fmt.Errorf("%w: policy service not wired", ErrAuthPolicyUnavailable)
	}
	return s.strictBoolKey(ctx, "pinRequiresTrustedDevice", true)
}

func (s *AuthPolicyService) strictBoolKey(ctx context.Context, key string, absent bool) (bool, error) {
	raw, present, err := s.cs.GetRawValueRequiredModule(ctx, "auth", key)
	if err != nil {
		return false, fmt.Errorf("%w: read %s: %w", ErrAuthPolicyUnavailable, key, err)
	}
	if !present {
		return absent, nil
	}
	v, err := StrictBool(raw)
	if err != nil {
		return false, fmt.Errorf("%w: %s is not a canonical boolean", ErrAuthPolicyUnavailable, key)
	}
	return v, nil
}

// PINLength is the number of digits a NEW PIN must have. Values
// outside 4–8 or unparsable fall back to the default; the PATCH boundary
// refuses them in the first place.
func (s *AuthPolicyService) PINLength(ctx context.Context) int {
	if s == nil || s.cs == nil {
		return DefaultPINLength
	}
	v := strings.TrimSpace(s.cs.GetValue(ctx, "auth", "pinLength"))
	n, err := strconv.Atoi(v)
	if err != nil || n < MinPINLength || n > MaxPINLength {
		return DefaultPINLength
	}
	return n
}

// PINAccessTokenTTL is the access-token lifetime of a PIN session.
func (s *AuthPolicyService) PINAccessTokenTTL(ctx context.Context) time.Duration {
	if s == nil || s.cs == nil {
		return DefaultPINAccessTokenTTL
	}
	v := strings.TrimSpace(s.cs.GetValue(ctx, "auth", "pinAccessTokenTTL"))
	if v == "" {
		return DefaultPINAccessTokenTTL
	}
	return clampPersistedDuration(v, DefaultPINAccessTokenTTL, MinAccessTokenTTL, MaxAccessTokenTTL, "pinAccessTokenTTL", slogDefault())
}

// PINRefreshTokenTTL is the idle timeout of a PIN session.
func (s *AuthPolicyService) PINRefreshTokenTTL(ctx context.Context) time.Duration {
	if s == nil || s.cs == nil {
		return DefaultPINRefreshTokenTTL
	}
	v := strings.TrimSpace(s.cs.GetValue(ctx, "auth", "pinRefreshTokenTTL"))
	if v == "" {
		return DefaultPINRefreshTokenTTL
	}
	return clampPersistedDuration(v, DefaultPINRefreshTokenTTL, MinPINRefreshTokenTTL, MaxPINRefreshTokenTTL, "pinRefreshTokenTTL", slogDefault())
}
