package services

import (
	"errors"
	"math"
	"strings"
	"testing"

	"github.com/orkestra/backend/pkg/sdk/iface"
)

func TestPlanSearch_Defaults(t *testing.T) {
	p, err := planSearch(iface.RetrievalSearchRequest{Corpus: "c", Query: "q"}, iface.RetrievalCorpusSpec{})
	require.NoError(t, err)
	assert.Equal(t, 10, p.Limit)
	assert.Equal(t, iface.RetrievalModeHybrid, p.Mode)
	assert.Equal(t, iface.RetrievalFusion{DenseWeight: 1, KeywordWeight: 1, Candidates: 50}, p.Fusion)
	assert.Equal(t, 0, p.RerankK)
	assert.Equal(t, 40, p.Fetch)
}

func TestPlanSearch_RejectsBadShapes(t *testing.T) {
	ok := iface.RetrievalSearchRequest{Corpus: "c", Query: "q"}
	cases := map[string]func(r *iface.RetrievalSearchRequest){
		"empty corpus":   func(r *iface.RetrievalSearchRequest) { r.Corpus = "" },
		"blank query":    func(r *iface.RetrievalSearchRequest) { r.Query = "   " },
		"long query":     func(r *iface.RetrievalSearchRequest) { r.Query = strings.Repeat("à", 2001) },
		"bad mode":       func(r *iface.RetrievalSearchRequest) { r.Mode = "semantic" },
		"limit -1":       func(r *iface.RetrievalSearchRequest) { r.Limit = -1 },
		"limit 101":      func(r *iface.RetrievalSearchRequest) { r.Limit = 101 },
		"dense 11":       func(r *iface.RetrievalSearchRequest) { r.Fusion = &iface.RetrievalFusion{DenseWeight: 11} },
		"keyword -1":     func(r *iface.RetrievalSearchRequest) { r.Fusion = &iface.RetrievalFusion{KeywordWeight: -1} },
		"dense inf":      func(r *iface.RetrievalSearchRequest) { r.Fusion = &iface.RetrievalFusion{DenseWeight: math.Inf(1)} },
		"dense nan":      func(r *iface.RetrievalSearchRequest) { r.Fusion = &iface.RetrievalFusion{DenseWeight: math.NaN()} },
		"candidates 501": func(r *iface.RetrievalSearchRequest) { r.Fusion = &iface.RetrievalFusion{Candidates: 501} },
		"candidates -1":  func(r *iface.RetrievalSearchRequest) { r.Fusion = &iface.RetrievalFusion{Candidates: -1} },
	}
	for name, mut := range cases {
		t.Run(name, func(t *testing.T) {
			r := ok
			mut(&r)
			_, err := planSearch(r, iface.RetrievalCorpusSpec{})
			if !errors.Is(err, iface.ErrRetrievalInvalidRequest) {
				t.Fatalf("want ErrRetrievalInvalidRequest, got %v", err)
			}
		})
	}
}

func TestPlanSearch_FusionOverrideFieldByField(t *testing.T) {
	spec := iface.RetrievalCorpusSpec{Fusion: iface.RetrievalFusion{DenseWeight: 2, KeywordWeight: 3, Candidates: 50}}
	p, err := planSearch(iface.RetrievalSearchRequest{Corpus: "c", Query: "q", Fusion: &iface.RetrievalFusion{DenseWeight: 4}}, spec)
	require.NoError(t, err)
	assert.Equal(t, iface.RetrievalFusion{DenseWeight: 4, KeywordWeight: 3, Candidates: 50}, p.Fusion)
}

func TestPlanSearch_CandidatesNeverBelowLimit(t *testing.T) {
	spec := iface.RetrievalCorpusSpec{Fusion: iface.RetrievalFusion{Candidates: 5}}
	p, err := planSearch(iface.RetrievalSearchRequest{Corpus: "c", Query: "q", Limit: 20}, spec)
	require.NoError(t, err)
	assert.Equal(t, 20, p.Fusion.Candidates)
	assert.Equal(t, 20, p.Fetch)
}

func TestPlanSearch_RerankK(t *testing.T) {
	req := iface.RetrievalSearchRequest{Corpus: "c", Query: "q"}
	spec := iface.RetrievalCorpusSpec{Rerank: iface.RetrievalRerank{Enabled: true}}
	p, err := planSearch(req, spec)
	require.NoError(t, err)
	assert.Equal(t, 20, p.RerankK)
	assert.Equal(t, 40, p.Fetch)

	spec.Rerank.TopK = 20
	req.Limit = 30
	p, err = planSearch(req, spec)
	require.NoError(t, err)
	assert.Equal(t, 30, p.RerankK)
	assert.Equal(t, 50, p.Fetch)

	spec.Rerank.Enabled = false
	p, err = planSearch(req, spec)
	require.NoError(t, err)
	assert.Equal(t, 0, p.RerankK)
}
