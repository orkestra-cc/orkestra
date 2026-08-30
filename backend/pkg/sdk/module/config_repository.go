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

// CompareAndSwapEnvironment replaces ONE environment sub-document, and only
// while its revision still matches. Returns false — not an error — when the
// revision has moved: losing a race is an expected outcome the caller decides
// what to do about, not a failure.
//
// Scoping the swap to the sub-document matters: Environments is a nested map,
// so guarding a whole-document replace with one environment's revision would
// silently discard concurrent edits to a sibling environment.
//
// It is a PIPELINE update: the "is this the active profile?" decision is
// taken by the server against the document's CURRENT activeEnvironment, in
// the same operation that writes the profile. The previous read-then-update
// let a concurrent activation land between the two and left the legacy
// mirror carrying the wrong profile. $literal stops the aggregation engine
// from interpreting stored maps (dotted keys, "$"-prefixed values) as
// expressions. configRevision advances in the same update, so an ordinary
// config write that read the document before this roster change loses its
// own compare-and-swap instead of passing it unseen; needsRestart is
// persisted as given — the inverse of the module's hot-reload capability.
//
// A document written before record lists existed carries no revision field at
// all. Absent and 0 are the same value, so an expectation of 0 also matches a
// missing field — otherwise the first mutation on every pre-existing module
// would fail against nothing.
func (r *ModuleConfigRepository) CompareAndSwapEnvironment(
	ctx context.Context, name, envName string, expectedRevision int64, next EnvironmentConfig, needsRestart bool,
) (bool, error) {
	envPath := "environments." + envName
	next.Revision = expectedRevision + 1
	next.UpdatedAt = time.Now().UTC()
	if next.ConfigValues == nil {
		next.ConfigValues = map[string]string{}
	}
	if next.EncryptedValues == nil {
		next.EncryptedValues = map[string]string{}
	}

	filter := bson.M{"moduleName": name}
	if expectedRevision == 0 {
		filter["$or"] = bson.A{
			bson.M{envPath + ".revision": expectedRevision},
			bson.M{envPath + ".revision": bson.M{"$exists": false}},
		}
	} else {
		filter[envPath+".revision"] = expectedRevision
	}

	isActive := bson.M{"$eq": bson.A{
		bson.M{"$ifNull": bson.A{"$activeEnvironment", "production"}},
		envName,
	}}
	update := mongo.Pipeline{{{Key: "$set", Value: bson.M{
		envPath:           bson.M{"$literal": next},
		"configValues":    bson.M{"$cond": bson.A{isActive, bson.M{"$literal": next.ConfigValues}, "$configValues"}},
		"encryptedValues": bson.M{"$cond": bson.A{isActive, bson.M{"$literal": next.EncryptedValues}, "$encryptedValues"}},
		"needsRestart":    needsRestart,
		"configRevision":  bson.M{"$add": bson.A{bson.M{"$ifNull": bson.A{"$configRevision", 0}}, 1}},
		"updatedAt":       next.UpdatedAt,
	}}}}

	res, err := r.collection.UpdateOne(ctx, filter, update)
	if err != nil {
		return false, fmt.Errorf("compare-and-swap environment %q/%q: %w", name, envName, err)
	}
	return res.MatchedCount == 1, nil
}

// ConfigMutation is the explicit shape of ONE atomic module_configs write.
// Exactly one of two forms is used:
//
//   - a values write: Env names the profile whose maps are REPLACED by
//     EnvValues/EnvSecrets (callers merge first) and whose revision becomes
//     EnvRevision+1; WriteLegacy additionally replaces the top-level legacy
//     maps with LegacyValues/LegacySecrets — the active-profile write and
//     the boot backfill both use this, the inactive-profile write does not;
//   - an activation: Activate names the profile to make active, and its
//     STORED maps are copied server-side into the legacy fields.
//
// Every form filters on ExpectedRevision, writes ExpectedRevision+1 back,
// and persists NeedsRestart as given. Secrets are ciphertext by the time
// they reach here.
type ConfigMutation struct {
	ExpectedRevision int64

	Env         string
	EnvValues   map[string]string
	EnvSecrets  map[string]string
	EnvRevision int64

	WriteLegacy   bool
	LegacyValues  map[string]string
	LegacySecrets map[string]string

	Activate string

	NeedsRestart bool
}

// validate rejects a shape the repository cannot express in one update, and
// rejects a forgotten map rather than guessing at it. A nil EnvSecrets or
// LegacySecrets is a caller bug, not "no secrets": this package already
// shipped the alternative once (a nil normalized to {} and written over
// every stored secret — "UpdateConfig wiped module secrets", fixed in
// v0.3.2) and must never repeat it. A caller that means to clear a map
// passes an explicit empty one; nil is refused instead.
func (m ConfigMutation) validate() error {
	switch {
	case m.Activate != "" && (m.Env != "" || m.WriteLegacy):
		return errors.New("config mutation: activation cannot be combined with a values write")
	case m.Activate == "" && m.Env == "" && !m.WriteLegacy:
		return errors.New("config mutation: nothing to write")
	case m.Env != "" && (m.EnvValues == nil || m.EnvSecrets == nil):
		return errors.New("config mutation: an environment write requires both maps (use an empty map to clear)")
	case m.WriteLegacy && (m.LegacyValues == nil || m.LegacySecrets == nil):
		return errors.New("config mutation: WriteLegacy requires both legacy maps (use an empty map to clear)")
	}
	return nil
}

// CompareAndSwapConfig applies one ConfigMutation to a module document in a
// SINGLE UpdateOne, and only while the document's configRevision still equals
// m.ExpectedRevision (an absent field matches 0). Returns (false, nil) when
// nothing matched — a lost race, or a profile that no longer exists — which
// the service reports as ErrRevisionStale.
//
// Because it is one update, either every target field lands or none does:
// there is no legacy/environment partial state and no second write whose
// failure could be logged and swallowed. An activation is a pipeline update
// so the copied values are read server-side at execution time, never from a
// snapshot the process took earlier.
func (r *ModuleConfigRepository) CompareAndSwapConfig(ctx context.Context, name string, m ConfigMutation) (bool, error) {
	if err := m.validate(); err != nil {
		return false, err
	}
	now := time.Now().UTC()
	next := m.ExpectedRevision + 1

	filter := bson.M{"moduleName": name}
	if m.ExpectedRevision == 0 {
		filter["$or"] = bson.A{
			bson.M{"configRevision": int64(0)},
			bson.M{"configRevision": bson.M{"$exists": false}},
		}
	} else {
		filter["configRevision"] = m.ExpectedRevision
	}

	var update any
	if m.Activate != "" {
		filter["environments."+m.Activate] = bson.M{"$exists": true}
		envPath := "$environments." + m.Activate
		update = mongo.Pipeline{{{Key: "$set", Value: bson.M{
			"activeEnvironment": m.Activate,
			"configValues":      bson.M{"$ifNull": bson.A{envPath + ".configValues", bson.M{}}},
			"encryptedValues":   bson.M{"$ifNull": bson.A{envPath + ".encryptedValues", bson.M{}}},
			"needsRestart":      m.NeedsRestart,
			"configRevision":    next,
			"updatedAt":         now,
		}}}}
	} else {
		set := bson.M{
			"needsRestart":   m.NeedsRestart,
			"configRevision": next,
			"updatedAt":      now,
		}
		if m.Env != "" {
			p := "environments." + m.Env
			filter[p] = bson.M{"$exists": true}
			set[p+".configValues"] = m.EnvValues
			set[p+".encryptedValues"] = m.EnvSecrets
			set[p+".revision"] = m.EnvRevision + 1
			set[p+".updatedAt"] = now
		}
		if m.WriteLegacy {
			set["configValues"] = m.LegacyValues
			set["encryptedValues"] = m.LegacySecrets
		}
		update = bson.M{"$set": set}
	}

	res, err := r.collection.UpdateOne(ctx, filter, update)
	if err != nil {
		return false, fmt.Errorf("compare-and-swap config %q: %w", name, err)
	}
	return res.MatchedCount == 1, nil
}

// MigrateToEnvironments copies the legacy top-level maps into a "production"
// profile (plus an empty "sandbox") and sets activeEnvironment, in one
// compare-and-swap that matches only while the document STILL has no
// profiles and its configRevision is the one the caller read. Returns
// (false, nil) when another writer migrated or moved the document first —
// the caller re-reads instead of copying a stale legacy snapshot over a
// profile that was just written. Advances configRevision.
func (r *ModuleConfigRepository) MigrateToEnvironments(
	ctx context.Context, name string, configValues, encryptedValues map[string]string, expectedRevision int64,
) (bool, error) {
	now := time.Now()
	noProfiles := bson.M{"$or": bson.A{
		bson.M{"environments": bson.M{"$exists": false}},
		bson.M{"environments": bson.M{}},
	}}
	revision := bson.M{"configRevision": expectedRevision}
	if expectedRevision == 0 {
		revision = bson.M{"$or": bson.A{
			bson.M{"configRevision": int64(0)},
			bson.M{"configRevision": bson.M{"$exists": false}},
		}}
	}
	filter := bson.M{"moduleName": name, "$and": bson.A{noProfiles, revision}}
	update := bson.M{"$set": bson.M{
		"activeEnvironment":       "production",
		"environments.production": EnvironmentConfig{ConfigValues: configValues, EncryptedValues: encryptedValues, UpdatedAt: now},
		"environments.sandbox":    EnvironmentConfig{ConfigValues: map[string]string{}, EncryptedValues: map[string]string{}, UpdatedAt: now},
		"configRevision":          expectedRevision + 1,
		"updatedAt":               now,
	}}
	res, err := r.collection.UpdateOne(ctx, filter, update)
	if err != nil {
		return false, fmt.Errorf("migrate to environments for %q: %w", name, err)
	}
	return res.MatchedCount == 1, nil
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
