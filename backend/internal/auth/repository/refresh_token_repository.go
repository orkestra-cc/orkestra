package repository

import (
	"context"
	"fmt"
	"time"

	"github.com/orkestra/backend/internal/auth/models"
	"github.com/orkestra/backend/internal/shared/utils"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// RefreshTokenRepository handles refresh token data operations
type RefreshTokenRepository interface {
	// Token management
	CreateRefreshToken(ctx context.Context, token *models.RefreshTokenDoc) error
	GetByToken(ctx context.Context, tokenHash string) (*models.RefreshTokenDoc, error)
	GetBySessionUUID(ctx context.Context, sessionUUID string) (*models.RefreshTokenDoc, error)
	GetActiveTokensByUser(ctx context.Context, userUUID string) ([]*models.RefreshTokenDoc, error)
	GetActiveTokensByDevice(ctx context.Context, userUUID, deviceID string) ([]*models.RefreshTokenDoc, error)

	// Token updates
	UpdateLastActivity(ctx context.Context, uuid string) error
	UpdateRiskScore(ctx context.Context, uuid string, riskScore float64, riskFactors []string) error
	RotateToken(ctx context.Context, oldTokenHash, newTokenHash string) error

	// Token revocation
	RevokeToken(ctx context.Context, tokenHash string, reason string) error
	RevokeTokenByUUID(ctx context.Context, uuid string, reason string) error
	RevokeTokensBySession(ctx context.Context, sessionUUID string, reason string) error
	RevokeTokensByUser(ctx context.Context, userUUID string, reason string) error
	RevokeTokensByDevice(ctx context.Context, userUUID, deviceID string, reason string) error

	// Cleanup operations
	CleanupExpiredTokens(ctx context.Context) (int64, error)
	CleanupRevokedTokens(ctx context.Context, olderThan time.Duration) (int64, error)

	// Analytics and monitoring
	GetTokenStats(ctx context.Context, userUUID string) (*TokenStats, error)
	GetDeviceTokenCount(ctx context.Context, userUUID, deviceID string) (int64, error)
}

type TokenStats struct {
	TotalTokens    int64 `json:"totalTokens"`
	ActiveTokens   int64 `json:"activeTokens"`
	RevokedTokens  int64 `json:"revokedTokens"`
	ExpiredTokens  int64 `json:"expiredTokens"`
	UniqueDevices  int64 `json:"uniqueDevices"`
	HighRiskTokens int64 `json:"highRiskTokens"`
}

type refreshTokenRepository struct {
	collection *mongo.Collection
}

// NewRefreshTokenRepository creates a new refresh token repository
func NewRefreshTokenRepository(db *mongo.Database) RefreshTokenRepository {
	return &refreshTokenRepository{
		collection: db.Collection(models.RefreshTokensCollection),
	}
}

func (r *refreshTokenRepository) CreateRefreshToken(ctx context.Context, token *models.RefreshTokenDoc) error {
	// Set timestamps and UUID if not provided
	now := time.Now()
	if token.UUID == "" {
		token.UUID = models.GenerateTimeOrderedUUID()
	}
	token.IssuedAt = now
	token.CreatedAt = now
	token.UpdatedAt = now
	token.LastActivity = now

	// Hash the token for storage security
	if token.Token != "" {
		token.Token = utils.HashRefreshToken(token.Token)
	}

	// Validate required fields
	if token.UserUUID == "" {
		return fmt.Errorf("user UUID is required")
	}
	if token.SessionUUID == "" {
		return fmt.Errorf("session UUID is required")
	}
	if token.DeviceID == "" {
		return fmt.Errorf("device ID is required")
	}

	_, err := r.collection.InsertOne(ctx, token)
	if err != nil {
		return fmt.Errorf("failed to create refresh token: %w", err)
	}

	return nil
}

func (r *refreshTokenRepository) GetByToken(ctx context.Context, tokenHash string) (*models.RefreshTokenDoc, error) {
	filter := bson.M{
		"token":     tokenHash,
		"isRevoked": false,
		"expiresAt": bson.M{"$gt": time.Now()},
	}

	var result models.RefreshTokenDoc
	err := r.collection.FindOne(ctx, filter).Decode(&result)
	if err != nil {
		if err == mongo.ErrNoDocuments {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to find refresh token: %w", err)
	}

	return &result, nil
}

func (r *refreshTokenRepository) GetBySessionUUID(ctx context.Context, sessionUUID string) (*models.RefreshTokenDoc, error) {
	filter := bson.M{
		"sessionUuid": sessionUUID,
		"isRevoked":   false,
		"expiresAt":   bson.M{"$gt": time.Now()},
	}

	var result models.RefreshTokenDoc
	err := r.collection.FindOne(ctx, filter).Decode(&result)
	if err != nil {
		if err == mongo.ErrNoDocuments {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to find refresh token by session: %w", err)
	}

	return &result, nil
}

func (r *refreshTokenRepository) GetActiveTokensByUser(ctx context.Context, userUUID string) ([]*models.RefreshTokenDoc, error) {
	filter := bson.M{
		"userUuid":  userUUID,
		"isRevoked": false,
		"expiresAt": bson.M{"$gt": time.Now()},
	}

	// Sort by last activity descending
	opts := options.Find().SetSort(bson.D{{Key: "lastActivity", Value: -1}})

	cursor, err := r.collection.Find(ctx, filter, opts)
	if err != nil {
		return nil, fmt.Errorf("failed to find active tokens: %w", err)
	}
	defer cursor.Close(ctx)

	var tokens []*models.RefreshTokenDoc
	for cursor.Next(ctx) {
		var token models.RefreshTokenDoc
		if err := cursor.Decode(&token); err != nil {
			return nil, fmt.Errorf("failed to decode refresh token: %w", err)
		}
		tokens = append(tokens, &token)
	}

	return tokens, nil
}

func (r *refreshTokenRepository) GetActiveTokensByDevice(ctx context.Context, userUUID, deviceID string) ([]*models.RefreshTokenDoc, error) {
	filter := bson.M{
		"userUuid":  userUUID,
		"deviceId":  deviceID,
		"isRevoked": false,
		"expiresAt": bson.M{"$gt": time.Now()},
	}

	cursor, err := r.collection.Find(ctx, filter)
	if err != nil {
		return nil, fmt.Errorf("failed to find device tokens: %w", err)
	}
	defer cursor.Close(ctx)

	var tokens []*models.RefreshTokenDoc
	for cursor.Next(ctx) {
		var token models.RefreshTokenDoc
		if err := cursor.Decode(&token); err != nil {
			return nil, fmt.Errorf("failed to decode refresh token: %w", err)
		}
		tokens = append(tokens, &token)
	}

	return tokens, nil
}

func (r *refreshTokenRepository) UpdateLastActivity(ctx context.Context, uuid string) error {
	filter := bson.M{"uuid": uuid}
	update := bson.M{
		"$set": bson.M{
			"lastActivity": time.Now(),
			"updatedAt":    time.Now(),
		},
	}

	result, err := r.collection.UpdateOne(ctx, filter, update)
	if err != nil {
		return fmt.Errorf("failed to update last activity: %w", err)
	}

	if result.MatchedCount == 0 {
		return fmt.Errorf("refresh token not found")
	}

	return nil
}

func (r *refreshTokenRepository) UpdateRiskScore(ctx context.Context, uuid string, riskScore float64, riskFactors []string) error {
	filter := bson.M{"uuid": uuid}
	update := bson.M{
		"$set": bson.M{
			"riskScore":   riskScore,
			"riskFactors": riskFactors,
			"updatedAt":   time.Now(),
		},
	}

	result, err := r.collection.UpdateOne(ctx, filter, update)
	if err != nil {
		return fmt.Errorf("failed to update risk score: %w", err)
	}

	if result.MatchedCount == 0 {
		return fmt.Errorf("refresh token not found")
	}

	return nil
}

func (r *refreshTokenRepository) RotateToken(ctx context.Context, oldTokenHash, newTokenHash string) error {
	// Start a transaction to ensure atomicity
	session, err := r.collection.Database().Client().StartSession()
	if err != nil {
		return fmt.Errorf("failed to start session: %w", err)
	}
	defer session.EndSession(ctx)

	_, err = session.WithTransaction(ctx, func(sc mongo.SessionContext) (interface{}, error) {
		// Find the old token
		filter := bson.M{"token": oldTokenHash}
		var oldToken models.RefreshTokenDoc
		err := r.collection.FindOne(sc, filter).Decode(&oldToken)
		if err != nil {
			return nil, fmt.Errorf("old token not found: %w", err)
		}

		// Create new token with same metadata but new hash
		newToken := oldToken
		newToken.UUID = models.GenerateTimeOrderedUUID()
		newToken.Token = newTokenHash
		newToken.IssuedAt = time.Now()
		newToken.UpdatedAt = time.Now()

		// Insert new token
		_, err = r.collection.InsertOne(sc, &newToken)
		if err != nil {
			return nil, fmt.Errorf("failed to create new token: %w", err)
		}

		// Revoke old token
		revokeUpdate := bson.M{
			"$set": bson.M{
				"isRevoked":     true,
				"revokedAt":     time.Now(),
				"revokedReason": "token_rotation",
				"updatedAt":     time.Now(),
			},
		}

		_, err = r.collection.UpdateOne(sc, filter, revokeUpdate)
		if err != nil {
			return nil, fmt.Errorf("failed to revoke old token: %w", err)
		}

		return nil, nil
	})

	return err
}

func (r *refreshTokenRepository) RevokeToken(ctx context.Context, tokenHash string, reason string) error {
	filter := bson.M{"token": tokenHash}
	update := bson.M{
		"$set": bson.M{
			"isRevoked":     true,
			"revokedAt":     time.Now(),
			"revokedReason": reason,
			"updatedAt":     time.Now(),
		},
	}

	result, err := r.collection.UpdateOne(ctx, filter, update)
	if err != nil {
		return fmt.Errorf("failed to revoke token: %w", err)
	}

	if result.MatchedCount == 0 {
		return fmt.Errorf("refresh token not found")
	}

	return nil
}

func (r *refreshTokenRepository) RevokeTokenByUUID(ctx context.Context, uuid string, reason string) error {
	filter := bson.M{"uuid": uuid}
	update := bson.M{
		"$set": bson.M{
			"isRevoked":     true,
			"revokedAt":     time.Now(),
			"revokedReason": reason,
			"updatedAt":     time.Now(),
		},
	}

	result, err := r.collection.UpdateOne(ctx, filter, update)
	if err != nil {
		return fmt.Errorf("failed to revoke token: %w", err)
	}

	if result.MatchedCount == 0 {
		return fmt.Errorf("refresh token not found")
	}

	return nil
}

func (r *refreshTokenRepository) RevokeTokensBySession(ctx context.Context, sessionUUID string, reason string) error {
	filter := bson.M{
		"sessionUuid": sessionUUID,
		"isRevoked":   false,
	}
	update := bson.M{
		"$set": bson.M{
			"isRevoked":     true,
			"revokedAt":     time.Now(),
			"revokedReason": reason,
			"updatedAt":     time.Now(),
		},
	}

	_, err := r.collection.UpdateMany(ctx, filter, update)
	if err != nil {
		return fmt.Errorf("failed to revoke session tokens: %w", err)
	}

	return nil
}

func (r *refreshTokenRepository) RevokeTokensByUser(ctx context.Context, userUUID string, reason string) error {
	filter := bson.M{
		"userUuid":  userUUID,
		"isRevoked": false,
	}
	update := bson.M{
		"$set": bson.M{
			"isRevoked":     true,
			"revokedAt":     time.Now(),
			"revokedReason": reason,
			"updatedAt":     time.Now(),
		},
	}

	_, err := r.collection.UpdateMany(ctx, filter, update)
	if err != nil {
		return fmt.Errorf("failed to revoke user tokens: %w", err)
	}

	return nil
}

func (r *refreshTokenRepository) RevokeTokensByDevice(ctx context.Context, userUUID, deviceID string, reason string) error {
	filter := bson.M{
		"userUuid":  userUUID,
		"deviceId":  deviceID,
		"isRevoked": false,
	}
	update := bson.M{
		"$set": bson.M{
			"isRevoked":     true,
			"revokedAt":     time.Now(),
			"revokedReason": reason,
			"updatedAt":     time.Now(),
		},
	}

	_, err := r.collection.UpdateMany(ctx, filter, update)
	if err != nil {
		return fmt.Errorf("failed to revoke device tokens: %w", err)
	}

	return nil
}

func (r *refreshTokenRepository) CleanupExpiredTokens(ctx context.Context) (int64, error) {
	filter := bson.M{
		"expiresAt": bson.M{"$lt": time.Now()},
	}

	result, err := r.collection.DeleteMany(ctx, filter)
	if err != nil {
		return 0, fmt.Errorf("failed to cleanup expired tokens: %w", err)
	}

	return result.DeletedCount, nil
}

func (r *refreshTokenRepository) CleanupRevokedTokens(ctx context.Context, olderThan time.Duration) (int64, error) {
	cutoff := time.Now().Add(-olderThan)
	filter := bson.M{
		"isRevoked": true,
		"revokedAt": bson.M{"$lt": cutoff},
	}

	result, err := r.collection.DeleteMany(ctx, filter)
	if err != nil {
		return 0, fmt.Errorf("failed to cleanup revoked tokens: %w", err)
	}

	return result.DeletedCount, nil
}

func (r *refreshTokenRepository) GetTokenStats(ctx context.Context, userUUID string) (*TokenStats, error) {
	pipeline := []bson.M{
		{
			"$match": bson.M{"userUuid": userUUID},
		},
		{
			"$group": bson.M{
				"_id":         nil,
				"totalTokens": bson.M{"$sum": 1},
				"activeTokens": bson.M{
					"$sum": bson.M{
						"$cond": bson.M{
							"if": bson.M{
								"$and": []bson.M{
									{"$eq": []interface{}{"$isRevoked", false}},
									{"$gt": []interface{}{"$expiresAt", time.Now()}},
								},
							},
							"then": 1,
							"else": 0,
						},
					},
				},
				"revokedTokens": bson.M{
					"$sum": bson.M{
						"$cond": bson.M{
							"if":   bson.M{"$eq": []interface{}{"$isRevoked", true}},
							"then": 1,
							"else": 0,
						},
					},
				},
				"expiredTokens": bson.M{
					"$sum": bson.M{
						"$cond": bson.M{
							"if":   bson.M{"$lt": []interface{}{"$expiresAt", time.Now()}},
							"then": 1,
							"else": 0,
						},
					},
				},
				"uniqueDevices": bson.M{"$addToSet": "$deviceId"},
				"highRiskTokens": bson.M{
					"$sum": bson.M{
						"$cond": bson.M{
							"if":   bson.M{"$gt": []interface{}{"$riskScore", 0.7}},
							"then": 1,
							"else": 0,
						},
					},
				},
			},
		},
		{
			"$project": bson.M{
				"totalTokens":    1,
				"activeTokens":   1,
				"revokedTokens":  1,
				"expiredTokens":  1,
				"uniqueDevices":  bson.M{"$size": "$uniqueDevices"},
				"highRiskTokens": 1,
			},
		},
	}

	cursor, err := r.collection.Aggregate(ctx, pipeline)
	if err != nil {
		return nil, fmt.Errorf("failed to get token stats: %w", err)
	}
	defer cursor.Close(ctx)

	var stats TokenStats
	if cursor.Next(ctx) {
		if err := cursor.Decode(&stats); err != nil {
			return nil, fmt.Errorf("failed to decode token stats: %w", err)
		}
	}

	return &stats, nil
}

func (r *refreshTokenRepository) GetDeviceTokenCount(ctx context.Context, userUUID, deviceID string) (int64, error) {
	filter := bson.M{
		"userUuid":  userUUID,
		"deviceId":  deviceID,
		"isRevoked": false,
		"expiresAt": bson.M{"$gt": time.Now()},
	}

	count, err := r.collection.CountDocuments(ctx, filter)
	if err != nil {
		return 0, fmt.Errorf("failed to count device tokens: %w", err)
	}

	return count, nil
}
