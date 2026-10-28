package repository

import (
	"context"
	"errors"

	"github.com/orkestra/backend/internal/core/retrieval/models"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
)

var (
	ErrStateExists   = errors.New("retrieval repository: corpus state exists")
	ErrStateConflict = errors.New("retrieval repository: corpus state changed concurrently")
)

// CorpusStates owns retrieval_corpus_state: installation-wide, one document
// per corpus (_id = corpus), updated with compare-and-swap on Version.
type CorpusStates struct{ coll *mongo.Collection }

func NewCorpusStates(db *mongo.Database) *CorpusStates {
	return &CorpusStates{coll: db.Collection(CollCorpusState)}
}

// Get returns the state of corpus, or nil, nil when absent.
func (r *CorpusStates) Get(ctx context.Context, corpus string) (*models.CorpusState, error) {
	var st models.CorpusState
	//tenantscope:allow system: installation-wide corpus state (ADR-0023 D11), one document per corpus
	err := r.coll.FindOne(ctx, bson.M{"_id": corpus}).Decode(&st)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &st, nil
}

// List returns every corpus state.
func (r *CorpusStates) List(ctx context.Context) ([]models.CorpusState, error) {
	//tenantscope:allow system: installation-wide corpus state (ADR-0023 D11), one document per corpus
	cur, err := r.coll.Find(ctx, bson.M{})
	if err != nil {
		return nil, err
	}
	var out []models.CorpusState
	if err := cur.All(ctx, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// Insert stores a new state at Version 1; ErrStateExists when present.
func (r *CorpusStates) Insert(ctx context.Context, st *models.CorpusState) error {
	doc := *st
	doc.Version = 1
	//tenantscope:allow system: installation-wide corpus state (ADR-0023 D11), one document per corpus
	_, err := r.coll.InsertOne(ctx, &doc)
	if mongo.IsDuplicateKeyError(err) {
		return ErrStateExists
	}
	if err != nil {
		return err
	}
	st.Version = 1
	return nil
}

// Save writes the worker-owned fields of st if the stored Version still
// equals st.Version, then increments st.Version; ErrStateConflict otherwise.
// It is a field update, not a replacement: _id, createdAt and any field this
// version of the code does not know (one written by a newer replica during a
// rolling upgrade) are left as stored. A zero omitempty field is unset, so the
// document keeps the shape Insert gives it.
func (r *CorpusStates) Save(ctx context.Context, st *models.CorpusState) error {
	next := st.Version + 1
	set := bson.M{
		"owner": st.Owner, "status": st.Status, "activeGeneration": st.ActiveGeneration,
		"locality": st.Locality, "updatedAt": st.UpdatedAt, "version": next,
	}
	unset := bson.M{}
	for k, v := range map[string]any{
		"reason": st.Reason, "collection": st.Collection, "ingestionProfileHash": st.IngestionProfileHash,
		"embeddingProfileId": st.EmbeddingProfileID, "declaredProfileHash": st.DeclaredProfileHash,
	} {
		if v == "" {
			unset[k] = ""
		} else {
			set[k] = v
		}
	}
	if st.Dimensions == 0 {
		unset["dimensions"] = ""
	} else {
		set["dimensions"] = st.Dimensions
	}
	update := bson.M{"$set": set}
	if len(unset) > 0 {
		update["$unset"] = unset
	}
	//tenantscope:allow system: installation-wide corpus state (ADR-0023 D11), one document per corpus
	res, err := r.coll.UpdateOne(ctx, bson.M{"_id": st.Corpus, "version": st.Version}, update)
	if err != nil {
		return err
	}
	if res.MatchedCount == 0 {
		return ErrStateConflict
	}
	st.Version = next
	return nil
}
