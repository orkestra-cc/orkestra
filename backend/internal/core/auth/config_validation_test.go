package auth

import (
	"context"
	"errors"
	"testing"

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
		{"lockout threshold absurd but accepted", map[string]string{"accountLockoutThreshold": "999999"}, ""},
	}
	m := &AuthModule{}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := m.ValidateConfig(context.Background(), tc.values)
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

func TestAuthModuleImplementsConfigValidator(t *testing.T) {
	var _ module.HasConfigValidator = (*AuthModule)(nil)
}
