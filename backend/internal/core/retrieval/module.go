// Package retrieval is the core module for corpora, indexing and hybrid
// search over tenant data with local inference (ADR-0023). This step holds
// the corpus registry, the engine contract, the Qdrant driver, the outbox and
// the index worker; search and routes arrive in later steps.
package retrieval

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/orkestra/backend/internal/core/retrieval/engine"
	"github.com/orkestra/backend/internal/core/retrieval/engine/qdrant"
	"github.com/orkestra/backend/internal/core/retrieval/repository"
	"github.com/orkestra/backend/internal/core/retrieval/services"
	"github.com/orkestra/backend/pkg/sdk/iface"
	"github.com/orkestra/backend/pkg/sdk/module"
)

const (
	keyWorkerLease        = "worker_lease"
	keyWorkerBatch        = "worker_batch"
	keyWorkerPollInterval = "worker_poll_interval"

	defaultWorkerLease  = 2 * time.Minute
	defaultWorkerBatch  = 32
	defaultWorkerPoll   = 5 * time.Second
	maxWorkerBatchField = 256
)

const (
	envURL    = "QDRANT_URL"
	envAPIKey = "QDRANT_API_KEY"
	envPrefix = "QDRANT_COLLECTION_PREFIX"

	defaultPrefix = "orkestra"

	healthTimeout = 2 * time.Second
)

// prefixPattern has no "_": collection names are "<prefix>_<corpus>_g<n>" and
// corpus names may hold "_", so an underscore in the prefix would let two
// (prefix, corpus) pairs share a collection ("a_b"+"c" and "a"+"b_c").
var prefixPattern = regexp.MustCompile(`^[a-z][a-z0-9-]{0,31}$`)

// Module is the retrieval core module. The Qdrant settings are operator
// environment, never database or API configuration (ADR-0023 D4/D9).
type Module struct {
	module.BaseModule

	mu     sync.Mutex
	logger *slog.Logger
	svc    *services.Service
	eng    engine.Engine // nil = not configured

	// newEngine builds the vector engine driver; tests replace it.
	newEngine func(qdrant.Config) (engine.Engine, error)

	// worker is nil while indexing is disabled; missing then names why.
	worker  *services.Worker
	missing string

	lifecycleMu sync.Mutex
	cancel      context.CancelFunc
	done        chan struct{}
}

func NewModule() *Module { return &Module{newEngine: qdrantEngine} }

func qdrantEngine(cfg qdrant.Config) (engine.Engine, error) {
	drv, err := qdrant.New(cfg)
	if err != nil {
		return nil, err
	}
	return drv, nil
}

func (m *Module) ConfigGroups() []module.ConfigGroup {
	return []module.ConfigGroup{
		{Key: "indexing", Label: "Indexing", Order: 1, Description: "Outbox worker: lease per key, keys per pass and pass interval."},
	}
}

func (m *Module) ConfigSchema() []module.ConfigField {
	one, maxBatch := 1, maxWorkerBatchField
	return []module.ConfigField{
		{Key: keyWorkerLease, Label: "Worker lease", Group: "indexing", Type: module.FieldDuration, Default: "2m", EnvVar: "RETRIEVAL_WORKER_LEASE",
			Description: "How long a worker owns one item while indexing it (30s–15m). Embedding and engine calls end a quarter of the lease before it expires."},
		{Key: keyWorkerBatch, Label: "Worker batch", Group: "indexing", Type: module.FieldInt, Default: "32", EnvVar: "RETRIEVAL_WORKER_BATCH", Min: &one, Max: &maxBatch,
			Description: "Items indexed per pass, shared round-robin across organizations."},
		{Key: keyWorkerPollInterval, Label: "Worker poll interval", Group: "indexing", Type: module.FieldDuration, Default: "5s", EnvVar: "RETRIEVAL_WORKER_POLL_INTERVAL",
			Description: "Pause between passes (1s–5m)."},
	}
}

func (m *Module) Name() string        { return "retrieval" }
func (m *Module) DisplayName() string { return "Retrieval" }
func (m *Module) Description() string {
	return "Corpora, indexing and hybrid search over tenant data with local inference (ADR-0023)"
}
func (m *Module) Category() module.ModuleCategory { return module.CategoryCore }
func (m *Module) Dependencies() []string          { return []string{"tenant", "authz", "llm"} }
func (m *Module) ProvidedServices() []module.ServiceKey {
	return []module.ServiceKey{module.ServiceRetrieval}
}

// Collections declares the installation-wide corpus state, the per-key
// indexing outbox and the per-tenant initial-build progress.
func (m *Module) Collections() []module.CollectionSpec {
	return []module.CollectionSpec{
		{Name: repository.CollCorpusState},
		{Name: repository.CollOutbox, Indexes: repository.OutboxIndexes()},
		{Name: repository.CollTenantProgress, Indexes: repository.TenantProgressIndexes()},
		{Name: repository.CollErasures, Indexes: repository.ErasureIndexes()},
	}
}

// Init reads the operator environment and registers the service. It never
// contacts Qdrant or llm, and never fails: a core Init error is fatal, so
// missing infrastructure is a log line, not an error.
func (m *Module) Init(deps *module.Dependencies) error {
	logger := deps.Logger
	if logger == nil {
		logger = slog.Default()
	}

	prefix := strings.TrimSpace(os.Getenv(envPrefix))
	switch {
	case prefix == "":
		prefix = defaultPrefix
	case !prefixPattern.MatchString(prefix):
		logger.Warn("retrieval: QDRANT_COLLECTION_PREFIX is invalid; using the default",
			slog.String("default", defaultPrefix))
		prefix = defaultPrefix
	}

	// eng stays an untyped nil unless the driver is built: Service.Engine
	// compares the interface to nil.
	var eng engine.Engine
	if url := strings.TrimSpace(os.Getenv(envURL)); url == "" {
		logger.Info("retrieval: QDRANT_URL is not set; search answers not configured until the retrieval overlay is enabled")
	} else {
		key := strings.TrimSpace(os.Getenv(envAPIKey))
		if key == "" {
			logger.Warn("retrieval: QDRANT_API_KEY is not set")
		}
		build := m.newEngine
		if build == nil {
			build = qdrantEngine
		}
		drv, err := build(qdrant.Config{URL: url, APIKey: key, Logger: logger})
		if err != nil {
			logger.Warn("retrieval: QDRANT_URL is not a valid http(s) URL; the vector engine is treated as not configured")
		} else {
			eng = drv
		}
	}

	svc := services.New(services.Options{Engine: eng, CollectionPrefix: prefix, Logger: logger})

	// The outbox works in every mode, QDRANT_URL unset included: owners
	// enqueue intents that the worker drains once an engine is configured.
	var worker *services.Worker
	missing := ""
	if deps.DB == nil {
		missing = "database"
	} else {
		outbox := repository.NewOutbox(deps.DB)
		states := repository.NewCorpusStates(deps.DB)
		progress := repository.NewTenantProgress(deps.DB)
		erasures := repository.NewErasures(deps.DB)
		svc.SetOutbox(services.NewOutbox(outbox, svc, time.Now))

		var absent []string
		embedder, ok := module.GetTyped[iface.LLMPinnedEmbedder](deps.Services, module.ServiceLLMGateway)
		if !ok {
			absent = append(absent, "LLMPinnedEmbedder")
		}
		profiles, ok := module.GetTyped[iface.LLMInferenceProfiles](deps.Services, module.ServiceLLMGateway)
		if !ok {
			absent = append(absent, "LLMInferenceProfiles")
		}
		tenants, ok := module.GetTyped[iface.TenantIDLister](deps.Services, module.ServiceTenantService)
		if !ok {
			absent = append(absent, "TenantIDLister")
		}
		modules, ok := module.GetTyped[module.ModuleStatusReader](deps.Services, module.ServiceModuleStatus)
		if !ok {
			absent = append(absent, "ModuleStatusReader")
		}
		missing = strings.Join(absent, ", ")
		if eng != nil && missing == "" {
			worker = services.NewWorker(services.WorkerDeps{
				Outbox: outbox, States: states, Progress: progress,
				Tombstones: services.NewTombstones(erasures),
				Catalog:    svc, Engine: eng, Profiles: profiles, Embedder: embedder,
				Modules: modules, Tenants: tenants, Prefix: prefix,
				Settings: func() services.WorkerSettings {
					return services.WorkerSettings{
						Lease:        deps.GetConfigDuration("retrieval", keyWorkerLease, defaultWorkerLease),
						Batch:        deps.GetConfigInt("retrieval", keyWorkerBatch, defaultWorkerBatch),
						PollInterval: deps.GetConfigDuration("retrieval", keyWorkerPollInterval, defaultWorkerPoll),
					}
				},
				Now:    time.Now,
				Logger: logger,
			})
		}
	}
	if deps.DB == nil {
		// Production always has a DB; the repository constructors would
		// panic on a nil handle, so the outbox stays unwired.
		logger.Warn("retrieval: no database handle; the outbox is not wired and indexing is disabled",
			slog.String("missing", missing))
	} else if eng != nil && worker == nil {
		logger.Warn("retrieval: indexing is disabled; the outbox fills but nothing is indexed",
			slog.String("missing", missing))
	}

	m.mu.Lock()
	m.logger, m.svc, m.eng, m.worker, m.missing = logger, svc, eng, worker, missing
	m.mu.Unlock()
	deps.Services.Register(module.ServiceRetrieval, svc)
	return nil
}

// Start seals corpus registration, then starts the index worker. Every
// module's Init has run by now, so a consumer that declares a dependency on
// retrieval has registered; a module retried later cannot register a corpus.
// Sealing comes first so the worker's first pass bootstraps generation 1 over
// the final registry. Idempotent: a second Start while running is a no-op.
func (m *Module) Start(ctx context.Context) error {
	m.mu.Lock()
	svc, worker := m.svc, m.worker
	m.mu.Unlock()
	if svc != nil {
		svc.Seal()
	}
	if worker == nil {
		return nil
	}
	m.lifecycleMu.Lock()
	defer m.lifecycleMu.Unlock()
	if m.cancel != nil {
		select {
		case <-m.done:
		default:
			return nil
		}
	}
	// WithoutCancel: the caller's context may be request-scoped (the admin
	// enable endpoint hands StartModule the HTTP request context), and a
	// worker that died with that response would silently stop indexing.
	// Stop owns the worker's lifetime (precedent: auth's maintenance loop).
	runCtx, cancel := context.WithCancel(context.WithoutCancel(ctx))
	done := make(chan struct{})
	go func() { defer close(done); worker.Run(runCtx) }()
	m.cancel, m.done = cancel, done
	return nil
}

// Stop cancels the worker and waits for it (or for ctx). A key held at that
// moment is still released or rescheduled: the worker's bookkeeping calls
// outlive the cancellation for a few seconds.
func (m *Module) Stop(ctx context.Context) error {
	m.lifecycleMu.Lock()
	cancel, done := m.cancel, m.done
	m.lifecycleMu.Unlock()
	if cancel == nil {
		return nil
	}
	cancel()
	select {
	case <-done:
		m.lifecycleMu.Lock()
		if m.done == done {
			m.cancel, m.done = nil, nil
		}
		m.lifecycleMu.Unlock()
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// HealthCheck reports the vector engine and the worker. Not configured is the
// designed degraded mode, not a fault. The handler of
// /v1/admin/modules/health copies err.Error() into its response, so an engine
// failure is the bare sentinel and the driver error goes to the DEBUG log.
func (m *Module) HealthCheck(ctx context.Context) error {
	m.mu.Lock()
	eng, logger, worker, missing := m.eng, m.logger, m.worker, m.missing
	m.mu.Unlock()
	if eng == nil {
		return nil
	}
	if worker == nil {
		return fmt.Errorf("retrieval: indexing disabled: %s", missing)
	}
	ctx, cancel := context.WithTimeout(ctx, healthTimeout)
	defer cancel()
	if err := eng.Health(ctx); err != nil {
		if logger != nil {
			logger.Debug("retrieval: engine health check failed", slog.String("error", err.Error()))
		}
		return iface.ErrRetrievalUnavailable
	}
	if problems := worker.Problems(); len(problems) > 0 {
		return errors.New("retrieval: " + strings.Join(problems, "; "))
	}
	return nil
}
