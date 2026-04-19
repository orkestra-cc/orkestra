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
	repo      *repository.Repository
	auditSink iface.AuditSink
}

func New(repo *repository.Repository) *Service {
	return &Service{repo: repo}
}

// SetAuditSink wires the compliance audit sink post-construction. Optional —
// the emit* helpers tolerate a nil sink when the compliance module is
// disabled or initializing later in the topological order.
func (s *Service) SetAuditSink(sink iface.AuditSink) { s.auditSink = sink }

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
	}, nil
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

	// Owner is auto-enrolled as a member with the "administrator" role.
	membership := &models.TenantMembership{
		UUID:       uuid.Must(uuid.NewV7()).String(),
		UserUUID:   ownerUUID,
		TenantUUID: t.UUID,
		TenantKind: kind,
		Roles:      []string{"administrator"},
		IsOwner:    true,
	}
	if err := s.repo.CreateMembership(ctx, membership); err != nil {
		return nil, err
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
	if err := s.repo.UpdateTenantStatus(ctx, tenantUUID, models.TenantStatusPurged); err != nil {
		return err
	}
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
	if err := s.repo.SoftDeleteTenant(ctx, tenantUUID); err != nil {
		return err
	}
	s.emitLifecycle(ctx, "tenant.deleted", tenantUUID)
	return nil
}

// TenantAdminView is a tenant plus its current member count, used by the
// platform-admin list endpoint to avoid an N+1.
type TenantAdminView struct {
	Tenant      *models.Tenant
	MemberCount int
}

// ListAllTenants returns every tenant in the system with live member counts.
// Used by the platform admin tenant management page — bypasses per-tenant
// membership gates and is only callable via system.tenants.admin.
func (s *Service) ListAllTenants(ctx context.Context, includeDeleted bool) ([]TenantAdminView, error) {
	tenants, err := s.repo.ListAllTenants(ctx, includeDeleted)
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
	counts, err := s.repo.CountMembersByTenants(ctx, uuids)
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

func (s *Service) ListMembers(ctx context.Context, tenantUUID string) ([]models.TenantMembership, error) {
	return s.repo.ListMembershipsByTenant(ctx, tenantUUID)
}

func (s *Service) RemoveMember(ctx context.Context, tenantUUID, userUUID string) error {
	return s.repo.DeleteMembership(ctx, userUUID, tenantUUID)
}

func (s *Service) SetMemberRoles(ctx context.Context, tenantUUID, userUUID string, roles []string) error {
	return s.repo.UpdateMembershipRoles(ctx, userUUID, tenantUUID, roles)
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
