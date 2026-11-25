package services

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/orkestra/backend/internal/core/llm/models"
	"github.com/orkestra/backend/internal/core/llm/repository"
	"github.com/orkestra/backend/pkg/sdk/ctxauth"
	"github.com/orkestra/backend/pkg/sdk/iface"
	"github.com/orkestra/backend/pkg/sdk/tenantrepo"
)

// ErrAlreadyFinalized: the ledger row was already closed (by the router
// or the reconciler); nothing was counted. It is the repository's
// sentinel so errors.Is works across the store boundary.
var ErrAlreadyFinalized = repository.ErrAlreadyFinalized

// ErrInvalidOutcome: Finalize was asked to close a row with an outcome
// other than ok, fallback or error (pending or empty would leave the row
// re-finalizable; budget_blocked is written only by the store).
var ErrInvalidOutcome = errors.New("llm: invalid usage outcome for finalization")

var finalOutcomes = map[string]bool{models.OutcomeOK: true, models.OutcomeFallback: true, models.OutcomeError: true}

// BudgetStore is what the service needs from Mongo;
// repository.BudgetStore implements it with real transactions. Its
// types live in models so repository never imports services (ruling 3.2).
type BudgetStore interface {
	// GetBudget returns the org's budget; a not-found error (either
	// package's) means no budget.
	GetBudget(ctx context.Context) (*models.Budget, error)
	// Reserve atomically admits ev (inserting it pending with its
	// reservation) when consumed+reserved+requested <= limit, or records
	// it budget_blocked and returns false when block is set — firing the
	// period's exhausted alert on the first refusal that finds
	// consumed+reserved already at the warn threshold (models.RefusalAlerts).
	// limit <= 0 means no budget.
	Reserve(ctx context.Context, period string, ev *models.UsageEvent, limit int64, warnPct int, block bool) (admitted bool, err error)
	// Finalize closes the pending row, applies real usage to the rollup,
	// releases the reservation and marks thresholds; it returns the
	// thresholds crossed by this call (an outbox row was inserted for
	// each). ErrAlreadyFinalized when the row is not pending.
	Finalize(ctx context.Context, period, uuid string, upd models.FinalizeUpdate) (crossed []string, err error)
}

var _ BudgetStore = (*repository.BudgetStore)(nil)

// BudgetConfig is read on every reservation (hot-reload).
type BudgetConfig struct {
	ReserveOutputTokens int           // default reserved output (budget_reserve_output_tokens)
	ReservationTTL      time.Duration // after which the reconciler may close a pending row; <= 0 means the 3-minute default
	WarnPct             int           // default warn threshold for new budgets (budget_warn_pct)
}

// defaultReservationTTL covers a config accessor that returns no TTL.
const defaultReservationTTL = 3 * time.Minute

type ReserveInput struct {
	UserUUID, ModelUUID, Provider, ModelID, CredentialKind, CredentialUUID, Caller, Purpose string
	EstimatedInput, ReserveOutput                                                           int
}

type FinalizeInput struct {
	Outcome, ErrorCode                                           string
	Fallback                                                     bool
	Usage                                                        iface.LLMUsage
	Attempts                                                     []models.Attempt
	Latency                                                      time.Duration
	Provider, ModelID, ModelUUID, CredentialKind, CredentialUUID string
}

// Reservation is the admission ticket the router carries to Finalize.
// Reserved = estimated input + ReservedOutput.
type Reservation struct {
	LedgerUUID     string
	Reserved       int
	ReservedOutput int
	WarnOnly       bool // admitted under a warn_only budget: no cap promise
	Period         string
	startedAt      time.Time
}

// BudgetService admits calls against the org's monthly budget and closes
// their ledger rows. Every counter it writes is a token count or a code;
// no prompt or output text reaches the ledger.
type BudgetService struct {
	store  BudgetStore
	cfg    func() BudgetConfig
	logger *slog.Logger
}

func NewBudgetService(store BudgetStore, cfg func() BudgetConfig, logger *slog.Logger) *BudgetService {
	if logger == nil {
		logger = slog.Default()
	}
	return &BudgetService{store: store, cfg: cfg, logger: logger}
}

func (s *BudgetService) Config() BudgetConfig { return s.cfg() }

// Reserve reserves EstimatedInput + ReserveOutput (or the configured
// default output) for one call in the current UTC month. Without a budget
// the call is always admitted and still ledgered. Over the limit with
// action block it returns iface.ErrLLMBudgetExceeded after recording a
// budget_blocked row; with warn_only it admits. A failing budget read
// fails the call closed (an error, not an admission).
func (s *BudgetService) Reserve(ctx context.Context, in ReserveInput) (*Reservation, error) {
	tenantID, ok := tenantrepo.CurrentTenantID(ctx)
	if !ok || tenantID == "" {
		return nil, tenantrepo.ErrTenantScopeMissing
	}
	cfg := s.cfg()
	reserveOut := in.ReserveOutput
	if reserveOut <= 0 {
		reserveOut = cfg.ReserveOutputTokens
	}
	reserveOut = max(reserveOut, 0)
	ttl := cfg.ReservationTTL
	if ttl <= 0 {
		ttl = defaultReservationTTL // a pending row must never be born expired
	}
	now := time.Now().UTC()
	expires := now.Add(ttl)
	ev := &models.UsageEvent{
		UUID: uuid.NewString(), TenantID: tenantID, UserUUID: in.UserUUID, ModelUUID: in.ModelUUID, Provider: in.Provider, ModelID: in.ModelID,
		CredentialKind: in.CredentialKind, CredentialUUID: in.CredentialUUID, Caller: in.Caller, Purpose: in.Purpose,
		ReservedTokens: max(in.EstimatedInput, 0) + reserveOut, ReservedOutputTokens: reserveOut, ReservationExpiresAt: &expires,
		Outcome: models.OutcomePending, CreatedAt: now,
	}
	if ev.UserUUID == "" {
		ev.UserUUID, _ = ctxauth.GetUserUUID(ctx)
	}
	period := models.PeriodOf(now)
	var limit int64
	warnPct := 0
	block := false
	budget, err := s.store.GetBudget(ctx)
	switch err = mapRepoErr(err); {
	case err == nil:
		// Fail closed: anything but an explicit warn_only blocks.
		limit, warnPct, block = budget.MonthlyTokenLimit, budget.WarnPct, budget.Action != models.BudgetActionWarnOnly
	case errors.Is(err, ErrNotFound):
		// No budget: no limit.
	default:
		return nil, fmt.Errorf("llm: read budget: %w", err)
	}
	admitted, err := s.store.Reserve(ctx, period, ev, limit, warnPct, block)
	if err != nil {
		return nil, fmt.Errorf("llm: reserve budget: %w", err)
	}
	if !admitted {
		return nil, iface.ErrLLMBudgetExceeded
	}
	return &Reservation{LedgerUUID: ev.UUID, Reserved: ev.ReservedTokens, ReservedOutput: reserveOut, WarnOnly: limit > 0 && !block, Period: period, startedAt: now}, nil
}

// Finalize is called once per reservation by the router (and, for
// orphans, by the reconciler). A second call is ErrAlreadyFinalized and
// counts nothing. Callers must never turn emitted output into an error
// because of it: a failure leaves the row pending for the reconciler.
func (s *BudgetService) Finalize(ctx context.Context, res *Reservation, in FinalizeInput) error {
	if res == nil {
		return nil
	}
	if !finalOutcomes[in.Outcome] {
		return fmt.Errorf("%w: %q", ErrInvalidOutcome, in.Outcome)
	}
	if in.Latency == 0 && !res.startedAt.IsZero() {
		in.Latency = time.Since(res.startedAt)
	}
	attempts := in.Attempts
	if len(attempts) > models.MaxLedgerAttempts {
		attempts = attempts[:models.MaxLedgerAttempts]
	}
	crossed, err := s.store.Finalize(ctx, res.Period, res.LedgerUUID, models.FinalizeUpdate{
		Outcome: in.Outcome, ErrorCode: in.ErrorCode, Fallback: in.Fallback,
		InputTokens: max(in.Usage.InputTokens, 0), OutputTokens: max(in.Usage.OutputTokens, 0),
		Attempts: attempts, LatencyMs: in.Latency.Milliseconds(),
		Provider: in.Provider, ModelID: in.ModelID, ModelUUID: in.ModelUUID, CredentialKind: in.CredentialKind, CredentialUUID: in.CredentialUUID,
		FinalizedAt: time.Now().UTC(),
	})
	if err != nil {
		return err
	}
	for _, th := range crossed {
		s.logger.InfoContext(ctx, "llm: budget threshold crossed", slog.String("threshold", th), slog.String("period", res.Period))
	}
	return nil
}
