package auth

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/orkestra/backend/internal/core/auth/models"
	"github.com/orkestra/backend/internal/core/auth/services"
	"github.com/orkestra/backend/internal/shared/errcode"
	"github.com/orkestra/backend/internal/shared/utils"
	"github.com/orkestra/backend/pkg/sdk/module"
)

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

var _ module.HasConfigSnapshotValidator = (*AuthModule)(nil)

// ValidateConfigSnapshot judges the complete target snapshot on all three
// mutation surfaces (active PATCH, named-environment PATCH, activation —
// spec §4.5): the duration bounds of ADR-0017 D6, then the login-method
// anti-lockout invariant of the password-login toggle (spec §4.4). The
// legacy value-only ValidateConfig hook is gone: the invariant depends on
// secret PRESENCE, which only the snapshot carries — no secret value
// crosses this boundary.
func (m *AuthModule) ValidateConfigSnapshot(_ context.Context, snap module.ConfigValidationSnapshot) error {
	if err := validateAuthDurations(snap.Values); err != nil {
		return err
	}
	if err := validateLockoutThresholdOrder(snap.Values); err != nil {
		return err
	}
	return validateLoginMethodInvariant(snap, services.ReadableNonEmptyFile)
}

// validateLockoutThresholdOrder enforces
// ipLockoutThreshold >= accountLockoutThreshold on the TARGET snapshot.
//
// Both sides are resolved through snapshotInt exactly the way the runtime
// accessors (AuthPolicyService.LockoutThreshold / IPLockoutThreshold)
// resolve them: absent, blank, malformed or non-positive all fall back to
// the schema default (5 / 100). That mirroring is load-bearing, not
// cosmetic — the accessors never reject a bad value, they silently
// substitute the default, so a rule that instead SKIPPED on a bad value
// would let a PATCH like {"accountLockoutThreshold":"0",
// "ipLockoutThreshold":"3"} through: the write looks unrelated to the
// invariant, but at read time it resolves to account=5, ip=3 — the exact
// oracle this rule exists to block. Comparing anything other than the
// values the platform will actually enforce defeats the rule.
func validateLockoutThresholdOrder(values map[string]string) error {
	account := snapshotInt(values, "accountLockoutThreshold", 5)
	ip := snapshotInt(values, "ipLockoutThreshold", 100)
	if ip >= account {
		return nil
	}
	return &module.ConfigValidationError{
		Field: "ipLockoutThreshold",
		Code:  errcode.AuthIPThresholdBelowAccount,
		Message: fmt.Sprintf(
			"must be at least the account threshold (%d): an address that locks before the account does turns a shared office or VPN egress into an oracle for which accounts exist behind it",
			account),
	}
}

// snapshotInt reads a positive integer from the snapshot, resolving to
// def on every non-positive input: absent, blank, malformed, zero or
// negative. This is deliberately the SAME fallback the runtime accessors
// apply (LockoutThreshold, IPLockoutThreshold) — snapshotInt has no
// "invalid, skip me" outcome of its own, because the accessors don't
// have one either. A validator that treated a bad value as "unreadable,
// ignore it" would be judging a state the runtime can never actually be
// in.
func snapshotInt(values map[string]string, key string, def int) int {
	raw, present := values[key]
	if !present {
		return def
	}
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return def
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n < 1 {
		return def
	}
	return n
}

// validateAuthDurations is the ValidateConfig loop verbatim: an empty value
// is always accepted (emptiness has field-specific meaning), a present
// malformed or out-of-range duration is a 422 naming the field.
func validateAuthDurations(values map[string]string) error {
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

// loginMethodSurfaces are the schema-key suffixes of the two tenancy
// surfaces (§4.4: S ∈ {Admin, Client}).
var loginMethodSurfaces = []string{"Admin", "Client"}

// validateLoginMethodInvariant enforces, per surface S:
//
//	valid(S) := passwordOn(S) ∨ (oauthOn(S) ∧ autoLink)
//
// judged from the TARGET snapshot's raw values (strict booleans, schema
// defaults for absent keys), effective values (EnvVar/default fallback for
// the structural fields) and secret presence — never from the active
// profile, never from a secret value. Malformed booleans among the eleven
// participating keys are rejected up-front naming that key (edge #29);
// only the cross-field lockout failure names passwordLoginEnabled<S> and
// carries the stable auth.login_method_lockout code.
func validateLoginMethodInvariant(snap module.ConfigValidationSnapshot, probe services.KeyFileProbe) error {
	autoLink, err := snapshotBool(snap.Values, "oauthAutoLinkByEmail", true)
	if err != nil {
		return err
	}
	// Parse all ten surface-scoped booleans first so a malformed value is
	// refused on every write, not only when its surface happens to be off.
	passwordOn := map[string]bool{}
	providerOn := map[string]map[string]bool{}
	for _, surface := range loginMethodSurfaces {
		on, err := snapshotBool(snap.Values, "passwordLoginEnabled"+surface, true)
		if err != nil {
			return err
		}
		passwordOn[surface] = on
		providerOn[surface] = map[string]bool{}
		for _, p := range services.WebProviderOrder {
			pv, err := snapshotBool(snap.Values, string(p)+"Enabled"+surface, false)
			if err != nil {
				return err
			}
			providerOn[surface][string(p)] = pv
		}
	}
	for _, surface := range loginMethodSurfaces {
		if passwordOn[surface] {
			continue
		}
		usable := false
		for _, p := range services.WebProviderOrder {
			if !providerOn[surface][string(p)] {
				continue
			}
			if _, ok := providerStructuralFromSnapshot(snap, p, probe); ok {
				usable = true
				break
			}
		}
		if usable && autoLink {
			continue
		}
		return &module.ConfigValidationError{
			Field: "passwordLoginEnabled" + surface,
			Code:  errcode.AuthLoginMethodLockout,
			Message: "turning email/password sign-in off would lock this surface out: keep the password method enabled, " +
				"or leave at least one fully configured OAuth provider enabled for this surface " +
				"(client ID, redirect URL and secret — for Apple also team ID, key ID and a private key) " +
				"together with 'Auto-link OAuth provider to existing email account'",
		}
	}
	return nil
}

// snapshotBool applies §4.4's strictBool over a raw snapshot value: absent
// key → schema default; present canonical boolean → its value; present
// malformed or empty → 422 naming the key. Never readBool.
func snapshotBool(values map[string]string, key string, def bool) (bool, error) {
	raw, present := values[key]
	if !present {
		return def, nil
	}
	v, err := services.StrictBool(raw)
	if err != nil {
		return false, &module.ConfigValidationError{
			Field:   key,
			Message: "must be exactly true or false",
		}
	}
	return v, nil
}

// providerStructuralFromSnapshot mirrors usableFromView's field mapping
// (oauth_provider_usability.go) over a VALIDATION snapshot instead of the
// active view, so the validator and the runtime agree field-for-field.
func providerStructuralFromSnapshot(snap module.ConfigValidationSnapshot, p models.OAuthProvider, probe services.KeyFileProbe) (string, bool) {
	fields := services.ProviderStructuralFields{
		ClientID:       snap.EffectiveValues[string(p)+"ClientId"],
		RedirectURL:    snap.EffectiveValues[string(p)+"RedirectURL"],
		SecretPresent:  snap.SecretPresent[string(p)+"ClientSecret"],
		TeamID:         snap.EffectiveValues["appleTeamId"],
		KeyID:          snap.EffectiveValues["appleKeyId"],
		PrivateKeyPath: snap.EffectiveValues["applePrivateKeyPath"],
	}
	if p == models.OAuthProviderApple {
		fields.SecretPresent = snap.SecretPresent["applePrivateKey"]
	}
	return services.ProviderStructurallyConfigured(p, fields, probe)
}
