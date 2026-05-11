package services

import (
	"context"
	"errors"
	"log/slog"
	"testing"
	"time"

	"github.com/pquerna/otp/totp"

	"github.com/orkestra/backend/internal/core/auth/models"
)

func TestRegenerateBackupCodes_ReplacesAtomically(t *testing.T) {
	t.Setenv("MFA_SECRET_ENCRYPTION_KEY", hex32())

	repo := newFakeFactorRepo()
	challenges := NewMFAChallengeService(NewMemoryOAuthStateStore())
	pw := NewPasswordService(slog.Default(), false)
	svc := NewMFAService(repo, challenges, pw, "Orkestra", slog.Default())

	user := &testUser{UUID: "u-regen", Email: "regen@example.com"}
	begin, _ := svc.BeginEnrollment(context.Background(), user.toUser())
	code, _ := totp.GenerateCode(begin.SecretBase32, time.Now())
	original, err := svc.ConfirmEnrollment(context.Background(), user.UUID, begin.ChallengeID, code)
	if err != nil {
		t.Fatalf("confirm: %v", err)
	}

	fresh, err := svc.RegenerateBackupCodes(context.Background(), user.UUID)
	if err != nil {
		t.Fatalf("regenerate: %v", err)
	}
	if len(fresh) != BackupCodeCount {
		t.Fatalf("expected %d codes, got %d", BackupCodeCount, len(fresh))
	}

	// The hash list must be replaced — original codes must NOT be
	// usable after regeneration. Verify by attempting to consume an
	// old code; it should be rejected.
	if err := svc.VerifyBackupCode(context.Background(), user.UUID, original[0]); err == nil {
		t.Fatalf("old backup code must stop working after regeneration")
	}

	// Pick one of the freshly issued codes and verify it works.
	if err := svc.VerifyBackupCode(context.Background(), user.UUID, fresh[0]); err != nil {
		t.Fatalf("fresh backup code must work: %v", err)
	}
}

func TestRegenerateBackupCodes_RejectsUnenrolled(t *testing.T) {
	t.Setenv("MFA_SECRET_ENCRYPTION_KEY", hex32())
	repo := newFakeFactorRepo()
	challenges := NewMFAChallengeService(NewMemoryOAuthStateStore())
	pw := NewPasswordService(slog.Default(), false)
	svc := NewMFAService(repo, challenges, pw, "Orkestra", slog.Default())

	_, err := svc.RegenerateBackupCodes(context.Background(), "u-never-enrolled")
	if !errors.Is(err, ErrMFANotEnrolled) {
		t.Fatalf("err = %v, want ErrMFANotEnrolled", err)
	}
}

func TestRegenerateBackupCodes_EmptyUUID(t *testing.T) {
	t.Setenv("MFA_SECRET_ENCRYPTION_KEY", hex32())
	repo := newFakeFactorRepo()
	challenges := NewMFAChallengeService(NewMemoryOAuthStateStore())
	pw := NewPasswordService(slog.Default(), false)
	svc := NewMFAService(repo, challenges, pw, "Orkestra", slog.Default())

	_, err := svc.RegenerateBackupCodes(context.Background(), "")
	if !errors.Is(err, ErrMFANotEnrolled) {
		t.Fatalf("err = %v, want ErrMFANotEnrolled", err)
	}
}

// Sanity guard so the test file's reliance on the model-level
// MFAFactorTOTP constant doesn't drift silently.
var _ = models.MFAFactorTOTP
