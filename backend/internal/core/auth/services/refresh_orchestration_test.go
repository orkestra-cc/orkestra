package services

// Phase 16: AuthService.RefreshTokensWithRiskAssessment orchestration
// tests. The repository-level rotation primitives (RotateWithFamily,
// RevokeFamily, replay detection via CAS) are already covered in
// refresh_rotation_test.go. This file exercises the SERVICE layer
// that strings them together: JWT validation → token-doc lookup →
// user lookup → mint new pair → atomic rotate → handle CAS-loss as
// replay.

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/orkestra-cc/orkestra-sdk/iface"
	authModels "github.com/orkestra/backend/internal/core/auth/models"
	"github.com/orkestra/backend/internal/core/auth/repository"
	userModels "github.com/orkestra/backend/internal/core/user/models"
	"github.com/orkestra/backend/internal/shared/utils"
)

// orchestrationEnv mirrors gatesEnv but wires AuthService instead of
// PasswordAuthService.
type orchestrationEnv struct {
	t       *testing.T
	users   *gateUserFake
	refresh *gateRefreshRepo
	oauth   *orchOAuthRepo
	jwt     JWTService
	auth    AuthService
}

func newOrchestrationEnv(t *testing.T) *orchestrationEnv {
	t.Helper()
	priv := testRSAKey()
	jwt, err := NewJWTServiceWithAudience(priv, &priv.PublicKey, "test", AudienceOperator, 15*time.Minute, 7*24*time.Hour)
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
	authSvc, err := NewAuthService(&AuthConfig{
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
	env.auth = authSvc
	return env
}

// orchOAuthRepo is the tiniest possible OAuth provider repo — the
// rotation flow only calls GetByUserUUID at the end of the happy
// path to populate the response. Returns no rows; the response
// payload still validates.
type orchOAuthRepo struct{}

func (orchOAuthRepo) CreateOAuthProvider(context.Context, *authModels.OAuthProviderDoc) error {
	return nil
}
func (orchOAuthRepo) LinkOAuthProvider(context.Context, string, *authModels.OAuthLink) error {
	return nil
}
func (orchOAuthRepo) GetByProviderAndID(context.Context, authModels.OAuthProvider, string) (*authModels.OAuthProviderDoc, error) {
	return nil, nil
}
func (orchOAuthRepo) GetByUserUUID(context.Context, string) ([]*authModels.OAuthProviderDoc, error) {
	return nil, nil
}
func (orchOAuthRepo) GetPrimaryProvider(context.Context, string) (*authModels.OAuthProviderDoc, error) {
	return nil, nil
}
func (orchOAuthRepo) UpdateLastUsed(context.Context, string) error { return nil }
func (orchOAuthRepo) SetPrimaryProvider(context.Context, string, authModels.OAuthProvider) error {
	return nil
}
func (orchOAuthRepo) UpdateRefreshToken(context.Context, string, string) error { return nil }
func (orchOAuthRepo) UpdateOAuthTokens(context.Context, string, string, string, *time.Time, *time.Time, []string) error {
	return nil
}
func (orchOAuthRepo) UnlinkProvider(context.Context, string, authModels.OAuthProvider) error {
	return nil
}
func (orchOAuthRepo) DeleteProvider(context.Context, string) error { return nil }
func (orchOAuthRepo) FindByEmail(context.Context, string) ([]*authModels.OAuthProviderDoc, error) {
	return nil, nil
}
func (orchOAuthRepo) ConsolidateProviders(context.Context, string, string) error { return nil }

// Compile-time guard.
var _ repository.OAuthProviderRepository = (*orchOAuthRepo)(nil)

// issueAndSeedRefresh mints a real refresh token via the JWT service
// and stores the corresponding token-doc in the fake repo so the
// refresh flow can find it. Returns the raw token + the doc.
func (e *orchestrationEnv) issueAndSeedRefresh(user *userModels.User, family string, opts ...func(*authModels.RefreshTokenDoc)) (string, *authModels.RefreshTokenDoc) {
	e.t.Helper()
	token, err := e.jwt.GenerateRefreshToken(user)
	if err != nil {
		e.t.Fatalf("GenerateRefreshToken: %v", err)
	}
	hash := utils.HashRefreshToken(token)
	doc := &authModels.RefreshTokenDoc{
		UUID:        uuid.NewString(),
		UserUUID:    user.UUID,
		Token:       hash,
		SessionUUID: "sess-A",
		DeviceID:    "dev-A",
		IPAddress:   "1.1.1.1",
		FamilyID:    family,
		ExpiresAt:   time.Now().Add(7 * 24 * time.Hour),
		IssuedAt:    time.Now(),
		CreatedAt:   time.Now(),
	}
	for _, opt := range opts {
		opt(doc)
	}
	e.refresh.seedRefreshDoc(hash, doc)
	return token, doc
}

func seededUser() *userModels.User {
	return activeUser("alice@example.com", "x")
}

// ===== Happy path =====

func TestRefreshTokensWithRiskAssessment_HappyPath_RotatesAndMintsNewPair(t *testing.T) {
	env := newOrchestrationEnv(t)
	user := seededUser()
	env.users.seed(user)

	rawRefresh, oldDoc := env.issueAndSeedRefresh(user, "fam-1")

	resp, err := env.auth.RefreshTokensWithRiskAssessment(context.Background(), rawRefresh, &authModels.SecurityContext{IPAddress: "2.2.2.2"})
	if err != nil {
		t.Fatalf("RefreshTokensWithRiskAssessment: %v", err)
	}
	if resp.AccessToken == "" || resp.RefreshToken == "" {
		t.Fatalf("response missing tokens: %+v", resp)
	}
	// Session UUID is preserved across rotation.
	if resp.SessionID != oldDoc.SessionUUID {
		t.Errorf("SessionID = %q, want %q (session preserved across rotation)", resp.SessionID, oldDoc.SessionUUID)
	}

	// Old hash is now revoked-rotated with SucceededBy populated.
	rotated, _ := env.refresh.GetByTokenAny(context.Background(), utils.HashRefreshToken(rawRefresh))
	if rotated == nil || !rotated.IsRevoked {
		t.Fatalf("old token must be revoked after rotation: %+v", rotated)
	}
	if rotated.RevokedReason != authModels.RevokeReasonRotated {
		t.Errorf("revoked reason = %q, want %q", rotated.RevokedReason, authModels.RevokeReasonRotated)
	}
	if rotated.SucceededBy == "" {
		t.Errorf("SucceededBy must be set on rotated row to walk the lineage")
	}

	// New row carries the same family.
	newDoc, _ := env.refresh.GetByTokenAny(context.Background(), resp.RefreshToken)
	if newDoc == nil {
		t.Fatalf("new refresh row not seeded under its hash")
	}
	if newDoc.FamilyID != oldDoc.FamilyID {
		t.Errorf("family drift: %q → %q (rotation must inherit)", oldDoc.FamilyID, newDoc.FamilyID)
	}
	// And the new access token validates under the same JWT service.
	claims, err := env.jwt.ValidateAccessToken(resp.AccessToken)
	if err != nil {
		t.Errorf("new access token must validate: %v", err)
	}
	if claims.UserUUID != user.UUID {
		t.Errorf("new access token UserUUID = %q, want %q", claims.UserUUID, user.UUID)
	}
}

// ===== Replay detection =====

func TestRefreshTokensWithRiskAssessment_ReplayOfRotatedToken_KillsFamily(t *testing.T) {
	env := newOrchestrationEnv(t)
	user := seededUser()
	env.users.seed(user)

	// First rotation marks the original as rotated.
	rawRefresh, oldDoc := env.issueAndSeedRefresh(user, "fam-replay")
	if _, err := env.auth.RefreshTokensWithRiskAssessment(context.Background(), rawRefresh, &authModels.SecurityContext{}); err != nil {
		t.Fatalf("first rotation: %v", err)
	}

	// Second use of the same (now rotated) token → replay.
	_, err := env.auth.RefreshTokensWithRiskAssessment(context.Background(), rawRefresh, &authModels.SecurityContext{})
	if !errors.Is(err, ErrRefreshTokenReplay) {
		t.Fatalf("got %v, want ErrRefreshTokenReplay", err)
	}

	// Every member of the family must now be revoked-replay.
	if rotated, _ := env.refresh.GetByTokenAny(context.Background(), utils.HashRefreshToken(rawRefresh)); rotated != nil {
		// Rotation already set it to RevokeReasonRotated; replay must
		// re-stamp the family but the original stays rotated. The
		// downstream new row produced in the first rotation is what
		// gets revoked under "replay_detected".
		_ = rotated // shape check; actual assertion below
	}
	// Inspect every row in the family — at least one must carry the
	// replay-detected reason after the second call.
	found := false
	env.refresh.mu.Lock()
	for _, d := range env.refresh.byHash {
		if d.FamilyID == oldDoc.FamilyID && d.RevokedReason == authModels.RevokeReasonReplayDetected {
			found = true
			break
		}
	}
	env.refresh.mu.Unlock()
	if !found {
		t.Errorf("expected at least one family member revoked with replay_detected reason")
	}
}

// ===== Error paths =====

func TestRefreshTokensWithRiskAssessment_ExpiredToken_ReturnsInvalid(t *testing.T) {
	env := newOrchestrationEnv(t)
	user := seededUser()
	env.users.seed(user)

	rawRefresh, _ := env.issueAndSeedRefresh(user, "fam-exp", func(d *authModels.RefreshTokenDoc) {
		d.ExpiresAt = time.Now().Add(-1 * time.Hour) // already expired
	})

	_, err := env.auth.RefreshTokensWithRiskAssessment(context.Background(), rawRefresh, &authModels.SecurityContext{})
	if !errors.Is(err, ErrInvalidRefreshToken) {
		t.Fatalf("got %v, want ErrInvalidRefreshToken for expired row", err)
	}
}

func TestRefreshTokensWithRiskAssessment_RevokedForLogout_ReturnsInvalidNotReplay(t *testing.T) {
	// A token revoked for any reason OTHER than "rotated" is treated as
	// invalid, NOT as a replay — logging out, password-change-driven
	// revocation, role-change revocation should not trigger family kill.
	env := newOrchestrationEnv(t)
	user := seededUser()
	env.users.seed(user)

	rawRefresh, _ := env.issueAndSeedRefresh(user, "fam-logout", func(d *authModels.RefreshTokenDoc) {
		d.IsRevoked = true
		d.RevokedReason = authModels.RevokeReasonLogout
	})

	_, err := env.auth.RefreshTokensWithRiskAssessment(context.Background(), rawRefresh, &authModels.SecurityContext{})
	if !errors.Is(err, ErrInvalidRefreshToken) {
		t.Fatalf("got %v, want ErrInvalidRefreshToken for logout-revoked row", err)
	}
	if errors.Is(err, ErrRefreshTokenReplay) {
		t.Fatalf("logout-revoked row must NOT trigger replay handling")
	}
}

func TestRefreshTokensWithRiskAssessment_UnknownToken_ReturnsInvalid(t *testing.T) {
	env := newOrchestrationEnv(t)
	user := seededUser()
	env.users.seed(user)

	// Mint a valid JWT but never seed the row in the repo.
	tok, err := env.jwt.GenerateRefreshToken(user)
	if err != nil {
		t.Fatalf("GenerateRefreshToken: %v", err)
	}
	_, err = env.auth.RefreshTokensWithRiskAssessment(context.Background(), tok, &authModels.SecurityContext{})
	if !errors.Is(err, ErrInvalidRefreshToken) {
		t.Fatalf("got %v, want ErrInvalidRefreshToken for unseeded token", err)
	}
}

func TestRefreshTokensWithRiskAssessment_MalformedJWT_Rejected(t *testing.T) {
	env := newOrchestrationEnv(t)
	_, err := env.auth.RefreshTokensWithRiskAssessment(context.Background(), "not-a-jwt", &authModels.SecurityContext{})
	if err == nil {
		t.Fatalf("malformed JWT must error")
	}
	// We don't strictly require ErrInvalidRefreshToken here — the JWT
	// parser returns its own error wrapped in fmt.Errorf. Just confirm
	// the call short-circuits.
}

func TestRefreshTokensWithRiskAssessment_UserDeleted_ReturnsError(t *testing.T) {
	env := newOrchestrationEnv(t)
	user := seededUser()
	// Note: do NOT seed the user. The token is valid but the user lookup
	// will fail.
	rawRefresh, _ := env.issueAndSeedRefresh(user, "fam-orphan")

	_, err := env.auth.RefreshTokensWithRiskAssessment(context.Background(), rawRefresh, &authModels.SecurityContext{})
	if err == nil {
		t.Fatalf("orphaned token (user deleted) must error")
	}
}

// ===== ValidateTokenWithRiskAssessment =====

func TestValidateTokenWithRiskAssessment_AcceptsValidToken(t *testing.T) {
	env := newOrchestrationEnv(t)
	user := seededUser()
	env.users.seed(user)
	tok, err := env.jwt.GenerateAccessToken(user)
	if err != nil {
		t.Fatalf("GenerateAccessToken: %v", err)
	}
	res, err := env.auth.ValidateTokenWithRiskAssessment(context.Background(), tok, &authModels.SecurityContext{})
	if err != nil {
		t.Fatalf("ValidateTokenWithRiskAssessment: %v", err)
	}
	if res == nil || !res.Valid {
		t.Fatalf("expected Valid=true, got %+v", res)
	}
	if res.Claims == nil || res.Claims.UserUUID != user.UUID {
		t.Errorf("claims.UserUUID = %v, want %q", res.Claims, user.UUID)
	}
}

func TestValidateTokenWithRiskAssessment_RejectsTampered(t *testing.T) {
	env := newOrchestrationEnv(t)
	user := seededUser()
	env.users.seed(user)
	tok, err := env.jwt.GenerateAccessToken(user)
	if err != nil {
		t.Fatalf("GenerateAccessToken: %v", err)
	}
	tampered := tok[:len(tok)-8] + "AAAAAAAA"
	res, err := env.auth.ValidateTokenWithRiskAssessment(context.Background(), tampered, &authModels.SecurityContext{})
	// Either the call returns an error OR the returned result reports
	// Valid=false — both are acceptable rejections of a tampered token.
	if err == nil && res != nil && res.Valid {
		t.Fatalf("tampered token must be rejected, got Valid=true")
	}
}

// suppress unused alias on iface (we don't need it directly here but
// keep the import explicit so future additions don't pay an import dance).
var _ iface.UserProvider = (*gateUserFake)(nil)
