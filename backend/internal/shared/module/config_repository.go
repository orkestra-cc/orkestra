package module

import (
	"context"
	"fmt"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

const moduleConfigCollection = "module_configs"

// ModuleConfigRepository provides MongoDB CRUD for the module_configs collection.
type ModuleConfigRepository struct {
	collection *mongo.Collection
}

// NewModuleConfigRepository creates a repository and ensures a unique index on moduleName.
func NewModuleConfigRepository(db *mongo.Database) *ModuleConfigRepository {
	coll := db.Collection(moduleConfigCollection)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	indexes := []mongo.IndexModel{
		{Keys: bson.D{{Key: "moduleName", Value: 1}}, Options: options.Index().SetUnique(true)},
	}
	coll.Indexes().CreateMany(ctx, indexes) //nolint:errcheck

	return &ModuleConfigRepository{collection: coll}
}

// FindByName retrieves a module config by name. Returns (nil, nil) when not found.
func (r *ModuleConfigRepository) FindByName(ctx context.Context, name string) (*ModuleConfig, error) {
	var doc ModuleConfig
	err := r.collection.FindOne(ctx, bson.M{"moduleName": name}).Decode(&doc)
	if err == mongo.ErrNoDocuments {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("find module config %q: %w", name, err)
	}
	return &doc, nil
}

// FindAll retrieves all module config documents.
func (r *ModuleConfigRepository) FindAll(ctx context.Context) ([]ModuleConfig, error) {
	cursor, err := r.collection.Find(ctx, bson.M{})
	if err != nil {
		return nil, fmt.Errorf("find all module configs: %w", err)
	}
	defer cursor.Close(ctx)

	var results []ModuleConfig
	if err := cursor.All(ctx, &results); err != nil {
		return nil, fmt.Errorf("decode module configs: %w", err)
	}
	return results, nil
}

// Upsert inserts or updates a module config. Uses $setOnInsert for createdAt
// so that existing documents preserve their original creation timestamp.
func (r *ModuleConfigRepository) Upsert(ctx context.Context, config *ModuleConfig) error {
	now := time.Now()
	config.UpdatedAt = now

	update := bson.M{
		"$set": bson.M{
			"displayName":     config.DisplayName,
			"description":     config.Description,
			"category":        config.Category,
			"enabled":         config.Enabled,
			"configValues":    config.ConfigValues,
			"encryptedValues": config.EncryptedValues,
			"configSchema":    config.ConfigSchema,
			"dependsOn":       config.DependsOn,
			"needsRestart":    config.NeedsRestart,
			"updatedAt":       now,
		},
		"$setOnInsert": bson.M{
			"createdAt": now,
		},
	}

	opts := options.Update().SetUpsert(true)
	_, err := r.collection.UpdateOne(
		ctx,
		bson.M{"moduleName": config.ModuleName},
		update,
		opts,
	)
	if err != nil {
		return fmt.Errorf("upsert module config %q: %w", config.ModuleName, err)
	}
	return nil
}

// UpdateEnabled toggles a module's enabled state and marks the module as
// needing a restart so the admin UI can signal that changes take effect
// after the backend is restarted.
func (r *ModuleConfigRepository) UpdateEnabled(ctx context.Context, name string, enabled bool) error {
	_, err := r.collection.UpdateOne(
		ctx,
		bson.M{"moduleName": name},
		bson.M{"$set": bson.M{"enabled": enabled, "needsRestart": true, "updatedAt": time.Now()}},
	)
	if err != nil {
		return fmt.Errorf("update enabled for %q: %w", name, err)
	}
	return nil
}

// UpdateConfigValues updates a module's plain and encrypted config values.
// Sets needsRestart to true so the admin UI can signal a restart is needed.
func (r *ModuleConfigRepository) UpdateConfigValues(ctx context.Context, name string, values map[string]string, encrypted map[string]string) error {
	_, err := r.collection.UpdateOne(
		ctx,
		bson.M{"moduleName": name},
		bson.M{"$set": bson.M{
			"configValues":    values,
			"encryptedValues": encrypted,
			"needsRestart":    true,
			"updatedAt":       time.Now(),
		}},
	)
	if err != nil {
		return fmt.Errorf("update config values for %q: %w", name, err)
	}
	return nil
}

// FindEnabledAddonNames returns the names of all non-core modules that have
// enabled=true in the module_configs collection. Used by the boot path so
// that modules enabled via the admin UI are loaded on next restart.
func (r *ModuleConfigRepository) FindEnabledAddonNames(ctx context.Context) ([]string, error) {
	cursor, err := r.collection.Find(ctx, bson.M{
		"enabled":  true,
		"category": bson.M{"$ne": string(CategoryCore)},
	}, options.Find().SetProjection(bson.M{"moduleName": 1}))
	if err != nil {
		return nil, fmt.Errorf("find enabled addon names: %w", err)
	}
	defer cursor.Close(ctx)

	var names []string
	for cursor.Next(ctx) {
		var doc struct {
			ModuleName string `bson:"moduleName"`
		}
		if err := cursor.Decode(&doc); err != nil {
			return nil, fmt.Errorf("decode enabled addon name: %w", err)
		}
		names = append(names, doc.ModuleName)
	}
	return names, cursor.Err()
}

// ClearNeedsRestart resets the needsRestart flag for a module. Called at boot
// for every loaded module so the flag only remains set for modules that are
// enabled in the DB but were not loaded in this process.
func (r *ModuleConfigRepository) ClearNeedsRestart(ctx context.Context, name string) error {
	_, err := r.collection.UpdateOne(
		ctx,
		bson.M{"moduleName": name},
		bson.M{"$set": bson.M{"needsRestart": false, "updatedAt": time.Now()}},
	)
	if err != nil {
		return fmt.Errorf("clear needsRestart for %q: %w", name, err)
	}
	return nil
}

// UpdateSchema updates only the config schema for a module (used during schema migration
// when the module already has a config document but fields were added/removed).
func (r *ModuleConfigRepository) UpdateSchema(ctx context.Context, name string, schema []ConfigField) error {
	_, err := r.collection.UpdateOne(
		ctx,
		bson.M{"moduleName": name},
		bson.M{"$set": bson.M{
			"configSchema": schema,
			"updatedAt":    time.Now(),
		}},
	)
	if err != nil {
		return fmt.Errorf("update schema for %q: %w", name, err)
	}
	return nil
}
