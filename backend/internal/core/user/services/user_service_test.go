package services

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	authModels "github.com/orkestra/backend/internal/core/auth/models"
	"github.com/orkestra/backend/internal/core/user/models"
	"github.com/orkestra/backend/internal/core/user/repository"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// fakeUserRepo is an in-memory implementation of repository.UserRepository
// used by every service-level test in this file. It tracks exactly the
// state the userService inspects: a map of uuid → *User, with helpers to
// seed and query call counts so tests can assert behaviour without a Mongo
// round trip.
//
// Unused methods return zero values rather than panicking — the service
// is broad and only a small slice is exercised per test, so a panic-on-
// unused approach would force every test to seed unrelated state.
type fakeUserRepo struct {
	mu           sync.Mutex
	users        map[string]*models.User // keyed by UUID
	createErr    error
	getErr       error
	listErr      error
	countCalls   int
	updateCalled bool
	// softAliased tracks calls to SoftDeleteAndAliasEmail so the
	// idempotent-on-NotFound test can verify the call reached the repo.
	softAliased []string
}

func newFakeUserRepo() *fakeUserRepo {
	return &fakeUserRepo{users: map[string]*models.User{}}
}

func (r *fakeUserRepo) seed(u *models.User) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.users[u.UUID] = u
}

func (r *fakeUserRepo) Create(_ context.Context, user *models.User) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.createErr != nil {
		return r.createErr
	}
	for _, u := range r.users {
		if u.Email == user.Email {
			return repository.ErrUserAlreadyExists
		}
	}
	if user.UUID == "" {
		user.UUID = "fake-uuid-" + user.Email
	}
	r.users[user.UUID] = user
	return nil
}

func (r *fakeUserRepo) GetByID(_ context.Context, id string) (*models.User, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.getErr != nil {
		return nil, r.getErr
	}
	if u, ok := r.users[id]; ok {
		return u, nil
	}
	return nil, repository.ErrUserNotFound
}

func (r *fakeUserRepo) GetByObjectID(_ context.Context, _ primitive.ObjectID) (*models.User, error) {
	return nil, repository.ErrUserNotFound
}

func (r *fakeUserRepo) GetByEmail(_ context.Context, email string) (*models.User, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, u := range r.users {
		if u.Email == email {
			return u, nil
		}
	}
	return nil, repository.ErrUserNotFound
}

func (r *fakeUserRepo) GetByUsername(_ context.Context, username string) (*models.User, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, u := range r.users {
		if u.Username == username {
			return u, nil
		}
	}
	return nil, repository.ErrUserNotFound
}

func (r *fakeUserRepo) Update(_ context.Context, id string, input *models.UpdateUserInput) (*models.User, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.updateCalled = true
	u, ok := r.users[id]
	if !ok {
		return nil, repository.ErrUserNotFound
	}
	if input.Email != "" {
		u.Email = input.Email
	}
	if input.Username != "" {
		u.Username = input.Username
	}
	if input.FullName != "" {
		u.FullName = input.FullName
	}
	if input.Avatar != "" {
		u.Avatar = input.Avatar
	}
	if input.Phone != "" {
		u.Phone = input.Phone
	}
	if input.Role != "" {
		u.Role = input.Role
	}
	if input.IsActive != nil {
		u.IsActive = *input.IsActive
	}
	return u, nil
}

func (r *fakeUserRepo) UpdateByObjectID(_ context.Context, _ primitive.ObjectID, _ *models.User) error {
	return nil
}

func (r *fakeUserRepo) UpdateLastLogin(_ context.Context, _ string) error { return nil }
func (r *fakeUserRepo) UpdateLastLoginByObjectID(_ context.Context, _ primitive.ObjectID) error {
	return nil
}
func (r *fakeUserRepo) Delete(_ context.Context, id string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.users[id]; !ok {
		return repository.ErrUserNotFound
	}
	delete(r.users, id)
	return nil
}
func (r *fakeUserRepo) DeleteByObjectID(_ context.Context, _ primitive.ObjectID) error { return nil }
func (r *fakeUserRepo) HardDelete(_ context.Context, _ string) error                   { return nil }

func (r *fakeUserRepo) SoftDeleteAndAliasEmail(_ context.Context, id string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.softAliased = append(r.softAliased, id)
	if _, ok := r.users[id]; !ok {
		return repository.ErrUserNotFound
	}
	delete(r.users, id)
	return nil
}

func (r *fakeUserRepo) UpdatePasswordHash(_ context.Context, _, _ string) error           { return nil }
func (r *fakeUserRepo) MarkEmailVerified(_ context.Context, _ string) error               { return nil }
func (r *fakeUserRepo) RecordFailedLogin(_ context.Context, _ string, _ *time.Time) error { return nil }
func (r *fakeUserRepo) ClearFailedLogins(_ context.Context, _ string) error               { return nil }
func (r *fakeUserRepo) SetMFAGraceStartedAt(_ context.Context, id string, when time.Time) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if u, ok := r.users[id]; ok {
		u.MFAGraceStartedAt = &when
	}
	return nil
}
func (r *fakeUserRepo) ClearMFAGraceStartedAt(_ context.Context, id string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if u, ok := r.users[id]; ok {
		u.MFAGraceStartedAt = nil
	}
	return nil
}

func (r *fakeUserRepo) GetByOAuthID(_ context.Context, _ models.OAuthProvider, _ string) (*models.User, error) {
	return nil, repository.ErrUserNotFound
}
func (r *fakeUserRepo) GetByOAuthLink(_ context.Context, _ models.OAuthProvider, _ string) (*models.User, error) {
	return nil, repository.ErrUserNotFound
}
func (r *fakeUserRepo) AddOAuthLink(_ context.Context, userUUID string, link models.OAuthLink) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if u, ok := r.users[userUUID]; ok {
		u.OAuthLinks = append(u.OAuthLinks, link)
	}
	return nil
}
func (r *fakeUserRepo) RemoveOAuthLink(_ context.Context, _ string, _ models.OAuthProvider, _ string) error {
	return nil
}
func (r *fakeUserRepo) SetPrimaryOAuthLink(_ context.Context, _ string, _ models.OAuthProvider, _ string) error {
	return nil
}
func (r *fakeUserRepo) GetOAuthLinks(_ context.Context, userUUID string) ([]models.OAuthLink, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if u, ok := r.users[userUUID]; ok {
		out := make([]models.OAuthLink, len(u.OAuthLinks))
		copy(out, u.OAuthLinks)
		return out, nil
	}
	return nil, repository.ErrUserNotFound
}
func (r *fakeUserRepo) UpdateOAuthLinkUsage(_ context.Context, _ string, _ models.OAuthProvider, _ string) error {
	return nil
}

func (r *fakeUserRepo) List(_ context.Context, _ *models.UserFilters, p *models.PaginationParams) ([]*models.User, int64, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.listErr != nil {
		return nil, 0, r.listErr
	}
	out := make([]*models.User, 0, len(r.users))
	for _, u := range r.users {
		out = append(out, u)
	}
	total := int64(len(out))
	// Crude pagination: respect PageSize so tests can assert TotalPages math.
	if p != nil && p.PageSize > 0 && len(out) > p.PageSize {
		out = out[:p.PageSize]
	}
	return out, total, nil
}

func (r *fakeUserRepo) ListWithOptions(_ context.Context, _ bson.M, _ ...*options.FindOptions) ([]*models.User, error) {
	return nil, nil
}
func (r *fakeUserRepo) GetByRole(_ context.Context, role string) ([]*models.User, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []*models.User
	for _, u := range r.users {
		if u.Role == role {
			out = append(out, u)
		}
	}
	return out, nil
}

func (r *fakeUserRepo) Count(_ context.Context, _ *models.UserFilters) (int64, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.countCalls++
	return int64(len(r.users)), nil
}
func (r *fakeUserRepo) CountWithFilter(_ context.Context, _ bson.M) (int64, error) { return 0, nil }
func (r *fakeUserRepo) ExistsByEmail(_ context.Context, email string) (bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, u := range r.users {
		if u.Email == email {
			return true, nil
		}
	}
	return false, nil
}
func (r *fakeUserRepo) ExistsByUUID(_ context.Context, id string) (bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	_, ok := r.users[id]
	return ok, nil
}
func (r *fakeUserRepo) ExistsByUsername(_ context.Context, _ string) (bool, error) { return false, nil }
func (r *fakeUserRepo) BackfillDefaultLanguage(_ context.Context, _ string) (int64, error) {
	return 0, nil
}

// fakeOAuthProviderRepo implements authRepository.OAuthProviderRepository
// with the minimum surface needed by the userService's enrichment path —
// GetByUserUUID returns whatever was seeded. Every mutation method is a
// no-op so unrelated tests don't have to construct one.
type fakeOAuthProviderRepo struct {
	mu   sync.Mutex
	docs map[string][]*authModels.OAuthProviderDoc // userUUID → providers
	err  error
}

func newFakeOAuthProviderRepo() *fakeOAuthProviderRepo {
	return &fakeOAuthProviderRepo{docs: map[string][]*authModels.OAuthProviderDoc{}}
}

func (r *fakeOAuthProviderRepo) seed(userUUID string, docs ...*authModels.OAuthProviderDoc) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.docs[userUUID] = append(r.docs[userUUID], docs...)
}

func (r *fakeOAuthProviderRepo) GetByUserUUID(_ context.Context, userUUID string) ([]*authModels.OAuthProviderDoc, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.err != nil {
		return nil, r.err
	}
	return r.docs[userUUID], nil
}

func (r *fakeOAuthProviderRepo) CreateOAuthProvider(context.Context, *authModels.OAuthProviderDoc) error {
	return nil
}
func (r *fakeOAuthProviderRepo) LinkOAuthProvider(context.Context, string, *authModels.OAuthLink) error {
	return nil
}
func (r *fakeOAuthProviderRepo) GetByProviderAndID(context.Context, authModels.OAuthProvider, string) (*authModels.OAuthProviderDoc, error) {
	return nil, nil
}
func (r *fakeOAuthProviderRepo) GetPrimaryProvider(context.Context, string) (*authModels.OAuthProviderDoc, error) {
	return nil, nil
}
func (r *fakeOAuthProviderRepo) UpdateLastUsed(context.Context, string) error { return nil }
func (r *fakeOAuthProviderRepo) SetPrimaryProvider(context.Context, string, authModels.OAuthProvider) error {
	return nil
}
func (r *fakeOAuthProviderRepo) UpdateRefreshToken(context.Context, string, string) error {
	return nil
}
func (r *fakeOAuthProviderRepo) UpdateOAuthTokens(context.Context, string, string, string, *time.Time, *time.Time, []string) error {
	return nil
}
func (r *fakeOAuthProviderRepo) UnlinkProvider(context.Context, string, authModels.OAuthProvider) error {
	return nil
}
func (r *fakeOAuthProviderRepo) DeleteProvider(context.Context, string) error { return nil }
func (r *fakeOAuthProviderRepo) FindByEmail(context.Context, string) ([]*authModels.OAuthProviderDoc, error) {
	return nil, nil
}
func (r *fakeOAuthProviderRepo) ConsolidateProviders(context.Context, string, string) error {
	return nil
}

// newSvcForTest is the single constructor every test uses. Returns the
// concrete *userService (not the interface) so tests can also reach the
// in-memory repo via the returned fakes when they need to assert on
// post-call state.
func newSvcForTest(t *testing.T) (*userService, *fakeUserRepo, *fakeOAuthProviderRepo) {
	t.Helper()
	users := newFakeUserRepo()
	oauth := newFakeOAuthProviderRepo()
	svc := &userService{userRepo: users, oauthProviderRepo: oauth}
	return svc, users, oauth
}

func TestCreateUser_RejectsNilInput(t *testing.T) {
	t.Parallel()
	svc, _, _ := newSvcForTest(t)
	_, err := svc.CreateUser(context.Background(), nil)
	if !errors.Is(err, ErrInvalidInput) {
		t.Errorf("err = %v, want ErrInvalidInput", err)
	}
}

func TestCreateUser_RejectsMissingFields(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name  string
		input *models.CreateUserInput
	}{
		{"missing email", &models.CreateUserInput{FullName: "x", Role: "operator"}},
		{"missing fullname", &models.CreateUserInput{Email: "a@b.c", Role: "operator"}},
		{"missing role", &models.CreateUserInput{Email: "a@b.c", FullName: "x"}},
	}
	for _, c := range cases {
		c := c
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			svc, _, _ := newSvcForTest(t)
			_, err := svc.CreateUser(context.Background(), c.input)
			if !errors.Is(err, ErrInvalidInput) {
				t.Errorf("err = %v, want ErrInvalidInput", err)
			}
		})
	}
}

func TestCreateUser_NormalizesEmail(t *testing.T) {
	t.Parallel()
	svc, users, _ := newSvcForTest(t)
	resp, err := svc.CreateUser(context.Background(), &models.CreateUserInput{
		Email:    "  USER@Example.COM  ",
		FullName: "User",
		Role:     "operator",
	})
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	if resp.Email != "user@example.com" {
		t.Errorf("response email = %q, want lowercased + trimmed", resp.Email)
	}
	// repo should also see the normalized form
	got, err := users.GetByEmail(context.Background(), "user@example.com")
	if err != nil {
		t.Fatalf("seeded user not findable by normalized email: %v", err)
	}
	if got.Email != "user@example.com" {
		t.Errorf("repo stored email = %q, want normalized", got.Email)
	}
}

func TestCreateUser_RejectsDuplicateEmail(t *testing.T) {
	t.Parallel()
	svc, users, _ := newSvcForTest(t)
	users.seed(&models.User{UUID: "u1", Email: "dup@example.com", Role: "operator"})
	_, err := svc.CreateUser(context.Background(), &models.CreateUserInput{
		Email: "dup@example.com", FullName: "x", Role: "operator",
	})
	if !errors.Is(err, ErrEmailNotUnique) {
		t.Errorf("err = %v, want ErrEmailNotUnique", err)
	}
}

func TestCreateUser_MapsRepoDuplicateError(t *testing.T) {
	t.Parallel()
	// A race in which ExistsByEmail returns false but Create fails on
	// the unique-index collision must still surface ErrEmailNotUnique.
	svc, users, _ := newSvcForTest(t)
	users.createErr = repository.ErrUserAlreadyExists
	_, err := svc.CreateUser(context.Background(), &models.CreateUserInput{
		Email: "race@example.com", FullName: "x", Role: "operator",
	})
	if !errors.Is(err, ErrEmailNotUnique) {
		t.Errorf("err = %v, want ErrEmailNotUnique", err)
	}
}

func TestGetUser_RejectsEmptyID(t *testing.T) {
	t.Parallel()
	svc, _, _ := newSvcForTest(t)
	_, err := svc.GetUser(context.Background(), "")
	if !errors.Is(err, ErrInvalidInput) {
		t.Errorf("err = %v, want ErrInvalidInput", err)
	}
}

func TestGetUser_MapsNotFound(t *testing.T) {
	t.Parallel()
	svc, _, _ := newSvcForTest(t)
	_, err := svc.GetUser(context.Background(), "missing")
	if !errors.Is(err, ErrUserNotFound) {
		t.Errorf("err = %v, want ErrUserNotFound", err)
	}
}

func TestGetUser_ReturnsResponseWithProviders(t *testing.T) {
	t.Parallel()
	svc, users, oauth := newSvcForTest(t)
	users.seed(&models.User{UUID: "u1", Email: "a@b.c", Role: "operator"})
	oauth.seed("u1", &authModels.OAuthProviderDoc{
		UUID:     "p1",
		UserUUID: "u1",
		Provider: authModels.OAuthProviderGoogle,
		Email:    "a@b.c",
		Metadata: map[string]interface{}{"picture": "https://cdn/u1.png"},
	})
	resp, err := svc.GetUser(context.Background(), "u1")
	if err != nil {
		t.Fatalf("GetUser: %v", err)
	}
	if len(resp.Providers) != 1 || resp.Providers[0].Provider != "google" {
		t.Errorf("Providers = %+v, want one google entry", resp.Providers)
	}
	if resp.Providers[0].Avatar != "https://cdn/u1.png" {
		t.Errorf("avatar from metadata not propagated; got %q", resp.Providers[0].Avatar)
	}
}

func TestGetUserByEmail_NormalizesAndMapsNotFound(t *testing.T) {
	t.Parallel()
	svc, users, _ := newSvcForTest(t)
	users.seed(&models.User{UUID: "u1", Email: "a@b.c", Role: "operator"})

	resp, err := svc.GetUserByEmail(context.Background(), "  A@B.C  ")
	if err != nil {
		t.Fatalf("GetUserByEmail: %v", err)
	}
	if resp.ID != "u1" {
		t.Errorf("ID = %q, want u1", resp.ID)
	}

	if _, err := svc.GetUserByEmail(context.Background(), ""); !errors.Is(err, ErrInvalidInput) {
		t.Errorf("empty email err = %v, want ErrInvalidInput", err)
	}
	if _, err := svc.GetUserByEmail(context.Background(), "missing@x.com"); !errors.Is(err, ErrUserNotFound) {
		t.Errorf("missing email err = %v, want ErrUserNotFound", err)
	}
}

func TestUpdateUser_RejectsBadInputs(t *testing.T) {
	t.Parallel()
	svc, _, _ := newSvcForTest(t)
	if _, err := svc.UpdateUser(context.Background(), "", &models.UpdateUserInput{}); !errors.Is(err, ErrInvalidInput) {
		t.Errorf("empty id err = %v", err)
	}
	if _, err := svc.UpdateUser(context.Background(), "id", nil); !errors.Is(err, ErrInvalidInput) {
		t.Errorf("nil input err = %v", err)
	}
}

func TestUpdateUser_DetectsEmailCollision(t *testing.T) {
	t.Parallel()
	svc, users, _ := newSvcForTest(t)
	users.seed(&models.User{UUID: "u1", Email: "a@b.c", Role: "operator"})
	users.seed(&models.User{UUID: "u2", Email: "x@y.z", Role: "operator"})
	_, err := svc.UpdateUser(context.Background(), "u1", &models.UpdateUserInput{Email: "x@y.z"})
	if !errors.Is(err, ErrEmailNotUnique) {
		t.Errorf("err = %v, want ErrEmailNotUnique (taken by u2)", err)
	}
}

func TestUpdateUser_AllowsKeepingSameEmail(t *testing.T) {
	t.Parallel()
	svc, users, _ := newSvcForTest(t)
	users.seed(&models.User{UUID: "u1", Email: "a@b.c", Role: "operator"})
	resp, err := svc.UpdateUser(context.Background(), "u1", &models.UpdateUserInput{Email: "a@b.c", FullName: "Renamed"})
	if err != nil {
		t.Fatalf("UpdateUser: %v", err)
	}
	if resp.FullName != "Renamed" {
		t.Errorf("FullName = %q, want Renamed", resp.FullName)
	}
}

func TestUpdateUser_MapsNotFound(t *testing.T) {
	t.Parallel()
	svc, _, _ := newSvcForTest(t)
	_, err := svc.UpdateUser(context.Background(), "missing", &models.UpdateUserInput{FullName: "x"})
	if !errors.Is(err, ErrUserNotFound) {
		t.Errorf("err = %v, want ErrUserNotFound", err)
	}
}

func TestDeleteUser_ValidatesAndMapsNotFound(t *testing.T) {
	t.Parallel()
	svc, users, _ := newSvcForTest(t)
	if err := svc.DeleteUser(context.Background(), ""); !errors.Is(err, ErrInvalidInput) {
		t.Errorf("empty id err = %v", err)
	}
	if err := svc.DeleteUser(context.Background(), "missing"); !errors.Is(err, ErrUserNotFound) {
		t.Errorf("missing err = %v", err)
	}
	users.seed(&models.User{UUID: "u1", Email: "a@b.c", Role: "operator"})
	if err := svc.DeleteUser(context.Background(), "u1"); err != nil {
		t.Errorf("Delete: %v", err)
	}
}

func TestSoftDeleteAndAliasEmail_IdempotentOnNotFound(t *testing.T) {
	t.Parallel()
	svc, users, _ := newSvcForTest(t)
	// Missing row → success (idempotent, per service-doc comment).
	if err := svc.SoftDeleteAndAliasEmail(context.Background(), "missing"); err != nil {
		t.Errorf("missing should be a no-op success, got %v", err)
	}
	if len(users.softAliased) != 1 || users.softAliased[0] != "missing" {
		t.Errorf("expected one call to repo with 'missing', got %+v", users.softAliased)
	}
	// Empty id is still ErrInvalidInput.
	if err := svc.SoftDeleteAndAliasEmail(context.Background(), ""); !errors.Is(err, ErrInvalidInput) {
		t.Errorf("empty id err = %v", err)
	}
}

func TestListUsers_PaginationDefaultsAndClamps(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name         string
		input        *models.PaginationParams
		wantPage     int
		wantPageSize int
	}{
		{"nil → defaults", nil, 1, 10},
		{"page=0 → 1", &models.PaginationParams{Page: 0, PageSize: 5}, 1, 5},
		{"pageSize=0 → 10", &models.PaginationParams{Page: 2, PageSize: 0}, 2, 10},
		{"pageSize>100 → 10", &models.PaginationParams{Page: 1, PageSize: 9999}, 1, 10},
	}
	for _, c := range cases {
		c := c
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			svc, _, _ := newSvcForTest(t)
			out, err := svc.ListUsers(context.Background(), nil, c.input)
			if err != nil {
				t.Fatalf("ListUsers: %v", err)
			}
			if out.Page != c.wantPage {
				t.Errorf("Page = %d, want %d", out.Page, c.wantPage)
			}
			if out.PageSize != c.wantPageSize {
				t.Errorf("PageSize = %d, want %d", out.PageSize, c.wantPageSize)
			}
		})
	}
}

func TestListUsers_TotalPagesMath(t *testing.T) {
	t.Parallel()
	svc, users, _ := newSvcForTest(t)
	for i := 0; i < 25; i++ {
		users.seed(&models.User{UUID: "u" + string(rune('a'+i)), Email: string(rune('a'+i)) + "@x.com", Role: "operator"})
	}
	out, err := svc.ListUsers(context.Background(), nil, &models.PaginationParams{Page: 1, PageSize: 10})
	if err != nil {
		t.Fatalf("ListUsers: %v", err)
	}
	if out.Total != 25 {
		t.Errorf("Total = %d, want 25", out.Total)
	}
	if out.TotalPages != 3 { // 25 / 10 = 2 remainder 5 → 3 pages
		t.Errorf("TotalPages = %d, want 3", out.TotalPages)
	}
}

func TestGetUsersByRole_ValidatesAndFilters(t *testing.T) {
	t.Parallel()
	svc, users, _ := newSvcForTest(t)
	if _, err := svc.GetUsersByRole(context.Background(), ""); !errors.Is(err, ErrInvalidInput) {
		t.Errorf("empty role err = %v", err)
	}
	users.seed(&models.User{UUID: "u1", Email: "a@b.c", Role: "operator"})
	users.seed(&models.User{UUID: "u2", Email: "x@y.z", Role: "administrator"})
	got, err := svc.GetUsersByRole(context.Background(), "administrator")
	if err != nil {
		t.Fatalf("GetUsersByRole: %v", err)
	}
	if len(got) != 1 || got[0].ID != "u2" {
		t.Errorf("results = %+v, want exactly u2", got)
	}
}

func TestValidateUserRole(t *testing.T) {
	t.Parallel()
	svc, users, _ := newSvcForTest(t)
	users.seed(&models.User{UUID: "u1", Role: "developer"})
	if err := svc.ValidateUserRole(context.Background(), "u1", []string{"developer", "operator"}); err != nil {
		t.Errorf("matching role err = %v, want nil", err)
	}
	if err := svc.ValidateUserRole(context.Background(), "u1", []string{"administrator"}); !errors.Is(err, ErrUnauthorized) {
		t.Errorf("non-matching role err = %v, want ErrUnauthorized", err)
	}
	if err := svc.ValidateUserRole(context.Background(), "missing", []string{"any"}); !errors.Is(err, repository.ErrUserNotFound) {
		t.Errorf("missing user err = %v, want repo ErrUserNotFound", err)
	}
}

func TestGetUserCount_PassesThrough(t *testing.T) {
	t.Parallel()
	svc, users, _ := newSvcForTest(t)
	users.seed(&models.User{UUID: "u1", Email: "a@b.c", Role: "operator"})
	users.seed(&models.User{UUID: "u2", Email: "x@y.z", Role: "operator"})
	n, err := svc.GetUserCount(context.Background(), nil)
	if err != nil {
		t.Fatalf("GetUserCount: %v", err)
	}
	if n != 2 {
		t.Errorf("count = %d, want 2", n)
	}
	if users.countCalls != 1 {
		t.Errorf("repo.Count called %d times, want 1", users.countCalls)
	}
}

func TestCreateUserWithPassword(t *testing.T) {
	t.Parallel()
	t.Run("rejects nil", func(t *testing.T) {
		t.Parallel()
		svc, _, _ := newSvcForTest(t)
		_, err := svc.CreateUserWithPassword(context.Background(), nil)
		if !errors.Is(err, ErrInvalidInput) {
			t.Errorf("err = %v", err)
		}
	})
	t.Run("rejects missing role", func(t *testing.T) {
		t.Parallel()
		svc, _, _ := newSvcForTest(t)
		_, err := svc.CreateUserWithPassword(context.Background(), &models.CreateUserInput{Email: "a@b.c", FullName: "x"})
		if !errors.Is(err, ErrInvalidInput) {
			t.Errorf("err = %v", err)
		}
	})
	t.Run("rejects duplicate email", func(t *testing.T) {
		t.Parallel()
		svc, users, _ := newSvcForTest(t)
		users.seed(&models.User{UUID: "u1", Email: "dup@x.com"})
		_, err := svc.CreateUserWithPassword(context.Background(), &models.CreateUserInput{
			Email: "dup@x.com", FullName: "x", Role: "operator", PasswordHash: "h",
		})
		if !errors.Is(err, ErrEmailNotUnique) {
			t.Errorf("err = %v", err)
		}
	})
	t.Run("uses caller-supplied UUID and stamps PasswordUpdatedAt", func(t *testing.T) {
		t.Parallel()
		svc, _, _ := newSvcForTest(t)
		u, err := svc.CreateUserWithPassword(context.Background(), &models.CreateUserInput{
			UUID:         "preminted-uuid",
			Email:        "new@x.com",
			FullName:     "New",
			Role:         "super_admin",
			PasswordHash: "argon2id$h",
		})
		if err != nil {
			t.Fatalf("CreateUserWithPassword: %v", err)
		}
		if u.UUID != "preminted-uuid" {
			t.Errorf("UUID = %q, want preminted-uuid", u.UUID)
		}
		if u.PasswordUpdatedAt == nil {
			t.Errorf("PasswordUpdatedAt not stamped")
		}
		if u.PasswordHash != "argon2id$h" {
			t.Errorf("PasswordHash not propagated")
		}
	})
}

func TestCreateUserFromOAuth_AddsLink(t *testing.T) {
	t.Parallel()
	svc, _, _ := newSvcForTest(t)
	u, err := svc.CreateUserFromOAuth(context.Background(), &models.CreateUserInput{
		Email:         "oauth@x.com",
		FullName:      "OAuth User",
		Role:          "operator",
		OAuthProvider: models.OAuthProviderGoogle,
		OAuthID:       "g-1",
	})
	if err != nil {
		t.Fatalf("CreateUserFromOAuth: %v", err)
	}
	if len(u.OAuthLinks) != 1 {
		t.Fatalf("expected one OAuthLink, got %d", len(u.OAuthLinks))
	}
	link := u.OAuthLinks[0]
	if link.Provider != models.OAuthProviderGoogle || link.ProviderID != "g-1" {
		t.Errorf("link = %+v, want google/g-1", link)
	}
	if !link.IsPrimary {
		t.Errorf("first link should be primary")
	}
}

func TestCreateUserFromOAuth_RejectsBadInputs(t *testing.T) {
	t.Parallel()
	svc, _, _ := newSvcForTest(t)
	cases := []*models.CreateUserInput{
		nil,
		{FullName: "x", Role: "operator"},  // missing email
		{Email: "a@b.c", Role: "operator"}, // missing fullname
		{Email: "a@b.c", FullName: "x"},    // missing role
	}
	for i, c := range cases {
		if _, err := svc.CreateUserFromOAuth(context.Background(), c); !errors.Is(err, ErrInvalidInput) {
			t.Errorf("case %d: err = %v, want ErrInvalidInput", i, err)
		}
	}
}

func TestGetUserForAuth(t *testing.T) {
	t.Parallel()
	svc, users, _ := newSvcForTest(t)
	users.seed(&models.User{UUID: "u1", Email: "a@b.c", Role: "operator", PasswordHash: "secret"})

	t.Run("returns raw user including hash", func(t *testing.T) {
		t.Parallel()
		u, err := svc.GetUserForAuth(context.Background(), "  A@B.C  ")
		if err != nil {
			t.Fatalf("GetUserForAuth: %v", err)
		}
		if u.PasswordHash != "secret" {
			t.Errorf("PasswordHash not exposed for auth flow")
		}
	})
	t.Run("rejects empty email", func(t *testing.T) {
		t.Parallel()
		if _, err := svc.GetUserForAuth(context.Background(), ""); !errors.Is(err, ErrInvalidInput) {
			t.Errorf("err = %v", err)
		}
	})
	t.Run("maps not found", func(t *testing.T) {
		t.Parallel()
		if _, err := svc.GetUserForAuth(context.Background(), "missing@x.com"); !errors.Is(err, ErrUserNotFound) {
			t.Errorf("err = %v", err)
		}
	})
}

func TestEmptyInputGuards(t *testing.T) {
	t.Parallel()
	svc, _, _ := newSvcForTest(t)
	ctx := context.Background()

	checks := []struct {
		name string
		fn   func() error
	}{
		{"GetUserByID", func() error { _, err := svc.GetUserByID(ctx, ""); return err }},
		{"GetUserByUsername", func() error { _, err := svc.GetUserByUsername(ctx, ""); return err }},
		{"GetUserByOAuthID", func() error { _, err := svc.GetUserByOAuthID(ctx, models.OAuthProviderGoogle, ""); return err }},
		{"GetUserByOAuthLink", func() error { _, err := svc.GetUserByOAuthLink(ctx, models.OAuthProviderGoogle, ""); return err }},
		{"AddOAuthLinkToUser", func() error { return svc.AddOAuthLinkToUser(ctx, "", models.OAuthLink{}) }},
		{"RemoveOAuthLinkFromUser empty user", func() error { return svc.RemoveOAuthLinkFromUser(ctx, "", models.OAuthProviderGoogle, "x") }},
		{"RemoveOAuthLinkFromUser empty providerID", func() error { return svc.RemoveOAuthLinkFromUser(ctx, "u", models.OAuthProviderGoogle, "") }},
		{"SetPrimaryOAuthLink empty providerID", func() error { return svc.SetPrimaryOAuthLink(ctx, "u", models.OAuthProviderGoogle, "") }},
		{"UpdateOAuthLinkUsage empty user", func() error { return svc.UpdateOAuthLinkUsage(ctx, "", models.OAuthProviderGoogle, "x") }},
		{"GetUserOAuthLinks", func() error { _, err := svc.GetUserOAuthLinks(ctx, ""); return err }},
		{"UpdateUserLastLogin", func() error { return svc.UpdateUserLastLogin(ctx, "") }},
		{"UpdatePasswordHash empty user", func() error { return svc.UpdatePasswordHash(ctx, "", "h") }},
		{"UpdatePasswordHash empty hash", func() error { return svc.UpdatePasswordHash(ctx, "u", "") }},
		{"MarkEmailVerified", func() error { return svc.MarkEmailVerified(ctx, "") }},
		{"RecordFailedLogin", func() error { return svc.RecordFailedLogin(ctx, "", nil) }},
		{"ClearFailedLogins", func() error { return svc.ClearFailedLogins(ctx, "") }},
		{"StartMFAGraceIfUnset", func() error { return svc.StartMFAGraceIfUnset(ctx, "") }},
		{"ResetMFAGrace", func() error { return svc.ResetMFAGrace(ctx, "") }},
		{"ClearMFAGrace", func() error { return svc.ClearMFAGrace(ctx, "") }},
		{"ValidateUserExists", func() error { _, err := svc.ValidateUserExists(ctx, ""); return err }},
		{"ValidateUserActive", func() error { _, err := svc.ValidateUserActive(ctx, ""); return err }},
		{"UpdateUserByObjectID nil update", func() error { return svc.UpdateUserByObjectID(ctx, primitive.NewObjectID(), nil) }},
	}
	for _, c := range checks {
		c := c
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			if err := c.fn(); !errors.Is(err, ErrInvalidInput) {
				t.Errorf("%s err = %v, want ErrInvalidInput", c.name, err)
			}
		})
	}
}

func TestStartMFAGraceIfUnset_PreservesExisting(t *testing.T) {
	t.Parallel()
	svc, users, _ := newSvcForTest(t)
	existing := time.Now().Add(-2 * 24 * time.Hour)
	users.seed(&models.User{UUID: "u1", MFAGraceStartedAt: &existing})

	if err := svc.StartMFAGraceIfUnset(context.Background(), "u1"); err != nil {
		t.Fatalf("StartMFAGraceIfUnset: %v", err)
	}
	got, _ := users.GetByID(context.Background(), "u1")
	if got.MFAGraceStartedAt == nil || !got.MFAGraceStartedAt.Equal(existing) {
		t.Errorf("existing grace stamp was overwritten; before=%v after=%v", existing, got.MFAGraceStartedAt)
	}
}

func TestStartMFAGraceIfUnset_StampsWhenUnset(t *testing.T) {
	t.Parallel()
	svc, users, _ := newSvcForTest(t)
	users.seed(&models.User{UUID: "u1"})

	before := time.Now().Add(-time.Second)
	if err := svc.StartMFAGraceIfUnset(context.Background(), "u1"); err != nil {
		t.Fatalf("StartMFAGraceIfUnset: %v", err)
	}
	got, _ := users.GetByID(context.Background(), "u1")
	if got.MFAGraceStartedAt == nil || got.MFAGraceStartedAt.Before(before) {
		t.Errorf("grace stamp not set to ~now; got %v", got.MFAGraceStartedAt)
	}
}

func TestValidateUserExistsAndActive(t *testing.T) {
	t.Parallel()
	svc, users, _ := newSvcForTest(t)
	users.seed(&models.User{UUID: "active", IsActive: true})
	users.seed(&models.User{UUID: "inactive", IsActive: false})

	t.Run("ValidateUserExists hit", func(t *testing.T) {
		t.Parallel()
		ok, err := svc.ValidateUserExists(context.Background(), "active")
		if err != nil || !ok {
			t.Errorf("ok=%v err=%v, want true/nil", ok, err)
		}
	})
	t.Run("ValidateUserActive returns IsActive", func(t *testing.T) {
		t.Parallel()
		ok, err := svc.ValidateUserActive(context.Background(), "inactive")
		if err != nil {
			t.Fatalf("err = %v", err)
		}
		if ok {
			t.Errorf("IsActive = true for inactive user")
		}
	})
	t.Run("ValidateUserActive missing user returns (false, nil)", func(t *testing.T) {
		t.Parallel()
		ok, err := svc.ValidateUserActive(context.Background(), "ghost")
		if err != nil || ok {
			t.Errorf("missing user: ok=%v err=%v, want false/nil", ok, err)
		}
	})
}
