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

// inactiveOAuthRepo records OAuth-provider writes while supplying the two
// lookup branches HandleOAuthCallbackWithLinking needs. Embedding the
// repository interface keeps irrelevant methods out of this focused test.
type inactiveOAuthRepo struct {
	repository.OAuthProviderRepository
	linked  *authModels.OAuthProviderDoc
	created int
	updated int
}

func (r *inactiveOAuthRepo) GetByProviderAndID(context.Context, authModels.OAuthProvider, string) (*authModels.OAuthProviderDoc, error) {
	return r.linked, nil
}

func (r *inactiveOAuthRepo) GetByUserUUID(context.Context, string) ([]*authModels.OAuthProviderDoc, error) {
	return nil, nil
}

func (r *inactiveOAuthRepo) CreateOAuthProvider(context.Context, *authModels.OAuthProviderDoc) error {
	r.created++
	return nil
}

func (r *inactiveOAuthRepo) UpdateLastUsed(context.Context, string) error {
	r.updated++
	return nil
}

func (r *inactiveOAuthRepo) UpdateOAuthTokens(context.Context, string, string, string, *time.Time, *time.Time, []string) error {
	r.updated++
	return nil
}

func (r *inactiveOAuthRepo) UpdateMetadata(context.Context, string, map[string]interface{}) error {
	r.updated++
	return nil
}

// ineligibleJWT delegates non-issuance methods to a real service but records
// every token-mint call, letting the table test prove the guard is first.
type ineligibleJWT struct {
	JWTService
	accessCalls  int
	refreshCalls int
}

func (j *ineligibleJWT) GenerateAccessTokenWithAMR(user *iface.User, amr []string, lastOTPAt int64) (string, error) {
	j.accessCalls++
	return j.JWTService.GenerateAccessTokenWithAMR(user, amr, lastOTPAt)
}

func (j *ineligibleJWT) GenerateRefreshToken(user *iface.User) (string, error) {
	j.refreshCalls++
	return j.JWTService.GenerateRefreshToken(user)
}

// ineligibleRefreshRepo records the only persistence methods full issuance
// may reach. Its embedded interface supplies the unused methods.
type ineligibleRefreshRepo struct {
	repository.RefreshTokenRepository
	created int
	revoked int
}

func (r *ineligibleRefreshRepo) RevokeTokensByDevice(context.Context, string, string, string) error {
	r.revoked++
	return nil
}

func (r *ineligibleRefreshRepo) CreateRefreshToken(context.Context, *authModels.RefreshTokenDoc) error {
	r.created++
	return nil
}

func TestHandleOAuthCallbackWithLinking_RejectsInactiveLinkedUser(t *testing.T) {
	users := newGateUserFake()
	inactive := activeUser("inactive-linked@example.com", "x")
	inactive.IsActive = false
	users.seed(inactive)
	repo := &inactiveOAuthRepo{linked: &authModels.OAuthProviderDoc{
		UUID: "link-1", UserUUID: inactive.UUID, Provider: authModels.OAuthProviderGoogle, ProviderID: "google-1",
	}}
	refresh := &ineligibleRefreshRepo{}
	svc := &authService{userService: users, oauthProviderRepo: repo, refreshTokenRepo: refresh}

	_, err := svc.HandleOAuthCallbackWithLinking(context.Background(), authModels.OAuthProviderGoogle, map[string]interface{}{
		"email": inactive.Email, "name": "Inactive", "provider_id": "google-1",
	}, nil, nil, &authModels.DeviceInfo{DeviceID: "device-1"})
	if !errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("err = %v, want ErrInvalidCredentials", err)
	}
	if refresh.created != 0 {
		t.Errorf("refresh rows = %d, want 0", refresh.created)
	}
}

func TestHandleOAuthCallbackWithLinking_RejectsInactiveEmailMatchedUser(t *testing.T) {
	users := newGateUserFake()
	inactive := activeUser("inactive-email@example.com", "x")
	inactive.IsActive = false
	users.seed(inactive)
	repo := &inactiveOAuthRepo{}
	refresh := &ineligibleRefreshRepo{}
	svc := &authService{userService: users, oauthProviderRepo: repo, refreshTokenRepo: refresh, policy: newPolicy(nil)}

	_, err := svc.HandleOAuthCallbackWithLinking(context.Background(), authModels.OAuthProviderGoogle, map[string]interface{}{
		"email": inactive.Email, "name": "Inactive", "provider_id": "google-2",
	}, nil, nil, &authModels.DeviceInfo{DeviceID: "device-1"})
	if !errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("err = %v, want ErrInvalidCredentials", err)
	}
	if repo.created != 0 || repo.updated != 0 {
		t.Errorf("provider writes = created:%d updated:%d, want 0", repo.created, repo.updated)
	}
	if refresh.created != 0 {
		t.Errorf("refresh rows = %d, want 0", refresh.created)
	}
}

func TestGenerateEnhancedTokenPair_RejectsIneligible(t *testing.T) {
	baseJWT, err := NewJWTServiceWithAudience(testRSAKey(), &testRSAKey().PublicKey, "test", "operator", time.Minute, time.Hour)
	if err != nil {
		t.Fatalf("new JWT service: %v", err)
	}

	for _, tc := range []struct {
		name string
		user *iface.User
	}{
		{name: "nil", user: nil},
		{name: "empty UUID", user: &iface.User{IsActive: true}},
		{name: "inactive", user: &iface.User{UUID: "inactive-user", IsActive: false}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			jwt := &ineligibleJWT{JWTService: baseJWT}
			refresh := &ineligibleRefreshRepo{}
			svc := &authService{
				jwtService:        jwt,
				refreshTokenRepo:  refresh,
				oauthProviderRepo: &inactiveOAuthRepo{},
			}

			_, err := svc.GenerateEnhancedTokenPair(context.Background(), tc.user, &authModels.DeviceInfo{DeviceID: "device-1"}, nil)
			if !errors.Is(err, ErrInvalidCredentials) {
				t.Fatalf("err = %v, want ErrInvalidCredentials", err)
			}
			if jwt.accessCalls != 0 || jwt.refreshCalls != 0 {
				t.Errorf("JWT calls = access:%d refresh:%d, want 0", jwt.accessCalls, jwt.refreshCalls)
			}
			if refresh.revoked != 0 || refresh.created != 0 {
				t.Errorf("persistence calls = revoke:%d create:%d, want 0", refresh.revoked, refresh.created)
			}
		})
	}
}

func TestValidateTokenEligibleUser_RejectsIneligible(t *testing.T) {
	for _, tc := range []struct {
		name string
		user *iface.User
	}{
		{name: "nil", user: nil},
		{name: "empty UUID", user: &iface.User{IsActive: true}},
		{name: "inactive", user: &iface.User{UUID: "inactive-user", IsActive: false}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if err := ValidateTokenEligibleUser(tc.user); !errors.Is(err, ErrInvalidCredentials) {
				t.Fatalf("err = %v, want ErrInvalidCredentials", err)
			}
		})
	}
}
