package services

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	authModels "github.com/orkestra/backend/internal/core/auth/models"
	"github.com/orkestra/backend/pkg/sdk/iface"
)

// adminUnlinkUserFake is a tiny iface.UserProvider tailored to the
// admin-unlink and get-auth-methods tests. It tracks just enough state
// to exercise the safeguards (last-credential lockout, self-action,
// not-found) and verify a successful call propagates to
// RemoveOAuthLinkFromUser. Everything else panics so a regression that
// adds a new dependency is visible immediately.
//
// Embedding gateUserFake would let us share more boilerplate, but it
// returns nil for OAuth link reads and no-ops the remove — both of
// which we need to exercise here. A purpose-built fake keeps the test
// state explicit.
type adminUnlinkUserFake struct {
	mu          sync.Mutex
	users       map[string]*iface.User
	removedCall *removedOAuthCall
}

type removedOAuthCall struct {
	userUUID   string
	provider   iface.OAuthProvider
	providerID string
}

func newAdminUnlinkUserFake() *adminUnlinkUserFake {
	return &adminUnlinkUserFake{users: map[string]*iface.User{}}
}

func (f *adminUnlinkUserFake) seed(u *iface.User) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.users[u.UUID] = u
}

func (f *adminUnlinkUserFake) GetUserByID(_ context.Context, id string) (*iface.User, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if u, ok := f.users[id]; ok {
		return u, nil
	}
	return nil, errNotFound
}

func (f *adminUnlinkUserFake) GetUserOAuthLinks(_ context.Context, userUUID string) ([]iface.OAuthLink, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if u, ok := f.users[userUUID]; ok {
		out := make([]iface.OAuthLink, len(u.OAuthLinks))
		copy(out, u.OAuthLinks)
		return out, nil
	}
	return nil, errNotFound
}

func (f *adminUnlinkUserFake) AddOAuthLinkToUser(_ context.Context, userUUID string, link iface.OAuthLink) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if u, ok := f.users[userUUID]; ok {
		u.OAuthLinks = append(u.OAuthLinks, link)
		return nil
	}
	return errNotFound
}

func (f *adminUnlinkUserFake) RemoveOAuthLinkFromUser(_ context.Context, userUUID string, provider iface.OAuthProvider, providerID string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.removedCall = &removedOAuthCall{userUUID: userUUID, provider: provider, providerID: providerID}
	if u, ok := f.users[userUUID]; ok {
		filtered := u.OAuthLinks[:0]
		for _, link := range u.OAuthLinks {
			if link.Provider == provider && link.ProviderID == providerID {
				continue
			}
			filtered = append(filtered, link)
		}
		u.OAuthLinks = filtered
	}
	return nil
}

// Unused-but-required UserProvider methods. Each panics to surface
// any unexpected dependency the system under test introduces.
func (f *adminUnlinkUserFake) GetUserByEmail(context.Context, string) (*iface.UserManagementResponse, error) {
	panic("unused: GetUserByEmail")
}
func (f *adminUnlinkUserFake) GetUserForAuth(context.Context, string) (*iface.User, error) {
	panic("unused: GetUserForAuth")
}
func (f *adminUnlinkUserFake) CreateUserFromOAuth(context.Context, *iface.CreateUserInput) (*iface.User, error) {
	panic("unused: CreateUserFromOAuth")
}
func (f *adminUnlinkUserFake) CreateUserWithPassword(context.Context, *iface.CreateUserInput) (*iface.User, error) {
	panic("unused: CreateUserWithPassword")
}
func (f *adminUnlinkUserFake) UpdatePasswordHash(context.Context, string, string) error {
	panic("unused: UpdatePasswordHash")
}
func (f *adminUnlinkUserFake) MarkEmailVerified(context.Context, string) error {
	panic("unused: MarkEmailVerified")
}
func (f *adminUnlinkUserFake) RecordFailedLogin(context.Context, string, *time.Time) error {
	panic("unused: RecordFailedLogin")
}
func (f *adminUnlinkUserFake) ClearFailedLogins(context.Context, string) error {
	panic("unused: ClearFailedLogins")
}
func (f *adminUnlinkUserFake) UpdateUser(context.Context, string, *iface.UpdateUserInput) (*iface.UserManagementResponse, error) {
	panic("unused: UpdateUser")
}
func (f *adminUnlinkUserFake) UpdateUserLastLogin(context.Context, string) error {
	panic("unused: UpdateUserLastLogin")
}
func (f *adminUnlinkUserFake) DeleteUser(context.Context, string) error {
	panic("unused: DeleteUser")
}
func (f *adminUnlinkUserFake) SoftDeleteAndAliasEmail(context.Context, string) error {
	panic("unused: SoftDeleteAndAliasEmail")
}
func (f *adminUnlinkUserFake) SetPrimaryOAuthLink(context.Context, string, iface.OAuthProvider, string) error {
	panic("unused: SetPrimaryOAuthLink")
}
func (f *adminUnlinkUserFake) GetUserCount(context.Context, *iface.UserFilters) (int64, error) {
	panic("unused: GetUserCount")
}
func (f *adminUnlinkUserFake) StartMFAGraceIfUnset(context.Context, string) error {
	panic("unused: StartMFAGraceIfUnset")
}
func (f *adminUnlinkUserFake) ResetMFAGrace(context.Context, string) error {
	panic("unused: ResetMFAGrace")
}
func (f *adminUnlinkUserFake) ClearMFAGrace(context.Context, string) error {
	panic("unused: ClearMFAGrace")
}

// newAdminUnlinkSvc constructs a minimal *authService backed by the
// fake user provider. Only the fields AdminUnlinkOAuth touches are
// populated — riskAssessment, jwt, etc. are nil because the unlink
// path never reaches them.
func newAdminUnlinkSvc(fake *adminUnlinkUserFake) *authService {
	return &authService{userService: fake}
}

func TestAdminUnlinkOAuth_Success(t *testing.T) {
	t.Parallel()
	fake := newAdminUnlinkUserFake()
	target := &iface.User{
		UUID:         "target-uuid",
		Email:        "target@example.com",
		PasswordHash: "argon2id$...", // has a usable password
		OAuthLinks: []iface.OAuthLink{
			{Provider: "google", ProviderID: "g-123", Email: "target@example.com", IsActive: true, IsPrimary: true, LinkedAt: time.Now()},
			{Provider: "github", ProviderID: "gh-456", Email: "target@example.com", IsActive: true, LinkedAt: time.Now()},
		},
	}
	fake.seed(target)
	svc := newGuardedUnlinkSvc(fake, nil, map[iface.OAuthProvider]bool{"google": true, "github": true}, nil)

	if err := svc.AdminUnlinkOAuth(context.Background(), "actor-uuid", "target-uuid", "github"); err != nil {
		t.Fatalf("AdminUnlinkOAuth: %v", err)
	}
	if fake.removedCall == nil {
		t.Fatalf("expected RemoveOAuthLinkFromUser to be called")
	}
	if fake.removedCall.providerID != "gh-456" {
		t.Errorf("provider id = %q, want gh-456", fake.removedCall.providerID)
	}
	if len(target.OAuthLinks) != 1 || target.OAuthLinks[0].Provider != "google" {
		t.Errorf("after unlink, links = %+v, want only google", target.OAuthLinks)
	}
}

func TestAdminUnlinkOAuth_SelfAction(t *testing.T) {
	t.Parallel()
	fake := newAdminUnlinkUserFake()
	fake.seed(&iface.User{UUID: "same-uuid", PasswordHash: "x", OAuthLinks: []iface.OAuthLink{
		{Provider: "google", ProviderID: "g-1", IsActive: true},
		{Provider: "github", ProviderID: "gh-1", IsActive: true},
	}})
	svc := newAdminUnlinkSvc(fake)

	err := svc.AdminUnlinkOAuth(context.Background(), "same-uuid", "same-uuid", "github")
	if !errors.Is(err, ErrAdminSelfAction) {
		t.Fatalf("err = %v, want ErrAdminSelfAction", err)
	}
	if fake.removedCall != nil {
		t.Errorf("self-action must not reach the user provider; got call %+v", fake.removedCall)
	}
}

func TestAdminUnlinkOAuth_LastCredentialLockout(t *testing.T) {
	t.Parallel()
	fake := newAdminUnlinkUserFake()
	// OAuth-only user with a single link — unlinking would lock them out.
	fake.seed(&iface.User{UUID: "only-oauth", PasswordHash: "", OAuthLinks: []iface.OAuthLink{
		{Provider: "google", ProviderID: "g-1", Email: "x@y.com", IsActive: true, IsPrimary: true},
	}})
	svc := newGuardedUnlinkSvc(fake, nil, map[iface.OAuthProvider]bool{"google": true}, nil)

	err := svc.AdminUnlinkOAuth(context.Background(), "actor", "only-oauth", "google")
	if !errors.Is(err, ErrLastCredentialRemoval) {
		t.Fatalf("err = %v, want ErrLastCredentialRemoval", err)
	}
	if fake.removedCall != nil {
		t.Errorf("safeguard must short-circuit before the user provider; got %+v", fake.removedCall)
	}
}

func TestAdminUnlinkOAuth_LastOAuthLinkButPasswordSet(t *testing.T) {
	t.Parallel()
	// A single OAuth link is fine to unlink IF the user has a password:
	// they retain a usable login method.
	fake := newAdminUnlinkUserFake()
	fake.seed(&iface.User{UUID: "has-pwd", PasswordHash: "argon2id$...", OAuthLinks: []iface.OAuthLink{
		{Provider: "google", ProviderID: "g-1", IsActive: true},
	}})
	svc := newGuardedUnlinkSvc(fake, nil, map[iface.OAuthProvider]bool{"google": true}, nil)

	if err := svc.AdminUnlinkOAuth(context.Background(), "actor", "has-pwd", "google"); err != nil {
		t.Fatalf("AdminUnlinkOAuth: %v", err)
	}
	if fake.removedCall == nil || fake.removedCall.providerID != "g-1" {
		t.Errorf("expected unlink to land; got %+v", fake.removedCall)
	}
}

func TestAdminUnlinkOAuth_ProviderNotLinked(t *testing.T) {
	t.Parallel()
	fake := newAdminUnlinkUserFake()
	fake.seed(&iface.User{UUID: "u1", PasswordHash: "x", OAuthLinks: []iface.OAuthLink{
		{Provider: "google", ProviderID: "g-1", IsActive: true},
	}})
	svc := newGuardedUnlinkSvc(fake, nil, map[iface.OAuthProvider]bool{"google": true}, nil)

	err := svc.AdminUnlinkOAuth(context.Background(), "actor", "u1", "github")
	if !errors.Is(err, ErrOAuthLinkNotFound) {
		t.Fatalf("err = %v, want ErrOAuthLinkNotFound", err)
	}
}

func TestAdminUnlinkOAuth_RejectsEmptyArgs(t *testing.T) {
	t.Parallel()
	fake := newAdminUnlinkUserFake()
	svc := newAdminUnlinkSvc(fake)
	if err := svc.AdminUnlinkOAuth(context.Background(), "", "x", "google"); err == nil {
		t.Errorf("empty actor must error")
	}
	if err := svc.AdminUnlinkOAuth(context.Background(), "x", "", "google"); err == nil {
		t.Errorf("empty target must error")
	}
}

// Compile-time guard: AdminMethodsView pointer equality + a typed
// import keeps the linter quiet about the authModels alias used by
// other tests in this package.
var _ = authModels.AuthMethodsView{}

// newGuardedUnlinkSvc is newAdminUnlinkSvc plus the PR 3 policy inputs:
// a per-surface password policy and the provider-usability seam.
func newGuardedUnlinkSvc(fake *adminUnlinkUserFake, policyValues map[string]string, usable map[iface.OAuthProvider]bool, usabilityErr error) *authService {
	s := newAdminUnlinkSvc(fake)
	s.policy = &AuthPolicyService{cs: &stubReader{values: policyValues}}
	s.audience = PolicyAudienceOperator
	s.SetProviderUsability(func(_ context.Context, _ PolicyAudience, p iface.OAuthProvider) (bool, error) {
		if usabilityErr != nil {
			return false, usabilityErr
		}
		return usable[p], nil
	})
	return s
}

// PR 3 §4.7: the guard counts USABLE links, not active rows.
func TestWouldLockOutOAuthUnlink_UsableSemantics(t *testing.T) {
	google := iface.OAuthLink{Provider: "google", ProviderID: "g-1", IsActive: true}
	github := iface.OAuthLink{Provider: "github", ProviderID: "h-1", IsActive: true}
	userNoPw := &iface.User{UUID: "u", PasswordHash: ""}
	userPw := &iface.User{UUID: "u", PasswordHash: "argon2id$..."}

	cases := []struct {
		name           string
		target         *iface.User
		links          []iface.OAuthLink
		provider       iface.OAuthProvider
		passwordUsable bool
		usable         map[iface.OAuthProvider]bool
		wantLocked     bool
		wantFound      bool
	}{
		{"sole usable link, no password hash → locked",
			userNoPw, []iface.OAuthLink{google}, "google", true, map[iface.OAuthProvider]bool{"google": true}, true, true},
		{"sole usable link, hash present but method off → locked",
			userPw, []iface.OAuthLink{google}, "google", false, map[iface.OAuthProvider]bool{"google": true}, true, true},
		{"sole usable link, usable password → allowed",
			userPw, []iface.OAuthLink{google}, "google", true, map[iface.OAuthProvider]bool{"google": true}, false, true},
		{"target provider itself unusable → removable (not a credential)",
			userNoPw, []iface.OAuthLink{google}, "google", false, map[iface.OAuthProvider]bool{"google": false}, false, true},
		{"another USABLE link remains → allowed",
			userNoPw, []iface.OAuthLink{google, github}, "google", false, map[iface.OAuthProvider]bool{"google": true, "github": true}, false, true},
		{"the other link is unusable → still locked",
			userNoPw, []iface.OAuthLink{google, github}, "google", false, map[iface.OAuthProvider]bool{"google": true, "github": false}, true, true},
		{"inactive other link never counts",
			userNoPw, []iface.OAuthLink{google, {Provider: "github", ProviderID: "h-1", IsActive: false}}, "google", false, map[iface.OAuthProvider]bool{"google": true, "github": true}, true, true},
		{"provider not linked → not found",
			userNoPw, []iface.OAuthLink{google}, "discord", true, map[iface.OAuthProvider]bool{"google": true}, false, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, locked, found := wouldLockOutOAuthUnlink(tc.target, tc.links, tc.provider, tc.passwordUsable, tc.usable)
			if found != tc.wantFound || locked != tc.wantLocked {
				t.Fatalf("got (locked=%v, found=%v), want (%v, %v)", locked, found, tc.wantLocked, tc.wantFound)
			}
		})
	}
}

func TestAdminUnlinkOAuth_UsableLinkGuard(t *testing.T) {
	seedTarget := func(fake *adminUnlinkUserFake, hash string) *iface.User {
		u := &iface.User{UUID: "target-uuid", PasswordHash: hash,
			OAuthLinks: []iface.OAuthLink{{Provider: "google", ProviderID: "g-1", IsActive: true}}}
		fake.seed(u)
		return u
	}
	t.Run("password off makes the sole usable link last_credential", func(t *testing.T) {
		fake := newAdminUnlinkUserFake()
		seedTarget(fake, "argon2id$...")
		svc := newGuardedUnlinkSvc(fake, map[string]string{"passwordLoginEnabledAdmin": "false"},
			map[iface.OAuthProvider]bool{"google": true}, nil)
		if err := svc.AdminUnlinkOAuth(context.Background(), "actor", "target-uuid", "google"); !errors.Is(err, ErrLastCredentialRemoval) {
			t.Fatalf("want ErrLastCredentialRemoval, got %v", err)
		}
	})
	t.Run("unusable target link is removable even with password off", func(t *testing.T) {
		fake := newAdminUnlinkUserFake()
		seedTarget(fake, "")
		svc := newGuardedUnlinkSvc(fake, map[string]string{"passwordLoginEnabledAdmin": "false"},
			map[iface.OAuthProvider]bool{"google": false}, nil)
		if err := svc.AdminUnlinkOAuth(context.Background(), "actor", "target-uuid", "google"); err != nil {
			t.Fatalf("disabled link is not a credential; want removal, got %v", err)
		}
	})
	t.Run("password policy uncertainty refuses with the policy sentinel", func(t *testing.T) {
		fake := newAdminUnlinkUserFake()
		seedTarget(fake, "argon2id$...")
		svc := newGuardedUnlinkSvc(fake, nil, map[iface.OAuthProvider]bool{"google": true}, nil)
		svc.policy = &AuthPolicyService{cs: &stubReader{rawErr: errors.New("mongo down")}}
		if err := svc.AdminUnlinkOAuth(context.Background(), "actor", "target-uuid", "google"); !errors.Is(err, ErrAuthPolicyUnavailable) {
			t.Fatalf("want ErrAuthPolicyUnavailable, got %v", err)
		}
	})
	t.Run("usability uncertainty refuses rather than counting the link", func(t *testing.T) {
		fake := newAdminUnlinkUserFake()
		seedTarget(fake, "argon2id$...")
		svc := newGuardedUnlinkSvc(fake, nil, nil, fmt.Errorf("%w: undecryptable secret", ErrAuthPolicyUnavailable))
		if err := svc.AdminUnlinkOAuth(context.Background(), "actor", "target-uuid", "google"); !errors.Is(err, ErrAuthPolicyUnavailable) {
			t.Fatalf("want ErrAuthPolicyUnavailable, got %v", err)
		}
	})
	t.Run("missing usability wiring is an outage, not a pass", func(t *testing.T) {
		fake := newAdminUnlinkUserFake()
		seedTarget(fake, "argon2id$...")
		svc := newAdminUnlinkSvc(fake)
		svc.policy = &AuthPolicyService{cs: &stubReader{values: map[string]string{}}}
		svc.audience = PolicyAudienceOperator
		if err := svc.AdminUnlinkOAuth(context.Background(), "actor", "target-uuid", "google"); !errors.Is(err, ErrAuthPolicyUnavailable) {
			t.Fatalf("want ErrAuthPolicyUnavailable, got %v", err)
		}
	})
}
