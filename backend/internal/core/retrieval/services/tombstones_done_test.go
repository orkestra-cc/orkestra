package services

import (
	"context"
	"testing"
	"time"

	"github.com/orkestra/backend/internal/core/retrieval/repository"
)

func TestErasureFinalization_DoneTombstoneStillCovers(t *testing.T) {
	db := newServicesMongoDB(t)
	ctx := context.Background()
	fin := repository.NewErasureFinalization(db)
	now := time.Now()
	if _, _, err := fin.EnsureUser(ctx, "u1", now); err != nil {
		t.Fatal(err)
	}
	e, err := fin.ClaimDue(ctx, now, time.Minute)
	if err != nil || e == nil {
		t.Fatalf("ClaimDue = %v, %v", e, err)
	}
	if err := fin.MarkDone(ctx, e.UUID, *e.NextAttemptAt, now); err != nil {
		t.Fatal(err)
	}
	tombs := NewTombstones(repository.NewErasures(db))
	got, err := tombs.Covered(tombCtx("tA"), "tA", "people", []Probe{{SourceID: "s1", Subjects: []string{"user:u1"}}})
	if err != nil {
		t.Fatal(err)
	}
	if !got["s1"] {
		t.Fatalf("done tombstone no longer covers: %v", got)
	}
}
