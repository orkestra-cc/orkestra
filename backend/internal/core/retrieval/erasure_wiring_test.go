package retrieval

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/orkestra/backend/internal/core/retrieval/models"
	"github.com/orkestra/backend/internal/core/retrieval/repository"
	"github.com/orkestra/backend/internal/core/retrieval/services"
	"github.com/orkestra/backend/pkg/sdk/iface"
	"github.com/orkestra/backend/pkg/sdk/module"
	"go.mongodb.org/mongo-driver/mongo"
)

// fakeHooks is the tenant service as erasure sees it: it records the purge
// hooks registered on it and serves a settable purged-tenant list, counting
// the calls. It also lists active tenants, so indexing resolves.
type fakeHooks struct {
	mu        sync.Mutex
	fns       []func(context.Context, string) error
	purged    []string
	listErr   error
	listCalls int
}

func (h *fakeHooks) RegisterTenantPurgeHook(fn func(context.Context, string) error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.fns = append(h.fns, fn)
}

func (h *fakeHooks) ListPurgedTenantIDs(context.Context) ([]string, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.listCalls++
	return h.purged, h.listErr
}

func (h *fakeHooks) ListActiveTenantIDs(context.Context, string) ([]string, error) { return nil, nil }

func (h *fakeHooks) hooks() []func(context.Context, string) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	return append([]func(context.Context, string) error(nil), h.fns...)
}

func (h *fakeHooks) calls() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.listCalls
}

// listerOnly lists active tenants only (a tenant service predating PR 6).
type listerOnly struct{}

func (listerOnly) ListActiveTenantIDs(context.Context, string) ([]string, error) { return nil, nil }

// hooksOnly registers purge hooks but cannot list purged tenants.
type hooksOnly struct{ listerOnly }

func (hooksOnly) RegisterTenantPurgeHook(func(context.Context, string) error) {}

// recordingAuditSink keeps every event it receives.
type recordingAuditSink struct {
	mu     sync.Mutex
	events []iface.AuditEvent
}

func (s *recordingAuditSink) Emit(_ context.Context, ev iface.AuditEvent) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.events = append(s.events, ev)
}

func (s *recordingAuditSink) byAction(action string) []iface.AuditEvent {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []iface.AuditEvent
	for _, ev := range s.events {
		if ev.Action == action {
			out = append(out, ev)
		}
	}
	return out
}

// initErasure initializes the module without an engine over db, with tenants
// (if not nil) under ServiceTenantService and an empty PII producer registry.
func initErasure(t *testing.T, db *mongo.Database, tenants any) (*Module, *module.ServiceRegistry, *iface.PIIProducerRegistry, *syncBuffer) {
	t.Helper()
	t.Setenv("QDRANT_URL", "")
	var buf syncBuffer
	reg := module.NewServiceRegistry()
	reg.Register(module.ServiceLLMGateway, fullGateway{})
	if tenants != nil {
		reg.Register(module.ServiceTenantService, tenants)
	}
	reg.Register(module.ServiceModuleStatus, module.ModuleStatusReader(fakeStatus{}))
	registerSearchServices(reg)
	producers := iface.NewPIIProducerRegistry()
	reg.Register(module.ServicePIIProducerRegistry, producers)
	m := NewModule()
	if err := m.Init(&module.Dependencies{DB: db, Logger: slog.New(slog.NewTextHandler(&buf, nil)), Services: reg}); err != nil {
		t.Fatalf("Init: %v", err)
	}
	return m, reg, producers, &buf
}

func TestInit_RegistersRetrievalPIIProducer(t *testing.T) {
	_, _, producers, logs := initErasure(t, offlineDB(t), &fakeHooks{})
	list := producers.List()
	if len(list) != 1 || list[0].Subject() != "retrieval" {
		t.Fatalf("producers = %v, want exactly the retrieval producer", list)
	}
	if strings.Contains(logs.String(), "level=WARN") {
		t.Fatalf("unexpected WARN: %s", logs)
	}
}

// Without the compliance registry the producer is skipped silently (llm
// precedent).
func TestInit_WithoutPIIRegistrySkipsSilently(t *testing.T) {
	t.Setenv("QDRANT_URL", "")
	var buf bytes.Buffer
	reg := module.NewServiceRegistry()
	reg.Register(module.ServiceLLMGateway, fullGateway{})
	reg.Register(module.ServiceTenantService, &fakeHooks{})
	reg.Register(module.ServiceModuleStatus, module.ModuleStatusReader(fakeStatus{}))
	registerSearchServices(reg)
	if err := NewModule().Init(&module.Dependencies{DB: offlineDB(t), Logger: slog.New(slog.NewTextHandler(&buf, nil)), Services: reg}); err != nil {
		t.Fatalf("Init: %v", err)
	}
	if strings.Contains(buf.String(), "level=WARN") {
		t.Fatalf("unexpected WARN: %s", buf.String())
	}
}

func TestInit_RegistersTenantPurgeHook(t *testing.T) {
	hooks := &fakeHooks{}
	initErasure(t, offlineDB(t), hooks)
	if n := len(hooks.hooks()); n != 1 {
		t.Fatalf("purge hooks registered = %d, want 1", n)
	}
}

// P16: a tenant service that cannot take the hook or list purged tenants
// leaves Init successful with exactly one WARN naming what is missing.
func TestInit_WithoutTenantPurgeHooksWarnsAndSucceeds(t *testing.T) {
	for _, c := range []struct {
		name    string
		tenants any
		missing []string
		present []string
	}{
		{"nothing registered", nil, []string{"TenantPurgeHooks", "PurgedTenantIDLister"}, nil},
		{"lister of active tenants only", iface.TenantIDLister(listerOnly{}), []string{"TenantPurgeHooks", "PurgedTenantIDLister"}, nil},
		{"hooks without the purged lister", hooksOnly{}, []string{"PurgedTenantIDLister"}, []string{"TenantPurgeHooks"}},
	} {
		t.Run(c.name, func(t *testing.T) {
			_, reg, producers, logs := initErasure(t, offlineDB(t), c.tenants)
			l := warnLines(logs.String(), "tenant purge erasure is not fully wired")
			if len(l) != 1 {
				t.Fatalf("want one tenant purge WARN, got %q (all: %s)", l, logs)
			}
			for _, name := range c.missing {
				if !strings.Contains(l[0], name) {
					t.Errorf("WARN does not name %s: %s", name, l[0])
				}
			}
			for _, name := range c.present {
				if strings.Contains(l[0], name) {
					t.Errorf("WARN names %s, which is present: %s", name, l[0])
				}
			}
			// The rest of erasure is still wired.
			if len(producers.List()) != 1 {
				t.Fatal("PII producer not registered")
			}
			err := service(t, reg).DeleteBySubject(context.Background(), "t1", "crm:42")
			if errors.Is(err, iface.ErrRetrievalNotConfigured) {
				t.Fatalf("DeleteBySubject = %v, want the erasure wired", err)
			}
		})
	}
}

func TestInit_RegisteredServiceAcceptsAuditSink(t *testing.T) {
	_, reg, _, _ := initErasure(t, offlineDB(t), &fakeHooks{})
	if _, ok := module.GetTyped[iface.AuditSinkSetter](reg, module.ServiceRetrieval); !ok {
		t.Fatal("ServiceRetrieval is not an iface.AuditSinkSetter")
	}
}

// stubConfigRepo serves one retrieval config document; only FindByName is
// used by GetConfig (the embedded nil interface panics on anything else).
type stubConfigRepo struct {
	module.ConfigRepository
	doc *module.ModuleConfig
}

func (r stubConfigRepo) FindByName(context.Context, string) (*module.ModuleConfig, error) {
	return r.doc, nil
}

// The production fence never follows a live lease change: the finalizer's
// lease is MaxWorkerLease, whatever worker_lease says.
func TestFinalizerConfig_FenceUsesMaxWorkerLease(t *testing.T) {
	repo := stubConfigRepo{doc: &module.ModuleConfig{
		ModuleName: "retrieval", ActiveEnvironment: "production",
		Environments: map[string]module.EnvironmentConfig{"production": {ConfigValues: map[string]string{keyWorkerLease: "5m"}}},
	}}
	withLease := &module.Dependencies{ConfigService: module.NewModuleConfigService(repo, nil, slog.New(slog.DiscardHandler))}
	if got := withLease.GetConfigDuration("retrieval", keyWorkerLease, defaultWorkerLease); got != 5*time.Minute {
		t.Fatalf("worker_lease = %v, the stub config is not read", got)
	}
	for name, deps := range map[string]*module.Dependencies{"default": {}, "worker_lease 5m": withLease} {
		cfg := finalizerConfig(deps)()
		want := services.FinalizerConfig{
			Lease: services.MaxWorkerLease, WriteTimeout: 30 * time.Second,
			Interval: 15 * time.Second, Batch: 50, Hold: 2 * time.Minute,
		}
		if cfg != want {
			t.Errorf("%s: finalizerConfig = %+v, want %+v", name, cfg, want)
		}
		if f := services.FencingWindow(cfg.Lease, cfg.WriteTimeout); f != 16*time.Minute {
			t.Errorf("%s: fence = %v, want 16m", name, f)
		}
	}
}

// P11: the reconcile loop runs on its own cadence under Start and ends with
// Stop. The lister returns no ids, so the offline database is never queried.
func TestModule_PurgedTenantReconcileRuns(t *testing.T) {
	hooks := &fakeHooks{}
	m, _, _, logs := initErasure(t, offlineDB(t), hooks)
	m.reconcileFirst, m.reconcileInterval = 10*time.Millisecond, 10*time.Millisecond
	if err := m.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "two reconcile runs", func() bool { return hooks.calls() >= 2 })
	stopCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := m.Stop(stopCtx); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	n := hooks.calls()
	time.Sleep(50 * time.Millisecond)
	if hooks.calls() != n {
		t.Fatal("the reconcile loop outlived Stop")
	}
	if strings.Contains(logs.String(), "level=WARN") {
		t.Fatalf("unexpected WARN: %s", logs)
	}
}

// A failed run WARNs once and names no tenant id (the error text may carry
// one, so it is not logged).
func TestModule_PurgedTenantReconcileFailureWarns(t *testing.T) {
	hooks := &fakeHooks{listErr: errors.New("tenant t-secret unreadable")}
	m, _, _, logs := initErasure(t, offlineDB(t), hooks)
	m.reconcileFirst, m.reconcileInterval = 10*time.Millisecond, time.Hour
	if err := m.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	// Wait for the WARN itself: a Stop landing between the lister call and
	// the WARN would skip it (the loop returns quietly once stopping).
	waitFor(t, "the reconcile WARN", func() bool {
		return len(warnLines(logs.String(), "purged-tenant reconciliation failed")) >= 1
	})
	if err := m.Stop(context.Background()); err != nil {
		t.Fatal(err)
	}
	l := warnLines(logs.String(), "purged-tenant reconciliation failed")
	if len(l) != 1 {
		t.Fatalf("want one reconcile WARN, got %q (all: %s)", l, logs)
	}
	if strings.Contains(logs.String(), "t-secret") {
		t.Fatalf("WARN carries the error text: %s", l[0])
	}
}

// syncBuffer is a bytes.Buffer safe for the module's background loops to
// write while the test reads it.
type syncBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (s *syncBuffer) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.Write(p)
}

func (s *syncBuffer) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.String()
}

// Deferred #25: Start runs the erasure finalizer even without an engine (it
// is the only path that takes fencing rows to done), and Stop ends it. Over
// an unreachable database every sweep's claim fails and WARNs, so the WARNs
// count the sweeps.
func TestModule_FinalizerRunsWithoutEngineAndStopsWithStop(t *testing.T) {
	m, _, _, logs := initErasure(t, offlineDB(t), &fakeHooks{})
	if m.eng != nil || m.finalizer == nil {
		t.Fatalf("want no engine and a finalizer, got eng=%v finalizer=%v", m.eng, m.finalizer)
	}
	m.finalizerEvery = 10 * time.Millisecond
	m.reconcileFirst = time.Hour
	if err := m.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	sweeps := func() int { return len(warnLines(logs.String(), "erasure finalization sweep failed")) }
	waitFor(t, "two finalizer sweeps", func() bool { return sweeps() >= 2 })
	stopCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := m.Stop(stopCtx); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	n := sweeps()
	time.Sleep(100 * time.Millisecond)
	if sweeps() != n {
		t.Fatal("the finalizer outlived Stop")
	}
}

// With a database, the finalizer started by Start claims and finalizes a due
// row (no engine and no collection recorded: done at once), and claims
// nothing once Stop has returned.
func TestModule_FinalizerFinalizesDueRowsUntilStop(t *testing.T) {
	db := mongoDB(t)
	m, _, _, _ := initErasure(t, db, &fakeHooks{})
	m.finalizerEvery = 10 * time.Millisecond
	m.reconcileFirst = time.Hour
	store := repository.NewErasureFinalization(db)
	ctx := context.Background()
	first, _, err := store.EnsureSubject(ctx, "t1", "crm:1", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if err := m.Start(ctx); err != nil {
		t.Fatal(err)
	}
	status := func(id string) string {
		e, err := store.Get(ctx, id)
		if err != nil || e == nil {
			t.Fatalf("Get %s: %v %v", id, e, err)
		}
		return e.EffectiveStatus()
	}
	waitFor(t, "the finalizer to close the row", func() bool { return status(first.UUID) == models.ErasureStatusDone })
	stopCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err := m.Stop(stopCtx); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	second, _, err := store.EnsureSubject(ctx, "t1", "crm:2", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	time.Sleep(100 * time.Millisecond)
	if got := status(second.UUID); got != models.ErasureStatusPending {
		t.Fatalf("a row recorded after Stop is %q, want pending (the finalizer outlived Stop)", got)
	}
}

// One audit relay is shared by the finalizer, the erasure service, the PII
// producer's finalizer and the Service: a sink compliance pushes after Init
// receives the events of every erasure path.
func TestInit_ErasurePathsShareTheAuditRelay(t *testing.T) {
	db := mongoDB(t)
	hooks := &fakeHooks{}
	_, reg, producers, _ := initErasure(t, db, hooks)
	svc := service(t, reg)
	if err := svc.RegisterCorpus(validSpec()); err != nil {
		t.Fatal(err)
	}
	sink := &recordingAuditSink{}
	setter, _ := module.GetTyped[iface.AuditSinkSetter](reg, module.ServiceRetrieval)
	setter.SetAuditSink(sink) // as compliance does from its Init (Task 7)
	ctx := context.Background()

	if err := svc.DeleteBySubject(ctx, "t1", "crm:42"); err != nil {
		t.Fatalf("DeleteBySubject: %v", err)
	}
	if got := sink.byAction(services.AuditErasureRecorded); len(got) != 1 || got[0].TenantID != "t1" || got[0].Metadata["kind"] != "subject" {
		t.Fatalf("recorded events = %+v, want one subject erasure of t1", got)
	}
	// No engine and no corpus state: the inline pass closes the row at once.
	if got := sink.byAction(services.AuditErasureFinalized); len(got) != 1 {
		t.Fatalf("finalized events = %+v, want one", got)
	}

	fns := hooks.hooks()
	if len(fns) != 1 {
		t.Fatalf("purge hooks = %d", len(fns))
	}
	if err := fns[0](ctx, "t2"); err != nil {
		t.Fatalf("purge hook: %v", err)
	}
	if got := sink.byAction(services.AuditErasureRecorded); len(got) != 2 || got[1].TenantID != "t2" || got[1].Metadata["kind"] != "tenant" {
		t.Fatalf("recorded events = %+v, want the tenant purge of t2 second", got)
	}

	res, err := producers.List()[0].PurgePersonalData(ctx, "u1", iface.EraseHardDelete)
	if err != nil || res.Pending {
		t.Fatalf("PurgePersonalData = %+v, %v; want done", res, err)
	}
	if got := sink.byAction(services.AuditErasureFinalized); len(got) != 3 {
		t.Fatalf("finalized events = %d, want 3 (subject, tenant, user)", len(got))
	}
}
