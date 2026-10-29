package services

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"sort"
	"time"
	"unicode/utf8"

	"github.com/orkestra/backend/internal/core/retrieval/engine"
	"github.com/orkestra/backend/internal/core/retrieval/models"
	"github.com/orkestra/backend/internal/core/retrieval/repository"
	"github.com/orkestra/backend/internal/shared/errcode"
	"github.com/orkestra/backend/pkg/sdk/iface"
	"github.com/orkestra/backend/pkg/sdk/module"
	"github.com/orkestra/backend/pkg/sdk/tenantrepo"
)

// ErrCorpusRegistered reports that an admin action needs the corpus to be
// unregistered, and it is still registered (or was re-registered meanwhile).
var ErrCorpusRegistered = errors.New("retrieval: corpus is still registered")

// ErrCorpusNotFound reports that the corpus has no retrieval state to act on.
var ErrCorpusNotFound = errors.New("retrieval: no state for this corpus")

// Bounds on the calls a report makes to other components, so a hung sidecar
// cannot hang the admin page.
const (
	adminHealthTimeout  = 2 * time.Second
	adminProfileTimeout = 5 * time.Second
	adminCountTimeout   = 5 * time.Second
)

const auditResourceCorpus = "retrieval_corpus"

// AdminStoreAPI is the persistence AdminService needs: the new admin queries
// plus the existing corpus-state and tenant-progress reads.
type AdminStoreAPI interface {
	OutboxBacklogAll(ctx context.Context) (map[string]models.RetrievalBacklog, error)
	ProgressCounts(ctx context.Context) (map[models.ProgressKey]models.ProgressCounts, error)
	ErasureCounts(ctx context.Context) (models.RetrievalErasureCounts, error)
	DeleteCorpusRows(ctx context.Context, corpus string) (outbox, progress int64, err error)
	DeleteCorpusState(ctx context.Context, corpus string, version int64) error
	OutboxBacklogTenant(ctx context.Context) (map[string]models.RetrievalBacklog, error)
	TenantErasuresPending(ctx context.Context) (int64, error)
	SearchLog(ctx context.Context, q models.SearchLogQuery) ([]models.SearchLog, int64, error)
	CorpusState(ctx context.Context, name string) (*models.CorpusState, error) // absent: nil, nil
	CorpusStateList(ctx context.Context) ([]models.CorpusState, error)
	TenantBuild(ctx context.Context, corpus string, generation int) (*models.TenantProgress, error) // absent: nil, nil
}

// AdminDeps are the collaborators of AdminService.
type AdminDeps struct {
	Runtime  AdminRuntime
	Store    AdminStoreAPI
	Profiles iface.LLMInferenceProfiles // nil: every inference row reports not_configured
	Modules  module.ModuleStatusReader  // nil: every owner reports not running
	Audit    *AuditRelay                // nil: no audit
	Logger   *slog.Logger
}

// AdminService is the tier-1 admin view over the retrieval module: what is
// indexed, how, with which engine and endpoints, plus the diagnostic search
// and the orphan-corpus drop.
type AdminService struct {
	rt       AdminRuntime
	store    AdminStoreAPI
	profiles iface.LLMInferenceProfiles
	modules  module.ModuleStatusReader
	audit    *AuditRelay
	log      *slog.Logger
}

func NewAdminService(d AdminDeps) *AdminService {
	if d.Audit == nil {
		d.Audit = NewAuditRelay()
	}
	if d.Logger == nil {
		d.Logger = slog.Default()
	}
	return &AdminService{rt: d.Runtime, store: d.Store, profiles: d.Profiles, modules: d.Modules, audit: d.Audit, log: d.Logger}
}

func requireOperator(ctx context.Context) error {
	if err := tenantrepo.RequireInternalTenant(ctx); err != nil {
		return fmt.Errorf("%w: %w", iface.ErrRetrievalForbidden, err)
	}
	return nil
}

func (a *AdminService) ownerRunning(owner string) bool {
	return a.modules != nil && owner != "" && a.modules.IsStarted(owner)
}

// engineStatus reports the engine and returns it when it is usable. Neither
// a missing engine nor a failing health check is an error of the report.
func (a *AdminService) engineStatus(ctx context.Context) (engine.Engine, models.RetrievalEngineStatus) {
	eng, err := a.rt.Engine()
	if err != nil {
		if errors.Is(err, iface.ErrRetrievalNotConfigured) {
			return nil, models.RetrievalEngineStatus{Status: models.ComponentNotConfigured}
		}
		return nil, models.RetrievalEngineStatus{Status: models.ComponentUnavailable}
	}
	hctx, cancel := context.WithTimeout(ctx, adminHealthTimeout)
	defer cancel()
	if err := eng.Health(hctx); err != nil {
		a.log.Warn("retrieval admin: engine health check failed", "error", err)
		return nil, models.RetrievalEngineStatus{Status: models.ComponentUnavailable}
	}
	return eng, models.RetrievalEngineStatus{Status: models.ComponentOK}
}

type inferenceKey struct{ endpoint, kind string }

// inferenceStatuses reports every distinct (endpoint, role) the registered
// corpora use, sorted by endpoint, then kind.
func (a *AdminService) inferenceStatuses(ctx context.Context, specs []iface.RetrievalCorpusSpec) []models.RetrievalInferenceStatus {
	seen := map[inferenceKey]struct{}{}
	for _, sp := range specs {
		if e := sp.Ingestion.EmbedEndpoint; e != "" {
			seen[inferenceKey{e, string(iface.InferenceKindEmbed)}] = struct{}{}
		}
		if sp.Rerank.Enabled && sp.Rerank.Endpoint != "" {
			seen[inferenceKey{sp.Rerank.Endpoint, string(iface.InferenceKindRerank)}] = struct{}{}
		}
	}
	keys := make([]inferenceKey, 0, len(seen))
	for k := range seen {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool {
		if keys[i].endpoint != keys[j].endpoint {
			return keys[i].endpoint < keys[j].endpoint
		}
		return keys[i].kind < keys[j].kind
	})
	out := make([]models.RetrievalInferenceStatus, 0, len(keys))
	for _, k := range keys {
		out = append(out, a.inferenceStatus(ctx, k))
	}
	return out
}

func (a *AdminService) inferenceStatus(ctx context.Context, k inferenceKey) models.RetrievalInferenceStatus {
	row := models.RetrievalInferenceStatus{Endpoint: k.endpoint, Kind: k.kind, Status: models.ComponentNotConfigured}
	if a.profiles == nil {
		return row
	}
	pctx, cancel := context.WithTimeout(ctx, adminProfileTimeout)
	defer cancel()
	p, err := a.profiles.InferenceProfile(pctx, k.endpoint)
	switch {
	case err == nil && p != nil:
		row.Profile, row.Boundary = p.ID, p.Boundary
		row.Status = models.ComponentOK
		if string(p.Kind) != k.kind {
			row.Status = models.ComponentWrongKind
		}
	case err == nil:
		row.Status = models.ComponentNotConfigured
	case errors.Is(err, iface.ErrLLMNotConfigured):
		row.Status = models.ComponentNotConfigured
	case errors.Is(err, iface.ErrLLMProfileMismatch):
		row.Status = models.ComponentMismatch
	default:
		a.log.Warn("retrieval admin: inference profile lookup failed", "endpoint", k.endpoint, "error", err)
		row.Status = models.ComponentUnavailable
	}
	return row
}

// stateOf maps a stored state row to the report's state and reason. A missing
// row, or an active one with no generation yet, is uninitialized; a status
// this version does not know (a newer replica wrote it) is an error.
func stateOf(st *models.CorpusState) (state, reason string) {
	if st == nil {
		return "uninitialized", ""
	}
	switch st.Status {
	case models.CorpusStatusError:
		return "error", st.Reason
	case models.CorpusStatusProfileChanged:
		return "profile_changed", ""
	case models.CorpusStatusActive:
		if st.ActiveGeneration > 0 {
			return "active", ""
		}
		return "uninitialized", ""
	default:
		return "error", "unknown_state"
	}
}

// collectionFor is the collection of generation gen: the state row's own name
// when it records one for the active generation, else the derived name.
func (a *AdminService) collectionFor(st *models.CorpusState, corpus string, gen int) string {
	if st != nil && st.Collection != "" && gen == st.ActiveGeneration {
		return st.Collection
	}
	return engine.CollectionName(a.rt.CollectionPrefix(), corpus, gen)
}

// Corpora is the installation-wide report: engine, inference endpoints,
// registered corpora then orphans, and erasure counts. An engine or inference
// failure is reported in the result; a store failure fails the call.
func (a *AdminService) Corpora(ctx context.Context) (*models.RetrievalCorporaReport, error) {
	if err := requireOperator(ctx); err != nil {
		return nil, err
	}
	states, err := a.store.CorpusStateList(ctx)
	if err != nil {
		return nil, fmt.Errorf("retrieval admin: corpus states: %w", err)
	}
	backlog, err := a.store.OutboxBacklogAll(ctx)
	if err != nil {
		return nil, fmt.Errorf("retrieval admin: outbox backlog: %w", err)
	}
	progress, err := a.store.ProgressCounts(ctx)
	if err != nil {
		return nil, fmt.Errorf("retrieval admin: tenant progress: %w", err)
	}
	erasures, err := a.store.ErasureCounts(ctx)
	if err != nil {
		return nil, fmt.Errorf("retrieval admin: erasures: %w", err)
	}

	specs := a.rt.Corpora()
	_, engStatus := a.engineStatus(ctx)
	rep := &models.RetrievalCorporaReport{
		Engine:    engStatus,
		Inference: a.inferenceStatuses(ctx, specs),
		Corpora:   make([]models.RetrievalCorpusStatus, 0, len(specs)+len(states)),
		Erasures:  erasures,
	}

	byName := make(map[string]*models.CorpusState, len(states))
	for i := range states {
		byName[states[i].Corpus] = &states[i]
	}
	build := func(c models.RetrievalCorpusStatus, st *models.CorpusState) models.RetrievalCorpusStatus {
		c.State, c.Reason = stateOf(st)
		c.Outbox = backlog[c.Name]
		if st != nil {
			c.ActiveGeneration = st.ActiveGeneration
			c.Collection = st.Collection
			c.IngestionProfile = st.IngestionProfileHash
			c.EmbeddingProfile = st.EmbeddingProfileID
			if st.ActiveGeneration > 0 {
				n := progress[models.ProgressKey{Corpus: c.Name, Generation: st.ActiveGeneration}]
				c.TenantsDone, c.TenantsBuilding = n.Done, n.Building
			}
		}
		return c
	}

	registered := make(map[string]struct{}, len(specs))
	for _, sp := range specs {
		registered[sp.Name] = struct{}{}
		loc := sp.Locality
		if loc == "" {
			loc = iface.RetrievalLocalOnly
		}
		rep.Corpora = append(rep.Corpora, build(models.RetrievalCorpusStatus{
			Name: sp.Name, Owner: sp.Owner, OwnerRunning: a.ownerRunning(sp.Owner), Registered: true,
			Locality: loc, EmbedEndpoint: sp.Ingestion.EmbedEndpoint, RerankEnabled: sp.Rerank.Enabled,
		}, byName[sp.Name]))
	}
	orphans := make([]models.CorpusState, 0)
	for _, st := range states {
		if _, ok := registered[st.Corpus]; !ok {
			orphans = append(orphans, st)
		}
	}
	sort.Slice(orphans, func(i, j int) bool { return orphans[i].Corpus < orphans[j].Corpus })
	for i := range orphans {
		st := &orphans[i]
		rep.Corpora = append(rep.Corpora, build(models.RetrievalCorpusStatus{
			Name: st.Corpus, Owner: st.Owner, OwnerRunning: a.ownerRunning(st.Owner),
			Locality: iface.RetrievalLocality(st.Locality),
		}, st))
	}
	return rep, nil
}

// TenantIndexing is the calling tenant's view of the registered corpora.
func (a *AdminService) TenantIndexing(ctx context.Context) (*models.RetrievalTenantIndexingReport, error) {
	if err := requireOperator(ctx); err != nil {
		return nil, err
	}
	tenant, _ := tenantrepo.CurrentTenantID(ctx)
	states, err := a.store.CorpusStateList(ctx)
	if err != nil {
		return nil, fmt.Errorf("retrieval admin: corpus states: %w", err)
	}
	backlog, err := a.store.OutboxBacklogTenant(ctx)
	if err != nil {
		return nil, fmt.Errorf("retrieval admin: outbox backlog: %w", err)
	}
	pending, err := a.store.TenantErasuresPending(ctx)
	if err != nil {
		return nil, fmt.Errorf("retrieval admin: erasures: %w", err)
	}
	byName := make(map[string]*models.CorpusState, len(states))
	for i := range states {
		byName[states[i].Corpus] = &states[i]
	}

	eng, engStatus := a.engineStatus(ctx)
	rep := &models.RetrievalTenantIndexingReport{
		TenantID: tenant, Engine: engStatus, Corpora: []models.RetrievalTenantCorpus{}, ErasuresPending: pending,
	}
	for _, sp := range a.rt.Corpora() {
		st := byName[sp.Name]
		c := models.RetrievalTenantCorpus{
			Corpus: sp.Name, Owner: sp.Owner, OwnerRunning: a.ownerRunning(sp.Owner),
			InitialBuild: "pending", Outbox: backlog[sp.Name],
		}
		if st != nil && st.ActiveGeneration > 0 {
			prog, err := a.store.TenantBuild(ctx, sp.Name, st.ActiveGeneration)
			if err != nil {
				return nil, fmt.Errorf("retrieval admin: tenant build: %w", err)
			}
			if prog != nil {
				switch prog.Status {
				case models.ProgressDone:
					c.InitialBuild = "done"
				case models.ProgressBuilding:
					c.InitialBuild = "building"
				}
			}
			if eng != nil && rep.Engine.Status == models.ComponentOK {
				c.Points = a.countPoints(ctx, eng, a.collectionFor(st, sp.Name, st.ActiveGeneration), tenant, &rep.Engine)
			}
		}
		rep.Corpora = append(rep.Corpora, c)
	}
	return rep, nil
}

// countPoints counts the tenant's points. A missing collection leaves the
// count unknown without blaming the engine; any other failure marks it
// unavailable.
func (a *AdminService) countPoints(ctx context.Context, eng engine.Engine, collection, tenant string, st *models.RetrievalEngineStatus) *int64 {
	cctx, cancel := context.WithTimeout(ctx, adminCountTimeout)
	defer cancel()
	n, err := eng.Count(cctx, collection, engine.Selector{TenantID: tenant})
	switch {
	case err == nil:
		return &n
	case errors.Is(err, engine.ErrNotFound):
	default:
		a.log.Warn("retrieval admin: point count failed", "collection", collection, "error", err)
		st.Status = models.ComponentUnavailable
	}
	return nil
}

// Diagnose runs a search for the caller through the normal pipeline and
// reports the per-stage scores. The audit event carries the shape of the
// request, never the query text or the filter values.
func (a *AdminService) Diagnose(ctx context.Context, corpus string, in models.RetrievalDiagnosticsRequest) (*models.RetrievalDiagnosticsReport, error) {
	res, err := a.rt.Diagnose(ctx, iface.RetrievalSearchRequest{
		Corpus: corpus, Query: in.Query, Filter: in.Filter, Mode: in.Mode, Limit: in.Limit,
	})
	meta := map[string]any{
		"mode": string(effectiveMode(in.Mode)), "limit": effectiveLimit(in.Limit),
		"queryRunes": utf8.RuneCountInString(in.Query), "hits": 0, "degraded": false,
	}
	if err != nil {
		a.audit.EmitOutcome(ctx, AuditDiagnosticsRun, auditResourceCorpus, corpus, "failure", meta)
		return nil, err
	}
	meta["hits"], meta["degraded"] = len(res.Result.Hits), res.Result.Degraded
	a.audit.EmitOutcome(ctx, AuditDiagnosticsRun, auditResourceCorpus, corpus, "success", meta)

	rep := &models.RetrievalDiagnosticsReport{
		Corpus: corpus, Collection: res.Collection, Profile: res.Profile,
		Principals: slices.Clone(res.Principals), Degraded: res.Result.Degraded,
		Hits: make([]models.RetrievalDiagnosticHit, 0, len(res.Result.Hits)),
	}
	if rep.Principals == nil {
		rep.Principals = []string{}
	}
	for i, h := range res.Result.Hits {
		hit := models.RetrievalDiagnosticHit{
			SourceID: h.SourceID, ChunkID: h.ChunkID, Rank: h.Rank, Score: h.Score, Snippet: h.Snippet,
		}
		if i < len(res.Scores) {
			s := res.Scores[i]
			hit.Fused, hit.Dense, hit.Keyword, hit.Rerank = s.Fused, s.Dense, s.Keyword, s.Rerank
		}
		rep.Hits = append(rep.Hits, hit)
	}
	return rep, nil
}

func effectiveMode(m iface.RetrievalMode) iface.RetrievalMode {
	if m == "" {
		return iface.RetrievalModeHybrid
	}
	return m
}

func effectiveLimit(l int) int {
	if l == 0 {
		return DefaultSearchLimit
	}
	return l
}

// DropOrphanCorpus removes everything the installation keeps for a corpus no
// module registers any more: its engine collections, its outbox and
// tenant-progress rows and, last, its state row. Search-log rows and erasure
// tombstones are untouched. It refuses a registered corpus. Dropping a corpus
// whose state is already gone is ErrCorpusNotFound.
func (a *AdminService) DropOrphanCorpus(ctx context.Context, corpus string) error {
	if err := requireOperator(ctx); err != nil {
		return err
	}
	if _, ok := a.rt.Corpus(corpus); ok {
		return ErrCorpusRegistered
	}
	st, err := a.store.CorpusState(ctx, corpus)
	if err != nil {
		return fmt.Errorf("retrieval admin: corpus state: %w", err)
	}
	if st == nil {
		return ErrCorpusNotFound
	}

	collections := a.orphanCollections(st)
	meta := map[string]any{"collections": collections}
	err = a.dropOrphan(ctx, st, collections, meta)
	if err != nil {
		meta["reason"] = auditReason(err)
		a.audit.EmitOutcome(ctx, AuditCorpusDropped, auditResourceCorpus, corpus, "failure", meta)
		return err
	}
	a.audit.EmitOutcome(ctx, AuditCorpusDropped, auditResourceCorpus, corpus, "success", meta)
	return nil
}

// orphanCollections lists the collections the corpus may own: generations 1
// through the active one (a failed bootstrap can have created g1 before any
// state recorded it) and the state's own collection name.
func (a *AdminService) orphanCollections(st *models.CorpusState) []string {
	var names []string
	for g := 1; g <= max(1, st.ActiveGeneration); g++ {
		names = append(names, engine.CollectionName(a.rt.CollectionPrefix(), st.Corpus, g))
	}
	if st.Collection != "" && !slices.Contains(names, st.Collection) {
		names = append(names, st.Collection)
	}
	return names
}

func (a *AdminService) dropOrphan(ctx context.Context, st *models.CorpusState, collections []string, meta map[string]any) error {
	eng, err := a.rt.Engine()
	if err != nil {
		return err
	}
	for _, name := range collections {
		if err := eng.DropCollection(ctx, name); err != nil && !errors.Is(err, engine.ErrNotFound) {
			a.log.Warn("retrieval admin: dropping orphan collection failed", "corpus", st.Corpus, "collection", name, "error", err)
			return fmt.Errorf("%w", iface.ErrRetrievalUnavailable)
		}
	}
	outbox, progress, err := a.store.DeleteCorpusRows(ctx, st.Corpus)
	if err != nil {
		return fmt.Errorf("retrieval admin: delete corpus rows: %w", err)
	}
	meta["outboxRows"], meta["progressRows"] = outbox, progress

	err = a.store.DeleteCorpusState(ctx, st.Corpus, st.Version)
	if !errors.Is(err, repository.ErrStateConflict) {
		if err != nil {
			return fmt.Errorf("retrieval admin: delete corpus state: %w", err)
		}
		return nil
	}
	// The compare-and-delete missed: the row is gone (a concurrent drop
	// finished, which is the outcome we wanted) or its version moved (another
	// replica re-bootstrapped the corpus, so it is registered again).
	cur, gerr := a.store.CorpusState(ctx, st.Corpus)
	switch {
	case gerr != nil:
		return fmt.Errorf("retrieval admin: corpus state: %w", gerr)
	case cur == nil:
		return nil
	default:
		return ErrCorpusRegistered
	}
}

// auditReason is the error code the admin handler answers a failed action
// with, so the audit trail and the response agree.
func auditReason(err error) string {
	switch {
	case errors.Is(err, iface.ErrRetrievalNotConfigured):
		return errcode.RetrievalNotConfigured
	case errors.Is(err, iface.ErrRetrievalUnavailable):
		return errcode.RetrievalUnavailable
	case errors.Is(err, ErrCorpusRegistered):
		return errcode.RetrievalCorpusRegistered
	case errors.Is(err, ErrCorpusNotFound):
		return errcode.RetrievalCorpusNotFound
	default:
		return errcode.RetrievalInternal
	}
}
