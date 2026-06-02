package middleware

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	authModels "github.com/orkestra/backend/internal/core/auth/models"
	sharedErrors "github.com/orkestra/backend/internal/shared/errors"
	"github.com/orkestra/backend/pkg/sdk/ctxauth"
	"github.com/orkestra/backend/pkg/sdk/iface"
)

// --- Test doubles -----------------------------------------------------------

type fakeAuthz struct {
	allow    bool
	forPerm  string
	forUser  string
	lastCall string
}

func (f *fakeAuthz) HasPermission(_ context.Context, userUUID, _, permission string) (bool, error) {
	f.lastCall = userUUID + ":" + permission
	if !f.allow {
		return false, nil
	}
	if f.forPerm != "" && f.forPerm != permission {
		return false, nil
	}
	if f.forUser != "" && f.forUser != userUUID {
		return false, nil
	}
	return true, nil
}

func (f *fakeAuthz) GetEffectivePermissions(_ context.Context, _, _ string) ([]string, error) {
	return nil, nil
}

func (f *fakeAuthz) RegisterPermissions(_ context.Context, _ []iface.PermissionSpec) error {
	return nil
}

type fakeTenantProvider struct {
	tenants map[string]*iface.Tenant
}

func (f *fakeTenantProvider) GetTenant(_ context.Context, tenantUUID string) (*iface.Tenant, error) {
	if t, ok := f.tenants[tenantUUID]; ok {
		return t, nil
	}
	return nil, iface.ErrKMSKeyNotFound // any non-nil error works for the test
}

func (f *fakeTenantProvider) ListUserMemberships(_ context.Context, _ string) ([]iface.TenantMembership, error) {
	return nil, nil
}
func (f *fakeTenantProvider) IsMember(_ context.Context, _, _ string) (bool, error) {
	return false, nil
}
func (f *fakeTenantProvider) HasCapability(_ context.Context, _, _ string) (bool, error) {
	return false, nil
}
func (f *fakeTenantProvider) GrantCapability(_ context.Context, _ iface.GrantCapabilityInput) error {
	return nil
}
func (f *fakeTenantProvider) RevokeCapability(_ context.Context, _, _ string) error { return nil }
func (f *fakeTenantProvider) ListCapabilityIDs(_ context.Context, _ string) ([]string, error) {
	return nil, nil
}
func (f *fakeTenantProvider) ActivateTenant(_ context.Context, _ string) error { return nil }
func (f *fakeTenantProvider) SetTenantStripeCustomerID(_ context.Context, _, _ string) error {
	return nil
}
func (f *fakeTenantProvider) EnsureTenantForUser(_ context.Context, _ string) (*iface.Tenant, error) {
	return nil, nil
}

type capturingSink struct {
	events []iface.AuditEvent
}

func (c *capturingSink) Emit(_ context.Context, event iface.AuditEvent) {
	c.events = append(c.events, event)
}

// --- Helper: minimal middleware fixture -------------------------------------

func newTestMiddleware(authz iface.AuthzProvider, tenant iface.TenantProvider, sink iface.AuditSink) *AuthMiddleware {
	logger := slog.New(slog.NewTextHandler(io.Discard, &slog.HandlerOptions{Level: slog.LevelError}))
	m := &AuthMiddleware{
		errorManager: sharedErrors.NewManager(logger, true),
		authz:        authz,
		tenant:       tenant,
	}
	if sink != nil {
		m.SetAuditSink(sink)
	}
	return m
}

func baseClaims(userUUID string, memberships []authModels.TenantMembership) *authModels.JWTClaims {
	return &authModels.JWTClaims{
		UserUUID:    userUUID,
		Email:       "admin@example.com",
		SystemRole:  "administrator",
		Memberships: memberships,
	}
}

// --- Tests ------------------------------------------------------------------

func TestSetUserContext_NonMember_WithoutSystemAdmin_Rejects(t *testing.T) {
	m := newTestMiddleware(&fakeAuthz{allow: false}, &fakeTenantProvider{}, nil)
	claims := baseClaims("user-1", nil)

	req := httptest.NewRequest(http.MethodGet, "/v1/anything", nil)
	req.Header.Set(TenantIDHeader, "tenant-X")
	rec := httptest.NewRecorder()

	called := false
	m.setUserContext(rec, req, claims, http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {
		called = true
	}))

	if called {
		t.Fatalf("downstream handler must not run when non-member header is rejected")
	}
	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", rec.Code)
	}
}

func TestSetUserContext_NonMember_WithSystemAdmin_Impersonates(t *testing.T) {
	authz := &fakeAuthz{allow: true}
	// IsCompany=true → business path (no MFA gate, .business audit action).
	tenants := &fakeTenantProvider{
		tenants: map[string]*iface.Tenant{
			"tenant-X": {UUID: "tenant-X", Kind: iface.TenantKindExternal, Name: "ACME", Slug: "acme", IsCompany: true},
		},
	}
	sink := &capturingSink{}
	m := newTestMiddleware(authz, tenants, sink)
	claims := baseClaims("admin-1", nil)

	req := httptest.NewRequest(http.MethodGet, "/v1/customers", nil)
	req.Header.Set(TenantIDHeader, "tenant-X")
	rec := httptest.NewRecorder()

	var seen context.Context
	m.setUserContext(rec, req, claims, http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		seen = r.Context()
	}))

	if seen == nil {
		t.Fatalf("handler must run when admin impersonation resolves the tenant")
	}
	tenantID, ok := ctxauth.GetTenantID(seen)
	if !ok || tenantID != "tenant-X" {
		t.Fatalf("expected tenantID=tenant-X, got %q ok=%v", tenantID, ok)
	}
	if kind := ctxauth.TenantKindFromContext(seen); kind != iface.TenantKindExternal {
		t.Fatalf("expected external kind, got %q", kind)
	}
	if !ctxauth.IsImpersonating(seen) {
		t.Fatalf("expected ctxauth.IsImpersonating(ctx)=true")
	}
	if len(sink.events) != 1 {
		t.Fatalf("expected 1 audit event, got %d", len(sink.events))
	}
	if got := sink.events[0].Action; got != auditActionImpersonateBusiness {
		t.Fatalf("expected %q for IsCompany=true target, got %q", auditActionImpersonateBusiness, got)
	}
	if sink.events[0].ResourceID != "tenant-X" || sink.events[0].ActorUserID != "admin-1" {
		t.Fatalf("audit event fields mismatch: %+v", sink.events[0])
	}
}

func TestSetUserContext_PersonalTarget_WithoutMFA_DemandsStepUp(t *testing.T) {
	authz := &fakeAuthz{allow: true}
	tenants := &fakeTenantProvider{
		tenants: map[string]*iface.Tenant{
			"tenant-P": {
				UUID:          "tenant-P",
				Kind:          iface.TenantKindExternal,
				Name:          "alice",
				Slug:          "alice",
				SignupChannel: iface.SignupChannelSelfServe,
			},
		},
	}
	sink := &capturingSink{}
	m := newTestMiddleware(authz, tenants, sink)
	claims := baseClaims("admin-1", nil) // no AMR — pwd-only session

	req := httptest.NewRequest(http.MethodGet, "/v1/anything", nil)
	req.Header.Set(TenantIDHeader, "tenant-P")
	rec := httptest.NewRecorder()

	called := false
	m.setUserContext(rec, req, claims, http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {
		called = true
	}))

	if called {
		t.Fatalf("downstream handler must not run when personal-target MFA gate fails")
	}
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 step_up_required, got %d", rec.Code)
	}
	if got := rec.Header().Get("WWW-Authenticate"); got != `MFA error="step_up_required"` {
		t.Fatalf("expected step_up_required challenge, got %q", got)
	}
	if len(sink.events) != 0 {
		t.Fatalf("MFA-gated impersonation must not emit audit event, got %d", len(sink.events))
	}
}

func TestSetUserContext_PersonalTarget_WithMFA_ImpersonatesAndAuditsPersonal(t *testing.T) {
	authz := &fakeAuthz{allow: true}
	tenants := &fakeTenantProvider{
		tenants: map[string]*iface.Tenant{
			"tenant-P": {
				UUID:          "tenant-P",
				Kind:          iface.TenantKindExternal,
				Name:          "alice",
				Slug:          "alice",
				SignupChannel: iface.SignupChannelSelfServe,
			},
		},
	}
	sink := &capturingSink{}
	m := newTestMiddleware(authz, tenants, sink)
	claims := baseClaims("admin-1", nil)
	claims.AMR = []string{"pwd", "otp"}

	req := httptest.NewRequest(http.MethodGet, "/v1/anything", nil)
	req.Header.Set(TenantIDHeader, "tenant-P")
	rec := httptest.NewRecorder()

	var seen context.Context
	m.setUserContext(rec, req, claims, http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		seen = r.Context()
	}))

	if seen == nil {
		t.Fatalf("handler must run when personal-target impersonation passes the MFA gate")
	}
	if !ctxauth.IsImpersonating(seen) {
		t.Fatalf("expected ctxauth.IsImpersonating(ctx)=true")
	}
	if len(sink.events) != 1 {
		t.Fatalf("expected 1 audit event, got %d", len(sink.events))
	}
	if got := sink.events[0].Action; got != auditActionImpersonatePersonal {
		t.Fatalf("expected %q for personal target, got %q", auditActionImpersonatePersonal, got)
	}
	md := sink.events[0].Metadata
	if isCompany, _ := md["targetIsCompany"].(bool); isCompany {
		t.Fatalf("metadata.targetIsCompany must be false for personal target")
	}
	if ch, _ := md["targetSignupChannel"].(string); ch != iface.SignupChannelSelfServe {
		t.Fatalf("metadata.targetSignupChannel = %q, want %q", ch, iface.SignupChannelSelfServe)
	}
}

func TestSetUserContext_Member_PathUnchanged(t *testing.T) {
	m := newTestMiddleware(&fakeAuthz{allow: false}, &fakeTenantProvider{}, nil)
	claims := baseClaims("user-1", []authModels.TenantMembership{
		{TenantUUID: "tenant-Y", TenantKind: iface.TenantKindInternal, Roles: []string{"administrator"}},
	})

	req := httptest.NewRequest(http.MethodGet, "/v1/anything", nil)
	req.Header.Set(TenantIDHeader, "tenant-Y")
	rec := httptest.NewRecorder()

	var seen context.Context
	m.setUserContext(rec, req, claims, http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		seen = r.Context()
	}))

	if seen == nil {
		t.Fatalf("handler must run for a regular member")
	}
	if ctxauth.IsImpersonating(seen) {
		t.Fatalf("regular members must not be flagged as impersonating")
	}
	if tenantID, _ := ctxauth.GetTenantID(seen); tenantID != "tenant-Y" {
		t.Fatalf("expected tenantID=tenant-Y, got %q", tenantID)
	}
}

func TestImpersonationAudit_DedupesWithinTTL(t *testing.T) {
	authz := &fakeAuthz{allow: true}
	tenants := &fakeTenantProvider{
		tenants: map[string]*iface.Tenant{
			"tenant-X": {UUID: "tenant-X", Kind: iface.TenantKindExternal, Name: "ACME", Slug: "acme", IsCompany: true},
		},
	}
	sink := &capturingSink{}
	m := newTestMiddleware(authz, tenants, sink)
	m.impersonationDedupeTTL = time.Hour // prevent flaky dedupe under slow CI
	claims := baseClaims("admin-1", nil)

	for i := 0; i < 5; i++ {
		req := httptest.NewRequest(http.MethodGet, "/v1/whatever", nil)
		req.Header.Set(TenantIDHeader, "tenant-X")
		rec := httptest.NewRecorder()
		m.setUserContext(rec, req, claims, http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {}))
	}

	if len(sink.events) != 1 {
		t.Fatalf("expected dedupe to emit exactly 1 audit event across 5 impersonations, got %d", len(sink.events))
	}
}
