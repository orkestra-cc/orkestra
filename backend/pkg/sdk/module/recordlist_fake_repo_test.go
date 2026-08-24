package module

import (
	"context"
	"log/slog"
	"testing"
)

func TestServiceAcceptsAFakeRepository(t *testing.T) {
	repo := newFakeConfigRepo()
	repo.docs["demo"] = &ModuleConfig{ModuleName: "demo", ConfigValues: map[string]string{"a": "1"}}

	svc := NewModuleConfigService(repo, fakeRedisClient{}, slog.Default())

	got, err := svc.GetConfig(context.Background(), "demo")
	if err != nil {
		t.Fatalf("GetConfig: %v", err)
	}
	if got == nil || got.ConfigValues["a"] != "1" {
		t.Fatalf("service did not read through the fake repository: %+v", got)
	}
}

// fakeConfigRepo is an in-memory ConfigRepository. It exists so the service's
// concurrency behaviour can be asserted in a test that actually RUNS: the
// MONGO_TEST_URI-guarded repository tests skip in every CI run, so a
// guarantee asserted only there is not covered at all.
type fakeConfigRepo struct {
	docs        map[string]*ModuleConfig
	casFailures int // fail this many CAS attempts before allowing one through
	casCalls    int
}

func newFakeConfigRepo() *fakeConfigRepo {
	return &fakeConfigRepo{docs: map[string]*ModuleConfig{}}
}

func (f *fakeConfigRepo) FindByName(_ context.Context, name string) (*ModuleConfig, error) {
	doc, ok := f.docs[name]
	if !ok {
		return nil, nil
	}
	cp := *doc
	return &cp, nil
}

func (f *fakeConfigRepo) FindAll(context.Context) ([]ModuleConfig, error) { return nil, nil }

func (f *fakeConfigRepo) Upsert(_ context.Context, c *ModuleConfig) error {
	f.docs[c.ModuleName] = c
	return nil
}

func (f *fakeConfigRepo) UpdateEnabled(context.Context, string, bool) error { return nil }

func (f *fakeConfigRepo) UpdateConfigValues(_ context.Context, name string, v, e map[string]string) error {
	doc := f.docs[name]
	doc.ConfigValues, doc.EncryptedValues = v, e
	return nil
}

func (f *fakeConfigRepo) UpdateEnvironmentConfig(_ context.Context, name, env string, v, e map[string]string) error {
	doc := f.docs[name]
	cfg := doc.Environments[env]
	cfg.ConfigValues, cfg.EncryptedValues = v, e
	doc.Environments[env] = cfg
	return nil
}

func (f *fakeConfigRepo) SetActiveEnvironment(_ context.Context, name, env string) error {
	f.docs[name].ActiveEnvironment = env
	return nil
}

func (f *fakeConfigRepo) MigrateToEnvironments(context.Context, string, map[string]string, map[string]string) error {
	return nil
}

func (f *fakeConfigRepo) ClearNeedsRestart(context.Context, string) error { return nil }

func (f *fakeConfigRepo) RefreshMetadata(context.Context, Module) error { return nil }

// CompareAndSwapEnvironment mirrors the Mongo implementation's contract: a
// mismatched revision is a lost race (false, nil), not an error. casFailures
// forces the first N attempts to lose, which is how a concurrent writer is
// modelled without a second goroutine.
func (f *fakeConfigRepo) CompareAndSwapEnvironment(_ context.Context, name, env string, expected int64, next EnvironmentConfig) (bool, error) {
	f.casCalls++
	if f.casFailures > 0 {
		f.casFailures--
		return false, nil
	}
	doc, ok := f.docs[name]
	if !ok {
		return false, nil
	}
	cur := doc.Environments[env]
	if cur.Revision != expected {
		return false, nil
	}
	next.Revision = cur.Revision + 1
	doc.Environments[env] = next
	if doc.ActiveEnv() == env {
		doc.ConfigValues, doc.EncryptedValues = next.ConfigValues, next.EncryptedValues
	}
	return true, nil
}
