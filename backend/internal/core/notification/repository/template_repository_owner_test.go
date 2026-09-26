package repository

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/orkestra/backend/internal/core/notification/models"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

func newOwnerTestRepo(t *testing.T) (TemplateRepository, *mongo.Collection) {
	t.Helper()
	uri := os.Getenv("MONGO_TEST_URI")
	if uri == "" {
		t.Skip("set MONGO_TEST_URI")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	client, err := mongo.Connect(ctx, options.Client().ApplyURI(uri))
	if err != nil {
		t.Fatal(err)
	}
	suffix := make([]byte, 4)
	_, _ = rand.Read(suffix)
	db := client.Database("orkestra_test_notif_tpl_" + hex.EncodeToString(suffix))
	t.Cleanup(func() {
		c, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = db.Drop(c)
		_ = client.Disconnect(c)
	})
	coll := db.Collection(models.NotificationTemplatesCollection)
	_, err = coll.Indexes().CreateOne(ctx, mongo.IndexModel{
		Keys:    bson.D{{Key: "ownerTenantId", Value: 1}, {Key: "templateId", Value: 1}, {Key: "locale", Value: 1}},
		Options: options.Index().SetUnique(true),
	})
	if err != nil {
		t.Fatal(err)
	}
	return NewTemplateRepository(db), coll
}

func TestOwnedTemplatesAreIsolatedPerTenant(t *testing.T) {
	repo, _ := newOwnerTestRepo(t)
	ctx := context.Background()
	a := &models.TemplateDoc{OwnerTenantID: "A", TemplateID: "digest:0f0e0d0c-0b0a-4908-8706-050403020100", Locale: "it", Subject: "A"}
	if err := repo.Create(ctx, a); err != nil {
		t.Fatal(err)
	}
	if err := repo.Create(ctx, a); !errors.Is(err, ErrExists) {
		t.Fatalf("second Create: want ErrExists, got %v", err)
	}
	if _, err := repo.GetOwned(ctx, "B", a.TemplateID, "it"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("tenant B must not see A's snapshot, got %v", err)
	}
	got, err := repo.GetOwned(ctx, "A", a.TemplateID, "it")
	if err != nil || got.Subject != "A" {
		t.Fatalf("owner read: %v %+v", err, got)
	}
	// Same key for tenant B is a different document.
	b := &models.TemplateDoc{OwnerTenantID: "B", TemplateID: a.TemplateID, Locale: "it", Subject: "B"}
	if err := repo.Create(ctx, b); err != nil {
		t.Fatalf("same id for another owner must be allowed: %v", err)
	}
}

func TestSystemQueriesIgnoreOwnedDocs(t *testing.T) {
	repo, coll := newOwnerTestRepo(t)
	ctx := context.Background()
	// Legacy system doc: no ownerTenantId field at all.
	_, _ = coll.InsertOne(ctx, bson.M{"uuid": "u1", "templateId": "auth.verify_email", "locale": "en", "subject": "legacy", "isSystem": true})
	_ = repo.Create(ctx, &models.TemplateDoc{OwnerTenantID: "A", TemplateID: "auth.verify_email", Locale: "en", Subject: "owned"})
	sys, err := repo.GetByID(ctx, "auth.verify_email", "en")
	if err != nil || sys.Subject != "legacy" {
		t.Fatalf("system read must return the unowned doc: %v %+v", err, sys)
	}
	list, _ := repo.List(ctx)
	if len(list) != 1 || list[0].Subject != "legacy" {
		t.Fatalf("List must return only system docs, got %+v", list)
	}
	// Upsert (system) on the legacy doc stamps ownerTenantId "" without duplicating.
	if err := repo.Upsert(ctx, &models.TemplateDoc{TemplateID: "auth.verify_email", Locale: "en", Subject: "edited", IsSystem: true}); err != nil {
		t.Fatal(err)
	}
	//tenantscope:allow system: test assertion counts system rows in one isolated per-run test database
	n, _ := coll.CountDocuments(ctx, bson.M{"templateId": "auth.verify_email", "locale": "en", "ownerTenantId": bson.M{"$in": bson.A{"", nil}}})
	if n != 1 {
		t.Fatalf("system upsert must not duplicate, count=%d", n)
	}
}

func TestDeleteOwnedByPrefixIsOwnerBound(t *testing.T) {
	repo, coll := newOwnerTestRepo(t)
	ctx := context.Background()
	const id = "digest:0f0e0d0c-0b0a-4908-8706-050403020100"
	_ = repo.Create(ctx, &models.TemplateDoc{OwnerTenantID: "A", TemplateID: id, Locale: "it"})
	_ = repo.Create(ctx, &models.TemplateDoc{OwnerTenantID: "A", TemplateID: id + ":run:1", Locale: "it"})
	_ = repo.Create(ctx, &models.TemplateDoc{OwnerTenantID: "B", TemplateID: id, Locale: "it"})
	_, _ = coll.InsertOne(ctx, bson.M{"uuid": "sys", "templateId": id, "locale": "en"}) // legacy, unowned
	n, err := repo.DeleteOwnedByPrefix(ctx, "A", id)
	if err != nil || n != 2 {
		t.Fatalf("want 2 deleted for A, got %d %v", n, err)
	}
	if _, err := repo.DeleteOwnedByPrefix(ctx, "", id); err == nil {
		t.Fatal("empty owner must be refused")
	}
	for _, bad := range []string{"digest:", "digest", "preview:test:" + "0f0e0d0c-0b0a-4908-8706-050403020100" + ":x"} {
		if _, err := repo.DeleteOwnedByPrefix(ctx, "A", bad); err == nil {
			t.Fatalf("prefix %q must be refused", bad)
		}
	}
	exact := "preview:test:0f0e0d0c-0b0a-4908-8706-050403020100:1b2c3d4e-0000-4000-8000-000000000001"
	_ = repo.Create(ctx, &models.TemplateDoc{OwnerTenantID: "A", TemplateID: exact, Locale: "it"})
	_ = repo.Create(ctx, &models.TemplateDoc{OwnerTenantID: "A", TemplateID: "preview:test:0f0e0d0c-0b0a-4908-8706-050403020100:1b2c3d4e-0000-4000-8000-000000000002", Locale: "it"})
	if n, err := repo.DeleteOwnedByPrefix(ctx, "A", exact); err != nil || n != 1 {
		t.Fatalf("an exact test-send key deletes only itself: %d %v", n, err)
	}
	// Survivors: B's snapshot, the legacy unowned row, and A's sibling
	// test-send snapshot (…0002) that the exact prefix must not sweep.
	//tenantscope:allow system: test assertion counts every row in one isolated per-run test database
	rest, _ := coll.CountDocuments(ctx, bson.M{})
	if rest != 3 {
		t.Fatalf("B's, the legacy and the sibling test-send doc must survive, count=%d", rest)
	}
}

func TestUpsertOwnedIsOwnerBound(t *testing.T) {
	repo, coll := newOwnerTestRepo(t)
	ctx := context.Background()
	const id, loc = "digest:0f0e0d0c-0b0a-4908-8706-050403020100", "it"
	// A system row and tenant B's row already carry this (templateId, locale).
	if err := repo.Upsert(ctx, &models.TemplateDoc{TemplateID: id, Locale: loc, Subject: "system"}); err != nil {
		t.Fatal(err)
	}
	if err := repo.Create(ctx, &models.TemplateDoc{OwnerTenantID: "B", TemplateID: id, Locale: loc, Subject: "B"}); err != nil {
		t.Fatal(err)
	}
	// A's write must land on A's own document — never on B's, never on the
	// system one. Dropping ownerTenantId from the filter makes this update hit
	// whichever of the two Mongo happens to return first: that is the
	// cross-tenant overwrite this whole change exists to prevent.
	if err := repo.UpsertOwned(ctx, &models.TemplateDoc{OwnerTenantID: "A", TemplateID: id, Locale: loc, Subject: "A"}); err != nil {
		t.Fatal(err)
	}
	if got, err := repo.GetOwned(ctx, "B", id, loc); err != nil || got.Subject != "B" {
		t.Fatalf("tenant B's snapshot must be untouched: %v %+v", err, got)
	}
	if got, err := repo.GetOwned(ctx, "A", id, loc); err != nil || got.Subject != "A" {
		t.Fatalf("owner read after UpsertOwned: %v %+v", err, got)
	}
	if sys, err := repo.GetByID(ctx, id, loc); err != nil || sys.Subject != "system" {
		t.Fatalf("the system row must be untouched: %v %+v", err, sys)
	}
	// Three distinct documents, not one overwritten in place.
	//tenantscope:allow system: test assertion counts the three owners of one key in an isolated per-run test database
	n, _ := coll.CountDocuments(ctx, bson.M{"templateId": id, "locale": loc})
	if n != 3 {
		t.Fatalf("system + A + B must be three documents, count=%d", n)
	}
	// A second UpsertOwned updates in place rather than inserting a fourth.
	if err := repo.UpsertOwned(ctx, &models.TemplateDoc{OwnerTenantID: "A", TemplateID: id, Locale: loc, Subject: "A2"}); err != nil {
		t.Fatal(err)
	}
	//tenantscope:allow system: test assertion counts the three owners of one key in an isolated per-run test database
	n, _ = coll.CountDocuments(ctx, bson.M{"templateId": id, "locale": loc})
	if got, err := repo.GetOwned(ctx, "A", id, loc); err != nil || got.Subject != "A2" || n != 3 {
		t.Fatalf("re-upsert must update in place: %v %+v count=%d", err, got, n)
	}
	if err := repo.UpsertOwned(ctx, &models.TemplateDoc{TemplateID: id, Locale: loc}); err == nil {
		t.Fatal("empty owner must be refused")
	}
}

func TestExistsSystemTemplateIgnoresOwnedDocs(t *testing.T) {
	repo, _ := newOwnerTestRepo(t)
	ctx := context.Background()
	const id, loc = "auth.verify_email", "en"
	if err := repo.Create(ctx, &models.TemplateDoc{OwnerTenantID: "A", TemplateID: id, Locale: loc, Subject: "owned"}); err != nil {
		t.Fatal(err)
	}
	// Only tenant A owns this key; the system template is still unseeded. A
	// filter that ignored ownerTenantId would report it seeded and SeedDefaults
	// would skip it, leaving the deployment with no system template at all.
	ok, err := repo.ExistsSystemTemplate(ctx, id, loc)
	if err != nil {
		t.Fatal(err)
	}
	if ok {
		t.Fatal("an owned row must not make the system template look seeded")
	}
	if err := repo.Upsert(ctx, &models.TemplateDoc{TemplateID: id, Locale: loc, Subject: "system", IsSystem: true}); err != nil {
		t.Fatal(err)
	}
	if ok, err := repo.ExistsSystemTemplate(ctx, id, loc); err != nil || !ok {
		t.Fatalf("the seeded system row must be found: %v %v", ok, err)
	}
}
