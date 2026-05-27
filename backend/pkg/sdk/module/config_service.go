package module

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"time"
)

// profileAddons maps the ORKESTRA_PROFILE values accepted by the first-boot
// seeder to the addons pre-enabled on first boot. Every binary compiles
// every addon; the profile only decides what is *enabled* on a fresh
// install.
//
//   - "minimal": no addons pre-enabled. Core-only deployment; the operator
//     turns on each addon explicitly at /admin/modules.
//   - "full": the "*" sentinel expands at runtime to every non-core addon
//     except dev (which keeps its own !IsProduction() gate so it never
//     auto-enables on a production-tagged compose).
//
// On subsequent boots (existing module_configs document found) the override
// is ignored entirely; admin-set values are authoritative.
var profileAddons = map[string][]string{
	"minimal": {},
	"full":    {"*"},
}

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

	// ORKESTRA_PROFILE override applies only to modules that don't yet have a
	// config document. Computed once so logging fires at most once per boot.
	profileOverride := s.computeProfileOverride(modules)

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
		doc := s.buildInitialConfig(m, profileOverride)
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

// computeProfileOverride resolves ORKESTRA_PROFILE to a set of addon names
// that should be marked enabled on first boot. Returns nil when the env var
// is unset or names an unknown profile — callers fall back to each module's
// own Enabled() in that case.
//
// Core modules and the dev addon are excluded from the override regardless
// of profile: core is always on, dev keeps its own !IsProduction() gate.
func (s *ModuleConfigService) computeProfileOverride(modules []Module) map[string]bool {
	profile := strings.TrimSpace(os.Getenv("ORKESTRA_PROFILE"))
	if profile == "" {
		return nil
	}
	addons, ok := profileAddons[profile]
	if !ok {
		s.logger.Warn("ORKESTRA_PROFILE: unknown profile, ignoring",
			slog.String("profile", profile),
		)
		return nil
	}

	enabled := make(map[string]bool)
	if len(addons) == 1 && addons[0] == "*" {
		// "full" sentinel — every non-core addon except dev.
		for _, m := range modules {
			if m.Category() == CategoryCore || m.Name() == "dev" {
				continue
			}
			enabled[m.Name()] = true
		}
	} else {
		for _, name := range addons {
			enabled[name] = true
		}
	}

	s.logger.Info("ORKESTRA_PROFILE: pre-enabling addons on first boot",
		slog.String("profile", profile),
		slog.Int("count", len(enabled)),
	)
	return enabled
}

// buildInitialConfig constructs a ModuleConfig from a module's declarations
// and current env vars. When profileOverride is non-nil, non-core, non-dev
// addons take their enabled state from the override map (true if listed,
// false otherwise) instead of m.Enabled().
func (s *ModuleConfigService) buildInitialConfig(m Module, profileOverride map[string]bool) ModuleConfig {
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
	if profileOverride != nil && m.Category() != CategoryCore && m.Name() != "dev" {
		enabled = profileOverride[m.Name()]
	}

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
	// Lazily migrate any documents without environments.
	for i := range docs {
		if err := s.ensureEnvironments(ctx, &docs[i]); err != nil {
			s.logger.Warn("GetAllConfigs: failed to migrate environments",
				slog.String("module", docs[i].ModuleName), slog.String("error", err.Error()))
		}
	}
	if len(s.knownModules) == 0 {
		return docs, nil
	}
	present := make(map[string]bool, len(docs))
	for _, d := range docs {
		present[d.ModuleName] = true
	}
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
	// SKU defaults the operator originally got.
	moduleSlice := make([]Module, 0, len(s.knownModules))
	for _, mm := range s.knownModules {
		moduleSlice = append(moduleSlice, mm)
	}
	profileOverride := s.computeProfileOverride(moduleSlice)
	doc := s.buildInitialConfig(m, profileOverride)
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

// UpdateConfig updates a module's config values and encrypted secrets for the
// active environment, then invalidates the Redis cache for immediate propagation.
// Also keeps the legacy top-level fields in sync for backward compatibility.
func (s *ModuleConfigService) UpdateConfig(ctx context.Context, name string, values map[string]string, secrets map[string]string) error {
	encrypted := make(map[string]string, len(secrets))
	for k, v := range secrets {
		enc, err := encryptSecret(v)
		if err != nil {
			return fmt.Errorf("encrypt secret %q: %w", k, err)
		}
		encrypted[k] = enc
	}

	// Update legacy top-level fields for backward compat.
	if err := s.repo.UpdateConfigValues(ctx, name, values, encrypted); err != nil {
		return err
	}

	// Also update the active environment if environments exist.
	doc, err := s.repo.FindByName(ctx, name)
	if err == nil && doc != nil && len(doc.Environments) > 0 {
		activeEnv := doc.ActiveEnv()
		if err := s.repo.UpdateEnvironmentConfig(ctx, name, activeEnv, values, encrypted); err != nil {
			s.logger.Warn("UpdateConfig: failed to update environment config",
				slog.String("module", name), slog.String("env", activeEnv), slog.String("error", err.Error()))
		}
	}

	return s.InvalidateCache(ctx, name)
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

	// Merge with existing env values (don't wipe unset fields).
	existingEnv := doc.Environments[envName]
	mergedValues := existingEnv.ConfigValues
	if mergedValues == nil {
		mergedValues = make(map[string]string)
	}
	for k, v := range values {
		mergedValues[k] = v
	}

	mergedEncrypted := existingEnv.EncryptedValues
	if mergedEncrypted == nil {
		mergedEncrypted = make(map[string]string)
	}
	for k, v := range secrets {
		enc, err := encryptSecret(v)
		if err != nil {
			return fmt.Errorf("encrypt secret %q: %w", k, err)
		}
		mergedEncrypted[k] = enc
	}

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
