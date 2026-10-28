package services

import (
	"context"
	"errors"
	"testing"

	"github.com/orkestra/backend/internal/core/retrieval/models"
)

// fakeStateList serves a fixed corpus-state listing.
type fakeStateList struct {
	states []models.CorpusState
	err    error
}

func (f fakeStateList) List(context.Context) ([]models.CorpusState, error) { return f.states, f.err }

// P15: every corpus state with an active generation is listed, registered or
// not; a state without one is absent.
func TestLiveCollections_ListsActiveGenerations(t *testing.T) {
	lister := NewLiveCollections(fakeStateList{states: []models.CorpusState{
		{Corpus: "people", ActiveGeneration: 1, Collection: "pfx_people_g1"},
		{Corpus: "notes"},
		{Corpus: "legacy", ActiveGeneration: 3},
	}}, "pfx")
	got, err := lister.LiveCollections(context.Background())
	require.NoError(t, err)
	assert.Equal(t, map[string][]string{
		"people": {"pfx_people_g1"},
		"legacy": {"pfx_legacy_g3"},
	}, got)

	boom := errors.New("mongo down")
	_, err = NewLiveCollections(fakeStateList{err: boom}, "pfx").LiveCollections(context.Background())
	if !errors.Is(err, boom) {
		t.Fatalf("err = %v, want %v", err, boom)
	}
}
