package services

import (
	"context"
	"fmt"

	"github.com/orkestra/backend/internal/core/retrieval/engine"
	"github.com/orkestra/backend/internal/core/retrieval/models"
)

// CorpusStateGetter reads one corpus state, nil when absent; satisfied by
// *repository.CorpusStates.
type CorpusStateGetter interface {
	Get(ctx context.Context, corpus string) (*models.CorpusState, error)
}

// corpusStateReader is the CorpusStateReader search pins through: the active
// generation the index worker recorded in retrieval_corpus_state.
type corpusStateReader struct {
	states CorpusStateGetter
	prefix string
}

// NewCorpusStateReader adapts the corpus-state read to CorpusStateReader.
// prefix builds the collection name of a state that recorded none.
func NewCorpusStateReader(states CorpusStateGetter, prefix string) CorpusStateReader {
	return corpusStateReader{states: states, prefix: prefix}
}

// ActiveGeneration returns the generation search pins. A missing state or
// generation 0 is ErrNoActiveGeneration. Any status with an active generation
// is served, profile_changed and error included: the active generation keeps
// answering, and a changed embedding model is caught by the profile-ID check
// (keyword fallback, degraded).
func (r corpusStateReader) ActiveGeneration(ctx context.Context, corpus string) (PinnedGeneration, error) {
	st, err := r.states.Get(ctx, corpus)
	if err != nil {
		return PinnedGeneration{}, fmt.Errorf("retrieval: corpus state %q: %w", corpus, err)
	}
	if st == nil || st.ActiveGeneration <= 0 {
		return PinnedGeneration{}, ErrNoActiveGeneration
	}
	collection := st.Collection
	if collection == "" {
		collection = engine.CollectionName(r.prefix, corpus, st.ActiveGeneration)
	}
	return PinnedGeneration{
		Collection:     collection,
		Generation:     st.ActiveGeneration,
		EmbedProfileID: st.EmbeddingProfileID,
	}, nil
}
