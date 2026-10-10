// Package repository holds the llm module's Mongo access. Every query goes
// through pkg/sdk/tenantrepo so it is scoped to the caller's org; the only
// cross-tenant queries are the DSR methods marked //tenantscope:allow dsr:
// (Grants.DeleteByUser, Grants.ListByUserAllTenants, and the
// ListBy*/Pseudonymize* actor methods on all three repositories). They are
// reached exclusively from the compliance DSR pipeline through
// iface.PIIProducer.
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

const (
	CollCredentials = "llm_credentials"
	CollModels      = "llm_models"
	CollGrants      = "llm_grants"
)

var (
	ErrNotFound      = errors.New("llm repository: not found")
	ErrDuplicateName = errors.New("llm repository: duplicate name")
)

type Credentials struct{ coll *mongo.Collection }

func NewCredentials(db *mongo.Database) *Credentials {
	return &Credentials{coll: db.Collection(CollCredentials)}
}

func (r *Credentials) Insert(ctx context.Context, c *models.Credential) error {
	tenantID, err := tenantrepo.StampInsert(ctx)
	if err != nil {
		return err
	}
	c.TenantID = tenantID
	if _, err := r.coll.InsertOne(ctx, c); err != nil {
		if mongo.IsDuplicateKeyError(err) {
			return ErrDuplicateName
		}
		return err
	}
	return nil
}

func (r *Credentials) List(ctx context.Context) ([]models.Credential, error) {
	filter, err := tenantrepo.Scope(ctx, bson.M{})
	if err != nil {
		return nil, err
	}
	cur, err := r.coll.Find(ctx, filter, options.Find().SetSort(bson.D{{Key: "name", Value: 1}}))
	if err != nil {
		return nil, err
	}
	out := []models.Credential{}
	if err := cur.All(ctx, &out); err != nil {
		return nil, err
	}
	return out, nil
}

func (r *Credentials) Get(ctx context.Context, uuid string) (*models.Credential, error) {
	filter, err := tenantrepo.Scope(ctx, bson.M{"uuid": uuid})
	if err != nil {
		return nil, err
	}
	var c models.Credential
	if err := r.coll.FindOne(ctx, filter).Decode(&c); err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return &c, nil
}

// Patch writes only the fields p provides — name, baseUrl (unset when the
// provided value is empty), status — plus updatedAt, with a targeted $set on
// (tenant, uuid), and returns the updatedAt it wrote. Fields the request did
// not provide are never written, so a patch built from a stale read cannot
// revert a concurrent disable, endpoint change or rename. It owns nothing
// else: the secret and its display tail belong to SetSecret, createdBy to
// the DSR pseudonymization, and createdAt, tenantId and uuid never change.
func (r *Credentials) Patch(ctx context.Context, uuid string, p models.CredentialPatch) (time.Time, error) {
	filter, err := tenantrepo.Scope(ctx, bson.M{"uuid": uuid})
	if err != nil {
		return time.Time{}, err
	}
	now := time.Now().UTC()
	set := bson.M{"updatedAt": now}
	update := bson.M{"$set": set}
	if p.Name != nil {
		set["name"] = *p.Name
	}
	if p.Status != nil {
		set["status"] = *p.Status
	}
	if p.BaseURL != nil {
		if *p.BaseURL != "" {
			set["baseUrl"] = *p.BaseURL
		} else {
			update["$unset"] = bson.M{"baseUrl": ""}
		}
	}
	res, err := r.coll.UpdateOne(ctx, filter, update)
	if err != nil {
		if mongo.IsDuplicateKeyError(err) {
			return time.Time{}, ErrDuplicateName
		}
		return time.Time{}, err
	}
	if res.MatchedCount == 0 {
		return time.Time{}, ErrNotFound
	}
	return now, nil
}

// SetSecret is the only writer of the secret envelope and its display tail:
// a targeted $set of secret, secretLast4 (unset when empty, the tail of a
// short secret) and updatedAt on (tenant, uuid). A new secret also clears
// the reserved lastTest* result, which described the previous one. It
// returns the updatedAt it wrote.
func (r *Credentials) SetSecret(ctx context.Context, uuid string, env models.Envelope, last4 string) (time.Time, error) {
	filter, err := tenantrepo.Scope(ctx, bson.M{"uuid": uuid})
	if err != nil {
		return time.Time{}, err
	}
	now := time.Now().UTC()
	set := bson.M{"secret": env, "updatedAt": now}
	unset := bson.M{"lastTestedAt": "", "lastTestStatus": "", "lastTestError": ""}
	if last4 != "" {
		set["secretLast4"] = last4
	} else {
		unset["secretLast4"] = ""
	}
	res, err := r.coll.UpdateOne(ctx, filter, bson.M{"$set": set, "$unset": unset})
	if err != nil {
		return time.Time{}, err
	}
	if res.MatchedCount == 0 {
		return time.Time{}, ErrNotFound
	}
	return now, nil
}

func (r *Credentials) Delete(ctx context.Context, uuid string) error {
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

// ListByCreator exports every credential the data subject created, across
// all tenants (the secret envelope is never serialized: json:"-").
func (r *Credentials) ListByCreator(ctx context.Context, userUUID string) ([]models.Credential, error) {
	//tenantscope:allow dsr: right-of-access export of credentials created by the subject across every org, invoked only by the compliance DSR pipeline through iface.PIIProducer
	cur, err := r.coll.Find(ctx, bson.M{"createdBy": userUUID})
	if err != nil {
		return nil, err
	}
	out := []models.Credential{}
	if err := cur.All(ctx, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// PseudonymizeCreator rewrites createdBy to models.ErasedActor on every
// credential the subject created, across all tenants. The credentials
// themselves are org assets and stay.
func (r *Credentials) PseudonymizeCreator(ctx context.Context, userUUID string) (int64, error) {
	//tenantscope:allow dsr: right-to-erasure pseudonymization of the createdBy actor across every org, invoked only by the compliance DSR pipeline through iface.PIIProducer
	res, err := r.coll.UpdateMany(ctx, bson.M{"createdBy": userUUID}, bson.M{"$set": bson.M{"createdBy": models.ErasedActor}})
	if err != nil {
		return 0, err
	}
	return res.ModifiedCount, nil
}
