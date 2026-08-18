package repository

import (
	"context"
	"errors"
	"time"

	"github.com/orkestra/backend/internal/core/auth/models"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

var ErrServiceAccountCredentialNotFound = errors.New("service account credential not found")

// ServiceAccountCredentialRepository persists machine credentials for
// service accounts. The backing collection is platform-global (not
// tier-split) — see models.ServiceAccountCredentialsCollection.
type ServiceAccountCredentialRepository interface {
	Create(ctx context.Context, doc *models.ServiceAccountCredential) error
	GetByClientID(ctx context.Context, clientID string) (*models.ServiceAccountCredential, error)
	ListByUser(ctx context.Context, userUUID string) ([]models.ServiceAccountCredential, error)
	CountActiveByUser(ctx context.Context, userUUID string) (int64, error)
	Revoke(ctx context.Context, credentialUUID string, at time.Time) error
	StampLastUsed(ctx context.Context, credentialUUID string, at time.Time) error
}

type serviceAccountCredentialRepository struct {
	coll *mongo.Collection
}

// NewServiceAccountCredentialRepository binds to service_account_credentials.
func NewServiceAccountCredentialRepository(db *mongo.Database) ServiceAccountCredentialRepository {
	return &serviceAccountCredentialRepository{
		coll: db.Collection(models.ServiceAccountCredentialsCollection),
	}
}

func (r *serviceAccountCredentialRepository) Create(ctx context.Context, doc *models.ServiceAccountCredential) error {
	if doc.CreatedAt.IsZero() {
		doc.CreatedAt = time.Now()
	}
	//tenantscope:allow system: platform-global machine credential store, not tier-split — see models.ServiceAccountCredentialsCollection
	_, err := r.coll.InsertOne(ctx, doc)
	return err
}

func (r *serviceAccountCredentialRepository) GetByClientID(ctx context.Context, clientID string) (*models.ServiceAccountCredential, error) {
	var doc models.ServiceAccountCredential
	//tenantscope:allow system: platform-global machine credential store keyed by clientId
	err := r.coll.FindOne(ctx, bson.M{"clientId": clientID}).Decode(&doc)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return nil, ErrServiceAccountCredentialNotFound
	}
	if err != nil {
		return nil, err
	}
	return &doc, nil
}

func (r *serviceAccountCredentialRepository) ListByUser(ctx context.Context, userUUID string) ([]models.ServiceAccountCredential, error) {
	opts := options.Find().SetSort(bson.D{{Key: "createdAt", Value: 1}})
	//tenantscope:allow system: platform-global machine credential store keyed by userUuid
	cursor, err := r.coll.Find(ctx, bson.M{"userUuid": userUUID}, opts)
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)

	var docs []models.ServiceAccountCredential
	if err := cursor.All(ctx, &docs); err != nil {
		return nil, err
	}
	return docs, nil
}

func (r *serviceAccountCredentialRepository) CountActiveByUser(ctx context.Context, userUUID string) (int64, error) {
	//tenantscope:allow system: platform-global machine credential store keyed by userUuid
	return r.coll.CountDocuments(ctx, bson.M{"userUuid": userUUID, "revokedAt": bson.M{"$exists": false}})
}

func (r *serviceAccountCredentialRepository) Revoke(ctx context.Context, credentialUUID string, at time.Time) error {
	//tenantscope:allow system: platform-global machine credential store keyed by uuid
	res, err := r.coll.UpdateOne(ctx, bson.M{"uuid": credentialUUID, "revokedAt": bson.M{"$exists": false}},
		bson.M{"$set": bson.M{"revokedAt": at}})
	if err != nil {
		return err
	}
	if res.MatchedCount == 0 {
		return ErrServiceAccountCredentialNotFound
	}
	return nil
}

func (r *serviceAccountCredentialRepository) StampLastUsed(ctx context.Context, credentialUUID string, at time.Time) error {
	//tenantscope:allow system: platform-global machine credential store keyed by uuid
	res, err := r.coll.UpdateOne(ctx, bson.M{"uuid": credentialUUID},
		bson.M{"$set": bson.M{"lastUsedAt": at}})
	if err != nil {
		return err
	}
	if res.MatchedCount == 0 {
		return ErrServiceAccountCredentialNotFound
	}
	return nil
}
