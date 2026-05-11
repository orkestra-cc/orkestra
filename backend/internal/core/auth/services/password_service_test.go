package services

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"
)

// silentLogger discards log output so test runs stay quiet.
func silentLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// newSvcWithPolicy constructs a passwordService whose policy is wired
// to a stub ConfigService — the only seam the service reads through.
// HIBP is disabled in every test by setting `breachedPasswordCheck` to
// "false" so no real HTTPS call leaves the host.
func newSvcWithPolicy(values map[string]string) PasswordService {
	if values == nil {
		values = map[string]string{}
	}
	if _, set := values["breachedPasswordCheck"]; !set {
		values["breachedPasswordCheck"] = "false"
	}
	svc := NewPasswordService(silentLogger(), false /* hibp constructor flag — overridden by policy */)
	svc.SetPolicy(&AuthPolicyService{cs: &stubReader{values: values}})
	return svc
}

func TestValidatePolicy_MinLengthFromConfig(t *testing.T) {
	svc := newSvcWithPolicy(map[string]string{"passwordMinLength": "16"})
	ctx := context.Background()
	if err := svc.ValidatePolicy(ctx, "shorter-than-16", ""); !errors.Is(err, ErrPasswordTooShort) {
		t.Fatalf("expected ErrPasswordTooShort for 15-char password under min=16, got %v", err)
	}
	if err := svc.ValidatePolicy(ctx, "long-enough-pw-x", ""); err != nil {
		t.Fatalf("16-char password should pass min=16, got %v", err)
	}
}

func TestValidatePolicy_LegacyDefaultsWhenUnset(t *testing.T) {
	// No length keys set — service falls back to legacy 10..128.
	svc := newSvcWithPolicy(nil)
	ctx := context.Background()
	if err := svc.ValidatePolicy(ctx, "1234567", ""); !errors.Is(err, ErrPasswordTooShort) {
		t.Fatalf("7-char password should fail legacy min=10, got %v", err)
	}
	if err := svc.ValidatePolicy(ctx, "1234567890", ""); err != nil {
		t.Fatalf("10-char password should pass legacy min=10, got %v", err)
	}
}

func TestValidatePolicy_MaxLength(t *testing.T) {
	svc := newSvcWithPolicy(map[string]string{"passwordMaxLength": "20"})
	ctx := context.Background()
	long := "this-is-twenty-five-chars"
	if err := svc.ValidatePolicy(ctx, long, ""); !errors.Is(err, ErrPasswordTooLong) {
		t.Fatalf("expected ErrPasswordTooLong, got %v", err)
	}
}

func TestValidatePolicy_ComplexityFlags(t *testing.T) {
	cases := []struct {
		name      string
		flags     map[string]string
		password  string
		expectErr error
	}{
		{
			name:      "require upper rejects all lowercase",
			flags:     map[string]string{"passwordRequireUpper": "true"},
			password:  "all-lowercase-here",
			expectErr: ErrPasswordMissingUpper,
		},
		{
			name:      "require digit rejects letters-only",
			flags:     map[string]string{"passwordRequireDigit": "true"},
			password:  "no-digits-present",
			expectErr: ErrPasswordMissingDigit,
		},
		{
			name:      "require symbol accepts unusual punctuation",
			flags:     map[string]string{"passwordRequireSymbol": "true"},
			password:  "passphrase—em-dash",
			expectErr: nil, // em-dash is non-alnum → counts as symbol
		},
		{
			name: "all flags satisfied passes",
			flags: map[string]string{
				"passwordRequireUpper":  "true",
				"passwordRequireLower":  "true",
				"passwordRequireDigit":  "true",
				"passwordRequireSymbol": "true",
			},
			password:  "Strong-pass-1!!!",
			expectErr: nil,
		},
		{
			name:      "missing lower with all required",
			flags:     map[string]string{"passwordRequireLower": "true"},
			password:  "ALL-CAPS-1234567",
			expectErr: ErrPasswordMissingLower,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			svc := newSvcWithPolicy(tc.flags)
			err := svc.ValidatePolicy(context.Background(), tc.password, "")
			if tc.expectErr == nil && err != nil {
				t.Fatalf("expected no error, got %v", err)
			}
			if tc.expectErr != nil && !errors.Is(err, tc.expectErr) {
				t.Fatalf("expected %v, got %v", tc.expectErr, err)
			}
		})
	}
}

func TestValidatePolicy_NoComplexityFlags_AcceptsSimplePassword(t *testing.T) {
	// All complexity flags off → length-only enforcement.
	svc := newSvcWithPolicy(nil)
	if err := svc.ValidatePolicy(context.Background(), "all-lowercase-no-digits", ""); err != nil {
		t.Fatalf("legacy mode should accept lowercase-only passwords, got %v", err)
	}
}
