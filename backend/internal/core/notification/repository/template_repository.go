package repository

import (
	"context"
	"errors"
	"time"

	"github.com/orkestra/backend/internal/core/notification/models"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// TemplateRepository stores notification templates. System defaults are
// seeded on module Start() and can be overridden by admins.
type TemplateRepository interface {
	GetByID(ctx context.Context, templateID, locale string) (*models.TemplateDoc, error)
	List(ctx context.Context) ([]*models.TemplateDoc, error)
	Upsert(ctx context.Context, doc *models.TemplateDoc) error
	DeleteByID(ctx context.Context, templateID, locale string) error
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

func (r *templateRepository) GetByID(ctx context.Context, templateID, locale string) (*models.TemplateDoc, error) {
	// Prefer exact locale match, fall back to "en".
	if locale == "" {
		locale = "en"
	}
	for _, l := range []string{locale, "en"} {
		var doc models.TemplateDoc
		err := r.coll.FindOne(ctx, bson.M{"templateId": templateID, "locale": l}).Decode(&doc)
		if err == nil {
			return &doc, nil
		}
		if !errors.Is(err, mongo.ErrNoDocuments) {
			return nil, err
		}
	}
	return nil, ErrNotFound
}

func (r *templateRepository) List(ctx context.Context) ([]*models.TemplateDoc, error) {
	cursor, err := r.coll.Find(ctx, bson.M{}, options.Find().SetSort(bson.D{{Key: "templateId", Value: 1}, {Key: "locale", Value: 1}}))
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

func (r *templateRepository) Upsert(ctx context.Context, doc *models.TemplateDoc) error {
	now := time.Now()
	if doc.CreatedAt.IsZero() {
		doc.CreatedAt = now
	}
	doc.UpdatedAt = now
	if doc.Version == 0 {
		doc.Version = 1
	}
	_, err := r.coll.UpdateOne(ctx,
		bson.M{"templateId": doc.TemplateID, "locale": doc.Locale},
		bson.M{
			"$set": bson.M{
				"channel":     doc.Channel,
				"subject":     doc.Subject,
				"bodyText":    doc.BodyText,
				"bodyHtml":    doc.BodyHTML,
				"description": doc.Description,
				"variables":   doc.Variables,
				"isSystem":    doc.IsSystem,
				"version":     doc.Version,
				"updatedAt":   doc.UpdatedAt,
			},
			"$setOnInsert": bson.M{
				"uuid":       doc.UUID,
				"templateId": doc.TemplateID,
				"locale":     doc.Locale,
				"createdAt":  doc.CreatedAt,
			},
		},
		options.Update().SetUpsert(true),
	)
	return err
}

func (r *templateRepository) DeleteByID(ctx context.Context, templateID, locale string) error {
	_, err := r.coll.DeleteOne(ctx, bson.M{"templateId": templateID, "locale": locale})
	return err
}

func (r *templateRepository) ExistsSystemTemplate(ctx context.Context, templateID, locale string) (bool, error) {
	count, err := r.coll.CountDocuments(ctx, bson.M{
		"templateId": templateID,
		"locale":     locale,
	})
	if err != nil {
		return false, err
	}
	return count > 0, nil
}
