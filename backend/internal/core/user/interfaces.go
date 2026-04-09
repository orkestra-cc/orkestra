package user

import (
	"context"

	"github.com/orkestra/backend/internal/core/user/models"
	"go.mongodb.org/mongo-driver/bson/primitive"
)

// UserServiceForAuth defines the user service interface for auth module to use
// This interface provides all user-related operations needed by the auth module
type UserServiceForAuth interface {
	// User retrieval operations
	GetUserByID(ctx context.Context, id string) (*models.User, error)
	GetUserByObjectID(ctx context.Context, id primitive.ObjectID) (*models.User, error)
	GetUserByEmail(ctx context.Context, email string) (*models.User, error)
	GetUserByUsername(ctx context.Context, username string) (*models.User, error)

	// OAuth-specific user operations
	GetUserByOAuthID(ctx context.Context, provider models.OAuthProvider, oauthID string) (*models.User, error)
	GetUserByOAuthLink(ctx context.Context, provider models.OAuthProvider, providerID string) (*models.User, error)
	CreateUserFromOAuth(ctx context.Context, input *models.CreateUserInput) (*models.User, error)

	// OAuth link management
	AddOAuthLinkToUser(ctx context.Context, userUUID string, link models.OAuthLink) error
	RemoveOAuthLinkFromUser(ctx context.Context, userUUID string, provider models.OAuthProvider, providerID string) error
	SetPrimaryOAuthLink(ctx context.Context, userUUID string, provider models.OAuthProvider, providerID string) error
	UpdateOAuthLinkUsage(ctx context.Context, userUUID string, provider models.OAuthProvider, providerID string) error
	GetUserOAuthLinks(ctx context.Context, userUUID string) ([]models.OAuthLink, error)

	// User updates
	UpdateUserLastLogin(ctx context.Context, id string) error
	UpdateUserLastLoginByObjectID(ctx context.Context, id primitive.ObjectID) error
	UpdateUserByObjectID(ctx context.Context, id primitive.ObjectID, update *models.User) error

	// User validation
	ValidateUserExists(ctx context.Context, id string) (bool, error)
	ValidateUserActive(ctx context.Context, id string) (bool, error)

	// User count for first-user detection
	GetUserCount(ctx context.Context, filters *models.UserFilters) (int64, error)
}
