package repository

import (
	"context"
	"errors"
	"time"

	"github.com/orkestra-cc/orkestra-addon-marketing/models"
	"github.com/orkestra-cc/orkestra-sdk/tenantrepo"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// ErrCardNotFound is returned when a card lookup finds no document
// in the caller's tenant scope.
var ErrCardNotFound = errors.New("marketing: card not found")

// CardRepository is the persistence boundary for marketing_cards.
type CardRepository struct {
	coll *mongo.Collection
}

// NewCardRepository binds a repository to the marketing_cards
// collection.
func NewCardRepository(db *mongo.Database) *CardRepository {
	return &CardRepository{coll: db.Collection(models.CardsCollection)}
}

// Create inserts a new card. Stamps tenantId + UpdatedAt; the caller
// supplies UUID, Code, IssuedAt, IssuedBy, and the snapshot Benefits.
func (r *CardRepository) Create(ctx context.Context, c *models.Card) error {
	if c == nil {
		return errors.New("marketing: nil card")
	}
	tenantID, err := tenantrepo.StampInsert(ctx)
	if err != nil {
		return err
	}
	c.TenantID = tenantID
	c.UpdatedAt = time.Now().UTC()
	_, err = r.coll.InsertOne(ctx, c)
	return err
}

// GetByUUID returns the card with the given UUID in the caller's
// tenant scope.
func (r *CardRepository) GetByUUID(ctx context.Context, uuid string) (*models.Card, error) {
	filter, err := tenantrepo.Scope(ctx, bson.M{"uuid": uuid})
	if err != nil {
		return nil, err
	}
	var out models.Card
	if err := r.coll.FindOne(ctx, filter).Decode(&out); err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			return nil, ErrCardNotFound
		}
		return nil, err
	}
	return &out, nil
}

// GetByCode returns the card with the given code in the caller's
// tenant scope. Used by the future Tier-2 client portal when a
// member redeems a code; also useful for ops support flows.
func (r *CardRepository) GetByCode(ctx context.Context, code string) (*models.Card, error) {
	filter, err := tenantrepo.Scope(ctx, bson.M{"code": code})
	if err != nil {
		return nil, err
	}
	var out models.Card
	if err := r.coll.FindOne(ctx, filter).Decode(&out); err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			return nil, ErrCardNotFound
		}
		return nil, err
	}
	return &out, nil
}

// ListByPerson returns every card (regardless of status) belonging
// to the given person UUID, ordered by issuedAt desc.
func (r *CardRepository) ListByPerson(ctx context.Context, personUUID string) ([]models.Card, error) {
	filter, err := tenantrepo.Scope(ctx, bson.M{"personUuid": personUUID})
	if err != nil {
		return nil, err
	}
	opts := options.Find().SetSort(bson.D{{Key: "issuedAt", Value: -1}})
	cur, err := r.coll.Find(ctx, filter, opts)
	if err != nil {
		return nil, err
	}
	defer cur.Close(ctx)
	out := make([]models.Card, 0)
	if err := cur.All(ctx, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// ListActiveByType returns every active card of the given type in
// the caller's tenant. Used by the persons list handler when
// resolving `?activeCardOfType=<typeUuid>` — the caller collects the
// returned UUIDs and passes them into PersonListFilter.ActiveCardUUIDs
// to leverage the activeCardUuids multikey index.
func (r *CardRepository) ListActiveByType(ctx context.Context, cardTypeUUID string) ([]models.Card, error) {
	filter, err := tenantrepo.Scope(ctx, bson.M{
		"cardTypeUuid": cardTypeUUID,
		"status":       models.CardStatusActive,
	})
	if err != nil {
		return nil, err
	}
	cur, err := r.coll.Find(ctx, filter)
	if err != nil {
		return nil, err
	}
	defer cur.Close(ctx)
	out := make([]models.Card, 0)
	if err := cur.All(ctx, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// FindActiveByPersonAndType returns the active card (if any) for the
// given (personUuid, cardTypeUuid) pair. Consumed by CardService.Issue
// to enforce the AllowMultiplePerPerson=false rule via a pre-write
// probe before committing the new row.
func (r *CardRepository) FindActiveByPersonAndType(
	ctx context.Context, personUUID, cardTypeUUID string,
) (*models.Card, error) {
	filter, err := tenantrepo.Scope(ctx, bson.M{
		"personUuid":   personUUID,
		"cardTypeUuid": cardTypeUUID,
		"status":       models.CardStatusActive,
	})
	if err != nil {
		return nil, err
	}
	var out models.Card
	if err := r.coll.FindOne(ctx, filter).Decode(&out); err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			return nil, ErrCardNotFound
		}
		return nil, err
	}
	return &out, nil
}

// CountByType returns the total number of cards (any status) of a
// given type in the caller's tenant. Used by the DeleteCardType
// service path to enforce "type is deletable only when no cards
// reference it" — the schema doc spells out the rule.
func (r *CardRepository) CountByType(ctx context.Context, cardTypeUUID string) (int64, error) {
	filter, err := tenantrepo.Scope(ctx, bson.M{"cardTypeUuid": cardTypeUUID})
	if err != nil {
		return 0, err
	}
	return r.coll.CountDocuments(ctx, filter)
}

// Update applies $set on mutable fields of a card. The transition
// matrix is enforced by CardService (only legal (from, to) edges
// reach this method).
func (r *CardRepository) Update(ctx context.Context, uuid string, patch bson.M) error {
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
		return ErrCardNotFound
	}
	return nil
}

// UpdateWithUnset applies $set + $unset in a single document update.
// Used by CardService.Reinstate to clear the suspended_* family while
// setting status back to active.
func (r *CardRepository) UpdateWithUnset(
	ctx context.Context, uuid string, set bson.M, unset bson.M,
) error {
	if set == nil {
		set = bson.M{}
	}
	set["updatedAt"] = time.Now().UTC()
	filter, err := tenantrepo.Scope(ctx, bson.M{"uuid": uuid})
	if err != nil {
		return err
	}
	update := bson.M{"$set": set}
	if len(unset) > 0 {
		update["$unset"] = unset
	}
	res, err := r.coll.UpdateOne(ctx, filter, update)
	if err != nil {
		return err
	}
	if res.MatchedCount == 0 {
		return ErrCardNotFound
	}
	return nil
}

// ListExpiringAcrossTenants returns up to `limit` cards whose
// expiresAt is at or before `asOf` and whose status is still active.
//
// This is one of THREE documented cross-tenant bypasses in the
// marketing addon, alongside
// ScoreSnapshotRepository.ListStaleAcrossTenants (Phase 2, nightly
// score recompute) and ImportJobRepository.ListAcrossTenantsByStatus
// (Phase 3, worker boot recovery). The card expiration scheduler
// runs as a background ticker with no inbound tenant ctx, so the
// read path must scan every tenant. The caller (CardScheduler.tick)
// re-stamps tenantId onto a fresh ctx via ctxauth.KeyTenantID before
// invoking CardService.Expire — every downstream write is scoped
// normally.
//
// DO NOT GENERALIZE THIS PATTERN. Every new cross-tenant background
// job earns one targeted method here, not a generic bypass helper.
func (r *CardRepository) ListExpiringAcrossTenants(
	ctx context.Context, asOf time.Time, limit int64,
) ([]models.Card, error) {
	if limit <= 0 {
		limit = 200
	}
	if limit > 1000 {
		limit = 1000
	}
	// Intentionally no tenantrepo.Scope call. The (tenantId, status,
	// expiresAt) sparse index covers this query; rows without
	// expiresAt are not even visited.
	filter := bson.M{
		"status":    models.CardStatusActive,
		"expiresAt": bson.M{"$lte": asOf},
	}
	opts := options.Find().
		SetLimit(limit).
		SetSort(bson.D{{Key: "expiresAt", Value: 1}})
	cur, err := r.coll.Find(ctx, filter, opts)
	if err != nil {
		return nil, err
	}
	defer cur.Close(ctx)
	out := make([]models.Card, 0)
	if err := cur.All(ctx, &out); err != nil {
		return nil, err
	}
	return out, nil
}
