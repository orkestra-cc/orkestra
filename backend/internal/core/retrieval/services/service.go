// Package services holds the retrieval module's Service: the corpus registry,
// the outbox, the index worker, the search pipeline (Searcher) and the
// principal resolver. Data-subject erasure (DeleteBySubject, the tenant purge
// and their finalization) is the ErasureService; Service delegates to it.
package services

import (
	"context"
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
	searcher     *Searcher
	principals   *PrincipalResolver
	erasure      *ErasureService
	audit        *AuditRelay
	sink         iface.AuditSink // pending until a relay is wired
}

var (
	_ iface.RetrievalCorpusRegistry    = (*Service)(nil)
	_ iface.RetrievalSearch            = (*Service)(nil)
	_ iface.RetrievalPrincipalResolver = (*Service)(nil)
	_ iface.AuditSinkSetter            = (*Service)(nil)
)

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

// SetSearch wires the search pipeline and the principal resolver; module Init
// calls it once, and only when a database handle is present. A nil argument
// leaves that capability failing closed.
func (s *Service) SetSearch(searcher *Searcher, principals *PrincipalResolver) {
	s.mu.Lock()
	s.searcher, s.principals = searcher, principals
	s.mu.Unlock()
}

// Search implements iface.RetrievalSearch. Without a wired Searcher it
// returns iface.ErrRetrievalNotConfigured.
func (s *Service) Search(ctx context.Context, req iface.RetrievalSearchRequest) (*iface.RetrievalSearchResult, error) {
	s.mu.RLock()
	searcher := s.searcher
	s.mu.RUnlock()
	if searcher == nil {
		return nil, fmt.Errorf("%w: search is not wired", iface.ErrRetrievalNotConfigured)
	}
	return searcher.Search(ctx, req)
}

// SetErasure wires the erasure service; module Init calls it once, and only
// when a database handle is present.
func (s *Service) SetErasure(e *ErasureService) {
	s.mu.Lock()
	s.erasure = e
	s.mu.Unlock()
}

// SetAuditRelay wires the relay the module's audit events go through. A sink
// pushed earlier by SetAuditSink is adopted by the relay, so the order of the
// two setters cannot lose it.
func (s *Service) SetAuditRelay(a *AuditRelay) {
	s.mu.Lock()
	s.audit = a
	sink := s.sink
	s.mu.Unlock()
	if a != nil && sink != nil {
		a.SetAuditSink(sink)
	}
}

// SetAuditSink satisfies iface.AuditSinkSetter: compliance publishes its sink
// here. It is kept, and forwarded to the current relay when one is wired; nil
// is ignored.
func (s *Service) SetAuditSink(sink iface.AuditSink) {
	if sink == nil {
		return
	}
	s.mu.Lock()
	s.sink = sink
	relay := s.audit
	s.mu.Unlock()
	if relay != nil {
		relay.SetAuditSink(sink)
	}
}

// DeleteBySubject implements iface.RetrievalSearch by delegating to the
// ErasureService. Without one it returns iface.ErrRetrievalNotConfigured.
func (s *Service) DeleteBySubject(ctx context.Context, tenantID, subject string) error {
	s.mu.RLock()
	e := s.erasure
	s.mu.RUnlock()
	if e == nil {
		return fmt.Errorf("%w: erasure is not wired", iface.ErrRetrievalNotConfigured)
	}
	return e.DeleteBySubject(ctx, tenantID, subject)
}

// Principals implements iface.RetrievalPrincipalResolver for the caller in
// ctx. Without a wired resolver it fails closed with
// iface.ErrRetrievalForbidden. A resolver error is mapped by principalError,
// exactly as search step 3 maps it: iface.ErrRetrievalForbidden passes
// through, anything else is iface.ErrRetrievalUnavailable naming only the
// error's kind (the resolver's text may name a contributor's principals).
func (s *Service) Principals(ctx context.Context) (iface.RetrievalPrincipalContext, error) {
	s.mu.RLock()
	r := s.principals
	s.mu.RUnlock()
	if r == nil {
		return iface.RetrievalPrincipalContext{}, fmt.Errorf("%w: the principal resolver is not wired", iface.ErrRetrievalForbidden)
	}
	pc, err := r.Principals(ctx)
	if err != nil {
		return iface.RetrievalPrincipalContext{}, principalError(ctx, err)
	}
	return pc, nil
}
