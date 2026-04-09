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

const conversationCollection = "agent_conversations"

// ConversationRepository defines CRUD operations for agent conversations
type ConversationRepository interface {
	Create(ctx context.Context, conv *models.Conversation) error
	GetByUUID(ctx context.Context, uuid string) (*models.Conversation, error)
	ListByProject(ctx context.Context, projectUUID, userUUID string, limit, offset int) ([]models.Conversation, int64, error)
	AppendMessage(ctx context.Context, uuid string, msg models.Message) error
	Delete(ctx context.Context, uuid string) error
}

type conversationRepository struct {
	collection *mongo.Collection
}

// NewConversationRepository creates a new ConversationRepository with indexes
func NewConversationRepository(db *mongo.Database) ConversationRepository {
	coll := db.Collection(conversationCollection)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	indexes := []mongo.IndexModel{
		{Keys: bson.D{{Key: "uuid", Value: 1}}, Options: options.Index().SetUnique(true)},
		{Keys: bson.D{{Key: "projectUuid", Value: 1}, {Key: "createdAt", Value: -1}}},
		{Keys: bson.D{{Key: "userUuid", Value: 1}}},
	}
	coll.Indexes().CreateMany(ctx, indexes) //nolint:errcheck

	return &conversationRepository{collection: coll}
}

func (r *conversationRepository) Create(ctx context.Context, conv *models.Conversation) error {
	if conv.UUID == "" {
		conv.UUID = uuid.New().String()
	}
	now := time.Now()
	conv.CreatedAt = now
	conv.UpdatedAt = now
	if conv.Messages == nil {
		conv.Messages = []models.Message{}
	}

	_, err := r.collection.InsertOne(ctx, conv)
	if err != nil {
		return fmt.Errorf("insert conversation: %w", err)
	}
	return nil
}

func (r *conversationRepository) GetByUUID(ctx context.Context, uuid string) (*models.Conversation, error) {
	var conv models.Conversation
	err := r.collection.FindOne(ctx, bson.M{"uuid": uuid}).Decode(&conv)
	if err != nil {
		if err == mongo.ErrNoDocuments {
			return nil, fmt.Errorf("conversation not found: %s", uuid)
		}
		return nil, fmt.Errorf("get conversation: %w", err)
	}
	return &conv, nil
}

func (r *conversationRepository) ListByProject(ctx context.Context, projectUUID, userUUID string, limit, offset int) ([]models.Conversation, int64, error) {
	filter := bson.M{"projectUuid": projectUUID}
	if userUUID != "" {
		filter["userUuid"] = userUUID
	}

	total, err := r.collection.CountDocuments(ctx, filter)
	if err != nil {
		return nil, 0, fmt.Errorf("count conversations: %w", err)
	}

	if limit <= 0 {
		limit = 20
	}

	// Project only metadata, not full messages, for the list view
	projection := bson.M{
		"uuid":        1,
		"projectUuid": 1,
		"userUuid":    1,
		"persona":     1,
		"title":       1,
		"createdAt":   1,
		"updatedAt":   1,
	}

	opts := options.Find().
		SetSort(bson.D{{Key: "updatedAt", Value: -1}}).
		SetLimit(int64(limit)).
		SetSkip(int64(offset)).
		SetProjection(projection)

	cursor, err := r.collection.Find(ctx, filter, opts)
	if err != nil {
		return nil, 0, fmt.Errorf("list conversations: %w", err)
	}
	defer cursor.Close(ctx)

	var convs []models.Conversation
	if err := cursor.All(ctx, &convs); err != nil {
		return nil, 0, fmt.Errorf("decode conversations: %w", err)
	}
	if convs == nil {
		convs = []models.Conversation{}
	}
	return convs, total, nil
}

func (r *conversationRepository) AppendMessage(ctx context.Context, uuid string, msg models.Message) error {
	msg.CreatedAt = time.Now()

	update := bson.M{
		"$push": bson.M{"messages": msg},
		"$set":  bson.M{"updatedAt": time.Now()},
	}

	result, err := r.collection.UpdateOne(ctx, bson.M{"uuid": uuid}, update)
	if err != nil {
		return fmt.Errorf("append message: %w", err)
	}
	if result.MatchedCount == 0 {
		return fmt.Errorf("conversation not found: %s", uuid)
	}
	return nil
}

func (r *conversationRepository) Delete(ctx context.Context, uuid string) error {
	result, err := r.collection.DeleteOne(ctx, bson.M{"uuid": uuid})
	if err != nil {
		return fmt.Errorf("delete conversation: %w", err)
	}
	if result.DeletedCount == 0 {
		return fmt.Errorf("conversation not found: %s", uuid)
	}
	return nil
}
