package module

import (
	"context"
	"fmt"
	"log/slog"
	"sort"
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
	casFailures int // fail this many environment-CAS attempts before allowing one through
	casCalls    int
	// docCasFailures / docCasCalls are the document-level twins for
	// CompareAndSwapConfig, kept separate so a record-list test's counters
	// are never disturbed by a config-write test and vice versa.
	docCasFailures int
	docCasCalls    int
	// beforeDocCAS runs inside CompareAndSwapConfig before the revision is
	// compared — the window in which a concurrent writer lands. It is how a
	// two-writer race is modelled without a second goroutine.
	beforeDocCAS func()
	// duringActivate runs inside an activation, modelling a concurrent
	// write landing in the window a two-step activation leaves open.
	duringActivate func()
	// beforeMigrate runs inside MigrateToEnvironments before the no-profiles
	// check — the window in which a concurrent writer migrates first.
	beforeMigrate func()
	// migrateErr, when set, makes MigrateToEnvironments fail with it.
	migrateErr error
	// refreshErr, when set, makes RefreshMetadata fail with it — the
	// boot-seeding failure that leaves a document judged against a stale
	// stored schema.
	refreshErr error
	// findErr, when set, makes FindByName fail with it — models a
	// repository outage.
	findErr error
}

func newFakeConfigRepo() *fakeConfigRepo {
	return &fakeConfigRepo{docs: map[string]*ModuleConfig{}}
}

// FindByName returns a DEEP copy, the way a real Mongo read does: the caller
// holds a snapshot, and a later write to the stored document is invisible to
// it. A shallow copy shares every map, which would let a test that mutates
// stored state be silently observed through the caller's "snapshot" — exactly
// the staleness these tests exist to detect.
func (f *fakeConfigRepo) FindByName(_ context.Context, name string) (*ModuleConfig, error) {
	if f.findErr != nil {
		return nil, f.findErr
	}
	doc, ok := f.docs[name]
	if !ok {
		return nil, nil
	}
	cp := *doc
	cp.ConfigValues = copyStrings(doc.ConfigValues)
	cp.EncryptedValues = copyStrings(doc.EncryptedValues)
	if doc.Environments != nil {
		cp.Environments = make(map[string]EnvironmentConfig, len(doc.Environments))
		for k, env := range doc.Environments {
			env.ConfigValues = copyStrings(env.ConfigValues)
			env.EncryptedValues = copyStrings(env.EncryptedValues)
			cp.Environments[k] = env
		}
	}
	return &cp, nil
}

func copyStrings(m map[string]string) map[string]string {
	if m == nil {
		return nil
	}
	out := make(map[string]string, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}

// FindAll returns deep copies in name order, like the Mongo repository
// would (order is not part of the contract; determinism is convenient).
func (f *fakeConfigRepo) FindAll(ctx context.Context) ([]ModuleConfig, error) {
	names := make([]string, 0, len(f.docs))
	for n := range f.docs {
		names = append(names, n)
	}
	sort.Strings(names)
	out := make([]ModuleConfig, 0, len(names))
	for _, n := range names {
		cp, _ := f.FindByName(ctx, n)
		out = append(out, *cp)
	}
	return out, nil
}

func (f *fakeConfigRepo) Upsert(_ context.Context, c *ModuleConfig) error {
	f.docs[c.ModuleName] = c
	return nil
}

func (f *fakeConfigRepo) UpdateEnabled(context.Context, string, bool) error { return nil }

// MigrateToEnvironments mirrors the real one: a no-op fake would let the
// service believe a legacy document had been migrated while the stored one
// still had no Environments map at all. Like the Mongo compare-and-swap, it
// matches only a document that still has no profiles at the read revision.
func (f *fakeConfigRepo) MigrateToEnvironments(_ context.Context, name string, cv, ev map[string]string, expectedRevision int64) (bool, error) {
	if f.migrateErr != nil {
		return false, f.migrateErr
	}
	if f.beforeMigrate != nil {
		hook := f.beforeMigrate
		f.beforeMigrate = nil
		hook()
	}
	doc, ok := f.docs[name]
	if !ok {
		return false, fmt.Errorf("module %q not found", name)
	}
	if len(doc.Environments) > 0 || doc.ConfigRevision != expectedRevision {
		return false, nil
	}
	doc.ActiveEnvironment = "production"
	doc.Environments = map[string]EnvironmentConfig{
		"production": {ConfigValues: copyStrings(cv), EncryptedValues: copyStrings(ev)},
		"sandbox":    {ConfigValues: map[string]string{}, EncryptedValues: map[string]string{}},
	}
	doc.ConfigRevision = expectedRevision + 1
	return true, nil
}

func (f *fakeConfigRepo) ClearNeedsRestart(context.Context, string) error { return nil }

func (f *fakeConfigRepo) RefreshMetadata(context.Context, Module) error { return f.refreshErr }

// CompareAndSwapEnvironment mirrors the Mongo implementation's contract: a
// mismatched revision is a lost race (false, nil), not an error; the
// document-level configRevision advances in the same write. casFailures
// forces the first N attempts to lose.
func (f *fakeConfigRepo) CompareAndSwapEnvironment(_ context.Context, name, env string, expected int64, next EnvironmentConfig, needsRestart bool) (bool, error) {
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
	doc.NeedsRestart = needsRestart
	doc.ConfigRevision++
	return true, nil
}

// CompareAndSwapConfig mirrors the Mongo single-update contract: the whole
// mutation lands or nothing does, the revision is compared at execution time
// (after beforeDocCAS, so a modelled concurrent write is visible), and an
// activation copies the STORED profile maps rather than a caller snapshot.
func (f *fakeConfigRepo) CompareAndSwapConfig(_ context.Context, name string, m ConfigMutation) (bool, error) {
	if err := m.validate(); err != nil {
		return false, err
	}
	f.docCasCalls++
	if f.beforeDocCAS != nil {
		f.beforeDocCAS()
	}
	if f.docCasFailures > 0 {
		f.docCasFailures--
		return false, nil
	}
	doc, ok := f.docs[name]
	if !ok || doc.ConfigRevision != m.ExpectedRevision {
		return false, nil
	}
	if m.Activate != "" {
		if _, ok := doc.Environments[m.Activate]; !ok {
			return false, nil
		}
		if f.duringActivate != nil {
			f.duringActivate()
		}
		cfg := doc.Environments[m.Activate]
		doc.ActiveEnvironment = m.Activate
		doc.ConfigValues = copyStrings(cfg.ConfigValues)
		doc.EncryptedValues = copyStrings(cfg.EncryptedValues)
	} else {
		if m.Env != "" {
			cur, ok := doc.Environments[m.Env]
			if !ok {
				return false, nil
			}
			cur.ConfigValues = copyStrings(m.EnvValues)
			cur.EncryptedValues = copyStrings(m.EnvSecrets)
			cur.Revision = m.EnvRevision + 1
			doc.Environments[m.Env] = cur
		}
		if m.WriteLegacy {
			doc.ConfigValues = copyStrings(m.LegacyValues)
			doc.EncryptedValues = copyStrings(m.LegacySecrets)
		}
	}
	doc.NeedsRestart = m.NeedsRestart
	doc.ConfigRevision = m.ExpectedRevision + 1
	return true, nil
}
