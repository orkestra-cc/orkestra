package repository

import (
	"context"

	"github.com/orkestra/backend/internal/core/retrieval/models"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// SearchLogDSR is the data-subject and tenant-purge view of retrieval_search_log
// (ADR-0023 D12). Deletes run in bounded batches and return the total.
type SearchLogDSR struct{ coll *mongo.Collection }

func NewSearchLogDSR(db *mongo.Database) *SearchLogDSR {
	return &SearchLogDSR{coll: db.Collection(CollSearchLog)}
}

func userSearchFilter(userUUID string) bson.M {
	return bson.M{"$or": []bson.M{{"userUuid": userUUID}, {"subjects": userSubjectPrefix + userUUID}}}
}

// ListByUserAllTenants returns every row where the user searched or is a
// returned subject, across tenants, oldest first.
func (r *SearchLogDSR) ListByUserAllTenants(ctx context.Context, userUUID string) ([]models.SearchLog, error) {
	//tenantscope:allow dsr: right-of-access/erasure of the subject across every org, invoked only by the compliance DSR pipeline through iface.PIIProducer
	cur, err := r.coll.Find(ctx, userSearchFilter(userUUID), options.Find().SetSort(bson.D{{Key: "createdAt", Value: 1}}))
	if err != nil {
		return nil, err
	}
	var out []models.SearchLog
	if err := cur.All(ctx, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// DeleteByUserAllTenants deletes every row where the user searched or is a
// returned subject, across tenants.
func (r *SearchLogDSR) DeleteByUserAllTenants(ctx context.Context, userUUID string) (int64, error) {
	return deleteInBatches(ctx, r.coll, userSearchFilter(userUUID))
}

// DeleteBySubject deletes the tenant's rows that returned subject.
func (r *SearchLogDSR) DeleteBySubject(ctx context.Context, tenantID, subject string) (int64, error) {
	return deleteInBatches(ctx, r.coll, bson.M{"tenantId": tenantID, "subjects": subject})
}

// DeleteByTenant deletes every row of the tenant.
func (r *SearchLogDSR) DeleteByTenant(ctx context.Context, tenantID string) (int64, error) {
	return deleteInBatches(ctx, r.coll, bson.M{"tenantId": tenantID})
}
