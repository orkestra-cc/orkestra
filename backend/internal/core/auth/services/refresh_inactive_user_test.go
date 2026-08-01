package services

// Security regression tests: a deactivated account must not be able to
// keep minting access tokens.
//
// Login checks user.IsActive (password_auth_service.go), but that is the
// one path an already-signed-in attacker no longer needs. The refresh
// endpoints loaded the user only to populate JWT claims and never looked
// at IsActive — so deactivating an account (offboarding, a compromise
// response, or the inactiveAccountAutoDisableDays sweep) left the holder
// of a refresh token able to roll their session forward for the full
// refresh-token lifetime.

import (
	"context"
	"testing"

	"github.com/orkestra/backend/pkg/sdk/iface"
)

func TestRefreshTokensWithRiskAssessment_RejectsDeactivatedUser(t *testing.T) {
	env := newOrchestrationEnv(t)
	user := &iface.User{UUID: "u-deactivated", Email: "gone@example.com", Role: "operator", IsActive: true}
	env.users.seed(user)
	token, _ := env.issueAndSeedRefresh(user, "fam-1")

	// The account is disabled while the attacker holds a live refresh token.
	user.IsActive = false

	resp, err := env.auth.RefreshTokensWithRiskAssessment(context.Background(), token, nil)
	if err == nil {
		t.Fatal("a deactivated user must not be able to refresh")
	}
	if resp != nil {
		t.Errorf("no token pair may be minted for a deactivated user, got %+v", resp)
	}
}

func TestMintAccessTokenFromRefresh_RejectsDeactivatedUser(t *testing.T) {
	env := newOrchestrationEnv(t)
	user := &iface.User{UUID: "u-deactivated-2", Email: "gone2@example.com", Role: "operator", IsActive: true}
	env.users.seed(user)
	token, _ := env.issueAndSeedRefresh(user, "fam-2")

	user.IsActive = false

	resp, err := env.auth.MintAccessTokenFromRefresh(context.Background(), token, nil)
	if err == nil {
		t.Fatal("a deactivated user must not be able to bootstrap a session")
	}
	if resp != nil {
		t.Errorf("no access token may be minted for a deactivated user, got %+v", resp)
	}
}

func TestRefreshTokensWithRiskAssessment_AllowsActiveUser(t *testing.T) {
	// Guard against an over-broad fix that breaks the happy path.
	env := newOrchestrationEnv(t)
	user := &iface.User{UUID: "u-active", Email: "here@example.com", Role: "operator", IsActive: true}
	env.users.seed(user)
	token, _ := env.issueAndSeedRefresh(user, "fam-3")

	resp, err := env.auth.RefreshTokensWithRiskAssessment(context.Background(), token, nil)
	if err != nil {
		t.Fatalf("an active user must still be able to refresh: %v", err)
	}
	if resp == nil || resp.AccessToken == "" {
		t.Fatal("expected a fresh token pair for an active user")
	}
}
