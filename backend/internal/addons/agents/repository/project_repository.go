package repository

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"

	"github.com/orkestra/backend/internal/addons/agents/models"
)

const projectCollection = "agent_projects"

// ProjectRepository defines CRUD operations for agent projects
type ProjectRepository interface {
	Create(ctx context.Context, project *models.Project) error
	GetByUUID(ctx context.Context, uuid string) (*models.Project, error)
	GetPersonalByUserUUID(ctx context.Context, userUUID string) (*models.Project, error)
	List(ctx context.Context, status string) ([]models.Project, error)
	Update(ctx context.Context, uuid string, update bson.M) (*models.Project, error)
	Delete(ctx context.Context, uuid string) error
}

type projectRepository struct {
	collection *mongo.Collection
}

// NewProjectRepository creates a new ProjectRepository with indexes
func NewProjectRepository(db *mongo.Database) ProjectRepository {
	coll := db.Collection(projectCollection)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	indexes := []mongo.IndexModel{
		{Keys: bson.D{{Key: "uuid", Value: 1}}, Options: options.Index().SetUnique(true)},
		{Keys: bson.D{{Key: "status", Value: 1}}},
		{Keys: bson.D{{Key: "createdBy", Value: 1}}},
		{
			Keys: bson.D{{Key: "personalUserUuid", Value: 1}},
			Options: options.Index().
				SetSparse(true).
				SetUnique(true),
		},
	}
	coll.Indexes().CreateMany(ctx, indexes) //nolint:errcheck // best-effort index ensure; subsequent reads will surface persistent failures

	return &projectRepository{collection: coll}
}

func (r *projectRepository) Create(ctx context.Context, project *models.Project) error {
	if project.UUID == "" {
		project.UUID = uuid.New().String()
	}
	now := time.Now()
	project.CreatedAt = now
	project.UpdatedAt = now
	if project.Status == "" {
		project.Status = models.ProjectStatusActive
	}
	if project.DocumentUUIDs == nil {
		project.DocumentUUIDs = []string{}
	}

	_, err := r.collection.InsertOne(ctx, project)
	if err != nil {
		return fmt.Errorf("insert project: %w", err)
	}
	return nil
}

func (r *projectRepository) GetByUUID(ctx context.Context, uuid string) (*models.Project, error) {
	var project models.Project
	err := r.collection.FindOne(ctx, bson.M{"uuid": uuid}).Decode(&project)
	if err != nil {
		if err == mongo.ErrNoDocuments {
			return nil, fmt.Errorf("project not found: %s", uuid)
		}
		return nil, fmt.Errorf("get project: %w", err)
	}
	return &project, nil
}

func (r *projectRepository) GetPersonalByUserUUID(ctx context.Context, userUUID string) (*models.Project, error) {
	var project models.Project
	err := r.collection.FindOne(ctx, bson.M{
		"isPersonal":       true,
		"personalUserUuid": userUUID,
	}).Decode(&project)
	if err != nil {
		if err == mongo.ErrNoDocuments {
			return nil, nil // not found — caller should auto-create
		}
		return nil, fmt.Errorf("get personal project: %w", err)
	}
	return &project, nil
}

func (r *projectRepository) List(ctx context.Context, status string) ([]models.Project, error) {
	filter := bson.M{
		"isPersonal": bson.M{"$ne": true}, // exclude personal projects
	}
	if status != "" {
		filter["status"] = status
	}

	opts := options.Find().SetSort(bson.D{{Key: "createdAt", Value: -1}})
	cursor, err := r.collection.Find(ctx, filter, opts)
	if err != nil {
		return nil, fmt.Errorf("list projects: %w", err)
	}
	defer cursor.Close(ctx)

	var projects []models.Project
	if err := cursor.All(ctx, &projects); err != nil {
		return nil, fmt.Errorf("decode projects: %w", err)
	}
	if projects == nil {
		projects = []models.Project{}
	}
	return projects, nil
}

func (r *projectRepository) Update(ctx context.Context, uuid string, update bson.M) (*models.Project, error) {
	update["updatedAt"] = time.Now()

	opts := options.FindOneAndUpdate().SetReturnDocument(options.After)
	var project models.Project
	err := r.collection.FindOneAndUpdate(
		ctx,
		bson.M{"uuid": uuid},
		bson.M{"$set": update},
		opts,
	).Decode(&project)
	if err != nil {
		if err == mongo.ErrNoDocuments {
			return nil, fmt.Errorf("project not found: %s", uuid)
		}
		return nil, fmt.Errorf("update project: %w", err)
	}
	return &project, nil
}

func (r *projectRepository) Delete(ctx context.Context, uuid string) error {
	result, err := r.collection.DeleteOne(ctx, bson.M{"uuid": uuid})
	if err != nil {
		return fmt.Errorf("delete project: %w", err)
	}
	if result.DeletedCount == 0 {
		return fmt.Errorf("project not found: %s", uuid)
	}
	return nil
}
