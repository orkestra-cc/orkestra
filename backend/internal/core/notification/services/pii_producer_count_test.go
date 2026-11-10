package services

import (
	"context"
	"slices"
	"testing"
	"time"

	"github.com/google/uuid"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"

	"github.com/orkestra/backend/internal/core/notification/models"
	"github.com/orkestra/backend/pkg/sdk/iface"
)

func seedNotificationRows(t *testing.T, db *mongo.Database, user string) {
	t.Helper()
	ctx := context.Background()
	now := time.Now().UTC()
	for i := 0; i < 2; i++ {
		if _, err := db.Collection(models.NotificationMessagesCollection).InsertOne(ctx,
			bson.M{"uuid": uuid.NewString(), "recipientUserUuid": user, "createdAt": now}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := db.Collection(models.NotificationPreferencesCollection).InsertOne(ctx,
		bson.M{"uuid": uuid.NewString(), "userUuid": user, "createdAt": now}); err != nil {
		t.Fatal(err)
	}
	seedDevice(t, db, "operator", user, "d-op-"+user, "tok-op-"+user)
	seedDevice(t, db, "client", user, "d-cl-"+user, "tok-cl-"+user)
}

func TestPIIProducer_CountMatchesPurge(t *testing.T) {
	db := piiTestDB(t)
	seedNotificationRows(t, db, "u1")
	seedNotificationRows(t, db, "other")
	p := NewPIIProducer(db).(*piiProducer)
	ctx := context.Background()

	got, err := p.CountPersonalData(ctx, "u1")
	if err != nil {
		t.Fatal(err)
	}
	if got.Rows != 5 {
		t.Fatalf("rows = %d, want 5 (2 messages, 1 preference, 2 devices)", got.Rows)
	}
	res, err := p.PurgePersonalData(ctx, "u1", iface.EraseHardDelete)
	if err != nil {
		t.Fatal(err)
	}
	if want := res.RowsDeleted + res.RowsAnonymized; got.Rows != want {
		t.Fatalf("count %d != purge result %d", got.Rows, want)
	}
	slices.Sort(got.Collections)
	slices.Sort(res.Collections)
	if !slices.Equal(got.Collections, res.Collections) {
		t.Fatalf("collections %v != %v", got.Collections, res.Collections)
	}
	other, err := p.CountPersonalData(ctx, "other")
	if err != nil || other.Rows != 5 {
		t.Fatalf("other subject count = %+v, %v; want 5 untouched", other, err)
	}
	after, err := p.CountPersonalData(ctx, "u1")
	if err != nil || after.Rows != 0 || len(after.Collections) != 0 {
		t.Fatalf("count after purge = %+v, %v", after, err)
	}
}
