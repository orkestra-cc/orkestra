package services

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	authModels "github.com/orkestra/backend/internal/core/auth/models"
	userModels "github.com/orkestra/backend/internal/core/user/models"
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
	users       map[string]*userModels.User
	removedCall *removedOAuthCall
}

type removedOAuthCall struct {
	userUUID   string
	provider   userModels.OAuthProvider
	providerID string
}

func newAdminUnlinkUserFake() *adminUnlinkUserFake {
	return &adminUnlinkUserFake{users: map[string]*userModels.User{}}
}

func (f *adminUnlinkUserFake) seed(u *userModels.User) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.users[u.UUID] = u
}

func (f *adminUnlinkUserFake) GetUserByID(_ context.Context, id string) (*userModels.User, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if u, ok := f.users[id]; ok {
		return u, nil
	}
	return nil, errNotFound
}

func (f *adminUnlinkUserFake) GetUserOAuthLinks(_ context.Context, userUUID string) ([]userModels.OAuthLink, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if u, ok := f.users[userUUID]; ok {
		out := make([]userModels.OAuthLink, len(u.OAuthLinks))
		copy(out, u.OAuthLinks)
		return out, nil
	}
	return nil, errNotFound
}

func (f *adminUnlinkUserFake) AddOAuthLinkToUser(_ context.Context, userUUID string, link userModels.OAuthLink) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if u, ok := f.users[userUUID]; ok {
		u.OAuthLinks = append(u.OAuthLinks, link)
		return nil
	}
	return errNotFound
}

func (f *adminUnlinkUserFake) RemoveOAuthLinkFromUser(_ context.Context, userUUID string, provider userModels.OAuthProvider, providerID string) error {
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
func (f *adminUnlinkUserFake) GetUserByEmail(context.Context, string) (*userModels.UserManagementResponse, error) {
	panic("unused: GetUserByEmail")
}
func (f *adminUnlinkUserFake) GetUserForAuth(context.Context, string) (*userModels.User, error) {
	panic("unused: GetUserForAuth")
}
func (f *adminUnlinkUserFake) CreateUserFromOAuth(context.Context, *userModels.CreateUserInput) (*userModels.User, error) {
	panic("unused: CreateUserFromOAuth")
}
func (f *adminUnlinkUserFake) CreateUserWithPassword(context.Context, *userModels.CreateUserInput) (*userModels.User, error) {
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
func (f *adminUnlinkUserFake) UpdateUser(context.Context, string, *userModels.UpdateUserInput) (*userModels.UserManagementResponse, error) {
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
func (f *adminUnlinkUserFake) SetPrimaryOAuthLink(context.Context, string, userModels.OAuthProvider, string) error {
	panic("unused: SetPrimaryOAuthLink")
}
func (f *adminUnlinkUserFake) GetUserCount(context.Context, *userModels.UserFilters) (int64, error) {
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
	target := &userModels.User{
		UUID:         "target-uuid",
		Email:        "target@example.com",
		PasswordHash: "argon2id$...", // has a usable password
		OAuthLinks: []userModels.OAuthLink{
			{Provider: "google", ProviderID: "g-123", Email: "target@example.com", IsActive: true, IsPrimary: true, LinkedAt: time.Now()},
			{Provider: "github", ProviderID: "gh-456", Email: "target@example.com", IsActive: true, LinkedAt: time.Now()},
		},
	}
	fake.seed(target)
	svc := newAdminUnlinkSvc(fake)

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
	fake.seed(&userModels.User{UUID: "same-uuid", PasswordHash: "x", OAuthLinks: []userModels.OAuthLink{
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
	fake.seed(&userModels.User{UUID: "only-oauth", PasswordHash: "", OAuthLinks: []userModels.OAuthLink{
		{Provider: "google", ProviderID: "g-1", Email: "x@y.com", IsActive: true, IsPrimary: true},
	}})
	svc := newAdminUnlinkSvc(fake)

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
	fake.seed(&userModels.User{UUID: "has-pwd", PasswordHash: "argon2id$...", OAuthLinks: []userModels.OAuthLink{
		{Provider: "google", ProviderID: "g-1", IsActive: true},
	}})
	svc := newAdminUnlinkSvc(fake)

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
	fake.seed(&userModels.User{UUID: "u1", PasswordHash: "x", OAuthLinks: []userModels.OAuthLink{
		{Provider: "google", ProviderID: "g-1", IsActive: true},
	}})
	svc := newAdminUnlinkSvc(fake)

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
