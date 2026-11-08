// Package jobs is the core module that schedules and runs the background
// jobs other modules declare through module.HasJobs (spec J1–J22, ADR-0024).
// It owns the jobs_states and jobs_runs collections, the per-replica
// scheduler (lease, retry, catch-up), the Tier-1 admin API under
// /v1/admin/jobs, the system.jobs.* permissions, the orkestra_jobs_*
// metrics and its own runs_cleanup job. See AGENTS.md for the contract.
package jobs

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"log/slog"
	"os"
	"sync"
	"time"

	"github.com/orkestra/backend/internal/core/jobs/handlers"
	"github.com/orkestra/backend/internal/core/jobs/models"
	"github.com/orkestra/backend/internal/core/jobs/repository"
	"github.com/orkestra/backend/internal/core/jobs/scheduler"
	"github.com/orkestra/backend/internal/core/jobs/services"
	"github.com/orkestra/backend/pkg/sdk/iface"
	sdkjobs "github.com/orkestra/backend/pkg/sdk/jobs"
	"github.com/orkestra/backend/pkg/sdk/metrics"
	"github.com/orkestra/backend/pkg/sdk/module"
)

// Module is the jobs core module.
type Module struct {
	module.BaseModule

	deps    *module.Dependencies
	logger  *slog.Logger
	states  repository.StateRepository
	runs    repository.RunRepository
	svc     *services.AdminService
	handler *handlers.Handler
	tz      *tzCache
	metrics scheduler.Metrics
	now     func() time.Time

	mu       sync.Mutex
	engine   *scheduler.Engine
	loopDone chan struct{}
}

// NewModule returns the jobs module.
func NewModule() *Module {
	return &Module{now: time.Now}
}

var (
	_ module.HasJobs         = (*Module)(nil)
	_ module.Startable       = (*Module)(nil)
	_ module.Stoppable       = (*Module)(nil)
	_ module.HealthCheckable = (*Module)(nil)
	_ module.Routable        = (*Module)(nil)
)

func (m *Module) Name() string        { return "jobs" }
func (m *Module) DisplayName() string { return "Jobs" }
func (m *Module) Description() string {
	return "Background jobs: schedules, leases, retries and run history of the jobs modules declare (ADR-0024)"
}
func (m *Module) Category() module.ModuleCategory { return module.CategoryCore }

// Dependencies: none. The catalog, the module-state reader and the audit
// sink are resolved from the service registry when needed.
func (m *Module) Dependencies() []string { return nil }

func (m *Module) OptionalServices() []module.ServiceKey {
	return []module.ServiceKey{module.ServiceAuditSink}
}

// Permissions are the two system permissions of the admin API.
func (m *Module) Permissions() []iface.PermissionSpec {
	return []iface.PermissionSpec{
		{Key: "system.jobs.read", Module: "jobs", Description: "Read background jobs and their run history", System: true},
		{Key: "system.jobs.manage", Module: "jobs", Description: "Change job schedules, enable or disable jobs and run them on demand", System: true},
	}
}

// Collections declares the platform-level (tenant-less, spec J15) state and
// run collections. jobs_states is keyed by job key; its indexes serve the
// due query. jobs_runs serves the per-job history (keyset on startedAt,_id),
// the status filter and the retention delete.
func (m *Module) Collections() []module.CollectionSpec {
	return []module.CollectionSpec{
		{Name: models.StatesCollection, Indexes: []module.IndexSpec{
			{Keys: map[string]int{"nextRunAt": 1}},
			{Keys: map[string]int{"retryAt": 1}, Sparse: true},
			{Keys: map[string]int{"runRequestedAt": 1}, Sparse: true},
		}},
		{Name: models.RunsCollection, Indexes: []module.IndexSpec{
			{OrderedKeys: []module.IndexKey{{Field: "key", Direction: 1}, {Field: "startedAt", Direction: -1}, {Field: "_id", Direction: -1}}},
			{Keys: map[string]int{"startedAt": 1}},
			{OrderedKeys: []module.IndexKey{{Field: "key", Direction: 1}, {Field: "status", Direction: 1}, {Field: "startedAt", Direction: -1}}},
		}},
	}
}

// Init builds the repositories, the timezone cache, the admin service and
// the handler. The scheduler is built at Start, when the catalog is full.
func (m *Module) Init(deps *module.Dependencies) error {
	if deps == nil || deps.DB == nil {
		return errors.New("jobs: database is required")
	}
	m.deps = deps
	m.logger = deps.Logger
	if m.logger == nil {
		m.logger = slog.Default()
	}
	m.states = repository.NewStateRepository(deps.DB.Collection(models.StatesCollection))
	m.runs = repository.NewRunRepository(deps.DB.Collection(models.RunsCollection))
	m.tz = &tzCache{read: func() string { return deps.GetConfig("jobs", keyTimezone) }, now: m.now, logger: m.logger}
	if m.metrics == nil {
		m.metrics = runMetrics{c: metrics.Default()}
	}
	m.svc = services.New(services.Options{
		Catalog:         m.catalog,
		States:          m.states,
		Runs:            m.runs,
		DefaultLocation: m.tz.Location,
		ModuleStarted:   m.moduleStarted,
		AuditSink:       m.auditSink,
		Wake:            m.wake,
		Now:             m.now,
		Logger:          m.logger,
	})
	m.handler = handlers.New(m.svc, m.logger)
	return nil
}

// catalog is the registry's job catalog (published before Init, filled after
// InitAll and on RetryInit).
func (m *Module) catalog() *sdkjobs.Catalog {
	if m.deps == nil {
		return nil
	}
	c, _ := module.GetTyped[*sdkjobs.Catalog](m.deps.Services, module.ServiceJobCatalog)
	return c
}

// moduleStarted reports the registry's runtime state (core modules count as
// started once StartAll started them). Without a reader every module runs.
func (m *Module) moduleStarted(name string) bool {
	if m.deps == nil {
		return true
	}
	r, ok := module.GetTyped[module.ModuleStatusReader](m.deps.Services, module.ServiceModuleStatus)
	if !ok || r == nil {
		return true
	}
	return r.IsStarted(name)
}

// auditSink resolves the sink per event: compliance registers it in its own
// Init, after this module's.
func (m *Module) auditSink() iface.AuditSink {
	if m.deps == nil {
		return nil
	}
	s, _ := module.GetTyped[iface.AuditSink](m.deps.Services, module.ServiceAuditSink)
	return s
}

func (m *Module) currentEngine() *scheduler.Engine {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.engine
}

func (m *Module) wake() {
	if e := m.currentEngine(); e != nil {
		e.Wake()
	}
}

// replicaID is the hostname plus a short random suffix, so two processes on
// one host (or a restarted container reusing its name) hold distinct leases.
func replicaID() string {
	host, _ := os.Hostname()
	if host == "" {
		host = "replica"
	}
	b := make([]byte, 4)
	_, _ = rand.Read(b)
	return host + "-" + hex.EncodeToString(b)
}

// Start builds the scheduler over the (now complete) catalog and runs its
// loop in a goroutine. The loop's context is detached from ctx, which may be
// request-scoped; Stop owns its lifetime.
func (m *Module) Start(ctx context.Context) error {
	if m.states == nil {
		return errors.New("jobs: module not initialised")
	}
	cat := m.catalog()
	if cat == nil {
		return errors.New("jobs: job catalog is not registered")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.engine != nil {
		select {
		case <-m.loopDone:
		default:
			return nil // already running
		}
	}
	e := scheduler.New(cat, m.states, m.runs, scheduler.Options{
		Replica:         replicaID(),
		Logger:          m.logger,
		MaxConcurrent:   m.maxConcurrent(),
		DefaultLocation: m.tz.Location,
		ModuleStarted:   m.moduleStarted,
		Metrics:         m.metrics,
	})
	done := make(chan struct{})
	loopCtx := context.WithoutCancel(ctx)
	go func() {
		defer close(done)
		e.Start(loopCtx)
	}()
	m.engine, m.loopDone = e, done
	return nil
}

// Stop cancels the running jobs, waits up to the scheduler's grace period
// and releases their leases (spec J18).
func (m *Module) Stop(ctx context.Context) error {
	m.mu.Lock()
	e, done := m.engine, m.loopDone
	m.mu.Unlock()
	if e == nil {
		return nil
	}
	if err := e.Stop(ctx); err != nil {
		return err
	}
	select {
	case <-done:
	case <-ctx.Done():
		return ctx.Err()
	}
	return nil
}

// HealthCheck is degraded when the loop is not running or claims have failed
// for more than five minutes (spec J20).
func (m *Module) HealthCheck(context.Context) error {
	e := m.currentEngine()
	if e == nil {
		return errors.New("jobs: scheduler not started")
	}
	return e.Healthy()
}
