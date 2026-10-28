package services

import (
	"context"
	"errors"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/orkestra/backend/internal/core/retrieval/engine"
)

// retractingLog is a searchLog that also records DeleteRow calls.
type retractingLog struct {
	searchLog
	dmu     sync.Mutex
	deleted []string
	delErr  error
}

func (l *retractingLog) DeleteRow(_ context.Context, uuid string) error {
	l.dmu.Lock()
	defer l.dmu.Unlock()
	l.deleted = append(l.deleted, uuid)
	return l.delErr
}

// lateTombstones answers the inner fake on call 1 (the candidate veto) and,
// from call 2 on (the post-write re-check), coveredLater for every probe.
type lateTombstones struct {
	inner        *searchTombstones
	mu           sync.Mutex
	calls        int
	coveredLater bool
	errLater     error
	probes       [][]Probe
}

func (l *lateTombstones) Covered(ctx context.Context, tenantID, corpus string, probes []Probe) (map[string]bool, error) {
	l.mu.Lock()
	l.calls++
	n := l.calls
	l.mu.Unlock()
	if n == 1 {
		return l.inner.Covered(ctx, tenantID, corpus, probes)
	}
	l.mu.Lock()
	l.probes = append(l.probes, probes)
	l.mu.Unlock()
	if l.errLater != nil {
		return nil, l.errLater
	}
	out := make(map[string]bool, len(probes))
	for _, p := range probes {
		out[p.SourceID] = l.coveredLater
	}
	return out, nil
}

func retractFixture(t *testing.T, late *lateTombstones) (*searchFixture, *retractingLog) {
	fx := newSearchFixture(t)
	rl := &retractingLog{}
	late.inner = fx.tombstones
	fx.tweak = func(d *SearchDeps) { d.Log = rl; d.Tombstones = late }
	return fx, rl
}

func TestSearch_LogRowRetractedWhenAnErasureLandsMidSearch(t *testing.T) {
	late := &lateTombstones{coveredLater: true}
	fx, rl := retractFixture(t, late)
	res, err := fx.search(contactsReq())
	if err != nil || len(res.Hits) != 3 {
		t.Fatalf("hits = %v, err = %v", res, err)
	}
	if len(rl.rows) != 1 || len(rl.deleted) != 1 || rl.deleted[0] != rl.rows[0].UUID {
		t.Fatalf("rows = %d, deleted = %v", len(rl.rows), rl.deleted)
	}
	row := rl.rows[0]
	want := slices.Clone(row.Subjects)
	want = append(want, "user:u-1")
	slices.Sort(want)
	if len(late.probes) != 1 || len(late.probes[0]) != 1 ||
		late.probes[0][0].SourceID != row.UUID || !slices.Equal(late.probes[0][0].Subjects, want) {
		t.Fatalf("probes = %+v, want subjects %v", late.probes, want)
	}
}

func TestSearch_LogRowKeptWhenNoErasureCoversIt(t *testing.T) {
	late := &lateTombstones{}
	fx, rl := retractFixture(t, late)
	if _, err := fx.search(contactsReq()); err != nil {
		t.Fatal(err)
	}
	if len(rl.deleted) != 0 || late.calls != 2 {
		t.Fatalf("deleted = %v, calls = %d", rl.deleted, late.calls)
	}
}

func TestSearch_LogRetractCheckFailureKeepsRowAndWarns(t *testing.T) {
	late := &lateTombstones{errLater: errors.New("mongo down")}
	fx, rl := retractFixture(t, late)
	if _, err := fx.search(contactsReq()); err != nil {
		t.Fatal(err)
	}
	logs := fx.logs.String()
	if len(rl.deleted) != 0 || !strings.Contains(logs, "search log erasure re-check failed") || strings.Contains(logs, "mongo down") {
		t.Fatalf("deleted = %v, logs = %s", rl.deleted, logs)
	}
}

func TestSearch_FailedSearchRowIsRecheckedToo(t *testing.T) {
	// The engine fails before the candidate veto, so the re-check is the first
	// Covered call; start the counter past the veto.
	late := &lateTombstones{coveredLater: true, calls: 1}
	fx, rl := retractFixture(t, late)
	fx.engine.err = engine.ErrUnavailable
	if _, err := fx.search(contactsReq()); err == nil {
		t.Fatal("want an error")
	}
	if len(rl.rows) != 1 || len(rl.deleted) != 1 || rl.deleted[0] != rl.rows[0].UUID {
		t.Fatalf("rows = %d, deleted = %v", len(rl.rows), rl.deleted)
	}
	if len(late.probes) != 1 || !slices.Equal(late.probes[0][0].Subjects, []string{"user:u-1"}) {
		t.Fatalf("probes = %+v", late.probes)
	}
}

func TestSearch_NoRetractorNoRecheck(t *testing.T) {
	fx := newSearchFixture(t)
	if _, err := fx.search(contactsReq()); err != nil {
		t.Fatal(err)
	}
	if len(fx.tombstones.probes) != 1 {
		t.Fatalf("Covered calls = %d, want 1", len(fx.tombstones.probes))
	}
}

func TestSearch_RetractDeleteFailureWarns(t *testing.T) {
	late := &lateTombstones{coveredLater: true}
	fx, rl := retractFixture(t, late)
	rl.delErr = errors.New("boom")
	if _, err := fx.search(contactsReq()); err != nil {
		t.Fatal(err)
	}
	logs := fx.logs.String()
	if strings.Count(logs, "search log retraction failed") != 1 || strings.Contains(logs, "boom") {
		t.Fatalf("logs = %s", logs)
	}
}
