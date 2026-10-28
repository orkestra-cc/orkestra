package repository

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/orkestra/backend/internal/core/retrieval/models"
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
