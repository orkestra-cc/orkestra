package repository

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/orkestra/backend/internal/shared/utils"
	"github.com/orkestra/backend/pkg/sdk/iface"
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
	// ADR-0003 PR-D D-8: tier-split user collections are the only
	// canonical user storage. Operator-tier rows live in
	// operator_users, client-tier rows in client_users.
	OperatorUsersCollection = "operator_users"
	ClientUsersCollection   = "client_users"
)

type UserRepository interface {
	// Core CRUD Operations
	Create(ctx context.Context, user *iface.User) error
	GetByID(ctx context.Context, id string) (*iface.User, error)
	GetByObjectID(ctx context.Context, id primitive.ObjectID) (*iface.User, error)
	GetByEmail(ctx context.Context, email string) (*iface.User, error)
	GetByUsername(ctx context.Context, username string) (*iface.User, error)
	Update(ctx context.Context, id string, input *iface.UpdateUserInput) (*iface.User, error)
	UpdateByObjectID(ctx context.Context, id primitive.ObjectID, update *iface.User) error
	UpdateLastLogin(ctx context.Context, id string) error
	UpdateLastLoginByObjectID(ctx context.Context, id primitive.ObjectID) error
	Delete(ctx context.Context, id string) error
	DeleteByObjectID(ctx context.Context, id primitive.ObjectID) error
	// HardDelete removes the row entirely — used by the GDPR DSR pipeline
	// for right-to-erasure. Distinct from Delete which soft-deletes via a
	// deletedAt stamp (keeps the row for audit + re-activation).
	HardDelete(ctx context.Context, id string) error
	// SoftDeleteAndAliasEmail soft-deletes the row AND renames the email
	// to a one-shot alias so the per-collection unique email index no
	// longer collides with a fresh signup using the original address.
	// Used by the tenant cascade-delete hook: when an external Tier-2
	// tenant is deleted and its owner has no other live memberships,
	// this frees the email so the same human can re-register without
	// hitting "Email already in use". The original email is preserved on
	// originalEmail for audit. Returns ErrUserNotFound if no live row
	// matches.
	SoftDeleteAndAliasEmail(ctx context.Context, id string) error

	// Password-auth operations
	UpdatePasswordHash(ctx context.Context, userUUID, hash string) error
	MarkEmailVerified(ctx context.Context, userUUID string) error
	RecordFailedLogin(ctx context.Context, userUUID string, lockUntil *time.Time) error
	ClearFailedLogins(ctx context.Context, userUUID string) error

	// MFA grace-period operations — used by the auth module when a privileged
	// user first logs in without an enrolled factor (set) or when an admin
	// forces a reset (set again to restart the countdown). Clearing happens
	// on successful enrollment.
	SetMFAGraceStartedAt(ctx context.Context, userUUID string, when time.Time) error
	ClearMFAGraceStartedAt(ctx context.Context, userUUID string) error

	// BumpMFAEpoch increments mfaEpoch and returns the new value. Called
	// by the auth module's MFA/WebAuthn services on every credential
	// removal or replacement (never on addition) — see iface.User.MFAEpoch
	// and iface.MFAEpochBumper for why. Returns ErrUserNotFound if no
	// live row matches.
	BumpMFAEpoch(ctx context.Context, userUUID string) (int, error)

	// ListByKind returns live (not soft-deleted) users whose Kind matches —
	// used by the admin service-account listing surface. kind is compared
	// verbatim; pass iface.UserKindService to list machine principals.
	ListByKind(ctx context.Context, kind string) ([]iface.User, error)

	// UpdateOAuthLinkData replaces the embedded OAuthLink.OAuthData map
	// for a matching (provider, providerID) link on the user's row. Used
	// by the OAuth callback link-reuse path so the cached `picture` URL
	// refreshes on every login — ResolveAvatar reads this map for
	// AvatarSource=oauth_* and the SPA expects a current image.
	UpdateOAuthLinkData(ctx context.Context, userUUID string, provider iface.OAuthProvider, providerID string, data map[string]interface{}) error

	// SetAvatarSource overwrites the user's avatar preference. The caller
	// is responsible for any tier-aware policy (this repo only persists).
	// When source is "initials" the objectKey arg should be empty and any
	// existing avatarObjectKey is unset so a future presigned GET cannot
	// resurrect a stale blob. When source is "uploaded" the caller passes
	// the freshly-committed S3 object key. For oauth_* the objectKey is
	// always empty (the URL is resolved from the matching OAuthLink at
	// read time).
	SetAvatarSource(ctx context.Context, userUUID, source, objectKey string) error

	// OAuth Operations
	GetByOAuthID(ctx context.Context, provider iface.OAuthProvider, oauthID string) (*iface.User, error)
	GetByOAuthLink(ctx context.Context, provider iface.OAuthProvider, providerID string) (*iface.User, error)
	AddOAuthLink(ctx context.Context, userUUID string, link iface.OAuthLink) error
	RemoveOAuthLink(ctx context.Context, userUUID string, provider iface.OAuthProvider, providerID string) error
	SetPrimaryOAuthLink(ctx context.Context, userUUID string, provider iface.OAuthProvider, providerID string) error
	GetOAuthLinks(ctx context.Context, userUUID string) ([]iface.OAuthLink, error)
	UpdateOAuthLinkUsage(ctx context.Context, userUUID string, provider iface.OAuthProvider, providerID string) error

	// Query Operations
	List(ctx context.Context, filters *iface.UserFilters, pagination *iface.PaginationParams) ([]*iface.User, int64, error)
	ListWithOptions(ctx context.Context, filter bson.M, opts ...*options.FindOptions) ([]*iface.User, error)
	GetByRole(ctx context.Context, role string) ([]*iface.User, error)

	// Utility Operations
	Count(ctx context.Context, filters *iface.UserFilters) (int64, error)
	CountWithFilter(ctx context.Context, filter bson.M) (int64, error)
	ExistsByEmail(ctx context.Context, email string) (bool, error)
	ExistsByUUID(ctx context.Context, uuid string) (bool, error)
	ExistsByUsername(ctx context.Context, username string) (bool, error)
	// BackfillDefaultLanguage sets language=DefaultLanguage on every row
	// whose language field is missing or empty. Returns the modified
	// count. Idempotent — subsequent calls find no rows to update.
	// Called once on module Init so pre-language users get a sane value
	// without a release-coupled migration step.
	BackfillDefaultLanguage(ctx context.Context, defaultLanguage string) (int64, error)

	// GetLifecycleProjection resolves the minimal isActive/deletedAt
	// projection for uuid — the ONLY lookup in this repository that
	// INCLUDES soft-deleted rows. Every other Get*/Exists* method filters
	// out deletedAt, which is exactly why none of them can distinguish "no
	// row ever existed" from "the row was soft-deleted": both look like a
	// miss. This method exists for iface.UserLifecycleStateProvider (the
	// setup finalizer's access probe + recovery audit), which needs that
	// distinction and nothing else — the projection carries no other
	// field, so a caller cannot recover profile data through this path.
	// Returns ErrUserNotFound when no row matches at all, deleted or not.
	GetLifecycleProjection(ctx context.Context, uuid string) (*LifecycleRow, error)
}

// LifecycleRow is the narrow isActive/deletedAt projection returned by
// GetLifecycleProjection. Deliberately minimal — see that method's doc.
type LifecycleRow struct {
	IsActive  bool       `bson:"isActive"`
	DeletedAt *time.Time `bson:"deletedAt,omitempty"`
}

type mongoUserRepository struct {
	collection *mongo.Collection
	// tier is the audience this repo binds to ("operator" / "client").
	// Every Create-side write stamps user.Tier so a tier-guard test can
	// assert that operator_users rows always carry Tier="operator" and
	// likewise for clients.
	tier string
}

// NewOperatorUserRepository binds to operator_users and stamps
// Tier="operator" on every Create-side write. Registered under
// module.ServiceOperatorUserProvider and (since ADR-0003 PR-D D-8)
// the canonical module.ServiceUserService.
func NewOperatorUserRepository(db *mongo.Database) UserRepository {
	return &mongoUserRepository{
		collection: db.Collection(OperatorUsersCollection),
		tier:       iface.TierOperator,
	}
}

// NewClientUserRepository binds to client_users and stamps
// Tier="client" on every Create-side write.
func NewClientUserRepository(db *mongo.Database) UserRepository {
	return &mongoUserRepository{
		collection: db.Collection(ClientUsersCollection),
		tier:       iface.TierClient,
	}
}

// Create creates a new user
func (r *mongoUserRepository) Create(ctx context.Context, user *iface.User) error {
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

	// ADR-0003 PR-B: stamp the audience tier on every operator/client
	// row so the integrity test can assert each collection only
	// holds rows of its own tier.
	if r.tier != "" {
		user.Tier = r.tier
	}

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
func (r *mongoUserRepository) GetByID(ctx context.Context, id string) (*iface.User, error) {
	var user iface.User
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
func (r *mongoUserRepository) GetByEmail(ctx context.Context, email string) (*iface.User, error) {
	var user iface.User
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
func (r *mongoUserRepository) Update(ctx context.Context, id string, input *iface.UpdateUserInput) (*iface.User, error) {
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
	if input.IsActive != nil {
		update["$set"].(bson.M)["isActive"] = *input.IsActive
	}
	if input.Language != "" {
		update["$set"].(bson.M)["language"] = input.Language
	}

	filter := bson.M{
		"uuid":      id,
		"deletedAt": bson.M{"$exists": false},
	}

	opts := options.FindOneAndUpdate().SetReturnDocument(options.After)
	var updatedUser iface.User

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

// HardDelete permanently removes the user row. Intended for the GDPR
// DSR right-to-erasure pipeline — the row is personal data and cannot
// simply be soft-deleted. Idempotent: missing rows return ErrUserNotFound
// so callers can tell a no-op from a hit.
func (r *mongoUserRepository) HardDelete(ctx context.Context, id string) error {
	result, err := r.collection.DeleteOne(ctx, bson.M{"uuid": id})
	if err != nil {
		return fmt.Errorf("failed to hard-delete user: %w", err)
	}
	if result.DeletedCount == 0 {
		return ErrUserNotFound
	}
	return nil
}

// SoftDeleteAndAliasEmail soft-deletes a live user row AND rewrites the
// email to a one-shot alias of the form "<original>+deleted-<unix>@orphan.local".
// Both fields are updated atomically so a concurrent read can never see a
// soft-deleted row that still owns the original email. The unique email
// index on this collection is full (not partial) so freeing the email
// requires renaming it — see user/CLAUDE.md "Soft delete only" note.
func (r *mongoUserRepository) SoftDeleteAndAliasEmail(ctx context.Context, id string) error {
	now := time.Now()
	// Read the current email so we can prefix the alias for traceability
	// without leaking the address in cleartext to any other index.
	var existing struct {
		Email string `bson:"email"`
	}
	err := r.collection.FindOne(ctx, bson.M{
		"uuid":      id,
		"deletedAt": bson.M{"$exists": false},
	}).Decode(&existing)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return ErrUserNotFound
	}
	if err != nil {
		return fmt.Errorf("failed to read user before soft-delete-and-alias: %w", err)
	}
	alias := fmt.Sprintf("%s+deleted-%d@orphan.local", existing.Email, now.UnixNano())
	res, updateErr := r.collection.UpdateOne(ctx,
		bson.M{"uuid": id, "deletedAt": bson.M{"$exists": false}},
		bson.M{"$set": bson.M{
			"deletedAt":     now,
			"updatedAt":     now,
			"originalEmail": existing.Email,
			"email":         alias,
		}},
	)
	if updateErr != nil {
		return fmt.Errorf("failed to soft-delete-and-alias user: %w", updateErr)
	}
	if res.MatchedCount == 0 {
		// The row was deleted between the read and the update — treat as
		// already gone, the caller's intent is satisfied.
		return ErrUserNotFound
	}
	return nil
}

// List retrieves users with filters and pagination
func (r *mongoUserRepository) List(ctx context.Context, filters *iface.UserFilters, pagination *iface.PaginationParams) ([]*iface.User, int64, error) {
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

	var users []*iface.User
	for cursor.Next(ctx) {
		var user iface.User
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
func (r *mongoUserRepository) GetByRole(ctx context.Context, role string) ([]*iface.User, error) {
	filter := bson.M{
		"role":      role,
		"deletedAt": bson.M{"$exists": false},
	}

	cursor, err := r.collection.Find(ctx, filter)
	if err != nil {
		return nil, fmt.Errorf("failed to find users by role: %w", err)
	}
	defer cursor.Close(ctx)

	var users []*iface.User
	for cursor.Next(ctx) {
		var user iface.User
		if err := cursor.Decode(&user); err != nil {
			return nil, fmt.Errorf("failed to decode user: %w", err)
		}
		users = append(users, &user)
	}

	return users, nil
}

// ListByKind returns live users matching Kind — the service-account
// listing seam. Machine principals are rare relative to the collection
// as a whole, so an unpaginated scan is fine at expected volumes.
func (r *mongoUserRepository) ListByKind(ctx context.Context, kind string) ([]iface.User, error) {
	//tenantscope:allow system: platform user directory; admin-only read of machine principals
	cur, err := r.collection.Find(ctx, bson.M{"kind": kind, "deletedAt": bson.M{"$exists": false}})
	if err != nil {
		return nil, err
	}
	defer cur.Close(ctx)
	var users []iface.User
	if err := cur.All(ctx, &users); err != nil {
		return nil, err
	}
	return users, nil
}

// Count counts users with filters
func (r *mongoUserRepository) Count(ctx context.Context, filters *iface.UserFilters) (int64, error) {
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

// GetLifecycleProjection resolves the isActive/deletedAt projection for
// uuid. Unlike every other lookup in this file, the filter deliberately
// omits the deletedAt exclusion — see the interface doc comment for why:
// this is the one query that must see soft-deleted rows so the caller can
// distinguish "missing" from "deleted". The projection restricts the
// response to exactly the two fields the caller needs to classify the
// row; no other field is ever returned.
func (r *mongoUserRepository) GetLifecycleProjection(ctx context.Context, uuid string) (*LifecycleRow, error) {
	filter := bson.M{"uuid": uuid}
	opts := options.FindOne().SetProjection(bson.M{"isActive": 1, "deletedAt": 1, "_id": 0})

	var row LifecycleRow
	//tenantscope:allow system: platform user lifecycle probe for setup finalizer recovery
	err := r.collection.FindOne(ctx, filter, opts).Decode(&row)
	if err != nil {
		if err == mongo.ErrNoDocuments {
			return nil, ErrUserNotFound
		}
		return nil, fmt.Errorf("failed to get user lifecycle projection: %w", err)
	}

	return &row, nil
}

// buildFilter builds MongoDB filter from UserFilters
func (r *mongoUserRepository) buildFilter(filters *iface.UserFilters) bson.M {
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

	return filter
}

// GetByObjectID retrieves a user by MongoDB ObjectID
func (r *mongoUserRepository) GetByObjectID(ctx context.Context, id primitive.ObjectID) (*iface.User, error) {
	var user iface.User
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
func (r *mongoUserRepository) GetByUsername(ctx context.Context, username string) (*iface.User, error) {
	var user iface.User
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
func (r *mongoUserRepository) UpdateByObjectID(ctx context.Context, id primitive.ObjectID, update *iface.User) error {
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
func (r *mongoUserRepository) GetByOAuthID(ctx context.Context, provider iface.OAuthProvider, oauthID string) (*iface.User, error) {
	var user iface.User
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
func (r *mongoUserRepository) GetByOAuthLink(ctx context.Context, provider iface.OAuthProvider, providerID string) (*iface.User, error) {
	var user iface.User
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
func (r *mongoUserRepository) AddOAuthLink(ctx context.Context, userUUID string, link iface.OAuthLink) error {
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
func (r *mongoUserRepository) RemoveOAuthLink(ctx context.Context, userUUID string, provider iface.OAuthProvider, providerID string) error {
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
func (r *mongoUserRepository) SetPrimaryOAuthLink(ctx context.Context, userUUID string, provider iface.OAuthProvider, providerID string) error {
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
func (r *mongoUserRepository) GetOAuthLinks(ctx context.Context, userUUID string) ([]iface.OAuthLink, error) {
	user, err := r.GetByID(ctx, userUUID)
	if err != nil {
		return nil, err
	}

	return user.OAuthLinks, nil
}

// UpdateOAuthLinkData replaces the embedded OAuth link's OAuthData
// map (commonly used to refresh `picture` on every callback). Quiet
// no-op when the link isn't found — the caller is the OAuth login
// path which mustn't fail just because a legacy account doesn't have
// the embedded link yet (the parallel auth_oauth_providers write
// covers it).
func (r *mongoUserRepository) UpdateOAuthLinkData(ctx context.Context, userUUID string, provider iface.OAuthProvider, providerID string, data map[string]interface{}) error {
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
			"oauthLinks.$.oauthData": data,
			"updatedAt":              now,
		},
	}
	if _, err := r.collection.UpdateOne(ctx, filter, update); err != nil {
		return fmt.Errorf("failed to update OAuth link data: %w", err)
	}
	return nil
}

// UpdateOAuthLinkUsage updates the last used timestamp for an OAuth link
func (r *mongoUserRepository) UpdateOAuthLinkUsage(ctx context.Context, userUUID string, provider iface.OAuthProvider, providerID string) error {
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
func (r *mongoUserRepository) ListWithOptions(ctx context.Context, filter bson.M, opts ...*options.FindOptions) ([]*iface.User, error) {
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

	var users []*iface.User
	for cursor.Next(ctx) {
		var user iface.User
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

// UpdatePasswordHash stores a new argon2id hash and bumps PasswordUpdatedAt.
func (r *mongoUserRepository) UpdatePasswordHash(ctx context.Context, userUUID, hash string) error {
	now := time.Now()
	filter := bson.M{
		"uuid":      userUUID,
		"deletedAt": bson.M{"$exists": false},
	}
	update := bson.M{
		"$set": bson.M{
			"passwordHash":      hash,
			"passwordUpdatedAt": now,
			"updatedAt":         now,
		},
	}
	result, err := r.collection.UpdateOne(ctx, filter, update)
	if err != nil {
		return fmt.Errorf("update password hash: %w", err)
	}
	if result.MatchedCount == 0 {
		return ErrUserNotFound
	}
	return nil
}

// MarkEmailVerified flips emailVerified to true.
func (r *mongoUserRepository) MarkEmailVerified(ctx context.Context, userUUID string) error {
	now := time.Now()
	filter := bson.M{
		"uuid":      userUUID,
		"deletedAt": bson.M{"$exists": false},
	}
	update := bson.M{
		"$set": bson.M{
			"emailVerified": true,
			"updatedAt":     now,
		},
	}
	result, err := r.collection.UpdateOne(ctx, filter, update)
	if err != nil {
		return fmt.Errorf("mark email verified: %w", err)
	}
	if result.MatchedCount == 0 {
		return ErrUserNotFound
	}
	return nil
}

// RecordFailedLogin increments the failed counter and optionally sets a lockout.
func (r *mongoUserRepository) RecordFailedLogin(ctx context.Context, userUUID string, lockUntil *time.Time) error {
	filter := bson.M{
		"uuid":      userUUID,
		"deletedAt": bson.M{"$exists": false},
	}
	set := bson.M{"updatedAt": time.Now()}
	if lockUntil != nil {
		set["lockedUntil"] = *lockUntil
	}
	update := bson.M{
		"$inc": bson.M{"failedLoginCount": 1},
		"$set": set,
	}
	_, err := r.collection.UpdateOne(ctx, filter, update)
	if err != nil {
		return fmt.Errorf("record failed login: %w", err)
	}
	return nil
}

// ClearFailedLogins resets the counter and removes the lockout.
func (r *mongoUserRepository) ClearFailedLogins(ctx context.Context, userUUID string) error {
	filter := bson.M{
		"uuid":      userUUID,
		"deletedAt": bson.M{"$exists": false},
	}
	update := bson.M{
		"$set":   bson.M{"failedLoginCount": 0, "updatedAt": time.Now()},
		"$unset": bson.M{"lockedUntil": ""},
	}
	_, err := r.collection.UpdateOne(ctx, filter, update)
	if err != nil {
		return fmt.Errorf("clear failed logins: %w", err)
	}
	return nil
}

// SetMFAGraceStartedAt stamps the grace-period clock on the user document.
// The caller is responsible for deciding whether the user is eligible —
// this method only persists. Idempotency is a concern for the service layer
// (StartMFAGraceIfUnset); this repo method unconditionally overwrites.
func (r *mongoUserRepository) SetMFAGraceStartedAt(ctx context.Context, userUUID string, when time.Time) error {
	filter := bson.M{
		"uuid":      userUUID,
		"deletedAt": bson.M{"$exists": false},
	}
	update := bson.M{
		"$set": bson.M{
			"mfaGraceStartedAt": when,
			"updatedAt":         time.Now(),
		},
	}
	result, err := r.collection.UpdateOne(ctx, filter, update)
	if err != nil {
		return fmt.Errorf("set mfa grace: %w", err)
	}
	if result.MatchedCount == 0 {
		return ErrUserNotFound
	}
	return nil
}

// ClearMFAGraceStartedAt removes the grace stamp — called on successful
// enrollment so a future privilege revocation followed by re-grant starts
// a fresh window rather than inheriting the stale one.
func (r *mongoUserRepository) ClearMFAGraceStartedAt(ctx context.Context, userUUID string) error {
	filter := bson.M{
		"uuid":      userUUID,
		"deletedAt": bson.M{"$exists": false},
	}
	update := bson.M{
		"$unset": bson.M{"mfaGraceStartedAt": ""},
		"$set":   bson.M{"updatedAt": time.Now()},
	}
	_, err := r.collection.UpdateOne(ctx, filter, update)
	if err != nil {
		return fmt.Errorf("clear mfa grace: %w", err)
	}
	return nil
}

// BumpMFAEpoch increments mfaEpoch and returns the new value in one
// round trip (FindOneAndUpdate with ReturnDocument: After). A read-then-
// write would let two concurrent removals both write the same value,
// which would leave one of the two removals' tokens still valid.
func (r *mongoUserRepository) BumpMFAEpoch(ctx context.Context, userUUID string) (int, error) {
	filter := bson.M{
		"uuid":      userUUID,
		"deletedAt": bson.M{"$exists": false},
	}
	update := bson.M{
		"$inc": bson.M{"mfaEpoch": 1},
		"$set": bson.M{"updatedAt": time.Now()},
	}
	opts := options.FindOneAndUpdate().
		SetReturnDocument(options.After).
		SetProjection(bson.M{"mfaEpoch": 1})

	var out struct {
		MFAEpoch int `bson:"mfaEpoch"`
	}
	err := r.collection.FindOneAndUpdate(ctx, filter, update, opts).Decode(&out)
	if err != nil {
		if err == mongo.ErrNoDocuments {
			return 0, ErrUserNotFound
		}
		return 0, fmt.Errorf("failed to bump mfa epoch: %w", err)
	}
	return out.MFAEpoch, nil
}

// SetAvatarSource persists the user's avatar source preference and the
// matching blob storage handle. Pass an empty objectKey when source is
// anything other than "uploaded" — the helper $unsets avatarObjectKey
// in that case so a future presigned-URL resolve does not pick up a
// stale blob from a prior upload. ErrUserNotFound when no live row
// matches.
func (r *mongoUserRepository) SetAvatarSource(ctx context.Context, userUUID, source, objectKey string) error {
	filter := bson.M{
		"uuid":      userUUID,
		"deletedAt": bson.M{"$exists": false},
	}
	set := bson.M{
		"avatarSource": source,
		"updatedAt":    time.Now(),
	}
	update := bson.M{}
	if objectKey == "" {
		update["$unset"] = bson.M{"avatarObjectKey": ""}
	} else {
		set["avatarObjectKey"] = objectKey
	}
	update["$set"] = set
	result, err := r.collection.UpdateOne(ctx, filter, update)
	if err != nil {
		return fmt.Errorf("set avatar source: %w", err)
	}
	if result.MatchedCount == 0 {
		return ErrUserNotFound
	}
	return nil
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

// BackfillDefaultLanguage stamps the supplied language on every row
// whose `language` field is missing or empty. Soft-deleted rows are
// updated too so a reactivation does not race with a parallel backfill.
// Idempotent: the next call matches nothing.
func (r *mongoUserRepository) BackfillDefaultLanguage(ctx context.Context, defaultLanguage string) (int64, error) {
	filter := bson.M{
		"$or": []bson.M{
			{"language": bson.M{"$exists": false}},
			{"language": ""},
		},
	}
	update := bson.M{"$set": bson.M{"language": defaultLanguage}}
	//tenantscope:allow user collections (operator_users / client_users) are tier-scoped, not org-scoped — the backfill is a one-shot global migration of the preference field.
	res, err := r.collection.UpdateMany(ctx, filter, update)
	if err != nil {
		return 0, fmt.Errorf("failed to backfill default language: %w", err)
	}
	return res.ModifiedCount, nil
}
