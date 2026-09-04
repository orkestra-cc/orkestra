package services

import (
	"context"
	"testing"

	"github.com/orkestra/backend/pkg/sdk/iface"
)

// newUserServiceForEpochTest follows the harness every other test in this
// package uses: an in-memory fakeUserRepo (see user_service_test.go), not
// a live MongoDB — this module has no MONGO_TEST_URI-guarded tests to
// mirror, so a second fake repository is not warranted either.
//
// BumpMFAEpoch is deliberately narrow — not on the exported UserService
// interface, the same UserLifecycleState precedent (see user_service.go)
// — so the return type here is the concrete *userService, exactly like
// TestUserLifecycleState's newSvcForTest.
func newUserServiceForEpochTest(t *testing.T) (*userService, *fakeUserRepo) {
	t.Helper()
	svc, users, _ := newSvcForTest(t)
	return svc, users
}

// seedUser plants a live user row and returns its UUID.
func (r *fakeUserRepo) seedUser(t *testing.T, uuid string) string {
	t.Helper()
	r.seed(&iface.User{
		UUID:     uuid,
		Email:    uuid + "@example.com",
		Role:     "operator",
		IsActive: true,
	})
	return uuid
}

// seedUserWithoutEpochField models a document written before mfaEpoch
// existed on the schema. In this in-memory fake that is indistinguishable
// from seedUser: MFAEpoch is simply left at its Go zero value, which is
// exactly what a real BSON document missing the field would decode to.
func (r *fakeUserRepo) seedUserWithoutEpochField(t *testing.T, uuid string) string {
	t.Helper()
	return r.seedUser(t, uuid)
}

// The epoch is what makes a factor removal take effect on the CALLER's
// current token, without waiting for a refresh and without depending on
// a revocation write succeeding. It must be monotone and it must start
// at zero for every document that predates it.
func TestBumpMFAEpoch_IsMonotone(t *testing.T) {
	svc, repo := newUserServiceForEpochTest(t)
	ctx := context.Background()
	uuid := repo.seedUser(t, "u-1")

	got, err := svc.BumpMFAEpoch(ctx, uuid)
	if err != nil {
		t.Fatalf("BumpMFAEpoch: %v", err)
	}
	if got != 1 {
		t.Fatalf("first bump = %d, want 1 (an absent field reads as 0)", got)
	}
	got, err = svc.BumpMFAEpoch(ctx, uuid)
	if err != nil {
		t.Fatalf("BumpMFAEpoch: %v", err)
	}
	if got != 2 {
		t.Fatalf("second bump = %d, want 2", got)
	}

	user, err := svc.GetUserByID(ctx, uuid)
	if err != nil {
		t.Fatalf("GetUserByID: %v", err)
	}
	if user.MFAEpoch != 2 {
		t.Fatalf("persisted MFAEpoch = %d, want 2", user.MFAEpoch)
	}
}

// Every document written before this ships has no mfaEpoch. It must
// read as 0 and match every pre-deploy token, so the deploy itself
// downgrades nobody (edge case 12).
func TestUser_MissingMFAEpochReadsAsZero(t *testing.T) {
	svc, repo := newUserServiceForEpochTest(t)
	uuid := repo.seedUserWithoutEpochField(t, "u-legacy")

	user, err := svc.GetUserByID(context.Background(), uuid)
	if err != nil {
		t.Fatalf("GetUserByID: %v", err)
	}
	if user.MFAEpoch != 0 {
		t.Fatalf("MFAEpoch = %d for a legacy document, want 0", user.MFAEpoch)
	}
}

func TestBumpMFAEpoch_UnknownUserIsAnError(t *testing.T) {
	svc, _ := newUserServiceForEpochTest(t)
	if _, err := svc.BumpMFAEpoch(context.Background(), "does-not-exist"); err == nil {
		t.Fatal("bumping a user that does not exist must be an error, never a silent 0")
	}
}

// The seam is what the auth module resolves; a compile-time assertion is
// cheaper than discovering the mismatch at boot.
func TestUserService_ImplementsMFAEpochBumper(t *testing.T) {
	var _ iface.MFAEpochBumper = (*userService)(nil)
}
