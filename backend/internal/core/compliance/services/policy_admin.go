package services

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/orkestra/backend/internal/core/compliance/models"
	"github.com/orkestra/backend/internal/core/compliance/repository"
	"github.com/orkestra/backend/internal/shared/utils"
	"github.com/orkestra/backend/pkg/sdk/ctxauth"
	"github.com/orkestra/backend/pkg/sdk/iface"
)

var (
	ErrReasonRequired          = errors.New("compliance: a reason of 1 to 500 characters is required")
	ErrPlatformProtected       = errors.New("compliance: the platform policy cannot be deleted or assigned")
	ErrPolicyInUse             = errors.New("compliance: the policy is assigned to at least one tenant")
	ErrSelfApproval            = errors.New("compliance: a change request cannot be decided by its author")
	ErrChangeRequestSuperseded = errors.New("compliance: the state changed since the request, which is now superseded")
	ErrTenantNotFound          = errors.New("compliance: tenant not found")
	ErrPolicyUnavailable       = errors.New("compliance: the policy snapshot is not loaded yet")
)

// ValidationError carries the blocking issues of a change.
type ValidationError struct{ Result ValidationResult }

func (e *ValidationError) Error() string {
	return "compliance: invalid policy: " + strings.Join(e.Result.ErrorCodes(), ", ")
}

// WarningsError refuses a change whose warnings were not acknowledged.
type WarningsError struct{ Codes []string }

func (e *WarningsError) Error() string {
	return "compliance: warnings not acknowledged: " + strings.Join(e.Codes, ", ")
}

// changeRequestTTL is how long a request stays pending (spec §1.5).
const changeRequestTTL = 14 * 24 * time.Hour

const (
	expiryInterval   = time.Hour
	expiryFirstDelay = time.Minute
)

type policyRepo interface {
	WithTxn(ctx context.Context, fn func(ctx context.Context) error) error
	InsertPolicy(ctx context.Context, p *models.Policy) error
	GetPolicy(ctx context.Context, uuid string) (*models.Policy, error)
	GetPlatformPolicy(ctx context.Context) (*models.Policy, error)
	ListPolicies(ctx context.Context) ([]models.Policy, error)
	NameTaken(ctx context.Context, name, exceptUUID string) (bool, error)
	ReplacePolicy(ctx context.Context, p *models.Policy, expectedVersion int) error
	DeletePolicy(ctx context.Context, uuid string, expectedVersion int) error
	InsertVersion(ctx context.Context, v *models.PolicyVersion) error
	ListVersions(ctx context.Context, policyUUID string) ([]models.PolicyVersion, error)
	GetAssignment(ctx context.Context, tenantID string) (*models.PolicyAssignment, error)
	ListAssignments(ctx context.Context) ([]models.PolicyAssignment, error)
	CountAssignments(ctx context.Context, policyUUID string) (int64, error)
	UpsertAssignment(ctx context.Context, a *models.PolicyAssignment) error
	DeleteAssignment(ctx context.Context, tenantID string) error
	InsertAssignmentHistory(ctx context.Context, h *models.PolicyAssignmentHistory) error
	InsertChangeRequest(ctx context.Context, cr *models.PolicyChangeRequest) error
	GetChangeRequest(ctx context.Context, uuid string) (*models.PolicyChangeRequest, error)
	ListChangeRequests(ctx context.Context, status string) ([]models.PolicyChangeRequest, error)
	DecideChangeRequest(ctx context.Context, uuid, status, decidedBy, note string, at time.Time) error
	ListPendingBefore(ctx context.Context, before time.Time) ([]models.PolicyChangeRequest, error)
}

type tenantLookup interface {
	GetTenant(ctx context.Context, tenantUUID string) (*iface.Tenant, error)
}

// Actor is who performs a change: the operator's UUID, the operator's
// current (internal) tenant and client IP, recorded on the audit event.
type Actor struct {
	UserID   string
	TenantID string
	IP       string
}

func ActorFromContext(ctx context.Context) Actor {
	var a Actor
	a.UserID, _ = ctxauth.GetUserUUID(ctx)
	a.TenantID, _ = ctxauth.GetTenantID(ctx)
	a.IP, _ = ctxauth.GetClientIP(ctx)
	return a
}

type WriteResult struct {
	Applied       bool
	Policy        *models.Policy
	Assignment    *models.PolicyAssignment
	ChangeRequest *models.PolicyChangeRequest
	Warnings      []PolicyIssue
}

type PolicyView struct {
	models.Policy
	AssignedTenants       int      `json:"assignedTenants"`
	LessRestrictiveFields []string `json:"lessRestrictiveFields"`
	ReviewOverdue         bool     `json:"reviewOverdue"`
}

type AssignmentView struct {
	models.PolicyAssignment
	TenantName string `json:"tenantName,omitempty"`
	PolicyName string `json:"policyName,omitempty"`
}

type PolicyDetail struct {
	Policy      models.Policy    `json:"policy"`
	Assignments []AssignmentView `json:"assignments"`
}

// approval is the second operator's decision a change is applied under.
type approval struct {
	request *models.PolicyChangeRequest
	by      Actor
	note    string
}

// PolicyAdminService validates and applies policy-engine changes (spec
// §1.4–1.5). Every write runs in one Mongo transaction with its version or
// history row; the audit event follows the commit (the T3 outbox makes it
// atomic).
type PolicyAdminService struct {
	repo     policyRepo
	policies *PolicyService
	tenants  tenantLookup
	sink     iface.AuditSink
	fourEyes func(context.Context) bool
	logger   *slog.Logger
	now      func() time.Time
	newUUID  func() string
}

func NewPolicyAdminService(repo policyRepo, policies *PolicyService, tenants tenantLookup, sink iface.AuditSink,
	fourEyes func(context.Context) bool, logger *slog.Logger) *PolicyAdminService {
	return &PolicyAdminService{repo: repo, policies: policies, tenants: tenants, sink: sink, fourEyes: fourEyes,
		logger: logger, now: time.Now, newUUID: uuid.NewString}
}

// evidenceTextPolicy scans free text kept as compliance evidence: e-mails,
// IBANs and codici fiscali are masked; IP addresses stay whole, as in the
// evidence core (spec §3.1).
var evidenceTextPolicy = func() iface.LogContentPolicy {
	p := iface.DefaultLogContentPolicy()
	p.IPAddress = iface.IPAddressFull
	return p
}()

// maskReason masks personal data typed into a reason or a note before it is
// stored (spec §1.3). Idempotent.
func maskReason(s string) string {
	out, _ := utils.MaskKV(&evidenceTextPolicy, nil, "reason", s)
	str, ok := out.(string)
	if !ok {
		return "[REDACTED]"
	}
	return str
}

// EnsurePlatformPolicy creates the platform policy on first boot (spec
// §10.1). Concurrent replicas race on the unique indexes; the loser finds the
// winner's policy and returns nil.
func (s *PolicyAdminService) EnsurePlatformPolicy(ctx context.Context) error {
	if _, err := s.repo.GetPlatformPolicy(ctx); err == nil || !errors.Is(err, repository.ErrPolicyNotFound) {
		return err
	}
	p := models.NewPlatformPolicy(s.newUUID(), s.now().UTC())
	err := s.repo.WithTxn(ctx, func(ctx context.Context) error {
		if err := s.repo.InsertPolicy(ctx, &p); err != nil {
			return err
		}
		return s.repo.InsertVersion(ctx, s.version(p, models.VersionCreate, models.SystemActor, p.CreatedAt, nil))
	})
	if err != nil {
		// A concurrent replica may have created the platform policy first;
		// Mongo reports either the name or the platform-flag duplicate. If
		// the policy exists now, the boot succeeded (the winner audits it).
		if _, gerr := s.repo.GetPlatformPolicy(ctx); gerr == nil {
			return nil
		}
		return err
	}
	s.emit(ctx, Actor{UserID: models.SystemActor}, "compliance.policy.created", "compliance_policy", p.UUID,
		map[string]any{"version": p.Version, "platform": true, "after": p.Input(), "reason": p.ChangeReason})
	return nil
}

// --- policies ---

func (s *PolicyAdminService) Create(ctx context.Context, actor Actor, in models.PolicyInput, reason string, ack bool) (*WriteResult, error) {
	reason, ok := NormalizeReason(reason)
	if !ok {
		return nil, ErrReasonRequired
	}
	res, in, err := s.evaluatePolicy(ctx, nil, in)
	if err != nil {
		return nil, err
	}
	if len(res.Errors) > 0 {
		return nil, &ValidationError{Result: res}
	}
	cr := &models.PolicyChangeRequest{Kind: models.ChangeCreate, Payload: models.ChangePayload{Policy: &in}}
	return s.gate(ctx, actor, res.Warnings, ack, reason, cr, func(ctx context.Context) (*WriteResult, error) {
		return s.applyCreate(ctx, actor, actor.UserID, in, reason, nil)
	})
}

func (s *PolicyAdminService) Update(ctx context.Context, actor Actor, policyUUID string, expectedVersion int, in models.PolicyInput, reason string, ack bool) (*WriteResult, error) {
	reason, ok := NormalizeReason(reason)
	if !ok {
		return nil, ErrReasonRequired
	}
	cur, err := s.repo.GetPolicy(ctx, policyUUID)
	if err != nil {
		return nil, err
	}
	if cur.Version != expectedVersion {
		return nil, repository.ErrPolicyVersionConflict
	}
	res, in, err := s.evaluatePolicy(ctx, cur, in)
	if err != nil {
		return nil, err
	}
	if len(res.Errors) > 0 {
		return nil, &ValidationError{Result: res}
	}
	cr := &models.PolicyChangeRequest{Kind: models.ChangeUpdate, PolicyUUID: policyUUID, ExpectedVersion: expectedVersion,
		Payload: models.ChangePayload{Policy: &in}}
	return s.gate(ctx, actor, res.Warnings, ack, reason, cr, func(ctx context.Context) (*WriteResult, error) {
		return s.applyUpdate(ctx, actor, actor.UserID, policyUUID, expectedVersion, in, reason, nil)
	})
}

// ValidateDraft is POST policies/validate: the issues of in as a new tenant
// policy (policyUUID == "") or as the next version of policyUUID.
func (s *PolicyAdminService) ValidateDraft(ctx context.Context, policyUUID string, in models.PolicyInput) (ValidationResult, error) {
	var cur *models.Policy
	if policyUUID != "" {
		p, err := s.repo.GetPolicy(ctx, policyUUID)
		if err != nil {
			return ValidationResult{}, err
		}
		cur = p
	}
	res, _, err := s.evaluatePolicy(ctx, cur, in)
	return res, err
}

// evaluatePolicy normalizes in as the next state of cur (nil for a new
// tenant policy) and returns every issue, including the weakening of cur.
func (s *PolicyAdminService) evaluatePolicy(ctx context.Context, cur *models.Policy, in models.PolicyInput) (ValidationResult, models.PolicyInput, error) {
	isPlatform := cur != nil && cur.IsPlatformDefault
	in = NormalizePolicyInput(in, isPlatform)
	except := ""
	if cur != nil {
		except = cur.UUID
	}
	taken, err := s.repo.NameTaken(ctx, in.Name, except)
	if err != nil {
		return ValidationResult{}, in, err
	}
	platform, err := s.repo.GetPlatformPolicy(ctx)
	if err != nil {
		return ValidationResult{}, in, err
	}
	check := PolicyCheck{Input: in, IsPlatform: isPlatform, NameTaken: taken, Now: s.now(), Current: cur}
	if !isPlatform {
		check.Platform = platform
	}
	res := ValidatePolicy(check)
	if cur != nil && len(res.Errors) == 0 {
		next := withInput(*cur, in)
		basePlatform := platform
		if isPlatform {
			basePlatform = &next
		}
		res.Warnings = append(res.Warnings, LessRestrictiveThanCurrent(&next, cur, basePlatform)...)
	}
	return res, in, nil
}

// withInput returns p with the editable fields of in; a tenant policy never
// keeps a sinks section.
func withInput(p models.Policy, in models.PolicyInput) models.Policy {
	p.Name, p.Description = in.Name, in.Description
	p.LogContent, p.Retention, p.Accountability = in.LogContent, in.Retention, in.Accountability
	p.Sinks = nil
	if p.IsPlatformDefault {
		p.Sinks = in.Sinks
	}
	return p
}

func (s *PolicyAdminService) Delete(ctx context.Context, actor Actor, policyUUID string, expectedVersion int, reason string) error {
	reason, ok := NormalizeReason(reason)
	if !ok {
		return ErrReasonRequired
	}
	now := s.now().UTC()
	var before models.Policy
	err := s.repo.WithTxn(ctx, func(ctx context.Context) error {
		p, err := s.repo.GetPolicy(ctx, policyUUID)
		if err != nil {
			return err
		}
		if p.IsPlatformDefault {
			return ErrPlatformProtected
		}
		if p.Version != expectedVersion {
			return repository.ErrPolicyVersionConflict
		}
		n, err := s.repo.CountAssignments(ctx, policyUUID)
		if err != nil {
			return err
		}
		if n > 0 {
			return ErrPolicyInUse
		}
		before = *p
		gone := *p
		gone.Version++
		gone.UpdatedBy, gone.UpdatedAt, gone.ChangeReason = actor.UserID, now, maskReason(reason)
		if err := s.repo.DeletePolicy(ctx, policyUUID, expectedVersion); err != nil {
			return err
		}
		return s.repo.InsertVersion(ctx, s.version(gone, models.VersionDelete, actor.UserID, now, nil))
	})
	if err != nil {
		return err
	}
	s.afterWrite(ctx)
	s.emit(ctx, actor, "compliance.policy.deleted", "compliance_policy", policyUUID,
		map[string]any{"version": before.Version + 1, "before": before.Input(), "reason": maskReason(reason)})
	return nil
}

// --- assignments ---

func (s *PolicyAdminService) Assign(ctx context.Context, actor Actor, tenantID, policyUUID, reason string, ack bool) (*WriteResult, error) {
	reason, ok := NormalizeReason(reason)
	if !ok {
		return nil, ErrReasonRequired
	}
	tenant, err := s.lookupTenant(ctx, tenantID)
	if err != nil {
		return nil, err
	}
	warnings, target, prev, err := s.assignmentWarnings(ctx, tenantID, policyUUID)
	if err != nil {
		return nil, err
	}
	cr := &models.PolicyChangeRequest{Kind: models.ChangeAssign, PolicyUUID: policyUUID, TenantID: tenantID,
		ExpectedVersion: target.Version,
		Payload:         models.ChangePayload{TenantID: tenantID, PolicyUUID: policyUUID, PreviousPolicyUUID: prev}}
	return s.gate(ctx, actor, warnings, ack, reason, cr, func(ctx context.Context) (*WriteResult, error) {
		return s.applyAssign(ctx, actor, actor.UserID, tenant, policyUUID, target.Version, prev, reason, nil)
	})
}

func (s *PolicyAdminService) Unassign(ctx context.Context, actor Actor, tenantID, reason string, ack bool) (*WriteResult, error) {
	reason, ok := NormalizeReason(reason)
	if !ok {
		return nil, ErrReasonRequired
	}
	tenant, err := s.lookupTenant(ctx, tenantID)
	if err != nil {
		return nil, err
	}
	warnings, _, prev, err := s.assignmentWarnings(ctx, tenantID, "")
	if err != nil {
		return nil, err
	}
	cr := &models.PolicyChangeRequest{Kind: models.ChangeUnassign, PolicyUUID: prev, TenantID: tenantID,
		Payload: models.ChangePayload{TenantID: tenantID, PreviousPolicyUUID: prev}}
	return s.gate(ctx, actor, warnings, ack, reason, cr, func(ctx context.Context) (*WriteResult, error) {
		return s.applyUnassign(ctx, actor, actor.UserID, tenant, prev, reason, nil)
	})
}

// ValidateAssignment returns the warnings of assigning policyUUID to the
// tenant, or of removing its assignment when policyUUID is "".
func (s *PolicyAdminService) ValidateAssignment(ctx context.Context, tenantID, policyUUID string) (ValidationResult, error) {
	if _, err := s.lookupTenant(ctx, tenantID); err != nil {
		return ValidationResult{}, err
	}
	warnings, _, _, err := s.assignmentWarnings(ctx, tenantID, policyUUID)
	return ValidationResult{Warnings: warnings}, err
}

// assignmentWarnings computes the warnings of moving the tenant to
// policyUUID ("" = back to the platform). It returns the target policy (the
// platform one for a removal) and the tenant's current assignment.
func (s *PolicyAdminService) assignmentWarnings(ctx context.Context, tenantID, policyUUID string) ([]PolicyIssue, *models.Policy, string, error) {
	platform, err := s.repo.GetPlatformPolicy(ctx)
	if err != nil {
		return nil, nil, "", err
	}
	current, prev, err := s.currentFor(ctx, tenantID, platform)
	if err != nil {
		return nil, nil, "", err
	}
	if policyUUID == "" {
		if prev == "" {
			return nil, nil, "", repository.ErrAssignmentNotFound
		}
		return LessRestrictiveThanCurrent(platform, current, platform), platform, prev, nil
	}
	target, err := s.repo.GetPolicy(ctx, policyUUID)
	if err != nil {
		return nil, nil, "", err
	}
	if target.IsPlatformDefault {
		return nil, nil, "", ErrPlatformProtected
	}
	w := PolicyWarnings(target.Input(), false, platform, s.now())
	w = append(w, LessRestrictiveThanCurrent(target, current, platform)...)
	return w, target, prev, nil
}

// currentFor returns the policy the tenant is under and its assignment's
// policy UUID ("" when unassigned). An assignment whose policy vanished
// counts as the platform policy here (the resolver gives it the strictest).
func (s *PolicyAdminService) currentFor(ctx context.Context, tenantID string, platform *models.Policy) (*models.Policy, string, error) {
	a, err := s.repo.GetAssignment(ctx, tenantID)
	if errors.Is(err, repository.ErrAssignmentNotFound) {
		return platform, "", nil
	}
	if err != nil {
		return nil, "", err
	}
	p, err := s.repo.GetPolicy(ctx, a.PolicyUUID)
	if errors.Is(err, repository.ErrPolicyNotFound) {
		return platform, a.PolicyUUID, nil
	}
	if err != nil {
		return nil, "", err
	}
	return p, a.PolicyUUID, nil
}

func (s *PolicyAdminService) lookupTenant(ctx context.Context, tenantID string) (*iface.Tenant, error) {
	if s.tenants == nil {
		return nil, errors.New("compliance: tenant provider not available")
	}
	t, err := s.tenants.GetTenant(ctx, tenantID)
	if errors.Is(err, iface.ErrTenantNotFound) || (err == nil && t == nil) {
		return nil, ErrTenantNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("compliance: tenant lookup: %w", err)
	}
	return t, nil
}

// --- four eyes ---

// gate applies a change at once when it has no warnings, refuses it when
// its warnings are not acknowledged, applies it when four eyes is off, and
// otherwise stores it as a pending change request (spec §1.5).
func (s *PolicyAdminService) gate(ctx context.Context, actor Actor, warnings []PolicyIssue, ack bool, reason string,
	cr *models.PolicyChangeRequest, apply func(context.Context) (*WriteResult, error)) (*WriteResult, error) {
	codes := uniqueCodes(warnings)
	if len(codes) > 0 && !ack {
		return nil, &WarningsError{Codes: codes}
	}
	if len(codes) == 0 || !s.fourEyes(ctx) {
		res, err := apply(ctx)
		if err != nil {
			return nil, err
		}
		res.Warnings = warnings
		return res, nil
	}
	cr.UUID = s.newUUID()
	cr.Warnings = codes
	cr.Reason = maskReason(reason)
	cr.RequestedBy = actor.UserID
	cr.RequestedAt = s.now().UTC()
	cr.Status = models.ChangeStatusPending
	if err := s.repo.InsertChangeRequest(ctx, cr); err != nil {
		return nil, err
	}
	s.emit(ctx, actor, "compliance.change_request.created", "compliance_change_request", cr.UUID, map[string]any{
		"kind": cr.Kind, "policyUuid": cr.PolicyUUID, "tenantId": cr.TenantID, "warnings": codes, "reason": cr.Reason,
	})
	return &WriteResult{ChangeRequest: cr, Warnings: warnings}, nil
}

func (s *PolicyAdminService) Approve(ctx context.Context, actor Actor, requestUUID, note string) (*WriteResult, error) {
	note, ok := NormalizeReason(note)
	if !ok {
		return nil, ErrReasonRequired
	}
	cr, err := s.repo.GetChangeRequest(ctx, requestUUID)
	if err != nil {
		return nil, err
	}
	if cr.Status != models.ChangeStatusPending {
		return nil, repository.ErrChangeRequestNotPending
	}
	if cr.RequestedBy == actor.UserID {
		return nil, ErrSelfApproval
	}
	ap := &approval{request: cr, by: actor, note: maskReason(note)}
	res, err := s.applyRequest(ctx, cr, ap)
	if isStale(err) {
		// Only the call that wins the compare-and-set records the outcome: if
		// a concurrent approver already decided the request, this one lost
		// the race and reports it as not pending, without an audit event.
		if derr := s.repo.DecideChangeRequest(ctx, cr.UUID, models.ChangeStatusSuperseded, actor.UserID, ap.note, s.now().UTC()); derr != nil {
			return nil, derr
		}
		s.emit(ctx, actor, "compliance.change_request.superseded", "compliance_change_request", cr.UUID,
			map[string]any{"kind": cr.Kind, "requestedBy": cr.RequestedBy, "cause": err.Error()})
		return nil, ErrChangeRequestSuperseded
	}
	if err != nil {
		return nil, err
	}
	s.emit(ctx, actor, "compliance.change_request.approved", "compliance_change_request", cr.UUID,
		map[string]any{"kind": cr.Kind, "requestedBy": cr.RequestedBy, "note": ap.note})
	return res, nil
}

// isStale tells a change that no longer fits the current state (superseded)
// from a failure.
func isStale(err error) bool {
	var verr *ValidationError
	return errors.Is(err, repository.ErrPolicyVersionConflict) || errors.Is(err, repository.ErrPolicyNameTaken) ||
		errors.Is(err, repository.ErrPolicyNotFound) || errors.Is(err, repository.ErrAssignmentNotFound) ||
		errors.Is(err, ErrTenantNotFound) || errors.Is(err, ErrPlatformProtected) || errors.As(err, &verr)
}

// applyRequest revalidates a request against the current state and applies
// it on behalf of its author.
func (s *PolicyAdminService) applyRequest(ctx context.Context, cr *models.PolicyChangeRequest, ap *approval) (*WriteResult, error) {
	author := cr.RequestedBy
	switch cr.Kind {
	case models.ChangeCreate, models.ChangeUpdate:
		if cr.Payload.Policy == nil {
			return nil, fmt.Errorf("compliance: change request %s has no policy payload", cr.UUID)
		}
		var cur *models.Policy
		if cr.Kind == models.ChangeUpdate {
			p, err := s.repo.GetPolicy(ctx, cr.PolicyUUID)
			if err != nil {
				return nil, err
			}
			if p.Version != cr.ExpectedVersion {
				return nil, repository.ErrPolicyVersionConflict
			}
			cur = p
		}
		res, in, err := s.evaluatePolicy(ctx, cur, *cr.Payload.Policy)
		if err != nil {
			return nil, err
		}
		if len(res.Errors) > 0 {
			return nil, &ValidationError{Result: res}
		}
		if cur == nil {
			return s.applyCreate(ctx, ap.by, author, in, cr.Reason, ap)
		}
		return s.applyUpdate(ctx, ap.by, author, cr.PolicyUUID, cr.ExpectedVersion, in, cr.Reason, ap)
	case models.ChangeAssign:
		tenant, err := s.lookupTenant(ctx, cr.TenantID)
		if err != nil {
			return nil, err
		}
		return s.applyAssign(ctx, ap.by, author, tenant, cr.PolicyUUID, cr.ExpectedVersion, cr.Payload.PreviousPolicyUUID, cr.Reason, ap)
	case models.ChangeUnassign:
		tenant, err := s.lookupTenant(ctx, cr.TenantID)
		if err != nil {
			return nil, err
		}
		return s.applyUnassign(ctx, ap.by, author, tenant, cr.Payload.PreviousPolicyUUID, cr.Reason, ap)
	}
	return nil, fmt.Errorf("compliance: unknown change request kind %q", cr.Kind)
}

func (s *PolicyAdminService) Reject(ctx context.Context, actor Actor, requestUUID, note string) (*models.PolicyChangeRequest, error) {
	note, ok := NormalizeReason(note)
	if !ok {
		return nil, ErrReasonRequired
	}
	cr, err := s.repo.GetChangeRequest(ctx, requestUUID)
	if err != nil {
		return nil, err
	}
	if cr.Status != models.ChangeStatusPending {
		return nil, repository.ErrChangeRequestNotPending
	}
	if cr.RequestedBy == actor.UserID {
		return nil, ErrSelfApproval
	}
	now := s.now().UTC()
	note = maskReason(note)
	if err := s.repo.DecideChangeRequest(ctx, cr.UUID, models.ChangeStatusRejected, actor.UserID, note, now); err != nil {
		return nil, err
	}
	cr.Status, cr.DecidedBy, cr.DecidedAt, cr.DecisionNote = models.ChangeStatusRejected, actor.UserID, &now, note
	s.emit(ctx, actor, "compliance.change_request.rejected", "compliance_change_request", cr.UUID,
		map[string]any{"kind": cr.Kind, "requestedBy": cr.RequestedBy, "note": note})
	return cr, nil
}

// ExpireStale expires the requests pending for more than 14 days.
func (s *PolicyAdminService) ExpireStale(ctx context.Context) (int, error) {
	now := s.now().UTC()
	pending, err := s.repo.ListPendingBefore(ctx, now.Add(-changeRequestTTL))
	if err != nil {
		return 0, err
	}
	n := 0
	for _, cr := range pending {
		err := s.repo.DecideChangeRequest(ctx, cr.UUID, models.ChangeStatusExpired, models.SystemActor, "", now)
		if errors.Is(err, repository.ErrChangeRequestNotPending) {
			continue
		}
		if err != nil {
			return n, err
		}
		n++
		s.emit(ctx, Actor{UserID: models.SystemActor}, "compliance.change_request.expired", "compliance_change_request", cr.UUID,
			map[string]any{"kind": cr.Kind, "requestedBy": cr.RequestedBy, "requestedAt": cr.RequestedAt})
	}
	return n, nil
}

// ExpiryLoop runs ExpireStale every hour (first run a minute after start).
// Every replica may run it: the pending compare-and-set makes it idempotent.
func (s *PolicyAdminService) ExpiryLoop(ctx context.Context, stop <-chan struct{}) {
	timer := time.NewTimer(expiryFirstDelay)
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-stop:
			return
		case <-timer.C:
			if _, err := s.ExpireStale(ctx); err != nil {
				s.logger.Warn("compliance: change request expiry failed", slog.String("error", err.Error()))
			}
			timer.Reset(expiryInterval)
		}
	}
}

// --- apply (one transaction each) ---

func (s *PolicyAdminService) decide(ctx context.Context, ap *approval, at time.Time) error {
	if ap == nil {
		return nil
	}
	return s.repo.DecideChangeRequest(ctx, ap.request.UUID, models.ChangeStatusApproved, ap.by.UserID, ap.note, at)
}

func (s *PolicyAdminService) version(p models.Policy, kind, author string, at time.Time, ap *approval) *models.PolicyVersion {
	v := &models.PolicyVersion{PolicyUUID: p.UUID, Version: p.Version, ChangeKind: kind, Snapshot: p,
		ChangedBy: author, ChangedAt: at, Reason: p.ChangeReason}
	if ap != nil {
		v.ChangeRequestUUID, v.ApprovedBy = ap.request.UUID, ap.by.UserID
	}
	return v
}

func (s *PolicyAdminService) applyCreate(ctx context.Context, by Actor, author string, in models.PolicyInput, reason string, ap *approval) (*WriteResult, error) {
	now := s.now().UTC()
	p := withInput(models.Policy{UUID: s.newUUID(), Version: 1, CreatedBy: author, CreatedAt: now}, in)
	p.UpdatedBy, p.UpdatedAt, p.ChangeReason = author, now, maskReason(reason)
	err := s.repo.WithTxn(ctx, func(ctx context.Context) error {
		if err := s.decide(ctx, ap, now); err != nil {
			return err
		}
		if err := s.repo.InsertPolicy(ctx, &p); err != nil {
			return err
		}
		return s.repo.InsertVersion(ctx, s.version(p, models.VersionCreate, author, now, ap))
	})
	if err != nil {
		return nil, err
	}
	s.afterWrite(ctx)
	s.emit(ctx, by, "compliance.policy.created", "compliance_policy", p.UUID,
		withApproval(map[string]any{"version": 1, "after": p.Input(), "reason": p.ChangeReason}, author, ap))
	return &WriteResult{Applied: true, Policy: &p}, nil
}

func (s *PolicyAdminService) applyUpdate(ctx context.Context, by Actor, author, policyUUID string, expected int, in models.PolicyInput, reason string, ap *approval) (*WriteResult, error) {
	now := s.now().UTC()
	var before, after models.Policy
	err := s.repo.WithTxn(ctx, func(ctx context.Context) error {
		cur, err := s.repo.GetPolicy(ctx, policyUUID)
		if err != nil {
			return err
		}
		if cur.Version != expected {
			return repository.ErrPolicyVersionConflict
		}
		if err := s.decide(ctx, ap, now); err != nil {
			return err
		}
		before = *cur
		after = withInput(*cur, in)
		after.Version = cur.Version + 1
		after.UpdatedBy, after.UpdatedAt, after.ChangeReason = author, now, maskReason(reason)
		if err := s.repo.ReplacePolicy(ctx, &after, expected); err != nil {
			return err
		}
		return s.repo.InsertVersion(ctx, s.version(after, models.VersionUpdate, author, now, ap))
	})
	if err != nil {
		return nil, err
	}
	s.afterWrite(ctx)
	s.emit(ctx, by, "compliance.policy.updated", "compliance_policy", policyUUID, withApproval(map[string]any{
		"version": after.Version, "before": before.Input(), "after": after.Input(), "reason": after.ChangeReason,
	}, author, ap))
	return &WriteResult{Applied: true, Policy: &after}, nil
}

// checkPrevious fails when the tenant's assignment is no longer prev.
func (s *PolicyAdminService) checkPrevious(ctx context.Context, tenantID, prev string) error {
	cur := ""
	a, err := s.repo.GetAssignment(ctx, tenantID)
	switch {
	case err == nil:
		cur = a.PolicyUUID
	case !errors.Is(err, repository.ErrAssignmentNotFound):
		return err
	}
	if cur != prev {
		return repository.ErrPolicyVersionConflict
	}
	return nil
}

func (s *PolicyAdminService) history(tenant *iface.Tenant, action, policyUUID, prev, author, reason string, at time.Time, ap *approval) *models.PolicyAssignmentHistory {
	h := &models.PolicyAssignmentHistory{UUID: s.newUUID(), TenantID: tenant.UUID, TenantKind: tenant.Kind, Action: action,
		PolicyUUID: policyUUID, PreviousPolicyUUID: prev, ChangedBy: author, ChangedAt: at, Reason: reason}
	if ap != nil {
		h.ChangeRequestUUID, h.ApprovedBy = ap.request.UUID, ap.by.UserID
	}
	return h
}

func (s *PolicyAdminService) applyAssign(ctx context.Context, by Actor, author string, tenant *iface.Tenant, policyUUID string, expected int, prev, reason string, ap *approval) (*WriteResult, error) {
	now := s.now().UTC()
	a := models.PolicyAssignment{TenantID: tenant.UUID, TenantKind: tenant.Kind, PolicyUUID: policyUUID,
		AssignedBy: author, AssignedAt: now, Reason: maskReason(reason)}
	err := s.repo.WithTxn(ctx, func(ctx context.Context) error {
		p, err := s.repo.GetPolicy(ctx, policyUUID)
		if err != nil {
			return err
		}
		if p.IsPlatformDefault {
			return ErrPlatformProtected
		}
		if p.Version != expected {
			return repository.ErrPolicyVersionConflict
		}
		if err := s.checkPrevious(ctx, tenant.UUID, prev); err != nil {
			return err
		}
		if err := s.decide(ctx, ap, now); err != nil {
			return err
		}
		if err := s.repo.UpsertAssignment(ctx, &a); err != nil {
			return err
		}
		return s.repo.InsertAssignmentHistory(ctx, s.history(tenant, models.AssignmentActionAssign, policyUUID, prev, author, a.Reason, now, ap))
	})
	if err != nil {
		return nil, err
	}
	s.afterWrite(ctx)
	s.emit(ctx, by, "compliance.policy.assigned", "tenant", tenant.UUID, withApproval(map[string]any{
		"policyUuid": policyUUID, "policyVersion": expected, "previousPolicyUuid": prev, "reason": a.Reason,
	}, author, ap))
	return &WriteResult{Applied: true, Assignment: &a}, nil
}

func (s *PolicyAdminService) applyUnassign(ctx context.Context, by Actor, author string, tenant *iface.Tenant, prev, reason string, ap *approval) (*WriteResult, error) {
	now := s.now().UTC()
	masked := maskReason(reason)
	err := s.repo.WithTxn(ctx, func(ctx context.Context) error {
		if err := s.checkPrevious(ctx, tenant.UUID, prev); err != nil {
			return err
		}
		if err := s.decide(ctx, ap, now); err != nil {
			return err
		}
		if err := s.repo.DeleteAssignment(ctx, tenant.UUID); err != nil {
			return err
		}
		return s.repo.InsertAssignmentHistory(ctx, s.history(tenant, models.AssignmentActionUnassign, "", prev, author, masked, now, ap))
	})
	if err != nil {
		return nil, err
	}
	s.afterWrite(ctx)
	s.emit(ctx, by, "compliance.policy.unassigned", "tenant", tenant.UUID, withApproval(map[string]any{
		"previousPolicyUuid": prev, "reason": masked,
	}, author, ap))
	return &WriteResult{Applied: true}, nil
}

func withApproval(meta map[string]any, author string, ap *approval) map[string]any {
	if ap != nil {
		meta["requestedBy"] = author
		meta["changeRequestUuid"] = ap.request.UUID
		meta["approvedBy"] = ap.by.UserID
	}
	return meta
}

// afterWrite refreshes this replica's snapshot at once (the others follow
// within PolicyRefreshInterval).
func (s *PolicyAdminService) afterWrite(ctx context.Context) {
	if s.policies == nil {
		return
	}
	if err := s.policies.Refresh(ctx); err != nil {
		s.logger.Warn("compliance: policy refresh after a write failed", slog.String("error", err.Error()))
	}
}

func (s *PolicyAdminService) emit(ctx context.Context, by Actor, action, resourceType, resourceID string, meta map[string]any) {
	if s.sink == nil {
		return
	}
	ev := iface.AuditEvent{TenantID: by.TenantID, ActorUserID: by.UserID, ActorType: "user", Action: action,
		ResourceType: resourceType, ResourceID: resourceID, Outcome: "success", IPAddress: by.IP, Metadata: meta}
	if by.UserID == models.SystemActor {
		ev.ActorUserID, ev.ActorType = "", "system"
	}
	s.sink.Emit(ctx, ev)
}

// --- reads ---

func (s *PolicyAdminService) ListPolicies(ctx context.Context) ([]PolicyView, error) {
	ps, err := s.repo.ListPolicies(ctx)
	if err != nil {
		return nil, err
	}
	as, err := s.repo.ListAssignments(ctx)
	if err != nil {
		return nil, err
	}
	counts := map[string]int{}
	for _, a := range as {
		counts[a.PolicyUUID]++
	}
	var platform *models.Policy
	for i := range ps {
		if ps[i].IsPlatformDefault {
			platform = &ps[i]
		}
	}
	now := s.now()
	out := make([]PolicyView, 0, len(ps))
	for _, p := range ps {
		v := PolicyView{Policy: p, AssignedTenants: counts[p.UUID], LessRestrictiveFields: []string{}}
		if !p.IsPlatformDefault && platform != nil {
			if f := lessRestrictiveFields(p.LogContent, platform.LogContent, p.Retention, platform.Retention); f != nil {
				v.LessRestrictiveFields = f
			}
		}
		due := p.Accountability.ReviewDueAt
		v.ReviewOverdue = due.IsZero() || due.Before(now)
		out = append(out, v)
	}
	return out, nil
}

func (s *PolicyAdminService) GetPolicy(ctx context.Context, policyUUID string) (*PolicyDetail, error) {
	p, err := s.repo.GetPolicy(ctx, policyUUID)
	if err != nil {
		return nil, err
	}
	all, err := s.ListAssignments(ctx)
	if err != nil {
		return nil, err
	}
	d := &PolicyDetail{Policy: *p, Assignments: []AssignmentView{}}
	for _, a := range all {
		if a.PolicyUUID == policyUUID {
			d.Assignments = append(d.Assignments, a)
		}
	}
	return d, nil
}

func (s *PolicyAdminService) ListVersions(ctx context.Context, policyUUID string) ([]models.PolicyVersion, error) {
	return s.repo.ListVersions(ctx, policyUUID)
}

// ListAssignments adds the tenant and policy names (best effort: a failed
// tenant lookup leaves the name empty).
func (s *PolicyAdminService) ListAssignments(ctx context.Context) ([]AssignmentView, error) {
	as, err := s.repo.ListAssignments(ctx)
	if err != nil {
		return nil, err
	}
	ps, err := s.repo.ListPolicies(ctx)
	if err != nil {
		return nil, err
	}
	names := map[string]string{}
	for _, p := range ps {
		names[p.UUID] = p.Name
	}
	out := make([]AssignmentView, 0, len(as))
	for _, a := range as {
		v := AssignmentView{PolicyAssignment: a, PolicyName: names[a.PolicyUUID]}
		if s.tenants != nil {
			if t, err := s.tenants.GetTenant(ctx, a.TenantID); err == nil && t != nil {
				v.TenantName = t.Name
			}
		}
		out = append(out, v)
	}
	return out, nil
}

func (s *PolicyAdminService) ListChangeRequests(ctx context.Context, status string) ([]models.PolicyChangeRequest, error) {
	return s.repo.ListChangeRequests(ctx, status)
}

func (s *PolicyAdminService) GetChangeRequest(ctx context.Context, requestUUID string) (*models.PolicyChangeRequest, error) {
	return s.repo.GetChangeRequest(ctx, requestUUID)
}
