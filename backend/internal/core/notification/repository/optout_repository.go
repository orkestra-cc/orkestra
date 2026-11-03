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
// ($max) on every call, which is what a lift is guarded by; tenantId and
// sourceContext are latest-writer-wins, "" included, so a later unattributed
// opt-out makes the row unattributable rather than leaving a stale tenant.
func (r *marketingOptoutRepo) Upsert(ctx context.Context, doc models.MarketingOptoutDoc) error {
	addr := NormalizeOptoutAddress(doc.Address)
	if addr == "" {
		return nil
	}
	//tenantscope:allow system: opt-outs are keyed by address and apply platform-wide — a recipient does not opt out one tenant at a time, and scoping this per tenant would make the opt-out trivially bypassable
	_, err := r.col().UpdateOne(ctx,
		bson.M{"address": addr},
		bson.M{
			"$setOnInsert": bson.M{
				"address":         addr,
				"category":        doc.Category,
				"at":              doc.At,
				"sourceTokenUuid": doc.SourceTokenUUID,
			},
			"$set": bson.M{
				"tenantId":      doc.TenantID,
				"sourceContext": doc.SourceContext,
			},
			"$max": bson.M{"lastAt": doc.At},
		},
		options.Update().SetUpsert(true),
	)
	if err != nil && mongo.IsDuplicateKeyError(err) {
		// The unique address index (module.go's Collections()) means this
		// UpdateOne+$setOnInsert+upsert does not converge silently under
		// concurrency: two callers racing the same address can both miss
		// the filter and both attempt the insert, and the loser lands here
		// with E11000. That loser's desired state — an opt-out document
		// exists for this address — is already true, because the winner
		// just created it. Treat it as success: the public one-click
		// unsubscribe endpoint built on this must render its generic
		// response, never a 5xx, on a race it did nothing wrong to cause.
		return nil
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
