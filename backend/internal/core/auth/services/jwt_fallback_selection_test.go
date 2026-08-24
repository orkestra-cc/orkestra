package services

// Task 6.2: operator tenant fallback selection. Before this task,
// loadMemberships picked the fallback tenant purely from the ordered
// membership list — first OWNED, else first membership. This task
// prepends the operational platform default (iface.DefaultTenantProvider,
// tenant module PR 3): when it names one of the user's valid memberships,
// it wins over an earlier-listed owned membership. It grants nothing on
// its own — a user who is not a member never receives it — and any
// failure (nil provider, no default assigned, or a provider ERROR) must
// fall straight through to the pre-existing owner-first rule so a
// transient platform-default read never blocks login.

import (
	"context"
	"errors"
	"testing"
	"time"

	authModels "github.com/orkestra/backend/internal/core/auth/models"
	"github.com/orkestra/backend/pkg/sdk/iface"
)

// fallbackMembershipFake is a minimal iface.TenantProvider whose
// ListUserMemberships returns a fixed, caller-supplied list (simulating the
// tenant repository's already-sorted output) or a fixed error. Every other
// method panics — the fallback-selection paths under test never call them.
type fallbackMembershipFake struct {
	mbrs []iface.TenantMembership
	err  error
}

func (f *fallbackMembershipFake) GetTenant(context.Context, string) (*iface.Tenant, error) {
	panic("not used")
}
func (f *fallbackMembershipFake) ListUserMemberships(context.Context, string) ([]iface.TenantMembership, error) {
	return f.mbrs, f.err
}
func (f *fallbackMembershipFake) IsMember(context.Context, string, string) (bool, error) {
	panic("not used")
}
func (f *fallbackMembershipFake) ActivateTenant(context.Context, string) error {
	panic("not used")
}
func (f *fallbackMembershipFake) SetTenantStripeCustomerID(context.Context, string, string) error {
	panic("not used")
}
func (f *fallbackMembershipFake) EnsureTenantForUser(context.Context, string) (*iface.Tenant, error) {
	panic("not used")
}

// fallbackDefaultTenantFake implements iface.DefaultTenantProvider with a
// caller-supplied fixed response, so a test can drive every combination the
// selection logic must tolerate: a real tenant, (nil, nil) ("no default
// assigned"), or a non-nil error.
type fallbackDefaultTenantFake struct {
	tenant *iface.Tenant
	err    error
}

func (f *fallbackDefaultTenantFake) GetDefaultTenant(context.Context) (*iface.Tenant, error) {
	return f.tenant, f.err
}

// newFallbackTestJWT builds an operator-audience JWT service with NO tenant
// provider and NO default-tenant provider wired — callers set exactly what
// their case needs, so a case that must prove "nil provider falls through"
// really does exercise a nil s.defaultTenants rather than a fake standing in
// for one.
func newFallbackTestJWT(t *testing.T) JWTService {
	t.Helper()
	priv := testRSAKey()
	svc, err := NewJWTServiceWithAudience(priv, &priv.PublicKey, "test", AudienceOperator, 15*time.Minute, 7*24*time.Hour)
	if err != nil {
		t.Fatalf("NewJWTServiceWithAudience: %v", err)
	}
	return svc
}

// mintAndValidate is the shared round trip: mint an access token for a
// fixed user/device/session, then validate it back into claims. Exercises
// the FULL public path (GenerateEnhancedAccessToken -> claimsToMap -> sign
// -> ValidateAccessToken -> mapToClaims), not just the unexported selection
// helper, so a wiring regression between the two would also be caught.
func mintAndValidate(t *testing.T, svc JWTService) *authModels.JWTClaims {
	t.Helper()
	user := &iface.User{UUID: "user-1", Email: "u@example.com", Role: "operator"}
	device := &authModels.DeviceInfo{DeviceID: "dev-1"}
	sec := &authModels.SecurityContext{SessionID: "sess-1"}
	token, err := svc.GenerateEnhancedAccessToken(user, device, sec)
	if err != nil {
		t.Fatalf("GenerateEnhancedAccessToken: %v", err)
	}
	claims, err := svc.ValidateAccessToken(token)
	if err != nil {
		t.Fatalf("ValidateAccessToken: %v", err)
	}
	return claims
}

// TestFallbackSelection_TableDriven covers every case the brief enumerates:
// platform default wins when it's a membership (even over an earlier owned
// one), falls through to owner-first when it's not a membership, falls
// through to first-membership when nothing is owned, and falls through on
// every provider failure mode (nil, no-default, error).
func TestFallbackSelection_TableDriven(t *testing.T) {
	mbrA := iface.TenantMembership{TenantUUID: "tenant-A", TenantKind: "internal", Roles: []string{"org_owner"}, IsOwner: true}
	mbrB := iface.TenantMembership{TenantUUID: "tenant-B", TenantKind: "internal", Roles: []string{"org_admin"}, IsOwner: false}
	mbrC := iface.TenantMembership{TenantUUID: "tenant-C", TenantKind: "external", Roles: []string{"org_member"}, IsOwner: false}

	cases := []struct {
		name         string
		mbrs         []iface.TenantMembership
		dt           iface.DefaultTenantProvider // nil = SetDefaultTenantProvider never called
		wantFallback string
		wantKind     string
	}{
		{
			name:         "platform default among memberships wins even over an earlier owned membership",
			mbrs:         []iface.TenantMembership{mbrA, mbrB, mbrC}, // mbrA is owned AND listed first
			dt:           &fallbackDefaultTenantFake{tenant: &iface.Tenant{UUID: "tenant-B", Kind: "internal"}},
			wantFallback: "tenant-B",
			wantKind:     "internal",
		},
		{
			name:         "platform default not a membership falls through to first owned",
			mbrs:         []iface.TenantMembership{mbrB, mbrA, mbrC}, // mbrA (owned) is not first in list order
			dt:           &fallbackDefaultTenantFake{tenant: &iface.Tenant{UUID: "tenant-ghost", Kind: "internal"}},
			wantFallback: "tenant-A",
			wantKind:     "internal",
		},
		{
			name:         "no owned membership falls through to first membership",
			mbrs:         []iface.TenantMembership{mbrC, mbrB}, // neither is owned
			dt:           &fallbackDefaultTenantFake{tenant: &iface.Tenant{UUID: "tenant-ghost", Kind: "internal"}},
			wantFallback: "tenant-C",
			wantKind:     "external",
		},
		{
			name:         "nil provider (never wired) falls through to owner-first rule",
			mbrs:         []iface.TenantMembership{mbrB, mbrA},
			dt:           nil,
			wantFallback: "tenant-A",
			wantKind:     "internal",
		},
		{
			name:         "provider returns (nil, nil) — no default assigned — falls through to owner-first rule",
			mbrs:         []iface.TenantMembership{mbrB, mbrA},
			dt:           &fallbackDefaultTenantFake{tenant: nil, err: nil},
			wantFallback: "tenant-A",
			wantKind:     "internal",
		},
		{
			name:         "provider ERROR falls through to owner-first rule without failing issuance",
			mbrs:         []iface.TenantMembership{mbrB, mbrA},
			dt:           &fallbackDefaultTenantFake{tenant: nil, err: errors.New("platform default lookup: boom")},
			wantFallback: "tenant-A",
			wantKind:     "internal",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			svc := newFallbackTestJWT(t)
			svc.SetTenantProvider(&fallbackMembershipFake{mbrs: tc.mbrs})
			if tc.dt != nil {
				svc.SetDefaultTenantProvider(tc.dt)
			}

			claims := mintAndValidate(t, svc)

			if claims.TenantFallbackID != tc.wantFallback {
				t.Fatalf("TenantFallbackID = %q, want %q", claims.TenantFallbackID, tc.wantFallback)
			}
			// The selected fallback must be stamped into BOTH the fallback
			// claim and the operator acting-tenant, with its kind —
			// preserves pre-existing operator-session behaviour.
			if claims.ActingTenantID != tc.wantFallback {
				t.Fatalf("ActingTenantID = %q, want %q", claims.ActingTenantID, tc.wantFallback)
			}
			if claims.ActingTenantKind != tc.wantKind {
				t.Fatalf("ActingTenantKind = %q, want %q", claims.ActingTenantKind, tc.wantKind)
			}
		})
	}
}

// TestFallbackSelection_NonMemberNeverReceivesPlatformDefault is the
// security-critical negative case, isolated from the table above for
// visibility: the platform default names a real tenant, but that tenant is
// NOT among the caller's memberships (empty membership list — e.g. a user
// with no tenant at all). Selection must never hand out a tenant the user
// doesn't belong to.
func TestFallbackSelection_NonMemberNeverReceivesPlatformDefault(t *testing.T) {
	svc := newFallbackTestJWT(t)
	svc.SetTenantProvider(&fallbackMembershipFake{mbrs: nil})
	svc.SetDefaultTenantProvider(&fallbackDefaultTenantFake{tenant: &iface.Tenant{UUID: "tenant-platform-default", Kind: "internal"}})

	claims := mintAndValidate(t, svc)

	if claims.TenantFallbackID != "" {
		t.Fatalf("TenantFallbackID = %q, want empty — user has no memberships, must not receive the platform default", claims.TenantFallbackID)
	}
	if claims.ActingTenantID != "" {
		t.Fatalf("ActingTenantID = %q, want empty", claims.ActingTenantID)
	}
	if len(claims.Memberships) != 0 {
		t.Fatalf("Memberships = %+v, want empty", claims.Memberships)
	}
}

// TestFallbackSelection_EmbeddedListPreservesProviderOrder pins that
// loadMemberships does NOT re-sort: the `mbr` claim embedded in the token
// must appear in exactly the order the provider (repository) returned it,
// so selection and the embedded list can never disagree about ordering.
func TestFallbackSelection_EmbeddedListPreservesProviderOrder(t *testing.T) {
	mbrs := []iface.TenantMembership{
		{TenantUUID: "tenant-Z", TenantKind: "internal", Roles: []string{"org_member"}},
		{TenantUUID: "tenant-A", TenantKind: "internal", Roles: []string{"org_member"}},
		{TenantUUID: "tenant-M", TenantKind: "external", Roles: []string{"org_member"}},
	}
	svc := newFallbackTestJWT(t)
	svc.SetTenantProvider(&fallbackMembershipFake{mbrs: mbrs})

	claims := mintAndValidate(t, svc)

	if len(claims.Memberships) != len(mbrs) {
		t.Fatalf("Memberships len = %d, want %d", len(claims.Memberships), len(mbrs))
	}
	want := []string{"tenant-Z", "tenant-A", "tenant-M"}
	for i, w := range want {
		if claims.Memberships[i].TenantUUID != w {
			t.Fatalf("Memberships order = %v, want %v (provider order, no re-sort)", membershipUUIDs(claims.Memberships), want)
		}
	}
}

func membershipUUIDs(mbrs []authModels.TenantMembership) []string {
	out := make([]string, len(mbrs))
	for i, m := range mbrs {
		out[i] = m.TenantUUID
	}
	return out
}

// TestFallbackSelection_JWTIsSnapshot_NotMutatedByLaterProviderChange pins
// the spec sentence: transferring the platform default does not revoke or
// rewrite already-issued tokens — they keep their prior fallback until
// refresh, reauthentication, or expiry. A NEW token minted after the
// transfer uses the new default (the user is a member of both tenants).
func TestFallbackSelection_JWTIsSnapshot_NotMutatedByLaterProviderChange(t *testing.T) {
	mbrs := []iface.TenantMembership{
		{TenantUUID: "tenant-A", TenantKind: "internal", Roles: []string{"org_owner"}, IsOwner: true},
		{TenantUUID: "tenant-B", TenantKind: "internal", Roles: []string{"org_admin"}, IsOwner: false},
	}
	dt := &fallbackDefaultTenantFake{tenant: &iface.Tenant{UUID: "tenant-A", Kind: "internal"}}
	svc := newFallbackTestJWT(t)
	svc.SetTenantProvider(&fallbackMembershipFake{mbrs: mbrs})
	svc.SetDefaultTenantProvider(dt)

	user := &iface.User{UUID: "user-1", Email: "u@example.com", Role: "operator"}
	device := &authModels.DeviceInfo{DeviceID: "dev-1"}
	sec := &authModels.SecurityContext{SessionID: "sess-1"}

	token1, err := svc.GenerateEnhancedAccessToken(user, device, sec)
	if err != nil {
		t.Fatalf("GenerateEnhancedAccessToken (1st): %v", err)
	}
	claims1, err := svc.ValidateAccessToken(token1)
	if err != nil {
		t.Fatalf("ValidateAccessToken (1st): %v", err)
	}
	if claims1.TenantFallbackID != "tenant-A" {
		t.Fatalf("first mint TenantFallbackID = %q, want tenant-A", claims1.TenantFallbackID)
	}

	// Transfer the platform default to tenant-B — the fake's underlying
	// state changes exactly like a live TransferDefaultTenant call would.
	dt.tenant = &iface.Tenant{UUID: "tenant-B", Kind: "internal"}

	// Re-validating the SAME already-issued token string must still yield
	// the OLD fallback. The JWT is a signed byte string; nothing about
	// re-validating it consults the provider again.
	claims1Again, err := svc.ValidateAccessToken(token1)
	if err != nil {
		t.Fatalf("ValidateAccessToken (1st, re-checked): %v", err)
	}
	if claims1Again.TenantFallbackID != "tenant-A" {
		t.Fatalf("already-issued token's fallback = %q after a later provider change, want tenant-A (JWT is a snapshot, not revoked/rewritten)", claims1Again.TenantFallbackID)
	}

	// A newly minted token, after the transfer, must use the NEW default.
	token2, err := svc.GenerateEnhancedAccessToken(user, device, sec)
	if err != nil {
		t.Fatalf("GenerateEnhancedAccessToken (2nd): %v", err)
	}
	claims2, err := svc.ValidateAccessToken(token2)
	if err != nil {
		t.Fatalf("ValidateAccessToken (2nd): %v", err)
	}
	if claims2.TenantFallbackID != "tenant-B" {
		t.Fatalf("second mint TenantFallbackID = %q, want tenant-B (new platform default)", claims2.TenantFallbackID)
	}
}
