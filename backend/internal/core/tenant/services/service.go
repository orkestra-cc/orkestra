package services

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/orkestra/backend/internal/core/tenant/models"
	"github.com/orkestra/backend/internal/core/tenant/repository"
	"github.com/orkestra/backend/internal/shared/iface"
	"go.mongodb.org/mongo-driver/bson"
)

// Service owns tenant lifecycle and implements iface.TenantProvider.
type Service struct {
	repo            *repository.Repository
	auditSink       iface.AuditSink
	kms             iface.KMSProvider
	bindOwner       OwnerRoleBinder
	postDeleteHooks []TenantPostDeleteHook
	// userDisplay is the lazy lookup the unified-clients refactor (Phase 1)
	// uses to seed a personal tenant's Name from the owner User's FullName
	// (EnsureTenantForUser) and to render the FatturaPA CedentePrestatore
	// party name for sole-proprietor tenants (ResolveBillingParty). Wired by
	// the tenant module's Init from the registered ClientUserProvider.
	userDisplay UserDisplayResolver
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

// --- Tenant lifecycle ---

func (s *Service) CreateTenant(ctx context.Context, ownerUUID string, input models.CreateTenantInput) (*models.Tenant, error) {
	slug := slugify(input.Slug)
	if slug == "" {
		slug = slugify(input.Name)
	}
	if existing, _ := s.repo.GetTenantBySlug(ctx, slug); existing != nil {
		return nil, fmt.Errorf("slug already in use: %s", slug)
	}

	plan := input.Plan
	if plan == "" {
		plan = models.PlanFree
	}

	kind := input.Kind
	if !kind.Valid() {
		kind = models.TenantKindInternal
	}

	var parent *string
	if input.ParentTenantUUID != nil && *input.ParentTenantUUID != "" {
		if kind != models.TenantKindExternal {
			return nil, fmt.Errorf("parentTenantUUID is only allowed for external tenants")
		}
		p := *input.ParentTenantUUID
		if _, err := s.repo.GetTenantByUUID(ctx, p); err != nil {
			return nil, fmt.Errorf("parent tenant not found: %s", p)
		}
		parent = &p
	}

	sigChan := models.SignupChannelSeeded
	if kind == models.TenantKindExternal {
		sigChan = models.SignupChannelSalesAssisted
	}

	t := &models.Tenant{
		UUID:             uuid.Must(uuid.NewV7()).String(),
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
		return nil, err
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
func (s *Service) SuspendTenant(ctx context.Context, tenantUUID string) error {
	if err := s.repo.UpdateTenantStatus(ctx, tenantUUID, models.TenantStatusSuspended); err != nil {
		return err
	}
	s.emitLifecycle(ctx, "tenant.lifecycle.suspended", tenantUUID)
	return nil
}

func (s *Service) ArchiveTenant(ctx context.Context, tenantUUID string) error {
	if err := s.repo.UpdateTenantStatus(ctx, tenantUUID, models.TenantStatusArchived); err != nil {
		return err
	}
	s.emitLifecycle(ctx, "tenant.lifecycle.archived", tenantUUID)
	return nil
}

func (s *Service) PurgeTenant(ctx context.Context, tenantUUID string) error {
	// Fetch first so we know the KMSKeyID (if any) before flipping
	// status — the row is still readable in purged state but carrying
	// a live keyID would defeat crypto-shred.
	existing, lookupErr := s.repo.GetTenantByUUID(ctx, tenantUUID)
	cascadeCtx := s.buildPostDeleteContext(ctx, existing, true)
	if err := s.cascadeTenantData(ctx, tenantUUID); err != nil {
		return err
	}
	if err := s.repo.UpdateTenantStatus(ctx, tenantUUID, models.TenantStatusPurged); err != nil {
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
			return fmt.Errorf("slug already in use: %s", slug)
		}
		update["slug"] = slug
	}
	if input.Settings != nil {
		update["settings"] = input.Settings
	}
	if len(update) == 0 {
		return nil
	}
	return s.repo.UpdateTenant(ctx, tenantUUID, update)
}

// UpdatePlan updates the tenant's plan label. Plan is informational only —
// entitlements are driven by the capability projection (see HasCapability /
// GrantCapability) and are not derived from the plan name.
func (s *Service) UpdatePlan(ctx context.Context, tenantUUID string, input models.UpdatePlanInput) error {
	return s.repo.UpdateTenant(ctx, tenantUUID, bson.M{"plan": input.Plan})
}

func (s *Service) DeleteTenant(ctx context.Context, tenantUUID string) error {
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
	if err := s.repo.SoftDeleteTenant(ctx, tenantUUID); err != nil {
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
	return s.repo.DeleteMembership(ctx, userUUID, tenantUUID)
}

func (s *Service) SetMemberRoles(ctx context.Context, tenantUUID, userUUID string, roles []string) error {
	return s.repo.UpdateMembershipRoles(ctx, userUUID, tenantUUID, roles)
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
