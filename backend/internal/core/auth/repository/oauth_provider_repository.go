package repository

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/orkestra/backend/internal/core/auth/models"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// ErrOAuthIdentityDuplicate wraps the unique-index violation on
// (provider, providerId). Under that index a duplicate means one thing:
// this identity is already recorded, and the caller must re-read to find
// out whose it is.
var ErrOAuthIdentityDuplicate = errors.New("oauth identity already recorded")

// ErrOAuthProviderAlreadyLinked wraps the unique-index violation on
// (userUuid, provider): this user already has an identity of this
// provider — a different one. Not an ownership question.
var ErrOAuthProviderAlreadyLinked = errors.New("oauth provider already linked to this user")

// duplicateKeySentinel maps a Mongo duplicate-key error to the sentinel of
// the index it violated. The provider collections carry two unique
// indexes: (provider, providerId) — the identity — and (userUuid,
// provider) — one identity per provider per user. Only the first is an
// ownership question; the second is "already linked". An unrecognised
// index falls back to the identity sentinel: the caller's re-read then
// decides, and a miss there is a refusal, never a silent continue.
func duplicateKeySentinel(err error) error {
	if err != nil && strings.Contains(err.Error(), "userUuid") {
		return ErrOAuthProviderAlreadyLinked
	}
	return ErrOAuthIdentityDuplicate
}

// OAuthProviderRepository handles OAuth provider data operations
type OAuthProviderRepository interface {
	// Create and link OAuth provider
	CreateOAuthProvider(ctx context.Context, provider *models.OAuthProviderDoc) error
	LinkOAuthProvider(ctx context.Context, userUUID string, link *models.OAuthLink) error

	// Find providers
	GetByProviderAndID(ctx context.Context, provider models.OAuthProvider, providerID string) (*models.OAuthProviderDoc, error)
	// GetByProviderAndIDIncludingUnlinked also returns a tombstoned row
	// (UnlinkedAt != nil) — for callers that must tell "unlinked" apart
	// from "never seen".
	GetByProviderAndIDIncludingUnlinked(ctx context.Context, provider models.OAuthProvider, providerID string) (*models.OAuthProviderDoc, error)
	GetByUserUUID(ctx context.Context, userUUID string) ([]*models.OAuthProviderDoc, error)
	GetPrimaryProvider(ctx context.Context, userUUID string) (*models.OAuthProviderDoc, error)

	// Update operations
	UpdateLastUsed(ctx context.Context, uuid string) error
	SetPrimaryProvider(ctx context.Context, userUUID string, provider models.OAuthProvider) error
	UpdateRefreshToken(ctx context.Context, uuid string, refreshToken string) error
	UpdateOAuthTokens(ctx context.Context, uuid string, accessToken, refreshToken string, accessTokenExpiresAt, refreshTokenExpiresAt *time.Time, scopes []string) error
	// UpdateMetadata replaces the document's metadata map. Used on
	// OAuth callback link-reuse so the cached `picture` URL refreshes
	// every time the user signs in via the same provider (the IdP may
	// have rotated their avatar between sessions).
	UpdateMetadata(ctx context.Context, uuid string, metadata map[string]interface{}) error

	// Unlink operations
	UnlinkProvider(ctx context.Context, userUUID string, provider models.OAuthProvider) error
	DeleteProvider(ctx context.Context, uuid string) error

	// Account consolidation
	FindByEmail(ctx context.Context, email string) ([]*models.OAuthProviderDoc, error)
	ConsolidateProviders(ctx context.Context, fromUserUUID, toUserUUID string) error
}

type oauthProviderRepository struct {
	collection *mongo.Collection
	// tier — see authSessionRepository.tier (ADR-0003 PR-D).
	tier string
}

// NewOperatorOAuthProviderRepository binds to operator_oauth_providers
// and stamps Tier="operator" on every CreateOAuthProvider write.
// ADR-0003 PR-D.
func NewOperatorOAuthProviderRepository(db *mongo.Database) OAuthProviderRepository {
	return &oauthProviderRepository{
		collection: db.Collection(models.OperatorOAuthProvidersCollection),
		tier:       models.TierOperator,
	}
}

// NewClientOAuthProviderRepository binds to client_oauth_providers and
// stamps Tier="client" on every CreateOAuthProvider write. ADR-0003 PR-D.
func NewClientOAuthProviderRepository(db *mongo.Database) OAuthProviderRepository {
	return &oauthProviderRepository{
		collection: db.Collection(models.ClientOAuthProvidersCollection),
		tier:       models.TierClient,
	}
}

func (r *oauthProviderRepository) CreateOAuthProvider(ctx context.Context, provider *models.OAuthProviderDoc) error {
	// Set timestamps and UUID if not provided
	now := time.Now()
	if provider.UUID == "" {
		provider.UUID = models.GenerateTimeOrderedUUID()
	}
	provider.CreatedAt = now
	provider.UpdatedAt = now
	// ADR-0003 PR-D: stamp the audience tier — see authSessionRepository.
	if r.tier != "" {
		provider.Tier = r.tier
	}

	// The unique (provider, providerId) index decides ownership — not a
	// read-then-insert, which was the race the index exists to close. A
	// duplicate key is surfaced as ErrOAuthIdentityDuplicate so the
	// service can re-read and find out whose identity it is.
	if _, err := r.collection.InsertOne(ctx, provider); err != nil {
		if mongo.IsDuplicateKeyError(err) {
			return fmt.Errorf("%w: %v", duplicateKeySentinel(err), err)
		}
		return fmt.Errorf("failed to create OAuth provider: %w", err)
	}

	return nil
}

func (r *oauthProviderRepository) LinkOAuthProvider(ctx context.Context, userUUID string, link *models.OAuthLink) error {
	// Convert OAuthLink to OAuthProviderDoc
	provider := &models.OAuthProviderDoc{
		UUID:       models.GenerateTimeOrderedUUID(),
		UserUUID:   userUUID,
		Provider:   link.Provider,
		ProviderID: link.ProviderID,
		Email:      link.Email,
		IsPrimary:  link.IsPrimary,
		LinkedAt:   link.LinkedAt,
		LastUsed:   link.LastUsed,
	}

	return r.CreateOAuthProvider(ctx, provider)
}

// GetByProviderAndID resolves an ACTIVE identity. A tombstoned document
// (UnlinkedAt != nil) is deliberately invisible here: every caller of
// this method is asking "who owns this identity right now", and an
// unlinked identity is owned by nobody.
//
// Use GetByProviderAndIDIncludingUnlinked when the caller needs to tell
// "unlinked" apart from "never seen" — the callback does, so it can
// answer oauth_identity_unlinked instead of starting a signup.
func (r *oauthProviderRepository) GetByProviderAndID(ctx context.Context, provider models.OAuthProvider, providerID string) (*models.OAuthProviderDoc, error) {
	return r.findIdentity(ctx, provider, providerID, false)
}

func (r *oauthProviderRepository) GetByProviderAndIDIncludingUnlinked(ctx context.Context, provider models.OAuthProvider, providerID string) (*models.OAuthProviderDoc, error) {
	return r.findIdentity(ctx, provider, providerID, true)
}

func (r *oauthProviderRepository) findIdentity(ctx context.Context, provider models.OAuthProvider, providerID string, includeUnlinked bool) (*models.OAuthProviderDoc, error) {
	filter := bson.M{
		"provider":   provider,
		"providerId": providerID,
	}
	if !includeUnlinked {
		filter["unlinkedAt"] = bson.M{"$exists": false}
	}

	var result models.OAuthProviderDoc
	//tenantscope:allow OAuth identities are audience-tier scoped, not org scoped; this repository is bound to one tier collection.
	err := r.collection.FindOne(ctx, filter).Decode(&result)
	if err != nil {
		if err == mongo.ErrNoDocuments {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to find OAuth provider: %w", err)
	}

	return &result, nil
}

// GetByUserUUID lists what the user HAS: tombstoned rows are excluded.
func (r *oauthProviderRepository) GetByUserUUID(ctx context.Context, userUUID string) ([]*models.OAuthProviderDoc, error) {
	filter := bson.M{"userUuid": userUUID, "unlinkedAt": bson.M{"$exists": false}}

	// Sort by isPrimary desc, then by linkedAt desc
	opts := options.Find().SetSort(bson.D{
		{Key: "isPrimary", Value: -1},
		{Key: "linkedAt", Value: -1},
	})

	//tenantscope:allow OAuth identities are audience-tier scoped, not org scoped; this repository is bound to one tier collection.
	cursor, err := r.collection.Find(ctx, filter, opts)
	if err != nil {
		return nil, fmt.Errorf("failed to find OAuth providers: %w", err)
	}
	defer cursor.Close(ctx)

	var providers []*models.OAuthProviderDoc
	for cursor.Next(ctx) {
		var provider models.OAuthProviderDoc
		if err := cursor.Decode(&provider); err != nil {
			return nil, fmt.Errorf("failed to decode OAuth provider: %w", err)
		}
		providers = append(providers, &provider)
	}

	if err := cursor.Err(); err != nil {
		return nil, fmt.Errorf("cursor error: %w", err)
	}

	return providers, nil
}

func (r *oauthProviderRepository) GetPrimaryProvider(ctx context.Context, userUUID string) (*models.OAuthProviderDoc, error) {
	filter := bson.M{
		"userUuid":  userUUID,
		"isPrimary": true,
	}

	var result models.OAuthProviderDoc
	//tenantscope:allow OAuth identities are audience-tier scoped, not org scoped; this repository is bound to one tier collection.
	err := r.collection.FindOne(ctx, filter).Decode(&result)
	if err != nil {
		if err == mongo.ErrNoDocuments {
			// If no primary provider, return the first linked provider
			providers, err := r.GetByUserUUID(ctx, userUUID)
			if err != nil || len(providers) == 0 {
				return nil, fmt.Errorf("no OAuth providers found for user")
			}
			return providers[0], nil
		}
		return nil, fmt.Errorf("failed to find primary OAuth provider: %w", err)
	}

	return &result, nil
}

func (r *oauthProviderRepository) UpdateLastUsed(ctx context.Context, uuid string) error {
	filter := bson.M{"uuid": uuid}
	now := time.Now()
	update := bson.M{
		"$set": bson.M{
			"lastUsed":  now,
			"updatedAt": now,
		},
	}

	//tenantscope:allow OAuth identities are audience-tier scoped, not org scoped; this repository is bound to one tier collection.
	result, err := r.collection.UpdateOne(ctx, filter, update)
	if err != nil {
		return fmt.Errorf("failed to update last used: %w", err)
	}

	if result.MatchedCount == 0 {
		return fmt.Errorf("OAuth provider not found")
	}

	return nil
}

// UpdateMetadata replaces the metadata document on the provider row.
// Used by the OAuth callback link-reuse path so the cached `picture`
// URL refreshes on every login — the IdP may have rotated the user's
// avatar between sessions and the embedded `User.OAuthLinks` ought to
// reflect the current value.
func (r *oauthProviderRepository) UpdateMetadata(ctx context.Context, uuid string, metadata map[string]interface{}) error {
	if uuid == "" {
		return fmt.Errorf("uuid is required")
	}
	filter := bson.M{"uuid": uuid}
	update := bson.M{
		"$set": bson.M{
			"metadata":  metadata,
			"updatedAt": time.Now(),
		},
	}
	//tenantscope:allow OAuth identities are audience-tier scoped, not org scoped; this repository is bound to one tier collection.
	result, err := r.collection.UpdateOne(ctx, filter, update)
	if err != nil {
		return fmt.Errorf("failed to update metadata: %w", err)
	}
	if result.MatchedCount == 0 {
		return fmt.Errorf("OAuth provider not found")
	}
	return nil
}

func (r *oauthProviderRepository) SetPrimaryProvider(ctx context.Context, userUUID string, provider models.OAuthProvider) error {
	// Start a transaction to ensure atomicity
	session, err := r.collection.Database().Client().StartSession()
	if err != nil {
		return fmt.Errorf("failed to start session: %w", err)
	}
	defer session.EndSession(ctx)

	_, err = session.WithTransaction(ctx, func(sc mongo.SessionContext) (interface{}, error) {
		// First, unset all primary flags for this user
		unsetFilter := bson.M{"userUuid": userUUID}
		unsetUpdate := bson.M{
			"$set": bson.M{
				"isPrimary": false,
				"updatedAt": time.Now(),
			},
		}

		//tenantscope:allow OAuth identities are audience-tier scoped, not org scoped; this repository is bound to one tier collection.
		_, err := r.collection.UpdateMany(sc, unsetFilter, unsetUpdate)
		if err != nil {
			return nil, fmt.Errorf("failed to unset primary flags: %w", err)
		}

		// Then, set the new primary provider
		setPrimaryFilter := bson.M{
			"userUuid": userUUID,
			"provider": provider,
		}
		setPrimaryUpdate := bson.M{
			"$set": bson.M{
				"isPrimary": true,
				"updatedAt": time.Now(),
			},
		}

		//tenantscope:allow OAuth identities are audience-tier scoped, not org scoped; this repository is bound to one tier collection.
		result, err := r.collection.UpdateOne(sc, setPrimaryFilter, setPrimaryUpdate)
		if err != nil {
			return nil, fmt.Errorf("failed to set primary provider: %w", err)
		}

		if result.MatchedCount == 0 {
			return nil, fmt.Errorf("provider not found for user")
		}

		return nil, nil
	})

	return err
}

func (r *oauthProviderRepository) UpdateRefreshToken(ctx context.Context, uuid string, refreshToken string) error {
	filter := bson.M{"uuid": uuid}
	update := bson.M{
		"$set": bson.M{
			"refreshToken": refreshToken, // Should be encrypted
			"updatedAt":    time.Now(),
		},
	}

	//tenantscope:allow OAuth identities are audience-tier scoped, not org scoped; this repository is bound to one tier collection.
	result, err := r.collection.UpdateOne(ctx, filter, update)
	if err != nil {
		return fmt.Errorf("failed to update refresh token: %w", err)
	}

	if result.MatchedCount == 0 {
		return fmt.Errorf("OAuth provider not found")
	}

	return nil
}

func (r *oauthProviderRepository) UpdateOAuthTokens(ctx context.Context, uuid string, accessToken, refreshToken string, accessTokenExpiresAt, refreshTokenExpiresAt *time.Time, scopes []string) error {
	filter := bson.M{"uuid": uuid}
	updateFields := bson.M{
		"tokenStatus":      "active",
		"lastTokenRefresh": time.Now(),
		"updatedAt":        time.Now(),
	}

	// Add tokens if provided
	if accessToken != "" {
		updateFields["accessToken"] = accessToken
		if accessTokenExpiresAt != nil {
			updateFields["accessTokenExpiresAt"] = *accessTokenExpiresAt
		}
	}

	if refreshToken != "" {
		updateFields["refreshToken"] = refreshToken
		if refreshTokenExpiresAt != nil {
			updateFields["refreshTokenExpiresAt"] = *refreshTokenExpiresAt
		}
	}

	if len(scopes) > 0 {
		updateFields["scopes"] = scopes
	}

	update := bson.M{"$set": updateFields}

	//tenantscope:allow OAuth identities are audience-tier scoped, not org scoped; this repository is bound to one tier collection.
	result, err := r.collection.UpdateOne(ctx, filter, update)
	if err != nil {
		return fmt.Errorf("failed to update OAuth tokens: %w", err)
	}

	if result.MatchedCount == 0 {
		return fmt.Errorf("OAuth provider not found")
	}

	return nil
}

func (r *oauthProviderRepository) UnlinkProvider(ctx context.Context, userUUID string, provider models.OAuthProvider) error {
	// Check if this is the only provider for the user
	userProviders, err := r.GetByUserUUID(ctx, userUUID)
	if err != nil {
		return fmt.Errorf("failed to check user providers: %w", err)
	}

	if len(userProviders) <= 1 {
		return fmt.Errorf("cannot unlink the last OAuth provider")
	}

	// Delete the provider
	filter := bson.M{
		"userUuid": userUUID,
		"provider": provider,
	}

	//tenantscope:allow OAuth identities are audience-tier scoped, not org scoped; this repository is bound to one tier collection.
	result, err := r.collection.DeleteOne(ctx, filter)
	if err != nil {
		return fmt.Errorf("failed to unlink provider: %w", err)
	}

	if result.DeletedCount == 0 {
		return fmt.Errorf("provider not found")
	}

	// If we removed the primary provider, set another one as primary
	remainingProviders, err := r.GetByUserUUID(ctx, userUUID)
	if err != nil {
		return fmt.Errorf("failed to get remaining providers: %w", err)
	}

	hasPrimary := false
	for _, p := range remainingProviders {
		if p.IsPrimary {
			hasPrimary = true
			break
		}
	}

	if !hasPrimary && len(remainingProviders) > 0 {
		// Set the first remaining provider as primary
		err = r.SetPrimaryProvider(ctx, userUUID, remainingProviders[0].Provider)
		if err != nil {
			return fmt.Errorf("failed to set new primary provider: %w", err)
		}
	}

	return nil
}

func (r *oauthProviderRepository) DeleteProvider(ctx context.Context, uuid string) error {
	filter := bson.M{"uuid": uuid}

	//tenantscope:allow OAuth identities are audience-tier scoped, not org scoped; this repository is bound to one tier collection.
	result, err := r.collection.DeleteOne(ctx, filter)
	if err != nil {
		return fmt.Errorf("failed to delete OAuth provider: %w", err)
	}

	if result.DeletedCount == 0 {
		return fmt.Errorf("OAuth provider not found")
	}

	return nil
}

func (r *oauthProviderRepository) FindByEmail(ctx context.Context, email string) ([]*models.OAuthProviderDoc, error) {
	filter := bson.M{"email": email}

	//tenantscope:allow OAuth identities are audience-tier scoped, not org scoped; this repository is bound to one tier collection.
	cursor, err := r.collection.Find(ctx, filter)
	if err != nil {
		return nil, fmt.Errorf("failed to find providers by email: %w", err)
	}
	defer cursor.Close(ctx)

	var providers []*models.OAuthProviderDoc
	for cursor.Next(ctx) {
		var provider models.OAuthProviderDoc
		if err := cursor.Decode(&provider); err != nil {
			return nil, fmt.Errorf("failed to decode OAuth provider: %w", err)
		}
		providers = append(providers, &provider)
	}

	return providers, nil
}

func (r *oauthProviderRepository) ConsolidateProviders(ctx context.Context, fromUserUUID, toUserUUID string) error {
	// Get all providers for the source user
	fromProviders, err := r.GetByUserUUID(ctx, fromUserUUID)
	if err != nil {
		return fmt.Errorf("failed to get source providers: %w", err)
	}

	// Get existing providers for target user
	toProviders, err := r.GetByUserUUID(ctx, toUserUUID)
	if err != nil {
		return fmt.Errorf("failed to get target providers: %w", err)
	}

	// Create a map of existing provider types for target user
	existingProviders := make(map[models.OAuthProvider]bool)
	for _, provider := range toProviders {
		existingProviders[provider.Provider] = true
	}

	// Transfer providers that don't already exist for target user
	for _, provider := range fromProviders {
		if !existingProviders[provider.Provider] {
			// Update the provider to point to the target user
			filter := bson.M{"uuid": provider.UUID}
			update := bson.M{
				"$set": bson.M{
					"userUuid":  toUserUUID,
					"isPrimary": false, // Don't make it primary automatically
					"updatedAt": time.Now(),
				},
			}

			//tenantscope:allow OAuth identities are audience-tier scoped, not org scoped; this repository is bound to one tier collection.
			_, err := r.collection.UpdateOne(ctx, filter, update)
			if err != nil {
				return fmt.Errorf("failed to transfer provider %s: %w", provider.Provider, err)
			}
		} else {
			// Delete duplicate provider
			err := r.DeleteProvider(ctx, provider.UUID)
			if err != nil {
				return fmt.Errorf("failed to delete duplicate provider %s: %w", provider.Provider, err)
			}
		}
	}

	return nil
}
