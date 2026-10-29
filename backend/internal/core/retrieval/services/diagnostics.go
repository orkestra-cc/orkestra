package services

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"github.com/orkestra/backend/internal/core/retrieval/engine"
	"github.com/orkestra/backend/pkg/sdk/iface"
)

// StageScores are the per-stage scores of one returned hit.
type StageScores struct {
	ChunkID string
	Fused   float32  // the engine's fused score, before any rerank
	Dense   *float32 // dense arm's cosine; nil when the point was not in that arm or the arm did not run
	Keyword *float32 // keyword arm's BM25; nil likewise
	Rerank  *float32 // reranker's score; nil without a successful rerank of this hit
}

// DiagnosticResult is a search result with what each stage saw.
type DiagnosticResult struct {
	Result     iface.RetrievalSearchResult
	Collection string        // the collection pinned at D5 step 2; "" without an active generation
	Profile    string        // embedding profile ID pinned at D5 step 2
	Principals []string      // the caller's principals as applied at D5 step 3
	Scores     []StageScores // one per Result.Hits[i], same order
}

// Diagnose runs exactly the D5 pipeline Search runs (same gate, same
// principals, same erasure veto and Visible check, same search-log row and
// post-write re-check) with engine.Query.Diagnostics set, and returns the
// per-stage scores of the returned hits. It is one code path with Search, so
// no platform role gets a bypass (ADR D10) and the hits are exactly those a
// consumer search by the same caller returns.
func (s *Searcher) Diagnose(ctx context.Context, req iface.RetrievalSearchRequest) (*DiagnosticResult, error) {
	d := &diagRecord{}
	res, err := s.search(ctx, req, d)
	if err != nil {
		return nil, err
	}
	scores := d.scores
	if scores == nil {
		scores = []StageScores{}
	}
	return &DiagnosticResult{
		Result:     *res,
		Collection: d.collection,
		Profile:    d.profile,
		Principals: slices.Clone(d.principals),
		Scores:     scores,
	}, nil
}

// Diagnose implements AdminRuntime. Without a wired Searcher it returns
// iface.ErrRetrievalNotConfigured.
func (s *Service) Diagnose(ctx context.Context, req iface.RetrievalSearchRequest) (*DiagnosticResult, error) {
	s.mu.RLock()
	searcher := s.searcher
	s.mu.RUnlock()
	if searcher == nil {
		return nil, fmt.Errorf("%w: search is not wired", iface.ErrRetrievalNotConfigured)
	}
	return searcher.Diagnose(ctx, req)
}

// diagRecord collects, during a diagnostic run, what DiagnosticResult needs.
// A nil *diagRecord is a consumer search: every method is then a no-op.
type diagRecord struct {
	collection string
	profile    string
	principals []string
	// checked holds, by point ID, the scores of every candidate that passed
	// step 6, taken before step 7 overwrites Score with the reranker's.
	checked map[string]StageScores
	scores  []StageScores
}

// resolved records the pin (step 2) and the principals (step 3).
func (d *diagRecord) resolved(pinned PinnedGeneration, principals []string) {
	if d == nil {
		return
	}
	d.collection, d.profile = pinned.Collection, pinned.EmbedProfileID
	d.principals = slices.Clone(principals)
}

// survivors records the scores of the checked candidates of query q. An arm
// q did not send reports no score, whatever the engine answered.
func (d *diagRecord) survivors(q engine.Query, cands []engine.Candidate) {
	if d == nil {
		return
	}
	dense, keyword := len(q.Dense) > 0, strings.TrimSpace(q.Keyword) != ""
	d.checked = make(map[string]StageScores, len(cands))
	for _, c := range cands {
		sc := StageScores{ChunkID: c.Runtime.ChunkID, Fused: c.Score}
		if dense {
			sc.Dense = copyScore(c.DenseScore)
		}
		if keyword {
			sc.Keyword = copyScore(c.KeywordScore)
		}
		d.checked[c.PointID] = sc
	}
}

// reranked records the rerank scores of the head: scores[i] is ordered[i]'s.
func (d *diagRecord) reranked(ordered []engine.Candidate, scores []float32) {
	if d == nil {
		return
	}
	for i, v := range scores {
		if i >= len(ordered) {
			break
		}
		sc, ok := d.checked[ordered[i].PointID]
		if !ok {
			continue
		}
		sc.Rerank = &v
		d.checked[ordered[i].PointID] = sc
	}
}

// returned builds Scores in the order of the returned candidates (step 8).
func (d *diagRecord) returned(cands []engine.Candidate) {
	if d == nil {
		return
	}
	d.scores = make([]StageScores, 0, len(cands))
	for _, c := range cands {
		sc, ok := d.checked[c.PointID]
		if !ok {
			// Every returned candidate passed step 6; keep the one-per-hit
			// invariant even so.
			sc = StageScores{ChunkID: c.Runtime.ChunkID, Fused: c.Score}
		}
		d.scores = append(d.scores, sc)
	}
}

func copyScore(p *float32) *float32 {
	if p == nil {
		return nil
	}
	v := *p
	return &v
}
