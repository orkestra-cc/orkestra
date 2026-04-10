package repository

import (
	"context"
	"errors"
	"regexp"
	"time"

	"github.com/google/uuid"
	"github.com/orkestra/backend/internal/addons/documents/models"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// Package-level errors
var (
	ErrTemplateNotFound      = errors.New("template not found")
	ErrTemplateAlreadyExists = errors.New("template with this name already exists")
	ErrCannotDeleteBuiltIn   = errors.New("cannot delete built-in template")
	ErrNoDefaultTemplate     = errors.New("no default template configured for this type")
)

// TemplateRepository defines the interface for template data access
type TemplateRepository interface {
	// Create operations
	Create(ctx context.Context, template *models.Template) error
	CreateBatch(ctx context.Context, templates []*models.Template) error

	// Read operations
	GetByID(ctx context.Context, id string) (*models.Template, error)
	GetByUUID(ctx context.Context, uuid string) (*models.Template, error)
	GetByName(ctx context.Context, name string) (*models.Template, error)
	GetDefault(ctx context.Context, templateType models.TemplateType) (*models.Template, error)
	List(ctx context.Context, filters *models.TemplateFilters, pagination models.PaginationParams) ([]models.Template, int64, error)
	ListByType(ctx context.Context, templateType models.TemplateType) ([]models.Template, error)
	ListActive(ctx context.Context) ([]models.Template, error)

	// Update operations
	Update(ctx context.Context, template *models.Template) error
	SetDefault(ctx context.Context, uuid string, templateType models.TemplateType) error
	ClearDefault(ctx context.Context, templateType models.TemplateType) error

	// Delete operations
	SoftDelete(ctx context.Context, uuid string) error

	// Utility
	ExistsByName(ctx context.Context, name string) (bool, error)
	ExistsByUUID(ctx context.Context, uuid string) (bool, error)
	Count(ctx context.Context) (int64, error)
	CountByType(ctx context.Context, templateType models.TemplateType) (int64, error)
}

type templateRepository struct {
	collection *mongo.Collection
}

// NewTemplateRepository creates a new template repository instance
func NewTemplateRepository(db *mongo.Database) TemplateRepository {
	repo := &templateRepository{
		collection: db.Collection(models.DocumentTemplatesCollection),
	}
	repo.createIndexes(context.Background())
	return repo
}

func (r *templateRepository) createIndexes(ctx context.Context) {
	indexes := []mongo.IndexModel{
		{
			Keys:    bson.D{{Key: "uuid", Value: 1}},
			Options: options.Index().SetUnique(true),
		},
		{
			Keys:    bson.D{{Key: "name", Value: 1}},
			Options: options.Index().SetUnique(true).SetSparse(true),
		},
		{
			Keys: bson.D{{Key: "type", Value: 1}},
		},
		{
			Keys: bson.D{{Key: "isDefault", Value: 1}},
		},
		{
			Keys: bson.D{{Key: "isBuiltIn", Value: 1}},
		},
		{
			Keys: bson.D{{Key: "isActive", Value: 1}},
		},
		{
			Keys:    bson.D{{Key: "deletedAt", Value: 1}},
			Options: options.Index().SetSparse(true),
		},
		{
			Keys: bson.D{{Key: "createdAt", Value: -1}},
		},
		// Compound index for type + isDefault (for finding default template)
		{
			Keys: bson.D{
				{Key: "type", Value: 1},
				{Key: "isDefault", Value: 1},
			},
		},
	}
	_, _ = r.collection.Indexes().CreateMany(ctx, indexes)
}

// Create creates a new template
func (r *templateRepository) Create(ctx context.Context, template *models.Template) error {
	if template.UUID == "" {
		template.UUID = uuid.New().String()
	}
	now := time.Now()
	template.CreatedAt = now
	template.UpdatedAt = now
	template.Version = 1

	_, err := r.collection.InsertOne(ctx, template)
	if mongo.IsDuplicateKeyError(err) {
		return ErrTemplateAlreadyExists
	}
	return err
}

// CreateBatch creates multiple templates at once
func (r *templateRepository) CreateBatch(ctx context.Context, templates []*models.Template) error {
	if len(templates) == 0 {
		return nil
	}

	now := time.Now()
	docs := make([]interface{}, len(templates))
	for i, template := range templates {
		if template.UUID == "" {
			template.UUID = uuid.New().String()
		}
		template.CreatedAt = now
		template.UpdatedAt = now
		template.Version = 1
		docs[i] = template
	}

	_, err := r.collection.InsertMany(ctx, docs)
	return err
}

// GetByID retrieves a template by MongoDB ObjectID
func (r *templateRepository) GetByID(ctx context.Context, id string) (*models.Template, error) {
	var template models.Template
	err := r.collection.FindOne(ctx, bson.M{
		"_id":       id,
		"deletedAt": nil,
	}).Decode(&template)
	if err == mongo.ErrNoDocuments {
		return nil, ErrTemplateNotFound
	}
	return &template, err
}

// GetByUUID retrieves a template by UUID
func (r *templateRepository) GetByUUID(ctx context.Context, uuid string) (*models.Template, error) {
	var template models.Template
	err := r.collection.FindOne(ctx, bson.M{
		"uuid":      uuid,
		"deletedAt": nil,
	}).Decode(&template)
	if err == mongo.ErrNoDocuments {
		return nil, ErrTemplateNotFound
	}
	return &template, err
}

// GetByName retrieves a template by name
func (r *templateRepository) GetByName(ctx context.Context, name string) (*models.Template, error) {
	var template models.Template
	err := r.collection.FindOne(ctx, bson.M{
		"name":      name,
		"deletedAt": nil,
	}).Decode(&template)
	if err == mongo.ErrNoDocuments {
		return nil, ErrTemplateNotFound
	}
	return &template, err
}

// GetDefault retrieves the default template for a given type
func (r *templateRepository) GetDefault(ctx context.Context, templateType models.TemplateType) (*models.Template, error) {
	var template models.Template
	err := r.collection.FindOne(ctx, bson.M{
		"type":      templateType,
		"isDefault": true,
		"isActive":  true,
		"deletedAt": nil,
	}).Decode(&template)
	if err == mongo.ErrNoDocuments {
		return nil, ErrNoDefaultTemplate
	}
	return &template, err
}

// List retrieves templates with optional filters and pagination
func (r *templateRepository) List(ctx context.Context, filters *models.TemplateFilters, pagination models.PaginationParams) ([]models.Template, int64, error) {
	filter := bson.M{"deletedAt": nil}

	if filters != nil {
		if filters.Type != nil {
			filter["type"] = *filters.Type
		}
		if filters.IsDefault != nil {
			filter["isDefault"] = *filters.IsDefault
		}
		if filters.IsBuiltIn != nil {
			filter["isBuiltIn"] = *filters.IsBuiltIn
		}
		if filters.IsActive != nil {
			filter["isActive"] = *filters.IsActive
		}
		if filters.Search != nil && *filters.Search != "" {
			// Escape special regex characters to prevent ReDoS attacks
			escapedSearch := regexp.QuoteMeta(*filters.Search)
			filter["$or"] = []bson.M{
				{"name": bson.M{"$regex": escapedSearch, "$options": "i"}},
				{"description": bson.M{"$regex": escapedSearch, "$options": "i"}},
			}
		}
	}

	// Count total matching documents
	total, err := r.collection.CountDocuments(ctx, filter)
	if err != nil {
		return nil, 0, err
	}

	// Set up pagination
	skip := int64((pagination.Page - 1) * pagination.PageSize)
	limit := int64(pagination.PageSize)

	opts := options.Find().
		SetSkip(skip).
		SetLimit(limit).
		SetSort(bson.D{{Key: "createdAt", Value: -1}})

	cursor, err := r.collection.Find(ctx, filter, opts)
	if err != nil {
		return nil, 0, err
	}
	defer cursor.Close(ctx)

	var templates []models.Template
	if err := cursor.All(ctx, &templates); err != nil {
		return nil, 0, err
	}

	return templates, total, nil
}

// ListByType retrieves all templates of a specific type
func (r *templateRepository) ListByType(ctx context.Context, templateType models.TemplateType) ([]models.Template, error) {
	cursor, err := r.collection.Find(ctx, bson.M{
		"type":      templateType,
		"deletedAt": nil,
	})
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)

	var templates []models.Template
	if err := cursor.All(ctx, &templates); err != nil {
		return nil, err
	}
	return templates, nil
}

// ListActive retrieves all active templates
func (r *templateRepository) ListActive(ctx context.Context) ([]models.Template, error) {
	cursor, err := r.collection.Find(ctx, bson.M{
		"isActive":  true,
		"deletedAt": nil,
	})
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)

	var templates []models.Template
	if err := cursor.All(ctx, &templates); err != nil {
		return nil, err
	}
	return templates, nil
}

// Update updates an existing template
func (r *templateRepository) Update(ctx context.Context, template *models.Template) error {
	now := time.Now()
	template.UpdatedAt = now
	template.Version++

	update := bson.M{
		"$set": bson.M{
			"name":        template.Name,
			"description": template.Description,
			"htmlContent": template.HTMLContent,
			"cssContent":  template.CSSContent,
			"pageSize":    template.PageSize,
			"orientation": template.Orientation,
			"margins":     template.Margins,
			"headerHtml":  template.HeaderHTML,
			"footerHtml":  template.FooterHTML,
			"isActive":    template.IsActive,
			"updatedAt":   now,
			"updatedBy":   template.UpdatedBy,
			"version":     template.Version,
		},
	}

	result, err := r.collection.UpdateOne(ctx, bson.M{
		"uuid":      template.UUID,
		"deletedAt": nil,
	}, update)
	if err != nil {
		return err
	}
	if result.MatchedCount == 0 {
		return ErrTemplateNotFound
	}
	return nil
}

// SetDefault sets a template as the default for its type
func (r *templateRepository) SetDefault(ctx context.Context, uuid string, templateType models.TemplateType) error {
	now := time.Now()

	// Clear existing default for this type
	_, err := r.collection.UpdateMany(ctx, bson.M{
		"type":      templateType,
		"isDefault": true,
		"deletedAt": nil,
	}, bson.M{
		"$set": bson.M{
			"isDefault": false,
			"updatedAt": now,
		},
	})
	if err != nil {
		return err
	}

	// Set the new default
	result, err := r.collection.UpdateOne(ctx, bson.M{
		"uuid":      uuid,
		"type":      templateType,
		"deletedAt": nil,
	}, bson.M{
		"$set": bson.M{
			"isDefault": true,
			"updatedAt": now,
		},
	})
	if err != nil {
		return err
	}
	if result.MatchedCount == 0 {
		return ErrTemplateNotFound
	}
	return nil
}

// ClearDefault clears the default template for a type
func (r *templateRepository) ClearDefault(ctx context.Context, templateType models.TemplateType) error {
	now := time.Now()
	_, err := r.collection.UpdateMany(ctx, bson.M{
		"type":      templateType,
		"isDefault": true,
		"deletedAt": nil,
	}, bson.M{
		"$set": bson.M{
			"isDefault": false,
			"updatedAt": now,
		},
	})
	return err
}

// SoftDelete performs a soft delete on a template
func (r *templateRepository) SoftDelete(ctx context.Context, uuid string) error {
	// First check if it's a built-in template
	var template models.Template
	err := r.collection.FindOne(ctx, bson.M{
		"uuid":      uuid,
		"deletedAt": nil,
	}).Decode(&template)
	if err == mongo.ErrNoDocuments {
		return ErrTemplateNotFound
	}
	if err != nil {
		return err
	}
	if template.IsBuiltIn {
		return ErrCannotDeleteBuiltIn
	}

	now := time.Now()
	update := bson.M{
		"$set": bson.M{
			"deletedAt": now,
			"updatedAt": now,
			"isActive":  false,
			"isDefault": false,
		},
	}

	result, err := r.collection.UpdateOne(ctx, bson.M{
		"uuid":      uuid,
		"deletedAt": nil,
	}, update)
	if err != nil {
		return err
	}
	if result.MatchedCount == 0 {
		return ErrTemplateNotFound
	}
	return nil
}

// ExistsByName checks if a template with the given name exists
func (r *templateRepository) ExistsByName(ctx context.Context, name string) (bool, error) {
	count, err := r.collection.CountDocuments(ctx, bson.M{
		"name":      name,
		"deletedAt": nil,
	})
	return count > 0, err
}

// ExistsByUUID checks if a template with the given UUID exists
func (r *templateRepository) ExistsByUUID(ctx context.Context, uuid string) (bool, error) {
	count, err := r.collection.CountDocuments(ctx, bson.M{
		"uuid":      uuid,
		"deletedAt": nil,
	})
	return count > 0, err
}

// Count returns the total number of templates
func (r *templateRepository) Count(ctx context.Context) (int64, error) {
	return r.collection.CountDocuments(ctx, bson.M{"deletedAt": nil})
}

// CountByType returns the number of templates of a specific type
func (r *templateRepository) CountByType(ctx context.Context, templateType models.TemplateType) (int64, error) {
	return r.collection.CountDocuments(ctx, bson.M{
		"type":      templateType,
		"deletedAt": nil,
	})
}
