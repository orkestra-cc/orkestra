package services

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"reflect"
	"slices"
	"time"

	"github.com/google/uuid"
	"github.com/orkestra/backend/internal/core/retrieval/engine"
	"github.com/orkestra/backend/internal/core/retrieval/filter"
	"github.com/orkestra/backend/internal/core/retrieval/models"
	"github.com/orkestra/backend/internal/core/retrieval/repository"
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

// SearchLogRetractor deletes one search-log row of the ctx tenant; satisfied by *repository.SearchLog.
type SearchLogRetractor interface {
	DeleteRow(ctx context.Context, uuid string) error
}

var _ SearchLogRetractor = (*repository.SearchLog)(nil)

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
	// retractor is d.Log when it can also delete a row; nil otherwise.
	retractor SearchLogRetractor
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
	s := &Searcher{d: d}
	if d.Log != nil {
		s.retractor, _ = d.Log.(SearchLogRetractor)
	}
	return s
}

// gated is what the gate established about a request.
type gated struct {
	tenantID string
	userUUID string
	spec     iface.RetrievalCorpusSpec
	plan     searchPlan
	filter   *engine.Filter
	// filterJSON is the request's filter as stored in the search log
	// (models.SearchLog.FilterJSON); "" without a filter.
	filterJSON string
}

func unavailable(what string) error {
	return fmt.Errorf("%w: %s", iface.ErrRetrievalUnavailable, what)
}

// searchLogErrorTimeout bounds the best-effort row of a failed search, which
// is written detached from the caller's cancellation (precedent: the worker's
// bookkeepingTimeout).
const searchLogErrorTimeout = 5 * time.Second

// Search-log error kinds (models.SearchLog.ErrorKind).
const (
	errKindNotConfigured   = "not_configured"
	errKindUnavailable     = "unavailable"
	errKindPrincipals      = "principals"
	errKindVisibilityCheck = "visibility_check"
	errKindCanceled        = "canceled"
)

// searchRecord collects, while a search that passed the gate runs, what its
// search-log row needs.
type searchRecord struct {
	now        func() time.Time
	start      time.Time
	timings    models.SearchTimings
	principals []string
	returned   []engine.Candidate // the candidates behind the returned hits
	kind       string             // set with the error
}

// since is the milliseconds elapsed from t0 by the Searcher's clock.
func (r *searchRecord) since(t0 time.Time) int64 { return r.now().Sub(t0).Milliseconds() }

// fail records the error kind of a failed stage and returns err.
func (r *searchRecord) fail(kind string, err error) error {
	r.kind = kind
	return err
}

// Search runs the pipeline in ADR order. Every search that passes the gate
// leaves one search-log row (ADR D12, plan decision 2): a result is returned
// only after its row is written, and a failed write fails the search closed;
// a failed search is logged best-effort. Nothing in this method logs the
// query, a snippet, chunk text, principals or subjects.
func (s *Searcher) Search(ctx context.Context, req iface.RetrievalSearchRequest) (*iface.RetrievalSearchResult, error) {
	rec := &searchRecord{now: s.d.Now, start: s.d.Now()}
	// Step 1: gate. A refusal writes no row.
	g, err := s.gate(ctx, req)
	if err != nil {
		return nil, err
	}
	if s.d.Log == nil {
		// A result nobody can answer for is never served (plan decision 2).
		return nil, fmt.Errorf("%w: %w", iface.ErrRetrievalUnavailable, ErrSearchLogWrite)
	}

	res, err := s.run(ctx, req, g, rec)
	row := s.logRow(req, g, rec, res, err)
	if err != nil {
		s.logFailed(ctx, g, row)
		return nil, err
	}
	if werr := s.d.Log.Insert(ctx, row); werr != nil {
		s.d.Logger.Warn("retrieval: search log write failed; the search fails closed",
			slog.String("corpus", g.spec.Name), slog.String("tenant", g.tenantID))
		return nil, fmt.Errorf("%w: %w", iface.ErrRetrievalUnavailable, ErrSearchLogWrite)
	}
	s.retract(ctx, g, row)
	return res, nil
}

// logFailed writes the row of a search that failed after the gate. It is
// best-effort (the search returns no data): it runs detached from the
// caller's cancellation, so a cancelled caller still leaves its row, under
// searchLogErrorTimeout, and a failure only WARNs.
func (s *Searcher) logFailed(ctx context.Context, g gated, row *models.SearchLog) {
	lctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), searchLogErrorTimeout)
	defer cancel()
	if err := s.d.Log.Insert(lctx, row); err != nil {
		s.d.Logger.Warn("retrieval: search log write failed for a failed search",
			slog.String("corpus", g.spec.Name), slog.String("tenant", g.tenantID),
			slog.String("kind", row.ErrorKind))
		return
	}
	s.retract(ctx, g, row)
}

// retract re-reads the tombstones after row was written and deletes the row
// when an erasure covers the searcher or any subject it names. Ordering: an
// eraser writes the tombstone and then deletes rows (the producer, and
// finalizer stage 0); the searcher writes its row and then reads tombstones.
// Either this re-check sees the tombstone, or the eraser's delete sees the
// row, so a row written after the erasure cannot survive. Hits already
// returned passed the step 6 veto before the erasure was recorded and are
// not recalled. A failed re-check keeps the row for finalizer stage 0.
func (s *Searcher) retract(ctx context.Context, g gated, row *models.SearchLog) {
	if s.retractor == nil || s.d.Tombstones == nil {
		return
	}
	rctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), searchLogErrorTimeout)
	defer cancel()
	subjects := slices.Clone(row.Subjects)
	if row.UserUUID != "" {
		subjects = append(subjects, "user:"+row.UserUUID)
	}
	slices.Sort(subjects)
	subjects = slices.Compact(subjects)
	covered, err := s.d.Tombstones.Covered(rctx, g.tenantID, g.spec.Name, []Probe{{SourceID: row.UUID, Subjects: subjects}})
	if err != nil {
		s.d.Logger.Warn("retrieval: search log erasure re-check failed; the row is kept for the erasure finalizer",
			slog.String("corpus", g.spec.Name), slog.String("tenant", g.tenantID))
		return
	}
	if !covered[row.UUID] {
		return
	}
	if err := s.retractor.DeleteRow(rctx, row.UUID); err != nil {
		s.d.Logger.Warn("retrieval: search log retraction failed",
			slog.String("corpus", g.spec.Name), slog.String("tenant", g.tenantID))
	}
}

// logRow builds the search-log row of a search that passed the gate. Source
// IDs and subjects come from the returned hits only.
func (s *Searcher) logRow(req iface.RetrievalSearchRequest, g gated, rec *searchRecord,
	res *iface.RetrievalSearchResult, err error) *models.SearchLog {
	row := &models.SearchLog{
		UUID:       uuid.NewString(),
		UserUUID:   g.userUUID,
		Corpus:     g.spec.Name,
		Query:      req.Query,
		FilterJSON: g.filterJSON,
		Mode:       string(g.plan.Mode),
		Principals: slices.Clone(rec.principals),
		SourceIDs:  []string{},
		Subjects:   []string{},
	}
	if err != nil {
		row.Outcome, row.ErrorKind = models.SearchOutcomeError, errorKind(rec.kind, err)
	} else {
		row.HitCount, row.Degraded = len(res.Hits), res.Degraded
		row.Outcome = models.SearchOutcomeOK
		if res.Degraded {
			row.Outcome = models.SearchOutcomeDegraded
		}
		for _, c := range rec.returned {
			if !slices.Contains(row.SourceIDs, c.Runtime.SourceID) {
				row.SourceIDs = append(row.SourceIDs, c.Runtime.SourceID)
			}
			for _, sub := range c.Runtime.Subjects {
				if !slices.Contains(row.Subjects, sub) {
					row.Subjects = append(row.Subjects, sub)
				}
			}
		}
		slices.Sort(row.Subjects)
	}
	now := s.d.Now()
	rec.timings.TotalMs = now.Sub(rec.start).Milliseconds()
	row.Timings = rec.timings
	row.CreatedAt = now.UTC()
	return row
}

// errorKind is the row's ErrorKind: the failed stage's kind, except that an
// unavailable stage failing on a context error (the caller's cancellation or
// deadline, as returned by embed or the engine) is "canceled".
func errorKind(kind string, err error) string {
	if kind == "" {
		kind = errKindUnavailable
	}
	if kind == errKindUnavailable && (errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded)) {
		return errKindCanceled
	}
	return kind
}

// run is steps 2 to 8 of a search that passed the gate; rec collects what
// the search-log row needs.
func (s *Searcher) run(ctx context.Context, req iface.RetrievalSearchRequest, g gated, rec *searchRecord) (*iface.RetrievalSearchResult, error) {
	if s.d.Tombstones == nil || g.spec.Source == nil {
		// Step 6 could not run, so no candidate may be fetched at all.
		return nil, rec.fail(errKindVisibilityCheck,
			fmt.Errorf("%w: the erasure or source check is not configured", iface.ErrRetrievalVisibilityCheck))
	}

	// Step 2: resolve and pin.
	if s.d.Engine == nil {
		return nil, rec.fail(errKindNotConfigured, iface.ErrRetrievalNotConfigured)
	}
	pinned, err := s.pin(ctx, g.spec.Name)
	if errors.Is(err, ErrNoActiveGeneration) {
		rec.timings.ResolveMs = rec.since(rec.start)
		return &iface.RetrievalSearchResult{Hits: []iface.RetrievalHit{}, Degraded: true}, nil
	}
	if err != nil {
		return nil, rec.fail(errKindUnavailable, err)
	}

	// Step 3: principals.
	pc, err := s.principals(ctx, g)
	if err != nil {
		return nil, rec.fail(errKindPrincipals, err)
	}
	rec.principals = pc.Principals
	rec.timings.ResolveMs = rec.since(rec.start)

	// Step 4: arms.
	var (
		vec      []float32
		denseOK  bool
		degraded bool
	)
	if g.plan.Mode != iface.RetrievalModeKeyword {
		t0 := s.d.Now()
		vec, denseOK, err = s.denseVector(ctx, g, pinned, req.Query)
		rec.timings.EmbedMs = rec.since(t0)
		if err != nil {
			return nil, rec.fail(errKindUnavailable, err)
		}
		degraded = !denseOK
	}
	keyword := req.Query
	if g.plan.Mode == iface.RetrievalModeDense && denseOK {
		keyword = ""
	}

	// Steps 5 and 6: fusion, then erasure veto and source check, with one
	// refill when the drops shorten the page.
	cands, short, err := s.fetchChecked(ctx, g, pinned.Collection, pc, rec, engine.Query{
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
		return nil, err // fetchChecked recorded the kind
	}
	// Never fewer than the permitted results without a flag (D5).
	degraded = degraded || short

	// Step 7: optional rerank of the checked candidates.
	if g.plan.RerankK > 0 && len(cands) > 0 {
		t0 := s.d.Now()
		ordered, _, ok := s.rerank(ctx, g.tenantID, g.spec, req.Query, cands, g.plan.RerankK)
		rec.timings.RerankMs = rec.since(t0)
		if ok {
			cands = ordered
		} else {
			if err := ctx.Err(); err != nil {
				return nil, rec.fail(errKindUnavailable, err)
			}
			degraded = true
		}
	}

	// Step 8: result.
	cands = cands[:min(len(cands), g.plan.Limit)]
	rec.returned = cands
	return &iface.RetrievalSearchResult{Hits: s.toHits(g.spec, req.Query, cands), Degraded: degraded}, nil
}

// fetchChecked runs the engine query q (step 5) and step 6 over its answer.
// When the checks leave fewer than plan.Limit survivors, the engine returned
// a full page and plan.Fusion.Candidates allows more, it queries once more
// with Limit = plan.Fusion.Candidates and checks only the points the first
// call did not return (plan decision 3). There is never a third call. short
// reports a page still under plan.Limit whose last engine call came back
// full, or from which the refill dropped a changed source (below), so more
// permitted results may exist.
//
// The erasure veto runs once per engine call, with one probe per source
// built from that call's chunks only. That is complete because of two
// invariants: every chunk of one item revision carries the item's Subjects
// (ingest.go buildPoints stamps them on each point), and the revision check
// against Visible keeps a single revision per source. So a call's chunks of
// a source probe exactly the subjects all its chunks share. Across the
// refill the answer can still change (a tombstone landing between the two
// calls): a source seen in both calls is re-checked together with its
// first-call survivors, so a source the refill finds covered (or no longer
// visible) loses every chunk, not only the new ones. A point ID does not
// carry the revision (engine.PointID hashes tenant, corpus, source and
// ordinal), so the worker can replace a point between the calls under the
// same ID: when the refill's copy of a first-call point differs from it in
// any runtime field (revision, VisibleTo, Subjects, provenance), its source
// is dropped whole, first-call survivors and new refill points alike, and is
// not checked again. So every returned candidate is the exact copy its last
// check of step 6 saw. This adds no source to either call, and the survivors
// keep their rank positions.
func (s *Searcher) fetchChecked(ctx context.Context, g gated, collection string, pc iface.RetrievalPrincipalContext,
	rec *searchRecord, q engine.Query) (survivors []engine.Candidate, short bool, err error) {
	query := func(q engine.Query) ([]engine.Candidate, error) {
		t0 := s.d.Now()
		defer func() { rec.timings.EngineMs += rec.since(t0) }()
		cands, err := s.query(ctx, g, collection, q)
		if err != nil {
			return nil, rec.fail(errKindUnavailable, err)
		}
		return cands, nil
	}
	check := func(cands []engine.Candidate) ([]engine.Candidate, error) {
		t0 := s.d.Now()
		defer func() { rec.timings.CheckMs += rec.since(t0) }()
		out, err := s.checkCandidates(ctx, g.tenantID, g.spec.Name, g.spec.Source, pc, cands)
		if err != nil {
			return nil, rec.fail(errKindVisibilityCheck, err)
		}
		return out, nil
	}

	first, err := query(q)
	if err != nil {
		return nil, false, err
	}
	survivors, err = check(first)
	if err != nil {
		return nil, false, err
	}
	last, lastLimit := first, q.Limit
	// A point ID does not carry the revision: a refill copy that differs
	// from the first call's drops its whole source, unchecked.
	changed := map[string]bool{}
	if len(survivors) < g.plan.Limit && len(first) >= q.Limit && q.Limit < g.plan.Fusion.Candidates {
		q.Limit = g.plan.Fusion.Candidates
		second, err := query(q)
		if err != nil {
			return nil, false, err
		}
		byID := make(map[string]engine.Candidate, len(first))
		for _, c := range first {
			byID[c.PointID] = c
		}
		for _, c := range second {
			if prev, ok := byID[c.PointID]; ok && !sameRuntime(prev.Runtime, c.Runtime) {
				changed[prev.Runtime.SourceID] = true
				changed[c.Runtime.SourceID] = true
			}
		}
		seen := make(map[string]bool, len(first)+len(second))
		for _, c := range first {
			seen[c.PointID] = true
		}
		var fresh []engine.Candidate
		for _, c := range second {
			if !seen[c.PointID] && !changed[c.Runtime.SourceID] {
				seen[c.PointID] = true
				fresh = append(fresh, c)
			}
		}
		freshSources := make(map[string]bool, len(fresh))
		for _, c := range fresh {
			freshSources[c.Runtime.SourceID] = true
		}
		var recheck []engine.Candidate
		rechecked := map[string]bool{}
		for _, c := range survivors {
			if freshSources[c.Runtime.SourceID] {
				recheck = append(recheck, c)
				rechecked[c.PointID] = true
			}
		}
		more, err := check(append(recheck, fresh...))
		if err != nil {
			return nil, false, err
		}
		passed := make(map[string]bool, len(more))
		for _, c := range more {
			passed[c.PointID] = true
		}
		kept := make([]engine.Candidate, 0, len(survivors)+len(more))
		for _, c := range survivors {
			if changed[c.Runtime.SourceID] {
				continue
			}
			if !rechecked[c.PointID] || passed[c.PointID] {
				kept = append(kept, c)
			}
		}
		for _, c := range more {
			if !rechecked[c.PointID] {
				kept = append(kept, c)
			}
		}
		survivors = kept
		last, lastLimit = second, q.Limit
	}
	// A dropped changed source was never checked and may have been
	// permitted: a page under Limit then says so even after a short refill.
	return survivors, len(survivors) < g.plan.Limit && (len(last) >= lastLimit || len(changed) > 0), nil
}

// sameRuntime reports that two engine copies of one point carry the same
// runtime payload: revision, access (VisibleTo, Subjects) and provenance.
func sameRuntime(a, b engine.Runtime) bool {
	if !slices.Equal(a.VisibleTo, b.VisibleTo) || !slices.Equal(a.Subjects, b.Subjects) {
		return false
	}
	a.VisibleTo, a.Subjects, b.VisibleTo, b.Subjects = nil, nil, nil, nil
	return reflect.DeepEqual(a, b)
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
	if req.Filter != nil {
		// The search log stores the filter as canonical JSON. A validated
		// filter without a JSON form (a json.Number the encoder rejects, a
		// time.Time outside years 0-9999) could not be audited: refuse it.
		b, err := json.Marshal(req.Filter)
		if err != nil {
			return g, fmt.Errorf("%w: the filter has no JSON form", iface.ErrRetrievalInvalidFilter)
		}
		g.filterJSON = string(b)
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
// 6); its text may name a contributor's principals, so only its kind is
// logged and principalError maps what is returned.
func (s *Searcher) principals(ctx context.Context, g gated) (iface.RetrievalPrincipalContext, error) {
	if s.d.Principals == nil {
		return iface.RetrievalPrincipalContext{}, unavailable("principal resolver is not configured")
	}
	pc, err := s.d.Principals.Principals(ctx)
	if err != nil {
		s.d.Logger.Warn("retrieval: search principal resolution failed",
			slog.String("corpus", g.spec.Name), slog.String("tenant", g.tenantID),
			slog.String("kind", principalErrorKind(err)))
		return iface.RetrievalPrincipalContext{}, principalError(ctx, err)
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
		(p.Dimensions > 0 && len(res.Vectors[0]) != p.Dimensions) || !finiteVector(res.Vectors[0]) {
		// A NaN or Inf would fail the engine query and with it the whole
		// search; like any unusable vector it only drops the dense arm.
		return skip("bad_vector")
	}
	return res.Vectors[0], true, nil
}

// finiteVector reports whether every value of v is a finite number.
func finiteVector(v []float32) bool {
	for _, x := range v {
		if f := float64(x); math.IsNaN(f) || math.IsInf(f, 0) {
			return false
		}
	}
	return true
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
		if r.Index < 0 || r.Index >= n || seen[r.Index] ||
			math.IsNaN(float64(r.Score)) || math.IsInf(float64(r.Score), 0) {
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
