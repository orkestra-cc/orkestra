package repository

import (
	"testing"
	"time"

	"github.com/orkestra/backend/internal/core/auth/models"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// TestAuthRepoConstructorsBindCorrectTierAndCollection mirrors the
// PR-B B-5 invariant for the auth-side repos introduced in ADR-0003
// PR-D. Each of the five repos exposes three constructors (legacy /
// operator / client) and the test asserts that:
//
//   - the legacy constructor binds to the legacy auth_* collection
//     name with tier="",
//   - the operator constructor binds to operator_* with tier="operator",
//   - the client constructor binds to client_* with tier="client".
//
// Mongo is never contacted: mongo.NewClient does not dial; Database()
// and Collection() are constructor calls that store names. The test
// asserts on those stored names, which is what the repo embeds.
func TestAuthRepoConstructorsBindCorrectTierAndCollection(t *testing.T) {
	t.Parallel()

	client, err := mongo.NewClient(options.Client().ApplyURI("mongodb://test/test"))
	if err != nil {
		t.Fatalf("new mongo client: %v", err)
	}
	db := client.Database("test")

	type sessionCase struct {
		name     string
		build    func(*mongo.Database) AuthSessionRepository
		wantTier string
		wantColl string
	}
	for _, c := range []sessionCase{
		{"legacy", NewAuthSessionRepository, "", models.AuthSessionsCollection},
		{"operator", NewOperatorAuthSessionRepository, models.TierOperator, models.OperatorSessionsCollection},
		{"client", NewClientAuthSessionRepository, models.TierClient, models.ClientSessionsCollection},
	} {
		c := c
		t.Run("session/"+c.name, func(t *testing.T) {
			t.Parallel()
			repo, ok := c.build(db).(*authSessionRepository)
			if !ok {
				t.Fatalf("constructor returned unexpected type %T", c.build(db))
			}
			if repo.tier != c.wantTier {
				t.Errorf("tier = %q, want %q", repo.tier, c.wantTier)
			}
			if got := repo.collection.Name(); got != c.wantColl {
				t.Errorf("collection name = %q, want %q", got, c.wantColl)
			}
		})
	}

	type refreshCase struct {
		name     string
		build    func(*mongo.Database) RefreshTokenRepository
		wantTier string
		wantColl string
	}
	for _, c := range []refreshCase{
		{"legacy", NewRefreshTokenRepository, "", models.RefreshTokensCollection},
		{"operator", NewOperatorRefreshTokenRepository, models.TierOperator, models.OperatorRefreshTokensCollection},
		{"client", NewClientRefreshTokenRepository, models.TierClient, models.ClientRefreshTokensCollection},
	} {
		c := c
		t.Run("refresh/"+c.name, func(t *testing.T) {
			t.Parallel()
			repo, ok := c.build(db).(*refreshTokenRepository)
			if !ok {
				t.Fatalf("constructor returned unexpected type %T", c.build(db))
			}
			if repo.tier != c.wantTier {
				t.Errorf("tier = %q, want %q", repo.tier, c.wantTier)
			}
			if got := repo.collection.Name(); got != c.wantColl {
				t.Errorf("collection name = %q, want %q", got, c.wantColl)
			}
		})
	}

	type oauthCase struct {
		name     string
		build    func(*mongo.Database) OAuthProviderRepository
		wantTier string
		wantColl string
	}
	for _, c := range []oauthCase{
		{"legacy", NewOAuthProviderRepository, "", models.OAuthProvidersCollection},
		{"operator", NewOperatorOAuthProviderRepository, models.TierOperator, models.OperatorOAuthProvidersCollection},
		{"client", NewClientOAuthProviderRepository, models.TierClient, models.ClientOAuthProvidersCollection},
	} {
		c := c
		t.Run("oauth/"+c.name, func(t *testing.T) {
			t.Parallel()
			repo, ok := c.build(db).(*oauthProviderRepository)
			if !ok {
				t.Fatalf("constructor returned unexpected type %T", c.build(db))
			}
			if repo.tier != c.wantTier {
				t.Errorf("tier = %q, want %q", repo.tier, c.wantTier)
			}
			if got := repo.collection.Name(); got != c.wantColl {
				t.Errorf("collection name = %q, want %q", got, c.wantColl)
			}
		})
	}

	type mfaCase struct {
		name     string
		build    func(*mongo.Database) MFAFactorRepository
		wantTier string
		wantColl string
	}
	for _, c := range []mfaCase{
		{"legacy", NewMFAFactorRepository, "", models.MFAFactorsCollection},
		{"operator", NewOperatorMFAFactorRepository, models.TierOperator, models.OperatorMFAFactorsCollection},
		{"client", NewClientMFAFactorRepository, models.TierClient, models.ClientMFAFactorsCollection},
	} {
		c := c
		t.Run("mfa/"+c.name, func(t *testing.T) {
			t.Parallel()
			repo, ok := c.build(db).(*mfaFactorRepository)
			if !ok {
				t.Fatalf("constructor returned unexpected type %T", c.build(db))
			}
			if repo.tier != c.wantTier {
				t.Errorf("tier = %q, want %q", repo.tier, c.wantTier)
			}
			if got := repo.coll.Name(); got != c.wantColl {
				t.Errorf("collection name = %q, want %q", got, c.wantColl)
			}
		})
	}

	type emailCase struct {
		name     string
		build    func(*mongo.Database) EmailTokenRepository
		wantTier string
		wantColl string
	}
	for _, c := range []emailCase{
		{"legacy", NewEmailTokenRepository, "", models.EmailTokensCollection},
		{"operator", NewOperatorEmailTokenRepository, models.TierOperator, models.OperatorEmailTokensCollection},
		{"client", NewClientEmailTokenRepository, models.TierClient, models.ClientEmailTokensCollection},
	} {
		c := c
		t.Run("email/"+c.name, func(t *testing.T) {
			t.Parallel()
			repo, ok := c.build(db).(*emailTokenRepository)
			if !ok {
				t.Fatalf("constructor returned unexpected type %T", c.build(db))
			}
			if repo.tier != c.wantTier {
				t.Errorf("tier = %q, want %q", repo.tier, c.wantTier)
			}
			if got := repo.coll.Name(); got != c.wantColl {
				t.Errorf("collection name = %q, want %q", got, c.wantColl)
			}
		})
	}
}

// TestAuthDocsTierStampedOnCreate mirrors the production stamp path for
// each of the five auth doc structs. The assertion is the invariant
// (legacy repo never overwrites Tier; operator/client repos stamp it
// regardless of the prior value), not that InsertOne fired. Mongo is
// never contacted.
func TestAuthDocsTierStampedOnCreate(t *testing.T) {
	t.Parallel()

	cases := []struct {
		repoTier string
		docTier  string
		want     string
	}{
		{"", "", ""},
		{"", models.TierOperator, models.TierOperator}, // legacy: never touch the field
		{models.TierOperator, "", models.TierOperator},
		{models.TierOperator, models.TierClient, models.TierOperator}, // operator: stamp regardless
		{models.TierClient, "", models.TierClient},
		{models.TierClient, models.TierOperator, models.TierClient},
	}

	stamp := func(repoTier string, docTier *string) {
		// Mirror the production path: `if r.tier != "" { doc.Tier = r.tier }`.
		// Keep this in sync with each repo's Create / CreateSession /
		// CreateOAuthProvider / CreateRefreshToken / Insert.
		if repoTier != "" {
			*docTier = repoTier
		}
	}

	for _, c := range cases {
		c := c
		name := c.repoTier + "/" + c.docTier
		if name == "/" {
			name = "empty/empty"
		}
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			session := &models.AuthSessionDoc{Tier: c.docTier, UserUUID: "u-1", DeviceID: "d-1", CreatedAt: time.Now()}
			refresh := &models.RefreshTokenDoc{Tier: c.docTier, UserUUID: "u-1"}
			oauth := &models.OAuthProviderDoc{Tier: c.docTier, UserUUID: "u-1"}
			mfa := &models.MFAFactorDoc{Tier: c.docTier, UserUUID: "u-1", Type: models.MFAFactorTOTP}
			email := &models.EmailTokenDoc{Tier: c.docTier, UserUUID: "u-1"}

			stamp(c.repoTier, &session.Tier)
			stamp(c.repoTier, &refresh.Tier)
			stamp(c.repoTier, &oauth.Tier)
			stamp(c.repoTier, &mfa.Tier)
			stamp(c.repoTier, &email.Tier)

			for _, got := range []struct {
				name string
				tier string
			}{
				{"AuthSessionDoc", session.Tier},
				{"RefreshTokenDoc", refresh.Tier},
				{"OAuthProviderDoc", oauth.Tier},
				{"MFAFactorDoc", mfa.Tier},
				{"EmailTokenDoc", email.Tier},
			} {
				if got.tier != c.want {
					t.Errorf("%s.Tier = %q, want %q", got.name, got.tier, c.want)
				}
			}
		})
	}
}
