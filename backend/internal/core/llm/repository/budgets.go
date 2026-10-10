package repository

import (
	"context"
	"errors"
	"time"

	"github.com/orkestra/backend/internal/core/llm/models"
	"github.com/orkestra/backend/pkg/sdk/tenantrepo"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

const CollBudgets = "llm_budgets"

// Budgets owns llm_budgets (at most one document per org).
type Budgets struct{ coll *mongo.Collection }

func NewBudgets(db *mongo.Database) *Budgets { return &Budgets{coll: db.Collection(CollBudgets)} }

// Get returns the org's budget, ErrNotFound when it has none (no budget
// means no limit).
func (r *Budgets) Get(ctx context.Context) (*models.Budget, error) {
	filter, err := tenantrepo.Scope(ctx, bson.M{})
	if err != nil {
		return nil, err
	}
	var b models.Budget
	if err := r.coll.FindOne(ctx, filter).Decode(&b); err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return &b, nil
}

// Put creates or replaces the org's budget.
func (r *Budgets) Put(ctx context.Context, b *models.Budget) error {
	tenantID, err := tenantrepo.StampInsert(ctx)
	if err != nil {
		return err
	}
	b.TenantID = tenantID
	if b.NotifyExtraUserUUIDs == nil {
		b.NotifyExtraUserUUIDs = []string{}
	}
	filter, err := tenantrepo.Scope(ctx, bson.M{})
	if err != nil {
		return err
	}
	_, err = r.coll.ReplaceOne(ctx, filter, b, options.Replace().SetUpsert(true))
	return err
}

// BudgetStore implements services.BudgetStore over the four collections
// with Mongo transactions: admission (consumed + reserved + requested <=
// limit) and the ledger insert commit together, and so do the ledger
// close, the rollup update, the threshold marks and their outbox rows.
type BudgetStore struct {
	db      *mongo.Database
	usage   *Usage
	monthly *Monthly
	budgets *Budgets
	outbox  *Outbox
}

func NewBudgetStore(db *mongo.Database, usage *Usage, monthly *Monthly, budgets *Budgets, outbox *Outbox) *BudgetStore {
	return &BudgetStore{db: db, usage: usage, monthly: monthly, budgets: budgets, outbox: outbox}
}

// GetBudget is Budgets.Get (ErrNotFound when the org has none).
func (s *BudgetStore) GetBudget(ctx context.Context) (*models.Budget, error) {
	return s.budgets.Get(ctx)
}

// Reserve admits ev in one transaction: it locks the period's rollup
// (GetForUpdate), checks consumed + reserved + ev.ReservedTokens <= limit,
// then adds the reservation and inserts ev pending. Concurrent
// reservations of one org write the same rollup document, so they
// conflict and are serialized by WithTransaction's retry: two callers
// can never both pass on the same remaining balance.
//
// limit <= 0 means no budget: always admitted. When the request does not
// fit and block is set, the same transaction records ev as
// budget_blocked (no reservation), marks exhaustedAt and inserts the
// period's single "exhausted" outbox event if this is the first refusal
// (ruling 4.1), and Reserve returns false. Under warn_only the call is
// admitted over the limit.
func (s *BudgetStore) Reserve(ctx context.Context, period string, ev *models.UsageEvent, limit int64, block bool) (bool, error) {
	orig := *ev
	admitted := false
	err := WithTxn(ctx, s.db, func(sc mongo.SessionContext) error {
		row := orig // the closure may run more than once
		admitted = false
		m, err := s.monthly.GetForUpdate(sc, period)
		if err != nil {
			return err
		}
		fits := limit <= 0 || m.Consumed()+m.ReservedTokens+int64(row.ReservedTokens) <= limit
		if !fits && block {
			now := time.Now().UTC()
			row.Outcome, row.ReservedTokens, row.ReservedOutputTokens = models.OutcomeBudgetBlocked, 0, 0
			row.ReservationExpiresAt, row.FinalizedAt = nil, &now
			if err := s.usage.InsertPending(sc, &row); err != nil {
				return err
			}
			if err := s.markAndEnqueue(sc, period, models.ThresholdExhausted, m, limit); err != nil {
				return err
			}
			*ev = row
			return nil
		}
		if err := s.monthly.Reserve(sc, period, int64(row.ReservedTokens)); err != nil {
			return err
		}
		if err := s.usage.InsertPending(sc, &row); err != nil {
			return err
		}
		*ev = row
		admitted = true
		return nil
	})
	if err != nil {
		*ev = orig
		return false, err
	}
	return admitted, nil
}

// Finalize closes the pending row uuid and, in the same transaction,
// releases its reservation, adds the real tokens to the payer's counters
// of the row's period and marks the thresholds this call crossed, each
// with its outbox event. It returns the thresholds crossed by this call.
// ErrAlreadyFinalized (nothing written) when the row is not pending.
// The period and the reservation are the row's own (its createdAt and
// reservedTokens), never the caller's; period is accepted for the
// services.BudgetStore contract.
func (s *BudgetStore) Finalize(ctx context.Context, _ string, uuid string, upd models.FinalizeUpdate) ([]string, error) {
	var crossed []string
	err := WithTxn(ctx, s.db, func(sc mongo.SessionContext) error {
		crossed = nil
		budget, err := s.budgets.Get(sc)
		if err != nil && !errors.Is(err, ErrNotFound) {
			return err
		}
		row, err := s.usage.Finalize(sc, uuid, upd)
		if err != nil {
			return err
		}
		kind := upd.CredentialKind
		if kind == "" {
			kind = row.CredentialKind
		}
		period := models.PeriodOf(row.CreatedAt)
		if err := s.monthly.Apply(sc, period, kind, int64(row.ReservedTokens), int64(upd.InputTokens), int64(upd.OutputTokens)); err != nil {
			return err
		}
		if budget == nil || budget.MonthlyTokenLimit <= 0 {
			return nil
		}
		m, err := s.monthly.Get(sc, period)
		if err != nil {
			return err
		}
		limit := budget.MonthlyTokenLimit
		if budget.WarnPct > 0 && m.Consumed()*100 >= limit*int64(budget.WarnPct) && m.WarnedAt == nil {
			if err := s.markAndEnqueue(sc, period, models.ThresholdWarn, m, limit); err != nil {
				return err
			}
			crossed = append(crossed, models.ThresholdWarn)
		}
		if m.Consumed() >= limit && m.ExhaustedAt == nil {
			if err := s.markAndEnqueue(sc, period, models.ThresholdExhausted, m, limit); err != nil {
				return err
			}
			crossed = append(crossed, models.ThresholdExhausted)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return crossed, nil
}

// markAndEnqueue sets the threshold mark on the rollup and, when this
// call set it, inserts the outbox event in the same transaction. The
// mark is the dedup point inside a period; the unique
// (tenantId, period, threshold) index is the last line of defence.
func (s *BudgetStore) markAndEnqueue(sc mongo.SessionContext, period, threshold string, m *models.UsageMonthly, limit int64) error {
	marked, err := s.monthly.MarkThreshold(sc, period, threshold)
	if err != nil || !marked {
		return err
	}
	now := time.Now().UTC()
	pct := int64(0)
	if limit > 0 {
		pct = m.Consumed() * 100 / limit
	}
	_, err = s.outbox.InsertUnique(sc, &models.OutboxEvent{
		UUID: newUUID(), Period: period, Threshold: threshold, Status: models.OutboxStatusPending,
		Payload:       map[string]any{"consumedTokens": m.Consumed(), "reservedTokens": m.ReservedTokens, "limitTokens": limit, "pct": pct},
		NextAttemptAt: now, CreatedAt: now,
	})
	return err
}
