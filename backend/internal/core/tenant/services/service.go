package services

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/orkestra/backend/internal/core/tenant/models"
	"github.com/orkestra/backend/internal/core/tenant/repository"
	"github.com/orkestra/backend/internal/shared/errcode"
	"github.com/orkestra/backend/pkg/sdk/iface"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
)

// Service owns tenant lifecycle and implements iface.TenantProvider.
type Service struct {
	repo            *repository.Repository
	auditSink       iface.AuditSink
	kms             iface.KMSProvider
	bindOwner       OwnerRoleBinder
	unbindMember    MemberUnbinder
	postDeleteHooks []TenantPostDeleteHook
	// userDisplay is the lazy lookup the unified-clients refactor (Phase 1)
	// uses to seed a personal tenant's Name from the owner User's FullName
	// (EnsureTenantForUser) and to render the FatturaPA CedentePrestatore
	// party name for sole-proprietor tenants (ResolveBillingParty). Wired by
	// the tenant module's Init from the registered ClientUserProvider.
	userDisplay UserDisplayResolver
	// provisioningMode reads the admin-managed per-tier tenant-creation policy
	// (open | manual | single) from the tenant module config at request time.
	// Wired by the tenant module's Init from the ModuleConfigService; nil
	// (tests, config service missing) resolves per-tier via ProvisioningMode's
	// fail-closed (internal) / fail-open (external) defaults — see that method.
	provisioningMode ProvisioningModeResolver
}

// ProvisioningModeResolver returns the configured provisioning mode for a tenant
// tier. Implemented as a closure over ModuleConfigService so admin edits at
// /admin/modules/tenant take effect on the next call (30s Redis cache). An
// empty, unknown, or (for internal) legacy `open` return is normalised by
// Service.ProvisioningMode — see that method for the per-tier rules.
type ProvisioningModeResolver func(ctx context.Context, kind models.TenantKind) string

// ErrProvisioningLocked is returned by CreateTenant (and EnsureTenantForUser)
// when the active provisioning policy forbids creating another tenant of that
// tier — `single` mode with one already present, or `manual`/`single` blocking
// lazy provisioning of an external personal tenant. Handlers map it to 409.
var ErrProvisioningLocked = errors.New("tenant: provisioning locked by policy")

// ErrSlugAlreadyInUse is returned when a create or update would reuse an
// existing tenant slug. Handlers map it to a stable 409 response; the wrapped
// slug remains diagnostic data for logs only.
var ErrSlugAlreadyInUse = errors.New("tenant: slug already in use")

// ErrDefaultReassignmentRequired is returned by SuspendTenant, ArchiveTenant,
// DeleteTenant, and PurgeTenant when the target is the platform default
// Tier-1 tenant (repository.RunDefaultGuarded aborted with
// repository.ErrDefaultGuard). Maps to 409 tenant.default_reassignment_required.
// The caller must transfer the default (TransferDefaultTenant) to another
// operational internal tenant before retrying the lifecycle mutation.
var ErrDefaultReassignmentRequired = errors.New("tenant: default must be reassigned first")

// ErrDefaultAlreadyAssigned is returned by AssignDefaultTenant when the
// platform default pointer already names a DIFFERENT tenant than the one
// requested (repository.AssignDefault detects this atomically inside its
// own transaction — see repository.ErrDefaultAlreadyAssigned). AssignDefaultTenant
// is the idempotent setup/migration entry point — pointing the default at a
// different tenant once one is already assigned is an explicit admin action
// (TransferDefaultTenant), never an implicit side effect of a second Assign
// call.
var ErrDefaultAlreadyAssigned = errors.New("tenant: default already assigned to a different tenant")

func tenantWriteError(err error) error {
	if mongo.IsDuplicateKeyError(err) && strings.Contains(err.Error(), "index: slug_1") {
		return fmt.Errorf("%w: %v", ErrSlugAlreadyInUse, err)
	}
	return err
}

// OwnerRoleBinder is invoked from CreateTenant after the owner membership
// is inserted, to grant the org_owner authz binding so the new tenant's
// owner has actual permissions inside their tenant. Wired by the authz
// module's Init via SetOwnerRoleBinder — the dependency points authz →
// tenant, so tenant must not import the authz package directly.
//
// Failure semantics: a non-nil error from this hook causes CreateTenant
// to soft-delete the tenant and propagate the error. Without the binding
// the owner cannot do anything meaningful inside their own tenant, so
// proceeding silently would create an unrecoverable broken state.
type OwnerRoleBinder func(ctx context.Context, ownerUUID, tenantUUID, roleName string) error

// MemberUnbinder removes every authz binding for a (user, tenant) pair. Wired
// by the authz module via SetMemberUnbinder — the same authz → tenant
// inversion as OwnerRoleBinder, so tenant must not import authz directly.
// Called when a member is removed or has their role changed so the
// membership's authz binding never outlives the membership/role that
// justified it. Nil (authz disabled / tests) means the membership row is
// mutated without touching bindings — the legacy behavior.
type MemberUnbinder func(ctx context.Context, userUUID, tenantUUID string) error

func New(repo *repository.Repository) *Service {
	return &Service{repo: repo}
}

// SetAuditSink wires the compliance audit sink post-construction. Optional —
// the emit* helpers tolerate a nil sink when the compliance module is
// disabled or initializing later in the topological order.
func (s *Service) SetAuditSink(sink iface.AuditSink) { s.auditSink = sink }

// SetKMSProvider wires the per-tenant envelope-encryption provider
// post-construction. When set, CreateTenant mints a KMS key for every
// new tenant and PurgeTenant crypto-shreds it — the GDPR
// right-to-erasure primitive mandated by ADR-0001 Phase 4.3. When the
// provider is nil (compliance module disabled, master key missing)
// tenants are created without a KMS key; purge remains a status flip.
func (s *Service) SetKMSProvider(kms iface.KMSProvider) { s.kms = kms }

// SetOwnerRoleBinder wires the post-membership hook that grants the
// owner's authz binding. See OwnerRoleBinder for failure semantics.
// Wired by the authz module after both tenant.Init and authz.Init
// complete; nil binder (authz disabled, or tests) means CreateTenant
// inserts the membership without an authz binding — the owner relies
// on their platform system role to act, which is the legacy behavior.
func (s *Service) SetOwnerRoleBinder(fn OwnerRoleBinder) { s.bindOwner = fn }

// SetMemberUnbinder wires the hook that drops a member's tenant-scoped authz
// binding(s) on member removal and on role change. See MemberUnbinder for the
// nil-hook (legacy) semantics. Wired by the authz module alongside
// SetOwnerRoleBinder.
func (s *Service) SetMemberUnbinder(fn MemberUnbinder) { s.unbindMember = fn }

// SetProvisioningModeResolver wires the per-tier tenant-creation policy reader.
// Wired by the tenant module's Init from the ModuleConfigService. Nil resolves
// per-tier via ProvisioningMode's fail-closed (internal) / fail-open (external)
// defaults.
func (s *Service) SetProvisioningModeResolver(fn ProvisioningModeResolver) { s.provisioningMode = fn }

// ProvisioningMode returns the active provisioning policy for a tier.
// Tier-1 resolution is FAIL-CLOSED: missing, unknown, or legacy `open`
// values resolve to manual — `open` is no longer a valid internal mode.
// Tier-2 keeps its historical behaviour (unknown/missing resolve to open,
// which still gates self-serve/lazy provisioning). An invalid kind is
// normalised to internal.
func (s *Service) ProvisioningMode(ctx context.Context, kind models.TenantKind) string {
	if !kind.Valid() {
		kind = models.TenantKindInternal
	}
	var mode string
	if s.provisioningMode != nil {
		mode = strings.TrimSpace(s.provisioningMode(ctx, kind))
	}
	switch mode {
	case models.ProvisioningModeManual, models.ProvisioningModeSingle:
		return mode
	}
	if kind == models.TenantKindInternal {
		return models.ProvisioningModeManual
	}
	return models.ProvisioningModeOpen
}

// CountProvisioningSlotsByKind returns the number of tenants of a tier that
// occupy a provisioning slot. Exposed so handlers can render the
// provisioning-policy surface; also used internally by the `single`-mode
// invariant.
func (s *Service) CountProvisioningSlotsByKind(ctx context.Context, kind models.TenantKind) (int64, error) {
	return s.repo.CountProvisioningSlotsByKind(ctx, kind)
}

// TenantPostDeleteContext carries the data a cascade hook needs to clean
// up tenant-adjacent state owned by other modules — authz bindings, the
// orphaned owner's user account on the per-tier user collections, anything
// else that points at the tenant. Computed inside DeleteTenant /
// PurgeTenant before the hooks fire so each subscriber gets a consistent
// snapshot regardless of execution order.
type TenantPostDeleteContext struct {
	TenantUUID string
	// Kind is "internal" or "external". User-cleanup hooks key on this
	// because operator users may legitimately outlive a single tenant
	// (one human, many internal workspaces) while external Tier-2
	// signups exist solely to hold the client tenant.
	Kind string
	// OwnerUserUUID is the tenant's recorded owner. May be empty for
	// legacy rows that never stamped an owner — hooks must tolerate that.
	OwnerUserUUID string
	// OwnerHasOtherTenants is true when the owner still belongs to at
	// least one tenant after this delete. User-eviction hooks must check
	// it before reclaiming the email — a user with active memberships
	// elsewhere cannot have their account aliased away.
	OwnerHasOtherTenants bool
	// Hard is true for PurgeTenant (irreversible erasure), false for
	// DeleteTenant (soft-delete with deletedAt). Hooks may use this to
	// hard-delete vs soft-delete on their side, though most cascade
	// targets (memberships, ancestors, bindings) are hard-deleted in
	// either case because they have no soft-delete pattern.
	Hard bool
}

// TenantPostDeleteHook is invoked after the tenant module has finished
// its own cascade (memberships, ancestors, lifecycle status). Hooks fire
// in registration order; a non-nil error is logged via the audit sink
// but does not abort subsequent hooks — best-effort cleanup so a single
// flaky downstream module doesn't leave the rest of the system in a
// half-cascaded state.
type TenantPostDeleteHook func(ctx context.Context, c TenantPostDeleteContext) error

// RegisterPostDeleteHook appends a cascade hook. Called by other modules
// during their Init (authz wires binding-cleanup, tenant itself wires the
// orphaned-owner-user evictor via the user iface).
func (s *Service) RegisterPostDeleteHook(fn TenantPostDeleteHook) {
	if fn == nil {
		return
	}
	s.postDeleteHooks = append(s.postDeleteHooks, fn)
}

// emitAudit forwards to the compliance sink when wired; no-op otherwise.
func (s *Service) emitAudit(ctx context.Context, event iface.AuditEvent) {
	if s.auditSink == nil {
		return
	}
	s.auditSink.Emit(ctx, event)
}

// actorFromContext pulls the authenticated principal out of the request
// context so lifecycle emits can attribute the change. Safe to call when
// no principal is resolved — returns empty fields.
func actorFromContext(ctx context.Context) (userUUID, email, kind string) {
	if v, ok := ctx.Value("userUUID").(string); ok {
		userUUID = v
	}
	if v, ok := ctx.Value("userEmail").(string); ok {
		email = v
	}
	actorType := "system"
	if userUUID != "" {
		actorType = "user"
	}
	return userUUID, email, actorType
}

// resolveDefaultActor determines the audit actor for a default-tenant
// assignment or transfer. An explicit actorUUID — passed by an HTTP handler
// that already resolved the caller from ctxauth, or left empty by an
// unattended migration/reconciliation caller — wins over the request
// context; when it is empty the context is consulted (actorFromContext) so
// a caller that only threads identity through ctx still attributes
// correctly. Both empty means a genuinely unattended caller: ActorType
// "system", ActorUserID "" — the literal string "system" is never stored in
// the ActorUserID field itself, only in ActorType.
func (s *Service) resolveDefaultActor(ctx context.Context, actorUUID string) (userUUID, email, actorType string) {
	if actorUUID != "" {
		return actorUUID, "", "user"
	}
	return actorFromContext(ctx)
}

// --- Provider interface ---

func (s *Service) GetTenant(ctx context.Context, tenantUUID string) (*iface.Tenant, error) {
	t, err := s.repo.GetTenantByUUID(ctx, tenantUUID)
	if err != nil {
		return nil, err
	}
	return tenantToIface(t), nil
}

// tenantToIface flattens a tenant document into the cross-module DTO shape.
// Centralized so every provider entry point returns the same projection.
func tenantToIface(t *models.Tenant) *iface.Tenant {
	kind := string(t.Kind)
	if kind == "" {
		kind = iface.TenantKindInternal
	}
	status := string(t.Status)
	if status == "" {
		status = iface.TenantStatusActive
	}
	var parent string
	if t.ParentTenantUUID != nil {
		parent = *t.ParentTenantUUID
	}
	return &iface.Tenant{
		UUID:             t.UUID,
		Kind:             kind,
		ParentTenantUUID: parent,
		Status:           status,
		Name:             t.Name,
		Slug:             t.Slug,
		Plan:             t.Plan,
		LegalName:        t.LegalName,
		Email:            t.PrimaryContact.Email,
		VATNumber:        t.VATNumber,
		FiscalCode:       t.FiscalCode,
		Country:          t.BillingAddress.Country,
		StripeCustomerID: t.StripeCustomerID,
		IsCompany:        t.IsCompany,
		SignupChannel:    t.SignupChannel,
	}
}

func (s *Service) ListUserMemberships(ctx context.Context, userUUID string) ([]iface.TenantMembership, error) {
	mbrs, err := s.repo.ListMembershipsByUser(ctx, userUUID)
	if err != nil {
		return nil, err
	}
	out := make([]iface.TenantMembership, 0, len(mbrs))
	for _, m := range mbrs {
		t, err := s.repo.GetTenantByUUID(ctx, m.TenantUUID)
		if err != nil {
			continue // tenant may be archived, skip
		}
		kind := string(m.TenantKind)
		if kind == "" {
			kind = string(t.Kind)
		}
		if kind == "" {
			kind = iface.TenantKindInternal
		}
		out = append(out, iface.TenantMembership{
			TenantUUID: t.UUID,
			TenantName: t.Name,
			TenantSlug: t.Slug,
			TenantKind: kind,
			Roles:      m.Roles,
			IsOwner:    m.IsOwner,
		})
	}
	return out, nil
}

func (s *Service) IsMember(ctx context.Context, userUUID, tenantUUID string) (bool, error) {
	_, err := s.repo.GetMembership(ctx, userUUID, tenantUUID)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

// --- Platform default tenant ---
//
// Only kind=internal is supported: the platform default is always a Tier-1
// (operator) tenant. See models.TenantDefault and repository/defaults.go for
// the pointer row and its guarded transactions.

// DefaultTenantUUID returns the platform default Tier-1 tenant's UUID, or
// "" when no default has been assigned yet — an unassigned platform is a
// normal (pre-setup) state, not an error.
func (s *Service) DefaultTenantUUID(ctx context.Context) (string, error) {
	d, err := s.repo.GetDefault(ctx, models.TenantKindInternal)
	if errors.Is(err, repository.ErrNotFound) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	return d.TenantUUID, nil
}

// GetDefaultTenant implements iface.DefaultTenantProvider. It returns
// (nil, nil) — never an error — both when no default is assigned and when
// the pointer names a tenant that is no longer operational (suspended,
// archived, purged, or soft-deleted): the provider never hands out a
// non-operational target. Membership validation, RBAC, audience checks, and
// X-Tenant-ID override all still apply downstream; this method grants
// nothing by itself.
func (s *Service) GetDefaultTenant(ctx context.Context) (*iface.Tenant, error) {
	d, err := s.repo.GetDefault(ctx, models.TenantKindInternal)
	if errors.Is(err, repository.ErrNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	t, err := s.repo.GetTenantByUUID(ctx, d.TenantUUID)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			// Soft-deleted (GetTenantByUUID filters deletedAt) — not
			// operational, so the provider hands out nothing rather than
			// erroring.
			return nil, nil
		}
		return nil, err
	}
	if t.Status != models.TenantStatusActive {
		return nil, nil
	}
	return tenantToIface(t), nil
}

// AssignDefaultTenant is the setup/migration entry point for the platform
// default. Idempotent: when the pointer already names tenantUUID this is a
// no-op success (no repository write, no duplicate row, no re-emitted
// audit event). When the pointer already names a DIFFERENT tenant it
// returns ErrDefaultAlreadyAssigned rather than silently replacing it —
// replacing an established default is an explicit admin action
// (TransferDefaultTenant), never an implicit side effect of Assign. source
// records provenance and must be one of the models.DefaultUpdateSource*
// constants (typically Setup or Migration).
//
// The conflict decision (does a pointer already exist, and if so does it
// already name tenantUUID) is made by repository.AssignDefault INSIDE its
// own transaction — not by a read-then-act sequence here — because a
// service-level pre-check followed by a separate write cannot close the
// window between two concurrent callers both observing "unassigned" and
// both proceeding to write. See repository.ErrDefaultAlreadyAssigned's doc
// for how the transactional write-conflict retry makes that race safe.
func (s *Service) AssignDefaultTenant(ctx context.Context, tenantUUID, actorUUID, source string) error {
	created, err := s.repo.AssignDefault(ctx, models.TenantKindInternal, tenantUUID, actorUUID, source)
	if err != nil {
		if errors.Is(err, repository.ErrDefaultAlreadyAssigned) {
			return ErrDefaultAlreadyAssigned
		}
		return err
	}
	if !created {
		// Idempotent no-op: the pointer already named tenantUUID. No write
		// happened, so no audit event — re-asserting an unchanged fact is
		// not a new assignment.
		return nil
	}

	userUUID, email, actorType := s.resolveDefaultActor(ctx, actorUUID)
	s.emitAudit(ctx, iface.AuditEvent{
		TenantID:     tenantUUID,
		TenantKind:   string(models.TenantKindInternal),
		ActorUserID:  userUUID,
		ActorEmail:   email,
		ActorType:    actorType,
		Action:       "tenant.default.assigned",
		ResourceType: "tenant",
		ResourceID:   tenantUUID,
		Metadata: map[string]any{
			"tenantUUID": tenantUUID,
			"source":     source,
		},
	})
	return nil
}

// TransferDefaultTenant is the admin transfer path: system.tenants.admin
// plus step-up MFA are enforced at the HTTP layer. Requires an existing
// default pointer (repository.SetDefault's requireExisting=true) and moves
// it to tenantUUID, which repository.SetDefault validates — inside the same
// transaction — as an operational internal tenant. A target rejected as not
// operational (repository.ErrDefaultTargetNotOperational) is audited as a
// denied transfer and the pointer is left untouched; any other error
// (including repository.ErrNotFound when no pointer exists yet) propagates
// without an audit emission. On success, audits tenant.default.transferred
// with the previous and new tenant UUIDs.
func (s *Service) TransferDefaultTenant(ctx context.Context, tenantUUID, actorUUID string) error {
	prevUUID, err := s.repo.SetDefault(ctx, models.TenantKindInternal, tenantUUID, actorUUID, models.DefaultUpdateSourceTransfer, true)
	userUUID, email, actorType := s.resolveDefaultActor(ctx, actorUUID)
	if err != nil {
		if errors.Is(err, repository.ErrDefaultTargetNotOperational) {
			s.emitAudit(ctx, iface.AuditEvent{
				TenantID:     tenantUUID,
				TenantKind:   string(models.TenantKindInternal),
				ActorUserID:  userUUID,
				ActorEmail:   email,
				ActorType:    actorType,
				Action:       "tenant.default.transferred",
				ResourceType: "tenant",
				ResourceID:   tenantUUID,
				Outcome:      "denied",
				Metadata: map[string]any{
					"newTenantUUID": tenantUUID,
					"reason":        "target_not_operational",
				},
			})
		}
		return err
	}
	s.emitAudit(ctx, iface.AuditEvent{
		TenantID:     tenantUUID,
		TenantKind:   string(models.TenantKindInternal),
		ActorUserID:  userUUID,
		ActorEmail:   email,
		ActorType:    actorType,
		Action:       "tenant.default.transferred",
		ResourceType: "tenant",
		ResourceID:   tenantUUID,
		Metadata: map[string]any{
			"previousTenantUUID": prevUUID,
			"newTenantUUID":      tenantUUID,
		},
	})
	return nil
}

// emitDefaultGuardDenied emits a denied audit event for a lifecycle
// mutation blocked by the platform-default guard (repository.ErrDefaultGuard
// via repository.RunDefaultGuarded). action must be exactly the action
// string the same lifecycle method emits on success — the existing
// denied-event convention reuses the action and flips Outcome, rather than
// minting a separate "refused" action name.
func (s *Service) emitDefaultGuardDenied(ctx context.Context, action, tenantUUID string) {
	userUUID, email, actorType := actorFromContext(ctx)
	s.emitAudit(ctx, iface.AuditEvent{
		TenantID:     tenantUUID,
		ActorUserID:  userUUID,
		ActorEmail:   email,
		ActorType:    actorType,
		Action:       action,
		ResourceType: "tenant",
		ResourceID:   tenantUUID,
		Outcome:      "denied",
		Metadata:     map[string]any{"code": errcode.TenantDefaultReassignmentRequired},
	})
}

// --- Tenant lifecycle ---

// CreateTenant provisions a brand-new tenant with a freshly minted UUID.
// It is a thin wrapper over the shared absent-to-present primitive so every
// actual creation — normal or setup-reserved — passes the same service-level
// provisioning guard. See tenant/CLAUDE.md#creation-vs-reconciliation.
func (s *Service) CreateTenant(ctx context.Context, ownerUUID string, input models.CreateTenantInput) (*models.Tenant, error) {
	return s.createTenantWithUUID(ctx, ownerUUID, uuid.Must(uuid.NewV7()).String(), input)
}

func (s *Service) createTenantWithUUID(ctx context.Context, ownerUUID, tenantUUID string, input models.CreateTenantInput) (*models.Tenant, error) {
	slug := slugify(input.Slug)
	if slug == "" {
		slug = slugify(input.Name)
	}
	if existing, _ := s.repo.GetTenantBySlug(ctx, slug); existing != nil {
		return nil, fmt.Errorf("%w: %s", ErrSlugAlreadyInUse, slug)
	}

	plan := input.Plan
	if plan == "" {
		plan = models.PlanFree
	}

	kind := input.Kind
	if !kind.Valid() {
		kind = models.TenantKindInternal
	}

	// Provisioning policy backstop: in `single` mode a tier may hold at most
	// one tenant occupying a provisioning slot (see CLAUDE.md's Lifecycle
	// terminology). Enforced here (not just at the handler) so every
	// creation path — POST /v1/tenants, divisions, lazy provisioning — is
	// covered. The first tenant on a fresh install has count 0 and passes,
	// so bootstrap is never blocked.
	if s.ProvisioningMode(ctx, kind) == models.ProvisioningModeSingle {
		n, err := s.repo.CountProvisioningSlotsByKind(ctx, kind)
		if err != nil {
			return nil, fmt.Errorf("tenant: count tenants for single-mode check: %w", err)
		}
		if n > 0 {
			return nil, ErrProvisioningLocked
		}
	}

	var parent *string
	if input.ParentTenantUUID != nil && *input.ParentTenantUUID != "" {
		if kind != models.TenantKindExternal {
			return nil, fmt.Errorf("parentTenantUUID is only allowed for external tenants")
		}
		p := *input.ParentTenantUUID
		parentTenant, err := s.repo.GetTenantByUUID(ctx, p)
		if err != nil {
			return nil, fmt.Errorf("parent tenant not found: %s", p)
		}
		// The parent must itself be external. Without this an external tenant
		// could be grafted onto an internal operator tenant's closure table, and
		// ResolveBillingParty would then resolve the operator's legal identity
		// (VAT/fiscal/PEC) as the invoicing party. CreateDivision enforces the
		// same rule; POST /v1/tenants must not be a weaker second path.
		if !parentTenant.IsExternal() {
			return nil, fmt.Errorf("parent tenant must be external")
		}
		parent = &p
	}

	sigChan := models.SignupChannelSeeded
	if kind == models.TenantKindExternal {
		sigChan = models.SignupChannelSalesAssisted
	}

	t := &models.Tenant{
		UUID:             tenantUUID,
		Kind:             kind,
		Status:           models.TenantStatusActive,
		ParentTenantUUID: parent,
		Name:             strings.TrimSpace(input.Name),
		Slug:             slug,
		OwnerUserUUID:    ownerUUID,
		SignupChannel:    sigChan,
		Region:           "eu-west",
		Plan:             plan,
	}

	if err := s.repo.CreateTenant(ctx, t); err != nil {
		return nil, tenantWriteError(err)
	}

	// Per-tenant KMS key — minted before membership bookkeeping so a
	// failure here aborts the tenant cleanly (soft-delete the row,
	// return the error) rather than leaving a half-provisioned
	// tenant. When KMS is not wired (compliance disabled) the step
	// is silently skipped and KMSKeyID stays empty.
	if s.kms != nil {
		keyID, err := s.kms.CreateKey(ctx, t.UUID)
		if err != nil {
			_ = s.repo.SoftDeleteTenant(ctx, t.UUID)
			return nil, fmt.Errorf("tenant: mint KMS key: %w", err)
		}
		t.KMSKeyID = &keyID
		if err := s.repo.UpdateTenant(ctx, t.UUID, bson.M{"kmsKeyID": keyID}); err != nil {
			return nil, fmt.Errorf("tenant: stamp KMS key: %w", err)
		}
	}

	// Closure-table bookkeeping: self-row at depth 0 for every tenant,
	// plus the transitive chain when a parent is set.
	if err := s.repo.InsertSelfAncestor(ctx, t.UUID); err != nil {
		return nil, fmt.Errorf("tenant: insert self ancestor: %w", err)
	}
	if parent != nil {
		if err := s.repo.AttachToParent(ctx, t.UUID, *parent); err != nil {
			return nil, fmt.Errorf("tenant: attach to parent: %w", err)
		}
	}

	// Owner is auto-enrolled as a member with the org_owner role
	// (Section B item #3 commit B of the auth roadmap, 2026-04-24).
	// org_owner is a tenant-scoped role; the platform-level
	// "administrator" string is no longer denormalized here because
	// granting platform-admin via a tenant membership conflates the two
	// tiers. The actual authz binding is created by the OwnerRoleBinder
	// hook below — without it the role name on Membership.Roles is
	// purely informational.
	membership := &models.TenantMembership{
		UUID:       uuid.Must(uuid.NewV7()).String(),
		UserUUID:   ownerUUID,
		TenantUUID: t.UUID,
		TenantKind: kind,
		Roles:      []string{"org_owner"},
		IsOwner:    true,
	}
	if err := s.repo.CreateMembership(ctx, membership); err != nil {
		return nil, err
	}

	// Grant the org_owner authz binding so the owner can actually act
	// in the tenant they just created. Without this hook the owner has
	// only their platform system role (which for an external client
	// signing up is "guest"), so they couldn't even read their own
	// tenant. Failure soft-deletes the tenant — same pattern as the
	// KMS step above — to avoid leaving a half-provisioned tenant.
	if s.bindOwner != nil {
		if err := s.bindOwner(ctx, ownerUUID, t.UUID, "org_owner"); err != nil {
			_ = s.repo.SoftDeleteTenant(ctx, t.UUID)
			return nil, fmt.Errorf("tenant: bind owner role: %w", err)
		}
	}
	return t, nil
}

// ErrSetupTenantConflict is returned by EnsureSetupTenant when the reserved
// tenant UUID already names a row whose immutable setup identity — kind,
// owner, normalized name, or slug — does not match what the caller supplied.
// This is a setup-remediation signal, never silent adoption: a reserved UUID
// must resolve to the same tenant on every replay of the saga stage.
var ErrSetupTenantConflict = errors.New("tenant: reserved setup tenant identity mismatch")

// ErrSetupTenantRemediation is returned by EnsureSetupTenant when the
// reserved tenant row is archived-and-purged (Status == purged, or PurgedAt
// set). Purge is terminal — PurgeTenant has already crypto-shredded the
// row's KMS key — so EnsureSetupTenant never resurrects it; an operator must
// remediate (e.g. assign a fresh reservation) rather than have the saga
// silently retry against a dead row.
var ErrSetupTenantRemediation = errors.New("tenant: reserved setup tenant requires remediation")

// EnsureSetupTenant converges the reserved setup-tenant UUID to a fully
// reconciled, operational internal tenant. It is the primitive a resumable
// provisioning saga (a later PR) calls repeatedly — after a lost response, a
// crashed executor, or an expired lease — until it observes a nil error, so
// every step it takes must be safe to replay any number of times, including
// concurrently. Idempotency is ordered deliberately around the `single`
// provisioning gate; see tenant/CLAUDE.md#creation-vs-reconciliation for the
// contract this method and CreateTenant both honour:
//
//  1. The reserved UUID already names a row → this is RECONCILIATION, not
//     creation: reconcileSetupTenant validates the row's immutable setup
//     identity, stamps the enterprise plan, and reconciles its dependent
//     rows. The `single` gate is never consulted for a row that already
//     occupies its own slot, so the reserved tenant can never count against
//     itself on a retry.
//  2. No row exists yet → call the shared absent-to-present primitive with
//     the reserved UUID and an EXPLICIT models.PlanEnterprise — CreateTenant's
//     empty-plan fallback is `free`, and setup must never inherit it.
//  3. The primitive fails with a duplicate-key error (on the tenant row
//     itself, or — because a concurrent reconcile can race ahead of a
//     concurrent creation — on one of the dependent rows the primitive also
//     writes) → reread the reserved UUID and reconcile whatever is there.
//     When the reread finds nothing, an UNRELATED tenant holds the slug:
//     that is a real conflict, not a race, so the original
//     ErrSlugAlreadyInUse propagates rather than being swallowed.
//  4. A prior attempt's partial-failure rollback (createTenantWithUUID's own
//     SoftDeleteTenant calls) soft-deleted the row → restoring it
//     re-occupies a provisioning slot, so reconcileSetupTenant applies the
//     `single` gate against OTHER occupants first. A row that reached
//     Status == purged, OR that an operator explicitly archived (Status ==
//     archived with DeletedAt == nil — the ArchiveTenant signature, distinct
//     from this seam's own soft-delete rollback), is never resurrected this
//     way — see ErrSetupTenantRemediation.
//
// coordinatorAttested is the caller's statement that the setup coordinator
// record for THIS reservation exists and is not completed — that a
// finalization attempt is genuinely in flight. It is the missing half of
// the design's restore rule ("the COORDINATOR and immutable identity prove
// that a prior attempt for this same setup reservation soft-deleted the
// row"): the platform-admin destructive delete route calls the very same
// SoftDeleteTenant this seam's own rollback calls, so the row signature
// alone cannot distinguish "our previous attempt rolled this back" from
// "an operator deleted it". Only the setup saga holds the coordinator, so
// only the setup saga can attest; every other caller passes false and a
// soft-deleted reserved row then enters remediation instead of being
// silently restored. The tenant service deliberately does not resolve the
// coordinator itself — systeminit is not reachable from here, and
// inventing a second source of truth for it would be worse than taking
// the caller's word under a documented contract.
func (s *Service) EnsureSetupTenant(ctx context.Context, tenantUUID, ownerUUID, name, slug string, coordinatorAttested bool) error {
	normName := strings.TrimSpace(name)
	normSlug := slugify(slug)
	if normSlug == "" {
		normSlug = slugify(name)
	}

	existing, err := s.repo.GetTenantByUUIDIncludingDeleted(ctx, tenantUUID)
	switch {
	case err == nil:
		return s.reconcileSetupTenant(ctx, existing, ownerUUID, normName, normSlug, coordinatorAttested)
	case errors.Is(err, repository.ErrNotFound):
		_, cerr := s.createTenantWithUUID(ctx, ownerUUID, tenantUUID, models.CreateTenantInput{
			Name: normName,
			Slug: normSlug,
			Kind: models.TenantKindInternal,
			Plan: models.PlanEnterprise, // explicit: never CreateTenant's free fallback
		})
		if cerr == nil {
			return nil
		}
		if errors.Is(cerr, ErrSlugAlreadyInUse) || mongo.IsDuplicateKeyError(cerr) {
			// A concurrent EnsureSetupTenant call for this SAME reserved
			// UUID may have won the tenant-row insert, or raced us on a
			// downstream dependent row (see reconcileSetupTenant). Either
			// way the reread is the correct recovery: when it comes back
			// empty, the reserved UUID is genuinely free and an unrelated
			// tenant holds the slug, so cerr (ErrSlugAlreadyInUse) is the
			// right error to propagate rather than mask.
			winner, rerr := s.repo.GetTenantByUUIDIncludingDeleted(ctx, tenantUUID)
			if rerr == nil {
				return s.reconcileSetupTenant(ctx, winner, ownerUUID, normName, normSlug, coordinatorAttested)
			}
			return cerr
		}
		return cerr
	default:
		return err
	}
}

// reconcileSetupTenant converges an EXISTING reserved-UUID row (soft-deleted
// or not) to the fully-provisioned state EnsureSetupTenant promises. It
// never trips the `single` provisioning gate against the row it is
// reconciling: a row that already occupies a slot skips the gate entirely,
// and a soft-deleted row being restored is checked against OTHER occupants
// only (its own deletedAt != nil already excludes it from
// CountProvisioningSlotsByKind's count). Every dependent write is treated as
// replay-safe: a duplicate-key error from a racing writer — another
// EnsureSetupTenant call, or the absent-to-present primitive itself — is read
// back and VALIDATED, never trusted blind.
//
// coordinatorAttested gates the restore branch only; see EnsureSetupTenant.
func (s *Service) reconcileSetupTenant(ctx context.Context, existing *models.Tenant, ownerUUID, name, slug string, coordinatorAttested bool) error {
	if existing.Kind != models.TenantKindInternal ||
		existing.OwnerUserUUID != ownerUUID ||
		existing.Slug != slug ||
		existing.Name != name {
		return ErrSetupTenantConflict
	}

	// Archived-and-purged is terminal — PurgeTenant already crypto-shredded
	// the KMS key — so it is never resurrected, remediation only.
	if existing.PurgedAt != nil || existing.Status == models.TenantStatusPurged {
		return ErrSetupTenantRemediation
	}

	// A row genuinely archived by the admin ArchiveTenant action (Status ==
	// archived, DeletedAt == nil) is a DIFFERENT state from the setup-owned
	// rollback signature checked below: ArchiveTenant never sets deletedAt,
	// so this predicate can only be true for a tenant an operator
	// deliberately archived outside the setup flow — never for a row this
	// seam soft-deleted itself. The design spec is explicit that such a row
	// is not resurrected either: "archived and purged reserved rows enter
	// remediation rather than being resurrected."
	if existing.Status == models.TenantStatusArchived && existing.DeletedAt == nil {
		return ErrSetupTenantRemediation
	}

	// A prior attempt's own rollback (createTenantWithUUID's SoftDeleteTenant
	// calls on a KMS/membership/bind failure) left this row soft-deleted.
	// Restoring it re-occupies a provisioning slot, so apply the SAME
	// `single` cardinality check CreateTenant applies on a genuine creation
	// — but against OTHER occupants: this row's own deletedAt != nil already
	// excludes it from the count below.
	if existing.DeletedAt != nil {
		// The row signature alone does not prove WHO soft-deleted it:
		// DeleteTenant (the MFA-gated platform-admin destructive route)
		// writes exactly what this seam's own rollback writes. Without the
		// caller's coordinator attestation — an in-flight reservation for
		// this very setup — restoring would silently undo a deliberate
		// operator action, so an unattested soft-deleted reserved row goes
		// to remediation instead. See EnsureSetupTenant's contract.
		if !coordinatorAttested {
			return ErrSetupTenantRemediation
		}
		if s.ProvisioningMode(ctx, models.TenantKindInternal) == models.ProvisioningModeSingle {
			n, err := s.repo.CountProvisioningSlotsByKind(ctx, models.TenantKindInternal)
			if err != nil {
				return fmt.Errorf("tenant: count tenants for setup-tenant single-mode check: %w", err)
			}
			if n > 0 {
				return ErrProvisioningLocked
			}
		}
		if err := s.repo.RestoreTenant(ctx, existing.UUID); err != nil {
			return fmt.Errorf("tenant: restore setup tenant: %w", err)
		}
		existing.DeletedAt = nil
		existing.ArchivedAt = nil
		existing.Status = models.TenantStatusActive
	}

	// Plan: setup must land on enterprise regardless of what a legacy or
	// partially-provisioned row currently carries.
	if existing.Plan != models.PlanEnterprise {
		if err := s.repo.UpdateTenant(ctx, existing.UUID, bson.M{"plan": models.PlanEnterprise}); err != nil {
			return fmt.Errorf("tenant: stamp setup tenant plan: %w", err)
		}
	}

	// KMS key: CreateKey is concurrent-idempotent (Task 4.1) — every caller
	// converges on the single winning keyID — so it is always safe to call;
	// only the stamp is conditional, to avoid a redundant write once a
	// previous attempt already recorded it.
	if s.kms != nil {
		keyID, err := s.kms.CreateKey(ctx, existing.UUID)
		if err != nil {
			return fmt.Errorf("tenant: mint setup tenant KMS key: %w", err)
		}
		if existing.KMSKeyID == nil || *existing.KMSKeyID == "" {
			if err := s.repo.UpdateTenant(ctx, existing.UUID, bson.M{"kmsKeyID": keyID}); err != nil {
				return fmt.Errorf("tenant: stamp setup tenant KMS key: %w", err)
			}
		}
	}

	// Closure self-row: guarded by the (descendantUUID, ancestorUUID) unique
	// index — a duplicate-key error means a racing writer already inserted
	// it, which is success, not failure.
	if err := s.repo.InsertSelfAncestor(ctx, existing.UUID); err != nil && !mongo.IsDuplicateKeyError(err) {
		return fmt.Errorf("tenant: insert setup tenant self ancestor: %w", err)
	}

	// Owner membership: guarded by the (userUUID, tenantId) unique index. A
	// duplicate-key loser rereads the winner and VALIDATES it rather than
	// assuming it matches what this call intended to write.
	membership := &models.TenantMembership{
		UUID:       uuid.Must(uuid.NewV7()).String(),
		UserUUID:   ownerUUID,
		TenantUUID: existing.UUID,
		TenantKind: models.TenantKindInternal,
		Roles:      []string{"org_owner"},
		IsOwner:    true,
	}
	if err := s.repo.CreateMembership(ctx, membership); err != nil {
		if !mongo.IsDuplicateKeyError(err) {
			return fmt.Errorf("tenant: create setup tenant owner membership: %w", err)
		}
		winner, rerr := s.repo.GetMembership(ctx, ownerUUID, existing.UUID)
		if rerr != nil {
			return fmt.Errorf("tenant: reread setup tenant owner membership: %w", rerr)
		}
		if !winner.IsOwner || winner.TenantKind != models.TenantKindInternal || !slices.Contains(winner.Roles, "org_owner") {
			return ErrSetupTenantConflict
		}
	}

	// authz binding: an ensure since Task 4.2 — concurrent-safe and
	// replay-safe on its own.
	if s.bindOwner != nil {
		if err := s.bindOwner(ctx, ownerUUID, existing.UUID, "org_owner"); err != nil {
			return fmt.Errorf("tenant: bind setup tenant owner role: %w", err)
		}
	}

	return nil
}

// CreateExternalTenant is the dedicated factory for Tier-2 tenants (external
// clients registering on the platform). The caller is typically the
// onboarding module (Phase 3). signupChannel distinguishes self-serve
// signups from sales-assisted provisioning.
func (s *Service) CreateExternalTenant(ctx context.Context, ownerUUID, name, slug, signupChannel string, parentTenantUUID *string) (*models.Tenant, error) {
	if signupChannel == "" {
		signupChannel = models.SignupChannelSelfServe
	}
	input := models.CreateTenantInput{
		Name:             name,
		Slug:             slug,
		Kind:             models.TenantKindExternal,
		ParentTenantUUID: parentTenantUUID,
	}
	t, err := s.CreateTenant(ctx, ownerUUID, input)
	if err != nil {
		return nil, err
	}
	t.SignupChannel = signupChannel
	t.Status = models.TenantStatusProvisioning
	if err := s.repo.UpdateTenant(ctx, t.UUID, bson.M{
		"signupChannel": signupChannel,
		"status":        string(models.TenantStatusProvisioning),
	}); err != nil {
		return nil, err
	}
	s.emitAudit(ctx, iface.AuditEvent{
		TenantID:     t.UUID,
		TenantKind:   string(t.Kind),
		ActorUserID:  ownerUUID,
		ActorType:    "user",
		Action:       "tenant.lifecycle.provisioned",
		ResourceType: "tenant",
		ResourceID:   t.UUID,
		Metadata:     map[string]any{"signupChannel": signupChannel, "name": t.Name, "slug": t.Slug},
	})
	return t, nil
}

// CreateDivision creates a sub-tenant under the given external parent. A
// division is a Tier-2 tenant with Kind=external and ParentTenantUUID set
// to parentUUID. Internal (operator) tenants cannot have divisions — the
// division concept exists only for external clients that run multi-workspace
// organisations. Status is `active` (operator-seeded, not self-serve) and
// SignupChannel=seeded.
//
// Callers:
//   - Platform admins via /v1/admin/tenants/{parentId}/divisions (system.tenants.admin)
//   - Tenant members with tenant.update on the parent via
//     /v1/tenants/{parentId}/divisions
//
// Slug uniqueness is global (same as the regular create path); callers that
// hit a clash can retry with a distinct slug.
func (s *Service) CreateDivision(ctx context.Context, parentUUID, ownerUUID, name, slug string) (*models.Tenant, error) {
	if parentUUID == "" {
		return nil, errors.New("tenant: CreateDivision requires parentUUID")
	}
	if ownerUUID == "" {
		return nil, errors.New("tenant: CreateDivision requires ownerUUID")
	}
	parent, err := s.repo.GetTenantByUUID(ctx, parentUUID)
	if err != nil {
		return nil, fmt.Errorf("tenant: parent lookup: %w", err)
	}
	if parent.Kind != models.TenantKindExternal {
		return nil, fmt.Errorf("tenant: divisions are only allowed under external parents (parent kind=%s)", parent.Kind)
	}
	input := models.CreateTenantInput{
		Name:             name,
		Slug:             slug,
		Kind:             models.TenantKindExternal,
		ParentTenantUUID: &parentUUID,
	}
	t, err := s.CreateTenant(ctx, ownerUUID, input)
	if err != nil {
		return nil, err
	}
	s.emitAudit(ctx, iface.AuditEvent{
		TenantID:     t.UUID,
		TenantKind:   string(t.Kind),
		ActorUserID:  ownerUUID,
		ActorType:    "user",
		Action:       "tenant.division.created",
		ResourceType: "tenant",
		ResourceID:   t.UUID,
		Metadata: map[string]any{
			"parentTenantUUID": parentUUID,
			"name":             t.Name,
			"slug":             t.Slug,
		},
	})
	return t, nil
}

// ListDivisions returns the direct children of the given tenant — rows
// whose ParentTenantUUID equals parentUUID. The closure table supports
// arbitrary-depth descendants but this iteration's UX shows depth=1 only.
// Archived/purged rows are filtered server-side by the repo filter.
func (s *Service) ListDivisions(ctx context.Context, parentUUID string) ([]models.Tenant, error) {
	if parentUUID == "" {
		return []models.Tenant{}, nil
	}
	parent := parentUUID
	return s.repo.ListTenants(ctx, repository.TenantListFilter{ParentTenantUUID: &parent})
}

// MarkTenantActive flips a provisioning tenant to active once the onboarding
// saga (KMS key, IdP defaults, trial subscription, welcome email) completes.
func (s *Service) MarkTenantActive(ctx context.Context, tenantUUID string) error {
	if err := s.repo.UpdateTenantStatus(ctx, tenantUUID, models.TenantStatusActive); err != nil {
		return err
	}
	s.emitLifecycle(ctx, "tenant.lifecycle.activated", tenantUUID)
	return nil
}

// SuspendTenant, ArchiveTenant, PurgeTenant drive lifecycle transitions.
// PurgeTenant eventually triggers crypto-shred of the tenant's KMS key
// (Phase 4); today it only flips the status.
//
// Every one of these four lifecycle mutations — Suspend, Archive, Delete,
// Purge — wraps its status/deletedAt write in repository.RunDefaultGuarded
// so the platform default Tier-1 tenant cannot be suspended, archived,
// soft-deleted, or purged out from under the platform without an explicit
// TransferDefaultTenant first. The guard lives here, not only at the HTTP
// handler boundary, because it is an invariant every caller must observe —
// including non-HTTP callers (background flows, later saga stages) that
// never pass through a handler at all.
func (s *Service) SuspendTenant(ctx context.Context, tenantUUID string) error {
	if err := s.repo.RunDefaultGuarded(ctx, models.TenantKindInternal, tenantUUID, func(sc mongo.SessionContext) error {
		return s.repo.UpdateTenantStatus(sc, tenantUUID, models.TenantStatusSuspended)
	}); err != nil {
		if errors.Is(err, repository.ErrDefaultGuard) {
			s.emitDefaultGuardDenied(ctx, "tenant.lifecycle.suspended", tenantUUID)
			return ErrDefaultReassignmentRequired
		}
		return err
	}
	s.emitLifecycle(ctx, "tenant.lifecycle.suspended", tenantUUID)
	return nil
}

func (s *Service) ArchiveTenant(ctx context.Context, tenantUUID string) error {
	if err := s.repo.RunDefaultGuarded(ctx, models.TenantKindInternal, tenantUUID, func(sc mongo.SessionContext) error {
		return s.repo.UpdateTenantStatus(sc, tenantUUID, models.TenantStatusArchived)
	}); err != nil {
		if errors.Is(err, repository.ErrDefaultGuard) {
			s.emitDefaultGuardDenied(ctx, "tenant.lifecycle.archived", tenantUUID)
			return ErrDefaultReassignmentRequired
		}
		return err
	}
	s.emitLifecycle(ctx, "tenant.lifecycle.archived", tenantUUID)
	return nil
}

func (s *Service) PurgeTenant(ctx context.Context, tenantUUID string) error {
	// Cheap pre-check FIRST, before the (expensive) cascade runs at all —
	// covers the common denied case without paying for cascadeTenantData.
	// This check alone is racy (a concurrent transfer could move the
	// default between this read and the guarded write below); the guarded
	// UpdateTenantStatus write further down is the actual invariant that
	// protects the tenant row. A genuine repository error from
	// DefaultTenantUUID here is deliberately swallowed (err == nil guards
	// the comparison, so an error just falls through to the cascade) — the
	// guarded write below still enforces the invariant even when this
	// optimization couldn't run.
	if def, err := s.DefaultTenantUUID(ctx); err == nil && def == tenantUUID {
		s.emitDefaultGuardDenied(ctx, "tenant.lifecycle.purged", tenantUUID)
		return ErrDefaultReassignmentRequired
	}

	// Fetch first so we know the KMSKeyID (if any) before flipping
	// status — the row is still readable in purged state but carrying
	// a live keyID would defeat crypto-shred. Include soft-deleted rows:
	// the documented flow is archive/soft-delete → purge, and the plain
	// getter filters deletedAt:nil, so on that path existing would be nil
	// and the crypto-shred + authz-binding cascade would silently no-op.
	existing, lookupErr := s.repo.GetTenantByUUIDIncludingDeleted(ctx, tenantUUID)
	cascadeCtx := s.buildPostDeleteContext(ctx, existing, true)
	if err := s.cascadeTenantData(ctx, tenantUUID); err != nil {
		return err
	}
	if err := s.repo.RunDefaultGuarded(ctx, models.TenantKindInternal, tenantUUID, func(sc mongo.SessionContext) error {
		return s.repo.UpdateTenantStatus(sc, tenantUUID, models.TenantStatusPurged)
	}); err != nil {
		if errors.Is(err, repository.ErrDefaultGuard) {
			s.emitDefaultGuardDenied(ctx, "tenant.lifecycle.purged", tenantUUID)
			return ErrDefaultReassignmentRequired
		}
		return err
	}
	// Crypto-shred: delete the DEK so every ciphertext written under
	// it becomes unrecoverable. Best-effort at the purge boundary —
	// log and continue if the KMS provider is transiently unhealthy;
	// the key row stays active and can be shredded manually. Without
	// crypto-shred the row is still marked purged and downstream
	// reads are blocked by status gating.
	if s.kms != nil && lookupErr == nil && existing != nil && existing.KMSKeyID != nil && *existing.KMSKeyID != "" {
		if err := s.kms.DeleteKey(ctx, *existing.KMSKeyID); err != nil {
			// The audit row below still fires so auditors see the
			// attempt; a retry pathway is tracked as tech debt.
			_ = err
		}
	}
	s.runPostDeleteHooks(ctx, cascadeCtx)
	s.emitLifecycle(ctx, "tenant.lifecycle.purged", tenantUUID)
	return nil
}

// emitLifecycle is shared boilerplate for the status-transition emits.
func (s *Service) emitLifecycle(ctx context.Context, action, tenantUUID string) {
	userUUID, email, actorType := actorFromContext(ctx)
	s.emitAudit(ctx, iface.AuditEvent{
		TenantID:     tenantUUID,
		ActorUserID:  userUUID,
		ActorEmail:   email,
		ActorType:    actorType,
		Action:       action,
		ResourceType: "tenant",
		ResourceID:   tenantUUID,
	})
}

func (s *Service) UpdateTenant(ctx context.Context, tenantUUID string, input models.UpdateTenantInput) error {
	update := bson.M{}
	if input.Name != nil {
		update["name"] = strings.TrimSpace(*input.Name)
	}
	if input.Slug != nil {
		slug := slugify(*input.Slug)
		if existing, _ := s.repo.GetTenantBySlug(ctx, slug); existing != nil && existing.UUID != tenantUUID {
			return fmt.Errorf("%w: %s", ErrSlugAlreadyInUse, slug)
		}
		update["slug"] = slug
	}
	if input.Settings != nil {
		update["settings"] = input.Settings
	}
	if len(update) == 0 {
		return nil
	}
	return tenantWriteError(s.repo.UpdateTenant(ctx, tenantUUID, update))
}

// UpdatePlan updates the tenant's plan label. Plan is informational only —
// entitlements are driven by the capability projection (see HasCapability /
// GrantCapability) and are not derived from the plan name.
func (s *Service) UpdatePlan(ctx context.Context, tenantUUID string, input models.UpdatePlanInput) error {
	return s.repo.UpdateTenant(ctx, tenantUUID, bson.M{"plan": input.Plan})
}

func (s *Service) DeleteTenant(ctx context.Context, tenantUUID string) error {
	// Cheap pre-check FIRST, before the (expensive) cascade runs at all —
	// covers the common denied case without paying for cascadeTenantData.
	// This check alone is racy (a concurrent transfer could move the
	// default between this read and the guarded write below); the guarded
	// SoftDeleteTenant write further down is the actual invariant that
	// protects the tenant row. A genuine repository error from
	// DefaultTenantUUID here is deliberately swallowed (err == nil guards
	// the comparison, so an error just falls through to the cascade) — the
	// guarded write below still enforces the invariant even when this
	// optimization couldn't run.
	if def, err := s.DefaultTenantUUID(ctx); err == nil && def == tenantUUID {
		s.emitDefaultGuardDenied(ctx, "tenant.deleted", tenantUUID)
		return ErrDefaultReassignmentRequired
	}

	// Fetch the tenant before mutating so the cascade context
	// (kind / owner / orphan flag) is computed against the pre-delete
	// state. A missing row falls through with a nil snapshot — hooks
	// already tolerate empty fields and the soft-delete below will
	// surface ErrNotFound the same as before.
	existing, _ := s.repo.GetTenantByUUID(ctx, tenantUUID)
	cascadeCtx := s.buildPostDeleteContext(ctx, existing, false)
	if err := s.cascadeTenantData(ctx, tenantUUID); err != nil {
		return err
	}
	if err := s.repo.RunDefaultGuarded(ctx, models.TenantKindInternal, tenantUUID, func(sc mongo.SessionContext) error {
		return s.repo.SoftDeleteTenant(sc, tenantUUID)
	}); err != nil {
		if errors.Is(err, repository.ErrDefaultGuard) {
			s.emitDefaultGuardDenied(ctx, "tenant.deleted", tenantUUID)
			return ErrDefaultReassignmentRequired
		}
		return err
	}
	s.runPostDeleteHooks(ctx, cascadeCtx)
	s.emitLifecycle(ctx, "tenant.deleted", tenantUUID)
	return nil
}

// cascadeTenantData hard-deletes data the tenant module owns directly:
// memberships and the closure-table rows. Memberships have no soft-delete
// pattern (DeleteMembership has always hard-deleted singles) and ancestors
// are pure derived data, so dropping them outright matches the existing
// invariants. Cross-module data (authz bindings, the owner's user row) is
// handled by registered hooks.
func (s *Service) cascadeTenantData(ctx context.Context, tenantUUID string) error {
	if _, err := s.repo.DeleteMembershipsByTenant(ctx, tenantUUID); err != nil {
		return fmt.Errorf("tenant: drop memberships: %w", err)
	}
	if _, err := s.repo.DeleteAncestorsByTenant(ctx, tenantUUID); err != nil {
		return fmt.Errorf("tenant: drop ancestors: %w", err)
	}
	return nil
}

// buildPostDeleteContext snapshots the data hooks need before mutation.
// The orphan flag is computed against the pre-cascade membership set so
// "owner has at least one other tenant" stays true even though we are
// about to drop the membership for THIS tenant — the count check filters
// the deleting tenant out explicitly.
func (s *Service) buildPostDeleteContext(ctx context.Context, t *models.Tenant, hard bool) TenantPostDeleteContext {
	out := TenantPostDeleteContext{Hard: hard}
	if t == nil {
		return out
	}
	out.TenantUUID = t.UUID
	out.Kind = string(t.Kind)
	out.OwnerUserUUID = t.OwnerUserUUID
	if t.OwnerUserUUID == "" {
		return out
	}
	memberships, err := s.repo.ListMembershipsByUser(ctx, t.OwnerUserUUID)
	if err != nil {
		// Be conservative on a lookup failure: assume the owner has
		// other tenants so we never accidentally evict an account
		// that's still in use elsewhere.
		out.OwnerHasOtherTenants = true
		return out
	}
	for i := range memberships {
		if memberships[i].TenantUUID != t.UUID {
			out.OwnerHasOtherTenants = true
			return out
		}
	}
	return out
}

// runPostDeleteHooks fans out the cascade to subscribers. Hooks are
// best-effort: an error is recorded as an audit event but does not abort
// the remaining hooks — leaving the system half-cascaded because hook 2
// failed would be worse than continuing.
func (s *Service) runPostDeleteHooks(ctx context.Context, c TenantPostDeleteContext) {
	for _, hook := range s.postDeleteHooks {
		if hook == nil {
			continue
		}
		if err := hook(ctx, c); err != nil {
			s.emitAudit(ctx, iface.AuditEvent{
				TenantID:     c.TenantUUID,
				Action:       "tenant.cascade.hook_failed",
				ResourceType: "tenant",
				ResourceID:   c.TenantUUID,
				Outcome:      "failure",
				Metadata:     map[string]any{"error": err.Error()},
			})
		}
	}
}

// TenantAdminView is a tenant plus its current member count, used by the
// platform-admin list endpoint to avoid an N+1. When the caller passed a Q
// filter, MatchedMembers carries up to repository.MaxMatchedMembersPerTenant
// member-side hits so the UI can show "matched: alice@x" chips on each row.
type TenantAdminView struct {
	Tenant         *models.Tenant
	MemberCount    int
	MatchedMembers []repository.MemberMatch
}

// ListAllTenants returns every tenant in the system with live member counts.
// Used by the platform admin tenant management page — bypasses per-tenant
// membership gates and is only callable via system.tenants.admin.
func (s *Service) ListAllTenants(ctx context.Context, includeDeleted bool) ([]TenantAdminView, error) {
	return s.ListAllTenantsFiltered(ctx, repository.TenantListFilter{IncludeDeleted: includeDeleted})
}

// adminListRepo is the slice of repository.Repository that
// listAllTenantsFiltered needs. Extracted so the routing decision (Q trim,
// search-vs-list dispatch, count attachment) can be tested with a fake repo
// without spinning up Mongo.
type adminListRepo interface {
	ListTenants(ctx context.Context, f repository.TenantListFilter) ([]models.Tenant, error)
	SearchTenantsByQ(ctx context.Context, f repository.TenantListFilter) ([]repository.TenantSearchResult, error)
	CountMembersByTenants(ctx context.Context, tenantUUIDs []string) (map[string]int, error)
}

// ListAllTenantsFiltered is the kind/parent-aware variant used by the Phase 3
// split between the Internal Tenants and Clients admin pages. When filter.Q
// is non-empty it routes to the member-aware aggregation in
// repository.SearchTenantsByQ so the search box on /admin/clients can match
// tenant name + slug + member email/fullName/username in a single round trip.
func (s *Service) ListAllTenantsFiltered(ctx context.Context, filter repository.TenantListFilter) ([]TenantAdminView, error) {
	return listAllTenantsFiltered(ctx, s.repo, filter)
}

func listAllTenantsFiltered(ctx context.Context, repo adminListRepo, filter repository.TenantListFilter) ([]TenantAdminView, error) {
	if strings.TrimSpace(filter.Q) != "" {
		results, err := repo.SearchTenantsByQ(ctx, filter)
		if err != nil {
			return nil, err
		}
		out := make([]TenantAdminView, len(results))
		for i := range results {
			t := results[i].Tenant
			out[i] = TenantAdminView{
				Tenant:         &t,
				MemberCount:    results[i].MemberCount,
				MatchedMembers: results[i].MatchedMembers,
			}
		}
		return out, nil
	}
	tenants, err := repo.ListTenants(ctx, filter)
	if err != nil {
		return nil, err
	}
	if len(tenants) == 0 {
		return []TenantAdminView{}, nil
	}
	uuids := make([]string, len(tenants))
	for i := range tenants {
		uuids[i] = tenants[i].UUID
	}
	counts, err := repo.CountMembersByTenants(ctx, uuids)
	if err != nil {
		return nil, err
	}
	out := make([]TenantAdminView, len(tenants))
	for i := range tenants {
		t := tenants[i]
		out[i] = TenantAdminView{Tenant: &t, MemberCount: counts[t.UUID]}
	}
	return out, nil
}

// --- Hierarchy queries (closure table) ---

// GetAncestors returns every ancestor of tenantUUID (including itself at
// depth 0), sorted by depth ascending.
func (s *Service) GetAncestors(ctx context.Context, tenantUUID string) ([]models.TenantAncestor, error) {
	return s.repo.ListAncestors(ctx, tenantUUID)
}

// GetDescendantUUIDs returns every descendant UUID (including the tenant
// itself).
func (s *Service) GetDescendantUUIDs(ctx context.Context, tenantUUID string) ([]string, error) {
	return s.repo.ListDescendantUUIDs(ctx, tenantUUID)
}

// IsDescendantOf reports whether descendant is inside the tree rooted at
// ancestor (inclusive).
func (s *Service) IsDescendantOf(ctx context.Context, ancestorUUID, descendantUUID string) (bool, error) {
	return s.repo.IsAncestorOf(ctx, ancestorUUID, descendantUUID)
}

// --- Memberships ---

// ErrMembershipExists is returned by AttachMember when (userUUID, tenantUUID)
// already has a membership row. Admin-attach is intentionally not a "re-bind
// with a different role" path — separating create-vs-update at the service
// boundary keeps the authz binding mutation explicit (callers must use
// SetMemberRoles + a separate authz binding update to change role).
var ErrMembershipExists = errors.New("tenant: user is already a member of tenant")

// ErrAttachInput is returned when the inputs to AttachMember are missing or
// blank. The handler maps this to 400 — the callers are admins, not anonymous
// clients, so a clear validation error is appropriate.
var ErrAttachInput = errors.New("tenant: attach requires non-empty tenantUUID, userUUID, role")

func (s *Service) ListMembers(ctx context.Context, tenantUUID string) ([]models.TenantMembership, error) {
	return s.repo.ListMembershipsByTenant(ctx, tenantUUID)
}

func (s *Service) RemoveMember(ctx context.Context, tenantUUID, userUUID string) error {
	// Drop the member's tenant-scoped authz binding(s) first so a removed
	// member never keeps permissions, and a later re-attach can't union a
	// stale role. Unbind-before-delete fails toward LESS access: if the
	// membership delete then fails, the member is left without a binding
	// rather than with a dangling one. Nil unbinder (authz disabled / tests)
	// keeps the legacy membership-only delete.
	if s.unbindMember != nil {
		if err := s.unbindMember(ctx, userUUID, tenantUUID); err != nil {
			return fmt.Errorf("tenant: unbind member on remove: %w", err)
		}
	}
	return s.repo.DeleteMembership(ctx, userUUID, tenantUUID)
}

// SetMemberRoles changes a member's tenant role(s) and re-points their authz
// binding to match. Membership.Roles is only a denormalized hint; the authz
// binding is the source of truth for permissions, so the two must move
// together — updating the denorm alone (the previous behavior) left the old
// role's binding granting the old permissions, which is how a role "change"
// via remove+re-attach silently accumulated grants. The first role drives the
// binding (one tenant-scoped role per membership, mirroring AttachMember);
// any extra entries are stored on the denorm only. Returns
// repository.ErrNotFound when the user is not a member of the tenant. Nil
// hooks (authz disabled / tests) fall back to a denorm-only update.
func (s *Service) SetMemberRoles(ctx context.Context, tenantUUID, userUUID string, roles []string) error {
	if _, err := s.repo.GetMembership(ctx, userUUID, tenantUUID); err != nil {
		return err
	}
	if err := s.repo.UpdateMembershipRoles(ctx, userUUID, tenantUUID, roles); err != nil {
		return err
	}
	if s.unbindMember == nil || s.bindOwner == nil {
		return nil
	}
	if err := s.unbindMember(ctx, userUUID, tenantUUID); err != nil {
		return fmt.Errorf("tenant: unbind member on role change: %w", err)
	}
	if len(roles) > 0 && strings.TrimSpace(roles[0]) != "" {
		if err := s.bindOwner(ctx, userUUID, tenantUUID, roles[0]); err != nil {
			return fmt.Errorf("tenant: bind new role on role change: %w", err)
		}
	}
	return nil
}

// AttachMember binds an existing user to an existing tenant with a single
// tenant-scoped role. Used by the operator-admin direct-grant flow that
// replaces the retired token-based invite (Phase 5 of the polymorphic-owner
// refactor) — operators curate which clients aggregate under which tenants
// without going through an email-invite handshake.
//
// Behavior:
//   - 404 (repository.ErrNotFound) when the tenant is missing or soft-deleted
//   - 409 (ErrMembershipExists) when the user is already a member; admins
//     change roles via the (future) SetMemberRoles route, not by re-attach
//   - the membership is inserted with Roles=[roleName], IsOwner=isOwner
//   - the OwnerRoleBinder hook (wired by authz) creates the authz binding
//     for the named role using granter="system" so the cascade rule does
//     not block platform-issued grants. Without authz wired the membership
//     still persists — the role name on Membership.Roles is informational
//     and the user has no extra permissions until a binding lands later.
//
// Idempotency: not idempotent on input — each call requires a clean state.
// The tenant lookup happens before the membership write so a missing tenant
// 404s cleanly without a half-attached row.
func (s *Service) AttachMember(ctx context.Context, tenantUUID, userUUID, roleName string, isOwner bool) (*models.TenantMembership, error) {
	if strings.TrimSpace(tenantUUID) == "" || strings.TrimSpace(userUUID) == "" || strings.TrimSpace(roleName) == "" {
		return nil, ErrAttachInput
	}
	t, err := s.repo.GetTenantByUUID(ctx, tenantUUID)
	if err != nil {
		return nil, err
	}
	if t.DeletedAt != nil {
		return nil, repository.ErrNotFound
	}
	if existing, err := s.repo.GetMembership(ctx, userUUID, tenantUUID); err == nil && existing != nil {
		return nil, ErrMembershipExists
	} else if err != nil && !errors.Is(err, repository.ErrNotFound) {
		return nil, err
	}
	membership := &models.TenantMembership{
		UUID:       uuid.Must(uuid.NewV7()).String(),
		UserUUID:   userUUID,
		TenantUUID: tenantUUID,
		TenantKind: t.Kind,
		Roles:      []string{roleName},
		IsOwner:    isOwner,
	}
	if err := s.repo.CreateMembership(ctx, membership); err != nil {
		return nil, err
	}
	if s.bindOwner != nil {
		if err := s.bindOwner(ctx, userUUID, tenantUUID, roleName); err != nil {
			// Roll back the membership so the operator sees a clean failure
			// rather than a half-attached row with no authz binding.
			_ = s.repo.DeleteMembership(ctx, userUUID, tenantUUID)
			return nil, fmt.Errorf("tenant: bind role on attach: %w", err)
		}
	}
	actor, _, _ := actorFromContext(ctx)
	if actor == "" {
		actor = "system"
	}
	s.emitAudit(ctx, iface.AuditEvent{
		TenantID:     tenantUUID,
		ActorUserID:  actor,
		Action:       "tenant.member.attached",
		ResourceType: "tenant_membership",
		ResourceID:   membership.UUID,
		Metadata: map[string]any{
			"userUUID": userUUID,
			"role":     roleName,
			"isOwner":  isOwner,
		},
	})
	return membership, nil
}

// --- Invites ---

// CreateInvite generates a single-use invite token, persists only its hash,
// and returns the raw token exactly once on the struct's transient Token
// field. Callers must relay the raw token to the invitee immediately.
func (s *Service) CreateInvite(ctx context.Context, tenantUUID, invitedBy string, input models.InviteInput) (*models.TenantInvite, error) {
	raw, hash, err := generateInviteToken()
	if err != nil {
		return nil, fmt.Errorf("tenant: generate invite token: %w", err)
	}
	inv := &models.TenantInvite{
		UUID:       uuid.Must(uuid.NewV7()).String(),
		TenantUUID: tenantUUID,
		Email:      strings.ToLower(strings.TrimSpace(input.Email)),
		Roles:      input.Roles,
		Token:      raw, // transient: returned once, not persisted
		TokenHash:  hash,
		InvitedBy:  invitedBy,
		ExpiresAt:  time.Now().Add(7 * 24 * time.Hour),
	}
	if err := s.repo.CreateInvite(ctx, inv); err != nil {
		return nil, err
	}
	return inv, nil
}

// ListInvites returns invites for a tenant. Caller scopes visibility:
// pending-only by default, all invites when onlyPending is false. Raw tokens
// are zeroed out before returning.
func (s *Service) ListInvites(ctx context.Context, tenantUUID string, onlyPending bool) ([]models.TenantInvite, error) {
	invs, err := s.repo.ListInvitesByTenant(ctx, tenantUUID, onlyPending)
	if err != nil {
		return nil, err
	}
	for i := range invs {
		invs[i].Token = ""
	}
	return invs, nil
}

// RevokeInvite deletes a pending invite by UUID. The tenantUUID is required
// to prevent cross-tenant spoofing via a guessed invite UUID.
func (s *Service) RevokeInvite(ctx context.Context, tenantUUID, inviteUUID string) error {
	return s.repo.DeleteInvite(ctx, tenantUUID, inviteUUID)
}

func (s *Service) AcceptInvite(ctx context.Context, userUUID, token string) (*models.Tenant, error) {
	inv, err := s.repo.GetInviteByTokenHash(ctx, hashInviteToken(token))
	if err != nil {
		return nil, err
	}
	if inv.AcceptedAt != nil {
		return nil, errors.New("invite already accepted")
	}
	if time.Now().After(inv.ExpiresAt) {
		return nil, errors.New("invite expired")
	}
	membership := &models.TenantMembership{
		UUID:       uuid.Must(uuid.NewV7()).String(),
		UserUUID:   userUUID,
		TenantUUID: inv.TenantUUID,
		Roles:      inv.Roles,
		InvitedBy:  inv.InvitedBy,
	}
	if err := s.repo.CreateMembership(ctx, membership); err != nil {
		return nil, err
	}
	if err := s.repo.MarkInviteAccepted(ctx, inv.UUID); err != nil {
		return nil, err
	}
	return s.repo.GetTenantByUUID(ctx, inv.TenantUUID)
}

func (s *Service) GetTenantModel(ctx context.Context, tenantUUID string) (*models.Tenant, error) {
	return s.repo.GetTenantByUUID(ctx, tenantUUID)
}

// --- Helpers ---

func slugify(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	var b strings.Builder
	lastDash := false
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
			lastDash = false
		case r == ' ' || r == '-' || r == '_':
			if !lastDash && b.Len() > 0 {
				b.WriteByte('-')
				lastDash = true
			}
		}
	}
	return strings.TrimRight(b.String(), "-")
}

// generateInviteToken produces 32 random bytes → base64url → SHA-256 hex
// digest. The raw token is returned to the caller once; only the hash is
// stored.
func generateInviteToken() (raw, hash string, err error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", "", err
	}
	raw = base64.RawURLEncoding.EncodeToString(buf)
	hash = hashInviteToken(raw)
	return raw, hash, nil
}

func hashInviteToken(raw string) string {
	sum := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(sum[:])
}
