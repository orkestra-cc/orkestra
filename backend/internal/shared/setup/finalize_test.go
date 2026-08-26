package setup

// Task 5.5: POST /v1/setup/finalize — the resumable finalization saga.
//
// These tests drive Service.Finalize through every row of the design's
// request state table (see the "Resumable saga" section of
// docs/superpowers/specs/2026-08-23-tier1-default-tenant-setup-design.md)
// plus the saga specifics: stage ordering, at-least-once replay safety,
// the recovery CAS, the two audit shapes, and the typed 202 body that must
// stay OUTSIDE the Problem Details envelope.
//
// The store fake reproduces *systeminit.Repo's CAS semantics exactly —
// every mutator matches on the same filter fields the Mongo update uses
// and answers (false, nil) on a miss, which is a lost race and not an
// error. finalize_integration_test.go runs the same saga against the real
// Mongo-backed store.

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humachi"
	"github.com/go-chi/chi/v5"

	authModels "github.com/orkestra/backend/internal/core/auth/models"
	"github.com/orkestra/backend/internal/shared/config"
	"github.com/orkestra/backend/internal/shared/errcode"
	"github.com/orkestra/backend/internal/shared/systeminit"
	"github.com/orkestra/backend/internal/testkit"
	"github.com/orkestra/backend/pkg/sdk/iface"
	"github.com/orkestra/backend/pkg/sdk/module"
)

// --- shared ordered call log -------------------------------------------

// callLog is the single ordered transcript every fake in this file writes
// to. Stage ordering ("config, then tenant, then default, then finish")
// is only meaningful as one interleaved sequence, so the store, the tenant
// seam and the config writer all append to the same log rather than each
// keeping a private list.
type callLog struct {
	mu      sync.Mutex
	entries []string
}

func (c *callLog) add(entry string) {
	if c == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.entries = append(c.entries, entry)
}

func (c *callLog) snapshot() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]string(nil), c.entries...)
}

// only returns the transcript filtered to the given prefixes, so a test
// can assert the effect order without pinning every coordinator read.
func (c *callLog) only(prefixes ...string) []string {
	var out []string
	for _, e := range c.snapshot() {
		for _, p := range prefixes {
			if strings.HasPrefix(e, p) {
				out = append(out, e)
				break
			}
		}
	}
	return out
}

func (c *callLog) count(entry string) int {
	n := 0
	for _, e := range c.snapshot() {
		if e == entry {
			n++
		}
	}
	return n
}

// --- store fake --------------------------------------------------------

type sagaStore struct {
	mu  sync.Mutex
	log *callLog

	rec *systeminit.FinalizationRecord

	// frozen clock; the zero value means "use the wall clock".
	now time.Time

	getErr     error
	initErr    error
	reserveErr error
	claimErr   error

	// hookReserve runs at the start of ReserveRequest, holding the store
	// lock, so a test can simulate another actor changing the record
	// between the authorization check and the reservation CAS.
	hookReserve func(rec *systeminit.FinalizationRecord)
}

func (s *sagaStore) clock() time.Time {
	if s.now.IsZero() {
		return time.Now().UTC()
	}
	return s.now
}

func (s *sagaStore) Get(context.Context) (*systeminit.FinalizationRecord, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.log.add("store.Get")
	if s.getErr != nil {
		return nil, s.getErr
	}
	if s.rec == nil {
		return nil, nil
	}
	cp := *s.rec
	return &cp, nil
}

func (s *sagaStore) InitializeFresh(_ context.Context, adminUUID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.log.add("store.InitializeFresh:" + adminUUID)
	if s.initErr != nil {
		return s.initErr
	}
	if s.rec != nil {
		return nil // $setOnInsert: never clobbers
	}
	now := s.clock()
	s.rec = &systeminit.FinalizationRecord{
		AdminUUID: adminUUID, Source: systeminit.SourceFresh,
		Stage: systeminit.StageConfig, Revision: 1,
		CreatedAt: now, UpdatedAt: now,
	}
	return nil
}

func (s *sagaStore) EnsureRecord(_ context.Context, source string, completed *systeminit.FinalizationResult) (*systeminit.FinalizationRecord, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.log.add("store.EnsureRecord:" + source)
	if s.rec == nil {
		now := s.clock()
		s.rec = &systeminit.FinalizationRecord{
			Source: source, Stage: systeminit.StageConfig, Revision: 1,
			CreatedAt: now, UpdatedAt: now,
		}
		if completed != nil {
			s.rec.Stage = systeminit.StageDone
			s.rec.CompletedAt = &now
			s.rec.Result = completed
		}
	}
	cp := *s.rec
	return &cp, nil
}

func (s *sagaStore) ReserveRequest(_ context.Context, adminUUID, tenantUUID, name, slug, mode, requestHash string) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.log.add("store.ReserveRequest")
	if s.hookReserve != nil && s.rec != nil {
		s.hookReserve(s.rec)
	}
	if s.reserveErr != nil {
		return false, s.reserveErr
	}
	if s.rec == nil || s.rec.AdminUUID != adminUUID || s.rec.RequestHash != "" || s.rec.CompletedAt != nil {
		return false, nil
	}
	s.rec.TenantUUID = tenantUUID
	s.rec.TenantName = name
	s.rec.TenantSlug = slug
	s.rec.Mode = mode
	s.rec.RequestHash = requestHash
	s.rec.Stage = systeminit.StageConfig
	s.rec.Revision++
	s.rec.UpdatedAt = s.clock()
	return true, nil
}

func (s *sagaStore) ClaimStage(_ context.Context, requestHash string, stage int, revision int64, owner string, leaseUntil time.Time) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.log.add("store.ClaimStage")
	if s.claimErr != nil {
		return false, s.claimErr
	}
	if s.rec == nil || s.rec.RequestHash != requestHash || s.rec.Stage != stage || s.rec.Revision != revision {
		return false, nil
	}
	if s.rec.LeaseUntil != nil && s.rec.LeaseUntil.After(s.clock()) {
		return false, nil // a live lease is held by somebody else
	}
	s.rec.LeaseOwner = owner
	until := leaseUntil
	s.rec.LeaseUntil = &until
	s.rec.UpdatedAt = s.clock()
	return true, nil
}

func (s *sagaStore) RenewLease(_ context.Context, owner string, leaseUntil time.Time) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.log.add("store.RenewLease")
	if s.rec == nil || s.rec.LeaseOwner != owner {
		return false, nil
	}
	until := leaseUntil
	s.rec.LeaseUntil = &until
	s.rec.UpdatedAt = s.clock()
	return true, nil
}

func (s *sagaStore) AdvanceStage(_ context.Context, owner string, stage int, revision int64) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.log.add("store.AdvanceStage")
	if s.rec == nil || s.rec.LeaseOwner != owner || s.rec.Stage != stage || s.rec.Revision != revision {
		return false, nil
	}
	s.rec.Stage = stage + 1
	s.rec.Revision++
	s.rec.LeaseOwner = ""
	s.rec.LeaseUntil = nil
	s.rec.UpdatedAt = s.clock()
	return true, nil
}

func (s *sagaStore) Complete(_ context.Context, owner string, revision int64, result systeminit.FinalizationResult) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.log.add("store.Complete")
	if s.rec == nil || s.rec.LeaseOwner != owner || s.rec.Stage != systeminit.StageFinish || s.rec.Revision != revision {
		return false, nil
	}
	now := s.clock()
	s.rec.Stage = systeminit.StageDone
	s.rec.Revision++
	s.rec.LeaseOwner = ""
	s.rec.LeaseUntil = nil
	s.rec.CompletedAt = &now
	snapshot := result
	s.rec.Result = &snapshot
	s.rec.UpdatedAt = now
	return true, nil
}

func (s *sagaStore) ClaimRecovery(_ context.Context, observedAdminUUID string, revision int64, newAdminUUID string) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.log.add("store.ClaimRecovery:" + observedAdminUUID + "->" + newAdminUUID)
	if s.rec == nil || s.rec.Revision != revision || s.rec.AdminUUID != observedAdminUUID {
		return false, nil
	}
	s.rec.AdminUUID = newAdminUUID
	s.rec.Revision++
	s.rec.UpdatedAt = s.clock()
	return true, nil
}

func (s *sagaStore) ClaimReconcileLease(context.Context, int, string, time.Time) (bool, error) {
	s.log.add("store.ClaimReconcileLease")
	return false, nil
}

func (s *sagaStore) FinishReconcile(context.Context, int, string) (bool, error) {
	s.log.add("store.FinishReconcile")
	return false, nil
}

var _ systeminit.FinalizationStore = (*sagaStore)(nil)

// snapshotRecord returns a copy of the fake's current record for assertions.
func (s *sagaStore) snapshotRecord() *systeminit.FinalizationRecord {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.rec == nil {
		return nil
	}
	cp := *s.rec
	return &cp
}

// --- tenant seam fake --------------------------------------------------

type fakeTenants struct {
	log *callLog

	ensureErr error
	assignErr error

	mu             sync.Mutex
	ensureArgs     []ensureCall
	assignArgs     []assignCall
	ensureAttested []bool
}

type ensureCall struct{ tenantUUID, ownerUUID, name, slug string }
type assignCall struct{ tenantUUID, actorUUID, source string }

func (f *fakeTenants) EnsureSetupTenant(_ context.Context, tenantUUID, ownerUUID, name, slug string, coordinatorAttested bool) error {
	f.log.add("tenants.EnsureSetupTenant")
	f.mu.Lock()
	f.ensureArgs = append(f.ensureArgs, ensureCall{tenantUUID, ownerUUID, name, slug})
	f.ensureAttested = append(f.ensureAttested, coordinatorAttested)
	f.mu.Unlock()
	return f.ensureErr
}

func (f *fakeTenants) AssignDefaultTenant(_ context.Context, tenantUUID, actorUUID, source string) error {
	f.log.add("tenants.AssignDefaultTenant")
	f.mu.Lock()
	f.assignArgs = append(f.assignArgs, assignCall{tenantUUID, actorUUID, source})
	f.mu.Unlock()
	return f.assignErr
}

var _ SetupTenantEnsurer = (*fakeTenants)(nil)

// --- config writer fake ------------------------------------------------

type fakeModuleConfig struct {
	log *callLog

	updateErr error

	mu      sync.Mutex
	updates []configUpdate
}

type configUpdate struct {
	module string
	values map[string]string
}

func (f *fakeModuleConfig) GetConfig(context.Context, string) (*module.ModuleConfig, error) {
	return nil, errors.New("fakeModuleConfig: GetConfig is not used by these tests")
}

func (f *fakeModuleConfig) UpdateConfig(_ context.Context, name string, values map[string]string, _ map[string]string) error {
	f.log.add("config.UpdateConfig")
	f.mu.Lock()
	copied := map[string]string{}
	for k, v := range values {
		copied[k] = v
	}
	f.updates = append(f.updates, configUpdate{module: name, values: copied})
	f.mu.Unlock()
	return f.updateErr
}

// --- audit fake --------------------------------------------------------

type fakeAudit struct {
	mu     sync.Mutex
	events []iface.AuditEvent
}

func (f *fakeAudit) Emit(_ context.Context, ev iface.AuditEvent) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.events = append(f.events, ev)
}

func (f *fakeAudit) byAction(action string) []iface.AuditEvent {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []iface.AuditEvent
	for _, ev := range f.events {
		if ev.Action == action {
			out = append(out, ev)
		}
	}
	return out
}

var _ iface.AuditSink = (*fakeAudit)(nil)

// --- fixture -----------------------------------------------------------

type sagaFixture struct {
	svc     *Service
	store   *sagaStore
	tenants *fakeTenants
	cfg     *fakeModuleConfig
	audit   *fakeAudit
	users   *fakeLifecycleUsers
	log     *callLog
}

func newSagaFixture(rec *systeminit.FinalizationRecord, states map[string]iface.UserLifecycleState) sagaFixture {
	log := &callLog{}
	store := &sagaStore{log: log, rec: rec}
	tenants := &fakeTenants{log: log}
	cfg := &fakeModuleConfig{log: log}
	audit := &fakeAudit{}
	users := &fakeLifecycleUsers{count: 1, states: states}
	svc := NewService(users, &stubAdmin{}, store, cfg, tenants, audit, discardLogger())
	return sagaFixture{svc: svc, store: store, tenants: tenants, cfg: cfg, audit: audit, users: users, log: log}
}

// testInput is the canonical un-normalized payload: surrounding and inner
// whitespace on the name, mixed case + padding on the slug. Normalization
// collapses it to ("Acme Corp", "acme").
func testInput(allow bool) FinalizeInput {
	return FinalizeInput{
		TenantName:                     "  Acme   Corp ",
		TenantSlug:                     "  ACME  ",
		AllowAdditionalInternalTenants: allow,
	}
}

func testHash(allow bool) string {
	in := testInput(allow)
	_, _, _, hash := normalizeFinalize(in.TenantName, in.TenantSlug, in.AllowAdditionalInternalTenants)
	return hash
}

func ptrTime(t time.Time) *time.Time { return &t }

// --- initial-admin coordinator binding ---------------------------------

func adminTokens(userID string) *authModels.TokenResponse {
	return &authModels.TokenResponse{
		AccessToken: "access-xyz",
		TokenType:   "Bearer",
		User:        &iface.UserManagementResponse{ID: userID, Email: "root@example.com"},
	}
}

// TestCreateInitialAdmin_BindsCoordinatorFromTokenUserID pins where the
// bound administrator UUID comes from: the token response's user identity
// (UserManagementResponse exposes the UUID as `id`), never a read-back of
// the first_admin sentinel — those two records have deliberately separate
// lifecycles.
func TestCreateInitialAdmin_BindsCoordinatorFromTokenUserID(t *testing.T) {
	store := &sagaStore{log: &callLog{}}
	svc := NewService(&stubUsers{count: 0}, &stubAdmin{resp: adminTokens("user-uuid-1")}, store, nil, nil, nil, discardLogger())

	if _, err := svc.CreateInitialAdmin(context.Background(), "root@example.com", "verysecretpw!", "Root Admin", "10.0.0.1"); err != nil {
		t.Fatalf("CreateInitialAdmin: %v", err)
	}
	rec := store.snapshotRecord()
	if rec == nil {
		t.Fatal("no coordinator record was created")
	}
	if rec.AdminUUID != "user-uuid-1" {
		t.Errorf("adminUUID = %q, want the token response's user id", rec.AdminUUID)
	}
	if rec.Source != systeminit.SourceFresh {
		t.Errorf("source = %q, want %q", rec.Source, systeminit.SourceFresh)
	}
}

// TestCreateInitialAdmin_DoesNotClobberExistingCoordinator proves the
// insert-only semantics: a legacy or recovery record that already exists
// keeps its binding.
func TestCreateInitialAdmin_DoesNotClobberExistingCoordinator(t *testing.T) {
	store := &sagaStore{
		log: &callLog{},
		rec: &systeminit.FinalizationRecord{
			AdminUUID: "already-recovered", Source: systeminit.SourceLegacyRecovery,
			Stage: systeminit.StageConfig, Revision: 5,
		},
	}
	svc := NewService(&stubUsers{count: 0}, &stubAdmin{resp: adminTokens("user-uuid-1")}, store, nil, nil, nil, discardLogger())

	if _, err := svc.CreateInitialAdmin(context.Background(), "root@example.com", "verysecretpw!", "Root Admin", "10.0.0.1"); err != nil {
		t.Fatalf("CreateInitialAdmin: %v", err)
	}
	if rec := store.snapshotRecord(); rec.AdminUUID != "already-recovered" || rec.Revision != 5 {
		t.Errorf("existing coordinator was clobbered: %+v", rec)
	}
}

func TestCreateInitialAdmin_CoordinatorBindFailure_IsReported(t *testing.T) {
	store := &sagaStore{log: &callLog{}, initErr: errors.New("mongo down")}
	svc := NewService(&stubUsers{count: 0}, &stubAdmin{resp: adminTokens("user-uuid-1")}, store, nil, nil, nil, discardLogger())

	if _, err := svc.CreateInitialAdmin(context.Background(), "root@example.com", "verysecretpw!", "Root Admin", "10.0.0.1"); err == nil {
		t.Fatal("a coordinator bind failure must not be swallowed")
	}
}

// --- normalization + mode mapping --------------------------------------

func TestNormalizeFinalize_CollapsesAndMapsMode(t *testing.T) {
	name, slug, mode, hash := normalizeFinalize("  Acme   Corp ", "  ACME  ", true)
	if name != "Acme Corp" {
		t.Errorf("name = %q, want %q", name, "Acme Corp")
	}
	if slug != "acme" {
		t.Errorf("slug = %q, want %q", slug, "acme")
	}
	if mode != modeManual {
		t.Errorf("mode = %q, want %q (allow=true)", mode, modeManual)
	}
	if len(hash) != 64 {
		t.Errorf("hash = %q, want a 64-char hex sha256", hash)
	}

	_, _, singleMode, singleHash := normalizeFinalize("Acme Corp", "acme", false)
	if singleMode != modeSingle {
		t.Errorf("mode = %q, want %q (allow=false)", singleMode, modeSingle)
	}
	if singleHash == hash {
		t.Error("the mode must take part in the request hash: manual and single hashed equal")
	}

	// The hash is over the NORMALIZED tuple: a payload that differs only
	// in whitespace/case is the same request and must replay, not conflict.
	if _, _, _, again := normalizeFinalize("Acme Corp", "acme", true); again != hash {
		t.Error("normalized-equal payloads produced different hashes")
	}
}

func TestNormalizeFinalize_ModeConstantsPinnedToTenantModule(t *testing.T) {
	// shared/setup imports no tenant package, so these two constants are
	// pinned by value. If the tenant module ever renames its provisioning
	// modes, this test is the tripwire (the wire values are also the
	// module-config values the validator accepts).
	if modeManual != "manual" || modeSingle != "single" {
		t.Fatalf("mode constants drifted: manual=%q single=%q", modeManual, modeSingle)
	}
}

// --- state table row 1: nothing reserved -> reserve + run the saga ------

func TestFinalize_FreshReservation_RunsAllStagesInOrder(t *testing.T) {
	fx := newSagaFixture(
		&systeminit.FinalizationRecord{AdminUUID: "admin-1", Source: systeminit.SourceFresh, Stage: systeminit.StageConfig, Revision: 1},
		map[string]iface.UserLifecycleState{"admin-1": iface.UserLifecycleActive},
	)

	res, err := fx.svc.Finalize(context.Background(), "admin-1", "super_admin", testInput(true))
	if err != nil {
		t.Fatalf("Finalize: %v", err)
	}

	if res.TenantName != "Acme Corp" || res.TenantSlug != "acme" || res.Mode != modeManual {
		t.Errorf("result = %+v, want normalized name/slug and manual mode", res)
	}
	if res.TenantUUID == "" {
		t.Error("result carries no tenant UUID")
	}

	// Effects ran exactly once, in saga order.
	want := []string{"config.UpdateConfig", "tenants.EnsureSetupTenant", "tenants.AssignDefaultTenant"}
	got := fx.log.only("config.", "tenants.")
	if len(got) != len(want) {
		t.Fatalf("effect order = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("effect order = %v, want %v", got, want)
		}
	}

	// The reserved UUID is pre-minted by the service and is what every
	// stage acts on.
	rec := fx.store.snapshotRecord()
	if rec.TenantUUID != res.TenantUUID {
		t.Errorf("record tenant UUID %q != result %q", rec.TenantUUID, res.TenantUUID)
	}
	if rec.CompletedAt == nil || rec.Stage != systeminit.StageDone {
		t.Errorf("record not completed: stage=%d completedAt=%v", rec.Stage, rec.CompletedAt)
	}
	if rec.Result == nil || rec.Result.TenantUUID != res.TenantUUID || rec.Result.Mode != modeManual {
		t.Errorf("persisted result snapshot = %+v", rec.Result)
	}
	if rec.LeaseOwner != "" || rec.LeaseUntil != nil {
		t.Errorf("lease not released on completion: owner=%q until=%v", rec.LeaseOwner, rec.LeaseUntil)
	}

	// Stage arguments.
	if len(fx.cfg.updates) != 1 ||
		fx.cfg.updates[0].module != "tenant" ||
		fx.cfg.updates[0].values["provisioning.internal.mode"] != modeManual {
		t.Errorf("config update = %+v, want tenant/provisioning.internal.mode=manual", fx.cfg.updates)
	}
	if len(fx.tenants.ensureArgs) != 1 {
		t.Fatalf("EnsureSetupTenant calls = %d, want 1", len(fx.tenants.ensureArgs))
	}
	ec := fx.tenants.ensureArgs[0]
	if ec.tenantUUID != res.TenantUUID || ec.ownerUUID != "admin-1" || ec.name != "Acme Corp" || ec.slug != "acme" {
		t.Errorf("EnsureSetupTenant args = %+v", ec)
	}
	if !fx.tenants.ensureAttested[0] {
		t.Error("EnsureSetupTenant must receive a truthful coordinator attestation from the in-flight saga")
	}
	if len(fx.tenants.assignArgs) != 1 {
		t.Fatalf("AssignDefaultTenant calls = %d, want 1", len(fx.tenants.assignArgs))
	}
	ac := fx.tenants.assignArgs[0]
	if ac.tenantUUID != res.TenantUUID || ac.actorUUID != "admin-1" || ac.source != "setup" {
		t.Errorf("AssignDefaultTenant args = %+v", ac)
	}

	// Audit: exactly one setup.completed with the minimal metadata.
	events := fx.audit.byAction("setup.completed")
	if len(events) != 1 {
		t.Fatalf("setup.completed events = %d, want 1", len(events))
	}
	ev := events[0]
	if ev.ActorUserID != "admin-1" {
		t.Errorf("audit ActorUserID = %q, want the bound administrator", ev.ActorUserID)
	}
	if ev.Metadata["tenantUUID"] != res.TenantUUID || ev.Metadata["mode"] != modeManual {
		t.Errorf("audit metadata = %v", ev.Metadata)
	}
}

func TestFinalize_SingleMode_PersistsSingle(t *testing.T) {
	fx := newSagaFixture(
		&systeminit.FinalizationRecord{AdminUUID: "admin-1", Stage: systeminit.StageConfig, Revision: 1},
		map[string]iface.UserLifecycleState{"admin-1": iface.UserLifecycleActive},
	)

	res, err := fx.svc.Finalize(context.Background(), "admin-1", "super_admin", testInput(false))
	if err != nil {
		t.Fatalf("Finalize: %v", err)
	}
	if res.Mode != modeSingle {
		t.Errorf("mode = %q, want %q", res.Mode, modeSingle)
	}
	if fx.cfg.updates[0].values["provisioning.internal.mode"] != modeSingle {
		t.Errorf("persisted mode = %q, want single", fx.cfg.updates[0].values["provisioning.internal.mode"])
	}
}

// --- state table row 2: matching hash + live lease -> 202 --------------

func TestFinalize_MatchingHashLiveLease_ReturnsInProgress(t *testing.T) {
	now := time.Now().UTC()
	fx := newSagaFixture(
		&systeminit.FinalizationRecord{
			AdminUUID: "admin-1", Stage: systeminit.StageTenant, Revision: 4,
			RequestHash: testHash(true), TenantUUID: "tenant-1", TenantName: "Acme Corp",
			TenantSlug: "acme", Mode: modeManual,
			LeaseOwner: "another-executor", LeaseUntil: ptrTime(now.Add(30 * time.Second)),
		},
		map[string]iface.UserLifecycleState{"admin-1": iface.UserLifecycleActive},
	)

	_, err := fx.svc.Finalize(context.Background(), "admin-1", "super_admin", testInput(true))
	if !errors.Is(err, ErrFinalizationInProgress) {
		t.Fatalf("err = %v, want ErrFinalizationInProgress", err)
	}
	if effects := fx.log.only("config.", "tenants."); len(effects) != 0 {
		t.Errorf("a 202 loser must execute no stage; ran %v", effects)
	}
	rec := fx.store.snapshotRecord()
	if rec.Stage != systeminit.StageTenant || rec.Revision != 4 || rec.LeaseOwner != "another-executor" {
		t.Errorf("loser mutated the record: %+v", rec)
	}
}

// --- state table row 3: matching hash + expired lease -> resume --------

func TestFinalize_MatchingHashExpiredLease_ResumesFromRecordStage(t *testing.T) {
	now := time.Now().UTC()
	fx := newSagaFixture(
		&systeminit.FinalizationRecord{
			AdminUUID: "admin-1", Stage: systeminit.StageDefault, Revision: 7,
			RequestHash: testHash(true), TenantUUID: "tenant-1", TenantName: "Acme Corp",
			TenantSlug: "acme", Mode: modeManual,
			LeaseOwner: "crashed-executor", LeaseUntil: ptrTime(now.Add(-time.Minute)),
		},
		map[string]iface.UserLifecycleState{"admin-1": iface.UserLifecycleActive},
	)

	res, err := fx.svc.Finalize(context.Background(), "admin-1", "super_admin", testInput(true))
	if err != nil {
		t.Fatalf("Finalize: %v", err)
	}
	if res.TenantUUID != "tenant-1" {
		t.Errorf("resumed on tenant %q, want the reserved tenant-1", res.TenantUUID)
	}

	// Stages already recorded complete must NOT re-execute.
	if got := fx.log.only("config."); len(got) != 0 {
		t.Errorf("stage 1 re-executed on resume: %v", got)
	}
	if got := fx.log.only("tenants.EnsureSetupTenant"); len(got) != 0 {
		t.Errorf("stage 2 re-executed on resume: %v", got)
	}
	if got := fx.log.only("tenants.AssignDefaultTenant"); len(got) != 1 {
		t.Errorf("stage 3 ran %d times, want exactly 1", len(got))
	}
	if rec := fx.store.snapshotRecord(); rec.CompletedAt == nil {
		t.Error("resume did not complete the saga")
	}
}

// --- state table row 4: different hash -> 409 already started ----------

func TestFinalize_DifferentHashWhileReserved_AlreadyStarted(t *testing.T) {
	fx := newSagaFixture(
		&systeminit.FinalizationRecord{
			AdminUUID: "admin-1", Stage: systeminit.StageTenant, Revision: 4,
			RequestHash: "a-different-request-hash", TenantUUID: "tenant-1",
			TenantName: "Other", TenantSlug: "other", Mode: modeSingle,
		},
		map[string]iface.UserLifecycleState{"admin-1": iface.UserLifecycleActive},
	)

	_, err := fx.svc.Finalize(context.Background(), "admin-1", "super_admin", testInput(true))
	if !errors.Is(err, ErrFinalizationAlreadyStarted) {
		t.Fatalf("err = %v, want ErrFinalizationAlreadyStarted", err)
	}
	if effects := fx.log.only("config.", "tenants."); len(effects) != 0 {
		t.Errorf("a different payload must never reach a side effect; ran %v", effects)
	}
	if rec := fx.store.snapshotRecord(); rec.RequestHash != "a-different-request-hash" {
		t.Errorf("reservation was overwritten: %+v", rec)
	}
}

// --- state table row 5: complete + matching hash -> replay -------------

func TestFinalize_CompleteMatchingHash_ReplaysSnapshot(t *testing.T) {
	now := time.Now().UTC()
	fx := newSagaFixture(
		&systeminit.FinalizationRecord{
			AdminUUID: "admin-1", Stage: systeminit.StageDone, Revision: 9,
			RequestHash: testHash(true), CompletedAt: &now,
			Result: &systeminit.FinalizationResult{
				TenantUUID: "tenant-1", TenantName: "Acme Corp", TenantSlug: "acme", Mode: modeManual,
			},
		},
		map[string]iface.UserLifecycleState{"admin-1": iface.UserLifecycleActive},
	)

	res, err := fx.svc.Finalize(context.Background(), "admin-1", "super_admin", testInput(true))
	if err != nil {
		t.Fatalf("Finalize: %v", err)
	}
	if res.TenantUUID != "tenant-1" || res.TenantName != "Acme Corp" || res.TenantSlug != "acme" || res.Mode != modeManual {
		t.Errorf("replay result = %+v, want the persisted snapshot", res)
	}
	if effects := fx.log.only("config.", "tenants."); len(effects) != 0 {
		t.Errorf("a replay must execute no stage; ran %v", effects)
	}
	for _, entry := range fx.log.snapshot() {
		if entry != "store.Get" {
			t.Errorf("replay performed a store mutation: %v", fx.log.snapshot())
			break
		}
	}
	if len(fx.audit.events) != 0 {
		t.Errorf("replay emitted audit events: %+v", fx.audit.events)
	}
}

// --- state table row 6: complete + different/missing hash -> 409 -------

func TestFinalize_CompleteDifferentHash_AlreadyCompleted(t *testing.T) {
	now := time.Now().UTC()
	fx := newSagaFixture(
		&systeminit.FinalizationRecord{
			AdminUUID: "admin-1", Stage: systeminit.StageDone, Revision: 9,
			RequestHash: testHash(false), CompletedAt: &now,
			Result: &systeminit.FinalizationResult{TenantUUID: "tenant-1", Mode: modeSingle},
		},
		map[string]iface.UserLifecycleState{"admin-1": iface.UserLifecycleActive},
	)

	_, err := fx.svc.Finalize(context.Background(), "admin-1", "super_admin", testInput(true))
	if !errors.Is(err, ErrFinalizationAlreadyCompleted) {
		t.Fatalf("err = %v, want ErrFinalizationAlreadyCompleted", err)
	}
}

func TestFinalize_CompletedByMigrationNoHash_AlreadyCompleted(t *testing.T) {
	now := time.Now().UTC()
	fx := newSagaFixture(
		&systeminit.FinalizationRecord{
			AdminUUID: "admin-1", Source: systeminit.SourceMigration,
			Stage: systeminit.StageDone, Revision: 2, CompletedAt: &now,
			Result: &systeminit.FinalizationResult{TenantUUID: "legacy-tenant", Mode: modeManual},
		},
		map[string]iface.UserLifecycleState{"admin-1": iface.UserLifecycleActive},
	)

	// An installation completed by migration has no client request hash,
	// so no payload can ever match it.
	_, err := fx.svc.Finalize(context.Background(), "admin-1", "super_admin", testInput(true))
	if !errors.Is(err, ErrFinalizationAlreadyCompleted) {
		t.Fatalf("err = %v, want ErrFinalizationAlreadyCompleted", err)
	}
	_, err = fx.svc.Finalize(context.Background(), "admin-1", "super_admin", testInput(false))
	if !errors.Is(err, ErrFinalizationAlreadyCompleted) {
		t.Fatalf("err = %v, want ErrFinalizationAlreadyCompleted", err)
	}
}

func TestFinalize_CompleteMatchingHashButUnauthorizedCaller_AlreadyCompleted(t *testing.T) {
	now := time.Now().UTC()
	fx := newSagaFixture(
		&systeminit.FinalizationRecord{
			AdminUUID: "admin-1", Stage: systeminit.StageDone, Revision: 9,
			RequestHash: testHash(true), CompletedAt: &now,
			Result: &systeminit.FinalizationResult{TenantUUID: "tenant-1", Mode: modeManual},
		},
		map[string]iface.UserLifecycleState{"admin-1": iface.UserLifecycleActive, "admin-2": iface.UserLifecycleActive},
	)

	// Only the AUTHORIZED replay receives the snapshot; anybody else is
	// told the truth that setup is finished, without confirming that
	// their payload matched.
	_, err := fx.svc.Finalize(context.Background(), "admin-2", "super_admin", testInput(true))
	if !errors.Is(err, ErrFinalizationAlreadyCompleted) {
		t.Fatalf("err = %v, want ErrFinalizationAlreadyCompleted", err)
	}
}

// --- authorization + recovery ------------------------------------------

func TestFinalize_BoundToAnotherAdmin_Forbidden(t *testing.T) {
	fx := newSagaFixture(
		&systeminit.FinalizationRecord{AdminUUID: "admin-1", Stage: systeminit.StageConfig, Revision: 1},
		map[string]iface.UserLifecycleState{"admin-1": iface.UserLifecycleActive},
	)

	_, err := fx.svc.Finalize(context.Background(), "admin-2", "super_admin", testInput(true))
	if !errors.Is(err, ErrFinalizerBoundToAnotherAdmin) {
		t.Fatalf("err = %v, want ErrFinalizerBoundToAnotherAdmin", err)
	}
	if effects := fx.log.only("config.", "tenants.", "store.ClaimRecovery", "store.ReserveRequest"); len(effects) != 0 {
		t.Errorf("denied caller mutated state: %v", effects)
	}
}

func TestFinalize_UnusableBindingLowerRole_RecoveryRequiresSuperAdmin(t *testing.T) {
	fx := newSagaFixture(
		&systeminit.FinalizationRecord{AdminUUID: "ghost", Stage: systeminit.StageConfig, Revision: 1},
		map[string]iface.UserLifecycleState{"ghost": iface.UserLifecycleDeleted, "admin-2": iface.UserLifecycleActive},
	)

	_, err := fx.svc.Finalize(context.Background(), "admin-2", "administrator", testInput(true))
	if !errors.Is(err, ErrRecoveryRequiresSuperAdmin) {
		t.Fatalf("err = %v, want ErrRecoveryRequiresSuperAdmin", err)
	}
	if n := fx.log.count("store.ClaimRecovery:ghost->admin-2"); n != 0 {
		t.Error("a lower system role must never reach the recovery CAS")
	}
}

func TestFinalize_UnusableBindingActiveSuperAdmin_ClaimsRecoveryAndAudits(t *testing.T) {
	for _, tc := range []struct {
		name       string
		bound      string
		boundState iface.UserLifecycleState
		wantReason string
	}{
		{"deleted binding", "ghost", iface.UserLifecycleDeleted, "deleted"},
		{"missing binding", "ghost", iface.UserLifecycleMissing, "missing"},
		{"inactive binding", "ghost", iface.UserLifecycleInactive, "inactive"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fx := newSagaFixture(
				&systeminit.FinalizationRecord{AdminUUID: tc.bound, Stage: systeminit.StageConfig, Revision: 3},
				map[string]iface.UserLifecycleState{tc.bound: tc.boundState, "sa-1": iface.UserLifecycleActive},
			)

			res, err := fx.svc.Finalize(context.Background(), "sa-1", "super_admin", testInput(true))
			if err != nil {
				t.Fatalf("Finalize: %v", err)
			}
			if n := fx.log.count("store.ClaimRecovery:ghost->sa-1"); n != 1 {
				t.Fatalf("recovery CAS ran %d times with the observed binding, want 1 (log=%v)", n, fx.log.snapshot())
			}

			events := fx.audit.byAction("setup.finalizer.recovered")
			if len(events) != 1 {
				t.Fatalf("setup.finalizer.recovered events = %d, want 1", len(events))
			}
			ev := events[0]
			if ev.ActorUserID != "sa-1" {
				t.Errorf("audit actor = %q, want the winning super_admin", ev.ActorUserID)
			}
			if ev.Metadata["previousAdminUUID"] != tc.bound {
				t.Errorf("metadata previousAdminUUID = %v, want %q", ev.Metadata["previousAdminUUID"], tc.bound)
			}
			if ev.Metadata["reason"] != tc.wantReason {
				t.Errorf("metadata reason = %v, want %q", ev.Metadata["reason"], tc.wantReason)
			}
			if ev.Metadata["newAdminUUID"] != "sa-1" {
				t.Errorf("metadata newAdminUUID = %v, want %q", ev.Metadata["newAdminUUID"], "sa-1")
			}
			// Recovery metadata stays minimal: no name, email, token or
			// profile snapshot may ever appear.
			if len(ev.Metadata) != 3 {
				t.Errorf("recovery metadata carries %d keys (%v); only previousAdminUUID/reason/newAdminUUID are allowed", len(ev.Metadata), ev.Metadata)
			}
			if ev.ActorEmail != "" {
				t.Errorf("recovery audit leaks an email: %q", ev.ActorEmail)
			}

			// Recovery hands the saga to the new administrator.
			if rec := fx.store.snapshotRecord(); rec.AdminUUID != "sa-1" {
				t.Errorf("binding = %q, want sa-1", rec.AdminUUID)
			}
			if len(fx.tenants.ensureArgs) != 1 || fx.tenants.ensureArgs[0].ownerUUID != "sa-1" {
				t.Errorf("setup tenant owner = %+v, want the recovered administrator", fx.tenants.ensureArgs)
			}
			if res.TenantUUID == "" {
				t.Error("recovery run produced no tenant")
			}
		})
	}
}

func TestFinalize_EmptyBindingSuperAdmin_RecoversWithMissingReasonAndNoPreviousUUID(t *testing.T) {
	fx := newSagaFixture(
		&systeminit.FinalizationRecord{AdminUUID: "", Source: systeminit.SourceLegacyRecovery, Stage: systeminit.StageConfig, Revision: 1},
		map[string]iface.UserLifecycleState{"sa-1": iface.UserLifecycleActive},
	)

	if _, err := fx.svc.Finalize(context.Background(), "sa-1", "super_admin", testInput(true)); err != nil {
		t.Fatalf("Finalize: %v", err)
	}
	events := fx.audit.byAction("setup.finalizer.recovered")
	if len(events) != 1 {
		t.Fatalf("setup.finalizer.recovered events = %d, want 1", len(events))
	}
	ev := events[0]
	if _, present := ev.Metadata["previousAdminUUID"]; present {
		t.Errorf("an empty binding has no previous UUID; metadata = %v", ev.Metadata)
	}
	if ev.Metadata["reason"] != "missing" {
		t.Errorf("reason = %v, want missing", ev.Metadata["reason"])
	}
}

func TestFinalize_NoCoordinatorRecordSuperAdmin_CreatesThenClaims(t *testing.T) {
	// evaluateAccess can hand back "may claim recovery" with a nil record
	// — a legacy installation that predates the coordinator. The claim
	// path must cope with there being nothing to CAS against yet.
	fx := newSagaFixture(nil, map[string]iface.UserLifecycleState{"sa-1": iface.UserLifecycleActive})

	res, err := fx.svc.Finalize(context.Background(), "sa-1", "super_admin", testInput(false))
	if err != nil {
		t.Fatalf("Finalize: %v", err)
	}
	if n := fx.log.count("store.EnsureRecord:" + systeminit.SourceLegacyRecovery); n != 1 {
		t.Errorf("EnsureRecord(legacy_recovery) ran %d times, want 1 (log=%v)", n, fx.log.snapshot())
	}
	if n := fx.log.count("store.ClaimRecovery:->sa-1"); n != 1 {
		t.Errorf("recovery CAS ran %d times, want 1 (log=%v)", n, fx.log.snapshot())
	}
	if rec := fx.store.snapshotRecord(); rec == nil || rec.AdminUUID != "sa-1" || rec.CompletedAt == nil {
		t.Errorf("record = %+v, want claimed + completed", rec)
	}
	if res.Mode != modeSingle {
		t.Errorf("mode = %q, want single", res.Mode)
	}
}

func TestFinalize_RecoveryCASLost_ReEvaluatesOnce(t *testing.T) {
	// A concurrent super_admin already won the claim: the record now binds
	// somebody else, and our CAS misses. The loser must re-evaluate
	// authorization and be told the binding belongs to another admin —
	// never overwrite the winner.
	fx := newSagaFixture(
		&systeminit.FinalizationRecord{AdminUUID: "ghost", Stage: systeminit.StageConfig, Revision: 3},
		map[string]iface.UserLifecycleState{
			"ghost": iface.UserLifecycleDeleted,
			"sa-1":  iface.UserLifecycleActive,
			"sa-2":  iface.UserLifecycleActive,
		},
	)
	// Simulate the winner landing between our read and our CAS by moving
	// the record on before Finalize runs its claim: the observed revision
	// will no longer match.
	fx.store.rec.AdminUUID = "sa-2"
	fx.store.rec.Revision = 4
	fx.users.states["ghost"] = iface.UserLifecycleDeleted

	_, err := fx.svc.Finalize(context.Background(), "sa-1", "super_admin", testInput(true))
	if !errors.Is(err, ErrFinalizerBoundToAnotherAdmin) {
		t.Fatalf("err = %v, want ErrFinalizerBoundToAnotherAdmin", err)
	}
	if rec := fx.store.snapshotRecord(); rec.AdminUUID != "sa-2" {
		t.Errorf("CAS loser overwrote the winner: binding = %q", rec.AdminUUID)
	}
	if len(fx.audit.byAction("setup.finalizer.recovered")) != 0 {
		t.Error("a lost recovery CAS must not emit a recovery audit event")
	}
}

// TestFinalize_BindingStolenBeforeReservation_IsForbiddenNotConflict pins
// the ordering of the post-reservation state table: when the reservation
// CAS misses because somebody recovered the binding in between, the record
// still carries NO request hash — reporting "a different request is
// already reserved" would be false. The caller is told the truth instead:
// they have no authority here.
func TestFinalize_BindingStolenBeforeReservation_IsForbiddenNotConflict(t *testing.T) {
	fx := newSagaFixture(
		&systeminit.FinalizationRecord{AdminUUID: "admin-1", Stage: systeminit.StageConfig, Revision: 1},
		map[string]iface.UserLifecycleState{"admin-1": iface.UserLifecycleActive},
	)
	fx.store.hookReserve = func(rec *systeminit.FinalizationRecord) {
		rec.AdminUUID = "someone-else"
		rec.Revision++
	}

	_, err := fx.svc.Finalize(context.Background(), "admin-1", "super_admin", testInput(true))
	if !errors.Is(err, ErrFinalizerBoundToAnotherAdmin) {
		t.Fatalf("err = %v, want ErrFinalizerBoundToAnotherAdmin", err)
	}
	if rec := fx.store.snapshotRecord(); rec.RequestHash != "" {
		t.Errorf("a losing reservation still wrote a hash: %q", rec.RequestHash)
	}
	if effects := fx.log.only("config.", "tenants."); len(effects) != 0 {
		t.Errorf("stages ran despite the lost reservation: %v", effects)
	}
}

func TestFinalize_LifecycleLookupError_UnavailableAndNoClaim(t *testing.T) {
	fx := newSagaFixture(
		&systeminit.FinalizationRecord{AdminUUID: "ghost", Stage: systeminit.StageConfig, Revision: 1},
		map[string]iface.UserLifecycleState{"sa-1": iface.UserLifecycleActive},
	)
	fx.users.err = errors.New("mongo down")

	_, err := fx.svc.Finalize(context.Background(), "sa-1", "super_admin", testInput(true))
	if !errors.Is(err, ErrFinalizerStateUnavailable) {
		t.Fatalf("err = %v, want ErrFinalizerStateUnavailable", err)
	}
	if n := fx.log.count("store.ClaimRecovery:ghost->sa-1"); n != 0 {
		t.Error("a lookup failure must never open recovery")
	}
	if effects := fx.log.only("config.", "tenants."); len(effects) != 0 {
		t.Errorf("a lookup failure executed stages: %v", effects)
	}
}

func TestFinalize_CoordinatorReadError_Unavailable(t *testing.T) {
	fx := newSagaFixture(nil, map[string]iface.UserLifecycleState{"admin-1": iface.UserLifecycleActive})
	fx.store.getErr = errors.New("mongo down")

	_, err := fx.svc.Finalize(context.Background(), "admin-1", "super_admin", testInput(true))
	if !errors.Is(err, ErrFinalizerStateUnavailable) {
		t.Fatalf("err = %v, want ErrFinalizerStateUnavailable", err)
	}
}

// --- stage failure ------------------------------------------------------

func TestFinalize_StageFailure_DoesNotAdvanceOrComplete(t *testing.T) {
	fx := newSagaFixture(
		&systeminit.FinalizationRecord{AdminUUID: "admin-1", Stage: systeminit.StageConfig, Revision: 1},
		map[string]iface.UserLifecycleState{"admin-1": iface.UserLifecycleActive},
	)
	fx.tenants.ensureErr = errors.New("kms unavailable")

	_, err := fx.svc.Finalize(context.Background(), "admin-1", "super_admin", testInput(true))
	if err == nil {
		t.Fatal("expected an error when the tenant stage fails")
	}
	if errors.Is(err, ErrFinalizationInProgress) {
		t.Fatalf("a stage failure must not masquerade as in-progress: %v", err)
	}

	rec := fx.store.snapshotRecord()
	if rec.Stage != systeminit.StageTenant {
		t.Errorf("stage = %d, want it parked on StageTenant (%d)", rec.Stage, systeminit.StageTenant)
	}
	if rec.CompletedAt != nil {
		t.Error("setup was marked complete despite a failed stage")
	}
	if n := fx.log.count("store.Complete"); n != 0 {
		t.Errorf("Complete ran %d times after a stage failure", n)
	}
	if len(fx.audit.byAction("setup.completed")) != 0 {
		t.Error("setup.completed audited despite a failed stage")
	}
	if len(fx.tenants.assignArgs) != 0 {
		t.Error("a later stage ran despite the earlier stage failing")
	}
}

func TestFinalize_ConfigStageFailure_LeavesStageOne(t *testing.T) {
	fx := newSagaFixture(
		&systeminit.FinalizationRecord{AdminUUID: "admin-1", Stage: systeminit.StageConfig, Revision: 1},
		map[string]iface.UserLifecycleState{"admin-1": iface.UserLifecycleActive},
	)
	fx.cfg.updateErr = errors.New("module config write failed")

	if _, err := fx.svc.Finalize(context.Background(), "admin-1", "super_admin", testInput(true)); err == nil {
		t.Fatal("expected an error when the config stage fails")
	}
	rec := fx.store.snapshotRecord()
	if rec.Stage != systeminit.StageConfig || rec.CompletedAt != nil {
		t.Errorf("record advanced past the failed stage: %+v", rec)
	}
	if len(fx.tenants.ensureArgs) != 0 {
		t.Error("stage 2 ran after stage 1 failed")
	}
}

// --- 202 body shape -----------------------------------------------------

func TestFinalizeInProgressBody_CarriesNoProblemDetailsMembers(t *testing.T) {
	body := FinalizeResponseBody{State: finalizeStateInProgress}
	data, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}
	var m map[string]json.RawMessage
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}
	if len(m) != 1 {
		t.Fatalf("in-progress body = %s, want exactly {\"state\":…}", data)
	}
	if _, ok := m["state"]; !ok {
		t.Fatalf("in-progress body = %s, want a state member", data)
	}
	for _, forbidden := range []string{"status", "title", "detail", "code", "type", "errors"} {
		if _, present := m[forbidden]; present {
			t.Errorf("202 body carries Problem Details member %q: %s", forbidden, data)
		}
	}
	if finalizeStateInProgress != "setup.finalization_in_progress" {
		t.Errorf("state = %q, want setup.finalization_in_progress", finalizeStateInProgress)
	}
}

// --- handler ------------------------------------------------------------

func newFinalizeHandler(fx sagaFixture) *Handler {
	return NewHandler(fx.svc, config.CookieConfig{})
}

func TestFinalizeHandler_Success_200WithBody(t *testing.T) {
	fx := newSagaFixture(
		&systeminit.FinalizationRecord{AdminUUID: "admin-1", Stage: systeminit.StageConfig, Revision: 1},
		map[string]iface.UserLifecycleState{"admin-1": iface.UserLifecycleActive},
	)
	h := newFinalizeHandler(fx)

	ctx := testkit.NewIdentity("admin-1", "admin@example.com", "super_admin").ContextFor(context.Background(), "")
	req := &FinalizeRequest{}
	req.Body.TenantName = "  Acme   Corp "
	req.Body.TenantSlug = "  ACME  "
	allow := true
	req.Body.AllowAdditionalInternalTenants = &allow

	out, err := h.Finalize(ctx, req)
	if err != nil {
		t.Fatalf("Finalize: %v", err)
	}
	if out.Status != http.StatusOK {
		t.Errorf("status = %d, want 200", out.Status)
	}
	if out.RetryAfter != "" {
		t.Errorf("Retry-After = %q, want empty on the terminal 200", out.RetryAfter)
	}
	if out.CacheControl != "no-store" {
		t.Errorf("Cache-Control = %q, want no-store", out.CacheControl)
	}
	if out.Body.TenantName != "Acme Corp" || out.Body.TenantSlug != "acme" || out.Body.Mode != modeManual {
		t.Errorf("body = %+v", out.Body)
	}
	if out.Body.AllowAdditionalInternalTenants == nil || !*out.Body.AllowAdditionalInternalTenants {
		t.Errorf("allowAdditionalInternalTenants = %v, want true (manual)", out.Body.AllowAdditionalInternalTenants)
	}
	if out.Body.State != "" {
		t.Errorf("state = %q, want empty on the terminal 200", out.Body.State)
	}
}

func TestFinalizeHandler_InProgress_202WithRetryAfter(t *testing.T) {
	now := time.Now().UTC()
	fx := newSagaFixture(
		&systeminit.FinalizationRecord{
			AdminUUID: "admin-1", Stage: systeminit.StageTenant, Revision: 4,
			RequestHash: testHash(true), TenantUUID: "tenant-1", Mode: modeManual,
			LeaseOwner: "another-executor", LeaseUntil: ptrTime(now.Add(30 * time.Second)),
		},
		map[string]iface.UserLifecycleState{"admin-1": iface.UserLifecycleActive},
	)
	h := newFinalizeHandler(fx)

	ctx := testkit.NewIdentity("admin-1", "admin@example.com", "super_admin").ContextFor(context.Background(), "")
	req := &FinalizeRequest{}
	req.Body.TenantName = "Acme Corp"
	req.Body.TenantSlug = "acme"
	allow := true
	req.Body.AllowAdditionalInternalTenants = &allow

	out, err := h.Finalize(ctx, req)
	if err != nil {
		t.Fatalf("the 202 is a success, not an error envelope: %v", err)
	}
	if out.Status != http.StatusAccepted {
		t.Errorf("status = %d, want 202", out.Status)
	}
	if out.RetryAfter == "" {
		t.Error("202 carries no Retry-After header")
	}
	if out.Body.State != finalizeStateInProgress {
		t.Errorf("state = %q, want %q", out.Body.State, finalizeStateInProgress)
	}
	if out.Body.TenantID != "" || out.Body.Mode != "" || out.Body.AllowAdditionalInternalTenants != nil {
		t.Errorf("202 body leaks result fields: %+v", out.Body)
	}
}

func TestFinalizeHandler_NoCallerIdentity_401(t *testing.T) {
	fx := newSagaFixture(nil, nil)
	h := newFinalizeHandler(fx)

	req := &FinalizeRequest{}
	allow := false
	req.Body.AllowAdditionalInternalTenants = &allow
	_, err := h.Finalize(context.Background(), req)
	if err == nil {
		t.Fatal("expected 401")
	}
	var se huma.StatusError
	if !errors.As(err, &se) || se.GetStatus() != http.StatusUnauthorized {
		t.Fatalf("err = %v, want 401", err)
	}
}

func TestFinalizeHandler_ErrorMapping(t *testing.T) {
	cases := []struct {
		name       string
		err        error
		wantStatus int
		wantCode   string
	}{
		{"bound to another admin", ErrFinalizerBoundToAnotherAdmin, http.StatusForbidden, errcode.SetupFinalizerBoundToAnotherAdmin},
		{"recovery requires super admin", ErrRecoveryRequiresSuperAdmin, http.StatusForbidden, errcode.SetupRecoveryRequiresSuperAdmin},
		{"already started", ErrFinalizationAlreadyStarted, http.StatusConflict, errcode.SetupFinalizationAlreadyStarted},
		{"already completed", ErrFinalizationAlreadyCompleted, http.StatusConflict, errcode.SetupAlreadyCompleted},
		{"state unavailable", ErrFinalizerStateUnavailable, http.StatusServiceUnavailable, errcode.SetupFinalizerStateUnavailable},
		{"seam slug conflict", &SeamError{Kind: SeamSlugConflict}, http.StatusConflict, errcode.TenantSlugAlreadyInUse},
		{"seam provisioning locked", &SeamError{Kind: SeamProvisioningLocked}, http.StatusConflict, errcode.TenantProvisioningLocked},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			mapped := mapFinalizeError(discardLogger(), c.err)
			var ee *errcode.Error
			if !errors.As(mapped, &ee) {
				t.Fatalf("mapped error carries no *errcode.Error: %v", mapped)
			}
			if ee.Status != c.wantStatus {
				t.Errorf("status = %d, want %d", ee.Status, c.wantStatus)
			}
			if ee.Code != c.wantCode {
				t.Errorf("code = %q, want %q", ee.Code, c.wantCode)
			}
			if ee.Detail == "" || strings.Contains(ee.Detail, c.err.Error()) {
				t.Errorf("detail must be a fixed written sentence, got %q", ee.Detail)
			}
		})
	}
}

func TestFinalizeHandler_UnnamedError_Is500WithFixedDetail(t *testing.T) {
	mapped := mapFinalizeError(discardLogger(), errors.New("kms exploded: dial tcp 10.0.0.5:443"))
	var se huma.StatusError
	if !errors.As(mapped, &se) {
		t.Fatalf("mapped error is not a huma.StatusError: %v", mapped)
	}
	if se.GetStatus() != http.StatusInternalServerError {
		t.Errorf("status = %d, want 500 — an error the handler cannot name is a server fault", se.GetStatus())
	}
	if strings.Contains(se.Error(), "10.0.0.5") {
		t.Errorf("client-facing detail leaks the underlying error: %q", se.Error())
	}
}

// TestFinalizeHandler_OmittedBool_IsSchemaRejected proves the required
// pointer bool: an omitted allowAdditionalInternalTenants is a 422 from
// Huma's request validation, never a silent `false` reaching the service.
func TestFinalizeHandler_OmittedBool_IsSchemaRejected(t *testing.T) {
	fx := newSagaFixture(
		&systeminit.FinalizationRecord{AdminUUID: "admin-1", Stage: systeminit.StageConfig, Revision: 1},
		map[string]iface.UserLifecycleState{"admin-1": iface.UserLifecycleActive},
	)
	h := newFinalizeHandler(fx)

	mux := chi.NewRouter()
	api := humachi.New(mux, huma.DefaultConfig("finalize-schema-test", "1.0.0"))
	h.RegisterProtectedRoutes(api)

	body := strings.NewReader(`{"tenantName":"Acme Corp","tenantSlug":"acme"}`)
	req := httptest.NewRequest(http.MethodPost, "/v1/setup/finalize", body)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422 (body=%s)", rec.Code, rec.Body.String())
	}
	if entries := fx.log.snapshot(); len(entries) != 0 {
		t.Errorf("the handler ran despite the schema rejection: %v", entries)
	}

	// …while the same payload WITH the flag reaches the service (and is
	// rejected only because the test request carries no identity).
	body = strings.NewReader(`{"tenantName":"Acme Corp","tenantSlug":"acme","allowAdditionalInternalTenants":false}`)
	req = httptest.NewRequest(http.MethodPost, "/v1/setup/finalize", body)
	req.Header.Set("Content-Type", "application/json")
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401 — the payload should have passed schema validation (body=%s)", rec.Code, rec.Body.String())
	}
}
