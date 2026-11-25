package services

import (
	"context"
	"log/slog"
	"maps"
	"slices"
	"sync"
	"testing"
	"unicode/utf8"

	"github.com/orkestra/backend/internal/core/retrieval/engine"
	"github.com/orkestra/backend/internal/core/retrieval/models"
	"github.com/orkestra/backend/internal/testkit"
	"github.com/orkestra/backend/pkg/sdk/ctxauth"
	"github.com/orkestra/backend/pkg/sdk/iface"
)

// Search fakes. PR 4's fakes_test.go owns the fake* names in this package, so
// every search fake is prefixed search instead. Each records its calls.

// searchEngine answers Query from a script: call i returns results[i] (the
// last entry once the script runs out). Every other Engine method panics, so
// the search path can never write.
type searchEngine struct {
	mu          sync.Mutex
	queries     []engine.Query
	collections []string
	results     [][]engine.Candidate
	err         error
}

func (e *searchEngine) Query(_ context.Context, collection string, q engine.Query) ([]engine.Candidate, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	q.Principals = slices.Clone(q.Principals)
	e.queries = append(e.queries, q)
	e.collections = append(e.collections, collection)
	if e.err != nil {
		return nil, e.err
	}
	if len(e.results) == 0 {
		return nil, nil
	}
	i := min(len(e.queries)-1, len(e.results)-1)
	out := make([]engine.Candidate, len(e.results[i]))
	for j, c := range e.results[i] {
		c.Runtime = copyRuntime(c.Runtime)
		out[j] = c
	}
	return out, nil
}

func (e *searchEngine) calls() int {
	e.mu.Lock()
	defer e.mu.Unlock()
	return len(e.queries)
}

func (e *searchEngine) Health(context.Context) error { panic("searchEngine: Health") }
func (e *searchEngine) EnsureCollection(context.Context, engine.CollectionSpec) error {
	panic("searchEngine: EnsureCollection")
}
func (e *searchEngine) DropCollection(context.Context, string) error {
	panic("searchEngine: DropCollection")
}
func (e *searchEngine) Upsert(context.Context, string, []engine.Point) error {
	panic("searchEngine: Upsert")
}
func (e *searchEngine) UpdatePayload(context.Context, string, []engine.Point) error {
	panic("searchEngine: UpdatePayload")
}
func (e *searchEngine) DeleteOtherRevisions(context.Context, string, string, string, int64) error {
	panic("searchEngine: DeleteOtherRevisions")
}
func (e *searchEngine) DeleteByFilter(context.Context, string, engine.Selector) error {
	panic("searchEngine: DeleteByFilter")
}
func (e *searchEngine) Count(context.Context, string, engine.Selector) (int64, error) {
	panic("searchEngine: Count")
}

// searchState returns gens in sequence (the last one once it runs out), so a
// test can prove the pin is read once per search.
type searchState struct {
	mu      sync.Mutex
	gens    []PinnedGeneration
	err     error
	corpora []string
}

func (s *searchState) ActiveGeneration(_ context.Context, corpus string) (PinnedGeneration, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.corpora = append(s.corpora, corpus)
	if s.err != nil {
		return PinnedGeneration{}, s.err
	}
	if len(s.gens) == 0 {
		return PinnedGeneration{}, ErrNoActiveGeneration
	}
	return s.gens[min(len(s.corpora)-1, len(s.gens)-1)], nil
}

func (s *searchState) reads() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.corpora)
}

// searchProfiles serves profiles by endpoint name; errs wins over profiles.
type searchProfiles struct {
	mu       sync.Mutex
	profiles map[string]*iface.InferenceProfile
	errs     map[string]error
	calls    []string
}

func (p *searchProfiles) InferenceProfile(_ context.Context, endpoint string) (*iface.InferenceProfile, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.calls = append(p.calls, endpoint)
	if err := p.errs[endpoint]; err != nil {
		return nil, err
	}
	prof, ok := p.profiles[endpoint]
	if !ok || prof == nil {
		return nil, nil
	}
	cp := *prof
	return &cp, nil
}

// searchEmbedder returns one vector, vec, per call unless vectors overrides
// the whole Vectors field; onCall runs first (for example to cancel ctx).
type searchEmbedder struct {
	mu      sync.Mutex
	reqs    []iface.PinnedEmbedRequest
	vec     []float32
	vectors [][]float32
	err     error
	onCall  func()
}

func (e *searchEmbedder) EmbedWithProfile(_ context.Context, req iface.PinnedEmbedRequest) (*iface.EmbedResult, error) {
	e.mu.Lock()
	req.Inputs = slices.Clone(req.Inputs)
	e.reqs = append(e.reqs, req)
	hook, err := e.onCall, e.err
	vectors := e.vectors
	if vectors == nil {
		vectors = [][]float32{slices.Clone(e.vec)}
	}
	e.mu.Unlock()
	if hook != nil {
		hook()
	}
	if err != nil {
		return nil, err
	}
	return &iface.EmbedResult{Vectors: vectors, Dimensions: len(vectors[0])}, nil
}

// searchReranker records rerank requests and returns res or err.
type searchReranker struct {
	mu   sync.Mutex
	reqs []iface.RerankRequest
	res  *iface.RerankResult
	err  error
	// onCall, when set, runs first (for example to cancel ctx).
	onCall func()
}

func (r *searchReranker) Rerank(_ context.Context, req iface.RerankRequest) (*iface.RerankResult, error) {
	r.mu.Lock()
	hook := r.onCall
	defer r.mu.Unlock()
	if hook != nil {
		hook()
	}
	req.Documents = slices.Clone(req.Documents)
	r.reqs = append(r.reqs, req)
	if r.err != nil {
		return nil, r.err
	}
	return r.res, nil
}

// searchSource answers Visible: an id in missing is left out of the map, an
// id in visible gets that answer, any other id is visible at revision 1 (the
// fixture candidates' revision). With block set it waits for ctx to end.
// onCall, when set, runs first with the call's ctx and 1-based number; a
// non-nil return is the call's error. Load and Enumerate panic: search never
// loads.
type searchSource struct {
	mu      sync.Mutex
	visible map[string]iface.RetrievalVisibility
	missing map[string]bool
	err     error
	block   bool
	onCall  func(ctx context.Context, call int) error
	calls   [][]string
	tenants []string
	pcs     []iface.RetrievalPrincipalContext
}

func (s *searchSource) Load(context.Context, string, []string) ([]iface.RetrievalLoaded, error) {
	panic("searchSource: Load")
}

func (s *searchSource) Enumerate(context.Context, string, string, int) (iface.RetrievalPage, error) {
	panic("searchSource: Enumerate")
}

func (s *searchSource) Visible(ctx context.Context, tenantID string, pc iface.RetrievalPrincipalContext, ids []string) (map[string]iface.RetrievalVisibility, error) {
	s.mu.Lock()
	s.calls = append(s.calls, slices.Clone(ids))
	s.tenants = append(s.tenants, tenantID)
	pc.Principals = slices.Clone(pc.Principals)
	s.pcs = append(s.pcs, pc)
	block, err, hook, call := s.block, s.err, s.onCall, len(s.calls)
	visible, missing := maps.Clone(s.visible), maps.Clone(s.missing)
	s.mu.Unlock()
	if hook != nil {
		if err := hook(ctx, call); err != nil {
			return nil, err
		}
	}
	if block {
		<-ctx.Done()
		return nil, ctx.Err()
	}
	if err != nil {
		return nil, err
	}
	out := make(map[string]iface.RetrievalVisibility, len(ids))
	for _, id := range ids {
		if missing[id] {
			continue
		}
		if v, ok := visible[id]; ok {
			out[id] = v
			continue
		}
		out[id] = iface.RetrievalVisibility{Visible: true, Revision: 1}
	}
	return out, nil
}

// searchTombstones covers the source IDs set in covered and leaves the ones
// in missing out of its answer. A source in coveredFrom is covered from that
// 1-based call on (a tombstone landing between two calls).
type searchTombstones struct {
	mu          sync.Mutex
	covered     map[string]bool
	coveredFrom map[string]int
	missing     map[string]bool
	err         error
	probes      [][]Probe
	args        []string // "tenant|corpus" per call
}

func (t *searchTombstones) Covered(_ context.Context, tenantID, corpus string, probes []Probe) (map[string]bool, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.args = append(t.args, tenantID+"|"+corpus)
	cp := make([]Probe, len(probes))
	for i, p := range probes {
		cp[i] = Probe{SourceID: p.SourceID, Subjects: slices.Clone(p.Subjects)}
	}
	t.probes = append(t.probes, cp)
	if t.err != nil {
		return nil, t.err
	}
	out := make(map[string]bool, len(probes))
	for _, p := range probes {
		if t.missing[p.SourceID] {
			continue
		}
		from, ok := t.coveredFrom[p.SourceID]
		out[p.SourceID] = t.covered[p.SourceID] || (ok && len(t.probes) >= from)
	}
	return out, nil
}

// searchAuthz answers HasPermission with allow or err and records each
// question as "user|org|permission".
type searchAuthz struct {
	mu    sync.Mutex
	allow bool
	err   error
	asked []string
}

func (a *searchAuthz) HasPermission(_ context.Context, userUUID, orgUUID, permission string) (bool, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.asked = append(a.asked, userUUID+"|"+orgUUID+"|"+permission)
	if a.err != nil {
		return false, a.err
	}
	return a.allow, nil
}

func (a *searchAuthz) GetEffectivePermissions(context.Context, string, string) ([]string, error) {
	panic("searchAuthz: GetEffectivePermissions")
}

func (a *searchAuthz) RegisterPermissions(context.Context, []iface.PermissionSpec) error {
	panic("searchAuthz: RegisterPermissions")
}

// searchPrincipals returns pc or err.
type searchPrincipals struct {
	mu    sync.Mutex
	pc    iface.RetrievalPrincipalContext
	err   error
	calls int
}

func (p *searchPrincipals) Principals(context.Context) (iface.RetrievalPrincipalContext, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.calls++
	if p.err != nil {
		return iface.RetrievalPrincipalContext{}, p.err
	}
	pc := p.pc
	pc.Principals = slices.Clone(pc.Principals)
	return pc, nil
}

// searchModules reports the modules set to true as started.
type searchModules struct {
	mu      sync.Mutex
	started map[string]bool
}

func (m *searchModules) IsStarted(name string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.started[name]
}

// searchCorpora serves copies of the registered specs.
type searchCorpora struct {
	mu    sync.Mutex
	specs map[string]iface.RetrievalCorpusSpec
}

func (c *searchCorpora) Corpus(name string) (iface.RetrievalCorpusSpec, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	spec, ok := c.specs[name]
	if !ok {
		return iface.RetrievalCorpusSpec{}, false
	}
	return cloneSpec(spec), true
}

// searchLog stores inserted rows and the ctx tenant of each insert.
type searchLog struct {
	mu        sync.Mutex
	rows      []*models.SearchLog
	err       error
	ctxTenant []string
}

func (l *searchLog) Insert(ctx context.Context, row *models.SearchLog) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	tenant, _ := ctxauth.GetTenantID(ctx)
	l.ctxTenant = append(l.ctxTenant, tenant)
	if l.err != nil {
		return l.err
	}
	cp := *row
	l.rows = append(l.rows, &cp)
	return nil
}

// cand builds a candidate of tenant t-1 in corpus contacts. Its PointID is
// the real point ID (the refill dedups on it) and its score falls with the
// ordinal.
func cand(source string, rev int64, ordinal int, text string, subjects ...string) engine.Candidate {
	return engine.Candidate{
		PointID: engine.PointID("t-1", "contacts", source, ordinal),
		Runtime: engine.Runtime{
			TenantID:       "t-1",
			SourceID:       source,
			SourceRevision: rev,
			ChunkID:        ChunkID(source, ordinal),
			Ordinal:        ordinal,
			VisibleTo:      []string{"tenant:*"},
			Subjects:       slices.Clone(subjects),
			CharStart:      0,
			CharEnd:        utf8.RuneCountInString(text),
		},
		Text:  text,
		Score: 1 / float32(ordinal+1),
	}
}

const (
	searchText1 = "Alice Rossi is the head of sales for the northern region."
	searchText2 = "Contact c-2 moved from marketing to the sales team in March."
	searchText3 = "Bob Bianchi leads procurement and reports to the head of sales."
)

// searchFixture is a Searcher whose every dependency is a recording fake:
// corpus contacts of module crm, internal tenant t-1, user u-1.
type searchFixture struct {
	t          *testing.T
	ctx        context.Context
	spec       iface.RetrievalCorpusSpec
	corpora    *searchCorpora
	state      *searchState
	engine     *searchEngine
	source     *searchSource
	tombstones *searchTombstones
	authz      *searchAuthz
	principals *searchPrincipals
	profiles   *searchProfiles
	embedder   *searchEmbedder
	reranker   *searchReranker
	modules    *searchModules
	log        *searchLog
	logs       *lockedBuffer
	// tweak, when set, edits the deps right before the Searcher is built
	// (a nil dependency, a timeout).
	tweak func(d *SearchDeps)
}

func newSearchFixture(t *testing.T) *searchFixture {
	t.Helper()
	id := testkit.NewIdentity("u-1", "u1@example.test", "operator").WithTenant("t-1", []string{"org_member"}, true)
	fx := &searchFixture{
		t:   t,
		ctx: ctxauth.WithTenantKind(id.ContextFor(context.Background(), "t-1"), "internal"),
		spec: iface.RetrievalCorpusSpec{
			Name:       "contacts",
			Owner:      "crm",
			Permission: "crm.contact.read",
			Filters: []iface.RetrievalFilterField{
				{Name: "team", Type: iface.RetrievalFieldKeyword},
				{Name: "seniority", Type: iface.RetrievalFieldInteger},
			},
			Ingestion: iface.RetrievalIngestionProfile{EmbedEndpoint: "embed"},
			Locality:  "",
			Fusion:    iface.RetrievalFusion{DenseWeight: 1, KeywordWeight: 1, Candidates: 100},
			Rerank:    iface.RetrievalRerank{Endpoint: "rerank", Enabled: false, TopK: DefaultRerankTopK},
		},
		state: &searchState{gens: []PinnedGeneration{{Collection: "orkestra_contacts_g1", Generation: 1, EmbedProfileID: "ip_embed_a"}}},
		engine: &searchEngine{results: [][]engine.Candidate{{
			cand("s1", 1, 0, searchText1, "user:alice"),
			cand("s2", 1, 0, searchText2, "crm:c-2"),
			cand("s3", 1, 0, searchText3, "user:bob"),
		}}},
		source:     &searchSource{},
		tombstones: &searchTombstones{},
		authz:      &searchAuthz{allow: true},
		principals: &searchPrincipals{pc: iface.RetrievalPrincipalContext{
			TenantID: "t-1", UserUUID: "u-1", Principals: []string{"tenant:*", "user:u-1"},
		}},
		profiles: &searchProfiles{profiles: map[string]*iface.InferenceProfile{
			"embed":  {ID: "ip_embed_a", Endpoint: "embed", Kind: iface.InferenceKindEmbed, Boundary: iface.InferenceBoundaryLocal, Dimensions: 4},
			"rerank": {ID: "ip_rerank_a", Endpoint: "rerank", Kind: iface.InferenceKindRerank, Boundary: iface.InferenceBoundaryLocal},
		}},
		embedder: &searchEmbedder{vec: []float32{0.1, 0.2, 0.3, 0.4}},
		reranker: &searchReranker{},
		modules:  &searchModules{started: map[string]bool{"crm": true}},
		log:      &searchLog{},
		logs:     &lockedBuffer{},
	}
	return fx
}

// deps wires the fakes; the spec is registered as it stands now, with the
// fixture's source.
func (fx *searchFixture) deps() SearchDeps {
	spec := cloneSpec(fx.spec)
	spec.Source = fx.source
	fx.corpora = &searchCorpora{specs: map[string]iface.RetrievalCorpusSpec{spec.Name: spec}}
	d := SearchDeps{
		Corpora:    fx.corpora,
		State:      fx.state,
		Engine:     fx.engine,
		Tombstones: fx.tombstones,
		Authz:      fx.authz,
		Principals: fx.principals,
		Profiles:   fx.profiles,
		Embedder:   fx.embedder,
		Reranker:   fx.reranker,
		Modules:    fx.modules,
		Log:        fx.log,
		Logger:     slog.New(slog.NewTextHandler(fx.logs, &slog.HandlerOptions{Level: slog.LevelDebug})),
	}
	if fx.tweak != nil {
		fx.tweak(&d)
	}
	return d
}

func (fx *searchFixture) search(req iface.RetrievalSearchRequest) (*iface.RetrievalSearchResult, error) {
	return NewSearcher(fx.deps()).Search(fx.ctx, req)
}

// lastQuery is the most recent engine query; the test fails without one.
func (fx *searchFixture) lastQuery() engine.Query {
	fx.t.Helper()
	fx.engine.mu.Lock()
	defer fx.engine.mu.Unlock()
	if len(fx.engine.queries) == 0 {
		fx.t.Fatalf("no engine query was made")
	}
	return fx.engine.queries[len(fx.engine.queries)-1]
}

// contactsReq is a valid default request against the fixture corpus.
func contactsReq() iface.RetrievalSearchRequest {
	return iface.RetrievalSearchRequest{Corpus: "contacts", Query: "head of sales"}
}
