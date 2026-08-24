package module

import (
	"context"
	"errors"
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
	coll.Indexes().CreateMany(ctx, indexes) //nolint:errcheck // best-effort index ensure; subsequent reads will surface persistent failures

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

	setFields := bson.M{
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
	}
	if config.ActiveEnvironment != "" {
		setFields["activeEnvironment"] = config.ActiveEnvironment
	}
	if len(config.Environments) > 0 {
		setFields["environments"] = config.Environments
	}

	update := bson.M{
		"$set": setFields,
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

// UpdateEnvironmentConfig updates config values and encrypted secrets for a
// specific named environment within a module's Environments map.
func (r *ModuleConfigRepository) UpdateEnvironmentConfig(ctx context.Context, name, envName string, values map[string]string, encrypted map[string]string) error {
	now := time.Now()
	update := bson.M{
		"$set": bson.M{
			fmt.Sprintf("environments.%s.configValues", envName):    values,
			fmt.Sprintf("environments.%s.encryptedValues", envName): encrypted,
			fmt.Sprintf("environments.%s.updatedAt", envName):       now,
			"needsRestart": true,
			"updatedAt":    now,
		},
	}
	_, err := r.collection.UpdateOne(ctx, bson.M{"moduleName": name}, update)
	if err != nil {
		return fmt.Errorf("update environment config %q/%q: %w", name, envName, err)
	}
	return nil
}

// CompareAndSwapEnvironment replaces ONE environment sub-document, and only
// while its revision still matches. Returns false — not an error — when the
// revision has moved: losing a race is an expected outcome the caller decides
// what to do about, not a failure.
//
// Scoping the swap to the sub-document matters: Environments is a nested map,
// so guarding a whole-document replace with one environment's revision would
// silently discard concurrent edits to a sibling environment. When env is the
// active one, the legacy top-level maps are synced in the SAME update, so the
// two can never diverge.
//
// A document written before record lists existed carries no revision field at
// all. Absent and 0 are the same value, so an expectation of 0 also matches a
// missing field — otherwise the first mutation on every pre-existing module
// would fail against nothing.
func (r *ModuleConfigRepository) CompareAndSwapEnvironment(
	ctx context.Context, name, envName string, expectedRevision int64, next EnvironmentConfig,
) (bool, error) {
	envPath := "environments." + envName
	next.Revision = expectedRevision + 1
	next.UpdatedAt = time.Now().UTC()

	revisionMatches := bson.M{envPath + ".revision": expectedRevision}
	if expectedRevision == 0 {
		revisionMatches = bson.M{"$or": bson.A{
			bson.M{envPath + ".revision": expectedRevision},
			bson.M{envPath + ".revision": bson.M{"$exists": false}},
		}}
	}
	filter := bson.M{"moduleName": name}
	for k, v := range revisionMatches {
		filter[k] = v
	}

	set := bson.M{
		envPath:        next,
		"needsRestart": true,
		"updatedAt":    next.UpdatedAt,
	}

	var doc ModuleConfig
	if err := r.collection.FindOne(ctx, bson.M{"moduleName": name}).Decode(&doc); err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			return false, fmt.Errorf("module %q not found", name)
		}
		return false, fmt.Errorf("compare-and-swap environment %q/%q: %w", name, envName, err)
	}
	if doc.ActiveEnv() == envName {
		set["configValues"] = next.ConfigValues
		set["encryptedValues"] = next.EncryptedValues
	}

	res, err := r.collection.UpdateOne(ctx, filter, bson.M{"$set": set})
	if err != nil {
		return false, fmt.Errorf("compare-and-swap environment %q/%q: %w", name, envName, err)
	}
	return res.MatchedCount == 1, nil
}

// SetActiveEnvironment switches the active environment for a module.
func (r *ModuleConfigRepository) SetActiveEnvironment(ctx context.Context, name, envName string) error {
	now := time.Now()
	_, err := r.collection.UpdateOne(
		ctx,
		bson.M{"moduleName": name},
		bson.M{"$set": bson.M{"activeEnvironment": envName, "needsRestart": true, "updatedAt": now}},
	)
	if err != nil {
		return fmt.Errorf("set active environment for %q to %q: %w", name, envName, err)
	}
	return nil
}

// ActivateEnvironment sets the active environment and copies that
// environment's values into the legacy top-level maps in ONE update, reading
// them server-side at execution time. The previous two-step version copied a
// snapshot the process had read earlier, so a write landing in between was
// silently rolled back — including a removal, which brought its secret back.
func (r *ModuleConfigRepository) ActivateEnvironment(ctx context.Context, name, envName string) error {
	envPath := "$environments." + envName
	res, err := r.collection.UpdateOne(ctx,
		bson.M{"moduleName": name, "environments." + envName: bson.M{"$exists": true}},
		mongo.Pipeline{{{Key: "$set", Value: bson.M{
			"activeEnvironment": envName,
			"configValues":      bson.M{"$ifNull": bson.A{envPath + ".configValues", bson.M{}}},
			"encryptedValues":   bson.M{"$ifNull": bson.A{envPath + ".encryptedValues", bson.M{}}},
			"needsRestart":      true,
			"updatedAt":         time.Now().UTC(),
		}}}},
	)
	if err != nil {
		return fmt.Errorf("activate environment %q for %q: %w", envName, name, err)
	}
	if res.MatchedCount == 0 {
		return fmt.Errorf("environment %q not found for module %q", envName, name)
	}
	return nil
}

// MigrateToEnvironments copies legacy top-level ConfigValues/EncryptedValues
// into the Environments map and sets ActiveEnvironment = "production".
// This is a one-time migration for documents that predate the environment feature.
func (r *ModuleConfigRepository) MigrateToEnvironments(ctx context.Context, name string, configValues, encryptedValues map[string]string) error {
	now := time.Now()
	prodEnv := EnvironmentConfig{
		ConfigValues:    configValues,
		EncryptedValues: encryptedValues,
		UpdatedAt:       now,
	}
	sandboxEnv := EnvironmentConfig{
		ConfigValues:    make(map[string]string),
		EncryptedValues: make(map[string]string),
		UpdatedAt:       now,
	}
	_, err := r.collection.UpdateOne(
		ctx,
		bson.M{"moduleName": name},
		bson.M{"$set": bson.M{
			"activeEnvironment":       "production",
			"environments.production": prodEnv,
			"environments.sandbox":    sandboxEnv,
			"updatedAt":               now,
		}},
	)
	if err != nil {
		return fmt.Errorf("migrate to environments for %q: %w", name, err)
	}
	return nil
}

// RefreshMetadata rewrites the fields derived from a module's code
// (displayName, description, category, configSchema, dependsOn) so the
// stored document stays in sync with the current binary. Admin-editable
// fields (enabled, configValues, environments) are preserved.
func (r *ModuleConfigRepository) RefreshMetadata(ctx context.Context, m Module) error {
	_, err := r.collection.UpdateOne(
		ctx,
		bson.M{"moduleName": m.Name()},
		bson.M{"$set": bson.M{
			"displayName":  DisplayNameOf(m),
			"description":  DescriptionOf(m),
			"category":     m.Category(),
			"configSchema": ConfigSchemaOf(m),
			"dependsOn":    DependenciesOf(m),
			"updatedAt":    time.Now(),
		}},
	)
	if err != nil {
		return fmt.Errorf("refresh metadata for %q: %w", m.Name(), err)
	}
	return nil
}
