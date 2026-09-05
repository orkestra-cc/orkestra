package services

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	authModels "github.com/orkestra/backend/internal/core/auth/models"
	"github.com/orkestra/backend/internal/shared/utils"
	"github.com/orkestra/backend/pkg/sdk/iface"
)

// auth_time (D11) is stamped by every path that CREATES a session, and by
// nothing else. There are exactly two such mint sites:
//
//  1. issueTokensForSession — the shared chokepoint behind password login,
//     MFA login completion, passkey login completion, IssueLoginTokens /
//     IssueLoginTokensExternal, and the setup wizard's initial-admin mint
//     (RegisterInitialAdmin calls issueTokens, which delegates here);
//  2. HandleOAuthCallbackWithLinking — the OAuth callback's token pair,
//     which is also what the client-tier relay completion finishes into
//     (finishOAuthCompletion calls this same method on the target handler).
//
// The tests below drive one representative caller of each site. A gap here
// is a user who cannot enrol a first factor without an unexplained
// re-login.

func assertStampedNow(t *testing.T, authTime int64, what string) {
	t.Helper()
	if authTime == 0 {
		t.Fatalf("%s: auth_time must be stamped at session creation", what)
	}
	if delta := time.Since(time.Unix(authTime, 0)); delta > time.Minute || delta < -time.Minute {
		t.Fatalf("%s: auth_time is %v away from now", what, delta)
	}
}

// The login funnel. Reaching it through IssueLoginTokens exercises the same
// issueTokensForSession every interactive login path lands on.
func TestAuthTime_StampedByTheLoginFunnel(t *testing.T) {
	refresh := &sessionIdentityRefreshRepo{}
	sessions := &sessionIdentitySessionRepo{}
	svc, jwt := newSessionIdentityPasswordAuth(t, refresh, sessions)

	response, err := svc.IssueLoginTokens(context.Background(), &iface.User{
		UUID:     "user-auth-time",
		Email:    "authtime@example.com",
		Role:     "operator",
		IsActive: true,
		MFAEpoch: 5,
	}, "device-auth-time", "web", "127.0.0.1", []string{"pwd"}, 0)
	if err != nil {
		t.Fatalf("IssueLoginTokens: %v", err)
	}
	claims, err := jwt.ValidateAccessToken(response.AccessToken)
	if err != nil {
		t.Fatalf("ValidateAccessToken: %v", err)
	}
	assertStampedNow(t, claims.AuthTime, "login funnel")

	// mfae rides the same mint, read off the *iface.User the funnel
	// already holds — never re-read later, or a token would claim an
	// epoch that was not current when it was signed.
	if claims.MFAEpoch != 5 {
		t.Errorf("MFAEpoch = %d, want 5 (the user's epoch at login)", claims.MFAEpoch)
	}

	// The refresh token carries auth_time too. It is the only durable
	// record of the session's origin the rotation path can read: the
	// refresh row has no such column and the session row is only fetched
	// when the absolute-session cap happens to be enabled.
	refreshClaims, err := jwt.ValidateRefreshToken(response.RefreshToken)
	if err != nil {
		t.Fatalf("ValidateRefreshToken: %v", err)
	}
	if refreshClaims.AuthTime != claims.AuthTime {
		t.Fatalf("refresh auth_time = %d, want the access token's %d", refreshClaims.AuthTime, claims.AuthTime)
	}
}

// The OAuth callback mint — also the mint the client-tier relay completes
// into, since HandleOAuthRelayCompleteHTTP finishes through
// finishOAuthCompletion → HandleOAuthCallbackWithLinking.
func TestAuthTime_StampedByTheOAuthCallback(t *testing.T) {
	privateKey := testRSAKey()
	jwt, err := NewJWTServiceWithAudience(privateKey, &privateKey.PublicKey, "test", AudienceOperator, 15*time.Minute, 24*time.Hour)
	if err != nil {
		t.Fatalf("NewJWTServiceWithAudience: %v", err)
	}
	jwt.SetTenantProvider(gateTenantProvider{})

	users := newGateUserFake()
	user := activeUser("oauth-authtime@example.com", "x")
	user.MFAEpoch = 2
	users.seed(user)
	svc := &authService{
		userService:    users,
		tenantProvider: gateTenantProvider{},
		oauthProviderRepo: &inactiveOAuthRepo{linked: &authModels.OAuthProviderDoc{
			UUID:       "oauth-link-authtime",
			UserUUID:   user.UUID,
			Provider:   authModels.OAuthProviderGoogle,
			ProviderID: "google-authtime",
		}},
		refreshTokenRepo: &oauthSessionIdentityRefreshRepo{},
		authSessionRepo:  &oauthSessionIdentitySessionRepo{},
		jwtService:       jwt,
	}

	response, err := svc.HandleOAuthCallbackWithLinking(context.Background(), authModels.OAuthProviderGoogle, map[string]interface{}{
		"email":       user.Email,
		"name":        "OAuth AuthTime",
		"provider_id": "google-authtime",
	}, nil, &authModels.SecurityContext{IPAddress: "203.0.113.10"}, &authModels.DeviceInfo{
		DeviceID: "oauth-device", DeviceType: "desktop", Platform: "web",
	})
	if err != nil {
		t.Fatalf("HandleOAuthCallbackWithLinking: %v", err)
	}
	claims, err := jwt.ValidateAccessToken(response.AccessToken)
	if err != nil {
		t.Fatalf("ValidateAccessToken: %v", err)
	}
	assertStampedNow(t, claims.AuthTime, "oauth callback")
	if claims.MFAEpoch != 2 {
		t.Errorf("MFAEpoch = %d, want 2", claims.MFAEpoch)
	}
}

// seedRefreshCarryingAuthTime mints a real refresh token that carries an
// auth_time (as every post-login refresh token now does) and stores the
// matching row, so the rotation and bootstrap paths can be driven with a
// session whose origin is in the past.
func seedRefreshCarryingAuthTime(t *testing.T, e *orchestrationEnv, user *iface.User, authTime int64) string {
	t.Helper()
	token, err := e.jwt.GenerateEnhancedRefreshToken(user,
		&authModels.DeviceInfo{DeviceID: "dev-A"},
		&authModels.SecurityContext{SessionID: "sess-A", AuthTime: authTime})
	if err != nil {
		t.Fatalf("GenerateEnhancedRefreshToken: %v", err)
	}
	hash := utils.HashRefreshToken(token)
	e.refresh.seedRefreshDoc(hash, &authModels.RefreshTokenDoc{
		UUID:        uuid.NewString(),
		UserUUID:    user.UUID,
		Token:       hash,
		SessionUUID: "sess-A",
		DeviceID:    "dev-A",
		IPAddress:   "1.1.1.1",
		FamilyID:    "fam-authtime",
		ExpiresAt:   time.Now().Add(7 * 24 * time.Hour),
		IssuedAt:    time.Now(),
		CreatedAt:   time.Now(),
	})
	return token
}

// A refresh CARRIES auth_time: it describes the session's origin, not the
// token's. Re-stamping here would make freshness unfalsifiable — any client
// that refreshes on a timer would look permanently freshly authenticated.
func TestAuthTime_CarriedUnchangedByRefreshRotation(t *testing.T) {
	env := newOrchestrationEnv(t)
	user := &iface.User{UUID: "u-refresh-authtime", Email: "refresh-authtime@example.com", Role: "operator", IsActive: true}
	env.users.seed(user)
	origin := time.Now().Add(-2 * time.Hour).Unix()
	token := seedRefreshCarryingAuthTime(t, env, user, origin)

	resp, err := env.auth.RefreshTokensWithRiskAssessment(context.Background(), token, nil)
	if err != nil {
		t.Fatalf("refresh: %v", err)
	}
	claims, err := env.jwt.ValidateAccessToken(resp.AccessToken)
	if err != nil {
		t.Fatalf("ValidateAccessToken: %v", err)
	}
	if claims.AuthTime != origin {
		t.Fatalf("refresh AuthTime = %d, want the session's original %d — a refresh is not an authentication", claims.AuthTime, origin)
	}
	rotated, err := env.jwt.ValidateRefreshToken(resp.RefreshToken)
	if err != nil {
		t.Fatalf("ValidateRefreshToken: %v", err)
	}
	if rotated.AuthTime != origin {
		t.Fatalf("rotated refresh AuthTime = %d, want %d — the origin must survive every rotation", rotated.AuthTime, origin)
	}
}

// The non-rotating bootstrap mint (/session) has the same rule.
func TestAuthTime_CarriedUnchangedByMintAccessTokenFromRefresh(t *testing.T) {
	env := newOrchestrationEnv(t)
	user := &iface.User{UUID: "u-bootstrap-authtime", Email: "bootstrap-authtime@example.com", Role: "operator", IsActive: true}
	env.users.seed(user)
	origin := time.Now().Add(-3 * time.Hour).Unix()
	token := seedRefreshCarryingAuthTime(t, env, user, origin)

	resp, err := env.auth.MintAccessTokenFromRefresh(context.Background(), token, nil)
	if err != nil {
		t.Fatalf("MintAccessTokenFromRefresh: %v", err)
	}
	claims, err := env.jwt.ValidateAccessToken(resp.AccessToken)
	if err != nil {
		t.Fatalf("ValidateAccessToken: %v", err)
	}
	if claims.AuthTime != origin {
		t.Fatalf("bootstrap AuthTime = %d, want the session's original %d", claims.AuthTime, origin)
	}
}

// The machine mints must NOT stamp auth_time. Both the client-credentials
// grant and the dev-token endpoint reach the signer through
// GenerateAccessToken, so stamping "now" centrally would hand every service
// token a rolling freshness window and let it satisfy an enrolment gate no
// human presence ever backed.
func TestAuthTime_NotStampedByTheMachineMints(t *testing.T) {
	privateKey := testRSAKey()
	svc, err := NewJWTServiceWithAudience(privateKey, &privateKey.PublicKey, "test", AudienceOperator, 15*time.Minute, 24*time.Hour)
	if err != nil {
		t.Fatalf("NewJWTServiceWithAudience: %v", err)
	}
	svc.SetTenantProvider(gateTenantProvider{})

	token, err := svc.GenerateAccessToken(&iface.User{
		UUID: "svc-1", Email: "svc@example.com", Role: "operator",
		Kind: iface.UserKindService, IsActive: true,
	})
	if err != nil {
		t.Fatalf("GenerateAccessToken: %v", err)
	}
	claims, err := svc.ValidateAccessToken(token)
	if err != nil {
		t.Fatalf("ValidateAccessToken: %v", err)
	}
	if claims.AuthTime != 0 {
		t.Fatalf("AuthTime = %d, want 0 — a machine principal proves no interactive presence", claims.AuthTime)
	}
}
