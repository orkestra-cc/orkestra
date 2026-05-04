package models

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
	"go.mongodb.org/mongo-driver/bson/primitive"
)

// UserOAuthProviderInfo represents OAuth provider information in user API responses
type UserOAuthProviderInfo struct {
	Provider string `json:"provider" validate:"required"`
	Email    string `json:"email" validate:"required,email"`
	Avatar   string `json:"avatar,omitempty"`
}

// OAuthProvider represents the supported OAuth providers
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

// OAuthLink represents a linked OAuth provider for a user
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

// MedicalCheck represents a medical check record for a user
type MedicalCheck struct {
	ID     string     `bson:"id" json:"id"`
	Type   string     `bson:"type" json:"type" validate:"required"`
	Notes  string     `bson:"notes,omitempty" json:"notes,omitempty"`
	Expiry *time.Time `bson:"expiry,omitempty" json:"expiry,omitempty"`
	Booked *time.Time `bson:"booked,omitempty" json:"booked,omitempty"`
	Where  string     `bson:"where,omitempty" json:"where,omitempty"`
	Doctor string     `bson:"doctor,omitempty" json:"doctor,omitempty"`
}

// User represents the unified user model combining authentication and driver-specific fields
// Tier values stamped on every user record by ADR-0003 PR-B as a
// defense-in-depth guard. A repository invariant test asserts the field
// matches the owning collection (operator_users → "operator", client_users
// → "client") so a misrouted query against the wrong collection fails
// loudly. The legacy `users` collection (pre-PR-B) leaves the field empty
// — repositories that read from it skip the guard until the migration
// script populates Tier in-place.
const (
	TierOperator = "operator"
	TierClient   = "client"
)

type User struct {
	ID       primitive.ObjectID `bson:"_id,omitempty" json:"-"`
	UUID     string             `bson:"uuid" json:"id" validate:"required"`
	// Tier discriminates operator (Tier-1 internal) from client (Tier-2
	// external) users. Set by the constructor matching the target
	// collection; checked on read by the tier-aware repositories.
	Tier  string `bson:"tier,omitempty" json:"-"`
	Email string `bson:"email" json:"email" validate:"required,email"`
	Username string             `bson:"username" json:"username"`
	FullName string             `bson:"fullName" json:"fullName"`
	Avatar   string             `bson:"avatar,omitempty" json:"avatar,omitempty"`
	Phone    string             `bson:"phone" json:"phone" validate:"omitempty,e164"`
	PIN      string             `bson:"pin,omitempty" json:"-"` // Encrypted, never exposed
	Role     string             `bson:"role" json:"role" validate:"required,oneof=super_admin administrator developer manager operator guest"`

	// OAuth fields
	OAuthLinks    []OAuthLink            `bson:"oauthLinks,omitempty" json:"oauthLinks,omitempty"`
	OAuthProvider OAuthProvider          `bson:"oauthProvider,omitempty" json:"oauthProvider,omitempty"` // Deprecated, for backward compatibility
	OAuthID       string                 `bson:"oauthId,omitempty" json:"oauthId,omitempty"`             // Deprecated, for backward compatibility
	OAuthData     map[string]interface{} `bson:"oauthData,omitempty" json:"-"`                           // Deprecated, for backward compatibility

	// Driver-specific fields
	LicenseNumber    string         `bson:"licenseNumber,omitempty" json:"licenseNumber,omitempty"`
	LicenseExpiry    *time.Time     `bson:"licenseExpiry,omitempty" json:"licenseExpiry,omitempty"`
	DriverCardNumber string         `bson:"driverCardNumber,omitempty" json:"driverCardNumber,omitempty"`
	DriverCardExpiry *time.Time     `bson:"driverCardExpiry,omitempty" json:"driverCardExpiry,omitempty"`
	CQCExpiry        *time.Time     `bson:"cqcExpiry,omitempty" json:"cqcExpiry,omitempty"`
	ADRNumber        string         `bson:"adrNumber,omitempty" json:"adrNumber,omitempty"`
	ADRExpiry        *time.Time     `bson:"adrExpiry,omitempty" json:"adrExpiry,omitempty"`
	TachigrafExpiry  *time.Time     `bson:"tachigrafExpiry,omitempty" json:"tachigrafExpiry,omitempty"`
	MedicalChecks    []MedicalCheck `bson:"medicalChecks,omitempty" json:"medicalChecks,omitempty"`

	// Password authentication (argon2id hash). Never serialized.
	PasswordHash      string     `bson:"passwordHash,omitempty" json:"-"`
	PasswordUpdatedAt *time.Time `bson:"passwordUpdatedAt,omitempty" json:"-"`
	FailedLoginCount  int        `bson:"failedLoginCount,omitempty" json:"-"`
	LockedUntil       *time.Time `bson:"lockedUntil,omitempty" json:"-"`

	// MFAGraceStartedAt is set when a privileged user logs in without an
	// enrolled MFA factor. They have a bounded window from this timestamp
	// to enroll before login begins failing with mfa_enrollment_required.
	// Nil = grace has not begun (and isn't yet relevant for this user).
	MFAGraceStartedAt *time.Time `bson:"mfaGraceStartedAt,omitempty" json:"-"`

	// Status and metadata
	IsActive      bool       `bson:"isActive" json:"isActive"`
	EmailVerified bool       `bson:"emailVerified" json:"emailVerified"`
	LastLogin     *time.Time `bson:"lastLogin,omitempty" json:"lastLogin,omitempty"`
	CreatedAt     time.Time  `bson:"createdAt" json:"createdAt"`
	UpdatedAt     time.Time  `bson:"updatedAt" json:"updatedAt"`
	DeletedAt     *time.Time `bson:"deletedAt,omitempty" json:"-"`
}

// CreateUserInput represents input for creating a new user
type CreateUserInput struct {
	// UUID lets the caller pre-mint the user's UUID so the systeminit
	// first-admin sentinel can be claimed with the same UUID that will
	// end up on the user document. Leave empty for the service to mint a
	// UUIDv7. Never exposed over the JSON API surface — external callers
	// do not get to pick UUIDs.
	UUID             string                 `json:"-"`
	Email            string                 `json:"email" validate:"required,email"`
	Username         string                 `json:"username" validate:"omitempty,min=3,max=50"`
	FullName         string                 `json:"fullName" validate:"required,min=1,max=100"`
	Avatar           string                 `json:"avatar,omitempty"`
	Phone            string                 `json:"phone" validate:"omitempty,e164"`
	PIN              string                 `json:"pin" validate:"omitempty,len=4,numeric"`
	PasswordHash     string                 `json:"-"` // set by auth service, never from external input
	Role             string                 `json:"role" validate:"required,oneof=super_admin administrator developer manager operator guest"`
	OAuthProvider    OAuthProvider          `json:"oauthProvider,omitempty" validate:"omitempty,oneof=google apple discord github"`
	OAuthID          string                 `json:"oauthId,omitempty"`
	OAuthData        map[string]interface{} `json:"oauthData,omitempty"`
	LicenseNumber    string                 `json:"licenseNumber,omitempty"`
	LicenseExpiry    *time.Time             `json:"licenseExpiry,omitempty"`
	DriverCardNumber string                 `json:"driverCardNumber,omitempty"`
	DriverCardExpiry *time.Time             `json:"driverCardExpiry,omitempty"`
	CQCExpiry        *time.Time             `json:"cqcExpiry,omitempty"`
	ADRNumber        string                 `json:"adrNumber,omitempty"`
	ADRExpiry        *time.Time             `json:"adrExpiry,omitempty"`
	TachigrafExpiry  *time.Time             `json:"tachigrafExpiry,omitempty"`
	MedicalChecks    []MedicalCheck         `json:"medicalChecks,omitempty"`
}

// UpdateUserInput represents input for updating a user
type UpdateUserInput struct {
	Email            string         `json:"email,omitempty" validate:"omitempty,email"`
	Username         string         `json:"username,omitempty" validate:"omitempty,min=3,max=50"`
	FullName         string         `json:"fullName,omitempty" validate:"omitempty,min=1,max=100"`
	Avatar           string         `json:"avatar,omitempty"`
	Phone            string         `json:"phone,omitempty" validate:"omitempty,e164"`
	PIN              string         `json:"pin,omitempty" validate:"omitempty,len=4,numeric"`
	Role             string         `json:"role,omitempty" validate:"omitempty,oneof=super_admin administrator developer manager operator guest"`
	LicenseNumber    string         `json:"licenseNumber,omitempty"`
	LicenseExpiry    *time.Time     `json:"licenseExpiry,omitempty"`
	DriverCardNumber string         `json:"driverCardNumber,omitempty"`
	DriverCardExpiry *time.Time     `json:"driverCardExpiry,omitempty"`
	CQCExpiry        *time.Time     `json:"cqcExpiry,omitempty"`
	ADRNumber        string         `json:"adrNumber,omitempty"`
	ADRExpiry        *time.Time     `json:"adrExpiry,omitempty"`
	TachigrafExpiry  *time.Time     `json:"tachigrafExpiry,omitempty"`
	MedicalChecks    []MedicalCheck `json:"medicalChecks,omitempty"`
	IsActive         *bool          `json:"isActive,omitempty"`
}

// UserManagementResponse represents the user data returned in API responses
type UserManagementResponse struct {
	ID               string                  `json:"id"`
	Email            string                  `json:"email"`
	Username         string                  `json:"username"`
	FullName         string                  `json:"fullName"`
	Avatar           string                  `json:"avatar,omitempty"`
	Phone            string                  `json:"phone,omitempty"`
	Role             string                  `json:"role"`
	Providers        []UserOAuthProviderInfo `json:"providers"`
	LicenseNumber    string                  `json:"licenseNumber,omitempty"`
	LicenseExpiry    *time.Time              `json:"licenseExpiry,omitempty"`
	DriverCardNumber string                  `json:"driverCardNumber,omitempty"`
	DriverCardExpiry *time.Time              `json:"driverCardExpiry,omitempty"`
	CQCExpiry        *time.Time              `json:"cqcExpiry,omitempty"`
	ADRNumber        string                  `json:"adrNumber,omitempty"`
	ADRExpiry        *time.Time              `json:"adrExpiry,omitempty"`
	TachigrafExpiry  *time.Time              `json:"tachigrafExpiry,omitempty"`
	MedicalChecks    []MedicalCheck          `json:"medicalChecks,omitempty"`
	IsActive         bool                    `json:"isActive"`
	EmailVerified    bool                    `json:"emailVerified"`
	LastLogin        *time.Time              `json:"lastLogin,omitempty"`
	CreatedAt        time.Time               `json:"createdAt"`
	UpdatedAt        time.Time               `json:"updatedAt"`
}

// UserManagementListResponse represents paginated user list response
type UserManagementListResponse struct {
	Users      []UserManagementResponse `json:"users"`
	Total      int64                    `json:"total"`
	Page       int                      `json:"page"`
	PageSize   int                      `json:"pageSize"`
	TotalPages int                      `json:"totalPages"`
}

// UserFilters represents filters for user queries
type UserFilters struct {
	Role           string `json:"role,omitempty" validate:"omitempty,oneof=super_admin administrator developer manager operator guest"`
	IsActive       *bool  `json:"isActive,omitempty"`
	EmailVerified  *bool  `json:"emailVerified,omitempty"`
	Search         string `json:"search,omitempty"`         // Search in name, email, username
	HasExpiredDocs bool   `json:"hasExpiredDocs,omitempty"` // Filter users with expired documents
}

// PaginationParams represents pagination parameters
type PaginationParams struct {
	Page     int `json:"page" validate:"min=1" default:"1"`
	PageSize int `json:"pageSize" validate:"min=1,max=100" default:"10"`
}

// ToResponse converts User model to UserManagementResponse
func (u *User) ToResponse() *UserManagementResponse {
	return &UserManagementResponse{
		ID:               u.UUID,
		Email:            u.Email,
		Username:         u.Username,
		FullName:         u.FullName,
		Avatar:           u.Avatar,
		Phone:            u.Phone,
		Role:             u.Role,
		Providers:        make([]UserOAuthProviderInfo, 0), // Initialize as empty, will be populated by service
		LicenseNumber:    u.LicenseNumber,
		LicenseExpiry:    u.LicenseExpiry,
		DriverCardNumber: u.DriverCardNumber,
		DriverCardExpiry: u.DriverCardExpiry,
		CQCExpiry:        u.CQCExpiry,
		ADRNumber:        u.ADRNumber,
		ADRExpiry:        u.ADRExpiry,
		TachigrafExpiry:  u.TachigrafExpiry,
		MedicalChecks:    u.MedicalChecks,
		IsActive:         u.IsActive,
		EmailVerified:    u.EmailVerified,
		LastLogin:        u.LastLogin,
		CreatedAt:        u.CreatedAt,
		UpdatedAt:        u.UpdatedAt,
	}
}

// HasExpiredDocuments checks if the user has any expired driver documents
func (u *User) HasExpiredDocuments() bool {
	now := time.Now()

	if u.LicenseExpiry != nil && u.LicenseExpiry.Before(now) {
		return true
	}
	if u.DriverCardExpiry != nil && u.DriverCardExpiry.Before(now) {
		return true
	}
	if u.CQCExpiry != nil && u.CQCExpiry.Before(now) {
		return true
	}
	if u.ADRExpiry != nil && u.ADRExpiry.Before(now) {
		return true
	}
	if u.TachigrafExpiry != nil && u.TachigrafExpiry.Before(now) {
		return true
	}

	return false
}

// GetExpiringSoonDocuments returns documents expiring within the next 30 days
func (u *User) GetExpiringSoonDocuments() []string {
	var expiring []string
	now := time.Now()
	thirtyDaysFromNow := now.AddDate(0, 0, 30)

	if u.LicenseExpiry != nil && u.LicenseExpiry.After(now) && u.LicenseExpiry.Before(thirtyDaysFromNow) {
		expiring = append(expiring, "License")
	}
	if u.DriverCardExpiry != nil && u.DriverCardExpiry.After(now) && u.DriverCardExpiry.Before(thirtyDaysFromNow) {
		expiring = append(expiring, "Driver Card")
	}
	if u.CQCExpiry != nil && u.CQCExpiry.After(now) && u.CQCExpiry.Before(thirtyDaysFromNow) {
		expiring = append(expiring, "CQC")
	}
	if u.ADRExpiry != nil && u.ADRExpiry.After(now) && u.ADRExpiry.Before(thirtyDaysFromNow) {
		expiring = append(expiring, "ADR")
	}
	if u.TachigrafExpiry != nil && u.TachigrafExpiry.After(now) && u.TachigrafExpiry.Before(thirtyDaysFromNow) {
		expiring = append(expiring, "Tachigrafo")
	}

	return expiring
}

// NewUser creates a new user with default values
func NewUser() *User {
	now := time.Now()
	return &User{
		UUID:          GenerateUUIDv7(),
		IsActive:      true,
		EmailVerified: false,
		Role:          "operator",
		CreatedAt:     now,
		UpdatedAt:     now,
		OAuthLinks:    make([]OAuthLink, 0),
		MedicalChecks: make([]MedicalCheck, 0),
	}
}

// GenerateUUIDv7 generates a new UUID v7 (time-ordered)
func GenerateUUIDv7() string {
	return uuid.Must(uuid.NewV7()).String()
}

// GetPrimaryOAuthLink returns the primary OAuth link for the user
func (u *User) GetPrimaryOAuthLink() *OAuthLink {
	for i := range u.OAuthLinks {
		if u.OAuthLinks[i].IsPrimary && u.OAuthLinks[i].IsActive {
			return &u.OAuthLinks[i]
		}
	}
	return nil
}

// AddOAuthLink adds a new OAuth link to the user
func (u *User) AddOAuthLink(provider OAuthProvider, providerID, email string, oauthData map[string]interface{}, isPrimary bool) {
	now := time.Now()

	// If this is the primary link, mark all others as non-primary
	if isPrimary {
		for i := range u.OAuthLinks {
			u.OAuthLinks[i].IsPrimary = false
		}
	}

	oauthLink := OAuthLink{
		Provider:   provider,
		ProviderID: providerID,
		Email:      email,
		LinkedAt:   now,
		IsActive:   true,
		IsPrimary:  isPrimary,
		OAuthData:  oauthData,
		LastUsed:   &now,
	}

	u.OAuthLinks = append(u.OAuthLinks, oauthLink)
	u.UpdatedAt = now
}

// UpdateOAuthLinkUsage updates the last used timestamp for an OAuth link
func (u *User) UpdateOAuthLinkUsage(provider OAuthProvider, providerID string) {
	now := time.Now()
	for i := range u.OAuthLinks {
		if u.OAuthLinks[i].Provider == provider && u.OAuthLinks[i].ProviderID == providerID {
			u.OAuthLinks[i].LastUsed = &now
			u.UpdatedAt = now
			break
		}
	}
}

// RemoveOAuthLink removes an OAuth link from the user
func (u *User) RemoveOAuthLink(provider OAuthProvider, providerID string) error {
	if len(u.OAuthLinks) <= 1 {
		return fmt.Errorf("cannot remove the last OAuth link")
	}

	for i, link := range u.OAuthLinks {
		if link.Provider == provider && link.ProviderID == providerID {
			u.OAuthLinks = append(u.OAuthLinks[:i], u.OAuthLinks[i+1:]...)
			u.UpdatedAt = time.Now()

			// If this was the primary link, make the first remaining link primary
			if link.IsPrimary && len(u.OAuthLinks) > 0 {
				u.OAuthLinks[0].IsPrimary = true
			}
			return nil
		}
	}

	return fmt.Errorf("OAuth link not found")
}

// SetPrimaryOAuthLink sets a specific OAuth link as primary
func (u *User) SetPrimaryOAuthLink(provider OAuthProvider, providerID string) error {
	for i := range u.OAuthLinks {
		if u.OAuthLinks[i].Provider == provider && u.OAuthLinks[i].ProviderID == providerID {
			// Mark all as non-primary first
			for j := range u.OAuthLinks {
				u.OAuthLinks[j].IsPrimary = false
			}
			// Set this one as primary
			u.OAuthLinks[i].IsPrimary = true
			u.UpdatedAt = time.Now()
			return nil
		}
	}
	return fmt.Errorf("OAuth link not found")
}
