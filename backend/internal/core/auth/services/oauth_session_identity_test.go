package services

import (
	"context"
	"errors"
	"testing"
	"time"

	authModels "github.com/orkestra/backend/internal/core/auth/models"
	"github.com/orkestra/backend/internal/core/auth/repository"
)

type oauthSessionIdentityRefreshRepo struct {
	repository.RefreshTokenRepository
	createErr error
	created   *authModels.RefreshTokenDoc
	revoked   []string
}

func (r *oauthSessionIdentityRefreshRepo) RevokeTokensByDevice(context.Context, string, string, string) error {
	return nil
}

func (r *oauthSessionIdentityRefreshRepo) CreateRefreshToken(_ context.Context, doc *authModels.RefreshTokenDoc) error {
	if r.createErr != nil {
		return r.createErr
	}
	copy := *doc
	r.created = &copy
	return nil
}

func (r *oauthSessionIdentityRefreshRepo) RevokeTokensBySession(_ context.Context, sessionID, _ string) error {
	r.revoked = append(r.revoked, sessionID)
	return nil
}

type oauthSessionIdentitySessionRepo struct {
	repository.AuthSessionRepository
	createErr error
	created   *authModels.AuthSessionDoc
}

func (r *oauthSessionIdentitySessionRepo) CreateSession(_ context.Context, doc *authModels.AuthSessionDoc) error {
	if r.createErr != nil {
		return r.createErr
	}
	copy := *doc
	r.created = &copy
	return nil
}

func TestOAuthSessionIdentity_PartialMFAChallengeCarriesSID(t *testing.T) {
	factors := newFakeFactorRepo()
	user := activeUser("oauth-mfa-session@example.com", "x")
	user.Role = "administrator"
	if err := factors.Insert(context.Background(), &authModels.MFAFactorDoc{
		UUID: "factor-session", UserUUID: user.UUID, Type: authModels.MFAFactorTOTP,
	}); err != nil {
		t.Fatalf("Insert factor: %v", err)
	}
	challenges := NewMFAChallengeService(NewMemoryOAuthStateStore())
	svc := &authService{
		mfaFactorRepo:       factors,
		mfaChallengeService: challenges,
	}

	response, err := svc.GenerateEnhancedTokenPair(context.Background(), user, &authModels.DeviceInfo{DeviceID: "oauth-mfa-device"}, nil)
	if err != nil {
		t.Fatalf("GenerateEnhancedTokenPair: %v", err)
	}
	if !response.RequiresMFA || response.MFAToken == "" {
		t.Fatalf("response = %+v, want partial MFA challenge", response)
	}
	challenge, err := challenges.Peek(context.Background(), response.MFAToken)
	if err != nil {
		t.Fatalf("Peek: %v", err)
	}
	if challenge.SessionID == "" {
		t.Fatal("partial OAuth challenge has no pending session id")
	}
}

func TestOAuthSessionIdentity_PreservesCanonicalSID(t *testing.T) {
	privateKey := testRSAKey()
	jwt, err := NewJWTServiceWithAudience(privateKey, &privateKey.PublicKey, "test", AudienceOperator, 15*time.Minute, 24*time.Hour)
	if err != nil {
		t.Fatalf("NewJWTServiceWithAudience: %v", err)
	}
	jwt.SetTenantProvider(gateTenantProvider{})

	users := newGateUserFake()
	user := activeUser("oauth-session@example.com", "x")
	users.seed(user)
	oauthRepo := &inactiveOAuthRepo{linked: &authModels.OAuthProviderDoc{
		UUID:       "oauth-link-session",
		UserUUID:   user.UUID,
		Provider:   authModels.OAuthProviderGoogle,
		ProviderID: "google-session",
	}}
	refreshRepo := &oauthSessionIdentityRefreshRepo{}
	sessionRepo := &oauthSessionIdentitySessionRepo{}
	svc := &authService{
		userService:       users,
		tenantProvider:    gateTenantProvider{},
		oauthProviderRepo: oauthRepo,
		refreshTokenRepo:  refreshRepo,
		authSessionRepo:   sessionRepo,
		jwtService:        jwt,
	}

	response, err := svc.HandleOAuthCallbackWithLinking(context.Background(), authModels.OAuthProviderGoogle, map[string]interface{}{
		"email":       user.Email,
		"name":        "OAuth Session",
		"provider_id": "google-session",
	}, nil, &authModels.SecurityContext{IPAddress: "203.0.113.10"}, &authModels.DeviceInfo{
		DeviceID:    "oauth-device",
		DeviceType:  "desktop",
		Platform:    "web",
		Fingerprint: "oauth-fingerprint",
	})
	if err != nil {
		t.Fatalf("HandleOAuthCallbackWithLinking: %v", err)
	}
	accessClaims, err := jwt.ValidateAccessToken(response.AccessToken)
	if err != nil {
		t.Fatalf("ValidateAccessToken: %v", err)
	}
	refreshClaims, err := jwt.ValidateRefreshToken(response.RefreshToken)
	if err != nil {
		t.Fatalf("ValidateRefreshToken: %v", err)
	}
	if refreshRepo.created == nil || sessionRepo.created == nil {
		t.Fatalf("expected persisted refresh and auth-session records")
	}

	for name, got := range map[string]string{
		"access claim":  accessClaims.SessionID,
		"refresh claim": refreshClaims.SessionID,
		"response":      response.SessionID,
		"refresh row":   refreshRepo.created.SessionUUID,
		"session row":   sessionRepo.created.UUID,
	} {
		if got != response.SessionID {
			t.Errorf("%s sid = %q, want %q", name, got, response.SessionID)
		}
	}
	if sessionRepo.created.LoginMethod != "oauth" {
		t.Errorf("session login method = %q, want oauth", sessionRepo.created.LoginMethod)
	}
	if response.SessionID == "" {
		t.Fatal("canonical session id is empty")
	}
	if len(refreshRepo.revoked) != 0 {
		t.Fatalf("unexpected refresh rollback: %v", refreshRepo.revoked)
	}
	if response.User == nil || response.User.ID != user.UUID {
		t.Fatalf("response user = %+v, want %q", response.User, user.UUID)
	}
}

func TestOAuthSessionIdentity_PersistenceFailuresReturnNoTokens(t *testing.T) {
	privateKey := testRSAKey()
	jwt, err := NewJWTServiceWithAudience(privateKey, &privateKey.PublicKey, "test", AudienceOperator, 15*time.Minute, 24*time.Hour)
	if err != nil {
		t.Fatalf("NewJWTServiceWithAudience: %v", err)
	}
	jwt.SetTenantProvider(gateTenantProvider{})
	user := activeUser("oauth-persistence@example.com", "x")

	for _, tc := range []struct {
		name         string
		refresh      *oauthSessionIdentityRefreshRepo
		sessions     *oauthSessionIdentitySessionRepo
		wantRollback bool
	}{
		{
			name:     "refresh row",
			refresh:  &oauthSessionIdentityRefreshRepo{createErr: errors.New("refresh persistence failed")},
			sessions: &oauthSessionIdentitySessionRepo{},
		},
		{
			name:         "session row",
			refresh:      &oauthSessionIdentityRefreshRepo{},
			sessions:     &oauthSessionIdentitySessionRepo{createErr: errors.New("session persistence failed")},
			wantRollback: true,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			svc := &authService{
				refreshTokenRepo:  tc.refresh,
				authSessionRepo:   tc.sessions,
				oauthProviderRepo: &inactiveOAuthRepo{},
				jwtService:        jwt,
			}
			response, err := svc.GenerateEnhancedTokenPair(context.Background(), user, &authModels.DeviceInfo{DeviceID: "persistence-device"}, nil)
			if err == nil {
				t.Fatal("GenerateEnhancedTokenPair succeeded despite persistence failure")
			}
			if response != nil {
				t.Fatalf("response = %+v, want no usable tokens", response)
			}
			if got := len(tc.refresh.revoked); (got == 1) != tc.wantRollback {
				t.Fatalf("refresh rollbacks = %v, want rollback=%v", tc.refresh.revoked, tc.wantRollback)
			}
		})
	}
}
