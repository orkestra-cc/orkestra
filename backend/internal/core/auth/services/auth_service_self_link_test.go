package services

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	authModels "github.com/orkestra/backend/internal/core/auth/models"
	"github.com/orkestra/backend/pkg/sdk/iface"
)

// fakeOAuthProviderRepo is the minimal shape SelfLinkOAuthFromCallback
// touches: GetByProviderAndID for the "already-claimed" check and
// CreateOAuthProvider for the provider-side mirror. Other methods
// panic so a future dependency surfaces as a test failure.
type fakeOAuthProviderRepo struct {
	mu        sync.Mutex
	byKey     map[string]*authModels.OAuthProviderDoc // key = provider+"|"+providerID
	created   []*authModels.OAuthProviderDoc
	createErr error
}

func newFakeOAuthProviderRepo() *fakeOAuthProviderRepo {
	return &fakeOAuthProviderRepo{byKey: map[string]*authModels.OAuthProviderDoc{}}
}

func (r *fakeOAuthProviderRepo) seed(d *authModels.OAuthProviderDoc) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.byKey[string(d.Provider)+"|"+d.ProviderID] = d
}

func (r *fakeOAuthProviderRepo) CreateOAuthProvider(_ context.Context, d *authModels.OAuthProviderDoc) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.createErr != nil {
		return r.createErr
	}
	r.byKey[string(d.Provider)+"|"+d.ProviderID] = d
	r.created = append(r.created, d)
	return nil
}

func (r *fakeOAuthProviderRepo) GetByProviderAndID(_ context.Context, provider authModels.OAuthProvider, providerID string) (*authModels.OAuthProviderDoc, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if d, ok := r.byKey[string(provider)+"|"+providerID]; ok {
		return d, nil
	}
	return nil, nil
}

func (r *fakeOAuthProviderRepo) LinkOAuthProvider(context.Context, string, *authModels.OAuthLink) error {
	panic("unused: LinkOAuthProvider")
}
func (r *fakeOAuthProviderRepo) GetByUserUUID(context.Context, string) ([]*authModels.OAuthProviderDoc, error) {
	panic("unused: GetByUserUUID")
}
func (r *fakeOAuthProviderRepo) GetPrimaryProvider(context.Context, string) (*authModels.OAuthProviderDoc, error) {
	panic("unused: GetPrimaryProvider")
}
func (r *fakeOAuthProviderRepo) UpdateLastUsed(context.Context, string) error {
	panic("unused: UpdateLastUsed")
}
func (r *fakeOAuthProviderRepo) SetPrimaryProvider(context.Context, string, authModels.OAuthProvider) error {
	panic("unused: SetPrimaryProvider")
}
func (r *fakeOAuthProviderRepo) UpdateRefreshToken(context.Context, string, string) error {
	panic("unused: UpdateRefreshToken")
}
func (r *fakeOAuthProviderRepo) UpdateOAuthTokens(context.Context, string, string, string, *time.Time, *time.Time, []string) error {
	panic("unused: UpdateOAuthTokens")
}
func (r *fakeOAuthProviderRepo) UpdateMetadata(context.Context, string, map[string]interface{}) error {
	return nil
}
func (r *fakeOAuthProviderRepo) UnlinkProvider(context.Context, string, authModels.OAuthProvider) error {
	panic("unused: UnlinkProvider")
}
func (r *fakeOAuthProviderRepo) DeleteProvider(context.Context, string) error {
	panic("unused: DeleteProvider")
}
func (r *fakeOAuthProviderRepo) FindByEmail(context.Context, string) ([]*authModels.OAuthProviderDoc, error) {
	panic("unused: FindByEmail")
}
func (r *fakeOAuthProviderRepo) ConsolidateProviders(context.Context, string, string) error {
	panic("unused: ConsolidateProviders")
}

func newSelfLinkSvc(users *adminUnlinkUserFake, repo *fakeOAuthProviderRepo) *authService {
	return &authService{userService: users, oauthProviderRepo: repo}
}

func sampleUserInfo(providerID, email string) map[string]interface{} {
	return map[string]interface{}{
		"provider_id":    providerID,
		"email":          email,
		"name":           "Sample User",
		"picture":        "https://example.com/pic.png",
		"email_verified": true,
	}
}

func TestSelfLinkOAuth_Success(t *testing.T) {
	t.Parallel()
	users := newAdminUnlinkUserFake()
	users.seed(&iface.User{
		UUID:         "u-1",
		Email:        "u@example.com",
		PasswordHash: "argon2id$...",
		OAuthLinks:   []iface.OAuthLink{},
	})
	repo := newFakeOAuthProviderRepo()
	svc := newSelfLinkSvc(users, repo)

	err := svc.SelfLinkOAuthFromCallback(
		context.Background(),
		"u-1",
		"google",
		sampleUserInfo("g-123", "u@example.com"),
		nil,
	)
	if err != nil {
		t.Fatalf("link: %v", err)
	}
	if len(repo.created) != 1 {
		t.Fatalf("provider repo write count = %d, want 1", len(repo.created))
	}
	if repo.created[0].UserUUID != "u-1" || repo.created[0].ProviderID != "g-123" {
		t.Errorf("provider doc = %+v", repo.created[0])
	}
	user := users.users["u-1"]
	if len(user.OAuthLinks) != 1 || user.OAuthLinks[0].Provider != "google" || user.OAuthLinks[0].ProviderID != "g-123" {
		t.Errorf("user.OAuthLinks = %+v", user.OAuthLinks)
	}
}

func TestSelfLinkOAuth_RejectsClaimedByOther(t *testing.T) {
	t.Parallel()
	users := newAdminUnlinkUserFake()
	users.seed(&iface.User{UUID: "u-attacker", PasswordHash: "x", OAuthLinks: []iface.OAuthLink{}})
	repo := newFakeOAuthProviderRepo()
	repo.seed(&authModels.OAuthProviderDoc{
		UUID:       "existing",
		UserUUID:   "u-victim",
		Provider:   "google",
		ProviderID: "g-123",
		Email:      "victim@example.com",
	})
	svc := newSelfLinkSvc(users, repo)

	err := svc.SelfLinkOAuthFromCallback(
		context.Background(),
		"u-attacker",
		"google",
		sampleUserInfo("g-123", "victim@example.com"),
		nil,
	)
	if !errors.Is(err, ErrOAuthLinkClaimedByOther) {
		t.Fatalf("err = %v, want ErrOAuthLinkClaimedByOther", err)
	}
	if len(repo.created) != 0 {
		t.Errorf("must not write provider doc when identity is claimed elsewhere")
	}
	user := users.users["u-attacker"]
	if len(user.OAuthLinks) != 0 {
		t.Errorf("must not append to attacker's OAuthLinks")
	}
}

func TestSelfLinkOAuth_IdempotentReLink(t *testing.T) {
	t.Parallel()
	users := newAdminUnlinkUserFake()
	users.seed(&iface.User{
		UUID:         "u-1",
		PasswordHash: "x",
		OAuthLinks: []iface.OAuthLink{
			{Provider: "google", ProviderID: "g-123", Email: "u@example.com", IsActive: true, LinkedAt: time.Now()},
		},
	})
	repo := newFakeOAuthProviderRepo()
	repo.seed(&authModels.OAuthProviderDoc{
		UUID:       "existing",
		UserUUID:   "u-1",
		Provider:   "google",
		ProviderID: "g-123",
		Email:      "u@example.com",
	})
	svc := newSelfLinkSvc(users, repo)

	if err := svc.SelfLinkOAuthFromCallback(
		context.Background(),
		"u-1",
		"google",
		sampleUserInfo("g-123", "u@example.com"),
		nil,
	); err != nil {
		t.Fatalf("idempotent re-link must not error, got %v", err)
	}
	if len(repo.created) != 0 {
		t.Errorf("idempotent re-link must not write again, got %d", len(repo.created))
	}
}

func TestSelfLinkOAuth_RejectsDuplicateProvider(t *testing.T) {
	t.Parallel()
	// User already has a Google link to a different providerID.
	// A new Google link (different account) must be refused — one user
	// can only attach one identity per provider in this iteration.
	users := newAdminUnlinkUserFake()
	users.seed(&iface.User{
		UUID:         "u-1",
		PasswordHash: "x",
		OAuthLinks: []iface.OAuthLink{
			{Provider: "google", ProviderID: "g-existing", Email: "old@example.com", IsActive: true, LinkedAt: time.Now()},
		},
	})
	repo := newFakeOAuthProviderRepo()
	svc := newSelfLinkSvc(users, repo)

	err := svc.SelfLinkOAuthFromCallback(
		context.Background(),
		"u-1",
		"google",
		sampleUserInfo("g-fresh", "new@example.com"),
		nil,
	)
	if !errors.Is(err, ErrOAuthLinkAlreadyExists) {
		t.Fatalf("err = %v, want ErrOAuthLinkAlreadyExists", err)
	}
}

func TestSelfLinkOAuth_RejectsIncompleteUserInfo(t *testing.T) {
	t.Parallel()
	users := newAdminUnlinkUserFake()
	users.seed(&iface.User{UUID: "u-1", PasswordHash: "x"})
	repo := newFakeOAuthProviderRepo()
	svc := newSelfLinkSvc(users, repo)

	err := svc.SelfLinkOAuthFromCallback(
		context.Background(),
		"u-1",
		"google",
		map[string]interface{}{"name": "no provider id"},
		nil,
	)
	if !errors.Is(err, ErrOAuthLinkInvalidUserInfo) {
		t.Fatalf("err = %v, want ErrOAuthLinkInvalidUserInfo", err)
	}
}

// nilUserProviderFake answers GetUserByID with (nil, nil) — the one input
// that reaches SelfLinkOAuthFromCallback's `user == nil` branch. A
// conforming provider returns iface.ErrUserNotFound instead (the seeded
// fake above does), so this shape has to be built deliberately.
type nilUserProviderFake struct{ *adminUnlinkUserFake }

func (nilUserProviderFake) GetUserByID(context.Context, string) (*iface.User, error) {
	return nil, nil
}

// TestSelfLinkOAuth_NilUserReturnsTheSDKSentinel pins the pairing spec
// §8 #18(c) requires: the handler mappers classify not-found with
// errors.Is(err, iface.ErrUserNotFound), so a branch that returns a FRESH
// fmt.Errorf("user not found") — a different error value that only matched
// by message — is invisible to them. This branch must return the sentinel,
// wrapped, and it must not go back to being a bare string.
func TestSelfLinkOAuth_NilUserReturnsTheSDKSentinel(t *testing.T) {
	t.Parallel()
	svc := &authService{
		userService:       nilUserProviderFake{newAdminUnlinkUserFake()},
		oauthProviderRepo: newFakeOAuthProviderRepo(),
	}

	err := svc.SelfLinkOAuthFromCallback(
		context.Background(),
		"u-missing",
		"google",
		sampleUserInfo("g-123", "u@example.com"),
		nil,
	)
	if err == nil {
		t.Fatal("link with a nil user: err = nil, want the not-found sentinel")
	}
	if !errors.Is(err, iface.ErrUserNotFound) {
		t.Fatalf("err = %v, want it to wrap iface.ErrUserNotFound", err)
	}
}
