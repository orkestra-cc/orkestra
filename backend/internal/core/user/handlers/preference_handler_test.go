package handlers

import (
	"context"
	"errors"
	"net/http"
	"testing"

	"github.com/orkestra/backend/internal/core/user/services"
	"github.com/orkestra/backend/internal/shared/errcode"
	"github.com/orkestra/backend/pkg/sdk/ctxauth"
	"github.com/orkestra/backend/pkg/sdk/iface"
	"github.com/orkestra/backend/pkg/sdk/module"
)

type fakePrefService struct {
	get    func(userUUID, mod string) (*services.ModulePreferences, error)
	put    func(userUUID, mod string, values map[string]string) (*services.ModulePreferences, error)
	getAll func(userUUID string) ([]*services.ModulePreferences, error)
}

func (f *fakePrefService) Get(_ context.Context, u, m string) (*services.ModulePreferences, error) {
	return f.get(u, m)
}
func (f *fakePrefService) GetAll(_ context.Context, u string) ([]*services.ModulePreferences, error) {
	return f.getAll(u)
}
func (f *fakePrefService) Put(_ context.Context, u, m string, v map[string]string) (*services.ModulePreferences, error) {
	return f.put(u, m, v)
}

// fakeAuthz grants `allowed` at any scope, and `inTenant[tenant]` only in
// that tenant.
type fakeAuthz struct {
	allowed  map[string]bool
	inTenant map[string]map[string]bool
}

func (f fakeAuthz) HasPermission(_ context.Context, _, tenantID, perm string) (bool, error) {
	return f.allowed[perm] || f.inTenant[tenantID][perm], nil
}
func (f fakeAuthz) GetEffectivePermissions(context.Context, string, string) ([]string, error) {
	return nil, nil
}
func (f fakeAuthz) RegisterPermissions(context.Context, []iface.PermissionSpec) error { return nil }

func userCtx(uuid string) context.Context {
	return context.WithValue(context.Background(), ctxauth.KeyUserUUID, uuid)
}

func TestPreferenceHandler_RequiresAPrincipal(t *testing.T) {
	h := NewPreferenceHandler(&fakePrefService{}, nil, nil, nil)
	_, err := h.GetMyPreferences(context.Background(), &GetMyPreferencesRequest{Module: "crm"})
	assertStatus(t, err, http.StatusUnauthorized)
	_, err = h.PutMyPreferences(context.Background(), &PutMyPreferencesRequest{Module: "crm"})
	assertStatus(t, err, http.StatusUnauthorized)
	_, err = h.ListMyPreferences(context.Background(), nil)
	assertStatus(t, err, http.StatusUnauthorized)
	_, err = h.MyProfileSections(context.Background(), nil)
	assertStatus(t, err, http.StatusUnauthorized)
}

func TestPreferenceHandler_MapsServiceErrors(t *testing.T) {
	svc := &fakePrefService{
		get: func(_, m string) (*services.ModulePreferences, error) {
			if m == "nope" {
				return nil, services.ErrPreferenceModuleUnknown
			}
			return &services.ModulePreferences{Module: m, Values: map[string]string{"a": "1"}}, nil
		},
		put: func(_, _ string, _ map[string]string) (*services.ModulePreferences, error) {
			return nil, errors.Join(services.ErrPreferenceInvalid, module.ValueErrors{
				{Field: "pageSize", Message: "must be at most 100"},
				{Field: "ghost", Message: "is not a declared field"},
			})
		},
	}
	h := NewPreferenceHandler(svc, nil, nil, nil)

	_, err := h.GetMyPreferences(userCtx("u-1"), &GetMyPreferencesRequest{Module: "nope"})
	assertStatus(t, err, http.StatusNotFound)
	assertErrCode(t, err, errcode.UserPreferencesModuleUnknown)

	resp, err := h.GetMyPreferences(userCtx("u-1"), &GetMyPreferencesRequest{Module: "crm"})
	if err != nil || resp.Body.Values["a"] != "1" {
		t.Fatalf("Get: resp=%+v err=%v", resp, err)
	}

	_, err = h.PutMyPreferences(userCtx("u-1"), &PutMyPreferencesRequest{Module: "crm"})
	assertStatus(t, err, http.StatusUnprocessableEntity)
	assertErrCode(t, err, errcode.UserPreferencesInvalid)
	var e *errcode.Error
	if errors.As(err, &e) && (e.Detail == "" || !contains(e.Detail, "pageSize") || !contains(e.Detail, "ghost")) {
		t.Errorf("422 detail must list every problem, got %q", e.Detail)
	}
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && (func() bool {
		for i := 0; i+len(sub) <= len(s); i++ {
			if s[i:i+len(sub)] == sub {
				return true
			}
		}
		return false
	})()
}

func TestPreferenceHandler_PutPassesTheCallerAndValues(t *testing.T) {
	var gotUser, gotModule string
	var gotValues map[string]string
	svc := &fakePrefService{put: func(u, m string, v map[string]string) (*services.ModulePreferences, error) {
		gotUser, gotModule, gotValues = u, m, v
		return &services.ModulePreferences{Module: m, Values: v}, nil
	}}
	h := NewPreferenceHandler(svc, nil, nil, nil)
	req := &PutMyPreferencesRequest{Module: "crm"}
	req.Body.Values = map[string]string{"columns": "name"}
	if _, err := h.PutMyPreferences(userCtx("u-9"), req); err != nil {
		t.Fatal(err)
	}
	if gotUser != "u-9" || gotModule != "crm" || gotValues["columns"] != "name" {
		t.Errorf("put forwarded (%q, %q, %v)", gotUser, gotModule, gotValues)
	}
}

func TestPreferenceHandler_ProfileSectionsFilterByModuleStateAndPermission(t *testing.T) {
	reg := module.NewUserPreferenceRegistry()
	if err := reg.RegisterSections("people", []module.ProfileSectionSpec{
		{Key: "people.person", LabelKey: "l", Surfaces: []string{"self", "admin"}},
		{Key: "people.hr", LabelKey: "l", Permission: "people.update", Surfaces: []string{"admin"}},
	}); err != nil {
		t.Fatal(err)
	}
	if err := reg.RegisterSections("crm", []module.ProfileSectionSpec{{Key: "crm.activity", LabelKey: "l"}}); err != nil {
		t.Fatal(err)
	}
	enabled := func(_ context.Context, m string) bool { return m != "crm" }
	h := NewPreferenceHandler(&fakePrefService{}, reg, enabled, fakeAuthz{allowed: map[string]bool{}})

	self, err := h.MyProfileSections(userCtx("u-1"), nil)
	if err != nil {
		t.Fatal(err)
	}
	if self.Body.Surface != "self" || len(self.Body.Sections) != 1 || self.Body.Sections[0].Key != "people.person" {
		t.Errorf("self sections = %+v (crm disabled must be dropped)", self.Body.Sections)
	}
	admin, err := h.ProfileSectionsForUser(userCtx("u-1"), &ProfileSectionsForUserRequest{ID: "t-1"})
	if err != nil {
		t.Fatal(err)
	}
	if len(admin.Body.Sections) != 1 || admin.Body.Sections[0].Key != "people.person" {
		t.Errorf("admin without people.update = %+v", admin.Body.Sections)
	}
	// With the permission the gated section appears.
	h2 := NewPreferenceHandler(&fakePrefService{}, reg, enabled, fakeAuthz{allowed: map[string]bool{"people.update": true}})
	tenantCtx := context.WithValue(userCtx("u-1"), ctxauth.KeyTenantID, "tenant-1")
	admin, _ = h2.ProfileSectionsForUser(tenantCtx, &ProfileSectionsForUserRequest{ID: "t-1"})
	if len(admin.Body.Sections) != 2 {
		t.Errorf("admin with people.update = %+v", admin.Body.Sections)
	}
	// No authz wired: a discovery list keeps every enabled section.
	h3 := NewPreferenceHandler(&fakePrefService{}, reg, enabled, nil)
	admin, _ = h3.ProfileSectionsForUser(userCtx("u-1"), &ProfileSectionsForUserRequest{ID: "t-1"})
	if len(admin.Body.Sections) != 2 {
		t.Errorf("admin without authz provider = %+v", admin.Body.Sections)
	}
	// No catalog at all: empty list, not nil.
	h4 := NewPreferenceHandler(&fakePrefService{}, nil, nil, nil)
	self, _ = h4.MyProfileSections(userCtx("u-1"), nil)
	if self.Body.Sections == nil || len(self.Body.Sections) != 0 {
		t.Errorf("no catalog → empty list, got %v", self.Body.Sections)
	}
}

// The section permission is checked in the request tenant, the scope the
// SPA's slot registry and the module's own routes use; at global scope a
// platform role conveys every `.self` key, so a global check listed a
// section the module then refused with 403. With no tenant a gated section
// is dropped rather than checked globally.
func TestPreferenceHandler_ProfileSectionPermissionIsTenantScoped(t *testing.T) {
	reg := module.NewUserPreferenceRegistry()
	if err := reg.RegisterSections("people", []module.ProfileSectionSpec{
		{Key: "people.person", LabelKey: "l", Permission: "people.self", Surfaces: []string{"self"}},
	}); err != nil {
		t.Fatal(err)
	}
	enabled := func(context.Context, string) bool { return true }
	// Held at global scope only (a platform operator), and in tenant-1 only.
	authz := fakeAuthz{inTenant: map[string]map[string]bool{"": {"people.self": true}, "tenant-1": {"people.self": true}}}
	h := NewPreferenceHandler(&fakePrefService{}, reg, enabled, authz)

	inTenant := func(tid string) context.Context {
		return context.WithValue(userCtx("u-1"), ctxauth.KeyTenantID, tid)
	}
	for _, tc := range []struct {
		name string
		ctx  context.Context
		want int
	}{
		{"member tenant", inTenant("tenant-1"), 1},
		{"tenant without the key", inTenant("tenant-2"), 0},
		{"no tenant resolved: gated section dropped", userCtx("u-1"), 0},
	} {
		got, err := h.MyProfileSections(tc.ctx, nil)
		if err != nil {
			t.Fatal(err)
		}
		if len(got.Body.Sections) != tc.want {
			t.Errorf("%s: sections = %+v, want %d", tc.name, got.Body.Sections, tc.want)
		}
	}
}
