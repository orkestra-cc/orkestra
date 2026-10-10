package repository

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/orkestra/backend/internal/core/llm/models"
	"github.com/orkestra/backend/pkg/sdk/tenantrepo"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

type Grants struct{ coll *mongo.Collection }

func NewGrants(db *mongo.Database) *Grants { return &Grants{coll: db.Collection(CollGrants)} }

func (r *Grants) list(ctx context.Context, extra bson.M) ([]models.LLMGrant, error) {
	filter, err := tenantrepo.Scope(ctx, extra)
	if err != nil {
		return nil, err
	}
	cur, err := r.coll.Find(ctx, filter)
	if err != nil {
		return nil, err
	}
	out := []models.LLMGrant{}
	if err := cur.All(ctx, &out); err != nil {
		return nil, err
	}
	return out, nil
}

func (r *Grants) ListByModel(ctx context.Context, modelUUID string) ([]models.LLMGrant, error) {
	return r.list(ctx, bson.M{"modelUuid": modelUUID})
}

func (r *Grants) ListByUser(ctx context.Context, userUUID string) ([]models.LLMGrant, error) {
	return r.list(ctx, bson.M{"userUuid": userUUID})
}

// Replace makes the grant set of modelUUID exactly userUUIDs: delete what
// is no longer listed, insert what is new. Two steps, no transaction, so
// concurrent calls may interleave; the semantics are last-writer-wins on a
// full-replace endpoint. A unique (tenantId, modelUuid, userUuid) index
// makes a racing insert of the same grant a duplicate-key error; the
// insert is unordered so one duplicate never stops the remaining rows, and
// duplicate-key results are tolerated (the grant already exists, which is
// the desired end state). Any other error is returned.
func (r *Grants) Replace(ctx context.Context, modelUUID, actor string, userUUIDs []string) error {
	tenantID, err := tenantrepo.StampInsert(ctx)
	if err != nil {
		return err
	}
	if userUUIDs == nil {
		userUUIDs = []string{} // a nil slice marshals to null and $nin rejects it
	}
	delFilter, err := tenantrepo.Scope(ctx, bson.M{"modelUuid": modelUUID, "userUuid": bson.M{"$nin": userUUIDs}})
	if err != nil {
		return err
	}
	if _, err := r.coll.DeleteMany(ctx, delFilter); err != nil {
		return err
	}
	existing, err := r.ListByModel(ctx, modelUUID)
	if err != nil {
		return err
	}
	have := map[string]bool{}
	for _, g := range existing {
		have[g.UserUUID] = true
	}
	now := time.Now().UTC()
	var docs []any
	for _, u := range userUUIDs {
		if have[u] {
			continue
		}
		have[u] = true // tolerate duplicates within the request
		docs = append(docs, models.LLMGrant{UUID: uuid.NewString(), TenantID: tenantID, ModelUUID: modelUUID, UserUUID: u, GrantedBy: actor, CreatedAt: now})
	}
	if len(docs) == 0 {
		return nil
	}
	if _, err := r.coll.InsertMany(ctx, docs, options.InsertMany().SetOrdered(false)); err != nil && !onlyDuplicateKeyErrors(err) {
		return err
	}
	return nil
}

// onlyDuplicateKeyErrors is true when every write error of an unordered
// bulk insert is a duplicate key. A command-level or network error (no
// BulkWriteException) is never swallowed.
func onlyDuplicateKeyErrors(err error) bool {
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

func (r *Grants) DeleteByModel(ctx context.Context, modelUUID string) error {
	filter, err := tenantrepo.Scope(ctx, bson.M{"modelUuid": modelUUID})
	if err != nil {
		return err
	}
	_, err = r.coll.DeleteMany(ctx, filter)
	return err
}

// DeleteByUser erases every grant of a data subject across all tenants.
// Only the DSR pipeline (PIIProducer.PurgePersonalData) calls it.
func (r *Grants) DeleteByUser(ctx context.Context, userUUID string) (int64, error) {
	//tenantscope:allow dsr: right-to-erasure of the subject across every org, invoked only by the compliance DSR pipeline through iface.PIIProducer
	res, err := r.coll.DeleteMany(ctx, bson.M{"userUuid": userUUID})
	if err != nil {
		return 0, err
	}
	return res.DeletedCount, nil
}

// ListByUserAllTenants exports every grant of a data subject for DSR.
func (r *Grants) ListByUserAllTenants(ctx context.Context, userUUID string) ([]models.LLMGrant, error) {
	//tenantscope:allow dsr: right-of-access export of the subject across every org, invoked only by the compliance DSR pipeline through iface.PIIProducer
	cur, err := r.coll.Find(ctx, bson.M{"userUuid": userUUID})
	if err != nil {
		return nil, err
	}
	out := []models.LLMGrant{}
	if err := cur.All(ctx, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// ListByGranter exports every grant the data subject handed out (the
// grantedBy actor), across all tenants.
func (r *Grants) ListByGranter(ctx context.Context, userUUID string) ([]models.LLMGrant, error) {
	//tenantscope:allow dsr: right-of-access export of grants issued by the subject across every org, invoked only by the compliance DSR pipeline through iface.PIIProducer
	cur, err := r.coll.Find(ctx, bson.M{"grantedBy": userUUID})
	if err != nil {
		return nil, err
	}
	out := []models.LLMGrant{}
	if err := cur.All(ctx, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// PseudonymizeGranter rewrites grantedBy to models.ErasedActor on every
// grant the subject issued, across all tenants. The grants (for other
// users) stay.
func (r *Grants) PseudonymizeGranter(ctx context.Context, userUUID string) (int64, error) {
	//tenantscope:allow dsr: right-to-erasure pseudonymization of the grantedBy actor across every org, invoked only by the compliance DSR pipeline through iface.PIIProducer
	res, err := r.coll.UpdateMany(ctx, bson.M{"grantedBy": userUUID}, bson.M{"$set": bson.M{"grantedBy": models.ErasedActor}})
	if err != nil {
		return 0, err
	}
	return res.ModifiedCount, nil
}
