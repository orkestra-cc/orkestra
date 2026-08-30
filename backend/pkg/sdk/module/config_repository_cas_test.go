package module

import (
	"context"
	"testing"
	"time"
)

func seedRevisionDoc(t *testing.T, repo *ModuleConfigRepository) {
	t.Helper()
	doc := &ModuleConfig{
		ModuleName:        "cas",
		Category:          CategoryCore,
		ActiveEnvironment: "production",
		ConfigValues:      map[string]string{"a": "old"},
		EncryptedValues:   map[string]string{},
		Environments: map[string]EnvironmentConfig{
			"production": {ConfigValues: map[string]string{"a": "old"}, EncryptedValues: map[string]string{}, UpdatedAt: time.Now()},
			"sandbox":    {ConfigValues: map[string]string{"a": "sb"}, EncryptedValues: map[string]string{"s": "ct"}, UpdatedAt: time.Now()},
		},
	}
	if err := repo.Upsert(context.Background(), doc); err != nil {
		t.Fatalf("seed: %v", err)
	}
}

// Values, secrets and metadata land in ONE UpdateOne; the legacy mirror and
// the profile are identical afterwards; a document seeded without the field
// compares as revision 0 and becomes 1.
func TestMongoCompareAndSwapConfig_SingleUpdatePersistsEverything(t *testing.T) {
	ctx := context.Background()
	_, repo := newTestConfigService(t)
	seedRevisionDoc(t, repo)

	won, err := repo.CompareAndSwapConfig(ctx, "cas", ConfigMutation{
		ExpectedRevision: 0, Env: "production",
		EnvValues: map[string]string{"a": "new"}, EnvSecrets: map[string]string{"k": "ciphertext"}, EnvRevision: 0,
		WriteLegacy: true, LegacyValues: map[string]string{"a": "new"}, LegacySecrets: map[string]string{"k": "ciphertext"},
		NeedsRestart: false,
	})
	if err != nil || !won {
		t.Fatalf("CAS: won=%v err=%v", won, err)
	}
	doc, err := repo.FindByName(ctx, "cas")
	if err != nil {
		t.Fatal(err)
	}
	if doc.ConfigRevision != 1 {
		t.Errorf("configRevision = %d, want 1", doc.ConfigRevision)
	}
	prod := doc.Environments["production"]
	if prod.ConfigValues["a"] != "new" || prod.EncryptedValues["k"] != "ciphertext" || prod.Revision != 1 {
		t.Errorf("profile not written: %+v", prod)
	}
	if doc.ConfigValues["a"] != "new" || doc.EncryptedValues["k"] != "ciphertext" {
		t.Errorf("legacy mirror not synced: %v %v", doc.ConfigValues, doc.EncryptedValues)
	}
	if doc.NeedsRestart {
		t.Error("needsRestart = true, want the given false")
	}
	if doc.Environments["sandbox"].ConfigValues["a"] != "sb" {
		t.Error("sibling profile disturbed")
	}
}

func TestMongoCompareAndSwapConfig_StaleWriterChangesNothing(t *testing.T) {
	ctx := context.Background()
	_, repo := newTestConfigService(t)
	seedRevisionDoc(t, repo)
	// First writer moves the document to revision 1.
	if won, err := repo.CompareAndSwapConfig(ctx, "cas", ConfigMutation{
		ExpectedRevision: 0, WriteLegacy: true, LegacyValues: map[string]string{"a": "first"}, LegacySecrets: map[string]string{},
	}); err != nil || !won {
		t.Fatalf("first writer: won=%v err=%v", won, err)
	}
	// Second writer still expects 0.
	won, err := repo.CompareAndSwapConfig(ctx, "cas", ConfigMutation{
		ExpectedRevision: 0, WriteLegacy: true, LegacyValues: map[string]string{"a": "second"}, LegacySecrets: map[string]string{},
	})
	if err != nil || won {
		t.Fatalf("stale writer: won=%v err=%v, want (false, nil)", won, err)
	}
	doc, _ := repo.FindByName(ctx, "cas")
	if doc.ConfigValues["a"] != "first" || doc.ConfigRevision != 1 {
		t.Errorf("stale writer changed the document: %v rev=%d", doc.ConfigValues, doc.ConfigRevision)
	}
}

// A write error leaves the document untouched — there is no first half that
// can land without the second. A cancelled context is the injectable failure.
func TestMongoCompareAndSwapConfig_WriteErrorLeavesDocumentUnchanged(t *testing.T) {
	_, repo := newTestConfigService(t)
	seedRevisionDoc(t, repo)
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := repo.CompareAndSwapConfig(cancelled, "cas", ConfigMutation{
		ExpectedRevision: 0, Env: "production", EnvValues: map[string]string{"a": "never"}, EnvSecrets: map[string]string{},
		WriteLegacy: true, LegacyValues: map[string]string{"a": "never"}, LegacySecrets: map[string]string{},
	})
	if err == nil {
		t.Fatal("a cancelled context must surface as an error")
	}
	doc, _ := repo.FindByName(context.Background(), "cas")
	if doc.ConfigValues["a"] != "old" || doc.Environments["production"].ConfigValues["a"] != "old" || doc.ConfigRevision != 0 {
		t.Errorf("a failed write left partial state: %+v", doc)
	}
}

func TestMongoCompareAndSwapConfig_ActivationCopiesServerSide(t *testing.T) {
	ctx := context.Background()
	_, repo := newTestConfigService(t)
	seedRevisionDoc(t, repo)
	won, err := repo.CompareAndSwapConfig(ctx, "cas", ConfigMutation{ExpectedRevision: 0, Activate: "sandbox", NeedsRestart: true})
	if err != nil || !won {
		t.Fatalf("activation: won=%v err=%v", won, err)
	}
	doc, _ := repo.FindByName(ctx, "cas")
	if doc.ActiveEnv() != "sandbox" || doc.ConfigValues["a"] != "sb" || doc.EncryptedValues["s"] != "ct" {
		t.Errorf("activation did not copy the stored sandbox maps: %+v", doc)
	}
	if doc.ConfigRevision != 1 || !doc.NeedsRestart {
		t.Errorf("configRevision=%d needsRestart=%v, want 1/true", doc.ConfigRevision, doc.NeedsRestart)
	}
	if won, err := repo.CompareAndSwapConfig(ctx, "cas", ConfigMutation{ExpectedRevision: 1, Activate: "nope"}); err != nil || won {
		t.Errorf("unknown profile: won=%v err=%v, want (false, nil)", won, err)
	}
}

// The active-profile decision is taken server-side in the same update, so
// the legacy mirror always tracks the profile that is active AT WRITE TIME —
// a caller who read the document before a concurrent activation cannot
// leave the mirror carrying the wrong profile.
func TestMongoCompareAndSwapEnvironment_MirrorFollowsStoredActiveEnv(t *testing.T) {
	ctx := context.Background()
	_, repo := newTestConfigService(t)
	seedRevisionDoc(t, repo) // active: production
	// Another writer activates sandbox; the mirror is now sandbox's.
	if won, err := repo.CompareAndSwapConfig(ctx, "cas", ConfigMutation{ExpectedRevision: 0, Activate: "sandbox"}); err != nil || !won {
		t.Fatalf("activation: won=%v err=%v", won, err)
	}
	// A record-list write to production, decided by a caller who read the
	// document BEFORE that activation (env revision still 0).
	won, err := repo.CompareAndSwapEnvironment(ctx, "cas", "production", 0,
		EnvironmentConfig{ConfigValues: map[string]string{"a": "prod-new"}, EncryptedValues: map[string]string{}}, false)
	if err != nil || !won {
		t.Fatalf("production env CAS: won=%v err=%v", won, err)
	}
	doc, _ := repo.FindByName(ctx, "cas")
	if doc.Environments["production"].ConfigValues["a"] != "prod-new" {
		t.Error("production profile not written")
	}
	if doc.ConfigValues["a"] != "sb" {
		t.Errorf("mirror = %v, want the ACTIVE (sandbox) values untouched", doc.ConfigValues)
	}
	// A write to the now-active sandbox does update the mirror.
	won, err = repo.CompareAndSwapEnvironment(ctx, "cas", "sandbox", 0,
		EnvironmentConfig{ConfigValues: map[string]string{"a": "sb-new"}, EncryptedValues: map[string]string{}}, false)
	if err != nil || !won {
		t.Fatalf("sandbox env CAS: won=%v err=%v", won, err)
	}
	doc, _ = repo.FindByName(ctx, "cas")
	if doc.ConfigValues["a"] != "sb-new" || doc.ConfigRevision != 3 {
		t.Errorf("mirror=%v configRevision=%d, want sb-new / 3", doc.ConfigValues, doc.ConfigRevision)
	}
}

// Record-list and ordinary writes cannot pass each other unseen: the
// environment CAS bumps configRevision, so an ordinary writer that read the
// document earlier loses.
func TestMongoCompareAndSwapEnvironment_BumpsConfigRevision(t *testing.T) {
	ctx := context.Background()
	_, repo := newTestConfigService(t)
	seedRevisionDoc(t, repo)
	won, err := repo.CompareAndSwapEnvironment(ctx, "cas", "production", 0,
		EnvironmentConfig{ConfigValues: map[string]string{"a": "rl"}, EncryptedValues: map[string]string{}}, false)
	if err != nil || !won {
		t.Fatalf("env CAS: won=%v err=%v", won, err)
	}
	doc, _ := repo.FindByName(ctx, "cas")
	if doc.ConfigRevision != 1 || doc.NeedsRestart {
		t.Errorf("configRevision=%d needsRestart=%v after env CAS, want 1/false", doc.ConfigRevision, doc.NeedsRestart)
	}
	if won, err := repo.CompareAndSwapConfig(ctx, "cas", ConfigMutation{ExpectedRevision: 0, WriteLegacy: true, LegacyValues: map[string]string{}, LegacySecrets: map[string]string{}}); err != nil || won {
		t.Errorf("ordinary writer at revision 0 must lose after a roster write: won=%v err=%v", won, err)
	}
}
