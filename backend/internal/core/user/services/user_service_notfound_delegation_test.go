package services

// repository.ErrUserNotFound and iface.ErrUserNotFound are two DIFFERENT
// error values carrying the same message. The lookups on this service have
// always translated the first into the second; the thin delegations below
// used to return the repository value RAW, which nothing noticed while
// consumers classified not-found by comparing err.Error() — and became
// load-bearing the moment auth's handler mappers moved to
// errors.Is(err, iface.ErrUserNotFound).
//
// The reachable case is an unlink race: AdminUnlinkOAuth / SelfUnlinkOAuth
// read the user, decide, then call RemoveOAuthLinkFromUser, and a user
// soft-deleted in that window answered 404 before and would have started
// answering 500. See auth/handlers' TestUnlinkRace_* for that end of it.

import (
	"context"
	"errors"
	"testing"

	"github.com/orkestra/backend/internal/core/user/repository"
	"github.com/orkestra/backend/pkg/sdk/iface"
)

// notFoundRepo answers every method this test drives with the REPOSITORY's
// own not-found value — the input the delegations used to pass straight
// through. It embeds fakeUserRepo so the rest of the (broad) interface is
// satisfied without restating it.
type notFoundRepo struct{ *fakeUserRepo }

func (notFoundRepo) GetByID(context.Context, string) (*iface.User, error) {
	return nil, repository.ErrUserNotFound
}
func (notFoundRepo) GetOAuthLinks(context.Context, string) ([]iface.OAuthLink, error) {
	return nil, repository.ErrUserNotFound
}
func (notFoundRepo) AddOAuthLink(context.Context, string, iface.OAuthLink) error {
	return repository.ErrUserNotFound
}
func (notFoundRepo) RemoveOAuthLink(context.Context, string, iface.OAuthProvider, string) error {
	return repository.ErrUserNotFound
}
func (notFoundRepo) SetPrimaryOAuthLink(context.Context, string, iface.OAuthProvider, string) error {
	return repository.ErrUserNotFound
}
func (notFoundRepo) UpdateLastLogin(context.Context, string) error {
	return repository.ErrUserNotFound
}
func (notFoundRepo) UpdatePasswordHash(context.Context, string, string) error {
	return repository.ErrUserNotFound
}
func (notFoundRepo) MarkEmailVerified(context.Context, string) error {
	return repository.ErrUserNotFound
}

// TestDelegationsTranslateRepositoryNotFound covers every delegation that
// used to hand the repository's own value to a caller outside this module.
// One row per delegation, so a new pass-through is one row rather than a
// rewritten test.
func TestDelegationsTranslateRepositoryNotFound(t *testing.T) {
	svc := &userService{userRepo: notFoundRepo{newFakeUserRepo()}}
	ctx := context.Background()

	cases := []struct {
		name string
		call func() error
	}{
		{"AddOAuthLinkToUser", func() error {
			return svc.AddOAuthLinkToUser(ctx, "u-1", iface.OAuthLink{Provider: "google", ProviderID: "g-1"})
		}},
		{"RemoveOAuthLinkFromUser", func() error {
			return svc.RemoveOAuthLinkFromUser(ctx, "u-1", "google", "g-1")
		}},
		{"SetPrimaryOAuthLink", func() error {
			return svc.SetPrimaryOAuthLink(ctx, "u-1", "google", "g-1")
		}},
		{"GetUserOAuthLinks", func() error {
			_, err := svc.GetUserOAuthLinks(ctx, "u-1")
			return err
		}},
		{"UpdateUserLastLogin", func() error { return svc.UpdateUserLastLogin(ctx, "u-1") }},
		{"UpdatePasswordHash", func() error { return svc.UpdatePasswordHash(ctx, "u-1", "argon2id$hash") }},
		{"MarkEmailVerified", func() error { return svc.MarkEmailVerified(ctx, "u-1") }},
		{"StartMFAGraceIfUnset", func() error { return svc.StartMFAGraceIfUnset(ctx, "u-1") }},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.call()
			if err == nil {
				t.Fatal("err = nil, want the not-found sentinel")
			}
			if !errors.Is(err, iface.ErrUserNotFound) {
				t.Fatalf("err = %v (%T), want it to satisfy errors.Is(iface.ErrUserNotFound) — a consumer outside this module classifies by identity and cannot see repository.ErrUserNotFound", err, err)
			}
		})
	}
}

// boomRepo is the bound: every other repository failure must pass through
// untouched, or the translation becomes "everything is a 404".
type boomRepo struct{ *fakeUserRepo }

var errRepoBoom = errors.New("mongo: no reachable servers")

func (boomRepo) GetByID(context.Context, string) (*iface.User, error) { return nil, errRepoBoom }
func (boomRepo) GetOAuthLinks(context.Context, string) ([]iface.OAuthLink, error) {
	return nil, errRepoBoom
}
func (boomRepo) RemoveOAuthLink(context.Context, string, iface.OAuthProvider, string) error {
	return errRepoBoom
}
func (boomRepo) UpdatePasswordHash(context.Context, string, string) error { return errRepoBoom }

func TestDelegationsLeaveOtherErrorsAlone(t *testing.T) {
	svc := &userService{userRepo: boomRepo{newFakeUserRepo()}}
	ctx := context.Background()

	cases := map[string]func() error{
		"RemoveOAuthLinkFromUser": func() error { return svc.RemoveOAuthLinkFromUser(ctx, "u-1", "google", "g-1") },
		"GetUserOAuthLinks":       func() error { _, err := svc.GetUserOAuthLinks(ctx, "u-1"); return err },
		"UpdatePasswordHash":      func() error { return svc.UpdatePasswordHash(ctx, "u-1", "argon2id$hash") },
		"StartMFAGraceIfUnset":    func() error { return svc.StartMFAGraceIfUnset(ctx, "u-1") },
	}
	for name, call := range cases {
		t.Run(name, func(t *testing.T) {
			err := call()
			if !errors.Is(err, errRepoBoom) {
				t.Fatalf("err = %v, want the repository failure unchanged", err)
			}
			if errors.Is(err, iface.ErrUserNotFound) {
				t.Fatal("a store outage must not be reported as not-found")
			}
		})
	}
}

// asUserNotFound is the one-liner the delegations share; pin its two rules
// directly so a future caller cannot be surprised by the nil case.
func TestAsUserNotFound(t *testing.T) {
	if got := asUserNotFound(nil); got != nil {
		t.Errorf("asUserNotFound(nil) = %v, want nil", got)
	}
	if got := asUserNotFound(repository.ErrUserNotFound); !errors.Is(got, iface.ErrUserNotFound) {
		t.Errorf("asUserNotFound(repository.ErrUserNotFound) = %v, want the SDK sentinel", got)
	}
	if got := asUserNotFound(errRepoBoom); !errors.Is(got, errRepoBoom) {
		t.Errorf("asUserNotFound(other) = %v, want it unchanged", got)
	}
}
