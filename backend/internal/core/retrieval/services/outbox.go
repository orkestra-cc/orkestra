package services

import (
	"context"
	"errors"
	"fmt"
	"time"
	"unicode/utf8"

	"github.com/orkestra/backend/internal/core/retrieval/repository"
	"github.com/orkestra/backend/pkg/sdk/iface"
	"github.com/orkestra/backend/pkg/sdk/tenantrepo"
)

// MaxIntentsPerEnqueue bounds one Enqueue call.
const MaxIntentsPerEnqueue = 1000

const maxSourceIDBytes = 256

// OutboxWriter persists intents; satisfied by *repository.Outbox.
type OutboxWriter interface {
	Enqueue(ctx context.Context, intents []iface.RetrievalIntent, now time.Time) error
}

// CorpusCatalog is what indexing needs from the corpus registry.
type CorpusCatalog interface {
	Corpus(name string) (iface.RetrievalCorpusSpec, bool)
	Corpora() []iface.RetrievalCorpusSpec // sorted by Name
}

var (
	_ OutboxWriter  = (*repository.Outbox)(nil)
	_ CorpusCatalog = (*Service)(nil)
)

// ValidateIntent checks the SourceID and Revision rules; failures wrap
// iface.ErrRetrievalInvalidIntent.
func ValidateIntent(i iface.RetrievalIntent) error {
	switch {
	case len(i.SourceID) < 1 || len(i.SourceID) > maxSourceIDBytes:
		return fmt.Errorf("%w: source id must be 1-%d bytes", iface.ErrRetrievalInvalidIntent, maxSourceIDBytes)
	case !utf8.ValidString(i.SourceID):
		return fmt.Errorf("%w: source id is not valid UTF-8", iface.ErrRetrievalInvalidIntent)
	case containsNUL(i.SourceID):
		return fmt.Errorf("%w: source id contains NUL", iface.ErrRetrievalInvalidIntent)
	case i.Revision < 0:
		return fmt.Errorf("%w: negative revision", iface.ErrRetrievalInvalidIntent)
	}
	return nil
}

func containsNUL(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] == 0 {
			return true
		}
	}
	return false
}

// Outbox accepts index intents inside the owner's transaction. It never
// depends on the engine or on the owning module's lifecycle (ADR D1).
type Outbox struct {
	store   OutboxWriter
	catalog CorpusCatalog
	now     func() time.Time
}

// NewOutbox builds an Outbox.
func NewOutbox(store OutboxWriter, catalog CorpusCatalog, now func() time.Time) *Outbox {
	if now == nil {
		now = time.Now
	}
	return &Outbox{store: store, catalog: catalog, now: now}
}

var _ iface.RetrievalOutbox = (*Outbox)(nil)

// Enqueue validates every intent, then writes them all with the caller's ctx
// (which carries the source write's session).
func (o *Outbox) Enqueue(ctx context.Context, intents ...iface.RetrievalIntent) error {
	if len(intents) == 0 {
		return nil
	}
	if len(intents) > MaxIntentsPerEnqueue {
		return fmt.Errorf("%w: more than %d intents", iface.ErrRetrievalInvalidIntent, MaxIntentsPerEnqueue)
	}
	if err := tenantrepo.RequireInternalTenant(ctx); err != nil {
		return fmt.Errorf("retrieval enqueue: %w", err)
	}
	for _, in := range intents {
		if _, ok := o.catalog.Corpus(in.Corpus); !ok {
			return fmt.Errorf("%w: %q", iface.ErrRetrievalUnknownCorpus, in.Corpus)
		}
	}
	for _, in := range intents {
		if err := ValidateIntent(in); err != nil {
			return err
		}
	}
	return o.store.Enqueue(ctx, intents, o.now())
}

var errOutboxNotWired = errors.New("retrieval: outbox is not wired")

// SetOutbox wires the outbox; module Init calls it before registering.
func (s *Service) SetOutbox(o *Outbox) {
	s.mu.Lock()
	s.outbox = o
	s.mu.Unlock()
}

var _ iface.RetrievalOutbox = (*Service)(nil)

// Enqueue delegates to the wired Outbox.
func (s *Service) Enqueue(ctx context.Context, intents ...iface.RetrievalIntent) error {
	s.mu.RLock()
	o := s.outbox
	s.mu.RUnlock()
	if o == nil {
		return errOutboxNotWired
	}
	return o.Enqueue(ctx, intents...)
}
