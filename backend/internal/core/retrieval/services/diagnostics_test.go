package services

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/orkestra/backend/internal/core/retrieval/engine"
	"github.com/orkestra/backend/internal/core/retrieval/models"
	"github.com/orkestra/backend/internal/testkit"
	"github.com/orkestra/backend/pkg/sdk/ctxauth"
	"github.com/orkestra/backend/pkg/sdk/iface"
)

// Diagnostics (PR 7): Diagnose runs the Search pipeline itself with
// engine.Query.Diagnostics set and reports per-stage scores.

func (fx *searchFixture) diagnose(req iface.RetrievalSearchRequest) (*DiagnosticResult, error) {
	return NewSearcher(fx.deps()).Diagnose(fx.ctx, req)
}

func f32(v float32) *float32 { return &v }

// withArmScores gives every fixture candidate arm scores, as the engine does
// under Diagnostics (the fake answers the same either way).
func withArmScores(fx *searchFixture) {
	for i, cs := range fx.engine.results {
		for j := range cs {
			fx.engine.results[i][j].DenseScore = f32(0.9 - 0.1*float32(j))
			fx.engine.results[i][j].KeywordScore = f32(7 - float32(j))
		}
	}
}

func scoreChunks(res *DiagnosticResult) []string {
	out := []string{}
	for _, s := range res.Scores {
		out = append(out, s.ChunkID)
	}
	return out
}

func TestDiagnose_ReturnsArmScores(t *testing.T) {
	fx := newSearchFixture(t)
	withArmScores(fx)
	res, err := fx.diagnose(contactsReq())
	require.NoError(t, err)
	require.Len(t, res.Result.Hits, 3)
	require.Len(t, res.Scores, len(res.Result.Hits))
	for i, h := range res.Result.Hits {
		s := res.Scores[i]
		assert.Equal(t, h.ChunkID, s.ChunkID)
		assert.Equal(t, h.Score, s.Fused) // no rerank: the hit's score is the fused one
		require.NotNil(t, s.Dense)
		require.NotNil(t, s.Keyword)
		assert.Equal(t, 0.9-0.1*float32(i), *s.Dense)
		assert.Equal(t, 7-float32(i), *s.Keyword)
		assert.Nil(t, s.Rerank)
	}
	assert.Equal(t, "orkestra_contacts_g1", res.Collection)
	assert.Equal(t, "ip_embed_a", res.Profile)
	assert.Equal(t, []string{"tenant:*", "user:u-1"}, res.Principals)
	assert.Equal(t, true, fx.lastQuery().Diagnostics)
	assert.Equal(t, false, res.Result.Degraded)
}

// The arm scores are copies: the result does not alias the engine's answer.
func TestDiagnose_ScoresDoNotAliasCandidates(t *testing.T) {
	fx := newSearchFixture(t)
	withArmScores(fx)
	res, err := fx.diagnose(contactsReq())
	require.NoError(t, err)
	*fx.engine.results[0][0].DenseScore = 42
	assert.Equal(t, float32(0.9), *res.Scores[0].Dense)
}

// The refill (a second engine call) carries the Diagnostics flag too.
func TestDiagnose_RefillAlsoRequestsDiagnostics(t *testing.T) {
	fx := newSearchFixture(t)
	fx.source.visible = map[string]iface.RetrievalVisibility{"s1": {Visible: false, Revision: 1}}
	req := limitReq(3)
	// Fetch = min(100, 4*3) = 12: a full first page triggers the refill.
	page := make([]engine.Candidate, 12)
	for i := range page {
		page[i] = cand("s1", 1, i, searchText1)
	}
	fx.engine.results = [][]engine.Candidate{page, append(page, cand("s2", 1, 0, searchText2))}
	_, err := fx.diagnose(req)
	require.NoError(t, err)
	require.Len(t, fx.engine.queries, 2)
	for i, q := range fx.engine.queries {
		assert.Equal(t, true, q.Diagnostics, i)
	}
}

// Rerank overwrites a candidate's score; the fused score is kept from before
// step 7, and the rerank score is reported for the head only.
func TestDiagnose_KeepsFusedScoreThroughRerank(t *testing.T) {
	fx := rerankFixture(t)
	fx.spec.Rerank.TopK = 2 // Limit 1: RerankK = max(TopK 2, Limit 1) = 2, so the head (n) is the top 2 of the 3 candidates
	fx.engine.results = [][]engine.Candidate{{
		cand("s1", 1, 0, searchText1),
		cand("s2", 1, 1, searchText2),
		cand("s3", 1, 2, searchText3),
	}}
	fx.reranker.res = &iface.RerankResult{Results: []iface.RerankScore{{Index: 1, Score: 0.8}, {Index: 0, Score: 0.2}}}

	// Same request as a consumer search: the hits must be identical.
	want, err := fx.search(limitReq(1))
	require.NoError(t, err)
	res, err := fx.diagnose(limitReq(1))
	require.NoError(t, err)
	assert.Equal(t, *want, res.Result)

	require.Len(t, res.Scores, 1)
	s := res.Scores[0]
	assert.Equal(t, ChunkID("s2", 1), s.ChunkID)
	assert.Equal(t, float32(1)/2, s.Fused) // cand ordinal 1
	require.NotNil(t, s.Rerank)
	assert.Equal(t, float32(0.8), *s.Rerank)
	assert.Equal(t, float32(0.8), res.Result.Hits[0].Score)
}

// planSearch keeps RerankK >= Limit, so every returned hit of a reranked
// search is in the head: each carries its rerank score and its fused one.
func TestDiagnose_RerankScoresFollowTheHits(t *testing.T) {
	fx := rerankFixture(t)
	fx.engine.results = [][]engine.Candidate{{
		cand("s1", 1, 0, searchText1),
		cand("s2", 1, 1, searchText2),
		cand("s3", 1, 2, searchText3),
	}}
	fx.reranker.res = &iface.RerankResult{Results: []iface.RerankScore{{Index: 2, Score: 0.9}, {Index: 0, Score: 0.5}, {Index: 1, Score: 0.1}}}
	res, err := fx.diagnose(limitReq(3))
	require.NoError(t, err)
	require.Len(t, res.Scores, 3)
	assert.Equal(t, []string{ChunkID("s3", 2), ChunkID("s1", 0), ChunkID("s2", 1)}, scoreChunks(res))
	for i, want := range []float32{0.9, 0.5, 0.1} {
		require.NotNil(t, res.Scores[i].Rerank)
		assert.Equal(t, want, *res.Scores[i].Rerank)
		assert.Equal(t, want, res.Result.Hits[i].Score)
	}
	assert.Equal(t, []float32{1.0 / 3, 1, 0.5}, []float32{res.Scores[0].Fused, res.Scores[1].Fused, res.Scores[2].Fused})
}

// A failed rerank keeps the fused order: no rerank score, degraded.
func TestDiagnose_FailedRerankReportsNoRerankScore(t *testing.T) {
	fx := rerankFixture(t)
	fx.reranker.res = nil // bad_result
	res, err := fx.diagnose(contactsReq())
	require.NoError(t, err)
	assert.Equal(t, true, res.Result.Degraded)
	require.Len(t, res.Scores, 3)
	for _, s := range res.Scores {
		assert.Nil(t, s.Rerank)
	}
}

// A keyword-only answer reports no dense score, whatever the engine says.
func TestDiagnose_KeywordModeHasNoDenseScore(t *testing.T) {
	fx := newSearchFixture(t)
	withArmScores(fx)
	req := contactsReq()
	req.Mode = iface.RetrievalModeKeyword
	res, err := fx.diagnose(req)
	require.NoError(t, err)
	require.Len(t, res.Scores, 3)
	for _, s := range res.Scores {
		assert.Nil(t, s.Dense)
		assert.NotNil(t, s.Keyword)
	}
}

// The dense arm falling back (degraded) also reports no dense score.
func TestDiagnose_DenseFallbackHasNoDenseScore(t *testing.T) {
	fx := newSearchFixture(t)
	withArmScores(fx)
	fx.tweak = func(d *SearchDeps) { d.Embedder = nil }
	res, err := fx.diagnose(contactsReq())
	require.NoError(t, err)
	assert.Equal(t, true, res.Result.Degraded)
	require.Len(t, res.Scores, 3)
	for _, s := range res.Scores {
		assert.Nil(t, s.Dense)
		assert.NotNil(t, s.Keyword)
	}
}

// Dense mode with the dense arm up sends no keyword arm: no keyword score.
func TestDiagnose_DenseModeHasNoKeywordScore(t *testing.T) {
	fx := newSearchFixture(t)
	withArmScores(fx)
	req := contactsReq()
	req.Mode = iface.RetrievalModeDense
	res, err := fx.diagnose(req)
	require.NoError(t, err)
	require.Len(t, res.Scores, 3)
	for _, s := range res.Scores {
		assert.NotNil(t, s.Dense)
		assert.Nil(t, s.Keyword)
	}
}

func TestDiagnose_NoActiveGenerationIsEmpty(t *testing.T) {
	fx := newSearchFixture(t)
	fx.state.gens = nil
	res, err := fx.diagnose(contactsReq())
	require.NoError(t, err)
	assert.Equal(t, true, res.Result.Degraded)
	assert.Len(t, res.Result.Hits, 0)
	assert.NotNil(t, res.Scores)
	assert.Len(t, res.Scores, 0)
	assert.Equal(t, "", res.Collection)
	noEngineCall(t, fx)
	assert.Len(t, fx.log.rows, 1)
}

func TestDiagnose_NoBypassForPlatformAdmin(t *testing.T) { // Review Focus 1
	for _, role := range []string{"administrator", "super_admin"} {
		t.Run(role, func(t *testing.T) {
			fx := newSearchFixture(t)
			id := testkit.NewIdentity("u-1", "u1@example.test", role).WithTenant("t-1", []string{"org_owner"}, true)
			fx.ctx = ctxauth.WithTenantKind(id.ContextFor(context.Background(), "t-1"), "internal")
			secret := cand("secret", 1, 0, "the secret plan", "user:someone-else")
			secret.Runtime.VisibleTo = []string{"user:someone-else"}
			secret.Score = 10 // the best match
			fx.engine.results = [][]engine.Candidate{{
				secret,
				cand("s1", 1, 0, searchText1, "user:alice"),
				cand("s2", 1, 0, searchText2, "crm:c-2"),
			}}
			fx.source.visible = map[string]iface.RetrievalVisibility{"secret": {Visible: false, Revision: 1}}

			res, err := fx.diagnose(contactsReq())
			require.NoError(t, err)
			for _, h := range res.Result.Hits {
				if h.SourceID == "secret" {
					t.Fatalf("diagnostics returned a source the caller cannot see: %+v", h)
				}
				if strings.Contains(h.Snippet, "secret plan") {
					t.Fatalf("diagnostics returned refused text: %q", h.Snippet)
				}
			}
			for _, s := range res.Scores {
				if s.ChunkID == secret.Runtime.ChunkID {
					t.Fatalf("diagnostics scored a refused chunk: %+v", s)
				}
			}
			assert.Equal(t, []string{"s1", "s2"}, hitSources(&res.Result))
			assert.Equal(t, 1, fx.principals.calls)
			assert.Equal(t, []string{"tenant:*", "user:u-1"}, fx.lastQuery().Principals)
			assert.Equal(t, []string{"tenant:*", "user:u-1"}, res.Principals)
			checked := false
			for _, ids := range fx.source.calls {
				checked = checked || slices.Contains(ids, "secret")
			}
			assert.Equal(t, true, checked, "Visible was asked about the secret source")
			// The log row names only what was returned.
			require.Len(t, fx.log.rows, 1)
			assert.Equal(t, []string{"s1", "s2"}, fx.log.rows[0].SourceIDs)
			assert.Equal(t, false, slices.Contains(fx.log.rows[0].Subjects, "user:someone-else"))
		})
	}
}

// The erasure veto applies to diagnostics as to any search.
func TestDiagnose_ErasureVetoApplies(t *testing.T) {
	fx := newSearchFixture(t)
	fx.tombstones.covered = map[string]bool{"s2": true}
	res, err := fx.diagnose(contactsReq())
	require.NoError(t, err)
	assert.Equal(t, []string{"s1", "s3"}, hitSources(&res.Result))
	assert.Equal(t, []string{ChunkID("s1", 0), ChunkID("s3", 0)}, scoreChunks(res))
}

func TestDiagnose_WritesSearchLogRow(t *testing.T) {
	fx := newSearchFixture(t)
	res, err := fx.diagnose(contactsReq())
	require.NoError(t, err)
	require.Len(t, fx.log.rows, 1)
	row := fx.log.rows[0]
	assert.Equal(t, "u-1", row.UserUUID)
	assert.Equal(t, "contacts", row.Corpus)
	assert.Equal(t, "head of sales", row.Query)
	assert.Equal(t, hitSources(&res.Result), row.SourceIDs)
	assert.Equal(t, []string{"tenant:*", "user:u-1"}, row.Principals)
	assert.Equal(t, len(res.Result.Hits), row.HitCount)
	assert.Equal(t, []string{"t-1"}, fx.log.ctxTenant)

	// The same row a consumer search writes, but for its uuid and timings.
	fx2 := newSearchFixture(t)
	_, err = fx2.search(contactsReq())
	require.NoError(t, err)
	require.Len(t, fx2.log.rows, 1)
	a, b := *row, *fx2.log.rows[0]
	a.UUID, b.UUID = "", ""
	a.Timings, b.Timings = models.SearchTimings{}, models.SearchTimings{}
	a.CreatedAt, b.CreatedAt = time.Time{}, time.Time{}
	assert.Equal(t, b, a)
}

// A failed write fails the diagnostic run closed, like a search.
func TestDiagnose_LogWriteFailureFailsClosed(t *testing.T) {
	fx := newSearchFixture(t)
	fx.log.err = errors.New("insert failed")
	res, err := fx.diagnose(contactsReq())
	requireIs(t, err, iface.ErrRetrievalUnavailable)
	requireIs(t, err, ErrSearchLogWrite)
	assert.Nil(t, res)
}

// The diagnostic row goes through the PR 6 post-write re-check like any row.
func TestDiagnose_LogRowRetractedOrConfirmed(t *testing.T) {
	t.Run("covered", func(t *testing.T) {
		late := &lateTombstones{coveredLater: true}
		fx, rl := retractFixture(t, late)
		_, err := fx.diagnose(contactsReq())
		require.NoError(t, err)
		require.Len(t, rl.rows, 1)
		assert.Equal(t, true, rl.rows[0].Unchecked)
		assert.Equal(t, []string{rl.rows[0].UUID}, rl.deleted)
		assert.Len(t, rl.stored(), 0)
	})
	t.Run("not covered", func(t *testing.T) {
		late := &lateTombstones{}
		fx, rl := retractFixture(t, late)
		_, err := fx.diagnose(contactsReq())
		require.NoError(t, err)
		require.Len(t, rl.rows, 1)
		assert.Equal(t, []string{rl.rows[0].UUID}, rl.checked)
		assert.Len(t, rl.deleted, 0)
	})
}

// A failed diagnostic run still leaves its best-effort row.
func TestDiagnose_FailedRunLeavesRow(t *testing.T) {
	fx := newSearchFixture(t)
	fx.engine.err = engine.ErrUnavailable
	_, err := fx.diagnose(contactsReq())
	requireIs(t, err, iface.ErrRetrievalUnavailable)
	require.Len(t, fx.log.rows, 1)
	assert.Equal(t, models.SearchOutcomeError, fx.log.rows[0].Outcome)
	assert.Equal(t, true, fx.lastQuery().Diagnostics)
}

func TestSearch_NeverRequestsDiagnostics(t *testing.T) { // Review Focus 2
	fx := newSearchFixture(t)
	withArmScores(fx)
	s := NewSearcher(fx.deps())
	_, err := s.Diagnose(fx.ctx, contactsReq())
	require.NoError(t, err)
	before := fx.engine.calls()
	require.Equal(t, true, before > 0)

	res, err := s.Search(fx.ctx, contactsReq())
	require.NoError(t, err)
	fx.engine.mu.Lock()
	after := slices.Clone(fx.engine.queries[before:])
	fx.engine.mu.Unlock()
	require.Equal(t, true, len(after) > 0)
	for i, q := range after {
		assert.Equal(t, false, q.Diagnostics, i)
	}
	b, err := json.Marshal(res)
	require.NoError(t, err)
	for _, key := range []string{`"dense"`, `"keyword"`, `"rerank"`, `"fused"`} {
		if strings.Contains(string(b), key) {
			t.Fatalf("a consumer result carries %s: %s", key, b)
		}
	}
}

func TestDiagnose_PropagatesGateErrors(t *testing.T) {
	t.Run("forbidden", func(t *testing.T) {
		fx := newSearchFixture(t)
		fx.authz.allow = false
		res, err := fx.diagnose(contactsReq())
		requireIs(t, err, iface.ErrRetrievalForbidden)
		assert.Nil(t, res)
		noEngineCall(t, fx)
		assert.Len(t, fx.log.rows, 0)
	})
	t.Run("external tenant", func(t *testing.T) {
		fx := newSearchFixture(t)
		fx.ctx = externalCtx()
		_, err := fx.diagnose(contactsReq())
		requireIs(t, err, iface.ErrRetrievalForbidden)
		noEngineCall(t, fx)
	})
	t.Run("unknown corpus", func(t *testing.T) {
		fx := newSearchFixture(t)
		req := contactsReq()
		req.Corpus = "nope"
		_, err := fx.diagnose(req)
		requireIs(t, err, iface.ErrRetrievalUnknownCorpus)
		noEngineCall(t, fx)
	})
	t.Run("not configured", func(t *testing.T) {
		fx := newSearchFixture(t)
		fx.tweak = func(d *SearchDeps) { d.Engine = nil }
		res, err := fx.diagnose(contactsReq())
		requireIs(t, err, iface.ErrRetrievalNotConfigured)
		assert.Nil(t, res)
	})
}
