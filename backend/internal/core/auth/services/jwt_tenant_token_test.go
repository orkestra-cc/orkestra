package services

import (
	"testing"
	"time"

	"github.com/orkestra/backend/pkg/sdk/iface"
)

// TestBuildTenantScopedClaims locks the security-critical injection: a dev
// token minted for a principal with no DB membership must carry the acting
// tenant (DefaultTenantID/ActingTenantID) AND a matching synthetic membership,
// so tenant-scoped reads (billing/documents) resolve a tenant from context.
func TestBuildTenantScopedClaims(t *testing.T) {
	now := time.Unix(1_000_000, 0)
	u := &iface.User{UUID: "dev-administrator-1", Email: "administrator@orkestra.dev", Role: "administrator"}

	c := buildTenantScopedClaims(u, "org-acme", "", []string{"administrator"}, now, time.Hour, "orkestra", "operator")

	if c.DefaultTenantID != "org-acme" || c.ActingTenantID != "org-acme" {
		t.Fatalf("acting tenant not set: default=%q acting=%q", c.DefaultTenantID, c.ActingTenantID)
	}
	if c.ActingTenantKind != "internal" {
		t.Fatalf("empty kind should default to internal, got %q", c.ActingTenantKind)
	}
	if len(c.Memberships) != 1 {
		t.Fatalf("want exactly one synthetic membership, got %d", len(c.Memberships))
	}
	m := c.Memberships[0]
	if m.TenantUUID != "org-acme" || m.TenantKind != "internal" || len(m.Roles) != 1 || m.Roles[0] != "administrator" {
		t.Fatalf("synthetic membership wrong: %+v", m)
	}
	if c.UserUUID != u.UUID || c.SystemRole != "administrator" || c.Email != u.Email {
		t.Fatalf("user fields wrong: %+v", c)
	}
	if c.TokenType != "access" || c.Issuer != "orkestra" || c.Audience != "operator" {
		t.Fatalf("token meta wrong: type=%q iss=%q aud=%q", c.TokenType, c.Issuer, c.Audience)
	}
	if c.ExpiresAt != now.Add(time.Hour).Unix() || c.IssuedAt != now.Unix() {
		t.Fatalf("timestamps wrong: exp=%d iat=%d", c.ExpiresAt, c.IssuedAt)
	}
}

// TestBuildTenantScopedClaims_ExplicitKind — an explicit kind (e.g. external)
// is preserved, not overridden by the internal default.
func TestBuildTenantScopedClaims_ExplicitKind(t *testing.T) {
	now := time.Unix(1_000_000, 0)
	u := &iface.User{UUID: "dev-1", Email: "x@orkestra.dev", Role: "operator"}
	c := buildTenantScopedClaims(u, "client-1", "external", []string{"operator"}, now, time.Hour, "orkestra", "client")
	if c.ActingTenantKind != "external" || c.Memberships[0].TenantKind != "external" {
		t.Fatalf("explicit kind not preserved: %q / %q", c.ActingTenantKind, c.Memberships[0].TenantKind)
	}
}
