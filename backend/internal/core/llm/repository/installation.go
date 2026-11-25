package repository

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/orkestra/backend/internal/core/llm/models"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// CollInstallation holds the one installation-wide document (no tenantId).
const CollInstallation = "llm_installation"

// Installation owns llm_installation.
type Installation struct{ coll *mongo.Collection }

func NewInstallation(db *mongo.Database) *Installation {
	return &Installation{coll: db.Collection(CollInstallation)}
}

// GetOrCreateHostID returns the stable ext_agent_host_id, minting it on
// first use. $setOnInsert makes concurrent first calls agree; an upsert
// race can still hand the loser a duplicate-key error on _id, in which
// case the winner's document is read back.
func (r *Installation) GetOrCreateHostID(ctx context.Context) (string, error) {
	var doc models.Installation
	//tenantscope:allow system: per-installation OpenAI host identifier, global by OpenAI's contract (one deployment = one host), never tenant data
	err := r.coll.FindOneAndUpdate(ctx,
		bson.M{"_id": models.InstallationDocID},
		bson.M{"$setOnInsert": bson.M{"extAgentHostId": "urn:uuid:" + uuid.NewString(), "createdAt": time.Now().UTC()}},
		options.FindOneAndUpdate().SetUpsert(true).SetReturnDocument(options.After),
	).Decode(&doc)
	if err != nil {
		if !mongo.IsDuplicateKeyError(err) {
			return "", err
		}
		//tenantscope:allow system: per-installation OpenAI host identifier, global by OpenAI's contract (one deployment = one host), never tenant data
		if err := r.coll.FindOne(ctx, bson.M{"_id": models.InstallationDocID}).Decode(&doc); err != nil {
			if errors.Is(err, mongo.ErrNoDocuments) {
				return "", ErrNotFound
			}
			return "", err
		}
	}
	return doc.ExtAgentHostID, nil
}
