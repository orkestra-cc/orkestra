package repository

import (
	"context"
	"errors"
	"sort"
	"testing"

	"github.com/orkestra/backend/internal/core/retrieval/models"
	"go.mongodb.org/mongo-driver/mongo"
)

func newErasures(t *testing.T) (*Erasures, *mongo.Database) {
	t.Helper()
	db := newTestDB(t)
	ensureIndexes(t, db, CollErasures, ErasureIndexes())
	return NewErasures(db), db
}

func TestErasures_InsertValidatesShape(t *testing.T) {
	r, _ := newErasures(t)
	bad := map[string]struct {
		ctx context.Context
		e   models.Erasure
	}{
		"global with tenant":  {tctx("t1"), models.Erasure{Scope: "global", Kind: "user", TenantID: "t1", Subject: "user:u1"}},
		"global kind item":    {tctx("t1"), models.Erasure{Scope: "global", Kind: "item", Subject: "user:u1"}},
		"global bad subject":  {tctx("t1"), models.Erasure{Scope: "global", Kind: "user", Subject: "crm:1"}},
		"global empty user":   {tctx("t1"), models.Erasure{Scope: "global", Kind: "user", Subject: "user:"}},
		"global with corpus":  {tctx("t1"), models.Erasure{Scope: "global", Kind: "user", Subject: "user:u1", Corpus: "c"}},
		"tenant kind user":    {tctx("t1"), models.Erasure{Scope: "tenant", Kind: "user", Subject: "user:u1"}},
		"item without source": {tctx("t1"), models.Erasure{Scope: "tenant", Kind: "item", Corpus: "c"}},
		"subject empty":       {tctx("t1"), models.Erasure{Scope: "tenant", Kind: "subject"}},
		"foreign tenant":      {tctx("t1"), models.Erasure{Scope: "tenant", Kind: "tenant", TenantID: "t2"}},
		"unknown scope":       {tctx("t1"), models.Erasure{Scope: "x", Kind: "tenant"}},
	}
	for name, c := range bad {
		e := c.e
		if err := r.Insert(c.ctx, &e); !errors.Is(err, ErrInvalidErasure) {
			t.Errorf("%s: err = %v, want ErrInvalidErasure", name, err)
		}
	}
	e := models.Erasure{Scope: "tenant", Kind: "item", Corpus: "crm_contacts", SourceID: "a"}
	if err := r.Insert(tctx("t1"), &e); err != nil {
		t.Fatal(err)
	}
	if e.TenantID != "t1" || e.UUID == "" || e.CreatedAt.IsZero() {
		t.Fatalf("stored = %+v", e)
	}
}

func TestErasures_CoveringFindsGlobalAndTenantTombstones(t *testing.T) {
	r, _ := newErasures(t)
	ins := func(ctx context.Context, e models.Erasure) {
		t.Helper()
		if err := r.Insert(ctx, &e); err != nil {
			t.Fatal(err)
		}
	}
	ins(tctx("t1"), models.Erasure{Scope: "global", Kind: "user", Subject: "user:u1"})
	ins(tctx("t1"), models.Erasure{Scope: "tenant", Kind: "item", Corpus: "crm_contacts", SourceID: "a"})
	ins(tctx("t1"), models.Erasure{Scope: "tenant", Kind: "subject", Subject: "crm:1"})
	ins(tctx("t2"), models.Erasure{Scope: "tenant", Kind: "tenant"})
	ins(tctx("t1"), models.Erasure{Scope: "tenant", Kind: "item", Corpus: "other", SourceID: "a"})
	ins(tctx("t1"), models.Erasure{Scope: "tenant", Kind: "item", Corpus: "crm_contacts", SourceID: "zz"})

	got, err := r.Covering(tctx("t1"), "crm_contacts", []string{"a", "b"}, []string{"user:u1", "crm:1"})
	if err != nil {
		t.Fatal(err)
	}
	var kinds []string
	for _, e := range got {
		kinds = append(kinds, e.Scope+"/"+e.Kind)
	}
	sort.Strings(kinds)
	want := []string{"global/user", "tenant/item", "tenant/subject"}
	if len(kinds) != 3 || kinds[0] != want[0] || kinds[1] != want[1] || kinds[2] != want[2] {
		t.Fatalf("got %v", kinds)
	}
	// no user: subject -> only tenant tombstones
	got, err = r.Covering(tctx("t1"), "crm_contacts", []string{"a"}, []string{"crm:1"})
	if err != nil || len(got) != 2 {
		t.Fatalf("got %d, %v", len(got), err)
	}
}
