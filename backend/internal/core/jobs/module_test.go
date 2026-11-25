package jobs

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"reflect"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/go-chi/chi/v5"

	"github.com/orkestra/backend/internal/core/jobs/handlers"
	"github.com/orkestra/backend/internal/core/jobs/models"
	"github.com/orkestra/backend/internal/core/jobs/repository"
	"github.com/orkestra/backend/internal/core/jobs/services"
	sharedErrors "github.com/orkestra/backend/internal/shared/errors"
	"github.com/orkestra/backend/internal/shared/middleware"
	"github.com/orkestra/backend/pkg/sdk/ctxauth"
	"github.com/orkestra/backend/pkg/sdk/iface"
	sdkjobs "github.com/orkestra/backend/pkg/sdk/jobs"
	"github.com/orkestra/backend/pkg/sdk/module"
)

var t0 = time.Date(2026, 11, 8, 12, 0, 0, 0, time.UTC)

func TestJobsDeclaresRunsCleanup(t *testing.T) {
	defs := NewModule().Jobs()
	if len(defs) != 1 {
		t.Fatalf("%d jobs, want 1", len(defs))
	}
	d := defs[0].Normalized()
	if err := d.Validate(); err != nil {
		t.Fatalf("runs_cleanup invalid: %v", err)
	}
	if d.Name != "runs_cleanup" || d.Title != "Delete old job runs" || d.Schedule.Kind() != sdkjobs.ScheduleEvery ||
		d.Schedule.Every() != 24*time.Hour || d.DisabledByDefault {
		t.Errorf("runs_cleanup = %+v", d)
	}
	cat := sdkjobs.NewCatalog()
	if err := cat.Register("jobs", defs); err != nil {
		t.Fatalf("register: %v", err)
	}
	if _, ok := cat.Get("jobs.runs_cleanup"); !ok {
		t.Error("key jobs.runs_cleanup not registered")
	}
}

func TestPermissions(t *testing.T) {
	var keys []string
	for _, p := range NewModule().Permissions() {
		if p.Module != "jobs" || !p.System || p.Description == "" {
			t.Errorf("permission %+v", p)
		}
		keys = append(keys, p.Key)
	}
	if want := []string{"system.jobs.read", "system.jobs.manage"}; !slices.Equal(keys, want) {
		t.Errorf("keys = %v, want %v", keys, want)
	}
}

func TestDeclarations(t *testing.T) {
	m := NewModule()
	if m.Name() != "jobs" || m.Category() != module.CategoryCore || len(m.Dependencies()) != 0 {
		t.Errorf("name/category/deps = %s %s %v", m.Name(), m.Category(), m.Dependencies())
	}
	if err := module.ValidateConfigDeclarations(m.ConfigSchema(), m.ConfigGroups()); err != nil {
		t.Errorf("config declarations: %v", err)
	}
	names := map[string]int{}
	for _, c := range m.Collections() {
		names[c.Name] = len(c.Indexes)
	}
	if names[models.StatesCollection] != 3 || names[models.RunsCollection] != 3 {
		t.Errorf("collections = %v", names)
	}
}

func TestValidateConfigSnapshot(t *testing.T) {
	m := NewModule()
	ok := map[string]string{"timezone": "Europe/Rome", "maxConcurrentRuns": "32", "runRetentionDays": "1"}
	if err := m.ValidateConfigSnapshot(context.Background(), module.ConfigValidationSnapshot{EffectiveValues: ok}); err != nil {
		t.Fatalf("valid snapshot: %v", err)
	}
	for _, bad := range []map[string]string{
		{"timezone": "Mars/Olympus"},
		{"timezone": "Local"},
		{"maxConcurrentRuns": "0"},
		{"maxConcurrentRuns": "33"},
		{"runRetentionDays": "366"},
		{"runRetentionDays": "ten"},
	} {
		err := m.ValidateConfigSnapshot(context.Background(), module.ConfigValidationSnapshot{EffectiveValues: bad})
		var ve *module.ConfigValidationError
		if !errors.As(err, &ve) {
			t.Errorf("%v: err = %v, want a ConfigValidationError", bad, err)
		}
	}
}

func TestTimezoneCache(t *testing.T) {
	now := t0
	name := "Europe/Rome"
	reads := 0
	c := &tzCache{read: func() string { reads++; return name }, now: func() time.Time { return now }, logger: slog.New(slog.NewTextHandler(io.Discard, nil))}
	if l := c.Location(); l.String() != "Europe/Rome" {
		t.Fatalf("loc = %v", l)
	}
	name = "America/New_York"
	now = now.Add(tzRefresh - time.Second)
	if l := c.Location(); l.String() != "Europe/Rome" || reads != 1 {
		t.Errorf("within refresh: loc=%v reads=%d", l, reads)
	}
	now = now.Add(2 * time.Second)
	if l := c.Location(); l.String() != "America/New_York" || reads != 2 {
		t.Errorf("after refresh: loc=%v reads=%d", l, reads)
	}
	name = "Nowhere/City"
	now = now.Add(tzRefresh)
	if l := c.Location(); l != time.UTC {
		t.Errorf("invalid name: loc=%v, want UTC", l)
	}
}

// --- runs_cleanup -------------------------------------------------------------

type fakeRuns struct {
	repository.RunRepository
	cutoff time.Time
	n      int64
	err    error
}

func (f *fakeRuns) DeleteOlderThan(_ context.Context, cutoff time.Time) (int64, error) {
	f.cutoff = cutoff
	return f.n, f.err
}

func TestRunsCleanupDeletesPastRetention(t *testing.T) {
	runs := &fakeRuns{n: 7}
	m := NewModule()
	m.runs = runs
	m.now = func() time.Time { return t0 }
	run := sdkjobs.NewRun("jobs.runs_cleanup", "r1", sdkjobs.TriggerSchedule, 1, t0)
	if err := m.Jobs()[0].Run(context.Background(), run); err != nil {
		t.Fatal(err)
	}
	if want := t0.Add(-30 * 24 * time.Hour); !runs.cutoff.Equal(want) {
		t.Errorf("cutoff = %v, want %v (default 30 days)", runs.cutoff, want)
	}
	if d, _ := run.Details(); d["deleted"] != int64(7) {
		t.Errorf("details = %v", d)
	}
	runs.err = errors.New("mongo down")
	if err := m.Jobs()[0].Run(context.Background(), sdkjobs.NewRun("jobs.runs_cleanup", "r2", sdkjobs.TriggerSchedule, 1, t0)); err == nil {
		t.Error("a failed delete reported success")
	}
}

// --- routes -------------------------------------------------------------------

type fakeStates struct {
	repository.StateRepository
	mu   sync.Mutex
	docs map[string]models.State
}

func (f *fakeStates) Get(_ context.Context, key string) (*models.State, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	s, ok := f.docs[key]
	if !ok {
		return nil, repository.ErrNotFound
	}
	return &s, nil
}

func (f *fakeStates) List(context.Context) ([]models.State, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []models.State
	for _, s := range f.docs {
		out = append(out, s)
	}
	return out, nil
}

func (f *fakeStates) SetOverride(_ context.Context, key string, _ int64, o *models.Override, fp string, next *time.Time, _ string, _ time.Time) (*models.State, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	s := f.docs[key]
	s.Override, s.Fingerprint, s.NextRunAt = o, fp, next
	s.Revision++
	f.docs[key] = s
	return &s, nil
}

func (f *fakeStates) RequestRun(_ context.Context, key, by string, now time.Time) (*models.State, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	s := f.docs[key]
	s.RunRequestedAt, s.RunRequestedBy = &now, by
	f.docs[key] = s
	return &s, nil
}

func (f *fakeRuns) List(context.Context, string, string, *repository.RunCursor, int) ([]models.RunRecord, *repository.RunCursor, error) {
	return nil, nil, nil
}

// routedModule is a module whose admin service runs over in-memory state
// holding jobs.runs_cleanup.
func routedModule(t *testing.T) *Module {
	t.Helper()
	m := NewModule()
	cat := sdkjobs.NewCatalog()
	if err := cat.Register("jobs", m.Jobs()); err != nil {
		t.Fatal(err)
	}
	states := &fakeStates{docs: map[string]models.State{"jobs.runs_cleanup": {Key: "jobs.runs_cleanup", Module: "jobs", Revision: 1}}}
	m.svc = services.New(services.Options{Catalog: func() *sdkjobs.Catalog { return cat }, States: states, Runs: &fakeRuns{}, Now: func() time.Time { return t0 }})
	m.handler = handlers.New(m.svc, slog.New(slog.NewTextHandler(io.Discard, nil)))
	return m
}

func docsless() huma.Config {
	cfg := huma.DefaultConfig("test", "1.0.0")
	cfg.OpenAPIPath, cfg.DocsPath, cfg.SchemasPath = "", "", ""
	return cfg
}

func mount(m *Module, mw module.RoleMiddleware) *chi.Mux {
	router := chi.NewRouter()
	m.RegisterRoutes(&module.RouteInfo{
		Operator:  &module.APISurface{Audience: module.AudienceOperator, ProtectedRouter: router, AuthMW: mw},
		APIConfig: docsless(),
	})
	return router
}

type guardsKey struct{}

// recordingMW records the gates a request crosses and lets it through.
type recordingMW struct{}

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

var routeTable = []struct {
	method, path, body, gate string
}{
	{http.MethodGet, "/v1/admin/jobs", "", "sys:" + PermRead},
	{http.MethodGet, "/v1/admin/jobs/jobs.runs_cleanup", "", "sys:" + PermRead},
	{http.MethodGet, "/v1/admin/jobs/jobs.runs_cleanup/runs", "", "sys:" + PermRead},
	{http.MethodPost, "/v1/admin/jobs/schedule-preview", `{"kind":"cron","cron":"0 2 * * *"}`, "sys:" + PermRead},
	{http.MethodPatch, "/v1/admin/jobs/jobs.runs_cleanup", `{"revision":1,"enabled":false}`, "sys:" + PermManage},
	{http.MethodDelete, "/v1/admin/jobs/jobs.runs_cleanup/override?revision=1", "", "sys:" + PermManage},
	{http.MethodPost, "/v1/admin/jobs/jobs.runs_cleanup/run", "", "sys:" + PermManage},
}

func TestRouteGuards(t *testing.T) {
	router := mount(routedModule(t), recordingMW{})
	used := map[string]bool{}
	for _, tc := range routeTable {
		t.Run(tc.method+" "+tc.path, func(t *testing.T) {
			var got []string
			req := httptest.NewRequest(tc.method, tc.path, strings.NewReader(tc.body))
			req.Header.Set("Content-Type", "application/json")
			req = req.WithContext(context.WithValue(req.Context(), guardsKey{}, &got))
			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, req)
			if rec.Code == http.StatusNotFound && len(got) == 0 || rec.Code == http.StatusMethodNotAllowed {
				t.Fatalf("route not mounted (status %d)", rec.Code)
			}
			if !reflect.DeepEqual(got, []string{tc.gate}) {
				t.Fatalf("gates = %v, want [%s]", got, tc.gate)
			}
			used[strings.TrimPrefix(tc.gate, "sys:")] = true
		})
	}
	mounted := 0
	_ = chi.Walk(router, func(string, string, http.Handler, ...func(http.Handler) http.Handler) error {
		mounted++
		return nil
	})
	if mounted != len(routeTable) {
		t.Errorf("%d routes mounted, %d asserted", mounted, len(routeTable))
	}
	for _, p := range NewModule().Permissions() {
		if !used[p.Key] {
			t.Errorf("permission %s gates no route", p.Key)
		}
	}
}

// readOnlyAuthz grants system.jobs.read and nothing else.
type readOnlyAuthz struct{}

func (readOnlyAuthz) HasPermission(_ context.Context, _, tenantID, permission string) (bool, error) {
	return tenantID == "" && permission == PermRead, nil
}
func (readOnlyAuthz) GetEffectivePermissions(context.Context, string, string) ([]string, error) {
	return nil, nil
}
func (readOnlyAuthz) RegisterPermissions(context.Context, []iface.PermissionSpec) error { return nil }

// An operator holding system.jobs.read but not system.jobs.manage reads jobs
// and previews schedules, and is refused every change.
func TestReadOnlyOperatorCannotManage(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	authMW := middleware.NewAuthMiddleware(nil, sharedErrors.NewManager(logger, true))
	authMW.SetAuthzProvider(readOnlyAuthz{})
	router := mount(routedModule(t), authMW)

	for _, tc := range routeTable {
		t.Run(tc.method+" "+tc.path, func(t *testing.T) {
			req := httptest.NewRequest(tc.method, tc.path, strings.NewReader(tc.body))
			req.Header.Set("Content-Type", "application/json")
			req = req.WithContext(context.WithValue(req.Context(), ctxauth.KeyUserUUID, "u-reader"))
			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, req)
			want := http.StatusOK
			if strings.HasSuffix(tc.gate, PermManage) {
				want = http.StatusForbidden
			}
			if rec.Code != want {
				t.Fatalf("status = %d, want %d; body = %s", rec.Code, want, rec.Body)
			}
		})
	}
}
