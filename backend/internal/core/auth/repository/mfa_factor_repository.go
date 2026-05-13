package repository

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/orkestra/backend/internal/core/auth/models"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// ErrMFAFactorNotFound signals that no matching factor exists for the user.
var ErrMFAFactorNotFound = errors.New("mfa factor not found")

// MFAFactorRepository persists the MFA factor rows owned by the auth module.
// All methods are context-aware and return ErrMFAFactorNotFound when the
// caller should treat the absence as "not enrolled" rather than an error.
type MFAFactorRepository interface {
	Insert(ctx context.Context, doc *models.MFAFactorDoc) error
	FindByUserAndType(ctx context.Context, userUUID string, factorType models.MFAFactorType) (*models.MFAFactorDoc, error)
	UpdateLastUsed(ctx context.Context, uuid string, when time.Time) error
	// AdvanceLastUsedStep atomically updates LastUsedStep only if the
	// supplied step is strictly greater than the stored value. Returns
	// (true, nil) when the write landed, (false, nil) when another
	// concurrent verify beat this one to the same step. The concurrency
	// guarantee is what prevents replay within the 30-second TOTP window.
	AdvanceLastUsedStep(ctx context.Context, uuid string, step int64, when time.Time) (bool, error)
	ConsumeBackupCode(ctx context.Context, userUUID, hashedCode string) (bool, error)
	// ReplaceBackupCodes atomically swaps the user's TOTP-factor backup
	// code list for a fresh set. Used by self-service regeneration —
	// the old codes must stop working immediately, hence $set rather
	// than the consume-one-at-a-time $pull contract above. Returns
	// ErrMFAFactorNotFound when no TOTP row exists for the user.
	ReplaceBackupCodes(ctx context.Context, userUUID string, hashedCodes []string) error
	Delete(ctx context.Context, uuid string) error
	// DeleteAllByUser hard-deletes every MFA factor row for the user.
	// Used by the GDPR DSR right-to-erasure pipeline — the rows contain
	// encrypted TOTP secrets and hashed backup codes tied to userUUID.
	DeleteAllByUser(ctx context.Context, userUUID string) (int64, error)

	// AppendWebAuthnCredential upserts the user's webauthn factor row and
	// pushes the new credential into its embedded list. Implementation is
	// idempotent on the (userUuid, type=webauthn) document, so the first
	// caller creates the row, subsequent callers extend it.
	AppendWebAuthnCredential(ctx context.Context, userUUID string, cred models.WebAuthnCredential) error
	// UpdateWebAuthnCredential atomically updates a single embedded
	// credential's sign counter, last-used timestamp, and clone-warning
	// flag, matched by credentialId. Returns (true, nil) on a successful
	// write; (false, nil) when no document matched (already removed).
	UpdateWebAuthnCredential(ctx context.Context, userUUID string, credentialID []byte, signCount uint32, when time.Time, cloneWarning bool) (bool, error)
	// RemoveWebAuthnCredential pulls one credential by ID from the user's
	// webauthn factor row. When the resulting list is empty the row is
	// deleted so MFAStatus reverts to not-enrolled. Returns (true, nil)
	// when a credential was removed.
	RemoveWebAuthnCredential(ctx context.Context, userUUID string, credentialID []byte) (bool, error)
}

type mfaFactorRepository struct {
	coll *mongo.Collection
	// tier — see authSessionRepository.tier (ADR-0003 PR-D).
	tier string
}

// NewOperatorMFAFactorRepository binds to operator_mfa_factors and
// stamps Tier="operator" on every Insert / AppendWebAuthnCredential
// write. ADR-0003 PR-D.
func NewOperatorMFAFactorRepository(db *mongo.Database) MFAFactorRepository {
	return &mfaFactorRepository{
		coll: db.Collection(models.OperatorMFAFactorsCollection),
		tier: models.TierOperator,
	}
}

// NewClientMFAFactorRepository binds to client_mfa_factors and stamps
// Tier="client" on writes. ADR-0003 PR-D.
func NewClientMFAFactorRepository(db *mongo.Database) MFAFactorRepository {
	return &mfaFactorRepository{
		coll: db.Collection(models.ClientMFAFactorsCollection),
		tier: models.TierClient,
	}
}

func (r *mfaFactorRepository) Insert(ctx context.Context, doc *models.MFAFactorDoc) error {
	if doc.CreatedAt.IsZero() {
		doc.CreatedAt = time.Now()
	}
	if r.tier != "" {
		doc.Tier = r.tier
	}
	_, err := r.coll.InsertOne(ctx, doc)
	return err
}

func (r *mfaFactorRepository) FindByUserAndType(ctx context.Context, userUUID string, factorType models.MFAFactorType) (*models.MFAFactorDoc, error) {
	var doc models.MFAFactorDoc
	err := r.coll.FindOne(ctx, bson.M{"userUuid": userUUID, "type": factorType}).Decode(&doc)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return nil, ErrMFAFactorNotFound
	}
	if err != nil {
		return nil, err
	}
	return &doc, nil
}

func (r *mfaFactorRepository) UpdateLastUsed(ctx context.Context, uuid string, when time.Time) error {
	_, err := r.coll.UpdateOne(ctx,
		bson.M{"uuid": uuid},
		bson.M{"$set": bson.M{"lastUsedAt": when}},
	)
	return err
}

// AdvanceLastUsedStep updates the step only if strictly newer than the
// stored value. The filter's `$lt` predicate turns the CAS into a single
// atomic op; a concurrent Verify that races a second call with the same
// step sees ModifiedCount==0 and knows its code was already consumed.
func (r *mfaFactorRepository) AdvanceLastUsedStep(ctx context.Context, uuid string, step int64, when time.Time) (bool, error) {
	res, err := r.coll.UpdateOne(ctx,
		bson.M{
			"uuid": uuid,
			"$or": []bson.M{
				{"lastUsedStep": bson.M{"$lt": step}},
				{"lastUsedStep": bson.M{"$exists": false}},
			},
		},
		bson.M{"$set": bson.M{"lastUsedStep": step, "lastUsedAt": when}},
	)
	if err != nil {
		return false, err
	}
	return res.ModifiedCount > 0, nil
}

// ConsumeBackupCode removes a single backup-code hash from the user's factor.
// Returns (true, nil) when a code was removed, (false, nil) when the hash was
// not present (already used or never issued). A non-nil error is only returned
// for driver/io failures.
func (r *mfaFactorRepository) ConsumeBackupCode(ctx context.Context, userUUID, hashedCode string) (bool, error) {
	res, err := r.coll.UpdateOne(ctx,
		bson.M{
			"userUuid":          userUUID,
			"backupCodesHashed": hashedCode,
		},
		bson.M{"$pull": bson.M{"backupCodesHashed": hashedCode}},
	)
	if err != nil {
		return false, err
	}
	return res.ModifiedCount > 0, nil
}

// ReplaceBackupCodes overwrites the backup-code hash list on the user's
// TOTP factor in a single update. A nil/empty hashedCodes slice clears
// the codes (caller's responsibility to refuse that path — the service
// layer always passes a freshly generated list).
func (r *mfaFactorRepository) ReplaceBackupCodes(ctx context.Context, userUUID string, hashedCodes []string) error {
	if hashedCodes == nil {
		hashedCodes = []string{}
	}
	res, err := r.coll.UpdateOne(ctx,
		bson.M{"userUuid": userUUID, "type": models.MFAFactorTOTP},
		bson.M{"$set": bson.M{"backupCodesHashed": hashedCodes}},
	)
	if err != nil {
		return err
	}
	if res.MatchedCount == 0 {
		return ErrMFAFactorNotFound
	}
	return nil
}

func (r *mfaFactorRepository) Delete(ctx context.Context, uuid string) error {
	_, err := r.coll.DeleteOne(ctx, bson.M{"uuid": uuid})
	return err
}

func (r *mfaFactorRepository) DeleteAllByUser(ctx context.Context, userUUID string) (int64, error) {
	res, err := r.coll.DeleteMany(ctx, bson.M{"userUuid": userUUID})
	if err != nil {
		return 0, err
	}
	return res.DeletedCount, nil
}

// AppendWebAuthnCredential upserts the (userUuid, webauthn) row and pushes
// a credential. The upsert keeps creation idempotent — the first call mints
// the row with CreatedAt=now, subsequent calls only $push. SetOnInsert is
// what makes the upsert distinguishable from the $push update path.
func (r *mfaFactorRepository) AppendWebAuthnCredential(ctx context.Context, userUUID string, cred models.WebAuthnCredential) error {
	if cred.CreatedAt.IsZero() {
		cred.CreatedAt = time.Now()
	}
	now := time.Now()
	filter := bson.M{"userUuid": userUUID, "type": models.MFAFactorWebAuthn}
	setOnInsert := bson.M{
		"uuid":       uuid.NewString(),
		"userUuid":   userUUID,
		"type":       models.MFAFactorWebAuthn,
		"createdAt":  now,
		"verifiedAt": now,
	}
	if r.tier != "" {
		setOnInsert["tier"] = r.tier
	}
	update := bson.M{
		"$push":        bson.M{"webauthnCredentials": cred},
		"$setOnInsert": setOnInsert,
	}
	opts := options.Update().SetUpsert(true)
	_, err := r.coll.UpdateOne(ctx, filter, update, opts)
	return err
}

// UpdateWebAuthnCredential bumps a credential's sign counter, last-used
// time, and clone-warning flag in place. Positional operator $ targets the
// matched array element (the credentialId predicate filters by raw bytes).
func (r *mfaFactorRepository) UpdateWebAuthnCredential(ctx context.Context, userUUID string, credentialID []byte, signCount uint32, when time.Time, cloneWarning bool) (bool, error) {
	res, err := r.coll.UpdateOne(ctx,
		bson.M{
			"userUuid":                         userUUID,
			"type":                             models.MFAFactorWebAuthn,
			"webauthnCredentials.credentialId": credentialID,
		},
		bson.M{"$set": bson.M{
			"webauthnCredentials.$.signCount":    signCount,
			"webauthnCredentials.$.lastUsedAt":   when,
			"webauthnCredentials.$.cloneWarning": cloneWarning,
			"lastUsedAt":                         when,
		}},
	)
	if err != nil {
		return false, err
	}
	return res.ModifiedCount > 0, nil
}

// RemoveWebAuthnCredential pulls a credential by ID. When the resulting
// list is empty the row is deleted so subsequent Status calls return
// not-enrolled — keeping an empty webauthn row would lie to the policy
// check at login time.
func (r *mfaFactorRepository) RemoveWebAuthnCredential(ctx context.Context, userUUID string, credentialID []byte) (bool, error) {
	res, err := r.coll.UpdateOne(ctx,
		bson.M{"userUuid": userUUID, "type": models.MFAFactorWebAuthn},
		bson.M{"$pull": bson.M{"webauthnCredentials": bson.M{"credentialId": credentialID}}},
	)
	if err != nil {
		return false, err
	}
	if res.ModifiedCount == 0 {
		return false, nil
	}

	// If the array is now empty, drop the row so the user reverts to
	// not-enrolled. We do this in a follow-up read+delete because Mongo
	// has no atomic "pull and delete-if-empty" — the worst-case race here
	// is leaving an empty row briefly, which still reports correctly via
	// the credential count check at the service layer.
	var doc models.MFAFactorDoc
	err = r.coll.FindOne(ctx, bson.M{"userUuid": userUUID, "type": models.MFAFactorWebAuthn}).Decode(&doc)
	if err == nil && len(doc.WebAuthnCredentials) == 0 {
		_, _ = r.coll.DeleteOne(ctx, bson.M{"uuid": doc.UUID})
	}
	return true, nil
}
