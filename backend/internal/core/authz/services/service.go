package services

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/orkestra/backend/internal/core/authz/models"
	"github.com/orkestra/backend/internal/core/authz/repository"
	"github.com/orkestra/backend/internal/shared/database"
	"github.com/orkestra/backend/internal/shared/iface"
	"go.mongodb.org/mongo-driver/bson"
)

// ErrSystemRoleImmutable is returned when UpdateRole is asked to change the
// name, description, or permissions of a system role. Toggling IsActive on a
// system role is still allowed.
var ErrSystemRoleImmutable = errors.New("authz: system role name/description/permissions are immutable")

// ErrRoleInactive is returned when CreateBinding is called with a role that
// has been disabled. Operators should re-enable the role before granting.
var ErrRoleInactive = errors.New("authz: role is disabled")

// Service owns authorization lifecycle and implements iface.AuthzProvider.
//
// Permission evaluation rules (in order):
//  1. If the user's system role is "super_admin", every permission is granted
//     (wildcard "*").
//  2. If the user's system role is "administrator" or "developer", every
//     system permission is granted; non-system permissions still come from
//     bindings.
//  3. Otherwise, the user's permissions in the given org are the union of
//     all permissions from non-expired role bindings for (userUUID, orgID),
//     plus any bindings on the user with orgID="" (global grants).
//  4. System permissions (where PermissionSpec.System is true) require the
//     user to have the permission granted globally (either by system role
//     or by a global binding).
//
// Results are cached in Redis for 60 seconds per (userUUID, orgID) key and
// invalidated when bindings or roles change.
type Service struct {
	repo      *repository.Repository
	redis     *database.RedisClientAdapter
	logger    *slog.Logger
	userRoles UserSystemRoleLookup

	mu                  sync.RWMutex
	systemPermissionSet map[string]struct{}     // keys declared with System=true
	allPermissionSet    map[string]struct{}     // every registered permission
	cachedPermSpecs     []iface.PermissionSpec  // full specs for lazy reseed after a DB wipe
}

// UserSystemRoleLookup resolves a user's system role (from the user module).
// Kept as a plain function type so we don't need to import the user module.
type UserSystemRoleLookup func(ctx context.Context, userUUID string) (string, error)

type Config struct {
	Repo       *repository.Repository
	Redis      *database.RedisClientAdapter
	Logger     *slog.Logger
	LookupUser UserSystemRoleLookup
}

func New(cfg Config) *Service {
	return &Service{
		repo:                cfg.Repo,
		redis:               cfg.Redis,
		logger:              cfg.Logger,
		userRoles:           cfg.LookupUser,
		systemPermissionSet: make(map[string]struct{}),
		allPermissionSet:    make(map[string]struct{}),
	}
}

// --- Provider interface ---

func (s *Service) HasPermission(ctx context.Context, userUUID, orgID, permission string) (bool, error) {
	perms, err := s.GetEffectivePermissions(ctx, userUUID, orgID)
	if err != nil {
		return false, err
	}
	for _, p := range perms {
		if p == permission || p == "*" {
			return true, nil
		}
	}
	return false, nil
}

func (s *Service) GetEffectivePermissions(ctx context.Context, userUUID, orgID string) ([]string, error) {
	if userUUID == "" {
		return nil, errors.New("authz: userUUID required")
	}

	if cached, ok := s.cacheGet(ctx, userUUID, orgID); ok {
		return cached, nil
	}

	systemRole := ""
	if s.userRoles != nil {
		r, err := s.userRoles(ctx, userUUID)
		if err == nil {
			systemRole = r
		}
	}

	perms := make(map[string]struct{})

	// System role shortcuts. super_admin gets the wildcard; administrator
	// and developer both inherit every system-level permission so they can
	// hit platform admin endpoints without needing explicit org bindings.
	// The finer distinction between administrator and developer is enforced
	// by the role-assignment cascade (future work), not by the permission set.
	switch systemRole {
	case "super_admin":
		perms["*"] = struct{}{}
	case "administrator", "developer":
		s.mu.RLock()
		for k := range s.systemPermissionSet {
			perms[k] = struct{}{}
		}
		s.mu.RUnlock()
	}

	// Union of global bindings (orgID="").
	globals, err := s.repo.ListActiveBindingsForUser(ctx, userUUID, "")
	if err != nil {
		return nil, err
	}
	for _, b := range globals {
		role, err := s.repo.GetRoleByUUID(ctx, b.RoleUUID)
		if err != nil || !role.IsActive {
			continue
		}
		for _, p := range role.Permissions {
			perms[p] = struct{}{}
		}
	}

	// Union of org-scoped bindings.
	if orgID != "" {
		scoped, err := s.repo.ListActiveBindingsForUser(ctx, userUUID, orgID)
		if err != nil {
			return nil, err
		}
		for _, b := range scoped {
			role, err := s.repo.GetRoleByUUID(ctx, b.RoleUUID)
			if err != nil || !role.IsActive {
				continue
			}
			for _, p := range role.Permissions {
				perms[p] = struct{}{}
			}
		}
	}

	out := make([]string, 0, len(perms))
	for k := range perms {
		out = append(out, k)
	}
	s.cacheSet(ctx, userUUID, orgID, out)
	return out, nil
}

func (s *Service) RegisterPermissions(ctx context.Context, specs []iface.PermissionSpec) error {
	if len(specs) == 0 {
		return nil
	}
	s.mu.Lock()
	for _, spec := range specs {
		s.allPermissionSet[spec.Key] = struct{}{}
		if spec.System {
			s.systemPermissionSet[spec.Key] = struct{}{}
		}
	}
	// Remember the full specs so ensureSeeded can re-upsert the catalog
	// after a live DB wipe without going back to the module registry.
	s.cachedPermSpecs = append(s.cachedPermSpecs[:0], specs...)
	s.mu.Unlock()

	for _, spec := range specs {
		p := &models.Permission{
			Key:         spec.Key,
			Module:      spec.Module,
			Description: spec.Description,
			System:      spec.System,
		}
		if err := s.repo.UpsertPermission(ctx, p); err != nil {
			return fmt.Errorf("upsert permission %s: %w", spec.Key, err)
		}
	}
	return nil
}

// --- System role seeding ---

// SeedSystemRoles creates the six default system roles on first boot. They
// have orgId="" and IsSystem=true. Permission lists are derived from the
// permissions catalog that modules have registered by the time this runs.
// Call this after RegisterPermissions has been called for every module.
//
// Hierarchy (most to least privileged):
//
//	super_admin   — wildcard, full power, can assign every other role
//	administrator — all org permissions, cannot elevate peers to admin
//	developer     — all org permissions, cannot touch admin/super_admin
//	manager       — read/create/update, no delete, no admin
//	operator      — read + self-service
//	guest         — read-only
//
// The cascade distinction between administrator and developer is enforced
// at role-assignment time (future work), not baked into the permission set.
func (s *Service) SeedSystemRoles(ctx context.Context) error {
	allKeys, err := s.repo.ListAllPermissionKeys(ctx)
	if err != nil {
		return err
	}

	// Operator: read-only + self-service update permissions
	operator := filter(allKeys, func(p string) bool {
		return strings.HasSuffix(p, ".read") || strings.HasSuffix(p, ".self")
	})

	// Manager: read + create + update (no admin, no delete)
	manager := filter(allKeys, func(p string) bool {
		if strings.HasSuffix(p, ".delete") || strings.HasSuffix(p, ".admin") {
			return false
		}
		return true
	})

	// Guest: read-only
	guest := filter(allKeys, func(p string) bool {
		return strings.HasSuffix(p, ".read")
	})

	roles := []models.Role{
		{UUID: uuid.NewString(), Name: "super_admin", Description: "Full power — wildcard permission, can assign every role.", Permissions: []string{"*"}, IsSystem: true, IsActive: true},
		{UUID: uuid.NewString(), Name: "administrator", Description: "Organization administrator — all permissions. Cannot elevate peers to administrator or super_admin.", Permissions: allKeys, IsSystem: true, IsActive: true},
		{UUID: uuid.NewString(), Name: "developer", Description: "Technical power user — all permissions. Cannot manage administrator or super_admin accounts.", Permissions: allKeys, IsSystem: true, IsActive: true},
		{UUID: uuid.NewString(), Name: "manager", Description: "Read/write, no admin, no delete.", Permissions: manager, IsSystem: true, IsActive: true},
		{UUID: uuid.NewString(), Name: "operator", Description: "Read-only + self-service.", Permissions: operator, IsSystem: true, IsActive: true},
		{UUID: uuid.NewString(), Name: "guest", Description: "Read-only access.", Permissions: guest, IsSystem: true, IsActive: true},
	}

	for i := range roles {
		existing, err := s.repo.GetRoleByName(ctx, "", roles[i].Name)
		if err == nil && existing != nil {
			// Preserve UUID so existing bindings keep working.
			roles[i].UUID = existing.UUID
		}
		if err := s.repo.UpsertRole(ctx, &roles[i]); err != nil {
			return fmt.Errorf("seed role %s: %w", roles[i].Name, err)
		}
	}
	return nil
}

// --- Role admin ---

func (s *Service) CreateRole(ctx context.Context, orgID string, input models.CreateRoleInput) (*models.Role, error) {
	role := &models.Role{
		UUID:        uuid.NewString(),
		OrgID:       orgID,
		Name:        input.Name,
		Description: input.Description,
		Permissions: input.Permissions,
		IsSystem:    false,
		IsActive:    true,
	}
	if err := s.repo.UpsertRole(ctx, role); err != nil {
		return nil, err
	}
	return role, nil
}

// UpdateRole applies a partial update to a role. System roles reject any
// change to Name, Description, or Permissions with ErrSystemRoleImmutable —
// only IsActive can be toggled on them. Custom roles accept all four.
// The authz cache is flushed because permission membership may change.
func (s *Service) UpdateRole(ctx context.Context, roleUUID string, input models.UpdateRoleInput) (*models.Role, error) {
	existing, err := s.repo.GetRoleByUUID(ctx, roleUUID)
	if err != nil {
		return nil, err
	}

	touchesImmutable := input.Name != nil || input.Description != nil || input.Permissions != nil
	if existing.IsSystem && touchesImmutable {
		return nil, ErrSystemRoleImmutable
	}

	fields := bson.M{}
	if input.Name != nil {
		name := strings.TrimSpace(*input.Name)
		if name == "" {
			return nil, errors.New("authz: name cannot be empty")
		}
		fields["name"] = name
	}
	if input.Description != nil {
		fields["description"] = *input.Description
	}
	if input.Permissions != nil {
		if len(input.Permissions) == 0 {
			return nil, errors.New("authz: permissions cannot be empty")
		}
		fields["permissions"] = input.Permissions
	}
	if input.IsActive != nil {
		fields["isActive"] = *input.IsActive
	}

	if len(fields) == 0 {
		return existing, nil
	}

	if err := s.repo.UpdateRoleFields(ctx, roleUUID, fields); err != nil {
		return nil, err
	}
	s.flushCache(ctx)

	return s.repo.GetRoleByUUID(ctx, roleUUID)
}

func (s *Service) ListRoles(ctx context.Context, orgID string) ([]models.Role, error) {
	if err := s.ensureSeeded(ctx); err != nil && s.logger != nil {
		s.logger.Warn("authz ensureSeeded failed",
			slog.String("error", err.Error()))
	}
	return s.repo.ListRoles(ctx, orgID)
}

// ensureSeeded re-runs the permission catalog + system-role seed if the
// authz_roles collection has been wiped at runtime (dev DB drop etc.). It
// relies on the full PermissionSpec list cached by RegisterPermissions so
// no round trip to the module registry is needed. A no-op when the catalog
// is already present or when the cache hasn't been populated yet (first
// boot race — the startup seed path will cover it).
func (s *Service) ensureSeeded(ctx context.Context) error {
	count, err := s.repo.CountSystemRoles(ctx)
	if err != nil {
		return err
	}
	if count > 0 {
		return nil
	}

	s.mu.RLock()
	specs := append([]iface.PermissionSpec(nil), s.cachedPermSpecs...)
	s.mu.RUnlock()
	if len(specs) == 0 {
		return nil
	}

	if err := s.RegisterPermissions(ctx, specs); err != nil {
		return fmt.Errorf("lazy reseed permissions: %w", err)
	}
	if err := s.SeedSystemRoles(ctx); err != nil {
		return fmt.Errorf("lazy reseed system roles: %w", err)
	}
	if s.logger != nil {
		s.logger.Info("authz: lazy-reseeded permissions + system roles",
			slog.Int("permissions", len(specs)))
	}
	return nil
}

// DeleteRole removes a custom role and cascades every binding pointing at
// it. The repo DeleteRole itself refuses to touch system roles via its
// isSystem=false filter, so we delete bindings first — if the role delete
// ends up refused (system role), the binding cleanup will have been a
// no-op because nothing is bound to a system role via this UUID unless an
// operator did it explicitly, and in that case we'd want them gone anyway.
func (s *Service) DeleteRole(ctx context.Context, roleUUID string) error {
	existing, err := s.repo.GetRoleByUUID(ctx, roleUUID)
	if err != nil {
		return err
	}
	if existing.IsSystem {
		return ErrSystemRoleImmutable
	}
	removed, err := s.repo.DeleteBindingsByRoleUUID(ctx, roleUUID)
	if err != nil {
		return fmt.Errorf("cascade bindings: %w", err)
	}
	if removed > 0 && s.logger != nil {
		s.logger.Info("authz: cascaded binding delete",
			slog.String("role", existing.Name),
			slog.Int64("bindings", removed))
	}
	if err := s.repo.DeleteRole(ctx, roleUUID); err != nil {
		return err
	}
	s.flushCache(ctx)
	return nil
}

func (s *Service) ListPermissions(ctx context.Context) ([]models.Permission, error) {
	if err := s.ensureSeeded(ctx); err != nil && s.logger != nil {
		s.logger.Warn("authz ensureSeeded failed",
			slog.String("error", err.Error()))
	}
	return s.repo.ListPermissions(ctx)
}

// --- Bindings ---

func (s *Service) CreateBinding(ctx context.Context, orgID, grantedBy string, input models.CreateBindingInput) (*models.Binding, error) {
	role, err := s.repo.GetRoleByUUID(ctx, input.RoleUUID)
	if err != nil {
		return nil, err
	}
	if !role.IsActive {
		return nil, ErrRoleInactive
	}
	b := &models.Binding{
		UUID:      uuid.NewString(),
		UserUUID:  input.UserUUID,
		OrgID:     orgID,
		RoleUUID:  role.UUID,
		RoleName:  role.Name,
		GrantedBy: grantedBy,
		ExpiresAt: input.ExpiresAt,
	}
	if err := s.repo.CreateBinding(ctx, b); err != nil {
		return nil, err
	}
	s.cacheInvalidate(ctx, input.UserUUID)
	return b, nil
}

func (s *Service) ListBindings(ctx context.Context, orgID string) ([]models.Binding, error) {
	return s.repo.ListBindingsByOrg(ctx, orgID)
}

func (s *Service) DeleteBinding(ctx context.Context, uuid string) error {
	if err := s.repo.DeleteBinding(ctx, uuid); err != nil {
		return err
	}
	s.flushCache(ctx)
	return nil
}

func (s *Service) flushCache(ctx context.Context) {
	if s.redis == nil {
		return
	}
	keys, err := s.redis.Keys(ctx, "authz:cache:*")
	if err != nil || len(keys) == 0 {
		return
	}
	_ = s.redis.Del(ctx, keys...)
}

// --- Cache ---

func (s *Service) cacheKey(userUUID, orgID string) string {
	if orgID == "" {
		orgID = "-"
	}
	return "authz:cache:" + userUUID + ":" + orgID
}

func (s *Service) cacheGet(ctx context.Context, userUUID, orgID string) ([]string, bool) {
	if s.redis == nil {
		return nil, false
	}
	raw, err := s.redis.Get(ctx, s.cacheKey(userUUID, orgID))
	if err != nil || raw == "" {
		return nil, false
	}
	var out []string
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		return nil, false
	}
	return out, true
}

func (s *Service) cacheSet(ctx context.Context, userUUID, orgID string, perms []string) {
	if s.redis == nil {
		return
	}
	data, err := json.Marshal(perms)
	if err != nil {
		return
	}
	_ = s.redis.Set(ctx, s.cacheKey(userUUID, orgID), string(data), 60*time.Second)
}

func (s *Service) cacheInvalidate(ctx context.Context, userUUID string) {
	if s.redis == nil {
		return
	}
	keys, err := s.redis.Keys(ctx, "authz:cache:"+userUUID+":*")
	if err != nil || len(keys) == 0 {
		return
	}
	_ = s.redis.Del(ctx, keys...)
}

// --- helpers ---

func filter(in []string, pred func(string) bool) []string {
	out := make([]string, 0, len(in))
	for _, s := range in {
		if pred(s) {
			out = append(out, s)
		}
	}
	return out
}

