//go:build integration

package repository

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/orkestra/backend/internal/core/auth/models"
	"github.com/orkestra/backend/pkg/sdk/iface"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

func authTestDB(t *testing.T) *mongo.Database {
	t.Helper()
	uri := os.Getenv("MONGO_URI")
	if uri == "" {
		t.Skip("MONGO_URI not set")
	}
	dbName := os.Getenv("MONGO_DATABASE")
	if dbName == "" {
		dbName = "orkestra"
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	client, err := mongo.Connect(ctx, options.Client().ApplyURI(uri))
	if err != nil {
		t.Fatalf("connect mongo: %v", err)
	}
	t.Cleanup(func() { _ = client.Disconnect(context.Background()) })
	return client.Database(dbName)
}

func TestServiceAccountCredentialLifecycle(t *testing.T) {
	db := authTestDB(t)
	repo := NewServiceAccountCredentialRepository(db)
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Millisecond)
	doc := &models.ServiceAccountCredential{
		UUID: "itest-" + iface.GenerateUUIDv7(), UserUUID: "itest-user-1",
		ClientID: "sa_itest01", SecretHash: "$argon2id$fake", Label: "initial", CreatedAt: now,
	}
	t.Cleanup(func() {
		_, _ = db.Collection(models.ServiceAccountCredentialsCollection).
			DeleteMany(context.Background(), bson.M{"uuid": bson.M{"$regex": "^itest-"}})
	})
	if err := repo.Create(ctx, doc); err != nil {
		t.Fatal(err)
	}

	got, err := repo.GetByClientID(ctx, "sa_itest01")
	if err != nil || got.UUID != doc.UUID {
		t.Fatalf("get: %v %+v", err, got)
	}

	if n, _ := repo.CountActiveByUser(ctx, "itest-user-1"); n != 1 {
		t.Fatalf("active = %d, want 1", n)
	}
	if err := repo.StampLastUsed(ctx, doc.UUID, now); err != nil {
		t.Fatal(err)
	}
	if err := repo.Revoke(ctx, doc.UUID, now); err != nil {
		t.Fatal(err)
	}
	if n, _ := repo.CountActiveByUser(ctx, "itest-user-1"); n != 0 {
		t.Fatalf("active after revoke = %d, want 0", n)
	}

	revoked, err := repo.GetByClientID(ctx, "sa_itest01")
	if err != nil {
		t.Fatalf("revoked credential must still be fetchable by clientId: %v", err)
	}
	if revoked.RevokedAt == nil {
		t.Fatalf("revoked credential must carry RevokedAt, got nil")
	}

	if _, err := repo.GetByClientID(ctx, "sa_missing"); !errors.Is(err, ErrServiceAccountCredentialNotFound) {
		t.Fatalf("missing lookup err = %v", err)
	}
}
