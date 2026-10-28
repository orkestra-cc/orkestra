package services

import (
	"context"
	"errors"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/orkestra/backend/internal/core/retrieval/engine"
	"github.com/orkestra/backend/internal/core/retrieval/models"
)

// retractingLog is a searchLog that also records DeleteRow and MarkChecked
// calls. rows stays the insert log (as written); a successful delete, mark or
// sweep changes only the state stored() reports, like the repository would.
type retractingLog struct {
	searchLog
	dmu     sync.Mutex
	deleted []string
	delErr  error
	checked []string
	markErr error
	gone    map[string]bool // deleted or swept
	cleared map[string]bool // marked checked
}

func (l *retractingLog) DeleteRow(_ context.Context, uuid string) error {
	l.dmu.Lock()
	defer l.dmu.Unlock()
	l.deleted = append(l.deleted, uuid)
	if l.delErr != nil {
		return l.delErr
	}
	if l.gone == nil {
		l.gone = map[string]bool{}
	}
	l.gone[uuid] = true
	return nil
}

func (l *retractingLog) MarkChecked(_ context.Context, uuid string) error {
	l.dmu.Lock()
	defer l.dmu.Unlock()
	l.checked = append(l.checked, uuid)
	if l.markErr != nil {
		return l.markErr
	}
	if l.cleared == nil {
		l.cleared = map[string]bool{}
	}
	l.cleared[uuid] = true
	return nil
}

// stored is the rows still held, with their current unchecked marker.
func (l *retractingLog) stored() []models.SearchLog {
	l.mu.Lock()
	rows := slices.Clone(l.rows)
	l.mu.Unlock()
	l.dmu.Lock()
	defer l.dmu.Unlock()
	var out []models.SearchLog
	for _, r := range rows {
		if l.gone[r.UUID] {
			continue
		}
		row := *r
		if l.cleared[r.UUID] {
			row.Unchecked = false
		}
		out = append(out, row)
	}
	return out
}

// PurgeBefore and PurgeUncheckedBefore make the fake a SearchLogPurger; the
// unchecked sweep mirrors repository.SearchLog.PurgeUncheckedBefore over
// stored().
func (l *retractingLog) PurgeBefore(context.Context, time.Time) (int64, error) { return 0, nil }

func (l *retractingLog) PurgeUncheckedBefore(_ context.Context, cutoff time.Time) (int64, error) {
	var n int64
	for _, r := range l.stored() {
		if r.Unchecked && r.CreatedAt.Before(cutoff) {
			l.dmu.Lock()
			if l.gone == nil {
				l.gone = map[string]bool{}
			}
			l.gone[r.UUID] = true
			l.dmu.Unlock()
			n++
		}
	}
	return n, nil
}

// lateTombstones answers the inner fake on call 1 (the candidate veto) and,
// from call 2 on (the post-write re-check), coveredLater for every probe.
type lateTombstones struct {
	inner        *searchTombstones
	mu           sync.Mutex
	calls        int
	coveredLater bool
	errLater     error
	failLater    int // with errLater: fail only this many re-checks (0 = all)
	failed       int
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
	fail := l.errLater != nil && (l.failLater == 0 || l.failed < l.failLater)
	if fail {
		l.failed++
	}
	l.mu.Unlock()
	if fail {
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
	if !row.Unchecked || len(rl.checked) != 0 || len(rl.stored()) != 0 {
		t.Fatalf("written unchecked = %v, checked = %v, stored = %d", row.Unchecked, rl.checked, len(rl.stored()))
	}
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
	// G2: written provisional, then marked checked once nothing covers it.
	if len(rl.rows) != 1 || !rl.rows[0].Unchecked {
		t.Fatalf("written rows = %+v, want one unchecked", rl.rows)
	}
	if !slices.Equal(rl.checked, []string{rl.rows[0].UUID}) {
		t.Fatalf("checked = %v, want [%s]", rl.checked, rl.rows[0].UUID)
	}
	// A checked row is not touched by the unchecked sweep, however old.
	n, err := PurgeSearchLog(context.Background(), rl, DefaultSearchLogRetentionDays, time.Now().Add(time.Hour))
	if err != nil || n != 0 {
		t.Fatalf("sweep n = %d, err = %v", n, err)
	}
	if st := rl.stored(); len(st) != 1 || st[0].Unchecked {
		t.Fatalf("stored = %+v, want the checked row", st)
	}
}

func TestSearch_LogRetractCheckFailureKeepsRowAndWarns(t *testing.T) {
	late := &lateTombstones{errLater: errors.New("mongo down")}
	fx, rl := retractFixture(t, late)
	if _, err := fx.search(contactsReq()); err != nil {
		t.Fatal(err)
	}
	logs := fx.logs.String()
	const warn = "search log erasure re-check failed; the row is swept as unchecked"
	if len(rl.deleted) != 0 || strings.Count(logs, warn) != 1 || strings.Contains(logs, "mongo down") {
		t.Fatalf("deleted = %v, logs = %s", rl.deleted, logs)
	}
	if st := rl.stored(); len(rl.checked) != 0 || len(st) != 1 || !st[0].Unchecked {
		t.Fatalf("checked = %v, stored = %+v, want the row still unchecked", rl.checked, st)
	}
	if late.calls != 3 { // the candidate veto, the re-check and its one retry
		t.Fatalf("Covered calls = %d, want 3", late.calls)
	}
}

func TestSearch_LogRetractCheckRetriedOnceThenRetracts(t *testing.T) {
	late := &lateTombstones{coveredLater: true, errLater: errors.New("mongo down"), failLater: 1}
	fx, rl := retractFixture(t, late)
	if _, err := fx.search(contactsReq()); err != nil {
		t.Fatal(err)
	}
	if len(rl.rows) != 1 || len(rl.deleted) != 1 || rl.deleted[0] != rl.rows[0].UUID {
		t.Fatalf("rows = %d, deleted = %v", len(rl.rows), rl.deleted)
	}
	if late.calls != 3 || strings.Contains(fx.logs.String(), "re-check failed") {
		t.Fatalf("calls = %d, logs = %s", late.calls, fx.logs.String())
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
	// Nothing will re-check the row, so it is not written provisional.
	if len(fx.log.rows) != 1 || fx.log.rows[0].Unchecked {
		t.Fatalf("rows = %+v, want one confirmed row", fx.log.rows)
	}
}

// requireSweptAfterGrace: a recent unchecked row survives the retention run;
// once older than UncheckedSearchLogGrace the run deletes it.
func requireSweptAfterGrace(t *testing.T, rl *retractingLog) {
	t.Helper()
	st := rl.stored()
	if len(st) != 1 || !st[0].Unchecked {
		t.Fatalf("stored = %+v, want one unchecked row", st)
	}
	ctx := context.Background()
	at := st[0].CreatedAt
	if n, err := PurgeSearchLog(ctx, rl, DefaultSearchLogRetentionDays, at.Add(UncheckedSearchLogGrace-time.Second)); err != nil || n != 0 {
		t.Fatalf("within the grace: n = %d, err = %v", n, err)
	}
	if len(rl.stored()) != 1 {
		t.Fatal("a recent unchecked row was swept")
	}
	if n, err := PurgeSearchLog(ctx, rl, DefaultSearchLogRetentionDays, at.Add(UncheckedSearchLogGrace+time.Second)); err != nil || n != 1 {
		t.Fatalf("after the grace: n = %d, err = %v", n, err)
	}
	if len(rl.stored()) != 0 {
		t.Fatal("the unchecked row survived the sweep")
	}
}

// G2: a covered row whose delete fails stays unchecked, and the retention
// sweep removes it once older than the grace, even after the erasure is done.
func TestSearch_RetractDeleteFailureLeavesRowForTheSweep(t *testing.T) {
	late := &lateTombstones{coveredLater: true}
	fx, rl := retractFixture(t, late)
	rl.delErr = errors.New("boom")
	if _, err := fx.search(contactsReq()); err != nil {
		t.Fatal(err)
	}
	logs := fx.logs.String()
	const warn = "search log retraction failed; the row is swept as unchecked"
	if strings.Count(logs, warn) != 1 || strings.Contains(logs, "boom") {
		t.Fatalf("logs = %s", logs)
	}
	if len(rl.checked) != 0 {
		t.Fatalf("checked = %v, a covered row is never marked checked", rl.checked)
	}
	requireSweptAfterGrace(t, rl)
}

// G2: a row no erasure covers whose mark fails stays unchecked too, and is
// swept (fail-closed for privacy).
func TestSearch_RetractMarkFailureLeavesRowForTheSweep(t *testing.T) {
	late := &lateTombstones{}
	fx, rl := retractFixture(t, late)
	rl.markErr = errors.New("boom")
	if _, err := fx.search(contactsReq()); err != nil {
		t.Fatal(err)
	}
	logs := fx.logs.String()
	const warn = "search log row could not be marked checked; the row is swept as unchecked"
	if strings.Count(logs, warn) != 1 || strings.Contains(logs, "boom") || len(rl.deleted) != 0 {
		t.Fatalf("deleted = %v, logs = %s", rl.deleted, logs)
	}
	requireSweptAfterGrace(t, rl)
}

// G2: a failed re-check leaves the row for the sweep as well.
func TestSearch_RetractCheckFailureLeavesRowForTheSweep(t *testing.T) {
	late := &lateTombstones{errLater: errors.New("mongo down")}
	fx, rl := retractFixture(t, late)
	if _, err := fx.search(contactsReq()); err != nil {
		t.Fatal(err)
	}
	requireSweptAfterGrace(t, rl)
}
