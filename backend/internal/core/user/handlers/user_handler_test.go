package handlers

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/orkestra/backend/internal/core/user/services"
	"github.com/orkestra/backend/internal/shared/errcode"
	"github.com/orkestra/backend/pkg/sdk/ctxauth"
	"github.com/orkestra/backend/pkg/sdk/iface"
	"go.mongodb.org/mongo-driver/bson/primitive"
)

// fakeUserService is a hand-rolled stub of services.UserService. Each
// method delegates to a function field on the struct, defaulting to a
// "not implemented" panic so tests have to opt in explicitly. The handler
// layer only consumes a small slice of the interface — every test sets
// the one or two fields it needs and leaves the rest panicking.
type fakeUserService struct {
	createUserFn             func(ctx context.Context, input *iface.CreateUserInput) (*iface.UserManagementResponse, error)
	getUserFn                func(ctx context.Context, id string) (*iface.UserManagementResponse, error)
	getUserByEmailFn         func(ctx context.Context, email string) (*iface.UserManagementResponse, error)
	updateUserFn             func(ctx context.Context, id string, input *iface.UpdateUserInput) (*iface.UserManagementResponse, error)
	deleteUserFn             func(ctx context.Context, id string) error
	listUsersFn              func(ctx context.Context, filters *iface.UserFilters, pagination *iface.PaginationParams) (*iface.UserManagementListResponse, error)
	getUsersByRoleFn         func(ctx context.Context, role string) ([]*iface.UserManagementResponse, error)
	getUserCountFn           func(ctx context.Context, filters *iface.UserFilters) (int64, error)
	softDeleteAndAliasFn     func(ctx context.Context, id string) error
	createUserWithPasswordFn func(ctx context.Context, input *iface.CreateUserInput) (*iface.User, error)
	markEmailVerifiedFn      func(ctx context.Context, userUUID string) error
	countActiveAdminsFn      func(ctx context.Context, excludeUUID string) (int64, error)

	// last-call snapshots — set by methods that have a function field so
	// tests can assert pass-through (e.g. ListUsers must forward query
	// params verbatim).
	lastListFilters       *iface.UserFilters
	lastListPagination    *iface.PaginationParams
	lastCountFilters      *iface.UserFilters
	lastSoftDeleteAliasID string
	lastDeleteID          string

	// sink is the optional audit sink returned by AuditSink(). Tests
	// that exercise the audit-emit path attach a captureSink here.
	sink iface.AuditSink
}

func (f *fakeUserService) CreateUser(ctx context.Context, input *iface.CreateUserInput) (*iface.UserManagementResponse, error) {
	return f.createUserFn(ctx, input)
}
func (f *fakeUserService) GetUser(ctx context.Context, id string) (*iface.UserManagementResponse, error) {
	// Nil-tolerant: handlers now call GetUser for pre-state snapshots
	// (UpdateUser delta detection, DeleteUser last-admin check). Tests
	// that don't care about the snapshot can leave getUserFn unset and
	// get a clean (nil, nil) — the handler treats a missing snapshot
	// as "no delta to emit / quorum check short-circuits" without
	// failing the request. Tests that DO care about the snapshot set
	// getUserFn explicitly.
	if f.getUserFn == nil {
		return nil, nil
	}
	return f.getUserFn(ctx, id)
}
func (f *fakeUserService) GetUserByEmail(ctx context.Context, email string) (*iface.UserManagementResponse, error) {
	return f.getUserByEmailFn(ctx, email)
}
func (f *fakeUserService) UpdateUser(ctx context.Context, id string, input *iface.UpdateUserInput) (*iface.UserManagementResponse, error) {
	return f.updateUserFn(ctx, id, input)
}
func (f *fakeUserService) DeleteUser(ctx context.Context, id string) error {
	f.lastDeleteID = id
	if f.deleteUserFn != nil {
		return f.deleteUserFn(ctx, id)
	}
	panic("unused: DeleteUser")
}
func (f *fakeUserService) CountActiveAdministrators(ctx context.Context, excludeUUID string) (int64, error) {
	if f.countActiveAdminsFn != nil {
		return f.countActiveAdminsFn(ctx, excludeUUID)
	}
	panic("unused: CountActiveAdministrators")
}
func (f *fakeUserService) ListUsers(ctx context.Context, filters *iface.UserFilters, pagination *iface.PaginationParams) (*iface.UserManagementListResponse, error) {
	f.lastListFilters = filters
	f.lastListPagination = pagination
	return f.listUsersFn(ctx, filters, pagination)
}
func (f *fakeUserService) GetUsersByRole(ctx context.Context, role string) ([]*iface.UserManagementResponse, error) {
	return f.getUsersByRoleFn(ctx, role)
}
func (f *fakeUserService) GetUserCount(ctx context.Context, filters *iface.UserFilters) (int64, error) {
	f.lastCountFilters = filters
	return f.getUserCountFn(ctx, filters)
}

// Below: every other UserService method panics. The handler under test
// must not reach them; if it does, the panic is the regression signal.
func (f *fakeUserService) GetUserForAuth(context.Context, string) (*iface.User, error) {
	panic("unused: GetUserForAuth")
}
func (f *fakeUserService) CreateUserWithPassword(ctx context.Context, input *iface.CreateUserInput) (*iface.User, error) {
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
	f.lastSoftDeleteAliasID = id
	if f.softDeleteAndAliasFn != nil {
		return f.softDeleteAndAliasFn(ctx, id)
	}
	panic("unused: SoftDeleteAndAliasEmail")
}
func (f *fakeUserService) ValidateUserRole(context.Context, string, []string) error {
	panic("unused: ValidateUserRole")
}
func (f *fakeUserService) GetUserByID(context.Context, string) (*iface.User, error) {
	panic("unused: GetUserByID")
}
func (f *fakeUserService) GetUserByObjectID(context.Context, primitive.ObjectID) (*iface.User, error) {
	panic("unused: GetUserByObjectID")
}
func (f *fakeUserService) GetUserByUsername(context.Context, string) (*iface.User, error) {
	panic("unused: GetUserByUsername")
}
func (f *fakeUserService) GetUserByOAuthID(context.Context, iface.OAuthProvider, string) (*iface.User, error) {
	panic("unused: GetUserByOAuthID")
}
func (f *fakeUserService) GetUserByOAuthLink(context.Context, iface.OAuthProvider, string) (*iface.User, error) {
	panic("unused: GetUserByOAuthLink")
}
func (f *fakeUserService) CreateUserFromOAuth(context.Context, *iface.CreateUserInput) (*iface.User, error) {
	panic("unused: CreateUserFromOAuth")
}
func (f *fakeUserService) AddOAuthLinkToUser(context.Context, string, iface.OAuthLink) error {
	panic("unused: AddOAuthLinkToUser")
}
func (f *fakeUserService) RemoveOAuthLinkFromUser(context.Context, string, iface.OAuthProvider, string) error {
	panic("unused: RemoveOAuthLinkFromUser")
}
func (f *fakeUserService) SetPrimaryOAuthLink(context.Context, string, iface.OAuthProvider, string) error {
	panic("unused: SetPrimaryOAuthLink")
}
func (f *fakeUserService) UpdateOAuthLinkUsage(context.Context, string, iface.OAuthProvider, string) error {
	panic("unused: UpdateOAuthLinkUsage")
}
func (f *fakeUserService) GetUserOAuthLinks(context.Context, string) ([]iface.OAuthLink, error) {
	panic("unused: GetUserOAuthLinks")
}
func (f *fakeUserService) UpdateUserLastLogin(context.Context, string) error {
	panic("unused: UpdateUserLastLogin")
}
func (f *fakeUserService) UpdateUserLastLoginByObjectID(context.Context, primitive.ObjectID) error {
	panic("unused: UpdateUserLastLoginByObjectID")
}
func (f *fakeUserService) UpdateUserByObjectID(context.Context, primitive.ObjectID, *iface.User) error {
	panic("unused: UpdateUserByObjectID")
}
func (f *fakeUserService) ValidateUserExists(context.Context, string) (bool, error) {
	panic("unused: ValidateUserExists")
}
func (f *fakeUserService) ValidateUserActive(context.Context, string) (bool, error) {
	panic("unused: ValidateUserActive")
}
func (f *fakeUserService) SetAvatarSource(context.Context, string, string, string) (string, error) {
	panic("unused: SetAvatarSource")
}
func (f *fakeUserService) UpdateOAuthLinkData(context.Context, string, iface.OAuthProvider, string, map[string]interface{}) error {
	panic("unused: UpdateOAuthLinkData")
}

// AuditSink implements the narrow auditEmitter capability the handlers
// type-assert against. Returning the embedded sink (or nil) opts the
// fake into audit emission per-test — tests that want to assert the
// emit pattern construct a captureSink, attach it here, and inspect
// captureSink.events after the handler call.
func (f *fakeUserService) AuditSink() iface.AuditSink {
	if f.sink == nil {
		return nil
	}
	return f.sink
}

// captureSink is a minimal iface.AuditSink that buffers every emitted
// event in memory for test assertions. Goroutine-safe enough for the
// sequential handler tests; if a future test fans out concurrent calls
// it should wrap Emit in a mutex.
type captureSink struct {
	events []iface.AuditEvent
}

func (c *captureSink) Emit(_ context.Context, event iface.AuditEvent) {
	c.events = append(c.events, event)
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

// adminCtx stamps a super_admin caller onto the context so the
// role-escalation guard in CreateUser / UpdateUser doesn't gate test
// cases that aren't exercising the guard itself. Mirror of what the
// real AuthMiddleware does on every request.
func adminCtx() context.Context {
	ctx := context.WithValue(context.Background(), ctxauth.KeyUserUUID, "test-admin")
	return context.WithValue(ctx, ctxauth.KeySystemRole, "super_admin")
}

func TestCreateUserHandler(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name       string
		svcResp    *iface.UserManagementResponse
		svcErr     error
		wantStatus int // 0 = success
		wantBodyID string
	}{
		{
			name:       "happy path",
			svcResp:    &iface.UserManagementResponse{ID: "u1", Email: "a@b.c"},
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
				createUserFn: func(context.Context, *iface.CreateUserInput) (*iface.UserManagementResponse, error) {
					return c.svcResp, c.svcErr
				},
			}
			h := NewUserHandler(svc)
			resp, err := h.CreateUser(adminCtx(), &CreateUserRequest{
				Body: iface.CreateUserInput{Email: "x@y.z", FullName: "x", Role: "operator"},
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
		svcResp    *iface.UserManagementResponse
		svcErr     error
		wantStatus int
	}{
		{name: "happy", svcResp: &iface.UserManagementResponse{ID: "u1"}},
		{name: "not found → 404", svcErr: services.ErrUserNotFound, wantStatus: 404},
		{name: "invalid id → 400", svcErr: services.ErrInvalidInput, wantStatus: 400},
		{name: "unknown → 500", svcErr: errors.New("boom"), wantStatus: 500},
	}
	for _, c := range cases {
		c := c
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			svc := &fakeUserService{
				getUserFn: func(context.Context, string) (*iface.UserManagementResponse, error) {
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
				updateUserFn: func(context.Context, string, *iface.UpdateUserInput) (*iface.UserManagementResponse, error) {
					if c.svcErr != nil {
						return nil, c.svcErr
					}
					return &iface.UserManagementResponse{ID: "u1", FullName: "Renamed"}, nil
				},
			}
			h := NewUserHandler(svc)
			resp, err := h.UpdateUser(adminCtx(), &UpdateUserRequest{
				ID:   "u1",
				Body: iface.UpdateUserInput{FullName: "Renamed"},
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

	// boolPtr returns a pointer to b. Helps construct UpdateUserInput
	// patches that need to distinguish "not provided" (nil) from
	// "explicitly false".
	boolPtr := func(b bool) *bool { return &b }

	t.Run("deactivating the last active admin refused", func(t *testing.T) {
		t.Parallel()
		svc := &fakeUserService{
			getUserFn: func(context.Context, string) (*iface.UserManagementResponse, error) {
				return &iface.UserManagementResponse{ID: "u1", Role: "administrator", IsActive: true}, nil
			},
			countActiveAdminsFn: func(context.Context, string) (int64, error) { return 0, nil },
		}
		h := NewUserHandler(svc)
		_, err := h.UpdateUser(adminCtx(), &UpdateUserRequest{
			ID:   "u1",
			Body: iface.UpdateUserInput{IsActive: boolPtr(false)},
		})
		assertStatus(t, err, 403)
		assertErrCode(t, err, errcode.UserLastAdminForbidden)
	})

	t.Run("demoting the last active admin refused", func(t *testing.T) {
		t.Parallel()
		svc := &fakeUserService{
			getUserFn: func(context.Context, string) (*iface.UserManagementResponse, error) {
				return &iface.UserManagementResponse{ID: "u1", Role: "super_admin", IsActive: true}, nil
			},
			countActiveAdminsFn: func(context.Context, string) (int64, error) { return 0, nil },
		}
		h := NewUserHandler(svc)
		// Caller is super_admin so the role-escalation guard accepts the
		// demote → the last-admin check is the gate under test.
		ctx := context.WithValue(context.Background(), ctxauth.KeySystemRole, "super_admin")
		_, err := h.UpdateUser(ctx, &UpdateUserRequest{
			ID:   "u1",
			Body: iface.UpdateUserInput{Role: "operator"},
		})
		assertStatus(t, err, 403)
		assertErrCode(t, err, errcode.UserLastAdminForbidden)
	})

	t.Run("benign profile patch skips the guard", func(t *testing.T) {
		t.Parallel()
		// A rename does not flip isActive and does not demote a role, so
		// the handler must NOT consult the admin counter. The panicking
		// countActiveAdminsFn fallback proves it.
		svc := &fakeUserService{
			updateUserFn: func(context.Context, string, *iface.UpdateUserInput) (*iface.UserManagementResponse, error) {
				return &iface.UserManagementResponse{ID: "u1", FullName: "Renamed"}, nil
			},
			// countActiveAdminsFn is nil — would panic if called.
		}
		h := NewUserHandler(svc)
		if _, err := h.UpdateUser(adminCtx(), &UpdateUserRequest{
			ID:   "u1",
			Body: iface.UpdateUserInput{FullName: "Renamed"},
		}); err != nil {
			t.Fatalf("err = %v", err)
		}
	})

	t.Run("promoting to administrator skips the guard", func(t *testing.T) {
		t.Parallel()
		svc := &fakeUserService{
			getUserFn: func(context.Context, string) (*iface.UserManagementResponse, error) {
				return &iface.UserManagementResponse{ID: "u1", Kind: "", Role: "guest", IsActive: true}, nil
			},
			updateUserFn: func(context.Context, string, *iface.UpdateUserInput) (*iface.UserManagementResponse, error) {
				return &iface.UserManagementResponse{ID: "u1", Role: "administrator"}, nil
			},
		}
		h := NewUserHandler(svc)
		// adminCtx() = super_admin caller, so the role-escalation guard
		// accepts the promote to administrator (5 >= 4) and the
		// last-admin path isn't triggered (no demotion intent).
		if _, err := h.UpdateUser(adminCtx(), &UpdateUserRequest{
			ID:   "u1",
			Body: iface.UpdateUserInput{Role: "administrator"},
		}); err != nil {
			t.Fatalf("err = %v", err)
		}
	})

	// --- role-escalation guard, the dedicated path ---

	t.Run("administrator cannot promote anyone to super_admin", func(t *testing.T) {
		t.Parallel()
		// No updateUserFn — handler must short-circuit BEFORE the
		// service call. If it reaches the panicking fallback, the
		// guard fired too late.
		svc := &fakeUserService{}
		h := NewUserHandler(svc)
		ctx := context.WithValue(context.Background(), ctxauth.KeyUserUUID, "admin-1")
		ctx = context.WithValue(ctx, ctxauth.KeySystemRole, "administrator")
		_, err := h.UpdateUser(ctx, &UpdateUserRequest{
			ID:   "u2",
			Body: iface.UpdateUserInput{Role: "super_admin"},
		})
		assertStatus(t, err, 403)
		assertErrCode(t, err, errcode.UserRoleEscalationForbidden)
	})

	t.Run("administrator can assign another administrator (equal tier)", func(t *testing.T) {
		t.Parallel()
		svc := &fakeUserService{
			getUserFn: func(context.Context, string) (*iface.UserManagementResponse, error) {
				return &iface.UserManagementResponse{ID: "u2", Role: "operator", IsActive: true}, nil
			},
			updateUserFn: func(context.Context, string, *iface.UpdateUserInput) (*iface.UserManagementResponse, error) {
				return &iface.UserManagementResponse{ID: "u2", Role: "administrator"}, nil
			},
		}
		h := NewUserHandler(svc)
		ctx := context.WithValue(context.Background(), ctxauth.KeyUserUUID, "admin-1")
		ctx = context.WithValue(ctx, ctxauth.KeySystemRole, "administrator")
		if _, err := h.UpdateUser(ctx, &UpdateUserRequest{
			ID:   "u2",
			Body: iface.UpdateUserInput{Role: "administrator"},
		}); err != nil {
			t.Fatalf("err = %v", err)
		}
	})

	t.Run("manager cannot promote to administrator", func(t *testing.T) {
		t.Parallel()
		svc := &fakeUserService{}
		h := NewUserHandler(svc)
		ctx := context.WithValue(context.Background(), ctxauth.KeyUserUUID, "mgr-1")
		ctx = context.WithValue(ctx, ctxauth.KeySystemRole, "manager")
		_, err := h.UpdateUser(ctx, &UpdateUserRequest{
			ID:   "u2",
			Body: iface.UpdateUserInput{Role: "administrator"},
		})
		assertStatus(t, err, 403)
		assertErrCode(t, err, errcode.UserRoleEscalationForbidden)
	})

	t.Run("missing caller role refuses every role assignment", func(t *testing.T) {
		t.Parallel()
		// Defensive fail-closed: a degraded middleware that forgets to
		// stamp KeySystemRole must NOT let the request through. The
		// canAssignRole tier ladder treats -1 (unknown) as below every
		// real tier.
		svc := &fakeUserService{}
		h := NewUserHandler(svc)
		_, err := h.UpdateUser(context.Background(), &UpdateUserRequest{
			ID:   "u2",
			Body: iface.UpdateUserInput{Role: "guest"},
		})
		assertStatus(t, err, 403)
		assertErrCode(t, err, errcode.UserRoleEscalationForbidden)
	})

	t.Run("profile-only patch with no role bypasses the role guard", func(t *testing.T) {
		t.Parallel()
		// Empty Role means the guard short-circuits — even a missing
		// caller role isn't a problem when there's nothing to assign.
		svc := &fakeUserService{
			updateUserFn: func(context.Context, string, *iface.UpdateUserInput) (*iface.UserManagementResponse, error) {
				return &iface.UserManagementResponse{ID: "u2", FullName: "Renamed"}, nil
			},
		}
		h := NewUserHandler(svc)
		if _, err := h.UpdateUser(context.Background(), &UpdateUserRequest{
			ID:   "u2",
			Body: iface.UpdateUserInput{FullName: "Renamed"},
		}); err != nil {
			t.Fatalf("err = %v", err)
		}
	})
}

func TestUpdateUserHandlerServiceAccountGuard(t *testing.T) {
	t.Parallel()

	// --- service-account privileged-role guard ---

	t.Run("service account cannot be assigned administrator role", func(t *testing.T) {
		t.Parallel()
		// No updateUserFn — handler must short-circuit BEFORE the service
		// call. If it reaches the panicking fallback, the guard fired too late.
		svc := &fakeUserService{
			getUserFn: func(context.Context, string) (*iface.UserManagementResponse, error) {
				return &iface.UserManagementResponse{ID: "svc1", Kind: iface.UserKindService, Role: "guest", IsActive: true}, nil
			},
		}
		h := NewUserHandler(svc)
		ctx := context.WithValue(context.Background(), ctxauth.KeyUserUUID, "admin-1")
		ctx = context.WithValue(ctx, ctxauth.KeySystemRole, "super_admin")
		_, err := h.UpdateUser(ctx, &UpdateUserRequest{
			ID:   "svc1",
			Body: iface.UpdateUserInput{Role: "administrator"},
		})
		assertStatus(t, err, 403)
		assertErrCode(t, err, errcode.UserRoleEscalationForbidden)
	})

	t.Run("service account cannot be assigned super_admin role", func(t *testing.T) {
		t.Parallel()
		svc := &fakeUserService{
			getUserFn: func(context.Context, string) (*iface.UserManagementResponse, error) {
				return &iface.UserManagementResponse{ID: "svc2", Kind: iface.UserKindService, Role: "guest", IsActive: true}, nil
			},
		}
		h := NewUserHandler(svc)
		ctx := context.WithValue(context.Background(), ctxauth.KeyUserUUID, "admin-1")
		ctx = context.WithValue(ctx, ctxauth.KeySystemRole, "super_admin")
		_, err := h.UpdateUser(ctx, &UpdateUserRequest{
			ID:   "svc2",
			Body: iface.UpdateUserInput{Role: "super_admin"},
		})
		assertStatus(t, err, 403)
		assertErrCode(t, err, errcode.UserRoleEscalationForbidden)
	})

	t.Run("service account can be assigned non-privileged role (guest)", func(t *testing.T) {
		t.Parallel()
		svc := &fakeUserService{
			getUserFn: func(context.Context, string) (*iface.UserManagementResponse, error) {
				return &iface.UserManagementResponse{ID: "svc3", Kind: iface.UserKindService, Role: "operator", IsActive: true}, nil
			},
			updateUserFn: func(context.Context, string, *iface.UpdateUserInput) (*iface.UserManagementResponse, error) {
				return &iface.UserManagementResponse{ID: "svc3", Kind: iface.UserKindService, Role: "guest"}, nil
			},
		}
		h := NewUserHandler(svc)
		ctx := context.WithValue(context.Background(), ctxauth.KeyUserUUID, "admin-1")
		ctx = context.WithValue(ctx, ctxauth.KeySystemRole, "super_admin")
		if _, err := h.UpdateUser(ctx, &UpdateUserRequest{
			ID:   "svc3",
			Body: iface.UpdateUserInput{Role: "guest"},
		}); err != nil {
			t.Fatalf("err = %v", err)
		}
	})

	t.Run("human can still be assigned administrator role (no regression)", func(t *testing.T) {
		t.Parallel()
		svc := &fakeUserService{
			getUserFn: func(context.Context, string) (*iface.UserManagementResponse, error) {
				return &iface.UserManagementResponse{ID: "u1", Kind: "", Role: "guest", IsActive: true}, nil
			},
			updateUserFn: func(context.Context, string, *iface.UpdateUserInput) (*iface.UserManagementResponse, error) {
				return &iface.UserManagementResponse{ID: "u1", Kind: "", Role: "administrator"}, nil
			},
		}
		h := NewUserHandler(svc)
		ctx := context.WithValue(context.Background(), ctxauth.KeyUserUUID, "admin-1")
		ctx = context.WithValue(ctx, ctxauth.KeySystemRole, "super_admin")
		if _, err := h.UpdateUser(ctx, &UpdateUserRequest{
			ID:   "u1",
			Body: iface.UpdateUserInput{Role: "administrator"},
		}); err != nil {
			t.Fatalf("err = %v", err)
		}
	})

	t.Run("privileged role assignment fails closed when pre-read unavailable", func(t *testing.T) {
		t.Parallel()
		// Pre-read returns error (nil GetUser fn). Handler must refuse any
		// privileged role (super_admin) even though it can't read the target's Kind.
		svc := &fakeUserService{
			getUserFn: func(context.Context, string) (*iface.UserManagementResponse, error) {
				return nil, services.ErrUserNotFound
			},
		}
		h := NewUserHandler(svc)
		ctx := context.WithValue(context.Background(), ctxauth.KeyUserUUID, "admin-1")
		ctx = context.WithValue(ctx, ctxauth.KeySystemRole, "super_admin")
		_, err := h.UpdateUser(ctx, &UpdateUserRequest{
			ID:   "unknown",
			Body: iface.UpdateUserInput{Role: "super_admin"},
		})
		assertStatus(t, err, 403)
		assertErrCode(t, err, errcode.UserRoleEscalationForbidden)
	})
}

func TestDeleteUserHandler(t *testing.T) {
	t.Parallel()

	// targetOperator is the default "deletable" target — an active
	// operator who isn't part of the platform-administrator pool, so the
	// last-admin guard short-circuits and the request reaches the service
	// layer. Used by all happy-path / pass-through cases.
	targetOperator := func() *iface.UserManagementResponse {
		return &iface.UserManagementResponse{ID: "u1", Role: "operator", IsActive: true}
	}

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
				getUserFn: func(context.Context, string) (*iface.UserManagementResponse, error) {
					return targetOperator(), nil
				},
				softDeleteAndAliasFn: func(context.Context, string) error { return c.svcErr },
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
				if svc.lastSoftDeleteAliasID != "u1" {
					t.Errorf("expected SoftDeleteAndAliasEmail to be called with u1, got %q", svc.lastSoftDeleteAliasID)
				}
				if svc.lastDeleteID != "" {
					t.Errorf("plain DeleteUser must not be called; got id=%q", svc.lastDeleteID)
				}
				return
			}
			assertStatus(t, err, c.wantStatus)
		})
	}

	t.Run("self-delete refused with code", func(t *testing.T) {
		t.Parallel()
		// No service stubs — the handler must short-circuit before any
		// lookup happens. If it reaches the panicking fake we know the
		// guard fired too late.
		svc := &fakeUserService{}
		h := NewUserHandler(svc)
		ctx := context.WithValue(context.Background(), ctxauth.KeyUserUUID, "u1")
		_, err := h.DeleteUser(ctx, &DeleteUserRequest{ID: "u1"})
		assertStatus(t, err, 403)
		assertErrCode(t, err, errcode.UserSelfDeleteForbidden)
		if svc.lastSoftDeleteAliasID != "" || svc.lastDeleteID != "" {
			t.Errorf("guard fired too late — service was called")
		}
	})

	t.Run("last active administrator refused with code", func(t *testing.T) {
		t.Parallel()
		svc := &fakeUserService{
			getUserFn: func(context.Context, string) (*iface.UserManagementResponse, error) {
				return &iface.UserManagementResponse{ID: "u1", Role: "administrator", IsActive: true}, nil
			},
			countActiveAdminsFn: func(context.Context, string) (int64, error) {
				return 0, nil
			},
		}
		h := NewUserHandler(svc)
		_, err := h.DeleteUser(context.Background(), &DeleteUserRequest{ID: "u1"})
		assertStatus(t, err, 403)
		assertErrCode(t, err, errcode.UserLastAdminForbidden)
		if svc.lastSoftDeleteAliasID != "" {
			t.Errorf("guard should block before SoftDeleteAndAliasEmail")
		}
	})

	t.Run("non-last administrator passes guard", func(t *testing.T) {
		t.Parallel()
		// Two other active admins remain — quorum holds, delete proceeds.
		svc := &fakeUserService{
			getUserFn: func(context.Context, string) (*iface.UserManagementResponse, error) {
				return &iface.UserManagementResponse{ID: "u1", Role: "administrator", IsActive: true}, nil
			},
			countActiveAdminsFn: func(context.Context, string) (int64, error) {
				return 2, nil
			},
			softDeleteAndAliasFn: func(context.Context, string) error { return nil },
		}
		h := NewUserHandler(svc)
		if _, err := h.DeleteUser(context.Background(), &DeleteUserRequest{ID: "u1"}); err != nil {
			t.Fatalf("err = %v", err)
		}
		if svc.lastSoftDeleteAliasID != "u1" {
			t.Errorf("SoftDeleteAndAliasEmail not called")
		}
	})

	t.Run("inactive admin row skips quorum check", func(t *testing.T) {
		t.Parallel()
		// An already-deactivated admin is not in the active-administrator
		// pool, so removing them cannot strand the platform — quorum
		// counter must NOT be consulted.
		svc := &fakeUserService{
			getUserFn: func(context.Context, string) (*iface.UserManagementResponse, error) {
				return &iface.UserManagementResponse{ID: "u1", Role: "administrator", IsActive: false}, nil
			},
			softDeleteAndAliasFn: func(context.Context, string) error { return nil },
			// countActiveAdminsFn is intentionally nil — the panicking
			// fallback fires if the handler calls it.
		}
		h := NewUserHandler(svc)
		if _, err := h.DeleteUser(context.Background(), &DeleteUserRequest{ID: "u1"}); err != nil {
			t.Fatalf("err = %v", err)
		}
	})
}

// assertErrCode unwraps an errcode.Error from err and fails the test if
// the carried Code doesn't match wantCode. Use alongside assertStatus to
// pin both the HTTP status AND the stable wire code on guard responses.
func assertErrCode(t *testing.T, err error, wantCode string) {
	t.Helper()
	if err == nil {
		t.Fatalf("expected errcode.Error with code %q, got nil", wantCode)
	}
	var ec *errcode.Error
	if !errors.As(err, &ec) {
		t.Fatalf("err %v is not an *errcode.Error", err)
	}
	if ec.Code != wantCode {
		t.Errorf("code = %q, want %q", ec.Code, wantCode)
	}
}

// TestAdminLifecycleAuditEmits pins the audit emission contract: every
// admin-lifecycle path on /admin/users either lands a single
// well-shaped event on the compliance sink, or — if compliance is
// disabled (no sink wired) — emits nothing without erroring. The sink
// is exercised through the handler's auditEmitter probe so this also
// guards the type-assertion plumbing.
func TestAdminLifecycleAuditEmits(t *testing.T) {
	t.Parallel()

	boolPtr := func(b bool) *bool { return &b }
	actorCtx := func() context.Context {
		// The handler reads ActorUserID + ActorEmail + system role from
		// ctxauth; the real middleware stamps all three before the
		// handler runs. Tests default to super_admin so the role-
		// escalation guard never short-circuits the path under test —
		// individual cases override the role when needed.
		ctx := context.WithValue(context.Background(), ctxauth.KeyUserUUID, "admin-1")
		ctx = context.WithValue(ctx, ctxauth.KeyUserEmail, "admin@example.com")
		return context.WithValue(ctx, ctxauth.KeySystemRole, "super_admin")
	}

	// assertEvent fails the test unless `events` contains exactly one
	// event matching action + outcome. The single-event check is part
	// of the contract — a stray double-emit would distort SOC2 totals.
	assertEvent := func(t *testing.T, events []iface.AuditEvent, wantAction, wantOutcome string) iface.AuditEvent {
		t.Helper()
		matches := []iface.AuditEvent{}
		for _, e := range events {
			if e.Action == wantAction && e.Outcome == wantOutcome {
				matches = append(matches, e)
			}
		}
		if len(matches) != 1 {
			t.Fatalf("want exactly 1 %q/%q event, got %d (all events: %+v)", wantAction, wantOutcome, len(matches), events)
		}
		return matches[0]
	}

	t.Run("delete success emits user.deleted", func(t *testing.T) {
		t.Parallel()
		sink := &captureSink{}
		svc := &fakeUserService{
			sink: sink,
			getUserFn: func(context.Context, string) (*iface.UserManagementResponse, error) {
				return &iface.UserManagementResponse{ID: "u1", Role: "operator", IsActive: true}, nil
			},
			softDeleteAndAliasFn: func(context.Context, string) error { return nil },
		}
		h := NewUserHandler(svc)
		if _, err := h.DeleteUser(actorCtx(), &DeleteUserRequest{ID: "u1"}); err != nil {
			t.Fatalf("err = %v", err)
		}
		ev := assertEvent(t, sink.events, "user.deleted", "success")
		if ev.ResourceID != "u1" || ev.ResourceType != "user" {
			t.Errorf("resource = %s/%s, want user/u1", ev.ResourceType, ev.ResourceID)
		}
		if ev.ActorUserID != "admin-1" || ev.ActorEmail != "admin@example.com" {
			t.Errorf("actor = %s/%s, want admin-1/admin@example.com", ev.ActorUserID, ev.ActorEmail)
		}
	})

	t.Run("delete self emits user.delete.refused denied with code", func(t *testing.T) {
		t.Parallel()
		sink := &captureSink{}
		svc := &fakeUserService{sink: sink}
		h := NewUserHandler(svc)
		// Actor UUID == target UUID triggers the self-delete guard
		// before any service call, so we don't even need getUserFn.
		_, err := h.DeleteUser(actorCtx(), &DeleteUserRequest{ID: "admin-1"})
		assertStatus(t, err, 403)
		ev := assertEvent(t, sink.events, "user.delete.refused", "denied")
		if got, _ := ev.Metadata["code"].(string); got != errcode.UserSelfDeleteForbidden {
			t.Errorf("metadata.code = %q, want %q", got, errcode.UserSelfDeleteForbidden)
		}
	})

	t.Run("delete last-admin emits user.delete.refused denied with code", func(t *testing.T) {
		t.Parallel()
		sink := &captureSink{}
		svc := &fakeUserService{
			sink: sink,
			getUserFn: func(context.Context, string) (*iface.UserManagementResponse, error) {
				return &iface.UserManagementResponse{ID: "u1", Role: "administrator", IsActive: true}, nil
			},
			countActiveAdminsFn: func(context.Context, string) (int64, error) { return 0, nil },
		}
		h := NewUserHandler(svc)
		_, err := h.DeleteUser(actorCtx(), &DeleteUserRequest{ID: "u1"})
		assertStatus(t, err, 403)
		ev := assertEvent(t, sink.events, "user.delete.refused", "denied")
		if got, _ := ev.Metadata["code"].(string); got != errcode.UserLastAdminForbidden {
			t.Errorf("metadata.code = %q, want %q", got, errcode.UserLastAdminForbidden)
		}
	})

	t.Run("update activate emits user.activated", func(t *testing.T) {
		t.Parallel()
		sink := &captureSink{}
		svc := &fakeUserService{
			sink: sink,
			getUserFn: func(context.Context, string) (*iface.UserManagementResponse, error) {
				return &iface.UserManagementResponse{ID: "u1", Role: "operator", IsActive: false}, nil
			},
			updateUserFn: func(context.Context, string, *iface.UpdateUserInput) (*iface.UserManagementResponse, error) {
				return &iface.UserManagementResponse{ID: "u1", Role: "operator", IsActive: true}, nil
			},
		}
		h := NewUserHandler(svc)
		if _, err := h.UpdateUser(actorCtx(), &UpdateUserRequest{
			ID:   "u1",
			Body: iface.UpdateUserInput{IsActive: boolPtr(true)},
		}); err != nil {
			t.Fatalf("err = %v", err)
		}
		assertEvent(t, sink.events, "user.activated", "success")
	})

	t.Run("update deactivate emits user.deactivated", func(t *testing.T) {
		t.Parallel()
		sink := &captureSink{}
		svc := &fakeUserService{
			sink: sink,
			getUserFn: func(context.Context, string) (*iface.UserManagementResponse, error) {
				return &iface.UserManagementResponse{ID: "u1", Role: "operator", IsActive: true}, nil
			},
			countActiveAdminsFn: func(context.Context, string) (int64, error) { return 1, nil },
			updateUserFn: func(context.Context, string, *iface.UpdateUserInput) (*iface.UserManagementResponse, error) {
				return &iface.UserManagementResponse{ID: "u1", Role: "operator", IsActive: false}, nil
			},
		}
		h := NewUserHandler(svc)
		if _, err := h.UpdateUser(actorCtx(), &UpdateUserRequest{
			ID:   "u1",
			Body: iface.UpdateUserInput{IsActive: boolPtr(false)},
		}); err != nil {
			t.Fatalf("err = %v", err)
		}
		assertEvent(t, sink.events, "user.deactivated", "success")
	})

	t.Run("update role change emits user.role.changed with from/to", func(t *testing.T) {
		t.Parallel()
		sink := &captureSink{}
		svc := &fakeUserService{
			sink: sink,
			getUserFn: func(context.Context, string) (*iface.UserManagementResponse, error) {
				return &iface.UserManagementResponse{ID: "u1", Role: "administrator", IsActive: true}, nil
			},
			countActiveAdminsFn: func(context.Context, string) (int64, error) { return 1, nil },
			updateUserFn: func(context.Context, string, *iface.UpdateUserInput) (*iface.UserManagementResponse, error) {
				return &iface.UserManagementResponse{ID: "u1", Role: "operator", IsActive: true}, nil
			},
		}
		h := NewUserHandler(svc)
		if _, err := h.UpdateUser(actorCtx(), &UpdateUserRequest{
			ID:   "u1",
			Body: iface.UpdateUserInput{Role: "operator"},
		}); err != nil {
			t.Fatalf("err = %v", err)
		}
		ev := assertEvent(t, sink.events, "user.role.changed", "success")
		if got, _ := ev.Metadata["from"].(string); got != "administrator" {
			t.Errorf("metadata.from = %q, want administrator", got)
		}
		if got, _ := ev.Metadata["to"].(string); got != "operator" {
			t.Errorf("metadata.to = %q, want operator", got)
		}
	})

	t.Run("update last-admin demote emits user.update.refused denied", func(t *testing.T) {
		t.Parallel()
		sink := &captureSink{}
		svc := &fakeUserService{
			sink: sink,
			getUserFn: func(context.Context, string) (*iface.UserManagementResponse, error) {
				return &iface.UserManagementResponse{ID: "u1", Role: "super_admin", IsActive: true}, nil
			},
			countActiveAdminsFn: func(context.Context, string) (int64, error) { return 0, nil },
		}
		h := NewUserHandler(svc)
		_, err := h.UpdateUser(actorCtx(), &UpdateUserRequest{
			ID:   "u1",
			Body: iface.UpdateUserInput{Role: "operator"},
		})
		assertStatus(t, err, 403)
		ev := assertEvent(t, sink.events, "user.update.refused", "denied")
		if got, _ := ev.Metadata["attempted"].(string); got != "role_change" {
			t.Errorf("metadata.attempted = %q, want role_change", got)
		}
	})

	t.Run("no sink wired is a silent no-op", func(t *testing.T) {
		t.Parallel()
		// sink is nil — handler's emitAudit must short-circuit. The
		// happy-path delete still succeeds and the test passes if no
		// panic and the right HTTP outcome arrives.
		svc := &fakeUserService{
			getUserFn: func(context.Context, string) (*iface.UserManagementResponse, error) {
				return &iface.UserManagementResponse{ID: "u1", Role: "operator", IsActive: true}, nil
			},
			softDeleteAndAliasFn: func(context.Context, string) error { return nil },
		}
		h := NewUserHandler(svc)
		if _, err := h.DeleteUser(actorCtx(), &DeleteUserRequest{ID: "u1"}); err != nil {
			t.Fatalf("err = %v", err)
		}
	})
}

func TestListUsersHandler(t *testing.T) {
	t.Parallel()

	t.Run("forwards filters and pagination", func(t *testing.T) {
		t.Parallel()
		svc := &fakeUserService{
			listUsersFn: func(_ context.Context, _ *iface.UserFilters, _ *iface.PaginationParams) (*iface.UserManagementListResponse, error) {
				return &iface.UserManagementListResponse{
					Users: []iface.UserManagementResponse{{ID: "u1"}},
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
			listUsersFn: func(context.Context, *iface.UserFilters, *iface.PaginationParams) (*iface.UserManagementListResponse, error) {
				return &iface.UserManagementListResponse{}, nil
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
			listUsersFn: func(context.Context, *iface.UserFilters, *iface.PaginationParams) (*iface.UserManagementListResponse, error) {
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
			getUsersByRoleFn: func(context.Context, string) ([]*iface.UserManagementResponse, error) {
				return []*iface.UserManagementResponse{{ID: "u1"}, {ID: "u2"}}, nil
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
			getUsersByRoleFn: func(context.Context, string) ([]*iface.UserManagementResponse, error) {
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
			getUsersByRoleFn: func(context.Context, string) ([]*iface.UserManagementResponse, error) {
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
		svcResp    *iface.UserManagementResponse
		svcErr     error
		wantStatus int
	}{
		{name: "happy", svcResp: &iface.UserManagementResponse{ID: "u1"}},
		{name: "not found → 404", svcErr: services.ErrUserNotFound, wantStatus: 404},
		{name: "invalid → 400", svcErr: services.ErrInvalidInput, wantStatus: 400},
		{name: "unknown → 500", svcErr: errors.New("boom"), wantStatus: 500},
	}
	for _, c := range cases {
		c := c
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			svc := &fakeUserService{
				getUserByEmailFn: func(context.Context, string) (*iface.UserManagementResponse, error) {
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
			getUserCountFn: func(context.Context, *iface.UserFilters) (int64, error) { return 42, nil },
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
			getUserCountFn: func(context.Context, *iface.UserFilters) (int64, error) {
				return 0, errors.New("boom")
			},
		}
		h := NewUserHandler(svc)
		_, err := h.GetUserCount(context.Background(), &GetUserCountRequest{})
		assertStatus(t, err, 500)
	})
}
