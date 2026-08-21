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
	repo        *ModuleConfigRepository
	redis       RedisClient
	logger      *slog.Logger
	coreModules map[string]bool // precomputed set — never hits DB/Redis

	// knownModules is captured during SeedFromModules so the service can
	// lazy-rebuild the module_configs collection if it is emptied at
	// runtime (dev DB wipe, accidental drop, etc.) without requiring a
	// backend restart. Populated once at boot and then read-only.
	knownModules map[string]Module
}

const (
	enabledCachePrefix = "module:enabled:"
	enabledCacheTTL    = 30 * time.Second
)

// NewModuleConfigService creates a new config service.
func NewModuleConfigService(repo *ModuleConfigRepository, redis RedisClient, logger *slog.Logger) *ModuleConfigService {
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
			s.logger.Warn("GetConfig: failed to migrate environments",
				slog.String("module", name), slog.String("error", err.Error()))
		}
		return doc, nil
	}
	return s.lazySeed(ctx, name)
}

// ensureEnvironments lazily migrates a legacy document (no Environments map)
// by copying the top-level ConfigValues/EncryptedValues into a "production"
// environment profile and creating an empty "sandbox" profile.
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

	if err := s.repo.MigrateToEnvironments(ctx, doc.ModuleName, cv, ev); err != nil {
		return err
	}

	// Update in-memory doc so callers see the migration immediately.
	now := time.Now()
	doc.ActiveEnvironment = "production"
	doc.Environments = map[string]EnvironmentConfig{
		"production": {ConfigValues: cv, EncryptedValues: ev, UpdatedAt: now},
		"sandbox":    {ConfigValues: make(map[string]string), EncryptedValues: make(map[string]string), UpdatedAt: now},
	}
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

// validateModuleConfig runs the module's optional ValidateConfig hook
// against exactly the non-secret values the update would persist.
// Modules that are unknown to this service, or that omit the seam, are
// accepted unchanged. ADR-0017 D6.
func (s *ModuleConfigService) validateModuleConfig(ctx context.Context, name string, merged map[string]string) error {
	m, ok := s.knownModules[name]
	if !ok {
		return nil
	}
	v, ok := m.(HasConfigValidator)
	if !ok {
		return nil
	}
	return v.ValidateConfig(ctx, merged)
}

// UpdateConfig updates a module's config values and encrypted secrets for the
// active environment, then invalidates the Redis cache for immediate propagation.
// Also keeps the legacy top-level fields in sync for backward compatibility.
//
// Incoming values and secrets are MERGED into the module's stored config — keys
// not present in the call are preserved, never wiped. Pass only the keys you
// want to add or change. This guard is load-bearing: a config-only update
// (e.g. flipping a feature toggle) carries no secrets, and replacing rather
// than merging would blank out every encrypted secret the module holds.
func (s *ModuleConfigService) UpdateConfig(ctx context.Context, name string, values map[string]string, secrets map[string]string) error {
	// Load first: the module validator must see the merged document, and
	// nothing may be encrypted or written before it has accepted it. The
	// legacy top-level fields and the active environment can diverge, so
	// each target is merged against its own existing maps below — the
	// validator sees the legacy top-level merge, since that is the map the
	// admin API has historically treated as the config of record.
	existing, err := s.repo.FindByName(ctx, name)
	if err != nil {
		return err
	}
	if existing == nil {
		return fmt.Errorf("module %q not found", name)
	}

	mergedValues := mergeStringMaps(existing.ConfigValues, values)
	if err := s.validateModuleConfig(ctx, name, mergedValues); err != nil {
		return err
	}

	encrypted := make(map[string]string, len(secrets))
	for k, v := range secrets {
		enc, err := encryptSecret(v)
		if err != nil {
			return fmt.Errorf("encrypt secret %q: %w", k, err)
		}
		encrypted[k] = enc
	}

	// Update legacy top-level fields for backward compat.
	if err := s.repo.UpdateConfigValues(
		ctx, name,
		mergedValues,
		mergeStringMaps(existing.EncryptedValues, encrypted),
	); err != nil {
		return err
	}

	// Also update the active environment if environments exist.
	if len(existing.Environments) > 0 {
		activeEnv := existing.ActiveEnv()
		env := existing.Environments[activeEnv]
		if err := s.repo.UpdateEnvironmentConfig(
			ctx, name, activeEnv,
			mergeStringMaps(env.ConfigValues, values),
			mergeStringMaps(env.EncryptedValues, encrypted),
		); err != nil {
			s.logger.Warn("UpdateConfig: failed to update environment config",
				slog.String("module", name), slog.String("env", activeEnv), slog.String("error", err.Error()))
		}
	}

	return s.InvalidateCache(ctx, name)
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

// UpdateEnvironmentConfig updates config values and secrets for a specific
// named environment. If updating the active environment, also syncs legacy fields.
func (s *ModuleConfigService) UpdateEnvironmentConfig(ctx context.Context, name, envName string, values map[string]string, secrets map[string]string) error {
	// Ensure the module exists and has environments.
	doc, err := s.GetConfig(ctx, name)
	if err != nil {
		return err
	}
	if doc == nil {
		return fmt.Errorf("module %q not found", name)
	}

	// Verify environment exists.
	if _, ok := doc.Environments[envName]; !ok {
		return fmt.Errorf("environment %q not found for module %q", envName, name)
	}

	// Merge with existing env values (don't wipe unset fields), then let the
	// module's optional validator see the merged result before anything is
	// encrypted or persisted. This is the named-environment PATCH surface;
	// it must not be a bypass around the active-config PATCH's validation.
	existingEnv := doc.Environments[envName]
	mergedValues := mergeStringMaps(existingEnv.ConfigValues, values)
	if err := s.validateModuleConfig(ctx, name, mergedValues); err != nil {
		return err
	}

	encrypted := make(map[string]string, len(secrets))
	for k, v := range secrets {
		enc, err := encryptSecret(v)
		if err != nil {
			return fmt.Errorf("encrypt secret %q: %w", k, err)
		}
		encrypted[k] = enc
	}

	mergedEncrypted := mergeStringMaps(existingEnv.EncryptedValues, encrypted)

	if err := s.repo.UpdateEnvironmentConfig(ctx, name, envName, mergedValues, mergedEncrypted); err != nil {
		return err
	}

	// If this is the active environment, also sync legacy top-level fields.
	if envName == doc.ActiveEnv() {
		if err := s.repo.UpdateConfigValues(ctx, name, mergedValues, mergedEncrypted); err != nil {
			s.logger.Warn("UpdateEnvironmentConfig: failed to sync legacy fields",
				slog.String("module", name), slog.String("error", err.Error()))
		}
	}

	return s.InvalidateCache(ctx, name)
}

// SetActiveEnvironment switches the active environment for a module and syncs
// the active environment's config to the legacy top-level fields.
func (s *ModuleConfigService) SetActiveEnvironment(ctx context.Context, name, envName string) error {
	doc, err := s.GetConfig(ctx, name)
	if err != nil {
		return err
	}
	if doc == nil {
		return fmt.Errorf("module %q not found", name)
	}
	if _, ok := doc.Environments[envName]; !ok {
		return fmt.Errorf("environment %q not found for module %q", envName, name)
	}

	if err := s.repo.SetActiveEnvironment(ctx, name, envName); err != nil {
		return err
	}

	// Sync the newly active environment's values to legacy top-level fields.
	env := doc.Environments[envName]
	cv := env.ConfigValues
	if cv == nil {
		cv = make(map[string]string)
	}
	ev := env.EncryptedValues
	if ev == nil {
		ev = make(map[string]string)
	}
	if err := s.repo.UpdateConfigValues(ctx, name, cv, ev); err != nil {
		s.logger.Warn("SetActiveEnvironment: failed to sync legacy fields",
			slog.String("module", name), slog.String("error", err.Error()))
	}

	return s.InvalidateCache(ctx, name)
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

	// Build secret status map.
	secretStatus := make(map[string]bool)
	for _, field := range doc.ConfigSchema {
		if field.Type == FieldSecret {
			_, hasValue := env.EncryptedValues[field.Key]
			secretStatus[field.Key] = hasValue
		}
	}

	return &env, secretStatus, nil
}

// UpdateEnabled toggles a module's enabled state and invalidates the cache.
func (s *ModuleConfigService) UpdateEnabled(ctx context.Context, name string, enabled bool) error {
	if s.coreModules[name] {
		return fmt.Errorf("cannot disable core module %q", name)
	}
	if err := s.repo.UpdateEnabled(ctx, name, enabled); err != nil {
		return err
	}
	return s.InvalidateCache(ctx, name)
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
