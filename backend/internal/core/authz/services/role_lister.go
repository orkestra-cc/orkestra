package services

import (
	"context"
	"errors"
	"sort"

	"github.com/orkestra/backend/internal/core/authz/repository"
)

// ErrRoleListTenantRequired is returned by ActiveRoleIDs for an empty tenant.
var ErrRoleListTenantRequired = errors.New("authz: listing a user's roles requires a tenant")

// ActiveRoleIDs implements iface.AuthzRoleLister. It returns the sorted,
// deduplicated UUIDs of active roles reachable through the user's
// non-expired bindings in tenantID. A role is kept when it is a system role
// (tenantId "") or belongs to tenantID. A dangling binding is skipped; any
// other repository error fails the call, because silently dropping a role
// principal would hide records without a flag. Uncached.
func (s *Service) ActiveRoleIDs(ctx context.Context, userUUID, tenantID string) ([]string, error) {
	if tenantID == "" {
		return nil, ErrRoleListTenantRequired
	}
	if userUUID == "" {
		return nil, errors.New("authz: userUUID required")
	}
	bindings, err := s.repo.ListActiveBindingsForUser(ctx, userUUID, tenantID)
	if err != nil {
		return nil, err
	}
	seen := make(map[string]struct{}, len(bindings))
	out := make([]string, 0, len(bindings))
	for _, b := range bindings {
		if _, dup := seen[b.RoleUUID]; dup {
			continue
		}
		role, err := s.repo.GetRoleByUUID(ctx, b.RoleUUID)
		if err != nil {
			if errors.Is(err, repository.ErrNotFound) {
				continue
			}
			return nil, err
		}
		if !role.IsActive || (role.TenantID != "" && role.TenantID != tenantID) {
			continue
		}
		seen[b.RoleUUID] = struct{}{}
		out = append(out, b.RoleUUID)
	}
	sort.Strings(out)
	return out, nil
}

// boundRoleNames returns the sorted, deduplicated names of the active roles
// bound to userUUID in tenantID (system roles and roles of that tenant), the
// tenant_roles a Cedar principal carries when the check is about a user other
// than the caller. Same filters and error policy as ActiveRoleIDs.
func (s *Service) boundRoleNames(ctx context.Context, userUUID, tenantID string) ([]string, error) {
	bindings, err := s.repo.ListActiveBindingsForUser(ctx, userUUID, tenantID)
	if err != nil {
		return nil, err
	}
	seen := make(map[string]struct{}, len(bindings))
	out := make([]string, 0, len(bindings))
	for _, b := range bindings {
		role, err := s.repo.GetRoleByUUID(ctx, b.RoleUUID)
		if err != nil {
			if errors.Is(err, repository.ErrNotFound) {
				continue
			}
			return nil, err
		}
		if !role.IsActive || (role.TenantID != "" && role.TenantID != tenantID) {
			continue
		}
		if _, dup := seen[role.Name]; dup {
			continue
		}
		seen[role.Name] = struct{}{}
		out = append(out, role.Name)
	}
	sort.Strings(out)
	return out, nil
}
