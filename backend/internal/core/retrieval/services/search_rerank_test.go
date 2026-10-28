package services

import (
	"context"
	"errors"
	"math"
	"strings"
	"testing"

	"github.com/orkestra/backend/internal/core/retrieval/engine"
	"github.com/orkestra/backend/pkg/sdk/iface"
)

// --- Step 7: optional rerank ---

func rerankFixture(t *testing.T) *searchFixture {
	t.Helper()
	fx := newSearchFixture(t)
	fx.spec.Rerank = iface.RetrievalRerank{Endpoint: "rerank", Enabled: true, TopK: 50}
	return fx
}

func hitScores(res *iface.RetrievalSearchResult) []float32 {
	out := []float32{}
	for _, h := range res.Hits {
		out = append(out, h.Score)
	}
	return out
}

func limitReq(limit int) iface.RetrievalSearchRequest {
	req := contactsReq()
	req.Limit = limit
	return req
}

func TestSearch_RerankDisabledByDefault(t *testing.T) {
	fx := newSearchFixture(t)
	fx.reranker.res = &iface.RerankResult{Results: []iface.RerankScore{{Index: 2, Score: 0.9}}}
	res, err := fx.search(contactsReq())
	require.NoError(t, err)
	assert.Len(t, fx.reranker.reqs, 0)
	assert.Equal(t, []string{"s1", "s2", "s3"}, hitSources(res))
}

func TestSearch_RerankReordersHead(t *testing.T) {
	fx := rerankFixture(t)
	fx.reranker.res = &iface.RerankResult{Results: []iface.RerankScore{{Index: 2, Score: 0.9}, {Index: 0, Score: 0.5}, {Index: 1, Score: 0.1}}}
	res, err := fx.search(limitReq(2))
	require.NoError(t, err)
	require.Len(t, fx.reranker.reqs, 1)
	assert.Equal(t, iface.RerankRequest{
		Caller: "retrieval", OnBehalfOf: "t-1", Profile: "ip_rerank_a", Query: "head of sales",
		Documents: []string{searchText1, searchText2, searchText3}, TopN: 3,
	}, fx.reranker.reqs[0])
	assert.Equal(t, []string{"s3", "s1"}, hitSources(res))
	assert.Equal(t, []float32{0.9, 0.5}, hitScores(res))
	assert.Equal(t, false, res.Degraded)
}

func TestSearch_RerankNCappedByTopK(t *testing.T) {
	fx := rerankFixture(t)
	fx.spec.Rerank.TopK = 2 // RerankK = max(2, Limit 3) = 3 -> use Limit 1 to get n = 2
	fx.reranker.res = &iface.RerankResult{Results: []iface.RerankScore{{Index: 1, Score: 0.8}, {Index: 0, Score: 0.2}}}
	res, err := fx.search(limitReq(1))
	require.NoError(t, err)
	require.Len(t, fx.reranker.reqs, 1)
	assert.Equal(t, 2, fx.reranker.reqs[0].TopN)
	assert.Equal(t, []string{"s2"}, hitSources(res))
	assert.Equal(t, []float32{0.8}, hitScores(res))
}

func TestSearch_RerankNeverSeesRefusedText(t *testing.T) { // Review Focus 1
	fx := rerankFixture(t)
	fx.source.visible = map[string]iface.RetrievalVisibility{"s2": {Visible: false, Revision: 1}}
	fx.tombstones.covered = map[string]bool{"s3": true}
	fx.reranker.res = &iface.RerankResult{Results: []iface.RerankScore{{Index: 0, Score: 0.7}}}
	res, err := fx.search(contactsReq())
	require.NoError(t, err)
	require.Len(t, fx.reranker.reqs, 1)
	assert.Equal(t, []string{searchText1}, fx.reranker.reqs[0].Documents)
	assert.Equal(t, []string{"s1"}, hitSources(res))
}

func TestSearch_RerankFailureKeepsFusedOrderDegraded(t *testing.T) {
	fx := rerankFixture(t)
	fx.reranker.err = errors.New("provider says: Alice Rossi")
	res, err := fx.search(limitReq(2))
	require.NoError(t, err)
	assert.Equal(t, []string{"s1", "s2"}, hitSources(res))
	assert.Equal(t, []float32{1, 1}, hitScores(res))
	assert.Equal(t, true, res.Degraded)
	assert.Contains(t, fx.logs.String(), "corpus=contacts")
	for _, leak := range []string{"Alice", "head of sales"} {
		if strings.Contains(fx.logs.String(), leak) {
			t.Fatalf("log leaked %q: %s", leak, fx.logs.String())
		}
	}
}

func TestSearch_RerankHostedEndpointRefusedForLocalOnly(t *testing.T) {
	fx := rerankFixture(t)
	fx.profiles.profiles["rerank"].Boundary = iface.InferenceBoundaryHosted
	fx.reranker.res = &iface.RerankResult{Results: []iface.RerankScore{{Index: 2, Score: 0.9}}}
	res, err := fx.search(limitReq(2))
	require.NoError(t, err)
	assert.Len(t, fx.reranker.reqs, 0)
	assert.Equal(t, []string{"s1", "s2"}, hitSources(res))
	assert.Equal(t, true, res.Degraded)

	fx = rerankFixture(t)
	fx.spec.Locality = iface.RetrievalHostedAllowed
	fx.profiles.profiles["rerank"].Boundary = iface.InferenceBoundaryHosted
	fx.reranker.res = &iface.RerankResult{Results: []iface.RerankScore{{Index: 2, Score: 0.9}, {Index: 0, Score: 0.5}, {Index: 1, Score: 0.1}}}
	res, err = fx.search(limitReq(2))
	require.NoError(t, err)
	assert.Len(t, fx.reranker.reqs, 1)
	assert.Equal(t, false, res.Degraded)
}

func TestSearch_RerankMalformedResultIsFailure(t *testing.T) {
	cases := map[string]*iface.RerankResult{
		"duplicate": {Results: []iface.RerankScore{{Index: 0, Score: 0.9}, {Index: 0, Score: 0.5}, {Index: 1, Score: 0.1}}},
		"range":     {Results: []iface.RerankScore{{Index: 5, Score: 0.9}}},
		"negative":  {Results: []iface.RerankScore{{Index: -1, Score: 0.9}, {Index: 0, Score: 0.5}, {Index: 1, Score: 0.1}}},
		"short":     {Results: []iface.RerankScore{{Index: 0, Score: 0.9}}},
		"nil":       nil,
		"nan":       {Results: []iface.RerankScore{{Index: 0, Score: float32(math.NaN())}, {Index: 1, Score: 0.5}, {Index: 2, Score: 0.1}}},
		"+inf":      {Results: []iface.RerankScore{{Index: 0, Score: float32(math.Inf(1))}, {Index: 1, Score: 0.5}, {Index: 2, Score: 0.1}}},
		"-inf":      {Results: []iface.RerankScore{{Index: 0, Score: 0.9}, {Index: 1, Score: 0.5}, {Index: 2, Score: float32(math.Inf(-1))}}},
	}
	for name, rr := range cases {
		t.Run(name, func(t *testing.T) {
			fx := rerankFixture(t)
			fx.reranker.res = rr
			res, err := fx.search(limitReq(2))
			require.NoError(t, err)
			assert.Equal(t, []string{"s1", "s2"}, hitSources(res))
			assert.Equal(t, []float32{1, 1}, hitScores(res))
			assert.Equal(t, true, res.Degraded)
		})
	}
}

func TestSearch_RerankUnavailableDegrades(t *testing.T) {
	cases := map[string]func(fx *searchFixture){
		"nil reranker": func(fx *searchFixture) { fx.tweak = func(d *SearchDeps) { d.Reranker = nil } },
		"nil profiles": func(fx *searchFixture) { fx.tweak = func(d *SearchDeps) { d.Profiles = nil } },
		"profile err":  func(fx *searchFixture) { fx.profiles.errs = map[string]error{"rerank": errors.New("boom")} },
		"wrong kind": func(fx *searchFixture) {
			fx.profiles.profiles["rerank"].Kind = iface.InferenceKindEmbed
		},
	}
	for name, setup := range cases {
		t.Run(name, func(t *testing.T) {
			fx := rerankFixture(t)
			fx.reranker.res = &iface.RerankResult{Results: []iface.RerankScore{{Index: 0, Score: 0.9}, {Index: 1, Score: 0.5}, {Index: 2, Score: 0.1}}}
			setup(fx)
			res, err := fx.search(limitReq(2))
			require.NoError(t, err)
			assert.Len(t, fx.reranker.reqs, 0)
			assert.Equal(t, []string{"s1", "s2"}, hitSources(res))
			assert.Equal(t, true, res.Degraded)
		})
	}
}

func TestSearch_RerankCancelledCallerReturnsCtxErr(t *testing.T) {
	fx := rerankFixture(t)
	ctx, cancel := context.WithCancel(fx.ctx)
	fx.reranker.err = context.Canceled
	fx.reranker.onCall = cancel
	res, err := NewSearcher(fx.deps()).Search(ctx, limitReq(2))
	requireIs(t, err, context.Canceled)
	assert.Nil(t, res)
}

func TestRerank_HeadReorderedTailKeepsFusedOrder(t *testing.T) {
	fx := rerankFixture(t)
	fx.reranker.res = &iface.RerankResult{Results: []iface.RerankScore{{Index: 1, Score: 0.8}, {Index: 0, Score: 0.2}}}
	surv := []engine.Candidate{
		cand("s1", 1, 0, searchText1), cand("s2", 1, 0, searchText2), cand("s3", 1, 0, searchText3),
	}
	ordered, scores, ok := NewSearcher(fx.deps()).rerank(fx.ctx, "t-1", fx.spec, "q", surv, 2)
	require.Equal(t, true, ok)
	require.Len(t, ordered, 3)
	got := []string{ordered[0].Runtime.SourceID, ordered[1].Runtime.SourceID, ordered[2].Runtime.SourceID}
	assert.Equal(t, []string{"s2", "s1", "s3"}, got)
	assert.Equal(t, []float32{0.8, 0.2}, scores)
	assert.Equal(t, []float32{0.8, 0.2, surv[2].Score}, []float32{ordered[0].Score, ordered[1].Score, ordered[2].Score})
	assert.Equal(t, float32(1), surv[0].Score) // input untouched
	assert.Equal(t, []string{searchText1, searchText2}, fx.reranker.reqs[0].Documents)
}
