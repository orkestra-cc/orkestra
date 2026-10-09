package services

// H-6 (spec §4.7 D30): the OAuth signup branch called ClaimFirstAdmin with
// no audience guard, and the claimer is wired into the CLIENT bundle
// (tier_bundle.go). So on a fresh install the first client-tier OAuth
// signup — an external customer — became the platform super_admin. The
// password path has had the guard all along; these are its twins.

import (
	"context"
	"errors"
	"testing"
	"time"

	authModels "github.com/orkestra/backend/internal/core/auth/models"
	"github.com/orkestra/backend/pkg/sdk/iface"
)

type oauthGateEnv struct {
	users   *gateUserFake
	claimer *gateClaimer
	auth    AuthService
}

// newOAuthGateService builds a real authService for one audience, with an
// open registration policy, over the gate fakes.
func newOAuthGateService(t *testing.T, aud PolicyAudience) (*oauthGateEnv, *gateClaimer) {
	t.Helper()
	priv := testRSAKey()
	jwt, err := NewJWTServiceWithAudience(priv, &priv.PublicKey, "test", string(aud), 15*time.Minute, 7*24*time.Hour)
	if err != nil {
		t.Fatalf("jwt: %v", err)
	}
	jwt.SetTenantProvider(gateTenantProvider{})
	env := &oauthGateEnv{users: newGateUserFake(), claimer: newGateClaimer()}
	authSvc, err := NewAuthService(&AuthConfig{
		UserService:       env.users,
		TenantProvider:    gateTenantProvider{},
		OAuthProviderRepo: &orchOAuthRepo{},
		RefreshTokenRepo:  newGateRefreshRepo(),
		AuthSessionRepo:   newGateSessionRepo(),
		JWTService:        jwt,
		FirstAdminClaimer: env.claimer,
	})
	if err != nil {
		t.Fatalf("NewAuthService: %v", err)
	}
	authSvc.SetAudience(aud)
	authSvc.SetPolicy(NewAuthPolicyServiceForTest(map[string]string{
		"registrationEnabledAdmin":  "true",
		"registrationEnabledClient": "true",
		"oauthAllowSignupAdmin":     "true",
		"oauthAllowSignupClient":    "true",
		"oauthAutoLinkByEmail":      "true",
	}))
	env.auth = authSvc
	return env, env.claimer
}

// completeOAuthSignup drives the web-callback application half for a
// brand-new, provider-verified identity and returns the account it created.
func completeOAuthSignup(t *testing.T, env *oauthGateEnv, email string) (*iface.User, error) {
	t.Helper()
	_, err := env.auth.HandleOAuthCallbackWithLinking(context.Background(), authModels.OAuthProviderGoogle,
		map[string]interface{}{
			"email": email, "name": "Signup User", "provider_id": "gid-" + email, "email_verified": true,
		}, nil, &authModels.SecurityContext{IPAddress: "1.1.1.1", Timestamp: time.Now()},
		&authModels.DeviceInfo{Platform: "web"}) // no DeviceID: the gate refresh fake has no device-dedup
	if err != nil {
		return nil, err
	}
	return env.users.GetUserForAuth(context.Background(), email)
}

func TestOAuthCallback_ClientFirstUser_NeverClaimsSuperAdmin(t *testing.T) {
	env, claimer := newOAuthGateService(t, PolicyAudienceClient)

	user, err := completeOAuthSignup(t, env, "someone@example.com")
	if err != nil {
		t.Fatalf("signup: %v", err)
	}
	if claimer.claimCalls() != 0 {
		t.Fatal("the client tier must never attempt the first-admin claim")
	}
	if user.Role == "super_admin" {
		t.Fatalf("role = %q — a client-tier signup must never be super_admin", user.Role)
	}
}

func TestOAuthCallback_OperatorFirstUser_StillClaims(t *testing.T) {
	env, claimer := newOAuthGateService(t, PolicyAudienceOperator)

	user, err := completeOAuthSignup(t, env, "founder@example.com")
	if err != nil {
		t.Fatalf("signup: %v", err)
	}
	if claimer.claimCalls() != 1 {
		t.Fatalf("claim calls = %d, want 1 — the operator tier must still claim on a fresh install", claimer.claimCalls())
	}
	if user.Role != "super_admin" {
		t.Fatalf("role = %q, want super_admin", user.Role)
	}
}

// A claim ERROR is fatal, as it is on the password path
// (password_auth_service.go: "claim first admin: %w"). Swallowing it is how
// a lost race becomes a silent guest — and how a genuinely broken sentinel
// store silently stops minting super_admins on an install that looks fresh.
func TestOAuthCallback_ClaimErrorIsFatal(t *testing.T) {
	env, claimer := newOAuthGateService(t, PolicyAudienceOperator)
	claimer.failWith(errors.New("mongo down"))

	if _, err := completeOAuthSignup(t, env, "founder@example.com"); err == nil {
		t.Fatal("a claim error must fail the signup, not fall through to guest")
	}
	if n := len(env.users.createdUsers); n != 0 {
		t.Fatalf("created users = %d, want 0 — nothing may be created on a claim error", n)
	}
}

// A LOST race is not an error: the sentinel is already taken, so the
// signup proceeds with the tier default.
func TestOAuthCallback_LostClaimRaceFallsBackToTheTierDefault(t *testing.T) {
	env, claimer := newOAuthGateService(t, PolicyAudienceOperator)
	claimer.alreadyClaimed()

	user, err := completeOAuthSignup(t, env, "second@example.com")
	if err != nil {
		t.Fatalf("signup: %v", err)
	}
	if user.Role != "guest" {
		t.Fatalf("role = %q, want the operator tier default 'guest'", user.Role)
	}
}

// The sentinel and the created user must carry the SAME uuid, or a
// rollback's Release (which deletes only a matching uuid) can never match
// and the sentinel is stranded.
func TestOAuthCallback_SentinelAndUserShareTheUUID(t *testing.T) {
	env, claimer := newOAuthGateService(t, PolicyAudienceOperator)
	user, err := completeOAuthSignup(t, env, "founder@example.com")
	if err != nil {
		t.Fatalf("signup: %v", err)
	}
	if claimer.claimedUUID() == "" || claimer.claimedUUID() != user.UUID {
		t.Fatalf("sentinel uuid %q != user uuid %q", claimer.claimedUUID(), user.UUID)
	}
}
