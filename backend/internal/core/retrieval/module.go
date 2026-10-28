// Package retrieval is the core module for corpora, indexing and hybrid
// search over tenant data with local inference (ADR-0023). This step holds
// the corpus registry, the engine contract and the Qdrant driver; the
// indexing worker, search and routes arrive in later steps.
package retrieval

import (
	"context"
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
}

func NewModule() *Module { return &Module{} }

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

// Collections declares the installation-wide corpus state (shape and
// repository arrive with the worker) and the per-key indexing outbox.
func (m *Module) Collections() []module.CollectionSpec {
	return []module.CollectionSpec{
		{Name: repository.CollCorpusState},
		{Name: repository.CollOutbox, Indexes: repository.OutboxIndexes()},
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
		drv, err := qdrant.New(qdrant.Config{URL: url, APIKey: key, Logger: logger})
		if err != nil {
			logger.Warn("retrieval: QDRANT_URL is not a valid http(s) URL; the vector engine is treated as not configured")
		} else {
			eng = drv
		}
	}

	svc := services.New(services.Options{Engine: eng, CollectionPrefix: prefix, Logger: logger})
	m.mu.Lock()
	m.logger, m.svc, m.eng = logger, svc, eng
	m.mu.Unlock()
	deps.Services.Register(module.ServiceRetrieval, svc)
	return nil
}

// Start seals corpus registration. Every module's Init has run by now, so a
// consumer that declares a dependency on retrieval has registered; a module
// retried later cannot register a corpus. It is idempotent.
func (m *Module) Start(_ context.Context) error {
	m.mu.Lock()
	svc := m.svc
	m.mu.Unlock()
	if svc != nil {
		svc.Seal()
	}
	return nil
}

func (m *Module) Stop(_ context.Context) error { return nil }

// HealthCheck reports the vector engine. Not configured is the designed
// degraded mode, not a fault. The handler of /v1/admin/modules/health copies
// err.Error() into its response, so the error is the bare sentinel and the
// driver error goes to the DEBUG log.
func (m *Module) HealthCheck(ctx context.Context) error {
	m.mu.Lock()
	eng, logger := m.eng, m.logger
	m.mu.Unlock()
	if eng == nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(ctx, healthTimeout)
	defer cancel()
	if err := eng.Health(ctx); err != nil {
		if logger != nil {
			logger.Debug("retrieval: engine health check failed", slog.String("error", err.Error()))
		}
		return iface.ErrRetrievalUnavailable
	}
	return nil
}
