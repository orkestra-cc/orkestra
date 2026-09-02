package services

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	authModels "github.com/orkestra/backend/internal/core/auth/models"
	authRepository "github.com/orkestra/backend/internal/core/auth/repository"
	"github.com/orkestra/backend/internal/core/user/repository"
	"github.com/orkestra/backend/internal/shared/blob"
	"github.com/orkestra/backend/internal/shared/utils"
	"github.com/orkestra/backend/pkg/sdk/iface"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
)

var (
	// Aliased to the SDK sentinel so consumers outside this module (auth's
	// refresh path) can classify it with errors.Is without importing this
	// package. Same value, same message — every existing return site and every
	// `err == ErrUserNotFound` comparison is unaffected.
	ErrUserNotFound           = iface.ErrUserNotFound
	ErrUserAlreadyExists      = errors.New("user already exists")
	ErrInvalidInput           = errors.New("invalid input")
	ErrUnauthorized           = errors.New("unauthorized operation")
	ErrEmailNotUnique         = errors.New("email address already in use")
	ErrOAuthProviderNotLinked = errors.New("oauth provider is not linked to this user")
	ErrInvalidAvatarSource    = errors.New("invalid avatar source")
)

// UserService defines the interface for user business logic
type UserService interface {
	// Core CRUD operations
	CreateUser(ctx context.Context, input *iface.CreateUserInput) (*iface.UserManagementResponse, error)
	GetUser(ctx context.Context, id string) (*iface.UserManagementResponse, error)
	GetUserByEmail(ctx context.Context, email string) (*iface.UserManagementResponse, error)
	GetUserForAuth(ctx context.Context, email string) (*iface.User, error)
	CreateUserWithPassword(ctx context.Context, input *iface.CreateUserInput) (*iface.User, error)
	UpdatePasswordHash(ctx context.Context, userUUID, hash string) error
	MarkEmailVerified(ctx context.Context, userUUID string) error
	RecordFailedLogin(ctx context.Context, userUUID string, lockUntil *time.Time) error
	ClearFailedLogins(ctx context.Context, userUUID string) error
	UpdateUser(ctx context.Context, id string, input *iface.UpdateUserInput) (*iface.UserManagementResponse, error)
	DeleteUser(ctx context.Context, id string) error
	// SetAvatarSource persists the user's avatar-source preference and the
	// blob storage handle for an uploaded image. Validates that an OAuth-
	// backed source is actually linked on the user. Returns the previously
	// stored objectKey (empty when there was none) so the caller can GC
	// the orphan blob from object storage. ErrUserNotFound when the user
	// is missing; ErrOAuthProviderNotLinked when source is oauth_* and
	// the user has no matching active link.
	SetAvatarSource(ctx context.Context, userUUID, source, objectKey string) (previousObjectKey string, err error)
	// SoftDeleteAndAliasEmail soft-deletes the user and renames the
	// email to a one-shot alias so the unique index no longer collides
	// with a fresh signup using the original address. Used by the
	// tenant-cascade hook for orphaned external owners.
	SoftDeleteAndAliasEmail(ctx context.Context, id string) error

	// Query operations
	ListUsers(ctx context.Context, filters *iface.UserFilters, pagination *iface.PaginationParams) (*iface.UserManagementListResponse, error)
	GetUsersByRole(ctx context.Context, role string) ([]*iface.UserManagementResponse, error)

	// Utility operations
	ValidateUserRole(ctx context.Context, userID string, allowedRoles []string) error
	GetUserCount(ctx context.Context, filters *iface.UserFilters) (int64, error)
	// CountActiveAdministrators returns the number of live (not-deleted)
	// users with isActive=true whose system role is super_admin or
	// administrator, excluding excludeUUID when non-empty. Used by the
	// last-admin guard on delete / deactivate / role-demote so the
	// platform can never be locked out by removing its only operator.
	// Best-effort under concurrent edits — see backend/CLAUDE.md
	// "Error-code contract" `user.last_admin_forbidden`.
	CountActiveAdministrators(ctx context.Context, excludeUUID string) (int64, error)

	// Methods needed by auth module (raw model returns)
	GetUserByID(ctx context.Context, id string) (*iface.User, error)
	GetUserByObjectID(ctx context.Context, id primitive.ObjectID) (*iface.User, error)
	GetUserByUsername(ctx context.Context, username string) (*iface.User, error)
	GetUserByOAuthID(ctx context.Context, provider iface.OAuthProvider, oauthID string) (*iface.User, error)
	GetUserByOAuthLink(ctx context.Context, provider iface.OAuthProvider, providerID string) (*iface.User, error)
	CreateUserFromOAuth(ctx context.Context, input *iface.CreateUserInput) (*iface.User, error)
	AddOAuthLinkToUser(ctx context.Context, userUUID string, link iface.OAuthLink) error
	RemoveOAuthLinkFromUser(ctx context.Context, userUUID string, provider iface.OAuthProvider, providerID string) error
	SetPrimaryOAuthLink(ctx context.Context, userUUID string, provider iface.OAuthProvider, providerID string) error
	UpdateOAuthLinkUsage(ctx context.Context, userUUID string, provider iface.OAuthProvider, providerID string) error
	// UpdateOAuthLinkData refreshes the embedded OAuthLink.OAuthData
	// payload for the matching (provider, providerID). Called from the
	// auth OAuth callback on every link reuse so the cached `picture`
	// URL tracks the user's latest IdP avatar.
	UpdateOAuthLinkData(ctx context.Context, userUUID string, provider iface.OAuthProvider, providerID string, data map[string]interface{}) error
	GetUserOAuthLinks(ctx context.Context, userUUID string) ([]iface.OAuthLink, error)
	UpdateUserLastLogin(ctx context.Context, id string) error
	UpdateUserLastLoginByObjectID(ctx context.Context, id primitive.ObjectID) error
	UpdateUserByObjectID(ctx context.Context, id primitive.ObjectID, update *iface.User) error
	ValidateUserExists(ctx context.Context, id string) (bool, error)
	ValidateUserActive(ctx context.Context, id string) (bool, error)
}

type userService struct {
	userRepo          repository.UserRepository
	oauthProviderRepo authRepository.OAuthProviderRepository
	// blobStore is optional — when wired, uploaded-source avatars are
	// resolved to a fresh presigned GET on every response build. When
	// nil, uploaded avatars fall back to whatever URL is stored on the
	// user document (likely stale or empty).
	blobStore blob.Store
	// auditSink is wired post-construction by the compliance module
	// during its post-Init wiring loop. nil-tolerant: emitAudit is a
	// silent no-op when the compliance addon is disabled.
	auditSink iface.AuditSink
}

// NewUserService creates a new user service
func NewUserService(userRepo repository.UserRepository, oauthProviderRepo authRepository.OAuthProviderRepository) UserService {
	return &userService{
		userRepo:          userRepo,
		oauthProviderRepo: oauthProviderRepo,
	}
}

// SetBlobStore wires the process-wide blob.Store used to mint
// presigned GET URLs for uploaded avatars. Optional — passing nil
// (or never calling this) leaves resolution falling back to the
// stored Avatar field.
func (s *userService) SetBlobStore(store blob.Store) {
	s.blobStore = store
}

// SetAuditSink wires the compliance audit sink post-construction so the
// admin-lifecycle handlers can emit events without importing the
// compliance addon. Satisfies iface.AuditSinkSetter; called from
// compliance's post-Init wiring loop. nil-tolerant — when the
// compliance addon is disabled the handlers' emit helpers no-op.
func (s *userService) SetAuditSink(sink iface.AuditSink) {
	s.auditSink = sink
}

// AuditSink returns the wired compliance audit sink (nil when the
// compliance addon is disabled). Exposed so handlers, not just the
// service's own methods, can decide whether and what to emit — the
// admin lifecycle audit events live on the handlers because that is
// where the actor identity (from ctxauth) is available.
func (s *userService) AuditSink() iface.AuditSink {
	return s.auditSink
}

// CreateUser creates a new user
func (s *userService) CreateUser(ctx context.Context, input *iface.CreateUserInput) (*iface.UserManagementResponse, error) {
	if input == nil {
		return nil, ErrInvalidInput
	}

	// Validate required fields
	if input.Email == "" || input.FullName == "" || input.Role == "" {
		return nil, ErrInvalidInput
	}

	// Normalize email
	input.Email = strings.ToLower(strings.TrimSpace(input.Email))

	// Check if user already exists
	exists, err := s.userRepo.ExistsByEmail(ctx, input.Email)
	if err != nil {
		return nil, fmt.Errorf("failed to check user existence: %w", err)
	}
	if exists {
		return nil, ErrEmailNotUnique
	}

	// Create user model
	user := iface.NewUser()
	user.UUID = uuid.New().String()
	user.Email = input.Email
	user.Username = input.Username
	user.FullName = input.FullName
	user.Avatar = input.Avatar
	user.Phone = input.Phone
	user.Role = input.Role

	// Encrypt PIN if provided
	if input.PIN != "" {
		encryptedPIN, err := s.encryptPIN(input.PIN)
		if err != nil {
			return nil, fmt.Errorf("failed to encrypt PIN: %w", err)
		}
		user.PIN = encryptedPIN
	}

	// Create user in repository
	if err := s.userRepo.Create(ctx, user); err != nil {
		if err == repository.ErrUserAlreadyExists {
			return nil, ErrEmailNotUnique
		}
		return nil, fmt.Errorf("failed to create user: %w", err)
	}

	return user.ToResponse(), nil
}

// GetUser retrieves a user by ID
func (s *userService) GetUser(ctx context.Context, id string) (*iface.UserManagementResponse, error) {
	if id == "" {
		return nil, ErrInvalidInput
	}

	user, err := s.userRepo.GetByID(ctx, id)
	if err != nil {
		if err == repository.ErrUserNotFound {
			return nil, ErrUserNotFound
		}
		return nil, fmt.Errorf("failed to get user: %w", err)
	}

	response := user.ToResponse()

	// Enrich with OAuth providers (includes provider info, email, and avatars)
	if err := s.enrichWithOAuthProviders(ctx, response); err != nil {
		// Log error but don't fail the request
		fmt.Printf("Warning: failed to enrich user %s with OAuth providers: %v\n", id, err)
	}

	return response, nil
}

// GetUserByEmail retrieves a user by email
func (s *userService) GetUserByEmail(ctx context.Context, email string) (*iface.UserManagementResponse, error) {
	if email == "" {
		return nil, ErrInvalidInput
	}

	email = strings.ToLower(strings.TrimSpace(email))
	user, err := s.userRepo.GetByEmail(ctx, email)
	if err != nil {
		if err == repository.ErrUserNotFound {
			return nil, ErrUserNotFound
		}
		return nil, fmt.Errorf("failed to get user by email: %w", err)
	}

	response := user.ToResponse()

	// Enrich with OAuth providers (includes provider info, email, and avatars)
	if err := s.enrichWithOAuthProviders(ctx, response); err != nil {
		// Log error but don't fail the request
		fmt.Printf("Warning: failed to enrich user %s with OAuth providers: %v\n", response.ID, err)
	}

	return response, nil
}

// UpdateUser updates a user
func (s *userService) UpdateUser(ctx context.Context, id string, input *iface.UpdateUserInput) (*iface.UserManagementResponse, error) {
	if id == "" || input == nil {
		return nil, ErrInvalidInput
	}

	// Check if user exists
	existingUser, err := s.userRepo.GetByID(ctx, id)
	if err != nil {
		if err == repository.ErrUserNotFound {
			return nil, ErrUserNotFound
		}
		return nil, fmt.Errorf("failed to get user: %w", err)
	}

	// Check if email is being changed and if it's already taken
	if input.Email != "" && input.Email != existingUser.Email {
		existingByEmail, err := s.userRepo.GetByEmail(ctx, input.Email)
		if err == nil && existingByEmail != nil && existingByEmail.UUID != id {
			return nil, ErrEmailNotUnique
		}
		// If err is ErrUserNotFound, that's good - the email is available
		if err != nil && err != repository.ErrUserNotFound {
			return nil, fmt.Errorf("failed to check email uniqueness: %w", err)
		}
	}

	// Encrypt PIN if provided
	if input.PIN != "" {
		encryptedPIN, err := s.encryptPIN(input.PIN)
		if err != nil {
			return nil, fmt.Errorf("failed to encrypt PIN: %w", err)
		}
		input.PIN = encryptedPIN
	}

	// Update user in repository
	updatedUser, err := s.userRepo.Update(ctx, id, input)
	if err != nil {
		if err == repository.ErrUserNotFound {
			return nil, ErrUserNotFound
		}
		return nil, fmt.Errorf("failed to update user: %w", err)
	}

	response := updatedUser.ToResponse()

	// Enrich with OAuth providers (includes provider info, email, and avatars)
	if err := s.enrichWithOAuthProviders(ctx, response); err != nil {
		// Log error but don't fail the request
		fmt.Printf("Warning: failed to enrich updated user %s with OAuth providers: %v\n", id, err)
	}

	return response, nil
}

// DeleteUser deletes a user
func (s *userService) DeleteUser(ctx context.Context, id string) error {
	if id == "" {
		return ErrInvalidInput
	}

	err := s.userRepo.Delete(ctx, id)
	if err != nil {
		if err == repository.ErrUserNotFound {
			return ErrUserNotFound
		}
		return fmt.Errorf("failed to delete user: %w", err)
	}

	return nil
}

// SoftDeleteAndAliasEmail soft-deletes a user and frees the email by
// rewriting it to an alias. Idempotent: a missing or already-deleted row
// is treated as success (the caller's intent — "this email should no
// longer block signup" — is already satisfied).
func (s *userService) SoftDeleteAndAliasEmail(ctx context.Context, id string) error {
	if id == "" {
		return ErrInvalidInput
	}
	if err := s.userRepo.SoftDeleteAndAliasEmail(ctx, id); err != nil {
		if errors.Is(err, repository.ErrUserNotFound) {
			return nil
		}
		return fmt.Errorf("failed to soft-delete-and-alias user: %w", err)
	}
	return nil
}

// ListUsers retrieves users with filters and pagination
func (s *userService) ListUsers(ctx context.Context, filters *iface.UserFilters, pagination *iface.PaginationParams) (*iface.UserManagementListResponse, error) {
	// Set default pagination if not provided
	if pagination == nil {
		pagination = &iface.PaginationParams{
			Page:     1,
			PageSize: 10,
		}
	}

	// Validate pagination
	if pagination.Page < 1 {
		pagination.Page = 1
	}
	if pagination.PageSize < 1 || pagination.PageSize > 100 {
		pagination.PageSize = 10
	}

	users, total, err := s.userRepo.List(ctx, filters, pagination)
	if err != nil {
		return nil, fmt.Errorf("failed to list users: %w", err)
	}

	// Convert to response format
	userResponses := make([]iface.UserManagementResponse, len(users))
	for i, user := range users {
		userResponses[i] = *user.ToResponse()
	}

	// Enrich with OAuth data (providers and avatars)
	if err := s.enrichMultipleWithOAuthData(ctx, userResponses); err != nil {
		// Log error but don't fail the request
		fmt.Printf("Warning: failed to enrich users with OAuth data: %v\n", err)
	}

	// Calculate total pages
	totalPages := int(total) / pagination.PageSize
	if int(total)%pagination.PageSize > 0 {
		totalPages++
	}

	return &iface.UserManagementListResponse{
		Users:      userResponses,
		Total:      total,
		Page:       pagination.Page,
		PageSize:   pagination.PageSize,
		TotalPages: totalPages,
	}, nil
}

// GetUsersByRole retrieves users by role
func (s *userService) GetUsersByRole(ctx context.Context, role string) ([]*iface.UserManagementResponse, error) {
	if role == "" {
		return nil, ErrInvalidInput
	}

	users, err := s.userRepo.GetByRole(ctx, role)
	if err != nil {
		return nil, fmt.Errorf("failed to get users by role: %w", err)
	}

	responses := make([]*iface.UserManagementResponse, len(users))
	for i, user := range users {
		responses[i] = user.ToResponse()
	}

	// Convert to slice for enrichment
	responseSlice := make([]iface.UserManagementResponse, len(responses))
	for i, response := range responses {
		responseSlice[i] = *response
	}

	// Enrich with OAuth data (providers and avatars)
	if err := s.enrichMultipleWithOAuthData(ctx, responseSlice); err != nil {
		// Log error but don't fail the request
		fmt.Printf("Warning: failed to enrich users by role with OAuth data: %v\n", err)
	}

	// Convert back to pointer slice
	for i := range responses {
		responses[i] = &responseSlice[i]
	}

	return responses, nil
}

// ValidateUserRole checks if a user has one of the allowed roles
func (s *userService) ValidateUserRole(ctx context.Context, userID string, allowedRoles []string) error {
	user, err := s.userRepo.GetByID(ctx, userID)
	if err != nil {
		return err
	}

	for _, role := range allowedRoles {
		if user.Role == role {
			return nil
		}
	}

	return ErrUnauthorized
}

// GetUserCount returns the total count of users with filters
func (s *userService) GetUserCount(ctx context.Context, filters *iface.UserFilters) (int64, error) {
	count, err := s.userRepo.Count(ctx, filters)
	if err != nil {
		return 0, fmt.Errorf("failed to count users: %w", err)
	}
	return count, nil
}

// CountActiveAdministrators counts live, active super_admin/administrator
// rows, optionally excluding excludeUUID. The repo's CountWithFilter adds
// the deletedAt-missing predicate so this call only needs to supply the
// role + isActive + uuid clauses.
func (s *userService) CountActiveAdministrators(ctx context.Context, excludeUUID string) (int64, error) {
	filter := bson.M{
		"role":     bson.M{"$in": []string{"super_admin", "administrator"}},
		"isActive": true,
	}
	if excludeUUID != "" {
		filter["uuid"] = bson.M{"$ne": excludeUUID}
	}
	count, err := s.userRepo.CountWithFilter(ctx, filter)
	if err != nil {
		return 0, fmt.Errorf("count active administrators: %w", err)
	}
	return count, nil
}

// encryptPIN encrypts a PIN using the crypto utility
func (s *userService) encryptPIN(pin string) (string, error) {
	// Use the existing OAuth token encryption utility as it provides AES-GCM encryption
	return utils.EncryptOAuthToken(pin)
}

// decryptPIN decrypts a PIN using the crypto utility
func (s *userService) decryptPIN(encryptedPIN string) (string, error) {
	// Use the existing OAuth token decryption utility
	return utils.DecryptOAuthToken(encryptedPIN)
}

// enrichWithOAuthProviders enriches user response with OAuth providers
// from the auth_oauth_providers collection AND rebuilds response.Avatar
// according to the user's stated AvatarSource preference. Read paths
// always come through here so the URL stamped on the wire is fresh
// (presigned for uploaded, current provider picture for oauth_*) and
// never the stale value the document happens to carry.
func (s *userService) enrichWithOAuthProviders(ctx context.Context, response *iface.UserManagementResponse) error {
	// Fetch OAuth providers for the user from the oauth_providers collection
	oauthProviders, err := s.oauthProviderRepo.GetByUserUUID(ctx, response.ID)
	if err != nil {
		// Don't fail the request if OAuth data isn't available, just log and continue
		fmt.Printf("Warning: failed to fetch OAuth providers for user %s: %v\n", response.ID, err)
		return nil
	}

	// Extract provider info with email and avatar
	providers := make([]iface.UserOAuthProviderInfo, 0, len(oauthProviders))
	var primaryAvatar string

	for _, provider := range oauthProviders {
		var avatarURL string

		// Extract avatar based on provider type
		switch provider.Provider {
		case authModels.OAuthProviderGoogle:
			if picture, ok := provider.Metadata["picture"].(string); ok && picture != "" {
				avatarURL = picture
			}
		case authModels.OAuthProviderDiscord:
			if picture, ok := provider.Metadata["picture"].(string); ok && picture != "" {
				avatarURL = picture
			}
		case authModels.OAuthProviderGitHub:
			if picture, ok := provider.Metadata["picture"].(string); ok && picture != "" {
				avatarURL = picture
			}
		case authModels.OAuthProviderApple:
			// Apple doesn't provide avatar URLs
			avatarURL = ""
		}

		// Create provider info object
		providerInfo := iface.UserOAuthProviderInfo{
			Provider: string(provider.Provider),
			Email:    provider.Email,
			Avatar:   avatarURL,
		}

		providers = append(providers, providerInfo)

		if provider.IsPrimary && avatarURL != "" {
			primaryAvatar = avatarURL
		}
	}

	// Set the providers field
	response.Providers = providers

	// Resolve Avatar from AvatarSource. The repo-loaded user document
	// is the source of truth for AvatarObjectKey + OAuthLinks pictures;
	// fetch it once so blob.ResolveAvatarURL can read both without an
	// extra per-source lookup. On a miss we fall back to the legacy
	// "primary OAuth picture when avatar is empty" behaviour so legacy
	// users without an AvatarSource keep rendering.
	user, err := s.userRepo.GetByID(ctx, response.ID)
	if err == nil && user != nil {
		response.AvatarSource = user.AvatarSource
		resolved := blob.ResolveAvatarURL(ctx, user, s.blobStore)
		// Empty resolved + non-empty source = explicit "no avatar"
		// (initials or unlinked OAuth) — frontend renders initials.
		// Don't fall back to a stale field.
		if user.AvatarSource != "" {
			response.Avatar = resolved
			return nil
		}
		// Legacy user (source unset) — keep the prior fallback.
		if resolved != "" {
			response.Avatar = resolved
			return nil
		}
	}
	if response.Avatar == "" && primaryAvatar != "" {
		response.Avatar = primaryAvatar
	}

	return nil
}

// enrichMultipleWithOAuthData enriches multiple user responses with OAuth providers and avatars
func (s *userService) enrichMultipleWithOAuthData(ctx context.Context, responses []iface.UserManagementResponse) error {
	for i := range responses {
		// Enrich with OAuth providers (includes provider info, email, and avatars)
		if err := s.enrichWithOAuthProviders(ctx, &responses[i]); err != nil {
			// Log error but continue with other users
			fmt.Printf("Warning: failed to enrich OAuth providers for user %s: %v\n", responses[i].ID, err)
		}
	}
	return nil
}

// Methods for UserServiceForAuth interface (used by auth module)

// GetUserByID retrieves a user by UUID (returns raw model for auth)
func (s *userService) GetUserByID(ctx context.Context, id string) (*iface.User, error) {
	if id == "" {
		return nil, ErrInvalidInput
	}

	user, err := s.userRepo.GetByID(ctx, id)
	if err != nil {
		if errors.Is(err, repository.ErrUserNotFound) {
			return nil, ErrUserNotFound
		}
		return nil, fmt.Errorf("failed to get user by ID: %w", err)
	}

	return user, nil
}

// GetUserByObjectID retrieves a user by MongoDB ObjectID (returns raw model for auth)
func (s *userService) GetUserByObjectID(ctx context.Context, id primitive.ObjectID) (*iface.User, error) {
	user, err := s.userRepo.GetByObjectID(ctx, id)
	if err != nil {
		if err == repository.ErrUserNotFound {
			return nil, ErrUserNotFound
		}
		return nil, fmt.Errorf("failed to get user by ObjectID: %w", err)
	}

	return user, nil
}

// GetUserByUsername retrieves a user by username (returns raw model for auth)
func (s *userService) GetUserByUsername(ctx context.Context, username string) (*iface.User, error) {
	if username == "" {
		return nil, ErrInvalidInput
	}

	user, err := s.userRepo.GetByUsername(ctx, username)
	if err != nil {
		if err == repository.ErrUserNotFound {
			return nil, ErrUserNotFound
		}
		return nil, fmt.Errorf("failed to get user by username: %w", err)
	}

	return user, nil
}

// GetUserByOAuthID retrieves a user by OAuth provider and ID
func (s *userService) GetUserByOAuthID(ctx context.Context, provider iface.OAuthProvider, oauthID string) (*iface.User, error) {
	if oauthID == "" {
		return nil, ErrInvalidInput
	}

	user, err := s.userRepo.GetByOAuthID(ctx, provider, oauthID)
	if err != nil {
		if err == repository.ErrUserNotFound {
			return nil, ErrUserNotFound
		}
		return nil, fmt.Errorf("failed to get user by OAuth ID: %w", err)
	}

	return user, nil
}

// GetUserByOAuthLink retrieves a user by OAuth link
func (s *userService) GetUserByOAuthLink(ctx context.Context, provider iface.OAuthProvider, providerID string) (*iface.User, error) {
	if providerID == "" {
		return nil, ErrInvalidInput
	}

	user, err := s.userRepo.GetByOAuthLink(ctx, provider, providerID)
	if err != nil {
		if err == repository.ErrUserNotFound {
			return nil, ErrUserNotFound
		}
		return nil, fmt.Errorf("failed to get user by OAuth link: %w", err)
	}

	return user, nil
}

// CreateUserFromOAuth creates a new user from OAuth data
func (s *userService) CreateUserFromOAuth(ctx context.Context, input *iface.CreateUserInput) (*iface.User, error) {
	if input == nil {
		return nil, ErrInvalidInput
	}

	// Validate required fields
	if input.Email == "" || input.FullName == "" || input.Role == "" {
		return nil, ErrInvalidInput
	}

	// Normalize email
	input.Email = strings.ToLower(strings.TrimSpace(input.Email))

	// Check if user already exists
	exists, err := s.userRepo.ExistsByEmail(ctx, input.Email)
	if err != nil {
		return nil, fmt.Errorf("failed to check user existence: %w", err)
	}
	if exists {
		return nil, ErrEmailNotUnique
	}

	// Create user model
	user := iface.NewUser()
	user.Email = input.Email
	user.Username = input.Username
	user.FullName = input.FullName
	user.Avatar = input.Avatar
	user.Role = input.Role
	// IdP already vouched for the email — propagate so the new account
	// isn't asked to verify what Google/Apple/GitHub/Discord just confirmed.
	user.EmailVerified = input.EmailVerified

	// Add OAuth information if provided
	if input.OAuthProvider != "" && input.OAuthID != "" {
		user.OAuthProvider = input.OAuthProvider
		user.OAuthID = input.OAuthID
		user.OAuthData = input.OAuthData

		// Also add as OAuth link for new multi-provider support
		user.AddOAuthLink(input.OAuthProvider, input.OAuthID, input.Email, input.OAuthData, true)
	}

	// Create user in repository
	if err := s.userRepo.Create(ctx, user); err != nil {
		if err == repository.ErrUserAlreadyExists {
			return nil, ErrEmailNotUnique
		}
		return nil, fmt.Errorf("failed to create user from OAuth: %w", err)
	}

	return user, nil
}

// asUserNotFound maps the repository's OWN not-found value onto the SDK
// sentinel and leaves every other error untouched.
//
// repository.ErrUserNotFound and iface.ErrUserNotFound are two DIFFERENT
// error values that happen to carry the same message, "user not found". The
// lookups on this service (GetUserByID and friends) have always translated;
// the thin delegations below used to pass the repository value straight out,
// which was invisible while consumers classified not-found by comparing
// err.Error() — and became load-bearing the moment auth's handler mappers
// moved to errors.Is(err, iface.ErrUserNotFound). The reachable case is an
// unlink race: a user soft-deleted between AdminUnlinkOAuth's read and its
// $pull, which answered 404 and would otherwise have started answering 500.
//
// Passing nil through returns nil, so a delegation can wrap its return
// directly.
func asUserNotFound(err error) error {
	if errors.Is(err, repository.ErrUserNotFound) {
		return ErrUserNotFound
	}
	return err
}

// AddOAuthLinkToUser adds an OAuth link to a user
func (s *userService) AddOAuthLinkToUser(ctx context.Context, userUUID string, link iface.OAuthLink) error {
	if userUUID == "" {
		return ErrInvalidInput
	}

	return asUserNotFound(s.userRepo.AddOAuthLink(ctx, userUUID, link))
}

// RemoveOAuthLinkFromUser removes an OAuth link from a user
func (s *userService) RemoveOAuthLinkFromUser(ctx context.Context, userUUID string, provider iface.OAuthProvider, providerID string) error {
	if userUUID == "" || providerID == "" {
		return ErrInvalidInput
	}

	return asUserNotFound(s.userRepo.RemoveOAuthLink(ctx, userUUID, provider, providerID))
}

// SetPrimaryOAuthLink sets a specific OAuth link as primary
func (s *userService) SetPrimaryOAuthLink(ctx context.Context, userUUID string, provider iface.OAuthProvider, providerID string) error {
	if userUUID == "" || providerID == "" {
		return ErrInvalidInput
	}

	return asUserNotFound(s.userRepo.SetPrimaryOAuthLink(ctx, userUUID, provider, providerID))
}

// UpdateOAuthLinkUsage updates the last used timestamp for an OAuth link
func (s *userService) UpdateOAuthLinkUsage(ctx context.Context, userUUID string, provider iface.OAuthProvider, providerID string) error {
	if userUUID == "" || providerID == "" {
		return ErrInvalidInput
	}

	return s.userRepo.UpdateOAuthLinkUsage(ctx, userUUID, provider, providerID)
}

// UpdateOAuthLinkData refreshes the embedded OAuth link's OAuthData
// payload — currently used to keep the cached `picture` URL in sync
// with the IdP. Quiet no-op on missing link (legacy accounts).
func (s *userService) UpdateOAuthLinkData(ctx context.Context, userUUID string, provider iface.OAuthProvider, providerID string, data map[string]interface{}) error {
	if userUUID == "" || providerID == "" {
		return ErrInvalidInput
	}
	return s.userRepo.UpdateOAuthLinkData(ctx, userUUID, provider, providerID, data)
}

// GetUserOAuthLinks gets all OAuth links for a user
func (s *userService) GetUserOAuthLinks(ctx context.Context, userUUID string) ([]iface.OAuthLink, error) {
	if userUUID == "" {
		return nil, ErrInvalidInput
	}

	links, err := s.userRepo.GetOAuthLinks(ctx, userUUID)
	if err != nil {
		return nil, asUserNotFound(err)
	}
	return links, nil
}

// UpdateUserLastLogin updates the last login time for a user
func (s *userService) UpdateUserLastLogin(ctx context.Context, id string) error {
	if id == "" {
		return ErrInvalidInput
	}

	return asUserNotFound(s.userRepo.UpdateLastLogin(ctx, id))
}

// UpdateUserLastLoginByObjectID updates the last login time for a user by ObjectID
func (s *userService) UpdateUserLastLoginByObjectID(ctx context.Context, id primitive.ObjectID) error {
	return s.userRepo.UpdateLastLoginByObjectID(ctx, id)
}

// UpdateUserByObjectID updates a user by ObjectID
func (s *userService) UpdateUserByObjectID(ctx context.Context, id primitive.ObjectID, update *iface.User) error {
	if update == nil {
		return ErrInvalidInput
	}

	return s.userRepo.UpdateByObjectID(ctx, id, update)
}

// ValidateUserExists checks if a user exists
func (s *userService) ValidateUserExists(ctx context.Context, id string) (bool, error) {
	if id == "" {
		return false, ErrInvalidInput
	}

	return s.userRepo.ExistsByUUID(ctx, id)
}

// GetUserForAuth returns the raw user model for authentication flows,
// including the password hash and lockout fields. Never use this for
// general user lookups — use GetUserByEmail instead.
func (s *userService) GetUserForAuth(ctx context.Context, email string) (*iface.User, error) {
	if email == "" {
		return nil, ErrInvalidInput
	}
	email = strings.ToLower(strings.TrimSpace(email))
	user, err := s.userRepo.GetByEmail(ctx, email)
	if err != nil {
		if err == repository.ErrUserNotFound {
			return nil, ErrUserNotFound
		}
		return nil, fmt.Errorf("get user for auth: %w", err)
	}
	return user, nil
}

// CreateUserWithPassword creates a new user from a password signup flow.
// The caller must hash the password before calling — this service does
// not hash (that lives in the auth module's password service).
func (s *userService) CreateUserWithPassword(ctx context.Context, input *iface.CreateUserInput) (*iface.User, error) {
	if input == nil {
		return nil, ErrInvalidInput
	}
	if input.Email == "" || input.FullName == "" || input.Role == "" {
		return nil, ErrInvalidInput
	}
	input.Email = strings.ToLower(strings.TrimSpace(input.Email))

	exists, err := s.userRepo.ExistsByEmail(ctx, input.Email)
	if err != nil {
		return nil, fmt.Errorf("check user existence: %w", err)
	}
	if exists {
		return nil, ErrEmailNotUnique
	}

	user := iface.NewUser()
	if input.UUID != "" {
		// Caller pre-minted the UUID (e.g. the auth module claimed the
		// system_init sentinel with this value before calling us).
		user.UUID = input.UUID
	}
	user.Email = input.Email
	user.Username = input.Username
	user.FullName = input.FullName
	user.Avatar = input.Avatar
	user.Phone = input.Phone
	user.Role = input.Role
	user.Kind = input.Kind
	user.PasswordHash = input.PasswordHash
	now := time.Now()
	user.PasswordUpdatedAt = &now

	if err := s.userRepo.Create(ctx, user); err != nil {
		if err == repository.ErrUserAlreadyExists {
			return nil, ErrEmailNotUnique
		}
		return nil, fmt.Errorf("create user with password: %w", err)
	}
	return user, nil
}

// ListUsersByKind satisfies iface.ServiceAccountLister — an optional
// extension consumers type-assert for rather than a UserService interface
// method, mirroring the SetBlobStore/SetAuditSink post-construction wiring
// pattern above. Not exposed as HTTP surface by this task.
func (s *userService) ListUsersByKind(ctx context.Context, kind string) ([]iface.User, error) {
	return s.userRepo.ListByKind(ctx, kind)
}

// UpdatePasswordHash delegates to the repository.
func (s *userService) UpdatePasswordHash(ctx context.Context, userUUID, hash string) error {
	if userUUID == "" || hash == "" {
		return ErrInvalidInput
	}
	return asUserNotFound(s.userRepo.UpdatePasswordHash(ctx, userUUID, hash))
}

// MarkEmailVerified delegates to the repository.
func (s *userService) MarkEmailVerified(ctx context.Context, userUUID string) error {
	if userUUID == "" {
		return ErrInvalidInput
	}
	return asUserNotFound(s.userRepo.MarkEmailVerified(ctx, userUUID))
}

// RecordFailedLogin delegates to the repository.
func (s *userService) RecordFailedLogin(ctx context.Context, userUUID string, lockUntil *time.Time) error {
	if userUUID == "" {
		return ErrInvalidInput
	}
	return s.userRepo.RecordFailedLogin(ctx, userUUID, lockUntil)
}

// ClearFailedLogins delegates to the repository.
func (s *userService) ClearFailedLogins(ctx context.Context, userUUID string) error {
	if userUUID == "" {
		return ErrInvalidInput
	}
	return s.userRepo.ClearFailedLogins(ctx, userUUID)
}

// StartMFAGraceIfUnset stamps the MFA grace timestamp only when it isn't
// already set. Called on first login of a privileged user without a factor,
// and on admin-triggered privilege grants — both paths want to leave an
// existing countdown in place so multiple role changes don't reset it.
func (s *userService) StartMFAGraceIfUnset(ctx context.Context, userUUID string) error {
	if userUUID == "" {
		return ErrInvalidInput
	}
	user, err := s.userRepo.GetByID(ctx, userUUID)
	if err != nil {
		return asUserNotFound(err)
	}
	if user.MFAGraceStartedAt != nil && !user.MFAGraceStartedAt.IsZero() {
		return nil
	}
	return asUserNotFound(s.userRepo.SetMFAGraceStartedAt(ctx, userUUID, time.Now()))
}

// ResetMFAGrace unconditionally overwrites the grace timestamp with now.
// Used by the admin MFA reset endpoint after clearing a user's factor, so
// the target has a fresh enrollment window regardless of prior state.
func (s *userService) ResetMFAGrace(ctx context.Context, userUUID string) error {
	if userUUID == "" {
		return ErrInvalidInput
	}
	return asUserNotFound(s.userRepo.SetMFAGraceStartedAt(ctx, userUUID, time.Now()))
}

// ClearMFAGrace removes the grace stamp after a successful enrollment.
func (s *userService) ClearMFAGrace(ctx context.Context, userUUID string) error {
	if userUUID == "" {
		return ErrInvalidInput
	}
	return s.userRepo.ClearMFAGraceStartedAt(ctx, userUUID)
}

// avatarSourceToProvider maps an oauth_* avatar source to its OAuth
// provider name. Returns ("", false) for non-OAuth sources.
func avatarSourceToProvider(source string) (iface.OAuthProvider, bool) {
	switch source {
	case iface.AvatarSourceOAuthGoogle:
		return iface.OAuthProviderGoogle, true
	case iface.AvatarSourceOAuthApple:
		return iface.OAuthProviderApple, true
	case iface.AvatarSourceOAuthGitHub:
		return iface.OAuthProviderGitHub, true
	case iface.AvatarSourceOAuthDiscord:
		return iface.OAuthProviderDiscord, true
	}
	return "", false
}

// SetAvatarSource validates the requested source against the user's
// current OAuth links and persists the new preference. Returns the
// object key the user had stored *before* the change so the caller can
// schedule the orphan blob for deletion. The caller is responsible for
// authorization — this service only verifies the source/link invariant.
func (s *userService) SetAvatarSource(ctx context.Context, userUUID, source, objectKey string) (string, error) {
	if userUUID == "" || source == "" {
		return "", ErrInvalidInput
	}
	switch source {
	case iface.AvatarSourceInitials, iface.AvatarSourceUploaded,
		iface.AvatarSourceOAuthGoogle, iface.AvatarSourceOAuthApple,
		iface.AvatarSourceOAuthGitHub, iface.AvatarSourceOAuthDiscord:
	default:
		return "", ErrInvalidAvatarSource
	}
	if source != iface.AvatarSourceUploaded && objectKey != "" {
		// Defensive — only uploads carry an object key.
		objectKey = ""
	}
	existing, err := s.userRepo.GetByID(ctx, userUUID)
	if err != nil {
		if errors.Is(err, repository.ErrUserNotFound) {
			return "", ErrUserNotFound
		}
		return "", fmt.Errorf("get user: %w", err)
	}
	// Snapshot the previous object key before the write — the fake-repo
	// path uses pointer aliasing and the real path's UpdateOne could
	// race, so capturing here is the safe behaviour everywhere.
	previousObjectKey := existing.AvatarObjectKey
	if provider, isOAuth := avatarSourceToProvider(source); isOAuth {
		linked := false
		for _, link := range existing.OAuthLinks {
			if link.Provider == provider && link.IsActive {
				linked = true
				break
			}
		}
		if !linked {
			return "", ErrOAuthProviderNotLinked
		}
	}
	if err := s.userRepo.SetAvatarSource(ctx, userUUID, source, objectKey); err != nil {
		if errors.Is(err, repository.ErrUserNotFound) {
			return "", ErrUserNotFound
		}
		return "", fmt.Errorf("set avatar source: %w", err)
	}
	return previousObjectKey, nil
}

// ValidateUserActive checks if a user is active
func (s *userService) ValidateUserActive(ctx context.Context, id string) (bool, error) {
	if id == "" {
		return false, ErrInvalidInput
	}

	user, err := s.userRepo.GetByID(ctx, id)
	if err != nil {
		if err == repository.ErrUserNotFound {
			return false, nil
		}
		return false, fmt.Errorf("failed to validate user active status: %w", err)
	}

	return user.IsActive, nil
}

// UserLifecycleState satisfies iface.UserLifecycleStateProvider. It
// classifies userUUID as active / inactive / deleted / missing using
// GetLifecycleProjection, the one repository lookup that includes
// soft-deleted rows — every other lookup on this service excludes them,
// which is exactly why none of them can tell "no such user" apart from
// "the user was deleted" for the setup finalizer's recovery audit.
//
// A repository failure is returned as an error and the empty string, on
// purpose: it must never be reported as iface.UserLifecycleMissing (or
// any other state), because callers use "missing" as the precondition for
// letting a different operator claim recovery of an in-flight setup — a
// database blip masquerading as "the bound administrator no longer
// exists" would be a security hole, not a glitch.
func (s *userService) UserLifecycleState(ctx context.Context, userUUID string) (iface.UserLifecycleState, error) {
	row, err := s.userRepo.GetLifecycleProjection(ctx, userUUID)
	if err != nil {
		if err == repository.ErrUserNotFound {
			return iface.UserLifecycleMissing, nil
		}
		return "", fmt.Errorf("resolve user lifecycle state: %w", err)
	}
	if row.DeletedAt != nil {
		return iface.UserLifecycleDeleted, nil
	}
	if row.IsActive {
		return iface.UserLifecycleActive, nil
	}
	return iface.UserLifecycleInactive, nil
}

// Compile-time assertion: the canonical user-module service (registered
// under module.ServiceUserService — see module.go) satisfies
// iface.UserLifecycleStateProvider. shared/setup type-asserts against
// this interface rather than the wide iface.UserProvider (Task 5.5).
var _ iface.UserLifecycleStateProvider = (*userService)(nil)
