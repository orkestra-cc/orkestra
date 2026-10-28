package repository

import (
	"context"
	"fmt"
	"slices"
	"testing"
	"time"

	"github.com/orkestra/backend/internal/core/retrieval/models"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
)

// seedDSR inserts r1..r4: r1 {tA, u1}, r2 {tB, u2, subjects user:u1 crm:7},
// r3 {tA, u2, crm:7}, r4 {tB, u3}.
func seedDSR(t *testing.T) (*SearchLogDSR, *mongo.Database) {
	t.Helper()
	db := newTestDB(t)
	ensureIndexes(t, db, CollSearchLog, SearchLogIndexes())
	log := NewSearchLog(db)
	base := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	rows := []struct {
		tenant, uuid, user string
		subjects           []string
	}{
		{"tA", "r1", "u1", nil},
		{"tB", "r2", "u2", []string{"user:u1", "crm:7"}},
		{"tA", "r3", "u2", []string{"crm:7"}},
		{"tB", "r4", "u3", nil},
	}
	for i, r := range rows {
		err := log.Insert(tctx(r.tenant), &models.SearchLog{
			UUID: r.uuid, UserUUID: r.user, Corpus: "people", Subjects: r.subjects,
			Outcome: models.SearchOutcomeOK, CreatedAt: base.Add(time.Duration(i) * time.Minute),
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	return NewSearchLogDSR(db), db
}

func remaining(t *testing.T, db *mongo.Database) []string {
	t.Helper()
	var out []string
	for _, id := range []string{"r1", "r2", "r3", "r4"} {
		if count(t, db, bson.M{"uuid": id}) == 1 {
			out = append(out, id)
		}
	}
	return out
}

func TestSearchLogDSR_ListByUserMatchesSearcherOrSubjectAcrossTenants(t *testing.T) {
	r, _ := seedDSR(t)
	rows, err := r.ListByUserAllTenants(context.Background(), "u1")
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, row := range rows {
		got = append(got, row.UUID)
	}
	if !slices.Equal(got, []string{"r1", "r2"}) {
		t.Fatalf("got %v, want [r1 r2]", got)
	}
}

func TestSearchLogDSR_DeleteByUser(t *testing.T) {
	r, db := seedDSR(t)
	n, err := r.DeleteByUserAllTenants(context.Background(), "u1")
	if err != nil || n != 2 {
		t.Fatalf("deleted = %d, %v; want 2", n, err)
	}
	if got := remaining(t, db); !slices.Equal(got, []string{"r3", "r4"}) {
		t.Fatalf("remaining = %v", got)
	}
}

func TestSearchLogDSR_DeleteBySubjectStaysInTenant(t *testing.T) {
	r, db := seedDSR(t)
	n, err := r.DeleteBySubject(context.Background(), "tA", "crm:7")
	if err != nil || n != 1 {
		t.Fatalf("deleted = %d, %v; want 1", n, err)
	}
	if got := remaining(t, db); !slices.Equal(got, []string{"r1", "r2", "r4"}) {
		t.Fatalf("remaining = %v", got)
	}
}

func TestSearchLogDSR_DeleteByTenant(t *testing.T) {
	r, db := seedDSR(t)
	n, err := r.DeleteByTenant(context.Background(), "tB")
	if err != nil || n != 2 {
		t.Fatalf("deleted = %d, %v; want 2", n, err)
	}
	if got := remaining(t, db); !slices.Equal(got, []string{"r1", "r3"}) {
		t.Fatalf("remaining = %v", got)
	}
}

func TestSearchLogDSR_DeleteByTenantBatches(t *testing.T) {
	old := searchLogPurgeBatch
	searchLogPurgeBatch = 2
	t.Cleanup(func() { searchLogPurgeBatch = old })
	db := newTestDB(t)
	log := NewSearchLog(db)
	for i := 0; i < 5; i++ {
		if err := log.Insert(tctx("tB"), &models.SearchLog{UUID: fmt.Sprintf("b%d", i), Outcome: models.SearchOutcomeOK}); err != nil {
			t.Fatal(err)
		}
	}
	if err := log.Insert(tctx("tA"), &models.SearchLog{UUID: "a1", Outcome: models.SearchOutcomeOK}); err != nil {
		t.Fatal(err)
	}
	n, err := NewSearchLogDSR(db).DeleteByTenant(context.Background(), "tB")
	if err != nil || n != 5 {
		t.Fatalf("deleted = %d, %v; want 5", n, err)
	}
	if left := count(t, db, bson.M{}); left != 1 {
		t.Fatalf("rows left = %d, want 1", left)
	}
}
