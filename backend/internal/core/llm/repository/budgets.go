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

// ListReferencingUser returns, across every org, the budgets that name
// the data subject as an extra alert recipient or as the last editor.
func (r *Budgets) ListReferencingUser(ctx context.Context, userUUID string) ([]models.Budget, error) {
	//tenantscope:allow dsr: right-of-access export of the budgets naming the subject (recipient or updatedBy) across every org, invoked only by the compliance DSR pipeline through iface.PIIProducer
	cur, err := r.coll.Find(ctx, bson.M{"$or": []bson.M{{"notifyExtraUserUuids": userUUID}, {"updatedBy": userUUID}}})
	if err != nil {
		return nil, err
	}
	out := []models.Budget{}
	if err := cur.All(ctx, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// RemoveRecipient drops the data subject from every org's extra alert
// recipients.
func (r *Budgets) RemoveRecipient(ctx context.Context, userUUID string) (int64, error) {
	//tenantscope:allow dsr: right-to-erasure removal of the subject from budget alert recipients across every org, invoked only by the compliance DSR pipeline through iface.PIIProducer
	res, err := r.coll.UpdateMany(ctx, bson.M{"notifyExtraUserUuids": userUUID}, bson.M{"$pull": bson.M{"notifyExtraUserUuids": userUUID}})
	if err != nil {
		return 0, err
	}
	return res.ModifiedCount, nil
}

// PseudonymizeUpdater rewrites updatedBy = subject to models.ErasedActor
// in every org; updatedAt is left as is (the budget did not change).
func (r *Budgets) PseudonymizeUpdater(ctx context.Context, userUUID string) (int64, error) {
	//tenantscope:allow dsr: right-to-erasure pseudonymization of the budget updatedBy actor across every org, invoked only by the compliance DSR pipeline through iface.PIIProducer
	res, err := r.coll.UpdateMany(ctx, bson.M{"updatedBy": userUUID}, bson.M{"$set": bson.M{"updatedBy": models.ErasedActor}})
	if err != nil {
		return 0, err
	}
	return res.ModifiedCount, nil
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
// budget_blocked (no reservation) and Reserve returns false. If consumed
// + reserved already reach the warn threshold (warnPct of the limit, or
// the limit itself when warnPct is 0) it also marks exhaustedAt and
// inserts the period's single "exhausted" outbox event, on the first such
// refusal (ruling 4.1 as refined by the final review): one oversized
// request against a largely unused balance does not burn the alert.
// Otherwise, when the request alone exceeds the whole limit (it can never
// be admitted), it marks oversizedAt and inserts the period's single
// "oversized" event instead. Under warn_only the call is admitted over
// the limit.
func (s *BudgetStore) Reserve(ctx context.Context, period string, ev *models.UsageEvent, limit int64, warnPct int, block bool) (bool, error) {
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
			requested := int64(orig.ReservedTokens)
			switch {
			case models.RefusalAlerts(m.Consumed()+m.ReservedTokens, limit, warnPct):
				if err := s.markAndEnqueue(sc, period, models.ThresholdExhausted, m, limit, 0); err != nil {
					return err
				}
			case models.RefusalOversized(requested, limit):
				if err := s.markAndEnqueue(sc, period, models.ThresholdOversized, m, limit, requested); err != nil {
					return err
				}
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
			if err := s.markAndEnqueue(sc, period, models.ThresholdWarn, m, limit, 0); err != nil {
				return err
			}
			crossed = append(crossed, models.ThresholdWarn)
		}
		if m.Consumed() >= limit && m.ExhaustedAt == nil {
			if err := s.markAndEnqueue(sc, period, models.ThresholdExhausted, m, limit, 0); err != nil {
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
// requested (the refused reservation, oversized only; 0 otherwise) is
// added to the payload as requestTokens.
func (s *BudgetStore) markAndEnqueue(sc mongo.SessionContext, period, threshold string, m *models.UsageMonthly, limit, requested int64) error {
	marked, err := s.monthly.MarkThreshold(sc, period, threshold)
	if err != nil || !marked {
		return err
	}
	now := time.Now().UTC()
	pct := int64(0)
	if limit > 0 {
		pct = m.Consumed() * 100 / limit
	}
	payload := map[string]any{"consumedTokens": m.Consumed(), "reservedTokens": m.ReservedTokens, "limitTokens": limit, "pct": pct}
	if requested > 0 {
		payload["requestTokens"] = requested
	}
	_, err = s.outbox.InsertUnique(sc, &models.OutboxEvent{
		UUID: newUUID(), Period: period, Threshold: threshold, Status: models.OutboxStatusPending,
		Payload:       payload,
		NextAttemptAt: now, CreatedAt: now,
	})
	return err
}
