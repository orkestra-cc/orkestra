package services

import (
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/orkestra/backend/internal/core/auth/models"
	"github.com/orkestra/backend/pkg/sdk/iface"
)

// TestBuildTenantScopedClaims locks the security-critical injection: a dev
// token minted for a principal with no DB membership must carry the acting
// tenant (TenantFallbackID/ActingTenantID) AND a matching synthetic membership,
// so tenant-scoped reads (billing/documents) resolve a tenant from context.
func TestBuildTenantScopedClaims(t *testing.T) {
	now := time.Unix(1_000_000, 0)
	u := &iface.User{UUID: "dev-administrator-1", Email: "administrator@orkestra.dev", Role: "administrator"}

	c := buildTenantScopedClaims(u, "org-acme", "", []string{"administrator"}, now, time.Hour, "orkestra", "operator")

	if c.TenantFallbackID != "org-acme" || c.ActingTenantID != "org-acme" {
		t.Fatalf("acting tenant not set: fallback=%q acting=%q", c.TenantFallbackID, c.ActingTenantID)
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

// TestClaimsToMap_TenantFallbackWireKeyIsDtid pins the wire contract behind
// the TenantFallbackID rename (formerly DefaultTenantID): the Go identifier
// changed, but claimsToMap — the function that actually shapes what gets
// signed into a JWT — must still emit the claim under the legacy "dtid" key.
// Any regression here breaks every already-issued token.
func TestClaimsToMap_TenantFallbackWireKeyIsDtid(t *testing.T) {
	s := &jwtService{}
	claims := &models.JWTClaims{
		UserUUID:         "user-1",
		TenantFallbackID: "org-acme",
	}

	m := s.claimsToMap(claims)

	got, ok := m["dtid"]
	if !ok {
		t.Fatalf("claimsToMap: expected a %q key, got none. keys=%v", "dtid", m)
	}
	if got != "org-acme" {
		t.Fatalf("claimsToMap[%q] = %v, want %q", "dtid", got, "org-acme")
	}
	if _, ok := m["TenantFallbackID"]; ok {
		t.Fatalf("claimsToMap must not leak the Go identifier as a claim key")
	}
	if _, ok := m["DefaultTenantID"]; ok {
		t.Fatalf("claimsToMap must not leak the legacy Go identifier as a claim key")
	}
}

// TestMapToClaims_LegacyDtidPopulatesTenantFallbackID pins the read side of
// the same contract: a hand-built claim map shaped exactly like what an
// already-issued token (minted before this rename) produces — a bare "dtid"
// key — must still populate the renamed TenantFallbackID field. This is
// what makes the rename safe for tokens signed by the old code.
func TestMapToClaims_LegacyDtidPopulatesTenantFallbackID(t *testing.T) {
	s := &jwtService{}
	legacyShape := jwt.MapClaims{
		"sub":  "user-1",
		"dtid": "org-legacy",
	}

	claims := s.mapToClaims(legacyShape)

	if claims.TenantFallbackID != "org-legacy" {
		t.Fatalf("mapToClaims: TenantFallbackID = %q, want %q (from legacy %q claim)",
			claims.TenantFallbackID, "org-legacy", "dtid")
	}
}

// TestBuildTenantScopedClaims_NoAuthTimeButCarriesMFAEpoch is the second arm
// of the machine-mint exclusion tripwire — the first is
// TestAuthTime_NotStampedByTheMachineMints, which covers GenerateAccessToken.
// Ruling R3 excludes auth_time from BOTH signers, and this is the one the
// dev-token endpoint takes by default whenever a tenant resolves
// (devtoken.go's GenerateAccessTokenForTenant branch), so leaving it
// uncovered would leave the exclusion half-guarded exactly where it is most
// likely to be violated.
//
// mfae is the opposite call and is asserted here as present: it proves
// nothing on its own and is only ever consulted for a token that already
// carries an MFA marker, which these never do.
func TestBuildTenantScopedClaims_NoAuthTimeButCarriesMFAEpoch(t *testing.T) {
	now := time.Unix(1_000_000, 0)
	u := &iface.User{UUID: "dev-1", Email: "x@orkestra.dev", Role: "administrator", MFAEpoch: 6}

	c := buildTenantScopedClaims(u, "org-acme", "", []string{"administrator"}, now, time.Hour, "orkestra", "operator")

	if c.AuthTime != 0 {
		t.Errorf("AuthTime = %d, want 0 — a synthetic dev-token principal proves no interactive presence, and a freshness window here would satisfy the first-factor enrolment gate", c.AuthTime)
	}
	if c.MFAEpoch != 6 {
		t.Errorf("MFAEpoch = %d, want 6 — the epoch is a fact about the subject and must not go missing on this mint", c.MFAEpoch)
	}
}
