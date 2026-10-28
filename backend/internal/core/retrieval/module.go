// Package retrieval is the core module for corpora, indexing and hybrid
// search over tenant data with local inference (ADR-0023). It holds the
// corpus registry, the engine contract, the Qdrant driver, the outbox, the
// index worker, the search pipeline with its principal resolver and search
// log, the search-log retention loop, and erasure (tombstones, the
// finalizer, the tenant purge). The admin routes arrive in PR 7.
package retrieval

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"regexp"
	"strconv"
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
	keyPrincipalCap           = "principal_cap"
	keySearchLogRetentionDays = "search_log_retention_days"

	minPrincipalCap           = 16
	maxPrincipalCap           = 4096
	minSearchLogRetentionDays = 1

	// The search-log purge first runs a minute after Start, then daily
	// (precedent: llm's usage retention).
	searchLogRetentionFirstRun = time.Minute
	searchLogRetentionEvery    = 24 * time.Hour
)

const (
	// The erasure finalizer's fence follows the longest lease a worker can
	// hold, never the live worker_lease (ADR-0023 D12): FencingWindow(15m,
	// 30s) = 16m. No config key is read.
	finalizerWriteTimeout = 30 * time.Second
	finalizerInterval     = 15 * time.Second
	finalizerBatch        = 50
	finalizerHold         = 2 * time.Minute

	// Purged tenants without a tombstone are reconciled a minute after
	// Start, then every 10 minutes (P11).
	purgedTenantReconcileFirstRun = time.Minute
	purgedTenantReconcileEvery    = 10 * time.Minute
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

	// purger is the search log, nil without a database; settings reads the
	// search config on every use (hot reload).
	purger   services.SearchLogPurger
	settings func() searchSettings
	// retention cadence; zero means the defaults (test seam).
	retentionFirst, retentionInterval time.Duration

	// finalizer and erasure are nil without a database: the erasure
	// finalization sweep and the purged-tenant reconciliation.
	finalizer *services.Finalizer
	erasure   *services.ErasureService
	// reconcile cadence; zero means the defaults (test seam).
	reconcileFirst, reconcileInterval time.Duration

	// One cancel and one done channel cover every background job (the
	// worker, the retention loop, the finalizer and the reconcile loop).
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
		{Key: "search", Label: "Search & audit", Order: 2, Description: "Principal cap per search and how long the search log is kept."},
	}
}

func (m *Module) ConfigSchema() []module.ConfigField {
	one, maxBatch := 1, maxWorkerBatchField
	minCap, maxCap := minPrincipalCap, maxPrincipalCap
	minDays, maxDays := minSearchLogRetentionDays, services.MaxSearchLogRetentionDays
	return []module.ConfigField{
		{Key: keyWorkerLease, Label: "Worker lease", Group: "indexing", Type: module.FieldDuration, Default: "2m", EnvVar: "RETRIEVAL_WORKER_LEASE",
			Description: "How long a worker owns one item while indexing it (30s–15m). Embedding and engine calls end a quarter of the lease before it expires."},
		{Key: keyWorkerBatch, Label: "Worker batch", Group: "indexing", Type: module.FieldInt, Default: "32", EnvVar: "RETRIEVAL_WORKER_BATCH", Min: &one, Max: &maxBatch,
			Description: "Items indexed per pass, shared round-robin across organizations."},
		{Key: keyWorkerPollInterval, Label: "Worker poll interval", Group: "indexing", Type: module.FieldDuration, Default: "5s", EnvVar: "RETRIEVAL_WORKER_POLL_INTERVAL",
			Description: "Pause between passes (1s–5m)."},
		{Key: keyPrincipalCap, Label: "Principal cap", Group: "search", Type: module.FieldInt,
			Default: strconv.Itoa(services.DefaultPrincipalCap), EnvVar: "RETRIEVAL_PRINCIPAL_CAP", Min: &minCap, Max: &maxCap,
			Description: "Maximum number of principals (user, tenant, roles, contributors) a search may carry; a caller above it gets no results until the cap is raised. Fails closed."},
		{Key: keySearchLogRetentionDays, Label: "Search log retention (days)", Group: "search", Type: module.FieldInt,
			Default: strconv.Itoa(services.DefaultSearchLogRetentionDays), EnvVar: "RETRIEVAL_SEARCH_LOG_RETENTION_DAYS", Min: &minDays, Max: &maxDays,
			Description: "Search-log rows older than this are deleted daily. The log records who searched for what and which people the results named (ADR-0023 D12)."},
	}
}

// searchSettings are the search config keys, read on every use.
type searchSettings struct{ principalCap, retentionDays int }

// clampSearchSettings applies the defaults and bounds: principalCap 16..4096
// (<= 0: 256), retentionDays 1..3650 (<= 0: 90). The admin form enforces the
// same bounds; this guards a value set around it (env seed, direct write).
func clampSearchSettings(s searchSettings) searchSettings {
	switch {
	case s.principalCap <= 0:
		s.principalCap = services.DefaultPrincipalCap
	case s.principalCap < minPrincipalCap:
		s.principalCap = minPrincipalCap
	case s.principalCap > maxPrincipalCap:
		s.principalCap = maxPrincipalCap
	}
	switch {
	case s.retentionDays <= 0:
		s.retentionDays = services.DefaultSearchLogRetentionDays
	case s.retentionDays > services.MaxSearchLogRetentionDays:
		s.retentionDays = services.MaxSearchLogRetentionDays
	}
	return s
}

// readSearchSettings reads and clamps the search config keys.
func readSearchSettings(deps *module.Dependencies) searchSettings {
	return clampSearchSettings(searchSettings{
		principalCap:  deps.GetConfigInt("retrieval", keyPrincipalCap, services.DefaultPrincipalCap),
		retentionDays: deps.GetConfigInt("retrieval", keySearchLogRetentionDays, services.DefaultSearchLogRetentionDays),
	})
}

// searchServices are the other modules' services search and indexing use; a
// field is nil when its service is not registered.
type searchServices struct {
	authz    iface.AuthzProvider
	roles    iface.AuthzRoleLister
	members  iface.TenantProvider
	profiles iface.LLMInferenceProfiles
	embedder iface.LLMPinnedEmbedder
	reranker iface.LLMReranker
}

// resolveSearchServices resolves them once: the authz provider and its role
// lister from ServiceAuthzProvider, the membership checker from
// ServiceTenantProvider, and the inference companions by type assertion on
// the ServiceLLMGateway value.
func resolveSearchServices(reg *module.ServiceRegistry) searchServices {
	var s searchServices
	s.authz, _ = module.GetTyped[iface.AuthzProvider](reg, module.ServiceAuthzProvider)
	s.roles, _ = module.GetTyped[iface.AuthzRoleLister](reg, module.ServiceAuthzProvider)
	s.members, _ = module.GetTyped[iface.TenantProvider](reg, module.ServiceTenantProvider)
	s.profiles, _ = module.GetTyped[iface.LLMInferenceProfiles](reg, module.ServiceLLMGateway)
	s.embedder, _ = module.GetTyped[iface.LLMPinnedEmbedder](reg, module.ServiceLLMGateway)
	s.reranker, _ = module.GetTyped[iface.LLMReranker](reg, module.ServiceLLMGateway)
	return s
}

// missing names the unresolved services: first those whose absence fails
// every search closed, then the inference companions, whose absence degrades.
func (s searchServices) missing(modules module.ModuleStatusReader) []string {
	var out []string
	for _, c := range []struct {
		absent bool
		name   string
	}{
		{s.authz == nil, "AuthzProvider"},
		{s.roles == nil, "AuthzRoleLister"},
		{s.members == nil, "TenantProvider"},
		{modules == nil, "ModuleStatusReader"},
		{s.profiles == nil, "LLMInferenceProfiles"},
		{s.embedder == nil, "LLMPinnedEmbedder"},
		{s.reranker == nil, "LLMReranker"},
	} {
		if c.absent {
			out = append(out, c.name)
		}
	}
	return out
}

func (m *Module) Name() string        { return "retrieval" }
func (m *Module) DisplayName() string { return "Retrieval" }
func (m *Module) Description() string {
	return "Corpora, indexing and hybrid search over tenant data with local inference (ADR-0023)"
}
func (m *Module) Category() module.ModuleCategory { return module.CategoryCore }
func (m *Module) Dependencies() []string          { return []string{"tenant", "authz", "llm"} }

// HotReloadConfig is true: every config key is read per pass or per call (the
// worker's Settings closure re-reads lease, batch and poll interval on every
// pass). The Qdrant settings are environment, not module config.
func (m *Module) HotReloadConfig() bool { return true }
func (m *Module) ProvidedServices() []module.ServiceKey {
	return []module.ServiceKey{module.ServiceRetrieval}
}

// Collections declares the installation-wide corpus state, the per-key
// indexing outbox, the per-tenant initial-build progress, the erasure
// tombstones and the search log.
func (m *Module) Collections() []module.CollectionSpec {
	return []module.CollectionSpec{
		{Name: repository.CollCorpusState},
		{Name: repository.CollOutbox, Indexes: repository.OutboxIndexes()},
		{Name: repository.CollTenantProgress, Indexes: repository.TenantProgressIndexes()},
		{Name: repository.CollErasures, Indexes: repository.ErasureIndexes()},
		{Name: repository.CollSearchLog, Indexes: repository.SearchLogIndexes()},
	}
}

// finalizerConfig is the erasure finalizer's configuration: constants, with
// the fence derived from services.MaxWorkerLease, so a live worker_lease
// change never shortens it (a worker that claimed under the old lease may
// still write). deps is not read.
func finalizerConfig(_ *module.Dependencies) func() services.FinalizerConfig {
	return func() services.FinalizerConfig {
		return services.FinalizerConfig{
			Lease:        services.MaxWorkerLease,
			WriteTimeout: finalizerWriteTimeout,
			Interval:     finalizerInterval,
			Batch:        finalizerBatch,
			Hold:         finalizerHold,
		}
	}
}

// corpusOwner reports whether owner registered a corpus; it reads the
// registry at call time, so corpora registered after Init count.
func corpusOwner(svc *services.Service) func(owner string) bool {
	return func(owner string) bool {
		for _, c := range svc.Corpora() {
			if c.Owner == owner {
				return true
			}
		}
		return false
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

	// The outbox is wired whenever a database handle is present, QDRANT_URL
	// unset included: owners enqueue intents that the worker drains once an
	// engine is configured. With a nil DB it stays unwired (Enqueue errors).
	var (
		worker    *services.Worker
		purger    services.SearchLogPurger
		finalizer *services.Finalizer
		erasure   *services.ErasureService
		settings  = func() searchSettings { return readSearchSettings(deps) }
	)
	missing := ""
	if deps.DB == nil {
		missing = "database"
	} else {
		outbox := repository.NewOutbox(deps.DB)
		states := repository.NewCorpusStates(deps.DB)
		progress := repository.NewTenantProgress(deps.DB)
		tombstones := services.NewTombstones(repository.NewErasures(deps.DB))
		searchLog := repository.NewSearchLog(deps.DB)
		svc.SetOutbox(services.NewOutbox(outbox, svc, time.Now))

		svcs := resolveSearchServices(deps.Services)
		var absent []string
		if svcs.embedder == nil {
			absent = append(absent, "LLMPinnedEmbedder")
		}
		if svcs.profiles == nil {
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
				Tombstones: tombstones,
				Catalog:    svc, Engine: eng, Profiles: svcs.profiles, Embedder: svcs.embedder,
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

		// Search is wired whenever a database handle is present, QDRANT_URL
		// unset included: Tombstones and the search log are database-backed,
		// so an engine-less search passes the gate and the visibility guard,
		// answers ErrRetrievalNotConfigured at step 2 and leaves its row.
		// A missing service stays nil and fails closed (or degrades) per
		// search; Init never errors and contacts nothing (ADR D1).
		resolver := &services.PrincipalResolver{
			// A nil iface.TenantProvider converts to a nil MembershipChecker
			// (interface to interface), so the resolver's == nil check holds.
			Members:      svcs.members,
			Roles:        svcs.roles,
			Contributors: svc.PrincipalContributors,
			Cap:          func() int { return settings().principalCap },
		}
		svc.SetSearch(services.NewSearcher(services.SearchDeps{
			Corpora:    svc,
			State:      services.NewCorpusStateReader(states, prefix),
			Engine:     eng,
			Tombstones: tombstones,
			Authz:      svcs.authz,
			Principals: resolver,
			Profiles:   svcs.profiles,
			Embedder:   svcs.embedder,
			Reranker:   svcs.reranker,
			Modules:    modules,
			Log:        searchLog,
			Now:        time.Now,
			Logger:     logger,
		}), resolver)
		purger = searchLog
		if gaps := svcs.missing(modules); len(gaps) > 0 {
			logger.Warn("retrieval: search is missing services; searches fail closed without the authorization, membership or module-status ones and degrade without the inference ones",
				slog.String("missing", strings.Join(gaps, ", ")))
		}

		finalizer, erasure = wireErasure(deps, svc, states, eng, prefix, logger)
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
	m.purger, m.settings = purger, settings
	m.finalizer, m.erasure = finalizer, erasure
	m.mu.Unlock()
	deps.Services.Register(module.ServiceRetrieval, svc)
	return nil
}

// wireErasure builds erasure over the database (ADR-0023 D12): the finalizer,
// the erasure service behind DeleteBySubject and the tenant purge hook, and
// the PII producer. One audit relay serves all of them and the Service, so
// the sink compliance pushes later through Service.SetAuditSink reaches every
// erasure event. It contacts nothing. A missing PII registry is skipped
// silently (compliance absent); a tenant service that cannot take the purge
// hook or list purged tenants gives one WARN.
func wireErasure(deps *module.Dependencies, svc *services.Service, states *repository.CorpusStates,
	eng engine.Engine, prefix string, logger *slog.Logger) (*services.Finalizer, *services.ErasureService) {
	relay := services.NewAuditRelay()
	store := repository.NewErasureFinalization(deps.DB)
	logs := repository.NewSearchLogDSR(deps.DB)
	finalizer := services.NewFinalizer(store, logs, services.NewLiveCollections(states, prefix), eng,
		finalizerConfig(deps), relay, time.Now, logger)

	hooks, hooksOK := module.GetTyped[iface.TenantPurgeHooks](deps.Services, module.ServiceTenantService)
	// purged stays a nil interface when absent: reconciliation is then a no-op.
	purged, purgedOK := module.GetTyped[iface.PurgedTenantIDLister](deps.Services, module.ServiceTenantService)
	erasure := services.NewErasureService(store, finalizer, corpusOwner(svc), purged, relay, time.Now, logger)
	svc.SetAuditRelay(relay)
	svc.SetErasure(erasure)

	if reg, ok := module.GetTyped[*iface.PIIProducerRegistry](deps.Services, module.ServicePIIProducerRegistry); ok {
		reg.Register(services.NewPIIProducer(store, logs, finalizer, time.Now, logger))
	}
	if hooksOK {
		hooks.RegisterTenantPurgeHook(erasure.PurgeTenant)
	}
	var absent []string
	if !hooksOK {
		absent = append(absent, "TenantPurgeHooks")
	}
	if !purgedOK {
		absent = append(absent, "PurgedTenantIDLister")
	}
	if len(absent) > 0 {
		logger.Warn("retrieval: tenant purge erasure is not fully wired",
			slog.String("missing", strings.Join(absent, ", ")))
	}
	return finalizer, erasure
}

// Start seals corpus registration, then starts the background jobs: the
// index worker, when built, and, whenever a database handle is present
// (engine or not), the search-log retention loop, the erasure finalizer and
// the purged-tenant reconcile loop.
// Every module's Init has run by now, so a consumer that declares a
// dependency on retrieval has registered; a module retried later cannot
// register a corpus. Sealing comes first so the worker's first pass
// bootstraps generation 1 over the final registry. Idempotent: a second
// Start while running is a no-op.
func (m *Module) Start(ctx context.Context) error {
	m.mu.Lock()
	svc, worker, purger := m.svc, m.worker, m.purger
	finalizer, erasure := m.finalizer, m.erasure
	m.mu.Unlock()
	if svc != nil {
		svc.Seal()
	}
	if worker == nil && purger == nil && finalizer == nil {
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
	// Stop owns the jobs' lifetime (precedent: auth's maintenance loop).
	runCtx, cancel := context.WithCancel(context.WithoutCancel(ctx))
	done := make(chan struct{})
	var wg sync.WaitGroup
	if worker != nil {
		wg.Add(1)
		go func() { defer wg.Done(); worker.Run(runCtx) }()
	}
	if purger != nil {
		wg.Add(1)
		go func() { defer wg.Done(); m.searchLogRetentionLoop(runCtx) }()
	}
	if finalizer != nil {
		wg.Add(1)
		go func() { defer wg.Done(); finalizer.Run(runCtx) }()
	}
	if erasure != nil {
		wg.Add(1)
		go func() { defer wg.Done(); m.purgedTenantReconcileLoop(runCtx) }()
	}
	go func() { wg.Wait(); close(done) }()
	m.cancel, m.done = cancel, done
	return nil
}

// searchLogRetentionLoop purges the search log a minute after Start, then
// every 24 hours, until ctx ends.
func (m *Module) searchLogRetentionLoop(ctx context.Context) {
	first, every := m.retentionFirst, m.retentionInterval
	if first <= 0 {
		first = searchLogRetentionFirstRun
	}
	if every <= 0 {
		every = searchLogRetentionEvery
	}
	t := time.NewTimer(first)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			m.purgeSearchLog(ctx)
			t.Reset(every)
		}
	}
}

// purgeSearchLog deletes every tenant's rows older than
// search_log_retention_days (read now, so a change applies at the next run).
// A failure is logged and retried at the next run.
func (m *Module) purgeSearchLog(ctx context.Context) {
	m.mu.Lock()
	purger, settings, logger := m.purger, m.settings, m.logger
	m.mu.Unlock()
	if purger == nil || settings == nil {
		return
	}
	days := settings().retentionDays
	n, err := services.PurgeSearchLog(ctx, purger, days, time.Now())
	if err != nil {
		if ctx.Err() != nil {
			return // stopping
		}
		logger.WarnContext(ctx, "retrieval: search log retention purge failed",
			slog.Int("retention_days", days), slog.Int64("deleted", n), slog.String("error", err.Error()))
		return
	}
	logger.InfoContext(ctx, "retrieval: search log retention purge",
		slog.Int("retention_days", days), slog.Int64("deleted", n))
}

// purgedTenantReconcileLoop writes the tombstone of every purged tenant that
// has none (a purge hook that never ran or failed) a minute after Start, then
// every 10 minutes, until ctx ends.
func (m *Module) purgedTenantReconcileLoop(ctx context.Context) {
	first, every := m.reconcileFirst, m.reconcileInterval
	if first <= 0 {
		first = purgedTenantReconcileFirstRun
	}
	if every <= 0 {
		every = purgedTenantReconcileEvery
	}
	t := time.NewTimer(first)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			m.reconcilePurgedTenants(ctx)
			t.Reset(every)
		}
	}
}

// reconcilePurgedTenants runs one reconciliation. A failure WARNs once
// without the error text, which may name a tenant, and is retried at the next
// run.
func (m *Module) reconcilePurgedTenants(ctx context.Context) {
	m.mu.Lock()
	erasure, logger := m.erasure, m.logger
	m.mu.Unlock()
	if erasure == nil {
		return
	}
	n, err := erasure.ReconcilePurgedTenants(ctx)
	if err != nil {
		if ctx.Err() != nil {
			return // stopping
		}
		reason := "error"
		if errors.Is(err, context.DeadlineExceeded) {
			reason = "deadline"
		}
		logger.WarnContext(ctx, "retrieval: purged-tenant reconciliation failed; retrying at the next run",
			slog.Int("created", n), slog.String("reason", reason))
		return
	}
	if n > 0 {
		logger.InfoContext(ctx, "retrieval: purged-tenant reconciliation recorded tenant tombstones", slog.Int("created", n))
	}
}

// Stop cancels the background jobs and waits for them (or for ctx). A key
// held at that moment is still released or rescheduled: the worker's
// bookkeeping calls outlive the cancellation for a few seconds.
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
