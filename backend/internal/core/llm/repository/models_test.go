package repository

import (
	"context"
	"errors"
	"reflect"
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

func TestModels_SetAccessTouchesOnlyAccessAndIsTenantScoped(t *testing.T) {
	db := newTestDB(t)
	ensureUniqueNameIndex(t, db, CollModels)
	repo := NewModels(db)
	t1, t2 := ctxFor("t1"), ctxFor("t2")
	if err := repo.Insert(t1, newModel("m1", "a", "c1", models.ModelStatusActive, "u")); err != nil {
		t.Fatal(err)
	}
	if err := repo.Insert(t2, newModel("m2", "a", "c1", models.ModelStatusActive, "u")); err != nil {
		t.Fatal(err)
	}
	before, err := repo.Get(t1, "m1")
	if err != nil {
		t.Fatal(err)
	}

	if err := repo.SetAccess(t1, "m1", models.AccessEveryone); err != nil {
		t.Fatal(err)
	}
	after, err := repo.Get(t1, "m1")
	if err != nil {
		t.Fatal(err)
	}
	if after.Access != models.AccessEveryone || !after.UpdatedAt.After(before.UpdatedAt) {
		t.Fatalf("access %q updatedAt %v (before %v)", after.Access, after.UpdatedAt, before.UpdatedAt)
	}
	restored := *after
	restored.Access, restored.UpdatedAt = before.Access, before.UpdatedAt
	if !reflect.DeepEqual(restored, *before) {
		t.Fatalf("SetAccess changed more than access:\n before %+v\n after  %+v", before, after)
	}

	// The other org's model of the same shape is untouched, and a foreign or
	// unknown uuid is not found.
	if err := repo.SetAccess(t2, "m1", models.AccessEveryone); !errors.Is(err, ErrNotFound) {
		t.Fatalf("cross-tenant SetAccess err = %v", err)
	}
	if err := repo.SetAccess(t1, "nope", models.AccessEveryone); !errors.Is(err, ErrNotFound) {
		t.Fatalf("unknown SetAccess err = %v", err)
	}
	other, err := repo.Get(t2, "m2")
	if err != nil || other.Access != models.AccessGranted {
		t.Fatalf("other org model = %+v, %v", other, err)
	}
}

func TestModels_UpdateDoesNotRevertAccess(t *testing.T) {
	db := newTestDB(t)
	ensureUniqueNameIndex(t, db, CollModels)
	repo := NewModels(db)
	t1 := ctxFor("t1")
	if err := repo.Insert(t1, newModel("m1", "a", "c1", models.ModelStatusActive, "u")); err != nil {
		t.Fatal(err)
	}
	if err := repo.Insert(t1, newModel("m2", "b", "c1", models.ModelStatusActive, "u")); err != nil {
		t.Fatal(err)
	}
	stale, err := repo.Get(t1, "m1") // carries access granted
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.SetAccess(t1, "m1", models.AccessEveryone); err != nil {
		t.Fatal(err)
	}
	budget := 512
	stale.Name, stale.Status, stale.BudgetReserveOutputTokens = "renamed", models.ModelStatusDisabled, &budget
	if err := repo.Update(t1, stale); err != nil {
		t.Fatal(err)
	}
	got, err := repo.Get(t1, "m1")
	if err != nil {
		t.Fatal(err)
	}
	if got.Access != models.AccessEveryone {
		t.Fatalf("Update reverted access to %q", got.Access)
	}
	if got.Name != "renamed" || got.Status != models.ModelStatusDisabled || got.BudgetReserveOutputTokens == nil || *got.BudgetReserveOutputTokens != 512 {
		t.Fatalf("Update did not apply the editable fields: %+v", got)
	}
	if got.CreatedBy != "u" || got.TenantID != "t1" {
		t.Fatalf("immutable fields changed: %+v", got)
	}

	// Clearing the budget removes it; a name taken by another model is a
	// duplicate; another org cannot update the model.
	got.BudgetReserveOutputTokens = nil
	if err := repo.Update(t1, got); err != nil {
		t.Fatal(err)
	}
	if again, _ := repo.Get(t1, "m1"); again.BudgetReserveOutputTokens != nil {
		t.Fatalf("budget not cleared: %v", *again.BudgetReserveOutputTokens)
	}
	got.Name = "b"
	if err := repo.Update(t1, got); !errors.Is(err, ErrDuplicateName) {
		t.Fatalf("duplicate err = %v", err)
	}
	if err := repo.Update(ctxFor("t2"), got); !errors.Is(err, ErrNotFound) {
		t.Fatalf("cross-tenant Update err = %v", err)
	}
}
