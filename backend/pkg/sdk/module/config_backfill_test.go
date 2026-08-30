package module

import (
	"context"
	"errors"
	"log/slog"
	"reflect"
	"testing"
)

type backfillModule struct{ BaseModule }

func (backfillModule) Name() string             { return "bf" }
func (backfillModule) Init(*Dependencies) error { return nil }
func (backfillModule) ConfigSchema() []ConfigField {
	return []ConfigField{
		{Key: "existing", Type: FieldString, Default: "d"},
		{Key: "cleared", Type: FieldString, Default: "d"},
		{Key: "toggle", Type: FieldBool, Default: "false"},
		{Key: "fromEnv", Type: FieldString, EnvVar: "BF_TEST_FROM_ENV", Default: "envdefault"},
		{Key: "noDefault", Type: FieldString},
		{Key: "secret", Type: FieldSecret, Default: "s3cr3t"},
		{Key: "list", Type: FieldRecordList, Items: []ConfigItemField{{Key: "host", Type: FieldString, Default: "h"}}},
	}
}

func backfillSvc(t *testing.T, doc *ModuleConfig) (*ModuleConfigService, *fakeConfigRepo) {
	t.Helper()
	withEncryptionKey(t)
	repo := newFakeConfigRepo()
	repo.docs["bf"] = doc
	return NewModuleConfigService(repo, fakeRedisClient{}, slog.Default()), repo
}

func TestSeedFromModules_BackfillsAbsentSchemaKeys(t *testing.T) {
	t.Setenv("BF_TEST_FROM_ENV", "from-env")
	svc, repo := backfillSvc(t, &ModuleConfig{
		ModuleName: "bf", ActiveEnvironment: "sandbox", ConfigRevision: 5, NeedsRestart: true,
		ConfigValues:    map[string]string{"existing": "legacy", "cleared": ""},
		EncryptedValues: map[string]string{},
		Environments: map[string]EnvironmentConfig{
			"production": {ConfigValues: map[string]string{}, EncryptedValues: map[string]string{}},
			"sandbox":    {ConfigValues: map[string]string{"existing": "sb", "cleared": ""}, EncryptedValues: map[string]string{}, Revision: 2},
		},
	})
	if err := svc.SeedFromModules(context.Background(), []Module{backfillModule{}}); err != nil {
		t.Fatal(err)
	}
	doc := repo.docs["bf"]
	sb := doc.Environments["sandbox"]
	// Absent keys gained their EnvVar/Default in the ACTIVE profile; the
	// mirror is an exact copy of the result.
	for _, m := range []map[string]string{sb.ConfigValues, doc.ConfigValues} {
		if m["toggle"] != "false" || m["fromEnv"] != "from-env" {
			t.Errorf("backfill missing: %v", m)
		}
		if _, ok := m["noDefault"]; ok {
			t.Error("a field with no EnvVar/Default must not be invented")
		}
		if v, ok := m["cleared"]; !ok || v != "" {
			t.Error("an explicitly empty stored value must be left alone")
		}
	}
	if sb.ConfigValues["existing"] != "sb" {
		t.Error("a present profile value was overwritten")
	}
	// The mirror is rebuilt from the ACTIVE profile, not backfilled on its
	// own: its stale "legacy" value is replaced by the profile's "sb".
	if doc.ConfigValues["existing"] != "sb" {
		t.Errorf("mirror existing = %q, want the profile's %q", doc.ConfigValues["existing"], "sb")
	}
	// Secrets go through the encrypted path, encrypted ONCE: profile and
	// mirror carry the identical ciphertext.
	plain, err := decryptSecret(sb.EncryptedValues["secret"])
	if err != nil || plain != "s3cr3t" || doc.EncryptedValues["secret"] == "" {
		t.Errorf("secret backfill: %q %v", plain, err)
	}
	if sb.EncryptedValues["secret"] != doc.EncryptedValues["secret"] {
		t.Error("secret was encrypted twice — profile and mirror must be identical")
	}
	// Record lists are schema-level constructs with nothing to seed.
	if _, ok := sb.ConfigValues["list"]; ok {
		t.Error("record list key must not be backfilled")
	}
	// The inactive profile is untouched; the revision advanced exactly once.
	if len(doc.Environments["production"].ConfigValues) != 0 {
		t.Error("inactive profile was backfilled")
	}
	if doc.ConfigRevision != 6 || sb.Revision != 3 || repo.docCasCalls != 1 {
		t.Errorf("revision=%d sbRevision=%d casCalls=%d, want 6/3/1", doc.ConfigRevision, sb.Revision, repo.docCasCalls)
	}
	// The backfill write carried needsRestart=false itself, so the second
	// write boot would otherwise owe is not made.
	if doc.NeedsRestart {
		t.Error("the backfill write must persist needsRestart=false")
	}
	if repo.clearRestartCalls != 0 {
		t.Errorf("clearRestartCalls = %d, want 0 — the backfill write already cleared it", repo.clearRestartCalls)
	}
}

func TestSeedFromModules_CompleteDocumentIsNotRewritten(t *testing.T) {
	t.Setenv("BF_TEST_FROM_ENV", "")
	full := map[string]string{"existing": "x", "cleared": "", "toggle": "true", "fromEnv": "e", "noDefault": ""}
	withEncryptionKey(t)
	ct, _ := encryptSecret("s")
	svc, repo := backfillSvc(t, &ModuleConfig{
		ModuleName: "bf", ActiveEnvironment: "production", ConfigRevision: 9,
		ConfigValues: mergeStringMaps(full, nil), EncryptedValues: map[string]string{"secret": ct},
		Environments: map[string]EnvironmentConfig{
			"production": {ConfigValues: mergeStringMaps(full, nil), EncryptedValues: map[string]string{"secret": ct}},
		},
	})
	if err := svc.SeedFromModules(context.Background(), []Module{backfillModule{}}); err != nil {
		t.Fatal(err)
	}
	if repo.docs["bf"].ConfigRevision != 9 || repo.docCasCalls != 0 {
		t.Errorf("a complete document was rewritten: revision=%d casCalls=%d", repo.docs["bf"].ConfigRevision, repo.docCasCalls)
	}
	// Nothing was written, so the boot-time clear is still owed and made.
	if repo.clearRestartCalls != 1 {
		t.Errorf("clearRestartCalls = %d, want 1", repo.clearRestartCalls)
	}
}

// A legacy document with no profiles gets its mirror backfilled; the later
// lazy migration copies the complete mirror into the production profile.
func TestSeedFromModules_LegacyDocumentBackfillsMirrorOnly(t *testing.T) {
	t.Setenv("BF_TEST_FROM_ENV", "")
	svc, repo := backfillSvc(t, &ModuleConfig{ModuleName: "bf", ConfigValues: map[string]string{}, EncryptedValues: map[string]string{}})
	if err := svc.SeedFromModules(context.Background(), []Module{backfillModule{}}); err != nil {
		t.Fatal(err)
	}
	doc := repo.docs["bf"]
	want := map[string]string{"existing": "d", "cleared": "d", "toggle": "false", "fromEnv": "envdefault"}
	if !reflect.DeepEqual(doc.ConfigValues, want) {
		t.Errorf("mirror = %v, want %v", doc.ConfigValues, want)
	}
	if len(doc.Environments) != 0 {
		t.Error("backfill must not invent profiles — that is the lazy migration's job")
	}
}

// A lost CAS means a concurrently booting replica wrote first — possibly an
// older binary that knew fewer keys. The document is re-read and the missing
// set recomputed, never assumed complete.
func TestSeedFromModules_LostCASIsRetriedAgainstTheFreshDocument(t *testing.T) {
	t.Setenv("BF_TEST_FROM_ENV", "")
	svc, repo := backfillSvc(t, &ModuleConfig{
		ModuleName: "bf", ActiveEnvironment: "production", ConfigRevision: 1,
		ConfigValues: map[string]string{}, EncryptedValues: map[string]string{},
		Environments: map[string]EnvironmentConfig{"production": {ConfigValues: map[string]string{}, EncryptedValues: map[string]string{}}},
	})
	// The other replica's write lands inside our window: it knew only
	// "toggle" (mirror only) and moved the revision. The hook is idempotent,
	// so firing again on the retry changes nothing.
	repo.beforeDocCAS = func() {
		d := repo.docs["bf"]
		d.ConfigValues["toggle"] = "false"
		d.ConfigRevision = 2
	}
	if err := svc.SeedFromModules(context.Background(), []Module{backfillModule{}}); err != nil {
		t.Fatal(err)
	}
	doc := repo.docs["bf"]
	if repo.docCasCalls != 2 {
		t.Errorf("docCasCalls = %d, want 2 (one lost, one won)", repo.docCasCalls)
	}
	if doc.ConfigRevision != 3 {
		t.Errorf("configRevision = %d, want 3", doc.ConfigRevision)
	}
	if doc.ConfigValues["existing"] != "d" || doc.ConfigValues["toggle"] != "false" {
		t.Errorf("mirror after retry: %v", doc.ConfigValues)
	}
	if doc.Environments["production"].ConfigValues["toggle"] != "false" || doc.Environments["production"].ConfigValues["existing"] != "d" {
		t.Errorf("profile after retry: %v", doc.Environments["production"].ConfigValues)
	}
}

// Keys whose EnvVar/Default is empty stay ABSENT: absence is meaningful to
// GetRawValue readers (ADR-0017 — an absent sessionAbsoluteTTL is the default
// cap, a present "" disables it), so inventing "" would change policy.
func TestSeedFromModules_EmptyFallbackKeysStayAbsent(t *testing.T) {
	t.Setenv("BF_TEST_FROM_ENV", "")
	svc, repo := backfillSvc(t, &ModuleConfig{
		ModuleName: "bf", ActiveEnvironment: "production",
		ConfigValues: map[string]string{}, EncryptedValues: map[string]string{},
		Environments: map[string]EnvironmentConfig{"production": {ConfigValues: map[string]string{}, EncryptedValues: map[string]string{}}},
	})
	if err := svc.SeedFromModules(context.Background(), []Module{backfillModule{}}); err != nil {
		t.Fatal(err)
	}
	for _, m := range []map[string]string{repo.docs["bf"].ConfigValues, repo.docs["bf"].Environments["production"].ConfigValues} {
		if _, ok := m["noDefault"]; ok {
			t.Error("a key with an empty fallback was invented as \"\"")
		}
	}
}

// A secret that cannot be encrypted (no OAUTH_TOKEN_ENCRYPTION_KEY) is a
// backfill FAILURE, not a skipped key: nothing is written, the failure is
// recorded, and the required-module gate refuses. First-boot seeding keeps
// its warn-and-skip; a backfill that "succeeded" minus a secret would let
// a strict reader serve an incomplete document.
func TestSeedFromModules_UnencryptableSecretFailsTheBackfill(t *testing.T) {
	t.Setenv("BF_TEST_FROM_ENV", "")
	svc, repo := backfillSvc(t, &ModuleConfig{
		ModuleName: "bf", ActiveEnvironment: "production",
		ConfigValues: map[string]string{}, EncryptedValues: map[string]string{},
		Environments: map[string]EnvironmentConfig{"production": {ConfigValues: map[string]string{}, EncryptedValues: map[string]string{}}},
	})
	t.Setenv("OAUTH_TOKEN_ENCRYPTION_KEY", "")
	if err := svc.SeedFromModules(context.Background(), []Module{backfillModule{}}); err != nil {
		t.Fatal(err)
	}
	if repo.docCasCalls != 0 {
		t.Error("a backfill that could not encrypt a secret must write nothing")
	}
	if _, ok := repo.docs["bf"].Environments["production"].ConfigValues["existing"]; ok {
		t.Error("non-secret keys must not be written by a failed backfill (all-or-nothing)")
	}
	if err := svc.seedFailures["bf"]; err == nil {
		t.Fatal("the encryption failure must be recorded")
	}
	if err := svc.RequirePersistedConfig(context.Background(), "bf"); err == nil {
		t.Fatal("the required-module gate must refuse")
	}
}

func TestBackfillSchemaKeys_ReturnsSortedNamesWrittenOnce(t *testing.T) {
	t.Setenv("BF_TEST_FROM_ENV", "")
	svc, repo := backfillSvc(t, &ModuleConfig{
		ModuleName: "bf", ActiveEnvironment: "production",
		ConfigValues: map[string]string{}, EncryptedValues: map[string]string{},
		Environments: map[string]EnvironmentConfig{"production": {ConfigValues: map[string]string{"toggle": "true"}, EncryptedValues: map[string]string{}}},
	})
	keys, wrote, err := svc.backfillSchemaKeys(context.Background(), backfillModule{}, repo.docs["bf"])
	if err != nil || !wrote {
		t.Fatalf("wrote=%v err=%v", wrote, err)
	}
	// The keys added to the ACTIVE profile (toggle was already there); the
	// mirror is then a copy, so its own emptiness adds nothing to the list.
	want := []string{"cleared", "existing", "fromEnv", "secret"}
	if !reflect.DeepEqual(keys, want) {
		t.Errorf("keys = %v, want %v", keys, want)
	}
	if repo.docs["bf"].ConfigValues["toggle"] != "true" {
		t.Error("mirror must carry the profile's value, not a default")
	}
}

// A mirror that diverged from a complete profile is realigned to the
// profile — the profile is what the runtime and the admin UI read.
func TestSeedFromModules_MirrorIsRebuiltFromTheActiveProfile(t *testing.T) {
	t.Setenv("BF_TEST_FROM_ENV", "")
	withEncryptionKey(t)
	ct, _ := encryptSecret("s")
	full := map[string]string{"existing": "custom", "cleared": "", "toggle": "true", "fromEnv": "e", "noDefault": ""}
	svc, repo := backfillSvc(t, &ModuleConfig{
		ModuleName: "bf", ActiveEnvironment: "production", ConfigRevision: 4,
		// Mirror: stale value, an extra key the profile does not have, and no secret.
		ConfigValues:    map[string]string{"existing": "stale", "orphan": "x", "toggle": "true", "fromEnv": "e", "cleared": "", "noDefault": ""},
		EncryptedValues: map[string]string{},
		Environments: map[string]EnvironmentConfig{
			"production": {ConfigValues: mergeStringMaps(full, nil), EncryptedValues: map[string]string{"secret": ct}},
		},
	})
	if err := svc.SeedFromModules(context.Background(), []Module{backfillModule{}}); err != nil {
		t.Fatal(err)
	}
	doc := repo.docs["bf"]
	if !reflect.DeepEqual(doc.ConfigValues, full) || doc.EncryptedValues["secret"] != ct {
		t.Errorf("mirror = %v / %v, want an exact copy of the profile", doc.ConfigValues, doc.EncryptedValues)
	}
	if !reflect.DeepEqual(doc.Environments["production"].ConfigValues, full) {
		t.Error("a complete profile must not be touched")
	}
	if doc.ConfigRevision != 5 || repo.docCasCalls != 1 {
		t.Errorf("revision=%d casCalls=%d, want 5/1", doc.ConfigRevision, repo.docCasCalls)
	}
}

// A backfill failure is recorded so the required-module gate can refuse
// to serve; the boot itself continues (non-required modules degrade).
func TestSeedFromModules_BackfillFailureIsRecorded(t *testing.T) {
	t.Setenv("BF_TEST_FROM_ENV", "")
	svc, repo := backfillSvc(t, &ModuleConfig{
		ModuleName: "bf", ActiveEnvironment: "production",
		ConfigValues: map[string]string{}, EncryptedValues: map[string]string{},
		Environments: map[string]EnvironmentConfig{"production": {ConfigValues: map[string]string{}, EncryptedValues: map[string]string{}}},
	})
	repo.docCasFailures = backfillMaxAttempts // every attempt loses
	if err := svc.SeedFromModules(context.Background(), []Module{backfillModule{}}); err != nil {
		t.Fatal(err)
	}
	if err := svc.seedFailures["bf"]; err == nil || !errors.Is(err, ErrRevisionStale) {
		t.Fatalf("seedFailures[bf] = %v, want the recorded ErrRevisionStale", err)
	}
	if err := svc.RequirePersistedConfig(context.Background(), "bf"); err == nil {
		t.Fatal("the required-module gate must refuse a module whose backfill failed")
	}
}
