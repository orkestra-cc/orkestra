package services

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/orkestra/backend/internal/core/llm/models"
	"github.com/orkestra/backend/internal/core/llm/repository"
	"github.com/orkestra/backend/pkg/sdk/iface"
	"github.com/orkestra/backend/pkg/sdk/tenantrepo"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// newBudgetDB connects to MONGO_TEST_URI (replica set) and returns an
// isolated database with the budget collections' unique indexes, dropped
// on cleanup. Skips without the variable, like the repository harness.
func newBudgetDB(t *testing.T) *mongo.Database {
	t.Helper()
	uri := os.Getenv("MONGO_TEST_URI")
	if uri == "" {
		t.Skip("MONGO_TEST_URI not set")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	client, err := mongo.Connect(ctx, options.Client().ApplyURI(uri))
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	db := client.Database(fmt.Sprintf("llm_svc_test_%d", time.Now().UnixNano()))
	t.Cleanup(func() {
		_ = db.Drop(context.Background())
		_ = client.Disconnect(context.Background())
	})
	for coll, keys := range map[string]bson.D{
		repository.CollUsageMonthly: {{Key: "tenantId", Value: 1}, {Key: "period", Value: 1}},
		repository.CollUsage:        {{Key: "tenantId", Value: 1}, {Key: "uuid", Value: 1}},
		repository.CollBudgets:      {{Key: "tenantId", Value: 1}},
		repository.CollBudgetOutbox: {{Key: "tenantId", Value: 1}, {Key: "period", Value: 1}, {Key: "threshold", Value: 1}},
	} {
		if _, err := db.Collection(coll).Indexes().CreateOne(context.Background(), mongo.IndexModel{Keys: keys, Options: options.Index().SetUnique(true)}); err != nil {
			t.Fatal(err)
		}
	}
	return db
}

type budgetRig struct {
	db      *mongo.Database
	usage   *repository.Usage
	monthly *repository.Monthly
	budgets *repository.Budgets
	outbox  *repository.Outbox
	svc     *BudgetService
}

func newBudgetRig(t *testing.T, cfg BudgetConfig) *budgetRig {
	db := newBudgetDB(t)
	r := &budgetRig{db: db, usage: repository.NewUsage(db), monthly: repository.NewMonthly(db), budgets: repository.NewBudgets(db), outbox: repository.NewOutbox(db)}
	r.svc = NewBudgetService(repository.NewBudgetStore(db, r.usage, r.monthly, r.budgets, r.outbox), func() BudgetConfig { return cfg }, slog.Default())
	return r
}

// Review focus 3: the process dies between the provider call and the
// finalization. The reconciler charges the whole reservation (estimated
// input as input, reserved output as output) and closes the row by its
// next tick; a late finalization afterwards counts nothing.
func TestReconciler_ChargesOrphanOnceAndLateFinalizeIsNoop(t *testing.T) {
	rig := newBudgetRig(t, BudgetConfig{ReserveOutputTokens: 100, ReservationTTL: -time.Second}) // already expired
	ctx := ctxFor("t1")
	if err := rig.budgets.Put(ctx, &models.Budget{MonthlyTokenLimit: 100000, WarnPct: 80, Action: models.BudgetActionBlock}); err != nil {
		t.Fatal(err)
	}
	res, err := rig.svc.Reserve(ctx, ReserveInput{Caller: "t", Purpose: "default", CredentialKind: models.CredentialKindOrg, ModelUUID: "m1", EstimatedInput: 250, ReserveOutput: 50})
	if err != nil {
		t.Fatal(err)
	}
	// ... the router crashes here: no Finalize.
	rec := NewReconciler(rig.usage, rig.svc, slog.Default())
	n, err := rec.RunOnce(context.Background()) // ticker context: no tenant
	if err != nil || n != 1 {
		t.Fatalf("reconcile = %d, %v", n, err)
	}
	m, _ := rig.monthly.Get(ctx, res.Period)
	if m.OrgInputTokens != 250 || m.OrgOutputTokens != 50 || m.ReservedTokens != 0 || m.Requests != 1 {
		t.Fatalf("monthly after reconcile = %+v", m)
	}
	var row models.UsageEvent
	rowFilter, err := tenantrepo.Scope(ctx, bson.M{"uuid": res.LedgerUUID})
	if err != nil {
		t.Fatal(err)
	}
	if err := rig.db.Collection(repository.CollUsage).FindOne(context.Background(), rowFilter).Decode(&row); err != nil {
		t.Fatal(err)
	}
	if row.Outcome != models.OutcomeError || row.ErrorCode != "reconciled_timeout" || row.InputTokens != 250 || row.OutputTokens != 50 || row.ReservedTokens != 0 {
		t.Fatalf("row after reconcile = %+v", row)
	}
	// The late finalization of the dead request: refused, nothing counted.
	err = rig.svc.Finalize(ctx, res, FinalizeInput{Outcome: models.OutcomeOK, Usage: iface.LLMUsage{InputTokens: 9, OutputTokens: 9}})
	if !errors.Is(err, ErrAlreadyFinalized) {
		t.Fatalf("late finalize err = %v", err)
	}
	if m2, _ := rig.monthly.Get(ctx, res.Period); m2.Consumed() != 300 || m2.Requests != 1 {
		t.Fatalf("monthly after late finalize = %+v", m2)
	}
	// Next tick: nothing left.
	if n, err := rec.RunOnce(context.Background()); n != 0 || err != nil {
		t.Fatalf("second reconcile = %d, %v", n, err)
	}
}

// The reverse order: a normal finalization closes the row first, so a
// reconciler pass that listed it just before is a no-op.
func TestReconciler_RacingFinalizeCountsOnce(t *testing.T) {
	rig := newBudgetRig(t, BudgetConfig{ReserveOutputTokens: 100, ReservationTTL: -time.Second})
	ctx := ctxFor("t1")
	res, err := rig.svc.Reserve(ctx, ReserveInput{Caller: "t", CredentialKind: models.CredentialKindOrg, EstimatedInput: 10, ReserveOutput: 10})
	if err != nil {
		t.Fatal(err)
	}
	rec := NewReconciler(rig.usage, rig.svc, slog.Default())
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		if err := rig.svc.Finalize(ctx, res, FinalizeInput{Outcome: models.OutcomeOK, Usage: iface.LLMUsage{InputTokens: 3, OutputTokens: 4}}); err != nil && !errors.Is(err, ErrAlreadyFinalized) {
			t.Errorf("finalize: %v", err)
		}
	}()
	go func() {
		defer wg.Done()
		if _, err := rec.RunOnce(context.Background()); err != nil {
			t.Errorf("reconcile: %v", err)
		}
	}()
	wg.Wait()
	m, _ := rig.monthly.Get(ctx, res.Period)
	if m.Requests != 1 || m.ReservedTokens != 0 || (m.Consumed() != 7 && m.Consumed() != 20) {
		t.Fatalf("monthly = %+v (want exactly one of the two charges)", m)
	}
}

type dedupNotifier struct {
	mu        sync.Mutex
	calls     int
	delivered map[string]int // per idempotency key, as the notification sender dedups
}

func (n *dedupNotifier) SendBudgetEvent(_ context.Context, ev models.OutboxEvent) error {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.calls++
	n.delivered["llm-budget-"+ev.UUID]++
	return nil
}

// Review focus 4: a warn threshold crossed by several finalizations at
// once yields one outbox event; a worker that crashed between leasing
// (sending) and MarkSent does not let a second worker send while its
// lease holds, and the recovery after expiry re-sends under the same
// idempotency key, so one logical notification results.
func TestOutbox_OneEventAndNoDoubleSendAfterCrash(t *testing.T) {
	rig := newBudgetRig(t, BudgetConfig{ReserveOutputTokens: 10, ReservationTTL: time.Minute})
	ctx := ctxFor("t1")
	if err := rig.budgets.Put(ctx, &models.Budget{MonthlyTokenLimit: 1000, WarnPct: 50, Action: models.BudgetActionWarnOnly}); err != nil {
		t.Fatal(err)
	}
	const calls = 5
	reservations := make([]*Reservation, calls)
	for i := range reservations {
		res, err := rig.svc.Reserve(ctx, ReserveInput{Caller: "t", CredentialKind: models.CredentialKindOrg, EstimatedInput: 90, ReserveOutput: 10})
		if err != nil {
			t.Fatal(err)
		}
		reservations[i] = res
	}
	var wg sync.WaitGroup
	for _, res := range reservations {
		wg.Add(1)
		go func(res *Reservation) {
			defer wg.Done()
			if err := rig.svc.Finalize(ctx, res, FinalizeInput{Outcome: models.OutcomeOK, Usage: iface.LLMUsage{InputTokens: 100, OutputTokens: 20}}); err != nil {
				t.Errorf("finalize: %v", err)
			}
		}(res)
	}
	wg.Wait()
	all, err := tenantrepo.Scope(ctx, bson.M{})
	if err != nil {
		t.Fatal(err)
	}
	if n, _ := rig.db.Collection(repository.CollBudgetOutbox).CountDocuments(context.Background(), all); n != 1 {
		t.Fatalf("outbox rows = %d, want 1 (600/1000 crosses 50%% once)", n)
	}
	// Worker A leases the event and dies before MarkSent.
	crashed, err := rig.outbox.LeaseDue(context.Background(), time.Now().UTC(), time.Hour)
	if err != nil || crashed == nil {
		t.Fatalf("lease = %+v, %v", crashed, err)
	}
	notifier := &dedupNotifier{delivered: map[string]int{}}
	w := NewOutboxWorker(rig.outbox, notifier, slog.Default())
	if n, err := w.RunOnce(context.Background()); n != 0 || err != nil || notifier.calls != 0 {
		t.Fatalf("pass under a live lease = %d, %v, calls %d", n, err, notifier.calls)
	}
	// The lease expires; two workers recover concurrently: one send.
	evFilter, err := tenantrepo.Scope(ctx, bson.M{"uuid": crashed.UUID})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := rig.db.Collection(repository.CollBudgetOutbox).UpdateOne(context.Background(), evFilter,
		bson.M{"$set": bson.M{"leaseUntil": time.Now().UTC().Add(-time.Second)}}); err != nil {
		t.Fatal(err)
	}
	w2 := NewOutboxWorker(rig.outbox, notifier, slog.Default())
	wg.Add(2)
	for _, worker := range []*OutboxWorker{w, w2} {
		go func(worker *OutboxWorker) {
			defer wg.Done()
			if _, err := worker.RunOnce(context.Background()); err != nil {
				t.Errorf("recover: %v", err)
			}
		}(worker)
	}
	wg.Wait()
	if notifier.calls != 1 || notifier.delivered["llm-budget-"+crashed.UUID] != 1 {
		t.Fatalf("calls = %d, delivered = %v", notifier.calls, notifier.delivered)
	}
	var ev models.OutboxEvent
	if err := rig.db.Collection(repository.CollBudgetOutbox).FindOne(context.Background(), evFilter).Decode(&ev); err != nil {
		t.Fatal(err)
	}
	if ev.Status != models.OutboxStatusSent || ev.SentAt == nil || ev.LeaseUntil != nil {
		t.Fatalf("event = %+v", ev)
	}
	if n, err := w.RunOnce(context.Background()); n != 0 || err != nil || notifier.calls != 1 {
		t.Fatalf("pass after sent = %d, %v, calls %d", n, err, notifier.calls)
	}
}
