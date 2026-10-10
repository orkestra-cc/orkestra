package repository

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/orkestra/backend/internal/core/llm/models"
)

func newModel(uuid, name, cred, status, by string) *models.Model {
	now := time.Now().UTC()
	return &models.Model{
		UUID: uuid, Name: name, Provider: models.ProviderMock, ModelID: "mock-1",
		CredentialRef: models.LLMCredentialRef{Kind: models.CredentialKindOrg, CredentialUUID: cred},
		Purposes:      []models.LLMModelPurpose{{Purpose: "chat", Priority: 1}},
		Access:        models.AccessGranted, Status: status, CreatedBy: by, CreatedAt: now, UpdatedAt: now,
	}
}

func TestModels_ScopedListsAndUniqueName(t *testing.T) {
	db := newTestDB(t)
	ensureUniqueNameIndex(t, db, CollModels)
	repo := NewModels(db)
	t1, t2 := ctxFor("t1"), ctxFor("t2")
	for _, m := range []*models.Model{
		newModel("m1", "a", "c1", models.ModelStatusActive, "u"),
		newModel("m2", "b", "c1", models.ModelStatusDisabled, "u"),
		newModel("m3", "c", "c2", models.ModelStatusActive, "u"),
	} {
		if err := repo.Insert(t1, m); err != nil {
			t.Fatal(err)
		}
	}
	if err := repo.Insert(t2, newModel("m4", "a", "c1", models.ModelStatusActive, "u")); err != nil {
		t.Fatalf("same name other tenant: %v", err)
	}
	if err := repo.Insert(t1, newModel("m5", "a", "c1", models.ModelStatusActive, "u")); !errors.Is(err, ErrDuplicateName) {
		t.Fatalf("duplicate err = %v", err)
	}
	ids := func(ms []models.Model, err error) []string {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
		out := []string{}
		for _, m := range ms {
			out = append(out, m.UUID)
		}
		return out
	}
	eq := func(got, want []string) {
		t.Helper()
		if len(got) != len(want) {
			t.Fatalf("got %v want %v", got, want)
		}
		for i := range got {
			if got[i] != want[i] {
				t.Fatalf("got %v want %v", got, want)
			}
		}
	}
	eq(ids(repo.List(t1)), []string{"m1", "m2", "m3"})
	eq(ids(repo.ListActive(t1)), []string{"m1", "m3"})
	eq(ids(repo.ListActiveByCredential(t1, "c1")), []string{"m1"})
	eq(ids(repo.ListActive(t2)), []string{"m4"})
	if _, err := repo.Get(t2, "m1"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("cross-tenant get err = %v", err)
	}

	m, err := repo.Get(t1, "m1")
	if err != nil {
		t.Fatal(err)
	}
	before := m.UpdatedAt
	m.Status = models.ModelStatusDisabled
	if err := repo.Update(t1, m); err != nil {
		t.Fatal(err)
	}
	if got, _ := repo.Get(t1, "m1"); got.Status != models.ModelStatusDisabled || !got.UpdatedAt.After(before) {
		t.Fatalf("after update = %+v", got)
	}
	if err := repo.Update(t2, m); !errors.Is(err, ErrNotFound) {
		t.Fatalf("cross-tenant update err = %v", err)
	}
	if err := repo.Delete(t2, "m1"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("cross-tenant delete err = %v", err)
	}
	if err := repo.Delete(t1, "m1"); err != nil {
		t.Fatal(err)
	}
}

func TestModels_DSRActorMethods(t *testing.T) {
	db := newTestDB(t)
	repo := NewModels(db)
	for _, x := range []struct {
		tenant string
		m      *models.Model
	}{
		{"t1", newModel("m1", "a", "c", models.ModelStatusActive, "subject")},
		{"t2", newModel("m2", "a", "c", models.ModelStatusActive, "subject")},
		{"t1", newModel("m3", "b", "c", models.ModelStatusActive, "other")},
	} {
		if err := repo.Insert(ctxFor(x.tenant), x.m); err != nil {
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
	for _, x := range []struct{ tenant, uuid, want string }{{"t1", "m1", models.ErasedActor}, {"t2", "m2", models.ErasedActor}, {"t1", "m3", "other"}} {
		got, err := repo.Get(ctxFor(x.tenant), x.uuid)
		if err != nil || got.CreatedBy != x.want {
			t.Fatalf("%s createdBy = %+v, %v; want %q", x.uuid, got, err, x.want)
		}
	}
}
