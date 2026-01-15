package services

import (
	"context"
	"time"

	"github.com/orkestra/backend/internal/auth/models"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// SecurityEventService handles security event logging and analysis
type SecurityEventService interface {
	RecordEvent(ctx context.Context, event *models.SecurityEvent) error
	GetUserEvents(ctx context.Context, userUUID string, limit int) ([]*models.SecurityEvent, error)
	GetEventsByType(ctx context.Context, eventType string, limit int) ([]*models.SecurityEvent, error)
	GetSuspiciousEvents(ctx context.Context, limit int) ([]*models.SecurityEvent, error)
	MarkEventResolved(ctx context.Context, eventID primitive.ObjectID, resolvedBy string) error
	AnalyzeSecurityTrends(ctx context.Context, userUUID string, hours int) (*SecurityTrendAnalysis, error)
	GetFailedLoginAttempts(ctx context.Context, userUUID string, hours int) (int, error)
	GetSuspiciousActivityScore(ctx context.Context, userUUID string) (float64, error)
}

type SecurityTrendAnalysis struct {
	UserUUID             string    `json:"userUuid"`
	PeriodStart          time.Time `json:"periodStart"`
	PeriodEnd            time.Time `json:"periodEnd"`
	TotalEvents          int       `json:"totalEvents"`
	FailedLogins         int       `json:"failedLogins"`
	SuspiciousActivities int       `json:"suspiciousActivities"`
	UniqueLocations      int       `json:"uniqueLocations"`
	UniqueDevices        int       `json:"uniqueDevices"`
	RiskTrendScore       float64   `json:"riskTrendScore"`
	RecommendedActions   []string  `json:"recommendedActions"`
}

type securityEventService struct {
	db         *mongo.Database
	collection *mongo.Collection
}

// NewSecurityEventService creates a new security event service
func NewSecurityEventService(db *mongo.Database) (SecurityEventService, error) {
	collection := db.Collection("security_events")

	// Create indexes
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	indexes := []mongo.IndexModel{
		{
			Keys: bson.D{{Key: "userUuid", Value: 1}},
		},
		{
			Keys: bson.D{{Key: "eventType", Value: 1}},
		},
		{
			Keys: bson.D{{Key: "timestamp", Value: -1}},
		},
		{
			Keys: bson.D{{Key: "userUuid", Value: 1}, {Key: "timestamp", Value: -1}},
		},
		{
			Keys: bson.D{{Key: "riskScore", Value: -1}},
		},
		{
			Keys: bson.D{{Key: "resolved", Value: 1}},
		},
		{
			Keys: bson.D{{Key: "sessionId", Value: 1}},
		},
		{
			Keys: bson.D{{Key: "ip", Value: 1}},
		},
		{
			// TTL index - events older than 1 year are automatically deleted
			Keys:    bson.D{{Key: "timestamp", Value: 1}},
			Options: options.Index().SetExpireAfterSeconds(365 * 24 * 60 * 60),
		},
	}

	if _, err := collection.Indexes().CreateMany(ctx, indexes); err != nil {
		return nil, err
	}

	return &securityEventService{
		db:         db,
		collection: collection,
	}, nil
}

func (s *securityEventService) RecordEvent(ctx context.Context, event *models.SecurityEvent) error {
	if event.Timestamp.IsZero() {
		event.Timestamp = time.Now()
	}

	_, err := s.collection.InsertOne(ctx, event)
	return err
}

func (s *securityEventService) GetUserEvents(ctx context.Context, userUUID string, limit int) ([]*models.SecurityEvent, error) {
	filter := bson.M{"userUuid": userUUID}

	opts := options.Find().
		SetSort(bson.D{{Key: "timestamp", Value: -1}}).
		SetLimit(int64(limit))

	cursor, err := s.collection.Find(ctx, filter, opts)
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)

	var events []*models.SecurityEvent
	if err := cursor.All(ctx, &events); err != nil {
		return nil, err
	}

	return events, nil
}

func (s *securityEventService) GetEventsByType(ctx context.Context, eventType string, limit int) ([]*models.SecurityEvent, error) {
	filter := bson.M{"eventType": eventType}

	opts := options.Find().
		SetSort(bson.D{{Key: "timestamp", Value: -1}}).
		SetLimit(int64(limit))

	cursor, err := s.collection.Find(ctx, filter, opts)
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)

	var events []*models.SecurityEvent
	if err := cursor.All(ctx, &events); err != nil {
		return nil, err
	}

	return events, nil
}

func (s *securityEventService) GetSuspiciousEvents(ctx context.Context, limit int) ([]*models.SecurityEvent, error) {
	filter := bson.M{
		"$or": []bson.M{
			{"riskScore": bson.M{"$gte": 0.7}},
			{"resolved": false},
			{"eventType": bson.M{"$regex": "suspicious|failed|blocked", "$options": "i"}},
		},
	}

	opts := options.Find().
		SetSort(bson.D{{Key: "timestamp", Value: -1}}).
		SetLimit(int64(limit))

	cursor, err := s.collection.Find(ctx, filter, opts)
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)

	var events []*models.SecurityEvent
	if err := cursor.All(ctx, &events); err != nil {
		return nil, err
	}

	return events, nil
}

func (s *securityEventService) MarkEventResolved(ctx context.Context, eventID primitive.ObjectID, resolvedBy string) error {
	filter := bson.M{"_id": eventID}
	update := bson.M{
		"$set": bson.M{
			"resolved":   true,
			"resolvedAt": time.Now(),
			"resolvedBy": resolvedBy,
		},
	}

	_, err := s.collection.UpdateOne(ctx, filter, update)
	return err
}

func (s *securityEventService) AnalyzeSecurityTrends(ctx context.Context, userUUID string, hours int) (*SecurityTrendAnalysis, error) {
	now := time.Now()
	periodStart := now.Add(-time.Duration(hours) * time.Hour)

	// Aggregate security events for the period
	pipeline := []bson.M{
		{
			"$match": bson.M{
				"userUuid": userUUID,
				"timestamp": bson.M{
					"$gte": periodStart,
					"$lte": now,
				},
			},
		},
		{
			"$group": bson.M{
				"_id":         nil,
				"totalEvents": bson.M{"$sum": 1},
				"failedLogins": bson.M{
					"$sum": bson.M{
						"$cond": []interface{}{
							bson.M{"$regexMatch": bson.M{
								"input":   "$eventType",
								"regex":   "failed_login|login_failed",
								"options": "i",
							}},
							1,
							0,
						},
					},
				},
				"suspiciousActivities": bson.M{
					"$sum": bson.M{
						"$cond": []interface{}{
							bson.M{"$gte": []interface{}{"$riskScore", 0.7}},
							1,
							0,
						},
					},
				},
				"uniqueLocations": bson.M{
					"$addToSet": "$location.country",
				},
				"uniqueDevices": bson.M{
					"$addToSet": "$metadata.deviceId",
				},
				"avgRiskScore": bson.M{"$avg": "$riskScore"},
			},
		},
		{
			"$project": bson.M{
				"totalEvents":          1,
				"failedLogins":         1,
				"suspiciousActivities": 1,
				"uniqueLocations":      bson.M{"$size": "$uniqueLocations"},
				"uniqueDevices":        bson.M{"$size": "$uniqueDevices"},
				"avgRiskScore":         1,
			},
		},
	}

	cursor, err := s.collection.Aggregate(ctx, pipeline)
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)

	var result struct {
		TotalEvents          int     `bson:"totalEvents"`
		FailedLogins         int     `bson:"failedLogins"`
		SuspiciousActivities int     `bson:"suspiciousActivities"`
		UniqueLocations      int     `bson:"uniqueLocations"`
		UniqueDevices        int     `bson:"uniqueDevices"`
		AvgRiskScore         float64 `bson:"avgRiskScore"`
	}

	if cursor.Next(ctx) {
		if err := cursor.Decode(&result); err != nil {
			return nil, err
		}
	}

	// Calculate risk trend score
	riskTrendScore := s.calculateRiskTrendScore(&result)

	// Generate recommendations
	recommendations := s.generateSecurityRecommendations(&result, riskTrendScore)

	analysis := &SecurityTrendAnalysis{
		UserUUID:             userUUID,
		PeriodStart:          periodStart,
		PeriodEnd:            now,
		TotalEvents:          result.TotalEvents,
		FailedLogins:         result.FailedLogins,
		SuspiciousActivities: result.SuspiciousActivities,
		UniqueLocations:      result.UniqueLocations,
		UniqueDevices:        result.UniqueDevices,
		RiskTrendScore:       riskTrendScore,
		RecommendedActions:   recommendations,
	}

	return analysis, nil
}

func (s *securityEventService) GetFailedLoginAttempts(ctx context.Context, userUUID string, hours int) (int, error) {
	now := time.Now()
	since := now.Add(-time.Duration(hours) * time.Hour)

	filter := bson.M{
		"userUuid": userUUID,
		"eventType": bson.M{
			"$regex":   "failed_login|login_failed|failed_auth",
			"$options": "i",
		},
		"timestamp": bson.M{
			"$gte": since,
			"$lte": now,
		},
	}

	count, err := s.collection.CountDocuments(ctx, filter)
	if err != nil {
		return 0, err
	}

	return int(count), nil
}

func (s *securityEventService) GetSuspiciousActivityScore(ctx context.Context, userUUID string) (float64, error) {
	// Calculate suspicious activity score based on recent events
	now := time.Now()
	since := now.Add(-24 * time.Hour) // Last 24 hours

	pipeline := []bson.M{
		{
			"$match": bson.M{
				"userUuid": userUUID,
				"timestamp": bson.M{
					"$gte": since,
					"$lte": now,
				},
			},
		},
		{
			"$group": bson.M{
				"_id":          nil,
				"avgRiskScore": bson.M{"$avg": "$riskScore"},
				"maxRiskScore": bson.M{"$max": "$riskScore"},
				"totalEvents":  bson.M{"$sum": 1},
				"highRiskEvents": bson.M{
					"$sum": bson.M{
						"$cond": []interface{}{
							bson.M{"$gte": []interface{}{"$riskScore", 0.7}},
							1,
							0,
						},
					},
				},
			},
		},
	}

	cursor, err := s.collection.Aggregate(ctx, pipeline)
	if err != nil {
		return 0.0, err
	}
	defer cursor.Close(ctx)

	var result struct {
		AvgRiskScore   float64 `bson:"avgRiskScore"`
		MaxRiskScore   float64 `bson:"maxRiskScore"`
		TotalEvents    int     `bson:"totalEvents"`
		HighRiskEvents int     `bson:"highRiskEvents"`
	}

	if cursor.Next(ctx) {
		if err := cursor.Decode(&result); err != nil {
			return 0.0, err
		}
	}

	// Calculate composite suspicious activity score
	score := result.AvgRiskScore * 0.4

	if result.TotalEvents > 0 {
		highRiskRatio := float64(result.HighRiskEvents) / float64(result.TotalEvents)
		score += highRiskRatio * 0.3
	}

	score += result.MaxRiskScore * 0.3

	// Adjust for activity volume
	if result.TotalEvents > 20 {
		score += 0.1 // Bonus for high activity volume
	}

	if score > 1.0 {
		score = 1.0
	}

	return score, nil
}

// Helper functions

func (s *securityEventService) calculateRiskTrendScore(result interface{}) float64 {
	r := result.(*struct {
		TotalEvents          int     `bson:"totalEvents"`
		FailedLogins         int     `bson:"failedLogins"`
		SuspiciousActivities int     `bson:"suspiciousActivities"`
		UniqueLocations      int     `bson:"uniqueLocations"`
		UniqueDevices        int     `bson:"uniqueDevices"`
		AvgRiskScore         float64 `bson:"avgRiskScore"`
	})

	score := 0.0

	// Base score from average risk
	score += r.AvgRiskScore * 0.4

	// Failed login penalty
	if r.TotalEvents > 0 {
		failureRate := float64(r.FailedLogins) / float64(r.TotalEvents)
		score += failureRate * 0.3
	}

	// Suspicious activity penalty
	if r.TotalEvents > 0 {
		suspiciousRate := float64(r.SuspiciousActivities) / float64(r.TotalEvents)
		score += suspiciousRate * 0.2
	}

	// Location diversity penalty (too many locations = suspicious)
	if r.UniqueLocations > 3 {
		score += 0.1
	}

	// Device diversity penalty
	if r.UniqueDevices > 5 {
		score += 0.1
	}

	if score > 1.0 {
		score = 1.0
	}

	return score
}

func (s *securityEventService) generateSecurityRecommendations(result interface{}, riskScore float64) []string {
	r := result.(*struct {
		TotalEvents          int     `bson:"totalEvents"`
		FailedLogins         int     `bson:"failedLogins"`
		SuspiciousActivities int     `bson:"suspiciousActivities"`
		UniqueLocations      int     `bson:"uniqueLocations"`
		UniqueDevices        int     `bson:"uniqueDevices"`
		AvgRiskScore         float64 `bson:"avgRiskScore"`
	})

	var recommendations []string

	if riskScore > 0.7 {
		recommendations = append(recommendations, "Enable mandatory multi-factor authentication")
		recommendations = append(recommendations, "Review and update account security settings")
	}

	if r.FailedLogins > 5 {
		recommendations = append(recommendations, "Consider password reset due to multiple failed attempts")
		recommendations = append(recommendations, "Review account for potential compromise")
	}

	if r.UniqueLocations > 3 {
		recommendations = append(recommendations, "Verify recent login locations")
		recommendations = append(recommendations, "Enable location-based security alerts")
	}

	if r.UniqueDevices > 5 {
		recommendations = append(recommendations, "Review and remove unrecognized devices")
		recommendations = append(recommendations, "Enable device registration requirements")
	}

	if r.SuspiciousActivities > 3 {
		recommendations = append(recommendations, "Investigate recent suspicious activities")
		recommendations = append(recommendations, "Consider temporary account restrictions")
	}

	if len(recommendations) == 0 {
		recommendations = append(recommendations, "Continue monitoring account activity")
	}

	return recommendations
}
