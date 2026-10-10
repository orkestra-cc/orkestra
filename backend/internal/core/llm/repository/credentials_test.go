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
	hijack := "hijack"
	if _, err := repo.Patch(ctxFor("t2"), "c-a", models.CredentialPatch{Name: &hijack}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("cross-tenant update err = %v", err)
	}
	if err := repo.Delete(ctxFor("t2"), "c-a"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("cross-tenant delete err = %v", err)
	}
	// Owner update bumps UpdatedAt.
	renamed := "renamed"
	if _, err := repo.Patch(ctxFor("t1"), "c-a", models.CredentialPatch{Name: &renamed}); err != nil {
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
	if err := NewGrants(db).Replace(context.Background(), "m-x", models.AccessGranted, "u", []string{"u1"}); err == nil {
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

// TestCredentials_StaleUpdateDoesNotRevertSecretOrCreator: Update is
// field-owned. A rename that read the credential before a rotation and an
// erasure finishes after them, and must not write back the old secret, the
// old display tail or the erased creator.
func TestCredentials_StaleUpdateDoesNotRevertSecretOrCreator(t *testing.T) {
	db := newTestDB(t)
	repo := NewCredentials(db)
	ctx := ctxFor("t1")
	now := time.Now().UTC().Add(-time.Hour)
	oldEnv := models.Envelope{Alg: models.EnvelopeAlgLocal, SchemaVersion: models.EnvelopeSchemaVersion, Ciphertext: "b2xk"}
	a := &models.Credential{UUID: "c-a", Name: "k", Provider: models.ProviderOpenAI, Secret: oldEnv, SecretLast4: "1111", Status: models.CredentialStatusActive, CreatedBy: "subject", CreatedAt: now, UpdatedAt: now}
	if err := repo.Insert(ctx, a); err != nil {
		t.Fatal(err)
	}
	stale, err := repo.Get(ctx, "c-a")
	if err != nil {
		t.Fatal(err)
	}
	// A rotation and an erasure land after the rename's read.
	newEnv := models.Envelope{Alg: models.EnvelopeAlgLocal, SchemaVersion: models.EnvelopeSchemaVersion, Ciphertext: "bmV3"}
	if _, err := repo.SetSecret(ctx, "c-a", newEnv, "2222"); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.PseudonymizeCreator(context.Background(), "subject"); err != nil {
		t.Fatal(err)
	}
	_ = stale
	renamed, disabled := "renamed", models.CredentialStatusDisabled
	if _, err := repo.Patch(ctx, "c-a", models.CredentialPatch{Name: &renamed, Status: &disabled}); err != nil {
		t.Fatalf("patch: %v", err)
	}
	got, err := repo.Get(ctx, "c-a")
	if err != nil {
		t.Fatal(err)
	}
	if got.Name != "renamed" || got.Status != models.CredentialStatusDisabled {
		t.Fatalf("patch fields not written: %+v", got)
	}
	if got.Secret.Ciphertext != "bmV3" || got.SecretLast4 != "2222" {
		t.Fatalf("stale update reverted the rotated secret: ciphertext=%q last4=%q", got.Secret.Ciphertext, got.SecretLast4)
	}
	if got.CreatedBy != models.ErasedActor {
		t.Fatalf("stale update restored createdBy = %q, want %q", got.CreatedBy, models.ErasedActor)
	}
	if !got.CreatedAt.Equal(a.CreatedAt.Truncate(time.Millisecond)) {
		t.Fatalf("createdAt rewritten: %v, want %v", got.CreatedAt, a.CreatedAt)
	}
}

// TestCredentials_SetSecretIsTargetedAndTenantScoped: SetSecret writes the
// envelope and the tail only, clears the tail for a short secret, and
// another org cannot reach the row.
func TestCredentials_SetSecretIsTargetedAndTenantScoped(t *testing.T) {
	db := newTestDB(t)
	repo := NewCredentials(db)
	ctx := ctxFor("t1")
	now := time.Now().UTC().Add(-time.Hour)
	a := &models.Credential{UUID: "c-a", Name: "k", Provider: models.ProviderOpenAI, BaseURL: "https://api.openai.com/v1", SecretLast4: "1111", Status: models.CredentialStatusDisabled, CreatedBy: "u-admin", CreatedAt: now, UpdatedAt: now}
	if err := repo.Insert(ctx, a); err != nil {
		t.Fatal(err)
	}
	env := models.Envelope{Alg: models.EnvelopeAlgLocal, SchemaVersion: models.EnvelopeSchemaVersion, Ciphertext: "bmV3"}
	if _, err := repo.SetSecret(ctxFor("t2"), "c-a", env, "2222"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("cross-tenant SetSecret err = %v", err)
	}
	if _, err := repo.SetSecret(ctx, "missing", env, "2222"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("unknown SetSecret err = %v", err)
	}
	if at, err := repo.SetSecret(ctx, "c-a", env, "2222"); err != nil || !at.After(now) {
		t.Fatalf("SetSecret: at=%v err=%v", at, err)
	}
	got, err := repo.Get(ctx, "c-a")
	if err != nil {
		t.Fatal(err)
	}
	if got.Secret.Ciphertext != "bmV3" || got.SecretLast4 != "2222" || !got.UpdatedAt.After(now) {
		t.Fatalf("after SetSecret = %+v", got)
	}
	if got.Name != "k" || got.Status != models.CredentialStatusDisabled || got.BaseURL != "https://api.openai.com/v1" || got.CreatedBy != "u-admin" {
		t.Fatalf("SetSecret touched other fields: %+v", got)
	}
	if _, err := repo.SetSecret(ctx, "c-a", env, ""); err != nil {
		t.Fatal(err)
	}
	if got, _ := repo.Get(ctx, "c-a"); got.SecretLast4 != "" {
		t.Fatalf("short secret kept tail %q", got.SecretLast4)
	}
}

// TestCredentials_PatchWritesOnlyProvidedFields: a name-only patch leaves a
// status and baseUrl changed after the caller's read alone; a provided empty
// baseUrl unsets it; unknown uuid and duplicate names map to the sentinels.
func TestCredentials_PatchWritesOnlyProvidedFields(t *testing.T) {
	db := newTestDB(t)
	ensureUniqueNameIndex(t, db, CollCredentials)
	repo := NewCredentials(db)
	ctx := ctxFor("t1")
	now := time.Now().UTC().Add(-time.Hour)
	mk := func(uuid, name string) *models.Credential {
		return &models.Credential{UUID: uuid, Name: name, Provider: models.ProviderOpenAICompatible, BaseURL: "https://one.example/v1", Status: models.CredentialStatusActive, CreatedBy: "u-admin", CreatedAt: now, UpdatedAt: now}
	}
	if err := repo.Insert(ctx, mk("c-a", "alpha")); err != nil {
		t.Fatal(err)
	}
	if err := repo.Insert(ctx, mk("c-b", "beta")); err != nil {
		t.Fatal(err)
	}

	// A concurrent writer disables and moves the endpoint after our read...
	disabled, other := models.CredentialStatusDisabled, "https://two.example/v1"
	if _, err := repo.Patch(ctx, "c-a", models.CredentialPatch{Status: &disabled, BaseURL: &other}); err != nil {
		t.Fatal(err)
	}
	// ...then a rename that only provided the name must not revert them.
	renamed := "alpha2"
	at, err := repo.Patch(ctx, "c-a", models.CredentialPatch{Name: &renamed})
	if err != nil {
		t.Fatalf("name-only patch: %v", err)
	}
	got, err := repo.Get(ctx, "c-a")
	if err != nil {
		t.Fatal(err)
	}
	if got.Name != "alpha2" || got.Status != models.CredentialStatusDisabled || got.BaseURL != "https://two.example/v1" {
		t.Fatalf("name-only patch touched other fields: %+v", got)
	}
	if !got.UpdatedAt.Equal(at.Truncate(time.Millisecond)) || !got.UpdatedAt.After(now) {
		t.Fatalf("updatedAt = %v, returned %v", got.UpdatedAt, at)
	}

	// A provided empty baseUrl clears the stored one.
	empty := ""
	if _, err := repo.Patch(ctx, "c-a", models.CredentialPatch{BaseURL: &empty}); err != nil {
		t.Fatal(err)
	}
	if got, _ := repo.Get(ctx, "c-a"); got.BaseURL != "" || got.Name != "alpha2" {
		t.Fatalf("empty baseUrl not unset (or name lost): %+v", got)
	}

	if _, err := repo.Patch(ctx, "missing", models.CredentialPatch{Name: &renamed}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("unknown uuid err = %v", err)
	}
	if _, err := repo.Patch(ctxFor("t2"), "c-a", models.CredentialPatch{Name: &renamed}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("cross-tenant err = %v", err)
	}
	dup := "beta"
	if _, err := repo.Patch(ctx, "c-a", models.CredentialPatch{Name: &dup}); !errors.Is(err, ErrDuplicateName) {
		t.Fatalf("duplicate err = %v", err)
	}
}
