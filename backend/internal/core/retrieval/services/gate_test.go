package services

import (
	"context"
	"io"
	"log/slog"
	"testing"

	"github.com/orkestra/backend/internal/core/retrieval/engine"
	"github.com/orkestra/backend/internal/core/retrieval/models"
	"github.com/orkestra/backend/pkg/sdk/iface"
)

type gateEnv struct {
	w        *Worker
	eng      *fakeEngine
	states   *fakeStates
	profiles *fakeProfiles
	modules  *fakeModules
	spec     iface.RetrievalCorpusSpec
}

func newGateEnv() *gateEnv {
	spec := iface.RetrievalCorpusSpec{Name: "crm_contacts", Owner: "crm",
		Filters:   []iface.RetrievalFilterField{{Name: "stage", Type: "keyword"}},
		Ingestion: goldenProfile()}
	e := &gateEnv{
		eng:      newFakeEngine(),
		states:   newFakeStates(),
		profiles: &fakeProfiles{profile: &iface.InferenceProfile{ID: "ip_a", Kind: iface.InferenceKindEmbed, Boundary: iface.InferenceBoundaryLocal, Dimensions: 8}},
		modules:  &fakeModules{started: map[string]bool{"crm": true}},
		spec:     spec,
	}
	clk := newClock()
	e.w = NewWorker(WorkerDeps{
		States: e.states, Catalog: &fakeCatalog{specs: []iface.RetrievalCorpusSpec{spec}},
		Engine: e.eng, Profiles: e.profiles, Modules: e.modules,
		Prefix: "orkestra", Now: clk.Now, Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	return e
}

func (e *gateEnv) prepare(t *testing.T) (*corpusGate, string) {
	t.Helper()
	g, problem, err := e.w.prepareCorpus(context.Background(), e.spec)
	require.NoError(t, err)
	return g, problem
}

func TestGate_BootstrapCreatesGenerationOne(t *testing.T) {
	e := newGateEnv()
	g, problem := e.prepare(t)
	require.NotNil(t, g)
	assert.Empty(t, problem)
	require.Len(t, e.eng.specs, 1)
	assert.Equal(t, engine.CollectionSpec{Name: "orkestra_crm_contacts_g1", Dimensions: 8,
		Fields: e.spec.Filters, Analyzer: e.spec.Ingestion.Analyzer}, e.eng.specs[0])
	st := e.states.get("crm_contacts")
	assert.Equal(t, models.CorpusStatusActive, st.Status)
	assert.Equal(t, 1, st.ActiveGeneration)
	assert.Equal(t, "orkestra_crm_contacts_g1", st.Collection)
	assert.Equal(t, "ip_a", st.EmbeddingProfileID)
	assert.Equal(t, 8, st.Dimensions)
	assert.Equal(t, "crm", st.Owner)
	assert.Equal(t, IngestionProfileHash(e.spec.Ingestion, "ip_a"), st.IngestionProfileHash)
	assert.Equal(t, st.IngestionProfileHash, g.ProfileHash)

	// a second pass is idempotent
	g, problem = e.prepare(t)
	require.NotNil(t, g)
	assert.Empty(t, problem)
	assert.Equal(t, 1, e.eng.callCount("EnsureCollection"))
}

func TestGate_BootstrapEngineDown(t *testing.T) {
	e := newGateEnv()
	e.eng.setEnsureErr(engine.ErrUnavailable)
	g, problem := e.prepare(t)
	assert.Nil(t, g)
	assert.Equal(t, "corpus crm_contacts: vector engine unavailable", problem)
	assert.Equal(t, 0, e.states.writes)
}

func TestGate_LocalOnlyRefusesHostedEndpoint(t *testing.T) {
	e := newGateEnv()
	e.profiles.update(func(p *iface.InferenceProfile) {
		p.Boundary = iface.InferenceBoundaryHosted
		p.Endpoint = "embed"
	})
	g, problem := e.prepare(t)
	assert.Nil(t, g)
	assert.Equal(t, `corpus crm_contacts: local_only corpus uses hosted endpoint "embed"`, problem)
	assert.Equal(t, 0, e.eng.callCount("EnsureCollection"))
	st := e.states.get("crm_contacts")
	assert.Equal(t, models.CorpusStatusError, st.Status)
	assert.Equal(t, models.ReasonLocality, st.Reason)
	assert.Equal(t, 0, st.ActiveGeneration)

	e.spec.Locality = iface.RetrievalHostedAllowed
	g, problem = e.prepare(t)
	require.NotNil(t, g)
	assert.Empty(t, problem)
	st = e.states.get("crm_contacts")
	assert.Equal(t, models.CorpusStatusActive, st.Status)
	assert.Equal(t, 1, st.ActiveGeneration)
	assert.Empty(t, st.Reason)
	assert.Equal(t, string(iface.RetrievalHostedAllowed), st.Locality)
}

func TestGate_ProfileChangedKeepsTheActiveGeneration(t *testing.T) {
	e := newGateEnv()
	g, _ := e.prepare(t)
	require.NotNil(t, g)
	oldHash := g.ProfileHash

	old := e.spec
	e.spec.Ingestion.Chunking = iface.RetrievalChunking{Strategy: "fixed", MaxTokens: 128, Overlap: 16}
	newHash := IngestionProfileHash(e.spec.Ingestion, "ip_a")
	g, problem := e.prepare(t)
	assert.Nil(t, g)
	assert.Contains(t, problem, "ingestion profile changed")
	st := e.states.get("crm_contacts")
	assert.Equal(t, models.CorpusStatusProfileChanged, st.Status)
	assert.Equal(t, newHash, st.DeclaredProfileHash)
	assert.Equal(t, oldHash, st.IngestionProfileHash)
	assert.Equal(t, 1, st.ActiveGeneration)
	assert.Equal(t, "orkestra_crm_contacts_g1", st.Collection)
	assert.Equal(t, 1, e.eng.callCount("EnsureCollection"))

	// a repeated pass does not write again
	w := e.states.writes
	_, _ = e.prepare(t)
	assert.Equal(t, w, e.states.writes)

	e.spec = old
	g, problem = e.prepare(t)
	require.NotNil(t, g)
	assert.Empty(t, problem)
	st = e.states.get("crm_contacts")
	assert.Equal(t, models.CorpusStatusActive, st.Status)
	assert.Empty(t, st.DeclaredProfileHash)
}

func TestGate_OwnerNotStartedTouchesNothing(t *testing.T) {
	e := newGateEnv()
	e.modules.setStarted("crm", false)
	g, problem := e.prepare(t)
	assert.Nil(t, g)
	assert.Empty(t, problem)
	assert.Equal(t, 0, e.profiles.callCount())
	assert.Equal(t, 0, e.states.writes)
	assert.Empty(t, e.eng.callLog())
}

func TestGate_TransientProfileErrorWritesNothing(t *testing.T) {
	for name, tc := range map[string]struct {
		err  error
		dims int
	}{
		"unavailable": {iface.ErrLLMProviderUnavailable, 8},
		"mismatch":    {iface.ErrLLMProfileMismatch, 8},
		"no dims":     {nil, 0},
	} {
		t.Run(name, func(t *testing.T) {
			e := newGateEnv()
			e.profiles.setErr(tc.err)
			e.profiles.update(func(p *iface.InferenceProfile) { p.Dimensions = tc.dims })
			g, problem := e.prepare(t)
			assert.Nil(t, g)
			assert.Equal(t, "corpus crm_contacts: inference profile unavailable", problem)
			assert.Equal(t, 0, e.states.writes)
			assert.Empty(t, e.eng.callLog())
		})
	}
}

func TestGate_WrongKindPersistsError(t *testing.T) {
	e := newGateEnv()
	e.profiles.update(func(p *iface.InferenceProfile) { p.Kind = iface.InferenceKindChat })
	g, problem := e.prepare(t)
	assert.Nil(t, g)
	assert.Equal(t, `corpus crm_contacts: endpoint "embed" is not an embedding endpoint`, problem)
	st := e.states.get("crm_contacts")
	assert.Equal(t, models.CorpusStatusError, st.Status)
	assert.Equal(t, models.ReasonEndpointKind, st.Reason)
}

func TestGate_UnknownEndpointPersistsErrorAndRecovers(t *testing.T) {
	e := newGateEnv()
	e.profiles.setErr(iface.ErrLLMNotConfigured)
	g, problem := e.prepare(t)
	assert.Nil(t, g)
	assert.Equal(t, `corpus crm_contacts: embed endpoint "embed" is not configured`, problem)
	st := e.states.get("crm_contacts")
	assert.Equal(t, models.CorpusStatusError, st.Status)
	assert.Equal(t, models.ReasonEndpointUnknown, st.Reason)

	e.profiles.setErr(nil)
	g, problem = e.prepare(t)
	require.NotNil(t, g)
	assert.Empty(t, problem)
	st = e.states.get("crm_contacts")
	assert.Equal(t, models.CorpusStatusActive, st.Status)
	assert.Equal(t, 1, st.ActiveGeneration)
	assert.Empty(t, st.Reason)
}
