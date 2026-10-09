package services

// Two concurrent unlinks on one account (Google and GitHub, no usable
// password) could each read both links, each pass the lockout guard,
// and each delete a different row — leaving no way to sign in. The
// guard and the removal therefore run under a per-account lock that
// holds across replicas; a second change on the same account is refused
// while the first holds it, and a lock store that cannot answer refuses
// rather than guesses.

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/orkestra/backend/pkg/sdk/iface"
)

// fakeUnlinkLock is the narrow SetNX + Eval shape the lock needs. It
// checks the ordering contract itself: nothing may have been removed
// when the lock is taken, and the removal must be done when it is
// released.
type fakeUnlinkLock struct {
	mu       sync.Mutex
	acquire  bool
	err      error
	users    *adminUnlinkUserFake
	keys     []string
	released int
	t        *testing.T
}

func (f *fakeUnlinkLock) SetNX(_ context.Context, key string, _ interface{}, ttl time.Duration) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.keys = append(f.keys, key)
	if f.users != nil && f.users.removedCall != nil {
		f.t.Errorf("the lock must be taken before anything is removed")
	}
	if ttl <= 0 || ttl > time.Minute {
		f.t.Errorf("the lock must expire on its own within a minute, got %v", ttl)
	}
	return f.acquire, f.err
}

func (f *fakeUnlinkLock) Eval(_ context.Context, script string, keys []string, _ ...interface{}) (interface{}, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if !strings.Contains(script, "DEL") {
		f.t.Errorf("release must delete the key, script was %q", script)
	}
	if f.users != nil && f.users.removedCall == nil {
		f.t.Errorf("the lock must be held until the removal is done")
	}
	f.released++
	return int64(1), nil
}

func twoLinkedUser(uuid string) *iface.User {
	return &iface.User{
		UUID: uuid, Email: uuid + "@example.com", PasswordHash: "",
		OAuthLinks: []iface.OAuthLink{
			{Provider: "google", ProviderID: "g-1", IsActive: true, IsPrimary: true, LinkedAt: time.Now()},
			{Provider: "github", ProviderID: "gh-1", IsActive: true, LinkedAt: time.Now()},
		},
	}
}

func TestSelfUnlinkOAuth_HoldsTheAccountLockAcrossGuardAndRemoval(t *testing.T) {
	fake := newAdminUnlinkUserFake()
	fake.seed(twoLinkedUser("u-1"))
	svc := newGuardedUnlinkSvc(fake, nil, map[iface.OAuthProvider]bool{"google": true, "github": true}, nil)
	lock := &fakeUnlinkLock{acquire: true, users: fake, t: t}
	svc.SetUnlinkLock(lock)

	if err := svc.SelfUnlinkOAuth(context.Background(), "u-1", "github"); err != nil {
		t.Fatalf("SelfUnlinkOAuth: %v", err)
	}
	if fake.removedCall == nil || fake.removedCall.providerID != "gh-1" {
		t.Fatalf("expected the github link removed, got %+v", fake.removedCall)
	}
	if len(lock.keys) != 1 || !strings.Contains(lock.keys[0], "u-1") {
		t.Fatalf("the lock must be keyed per account, got %v", lock.keys)
	}
	if lock.released != 1 {
		t.Fatalf("the lock must be released exactly once, got %d", lock.released)
	}
}

func TestSelfUnlinkOAuth_RefusesWhileAnotherChangeHoldsTheAccount(t *testing.T) {
	fake := newAdminUnlinkUserFake()
	fake.seed(twoLinkedUser("u-2"))
	svc := newGuardedUnlinkSvc(fake, nil, map[iface.OAuthProvider]bool{"google": true, "github": true}, nil)
	svc.SetUnlinkLock(&fakeUnlinkLock{acquire: false, users: fake, t: t})

	err := svc.SelfUnlinkOAuth(context.Background(), "u-2", "github")
	if !errors.Is(err, ErrCredentialChangeInProgress) {
		t.Fatalf("err = %v, want ErrCredentialChangeInProgress", err)
	}
	if fake.removedCall != nil {
		t.Fatalf("nothing may be removed without the lock, got %+v", fake.removedCall)
	}
}

func TestSelfUnlinkOAuth_LockStoreFailureRefuses(t *testing.T) {
	fake := newAdminUnlinkUserFake()
	fake.seed(twoLinkedUser("u-3"))
	svc := newGuardedUnlinkSvc(fake, nil, map[iface.OAuthProvider]bool{"google": true, "github": true}, nil)
	svc.SetUnlinkLock(&fakeUnlinkLock{err: errors.New("redis: connection refused"), users: fake, t: t})

	err := svc.SelfUnlinkOAuth(context.Background(), "u-3", "github")
	if !errors.Is(err, ErrOAuthStoreUnavailable) {
		t.Fatalf("err = %v, want ErrOAuthStoreUnavailable (refuse, never guess)", err)
	}
	if fake.removedCall != nil {
		t.Fatalf("nothing may be removed when the lock store cannot answer, got %+v", fake.removedCall)
	}
}

func TestAdminUnlinkOAuth_RefusesWhileAnotherChangeHoldsTheAccount(t *testing.T) {
	fake := newAdminUnlinkUserFake()
	fake.seed(twoLinkedUser("u-4"))
	svc := newGuardedUnlinkSvc(fake, nil, map[iface.OAuthProvider]bool{"google": true, "github": true}, nil)
	lock := &fakeUnlinkLock{acquire: false, users: fake, t: t}
	svc.SetUnlinkLock(lock)

	err := svc.AdminUnlinkOAuth(context.Background(), "admin-1", "u-4", "google")
	if !errors.Is(err, ErrCredentialChangeInProgress) {
		t.Fatalf("err = %v, want ErrCredentialChangeInProgress", err)
	}
	if fake.removedCall != nil {
		t.Fatalf("nothing may be removed without the lock, got %+v", fake.removedCall)
	}
	// The admin and the self path contend for the SAME key: the account
	// is what is serialized, not the caller.
	if len(lock.keys) != 1 || !strings.Contains(lock.keys[0], "u-4") {
		t.Fatalf("the lock must be keyed by the target account, got %v", lock.keys)
	}
}

// A fork that never wires a lock keeps today's behaviour: the guard runs
// unserialized rather than refusing every unlink.
func TestSelfUnlinkOAuth_NoLockWiredStillUnlinks(t *testing.T) {
	fake := newAdminUnlinkUserFake()
	fake.seed(twoLinkedUser("u-5"))
	svc := newGuardedUnlinkSvc(fake, nil, map[iface.OAuthProvider]bool{"google": true, "github": true}, nil)
	if err := svc.SelfUnlinkOAuth(context.Background(), "u-5", "github"); err != nil {
		t.Fatalf("SelfUnlinkOAuth without a lock: %v", err)
	}
}
