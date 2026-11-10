package services

import (
	"context"
	"os"
	"slices"
	"testing"
	"time"

	"github.com/google/uuid"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"

	"github.com/orkestra/backend/internal/core/user/repository"
	"github.com/orkestra/backend/pkg/sdk/iface"
)

// countFixture seeds a live user with two preference documents next to
// another user's, against real repositories (the fakes' purge is a no-op).
func countFixture(t *testing.T) (iface.PIIProducer, *mongo.Database) {
	t.Helper()
	uri := os.Getenv("MONGO_TEST_URI")
	if uri == "" {
		t.Skip("set MONGO_TEST_URI to run live user PII producer tests")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	client, err := mongo.Connect(ctx, options.Client().ApplyURI(uri))
	if err != nil {
		t.Fatal(err)
	}
	db := client.Database("user_pii_count_" + uuid.NewString())
	t.Cleanup(func() {
		_ = db.Drop(context.Background())
		_ = client.Disconnect(context.Background())
	})
	users := repository.NewOperatorUserRepository(db)
	prefs := repository.NewPreferenceRepository(db)
	for _, id := range []string{"u1", "other"} {
		//tenantscope:allow Test seeds users directly in an isolated test database.
		if _, err := db.Collection(repository.OperatorUsersCollection).InsertOne(ctx,
			bson.M{"uuid": id, "email": id + "@example.com", "role": "operator", "isActive": true}); err != nil {
			t.Fatal(err)
		}
		for _, m := range []string{"crm", "billing"} {
			if _, err := prefs.Upsert(ctx, id, m, map[string]string{"k": "v"}); err != nil {
				t.Fatal(err)
			}
		}
	}
	return NewPIIProducer(users, prefs), db
}

func TestPIIProducer_CountMatchesPurge(t *testing.T) {
	modes := map[string]iface.EraseMode{"hard-delete": iface.EraseHardDelete, "anonymize": iface.EraseAnonymize}
	for name, mode := range modes {
		t.Run(name, func(t *testing.T) {
			p, _ := countFixture(t)
			counter, ok := p.(iface.PIIProducerCounter)
			if !ok {
				t.Fatal("user producer does not implement PIIProducerCounter")
			}
			ctx := context.Background()

			got, err := counter.CountPersonalData(ctx, "u1")
			if err != nil {
				t.Fatal(err)
			}
			res, err := p.PurgePersonalData(ctx, "u1", mode)
			if err != nil {
				t.Fatal(err)
			}
			if got.Rows != 3 || got.Rows != res.RowsDeleted+res.RowsAnonymized {
				t.Fatalf("count %d, purge %+v; want 3 (2 preferences + identity) and equal", got.Rows, res)
			}
			a, b := slices.Clone(got.Collections), slices.Clone(res.Collections)
			slices.Sort(a)
			slices.Sort(b)
			if !slices.Equal(a, b) {
				t.Fatalf("collections %v != %v", a, b)
			}
			other, err := counter.CountPersonalData(ctx, "other")
			if err != nil || other.Rows != 3 {
				t.Fatalf("other subject = %+v, %v; want 3 untouched", other, err)
			}
		})
	}
}

// An already soft-deleted tombstone is still a row a hard delete removes.
func TestPIIProducer_CountIncludesTombstoneRemovedByHardDelete(t *testing.T) {
	p, db := countFixture(t)
	ctx := context.Background()
	//tenantscope:allow Test marks one seeded user soft-deleted in an isolated test database.
	if _, err := db.Collection(repository.OperatorUsersCollection).UpdateOne(ctx,
		bson.M{"uuid": "u1"}, bson.M{"$set": bson.M{"deletedAt": time.Now()}}); err != nil {
		t.Fatal(err)
	}
	got, err := p.(iface.PIIProducerCounter).CountPersonalData(ctx, "u1")
	if err != nil {
		t.Fatal(err)
	}
	res, err := p.PurgePersonalData(ctx, "u1", iface.EraseHardDelete)
	if err != nil {
		t.Fatal(err)
	}
	if got.Rows != 3 || got.Rows != res.RowsDeleted {
		t.Fatalf("count %d, purge %+v; want 3 and equal", got.Rows, res)
	}
}
