package repository

import (
	"context"
	"errors"
	"time"

	"github.com/orkestra-cc/orkestra-addon-marketing/models"
	"github.com/orkestra-cc/orkestra-sdk/tenantrepo"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// ErrPersonNotFound is returned by Person lookups when no document
// matches in the tenant scope.
var ErrPersonNotFound = errors.New("marketing: person not found")

// PersonRepository is the persistence boundary for marketing_persons.
type PersonRepository struct {
	coll *mongo.Collection
}

// NewPersonRepository binds a repository to the marketing_persons
// collection on db.
func NewPersonRepository(db *mongo.Database) *PersonRepository {
	return &PersonRepository{coll: db.Collection(models.PersonsCollection)}
}

// Create inserts a new Person, stamping tenantId / timestamps and
// normalising every email address to lowercase. The caller is
// responsible for populating UUID.
//
// Identity-minimum (HasMinimumIdentity) is checked at the service
// layer, not here — importers occasionally stage incomplete rows for
// later enrichment.
func (r *PersonRepository) Create(ctx context.Context, p *models.Person) error {
	if p == nil {
		return errors.New("marketing: nil person")
	}
	tenantID, err := tenantrepo.StampInsert(ctx)
	if err != nil {
		return err
	}
	p.TenantID = tenantID
	normalizePersonEmails(p)
	now := time.Now().UTC()
	p.CreatedAt = now
	p.UpdatedAt = now
	_, err = r.coll.InsertOne(ctx, p)
	return err
}

// GetByUUID returns the person with the given UUID in the caller's
// tenant scope, or ErrPersonNotFound when no document matches.
func (r *PersonRepository) GetByUUID(ctx context.Context, uuid string) (*models.Person, error) {
	filter, err := tenantrepo.Scope(ctx, bson.M{"uuid": uuid})
	if err != nil {
		return nil, err
	}
	var out models.Person
	if err := r.coll.FindOne(ctx, filter).Decode(&out); err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			return nil, ErrPersonNotFound
		}
		return nil, err
	}
	return &out, nil
}

// LookupByEmail returns the person in the caller's tenant carrying the
// given email address anywhere in their emails[] array (not just on
// the primary entry). The input is normalised before matching so the
// (tenantId, emails.address) unique index can serve the lookup.
func (r *PersonRepository) LookupByEmail(ctx context.Context, email string) (*models.Person, error) {
	email = NormalizeEmail(email)
	if email == "" {
		return nil, ErrPersonNotFound
	}
	filter, err := tenantrepo.Scope(ctx, bson.M{"emails.address": email})
	if err != nil {
		return nil, err
	}
	var out models.Person
	if err := r.coll.FindOne(ctx, filter).Decode(&out); err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			return nil, ErrPersonNotFound
		}
		return nil, err
	}
	return &out, nil
}

// PersonListFilter parameterises the persons read surface. All fields
// optional.
type PersonListFilter struct {
	TagUUIDs []string
	HasEmail bool
	Source   string
	Limit    int64
	Skip     int64
}

// List returns persons matching filter, newest-first by updatedAt.
// Limit defaults to 50 (cap 500).
func (r *PersonRepository) List(ctx context.Context, f PersonListFilter) ([]models.Person, error) {
	base := bson.M{}
	if len(f.TagUUIDs) > 0 {
		base["tags"] = bson.M{"$in": f.TagUUIDs}
	}
	if f.HasEmail {
		base["emails.0"] = bson.M{"$exists": true}
	}
	if f.Source != "" {
		base["sources.importer"] = f.Source
	}
	filter, err := tenantrepo.Scope(ctx, base)
	if err != nil {
		return nil, err
	}
	limit := f.Limit
	if limit <= 0 {
		limit = 50
	}
	if limit > 500 {
		limit = 500
	}
	opts := options.Find().
		SetSort(bson.D{{Key: "updatedAt", Value: -1}}).
		SetLimit(limit).
		SetSkip(f.Skip)
	cur, err := r.coll.Find(ctx, filter, opts)
	if err != nil {
		return nil, err
	}
	defer cur.Close(ctx)
	out := make([]models.Person, 0)
	if err := cur.All(ctx, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// Update applies $set on the mutable fields of the person identified
// by UUID. Returns ErrPersonNotFound when no row matches.
//
// Callers are expected to use the higher-level service when the patch
// touches emails[] / tags[] / sources[] — the repository does not run
// the importer auto-merge logic here.
func (r *PersonRepository) Update(ctx context.Context, uuid string, patch bson.M) error {
	if patch == nil {
		patch = bson.M{}
	}
	patch["updatedAt"] = time.Now().UTC()
	filter, err := tenantrepo.Scope(ctx, bson.M{"uuid": uuid})
	if err != nil {
		return err
	}
	res, err := r.coll.UpdateOne(ctx, filter, bson.M{"$set": patch})
	if err != nil {
		return err
	}
	if res.MatchedCount == 0 {
		return ErrPersonNotFound
	}
	return nil
}

// AppendSource pushes a new provenance entry onto the person's
// sources[] array.
func (r *PersonRepository) AppendSource(ctx context.Context, uuid string, src models.ProvenanceSource) error {
	filter, err := tenantrepo.Scope(ctx, bson.M{"uuid": uuid})
	if err != nil {
		return err
	}
	_, err = r.coll.UpdateOne(ctx, filter, bson.M{
		"$push": bson.M{"sources": src},
		"$set":  bson.M{"updatedAt": time.Now().UTC()},
	})
	return err
}

// Delete hard-deletes a person by UUID inside the caller's tenant.
// The membership cascade is the service's responsibility.
func (r *PersonRepository) Delete(ctx context.Context, uuid string) error {
	filter, err := tenantrepo.Scope(ctx, bson.M{"uuid": uuid})
	if err != nil {
		return err
	}
	res, err := r.coll.DeleteOne(ctx, filter)
	if err != nil {
		return err
	}
	if res.DeletedCount == 0 {
		return ErrPersonNotFound
	}
	return nil
}

func normalizePersonEmails(p *models.Person) {
	for i := range p.Emails {
		p.Emails[i].Address = NormalizeEmail(p.Emails[i].Address)
	}
}
