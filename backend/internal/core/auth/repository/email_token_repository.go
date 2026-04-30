package repository

import (
	"context"
	"errors"
	"time"

	"github.com/orkestra/backend/internal/core/auth/models"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
)

var ErrEmailTokenNotFound = errors.New("email token not found")

// EmailTokenRepository persists single-use tokens for email verification
// and password reset.
type EmailTokenRepository interface {
	Create(ctx context.Context, doc *models.EmailTokenDoc) error
	GetByHash(ctx context.Context, hash string) (*models.EmailTokenDoc, error)
	MarkUsed(ctx context.Context, hash string) error
	InvalidateByUserAndPurpose(ctx context.Context, userUUID, purpose string) error
	// DeleteAllByUser hard-deletes every email token (used, unused,
	// expired) for the user. Used by the GDPR DSR right-to-erasure
	// pipeline.
	DeleteAllByUser(ctx context.Context, userUUID string) (int64, error)
}

type emailTokenRepository struct {
	coll *mongo.Collection
	// tier — see authSessionRepository.tier (ADR-0003 PR-D).
	tier string
}

// NewEmailTokenRepository wires a repository against the legacy
// `auth_email_tokens` collection. Tier is empty.
func NewEmailTokenRepository(db *mongo.Database) EmailTokenRepository {
	return &emailTokenRepository{
		coll: db.Collection(models.EmailTokensCollection),
	}
}

// NewOperatorEmailTokenRepository binds to operator_email_tokens and
// stamps Tier="operator" on every Create write. ADR-0003 PR-D.
func NewOperatorEmailTokenRepository(db *mongo.Database) EmailTokenRepository {
	return &emailTokenRepository{
		coll: db.Collection(models.OperatorEmailTokensCollection),
		tier: models.TierOperator,
	}
}

// NewClientEmailTokenRepository binds to client_email_tokens and stamps
// Tier="client" on every Create write. ADR-0003 PR-D.
func NewClientEmailTokenRepository(db *mongo.Database) EmailTokenRepository {
	return &emailTokenRepository{
		coll: db.Collection(models.ClientEmailTokensCollection),
		tier: models.TierClient,
	}
}

func (r *emailTokenRepository) Create(ctx context.Context, doc *models.EmailTokenDoc) error {
	if doc.CreatedAt.IsZero() {
		doc.CreatedAt = time.Now()
	}
	if r.tier != "" {
		doc.Tier = r.tier
	}
	_, err := r.coll.InsertOne(ctx, doc)
	return err
}

func (r *emailTokenRepository) GetByHash(ctx context.Context, hash string) (*models.EmailTokenDoc, error) {
	var doc models.EmailTokenDoc
	err := r.coll.FindOne(ctx, bson.M{"tokenHash": hash}).Decode(&doc)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return nil, ErrEmailTokenNotFound
	}
	if err != nil {
		return nil, err
	}
	return &doc, nil
}

func (r *emailTokenRepository) MarkUsed(ctx context.Context, hash string) error {
	now := time.Now()
	_, err := r.coll.UpdateOne(ctx,
		bson.M{"tokenHash": hash},
		bson.M{"$set": bson.M{"usedAt": now}},
	)
	return err
}

func (r *emailTokenRepository) InvalidateByUserAndPurpose(ctx context.Context, userUUID, purpose string) error {
	now := time.Now()
	_, err := r.coll.UpdateMany(ctx,
		bson.M{
			"userUuid": userUUID,
			"purpose":  purpose,
			"usedAt":   bson.M{"$exists": false},
		},
		bson.M{"$set": bson.M{"usedAt": now}},
	)
	return err
}

func (r *emailTokenRepository) DeleteAllByUser(ctx context.Context, userUUID string) (int64, error) {
	res, err := r.coll.DeleteMany(ctx, bson.M{"userUuid": userUUID})
	if err != nil {
		return 0, err
	}
	return res.DeletedCount, nil
}
