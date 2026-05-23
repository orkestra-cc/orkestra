package handlers

import (
	"context"
	"errors"
	"testing"

	"github.com/orkestra-cc/orkestra-sdk/iface"
	"github.com/orkestra-cc/orkestra-sdk/module"
	"github.com/orkestra/backend/internal/core/user/models"
	"github.com/orkestra/backend/internal/core/user/services"
)

// fakePasswordHasher implements iface.PasswordHasher for the
// create-flow tests. hashFn / validateFn default to no-op success.
type fakePasswordHasher struct {
	hashFn     func(plaintext string) (string, error)
	validateFn func(ctx context.Context, plaintext, email string) error
}

func (f *fakePasswordHasher) Hash(plaintext string) (string, error) {
	if f.hashFn != nil {
		return f.hashFn(plaintext)
	}
	return "argon2id$" + plaintext, nil
}
func (f *fakePasswordHasher) ValidatePolicy(ctx context.Context, plaintext, email string) error {
	if f.validateFn != nil {
		return f.validateFn(ctx, plaintext, email)
	}
	return nil
}

// fakeAdminAuthInviter is the slim iface.AdminAuthInviter the admin
// client handler reaches through the registry to send invites /
// verification / reset emails.
type fakeAdminAuthInviter struct {
	sendInviteFn           func(ctx context.Context, userUUID, inviterName string) error
	resendVerificationFn   func(ctx context.Context, userUUID string) error
	triggerPasswordResetFn func(ctx context.Context, userUUID string) error
}

func (f *fakeAdminAuthInviter) AdminSendInvite(ctx context.Context, u, n string) error {
	return f.sendInviteFn(ctx, u, n)
}
func (f *fakeAdminAuthInviter) AdminResendVerification(ctx context.Context, u string) error {
	return f.resendVerificationFn(ctx, u)
}
func (f *fakeAdminAuthInviter) AdminTriggerPasswordReset(ctx context.Context, u string) error {
	return f.triggerPasswordResetFn(ctx, u)
}

// fakeTenantProvider implements iface.TenantProvider but only the
// ListUserMemberships method is exercised — everything else panics so
// regressions that reach for the wider surface fail loud.
type fakeTenantProvider struct {
	listMembershipsFn func(ctx context.Context, userUUID string) ([]iface.TenantMembership, error)
}

func (f *fakeTenantProvider) ListUserMemberships(ctx context.Context, userUUID string) ([]iface.TenantMembership, error) {
	return f.listMembershipsFn(ctx, userUUID)
}
func (f *fakeTenantProvider) GetTenant(context.Context, string) (*iface.Tenant, error) {
	panic("unused: GetTenant")
}
func (f *fakeTenantProvider) IsMember(context.Context, string, string) (bool, error) {
	panic("unused: IsMember")
}
func (f *fakeTenantProvider) ActivateTenant(context.Context, string) error {
	panic("unused: ActivateTenant")
}
func (f *fakeTenantProvider) SetTenantStripeCustomerID(context.Context, string, string) error {
	panic("unused: SetTenantStripeCustomerID")
}
func (f *fakeTenantProvider) EnsureTenantForUser(context.Context, string) (*iface.Tenant, error) {
	panic("unused: EnsureTenantForUser")
}

// newAdminHandler is the common test wiring: a fakeUserService plus a
// fresh ServiceRegistry. The caller layers tenant / hasher / inviter
// onto the registry as the specific test needs.
func newAdminHandler(svc services.UserService) (*AdminClientUserHandler, *module.ServiceRegistry) {
	reg := module.NewServiceRegistry()
	return NewAdminClientUserHandler(svc, reg), reg
}

func TestListClientUsersAdmin_NoTenantProvider(t *testing.T) {
	t.Parallel()
	svc := &fakeUserService{
		listUsersFn: func(context.Context, *models.UserFilters, *models.PaginationParams) (*models.UserManagementListResponse, error) {
			return &models.UserManagementListResponse{
				Users: []models.UserManagementResponse{
					{ID: "u1", Email: "a@b.c"},
					{ID: "u2", Email: "x@y.z"},
				},
				Total: 2, Page: 1, PageSize: 50, TotalPages: 1,
			}, nil
		},
	}
	h, _ := newAdminHandler(svc)
	resp, err := h.ListClientUsersAdmin(context.Background(), &ListClientUsersAdminRequest{
		Page: 1, PageSize: 50, IsActive: true, EmailVerified: true, Role: "operator", Search: "x",
	})
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if len(resp.Body.Users) != 2 {
		t.Fatalf("users=%d, want 2", len(resp.Body.Users))
	}
	for _, u := range resp.Body.Users {
		if u.Memberships == nil {
			t.Errorf("user %q: Memberships should be empty slice, not nil", u.ID)
		}
		if len(u.Memberships) != 0 {
			t.Errorf("user %q: expected zero memberships, got %d", u.ID, len(u.Memberships))
		}
	}
}

func TestListClientUsersAdmin_WithTenantProvider(t *testing.T) {
	t.Parallel()
	svc := &fakeUserService{
		listUsersFn: func(context.Context, *models.UserFilters, *models.PaginationParams) (*models.UserManagementListResponse, error) {
			return &models.UserManagementListResponse{
				Users: []models.UserManagementResponse{{ID: "u1"}, {ID: "u2"}},
			}, nil
		},
	}
	h, reg := newAdminHandler(svc)
	reg.Register(module.ServiceTenantProvider, &fakeTenantProvider{
		listMembershipsFn: func(_ context.Context, userUUID string) ([]iface.TenantMembership, error) {
			if userUUID == "u1" {
				return []iface.TenantMembership{
					{TenantUUID: "t1", TenantName: "T1", Roles: []string{"org_owner"}, IsOwner: true},
				}, nil
			}
			// Second user has a tenant lookup failure — handler must
			// not abort the whole list; just return empty memberships.
			return nil, errors.New("boom")
		},
	})

	resp, err := h.ListClientUsersAdmin(context.Background(), &ListClientUsersAdminRequest{Page: 1, PageSize: 50})
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	u1 := resp.Body.Users[0]
	u2 := resp.Body.Users[1]
	if len(u1.Memberships) != 1 || u1.Memberships[0].TenantUUID != "t1" {
		t.Errorf("u1 memberships = %+v", u1.Memberships)
	}
	if len(u2.Memberships) != 0 {
		t.Errorf("u2 memberships should be empty on lookup err, got %+v", u2.Memberships)
	}
}

func TestListClientUsersAdmin_SvcError(t *testing.T) {
	t.Parallel()
	svc := &fakeUserService{
		listUsersFn: func(context.Context, *models.UserFilters, *models.PaginationParams) (*models.UserManagementListResponse, error) {
			return nil, errors.New("boom")
		},
	}
	h, _ := newAdminHandler(svc)
	_, err := h.ListClientUsersAdmin(context.Background(), &ListClientUsersAdminRequest{Page: 1, PageSize: 50})
	assertStatus(t, err, 500)
}

func TestGetClientUserAdmin(t *testing.T) {
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
				getUserFn: func(context.Context, string) (*models.UserManagementResponse, error) {
					if c.svcErr != nil {
						return nil, c.svcErr
					}
					return &models.UserManagementResponse{ID: "u1", Email: "a@b.c"}, nil
				},
			}
			h, _ := newAdminHandler(svc)
			resp, err := h.GetClientUserAdmin(context.Background(), &GetClientUserAdminRequest{ID: "u1"})
			if c.wantStatus == 0 {
				if err != nil {
					t.Fatalf("err = %v", err)
				}
				if resp.Body.ID != "u1" {
					t.Errorf("body.ID = %q", resp.Body.ID)
				}
				if resp.Body.Memberships == nil {
					t.Errorf("Memberships should be empty slice, not nil")
				}
				return
			}
			assertStatus(t, err, c.wantStatus)
		})
	}
}

func TestUpdateClientUserAdmin(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name       string
		updateErr  error
		wantStatus int
	}{
		{name: "happy"},
		{name: "not found → 404", updateErr: services.ErrUserNotFound, wantStatus: 404},
		{name: "email taken → 409", updateErr: services.ErrEmailNotUnique, wantStatus: 409},
		{name: "invalid → 400", updateErr: services.ErrInvalidInput, wantStatus: 400},
		{name: "unknown → 500", updateErr: errors.New("boom"), wantStatus: 500},
	}
	for _, c := range cases {
		c := c
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			svc := &fakeUserService{
				updateUserFn: func(context.Context, string, *models.UpdateUserInput) (*models.UserManagementResponse, error) {
					if c.updateErr != nil {
						return nil, c.updateErr
					}
					return &models.UserManagementResponse{ID: "u1", FullName: "Renamed"}, nil
				},
				getUserFn: func(context.Context, string) (*models.UserManagementResponse, error) {
					return &models.UserManagementResponse{ID: "u1", FullName: "Renamed"}, nil
				},
			}
			h, _ := newAdminHandler(svc)
			resp, err := h.UpdateClientUserAdmin(context.Background(), &UpdateClientUserAdminRequest{
				ID:   "u1",
				Body: UpdateClientUserAdminBody{FullName: "Renamed"},
			})
			if c.wantStatus == 0 {
				if err != nil {
					t.Fatalf("err = %v", err)
				}
				if resp.Body.FullName != "Renamed" {
					t.Errorf("FullName = %q", resp.Body.FullName)
				}
				return
			}
			assertStatus(t, err, c.wantStatus)
		})
	}
}

func TestUpdateClientUserAdmin_ReloadFails(t *testing.T) {
	t.Parallel()
	svc := &fakeUserService{
		updateUserFn: func(context.Context, string, *models.UpdateUserInput) (*models.UserManagementResponse, error) {
			return &models.UserManagementResponse{ID: "u1"}, nil
		},
		getUserFn: func(context.Context, string) (*models.UserManagementResponse, error) {
			return nil, errors.New("post-update read failed")
		},
	}
	h, _ := newAdminHandler(svc)
	_, err := h.UpdateClientUserAdmin(context.Background(), &UpdateClientUserAdminRequest{
		ID: "u1", Body: UpdateClientUserAdminBody{FullName: "x"},
	})
	assertStatus(t, err, 500)
}

func TestDeleteClientUserAdmin(t *testing.T) {
	t.Parallel()
	t.Run("happy", func(t *testing.T) {
		t.Parallel()
		svc := &fakeUserService{softDeleteAndAliasFn: func(context.Context, string) error { return nil }}
		h, _ := newAdminHandler(svc)
		resp, err := h.DeleteClientUserAdmin(context.Background(), &DeleteClientUserAdminRequest{ID: "u1"})
		if err != nil {
			t.Fatalf("err = %v", err)
		}
		if resp.Body.Message != "Client user deleted" {
			t.Errorf("message = %q", resp.Body.Message)
		}
	})
	t.Run("invalid → 400", func(t *testing.T) {
		t.Parallel()
		svc := &fakeUserService{softDeleteAndAliasFn: func(context.Context, string) error {
			return services.ErrInvalidInput
		}}
		h, _ := newAdminHandler(svc)
		_, err := h.DeleteClientUserAdmin(context.Background(), &DeleteClientUserAdminRequest{ID: ""})
		assertStatus(t, err, 400)
	})
	t.Run("unknown → 500", func(t *testing.T) {
		t.Parallel()
		svc := &fakeUserService{softDeleteAndAliasFn: func(context.Context, string) error {
			return errors.New("boom")
		}}
		h, _ := newAdminHandler(svc)
		_, err := h.DeleteClientUserAdmin(context.Background(), &DeleteClientUserAdminRequest{ID: "u1"})
		assertStatus(t, err, 500)
	})
}

func TestCreateClientUserAdmin_NoHasher(t *testing.T) {
	t.Parallel()
	// PasswordHasher not registered → 503. We don't even need a UserService
	// behaviour because the early-exit fires before reaching it.
	h, _ := newAdminHandler(&fakeUserService{})
	_, err := h.CreateClientUserAdmin(context.Background(), &CreateClientUserAdminRequest{
		Body: CreateClientUserAdminBody{
			Email: "a@b.c", FullName: "x", Role: "operator", Password: "Hunter2!Hunter2!",
		},
	})
	assertStatus(t, err, 503)
}

func TestCreateClientUserAdmin_PolicyFailure(t *testing.T) {
	t.Parallel()
	h, reg := newAdminHandler(&fakeUserService{})
	reg.Register(module.ServicePasswordService, &fakePasswordHasher{
		validateFn: func(context.Context, string, string) error {
			return errors.New("password too short")
		},
	})
	_, err := h.CreateClientUserAdmin(context.Background(), &CreateClientUserAdminRequest{
		Body: CreateClientUserAdminBody{Email: "a@b.c", FullName: "x", Role: "operator", Password: "short"},
	})
	assertStatus(t, err, 400)
}

func TestCreateClientUserAdmin_HashFailure(t *testing.T) {
	t.Parallel()
	h, reg := newAdminHandler(&fakeUserService{})
	reg.Register(module.ServicePasswordService, &fakePasswordHasher{
		hashFn: func(string) (string, error) { return "", errors.New("entropy gone") },
	})
	_, err := h.CreateClientUserAdmin(context.Background(), &CreateClientUserAdminRequest{
		Body: CreateClientUserAdminBody{Email: "a@b.c", FullName: "x", Role: "operator", Password: "Hunter2!Hunter2!"},
	})
	assertStatus(t, err, 500)
}

func TestCreateClientUserAdmin_ServiceErrors(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name       string
		createErr  error
		wantStatus int
	}{
		{"duplicate email → 409", services.ErrEmailNotUnique, 409},
		{"invalid input → 400", services.ErrInvalidInput, 400},
		{"unknown → 500", errors.New("boom"), 500},
	}
	for _, c := range cases {
		c := c
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			svc := &fakeUserService{
				createUserWithPasswordFn: func(context.Context, *models.CreateUserInput) (*models.User, error) {
					return nil, c.createErr
				},
			}
			h, reg := newAdminHandler(svc)
			reg.Register(module.ServicePasswordService, &fakePasswordHasher{})
			_, err := h.CreateClientUserAdmin(context.Background(), &CreateClientUserAdminRequest{
				Body: CreateClientUserAdminBody{
					Email: "a@b.c", FullName: "x", Role: "operator", Password: "Hunter2!Hunter2!",
				},
			})
			assertStatus(t, err, c.wantStatus)
		})
	}
}

func TestCreateClientUserAdmin_Happy(t *testing.T) {
	t.Parallel()
	markCalled := false
	svc := &fakeUserService{
		createUserWithPasswordFn: func(_ context.Context, input *models.CreateUserInput) (*models.User, error) {
			if input.PasswordHash == "" {
				t.Errorf("PasswordHash not propagated to service")
			}
			return &models.User{UUID: "new-uuid", Email: input.Email}, nil
		},
		markEmailVerifiedFn: func(_ context.Context, userUUID string) error {
			markCalled = true
			if userUUID != "new-uuid" {
				t.Errorf("MarkEmailVerified got %q, want new-uuid", userUUID)
			}
			return nil
		},
		getUserFn: func(context.Context, string) (*models.UserManagementResponse, error) {
			return &models.UserManagementResponse{ID: "new-uuid", Email: "a@b.c", EmailVerified: true}, nil
		},
	}
	h, reg := newAdminHandler(svc)
	reg.Register(module.ServicePasswordService, &fakePasswordHasher{})
	resp, err := h.CreateClientUserAdmin(context.Background(), &CreateClientUserAdminRequest{
		Body: CreateClientUserAdminBody{
			Email: "a@b.c", FullName: "x", Role: "operator", Password: "Hunter2!Hunter2!",
		},
	})
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if !markCalled {
		t.Errorf("MarkEmailVerified must be called on admin create path")
	}
	if resp.Body.ID != "new-uuid" {
		t.Errorf("body.ID = %q", resp.Body.ID)
	}
}

func TestCreateClientUserAdmin_MarkVerifiedFailureIsBestEffort(t *testing.T) {
	t.Parallel()
	// A failure to flip EmailVerified must NOT fail the request — the
	// row was created successfully and the operator can verify manually.
	svc := &fakeUserService{
		createUserWithPasswordFn: func(context.Context, *models.CreateUserInput) (*models.User, error) {
			return &models.User{UUID: "u1"}, nil
		},
		markEmailVerifiedFn: func(context.Context, string) error { return errors.New("mongo down") },
		getUserFn: func(context.Context, string) (*models.UserManagementResponse, error) {
			return &models.UserManagementResponse{ID: "u1"}, nil
		},
	}
	h, reg := newAdminHandler(svc)
	reg.Register(module.ServicePasswordService, &fakePasswordHasher{})
	resp, err := h.CreateClientUserAdmin(context.Background(), &CreateClientUserAdminRequest{
		Body: CreateClientUserAdminBody{
			Email: "a@b.c", FullName: "x", Role: "operator", Password: "Hunter2!Hunter2!",
		},
	})
	if err != nil {
		t.Fatalf("err = %v (should swallow MarkEmailVerified failure)", err)
	}
	if resp.Body.ID != "u1" {
		t.Errorf("body.ID = %q", resp.Body.ID)
	}
}

func TestInviteClientUserAdmin_NoAuth(t *testing.T) {
	t.Parallel()
	h, _ := newAdminHandler(&fakeUserService{})
	_, err := h.InviteClientUserAdmin(context.Background(), &InviteClientUserAdminRequest{
		Body: InviteClientUserAdminBody{Email: "a@b.c", FullName: "x", Role: "operator"},
	})
	assertStatus(t, err, 503)
}

func TestInviteClientUserAdmin_Happy(t *testing.T) {
	t.Parallel()
	svc := &fakeUserService{
		createUserFn: func(_ context.Context, input *models.CreateUserInput) (*models.UserManagementResponse, error) {
			return &models.UserManagementResponse{ID: "u1", Email: input.Email}, nil
		},
		getUserFn: func(context.Context, string) (*models.UserManagementResponse, error) {
			return &models.UserManagementResponse{ID: "u1"}, nil
		},
	}
	inviterCalled := false
	h, reg := newAdminHandler(svc)
	reg.Register(module.ServiceClientPasswordAuthService, &fakeAdminAuthInviter{
		sendInviteFn: func(_ context.Context, u, n string) error {
			inviterCalled = true
			if u != "u1" || n != "Operator Bob" {
				t.Errorf("got (uuid=%q, name=%q)", u, n)
			}
			return nil
		},
	})
	resp, err := h.InviteClientUserAdmin(context.Background(), &InviteClientUserAdminRequest{
		Body: InviteClientUserAdminBody{
			Email: "a@b.c", FullName: "x", Role: "operator", InviterName: "Operator Bob",
		},
	})
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if !inviterCalled {
		t.Errorf("inviter not called")
	}
	if resp.Body.ID != "u1" {
		t.Errorf("body.ID = %q", resp.Body.ID)
	}
}

func TestInviteClientUserAdmin_CreateErrors(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name       string
		err        error
		wantStatus int
	}{
		{"dup email → 409", services.ErrEmailNotUnique, 409},
		{"invalid → 400", services.ErrInvalidInput, 400},
		{"unknown → 500", errors.New("boom"), 500},
	}
	for _, c := range cases {
		c := c
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			svc := &fakeUserService{
				createUserFn: func(context.Context, *models.CreateUserInput) (*models.UserManagementResponse, error) {
					return nil, c.err
				},
			}
			h, reg := newAdminHandler(svc)
			reg.Register(module.ServiceClientPasswordAuthService, &fakeAdminAuthInviter{})
			_, err := h.InviteClientUserAdmin(context.Background(), &InviteClientUserAdminRequest{
				Body: InviteClientUserAdminBody{Email: "a@b.c", FullName: "x", Role: "operator"},
			})
			assertStatus(t, err, c.wantStatus)
		})
	}
}

func TestInviteClientUserAdmin_SendFailureIs502(t *testing.T) {
	t.Parallel()
	// User row was created but the invite email failed — handler must
	// return 502 (and intentionally NOT roll back the user, so a resend
	// from the detail page can fix it).
	svc := &fakeUserService{
		createUserFn: func(context.Context, *models.CreateUserInput) (*models.UserManagementResponse, error) {
			return &models.UserManagementResponse{ID: "u1"}, nil
		},
	}
	h, reg := newAdminHandler(svc)
	reg.Register(module.ServiceClientPasswordAuthService, &fakeAdminAuthInviter{
		sendInviteFn: func(context.Context, string, string) error { return errors.New("smtp down") },
	})
	_, err := h.InviteClientUserAdmin(context.Background(), &InviteClientUserAdminRequest{
		Body: InviteClientUserAdminBody{Email: "a@b.c", FullName: "x", Role: "operator"},
	})
	assertStatus(t, err, 502)
}

func TestResendInviteClientUserAdmin(t *testing.T) {
	t.Parallel()
	t.Run("no auth → 503", func(t *testing.T) {
		t.Parallel()
		h, _ := newAdminHandler(&fakeUserService{})
		_, err := h.ResendInviteClientUserAdmin(context.Background(), &ResendInviteClientUserAdminRequest{ID: "u1"})
		assertStatus(t, err, 503)
	})
	t.Run("happy", func(t *testing.T) {
		t.Parallel()
		h, reg := newAdminHandler(&fakeUserService{})
		reg.Register(module.ServiceClientPasswordAuthService, &fakeAdminAuthInviter{
			sendInviteFn: func(context.Context, string, string) error { return nil },
		})
		resp, err := h.ResendInviteClientUserAdmin(context.Background(), &ResendInviteClientUserAdminRequest{ID: "u1"})
		if err != nil {
			t.Fatalf("err = %v", err)
		}
		if !resp.Body.Success || resp.Body.Message == "" {
			t.Errorf("response = %+v", resp.Body)
		}
	})
	t.Run("not found → 404", func(t *testing.T) {
		t.Parallel()
		h, reg := newAdminHandler(&fakeUserService{})
		reg.Register(module.ServiceClientPasswordAuthService, &fakeAdminAuthInviter{
			sendInviteFn: func(context.Context, string, string) error { return services.ErrUserNotFound },
		})
		_, err := h.ResendInviteClientUserAdmin(context.Background(), &ResendInviteClientUserAdminRequest{ID: "ghost"})
		assertStatus(t, err, 404)
	})
}

func TestResendVerificationClientUserAdmin(t *testing.T) {
	t.Parallel()
	t.Run("no auth → 503", func(t *testing.T) {
		t.Parallel()
		h, _ := newAdminHandler(&fakeUserService{})
		_, err := h.ResendVerificationClientUserAdmin(context.Background(), &ResendVerificationClientUserAdminRequest{ID: "u1"})
		assertStatus(t, err, 503)
	})
	t.Run("happy", func(t *testing.T) {
		t.Parallel()
		h, reg := newAdminHandler(&fakeUserService{})
		reg.Register(module.ServiceClientPasswordAuthService, &fakeAdminAuthInviter{
			resendVerificationFn: func(context.Context, string) error { return nil },
		})
		resp, err := h.ResendVerificationClientUserAdmin(context.Background(), &ResendVerificationClientUserAdminRequest{ID: "u1"})
		if err != nil || !resp.Body.Success {
			t.Errorf("err=%v body=%+v", err, resp.Body)
		}
	})
	t.Run("invalid → 400", func(t *testing.T) {
		t.Parallel()
		h, reg := newAdminHandler(&fakeUserService{})
		reg.Register(module.ServiceClientPasswordAuthService, &fakeAdminAuthInviter{
			resendVerificationFn: func(context.Context, string) error { return services.ErrInvalidInput },
		})
		_, err := h.ResendVerificationClientUserAdmin(context.Background(), &ResendVerificationClientUserAdminRequest{ID: ""})
		assertStatus(t, err, 400)
	})
}

func TestSendPasswordResetClientUserAdmin(t *testing.T) {
	t.Parallel()
	t.Run("no auth → 503", func(t *testing.T) {
		t.Parallel()
		h, _ := newAdminHandler(&fakeUserService{})
		_, err := h.SendPasswordResetClientUserAdmin(context.Background(), &SendPasswordResetClientUserAdminRequest{ID: "u1"})
		assertStatus(t, err, 503)
	})
	t.Run("happy", func(t *testing.T) {
		t.Parallel()
		h, reg := newAdminHandler(&fakeUserService{})
		reg.Register(module.ServiceClientPasswordAuthService, &fakeAdminAuthInviter{
			triggerPasswordResetFn: func(context.Context, string) error { return nil },
		})
		resp, err := h.SendPasswordResetClientUserAdmin(context.Background(), &SendPasswordResetClientUserAdminRequest{ID: "u1"})
		if err != nil || !resp.Body.Success {
			t.Errorf("err=%v body=%+v", err, resp.Body)
		}
	})
	t.Run("notification down → 503", func(t *testing.T) {
		t.Parallel()
		// mapInviteErr does message-matching for this sentinel because
		// auth's ErrNotificationDown isn't importable from the user
		// package. Pin the exact string the matcher expects.
		h, reg := newAdminHandler(&fakeUserService{})
		reg.Register(module.ServiceClientPasswordAuthService, &fakeAdminAuthInviter{
			triggerPasswordResetFn: func(context.Context, string) error {
				return errors.New("notifications disabled — cannot send email")
			},
		})
		_, err := h.SendPasswordResetClientUserAdmin(context.Background(), &SendPasswordResetClientUserAdminRequest{ID: "u1"})
		assertStatus(t, err, 503)
	})
}

func TestMapInviteErr(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name       string
		in         error
		wantStatus int
		wantNil    bool
	}{
		{name: "nil passes through", in: nil, wantNil: true},
		{name: "ErrUserNotFound → 404", in: services.ErrUserNotFound, wantStatus: 404},
		{name: "ErrInvalidInput → 400", in: services.ErrInvalidInput, wantStatus: 400},
		{name: "notification-down sentinel → 503", in: errors.New("notifications disabled — cannot send email"), wantStatus: 503},
		{name: "anything else → 500", in: errors.New("boom"), wantStatus: 500},
	}
	for _, c := range cases {
		c := c
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			got := mapInviteErr(c.in, "generic")
			if c.wantNil {
				if got != nil {
					t.Errorf("got %v, want nil", got)
				}
				return
			}
			assertStatus(t, got, c.wantStatus)
		})
	}
}
