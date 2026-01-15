package repository

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/orkestra/backend/internal/shared/utils"
	"github.com/orkestra/backend/internal/user/models"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

var (
	ErrUserNotFound      = errors.New("user not found")
	ErrUserAlreadyExists = errors.New("user already exists")
	ErrInvalidUserID     = errors.New("invalid user ID")
)

const (
	UsersCollection = "users"
)

type UserRepository interface {
	// Core CRUD Operations
	Create(ctx context.Context, user *models.User) error
	GetByID(ctx context.Context, id string) (*models.User, error)
	GetByObjectID(ctx context.Context, id primitive.ObjectID) (*models.User, error)
	GetByEmail(ctx context.Context, email string) (*models.User, error)
	GetByUsername(ctx context.Context, username string) (*models.User, error)
	Update(ctx context.Context, id string, input *models.UpdateUserInput) (*models.User, error)
	UpdateByObjectID(ctx context.Context, id primitive.ObjectID, update *models.User) error
	UpdateLastLogin(ctx context.Context, id string) error
	UpdateLastLoginByObjectID(ctx context.Context, id primitive.ObjectID) error
	Delete(ctx context.Context, id string) error
	DeleteByObjectID(ctx context.Context, id primitive.ObjectID) error

	// OAuth Operations
	GetByOAuthID(ctx context.Context, provider models.OAuthProvider, oauthID string) (*models.User, error)
	GetByOAuthLink(ctx context.Context, provider models.OAuthProvider, providerID string) (*models.User, error)
	AddOAuthLink(ctx context.Context, userUUID string, link models.OAuthLink) error
	RemoveOAuthLink(ctx context.Context, userUUID string, provider models.OAuthProvider, providerID string) error
	SetPrimaryOAuthLink(ctx context.Context, userUUID string, provider models.OAuthProvider, providerID string) error
	GetOAuthLinks(ctx context.Context, userUUID string) ([]models.OAuthLink, error)
	UpdateOAuthLinkUsage(ctx context.Context, userUUID string, provider models.OAuthProvider, providerID string) error

	// Query Operations
	List(ctx context.Context, filters *models.UserFilters, pagination *models.PaginationParams) ([]*models.User, int64, error)
	ListWithOptions(ctx context.Context, filter bson.M, opts ...*options.FindOptions) ([]*models.User, error)
	GetByRole(ctx context.Context, role string) ([]*models.User, error)
	GetUsersWithExpiredDocuments(ctx context.Context) ([]*models.User, error)
	GetUsersWithExpiringSoonDocuments(ctx context.Context, days int) ([]*models.User, error)

	// Utility Operations
	Count(ctx context.Context, filters *models.UserFilters) (int64, error)
	CountWithFilter(ctx context.Context, filter bson.M) (int64, error)
	ExistsByEmail(ctx context.Context, email string) (bool, error)
	ExistsByUUID(ctx context.Context, uuid string) (bool, error)
	ExistsByUsername(ctx context.Context, username string) (bool, error)
}

type mongoUserRepository struct {
	collection *mongo.Collection
}

// NewUserRepository creates a new user repository
func NewUserRepository(db *mongo.Database) UserRepository {
	return &mongoUserRepository{
		collection: db.Collection(UsersCollection),
	}
}

// Create creates a new user
func (r *mongoUserRepository) Create(ctx context.Context, user *models.User) error {
	// Check if user already exists by email
	exists, err := r.ExistsByEmail(ctx, user.Email)
	if err != nil {
		return fmt.Errorf("failed to check user existence: %w", err)
	}
	if exists {
		return ErrUserAlreadyExists
	}

	// Set timestamps
	now := time.Now()
	user.CreatedAt = now
	user.UpdatedAt = now

	_, err = r.collection.InsertOne(ctx, user)
	if err != nil {
		if mongo.IsDuplicateKeyError(err) {
			return ErrUserAlreadyExists
		}
		return fmt.Errorf("failed to create user: %w", err)
	}

	return nil
}

// GetByID retrieves a user by UUID
func (r *mongoUserRepository) GetByID(ctx context.Context, id string) (*models.User, error) {
	var user models.User
	filter := bson.M{
		"uuid":      id,
		"deletedAt": bson.M{"$exists": false},
	}

	err := r.collection.FindOne(ctx, filter).Decode(&user)
	if err != nil {
		if err == mongo.ErrNoDocuments {
			return nil, ErrUserNotFound
		}
		return nil, fmt.Errorf("failed to get user: %w", err)
	}

	return &user, nil
}

// GetByEmail retrieves a user by email
func (r *mongoUserRepository) GetByEmail(ctx context.Context, email string) (*models.User, error) {
	var user models.User
	filter := bson.M{
		"email":     email,
		"deletedAt": bson.M{"$exists": false},
	}

	err := r.collection.FindOne(ctx, filter).Decode(&user)
	if err != nil {
		if err == mongo.ErrNoDocuments {
			return nil, ErrUserNotFound
		}
		return nil, fmt.Errorf("failed to get user by email: %w", err)
	}

	return &user, nil
}

// Update updates a user
func (r *mongoUserRepository) Update(ctx context.Context, id string, input *models.UpdateUserInput) (*models.User, error) {
	// Build update document
	update := bson.M{
		"$set": bson.M{
			"updatedAt": time.Now(),
		},
	}

	if input.Email != "" {
		update["$set"].(bson.M)["email"] = input.Email
	}
	if input.Username != "" {
		update["$set"].(bson.M)["username"] = input.Username
	}
	if input.FullName != "" {
		update["$set"].(bson.M)["fullName"] = input.FullName
	}
	if input.Avatar != "" {
		update["$set"].(bson.M)["avatar"] = input.Avatar
	}
	if input.Phone != "" {
		update["$set"].(bson.M)["phone"] = input.Phone
	}
	if input.PIN != "" {
		update["$set"].(bson.M)["pin"] = input.PIN
	}
	if input.Role != "" {
		update["$set"].(bson.M)["role"] = input.Role
	}
	if input.LicenseNumber != "" {
		update["$set"].(bson.M)["licenseNumber"] = input.LicenseNumber
	}
	if input.LicenseExpiry != nil {
		update["$set"].(bson.M)["licenseExpiry"] = input.LicenseExpiry
	}
	if input.DriverCardNumber != "" {
		update["$set"].(bson.M)["driverCardNumber"] = input.DriverCardNumber
	}
	if input.DriverCardExpiry != nil {
		update["$set"].(bson.M)["driverCardExpiry"] = input.DriverCardExpiry
	}
	if input.CQCExpiry != nil {
		update["$set"].(bson.M)["cqcExpiry"] = input.CQCExpiry
	}
	if input.ADRNumber != "" {
		update["$set"].(bson.M)["adrNumber"] = input.ADRNumber
	}
	if input.ADRExpiry != nil {
		update["$set"].(bson.M)["adrExpiry"] = input.ADRExpiry
	}
	if input.TachigrafExpiry != nil {
		update["$set"].(bson.M)["tachigrafExpiry"] = input.TachigrafExpiry
	}
	if input.MedicalChecks != nil {
		update["$set"].(bson.M)["medicalChecks"] = input.MedicalChecks
	}
	if input.IsActive != nil {
		update["$set"].(bson.M)["isActive"] = *input.IsActive
	}

	filter := bson.M{
		"uuid":      id,
		"deletedAt": bson.M{"$exists": false},
	}

	opts := options.FindOneAndUpdate().SetReturnDocument(options.After)
	var updatedUser models.User

	err := r.collection.FindOneAndUpdate(ctx, filter, update, opts).Decode(&updatedUser)
	if err != nil {
		if err == mongo.ErrNoDocuments {
			return nil, ErrUserNotFound
		}
		return nil, fmt.Errorf("failed to update user: %w", err)
	}

	return &updatedUser, nil
}

// Delete soft deletes a user
func (r *mongoUserRepository) Delete(ctx context.Context, id string) error {
	filter := bson.M{
		"uuid":      id,
		"deletedAt": bson.M{"$exists": false},
	}

	update := bson.M{
		"$set": bson.M{
			"deletedAt": time.Now(),
			"updatedAt": time.Now(),
		},
	}

	result, err := r.collection.UpdateOne(ctx, filter, update)
	if err != nil {
		return fmt.Errorf("failed to delete user: %w", err)
	}

	if result.MatchedCount == 0 {
		return ErrUserNotFound
	}

	return nil
}

// List retrieves users with filters and pagination
func (r *mongoUserRepository) List(ctx context.Context, filters *models.UserFilters, pagination *models.PaginationParams) ([]*models.User, int64, error) {
	// Build filter
	filter := r.buildFilter(filters)

	// Count total documents
	total, err := r.collection.CountDocuments(ctx, filter)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to count users: %w", err)
	}

	// Build options
	opts := options.Find()
	if pagination != nil {
		skip := int64((pagination.Page - 1) * pagination.PageSize)
		limit := int64(pagination.PageSize)
		opts.SetSkip(skip).SetLimit(limit)
	}

	// Add sorting by createdAt desc
	opts.SetSort(bson.M{"createdAt": -1})

	cursor, err := r.collection.Find(ctx, filter, opts)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to find users: %w", err)
	}
	defer cursor.Close(ctx)

	var users []*models.User
	for cursor.Next(ctx) {
		var user models.User
		if err := cursor.Decode(&user); err != nil {
			return nil, 0, fmt.Errorf("failed to decode user: %w", err)
		}
		users = append(users, &user)
	}

	if err := cursor.Err(); err != nil {
		return nil, 0, fmt.Errorf("cursor error: %w", err)
	}

	return users, total, nil
}

// GetByRole retrieves users by role
func (r *mongoUserRepository) GetByRole(ctx context.Context, role string) ([]*models.User, error) {
	filter := bson.M{
		"role":      role,
		"deletedAt": bson.M{"$exists": false},
	}

	cursor, err := r.collection.Find(ctx, filter)
	if err != nil {
		return nil, fmt.Errorf("failed to find users by role: %w", err)
	}
	defer cursor.Close(ctx)

	var users []*models.User
	for cursor.Next(ctx) {
		var user models.User
		if err := cursor.Decode(&user); err != nil {
			return nil, fmt.Errorf("failed to decode user: %w", err)
		}
		users = append(users, &user)
	}

	return users, nil
}

// GetUsersWithExpiredDocuments retrieves users with expired documents
func (r *mongoUserRepository) GetUsersWithExpiredDocuments(ctx context.Context) ([]*models.User, error) {
	now := time.Now()
	filter := bson.M{
		"deletedAt": bson.M{"$exists": false},
		"$or": []bson.M{
			{"licenseExpiry": bson.M{"$lt": now}},
			{"driverCardExpiry": bson.M{"$lt": now}},
			{"cqcExpiry": bson.M{"$lt": now}},
			{"adrExpiry": bson.M{"$lt": now}},
			{"tachigrafExpiry": bson.M{"$lt": now}},
		},
	}

	cursor, err := r.collection.Find(ctx, filter)
	if err != nil {
		return nil, fmt.Errorf("failed to find users with expired documents: %w", err)
	}
	defer cursor.Close(ctx)

	var users []*models.User
	for cursor.Next(ctx) {
		var user models.User
		if err := cursor.Decode(&user); err != nil {
			return nil, fmt.Errorf("failed to decode user: %w", err)
		}
		users = append(users, &user)
	}

	return users, nil
}

// GetUsersWithExpiringSoonDocuments retrieves users with documents expiring soon
func (r *mongoUserRepository) GetUsersWithExpiringSoonDocuments(ctx context.Context, days int) ([]*models.User, error) {
	now := time.Now()
	futureDate := now.AddDate(0, 0, days)

	filter := bson.M{
		"deletedAt": bson.M{"$exists": false},
		"$or": []bson.M{
			{"licenseExpiry": bson.M{"$gte": now, "$lte": futureDate}},
			{"driverCardExpiry": bson.M{"$gte": now, "$lte": futureDate}},
			{"cqcExpiry": bson.M{"$gte": now, "$lte": futureDate}},
			{"adrExpiry": bson.M{"$gte": now, "$lte": futureDate}},
			{"tachigrafExpiry": bson.M{"$gte": now, "$lte": futureDate}},
		},
	}

	cursor, err := r.collection.Find(ctx, filter)
	if err != nil {
		return nil, fmt.Errorf("failed to find users with expiring documents: %w", err)
	}
	defer cursor.Close(ctx)

	var users []*models.User
	for cursor.Next(ctx) {
		var user models.User
		if err := cursor.Decode(&user); err != nil {
			return nil, fmt.Errorf("failed to decode user: %w", err)
		}
		users = append(users, &user)
	}

	return users, nil
}

// Count counts users with filters
func (r *mongoUserRepository) Count(ctx context.Context, filters *models.UserFilters) (int64, error) {
	filter := r.buildFilter(filters)
	count, err := r.collection.CountDocuments(ctx, filter)
	if err != nil {
		return 0, fmt.Errorf("failed to count users: %w", err)
	}
	return count, nil
}

// ExistsByEmail checks if a user exists by email
func (r *mongoUserRepository) ExistsByEmail(ctx context.Context, email string) (bool, error) {
	filter := bson.M{
		"email":     email,
		"deletedAt": bson.M{"$exists": false},
	}

	count, err := r.collection.CountDocuments(ctx, filter)
	if err != nil {
		return false, fmt.Errorf("failed to check user existence by email: %w", err)
	}

	return count > 0, nil
}

// ExistsByUUID checks if a user exists by UUID
func (r *mongoUserRepository) ExistsByUUID(ctx context.Context, uuid string) (bool, error) {
	filter := bson.M{
		"uuid":      uuid,
		"deletedAt": bson.M{"$exists": false},
	}

	count, err := r.collection.CountDocuments(ctx, filter)
	if err != nil {
		return false, fmt.Errorf("failed to check user existence by UUID: %w", err)
	}

	return count > 0, nil
}

// buildFilter builds MongoDB filter from UserFilters
func (r *mongoUserRepository) buildFilter(filters *models.UserFilters) bson.M {
	filter := bson.M{
		"deletedAt": bson.M{"$exists": false},
	}

	if filters == nil {
		return filter
	}

	if filters.Role != "" {
		filter["role"] = filters.Role
	}

	if filters.IsActive != nil {
		filter["isActive"] = *filters.IsActive
	}

	if filters.EmailVerified != nil {
		filter["emailVerified"] = *filters.EmailVerified
	}

	if filters.Search != "" {
		// Escape regex metacharacters to prevent ReDoS attacks
		escapedSearch := utils.EscapeRegex(filters.Search)
		searchRegex := primitive.Regex{Pattern: escapedSearch, Options: "i"}
		filter["$or"] = []bson.M{
			{"fullName": searchRegex},
			{"email": searchRegex},
			{"username": searchRegex},
		}
	}

	if filters.HasExpiredDocs {
		now := time.Now()
		filter["$or"] = []bson.M{
			{"licenseExpiry": bson.M{"$lt": now}},
			{"driverCardExpiry": bson.M{"$lt": now}},
			{"cqcExpiry": bson.M{"$lt": now}},
			{"adrExpiry": bson.M{"$lt": now}},
			{"tachigrafExpiry": bson.M{"$lt": now}},
		}
	}

	return filter
}

// GetByObjectID retrieves a user by MongoDB ObjectID
func (r *mongoUserRepository) GetByObjectID(ctx context.Context, id primitive.ObjectID) (*models.User, error) {
	var user models.User
	filter := bson.M{
		"_id":       id,
		"deletedAt": bson.M{"$exists": false},
	}

	err := r.collection.FindOne(ctx, filter).Decode(&user)
	if err != nil {
		if err == mongo.ErrNoDocuments {
			return nil, ErrUserNotFound
		}
		return nil, fmt.Errorf("failed to get user by ObjectID: %w", err)
	}

	return &user, nil
}

// GetByUsername retrieves a user by username
func (r *mongoUserRepository) GetByUsername(ctx context.Context, username string) (*models.User, error) {
	var user models.User
	filter := bson.M{
		"username":  username,
		"deletedAt": bson.M{"$exists": false},
	}

	err := r.collection.FindOne(ctx, filter).Decode(&user)
	if err != nil {
		if err == mongo.ErrNoDocuments {
			return nil, ErrUserNotFound
		}
		return nil, fmt.Errorf("failed to get user by username: %w", err)
	}

	return &user, nil
}

// UpdateByObjectID updates a user by ObjectID
func (r *mongoUserRepository) UpdateByObjectID(ctx context.Context, id primitive.ObjectID, update *models.User) error {
	filter := bson.M{
		"_id":       id,
		"deletedAt": bson.M{"$exists": false},
	}

	updateDoc := bson.M{
		"$set": bson.M{
			"updatedAt": time.Now(),
		},
	}

	// Add all non-empty fields to the update document
	if update.Email != "" {
		updateDoc["$set"].(bson.M)["email"] = update.Email
	}
	if update.Username != "" {
		updateDoc["$set"].(bson.M)["username"] = update.Username
	}
	if update.FullName != "" {
		updateDoc["$set"].(bson.M)["fullName"] = update.FullName
	}
	if update.Role != "" {
		updateDoc["$set"].(bson.M)["role"] = update.Role
	}
	if update.Avatar != "" {
		updateDoc["$set"].(bson.M)["avatar"] = update.Avatar
	}
	if len(update.OAuthLinks) > 0 {
		updateDoc["$set"].(bson.M)["oauthLinks"] = update.OAuthLinks
	}

	result, err := r.collection.UpdateOne(ctx, filter, updateDoc)
	if err != nil {
		return fmt.Errorf("failed to update user by ObjectID: %w", err)
	}

	if result.MatchedCount == 0 {
		return ErrUserNotFound
	}

	return nil
}

// UpdateLastLogin updates the last login time for a user by UUID
func (r *mongoUserRepository) UpdateLastLogin(ctx context.Context, id string) error {
	filter := bson.M{
		"uuid":      id,
		"deletedAt": bson.M{"$exists": false},
	}

	update := bson.M{
		"$set": bson.M{
			"lastLogin": time.Now(),
			"updatedAt": time.Now(),
		},
	}

	result, err := r.collection.UpdateOne(ctx, filter, update)
	if err != nil {
		return fmt.Errorf("failed to update last login: %w", err)
	}

	if result.MatchedCount == 0 {
		return ErrUserNotFound
	}

	return nil
}

// UpdateLastLoginByObjectID updates the last login time for a user by ObjectID
func (r *mongoUserRepository) UpdateLastLoginByObjectID(ctx context.Context, id primitive.ObjectID) error {
	filter := bson.M{
		"_id":       id,
		"deletedAt": bson.M{"$exists": false},
	}

	update := bson.M{
		"$set": bson.M{
			"lastLogin": time.Now(),
			"updatedAt": time.Now(),
		},
	}

	result, err := r.collection.UpdateOne(ctx, filter, update)
	if err != nil {
		return fmt.Errorf("failed to update last login by ObjectID: %w", err)
	}

	if result.MatchedCount == 0 {
		return ErrUserNotFound
	}

	return nil
}

// DeleteByObjectID soft deletes a user by ObjectID
func (r *mongoUserRepository) DeleteByObjectID(ctx context.Context, id primitive.ObjectID) error {
	filter := bson.M{
		"_id":       id,
		"deletedAt": bson.M{"$exists": false},
	}

	update := bson.M{
		"$set": bson.M{
			"deletedAt": time.Now(),
			"updatedAt": time.Now(),
		},
	}

	result, err := r.collection.UpdateOne(ctx, filter, update)
	if err != nil {
		return fmt.Errorf("failed to delete user by ObjectID: %w", err)
	}

	if result.MatchedCount == 0 {
		return ErrUserNotFound
	}

	return nil
}

// GetByOAuthID retrieves a user by OAuth provider and ID (legacy method)
func (r *mongoUserRepository) GetByOAuthID(ctx context.Context, provider models.OAuthProvider, oauthID string) (*models.User, error) {
	var user models.User
	filter := bson.M{
		"oauthProvider": provider,
		"oauthId":       oauthID,
		"deletedAt":     bson.M{"$exists": false},
	}

	err := r.collection.FindOne(ctx, filter).Decode(&user)
	if err != nil {
		if err == mongo.ErrNoDocuments {
			return nil, ErrUserNotFound
		}
		return nil, fmt.Errorf("failed to get user by OAuth ID: %w", err)
	}

	return &user, nil
}

// GetByOAuthLink retrieves a user by OAuth link
func (r *mongoUserRepository) GetByOAuthLink(ctx context.Context, provider models.OAuthProvider, providerID string) (*models.User, error) {
	var user models.User
	filter := bson.M{
		"oauthLinks": bson.M{
			"$elemMatch": bson.M{
				"provider":   provider,
				"providerId": providerID,
				"isActive":   true,
			},
		},
		"deletedAt": bson.M{"$exists": false},
	}

	err := r.collection.FindOne(ctx, filter).Decode(&user)
	if err != nil {
		if err == mongo.ErrNoDocuments {
			return nil, ErrUserNotFound
		}
		return nil, fmt.Errorf("failed to get user by OAuth link: %w", err)
	}

	return &user, nil
}

// AddOAuthLink adds a new OAuth link to a user
func (r *mongoUserRepository) AddOAuthLink(ctx context.Context, userUUID string, link models.OAuthLink) error {
	filter := bson.M{
		"uuid":      userUUID,
		"deletedAt": bson.M{"$exists": false},
	}

	// If this is the primary link, set all others to non-primary
	update := bson.M{}
	if link.IsPrimary {
		// First, set all existing links to non-primary
		update["$set"] = bson.M{
			"oauthLinks.$[].isPrimary": false,
			"updatedAt":                time.Now(),
		}
		if _, err := r.collection.UpdateOne(ctx, filter, update); err != nil {
			return fmt.Errorf("failed to update primary status: %w", err)
		}
	}

	// Then add the new link
	update = bson.M{
		"$push": bson.M{"oauthLinks": link},
		"$set":  bson.M{"updatedAt": time.Now()},
	}

	result, err := r.collection.UpdateOne(ctx, filter, update)
	if err != nil {
		return fmt.Errorf("failed to add OAuth link: %w", err)
	}

	if result.MatchedCount == 0 {
		return ErrUserNotFound
	}

	return nil
}

// RemoveOAuthLink removes an OAuth link from a user
func (r *mongoUserRepository) RemoveOAuthLink(ctx context.Context, userUUID string, provider models.OAuthProvider, providerID string) error {
	filter := bson.M{
		"uuid":      userUUID,
		"deletedAt": bson.M{"$exists": false},
	}

	update := bson.M{
		"$pull": bson.M{
			"oauthLinks": bson.M{
				"provider":   provider,
				"providerId": providerID,
			},
		},
		"$set": bson.M{"updatedAt": time.Now()},
	}

	result, err := r.collection.UpdateOne(ctx, filter, update)
	if err != nil {
		return fmt.Errorf("failed to remove OAuth link: %w", err)
	}

	if result.MatchedCount == 0 {
		return ErrUserNotFound
	}

	return nil
}

// SetPrimaryOAuthLink sets a specific OAuth link as primary
func (r *mongoUserRepository) SetPrimaryOAuthLink(ctx context.Context, userUUID string, provider models.OAuthProvider, providerID string) error {
	filter := bson.M{
		"uuid":      userUUID,
		"deletedAt": bson.M{"$exists": false},
	}

	// First, set all links to non-primary
	update := bson.M{
		"$set": bson.M{
			"oauthLinks.$[].isPrimary": false,
			"updatedAt":                time.Now(),
		},
	}

	if _, err := r.collection.UpdateOne(ctx, filter, update); err != nil {
		return fmt.Errorf("failed to reset primary status: %w", err)
	}

	// Then set the specific link as primary
	filter["oauthLinks"] = bson.M{
		"$elemMatch": bson.M{
			"provider":   provider,
			"providerId": providerID,
		},
	}

	update = bson.M{
		"$set": bson.M{
			"oauthLinks.$.isPrimary": true,
			"updatedAt":              time.Now(),
		},
	}

	result, err := r.collection.UpdateOne(ctx, filter, update)
	if err != nil {
		return fmt.Errorf("failed to set primary OAuth link: %w", err)
	}

	if result.MatchedCount == 0 {
		return ErrUserNotFound
	}

	return nil
}

// GetOAuthLinks gets all OAuth links for a user
func (r *mongoUserRepository) GetOAuthLinks(ctx context.Context, userUUID string) ([]models.OAuthLink, error) {
	user, err := r.GetByID(ctx, userUUID)
	if err != nil {
		return nil, err
	}

	return user.OAuthLinks, nil
}

// UpdateOAuthLinkUsage updates the last used timestamp for an OAuth link
func (r *mongoUserRepository) UpdateOAuthLinkUsage(ctx context.Context, userUUID string, provider models.OAuthProvider, providerID string) error {
	filter := bson.M{
		"uuid": userUUID,
		"oauthLinks": bson.M{
			"$elemMatch": bson.M{
				"provider":   provider,
				"providerId": providerID,
			},
		},
		"deletedAt": bson.M{"$exists": false},
	}

	now := time.Now()
	update := bson.M{
		"$set": bson.M{
			"oauthLinks.$.lastUsed": now,
			"updatedAt":             now,
		},
	}

	result, err := r.collection.UpdateOne(ctx, filter, update)
	if err != nil {
		return fmt.Errorf("failed to update OAuth link usage: %w", err)
	}

	if result.MatchedCount == 0 {
		return ErrUserNotFound
	}

	return nil
}

// ListWithOptions retrieves users with custom filters and options
func (r *mongoUserRepository) ListWithOptions(ctx context.Context, filter bson.M, opts ...*options.FindOptions) ([]*models.User, error) {
	// Ensure deletedAt filter
	if filter == nil {
		filter = bson.M{}
	}
	filter["deletedAt"] = bson.M{"$exists": false}

	cursor, err := r.collection.Find(ctx, filter, opts...)
	if err != nil {
		return nil, fmt.Errorf("failed to find users: %w", err)
	}
	defer cursor.Close(ctx)

	var users []*models.User
	for cursor.Next(ctx) {
		var user models.User
		if err := cursor.Decode(&user); err != nil {
			return nil, fmt.Errorf("failed to decode user: %w", err)
		}
		users = append(users, &user)
	}

	if err := cursor.Err(); err != nil {
		return nil, fmt.Errorf("cursor error: %w", err)
	}

	return users, nil
}

// CountWithFilter counts users with a custom filter
func (r *mongoUserRepository) CountWithFilter(ctx context.Context, filter bson.M) (int64, error) {
	// Ensure deletedAt filter
	if filter == nil {
		filter = bson.M{}
	}
	filter["deletedAt"] = bson.M{"$exists": false}

	count, err := r.collection.CountDocuments(ctx, filter)
	if err != nil {
		return 0, fmt.Errorf("failed to count users: %w", err)
	}

	return count, nil
}

// ExistsByUsername checks if a user exists by username
func (r *mongoUserRepository) ExistsByUsername(ctx context.Context, username string) (bool, error) {
	filter := bson.M{
		"username":  username,
		"deletedAt": bson.M{"$exists": false},
	}

	count, err := r.collection.CountDocuments(ctx, filter)
	if err != nil {
		return false, fmt.Errorf("failed to check user existence by username: %w", err)
	}

	return count > 0, nil
}
