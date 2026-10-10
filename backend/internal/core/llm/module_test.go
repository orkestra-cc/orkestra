package llm

import (
	"bytes"
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/go-chi/chi/v5"
	"github.com/orkestra/backend/internal/core/llm/handlers"
	"github.com/orkestra/backend/internal/core/llm/models"
	"github.com/orkestra/backend/internal/core/llm/providers"
	"github.com/orkestra/backend/internal/core/llm/repository"
	"github.com/orkestra/backend/internal/core/llm/services"
	"github.com/orkestra/backend/internal/testkit"
	"github.com/orkestra/backend/pkg/sdk/ctxauth"
	"github.com/orkestra/backend/pkg/sdk/iface"
	"github.com/orkestra/backend/pkg/sdk/module"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

func TestModule_Declarations(t *testing.T) {
	m := NewModule()
	if m.Name() != "llm" || m.Category() != module.CategoryCore {
		t.Fatalf("name/category = %s/%s", m.Name(), m.Category())
	}
	deps := map[string]bool{}
	for _, d := range m.Dependencies() {
		deps[d] = true
	}
	for _, want := range []string{"user", "tenant", "notification"} {
		if !deps[want] {
			t.Errorf("missing dependency %s", want)
		}
	}
	if got := m.ProvidedServices(); len(got) != 1 || got[0] != module.ServiceLLMGateway {
		t.Fatalf("ProvidedServices = %v", got)
	}
	if !m.HotReloadConfig() {
		t.Fatal("config must hot-reload")
	}

	system, self := 0, 0
	for _, p := range m.Permissions() {
		if p.Module != "llm" || !strings.HasPrefix(p.Key, "llm.") {
			t.Errorf("permission %s has module %q", p.Key, p.Module)
		}
		if p.System {
			system++
		} else {
			self++
		}
	}
	// PR 1 declares only the permissions its routes use (ruling M4).
	if system != 4 || self != 1 {
		t.Fatalf("permissions = %d system + %d self, want 4 + 1", system, self)
	}

	keys := map[string]bool{}
	for _, f := range m.ConfigSchema() {
		keys[f.Key] = true
	}
	for _, want := range []string{"allow_hosted", "request_timeout", "usage_retention_days", "budget_warn_pct", "budget_reserve_output_tokens"} {
		if !keys[want] {
			t.Errorf("missing config key %s", want)
		}
	}
}

// The repository maps duplicate-key errors to ErrDuplicateName and
// Grants.Replace tolerates racing inserts; both rely on these unique indexes.
func TestModule_UniqueIndexes(t *testing.T) {
	unique := map[string][][]string{}
	for _, c := range NewModule().Collections() {
		for _, idx := range c.Indexes {
			if !idx.Unique {
				continue
			}
			var fields []string
			for _, k := range idx.OrderedKeys {
				fields = append(fields, k.Field)
			}
			unique[c.Name] = append(unique[c.Name], fields)
		}
	}
	want := map[string][]string{
		repository.CollCredentials: {"tenantId", "name"},
		repository.CollModels:      {"tenantId", "name"},
		repository.CollGrants:      {"tenantId", "modelUuid", "userUuid"},
	}
	for coll, fields := range want {
		found := false
		for _, got := range unique[coll] {
			if reflect.DeepEqual(got, fields) {
				found = true
			}
		}
		if !found {
			t.Errorf("%s has no unique index on %v (got %v)", coll, fields, unique[coll])
		}
	}
	for _, c := range NewModule().Collections() {
		for _, idx := range c.Indexes {
			if len(idx.OrderedKeys) == 0 || idx.OrderedKeys[0].Field != "tenantId" {
				t.Errorf("%s index %+v does not lead with tenantId", c.Name, idx)
			}
		}
	}
}

// --- route guards ------------------------------------------------------------

type guardsKey struct{}

// recordingMW is a module.RoleMiddleware whose gates record their name and
// argument on the request and always pass, so a test can read back which
// gates protect each mounted operation.
type recordingMW struct{}

var _ module.RoleMiddleware = recordingMW{}

func mark(label string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if rec, ok := r.Context().Value(guardsKey{}).(*[]string); ok {
				*rec = append(*rec, label)
			}
			next.ServeHTTP(w, r)
		})
	}
}

func (recordingMW) RequirePermission(p string) func(http.Handler) http.Handler {
	return mark("perm:" + p)
}
func (recordingMW) RequireSystemPermission(p string) func(http.Handler) http.Handler {
	return mark("sys:" + p)
}
func (recordingMW) RequireCapability(c string) func(http.Handler) http.Handler {
	return mark("cap:" + c)
}
func (recordingMW) RequireGlobal() func(http.Handler) http.Handler { return mark("global") }
func (recordingMW) RequireMFA() func(http.Handler) http.Handler    { return mark("mfa") }
func (recordingMW) RequireStepUp(d time.Duration) func(http.Handler) http.Handler {
	return mark("stepup:" + d.String())
}
func (recordingMW) RequireLowRisk(float64) func(http.Handler) http.Handler { return mark("lowrisk") }
func (recordingMW) RequireInternalTenant() func(http.Handler) http.Handler {
	return mark("internal")
}
func (recordingMW) RequireExternalTenant() func(http.Handler) http.Handler {
	return mark("external")
}

// Empty repositories: the guarded handlers run for real and fail or answer
// empty, which is irrelevant here — only the recorded gates are asserted.
type emptyCreds struct{}

func (emptyCreds) Insert(context.Context, *models.Credential) error  { return nil }
func (emptyCreds) List(context.Context) ([]models.Credential, error) { return nil, nil }
func (emptyCreds) Get(context.Context, string) (*models.Credential, error) {
	return nil, repository.ErrNotFound
}
func (emptyCreds) Update(context.Context, *models.Credential) error { return repository.ErrNotFound }
func (emptyCreds) Delete(context.Context, string) error             { return repository.ErrNotFound }

type emptyModels struct{}

func (emptyModels) Insert(context.Context, *models.Model) error        { return nil }
func (emptyModels) List(context.Context) ([]models.Model, error)       { return nil, nil }
func (emptyModels) ListActive(context.Context) ([]models.Model, error) { return nil, nil }
func (emptyModels) ListActiveByCredential(context.Context, string) ([]models.Model, error) {
	return nil, nil
}
func (emptyModels) Get(context.Context, string) (*models.Model, error) {
	return nil, repository.ErrNotFound
}
func (emptyModels) Update(context.Context, *models.Model) error { return repository.ErrNotFound }
func (emptyModels) Delete(context.Context, string) error        { return repository.ErrNotFound }

type emptyGrants struct{}

func (emptyGrants) ListByModel(context.Context, string) ([]models.LLMGrant, error) { return nil, nil }
func (emptyGrants) ListByUser(context.Context, string) ([]models.LLMGrant, error)  { return nil, nil }
func (emptyGrants) Replace(context.Context, string, string, []string) error        { return nil }
func (emptyGrants) DeleteByModel(context.Context, string) error                    { return nil }

// operatorCtx is what AuthMiddleware leaves for an operator acting in the
// internal tenant t1; the recording gates do not set it.
func operatorCtx() context.Context {
	id := testkit.NewIdentity("u-admin", "admin@example.test", "administrator").WithTenant("t1", []string{"org_owner"}, true)
	return ctxauth.WithTenantKind(id.ContextFor(context.Background(), "t1"), "internal")
}

func moduleForRoutes(t *testing.T) *Module {
	t.Helper()
	logger := slog.New(slog.NewTextHandler(&bytes.Buffer{}, nil))
	vault, err := services.NewVault("", logger)
	if err != nil {
		t.Fatal(err)
	}
	cfg := func() services.CatalogConfig { return services.CatalogConfig{} }
	catalog := services.NewCatalogService(emptyCreds{}, emptyModels{}, emptyGrants{}, vault, nil, cfg, logger)
	gw := services.NewGateway(services.NewAccessResolver(emptyModels{}, emptyGrants{}), catalog, providers.NewRegistry(), vault, logger)
	m := NewModule()
	m.admin = handlers.NewAdminHandler(catalog, logger)
	m.self = handlers.NewSelfHandler(gw, logger)
	return m
}

// docsless is the test API config without the OpenAPI/docs/schema routes,
// so the walk below counts only the module's own operations.
func docsless() huma.Config {
	cfg := huma.DefaultConfig("test", "1.0.0")
	cfg.OpenAPIPath, cfg.DocsPath, cfg.SchemasPath = "", "", ""
	return cfg
}

func TestModule_RouteGuards(t *testing.T) {
	router := chi.NewRouter()
	moduleForRoutes(t).RegisterRoutes(&module.RouteInfo{
		Operator: &module.APISurface{
			Audience:        module.AudienceOperator,
			ProtectedRouter: router,
			AuthMW:          recordingMW{},
		},
		APIConfig: docsless(),
	})

	const id = "/0b6e3f6a-7a4c-4a4e-9a51-6d1c2b7f8e90"
	stepUp := "stepup:" + (5 * time.Minute).String()
	read := []string{"internal", "sys:llm.admin.read"}
	cases := []struct {
		method, path string
		want         []string
	}{
		{http.MethodGet, "/v1/admin/llm/credentials", read},
		{http.MethodGet, "/v1/admin/llm/credentials" + id, read},
		{http.MethodGet, "/v1/admin/llm/models", read},
		{http.MethodGet, "/v1/admin/llm/models" + id, read},
		{http.MethodPost, "/v1/admin/llm/credentials", []string{"internal", "sys:llm.credentials.admin", stepUp}},
		{http.MethodPost, "/v1/admin/llm/credentials" + id + "/rotate", []string{"internal", "sys:llm.credentials.admin", stepUp}},
		{http.MethodDelete, "/v1/admin/llm/credentials" + id, []string{"internal", "sys:llm.credentials.admin", stepUp}},
		{http.MethodPatch, "/v1/admin/llm/credentials" + id, []string{"internal", "sys:llm.credentials.admin", stepUp}},
		{http.MethodPost, "/v1/admin/llm/models", []string{"internal", "sys:llm.models.admin"}},
		{http.MethodPatch, "/v1/admin/llm/models" + id, []string{"internal", "sys:llm.models.admin"}},
		{http.MethodDelete, "/v1/admin/llm/models" + id, []string{"internal", "sys:llm.models.admin"}},
		{http.MethodPut, "/v1/admin/llm/models" + id + "/grants", []string{"internal", "sys:llm.grants.admin", stepUp}},
		{http.MethodGet, "/v1/llm/me/models", []string{"internal", "perm:llm.models.self"}},
	}
	used := map[string]bool{}
	for _, tc := range cases {
		t.Run(tc.method+" "+tc.path, func(t *testing.T) {
			var got []string
			req := httptest.NewRequest(tc.method, tc.path, strings.NewReader("{}"))
			req.Header.Set("Content-Type", "application/json")
			req = req.WithContext(context.WithValue(operatorCtx(), guardsKey{}, &got))
			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, req)
			if rec.Code == http.StatusNotFound && len(got) == 0 || rec.Code == http.StatusMethodNotAllowed {
				t.Fatalf("route not mounted (status %d)", rec.Code)
			}
			for _, g := range got {
				if p, ok := strings.CutPrefix(g, "sys:"); ok {
					used[p] = true
				}
				if p, ok := strings.CutPrefix(g, "perm:"); ok {
					used[p] = true
				}
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("gates = %v, want %v", got, tc.want)
			}
		})
	}

	// Every mounted route is in the table above, and every declared
	// permission gates at least one route.
	mounted := 0
	if err := chi.Walk(router, func(string, string, http.Handler, ...func(http.Handler) http.Handler) error {
		mounted++
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if mounted != len(cases) {
		t.Errorf("%d routes mounted, %d asserted", mounted, len(cases))
	}
	for _, p := range NewModule().Permissions() {
		if !used[p.Key] {
			t.Errorf("permission %s is declared but gates no route", p.Key)
		}
	}
}

// --- Init --------------------------------------------------------------------

type noMembers struct{}

func (noMembers) ListTenantMembers(context.Context, string) ([]iface.TenantMemberSummary, error) {
	return nil, nil
}

// offlineDB returns a database handle whose client never reaches a server:
// Init only builds repositories from it.
func offlineDB(t *testing.T) *mongo.Database {
	t.Helper()
	client, err := mongo.Connect(context.Background(), options.Client().ApplyURI("mongodb://127.0.0.1:1").SetServerSelectionTimeout(10*time.Millisecond))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = client.Disconnect(context.Background()) })
	return client.Database("llm_module_test")
}

func initModule(t *testing.T, key string) (*Module, *module.ServiceRegistry, *iface.PIIProducerRegistry, string) {
	t.Helper()
	t.Setenv("LLM_SECRET_ENCRYPTION_KEY", key)
	logs := &bytes.Buffer{}
	reg := module.NewServiceRegistry()
	reg.Register(module.ServiceTenantDirectoryReader, iface.TenantDirectoryReader(noMembers{}))
	pii := iface.NewPIIProducerRegistry()
	reg.Register(module.ServicePIIProducerRegistry, pii)
	m := NewModule()
	if err := m.Init(&module.Dependencies{DB: offlineDB(t), Logger: slog.New(slog.NewTextHandler(logs, nil)), Services: reg}); err != nil {
		t.Fatalf("Init: %v", err)
	}
	return m, reg, pii, logs.String()
}

func TestModule_InitWiresGatewayAndPIIProducer(t *testing.T) {
	m, reg, pii, logs := initModule(t, strings.Repeat("ab", 32))
	if _, ok := module.GetTyped[iface.LLMGateway](reg, module.ServiceLLMGateway); !ok {
		t.Fatal("gateway not registered under ServiceLLMGateway")
	}
	if _, ok := module.GetTyped[iface.KMSProviderSetter](reg, module.ServiceLLMGateway); !ok {
		t.Fatal("the gateway must accept a KMS provider (compliance late wiring)")
	}
	if _, ok := module.GetTyped[iface.AuditSinkSetter](reg, module.ServiceLLMGateway); !ok {
		t.Fatal("the gateway must accept an audit sink (compliance late wiring)")
	}
	found := false
	for _, p := range pii.List() {
		if p.Subject() == "llm" {
			found = true
		}
	}
	if !found {
		t.Fatal("PII producer not registered")
	}
	if strings.Contains(logs, "level=WARN") {
		t.Fatalf("a valid key must not warn: %s", logs)
	}
	if err := m.HealthCheck(context.Background()); err != nil {
		t.Fatalf("HealthCheck = %v", err)
	}
}

// A malformed or placeholder key must never stop the core from booting: it
// is treated as absent, with one WARN that never prints the value.
func TestModule_InitMalformedKeyWarnsAndContinues(t *testing.T) {
	const placeholder = "changeme-llm-placeholder"
	m, _, _, logs := initModule(t, placeholder)
	if n := strings.Count(logs, "level=WARN"); n != 1 {
		t.Fatalf("WARN lines = %d, want 1: %s", n, logs)
	}
	if strings.Contains(logs, placeholder) {
		t.Fatalf("the key value reached the log: %s", logs)
	}
	if m.vault == nil || m.vault.Available() {
		t.Fatal("a malformed key must leave the vault without a key")
	}
	if err := m.HealthCheck(context.Background()); err != nil {
		t.Fatalf("no key is a degraded state, not an unhealthy module: %v", err)
	}
}

func TestModule_InitMissingKeyWarnsOnce(t *testing.T) {
	m, _, _, logs := initModule(t, "")
	if n := strings.Count(logs, "level=WARN"); n != 1 {
		t.Fatalf("WARN lines = %d, want 1: %s", n, logs)
	}
	if err := m.HealthCheck(context.Background()); err != nil {
		t.Fatalf("HealthCheck = %v", err)
	}
}

func TestModule_InitRequiresTheTenantDirectory(t *testing.T) {
	t.Setenv("LLM_SECRET_ENCRYPTION_KEY", "")
	m := NewModule()
	err := m.Init(&module.Dependencies{DB: offlineDB(t), Logger: slog.New(slog.NewTextHandler(&bytes.Buffer{}, nil)), Services: module.NewServiceRegistry()})
	if err == nil {
		t.Fatal("Init must fail without the tenant directory reader")
	}
}
