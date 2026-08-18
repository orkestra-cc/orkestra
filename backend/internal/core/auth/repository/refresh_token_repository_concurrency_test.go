package repository

import (
	"context"
	"errors"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/orkestra/backend/internal/core/auth/models"
	"github.com/orkestra/backend/internal/shared/utils"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

func liveRefreshRepository(t *testing.T) (*refreshTokenRepository, func()) {
	t.Helper()
	uri := os.Getenv("MONGO_TEST_URI")
	if uri == "" {
		uri = os.Getenv("MONGO_URI")
	}
	if uri == "" {
		t.Skip("set MONGO_TEST_URI or MONGO_URI to run live refresh repository tests")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	client, err := mongo.Connect(ctx, options.Client().ApplyURI(uri))
	if err != nil {
		t.Fatalf("mongo.Connect: %v", err)
	}
	if err := client.Ping(ctx, nil); err != nil {
		_ = client.Disconnect(context.Background())
		t.Fatalf("mongo.Ping: %v", err)
	}
	db := client.Database("auth_refresh_race_" + uuid.NewString())
	repo := NewOperatorRefreshTokenRepository(db).(*refreshTokenRepository)
	if _, err := repo.familyCollection.Indexes().CreateOne(ctx, mongo.IndexModel{
		Keys: bson.D{{Key: "familyId", Value: 1}}, Options: options.Index().SetUnique(true),
	}); err != nil {
		t.Fatalf("create family index: %v", err)
	}
	cleanup := func() {
		_ = db.Drop(context.Background())
		_ = client.Disconnect(context.Background())
	}
	return repo, cleanup
}

func refreshRaceDoc(raw, family string) *models.RefreshTokenDoc {
	return &models.RefreshTokenDoc{
		UUID: models.GenerateUUIDv7(), UserUUID: "race-user", Token: raw,
		SessionUUID: "race-session", DeviceID: "race-device", DeviceType: "desktop",
		Platform: "web", Fingerprint: "race-fingerprint", ExpiresAt: time.Now().Add(time.Hour),
		FamilyID: family,
	}
}

func TestRefreshRepository_CompromiseMarkerMakesLateSuccessorBornRevoked(t *testing.T) {
	repo, cleanup := liveRefreshRepository(t)
	defer cleanup()
	ctx := context.Background()
	family := "family-marker-gap"
	old := refreshRaceDoc("old-marker-gap", family)
	if err := repo.CreateRefreshToken(ctx, old); err != nil {
		t.Fatalf("CreateRefreshToken: %v", err)
	}
	now := time.Now()
	if _, err := repo.familyCollection.InsertOne(ctx, &models.RefreshTokenFamilyStateDoc{
		FamilyID: family, RevokedAt: now, RevokedReason: models.RevokeReasonReplayDetected,
		CreatedAt: now, UpdatedAt: now,
	}); err != nil {
		t.Fatalf("insert compromise marker: %v", err)
	}

	successor := refreshRaceDoc("successor-marker-gap", family)
	err := repo.RotateWithFamily(ctx, utils.HashRefreshToken("old-marker-gap"), successor)
	if !errors.Is(err, ErrTokenAlreadyRotated) {
		t.Fatalf("RotateWithFamily = %v, want ErrTokenAlreadyRotated", err)
	}
	stored, err := repo.GetByTokenAny(ctx, utils.HashRefreshToken("successor-marker-gap"))
	if err != nil {
		t.Fatalf("GetByTokenAny: %v", err)
	}
	if stored == nil || !stored.IsRevoked || stored.RevokedReason != models.RevokeReasonReplayDetected {
		t.Fatalf("late successor escaped family fence: %+v", stored)
	}
}

func TestRefreshRepository_RotateAndRevokeFamilyHaveNoActiveSuccessor(t *testing.T) {
	repo, cleanup := liveRefreshRepository(t)
	defer cleanup()
	ctx := context.Background()

	for i := 0; i < 40; i++ {
		family := uuid.NewString()
		oldRaw := "old-" + family
		nextRaw := "next-" + family
		if err := repo.CreateRefreshToken(ctx, refreshRaceDoc(oldRaw, family)); err != nil {
			t.Fatalf("iteration %d create: %v", i, err)
		}
		start := make(chan struct{})
		var wg sync.WaitGroup
		wg.Add(2)
		go func() {
			defer wg.Done()
			<-start
			_ = repo.RotateWithFamily(ctx, utils.HashRefreshToken(oldRaw), refreshRaceDoc(nextRaw, family))
		}()
		go func() {
			defer wg.Done()
			<-start
			_, _ = repo.RevokeFamily(ctx, family, models.RevokeReasonReplayDetected)
		}()
		close(start)
		wg.Wait()

		//tenantscope:allow Live repository test inspects one isolated test database directly.
		active, err := repo.collection.CountDocuments(ctx, bson.M{"familyId": family, "isRevoked": false})
		if err != nil {
			t.Fatalf("iteration %d count active: %v", i, err)
		}
		if active != 0 {
			t.Fatalf("iteration %d left %d active family members", i, active)
		}
	}
}

func TestRefreshRepository_RevokeFamilyStampsMarkerTier(t *testing.T) {
	operatorRepo, cleanup := liveRefreshRepository(t)
	defer cleanup()
	ctx := context.Background()
	clientRepo := NewClientRefreshTokenRepository(operatorRepo.collection.Database()).(*refreshTokenRepository)
	for _, tc := range []struct {
		name string
		repo *refreshTokenRepository
		tier string
	}{
		{name: "operator", repo: operatorRepo, tier: models.TierOperator},
		{name: "client", repo: clientRepo, tier: models.TierClient},
	} {
		t.Run(tc.name, func(t *testing.T) {
			family := "family-tier-stamp-" + tc.name
			if _, err := tc.repo.RevokeFamily(ctx, family, models.RevokeReasonReplayDetected); err != nil {
				t.Fatalf("RevokeFamily: %v", err)
			}
			var state models.RefreshTokenFamilyStateDoc
			//tenantscope:allow Live repository test inspects one isolated test database directly.
			if err := tc.repo.familyCollection.FindOne(ctx, bson.M{"familyId": family}).Decode(&state); err != nil {
				t.Fatalf("FindOne family marker: %v", err)
			}
			if state.Tier != tc.tier {
				t.Fatalf("family marker tier = %q, want %q", state.Tier, tc.tier)
			}
		})
	}
}
