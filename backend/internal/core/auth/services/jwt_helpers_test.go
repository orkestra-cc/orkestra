package services

// Phase 14a: cover the JWT helpers that previously sat at 0% — they
// run on every login (GenerateTokenPair / GenerateTokenPairWithAMR)
// or guard a degraded boot (IsEnabled). Worth pinning explicitly so a
// rename or signature drift surfaces in CI rather than at runtime.

import (
	"testing"
	"time"

	authModels "github.com/orkestra/backend/internal/core/auth/models"
	"github.com/orkestra/backend/pkg/sdk/iface"
)

func newTestJWT(t *testing.T, audience string) JWTService {
	t.Helper()
	priv := testRSAKey()
	svc, err := NewJWTServiceWithAudience(priv, &priv.PublicKey, "test", audience, 15*time.Minute, 7*24*time.Hour)
	if err != nil {
		t.Fatalf("NewJWTServiceWithAudience: %v", err)
	}
	svc.SetTenantProvider(gateTenantProvider{})
	return svc
}

// ===== GenerateTokenPair =====

func TestGenerateTokenPair_PopulatesEveryField(t *testing.T) {
	svc := newTestJWT(t, AudienceOperator)
	user := &iface.User{UUID: "u-1", Email: "alice@example.com", Role: "operator"}
	device := &authModels.DeviceInfo{DeviceID: "dev-A", Platform: "web"}
	sec := &authModels.SecurityContext{SessionID: "sess-A", IPAddress: "1.2.3.4"}

	pair, err := svc.GenerateTokenPair(user, device, sec)
	if err != nil {
		t.Fatalf("GenerateTokenPair: %v", err)
	}
	if pair == nil {
		t.Fatalf("expected non-nil pair")
	}
	if pair.AccessToken == "" || pair.RefreshToken == "" {
		t.Errorf("missing tokens: access=%q refresh=%q", pair.AccessToken, pair.RefreshToken)
	}
	if pair.TokenType != "Bearer" {
		t.Errorf("TokenType = %q, want Bearer", pair.TokenType)
	}
	if pair.SessionID != "sess-A" {
		t.Errorf("SessionID = %q, want sess-A", pair.SessionID)
	}
	if pair.DeviceID != "dev-A" {
		t.Errorf("DeviceID = %q, want dev-A", pair.DeviceID)
	}
	if pair.ExpiresIn != int64((15 * time.Minute).Seconds()) {
		t.Errorf("ExpiresIn = %d, want 900", pair.ExpiresIn)
	}
	if len(pair.Scope) == 0 {
		t.Errorf("Scope should not be empty, got %v", pair.Scope)
	}
	// Round-trip: the access token must validate cleanly with the same service.
	claims, err := svc.ValidateAccessToken(pair.AccessToken)
	if err != nil {
		t.Fatalf("ValidateAccessToken: %v", err)
	}
	if claims.UserUUID != "u-1" || claims.Email != "alice@example.com" {
		t.Errorf("claims mismatch: %+v", claims)
	}
	if claims.Audience != AudienceOperator {
		t.Errorf("audience drift: claim=%q, want %q", claims.Audience, AudienceOperator)
	}
}

func TestGenerateTokenPair_RefreshIsAcceptedByValidator(t *testing.T) {
	// The refresh token is presented to /refresh-cookie / /refresh and
	// must round-trip through ValidateRefreshToken with no surprise
	// extra fields. Pin it so the issuer + validator stay in sync.
	svc := newTestJWT(t, AudienceClient)
	user := &iface.User{UUID: "u-2", Role: "operator"}
	device := &authModels.DeviceInfo{DeviceID: "dev-B"}
	sec := &authModels.SecurityContext{SessionID: "sess-B"}

	pair, err := svc.GenerateTokenPair(user, device, sec)
	if err != nil {
		t.Fatalf("GenerateTokenPair: %v", err)
	}
	claims, err := svc.ValidateRefreshToken(pair.RefreshToken)
	if err != nil {
		t.Fatalf("ValidateRefreshToken: %v", err)
	}
	if claims.TokenType != "refresh" {
		t.Errorf("TokenType = %q, want refresh", claims.TokenType)
	}
	if claims.UserUUID != "u-2" {
		t.Errorf("UserUUID = %q", claims.UserUUID)
	}
	if claims.Audience != AudienceClient {
		t.Errorf("Audience = %q, want %q", claims.Audience, AudienceClient)
	}
}

// ===== GenerateTokenPairWithAMR =====

func TestGenerateTokenPairWithAMR_StampsAMRAndLastOTPOnAccessToken(t *testing.T) {
	svc := newTestJWT(t, AudienceOperator)
	user := &iface.User{UUID: "u-3", Role: "operator"}
	device := &authModels.DeviceInfo{DeviceID: "dev-C"}
	now := time.Now().Unix()
	sec := &authModels.SecurityContext{SessionID: "sess-C"}

	pair, err := svc.GenerateTokenPairWithAMR(user, device, sec, []string{"pwd", "otp"}, now)
	if err != nil {
		t.Fatalf("GenerateTokenPairWithAMR: %v", err)
	}
	claims, err := svc.ValidateAccessToken(pair.AccessToken)
	if err != nil {
		t.Fatalf("ValidateAccessToken: %v", err)
	}
	if len(claims.AMR) != 2 || claims.AMR[0] != "pwd" || claims.AMR[1] != "otp" {
		t.Errorf("AMR = %v, want [pwd otp]", claims.AMR)
	}
	if claims.LastOTPAt != now {
		t.Errorf("LastOTPAt = %d, want %d", claims.LastOTPAt, now)
	}
	// Defense-in-depth: the helper must not mutate the caller's
	// SecurityContext. The function comment promises a copy.
	if len(sec.AMR) != 0 {
		t.Errorf("caller's SecurityContext.AMR was mutated: %v", sec.AMR)
	}
	if sec.LastOTPAt != 0 {
		t.Errorf("caller's SecurityContext.LastOTPAt was mutated: %d", sec.LastOTPAt)
	}
}

func TestGenerateTokenPairWithAMR_PreservesSession(t *testing.T) {
	svc := newTestJWT(t, AudienceOperator)
	user := &iface.User{UUID: "u-session", Email: "session@example.com", Role: "operator"}
	device := &authModels.DeviceInfo{DeviceID: "device-session", Platform: "web"}
	security := &authModels.SecurityContext{SessionID: "session-canonical"}

	pair, err := svc.GenerateTokenPairWithAMR(user, device, security, []string{"pwd"}, 0)
	if err != nil {
		t.Fatalf("GenerateTokenPairWithAMR: %v", err)
	}
	accessClaims, err := svc.ValidateAccessToken(pair.AccessToken)
	if err != nil {
		t.Fatalf("ValidateAccessToken: %v", err)
	}
	refreshClaims, err := svc.ValidateRefreshToken(pair.RefreshToken)
	if err != nil {
		t.Fatalf("ValidateRefreshToken: %v", err)
	}
	if accessClaims.SessionID != "session-canonical" {
		t.Errorf("access SessionID = %q, want session-canonical", accessClaims.SessionID)
	}
	if refreshClaims.SessionID != accessClaims.SessionID {
		t.Errorf("refresh SessionID = %q, want %q", refreshClaims.SessionID, accessClaims.SessionID)
	}
}

func TestGenerateTokenPairWithAMR_RequiresSession(t *testing.T) {
	svc := newTestJWT(t, AudienceOperator)
	user := &iface.User{UUID: "u-required-session", Role: "operator"}
	device := &authModels.DeviceInfo{DeviceID: "device-required-session"}

	pair, err := svc.GenerateTokenPairWithAMR(user, device, &authModels.SecurityContext{}, []string{"pwd"}, 0)
	if err == nil {
		t.Fatalf("GenerateTokenPairWithAMR returned a token pair without a session id: %+v", pair)
	}
}

func TestSessionAwareTokenIssuance_RequiresSessionContext(t *testing.T) {
	svc := newTestJWT(t, AudienceOperator)
	user := &iface.User{UUID: "u-session-required", Role: "operator"}
	device := &authModels.DeviceInfo{DeviceID: "device-session-required"}

	for name, security := range map[string]*authModels.SecurityContext{
		"nil context":   nil,
		"empty session": {},
	} {
		t.Run(name, func(t *testing.T) {
			if pair, err := svc.GenerateTokenPair(user, device, security); err == nil || pair != nil {
				t.Fatalf("GenerateTokenPair = %+v, %v; want no pair and an error", pair, err)
			}
			if pair, err := svc.GenerateTokenPairWithAMR(user, device, security, []string{"pwd"}, 0); err == nil || pair != nil {
				t.Fatalf("GenerateTokenPairWithAMR = %+v, %v; want no pair and an error", pair, err)
			}
			if token, err := svc.GenerateAccessTokenForSessionWithAMR(user, device, security, []string{"pwd"}, 0); err == nil || token != "" {
				t.Fatalf("GenerateAccessTokenForSessionWithAMR = %q, %v; want no token and an error", token, err)
			}
		})
	}
}

func TestGenerateAccessTokenForSessionWithAMR_PreservesSession(t *testing.T) {
	svc := newTestJWT(t, AudienceOperator)
	user := &iface.User{UUID: "u-session-access", Role: "operator"}
	device := &authModels.DeviceInfo{DeviceID: "device-session-access"}
	security := &authModels.SecurityContext{SessionID: "session-access-canonical", AMR: []string{"oauth"}, LastOTPAt: 11}

	token, err := svc.GenerateAccessTokenForSessionWithAMR(user, device, security, []string{"pwd", "otp"}, 22)
	if err != nil {
		t.Fatalf("GenerateAccessTokenForSessionWithAMR: %v", err)
	}
	claims, err := svc.ValidateAccessToken(token)
	if err != nil {
		t.Fatalf("ValidateAccessToken: %v", err)
	}
	if claims.SessionID != "session-access-canonical" {
		t.Errorf("SessionID = %q, want session-access-canonical", claims.SessionID)
	}
	if len(claims.AMR) != 2 || claims.AMR[0] != "pwd" || claims.AMR[1] != "otp" || claims.LastOTPAt != 22 {
		t.Errorf("claims = %+v, want supplied AMR and last OTP timestamp", claims)
	}
	if len(security.AMR) != 1 || security.AMR[0] != "oauth" || security.LastOTPAt != 11 {
		t.Errorf("caller security context mutated: %+v", security)
	}
}

// ===== IsEnabled =====

func TestIsEnabled_TrueOnNormalConstruction(t *testing.T) {
	svc := newTestJWT(t, AudienceOperator)
	if !svc.IsEnabled() {
		t.Errorf("IsEnabled must report true when keys are configured")
	}
}
