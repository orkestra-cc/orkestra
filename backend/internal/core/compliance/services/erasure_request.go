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

// ErrErasureRequestExists is returned by Open when the subject already has a
// pending or incomplete request; the concrete error is *ErasureRequestExistsError.
var ErrErasureRequestExists = errors.New("compliance: the subject already has an open erasure request")

// ErasureRequestExistsError carries the UUID of the subject's open request.
type ErasureRequestExistsError struct{ UUID string }

func (e *ErasureRequestExistsError) Error() string { return ErrErasureRequestExists.Error() }
func (e *ErasureRequestExistsError) Is(target error) bool {
	return target == ErrErasureRequestExists
}

// ErrErasureApprovalsMissing is returned by Execute of a pending request with
// fewer distinct approvals than it requires; the concrete error is
// *ErasureApprovalsMissingError.
var ErrErasureApprovalsMissing = errors.New("compliance: the erasure request lacks the required approvals")

// ErasureApprovalsMissingError reports the distinct approvals the request would
// have with the executor counted (Have) and the number it requires (Need).
type ErasureApprovalsMissingError struct{ Have, Need int }

func (e *ErasureApprovalsMissingError) Error() string { return ErrErasureApprovalsMissing.Error() }
func (e *ErasureApprovalsMissingError) Is(target error) bool {
	return target == ErrErasureApprovalsMissing
}

// ErrErasureSelfApproval is returned when the data subject is the operator who
// opens, approves or executes their own erasure request.
var ErrErasureSelfApproval = errors.New("compliance: an operator cannot act on their own erasure request")

// ErrErasureAlreadyApproved is returned when the operator already approved.
var ErrErasureAlreadyApproved = errors.New("compliance: the operator already approved this erasure request")

// ErrErasureModeMismatch is returned when the mode differs from the one the
// request locked.
var ErrErasureModeMismatch = errors.New("compliance: the erasure mode differs from the one the request locked")

const (
	minRequiredApprovals = 1
	maxRequiredApprovals = 5
)

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
	Claim(ctx context.Context, uuid, fromStatus, mode, actor string, now, leaseUntil time.Time, approval *models.ErasureApproval) (*models.ErasureRequest, error)
	AddApproval(ctx context.Context, uuid, mode string, a models.ErasureApproval) (*models.ErasureRequest, error)
	HasOpen(ctx context.Context, userUUID string) (string, bool, error)
	ClaimDue(ctx context.Context, now, leaseUntil time.Time, maxAttempts int) (*models.ErasureRequest, error)
	RecordProgress(ctx context.Context, uuid string, leaseUntil time.Time, progress []models.ErasureProducerProgress) error
	Settle(ctx context.Context, uuid string, leaseUntil time.Time, s repository.ErasureSettlement) error
	HasIncomplete(ctx context.Context, userUUID string) (bool, error)
	ClosePartial(ctx context.Context, uuid string, now time.Time, expected []models.ErasureProducerProgress, abandoned []string, closedBy, note string) error
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

	requiredApprovals func(context.Context) int // nil: 1
}

// SetRequiredApprovals installs the source of erasure_required_approvals, read
// when a request is created (Lodge, Open) and snapshotted on the row.
func (s *ErasureRequestService) SetRequiredApprovals(f func(context.Context) int) {
	s.requiredApprovals = f
}

// snapshotApprovals is the configured number of distinct approvals, clamped to 1-5.
func (s *ErasureRequestService) snapshotApprovals(ctx context.Context) int {
	n := minRequiredApprovals
	if s.requiredApprovals != nil {
		n = s.requiredApprovals(ctx)
	}
	return min(max(n, minRequiredApprovals), maxRequiredApprovals)
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

		RequiredApprovals: s.snapshotApprovals(ctx),
	}
	if err := s.repo.Insert(ctx, req); err != nil {
		return nil, err
	}
	return req, nil
}

// OpenInput is an operator's request to erase a subject.
type OpenInput struct {
	UserUUID, Reason, RequestedBy string
	Mode                          iface.EraseMode
}

// Open records a pending erasure request an operator opens for a subject
// (source admin). The reason is mandatory and masked; the requester's approval
// (note: the reason) is already on the row and locks the mode. A held subject,
// a hard delete the retention gate refuses, and a subject with an open request
// are refused and nothing is written. The subject cannot open their own.
func (s *ErasureRequestService) Open(ctx context.Context, in OpenInput) (*models.ErasureRequest, error) {
	reason, valid := NormalizeReason(in.Reason)
	if !valid {
		return nil, ErrReasonRequired
	}
	reason = maskReason(reason)
	if in.RequestedBy == in.UserUUID {
		return nil, ErrErasureSelfApproval
	}
	if err := s.dsr.CheckLegalHold(ctx, in.UserUUID); err != nil {
		return nil, err
	}
	if err := s.dsr.CheckRetention(ctx, in.UserUUID, in.Mode); err != nil {
		return nil, err
	}
	if id, open, err := s.repo.HasOpen(ctx, in.UserUUID); err != nil {
		return nil, err
	} else if open {
		return nil, &ErasureRequestExistsError{UUID: id}
	}
	now := s.now()
	req := &models.ErasureRequest{
		UUID:              uuid.NewString(),
		UserUUID:          in.UserUUID,
		Reason:            reason,
		Status:            models.ErasureRequestPending,
		RequestedAt:       now,
		Source:            models.ErasureSourceAdmin,
		RequestedBy:       in.RequestedBy,
		Mode:              eraseModeLabel(in.Mode),
		RequiredApprovals: s.snapshotApprovals(ctx),
		Approvals:         []models.ErasureApproval{{By: in.RequestedBy, At: now, Note: reason}},
	}
	if err := s.repo.Insert(ctx, req); err != nil {
		if errors.Is(err, repository.ErrErasureOpenExists) {
			// Another operator won the race: name the request that holds the subject.
			if id, open, herr := s.repo.HasOpen(ctx, in.UserUUID); herr == nil && open {
				return nil, &ErasureRequestExistsError{UUID: id}
			}
			return nil, ErrErasureRequestExists
		}
		return nil, err
	}
	if s.sink != nil {
		s.sink.Emit(ctx, iface.AuditEvent{
			ActorUserID:  in.RequestedBy,
			ActorType:    "user",
			Action:       "gdpr.dsr.erasure_opened",
			ResourceType: "erasure_request",
			ResourceID:   req.UUID,
			Outcome:      "success",
			Metadata: map[string]any{
				"subjectUserUuid":   in.UserUUID,
				"mode":              req.Mode,
				"requiredApprovals": req.RequiredApprovals,
			},
		})
	}
	return req, nil
}

// Approve records the operator's approval of a pending request and locks its
// mode on the first one. The subject cannot approve; the note is mandatory and
// masked. A lost compare-and-set is resolved by re-reading the row, so the
// caller gets the precise sentinel.
func (s *ErasureRequestService) Approve(ctx context.Context, requestUUID string, mode iface.EraseMode, actor, note string) (*models.ErasureRequest, error) {
	note, valid := NormalizeReason(note)
	if !valid {
		return nil, ErrReasonRequired
	}
	note = maskReason(note)
	row, err := s.repo.Get(ctx, requestUUID)
	if err != nil {
		return nil, err
	}
	if row.Status != models.ErasureRequestPending {
		return nil, repository.ErrErasureRequestNotFound
	}
	if actor == row.UserUUID {
		return nil, ErrErasureSelfApproval
	}
	label := eraseModeLabel(mode)
	got, err := s.repo.AddApproval(ctx, requestUUID, label, models.ErasureApproval{By: actor, At: s.now(), Note: note})
	if errors.Is(err, repository.ErrErasureApprovalConflict) {
		return nil, s.approvalConflict(ctx, requestUUID, label, actor)
	}
	if err != nil {
		return nil, err
	}
	if s.sink != nil {
		s.sink.Emit(ctx, iface.AuditEvent{
			ActorUserID:  actor,
			ActorType:    "user",
			Action:       "gdpr.dsr.erasure_approved",
			ResourceType: "erasure_request",
			ResourceID:   requestUUID,
			Outcome:      "success",
			Metadata: map[string]any{
				"subjectUserUuid": got.UserUUID,
				"mode":            got.Mode,
				"approvals":       len(got.Approvals),
				"required":        got.RequiredApprovals,
			},
		})
	}
	return got, nil
}

// approvalConflict re-reads the request after a lost approval compare-and-set
// and names why it lost.
func (s *ErasureRequestService) approvalConflict(ctx context.Context, requestUUID, label, actor string) error {
	cur, err := s.repo.Get(ctx, requestUUID)
	if err != nil {
		return err
	}
	switch {
	case cur.Status != models.ErasureRequestPending:
		return repository.ErrErasureRequestNotFound
	case approvedBy(cur, actor):
		return ErrErasureAlreadyApproved
	case cur.Mode != "" && cur.Mode != label:
		return ErrErasureModeMismatch
	}
	return repository.ErrErasureApprovalConflict
}

func approvedBy(r *models.ErasureRequest, by string) bool {
	for _, a := range r.Approvals {
		if a.By == by {
			return true
		}
	}
	return false
}

// ListOpen returns requests awaiting operator action or further erasure work.
func (s *ErasureRequestService) ListOpen(ctx context.Context) ([]models.ErasureRequest, error) {
	rows, err := s.repo.ListByStatus(ctx, models.ErasureRequestPending, models.ErasureRequestIncomplete)
	if err != nil {
		return nil, err
	}
	for i := range rows {
		if rows[i].Status == models.ErasureRequestIncomplete {
			_, rows[i].Closable = s.closable(rows[i].Progress)
		}
	}
	return rows, nil
}

// closable reports the abandoned keys of a request with progress, judged
// against the producers registered now: the progress is first completed with
// PlanProgress, so a registered producer that never ran for the subject is
// pending and keeps the request open.
func (s *ErasureRequestService) closable(progress []models.ErasureProducerProgress) ([]string, bool) {
	plan, _ := s.dsr.PlanProgress(progress)
	return abandonedKeys(plan, s.dsr.RegisteredKeys())
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
// gone from this binary, recording them as abandoned. A registered producer
// missing from the progress (added since the last run) has not erased the
// subject's data, so it makes the request not closable. The write is fenced by
// the progress checked here: a run that changed it meanwhile makes the close
// fail with repository.ErrErasureRequestBusy. Nothing is erased, so the legal
// hold does not gate it. The note is masked before it is stored.
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
	keys, ok := s.closable(row.Progress)
	if !ok {
		return nil, ErrErasureRequestNotClosable
	}
	if err := s.repo.ClosePartial(ctx, requestUUID, s.now(), row.Progress, keys, closedBy, note); err != nil {
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
	return s.ExecuteInMode(ctx, requestUUID, eraseModeLabel(mode), resolvedBy)
}

// ExecuteInMode is Execute with the mode as its label ("hard_delete",
// "anonymize") or "" for no choice: a pending request then runs in the mode
// its approvals locked, else as a hard delete. A label that differs from a
// locked mode is ErrErasureModeMismatch, judged before the retention gate so
// the gate always sees the mode that will run.
func (s *ErasureRequestService) ExecuteInMode(ctx context.Context, requestUUID, label, resolvedBy string) (*ErasureOutcome, error) {
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
	// An incomplete request keeps its stored mode; a pending one runs in its
	// locked mode, or the one chosen now when none is locked yet.
	effective := label
	switch {
	case req.Status == models.ErasureRequestIncomplete:
		effective = req.Mode
	case req.Mode != "" && label != "" && label != req.Mode:
		return nil, ErrErasureModeMismatch
	case req.Mode != "":
		effective = req.Mode
	case label == "":
		effective = eraseModeLabel(iface.EraseHardDelete)
	}
	if err := s.dsr.CheckRetention(ctx, req.UserUUID, parseEraseMode(effective)); err != nil {
		return nil, err
	}
	label = effective
	// The executor of a pending request counts as an approver (when not one
	// already); incomplete requests (retries) skip approvals.
	var approval *models.ErasureApproval
	if req.Status == models.ErasureRequestPending {
		if resolvedBy == req.UserUUID {
			return nil, ErrErasureSelfApproval
		}
		have := distinctApprovers(req, resolvedBy)
		if need := max(1, req.RequiredApprovals); have < need {
			return nil, &ErasureApprovalsMissingError{Have: have, Need: need}
		}
		if !approvedBy(req, resolvedBy) {
			approval = &models.ErasureApproval{By: resolvedBy, At: s.now(), Note: "executed"}
		}
	}
	now := s.now()
	row, err := s.repo.Claim(ctx, requestUUID, req.Status, label, resolvedBy, now, now.Add(erasureRunLease), approval)
	if errors.Is(err, repository.ErrErasureApprovalConflict) {
		// A concurrent approval changed the mode or added this operator.
		cerr := s.approvalConflict(ctx, requestUUID, label, resolvedBy)
		if !errors.Is(cerr, ErrErasureAlreadyApproved) {
			return nil, cerr
		}
		// The operator's own approval landed meanwhile: they already count,
		// so claim once more without adding one.
		now = s.now()
		row, err = s.repo.Claim(ctx, requestUUID, req.Status, label, resolvedBy, now, now.Add(erasureRunLease), nil)
		if errors.Is(err, repository.ErrErasureApprovalConflict) {
			return nil, repository.ErrErasureRequestBusy
		}
	}
	if err != nil {
		return nil, err
	}
	return s.run(ctx, row)
}

// distinctApprovers counts the distinct operators on the request plus actor.
func distinctApprovers(r *models.ErasureRequest, actor string) int {
	seen := map[string]struct{}{actor: {}}
	for _, a := range r.Approvals {
		seen[a.By] = struct{}{}
	}
	return len(seen)
}

// Reject closes a pending request without erasing. An incomplete request has
// already started erasing and is refused.
func (s *ErasureRequestService) Reject(ctx context.Context, requestUUID, resolvedBy, note string) error {
	if req, err := s.repo.Get(ctx, requestUUID); err == nil && req.Status == models.ErasureRequestIncomplete {
		return ErrErasureRequestStarted
	}
	return s.repo.Resolve(ctx, requestUUID, models.ErasureRequestRejected, resolvedBy, "", note)
}

// EraseNow records an incomplete, leased request for the subject (attempt 1),
// with every registered producer pending, and runs the erasure at once. The
// row is written before any producer runs, so a crash leaves a request the
// worker finishes. A subject already under a legal hold records nothing.
func (s *ErasureRequestService) EraseNow(ctx context.Context, userUUID, tenantID, source, actor string, mode iface.EraseMode) (*ErasureOutcome, error) {
	if err := s.dsr.CheckLegalHold(ctx, userUUID); err != nil {
		return nil, err
	}
	if err := s.dsr.CheckRetention(ctx, userUUID, mode); err != nil {
		return nil, err
	}
	now := s.now()
	lease := now.Add(erasureRunLease)
	plan, _ := s.dsr.PlanProgress(nil)
	row := &models.ErasureRequest{
		UUID:          uuid.NewString(),
		UserUUID:      userUUID,
		TenantID:      tenantID,
		Status:        models.ErasureRequestIncomplete,
		Source:        source,
		Mode:          eraseModeLabel(mode),
		ResolvedBy:    actor,
		RequestedAt:   now,
		Progress:      plan,
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
// outcome. Before any producer runs, every registered producer missing from
// the stored progress is recorded as pending (under the lease); when that
// fails no producer runs and the error is returned, the row staying incomplete
// for the worker. The attempt was counted when the row was claimed, so
// row.Attempts is this run's number. The run survives caller cancellation (bounded by half
// the lease) and Settle is bounded by what is left of the lease (at least
// settleFloor). A run error is returned after settling. When Settle fails or
// the lease was lost the outcome is not stored: the request is reported as it
// is stored, incomplete, and the worker re-runs the remaining producers once
// the lease expires (or the newer holder's state stands).
func (s *ErasureRequestService) run(ctx context.Context, row *models.ErasureRequest) (*ErasureOutcome, error) {
	runCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), erasureRunLease/2)
	defer cancel()
	if plan, added := s.dsr.PlanProgress(row.Progress); added {
		if err := s.repo.RecordProgress(runCtx, row.UUID, *row.LeaseUntil, plan); err != nil {
			s.logger.Warn("compliance: could not record the producers of an erasure run; no producer ran and the worker will retry",
				slog.String("request", row.UUID), slog.String("error", err.Error()))
			return nil, err
		}
		row.Progress = plan
	}
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
	case errors.Is(runErr, ErrRetentionMinimum):
		st.LastError = models.ErasureLastErrorRetention
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
