package services

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"time"

	"github.com/orkestra/backend/internal/core/retrieval/engine"
	"github.com/orkestra/backend/internal/core/retrieval/filter"
	"github.com/orkestra/backend/internal/core/retrieval/models"
	"github.com/orkestra/backend/pkg/sdk/ctxauth"
	"github.com/orkestra/backend/pkg/sdk/iface"
	"github.com/orkestra/backend/pkg/sdk/module"
	"github.com/orkestra/backend/pkg/sdk/tenantrepo"
)

// CorpusLookup reads a registered corpus; satisfied by *Service.
type CorpusLookup interface {
	Corpus(name string) (iface.RetrievalCorpusSpec, bool)
}

// PinnedGeneration is the pair search pins for the whole request (ADR D5 step 2).
type PinnedGeneration struct {
	Collection     string // <prefix>_<corpus>_g<generation>
	Generation     int
	EmbedProfileID string // InferenceProfile.ID the generation was built with
}

var ErrNoActiveGeneration = errors.New("retrieval: the corpus has no active generation yet")

// ErrSearchLogWrite means the search-log row could not be written, so the
// search fails closed (plan decision 2).
var ErrSearchLogWrite = errors.New("retrieval: search log write failed")

// CorpusStateReader reads the generation a search pins. A corpus without an
// active generation returns ErrNoActiveGeneration.
type CorpusStateReader interface {
	ActiveGeneration(ctx context.Context, corpus string) (PinnedGeneration, error)
}

// SearchLogWriter stores one search-log row; satisfied by *repository.SearchLog.
type SearchLogWriter interface {
	Insert(ctx context.Context, row *models.SearchLog) error
}

// DefaultVisibleTimeout bounds the owning module's Visible call.
const DefaultVisibleTimeout = 2 * time.Second

// searchCaller is the caller attributed in every inference request.
const searchCaller = "retrieval"

// SearchDeps are the Searcher's collaborators. A nil dependency the pipeline
// needs fails the search closed; only the inference companions are optional.
type SearchDeps struct {
	Corpora        CorpusLookup
	State          CorpusStateReader
	Engine         engine.Engine // nil: QDRANT_URL unset -> ErrRetrievalNotConfigured
	Tombstones     TombstoneChecker
	Authz          iface.AuthzProvider
	Principals     iface.RetrievalPrincipalResolver
	Profiles       iface.LLMInferenceProfiles // nil: dense arm and rerank unavailable
	Embedder       iface.LLMPinnedEmbedder    // nil: dense arm unavailable
	Reranker       iface.LLMReranker          // nil: rerank unavailable
	Modules        module.ModuleStatusReader
	Log            SearchLogWriter
	Now            func() time.Time // nil: time.Now
	Logger         *slog.Logger
	VisibleTimeout time.Duration // 0: DefaultVisibleTimeout
}

// Searcher runs the search pipeline of ADR-0023 D5.
type Searcher struct {
	d SearchDeps
}

// NewSearcher applies the defaults of SearchDeps.
func NewSearcher(d SearchDeps) *Searcher {
	if d.Now == nil {
		d.Now = time.Now
	}
	if d.Logger == nil {
		d.Logger = slog.Default()
	}
	if d.VisibleTimeout <= 0 {
		d.VisibleTimeout = DefaultVisibleTimeout
	}
	return &Searcher{d: d}
}

// gated is what the gate established about a request.
type gated struct {
	tenantID string
	userUUID string
	spec     iface.RetrievalCorpusSpec
	plan     searchPlan
	filter   *engine.Filter
}

func unavailable(what string) error {
	return fmt.Errorf("%w: %s", iface.ErrRetrievalUnavailable, what)
}

// Search runs the pipeline in ADR order. Nothing in this method logs the
// query, a snippet, chunk text, principals or subjects.
func (s *Searcher) Search(ctx context.Context, req iface.RetrievalSearchRequest) (*iface.RetrievalSearchResult, error) {
	// Step 1: gate.
	g, err := s.gate(ctx, req)
	if err != nil {
		return nil, err
	}
	if s.d.Log == nil {
		// A result nobody can answer for is never served (plan decision 2).
		return nil, fmt.Errorf("%w: %w", iface.ErrRetrievalUnavailable, ErrSearchLogWrite)
	}
	if s.d.Tombstones == nil || g.spec.Source == nil {
		// Step 6 could not run, so no candidate may be fetched at all.
		return nil, fmt.Errorf("%w: the erasure or source check is not configured", iface.ErrRetrievalVisibilityCheck)
	}

	// Step 2: resolve and pin.
	if s.d.Engine == nil {
		return nil, iface.ErrRetrievalNotConfigured
	}
	pinned, err := s.pin(ctx, g.spec.Name)
	if errors.Is(err, ErrNoActiveGeneration) {
		return &iface.RetrievalSearchResult{Hits: []iface.RetrievalHit{}, Degraded: true}, nil
	}
	if err != nil {
		return nil, err
	}

	// Step 3: principals.
	pc, err := s.principals(ctx, g)
	if err != nil {
		return nil, err
	}

	// Step 4: arms.
	var (
		vec      []float32
		denseOK  bool
		degraded bool
	)
	if g.plan.Mode != iface.RetrievalModeKeyword {
		vec, denseOK, err = s.denseVector(ctx, g, pinned, req.Query)
		if err != nil {
			return nil, err
		}
		degraded = !denseOK
	}
	keyword := req.Query
	if g.plan.Mode == iface.RetrievalModeDense && denseOK {
		keyword = ""
	}

	// Steps 5 and 6: fusion, then erasure veto and source check, with one
	// refill when the drops shorten the page.
	cands, short, err := s.fetchChecked(ctx, g, pinned.Collection, pc, engine.Query{
		TenantID:    g.tenantID,
		Principals:  append([]string(nil), pc.Principals...),
		Filter:      g.filter,
		Dense:       vec,
		Keyword:     keyword,
		Weights:     engine.Weights{Dense: g.plan.Fusion.DenseWeight, Keyword: g.plan.Fusion.KeywordWeight},
		PerArm:      g.plan.Fusion.Candidates,
		Limit:       g.plan.Fetch,
		Diagnostics: false,
	})
	if err != nil {
		return nil, err
	}
	// Never fewer than the permitted results without a flag (D5).
	degraded = degraded || short

	// Step 7: optional rerank of the checked candidates.
	if g.plan.RerankK > 0 && len(cands) > 0 {
		if ordered, _, ok := s.rerank(ctx, g.tenantID, g.spec, req.Query, cands, g.plan.RerankK); ok {
			cands = ordered
		} else {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			degraded = true
		}
	}

	// Step 8: result.
	cands = cands[:min(len(cands), g.plan.Limit)]
	return &iface.RetrievalSearchResult{Hits: s.toHits(g.spec, req.Query, cands), Degraded: degraded}, nil
}

// fetchChecked runs the engine query q (step 5) and step 6 over its answer.
// When the checks leave fewer than plan.Limit survivors, the engine returned
// a full page and plan.Fusion.Candidates allows more, it queries once more
// with Limit = plan.Fusion.Candidates and checks only the points the first
// call did not return (plan decision 3). There is never a third call. short
// reports a page still under plan.Limit whose last engine call came back
// full, so more permitted results may exist.
func (s *Searcher) fetchChecked(ctx context.Context, g gated, collection string, pc iface.RetrievalPrincipalContext,
	q engine.Query) (survivors []engine.Candidate, short bool, err error) {
	first, err := s.query(ctx, g, collection, q)
	if err != nil {
		return nil, false, err
	}
	survivors, err = s.checkCandidates(ctx, g.tenantID, g.spec.Name, g.spec.Source, pc, first)
	if err != nil {
		return nil, false, err
	}
	last, lastLimit := first, q.Limit
	if len(survivors) < g.plan.Limit && len(first) >= q.Limit && q.Limit < g.plan.Fusion.Candidates {
		q.Limit = g.plan.Fusion.Candidates
		second, err := s.query(ctx, g, collection, q)
		if err != nil {
			return nil, false, err
		}
		seen := make(map[string]bool, len(first)+len(second))
		for _, c := range first {
			seen[c.PointID] = true
		}
		var fresh []engine.Candidate
		for _, c := range second {
			if !seen[c.PointID] {
				seen[c.PointID] = true
				fresh = append(fresh, c)
			}
		}
		more, err := s.checkCandidates(ctx, g.tenantID, g.spec.Name, g.spec.Source, pc, fresh)
		if err != nil {
			return nil, false, err
		}
		survivors = append(survivors, more...)
		last, lastLimit = second, q.Limit
	}
	return survivors, len(survivors) < g.plan.Limit && len(last) >= lastLimit, nil
}

// checkCandidates applies ADR D5 step 6 to cands in order and returns the
// survivors in the same order. Any error wraps iface.ErrRetrievalVisibilityCheck.
//
// In order: a candidate of another tenant (or without a source ID) is
// dropped; one Probe per distinct source, in first-seen order and carrying
// the sorted union of its chunks' subjects, goes to the erasure veto and a
// covered source is dropped; the remaining sources go to src.Visible under
// VisibleTimeout, and a source missing from its answer, not visible, or
// visible at another revision than the candidate's is dropped. A failed or
// timed-out check returns no candidate at all. Neither the checker's nor the
// source's error text is passed on: it may name a subject or a record.
func (s *Searcher) checkCandidates(ctx context.Context, tenantID, corpus string, src iface.RetrievalSource,
	pc iface.RetrievalPrincipalContext, cands []engine.Candidate) ([]engine.Candidate, error) {
	if s.d.Tombstones == nil || src == nil {
		return nil, fmt.Errorf("%w: the erasure or source check is not configured", iface.ErrRetrievalVisibilityCheck)
	}

	// 1. Second guard behind the engine's own tenant filter.
	kept := make([]engine.Candidate, 0, len(cands))
	foreign := false
	for _, c := range cands {
		if c.Runtime.TenantID != tenantID || c.Runtime.SourceID == "" {
			foreign = true
			continue
		}
		kept = append(kept, c)
	}
	if foreign {
		s.d.Logger.Warn("retrieval: search dropped candidates of another tenant or without a source",
			slog.String("corpus", corpus))
	}
	if len(kept) == 0 {
		return []engine.Candidate{}, nil
	}

	// 2. One probe per source, in first-seen order.
	var order []string
	subjects := map[string][]string{}
	for _, c := range kept {
		id := c.Runtime.SourceID
		if _, ok := subjects[id]; !ok {
			order = append(order, id)
			subjects[id] = nil
		}
		for _, sub := range c.Runtime.Subjects {
			if !slices.Contains(subjects[id], sub) {
				subjects[id] = append(subjects[id], sub)
			}
		}
	}
	probes := make([]Probe, 0, len(order))
	for _, id := range order {
		subs := subjects[id]
		slices.Sort(subs)
		probes = append(probes, Probe{SourceID: id, Subjects: subs})
	}

	// 3. Erasure veto. A source the answer does not mention counts as covered.
	covered, err := s.d.Tombstones.Covered(ctx, tenantID, corpus, probes)
	if err == nil && ctx.Err() != nil {
		err = ctx.Err()
	}
	if err != nil {
		return nil, s.checkFailed(ctx, ctx, tenantID, corpus, "erasure_veto")
	}
	var ids []string
	for _, id := range order {
		if c, ok := covered[id]; ok && !c {
			ids = append(ids, id)
		}
	}
	if len(ids) == 0 {
		return []engine.Candidate{}, nil
	}

	// 4. The owning module's late visibility check.
	vctx, cancel := context.WithTimeout(ctx, s.d.VisibleTimeout)
	defer cancel()
	callerPC := pc
	callerPC.Principals = slices.Clone(pc.Principals)
	vis, err := src.Visible(vctx, tenantID, callerPC, ids)
	if err == nil && vctx.Err() != nil {
		// An answer that arrives after the deadline is not trusted either.
		err = vctx.Err()
	}
	if err != nil {
		return nil, s.checkFailed(ctx, vctx, tenantID, corpus, "source_visible")
	}
	allowed := make(map[string]bool, len(ids))
	for _, id := range ids {
		allowed[id] = true
	}
	out := make([]engine.Candidate, 0, len(kept))
	for _, c := range kept {
		id := c.Runtime.SourceID
		if !allowed[id] {
			continue
		}
		v, ok := vis[id]
		if !ok || !v.Visible || v.Revision != c.Runtime.SourceRevision {
			continue
		}
		out = append(out, c)
	}
	return out, nil
}

// checkFailed logs a failed step-6 check by kind and returns the error the
// search fails with. Only a context error stays in the chain: the checker's
// or the source's own error text may name a subject or a record.
func (s *Searcher) checkFailed(ctx, stageCtx context.Context, tenantID, corpus, stage string) error {
	kind, cause := "error", error(nil)
	switch {
	case ctx.Err() != nil:
		kind, cause = "canceled", ctx.Err()
	case stageCtx.Err() != nil:
		kind, cause = "timeout", stageCtx.Err()
	}
	s.d.Logger.Warn("retrieval: search visibility check failed",
		slog.String("corpus", corpus), slog.String("tenant", tenantID),
		slog.String("stage", stage), slog.String("kind", kind))
	if cause != nil {
		return fmt.Errorf("%w: %s: %w", iface.ErrRetrievalVisibilityCheck, stage, cause)
	}
	return fmt.Errorf("%w: %s failed", iface.ErrRetrievalVisibilityCheck, stage)
}

// gate is step 1, in this order: internal tenant, caller identity, corpus,
// request shape, permission, filter, owning module started.
func (s *Searcher) gate(ctx context.Context, req iface.RetrievalSearchRequest) (gated, error) {
	var g gated
	if err := tenantrepo.RequireInternalTenant(ctx); err != nil {
		return g, fmt.Errorf("%w: %w", iface.ErrRetrievalForbidden, err)
	}
	g.userUUID, _ = ctxauth.GetUserUUID(ctx)
	g.tenantID, _ = ctxauth.GetTenantID(ctx)
	if g.userUUID == "" || g.tenantID == "" {
		return g, iface.ErrRetrievalForbidden
	}

	if s.d.Corpora == nil {
		return g, unavailable("corpus registry is not configured")
	}
	spec, ok := s.d.Corpora.Corpus(req.Corpus)
	if req.Corpus == "" || !ok {
		return g, iface.ErrRetrievalUnknownCorpus
	}
	g.spec = spec

	plan, err := planSearch(req, spec)
	if err != nil {
		return g, err
	}
	g.plan = plan

	if s.d.Authz == nil {
		return g, unavailable("permission checker is not configured")
	}
	allowed, err := s.d.Authz.HasPermission(ctx, g.userUUID, g.tenantID, spec.Permission)
	if err != nil {
		// Never reported as forbidden: the caller may well hold the permission.
		return g, fmt.Errorf("%w: permission check: %w", iface.ErrRetrievalUnavailable, err)
	}
	if !allowed {
		return g, iface.ErrRetrievalForbidden
	}

	if g.filter, err = filter.Compile(req.Filter, spec.Filters); err != nil {
		return g, err
	}

	if s.d.Modules == nil || !s.d.Modules.IsStarted(spec.Owner) {
		return g, iface.ErrRetrievalCorpusUnavailable
	}
	return g, nil
}

// pin is step 2: the corpus state is read once and its collection and
// embedding profile hold for the whole request. ErrNoActiveGeneration is
// returned as is.
func (s *Searcher) pin(ctx context.Context, corpus string) (PinnedGeneration, error) {
	if s.d.State == nil {
		return PinnedGeneration{}, unavailable("corpus state is not configured")
	}
	pinned, err := s.d.State.ActiveGeneration(ctx, corpus)
	if errors.Is(err, ErrNoActiveGeneration) {
		return PinnedGeneration{}, ErrNoActiveGeneration
	}
	if err != nil {
		return PinnedGeneration{}, fmt.Errorf("%w: corpus state: %w", iface.ErrRetrievalUnavailable, err)
	}
	if pinned.Collection == "" {
		return PinnedGeneration{}, unavailable("the active generation has no collection")
	}
	return pinned, nil
}

// principals is step 3. A resolution error fails the search (plan decision
// 6); its text may name a contributor's principals, so only its kind is logged.
func (s *Searcher) principals(ctx context.Context, g gated) (iface.RetrievalPrincipalContext, error) {
	if s.d.Principals == nil {
		return iface.RetrievalPrincipalContext{}, unavailable("principal resolver is not configured")
	}
	pc, err := s.d.Principals.Principals(ctx)
	if err != nil {
		s.d.Logger.Warn("retrieval: search principal resolution failed",
			slog.String("corpus", g.spec.Name), slog.String("tenant", g.tenantID),
			slog.String("kind", principalErrorKind(err)))
		if errors.Is(err, iface.ErrRetrievalForbidden) {
			return iface.RetrievalPrincipalContext{}, fmt.Errorf("retrieval: principals: %w", err)
		}
		return iface.RetrievalPrincipalContext{}, fmt.Errorf("%w: principal resolution: %w", iface.ErrRetrievalUnavailable, err)
	}
	// Second guard: the engine query is scoped by these values, so they must
	// be the caller's and must be usable as a filter.
	if pc.TenantID != g.tenantID || pc.UserUUID != g.userUUID || len(pc.Principals) == 0 {
		return iface.RetrievalPrincipalContext{}, unavailable("principal context does not match the caller")
	}
	for _, p := range pc.Principals {
		if p == "" {
			return iface.RetrievalPrincipalContext{}, unavailable("principal context has an empty principal")
		}
	}
	return pc, nil
}

func principalErrorKind(err error) string {
	switch {
	case errors.Is(err, iface.ErrRetrievalForbidden):
		return "forbidden"
	case errors.Is(err, ErrPrincipalCapExceeded):
		return "cap_exceeded"
	case errors.Is(err, ErrContributorPrincipal):
		return "invalid_contributor_principal"
	case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
		return "canceled"
	}
	return "error"
}

// denseVector is step 4's dense arm. It returns ok=false, after a WARN, when
// the arm cannot run; the search then answers keyword-only and degraded. The
// only error is the caller's own cancellation.
func (s *Searcher) denseVector(ctx context.Context, g gated, pinned PinnedGeneration, query string) ([]float32, bool, error) {
	endpoint := g.spec.Ingestion.EmbedEndpoint
	skip := func(reason string) ([]float32, bool, error) {
		s.d.Logger.Warn("retrieval: search dense arm unavailable; answering keyword-only",
			slog.String("corpus", g.spec.Name), slog.String("tenant", g.tenantID),
			slog.String("endpoint", endpoint), slog.String("reason", reason))
		return nil, false, nil
	}
	if s.d.Profiles == nil || s.d.Embedder == nil {
		return skip("inference_not_configured")
	}
	p, err := s.d.Profiles.InferenceProfile(ctx, endpoint)
	if err != nil {
		if ctx.Err() != nil {
			return nil, false, ctx.Err()
		}
		return skip(inferenceErrorReason(err))
	}
	switch {
	case p == nil:
		return skip("profile_unavailable")
	case p.Kind != iface.InferenceKindEmbed:
		return skip("endpoint_kind")
	case pinned.EmbedProfileID == "" || p.ID != pinned.EmbedProfileID:
		// The endpoint now computes vectors the pinned generation was not
		// built with (Review Focus 3): never mix them.
		return skip("profile_mismatch")
	case !localityAllows(g.spec.Locality, p.Boundary):
		// The query text never reaches an endpoint the corpus forbids
		// (plan decision 4).
		return skip("locality")
	}
	res, err := s.d.Embedder.EmbedWithProfile(ctx, iface.PinnedEmbedRequest{
		Caller:     searchCaller,
		OnBehalfOf: g.tenantID,
		Profile:    pinned.EmbedProfileID,
		Kind:       iface.EmbedInputQuery,
		Inputs:     []string{query},
	})
	if err != nil {
		if ctx.Err() != nil {
			return nil, false, ctx.Err()
		}
		return skip(inferenceErrorReason(err))
	}
	if res == nil || len(res.Vectors) != 1 || len(res.Vectors[0]) == 0 ||
		(p.Dimensions > 0 && len(res.Vectors[0]) != p.Dimensions) {
		return skip("bad_vector")
	}
	return res.Vectors[0], true, nil
}

// inferenceErrorReason classifies an inference error for the log without
// its text.
func inferenceErrorReason(err error) string {
	switch {
	case errors.Is(err, iface.ErrLLMProfileMismatch):
		return "profile_mismatch"
	case errors.Is(err, iface.ErrLLMNotConfigured):
		return "endpoint_not_configured"
	case errors.Is(err, iface.ErrLLMProviderUnavailable):
		return "provider_unavailable"
	}
	return "inference_error"
}

// localityAllows reports whether a corpus of locality loc may send text to an
// endpoint with boundary b. Anything but hosted_allowed is local_only.
func localityAllows(loc iface.RetrievalLocality, b iface.InferenceBoundary) bool {
	return loc == iface.RetrievalHostedAllowed || b == iface.InferenceBoundaryLocal
}

// effectiveLocality is the locality a hit reports: "" is local_only.
func effectiveLocality(loc iface.RetrievalLocality) iface.RetrievalLocality {
	if loc == "" {
		return iface.RetrievalLocalOnly
	}
	return loc
}

// query is one engine call (step 5). Every engine error is reported as
// unavailable; the cause stays in the chain (a cancellation remains
// errors.Is context.Canceled) and only its kind is logged.
func (s *Searcher) query(ctx context.Context, g gated, collection string, q engine.Query) ([]engine.Candidate, error) {
	cands, err := s.d.Engine.Query(ctx, collection, q)
	if err != nil {
		s.d.Logger.Warn("retrieval: search engine query failed",
			slog.String("corpus", g.spec.Name), slog.String("tenant", g.tenantID),
			slog.String("kind", engineErrorKind(err)))
		return nil, fmt.Errorf("%w: engine query: %w", iface.ErrRetrievalUnavailable, err)
	}
	return cands, nil
}

func engineErrorKind(err error) string {
	switch {
	case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
		return "canceled"
	case errors.Is(err, engine.ErrUnavailable):
		return "unavailable"
	case errors.Is(err, engine.ErrNotFound):
		return "collection_not_found"
	}
	return "refused"
}

// toHits is step 8 over the candidates to return, in order. Hits is never nil.
func (s *Searcher) toHits(spec iface.RetrievalCorpusSpec, query string, cands []engine.Candidate) []iface.RetrievalHit {
	loc := effectiveLocality(spec.Locality)
	hits := make([]iface.RetrievalHit, 0, len(cands))
	for i, c := range cands {
		rt := c.Runtime
		hits = append(hits, iface.RetrievalHit{
			SourceID: rt.SourceID,
			ChunkID:  rt.ChunkID,
			Rank:     i + 1,
			Score:    c.Score,
			Snippet:  Snippet(c.Text, query, SnippetMaxRunes),
			Provenance: iface.RetrievalProvenance{
				SourceID:  rt.SourceID,
				ChunkID:   rt.ChunkID,
				Ordinal:   rt.Ordinal,
				Page:      rt.Page,
				CharStart: rt.CharStart,
				CharEnd:   rt.CharEnd,
			},
			Locality: loc,
		})
	}
	return hits
}

// rerank is step 7 over the checked survivors in fused order. It sends the
// text of the first n = min(k, len(survivors)) to the corpus's rerank
// endpoint and returns survivors with that head reordered, each head
// candidate carrying the reranker's score (one score scale, plan decision
// 7), the rest following in fused order. ok=false, after a WARN with the
// reason, means the fused order and scores stand (plan decision 1); the
// caller turns that into Degraded, or into ctx.Err() for a cancelled caller.
// survivors is never modified. Neither the query, a document nor a provider
// error text is logged or returned.
func (s *Searcher) rerank(ctx context.Context, tenantID string, spec iface.RetrievalCorpusSpec, query string,
	survivors []engine.Candidate, k int) (ordered []engine.Candidate, scores []float32, ok bool) {
	n := min(k, len(survivors))
	if n < 1 {
		return survivors, nil, false
	}
	endpoint := spec.Rerank.Endpoint
	skip := func(reason string) ([]engine.Candidate, []float32, bool) {
		s.d.Logger.Warn("retrieval: search rerank unavailable; keeping the fused order",
			slog.String("corpus", spec.Name), slog.String("tenant", tenantID),
			slog.String("endpoint", endpoint), slog.String("reason", reason))
		return survivors, nil, false
	}
	if s.d.Profiles == nil || s.d.Reranker == nil {
		return skip("inference_not_configured")
	}
	p, err := s.d.Profiles.InferenceProfile(ctx, endpoint)
	if err != nil {
		return skip(inferenceErrorReason(err))
	}
	switch {
	case p == nil:
		return skip("profile_unavailable")
	case p.Kind != iface.InferenceKindRerank:
		return skip("endpoint_kind")
	case !localityAllows(spec.Locality, p.Boundary):
		return skip("locality")
	}
	docs := make([]string, n)
	for i := range docs {
		docs[i] = survivors[i].Text
	}
	res, err := s.d.Reranker.Rerank(ctx, iface.RerankRequest{
		Caller:     searchCaller,
		OnBehalfOf: tenantID,
		Profile:    p.ID,
		Query:      query,
		Documents:  docs,
		TopN:       n,
	})
	if err != nil {
		return skip(inferenceErrorReason(err))
	}
	if res == nil || len(res.Results) != n {
		return skip("bad_result")
	}
	seen := make([]bool, n)
	for _, r := range res.Results {
		if r.Index < 0 || r.Index >= n || seen[r.Index] {
			return skip("bad_result")
		}
		seen[r.Index] = true
	}
	ordered = make([]engine.Candidate, 0, len(survivors))
	scores = make([]float32, 0, n)
	for _, r := range res.Results {
		c := survivors[r.Index]
		c.Score = r.Score
		ordered = append(ordered, c)
		scores = append(scores, r.Score)
	}
	ordered = append(ordered, survivors[n:]...)
	return ordered, scores, true
}

// FilterSchema returns a copy of the corpus's declared filter fields.
func (s *Searcher) FilterSchema(corpus string) ([]iface.RetrievalFilterField, error) {
	if s.d.Corpora == nil {
		return nil, iface.ErrRetrievalUnknownCorpus
	}
	spec, ok := s.d.Corpora.Corpus(corpus)
	if corpus == "" || !ok {
		return nil, iface.ErrRetrievalUnknownCorpus
	}
	return append([]iface.RetrievalFilterField(nil), spec.Filters...), nil
}
