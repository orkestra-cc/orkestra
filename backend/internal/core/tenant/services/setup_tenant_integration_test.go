package services

// These tests exercise EnsureSetupTenant — the reserved-UUID reconciliation
// seam a later PR's resumable setup saga calls, potentially many times, to
// converge a reserved tenant UUID into a fully provisioned, operational
// tenant, no matter how many times the stage is replayed after a lost
// response, a crashed executor, or an expired lease. They run against a
// genuine replica-set MongoDB so the real unique indexes (tenants' `uuid`
// and `slug`, the membership `(userUUID, tenantId)` compound, and the
// ancestor `(descendantUUID, ancestorUUID)` compound) drive the
// duplicate-key races this seam is built to survive — a mock repository
// could not exercise that. Point MONGO_TEST_URI at the replica-set
// instance, e.g.:
//
//	MONGO_TEST_URI='mongodb://localhost:28017/?directConnection=true' \
//	  go test ./internal/core/tenant/services/... -run TestEnsureSetupTenant -v
//
// directConnection=true is mandatory against the CI mongod (replica set
// rs0) — without it the driver's replica-set discovery can resolve to a
// different, unrelated database on this host. newSetupTenantTestDB skips the
// whole suite when MONGO_TEST_URI is unset, so a plain `go test ./...` run
// reports these as skipped rather than failing.

import (
	"context"
	"errors"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/orkestra/backend/internal/core/tenant/models"
	"github.com/orkestra/backend/internal/core/tenant/repository"
	"github.com/orkestra/backend/pkg/sdk/iface"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// newSetupTenantTestDB spins up an isolated database for one test, building
// the same unique indexes module.go::Collections() builds at boot for the
// tenants / tenant_memberships / tenant_ancestors collections. Without them
// the duplicate-key races EnsureSetupTenant is built to survive can't fire
// at all, so a harness that skipped them would exercise a weaker,
// unrepresentative schema than production ever runs. Each test gets a
// unique database name (randSuffix, shared with the sibling
// default_tenant_integration_test.go harness in this package); dropped on
// teardown.
func newSetupTenantTestDB(t *testing.T) (*mongo.Database, func()) {
	t.Helper()
	uri := os.Getenv(defaultsMongoTestURIEnv)
	if uri == "" {
		t.Skipf("skipping integration test: set %s to run (e.g. mongodb://localhost:28017/?directConnection=true)", defaultsMongoTestURIEnv)
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
	dbName := "orkestra_test_tenant_svc_setup_" + randSuffix(t)
	db := client.Database(dbName)

	//tenantscope:allow system: test setup mirrors the production index build (module.go::Collections()) for the tenant registry's uuid/slug uniqueness
	if _, err := db.Collection(repository.CollTenants).Indexes().CreateMany(ctx, []mongo.IndexModel{
		{Keys: bson.D{{Key: "uuid", Value: 1}}, Options: options.Index().SetUnique(true)},
		{Keys: bson.D{{Key: "slug", Value: 1}}, Options: options.Index().SetUnique(true).SetSparse(true)},
	}); err != nil {
		t.Fatalf("create tenants unique indexes: %v", err)
	}
	//tenantscope:allow system: test setup mirrors the production index build (module.go::Collections()) for the owner-membership uniqueness EnsureSetupTenant's reconcile relies on
	if _, err := db.Collection(repository.CollMemberships).Indexes().CreateOne(ctx, mongo.IndexModel{
		Keys:    bson.D{{Key: "userUUID", Value: 1}, {Key: "tenantId", Value: 1}},
		Options: options.Index().SetUnique(true),
	}); err != nil {
		t.Fatalf("create tenant_memberships unique index: %v", err)
	}
	//tenantscope:allow system: test setup mirrors the production index build (module.go::Collections()) for the closure self-row uniqueness EnsureSetupTenant's reconcile relies on
	if _, err := db.Collection(repository.CollAncestors).Indexes().CreateOne(ctx, mongo.IndexModel{
		Keys:    bson.D{{Key: "descendantUUID", Value: 1}, {Key: "ancestorUUID", Value: 1}},
		Options: options.Index().SetUnique(true),
	}); err != nil {
		t.Fatalf("create tenant_ancestors unique index: %v", err)
	}

	cleanup := func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = db.Drop(ctx)
		_ = client.Disconnect(ctx)
	}
	return db, cleanup
}

// fakeSetupKMS is a minimal, mutex-guarded iface.KMSProvider fake standing in
// for a real KMS module in these service-layer tests. CreateKey is
// idempotent per tenant (mirrors Task 4.1's LocalKMS.CreateKey contract —
// racing callers for one tenantUUID converge on a single key) and records
// every FRESH mint so tests can assert EnsureSetupTenant never causes a
// second key to be minted for the same tenant across retries or concurrent
// callers.
type fakeSetupKMS struct {
	mu     sync.Mutex
	keys   map[string]string
	minted map[string]int
}

var _ iface.KMSProvider = (*fakeSetupKMS)(nil)

func newFakeSetupKMS() *fakeSetupKMS {
	return &fakeSetupKMS{keys: map[string]string{}, minted: map[string]int{}}
}

func (f *fakeSetupKMS) CreateKey(_ context.Context, tenantUUID string) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if id, ok := f.keys[tenantUUID]; ok {
		return id, nil
	}
	id := "kms-" + tenantUUID
	f.keys[tenantUUID] = id
	f.minted[tenantUUID]++
	return id, nil
}

func (f *fakeSetupKMS) Encrypt(_ context.Context, _ string, plaintext []byte) ([]byte, error) {
	return plaintext, nil
}

func (f *fakeSetupKMS) Decrypt(_ context.Context, _ string, ciphertext []byte) ([]byte, error) {
	return ciphertext, nil
}

func (f *fakeSetupKMS) DeleteKey(_ context.Context, _ string) error { return nil }

// mintCount reports how many FRESH (non-cache-hit) keys CreateKey has minted
// for tenantUUID. A correct EnsureSetupTenant never pushes this above 1 for
// one reserved tenant, no matter how many times it is retried or how many
// concurrent callers race on it.
func (f *fakeSetupKMS) mintCount(tenantUUID string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.minted[tenantUUID]
}

// setupBindCall records one OwnerRoleBinder invocation.
type setupBindCall struct {
	ownerUUID, tenantUUID, roleName string
}

// fakeSetupBinder is a mutex-guarded OwnerRoleBinder fake. failFirst — set
// before the service is exercised, never mutated concurrently with it —
// controls how many LEADING calls return an error, simulating the
// bind-owner failure that makes createTenantWithUUID soft-delete a
// partially-provisioned tenant.
type fakeSetupBinder struct {
	mu        sync.Mutex
	calls     []setupBindCall
	failFirst int
}

func (f *fakeSetupBinder) bind(_ context.Context, ownerUUID, tenantUUID, roleName string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, setupBindCall{ownerUUID, tenantUUID, roleName})
	if f.failFirst > 0 {
		f.failFirst--
		return errors.New("fake bind owner failure")
	}
	return nil
}

func (f *fakeSetupBinder) callCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.calls)
}

// newSetupTenantService wires a Service against the real repository plus the
// two fakes EnsureSetupTenant depends on: a recording, concurrent-idempotent
// KMS provider and a recording owner-role binder.
func newSetupTenantService(db *mongo.Database) (*repository.Repository, *Service, *fakeSetupKMS, *fakeSetupBinder) {
	repo := repository.New(db)
	svc := New(repo)
	kms := newFakeSetupKMS()
	binder := &fakeSetupBinder{}
	svc.SetKMSProvider(kms)
	svc.SetOwnerRoleBinder(binder.bind)
	return repo, svc, kms, binder
}

// singleModeResolver is a ProvisioningModeResolver that always reports
// `single` for every tier, regardless of kind.
func singleModeResolver(_ context.Context, _ models.TenantKind) string {
	return models.ProvisioningModeSingle
}

// seedRawTenant inserts tn exactly as given via the plain repository
// CreateTenant path — no service-level defaulting or provisioning gate —
// so tests can construct fixtures (soft-deleted, purged, legacy-empty-plan
// rows) that no service method would ever produce on its own.
func seedRawTenant(t *testing.T, repo *repository.Repository, tn *models.Tenant) {
	t.Helper()
	if err := repo.CreateTenant(context.Background(), tn); err != nil {
		t.Fatalf("seed tenant %s: %v", tn.UUID, err)
	}
}

// TestEnsureSetupTenant_FreshCreatesWithReservedUUIDAndEnterprisePlan is the
// spec's explicit plan test: it must fail if EnsureSetupTenant omitted the
// EXPLICIT models.PlanEnterprise on the fresh-creation path and inherited
// createTenantWithUUID's empty-plan-defaults-to-free fallback instead.
func TestEnsureSetupTenant_FreshCreatesWithReservedUUIDAndEnterprisePlan(t *testing.T) {
	db, cleanup := newSetupTenantTestDB(t)
	defer cleanup()
	repo, svc, kms, binder := newSetupTenantService(db)
	ctx := context.Background()

	suffix := randSuffix(t)
	tenantUUID := "setup-" + suffix
	ownerUUID := "owner-" + suffix
	name := "Setup Tenant " + suffix
	slug := "setup-tenant-" + suffix

	if err := svc.EnsureSetupTenant(ctx, tenantUUID, ownerUUID, name, slug); err != nil {
		t.Fatalf("EnsureSetupTenant: %v", err)
	}

	tn, err := repo.GetTenantByUUID(ctx, tenantUUID)
	if err != nil {
		t.Fatalf("GetTenantByUUID: %v", err)
	}
	if tn.Kind != models.TenantKindInternal {
		t.Errorf("Kind = %q, want internal", tn.Kind)
	}
	if tn.Status != models.TenantStatusActive {
		t.Errorf("Status = %q, want active", tn.Status)
	}
	if tn.Plan != models.PlanEnterprise {
		t.Errorf("Plan = %q, want enterprise — EnsureSetupTenant must not inherit CreateTenant's free fallback", tn.Plan)
	}

	ancestors, err := repo.ListAncestors(ctx, tenantUUID)
	if err != nil {
		t.Fatalf("ListAncestors: %v", err)
	}
	if len(ancestors) != 1 || ancestors[0].AncestorUUID != tenantUUID || ancestors[0].Depth != 0 {
		t.Errorf("ancestors = %+v, want exactly one depth-0 self row", ancestors)
	}

	m, err := repo.GetMembership(ctx, ownerUUID, tenantUUID)
	if err != nil {
		t.Fatalf("GetMembership: %v", err)
	}
	if !m.IsOwner {
		t.Error("membership.IsOwner = false, want true")
	}
	if len(m.Roles) != 1 || m.Roles[0] != "org_owner" {
		t.Errorf("membership.Roles = %v, want [org_owner]", m.Roles)
	}

	if got := binder.callCount(); got != 1 {
		t.Fatalf("bindOwner called %d times, want 1", got)
	}
	if call := binder.calls[0]; call.roleName != "org_owner" || call.ownerUUID != ownerUUID || call.tenantUUID != tenantUUID {
		t.Errorf("bindOwner call = %+v, want org_owner/%s/%s", call, ownerUUID, tenantUUID)
	}

	if got := kms.mintCount(tenantUUID); got != 1 {
		t.Errorf("KMS mint count = %d, want 1", got)
	}
}

// TestEnsureSetupTenant_RetryBypassesSingleGateAgainstItself proves the
// reserved tenant is never counted against itself: under `single` mode, a
// second EnsureSetupTenant call with the same args must succeed, not trip
// ErrProvisioningLocked because the row it is about to reconcile already
// occupies the platform's one slot.
func TestEnsureSetupTenant_RetryBypassesSingleGateAgainstItself(t *testing.T) {
	db, cleanup := newSetupTenantTestDB(t)
	defer cleanup()
	repo, svc, kms, _ := newSetupTenantService(db)
	svc.SetProvisioningModeResolver(singleModeResolver)
	ctx := context.Background()

	suffix := randSuffix(t)
	tenantUUID := "setup-" + suffix
	ownerUUID := "owner-" + suffix
	name := "Setup Tenant " + suffix
	slug := "setup-tenant-" + suffix

	if err := svc.EnsureSetupTenant(ctx, tenantUUID, ownerUUID, name, slug); err != nil {
		t.Fatalf("first EnsureSetupTenant: %v", err)
	}
	if err := svc.EnsureSetupTenant(ctx, tenantUUID, ownerUUID, name, slug); err != nil {
		t.Fatalf("retry EnsureSetupTenant under `single` mode = %v, want nil (must not trip ErrProvisioningLocked against itself)", err)
	}

	tenants, err := repo.ListTenantsByKind(ctx, models.TenantKindInternal, false)
	if err != nil {
		t.Fatalf("ListTenantsByKind: %v", err)
	}
	if len(tenants) != 1 {
		t.Fatalf("tenants = %d, want exactly 1 (no duplicate row)", len(tenants))
	}

	ancestors, err := repo.ListAncestors(ctx, tenantUUID)
	if err != nil {
		t.Fatalf("ListAncestors: %v", err)
	}
	if len(ancestors) != 1 {
		t.Fatalf("ancestors = %d, want exactly 1 (no duplicate closure row)", len(ancestors))
	}

	memberships, err := repo.ListMembershipsByTenant(ctx, tenantUUID)
	if err != nil {
		t.Fatalf("ListMembershipsByTenant: %v", err)
	}
	if len(memberships) != 1 {
		t.Fatalf("memberships = %d, want exactly 1 (no duplicate membership)", len(memberships))
	}

	if got := kms.mintCount(tenantUUID); got != 1 {
		t.Errorf("KMS mint count = %d, want 1 (no duplicate key)", got)
	}
}

// TestEnsureSetupTenant_RetryAfterPartialFailure drives a real
// partial-provisioning failure (a bindOwner error on the fresh-create path,
// which makes createTenantWithUUID soft-delete the tenant it just inserted)
// and then a healthy retry, asserting the row is restored and every
// dependent is reconciled without duplication.
func TestEnsureSetupTenant_RetryAfterPartialFailure(t *testing.T) {
	db, cleanup := newSetupTenantTestDB(t)
	defer cleanup()
	repo, svc, kms, binder := newSetupTenantService(db)
	svc.SetProvisioningModeResolver(singleModeResolver)
	binder.failFirst = 1
	ctx := context.Background()

	suffix := randSuffix(t)
	tenantUUID := "setup-" + suffix
	ownerUUID := "owner-" + suffix
	name := "Setup Tenant " + suffix
	slug := "setup-tenant-" + suffix

	if err := svc.EnsureSetupTenant(ctx, tenantUUID, ownerUUID, name, slug); err == nil {
		t.Fatal("first EnsureSetupTenant with a failing bindOwner = nil, want an error")
	}

	soft, err := repo.GetTenantByUUIDIncludingDeleted(ctx, tenantUUID)
	if err != nil {
		t.Fatalf("GetTenantByUUIDIncludingDeleted after partial failure: %v", err)
	}
	if soft.DeletedAt == nil {
		t.Fatal("tenant row not soft-deleted after bindOwner failure — createTenantWithUUID's rollback should have run")
	}

	if err := svc.EnsureSetupTenant(ctx, tenantUUID, ownerUUID, name, slug); err != nil {
		t.Fatalf("retry EnsureSetupTenant with a healthy bindOwner: %v", err)
	}

	restored, err := repo.GetTenantByUUID(ctx, tenantUUID)
	if err != nil {
		t.Fatalf("GetTenantByUUID after restore: %v", err)
	}
	if restored.DeletedAt != nil {
		t.Error("tenant row still soft-deleted after successful retry")
	}
	if restored.Status != models.TenantStatusActive {
		t.Errorf("Status = %q, want active", restored.Status)
	}

	if _, err := repo.GetMembership(ctx, ownerUUID, tenantUUID); err != nil {
		t.Fatalf("GetMembership after restore: %v", err)
	}
	ancestors, err := repo.ListAncestors(ctx, tenantUUID)
	if err != nil {
		t.Fatalf("ListAncestors after restore: %v", err)
	}
	if len(ancestors) != 1 {
		t.Fatalf("ancestors = %d, want exactly 1", len(ancestors))
	}
	if got := kms.mintCount(tenantUUID); got != 1 {
		t.Errorf("KMS mint count = %d, want 1 (no duplicate key across the retry)", got)
	}
	if got := binder.callCount(); got != 2 {
		t.Errorf("bindOwner called %d times, want 2 (failing first call + succeeding retry)", got)
	}
}

// TestEnsureSetupTenant_RestoreBlockedByOccupiedSlot proves the restore
// branch's `single` check is applied against OTHER slot occupants, not the
// row being restored: a soft-deleted reserved row plus a second, unrelated
// active internal tenant must return ErrProvisioningLocked and leave the
// reserved row un-restored.
func TestEnsureSetupTenant_RestoreBlockedByOccupiedSlot(t *testing.T) {
	db, cleanup := newSetupTenantTestDB(t)
	defer cleanup()
	repo, svc, _, _ := newSetupTenantService(db)
	svc.SetProvisioningModeResolver(singleModeResolver)
	ctx := context.Background()

	suffix := randSuffix(t)
	tenantUUID := "setup-" + suffix
	ownerUUID := "owner-" + suffix
	name := "Setup Tenant " + suffix
	slug := "setup-tenant-" + suffix

	deletedAt := time.Now()
	seedRawTenant(t, repo, &models.Tenant{
		UUID:          tenantUUID,
		Kind:          models.TenantKindInternal,
		Status:        models.TenantStatusArchived,
		Name:          name,
		Slug:          slug,
		OwnerUserUUID: ownerUUID,
		Plan:          models.PlanEnterprise,
		DeletedAt:     &deletedAt,
		ArchivedAt:    &deletedAt,
	})

	// A second, unrelated internal tenant occupies the platform's only
	// `single`-mode slot.
	seedRawTenant(t, repo, &models.Tenant{
		UUID:          "occupant-" + suffix,
		Kind:          models.TenantKindInternal,
		Status:        models.TenantStatusActive,
		Name:          "Occupant " + suffix,
		Slug:          "occupant-" + suffix,
		OwnerUserUUID: "occupant-owner-" + suffix,
		Plan:          models.PlanFree,
	})

	err := svc.EnsureSetupTenant(ctx, tenantUUID, ownerUUID, name, slug)
	if !errors.Is(err, ErrProvisioningLocked) {
		t.Fatalf("EnsureSetupTenant = %v, want ErrProvisioningLocked", err)
	}

	still, gerr := repo.GetTenantByUUIDIncludingDeleted(ctx, tenantUUID)
	if gerr != nil {
		t.Fatalf("GetTenantByUUIDIncludingDeleted: %v", gerr)
	}
	if still.DeletedAt == nil {
		t.Error("reserved tenant was resurrected despite the occupied slot")
	}
}

// TestEnsureSetupTenant_PurgedNotResurrected proves an archived-and-purged
// reserved row is never brought back — remediation, not restoration.
func TestEnsureSetupTenant_PurgedNotResurrected(t *testing.T) {
	db, cleanup := newSetupTenantTestDB(t)
	defer cleanup()
	repo, svc, _, _ := newSetupTenantService(db)
	ctx := context.Background()

	suffix := randSuffix(t)
	tenantUUID := "setup-" + suffix
	ownerUUID := "owner-" + suffix
	name := "Setup Tenant " + suffix
	slug := "setup-tenant-" + suffix

	purgedAt := time.Now()
	seedRawTenant(t, repo, &models.Tenant{
		UUID:          tenantUUID,
		Kind:          models.TenantKindInternal,
		Status:        models.TenantStatusPurged,
		Name:          name,
		Slug:          slug,
		OwnerUserUUID: ownerUUID,
		Plan:          models.PlanEnterprise,
		PurgedAt:      &purgedAt,
	})

	err := svc.EnsureSetupTenant(ctx, tenantUUID, ownerUUID, name, slug)
	if !errors.Is(err, ErrSetupTenantRemediation) {
		t.Fatalf("EnsureSetupTenant = %v, want ErrSetupTenantRemediation", err)
	}
}

// TestEnsureSetupTenant_ArchivedNotResurrected is an additional test beyond
// the brief's mandated nine, added because the design spec
// (docs/superpowers/specs/2026-08-23-tier1-default-tenant-setup-design.md)
// is explicit that "archived and purged reserved rows enter remediation
// rather than being resurrected" — two DISTINCT non-resurrectable states,
// separate from the setup-owned soft-delete rollback signature (DeletedAt
// != nil) that IS restored. A row reaching Status == archived via the admin
// ArchiveTenant action never sets DeletedAt, so this state is only reachable
// by a deliberate operator action outside the setup flow, never by the
// seam's own rollback.
func TestEnsureSetupTenant_ArchivedNotResurrected(t *testing.T) {
	db, cleanup := newSetupTenantTestDB(t)
	defer cleanup()
	repo, svc, _, _ := newSetupTenantService(db)
	ctx := context.Background()

	suffix := randSuffix(t)
	tenantUUID := "setup-" + suffix
	ownerUUID := "owner-" + suffix
	name := "Setup Tenant " + suffix
	slug := "setup-tenant-" + suffix

	archivedAt := time.Now()
	seedRawTenant(t, repo, &models.Tenant{
		UUID:          tenantUUID,
		Kind:          models.TenantKindInternal,
		Status:        models.TenantStatusArchived,
		Name:          name,
		Slug:          slug,
		OwnerUserUUID: ownerUUID,
		Plan:          models.PlanEnterprise,
		ArchivedAt:    &archivedAt,
		// DeletedAt deliberately left nil — this is what distinguishes an
		// admin ArchiveTenant row from the seam's own soft-delete rollback.
	})

	err := svc.EnsureSetupTenant(ctx, tenantUUID, ownerUUID, name, slug)
	if !errors.Is(err, ErrSetupTenantRemediation) {
		t.Fatalf("EnsureSetupTenant on an admin-archived reserved row = %v, want ErrSetupTenantRemediation", err)
	}

	still, gerr := repo.GetTenantByUUIDIncludingDeleted(ctx, tenantUUID)
	if gerr != nil {
		t.Fatalf("GetTenantByUUIDIncludingDeleted: %v", gerr)
	}
	if still.Status != models.TenantStatusArchived {
		t.Errorf("Status = %q, want archived — row must not be resurrected", still.Status)
	}
}

// TestEnsureSetupTenant_IdentityMismatch proves a reserved UUID that already
// names a row with a DIFFERENT owner is a conflict, never silently adopted.
func TestEnsureSetupTenant_IdentityMismatch(t *testing.T) {
	db, cleanup := newSetupTenantTestDB(t)
	defer cleanup()
	repo, svc, _, _ := newSetupTenantService(db)
	ctx := context.Background()

	suffix := randSuffix(t)
	tenantUUID := "setup-" + suffix
	name := "Setup Tenant " + suffix
	slug := "setup-tenant-" + suffix

	seedRawTenant(t, repo, &models.Tenant{
		UUID:          tenantUUID,
		Kind:          models.TenantKindInternal,
		Status:        models.TenantStatusActive,
		Name:          name,
		Slug:          slug,
		OwnerUserUUID: "original-owner-" + suffix,
		Plan:          models.PlanEnterprise,
	})

	err := svc.EnsureSetupTenant(ctx, tenantUUID, "different-owner-"+suffix, name, slug)
	if !errors.Is(err, ErrSetupTenantConflict) {
		t.Fatalf("EnsureSetupTenant with mismatched owner = %v, want ErrSetupTenantConflict", err)
	}
}

// TestEnsureSetupTenant_SlugHeldByOtherUUIDStaysConflict proves that when an
// UNRELATED tenant holds the slug, EnsureSetupTenant never adopts it — the
// reread-and-reconcile recovery path only fires when the reserved UUID
// itself resolves to a row; here it stays absent, so the original
// ErrSlugAlreadyInUse must propagate.
func TestEnsureSetupTenant_SlugHeldByOtherUUIDStaysConflict(t *testing.T) {
	db, cleanup := newSetupTenantTestDB(t)
	defer cleanup()
	repo, svc, _, _ := newSetupTenantService(db)
	ctx := context.Background()

	suffix := randSuffix(t)
	slug := "contested-slug-" + suffix
	seedRawTenant(t, repo, &models.Tenant{
		UUID:          "unrelated-" + suffix,
		Kind:          models.TenantKindInternal,
		Status:        models.TenantStatusActive,
		Name:          "Unrelated " + suffix,
		Slug:          slug,
		OwnerUserUUID: "unrelated-owner-" + suffix,
		Plan:          models.PlanFree,
	})

	tenantUUID := "setup-" + suffix
	ownerUUID := "owner-" + suffix
	err := svc.EnsureSetupTenant(ctx, tenantUUID, ownerUUID, "Setup Tenant "+suffix, slug)
	if !errors.Is(err, ErrSlugAlreadyInUse) {
		t.Fatalf("EnsureSetupTenant with a slug held by an unrelated tenant = %v, want ErrSlugAlreadyInUse", err)
	}

	if _, gerr := repo.GetTenantByUUIDIncludingDeleted(ctx, tenantUUID); !errors.Is(gerr, repository.ErrNotFound) {
		t.Errorf("reserved UUID %q must stay absent, got err=%v", tenantUUID, gerr)
	}
}

// TestEnsureSetupTenant_ConcurrentConvergesOnReservedUUID races two
// EnsureSetupTenant calls for the SAME reserved UUID/owner/name/slug across
// a real start barrier and asserts both succeed and converge on exactly one
// tenant row, one closure self-row, one owner membership, and one KMS key —
// regardless of which goroutine wins the tenant-row insert and which one
// loses and reconciles.
func TestEnsureSetupTenant_ConcurrentConvergesOnReservedUUID(t *testing.T) {
	db, cleanup := newSetupTenantTestDB(t)
	defer cleanup()
	repo, svc, kms, _ := newSetupTenantService(db)
	ctx := context.Background()

	suffix := randSuffix(t)
	tenantUUID := "setup-" + suffix
	ownerUUID := "owner-" + suffix
	name := "Setup Tenant " + suffix
	slug := "setup-tenant-" + suffix

	start := make(chan struct{})
	var wg sync.WaitGroup
	results := make([]error, 2)
	wg.Add(2)
	for i := range 2 {
		go func() {
			defer wg.Done()
			<-start
			results[i] = svc.EnsureSetupTenant(ctx, tenantUUID, ownerUUID, name, slug)
		}()
	}
	close(start)
	wg.Wait()

	for i, err := range results {
		if err != nil {
			t.Fatalf("goroutine %d: EnsureSetupTenant = %v, want nil", i, err)
		}
	}

	tenants, err := repo.ListTenantsByKind(ctx, models.TenantKindInternal, false)
	if err != nil {
		t.Fatalf("ListTenantsByKind: %v", err)
	}
	if len(tenants) != 1 {
		t.Fatalf("tenants = %d, want exactly 1", len(tenants))
	}

	ancestors, err := repo.ListAncestors(ctx, tenantUUID)
	if err != nil {
		t.Fatalf("ListAncestors: %v", err)
	}
	if len(ancestors) != 1 {
		t.Fatalf("ancestors = %d, want exactly 1", len(ancestors))
	}

	memberships, err := repo.ListMembershipsByTenant(ctx, tenantUUID)
	if err != nil {
		t.Fatalf("ListMembershipsByTenant: %v", err)
	}
	if len(memberships) != 1 {
		t.Fatalf("memberships = %d, want exactly 1", len(memberships))
	}

	if got := kms.mintCount(tenantUUID); got != 1 {
		t.Errorf("KMS mint count = %d, want 1", got)
	}
}

// TestEnsureSetupTenant_StampsPlanOnLegacyRow proves a reserved row that
// already occupies its slot but was inserted with an empty plan (a legacy
// row, or a row created by something other than EnsureSetupTenant) gets
// stamped to enterprise by reconciliation.
func TestEnsureSetupTenant_StampsPlanOnLegacyRow(t *testing.T) {
	db, cleanup := newSetupTenantTestDB(t)
	defer cleanup()
	repo, svc, _, _ := newSetupTenantService(db)
	ctx := context.Background()

	suffix := randSuffix(t)
	tenantUUID := "setup-" + suffix
	ownerUUID := "owner-" + suffix
	name := "Setup Tenant " + suffix
	slug := "setup-tenant-" + suffix

	seedRawTenant(t, repo, &models.Tenant{
		UUID:          tenantUUID,
		Kind:          models.TenantKindInternal,
		Status:        models.TenantStatusActive,
		Name:          name,
		Slug:          slug,
		OwnerUserUUID: ownerUUID,
		Plan:          "", // legacy row, never stamped
	})

	if err := svc.EnsureSetupTenant(ctx, tenantUUID, ownerUUID, name, slug); err != nil {
		t.Fatalf("EnsureSetupTenant: %v", err)
	}

	tn, err := repo.GetTenantByUUID(ctx, tenantUUID)
	if err != nil {
		t.Fatalf("GetTenantByUUID: %v", err)
	}
	if tn.Plan != models.PlanEnterprise {
		t.Errorf("Plan = %q, want enterprise", tn.Plan)
	}
}
