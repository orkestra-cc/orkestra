package services

// Task 2 (service accounts, phase A): every interactive authentication
// surface must reject a service-principal user (iface.User.Kind ==
// iface.UserKindService) fail-closed. Service principals authenticate
// exclusively through the client-credentials grant (a future task); none
// of password login, refresh-token rotation, or OAuth token issuance may
// ever hand one a session.
//
// Fixtures mirror the existing package style verbatim:
//   - Login: newGatesEnv + gatesEnv.hashedUser (see gates_test.go)
//   - Refresh (all three read paths): newOrchestrationEnv +
//     issueAndSeedRefresh (see refresh_orchestration_test.go,
//     refresh_inactive_user_test.go)
//   - OAuth token pair: newOrchestrationEnv, calling
//     GenerateEnhancedTokenPair directly (see refresh_inactive_user_test.go
//     style — no HTTP/handler layer involved)

import (
	"context"
	"errors"
	"testing"

	"github.com/orkestra/backend/pkg/sdk/iface"
)

func TestLoginRejectsServicePrincipal(t *testing.T) {
	env := newGatesEnv(t, PolicyAudienceOperator, nil, nil)
	u := env.hashedUser("sa-bot@service.invalid", "irrelevant-but-well-formed")
	u.Kind = iface.UserKindService

	_, err := env.auth.Login(context.Background(), LoginInput{
		Email: "sa-bot@service.invalid", Password: "irrelevant-but-well-formed", IP: "127.0.0.1",
	})
	if !errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("err = %v, want ErrInvalidCredentials", err)
	}
}

func TestRefreshRejectsServicePrincipal(t *testing.T) {
	env := newOrchestrationEnv(t)
	user := &iface.User{UUID: "u-service", Email: "sa-bot@service.invalid", Role: "operator", IsActive: true, Kind: iface.UserKindService}
	env.users.seed(user)
	token, _ := env.issueAndSeedRefresh(user, "fam-service")

	resp, err := env.auth.RefreshTokensWithRiskAssessment(context.Background(), token, nil)
	if !errors.Is(err, ErrInvalidRefreshToken) {
		t.Fatalf("err = %v, want ErrInvalidRefreshToken", err)
	}
	if resp != nil {
		t.Errorf("no token pair may be minted for a service principal, got %+v", resp)
	}
}

func TestPeekRefreshTokenRejectsServicePrincipal(t *testing.T) {
	env := newOrchestrationEnv(t)
	user := &iface.User{UUID: "u-service-2", Email: "sa-bot2@service.invalid", Role: "operator", IsActive: true, Kind: iface.UserKindService}
	env.users.seed(user)
	token, _ := env.issueAndSeedRefresh(user, "fam-service-peek")

	_, err := env.auth.PeekRefreshToken(context.Background(), token)
	if !errors.Is(err, ErrInvalidRefreshToken) {
		t.Fatalf("err = %v, want ErrInvalidRefreshToken", err)
	}
}

func TestMintAccessTokenFromRefreshRejectsServicePrincipal(t *testing.T) {
	env := newOrchestrationEnv(t)
	user := &iface.User{UUID: "u-service-3", Email: "sa-bot3@service.invalid", Role: "operator", IsActive: true, Kind: iface.UserKindService}
	env.users.seed(user)
	token, _ := env.issueAndSeedRefresh(user, "fam-service-mint")

	resp, err := env.auth.MintAccessTokenFromRefresh(context.Background(), token, nil)
	if !errors.Is(err, ErrInvalidRefreshToken) {
		t.Fatalf("err = %v, want ErrInvalidRefreshToken", err)
	}
	if resp != nil {
		t.Errorf("no access token may be minted for a service principal, got %+v", resp)
	}
}

func TestOAuthTokenPairRejectsServicePrincipal(t *testing.T) {
	env := newOrchestrationEnv(t)
	_, err := env.auth.GenerateEnhancedTokenPair(context.Background(), &iface.User{UUID: "u1", IsActive: true, Kind: iface.UserKindService}, nil, nil)
	if !errors.Is(err, ErrUserInactive) {
		t.Fatalf("err = %v, want ErrUserInactive", err)
	}
}

// TestIssueLoginTokensRejectsServicePrincipal covers the exported
// iface.LoginTokenIssuer seam directly: a caller that reaches
// IssueLoginTokens (or its wrapper IssueLoginTokensExternal) without
// going through the guarded Login must still be refused for a service
// principal. Uses newGatesEnv so env.auth is a *PasswordAuthService —
// IssueLoginTokens takes the user object directly, no repo lookup, so no
// seeding is needed; the guard must trip before any refresh-token /
// session side effect runs.
func TestIssueLoginTokensRejectsServicePrincipal(t *testing.T) {
	env := newGatesEnv(t, PolicyAudienceOperator, nil, nil)
	user := &iface.User{UUID: "u-direct-service", Email: "sa-direct@service.invalid", Role: "operator", IsActive: true, Kind: iface.UserKindService}

	_, err := env.auth.IssueLoginTokens(context.Background(), user, "device-1", "web", "127.0.0.1", []string{"pwd"}, 0)
	if !errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("err = %v, want ErrInvalidCredentials", err)
	}
}

// TestIssueLoginTokensExternalRejectsServicePrincipal covers the
// iface.LoginTokenIssuer-shaped wrapper that extracted addons (e.g. the
// identity module's OIDC bridge) consume through the ServiceRegistry.
func TestIssueLoginTokensExternalRejectsServicePrincipal(t *testing.T) {
	env := newGatesEnv(t, PolicyAudienceOperator, nil, nil)
	user := &iface.User{UUID: "u-direct-service-2", Email: "sa-direct2@service.invalid", Role: "operator", IsActive: true, Kind: iface.UserKindService}

	_, err := env.auth.IssueLoginTokensExternal(context.Background(), user, "device-2", "web", "127.0.0.1", []string{"oauth"})
	if !errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("err = %v, want ErrInvalidCredentials", err)
	}
}
