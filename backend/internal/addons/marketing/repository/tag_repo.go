package repository

import (
	"context"
	"errors"
	"regexp"
	"time"

	"github.com/orkestra-cc/orkestra-addon-marketing/models"
	"github.com/orkestra-cc/orkestra-sdk/tenantrepo"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
)

// ErrTagNotFound is returned when a tag lookup finds no document in
// the caller's tenant scope.
var ErrTagNotFound = errors.New("marketing: tag not found")

// TagRepository is the persistence boundary for marketing_tags.
type TagRepository struct {
	coll *mongo.Collection
}

// NewTagRepository binds a repository to the marketing_tags
// collection.
func NewTagRepository(db *mongo.Database) *TagRepository {
	return &TagRepository{coll: db.Collection(models.TagsCollection)}
}

// Create inserts a new tag, stamping tenantId / timestamps. The
// caller computes Path and UUID — the repository does not derive
// either, because both are part of the service's tree-management
// responsibilities (path rebuild on parent change, slug
// auto-generation from name on first create).
func (r *TagRepository) Create(ctx context.Context, t *models.Tag) error {
	if t == nil {
		return errors.New("marketing: nil tag")
	}
	tenantID, err := tenantrepo.StampInsert(ctx)
	if err != nil {
		return err
	}
	t.TenantID = tenantID
	now := time.Now().UTC()
	t.CreatedAt = now
	t.UpdatedAt = now
	_, err = r.coll.InsertOne(ctx, t)
	return err
}

// GetByUUID returns the tag with the given UUID in the caller's
// tenant scope.
func (r *TagRepository) GetByUUID(ctx context.Context, uuid string) (*models.Tag, error) {
	filter, err := tenantrepo.Scope(ctx, bson.M{"uuid": uuid})
	if err != nil {
		return nil, err
	}
	var out models.Tag
	if err := r.coll.FindOne(ctx, filter).Decode(&out); err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			return nil, ErrTagNotFound
		}
		return nil, err
	}
	return &out, nil
}

// GetBySlug returns the tag with the given slug in the caller's
// tenant scope.
func (r *TagRepository) GetBySlug(ctx context.Context, slug string) (*models.Tag, error) {
	filter, err := tenantrepo.Scope(ctx, bson.M{"slug": slug})
	if err != nil {
		return nil, err
	}
	var out models.Tag
	if err := r.coll.FindOne(ctx, filter).Decode(&out); err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			return nil, ErrTagNotFound
		}
		return nil, err
	}
	return &out, nil
}

// List returns every tag in the caller's tenant. The set is bounded
// by tenant configuration (typical: tens to low hundreds) so no
// pagination — the admin UI renders the full tree.
func (r *TagRepository) List(ctx context.Context) ([]models.Tag, error) {
	filter, err := tenantrepo.Scope(ctx, bson.M{})
	if err != nil {
		return nil, err
	}
	cur, err := r.coll.Find(ctx, filter)
	if err != nil {
		return nil, err
	}
	defer cur.Close(ctx)
	out := make([]models.Tag, 0)
	if err := cur.All(ctx, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// ListChildren returns the direct children of the given parent UUID
// (empty string = root tags).
func (r *TagRepository) ListChildren(ctx context.Context, parentUUID string) ([]models.Tag, error) {
	filter, err := tenantrepo.Scope(ctx, bson.M{"parentUuid": parentUUID})
	if err != nil {
		return nil, err
	}
	cur, err := r.coll.Find(ctx, filter)
	if err != nil {
		return nil, err
	}
	defer cur.Close(ctx)
	out := make([]models.Tag, 0)
	if err := cur.All(ctx, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// ListDescendants returns every tag whose path is prefixed by the
// given subtree root path (e.g. "/Industry/Manufacturing" returns
// the entire automotive/heavy/light/etc. subtree underneath).
func (r *TagRepository) ListDescendants(ctx context.Context, pathPrefix string) ([]models.Tag, error) {
	if pathPrefix == "" {
		return r.List(ctx)
	}
	// regexp.QuoteMeta covers metacharacters in path segments.
	filter, err := tenantrepo.Scope(ctx, bson.M{
		"path": bson.M{"$regex": "^" + regexp.QuoteMeta(pathPrefix) + "(/|$)"},
	})
	if err != nil {
		return nil, err
	}
	cur, err := r.coll.Find(ctx, filter)
	if err != nil {
		return nil, err
	}
	defer cur.Close(ctx)
	out := make([]models.Tag, 0)
	if err := cur.All(ctx, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// Update applies $set on mutable fields of a tag.
func (r *TagRepository) Update(ctx context.Context, uuid string, patch bson.M) error {
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
		return ErrTagNotFound
	}
	return nil
}

// Delete hard-deletes a tag by UUID. Cascade on the tags[] arrays of
// Person/Organization is the service's responsibility.
func (r *TagRepository) Delete(ctx context.Context, uuid string) error {
	filter, err := tenantrepo.Scope(ctx, bson.M{"uuid": uuid})
	if err != nil {
		return err
	}
	res, err := r.coll.DeleteOne(ctx, filter)
	if err != nil {
		return err
	}
	if res.DeletedCount == 0 {
		return ErrTagNotFound
	}
	return nil
}
