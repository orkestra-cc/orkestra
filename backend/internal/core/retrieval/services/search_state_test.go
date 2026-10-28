package services

import (
	"context"
	"errors"
	"testing"

	"github.com/orkestra/backend/internal/core/retrieval/models"
)

type erroringStates struct{ err error }

func (e erroringStates) Get(context.Context, string) (*models.CorpusState, error) { return nil, e.err }

func TestCorpusStateReader_ActiveGeneration(t *testing.T) {
	states := newFakeStates()
	states.states["contacts"] = models.CorpusState{
		Corpus: "contacts", Status: models.CorpusStatusActive, ActiveGeneration: 3,
		Collection: "orkestra_contacts_g3", EmbeddingProfileID: "ip_embed_a",
	}
	r := NewCorpusStateReader(states, "orkestra")
	got, err := r.ActiveGeneration(context.Background(), "contacts")
	require.NoError(t, err)
	assert.Equal(t, PinnedGeneration{Collection: "orkestra_contacts_g3", Generation: 3, EmbedProfileID: "ip_embed_a"}, got)
}

func TestCorpusStateReader_NoActiveGeneration(t *testing.T) {
	states := newFakeStates()
	states.states["fresh"] = models.CorpusState{Corpus: "fresh", Status: models.CorpusStatusError, ActiveGeneration: 0}
	r := NewCorpusStateReader(states, "orkestra")
	for _, corpus := range []string{"missing", "fresh"} {
		_, err := r.ActiveGeneration(context.Background(), corpus)
		requireIs(t, err, ErrNoActiveGeneration)
	}
}

// A state recorded without a collection name falls back to the naming helper.
func TestCorpusStateReader_CollectionFallback(t *testing.T) {
	states := newFakeStates()
	states.states["contacts"] = models.CorpusState{Corpus: "contacts", ActiveGeneration: 2, EmbeddingProfileID: "ip_x"}
	got, err := NewCorpusStateReader(states, "acme").ActiveGeneration(context.Background(), "contacts")
	require.NoError(t, err)
	assert.Equal(t, "acme_contacts_g2", got.Collection)
}

// profile_changed and error keep serving the active generation (P17).
func TestCorpusStateReader_ServesTheActiveGenerationWhateverTheStatus(t *testing.T) {
	for _, status := range []string{models.CorpusStatusProfileChanged, models.CorpusStatusError} {
		states := newFakeStates()
		states.states["contacts"] = models.CorpusState{Corpus: "contacts", Status: status, ActiveGeneration: 1,
			Collection: "orkestra_contacts_g1", EmbeddingProfileID: "ip_a"}
		got, err := NewCorpusStateReader(states, "orkestra").ActiveGeneration(context.Background(), "contacts")
		require.NoError(t, err)
		assert.Equal(t, 1, got.Generation)
	}
}

func TestCorpusStateReader_ReadErrorIsPassedOn(t *testing.T) {
	boom := errors.New("mongo down")
	_, err := NewCorpusStateReader(erroringStates{err: boom}, "orkestra").ActiveGeneration(context.Background(), "contacts")
	requireIs(t, err, boom)
	if errors.Is(err, ErrNoActiveGeneration) {
		t.Fatalf("a read error must not read as no active generation: %v", err)
	}
}
