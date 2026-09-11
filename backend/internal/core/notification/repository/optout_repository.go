package repository

import (
	"context"
	"strings"

	"github.com/orkestra/backend/internal/core/notification/models"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// MarketingOptoutRepository stores and answers the durable opt-out fact.
type MarketingOptoutRepository interface {
	Upsert(ctx context.Context, doc models.MarketingOptoutDoc) error
	IsOptedOut(ctx context.Context, address, category string) (bool, error)
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

// Upsert records the opt-out idempotently: replaying it after a crash is a
// no-op on an address that already opted out, and never a second document.
// The earliest opt-out instant wins — an address does not un-opt-out by
// clicking again.
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
		},
		options.Update().SetUpsert(true),
	)
	return err
}

// IsOptedOut answers the send path. Its error is NOT swallowed by the
// caller: a lookup that fails must stop the send, never let it through.
func (r *marketingOptoutRepo) IsOptedOut(ctx context.Context, address, category string) (bool, error) {
	addr := NormalizeOptoutAddress(address)
	if addr == "" {
		return false, nil
	}
	filter := bson.M{"address": addr}
	//tenantscope:allow system: same reason as Upsert — an opt-out is a platform-wide fact, not a tenant one
	n, err := r.col().CountDocuments(ctx, filter)
	if err != nil {
		return false, err
	}
	return n > 0, nil
}
