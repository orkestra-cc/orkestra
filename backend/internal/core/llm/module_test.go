package llm

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"strings"
	"sync"
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
	"github.com/orkestra/backend/pkg/sdk/tenantrepo"
	"go.mongodb.org/mongo-driver/bson"
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
	// Only the permissions the mounted routes use (amendment 1; PR 2 adds
	// llm.usage.read, llm.budgets.admin and llm.usage.self; PR 3
	// llm.accounts.admin and llm.accounts.self).
	if system != 7 || self != 3 {
		t.Fatalf("permissions = %d system + %d self, want 7 + 3", system, self)
	}

	keys := map[string]bool{}
	for _, f := range m.ConfigSchema() {
		keys[f.Key] = true
	}
	for _, want := range []string{"allow_hosted", "request_timeout", "usage_retention_days", "budget_warn_pct", "budget_reserve_output_tokens", "siwc_mode", "siwc_paste_port"} {
		if !keys[want] {
			t.Errorf("missing config key %s", want)
		}
	}

	groups := map[string]bool{}
	for _, g := range m.ConfigGroups() {
		groups[g.Key] = true
	}
	for _, f := range m.ConfigSchema() {
		if !groups[f.Group] {
			t.Errorf("config key %s is in undeclared group %q", f.Key, f.Group)
		}
		if f.Key == "siwc_mode" && f.Default != "disabled" {
			t.Errorf("siwc_mode default = %q, must be disabled", f.Default)
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
	want := map[string][][]string{
		repository.CollCredentials:  {{"tenantId", "name"}, {"tenantId", "uuid"}},
		repository.CollModels:       {{"tenantId", "name"}, {"tenantId", "uuid"}},
		repository.CollGrants:       {{"tenantId", "modelUuid", "userUuid"}},
		repository.CollUserAccounts: {{"tenantId", "userUuid", "clientId"}, {"tenantId", "userUuid", "provider", "selected"}, {"tenantId", "uuid"}},
		repository.CollUsage:        {{"tenantId", "uuid"}},
		repository.CollUsageMonthly: {{"tenantId", "period"}},
		repository.CollBudgets:      {{"tenantId"}},
		repository.CollBudgetOutbox: {{"tenantId", "period", "threshold"}, {"tenantId", "uuid"}},
	}
	for coll, all := range want {
		for _, fields := range all {
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
	}
	// The only indexes not led by tenantId serve the system sweeps that
	// run across every org by design (//tenantscope:allow system:): the
	// reconciler's pending scan, the daily usage retention purge and the
	// outbox worker's lease. Anything else must lead with tenantId.
	sweepIndexes := map[string][][]string{
		// userUuid: not a sweep but the DSR queries (subject across every
		// org, //tenantscope:allow dsr:), which have no tenantId to lead with.
		repository.CollUsage:        {{"outcome", "reservationExpiresAt"}, {"createdAt"}, {"userUuid"}},
		repository.CollBudgetOutbox: {{"status", "nextAttemptAt"}},
		// userUuid: the DSR queries over the ChatGPT registrations (export,
		// delete across every org, //tenantscope:allow dsr:).
		repository.CollUserAccounts: {{"userUuid"}},
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
			for _, sweep := range sweepIndexes[c.Name] {
				if reflect.DeepEqual(sweep, fields) && !idx.Unique {
					allowed = true
				}
			}
			if !allowed {
				t.Errorf("%s index %v does not lead with tenantId and is not a declared system sweep", c.Name, fields)
			}
		}
	}
}

// The one-selected-account rule rests on the partial unique index: a
// non-partial one would forbid a second unselected registration.
func TestModule_UserAccountSelectedIndexIsPartial(t *testing.T) {
	for _, c := range NewModule().Collections() {
		if c.Name != repository.CollUserAccounts {
			continue
		}
		for _, idx := range c.Indexes {
			n := len(idx.OrderedKeys)
			if n == 4 && idx.OrderedKeys[3].Field == "selected" {
				if !idx.Unique || idx.PartialFilter["selected"] != true {
					t.Fatalf("selected index = %+v, want unique + partial {selected: true}", idx)
				}
				return
			}
		}
	}
	t.Fatal("no (tenantId, userUuid, provider, selected) index on llm_user_accounts")
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
func (emptyCreds) Patch(context.Context, string, models.CredentialPatch) (time.Time, error) {
	return time.Time{}, repository.ErrNotFound
}
func (emptyCreds) SetSecret(context.Context, string, models.Envelope, string) (time.Time, error) {
	return time.Time{}, repository.ErrNotFound
}
func (emptyCreds) SetTestResult(context.Context, *models.Credential, time.Time, string, string) error {
	return repository.ErrNotFound
}
func (emptyCreds) Delete(context.Context, string) error { return repository.ErrNotFound }

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

type emptyUsage struct{}

func (emptyUsage) Query(context.Context, time.Time, time.Time, string) ([]models.LLMUsageRow, error) {
	return nil, nil
}
func (emptyUsage) UserMonth(_ context.Context, _ string, period string) (*models.LLMSelfUsage, error) {
	return &models.LLMSelfUsage{Period: period}, nil
}

type emptyMonthly struct{}

func (emptyMonthly) Get(_ context.Context, period string) (*models.UsageMonthly, error) {
	return &models.UsageMonthly{Period: period}, nil
}

type emptyBudgets struct{}

func (emptyBudgets) Get(context.Context) (*models.Budget, error) { return nil, repository.ErrNotFound }
func (emptyBudgets) Put(context.Context, *models.Budget) error   { return nil }

type emptyGrants struct{}

func (emptyGrants) ListByModel(context.Context, string) ([]models.LLMGrant, error) { return nil, nil }
func (emptyGrants) ListByUser(context.Context, string) ([]models.LLMGrant, error)  { return nil, nil }
func (emptyGrants) Replace(context.Context, string, string, string, []string) error {
	return repository.ErrNotFound
}
func (emptyGrants) DeleteByModel(context.Context, string) error { return nil }

// emptyAccounts is a linked-account store with nothing in it.
type emptyAccounts struct{}

var _ services.AccountRepo = emptyAccounts{}

func (emptyAccounts) Insert(context.Context, *models.UserAccount) error { return nil }
func (emptyAccounts) ListByUser(context.Context, string) ([]models.UserAccount, error) {
	return nil, nil
}
func (emptyAccounts) ListAdmin(context.Context) ([]models.UserAccount, error) { return nil, nil }
func (emptyAccounts) Get(context.Context, string) (*models.UserAccount, error) {
	return nil, repository.ErrNotFound
}
func (emptyAccounts) GetSelected(context.Context, string, string) (*models.UserAccount, error) {
	return nil, repository.ErrNotFound
}
func (emptyAccounts) Select(context.Context, string, string, string) error {
	return repository.ErrNotFound
}
func (emptyAccounts) Delete(context.Context, string) (*models.UserAccount, error) {
	return nil, repository.ErrNotFound
}
func (emptyAccounts) SetLastUsed(context.Context, string, time.Time) error { return nil }
func (emptyAccounts) SetStatus(context.Context, string, string, string) error {
	return nil
}
func (emptyAccounts) SetStatusIf(context.Context, string, time.Time, string, string) error {
	return nil
}
func (emptyAccounts) ReplaceTokens(context.Context, string, time.Time, models.AccountTokenSet) error {
	return repository.ErrNotFound
}

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
	gw := services.NewGateway(services.NewAccessResolver(emptyModels{}, emptyGrants{}, emptyCreds{}, cfg), catalog, providers.NewRegistry(), vault, logger)
	m := NewModule()
	m.admin = handlers.NewAdminHandler(catalog, logger)
	m.self = handlers.NewSelfHandler(gw, logger)
	m.ops = handlers.NewOpsHandler(services.NewAdminOps(catalog, providers.NewRegistry(), nil, services.NewRateLimiter(nil, logger), services.NewPinnedRunner(gw), logger), logger)
	m.usageH = handlers.NewUsageHandler(services.NewUsageService(emptyUsage{}, emptyMonthly{}, emptyBudgets{}, noMembers{}, catalog,
		func() services.BudgetConfig { return services.BudgetConfig{} }, logger), logger)
	siwcCfg := func() services.SIWCConfig { return services.SIWCConfig{Mode: services.SIWCModeDisabled} }
	limiter := services.NewRateLimiter(nil, logger)
	accounts := services.NewAccountsService(services.AccountsDeps{Repo: emptyAccounts{}, Vault: vault, Limiter: limiter, Config: siwcCfg, Logger: logger})
	m.siwc = handlers.NewSIWCHandler(accounts, siwcCfg, limiter, nil, "http://localhost:8080", logger)
	m.logger = logger
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
		{http.MethodPost, "/v1/admin/llm/credentials", []string{"internal", "sys:llm.credentials.admin", "mfa", stepUp}},
		{http.MethodPost, "/v1/admin/llm/credentials" + id + "/rotate", []string{"internal", "sys:llm.credentials.admin", "mfa", stepUp}},
		{http.MethodDelete, "/v1/admin/llm/credentials" + id, []string{"internal", "sys:llm.credentials.admin", "mfa", stepUp}},
		{http.MethodPatch, "/v1/admin/llm/credentials" + id, []string{"internal", "sys:llm.credentials.admin", "mfa", stepUp}},
		{http.MethodPost, "/v1/admin/llm/models", []string{"internal", "sys:llm.models.admin", "mfa"}},
		{http.MethodPatch, "/v1/admin/llm/models" + id, []string{"internal", "sys:llm.models.admin", "mfa"}},
		{http.MethodDelete, "/v1/admin/llm/models" + id, []string{"internal", "sys:llm.models.admin", "mfa"}},
		{http.MethodPut, "/v1/admin/llm/models" + id + "/grants", []string{"internal", "sys:llm.grants.admin", "mfa", stepUp}},
		{http.MethodGet, "/v1/llm/me/models", []string{"internal", "perm:llm.models.self"}},
		{http.MethodPost, "/v1/admin/llm/credentials" + id + "/test", []string{"internal", "sys:llm.credentials.admin", "mfa"}},
		{http.MethodPost, "/v1/admin/llm/credentials" + id + "/models", []string{"internal", "sys:llm.credentials.admin", "mfa"}},
		{http.MethodPost, "/v1/admin/llm/models" + id + "/prompt", []string{"internal", "sys:llm.models.admin", "mfa"}},
		{http.MethodGet, "/v1/admin/llm/usage", []string{"internal", "sys:llm.usage.read"}},
		{http.MethodGet, "/v1/admin/llm/budget", read},
		{http.MethodPut, "/v1/admin/llm/budget", []string{"internal", "sys:llm.budgets.admin", "mfa", stepUp}},
		{http.MethodGet, "/v1/llm/me/usage", []string{"internal", "perm:llm.usage.self"}},
		// Sign in with ChatGPT (ruling 5.6): own registrations without MFA or
		// step-up; the forced unlink with both; the org list with the reads.
		{http.MethodGet, "/v1/llm/accounts/openai", []string{"internal", "perm:llm.accounts.self"}},
		{http.MethodPost, "/v1/llm/accounts/openai/link", []string{"internal", "perm:llm.accounts.self"}},
		{http.MethodPost, "/v1/llm/accounts/openai/complete", []string{"internal", "perm:llm.accounts.self"}},
		{http.MethodPut, "/v1/llm/accounts/openai" + id + "/selected", []string{"internal", "perm:llm.accounts.self"}},
		{http.MethodDelete, "/v1/llm/accounts/openai" + id, []string{"internal", "perm:llm.accounts.self"}},
		{http.MethodGet, "/v1/admin/llm/accounts", read},
		{http.MethodDelete, "/v1/admin/llm/accounts" + id, []string{"internal", "sys:llm.accounts.admin", "mfa", stepUp}},
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

	// No budget is a 204 with no body, through the real Huma stack.
	req := httptest.NewRequest(http.MethodGet, "/v1/admin/llm/budget", nil)
	req = req.WithContext(context.WithValue(operatorCtx(), guardsKey{}, &[]string{}))
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusNoContent || rec.Body.Len() != 0 {
		t.Fatalf("GET budget without one = %d %q", rec.Code, rec.Body.String())
	}
}

// The public loopback callback exists only in loopback_local, on the root
// router (no auth gate), decided once at RegisterRoutes; in hosted_paste and
// disabled it is absent.
func TestModule_CallbackMountedOnlyInLoopbackLocal(t *testing.T) {
	for _, tc := range []struct {
		mode    string
		mounted bool
	}{
		{services.SIWCModeDisabled, false},
		{services.SIWCModeHostedPaste, false},
		{services.SIWCModeLoopbackLocal, true},
	} {
		t.Run(tc.mode, func(t *testing.T) {
			m := moduleForRoutes(t)
			m.siwcCfg = func() services.SIWCConfig {
				return services.SIWCConfig{Mode: tc.mode, AllowHosted: true, PastePort: services.SIWCDefaultPastePort, OperatorPublicURL: "http://127.0.0.1:3007"}
			}
			root, protected := chi.NewRouter(), chi.NewRouter()
			m.RegisterRoutes(&module.RouteInfo{
				Operator:  &module.APISurface{Audience: module.AudienceOperator, ProtectedRouter: protected, AuthMW: recordingMW{}},
				Router:    root,
				APIConfig: docsless(),
			})
			var rootRoutes []string
			if err := chi.Walk(root, func(method, route string, _ http.Handler, _ ...func(http.Handler) http.Handler) error {
				rootRoutes = append(rootRoutes, method+" "+route)
				return nil
			}); err != nil {
				t.Fatal(err)
			}
			want := []string(nil)
			if tc.mounted {
				want = []string{http.MethodGet + " " + services.SIWCCallbackPath}
			}
			if !reflect.DeepEqual(rootRoutes, want) {
				t.Fatalf("root routes = %v, want %v", rootRoutes, want)
			}
			// Never on the authenticated router either.
			if err := chi.Walk(protected, func(_, route string, _ http.Handler, _ ...func(http.Handler) http.Handler) error {
				if route == services.SIWCCallbackPath {
					t.Errorf("callback mounted behind auth")
				}
				return nil
			}); err != nil {
				t.Fatal(err)
			}
			if !tc.mounted {
				return
			}
			req := httptest.NewRequest(http.MethodGet, services.SIWCCallbackPath+"?code=c&state=s", nil)
			req.Host = "127.0.0.1:3007"
			rec := httptest.NewRecorder()
			root.ServeHTTP(rec, req)
			if rec.Code != http.StatusFound || !strings.HasPrefix(rec.Header().Get("Location"), "http://localhost:8080/user/security?") {
				t.Fatalf("callback = %d %q", rec.Code, rec.Header().Get("Location"))
			}
		})
	}
}

func TestModule_LiveRedisDropsTypedNil(t *testing.T) {
	var typed *typedNilRedis
	if liveRedis(typed) != nil || liveRedis(nil) != nil {
		t.Fatal("a nil or typed-nil client must become a plain nil")
	}
	if liveRedis(&typedNilRedis{}) == nil {
		t.Fatal("a real client must pass through")
	}
}

type typedNilRedis struct{}

func (*typedNilRedis) Get(context.Context, string) (string, error)                   { return "", nil }
func (*typedNilRedis) Set(context.Context, string, interface{}, time.Duration) error { return nil }
func (*typedNilRedis) Del(context.Context, ...string) error                          { return nil }
func (*typedNilRedis) Keys(context.Context, string) ([]string, error)                { return nil, nil }
func (*typedNilRedis) Incr(context.Context, string) (int64, error)                   { return 0, nil }
func (*typedNilRedis) Expire(context.Context, string, time.Duration) error           { return nil }

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

// The config accessors clamp every value: an out-of-range or zero stored
// value can never yield a zero deadline or a reservation that expires
// under a running call (Task 7 review: ReservationTTL must exceed the
// finalization window and cover two full attempts).
func TestModule_SettingsAreClamped(t *testing.T) {
	cases := []struct {
		in   moduleSettings
		want moduleSettings
	}{
		{moduleSettings{requestTimeout: 0, retentionDays: 0, warnPct: -5, reserveOutput: -1},
			moduleSettings{requestTimeout: defaultRequestTimeout, retentionDays: minRetentionDays, warnPct: 0, reserveOutput: 1}},
		{moduleSettings{requestTimeout: time.Second, retentionDays: 30, warnPct: 1, reserveOutput: 1},
			moduleSettings{requestTimeout: time.Second, retentionDays: minRetentionDays, warnPct: 1, reserveOutput: 1}},
		{moduleSettings{requestTimeout: -time.Second, retentionDays: 99999, warnPct: 500, reserveOutput: 1 << 30},
			moduleSettings{requestTimeout: defaultRequestTimeout, retentionDays: maxRetentionDays, warnPct: 100, reserveOutput: models.MaxOutputTokens}},
		{moduleSettings{requestTimeout: 2 * time.Second, retentionDays: 45, warnPct: 75, reserveOutput: 512},
			moduleSettings{requestTimeout: 2 * time.Second, retentionDays: 45, warnPct: 75, reserveOutput: 512}},
	}
	for _, tc := range cases {
		got := clampSettings(tc.in)
		if got != tc.want {
			t.Errorf("clamp(%+v) = %+v, want %+v", tc.in, got, tc.want)
		}
		b := got.budget()
		if b.ReservationTTL < 2*got.requestTimeout || b.ReservationTTL <= 6*time.Second {
			t.Errorf("ReservationTTL %v too short for request_timeout %v", b.ReservationTTL, got.requestTimeout)
		}
		if b.WarnPct != got.warnPct || b.ReserveOutputTokens != got.reserveOutput {
			t.Errorf("budget config = %+v from %+v", b, got)
		}
	}
}

func TestModule_InitRegistersSixAdaptersAndRouter(t *testing.T) {
	m, _, _, _ := initModule(t, strings.Repeat("ab", 32))
	kinds := map[string]bool{}
	for _, k := range m.registry.Kinds() {
		kinds[k] = true
	}
	for _, p := range models.Providers {
		if !kinds[p] {
			t.Errorf("no adapter registered for %s", p)
		}
	}
	if m.clientFor(models.ProviderOpenAI) != m.clientFor(models.ProviderOpenAI) || m.clientFor(models.ProviderOpenAI) == m.clientFor(models.ProviderGemini) {
		t.Fatal("one client per provider kind, reused")
	}
	if m.outboxW == nil || m.reconciler == nil || m.purge == nil || m.ops == nil || m.usageH == nil {
		t.Fatal("background jobs and handlers must be wired")
	}
	if m.accounts == nil || m.siwc == nil {
		t.Fatal("the linked-account service and its handler must be wired")
	}
}

// afterCall stamps lastUsedAt only for a successful user_account attempt
// (ruling 2.17); everything else must not touch the accounts store. With
// an offline database a write would log a WARN, so silence proves it.
func TestModule_AfterCallStampsOnlyPlanSuccess(t *testing.T) {
	t.Setenv("LLM_SECRET_ENCRYPTION_KEY", strings.Repeat("ab", 32))
	logs := &bytes.Buffer{}
	reg := module.NewServiceRegistry()
	reg.Register(module.ServiceTenantDirectoryReader, iface.TenantDirectoryReader(noMembers{}))
	m := NewModule()
	if err := m.Init(&module.Dependencies{DB: offlineDB(t), Logger: slog.New(slog.NewTextHandler(logs, nil)), Services: reg}); err != nil {
		t.Fatal(err)
	}
	ua := models.Model{Provider: models.ProviderOpenAI, CredentialRef: models.LLMCredentialRef{Kind: models.CredentialKindUserAccount}}
	org := models.Model{Provider: models.ProviderOpenAI, CredentialRef: models.LLMCredentialRef{Kind: models.CredentialKindOrg, CredentialUUID: "c1"}}
	ctx := operatorCtx()
	m.afterCall(ctx, ua, "acc-1", iface.ErrLLMQuotaExhausted)
	m.afterCall(ctx, ua, "acc-1", iface.ErrLLMUserAccountReauth)
	m.afterCall(ctx, org, "c1", nil)
	if strings.Contains(logs.String(), "last-use") {
		t.Fatalf("a failed or org-billed attempt touched the account: %s", logs.String())
	}
	m.afterCall(ctx, ua, "acc-1", nil)
	if !strings.Contains(logs.String(), "llm siwc: last-use stamp failed") {
		t.Fatalf("a successful plan call must stamp lastUsedAt: %s", logs.String())
	}
}

func TestModule_StartStopIsIdempotent(t *testing.T) {
	m, _, _, _ := initModule(t, "")
	ctx := context.Background()
	if err := m.Start(ctx); err != nil {
		t.Fatal(err)
	}
	if err := m.Start(ctx); err != nil {
		t.Fatal(err)
	}
	stopCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err := m.Stop(stopCtx); err != nil {
		t.Fatalf("Stop = %v", err)
	}
	if err := m.Stop(stopCtx); err != nil {
		t.Fatalf("second Stop = %v", err)
	}
	// Not initialized: both are no-ops.
	if err := NewModule().Start(ctx); err != nil {
		t.Fatal(err)
	}
}

// Every budget alert template exists in en and it.
func TestModule_NotificationTemplatesInEveryLocale(t *testing.T) {
	locales := map[string]map[string]bool{}
	for _, spec := range NewModule().NotificationTemplates() {
		if !strings.HasPrefix(spec.TemplateID, "llm.") {
			t.Errorf("template %s is not llm.-prefixed", spec.TemplateID)
		}
		if locales[spec.TemplateID] == nil {
			locales[spec.TemplateID] = map[string]bool{}
		}
		locales[spec.TemplateID][spec.Locale] = true
	}
	for _, id := range []string{services.TemplateBudgetWarning, services.TemplateBudgetExhausted} {
		if !locales[id]["en"] || !locales[id]["it"] {
			t.Errorf("%s locales = %v", id, locales[id])
		}
	}
}

// The grant-bypassing prompt path is not reachable from the object other
// modules resolve under ServiceLLMGateway.
func TestModule_RegisteredGatewayHasNoGrantBypass(t *testing.T) {
	_, reg, _, _ := initModule(t, "")
	if _, ok := module.GetTyped[services.PromptRunner](reg, module.ServiceLLMGateway); ok {
		t.Fatal("the registered gateway must not expose ChatPinned")
	}
}

// The retention loop runs its first pass after the first delay, then on
// every interval, and stops with Stop.
func TestModule_RetentionLoopRunsAndStops(t *testing.T) {
	m, _, _, _ := initModule(t, "")
	var mu sync.Mutex
	runs := 0
	m.purge = func(context.Context) { mu.Lock(); runs++; mu.Unlock() }
	m.retentionFirst, m.retentionInterval = 5*time.Millisecond, 5*time.Millisecond
	if err := m.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for {
		mu.Lock()
		n := runs
		mu.Unlock()
		if n >= 3 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("retention ran %d times in 5s", n)
		}
		time.Sleep(5 * time.Millisecond)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := m.Stop(ctx); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	after := runs
	mu.Unlock()
	time.Sleep(30 * time.Millisecond)
	mu.Lock()
	defer mu.Unlock()
	if runs != after {
		t.Fatalf("retention kept running after Stop: %d -> %d", after, runs)
	}
}

// runRetention against Mongo: old finalized ledger rows, rollups past 24
// months and settled old alerts go; pending rows, fresh rows and unsettled
// alerts stay.
func TestModule_RunRetentionPurges(t *testing.T) {
	uri := os.Getenv("MONGO_TEST_URI")
	if uri == "" {
		t.Skip("MONGO_TEST_URI not set")
	}
	client, err := mongo.Connect(context.Background(), options.Client().ApplyURI(uri))
	if err != nil {
		t.Fatal(err)
	}
	db := client.Database(fmt.Sprintf("llm_mod_test_%d", time.Now().UnixNano()))
	t.Cleanup(func() { _ = db.Drop(context.Background()); _ = client.Disconnect(context.Background()) })
	usage, monthly, outbox := repository.NewUsage(db), repository.NewMonthly(db), repository.NewOutbox(db)
	id := testkit.NewIdentity("u1", "u1@example.test", "administrator").WithTenant("t1", []string{"org_owner"}, true)
	ctx := ctxauth.WithTenantKind(id.ContextFor(context.Background(), "t1"), "internal")
	old, fresh := time.Now().UTC().AddDate(0, 0, -100), time.Now().UTC()
	for _, ev := range []*models.UsageEvent{
		{UUID: "old-ok", Outcome: models.OutcomeOK, CreatedAt: old},
		{UUID: "old-pending", Outcome: models.OutcomePending, CreatedAt: old},
		{UUID: "new-ok", Outcome: models.OutcomeOK, CreatedAt: fresh},
	} {
		if err := usage.InsertPending(ctx, ev); err != nil {
			t.Fatal(err)
		}
	}
	ancient := models.PeriodOf(time.Now().UTC().AddDate(0, -30, 0))
	for _, p := range []string{ancient, models.PeriodOf(fresh)} {
		if err := monthly.Reserve(ctx, p, 0); err != nil {
			t.Fatal(err)
		}
	}
	for _, ev := range []*models.OutboxEvent{
		{UUID: "ob-old-sent", Period: "2026-01", Threshold: models.ThresholdWarn, Status: models.OutboxStatusSent, CreatedAt: old, NextAttemptAt: old},
		{UUID: "ob-old-pending", Period: "2026-02", Threshold: models.ThresholdWarn, Status: models.OutboxStatusPending, CreatedAt: old, NextAttemptAt: old},
	} {
		if _, err := outbox.InsertUnique(ctx, ev); err != nil {
			t.Fatal(err)
		}
	}
	runRetention(context.Background(), slog.New(slog.NewTextHandler(&bytes.Buffer{}, nil)), 90, usage, monthly, outbox)

	count := func(coll string, extra bson.M) int64 {
		f, err := tenantrepo.Scope(ctx, extra)
		if err != nil {
			t.Fatal(err)
		}
		n, err := db.Collection(coll).CountDocuments(context.Background(), f)
		if err != nil {
			t.Fatal(err)
		}
		return n
	}
	if n := count(repository.CollUsage, bson.M{}); n != 2 || count(repository.CollUsage, bson.M{"uuid": "old-ok"}) != 0 {
		t.Fatalf("usage left = %d (want old-pending + new-ok)", n)
	}
	if count(repository.CollUsageMonthly, bson.M{"period": ancient}) != 0 || count(repository.CollUsageMonthly, bson.M{}) != 1 {
		t.Fatal("rollup retention wrong")
	}
	if count(repository.CollBudgetOutbox, bson.M{"uuid": "ob-old-sent"}) != 0 || count(repository.CollBudgetOutbox, bson.M{"uuid": "ob-old-pending"}) != 1 {
		t.Fatal("outbox retention wrong")
	}
}

// Deferred T8 minor: with no console origin the loopback callback's closed
// redirect would be relative (an API 404), so mounting it WARNs; with an
// origin it does not.
func TestModule_CallbackMountWarnsWithoutAConsoleOrigin(t *testing.T) {
	for _, origin := range []string{"", "http://localhost:8080"} {
		m := moduleForRoutes(t)
		logs := &bytes.Buffer{}
		logger := slog.New(slog.NewTextHandler(logs, nil))
		m.logger = logger
		cfg := func() services.SIWCConfig {
			return services.SIWCConfig{Mode: services.SIWCModeLoopbackLocal, AllowHosted: true, OperatorPublicURL: "http://127.0.0.1:3007"}
		}
		m.siwcCfg = cfg
		vault, err := services.NewVault("", logger)
		if err != nil {
			t.Fatal(err)
		}
		limiter := services.NewRateLimiter(nil, logger)
		accounts := services.NewAccountsService(services.AccountsDeps{Repo: emptyAccounts{}, Vault: vault, Limiter: limiter, Config: cfg, Logger: logger})
		m.siwc = handlers.NewSIWCHandler(accounts, cfg, limiter, nil, origin, logger)
		m.RegisterRoutes(&module.RouteInfo{
			Operator:  &module.APISurface{Audience: module.AudienceOperator, ProtectedRouter: chi.NewRouter(), AuthMW: recordingMW{}},
			Router:    chi.NewRouter(),
			APIConfig: docsless(),
		})
		warned := strings.Contains(logs.String(), "no operator console origin")
		if warned != (origin == "") {
			t.Fatalf("origin %q: warned = %v: %s", origin, warned, logs.String())
		}
	}
}

// Deferred T7 minor: Init wires the router the gateway serves with the
// module's own afterCall hook and a composite resolver over the module's
// accounts service, so the hook and the resolver cannot drift from what the
// tests of each exercise.
func TestModule_InitWiresTheRouterHookAndTheAccountResolver(t *testing.T) {
	t.Setenv("LLM_SECRET_ENCRYPTION_KEY", strings.Repeat("ab", 32))
	logs := &bytes.Buffer{}
	reg := module.NewServiceRegistry()
	reg.Register(module.ServiceTenantDirectoryReader, iface.TenantDirectoryReader(noMembers{}))
	m := NewModule()
	if err := m.Init(&module.Dependencies{DB: offlineDB(t), Logger: slog.New(slog.NewTextHandler(logs, nil)), Services: reg}); err != nil {
		t.Fatal(err)
	}
	r := m.gateway.Router()
	if r == nil || r.AfterCall == nil {
		t.Fatal("the gateway must serve a router with the AfterCall hook")
	}
	cr, ok := r.Credentials().(*services.CompositeCredentialResolver)
	if !ok {
		t.Fatalf("router resolver = %T, want the composite resolver", r.Credentials())
	}
	if cr.Accounts() != services.AccountCredentialSource(m.accounts) || m.accounts == nil {
		t.Fatal("kind=user_account must resolve through the module's accounts service")
	}
	// The hook behaves as module.afterCall: a failed plan call touches
	// nothing, a successful one stamps lastUsedAt (an offline database
	// makes the stamp WARN, which is the observable).
	ua := models.Model{Provider: models.ProviderOpenAI, CredentialRef: models.LLMCredentialRef{Kind: models.CredentialKindUserAccount}}
	r.AfterCall(operatorCtx(), ua, "acc-1", iface.ErrLLMQuotaExhausted)
	if strings.Contains(logs.String(), "last-use") {
		t.Fatalf("a failed attempt touched the account: %s", logs.String())
	}
	r.AfterCall(operatorCtx(), ua, "acc-1", nil)
	if !strings.Contains(logs.String(), "llm siwc: last-use stamp failed") {
		t.Fatalf("the router hook is not module.afterCall: %s", logs.String())
	}
}
