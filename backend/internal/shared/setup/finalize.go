package setup

// Finalization: the resumable setup saga behind POST /v1/setup/finalize.
//
// The design (docs/superpowers/specs/2026-08-23-tier1-default-tenant-setup-design.md,
// sections "Setup finalization API" and "Resumable saga") splits the work
// into four idempotent stages driven by one persistent coordinator record:
//
//	1. StageConfig  — persist provisioning.internal.mode
//	2. StageTenant  — ensure the reserved internal tenant + its dependent rows
//	3. StageDefault — assign that tenant as the platform default
//	4. StageFinish  — flip the setup phase to complete and snapshot the result
//
// Two rules make the whole thing safe and are worth stating up front,
// because every branch below exists to honour one of them:
//
//   - The stage lease prevents ROUTINE double execution. It is NOT an
//     exactly-once boundary: an external effect can succeed in the instant
//     before its executor loses the lease or crashes. Every stage body must
//     therefore stay correct under overlap and at-least-once replay.
//   - Stage completion is judged ONLY by the CAS-advanced record, never by
//     "we held the lease and the call returned nil". Every iteration of the
//     executor loop re-reads the record and drives from rec.Stage.

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/orkestra/backend/internal/shared/systeminit"
	"github.com/orkestra/backend/pkg/sdk/iface"
)

// Tier-1 provisioning modes, as they appear on the wire, in the
// module-config document, and in the coordinator record.
//
// These are pinned BY VALUE to the tenant module's
// models.ProvisioningModeManual / models.ProvisioningModeSingle:
// shared/setup imports no tenant package (the tenant service reaches this
// package only through the SetupTenantEnsurer seam below), so the coupling
// is a constant plus this comment plus
// TestNormalizeFinalize_ModeConstantsPinnedToTenantModule.
const (
	modeManual = "manual"
	modeSingle = "single"
)

// Values shared with the tenant module, likewise pinned by value:
//   - tenantModuleName is the module-config document name ("tenant").
//   - provisioningInternalModeKey is that module's config key.
//   - defaultAssignSource is models.DefaultUpdateSourceSetup.
//   - tenantKindInternal is models.TenantKindInternal, used only as audit
//     metadata (setup always creates an internal tenant).
const (
	tenantModuleName            = "tenant"
	provisioningInternalModeKey = "provisioning.internal.mode"
	defaultAssignSource         = "setup"
	tenantKindInternal          = "internal"
)

// finalizeStateInProgress is the typed 202 state string. It is part of the
// finalize response contract but is deliberately NOT an errcode constant
// and has NO golden-code entry: {status,title,detail,code} stays exclusive
// to error responses, and a 202 here is a successful accepted outcome.
const finalizeStateInProgress = "setup.finalization_in_progress"

// leaseTTL is the stage-lease duration. It must exceed the timeout of any
// single external call a stage makes (the slowest is StageTenant's
// EnsureSetupTenant, which fans out to tenant/KMS/closure/membership/authz
// writes) so a healthy executor never has its lease expire mid-stage.
const leaseTTL = 30 * time.Second

// maxSagaSteps bounds the executor loop. Four stages plus a handful of
// re-reads after a lost CAS is the realistic worst case; anything beyond
// that means we are racing another executor hard enough that answering
// "in progress, retry" is both true and more useful than spinning.
const maxSagaSteps = 12

// Audit actions emitted by finalization.
const (
	auditActionSetupCompleted   = "setup.completed"
	auditActionFinalizerRecover = "setup.finalizer.recovered"
)

// --- errors ------------------------------------------------------------

// Sentinels returned by Service.Finalize. The HTTP layer maps them in
// mapFinalizeError; nothing else in this package interprets them.
var (
	// ErrFinalizationInProgress means an identical request already holds
	// the current stage lease. This is a SUCCESS at the HTTP layer (202 +
	// Retry-After), not an error envelope — see finalizeStateInProgress.
	ErrFinalizationInProgress = errors.New("setup: finalization in progress")

	// ErrFinalizationAlreadyStarted means a DIFFERENT normalized request is
	// already reserved. 409 setup.finalization_already_started.
	ErrFinalizationAlreadyStarted = errors.New("setup: a different finalization request is already reserved")

	// ErrFinalizationTenantNameRequired means the tenant name is empty once
	// normalized. 422 setup.tenant_name_required.
	//
	// The request schema cannot express this: minLength:"1" constrains the
	// RAW string, so "   " satisfies it and normalizeFinalize then collapses
	// it to "". Nothing downstream re-checks — createTenantWithUUID only
	// TrimSpaces what it is handed — so without this guard setup completes
	// against a nameless Tier-1 organization, and every whitespace-only
	// variant hashes identically and replays as "the same request".
	ErrFinalizationTenantNameRequired = errors.New("setup: tenant name is required")

	// ErrFinalizationTenantSlugRequired is the same guard for the slug.
	// 422 setup.tenant_slug_required. The route's `pattern` makes this
	// unreachable over HTTP today; the check exists so the invariant holds
	// for non-HTTP callers and survives a future change to that pattern.
	ErrFinalizationTenantSlugRequired = errors.New("setup: tenant slug is required")

	// ErrFinalizationAlreadyCompleted means setup is complete and the
	// payload does not match a replayable finalized request (or the caller
	// is not the one authorized to replay it). 409 setup.already_completed.
	ErrFinalizationAlreadyCompleted = errors.New("setup: finalization already completed")

	// ErrFinalizerBoundToAnotherAdmin means the caller is not the usable
	// bound administrator. 403 setup.finalizer_bound_to_another_admin.
	ErrFinalizerBoundToAnotherAdmin = errors.New("setup: finalization is bound to another administrator")

	// ErrRecoveryRequiresSuperAdmin means the caller tried to recover an
	// empty or unusable binding without being an active super_admin.
	// 403 setup.recovery_requires_super_admin.
	ErrRecoveryRequiresSuperAdmin = errors.New("setup: finalizer recovery requires an active super_admin")

	// ErrFinalizerStateUnavailable means the coordinator record or the
	// bound user's state could not be read. Fails closed —
	// 503 setup.finalizer_state_unavailable — and never becomes a
	// recovery opportunity or an inferred phase.
	ErrFinalizerStateUnavailable = errors.New("setup: finalizer state unavailable")
)

// SeamErrorKind classifies a failure that came back through the tenant
// seam. See SeamError.
type SeamErrorKind string

const (
	SeamSlugConflict       SeamErrorKind = "slug_conflict"       // → 409 tenant.slug_already_in_use
	SeamProvisioningLocked SeamErrorKind = "provisioning_locked" // → 409 tenant.provisioning_locked
	SeamIdentityConflict   SeamErrorKind = "identity_conflict"   // → 500, fixed detail
	SeamRemediation        SeamErrorKind = "remediation"         // → 409, fixed detail
)

// SeamError is how a tenant-layer failure crosses into this package with
// its HTTP meaning intact.
//
// shared/setup imports NO tenant package, so it cannot match the tenant
// service's sentinels directly and must not string-match their text.
// Instead cmd/server — which may legally import both — wires an adapter
// around the concrete tenant service that translates
// ErrSlugAlreadyInUse / ErrProvisioningLocked / ErrSetupTenantConflict /
// ErrSetupTenantRemediation into one of the kinds above, wrapping the
// original error so logs keep the real cause.
type SeamError struct{ Kind SeamErrorKind }

func (e *SeamError) Error() string { return "setup seam: " + string(e.Kind) }

// --- seams -------------------------------------------------------------

// SetupTenantEnsurer is the narrow, local structural seam onto the tenant
// service. shared/setup imports no tenant service/repository package;
// cmd/server passes an adapter over *tenantServices.Service (see
// SeamError above for why the adapter, rather than the service itself, is
// what gets wired).
//
// coordinatorAttested is the caller's sworn statement that the setup
// coordinator record for THIS reservation exists and is not completed —
// i.e. that a finalization attempt is genuinely in flight. The tenant
// seam gates its restore-a-soft-deleted-reserved-row branch on it: the
// platform-admin destructive delete route performs the same soft delete
// the seam's own partial-failure rollback does, so identity alone cannot
// tell "our previous attempt rolled this back" from "an operator deleted
// it". Without the attestation a soft-deleted reserved row goes to setup
// remediation instead of being silently restored.
type SetupTenantEnsurer interface {
	EnsureSetupTenant(ctx context.Context, tenantUUID, ownerUUID, name, slug string, coordinatorAttested bool) error
	AssignDefaultTenant(ctx context.Context, tenantUUID, actorUUID, source string) error
}

// --- request / result --------------------------------------------------

// FinalizeInput is the normalized-on-entry finalize payload. The HTTP
// layer dereferences its required pointer bool into
// AllowAdditionalInternalTenants; an omitted value never reaches here
// (Huma rejects it with 422 first).
type FinalizeInput struct {
	TenantName                     string
	TenantSlug                     string
	AllowAdditionalInternalTenants bool
}

// FinalizeResult is the terminal outcome, whether produced by this
// execution or replayed from the coordinator's persisted snapshot.
type FinalizeResult struct {
	TenantUUID string
	TenantName string
	TenantSlug string
	Mode       string
}

// normalizeFinalize canonicalizes the request and derives its hash. The
// hash is what makes a retry "the same request": two payloads differing
// only in whitespace or slug case normalize to the same tuple and must
// therefore replay, not conflict. The mode participates in the hash
// because switching manual↔single is a different request, not a retry.
func normalizeFinalize(name, slug string, allow bool) (n, sl, mode, hash string) {
	n = strings.Join(strings.Fields(name), " ") // trim + collapse inner runs
	sl = strings.ToLower(strings.TrimSpace(slug))
	mode = modeSingle
	if allow {
		mode = modeManual
	}
	sum := sha256.Sum256([]byte(n + "\n" + sl + "\n" + mode))
	return n, sl, mode, hex.EncodeToString(sum[:])
}

// --- entry point -------------------------------------------------------

// Finalize drives the setup-finalization saga for callerUUID and answers
// the request state table from the design:
//
//	| phase           | reservation/hash state    | result                            |
//	|-----------------|---------------------------|-----------------------------------|
//	| tenant_required | nothing reserved          | reserve + execute                 |
//	| tenant_required | matching hash, live lease | ErrFinalizationInProgress (202)   |
//	| tenant_required | matching hash, no lease   | resume the first incomplete stage |
//	| tenant_required | different hash            | ErrFinalizationAlreadyStarted     |
//	| complete        | matching persisted hash   | replay the persisted snapshot     |
//	| complete        | different/missing hash    | ErrFinalizationAlreadyCompleted   |
//
// Authorization comes from evaluateAccessDetailed — the same read-only
// seam GET /v1/setup/finalization-access uses, which reads the caller's
// system role from the database rather than the `srole` claim (D28) — and
// this method adds the atomic claim the probe deliberately does not
// perform.
func (s *Service) Finalize(ctx context.Context, callerUUID string, in FinalizeInput) (*FinalizeResult, error) {
	if callerUUID == "" {
		// The handler rejects an unauthenticated request first; this is
		// the belt-and-braces path for a non-HTTP caller.
		return nil, ErrRecoveryRequiresSuperAdmin
	}
	name, slug, mode, hash := normalizeFinalize(in.TenantName, in.TenantSlug, in.AllowAdditionalInternalTenants)
	// Validate the NORMALIZED values, before the reservation: the schema
	// only ever saw the raw ones. See the two sentinels' doc comments.
	if name == "" {
		return nil, ErrFinalizationTenantNameRequired
	}
	if slug == "" {
		return nil, ErrFinalizationTenantSlugRequired
	}

	// Authorize, recovering the binding at most once. The loop body runs
	// twice at most: once normally, and once more after a recovery claim
	// so the decision is re-derived from the post-claim record rather than
	// assumed.
	var (
		access    FinalizationAccess
		rec       *systeminit.FinalizationRecord
		recovered bool
	)
	for {
		var (
			reason string
			err    error
		)
		access, rec, reason, err = s.evaluateAccessDetailed(ctx, callerUUID)
		if err != nil {
			return nil, unavailable(err)
		}

		// The complete phase short-circuits to the replay table. It is
		// checked here — after access has been evaluated, before an access
		// rejection is returned — because the only post-completion
		// outcome that is not a conflict is the authenticated,
		// matching-hash replay by the bound administrator.
		if isComplete(rec) {
			return replayCompleted(rec, access, hash)
		}

		if access.CanFinalize {
			break
		}
		if access.CanClaimRecovery && !recovered {
			recovered = true
			if err := s.claimFinalizerRecovery(ctx, rec, callerUUID, reason); err != nil {
				return nil, err
			}
			continue
		}
		return nil, accessDenied(access)
	}

	if rec == nil {
		// Unreachable: CanFinalize requires a non-empty bound admin, which
		// requires a record. Guarded anyway — a nil deref in the bootstrap
		// path would be a far worse failure than a 503.
		return nil, unavailable(errors.New("setup: finalization coordinator missing after authorization"))
	}

	// Reservation. ReserveRequest's filter refuses when a request is
	// already reserved or the record is complete, so its ok result is
	// deliberately ignored: winner and loser both reread and fall through
	// to the state table below — which is also the path taken on a resume,
	// where a reservation already exists.
	if rec.RequestHash == "" {
		reserved := uuid.Must(uuid.NewV7()).String()
		if _, err := s.store.ReserveRequest(ctx, callerUUID, reserved, name, slug, mode, hash); err != nil {
			return nil, unavailable(err)
		}
		var err error
		if rec, err = s.store.Get(ctx); err != nil {
			return nil, unavailable(err)
		}
		if rec == nil {
			return nil, errors.New("setup: finalization coordinator vanished after reservation")
		}
	}

	if isComplete(rec) {
		return replayCompleted(rec, access, hash)
	}
	// The binding is checked BEFORE the hash: a reservation that failed
	// because somebody recovered the binding out from under us leaves the
	// hash empty, and reporting "a different request is already reserved"
	// would then be plainly false. A caller with no authority here is told
	// exactly that, whatever the reservation state.
	if rec.AdminUUID != callerUUID {
		return nil, ErrFinalizerBoundToAnotherAdmin
	}
	if rec.RequestHash != hash {
		return nil, ErrFinalizationAlreadyStarted
	}
	return s.runSaga(ctx, hash)
}

// --- recovery ----------------------------------------------------------

// claimFinalizerRecovery performs the atomic binding takeover. rec may be
// nil: a legacy installation can reach "may claim recovery" with no
// coordinator record at all, in which case there is nothing to CAS
// against yet and the record is created first ($setOnInsert, so a
// concurrent creator wins harmlessly and we CAS against whatever exists).
//
// A lost CAS is NOT an error here: the caller re-evaluates access once
// and reports the authorization outcome derived from the winner's record.
// Only a genuine win is audited.
func (s *Service) claimFinalizerRecovery(ctx context.Context, rec *systeminit.FinalizationRecord, callerUUID, reason string) error {
	if rec == nil {
		created, err := s.store.EnsureRecord(ctx, systeminit.SourceLegacyRecovery, nil)
		if err != nil {
			return unavailable(err)
		}
		if created == nil {
			return errors.New("setup: finalization coordinator missing after ensure")
		}
		rec = created
	}

	observed := rec.AdminUUID
	won, err := s.store.ClaimRecovery(ctx, observed, rec.Revision, callerUUID)
	if err != nil {
		return unavailable(err)
	}
	if !won {
		return nil // caller re-evaluates; the winner's binding stands
	}

	// Minimal metadata by design: the old and new UUIDs plus the
	// lifecycle reason. No name, email, token, or profile snapshot.
	meta := map[string]any{
		"reason":       reason,
		"newAdminUUID": callerUUID,
	}
	if observed != "" {
		meta["previousAdminUUID"] = observed
	}
	s.emitAudit(ctx, iface.AuditEvent{
		ActorUserID:  callerUUID,
		Action:       auditActionFinalizerRecover,
		ResourceType: "setup",
		Metadata:     meta,
	})
	return nil
}

// --- executor ----------------------------------------------------------

// runSaga executes stages from the coordinator's current position until
// the record says the saga is complete.
//
// Every iteration re-reads the record: the loop never assumes that a
// stage it just ran is complete, only that the record says so after a
// successful CAS. A lost claim, a lost renewal, and a lost advance all
// fall back to the same place — reread and re-derive — because the effect
// bodies are replay-safe and the record is the only authority on progress.
func (s *Service) runSaga(ctx context.Context, hash string) (*FinalizeResult, error) {
	owner := uuid.NewString()

	for step := 0; step < maxSagaSteps; step++ {
		rec, err := s.store.Get(ctx)
		if err != nil {
			return nil, unavailable(err)
		}
		if rec == nil {
			return nil, errors.New("setup: finalization coordinator vanished mid-saga")
		}
		if isComplete(rec) {
			return resultFromRecord(rec)
		}
		if rec.RequestHash != hash {
			// Only a different reservation can do this, and only a record
			// reset could produce it. Refuse rather than execute stages
			// against somebody else's payload.
			return nil, ErrFinalizationAlreadyStarted
		}
		stage := rec.Stage
		if stage < systeminit.StageConfig || stage > systeminit.StageFinish {
			return nil, fmt.Errorf("setup: finalization coordinator at unexpected stage %d", stage)
		}

		claimed, err := s.store.ClaimStage(ctx, hash, stage, rec.Revision, owner, time.Now().UTC().Add(leaseTTL))
		if err != nil {
			return nil, unavailable(err)
		}
		if !claimed {
			// Either somebody holds a live lease on this stage, or the
			// record moved between our read and the CAS. Distinguish by
			// rereading: real progress means loop again, an unchanged
			// (stage, revision) means a live lease is held elsewhere.
			fresh, err := s.store.Get(ctx)
			if err != nil {
				return nil, unavailable(err)
			}
			if fresh == nil {
				return nil, errors.New("setup: finalization coordinator vanished mid-saga")
			}
			if isComplete(fresh) {
				return resultFromRecord(fresh)
			}
			if fresh.Stage != stage || fresh.Revision != rec.Revision {
				continue
			}
			return nil, ErrFinalizationInProgress
		}

		if stage == systeminit.StageFinish {
			// Stage 4 has no external effect: the Complete CAS *is* the
			// stage. Audit only after that CAS wins, so exactly one
			// executor emits setup.completed.
			result := systeminit.FinalizationResult{
				TenantUUID: rec.TenantUUID,
				TenantName: rec.TenantName,
				TenantSlug: rec.TenantSlug,
				Mode:       rec.Mode,
			}
			done, err := s.store.Complete(ctx, owner, rec.Revision, result)
			if err != nil {
				return nil, unavailable(err)
			}
			if !done {
				continue // stale owner: reread, never assume completion
			}
			s.emitAudit(ctx, iface.AuditEvent{
				TenantID:     rec.TenantUUID,
				TenantKind:   tenantKindInternal,
				ActorUserID:  rec.AdminUUID,
				Action:       auditActionSetupCompleted,
				ResourceType: "tenant",
				ResourceID:   rec.TenantUUID,
				Metadata: map[string]any{
					"tenantUUID": rec.TenantUUID,
					"mode":       rec.Mode,
				},
			})
			return &FinalizeResult{
				TenantUUID: result.TenantUUID,
				TenantName: result.TenantName,
				TenantSlug: result.TenantSlug,
				Mode:       result.Mode,
			}, nil
		}

		if err := s.runStage(ctx, rec, stage); err != nil {
			// Log names the failed stage and nothing else: no request
			// payload, no secrets. The next identical request resumes.
			s.logger.ErrorContext(ctx, "setup: finalization stage failed",
				"stage", stageName(stage), "error", err.Error())
			return nil, err
		}

		// Renew between the stage's external effect and its advance CAS —
		// the substep boundary inside a stage. A slow effect can consume
		// most of the lease, and an expired lease lets a competing
		// executor claim the stage out from under the advance.
		renewed, err := s.store.RenewLease(ctx, owner, time.Now().UTC().Add(leaseTTL))
		if err != nil {
			return nil, unavailable(err)
		}
		if !renewed {
			// Lost the lease. The effect already ran and is replay-safe,
			// so reread and let the record decide what still needs doing.
			s.logger.WarnContext(ctx, "setup: lost finalization stage lease before advance",
				"stage", stageName(stage))
			continue
		}

		advanced, err := s.store.AdvanceStage(ctx, owner, stage, rec.Revision)
		if err != nil {
			return nil, unavailable(err)
		}
		if !advanced {
			continue // stale owner: the record, not the lease, decides
		}
	}

	// Out of steps without reaching a terminal state: another executor is
	// making progress. "Retry shortly" is the honest answer.
	return nil, ErrFinalizationInProgress
}

// runStage performs one stage's external effect. Each body is idempotent
// and safe under overlap; none of them records progress — only the
// coordinator CAS in runSaga does.
func (s *Service) runStage(ctx context.Context, rec *systeminit.FinalizationRecord, stage int) error {
	switch stage {
	case systeminit.StageConfig:
		if s.configService == nil {
			return errors.New("setup: module config service is not wired")
		}
		// UpdateConfig runs the tenant module's provisioning-policy
		// validator, so a mode this saga persists is valid by
		// construction. Re-persisting the same value is a no-op write.
		if err := s.configService.UpdateConfig(ctx, tenantModuleName,
			map[string]string{provisioningInternalModeKey: rec.Mode}, nil); err != nil {
			return fmt.Errorf("setup: persist provisioning mode: %w", err)
		}
	case systeminit.StageTenant:
		if s.tenants == nil {
			return errors.New("setup: tenant seam is not wired")
		}
		// The attestation is true by construction on this path: runSaga
		// only reaches a stage of a record whose hash matches this
		// reservation and whose CompletedAt is nil. It is computed from
		// the record rather than hardcoded so the statement stays a fact
		// about the coordinator, not about this call site.
		attested := rec.RequestHash != "" && rec.CompletedAt == nil
		if err := s.tenants.EnsureSetupTenant(ctx, rec.TenantUUID, rec.AdminUUID, rec.TenantName, rec.TenantSlug, attested); err != nil {
			return fmt.Errorf("setup: ensure setup tenant: %w", err)
		}
	case systeminit.StageDefault:
		if s.tenants == nil {
			return errors.New("setup: tenant seam is not wired")
		}
		// Idempotent: a pointer already naming the reserved UUID is a
		// no-op, a pointer naming a different tenant is an error.
		if err := s.tenants.AssignDefaultTenant(ctx, rec.TenantUUID, rec.AdminUUID, defaultAssignSource); err != nil {
			return fmt.Errorf("setup: assign default tenant: %w", err)
		}
	default:
		return fmt.Errorf("setup: no effect defined for stage %d", stage)
	}
	return nil
}

// --- helpers -----------------------------------------------------------

func isComplete(rec *systeminit.FinalizationRecord) bool {
	return rec != nil && rec.CompletedAt != nil
}

// replayCompleted answers the two "complete" rows of the state table.
// Only an authorized, identical replay receives the persisted snapshot;
// everything else — a different payload, a migration record that has no
// client request hash at all, or a caller who is not the bound
// administrator — is told setup is already complete, without confirming
// whether the payload matched.
func replayCompleted(rec *systeminit.FinalizationRecord, access FinalizationAccess, hash string) (*FinalizeResult, error) {
	if access.CanFinalize && rec.RequestHash != "" && rec.RequestHash == hash {
		return resultFromRecord(rec)
	}
	return nil, ErrFinalizationAlreadyCompleted
}

func resultFromRecord(rec *systeminit.FinalizationRecord) (*FinalizeResult, error) {
	if rec.Result == nil {
		return nil, errors.New("setup: completed finalization carries no result snapshot")
	}
	return &FinalizeResult{
		TenantUUID: rec.Result.TenantUUID,
		TenantName: rec.Result.TenantName,
		TenantSlug: rec.Result.TenantSlug,
		Mode:       rec.Result.Mode,
	}, nil
}

// accessDenied translates a non-permissive access decision into the
// matching 403 sentinel. An empty reason cannot occur here (it would mean
// the decision was permissive) but degrades to the stricter of the two.
//
// One narrow case lands here with a permissive-looking decision: a caller
// whose single recovery claim was lost, whose re-evaluation says they may
// claim recovery again (the winner became unusable in the meantime). The
// spec allows exactly one re-evaluation, so that caller is refused with
// the recovery 403 and retries the POST for a fresh attempt rather than
// this handler looping against a moving record.
func accessDenied(access FinalizationAccess) error {
	if access.Reason == reasonBoundToAnotherAdmin {
		return ErrFinalizerBoundToAnotherAdmin
	}
	return ErrRecoveryRequiresSuperAdmin
}

// unavailable wraps a coordinator/lifecycle read or CAS failure so the
// HTTP layer answers a retryable 503 while the log keeps the real cause.
func unavailable(err error) error {
	return fmt.Errorf("%w: %w", ErrFinalizerStateUnavailable, err)
}

func stageName(stage int) string {
	switch stage {
	case systeminit.StageConfig:
		return "config"
	case systeminit.StageTenant:
		return "tenant"
	case systeminit.StageDefault:
		return "default"
	case systeminit.StageFinish:
		return "finish"
	default:
		return "unknown"
	}
}

// emitAudit is a nil-tolerant fire-and-forget emit: audit is optional
// wiring (the compliance module may be absent) and must never break the
// saga.
func (s *Service) emitAudit(ctx context.Context, event iface.AuditEvent) {
	if s.audit == nil {
		return
	}
	s.audit.Emit(ctx, event)
}
