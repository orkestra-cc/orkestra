package repository

import (
	"context"
	"fmt"
	"time"

	"github.com/orkestra/backend/internal/core/auth/models"
	"github.com/orkestra/backend/internal/shared/utils"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// ErrTokenAlreadyRotated is returned by RotateWithFamily when the CAS
// precondition ({isRevoked:false}) does not match — i.e. the old token has
// already been rotated, revoked, or doesn't exist. The refresh flow
// interprets this as a replay signal.
var ErrTokenAlreadyRotated = fmt.Errorf("refresh token already rotated or revoked")

// RefreshTokenRepository handles refresh token data operations
type RefreshTokenRepository interface {
	// Token management
	CreateRefreshToken(ctx context.Context, token *models.RefreshTokenDoc) error
	// GetByToken returns an active, non-revoked, non-expired refresh token.
	// Use this when you only care about "is this token still usable".
	GetByToken(ctx context.Context, tokenHash string) (*models.RefreshTokenDoc, error)
	// GetByTokenAny returns the refresh-token doc regardless of its
	// revocation state. Only the refresh flow calls this — it needs to
	// observe rotated rows so it can distinguish "never existed" from
	// "already used" (replay).
	GetByTokenAny(ctx context.Context, tokenHash string) (*models.RefreshTokenDoc, error)
	GetBySessionUUID(ctx context.Context, sessionUUID string) (*models.RefreshTokenDoc, error)
	GetActiveTokensByUser(ctx context.Context, userUUID string) ([]*models.RefreshTokenDoc, error)
	GetActiveTokensByDevice(ctx context.Context, userUUID, deviceID string) ([]*models.RefreshTokenDoc, error)

	// Token updates
	UpdateLastActivity(ctx context.Context, uuid string) error
	UpdateRiskScore(ctx context.Context, uuid string, riskScore float64, riskFactors []string) error
	// RotateToken is the legacy single-token rotation, still callable so older
	// code paths keep building.
	//
	// Deprecated: use RotateWithFamily.
	RotateToken(ctx context.Context, oldTokenHash, newTokenHash string) error
	// RotateWithFamily atomically marks oldTokenHash as rotated (setting
	// IsRevoked=true, RevokedReason=RevokeReasonRotated, SucceededBy=
	// newDoc.UUID) and inserts the new document. Returns
	// ErrTokenAlreadyRotated when the old row is not in the rotatable
	// state, which the refresh flow treats as a replay signal.
	RotateWithFamily(ctx context.Context, oldTokenHash string, newDoc *models.RefreshTokenDoc) error

	// Token revocation
	RevokeToken(ctx context.Context, tokenHash string, reason string) error
	RevokeTokenByUUID(ctx context.Context, uuid string, reason string) error
	RevokeTokensBySession(ctx context.Context, sessionUUID string, reason string) error
	RevokeTokensByUser(ctx context.Context, userUUID string, reason string) error
	RevokeTokensByDevice(ctx context.Context, userUUID, deviceID string, reason string) error
	// RevokeFamily marks every still-active token in the given family as
	// revoked with the supplied reason. Used for logout-family and for the
	// replay-detection response. Returns the number of rows revoked.
	RevokeFamily(ctx context.Context, familyID, reason string) (int64, error)

	// Cleanup operations
	CleanupExpiredTokens(ctx context.Context) (int64, error)
	// DeleteAllByUser hard-deletes every refresh token (active or revoked)
	// for the user. Used by the GDPR DSR right-to-erasure pipeline —
	// distinct from RevokeTokensByUser which only flips isRevoked.
	DeleteAllByUser(ctx context.Context, userUUID string) (int64, error)
	// CleanupRevokedTokens removes revoked rows whose RevokedAt is older
	// than `olderThan`. MUST be called with a value ≥ the refresh-token
	// lifetime, otherwise a replay within the live window won't find the
	// rotated row to match against. Today nothing schedules this — Block F
	// will add a guarded background reaper.
	CleanupRevokedTokens(ctx context.Context, olderThan time.Duration) (int64, error)

	// Analytics and monitoring
	GetTokenStats(ctx context.Context, userUUID string) (*TokenStats, error)
	GetDeviceTokenCount(ctx context.Context, userUUID, deviceID string) (int64, error)
	// CountFamilyMembers returns the total number of rows (including
	// revoked) with the given FamilyID — diagnostic helper for tests and
	// for the future "active sessions" admin view.
	CountFamilyMembers(ctx context.Context, familyID string) (int64, error)
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
	// tier — see authSessionRepository.tier (ADR-0003 PR-D).
	tier string
}

// NewOperatorRefreshTokenRepository binds to operator_refresh_tokens
// and stamps Tier="operator" on every CreateRefreshToken write.
// ADR-0003 PR-D.
func NewOperatorRefreshTokenRepository(db *mongo.Database) RefreshTokenRepository {
	return &refreshTokenRepository{
		collection: db.Collection(models.OperatorRefreshTokensCollection),
		tier:       models.TierOperator,
	}
}

// NewClientRefreshTokenRepository binds to client_refresh_tokens and
// stamps Tier="client" on every CreateRefreshToken write. ADR-0003 PR-D.
func NewClientRefreshTokenRepository(db *mongo.Database) RefreshTokenRepository {
	return &refreshTokenRepository{
		collection: db.Collection(models.ClientRefreshTokensCollection),
		tier:       models.TierClient,
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

	// ADR-0003 PR-D: stamp the audience tier — see authSessionRepository.
	if r.tier != "" {
		token.Tier = r.tier
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

// GetByTokenAny looks up a refresh token regardless of its revocation state.
// Returns (nil, nil) when the token never existed. Used by the refresh path
// so replay detection can see rotated rows that GetByToken would hide.
func (r *refreshTokenRepository) GetByTokenAny(ctx context.Context, tokenHash string) (*models.RefreshTokenDoc, error) {
	filter := bson.M{"token": tokenHash}

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

// RotateWithFamily atomically revokes the old row (marking it rotated with
// SucceededBy pointing at newDoc.UUID) and inserts newDoc. The CAS filter
// `{isRevoked:false}` is what makes concurrent refresh calls safe: only one
// caller can transition the row from active → rotated; the loser sees
// ErrTokenAlreadyRotated and the refresh service treats that as replay.
//
// Note: if InsertOne fails after the revoke succeeded, we return the insert
// error without reverting. That's deliberate — the old token has already
// been invalidated and must not become usable again; the resulting state
// (one extra revoked row, no successor) is safe, just a failed refresh.
func (r *refreshTokenRepository) RotateWithFamily(ctx context.Context, oldTokenHash string, newDoc *models.RefreshTokenDoc) error {
	if newDoc == nil {
		return fmt.Errorf("newDoc is required")
	}
	if newDoc.UUID == "" {
		newDoc.UUID = models.GenerateTimeOrderedUUID()
	}
	now := time.Now()
	res, err := r.collection.UpdateOne(ctx,
		bson.M{"token": oldTokenHash, "isRevoked": false},
		bson.M{"$set": bson.M{
			"isRevoked":     true,
			"revokedAt":     now,
			"revokedReason": models.RevokeReasonRotated,
			"succeededBy":   newDoc.UUID,
			"updatedAt":     now,
		}},
	)
	if err != nil {
		return fmt.Errorf("failed to mark old token rotated: %w", err)
	}
	if res.MatchedCount == 0 {
		return ErrTokenAlreadyRotated
	}

	// Normalise + hash the new doc exactly like CreateRefreshToken.
	newDoc.IssuedAt = now
	newDoc.CreatedAt = now
	newDoc.UpdatedAt = now
	newDoc.LastActivity = now
	if newDoc.Token != "" {
		newDoc.Token = utils.HashRefreshToken(newDoc.Token)
	}
	if r.tier != "" {
		newDoc.Tier = r.tier
	}
	if _, err := r.collection.InsertOne(ctx, newDoc); err != nil {
		return fmt.Errorf("failed to insert rotated token: %w", err)
	}
	return nil
}

// RevokeFamily marks every still-active row in the family as revoked. Pass
// an empty familyID and it returns (0, nil) — a no-op guard so the refresh
// path doesn't accidentally revoke every pre-Block-C row in the database.
func (r *refreshTokenRepository) RevokeFamily(ctx context.Context, familyID, reason string) (int64, error) {
	if familyID == "" {
		return 0, nil
	}
	now := time.Now()
	res, err := r.collection.UpdateMany(ctx,
		bson.M{"familyId": familyID, "isRevoked": false},
		bson.M{"$set": bson.M{
			"isRevoked":     true,
			"revokedAt":     now,
			"revokedReason": reason,
			"updatedAt":     now,
		}},
	)
	if err != nil {
		return 0, fmt.Errorf("failed to revoke family: %w", err)
	}
	return res.ModifiedCount, nil
}

// CountFamilyMembers counts every row (including revoked) with the family
// ID. Diagnostic only — not on any hot path.
func (r *refreshTokenRepository) CountFamilyMembers(ctx context.Context, familyID string) (int64, error) {
	if familyID == "" {
		return 0, nil
	}
	n, err := r.collection.CountDocuments(ctx, bson.M{"familyId": familyID})
	if err != nil {
		return 0, fmt.Errorf("failed to count family members: %w", err)
	}
	return n, nil
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
		if r.tier != "" {
			newToken.Tier = r.tier
		}

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

// DeleteAllByUser permanently removes every refresh token row for the
// user. Used by the GDPR DSR right-to-erasure path — revocation alone
// leaves personal data (userUUID, IP, device ID) in place, erasure
// requires the rows to be gone.
func (r *refreshTokenRepository) DeleteAllByUser(ctx context.Context, userUUID string) (int64, error) {
	res, err := r.collection.DeleteMany(ctx, bson.M{"userUuid": userUUID})
	if err != nil {
		return 0, fmt.Errorf("failed to delete refresh tokens by user: %w", err)
	}
	return res.DeletedCount, nil
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
