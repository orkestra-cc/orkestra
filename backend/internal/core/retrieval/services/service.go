// Package services holds the retrieval module's Service: the corpus registry
// and, in later steps, the outbox, search and principal-resolver methods.
package services

import (
	"fmt"
	"log/slog"
	"sync"

	"github.com/orkestra/backend/internal/core/retrieval/engine"
	"github.com/orkestra/backend/pkg/sdk/iface"
)

// Options configures a Service.
type Options struct {
	Engine           engine.Engine // nil = QDRANT_URL not configured
	CollectionPrefix string
	Logger           *slog.Logger
}

// Service is the single retrieval service registered under
// module.ServiceRetrieval.
type Service struct {
	engine engine.Engine
	prefix string
	logger *slog.Logger

	mu           sync.RWMutex
	sealed       bool
	corpora      map[string]iface.RetrievalCorpusSpec
	contributors []iface.RetrievalPrincipalContributor
	contribNames map[string]struct{}
	outbox       *Outbox
}

var _ iface.RetrievalCorpusRegistry = (*Service)(nil)

// New builds a Service. It never contacts the engine.
func New(opts Options) *Service {
	logger := opts.Logger
	if logger == nil {
		logger = slog.Default()
	}
	return &Service{
		engine:       opts.Engine,
		prefix:       opts.CollectionPrefix,
		logger:       logger,
		corpora:      map[string]iface.RetrievalCorpusSpec{},
		contribNames: map[string]struct{}{},
	}
}

// Engine returns the vector engine, or iface.ErrRetrievalNotConfigured when
// none is configured.
func (s *Service) Engine() (engine.Engine, error) {
	if s.engine == nil {
		return nil, fmt.Errorf("%w", iface.ErrRetrievalNotConfigured)
	}
	return s.engine, nil
}

// CollectionPrefix returns the Qdrant collection name prefix.
func (s *Service) CollectionPrefix() string { return s.prefix }

// Seal closes registration. It is idempotent.
func (s *Service) Seal() {
	s.mu.Lock()
	s.sealed = true
	s.mu.Unlock()
}
