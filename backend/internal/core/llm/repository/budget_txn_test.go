package repository

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/orkestra/backend/internal/core/llm/models"
	"github.com/orkestra/backend/pkg/sdk/tenantrepo"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// ensureBudgetIndexes creates the unique indexes the transactions rely on
// (module.Collections declares them; the test DB is created bare).
func ensureBudgetIndexes(t *testing.T, db *mongo.Database) {
	t.Helper()
	for coll, keys := range map[string]bson.D{
		CollUsageMonthly: {{Key: "tenantId", Value: 1}, {Key: "period", Value: 1}},
		CollUsage:        {{Key: "tenantId", Value: 1}, {Key: "uuid", Value: 1}},
		CollBudgets:      {{Key: "tenantId", Value: 1}},
		CollBudgetOutbox: {{Key: "tenantId", Value: 1}, {Key: "period", Value: 1}, {Key: "threshold", Value: 1}},
	} {
		if _, err := db.Collection(coll).Indexes().CreateOne(context.Background(), mongo.IndexModel{Keys: keys, Options: options.Index().SetUnique(true)}); err != nil {
			t.Fatal(err)
		}
	}
}

func pendingEvent(tenant string, i, reserved int) *models.UsageEvent {
	exp := time.Now().UTC().Add(time.Minute)
	return &models.UsageEvent{
		UUID: fmt.Sprintf("u-%s-%d-%d", tenant, i, time.Now().UnixNano()), TenantID: tenant, ModelUUID: "m1", Provider: "openai", ModelID: "gpt",
		CredentialKind: models.CredentialKindOrg, Caller: "test", Purpose: "default", ReservedTokens: reserved, ReservedOutputTokens: reserved / 4,
		ReservationExpiresAt: &exp, Outcome: models.OutcomePending, CreatedAt: time.Now().UTC(),
	}
}

func newStore(db *mongo.Database) *BudgetStore {
	return NewBudgetStore(db, NewUsage(db), NewMonthly(db), NewBudgets(db), NewOutbox(db))
}

func TestReserve_ConcurrentCallersCannotOverspend(t *testing.T) {
	db := newTestDB(t)
	ensureBudgetIndexes(t, db)
	monthly := NewMonthly(db)
	usage := NewUsage(db)
	ctx := ctxFor("t1")
	const limit, request, callers = int64(1000), int64(400), 5
	var wg sync.WaitGroup
	var mu sync.Mutex
	admitted := 0
	for i := 0; i < callers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			err := WithTxn(ctx, db, func(sc mongo.SessionContext) error {
				m, err := monthly.GetForUpdate(sc, "2026-10")
				if err != nil {
					return err
				}
				if m.Consumed()+m.ReservedTokens+request > limit {
					return ErrBudgetExceeded
				}
				if err := monthly.Reserve(sc, "2026-10", request); err != nil {
					return err
				}
				return usage.InsertPending(sc, pendingEvent("t1", i, int(request)))
			})
			if err != nil && !errors.Is(err, ErrBudgetExceeded) {
				t.Errorf("caller %d: %v", i, err)
			}
			mu.Lock()
			if err == nil {
				admitted++
			}
			mu.Unlock()
		}(i)
	}
	wg.Wait()
	if admitted != 2 {
		t.Fatalf("admitted = %d, want 2 (limit 1000, request 400)", admitted)
	}
	m, _ := monthly.Get(ctx, "2026-10")
	if m.ReservedTokens != 800 {
		t.Fatalf("reserved = %d", m.ReservedTokens)
	}
}

// countDocs and findDoc read raw rows of tenant t1 (every row these
// tests assert on belongs to it), scoped like production queries.
func countDocs(t *testing.T, db *mongo.Database, coll string, extra bson.M) int64 {
	t.Helper()
	filter, err := tenantrepo.Scope(ctxFor("t1"), extra)
	if err != nil {
		t.Fatal(err)
	}
	n, err := db.Collection(coll).CountDocuments(context.Background(), filter)
	if err != nil {
		t.Fatal(err)
	}
	return n
}

func findDoc(t *testing.T, db *mongo.Database, coll string, extra bson.M, out any) {
	t.Helper()
	filter, err := tenantrepo.Scope(ctxFor("t1"), extra)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Collection(coll).FindOne(context.Background(), filter).Decode(out); err != nil {
		t.Fatal(err)
	}
}

// Review focus 2: two concurrent reservations on one remaining balance —
// exactly one is admitted under block, the other is refused and ledgered
// budget_blocked (and fires the exhausted alert once, ruling 4.1); under
// warn_only both pass.
func TestBudgetStore_ConcurrentReservationsOnOneBalance(t *testing.T) {
	for _, tc := range []struct {
		action       string
		wantAdmitted int
	}{{models.BudgetActionBlock, 1}, {models.BudgetActionWarnOnly, 2}} {
		t.Run(tc.action, func(t *testing.T) {
			db := newTestDB(t)
			ensureBudgetIndexes(t, db)
			store := newStore(db)
			ctx := ctxFor("t1")
			period := models.PeriodOf(time.Now())
			if err := store.budgets.Put(ctx, &models.Budget{MonthlyTokenLimit: 1000, WarnPct: 80, Action: tc.action}); err != nil {
				t.Fatal(err)
			}
			// 400 already consumed: 600 left, two callers each want 500.
			if err := WithTxn(ctx, db, func(sc mongo.SessionContext) error {
				return store.monthly.Apply(sc, period, models.CredentialKindOrg, 0, 300, 100)
			}); err != nil {
				t.Fatal(err)
			}
			block := tc.action == models.BudgetActionBlock
			var wg sync.WaitGroup
			results := make([]bool, 2)
			for i := range results {
				wg.Add(1)
				go func(i int) {
					defer wg.Done()
					ok, err := store.Reserve(ctx, period, pendingEvent("t1", i, 500), 1000, block)
					if err != nil {
						t.Errorf("reserve %d: %v", i, err)
					}
					results[i] = ok
				}(i)
			}
			wg.Wait()
			admitted := 0
			for _, ok := range results {
				if ok {
					admitted++
				}
			}
			if admitted != tc.wantAdmitted {
				t.Fatalf("admitted = %d, want %d", admitted, tc.wantAdmitted)
			}
			m, _ := store.monthly.Get(ctx, period)
			if m.ReservedTokens != int64(500*tc.wantAdmitted) {
				t.Fatalf("reserved = %d", m.ReservedTokens)
			}
			blocked := countDocs(t, db, CollUsage, bson.M{"outcome": models.OutcomeBudgetBlocked})
			pending := countDocs(t, db, CollUsage, bson.M{"outcome": models.OutcomePending})
			exhausted := countDocs(t, db, CollBudgetOutbox, bson.M{"threshold": models.ThresholdExhausted})
			wantBlocked := int64(2 - tc.wantAdmitted)
			if blocked != wantBlocked || pending != int64(tc.wantAdmitted) || exhausted != wantBlocked {
				t.Fatalf("blocked=%d pending=%d exhausted outbox=%d", blocked, pending, exhausted)
			}
			if block {
				// A second refusal in the same period: still one alert.
				if ok, err := store.Reserve(ctx, period, pendingEvent("t1", 9, 500), 1000, true); ok || err != nil {
					t.Fatalf("third reserve = %v, %v", ok, err)
				}
				if n := countDocs(t, db, CollBudgetOutbox, bson.M{"threshold": models.ThresholdExhausted}); n != 1 {
					t.Fatalf("exhausted outbox rows = %d", n)
				}
				if m, _ := store.monthly.Get(ctx, period); m.ExhaustedAt == nil {
					t.Fatal("exhaustedAt not marked")
				}
			}
		})
	}
}

func TestBudgetStore_FinalizeAppliesOnceAndSplitsByKind(t *testing.T) {
	db := newTestDB(t)
	ensureBudgetIndexes(t, db)
	store := newStore(db)
	ctx := ctxFor("t1")
	period := models.PeriodOf(time.Now())
	ev := pendingEvent("t1", 1, 300)
	ev.CredentialKind = models.CredentialKindUserAccount
	if ok, err := store.Reserve(ctx, period, ev, 0, false); !ok || err != nil {
		t.Fatalf("reserve without budget = %v, %v", ok, err)
	}
	upd := models.FinalizeUpdate{Outcome: models.OutcomeOK, InputTokens: 120, OutputTokens: 30, LatencyMs: 5, FinalizedAt: time.Now().UTC(),
		Attempts: []models.Attempt{{Provider: "openai", CredentialKind: models.CredentialKindUserAccount, Outcome: "ok", InputTokens: 120, OutputTokens: 30}}}
	crossed, err := store.Finalize(ctx, period, ev.UUID, upd)
	if err != nil || len(crossed) != 0 {
		t.Fatalf("finalize = %v, %v", crossed, err)
	}
	if _, err := store.Finalize(ctx, period, ev.UUID, upd); !errors.Is(err, ErrAlreadyFinalized) {
		t.Fatalf("second finalize err = %v", err)
	}
	m, _ := store.monthly.Get(ctx, period)
	if m.UserAccountInputTokens != 120 || m.UserAccountOutputTokens != 30 || m.OrgInputTokens != 0 || m.ReservedTokens != 0 || m.Requests != 1 {
		t.Fatalf("monthly = %+v", m)
	}
	var row models.UsageEvent
	findDoc(t, db, CollUsage, bson.M{"uuid": ev.UUID}, &row)
	if row.Outcome != models.OutcomeOK || row.ReservedTokens != 0 || row.FinalizedAt == nil || len(row.Attempts) != 1 || row.InputTokens != 120 {
		t.Fatalf("row = %+v", row)
	}
	// Another tenant cannot finalize t1's row.
	ev2 := pendingEvent("t1", 2, 10)
	if ok, err := store.Reserve(ctx, period, ev2, 0, false); !ok || err != nil {
		t.Fatal(err)
	}
	if _, err := store.Finalize(ctxFor("t2"), period, ev2.UUID, upd); !errors.Is(err, ErrAlreadyFinalized) {
		t.Fatalf("cross-tenant finalize err = %v", err)
	}
}

// Review focus 4 (store half): several finalizations crossing the warn
// threshold at once produce exactly one outbox event.
func TestBudgetStore_ConcurrentFinalizationsCrossWarnOnce(t *testing.T) {
	db := newTestDB(t)
	ensureBudgetIndexes(t, db)
	store := newStore(db)
	ctx := ctxFor("t1")
	period := models.PeriodOf(time.Now())
	if err := store.budgets.Put(ctx, &models.Budget{MonthlyTokenLimit: 10000, WarnPct: 50, Action: models.BudgetActionBlock}); err != nil {
		t.Fatal(err)
	}
	const n = 6
	evs := make([]*models.UsageEvent, n)
	for i := range evs {
		evs[i] = pendingEvent("t1", i, 1000)
		if ok, err := store.Reserve(ctx, period, evs[i], 10000, true); !ok || err != nil {
			t.Fatalf("reserve %d = %v, %v", i, ok, err)
		}
	}
	var wg sync.WaitGroup
	var mu sync.Mutex
	crossings := 0
	for i := range evs {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			// 6 × 1000 = 6000 ≥ 50% of 10000: crossed by the 5th, whichever it is.
			c, err := store.Finalize(ctx, period, evs[i].UUID, models.FinalizeUpdate{Outcome: models.OutcomeOK, InputTokens: 900, OutputTokens: 100, FinalizedAt: time.Now().UTC()})
			if err != nil {
				t.Errorf("finalize %d: %v", i, err)
			}
			mu.Lock()
			crossings += len(c)
			mu.Unlock()
		}(i)
	}
	wg.Wait()
	if crossings != 1 {
		t.Fatalf("crossings reported = %d", crossings)
	}
	if got := countDocs(t, db, CollBudgetOutbox, bson.M{}); got != 1 {
		t.Fatalf("outbox rows = %d, want 1", got)
	}
	var ev models.OutboxEvent
	findDoc(t, db, CollBudgetOutbox, bson.M{}, &ev)
	if ev.Threshold != models.ThresholdWarn || ev.Status != models.OutboxStatusPending || ev.TenantID != "t1" || ev.Period != period {
		t.Fatalf("outbox = %+v", ev)
	}
	m, _ := store.monthly.Get(ctx, period)
	if m.Consumed() != 6000 || m.ReservedTokens != 0 || m.WarnedAt == nil || m.ExhaustedAt != nil {
		t.Fatalf("monthly = %+v", m)
	}
}

func TestOutbox_LeaseIsExclusiveAndRecoverable(t *testing.T) {
	db := newTestDB(t)
	ensureBudgetIndexes(t, db)
	ob := NewOutbox(db)
	ctx := ctxFor("t1")
	now := time.Now().UTC()
	ev := &models.OutboxEvent{UUID: "ob-1", Period: "2026-10", Threshold: models.ThresholdWarn, Status: models.OutboxStatusPending, NextAttemptAt: now, CreatedAt: now}
	if ok, err := ob.InsertUnique(ctx, ev); !ok || err != nil {
		t.Fatalf("insert = %v, %v", ok, err)
	}
	if ok, err := ob.InsertUnique(ctx, &models.OutboxEvent{UUID: "ob-2", Period: "2026-10", Threshold: models.ThresholdWarn, Status: models.OutboxStatusPending, NextAttemptAt: now, CreatedAt: now}); ok || err != nil {
		t.Fatalf("duplicate insert = %v, %v", ok, err)
	}
	got, err := ob.LeaseDue(context.Background(), now, time.Minute)
	if err != nil || got == nil || got.UUID != "ob-1" || got.Status != models.OutboxStatusSending || got.TenantID != "t1" {
		t.Fatalf("lease = %+v, %v", got, err)
	}
	// Leased: invisible to a second worker until the lease expires.
	if again, err := ob.LeaseDue(context.Background(), now, time.Minute); again != nil || err != nil {
		t.Fatalf("second lease = %+v, %v", again, err)
	}
	// The holder crashed: after expiry the event is leased again.
	later := now.Add(2 * time.Minute)
	again, err := ob.LeaseDue(context.Background(), later, time.Minute)
	if err != nil || again == nil || again.UUID != "ob-1" {
		t.Fatalf("lease after expiry = %+v, %v", again, err)
	}
	// The first holder's lease was taken over: its late settle is fenced
	// out and leaves the new holder's lease intact.
	if err := ob.MarkSent(ctx, "ob-1", *got.LeaseUntil); !errors.Is(err, ErrLeaseLost) {
		t.Fatalf("stale MarkSent err = %v", err)
	}
	var cur models.OutboxEvent
	findDoc(t, db, CollBudgetOutbox, bson.M{"uuid": "ob-1"}, &cur)
	if cur.Status != models.OutboxStatusSending || cur.LeaseUntil == nil || !cur.LeaseUntil.Equal(*again.LeaseUntil) {
		t.Fatalf("after stale MarkSent = %+v", cur)
	}
	if err := ob.Reschedule(ctx, "ob-1", *got.LeaseUntil, 1, later); !errors.Is(err, ErrLeaseLost) {
		t.Fatalf("stale Reschedule err = %v", err)
	}
	if err := ob.MarkSent(ctx, "ob-1", *again.LeaseUntil); err != nil {
		t.Fatal(err)
	}
	if done, err := ob.LeaseDue(context.Background(), later.Add(time.Hour), time.Minute); done != nil || err != nil {
		t.Fatalf("lease after sent = %+v, %v", done, err)
	}
	// Settling is tenant-scoped: t1 cannot settle t2's leased event.
	t2ev := &models.OutboxEvent{UUID: "ob-t2", Period: "2026-10", Threshold: models.ThresholdWarn, Status: models.OutboxStatusPending, NextAttemptAt: now, CreatedAt: now}
	if ok, err := ob.InsertUnique(ctxFor("t2"), t2ev); !ok || err != nil {
		t.Fatalf("t2 insert = %v, %v", ok, err)
	}
	t2lease, err := ob.LeaseDue(context.Background(), later.Add(time.Hour), time.Hour)
	if err != nil || t2lease == nil || t2lease.UUID != "ob-t2" {
		t.Fatalf("t2 lease = %+v, %v", t2lease, err)
	}
	if err := ob.MarkSent(ctx, "ob-t2", *t2lease.LeaseUntil); !errors.Is(err, ErrLeaseLost) {
		t.Fatalf("cross-tenant MarkSent err = %v", err)
	}
	t2filter, err := tenantrepo.Scope(ctxFor("t2"), bson.M{"uuid": "ob-t2"})
	if err != nil {
		t.Fatal(err)
	}
	var t2cur models.OutboxEvent
	if err := db.Collection(CollBudgetOutbox).FindOne(context.Background(), t2filter).Decode(&t2cur); err != nil {
		t.Fatal(err)
	}
	if t2cur.Status != models.OutboxStatusSending || t2cur.LeaseUntil == nil {
		t.Fatalf("t2 event after t1's MarkSent = %+v", t2cur)
	}
}

func TestUsage_QueryUserMonthAndSweeps(t *testing.T) {
	db := newTestDB(t)
	ensureBudgetIndexes(t, db)
	usage := NewUsage(db)
	ctx := ctxFor("t1")
	now := time.Now().UTC()
	fin := now
	rows := []*models.UsageEvent{
		{UUID: "a", UserUUID: "u1", ModelUUID: "m1", Caller: "x", CredentialKind: models.CredentialKindOrg, Outcome: models.OutcomeOK, InputTokens: 10, OutputTokens: 5, CreatedAt: now, FinalizedAt: &fin},
		{UUID: "b", UserUUID: "u1", ModelUUID: "m2", Caller: "x", CredentialKind: models.CredentialKindUserAccount, Outcome: models.OutcomeFallback, InputTokens: 20, OutputTokens: 7, CreatedAt: now, FinalizedAt: &fin},
		{UUID: "c", UserUUID: "u2", ModelUUID: "m1", Caller: "y", CredentialKind: models.CredentialKindOrg, Outcome: models.OutcomeError, InputTokens: 3, OutputTokens: 1, CreatedAt: now, FinalizedAt: &fin},
		{UUID: "d", UserUUID: "u1", ModelUUID: "m1", Caller: "x", CredentialKind: models.CredentialKindOrg, Outcome: models.OutcomeBudgetBlocked, CreatedAt: now, FinalizedAt: &fin},
		{UUID: "e", UserUUID: "u1", ModelUUID: "m1", Caller: "x", CredentialKind: models.CredentialKindOrg, Outcome: models.OutcomePending, ReservedTokens: 50, CreatedAt: now},
	}
	for _, r := range rows {
		if err := usage.InsertPending(ctx, r); err != nil {
			t.Fatal(err)
		}
	}
	other := &models.UsageEvent{UUID: "z", UserUUID: "u1", ModelUUID: "m1", Caller: "x", CredentialKind: models.CredentialKindOrg, Outcome: models.OutcomeOK, InputTokens: 1000, CreatedAt: now}
	if err := usage.InsertPending(ctxFor("t2"), other); err != nil {
		t.Fatal(err)
	}
	got, err := usage.Query(ctx, now.Add(-time.Hour), now.Add(time.Hour), "modelUuid")
	if err != nil {
		t.Fatal(err)
	}
	byKey := map[string]models.LLMUsageRow{}
	for _, r := range got {
		byKey[r.Key] = r
	}
	// Finalized rows with tokens (error included, ruling 5.7); pending and
	// budget_blocked excluded; t2 invisible.
	if len(got) != 2 || byKey["m1"].Requests != 2 || byKey["m1"].InputTokens != 13 || byKey["m2"].OutputTokens != 7 {
		t.Fatalf("query = %+v", got)
	}
	if _, err := usage.Query(ctx, now.Add(-time.Hour), now, "prompt"); err == nil {
		t.Fatal("unknown groupBy accepted")
	}
	self, err := usage.UserMonth(ctx, "u1", models.PeriodOf(now))
	if err != nil {
		t.Fatal(err)
	}
	if self.OrgInputTokens != 10 || self.UserAccountInputTokens != 20 || self.UserAccountOutputTokens != 7 || self.Requests != 2 {
		t.Fatalf("self = %+v", self)
	}
	// The reconciler sweep crosses tenants and returns only expired pending rows.
	exp := now.Add(-time.Second)
	late := &models.UsageEvent{UUID: "p2", ModelUUID: "m1", CredentialKind: models.CredentialKindOrg, Outcome: models.OutcomePending, ReservedTokens: 9, ReservationExpiresAt: &exp, CreatedAt: now}
	if err := usage.InsertPending(ctxFor("t2"), late); err != nil {
		t.Fatal(err)
	}
	pend, err := usage.ListPendingExpired(context.Background(), now, 10)
	if err != nil || len(pend) != 1 || pend[0].UUID != "p2" || pend[0].TenantID != "t2" {
		t.Fatalf("pending expired = %+v, %v", pend, err)
	}
	// Retention: finalized rows older than the cutoff go, pending stay.
	n, err := usage.PurgeBefore(context.Background(), now.Add(time.Hour))
	if err != nil || n != 5 {
		t.Fatalf("purge = %d, %v", n, err)
	}
	// t1 keeps only its pending row (t2's pending p2 also survives; the
	// purge count above already covered both orgs).
	if left := countDocs(t, db, CollUsage, bson.M{}); left != 1 {
		t.Fatalf("left after purge = %d", left)
	}
}

func TestUsage_DSRAndMonthlyPurge(t *testing.T) {
	db := newTestDB(t)
	ensureBudgetIndexes(t, db)
	usage := NewUsage(db)
	monthly := NewMonthly(db)
	now := time.Now().UTC()
	for i, tenant := range []string{"t1", "t2"} {
		if err := usage.InsertPending(ctxFor(tenant), &models.UsageEvent{UUID: fmt.Sprintf("r%d", i), UserUUID: "subject", CredentialUUID: "acc", Outcome: models.OutcomeOK, CreatedAt: now}); err != nil {
			t.Fatal(err)
		}
	}
	if err := usage.InsertPending(ctxFor("t1"), &models.UsageEvent{UUID: "keep", UserUUID: "someone", Outcome: models.OutcomeOK, CreatedAt: now}); err != nil {
		t.Fatal(err)
	}
	// An in-flight call of the subject: its reservation is still counted in
	// the rollup, so erasure must leave it to the reconciler.
	if err := usage.InsertPending(ctxFor("t1"), &models.UsageEvent{UUID: "inflight", UserUUID: "anon-x", CredentialUUID: "acc2", Outcome: models.OutcomePending, ReservedTokens: 40, CreatedAt: now}); err != nil {
		t.Fatal(err)
	}
	rows, err := usage.ListByUserAllTenants(context.Background(), "subject")
	if err != nil || len(rows) != 2 {
		t.Fatalf("export = %d, %v", len(rows), err)
	}
	if n, err := usage.AnonymizeByUser(context.Background(), "subject", "anon-x"); err != nil || n != 2 {
		t.Fatalf("anonymize = %d, %v", n, err)
	}
	// One of the two (cross-org count checked above) is t1's.
	if n := countDocs(t, db, CollUsage, bson.M{"userUuid": "anon-x", "credentialUuid": bson.M{"$exists": false}}); n != 1 {
		t.Fatalf("anonymized rows = %d", n)
	}
	if n, err := usage.DeleteByUser(context.Background(), "anon-x"); err != nil || n != 2 {
		t.Fatalf("delete = %d, %v", n, err)
	}
	if n := countDocs(t, db, CollUsage, bson.M{}); n != 2 {
		t.Fatalf("left = %d (want keep + inflight)", n)
	}
	if n := countDocs(t, db, CollUsage, bson.M{"uuid": "inflight", "outcome": models.OutcomePending}); n != 1 {
		t.Fatal("erasure deleted a pending row")
	}
	for _, p := range []string{"2023-01", models.PeriodOf(now)} {
		if err := WithTxn(ctxFor("t1"), db, func(sc mongo.SessionContext) error { return monthly.Reserve(sc, p, 1) }); err != nil {
			t.Fatal(err)
		}
	}
	if n, err := monthly.PurgeOlderThan(context.Background(), 24); err != nil || n != 1 {
		t.Fatalf("monthly purge = %d, %v", n, err)
	}
}

func TestBudgets_GetPut(t *testing.T) {
	db := newTestDB(t)
	ensureBudgetIndexes(t, db)
	b := NewBudgets(db)
	if _, err := b.Get(ctxFor("t1")); !errors.Is(err, ErrNotFound) {
		t.Fatalf("get absent = %v", err)
	}
	for _, limit := range []int64{100, 200} {
		if err := b.Put(ctxFor("t1"), &models.Budget{MonthlyTokenLimit: limit, WarnPct: 80, Action: models.BudgetActionBlock, UpdatedBy: "u"}); err != nil {
			t.Fatal(err)
		}
	}
	got, err := b.Get(ctxFor("t1"))
	if err != nil || got.MonthlyTokenLimit != 200 || got.TenantID != "t1" {
		t.Fatalf("get = %+v, %v", got, err)
	}
	if _, err := b.Get(ctxFor("t2")); !errors.Is(err, ErrNotFound) {
		t.Fatalf("t2 sees t1 budget: %v", err)
	}
}

// TestOutbox_PurgeSettledBefore deletes only sent/failed rows older than
// the cutoff, across orgs; pending and sending rows always stay.
func TestOutbox_PurgeSettledBefore(t *testing.T) {
	db := newTestDB(t)
	ensureBudgetIndexes(t, db)
	ob := NewOutbox(db)
	old := time.Now().UTC().Add(-48 * time.Hour)
	fresh := time.Now().UTC()
	rows := []struct {
		tenant, uuid, period, status string
		created                      time.Time
	}{
		{"t1", "old-sent", "2026-01", models.OutboxStatusSent, old},
		{"t2", "old-failed", "2026-01", models.OutboxStatusFailed, old},
		{"t1", "old-pending", "2026-02", models.OutboxStatusPending, old},
		{"t1", "old-sending", "2026-03", models.OutboxStatusSending, old},
		{"t1", "new-sent", "2026-04", models.OutboxStatusSent, fresh},
	}
	for _, r := range rows {
		if ok, err := ob.InsertUnique(ctxFor(r.tenant), &models.OutboxEvent{UUID: r.uuid, Period: r.period, Threshold: models.ThresholdWarn, Status: r.status, NextAttemptAt: r.created, CreatedAt: r.created}); !ok || err != nil {
			t.Fatalf("insert %s: %v %v", r.uuid, ok, err)
		}
	}
	n, err := ob.PurgeSettledBefore(context.Background(), time.Now().UTC().Add(-24*time.Hour))
	if err != nil || n != 2 {
		t.Fatalf("purged = %d, %v; want 2", n, err)
	}
	if left := countDocs(t, db, CollBudgetOutbox, bson.M{}); left != 3 {
		t.Fatalf("t1 kept = %d, want 3 (pending, sending, fresh sent)", left)
	}
	t2, err := tenantrepo.Scope(ctxFor("t2"), bson.M{})
	if err != nil {
		t.Fatal(err)
	}
	if n, err := db.Collection(CollBudgetOutbox).CountDocuments(context.Background(), t2); err != nil || n != 0 {
		t.Fatalf("t2 kept = %d, %v; want 0", n, err)
	}
}

// MarkDelivered is fenced by the lease like the settles: a stale holder
// cannot record progress on an event another worker took over.
func TestOutbox_MarkDeliveredIsLeaseFenced(t *testing.T) {
	db := newTestDB(t)
	ensureBudgetIndexes(t, db)
	ob := NewOutbox(db)
	ctx := ctxFor("t1")
	now := time.Now().UTC()
	if ok, err := ob.InsertUnique(ctx, &models.OutboxEvent{UUID: "ob-d", Period: "2026-10", Threshold: models.ThresholdWarn, Status: models.OutboxStatusPending, NextAttemptAt: now, CreatedAt: now}); !ok || err != nil {
		t.Fatal(ok, err)
	}
	ev, err := ob.LeaseDue(context.Background(), now, time.Minute)
	if err != nil || ev == nil {
		t.Fatal(ev, err)
	}
	for _, u := range []string{"u1", "u2", "u1"} {
		if err := ob.MarkDelivered(ctx, ev.UUID, *ev.LeaseUntil, u); err != nil {
			t.Fatal(err)
		}
	}
	if err := ob.MarkDelivered(ctxFor("t2"), ev.UUID, *ev.LeaseUntil, "u3"); !errors.Is(err, ErrLeaseLost) {
		t.Fatalf("cross-tenant MarkDelivered err = %v", err)
	}
	if err := ob.MarkDelivered(ctx, ev.UUID, ev.LeaseUntil.Add(time.Second), "u3"); !errors.Is(err, ErrLeaseLost) {
		t.Fatalf("stale-lease MarkDelivered err = %v", err)
	}
	var got models.OutboxEvent
	findDoc(t, db, CollBudgetOutbox, bson.M{"uuid": "ob-d"}, &got)
	if len(got.DeliveredTo) != 2 || got.DeliveredTo[0] != "u1" || got.DeliveredTo[1] != "u2" || got.Status != models.OutboxStatusSending {
		t.Fatalf("event = %+v", got)
	}
}
