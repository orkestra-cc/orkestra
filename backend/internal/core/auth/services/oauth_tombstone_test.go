package services

// H-7 (spec §4.8 D32 items 3, 6, 7): unlink $pulls only user.oauthLinks,
// so the provider document — which is what LOGIN keys on — survives and
// the identity keeps signing in. This release makes every reader
// tombstone-aware and moves it onto the provider collection; the next
// one writes the tombstone.

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	authModels "github.com/orkestra/backend/internal/core/auth/models"
	"github.com/orkestra/backend/internal/core/auth/repository"
	"github.com/orkestra/backend/pkg/sdk/iface"
)

func ptr(t time.Time) *time.Time { return &t }

// memOAuthRepo is an in-memory provider collection with the Mongo
// repository's tombstone semantics: the default lookups see only rows
// without unlinkedAt.
type memOAuthRepo struct {
	repository.OAuthProviderRepository
	mu   sync.Mutex
	docs []*authModels.OAuthProviderDoc
	// failure switches (see the ownership tests)
	createErr       error
	dupOwner        string // next create: the identity is already recorded for this user
	deleteErr       error
	getErr          error
	updateTokensErr error
	deleteCalls     int
}

func newMemOAuthRepo() *memOAuthRepo { return &memOAuthRepo{} }

func (r *memOAuthRepo) find(provider authModels.OAuthProvider, providerID string, includeUnlinked bool) *authModels.OAuthProviderDoc {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, d := range r.docs {
		if d.Provider == provider && d.ProviderID == providerID && (includeUnlinked || d.UnlinkedAt == nil) {
			return d
		}
	}
	return nil
}

func (r *memOAuthRepo) GetByProviderAndID(_ context.Context, p authModels.OAuthProvider, id string) (*authModels.OAuthProviderDoc, error) {
	if r.getErr != nil {
		return nil, r.getErr
	}
	return r.find(p, id, false), nil
}

func (r *memOAuthRepo) GetByProviderAndIDIncludingUnlinked(_ context.Context, p authModels.OAuthProvider, id string) (*authModels.OAuthProviderDoc, error) {
	if r.getErr != nil {
		return nil, r.getErr
	}
	return r.find(p, id, true), nil
}

// DeleteProvider removes one document by its uuid (the compensation and
// orphan-healing write).
func (r *memOAuthRepo) DeleteProvider(ctx context.Context, uuid string) error {
	// A real store honours a cancelled context: the compensation must
	// not run on the request's.
	if ctx.Err() != nil {
		return ctx.Err()
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.deleteCalls++
	if r.deleteErr != nil {
		return r.deleteErr
	}
	kept := r.docs[:0]
	for _, d := range r.docs {
		if d.UUID != uuid {
			kept = append(kept, d)
		}
	}
	r.docs = kept
	return nil
}

func (r *memOAuthRepo) GetByUserUUID(_ context.Context, userUUID string) ([]*authModels.OAuthProviderDoc, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []*authModels.OAuthProviderDoc
	for _, d := range r.docs {
		if d.UserUUID == userUUID && d.UnlinkedAt == nil {
			out = append(out, d)
		}
	}
	return out, nil
}

// CreateOAuthProvider behaves like the Mongo repository under the unique
// (provider, providerId) index: a second row for the same identity is
// ErrOAuthIdentityDuplicate. dupOwner simulates a race lost to another
// writer: the identity is recorded for dupOwner just before this insert.
func (r *memOAuthRepo) CreateOAuthProvider(_ context.Context, d *authModels.OAuthProviderDoc) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.createErr != nil {
		return r.createErr
	}
	if r.dupOwner != "" {
		owner := r.dupOwner
		r.dupOwner = ""
		r.docs = append(r.docs, &authModels.OAuthProviderDoc{
			UUID: "link-raced-" + d.ProviderID, UserUUID: owner, Provider: d.Provider, ProviderID: d.ProviderID,
			Email: d.Email, LinkedAt: time.Now(),
		})
		return fmt.Errorf("%w: E11000 duplicate key", repository.ErrOAuthIdentityDuplicate)
	}
	for _, x := range r.docs {
		if x.Provider == d.Provider && x.ProviderID == d.ProviderID {
			return fmt.Errorf("%w: E11000 duplicate key", repository.ErrOAuthIdentityDuplicate)
		}
	}
	// The per-user (userUuid, provider) unique index.
	for _, x := range r.docs {
		if x.UserUUID == d.UserUUID && x.Provider == d.Provider && x.UnlinkedAt == nil {
			return fmt.Errorf("%w: E11000 duplicate key", repository.ErrOAuthProviderAlreadyLinked)
		}
	}
	if d.UUID == "" {
		d.UUID = authModels.GenerateUUIDv7()
	}
	r.docs = append(r.docs, d)
	return nil
}

func (r *memOAuthRepo) UpdateLastUsed(context.Context, string) error { return nil }
func (r *memOAuthRepo) UpdateOAuthTokens(context.Context, string, string, string, *time.Time, *time.Time, []string) error {
	return r.updateTokensErr
}
func (r *memOAuthRepo) UpdateMetadata(context.Context, string, map[string]interface{}) error {
	return nil
}

type providerDoc struct {
	userUUID, provider, providerID, email string
	isPrimary                             bool
	unlinkedAt                            *time.Time
	createdAt                             time.Time // zero = an hour ago (an old row)
}

// tombstoneFixture pairs the provider store with the user store the
// service reads beside it, the first-admin claimer, and the compensation
// counter the service reports into.
type tombstoneFixture struct {
	repo         *memOAuthRepo
	users        *gateUserFake
	claimer      *gateClaimer
	compFailures int
}

// --- failure switches ---

func (f *tombstoneFixture) duplicateOnNextCreate(owner string) { f.repo.dupOwner = owner }
func (f *tombstoneFixture) failCreateWith(err error)           { f.repo.createErr = err }
func (f *tombstoneFixture) failDeleteProvider() {
	f.repo.deleteErr = errors.New("provider store: delete failed")
}
func (f *tombstoneFixture) failUpdateOAuthTokens() {
	f.repo.updateTokensErr = errors.New("provider store: update failed")
}
func (f *tombstoneFixture) failGetByProviderAndIDWith(err error) { f.repo.getErr = err }
func (f *tombstoneFixture) failCreateUserWith(err error)         { f.users.createFromOAuthAbortErr = err }
func (f *tombstoneFixture) failGetUserByIDWith(err error)        { f.users.setGetByIDErr(err) }
func (f *tombstoneFixture) failGetUserByEmailWith(err error)     { f.users.getByEmailErr = err }

// --- observations ---

func (f *tombstoneFixture) providerDocCount(provider, providerID string) int {
	f.repo.mu.Lock()
	defer f.repo.mu.Unlock()
	n := 0
	for _, d := range f.repo.docs {
		if string(d.Provider) == provider && d.ProviderID == providerID && d.UnlinkedAt == nil {
			n++
		}
	}
	return n
}

func (f *tombstoneFixture) providerDocOwner(provider, providerID string) string {
	if d := f.repo.find(authModels.OAuthProvider(provider), providerID, true); d != nil {
		return d.UserUUID
	}
	return ""
}

func (f *tombstoneFixture) createUserCalls() int           { return f.users.createFromOAuthCalls }
func (f *tombstoneFixture) addOAuthLinkCalls() int         { return f.users.addOAuthLinkCalls }
func (f *tombstoneFixture) claimerCalls() int              { return f.claimer.claimCalls() }
func (f *tombstoneFixture) sentinelReleased() bool         { return len(f.claimer.released) > 0 }
func (f *tombstoneFixture) compensationFailureMetric() int { return f.compFailures }

func (f *tombstoneFixture) seedProvider(t *testing.T, d providerDoc) {
	t.Helper()
	email := d.email
	if email == "" {
		email = fixtureEmail(d.userUUID)
	}
	created := d.createdAt
	if created.IsZero() {
		created = time.Now().Add(-time.Hour)
	}
	_ = f.repo.CreateOAuthProvider(context.Background(), &authModels.OAuthProviderDoc{
		UUID: "link-" + d.provider + "-" + d.providerID, UserUUID: d.userUUID,
		Provider: authModels.OAuthProvider(d.provider), ProviderID: d.providerID, Email: email,
		IsPrimary: d.isPrimary, LinkedAt: created, CreatedAt: created, UnlinkedAt: d.unlinkedAt,
	})
}

// fixtureEmail derives "u1@example.com" from "u-1".
func fixtureEmail(userUUID string) string {
	return strings.ReplaceAll(userUUID, "-", "") + "@example.com"
}

// seedUserWithNoEmbeddedLinks is an account whose provider rows were
// created by LOGIN: nothing in user.oauthLinks, and no password.
func (f *tombstoneFixture) seedUserWithNoEmbeddedLinks(t *testing.T, userUUID string) {
	t.Helper()
	f.seedUserWithEmail(t, userUUID, fixtureEmail(userUUID))
}

func (f *tombstoneFixture) seedUserWithEmail(t *testing.T, userUUID, email string) {
	t.Helper()
	f.users.seed(&iface.User{
		UUID: userUUID, Email: email, FullName: "U", Role: "guest", IsActive: true, EmailVerified: true,
		CreatedAt: time.Now(), UpdatedAt: time.Now(),
	})
}

func (f *tombstoneFixture) userHasEmbeddedLink(userUUID, provider, providerID string) bool {
	u, err := f.users.GetUserByID(context.Background(), userUUID)
	if err != nil || u == nil {
		return false
	}
	for _, l := range u.OAuthLinks {
		if string(l.Provider) == provider && l.ProviderID == providerID {
			return true
		}
	}
	return false
}

func (f *tombstoneFixture) failAddOAuthLink() {
	f.users.addOAuthLinkErr = errors.New("user store: write failed")
}

type callbackCfg struct {
	policy   map[string]string
	audience PolicyAudience
}

type callbackOpt func(*callbackCfg)

func withAutoLink(on bool) callbackOpt {
	return func(c *callbackCfg) {
		if on {
			c.policy["oauthAutoLinkByEmail"] = "true"
		} else {
			c.policy["oauthAutoLinkByEmail"] = "false"
		}
	}
}

func tier(aud PolicyAudience) callbackOpt { return func(c *callbackCfg) { c.audience = aud } }

// newOAuthCallbackService is an operator-tier service over the fixture,
// every provider usable, auto-link OFF unless withAutoLink(true).
func newOAuthCallbackService(t *testing.T, opts ...callbackOpt) (*authService, *tombstoneFixture) {
	t.Helper()
	cfg := &callbackCfg{audience: PolicyAudienceOperator, policy: map[string]string{
		"registrationEnabledAdmin":  "true",
		"registrationEnabledClient": "true",
		"oauthAllowSignupAdmin":     "true",
		"oauthAllowSignupClient":    "true",
		"oauthAutoLinkByEmail":      "false",
		"passwordLoginEnabledAdmin": "true",
	}}
	for _, o := range opts {
		o(cfg)
	}
	repo := newMemOAuthRepo()
	env := buildOAuthEnv(t, cfg.audience, repo, cfg.policy)
	env.auth.SetProviderUsability(func(context.Context, PolicyAudience, iface.OAuthProvider) (bool, error) { return true, nil })
	f := &tombstoneFixture{repo: repo, users: env.users, claimer: env.claimer}
	env.auth.oauthCompensationFailure = func() { f.compFailures++ }
	return env.auth, f
}

func completeOAuthCallback(t *testing.T, svc *authService, provider, providerID, email string) (*authModels.TokenResponse, error) {
	t.Helper()
	return completeOAuthCallbackInfo(t, context.Background(), svc, provider,
		map[string]interface{}{"email": email, "name": "U", "provider_id": providerID, "email_verified": true})
}

func completeOAuthCallbackInfo(t *testing.T, ctx context.Context, svc *authService, provider string, info map[string]interface{}) (*authModels.TokenResponse, error) {
	t.Helper()
	return svc.HandleOAuthCallbackWithLinking(ctx, authModels.OAuthProvider(provider), info,
		nil, &authModels.SecurityContext{IPAddress: "1.1.1.1", Timestamp: time.Now()},
		&authModels.DeviceInfo{Platform: "web"})
}

func hasProvider(v *authModels.AuthMethodsView, provider string) bool {
	for _, p := range v.OAuthProviders {
		if p.Provider == provider {
			return true
		}
	}
	return false
}

func TestCallback_TombstonedIdentityIsRefused(t *testing.T) {
	svc, repo := newOAuthCallbackService(t)
	repo.seedProvider(t, providerDoc{
		userUUID: "u-1", provider: "google", providerID: "1234",
		unlinkedAt: ptr(time.Now().Add(-time.Hour)),
	})

	_, err := completeOAuthCallback(t, svc, "google", "1234", "u1@example.com")
	if !errors.Is(err, ErrOAuthIdentityUnlinked) {
		t.Fatalf("err = %v, want ErrOAuthIdentityUnlinked", err)
	}
}

// The account itself is unaffected (edge case 20) — the user can still
// sign in another way.
func TestCallback_TombstoneDoesNotDisableTheAccount(t *testing.T) {
	svc, repo := newOAuthCallbackService(t)
	repo.seedUserWithNoEmbeddedLinks(t, "u-1")
	repo.seedProvider(t, providerDoc{userUUID: "u-1", provider: "google", providerID: "1234", unlinkedAt: ptr(time.Now())})
	repo.seedProvider(t, providerDoc{userUUID: "u-1", provider: "github", providerID: "9999"})

	if _, err := completeOAuthCallback(t, svc, "github", "9999", "u1@example.com"); err != nil {
		t.Fatalf("another linked identity must still work: %v", err)
	}
}

// A tombstoned identity must NOT fall through to the auto-link branch —
// which matches by verified email and would silently re-create the link
// the operator just removed.
func TestCallback_TombstoneDoesNotFallThroughToAutoLink(t *testing.T) {
	svc, repo := newOAuthCallbackService(t, withAutoLink(true))
	repo.seedProvider(t, providerDoc{userUUID: "u-1", provider: "google", providerID: "1234", unlinkedAt: ptr(time.Now())})
	repo.seedUserWithEmail(t, "u-1", "u1@example.com")

	_, err := completeOAuthCallback(t, svc, "google", "1234", "u1@example.com")
	if !errors.Is(err, ErrOAuthIdentityUnlinked) {
		t.Fatalf("err = %v — a tombstone must not be re-linked by the email branch", err)
	}
	if len(repo.users.createdUsers) != 0 {
		t.Fatal("and must not start a signup either")
	}
}

func TestGetUserAuthMethods_ReadsTheProviderCollection(t *testing.T) {
	svc, repo := newOAuthCallbackService(t)
	// A link created by LOGIN: the provider doc exists, the embedded
	// slice does not. It was invisible to auth-methods.
	repo.seedProvider(t, providerDoc{userUUID: "u-1", provider: "google", providerID: "1234", email: "u1@example.com"})
	repo.seedUserWithNoEmbeddedLinks(t, "u-1")

	methods, err := svc.GetUserAuthMethods(context.Background(), "u-1")
	if err != nil {
		t.Fatalf("GetUserAuthMethods: %v", err)
	}
	if !hasProvider(methods, "google") {
		t.Fatal("a login-created link must be visible in auth-methods")
	}
}

func TestGetUserAuthMethods_ExcludesTombstonedIdentities(t *testing.T) {
	svc, repo := newOAuthCallbackService(t)
	repo.seedUserWithNoEmbeddedLinks(t, "u-1")
	repo.seedProvider(t, providerDoc{userUUID: "u-1", provider: "google", providerID: "1234", unlinkedAt: ptr(time.Now())})

	methods, err := svc.GetUserAuthMethods(context.Background(), "u-1")
	if err != nil {
		t.Fatalf("GetUserAuthMethods: %v", err)
	}
	if hasProvider(methods, "google") {
		t.Fatal("an unlinked identity must not be listed")
	}
}

func TestGetOAuthLinks_ReadsTheProviderCollection(t *testing.T) {
	svc, repo := newOAuthCallbackService(t)
	repo.seedProvider(t, providerDoc{userUUID: "u-1", provider: "google", providerID: "1234"})
	repo.seedUserWithNoEmbeddedLinks(t, "u-1")

	links, err := svc.GetOAuthLinks(context.Background(), "u-1")
	if err != nil {
		t.Fatalf("GetOAuthLinks: %v", err)
	}
	if len(links.Links) != 1 {
		t.Fatalf("got %d links, want 1 — the embedded slice is a derived read-model, not the source", len(links.Links))
	}
}

// The lockout calculation must count what actually EXISTS, or a user
// with a login-created link is told they would be locked out when they
// would not.
func TestWouldLockOut_CountsProviderDocsNotEmbeddedLinks(t *testing.T) {
	svc, repo := newOAuthCallbackService(t)
	repo.seedUserWithNoEmbeddedLinks(t, "u-1") // and no password
	repo.seedProvider(t, providerDoc{userUUID: "u-1", provider: "google", providerID: "1234"})
	repo.seedProvider(t, providerDoc{userUUID: "u-1", provider: "github", providerID: "9999"})

	if err := svc.SelfUnlinkOAuth(context.Background(), "u-1", "google"); err != nil {
		t.Fatalf("unlinking one of two real links must be allowed: %v", err)
	}
}

func TestWouldLockOut_TombstonedLinkDoesNotCountAsAWayIn(t *testing.T) {
	svc, repo := newOAuthCallbackService(t)
	repo.seedUserWithNoEmbeddedLinks(t, "u-1")
	repo.seedProvider(t, providerDoc{userUUID: "u-1", provider: "google", providerID: "1234"})
	repo.seedProvider(t, providerDoc{userUUID: "u-1", provider: "github", providerID: "9999", unlinkedAt: ptr(time.Now())})

	if err := svc.SelfUnlinkOAuth(context.Background(), "u-1", "google"); !errors.Is(err, ErrLastCredentialRemoval) {
		t.Fatalf("err = %v — an unlinked identity is not a remaining way in", err)
	}
}

// Lazy repair: a login through an existing provider doc re-adds the
// embedded link when it is missing. Best-effort — the embedded slice no
// longer decides anything, so a failure is a WARN.
func TestCallback_RepairsTheEmbeddedReadModel(t *testing.T) {
	svc, repo := newOAuthCallbackService(t)
	repo.seedProvider(t, providerDoc{userUUID: "u-1", provider: "google", providerID: "1234"})
	repo.seedUserWithNoEmbeddedLinks(t, "u-1")

	if _, err := completeOAuthCallback(t, svc, "google", "1234", "u1@example.com"); err != nil {
		t.Fatalf("callback: %v", err)
	}
	if !repo.userHasEmbeddedLink("u-1", "google", "1234") {
		t.Fatal("the read-model must be repaired lazily on login")
	}
}

func TestCallback_RepairFailureDoesNotFailTheLogin(t *testing.T) {
	svc, repo := newOAuthCallbackService(t)
	repo.seedProvider(t, providerDoc{userUUID: "u-1", provider: "google", providerID: "1234"})
	repo.seedUserWithNoEmbeddedLinks(t, "u-1")
	repo.failAddOAuthLink()

	if _, err := completeOAuthCallback(t, svc, "google", "1234", "u1@example.com"); err != nil {
		t.Fatalf("the read-model repair is best-effort: %v", err)
	}
}
