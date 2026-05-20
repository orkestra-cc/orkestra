package iface

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
)

// User-related DTOs and helper methods. These live in iface (the SDK contract
// layer) so iface.UserProvider's method signatures don't import a
// backend-internal package. internal/core/user/models re-exports the same
// types via Go type aliases for backward compatibility — call sites
// continue to use `models.User` without source changes.
//
// Note: the legacy Mongo `_id` field (was `ID primitive.ObjectID`) was
// dropped during the move to keep iface storage-agnostic. UUID is the
// canonical user identifier across the platform; the bson decoder
// silently discards Mongo's auto-generated _id on read. The orphan
// `*ByObjectID` repo methods that previously used _id had no external
// callers and remain dead/internal API in user/repository.

// TierOperator and TierClient discriminate Tier-1 internal users
// (`operator_users`) from Tier-2 external users (`client_users`) at the
// document level. Stamped by the user constructor and asserted on read by
// per-tier repositories.
const (
	TierOperator = "operator"
	TierClient   = "client"
)

// DefaultLanguage is the BCP-47 code stamped on new users by NewUser and
// used as the backfill value for accounts that predate the language
// field. Adding a new SPA-supported language means adding it to the
// oneof in UpdateUserInput.Language alongside `en` and `it`, not
// changing this default.
const DefaultLanguage = "en"

// OAuthProvider represents the supported OAuth providers.
type OAuthProvider string

const (
	OAuthProviderGoogle  OAuthProvider = "google"
	OAuthProviderApple   OAuthProvider = "apple"
	OAuthProviderDiscord OAuthProvider = "discord"
	OAuthProviderGitHub  OAuthProvider = "github"
)

// UnmarshalJSON implements the json.Unmarshaler interface for OAuthProvider.
func (o *OAuthProvider) UnmarshalJSON(data []byte) error {
	var s string
	if err := json.Unmarshal(data, &s); err != nil {
		return err
	}
	*o = OAuthProvider(s)
	return nil
}

// MarshalJSON implements the json.Marshaler interface for OAuthProvider.
func (o OAuthProvider) MarshalJSON() ([]byte, error) {
	return json.Marshal(string(o))
}

// UserOAuthProviderInfo represents OAuth provider info in user API responses.
type UserOAuthProviderInfo struct {
	Provider string `json:"provider" validate:"required"`
	Email    string `json:"email" validate:"required,email"`
	Avatar   string `json:"avatar,omitempty"`
}

// OAuthLink represents a linked OAuth provider for a user.
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

// User is the unified user model combining identity, profile, OAuth, and
// password-auth bookkeeping. BSON tags are preserved so the existing Mongo
// driver can still serialize the type — the tags are inert metadata
// strings from iface's POV and the iface package does not import bson.
type User struct {
	UUID string `bson:"uuid" json:"id" validate:"required"`
	// Tier discriminates operator (Tier-1 internal) from client (Tier-2
	// external) users. Set by the constructor matching the target
	// collection; checked on read by the tier-aware repositories.
	Tier     string `bson:"tier,omitempty" json:"-"`
	Email    string `bson:"email" json:"email" validate:"required,email"`
	Username string `bson:"username" json:"username"`
	FullName string `bson:"fullName" json:"fullName"`
	Avatar   string `bson:"avatar,omitempty" json:"avatar,omitempty"`
	Phone    string `bson:"phone" json:"phone" validate:"omitempty,e164"`
	PIN      string `bson:"pin,omitempty" json:"-"` // Encrypted, never exposed
	Role     string `bson:"role" json:"role" validate:"required,oneof=super_admin administrator developer manager operator guest"`

	// OAuth fields
	OAuthLinks    []OAuthLink            `bson:"oauthLinks,omitempty" json:"oauthLinks,omitempty"`
	OAuthProvider OAuthProvider          `bson:"oauthProvider,omitempty" json:"oauthProvider,omitempty"` // Deprecated, for backward compatibility
	OAuthID       string                 `bson:"oauthId,omitempty" json:"oauthId,omitempty"`             // Deprecated, for backward compatibility
	OAuthData     map[string]interface{} `bson:"oauthData,omitempty" json:"-"`                           // Deprecated, for backward compatibility

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

	// Language is the user's preferred BCP-47 language tag (e.g. "en",
	// "it"). Read by the frontend on /me to drive react-i18next. Empty
	// on documents that predate the field; the user module backfills
	// DefaultLanguage on Start so the in-memory user always has a value.
	Language string `bson:"language,omitempty" json:"language,omitempty"`
}

// CreateUserInput represents input for creating a new user.
type CreateUserInput struct {
	// UUID lets the caller pre-mint the user's UUID so the systeminit
	// first-admin sentinel can be claimed with the same UUID that will
	// end up on the user document. Leave empty for the service to mint a
	// UUIDv7. Never exposed over the JSON API surface — external callers
	// do not get to pick UUIDs.
	UUID          string                 `json:"-"`
	Email         string                 `json:"email" validate:"required,email"`
	Username      string                 `json:"username" validate:"omitempty,min=3,max=50"`
	FullName      string                 `json:"fullName" validate:"required,min=1,max=100"`
	Avatar        string                 `json:"avatar,omitempty"`
	Phone         string                 `json:"phone" validate:"omitempty,e164"`
	PIN           string                 `json:"pin" validate:"omitempty,len=4,numeric"`
	PasswordHash  string                 `json:"-"` // set by auth service, never from external input
	Role          string                 `json:"role" validate:"required,oneof=super_admin administrator developer manager operator guest"`
	OAuthProvider OAuthProvider          `json:"oauthProvider,omitempty" validate:"omitempty,oneof=google apple discord github"`
	OAuthID       string                 `json:"oauthId,omitempty"`
	OAuthData     map[string]interface{} `json:"oauthData,omitempty"`
}

// UpdateUserInput represents input for updating a user.
type UpdateUserInput struct {
	Email    string `json:"email,omitempty" validate:"omitempty,email"`
	Username string `json:"username,omitempty" validate:"omitempty,min=3,max=50"`
	FullName string `json:"fullName,omitempty" validate:"omitempty,min=1,max=100"`
	Avatar   string `json:"avatar,omitempty"`
	Phone    string `json:"phone,omitempty" validate:"omitempty,e164"`
	PIN      string `json:"pin,omitempty" validate:"omitempty,len=4,numeric"`
	Role     string `json:"role,omitempty" validate:"omitempty,oneof=super_admin administrator developer manager operator guest"`
	IsActive *bool  `json:"isActive,omitempty"`
	// Language is the user's preferred BCP-47 language tag. The oneof
	// list is the source of truth for which languages the SPA ships
	// translations for — extend the validate and Huma enum tags in
	// lockstep with the SPA's locale files when adding a locale.
	Language string `json:"language,omitempty" enum:"en,it" validate:"omitempty,oneof=en it"`
}

// UserManagementResponse represents the user data returned in API responses.
type UserManagementResponse struct {
	ID            string                  `json:"id"`
	Email         string                  `json:"email"`
	Username      string                  `json:"username"`
	FullName      string                  `json:"fullName"`
	Avatar        string                  `json:"avatar,omitempty"`
	Phone         string                  `json:"phone,omitempty"`
	Role          string                  `json:"role"`
	Providers     []UserOAuthProviderInfo `json:"providers"`
	IsActive      bool                    `json:"isActive"`
	EmailVerified bool                    `json:"emailVerified"`
	LastLogin     *time.Time              `json:"lastLogin,omitempty"`
	CreatedAt     time.Time               `json:"createdAt"`
	UpdatedAt     time.Time               `json:"updatedAt"`
	Language      string                  `json:"language,omitempty"`
}

// UserManagementListResponse represents paginated user list response.
type UserManagementListResponse struct {
	Users      []UserManagementResponse `json:"users"`
	Total      int64                    `json:"total"`
	Page       int                      `json:"page"`
	PageSize   int                      `json:"pageSize"`
	TotalPages int                      `json:"totalPages"`
}

// AdminUserMembership is the trimmed tenant-membership row embedded on
// admin user-list responses. Mirrors TenantMembership shape so the admin
// frontend can render a "Tenants" column without a per-row fetch.
type AdminUserMembership struct {
	TenantUUID string   `json:"tenantUUID"`
	TenantName string   `json:"tenantName"`
	TenantSlug string   `json:"tenantSlug,omitempty"`
	TenantKind string   `json:"tenantKind"`
	Roles      []string `json:"roles,omitempty"`
	IsOwner    bool     `json:"isOwner,omitempty"`
}

// AdminClientUserItem is the row shape for the admin "Clients" page — a
// client_users row with its tenant memberships joined in. Self-registered
// users not yet attached to any tenant return an empty Memberships array so
// the frontend can render an "unattached" pill. Providers is populated only
// by the single-user GET path (the detail endpoint enriches with OAuth
// links via UserService).
type AdminClientUserItem struct {
	ID            string                  `json:"id"`
	Email         string                  `json:"email"`
	Username      string                  `json:"username,omitempty"`
	FullName      string                  `json:"fullName,omitempty"`
	Avatar        string                  `json:"avatar,omitempty"`
	Role          string                  `json:"role"`
	IsActive      bool                    `json:"isActive"`
	EmailVerified bool                    `json:"emailVerified"`
	LastLogin     *time.Time              `json:"lastLogin,omitempty"`
	CreatedAt     time.Time               `json:"createdAt"`
	Memberships   []AdminUserMembership   `json:"memberships"`
	Providers     []UserOAuthProviderInfo `json:"providers,omitempty"`
}

// AdminClientUserListResponse is the paginated payload for the admin
// client-users endpoint.
type AdminClientUserListResponse struct {
	Users      []AdminClientUserItem `json:"users"`
	Total      int64                 `json:"total"`
	Page       int                   `json:"page"`
	PageSize   int                   `json:"pageSize"`
	TotalPages int                   `json:"totalPages"`
}

// UserFilters represents filters for user queries.
type UserFilters struct {
	Role          string `json:"role,omitempty" validate:"omitempty,oneof=super_admin administrator developer manager operator guest"`
	IsActive      *bool  `json:"isActive,omitempty"`
	EmailVerified *bool  `json:"emailVerified,omitempty"`
	Search        string `json:"search,omitempty"` // Search in name, email, username
}

// PaginationParams represents pagination parameters.
type PaginationParams struct {
	Page     int `json:"page" validate:"min=1" default:"1"`
	PageSize int `json:"pageSize" validate:"min=1,max=100" default:"10"`
}

// --- Helper methods on User ---

// ToResponse converts User to a scrubbed UserManagementResponse safe for
// API surfaces. Sensitive fields (password hash, MFA grace, OAuth tokens)
// are deliberately omitted; the Providers slice is initialized empty —
// callers populate it from the OAuth provider repository.
func (u *User) ToResponse() *UserManagementResponse {
	return &UserManagementResponse{
		ID:            u.UUID,
		Email:         u.Email,
		Username:      u.Username,
		FullName:      u.FullName,
		Avatar:        u.Avatar,
		Phone:         u.Phone,
		Role:          u.Role,
		Providers:     make([]UserOAuthProviderInfo, 0),
		IsActive:      u.IsActive,
		EmailVerified: u.EmailVerified,
		LastLogin:     u.LastLogin,
		CreatedAt:     u.CreatedAt,
		UpdatedAt:     u.UpdatedAt,
		Language:      u.Language,
	}
}

// NewUser creates a new user with default values.
func NewUser() *User {
	now := time.Now()
	return &User{
		UUID:          GenerateUUIDv7(),
		IsActive:      true,
		EmailVerified: false,
		Role:          "operator",
		Language:      DefaultLanguage,
		CreatedAt:     now,
		UpdatedAt:     now,
		OAuthLinks:    make([]OAuthLink, 0),
	}
}

// GenerateUUIDv7 generates a new UUID v7 (time-ordered).
func GenerateUUIDv7() string {
	return uuid.Must(uuid.NewV7()).String()
}

// GetPrimaryOAuthLink returns the primary OAuth link for the user.
func (u *User) GetPrimaryOAuthLink() *OAuthLink {
	for i := range u.OAuthLinks {
		if u.OAuthLinks[i].IsPrimary && u.OAuthLinks[i].IsActive {
			return &u.OAuthLinks[i]
		}
	}
	return nil
}

// AddOAuthLink adds a new OAuth link to the user.
func (u *User) AddOAuthLink(provider OAuthProvider, providerID, email string, oauthData map[string]interface{}, isPrimary bool) {
	now := time.Now()
	if isPrimary {
		for i := range u.OAuthLinks {
			u.OAuthLinks[i].IsPrimary = false
		}
	}
	link := OAuthLink{
		Provider:   provider,
		ProviderID: providerID,
		Email:      email,
		LinkedAt:   now,
		IsActive:   true,
		IsPrimary:  isPrimary,
		OAuthData:  oauthData,
		LastUsed:   &now,
	}
	u.OAuthLinks = append(u.OAuthLinks, link)
	u.UpdatedAt = now
}

// UpdateOAuthLinkUsage updates the last-used timestamp for an OAuth link.
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

// RemoveOAuthLink removes an OAuth link from the user. Returns an error
// when the link would be the user's last; if the removed link was primary,
// the first remaining link is promoted.
func (u *User) RemoveOAuthLink(provider OAuthProvider, providerID string) error {
	if len(u.OAuthLinks) <= 1 {
		return fmt.Errorf("cannot remove the last OAuth link")
	}
	for i, link := range u.OAuthLinks {
		if link.Provider == provider && link.ProviderID == providerID {
			u.OAuthLinks = append(u.OAuthLinks[:i], u.OAuthLinks[i+1:]...)
			u.UpdatedAt = time.Now()
			if link.IsPrimary && len(u.OAuthLinks) > 0 {
				u.OAuthLinks[0].IsPrimary = true
			}
			return nil
		}
	}
	return fmt.Errorf("OAuth link not found")
}

// SetPrimaryOAuthLink sets a specific OAuth link as primary.
func (u *User) SetPrimaryOAuthLink(provider OAuthProvider, providerID string) error {
	for i := range u.OAuthLinks {
		if u.OAuthLinks[i].Provider == provider && u.OAuthLinks[i].ProviderID == providerID {
			for j := range u.OAuthLinks {
				u.OAuthLinks[j].IsPrimary = false
			}
			u.OAuthLinks[i].IsPrimary = true
			u.UpdatedAt = time.Now()
			return nil
		}
	}
	return fmt.Errorf("OAuth link not found")
}
