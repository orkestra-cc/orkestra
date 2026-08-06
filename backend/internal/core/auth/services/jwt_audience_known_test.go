package services

// Token validation accepted ANY non-empty `aud`.
//
// The comment at the check promised "defense in depth" against a caller
// that bypasses the mux-level audience gate, but the code only asserted
// the claim was present — `aud: "anything"` sailed through. Note this
// validator deliberately does NOT pin a single audience: one
// AuthMiddleware, holding one JWT service, guards both the operator and
// the client mux (cmd/server/main.go), so requiring equality with the
// minting audience would lock one whole tier out. What it can and must
// do is refuse audiences the platform never issues.

import (
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/orkestra/backend/pkg/sdk/iface"
)

// mintWithAudience signs an otherwise-valid access token carrying an
// arbitrary aud claim, using the service's own key so only the audience
// check can reject it.
func mintWithAudience(t *testing.T, svc JWTService, audience string) string {
	t.Helper()
	impl, ok := svc.(*jwtService)
	if !ok {
		t.Fatalf("unexpected JWTService implementation %T", svc)
	}
	now := time.Now()
	claims := jwt.MapClaims{
		"sub":  "u-1",
		"type": "access",
		"iss":  impl.issuer,
		"iat":  now.Unix(),
		"exp":  now.Add(15 * time.Minute).Unix(),
	}
	if audience != "" {
		claims["aud"] = audience
	}
	token := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
	signed, err := token.SignedString(impl.privateKey)
	if err != nil {
		t.Fatalf("sign: %v", err)
	}
	return signed
}

func newAudienceTestService(t *testing.T) JWTService {
	t.Helper()
	priv := testRSAKey()
	svc, err := NewJWTServiceWithAudience(priv, &priv.PublicKey, "test", AudienceOperator, 0, 0)
	if err != nil {
		t.Fatalf("NewJWTServiceWithAudience: %v", err)
	}
	svc.SetTenantProvider(gateTenantProvider{})
	return svc
}

func TestValidateAccessToken_RejectsUnknownAudience(t *testing.T) {
	svc := newAudienceTestService(t)

	for _, aud := range []string{"anything", "orkestra", "OPERATOR", "operator "} {
		t.Run(aud, func(t *testing.T) {
			if _, err := svc.ValidateAccessToken(mintWithAudience(t, svc, aud)); err == nil {
				t.Errorf("audience %q is not one the platform issues and must be rejected", aud)
			}
		})
	}
}

func TestValidateAccessToken_AcceptsEveryIssuedAudience(t *testing.T) {
	// One middleware instance serves both muxes, so the validator must
	// keep accepting both tiers; the mux-level RequireAudience is what
	// pins a request to its surface.
	svc := newAudienceTestService(t)

	for _, aud := range []string{AudienceOperator, AudienceClient, AudienceService} {
		t.Run(aud, func(t *testing.T) {
			if _, err := svc.ValidateAccessToken(mintWithAudience(t, svc, aud)); err != nil {
				t.Errorf("audience %q is issued by this platform and must validate: %v", aud, err)
			}
		})
	}
}

func TestValidateAccessToken_StillRejectsMissingAudience(t *testing.T) {
	svc := newAudienceTestService(t)

	if _, err := svc.ValidateAccessToken(mintWithAudience(t, svc, "")); err == nil {
		t.Error("a token with no aud claim must stay rejected (ADR-0003 PR-D cutover)")
	}
}

func TestGeneratedTokensCarryAnIssuedAudience(t *testing.T) {
	// Guard the pairing: whatever the minters stamp must be in the set
	// the validator accepts.
	svc := newAudienceTestService(t)
	token, err := svc.GenerateAccessToken(&iface.User{UUID: "u-1", Email: "a@b.c", Role: "operator"})
	if err != nil {
		t.Fatalf("GenerateAccessToken: %v", err)
	}
	if _, err := svc.ValidateAccessToken(token); err != nil {
		t.Fatalf("a freshly minted token must validate: %v", err)
	}
}
