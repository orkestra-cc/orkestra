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

// ErrInvalidMembership wraps caller-error responses on Membership
// writes.
var ErrInvalidMembership = errors.New("marketing: invalid membership")

// MembershipService orchestrates Membership writes, enforcing the two
// invariants the SDK index layer cannot express today (no
// PartialFilterExpression support yet, tracked Phase-2+):
//
//  1. At most one Active=true membership per (personUUID, orgUUID)
//     pair. When a duplicate active pair would be created, the prior
//     row is closed (Until=now, Active=false) rather than collide.
//  2. At most one Active=true membership with Primary=true per
//     Person. When a new membership claims primary, every other
//     active membership of that person is demoted.
type MembershipService struct {
	repo *repository.MembershipRepository
}

// NewMembershipService wires the service to its repository.
func NewMembershipService(repo *repository.MembershipRepository) *MembershipService {
	return &MembershipService{repo: repo}
}

// Create persists a new membership, applying the active-pair and
// active-primary invariants. Returns the created membership.
func (s *MembershipService) Create(ctx context.Context, m *models.Membership) (*models.Membership, error) {
	if m == nil {
		return nil, fmt.Errorf("%w: nil membership", ErrInvalidMembership)
	}
	if m.PersonUUID == "" || m.OrgUUID == "" {
		return nil, fmt.Errorf("%w: personUuid and orgUuid are required", ErrInvalidMembership)
	}
	if m.UUID == "" {
		m.UUID = uuid.New().String()
	}
	// Active defaults true on a fresh membership unless the caller
	// explicitly passed Until.
	if m.Until == nil {
		m.Active = true
	}

	// Enforce invariant 1: close any prior active pair before creating
	// the new row.
	prior, err := s.repo.FindActivePair(ctx, m.PersonUUID, m.OrgUUID)
	if err != nil && !errors.Is(err, repository.ErrMembershipNotFound) {
		return nil, err
	}
	if prior != nil {
		if err := s.repo.Close(ctx, prior.UUID); err != nil {
			return nil, err
		}
	}

	// Enforce invariant 2: when this membership claims primary, demote
	// every other active membership the person holds.
	if m.Primary && m.Active {
		if err := s.repo.UnsetPrimaryForPerson(ctx, m.PersonUUID); err != nil {
			return nil, err
		}
	}

	if actor, ok := ctxauth.GetUserUUID(ctx); ok {
		m.CreatedBy = actor
	}
	if err := s.repo.Create(ctx, m); err != nil {
		return nil, err
	}
	return s.repo.GetByUUID(ctx, m.UUID)
}

// Get returns the membership by UUID.
func (s *MembershipService) Get(ctx context.Context, uuid string) (*models.Membership, error) {
	return s.repo.GetByUUID(ctx, uuid)
}

// ListByPerson returns every membership of a Person (active +
// closed).
func (s *MembershipService) ListByPerson(ctx context.Context, personUUID string) ([]models.Membership, error) {
	return s.repo.ListByPerson(ctx, personUUID)
}

// ListByOrg returns every membership of an Organization.
func (s *MembershipService) ListByOrg(ctx context.Context, orgUUID string) ([]models.Membership, error) {
	return s.repo.ListByOrg(ctx, orgUUID)
}

// Update applies a patch and re-applies invariants when relevant
// fields are touched. Setting Primary=true triggers the demote pass.
// Setting Active=false routes through Close so the closure metadata
// (Until=now) is stamped consistently.
func (s *MembershipService) Update(ctx context.Context, uuid string, patch map[string]any) (*models.Membership, error) {
	existing, err := s.repo.GetByUUID(ctx, uuid)
	if err != nil {
		return nil, err
	}

	// Routing: explicit Close goes through Close() so Until is stamped
	// in one place.
	if active, ok := patch["active"].(bool); ok && !active && existing.Active {
		if err := s.repo.Close(ctx, uuid); err != nil {
			return nil, err
		}
		delete(patch, "active")
		delete(patch, "until")
	}

	if prim, ok := patch["primary"].(bool); ok && prim {
		if err := s.repo.UnsetPrimaryForPerson(ctx, existing.PersonUUID); err != nil {
			return nil, err
		}
	}

	if len(patch) > 0 {
		if err := s.repo.Update(ctx, uuid, patch); err != nil {
			return nil, err
		}
	}
	return s.repo.GetByUUID(ctx, uuid)
}

// Delete hard-removes the membership. Use Update with Active=false
// to keep historical state queryable; use Delete only when the row
// is genuinely garbage (e.g. importer cleanup, user-initiated
// removal of an erroneous link).
func (s *MembershipService) Delete(ctx context.Context, uuid string) error {
	return s.repo.Delete(ctx, uuid)
}
