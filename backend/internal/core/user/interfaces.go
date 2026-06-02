package user

import (
	"context"

	"github.com/orkestra/backend/pkg/sdk/iface"
	"go.mongodb.org/mongo-driver/bson/primitive"
)

// UserServiceForAuth defines the user service interface for auth module to use
// This interface provides all user-related operations needed by the auth module
type UserServiceForAuth interface {
	// User retrieval operations
	GetUserByID(ctx context.Context, id string) (*iface.User, error)
	GetUserByObjectID(ctx context.Context, id primitive.ObjectID) (*iface.User, error)
	GetUserByEmail(ctx context.Context, email string) (*iface.User, error)
	GetUserByUsername(ctx context.Context, username string) (*iface.User, error)

	// OAuth-specific user operations
	GetUserByOAuthID(ctx context.Context, provider iface.OAuthProvider, oauthID string) (*iface.User, error)
	GetUserByOAuthLink(ctx context.Context, provider iface.OAuthProvider, providerID string) (*iface.User, error)
	CreateUserFromOAuth(ctx context.Context, input *iface.CreateUserInput) (*iface.User, error)

	// OAuth link management
	AddOAuthLinkToUser(ctx context.Context, userUUID string, link iface.OAuthLink) error
	RemoveOAuthLinkFromUser(ctx context.Context, userUUID string, provider iface.OAuthProvider, providerID string) error
	SetPrimaryOAuthLink(ctx context.Context, userUUID string, provider iface.OAuthProvider, providerID string) error
	UpdateOAuthLinkUsage(ctx context.Context, userUUID string, provider iface.OAuthProvider, providerID string) error
	GetUserOAuthLinks(ctx context.Context, userUUID string) ([]iface.OAuthLink, error)

	// User updates
	UpdateUserLastLogin(ctx context.Context, id string) error
	UpdateUserLastLoginByObjectID(ctx context.Context, id primitive.ObjectID) error
	UpdateUserByObjectID(ctx context.Context, id primitive.ObjectID, update *iface.User) error

	// User validation
	ValidateUserExists(ctx context.Context, id string) (bool, error)
	ValidateUserActive(ctx context.Context, id string) (bool, error)

	// User count for first-user detection
	GetUserCount(ctx context.Context, filters *iface.UserFilters) (int64, error)
}
