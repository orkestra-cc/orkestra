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
// countAll counts across every org (DSR methods are cross-tenant).
func countAll(t *testing.T, db *mongo.Database, coll string, filter bson.M) int64 {
	t.Helper()
	//tenantscope:allow test: asserts the outcome of cross-tenant DSR methods across every org of a throwaway database
	n, err := db.Collection(coll).CountDocuments(context.Background(), filter)
	if err != nil {
		t.Fatal(err)
	}
	return n
}

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
					ok, err := store.Reserve(ctx, period, pendingEvent("t1", i, 500), 1000, 80, block)
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
				if ok, err := store.Reserve(ctx, period, pendingEvent("t1", 9, 500), 1000, 80, true); ok || err != nil {
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

// Final review I2 / ruling 4.1 refined: a refusal fires the period's
// single exhausted alert only once consumed + reserved already reach the
// warn threshold (or the limit when warnPct is 0). One oversized request
// at 10% is ledgered budget_blocked without burning the alert; a refusal
// near the limit still sends it, once. A request larger than the whole
// limit can never be admitted, so it fires the separate oversized alert
// instead: otherwise a budget smaller than one call blocks the org
// without telling anyone.
func TestBudgetStore_RefusalAlertsOnlyNearTheLimit(t *testing.T) {
	for _, tc := range []struct {
		name               string
		warnPct            int
		consumed, reserved int64 // before the refused request
		request            int
		wantAlert          bool
		wantOversized      bool
	}{
		{"request over the whole limit at 10%", 80, 100, 0, 5000, false, true},
		{"request over the whole limit with nothing used", 80, 0, 0, 1001, false, true},
		{"request of the whole limit with nothing used", 80, 0, 1, 1000, false, false},
		{"just under the warn threshold", 80, 700, 99, 500, false, false},
		{"consumed + reserved at the warn threshold", 80, 700, 100, 500, true, false},
		{"over the whole limit at the warn threshold", 80, 800, 0, 5000, true, false},
		{"warnPct 0, under the limit", 0, 900, 0, 500, false, false},
		{"warnPct 0, at the limit", 0, 990, 10, 1, true, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			db := newTestDB(t)
			ensureBudgetIndexes(t, db)
			store := newStore(db)
			ctx := ctxFor("t1")
			period := models.PeriodOf(time.Now())
			if err := WithTxn(ctx, db, func(sc mongo.SessionContext) error {
				if err := store.monthly.Apply(sc, period, models.CredentialKindOrg, 0, tc.consumed, 0); err != nil {
					return err
				}
				return store.monthly.Reserve(sc, period, tc.reserved)
			}); err != nil {
				t.Fatal(err)
			}
			ok, err := store.Reserve(ctx, period, pendingEvent("t1", 1, tc.request), 1000, tc.warnPct, true)
			if ok || err != nil {
				t.Fatalf("reserve = %v, %v; want a refusal", ok, err)
			}
			if n := countDocs(t, db, CollUsage, bson.M{"outcome": models.OutcomeBudgetBlocked}); n != 1 {
				t.Fatalf("budget_blocked rows = %d", n)
			}
			n := countDocs(t, db, CollBudgetOutbox, bson.M{"threshold": models.ThresholdExhausted})
			m, _ := store.monthly.Get(ctx, period)
			oversized := countDocs(t, db, CollBudgetOutbox, bson.M{"threshold": models.ThresholdOversized, "payload.requestTokens": int64(tc.request)})
			if tc.wantOversized != (oversized == 1) || tc.wantOversized != (m.OversizedAt != nil) {
				t.Fatalf("oversized outbox rows = %d, oversizedAt = %v; want alert %v", oversized, m.OversizedAt, tc.wantOversized)
			}
			if tc.wantOversized {
				// Once per period: a second refusal sends nothing more.
				if ok, err := store.Reserve(ctx, period, pendingEvent("t1", 2, tc.request), 1000, tc.warnPct, true); ok || err != nil {
					t.Fatalf("second reserve = %v, %v; want a refusal", ok, err)
				}
				if n := countDocs(t, db, CollBudgetOutbox, bson.M{"threshold": models.ThresholdOversized}); n != 1 {
					t.Fatalf("oversized outbox rows after a second refusal = %d", n)
				}
			}
			if tc.wantAlert {
				if n != 1 || m.ExhaustedAt == nil {
					t.Fatalf("exhausted outbox rows = %d, exhaustedAt = %v; want one alert", n, m.ExhaustedAt)
				}
				return
			}
			if n != 0 || m.ExhaustedAt != nil {
				t.Fatalf("exhausted outbox rows = %d, exhaustedAt = %v; want none", n, m.ExhaustedAt)
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
	if ok, err := store.Reserve(ctx, period, ev, 0, 0, false); !ok || err != nil {
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
	if ok, err := store.Reserve(ctx, period, ev2, 0, 0, false); !ok || err != nil {
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
		if ok, err := store.Reserve(ctx, period, evs[i], 10000, 80, true); !ok || err != nil {
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
	bg := context.Background()
	for i, tenant := range []string{"t1", "t2"} {
		if err := usage.InsertPending(ctxFor(tenant), &models.UsageEvent{UUID: fmt.Sprintf("r%d", i), UserUUID: "subject", CredentialUUID: "acc", Outcome: models.OutcomeOK, CreatedAt: now}); err != nil {
			t.Fatal(err)
		}
	}
	if err := usage.InsertPending(ctxFor("t1"), &models.UsageEvent{UUID: "keep", UserUUID: "someone", CredentialUUID: "acc", Outcome: models.OutcomeOK, CreatedAt: now}); err != nil {
		t.Fatal(err)
	}
	// An in-flight call of the subject: its reservation is still counted in
	// the rollup, so the hard delete must leave it to the reconciler.
	if err := usage.InsertPending(ctxFor("t1"), &models.UsageEvent{UUID: "inflight", UserUUID: "subject", CredentialUUID: "acc2", Outcome: models.OutcomePending, ReservedTokens: 40, CreatedAt: now}); err != nil {
		t.Fatal(err)
	}
	rows, err := usage.ListByUserAllTenants(bg, "subject")
	if err != nil || len(rows) != 3 {
		t.Fatalf("export = %d, %v", len(rows), err)
	}

	// Hard delete: the two finalized rows (both orgs), never the pending one.
	if n, err := usage.DeleteByUser(bg, "subject"); err != nil || n != 2 {
		t.Fatalf("delete = %d, %v", n, err)
	}
	if n := countAll(t, db, CollUsage, bson.M{"uuid": "inflight", "userUuid": "subject", "outcome": models.OutcomePending, "reservedTokens": 40}); n != 1 {
		t.Fatal("hard delete removed or altered the pending row")
	}
	if n := countAll(t, db, CollUsage, bson.M{"uuid": "keep", "userUuid": "someone", "credentialUuid": "acc"}); n != 1 {
		t.Fatal("hard delete touched another user's row")
	}

	// ... then the producer anonymizes what is left (the pending row).
	ids := []string{}
	newID := func() string {
		id := fmt.Sprintf("%s%d", models.ErasedUsagePrefix, len(ids))
		ids = append(ids, id)
		return id
	}
	if n, err := usage.AnonymizeByUser(bg, "subject", newID); err != nil || n != 1 || len(ids) != 1 {
		t.Fatalf("anonymize pending = %d, %v (ids %v)", n, err, ids)
	}
	if n := countAll(t, db, CollUsage, bson.M{"uuid": "inflight", "userUuid": ids[0], "credentialUuid": bson.M{"$exists": false}, "outcome": models.OutcomePending}); n != 1 {
		t.Fatal("pending row not anonymized in place")
	}

	// Anonymize mode on finalized rows: one replacement per row, across orgs.
	for i, tenant := range []string{"t1", "t2"} {
		if err := usage.InsertPending(ctxFor(tenant), &models.UsageEvent{UUID: fmt.Sprintf("a%d", i), UserUUID: "subject", CredentialUUID: "acc", InputTokens: 9, Outcome: models.OutcomeOK, CreatedAt: now}); err != nil {
			t.Fatal(err)
		}
	}
	if n, err := usage.AnonymizeByUser(bg, "subject", newID); err != nil || n != 2 || len(ids) != 3 {
		t.Fatalf("anonymize = %d, %v (ids %v)", n, err, ids)
	}
	for _, id := range ids[1:] {
		if n := countAll(t, db, CollUsage, bson.M{"userUuid": id, "credentialUuid": bson.M{"$exists": false}, "inputTokens": 9}); n != 1 {
			t.Fatalf("rows anonymized as %s = %d", id, n)
		}
	}
	if n := countAll(t, db, CollUsage, bson.M{"userUuid": "subject"}); n != 0 {
		t.Fatalf("rows still naming the subject = %d", n)
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

// A call in flight while its user is erased: the finalization must not
// restore the credential reference the anonymization removed, and must
// still release the reservation and count the tokens.
func TestUsage_FinalizeAfterAnonymizeKeepsCredentialRemoved(t *testing.T) {
	db := newTestDB(t)
	ensureBudgetIndexes(t, db)
	store := newStore(db)
	usage := NewUsage(db)
	monthly := NewMonthly(db)
	ctx := ctxFor("t1")
	period := models.PeriodOf(time.Now().UTC())

	erased := pendingEvent("t1", 1, 100)
	erased.UserUUID, erased.CredentialUUID = "subject", "cred-1"
	control := pendingEvent("t1", 2, 50)
	control.UserUUID, control.CredentialUUID = "other", "cred-1"
	for _, ev := range []*models.UsageEvent{erased, control} {
		if ok, err := store.Reserve(ctx, period, ev, 0, 0, true); err != nil || !ok {
			t.Fatalf("reserve = %v, %v", ok, err)
		}
	}
	if n, err := usage.AnonymizeByUser(context.Background(), "subject", func() string { return models.ErasedUsagePrefix + "x" }); err != nil || n != 1 {
		t.Fatalf("anonymize = %d, %v", n, err)
	}
	for _, ev := range []*models.UsageEvent{erased, control} {
		upd := models.FinalizeUpdate{Outcome: models.OutcomeOK, InputTokens: 10, OutputTokens: 5, FinalizedAt: time.Now().UTC(),
			Provider: "openai", ModelID: "gpt", ModelUUID: "m1", CredentialKind: models.CredentialKindOrg, CredentialUUID: "cred-1"}
		if _, err := store.Finalize(ctx, period, ev.UUID, upd); err != nil {
			t.Fatalf("finalize %s: %v", ev.UUID, err)
		}
	}
	if n := countDocs(t, db, CollUsage, bson.M{"uuid": erased.UUID, "userUuid": models.ErasedUsagePrefix + "x", "outcome": models.OutcomeOK,
		"credentialUuid": bson.M{"$exists": false}, "reservedTokens": 0, "reservationExpiresAt": bson.M{"$exists": false}}); n != 1 {
		t.Fatal("finalize restored credentialUuid on an anonymized row (or did not close it)")
	}
	if n := countDocs(t, db, CollUsage, bson.M{"uuid": control.UUID, "userUuid": "other", "credentialUuid": "cred-1", "outcome": models.OutcomeOK, "inputTokens": 10}); n != 1 {
		t.Fatal("finalize of a regular row lost its credentialUuid")
	}
	m, err := monthly.Get(ctx, period)
	if err != nil {
		t.Fatal(err)
	}
	if m.ReservedTokens != 0 || m.OrgInputTokens != 20 || m.OrgOutputTokens != 10 || m.Requests != 2 {
		t.Fatalf("rollup after finalize = %+v", m)
	}
}

func TestBudgetsAndOutbox_DSR(t *testing.T) {
	db := newTestDB(t)
	ensureBudgetIndexes(t, db)
	b := NewBudgets(db)
	o := NewOutbox(db)
	bg := context.Background()
	now := time.Now().UTC().Truncate(time.Millisecond)
	for tenant, bud := range map[string]*models.Budget{
		"t1": {MonthlyTokenLimit: 10, Action: models.BudgetActionBlock, NotifyExtraUserUUIDs: []string{"subject", "other"}, UpdatedBy: "subject", UpdatedAt: now},
		"t2": {MonthlyTokenLimit: 20, Action: models.BudgetActionBlock, NotifyExtraUserUUIDs: []string{"other"}, UpdatedBy: "admin", UpdatedAt: now},
		"t3": {MonthlyTokenLimit: 30, Action: models.BudgetActionBlock, NotifyExtraUserUUIDs: []string{"subject"}, UpdatedBy: "admin", UpdatedAt: now},
	} {
		if err := b.Put(ctxFor(tenant), bud); err != nil {
			t.Fatal(err)
		}
	}
	for i, tenant := range []string{"t1", "t2"} {
		ev := &models.OutboxEvent{UUID: fmt.Sprintf("ob%d", i), Period: "2026-03", Threshold: models.ThresholdWarn, Status: models.OutboxStatusSent, CreatedAt: now, NextAttemptAt: now}
		if ok, err := o.InsertUnique(ctxFor(tenant), ev); err != nil || !ok {
			t.Fatalf("insert outbox = %v, %v", ok, err)
		}
	}
	for tenant, set := range map[string]bson.M{
		"t1": {"uuid": "ob0", "to": []string{"subject", "other"}},
		"t2": {"uuid": "ob1", "to": []string{"other"}},
	} {
		filter, err := tenantrepo.Scope(ctxFor(tenant), bson.M{"uuid": set["uuid"]})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := db.Collection(CollBudgetOutbox).UpdateOne(bg, filter, bson.M{"$set": bson.M{"deliveredTo": set["to"]}}); err != nil {
			t.Fatal(err)
		}
	}

	refs, err := b.ListReferencingUser(bg, "subject")
	if err != nil || len(refs) != 2 {
		t.Fatalf("budgets referencing = %+v, %v", refs, err)
	}
	alerts, err := o.ListDeliveredTo(bg, "subject")
	if err != nil || len(alerts) != 1 || alerts[0].UUID != "ob0" || alerts[0].TenantID != "t1" {
		t.Fatalf("alerts delivered = %+v, %v", alerts, err)
	}
	if n, err := b.RemoveRecipient(bg, "subject"); err != nil || n != 2 {
		t.Fatalf("remove recipient = %d, %v", n, err)
	}
	if n, err := b.PseudonymizeUpdater(bg, "subject"); err != nil || n != 1 {
		t.Fatalf("pseudonymize updater = %d, %v", n, err)
	}
	if n, err := o.RemoveDelivered(bg, "subject"); err != nil || n != 1 {
		t.Fatalf("remove delivered = %d, %v", n, err)
	}
	t1, err := b.Get(ctxFor("t1"))
	if err != nil || len(t1.NotifyExtraUserUUIDs) != 1 || t1.NotifyExtraUserUUIDs[0] != "other" || t1.UpdatedBy != models.ErasedActor || !t1.UpdatedAt.Equal(now) || t1.MonthlyTokenLimit != 10 {
		t.Fatalf("t1 budget after erasure = %+v, %v", t1, err)
	}
	if t2, _ := b.Get(ctxFor("t2")); t2.UpdatedBy != "admin" || len(t2.NotifyExtraUserUUIDs) != 1 {
		t.Fatalf("t2 budget must be untouched: %+v", t2)
	}
	if n := countAll(t, db, CollBudgets, bson.M{"$or": []bson.M{{"notifyExtraUserUuids": "subject"}, {"updatedBy": "subject"}}}); n != 0 {
		t.Fatalf("budgets still naming the subject = %d", n)
	}
	if n := countAll(t, db, CollBudgetOutbox, bson.M{"deliveredTo": "subject"}); n != 0 {
		t.Fatalf("alerts still naming the subject = %d", n)
	}
	if n := countAll(t, db, CollBudgetOutbox, bson.M{"deliveredTo": "other"}); n != 2 {
		t.Fatalf("other recipients lost = %d", n)
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
