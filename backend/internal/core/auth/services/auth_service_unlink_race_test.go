package services

// The unlink race, auth's half.
//
// AdminUnlinkOAuth and SelfUnlinkOAuth read the user, decide, and only then
// call RemoveOAuthLinkFromUser — so a user soft-deleted inside that window
// makes the WRITE, not the read, answer not-found. That error is returned to
// the handler mappers verbatim, and since those now classify with
// errors.Is(err, iface.ErrUserNotFound) (spec §8 #18(c)), the chain only ends
// in a 404 if BOTH links hold:
//
//  1. the user module's RemoveOAuthLinkFromUser returns the SDK sentinel and
//     not the repository's own look-alike value — fixed in this commit,
//     covered by user/services' TestDelegationsTranslateRepositoryNotFound;
//  2. these two auth methods propagate it without replacing it with a verdict
//     (ErrOAuthLinkNotFound) or wrapping it in something errors.Is cannot see
//     — which is what this file pins.
//
// The two live in different modules and neither may import the other's
// services package, so no single test can span the chain; the mapper's end is
// handlers' TestMappersClassifyWrappedUserNotFound / …ClassifyBareUserNotFound.

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/orkestra/backend/pkg/sdk/iface"
)

// unlinkRaceUserFake makes the WRITE fail after the reads have succeeded —
// the shape of a delete landing mid-operation.
type unlinkRaceUserFake struct {
	*adminUnlinkUserFake
	removeErr error
}

func (f *unlinkRaceUserFake) RemoveOAuthLinkFromUser(context.Context, string, iface.OAuthProvider, string) error {
	return f.removeErr
}

func newUnlinkRaceSvc(users iface.UserProvider) *authService {
	s := &authService{userService: users}
	s.policy = &AuthPolicyService{cs: &stubReader{}}
	s.audience = PolicyAudienceOperator
	s.SetProviderUsability(func(context.Context, PolicyAudience, iface.OAuthProvider) (bool, error) {
		return true, nil
	})
	return s
}

func seedUnlinkRaceUser(uuid string) *iface.User {
	return &iface.User{
		UUID:         uuid,
		Email:        uuid + "@example.com",
		PasswordHash: "argon2id$...", // a usable password, so the lockout guard stays out of the way
		OAuthLinks: []iface.OAuthLink{
			{Provider: "google", ProviderID: "g-1", IsActive: true, IsPrimary: true, LinkedAt: time.Now()},
			{Provider: "github", ProviderID: "gh-1", IsActive: true, LinkedAt: time.Now()},
		},
	}
}

func TestUnlinkRace_NotFoundOnTheWriteReachesTheMapperIntact(t *testing.T) {
	t.Parallel()

	// Both the bare sentinel (what user/services returns) and a wrapped one
	// (what a fork's provider may return) must survive the trip.
	for _, remove := range []struct {
		name string
		err  error
	}{
		{"bare sentinel", iface.ErrUserNotFound},
		{"wrapped sentinel", fmt.Errorf("remove oauth link: %w", iface.ErrUserNotFound)},
	} {
		t.Run(remove.name, func(t *testing.T) {
			t.Run("AdminUnlinkOAuth", func(t *testing.T) {
				base := newAdminUnlinkUserFake()
				base.seed(seedUnlinkRaceUser("target-uuid"))
				svc := newUnlinkRaceSvc(&unlinkRaceUserFake{adminUnlinkUserFake: base, removeErr: remove.err})

				err := svc.AdminUnlinkOAuth(context.Background(), "actor-uuid", "target-uuid", "github")
				assertUnlinkRaceNotFound(t, err)
			})
			t.Run("SelfUnlinkOAuth", func(t *testing.T) {
				base := newAdminUnlinkUserFake()
				base.seed(seedUnlinkRaceUser("u-1"))
				svc := newUnlinkRaceSvc(&unlinkRaceUserFake{adminUnlinkUserFake: base, removeErr: remove.err})

				err := svc.SelfUnlinkOAuth(context.Background(), "u-1", "github")
				assertUnlinkRaceNotFound(t, err)
			})
		})
	}
}

func assertUnlinkRaceNotFound(t *testing.T, err error) {
	t.Helper()
	if err == nil {
		t.Fatal("err = nil, want the not-found sentinel from the write")
	}
	if !errors.Is(err, iface.ErrUserNotFound) {
		t.Fatalf("err = %v, want it to satisfy errors.Is(iface.ErrUserNotFound) — the handler mappers answer 404 on identity alone, so anything that loses it is a 500", err)
	}
	// It must stay the store's account of what happened, not be rewritten
	// into one of the unlink verdicts, which mean something else entirely.
	if errors.Is(err, ErrOAuthLinkNotFound) || errors.Is(err, ErrLastCredentialRemoval) {
		t.Fatalf("err = %v, want the not-found sentinel and not an unlink verdict", err)
	}
}

// The bound: a genuine store failure on the same write must NOT acquire
// not-found, or the race fix turns every outage into a 404.
func TestUnlinkRace_StoreFailureOnTheWriteIsNotNotFound(t *testing.T) {
	t.Parallel()
	boom := errors.New("mongo: no reachable servers")
	base := newAdminUnlinkUserFake()
	base.seed(seedUnlinkRaceUser("target-uuid"))
	svc := newUnlinkRaceSvc(&unlinkRaceUserFake{adminUnlinkUserFake: base, removeErr: boom})

	err := svc.AdminUnlinkOAuth(context.Background(), "actor-uuid", "target-uuid", "github")
	if !errors.Is(err, boom) {
		t.Fatalf("err = %v, want the store failure unchanged", err)
	}
	if errors.Is(err, iface.ErrUserNotFound) {
		t.Fatal("a store outage must not be reported as not-found")
	}
}
