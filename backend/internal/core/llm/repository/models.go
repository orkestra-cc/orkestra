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

type Models struct{ coll *mongo.Collection }

func NewModels(db *mongo.Database) *Models { return &Models{coll: db.Collection(CollModels)} }

func (r *Models) Insert(ctx context.Context, m *models.Model) error {
	tenantID, err := tenantrepo.StampInsert(ctx)
	if err != nil {
		return err
	}
	m.TenantID = tenantID
	if _, err := r.coll.InsertOne(ctx, m); err != nil {
		if mongo.IsDuplicateKeyError(err) {
			return ErrDuplicateName
		}
		return err
	}
	return nil
}

func (r *Models) find(ctx context.Context, extra bson.M) ([]models.Model, error) {
	filter, err := tenantrepo.Scope(ctx, extra)
	if err != nil {
		return nil, err
	}
	cur, err := r.coll.Find(ctx, filter, options.Find().SetSort(bson.D{{Key: "name", Value: 1}}))
	if err != nil {
		return nil, err
	}
	out := []models.Model{}
	if err := cur.All(ctx, &out); err != nil {
		return nil, err
	}
	return out, nil
}

func (r *Models) List(ctx context.Context) ([]models.Model, error) { return r.find(ctx, bson.M{}) }

func (r *Models) ListActive(ctx context.Context) ([]models.Model, error) {
	return r.find(ctx, bson.M{"status": models.ModelStatusActive})
}

func (r *Models) ListActiveByCredential(ctx context.Context, credentialUUID string) ([]models.Model, error) {
	return r.find(ctx, bson.M{"status": models.ModelStatusActive, "credentialRef.credentialUuid": credentialUUID})
}

func (r *Models) Get(ctx context.Context, uuid string) (*models.Model, error) {
	filter, err := tenantrepo.Scope(ctx, bson.M{"uuid": uuid})
	if err != nil {
		return nil, err
	}
	var m models.Model
	if err := r.coll.FindOne(ctx, filter).Decode(&m); err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return &m, nil
}

// Update writes the editable fields of m onto the document identified by
// (tenant, m.UUID) with a targeted $set and bumps UpdatedAt. TenantID is
// re-stamped from the context. It never touches access, nor the immutable
// uuid, tenantId, createdBy and createdAt: access is owned by SetAccess, so a
// patch that loaded a stale access cannot revert a concurrent grants change.
// An unset BudgetReserveOutputTokens is removed from the document.
func (r *Models) Update(ctx context.Context, m *models.Model) error {
	filter, err := tenantrepo.Scope(ctx, bson.M{"uuid": m.UUID})
	if err != nil {
		return err
	}
	tenantID, err := tenantrepo.StampInsert(ctx)
	if err != nil {
		return err
	}
	m.TenantID = tenantID
	m.UpdatedAt = time.Now().UTC()
	set := bson.M{
		"name":          m.Name,
		"provider":      m.Provider,
		"modelId":       m.ModelID,
		"capabilities":  m.Capabilities,
		"credentialRef": m.CredentialRef,
		"defaults":      m.Defaults,
		"purposes":      m.Purposes,
		"status":        m.Status,
		"updatedAt":     m.UpdatedAt,
	}
	update := bson.M{"$set": set}
	if m.BudgetReserveOutputTokens != nil {
		set["budgetReserveOutputTokens"] = *m.BudgetReserveOutputTokens
	} else {
		update["$unset"] = bson.M{"budgetReserveOutputTokens": ""}
	}
	res, err := r.coll.UpdateOne(ctx, filter, update)
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

// SetAccess sets access (and bumps updatedAt) on the model identified by
// (tenant, uuid) and nothing else. It is the only writer of access, so it
// cannot clobber a concurrent Update of the other fields.
func (r *Models) SetAccess(ctx context.Context, uuid, access string) error {
	filter, err := tenantrepo.Scope(ctx, bson.M{"uuid": uuid})
	if err != nil {
		return err
	}
	res, err := r.coll.UpdateOne(ctx, filter, bson.M{"$set": bson.M{"access": access, "updatedAt": time.Now().UTC()}})
	if err != nil {
		return err
	}
	if res.MatchedCount == 0 {
		return ErrNotFound
	}
	return nil
}

func (r *Models) Delete(ctx context.Context, uuid string) error {
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

// ListByCreator exports every model the data subject created, across all
// tenants.
func (r *Models) ListByCreator(ctx context.Context, userUUID string) ([]models.Model, error) {
	//tenantscope:allow dsr: right-of-access export of models created by the subject across every org, invoked only by the compliance DSR pipeline through iface.PIIProducer
	cur, err := r.coll.Find(ctx, bson.M{"createdBy": userUUID})
	if err != nil {
		return nil, err
	}
	out := []models.Model{}
	if err := cur.All(ctx, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// PseudonymizeCreator rewrites createdBy to models.ErasedActor on every
// model the subject created, across all tenants. The models themselves are
// org assets and stay.
func (r *Models) PseudonymizeCreator(ctx context.Context, userUUID string) (int64, error) {
	//tenantscope:allow dsr: right-to-erasure pseudonymization of the createdBy actor across every org, invoked only by the compliance DSR pipeline through iface.PIIProducer
	res, err := r.coll.UpdateMany(ctx, bson.M{"createdBy": userUUID}, bson.M{"$set": bson.M{"createdBy": models.ErasedActor}})
	if err != nil {
		return 0, err
	}
	return res.ModifiedCount, nil
}
