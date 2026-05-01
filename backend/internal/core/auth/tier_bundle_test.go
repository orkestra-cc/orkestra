package auth

import (
	"testing"

	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// TestBuildAuthTierBundlePicksMatchingConstructors covers the D-2
// invariant that the builder picks the operator-tier or client-tier
// repository constructor that matches d.tier and produces a non-nil
// AuthService + PasswordAuthService + RiskAssessmentService.
//
// The constructor → collection name binding itself is locked in by
// tier_guard_test.go in the repository package; this test only
// verifies that buildAuthTierBundle dispatches on tier and wires the
// bundle through. Mongo is never dialled — mongo.NewClient just stores
// names. Tier-shared singletons (jwtService, passwordService, etc.)
// are deliberately nil; NewAuthService/NewPasswordAuthService assign
// without validation and the bundle is never exercised here.
func TestBuildAuthTierBundlePicksMatchingConstructors(t *testing.T) {
	t.Parallel()

	client, err := mongo.NewClient(options.Client().ApplyURI("mongodb://test/test"))
	if err != nil {
		t.Fatalf("new mongo client: %v", err)
	}
	db := client.Database("test")

	for _, tier := range []audienceTier{tierOperator, tierClient} {
		tier := tier
		t.Run(string(tier), func(t *testing.T) {
			t.Parallel()

			bundle, err := buildAuthTierBundle(tierBundleDeps{db: db, tier: tier})
			if err != nil {
				t.Fatalf("buildAuthTierBundle: %v", err)
			}
			if bundle.tier != tier {
				t.Errorf("bundle.tier = %q, want %q", bundle.tier, tier)
			}
			if bundle.authService == nil {
				t.Error("authService is nil")
			}
			if bundle.passwordSvc == nil {
				t.Error("passwordSvc is nil")
			}
			if bundle.riskAssessment == nil {
				t.Error("riskAssessment is nil")
			}
			if bundle.authSessionRepo == nil ||
				bundle.refreshTokenRepo == nil ||
				bundle.oauthProviderRepo == nil ||
				bundle.mfaFactorRepo == nil ||
				bundle.emailTokenRepo == nil {
				t.Error("bundle has a nil repo")
			}
			// D-4: MFA service is always populated (TOTP doesn't gate
			// on env). WebAuthn stays nil because tierBundleDeps.webauthnRP
			// is nil here — matches the "passkeys disabled" branch
			// module.go takes when WEBAUTHN_RP_* env vars don't resolve.
			if bundle.mfaSvc == nil {
				t.Error("mfaSvc is nil — every tier should have a TOTP orchestrator")
			}
			if bundle.webauthnSvc != nil {
				t.Error("webauthnSvc should be nil when webauthnRP dep is nil")
			}
		})
	}
}
