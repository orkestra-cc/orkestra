package services

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/orkestra/backend/internal/core/retrieval/engine"
	"github.com/orkestra/backend/internal/core/retrieval/engine/enginefake"
	"github.com/orkestra/backend/internal/core/retrieval/models"
	"github.com/orkestra/backend/internal/core/retrieval/repository"
	"github.com/orkestra/backend/internal/testkit"
	"github.com/orkestra/backend/pkg/sdk/ctxauth"
	"github.com/orkestra/backend/pkg/sdk/iface"
)

// ---- fakes ----

// adminEngine adds fault injection and a call log to the shared fake.
type adminEngine struct {
	*enginefake.Engine
	log       *adminLog
	healthErr error
	dropErr   map[string]error
	drops     []string
}

func (e *adminEngine) Health(context.Context) error { return e.healthErr }

func (e *adminEngine) DropCollection(ctx context.Context, name string) error {
	e.drops = append(e.drops, name)
	e.log.add("drop:" + name)
	if err := e.dropErr[name]; err != nil {
		return err
	}
	return e.Engine.DropCollection(ctx, name)
}

type adminLog struct{ calls []string }

func (l *adminLog) add(s string) { l.calls = append(l.calls, s) }

type adminRuntimeFake struct {
	specs    []iface.RetrievalCorpusSpec
	eng      engine.Engine
	engErr   error
	diagnose func(iface.RetrievalSearchRequest) (*DiagnosticResult, error)
	lastReq  iface.RetrievalSearchRequest
}

func (r *adminRuntimeFake) Corpus(name string) (iface.RetrievalCorpusSpec, bool) {
	for _, s := range r.specs {
		if s.Name == name {
			return s, true
		}
	}
	return iface.RetrievalCorpusSpec{}, false
}

func (r *adminRuntimeFake) Corpora() []iface.RetrievalCorpusSpec {
	out := slices.Clone(r.specs)
	slices.SortFunc(out, func(a, b iface.RetrievalCorpusSpec) int { return strings.Compare(a.Name, b.Name) })
	return out
}

func (r *adminRuntimeFake) Engine() (engine.Engine, error) {
	if r.engErr != nil {
		return nil, r.engErr
	}
	return r.eng, nil
}

func (r *adminRuntimeFake) CollectionPrefix() string { return "p" }

func (r *adminRuntimeFake) Diagnose(_ context.Context, req iface.RetrievalSearchRequest) (*DiagnosticResult, error) {
	r.lastReq = req
	return r.diagnose(req)
}

type adminStoreFake struct {
	log       *adminLog
	states    map[string]models.CorpusState
	backlog   map[string]models.RetrievalBacklog
	tenantBkl map[string]models.RetrievalBacklog
	progress  map[models.ProgressKey]models.ProgressCounts
	builds    map[string]*models.TenantProgress // corpus -> row
	erasures  models.RetrievalErasureCounts
	pending   int64
	listErr   error

	logRows  []models.SearchLog
	logTotal int64
	logErr   error
	logCalls []models.SearchLogQuery

	rowsErr       error
	stateDelErr   error
	afterFirstGet func() // runs once, after the first CorpusState read returned its copy
	getCalls      int
}

func (s *adminStoreFake) OutboxBacklogAll(context.Context) (map[string]models.RetrievalBacklog, error) {
	return s.backlog, nil
}
func (s *adminStoreFake) ProgressCounts(context.Context) (map[models.ProgressKey]models.ProgressCounts, error) {
	return s.progress, nil
}
func (s *adminStoreFake) ErasureCounts(context.Context) (models.RetrievalErasureCounts, error) {
	return s.erasures, nil
}
func (s *adminStoreFake) DeleteCorpusRows(_ context.Context, corpus string) (int64, int64, error) {
	s.log.add("rows:" + corpus)
	return 3, 2, s.rowsErr
}
func (s *adminStoreFake) DeleteCorpusState(_ context.Context, corpus string, version int64) error {
	s.log.add("state:" + corpus)
	if s.stateDelErr != nil {
		return s.stateDelErr
	}
	st, ok := s.states[corpus]
	if !ok || st.Version != version {
		return repository.ErrStateConflict
	}
	delete(s.states, corpus)
	return nil
}
func (s *adminStoreFake) OutboxBacklogTenant(ctx context.Context) (map[string]models.RetrievalBacklog, error) {
	return s.tenantBkl, nil
}
func (s *adminStoreFake) TenantErasuresPending(context.Context) (int64, error) { return s.pending, nil }
func (s *adminStoreFake) SearchLog(_ context.Context, q models.SearchLogQuery) ([]models.SearchLog, int64, error) {
	s.logCalls = append(s.logCalls, q)
	return s.logRows, s.logTotal, s.logErr
}
func (s *adminStoreFake) CorpusState(_ context.Context, name string) (*models.CorpusState, error) {
	s.getCalls++
	st, ok := s.states[name]
	if s.getCalls == 1 && s.afterFirstGet != nil {
		s.afterFirstGet()
	}
	if !ok {
		return nil, nil
	}
	return &st, nil
}
func (s *adminStoreFake) CorpusStateList(context.Context) ([]models.CorpusState, error) {
	if s.listErr != nil {
		return nil, s.listErr
	}
	out := make([]models.CorpusState, 0, len(s.states))
	for _, st := range s.states {
		out = append(out, st)
	}
	return out, nil
}
func (s *adminStoreFake) TenantBuild(_ context.Context, corpus string, _ int) (*models.TenantProgress, error) {
	return s.builds[corpus], nil
}

type adminFx struct {
	rt    *adminRuntimeFake
	eng   *adminEngine
	store *adminStoreFake
	sink  *recordingSink
	log   *adminLog
	prof  *searchProfiles
	mods  *fakeModules
	svc   *AdminService
	ctx   context.Context
}

func adminSpec(name, owner string) iface.RetrievalCorpusSpec {
	return iface.RetrievalCorpusSpec{Name: name, Owner: owner, Ingestion: goldenProfile()}
}

func newAdminFx(t *testing.T, specs ...iface.RetrievalCorpusSpec) *adminFx {
	t.Helper()
	lg := &adminLog{}
	fx := &adminFx{log: lg, sink: &recordingSink{}, mods: &fakeModules{started: map[string]bool{"crm": true}}}
	fx.eng = &adminEngine{Engine: enginefake.New(), log: lg, dropErr: map[string]error{}}
	fx.rt = &adminRuntimeFake{specs: specs, eng: fx.eng}
	fx.store = &adminStoreFake{log: lg, states: map[string]models.CorpusState{}}
	fx.prof = &searchProfiles{profiles: map[string]*iface.InferenceProfile{
		"embed": {ID: "ip_x", Kind: iface.InferenceKindEmbed, Boundary: iface.InferenceBoundaryLocal},
	}}
	relay := NewAuditRelay()
	relay.SetAuditSink(fx.sink)
	fx.svc = NewAdminService(AdminDeps{
		Runtime: fx.rt, Store: fx.store, Profiles: fx.prof, Modules: fx.mods, Audit: relay,
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	id := testkit.NewIdentity("u-admin", "admin@example.test", "administrator").WithTenant("tA", []string{"org_owner"}, true)
	fx.ctx = ctxauth.WithClientIP(ctxauth.WithTenantKind(id.ContextFor(context.Background(), "tA"), "internal"), "203.0.113.9")
	return fx
}

func corporaByName(rep *models.RetrievalCorporaReport) map[string]models.RetrievalCorpusStatus {
	m := map[string]models.RetrievalCorpusStatus{}
	for _, c := range rep.Corpora {
		m[c.Name] = c
	}
	return m
}

// ---- Corpora ----

func TestAdminCorpora_NotConfiguredStillListsCorpora(t *testing.T) {
	fx := newAdminFx(t, adminSpec("crm", "crm"))
	fx.rt.engErr = fmt.Errorf("%w", iface.ErrRetrievalNotConfigured)
	rep, err := fx.svc.Corpora(fx.ctx)
	require.NoError(t, err)
	assert.Equal(t, models.ComponentNotConfigured, rep.Engine.Status)
	require.Len(t, rep.Corpora, 1)
	assert.Equal(t, "uninitialized", rep.Corpora[0].State)
	assert.True(t, rep.Corpora[0].Registered)
	assert.True(t, rep.Corpora[0].OwnerRunning)
}

func TestAdminCorpora_OrphansAndStates(t *testing.T) {
	fx := newAdminFx(t, adminSpec("crm", "crm"))
	fx.store.states["crm"] = models.CorpusState{Corpus: "crm", Owner: "crm", Status: models.CorpusStatusActive, ActiveGeneration: 1,
		Collection: "p_crm_g1", IngestionProfileHash: "ing_h", EmbeddingProfileID: "ip_x", Version: 1}
	fx.store.states["old"] = models.CorpusState{Corpus: "old", Owner: "gone", Status: models.CorpusStatusActive, ActiveGeneration: 2,
		Locality: "hosted_allowed", Version: 4}
	fx.store.states["aaa"] = models.CorpusState{Corpus: "aaa", Status: models.CorpusStatusActive, ActiveGeneration: 1, Version: 1}
	now := time.Now()
	fx.store.backlog = map[string]models.RetrievalBacklog{"crm": {Pending: 5, OldestAt: &now}}
	fx.store.progress = map[models.ProgressKey]models.ProgressCounts{
		{Corpus: "crm", Generation: 1}: {Done: 2, Building: 1},
		{Corpus: "crm", Generation: 9}: {Done: 99},
	}
	fx.store.erasures = models.RetrievalErasureCounts{Pending: 3, Fencing: 1}

	rep, err := fx.svc.Corpora(fx.ctx)
	require.NoError(t, err)
	require.Len(t, rep.Corpora, 3)
	assert.Equal(t, []string{"crm", "aaa", "old"}, []string{rep.Corpora[0].Name, rep.Corpora[1].Name, rep.Corpora[2].Name}, "registered first, then orphans by name")
	crm := rep.Corpora[0]
	assert.Equal(t, "active", crm.State)
	assert.Equal(t, 1, crm.ActiveGeneration)
	assert.Equal(t, "p_crm_g1", crm.Collection)
	assert.Equal(t, "ing_h", crm.IngestionProfile)
	assert.Equal(t, "ip_x", crm.EmbeddingProfile)
	assert.Equal(t, int64(5), crm.Outbox.Pending)
	assert.Equal(t, int64(2), crm.TenantsDone)
	assert.Equal(t, int64(1), crm.TenantsBuilding)
	assert.Equal(t, iface.RetrievalLocalOnly, crm.Locality, "empty spec locality is local_only")
	assert.Equal(t, "embed", crm.EmbedEndpoint)
	old := rep.Corpora[2]
	assert.False(t, old.Registered)
	assert.False(t, old.OwnerRunning)
	assert.Equal(t, "gone", old.Owner)
	assert.Equal(t, iface.RetrievalHostedAllowed, old.Locality)
	assert.Equal(t, int64(3), rep.Erasures.Pending)
	assert.Equal(t, models.ComponentOK, rep.Engine.Status)
}

func TestAdminCorpora_ErrorStates(t *testing.T) {
	cases := []struct {
		name       string
		st         *models.CorpusState
		state, why string
		gen        int
		coll       string
	}{
		{"locality", &models.CorpusState{Status: "error", Reason: models.ReasonLocality}, "error", "locality", 0, ""},
		{"endpoint_kind", &models.CorpusState{Status: "error", Reason: models.ReasonEndpointKind}, "error", "endpoint_kind", 0, ""},
		{"endpoint_unknown", &models.CorpusState{Status: "error", Reason: models.ReasonEndpointUnknown}, "error", "endpoint_unknown", 0, ""},
		{"error keeps generation", &models.CorpusState{Status: "error", Reason: models.ReasonEndpointUnknown, ActiveGeneration: 1, Collection: "p_c_g1"}, "error", "endpoint_unknown", 1, "p_c_g1"},
		{"profile_changed", &models.CorpusState{Status: "profile_changed", ActiveGeneration: 1}, "profile_changed", "", 1, ""},
		{"active", &models.CorpusState{Status: "active", ActiveGeneration: 1}, "active", "", 1, ""},
		{"active without generation", &models.CorpusState{Status: "active"}, "uninitialized", "", 0, ""},
		{"no row", nil, "uninitialized", "", 0, ""},
		{"unknown status", &models.CorpusState{Status: "weird", ActiveGeneration: 1}, "error", "unknown_state", 1, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fx := newAdminFx(t, adminSpec("c", "crm"))
			if tc.st != nil {
				tc.st.Corpus, tc.st.Version = "c", 1
				fx.store.states["c"] = *tc.st
			}
			rep, err := fx.svc.Corpora(fx.ctx)
			require.NoError(t, err)
			c := rep.Corpora[0]
			assert.Equal(t, tc.state, c.State)
			assert.Equal(t, tc.why, c.Reason)
			assert.Equal(t, tc.gen, c.ActiveGeneration)
			assert.Equal(t, tc.coll, c.Collection)
		})
	}
}

// The state rows come from the real Worker.prepareCorpus, so the report is
// pinned to what the gate actually writes for each failure.
func TestAdminCorpora_FailedBootstrapIsNotActive(t *testing.T) {
	steps := []struct {
		name   string
		mutate func(e *gateEnv)
		reason string
	}{
		{"endpoint unknown", func(e *gateEnv) { e.profiles.setErr(iface.ErrLLMNotConfigured) }, models.ReasonEndpointUnknown},
		{"rerank-kind profile", func(e *gateEnv) {
			e.profiles.setErr(nil)
			e.profiles.update(func(p *iface.InferenceProfile) { p.Kind = iface.InferenceKindRerank })
		}, models.ReasonEndpointKind},
		{"hosted boundary on local_only", func(e *gateEnv) {
			e.profiles.update(func(p *iface.InferenceProfile) {
				p.Kind, p.Boundary, p.Endpoint = iface.InferenceKindEmbed, iface.InferenceBoundaryHosted, "embed"
			})
		}, models.ReasonLocality},
	}
	e := newGateEnv()
	fx := newAdminFx(t, e.spec)
	for _, step := range steps {
		step.mutate(e)
		g, problem := e.prepare(t)
		require.Nil(t, g)
		require.NotEmpty(t, problem)
		fx.store.states = map[string]models.CorpusState{e.spec.Name: e.states.get(e.spec.Name)}
		rep, err := fx.svc.Corpora(fx.ctx)
		require.NoError(t, err)
		c := corporaByName(rep)[e.spec.Name]
		assert.Equal(t, "error", c.State, step.name)
		assert.Equal(t, step.reason, c.Reason, step.name)
		assert.Equal(t, 0, c.ActiveGeneration, step.name)
	}
}

func TestAdminCorpora_InferenceStatuses(t *testing.T) {
	a := adminSpec("a", "crm")
	a.Rerank = iface.RetrievalRerank{Endpoint: "rr", Enabled: true, TopK: 10}
	b := adminSpec("b", "crm") // same embed endpoint; rerank configured but disabled
	b.Rerank = iface.RetrievalRerank{Endpoint: "off", Enabled: false}
	c := adminSpec("c", "crm")
	c.Ingestion.EmbedEndpoint = "wrong"
	d := adminSpec("d", "crm")
	d.Ingestion.EmbedEndpoint = "gone"
	e := adminSpec("e", "crm")
	e.Ingestion.EmbedEndpoint = "down"
	f := adminSpec("f", "crm")
	f.Ingestion.EmbedEndpoint = "rr" // the rerank endpoint used in the embed role too
	fx := newAdminFx(t, a, b, c, d, e, f)
	fx.prof.profiles["rr"] = &iface.InferenceProfile{ID: "ip_rr", Kind: iface.InferenceKindRerank, Boundary: iface.InferenceBoundaryHosted}
	fx.prof.profiles["wrong"] = &iface.InferenceProfile{ID: "ip_w", Kind: iface.InferenceKindRerank}
	fx.prof.errs = map[string]error{
		"gone": iface.ErrLLMNotConfigured,
		"down": fmt.Errorf("boom: %w", iface.ErrLLMProviderUnavailable),
	}
	rep, err := fx.svc.Corpora(fx.ctx)
	require.NoError(t, err)

	type row struct{ endpoint, kind, status, profile string }
	var got []row
	for _, r := range rep.Inference {
		got = append(got, row{r.Endpoint, r.Kind, string(r.Status), r.Profile})
	}
	assert.Equal(t, []row{
		{"down", "embed", "unavailable", ""},
		{"embed", "embed", "ok", "ip_x"},
		{"gone", "embed", "not_configured", ""},
		{"rr", "embed", "wrong_kind", "ip_rr"},
		{"rr", "rerank", "ok", "ip_rr"},
		{"wrong", "embed", "wrong_kind", "ip_w"},
	}, got)
	for _, r := range rep.Inference {
		if r.Endpoint == "rr" {
			assert.Equal(t, iface.InferenceBoundaryHosted, r.Boundary)
		}
	}
	assert.False(t, slices.Contains(fx.prof.calls, "off"), "a disabled rerank adds no row and no lookup")

	// mismatch
	fx.prof.errs["gone"] = iface.ErrLLMProfileMismatch
	rep, err = fx.svc.Corpora(fx.ctx)
	require.NoError(t, err)
	for _, r := range rep.Inference {
		if r.Endpoint == "gone" {
			assert.Equal(t, models.ComponentMismatch, r.Status)
		}
	}
}

func TestAdminCorpora_NilProfilesAreNotConfigured(t *testing.T) {
	fx := newAdminFx(t, adminSpec("crm", "crm"))
	fx.svc.profiles = nil
	rep, err := fx.svc.Corpora(fx.ctx)
	require.NoError(t, err)
	require.Len(t, rep.Inference, 1)
	assert.Equal(t, models.ComponentNotConfigured, rep.Inference[0].Status)
}

func TestAdminCorpora_EngineDownIsReportedNotFailed(t *testing.T) {
	fx := newAdminFx(t, adminSpec("crm", "crm"))
	fx.eng.healthErr = engine.ErrUnavailable
	rep, err := fx.svc.Corpora(fx.ctx)
	require.NoError(t, err)
	assert.Equal(t, models.ComponentUnavailable, rep.Engine.Status)
	assert.Len(t, rep.Corpora, 1)
}

func TestAdminCorpora_StoreErrorFailsTheCall(t *testing.T) {
	fx := newAdminFx(t, adminSpec("crm", "crm"))
	fx.store.listErr = errors.New("mongo down")
	_, err := fx.svc.Corpora(fx.ctx)
	require.Error(t, err)
}

func TestAdminCorpora_RequiresInternalTenant(t *testing.T) {
	fx := newAdminFx(t, adminSpec("crm", "crm"))
	ext := ctxauth.WithTenantKind(fx.ctx, "external")
	_, err := fx.svc.Corpora(ext)
	assert.ErrorIs(t, err, iface.ErrRetrievalForbidden)
	_, err = fx.svc.TenantIndexing(ext)
	assert.ErrorIs(t, err, iface.ErrRetrievalForbidden)
	assert.ErrorIs(t, fx.svc.DropOrphanCorpus(ext, "old"), iface.ErrRetrievalForbidden)
	assert.Empty(t, fx.sink.events)
}

// ---- TenantIndexing ----

func TestTenantIndexing_PointsOnlyWhenEngineOK(t *testing.T) {
	fx := newAdminFx(t, adminSpec("crm", "crm"), adminSpec("fresh", "crm"))
	fx.store.states["crm"] = models.CorpusState{Corpus: "crm", Status: "active", ActiveGeneration: 1, Version: 1}
	fx.store.tenantBkl = map[string]models.RetrievalBacklog{"crm": {Pending: 2}}
	fx.store.builds = map[string]*models.TenantProgress{"crm": {Status: models.ProgressBuilding}}
	fx.store.pending = 4
	pt := func(id, tenant string) engine.Point {
		return engine.Point{ID: id, Dense: []float32{1}, Runtime: engine.Runtime{TenantID: tenant, SourceID: id, VisibleTo: []string{"tenant:*"}}}
	}
	fx.eng.Seed("p_crm_g1", pt("a", "tA"), pt("b", "tA"), pt("c", "tB"))

	rep, err := fx.svc.TenantIndexing(fx.ctx)
	require.NoError(t, err)
	assert.Equal(t, "tA", rep.TenantID)
	assert.Equal(t, int64(4), rep.ErasuresPending)
	require.Len(t, rep.Corpora, 2)
	crm, fresh := rep.Corpora[0], rep.Corpora[1]
	require.NotNil(t, crm.Points)
	assert.Equal(t, int64(2), *crm.Points, "only this tenant's points")
	assert.Equal(t, "building", crm.InitialBuild)
	assert.Equal(t, int64(2), crm.Outbox.Pending)
	assert.True(t, crm.OwnerRunning)
	assert.Nil(t, fresh.Points)
	assert.Equal(t, "pending", fresh.InitialBuild)
	assert.Equal(t, "active", crm.State)
	assert.Equal(t, "uninitialized", fresh.State)

	fx.store.builds["crm"] = &models.TenantProgress{Status: models.ProgressDone}
	rep, err = fx.svc.TenantIndexing(fx.ctx)
	require.NoError(t, err)
	assert.Equal(t, "done", rep.Corpora[0].InitialBuild)

	// engine not configured: no count, no failure
	fx.rt.engErr = fmt.Errorf("%w", iface.ErrRetrievalNotConfigured)
	rep, err = fx.svc.TenantIndexing(fx.ctx)
	require.NoError(t, err)
	assert.Equal(t, models.ComponentNotConfigured, rep.Engine.Status)
	assert.Nil(t, rep.Corpora[0].Points)
}

// A broken corpus is reported as broken to the organization, not as a
// queued build: its state says error (the reason stays operator-only).
func TestTenantIndexing_ReportsTheCorpusState(t *testing.T) {
	fx := newAdminFx(t, adminSpec("broken", "crm"), adminSpec("changed", "crm"))
	fx.store.states["broken"] = models.CorpusState{Corpus: "broken", Status: models.CorpusStatusError, Reason: "locality", Version: 1}
	fx.store.states["changed"] = models.CorpusState{Corpus: "changed", Status: models.CorpusStatusProfileChanged, ActiveGeneration: 1, Version: 1}
	rep, err := fx.svc.TenantIndexing(fx.ctx)
	require.NoError(t, err)
	require.Len(t, rep.Corpora, 2)
	byName := map[string]models.RetrievalTenantCorpus{}
	for _, c := range rep.Corpora {
		byName[c.Corpus] = c
	}
	assert.Equal(t, "error", byName["broken"].State)
	assert.Equal(t, "pending", byName["broken"].InitialBuild)
	assert.Equal(t, "profile_changed", byName["changed"].State)
	out, err := json.Marshal(rep)
	require.NoError(t, err)
	assert.NotContains(t, string(out), "locality")
}

func TestTenantIndexing_CountErrors(t *testing.T) {
	fx := newAdminFx(t, adminSpec("crm", "crm"))
	fx.store.states["crm"] = models.CorpusState{Corpus: "crm", Status: "active", ActiveGeneration: 1, Version: 1}

	// the collection is missing: points unknown, engine still ok
	rep, err := fx.svc.TenantIndexing(fx.ctx)
	require.NoError(t, err)
	assert.Nil(t, rep.Corpora[0].Points)
	assert.Equal(t, models.ComponentOK, rep.Engine.Status)

	fx.eng.Seed("p_crm_g1")
	fx.eng.FailCounts(engine.ErrUnavailable)
	rep, err = fx.svc.TenantIndexing(fx.ctx)
	require.NoError(t, err)
	assert.Nil(t, rep.Corpora[0].Points)
	assert.Equal(t, models.ComponentUnavailable, rep.Engine.Status)
}

func TestTenantIndexing_UsesTheStateCollectionName(t *testing.T) {
	fx := newAdminFx(t, adminSpec("crm", "crm"))
	fx.store.states["crm"] = models.CorpusState{Corpus: "crm", Status: "active", ActiveGeneration: 1, Collection: "custom_crm", Version: 1}
	fx.eng.Seed("custom_crm", engine.Point{ID: "a", Dense: []float32{1}, Runtime: engine.Runtime{TenantID: "tA", SourceID: "a", VisibleTo: []string{"tenant:*"}}})
	rep, err := fx.svc.TenantIndexing(fx.ctx)
	require.NoError(t, err)
	require.NotNil(t, rep.Corpora[0].Points)
	assert.Equal(t, int64(1), *rep.Corpora[0].Points)
}

// ---- DropOrphanCorpus ----

func TestDropOrphan_RefusesRegistered(t *testing.T) {
	fx := newAdminFx(t, adminSpec("crm", "crm"))
	fx.store.states["crm"] = models.CorpusState{Corpus: "crm", Version: 1}
	err := fx.svc.DropOrphanCorpus(fx.ctx, "crm")
	assert.ErrorIs(t, err, ErrCorpusRegistered)
	assert.Empty(t, fx.log.calls)
	assert.Equal(t, 0, fx.store.getCalls)
	// Past the operator check, so the refusal is audited with its reason.
	require.Len(t, fx.sink.events, 1)
	ev := fx.sink.events[0]
	assert.Equal(t, AuditCorpusDropped, ev.Action)
	assert.Equal(t, "failure", ev.Outcome)
	assert.Equal(t, "crm", ev.ResourceID)
	assert.Equal(t, map[string]any{"reason": "retrieval.corpus_registered"}, ev.Metadata)
}

func TestDropOrphan_UnknownCorpus(t *testing.T) {
	fx := newAdminFx(t)
	err := fx.svc.DropOrphanCorpus(fx.ctx, "nope")
	assert.ErrorIs(t, err, ErrCorpusNotFound)
	assert.Empty(t, fx.log.calls)
	require.Len(t, fx.sink.events, 1)
	assert.Equal(t, "failure", fx.sink.events[0].Outcome)
	assert.Equal(t, map[string]any{"reason": "retrieval.corpus_not_found"}, fx.sink.events[0].Metadata)
}

func TestDropOrphan_StateReadFailureIsAudited(t *testing.T) {
	fx := newAdminFx(t)
	fx.svc.store = stateGetFails{fx.store}
	err := fx.svc.DropOrphanCorpus(fx.ctx, "old")
	require.Error(t, err)
	assert.Empty(t, fx.log.calls)
	evs := fx.sink.byAction(AuditCorpusDropped)
	require.Len(t, evs, 1)
	assert.Equal(t, "failure", evs[0].Outcome)
	assert.Equal(t, "retrieval.internal", evs[0].Metadata["reason"])
	assert.NotContains(t, fmt.Sprint(evs[0].Metadata), "mongo")
}

type stateGetFails struct{ *adminStoreFake }

func (stateGetFails) CorpusState(context.Context, string) (*models.CorpusState, error) {
	return nil, errors.New("mongo: dial tcp 10.0.0.3:27017")
}

func TestDropOrphan_DropsEveryGeneration(t *testing.T) {
	fx := newAdminFx(t)
	fx.store.states["old"] = models.CorpusState{Corpus: "old", Status: "active", ActiveGeneration: 2, Collection: "p_old_g2", Version: 7}
	fx.eng.Seed("p_old_g2")
	require.NoError(t, fx.svc.DropOrphanCorpus(fx.ctx, "old"))

	assert.Equal(t, []string{"drop:p_old_g1", "drop:p_old_g2", "rows:old", "state:old"}, fx.log.calls)
	_, left := fx.store.states["old"]
	assert.False(t, left)
	evs := fx.sink.byAction(AuditCorpusDropped)
	require.Len(t, evs, 1)
	ev := evs[0]
	assert.Equal(t, "success", ev.Outcome)
	assert.Equal(t, "retrieval_corpus", ev.ResourceType)
	assert.Equal(t, "old", ev.ResourceID)
	assert.Equal(t, []string{"p_old_g1", "p_old_g2"}, ev.Metadata["collections"])
	assert.Equal(t, int64(3), ev.Metadata["outboxRows"])
	assert.Equal(t, int64(2), ev.Metadata["progressRows"])
}

func TestDropOrphan_FailedBootstrapStillDropsGenerationOne(t *testing.T) {
	fx := newAdminFx(t)
	fx.store.states["old"] = models.CorpusState{Corpus: "old", Status: "error", Reason: "locality", Version: 1}
	require.NoError(t, fx.svc.DropOrphanCorpus(fx.ctx, "old"))
	assert.Equal(t, []string{"p_old_g1"}, fx.eng.drops)
}

func TestDropOrphan_StateCollectionOutsideTheNamingScheme(t *testing.T) {
	fx := newAdminFx(t)
	fx.store.states["old"] = models.CorpusState{Corpus: "old", Status: "active", ActiveGeneration: 1, Collection: "legacy_old", Version: 1}
	require.NoError(t, fx.svc.DropOrphanCorpus(fx.ctx, "old"))
	assert.Equal(t, []string{"p_old_g1", "legacy_old"}, fx.eng.drops)
}

func TestDropOrphan_EngineOutageKeepsState(t *testing.T) {
	fx := newAdminFx(t)
	fx.store.states["old"] = models.CorpusState{Corpus: "old", Status: "active", ActiveGeneration: 2, Version: 1}
	fx.eng.dropErr["p_old_g2"] = engine.ErrUnavailable
	err := fx.svc.DropOrphanCorpus(fx.ctx, "old")
	assert.ErrorIs(t, err, iface.ErrRetrievalUnavailable)
	assert.Equal(t, []string{"drop:p_old_g1", "drop:p_old_g2"}, fx.log.calls, "rows and state untouched")
	_, kept := fx.store.states["old"]
	assert.True(t, kept)
	evs := fx.sink.byAction(AuditCorpusDropped)
	require.Len(t, evs, 1)
	assert.Equal(t, "failure", evs[0].Outcome)
	assert.Equal(t, "retrieval.unavailable", evs[0].Metadata["reason"])
}

func TestDropOrphan_EngineNotConfigured(t *testing.T) {
	fx := newAdminFx(t)
	fx.store.states["old"] = models.CorpusState{Corpus: "old", Version: 1}
	fx.rt.engErr = fmt.Errorf("%w", iface.ErrRetrievalNotConfigured)
	err := fx.svc.DropOrphanCorpus(fx.ctx, "old")
	assert.ErrorIs(t, err, iface.ErrRetrievalNotConfigured)
	evs := fx.sink.byAction(AuditCorpusDropped)
	require.Len(t, evs, 1)
	assert.Equal(t, "failure", evs[0].Outcome)
	assert.Equal(t, "retrieval.not_configured", evs[0].Metadata["reason"])
}

func TestDropOrphan_RowsFailureKeepsState(t *testing.T) {
	fx := newAdminFx(t)
	fx.store.states["old"] = models.CorpusState{Corpus: "old", Version: 1}
	fx.store.rowsErr = errors.New("mongo down")
	err := fx.svc.DropOrphanCorpus(fx.ctx, "old")
	require.Error(t, err)
	_, kept := fx.store.states["old"]
	assert.True(t, kept)
	assert.False(t, slices.Contains(fx.log.calls, "state:old"))
	assert.Equal(t, "retrieval.internal", fx.sink.byAction(AuditCorpusDropped)[0].Metadata["reason"])
}

// A partial rows failure (the store deleted some rows before failing) still
// reports the committed counts in the failure audit event.
func TestDropOrphan_RowsPartialFailureAuditsCommittedCounts(t *testing.T) {
	fx := newAdminFx(t)
	fx.store.states["old"] = models.CorpusState{Corpus: "old", Version: 1}
	fx.store.rowsErr = errors.New("progress delete failed")
	require.Error(t, fx.svc.DropOrphanCorpus(fx.ctx, "old"))
	evs := fx.sink.byAction(AuditCorpusDropped)
	require.Len(t, evs, 1)
	assert.Equal(t, "failure", evs[0].Outcome)
	assert.Equal(t, int64(3), evs[0].Metadata["outboxRows"])
	assert.Equal(t, int64(2), evs[0].Metadata["progressRows"])
}

// A retry after a complete drop finds no state: 404, nothing touched, and
// the refusal audited.
func TestDropOrphan_RetryAfterCompleteDropIsNotFound(t *testing.T) {
	fx := newAdminFx(t)
	fx.store.states["old"] = models.CorpusState{Corpus: "old", Version: 1}
	require.NoError(t, fx.svc.DropOrphanCorpus(fx.ctx, "old"))
	fx.log.calls, fx.sink.events = nil, nil
	assert.ErrorIs(t, fx.svc.DropOrphanCorpus(fx.ctx, "old"), ErrCorpusNotFound)
	assert.Empty(t, fx.log.calls)
	require.Len(t, fx.sink.events, 1)
	assert.Equal(t, "retrieval.corpus_not_found", fx.sink.events[0].Metadata["reason"])
}

// The row disappears between the read and the compare-and-delete (a
// concurrent drop finished): the outcome we wanted, so success.
func TestDropOrphan_RowGoneAtDeleteIsSuccess(t *testing.T) {
	fx := newAdminFx(t)
	fx.store.states["old"] = models.CorpusState{Corpus: "old", Version: 1}
	fx.store.afterFirstGet = func() { delete(fx.store.states, "old") }
	require.NoError(t, fx.svc.DropOrphanCorpus(fx.ctx, "old"))
	assert.Equal(t, "success", fx.sink.byAction(AuditCorpusDropped)[0].Outcome)
}

// The version moved: another replica re-bootstrapped the corpus.
func TestDropOrphan_VersionMovedIsConflict(t *testing.T) {
	fx := newAdminFx(t)
	fx.store.states["old"] = models.CorpusState{Corpus: "old", Version: 1}
	fx.store.afterFirstGet = func() {
		st := fx.store.states["old"]
		st.Version = 2
		fx.store.states["old"] = st
	}
	err := fx.svc.DropOrphanCorpus(fx.ctx, "old")
	assert.ErrorIs(t, err, ErrCorpusRegistered)
	_, kept := fx.store.states["old"]
	assert.True(t, kept)
	evs := fx.sink.byAction(AuditCorpusDropped)
	require.Len(t, evs, 1)
	assert.Equal(t, "failure", evs[0].Outcome)
	assert.Equal(t, "retrieval.corpus_registered", evs[0].Metadata["reason"])
}

// ---- Diagnose ----

func TestDiagnose_AuditCarriesNoQueryText(t *testing.T) {
	fx := newAdminFx(t, adminSpec("crm", "crm"))
	d := float32(0.5)
	fx.rt.diagnose = func(iface.RetrievalSearchRequest) (*DiagnosticResult, error) {
		return &DiagnosticResult{
			Result: iface.RetrievalSearchResult{Degraded: true, Hits: []iface.RetrievalHit{
				{SourceID: "s1", ChunkID: "c1", Rank: 1, Score: 0.9, Snippet: "snip"},
				{SourceID: "s2", ChunkID: "c2", Rank: 2, Score: 0.8},
			}},
			Collection: "p_crm_g1", Profile: "ip_x", Principals: []string{"tenant:*", "user:u1"},
			Scores: []StageScores{{ChunkID: "c1", Fused: 0.7, Dense: &d}, {ChunkID: "c2", Fused: 0.6}},
		}, nil
	}
	const secret = "salary of Mario Rossi"
	filter := &iface.RetrievalFilter{Field: "name", Op: "eq", Value: "Mario Rossi"}
	rep, err := fx.svc.Diagnose(fx.ctx, "crm", models.RetrievalDiagnosticsRequest{Query: secret, Filter: filter, Mode: iface.RetrievalModeDense, Limit: 5})
	require.NoError(t, err)

	assert.Equal(t, iface.RetrievalSearchRequest{Corpus: "crm", Query: secret, Filter: filter, Mode: iface.RetrievalModeDense, Limit: 5}, fx.rt.lastReq)
	require.Len(t, rep.Hits, 2)
	assert.Equal(t, float32(0.7), rep.Hits[0].Fused)
	assert.Equal(t, &d, rep.Hits[0].Dense)
	assert.Equal(t, float32(0.6), rep.Hits[1].Fused)
	assert.Nil(t, rep.Hits[1].Dense)
	assert.True(t, rep.Degraded)
	assert.Equal(t, "p_crm_g1", rep.Collection)

	evs := fx.sink.byAction(AuditDiagnosticsRun)
	require.Len(t, evs, 1)
	ev := evs[0]
	assert.Equal(t, "success", ev.Outcome)
	assert.Equal(t, "retrieval_corpus", ev.ResourceType)
	assert.Equal(t, "crm", ev.ResourceID)
	assert.Equal(t, map[string]any{"mode": "dense", "limit": 5, "queryRunes": len([]rune(secret)), "hits": 2, "degraded": true}, ev.Metadata)
	assert.Equal(t, "u-admin", ev.ActorUserID)
	assert.Equal(t, "tA", ev.TenantID)
	assert.NotContains(t, fmt.Sprintf("%v", ev), "Mario")
	assert.NotContains(t, fmt.Sprintf("%v", ev), "salary")
}

func TestDiagnose_FailureIsAuditedAndReturned(t *testing.T) {
	fx := newAdminFx(t, adminSpec("crm", "crm"))
	fx.rt.diagnose = func(iface.RetrievalSearchRequest) (*DiagnosticResult, error) {
		return nil, iface.ErrRetrievalForbidden
	}
	_, err := fx.svc.Diagnose(fx.ctx, "crm", models.RetrievalDiagnosticsRequest{Query: "q"})
	assert.ErrorIs(t, err, iface.ErrRetrievalForbidden)
	evs := fx.sink.byAction(AuditDiagnosticsRun)
	require.Len(t, evs, 1)
	assert.Equal(t, "failure", evs[0].Outcome)
	assert.Equal(t, map[string]any{
		"mode": "hybrid", "limit": 10, "queryRunes": 1, "hits": 0, "degraded": false, "reason": "retrieval.forbidden",
	}, evs[0].Metadata)
}

// Every pipeline refusal is audited with the code the handler answers.
func TestDiagnose_FailureReasonIsTheResponseCode(t *testing.T) {
	for err, want := range map[error]string{
		iface.ErrRetrievalUnknownCorpus:                        "retrieval.corpus_not_found",
		iface.ErrRetrievalInvalidFilter:                        "retrieval.invalid_filter",
		iface.ErrRetrievalLocality:                             "retrieval.locality_violation",
		iface.ErrRetrievalCorpusUnavailable:                    "retrieval.corpus_unavailable",
		fmt.Errorf("x: %w", iface.ErrRetrievalVisibilityCheck): "retrieval.visibility_check_failed",
		errors.New("qdrant: boom"):                             "retrieval.internal",
	} {
		fx := newAdminFx(t, adminSpec("crm", "crm"))
		fx.rt.diagnose = func(iface.RetrievalSearchRequest) (*DiagnosticResult, error) { return nil, err }
		_, got := fx.svc.Diagnose(fx.ctx, "crm", models.RetrievalDiagnosticsRequest{Query: "q"})
		require.ErrorIs(t, got, err)
		evs := fx.sink.byAction(AuditDiagnosticsRun)
		require.Len(t, evs, 1)
		assert.Equal(t, want, evs[0].Metadata["reason"], "error %v", err)
	}
}

// A non-internal tenant is refused before the pipeline runs and before any
// audit event, like the drop and the search-log read.
func TestDiagnose_RequiresInternalTenant(t *testing.T) {
	fx := newAdminFx(t, adminSpec("crm", "crm"))
	called := false
	fx.rt.diagnose = func(iface.RetrievalSearchRequest) (*DiagnosticResult, error) {
		called = true
		return &DiagnosticResult{}, nil
	}
	_, err := fx.svc.Diagnose(ctxauth.WithTenantKind(fx.ctx, "external"), "crm", models.RetrievalDiagnosticsRequest{Query: "q"})
	assert.ErrorIs(t, err, iface.ErrRetrievalForbidden)
	assert.False(t, called, "the pipeline must not run")
	assert.Empty(t, fx.sink.events)
}

// A nil relay would audit into nothing; it is a wiring bug caught at Init.
func TestNewAdminService_NilAuditPanics(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Error("NewAdminService with a nil Audit did not panic")
		}
	}()
	NewAdminService(AdminDeps{})
}

// ---- AuditRelay.EmitOutcome ----

func TestAuditRelay_EmitCarriesActorAndOutcome(t *testing.T) {
	sink := &recordingSink{}
	relay := NewAuditRelay()
	relay.SetAuditSink(sink)
	id := testkit.NewIdentity("u-1", "ann@example.test", "administrator").WithTenant("tA", []string{"org_owner"}, true)
	ctx := ctxauth.WithClientIP(ctxauth.WithTenantKind(id.ContextFor(context.Background(), "tA"), "internal"), "198.51.100.7")

	relay.EmitOutcome(ctx, "retrieval.x", "retrieval_corpus", "crm", "failure", map[string]any{"k": 1})
	relay.Emit(ctx, "retrieval.y", "retrieval_corpus", "crm", nil)

	require.Len(t, sink.events, 2)
	ev := sink.events[0]
	assert.Equal(t, "tA", ev.TenantID)
	assert.Equal(t, "internal", ev.TenantKind)
	assert.Equal(t, "u-1", ev.ActorUserID)
	assert.Equal(t, "ann@example.test", ev.ActorEmail)
	assert.Equal(t, "198.51.100.7", ev.IPAddress)
	assert.Equal(t, "user", ev.ActorType)
	assert.Equal(t, "failure", ev.Outcome)
	assert.Equal(t, "success", sink.events[1].Outcome)
}

// ---- real Mongo: the drop over the production repositories ----

type adminMongoStore struct {
	*repository.AdminStore
	states   *repository.CorpusStates
	progress *repository.TenantProgress
}

func (s adminMongoStore) CorpusState(ctx context.Context, n string) (*models.CorpusState, error) {
	return s.states.Get(ctx, n)
}
func (s adminMongoStore) CorpusStateList(ctx context.Context) ([]models.CorpusState, error) {
	return s.states.List(ctx)
}
func (s adminMongoStore) TenantBuild(ctx context.Context, c string, g int) (*models.TenantProgress, error) {
	return s.progress.Get(ctx, c, g)
}

func TestDropOrphan_MongoStoreAlreadyGone(t *testing.T) {
	db := newServicesMongoDB(t)
	st := adminMongoStore{repository.NewAdminStore(db), repository.NewCorpusStates(db), repository.NewTenantProgress(db)}
	fx := newAdminFx(t)
	fx.svc.store = st
	ctx := fx.ctx

	seed := &models.CorpusState{Corpus: "old", Owner: "gone", Status: models.CorpusStatusActive, ActiveGeneration: 1, Locality: "local_only"}
	require.NoError(t, st.states.Insert(ctx, seed))
	list, err := fx.svc.Corpora(ctx)
	require.NoError(t, err)
	require.Len(t, list.Corpora, 1)
	assert.False(t, list.Corpora[0].Registered)

	require.NoError(t, fx.svc.DropOrphanCorpus(ctx, "old"))
	got, err := st.states.Get(ctx, "old")
	require.NoError(t, err)
	assert.Nil(t, got)

	// retried drop: the state is already gone
	assert.ErrorIs(t, fx.svc.DropOrphanCorpus(ctx, "old"), ErrCorpusNotFound)
	// and the raw compare-and-delete on a gone row is a conflict, which the
	// service resolves by re-reading (covered with the fake above)
	assert.ErrorIs(t, st.DeleteCorpusState(ctx, "old", 1), repository.ErrStateConflict)
}

// ---- SearchLog ----

func TestSearchLog_EmitsOneAuditEventEvenWhenEmpty(t *testing.T) {
	fx := newAdminFx(t)
	page, err := fx.svc.SearchLog(fx.ctx, models.SearchLogQuery{})
	require.NoError(t, err)
	assert.NotNil(t, page.Items)
	assert.Empty(t, page.Items)
	assert.Equal(t, int64(0), page.Total)
	assert.Equal(t, 50, page.Limit)

	evs := fx.sink.byAction(AuditSearchLogRead)
	require.Len(t, evs, 1)
	assert.Len(t, fx.sink.events, 1)
	assert.Equal(t, "retrieval_search_log", evs[0].ResourceType)
	assert.Equal(t, "tA", evs[0].ResourceID)
	assert.Equal(t, "success", evs[0].Outcome)
	assert.Equal(t, 0, evs[0].Metadata["returned"])
	assert.Equal(t, 50, evs[0].Metadata["limit"])
}

func TestSearchLog_Validation(t *testing.T) {
	fx := newAdminFx(t)
	now := time.Now()
	later := now.Add(time.Hour)
	for name, q := range map[string]models.SearchLogQuery{
		"since after until": {Since: &later, Until: &now},
		"limit too big":     {Limit: 500},
		"limit negative":    {Limit: -1},
		"offset too big":    {Offset: 10001},
		"offset negative":   {Offset: -1},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := fx.svc.SearchLog(fx.ctx, q)
			assert.ErrorIs(t, err, iface.ErrRetrievalInvalidRequest)
		})
	}
	assert.Empty(t, fx.store.logCalls)
	assert.Empty(t, fx.sink.events)

	_, err := fx.svc.SearchLog(fx.ctx, models.SearchLogQuery{Limit: 200, Offset: 10000})
	require.NoError(t, err)
	_, err = fx.svc.SearchLog(fx.ctx, models.SearchLogQuery{})
	require.NoError(t, err)
	require.Len(t, fx.store.logCalls, 2)
	assert.Equal(t, 50, fx.store.logCalls[1].Limit)
}

func TestSearchLog_RequiresInternalTenant(t *testing.T) {
	fx := newAdminFx(t)
	_, err := fx.svc.SearchLog(ctxauth.WithTenantKind(fx.ctx, "external"), models.SearchLogQuery{})
	assert.ErrorIs(t, err, iface.ErrRetrievalForbidden)
	assert.Empty(t, fx.store.logCalls)
	assert.Empty(t, fx.sink.events)
}

func TestSearchLog_AuditHasNoRowContent(t *testing.T) {
	fx := newAdminFx(t)
	fx.store.logRows = []models.SearchLog{{
		UUID: "l1", TenantID: "tA", UserUUID: "u-9", Corpus: "crm", Query: "mario rossi",
		FilterJSON: `{"field":"city","op":"eq","value":"roma"}`, Subjects: []string{"subj-1"}, SourceIDs: []string{"src-1"},
		CreatedAt: time.Unix(1700000000, 0).UTC(),
	}}
	fx.store.logTotal = 1
	since := time.Unix(1699000000, 0).UTC()
	page, err := fx.svc.SearchLog(fx.ctx, models.SearchLogQuery{UserUUID: "u-9", Since: &since})
	require.NoError(t, err)
	require.Len(t, page.Items, 1)
	assert.Equal(t, "mario rossi", page.Items[0].Query)

	evs := fx.sink.byAction(AuditSearchLogRead)
	require.Len(t, evs, 1)
	assert.Equal(t, 1, evs[0].Metadata["returned"])
	assert.Equal(t, "u-9", evs[0].Metadata["userUuid"])
	assert.Equal(t, "", evs[0].Metadata["until"])
	for _, v := range evs[0].Metadata {
		s := fmt.Sprint(v)
		for _, secret := range []string{"mario", "rossi", "roma", "subj-1", "src-1", "city"} {
			assert.NotContains(t, s, secret)
		}
	}
}

func TestSearchLog_StoreErrorIsAuditedAsFailure(t *testing.T) {
	fx := newAdminFx(t)
	fx.store.logErr = errors.New("boom")
	_, err := fx.svc.SearchLog(fx.ctx, models.SearchLogQuery{})
	require.Error(t, err)
	evs := fx.sink.byAction(AuditSearchLogRead)
	require.Len(t, evs, 1)
	assert.Equal(t, "failure", evs[0].Outcome)
	assert.Equal(t, 0, evs[0].Metadata["returned"])
	assert.Equal(t, "retrieval.internal", evs[0].Metadata["reason"])
}

func TestAdminSearchLog_FilterKeepsLargeIntegersExact(t *testing.T) {
	fx := newAdminFx(t)
	fx.store.logRows = []models.SearchLog{{
		UUID: "l1", TenantID: "tA", FilterJSON: `{"field":"seniority","op":"eq","value":9007199254740993}`,
	}}
	page, err := fx.svc.SearchLog(fx.ctx, models.SearchLogQuery{})
	require.NoError(t, err)
	out, err := json.Marshal(page)
	require.NoError(t, err)
	assert.Contains(t, string(out), "9007199254740993")
	// The text the console displays is the stored string, byte for byte.
	assert.Equal(t, `{"field":"seniority","op":"eq","value":9007199254740993}`, page.Items[0].FilterText)
}

func TestAdminSearchLog_BadFilterIsOmittedWithoutLeaking(t *testing.T) {
	fx := newAdminFx(t)
	var buf strings.Builder
	fx.svc.log = slog.New(slog.NewTextHandler(&buf, nil))
	fx.store.logRows = []models.SearchLog{
		{UUID: "l1", TenantID: "tA", Corpus: "crm", FilterJSON: `{"value":"secret-val"`},
		{UUID: "l2", TenantID: "tA", Corpus: "crm", FilterJSON: `{"a":1} {"b":"secret-val"}`},
		{UUID: "l3", TenantID: "tA", Corpus: "crm"},
	}
	page, err := fx.svc.SearchLog(fx.ctx, models.SearchLogQuery{})
	require.NoError(t, err)
	require.Len(t, page.Items, 3)
	for _, it := range page.Items {
		assert.Nil(t, it.Filter)
		assert.Equal(t, "", it.FilterText) // both or neither
		assert.NotNil(t, it.Principals)
	}
	assert.Equal(t, 2, strings.Count(buf.String(), "level=WARN"))
	assert.NotContains(t, buf.String(), "secret-val")
	assert.Contains(t, buf.String(), "tenant=tA")
	assert.Contains(t, buf.String(), "corpus=crm")
}
