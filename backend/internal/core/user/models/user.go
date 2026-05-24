// Package models re-exports the SDK-canonical User types from pkg/sdk/iface
// so existing call sites continue to use models.User / models.OAuthLink /
// models.UserManagementResponse / etc. unchanged.
//
// Type aliases preserve identity — models.User == iface.User — so methods
// defined on iface.User (NewUser, ToResponse, AddOAuthLink, etc.) are
// callable on values typed as *models.User and vice versa.
//
// The previous owner of these types had a `User.ID primitive.ObjectID`
// field for Mongo's `_id`. It had no callers outside this package's
// repository and was dropped during the move so iface stays free of any
// mongo-driver dependency; UUID is the canonical identifier across every
// surface. The repository's *ByObjectID methods remain in place but their
// type signatures no longer touch the User struct.
package models

import "github.com/orkestra-cc/orkestra-sdk/iface"

// Tier discriminators — re-exported via aliases to avoid two sources of
// truth.
const (
	TierOperator = iface.TierOperator
	TierClient   = iface.TierClient
)

// OAuthProvider re-exports.
const (
	OAuthProviderGoogle  = iface.OAuthProviderGoogle
	OAuthProviderApple   = iface.OAuthProviderApple
	OAuthProviderDiscord = iface.OAuthProviderDiscord
	OAuthProviderGitHub  = iface.OAuthProviderGitHub
)

// AvatarSource re-exports — call sites use models.AvatarSource* but the
// canonical strings live in iface.
const (
	AvatarSourceInitials     = iface.AvatarSourceInitials
	AvatarSourceUploaded     = iface.AvatarSourceUploaded
	AvatarSourceOAuthGoogle  = iface.AvatarSourceOAuthGoogle
	AvatarSourceOAuthApple   = iface.AvatarSourceOAuthApple
	AvatarSourceOAuthGitHub  = iface.AvatarSourceOAuthGitHub
	AvatarSourceOAuthDiscord = iface.AvatarSourceOAuthDiscord
)

// Type aliases — call sites keep using `models.X` but the canonical
// definition lives in iface.
type (
	OAuthProvider               = iface.OAuthProvider
	OAuthLink                   = iface.OAuthLink
	User                        = iface.User
	CreateUserInput             = iface.CreateUserInput
	UpdateUserInput             = iface.UpdateUserInput
	UserOAuthProviderInfo       = iface.UserOAuthProviderInfo
	UserManagementResponse      = iface.UserManagementResponse
	UserManagementListResponse  = iface.UserManagementListResponse
	AdminUserMembership         = iface.AdminUserMembership
	AdminClientUserItem         = iface.AdminClientUserItem
	AdminClientUserListResponse = iface.AdminClientUserListResponse
	UserFilters                 = iface.UserFilters
	PaginationParams            = iface.PaginationParams
)

// NewUser and GenerateUUIDv7 are re-exported as function variables so call
// sites that do `models.NewUser()` keep working. The behaviour lives in
// iface.
var (
	NewUser        = iface.NewUser
	GenerateUUIDv7 = iface.GenerateUUIDv7
)
