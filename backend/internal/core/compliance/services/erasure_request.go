package services

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/orkestra/backend/internal/core/compliance/models"
	"github.com/orkestra/backend/internal/core/compliance/repository"
	"github.com/orkestra/backend/pkg/sdk/iface"
)

// ErrErasureRequestStarted is returned by Reject for a request whose erasure
// has already started: it can be retried, not rejected.
var ErrErasureRequestStarted = errors.New("compliance: the erasure has started; it can be retried, not rejected")

const (
	// ErasureRetryMaxAttempts bounds the automatic retries of one request;
	// after it nextAttemptAt is cleared and an operator must retry.
	ErasureRetryMaxAttempts = 20
	// erasureRunLease is how long one run owns its request; a run's deadline
	// is half of it.
	erasureRunLease = 5 * time.Minute

	erasureRetryBackoffBase = time.Minute
	erasureRetryBackoffCap  = 6 * time.Hour
)

// erasureRetryBackoff is the wait before retry number attempts: 1m << (attempts-1), capped at 6h.
func erasureRetryBackoff(attempts int) time.Duration {
	if attempts < 1 {
		attempts = 1
	}
	if attempts > 10 { // 1m<<9 already exceeds the cap; avoid shift overflow
		return erasureRetryBackoffCap
	}
	d := erasureRetryBackoffBase << (attempts - 1)
	if d > erasureRetryBackoffCap {
		return erasureRetryBackoffCap
	}
	return d
}

// erasureRequestRepo is the persistence seam the service depends on. Satisfied
// by *repository.ErasureRequestRepository in production and by
// erasuretest.MemRepo in unit tests.
type erasureRequestRepo interface {
	Insert(ctx context.Context, req *models.ErasureRequest) error
	Get(ctx context.Context, uuid string) (*models.ErasureRequest, error)
	ListByStatus(ctx context.Context, statuses ...string) ([]models.ErasureRequest, error)
	Resolve(ctx context.Context, uuid, status, resolvedBy, mode, note string) error
	Claim(ctx context.Context, uuid, fromStatus, mode, actor string, now, leaseUntil time.Time) (*models.ErasureRequest, error)
	ClaimDue(ctx context.Context, now, leaseUntil time.Time) (*models.ErasureRequest, error)
	Settle(ctx context.Context, uuid string, leaseUntil time.Time, s repository.ErasureSettlement) error
	HasIncomplete(ctx context.Context, userUUID string) (bool, error)
}

// ErasureOutcome is the result of one leased erasure run.
type ErasureOutcome struct {
	Request *models.ErasureRequest // the row as settled
	Run     *EraseResult           // what this run did
}

// ErasureRequestService runs the right-to-erasure workflow. Every path (an
// operator executing a request, a self-service erase, the retry worker) goes
// through one leased run that records per-producer progress and keeps the
// request incomplete until every producer is done.
type ErasureRequestService struct {
	repo   erasureRequestRepo
	dsr    *DSRService
	sink   iface.AuditSink // nil-safe
	logger *slog.Logger
	now    func() time.Time
}

// NewErasureRequestService wires the workflow service to its repo + the DSR
// pipeline used at execution time.
func NewErasureRequestService(repo erasureRequestRepo, dsr *DSRService, sink iface.AuditSink, logger *slog.Logger) *ErasureRequestService {
	if logger == nil {
		logger = slog.Default()
	}
	return &ErasureRequestService{repo: repo, dsr: dsr, sink: sink, logger: logger, now: func() time.Time { return time.Now().UTC() }}
}

// Lodge records a pending erasure request for the subject.
func (s *ErasureRequestService) Lodge(ctx context.Context, userUUID, tenantID, reason string) (*models.ErasureRequest, error) {
	req := &models.ErasureRequest{
		UUID:        uuid.NewString(),
		UserUUID:    userUUID,
		TenantID:    tenantID,
		Reason:      reason,
		Status:      models.ErasureRequestPending,
		RequestedAt: s.now(),
	}
	if err := s.repo.Insert(ctx, req); err != nil {
		return nil, err
	}
	return req, nil
}

// ListOpen returns requests awaiting operator action or further erasure work.
func (s *ErasureRequestService) ListOpen(ctx context.Context) ([]models.ErasureRequest, error) {
	return s.repo.ListByStatus(ctx, models.ErasureRequestPending, models.ErasureRequestIncomplete)
}

// HasIncomplete reports whether the subject has an erasure still incomplete.
func (s *ErasureRequestService) HasIncomplete(ctx context.Context, userUUID string) (bool, error) {
	return s.repo.HasIncomplete(ctx, userUUID)
}

// Execute runs the erasure of a pending request (in mode) or restarts an
// incomplete one (with its stored mode and a fresh attempt budget). A held
// subject errors with ErrLegalHoldActive and the row is left untouched.
func (s *ErasureRequestService) Execute(ctx context.Context, requestUUID string, mode iface.EraseMode, resolvedBy string) (*ErasureOutcome, error) {
	req, err := s.repo.Get(ctx, requestUUID)
	if err != nil {
		return nil, err
	}
	if req.Status != models.ErasureRequestPending && req.Status != models.ErasureRequestIncomplete {
		return nil, repository.ErrErasureRequestNotFound
	}
	if err := s.dsr.CheckLegalHold(ctx, req.UserUUID); err != nil {
		return nil, err
	}
	now := s.now()
	row, err := s.repo.Claim(ctx, requestUUID, req.Status, eraseModeLabel(mode), resolvedBy, now, now.Add(erasureRunLease))
	if err != nil {
		return nil, err
	}
	return s.run(ctx, row)
}

// Reject closes a pending request without erasing. An incomplete request has
// already started erasing and is refused.
func (s *ErasureRequestService) Reject(ctx context.Context, requestUUID, resolvedBy, note string) error {
	if req, err := s.repo.Get(ctx, requestUUID); err == nil && req.Status == models.ErasureRequestIncomplete {
		return ErrErasureRequestStarted
	}
	return s.repo.Resolve(ctx, requestUUID, models.ErasureRequestRejected, resolvedBy, "", note)
}

// EraseNow records an incomplete, leased request for the subject and runs the
// erasure at once. The row is written before any producer runs, so a crash
// leaves a request the worker finishes. A held subject records nothing.
func (s *ErasureRequestService) EraseNow(ctx context.Context, userUUID, tenantID, source, actor string, mode iface.EraseMode) (*ErasureOutcome, error) {
	if err := s.dsr.CheckLegalHold(ctx, userUUID); err != nil {
		return nil, err
	}
	now := s.now()
	lease := now.Add(erasureRunLease)
	row := &models.ErasureRequest{
		UUID:          uuid.NewString(),
		UserUUID:      userUUID,
		TenantID:      tenantID,
		Status:        models.ErasureRequestIncomplete,
		Source:        source,
		Mode:          eraseModeLabel(mode),
		ResolvedBy:    actor,
		RequestedAt:   now,
		LeaseUntil:    &lease,
		NextAttemptAt: &lease,
	}
	if err := s.repo.Insert(ctx, row); err != nil {
		return nil, err
	}
	return s.run(ctx, row)
}

// RetryDue claims the next incomplete request whose retry is due and runs its
// remaining producers. It reports false when nothing was due. Run errors are
// recorded on the row and logged, not returned.
func (s *ErasureRequestService) RetryDue(ctx context.Context) (bool, error) {
	now := s.now()
	row, err := s.repo.ClaimDue(ctx, now, now.Add(erasureRunLease))
	if err != nil {
		return false, err
	}
	if row == nil {
		return false, nil
	}
	if _, err := s.run(ctx, row); err != nil {
		s.logger.Warn("compliance: erasure retry run failed",
			slog.String("request", row.UUID), slog.String("error", err.Error()))
	}
	return true, nil
}

// run executes the remaining producers of a leased request and settles the
// outcome. The run survives caller cancellation (bounded by half the lease).
// A run error is returned after settling; settle errors are logged only: on a
// lost lease the newer holder's state stands, otherwise the lease expires and
// the worker re-runs the remaining producers.
func (s *ErasureRequestService) run(ctx context.Context, row *models.ErasureRequest) (*ErasureOutcome, error) {
	runCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), erasureRunLease/2)
	defer cancel()
	res, runErr := s.dsr.EraseRemaining(runCtx, row.UserUUID, parseEraseMode(row.Mode), row.Progress)

	attempts := row.Attempts + 1
	st := repository.ErasureSettlement{
		Status:   models.ErasureRequestIncomplete,
		Progress: row.Progress,
		Attempts: attempts,
	}
	switch {
	case errors.Is(runErr, ErrLegalHoldActive):
		st.LastError = models.ErasureLastErrorLegalHold
	case runErr != nil:
		st.LastError = models.ErasureLastErrorRun
	default:
		st.Progress = res.Progress
		if res.Complete() {
			st.Status = models.ErasureRequestCompleted
			resolved := s.now()
			st.ResolvedAt = &resolved
		}
	}
	if st.Status == models.ErasureRequestIncomplete {
		if attempts >= ErasureRetryMaxAttempts {
			s.logger.Error("compliance: erasure request needs an operator",
				slog.String("request", row.UUID), slog.Int("attempts", attempts))
		} else {
			next := s.now().Add(erasureRetryBackoff(attempts))
			st.NextAttemptAt = &next
		}
	}

	settleCtx := context.WithoutCancel(ctx)
	switch err := s.repo.Settle(settleCtx, row.UUID, *row.LeaseUntil, st); {
	case errors.Is(err, repository.ErrErasureLeaseLost):
		s.logger.Info("compliance: erasure run lease was taken over; keeping the newer state",
			slog.String("request", row.UUID))
	case err != nil:
		s.logger.Warn("compliance: erasure settle failed; the lease will expire and the worker will retry",
			slog.String("request", row.UUID), slog.String("error", err.Error()))
	}

	settled := *row
	settled.Status, settled.Progress, settled.Attempts = st.Status, st.Progress, st.Attempts
	settled.NextAttemptAt, settled.LastError, settled.LeaseUntil = st.NextAttemptAt, st.LastError, nil
	if st.ResolvedAt != nil {
		settled.ResolvedAt = st.ResolvedAt
	}
	if runErr != nil {
		return nil, runErr
	}
	return &ErasureOutcome{Request: &settled, Run: res}, nil
}
