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

// TestUser_MissingMFAEpochReadsAsZero (edge case 12: a pre-deploy
// document has no mfaEpoch and must read as 0) is a bson round trip and
// so belongs with the type, not the service —
// pkg/sdk/iface/user_mfa_epoch_test.go's TestUserMFAEpoch_BSONRoundTrip.
// A Go zero value asserted against a struct literal this package builds
// itself proves nothing about the bson tag, which is the thing edge case
// 12 actually depends on.

func TestBumpMFAEpoch_UnknownUserIsAnError(t *testing.T) {
	svc, _ := newUserServiceForEpochTest(t)
	if _, err := svc.BumpMFAEpoch(context.Background(), "does-not-exist"); err == nil {
		t.Fatal("bumping a user that does not exist must be an error, never a silent 0")
	}
}

// The seam iface.MFAEpochBumper is what the auth module resolves via
// module.GetTyped. The compile-time assertion pinning userService to it
// lives in the production file (user_service.go), right next to the
// method — a duplicate here would only restate what the compiler already
// enforces on every build.
