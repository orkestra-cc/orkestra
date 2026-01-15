package models

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
)

type OAuthProvider string

const (
	OAuthProviderGoogle  OAuthProvider = "google"
	OAuthProviderApple   OAuthProvider = "apple"
	OAuthProviderDiscord OAuthProvider = "discord"
	OAuthProviderGitHub  OAuthProvider = "github"
)

// UnmarshalJSON implements the json.Unmarshaler interface for OAuthProvider
func (o *OAuthProvider) UnmarshalJSON(data []byte) error {
	var s string
	if err := json.Unmarshal(data, &s); err != nil {
		return err
	}
	*o = OAuthProvider(s)
	return nil
}

// MarshalJSON implements the json.Marshaler interface for OAuthProvider
func (o OAuthProvider) MarshalJSON() ([]byte, error) {
	return json.Marshal(string(o))
}

type OAuthLink struct {
	Provider   OAuthProvider          `bson:"provider" json:"provider" validate:"required,oneof=google apple discord github"`
	ProviderID string                 `bson:"providerId" json:"providerId" validate:"required"`
	Email      string                 `bson:"email" json:"email" validate:"required,email"`
	LinkedAt   time.Time              `bson:"linkedAt" json:"linkedAt"`
	IsActive   bool                   `bson:"isActive" json:"isActive"`
	IsPrimary  bool                   `bson:"isPrimary" json:"isPrimary"`
	OAuthData  map[string]interface{} `bson:"oauthData,omitempty" json:"-"`
	LastUsed   *time.Time             `bson:"lastUsed,omitempty" json:"lastUsed,omitempty"`
}

// NOTE: User struct has been moved to internal/user/models/user.go
// Auth module now uses the unified user model from the user module

type RefreshToken struct {
	Token        string     `bson:"token" json:"-"`
	SessionID    string     `bson:"sessionId" json:"sessionId"`
	ExpiresAt    time.Time  `bson:"expiresAt" json:"-"`
	DeviceID     string     `bson:"deviceId,omitempty" json:"-"`
	DeviceName   string     `bson:"deviceName,omitempty" json:"deviceName,omitempty"`
	DeviceType   string     `bson:"deviceType,omitempty" json:"deviceType,omitempty"`
	Platform     string     `bson:"platform,omitempty" json:"platform,omitempty"`
	UserAgent    string     `bson:"userAgent,omitempty" json:"-"`
	IP           string     `bson:"ip,omitempty" json:"ip,omitempty"`
	Fingerprint  string     `bson:"fingerprint,omitempty" json:"-"`
	Location     *Location  `bson:"location,omitempty" json:"location,omitempty"`
	RiskScore    float64    `bson:"riskScore,omitempty" json:"riskScore,omitempty"`
	LastActivity *time.Time `bson:"lastActivity,omitempty" json:"lastActivity,omitempty"`
	CreatedAt    time.Time  `bson:"createdAt" json:"createdAt"`
}

type Location struct {
	Country   string  `bson:"country,omitempty" json:"country,omitempty"`
	City      string  `bson:"city,omitempty" json:"city,omitempty"`
	Latitude  float64 `bson:"latitude,omitempty" json:"latitude,omitempty"`
	Longitude float64 `bson:"longitude,omitempty" json:"longitude,omitempty"`
	Timezone  string  `bson:"timezone,omitempty" json:"timezone,omitempty"`
	ISP       string  `bson:"isp,omitempty" json:"isp,omitempty"`
}

// NOTE: CreateUserInput and UpdateUserInput have been moved to internal/user/models/user.go
// Auth module now uses the unified user input types from the user module

// OAuthProviderInfo represents OAuth provider information for API responses
type OAuthProviderInfo struct {
	Provider   OAuthProvider          `json:"provider"`
	ProviderID string                 `json:"providerId"`
	Email      string                 `json:"email"`
	IsPrimary  bool                   `json:"isPrimary"`
	Metadata   map[string]interface{} `json:"metadata,omitempty"`
	Scopes     []string               `json:"scopes,omitempty"`
}

// NOTE: UserResponse has been moved to internal/user/models/user.go
// Auth module now uses the unified user response types from the user module

// SessionCountResponse represents the response for session count
type SessionCountResponse struct {
	Count int `json:"count"`
}

// RenameSessionInput represents input for renaming a session
type RenameSessionInput struct {
	DeviceName string `json:"deviceName" validate:"required,min=1,max=50"`
}

// NOTE: ToResponse method has been moved to internal/user/models/user.go

// ToSessionInfo converts a RefreshToken to SessionInfo for API responses
func (rt *RefreshToken) ToSessionInfo(isCurrent bool) *SessionInfo {
	deviceName := rt.DeviceName
	if deviceName == "" {
		deviceName = generateFriendlyDeviceName(rt.DeviceType, rt.Platform)
	}

	lastActivity := rt.CreatedAt
	if rt.LastActivity != nil {
		lastActivity = *rt.LastActivity
	}

	return &SessionInfo{
		SessionID:    rt.SessionID,
		DeviceID:     rt.DeviceID,
		DeviceName:   deviceName,
		DeviceType:   rt.DeviceType,
		Platform:     rt.Platform,
		IPAddress:    rt.IP,
		Location:     rt.Location,
		RiskScore:    rt.RiskScore,
		IsCurrent:    isCurrent,
		LastActivity: lastActivity,
		CreatedAt:    rt.CreatedAt,
		ExpiresAt:    rt.ExpiresAt,
	}
}

// generateFriendlyDeviceName creates a user-friendly device name
func generateFriendlyDeviceName(deviceType, platform string) string {
	if deviceType == "" && platform == "" {
		return "Unknown Device"
	}

	platformName := platform
	switch platform {
	case "ios":
		platformName = "iOS"
	case "android":
		platformName = "Android"
	case "windows":
		platformName = "Windows"
	case "macos":
		platformName = "macOS"
	case "linux":
		platformName = "Linux"
	}

	deviceTypeName := deviceType
	switch deviceType {
	case "mobile":
		deviceTypeName = "Mobile"
	case "tablet":
		deviceTypeName = "Tablet"
	case "desktop":
		deviceTypeName = "Desktop"
	}

	if platformName != "" && deviceTypeName != "" {
		return fmt.Sprintf("%s %s", platformName, deviceTypeName)
	} else if platformName != "" {
		return platformName
	} else if deviceTypeName != "" {
		return deviceTypeName
	}

	return "Unknown Device"
}

// GenerateUUIDv7 generates a new UUID v7 (time-ordered)
func GenerateUUIDv7() string {
	return uuid.Must(uuid.NewV7()).String()
}

// NOTE: User-related functions (NewUser, GetPrimaryOAuthLink, AddOAuthLink, etc.)
// have been moved to internal/user/models/user.go
// Auth module now uses the unified user model from the user module

// ConvertOAuthProvidersToInfo converts OAuthProviderDoc to OAuthProviderInfo for API responses
func ConvertOAuthProvidersToInfo(providers []*OAuthProviderDoc) []OAuthProviderInfo {
	result := make([]OAuthProviderInfo, 0, len(providers))

	for _, provider := range providers {
		info := OAuthProviderInfo{
			Provider:   provider.Provider,
			ProviderID: provider.ProviderID,
			Email:      provider.Email,
			IsPrimary:  provider.IsPrimary,
			Metadata:   provider.Metadata,
			Scopes:     provider.Scopes,
		}
		result = append(result, info)
	}

	return result
}
