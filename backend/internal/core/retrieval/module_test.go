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
	"reflect"
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
	"github.com/orkestra/backend/internal/testkit"
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
	registerSearchServices(reg)
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
	if len(cols) != 5 || cols[0].Name != "retrieval_corpus_state" || repository.CollCorpusState != "retrieval_corpus_state" ||
		cols[1].Name != "retrieval_outbox" || repository.CollOutbox != "retrieval_outbox" ||
		cols[2].Name != "retrieval_tenant_progress" || repository.CollTenantProgress != "retrieval_tenant_progress" ||
		cols[3].Name != "retrieval_erasures" || repository.CollErasures != "retrieval_erasures" ||
		cols[4].Name != "retrieval_search_log" || repository.CollSearchLog != "retrieval_search_log" {
		t.Fatalf("collections = %v", cols)
	}
	if p := module.PermissionsOf(m); len(p) != 0 {
		t.Fatalf("permissions = %v", p)
	}
	if c := module.ConfigSchemaOf(m); len(c) != 5 {
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
		repository.CollOutbox:    {{[]string{"dirty", "nextAttemptAt"}, false}, {[]string{"leaseUntil"}, false}},
		repository.CollErasures:  {{[]string{"uuid"}, true}, {[]string{"scope", "kind", "subject"}, false}, {[]string{"status", "nextAttemptAt"}, false}},
		repository.CollSearchLog: {{[]string{"subjects"}, false}, {[]string{"createdAt"}, false}, {[]string{"userUuid"}, false}, {[]string{"unchecked", "createdAt"}, false}},
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

type fakeReranker struct{}

func (fakeReranker) Rerank(context.Context, iface.RerankRequest) (*iface.RerankResult, error) {
	return nil, errors.New("not used")
}

// fullGateway is a gateway with both indexing capabilities and the reranker;
// profilesOnly has no pinned embedder and no reranker.
type fullGateway struct {
	fakeProfiles
	fakeEmbedder
	fakeReranker
}
type profilesOnly struct{ fakeProfiles }

type fakeTenants struct{}

func (fakeTenants) ListActiveTenantIDs(context.Context, string) ([]string, error) { return nil, nil }

// The tenant service also takes the purge hook and lists purged tenants
// (none), so erasure is fully wired and adds no WARN (P16).
func (fakeTenants) RegisterTenantPurgeHook(func(context.Context, string) error) {}
func (fakeTenants) ListPurgedTenantIDs(context.Context) ([]string, error)       { return nil, nil }

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
	registerSearchServices(reg)
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
	if len(groups) != 2 || groups[0].Key != "indexing" || groups[0].Label != "Indexing" || groups[0].Order != 1 ||
		groups[0].Description != "Outbox worker: lease per key, keys per pass and pass interval." ||
		groups[1].Key != "search" || groups[1].Label != "Search & audit" || groups[1].Order != 2 || groups[1].Description == "" {
		t.Fatalf("groups = %+v", groups)
	}
	want := map[string]struct {
		typ         module.ConfigFieldType
		def, env    string
		description string
		group       string
	}{
		"worker_lease": {module.FieldDuration, "2m", "RETRIEVAL_WORKER_LEASE",
			"How long a worker owns one item while indexing it (30s–15m). Embedding and engine calls end a quarter of the lease before it expires.", "indexing"},
		"worker_batch": {module.FieldInt, "32", "RETRIEVAL_WORKER_BATCH",
			"Items indexed per pass, shared round-robin across organizations.", "indexing"},
		"worker_poll_interval": {module.FieldDuration, "5s", "RETRIEVAL_WORKER_POLL_INTERVAL",
			"Pause between passes (1s–5m).", "indexing"},
		"principal_cap": {module.FieldInt, "256", "RETRIEVAL_PRINCIPAL_CAP",
			"Maximum number of principals (user, tenant, roles, contributors) a search may carry; a caller above it gets no results until the cap is raised. Fails closed.", "search"},
		"search_log_retention_days": {module.FieldInt, "90", "RETRIEVAL_SEARCH_LOG_RETENTION_DAYS",
			"Search-log rows older than this are deleted daily. The log records who searched for what and which people the results named (ADR-0023 D12).", "search"},
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
		if f.Type != w.typ || f.Default != w.def || f.EnvVar != w.env || f.Description != w.description || f.Group != w.group {
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
	indexing := warnLines(logs.String(), "indexing is disabled")
	if len(indexing) != 1 || !strings.Contains(indexing[0], "LLMPinnedEmbedder") {
		t.Fatalf("want exactly one indexing WARN naming LLMPinnedEmbedder, got %q", indexing)
	}
	search := warnLines(logs.String(), "retrieval: search")
	if len(search) != 1 || !strings.Contains(search[0], "LLMPinnedEmbedder") || !strings.Contains(search[0], "LLMReranker") {
		t.Fatalf("want exactly one search WARN naming the inference companions, got %q", search)
	}
	if n := strings.Count(logs.String(), "level=WARN"); n != 2 {
		t.Fatalf("want two WARNs, got %d: %s", n, logs)
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
			registerSearchServices(reg)
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
			// Search is wired only with a database (P15): without one it is
			// not configured, and principals fail closed.
			search, ok := module.GetTyped[iface.RetrievalSearch](reg, module.ServiceRetrieval)
			if !ok {
				t.Fatal("not a RetrievalSearch")
			}
			if _, err := search.Search(callerCtx(), iface.RetrievalSearchRequest{Corpus: "contacts", Query: "q"}); !errors.Is(err, iface.ErrRetrievalNotConfigured) {
				t.Fatalf("Search err = %v, want ErrRetrievalNotConfigured", err)
			}
			resolver, ok := module.GetTyped[iface.RetrievalPrincipalResolver](reg, module.ServiceRetrieval)
			if !ok {
				t.Fatal("not a RetrievalPrincipalResolver")
			}
			if _, err := resolver.Principals(callerCtx()); !errors.Is(err, iface.ErrRetrievalForbidden) {
				t.Fatalf("Principals err = %v, want ErrRetrievalForbidden", err)
			}
			m.mu.Lock()
			purger := m.purger
			m.mu.Unlock()
			if purger != nil {
				t.Fatal("retention wired without a database")
			}
		})
	}
}

// The worker's lifetime is owned by Stop, not by the Start context: the admin
// enable endpoint hands StartModule a request context that ends with the
// response.
func TestModule_WorkerOutlivesTheStartContext(t *testing.T) {
	m, _, _, _ := initIndexing(t, offlineDB(t), fullGateway{})
	ctx, cancel := context.WithCancel(context.Background())
	if err := m.Start(ctx); err != nil {
		t.Fatal(err)
	}
	cancel()
	m.lifecycleMu.Lock()
	done := m.done
	m.lifecycleMu.Unlock()
	select {
	case <-done:
		t.Fatal("the worker stopped with the Start context")
	case <-time.After(300 * time.Millisecond):
	}
	stopCtx, stop := context.WithTimeout(context.Background(), 5*time.Second)
	defer stop()
	if err := m.Stop(stopCtx); err != nil {
		t.Fatalf("Stop = %v", err)
	}
	select {
	case <-done:
	default:
		t.Fatal("Stop returned before the worker ended")
	}
}

// ---- search wiring (PR 5) ----

// fakeAuthz is an AuthzProvider that also lists roles; every permission is
// granted. Unimplemented methods panic through the nil embedded interface.
type fakeAuthz struct{ iface.AuthzProvider }

func (fakeAuthz) HasPermission(context.Context, string, string, string) (bool, error) {
	return true, nil
}
func (fakeAuthz) ActiveRoleIDs(context.Context, string, string) ([]string, error) {
	return []string{"r-1"}, nil
}

// plainAuthz is an AuthzProvider without the role lister.
type plainAuthz struct{ iface.AuthzProvider }

// fakeTenantProvider reports every user as a member.
type fakeTenantProvider struct{ iface.TenantProvider }

func (fakeTenantProvider) IsMember(context.Context, string, string) (bool, error) { return true, nil }

// plainGateway is an LLMGateway with no installation-level companion.
type plainGateway struct{ iface.LLMGateway }

// companionGateway is an LLMGateway with every companion search asserts.
type companionGateway struct {
	iface.LLMGateway
	fullGateway
}

func registerSearchServices(reg *module.ServiceRegistry) {
	reg.Register(module.ServiceAuthzProvider, iface.AuthzProvider(fakeAuthz{}))
	reg.Register(module.ServiceTenantProvider, iface.TenantProvider(fakeTenantProvider{}))
}

// callerCtx is user u-1 in internal tenant t1.
func callerCtx() context.Context {
	id := testkit.NewIdentity("u-1", "u1@example.test", "operator").WithTenant("t1", []string{"org_member"}, true)
	return ctxauth.WithTenantKind(id.ContextFor(context.Background(), "t1"), "internal")
}

// warnLines returns the WARN lines of out that contain sub.
func warnLines(out, sub string) []string {
	var lines []string
	for _, l := range strings.Split(out, "\n") {
		if strings.Contains(l, "level=WARN") && strings.Contains(l, sub) {
			lines = append(lines, l)
		}
	}
	return lines
}

func TestClampSearchSettings(t *testing.T) {
	cases := []struct{ in, want searchSettings }{
		{searchSettings{0, 0}, searchSettings{256, 90}},
		{searchSettings{5, 0}, searchSettings{16, 90}},
		{searchSettings{100000, 5000}, searchSettings{4096, 3650}},
		{searchSettings{300, 30}, searchSettings{300, 30}},
		{searchSettings{-1, -7}, searchSettings{256, 90}},
		{searchSettings{16, 1}, searchSettings{16, 1}},
	}
	for _, tc := range cases {
		if got := clampSearchSettings(tc.in); got != tc.want {
			t.Errorf("clampSearchSettings(%+v) = %+v, want %+v", tc.in, got, tc.want)
		}
	}
	if got := readSearchSettings(&module.Dependencies{}); got != (searchSettings{256, 90}) {
		t.Errorf("readSearchSettings without config = %+v", got)
	}
}

func TestCollections_DeclareSearchLog(t *testing.T) {
	var spec *module.CollectionSpec
	for _, c := range NewModule().Collections() {
		if c.Name == "retrieval_search_log" {
			spec = &c
		}
	}
	if spec == nil {
		t.Fatal("retrieval_search_log is not declared")
	}
	// Ruling R-T4b: the repository's index set, not the plan's list.
	want := []struct {
		fields []string
		unique bool
	}{
		{[]string{"tenantId", "uuid"}, true},
		{[]string{"tenantId", "userUuid", "createdAt"}, false},
		{[]string{"subjects"}, false},
		{[]string{"createdAt"}, false},
		{[]string{"userUuid"}, false},
		{[]string{"unchecked", "createdAt"}, false},
	}
	if len(spec.Indexes) != len(want) {
		t.Fatalf("indexes = %+v", spec.Indexes)
	}
	for i, idx := range spec.Indexes {
		var fields []string
		for _, k := range idx.OrderedKeys {
			fields = append(fields, k.Field)
			if k.Direction != 1 {
				t.Errorf("index %d key %s direction = %v", i, k.Field, k.Direction)
			}
		}
		if !slices.Equal(fields, want[i].fields) || idx.Unique != want[i].unique {
			t.Errorf("index %d = %v unique=%v, want %v unique=%v", i, fields, idx.Unique, want[i].fields, want[i].unique)
		}
	}
	// G2: the unchecked sweep's index holds only provisional rows.
	if pf := spec.Indexes[len(spec.Indexes)-1].PartialFilter; !reflect.DeepEqual(pf, map[string]any{"unchecked": true}) {
		t.Errorf("unchecked index partial filter = %v", pf)
	}
}

func TestConfigSchema_DeclaresSearchKeys(t *testing.T) {
	m := NewModule()
	if err := module.ValidateConfigDeclarations(m.ConfigSchema(), m.ConfigGroups()); err != nil {
		t.Fatal(err)
	}
	want := map[string]struct {
		def, env string
		min, max int
	}{
		"principal_cap":             {"256", "RETRIEVAL_PRINCIPAL_CAP", 16, 4096},
		"search_log_retention_days": {"90", "RETRIEVAL_SEARCH_LOG_RETENTION_DAYS", 1, 3650},
	}
	found := 0
	for _, f := range m.ConfigSchema() {
		w, ok := want[f.Key]
		if !ok {
			continue
		}
		found++
		if f.Type != module.FieldInt || f.Default != w.def || f.EnvVar != w.env || f.Group != "search" || f.Label == "" {
			t.Errorf("%s = %+v", f.Key, f)
		}
		if f.Min == nil || *f.Min != w.min || f.Max == nil || *f.Max != w.max {
			t.Errorf("%s bounds = %v..%v, want %d..%d", f.Key, f.Min, f.Max, w.min, w.max)
		}
	}
	if found != len(want) {
		t.Fatalf("found %d of %d search keys", found, len(want))
	}
}

func TestResolveSearchServices_AssertsCompanions(t *testing.T) {
	reg := module.NewServiceRegistry()
	reg.Register(module.ServiceLLMGateway, iface.LLMGateway(companionGateway{}))
	reg.Register(module.ServiceAuthzProvider, iface.AuthzProvider(fakeAuthz{}))
	reg.Register(module.ServiceTenantProvider, iface.TenantProvider(fakeTenantProvider{}))
	s := resolveSearchServices(reg)
	if s.profiles == nil || s.embedder == nil || s.reranker == nil {
		t.Fatalf("companions = %+v, want all three", s)
	}
	if s.authz == nil || s.roles == nil || s.members == nil {
		t.Fatalf("authz/roles/members = %v/%v/%v", s.authz, s.roles, s.members)
	}

	reg = module.NewServiceRegistry()
	reg.Register(module.ServiceLLMGateway, iface.LLMGateway(plainGateway{}))
	reg.Register(module.ServiceAuthzProvider, iface.AuthzProvider(plainAuthz{}))
	s = resolveSearchServices(reg)
	if s.profiles != nil || s.embedder != nil || s.reranker != nil {
		t.Fatalf("plain gateway companions = %+v, want none", s)
	}
	if s.authz == nil || s.roles != nil || s.members != nil {
		t.Fatalf("authz/roles/members = %v/%v/%v, want authz only", s.authz, s.roles, s.members)
	}

	if s := resolveSearchServices(module.NewServiceRegistry()); s != (searchServices{}) {
		t.Fatalf("empty registry = %+v", s)
	}
}

// Without QDRANT_URL the search is still wired (a database is present), and
// it reports not configured: the erasure check is wired, so the visibility
// guard does not pre-empt it (R-T7a). The error-path log write is attempted.
func TestModule_SearchWithoutEngineIsNotConfigured(t *testing.T) {
	t.Setenv("QDRANT_URL", "")
	_, reg, logs := initModule(t)
	if err := service(t, reg).RegisterCorpus(validSpec()); err != nil {
		t.Fatal(err)
	}
	search, ok := module.GetTyped[iface.RetrievalSearch](reg, module.ServiceRetrieval)
	if !ok {
		t.Fatal("ServiceRetrieval is not a RetrievalSearch")
	}
	res, err := search.Search(callerCtx(), iface.RetrievalSearchRequest{Corpus: "contacts", Query: "head of sales"})
	if !errors.Is(err, iface.ErrRetrievalNotConfigured) || res != nil {
		t.Fatalf("Search = %v, %v; want ErrRetrievalNotConfigured", res, err)
	}
	// The offline database refuses the best-effort row: one WARN, no query text.
	if l := warnLines(logs.String(), "search log"); len(l) != 1 {
		t.Fatalf("want one search-log WARN, got %q", l)
	}
	if strings.Contains(logs.String(), "head of sales") {
		t.Fatalf("logs carry the query: %s", logs)
	}

	resolver, ok := module.GetTyped[iface.RetrievalPrincipalResolver](reg, module.ServiceRetrieval)
	if !ok {
		t.Fatal("ServiceRetrieval is not a RetrievalPrincipalResolver")
	}
	pc, err := resolver.Principals(callerCtx())
	if err != nil {
		t.Fatalf("Principals: %v", err)
	}
	if !slices.Equal(pc.Principals, []string{"role:r-1", "tenant:*", "user:u-1"}) {
		t.Fatalf("principals = %v", pc.Principals)
	}
}

// Missing authz, role lister, tenant provider and inference companions give
// one search WARN; Init still succeeds and principals fail closed.
func TestModule_SearchWarnsOnceNamingMissingServices(t *testing.T) {
	t.Setenv("QDRANT_URL", "")
	var buf bytes.Buffer
	reg := module.NewServiceRegistry()
	reg.Register(module.ServiceLLMGateway, plainGateway{})
	reg.Register(module.ServiceTenantService, iface.TenantIDLister(fakeTenants{}))
	reg.Register(module.ServiceModuleStatus, module.ModuleStatusReader(fakeStatus{}))
	m := NewModule()
	if err := m.Init(&module.Dependencies{DB: offlineDB(t), Logger: slog.New(slog.NewTextHandler(&buf, nil)), Services: reg}); err != nil {
		t.Fatalf("Init: %v", err)
	}
	all := warnLines(buf.String(), "")
	if len(all) != 1 {
		t.Fatalf("want exactly one WARN, got %q", all)
	}
	for _, name := range []string{"AuthzProvider", "AuthzRoleLister", "TenantProvider", "LLMInferenceProfiles", "LLMPinnedEmbedder", "LLMReranker"} {
		if !strings.Contains(all[0], name) {
			t.Errorf("search WARN does not name %s: %s", name, all[0])
		}
	}
	resolver, _ := module.GetTyped[iface.RetrievalPrincipalResolver](reg, module.ServiceRetrieval)
	if _, err := resolver.Principals(callerCtx()); err == nil {
		t.Fatal("Principals succeeded without a membership checker or role lister")
	}
	search, _ := module.GetTyped[iface.RetrievalSearch](reg, module.ServiceRetrieval)
	if err := service(t, reg).RegisterCorpus(validSpec()); err != nil {
		t.Fatal(err)
	}
	if _, err := search.Search(callerCtx(), iface.RetrievalSearchRequest{Corpus: "contacts", Query: "q"}); !errors.Is(err, iface.ErrRetrievalUnavailable) {
		t.Fatalf("Search without authz err = %v, want ErrRetrievalUnavailable", err)
	}
}

// A missing TenantProvider alone (role lister present) still fails Principals
// closed: the nil interface reaches the resolver as a nil MembershipChecker
// without a guard in Init (D1).
func TestModule_PrincipalsWithoutTenantProviderFailsClosed(t *testing.T) {
	t.Setenv("QDRANT_URL", "")
	reg := module.NewServiceRegistry()
	reg.Register(module.ServiceAuthzProvider, iface.AuthzProvider(fakeAuthz{}))
	reg.Register(module.ServiceLLMGateway, plainGateway{})
	reg.Register(module.ServiceTenantService, iface.TenantIDLister(fakeTenants{}))
	reg.Register(module.ServiceModuleStatus, module.ModuleStatusReader(fakeStatus{}))
	m := NewModule()
	if err := m.Init(&module.Dependencies{DB: offlineDB(t), Logger: slog.New(slog.DiscardHandler), Services: reg}); err != nil {
		t.Fatalf("Init: %v", err)
	}
	resolver, _ := module.GetTyped[iface.RetrievalPrincipalResolver](reg, module.ServiceRetrieval)
	pc, err := resolver.Principals(callerCtx())
	if !errors.Is(err, iface.ErrRetrievalUnavailable) {
		t.Fatalf("Principals err = %v, want ErrRetrievalUnavailable", err)
	}
	if len(pc.Principals) != 0 {
		t.Fatalf("principals = %v, want none", pc.Principals)
	}
}

// fakePurger records PurgeBefore cutoffs and answers n or err; the unchecked
// sweep records its cutoffs and deletes nothing.
type fakePurger struct {
	mu        sync.Mutex
	cutoffs   []time.Time
	unchecked []time.Time
	n         int64
	err       error
}

func (p *fakePurger) PurgeBefore(_ context.Context, cutoff time.Time) (int64, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.cutoffs = append(p.cutoffs, cutoff)
	return p.n, p.err
}

func (p *fakePurger) PurgeUncheckedBefore(_ context.Context, cutoff time.Time) (int64, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.unchecked = append(p.unchecked, cutoff)
	return 0, nil
}

func (p *fakePurger) uncheckedCalls() []time.Time {
	p.mu.Lock()
	defer p.mu.Unlock()
	return slices.Clone(p.unchecked)
}

func (p *fakePurger) calls() []time.Time {
	p.mu.Lock()
	defer p.mu.Unlock()
	return slices.Clone(p.cutoffs)
}

func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func startRetention(t *testing.T, m *Module, p *fakePurger) {
	t.Helper()
	m.mu.Lock()
	if m.purger == nil {
		m.mu.Unlock()
		t.Fatal("retention not wired")
	}
	m.purger = p
	m.mu.Unlock()
	m.retentionFirst, m.retentionInterval = 5*time.Millisecond, 5*time.Millisecond
	if err := m.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
}

// P14: the retention loop runs whenever a database exists, engine or not.
func TestModule_RetentionLoopRunsWithoutEngine(t *testing.T) {
	t.Setenv("QDRANT_URL", "")
	m, _, logs := initModule(t)
	if m.worker != nil {
		t.Fatal("worker built without an engine")
	}
	p := &fakePurger{n: 7}
	before := time.Now()
	startRetention(t, m, p)
	waitFor(t, "two purges", func() bool { return len(p.calls()) >= 2 })
	stopCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := m.Stop(stopCtx); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	n := len(p.calls())
	time.Sleep(30 * time.Millisecond)
	if len(p.calls()) != n {
		t.Fatal("the retention loop outlived Stop")
	}
	cutoff := p.calls()[0]
	wantLo, wantHi := before.UTC().AddDate(0, 0, -90), time.Now().UTC().AddDate(0, 0, -90)
	if cutoff.Before(wantLo) || cutoff.After(wantHi) {
		t.Fatalf("cutoff %v outside [%v, %v]", cutoff, wantLo, wantHi)
	}
	// G2: every run also sweeps unchecked rows older than the grace.
	if u := p.uncheckedCalls(); len(u) != len(p.calls()) ||
		u[0].Before(before.UTC().Add(-services.UncheckedSearchLogGrace)) || u[0].After(time.Now().UTC().Add(-services.UncheckedSearchLogGrace)) {
		t.Fatalf("unchecked sweeps = %v, retention runs = %d", u, len(p.calls()))
	}
	if !strings.Contains(logs.String(), "level=INFO") || !strings.Contains(logs.String(), "deleted=7") {
		t.Fatalf("missing INFO with the deleted count: %s", logs)
	}
}

func TestModule_RetentionFailureWarns(t *testing.T) {
	t.Setenv("QDRANT_URL", "")
	m, _, logs := initModule(t)
	p := &fakePurger{err: errors.New("mongo down")}
	startRetention(t, m, p)
	// Purges are sequential, so a second call means the first one's WARN
	// was written; the buffer is read only after Stop.
	waitFor(t, "two purges", func() bool { return len(p.calls()) >= 2 })
	if err := m.Stop(context.Background()); err != nil {
		t.Fatal(err)
	}
	if l := warnLines(logs.String(), "search log retention purge failed"); len(l) == 0 {
		t.Fatalf("missing retention WARN: %s", logs)
	}
}

// The worker and the retention loop share one cancel and one done channel:
// Stop returns only after both ended.
func TestModule_StopWaitsForWorkerAndRetention(t *testing.T) {
	m, _, _, _ := initIndexing(t, offlineDB(t), fullGateway{})
	if m.worker == nil {
		t.Fatal("worker not built")
	}
	p := &fakePurger{}
	startRetention(t, m, p)
	waitFor(t, "a purge", func() bool { return len(p.calls()) >= 1 })
	m.lifecycleMu.Lock()
	done := m.done
	m.lifecycleMu.Unlock()
	stopCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := m.Stop(stopCtx); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	select {
	case <-done:
	default:
		t.Fatal("Stop returned before the background jobs ended")
	}
	// A restart after Stop runs again.
	startRetention(t, m, p)
	n := len(p.calls())
	waitFor(t, "a purge after restart", func() bool { return len(p.calls()) > n })
	if err := m.Stop(stopCtx); err != nil {
		t.Fatal(err)
	}
}

// The worker re-reads worker_lease / worker_batch / worker_poll_interval on
// every pass (Settings closure), so the registry must say a config write needs
// no restart; the admin UI derives needsRestart from this.
func TestModule_HotReloadsConfig(t *testing.T) {
	m := NewModule()
	if !module.HotReloadsConfig(m) {
		t.Fatal("HotReloadsConfig = false, want true")
	}
	reg := module.NewModuleRegistry(slog.New(slog.NewTextHandler(&bytes.Buffer{}, nil)))
	reg.Register(m)
	if !reg.SupportsHotReload("retrieval") {
		t.Fatal("registry SupportsHotReload(retrieval) = false, want true")
	}
}
