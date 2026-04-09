package repository

import (
	"context"
	"errors"
	"time"

	"github.com/orkestra/backend/internal/core/auth/models"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
)

var (
	ErrUserNotFound = errors.New("user not found")
	ErrInvalidToken = errors.New("invalid refresh token")
)

// AuthRepository handles authentication-specific operations only
// User CRUD operations have been moved to UserRepository
type AuthRepository interface {
	// Session Management - All authentication state operations
	AddRefreshToken(ctx context.Context, userID primitive.ObjectID, token models.RefreshToken) error
	AddRefreshTokenByUUID(ctx context.Context, userUUID string, token models.RefreshToken) error
	RemoveRefreshToken(ctx context.Context, userID primitive.ObjectID, token string) error
	RemoveRefreshTokenByUUID(ctx context.Context, userUUID string, token string) error
	RemoveRefreshTokenByDeviceID(ctx context.Context, userID primitive.ObjectID, deviceID string) error
	RemoveRefreshTokenByDeviceIDAndUUID(ctx context.Context, userUUID string, deviceID string) error
	RemoveAllRefreshTokens(ctx context.Context, userID primitive.ObjectID) error
	RemoveAllRefreshTokensByUUID(ctx context.Context, userUUID string) error
	ValidateRefreshToken(ctx context.Context, userID primitive.ObjectID, token string) (bool, error)
	ValidateRefreshTokenByUUID(ctx context.Context, userUUID string, token string) (bool, error)
	GetActiveSessions(ctx context.Context, userID primitive.ObjectID) ([]models.RefreshToken, error)
	GetActiveSessionsByUUID(ctx context.Context, userUUID string) ([]models.RefreshToken, error)
	CountActiveSessions(ctx context.Context, userID primitive.ObjectID) (int, error)
	CountActiveSessionsByUUID(ctx context.Context, userUUID string) (int, error)
	UpdateSessionActivity(ctx context.Context, userID primitive.ObjectID, token string) error
	UpdateSessionActivityByUUID(ctx context.Context, userUUID string, token string) error
	RenameSession(ctx context.Context, userID primitive.ObjectID, deviceID string, deviceName string) error
	RenameSessionByUUID(ctx context.Context, userUUID string, deviceID string, deviceName string) error
}

type mongoAuthRepository struct {
	db         *mongo.Database
	collection *mongo.Collection
}

func NewAuthRepository(db *mongo.Database) (AuthRepository, error) {
	collection := db.Collection("users")

	// NOTE: Index creation is now handled by user module and migrations
	// Auth repository no longer creates indexes on user collection
	// All user-related indexing is managed by user repository

	return &mongoAuthRepository{
		db:         db,
		collection: collection,
	}, nil
}

func (r *mongoAuthRepository) AddRefreshToken(ctx context.Context, userID primitive.ObjectID, token models.RefreshToken) error {
	filter := bson.M{
		"_id":       userID,
		"deletedAt": nil,
	}

	update := bson.M{
		"$push": bson.M{
			"refreshTokens": bson.M{
				"$each":  []models.RefreshToken{token},
				"$slice": -10,
			},
		},
		"$set": bson.M{
			"updatedAt": time.Now(),
		},
	}

	result, err := r.collection.UpdateOne(ctx, filter, update)
	if err != nil {
		return err
	}

	if result.MatchedCount == 0 {
		return ErrUserNotFound
	}

	return nil
}

func (r *mongoAuthRepository) RemoveRefreshToken(ctx context.Context, userID primitive.ObjectID, token string) error {
	filter := bson.M{
		"_id":       userID,
		"deletedAt": nil,
	}

	update := bson.M{
		"$pull": bson.M{
			"refreshTokens": bson.M{
				"token": token,
			},
		},
		"$set": bson.M{
			"updatedAt": time.Now(),
		},
	}

	result, err := r.collection.UpdateOne(ctx, filter, update)
	if err != nil {
		return err
	}

	if result.MatchedCount == 0 {
		return ErrUserNotFound
	}

	return nil
}

func (r *mongoAuthRepository) RemoveAllRefreshTokens(ctx context.Context, userID primitive.ObjectID) error {
	filter := bson.M{
		"_id":       userID,
		"deletedAt": nil,
	}

	update := bson.M{
		"$set": bson.M{
			"refreshTokens": []models.RefreshToken{},
			"updatedAt":     time.Now(),
		},
	}

	result, err := r.collection.UpdateOne(ctx, filter, update)
	if err != nil {
		return err
	}

	if result.MatchedCount == 0 {
		return ErrUserNotFound
	}

	return nil
}

func (r *mongoAuthRepository) ValidateRefreshToken(ctx context.Context, userID primitive.ObjectID, token string) (bool, error) {
	filter := bson.M{
		"_id":                 userID,
		"refreshTokens.token": token,
		"refreshTokens.expiresAt": bson.M{
			"$gt": time.Now(),
		},
		"deletedAt": nil,
	}

	count, err := r.collection.CountDocuments(ctx, filter)
	if err != nil {
		return false, err
	}

	return count > 0, nil
}

// RemoveRefreshTokenByDeviceID removes a refresh token by device ID
func (r *mongoAuthRepository) RemoveRefreshTokenByDeviceID(ctx context.Context, userID primitive.ObjectID, deviceID string) error {
	filter := bson.M{
		"_id":       userID,
		"deletedAt": nil,
	}

	update := bson.M{
		"$pull": bson.M{
			"refreshTokens": bson.M{
				"deviceId": deviceID,
			},
		},
		"$set": bson.M{
			"updatedAt": time.Now(),
		},
	}

	result, err := r.collection.UpdateOne(ctx, filter, update)
	if err != nil {
		return err
	}

	if result.MatchedCount == 0 {
		return ErrUserNotFound
	}

	return nil
}

// GetActiveSessions retrieves all active refresh tokens for a user
func (r *mongoAuthRepository) GetActiveSessions(ctx context.Context, userID primitive.ObjectID) ([]models.RefreshToken, error) {
	pipeline := []bson.M{
		{
			"$match": bson.M{
				"_id":       userID,
				"deletedAt": nil,
			},
		},
		{
			"$project": bson.M{
				"refreshTokens": bson.M{
					"$filter": bson.M{
						"input": "$refreshTokens",
						"cond": bson.M{
							"$gt": []interface{}{"$$this.expiresAt", time.Now()},
						},
					},
				},
			},
		},
	}

	cursor, err := r.collection.Aggregate(ctx, pipeline)
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)

	var result struct {
		RefreshTokens []models.RefreshToken `bson:"refreshTokens"`
	}

	if cursor.Next(ctx) {
		if err := cursor.Decode(&result); err != nil {
			return nil, err
		}
	}

	return result.RefreshTokens, nil
}

// CountActiveSessions counts the number of active sessions for a user
func (r *mongoAuthRepository) CountActiveSessions(ctx context.Context, userID primitive.ObjectID) (int, error) {
	sessions, err := r.GetActiveSessions(ctx, userID)
	if err != nil {
		return 0, err
	}

	return len(sessions), nil
}

// UpdateSessionActivity updates the last activity timestamp for a session
func (r *mongoAuthRepository) UpdateSessionActivity(ctx context.Context, userID primitive.ObjectID, token string) error {
	filter := bson.M{
		"_id":                 userID,
		"refreshTokens.token": token,
		"deletedAt":           nil,
	}

	update := bson.M{
		"$set": bson.M{
			"refreshTokens.$.lastActivity": time.Now(),
			"updatedAt":                    time.Now(),
		},
	}

	result, err := r.collection.UpdateOne(ctx, filter, update)
	if err != nil {
		return err
	}

	if result.MatchedCount == 0 {
		return ErrInvalidToken
	}

	return nil
}

// RenameSession updates the device name for a session
func (r *mongoAuthRepository) RenameSession(ctx context.Context, userID primitive.ObjectID, deviceID string, deviceName string) error {
	filter := bson.M{
		"_id":                    userID,
		"refreshTokens.deviceId": deviceID,
		"deletedAt":              nil,
	}

	update := bson.M{
		"$set": bson.M{
			"refreshTokens.$.deviceName": deviceName,
			"updatedAt":                  time.Now(),
		},
	}

	result, err := r.collection.UpdateOne(ctx, filter, update)
	if err != nil {
		return err
	}

	if result.MatchedCount == 0 {
		return ErrInvalidToken
	}

	return nil
}

// UUID-based methods

func (r *mongoAuthRepository) AddRefreshTokenByUUID(ctx context.Context, userUUID string, token models.RefreshToken) error {
	filter := bson.M{
		"uuid":      userUUID,
		"deletedAt": nil,
	}

	update := bson.M{
		"$push": bson.M{
			"refreshTokens": bson.M{
				"$each":  []models.RefreshToken{token},
				"$slice": -10,
			},
		},
		"$set": bson.M{
			"updatedAt": time.Now(),
		},
	}

	result, err := r.collection.UpdateOne(ctx, filter, update)
	if err != nil {
		return err
	}

	if result.MatchedCount == 0 {
		return ErrUserNotFound
	}

	return nil
}

func (r *mongoAuthRepository) RemoveRefreshTokenByUUID(ctx context.Context, userUUID string, token string) error {
	filter := bson.M{
		"uuid":      userUUID,
		"deletedAt": nil,
	}

	update := bson.M{
		"$pull": bson.M{
			"refreshTokens": bson.M{
				"token": token,
			},
		},
		"$set": bson.M{
			"updatedAt": time.Now(),
		},
	}

	result, err := r.collection.UpdateOne(ctx, filter, update)
	if err != nil {
		return err
	}

	if result.MatchedCount == 0 {
		return ErrUserNotFound
	}

	return nil
}

func (r *mongoAuthRepository) RemoveRefreshTokenByDeviceIDAndUUID(ctx context.Context, userUUID string, deviceID string) error {
	filter := bson.M{
		"uuid":      userUUID,
		"deletedAt": nil,
	}

	update := bson.M{
		"$pull": bson.M{
			"refreshTokens": bson.M{
				"deviceId": deviceID,
			},
		},
		"$set": bson.M{
			"updatedAt": time.Now(),
		},
	}

	result, err := r.collection.UpdateOne(ctx, filter, update)
	if err != nil {
		return err
	}

	if result.MatchedCount == 0 {
		return ErrUserNotFound
	}

	return nil
}

func (r *mongoAuthRepository) RemoveAllRefreshTokensByUUID(ctx context.Context, userUUID string) error {
	filter := bson.M{
		"uuid":      userUUID,
		"deletedAt": nil,
	}

	update := bson.M{
		"$set": bson.M{
			"refreshTokens": []models.RefreshToken{},
			"updatedAt":     time.Now(),
		},
	}

	result, err := r.collection.UpdateOne(ctx, filter, update)
	if err != nil {
		return err
	}

	if result.MatchedCount == 0 {
		return ErrUserNotFound
	}

	return nil
}

func (r *mongoAuthRepository) ValidateRefreshTokenByUUID(ctx context.Context, userUUID string, token string) (bool, error) {
	filter := bson.M{
		"uuid":                userUUID,
		"refreshTokens.token": token,
		"refreshTokens.expiresAt": bson.M{
			"$gt": time.Now(),
		},
		"deletedAt": nil,
	}

	count, err := r.collection.CountDocuments(ctx, filter)
	if err != nil {
		return false, err
	}

	return count > 0, nil
}

func (r *mongoAuthRepository) GetActiveSessionsByUUID(ctx context.Context, userUUID string) ([]models.RefreshToken, error) {
	pipeline := []bson.M{
		{
			"$match": bson.M{
				"uuid":      userUUID,
				"deletedAt": nil,
			},
		},
		{
			"$project": bson.M{
				"refreshTokens": bson.M{
					"$filter": bson.M{
						"input": "$refreshTokens",
						"cond": bson.M{
							"$gt": []interface{}{"$$this.expiresAt", time.Now()},
						},
					},
				},
			},
		},
	}

	cursor, err := r.collection.Aggregate(ctx, pipeline)
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)

	var result struct {
		RefreshTokens []models.RefreshToken `bson:"refreshTokens"`
	}

	if cursor.Next(ctx) {
		if err := cursor.Decode(&result); err != nil {
			return nil, err
		}
	}

	return result.RefreshTokens, nil
}

func (r *mongoAuthRepository) CountActiveSessionsByUUID(ctx context.Context, userUUID string) (int, error) {
	sessions, err := r.GetActiveSessionsByUUID(ctx, userUUID)
	if err != nil {
		return 0, err
	}

	return len(sessions), nil
}

func (r *mongoAuthRepository) UpdateSessionActivityByUUID(ctx context.Context, userUUID string, token string) error {
	filter := bson.M{
		"uuid":                userUUID,
		"refreshTokens.token": token,
		"deletedAt":           nil,
	}

	update := bson.M{
		"$set": bson.M{
			"refreshTokens.$.lastActivity": time.Now(),
			"updatedAt":                    time.Now(),
		},
	}

	result, err := r.collection.UpdateOne(ctx, filter, update)
	if err != nil {
		return err
	}

	if result.MatchedCount == 0 {
		return ErrInvalidToken
	}

	return nil
}

func (r *mongoAuthRepository) RenameSessionByUUID(ctx context.Context, userUUID string, deviceID string, deviceName string) error {
	filter := bson.M{
		"uuid":                   userUUID,
		"refreshTokens.deviceId": deviceID,
		"deletedAt":              nil,
	}

	update := bson.M{
		"$set": bson.M{
			"refreshTokens.$.deviceName": deviceName,
			"updatedAt":                  time.Now(),
		},
	}

	result, err := r.collection.UpdateOne(ctx, filter, update)
	if err != nil {
		return err
	}

	if result.MatchedCount == 0 {
		return ErrInvalidToken
	}

	return nil
}
