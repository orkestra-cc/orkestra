package services

import (
	"context"
	"errors"
	"testing"
	"time"

	authModels "github.com/orkestra/backend/internal/core/auth/models"
	"github.com/orkestra/backend/internal/core/auth/repository"
	"github.com/orkestra/backend/pkg/sdk/iface"
)

type sessionIdentityRefreshRepo struct {
	repository.RefreshTokenRepository
	createErr error
	created   *authModels.RefreshTokenDoc
	revoked   []string
}

func (r *sessionIdentityRefreshRepo) CreateRefreshToken(_ context.Context, doc *authModels.RefreshTokenDoc) error {
	if r.createErr != nil {
		return r.createErr
	}
	copy := *doc
	r.created = &copy
	return nil
}

func (r *sessionIdentityRefreshRepo) RevokeTokensBySession(_ context.Context, sessionID, _ string) error {
	r.revoked = append(r.revoked, sessionID)
	return nil
}

type sessionIdentitySessionRepo struct {
	repository.AuthSessionRepository
	createErr error
	created   *authModels.AuthSessionDoc
}

func (r *sessionIdentitySessionRepo) GetDeviceSessionHistory(context.Context, string, string, int) ([]*authModels.AuthSessionDoc, error) {
	return nil, nil
}

func (r *sessionIdentitySessionRepo) CreateSession(_ context.Context, doc *authModels.AuthSessionDoc) error {
	if r.createErr != nil {
		return r.createErr
	}
	copy := *doc
	r.created = &copy
	return nil
}

func newSessionIdentityPasswordAuth(t *testing.T, refresh *sessionIdentityRefreshRepo, sessions *sessionIdentitySessionRepo) (*PasswordAuthService, JWTService) {
	t.Helper()
	privateKey := testRSAKey()
	jwt, err := NewJWTServiceWithAudience(privateKey, &privateKey.PublicKey, "test", AudienceOperator, 15*time.Minute, 24*time.Hour)
	if err != nil {
		t.Fatalf("NewJWTServiceWithAudience: %v", err)
	}
	jwt.SetTenantProvider(gateTenantProvider{})
	return NewPasswordAuthService(PasswordAuthConfig{
		UserService:      newGateUserFake(),
		JWTService:       jwt,
		RefreshTokenRepo: refresh,
		AuthSessionRepo:  sessions,
		Logger:           silentLogger(),
	}), jwt
}

func TestPasswordIssueTokens_PreservesCanonicalSessionIdentity(t *testing.T) {
	refresh := &sessionIdentityRefreshRepo{}
	sessions := &sessionIdentitySessionRepo{}
	svc, jwt := newSessionIdentityPasswordAuth(t, refresh, sessions)

	response, err := svc.IssueLoginTokens(context.Background(), &iface.User{
		UUID:     "user-session-identity",
		Email:    "session@example.com",
		Role:     "operator",
		IsActive: true,
	}, "device-session-identity", "web", "127.0.0.1", []string{"pwd"}, 0)
	if err != nil {
		t.Fatalf("IssueLoginTokens: %v", err)
	}
	accessClaims, err := jwt.ValidateAccessToken(response.AccessToken)
	if err != nil {
		t.Fatalf("ValidateAccessToken: %v", err)
	}
	refreshClaims, err := jwt.ValidateRefreshToken(response.RefreshToken)
	if err != nil {
		t.Fatalf("ValidateRefreshToken: %v", err)
	}
	if refresh.created == nil || sessions.created == nil {
		t.Fatalf("expected persisted refresh and session records")
	}

	for name, got := range map[string]string{
		"access claim":  accessClaims.SessionID,
		"refresh claim": refreshClaims.SessionID,
		"refresh row":   refresh.created.SessionUUID,
		"session row":   sessions.created.UUID,
	} {
		if got != response.SessionID {
			t.Errorf("%s session id = %q, want response session id %q", name, got, response.SessionID)
		}
	}
}

func TestPasswordIssueTokens_RefreshPersistenceFailureReturnsNoTokens(t *testing.T) {
	refresh := &sessionIdentityRefreshRepo{createErr: errors.New("refresh store unavailable")}
	sessions := &sessionIdentitySessionRepo{}
	svc, _ := newSessionIdentityPasswordAuth(t, refresh, sessions)

	response, err := svc.IssueLoginTokens(context.Background(), &iface.User{UUID: "user-refresh-failure", Role: "operator", IsActive: true}, "device", "web", "127.0.0.1", []string{"pwd"}, 0)
	if err == nil {
		t.Fatal("IssueLoginTokens succeeded despite refresh persistence failure")
	}
	if response != nil {
		t.Fatalf("IssueLoginTokens returned usable tokens after refresh persistence failure")
	}
	if sessions.created != nil {
		t.Fatal("session persistence ran after refresh persistence failure")
	}
}

func TestPasswordIssueTokens_SessionPersistenceFailureRevokesCanonicalRefresh(t *testing.T) {
	refresh := &sessionIdentityRefreshRepo{}
	sessions := &sessionIdentitySessionRepo{createErr: errors.New("session store unavailable")}
	svc, _ := newSessionIdentityPasswordAuth(t, refresh, sessions)

	response, err := svc.IssueLoginTokens(context.Background(), &iface.User{UUID: "user-session-failure", Role: "operator", IsActive: true}, "device", "web", "127.0.0.1", []string{"pwd"}, 0)
	if err == nil {
		t.Fatal("IssueLoginTokens succeeded despite session persistence failure")
	}
	if response != nil {
		t.Fatal("IssueLoginTokens returned usable tokens after session persistence failure")
	}
	if refresh.created == nil {
		t.Fatal("refresh row was not created before session persistence failed")
	}
	if len(refresh.revoked) != 1 || refresh.revoked[0] != refresh.created.SessionUUID {
		t.Fatalf("rollback revoked sessions = %v, want [%q]", refresh.revoked, refresh.created.SessionUUID)
	}
}

func TestPasswordIssueTokens_PartialMFAChallengeCarriesSID(t *testing.T) {
	env := newGatesEnv(t, PolicyAudienceOperator, map[string]string{"mfaEnabled": "true"}, nil)
	user := env.hashedUser("password-mfa-session@example.com", "correct-horse-battery")
	user.Role = "administrator"
	factors := newFakeFactorRepo()
	if err := factors.Insert(context.Background(), &authModels.MFAFactorDoc{
		UUID: "factor-password-session", UserUUID: user.UUID, Type: authModels.MFAFactorTOTP,
	}); err != nil {
		t.Fatalf("Insert factor: %v", err)
	}
	challenges := newFakeMFAChallenge()
	env.auth.mfaFactorRepo = factors
	env.auth.mfaChallengeService = challenges

	response, err := env.auth.Login(context.Background(), LoginInput{
		Email: user.Email, Password: "correct-horse-battery", DeviceID: "password-mfa-device",
	})
	if err != nil {
		t.Fatalf("Login: %v", err)
	}
	if !response.RequiresMFA || response.MFAToken == "" {
		t.Fatalf("response = %+v, want partial MFA challenge", response)
	}
	challenge, err := challenges.Peek(context.Background(), response.MFAToken)
	if err != nil {
		t.Fatalf("Peek: %v", err)
	}
	if challenge.SessionID == "" {
		t.Fatal("partial password challenge has no pending session id")
	}
}
