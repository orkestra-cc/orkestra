package services

import (
	"context"
	"fmt"

	"github.com/orkestra/backend/internal/core/retrieval/engine"
	"github.com/orkestra/backend/internal/core/retrieval/models"
	"github.com/orkestra/backend/internal/core/retrieval/repository"
)

// CorpusStateLister lists every corpus state document; satisfied by
// *repository.CorpusStates.
type CorpusStateLister interface {
	List(ctx context.Context) ([]models.CorpusState, error)
}

var _ CorpusStateLister = (*repository.CorpusStates)(nil)

// liveCollections is the CollectionLister of the erasure finalizer over
// retrieval_corpus_state.
type liveCollections struct {
	states CorpusStateLister
	prefix string
}

// NewLiveCollections adapts the corpus-state listing to CollectionLister.
// prefix builds the collection name of a state that recorded none.
func NewLiveCollections(states CorpusStateLister, prefix string) CollectionLister {
	return liveCollections{states: states, prefix: prefix}
}

// LiveCollections maps every corpus with an active generation to that
// generation's collection, registered corpora or not: an erasure must also
// reach a corpus whose owner no longer registers it. Corpus state holds only
// the active generation in delivery step 1.
func (l liveCollections) LiveCollections(ctx context.Context) (map[string][]string, error) {
	states, err := l.states.List(ctx)
	if err != nil {
		return nil, fmt.Errorf("retrieval: list corpus state: %w", err)
	}
	out := make(map[string][]string, len(states))
	for _, st := range states {
		if st.ActiveGeneration <= 0 {
			continue
		}
		collection := st.Collection
		if collection == "" {
			collection = engine.CollectionName(l.prefix, st.Corpus, st.ActiveGeneration)
		}
		out[st.Corpus] = []string{collection}
	}
	return out, nil
}
