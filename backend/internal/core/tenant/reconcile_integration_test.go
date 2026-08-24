package tenant

// Boot-reconciliation tests (tenant.Module.Start). They exercise the real
// upgrade path end to end: a real ModuleConfigService over a real
// module_configs collection, a real systeminit.Repo over a real system_init
// collection, and the real tenant repository/service — because every
// mechanism under test (the reconcile-lease CAS, AssignDefault's
// transaction, the config document's environment profiles) is a database
// behaviour with no substitutable fake.
//
// Point MONGO_TEST_URI at the replica-set instance:
//
//	MONGO_TEST_URI='mongodb://localhost:28017/?directConnection=true' \
//	  go test ./internal/core/tenant/... -run TestReconcile -v
//
// directConnection=true is mandatory against the CI mongod (replica set
// rs0) — without it the driver's replica-set discovery can resolve to a
// different, unrelated database on this host. newReconcileHarness skips
// the whole suite when MONGO_TEST_URI is unset, so a plain `go test ./...`
// reports these as skipped rather than failing.

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"io"
	"log/slog"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/orkestra/backend/internal/core/tenant/models"
	"github.com/orkestra/backend/internal/core/tenant/repository"
	"github.com/orkestra/backend/internal/shared/systeminit"
	"github.com/orkestra/backend/pkg/sdk/iface"
	"github.com/orkestra/backend/pkg/sdk/module"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

const reconcileMongoTestURIEnv = "MONGO_TEST_URI"

// stubUserCounter is the whole of the user seam boot reconciliation needs:
// "are there any operator users at all". Narrow on purpose — the module
// resolves module.ServiceUserService into the userCounter interface, so a
// test never has to implement all of iface.UserProvider.
type stubUserCounter struct {
	count int64
	err   error
}

func (s *stubUserCounter) GetUserCount(context.Context, *iface.UserFilters) (int64, error) {
	return s.count, s.err
}

// reconcileAuditSink records every emitted event so the tests can assert
// action, actor and cardinality without a compliance module.
type reconcileAuditSink struct {
	mu     sync.Mutex
	events []iface.AuditEvent
}

func (s *reconcileAuditSink) Emit(_ context.Context, e iface.AuditEvent) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.events = append(s.events, e)
}

func (s *reconcileAuditSink) countByAction(action string) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	n := 0
	for _, e := range s.events {
		if e.Action == action {
			n++
		}
	}
	return n
}

func (s *reconcileAuditSink) firstByAction(action string) (iface.AuditEvent, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, e := range s.events {
		if e.Action == action {
			return e, true
		}
	}
	return iface.AuditEvent{}, false
}

func (s *reconcileAuditSink) reset() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.events = nil
}

// reconcileNoopRedis satisfies module.RedisClient. These tests exercise the
// Mongo-backed config document, not the cache.
type reconcileNoopRedis struct{}

func (reconcileNoopRedis) Get(context.Context, string) (string, error) { return "", nil }
func (reconcileNoopRedis) Set(context.Context, string, interface{}, time.Duration) error {
	return nil
}
func (reconcileNoopRedis) Del(context.Context, ...string) error           { return nil }
func (reconcileNoopRedis) Keys(context.Context, string) ([]string, error) { return nil, nil }
func (reconcileNoopRedis) Incr(context.Context, string) (int64, error)    { return 1, nil }
func (reconcileNoopRedis) Expire(context.Context, string, time.Duration) error {
	return nil
}

type reconcileHarness struct {
	db      *mongo.Database
	repo    *repository.Repository
	cfgRepo *module.ModuleConfigRepository
	cfgSvc  *module.ModuleConfigService
	store   *systeminit.Repo
	users   *stubUserCounter
	sink    *reconcileAuditSink
	mod     *Module
}

func newReconcileHarness(t *testing.T) *reconcileHarness {
	t.Helper()
	uri := os.Getenv(reconcileMongoTestURIEnv)
	if uri == "" {
		t.Skipf("skipping integration test: set %s to run (e.g. mongodb://localhost:28017/?directConnection=true)", reconcileMongoTestURIEnv)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	client, err := mongo.Connect(ctx, options.Client().ApplyURI(uri))
	if err != nil {
		t.Fatalf("mongo connect: %v", err)
	}
	if err := client.Ping(ctx, nil); err != nil {
		t.Fatalf("mongo ping: %v", err)
	}
	db := client.Database("orkestra_test_tenant_reconcile_" + reconcileRandSuffix(t))
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = db.Drop(ctx)
		_ = client.Disconnect(ctx)
	})
	// Production builds this unique index at boot (module.go::Collections()).
	// AssignDefault's "one pointer per kind" invariant is enforced by it, so
	// a harness without it would test a weaker schema than production runs.
	//tenantscope:allow system: test setup mirrors the production index build for the platform-global default pointer collection
	if _, err := db.Collection(repository.CollDefaults).Indexes().CreateOne(ctx, mongo.IndexModel{
		Keys:    bson.D{{Key: "kind", Value: 1}},
		Options: options.Index().SetUnique(true),
	}); err != nil {
		t.Fatalf("create tenant_defaults unique kind index: %v", err)
	}

	store, err := systeminit.NewRepo(ctx, db)
	if err != nil {
		t.Fatalf("systeminit.NewRepo: %v", err)
	}
	h := &reconcileHarness{
		db:      db,
		repo:    repository.New(db),
		cfgRepo: module.NewModuleConfigRepository(db),
		store:   store,
		users:   &stubUserCounter{},
		sink:    &reconcileAuditSink{},
	}
	h.cfgSvc = module.NewModuleConfigService(h.cfgRepo, reconcileNoopRedis{},
		slog.New(slog.NewTextHandler(io.Discard, nil)))
	h.mod = h.newReplica(t)
	return h
}

// newReplica builds another tenant.Module over the SAME database, config
// service and coordinator store — a second backend replica booting against
// one installation. Every replica shares the harness's audit sink so a test
// can count events across all of them.
func (h *reconcileHarness) newReplica(t *testing.T) *Module {
	t.Helper()
	reg := module.NewServiceRegistry()
	reg.Register(module.ServiceSetupFinalizationStore, h.store)
	reg.Register(module.ServiceUserService, h.users)
	m := NewModule()
	h.cfgSvc.RegisterKnownModules([]module.Module{m})
	deps := &module.Dependencies{
		DB:            h.db,
		Services:      reg,
		ConfigService: h.cfgSvc,
		Logger:        slog.New(slog.NewTextHandler(io.Discard, nil)),
	}
	if err := m.Init(deps); err != nil {
		t.Fatalf("tenant Init: %v", err)
	}
	m.svc.SetAuditSink(h.sink)
	return m
}

func reconcileRandSuffix(t *testing.T) string {
	t.Helper()
	buf := make([]byte, 6)
	if _, err := rand.Read(buf); err != nil {
		t.Fatalf("rand.Read: %v", err)
	}
	return hex.EncodeToString(buf)
}

// seedTenant inserts a tenant row directly so the test controls createdAt
// (repository.CreateTenant stamps time.Now() and UpdateTenant refuses the
// immutable createdAt field).
func (h *reconcileHarness) seedTenant(t *testing.T, uuid string, status models.TenantStatus, createdAt time.Time, deleted bool) {
	t.Helper()
	doc := bson.M{
		"uuid":          uuid,
		"kind":          string(models.TenantKindInternal),
		"status":        string(status),
		"name":          "Tenant " + uuid,
		"slug":          "slug-" + uuid,
		"ownerUserUUID": "owner-" + uuid,
		"createdAt":     createdAt,
		"updatedAt":     createdAt,
	}
	if deleted {
		doc["deletedAt"] = createdAt
	}
	//tenantscope:allow system: test fixture seeds the tenant registry directly so createdAt is controllable (the repository stamps time.Now())
	if _, err := h.db.Collection(repository.CollTenants).InsertOne(context.Background(), doc); err != nil {
		t.Fatalf("seed tenant %s: %v", uuid, err)
	}
}

func (h *reconcileHarness) seedExternalTenant(t *testing.T, uuid string, createdAt time.Time) {
	t.Helper()
	doc := bson.M{
		"uuid":          uuid,
		"kind":          string(models.TenantKindExternal),
		"status":        string(models.TenantStatusActive),
		"name":          "Client " + uuid,
		"slug":          "slug-" + uuid,
		"ownerUserUUID": "owner-" + uuid,
		"createdAt":     createdAt,
		"updatedAt":     createdAt,
	}
	//tenantscope:allow system: test fixture seeds the tenant registry directly so createdAt is controllable (the repository stamps time.Now())
	if _, err := h.db.Collection(repository.CollTenants).InsertOne(context.Background(), doc); err != nil {
		t.Fatalf("seed external tenant %s: %v", uuid, err)
	}
}

// seedTenantConfig writes a module_configs document for the tenant module
// with the given legacy top-level values and per-environment profiles,
// bypassing ModuleConfigService's validator the way a legacy install's
// already-persisted document does.
func (h *reconcileHarness) seedTenantConfig(t *testing.T, legacy map[string]string, envs map[string]map[string]string) {
	t.Helper()
	environments := make(map[string]module.EnvironmentConfig, len(envs))
	for name, values := range envs {
		environments[name] = module.EnvironmentConfig{
			ConfigValues:    values,
			EncryptedValues: map[string]string{},
			UpdatedAt:       time.Now(),
		}
	}
	doc := &module.ModuleConfig{
		ModuleName:        "tenant",
		Category:          module.CategoryCore,
		Enabled:           true,
		ConfigValues:      legacy,
		EncryptedValues:   map[string]string{},
		ActiveEnvironment: "production",
		Environments:      environments,
	}
	if err := h.cfgRepo.Upsert(context.Background(), doc); err != nil {
		t.Fatalf("seed tenant module config: %v", err)
	}
}

func (h *reconcileHarness) configDoc(t *testing.T) *module.ModuleConfig {
	t.Helper()
	doc, err := h.cfgRepo.FindByName(context.Background(), "tenant")
	if err != nil {
		t.Fatalf("read tenant module config: %v", err)
	}
	if doc == nil {
		t.Fatal("tenant module config document is missing")
	}
	return doc
}

func (h *reconcileHarness) record(t *testing.T) *systeminit.FinalizationRecord {
	t.Helper()
	rec, err := h.store.Get(context.Background())
	if err != nil {
		t.Fatalf("read setup coordinator: %v", err)
	}
	return rec
}

// resetReconciliationVersion rewinds the stamped version so a second run
// actually performs the reconciliation instead of short-circuiting on the
// version check — the only way to prove a STEP is idempotent rather than
// merely gated.
func (h *reconcileHarness) resetReconciliationVersion(t *testing.T) {
	t.Helper()
	//tenantscope:allow system: test fixture rewinds the platform-global setup coordinator's stamped version
	if _, err := h.db.Collection("system_init").UpdateOne(context.Background(),
		bson.M{"key": "setup_finalization"},
		bson.M{"$set": bson.M{"reconciliationVersion": 0}}); err != nil {
		t.Fatalf("reset reconciliation version: %v", err)
	}
}

func (h *reconcileHarness) tenantCount(t *testing.T) int64 {
	t.Helper()
	//tenantscope:allow system: test assertion counts every tenant row platform-wide to prove reconciliation created none
	n, err := h.db.Collection(repository.CollTenants).CountDocuments(context.Background(), bson.M{})
	if err != nil {
		t.Fatalf("count tenants: %v", err)
	}
	return n
}

func (h *reconcileHarness) membershipCount(t *testing.T) int64 {
	t.Helper()
	//tenantscope:allow system: test assertion counts every membership row platform-wide to prove reconciliation touched none
	n, err := h.db.Collection(repository.CollMemberships).CountDocuments(context.Background(), bson.M{})
	if err != nil {
		t.Fatalf("count memberships: %v", err)
	}
	return n
}

func (h *reconcileHarness) defaultPointer(t *testing.T) *models.TenantDefault {
	t.Helper()
	d, err := h.repo.GetDefault(context.Background(), models.TenantKindInternal)
	if errors.Is(err, repository.ErrNotFound) {
		return nil
	}
	if err != nil {
		t.Fatalf("read default pointer: %v", err)
	}
	return d
}

// rawDefaultPointer reads the pointer document without decoding through the
// model so an ABSENT updatedBy can be told apart from an empty string.
func (h *reconcileHarness) rawDefaultPointer(t *testing.T) bson.M {
	t.Helper()
	var raw bson.M
	//tenantscope:allow system: test assertion reads the platform-global default pointer row verbatim to prove updatedBy is absent, not empty
	err := h.db.Collection(repository.CollDefaults).FindOne(context.Background(),
		bson.M{"kind": string(models.TenantKindInternal)}).Decode(&raw)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return nil
	}
	if err != nil {
		t.Fatalf("read raw default pointer: %v", err)
	}
	return raw
}

const (
	internalModeKey = "provisioning.internal.mode"
	externalModeKey = "provisioning.external.mode"
	openMigrated    = "tenant.provisioning.internal_open_migrated"
	defaultAssigned = "tenant.default.assigned"
)

// --- Case 1: upgrade with users but zero operational Tier-1 tenants ------

func TestReconcile_NoOperationalTenant_CreatesLegacyRecoveryRecord(t *testing.T) {
	t.Run("no tenant rows at all", func(t *testing.T) {
		h := newReconcileHarness(t)
		h.users.count = 3
		if err := h.mod.Start(context.Background()); err != nil {
			t.Fatalf("Start: %v", err)
		}
		rec := h.record(t)
		if rec == nil {
			t.Fatal("expected a setup coordinator record to be created")
		}
		if rec.Source != systeminit.SourceLegacyRecovery {
			t.Errorf("record source = %q, want %q", rec.Source, systeminit.SourceLegacyRecovery)
		}
		if rec.AdminUUID != "" {
			t.Errorf("record adminUUID = %q, want empty (an active super_admin claims it later)", rec.AdminUUID)
		}
		if rec.CompletedAt != nil {
			t.Error("legacy-recovery record must not be marked complete: setup is still tenant_required")
		}
		if rec.Stage != systeminit.StageConfig {
			t.Errorf("record stage = %d, want %d (StageConfig)", rec.Stage, systeminit.StageConfig)
		}
		if rec.ReconciliationVersion != setupReconciliationVersion {
			t.Errorf("reconciliationVersion = %d, want %d", rec.ReconciliationVersion, setupReconciliationVersion)
		}
		if d := h.defaultPointer(t); d != nil {
			t.Errorf("default pointer = %+v, want none assigned", d)
		}
		if n := h.tenantCount(t); n != 0 {
			t.Errorf("tenant rows = %d, want 0: reconciliation must never create a tenant", n)
		}
	})

	t.Run("provisioning and suspended tenants occupy slots but are not operational", func(t *testing.T) {
		h := newReconcileHarness(t)
		h.users.count = 3
		base := time.Now().Add(-72 * time.Hour).UTC()
		h.seedTenant(t, "t-provisioning", models.TenantStatusProvisioning, base, false)
		h.seedTenant(t, "t-suspended", models.TenantStatusSuspended, base.Add(time.Hour), false)

		if err := h.mod.Start(context.Background()); err != nil {
			t.Fatalf("Start: %v", err)
		}
		rec := h.record(t)
		if rec == nil || rec.Source != systeminit.SourceLegacyRecovery {
			t.Fatalf("record = %+v, want a legacy_recovery record", rec)
		}
		if d := h.defaultPointer(t); d != nil {
			t.Errorf("default pointer = %+v: a slot occupant that is not operational must never be selected", d)
		}
		if n := h.tenantCount(t); n != 2 {
			t.Errorf("tenant rows = %d, want the 2 seeded rows untouched", n)
		}
		if n := h.membershipCount(t); n != 0 {
			t.Errorf("membership rows = %d, want 0: reconciliation never touches memberships", n)
		}
	})
}

// --- Case 2: pristine install ---------------------------------------------

func TestReconcile_PristineInstall_CreatesNoRecordAndStampsNoVersion(t *testing.T) {
	h := newReconcileHarness(t)
	h.users.count = 0
	h.seedTenantConfig(t,
		map[string]string{internalModeKey: "open"},
		map[string]map[string]string{
			"production": {internalModeKey: "open"},
			"sandbox":    {internalModeKey: "open"},
		})

	if err := h.mod.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if rec := h.record(t); rec != nil {
		t.Fatalf("record = %+v, want none: the fresh path binds the coordinator via InitializeFresh after admin creation", rec)
	}
	// The config rewrite is the one step that still runs — it is idempotent
	// and safe for concurrent replicas without coordination.
	doc := h.configDoc(t)
	if got := doc.ConfigValues[internalModeKey]; got != models.ProvisioningModeManual {
		t.Errorf("legacy top-level internal mode = %q, want %q", got, models.ProvisioningModeManual)
	}
	for _, env := range []string{"production", "sandbox"} {
		if got := doc.Environments[env].ConfigValues[internalModeKey]; got != models.ProvisioningModeManual {
			t.Errorf("%s internal mode = %q, want %q", env, got, models.ProvisioningModeManual)
		}
	}
}

// --- Case 3: one operational Tier-1 tenant --------------------------------

func TestReconcile_OneOperationalTenant_BecomesPlatformDefault(t *testing.T) {
	h := newReconcileHarness(t)
	h.users.count = 2
	created := time.Now().Add(-48 * time.Hour).UTC()
	h.seedTenant(t, "t-only", models.TenantStatusActive, created, false)

	if err := h.mod.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}

	d := h.defaultPointer(t)
	if d == nil || d.TenantUUID != "t-only" {
		t.Fatalf("default pointer = %+v, want it to name t-only", d)
	}
	if d.UpdateSource != models.DefaultUpdateSourceMigration {
		t.Errorf("updateSource = %q, want %q", d.UpdateSource, models.DefaultUpdateSourceMigration)
	}
	raw := h.rawDefaultPointer(t)
	if _, present := raw["updatedBy"]; present {
		t.Error("updatedBy must be ABSENT on a migration-sourced pointer row, never an empty-string sentinel")
	}

	ev, ok := h.sink.firstByAction(defaultAssigned)
	if !ok {
		t.Fatalf("no %s audit event recorded", defaultAssigned)
	}
	if ev.ActorType != "system" {
		t.Errorf("ActorType = %q, want \"system\"", ev.ActorType)
	}
	if ev.ActorUserID != "" {
		t.Errorf("ActorUserID = %q, want empty — the literal \"system\" must never land in an actor-UUID field", ev.ActorUserID)
	}

	rec := h.record(t)
	if rec == nil {
		t.Fatal("expected a setup coordinator record")
	}
	if rec.Source != systeminit.SourceMigration {
		t.Errorf("record source = %q, want %q", rec.Source, systeminit.SourceMigration)
	}
	if rec.CompletedAt == nil {
		t.Error("a migration record whose default was assigned without interaction must be complete")
	}
	if rec.Result == nil || rec.Result.TenantUUID != "t-only" {
		t.Errorf("record result = %+v, want a snapshot of t-only", rec.Result)
	}
	if rec.ReconciliationVersion != setupReconciliationVersion {
		t.Errorf("reconciliationVersion = %d, want %d", rec.ReconciliationVersion, setupReconciliationVersion)
	}
	if n := h.tenantCount(t); n != 1 {
		t.Errorf("tenant rows = %d, want the single seeded row untouched", n)
	}
}

// --- Case 4: several operational tenants ----------------------------------

func TestReconcile_MultipleOperationalTenants_OldestWins(t *testing.T) {
	t.Run("oldest operational wins and non-operational rows are never selected", func(t *testing.T) {
		h := newReconcileHarness(t)
		h.users.count = 4
		base := time.Now().Add(-200 * time.Hour).UTC()
		// Every non-operational row below is OLDER than the winner, so a
		// selection that forgot the status/deletedAt predicate would pick
		// one of them instead.
		h.seedTenant(t, "t-suspended", models.TenantStatusSuspended, base, false)
		h.seedTenant(t, "t-archived", models.TenantStatusArchived, base.Add(time.Minute), false)
		h.seedTenant(t, "t-purged", models.TenantStatusPurged, base.Add(2*time.Minute), false)
		h.seedTenant(t, "t-soft-deleted", models.TenantStatusActive, base.Add(3*time.Minute), true)
		h.seedTenant(t, "t-provisioning", models.TenantStatusProvisioning, base.Add(4*time.Minute), false)
		h.seedExternalTenant(t, "t-external", base.Add(5*time.Minute))
		// Operational Tier-1 candidates.
		h.seedTenant(t, "t-winner", models.TenantStatusActive, base.Add(time.Hour), false)
		h.seedTenant(t, "t-younger", models.TenantStatusActive, base.Add(2*time.Hour), false)

		if err := h.mod.Start(context.Background()); err != nil {
			t.Fatalf("Start: %v", err)
		}
		d := h.defaultPointer(t)
		if d == nil || d.TenantUUID != "t-winner" {
			t.Fatalf("default pointer = %+v, want the oldest OPERATIONAL Tier-1 tenant (t-winner)", d)
		}
		if n := h.tenantCount(t); n != 8 {
			t.Errorf("tenant rows = %d, want the 8 seeded rows untouched", n)
		}
	})

	t.Run("uuid ascending breaks a createdAt tie", func(t *testing.T) {
		h := newReconcileHarness(t)
		h.users.count = 2
		same := time.Now().Add(-24 * time.Hour).UTC()
		h.seedTenant(t, "t-bbb", models.TenantStatusActive, same, false)
		h.seedTenant(t, "t-aaa", models.TenantStatusActive, same, false)

		if err := h.mod.Start(context.Background()); err != nil {
			t.Fatalf("Start: %v", err)
		}
		d := h.defaultPointer(t)
		if d == nil || d.TenantUUID != "t-aaa" {
			t.Fatalf("default pointer = %+v, want the deterministic uuid-ascending tie-break (t-aaa)", d)
		}
	})
}

// --- Case 5: Tier-1 open → manual migration -------------------------------

func TestReconcile_MigratesTier1OpenToManual(t *testing.T) {
	h := newReconcileHarness(t)
	h.users.count = 2
	h.seedTenant(t, "t-only", models.TenantStatusActive, time.Now().Add(-time.Hour).UTC(), false)
	h.seedTenantConfig(t,
		map[string]string{internalModeKey: "open", externalModeKey: "open"},
		map[string]map[string]string{
			"production": {internalModeKey: "open", externalModeKey: "open"},
			"sandbox":    {internalModeKey: "open", externalModeKey: "open"},
		})

	if err := h.mod.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}

	assertMigrated := func(t *testing.T, when string) {
		t.Helper()
		doc := h.configDoc(t)
		if got := doc.ConfigValues[internalModeKey]; got != models.ProvisioningModeManual {
			t.Errorf("%s: legacy top-level internal mode = %q, want %q", when, got, models.ProvisioningModeManual)
		}
		if got := doc.ConfigValues[externalModeKey]; got != models.ProvisioningModeOpen {
			t.Errorf("%s: legacy top-level EXTERNAL mode = %q, want %q — Tier-2 open is untouched", when, got, models.ProvisioningModeOpen)
		}
		for _, env := range []string{"production", "sandbox"} {
			if got := doc.Environments[env].ConfigValues[internalModeKey]; got != models.ProvisioningModeManual {
				t.Errorf("%s: %s internal mode = %q, want %q", when, env, got, models.ProvisioningModeManual)
			}
			if got := doc.Environments[env].ConfigValues[externalModeKey]; got != models.ProvisioningModeOpen {
				t.Errorf("%s: %s EXTERNAL mode = %q, want %q — Tier-2 open is untouched", when, env, got, models.ProvisioningModeOpen)
			}
		}
	}
	assertMigrated(t, "after first run")
	if n := h.sink.countByAction(openMigrated); n != 1 {
		t.Fatalf("%s events = %d, want exactly 1 per installation", openMigrated, n)
	}

	// Rewind the stamped version so the second run genuinely re-executes the
	// rewrite step instead of short-circuiting on the version check: this is
	// what proves the STEP is idempotent, not merely gated.
	h.resetReconciliationVersion(t)
	if err := h.mod.Start(context.Background()); err != nil {
		t.Fatalf("second Start: %v", err)
	}
	assertMigrated(t, "after second run")
	if n := h.sink.countByAction(openMigrated); n != 1 {
		t.Errorf("%s events after a second reconciliation = %d, want still exactly 1", openMigrated, n)
	}
}

// --- Case 6: legacy `single` with several occupied slots -------------------

func TestReconcile_LegacySingleWithMultipleSlots_ModeRetained(t *testing.T) {
	h := newReconcileHarness(t)
	h.users.count = 3
	base := time.Now().Add(-100 * time.Hour).UTC()
	h.seedTenant(t, "t-a", models.TenantStatusActive, base, false)
	h.seedTenant(t, "t-b", models.TenantStatusActive, base.Add(time.Hour), false)
	h.seedTenant(t, "t-c", models.TenantStatusSuspended, base.Add(2*time.Hour), false)
	h.seedTenantConfig(t,
		map[string]string{internalModeKey: models.ProvisioningModeSingle},
		map[string]map[string]string{
			"production": {internalModeKey: models.ProvisioningModeSingle},
			"sandbox":    {internalModeKey: models.ProvisioningModeSingle},
		})

	if err := h.mod.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}

	doc := h.configDoc(t)
	if got := doc.ConfigValues[internalModeKey]; got != models.ProvisioningModeSingle {
		t.Errorf("legacy top-level internal mode = %q, want %q retained — a remediation state, never silently loosened", got, models.ProvisioningModeSingle)
	}
	for _, env := range []string{"production", "sandbox"} {
		if got := doc.Environments[env].ConfigValues[internalModeKey]; got != models.ProvisioningModeSingle {
			t.Errorf("%s internal mode = %q, want %q retained", env, got, models.ProvisioningModeSingle)
		}
	}
	if n := h.sink.countByAction(openMigrated); n != 0 {
		t.Errorf("%s events = %d, want 0 — nothing was rewritten", openMigrated, n)
	}
	if d := h.defaultPointer(t); d == nil || d.TenantUUID != "t-a" {
		t.Errorf("default pointer = %+v, want the oldest operational tenant (t-a)", d)
	}
	rec := h.record(t)
	if rec == nil || rec.Result == nil || rec.Result.Mode != models.ProvisioningModeSingle {
		t.Errorf("record = %+v, want a completed migration record carrying the retained single mode", rec)
	}
}

// --- Case 7: version check on later boots ---------------------------------

func TestReconcile_SecondBootPerformsOnlyTheVersionRead(t *testing.T) {
	h := newReconcileHarness(t)
	h.users.count = 2
	h.seedTenant(t, "t-only", models.TenantStatusActive, time.Now().Add(-time.Hour).UTC(), false)

	if err := h.mod.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	rec := h.record(t)
	if rec == nil || rec.ReconciliationVersion != setupReconciliationVersion {
		t.Fatalf("record = %+v, want reconciliationVersion %d", rec, setupReconciliationVersion)
	}

	// Plant work the reconciliation WOULD do if it ran again. A second boot
	// must leave it alone: the version read is the only thing it performs.
	h.seedTenantConfig(t,
		map[string]string{internalModeKey: "open"},
		map[string]map[string]string{"production": {internalModeKey: "open"}})
	h.sink.reset()

	if err := h.mod.Start(context.Background()); err != nil {
		t.Fatalf("second Start: %v", err)
	}
	doc := h.configDoc(t)
	if got := doc.ConfigValues[internalModeKey]; got != "open" {
		t.Errorf("legacy top-level internal mode = %q, want it left at %q — a completed version must perform no work", got, "open")
	}
	if n := len(h.sink.events); n != 0 {
		t.Errorf("audit events on the second boot = %d, want 0", n)
	}
	if rec := h.record(t); rec == nil || rec.ReconciliationVersion != setupReconciliationVersion {
		t.Errorf("record = %+v, want reconciliationVersion still %d", rec, setupReconciliationVersion)
	}
}

// --- Case 8: concurrent replicas ------------------------------------------

func TestReconcile_ConcurrentReplicas_OneLeaseWinner(t *testing.T) {
	t.Run("two replicas both succeed and only one performs the writes", func(t *testing.T) {
		h := newReconcileHarness(t)
		h.users.count = 5
		base := time.Now().Add(-96 * time.Hour).UTC()
		h.seedTenant(t, "t-winner", models.TenantStatusActive, base, false)
		h.seedTenant(t, "t-younger", models.TenantStatusActive, base.Add(time.Hour), false)
		// A legacy `open` to rewrite makes the lease election observable:
		// the rewrite and its audit event live inside the leased body, so
		// two events would mean both replicas performed the writes.
		h.seedTenantConfig(t,
			map[string]string{internalModeKey: "open"},
			map[string]map[string]string{
				"production": {internalModeKey: "open"},
				"sandbox":    {internalModeKey: "open"},
			})
		replicaB := h.newReplica(t)

		var wg sync.WaitGroup
		start := make(chan struct{})
		errs := make([]error, 2)
		for i, m := range []*Module{h.mod, replicaB} {
			wg.Add(1)
			go func(i int, m *Module) {
				defer wg.Done()
				<-start
				errs[i] = m.Start(context.Background())
			}(i, m)
		}
		close(start)
		wg.Wait()

		for i, err := range errs {
			if err != nil {
				t.Errorf("replica %d Start: %v", i, err)
			}
		}
		if n := h.sink.countByAction(defaultAssigned); n != 1 {
			t.Errorf("%s events = %d, want exactly 1: only the lease winner writes", defaultAssigned, n)
		}
		if n := h.sink.countByAction(openMigrated); n != 1 {
			t.Errorf("%s events = %d, want exactly 1: the rewrite runs inside the leased body", openMigrated, n)
		}
		if d := h.defaultPointer(t); d == nil || d.TenantUUID != "t-winner" {
			t.Errorf("default pointer = %+v, want t-winner", d)
		}
		rec := h.record(t)
		if rec == nil || rec.ReconciliationVersion != setupReconciliationVersion {
			t.Fatalf("record = %+v, want reconciliationVersion %d", rec, setupReconciliationVersion)
		}
		if rec.ReconcileLeaseOwner != "" || rec.ReconcileLeaseUntil != nil {
			t.Errorf("reconcile lease not released: owner=%q until=%v", rec.ReconcileLeaseOwner, rec.ReconcileLeaseUntil)
		}
		// The saga's own stage lease is a separate mechanism on the same
		// document; reconciliation must never read or clear its fields.
		if rec.LeaseOwner != "" || rec.LeaseUntil != nil {
			t.Errorf("reconciliation touched the saga stage lease: owner=%q until=%v", rec.LeaseOwner, rec.LeaseUntil)
		}
	})

	t.Run("an expired reconcile lease is taken over", func(t *testing.T) {
		h := newReconcileHarness(t)
		h.users.count = 2
		h.seedTenant(t, "t-only", models.TenantStatusActive, time.Now().Add(-time.Hour).UTC(), false)
		if _, err := h.store.EnsureRecord(context.Background(), systeminit.SourceLegacyRecovery, nil); err != nil {
			t.Fatalf("EnsureRecord: %v", err)
		}
		expired := time.Now().Add(-5 * time.Minute).UTC()
		//tenantscope:allow system: test fixture plants an expired reconcile lease on the platform-global setup coordinator
		if _, err := h.db.Collection("system_init").UpdateOne(context.Background(),
			bson.M{"key": "setup_finalization"},
			bson.M{"$set": bson.M{"reconcileLeaseOwner": "crashed-replica", "reconcileLeaseUntil": expired}}); err != nil {
			t.Fatalf("plant expired lease: %v", err)
		}

		if err := h.mod.Start(context.Background()); err != nil {
			t.Fatalf("Start: %v", err)
		}
		rec := h.record(t)
		if rec == nil || rec.ReconciliationVersion != setupReconciliationVersion {
			t.Fatalf("record = %+v, want reconciliationVersion %d after taking over the expired lease", rec, setupReconciliationVersion)
		}
		if d := h.defaultPointer(t); d == nil || d.TenantUUID != "t-only" {
			t.Errorf("default pointer = %+v, want t-only", d)
		}
	})
}

// --- Case 9: database failure ---------------------------------------------

func TestReconcile_DatabaseFailure_StartReturnsError(t *testing.T) {
	h := newReconcileHarness(t)
	h.users.count = 2
	h.seedTenant(t, "t-only", models.TenantStatusActive, time.Now().Add(-time.Hour).UTC(), false)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := h.mod.Start(ctx); err == nil {
		t.Fatal("Start returned nil on a failed coordinator read: a reconciliation failure must abort startup before the HTTP listener")
	}
}

// --- Case 10: an existing default is never re-pointed ---------------------

func TestReconcile_ExistingDefault_LeftUntouched(t *testing.T) {
	h := newReconcileHarness(t)
	h.users.count = 3
	base := time.Now().Add(-50 * time.Hour).UTC()
	h.seedTenant(t, "t-oldest", models.TenantStatusActive, base, false)
	h.seedTenant(t, "t-chosen", models.TenantStatusActive, base.Add(time.Hour), false)

	actor := "11111111-2222-3333-4444-555555555555"
	if err := h.mod.svc.AssignDefaultTenant(context.Background(), "t-chosen", actor, models.DefaultUpdateSourceSetup); err != nil {
		t.Fatalf("seed existing default: %v", err)
	}
	h.sink.reset()

	if err := h.mod.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	d := h.defaultPointer(t)
	if d == nil || d.TenantUUID != "t-chosen" {
		t.Fatalf("default pointer = %+v, want the pre-existing t-chosen (never re-pointed at the oldest)", d)
	}
	if d.UpdateSource != models.DefaultUpdateSourceSetup {
		t.Errorf("updateSource = %q, want the original %q", d.UpdateSource, models.DefaultUpdateSourceSetup)
	}
	if n := h.sink.countByAction(defaultAssigned); n != 0 {
		t.Errorf("%s events = %d, want 0 — the pointer already existed", defaultAssigned, n)
	}
	rec := h.record(t)
	if rec == nil || rec.Source != systeminit.SourceMigration {
		t.Fatalf("record = %+v, want a migration record", rec)
	}
	if rec.Result == nil || rec.Result.TenantUUID != "t-chosen" {
		t.Errorf("record result = %+v, want a snapshot of the existing default", rec.Result)
	}
}
