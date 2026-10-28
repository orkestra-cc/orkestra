package services

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/orkestra/backend/internal/core/retrieval/engine"
	"github.com/orkestra/backend/internal/core/retrieval/models"
	"github.com/orkestra/backend/pkg/sdk/iface"
)

func TestValidateOwnerSubject(t *testing.T) {
	ok := []string{"crm:42", "crm_contact:0f8c-11", "a:x", "crm-pro:7", "crm:" + strings.Repeat("x", 252)}
	bad := []string{"", "crm", ":42", "crm:", "user:u1", "CRM:42", "crm:4 2", "crm:" + strings.Repeat("x", 253),
		"1crm:4", strings.Repeat("a", 65) + ":4", "crm:4\x00"}
	for _, s := range ok {
		owner, err := ValidateOwnerSubject(s)
		require.NoError(t, err)
		assert.Equal(t, s[:strings.Index(s, ":")], owner)
	}
	for _, s := range bad {
		_, err := ValidateOwnerSubject(s)
		requireIs(t, err, iface.ErrRetrievalInvalidRequest)
	}
}

// TestSubjectRule_IndexableIsErasable pins R-F1: every non-user subject an
// item can carry at ingest is accepted by DeleteBySubject, and nothing
// DeleteBySubject accepts is refused at ingest. The erasure service sees the
// same registered owners as ingest (the corpus owner "crm" plus "billing").
func TestSubjectRule_IndexableIsErasable(t *testing.T) {
	env := newErasureEnv(t, nil, nil)
	registered := func(o string) bool { return o == "crm" || ingestOwners(o) }
	svc := NewErasureService(env.flaky, env.fin, registered, nil, NewAuditRelay(), env.clk.Now,
		slog.New(slog.NewTextHandler(io.Discard, nil)))
	candidates := []string{
		"crm:42", "billing:7", "crm_contact:1", "crm:0f8c-11", "crm:" + strings.Repeat("x", 252),
		"crm:" + strings.Repeat("x", 253), "crm:a b", "Crm:1", "other:1", "contact-42", "crm:", ":1",
		"crm:4\x00", "crm:\u00a0x", "user:u1", "user:", "user:a b",
	}
	indexable := 0
	for _, s := range candidates {
		ingestErr := validateItem(iface.RetrievalItem{Subjects: []string{s}}, "crm", ingestOwners)
		if strings.HasPrefix(s, "user:") {
			continue // user subjects are erased through the PII producer
		}
		eraseErr := svc.DeleteBySubject(context.Background(), "tA", s)
		if (ingestErr == nil) != (eraseErr == nil) {
			t.Fatalf("subject %q: ingest err %v, DeleteBySubject err %v", s, ingestErr, eraseErr)
		}
		if ingestErr == nil {
			indexable++
		}
	}
	assert.Equal(t, 4, indexable)
	requireIs(t, svc.DeleteBySubject(context.Background(), "tA", "user:u1"), iface.ErrRetrievalInvalidRequest)
}

// flakyStore fails the first tenantFails EnsureTenant calls.
type flakyStore struct {
	*memStore
	mu          sync.Mutex
	tenantFails int
	tenantCalls int
}

func (f *flakyStore) EnsureTenant(ctx context.Context, tenantID string, now time.Time) (*models.Erasure, bool, error) {
	f.mu.Lock()
	f.tenantCalls++
	fail := f.tenantCalls <= f.tenantFails
	f.mu.Unlock()
	if fail {
		return nil, false, errors.New("mongo down")
	}
	return f.memStore.EnsureTenant(ctx, tenantID, now)
}

type stubPurged struct {
	ids []string
	err error
}

func (s stubPurged) ListPurgedTenantIDs(context.Context) ([]string, error) { return s.ids, s.err }

type erasureEnv struct {
	*finEnv
	flaky *flakyStore
	svc   *ErasureService
}

func newErasureEnv(t *testing.T, cols map[string][]string, purged iface.PurgedTenantIDLister) *erasureEnv {
	t.Helper()
	fe := newFinEnv(t, cols, true)
	flaky := &flakyStore{memStore: fe.store}
	// The finalizer reads the same rows through the embedded memStore.
	relay := NewAuditRelay()
	relay.SetAuditSink(fe.sink)
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	svc := NewErasureService(flaky, fe.fin, func(o string) bool { return o == "crm" }, purged, relay, fe.clk.Now, logger)
	return &erasureEnv{finEnv: fe, flaky: flaky, svc: svc}
}

func (e *erasureEnv) tombstones(kind string) []models.Erasure {
	e.store.mu.Lock()
	defer e.store.mu.Unlock()
	var out []models.Erasure
	for _, r := range e.store.rows {
		if r.Kind == kind {
			out = append(out, *r)
		}
	}
	return out
}

func TestDeleteBySubject_RefusesUnknownOwnerAndEmptyTenant(t *testing.T) {
	env := newErasureEnv(t, nil, nil)
	for _, c := range [][2]string{{"tA", "billing:1"}, {"", "crm:1"}, {"tA", "user:u1"}, {"tA", "crm"}} {
		requireIs(t, env.svc.DeleteBySubject(context.Background(), c[0], c[1]), iface.ErrRetrievalInvalidRequest)
	}
	assert.Empty(t, env.tombstones(models.ErasureKindSubject))
	assert.Empty(t, env.logs.callLog())
}

func TestDeleteBySubject_TombstoneLogsAndFirstPass(t *testing.T) {
	env := newErasureEnv(t, map[string][]string{"people": {"g1"}}, nil)
	env.eng.Seed("g1", finPoint("pa", "tA", "s1", "crm:42"), finPoint("pb", "tB", "s1", "crm:42"))
	require.NoError(t, env.svc.DeleteBySubject(context.Background(), "tA", "crm:42"))

	ts := env.tombstones(models.ErasureKindSubject)
	require.Len(t, ts, 1)
	assert.Equal(t, models.ErasureScopeTenant, ts[0].Scope)
	assert.Equal(t, "tA", ts[0].TenantID)
	assert.Equal(t, "crm:42", ts[0].Subject)
	assert.Equal(t, models.ErasureStatusFencing, ts[0].Status)
	assert.Equal(t, []string{"DeleteBySubject(tA,crm:42)"}, env.logs.callLog())
	assert.Equal(t, int64(0), env.count(t, "g1", engine.Selector{TenantID: "tA"}))
	assert.Equal(t, int64(1), env.count(t, "g1", engine.Selector{TenantID: "tB"}))

	ev := env.sink.byAction(AuditErasureRecorded)
	require.Len(t, ev, 1)
	assert.Equal(t, "tA", ev[0].TenantID)
	assert.Equal(t, "retrieval_erasure", ev[0].ResourceType)
	assert.Equal(t, "subject", ev[0].Metadata["kind"])
	_, hasSubject := ev[0].Metadata["subject"]
	assert.Equal(t, false, hasSubject)

	require.NoError(t, env.svc.DeleteBySubject(context.Background(), "tA", "crm:42"))
	assert.Len(t, env.tombstones(models.ErasureKindSubject), 1)
	assert.Len(t, env.sink.byAction(AuditErasureRecorded), 1)
}

func TestDeleteBySubject_NeedsNoTenantInContext(t *testing.T) {
	env := newErasureEnv(t, nil, nil)
	require.NoError(t, env.svc.DeleteBySubject(context.Background(), "tA", "crm:42"))
	require.Len(t, env.tombstones(models.ErasureKindSubject), 1)
}

func TestPurgeTenant_TombstoneLogsAndCount(t *testing.T) {
	env := newErasureEnv(t, map[string][]string{"people": {"g1"}, "orders": {"o1"}}, nil)
	env.eng.Seed("g1", finPoint("a1", "tA", "s1"), finPoint("b1", "tB", "s1"))
	env.eng.Seed("o1", finPoint("a2", "tA", "s2"), finPoint("b2", "tB", "s2"))
	require.NoError(t, env.svc.PurgeTenant(context.Background(), "tA"))

	ts := env.tombstones(models.ErasureKindTenant)
	require.Len(t, ts, 1)
	assert.Equal(t, "tA", ts[0].TenantID)
	assert.Equal(t, []string{"DeleteByTenant(tA)"}, env.logs.callLog())
	for _, c := range []string{"g1", "o1"} {
		assert.Equal(t, int64(0), env.count(t, c, engine.Selector{TenantID: "tA"}), c)
		assert.Equal(t, int64(1), env.count(t, c, engine.Selector{TenantID: "tB"}), c)
	}
	ev := env.sink.byAction(AuditErasureRecorded)
	require.Len(t, ev, 1)
	assert.Equal(t, "tenant", ev[0].Metadata["kind"])

	env.clk.Advance(env.row(t, ts[0].UUID).FenceUntil.Sub(env.clk.Now()))
	env.runOnce(t, 1)
	assert.Equal(t, models.ErasureStatusDone, env.row(t, ts[0].UUID).Status)
}

func TestPurgeTenant_RetriesTombstoneWriteThenFails(t *testing.T) {
	env := newErasureEnv(t, nil, nil)
	env.flaky.tenantFails = 3
	if env.svc.PurgeTenant(context.Background(), "tA") == nil {
		t.Fatal("PurgeTenant = nil, want the tombstone error")
	}
	assert.Equal(t, 3, env.flaky.tenantCalls)
	assert.Empty(t, env.sink.byAction(AuditErasureRecorded))
	assert.Empty(t, env.tombstones(models.ErasureKindTenant))

	env2 := newErasureEnv(t, nil, nil)
	env2.flaky.tenantFails = 2
	require.NoError(t, env2.svc.PurgeTenant(context.Background(), "tA"))
	assert.Equal(t, 3, env2.flaky.tenantCalls)
	assert.Len(t, env2.tombstones(models.ErasureKindTenant), 1)
}

func TestPurgeTenant_SurvivesCancelledRequestContext(t *testing.T) {
	env := newErasureEnv(t, nil, nil)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	require.NoError(t, env.svc.PurgeTenant(ctx, "tA"))
	assert.Len(t, env.tombstones(models.ErasureKindTenant), 1)
}

func TestPurgeTenant_RefusesEmptyTenant(t *testing.T) {
	env := newErasureEnv(t, nil, nil)
	requireIs(t, env.svc.PurgeTenant(context.Background(), ""), iface.ErrRetrievalInvalidRequest)
}

func TestPurgeTenant_LogDeleteFailureRecoveredByFinalizer(t *testing.T) {
	env := newErasureEnv(t, map[string][]string{"people": {"g1"}}, nil)
	env.eng.Seed("g1", finPoint("a1", "tA", "s1"))
	env.logs.setFail("DeleteByTenant", 1)
	require.NoError(t, env.svc.PurgeTenant(context.Background(), "tA"))

	id := env.tombstones(models.ErasureKindTenant)[0].UUID
	row := env.row(t, id)
	assert.Equal(t, models.ErasureStatusPending, row.Status)
	assert.Equal(t, 1, row.Attempts)

	env.clk.Advance(15 * time.Second)
	env.runOnce(t, 1)
	row = env.row(t, id)
	assert.Equal(t, models.ErasureStatusFencing, row.Status)
	env.clk.Advance(row.FenceUntil.Sub(env.clk.Now()))
	env.runOnce(t, 1)
	assert.Equal(t, models.ErasureStatusDone, env.row(t, id).Status)
}

func TestReconcilePurgedTenants_NoListerIsNoop(t *testing.T) {
	env := newErasureEnv(t, nil, nil)
	n, err := env.svc.ReconcilePurgedTenants(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 0, n)
	assert.Empty(t, env.tombstones(models.ErasureKindTenant))
}

func TestReconcilePurgedTenants_WritesOnlyMissingTombstones(t *testing.T) {
	env := newErasureEnv(t, nil, stubPurged{ids: []string{"tA", "tB"}})
	_, _, err := env.store.EnsureTenant(context.Background(), "tA", env.t0)
	require.NoError(t, err)

	n, err := env.svc.ReconcilePurgedTenants(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 1, n)
	assert.Equal(t, 2, len(env.tombstones(models.ErasureKindTenant)))
	assert.Equal(t, 1, env.flaky.tenantCalls, "only the missing tenant is ensured")
	ev := env.sink.byAction(AuditErasureRecorded)
	require.Len(t, ev, 1)
	assert.Equal(t, "tB", ev[0].TenantID)
	assert.Equal(t, "reconcile", ev[0].Metadata["source"])
	assert.Empty(t, env.logs.callLog())
}

func TestReconcilePurgedTenants_ListerErrorReturned(t *testing.T) {
	boom := errors.New("tenants down")
	env := newErasureEnv(t, nil, stubPurged{err: boom})
	n, err := env.svc.ReconcilePurgedTenants(context.Background())
	requireIs(t, err, boom)
	assert.Equal(t, 0, n)
}

func TestPurgeTenant_HookFailureRecoveredByReconcile(t *testing.T) {
	env := newErasureEnv(t, map[string][]string{"people": {"g1"}}, stubPurged{ids: []string{"tA"}})
	env.eng.Seed("g1", finPoint("a1", "tA", "s1"))
	env.flaky.tenantFails = 3
	if env.svc.PurgeTenant(context.Background(), "tA") == nil {
		t.Fatal("PurgeTenant = nil, want the tombstone error")
	}
	assert.Empty(t, env.tombstones(models.ErasureKindTenant))

	n, err := env.svc.ReconcilePurgedTenants(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 1, n)
	id := env.tombstones(models.ErasureKindTenant)[0].UUID

	env.runOnce(t, 1)
	row := env.row(t, id)
	assert.Equal(t, models.ErasureStatusFencing, row.Status)
	env.clk.Advance(row.FenceUntil.Sub(env.clk.Now()))
	env.runOnce(t, 1)
	assert.Equal(t, models.ErasureStatusDone, env.row(t, id).Status)
	assert.Equal(t, true, strings.Contains(strings.Join(env.logs.callLog(), " "), "DeleteByTenant(tA)"))
}

func TestService_DeleteBySubjectDelegates(t *testing.T) {
	var _ iface.AuditSinkSetter = (*Service)(nil)
	ctx := context.Background()

	err := New(Options{}).DeleteBySubject(ctx, "tA", "crm:42")
	requireIs(t, err, iface.ErrRetrievalNotConfigured)

	env := newErasureEnv(t, nil, nil)
	svc := New(Options{})
	relay := NewAuditRelay()
	svc.SetAuditRelay(relay)
	svc.SetErasure(NewErasureService(env.store, env.fin, func(o string) bool { return o == "crm" }, nil, relay, env.clk.Now, nil))
	sink := &recordingSink{}
	svc.SetAuditSink(sink)
	require.NoError(t, svc.DeleteBySubject(ctx, "tA", "crm:42"))
	assert.Len(t, env.tombstones(models.ErasureKindSubject), 1)
	assert.Len(t, sink.byAction(AuditErasureRecorded), 1)
}

func TestService_AuditSinkSurvivesSetterOrder(t *testing.T) {
	for _, sinkFirst := range []bool{true, false} {
		env := newErasureEnv(t, nil, nil)
		svc := New(Options{})
		relay := NewAuditRelay() // the relay the ErasureService emits through
		svc.SetErasure(NewErasureService(env.store, env.fin, func(o string) bool { return o == "crm" }, nil, relay, env.clk.Now, nil))
		sink := &recordingSink{}
		if sinkFirst {
			svc.SetAuditSink(sink)
			svc.SetAuditRelay(relay)
		} else {
			svc.SetAuditRelay(relay)
			svc.SetAuditSink(sink)
		}
		require.NoError(t, svc.DeleteBySubject(context.Background(), "tA", "crm:42"))
		assert.Equal(t, 1, len(sink.byAction(AuditErasureRecorded)))
	}
}
