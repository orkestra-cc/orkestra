package models

import (
	"time"

	"github.com/google/uuid"
	"go.mongodb.org/mongo-driver/bson/primitive"
)

// OAuthProviderDoc represents a document in the oauth_providers collection
type OAuthProviderDoc struct {
	ID         primitive.ObjectID `bson:"_id,omitempty" json:"-"`
	UUID       string             `bson:"uuid" json:"id" validate:"required"`
	UserUUID   string             `bson:"userUuid" json:"userUuid" validate:"required"`
	Provider   OAuthProvider      `bson:"provider" json:"provider" validate:"required,oneof=google apple discord github"`
	ProviderID string             `bson:"providerId" json:"providerId" validate:"required"`
	Email      string             `bson:"email" json:"email" validate:"required,email"`
	IsPrimary  bool               `bson:"isPrimary" json:"isPrimary"`
	LinkedAt   time.Time          `bson:"linkedAt" json:"linkedAt"`
	LastUsed   *time.Time         `bson:"lastUsed,omitempty" json:"lastUsed,omitempty"`
	// OAuth Provider Tokens (encrypted)
	AccessToken           string                 `bson:"accessToken,omitempty" json:"-"`  // Encrypted OAuth access token
	RefreshToken          string                 `bson:"refreshToken,omitempty" json:"-"` // Encrypted OAuth refresh token, only if ongoing access needed
	AccessTokenExpiresAt  *time.Time             `bson:"accessTokenExpiresAt,omitempty" json:"accessTokenExpiresAt,omitempty"`
	RefreshTokenExpiresAt *time.Time             `bson:"refreshTokenExpiresAt,omitempty" json:"refreshTokenExpiresAt,omitempty"`
	TokenStatus           string                 `bson:"tokenStatus" json:"tokenStatus"` // active, expired, revoked
	LastTokenRefresh      *time.Time             `bson:"lastTokenRefresh,omitempty" json:"lastTokenRefresh,omitempty"`
	Scopes                []string               `bson:"scopes,omitempty" json:"scopes,omitempty"`
	Metadata              map[string]interface{} `bson:"metadata,omitempty" json:"metadata,omitempty"`
	CreatedAt             time.Time              `bson:"createdAt" json:"createdAt"`
	UpdatedAt             time.Time              `bson:"updatedAt" json:"updatedAt"`
}

// RefreshTokenDoc represents a document in the refresh_tokens collection
type RefreshTokenDoc struct {
	ID          primitive.ObjectID `bson:"_id,omitempty" json:"-"`
	UUID        string             `bson:"uuid" json:"id" validate:"required"`
	UserUUID    string             `bson:"userUuid" json:"userUuid" validate:"required"`
	Token       string             `bson:"token" json:"-" validate:"required"` // Hashed token
	SessionUUID string             `bson:"sessionUuid" json:"sessionId" validate:"required"`

	// Device Information
	DeviceID   string `bson:"deviceId" json:"deviceId" validate:"required"`
	DeviceName string `bson:"deviceName,omitempty" json:"deviceName,omitempty"`
	DeviceType string `bson:"deviceType" json:"deviceType" validate:"required,oneof=mobile tablet desktop watch"`
	Platform   string `bson:"platform" json:"platform" validate:"required,oneof=ios android web windows macos linux"`
	AppVersion string `bson:"appVersion,omitempty" json:"appVersion,omitempty"`

	// Security Information
	Fingerprint string    `bson:"fingerprint" json:"-" validate:"required"`
	IPAddress   string    `bson:"ipAddress" json:"ipAddress"`
	Location    *Location `bson:"location,omitempty" json:"location,omitempty"`

	// Risk Assessment
	RiskScore   float64  `bson:"riskScore" json:"riskScore"`
	RiskFactors []string `bson:"riskFactors,omitempty" json:"riskFactors,omitempty"`

	// Metadata
	UserAgent     string     `bson:"userAgent,omitempty" json:"-"`
	IssuedAt      time.Time  `bson:"issuedAt" json:"issuedAt"`
	ExpiresAt     time.Time  `bson:"expiresAt" json:"expiresAt"`
	LastActivity  time.Time  `bson:"lastActivity" json:"lastActivity"`
	IsRevoked     bool       `bson:"isRevoked" json:"isRevoked"`
	RevokedAt     *time.Time `bson:"revokedAt,omitempty" json:"revokedAt,omitempty"`
	RevokedReason string     `bson:"revokedReason,omitempty" json:"revokedReason,omitempty"`
	CreatedAt     time.Time  `bson:"createdAt" json:"createdAt"`
	UpdatedAt     time.Time  `bson:"updatedAt" json:"updatedAt"`
}

// AuthSessionDoc represents a document in the auth_sessions collection
type AuthSessionDoc struct {
	ID       primitive.ObjectID `bson:"_id,omitempty" json:"-"`
	UUID     string             `bson:"uuid" json:"id" validate:"required"`
	UserUUID string             `bson:"userUuid" json:"userUuid" validate:"required"`
	DeviceID string             `bson:"deviceId" json:"deviceId" validate:"required"`

	// Session State
	IsActive     bool      `bson:"isActive" json:"isActive"`
	StartedAt    time.Time `bson:"startedAt" json:"startedAt"`
	LastActivity time.Time `bson:"lastActivity" json:"lastActivity"`
	ExpiresAt    time.Time `bson:"expiresAt" json:"expiresAt"`

	// Authentication Method
	LoginMethod  string        `bson:"loginMethod" json:"loginMethod"` // google, apple, password, biometric
	Provider     OAuthProvider `bson:"provider,omitempty" json:"provider,omitempty"`
	MFACompleted bool          `bson:"mfaCompleted" json:"mfaCompleted"`

	// Device & Location
	DeviceInfo DeviceInfo `bson:"deviceInfo" json:"deviceInfo"`
	IPAddress  string     `bson:"ipAddress" json:"ipAddress"`
	Location   *Location  `bson:"location,omitempty" json:"location,omitempty"`

	// Security Events
	SecurityEvents []SecurityEventLog `bson:"securityEvents" json:"securityEvents"`
	RiskScore      float64            `bson:"riskScore" json:"riskScore"`
	TrustLevel     string             `bson:"trustLevel" json:"trustLevel"` // untrusted, low, medium, high, trusted

	// Metadata
	UserAgent string                 `bson:"userAgent,omitempty" json:"userAgent,omitempty"`
	Metadata  map[string]interface{} `bson:"metadata,omitempty" json:"metadata,omitempty"`
	CreatedAt time.Time              `bson:"createdAt" json:"createdAt"`
	UpdatedAt time.Time              `bson:"updatedAt" json:"updatedAt"`
}

// SecurityEventLog represents a security event within a session
type SecurityEventLog struct {
	Type        string                 `bson:"type" json:"type"` // login, token_refresh, suspicious_activity, mfa_challenge, logout
	Timestamp   time.Time              `bson:"timestamp" json:"timestamp"`
	IPAddress   string                 `bson:"ipAddress,omitempty" json:"ipAddress,omitempty"`
	Success     bool                   `bson:"success" json:"success"`
	Description string                 `bson:"description,omitempty" json:"description,omitempty"`
	RiskDelta   float64                `bson:"riskDelta,omitempty" json:"riskDelta,omitempty"` // Change in risk score
	Details     map[string]interface{} `bson:"details,omitempty" json:"details,omitempty"`
}

// Collection names
const (
	UsersCollection          = "users"
	OAuthProvidersCollection = "oauth_providers"
	RefreshTokensCollection  = "refresh_tokens"
	AuthSessionsCollection   = "auth_sessions"
	SecurityEventsCollection = "security_events"
)

// Indexes for collections
type CollectionIndexes struct {
	Collection string
	Indexes    []IndexDefinition
}

type IndexDefinition struct {
	Keys   map[string]int
	Unique bool
	TTL    *time.Duration
	Name   string
}

// GetCollectionIndexes returns the indexes needed for each collection
func GetCollectionIndexes() []CollectionIndexes {
	day7 := 7 * 24 * time.Hour // Match refresh token expiry
	day90 := 90 * 24 * time.Hour

	return []CollectionIndexes{
		{
			Collection: UsersCollection,
			Indexes: []IndexDefinition{
				{Keys: map[string]int{"uuid": 1}, Unique: true, Name: "idx_uuid"},
				{Keys: map[string]int{"email": 1}, Unique: true, Name: "idx_email"},
				{Keys: map[string]int{"oauthLinks.provider": 1, "oauthLinks.providerId": 1}, Name: "idx_oauth_links"},
			},
		},
		{
			Collection: OAuthProvidersCollection,
			Indexes: []IndexDefinition{
				{Keys: map[string]int{"uuid": 1}, Unique: true, Name: "idx_uuid"},
				{Keys: map[string]int{"userUuid": 1}, Name: "idx_user_uuid"},
				{Keys: map[string]int{"provider": 1, "providerId": 1}, Unique: true, Name: "idx_provider_id"},
				{Keys: map[string]int{"email": 1}, Name: "idx_email"},
			},
		},
		{
			Collection: RefreshTokensCollection,
			Indexes: []IndexDefinition{
				{Keys: map[string]int{"uuid": 1}, Unique: true, Name: "idx_uuid"},
				{Keys: map[string]int{"userUuid": 1}, Name: "idx_user_uuid"},
				{Keys: map[string]int{"token": 1}, Unique: true, Name: "idx_token"},
				{Keys: map[string]int{"sessionUuid": 1}, Name: "idx_session_uuid"},
				{Keys: map[string]int{"deviceId": 1}, Name: "idx_device_id"},
				{Keys: map[string]int{"expiresAt": 1}, TTL: &day7, Name: "idx_ttl_expires"},
				{Keys: map[string]int{"isRevoked": 1, "userUuid": 1}, Name: "idx_revoked_user"},
			},
		},
		{
			Collection: AuthSessionsCollection,
			Indexes: []IndexDefinition{
				{Keys: map[string]int{"uuid": 1}, Unique: true, Name: "idx_uuid"},
				{Keys: map[string]int{"userUuid": 1}, Name: "idx_user_uuid"},
				{Keys: map[string]int{"deviceId": 1}, Name: "idx_device_id"},
				{Keys: map[string]int{"isActive": 1, "userUuid": 1}, Name: "idx_active_user"},
				{Keys: map[string]int{"expiresAt": 1}, TTL: &day90, Name: "idx_ttl_expires"},
			},
		},
		{
			Collection: SecurityEventsCollection,
			Indexes: []IndexDefinition{
				{Keys: map[string]int{"userUuid": 1, "timestamp": -1}, Name: "idx_user_time"},
				{Keys: map[string]int{"eventType": 1, "timestamp": -1}, Name: "idx_type_time"},
				{Keys: map[string]int{"severity": 1, "timestamp": -1}, Name: "idx_severity_time"},
				{Keys: map[string]int{"timestamp": 1}, TTL: &day90, Name: "idx_ttl_events"},
			},
		},
	}
}

// Helper function to generate UUIDs
func GenerateUUID() string {
	return uuid.NewString()
}

// Helper function to generate time-ordered UUIDs (UUID v7)
func GenerateTimeOrderedUUID() string {
	// UUID v7 is time-ordered, better for database indexing
	id, err := uuid.NewV7()
	if err != nil {
		// Fall back to regular UUID if v7 fails
		return uuid.NewString()
	}
	return id.String()
}
