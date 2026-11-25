package repository

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/orkestra/backend/internal/core/retrieval/models"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
)

func TestCorpusStates_InsertGetSaveCAS(t *testing.T) {
	db := newTestDB(t)
	r := NewCorpusStates(db)
	ctx := context.Background()
	if got, err := r.Get(ctx, "crm_contacts"); err != nil || got != nil {
		t.Fatalf("Get absent = %v, %v", got, err)
	}
	now := time.Date(2026, 10, 26, 10, 0, 0, 0, time.UTC)
	st := &models.CorpusState{Corpus: "crm_contacts", Owner: "crm", Status: models.CorpusStatusActive, Locality: "local_only", CreatedAt: now, UpdatedAt: now}
	if err := r.Insert(ctx, st); err != nil || st.Version != 1 {
		t.Fatalf("Insert = %v, version %d", err, st.Version)
	}
	if err := r.Insert(ctx, &models.CorpusState{Corpus: "crm_contacts"}); !errors.Is(err, ErrStateExists) {
		t.Fatalf("second Insert = %v", err)
	}
	a, _ := r.Get(ctx, "crm_contacts")
	b, _ := r.Get(ctx, "crm_contacts")
	a.Status = models.CorpusStatusProfileChanged
	if err := r.Save(ctx, a); err != nil || a.Version != 2 {
		t.Fatalf("Save a = %v, version %d", err, a.Version)
	}
	b.Status = models.CorpusStatusError
	if err := r.Save(ctx, b); !errors.Is(err, ErrStateConflict) {
		t.Fatalf("Save b = %v", err)
	}
	got, _ := r.Get(ctx, "crm_contacts")
	if got.Status != models.CorpusStatusProfileChanged || got.Version != 2 {
		t.Fatalf("stored = %+v", got)
	}
	all, err := r.List(ctx)
	if err != nil || len(all) != 1 {
		t.Fatalf("List = %v, %v", all, err)
	}
}

// Save updates only the fields the worker owns: a field another version of
// the code wrote (delivery step 2's generation being built) and createdAt
// survive, and a cleared omitempty field is removed, as Insert would store it.
func TestCorpusStates_SaveKeepsUnknownFieldsAndCreatedAt(t *testing.T) {
	db := newTestDB(t)
	r := NewCorpusStates(db)
	ctx := context.Background()
	created := time.Date(2026, 10, 26, 10, 0, 0, 0, time.UTC)
	st := &models.CorpusState{Corpus: "crm_contacts", Owner: "crm", Status: models.CorpusStatusError, Reason: models.ReasonLocality,
		ActiveGeneration: 1, Collection: "orkestra_crm_contacts_g1", IngestionProfileHash: "ing_a", EmbeddingProfileID: "ip_a",
		Dimensions: 8, Locality: "local_only", DeclaredProfileHash: "ing_b", CreatedAt: created, UpdatedAt: created}
	if err := r.Insert(ctx, st); err != nil {
		t.Fatal(err)
	}
	//tenantscope:allow system: test writes a field of a later delivery step into the installation-wide corpus state
	if _, err := db.Collection(CollCorpusState).UpdateOne(ctx, bson.M{"_id": "crm_contacts"}, bson.M{"$set": bson.M{"buildingGeneration": 2}}); err != nil {
		t.Fatal(err)
	}

	later := created.Add(time.Hour)
	st.Status, st.Reason, st.DeclaredProfileHash, st.UpdatedAt = models.CorpusStatusActive, "", "", later
	st.CreatedAt = later // Save never writes createdAt
	if err := r.Save(ctx, st); err != nil || st.Version != 2 {
		t.Fatalf("Save = %v, version %d", err, st.Version)
	}

	var raw bson.M
	//tenantscope:allow system: test reads the installation-wide corpus state document as stored
	if err := db.Collection(CollCorpusState).FindOne(ctx, bson.M{"_id": "crm_contacts"}).Decode(&raw); err != nil {
		t.Fatal(err)
	}
	if raw["buildingGeneration"] != int32(2) {
		t.Fatalf("buildingGeneration = %#v", raw["buildingGeneration"])
	}
	if got := raw["createdAt"].(primitive.DateTime).Time().UTC(); !got.Equal(created) {
		t.Fatalf("createdAt = %v, want %v", got, created)
	}
	for _, k := range []string{"reason", "declaredProfileHash"} {
		if _, ok := raw[k]; ok {
			t.Fatalf("%s kept after clearing: %#v", k, raw[k])
		}
	}
	got, _ := r.Get(ctx, "crm_contacts")
	if got.Status != models.CorpusStatusActive || got.Version != 2 || got.Collection != "orkestra_crm_contacts_g1" ||
		got.Dimensions != 8 || !got.UpdatedAt.Equal(later) {
		t.Fatalf("stored = %+v", got)
	}

	// a stale version still conflicts
	st.Version = 1
	if err := r.Save(ctx, st); !errors.Is(err, ErrStateConflict) {
		t.Fatalf("stale Save = %v", err)
	}
}
