package services

// In-memory fakeRepo for the Phase 12 integration tests. Implements
// the repoBackend interface with simple maps + slices. Production code
// stays bound to *repository.Repository — Go's structural typing means
// either implementation slots into the same Service field.

import (
	"context"
	"errors"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/orkestra/backend/internal/core/authz/models"
	"go.mongodb.org/mongo-driver/bson"
)

type fakeRepo struct {
	mu          sync.Mutex
	permissions map[string]models.Permission
	roles       map[string]models.Role         // keyed by UUID
	bindings    map[string]models.Binding      // keyed by binding UUID
	// Toggleable error injection for failure-path tests.
	createBindingErr error
}

func newFakeRepo() *fakeRepo {
	return &fakeRepo{
		permissions: map[string]models.Permission{},
		roles:       map[string]models.Role{},
		bindings:    map[string]models.Binding{},
	}
}

func (r *fakeRepo) UpsertPermission(_ context.Context, p *models.Permission) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.permissions[p.Key] = *p
	return nil
}

func (r *fakeRepo) ListPermissions(_ context.Context) ([]models.Permission, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]models.Permission, 0, len(r.permissions))
	for _, p := range r.permissions {
		out = append(out, p)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Key < out[j].Key })
	return out, nil
}

func (r *fakeRepo) ListAllPermissionKeys(_ context.Context) ([]string, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]string, 0, len(r.permissions))
	for k := range r.permissions {
		out = append(out, k)
	}
	sort.Strings(out)
	return out, nil
}

func (r *fakeRepo) UpsertRole(_ context.Context, role *models.Role) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if role.UUID == "" {
		return errors.New("fakeRepo.UpsertRole: UUID required")
	}
	// Mirror repository semantics: if a role with the same (tenantId, name)
	// exists, preserve its UUID. Caller seeds with the existing UUID.
	r.roles[role.UUID] = *role
	return nil
}

func (r *fakeRepo) UpdateRoleFields(_ context.Context, uuid string, fields bson.M) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	role, ok := r.roles[uuid]
	if !ok {
		return errors.New("fakeRepo: role not found")
	}
	for k, v := range fields {
		switch k {
		case "isActive":
			if b, ok := v.(bool); ok {
				role.IsActive = b
			}
		case "name":
			if s, ok := v.(string); ok {
				role.Name = s
			}
		case "description":
			if s, ok := v.(string); ok {
				role.Description = s
			}
		case "permissions":
			if ps, ok := v.([]string); ok {
				role.Permissions = append(role.Permissions[:0], ps...)
			}
		}
	}
	r.roles[uuid] = role
	return nil
}

func (r *fakeRepo) GetRoleByName(_ context.Context, tenantID, name string) (*models.Role, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, role := range r.roles {
		if role.TenantID == tenantID && role.Name == name {
			out := role
			return &out, nil
		}
	}
	return nil, errors.New("fakeRepo: role not found by name")
}

func (r *fakeRepo) GetRoleByUUID(_ context.Context, uuid string) (*models.Role, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	role, ok := r.roles[uuid]
	if !ok {
		return nil, errors.New("fakeRepo: role not found by uuid")
	}
	out := role
	return &out, nil
}

func (r *fakeRepo) CountSystemRoles(_ context.Context) (int64, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	var n int64
	for _, role := range r.roles {
		if role.IsSystem {
			n++
		}
	}
	return n, nil
}

func (r *fakeRepo) ListRoles(_ context.Context, tenantID string) ([]models.Role, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]models.Role, 0)
	for _, role := range r.roles {
		// System roles are global — visible to every tenant.
		if role.IsSystem || role.TenantID == tenantID {
			out = append(out, role)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

func (r *fakeRepo) DeleteRole(_ context.Context, uuid string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.roles[uuid]; !ok {
		return errors.New("fakeRepo: role not found")
	}
	// Mirror the repo's filter — refuse on system roles.
	if r.roles[uuid].IsSystem {
		return errors.New("fakeRepo: cannot delete system role")
	}
	delete(r.roles, uuid)
	return nil
}

func (r *fakeRepo) CreateBinding(_ context.Context, b *models.Binding) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.createBindingErr != nil {
		return r.createBindingErr
	}
	if b.UUID == "" {
		return errors.New("fakeRepo.CreateBinding: UUID required")
	}
	r.bindings[b.UUID] = *b
	return nil
}

func (r *fakeRepo) DeleteBinding(_ context.Context, uuid string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.bindings, uuid)
	return nil
}

func (r *fakeRepo) DeleteBindingsByRoleUUID(_ context.Context, roleUUID string) (int64, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	var n int64
	for k, b := range r.bindings {
		if b.RoleUUID == roleUUID {
			delete(r.bindings, k)
			n++
		}
	}
	return n, nil
}

func (r *fakeRepo) DeleteBindingsByTenant(_ context.Context, tenantUUID string) (int64, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	var n int64
	for k, b := range r.bindings {
		if b.TenantID == tenantUUID {
			delete(r.bindings, k)
			n++
		}
	}
	return n, nil
}

func (r *fakeRepo) ListActiveBindingsForUser(_ context.Context, userUUID, tenantID string) ([]models.Binding, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]models.Binding, 0)
	now := time.Now()
	for _, b := range r.bindings {
		if b.UserUUID != userUUID {
			continue
		}
		if b.TenantID != tenantID {
			continue
		}
		if b.ExpiresAt != nil && b.ExpiresAt.Before(now) {
			continue
		}
		out = append(out, b)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].UUID < out[j].UUID })
	return out, nil
}

func (r *fakeRepo) ListBindingsByTenant(_ context.Context, tenantID string) ([]models.Binding, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]models.Binding, 0)
	for _, b := range r.bindings {
		if b.TenantID != tenantID {
			continue
		}
		out = append(out, b)
	}
	return out, nil
}

// seedRole is a test helper that drops a role into the fake.
func (r *fakeRepo) seedRole(uuid, name string, isSystem bool, perms []string, tenantID string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.roles[uuid] = models.Role{
		UUID:        uuid,
		Name:        name,
		IsSystem:    isSystem,
		IsActive:    true,
		Permissions: append([]string(nil), perms...),
		TenantID:    tenantID,
	}
}

// seedBinding is a test helper that drops a binding into the fake.
func (r *fakeRepo) seedBinding(uuid, userUUID, tenantID, roleUUID string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.bindings[uuid] = models.Binding{
		UUID:     uuid,
		UserUUID: userUUID,
		TenantID: tenantID,
		RoleUUID: roleUUID,
	}
}

// hasRoleNamed returns true when any role currently in the fake has the
// given name. Cheap helper for assertions on SeedSystemRoles.
func (r *fakeRepo) hasRoleNamed(name string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, role := range r.roles {
		if role.Name == name {
			return true
		}
	}
	return false
}

// roleByName returns a copy of the named role, or nil. Convenience for
// assertions that need to inspect role state.
func (r *fakeRepo) roleByName(name string) *models.Role {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, role := range r.roles {
		if role.Name == name {
			out := role
			return &out
		}
	}
	return nil
}

// Compile-time guard that the fake satisfies the backend interface —
// any drift in repoBackend immediately breaks the build of this file
// rather than failing at test runtime with a confusing error.
var _ repoBackend = (*fakeRepo)(nil)

// permKeysContain is a tiny membership helper for permission slice
// assertions. Used by Phase 12 tests.
func permKeysContain(perms []string, want string) bool {
	for _, p := range perms {
		if p == want || (want != "*" && p == "*") {
			return true
		}
		if strings.EqualFold(p, want) {
			return true
		}
	}
	return false
}
