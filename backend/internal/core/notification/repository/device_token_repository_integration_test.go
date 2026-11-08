package repository

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/orkestra/backend/internal/core/notification/models"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

func newTestDeviceRepo(t *testing.T) (*deviceTokenRepo, func()) {
	t.Helper()
	uri := os.Getenv("MONGO_TEST_URI")
	if uri == "" {
		t.Skip("set MONGO_TEST_URI to run live device-token repository tests")
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
	db := client.Database("orkestra_sdd_test_devtok_" + uuid.NewString())
	// Same unique indexes module.go's Collections() declares.
	_, err = db.Collection(models.DeviceTokensCollection).Indexes().CreateMany(ctx, []mongo.IndexModel{
		{Keys: bson.D{{Key: "uuid", Value: 1}}, Options: options.Index().SetUnique(true)},
		{Keys: bson.D{{Key: "tier", Value: 1}, {Key: "userUuid", Value: 1}, {Key: "deviceId", Value: 1}}, Options: options.Index().SetUnique(true)},
		{Keys: bson.D{{Key: "token", Value: 1}}, Options: options.Index().SetUnique(true).
			SetPartialFilterExpression(bson.D{{Key: "active", Value: true}})},
	})
	if err != nil {
		t.Fatalf("create indexes: %v", err)
	}
	return &deviceTokenRepo{db: db}, func() {
		_ = db.Drop(context.Background())
		_ = client.Disconnect(context.Background())
	}
}

func dev(tier, user, device, token string) *models.DeviceToken {
	return &models.DeviceToken{Tier: tier, UserUUID: user, DeviceID: device, SessionID: "s-" + user, Token: token, Platform: "android"}
}

func TestUpsertCreatesAndRefreshes(t *testing.T) {
	r, done := newTestDeviceRepo(t)
	defer done()
	ctx := context.Background()
	t0 := time.Now().UTC().Truncate(time.Millisecond)
	a, err := r.Upsert(ctx, dev("operator", "u1", "d1", "tok-1"), t0)
	if err != nil || a.UUID == "" || !a.Active || !a.CreatedAt.Equal(t0) {
		t.Fatalf("first upsert: %+v %v", a, err)
	}
	t1 := t0.Add(time.Hour)
	b, err := r.Upsert(ctx, dev("operator", "u1", "d1", "tok-2"), t1)
	if err != nil {
		t.Fatal(err)
	}
	if b.UUID != a.UUID || b.Token != "tok-2" || !b.LastSeenAt.Equal(t1) || !b.CreatedAt.Equal(t0) {
		t.Fatalf("refresh: %+v", b)
	}
	rows, _ := r.ListForUser(ctx, "operator", "u1")
	if len(rows) != 1 {
		t.Fatalf("rows=%d", len(rows))
	}
}

func TestUpsertReassignsSharedToken(t *testing.T) {
	r, done := newTestDeviceRepo(t)
	defer done()
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Millisecond)
	if _, err := r.Upsert(ctx, dev("operator", "u1", "tablet", "shared"), now); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Upsert(ctx, dev("operator", "u2", "tablet", "shared"), now.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	u1, _ := r.ListForUser(ctx, "operator", "u1")
	if len(u1) != 1 || u1[0].Active || u1[0].DisabledReason != models.DeviceDisabledReassigned || u1[0].DisabledAt == nil {
		t.Fatalf("u1: %+v", u1)
	}
	u2, _ := r.ActiveForUsers(ctx, "operator", []string{"u1", "u2"})
	if len(u2) != 1 || u2[0].UserUUID != "u2" {
		t.Fatalf("active: %+v", u2)
	}
	// The previous owner logging back in on the device takes it back.
	if _, err := r.Upsert(ctx, dev("operator", "u1", "tablet", "shared"), now.Add(2*time.Minute)); err != nil {
		t.Fatal(err)
	}
	act, _ := r.ActiveForUsers(ctx, "operator", []string{"u1", "u2"})
	if len(act) != 1 || act[0].UserUUID != "u1" || act[0].DisabledReason != "" || act[0].DisabledAt != nil {
		t.Fatalf("after re-login: %+v", act)
	}
}

func TestDisable(t *testing.T) {
	r, done := newTestDeviceRepo(t)
	defer done()
	ctx := context.Background()
	now := time.Now().UTC()
	_, _ = r.Upsert(ctx, dev("client", "u1", "d1", "t1"), now)
	ok, err := r.Disable(ctx, "client", "u1", "d1", models.DeviceDisabledLogout, now)
	if err != nil || !ok {
		t.Fatalf("disable: %v %v", ok, err)
	}
	ok, _ = r.Disable(ctx, "client", "u1", "d1", models.DeviceDisabledLogout, now)
	if ok {
		t.Fatal("second disable should report false")
	}
	_, _ = r.Upsert(ctx, dev("client", "u1", "d2", "t2"), now)
	if err := r.DisableByToken(ctx, "t2", models.DeviceDisabledFCMUnregistered, now); err != nil {
		t.Fatal(err)
	}
	rows, _ := r.ListForUser(ctx, "client", "u1")
	for _, x := range rows {
		if x.Active {
			t.Fatalf("still active: %+v", x)
		}
	}
}

func TestActiveForUsersSkipsDisabled(t *testing.T) {
	r, done := newTestDeviceRepo(t)
	defer done()
	ctx := context.Background()
	now := time.Now().UTC()
	_, _ = r.Upsert(ctx, dev("operator", "u1", "d1", "t1"), now)
	_, _ = r.Upsert(ctx, dev("operator", "u1", "d2", "t2"), now)
	_, _ = r.Upsert(ctx, dev("client", "u1", "d3", "t3"), now)
	_, _ = r.Disable(ctx, "operator", "u1", "d2", models.DeviceDisabledLogout, now)
	got, err := r.ActiveForUsers(ctx, "operator", []string{"u1"})
	if err != nil || len(got) != 1 || got[0].DeviceID != "d1" {
		t.Fatalf("got %+v %v", got, err)
	}
	if got, _ := r.ActiveForUsers(ctx, "operator", nil); len(got) != 0 {
		t.Fatal("empty users should return none")
	}
	batch, _ := r.ListActiveBatch(ctx, "", 10)
	if len(batch) != 2 || batch[0].UUID > batch[1].UUID {
		t.Fatalf("batch %+v", batch)
	}
	next, _ := r.ListActiveBatch(ctx, batch[0].UUID, 10)
	if len(next) != 1 || next[0].UUID != batch[1].UUID {
		t.Fatalf("next %+v", next)
	}
}

func TestDisableStale(t *testing.T) {
	r, done := newTestDeviceRepo(t)
	defer done()
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Millisecond)
	_, _ = r.Upsert(ctx, dev("operator", "u1", "old", "t-old"), now.Add(-100*24*time.Hour))
	_, _ = r.Upsert(ctx, dev("operator", "u1", "new", "t-new"), now)
	n, err := r.DisableStale(ctx, now.Add(-60*24*time.Hour), now)
	if err != nil || n != 1 {
		t.Fatalf("n=%d err=%v", n, err)
	}
	rows, _ := r.ListForUser(ctx, "operator", "u1")
	for _, x := range rows {
		if (x.DeviceID == "old") == x.Active {
			t.Fatalf("unexpected state %+v", x)
		}
		if x.DeviceID == "old" && x.DisabledReason != models.DeviceDisabledStale {
			t.Fatalf("reason %q", x.DisabledReason)
		}
	}
}

func TestDeleteDisabledBefore(t *testing.T) {
	r, done := newTestDeviceRepo(t)
	defer done()
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Millisecond)
	_, _ = r.Upsert(ctx, dev("operator", "u1", "a", "ta"), now)
	_, _ = r.Upsert(ctx, dev("operator", "u1", "b", "tb"), now)
	_, _ = r.Upsert(ctx, dev("operator", "u1", "c", "tc"), now)
	_, _ = r.Disable(ctx, "operator", "u1", "a", models.DeviceDisabledLogout, now.Add(-48*time.Hour))
	_, _ = r.Disable(ctx, "operator", "u1", "b", models.DeviceDisabledLogout, now)
	n, err := r.DeleteDisabledBefore(ctx, now.Add(-24*time.Hour))
	if err != nil || n != 1 {
		t.Fatalf("n=%d err=%v", n, err)
	}
	rows, _ := r.ListForUser(ctx, "operator", "u1")
	if len(rows) != 2 {
		t.Fatalf("rows=%d", len(rows))
	}
}

func TestDeleteForUser(t *testing.T) {
	r, done := newTestDeviceRepo(t)
	defer done()
	ctx := context.Background()
	now := time.Now().UTC()
	_, _ = r.Upsert(ctx, dev("operator", "u1", "a", "ta"), now)
	_, _ = r.Upsert(ctx, dev("operator", "u1", "b", "tb"), now)
	_, _ = r.Upsert(ctx, dev("client", "u1", "a", "tc"), now)
	_, _ = r.Upsert(ctx, dev("operator", "u2", "a", "td"), now)
	n, err := r.DeleteForUser(ctx, "operator", "u1")
	if err != nil || n != 2 {
		t.Fatalf("n=%d err=%v", n, err)
	}
	if rows, _ := r.ListForUser(ctx, "client", "u1"); len(rows) != 1 {
		t.Fatal("client row must survive")
	}
}

func TestTokenUniqueAmongActive(t *testing.T) {
	r, done := newTestDeviceRepo(t)
	defer done()
	ctx := context.Background()
	now := time.Now().UTC()
	mk := func(user string) bson.D {
		return bson.D{{Key: "uuid", Value: uuid.NewString()}, {Key: "tier", Value: "operator"}, {Key: "userUuid", Value: user},
			{Key: "deviceId", Value: "d"}, {Key: "token", Value: "dup"}, {Key: "active", Value: true}}
	}
	if _, err := r.col().InsertOne(ctx, mk("u1")); err != nil {
		t.Fatal(err)
	}
	if _, err := r.col().InsertOne(ctx, mk("u2")); !mongo.IsDuplicateKeyError(err) {
		t.Fatalf("expected duplicate key, got %v", err)
	}
	// An inactive row with the same token is allowed.
	_, _ = r.Disable(ctx, "operator", "u1", "d", models.DeviceDisabledLogout, now)
	if _, err := r.col().InsertOne(ctx, mk("u2")); err != nil {
		t.Fatalf("after disable: %v", err)
	}
}
