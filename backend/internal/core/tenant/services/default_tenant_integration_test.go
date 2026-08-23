package services

// These tests exercise the platform default-tenant service layer —
// assign/transfer/read plus the lifecycle guards protecting them — against
// a genuine replica-set MongoDB. RunDefaultGuarded and SetDefault
// (repository/defaults.go) run inside multi-document transactions, which a
// standalone mongod does not support at all. Point MONGO_TEST_URI at the
// replica-set instance, e.g.:
//
//	MONGO_TEST_URI='mongodb://localhost:28017/?directConnection=true' \
//	  go test ./internal/core/tenant/services/... -run TestDefault -v
//
// directConnection=true is mandatory against the CI mongod (replica set
// rs0) — without it the driver's replica-set discovery can resolve to a
// different, unrelated database on this host. newDefaultsTestDB skips the
// whole suite when MONGO_TEST_URI is unset, so a plain `go test ./...` run
// reports these as skipped rather than failing.

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/orkestra/backend/internal/core/tenant/models"
	"github.com/orkestra/backend/internal/core/tenant/repository"
	"github.com/orkestra/backend/internal/shared/errcode"
	"github.com/orkestra/backend/pkg/sdk/iface"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

const defaultsMongoTestURIEnv = "MONGO_TEST_URI"

// newDefaultsTestDB spins up an isolated database for one test. Each test
// gets a unique name (random suffix) so parallel tests don't collide. The
// database is dropped on teardown.
func newDefaultsTestDB(t *testing.T) (*mongo.Database, func()) {
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
	dbName := "orkestra_test_tenant_svc_defaults_" + randSuffix(t)
	db := client.Database(dbName)
	cleanup := func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = db.Drop(ctx)
		_ = client.Disconnect(ctx)
	}
	return db, cleanup
}

func randSuffix(t *testing.T) string {
	t.Helper()
	buf := make([]byte, 6)
	if _, err := rand.Read(buf); err != nil {
		t.Fatalf("rand.Read: %v", err)
	}
	return hex.EncodeToString(buf)
}

// seedDefaultsTenant inserts a Tenant row with reasonable internal/active
// defaults; overrides must be applied via the `with` callback.
func seedDefaultsTenant(t *testing.T, repo *repository.Repository, with func(*models.Tenant)) *models.Tenant {
	t.Helper()
	suffix := randSuffix(t)
	tn := &models.Tenant{
		UUID:          "tenant-" + suffix,
		Kind:          models.TenantKindInternal,
		Status:        models.TenantStatusActive,
		Name:          "Tenant " + suffix,
		Slug:          "slug-" + suffix,
		OwnerUserUUID: "owner-" + suffix,
	}
	if with != nil {
		with(tn)
	}
	if err := repo.CreateTenant(context.Background(), tn); err != nil {
		t.Fatalf("seed tenant: %v", err)
	}
	return tn
}

// recordingAuditSink implements iface.AuditSink, recording every Emit call
// so tests can assert audit emission (action, outcome, metadata, actor)
// without standing up a real compliance module.
type recordingAuditSink struct {
	mu     sync.Mutex
	events []iface.AuditEvent
}

func (f *recordingAuditSink) Emit(_ context.Context, event iface.AuditEvent) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.events = append(f.events, event)
}

func (f *recordingAuditSink) all() []iface.AuditEvent {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]iface.AuditEvent, len(f.events))
	copy(out, f.events)
	return out
}

// lastByAction returns the most recently recorded event with the given
// action, if any.
func (f *recordingAuditSink) lastByAction(action string) (iface.AuditEvent, bool) {
	events := f.all()
	for i := len(events) - 1; i >= 0; i-- {
		if events[i].Action == action {
			return events[i], true
		}
	}
	return iface.AuditEvent{}, false
}

// TestAssignDefaultTenant_IdempotentAndConflict covers brief test case 1:
// assign → DefaultTenantUUID returns it; re-assign the same UUID is a nil-
// error no-op (no duplicate pointer row, no revision bump); assigning a
// DIFFERENT UUID while a default is already set is rejected.
func TestAssignDefaultTenant_IdempotentAndConflict(t *testing.T) {
	db, cleanup := newDefaultsTestDB(t)
	defer cleanup()
	repo := repository.New(db)
	svc := New(repo)
	sink := &recordingAuditSink{}
	svc.SetAuditSink(sink)
	ctx := context.Background()

	t1 := seedDefaultsTenant(t, repo, nil)
	actor := "323e4567-e89b-12d3-a456-426614174000"

	if err := svc.AssignDefaultTenant(ctx, t1.UUID, actor, models.DefaultUpdateSourceSetup); err != nil {
		t.Fatalf("AssignDefaultTenant(t1): %v", err)
	}
	got, err := svc.DefaultTenantUUID(ctx)
	if err != nil {
		t.Fatalf("DefaultTenantUUID: %v", err)
	}
	if got != t1.UUID {
		t.Fatalf("DefaultTenantUUID = %q, want %q", got, t1.UUID)
	}
	ev, ok := sink.lastByAction("tenant.default.assigned")
	if !ok {
		t.Fatalf("no tenant.default.assigned audit event recorded")
	}
	if ev.ActorType != "user" || ev.ActorUserID != actor {
		t.Fatalf("assigned event actor = (%q,%q), want (user,%q)", ev.ActorType, ev.ActorUserID, actor)
	}

	// Re-assign the SAME uuid: nil error, no duplicate row, no revision bump.
	before, err := repo.GetDefault(ctx, models.TenantKindInternal)
	if err != nil {
		t.Fatalf("GetDefault before re-assign: %v", err)
	}
	if err := svc.AssignDefaultTenant(ctx, t1.UUID, actor, models.DefaultUpdateSourceSetup); err != nil {
		t.Fatalf("AssignDefaultTenant(t1, re-assign) = %v, want nil (idempotent)", err)
	}
	//tenantscope:allow system: test asserts the pointer singleton for kind stays a single document after a re-assign no-op
	n, err := db.Collection(repository.CollDefaults).CountDocuments(ctx, bson.M{"kind": string(models.TenantKindInternal)})
	if err != nil {
		t.Fatalf("CountDocuments: %v", err)
	}
	if n != 1 {
		t.Fatalf("tenant_defaults document count = %d, want 1 (no duplicate row)", n)
	}
	after, err := repo.GetDefault(ctx, models.TenantKindInternal)
	if err != nil {
		t.Fatalf("GetDefault after re-assign: %v", err)
	}
	if after.Revision != before.Revision {
		t.Fatalf("Revision changed on idempotent re-assign: before=%d after=%d", before.Revision, after.Revision)
	}

	// Assign a DIFFERENT uuid while t1 is already the default: rejected.
	t2 := seedDefaultsTenant(t, repo, nil)
	err = svc.AssignDefaultTenant(ctx, t2.UUID, actor, models.DefaultUpdateSourceSetup)
	if !errors.Is(err, ErrDefaultAlreadyAssigned) {
		t.Fatalf("AssignDefaultTenant(t2, already assigned to t1) = %v, want ErrDefaultAlreadyAssigned", err)
	}
	got, err = svc.DefaultTenantUUID(ctx)
	if err != nil {
		t.Fatalf("DefaultTenantUUID after rejected assign: %v", err)
	}
	if got != t1.UUID {
		t.Fatalf("DefaultTenantUUID after rejected assign = %q, want unchanged %q", got, t1.UUID)
	}
}

// TestAssignDefaultTenant_MigrationActorIsSystemNoUpdatedByField covers
// brief test case 4: a migration-sourced assignment (empty actorUUID)
// audits with ActorType "system" and an empty ActorUserID, and the pointer
// row's raw document carries NO updatedBy key at all — not an empty
// string.
func TestAssignDefaultTenant_MigrationActorIsSystemNoUpdatedByField(t *testing.T) {
	db, cleanup := newDefaultsTestDB(t)
	defer cleanup()
	repo := repository.New(db)
	svc := New(repo)
	sink := &recordingAuditSink{}
	svc.SetAuditSink(sink)
	ctx := context.Background()

	t1 := seedDefaultsTenant(t, repo, nil)

	if err := svc.AssignDefaultTenant(ctx, t1.UUID, "", models.DefaultUpdateSourceMigration); err != nil {
		t.Fatalf("AssignDefaultTenant(migration): %v", err)
	}

	ev, ok := sink.lastByAction("tenant.default.assigned")
	if !ok {
		t.Fatalf("no tenant.default.assigned audit event recorded")
	}
	if ev.ActorType != "system" {
		t.Fatalf("ActorType = %q, want system", ev.ActorType)
	}
	if ev.ActorUserID != "" {
		t.Fatalf("ActorUserID = %q, want empty", ev.ActorUserID)
	}

	var raw bson.M
	//tenantscope:allow system: test asserts the raw pointer document shape directly (updatedBy must be absent, not empty, for a migration-sourced write)
	if err := db.Collection(repository.CollDefaults).FindOne(ctx, bson.M{"kind": string(models.TenantKindInternal)}).Decode(&raw); err != nil {
		t.Fatalf("decode raw pointer doc: %v", err)
	}
	if v, exists := raw["updatedBy"]; exists {
		t.Fatalf("updatedBy present in raw doc after migration write: %v (want field entirely absent)", v)
	}
}

// TestTransferDefaultTenant_MovesPointerAndAudits covers brief test case 2:
// transferring to an operational internal tenant moves the pointer and
// audits tenant.default.transferred with the previous/new UUIDs and a
// "user" actor.
func TestTransferDefaultTenant_MovesPointerAndAudits(t *testing.T) {
	db, cleanup := newDefaultsTestDB(t)
	defer cleanup()
	repo := repository.New(db)
	svc := New(repo)
	sink := &recordingAuditSink{}
	svc.SetAuditSink(sink)
	ctx := context.Background()

	t1 := seedDefaultsTenant(t, repo, nil)
	t2 := seedDefaultsTenant(t, repo, nil)
	setupActor := "423e4567-e89b-12d3-a456-426614174000"
	adminActor := "523e4567-e89b-12d3-a456-426614174000"

	if err := svc.AssignDefaultTenant(ctx, t1.UUID, setupActor, models.DefaultUpdateSourceSetup); err != nil {
		t.Fatalf("AssignDefaultTenant(t1): %v", err)
	}

	if err := svc.TransferDefaultTenant(ctx, t2.UUID, adminActor); err != nil {
		t.Fatalf("TransferDefaultTenant(t2): %v", err)
	}

	got, err := svc.DefaultTenantUUID(ctx)
	if err != nil {
		t.Fatalf("DefaultTenantUUID: %v", err)
	}
	if got != t2.UUID {
		t.Fatalf("DefaultTenantUUID after transfer = %q, want %q", got, t2.UUID)
	}

	ev, ok := sink.lastByAction("tenant.default.transferred")
	if !ok {
		t.Fatalf("no tenant.default.transferred audit event recorded")
	}
	if ev.ActorType != "user" || ev.ActorUserID != adminActor {
		t.Fatalf("transferred event actor = (%q,%q), want (user,%q)", ev.ActorType, ev.ActorUserID, adminActor)
	}
	if ev.Outcome == "denied" {
		t.Fatalf("transferred event Outcome = denied, want success (empty)")
	}
	if got := ev.Metadata["previousTenantUUID"]; got != t1.UUID {
		t.Fatalf("Metadata[previousTenantUUID] = %v, want %q", got, t1.UUID)
	}
	if got := ev.Metadata["newTenantUUID"]; got != t2.UUID {
		t.Fatalf("Metadata[newTenantUUID] = %v, want %q", got, t2.UUID)
	}
}

// TestTransferDefaultTenant_DeniedTargetNotOperational_NoStateChange adds
// the denied-transfer test the brief calls for: a transfer targeting a
// non-operational internal tenant is rejected, the pointer is left
// unchanged, and a denied tenant.default.transferred audit event fires.
func TestTransferDefaultTenant_DeniedTargetNotOperational_NoStateChange(t *testing.T) {
	db, cleanup := newDefaultsTestDB(t)
	defer cleanup()
	repo := repository.New(db)
	svc := New(repo)
	sink := &recordingAuditSink{}
	svc.SetAuditSink(sink)
	ctx := context.Background()

	t1 := seedDefaultsTenant(t, repo, nil)
	t2 := seedDefaultsTenant(t, repo, func(tn *models.Tenant) {
		tn.Status = models.TenantStatusSuspended
	})
	actor := "623e4567-e89b-12d3-a456-426614174000"

	if err := svc.AssignDefaultTenant(ctx, t1.UUID, actor, models.DefaultUpdateSourceSetup); err != nil {
		t.Fatalf("AssignDefaultTenant(t1): %v", err)
	}

	err := svc.TransferDefaultTenant(ctx, t2.UUID, actor)
	if !errors.Is(err, repository.ErrDefaultTargetNotOperational) {
		t.Fatalf("TransferDefaultTenant(non-operational) = %v, want ErrDefaultTargetNotOperational", err)
	}

	got, gerr := svc.DefaultTenantUUID(ctx)
	if gerr != nil {
		t.Fatalf("DefaultTenantUUID: %v", gerr)
	}
	if got != t1.UUID {
		t.Fatalf("DefaultTenantUUID after denied transfer = %q, want unchanged %q", got, t1.UUID)
	}

	ev, ok := sink.lastByAction("tenant.default.transferred")
	if !ok {
		t.Fatalf("no tenant.default.transferred audit event recorded for denied transfer")
	}
	if ev.Outcome != "denied" {
		t.Fatalf("Outcome = %q, want denied", ev.Outcome)
	}
}

// TestLifecycleGuards_BlockDefaultTarget covers brief test case 3:
// SuspendTenant/ArchiveTenant/DeleteTenant/PurgeTenant against the current
// default fail with ErrDefaultReassignmentRequired and leave the tenant row
// genuinely unchanged (plus emit a denied audit event with the stable error
// code); against a non-default tenant, each succeeds exactly as before.
func TestLifecycleGuards_BlockDefaultTarget(t *testing.T) {
	cases := []struct {
		name       string
		action     string
		call       func(s *Service, ctx context.Context, uuid string) error
		wantStatus models.TenantStatus
		wantDelete bool
	}{
		{
			name:       "suspend",
			action:     "tenant.lifecycle.suspended",
			call:       func(s *Service, ctx context.Context, uuid string) error { return s.SuspendTenant(ctx, uuid) },
			wantStatus: models.TenantStatusSuspended,
		},
		{
			name:       "archive",
			action:     "tenant.lifecycle.archived",
			call:       func(s *Service, ctx context.Context, uuid string) error { return s.ArchiveTenant(ctx, uuid) },
			wantStatus: models.TenantStatusArchived,
		},
		{
			name:       "delete",
			action:     "tenant.deleted",
			call:       func(s *Service, ctx context.Context, uuid string) error { return s.DeleteTenant(ctx, uuid) },
			wantStatus: models.TenantStatusArchived,
			wantDelete: true,
		},
		{
			name:       "purge",
			action:     "tenant.lifecycle.purged",
			call:       func(s *Service, ctx context.Context, uuid string) error { return s.PurgeTenant(ctx, uuid) },
			wantStatus: models.TenantStatusPurged,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			db, cleanup := newDefaultsTestDB(t)
			defer cleanup()
			repo := repository.New(db)
			svc := New(repo)
			sink := &recordingAuditSink{}
			svc.SetAuditSink(sink)
			ctx := context.Background()

			defaultTenant := seedDefaultsTenant(t, repo, nil)
			otherTenant := seedDefaultsTenant(t, repo, nil)

			if err := svc.AssignDefaultTenant(ctx, defaultTenant.UUID, "", models.DefaultUpdateSourceSetup); err != nil {
				t.Fatalf("AssignDefaultTenant: %v", err)
			}

			// Against the default: rejected, row genuinely unchanged.
			err := tc.call(svc, ctx, defaultTenant.UUID)
			if !errors.Is(err, ErrDefaultReassignmentRequired) {
				t.Fatalf("%s(default) = %v, want ErrDefaultReassignmentRequired", tc.name, err)
			}
			row, gerr := repo.GetTenantByUUIDIncludingDeleted(ctx, defaultTenant.UUID)
			if gerr != nil {
				t.Fatalf("GetTenantByUUIDIncludingDeleted(default): %v", gerr)
			}
			if row.Status != models.TenantStatusActive {
				t.Fatalf("default tenant Status = %q after denied %s, want unchanged active", row.Status, tc.name)
			}
			if row.DeletedAt != nil {
				t.Fatalf("default tenant DeletedAt set after denied %s, want nil", tc.name)
			}
			ev, ok := sink.lastByAction(tc.action)
			if !ok {
				t.Fatalf("no %s audit event recorded for denied %s", tc.action, tc.name)
			}
			if ev.Outcome != "denied" {
				t.Fatalf("denied %s event Outcome = %q, want denied", tc.name, ev.Outcome)
			}
			if code, _ := ev.Metadata["code"].(string); code != errcode.TenantDefaultReassignmentRequired {
				t.Fatalf("denied %s event Metadata[code] = %v, want %q", tc.name, ev.Metadata["code"], errcode.TenantDefaultReassignmentRequired)
			}

			// Confirm the pointer itself never moved or lost its revision
			// integrity because of the denial.
			got, derr := svc.DefaultTenantUUID(ctx)
			if derr != nil {
				t.Fatalf("DefaultTenantUUID: %v", derr)
			}
			if got != defaultTenant.UUID {
				t.Fatalf("DefaultTenantUUID after denied %s = %q, want unchanged %q", tc.name, got, defaultTenant.UUID)
			}

			// Against a non-default tenant: succeeds exactly as before.
			if err := tc.call(svc, ctx, otherTenant.UUID); err != nil {
				t.Fatalf("%s(non-default) = %v, want nil", tc.name, err)
			}
			row, gerr = repo.GetTenantByUUIDIncludingDeleted(ctx, otherTenant.UUID)
			if gerr != nil {
				t.Fatalf("GetTenantByUUIDIncludingDeleted(non-default): %v", gerr)
			}
			if row.Status != tc.wantStatus {
				t.Fatalf("non-default tenant Status = %q after %s, want %q", row.Status, tc.name, tc.wantStatus)
			}
			if tc.wantDelete && row.DeletedAt == nil {
				t.Fatalf("non-default tenant DeletedAt not set after %s", tc.name)
			}
		})
	}
}

// TestGetDefaultTenant_Semantics covers the provider-interface contract
// spelled out by the task brief: (nil, nil) — never an error — both when
// no default is assigned and when the pointer names a tenant that is no
// longer operational; the operational default is otherwise returned.
func TestGetDefaultTenant_Semantics(t *testing.T) {
	db, cleanup := newDefaultsTestDB(t)
	defer cleanup()
	repo := repository.New(db)
	svc := New(repo)
	ctx := context.Background()

	// No pointer assigned at all.
	tn, err := svc.GetDefaultTenant(ctx)
	if err != nil {
		t.Fatalf("GetDefaultTenant(unassigned) error = %v, want nil", err)
	}
	if tn != nil {
		t.Fatalf("GetDefaultTenant(unassigned) = %+v, want nil", tn)
	}

	t1 := seedDefaultsTenant(t, repo, nil)
	if err := svc.AssignDefaultTenant(ctx, t1.UUID, "", models.DefaultUpdateSourceSetup); err != nil {
		t.Fatalf("AssignDefaultTenant: %v", err)
	}
	tn, err = svc.GetDefaultTenant(ctx)
	if err != nil {
		t.Fatalf("GetDefaultTenant(operational) error = %v, want nil", err)
	}
	if tn == nil || tn.UUID != t1.UUID {
		t.Fatalf("GetDefaultTenant(operational) = %+v, want UUID %q", tn, t1.UUID)
	}

	// Simulate a stale pointer: flip the underlying tenant status directly
	// (bypassing the service-level guard, the way an out-of-band data fix
	// or a legacy row could) and confirm the provider still refuses to hand
	// out a non-operational target.
	if err := repo.UpdateTenantStatus(ctx, t1.UUID, models.TenantStatusSuspended); err != nil {
		t.Fatalf("UpdateTenantStatus: %v", err)
	}
	tn, err = svc.GetDefaultTenant(ctx)
	if err != nil {
		t.Fatalf("GetDefaultTenant(stale pointer) error = %v, want nil", err)
	}
	if tn != nil {
		t.Fatalf("GetDefaultTenant(stale pointer) = %+v, want nil (non-operational target never returned)", tn)
	}
}
