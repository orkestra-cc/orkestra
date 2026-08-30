package module

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"time"
)

// ModuleConfigService manages module configurations in MongoDB with Redis caching.
// It provides the hot-path IsEnabled() check used by the ModuleGate middleware.
type ModuleConfigService struct {
	repo        ConfigRepository
	redis       RedisClient
	logger      *slog.Logger
	coreModules map[string]bool // precomputed set — never hits DB/Redis

	// knownModules is captured during SeedFromModules so the service can
	// lazy-rebuild the module_configs collection if it is emptied at
	// runtime (dev DB wipe, accidental drop, etc.) without requiring a
	// backend restart. Populated once at boot and then read-only.
	knownModules map[string]Module

	// hotReload answers "does this module re-read its config at request
	// time" — installed by the registry from SupportsHotReload. Every
	// config/environment/activation write persists needsRestart =
	// !hotReload(name) in the same update as the values, so the flag can
	// never diverge through a later best-effort clear. Nil means every
	// write marks needsRestart (the pre-resolver behaviour).
	hotReload func(name string) bool
}

// SetHotReloadResolver installs the registry's hot-reload answer. See the
// hotReload field.
func (s *ModuleConfigService) SetHotReloadResolver(fn func(name string) bool) { s.hotReload = fn }

func (s *ModuleConfigService) needsRestartFor(name string) bool {
	if s.hotReload == nil {
		return true
	}
	return !s.hotReload(name)
}

// schemaFor returns the schema every mutation is judged against: the
// registered module's live declaration when the module is known — the
// binary is the source of truth, and the stored copy is only a boot-time
// snapshot whose refresh may have failed — or the stored snapshot for a
// document whose module is not registered with this service.
func (s *ModuleConfigService) schemaFor(name string, doc *ModuleConfig) []ConfigField {
	if m, ok := s.knownModules[name]; ok {
		return ConfigSchemaOf(m)
	}
	return doc.ConfigSchema
}

// encryptAll encrypts every submitted secret; an empty plaintext encrypts to
// "" (clearing the key), which GetSecret then reads as "fall back".
func encryptAll(secrets map[string]string) (map[string]string, error) {
	encrypted := make(map[string]string, len(secrets))
	for k, v := range secrets {
		enc, err := encryptSecret(v)
		if err != nil {
			return nil, fmt.Errorf("encrypt secret %q: %w", k, err)
		}
		encrypted[k] = enc
	}
	return encrypted, nil
}

const (
	enabledCachePrefix = "module:enabled:"
	enabledCacheTTL    = 30 * time.Second
)

// NewModuleConfigService creates a new config service.
func NewModuleConfigService(repo ConfigRepository, redis RedisClient, logger *slog.Logger) *ModuleConfigService {
	return &ModuleConfigService{
		repo:         repo,
		redis:        redis,
		logger:       logger,
		coreModules:  make(map[string]bool),
		knownModules: make(map[string]Module),
	}
}

// SetCoreModules builds the set of core module names from registered modules.
// Core modules always return true from IsEnabled without any DB/Redis check.
func (s *ModuleConfigService) SetCoreModules(modules []Module) {
	for _, m := range modules {
		if m.Category() == CategoryCore {
			s.coreModules[m.Name()] = true
		}
	}
}

// IsEnabled checks if a module is enabled. This is the hot path called on every
// HTTP request to gated modules.
//
// Lookup order: core map → Redis cache (30s TTL) → MongoDB → fail-open (true).
func (s *ModuleConfigService) IsEnabled(ctx context.Context, moduleName string) bool {
	// Core modules are always enabled — zero I/O.
	if s.coreModules[moduleName] {
		return true
	}

	// Try Redis cache first.
	cacheKey := enabledCachePrefix + moduleName
	cached, err := s.redis.Get(ctx, cacheKey)
	if err == nil && cached != "" {
		return cached == "true"
	}

	// Cache miss — fall through to MongoDB.
	doc, err := s.repo.FindByName(ctx, moduleName)
	if err != nil {
		// Fail-open: if DB is unreachable, don't break running modules.
		s.logger.Warn("IsEnabled: MongoDB lookup failed, assuming enabled",
			slog.String("module", moduleName),
			slog.String("error", err.Error()),
		)
		return true
	}

	// No document yet (first boot, before seeding) — assume enabled.
	if doc == nil {
		return true
	}

	// Cache the result in Redis.
	enabledStr := "false"
	if doc.Enabled {
		enabledStr = "true"
	}
	if err := s.redis.Set(ctx, cacheKey, enabledStr, enabledCacheTTL); err != nil {
		s.logger.Warn("IsEnabled: failed to cache result",
			slog.String("module", moduleName),
			slog.String("error", err.Error()),
		)
	}

	return doc.Enabled
}

// RegisterKnownModules adds modules to the in-memory known-modules catalog
// without touching MongoDB. Called from the boot path to advertise addons
// whose Enabled() returned false at first-boot seeding so GetConfig /
// GetAllConfigs can lazy-seed and return them to the admin UI.
// Entries already registered by SeedFromModules are preserved.
func (s *ModuleConfigService) RegisterKnownModules(modules []Module) {
	for _, m := range modules {
		if _, exists := s.knownModules[m.Name()]; !exists {
			s.knownModules[m.Name()] = m
		}
	}
}

// SeedFromModules populates the module_configs collection on first boot
// using each module's ConfigSchema() and current env var values.
// On subsequent boots, it only updates the schema (preserving admin-changed values).
func (s *ModuleConfigService) SeedFromModules(ctx context.Context, modules []Module) error {
	// Build core modules map as a side effect of seeding.
	s.SetCoreModules(modules)

	// Remember the registered modules so GetConfig can lazy-seed missing
	// docs later (e.g. after a live DB wipe in dev).
	for _, m := range modules {
		s.knownModules[m.Name()] = m
	}

	for _, m := range modules {
		existing, err := s.repo.FindByName(ctx, m.Name())
		if err != nil {
			s.logger.Error("SeedFromModules: failed to check existing config",
				slog.String("module", m.Name()),
				slog.String("error", err.Error()),
			)
			continue
		}

		if existing != nil {
			// Module already has a config document — refresh every code-derived
			// field (schema, dependencies, display name, etc.) so the stored
			// document stays in sync with the current binary. Admin-editable
			// fields (enabled, configValues, environments) are left untouched.
			if err := s.repo.RefreshMetadata(ctx, m); err != nil {
				s.logger.Error("SeedFromModules: failed to refresh metadata",
					slog.String("module", m.Name()),
					slog.String("error", err.Error()),
				)
			}
			// Clear needsRestart for loaded modules — this flag should only
			// remain set for modules that are enabled in DB but not loaded.
			if err := s.repo.ClearNeedsRestart(ctx, m.Name()); err != nil {
				s.logger.Error("SeedFromModules: failed to clear needsRestart",
					slog.String("module", m.Name()),
					slog.String("error", err.Error()),
				)
			}
			continue
		}

		// First boot for this module — create the config document with environments.
		doc := s.buildInitialConfig(m)
		if err := s.repo.Upsert(ctx, &doc); err != nil {
			s.logger.Error("SeedFromModules: failed to seed module config",
				slog.String("module", m.Name()),
				slog.String("error", err.Error()),
			)
			continue
		}

		s.logger.Info("Module config seeded",
			slog.String("module", m.Name()),
			slog.String("category", string(m.Category())),
			slog.Bool("enabled", doc.Enabled),
		)
	}

	return nil
}

// buildInitialConfig constructs a ModuleConfig from a module's declarations
// and current env vars. The enabled state comes from the module's own
// EnabledByDefault.
func (s *ModuleConfigService) buildInitialConfig(m Module) ModuleConfig {
	configValues := make(map[string]string)
	encryptedValues := make(map[string]string)

	schema := ConfigSchemaOf(m)
	for _, field := range schema {
		value := ""
		if field.EnvVar != "" {
			value = os.Getenv(field.EnvVar)
		}
		if value == "" {
			value = field.Default
		}
		if value == "" {
			continue
		}

		if field.Type == FieldSecret {
			encrypted, err := encryptSecret(value)
			if err != nil {
				s.logger.Warn("SeedFromModules: failed to encrypt secret, storing empty",
					slog.String("module", m.Name()),
					slog.String("field", field.Key),
					slog.String("error", err.Error()),
				)
				continue
			}
			encryptedValues[field.Key] = encrypted
		} else {
			configValues[field.Key] = value
		}
	}

	now := time.Now()
	environments := map[string]EnvironmentConfig{
		"production": {
			ConfigValues:    configValues,
			EncryptedValues: encryptedValues,
			UpdatedAt:       now,
		},
		"sandbox": {
			ConfigValues:    make(map[string]string),
			EncryptedValues: make(map[string]string),
			UpdatedAt:       now,
		},
	}

	enabled := EnabledByDefault(m)

	return ModuleConfig{
		ModuleName:        m.Name(),
		DisplayName:       DisplayNameOf(m),
		Description:       DescriptionOf(m),
		Category:          m.Category(),
		Enabled:           enabled,
		ConfigValues:      configValues,
		EncryptedValues:   encryptedValues,
		ConfigSchema:      schema,
		DependsOn:         DependenciesOf(m),
		ActiveEnvironment: "production",
		Environments:      environments,
	}
}

// GetConfig retrieves the full config document for a module. Used by admin API.
// If the module is registered but has no document in MongoDB (e.g. the
// collection was dropped while the backend was running), the config is
// rebuilt from the module's ConfigSchema and upserted before being returned.
// Legacy documents without an Environments map are lazily migrated.
func (s *ModuleConfigService) GetConfig(ctx context.Context, name string) (*ModuleConfig, error) {
	doc, err := s.repo.FindByName(ctx, name)
	if err != nil {
		return nil, err
	}
	if doc != nil {
		if err := s.ensureEnvironments(ctx, doc); err != nil {
			return nil, fmt.Errorf("module %q: migrate legacy config: %w", name, err)
		}
		return doc, nil
	}
	return s.lazySeed(ctx, name)
}

// ensureEnvironments lazily migrates a legacy document (no Environments map)
// by copying the top-level maps into a "production" profile and creating an
// empty "sandbox" profile, under a compare-and-swap. On success doc is
// updated in memory to match what was written (profiles AND the advanced
// configRevision). If another writer migrated or moved the document first,
// doc is REPLACED by a fresh read so the caller judges its own write against
// the current document rather than a stale legacy snapshot.
func (s *ModuleConfigService) ensureEnvironments(ctx context.Context, doc *ModuleConfig) error {
	if len(doc.Environments) > 0 {
		return nil // already migrated
	}
	cv := doc.ConfigValues
	if cv == nil {
		cv = make(map[string]string)
	}
	ev := doc.EncryptedValues
	if ev == nil {
		ev = make(map[string]string)
	}
	won, err := s.repo.MigrateToEnvironments(ctx, doc.ModuleName, cv, ev, doc.ConfigRevision)
	if err != nil {
		return err
	}
	if won {
		now := time.Now()
		doc.ActiveEnvironment = "production"
		doc.Environments = map[string]EnvironmentConfig{
			"production": {ConfigValues: cv, EncryptedValues: ev, UpdatedAt: now},
			"sandbox":    {ConfigValues: make(map[string]string), EncryptedValues: make(map[string]string), UpdatedAt: now},
		}
		doc.ConfigRevision++
		return nil
	}
	fresh, err := s.repo.FindByName(ctx, doc.ModuleName)
	if err != nil {
		return err
	}
	if fresh == nil {
		return fmt.Errorf("module %q not found", doc.ModuleName)
	}
	if len(fresh.Environments) == 0 {
		// The revision moved without a migration (a legacy-only backfill
		// landed). Retryable by the caller; never loop here.
		return fmt.Errorf("module %q: %w (profile migration)", doc.ModuleName, ErrRevisionStale)
	}
	*doc = *fresh
	return nil
}

// GetAllConfigs retrieves all module config documents. Used by admin API list endpoint.
// If the DB is missing documents for modules we know about, they are lazily
// rebuilt from each module's ConfigSchema so the admin UI never sees a
// partially-seeded catalog after a live DB wipe.
func (s *ModuleConfigService) GetAllConfigs(ctx context.Context) ([]ModuleConfig, error) {
	docs, err := s.repo.FindAll(ctx)
	if err != nil {
		return nil, err
	}

	// Drop orphan documents — modules no longer compiled into this binary
	// (addons a fork removed, or the ADR-0006 core-only collapse). Their routes
	// are gated/absent, so surfacing them in the admin UI is misleading. The
	// documents are left in the collection (non-destructive); they are simply
	// not returned. When knownModules is empty (service constructed without a
	// boot seed, e.g. some tooling paths) there is no registry to filter
	// against, so every document is returned unchanged.
	var present map[string]bool
	if len(s.knownModules) > 0 {
		docs, present = filterKnown(docs, s.knownModules)
	}

	// Lazily migrate any kept documents without environments.
	for i := range docs {
		if err := s.ensureEnvironments(ctx, &docs[i]); err != nil {
			s.logger.Warn("GetAllConfigs: failed to migrate environments",
				slog.String("module", docs[i].ModuleName), slog.String("error", err.Error()))
		}
	}

	// Lazy-seed any known module missing a document (e.g. after a dev DB wipe).
	for name := range s.knownModules {
		if present[name] {
			continue
		}
		if seeded, err := s.lazySeed(ctx, name); err == nil && seeded != nil {
			docs = append(docs, *seeded)
		}
	}
	return docs, nil
}

// filterKnown returns only the configs whose module is registered in known,
// alongside the set of module names that were present. Documents for modules
// absent from the binary's catalog (orphans) are dropped. Pure: no I/O.
func filterKnown(docs []ModuleConfig, known map[string]Module) ([]ModuleConfig, map[string]bool) {
	out := make([]ModuleConfig, 0, len(docs))
	present := make(map[string]bool, len(docs))
	for _, d := range docs {
		if _, ok := known[d.ModuleName]; !ok {
			continue
		}
		out = append(out, d)
		present[d.ModuleName] = true
	}
	return out, present
}

// lazySeed rebuilds a single module's config document from its declared
// schema and current env/defaults, then upserts it. Returns nil if the
// module is not registered with this service or if the seed write fails.
// Used by GetConfig/GetAllConfigs as a self-healing fallback for missing
// documents (empty collection after a dev DB wipe is the primary case).
func (s *ModuleConfigService) lazySeed(ctx context.Context, name string) (*ModuleConfig, error) {
	m, ok := s.knownModules[name]
	if !ok {
		return nil, nil
	}
	// Mirror first-boot seeding so a wiped collection rebuilds with the same
	// defaults the operator originally got.
	doc := s.buildInitialConfig(m)
	if err := s.repo.Upsert(ctx, &doc); err != nil {
		s.logger.Error("lazySeed: failed to upsert module config",
			slog.String("module", name),
			slog.String("error", err.Error()),
		)
		return nil, err
	}
	s.logger.Info("Module config lazy-seeded",
		slog.String("module", name),
		slog.String("category", string(m.Category())),
	)
	return s.repo.FindByName(ctx, name)
}

// UpdateConfig merges values and secrets into the ACTIVE environment profile
// and mirrors the result into the legacy top-level fields, in one
// compare-and-swap write. Keys not present in the call are preserved, never
// wiped — a toggle flip carries no secrets and must not blank the module's
// credentials.
//
// Profiles are the source of truth; the legacy maps are a compatibility
// mirror. The merge, the validation snapshot and the write all use the
// active profile, so a pre-existing divergence between profile and mirror
// is repaired by the write rather than perpetuated. A legacy document with
// no profiles completes its lazy migration first — itself a compare-and-swap
// — so the revision the write is judged against is the migrated document's.
func (s *ModuleConfigService) UpdateConfig(ctx context.Context, name string, values map[string]string, secrets map[string]string) error {
	doc, err := s.repo.FindByName(ctx, name)
	if err != nil {
		return err
	}
	if doc == nil {
		return fmt.Errorf("module %q not found", name)
	}
	// ensureEnvironments migrates under its own compare-and-swap and leaves
	// doc current — profiles, activeEnvironment and configRevision — whether
	// this call won the migration or re-read after another writer did.
	if err := s.ensureEnvironments(ctx, doc); err != nil {
		return err
	}
	env := doc.ActiveEnv()
	cur, ok := doc.Environments[env]
	if !ok {
		return fmt.Errorf("environment %q not found for module %q", env, name)
	}
	schema := s.schemaFor(name, doc)
	if err := validateSubmittedKeys(schema, values, secrets); err != nil {
		return err
	}
	mergedValues := mergeStringMaps(cur.ConfigValues, values)

	if err := s.validateCandidate(ctx, name, candidate{
		schema: schema, env: env, values: mergedValues,
		storedEncrypted: cur.EncryptedValues, submittedSecrets: secrets,
	}); err != nil {
		return err
	}

	encrypted, err := encryptAll(secrets)
	if err != nil {
		return err
	}
	mergedSecrets := mergeStringMaps(cur.EncryptedValues, encrypted)

	won, err := s.repo.CompareAndSwapConfig(ctx, name, ConfigMutation{
		ExpectedRevision: doc.ConfigRevision,
		Env:              env, EnvValues: mergedValues, EnvSecrets: mergedSecrets, EnvRevision: cur.Revision,
		WriteLegacy: true, LegacyValues: mergedValues, LegacySecrets: mergedSecrets,
		NeedsRestart: s.needsRestartFor(name),
	})
	if err != nil {
		return err
	}
	if !won {
		return ErrRevisionStale
	}
	// No cache invalidation: Redis caches only the enabled flag, which a
	// config write does not change. The CAS is the commit; nothing after it
	// may turn a committed write into a reported failure.
	return nil
}

// mergeStringMaps returns a new map containing every key in base overlaid with
// every key in overlay (overlay wins on conflict). base is never mutated and a
// nil base or overlay is treated as empty. It exists so partial config/secret
// updates preserve the keys they don't mention instead of replacing the whole
// map — see UpdateConfig / UpdateEnvironmentConfig.
func mergeStringMaps(base, overlay map[string]string) map[string]string {
	out := make(map[string]string, len(base)+len(overlay))
	for k, v := range base {
		out[k] = v
	}
	for k, v := range overlay {
		out[k] = v
	}
	return out
}

// UpdateEnvironmentConfig merges values and secrets into ONE named profile
// in one compare-and-swap write. When that profile is the active one the
// legacy mirror is synced in the same update; otherwise the mirror and the
// active profile are untouched. The module's validation seam sees the
// merged target profile — this surface must not be a bypass around the
// active-config PATCH.
func (s *ModuleConfigService) UpdateEnvironmentConfig(ctx context.Context, name, envName string, values map[string]string, secrets map[string]string) error {
	doc, err := s.GetConfig(ctx, name)
	if err != nil {
		return err
	}
	if doc == nil {
		return fmt.Errorf("module %q not found", name)
	}
	cur, ok := doc.Environments[envName]
	if !ok {
		return fmt.Errorf("environment %q not found for module %q", envName, name)
	}
	schema := s.schemaFor(name, doc)
	if err := validateSubmittedKeys(schema, values, secrets); err != nil {
		return err
	}
	mergedValues := mergeStringMaps(cur.ConfigValues, values)

	if err := s.validateCandidate(ctx, name, candidate{
		schema: schema, env: envName, values: mergedValues,
		storedEncrypted: cur.EncryptedValues, submittedSecrets: secrets,
	}); err != nil {
		return err
	}

	encrypted, err := encryptAll(secrets)
	if err != nil {
		return err
	}
	mergedSecrets := mergeStringMaps(cur.EncryptedValues, encrypted)

	mut := ConfigMutation{
		ExpectedRevision: doc.ConfigRevision,
		Env:              envName, EnvValues: mergedValues, EnvSecrets: mergedSecrets, EnvRevision: cur.Revision,
		NeedsRestart: s.needsRestartFor(name),
	}
	if envName == doc.ActiveEnv() {
		mut.WriteLegacy, mut.LegacyValues, mut.LegacySecrets = true, mergedValues, mergedSecrets
	}
	won, err := s.repo.CompareAndSwapConfig(ctx, name, mut)
	if err != nil {
		return err
	}
	if !won {
		return ErrRevisionStale
	}
	return nil
}

// SetActiveEnvironment switches the active profile in one compare-and-swap
// pipeline update that also copies the target's STORED values and secrets
// into the legacy mirror server-side. The module's validation seam judges
// the stored target profile as a whole — with the target's own secret
// presence, never the currently active profile's — strictly before the
// write, so a refused activation leaves the active name, the mirror and
// needsRestart exactly as they were.
func (s *ModuleConfigService) SetActiveEnvironment(ctx context.Context, name, envName string) error {
	doc, err := s.GetConfig(ctx, name)
	if err != nil {
		return err
	}
	if doc == nil {
		return fmt.Errorf("module %q not found", name)
	}
	target, ok := doc.Environments[envName]
	if !ok {
		return fmt.Errorf("environment %q not found for module %q", envName, name)
	}
	cv := target.ConfigValues
	if cv == nil {
		cv = make(map[string]string)
	}
	if err := s.validateCandidate(ctx, name, candidate{
		schema: s.schemaFor(name, doc), env: envName, values: cv,
		storedEncrypted: target.EncryptedValues, activation: true,
	}); err != nil {
		return err
	}
	won, err := s.repo.CompareAndSwapConfig(ctx, name, ConfigMutation{
		ExpectedRevision: doc.ConfigRevision, Activate: envName,
		NeedsRestart: s.needsRestartFor(name),
	})
	if err != nil {
		return err
	}
	if !won {
		return ErrRevisionStale
	}
	return nil
}

// GetEnvironmentConfig retrieves config values and secret status for a specific environment.
func (s *ModuleConfigService) GetEnvironmentConfig(ctx context.Context, name, envName string) (*EnvironmentConfig, map[string]bool, error) {
	doc, err := s.GetConfig(ctx, name)
	if err != nil {
		return nil, nil, err
	}
	if doc == nil {
		return nil, nil, fmt.Errorf("module %q not found", name)
	}

	env, ok := doc.Environments[envName]
	if !ok {
		return nil, nil, fmt.Errorf("environment %q not found for module %q", envName, name)
	}

	// Build secret status map. A record list's secrets live one per element,
	// at <field>.<slug>.<sub> — iterating the declared schema alone reports
	// every one of them as unset, and the operator retypes a credential the
	// deployment already holds.
	secretStatus := make(map[string]bool)
	for _, field := range doc.ConfigSchema {
		if field.Type == FieldSecret {
			_, hasValue := env.EncryptedValues[field.Key]
			secretStatus[field.Key] = hasValue
			continue
		}
		if field.Type != FieldRecordList {
			continue
		}
		for _, slug := range ParseRoster(env.ConfigValues, field.Key) {
			for _, item := range field.Items {
				if item.Type != FieldSecret {
					continue
				}
				key := ItemKey(field.Key, slug, item.Key)
				_, hasValue := env.EncryptedValues[key]
				secretStatus[key] = hasValue
			}
		}
	}

	return &env, secretStatus, nil
}

// UpdateEnabled persists a module's enabled state. The Redis-cached enabled
// flag is invalidated best-effort: the persisted state is the truth, the
// ModuleGate self-corrects within the 30-second cache TTL, and a Redis
// hiccup must not report a committed write as a failure.
func (s *ModuleConfigService) UpdateEnabled(ctx context.Context, name string, enabled bool) error {
	if s.coreModules[name] {
		return fmt.Errorf("cannot disable core module %q", name)
	}
	if err := s.repo.UpdateEnabled(ctx, name, enabled); err != nil {
		return err
	}
	if err := s.InvalidateCache(ctx, name); err != nil {
		s.logger.Warn("UpdateEnabled: failed to invalidate the enabled-flag cache; the gate converges within the cache TTL",
			slog.String("module", name), slog.String("error", err.Error()))
	}
	return nil
}

// InvalidateCache removes the Redis cached enabled state for a module,
// forcing the next IsEnabled call to re-fetch from MongoDB.
func (s *ModuleConfigService) InvalidateCache(ctx context.Context, name string) error {
	return s.redis.Del(ctx, enabledCachePrefix+name)
}

// ClearNeedsRestart resets the needsRestart flag for a module.
func (s *ModuleConfigService) ClearNeedsRestart(ctx context.Context, name string) error {
	return s.repo.ClearNeedsRestart(ctx, name)
}

// --- Config value readers (used by modules in Init) ---

// GetValue returns a plain config value for a module.
// Lookup: active environment ConfigValues → legacy ConfigValues → env var (from schema) → schema default → "".
func (s *ModuleConfigService) GetValue(ctx context.Context, moduleName, key string) string {
	doc, err := s.repo.FindByName(ctx, moduleName)
	if err != nil || doc == nil {
		return s.fallbackFromSchema(moduleName, key)
	}

	// Prefer active environment values.
	configValues := doc.ActiveConfigValues()
	if v, ok := configValues[key]; ok && v != "" {
		return v
	}
	return s.schemaFallback(doc.ConfigSchema, key)
}

// GetRawValue reports a module's stored non-secret config value together with
// whether the key is actually present in the active environment — WITHOUT
// GetValue's empty-means-absent collapse.
//
// GetValue answers "what value should I use", and folding an operator-cleared
// key into the schema default is right for that question. This answers the
// different question "did the operator say anything here", which a field whose
// empty value is itself a decision needs. See ADR-0017 D1: clearing
// sessionAbsoluteTTL disables the session cap, and GetValue cannot express that.
//
// THREE outcomes, and callers must keep them distinct:
//
//   - ("", false, nil)   — the read succeeded and the key is absent.
//   - (v,  true,  nil)   — the read succeeded; v may legitimately be "".
//   - ("", false, err)   — the read FAILED. Nothing is known about the key.
//
// The error is returned rather than folded into the presence flag because a
// caller whose "absent" branch substitutes a default would otherwise apply
// that default during a transient module_configs outage — silently swapping in
// a different policy exactly when it cannot verify the configured one. For
// sessionAbsoluteTTL that means re-enabling a cap an operator deliberately
// disabled, and the consequence is an irreversible sign-out of every session
// older than the default. A caller governing credentials should fail closed on
// err, not guess.
func (s *ModuleConfigService) GetRawValue(ctx context.Context, moduleName, key string) (string, bool, error) {
	doc, err := s.repo.FindByName(ctx, moduleName)
	if err != nil {
		return "", false, err
	}
	if doc == nil {
		// Not an error: a module with no config document has said nothing
		// about any key, which is exactly the "absent" answer.
		return "", false, nil
	}
	v, ok := doc.ActiveConfigValues()[key]
	return v, ok, nil
}

// GetSecret returns a decrypted secret config value for a module.
// Lookup: active environment EncryptedValues (decrypt) → legacy EncryptedValues → env var → schema default → "".
func (s *ModuleConfigService) GetSecret(ctx context.Context, moduleName, key string) string {
	doc, err := s.repo.FindByName(ctx, moduleName)
	if err != nil || doc == nil {
		return s.fallbackFromSchema(moduleName, key)
	}

	// Prefer active environment encrypted values.
	encryptedValues := doc.ActiveEncryptedValues()
	if enc, ok := encryptedValues[key]; ok && enc != "" {
		decrypted, err := decryptSecret(enc)
		if err != nil {
			s.logger.Warn("GetSecret: failed to decrypt, falling back to env",
				slog.String("module", moduleName), slog.String("key", key))
			return s.schemaFallback(doc.ConfigSchema, key)
		}
		return decrypted
	}

	// Secret might not be in encrypted store yet — try plain values or env fallback
	return s.schemaFallback(doc.ConfigSchema, key)
}

// schemaFallback looks up a key in the config schema and returns env var value or default.
func (s *ModuleConfigService) schemaFallback(schema []ConfigField, key string) string {
	for _, f := range schema {
		if f.Key == key {
			if f.EnvVar != "" {
				if v := os.Getenv(f.EnvVar); v != "" {
					return v
				}
			}
			return f.Default
		}
	}
	return ""
}

// fallbackFromSchema is used when DB lookup fails entirely — searches all module schemas.
func (s *ModuleConfigService) fallbackFromSchema(moduleName, key string) string {
	// Without DB doc we have no schema; just try env var naming convention
	return ""
}

// ModuleStatusInfo provides a summary for the admin API list endpoint.
type ModuleStatusInfo struct {
	Name        string         `json:"name"`
	DisplayName string         `json:"displayName"`
	Description string         `json:"description"`
	Category    ModuleCategory `json:"category"`
	Enabled     bool           `json:"enabled"`
	DependsOn   []string       `json:"dependsOn,omitempty"`
	HasConfig   bool           `json:"hasConfig"`
	FieldCount  int            `json:"fieldCount"`
}

// ModuleStatusJSON returns a JSON-serializable summary of all module configs.
// Used by health/status endpoints.
func (s *ModuleConfigService) ModuleStatusJSON(ctx context.Context) ([]byte, error) {
	configs, err := s.repo.FindAll(ctx)
	if err != nil {
		return nil, err
	}
	// Exclude orphan documents for modules not compiled into this binary —
	// same rationale as GetAllConfigs. See filterKnown.
	if len(s.knownModules) > 0 {
		configs, _ = filterKnown(configs, s.knownModules)
	}

	infos := make([]ModuleStatusInfo, len(configs))
	for i, c := range configs {
		infos[i] = ModuleStatusInfo{
			Name:        c.ModuleName,
			DisplayName: c.DisplayName,
			Description: c.Description,
			Category:    c.Category,
			Enabled:     c.Enabled,
			DependsOn:   c.DependsOn,
			HasConfig:   len(c.ConfigValues) > 0 || len(c.EncryptedValues) > 0,
			FieldCount:  len(c.ConfigSchema),
		}
	}
	return json.Marshal(infos)
}
