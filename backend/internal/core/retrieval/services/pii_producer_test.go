package services

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/orkestra/backend/internal/core/retrieval/engine"
	"github.com/orkestra/backend/internal/core/retrieval/engine/enginefake"
	"github.com/orkestra/backend/internal/core/retrieval/models"
	"github.com/orkestra/backend/pkg/sdk/iface"
)

// rowLogs is an in-memory SearchLogDSRRepo that really lists and deletes.
type rowLogs struct {
	mu        sync.Mutex
	rows      []models.SearchLog
	calls     []string
	deleteErr error // returned by DeleteByUserAllTenants when set
	// deletedBeforeErr is the partial count DeleteByUserAllTenants returns
	// with deleteErr (the batches deleteInBatches committed before failing).
	deletedBeforeErr int64
}

var _ SearchLogDSRRepo = (*rowLogs)(nil)

func (r *rowLogs) mine(row models.SearchLog, u string) bool {
	return row.UserUUID == u || slices.Contains(row.Subjects, "user:"+u)
}

func (r *rowLogs) ListByUserAllTenants(_ context.Context, u string) ([]models.SearchLog, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.calls = append(r.calls, "list")
	var out []models.SearchLog
	for _, row := range r.rows {
		if r.mine(row, u) {
			out = append(out, row)
		}
	}
	return out, nil
}

func (r *rowLogs) DeleteByUserAllTenants(_ context.Context, u string) (int64, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.calls = append(r.calls, "delete")
	if r.deleteErr != nil {
		return r.deletedBeforeErr, r.deleteErr
	}
	var keep []models.SearchLog
	var n int64
	for _, row := range r.rows {
		if r.mine(row, u) {
			n++
		} else {
			keep = append(keep, row)
		}
	}
	r.rows = keep
	return n, nil
}

func (r *rowLogs) DeleteBySubject(context.Context, string, string) (int64, error) { return 0, nil }
func (r *rowLogs) DeleteByTenant(context.Context, string) (int64, error)          { return 0, nil }

func (r *rowLogs) callLog() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return slices.Clone(r.calls)
}

type producerEnv struct {
	clk   *clock
	t0    time.Time
	store *memStore
	logs  *rowLogs
	eng   *enginefake.Engine
	fin   *Finalizer
	prod  *PIIProducer
}

func newProducerEnv(t *testing.T, cols map[string][]string, withEngine bool) *producerEnv {
	t.Helper()
	clk := newClock()
	env := &producerEnv{clk: clk, t0: clk.Now(), store: newMemStore(), logs: &rowLogs{}}
	var eng engine.Engine
	if withEngine {
		env.eng = enginefake.New()
		eng = env.eng
	}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	env.fin = NewFinalizer(env.store, env.logs, &staticLister{cols: cols}, eng, testFinalizerConfig, NewAuditRelay(), clk.Now, logger)
	env.prod = NewPIIProducer(env.store, env.logs, env.fin, clk.Now, logger)
	return env
}

func TestPIIProducer_Subject(t *testing.T) {
	require.Equal(t, "retrieval", newProducerEnv(t, nil, false).prod.Subject())
}

func TestPIIProducer_RefusesEmptySubject(t *testing.T) {
	env := newProducerEnv(t, nil, false)
	_, err := env.prod.ExportPersonalData(context.Background(), "")
	requireIs(t, err, ErrErasureEmptySubject)
	_, err = env.prod.PurgePersonalData(context.Background(), "", iface.EraseHardDelete)
	requireIs(t, err, ErrErasureEmptySubject)
	require.Empty(t, env.store.rows)
	require.Empty(t, env.logs.callLog())
}

func TestPIIProducer_ExportProjection(t *testing.T) {
	env := newProducerEnv(t, nil, false)
	at := env.t0
	env.logs.rows = []models.SearchLog{
		{TenantID: "tA", UserUUID: "u1", Corpus: "people", Query: "head of sales", FilterJSON: `{"field":"team","op":"eq","value":"emea"}`,
			SourceIDs: []string{"s1", "s2"}, HitCount: 2, Subjects: []string{"user:u9"}, CreatedAt: at},
		{TenantID: "tB", UserUUID: "u2", Corpus: "people", Query: "mario", Subjects: []string{"user:u1"}, CreatedAt: at},
	}
	export, err := env.prod.ExportPersonalData(context.Background(), "u1")
	require.NoError(t, err)
	out := export.(*PersonalDataExport)
	require.Equal(t, []ExportedSearch{{
		TenantID: "tA", Corpus: "people", Query: "head of sales",
		Filter:      map[string]any{"field": "team", "op": "eq", "value": "emea"},
		ResultCount: 2, At: at,
	}}, out.Searches)
	require.Equal(t, []ExportedAppearance{{TenantID: "tB", Corpus: "people", At: at}}, out.AppearedInResults)
	raw, err := json.Marshal(out)
	require.NoError(t, err)
	for _, leak := range []string{"u9", "u2", "mario", "s1"} {
		if strings.Contains(string(raw), leak) {
			t.Errorf("export leaks %q", leak)
		}
	}
}

func TestPIIProducer_ExportKeepsLargeIntegersExact(t *testing.T) {
	env := newProducerEnv(t, nil, false)
	env.logs.rows = []models.SearchLog{{TenantID: "tA", UserUUID: "u1", Corpus: "people", CreatedAt: env.t0,
		FilterJSON: `{"field":"seniority","op":"eq","value":9007199254740993}`}}
	export, err := env.prod.ExportPersonalData(context.Background(), "u1")
	require.NoError(t, err)
	raw, err := json.Marshal(export)
	require.NoError(t, err)
	require.Contains(t, string(raw), "9007199254740993")
}

func TestPIIProducer_ExportUndecodableFilterIsOmitted(t *testing.T) {
	env := newProducerEnv(t, nil, false)
	env.logs.rows = []models.SearchLog{{TenantID: "tA", UserUUID: "u1", Corpus: "people", CreatedAt: env.t0, FilterJSON: `{not json`}}
	export, err := env.prod.ExportPersonalData(context.Background(), "u1")
	require.NoError(t, err)
	require.Nil(t, export.(*PersonalDataExport).Searches[0].Filter)
}

func TestPIIProducer_ExportNothingHeld(t *testing.T) {
	env := newProducerEnv(t, nil, false)
	export, err := env.prod.ExportPersonalData(context.Background(), "u1")
	require.NoError(t, err)
	require.Nil(t, export)
}

func TestPIIProducer_PurgePendingUntilFinalized(t *testing.T) {
	ctx := context.Background()
	env := newProducerEnv(t, map[string][]string{"people": {"c_people_g1"}}, true)
	env.eng.Seed("c_people_g1", finPoint("p1", "tA", "s1", "user:u1"))
	env.logs.rows = []models.SearchLog{
		{TenantID: "tA", UserUUID: "u1", CreatedAt: env.t0},
		{TenantID: "tB", UserUUID: "u2", Subjects: []string{"user:u1"}, CreatedAt: env.t0},
	}

	r1, err := env.prod.PurgePersonalData(ctx, "u1", iface.EraseAnonymize)
	require.NoError(t, err)
	require.Equal(t, true, r1.Pending)
	require.Equal(t, 2, r1.RowsDeleted)
	require.Equal(t, []string{"retrieval_search_log", "retrieval_index"}, r1.Collections)
	rows, _ := env.store.ListUser(ctx, "u1")
	require.Len(t, rows, 1)
	require.Equal(t, models.ErasureStatusFencing, rows[0].Status)
	require.Empty(t, env.eng.Points("c_people_g1"))

	env.clk.Advance(time.Minute)
	r2, err := env.prod.PurgePersonalData(ctx, "u1", iface.EraseHardDelete)
	require.NoError(t, err)
	require.Equal(t, true, r2.Pending)
	require.Equal(t, 0, r2.RowsDeleted)
	rows, _ = env.store.ListUser(ctx, "u1")
	require.Len(t, rows, 1)

	env.clk.Advance(2 * time.Minute)
	n, err := env.fin.RunOnce(ctx)
	require.NoError(t, err)
	require.Equal(t, 1, n)

	r3, err := env.prod.PurgePersonalData(ctx, "u1", iface.EraseHardDelete)
	require.NoError(t, err)
	require.Equal(t, false, r3.Pending)
}

func TestPIIProducer_PurgeNoEngineNoCollectionsNotPending(t *testing.T) {
	env := newProducerEnv(t, map[string][]string{}, false)
	res, err := env.prod.PurgePersonalData(context.Background(), "u1", iface.EraseHardDelete)
	require.NoError(t, err)
	require.Equal(t, false, res.Pending)
}

func TestPIIProducer_PurgeEngineDownStillRecordsTombstone(t *testing.T) {
	ctx := context.Background()
	env := newProducerEnv(t, map[string][]string{"people": {"c_people_g1"}}, true)
	env.eng.Seed("c_people_g1", finPoint("p1", "tA", "s1", "user:u1"))
	env.eng.FailDeletes(engine.ErrUnavailable)
	res, err := env.prod.PurgePersonalData(ctx, "u1", iface.EraseHardDelete)
	require.NoError(t, err)
	require.Equal(t, true, res.Pending)
	rows, _ := env.store.ListUser(ctx, "u1")
	require.Len(t, rows, 1)
	require.Equal(t, models.ErasureStatusPending, rows[0].EffectiveStatus())
}

// TestPIIProducer_PurgeReturnsLogDeleteError: a failed search-log delete is
// returned and nothing reports done, also on a repeat erasure whose user
// tombstone is already done (no "done on faith").
func TestPIIProducer_PurgeReturnsLogDeleteError(t *testing.T) {
	ctx := context.Background()
	boom := errors.New("mongo down")
	for name, done := range map[string]bool{"first": false, "repeat after done": true} {
		t.Run(name, func(t *testing.T) {
			env := newProducerEnv(t, map[string][]string{}, false)
			if done {
				env.store.put(models.Erasure{UUID: "e-done", Scope: models.ErasureScopeGlobal, Kind: models.ErasureKindUser,
					Subject: "user:u1", Status: models.ErasureStatusDone, DoneAt: timePtr(env.t0)})
			}
			env.logs.deleteErr = boom
			res, err := env.prod.PurgePersonalData(ctx, "u1", iface.EraseHardDelete)
			requireIs(t, err, boom)
			require.Equal(t, iface.PurgeResult{}, res)
			rows, _ := env.store.ListUser(ctx, "u1")
			require.Len(t, rows, 1)
			if !done {
				// no inline pass ran: the tombstone is still pending
				require.Equal(t, models.ErasureStatusPending, rows[0].EffectiveStatus())
			}
		})
	}
}

// TestPIIProducer_PurgeLogDeleteErrorKeepsPartialCount (G3): a search-log
// delete that committed batches before failing reports them with the error,
// so compliance's running count keeps the rows a retry can no longer see.
func TestPIIProducer_PurgeLogDeleteErrorKeepsPartialCount(t *testing.T) {
	env := newProducerEnv(t, map[string][]string{}, false)
	boom := errors.New("mongo down mid-batch")
	env.logs.deleteErr = boom
	env.logs.deletedBeforeErr = 3
	res, err := env.prod.PurgePersonalData(context.Background(), "u1", iface.EraseHardDelete)
	requireIs(t, err, boom)
	require.Equal(t, iface.PurgeResult{RowsDeleted: 3, Collections: []string{"retrieval_search_log"}}, res)
}

// listUserFailStore fails ListUser, the read after the inline pass.
type listUserFailStore struct {
	*memStore
	err error
}

func (l listUserFailStore) ListUser(context.Context, string) ([]models.Erasure, error) {
	return nil, l.err
}

// TestPIIProducer_PurgeListUserErrorKeepsDeletedCount (G3): the search-log
// rows deleted before a failing ListUser are reported with the error.
func TestPIIProducer_PurgeListUserErrorKeepsDeletedCount(t *testing.T) {
	clk := newClock()
	boom := errors.New("list failed")
	store := listUserFailStore{memStore: newMemStore(), err: boom}
	logs := &rowLogs{rows: []models.SearchLog{
		{TenantID: "tA", UserUUID: "u1", CreatedAt: clk.Now()},
		{TenantID: "tB", UserUUID: "u2", Subjects: []string{"user:u1"}, CreatedAt: clk.Now()},
		{TenantID: "tB", UserUUID: "u2", CreatedAt: clk.Now()},
	}}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	fin := NewFinalizer(store, logs, &staticLister{cols: map[string][]string{}}, nil, testFinalizerConfig, NewAuditRelay(), clk.Now, logger)
	prod := NewPIIProducer(store, logs, fin, clk.Now, logger)
	res, err := prod.PurgePersonalData(context.Background(), "u1", iface.EraseHardDelete)
	requireIs(t, err, boom)
	require.Equal(t, iface.PurgeResult{RowsDeleted: 2, Collections: []string{"retrieval_search_log", "retrieval_index"}}, res)
}

// claimOneFailStore fails ClaimOne with an error whose text must never be logged.
type claimOneFailStore struct{ *memStore }

func (c claimOneFailStore) ClaimOne(context.Context, string, time.Time, time.Duration) (*models.Erasure, error) {
	return nil, errors.New("store failure naming user:u1 secret-text")
}

// TestPIIProducer_InlinePassFailureLogsReasonOnly (M2): the WARN carries the
// erasure uuid and a coarse reason, never the error text.
func TestPIIProducer_InlinePassFailureLogsReasonOnly(t *testing.T) {
	clk := newClock()
	store := claimOneFailStore{newMemStore()}
	logs := &lockedBuffer{}
	logger := slog.New(slog.NewTextHandler(logs, nil))
	fin := NewFinalizer(store, &rowLogs{}, &staticLister{cols: map[string][]string{}}, nil, testFinalizerConfig, NewAuditRelay(), clk.Now, logger)
	prod := NewPIIProducer(store, &rowLogs{}, fin, clk.Now, logger)
	res, err := prod.PurgePersonalData(context.Background(), "u1", iface.EraseHardDelete)
	require.NoError(t, err)
	require.Equal(t, true, res.Pending)
	out := logs.String()
	require.Contains(t, out, "inline erasure pass failed")
	require.Contains(t, out, "reason=error")
	for _, secret := range []string{"secret-text", "user:u1"} {
		require.Equal(t, false, strings.Contains(out, secret), secret)
	}
}
