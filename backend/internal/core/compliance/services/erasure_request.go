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

// ErrErasureRequestNotClosable is returned by ClosePartial for a request that
// is not incomplete or still waits on a producer this binary registers.
var ErrErasureRequestNotClosable = errors.New("compliance: the erasure request still waits on a registered producer")

// ErrErasureCloseNoteRequired is returned by ClosePartial without a valid note.
var ErrErasureCloseNoteRequired = errors.New("compliance: a note of 1 to 500 characters is required to close an erasure request")

const (
	// ErasureRetryMaxAttempts bounds the automatic retries of one request;
	// after it nextAttemptAt is cleared and an operator must retry.
	ErasureRetryMaxAttempts = 20
	// erasureRunLease is how long one run owns its request; a run's deadline
	// is half of it.
	erasureRunLease = 5 * time.Minute
	// erasureSettleFloor is the least time Settle gets, even when the run used
	// up its lease.
	erasureSettleFloor = 5 * time.Second

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
	ClaimDue(ctx context.Context, now, leaseUntil time.Time, maxAttempts int) (*models.ErasureRequest, error)
	Settle(ctx context.Context, uuid string, leaseUntil time.Time, s repository.ErasureSettlement) error
	HasIncomplete(ctx context.Context, userUUID string) (bool, error)
	ClosePartial(ctx context.Context, uuid string, now time.Time, abandoned []string, closedBy, note string) error
}

// ErasureOutcome is the result of one leased erasure run.
type ErasureOutcome struct {
	Request *models.ErasureRequest // the row as settled; as stored (incomplete) when Settle failed
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

	settleFloor time.Duration // erasureSettleFloor; shortened by tests
}

// NewErasureRequestService wires the workflow service to its repo + the DSR
// pipeline used at execution time.
func NewErasureRequestService(repo erasureRequestRepo, dsr *DSRService, sink iface.AuditSink, logger *slog.Logger) *ErasureRequestService {
	if logger == nil {
		logger = slog.Default()
	}
	return &ErasureRequestService{
		repo: repo, dsr: dsr, sink: sink, logger: logger,
		now:         func() time.Time { return time.Now().UTC() },
		settleFloor: erasureSettleFloor,
	}
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
	rows, err := s.repo.ListByStatus(ctx, models.ErasureRequestPending, models.ErasureRequestIncomplete)
	if err != nil {
		return nil, err
	}
	registered := s.dsr.RegisteredKeys()
	for i := range rows {
		if rows[i].Status == models.ErasureRequestIncomplete {
			_, rows[i].Closable = abandonedKeys(rows[i].Progress, registered)
		}
	}
	return rows, nil
}

// abandonedKeys returns the not-done progress keys absent from registered; ok
// is true when there is at least one and no not-done key is registered.
func abandonedKeys(progress []models.ErasureProducerProgress, registered map[string]bool) (keys []string, ok bool) {
	for _, p := range progress {
		if p.Outcome == models.ProducerOutcomeDone {
			continue
		}
		if registered[p.Key] {
			return keys, false
		}
		keys = append(keys, p.Key)
	}
	return keys, len(keys) > 0
}

// ClosePartial closes an incomplete request whose not-done producers are all
// gone from this binary, recording them as abandoned. Nothing is erased, so
// the legal hold does not gate it. The note is masked before it is stored.
func (s *ErasureRequestService) ClosePartial(ctx context.Context, requestUUID, closedBy, note string) (*models.ErasureRequest, error) {
	note, valid := NormalizeReason(note)
	if !valid {
		return nil, ErrErasureCloseNoteRequired
	}
	note = maskReason(note)
	row, err := s.repo.Get(ctx, requestUUID)
	if err != nil {
		return nil, err
	}
	switch row.Status {
	case models.ErasureRequestPending:
		return nil, ErrErasureRequestNotClosable
	case models.ErasureRequestIncomplete:
	default:
		return nil, repository.ErrErasureRequestNotFound
	}
	keys, ok := abandonedKeys(row.Progress, s.dsr.RegisteredKeys())
	if !ok {
		return nil, ErrErasureRequestNotClosable
	}
	if err := s.repo.ClosePartial(ctx, requestUUID, s.now(), keys, closedBy, note); err != nil {
		return nil, err
	}
	if s.sink != nil {
		s.sink.Emit(ctx, iface.AuditEvent{
			ActorUserID:  closedBy,
			ActorType:    "user",
			Action:       "gdpr.dsr.erasure_closed_partial",
			ResourceType: "erasure_request",
			ResourceID:   requestUUID,
			Outcome:      "success",
			Metadata: map[string]any{
				"subjectUserUuid": row.UserUUID,
				"abandoned":       keys,
				"mode":            row.Mode,
			},
		})
	}
	return s.repo.Get(ctx, requestUUID)
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

// EraseNow records an incomplete, leased request for the subject (attempt 1)
// and runs the erasure at once. The row is written before any producer runs,
// so a crash leaves a request the worker finishes. A subject already under a
// legal hold records nothing.
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
		Attempts:      1,
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
	row, err := s.repo.ClaimDue(ctx, now, now.Add(erasureRunLease), ErasureRetryMaxAttempts)
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
// outcome. The attempt was counted when the row was claimed, so row.Attempts
// is this run's number. The run survives caller cancellation (bounded by half
// the lease) and Settle is bounded by what is left of the lease (at least
// settleFloor). A run error is returned after settling. When Settle fails or
// the lease was lost the outcome is not stored: the request is reported as it
// is stored, incomplete, and the worker re-runs the remaining producers once
// the lease expires (or the newer holder's state stands).
func (s *ErasureRequestService) run(ctx context.Context, row *models.ErasureRequest) (*ErasureOutcome, error) {
	runCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), erasureRunLease/2)
	defer cancel()
	res, runErr := s.dsr.EraseRemaining(runCtx, row.UserUUID, parseEraseMode(row.Mode), row.Progress)

	attempts := row.Attempts
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

	settleCtx, cancelSettle := context.WithTimeout(context.WithoutCancel(ctx), s.settleTimeout(*row.LeaseUntil))
	defer cancelSettle()
	settled := *row
	settled.Status = models.ErasureRequestIncomplete
	switch err := s.repo.Settle(settleCtx, row.UUID, *row.LeaseUntil, st); {
	case errors.Is(err, repository.ErrErasureLeaseLost):
		s.logger.Warn("compliance: erasure run lease was lost; its outcome is not recorded and the newer state stands",
			slog.String("request", row.UUID), slog.String("intendedStatus", st.Status))
	case err != nil:
		s.logger.Warn("compliance: erasure settle failed; the lease will expire and the worker will retry",
			slog.String("request", row.UUID), slog.String("intendedStatus", st.Status), slog.String("error", err.Error()))
	default:
		settled.Status, settled.Progress, settled.Attempts = st.Status, st.Progress, st.Attempts
		settled.NextAttemptAt, settled.LastError, settled.LeaseUntil = st.NextAttemptAt, st.LastError, nil
		if st.ResolvedAt != nil {
			settled.ResolvedAt = st.ResolvedAt
		}
	}
	if runErr != nil {
		return nil, runErr
	}
	return &ErasureOutcome{Request: &settled, Run: res}, nil
}

// settleTimeout is the time left on the lease, but at least settleFloor.
func (s *ErasureRequestService) settleTimeout(leaseUntil time.Time) time.Duration {
	return max(leaseUntil.Sub(s.now()), s.settleFloor)
}
