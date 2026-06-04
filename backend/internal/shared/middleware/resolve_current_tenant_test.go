package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	authModels "github.com/orkestra/backend/internal/core/auth/models"
	authServices "github.com/orkestra/backend/internal/core/auth/services"
	"github.com/orkestra/backend/pkg/sdk/iface"
)

// reqWithTenantHeader builds a GET request carrying X-Tenant-ID (omitted when
// header is empty).
func reqWithTenantHeader(header string) *http.Request {
	r := httptest.NewRequest(http.MethodGet, "/v1/anything", nil)
	if header != "" {
		r.Header.Set(TenantIDHeader, header)
	}
	return r
}

func tenantClaims(audience, actingTenant string, mbrs ...authModels.TenantMembership) *authModels.JWTClaims {
	return &authModels.JWTClaims{
		UserUUID:         "user-1",
		Audience:         audience,
		Memberships:      mbrs,
		DefaultTenantID:  actingTenant,
		ActingTenantID:   actingTenant,
		ActingTenantKind: iface.TenantKindInternal,
	}
}

// TestResolveCurrentTenant_OperatorSwitcher locks in the org-switcher fix: an
// operator-audience token may override its stamped ActingTenantID by sending
// X-Tenant-ID for another tenant it belongs to. Client-portal tokens stay
// pinned. A header naming a non-member tenant falls back to the default.
func TestResolveCurrentTenant_OperatorSwitcher(t *testing.T) {
	tenantA := authModels.TenantMembership{TenantUUID: "tenant-A", TenantKind: iface.TenantKindInternal, Roles: []string{"org_owner"}}
	tenantB := authModels.TenantMembership{TenantUUID: "tenant-B", TenantKind: iface.TenantKindInternal, Roles: []string{"org_admin"}}

	cases := []struct {
		name       string
		audience   string
		acting     string
		header     string
		wantTenant string
		wantOK     bool
	}{
		{
			name:       "operator header overrides acting tenant",
			audience:   authServices.AudienceOperator,
			acting:     "tenant-A",
			header:     "tenant-B",
			wantTenant: "tenant-B",
			wantOK:     true,
		},
		{
			name:       "operator without header keeps acting tenant",
			audience:   authServices.AudienceOperator,
			acting:     "tenant-A",
			header:     "",
			wantTenant: "tenant-A",
			wantOK:     true,
		},
		{
			name:       "operator header to non-member falls back to acting tenant",
			audience:   authServices.AudienceOperator,
			acting:     "tenant-A",
			header:     "tenant-X",
			wantTenant: "tenant-A",
			wantOK:     true,
		},
		{
			name:       "client token stays pinned and ignores header",
			audience:   authServices.AudienceClient,
			acting:     "tenant-A",
			header:     "tenant-B",
			wantTenant: "tenant-A",
			wantOK:     true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			claims := tenantClaims(tc.audience, tc.acting, tenantA, tenantB)
			gotTenant, gotRoles, gotKind, gotOK := resolveCurrentTenant(reqWithTenantHeader(tc.header), claims)
			if gotOK != tc.wantOK {
				t.Fatalf("ok = %v, want %v", gotOK, tc.wantOK)
			}
			if gotTenant != tc.wantTenant {
				t.Fatalf("tenant = %q, want %q", gotTenant, tc.wantTenant)
			}
			if gotOK && gotKind != iface.TenantKindInternal {
				t.Fatalf("kind = %q, want %q", gotKind, iface.TenantKindInternal)
			}
			// When the switcher selects tenant-B, the roles must come from that
			// membership (org_admin), not the default tenant's (org_owner).
			if tc.wantTenant == "tenant-B" && (len(gotRoles) != 1 || gotRoles[0] != "org_admin") {
				t.Fatalf("roles = %v, want [org_admin] for switched tenant", gotRoles)
			}
		})
	}
}
