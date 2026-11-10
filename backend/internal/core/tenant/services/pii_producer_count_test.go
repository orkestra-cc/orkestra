package services

import (
	"context"
	"slices"
	"testing"
	"time"

	"go.mongodb.org/mongo-driver/bson"

	"github.com/orkestra/backend/internal/core/tenant/repository"
	"github.com/orkestra/backend/pkg/sdk/iface"
)

func TestPIIProducer_CountMatchesPurge(t *testing.T) {
	db, cleanup := newDefaultsTestDB(t)
	defer cleanup()
	ctx := context.Background()
	past := time.Now().Add(-time.Hour)
	// Expired memberships are purged too (the purge filter is the user alone),
	// so they must be counted.
	rows := []bson.M{
		{"userUUID": "u1", "tenantId": "t1"},
		{"userUUID": "u1", "tenantId": "t2", "expiresAt": past},
		{"userUUID": "other", "tenantId": "t1"},
	}
	for _, r := range rows {
		//tenantscope:allow Test seeds memberships directly in an isolated test database.
		if _, err := db.Collection(repository.CollMemberships).InsertOne(ctx, r); err != nil {
			t.Fatal(err)
		}
	}
	p := NewPIIProducer(repository.New(db))
	counter, ok := p.(iface.PIIProducerCounter)
	if !ok {
		t.Fatal("tenant producer does not implement PIIProducerCounter")
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
