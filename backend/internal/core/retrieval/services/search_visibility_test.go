package services

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/orkestra/backend/internal/core/retrieval/engine"
	"github.com/orkestra/backend/pkg/sdk/iface"
)

// --- Step 6: erasure veto, source visibility check, over-fetch refill ---

// hitSources lists the hits' SourceIDs in rank order.
func hitSources(res *iface.RetrievalSearchResult) []string {
	out := []string{}
	for _, h := range res.Hits {
		out = append(out, h.SourceID)
	}
	return out
}

// candRange builds candidates prefix<from>..prefix<to>, one chunk each, at
// revision 1.
func candRange(prefix string, from, to int) []engine.Candidate {
	var out []engine.Candidate
	for i := from; i <= to; i++ {
		id := fmt.Sprintf("%s%d", prefix, i)
		out = append(out, cand(id, 1, 0, "candidate "+id+" for the head of sales"))
	}
	return out
}

// refuse marks every id as not visible at revision 1.
func refuse(fx *searchFixture, ids ...string) {
	if fx.source.visible == nil {
		fx.source.visible = map[string]iface.RetrievalVisibility{}
	}
	for _, id := range ids {
		fx.source.visible[id] = iface.RetrievalVisibility{Visible: false, Revision: 1}
	}
}

func idRange(prefix string, from, to int) []string {
	var out []string
	for i := from; i <= to; i++ {
		out = append(out, fmt.Sprintf("%s%d", prefix, i))
	}
	return out
}

func noVisibleCall(t *testing.T, fx *searchFixture) {
	t.Helper()
	fx.source.mu.Lock()
	defer fx.source.mu.Unlock()
	if len(fx.source.calls) != 0 {
		t.Fatalf("Visible called %d times, want 0: %v", len(fx.source.calls), fx.source.calls)
	}
}

func TestSearch_TombstoneVetoDropsBeforeVisible(t *testing.T) { // Review Focus 2
	fx := newSearchFixture(t)
	fx.tombstones.covered = map[string]bool{"s3": true}
	res, err := fx.search(contactsReq())
	require.NoError(t, err)
	assert.Equal(t, []string{"s1", "s2"}, hitSources(res))
	assert.Equal(t, [][]string{{"s1", "s2"}}, fx.source.calls) // s3 is never asked
	assert.Equal(t, []string{"t-1|contacts"}, fx.tombstones.args)
	assert.Equal(t, []string{"t-1"}, fx.source.tenants)
}

func TestSearch_TombstoneProbeCarriesSubjects(t *testing.T) {
	t.Run("one probe per source, in first-seen order", func(t *testing.T) {
		fx := newSearchFixture(t)
		_, err := fx.search(contactsReq())
		require.NoError(t, err)
		assert.Equal(t, [][]Probe{{
			{SourceID: "s1", Subjects: []string{"user:alice"}},
			{SourceID: "s2", Subjects: []string{"crm:c-2"}},
			{SourceID: "s3", Subjects: []string{"user:bob"}},
		}}, fx.tombstones.probes)
	})
	t.Run("chunks of one source merge into one sorted probe", func(t *testing.T) {
		fx := newSearchFixture(t)
		fx.engine.results = [][]engine.Candidate{{
			cand("s1", 1, 0, searchText1, "user:b"),
			cand("s2", 1, 0, searchText2),
			cand("s1", 1, 1, searchText3, "user:a", "user:b"),
		}}
		res, err := fx.search(contactsReq())
		require.NoError(t, err)
		assert.Equal(t, [][]Probe{{
			{SourceID: "s1", Subjects: []string{"user:a", "user:b"}},
			{SourceID: "s2", Subjects: nil},
		}}, fx.tombstones.probes)
		// One Visible id per source; both chunks of s1 survive.
		assert.Equal(t, [][]string{{"s1", "s2"}}, fx.source.calls)
		assert.Equal(t, []string{"s1", "s2", "s1"}, hitSources(res))
	})
	t.Run("a covered source drops every one of its chunks", func(t *testing.T) {
		fx := newSearchFixture(t)
		fx.engine.results = [][]engine.Candidate{{
			cand("s1", 1, 0, searchText1, "user:alice"),
			cand("s2", 1, 0, searchText2),
			cand("s1", 1, 1, searchText3, "user:bob"),
		}}
		fx.tombstones.covered = map[string]bool{"s1": true}
		res, err := fx.search(contactsReq())
		require.NoError(t, err)
		assert.Equal(t, []string{"s2"}, hitSources(res))
	})
}

func TestSearch_TombstoneErrorFailsClosed(t *testing.T) {
	fx := newSearchFixture(t)
	fx.tombstones.err = errors.New("mongo: subject user:alice lookup failed")
	res, err := fx.search(contactsReq())
	requireIs(t, err, iface.ErrRetrievalVisibilityCheck)
	assert.Nil(t, res)
	noVisibleCall(t, fx)
	if strings.Contains(err.Error(), "user:alice") || strings.Contains(fx.logs.String(), "user:alice") {
		t.Fatalf("the checker's error text leaked: %v / %s", err, fx.logs.String())
	}
}

func TestSearch_TombstoneAnswerMissingASourceDropsIt(t *testing.T) {
	fx := newSearchFixture(t)
	fx.tombstones.missing = map[string]bool{"s2": true}
	res, err := fx.search(contactsReq())
	require.NoError(t, err)
	assert.Equal(t, []string{"s1", "s3"}, hitSources(res))
	assert.Equal(t, [][]string{{"s1", "s3"}}, fx.source.calls)
}

func TestSearch_AllCoveredSkipsVisible(t *testing.T) {
	fx := newSearchFixture(t)
	fx.tombstones.covered = map[string]bool{"s1": true, "s2": true, "s3": true}
	res, err := fx.search(contactsReq())
	require.NoError(t, err)
	assert.Equal(t, []string{}, hitSources(res))
	noVisibleCall(t, fx)
}

func TestSearch_ForeignTenantCandidateDropped(t *testing.T) {
	fx := newSearchFixture(t)
	foreign := cand("s9", 1, 0, "Foreign tenant record about the head of sales", "user:mallory")
	foreign.Runtime.TenantID = "t-2"
	fx.engine.results = [][]engine.Candidate{{
		cand("s1", 1, 0, searchText1, "user:alice"),
		foreign,
		cand("s2", 1, 0, searchText2, "crm:c-2"),
	}}
	res, err := fx.search(contactsReq())
	require.NoError(t, err)
	assert.Equal(t, []string{"s1", "s2"}, hitSources(res))
	assert.Equal(t, [][]string{{"s1", "s2"}}, fx.source.calls)
	assert.Equal(t, [][]Probe{{
		{SourceID: "s1", Subjects: []string{"user:alice"}},
		{SourceID: "s2", Subjects: []string{"crm:c-2"}},
	}}, fx.tombstones.probes)
	logs := fx.logs.String()
	assert.Contains(t, logs, "level=WARN")
	assert.Contains(t, logs, "corpus=contacts")
	for _, leak := range []string{"t-2", "s9", "user:mallory", "head of sales"} {
		if strings.Contains(logs, leak) {
			t.Fatalf("WARN carries %q: %s", leak, logs)
		}
	}
}

func TestSearch_CandidateWithoutSourceIDDropped(t *testing.T) {
	fx := newSearchFixture(t)
	bad := cand("", 1, 0, searchText3)
	fx.engine.results = [][]engine.Candidate{{cand("s1", 1, 0, searchText1), bad}}
	res, err := fx.search(contactsReq())
	require.NoError(t, err)
	assert.Equal(t, []string{"s1"}, hitSources(res))
	assert.Equal(t, [][]string{{"s1"}}, fx.source.calls)
}

func TestSearch_VisibleRefusedDropped(t *testing.T) { // Review Focus 1
	fx := newSearchFixture(t)
	fx.source.visible = map[string]iface.RetrievalVisibility{"s2": {Visible: false, Revision: 1}}
	res, err := fx.search(contactsReq())
	require.NoError(t, err)
	assert.Equal(t, []string{"s1", "s3"}, hitSources(res))
	assert.Equal(t, false, res.Degraded)
}

func TestSearch_VisibleMissingIDIsNotVisible(t *testing.T) {
	fx := newSearchFixture(t)
	fx.source.missing = map[string]bool{"s2": true}
	res, err := fx.search(contactsReq())
	require.NoError(t, err)
	assert.Equal(t, []string{"s1", "s3"}, hitSources(res))
}

func TestSearch_StaleRevisionDropped(t *testing.T) {
	fx := newSearchFixture(t)
	fx.source.visible = map[string]iface.RetrievalVisibility{
		"s1": {Visible: true, Revision: 2}, // the source moved on
		"s3": {Visible: true, Revision: 0}, // older than the index: also not this revision
	}
	res, err := fx.search(contactsReq())
	require.NoError(t, err)
	assert.Equal(t, []string{"s2"}, hitSources(res))
}

func TestSearch_VisibleErrorFailsClosed(t *testing.T) {
	fx := newSearchFixture(t)
	fx.source.err = errors.New("crm: contact Alice Rossi is private")
	res, err := fx.search(contactsReq())
	requireIs(t, err, iface.ErrRetrievalVisibilityCheck)
	assert.Nil(t, res)
	if strings.Contains(err.Error(), "Alice") || strings.Contains(fx.logs.String(), "Alice") {
		t.Fatalf("the source's error text leaked: %v / %s", err, fx.logs.String())
	}
	assert.Contains(t, fx.logs.String(), "corpus=contacts")
}

func TestSearch_VisibleTimeoutFailsClosed(t *testing.T) { // Review Focus 1
	fx := newSearchFixture(t)
	fx.source.block = true
	fx.tweak = func(d *SearchDeps) { d.VisibleTimeout = 20 * time.Millisecond }
	start := time.Now()
	res, err := fx.search(contactsReq())
	if d := time.Since(start); d >= time.Second {
		t.Fatalf("search took %v, want < 1s", d)
	}
	requireIs(t, err, iface.ErrRetrievalVisibilityCheck)
	requireIs(t, err, context.DeadlineExceeded)
	assert.Nil(t, res)
}

func TestSearch_VisibleAnswerAfterDeadlineFailsClosed(t *testing.T) {
	// A source that ignores ctx and answers "visible" late is still refused.
	fx := newSearchFixture(t)
	fx.source.onCall = func(ctx context.Context, _ int) error {
		<-ctx.Done()
		return nil
	}
	fx.tweak = func(d *SearchDeps) { d.VisibleTimeout = 20 * time.Millisecond }
	res, err := fx.search(contactsReq())
	requireIs(t, err, iface.ErrRetrievalVisibilityCheck)
	assert.Nil(t, res)
}

func TestSearch_VisibleCallerCancelFailsClosed(t *testing.T) {
	fx := newSearchFixture(t)
	ctx, cancel := context.WithCancel(fx.ctx)
	fx.ctx = ctx
	fx.source.onCall = func(context.Context, int) error {
		cancel()
		return context.Canceled
	}
	res, err := fx.search(contactsReq())
	requireIs(t, err, iface.ErrRetrievalVisibilityCheck)
	requireIs(t, err, context.Canceled)
	assert.Nil(t, res)
}

func TestSearch_VisibleGetsCallerPrincipalContext(t *testing.T) {
	fx := newSearchFixture(t)
	_, err := fx.search(contactsReq())
	require.NoError(t, err)
	assert.Equal(t, []iface.RetrievalPrincipalContext{{
		TenantID: "t-1", UserUUID: "u-1", Principals: []string{"tenant:*", "user:u-1"},
	}}, fx.source.pcs)
}

func TestSearch_RefillsOnceWhenDropsShortenThePage(t *testing.T) { // Review Focus 4
	fx := newSearchFixture(t)
	first := candRange("a", 1, 8)
	fx.engine.results = [][]engine.Candidate{first, append(candRange("a", 1, 8), candRange("b", 1, 4)...)}
	refuse(fx, idRange("a", 1, 7)...)
	req := contactsReq()
	req.Limit = 2
	res, err := fx.search(req)
	require.NoError(t, err)
	require.Len(t, fx.engine.queries, 2)
	assert.Equal(t, 8, fx.engine.queries[0].Limit)
	assert.Equal(t, 100, fx.engine.queries[1].Limit)
	// The refill repeats the first query but for its Limit.
	q0, q1 := fx.engine.queries[0], fx.engine.queries[1]
	q1.Limit = q0.Limit
	assert.Equal(t, q0, q1)
	assert.Equal(t, [][]string{idRange("a", 1, 8), {"b1", "b2", "b3", "b4"}}, fx.source.calls)
	assert.Equal(t, 2, len(fx.tombstones.probes))
	assert.Len(t, fx.tombstones.probes[1], 4) // only the new candidates are probed
	assert.Equal(t, []string{"a8", "b1"}, hitSources(res))
	assert.Equal(t, false, res.Degraded)
}

func TestSearch_ShortPageAfterRefillIsFlagged(t *testing.T) { // Review Focus 4
	fx := newSearchFixture(t)
	second := append(candRange("a", 1, 8), candRange("b", 1, 92)...)
	require.Len(t, second, 100)
	fx.engine.results = [][]engine.Candidate{candRange("a", 1, 8), second}
	refuse(fx, idRange("a", 1, 7)...)
	refuse(fx, idRange("b", 1, 92)...)
	req := contactsReq()
	req.Limit = 2
	res, err := fx.search(req)
	require.NoError(t, err)
	assert.Equal(t, []string{"a8"}, hitSources(res))
	assert.Equal(t, true, res.Degraded)
	assert.Len(t, fx.engine.queries, 2) // never a third call
}

func TestSearch_ShortPageWithExhaustedIndexIsNotFlagged(t *testing.T) {
	fx := newSearchFixture(t)
	refuse(fx, "s2")
	res, err := fx.search(contactsReq()) // Limit 10 -> Fetch 40; the engine has 3
	require.NoError(t, err)
	assert.Equal(t, []string{"s1", "s3"}, hitSources(res))
	assert.Len(t, fx.engine.queries, 1)
	assert.Equal(t, false, res.Degraded)
}

func TestSearch_RefillShortButNotFullIsNotFlagged(t *testing.T) {
	// The refill returned fewer than it asked for: the index is exhausted.
	fx := newSearchFixture(t)
	fx.engine.results = [][]engine.Candidate{candRange("a", 1, 8), append(candRange("a", 1, 8), candRange("b", 1, 4)...)}
	refuse(fx, idRange("a", 1, 7)...)
	refuse(fx, idRange("b", 1, 4)...)
	req := contactsReq()
	req.Limit = 2
	res, err := fx.search(req)
	require.NoError(t, err)
	assert.Equal(t, []string{"a8"}, hitSources(res))
	assert.Equal(t, false, res.Degraded)
	assert.Len(t, fx.engine.queries, 2)
}

func TestSearch_FullPageWithNoRoomToRefillIsFlagged(t *testing.T) {
	// Limit 25 -> Fetch 100 == Candidates: no refill, a full short page is flagged.
	fx := newSearchFixture(t)
	fx.engine.results = [][]engine.Candidate{candRange("a", 1, 100)}
	refuse(fx, idRange("a", 1, 90)...)
	req := contactsReq()
	req.Limit = 25
	res, err := fx.search(req)
	require.NoError(t, err)
	assert.Equal(t, idRange("a", 91, 100), hitSources(res))
	assert.Equal(t, true, res.Degraded)
	assert.Len(t, fx.engine.queries, 1)
}

// The veto runs per engine call. A source whose first-call chunk survived
// and whose refill chunk (same revision, same subjects) is covered by then
// loses every chunk, not only the new one (F5).
func TestSearch_RefillVetoDropsEveryChunkOfACoveredSource(t *testing.T) {
	fx := newSearchFixture(t)
	s0 := cand("s", 1, 0, "candidate s chunk 0 for the head of sales", "user:carol")
	s1 := cand("s", 1, 1, "candidate s chunk 1 for the head of sales", "user:carol")
	first := append(candRange("a", 1, 7), s0)
	second := append(append(slices.Clone(first), s1), candRange("b", 1, 3)...)
	fx.engine.results = [][]engine.Candidate{first, second}
	refuse(fx, idRange("a", 1, 7)...)
	fx.tombstones.coveredFrom = map[string]int{"s": 2}
	req := contactsReq()
	req.Limit = 2
	res, err := fx.search(req)
	require.NoError(t, err)
	require.Len(t, fx.engine.queries, 2)
	require.Len(t, fx.tombstones.probes, 2)
	// The first call let s through; the refill's probe of s found it covered.
	assert.Equal(t, Probe{SourceID: "s", Subjects: []string{"user:carol"}}, fx.tombstones.probes[0][7])
	assert.Equal(t, true, slices.ContainsFunc(fx.tombstones.probes[1], func(p Probe) bool {
		return p.SourceID == "s" && slices.Equal(p.Subjects, []string{"user:carol"})
	}))
	assert.Equal(t, []string{"b1", "b2"}, hitSources(res))
	row := onlyRow(t, fx)
	assert.Equal(t, false, slices.Contains(row.SourceIDs, "s"))
	assert.Equal(t, false, slices.Contains(row.Subjects, "user:carol"))
}

// A source seen in both calls and still clear keeps its first-call chunk in
// its rank position ahead of the refill's points.
func TestSearch_RefillKeepsRankOrderOfARecheckedSource(t *testing.T) {
	fx := newSearchFixture(t)
	s0 := cand("s", 1, 0, "candidate s chunk 0 for the head of sales", "user:carol")
	s1 := cand("s", 1, 1, "candidate s chunk 1 for the head of sales", "user:carol")
	first := append([]engine.Candidate{s0}, candRange("a", 1, 15)...) // Limit 4 -> Fetch 16
	second := append(append(slices.Clone(first), candRange("b", 1, 2)...), s1)
	fx.engine.results = [][]engine.Candidate{first, second}
	refuse(fx, idRange("a", 1, 15)...)
	req := contactsReq()
	req.Limit = 4
	res, err := fx.search(req)
	require.NoError(t, err)
	require.Len(t, fx.engine.queries, 2)
	assert.Equal(t, []string{"s", "b1", "b2", "s"}, hitSources(res))
}

// Point IDs do not carry the revision: between the two calls the worker can
// replace a point under the same ID. A refill copy of a first-call point that
// differs from it (here a newer revision naming a subject erased by then)
// drops its whole source, whatever the checks would say (P1, 4401533378).
func TestSearch_RefillDropsASourceWhosePointChangedRevision(t *testing.T) {
	fx := newSearchFixture(t)
	s0 := cand("s", 1, 0, "candidate s chunk 0 for the head of sales", "user:carol")
	s0v2 := cand("s", 2, 0, "candidate s chunk 0 for the head of sales", "user:carol", "user:dave")
	require.Equal(t, s0.PointID, s0v2.PointID)
	first := append(candRange("a", 1, 7), s0)
	second := append(append(slices.Clone(first[:7]), s0v2), candRange("b", 1, 3)...)
	fx.engine.results = [][]engine.Candidate{first, second}
	refuse(fx, idRange("a", 1, 7)...)
	fx.tombstones.coveredFrom = map[string]int{"s": 2}
	fx.source.onCall = func(_ context.Context, call int) error {
		if call == 1 { // reindexed at revision 2 after the first check
			fx.source.mu.Lock()
			fx.source.visible["s"] = iface.RetrievalVisibility{Visible: true, Revision: 2}
			fx.source.mu.Unlock()
		}
		return nil
	}
	req := contactsReq()
	req.Limit = 2
	res, err := fx.search(req)
	require.NoError(t, err)
	require.Len(t, fx.engine.queries, 2)
	assert.Equal(t, []string{"b1", "b2"}, hitSources(res))
	row := onlyRow(t, fx)
	assert.Equal(t, false, slices.Contains(row.SourceIDs, "s"))
	assert.Equal(t, false, slices.Contains(row.Subjects, "user:carol"))
	assert.Equal(t, false, slices.Contains(row.Subjects, "user:dave"))
}

// The same with an access change: the newer copy is shared with someone else
// and the owner no longer grants it. Neither copy, nor the source's other
// first-call survivor (s1, absent from the refill), is returned.
func TestSearch_RefillDropsASourceWhosePointChangedAccess(t *testing.T) {
	fx := newSearchFixture(t)
	s0 := cand("s", 1, 0, "candidate s chunk 0 for the head of sales")
	s1 := cand("s", 1, 1, "candidate s chunk 1 for the head of sales")
	s0v2 := cand("s", 2, 0, "candidate s chunk 0 for the head of sales")
	s0v2.Runtime.VisibleTo = []string{"user:u-9"}
	first := append([]engine.Candidate{s0, s1}, candRange("a", 1, 10)...) // Limit 3 -> Fetch 12
	second := append(append([]engine.Candidate{s0v2}, candRange("a", 1, 10)...), candRange("b", 1, 3)...)
	fx.engine.results = [][]engine.Candidate{first, second}
	refuse(fx, idRange("a", 1, 10)...)
	fx.source.onCall = func(_ context.Context, call int) error {
		if call == 1 {
			fx.source.mu.Lock()
			fx.source.visible["s"] = iface.RetrievalVisibility{Visible: false, Revision: 2}
			fx.source.mu.Unlock()
		}
		return nil
	}
	req := contactsReq()
	req.Limit = 3
	res, err := fx.search(req)
	require.NoError(t, err)
	require.Len(t, fx.engine.queries, 2)
	assert.Equal(t, []string{"b1", "b2", "b3"}, hitSources(res))
	assert.Equal(t, false, slices.Contains(onlyRow(t, fx).SourceIDs, "s"))
}

// A changed source is dropped even when the owner grants the newer revision,
// its new refill chunk included: the refill does not check it at all.
func TestSearch_RefillChangedSourceIsNotChecked(t *testing.T) {
	fx := newSearchFixture(t)
	s0 := cand("s", 1, 0, "candidate s chunk 0 for the head of sales")
	s0v2 := cand("s", 2, 0, "candidate s chunk 0 for the head of sales")
	s1v2 := cand("s", 2, 1, "candidate s chunk 1 for the head of sales")
	first := append(candRange("a", 1, 7), s0)
	second := append(append(slices.Clone(first[:7]), s0v2, s1v2), candRange("b", 1, 3)...)
	fx.engine.results = [][]engine.Candidate{first, second}
	refuse(fx, idRange("a", 1, 7)...)
	fx.source.onCall = func(_ context.Context, call int) error {
		if call == 1 { // the owner grants revision 2 by the refill
			fx.source.mu.Lock()
			fx.source.visible["s"] = iface.RetrievalVisibility{Visible: true, Revision: 2}
			fx.source.mu.Unlock()
		}
		return nil
	}
	req := contactsReq()
	req.Limit = 2
	res, err := fx.search(req)
	require.NoError(t, err)
	assert.Equal(t, []string{"b1", "b2"}, hitSources(res))
	require.Len(t, fx.source.calls, 2)
	assert.Equal(t, []string{"b1", "b2", "b3"}, fx.source.calls[1])
}

func TestSearch_RefillDropsDuplicateNewPoints(t *testing.T) {
	fx := newSearchFixture(t)
	b1 := candRange("b", 1, 1)[0]
	fx.engine.results = [][]engine.Candidate{candRange("a", 1, 8), append(candRange("a", 1, 8), b1, b1)}
	refuse(fx, idRange("a", 1, 8)...)
	req := contactsReq()
	req.Limit = 2
	res, err := fx.search(req)
	require.NoError(t, err)
	assert.Equal(t, []string{"b1"}, hitSources(res))
	assert.Equal(t, [][]string{idRange("a", 1, 8), {"b1"}}, fx.source.calls)
}

func TestSearch_RefillWithNothingNewSkipsChecks(t *testing.T) {
	fx := newSearchFixture(t)
	fx.engine.results = [][]engine.Candidate{candRange("a", 1, 8)} // replayed for the refill
	refuse(fx, idRange("a", 1, 7)...)
	req := contactsReq()
	req.Limit = 2
	res, err := fx.search(req)
	require.NoError(t, err)
	assert.Equal(t, []string{"a8"}, hitSources(res))
	assert.Len(t, fx.engine.queries, 2)
	assert.Len(t, fx.source.calls, 1)
	assert.Len(t, fx.tombstones.probes, 1)
	assert.Equal(t, false, res.Degraded) // 8 < 100: the refill was not a full page
}

func TestSearch_RefillVisibleErrorFailsClosed(t *testing.T) {
	fx := newSearchFixture(t)
	fx.engine.results = [][]engine.Candidate{candRange("a", 1, 8), append(candRange("a", 1, 8), candRange("b", 1, 4)...)}
	refuse(fx, idRange("a", 1, 7)...)
	fx.source.onCall = func(_ context.Context, call int) error {
		if call == 2 {
			return errors.New("crm: down")
		}
		return nil
	}
	req := contactsReq()
	req.Limit = 2
	res, err := fx.search(req)
	requireIs(t, err, iface.ErrRetrievalVisibilityCheck)
	assert.Nil(t, res) // a8 survived the first check but nothing is returned
}

func TestSearch_RefillEngineErrorFailsClosed(t *testing.T) {
	fx := newSearchFixture(t)
	fx.engine.results = [][]engine.Candidate{candRange("a", 1, 8)}
	refuse(fx, idRange("a", 1, 7)...)
	fx.source.onCall = func(_ context.Context, call int) error {
		if call == 1 {
			fx.engine.mu.Lock()
			fx.engine.err = engine.ErrUnavailable
			fx.engine.mu.Unlock()
		}
		return nil
	}
	req := contactsReq()
	req.Limit = 2
	res, err := fx.search(req)
	requireIs(t, err, iface.ErrRetrievalUnavailable)
	assert.Nil(t, res)
}
