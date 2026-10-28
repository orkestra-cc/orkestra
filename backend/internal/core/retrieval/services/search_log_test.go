package services

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/orkestra/backend/internal/core/retrieval/engine"
	"github.com/orkestra/backend/internal/core/retrieval/filter"
	"github.com/orkestra/backend/internal/core/retrieval/models"
	"github.com/orkestra/backend/pkg/sdk/iface"
)

// searchLogNow is the fixed clock of the search-log tests; it is not UTC so
// the tests prove the row is stamped in UTC.
var searchLogNow = time.Date(2026, 10, 26, 14, 30, 0, 0, time.FixedZone("CET", 3600))

func fixedSearchClock(fx *searchFixture) {
	prev := fx.tweak
	fx.tweak = func(d *SearchDeps) {
		if prev != nil {
			prev(d)
		}
		d.Now = func() time.Time { return searchLogNow }
	}
}

func onlyRow(t *testing.T, fx *searchFixture) *models.SearchLog {
	t.Helper()
	fx.log.mu.Lock()
	defer fx.log.mu.Unlock()
	if len(fx.log.rows) != 1 {
		t.Fatalf("search log rows = %d, want 1", len(fx.log.rows))
	}
	return fx.log.rows[0]
}

func TestSearch_WritesSearchLogRow(t *testing.T) {
	fx := newSearchFixture(t)
	fixedSearchClock(fx)
	fx.source.visible = map[string]iface.RetrievalVisibility{"s2": {Visible: false, Revision: 1}}
	req := contactsReq()
	req.Filter = &iface.RetrievalFilter{Field: "team", Op: iface.RetrievalOpEq, Value: "sales"}

	res, err := fx.search(req)
	require.NoError(t, err)
	require.NotNil(t, res)
	assert.Len(t, res.Hits, 2)

	row := onlyRow(t, fx)
	if row.UUID == "" {
		t.Error("row has no UUID")
	}
	assert.Equal(t, "u-1", row.UserUUID)
	assert.Equal(t, "contacts", row.Corpus)
	assert.Equal(t, req.Query, row.Query)
	// Canonical JSON of the request's filter (F3): the SDK's json tags, no
	// unset fields.
	assert.Equal(t, `{"field":"team","op":"eq","value":"sales"}`, row.FilterJSON)
	assert.Equal(t, "hybrid", row.Mode)
	assert.Equal(t, []string{"tenant:*", "user:u-1"}, row.Principals)
	assert.Equal(t, []string{"s1", "s3"}, row.SourceIDs)
	assert.Equal(t, []string{"user:alice", "user:bob"}, row.Subjects)
	assert.Equal(t, 2, row.HitCount)
	assert.Equal(t, false, row.Degraded)
	assert.Equal(t, models.SearchOutcomeOK, row.Outcome)
	assert.Equal(t, "", row.ErrorKind)
	assert.Equal(t, searchLogNow.UTC(), row.CreatedAt)
	if row.CreatedAt.Location() != time.UTC {
		t.Errorf("CreatedAt location = %v, want UTC", row.CreatedAt.Location())
	}
	assert.Equal(t, []string{"t-1"}, fx.log.ctxTenant)
}

// The row is unique per search and carries the request's principals even
// when the resolver's slice is reused.
func TestSearch_SearchLogRowsAreDistinct(t *testing.T) {
	fx := newSearchFixture(t)
	s := NewSearcher(fx.deps())
	for range 2 {
		if _, err := s.Search(fx.ctx, contactsReq()); err != nil {
			t.Fatal(err)
		}
	}
	fx.log.mu.Lock()
	defer fx.log.mu.Unlock()
	if len(fx.log.rows) != 2 || fx.log.rows[0].UUID == fx.log.rows[1].UUID {
		t.Fatalf("rows = %d, want 2 with distinct UUIDs", len(fx.log.rows))
	}
}

func TestSearch_SubjectsAreTheSortedUnionOfReturnedPoints(t *testing.T) {
	fx := newSearchFixture(t)
	fx.engine.results = [][]engine.Candidate{{
		cand("s1", 1, 0, searchText1, "user:zed", "user:alice"),
		cand("s1", 1, 1, searchText1, "user:alice"),
		cand("s3", 1, 0, searchText3, "user:bob"),
	}}
	req := contactsReq()
	req.Limit = 2 // s3 is fetched but not returned
	_, err := fx.search(req)
	require.NoError(t, err)
	row := onlyRow(t, fx)
	assert.Equal(t, []string{"s1"}, row.SourceIDs)
	assert.Equal(t, []string{"user:alice", "user:zed"}, row.Subjects)
	assert.Equal(t, 2, row.HitCount)
}

func TestSearch_LogWriteFailureFailsClosed(t *testing.T) {
	fx := newSearchFixture(t)
	fx.log.err = errors.New("mongo down")
	res, err := fx.search(contactsReq())
	assert.Nil(t, res)
	requireIs(t, err, iface.ErrRetrievalUnavailable)
	requireIs(t, err, ErrSearchLogWrite)
	if strings.Contains(err.Error(), "mongo down") {
		t.Errorf("error carries the writer's text: %v", err)
	}
	assert.Len(t, fx.log.ctxTenant, 1)
}

func TestSearch_FailedSearchAfterGateIsLogged(t *testing.T) {
	fx := newSearchFixture(t)
	fixedSearchClock(fx)
	fx.source.err = errors.New("crm down")
	res, err := fx.search(contactsReq())
	assert.Nil(t, res)
	requireIs(t, err, iface.ErrRetrievalVisibilityCheck)
	row := onlyRow(t, fx)
	assert.Equal(t, models.SearchOutcomeError, row.Outcome)
	assert.Equal(t, "visibility_check", row.ErrorKind)
	assert.Equal(t, 0, row.HitCount)
	assert.Empty(t, row.SourceIDs)
	assert.Empty(t, row.Subjects)
	assert.Equal(t, []string{"tenant:*", "user:u-1"}, row.Principals)
	assert.Equal(t, searchLogNow.UTC(), row.CreatedAt)

	// A failing log write on the error path keeps the original error.
	fx = newSearchFixture(t)
	fx.source.err = errors.New("crm down")
	fx.log.err = errors.New("mongo down")
	_, err = fx.search(contactsReq())
	requireIs(t, err, iface.ErrRetrievalVisibilityCheck)
	if errors.Is(err, ErrSearchLogWrite) {
		t.Errorf("error-path log failure replaced the search error: %v", err)
	}
	assert.Contains(t, fx.logs.String(), "level=WARN")
	assert.Contains(t, fx.logs.String(), "search log")
}

func TestSearch_GateRefusalIsNotLogged(t *testing.T) {
	fx := newSearchFixture(t)
	fx.authz.allow = false
	_, err := fx.search(contactsReq())
	requireIs(t, err, iface.ErrRetrievalForbidden)
	assert.Len(t, fx.log.rows, 0)
	assert.Len(t, fx.log.ctxTenant, 0)

	fx = newSearchFixture(t)
	_, err = fx.search(iface.RetrievalSearchRequest{Corpus: "nope", Query: "x"})
	requireIs(t, err, iface.ErrRetrievalUnknownCorpus)
	assert.Len(t, fx.log.ctxTenant, 0)
}

func TestSearch_DegradedOutcomeLogged(t *testing.T) {
	fx := newSearchFixture(t)
	fx.tweak = func(d *SearchDeps) { d.Embedder = nil } // keyword fallback
	res, err := fx.search(contactsReq())
	require.NoError(t, err)
	assert.Equal(t, true, res.Degraded)
	row := onlyRow(t, fx)
	assert.Equal(t, models.SearchOutcomeDegraded, row.Outcome)
	assert.Equal(t, true, row.Degraded)
	assert.Equal(t, "hybrid", row.Mode)
	assert.Equal(t, "", row.ErrorKind)
}

func TestSearch_NoActiveGenerationIsLoggedDegraded(t *testing.T) {
	fx := newSearchFixture(t)
	fx.state.gens = nil
	_, err := fx.search(contactsReq())
	require.NoError(t, err)
	row := onlyRow(t, fx)
	assert.Equal(t, models.SearchOutcomeDegraded, row.Outcome)
	assert.Equal(t, 0, row.HitCount)
	assert.Empty(t, row.Principals) // step 3 never ran

	// Its row is still written before the result: a failing write fails it.
	fx = newSearchFixture(t)
	fx.state.gens = nil
	fx.log.err = errors.New("mongo down")
	res, err := fx.search(contactsReq())
	assert.Nil(t, res)
	requireIs(t, err, ErrSearchLogWrite)
}

func TestSearch_ErrorKinds(t *testing.T) {
	cases := []struct {
		name  string
		setup func(fx *searchFixture)
		want  string
	}{
		{"not_configured", func(fx *searchFixture) { fx.tweak = func(d *SearchDeps) { d.Engine = nil } }, "not_configured"},
		{"state", func(fx *searchFixture) { fx.state.err = errors.New("mongo down") }, "unavailable"},
		{"engine", func(fx *searchFixture) { fx.engine.err = engine.ErrUnavailable }, "unavailable"},
		{"principals", func(fx *searchFixture) { fx.principals.err = ErrPrincipalCapExceeded }, "principals"},
		{"tombstones", func(fx *searchFixture) { fx.tombstones.err = errors.New("mongo down") }, "visibility_check"},
		{"engine_canceled", func(fx *searchFixture) { fx.engine.err = context.Canceled }, "canceled"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fx := newSearchFixture(t)
			tc.setup(fx)
			res, err := fx.search(contactsReq())
			assert.Nil(t, res)
			if err == nil {
				t.Fatal("search succeeded")
			}
			row := onlyRow(t, fx)
			assert.Equal(t, models.SearchOutcomeError, row.Outcome)
			assert.Equal(t, tc.want, row.ErrorKind)
		})
	}
}

// A cancelled caller still leaves a row: the error-path insert runs detached
// from the request's cancellation (P19), under the request's tenant.
func TestSearch_CanceledSearchIsLoggedDetached(t *testing.T) {
	fx := newSearchFixture(t)
	ctx, cancel := context.WithCancel(fx.ctx)
	defer cancel()
	fx.ctx = ctx
	fx.embedder.onCall = cancel
	fx.embedder.err = iface.ErrLLMProviderUnavailable
	var insertCtxErr error
	fx.tweak = func(d *SearchDeps) {
		d.Log = searchLogFunc(func(ctx context.Context, row *models.SearchLog) error {
			insertCtxErr = ctx.Err()
			if _, ok := ctx.Deadline(); !ok {
				t.Error("error-path insert has no deadline")
			}
			return fx.log.Insert(ctx, row)
		})
	}
	_, err := fx.search(contactsReq())
	requireIs(t, err, context.Canceled)
	if insertCtxErr != nil {
		t.Errorf("insert ctx err = %v, want a live context", insertCtxErr)
	}
	row := onlyRow(t, fx)
	assert.Equal(t, "canceled", row.ErrorKind)
	assert.Equal(t, []string{"t-1"}, fx.log.ctxTenant)
}

type searchLogFunc func(ctx context.Context, row *models.SearchLog) error

func (f searchLogFunc) Insert(ctx context.Context, row *models.SearchLog) error { return f(ctx, row) }

func TestSearch_TimingsAreMeasuredWithNow(t *testing.T) {
	fx := newSearchFixture(t)
	tick := searchLogNow
	fx.tweak = func(d *SearchDeps) {
		d.Now = func() time.Time { tick = tick.Add(time.Millisecond); return tick }
	}
	_, err := fx.search(contactsReq())
	require.NoError(t, err)
	row := onlyRow(t, fx)
	tm := row.Timings
	if tm.TotalMs <= 0 || tm.ResolveMs <= 0 || tm.EmbedMs <= 0 || tm.EngineMs <= 0 || tm.CheckMs <= 0 {
		t.Fatalf("timings = %+v, want every measured stage > 0", tm)
	}
	if tm.ResolveMs+tm.EmbedMs+tm.EngineMs+tm.CheckMs+tm.RerankMs > tm.TotalMs {
		t.Fatalf("stage timings exceed the total: %+v", tm)
	}
}

// The search log never reaches slog: no query text, principals or subjects.
func TestSearch_LogRowContentStaysOutOfSlog(t *testing.T) {
	fx := newSearchFixture(t)
	fx.log.err = errors.New("mongo down")
	fx.source.err = errors.New("crm down")
	_, _ = fx.search(contactsReq())
	out := fx.logs.String()
	for _, leak := range []string{"head of sales", "user:u-1", "tenant:*", "user:alice", "mongo down"} {
		if strings.Contains(out, leak) {
			t.Errorf("slog output carries %q: %s", leak, out)
		}
	}
}

// A request without a filter stores none.
func TestSearch_SearchLogRowWithoutFilter(t *testing.T) {
	fx := newSearchFixture(t)
	_, err := fx.search(contactsReq())
	require.NoError(t, err)
	assert.Equal(t, "", onlyRow(t, fx).FilterJSON)
}

// A filter that validates but has no JSON form (a hex json.Number on a float
// field) cannot be stored canonically: it is refused at the gate as invalid,
// before any engine call and without a row.
func TestSearch_FilterWithoutJSONFormIsRefused(t *testing.T) {
	fx := newSearchFixture(t)
	fx.spec.Filters = append(fx.spec.Filters, iface.RetrievalFilterField{Name: "score", Type: iface.RetrievalFieldFloat})
	req := contactsReq()
	req.Filter = &iface.RetrievalFilter{Field: "score", Op: iface.RetrievalOpGte, Value: json.Number("0x1p3")}
	require.NoError(t, filter.Validate(req.Filter, fx.spec.Filters)) // valid, only unstorable
	res, err := fx.search(req)
	requireIs(t, err, iface.ErrRetrievalInvalidFilter)
	assert.Nil(t, res)
	noEngineCall(t, fx)
	assert.Len(t, fx.log.rows, 0)
}
