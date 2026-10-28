package services

import (
	"context"
	"io"
	"log/slog"
	"slices"
	"testing"

	"github.com/orkestra/backend/internal/core/retrieval/engine"
	"github.com/orkestra/backend/internal/core/retrieval/engine/enginefake"
	"github.com/orkestra/backend/internal/core/retrieval/repository"
	"github.com/orkestra/backend/pkg/sdk/iface"
	"github.com/orkestra/backend/pkg/sdk/module"
)

// ADR-0023 acceptance: an erased user's data is never served from the moment
// its erasure is recorded, even when the engine cannot delete yet.
func TestPurgePersonalData_TombstoneVetoesSearchWhilePointsRemain(t *testing.T) {
	db := newServicesMongoDB(t)
	ensureIndexes(t, db, map[string][]module.IndexSpec{repository.CollErasures: repository.ErasureIndexes()})
	ctx := context.Background()

	fx := newSearchFixture(t)
	coll := fx.state.gens[0].Collection
	eng := enginefake.New()
	point := func(sourceID, subject string) engine.Point {
		return engine.Point{
			ID: engine.PointID("t-1", "contacts", sourceID, 0), Dense: []float32{1, 0, 0, 0}, Text: "x",
			Runtime: engine.Runtime{TenantID: "t-1", SourceID: sourceID, SourceRevision: 1,
				VisibleTo: []string{"tenant:*"}, Subjects: []string{subject}},
		}
	}
	eng.Seed(coll, point("s1", "user:alice"), point("s2", "crm:9"))
	eng.FailDeletes(engine.ErrUnavailable)

	quiet := slog.New(slog.NewTextHandler(io.Discard, nil))
	store := repository.NewErasureFinalization(db)
	logs := repository.NewSearchLogDSR(db)
	clk := newClock()
	fin := NewFinalizer(store, logs, &staticLister{cols: map[string][]string{"contacts": {coll}}}, eng, nil, nil, clk.Now, quiet)
	producer := NewPIIProducer(store, logs, fin, clk.Now, quiet)

	fx.tweak = func(d *SearchDeps) {
		d.Engine = eng
		d.Tombstones = NewTombstones(repository.NewErasures(db))
	}
	sourceIDs := func() []string {
		t.Helper()
		out, err := fx.search(iface.RetrievalSearchRequest{Corpus: "contacts", Query: "x", Mode: iface.RetrievalModeKeyword})
		require.NoError(t, err)
		var ids []string
		for _, h := range out.Hits {
			ids = append(ids, h.SourceID)
		}
		slices.Sort(ids)
		return ids
	}
	require.Equal(t, []string{"s1", "s2"}, sourceIDs()) // served before the erasure

	res, err := producer.PurgePersonalData(ctx, "alice", iface.EraseHardDelete)
	require.NoError(t, err)
	require.Equal(t, true, res.Pending)
	n, err := eng.Count(ctx, coll, engine.Selector{Subject: "user:alice"})
	require.NoError(t, err)
	require.Equal(t, int64(1), n)

	require.Equal(t, []string{"s2"}, sourceIDs())
}
