package services

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/orkestra-cc/orkestra-addon-marketing/models"
	"github.com/orkestra-cc/orkestra-addon-marketing/repository"
	"github.com/orkestra-cc/orkestra-sdk/ctxauth"
)

// ErrInvalidOrganization wraps caller-error responses on Organization
// writes — empty legal name, malformed payload, etc. Distinct from
// validation errors on the custom-fields bag so the handler can map
// each to the right HTTP code.
var ErrInvalidOrganization = errors.New("marketing: invalid organization")

// OrganizationService orchestrates the write path for organizations:
// generates the external UUID, runs custom-field validation against
// the tenant schema, hands off to the repository for persistence, and
// cascades to memberships on delete.
type OrganizationService struct {
	repo       *repository.OrganizationRepository
	customFlds *CustomFieldService
	mships     *repository.MembershipRepository
}

// NewOrganizationService wires the service with its collaborators.
// The membership repo is consumed only on delete (cascade); reads do
// not touch it.
func NewOrganizationService(
	repo *repository.OrganizationRepository,
	cf *CustomFieldService,
	mships *repository.MembershipRepository,
) *OrganizationService {
	return &OrganizationService{repo: repo, customFlds: cf, mships: mships}
}

// Create persists a new Organization. The caller may pass a zero
// UUID — the service mints a fresh one. Custom-field validation runs
// before insert; the bag is rejected wholesale on any per-field
// failure rather than partially merged.
func (s *OrganizationService) Create(ctx context.Context, org *models.Organization) (*models.Organization, error) {
	if org == nil {
		return nil, fmt.Errorf("%w: nil organization", ErrInvalidOrganization)
	}
	if org.LegalName == "" {
		return nil, fmt.Errorf("%w: legalName is required", ErrInvalidOrganization)
	}
	if org.Kind == "" {
		org.Kind = models.OrgKindCompany
	}
	if org.UUID == "" {
		org.UUID = uuid.New().String()
	}
	if err := s.customFlds.Validate(ctx, models.CustomFieldTargetOrganizations, org.CustomFields); err != nil {
		return nil, err
	}
	if actor, ok := ctxauth.GetUserUUID(ctx); ok {
		org.CreatedBy = actor
		org.UpdatedBy = actor
	}
	if err := s.repo.Create(ctx, org); err != nil {
		return nil, err
	}
	return s.repo.GetByUUID(ctx, org.UUID)
}

// Get returns the organization by UUID inside the caller's tenant.
// Repository ErrOrgNotFound is propagated; the handler maps it to a
// 404.
func (s *OrganizationService) Get(ctx context.Context, uuid string) (*models.Organization, error) {
	return s.repo.GetByUUID(ctx, uuid)
}

// List delegates to the repository — no additional filtering at the
// service layer for now.
func (s *OrganizationService) List(ctx context.Context, f repository.ListFilter) ([]models.Organization, error) {
	return s.repo.List(ctx, f)
}

// Update applies the patch, re-validating custom_fields when present.
// The patch is a generic map so partial updates work without
// rewriting unchanged fields.
func (s *OrganizationService) Update(ctx context.Context, uuid string, patch map[string]any) (*models.Organization, error) {
	if cf, ok := patch["customFields"].(map[string]any); ok {
		if err := s.customFlds.Validate(ctx, models.CustomFieldTargetOrganizations, cf); err != nil {
			return nil, err
		}
	}
	if actor, ok := ctxauth.GetUserUUID(ctx); ok {
		patch["updatedBy"] = actor
	}
	if err := s.repo.Update(ctx, uuid, patch); err != nil {
		return nil, err
	}
	return s.repo.GetByUUID(ctx, uuid)
}

// Delete hard-removes the organization and cascades to every
// membership pointing at it. Activity (Phase 2+) is intentionally
// not cascaded — historical engagement records stay queryable with
// the orgUuid orphan.
func (s *OrganizationService) Delete(ctx context.Context, uuid string) error {
	if err := s.mships.DeleteForOrg(ctx, uuid); err != nil {
		return err
	}
	return s.repo.Delete(ctx, uuid)
}
