package repository

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/orkestra/backend/internal/core/llm/models"
	"github.com/orkestra/backend/internal/testkit"
	"github.com/orkestra/backend/pkg/sdk/ctxauth"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

func ctxFor(tenant string) context.Context {
	id := testkit.NewIdentity("u-admin", "admin@example.test", "administrator").WithTenant(tenant, []string{"org_owner"}, true)
	return ctxauth.WithTenantKind(id.ContextFor(context.Background(), tenant), "internal")
}

func ensureUniqueNameIndex(t *testing.T, db *mongo.Database, coll string) {
	t.Helper()
	_, err := db.Collection(coll).Indexes().CreateOne(context.Background(), mongo.IndexModel{
		Keys:    bson.D{{Key: "tenantId", Value: 1}, {Key: "name", Value: 1}},
		Options: options.Index().SetUnique(true),
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestCredentials_ScopedByTenantAndUniqueName(t *testing.T) {
	db := newTestDB(t)
	ensureUniqueNameIndex(t, db, CollCredentials)
	repo := NewCredentials(db)
	now := time.Now().UTC()
	a := &models.Credential{UUID: "c-a", Name: "OpenAI prod", Provider: models.ProviderOpenAI, Status: models.CredentialStatusActive, CreatedBy: "u-admin", CreatedAt: now, UpdatedAt: now}
	if err := repo.Insert(ctxFor("t1"), a); err != nil {
		t.Fatalf("insert: %v", err)
	}
	// Same name, other tenant: allowed.
	b := *a
	b.UUID = "c-b"
	if err := repo.Insert(ctxFor("t2"), &b); err != nil {
		t.Fatalf("insert other tenant: %v", err)
	}
	// Same name, same tenant: duplicate.
	c := *a
	c.UUID = "c-c"
	if err := repo.Insert(ctxFor("t1"), &c); !errors.Is(err, ErrDuplicateName) {
		t.Fatalf("duplicate err = %v", err)
	}
	// List is scoped.
	list, err := repo.List(ctxFor("t1"))
	if err != nil || len(list) != 1 || list[0].UUID != "c-a" {
		t.Fatalf("list t1 = %+v, %v", list, err)
	}
	// Get across tenants misses.
	if _, err := repo.Get(ctxFor("t2"), "c-a"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("cross-tenant get err = %v", err)
	}
}

func TestCredentials_UpdateAndDeleteAreTenantScoped(t *testing.T) {
	db := newTestDB(t)
	repo := NewCredentials(db)
	now := time.Now().UTC().Add(-time.Hour)
	a := &models.Credential{UUID: "c-a", Name: "k", Provider: models.ProviderMock, Status: models.CredentialStatusActive, CreatedBy: "u-admin", CreatedAt: now, UpdatedAt: now}
	if err := repo.Insert(ctxFor("t1"), a); err != nil {
		t.Fatal(err)
	}
	// Other tenant cannot update or delete it.
	other := *a
	other.Name = "hijack"
	if err := repo.Update(ctxFor("t2"), &other); !errors.Is(err, ErrNotFound) {
		t.Fatalf("cross-tenant update err = %v", err)
	}
	if err := repo.Delete(ctxFor("t2"), "c-a"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("cross-tenant delete err = %v", err)
	}
	// Owner update bumps UpdatedAt.
	a.Name = "renamed"
	if err := repo.Update(ctxFor("t1"), a); err != nil {
		t.Fatalf("update: %v", err)
	}
	got, err := repo.Get(ctxFor("t1"), "c-a")
	if err != nil || got.Name != "renamed" || got.TenantID != "t1" || !got.UpdatedAt.After(now) {
		t.Fatalf("after update = %+v, %v", got, err)
	}
	if err := repo.Delete(ctxFor("t1"), "c-a"); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if err := repo.Delete(ctxFor("t1"), "c-a"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("second delete err = %v", err)
	}
}

// StampInsert panics in dev (ENV unset) and errors otherwise; ENVIRONMENT
// takes precedence over ENV in tenantrepo.isDev, so both are set.
func setProductionEnv(t *testing.T) {
	t.Helper()
	t.Setenv("ENVIRONMENT", "production")
	t.Setenv("ENV", "production")
}

func TestInsertWithoutTenantFailsClosed(t *testing.T) {
	setProductionEnv(t)
	db := newTestDB(t)
	if err := NewCredentials(db).Insert(context.Background(), &models.Credential{UUID: "c-x", Name: "x"}); err == nil {
		t.Fatal("credential insert without tenant must fail")
	}
	if err := NewModels(db).Insert(context.Background(), &models.Model{UUID: "m-x", Name: "x"}); err == nil {
		t.Fatal("model insert without tenant must fail")
	}
	if err := NewGrants(db).Replace(context.Background(), "m-x", "u", []string{"u1"}); err == nil {
		t.Fatal("grants replace without tenant must fail")
	}
	if _, err := NewCredentials(db).List(context.Background()); err == nil {
		t.Fatal("list without tenant must fail")
	}
}

func TestInsertWithoutTenantPanicsInDev(t *testing.T) {
	t.Setenv("ENVIRONMENT", "development")
	t.Setenv("ENV", "development")
	db := newTestDB(t)
	defer func() {
		if recover() == nil {
			t.Fatal("expected panic in dev")
		}
	}()
	_ = NewCredentials(db).Insert(context.Background(), &models.Credential{UUID: "c-x", Name: "x"})
}

func TestCredentials_DSRActorMethods(t *testing.T) {
	db := newTestDB(t)
	repo := NewCredentials(db)
	now := time.Now().UTC()
	mk := func(uuid, by string) *models.Credential {
		return &models.Credential{UUID: uuid, Name: uuid, Provider: models.ProviderMock, Status: models.CredentialStatusActive, CreatedBy: by, CreatedAt: now, UpdatedAt: now}
	}
	for _, x := range []struct {
		tenant string
		c      *models.Credential
	}{
		{"t1", mk("c1", "subject")}, {"t2", mk("c2", "subject")}, {"t1", mk("c3", "other")},
	} {
		if err := repo.Insert(ctxFor(x.tenant), x.c); err != nil {
			t.Fatal(err)
		}
	}
	list, err := repo.ListByCreator(context.Background(), "subject")
	if err != nil || len(list) != 2 {
		t.Fatalf("ListByCreator = %+v, %v", list, err)
	}
	n, err := repo.PseudonymizeCreator(context.Background(), "subject")
	if err != nil || n != 2 {
		t.Fatalf("PseudonymizeCreator = %d, %v", n, err)
	}
	if left, _ := repo.ListByCreator(context.Background(), "subject"); len(left) != 0 {
		t.Fatalf("subject still present: %+v", left)
	}
	for _, x := range []struct{ tenant, uuid, want string }{{"t1", "c1", models.ErasedActor}, {"t2", "c2", models.ErasedActor}, {"t1", "c3", "other"}} {
		got, err := repo.Get(ctxFor(x.tenant), x.uuid)
		if err != nil || got.CreatedBy != x.want {
			t.Fatalf("%s createdBy = %+v, %v; want %q", x.uuid, got, err, x.want)
		}
	}
}
