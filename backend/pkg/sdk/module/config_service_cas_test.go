package module

import (
	"context"
	"errors"
	"log/slog"
	"testing"
)

// invariantModule models PR 3's anti-lockout rule in miniature:
// password may be "false" only while provider is "true" AND the provider
// secret is present in the TARGET profile. It exists to prove the race and
// the target-secret rules end to end; the real rule lives in auth.
type invariantModule struct{ BaseModule }

func (invariantModule) Name() string             { return "inv" }
func (invariantModule) Init(*Dependencies) error { return nil }
func (invariantModule) ConfigSchema() []ConfigField {
	return []ConfigField{
		{Key: "password", Type: FieldBool, Default: "true"},
		{Key: "provider", Type: FieldBool, Default: "false"},
		{Key: "providerSecret", Type: FieldSecret},
		{Key: "extraSecret", Type: FieldSecret},
	}
}
func (invariantModule) HotReloadConfig() bool { return true }
func (invariantModule) ValidateConfigSnapshot(_ context.Context, s ConfigValidationSnapshot) error {
	if s.Values["password"] == "false" && !(s.EffectiveValues["provider"] == "true" && s.SecretPresent["providerSecret"]) {
		return &ConfigValidationError{Field: "password", Message: "would lock the surface out", Code: "x.lockout"}
	}
	return nil
}

func invDoc(ct string) *ModuleConfig {
	return &ModuleConfig{
		ModuleName: "inv", ActiveEnvironment: "production",
		ConfigSchema:    invariantModule{}.ConfigSchema(),
		ConfigValues:    map[string]string{"password": "true", "provider": "true"},
		EncryptedValues: map[string]string{"providerSecret": ct},
		Environments: map[string]EnvironmentConfig{
			"production": {ConfigValues: map[string]string{"password": "true", "provider": "true"}, EncryptedValues: map[string]string{"providerSecret": ct}, Revision: 2},
			"sandbox":    {ConfigValues: map[string]string{"password": "true", "provider": "true"}, EncryptedValues: map[string]string{}, Revision: 0},
		},
		ConfigRevision: 3,
	}
}

func newInvService(t *testing.T) (*ModuleConfigService, *fakeConfigRepo) {
	t.Helper()
	withEncryptionKey(t)
	ct, err := encryptSecret("shh")
	if err != nil {
		t.Fatal(err)
	}
	repo := newFakeConfigRepo()
	repo.docs["inv"] = invDoc(ct)
	svc := NewModuleConfigService(repo, fakeRedisClient{}, slog.Default())
	svc.RegisterKnownModules([]Module{invariantModule{}})
	svc.SetHotReloadResolver(func(string) bool { return true })
	return svc, repo
}

func TestUpdateConfig_AtomicProfileAndMirror(t *testing.T) {
	svc, repo := newInvService(t)
	if err := svc.UpdateConfig(context.Background(), "inv", map[string]string{"password": "false"}, map[string]string{"extraSecret": "v"}); err != nil {
		t.Fatalf("UpdateConfig: %v", err)
	}
	doc := repo.docs["inv"]
	prod := doc.Environments["production"]
	if prod.ConfigValues["password"] != "false" || doc.ConfigValues["password"] != "false" {
		t.Errorf("profile/mirror diverged: %v / %v", prod.ConfigValues, doc.ConfigValues)
	}
	if prod.EncryptedValues["extraSecret"] == "" || prod.EncryptedValues["extraSecret"] != doc.EncryptedValues["extraSecret"] {
		t.Error("secret not written identically to profile and mirror")
	}
	if prod.EncryptedValues["providerSecret"] == "" {
		t.Error("a config write wiped a secret it did not mention")
	}
	if doc.ConfigRevision != 4 || prod.Revision != 3 {
		t.Errorf("revisions: doc=%d env=%d, want 4/3", doc.ConfigRevision, prod.Revision)
	}
	if doc.NeedsRestart {
		t.Error("hot-reloadable module must persist needsRestart=false in the same write")
	}
	if repo.docCasCalls != 1 {
		t.Errorf("docCasCalls = %d, want exactly one write", repo.docCasCalls)
	}
}

func TestUpdateConfig_NeedsRestartWithoutResolverOrForColdModule(t *testing.T) {
	svc, repo := newInvService(t)
	svc.SetHotReloadResolver(nil)
	_ = svc.UpdateConfig(context.Background(), "inv", map[string]string{"provider": "true"}, nil)
	if !repo.docs["inv"].NeedsRestart {
		t.Error("without a resolver every write must mark needsRestart (pre-existing behaviour)")
	}
	svc.SetHotReloadResolver(func(string) bool { return false })
	_ = svc.UpdateConfig(context.Background(), "inv", map[string]string{"provider": "true"}, nil)
	if !repo.docs["inv"].NeedsRestart {
		t.Error("a module that does not hot-reload must mark needsRestart")
	}
}

// Two operators read revision 3. A disables password (valid: provider on).
// B disables the provider (valid on its own read). B's CAS lands after A's
// write: it must lose with ErrRevisionStale, and B's retry against the
// reloaded document must fail the invariant — two individually valid
// snapshots never combine into an invalid one.
func TestUpdateConfig_ConcurrentWritersCannotSkew(t *testing.T) {
	svc, repo := newInvService(t)
	ctx := context.Background()
	fired := false
	repo.beforeDocCAS = func() {
		if fired {
			return
		}
		fired = true
		// Writer A lands inside B's window. Detach the hook so A's own CAS
		// does not recurse.
		hook := repo.beforeDocCAS
		repo.beforeDocCAS = nil
		if err := svc.UpdateConfig(ctx, "inv", map[string]string{"password": "false"}, nil); err != nil {
			t.Fatalf("writer A: %v", err)
		}
		repo.beforeDocCAS = hook
	}
	err := svc.UpdateConfig(ctx, "inv", map[string]string{"provider": "false"}, nil)
	if !errors.Is(err, ErrRevisionStale) {
		t.Fatalf("writer B: err = %v, want ErrRevisionStale", err)
	}
	if repo.docs["inv"].ConfigValues["provider"] != "true" {
		t.Fatal("the loser's write reached the document")
	}
	// B reloads (UpdateConfig re-reads) and retries the same intent.
	repo.beforeDocCAS = nil
	err = svc.UpdateConfig(ctx, "inv", map[string]string{"provider": "false"}, nil)
	var typed *ConfigValidationError
	if !errors.As(err, &typed) || typed.Code != "x.lockout" {
		t.Fatalf("retry must fail the invariant: %v", err)
	}
}

func TestUpdateEnvironmentConfig_InactiveProfileLeavesMirrorAlone(t *testing.T) {
	svc, repo := newInvService(t)
	err := svc.UpdateEnvironmentConfig(context.Background(), "inv", "sandbox", map[string]string{"provider": "false"}, nil)
	if err != nil {
		t.Fatalf("inactive-profile PATCH: %v", err)
	}
	doc := repo.docs["inv"]
	if doc.Environments["sandbox"].ConfigValues["provider"] != "false" {
		t.Error("sandbox not written")
	}
	if doc.ConfigValues["provider"] != "true" || doc.Environments["production"].ConfigValues["provider"] != "true" {
		t.Error("an inactive-profile write must not touch the mirror or the active profile")
	}
	if doc.ConfigRevision != 4 || doc.Environments["sandbox"].Revision != 1 {
		t.Errorf("revisions: doc=%d sandbox=%d", doc.ConfigRevision, doc.Environments["sandbox"].Revision)
	}
	// Active-profile PATCH through the same method syncs the mirror.
	if err := svc.UpdateEnvironmentConfig(context.Background(), "inv", "production", map[string]string{"provider": "true"}, map[string]string{"providerSecret": "new"}); err != nil {
		t.Fatal(err)
	}
	if repo.docs["inv"].EncryptedValues["providerSecret"] != repo.docs["inv"].Environments["production"].EncryptedValues["providerSecret"] {
		t.Error("active-profile write did not sync the legacy mirror")
	}
}

// The validator judges the TARGET's own secrets: sandbox has no provider
// secret, so activating it while its password is off must be refused even
// though the active production profile does hold one. Submitting the
// sandbox secret plus the flip in one PATCH is accepted atomically.
func TestSnapshot_TargetSecretsNotActiveSecrets(t *testing.T) {
	svc, repo := newInvService(t)
	ctx := context.Background()
	err := svc.UpdateEnvironmentConfig(ctx, "inv", "sandbox", map[string]string{"password": "false"}, nil)
	var typed *ConfigValidationError
	if !errors.As(err, &typed) || typed.Code != "x.lockout" {
		t.Fatalf("sandbox without its own secret must be refused: %v", err)
	}
	if err := svc.UpdateEnvironmentConfig(ctx, "inv", "sandbox", map[string]string{"password": "false"}, map[string]string{"providerSecret": "sb"}); err != nil {
		t.Fatalf("secret + flip in one PATCH must be accepted: %v", err)
	}
	if repo.docs["inv"].Environments["sandbox"].EncryptedValues["providerSecret"] == "" {
		t.Error("submitted secret not persisted with the flip")
	}
	// Activation judges the stored target: sandbox is now valid → allowed;
	// a profile made invalid out of band is refused and nothing moves.
	if err := svc.SetActiveEnvironment(ctx, "inv", "sandbox"); err != nil {
		t.Fatalf("activating a valid sandbox: %v", err)
	}
	if repo.docs["inv"].ActiveEnvironment != "sandbox" || repo.docs["inv"].NeedsRestart {
		t.Errorf("activation state: active=%q needsRestart=%v", repo.docs["inv"].ActiveEnvironment, repo.docs["inv"].NeedsRestart)
	}
	env := repo.docs["inv"].Environments["production"]
	delete(env.EncryptedValues, "providerSecret")
	env.ConfigValues["password"] = "false"
	repo.docs["inv"].Environments["production"] = env
	rev := repo.docs["inv"].ConfigRevision
	if err := svc.SetActiveEnvironment(ctx, "inv", "production"); !errors.As(err, &typed) {
		t.Fatalf("activating an invalid production profile must be refused: %v", err)
	}
	if repo.docs["inv"].ActiveEnvironment != "sandbox" || repo.docs["inv"].ConfigRevision != rev {
		t.Error("a refused activation moved state")
	}
}

// failingRedisClient models a Redis outage: every Del errors.
type failingRedisClient struct{ fakeRedisClient }

func (failingRedisClient) Del(context.Context, ...string) error { return errors.New("redis down") }

// A committed compare-and-swap is a success, whatever Redis does afterwards:
// the cache holds only the enabled flag, which these writes never change.
func TestConfigWrites_DoNotReportRedisFailures(t *testing.T) {
	withEncryptionKey(t)
	ct, _ := encryptSecret("shh")
	repo := newFakeConfigRepo()
	repo.docs["inv"] = invDoc(ct)
	svc := NewModuleConfigService(repo, failingRedisClient{}, slog.Default())
	svc.RegisterKnownModules([]Module{invariantModule{}})
	ctx := context.Background()
	if err := svc.UpdateConfig(ctx, "inv", map[string]string{"provider": "true"}, nil); err != nil {
		t.Errorf("UpdateConfig reported a Redis failure after committing: %v", err)
	}
	if err := svc.UpdateEnvironmentConfig(ctx, "inv", "sandbox", map[string]string{"provider": "true"}, nil); err != nil {
		t.Errorf("UpdateEnvironmentConfig reported a Redis failure after committing: %v", err)
	}
	if err := svc.SetActiveEnvironment(ctx, "inv", "production"); err != nil {
		t.Errorf("SetActiveEnvironment reported a Redis failure after committing: %v", err)
	}
	if repo.docs["inv"].ConfigRevision != 6 {
		t.Errorf("configRevision = %d, want 6 — the three writes must all have landed", repo.docs["inv"].ConfigRevision)
	}
	// The enabled flag IS cached; its invalidation is best-effort.
	if err := svc.UpdateEnabled(ctx, "inv", false); err != nil {
		t.Errorf("UpdateEnabled reported a Redis failure after persisting: %v", err)
	}
}

func TestSetActiveEnvironment_StaleRevision(t *testing.T) {
	svc, repo := newInvService(t)
	repo.docCasFailures = 1
	if err := svc.SetActiveEnvironment(context.Background(), "inv", "sandbox"); !errors.Is(err, ErrRevisionStale) {
		t.Fatalf("err = %v, want ErrRevisionStale", err)
	}
}

// A legacy document (no profiles) is migrated first, then written to both
// the new production profile and the mirror — one source of truth.
func TestUpdateConfig_LegacyDocumentMigratesFirst(t *testing.T) {
	withEncryptionKey(t)
	repo := newFakeConfigRepo()
	repo.docs["plain"] = &ModuleConfig{ModuleName: "plain", ConfigValues: map[string]string{"a": "old"}, EncryptedValues: map[string]string{}}
	svc := NewModuleConfigService(repo, fakeRedisClient{}, slog.Default())
	svc.RegisterKnownModules([]Module{plainModule{}})
	if err := svc.UpdateConfig(context.Background(), "plain", map[string]string{"a": "new"}, nil); err != nil {
		t.Fatal(err)
	}
	doc := repo.docs["plain"]
	if doc.ActiveEnv() != "production" || doc.Environments["production"].ConfigValues["a"] != "new" || doc.ConfigValues["a"] != "new" {
		t.Errorf("legacy document not migrated + written: %+v", doc)
	}
	if doc.ConfigRevision != 2 {
		t.Errorf("configRevision = %d, want 2 (migration + write)", doc.ConfigRevision)
	}
}

// Two writers both read a legacy document. A migrates and writes. B's
// migration must LOSE (the document now has profiles) rather than re-copy
// B's stale legacy snapshot over A's freshly written profile; B then re-reads
// and its write lands on top of A's — both keys survive, revision 3.
func TestUpdateConfig_ConcurrentLegacyMigrationsCannotClobber(t *testing.T) {
	withEncryptionKey(t)
	repo := newFakeConfigRepo()
	repo.docs["plain"] = &ModuleConfig{ModuleName: "plain", ConfigValues: map[string]string{"a": "old", "b": "old"}, EncryptedValues: map[string]string{}}
	svc := NewModuleConfigService(repo, fakeRedisClient{}, slog.Default())
	svc.RegisterKnownModules([]Module{plainModule{}})
	ctx := context.Background()
	repo.beforeMigrate = func() {
		// Writer A lands inside B's migration window (the fake detaches the
		// hook before calling it, so A's own migration does not recurse).
		if err := svc.UpdateConfig(ctx, "plain", map[string]string{"a": "A"}, nil); err != nil {
			t.Fatalf("writer A: %v", err)
		}
	}
	if err := svc.UpdateConfig(ctx, "plain", map[string]string{"b": "B"}, nil); err != nil {
		t.Fatalf("writer B: %v", err)
	}
	doc := repo.docs["plain"]
	prod := doc.Environments["production"].ConfigValues
	if prod["a"] != "A" || prod["b"] != "B" || doc.ConfigValues["a"] != "A" || doc.ConfigValues["b"] != "B" {
		t.Errorf("B's stale migration clobbered A: profile=%v mirror=%v", prod, doc.ConfigValues)
	}
	if doc.ConfigRevision != 3 {
		t.Errorf("configRevision = %d, want 3 (migration, A, B)", doc.ConfigRevision)
	}
}

func TestGetConfig_MigrationFailureIsNotSwallowed(t *testing.T) {
	repo := newFakeConfigRepo()
	repo.docs["plain"] = &ModuleConfig{ModuleName: "plain", ConfigValues: map[string]string{"a": "old"}}
	repo.migrateErr = errors.New("write refused")
	svc := NewModuleConfigService(repo, fakeRedisClient{}, slog.Default())
	svc.RegisterKnownModules([]Module{plainModule{}})
	if _, err := svc.GetConfig(context.Background(), "plain"); err == nil {
		t.Fatal("a failed legacy migration must surface, not be logged and served as if migrated")
	}
}

// A legacy document can carry a plaintext secret under a key the schema
// declares as a secret. The lane rule only refuses NEW misfiled writes; the
// next ordinary write must also repair what is already stored, in the
// profile and in the legacy mirror alike, without touching the ciphertext.
func TestUpdateConfig_LegacyPlaintextSecretIsDroppedByTheNextWrite(t *testing.T) {
	svc, repo := newInvService(t)
	doc := repo.docs["inv"]
	doc.ConfigValues["providerSecret"] = "legacy-plaintext"
	prod := doc.Environments["production"]
	prod.ConfigValues["providerSecret"] = "legacy-plaintext"
	doc.Environments["production"] = prod
	storedCiphertext := prod.EncryptedValues["providerSecret"]

	if err := svc.UpdateConfig(context.Background(), "inv", map[string]string{"provider": "true"}, nil); err != nil {
		t.Fatalf("UpdateConfig: %v", err)
	}
	doc = repo.docs["inv"]
	prod = doc.Environments["production"]
	for name, m := range map[string]map[string]string{"profile": prod.ConfigValues, "mirror": doc.ConfigValues} {
		if v, ok := m["providerSecret"]; ok {
			t.Errorf("%s still carries the plaintext secret: %q", name, v)
		}
		if m["provider"] != "true" || m["password"] != "true" {
			t.Errorf("%s lost a non-secret key: %v", name, m)
		}
	}
	if prod.EncryptedValues["providerSecret"] != storedCiphertext {
		t.Error("the stored ciphertext must be untouched by the repair")
	}
}

// The lazy legacy→profiles migration used to copy the raw legacy mirror into
// the new production profile, duplicating any plaintext a pre-lane-rule
// document still carried under a secret key. The migrated profile is
// stripped instead.
func TestEnsureEnvironments_StripsLegacyPlaintext(t *testing.T) {
	withEncryptionKey(t)
	repo := newFakeConfigRepo()
	repo.docs["inv"] = &ModuleConfig{
		ModuleName:      "inv",
		ConfigSchema:    invariantModule{}.ConfigSchema(),
		ConfigValues:    map[string]string{"providerSecret": "plain", "password": "true"},
		EncryptedValues: map[string]string{},
	}
	svc := NewModuleConfigService(repo, fakeRedisClient{}, slog.Default())
	svc.RegisterKnownModules([]Module{invariantModule{}})

	doc, err := svc.GetConfig(context.Background(), "inv")
	if err != nil {
		t.Fatalf("GetConfig: %v", err)
	}
	prod := doc.Environments["production"]
	if v, ok := prod.ConfigValues["providerSecret"]; ok {
		t.Errorf("the migrated profile carries the plaintext secret: %q", v)
	}
	if prod.ConfigValues["password"] != "true" {
		t.Errorf("the migration lost a non-secret value: %v", prod.ConfigValues)
	}
	if v, ok := repo.docs["inv"].Environments["production"].ConfigValues["providerSecret"]; ok {
		t.Errorf("the persisted profile carries the plaintext secret: %q", v)
	}
}

// Activation copies the target profile into the legacy mirror. That copy has
// to be STRIPPED — otherwise activating a legacy profile republishes its
// plaintext into the mirror — and, when stripping removed something, the
// profile itself is repaired in the same compare-and-swap.
func TestSetActiveEnvironment_StripsAndRepairsTheTarget(t *testing.T) {
	svc, repo := newInvService(t)
	extra, err := encryptSecret("extra")
	if err != nil {
		t.Fatal(err)
	}
	sb := repo.docs["inv"].Environments["sandbox"]
	sb.ConfigValues = map[string]string{"providerSecret": "plain", "password": "true", "provider": "true"}
	sb.EncryptedValues = map[string]string{"extraSecret": extra}
	repo.docs["inv"].Environments["sandbox"] = sb

	if err := svc.SetActiveEnvironment(context.Background(), "inv", "sandbox"); err != nil {
		t.Fatalf("SetActiveEnvironment: %v", err)
	}
	doc := repo.docs["inv"]
	if doc.ActiveEnvironment != "sandbox" {
		t.Fatalf("activeEnvironment = %q, want sandbox", doc.ActiveEnvironment)
	}
	got := doc.Environments["sandbox"]
	for name, m := range map[string]map[string]string{"mirror": doc.ConfigValues, "profile": got.ConfigValues} {
		if v, ok := m["providerSecret"]; ok {
			t.Errorf("%s carries the plaintext secret after activation: %q", name, v)
		}
		if m["password"] != "true" || m["provider"] != "true" {
			t.Errorf("%s lost a non-secret value: %v", name, m)
		}
	}
	if doc.EncryptedValues["extraSecret"] != extra || got.EncryptedValues["extraSecret"] != extra {
		t.Errorf("the target's ciphertext was not copied: mirror=%v profile=%v", doc.EncryptedValues, got.EncryptedValues)
	}
	if got.Revision != 1 {
		t.Errorf("sandbox revision = %d, want 1 (the repair is a profile write)", got.Revision)
	}
	if doc.ConfigRevision != 4 {
		t.Errorf("configRevision = %d, want 4", doc.ConfigRevision)
	}
}
