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
	"github.com/orkestra/backend/internal/core/user/models"
	"github.com/orkestra/backend/internal/core/user/repository"
	"github.com/orkestra/backend/internal/shared/blob"
	"github.com/orkestra/backend/internal/shared/utils"
	"go.mongodb.org/mongo-driver/bson/primitive"
)

var (
	ErrUserNotFound           = errors.New("user not found")
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
	CreateUser(ctx context.Context, input *models.CreateUserInput) (*models.UserManagementResponse, error)
	GetUser(ctx context.Context, id string) (*models.UserManagementResponse, error)
	GetUserByEmail(ctx context.Context, email string) (*models.UserManagementResponse, error)
	GetUserForAuth(ctx context.Context, email string) (*models.User, error)
	CreateUserWithPassword(ctx context.Context, input *models.CreateUserInput) (*models.User, error)
	UpdatePasswordHash(ctx context.Context, userUUID, hash string) error
	MarkEmailVerified(ctx context.Context, userUUID string) error
	RecordFailedLogin(ctx context.Context, userUUID string, lockUntil *time.Time) error
	ClearFailedLogins(ctx context.Context, userUUID string) error
	UpdateUser(ctx context.Context, id string, input *models.UpdateUserInput) (*models.UserManagementResponse, error)
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
	ListUsers(ctx context.Context, filters *models.UserFilters, pagination *models.PaginationParams) (*models.UserManagementListResponse, error)
	GetUsersByRole(ctx context.Context, role string) ([]*models.UserManagementResponse, error)

	// Utility operations
	ValidateUserRole(ctx context.Context, userID string, allowedRoles []string) error
	GetUserCount(ctx context.Context, filters *models.UserFilters) (int64, error)

	// Methods needed by auth module (raw model returns)
	GetUserByID(ctx context.Context, id string) (*models.User, error)
	GetUserByObjectID(ctx context.Context, id primitive.ObjectID) (*models.User, error)
	GetUserByUsername(ctx context.Context, username string) (*models.User, error)
	GetUserByOAuthID(ctx context.Context, provider models.OAuthProvider, oauthID string) (*models.User, error)
	GetUserByOAuthLink(ctx context.Context, provider models.OAuthProvider, providerID string) (*models.User, error)
	CreateUserFromOAuth(ctx context.Context, input *models.CreateUserInput) (*models.User, error)
	AddOAuthLinkToUser(ctx context.Context, userUUID string, link models.OAuthLink) error
	RemoveOAuthLinkFromUser(ctx context.Context, userUUID string, provider models.OAuthProvider, providerID string) error
	SetPrimaryOAuthLink(ctx context.Context, userUUID string, provider models.OAuthProvider, providerID string) error
	UpdateOAuthLinkUsage(ctx context.Context, userUUID string, provider models.OAuthProvider, providerID string) error
	// UpdateOAuthLinkData refreshes the embedded OAuthLink.OAuthData
	// payload for the matching (provider, providerID). Called from the
	// auth OAuth callback on every link reuse so the cached `picture`
	// URL tracks the user's latest IdP avatar.
	UpdateOAuthLinkData(ctx context.Context, userUUID string, provider models.OAuthProvider, providerID string, data map[string]interface{}) error
	GetUserOAuthLinks(ctx context.Context, userUUID string) ([]models.OAuthLink, error)
	UpdateUserLastLogin(ctx context.Context, id string) error
	UpdateUserLastLoginByObjectID(ctx context.Context, id primitive.ObjectID) error
	UpdateUserByObjectID(ctx context.Context, id primitive.ObjectID, update *models.User) error
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

// CreateUser creates a new user
func (s *userService) CreateUser(ctx context.Context, input *models.CreateUserInput) (*models.UserManagementResponse, error) {
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
	user := models.NewUser()
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
func (s *userService) GetUser(ctx context.Context, id string) (*models.UserManagementResponse, error) {
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
func (s *userService) GetUserByEmail(ctx context.Context, email string) (*models.UserManagementResponse, error) {
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
func (s *userService) UpdateUser(ctx context.Context, id string, input *models.UpdateUserInput) (*models.UserManagementResponse, error) {
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
func (s *userService) ListUsers(ctx context.Context, filters *models.UserFilters, pagination *models.PaginationParams) (*models.UserManagementListResponse, error) {
	// Set default pagination if not provided
	if pagination == nil {
		pagination = &models.PaginationParams{
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
	userResponses := make([]models.UserManagementResponse, len(users))
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

	return &models.UserManagementListResponse{
		Users:      userResponses,
		Total:      total,
		Page:       pagination.Page,
		PageSize:   pagination.PageSize,
		TotalPages: totalPages,
	}, nil
}

// GetUsersByRole retrieves users by role
func (s *userService) GetUsersByRole(ctx context.Context, role string) ([]*models.UserManagementResponse, error) {
	if role == "" {
		return nil, ErrInvalidInput
	}

	users, err := s.userRepo.GetByRole(ctx, role)
	if err != nil {
		return nil, fmt.Errorf("failed to get users by role: %w", err)
	}

	responses := make([]*models.UserManagementResponse, len(users))
	for i, user := range users {
		responses[i] = user.ToResponse()
	}

	// Convert to slice for enrichment
	responseSlice := make([]models.UserManagementResponse, len(responses))
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
func (s *userService) GetUserCount(ctx context.Context, filters *models.UserFilters) (int64, error) {
	count, err := s.userRepo.Count(ctx, filters)
	if err != nil {
		return 0, fmt.Errorf("failed to count users: %w", err)
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
func (s *userService) enrichWithOAuthProviders(ctx context.Context, response *models.UserManagementResponse) error {
	// Fetch OAuth providers for the user from the oauth_providers collection
	oauthProviders, err := s.oauthProviderRepo.GetByUserUUID(ctx, response.ID)
	if err != nil {
		// Don't fail the request if OAuth data isn't available, just log and continue
		fmt.Printf("Warning: failed to fetch OAuth providers for user %s: %v\n", response.ID, err)
		return nil
	}

	// Extract provider info with email and avatar
	providers := make([]models.UserOAuthProviderInfo, 0, len(oauthProviders))
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
		providerInfo := models.UserOAuthProviderInfo{
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
func (s *userService) enrichMultipleWithOAuthData(ctx context.Context, responses []models.UserManagementResponse) error {
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
func (s *userService) GetUserByID(ctx context.Context, id string) (*models.User, error) {
	if id == "" {
		return nil, ErrInvalidInput
	}

	user, err := s.userRepo.GetByID(ctx, id)
	if err != nil {
		if err == repository.ErrUserNotFound {
			return nil, ErrUserNotFound
		}
		return nil, fmt.Errorf("failed to get user by ID: %w", err)
	}

	return user, nil
}

// GetUserByObjectID retrieves a user by MongoDB ObjectID (returns raw model for auth)
func (s *userService) GetUserByObjectID(ctx context.Context, id primitive.ObjectID) (*models.User, error) {
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
func (s *userService) GetUserByUsername(ctx context.Context, username string) (*models.User, error) {
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
func (s *userService) GetUserByOAuthID(ctx context.Context, provider models.OAuthProvider, oauthID string) (*models.User, error) {
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
func (s *userService) GetUserByOAuthLink(ctx context.Context, provider models.OAuthProvider, providerID string) (*models.User, error) {
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
func (s *userService) CreateUserFromOAuth(ctx context.Context, input *models.CreateUserInput) (*models.User, error) {
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
	user := models.NewUser()
	user.Email = input.Email
	user.Username = input.Username
	user.FullName = input.FullName
	user.Avatar = input.Avatar
	user.Role = input.Role

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

// AddOAuthLinkToUser adds an OAuth link to a user
func (s *userService) AddOAuthLinkToUser(ctx context.Context, userUUID string, link models.OAuthLink) error {
	if userUUID == "" {
		return ErrInvalidInput
	}

	return s.userRepo.AddOAuthLink(ctx, userUUID, link)
}

// RemoveOAuthLinkFromUser removes an OAuth link from a user
func (s *userService) RemoveOAuthLinkFromUser(ctx context.Context, userUUID string, provider models.OAuthProvider, providerID string) error {
	if userUUID == "" || providerID == "" {
		return ErrInvalidInput
	}

	return s.userRepo.RemoveOAuthLink(ctx, userUUID, provider, providerID)
}

// SetPrimaryOAuthLink sets a specific OAuth link as primary
func (s *userService) SetPrimaryOAuthLink(ctx context.Context, userUUID string, provider models.OAuthProvider, providerID string) error {
	if userUUID == "" || providerID == "" {
		return ErrInvalidInput
	}

	return s.userRepo.SetPrimaryOAuthLink(ctx, userUUID, provider, providerID)
}

// UpdateOAuthLinkUsage updates the last used timestamp for an OAuth link
func (s *userService) UpdateOAuthLinkUsage(ctx context.Context, userUUID string, provider models.OAuthProvider, providerID string) error {
	if userUUID == "" || providerID == "" {
		return ErrInvalidInput
	}

	return s.userRepo.UpdateOAuthLinkUsage(ctx, userUUID, provider, providerID)
}

// UpdateOAuthLinkData refreshes the embedded OAuth link's OAuthData
// payload — currently used to keep the cached `picture` URL in sync
// with the IdP. Quiet no-op on missing link (legacy accounts).
func (s *userService) UpdateOAuthLinkData(ctx context.Context, userUUID string, provider models.OAuthProvider, providerID string, data map[string]interface{}) error {
	if userUUID == "" || providerID == "" {
		return ErrInvalidInput
	}
	return s.userRepo.UpdateOAuthLinkData(ctx, userUUID, provider, providerID, data)
}

// GetUserOAuthLinks gets all OAuth links for a user
func (s *userService) GetUserOAuthLinks(ctx context.Context, userUUID string) ([]models.OAuthLink, error) {
	if userUUID == "" {
		return nil, ErrInvalidInput
	}

	return s.userRepo.GetOAuthLinks(ctx, userUUID)
}

// UpdateUserLastLogin updates the last login time for a user
func (s *userService) UpdateUserLastLogin(ctx context.Context, id string) error {
	if id == "" {
		return ErrInvalidInput
	}

	return s.userRepo.UpdateLastLogin(ctx, id)
}

// UpdateUserLastLoginByObjectID updates the last login time for a user by ObjectID
func (s *userService) UpdateUserLastLoginByObjectID(ctx context.Context, id primitive.ObjectID) error {
	return s.userRepo.UpdateLastLoginByObjectID(ctx, id)
}

// UpdateUserByObjectID updates a user by ObjectID
func (s *userService) UpdateUserByObjectID(ctx context.Context, id primitive.ObjectID, update *models.User) error {
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
func (s *userService) GetUserForAuth(ctx context.Context, email string) (*models.User, error) {
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
func (s *userService) CreateUserWithPassword(ctx context.Context, input *models.CreateUserInput) (*models.User, error) {
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

	user := models.NewUser()
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

// UpdatePasswordHash delegates to the repository.
func (s *userService) UpdatePasswordHash(ctx context.Context, userUUID, hash string) error {
	if userUUID == "" || hash == "" {
		return ErrInvalidInput
	}
	return s.userRepo.UpdatePasswordHash(ctx, userUUID, hash)
}

// MarkEmailVerified delegates to the repository.
func (s *userService) MarkEmailVerified(ctx context.Context, userUUID string) error {
	if userUUID == "" {
		return ErrInvalidInput
	}
	return s.userRepo.MarkEmailVerified(ctx, userUUID)
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
		return err
	}
	if user.MFAGraceStartedAt != nil && !user.MFAGraceStartedAt.IsZero() {
		return nil
	}
	return s.userRepo.SetMFAGraceStartedAt(ctx, userUUID, time.Now())
}

// ResetMFAGrace unconditionally overwrites the grace timestamp with now.
// Used by the admin MFA reset endpoint after clearing a user's factor, so
// the target has a fresh enrollment window regardless of prior state.
func (s *userService) ResetMFAGrace(ctx context.Context, userUUID string) error {
	if userUUID == "" {
		return ErrInvalidInput
	}
	return s.userRepo.SetMFAGraceStartedAt(ctx, userUUID, time.Now())
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
func avatarSourceToProvider(source string) (models.OAuthProvider, bool) {
	switch source {
	case models.AvatarSourceOAuthGoogle:
		return models.OAuthProviderGoogle, true
	case models.AvatarSourceOAuthApple:
		return models.OAuthProviderApple, true
	case models.AvatarSourceOAuthGitHub:
		return models.OAuthProviderGitHub, true
	case models.AvatarSourceOAuthDiscord:
		return models.OAuthProviderDiscord, true
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
	case models.AvatarSourceInitials, models.AvatarSourceUploaded,
		models.AvatarSourceOAuthGoogle, models.AvatarSourceOAuthApple,
		models.AvatarSourceOAuthGitHub, models.AvatarSourceOAuthDiscord:
	default:
		return "", ErrInvalidAvatarSource
	}
	if source != models.AvatarSourceUploaded && objectKey != "" {
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
