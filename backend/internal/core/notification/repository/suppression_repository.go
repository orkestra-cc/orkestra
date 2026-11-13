package repository

import (
	"context"
	"errors"
	"net/mail"
	"regexp"
	"strings"

	"github.com/orkestra/backend/internal/core/notification/models"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// SuppressionRepository stores the email suppression list
// (notification_suppressions): addresses that receive no email of any type.
// Every method takes addresses in any form and normalizes them with
// NormalizeSuppressionAddress; blanks are ignored.
type SuppressionRepository interface {
	// IsSuppressed is the send path's check.
	IsSuppressed(ctx context.Context, address string) (bool, error)
	// GetMany returns the rows of the addresses that have one, keyed by
	// normalized address.
	GetMany(ctx context.Context, addresses []string) (map[string]models.SuppressionDoc, error)
	// InsertIfAbsent writes a row for each address without one, all with the
	// attribution of tmpl, and returns the addresses it inserted. A row that
	// exists is left as it is (reason and date of the first suppression).
	InsertIfAbsent(ctx context.Context, addresses []string, tmpl models.SuppressionDoc) ([]string, error)
	// DeleteMany removes the addresses' rows and returns those it removed.
	DeleteMany(ctx context.Context, addresses []string) ([]string, error)
	// List returns up to limit rows in address order, after the address
	// after (exclusive), optionally keeping only addresses that contain
	// contains (literal, case-insensitive) and rows with reason.
	List(ctx context.Context, after string, limit int, contains, reason string) ([]models.SuppressionDoc, error)
}

type suppressionRepo struct{ coll *mongo.Collection }

// NewSuppressionRepository builds the Mongo repository.
func NewSuppressionRepository(db *mongo.Database) SuppressionRepository {
	return &suppressionRepo{coll: db.Collection(models.NotificationSuppressionsCollection)}
}

// NormalizeSuppressionAddress is the one canonical form of a suppression key:
// trimmed, lowercased, and reduced to the bare address when written as
// "Name <a@b>". The send path and every writer MUST use it, or a row written
// from one casing would not stop a send to another.
func NormalizeSuppressionAddress(a string) string {
	a = strings.TrimSpace(a)
	if strings.ContainsAny(a, "<>") {
		if parsed, err := mail.ParseAddress(a); err == nil {
			a = parsed.Address
		}
	}
	return strings.ToLower(strings.TrimSpace(a))
}

// normalizeSet returns the distinct normalized addresses, in input order.
func normalizeSet(addresses []string) []string {
	seen := make(map[string]struct{}, len(addresses))
	out := make([]string, 0, len(addresses))
	for _, a := range addresses {
		n := NormalizeSuppressionAddress(a)
		if n == "" {
			continue
		}
		if _, dup := seen[n]; dup {
			continue
		}
		seen[n] = struct{}{}
		out = append(out, n)
	}
	return out
}

func (r *suppressionRepo) IsSuppressed(ctx context.Context, address string) (bool, error) {
	addr := NormalizeSuppressionAddress(address)
	if addr == "" {
		return false, nil
	}
	//tenantscope:allow platform: the suppression list is address-keyed and platform-wide by design (tenantId is attribution only)
	n, err := r.coll.CountDocuments(ctx, bson.M{"address": addr}, options.Count().SetLimit(1))
	if err != nil {
		return false, err
	}
	return n > 0, nil
}

func (r *suppressionRepo) GetMany(ctx context.Context, addresses []string) (map[string]models.SuppressionDoc, error) {
	addrs := normalizeSet(addresses)
	out := make(map[string]models.SuppressionDoc, len(addrs))
	if len(addrs) == 0 {
		return out, nil
	}
	//tenantscope:allow platform: the suppression list is address-keyed and platform-wide by design (tenantId is attribution only)
	cur, err := r.coll.Find(ctx, bson.M{"address": bson.M{"$in": addrs}})
	if err != nil {
		return nil, err
	}
	defer cur.Close(ctx)
	for cur.Next(ctx) {
		var d models.SuppressionDoc
		if err := cur.Decode(&d); err != nil {
			return nil, err
		}
		out[d.Address] = d
	}
	return out, cur.Err()
}

func (r *suppressionRepo) InsertIfAbsent(ctx context.Context, addresses []string, tmpl models.SuppressionDoc) ([]string, error) {
	addrs := normalizeSet(addresses)
	if len(addrs) == 0 {
		return nil, nil
	}
	writes := make([]mongo.WriteModel, 0, len(addrs))
	for _, a := range addrs {
		set := bson.M{"address": a, "reason": tmpl.Reason, "createdAt": tmpl.CreatedAt}
		if tmpl.Source != "" {
			set["source"] = tmpl.Source
		}
		if tmpl.ActorUUID != "" {
			set["actorUuid"] = tmpl.ActorUUID
		}
		if tmpl.TenantID != "" {
			set["tenantId"] = tmpl.TenantID
		}
		writes = append(writes, mongo.NewUpdateOneModel().
			SetFilter(bson.M{"address": a}).
			SetUpdate(bson.M{"$setOnInsert": set}).
			SetUpsert(true))
	}
	res, err := r.coll.BulkWrite(ctx, writes, options.BulkWrite().SetOrdered(false))
	if err != nil && !isOnlyDuplicateKeyErrors(err) {
		return nil, err
	}
	if res == nil {
		return nil, nil
	}
	inserted := make([]string, 0, len(res.UpsertedIDs))
	for i := range addrs {
		if _, ok := res.UpsertedIDs[int64(i)]; ok {
			inserted = append(inserted, addrs[i])
		}
	}
	return inserted, nil
}

// isOnlyDuplicateKeyErrors: two concurrent upserts of the same address can
// race on the unique index; the loser's row exists, which is the answer.
func isOnlyDuplicateKeyErrors(err error) bool {
	var bwe mongo.BulkWriteException
	if !errors.As(err, &bwe) || bwe.WriteConcernError != nil || len(bwe.WriteErrors) == 0 {
		return false
	}
	for _, we := range bwe.WriteErrors {
		if we.Code != 11000 {
			return false
		}
	}
	return true
}

func (r *suppressionRepo) DeleteMany(ctx context.Context, addresses []string) ([]string, error) {
	addrs := normalizeSet(addresses)
	if len(addrs) == 0 {
		return nil, nil
	}
	present, err := r.GetMany(ctx, addrs)
	if err != nil {
		return nil, err
	}
	removed := make([]string, 0, len(present))
	for _, a := range addrs {
		if _, ok := present[a]; ok {
			removed = append(removed, a)
		}
	}
	if len(removed) == 0 {
		return nil, nil
	}
	// One statement per batch. A row deleted concurrently between the read
	// and this delete is still reported: it is gone either way.
	//tenantscope:allow platform: the suppression list is address-keyed and platform-wide by design (tenantId is attribution only)
	if _, err := r.coll.DeleteMany(ctx, bson.M{"address": bson.M{"$in": removed}}); err != nil {
		return nil, err
	}
	return removed, nil
}

func (r *suppressionRepo) List(ctx context.Context, after string, limit int, contains, reason string) ([]models.SuppressionDoc, error) {
	if limit <= 0 {
		return nil, nil
	}
	filter := bson.M{}
	if after = NormalizeSuppressionAddress(after); after != "" {
		filter["address"] = bson.M{"$gt": after}
	}
	if c := strings.ToLower(strings.TrimSpace(contains)); c != "" {
		// Addresses are stored lowercased, so a literal lowercase match is
		// the case-insensitive one; QuoteMeta keeps the input literal.
		addr, _ := filter["address"].(bson.M)
		if addr == nil {
			addr = bson.M{}
		}
		addr["$regex"] = regexp.QuoteMeta(c)
		filter["address"] = addr
	}
	if reason != "" {
		filter["reason"] = reason
	}
	//tenantscope:allow platform: the suppression list is address-keyed and platform-wide by design (tenantId is attribution only); the consumer filters what a tenant sees
	cur, err := r.coll.Find(ctx, filter, options.Find().SetSort(bson.D{{Key: "address", Value: 1}}).SetLimit(int64(limit)))
	if err != nil {
		return nil, err
	}
	defer cur.Close(ctx)
	out := make([]models.SuppressionDoc, 0, limit)
	if err := cur.All(ctx, &out); err != nil {
		return nil, err
	}
	return out, nil
}
