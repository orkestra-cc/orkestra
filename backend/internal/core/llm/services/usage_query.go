package services

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/orkestra/backend/internal/core/llm/models"
	"github.com/orkestra/backend/internal/core/llm/repository"
	"github.com/orkestra/backend/pkg/sdk/ctxauth"
	"github.com/orkestra/backend/pkg/sdk/iface"
	"github.com/orkestra/backend/pkg/sdk/tenantrepo"
)

// UsageRepo is the read side of llm_usage the reports need.
type UsageRepo interface {
	Query(ctx context.Context, from, to time.Time, groupBy string) ([]models.LLMUsageRow, error)
	UserMonth(ctx context.Context, userUUID, period string) (*models.LLMSelfUsage, error)
}

// MonthlyRepo reads one rollup (an absent month is an empty rollup).
type MonthlyRepo interface {
	Get(ctx context.Context, period string) (*models.UsageMonthly, error)
}

// BudgetRepo is llm_budgets: at most one document per org; Get answers
// repository.ErrNotFound when the org has none.
type BudgetRepo interface {
	Get(ctx context.Context) (*models.Budget, error)
	Put(ctx context.Context, b *models.Budget) error
}

var (
	_ UsageRepo   = (*repository.Usage)(nil)
	_ MonthlyRepo = (*repository.Monthly)(nil)
	_ BudgetRepo  = (*repository.Budgets)(nil)
)

// maxUsageRange bounds the admin report window.
const maxUsageRange = 366 * 24 * time.Hour

// UsageService serves the usage reports and the budget document. Every
// method requires an internal tenant and reads only the caller's org.
type UsageService struct {
	usage   UsageRepo
	monthly MonthlyRepo
	budgets BudgetRepo
	dir     iface.TenantDirectoryReader
	catalog *CatalogService // audit emission
	cfg     func() BudgetConfig
	now     func() time.Time
	logger  *slog.Logger
}

func NewUsageService(usage UsageRepo, monthly MonthlyRepo, budgets BudgetRepo, dir iface.TenantDirectoryReader, catalog *CatalogService, cfg func() BudgetConfig, logger *slog.Logger) *UsageService {
	if logger == nil {
		logger = slog.Default()
	}
	return &UsageService{usage: usage, monthly: monthly, budgets: budgets, dir: dir, catalog: catalog, cfg: cfg, now: time.Now, logger: logger}
}

// Admin aggregates the org's ledger in [from, to) by groupBy (model, user
// or caller) and adds the current month's rollup and the budget (nil when
// the org has none).
func (s *UsageService) Admin(ctx context.Context, from, to time.Time, groupBy string) (*models.LLMUsageReport, error) {
	if err := tenantrepo.RequireInternalTenant(ctx); err != nil {
		return nil, err
	}
	field, ok := models.UsageGroupFields[groupBy]
	if !ok {
		return nil, fmt.Errorf("%w: %w", iface.ErrLLMInvalidRequest, ErrInvalidGroupBy)
	}
	from, to = from.UTC(), to.UTC()
	if !to.After(from) || to.Sub(from) > maxUsageRange {
		return nil, fmt.Errorf("%w: %w", iface.ErrLLMInvalidRequest, ErrInvalidUsageRange)
	}
	rows, err := s.usage.Query(ctx, from, to, field)
	if err != nil {
		return nil, fmt.Errorf("llm: usage query: %w", err)
	}
	month, err := s.monthly.Get(ctx, models.PeriodOf(s.now()))
	if err != nil {
		return nil, fmt.Errorf("llm: usage month: %w", err)
	}
	b, err := s.GetBudget(ctx)
	if err != nil && !errors.Is(err, ErrNotFound) {
		return nil, err
	}
	rep := &models.LLMUsageReport{From: from, To: to, GroupBy: groupBy, Rows: rows, Month: models.LLMUsageMonth(*month), Budget: b}
	if rep.Rows == nil {
		rep.Rows = []models.LLMUsageRow{}
	}
	return rep, nil
}

// Self is the acting user's own consumption in the current UTC month,
// split by credential kind.
func (s *UsageService) Self(ctx context.Context) (*models.LLMSelfUsage, error) {
	if err := tenantrepo.RequireInternalTenant(ctx); err != nil {
		return nil, err
	}
	period := models.PeriodOf(s.now())
	userUUID, _ := ctxauth.GetUserUUID(ctx)
	if userUUID == "" {
		return &models.LLMSelfUsage{Period: period}, nil
	}
	u, err := s.usage.UserMonth(ctx, userUUID, period)
	if err != nil {
		return nil, fmt.Errorf("llm: self usage: %w", err)
	}
	return u, nil
}

// GetBudget returns the org's budget, ErrNotFound when it has none.
func (s *UsageService) GetBudget(ctx context.Context) (*models.LLMBudgetView, error) {
	if err := tenantrepo.RequireInternalTenant(ctx); err != nil {
		return nil, err
	}
	b, err := s.budgets.Get(ctx)
	if err != nil {
		return nil, mapRepoErr(err)
	}
	if b.NotifyExtraUserUUIDs == nil {
		b.NotifyExtraUserUUIDs = []string{}
	}
	v := models.LLMBudgetView(*b)
	return &v, nil
}

// PutBudget creates or replaces the org's budget. Every extra recipient
// must be a member of the org (one non-member rejects the whole call and
// nothing is written); a missing warnPct takes the module default.
func (s *UsageService) PutBudget(ctx context.Context, in models.LLMBudgetPutBody) (*models.LLMBudgetView, error) {
	if err := tenantrepo.RequireInternalTenant(ctx); err != nil {
		return nil, err
	}
	warn := s.cfg().WarnPct
	if in.WarnPct != nil {
		warn = *in.WarnPct
	}
	if in.MonthlyTokenLimit < 1 || warn < 0 || warn > 100 ||
		(in.Action != models.BudgetActionBlock && in.Action != models.BudgetActionWarnOnly) {
		return nil, fmt.Errorf("%w: %w", iface.ErrLLMInvalidRequest, ErrInvalidBudget)
	}
	tenantID, ok := tenantrepo.CurrentTenantID(ctx)
	if !ok || tenantID == "" {
		return nil, tenantrepo.ErrTenantScopeMissing
	}
	extra := make([]string, 0, len(in.NotifyExtraUserUUIDs))
	if len(in.NotifyExtraUserUUIDs) > 0 {
		members, err := s.dir.ListTenantMembers(ctx, tenantID)
		if err != nil {
			return nil, fmt.Errorf("llm: list members: %w", err)
		}
		isMember := make(map[string]bool, len(members))
		for _, mb := range members {
			isMember[mb.UserUUID] = true
		}
		seen := map[string]bool{}
		for _, u := range in.NotifyExtraUserUUIDs {
			if seen[u] {
				continue
			}
			if !isMember[u] {
				return nil, fmt.Errorf("%w: %w: %s", iface.ErrLLMInvalidRequest, ErrBudgetRecipientNotMember, u)
			}
			seen[u] = true
			extra = append(extra, u)
		}
	}
	actor, _ := ctxauth.GetUserUUID(ctx)
	b := &models.Budget{MonthlyTokenLimit: in.MonthlyTokenLimit, WarnPct: warn, Action: in.Action, NotifyExtraUserUUIDs: extra, UpdatedBy: actor, UpdatedAt: s.now().UTC()}
	if err := s.budgets.Put(ctx, b); err != nil {
		return nil, fmt.Errorf("llm: put budget: %w", err)
	}
	s.catalog.emit(ctx, "llm.budget.updated", "llm_budget", tenantID, map[string]any{"monthlyTokenLimit": b.MonthlyTokenLimit, "warnPct": b.WarnPct, "action": b.Action, "extraRecipients": len(extra)})
	v := models.LLMBudgetView(*b)
	return &v, nil
}
