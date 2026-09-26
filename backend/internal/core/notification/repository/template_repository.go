package repository

import (
	"context"
	"errors"
	"regexp"
	"time"

	"github.com/google/uuid"
	"github.com/orkestra/backend/internal/core/notification/models"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// ErrExists is returned by Create when (owner, templateId, locale) already exists.
var ErrExists = errors.New("notification: template exists")

// TemplateRepository stores notification templates. System defaults are
// seeded on module Start() and can be overridden by admins; documents that
// carry an ownerTenantId belong to a single tenant and are invisible to the
// system reads.
type TemplateRepository interface {
	GetByID(ctx context.Context, templateID, locale string) (*models.TemplateDoc, error)
	GetOwned(ctx context.Context, owner, templateID, locale string) (*models.TemplateDoc, error)
	List(ctx context.Context) ([]*models.TemplateDoc, error)
	Upsert(ctx context.Context, doc *models.TemplateDoc) error
	Create(ctx context.Context, doc *models.TemplateDoc) error
	UpsertOwned(ctx context.Context, doc *models.TemplateDoc) error
	DeleteByID(ctx context.Context, templateID, locale string) error
	DeleteOwnedByPrefix(ctx context.Context, owner, prefix string) (int64, error)
	ExistsSystemTemplate(ctx context.Context, templateID, locale string) (bool, error)
}

type templateRepository struct {
	coll *mongo.Collection
}

func NewTemplateRepository(db *mongo.Database) TemplateRepository {
	return &templateRepository{
		coll: db.Collection(models.NotificationTemplatesCollection),
	}
}

// systemOwner matches system rows: "" after this change, absent on legacy rows.
func systemOwner() bson.M { return bson.M{"$in": bson.A{"", nil}} }

func (r *templateRepository) GetByID(ctx context.Context, templateID, locale string) (*models.TemplateDoc, error) {
	// Prefer exact locale match, fall back to "en".
	if locale == "" {
		locale = "en"
	}
	for _, l := range []string{locale, "en"} {
		var doc models.TemplateDoc
		//tenantscope:allow system: notification templates are owned via ownerTenantId, resolved from ctx by the service
		err := r.coll.FindOne(ctx, bson.M{"ownerTenantId": systemOwner(), "templateId": templateID, "locale": l}).Decode(&doc)
		if err == nil {
			return &doc, nil
		}
		if !errors.Is(err, mongo.ErrNoDocuments) {
			return nil, err
		}
	}
	return nil, ErrNotFound
}

func (r *templateRepository) GetOwned(ctx context.Context, owner, templateID, locale string) (*models.TemplateDoc, error) {
	if owner == "" {
		return nil, ErrNotFound
	}
	var doc models.TemplateDoc
	//tenantscope:allow system: owner is the tenant id the service read from ctx
	err := r.coll.FindOne(ctx, bson.M{"ownerTenantId": owner, "templateId": templateID, "locale": locale}).Decode(&doc)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &doc, nil
}

func (r *templateRepository) List(ctx context.Context) ([]*models.TemplateDoc, error) {
	//tenantscope:allow admin-view: system templates only (ownerTenantId empty), operator admin surface
	cursor, err := r.coll.Find(ctx, bson.M{"ownerTenantId": systemOwner()},
		options.Find().SetSort(bson.D{{Key: "templateId", Value: 1}, {Key: "locale", Value: 1}}))
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)
	var out []*models.TemplateDoc
	for cursor.Next(ctx) {
		var d models.TemplateDoc
		if err := cursor.Decode(&d); err != nil {
			return nil, err
		}
		out = append(out, &d)
	}
	return out, cursor.Err()
}

func (r *templateRepository) upsert(ctx context.Context, filter bson.M, doc *models.TemplateDoc, owner string) error {
	now := time.Now().UTC()
	if doc.Version == 0 {
		doc.Version = 1
	}
	set := bson.M{
		"ownerTenantId": owner, "channel": doc.Channel, "subject": doc.Subject, "bodyText": doc.BodyText,
		"bodyHtml": doc.BodyHTML, "description": doc.Description, "variables": doc.Variables,
		"isSystem": doc.IsSystem, "version": doc.Version, "updatedAt": now,
	}
	uid := doc.UUID
	if uid == "" {
		uid = uuid.Must(uuid.NewV7()).String()
	}
	//tenantscope:allow system: filter carries ownerTenantId chosen by the service (system "" or ctx tenant)
	_, err := r.coll.UpdateOne(ctx, filter, bson.M{
		"$set":         set,
		"$setOnInsert": bson.M{"uuid": uid, "templateId": doc.TemplateID, "locale": doc.Locale, "createdAt": now},
	}, options.Update().SetUpsert(true))
	return err
}

// Upsert writes a SYSTEM template and stamps ownerTenantId "" (a legacy row
// without the field is matched and upgraded, never duplicated).
func (r *templateRepository) Upsert(ctx context.Context, doc *models.TemplateDoc) error {
	return r.upsert(ctx, bson.M{"ownerTenantId": systemOwner(), "templateId": doc.TemplateID, "locale": doc.Locale}, doc, "")
}

func (r *templateRepository) UpsertOwned(ctx context.Context, doc *models.TemplateDoc) error {
	if doc.OwnerTenantID == "" {
		return errors.New("notification: UpsertOwned requires an owner")
	}
	return r.upsert(ctx, bson.M{"ownerTenantId": doc.OwnerTenantID, "templateId": doc.TemplateID, "locale": doc.Locale}, doc, doc.OwnerTenantID)
}

// Create inserts only if absent: the unique index (ownerTenantId, templateId,
// locale) turns a race into ErrExists, never into an overwrite.
func (r *templateRepository) Create(ctx context.Context, doc *models.TemplateDoc) error {
	if doc.OwnerTenantID == "" {
		return errors.New("notification: Create requires an owner")
	}
	now := time.Now().UTC()
	if doc.UUID == "" {
		doc.UUID = uuid.Must(uuid.NewV7()).String()
	}
	if doc.Version == 0 {
		doc.Version = 1
	}
	doc.CreatedAt, doc.UpdatedAt = now, now
	//tenantscope:allow system: notification templates carry ownerTenantId (set by the service from ctx), not a tenantId document
	_, err := r.coll.InsertOne(ctx, doc)
	if mongo.IsDuplicateKeyError(err) {
		return ErrExists
	}
	return err
}

func (r *templateRepository) DeleteByID(ctx context.Context, templateID, locale string) error {
	//tenantscope:allow admin-view: system templates only
	_, err := r.coll.DeleteOne(ctx, bson.M{"ownerTenantId": systemOwner(), "templateId": templateID, "locale": locale})
	return err
}

// prefixRe: a lowercase head, optional segments (letters, digits, dashes) and
// a full UUID at the end — "digest:<uuid>", "preview:test:<uuid>:<uuid>".
// Anything not ending in a UUID could sweep a whole family.
var prefixRe = regexp.MustCompile(`^[a-z]+(:[a-z0-9-]+)*:[0-9a-f-]{36}$`)

func (r *templateRepository) DeleteOwnedByPrefix(ctx context.Context, owner, prefix string) (int64, error) {
	if owner == "" {
		return 0, errors.New("notification: DeleteOwnedByPrefix requires an owner")
	}
	if !prefixRe.MatchString(prefix) {
		return 0, errors.New("notification: prefix must be <segment>(:<segment>)*:<uuid>")
	}
	//tenantscope:allow system: owner is the tenant id the service read from ctx
	res, err := r.coll.DeleteMany(ctx, bson.M{"ownerTenantId": owner, "templateId": bson.M{"$regex": "^" + regexp.QuoteMeta(prefix) + "(:|$)"}})
	if err != nil {
		return 0, err
	}
	return res.DeletedCount, nil
}

func (r *templateRepository) ExistsSystemTemplate(ctx context.Context, templateID, locale string) (bool, error) {
	//tenantscope:allow system: seed check on system templates
	n, err := r.coll.CountDocuments(ctx, bson.M{"ownerTenantId": systemOwner(), "templateId": templateID, "locale": locale})
	return n > 0, err
}
