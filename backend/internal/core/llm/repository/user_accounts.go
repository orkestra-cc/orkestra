package repository

import (
	"context"
	"errors"
	"time"

	"github.com/orkestra/backend/internal/core/llm/models"
	"github.com/orkestra/backend/pkg/sdk/tenantrepo"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// CollUserAccounts holds the Sign in with ChatGPT registrations.
const CollUserAccounts = "llm_user_accounts"

// UserAccounts owns llm_user_accounts.
type UserAccounts struct{ coll *mongo.Collection }

func NewUserAccounts(db *mongo.Database) *UserAccounts {
	return &UserAccounts{coll: db.Collection(CollUserAccounts)}
}

// Insert stores a registration under the caller's org (stamped from ctx).
// ErrDuplicateName covers both unique indexes that can fire: the same
// issued client id for the same user, and a second Selected row for the
// same (user, provider) when two first links race — the caller tells them
// apart by re-reading.
func (r *UserAccounts) Insert(ctx context.Context, a *models.UserAccount) error {
	tenantID, err := tenantrepo.StampInsert(ctx)
	if err != nil {
		return err
	}
	a.TenantID = tenantID
	if _, err := r.coll.InsertOne(ctx, a); err != nil {
		if mongo.IsDuplicateKeyError(err) {
			return ErrDuplicateName
		}
		return err
	}
	return nil
}

func (r *UserAccounts) find(ctx context.Context, extra bson.M, opts *options.FindOptions) ([]models.UserAccount, error) {
	filter, err := tenantrepo.Scope(ctx, extra)
	if err != nil {
		return nil, err
	}
	cur, err := r.coll.Find(ctx, filter, opts)
	if err != nil {
		return nil, err
	}
	out := []models.UserAccount{}
	if err := cur.All(ctx, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// ListByUser returns the user's registrations in the caller's org, oldest
// link first.
func (r *UserAccounts) ListByUser(ctx context.Context, userUUID string) ([]models.UserAccount, error) {
	return r.find(ctx, bson.M{"userUuid": userUUID}, options.Find().SetSort(bson.D{{Key: "linkedAt", Value: 1}}))
}

// ListAdmin returns every registration of the caller's org, newest link
// first.
func (r *UserAccounts) ListAdmin(ctx context.Context) ([]models.UserAccount, error) {
	return r.find(ctx, bson.M{}, options.Find().SetSort(bson.D{{Key: "linkedAt", Value: -1}}))
}

func (r *UserAccounts) Get(ctx context.Context, uuid string) (*models.UserAccount, error) {
	return r.findOne(ctx, bson.M{"uuid": uuid})
}

// GetSelected returns the user's selected registration for provider, or
// ErrNotFound when there is none.
func (r *UserAccounts) GetSelected(ctx context.Context, userUUID, provider string) (*models.UserAccount, error) {
	return r.findOne(ctx, bson.M{"userUuid": userUUID, "provider": provider, "selected": true})
}

func (r *UserAccounts) findOne(ctx context.Context, extra bson.M) (*models.UserAccount, error) {
	filter, err := tenantrepo.Scope(ctx, extra)
	if err != nil {
		return nil, err
	}
	var a models.UserAccount
	if err := r.coll.FindOne(ctx, filter).Decode(&a); err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return &a, nil
}

// Update replaces the stored registration (matched on org and uuid) with
// a, whole-document. Callers that must not clobber a concurrent writer
// (token refresh, status, last use) need field-targeted writers instead.
func (r *UserAccounts) Update(ctx context.Context, a *models.UserAccount) error {
	filter, err := tenantrepo.Scope(ctx, bson.M{"uuid": a.UUID})
	if err != nil {
		return err
	}
	// The replacement carries the caller's org so a stale or zero TenantID
	// on a can never move the document out of its tenant.
	tenantID, err := tenantrepo.StampInsert(ctx)
	if err != nil {
		return err
	}
	a.TenantID = tenantID
	a.UpdatedAt = time.Now().UTC()
	res, err := r.coll.ReplaceOne(ctx, filter, a)
	if err != nil {
		if mongo.IsDuplicateKeyError(err) {
			return ErrDuplicateName
		}
		return err
	}
	if res.MatchedCount == 0 {
		return ErrNotFound
	}
	return nil
}

// Select makes uuid the user's selected registration for provider and
// deselects the others, in one transaction: the partial unique index on
// (tenant, user, provider, selected=true) means exactly one row holds the
// flag. An unknown uuid (or another user's, or another org's) is
// ErrNotFound and rolls the deselect back, so the previous selection
// survives.
func (r *UserAccounts) Select(ctx context.Context, userUUID, provider, uuid string) error {
	return WithTxn(ctx, r.coll.Database(), func(sc mongo.SessionContext) error {
		// Scoped inside the closure: WithTransaction may run it more than
		// once, and each run derives its filters from the session context.
		scopeAll, err := tenantrepo.Scope(sc, bson.M{"userUuid": userUUID, "provider": provider, "selected": true})
		if err != nil {
			return err
		}
		scopeOne, err := tenantrepo.Scope(sc, bson.M{"uuid": uuid, "userUuid": userUUID, "provider": provider})
		if err != nil {
			return err
		}
		now := time.Now().UTC()
		if _, err := r.coll.UpdateMany(sc, scopeAll, bson.M{"$set": bson.M{"selected": false, "updatedAt": now}}); err != nil {
			return err
		}
		res, err := r.coll.UpdateOne(sc, scopeOne, bson.M{"$set": bson.M{"selected": true, "updatedAt": now}})
		if err != nil {
			return err
		}
		if res.MatchedCount == 0 {
			return ErrNotFound
		}
		return nil
	})
}

func (r *UserAccounts) Delete(ctx context.Context, uuid string) error {
	filter, err := tenantrepo.Scope(ctx, bson.M{"uuid": uuid})
	if err != nil {
		return err
	}
	res, err := r.coll.DeleteOne(ctx, filter)
	if err != nil {
		return err
	}
	if res.DeletedCount == 0 {
		return ErrNotFound
	}
	return nil
}

// ListByUserAllTenants exports every registration of a data subject for
// DSR. The sealed envelopes are part of the rows; the PIIProducer maps
// them to the envelope-free export shape.
func (r *UserAccounts) ListByUserAllTenants(ctx context.Context, userUUID string) ([]models.UserAccount, error) {
	//tenantscope:allow dsr: right-of-access export of the subject's ChatGPT registrations across every org, invoked only by the compliance DSR pipeline through iface.PIIProducer
	cur, err := r.coll.Find(ctx, bson.M{"userUuid": userUUID})
	if err != nil {
		return nil, err
	}
	out := []models.UserAccount{}
	if err := cur.All(ctx, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// DeleteByUser erases every registration of a data subject across all
// tenants. Only the DSR pipeline (PIIProducer.PurgePersonalData) calls it.
func (r *UserAccounts) DeleteByUser(ctx context.Context, userUUID string) (int64, error) {
	//tenantscope:allow dsr: right-to-erasure of the subject's ChatGPT registrations across every org, invoked only by the compliance DSR pipeline through iface.PIIProducer
	res, err := r.coll.DeleteMany(ctx, bson.M{"userUuid": userUUID})
	if err != nil {
		return 0, err
	}
	return res.DeletedCount, nil
}
