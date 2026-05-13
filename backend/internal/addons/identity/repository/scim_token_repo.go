package repository

import (
	"context"
	"errors"
	"time"

	"github.com/orkestra-cc/orkestra-addon-identity/models"
	"github.com/orkestra-cc/orkestra-sdk/tenantrepo"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
)

// ErrScimTokenNotFound is returned when no active (non-revoked) token
// matches the given hash or tenant scope.
var ErrScimTokenNotFound = errors.New("identity: SCIM token not found")

// ScimTokenRepository persists hashed SCIM bearer tokens. The repo
// enforces "one active token per tenant" by revoking the previous row
// inside the rotate operation — a non-atomic sequence that is acceptable
// for v1 because rotations are operator-initiated and infrequent.
type ScimTokenRepository struct {
	coll *mongo.Collection
}

// NewScimTokenRepository wires the repo against the shared database.
func NewScimTokenRepository(db *mongo.Database) *ScimTokenRepository {
	return &ScimTokenRepository{coll: db.Collection(models.ScimTokensCollection)}
}

// Rotate revokes the tenant's current active token and inserts a new one
// with the supplied hash. Returns the freshly-inserted row. The tenant
// is taken from request context via tenantrepo.StampInsert — callers must
// run inside an authenticated admin handler.
func (r *ScimTokenRepository) Rotate(ctx context.Context, tokenHash, uuid string) (*models.ScimToken, error) {
	tenantID, err := tenantrepo.StampInsert(ctx)
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	// Revoke any existing live rows for this tenant. RevokedAt nil means
	// active; the update sets it to now so lookups by hash start failing.
	if _, err := r.coll.UpdateMany(ctx,
		bson.M{"tenantId": tenantID, "revokedAt": nil},
		bson.M{"$set": bson.M{"revokedAt": now}},
	); err != nil {
		return nil, err
	}
	doc := &models.ScimToken{
		UUID:      uuid,
		TenantID:  tenantID,
		TokenHash: tokenHash,
		CreatedAt: now,
	}
	if _, err := r.coll.InsertOne(ctx, doc); err != nil {
		return nil, err
	}
	return doc, nil
}

// FindActiveByHash resolves a raw hash to its owning tenant. Used by the
// SCIM auth middleware to convert a Bearer token into a tenant-scoped
// context. Crucially, the lookup is NOT tenant-scoped — the hash *is* the
// scope selector because only one tenant holds it.
//
// Returns ErrScimTokenNotFound on miss or on revoked rows.
func (r *ScimTokenRepository) FindActiveByHash(ctx context.Context, tokenHash string) (*models.ScimToken, error) {
	var out models.ScimToken
	err := r.coll.FindOne(ctx, bson.M{
		"tokenHash": tokenHash,
		"revokedAt": nil,
	}).Decode(&out)
	if err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			return nil, ErrScimTokenNotFound
		}
		return nil, err
	}
	return &out, nil
}

// GetActiveForCurrentTenant returns the tenant's live token row so the
// admin UI can show creation/rotation metadata. Does NOT return the raw
// token — that is only observable once at rotation time.
func (r *ScimTokenRepository) GetActiveForCurrentTenant(ctx context.Context) (*models.ScimToken, error) {
	filter, err := tenantrepo.Scope(ctx, bson.M{"revokedAt": nil})
	if err != nil {
		return nil, err
	}
	var out models.ScimToken
	if err := r.coll.FindOne(ctx, filter).Decode(&out); err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			return nil, ErrScimTokenNotFound
		}
		return nil, err
	}
	return &out, nil
}
