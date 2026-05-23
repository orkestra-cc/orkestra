package handlers

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/orkestra/backend/internal/core/user/models"
	"github.com/orkestra/backend/internal/core/user/services"
	"go.mongodb.org/mongo-driver/bson/primitive"
)

// fakeUserService is a hand-rolled stub of services.UserService. Each
// method delegates to a function field on the struct, defaulting to a
// "not implemented" panic so tests have to opt in explicitly. The handler
// layer only consumes a small slice of the interface — every test sets
// the one or two fields it needs and leaves the rest panicking.
type fakeUserService struct {
	createUserFn             func(ctx context.Context, input *models.CreateUserInput) (*models.UserManagementResponse, error)
	getUserFn                func(ctx context.Context, id string) (*models.UserManagementResponse, error)
	getUserByEmailFn         func(ctx context.Context, email string) (*models.UserManagementResponse, error)
	updateUserFn             func(ctx context.Context, id string, input *models.UpdateUserInput) (*models.UserManagementResponse, error)
	deleteUserFn             func(ctx context.Context, id string) error
	listUsersFn              func(ctx context.Context, filters *models.UserFilters, pagination *models.PaginationParams) (*models.UserManagementListResponse, error)
	getUsersByRoleFn         func(ctx context.Context, role string) ([]*models.UserManagementResponse, error)
	getUserCountFn           func(ctx context.Context, filters *models.UserFilters) (int64, error)
	softDeleteAndAliasFn     func(ctx context.Context, id string) error
	createUserWithPasswordFn func(ctx context.Context, input *models.CreateUserInput) (*models.User, error)
	markEmailVerifiedFn      func(ctx context.Context, userUUID string) error

	// last-call snapshots — set by methods that have a function field so
	// tests can assert pass-through (e.g. ListUsers must forward query
	// params verbatim).
	lastListFilters    *models.UserFilters
	lastListPagination *models.PaginationParams
	lastCountFilters   *models.UserFilters
}

func (f *fakeUserService) CreateUser(ctx context.Context, input *models.CreateUserInput) (*models.UserManagementResponse, error) {
	return f.createUserFn(ctx, input)
}
func (f *fakeUserService) GetUser(ctx context.Context, id string) (*models.UserManagementResponse, error) {
	return f.getUserFn(ctx, id)
}
func (f *fakeUserService) GetUserByEmail(ctx context.Context, email string) (*models.UserManagementResponse, error) {
	return f.getUserByEmailFn(ctx, email)
}
func (f *fakeUserService) UpdateUser(ctx context.Context, id string, input *models.UpdateUserInput) (*models.UserManagementResponse, error) {
	return f.updateUserFn(ctx, id, input)
}
func (f *fakeUserService) DeleteUser(ctx context.Context, id string) error {
	return f.deleteUserFn(ctx, id)
}
func (f *fakeUserService) ListUsers(ctx context.Context, filters *models.UserFilters, pagination *models.PaginationParams) (*models.UserManagementListResponse, error) {
	f.lastListFilters = filters
	f.lastListPagination = pagination
	return f.listUsersFn(ctx, filters, pagination)
}
func (f *fakeUserService) GetUsersByRole(ctx context.Context, role string) ([]*models.UserManagementResponse, error) {
	return f.getUsersByRoleFn(ctx, role)
}
func (f *fakeUserService) GetUserCount(ctx context.Context, filters *models.UserFilters) (int64, error) {
	f.lastCountFilters = filters
	return f.getUserCountFn(ctx, filters)
}

// Below: every other UserService method panics. The handler under test
// must not reach them; if it does, the panic is the regression signal.
func (f *fakeUserService) GetUserForAuth(context.Context, string) (*models.User, error) {
	panic("unused: GetUserForAuth")
}
func (f *fakeUserService) CreateUserWithPassword(ctx context.Context, input *models.CreateUserInput) (*models.User, error) {
	if f.createUserWithPasswordFn != nil {
		return f.createUserWithPasswordFn(ctx, input)
	}
	panic("unused: CreateUserWithPassword")
}
func (f *fakeUserService) UpdatePasswordHash(context.Context, string, string) error {
	panic("unused: UpdatePasswordHash")
}
func (f *fakeUserService) MarkEmailVerified(ctx context.Context, userUUID string) error {
	if f.markEmailVerifiedFn != nil {
		return f.markEmailVerifiedFn(ctx, userUUID)
	}
	panic("unused: MarkEmailVerified")
}
func (f *fakeUserService) RecordFailedLogin(context.Context, string, *time.Time) error {
	panic("unused: RecordFailedLogin")
}
func (f *fakeUserService) ClearFailedLogins(context.Context, string) error {
	panic("unused: ClearFailedLogins")
}
func (f *fakeUserService) SoftDeleteAndAliasEmail(ctx context.Context, id string) error {
	if f.softDeleteAndAliasFn != nil {
		return f.softDeleteAndAliasFn(ctx, id)
	}
	panic("unused: SoftDeleteAndAliasEmail")
}
func (f *fakeUserService) ValidateUserRole(context.Context, string, []string) error {
	panic("unused: ValidateUserRole")
}
func (f *fakeUserService) GetUserByID(context.Context, string) (*models.User, error) {
	panic("unused: GetUserByID")
}
func (f *fakeUserService) GetUserByObjectID(context.Context, primitive.ObjectID) (*models.User, error) {
	panic("unused: GetUserByObjectID")
}
func (f *fakeUserService) GetUserByUsername(context.Context, string) (*models.User, error) {
	panic("unused: GetUserByUsername")
}
func (f *fakeUserService) GetUserByOAuthID(context.Context, models.OAuthProvider, string) (*models.User, error) {
	panic("unused: GetUserByOAuthID")
}
func (f *fakeUserService) GetUserByOAuthLink(context.Context, models.OAuthProvider, string) (*models.User, error) {
	panic("unused: GetUserByOAuthLink")
}
func (f *fakeUserService) CreateUserFromOAuth(context.Context, *models.CreateUserInput) (*models.User, error) {
	panic("unused: CreateUserFromOAuth")
}
func (f *fakeUserService) AddOAuthLinkToUser(context.Context, string, models.OAuthLink) error {
	panic("unused: AddOAuthLinkToUser")
}
func (f *fakeUserService) RemoveOAuthLinkFromUser(context.Context, string, models.OAuthProvider, string) error {
	panic("unused: RemoveOAuthLinkFromUser")
}
func (f *fakeUserService) SetPrimaryOAuthLink(context.Context, string, models.OAuthProvider, string) error {
	panic("unused: SetPrimaryOAuthLink")
}
func (f *fakeUserService) UpdateOAuthLinkUsage(context.Context, string, models.OAuthProvider, string) error {
	panic("unused: UpdateOAuthLinkUsage")
}
func (f *fakeUserService) GetUserOAuthLinks(context.Context, string) ([]models.OAuthLink, error) {
	panic("unused: GetUserOAuthLinks")
}
func (f *fakeUserService) UpdateUserLastLogin(context.Context, string) error {
	panic("unused: UpdateUserLastLogin")
}
func (f *fakeUserService) UpdateUserLastLoginByObjectID(context.Context, primitive.ObjectID) error {
	panic("unused: UpdateUserLastLoginByObjectID")
}
func (f *fakeUserService) UpdateUserByObjectID(context.Context, primitive.ObjectID, *models.User) error {
	panic("unused: UpdateUserByObjectID")
}
func (f *fakeUserService) ValidateUserExists(context.Context, string) (bool, error) {
	panic("unused: ValidateUserExists")
}
func (f *fakeUserService) ValidateUserActive(context.Context, string) (bool, error) {
	panic("unused: ValidateUserActive")
}

// assertStatus pulls the status code out of a huma.StatusError. Fails
// the test if err isn't a StatusError or the code doesn't match.
func assertStatus(t *testing.T, err error, want int) {
	t.Helper()
	if err == nil {
		t.Fatalf("expected huma error with status %d, got nil", want)
	}
	var se huma.StatusError
	if !errors.As(err, &se) {
		t.Fatalf("err %v is not a huma.StatusError", err)
	}
	if got := se.GetStatus(); got != want {
		t.Errorf("status = %d, want %d (err: %v)", got, want, err)
	}
}

func TestCreateUserHandler(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name       string
		svcResp    *models.UserManagementResponse
		svcErr     error
		wantStatus int // 0 = success
		wantBodyID string
	}{
		{
			name:       "happy path",
			svcResp:    &models.UserManagementResponse{ID: "u1", Email: "a@b.c"},
			wantBodyID: "u1",
		},
		{name: "duplicate email → 409", svcErr: services.ErrEmailNotUnique, wantStatus: 409},
		{name: "invalid input → 400", svcErr: services.ErrInvalidInput, wantStatus: 400},
		{name: "unknown error → 500", svcErr: errors.New("boom"), wantStatus: 500},
	}
	for _, c := range cases {
		c := c
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			svc := &fakeUserService{
				createUserFn: func(context.Context, *models.CreateUserInput) (*models.UserManagementResponse, error) {
					return c.svcResp, c.svcErr
				},
			}
			h := NewUserHandler(svc)
			resp, err := h.CreateUser(context.Background(), &CreateUserRequest{
				Body: models.CreateUserInput{Email: "x@y.z", FullName: "x", Role: "operator"},
			})
			if c.wantStatus == 0 {
				if err != nil {
					t.Fatalf("err = %v, want nil", err)
				}
				if resp.Body.ID != c.wantBodyID {
					t.Errorf("body.ID = %q, want %q", resp.Body.ID, c.wantBodyID)
				}
				return
			}
			assertStatus(t, err, c.wantStatus)
		})
	}
}

func TestGetUserHandler(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name       string
		svcResp    *models.UserManagementResponse
		svcErr     error
		wantStatus int
	}{
		{name: "happy", svcResp: &models.UserManagementResponse{ID: "u1"}},
		{name: "not found → 404", svcErr: services.ErrUserNotFound, wantStatus: 404},
		{name: "invalid id → 400", svcErr: services.ErrInvalidInput, wantStatus: 400},
		{name: "unknown → 500", svcErr: errors.New("boom"), wantStatus: 500},
	}
	for _, c := range cases {
		c := c
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			svc := &fakeUserService{
				getUserFn: func(context.Context, string) (*models.UserManagementResponse, error) {
					return c.svcResp, c.svcErr
				},
			}
			h := NewUserHandler(svc)
			_, err := h.GetUser(context.Background(), &GetUserRequest{ID: "u1"})
			if c.wantStatus == 0 {
				if err != nil {
					t.Fatalf("err = %v", err)
				}
				return
			}
			assertStatus(t, err, c.wantStatus)
		})
	}
}

func TestUpdateUserHandler(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name       string
		svcErr     error
		wantStatus int
	}{
		{name: "happy"},
		{name: "not found → 404", svcErr: services.ErrUserNotFound, wantStatus: 404},
		{name: "email taken → 409", svcErr: services.ErrEmailNotUnique, wantStatus: 409},
		{name: "invalid → 400", svcErr: services.ErrInvalidInput, wantStatus: 400},
		{name: "unknown → 500", svcErr: errors.New("boom"), wantStatus: 500},
	}
	for _, c := range cases {
		c := c
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			svc := &fakeUserService{
				updateUserFn: func(context.Context, string, *models.UpdateUserInput) (*models.UserManagementResponse, error) {
					if c.svcErr != nil {
						return nil, c.svcErr
					}
					return &models.UserManagementResponse{ID: "u1", FullName: "Renamed"}, nil
				},
			}
			h := NewUserHandler(svc)
			resp, err := h.UpdateUser(context.Background(), &UpdateUserRequest{
				ID:   "u1",
				Body: models.UpdateUserInput{FullName: "Renamed"},
			})
			if c.wantStatus == 0 {
				if err != nil {
					t.Fatalf("err = %v", err)
				}
				if resp.Body.FullName != "Renamed" {
					t.Errorf("body.FullName = %q, want Renamed", resp.Body.FullName)
				}
				return
			}
			assertStatus(t, err, c.wantStatus)
		})
	}
}

func TestDeleteUserHandler(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name       string
		svcErr     error
		wantStatus int
	}{
		{name: "happy"},
		{name: "not found → 404", svcErr: services.ErrUserNotFound, wantStatus: 404},
		{name: "invalid → 400", svcErr: services.ErrInvalidInput, wantStatus: 400},
		{name: "unknown → 500", svcErr: errors.New("boom"), wantStatus: 500},
	}
	for _, c := range cases {
		c := c
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			svc := &fakeUserService{
				deleteUserFn: func(context.Context, string) error { return c.svcErr },
			}
			h := NewUserHandler(svc)
			resp, err := h.DeleteUser(context.Background(), &DeleteUserRequest{ID: "u1"})
			if c.wantStatus == 0 {
				if err != nil {
					t.Fatalf("err = %v", err)
				}
				if resp.Body.Message != "User deleted successfully" {
					t.Errorf("message = %q", resp.Body.Message)
				}
				return
			}
			assertStatus(t, err, c.wantStatus)
		})
	}
}

func TestListUsersHandler(t *testing.T) {
	t.Parallel()

	t.Run("forwards filters and pagination", func(t *testing.T) {
		t.Parallel()
		svc := &fakeUserService{
			listUsersFn: func(_ context.Context, _ *models.UserFilters, _ *models.PaginationParams) (*models.UserManagementListResponse, error) {
				return &models.UserManagementListResponse{
					Users: []models.UserManagementResponse{{ID: "u1"}},
					Total: 1, Page: 2, PageSize: 25, TotalPages: 1,
				}, nil
			},
		}
		h := NewUserHandler(svc)
		resp, err := h.ListUsers(context.Background(), &ListUsersRequest{
			Role: "administrator", IsActive: true, EmailVerified: true,
			Search: "ali", Page: 2, PageSize: 25,
		})
		if err != nil {
			t.Fatalf("err = %v", err)
		}
		if svc.lastListFilters.Role != "administrator" {
			t.Errorf("role not forwarded: %+v", svc.lastListFilters)
		}
		if svc.lastListFilters.IsActive == nil || !*svc.lastListFilters.IsActive {
			t.Errorf("IsActive not promoted to pointer: %+v", svc.lastListFilters.IsActive)
		}
		if svc.lastListFilters.EmailVerified == nil || !*svc.lastListFilters.EmailVerified {
			t.Errorf("EmailVerified not promoted to pointer: %+v", svc.lastListFilters.EmailVerified)
		}
		if svc.lastListFilters.Search != "ali" {
			t.Errorf("search not forwarded: %q", svc.lastListFilters.Search)
		}
		if svc.lastListPagination.Page != 2 || svc.lastListPagination.PageSize != 25 {
			t.Errorf("pagination not forwarded: %+v", svc.lastListPagination)
		}
		if resp.Body.Total != 1 {
			t.Errorf("body.Total = %d", resp.Body.Total)
		}
	})

	t.Run("optional booleans default to unset when false", func(t *testing.T) {
		t.Parallel()
		// The handler treats `false` as "not provided" so we leave both
		// pointers nil rather than setting them to false explicitly.
		// This is a known quirk of the current shape — the test pins
		// the behaviour so a future refactor that drops the workaround
		// is intentional.
		svc := &fakeUserService{
			listUsersFn: func(context.Context, *models.UserFilters, *models.PaginationParams) (*models.UserManagementListResponse, error) {
				return &models.UserManagementListResponse{}, nil
			},
		}
		h := NewUserHandler(svc)
		_, err := h.ListUsers(context.Background(), &ListUsersRequest{
			Role: "operator", IsActive: false, EmailVerified: false,
		})
		if err != nil {
			t.Fatalf("err = %v", err)
		}
		if svc.lastListFilters.IsActive != nil {
			t.Errorf("IsActive should be nil when query was false, got %+v", svc.lastListFilters.IsActive)
		}
		if svc.lastListFilters.EmailVerified != nil {
			t.Errorf("EmailVerified should be nil when query was false, got %+v", svc.lastListFilters.EmailVerified)
		}
	})

	t.Run("svc error → 500", func(t *testing.T) {
		t.Parallel()
		svc := &fakeUserService{
			listUsersFn: func(context.Context, *models.UserFilters, *models.PaginationParams) (*models.UserManagementListResponse, error) {
				return nil, errors.New("boom")
			},
		}
		h := NewUserHandler(svc)
		_, err := h.ListUsers(context.Background(), &ListUsersRequest{Page: 1, PageSize: 10})
		assertStatus(t, err, 500)
	})
}

func TestGetUsersByRoleHandler(t *testing.T) {
	t.Parallel()

	t.Run("happy", func(t *testing.T) {
		t.Parallel()
		svc := &fakeUserService{
			getUsersByRoleFn: func(context.Context, string) ([]*models.UserManagementResponse, error) {
				return []*models.UserManagementResponse{{ID: "u1"}, {ID: "u2"}}, nil
			},
		}
		h := NewUserHandler(svc)
		resp, err := h.GetUsersByRole(context.Background(), &GetUsersByRoleRequest{Role: "operator"})
		if err != nil {
			t.Fatalf("err = %v", err)
		}
		if len(resp.Body.Users) != 2 || resp.Body.Total != 2 {
			t.Errorf("users=%d total=%d, want 2/2", len(resp.Body.Users), resp.Body.Total)
		}
	})

	t.Run("invalid → 400", func(t *testing.T) {
		t.Parallel()
		svc := &fakeUserService{
			getUsersByRoleFn: func(context.Context, string) ([]*models.UserManagementResponse, error) {
				return nil, services.ErrInvalidInput
			},
		}
		h := NewUserHandler(svc)
		_, err := h.GetUsersByRole(context.Background(), &GetUsersByRoleRequest{Role: ""})
		assertStatus(t, err, 400)
	})

	t.Run("unknown error → 500", func(t *testing.T) {
		t.Parallel()
		svc := &fakeUserService{
			getUsersByRoleFn: func(context.Context, string) ([]*models.UserManagementResponse, error) {
				return nil, errors.New("boom")
			},
		}
		h := NewUserHandler(svc)
		_, err := h.GetUsersByRole(context.Background(), &GetUsersByRoleRequest{Role: "operator"})
		assertStatus(t, err, 500)
	})
}

func TestGetUserByEmailHandler(t *testing.T) {
	t.Parallel()

	t.Run("rejects empty query without hitting service", func(t *testing.T) {
		t.Parallel()
		// The fake panics on GetUserByEmail because the field is nil —
		// so the test asserts the handler does the empty-string check
		// _before_ calling the service.
		svc := &fakeUserService{}
		h := NewUserHandler(svc)
		_, err := h.GetUserByEmail(context.Background(), &GetUserByEmailRequest{Email: ""})
		assertStatus(t, err, 400)
	})

	cases := []struct {
		name       string
		svcResp    *models.UserManagementResponse
		svcErr     error
		wantStatus int
	}{
		{name: "happy", svcResp: &models.UserManagementResponse{ID: "u1"}},
		{name: "not found → 404", svcErr: services.ErrUserNotFound, wantStatus: 404},
		{name: "invalid → 400", svcErr: services.ErrInvalidInput, wantStatus: 400},
		{name: "unknown → 500", svcErr: errors.New("boom"), wantStatus: 500},
	}
	for _, c := range cases {
		c := c
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			svc := &fakeUserService{
				getUserByEmailFn: func(context.Context, string) (*models.UserManagementResponse, error) {
					return c.svcResp, c.svcErr
				},
			}
			h := NewUserHandler(svc)
			_, err := h.GetUserByEmail(context.Background(), &GetUserByEmailRequest{Email: "a@b.c"})
			if c.wantStatus == 0 {
				if err != nil {
					t.Fatalf("err = %v", err)
				}
				return
			}
			assertStatus(t, err, c.wantStatus)
		})
	}
}

func TestGetUserCountHandler(t *testing.T) {
	t.Parallel()

	t.Run("forwards filters", func(t *testing.T) {
		t.Parallel()
		svc := &fakeUserService{
			getUserCountFn: func(context.Context, *models.UserFilters) (int64, error) { return 42, nil },
		}
		h := NewUserHandler(svc)
		resp, err := h.GetUserCount(context.Background(), &GetUserCountRequest{
			Role: "operator", IsActive: true, EmailVerified: true, Search: "x",
		})
		if err != nil {
			t.Fatalf("err = %v", err)
		}
		if resp.Body.Count != 42 {
			t.Errorf("count = %d, want 42", resp.Body.Count)
		}
		if svc.lastCountFilters.Role != "operator" || svc.lastCountFilters.Search != "x" {
			t.Errorf("filters not forwarded: %+v", svc.lastCountFilters)
		}
		if svc.lastCountFilters.IsActive == nil || !*svc.lastCountFilters.IsActive {
			t.Errorf("IsActive not promoted")
		}
		if svc.lastCountFilters.EmailVerified == nil || !*svc.lastCountFilters.EmailVerified {
			t.Errorf("EmailVerified not promoted")
		}
	})

	t.Run("svc error → 500", func(t *testing.T) {
		t.Parallel()
		svc := &fakeUserService{
			getUserCountFn: func(context.Context, *models.UserFilters) (int64, error) {
				return 0, errors.New("boom")
			},
		}
		h := NewUserHandler(svc)
		_, err := h.GetUserCount(context.Background(), &GetUserCountRequest{})
		assertStatus(t, err, 500)
	})
}
