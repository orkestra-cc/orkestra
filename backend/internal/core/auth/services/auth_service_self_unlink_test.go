package services

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/orkestra/backend/pkg/sdk/iface"
)

// SelfUnlinkOAuth shares the lockout helper with AdminUnlinkOAuth, so
// the fake from auth_service_admin_unlink_test.go is reused. The test
// file lives next to its admin-unlink sibling so a future refactor of
// the helper surfaces both call sites.

func TestSelfUnlinkOAuth_Success(t *testing.T) {
	t.Parallel()
	fake := newAdminUnlinkUserFake()
	user := &iface.User{
		UUID:         "u-1",
		Email:        "u@example.com",
		PasswordHash: "argon2id$...",
		OAuthLinks: []iface.OAuthLink{
			{Provider: "google", ProviderID: "g-1", Email: "u@example.com", IsActive: true, IsPrimary: true, LinkedAt: time.Now()},
			{Provider: "github", ProviderID: "gh-1", Email: "u@example.com", IsActive: true, LinkedAt: time.Now()},
		},
	}
	fake.seed(user)
	svc := newGuardedUnlinkSvc(fake, nil, map[iface.OAuthProvider]bool{"google": true, "github": true}, nil)

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
	fake.seed(&iface.User{UUID: "u-2", PasswordHash: "", OAuthLinks: []iface.OAuthLink{
		{Provider: "google", ProviderID: "g-1", IsActive: true, IsPrimary: true},
	}})
	svc := newGuardedUnlinkSvc(fake, nil, map[iface.OAuthProvider]bool{"google": true}, nil)

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
	fake.seed(&iface.User{UUID: "u-3", PasswordHash: "x", OAuthLinks: []iface.OAuthLink{
		{Provider: "google", ProviderID: "g-1", IsActive: true},
	}})
	svc := newGuardedUnlinkSvc(fake, nil, map[iface.OAuthProvider]bool{"google": true}, nil)

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
	fake.seed(&iface.User{UUID: "u-4", PasswordHash: "x", OAuthLinks: []iface.OAuthLink{
		{Provider: "google", ProviderID: "g-1", IsActive: true},
		{Provider: "github", ProviderID: "gh-1", IsActive: true},
	}})
	svc := newGuardedUnlinkSvc(fake, nil, map[iface.OAuthProvider]bool{"google": true, "github": true}, nil)

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

// PR 3 §4.7: the self-service guard counts usable links too.
func TestSelfUnlinkOAuth_UsableLinkGuard(t *testing.T) {
	seed := func(fake *adminUnlinkUserFake, hash string) {
		fake.seed(&iface.User{UUID: "u-1", PasswordHash: hash,
			OAuthLinks: []iface.OAuthLink{{Provider: "google", ProviderID: "g-1", IsActive: true}}})
	}
	t.Run("password off makes the sole usable link last_credential", func(t *testing.T) {
		fake := newAdminUnlinkUserFake()
		seed(fake, "argon2id$...")
		svc := newGuardedUnlinkSvc(fake, map[string]string{"passwordLoginEnabledAdmin": "false"},
			map[iface.OAuthProvider]bool{"google": true}, nil)
		if err := svc.SelfUnlinkOAuth(context.Background(), "u-1", "google"); !errors.Is(err, ErrLastCredentialRemoval) {
			t.Fatalf("want ErrLastCredentialRemoval, got %v", err)
		}
		if fake.removedCall != nil {
			t.Errorf("guard must short-circuit before persistence; got %+v", fake.removedCall)
		}
	})
	t.Run("unusable target link is removable even with no password", func(t *testing.T) {
		fake := newAdminUnlinkUserFake()
		seed(fake, "")
		svc := newGuardedUnlinkSvc(fake, nil, map[iface.OAuthProvider]bool{"google": false}, nil)
		if err := svc.SelfUnlinkOAuth(context.Background(), "u-1", "google"); err != nil {
			t.Fatalf("disabled link is not a credential; want removal, got %v", err)
		}
		if fake.removedCall == nil || fake.removedCall.providerID != "g-1" {
			t.Fatalf("expected RemoveOAuthLinkFromUser(g-1); got %+v", fake.removedCall)
		}
	})
	t.Run("usability uncertainty refuses with the policy sentinel", func(t *testing.T) {
		fake := newAdminUnlinkUserFake()
		seed(fake, "argon2id$...")
		svc := newGuardedUnlinkSvc(fake, nil, nil, fmt.Errorf("%w: undecryptable secret", ErrAuthPolicyUnavailable))
		if err := svc.SelfUnlinkOAuth(context.Background(), "u-1", "google"); !errors.Is(err, ErrAuthPolicyUnavailable) {
			t.Fatalf("want ErrAuthPolicyUnavailable, got %v", err)
		}
		if fake.removedCall != nil {
			t.Errorf("uncertainty must not mutate; got %+v", fake.removedCall)
		}
	})
}
