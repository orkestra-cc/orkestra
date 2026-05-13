package services

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/orkestra/backend/internal/core/auth/models"
	"github.com/orkestra/backend/internal/core/auth/repository"
)

// stubDeviceTrustRepo satisfies repository.DeviceTrustRepository. The
// methods the service consumes carry configurable return values; the
// others are no-op/panic so accidental use surfaces loudly.
type stubDeviceTrustRepo struct {
	upserted  *models.DeviceTrustDoc
	upsertErr error

	getActive    *models.DeviceTrustDoc
	getActiveErr error

	listActive    []*models.DeviceTrustDoc
	listActiveErr error

	markUsedCount int

	revokeByDeviceCalls []string
	revokeByDeviceErr   error

	revokeAllCount  int64
	revokeAllErr    error
	revokeAllReason string

	deleteAllCount int64
	deleteAllErr   error
}

func (s *stubDeviceTrustRepo) Upsert(_ context.Context, grant *models.DeviceTrustDoc) error {
	if s.upsertErr != nil {
		return s.upsertErr
	}
	s.upserted = grant
	return nil
}

func (s *stubDeviceTrustRepo) GetActive(_ context.Context, _, _ string) (*models.DeviceTrustDoc, error) {
	if s.getActiveErr != nil {
		return nil, s.getActiveErr
	}
	if s.getActive == nil {
		return nil, repository.ErrDeviceTrustNotFound
	}
	return s.getActive, nil
}

func (s *stubDeviceTrustRepo) ListActiveByUser(_ context.Context, _ string) ([]*models.DeviceTrustDoc, error) {
	return s.listActive, s.listActiveErr
}

func (s *stubDeviceTrustRepo) MarkUsed(_ context.Context, _, _ string) error {
	s.markUsedCount++
	return nil
}

func (s *stubDeviceTrustRepo) RevokeByDevice(_ context.Context, _, deviceID, _ string) error {
	s.revokeByDeviceCalls = append(s.revokeByDeviceCalls, deviceID)
	return s.revokeByDeviceErr
}

func (s *stubDeviceTrustRepo) RevokeAllByUser(_ context.Context, _, reason string) (int64, error) {
	s.revokeAllReason = reason
	return s.revokeAllCount, s.revokeAllErr
}

func (s *stubDeviceTrustRepo) DeleteAllByUser(_ context.Context, _ string) (int64, error) {
	return s.deleteAllCount, s.deleteAllErr
}

// compile-time guard.
var _ repository.DeviceTrustRepository = (*stubDeviceTrustRepo)(nil)

// --- tests ---

func TestDeviceTrust_MarkTrustedUsesDefaultDuration(t *testing.T) {
	repo := &stubDeviceTrustRepo{}
	svc := NewDeviceTrustService(repo, 0, nil).(*deviceTrustService)
	now := time.Date(2026, 4, 25, 0, 0, 0, 0, time.UTC)
	svc.clock = func() time.Time { return now }

	err := svc.MarkTrusted(context.Background(), MarkTrustedInput{
		UserUUID:    "u",
		DeviceID:    "d",
		Fingerprint: "fp",
		GrantedAMR:  "otp",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if repo.upserted == nil {
		t.Fatalf("expected upsert to be called")
	}
	wantUntil := now.Add(models.DeviceTrustDuration)
	if !repo.upserted.TrustedUntil.Equal(wantUntil) {
		t.Errorf("trustedUntil = %v, want %v (default 30d)", repo.upserted.TrustedUntil, wantUntil)
	}
	if repo.upserted.GrantedAMR != "otp" {
		t.Errorf("grantedAmr = %q, want otp", repo.upserted.GrantedAMR)
	}
	if !repo.upserted.IsActive {
		t.Errorf("isActive should be true on a fresh grant")
	}
}

func TestDeviceTrust_MarkTrustedOverrideDuration(t *testing.T) {
	repo := &stubDeviceTrustRepo{}
	svc := NewDeviceTrustService(repo, 48*time.Hour, nil).(*deviceTrustService)
	now := time.Date(2026, 4, 25, 0, 0, 0, 0, time.UTC)
	svc.clock = func() time.Time { return now }

	err := svc.MarkTrusted(context.Background(), MarkTrustedInput{
		UserUUID: "u",
		DeviceID: "d",
		Duration: 7 * 24 * time.Hour, // explicit override wins
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := now.Add(7 * 24 * time.Hour)
	if !repo.upserted.TrustedUntil.Equal(want) {
		t.Errorf("explicit duration ignored: trustedUntil = %v, want %v", repo.upserted.TrustedUntil, want)
	}
}

func TestDeviceTrust_MarkTrustedSilentOnMissingIdentifiers(t *testing.T) {
	repo := &stubDeviceTrustRepo{}
	svc := NewDeviceTrustService(repo, 0, nil)

	// Missing userUUID
	if err := svc.MarkTrusted(context.Background(), MarkTrustedInput{DeviceID: "d"}); err != nil {
		t.Errorf("unexpected error on missing userUUID: %v", err)
	}
	// Missing deviceID
	if err := svc.MarkTrusted(context.Background(), MarkTrustedInput{UserUUID: "u"}); err != nil {
		t.Errorf("unexpected error on missing deviceID: %v", err)
	}
	if repo.upserted != nil {
		t.Errorf("upsert should not fire when identifiers are missing, got %+v", repo.upserted)
	}
}

func TestDeviceTrust_IsTrustedMatchingFingerprint(t *testing.T) {
	repo := &stubDeviceTrustRepo{
		getActive: &models.DeviceTrustDoc{
			UserUUID:    "u",
			DeviceID:    "d",
			Fingerprint: "fp-known",
		},
	}
	svc := NewDeviceTrustService(repo, 0, nil)
	trusted, doc, err := svc.IsTrusted(context.Background(), "u", "d", "fp-known")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !trusted {
		t.Errorf("matching fingerprint should be trusted")
	}
	if doc == nil {
		t.Errorf("expected doc to be returned on trust hit")
	}
	if repo.markUsedCount != 1 {
		t.Errorf("expected markUsed to be called once, got %d", repo.markUsedCount)
	}
}

func TestDeviceTrust_IsTrustedFingerprintMismatch(t *testing.T) {
	repo := &stubDeviceTrustRepo{
		getActive: &models.DeviceTrustDoc{
			UserUUID:    "u",
			DeviceID:    "d",
			Fingerprint: "fp-original",
		},
	}
	svc := NewDeviceTrustService(repo, 0, nil)
	trusted, _, err := svc.IsTrusted(context.Background(), "u", "d", "fp-spoofed")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if trusted {
		t.Errorf("fingerprint mismatch must not be trusted")
	}
	if repo.markUsedCount != 0 {
		t.Errorf("markUsed must not fire on fingerprint mismatch, got %d", repo.markUsedCount)
	}
}

func TestDeviceTrust_IsTrustedEmptyFingerprintOnGrant(t *testing.T) {
	// Legacy grants stored without a fingerprint (web path pre-C3
	// fingerprint collection) must still work — IsTrusted accepts any
	// fingerprint including empty when the grant records empty too.
	repo := &stubDeviceTrustRepo{
		getActive: &models.DeviceTrustDoc{
			UserUUID:    "u",
			DeviceID:    "d",
			Fingerprint: "",
		},
	}
	svc := NewDeviceTrustService(repo, 0, nil)
	trusted, _, err := svc.IsTrusted(context.Background(), "u", "d", "fp-whatever")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !trusted {
		t.Errorf("empty fingerprint on grant must accept any request fingerprint")
	}
}

func TestDeviceTrust_IsTrustedNotFoundReturnsFalse(t *testing.T) {
	repo := &stubDeviceTrustRepo{} // getActive defaults to ErrDeviceTrustNotFound
	svc := NewDeviceTrustService(repo, 0, nil)
	trusted, doc, err := svc.IsTrusted(context.Background(), "u", "d", "fp")
	if err != nil {
		t.Fatalf("ErrDeviceTrustNotFound should translate to (false, nil, nil): %v", err)
	}
	if trusted || doc != nil {
		t.Errorf("expected (false, nil), got (%v, %+v)", trusted, doc)
	}
}

func TestDeviceTrust_IsTrustedPropagatesOtherErrors(t *testing.T) {
	repo := &stubDeviceTrustRepo{getActiveErr: errors.New("mongo: timeout")}
	svc := NewDeviceTrustService(repo, 0, nil)
	trusted, _, err := svc.IsTrusted(context.Background(), "u", "d", "fp")
	if err == nil {
		t.Errorf("expected error to propagate")
	}
	if trusted {
		t.Errorf("error path must return false")
	}
}

func TestDeviceTrust_RevokeByDeviceIdempotent(t *testing.T) {
	// Revoking a non-existent device must not error — idempotent.
	repo := &stubDeviceTrustRepo{revokeByDeviceErr: repository.ErrDeviceTrustNotFound}
	svc := NewDeviceTrustService(repo, 0, nil)
	if err := svc.RevokeByDevice(context.Background(), "u", "d", ""); err != nil {
		t.Errorf("revoking unknown device must not error, got %v", err)
	}
}

func TestDeviceTrust_RevokeAllStampsReason(t *testing.T) {
	repo := &stubDeviceTrustRepo{revokeAllCount: 3}
	svc := NewDeviceTrustService(repo, 0, nil)
	if err := svc.RevokeAllByUser(context.Background(), "u", models.DeviceTrustRevokedOnPasswordChange); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if repo.revokeAllReason != models.DeviceTrustRevokedOnPasswordChange {
		t.Errorf("reason = %q, want %q", repo.revokeAllReason, models.DeviceTrustRevokedOnPasswordChange)
	}
}

func TestDeviceTrust_NilRepoNoOp(t *testing.T) {
	svc := NewDeviceTrustService(nil, 0, nil)
	// All four operations must be nil-safe.
	if err := svc.MarkTrusted(context.Background(), MarkTrustedInput{UserUUID: "u", DeviceID: "d"}); err != nil {
		t.Errorf("MarkTrusted with nil repo: %v", err)
	}
	trusted, _, err := svc.IsTrusted(context.Background(), "u", "d", "fp")
	if err != nil || trusted {
		t.Errorf("IsTrusted with nil repo: trusted=%v err=%v", trusted, err)
	}
	if _, err := svc.ListActive(context.Background(), "u"); err != nil {
		t.Errorf("ListActive with nil repo: %v", err)
	}
	if err := svc.RevokeByDevice(context.Background(), "u", "d", ""); err != nil {
		t.Errorf("RevokeByDevice with nil repo: %v", err)
	}
	if err := svc.RevokeAllByUser(context.Background(), "u", ""); err != nil {
		t.Errorf("RevokeAllByUser with nil repo: %v", err)
	}
}
