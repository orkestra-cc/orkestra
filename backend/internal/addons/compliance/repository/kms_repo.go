package repository

import (
	"context"
	stderrors "errors"
	"time"

	"github.com/orkestra/backend/internal/addons/compliance/models"
	"github.com/orkestra/backend/pkg/sdk/iface"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
)

// KMSKeyRepository is the persistence boundary for the kms_keys
// collection. Separate from the audit repo so each can evolve its own
// indexes / retention rules.
type KMSKeyRepository struct {
	coll *mongo.Collection
}

// NewKMSKeyRepo returns a repository bound to the compliance_kms_keys
// collection on db.
func NewKMSKeyRepo(db *mongo.Database) *KMSKeyRepository {
	return &KMSKeyRepository{coll: db.Collection(models.KMSKeysCollection)}
}

// Insert appends a freshly-minted KMS key row.
//
// this is an unscoped insert by design — the row IS the tenant-scope anchor.
//
//tenantscope:allow kms keys are keyed by tenantUuid stamped on the row itself;
func (r *KMSKeyRepository) Insert(ctx context.Context, key *models.KMSKey) error {
	_, err := r.coll.InsertOne(ctx, key)
	return err
}

// GetByTenant returns the active key row for tenantUUID. Returns
// ErrKMSKeyNotFound if no row exists (tenant predates KMS rollout or
// compliance was disabled at provisioning).
//
// authoritatively supplied by the caller (tenant service or envelope cipher).
//
//tenantscope:allow kms lookup is performed by tenantUUID argument, which is
func (r *KMSKeyRepository) GetByTenant(ctx context.Context, tenantUUID string) (*models.KMSKey, error) {
	var row models.KMSKey
	//tenantscope:allow lookup by tenantUUID argument (crypto-shred key metadata)
	err := r.coll.FindOne(ctx, bson.M{"tenantUuid": tenantUUID}).Decode(&row)
	if stderrors.Is(err, mongo.ErrNoDocuments) {
		return nil, iface.ErrKMSKeyNotFound
	}
	if err != nil {
		return nil, err
	}
	return &row, nil
}

// GetByUUID fetches the key metadata by its own UUID (keyID). The
// envelope cipher calls this on every Decrypt.
//
// shredded rows still resolvable for "was this ever a key?" queries.
//
//tenantscope:allow kms key lookup by uuid is the audit-trail primary key —
func (r *KMSKeyRepository) GetByUUID(ctx context.Context, keyUUID string) (*models.KMSKey, error) {
	var row models.KMSKey
	//tenantscope:allow key lookup by opaque keyID argument (shred-safe audit trail)
	err := r.coll.FindOne(ctx, bson.M{"uuid": keyUUID}).Decode(&row)
	if stderrors.Is(err, mongo.ErrNoDocuments) {
		return nil, iface.ErrKMSKeyNotFound
	}
	if err != nil {
		return nil, err
	}
	return &row, nil
}

// Shred clears the wrapped DEK and flips the row state to
// pending_deletion. The row survives so auditors can see that a key
// existed + when it was shredded; the key material is gone so every
// ciphertext wrapped with it is unrecoverable.
//
// itself is the tenant-scope anchor (one key per tenant).
//
//tenantscope:allow shred-by-keyID is the crypto-shred primitive; the keyID
func (r *KMSKeyRepository) Shred(ctx context.Context, keyUUID string) error {
	now := time.Now().UTC()
	//tenantscope:allow crypto-shred targets a specific keyID; no tenant scope applicable
	res, err := r.coll.UpdateOne(ctx, bson.M{"uuid": keyUUID},
		bson.M{"$set": bson.M{
			"state":      models.KMSStatePendingDeletion,
			"shreddedAt": now,
		}, "$unset": bson.M{
			"encryptedDek": "",
			"nonce":        "",
		}},
	)
	if err != nil {
		return err
	}
	if res.MatchedCount == 0 {
		return iface.ErrKMSKeyNotFound
	}
	return nil
}
