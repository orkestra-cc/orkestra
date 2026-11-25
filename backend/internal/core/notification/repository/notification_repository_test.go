package repository

// Mongo-gated tests for the delivery log page (same pattern as
// suppression_repository_test.go): random database, dropped afterwards.

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/orkestra/backend/internal/core/notification/models"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

func newTestNotificationRepo(t *testing.T) NotificationRepository {
	repo, _ := newTestNotificationRepoDB(t)
	return repo
}

func newTestNotificationRepoDB(t *testing.T) (NotificationRepository, *mongo.Database) {
	t.Helper()
	uri := os.Getenv("MONGO_TEST_URI")
	if uri == "" {
		t.Skip("set MONGO_TEST_URI to run live notification log repository tests")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	client, err := mongo.Connect(ctx, options.Client().ApplyURI(uri))
	if err != nil {
		t.Fatalf("mongo.Connect: %v", err)
	}
	db := client.Database("ork_test_notiflog_" + uuid.NewString()[:8])
	t.Cleanup(func() {
		_ = db.Drop(context.Background())
		_ = client.Disconnect(context.Background())
	})
	return NewNotificationRepository(db), db
}

func TestMongo_NotificationLogPageFiltersAndPages(t *testing.T) {
	repo := newTestNotificationRepo(t)
	ctx := context.Background()
	base := time.Date(2026, 11, 13, 10, 0, 0, 0, time.UTC)
	rows := []struct {
		addr, status, typ, category, errText string
	}{
		{"Alice@Example.test", models.StatusSuppressed, models.TypeTransactional, "auth.reset_password", "address_invalid"},
		{"bob@example.test", models.StatusSent, models.TypeTransactional, "auth.verify_email", ""},
		{"carol@other.test", models.StatusSuppressed, models.TypeMarketing, "marketing.newsletter", "marketing_optout"},
		{"a.b+c@example.test", models.StatusFailed, models.TypeMarketing, "marketing.newsletter", "sender=default err=x"},
		{"dave@example.test", models.StatusSent, models.TypeMarketing, "marketing.newsletter", ""},
	}
	for i, r := range rows {
		if err := repo.Create(ctx, &models.NotificationDoc{
			UUID: uuid.NewString(), Channel: models.ChannelEmail, Type: r.typ, Category: r.category,
			RecipientAddress: r.addr, Status: r.status, Error: r.errText,
			CreatedAt: base.Add(time.Duration(i) * time.Minute),
		}); err != nil {
			t.Fatalf("create: %v", err)
		}
	}

	// Newest first, with the total of the whole filter.
	items, total, err := repo.Page(ctx, Filter{}, 0, 2)
	if err != nil {
		t.Fatalf("page: %v", err)
	}
	if total != 5 || len(items) != 2 || items[0].RecipientAddress != "dave@example.test" || items[1].RecipientAddress != "a.b+c@example.test" {
		t.Fatalf("first page: total=%d items=%v", total, addrs(items))
	}
	items, _, err = repo.Page(ctx, Filter{}, 4, 2)
	if err != nil || len(items) != 1 || items[0].RecipientAddress != "Alice@Example.test" {
		t.Fatalf("last page: err=%v items=%v", err, addrs(items))
	}

	cases := []struct {
		name  string
		f     Filter
		total int64
	}{
		{"status", Filter{Status: models.StatusSuppressed}, 2},
		{"type", Filter{Type: models.TypeMarketing}, 3},
		{"category", Filter{Category: "marketing.newsletter"}, 3},
		{"address case-insensitive", Filter{AddressContains: "alice@EXAMPLE"}, 1},
		{"address literal, not a pattern", Filter{AddressContains: "a.b+c"}, 1},
		{"address dot is literal", Filter{AddressContains: "a.b+"}, 1},
		{"address no match", Filter{AddressContains: ".*"}, 0},
		{"combined", Filter{Status: models.StatusSent, Type: models.TypeMarketing}, 1},
	}
	for _, tc := range cases {
		items, total, err := repo.Page(ctx, tc.f, 0, 50)
		if err != nil {
			t.Fatalf("%s: %v", tc.name, err)
		}
		if total != tc.total || int64(len(items)) != tc.total {
			t.Errorf("%s: total=%d items=%v, want %d", tc.name, total, addrs(items), tc.total)
		}
	}

	// An empty result is an empty slice, so the API answers [] and not null.
	items, _, err = repo.Page(ctx, Filter{Status: models.StatusQueued}, 0, 10)
	if err != nil || items == nil || len(items) != 0 {
		t.Fatalf("empty page: err=%v items=%v", err, items)
	}
}

// TestMongo_NotificationLogPageUsesTheIndexes pins the indexes module.go
// declares for the page: unfiltered and by status, the newest-first sort with
// its _id tiebreak is read from an index, never sorted in memory.
func TestMongo_NotificationLogPageUsesTheIndexes(t *testing.T) {
	_, db := newTestNotificationRepoDB(t)
	ctx := context.Background()
	coll := db.Collection(models.NotificationMessagesCollection)
	if _, err := coll.Indexes().CreateMany(ctx, []mongo.IndexModel{
		{Keys: bson.D{{Key: "createdAt", Value: -1}, {Key: "_id", Value: -1}}},
		{Keys: bson.D{{Key: "status", Value: 1}, {Key: "createdAt", Value: -1}, {Key: "_id", Value: -1}}},
	}); err != nil {
		t.Fatalf("indexes: %v", err)
	}
	for _, filter := range []bson.D{{}, {{Key: "status", Value: models.StatusSuppressed}}} {
		var out bson.M
		err := db.RunCommand(ctx, bson.D{{Key: "explain", Value: bson.D{
			{Key: "find", Value: models.NotificationMessagesCollection},
			{Key: "filter", Value: filter},
			{Key: "sort", Value: bson.D{{Key: "createdAt", Value: -1}, {Key: "_id", Value: -1}}},
			{Key: "skip", Value: 25},
			{Key: "limit", Value: 25},
		}}}).Decode(&out)
		if err != nil {
			t.Fatalf("explain: %v", err)
		}
		plan, _ := json.Marshal(out["queryPlanner"].(bson.M)["winningPlan"])
		if strings.Contains(string(plan), `"SORT"`) || !strings.Contains(string(plan), "IXSCAN") {
			t.Fatalf("filter %v: the page sorts in memory or scans the collection: %s", filter, plan)
		}
	}
}

func addrs(items []*models.NotificationDoc) []string {
	out := make([]string, 0, len(items))
	for _, d := range items {
		out = append(out, d.RecipientAddress)
	}
	return out
}
