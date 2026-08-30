package module

import (
	"context"
	"testing"
)

func revisionDoc(rev int64) *ModuleConfig {
	return &ModuleConfig{
		ModuleName:        "demo",
		ActiveEnvironment: "production",
		ConfigValues:      map[string]string{"a": "old"},
		EncryptedValues:   map[string]string{},
		ConfigRevision:    rev,
		Environments: map[string]EnvironmentConfig{
			"production": {ConfigValues: map[string]string{"a": "old"}, EncryptedValues: map[string]string{}, Revision: 4},
			"sandbox":    {ConfigValues: map[string]string{"a": "sb"}, EncryptedValues: map[string]string{"s": "ct"}, Revision: 1},
		},
	}
}

func TestCompareAndSwapConfig_RejectsStaleAndAcceptsCurrent(t *testing.T) {
	repo := newFakeConfigRepo()
	repo.docs["demo"] = revisionDoc(7)
	mut := ConfigMutation{
		ExpectedRevision: 6, Env: "production",
		EnvValues: map[string]string{"a": "new"}, EnvSecrets: map[string]string{}, EnvRevision: 4,
		WriteLegacy: true, LegacyValues: map[string]string{"a": "new"}, LegacySecrets: map[string]string{},
		NeedsRestart: false,
	}
	won, err := repo.CompareAndSwapConfig(context.Background(), "demo", mut)
	if err != nil || won {
		t.Fatalf("stale expectation: won=%v err=%v, want (false, nil)", won, err)
	}
	if repo.docs["demo"].Environments["production"].ConfigValues["a"] != "old" {
		t.Fatal("a stale writer changed the document")
	}

	mut.ExpectedRevision = 7
	won, err = repo.CompareAndSwapConfig(context.Background(), "demo", mut)
	if err != nil || !won {
		t.Fatalf("current expectation lost: won=%v err=%v", won, err)
	}
	doc := repo.docs["demo"]
	if doc.ConfigRevision != 8 {
		t.Errorf("configRevision = %d, want 8", doc.ConfigRevision)
	}
	if doc.Environments["production"].Revision != 5 {
		t.Errorf("environment revision = %d, want 5", doc.Environments["production"].Revision)
	}
	if doc.Environments["production"].ConfigValues["a"] != "new" || doc.ConfigValues["a"] != "new" {
		t.Errorf("profile and legacy mirror must both carry the write: env=%v legacy=%v",
			doc.Environments["production"].ConfigValues, doc.ConfigValues)
	}
	if doc.NeedsRestart {
		t.Error("needsRestart was not persisted as given (false)")
	}
	if doc.Environments["sandbox"].ConfigValues["a"] != "sb" {
		t.Error("a production write touched the sandbox profile")
	}
}

// A document written before configRevision existed has no field. Absent and
// 0 are the same value, or the first mutation on every pre-existing module
// would fail against nothing.
func TestCompareAndSwapConfig_AbsentRevisionIsZero(t *testing.T) {
	repo := newFakeConfigRepo()
	repo.docs["demo"] = revisionDoc(0)
	won, err := repo.CompareAndSwapConfig(context.Background(), "demo", ConfigMutation{
		ExpectedRevision: 0, WriteLegacy: true,
		LegacyValues: map[string]string{"a": "x"}, LegacySecrets: map[string]string{},
	})
	if err != nil || !won {
		t.Fatalf("legacy document rejected an expected revision of 0: won=%v err=%v", won, err)
	}
	if repo.docs["demo"].ConfigRevision != 1 {
		t.Errorf("configRevision = %d, want 1", repo.docs["demo"].ConfigRevision)
	}
}

// Activation copies the target profile's STORED maps at execution time —
// never a snapshot the caller took earlier — and bumps configRevision.
func TestCompareAndSwapConfig_ActivationCopiesStoredMaps(t *testing.T) {
	repo := newFakeConfigRepo()
	repo.docs["demo"] = revisionDoc(2)
	repo.duringActivate = func() {
		env := repo.docs["demo"].Environments["sandbox"]
		delete(env.EncryptedValues, "s")
		repo.docs["demo"].Environments["sandbox"] = env
	}
	won, err := repo.CompareAndSwapConfig(context.Background(), "demo", ConfigMutation{
		ExpectedRevision: 2, Activate: "sandbox", NeedsRestart: true,
	})
	if err != nil || !won {
		t.Fatalf("activation lost: won=%v err=%v", won, err)
	}
	doc := repo.docs["demo"]
	if doc.ActiveEnvironment != "sandbox" || doc.ConfigValues["a"] != "sb" {
		t.Errorf("activation did not switch + copy: active=%q legacy=%v", doc.ActiveEnvironment, doc.ConfigValues)
	}
	if _, ok := doc.EncryptedValues["s"]; ok {
		t.Error("activation copied a stale snapshot and resurrected a removed secret")
	}
	if doc.ConfigRevision != 3 || !doc.NeedsRestart {
		t.Errorf("configRevision=%d needsRestart=%v, want 3/true", doc.ConfigRevision, doc.NeedsRestart)
	}
}

func TestCompareAndSwapConfig_RejectsUnknownProfileAndMalformedShape(t *testing.T) {
	repo := newFakeConfigRepo()
	repo.docs["demo"] = revisionDoc(0)
	if won, err := repo.CompareAndSwapConfig(context.Background(), "demo", ConfigMutation{Activate: "nope"}); err != nil || won {
		t.Errorf("activating an unknown profile: won=%v err=%v, want (false, nil)", won, err)
	}
	if won, err := repo.CompareAndSwapConfig(context.Background(), "demo", ConfigMutation{Env: "nope", EnvValues: map[string]string{}, EnvSecrets: map[string]string{}}); err != nil || won {
		t.Errorf("writing an unknown profile: won=%v err=%v, want (false, nil)", won, err)
	}
	if _, err := repo.CompareAndSwapConfig(context.Background(), "demo", ConfigMutation{Activate: "sandbox", Env: "production"}); err == nil {
		t.Error("activation combined with a values write must be rejected as a programming error")
	}
	if _, err := repo.CompareAndSwapConfig(context.Background(), "demo", ConfigMutation{}); err == nil {
		t.Error("an empty mutation must be rejected")
	}
	if _, err := repo.CompareAndSwapConfig(context.Background(), "demo", ConfigMutation{WriteLegacy: true, LegacyValues: map[string]string{}}); err == nil {
		t.Error("a nil LegacySecrets must be rejected as a programming error, not normalized into a wipe")
	}
	if repo.docs["demo"].ConfigRevision != 0 {
		t.Error("a rejected mutation moved the revision")
	}
}

// The record-list CAS increments configRevision in the same update, so an
// ordinary config write that read the document before a roster change loses
// its own CAS instead of silently passing it.
func TestCompareAndSwapEnvironment_BumpsConfigRevisionAndPersistsNeedsRestart(t *testing.T) {
	repo := newFakeConfigRepo()
	repo.docs["demo"] = revisionDoc(5)
	won, err := repo.CompareAndSwapEnvironment(context.Background(), "demo", "production", 4,
		EnvironmentConfig{ConfigValues: map[string]string{"a": "rl"}, EncryptedValues: map[string]string{}}, false)
	if err != nil || !won {
		t.Fatalf("record-list CAS lost: won=%v err=%v", won, err)
	}
	if repo.docs["demo"].ConfigRevision != 6 {
		t.Errorf("configRevision = %d, want 6", repo.docs["demo"].ConfigRevision)
	}
	if repo.docs["demo"].NeedsRestart {
		t.Error("needsRestart was not persisted as given (false)")
	}
	won, err = repo.CompareAndSwapConfig(context.Background(), "demo", ConfigMutation{
		ExpectedRevision: 5, WriteLegacy: true, LegacyValues: map[string]string{}, LegacySecrets: map[string]string{},
	})
	if err != nil || won {
		t.Errorf("a config write that read revision 5 must lose after the roster write: won=%v err=%v", won, err)
	}
}
