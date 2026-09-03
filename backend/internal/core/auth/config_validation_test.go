package auth

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/orkestra/backend/internal/core/auth/services"
	"github.com/orkestra/backend/internal/shared/errcode"
	"github.com/orkestra/backend/pkg/sdk/module"
)

// The input contract from the ADR-0017 design, exhaustively. Rows marked
// wantField must be rejected with 422 and must never reach persistence;
// the empty rows are legal because emptiness is field-specific meaning,
// not absence of a decision.
func TestAuthDurationPatchValidation(t *testing.T) {
	cases := []struct {
		name      string
		values    map[string]string
		wantField string
	}{
		{"absent key", map[string]string{}, ""},
		{"accessTokenTTL empty falls through to env", map[string]string{"accessTokenTTL": ""}, ""},
		{"accessTokenTTL blank is empty", map[string]string{"accessTokenTTL": "   "}, ""},
		{"accessTokenTTL at minimum", map[string]string{"accessTokenTTL": "1m"}, ""},
		{"accessTokenTTL at maximum", map[string]string{"accessTokenTTL": "24h"}, ""},
		{"accessTokenTTL day suffix", map[string]string{"accessTokenTTL": "1d"}, ""},
		{"accessTokenTTL below minimum", map[string]string{"accessTokenTTL": "30s"}, "accessTokenTTL"},
		{"accessTokenTTL above maximum", map[string]string{"accessTokenTTL": "9999h"}, "accessTokenTTL"},
		{"accessTokenTTL malformed", map[string]string{"accessTokenTTL": "forever"}, "accessTokenTTL"},
		{"accessTokenTTL zero", map[string]string{"accessTokenTTL": "0s"}, "accessTokenTTL"},
		{"accessTokenTTL negative", map[string]string{"accessTokenTTL": "-5m"}, "accessTokenTTL"},
		{"passwordResetTokenTTL empty uses default", map[string]string{"passwordResetTokenTTL": ""}, ""},
		{"passwordResetTokenTTL at minimum", map[string]string{"passwordResetTokenTTL": "5m"}, ""},
		{"passwordResetTokenTTL below minimum", map[string]string{"passwordResetTokenTTL": "1m"}, "passwordResetTokenTTL"},
		{"passwordResetTokenTTL above maximum", map[string]string{"passwordResetTokenTTL": "72h"}, "passwordResetTokenTTL"},
		{"sessionAbsoluteTTL empty disables the cap", map[string]string{"sessionAbsoluteTTL": ""}, ""},
		{"sessionAbsoluteTTL at minimum", map[string]string{"sessionAbsoluteTTL": "1h"}, ""},
		{"sessionAbsoluteTTL at maximum", map[string]string{"sessionAbsoluteTTL": "89d"}, ""},
		{"sessionAbsoluteTTL default", map[string]string{"sessionAbsoluteTTL": "720h"}, ""},
		{"sessionAbsoluteTTL below minimum", map[string]string{"sessionAbsoluteTTL": "30m"}, "sessionAbsoluteTTL"},
		{"sessionAbsoluteTTL above maximum", map[string]string{"sessionAbsoluteTTL": "90d"}, "sessionAbsoluteTTL"},
		{"sessionAbsoluteTTL malformed", map[string]string{"sessionAbsoluteTTL": "forever"}, "sessionAbsoluteTTL"},
		// Deliberately unbounded: neither governs an already-issued credential.
		{"lockout duration absurd but accepted", map[string]string{"accountLockoutDuration": "9999h"}, ""},
		// ipLockoutThreshold must climb alongside accountLockoutThreshold here:
		// left at its 100 default it would sit below 999999 and trip the
		// ip >= account ordering rule (config_validation.go), which is a
		// different, additive constraint from the unbounded-value point this
		// case makes.
		{"lockout threshold absurd but accepted", map[string]string{"accountLockoutThreshold": "999999", "ipLockoutThreshold": "999999"}, ""},
	}
	m := &AuthModule{}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := m.ValidateConfigSnapshot(context.Background(), module.ConfigValidationSnapshot{Values: tc.values})
			if tc.wantField == "" {
				if err != nil {
					t.Fatalf("ValidateConfig(%v) = %v, want nil", tc.values, err)
				}
				return
			}
			var typed *module.ConfigValidationError
			if !errors.As(err, &typed) {
				t.Fatalf("ValidateConfig(%v) = %v, want *ConfigValidationError", tc.values, err)
			}
			if typed.Field != tc.wantField {
				t.Errorf("Field = %q, want %q", typed.Field, tc.wantField)
			}
		})
	}
}

// The auth module is judged through the snapshot contract on all three
// mutation surfaces; the legacy value-only hook is gone so the SDK can
// never fall back to a validator that cannot see secret presence.
func TestAuthModuleImplementsSnapshotValidator(t *testing.T) {
	var _ module.HasConfigSnapshotValidator = (*AuthModule)(nil)
	var mod interface{} = &AuthModule{}
	if _, ok := mod.(module.HasConfigValidator); ok {
		t.Fatal("AuthModule must NOT keep HasConfigValidator — the snapshot validator replaces it")
	}
	if !(&AuthModule{}).HotReloadConfig() {
		t.Fatal("auth reads config lazily at request time; HotReloadConfig must be true so successful writes persist needsRestart=false")
	}
}

// snapFor builds a target snapshot with a structurally complete Google
// provider available for wiring into either surface. Tests override or
// delete keys per case. probe(nil) means "no readable Apple key file".
func snapFor(overrides map[string]string, secrets map[string]bool) module.ConfigValidationSnapshot {
	values := map[string]string{}
	effective := map[string]string{
		"googleClientId":    "gid.apps.example",
		"googleRedirectURL": "https://api.example.com/auth/oauth/google/callback",
	}
	present := map[string]bool{"googleClientSecret": true}
	for k, v := range overrides {
		values[k] = v
		// Non-secret overrides participate in the structural predicate via
		// EffectiveValues exactly as ConfigService merges them (§4.5).
		effective[k] = v
	}
	for k, v := range secrets {
		present[k] = v
	}
	return module.ConfigValidationSnapshot{
		Environment:     "production",
		Values:          values,
		EffectiveValues: effective,
		SecretPresent:   present,
	}
}

func TestLoginMethodInvariant(t *testing.T) {
	googleOnAdmin := map[string]string{"googleEnabledAdmin": "true"}

	cases := []struct {
		name      string
		overrides map[string]string
		secrets   map[string]bool
		probe     services.KeyFileProbe
		wantField string
		wantCode  string
	}{
		// Defaults: both password keys absent → legacy true → always valid.
		{name: "empty snapshot is valid", overrides: nil},
		// Password off with a usable provider + auto-link (default true) is the SSO-only happy path.
		{name: "admin off with usable google", overrides: merge(googleOnAdmin, map[string]string{"passwordLoginEnabledAdmin": "false"})},
		// Cross-field failures name passwordLoginEnabled<S> with the lockout code (§4.4).
		{name: "admin off with no provider at all", overrides: map[string]string{"passwordLoginEnabledAdmin": "false"},
			wantField: "passwordLoginEnabledAdmin", wantCode: errcode.AuthLoginMethodLockout},
		{name: "client off while only admin has google", overrides: merge(googleOnAdmin, map[string]string{"passwordLoginEnabledClient": "false"}),
			wantField: "passwordLoginEnabledClient", wantCode: errcode.AuthLoginMethodLockout},
		{name: "admin off, provider toggled but structurally incomplete (no secret)",
			overrides: merge(googleOnAdmin, map[string]string{"passwordLoginEnabledAdmin": "false"}),
			secrets:   map[string]bool{"googleClientSecret": false},
			wantField: "passwordLoginEnabledAdmin", wantCode: errcode.AuthLoginMethodLockout},
		{name: "admin off, provider structurally complete but toggle off",
			overrides: map[string]string{"passwordLoginEnabledAdmin": "false", "googleEnabledAdmin": "false"},
			wantField: "passwordLoginEnabledAdmin", wantCode: errcode.AuthLoginMethodLockout},
		{name: "admin off, auto-link explicitly off closes the linking loop",
			overrides: merge(googleOnAdmin, map[string]string{"passwordLoginEnabledAdmin": "false", "oauthAutoLinkByEmail": "false"}),
			wantField: "passwordLoginEnabledAdmin", wantCode: errcode.AuthLoginMethodLockout},
		// Blanking a structural field of the last usable provider while password is off (§4.4 symmetric).
		{name: "admin off, redirect URL blanked",
			overrides: merge(googleOnAdmin, map[string]string{"passwordLoginEnabledAdmin": "false", "googleRedirectURL": ""}),
			wantField: "passwordLoginEnabledAdmin", wantCode: errcode.AuthLoginMethodLockout},
		// Apple structural rules: inline PEM presence or a readable key file (probe-injected).
		{name: "admin off, apple usable via key file",
			overrides: map[string]string{
				"passwordLoginEnabledAdmin": "false", "appleEnabledAdmin": "true",
				"appleClientId": "com.example.svc", "appleRedirectURL": "https://api.example.com/cb",
				"appleTeamId": "TEAM1", "appleKeyId": "KEY1", "applePrivateKeyPath": "/keys/apple.p8",
			},
			probe: func(path string) bool { return path == "/keys/apple.p8" }},
		{name: "admin off, apple key file unreadable",
			overrides: map[string]string{
				"passwordLoginEnabledAdmin": "false", "appleEnabledAdmin": "true",
				"appleClientId": "com.example.svc", "appleRedirectURL": "https://api.example.com/cb",
				"appleTeamId": "TEAM1", "appleKeyId": "KEY1", "applePrivateKeyPath": "/keys/apple.p8",
			},
			probe:     func(string) bool { return false },
			wantField: "passwordLoginEnabledAdmin", wantCode: errcode.AuthLoginMethodLockout},
		// Malformed booleans among the eleven §4.4 keys are 422 naming THAT key,
		// up-front, regardless of surface state (deviation 10, edge #29).
		{name: "malformed password toggle", overrides: map[string]string{"passwordLoginEnabledAdmin": "treu"},
			wantField: "passwordLoginEnabledAdmin"},
		{name: "malformed provider toggle rejected even with password on",
			overrides: map[string]string{"githubEnabledClient": "treu"},
			wantField: "githubEnabledClient"},
		{name: "malformed auto-link", overrides: map[string]string{"oauthAutoLinkByEmail": "yes"},
			wantField: "oauthAutoLinkByEmail"},
		{name: "empty present password toggle is malformed", overrides: map[string]string{"passwordLoginEnabledClient": ""},
			wantField: "passwordLoginEnabledClient"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			probe := tc.probe
			if probe == nil {
				probe = func(string) bool { return false }
			}
			err := validateLoginMethodInvariant(snapFor(tc.overrides, tc.secrets), probe)
			if tc.wantField == "" {
				if err != nil {
					t.Fatalf("want valid, got %v", err)
				}
				return
			}
			var typed *module.ConfigValidationError
			if !errors.As(err, &typed) {
				t.Fatalf("want *ConfigValidationError, got %v", err)
			}
			if typed.Field != tc.wantField {
				t.Errorf("Field = %q, want %q", typed.Field, tc.wantField)
			}
			if typed.Code != tc.wantCode {
				t.Errorf("Code = %q, want %q", typed.Code, tc.wantCode)
			}
			if tc.wantCode == errcode.AuthLoginMethodLockout {
				// Both exits must be named (§4.4 error shape).
				for _, needle := range []string{"password", "provider", "auto-link"} {
					if !strings.Contains(strings.ToLower(typed.Message), needle) {
						t.Errorf("message %q must name the %s exit", typed.Message, needle)
					}
				}
			}
		})
	}
}

// TestValidateConfigSnapshot_BindsLoginMethodInvariant pins the WIRING, not
// the rule: every case in TestLoginMethodInvariant calls
// validateLoginMethodInvariant directly, so
// deleting the call from ValidateConfigSnapshot would leave them all green
// while the admin API stopped enforcing §4.4 entirely. This one case goes
// through the exported method the SDK actually calls.
func TestValidateConfigSnapshot_BindsLoginMethodInvariant(t *testing.T) {
	// Password off on the operator surface with no enabled provider — the
	// snapshot has Google's structural fields but no googleEnabledAdmin.
	err := (&AuthModule{}).ValidateConfigSnapshot(context.Background(),
		snapFor(map[string]string{"passwordLoginEnabledAdmin": "false"}, nil))
	var typed *module.ConfigValidationError
	if !errors.As(err, &typed) {
		t.Fatalf("want *ConfigValidationError, got %v", err)
	}
	if typed.Code != errcode.AuthLoginMethodLockout {
		t.Errorf("Code = %q, want %q", typed.Code, errcode.AuthLoginMethodLockout)
	}
	if typed.Field != "passwordLoginEnabledAdmin" {
		t.Errorf("Field = %q, want %q", typed.Field, "passwordLoginEnabledAdmin")
	}
}

// An IP threshold BELOW the account threshold makes the shared address
// lock before the account does, which turns "did this address get 429'd
// early?" into an oracle for whether an account exists behind it.
func TestValidateConfigSnapshot_RefusesIPThresholdBelowAccount(t *testing.T) {
	m := &AuthModule{}
	snap := module.ConfigValidationSnapshot{
		Values: map[string]string{
			"accountLockoutThreshold": "5",
			"ipLockoutThreshold":      "3",
		},
	}
	err := m.ValidateConfigSnapshot(context.Background(), snap)
	if err == nil {
		t.Fatal("want a validation error")
	}
	var ve *module.ConfigValidationError
	if !errors.As(err, &ve) {
		t.Fatalf("want *module.ConfigValidationError, got %T", err)
	}
	if ve.Field != "ipLockoutThreshold" {
		t.Errorf("Field = %q, want ipLockoutThreshold", ve.Field)
	}
	if ve.Code != errcode.AuthIPThresholdBelowAccount {
		t.Errorf("Code = %q, want %q", ve.Code, errcode.AuthIPThresholdBelowAccount)
	}
}

func TestValidateConfigSnapshot_AcceptsIPThresholdEqualToAccount(t *testing.T) {
	m := &AuthModule{}
	snap := module.ConfigValidationSnapshot{
		Values: map[string]string{
			"accountLockoutThreshold": "5",
			"ipLockoutThreshold":      "5",
		},
	}
	if err := m.ValidateConfigSnapshot(context.Background(), snap); err != nil {
		t.Fatalf("equality must be accepted: %v", err)
	}
}

// Absent keys mean "use the defaults" (5 and 100), which satisfy the
// rule — an operator who never touched either must not be refused.
func TestValidateConfigSnapshot_IPThresholdDefaultsPass(t *testing.T) {
	m := &AuthModule{}
	if err := m.ValidateConfigSnapshot(context.Background(), module.ConfigValidationSnapshot{
		Values: map[string]string{},
	}); err != nil {
		t.Fatalf("defaults must validate: %v", err)
	}
}

// A malformed value is not this rule's business — the field type
// already rejects it — so the rule must skip rather than mis-compare.
func TestValidateConfigSnapshot_IPThresholdMalformedSkipsRule(t *testing.T) {
	m := &AuthModule{}
	if err := m.ValidateConfigSnapshot(context.Background(), module.ConfigValidationSnapshot{
		Values: map[string]string{"ipLockoutThreshold": "lots", "accountLockoutThreshold": "5"},
	}); err != nil {
		t.Fatalf("a malformed value must not surface as the cross-field error: %v", err)
	}
}

func merge(maps ...map[string]string) map[string]string {
	out := map[string]string{}
	for _, m := range maps {
		for k, v := range m {
			out[k] = v
		}
	}
	return out
}
