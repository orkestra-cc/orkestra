package auth

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"

	"github.com/orkestra/backend/internal/core/auth/models"
	"github.com/orkestra/backend/internal/core/auth/repository"
	"github.com/orkestra/backend/internal/core/auth/services"
	"github.com/orkestra/backend/pkg/sdk/iface"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

type initialPasswordBundleUser struct {
	iface.UserProvider
	user        *iface.User
	setterCalls int
}

func (u *initialPasswordBundleUser) GetUserByID(context.Context, string) (*iface.User, error) {
	return u.user, nil
}

func (u *initialPasswordBundleUser) ClearFailedLogins(context.Context, string) error { return nil }

func (u *initialPasswordBundleUser) SetPasswordHashIfUnset(_ context.Context, id, hash string) error {
	if id != u.user.UUID || hash != "test-hash" {
		return errors.New("unexpected credential setter arguments")
	}
	u.setterCalls++
	u.user.PasswordHash = hash
	return nil
}

type bundlePasswordHasher struct{ services.PasswordService }

func (bundlePasswordHasher) Hash(string) (string, error)                          { return "test-hash", nil }
func (bundlePasswordHasher) ValidatePolicy(context.Context, string, string) error { return nil }

type bundleSecurityEvents struct {
	repository.SecurityEventRepository
	rows []*models.SecurityEvent
}

func (r *bundleSecurityEvents) Insert(_ context.Context, event *models.SecurityEvent) error {
	r.rows = append(r.rows, event)
	return nil
}

type bundleAuditEvents struct{ rows []iface.AuditEvent }

func (r *bundleAuditEvents) Emit(_ context.Context, event iface.AuditEvent) {
	r.rows = append(r.rows, event)
}

func TestTierBundle_InitialPasswordUsesTierDependencies(t *testing.T) {
	client, err := mongo.NewClient(options.Client().ApplyURI("mongodb://test/test"))
	if err != nil {
		t.Fatal(err)
	}
	users := &initialPasswordBundleUser{user: &iface.User{
		UUID: "operator-user", Email: "operator@example.com", IsActive: true, EmailVerified: true,
	}}
	events := &bundleSecurityEvents{}
	bundle, err := buildAuthTierBundle(tierBundleDeps{
		db: client.Database("test"), tier: tierOperator, userProvider: users,
		initialPasswordSetter: users, passwordService: bundlePasswordHasher{},
		authPolicy:        services.NewAuthPolicyServiceForTest(map[string]string{"passwordLoginEnabledAdmin": "true"}),
		securityEventRepo: events, logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	if err != nil {
		t.Fatal(err)
	}
	audit := &bundleAuditEvents{}
	bundle.authService.(iface.AuditSinkSetter).SetAuditSink(audit)
	// Both consumers receive the sink in production. Enrollment must still emit once.
	bundle.passwordSvc.SetAuditSink(audit)
	// Mongo is deliberately unconnected: teardown errors are best-effort, and
	// the emitted metadata must report that no teardown completion is known.
	if err := bundle.passwordSvc.SetInitialPassword(context.Background(), services.SetInitialPasswordInput{
		UserUUID: users.user.UUID, CurrentSID: "caller-sid", New: "new-passphrase",
	}); err != nil {
		t.Fatal(err)
	}
	if users.setterCalls != 1 || users.user.PasswordHash != "test-hash" {
		t.Fatal("bundle did not enroll through the supplied setter")
	}
	if len(events.rows) != 1 || events.rows[0].EventType != "self_password_added" || events.rows[0].UserUUID != "operator-user" {
		t.Fatalf("bundle security events = %+v, want one self_password_added for operator-user", events.rows)
	}
	if len(audit.rows) != 1 || audit.rows[0].Action != "auth.password.added" {
		t.Fatalf("bundle compliance events = %+v, want one auth.password.added", audit.rows)
	}
	if events.rows[0].Metadata["audience"] != "operator" || events.rows[0].Metadata["teardownComplete"] != false {
		t.Fatalf("unexpected tier/teardown metadata: %v", events.rows[0].Metadata)
	}
}

func TestTierBundle_ClientInitialPasswordSetterIsOptional(t *testing.T) {
	client, err := mongo.NewClient(options.Client().ApplyURI("mongodb://test/test"))
	if err != nil {
		t.Fatal(err)
	}
	bundle, err := buildAuthTierBundle(tierBundleDeps{db: client.Database("test"), tier: tierClient})
	if err != nil {
		t.Fatal(err)
	}
	if err := bundle.passwordSvc.SetInitialPassword(context.Background(), services.SetInitialPasswordInput{}); !errors.Is(err, services.ErrInitialPasswordUnavailable) {
		t.Fatalf("client initial-password error = %v, want setter unavailable", err)
	}
}

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
			// D20: the bundle's PolicyAudience is what module.go hands to
			// SetVerifyAttemptCounter on that tier's MFA + WebAuthn
			// handlers, and AttemptKeyMFAVerify puts it in the lockout
			// key. Deriving it here — from the same `tier` that selected
			// the repos above — is what makes "the operator handler is
			// wired with the operator audience" structural rather than a
			// literal repeated at four wiring sites. Get it wrong and the
			// two tiers silently share one lockout scope.
			wantAudience := services.PolicyAudienceOperator
			if tier == tierClient {
				wantAudience = services.PolicyAudienceClient
			}
			if bundle.policyAudience != wantAudience {
				t.Errorf("policyAudience = %q, want %q", bundle.policyAudience, wantAudience)
			}
		})
	}
}
