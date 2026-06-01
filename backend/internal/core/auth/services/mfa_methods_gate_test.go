package services

import (
	"context"
	"errors"
	"log/slog"
	"testing"

	"github.com/orkestra/backend/pkg/sdk/iface"
)

// Reuses the newPolicy helper defined in auth_policy_service_test.go.

// TestBeginEnrollment_RejectedWhenTOTPDisabledByPolicy: with mfaMethods
// configured to exclude totp, BeginEnrollment must short-circuit with
// ErrMFAMethodDisabled before generating a TOTP secret.
func TestBeginEnrollment_RejectedWhenTOTPDisabledByPolicy(t *testing.T) {
	t.Setenv("MFA_SECRET_ENCRYPTION_KEY", hex32())

	repo := newFakeFactorRepo()
	challenges := NewMFAChallengeService(NewMemoryOAuthStateStore())
	pw := NewPasswordService(slog.Default(), false)
	svc := NewMFAService(repo, challenges, pw, "Orkestra", slog.Default())
	svc.SetPolicy(newPolicy(map[string]string{
		"mfaMethods": "webauthn", // totp excluded
	}))

	user := &iface.User{UUID: "u-1", Email: "alice@example.com"}
	_, err := svc.BeginEnrollment(context.Background(), user)
	if !errors.Is(err, ErrMFAMethodDisabled) {
		t.Fatalf("BeginEnrollment err = %v, want ErrMFAMethodDisabled", err)
	}
}

// TestBeginEnrollment_AllowedWhenTOTPInPolicy: explicit allow-list
// containing totp lets enrollment proceed.
func TestBeginEnrollment_AllowedWhenTOTPInPolicy(t *testing.T) {
	t.Setenv("MFA_SECRET_ENCRYPTION_KEY", hex32())

	repo := newFakeFactorRepo()
	challenges := NewMFAChallengeService(NewMemoryOAuthStateStore())
	pw := NewPasswordService(slog.Default(), false)
	svc := NewMFAService(repo, challenges, pw, "Orkestra", slog.Default())
	svc.SetPolicy(newPolicy(map[string]string{
		"mfaMethods": "totp,webauthn",
	}))

	user := &iface.User{UUID: "u-1", Email: "alice@example.com"}
	begin, err := svc.BeginEnrollment(context.Background(), user)
	if err != nil {
		t.Fatalf("BeginEnrollment err = %v, want nil with totp in policy", err)
	}
	if begin == nil || begin.SecretBase32 == "" {
		t.Fatalf("expected a non-empty enrollment payload, got %+v", begin)
	}
}

// TestBeginEnrollment_AllowedWhenPolicyEmpty: the legacy "no
// restriction" path — an empty mfaMethods key means every method
// stays allowed, preserving current behaviour for deployments that
// haven't touched the new field.
func TestBeginEnrollment_AllowedWhenPolicyEmpty(t *testing.T) {
	t.Setenv("MFA_SECRET_ENCRYPTION_KEY", hex32())

	repo := newFakeFactorRepo()
	challenges := NewMFAChallengeService(NewMemoryOAuthStateStore())
	pw := NewPasswordService(slog.Default(), false)
	svc := NewMFAService(repo, challenges, pw, "Orkestra", slog.Default())
	svc.SetPolicy(newPolicy(map[string]string{}))

	user := &iface.User{UUID: "u-1", Email: "alice@example.com"}
	if _, err := svc.BeginEnrollment(context.Background(), user); err != nil {
		t.Fatalf("BeginEnrollment err = %v, want nil with empty policy", err)
	}
}

// TestBeginEnrollment_AllowedWhenPolicyNil: nil policy must keep the
// pre-Phase-3.6 behaviour (every method allowed).
func TestBeginEnrollment_AllowedWhenPolicyNil(t *testing.T) {
	t.Setenv("MFA_SECRET_ENCRYPTION_KEY", hex32())

	repo := newFakeFactorRepo()
	challenges := NewMFAChallengeService(NewMemoryOAuthStateStore())
	pw := NewPasswordService(slog.Default(), false)
	svc := NewMFAService(repo, challenges, pw, "Orkestra", slog.Default())
	// no SetPolicy

	user := &iface.User{UUID: "u-1", Email: "alice@example.com"}
	if _, err := svc.BeginEnrollment(context.Background(), user); err != nil {
		t.Fatalf("BeginEnrollment err = %v, want nil with nil policy", err)
	}
}
