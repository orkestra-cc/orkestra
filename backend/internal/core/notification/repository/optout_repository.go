package repository

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/orkestra/backend/internal/core/notification/models"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// MarketingOptoutRepository stores and answers the durable opt-out fact.
type MarketingOptoutRepository interface {
	Upsert(ctx context.Context, doc models.MarketingOptoutDoc) error
	IsOptedOut(ctx context.Context, address, category string) (bool, error)
	// Get returns the opt-out for an address, or (nil, nil) when none.
	Get(ctx context.Context, address string) (*models.MarketingOptoutDoc, error)
	// DeleteIfAt removes the opt-out only while its latest instant (lastAt,
	// or at on a legacy row without one) equals at, so a lift never deletes an
	// opt-out recorded again in the meantime. at must be the value Get
	// returned (Mongo keeps milliseconds).
	DeleteIfAt(ctx context.Context, address string, at time.Time) (bool, error)
}

type marketingOptoutRepo struct{ db *mongo.Database }

func NewMarketingOptoutRepository(db *mongo.Database) MarketingOptoutRepository {
	return &marketingOptoutRepo{db: db}
}

func (r *marketingOptoutRepo) col() *mongo.Collection {
	return r.db.Collection(models.NotificationMarketingOptoutsCollection)
}

// NormalizeOptoutAddress is the one place an address is canonicalized for
// this collection. Upsert and IsOptedOut MUST agree, or an opt-out recorded
// from a mail client's casing would not match the send path's lookup.
func NormalizeOptoutAddress(a string) string { return strings.ToLower(strings.TrimSpace(a)) }

// Upsert records the opt-out idempotently: replaying it after a crash never
// creates a second document and never un-opts-out an address. The first
// opt-out instant (at), category and source token are kept; lastAt advances
// ($max) on every call, which is what a lift is guarded by.
//
// Attribution (tenantId, sourceContext) is "agree or unknown": a new row takes
// the incoming pair; on an existing row the pair is kept only if BOTH incoming
// values equal the stored ones, otherwise both are cleared to "". A second
// writer that disagrees therefore makes the row unattributable instead of
// relabelling it, so one tenant cannot take over another's opt-out. This is a
// single update-pipeline statement (works with upsert), so the comparison and
// the write are atomic.
func (r *marketingOptoutRepo) Upsert(ctx context.Context, doc models.MarketingOptoutDoc) error {
	addr := NormalizeOptoutAddress(doc.Address)
	if addr == "" {
		return nil
	}
	lit := func(v any) bson.D { return bson.D{{Key: "$literal", Value: v}} }
	// On an upsert-insert the pipeline runs on a document holding only the
	// filter's address, so a missing "at" means "new row".
	isNew := bson.D{{Key: "$eq", Value: bson.A{bson.D{{Key: "$type", Value: "$at"}}, "missing"}}}
	agree := bson.D{{Key: "$and", Value: bson.A{
		bson.D{{Key: "$eq", Value: bson.A{bson.D{{Key: "$ifNull", Value: bson.A{"$tenantId", ""}}}, lit(doc.TenantID)}}},
		bson.D{{Key: "$eq", Value: bson.A{bson.D{{Key: "$ifNull", Value: bson.A{"$sourceContext", ""}}}, lit(doc.SourceContext)}}},
	}}}
	keepOrClear := func(incoming string) bson.D {
		return bson.D{{Key: "$cond", Value: bson.A{
			bson.D{{Key: "$or", Value: bson.A{isNew, agree}}}, lit(incoming), "",
		}}}
	}
	pipeline := mongo.Pipeline{{{Key: "$set", Value: bson.D{
		// lit: a "$"-prefixed local part is a valid address, not a field path.
		{Key: "address", Value: lit(addr)},
		{Key: "category", Value: bson.D{{Key: "$ifNull", Value: bson.A{"$category", lit(doc.Category)}}}},
		{Key: "at", Value: bson.D{{Key: "$ifNull", Value: bson.A{"$at", doc.At}}}},
		{Key: "sourceTokenUuid", Value: bson.D{{Key: "$ifNull", Value: bson.A{"$sourceTokenUuid", lit(doc.SourceTokenUUID)}}}},
		{Key: "lastAt", Value: bson.D{{Key: "$max", Value: bson.A{"$lastAt", doc.At}}}},
		{Key: "tenantId", Value: keepOrClear(doc.TenantID)},
		{Key: "sourceContext", Value: keepOrClear(doc.SourceContext)},
	}}}}
	update := func() error {
		//tenantscope:allow system: opt-outs are keyed by address and apply platform-wide — a recipient does not opt out one tenant at a time, and scoping this per tenant would make the opt-out trivially bypassable
		_, err := r.col().UpdateOne(ctx, bson.M{"address": addr}, pipeline, options.Update().SetUpsert(true))
		return err
	}
	err := update()
	if err != nil && mongo.IsDuplicateKeyError(err) {
		// The unique address index (module.go's Collections()) means this
		// UpdateOne+upsert does not converge silently under concurrency: two
		// callers racing the same new address can both miss the filter and
		// both attempt the insert, and the loser lands here with E11000. The
		// winner just created the row, so the loser's opt-out fact is already
		// true, but its $set/$max did not run. Retry once: the filter now
		// matches the winner's row, so lastAt and the attribution of the
		// later writer apply to it. A second E11000 is still a success (the
		// row exists); the public one-click unsubscribe endpoint built on
		// this must render its generic response, never a 5xx, on a race it
		// did nothing wrong to cause.
		err = update()
		if err != nil && mongo.IsDuplicateKeyError(err) {
			return nil
		}
	}
	return err
}

// IsOptedOut answers the send path. Its error is NOT swallowed by the
// caller: a lookup that fails must stop the send, never let it through.
func (r *marketingOptoutRepo) IsOptedOut(ctx context.Context, address, category string) (bool, error) {
	addr := NormalizeOptoutAddress(address)
	if addr == "" {
		return false, nil
	}
	// category is deliberately not part of the filter: any opt-out row for
	// this address suppresses ALL marketing, not just the category it was
	// recorded under. There is no per-category opt-out UI yet, so the send
	// path passing a real category here must not be read as evidence that
	// this call scopes by it.
	filter := bson.M{"address": addr}
	//tenantscope:allow system: same reason as Upsert — an opt-out is a platform-wide fact, not a tenant one
	n, err := r.col().CountDocuments(ctx, filter)
	if err != nil {
		return false, err
	}
	return n > 0, nil
}

// Get returns the stored opt-out, or (nil, nil) when the address has none.
func (r *marketingOptoutRepo) Get(ctx context.Context, address string) (*models.MarketingOptoutDoc, error) {
	addr := NormalizeOptoutAddress(address)
	if addr == "" {
		return nil, nil
	}
	var doc models.MarketingOptoutDoc
	//tenantscope:allow system: same reason as Upsert — an opt-out is a platform-wide fact keyed by address, not a tenant one
	err := r.col().FindOne(ctx, bson.M{"address": addr}).Decode(&doc)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &doc, nil
}

// DeleteIfAt deletes the row only when the address matches and its latest
// opt-out instant equals at: lastAt, or at itself on a legacy row that has no
// lastAt. A caller that read the opt-out, decided to lift it, and lost a race
// against a fresh opt-out (which advances lastAt) must not delete the newer
// fact.
func (r *marketingOptoutRepo) DeleteIfAt(ctx context.Context, address string, at time.Time) (bool, error) {
	addr := NormalizeOptoutAddress(address)
	if addr == "" {
		return false, nil
	}
	//tenantscope:allow system: same reason as Upsert — an opt-out is a platform-wide fact keyed by address, not a tenant one
	res, err := r.col().DeleteOne(ctx, bson.M{
		"address": addr,
		"$or": []bson.M{
			{"lastAt": at},
			{"lastAt": bson.M{"$exists": false}, "at": at},
		},
	})
	if err != nil {
		return false, err
	}
	return res.DeletedCount > 0, nil
}
