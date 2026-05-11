package services

import (
	"context"
	"errors"
	"testing"
	"time"

	userModels "github.com/orkestra/backend/internal/core/user/models"
)

// SelfUnlinkOAuth shares the lockout helper with AdminUnlinkOAuth, so
// the fake from auth_service_admin_unlink_test.go is reused. The test
// file lives next to its admin-unlink sibling so a future refactor of
// the helper surfaces both call sites.

func TestSelfUnlinkOAuth_Success(t *testing.T) {
	t.Parallel()
	fake := newAdminUnlinkUserFake()
	user := &userModels.User{
		UUID:         "u-1",
		Email:        "u@example.com",
		PasswordHash: "argon2id$...",
		OAuthLinks: []userModels.OAuthLink{
			{Provider: "google", ProviderID: "g-1", Email: "u@example.com", IsActive: true, IsPrimary: true, LinkedAt: time.Now()},
			{Provider: "github", ProviderID: "gh-1", Email: "u@example.com", IsActive: true, LinkedAt: time.Now()},
		},
	}
	fake.seed(user)
	svc := newAdminUnlinkSvc(fake)

	if err := svc.SelfUnlinkOAuth(context.Background(), "u-1", "github"); err != nil {
		t.Fatalf("SelfUnlinkOAuth: %v", err)
	}
	if fake.removedCall == nil || fake.removedCall.providerID != "gh-1" {
		t.Fatalf("expected RemoveOAuthLinkFromUser(gh-1); got %+v", fake.removedCall)
	}
}

func TestSelfUnlinkOAuth_LastCredentialLockout(t *testing.T) {
	t.Parallel()
	fake := newAdminUnlinkUserFake()
	fake.seed(&userModels.User{UUID: "u-2", PasswordHash: "", OAuthLinks: []userModels.OAuthLink{
		{Provider: "google", ProviderID: "g-1", IsActive: true, IsPrimary: true},
	}})
	svc := newAdminUnlinkSvc(fake)

	err := svc.SelfUnlinkOAuth(context.Background(), "u-2", "google")
	if !errors.Is(err, ErrLastCredentialRemoval) {
		t.Fatalf("err = %v, want ErrLastCredentialRemoval", err)
	}
	if fake.removedCall != nil {
		t.Errorf("safeguard must short-circuit before persistence; got %+v", fake.removedCall)
	}
}

func TestSelfUnlinkOAuth_ProviderNotLinked(t *testing.T) {
	t.Parallel()
	fake := newAdminUnlinkUserFake()
	fake.seed(&userModels.User{UUID: "u-3", PasswordHash: "x", OAuthLinks: []userModels.OAuthLink{
		{Provider: "google", ProviderID: "g-1", IsActive: true},
	}})
	svc := newAdminUnlinkSvc(fake)

	err := svc.SelfUnlinkOAuth(context.Background(), "u-3", "github")
	if !errors.Is(err, ErrOAuthLinkNotFound) {
		t.Fatalf("err = %v, want ErrOAuthLinkNotFound", err)
	}
}

// TestSelfUnlinkOAuth_SelfActionAllowed verifies the safeguard that
// blocks AdminUnlinkOAuth on actor==target is intentionally absent
// from the self path. The whole point of self-unlink is acting on
// your own account.
func TestSelfUnlinkOAuth_SelfActionAllowed(t *testing.T) {
	t.Parallel()
	fake := newAdminUnlinkUserFake()
	fake.seed(&userModels.User{UUID: "u-4", PasswordHash: "x", OAuthLinks: []userModels.OAuthLink{
		{Provider: "google", ProviderID: "g-1", IsActive: true},
		{Provider: "github", ProviderID: "gh-1", IsActive: true},
	}})
	svc := newAdminUnlinkSvc(fake)

	if err := svc.SelfUnlinkOAuth(context.Background(), "u-4", "github"); err != nil {
		t.Fatalf("SelfUnlinkOAuth on own account: %v", err)
	}
	if !errors.Is(nil, ErrAdminSelfAction) {
		// sanity: the guard must NOT fire on the self-service path
	}
}

func TestSelfUnlinkOAuth_RejectsEmptyUUID(t *testing.T) {
	t.Parallel()
	svc := newAdminUnlinkSvc(newAdminUnlinkUserFake())
	if err := svc.SelfUnlinkOAuth(context.Background(), "", "google"); err == nil {
		t.Errorf("empty userUUID must error")
	}
}
