package module

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"os"
	"sort"
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

	// requiredPersisted names modules whose config document must exist for
	// the rest of the process: after boot seeding, a missing document for
	// one of them is an outage — GetConfig fails and the list shows a
	// `missing` row — never a reason to rebuild it from schema defaults.
	// The strict credential-policy readers in auth depend on this: a lazy
	// re-seed from an admin page read would recreate a permissive default
	// exactly when the reader had correctly observed the outage. Populated
	// once by RequirePersistedConfig before traffic and read-only after.
	requiredPersisted map[string]bool
	requiredSealed    bool
	// seedFailures records, per module, the last boot-seeding or backfill
	// failure SeedFromModules logged (nil-free when everything succeeded).
	// RequirePersistedConfig consults it: a required module whose seed
	// failed must stop the boot, not serve a possibly incomplete document.
	seedFailures map[string]error
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
		repo:              repo,
		redis:             redis,
		logger:            logger,
		coreModules:       make(map[string]bool),
		knownModules:      make(map[string]Module),
		requiredPersisted: make(map[string]bool),
		seedFailures:      make(map[string]error),
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
			s.seedFailures[m.Name()] = err
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
				// A stale stored schema is what the live-schema rule guards
				// against; for a required module it is not a warning.
				s.seedFailures[m.Name()] = err
			}
			// Backfill: RefreshMetadata refreshes the SCHEMA but has never
			// added the keys a schema gained after the document was created,
			// so a runtime read of such a key had to guess a default. After
			// this, every schema key with a non-empty fallback is present in
			// the active profile and the legacy mirror, and the runtime, the
			// validator and the admin UI all read the same document.
			keys, wrote, err := s.backfillSchemaKeys(ctx, m, existing)
			switch {
			case err != nil:
				// Recorded so RequirePersistedConfig can refuse to serve a
				// required module whose document may be incomplete.
				s.seedFailures[m.Name()] = err
				s.logger.Error("SeedFromModules: failed to backfill schema keys",
					slog.String("module", m.Name()),
					slog.String("error", err.Error()),
				)
			case len(keys) > 0:
				s.logger.Info("Module config backfilled with schema defaults",
					slog.String("module", m.Name()),
					slog.Any("keys", keys),
				)
			case wrote:
				s.logger.Info("Module config legacy mirror realigned to the active profile",
					slog.String("module", m.Name()),
				)
			}
			// Clear needsRestart for loaded modules — this flag should only
			// remain set for modules that are enabled in DB but not loaded. A
			// backfill write already persisted needsRestart=false in its own
			// update, so the second write is owed only when nothing was written.
			if !wrote {
				if err := s.repo.ClearNeedsRestart(ctx, m.Name()); err != nil {
					s.logger.Error("SeedFromModules: failed to clear needsRestart",
						slog.String("module", m.Name()),
						slog.String("error", err.Error()),
					)
				}
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
			s.seedFailures[m.Name()] = err
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

// backfillMaxAttempts bounds the boot backfill's compare-and-swap retry. A
// lost race means a replica booted concurrently; re-reading and recomputing
// converges in one step, and three is plenty.
const backfillMaxAttempts = 3

// backfillSchemaKeys writes every schema key that is absent from the ACTIVE
// profile AND has a non-empty EnvVar/Default, then rewrites the legacy
// mirror to exactly the resulting profile, in ONE compare-and-swap that
// advances configRevision once. The profile is the source of truth (it is
// what ActiveConfigValues serves to the runtime and the admin UI); the
// mirror is never backfilled on its own, so a value present only in the
// profile reaches the mirror as that value, not as a schema default, and a
// mirror that had diverged is realigned. Secrets go through the same
// encrypted path first-boot seeding uses, each encrypted ONCE so the profile
// and the mirror carry identical ciphertext. Present profile keys, explicit
// empty strings included, are never touched.
//
// Keys whose fallback is empty stay ABSENT on purpose: absence is meaningful
// to GetRawValue readers (ADR-0017 D1 — an absent sessionAbsoluteTTL is the
// default cap, a present "" is "cap disabled"), so inventing "" would
// silently change policy. Record lists are schema-level constructs with
// nothing to seed. A document with no profiles gets only its mirror
// backfilled — the lazy migration copies it into the production profile
// later.
//
// NeedsRestart is written false: seeding runs before any module's Init, so
// every module reads the backfilled document and no restart is owed — the
// hot-reload resolver governs post-boot edits, not this one. Writing it here
// folds the ClearNeedsRestart that would otherwise follow into the same
// update.
//
// A lost compare-and-swap means a concurrently booting replica wrote first;
// the document is re-read and the missing set recomputed — that replica may
// run an older binary whose schema knows fewer keys. Returns the sorted,
// de-duplicated key names written; nil when nothing was missing (no write).
func (s *ModuleConfigService) backfillSchemaKeys(ctx context.Context, m Module, doc *ModuleConfig) (keys []string, wrote bool, err error) {
	schema := ConfigSchemaOf(m)
	for attempt := 0; attempt < backfillMaxAttempts; attempt++ {
		mut, added, write, err := s.buildBackfill(m, schema, doc)
		if err != nil {
			return nil, false, err
		}
		if !write {
			return nil, false, nil
		}
		won, err := s.repo.CompareAndSwapConfig(ctx, m.Name(), mut)
		if err != nil {
			return nil, false, err
		}
		if won {
			return added, true, nil
		}
		fresh, err := s.repo.FindByName(ctx, m.Name())
		if err != nil {
			return nil, false, err
		}
		if fresh == nil {
			return nil, false, fmt.Errorf("module %q: document disappeared during backfill", m.Name())
		}
		doc = fresh
	}
	return nil, false, fmt.Errorf("module %q: %w (backfill)", m.Name(), ErrRevisionStale)
}

// buildBackfill computes the mutation for one document. Profiles are the
// source of truth: the candidate is the ACTIVE profile plus its missing
// defaults, and the legacy mirror is rewritten to exactly that candidate —
// never backfilled on its own, which would hand a key present only in the
// profile a schema default instead of the profile's value. A document with
// no profiles (not yet migrated) backfills its mirror alone. Every secret
// is encrypted once. Returns the mutation, the keys added to the profile
// (or mirror, for a legacy document), and whether anything needs writing —
// a mirror that merely diverged from a complete profile is realigned too.
func (s *ModuleConfigService) buildBackfill(m Module, schema []ConfigField, doc *ModuleConfig) (mut ConfigMutation, added []string, write bool, err error) {
	ciphertext := map[string]string{} // key → ciphertext, encrypted once per key
	encryptOnce := func(f ConfigField, plain string) (string, error) {
		if enc, ok := ciphertext[f.Key]; ok {
			return enc, nil
		}
		enc, err := encryptSecret(plain)
		if err != nil {
			// NOT the first-boot posture (warn and skip). A backfill that
			// silently omits a secret would report success and let the
			// required-module gate serve an incomplete document.
			return "", fmt.Errorf("encrypt backfilled secret %q: %w", f.Key, err)
		}
		ciphertext[f.Key] = enc
		return enc, nil
	}
	mut = ConfigMutation{ExpectedRevision: doc.ConfigRevision, NeedsRestart: false}

	if len(doc.Environments) == 0 {
		values, secrets, keys, err := missingSchemaKeys(schema, doc.ConfigValues, doc.EncryptedValues, encryptOnce)
		if err != nil {
			return mut, nil, false, err
		}
		if len(keys) == 0 {
			return mut, nil, false, nil
		}
		mut.WriteLegacy, mut.LegacyValues, mut.LegacySecrets = true, values, secrets
		sort.Strings(keys)
		return mut, keys, true, nil
	}

	env := doc.ActiveEnv()
	cur, ok := doc.Environments[env]
	if !ok {
		return mut, nil, false, nil
	}
	values, secrets, keys, err := missingSchemaKeys(schema, cur.ConfigValues, cur.EncryptedValues, encryptOnce)
	if err != nil {
		return mut, nil, false, err
	}
	mirrorDiverged := !maps.Equal(doc.ConfigValues, values) || !maps.Equal(doc.EncryptedValues, secrets)
	if len(keys) == 0 && !mirrorDiverged {
		return mut, nil, false, nil
	}
	mut.Env, mut.EnvValues, mut.EnvSecrets, mut.EnvRevision = env, values, secrets, cur.Revision
	mut.WriteLegacy, mut.LegacyValues, mut.LegacySecrets = true, values, secrets
	sort.Strings(keys) // missingSchemaKeys adds each schema key at most once
	return mut, keys, true, nil
}

// missingSchemaKeys returns copies of values/secrets with every absent
// schema key whose EnvVar/Default is non-empty added, plus the keys added.
// Secrets are obtained through encryptOnce so a key missing from both the
// profile and the mirror is encrypted a single time; an encryption failure
// is the caller's failure, never a silently skipped key.
func missingSchemaKeys(schema []ConfigField, values, encrypted map[string]string, encryptOnce func(ConfigField, string) (string, error)) (map[string]string, map[string]string, []string, error) {
	outValues := mergeStringMaps(values, nil)
	outSecrets := mergeStringMaps(encrypted, nil)
	var added []string
	for _, f := range schema {
		if f.Type == FieldRecordList {
			continue
		}
		v := schemaFallbackValue(f)
		if v == "" {
			continue
		}
		if f.Type == FieldSecret {
			if _, ok := outSecrets[f.Key]; ok {
				continue
			}
			enc, err := encryptOnce(f, v)
			if err != nil {
				return nil, nil, nil, err
			}
			outSecrets[f.Key] = enc
		} else {
			if _, ok := outValues[f.Key]; ok {
				continue
			}
			outValues[f.Key] = v
		}
		added = append(added, f.Key)
	}
	return outValues, outSecrets, added, nil
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

var (
	// ErrRequiredConfigMissing: a module marked by RequirePersistedConfig has
	// no document. Recovery is restore the document, or fix Mongo and perform
	// a controlled restart so boot seeding can run — never a lazy re-seed.
	ErrRequiredConfigMissing = errors.New("module: required config document is missing")
	// ErrRequiredSetSealed: RequirePersistedConfig was already called. The set
	// is decided once before traffic; pass every name in that one call.
	ErrRequiredSetSealed = errors.New("module: required persisted-config set is already sealed")
)

// RequirePersistedConfig marks modules whose config document must exist
// from now on (see requiredPersisted), and is the BOOT GATE for them: it
// refuses — so cmd/server can abort before serving traffic — when a named
// module's document is missing or its boot seeding/backfill recorded a
// failure. A required module is one whose strict readers may never be
// handed an incomplete document; "log and continue" is not an option for
// it. Call it ONCE, after boot seeding has run; a second call fails with
// ErrRequiredSetSealed so the set cannot drift while the process serves.
// A refused call seals nothing (the caller is about to exit).
func (s *ModuleConfigService) RequirePersistedConfig(ctx context.Context, names ...string) error {
	if s.requiredSealed {
		return ErrRequiredSetSealed
	}
	for _, n := range names {
		if err := s.seedFailures[n]; err != nil {
			return fmt.Errorf("module %q: boot seeding failed, refusing to serve: %w", n, err)
		}
		doc, err := s.repo.FindByName(ctx, n)
		if err != nil {
			return fmt.Errorf("module %q: verify config document: %w", n, err)
		}
		if doc == nil {
			return fmt.Errorf("%w: %q", ErrRequiredConfigMissing, n)
		}
	}
	s.requiredSealed = true
	for _, n := range names {
		s.requiredPersisted[n] = true
	}
	return nil
}

// IsRequiredPersisted reports whether name was marked by RequirePersistedConfig.
func (s *ModuleConfigService) IsRequiredPersisted(name string) bool { return s.requiredPersisted[name] }

// ModuleConfigStatus is one row of ListConfigs: a present document, or a
// required module whose document is missing (Config nil).
type ModuleConfigStatus struct {
	Name    string
	Missing bool
	Config  *ModuleConfig
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
	if s.requiredPersisted[name] {
		s.logger.Error("GetConfig: required module config document is missing — restore it or restart",
			slog.String("module", name))
		return nil, fmt.Errorf("%w: %q", ErrRequiredConfigMissing, name)
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

// ListConfigs returns one row per registered module: the document when it
// exists (lazily migrated to profiles), a lazily re-seeded document for an
// ordinary module that lost its document (dev DB wipe), and a `missing` row
// for a REQUIRED module that lost its document. The required-missing case
// never fails the whole list — the list is the page an operator repairs
// from — and never re-seeds; a failed profile migration, being a write
// failure, does fail it. Orphan documents (modules not compiled into this
// binary) are dropped, non-destructively, as before.
func (s *ModuleConfigService) ListConfigs(ctx context.Context) ([]ModuleConfigStatus, error) {
	docs, err := s.repo.FindAll(ctx)
	if err != nil {
		return nil, err
	}
	var present map[string]bool
	if len(s.knownModules) > 0 {
		docs, present = filterKnown(docs, s.knownModules)
	}
	for i := range docs {
		// A failed migration is a real write failure (the lost-race case is
		// absorbed inside ensureEnvironments by re-reading); a read does not
		// paper over it by serving the unmigrated document.
		if err := s.ensureEnvironments(ctx, &docs[i]); err != nil {
			return nil, fmt.Errorf("module %q: migrate legacy config: %w", docs[i].ModuleName, err)
		}
	}
	out := make([]ModuleConfigStatus, 0, len(docs)+len(s.knownModules))
	for i := range docs {
		out = append(out, ModuleConfigStatus{Name: docs[i].ModuleName, Config: &docs[i]})
	}
	names := make([]string, 0, len(s.knownModules))
	for name := range s.knownModules {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		if present[name] {
			continue
		}
		if s.requiredPersisted[name] {
			s.logger.Error("ListConfigs: required module config document is missing — restore it or restart",
				slog.String("module", name))
			out = append(out, ModuleConfigStatus{Name: name, Missing: true})
			continue
		}
		if seeded, err := s.lazySeed(ctx, name); err == nil && seeded != nil {
			out = append(out, ModuleConfigStatus{Name: name, Config: seeded})
		}
	}
	return out, nil
}

// GetAllConfigs is ListConfigs without the missing rows — the pre-existing
// shape, kept for callers that only want documents.
func (s *ModuleConfigService) GetAllConfigs(ctx context.Context) ([]ModuleConfig, error) {
	statuses, err := s.ListConfigs(ctx)
	if err != nil {
		return nil, err
	}
	docs := make([]ModuleConfig, 0, len(statuses))
	for _, st := range statuses {
		if st.Config != nil {
			docs = append(docs, *st.Config)
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
//
// The body lives in UpdateActiveConfig, which additionally reports the
// profile it targeted; this signature is the one every caller outside the
// admin handler uses and is unchanged.
func (s *ModuleConfigService) UpdateConfig(ctx context.Context, name string, values map[string]string, secrets map[string]string) error {
	_, err := s.UpdateActiveConfig(ctx, name, values, secrets)
	return err
}

// UpdateActiveConfig is UpdateConfig plus the one fact the caller cannot
// derive: WHICH profile the write targeted. The handler's own pre-read is a
// second read, and an activation landing between the two makes it name the
// wrong profile — so the audit event takes the env from here, where the
// document the write was judged against was read.
//
// env is returned as soon as it is determined, on the error paths too; "" only
// when the document could not be read at all.
func (s *ModuleConfigService) UpdateActiveConfig(ctx context.Context, name string, values map[string]string, secrets map[string]string) (string, error) {
	doc, err := s.repo.FindByName(ctx, name)
	if err != nil {
		return "", err
	}
	if doc == nil {
		return "", fmt.Errorf("module %q not found", name)
	}
	env := doc.ActiveEnv()
	// Lane refusal comes first: a request that is about to be refused with
	// 422 must persist nothing, and ensureEnvironments below can commit a
	// legacy-profile migration (and bump configRevision) on its own.
	schema := s.schemaFor(name, doc)
	if err := validateSubmittedKeys(schema, values, secrets); err != nil {
		return env, err
	}
	// ensureEnvironments migrates under its own compare-and-swap and leaves
	// doc current — profiles, activeEnvironment and configRevision — whether
	// this call won the migration or re-read after another writer did.
	if err := s.ensureEnvironments(ctx, doc); err != nil {
		return env, err
	}
	env = doc.ActiveEnv()
	cur, ok := doc.Environments[env]
	if !ok {
		return env, fmt.Errorf("environment %q not found for module %q", env, name)
	}
	// This surface changes no membership, so the STORED roster is the one the
	// write is judged against: an element key for a slug outside it is
	// refused rather than parked in the document.
	if err := validateElementKeysInRoster(schema, cur.ConfigValues, values, secrets); err != nil {
		return env, err
	}
	mergedValues := mergeStringMaps(cur.ConfigValues, values)
	// A plaintext secret a legacy document still carries under a secret key
	// is dropped here, so this write repairs it.
	mergedValues = nonSecretValues(schema, mergedValues)

	if err := s.validateCandidate(ctx, name, candidate{
		schema: schema, env: env, values: mergedValues,
		storedEncrypted: cur.EncryptedValues, submittedSecrets: secrets,
	}); err != nil {
		return env, err
	}

	encrypted, err := encryptAll(secrets)
	if err != nil {
		return env, err
	}
	mergedSecrets := mergeStringMaps(cur.EncryptedValues, encrypted)

	won, err := s.repo.CompareAndSwapConfig(ctx, name, ConfigMutation{
		ExpectedRevision: doc.ConfigRevision,
		Env:              env, EnvValues: mergedValues, EnvSecrets: mergedSecrets, EnvRevision: cur.Revision,
		WriteLegacy: true, LegacyValues: mergedValues, LegacySecrets: mergedSecrets,
		NeedsRestart: s.needsRestartFor(name),
	})
	if err != nil {
		return env, err
	}
	if !won {
		return env, ErrRevisionStale
	}
	// No cache invalidation: Redis caches only the enabled flag, which a
	// config write does not change. The CAS is the commit; nothing after it
	// may turn a committed write into a reported failure.
	return env, nil
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
	// This surface changes no membership either — same rule, same roster.
	if err := validateElementKeysInRoster(schema, cur.ConfigValues, values, secrets); err != nil {
		return err
	}
	mergedValues := mergeStringMaps(cur.ConfigValues, values)
	// A plaintext secret a legacy document still carries under a secret key
	// is dropped here, so this write repairs it.
	mergedValues = nonSecretValues(schema, mergedValues)

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

// GetRawValueRequiredModule is GetRawValue for a module whose document must
// exist: the three outcomes are preserved, but a missing document is the
// ERROR outcome (ErrRequiredConfigMissing), not "absent". It never calls
// GetConfig's lazy-seed path. A caller governing credentials — auth's
// per-surface password policy — reads through this so an outage can never
// be mistaken for "the operator said nothing here" and fall back to a
// permissive default. GetRawValue itself is unchanged: changing it would
// alter SessionAbsoluteTTL's compatibility contract.
func (s *ModuleConfigService) GetRawValueRequiredModule(ctx context.Context, moduleName, key string) (string, bool, error) {
	doc, err := s.repo.FindByName(ctx, moduleName)
	if err != nil {
		return "", false, err
	}
	if doc == nil {
		return "", false, fmt.Errorf("%w: %q", ErrRequiredConfigMissing, moduleName)
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
