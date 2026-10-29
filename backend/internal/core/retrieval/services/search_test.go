package services

import (
	"context"
	"errors"
	"math"
	"strings"
	"testing"

	"github.com/orkestra/backend/internal/core/retrieval/engine"
	"github.com/orkestra/backend/internal/testkit"
	"github.com/orkestra/backend/pkg/sdk/ctxauth"
	"github.com/orkestra/backend/pkg/sdk/iface"
)

func requireIs(t *testing.T, err, target error) {
	t.Helper()
	if !errors.Is(err, target) {
		t.Fatalf("error %v is not %v", err, target)
	}
}

func noEngineCall(t *testing.T, fx *searchFixture) {
	t.Helper()
	if n := fx.engine.calls(); n != 0 {
		t.Fatalf("engine queried %d times, want 0", n)
	}
}

func externalCtx() context.Context {
	id := testkit.NewIdentity("u-1", "u1@example.test", "operator").WithTenant("t-1", []string{"org_member"}, true)
	return ctxauth.WithTenantKind(id.ContextFor(context.Background(), "t-1"), "external")
}

// --- Step 1: gate ---

func TestSearch_RefusesExternalTenant(t *testing.T) {
	fx := newSearchFixture(t)
	fx.ctx = externalCtx()
	res, err := fx.search(contactsReq())
	requireIs(t, err, iface.ErrRetrievalForbidden)
	assert.Nil(t, res)
	noEngineCall(t, fx)
	assert.Empty(t, fx.authz.asked)
}

func TestSearch_MissingIdentityIsForbidden(t *testing.T) {
	cases := map[string]context.Context{
		"no user, no tenant": ctxauth.WithTenantKind(context.Background(), "internal"),
		"user without tenant": ctxauth.WithTenantKind(
			testkit.NewIdentity("u-1", "u1@example.test", "operator").ContextFor(context.Background(), "-"), "internal"),
	}
	for name, ctx := range cases {
		t.Run(name, func(t *testing.T) {
			fx := newSearchFixture(t)
			fx.ctx = ctx
			_, err := fx.search(contactsReq())
			requireIs(t, err, iface.ErrRetrievalForbidden)
			noEngineCall(t, fx)
			assert.Empty(t, fx.authz.asked)
		})
	}
}

func TestSearch_RefusesCallerWithoutCorpusPermission(t *testing.T) {
	fx := newSearchFixture(t)
	fx.authz.allow = false
	res, err := fx.search(contactsReq())
	requireIs(t, err, iface.ErrRetrievalForbidden)
	assert.Nil(t, res)
	assert.Equal(t, []string{"u-1|t-1|crm.contact.read"}, fx.authz.asked)
	noEngineCall(t, fx)
	assert.Equal(t, 0, fx.principals.calls)
}

func TestSearch_PermissionCheckErrorFailsClosed(t *testing.T) {
	fx := newSearchFixture(t)
	fx.authz.err = errors.New("authz store down")
	res, err := fx.search(contactsReq())
	if err == nil || errors.Is(err, iface.ErrRetrievalForbidden) {
		t.Fatalf("err = %v, want a non-forbidden error", err)
	}
	requireIs(t, err, iface.ErrRetrievalUnavailable)
	assert.Nil(t, res)
	noEngineCall(t, fx)
}

func TestSearch_UnknownCorpus(t *testing.T) {
	for _, name := range []string{"deals", ""} {
		fx := newSearchFixture(t)
		req := contactsReq()
		req.Corpus = name
		_, err := fx.search(req)
		requireIs(t, err, iface.ErrRetrievalUnknownCorpus)
		assert.Empty(t, fx.authz.asked)
		noEngineCall(t, fx)
	}
}

func TestSearch_InvalidRequest(t *testing.T) {
	fx := newSearchFixture(t)
	req := contactsReq()
	req.Limit = 101
	_, err := fx.search(req)
	requireIs(t, err, iface.ErrRetrievalInvalidRequest)
	noEngineCall(t, fx)
}

func TestSearch_InvalidFilter(t *testing.T) {
	fx := newSearchFixture(t)
	req := contactsReq()
	req.Filter = &iface.RetrievalFilter{Field: "salary", Op: iface.RetrievalOpEq, Value: 1}
	_, err := fx.search(req)
	requireIs(t, err, iface.ErrRetrievalInvalidFilter)
	noEngineCall(t, fx)
}

func TestSearch_OwnerModuleDisabled(t *testing.T) {
	fx := newSearchFixture(t)
	fx.modules.started["crm"] = false
	_, err := fx.search(contactsReq())
	requireIs(t, err, iface.ErrRetrievalCorpusUnavailable)
	noEngineCall(t, fx)
	assert.Equal(t, 0, fx.state.reads())
}

func TestSearch_GateOrder(t *testing.T) {
	badFilter := &iface.RetrievalFilter{Field: "salary", Op: iface.RetrievalOpEq, Value: 1}

	t.Run("external tenant before unknown corpus", func(t *testing.T) {
		fx := newSearchFixture(t)
		fx.ctx = externalCtx()
		_, err := fx.search(iface.RetrievalSearchRequest{Corpus: "deals", Query: "x"})
		requireIs(t, err, iface.ErrRetrievalForbidden)
	})
	t.Run("permission before filter", func(t *testing.T) {
		fx := newSearchFixture(t)
		fx.authz.allow = false
		req := contactsReq()
		req.Filter = badFilter
		_, err := fx.search(req)
		requireIs(t, err, iface.ErrRetrievalForbidden)
	})
	t.Run("filter before owner module", func(t *testing.T) {
		fx := newSearchFixture(t)
		fx.modules.started["crm"] = false
		req := contactsReq()
		req.Filter = badFilter
		_, err := fx.search(req)
		requireIs(t, err, iface.ErrRetrievalInvalidFilter)
	})
	t.Run("corpus lookup before request shape", func(t *testing.T) { // P7
		fx := newSearchFixture(t)
		_, err := fx.search(iface.RetrievalSearchRequest{Corpus: "deals", Query: "x", Limit: 101})
		requireIs(t, err, iface.ErrRetrievalUnknownCorpus)
	})
	t.Run("request shape before permission", func(t *testing.T) {
		fx := newSearchFixture(t)
		fx.authz.allow = false
		req := contactsReq()
		req.Limit = 101
		_, err := fx.search(req)
		requireIs(t, err, iface.ErrRetrievalInvalidRequest)
		assert.Empty(t, fx.authz.asked)
	})
}

func TestSearch_NilDependenciesFailClosed(t *testing.T) {
	cases := []struct {
		name  string
		tweak func(d *SearchDeps)
		want  []error
	}{
		{"corpora", func(d *SearchDeps) { d.Corpora = nil }, []error{iface.ErrRetrievalUnavailable}},
		{"authz", func(d *SearchDeps) { d.Authz = nil }, []error{iface.ErrRetrievalUnavailable}},
		{"modules", func(d *SearchDeps) { d.Modules = nil }, []error{iface.ErrRetrievalCorpusUnavailable}},
		{"log", func(d *SearchDeps) { d.Log = nil }, []error{iface.ErrRetrievalUnavailable, ErrSearchLogWrite}},
		{"state", func(d *SearchDeps) { d.State = nil }, []error{iface.ErrRetrievalUnavailable}},
		{"principals", func(d *SearchDeps) { d.Principals = nil }, []error{iface.ErrRetrievalUnavailable}},
		{"tombstones", func(d *SearchDeps) { d.Tombstones = nil }, []error{iface.ErrRetrievalVisibilityCheck}},
		{"source", func(d *SearchDeps) {
			spec := cloneSpec(d.Corpora.(*searchCorpora).specs["contacts"])
			spec.Source = nil
			d.Corpora = sharedLookup{spec: spec}
		}, []error{iface.ErrRetrievalVisibilityCheck}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fx := newSearchFixture(t)
			fx.tweak = tc.tweak
			res, err := fx.search(contactsReq())
			for _, w := range tc.want {
				requireIs(t, err, w)
			}
			if errors.Is(err, iface.ErrRetrievalForbidden) {
				t.Fatalf("a missing dependency must not read as forbidden: %v", err)
			}
			assert.Nil(t, res)
			noEngineCall(t, fx)
		})
	}
}

// --- Step 2: resolve and pin ---

func TestSearch_NotConfigured(t *testing.T) {
	fx := newSearchFixture(t)
	fx.tweak = func(d *SearchDeps) { d.Engine = nil }
	res, err := fx.search(contactsReq())
	requireIs(t, err, iface.ErrRetrievalNotConfigured)
	assert.Nil(t, res)
	assert.Equal(t, 0, fx.state.reads())
}

func TestSearch_NoActiveGenerationIsEmptyAndDegraded(t *testing.T) {
	fx := newSearchFixture(t)
	fx.state.gens = nil // the fake answers ErrNoActiveGeneration
	res, err := fx.search(contactsReq())
	require.NoError(t, err)
	require.NotNil(t, res)
	if res.Hits == nil {
		t.Fatal("Hits is nil, want an empty slice")
	}
	assert.Len(t, res.Hits, 0)
	assert.Equal(t, true, res.Degraded)
	noEngineCall(t, fx)
	assert.Empty(t, fx.embedder.reqs)
}

func TestSearch_StateErrorIsUnavailable(t *testing.T) {
	fx := newSearchFixture(t)
	fx.state.err = errors.New("mongo down")
	res, err := fx.search(contactsReq())
	requireIs(t, err, iface.ErrRetrievalUnavailable)
	assert.Nil(t, res)
	noEngineCall(t, fx)
}

func TestSearch_EmptyPinnedCollectionFailsClosed(t *testing.T) {
	fx := newSearchFixture(t)
	fx.state.gens = []PinnedGeneration{{Collection: "", Generation: 1, EmbedProfileID: "ip_embed_a"}}
	_, err := fx.search(contactsReq())
	requireIs(t, err, iface.ErrRetrievalUnavailable)
	noEngineCall(t, fx)
}

func TestSearch_PinsCollectionAndProfileOnce(t *testing.T) {
	fx := newSearchFixture(t)
	fx.state.gens = []PinnedGeneration{
		{Collection: "orkestra_contacts_g1", Generation: 1, EmbedProfileID: "ip_embed_a"},
		{Collection: "orkestra_contacts_g2", Generation: 2, EmbedProfileID: "ip_embed_b"},
	}
	_, err := fx.search(contactsReq())
	require.NoError(t, err)
	assert.Equal(t, 1, fx.state.reads())
	assert.Equal(t, []string{"contacts"}, fx.state.corpora)
	assert.Equal(t, []string{"orkestra_contacts_g1"}, fx.engine.collections)
	require.Len(t, fx.embedder.reqs, 1)
	assert.Equal(t, "ip_embed_a", fx.embedder.reqs[0].Profile)
}

// --- Step 3: principals ---

func TestSearch_InjectsTenantAndPrincipals(t *testing.T) {
	fx := newSearchFixture(t)
	_, err := fx.search(contactsReq())
	require.NoError(t, err)
	q := fx.lastQuery()
	assert.Equal(t, "t-1", q.TenantID)
	assert.Equal(t, []string{"tenant:*", "user:u-1"}, q.Principals)
	assert.Equal(t, false, q.Diagnostics)
}

func TestSearch_PlatformRoleGetsNoBypass(t *testing.T) {
	fx := newSearchFixture(t)
	id := testkit.NewIdentity("u-1", "u1@example.test", "super_admin").WithTenant("t-1", []string{"org_owner"}, true)
	fx.ctx = ctxauth.WithTenantKind(id.ContextFor(context.Background(), "t-1"), "internal")
	_, err := fx.search(contactsReq())
	require.NoError(t, err)
	assert.Equal(t, 1, fx.principals.calls)
	assert.Equal(t, []string{"tenant:*", "user:u-1"}, fx.lastQuery().Principals)
}

func TestSearch_PrincipalResolutionErrorFailsClosed(t *testing.T) { // Review Focus 5
	boom := errors.New("contributor teams: team:secret-team-42 lookup failed")
	cases := map[string]error{
		"contributor error": boom,
		"cap exceeded":      ErrPrincipalCapExceeded,
		"reserved prefix":   ErrContributorPrincipal,
	}
	for name, perr := range cases {
		t.Run(name, func(t *testing.T) {
			fx := newSearchFixture(t)
			fx.principals.err = perr
			res, err := fx.search(contactsReq())
			if perr == boom {
				// The resolver's own error is never in the chain: its text may
				// name a contributor's principals (F1, R-T6a).
				if errors.Is(err, boom) {
					t.Fatalf("the resolver's error is in the chain: %v", err)
				}
			} else {
				requireIs(t, err, perr)
			}
			requireIs(t, err, iface.ErrRetrievalUnavailable)
			assert.Nil(t, res)
			noEngineCall(t, fx)
			assert.Empty(t, fx.embedder.reqs)
			if strings.Contains(err.Error(), "secret-team-42") {
				t.Fatalf("a principal error's text was returned: %v", err)
			}
			if strings.Contains(fx.logs.String(), "secret-team-42") {
				t.Fatalf("a principal error's text reached the log: %s", fx.logs.String())
			}
		})
	}
}

func TestSearch_PrincipalForbiddenStaysForbidden(t *testing.T) {
	fx := newSearchFixture(t)
	fx.principals.err = iface.ErrRetrievalForbidden
	_, err := fx.search(contactsReq())
	requireIs(t, err, iface.ErrRetrievalForbidden)
	noEngineCall(t, fx)
}

func TestSearch_PrincipalContextMustMatchCaller(t *testing.T) {
	cases := map[string]iface.RetrievalPrincipalContext{
		"other tenant":  {TenantID: "t-2", UserUUID: "u-1", Principals: []string{"user:u-1"}},
		"other user":    {TenantID: "t-1", UserUUID: "u-2", Principals: []string{"user:u-2"}},
		"no principals": {TenantID: "t-1", UserUUID: "u-1"},
		"empty entry":   {TenantID: "t-1", UserUUID: "u-1", Principals: []string{"user:u-1", ""}},
	}
	for name, pc := range cases {
		t.Run(name, func(t *testing.T) {
			fx := newSearchFixture(t)
			fx.principals.pc = pc
			_, err := fx.search(contactsReq())
			requireIs(t, err, iface.ErrRetrievalUnavailable)
			noEngineCall(t, fx)
		})
	}
}

// --- Step 4: arms ---

func TestSearch_HybridSendsBothArms(t *testing.T) {
	fx := newSearchFixture(t)
	res, err := fx.search(contactsReq())
	require.NoError(t, err)
	q := fx.lastQuery()
	assert.Equal(t, []float32{0.1, 0.2, 0.3, 0.4}, q.Dense)
	assert.Equal(t, "head of sales", q.Keyword)
	assert.Equal(t, engine.Weights{Dense: 1, Keyword: 1}, q.Weights)
	assert.Equal(t, 100, q.PerArm)
	assert.Equal(t, 40, q.Limit)
	assert.Nil(t, q.Filter)
	require.Len(t, fx.embedder.reqs, 1)
	assert.Equal(t, iface.PinnedEmbedRequest{
		Caller: "retrieval", OnBehalfOf: "t-1", Profile: "ip_embed_a",
		Kind: iface.EmbedInputQuery, Inputs: []string{"head of sales"},
	}, fx.embedder.reqs[0])
	assert.Equal(t, []string{"embed"}, fx.profiles.calls)
	assert.Equal(t, false, res.Degraded)
	assert.Len(t, fx.engine.queries, 1)
}

func TestSearch_CompiledFilterReachesEngine(t *testing.T) {
	fx := newSearchFixture(t)
	req := contactsReq()
	req.Filter = &iface.RetrievalFilter{Field: "team", Op: iface.RetrievalOpEq, Value: "north"}
	_, err := fx.search(req)
	require.NoError(t, err)
	assert.Equal(t, &engine.Filter{Field: "team", Match: []any{"north"}}, fx.lastQuery().Filter)
}

func TestSearch_ModeKeywordSkipsInference(t *testing.T) {
	fx := newSearchFixture(t)
	req := contactsReq()
	req.Mode = iface.RetrievalModeKeyword
	res, err := fx.search(req)
	require.NoError(t, err)
	assert.Empty(t, fx.embedder.reqs)
	assert.Empty(t, fx.profiles.calls)
	q := fx.lastQuery()
	assert.Nil(t, q.Dense)
	assert.Equal(t, "head of sales", q.Keyword)
	assert.Equal(t, false, res.Degraded)
}

func TestSearch_ModeDenseSkipsKeyword(t *testing.T) {
	fx := newSearchFixture(t)
	req := contactsReq()
	req.Mode = iface.RetrievalModeDense
	res, err := fx.search(req)
	require.NoError(t, err)
	q := fx.lastQuery()
	assert.Equal(t, "", q.Keyword)
	assert.Equal(t, []float32{0.1, 0.2, 0.3, 0.4}, q.Dense)
	assert.Equal(t, false, res.Degraded)
}

func TestSearch_ProfileMismatchFallsBackToKeyword(t *testing.T) { // Review Focus 3
	cases := []struct {
		name    string
		setup   func(fx *searchFixture)
		noEmbed bool
	}{
		{"endpoint serves another profile", func(fx *searchFixture) { fx.profiles.profiles["embed"].ID = "ip_embed_b" }, true},
		{"profile lookup reports a mismatch", func(fx *searchFixture) {
			fx.profiles.errs = map[string]error{"embed": iface.ErrLLMProfileMismatch}
		}, true},
		{"embed reports a mismatch", func(fx *searchFixture) { fx.embedder.err = iface.ErrLLMProfileMismatch }, false},
		{"embed provider unavailable", func(fx *searchFixture) { fx.embedder.err = iface.ErrLLMProviderUnavailable }, false},
		{"vector of the wrong length", func(fx *searchFixture) { fx.embedder.vec = []float32{0.1, 0.2, 0.3} }, false},
		// Beyond the plan's five: every other unusable answer degrades the same way.
		{"endpoint not configured", func(fx *searchFixture) {
			fx.profiles.errs = map[string]error{"embed": iface.ErrLLMNotConfigured}
		}, true},
		{"no profile", func(fx *searchFixture) { delete(fx.profiles.profiles, "embed") }, true},
		{"endpoint is not an embedder", func(fx *searchFixture) {
			fx.profiles.profiles["embed"].Kind = iface.InferenceKindRerank
		}, true},
		{"empty pinned profile", func(fx *searchFixture) {
			fx.state.gens = []PinnedGeneration{{Collection: "orkestra_contacts_g1", Generation: 1}}
			fx.profiles.profiles["embed"].ID = ""
		}, true},
		{"two vectors", func(fx *searchFixture) { fx.embedder.vectors = [][]float32{{1, 2, 3, 4}, {1, 2, 3, 4}} }, false},
		{"empty vector", func(fx *searchFixture) { fx.embedder.vectors = [][]float32{{}} }, false},
		// A non-finite value is a bad vector too: the arm degrades, the
		// search does not fail.
		{"NaN in the vector", func(fx *searchFixture) {
			fx.embedder.vec = []float32{0.1, float32(math.NaN()), 0.3, 0.4}
		}, false},
		{"+Inf in the vector", func(fx *searchFixture) {
			fx.embedder.vec = []float32{0.1, 0.2, float32(math.Inf(1)), 0.4}
		}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fx := newSearchFixture(t)
			tc.setup(fx)
			res, err := fx.search(contactsReq())
			require.NoError(t, err)
			assert.Equal(t, true, res.Degraded)
			q := fx.lastQuery()
			assert.Nil(t, q.Dense)
			assert.Equal(t, "head of sales", q.Keyword)
			if tc.noEmbed {
				assert.Len(t, fx.embedder.reqs, 0)
			}
			assert.Len(t, res.Hits, 3)
		})
	}
}

func TestSearch_MissingInferenceCompanionsAreKeywordDegraded(t *testing.T) {
	for name, tweak := range map[string]func(d *SearchDeps){
		"profiles": func(d *SearchDeps) { d.Profiles = nil },
		"embedder": func(d *SearchDeps) { d.Embedder = nil },
	} {
		t.Run(name, func(t *testing.T) {
			fx := newSearchFixture(t)
			fx.tweak = tweak
			res, err := fx.search(contactsReq())
			require.NoError(t, err)
			assert.Equal(t, true, res.Degraded)
			assert.Nil(t, fx.lastQuery().Dense)
			assert.Equal(t, "head of sales", fx.lastQuery().Keyword)
			assert.Empty(t, fx.embedder.reqs)
		})
	}
}

func TestSearch_LocalOnlyRefusesHostedEmbedEndpoint(t *testing.T) {
	t.Run("local_only refuses hosted", func(t *testing.T) {
		for _, loc := range []iface.RetrievalLocality{"", iface.RetrievalLocalOnly} {
			fx := newSearchFixture(t)
			fx.spec.Locality = loc
			fx.profiles.profiles["embed"].Boundary = iface.InferenceBoundaryHosted
			res, err := fx.search(contactsReq())
			require.NoError(t, err)
			assert.Len(t, fx.embedder.reqs, 0)
			assert.Equal(t, true, res.Degraded)
			assert.Nil(t, fx.lastQuery().Dense)
			assert.Equal(t, "head of sales", fx.lastQuery().Keyword)
		}
	})
	t.Run("hosted_allowed embeds", func(t *testing.T) {
		fx := newSearchFixture(t)
		fx.spec.Locality = iface.RetrievalHostedAllowed
		fx.profiles.profiles["embed"].Boundary = iface.InferenceBoundaryHosted
		res, err := fx.search(contactsReq())
		require.NoError(t, err)
		assert.Len(t, fx.embedder.reqs, 1)
		assert.Equal(t, false, res.Degraded)
		assert.NotNil(t, fx.lastQuery().Dense)
	})
}

func TestSearch_ModeDenseWithInferenceDownIsKeywordDegraded(t *testing.T) {
	fx := newSearchFixture(t)
	fx.embedder.err = iface.ErrLLMProviderUnavailable
	req := contactsReq()
	req.Mode = iface.RetrievalModeDense
	res, err := fx.search(req)
	require.NoError(t, err)
	q := fx.lastQuery()
	assert.Equal(t, "head of sales", q.Keyword)
	assert.Nil(t, q.Dense)
	assert.Equal(t, true, res.Degraded)
}

func TestSearch_DenseFallbackWarnCarriesNoQueryText(t *testing.T) {
	fx := newSearchFixture(t)
	fx.profiles.profiles["embed"].ID = "ip_embed_b"
	_, err := fx.search(iface.RetrievalSearchRequest{Corpus: "contacts", Query: "zanzibar quarterly"})
	require.NoError(t, err)
	out := fx.logs.String()
	assert.Contains(t, out, "level=WARN")
	assert.Contains(t, out, "endpoint=embed")
	assert.Contains(t, out, "reason=profile_mismatch")
	if strings.Contains(out, "zanzibar") {
		t.Fatalf("the query text reached the log: %s", out)
	}
}

func TestSearch_EmbedCanceledReturnsContextError(t *testing.T) {
	fx := newSearchFixture(t)
	ctx, cancel := context.WithCancel(fx.ctx)
	defer cancel()
	fx.ctx = ctx
	fx.embedder.onCall = cancel
	fx.embedder.err = iface.ErrLLMProviderUnavailable
	res, err := fx.search(contactsReq())
	requireIs(t, err, context.Canceled)
	assert.Nil(t, res)
	noEngineCall(t, fx)
}

// --- Step 5: fusion ---

func TestSearch_FusionOverrideWithinBounds(t *testing.T) {
	fx := newSearchFixture(t)
	req := contactsReq()
	req.Fusion = &iface.RetrievalFusion{DenseWeight: 3}
	_, err := fx.search(req)
	require.NoError(t, err)
	assert.Equal(t, engine.Weights{Dense: 3, Keyword: 1}, fx.lastQuery().Weights)
}

func TestSearch_EngineErrorsAreUnavailable(t *testing.T) {
	for _, eerr := range []error{engine.ErrUnavailable, engine.ErrNotFound, errors.New("x")} {
		t.Run(eerr.Error(), func(t *testing.T) {
			fx := newSearchFixture(t)
			fx.engine.err = eerr
			res, err := fx.search(contactsReq())
			requireIs(t, err, iface.ErrRetrievalUnavailable)
			assert.Nil(t, res)
			assert.Len(t, fx.engine.queries, 1)
		})
	}
}

// --- Step 8: result ---

func TestSearch_HitsCarrySnippetProvenanceLocality(t *testing.T) {
	fx := newSearchFixture(t)
	first := &fx.engine.results[0][0]
	first.Runtime.Page = 2
	first.Runtime.CharStart = 120
	first.Runtime.CharEnd = 177
	res, err := fx.search(contactsReq())
	require.NoError(t, err)
	require.Len(t, res.Hits, 3)
	assert.Equal(t, iface.RetrievalHit{
		SourceID: "s1",
		ChunkID:  "s1#0",
		Rank:     1,
		Score:    1,
		Snippet:  Snippet(searchText1, "head of sales", SnippetMaxRunes),
		Provenance: iface.RetrievalProvenance{
			SourceID: "s1", ChunkID: "s1#0", Ordinal: 0, Page: 2, CharStart: 120, CharEnd: 177,
		},
		Locality: iface.RetrievalLocalOnly,
	}, res.Hits[0])
	for i, h := range res.Hits {
		assert.Equal(t, i+1, h.Rank)
	}
	assert.Equal(t, []string{"s1", "s2", "s3"}, []string{res.Hits[0].SourceID, res.Hits[1].SourceID, res.Hits[2].SourceID})
	assert.Equal(t, false, res.Degraded)
}

func TestSearch_HitsReportHostedAllowedLocality(t *testing.T) {
	fx := newSearchFixture(t)
	fx.spec.Locality = iface.RetrievalHostedAllowed
	res, err := fx.search(contactsReq())
	require.NoError(t, err)
	require.Len(t, res.Hits, 3)
	assert.Equal(t, iface.RetrievalHostedAllowed, res.Hits[0].Locality)
}

func TestSearch_HitsKeepTheFirstLimitCandidates(t *testing.T) {
	fx := newSearchFixture(t)
	req := contactsReq()
	req.Limit = 2
	res, err := fx.search(req)
	require.NoError(t, err)
	require.Len(t, res.Hits, 2)
	assert.Equal(t, "s1", res.Hits[0].SourceID)
	assert.Equal(t, "s2", res.Hits[1].SourceID)
	assert.Equal(t, 8, fx.lastQuery().Limit) // Fetch = min(100, 4*2)
}

func TestSearch_EmptyEngineAnswerHasNonNilHits(t *testing.T) {
	fx := newSearchFixture(t)
	fx.engine.results = nil
	res, err := fx.search(contactsReq())
	require.NoError(t, err)
	if res.Hits == nil {
		t.Fatal("Hits is nil, want an empty slice")
	}
	assert.Len(t, res.Hits, 0)
}

// sharedLookup hands out its spec without copying, so only the Searcher's own
// copy can keep a caller's mutation out of the next answer.
type sharedLookup struct{ spec iface.RetrievalCorpusSpec }

func (l sharedLookup) Corpus(name string) (iface.RetrievalCorpusSpec, bool) {
	return l.spec, name == l.spec.Name
}

func TestSearcher_FilterSchema(t *testing.T) {
	fx := newSearchFixture(t)
	fx.tweak = func(d *SearchDeps) { d.Corpora = sharedLookup{spec: cloneSpec(fx.spec)} }
	s := NewSearcher(fx.deps())
	got, err := s.FilterSchema("contacts")
	require.NoError(t, err)
	assert.Equal(t, fx.spec.Filters, got)
	got[0].Name = "mutated"
	again, err := s.FilterSchema("contacts")
	require.NoError(t, err)
	assert.Equal(t, "team", again[0].Name)

	_, err = s.FilterSchema("deals")
	requireIs(t, err, iface.ErrRetrievalUnknownCorpus)

	_, err = NewSearcher(SearchDeps{}).FilterSchema("contacts")
	requireIs(t, err, iface.ErrRetrievalUnknownCorpus)
}
