package repository

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/orkestra/backend/internal/core/auth/models"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

func livePINDeviceDB(t *testing.T) *mongo.Database {
	t.Helper()
	uri := os.Getenv("MONGO_TEST_URI")
	if uri == "" {
		uri = os.Getenv("MONGO_URI")
	}
	if uri == "" {
		t.Skip("set MONGO_TEST_URI or MONGO_URI to run live PIN device repository tests")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	client, err := mongo.Connect(ctx, options.Client().ApplyURI(uri))
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	db := client.Database("auth_pin_devices_" + uuid.NewString())
	t.Cleanup(func() {
		_ = db.Drop(context.Background())
		_ = client.Disconnect(context.Background())
	})
	return db
}

func TestPINDeviceRepository_ConstructorsBindTier(t *testing.T) {
	client, err := mongo.NewClient(options.Client().ApplyURI("mongodb://test/test"))
	if err != nil {
		t.Fatal(err)
	}
	db := client.Database("test")
	for _, c := range []struct {
		build    func(*mongo.Database) PINDeviceRepository
		tier     string
		collName string
	}{
		{NewOperatorPINDeviceRepository, models.TierOperator, models.OperatorPINDevicesCollection},
		{NewClientPINDeviceRepository, models.TierClient, models.ClientPINDevicesCollection},
	} {
		repo := c.build(db).(*pinDeviceRepository)
		if repo.tier != c.tier || repo.coll.Name() != c.collName {
			t.Fatalf("tier=%q coll=%q, want %q %q", repo.tier, repo.coll.Name(), c.tier, c.collName)
		}
	}
}

func TestPINDeviceRepository_LifeCycle(t *testing.T) {
	db := livePINDeviceDB(t)
	repo := NewOperatorPINDeviceRepository(db)
	ctx := context.Background()

	first := &models.PINDeviceDoc{UUID: "d1", UserUUID: "u-1", DeviceID: "tab-1", SecretHash: "s1", Via: models.PINDeviceViaAdmin}
	if err := repo.Upsert(ctx, first); err != nil {
		t.Fatal(err)
	}
	// Re-enrolling the same device replaces its secret instead of adding a row.
	if err := repo.Upsert(ctx, &models.PINDeviceDoc{UUID: "d2", UserUUID: "u-1", DeviceID: "tab-1", SecretHash: "s2", Via: models.PINDeviceViaSelf}); err != nil {
		t.Fatal(err)
	}
	if err := repo.Upsert(ctx, &models.PINDeviceDoc{UUID: "d3", UserUUID: "u-2", DeviceID: "tab-1", SecretHash: "s3"}); err != nil {
		t.Fatal(err)
	}
	//tenantscope:allow test reads one isolated database's user-keyed PIN devices
	got, err := repo.Find(ctx, "u-1", "tab-1")
	if err != nil {
		t.Fatal(err)
	}
	if got.SecretHash != "s2" || got.Via != models.PINDeviceViaSelf || got.Tier != models.TierOperator {
		t.Fatalf("got %+v", got)
	}
	list, err := repo.ListByUser(ctx, "u-1")
	if err != nil || len(list) != 1 {
		t.Fatalf("list=%v err=%v, want one row", list, err)
	}

	when := time.Now().Truncate(time.Millisecond)
	if err := repo.TouchLastUsed(ctx, "u-1", "tab-1", when); err != nil {
		t.Fatal(err)
	}
	//tenantscope:allow test reads one isolated database's user-keyed PIN devices
	got, _ = repo.Find(ctx, "u-1", "tab-1")
	if got.LastUsedAt == nil || !got.LastUsedAt.Equal(when) {
		t.Fatalf("lastUsedAt=%v", got.LastUsedAt)
	}

	if err := repo.Delete(ctx, "u-1", "tab-1"); err != nil {
		t.Fatal(err)
	}
	if err := repo.Delete(ctx, "u-1", "tab-1"); !errors.Is(err, ErrPINDeviceNotFound) {
		t.Fatalf("second delete: %v", err)
	}
	//tenantscope:allow test reads one isolated database's user-keyed PIN devices
	if _, err := repo.Find(ctx, "u-1", "tab-1"); !errors.Is(err, ErrPINDeviceNotFound) {
		t.Fatalf("find after delete: %v", err)
	}
	n, err := repo.DeleteAllByUser(ctx, "u-2")
	if err != nil || n != 1 {
		t.Fatalf("deleteAll n=%d err=%v", n, err)
	}
	if list, _ := repo.ListByUser(ctx, "u-2"); len(list) != 0 {
		t.Fatalf("u-2 still has %d devices", len(list))
	}
}
