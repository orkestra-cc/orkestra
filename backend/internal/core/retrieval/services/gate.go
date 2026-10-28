package services

import (
	"context"
	"errors"
	"fmt"

	"github.com/orkestra/backend/internal/core/retrieval/engine"
	"github.com/orkestra/backend/internal/core/retrieval/models"
	"github.com/orkestra/backend/internal/core/retrieval/repository"
	"github.com/orkestra/backend/pkg/sdk/iface"
)

// corpusGate is what one pass may write for a corpus.
type corpusGate struct {
	Spec        iface.RetrievalCorpusSpec
	State       models.CorpusState
	ProfileHash string
}

// prepareCorpus decides whether this pass may write to the corpus, creating
// generation 1 on first use and enforcing locality. A nil gate with an empty
// problem is an expected skip; a non-empty problem is reported to the caller
// and never carries item data.
func (w *Worker) prepareCorpus(ctx context.Context, spec iface.RetrievalCorpusSpec) (*corpusGate, string, error) {
	if !w.Modules.IsStarted(spec.Owner) {
		return nil, "", nil
	}
	endpoint := spec.Ingestion.EmbedEndpoint
	profile, err := w.Profiles.InferenceProfile(ctx, endpoint)
	switch {
	case errors.Is(err, iface.ErrLLMNotConfigured):
		problem := fmt.Sprintf("corpus %s: embed endpoint %q is not configured", spec.Name, endpoint)
		return w.recordError(ctx, spec, models.ReasonEndpointUnknown, problem)
	case err != nil || profile == nil || profile.Dimensions <= 0:
		return nil, fmt.Sprintf("corpus %s: inference profile unavailable", spec.Name), nil
	case profile.Kind != iface.InferenceKindEmbed:
		problem := fmt.Sprintf("corpus %s: endpoint %q is not an embedding endpoint", spec.Name, endpoint)
		return w.recordError(ctx, spec, models.ReasonEndpointKind, problem)
	case spec.Locality != iface.RetrievalHostedAllowed && profile.Boundary != iface.InferenceBoundaryLocal:
		problem := fmt.Sprintf("corpus %s: local_only corpus uses hosted endpoint %q", spec.Name, endpoint)
		return w.recordError(ctx, spec, models.ReasonLocality, problem)
	}

	hash := IngestionProfileHash(spec.Ingestion, profile.ID)
	st, err := w.States.Get(ctx, spec.Name)
	if err != nil {
		return nil, "", err
	}
	if st == nil || st.ActiveGeneration == 0 {
		return w.bootstrap(ctx, spec, st, profile, hash)
	}
	if st.IngestionProfileHash != hash {
		if st.Status == models.CorpusStatusProfileChanged && st.DeclaredProfileHash == hash {
			return nil, profileChangedProblem(spec.Name), nil
		}
		st.Status, st.DeclaredProfileHash, st.Reason = models.CorpusStatusProfileChanged, hash, ""
		st.UpdatedAt = w.Now()
		if err := w.States.Save(ctx, st); err != nil {
			return nil, "", ignoreMoved(err)
		}
		return nil, profileChangedProblem(spec.Name), nil
	}
	if st.Status != models.CorpusStatusActive || st.Reason != "" || st.DeclaredProfileHash != "" {
		st.Status, st.Reason, st.DeclaredProfileHash = models.CorpusStatusActive, "", ""
		st.UpdatedAt = w.Now()
		if err := w.States.Save(ctx, st); err != nil {
			return nil, "", ignoreMoved(err)
		}
	}
	return &corpusGate{Spec: spec, State: *st, ProfileHash: hash}, "", nil
}

func profileChangedProblem(corpus string) string {
	return fmt.Sprintf("corpus %s: ingestion profile changed; indexing paused until a new generation is built", corpus)
}

// bootstrap creates generation 1's collection and records it as active.
func (w *Worker) bootstrap(ctx context.Context, spec iface.RetrievalCorpusSpec, st *models.CorpusState, profile *iface.InferenceProfile, hash string) (*corpusGate, string, error) {
	name := engine.CollectionName(w.Prefix, spec.Name, 1)
	err := w.Engine.EnsureCollection(ctx, engine.CollectionSpec{
		Name: name, Dimensions: profile.Dimensions, Fields: spec.Filters, Analyzer: spec.Ingestion.Analyzer,
	})
	if err != nil {
		if ctx.Err() != nil {
			return nil, "", ctx.Err()
		}
		w.Logger.Warn("retrieval: ensure collection failed", "corpus", spec.Name)
		return nil, fmt.Sprintf("corpus %s: vector engine unavailable", spec.Name), nil
	}
	now := w.Now()
	fresh := st == nil
	if fresh {
		st = &models.CorpusState{Corpus: spec.Name, CreatedAt: now}
	}
	st.Owner, st.Status, st.Reason = spec.Owner, models.CorpusStatusActive, ""
	st.ActiveGeneration, st.Collection = 1, name
	st.IngestionProfileHash, st.EmbeddingProfileID, st.Dimensions = hash, profile.ID, profile.Dimensions
	st.Locality, st.DeclaredProfileHash, st.UpdatedAt = declaredLocality(spec), "", now
	if fresh {
		err = w.States.Insert(ctx, st)
	} else {
		err = w.States.Save(ctx, st)
	}
	if err != nil {
		return nil, "", ignoreMoved(err)
	}
	return &corpusGate{Spec: spec, State: *st, ProfileHash: hash}, "", nil
}

// recordError persists status error with reason (keeping any generation
// fields) and returns the problem text; a concurrent move by another replica
// skips the pass without a problem.
func (w *Worker) recordError(ctx context.Context, spec iface.RetrievalCorpusSpec, reason, problem string) (*corpusGate, string, error) {
	st, err := w.States.Get(ctx, spec.Name)
	if err != nil {
		return nil, "", err
	}
	now := w.Now()
	if st == nil {
		err = w.States.Insert(ctx, &models.CorpusState{
			Corpus: spec.Name, Owner: spec.Owner, Status: models.CorpusStatusError, Reason: reason,
			Locality: declaredLocality(spec), CreatedAt: now, UpdatedAt: now,
		})
	} else if st.Status != models.CorpusStatusError || st.Reason != reason {
		st.Status, st.Reason, st.UpdatedAt = models.CorpusStatusError, reason, now
		err = w.States.Save(ctx, st)
	}
	if err != nil {
		return nil, "", ignoreMoved(err)
	}
	return nil, problem, nil
}

func declaredLocality(spec iface.RetrievalCorpusSpec) string {
	if spec.Locality == "" {
		return string(iface.RetrievalLocalOnly)
	}
	return string(spec.Locality)
}

func ignoreMoved(err error) error {
	if errors.Is(err, repository.ErrStateExists) || errors.Is(err, repository.ErrStateConflict) {
		return nil
	}
	return err
}
