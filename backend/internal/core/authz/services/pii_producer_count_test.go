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

	"github.com/orkestra/backend/internal/core/authz/repository"
	"github.com/orkestra/backend/pkg/sdk/iface"
)

func TestPIIProducer_CountMatchesPurge(t *testing.T) {
	uri := os.Getenv("MONGO_TEST_URI")
	if uri == "" {
		t.Skip("set MONGO_TEST_URI to run live authz PII producer tests")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	client, err := mongo.Connect(ctx, options.Client().ApplyURI(uri))
	if err != nil {
		t.Fatal(err)
	}
	db := client.Database("authz_pii_count_" + uuid.NewString())
	t.Cleanup(func() {
		_ = db.Drop(context.Background())
		_ = client.Disconnect(context.Background())
	})

	// A tenant binding, a global binding (tenantId "") and another subject's.
	for _, r := range []bson.M{
		{"userUUID": "u1", "tenantId": "t1", "roleId": "r1"},
		{"userUUID": "u1", "tenantId": "", "roleId": "r2"},
		{"userUUID": "other", "tenantId": "t1", "roleId": "r1"},
	} {
		//tenantscope:allow Test seeds bindings directly in an isolated test database.
		if _, err := db.Collection(repository.CollBindings).InsertOne(ctx, r); err != nil {
			t.Fatal(err)
		}
	}
	p := NewPIIProducer(repository.New(db))
	counter, ok := p.(iface.PIIProducerCounter)
	if !ok {
		t.Fatal("authz producer does not implement PIIProducerCounter")
	}

	got, err := counter.CountPersonalData(ctx, "u1")
	if err != nil {
		t.Fatal(err)
	}
	res, err := p.PurgePersonalData(ctx, "u1", iface.EraseHardDelete)
	if err != nil {
		t.Fatal(err)
	}
	if got.Rows != 2 || got.Rows != res.RowsDeleted+res.RowsAnonymized {
		t.Fatalf("count %d, purge %+v; want 2 and equal", got.Rows, res)
	}
	if !slices.Equal(got.Collections, res.Collections) {
		t.Fatalf("collections %v != %v", got.Collections, res.Collections)
	}
	other, err := counter.CountPersonalData(ctx, "other")
	if err != nil || other.Rows != 1 {
		t.Fatalf("other count = %+v, %v; want 1", other, err)
	}
	none, err := counter.CountPersonalData(ctx, "u1")
	if err != nil || none.Rows != 0 || len(none.Collections) != 0 {
		t.Fatalf("after purge = %+v, %v", none, err)
	}
}
