package repository

import (
	"context"
	"errors"
	"time"

	"github.com/orkestra-cc/orkestra-addon-marketing/models"
	"github.com/orkestra-cc/orkestra-sdk/tenantrepo"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
)

// ErrCardTypeNotFound is returned when a card-type lookup finds no
// document in the caller's tenant scope.
var ErrCardTypeNotFound = errors.New("marketing: card type not found")

// CardTypeRepository is the persistence boundary for
// marketing_card_types.
type CardTypeRepository struct {
	coll *mongo.Collection
}

// NewCardTypeRepository binds a repository to the marketing_card_types
// collection.
func NewCardTypeRepository(db *mongo.Database) *CardTypeRepository {
	return &CardTypeRepository{coll: db.Collection(models.CardTypesCollection)}
}

// Create inserts a new card type. The caller assigns UUID + Key.
// Stamps tenantId / timestamps / Active=true default.
func (r *CardTypeRepository) Create(ctx context.Context, t *models.CardType) error {
	if t == nil {
		return errors.New("marketing: nil card type")
	}
	tenantID, err := tenantrepo.StampInsert(ctx)
	if err != nil {
		return err
	}
	t.TenantID = tenantID
	now := time.Now().UTC()
	t.CreatedAt = now
	t.UpdatedAt = now
	if _, err := r.coll.InsertOne(ctx, t); err != nil {
		return err
	}
	return nil
}

// GetByUUID returns the card type with the given UUID in the caller's
// tenant scope.
func (r *CardTypeRepository) GetByUUID(ctx context.Context, uuid string) (*models.CardType, error) {
	filter, err := tenantrepo.Scope(ctx, bson.M{"uuid": uuid})
	if err != nil {
		return nil, err
	}
	var out models.CardType
	if err := r.coll.FindOne(ctx, filter).Decode(&out); err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			return nil, ErrCardTypeNotFound
		}
		return nil, err
	}
	return &out, nil
}

// GetByKey returns the card type with the given key in the caller's
// tenant scope.
func (r *CardTypeRepository) GetByKey(ctx context.Context, key string) (*models.CardType, error) {
	filter, err := tenantrepo.Scope(ctx, bson.M{"key": key})
	if err != nil {
		return nil, err
	}
	var out models.CardType
	if err := r.coll.FindOne(ctx, filter).Decode(&out); err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			return nil, ErrCardTypeNotFound
		}
		return nil, err
	}
	return &out, nil
}

// CardTypeFilter narrows a List call by active status. Zero-value
// (ActiveOnly=false) returns every type, archived or not.
type CardTypeFilter struct {
	ActiveOnly bool
}

// List returns every card type matching the filter in the caller's
// tenant. The set is bounded by tenant configuration (typical: a
// handful of types) so no pagination — the admin UI renders the full
// list.
func (r *CardTypeRepository) List(ctx context.Context, f CardTypeFilter) ([]models.CardType, error) {
	q := bson.M{}
	if f.ActiveOnly {
		q["active"] = true
	}
	filter, err := tenantrepo.Scope(ctx, q)
	if err != nil {
		return nil, err
	}
	cur, err := r.coll.Find(ctx, filter)
	if err != nil {
		return nil, err
	}
	defer cur.Close(ctx)
	out := make([]models.CardType, 0)
	if err := cur.All(ctx, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// Update applies $set on mutable fields of a card type.
func (r *CardTypeRepository) Update(ctx context.Context, uuid string, patch bson.M) error {
	if patch == nil {
		patch = bson.M{}
	}
	patch["updatedAt"] = time.Now().UTC()
	filter, err := tenantrepo.Scope(ctx, bson.M{"uuid": uuid})
	if err != nil {
		return err
	}
	res, err := r.coll.UpdateOne(ctx, filter, bson.M{"$set": patch})
	if err != nil {
		return err
	}
	if res.MatchedCount == 0 {
		return ErrCardTypeNotFound
	}
	return nil
}

// Delete hard-deletes a card type by UUID. Callers must verify
// upstream that no cards reference the type — the service layer
// enforces this rule because the repository has no view of
// marketing_cards.
func (r *CardTypeRepository) Delete(ctx context.Context, uuid string) error {
	filter, err := tenantrepo.Scope(ctx, bson.M{"uuid": uuid})
	if err != nil {
		return err
	}
	res, err := r.coll.DeleteOne(ctx, filter)
	if err != nil {
		return err
	}
	if res.DeletedCount == 0 {
		return ErrCardTypeNotFound
	}
	return nil
}
