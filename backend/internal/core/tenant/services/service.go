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
	repo *repository.Repository
}

func New(repo *repository.Repository) *Service {
	return &Service{repo: repo}
}

// --- Provider interface ---

func (s *Service) GetOrg(ctx context.Context, orgUUID string) (*iface.Org, error) {
	o, err := s.repo.GetOrgByUUID(ctx, orgUUID)
	if err != nil {
		return nil, err
	}
	kind := string(o.Kind)
	if kind == "" {
		kind = iface.TenantKindInternal
	}
	status := string(o.Status)
	if status == "" {
		status = iface.TenantStatusActive
	}
	var parent string
	if o.ParentTenantUUID != nil {
		parent = *o.ParentTenantUUID
	}
	return &iface.Org{
		UUID:             o.UUID,
		Kind:             kind,
		ParentTenantUUID: parent,
		Status:           status,
		Name:             o.Name,
		Slug:             o.Slug,
		Plan:             o.Plan,
		Features:         o.Features,
	}, nil
}

func (s *Service) ListUserMemberships(ctx context.Context, userUUID string) ([]iface.Membership, error) {
	mbrs, err := s.repo.ListMembershipsByUser(ctx, userUUID)
	if err != nil {
		return nil, err
	}
	out := make([]iface.Membership, 0, len(mbrs))
	for _, m := range mbrs {
		o, err := s.repo.GetOrgByUUID(ctx, m.OrgUUID)
		if err != nil {
			continue // org may be soft-deleted, skip
		}
		// Prefer the cached kind on the membership row; fall back to the
		// tenant row for pre-ADR-0001 memberships where TenantKind is empty.
		kind := string(m.TenantKind)
		if kind == "" {
			kind = string(o.Kind)
		}
		if kind == "" {
			kind = iface.TenantKindInternal
		}
		out = append(out, iface.Membership{
			OrgUUID:    o.UUID,
			OrgName:    o.Name,
			OrgSlug:    o.Slug,
			TenantKind: kind,
			Roles:      m.Roles,
			IsOwner:    m.IsOwner,
		})
	}
	return out, nil
}

func (s *Service) IsMember(ctx context.Context, userUUID, orgUUID string) (bool, error) {
	_, err := s.repo.GetMembership(ctx, userUUID, orgUUID)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

func (s *Service) HasEntitlement(ctx context.Context, orgUUID, feature string) (bool, error) {
	o, err := s.repo.GetOrgByUUID(ctx, orgUUID)
	if err != nil {
		return false, err
	}
	return o.HasFeature(feature), nil
}

// --- Org lifecycle ---

func (s *Service) CreateOrg(ctx context.Context, ownerUUID string, input models.CreateOrgInput) (*models.Org, error) {
	slug := slugify(input.Slug)
	if slug == "" {
		slug = slugify(input.Name)
	}
	if existing, _ := s.repo.GetOrgBySlug(ctx, slug); existing != nil {
		return nil, fmt.Errorf("slug already in use: %s", slug)
	}

	plan := input.Plan
	if plan == "" {
		plan = models.PlanFree
	}
	features := defaultFeaturesForPlan(plan)

	// Tier discriminator. Default to internal so the pre-ADR-0001 code paths
	// (setup wizard, tests) behave unchanged. External client self-registration
	// in Phase 3 will go through CreateExternalTenant instead.
	kind := input.Kind
	if !kind.Valid() {
		kind = models.TenantKindInternal
	}

	// Sub-tenants only apply to external clients. Enforce here rather than
	// in the repo so the invariant surfaces as a 4xx at the handler layer.
	var parent *string
	if input.ParentTenantUUID != nil && *input.ParentTenantUUID != "" {
		if kind != models.TenantKindExternal {
			return nil, fmt.Errorf("parentTenantUUID is only allowed for external tenants")
		}
		p := *input.ParentTenantUUID
		if _, err := s.repo.GetOrgByUUID(ctx, p); err != nil {
			return nil, fmt.Errorf("parent tenant not found: %s", p)
		}
		parent = &p
	}

	sigChan := models.SignupChannelSeeded
	if kind == models.TenantKindExternal {
		sigChan = models.SignupChannelSalesAssisted
	}

	org := &models.Org{
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
		Features:         features,
	}

	if err := s.repo.CreateOrg(ctx, org); err != nil {
		return nil, err
	}

	// Closure-table bookkeeping: self-row at depth 0 for every tenant,
	// plus the transitive chain when a parent is set.
	if err := s.repo.InsertSelfAncestor(ctx, org.UUID); err != nil {
		return nil, fmt.Errorf("tenant: insert self ancestor: %w", err)
	}
	if parent != nil {
		if err := s.repo.AttachToParent(ctx, org.UUID, *parent); err != nil {
			return nil, fmt.Errorf("tenant: attach to parent: %w", err)
		}
	}

	// Owner is auto-enrolled as a member with the "administrator" role.
	membership := &models.Membership{
		UUID:       uuid.Must(uuid.NewV7()).String(),
		UserUUID:   ownerUUID,
		OrgUUID:    org.UUID,
		TenantKind: kind,
		Roles:      []string{"administrator"},
		IsOwner:    true,
	}
	if err := s.repo.CreateMembership(ctx, membership); err != nil {
		return nil, err
	}
	return org, nil
}

// CreateExternalTenant is the dedicated factory for Tier-2 tenants (external
// clients registering on the platform). See ADR-0001. The caller is typically
// the onboarding module (Phase 3). signupChannel distinguishes self-serve
// signups from sales-assisted provisioning for later analytics.
func (s *Service) CreateExternalTenant(ctx context.Context, ownerUUID, name, slug, signupChannel string, parentTenantUUID *string) (*models.Org, error) {
	if signupChannel == "" {
		signupChannel = models.SignupChannelSelfServe
	}
	input := models.CreateOrgInput{
		Name:             name,
		Slug:             slug,
		Kind:             models.TenantKindExternal,
		ParentTenantUUID: parentTenantUUID,
	}
	org, err := s.CreateOrg(ctx, ownerUUID, input)
	if err != nil {
		return nil, err
	}
	org.SignupChannel = signupChannel
	org.Status = models.TenantStatusProvisioning
	if err := s.repo.UpdateOrg(ctx, org.UUID, bson.M{
		"signupChannel": signupChannel,
		"status":        string(models.TenantStatusProvisioning),
	}); err != nil {
		return nil, err
	}
	return org, nil
}

// MarkTenantActive flips a provisioning tenant to active once the onboarding
// saga (KMS key, IdP defaults, trial subscription, welcome email) completes.
func (s *Service) MarkTenantActive(ctx context.Context, tenantUUID string) error {
	return s.repo.UpdateOrgStatus(ctx, tenantUUID, models.TenantStatusActive)
}

// SuspendTenant, ArchiveTenant, PurgeTenant drive the lifecycle transitions
// introduced by ADR-0001. PurgeTenant eventually triggers crypto-shred of
// the tenant's KMS key (Phase 4); today it only flips the status so the
// operator console can exercise the transition end-to-end.
func (s *Service) SuspendTenant(ctx context.Context, tenantUUID string) error {
	return s.repo.UpdateOrgStatus(ctx, tenantUUID, models.TenantStatusSuspended)
}

func (s *Service) ArchiveTenant(ctx context.Context, tenantUUID string) error {
	return s.repo.UpdateOrgStatus(ctx, tenantUUID, models.TenantStatusArchived)
}

func (s *Service) PurgeTenant(ctx context.Context, tenantUUID string) error {
	return s.repo.UpdateOrgStatus(ctx, tenantUUID, models.TenantStatusPurged)
}

// --- Hierarchy queries (closure table) ---

// GetAncestors returns every ancestor of tenantUUID (including itself at
// depth 0), sorted by depth ascending. Useful for policy evaluation that
// needs to walk up the tenant tree.
func (s *Service) GetAncestors(ctx context.Context, tenantUUID string) ([]models.TenantAncestor, error) {
	return s.repo.ListAncestors(ctx, tenantUUID)
}

// GetDescendantUUIDs returns every descendant UUID (including the tenant
// itself). Used for cascade operations — archive parent → mark every
// sub-tenant.
func (s *Service) GetDescendantUUIDs(ctx context.Context, tenantUUID string) ([]string, error) {
	return s.repo.ListDescendantUUIDs(ctx, tenantUUID)
}

// IsDescendantOf reports whether descendant is inside the tree rooted at
// ancestor (inclusive). A tenant is a descendant of itself.
func (s *Service) IsDescendantOf(ctx context.Context, ancestorUUID, descendantUUID string) (bool, error) {
	return s.repo.IsAncestorOf(ctx, ancestorUUID, descendantUUID)
}

func (s *Service) UpdateOrg(ctx context.Context, orgUUID string, input models.UpdateOrgInput) error {
	update := bson.M{}
	if input.Name != nil {
		update["name"] = strings.TrimSpace(*input.Name)
	}
	if input.Slug != nil {
		slug := slugify(*input.Slug)
		if existing, _ := s.repo.GetOrgBySlug(ctx, slug); existing != nil && existing.UUID != orgUUID {
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
	return s.repo.UpdateOrg(ctx, orgUUID, update)
}

func (s *Service) UpdatePlan(ctx context.Context, orgUUID string, input models.UpdatePlanInput) error {
	features := input.Features
	if features == nil {
		features = defaultFeaturesForPlan(input.Plan)
	}
	return s.repo.UpdateOrg(ctx, orgUUID, bson.M{"plan": input.Plan, "features": features})
}

func (s *Service) DeleteOrg(ctx context.Context, orgUUID string) error {
	return s.repo.SoftDeleteOrg(ctx, orgUUID)
}

// OrgAdminView is an org plus its current member count, used by the
// platform-admin list endpoint to avoid an N+1.
type OrgAdminView struct {
	Org         *models.Org
	MemberCount int
}

// ListAllOrgs returns every org in the system with live member counts.
// Used by the platform admin tenant management page — bypasses per-org
// membership gates and is only callable via system.tenants.admin.
func (s *Service) ListAllOrgs(ctx context.Context, includeDeleted bool) ([]OrgAdminView, error) {
	orgs, err := s.repo.ListAllOrgs(ctx, includeDeleted)
	if err != nil {
		return nil, err
	}
	if len(orgs) == 0 {
		return []OrgAdminView{}, nil
	}
	uuids := make([]string, len(orgs))
	for i := range orgs {
		uuids[i] = orgs[i].UUID
	}
	counts, err := s.repo.CountMembersByOrgs(ctx, uuids)
	if err != nil {
		return nil, err
	}
	out := make([]OrgAdminView, len(orgs))
	for i := range orgs {
		o := orgs[i]
		out[i] = OrgAdminView{Org: &o, MemberCount: counts[o.UUID]}
	}
	return out, nil
}

// --- Memberships ---

func (s *Service) ListMembers(ctx context.Context, orgUUID string) ([]models.Membership, error) {
	return s.repo.ListMembershipsByOrg(ctx, orgUUID)
}

func (s *Service) RemoveMember(ctx context.Context, orgUUID, userUUID string) error {
	return s.repo.DeleteMembership(ctx, userUUID, orgUUID)
}

func (s *Service) SetMemberRoles(ctx context.Context, orgUUID, userUUID string, roles []string) error {
	return s.repo.UpdateMembershipRoles(ctx, userUUID, orgUUID, roles)
}

// --- Invites ---

// CreateInvite generates a single-use invite token, persists only its hash,
// and returns the raw token exactly once on the struct's transient Token
// field. Callers must relay the raw token to the invitee (over email or a
// copy-paste UI) immediately; after this function returns there is no way to
// recover it — the database only has the hash.
func (s *Service) CreateInvite(ctx context.Context, orgUUID, invitedBy string, input models.InviteInput) (*models.Invite, error) {
	raw, hash, err := generateInviteToken()
	if err != nil {
		return nil, fmt.Errorf("tenant: generate invite token: %w", err)
	}
	inv := &models.Invite{
		UUID:      uuid.Must(uuid.NewV7()).String(),
		OrgUUID:   orgUUID,
		Email:     strings.ToLower(strings.TrimSpace(input.Email)),
		Roles:     input.Roles,
		Token:     raw, // transient: returned to caller, not persisted
		TokenHash: hash,
		InvitedBy: invitedBy,
		ExpiresAt: time.Now().Add(7 * 24 * time.Hour),
	}
	if err := s.repo.CreateInvite(ctx, inv); err != nil {
		return nil, err
	}
	return inv, nil
}

// ListInvites returns invites for an org. Caller scopes visibility: pending-only
// by default, all invites when onlyPending is false. Raw tokens are zeroed out
// before returning — they are only retrievable once at creation time.
func (s *Service) ListInvites(ctx context.Context, orgUUID string, onlyPending bool) ([]models.Invite, error) {
	invs, err := s.repo.ListInvitesByOrg(ctx, orgUUID, onlyPending)
	if err != nil {
		return nil, err
	}
	for i := range invs {
		invs[i].Token = ""
	}
	return invs, nil
}

// RevokeInvite deletes a pending invite by UUID. The orgUUID is required to
// prevent cross-org spoofing via a guessed invite UUID.
func (s *Service) RevokeInvite(ctx context.Context, orgUUID, inviteUUID string) error {
	return s.repo.DeleteInvite(ctx, orgUUID, inviteUUID)
}

func (s *Service) AcceptInvite(ctx context.Context, userUUID, token string) (*models.Org, error) {
	// Look up by hash, not plaintext — the plaintext only exists in the
	// invitee's email/UI, never in the database.
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
	membership := &models.Membership{
		UUID:      uuid.Must(uuid.NewV7()).String(),
		UserUUID:  userUUID,
		OrgUUID:   inv.OrgUUID,
		Roles:     inv.Roles,
		InvitedBy: inv.InvitedBy,
	}
	if err := s.repo.CreateMembership(ctx, membership); err != nil {
		return nil, err
	}
	if err := s.repo.MarkInviteAccepted(ctx, inv.UUID); err != nil {
		return nil, err
	}
	return s.repo.GetOrgByUUID(ctx, inv.OrgUUID)
}

func (s *Service) GetOrgModel(ctx context.Context, orgUUID string) (*models.Org, error) {
	return s.repo.GetOrgByUUID(ctx, orgUUID)
}

// --- Helpers ---

func defaultFeaturesForPlan(plan string) []string {
	switch plan {
	case models.PlanEnterprise:
		return []string{models.FeatureWildcard}
	case models.PlanPro:
		return []string{"billing", "documents", "company", "sales", "agents"}
	default:
		return []string{"billing", "documents"}
	}
}

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

// generateInviteToken mirrors auth/services/password_auth_service.go's
// generateEmailToken: 32 random bytes → base64url → SHA-256 hex digest.
// The raw token is returned to the caller once; only the hash is stored.
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
