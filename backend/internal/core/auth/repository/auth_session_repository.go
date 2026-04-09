package repository

import (
	"context"
	"fmt"
	"time"

	"github.com/orkestra/backend/internal/core/auth/models"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// AuthSessionRepository handles authentication session data operations
type AuthSessionRepository interface {
	// Session lifecycle
	CreateSession(ctx context.Context, session *models.AuthSessionDoc) error
	GetByUUID(ctx context.Context, uuid string) (*models.AuthSessionDoc, error)
	GetByUserAndDevice(ctx context.Context, userUUID, deviceID string) (*models.AuthSessionDoc, error)
	GetActiveSessionsByUser(ctx context.Context, userUUID string) ([]*models.AuthSessionDoc, error)

	// Session updates
	UpdateLastActivity(ctx context.Context, uuid string) error
	UpdateRiskScore(ctx context.Context, uuid string, riskScore float64, trustLevel string) error
	AddSecurityEvent(ctx context.Context, uuid string, event *models.SecurityEventLog) error
	UpdateDeviceInfo(ctx context.Context, uuid string, deviceInfo *models.DeviceInfo) error

	// Session termination
	TerminateSession(ctx context.Context, uuid string) error
	TerminateSessionByDevice(ctx context.Context, userUUID, deviceID string) error
	TerminateAllUserSessions(ctx context.Context, userUUID string) error
	TerminateExpiredSessions(ctx context.Context) (int64, error)

	// Session queries
	GetSessionStats(ctx context.Context, userUUID string) (*SessionStats, error)
	GetActiveDevices(ctx context.Context, userUUID string) ([]*DeviceSession, error)
	GetSessionsByLocation(ctx context.Context, userUUID string, country string) ([]*models.AuthSessionDoc, error)
	GetHighRiskSessions(ctx context.Context, minRiskScore float64) ([]*models.AuthSessionDoc, error)

	// Device management
	RenameDevice(ctx context.Context, userUUID, deviceID, newName string) error
	GetDeviceSessionHistory(ctx context.Context, userUUID, deviceID string, limit int) ([]*models.AuthSessionDoc, error)

	// Security monitoring
	GetRecentSecurityEvents(ctx context.Context, userUUID string, eventType string, since time.Time) ([]*models.SecurityEventLog, error)
	GetSuspiciousSessions(ctx context.Context, userUUID string) ([]*models.AuthSessionDoc, error)
}

type SessionStats struct {
	TotalSessions    int64                      `json:"totalSessions"`
	ActiveSessions   int64                      `json:"activeSessions"`
	UniqueDevices    int64                      `json:"uniqueDevices"`
	LocationCount    map[string]int64           `json:"locationCount"`
	PlatformCount    map[string]int64           `json:"platformCount"`
	AverageRiskScore float64                    `json:"averageRiskScore"`
	HighRiskSessions int64                      `json:"highRiskSessions"`
	RecentEvents     []*models.SecurityEventLog `json:"recentEvents"`
}

type DeviceSession struct {
	DeviceID     string           `json:"deviceId"`
	DeviceName   string           `json:"deviceName"`
	DeviceType   string           `json:"deviceType"`
	Platform     string           `json:"platform"`
	LastActivity time.Time        `json:"lastActivity"`
	Location     *models.Location `json:"location,omitempty"`
	RiskScore    float64          `json:"riskScore"`
	TrustLevel   string           `json:"trustLevel"`
	IsActive     bool             `json:"isActive"`
	SessionCount int              `json:"sessionCount"`
}

type authSessionRepository struct {
	collection *mongo.Collection
}

// NewAuthSessionRepository creates a new auth session repository
func NewAuthSessionRepository(db *mongo.Database) AuthSessionRepository {
	return &authSessionRepository{
		collection: db.Collection(models.AuthSessionsCollection),
	}
}

func (r *authSessionRepository) CreateSession(ctx context.Context, session *models.AuthSessionDoc) error {
	// Set timestamps and UUID if not provided
	now := time.Now()
	if session.UUID == "" {
		session.UUID = models.GenerateTimeOrderedUUID()
	}
	session.StartedAt = now
	session.LastActivity = now
	session.CreatedAt = now
	session.UpdatedAt = now
	session.IsActive = true

	// Initialize security events if not provided
	if session.SecurityEvents == nil {
		session.SecurityEvents = []models.SecurityEventLog{}
	}

	// Validate required fields
	if session.UserUUID == "" {
		return fmt.Errorf("user UUID is required")
	}
	if session.DeviceID == "" {
		return fmt.Errorf("device ID is required")
	}

	// Set default expiration (30 days)
	if session.ExpiresAt.IsZero() {
		session.ExpiresAt = now.Add(30 * 24 * time.Hour)
	}

	_, err := r.collection.InsertOne(ctx, session)
	if err != nil {
		return fmt.Errorf("failed to create session: %w", err)
	}

	return nil
}

func (r *authSessionRepository) GetByUUID(ctx context.Context, uuid string) (*models.AuthSessionDoc, error) {
	filter := bson.M{"uuid": uuid}

	var result models.AuthSessionDoc
	err := r.collection.FindOne(ctx, filter).Decode(&result)
	if err != nil {
		if err == mongo.ErrNoDocuments {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to find session: %w", err)
	}

	return &result, nil
}

func (r *authSessionRepository) GetByUserAndDevice(ctx context.Context, userUUID, deviceID string) (*models.AuthSessionDoc, error) {
	filter := bson.M{
		"userUuid":  userUUID,
		"deviceId":  deviceID,
		"isActive":  true,
		"expiresAt": bson.M{"$gt": time.Now()},
	}

	// Get the most recent active session for this device
	opts := options.FindOne().SetSort(bson.D{{Key: "lastActivity", Value: -1}})

	var result models.AuthSessionDoc
	err := r.collection.FindOne(ctx, filter, opts).Decode(&result)
	if err != nil {
		if err == mongo.ErrNoDocuments {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to find session: %w", err)
	}

	return &result, nil
}

func (r *authSessionRepository) GetActiveSessionsByUser(ctx context.Context, userUUID string) ([]*models.AuthSessionDoc, error) {
	filter := bson.M{
		"userUuid":  userUUID,
		"isActive":  true,
		"expiresAt": bson.M{"$gt": time.Now()},
	}

	// Sort by last activity descending
	opts := options.Find().SetSort(bson.D{{Key: "lastActivity", Value: -1}})

	cursor, err := r.collection.Find(ctx, filter, opts)
	if err != nil {
		return nil, fmt.Errorf("failed to find active sessions: %w", err)
	}
	defer cursor.Close(ctx)

	var sessions []*models.AuthSessionDoc
	for cursor.Next(ctx) {
		var session models.AuthSessionDoc
		if err := cursor.Decode(&session); err != nil {
			return nil, fmt.Errorf("failed to decode session: %w", err)
		}
		sessions = append(sessions, &session)
	}

	return sessions, nil
}

func (r *authSessionRepository) UpdateLastActivity(ctx context.Context, uuid string) error {
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
		return fmt.Errorf("session not found")
	}

	return nil
}

func (r *authSessionRepository) UpdateRiskScore(ctx context.Context, uuid string, riskScore float64, trustLevel string) error {
	filter := bson.M{"uuid": uuid}
	update := bson.M{
		"$set": bson.M{
			"riskScore":  riskScore,
			"trustLevel": trustLevel,
			"updatedAt":  time.Now(),
		},
	}

	result, err := r.collection.UpdateOne(ctx, filter, update)
	if err != nil {
		return fmt.Errorf("failed to update risk score: %w", err)
	}

	if result.MatchedCount == 0 {
		return fmt.Errorf("session not found")
	}

	return nil
}

func (r *authSessionRepository) AddSecurityEvent(ctx context.Context, uuid string, event *models.SecurityEventLog) error {
	// Set timestamp if not provided
	if event.Timestamp.IsZero() {
		event.Timestamp = time.Now()
	}

	filter := bson.M{"uuid": uuid}
	update := bson.M{
		"$push": bson.M{
			"securityEvents": event,
		},
		"$set": bson.M{
			"updatedAt": time.Now(),
		},
	}

	result, err := r.collection.UpdateOne(ctx, filter, update)
	if err != nil {
		return fmt.Errorf("failed to add security event: %w", err)
	}

	if result.MatchedCount == 0 {
		return fmt.Errorf("session not found")
	}

	return nil
}

func (r *authSessionRepository) UpdateDeviceInfo(ctx context.Context, uuid string, deviceInfo *models.DeviceInfo) error {
	filter := bson.M{"uuid": uuid}
	update := bson.M{
		"$set": bson.M{
			"deviceInfo": deviceInfo,
			"updatedAt":  time.Now(),
		},
	}

	result, err := r.collection.UpdateOne(ctx, filter, update)
	if err != nil {
		return fmt.Errorf("failed to update device info: %w", err)
	}

	if result.MatchedCount == 0 {
		return fmt.Errorf("session not found")
	}

	return nil
}

func (r *authSessionRepository) TerminateSession(ctx context.Context, uuid string) error {
	filter := bson.M{"uuid": uuid}
	update := bson.M{
		"$set": bson.M{
			"isActive":  false,
			"updatedAt": time.Now(),
		},
	}

	result, err := r.collection.UpdateOne(ctx, filter, update)
	if err != nil {
		return fmt.Errorf("failed to terminate session: %w", err)
	}

	if result.MatchedCount == 0 {
		return fmt.Errorf("session not found")
	}

	return nil
}

func (r *authSessionRepository) TerminateSessionByDevice(ctx context.Context, userUUID, deviceID string) error {
	filter := bson.M{
		"userUuid": userUUID,
		"deviceId": deviceID,
		"isActive": true,
	}
	update := bson.M{
		"$set": bson.M{
			"isActive":  false,
			"updatedAt": time.Now(),
		},
	}

	_, err := r.collection.UpdateMany(ctx, filter, update)
	if err != nil {
		return fmt.Errorf("failed to terminate device sessions: %w", err)
	}

	return nil
}

func (r *authSessionRepository) TerminateAllUserSessions(ctx context.Context, userUUID string) error {
	filter := bson.M{
		"userUuid": userUUID,
		"isActive": true,
	}
	update := bson.M{
		"$set": bson.M{
			"isActive":  false,
			"updatedAt": time.Now(),
		},
	}

	_, err := r.collection.UpdateMany(ctx, filter, update)
	if err != nil {
		return fmt.Errorf("failed to terminate all user sessions: %w", err)
	}

	return nil
}

func (r *authSessionRepository) TerminateExpiredSessions(ctx context.Context) (int64, error) {
	filter := bson.M{
		"isActive":  true,
		"expiresAt": bson.M{"$lt": time.Now()},
	}
	update := bson.M{
		"$set": bson.M{
			"isActive":  false,
			"updatedAt": time.Now(),
		},
	}

	result, err := r.collection.UpdateMany(ctx, filter, update)
	if err != nil {
		return 0, fmt.Errorf("failed to terminate expired sessions: %w", err)
	}

	return result.ModifiedCount, nil
}

func (r *authSessionRepository) GetSessionStats(ctx context.Context, userUUID string) (*SessionStats, error) {
	pipeline := []bson.M{
		{
			"$match": bson.M{"userUuid": userUUID},
		},
		{
			"$group": bson.M{
				"_id":           nil,
				"totalSessions": bson.M{"$sum": 1},
				"activeSessions": bson.M{
					"$sum": bson.M{
						"$cond": bson.M{
							"if": bson.M{
								"$and": []bson.M{
									{"$eq": []interface{}{"$isActive", true}},
									{"$gt": []interface{}{"$expiresAt", time.Now()}},
								},
							},
							"then": 1,
							"else": 0,
						},
					},
				},
				"uniqueDevices": bson.M{"$addToSet": "$deviceId"},
				"avgRiskScore":  bson.M{"$avg": "$riskScore"},
				"highRiskSessions": bson.M{
					"$sum": bson.M{
						"$cond": bson.M{
							"if":   bson.M{"$gt": []interface{}{"$riskScore", 0.7}},
							"then": 1,
							"else": 0,
						},
					},
				},
				"locations": bson.M{"$push": "$location.country"},
				"platforms": bson.M{"$push": "$deviceInfo.platform"},
			},
		},
	}

	cursor, err := r.collection.Aggregate(ctx, pipeline)
	if err != nil {
		return nil, fmt.Errorf("failed to get session stats: %w", err)
	}
	defer cursor.Close(ctx)

	var result struct {
		TotalSessions    int64    `bson:"totalSessions"`
		ActiveSessions   int64    `bson:"activeSessions"`
		UniqueDevices    []string `bson:"uniqueDevices"`
		AvgRiskScore     float64  `bson:"avgRiskScore"`
		HighRiskSessions int64    `bson:"highRiskSessions"`
		Locations        []string `bson:"locations"`
		Platforms        []string `bson:"platforms"`
	}

	if cursor.Next(ctx) {
		if err := cursor.Decode(&result); err != nil {
			return nil, fmt.Errorf("failed to decode session stats: %w", err)
		}
	}

	// Count locations and platforms
	locationCount := make(map[string]int64)
	platformCount := make(map[string]int64)

	for _, location := range result.Locations {
		if location != "" {
			locationCount[location]++
		}
	}

	for _, platform := range result.Platforms {
		if platform != "" {
			platformCount[platform]++
		}
	}

	stats := &SessionStats{
		TotalSessions:    result.TotalSessions,
		ActiveSessions:   result.ActiveSessions,
		UniqueDevices:    int64(len(result.UniqueDevices)),
		LocationCount:    locationCount,
		PlatformCount:    platformCount,
		AverageRiskScore: result.AvgRiskScore,
		HighRiskSessions: result.HighRiskSessions,
		RecentEvents:     []*models.SecurityEventLog{}, // Would need separate query
	}

	return stats, nil
}

func (r *authSessionRepository) GetActiveDevices(ctx context.Context, userUUID string) ([]*DeviceSession, error) {
	pipeline := []bson.M{
		{
			"$match": bson.M{
				"userUuid":  userUUID,
				"isActive":  true,
				"expiresAt": bson.M{"$gt": time.Now()},
			},
		},
		{
			"$group": bson.M{
				"_id":          "$deviceId",
				"deviceName":   bson.M{"$last": "$deviceInfo.deviceName"},
				"deviceType":   bson.M{"$last": "$deviceInfo.deviceType"},
				"platform":     bson.M{"$last": "$deviceInfo.platform"},
				"lastActivity": bson.M{"$max": "$lastActivity"},
				"location":     bson.M{"$last": "$location"},
				"riskScore":    bson.M{"$avg": "$riskScore"},
				"trustLevel":   bson.M{"$last": "$trustLevel"},
				"sessionCount": bson.M{"$sum": 1},
			},
		},
		{
			"$sort": bson.M{"lastActivity": -1},
		},
	}

	cursor, err := r.collection.Aggregate(ctx, pipeline)
	if err != nil {
		return nil, fmt.Errorf("failed to get active devices: %w", err)
	}
	defer cursor.Close(ctx)

	var devices []*DeviceSession
	for cursor.Next(ctx) {
		var device DeviceSession
		if err := cursor.Decode(&device); err != nil {
			return nil, fmt.Errorf("failed to decode device session: %w", err)
		}
		device.DeviceID = cursor.Current.Lookup("_id").StringValue()
		device.IsActive = true
		devices = append(devices, &device)
	}

	return devices, nil
}

func (r *authSessionRepository) GetSessionsByLocation(ctx context.Context, userUUID string, country string) ([]*models.AuthSessionDoc, error) {
	filter := bson.M{
		"userUuid":         userUUID,
		"location.country": country,
	}

	opts := options.Find().SetSort(bson.D{{Key: "lastActivity", Value: -1}})

	cursor, err := r.collection.Find(ctx, filter, opts)
	if err != nil {
		return nil, fmt.Errorf("failed to find sessions by location: %w", err)
	}
	defer cursor.Close(ctx)

	var sessions []*models.AuthSessionDoc
	for cursor.Next(ctx) {
		var session models.AuthSessionDoc
		if err := cursor.Decode(&session); err != nil {
			return nil, fmt.Errorf("failed to decode session: %w", err)
		}
		sessions = append(sessions, &session)
	}

	return sessions, nil
}

func (r *authSessionRepository) GetHighRiskSessions(ctx context.Context, minRiskScore float64) ([]*models.AuthSessionDoc, error) {
	filter := bson.M{
		"isActive":  true,
		"riskScore": bson.M{"$gte": minRiskScore},
		"expiresAt": bson.M{"$gt": time.Now()},
	}

	opts := options.Find().SetSort(bson.D{{Key: "riskScore", Value: -1}})

	cursor, err := r.collection.Find(ctx, filter, opts)
	if err != nil {
		return nil, fmt.Errorf("failed to find high risk sessions: %w", err)
	}
	defer cursor.Close(ctx)

	var sessions []*models.AuthSessionDoc
	for cursor.Next(ctx) {
		var session models.AuthSessionDoc
		if err := cursor.Decode(&session); err != nil {
			return nil, fmt.Errorf("failed to decode session: %w", err)
		}
		sessions = append(sessions, &session)
	}

	return sessions, nil
}

func (r *authSessionRepository) RenameDevice(ctx context.Context, userUUID, deviceID, newName string) error {
	filter := bson.M{
		"userUuid": userUUID,
		"deviceId": deviceID,
	}
	update := bson.M{
		"$set": bson.M{
			"deviceInfo.deviceName": newName,
			"updatedAt":             time.Now(),
		},
	}

	_, err := r.collection.UpdateMany(ctx, filter, update)
	if err != nil {
		return fmt.Errorf("failed to rename device: %w", err)
	}

	return nil
}

func (r *authSessionRepository) GetDeviceSessionHistory(ctx context.Context, userUUID, deviceID string, limit int) ([]*models.AuthSessionDoc, error) {
	filter := bson.M{
		"userUuid": userUUID,
		"deviceId": deviceID,
	}

	opts := options.Find().
		SetSort(bson.D{{Key: "startedAt", Value: -1}}).
		SetLimit(int64(limit))

	cursor, err := r.collection.Find(ctx, filter, opts)
	if err != nil {
		return nil, fmt.Errorf("failed to get device session history: %w", err)
	}
	defer cursor.Close(ctx)

	var sessions []*models.AuthSessionDoc
	for cursor.Next(ctx) {
		var session models.AuthSessionDoc
		if err := cursor.Decode(&session); err != nil {
			return nil, fmt.Errorf("failed to decode session: %w", err)
		}
		sessions = append(sessions, &session)
	}

	return sessions, nil
}

func (r *authSessionRepository) GetRecentSecurityEvents(ctx context.Context, userUUID string, eventType string, since time.Time) ([]*models.SecurityEventLog, error) {
	matchStage := bson.M{
		"$match": bson.M{
			"userUuid": userUUID,
		},
	}

	if !since.IsZero() {
		matchStage["$match"].(bson.M)["securityEvents.timestamp"] = bson.M{"$gte": since}
	}

	pipeline := []bson.M{
		matchStage,
		{
			"$unwind": "$securityEvents",
		},
		{
			"$match": bson.M{
				"securityEvents.timestamp": bson.M{"$gte": since},
			},
		},
		{
			"$sort": bson.M{"securityEvents.timestamp": -1},
		},
		{
			"$limit": 100, // Limit to recent events
		},
	}

	if eventType != "" {
		pipeline = append(pipeline, bson.M{
			"$match": bson.M{
				"securityEvents.type": eventType,
			},
		})
	}

	cursor, err := r.collection.Aggregate(ctx, pipeline)
	if err != nil {
		return nil, fmt.Errorf("failed to get security events: %w", err)
	}
	defer cursor.Close(ctx)

	var events []*models.SecurityEventLog
	for cursor.Next(ctx) {
		var result struct {
			SecurityEvents models.SecurityEventLog `bson:"securityEvents"`
		}
		if err := cursor.Decode(&result); err != nil {
			return nil, fmt.Errorf("failed to decode security event: %w", err)
		}
		events = append(events, &result.SecurityEvents)
	}

	return events, nil
}

func (r *authSessionRepository) GetSuspiciousSessions(ctx context.Context, userUUID string) ([]*models.AuthSessionDoc, error) {
	// Define criteria for suspicious sessions
	filter := bson.M{
		"userUuid": userUUID,
		"isActive": true,
		"$or": []bson.M{
			{"riskScore": bson.M{"$gte": 0.8}}, // High risk score
			{"trustLevel": "untrusted"},        // Untrusted device
			{"securityEvents.type": bson.M{"$in": []string{"suspicious_activity", "mfa_challenge"}}}, // Suspicious events
		},
	}

	opts := options.Find().SetSort(bson.D{{Key: "riskScore", Value: -1}})

	cursor, err := r.collection.Find(ctx, filter, opts)
	if err != nil {
		return nil, fmt.Errorf("failed to find suspicious sessions: %w", err)
	}
	defer cursor.Close(ctx)

	var sessions []*models.AuthSessionDoc
	for cursor.Next(ctx) {
		var session models.AuthSessionDoc
		if err := cursor.Decode(&session); err != nil {
			return nil, fmt.Errorf("failed to decode session: %w", err)
		}
		sessions = append(sessions, &session)
	}

	return sessions, nil
}
