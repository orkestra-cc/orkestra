package repository

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/orkestra/backend/internal/core/llm/models"
	"github.com/orkestra/backend/pkg/sdk/tenantrepo"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
)

// Grants owns llm_grants and, inside Replace's transaction only, the
// access and grantsRevision fields of llm_models.
type Grants struct {
	coll   *mongo.Collection
	models *mongo.Collection
}

func NewGrants(db *mongo.Database) *Grants {
	return &Grants{coll: db.Collection(CollGrants), models: db.Collection(CollModels)}
}

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

// Replace decides who may use modelUUID: it sets the model's access and
// makes its grant set exactly userUUIDs, in one multi-document transaction
// (replica set required, as in every Orkestra environment). Inside it, the
// model document is written first — $set access and updatedAt, $inc
// grantsRevision — so two concurrent replacements of the same model write
// the same document: the later one hits a write conflict, and
// session.WithTransaction retries it from scratch once the first has
// committed. Replacements are therefore serialized and the end state is
// always exactly one caller's complete request (access and list together),
// never a union of two lists nor one caller's access with another's grants.
// A failure anywhere aborts the whole transaction, so nothing is written.
// An unknown model (or one of another org) is ErrNotFound. Every filter is
// tenantrepo-scoped; the closure uses only the session context it receives,
// because WithTransaction may run it more than once.
func (r *Grants) Replace(ctx context.Context, modelUUID, access, actor string, userUUIDs []string) error {
	tenantID, err := tenantrepo.StampInsert(ctx)
	if err != nil {
		return err
	}
	if userUUIDs == nil {
		userUUIDs = []string{} // a nil slice marshals to null and $nin rejects it
	}
	sess, err := r.coll.Database().Client().StartSession()
	if err != nil {
		return fmt.Errorf("llm: start session: %w", err)
	}
	defer sess.EndSession(ctx)
	_, err = sess.WithTransaction(ctx, func(sc mongo.SessionContext) (any, error) {
		return nil, r.replaceInTxn(sc, tenantID, modelUUID, access, actor, userUUIDs)
	})
	return err
}

func (r *Grants) replaceInTxn(sc mongo.SessionContext, tenantID, modelUUID, access, actor string, userUUIDs []string) error {
	now := time.Now().UTC()
	modelFilter, err := tenantrepo.Scope(sc, bson.M{"uuid": modelUUID})
	if err != nil {
		return err
	}
	res, err := r.models.UpdateOne(sc, modelFilter, bson.M{
		"$set": bson.M{"access": access, "updatedAt": now},
		"$inc": bson.M{"grantsRevision": int64(1)},
	})
	if err != nil {
		return err
	}
	if res.MatchedCount == 0 {
		return ErrNotFound
	}
	delFilter, err := tenantrepo.Scope(sc, bson.M{"modelUuid": modelUUID, "userUuid": bson.M{"$nin": userUUIDs}})
	if err != nil {
		return err
	}
	if _, err := r.coll.DeleteMany(sc, delFilter); err != nil {
		return err
	}
	existing, err := r.ListByModel(sc, modelUUID)
	if err != nil {
		return err
	}
	have := map[string]bool{}
	for _, g := range existing {
		have[g.UserUUID] = true
	}
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
	// No duplicate can come from a concurrent replacement (they are
	// serialized above), and a write error would abort the transaction
	// anyway, so any insert error is a real failure.
	_, err = r.coll.InsertMany(sc, docs)
	return err
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
