package retrieval

import (
	"context"
	"net/http"
	"net/http/httptest"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/go-chi/chi/v5"

	"github.com/orkestra/backend/internal/testkit"
	"github.com/orkestra/backend/pkg/sdk/ctxauth"
	"github.com/orkestra/backend/pkg/sdk/module"
)

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

// docsless is the test API config without the OpenAPI/docs/schema routes,
// so the walk below counts only the module's own operations.
func docsless() huma.Config {
	cfg := huma.DefaultConfig("test", "1.0.0")
	cfg.OpenAPIPath, cfg.DocsPath, cfg.SchemasPath = "", "", ""
	return cfg
}

func operatorCtx() context.Context {
	id := testkit.NewIdentity("u-admin", "admin@example.test", "administrator").WithTenant("t1", []string{"org_owner"}, true)
	return ctxauth.WithTenantKind(id.ContextFor(context.Background(), "t1"), "internal")
}

func mountRoutes(m *Module) *chi.Mux {
	router := chi.NewRouter()
	m.RegisterRoutes(&module.RouteInfo{
		Operator: &module.APISurface{
			Audience:        module.AudienceOperator,
			ProtectedRouter: router,
			AuthMW:          recordingMW{},
		},
		APIConfig: docsless(),
	})
	return router
}

func TestModule_RouteGuards(t *testing.T) {
	m, _, _ := initModule(t)
	router := mountRoutes(m)

	stepUp := "stepup:" + (5 * time.Minute).String()
	cases := []struct {
		method, path string
		want         []string
	}{
		{http.MethodGet, "/v1/admin/retrieval/corpora", []string{"internal", "sys:retrieval.corpus.read"}},
		{http.MethodPost, "/v1/admin/retrieval/corpora/crm/diagnostics", []string{"internal", "sys:retrieval.corpus.admin", "mfa", stepUp}},
		{http.MethodDelete, "/v1/admin/retrieval/corpora/crm", []string{"internal", "sys:retrieval.corpus.admin", "mfa", stepUp}},
		{http.MethodGet, "/v1/admin/retrieval/tenant/indexing", []string{"internal", "perm:retrieval.tenant.read"}},
		{http.MethodGet, "/v1/admin/retrieval/search-log", []string{"internal", "perm:retrieval.searchlog.admin", "mfa", stepUp}},
	}
	used := map[string]bool{}
	for _, tc := range cases {
		t.Run(tc.method+" "+tc.path, func(t *testing.T) {
			var got []string
			req := httptest.NewRequest(tc.method, tc.path, strings.NewReader(`{"query":"x"}`))
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
			t.Errorf("permission %s gates no route", p.Key)
		}
	}
}

// Without a database the routes still mount and answer 503, so the OpenAPI
// document and the route table do not depend on the deployment.
func TestModule_RoutesMountWithoutDatabase(t *testing.T) {
	router := mountRoutes(NewModule())
	req := httptest.NewRequest(http.MethodGet, "/v1/admin/retrieval/corpora", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req.WithContext(operatorCtx()))
	if rec.Code != http.StatusServiceUnavailable || !strings.Contains(rec.Body.String(), "retrieval.unavailable") {
		t.Fatalf("GET = %d %s, want 503 retrieval.unavailable", rec.Code, rec.Body)
	}
}

func TestModule_NavItem(t *testing.T) {
	items := NewModule().NavItems()
	if len(items) != 1 {
		t.Fatalf("%d nav items, want 1", len(items))
	}
	it := items[0]
	if it.Path != "/admin/retrieval" || it.MinRole != "administrator" || it.Tier != "internal" ||
		it.Realm != "platform" || it.Icon != "search" || it.Name != "Retrieval" || !it.Active {
		t.Errorf("nav item = %+v", it)
	}
}

func TestModule_PermissionKeys(t *testing.T) {
	var got []string
	for _, p := range NewModule().Permissions() {
		got = append(got, p.Key)
	}
	slices.Sort(got)
	want := []string{"retrieval.corpus.admin", "retrieval.corpus.read", "retrieval.searchlog.admin", "retrieval.tenant.read"}
	if !slices.Equal(got, want) {
		t.Errorf("permissions = %v, want %v", got, want)
	}
}

// The admin service shares the module's one audit relay: a sink pushed by
// compliance after Init (Service.SetAuditSink) must receive the admin
// events, or they would be audited into nothing.
func TestModule_AdminAuditReachesTheLateSink(t *testing.T) {
	m, reg, _ := initModule(t)
	sink := &recordingAuditSink{}
	service(t, reg).SetAuditSink(sink)

	router := mountRoutes(m)
	req := httptest.NewRequest(http.MethodGet, "/v1/admin/retrieval/search-log", nil)
	router.ServeHTTP(httptest.NewRecorder(), req.WithContext(operatorCtx()))

	// The offline database fails the read, which is audited as a failure.
	evs := sink.byAction("retrieval.searchlog.read")
	if len(evs) != 1 {
		t.Fatalf("%d search-log audit events reached the sink, want 1 (all: %+v)", len(evs), sink.events)
	}
	if evs[0].Outcome != "failure" {
		t.Errorf("outcome = %q, want failure", evs[0].Outcome)
	}
}
