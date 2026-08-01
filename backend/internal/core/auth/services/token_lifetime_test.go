package services

// Token lifetimes must come from configuration, not from literals.
//
// Four different numbers described the same session and none of them
// consulted the config: the refresh row expired in a hardcoded 7 days
// while the refresh JWT claimed JWT_REFRESH_TOKEN_EXPIRY (30 days by
// default), and every response reported `expiresIn: 900` regardless of
// JWT_ACCESS_TOKEN_EXPIRY or the admin-managed accessTokenTTL. A
// deployment that shortened the access-token TTL kept telling its SPA
// the token was good for 15 minutes, so the client refreshed too late
// and every session hit a burst of 401s.

import (
	"context"
	"testing"
	"time"

	authModels "github.com/orkestra/backend/internal/core/auth/models"
	"github.com/orkestra/backend/pkg/sdk/iface"
)

const (
	testAccessTTL  = 5 * time.Minute
	testRefreshTTL = 48 * time.Hour
)

// lifetimeEnv wires an AuthService whose JWT service carries
// deliberately non-default TTLs, so any surviving literal shows up.
func newLifetimeEnv(t *testing.T) *orchestrationEnv {
	t.Helper()
	priv := testRSAKey()
	jwt, err := NewJWTServiceWithAudience(priv, &priv.PublicKey, "test", AudienceOperator, testAccessTTL, testRefreshTTL)
	if err != nil {
		t.Fatalf("jwt: %v", err)
	}
	jwt.SetTenantProvider(gateTenantProvider{})

	env := &orchestrationEnv{
		t:       t,
		users:   newGateUserFake(),
		refresh: newGateRefreshRepo(),
		oauth:   &orchOAuthRepo{},
		jwt:     jwt,
	}
	svc, err := NewAuthService(&AuthConfig{
		UserService:       env.users,
		TenantProvider:    gateTenantProvider{},
		OAuthProviderRepo: env.oauth,
		RefreshTokenRepo:  env.refresh,
		AuthSessionRepo:   newGateSessionRepo(),
		JWTService:        jwt,
		FirstAdminClaimer: newGateClaimer(),
	})
	if err != nil {
		t.Fatalf("NewAuthService: %v", err)
	}
	env.auth = svc
	return env
}

func TestRefresh_ReportsConfiguredAccessTokenTTL(t *testing.T) {
	env := newLifetimeEnv(t)
	user := &iface.User{UUID: "u-ttl", Email: "ttl@example.com", Role: "operator", IsActive: true}
	env.users.seed(user)
	token, _ := env.issueAndSeedRefresh(user, "fam-ttl")

	resp, err := env.auth.RefreshTokensWithRiskAssessment(context.Background(), token, nil)
	if err != nil {
		t.Fatalf("refresh: %v", err)
	}

	want := int64(testAccessTTL.Seconds())
	if resp.ExpiresIn != want {
		t.Errorf("ExpiresIn = %d, want %d (the configured access TTL, not a literal)", resp.ExpiresIn, want)
	}
}

func TestRefresh_NewRowExpiresPerConfiguredRefreshTTL(t *testing.T) {
	env := newLifetimeEnv(t)
	user := &iface.User{UUID: "u-ttl2", Email: "ttl2@example.com", Role: "operator", IsActive: true}
	env.users.seed(user)
	token, _ := env.issueAndSeedRefresh(user, "fam-ttl2")

	resp, err := env.auth.RefreshTokensWithRiskAssessment(context.Background(), token, nil)
	if err != nil {
		t.Fatalf("refresh: %v", err)
	}

	// The fake keys rotated rows by the raw token the service handed it
	// (the real repo hashes on insert).
	doc, err := env.refresh.GetByTokenAny(context.Background(), resp.RefreshToken)
	if err != nil || doc == nil {
		t.Fatalf("rotated row not found: %v", err)
	}

	got := time.Until(doc.ExpiresAt)
	if got < testRefreshTTL-time.Minute || got > testRefreshTTL+time.Minute {
		t.Errorf("rotated refresh row expires in %s, want ~%s — the row must not outlive or undercut its own JWT", got, testRefreshTTL)
	}
}

func TestMintAccessTokenFromRefresh_ReportsConfiguredAccessTokenTTL(t *testing.T) {
	env := newLifetimeEnv(t)
	user := &iface.User{UUID: "u-ttl3", Email: "ttl3@example.com", Role: "operator", IsActive: true}
	env.users.seed(user)
	token, _ := env.issueAndSeedRefresh(user, "fam-ttl3")

	resp, err := env.auth.MintAccessTokenFromRefresh(context.Background(), token, &authModels.SecurityContext{})
	if err != nil {
		t.Fatalf("mint: %v", err)
	}

	want := int64(testAccessTTL.Seconds())
	if resp.ExpiresIn != want {
		t.Errorf("ExpiresIn = %d, want %d", resp.ExpiresIn, want)
	}
}

func TestJWTService_ExposesItsConfiguredLifetimes(t *testing.T) {
	priv := testRSAKey()
	svc, err := NewJWTServiceWithAudience(priv, &priv.PublicKey, "test", AudienceOperator, testAccessTTL, testRefreshTTL)
	if err != nil {
		t.Fatalf("jwt: %v", err)
	}

	if got := svc.AccessTokenTTL(context.Background()); got != testAccessTTL {
		t.Errorf("AccessTokenTTL = %s, want %s", got, testAccessTTL)
	}
	if got := svc.RefreshTokenTTL(); got != testRefreshTTL {
		t.Errorf("RefreshTokenTTL = %s, want %s", got, testRefreshTTL)
	}
}
