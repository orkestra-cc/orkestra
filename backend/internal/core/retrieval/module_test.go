package retrieval

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/orkestra/backend/internal/core/retrieval/engine"
	"github.com/orkestra/backend/internal/core/retrieval/engine/qdrant"
	"github.com/orkestra/backend/internal/core/retrieval/repository"
	"github.com/orkestra/backend/internal/core/retrieval/services"
	"github.com/orkestra/backend/pkg/sdk/ctxauth"
	"github.com/orkestra/backend/pkg/sdk/iface"
	"github.com/orkestra/backend/pkg/sdk/module"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

type stubSource struct{}

func (stubSource) Load(context.Context, string, []string) ([]iface.RetrievalLoaded, error) {
	return nil, nil
}
func (stubSource) Enumerate(context.Context, string, string, int) (iface.RetrievalPage, error) {
	return iface.RetrievalPage{}, nil
}
func (stubSource) Visible(context.Context, string, iface.RetrievalPrincipalContext, []string) (map[string]iface.RetrievalVisibility, error) {
	return nil, nil
}

func validSpec() iface.RetrievalCorpusSpec {
	return iface.RetrievalCorpusSpec{
		Name:       "contacts",
		Owner:      "crm",
		Permission: "crm.contact.read",
		Filters: []iface.RetrievalFilterField{
			{Name: "status", Type: iface.RetrievalFieldKeyword, Description: "Contact status"},
		},
		Ingestion: iface.RetrievalIngestionProfile{
			EmbedEndpoint: "embed",
			Chunking:      iface.RetrievalChunking{Strategy: "none"},
		},
		Source: stubSource{},
	}
}

func initModule(t *testing.T) (*Module, *module.ServiceRegistry, *bytes.Buffer) {
	t.Helper()
	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug}))
	reg := module.NewServiceRegistry()
	reg.Register(module.ServiceLLMGateway, fullGateway{})
	reg.Register(module.ServiceTenantService, iface.TenantIDLister(fakeTenants{}))
	reg.Register(module.ServiceModuleStatus, module.ModuleStatusReader(fakeStatus{}))
	m := NewModule()
	if err := m.Init(&module.Dependencies{DB: offlineDB(t), Logger: logger, Services: reg}); err != nil {
		t.Fatalf("Init: %v", err)
	}
	return m, reg, &buf
}

func service(t *testing.T, reg *module.ServiceRegistry) *services.Service {
	t.Helper()
	if _, ok := module.GetTyped[iface.RetrievalCorpusRegistry](reg, module.ServiceRetrieval); !ok {
		t.Fatal("ServiceRetrieval is not a RetrievalCorpusRegistry")
	}
	svc, ok := module.GetTyped[*services.Service](reg, module.ServiceRetrieval)
	if !ok {
		t.Fatal("ServiceRetrieval is not a *services.Service")
	}
	return svc
}

func TestModule_Declarations(t *testing.T) {
	m := NewModule()
	if m.Name() != "retrieval" || m.Category() != module.CategoryCore {
		t.Fatalf("name/category = %q/%v", m.Name(), m.Category())
	}
	if got := m.Dependencies(); !slices.Equal(got, []string{"tenant", "authz", "llm"}) {
		t.Fatalf("dependencies = %v", got)
	}
	if got := m.ProvidedServices(); !slices.Equal(got, []module.ServiceKey{module.ServiceRetrieval}) {
		t.Fatalf("provided = %v", got)
	}
	cols := m.Collections()
	if len(cols) != 4 || cols[0].Name != "retrieval_corpus_state" || repository.CollCorpusState != "retrieval_corpus_state" ||
		cols[1].Name != "retrieval_outbox" || repository.CollOutbox != "retrieval_outbox" ||
		cols[2].Name != "retrieval_tenant_progress" || repository.CollTenantProgress != "retrieval_tenant_progress" ||
		cols[3].Name != "retrieval_erasures" || repository.CollErasures != "retrieval_erasures" {
		t.Fatalf("collections = %v", cols)
	}
	if p := module.PermissionsOf(m); len(p) != 0 {
		t.Fatalf("permissions = %v", p)
	}
	if c := module.ConfigSchemaOf(m); len(c) != 3 {
		t.Fatalf("config schema = %v", c)
	}
}

// Every index leads with tenantId except the declared system sweeps (cross-
// tenant by design, //tenantscope:allow system:). Sweeps carry unique=false
// unless a later collection (global tombstones) declares otherwise.
func TestModule_IndexesLeadWithTenantOrAreDeclaredSweeps(t *testing.T) {
	type sweep struct {
		fields []string
		unique bool
	}
	sweeps := map[string][]sweep{
		repository.CollOutbox:   {{[]string{"dirty", "nextAttemptAt"}, false}, {[]string{"leaseUntil"}, false}},
		repository.CollErasures: {{[]string{"uuid"}, true}, {[]string{"scope", "kind", "subject"}, false}},
	}
	for _, c := range NewModule().Collections() {
		for _, idx := range c.Indexes {
			if len(idx.OrderedKeys) == 0 {
				t.Errorf("%s index %+v has no ordered keys", c.Name, idx)
				continue
			}
			if idx.OrderedKeys[0].Field == "tenantId" {
				continue
			}
			var fields []string
			for _, k := range idx.OrderedKeys {
				fields = append(fields, k.Field)
			}
			allowed := false
			for _, s := range sweeps[c.Name] {
				if slices.Equal(s.fields, fields) && s.unique == idx.Unique {
					allowed = true
				}
			}
			if !allowed {
				t.Errorf("%s index %v does not lead with tenantId and is not a declared system sweep", c.Name, fields)
			}
		}
	}
	var got []string
	for _, c := range NewModule().Collections() {
		if c.Name == repository.CollOutbox {
			for _, idx := range c.Indexes {
				if idx.Unique {
					for _, k := range idx.OrderedKeys {
						got = append(got, k.Field)
					}
				}
			}
		}
	}
	if !slices.Equal(got, []string{"tenantId", "corpus", "sourceId"}) {
		t.Errorf("outbox unique index = %v", got)
	}
	got = nil
	for _, c := range NewModule().Collections() {
		if c.Name == repository.CollTenantProgress {
			for _, idx := range c.Indexes {
				if idx.Unique {
					for _, k := range idx.OrderedKeys {
						got = append(got, k.Field)
					}
				}
			}
		}
	}
	if !slices.Equal(got, []string{"tenantId", "corpus", "generation"}) {
		t.Errorf("tenant progress unique index = %v", got)
	}
}

func TestModule_InitWithoutQdrantURL(t *testing.T) {
	t.Setenv("QDRANT_URL", "")
	m, reg, logs := initModule(t)
	svc := service(t, reg)
	if _, err := svc.Engine(); !errors.Is(err, iface.ErrRetrievalNotConfigured) {
		t.Fatalf("Engine() err = %v", err)
	}
	if err := m.HealthCheck(context.Background()); err != nil {
		t.Fatalf("HealthCheck = %v, want nil when not configured", err)
	}
	if !strings.Contains(logs.String(), "retrieval: QDRANT_URL is not set; search answers not configured until the retrieval overlay is enabled") {
		t.Fatalf("missing INFO log: %s", logs)
	}
}

func TestModule_InitNeverContactsTheEngine(t *testing.T) {
	var n atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { n.Add(1) }))
	defer srv.Close()
	t.Setenv("QDRANT_URL", srv.URL)
	t.Setenv("QDRANT_API_KEY", "k-SECRET")
	_, reg, logs := initModule(t)
	if _, err := service(t, reg).Engine(); err != nil {
		t.Fatalf("Engine() = %v", err)
	}
	if n.Load() != 0 {
		t.Fatalf("Init made %d requests", n.Load())
	}
	if strings.Contains(logs.String(), "k-SECRET") {
		t.Fatalf("logs leak the key: %s", logs)
	}
}

func TestModule_InitMalformedURLWarns(t *testing.T) {
	t.Setenv("QDRANT_URL", "ftp://user:pw@host")
	t.Setenv("QDRANT_API_KEY", "k")
	_, reg, logs := initModule(t)
	if _, err := service(t, reg).Engine(); !errors.Is(err, iface.ErrRetrievalNotConfigured) {
		t.Fatalf("Engine() err = %v", err)
	}
	out := logs.String()
	if !strings.Contains(out, "level=WARN") || !strings.Contains(out, "retrieval: QDRANT_URL is not a valid http(s) URL; the vector engine is treated as not configured") {
		t.Fatalf("missing WARN: %s", out)
	}
	if strings.Contains(out, "pw@host") {
		t.Fatalf("logs leak the URL: %s", out)
	}
}

func TestModule_InitMissingKeyWarns(t *testing.T) {
	t.Setenv("QDRANT_URL", "http://qdrant.invalid:6333")
	t.Setenv("QDRANT_API_KEY", "")
	_, _, logs := initModule(t)
	if !strings.Contains(logs.String(), "level=WARN") || !strings.Contains(logs.String(), "retrieval: QDRANT_API_KEY is not set") {
		t.Fatalf("missing WARN: %s", logs)
	}
}

func TestModule_InitBadPrefixFallsBack(t *testing.T) {
	t.Setenv("QDRANT_URL", "")
	t.Setenv("QDRANT_COLLECTION_PREFIX", "Bad Prefix")
	_, reg, logs := initModule(t)
	if got := service(t, reg).CollectionPrefix(); got != "orkestra" {
		t.Fatalf("prefix = %q", got)
	}
	if !strings.Contains(logs.String(), "level=WARN") {
		t.Fatalf("missing WARN: %s", logs)
	}
	t.Setenv("QDRANT_COLLECTION_PREFIX", " acme-1 ")
	_, reg, _ = initModule(t)
	if got := service(t, reg).CollectionPrefix(); got != "acme-1" {
		t.Fatalf("prefix = %q", got)
	}
}

// An underscore in the prefix would make collection names ambiguous: prefix
// "a_b" with corpus "c" and prefix "a" with corpus "b_c" both give "a_b_c_g1".
func TestModule_InitPrefixRefusesUnderscore(t *testing.T) {
	t.Setenv("QDRANT_URL", "")
	t.Setenv("QDRANT_COLLECTION_PREFIX", "a_b")
	_, reg, logs := initModule(t)
	if got := service(t, reg).CollectionPrefix(); got != "orkestra" {
		t.Fatalf("prefix = %q, want the default", got)
	}
	if !strings.Contains(logs.String(), "level=WARN") {
		t.Fatalf("missing WARN: %s", logs)
	}
}

func TestModule_HealthCheckUnavailable(t *testing.T) {
	status := http.StatusServiceUnavailable
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if status != http.StatusOK {
			w.WriteHeader(status)
			return
		}
		if r.URL.Path == "/" {
			_, _ = w.Write([]byte(`{"title":"qdrant","version":"1.20.0"}`))
			return
		}
		_, _ = w.Write([]byte(`{"result":{"collections":[]}}`))
	}))
	defer srv.Close()
	t.Setenv("QDRANT_URL", srv.URL)
	t.Setenv("QDRANT_API_KEY", "k-SECRET")
	m, _, logs := initModule(t)

	err := m.HealthCheck(context.Background())
	if !errors.Is(err, iface.ErrRetrievalUnavailable) {
		t.Fatalf("err = %v", err)
	}
	if msg := err.Error(); strings.Contains(msg, srv.URL) || strings.Contains(msg, "k-SECRET") || strings.Contains(msg, "503") {
		t.Fatalf("error leaks driver detail: %q", msg)
	}
	if strings.Contains(logs.String(), "k-SECRET") {
		t.Fatalf("logs leak the key: %s", logs)
	}

	status = http.StatusOK
	if err := m.HealthCheck(context.Background()); err != nil {
		t.Fatalf("healthy HealthCheck = %v", err)
	}
}

func TestModule_StartSealsRegistration(t *testing.T) {
	t.Setenv("QDRANT_URL", "")
	m, reg, _ := initModule(t)
	svc := service(t, reg)
	if err := svc.RegisterCorpus(validSpec()); err != nil {
		t.Fatalf("RegisterCorpus before Start: %v", err)
	}
	if err := m.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	other := validSpec()
	other.Name = "orders"
	err := svc.RegisterCorpus(other)
	if err == nil || !strings.Contains(err.Error(), "registration is closed") {
		t.Fatalf("RegisterCorpus after Start err = %v", err)
	}
	if err := m.Start(context.Background()); err != nil {
		t.Fatalf("second Start: %v", err)
	}
	if err := m.Stop(context.Background()); err != nil {
		t.Fatalf("Stop: %v", err)
	}
}

// ---- indexing wiring ----

// fakeEngine records the calls it receives; everything succeeds unless
// healthErr is set.
type fakeEngine struct {
	mu        sync.Mutex
	calls     []string
	healthErr error
}

var _ engine.Engine = (*fakeEngine)(nil)

func (f *fakeEngine) record(name string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, name)
}

func (f *fakeEngine) callLog() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.calls)
}

func (f *fakeEngine) setHealthErr(err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.healthErr = err
}

func (f *fakeEngine) Health(context.Context) error {
	f.record("Health")
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.healthErr
}
func (f *fakeEngine) EnsureCollection(context.Context, engine.CollectionSpec) error {
	f.record("EnsureCollection")
	return nil
}
func (f *fakeEngine) DropCollection(context.Context, string) error {
	f.record("DropCollection")
	return nil
}
func (f *fakeEngine) Upsert(context.Context, string, []engine.Point) error {
	f.record("Upsert")
	return nil
}
func (f *fakeEngine) UpdatePayload(context.Context, string, []engine.Point) error {
	f.record("UpdatePayload")
	return nil
}
func (f *fakeEngine) DeleteOtherRevisions(context.Context, string, string, string, int64) error {
	f.record("DeleteOtherRevisions")
	return nil
}
func (f *fakeEngine) DeleteByFilter(context.Context, string, engine.Selector) error {
	f.record("DeleteByFilter")
	return nil
}
func (f *fakeEngine) Count(context.Context, string, engine.Selector) (int64, error) {
	f.record("Count")
	return 0, nil
}
func (f *fakeEngine) Query(context.Context, string, engine.Query) ([]engine.Candidate, error) {
	f.record("Query")
	return nil, nil
}

// fakeProfiles serves a hosted embedding profile named "embed".
type fakeProfiles struct{}

func (fakeProfiles) InferenceProfile(context.Context, string) (*iface.InferenceProfile, error) {
	return &iface.InferenceProfile{ID: "ip_a", Endpoint: "embed", Kind: iface.InferenceKindEmbed,
		Boundary: iface.InferenceBoundaryHosted, Dimensions: 8}, nil
}

type fakeEmbedder struct{}

func (fakeEmbedder) EmbedWithProfile(context.Context, iface.PinnedEmbedRequest) (*iface.EmbedResult, error) {
	return nil, errors.New("not used")
}

// fullGateway is a gateway with both indexing capabilities; profilesOnly has
// no pinned embedder.
type fullGateway struct {
	fakeProfiles
	fakeEmbedder
}
type profilesOnly struct{ fakeProfiles }

type fakeTenants struct{}

func (fakeTenants) ListActiveTenantIDs(context.Context, string) ([]string, error) { return nil, nil }

type fakeStatus struct{}

func (fakeStatus) IsStarted(string) bool { return true }

func offlineDB(t *testing.T) *mongo.Database {
	t.Helper()
	client, err := mongo.Connect(context.Background(), options.Client().ApplyURI("mongodb://127.0.0.1:1").SetServerSelectionTimeout(10*time.Millisecond))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = client.Disconnect(context.Background()) })
	return client.Database("retrieval_module_test")
}

func mongoDB(t *testing.T) *mongo.Database {
	t.Helper()
	uri := os.Getenv("MONGO_TEST_URI")
	if uri == "" {
		t.Skip("MONGO_TEST_URI not set")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	client, err := mongo.Connect(ctx, options.Client().ApplyURI(uri))
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	db := client.Database(fmt.Sprintf("retrieval_module_test_%d", time.Now().UnixNano()))
	t.Cleanup(func() {
		_ = db.Drop(context.Background())
		_ = client.Disconnect(context.Background())
	})
	return db
}

// initIndexing initializes the module with a fake engine behind the
// newEngine seam and the given gateway.
func initIndexing(t *testing.T, db *mongo.Database, gateway any) (*Module, *module.ServiceRegistry, *bytes.Buffer, *fakeEngine) {
	t.Helper()
	t.Setenv("QDRANT_URL", "http://qdrant.invalid:6333")
	t.Setenv("QDRANT_API_KEY", "k")
	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug}))
	reg := module.NewServiceRegistry()
	if gateway != nil {
		reg.Register(module.ServiceLLMGateway, gateway)
	}
	reg.Register(module.ServiceTenantService, iface.TenantIDLister(fakeTenants{}))
	reg.Register(module.ServiceModuleStatus, module.ModuleStatusReader(fakeStatus{}))
	fe := &fakeEngine{}
	m := NewModule()
	m.newEngine = func(qdrant.Config) (engine.Engine, error) { return fe, nil }
	if err := m.Init(&module.Dependencies{DB: db, Logger: logger, Services: reg}); err != nil {
		t.Fatalf("Init: %v", err)
	}
	return m, reg, &buf, fe
}

func TestModule_ConfigSchemaDeclaresIndexingKeys(t *testing.T) {
	m := NewModule()
	if err := module.ValidateConfigDeclarations(m.ConfigSchema(), m.ConfigGroups()); err != nil {
		t.Fatal(err)
	}
	groups := m.ConfigGroups()
	if len(groups) != 1 || groups[0].Key != "indexing" || groups[0].Label != "Indexing" ||
		groups[0].Description != "Outbox worker: lease per key, keys per pass and pass interval." {
		t.Fatalf("groups = %+v", groups)
	}
	want := map[string]struct {
		typ         module.ConfigFieldType
		def, env    string
		description string
	}{
		"worker_lease": {module.FieldDuration, "2m", "RETRIEVAL_WORKER_LEASE",
			"How long a worker owns one item while indexing it (30s–15m). Embedding and engine calls end a quarter of the lease before it expires."},
		"worker_batch": {module.FieldInt, "32", "RETRIEVAL_WORKER_BATCH",
			"Items indexed per pass, shared round-robin across organizations."},
		"worker_poll_interval": {module.FieldDuration, "5s", "RETRIEVAL_WORKER_POLL_INTERVAL",
			"Pause between passes (1s–5m)."},
	}
	schema := m.ConfigSchema()
	if len(schema) != len(want) {
		t.Fatalf("schema has %d fields", len(schema))
	}
	for _, f := range schema {
		w, ok := want[f.Key]
		if !ok {
			t.Fatalf("unexpected key %q", f.Key)
		}
		if f.Type != w.typ || f.Default != w.def || f.EnvVar != w.env || f.Description != w.description || f.Group != "indexing" {
			t.Errorf("%s = %+v", f.Key, f)
		}
		if f.Key == "worker_batch" && (f.Min == nil || *f.Min != 1 || f.Max == nil || *f.Max != 256) {
			t.Errorf("worker_batch bounds = %v..%v", f.Min, f.Max)
		}
	}
}

func TestModule_InitTouchesNoEngine(t *testing.T) {
	_, _, _, fe := initIndexing(t, offlineDB(t), fullGateway{})
	if calls := fe.callLog(); len(calls) != 0 {
		t.Fatalf("Init called the engine: %v", calls)
	}
}

func TestModule_RegisteredServiceIsARetrievalOutbox(t *testing.T) {
	db := mongoDB(t)
	_, reg, _, _ := initIndexing(t, db, fullGateway{})
	ob, ok := module.GetTyped[iface.RetrievalOutbox](reg, module.ServiceRetrieval)
	if !ok {
		t.Fatal("ServiceRetrieval is not a RetrievalOutbox")
	}
	if err := service(t, reg).RegisterCorpus(validSpec()); err != nil {
		t.Fatal(err)
	}
	ctx := ctxauth.WithTenantKind(context.WithValue(context.Background(), ctxauth.KeyTenantID, "t1"), "internal")
	if err := ob.Enqueue(ctx, iface.RetrievalIntent{Corpus: "contacts", SourceID: "c1", Revision: 1}); err != nil {
		t.Fatalf("Enqueue: %v", err)
	}
	//tenantscope:allow system: test assertion counts rows directly
	n, err := db.Collection(repository.CollOutbox).CountDocuments(context.Background(), bson.M{"tenantId": "t1", "sourceId": "c1"})
	if err != nil || n != 1 {
		t.Fatalf("outbox rows = %d, err %v", n, err)
	}
}

func TestModule_StartWithoutEngineRunsNoWorker(t *testing.T) {
	t.Setenv("QDRANT_URL", "")
	m, _, logs := initModule(t)
	if m.worker != nil {
		t.Fatal("worker built without an engine")
	}
	if err := m.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if err := m.HealthCheck(context.Background()); err != nil {
		t.Fatalf("HealthCheck = %v", err)
	}
	if strings.Contains(logs.String(), "level=WARN") {
		t.Fatalf("unexpected WARN: %s", logs)
	}
	if err := m.Stop(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestModule_InitWarnsWhenTheGatewayLacksPinnedEmbeddings(t *testing.T) {
	m, _, logs, _ := initIndexing(t, offlineDB(t), profilesOnly{})
	if m.worker != nil {
		t.Fatal("worker built without a pinned embedder")
	}
	out := logs.String()
	if n := strings.Count(out, "level=WARN"); n != 1 || !strings.Contains(out, "LLMPinnedEmbedder") {
		t.Fatalf("want exactly one WARN naming LLMPinnedEmbedder, got %d: %s", n, out)
	}
	if err := m.HealthCheck(context.Background()); err == nil || !strings.Contains(err.Error(), "indexing disabled") {
		t.Fatalf("HealthCheck = %v", err)
	}
}

func TestModule_StartStopIsIdempotent(t *testing.T) {
	m, _, _, _ := initIndexing(t, offlineDB(t), fullGateway{})
	if m.worker == nil {
		t.Fatal("worker not built")
	}
	ctx := context.Background()
	for range 2 {
		if err := m.Start(ctx); err != nil {
			t.Fatal(err)
		}
	}
	stopCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	for range 2 {
		if err := m.Stop(stopCtx); err != nil {
			t.Fatalf("Stop = %v", err)
		}
	}
	if err := NewModule().Start(ctx); err != nil {
		t.Fatal(err)
	}
}

func TestModule_HealthCheckReportsEngineAndCorpusProblems(t *testing.T) {
	db := mongoDB(t)
	m, reg, _, fe := initIndexing(t, db, fullGateway{})
	fe.setHealthErr(errors.New("dial tcp 10.0.0.1: refused"))
	err := m.HealthCheck(context.Background())
	if !errors.Is(err, iface.ErrRetrievalUnavailable) {
		t.Fatalf("err = %v", err)
	}
	if strings.Contains(err.Error(), "10.0.0.1") {
		t.Fatalf("error leaks driver detail: %q", err)
	}

	fe.setHealthErr(nil)
	if err := service(t, reg).RegisterCorpus(validSpec()); err != nil {
		t.Fatal(err)
	}
	if err := m.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = m.Stop(context.Background()) }()
	deadline := time.Now().Add(10 * time.Second)
	for {
		err = m.HealthCheck(context.Background())
		if err != nil && strings.Contains(err.Error(), `local_only corpus uses hosted endpoint "embed"`) {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("HealthCheck after a pass = %v", err)
		}
		time.Sleep(50 * time.Millisecond)
	}
}

func TestModule_NilDatabaseWarnsOnceAndLeavesTheOutboxUnwired(t *testing.T) {
	for _, url := range []string{"", "http://qdrant.invalid:6333"} {
		t.Run("QDRANT_URL="+url, func(t *testing.T) {
			t.Setenv("QDRANT_URL", url)
			t.Setenv("QDRANT_API_KEY", "k")
			var buf bytes.Buffer
			reg := module.NewServiceRegistry()
			reg.Register(module.ServiceLLMGateway, fullGateway{})
			reg.Register(module.ServiceTenantService, iface.TenantIDLister(fakeTenants{}))
			reg.Register(module.ServiceModuleStatus, module.ModuleStatusReader(fakeStatus{}))
			m := NewModule()
			m.newEngine = func(qdrant.Config) (engine.Engine, error) { return &fakeEngine{}, nil }
			logger := slog.New(slog.NewTextHandler(&buf, nil))
			if err := m.Init(&module.Dependencies{Logger: logger, Services: reg}); err != nil {
				t.Fatalf("Init: %v", err)
			}
			if m.worker != nil {
				t.Fatal("worker built without a database")
			}
			out := buf.String()
			if n := strings.Count(out, "level=WARN"); n != 1 || !strings.Contains(out, "no database handle") {
				t.Fatalf("want exactly one database WARN, got %d: %s", n, out)
			}
			ob, ok := module.GetTyped[iface.RetrievalOutbox](reg, module.ServiceRetrieval)
			if !ok {
				t.Fatal("not a RetrievalOutbox")
			}
			ctx := ctxauth.WithTenantKind(context.WithValue(context.Background(), ctxauth.KeyTenantID, "t1"), "internal")
			if err := ob.Enqueue(ctx, iface.RetrievalIntent{Corpus: "contacts", SourceID: "c1"}); err == nil {
				t.Fatal("Enqueue succeeded with an unwired outbox")
			}
		})
	}
}
