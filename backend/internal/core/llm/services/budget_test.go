package services

import (
	"context"
	"errors"
	"log/slog"
	"testing"
	"time"

	"github.com/orkestra/backend/internal/core/llm/models"
	"github.com/orkestra/backend/internal/core/llm/repository"
	"github.com/orkestra/backend/pkg/sdk/iface"
)

// memBudgetStore implements BudgetStore (the narrow interface budget.go
// declares) with a sequential in-memory model; the Mongo-level races are
// covered by repository/budget_txn_test.go and budget_mongo_test.go. The
// router tests reuse it, so Finalize copies every field the router
// writes (attempts, fallback, error code).
type memBudgetStore struct {
	budget  *models.Budget
	monthly map[string]*models.UsageMonthly
	ledger  map[string]*models.UsageEvent
	outbox  []models.OutboxEvent
}

func newMemBudgetStore(limit int64, action string) *memBudgetStore {
	return &memBudgetStore{budget: &models.Budget{TenantID: "t1", MonthlyTokenLimit: limit, WarnPct: 80, Action: action},
		monthly: map[string]*models.UsageMonthly{}, ledger: map[string]*models.UsageEvent{}}
}

func (m *memBudgetStore) GetBudget(context.Context) (*models.Budget, error) {
	if m.budget == nil {
		return nil, ErrNotFound
	}
	return m.budget, nil
}

func (m *memBudgetStore) Reserve(_ context.Context, period string, ev *models.UsageEvent, limit int64, warnPct int, block bool) (bool, error) {
	mo := m.month(period)
	if limit > 0 && mo.Consumed()+mo.ReservedTokens+int64(ev.ReservedTokens) > limit && block {
		ev.Outcome, ev.ReservedTokens, ev.ReservedOutputTokens = models.OutcomeBudgetBlocked, 0, 0
		m.ledger[ev.UUID] = ev
		if models.RefusalAlerts(mo.Consumed()+mo.ReservedTokens, limit, warnPct) {
			m.mark(mo, models.ThresholdExhausted)
		}
		return false, nil
	}
	mo.ReservedTokens += int64(ev.ReservedTokens)
	m.ledger[ev.UUID] = ev
	return true, nil
}

func (m *memBudgetStore) Finalize(_ context.Context, period, uuid string, upd models.FinalizeUpdate) (crossed []string, err error) {
	ev, ok := m.ledger[uuid]
	if !ok || ev.Outcome != models.OutcomePending {
		return nil, ErrAlreadyFinalized
	}
	ev.Outcome, ev.ErrorCode, ev.Fallback, ev.Attempts = upd.Outcome, upd.ErrorCode, upd.Fallback, upd.Attempts
	ev.InputTokens, ev.OutputTokens, ev.LatencyMs = upd.InputTokens, upd.OutputTokens, upd.LatencyMs
	if upd.ModelUUID != "" {
		ev.ModelUUID, ev.Provider, ev.ModelID = upd.ModelUUID, upd.Provider, upd.ModelID
	}
	fin := upd.FinalizedAt
	ev.FinalizedAt = &fin
	kind := upd.CredentialKind
	if kind == "" {
		kind = ev.CredentialKind
	}
	mo := m.month(period)
	mo.Requests++
	mo.ReservedTokens -= int64(ev.ReservedTokens)
	ev.ReservedTokens = 0
	if kind == models.CredentialKindUserAccount {
		mo.UserAccountInputTokens += int64(upd.InputTokens)
		mo.UserAccountOutputTokens += int64(upd.OutputTokens)
	} else {
		mo.OrgInputTokens += int64(upd.InputTokens)
		mo.OrgOutputTokens += int64(upd.OutputTokens)
	}
	if m.budget == nil || m.budget.MonthlyTokenLimit <= 0 {
		return nil, nil
	}
	limit := m.budget.MonthlyTokenLimit
	if mo.Consumed()*100 >= limit*int64(m.budget.WarnPct) && m.mark(mo, models.ThresholdWarn) {
		crossed = append(crossed, models.ThresholdWarn)
	}
	if mo.Consumed() >= limit && m.mark(mo, models.ThresholdExhausted) {
		crossed = append(crossed, models.ThresholdExhausted)
	}
	return crossed, nil
}

func (m *memBudgetStore) mark(mo *models.UsageMonthly, threshold string) bool {
	at := &mo.WarnedAt
	if threshold == models.ThresholdExhausted {
		at = &mo.ExhaustedAt
	}
	if *at != nil {
		return false
	}
	now := time.Now()
	*at = &now
	m.outbox = append(m.outbox, models.OutboxEvent{TenantID: mo.TenantID, Period: mo.Period, Threshold: threshold})
	return true
}

func (m *memBudgetStore) month(p string) *models.UsageMonthly {
	if m.monthly[p] == nil {
		m.monthly[p] = &models.UsageMonthly{TenantID: "t1", Period: p}
	}
	return m.monthly[p]
}

func fixedBudgetCfg() BudgetConfig {
	return BudgetConfig{ReserveOutputTokens: 100, ReservationTTL: time.Minute}
}

func TestBudget_BlockThenWarnOnly(t *testing.T) {
	store := newMemBudgetStore(1000, models.BudgetActionBlock)
	svc := NewBudgetService(store, fixedBudgetCfg, slog.Default())
	ctx := ctxFor("t1")
	in := ReserveInput{Caller: "t", Purpose: "default", CredentialKind: models.CredentialKindOrg, EstimatedInput: 900, ReserveOutput: 100}
	res, err := svc.Reserve(ctx, in)
	if err != nil || res.Reserved != 1000 || res.ReservedOutput != 100 || res.WarnOnly {
		t.Fatalf("first reserve = %+v, %v", res, err)
	}
	if ev := store.ledger[res.LedgerUUID]; ev.TenantID != "t1" || ev.UserUUID != "u-admin" || ev.ReservedOutputTokens != 100 || ev.ReservationExpiresAt == nil {
		t.Fatalf("pending row = %+v", ev)
	}
	if _, err := svc.Reserve(ctx, in); !errors.Is(err, iface.ErrLLMBudgetExceeded) {
		t.Fatalf("second reserve err = %v", err)
	}
	blocked := 0
	for _, ev := range store.ledger {
		if ev.Outcome == models.OutcomeBudgetBlocked {
			blocked++
		}
	}
	// The first refusal of the period fires the exhausted alert (ruling 4.1).
	if blocked != 1 || len(store.outbox) != 1 || store.outbox[0].Threshold != models.ThresholdExhausted {
		t.Fatalf("budget_blocked rows = %d, outbox = %+v", blocked, store.outbox)
	}
	store.budget.Action = models.BudgetActionWarnOnly
	res2, err := svc.Reserve(ctx, in)
	if err != nil || !res2.WarnOnly {
		t.Fatalf("warn_only reserve = %+v, %v", res2, err)
	}
}

func TestBudget_ReserveOutputDefaultsToConfig(t *testing.T) {
	store := newMemBudgetStore(0, "")
	store.budget = nil
	svc := NewBudgetService(store, fixedBudgetCfg, slog.Default())
	res, err := svc.Reserve(ctxFor("t1"), ReserveInput{Caller: "t", EstimatedInput: 7, CredentialKind: models.CredentialKindOrg})
	if err != nil || res.Reserved != 107 || res.ReservedOutput != 100 {
		t.Fatalf("reserve = %+v, %v", res, err)
	}
}

func TestBudget_ReserveRequiresTenant(t *testing.T) {
	svc := NewBudgetService(newMemBudgetStore(0, ""), fixedBudgetCfg, slog.Default())
	if _, err := svc.Reserve(context.Background(), ReserveInput{Caller: "t"}); err == nil {
		t.Fatal("reserve without tenant accepted")
	}
}

func TestBudget_FinalizeCountsUserAccountTokensAndCrossesThresholds(t *testing.T) {
	store := newMemBudgetStore(1000, models.BudgetActionBlock)
	svc := NewBudgetService(store, fixedBudgetCfg, slog.Default())
	ctx := ctxFor("t1")
	res, _ := svc.Reserve(ctx, ReserveInput{Caller: "t", CredentialKind: models.CredentialKindUserAccount, EstimatedInput: 500, ReserveOutput: 100})
	attempts := make([]models.Attempt, 12)
	if err := svc.Finalize(ctx, res, FinalizeInput{Outcome: models.OutcomeOK, CredentialKind: models.CredentialKindUserAccount, Attempts: attempts, Fallback: true,
		Usage: iface.LLMUsage{InputTokens: 700, OutputTokens: 200}}); err != nil {
		t.Fatal(err)
	}
	mo := store.monthly[res.Period]
	if mo.UserAccountInputTokens != 700 || mo.UserAccountOutputTokens != 200 || mo.ReservedTokens != 0 || mo.WarnedAt == nil || mo.ExhaustedAt != nil {
		t.Fatalf("monthly = %+v", mo)
	}
	if ev := store.ledger[res.LedgerUUID]; len(ev.Attempts) != models.MaxLedgerAttempts || !ev.Fallback || ev.FinalizedAt == nil {
		t.Fatalf("row = %+v", ev)
	}
	// Finalizing twice is a no-op error, never a double count.
	if err := svc.Finalize(ctx, res, FinalizeInput{Outcome: models.OutcomeOK, Usage: iface.LLMUsage{InputTokens: 1, OutputTokens: 1}}); !errors.Is(err, ErrAlreadyFinalized) {
		t.Fatalf("second finalize err = %v", err)
	}
	if mo.Consumed() != 900 {
		t.Fatalf("consumed = %d", mo.Consumed())
	}
}

func TestBudget_NoBudgetMeansNoLimitButStillLedger(t *testing.T) {
	store := newMemBudgetStore(0, "")
	store.budget = nil
	svc := NewBudgetService(store, fixedBudgetCfg, slog.Default())
	res, err := svc.Reserve(ctxFor("t1"), ReserveInput{Caller: "t", EstimatedInput: 10, CredentialKind: models.CredentialKindOrg})
	if err != nil || res.LedgerUUID == "" || res.WarnOnly {
		t.Fatalf("reserve without budget = %+v, %v", res, err)
	}
}

// A repository-backed store reports a missing budget as
// repository.ErrNotFound; the service must treat it as "no limit"
// (ruling 3.2), never as an error that fails Chat.
type repoNotFoundStore struct{ *memBudgetStore }

func (repoNotFoundStore) GetBudget(context.Context) (*models.Budget, error) {
	return nil, repository.ErrNotFound
}

func TestBudget_RepositoryNotFoundIsNoBudget(t *testing.T) {
	store := repoNotFoundStore{newMemBudgetStore(0, "")}
	svc := NewBudgetService(store, fixedBudgetCfg, slog.Default())
	if _, err := svc.Reserve(ctxFor("t1"), ReserveInput{Caller: "t", EstimatedInput: 10}); err != nil {
		t.Fatalf("reserve = %v", err)
	}
}

func TestBudget_ReadErrorFailsClosed(t *testing.T) {
	store := failingBudgetStore{newMemBudgetStore(0, "")}
	svc := NewBudgetService(store, fixedBudgetCfg, slog.Default())
	if _, err := svc.Reserve(ctxFor("t1"), ReserveInput{Caller: "t", EstimatedInput: 10}); err == nil || errors.Is(err, iface.ErrLLMBudgetExceeded) {
		t.Fatalf("reserve with a broken budget read = %v", err)
	}
}

type failingBudgetStore struct{ *memBudgetStore }

func (failingBudgetStore) GetBudget(context.Context) (*models.Budget, error) {
	return nil, errors.New("mongo down")
}

func TestBudget_FinalizeRejectsNonFinalOutcomes(t *testing.T) {
	store := newMemBudgetStore(0, "")
	store.budget = nil
	svc := NewBudgetService(store, fixedBudgetCfg, slog.Default())
	ctx := ctxFor("t1")
	res, err := svc.Reserve(ctx, ReserveInput{Caller: "t", EstimatedInput: 10})
	if err != nil {
		t.Fatal(err)
	}
	for _, outcome := range []string{"", models.OutcomePending, models.OutcomeBudgetBlocked, "bogus"} {
		if err := svc.Finalize(ctx, res, FinalizeInput{Outcome: outcome, Usage: iface.LLMUsage{InputTokens: 5}}); !errors.Is(err, ErrInvalidOutcome) {
			t.Fatalf("outcome %q: err = %v", outcome, err)
		}
	}
	if ev := store.ledger[res.LedgerUUID]; ev.Outcome != models.OutcomePending || store.monthly[res.Period].Requests != 0 {
		t.Fatalf("a refused finalize wrote: %+v", ev)
	}
	if err := svc.Finalize(ctx, res, FinalizeInput{Outcome: models.OutcomeError}); err != nil {
		t.Fatalf("error outcome refused: %v", err)
	}
}

// Any action but an explicit warn_only blocks (fail closed).
func TestBudget_UnknownActionBlocks(t *testing.T) {
	for _, action := range []string{"", "bogus", models.BudgetActionBlock} {
		store := newMemBudgetStore(100, action)
		svc := NewBudgetService(store, fixedBudgetCfg, slog.Default())
		if _, err := svc.Reserve(ctxFor("t1"), ReserveInput{Caller: "t", EstimatedInput: 500}); !errors.Is(err, iface.ErrLLMBudgetExceeded) {
			t.Fatalf("action %q: err = %v", action, err)
		}
	}
}

func TestBudget_NonPositiveTTLUsesDefault(t *testing.T) {
	for _, ttl := range []time.Duration{0, -time.Hour} {
		store := newMemBudgetStore(0, "")
		store.budget = nil
		svc := NewBudgetService(store, func() BudgetConfig { return BudgetConfig{ReserveOutputTokens: 1, ReservationTTL: ttl} }, slog.Default())
		res, err := svc.Reserve(ctxFor("t1"), ReserveInput{Caller: "t", EstimatedInput: 1})
		if err != nil {
			t.Fatal(err)
		}
		left := time.Until(*store.ledger[res.LedgerUUID].ReservationExpiresAt)
		if left < defaultReservationTTL-time.Minute || left > defaultReservationTTL {
			t.Fatalf("ttl %v: reservation expires in %v", ttl, left)
		}
	}
}
