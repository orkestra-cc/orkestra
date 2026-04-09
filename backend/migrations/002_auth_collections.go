package migrations

import (
	"context"
	"fmt"
	"time"

	"github.com/orkestra/backend/internal/core/auth/models"
	userModels "github.com/orkestra/backend/internal/core/user/models"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// Migration002AuthCollections creates the new authentication collections with proper indexes
type Migration002AuthCollections struct {
	db *mongo.Database
}

func NewMigration002AuthCollections(db *mongo.Database) *Migration002AuthCollections {
	return &Migration002AuthCollections{db: db}
}

func (m *Migration002AuthCollections) Up(ctx context.Context) error {
	// Create collections
	collections := []string{
		models.OAuthProvidersCollection,
		models.RefreshTokensCollection,
		models.AuthSessionsCollection,
		models.SecurityEventsCollection,
	}

	for _, collName := range collections {
		// Check if collection exists
		names, err := m.db.ListCollectionNames(ctx, bson.D{})
		if err != nil {
			return fmt.Errorf("failed to list collections: %w", err)
		}

		exists := false
		for _, name := range names {
			if name == collName {
				exists = true
				break
			}
		}

		if !exists {
			if err := m.db.CreateCollection(ctx, collName); err != nil {
				return fmt.Errorf("failed to create collection %s: %w", collName, err)
			}
			fmt.Printf("Created collection: %s\n", collName)
		}
	}

	// Create indexes
	indexDefinitions := models.GetCollectionIndexes()
	for _, collIndexes := range indexDefinitions {
		coll := m.db.Collection(collIndexes.Collection)

		for _, indexDef := range collIndexes.Indexes {
			// Build index model
			indexModel := mongo.IndexModel{
				Keys: indexDef.Keys,
				Options: options.Index().
					SetName(indexDef.Name).
					SetBackground(true),
			}

			if indexDef.Unique {
				indexModel.Options.SetUnique(true)
			}

			if indexDef.TTL != nil {
				ttlSeconds := int32(indexDef.TTL.Seconds())
				indexModel.Options.SetExpireAfterSeconds(ttlSeconds)
			}

			// Create index
			indexName, err := coll.Indexes().CreateOne(ctx, indexModel)
			if err != nil {
				// Check if it's a duplicate index error
				if !mongo.IsDuplicateKeyError(err) {
					return fmt.Errorf("failed to create index %s on collection %s: %w",
						indexDef.Name, collIndexes.Collection, err)
				}
			} else {
				fmt.Printf("Created index %s on collection %s\n", indexName, collIndexes.Collection)
			}
		}
	}

	// Migrate existing user data to new structure
	if err := m.migrateExistingUsers(ctx); err != nil {
		return fmt.Errorf("failed to migrate existing users: %w", err)
	}

	return nil
}

func (m *Migration002AuthCollections) Down(ctx context.Context) error {
	// Note: This is a destructive operation and should be used carefully
	collections := []string{
		models.OAuthProvidersCollection,
		models.RefreshTokensCollection,
		models.AuthSessionsCollection,
		models.SecurityEventsCollection,
	}

	for _, collName := range collections {
		if err := m.db.Collection(collName).Drop(ctx); err != nil {
			// Ignore error if collection doesn't exist
			if err != mongo.ErrNoDocuments {
				return fmt.Errorf("failed to drop collection %s: %w", collName, err)
			}
		}
		fmt.Printf("Dropped collection: %s\n", collName)
	}

	return nil
}

func (m *Migration002AuthCollections) migrateExistingUsers(ctx context.Context) error {
	usersCollection := m.db.Collection(models.UsersCollection)
	oauthProvidersCollection := m.db.Collection(models.OAuthProvidersCollection)

	// Find all users with OAuth links
	cursor, err := usersCollection.Find(ctx, bson.M{
		"$or": []bson.M{
			{"oauthLinks": bson.M{"$exists": true, "$ne": []interface{}{}}},
			{"oauthProvider": bson.M{"$exists": true, "$ne": ""}},
		},
	})
	if err != nil {
		return fmt.Errorf("failed to find users with OAuth data: %w", err)
	}
	defer cursor.Close(ctx)

	migratedCount := 0
	for cursor.Next(ctx) {
		var user userModels.User
		if err := cursor.Decode(&user); err != nil {
			fmt.Printf("Warning: failed to decode user: %v\n", err)
			continue
		}

		// Skip if user doesn't have a UUID yet
		if user.UUID == "" {
			fmt.Printf("Skipping user %s - no UUID\n", user.ID.Hex())
			continue
		}

		// Migrate OAuth links to separate collection
		if len(user.OAuthLinks) > 0 {
			for _, link := range user.OAuthLinks {
				providerDoc := models.OAuthProviderDoc{
					UUID:       models.GenerateTimeOrderedUUID(),
					UserUUID:   user.UUID,
					Provider:   models.OAuthProvider(string(link.Provider)),
					ProviderID: link.ProviderID,
					Email:      link.Email,
					IsPrimary:  link.IsPrimary,
					LinkedAt:   link.LinkedAt,
					LastUsed:   link.LastUsed,
					CreatedAt:  time.Now(),
					UpdatedAt:  time.Now(),
				}

				// Check if this provider already exists
				existingCount, err := oauthProvidersCollection.CountDocuments(ctx, bson.M{
					"userUuid":   user.UUID,
					"provider":   link.Provider,
					"providerId": link.ProviderID,
				})
				if err != nil {
					fmt.Printf("Warning: failed to check existing provider: %v\n", err)
					continue
				}

				if existingCount == 0 {
					if _, err := oauthProvidersCollection.InsertOne(ctx, providerDoc); err != nil {
						fmt.Printf("Warning: failed to insert OAuth provider for user %s: %v\n", user.UUID, err)
						continue
					}
					migratedCount++
				}
			}
		} else if user.OAuthProvider != "" && user.OAuthID != "" {
			// Migrate legacy OAuth data
			providerDoc := models.OAuthProviderDoc{
				UUID:       models.GenerateTimeOrderedUUID(),
				UserUUID:   user.UUID,
				Provider:   models.OAuthProvider(user.OAuthProvider),
				ProviderID: user.OAuthID,
				Email:      user.Email,
				IsPrimary:  true,
				LinkedAt:   user.CreatedAt,
				LastUsed:   user.LastLogin,
				CreatedAt:  time.Now(),
				UpdatedAt:  time.Now(),
			}

			// Check if this provider already exists
			existingCount, err := oauthProvidersCollection.CountDocuments(ctx, bson.M{
				"userUuid":   user.UUID,
				"provider":   user.OAuthProvider,
				"providerId": user.OAuthID,
			})
			if err != nil {
				fmt.Printf("Warning: failed to check existing provider: %v\n", err)
				continue
			}

			if existingCount == 0 {
				if _, err := oauthProvidersCollection.InsertOne(ctx, providerDoc); err != nil {
					fmt.Printf("Warning: failed to insert OAuth provider for user %s: %v\n", user.UUID, err)
					continue
				}
				migratedCount++
			}
		}

		// NOTE: RefreshTokens are no longer stored in the User model
		// They are now managed separately by the auth module
		// RefreshToken migration has been skipped as the field no longer exists
	}

	if err := cursor.Err(); err != nil {
		return fmt.Errorf("cursor error: %w", err)
	}

	fmt.Printf("Migrated OAuth data for %d provider links\n", migratedCount)
	return nil
}

func (m *Migration002AuthCollections) Version() string {
	return "002_auth_collections"
}

func (m *Migration002AuthCollections) Description() string {
	return "Create separate collections for OAuth providers, refresh tokens, and auth sessions"
}
