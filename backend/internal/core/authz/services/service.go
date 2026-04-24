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
	"github.com/orkestra/backend/internal/core/authz/cedar"
	"github.com/orkestra/backend/internal/core/authz/models"
	"github.com/orkestra/backend/internal/core/authz/repository"
	"github.com/orkestra/backend/internal/shared/database"
	"github.com/orkestra/backend/internal/shared/iface"
	"github.com/orkestra/backend/internal/shared/metrics"
	"github.com/orkestra/backend/internal/shared/middleware"
	"go.mongodb.org/mongo-driver/bson"
)

// ErrSystemRoleImmutable is returned when UpdateRole is asked to change the
// name, description, or permissions of a system role. Toggling IsActive on a
// system role is still allowed.
var ErrSystemRoleImmutable = errors.New("authz: system role name/description/permissions are immutable")

// ErrRoleInactive is returned when CreateBinding is called with a role that
// has been disabled. Operators should re-enable the role before granting.
var ErrRoleInactive = errors.New("authz: role is disabled")

// ErrSystemRoleNotGrantableInTenant is returned when CreateBinding is asked
// to grant a platform-level system role (super_admin, administrator,
// developer, manager, operator, guest) with a non-empty tenantID. The system
// vs tenant tier separation requires global bindings (tenantID="") for
// system roles. Section B item #3 commit C of the auth roadmap, 2026-04-24.
var ErrSystemRoleNotGrantableInTenant = errors.New("authz: system roles must be granted via global bindings (tenantID=\"\"), not tenant-scoped bindings")

// ErrTenantRoleNotGrantableGlobally is returned when CreateBinding is asked
// to grant an org_* role (or any custom role) with an empty tenantID. The
// inverse of the rule above: tenant-scope roles must always carry a
// concrete tenant in their binding.
var ErrTenantRoleNotGrantableGlobally = errors.New("authz: tenant-scope roles must be granted via tenant-scoped bindings, not globally")

// ErrInsufficientPermissionsToGrant is returned when the cascade rule
// rejects a binding: the granter's effective permission set is not a
// superset of the role's permission set, so the grant would let the
// recipient do things the granter themselves cannot. Bypass: the literal
// granter "system" (used by the OwnerRoleBinder hook for platform-issued
// auto-grants) skips this check.
var ErrInsufficientPermissionsToGrant = errors.New("authz: caller cannot grant a role with permissions they do not themselves hold")

// ErrGranterRequired is returned when CreateBinding is called without a
// granter UUID and without the literal "system" sentinel. Without a known
// granter the cascade rule cannot be evaluated; refuse rather than
// silently waive the check.
var ErrGranterRequired = errors.New("authz: granter is required")

// granterSystem is the sentinel value handlers pass when a binding is
// platform-issued rather than user-initiated (e.g. the OwnerRoleBinder
// hook in tenant.CreateTenant). System grants bypass the cascade check
// because the platform is the trust root; the system/tenant separation
// rule still applies.
const granterSystem = "system"

// platformSystemRoleNames is the set of role names that may only ever be
// granted via global bindings (binding.tenantID == ""). Mirrors the slice
// in SeedSystemRoles. Adding a new platform role requires updating both
// this set and the seed list.
var platformSystemRoleNames = map[string]struct{}{
	"super_admin":   {},
	"administrator": {},
	"developer":     {},
	"manager":       {},
	"operator":      {},
	"guest":         {},
}

// isPlatformSystemRole reports whether the role is one of the 6 platform
// system roles. Uses the role name rather than IsSystem because org_*
// roles also carry IsSystem=true (they're seeded as built-in catalog rows
// even though they're tenant-scoped in semantics).
func isPlatformSystemRole(role *models.Role) bool {
	if role == nil {
		return false
	}
	_, ok := platformSystemRoleNames[role.Name]
	return ok
}

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
	repo               *repository.Repository
	redis              *database.RedisClientAdapter
	logger             *slog.Logger
	userRoles          UserSystemRoleLookup
	startMFAGrace      MFAGraceStarter
	lookupCaps         TenantCapabilityLookup
	lookupTenantStatus TenantStatusLookup
	production         bool // when true, developer role is restricted to read-only

	// cedarEngine is the Cedar evaluator. nil when Cedar is disabled
	// (boot-time construction failure, or explicitly turned off for tests).
	// When set, every HasPermission call also evaluates Cedar and emits a
	// structured telemetry log. For most actions the role-table verdict
	// remains authoritative; for actions listed in enforcedActions Cedar's
	// verdict overrides (Section B item #1 of the auth roadmap, 2026-04-24).
	cedarEngine *cedar.Engine

	// enforcedActions is the set of permission keys for which Cedar's
	// verdict overrides the role table. Empty (the default) keeps the
	// system in pure shadow mode. Populated from Config.EnforceActions
	// which the module reads from the CEDAR_ENFORCE_ACTIONS env var.
	// On a Cedar-side failure (engine nil after this point is impossible,
	// but evaluation panic / errors), HasPermission falls back to the
	// role-table verdict and records a "fallback_role" outcome on the
	// orkestra_cedar_enforced_total counter.
	enforcedActions map[string]struct{}

	mu                  sync.RWMutex
	systemPermissionSet map[string]struct{}    // keys declared with System=true
	allPermissionSet    map[string]struct{}    // every registered permission
	cachedPermSpecs     []iface.PermissionSpec // full specs for lazy reseed after a DB wipe
}

// UserSystemRoleLookup resolves a user's system role (from the user module).
// Kept as a plain function type so we don't need to import the user module.
type UserSystemRoleLookup func(ctx context.Context, userUUID string) (string, error)

// MFAGraceStarter starts the MFA enrollment grace clock for a user if it
// has not already started. Used as a post-binding hook when the caller
// just granted a privileged role — the callee owns the idempotency so a
// repeated grant doesn't reset an already-running clock.
type MFAGraceStarter func(ctx context.Context, userUUID string) error

// TenantCapabilityLookup returns the capability IDs the acting tenant
// currently holds active entitlements for. Used by the Cedar shadow-mode
// evaluator to populate Principal.Capabilities so the capability_grants
// defense-in-depth policy can reason about entitlements without this
// package importing the tenant module.
type TenantCapabilityLookup func(ctx context.Context, tenantUUID string) ([]string, error)

// TenantStatusLookup returns the tenant's lifecycle status ("active" |
// "suspended" | "archived" | "purged"). Threaded into Cedar's Resource
// so tenant_scope.cedar's inactive-tenant forbid rule has a real value
// to match on — previously the shadow evaluator hardcoded "active",
// which silenced that rule across every request. Kept as a callback so
// authz stays free of a direct tenant-module import; authz/module.go
// wires it from iface.TenantProvider.GetTenant.
type TenantStatusLookup func(ctx context.Context, tenantUUID string) (string, error)

type Config struct {
	Repo               *repository.Repository
	Redis              *database.RedisClientAdapter
	Logger             *slog.Logger
	LookupUser         UserSystemRoleLookup
	LookupCaps         TenantCapabilityLookup
	LookupTenantStatus TenantStatusLookup
	StartMFAGrace      MFAGraceStarter
	// Production gates sensitive role seeding decisions. When true, the
	// `developer` system role is seeded with a read-only permission set
	// (decision D9 in the Org-scoped RBAC plan). In dev and staging it
	// gets the full administrator-equivalent set so engineers can
	// actually debug things.
	Production bool
	// Environment is the deployment tag ("development" | "staging" |
	// "production") fed to the Cedar engine so policies can branch on
	// env. Empty defaults to "development" inside cedar.New.
	Environment string
	// EnforceActions is the per-permission allowlist of actions for which
	// Cedar's verdict overrides the role table. Empty keeps shadow mode
	// for every action. Wire this from CEDAR_ENFORCE_ACTIONS at the
	// module boundary; Service does not read env vars directly so tests
	// can construct it deterministically.
	EnforceActions []string
}

func New(cfg Config) *Service {
	enforced := make(map[string]struct{}, len(cfg.EnforceActions))
	for _, a := range cfg.EnforceActions {
		if a = strings.TrimSpace(a); a != "" {
			enforced[a] = struct{}{}
		}
	}
	s := &Service{
		repo:                cfg.Repo,
		redis:               cfg.Redis,
		logger:              cfg.Logger,
		userRoles:           cfg.LookupUser,
		startMFAGrace:       cfg.StartMFAGrace,
		lookupCaps:          cfg.LookupCaps,
		lookupTenantStatus:  cfg.LookupTenantStatus,
		production:          cfg.Production,
		enforcedActions:     enforced,
		systemPermissionSet: make(map[string]struct{}),
		allPermissionSet:    make(map[string]struct{}),
	}
	// Cedar shadow-mode engine. Failure to load the policies is a loud
	// slog.Error but does not block construction — shadow mode is
	// observability-only and must never turn a deployable binary into a
	// broken one. When enforce mode is active for some actions, the
	// engine load is still best-effort: a Cedar that fails to load just
	// means the enforce branch in HasPermission can't fire and every
	// action falls back to the role-table verdict (logged loud per call).
	if eng, err := cedar.New(cfg.Environment); err == nil {
		s.cedarEngine = eng
		if cfg.Logger != nil {
			mode := "shadow"
			if len(enforced) > 0 {
				mode = "enforce"
			}
			cfg.Logger.Info("cedar: engine loaded",
				slog.String("mode", mode),
				slog.Int("policies", eng.PolicyCount()),
				slog.Int("enforced_actions", len(enforced)),
				slog.String("env", cfg.Environment))
		}
	} else if cfg.Logger != nil {
		cfg.Logger.Error("cedar: failed to load policies — shadow mode disabled",
			slog.String("error", err.Error()))
	}
	return s
}

// roleElevatesPrivilege reports whether granting the named role should eagerly
// start the MFA enrollment grace clock for the target user. We match the same
// roles RoleRequiresMFA (in the auth module) considers privileged — keeping
// both in lock-step is load-bearing; if they drift a user could be gated at
// login without ever having had their grace window started.
func roleElevatesPrivilege(roleName string) bool {
	switch roleName {
	case "super_admin", "administrator", "org_owner", "org_admin":
		return true
	}
	return false
}

// --- Provider interface ---

func (s *Service) HasPermission(ctx context.Context, userUUID, tenantID, permission string) (bool, error) {
	perms, err := s.GetEffectivePermissions(ctx, userUUID, tenantID)
	if err != nil {
		return false, err
	}
	roleDecision := false
	for _, p := range perms {
		if p == permission || p == "*" {
			roleDecision = true
			break
		}
	}
	// Cedar evaluation. shadowEvaluate emits agree/divergence telemetry as
	// before and returns Cedar's verdict so the enforce branch below can
	// decide whether to override. ok=false means Cedar didn't run cleanly
	// (engine missing, panic, or evaluation errors) — under enforce mode
	// the call falls back to the role-table verdict.
	cedarDecision, cedarOK := s.shadowEvaluate(ctx, userUUID, tenantID, permission, roleDecision)
	if _, enforce := s.enforcedActions[permission]; enforce && s.cedarEngine != nil {
		return s.applyCedarEnforcement(permission, roleDecision, cedarDecision, cedarOK), nil
	}
	return roleDecision, nil
}

// shadowEvaluate runs the Cedar engine for the same (user, tenant,
// permission) triple and logs the outcome. When Cedar agrees with the
// role table, the line is emitted at Debug level ("cedar: agree"). When
// they disagree the level is Warn ("cedar: divergence") so operators can
// triage before flipping enforcement.
//
// Returns (decision, ok). ok is false when the engine is unavailable or
// the evaluation panicked / returned errors — callers in enforce mode
// must treat that as a fallback signal rather than a deny.
func (s *Service) shadowEvaluate(ctx context.Context, userUUID, tenantID, permission string, roleDecision bool) (decision cedar.Decision, ok bool) {
	if s.cedarEngine == nil || s.logger == nil {
		return cedar.Decision{}, false
	}
	defer func() {
		if r := recover(); r != nil {
			s.logger.Warn("cedar: shadow eval panicked",
				slog.String("permission", permission),
				slog.Any("recover", r))
			// Named returns let the deferred recover signal failure to
			// the enforce branch — without this an enforce-mode action
			// would silently grant on a Cedar panic.
			decision = cedar.Decision{}
			ok = false
		}
	}()
	start := time.Now()
	systemRole := ""
	if s.userRoles != nil {
		if r, err := s.userRoles(ctx, userUUID); err == nil {
			systemRole = r
		}
	}
	tenantRoles, _ := middleware.GetTenantRoles(ctx)
	tenantKind := middleware.TenantKindFromContext(ctx)
	if tenantKind == "" {
		// Fall back to "internal" for global/pre-ADR-0001 calls so
		// tier-aware forbid rules don't fire against an unknown kind.
		tenantKind = "internal"
	}
	var capabilities []string
	if s.lookupCaps != nil && tenantID != "" {
		if caps, err := s.lookupCaps(ctx, tenantID); err == nil {
			capabilities = caps
		}
	}
	// Tenant status drives tenant_scope.cedar's inactive-tenant forbid rule.
	// Fall back to "active" when the lookup isn't wired or the tenant isn't
	// found — global routes and test harnesses both depend on that default
	// so an absent signal doesn't flip previously-passing requests to deny.
	tenantStatus := "active"
	if s.lookupTenantStatus != nil && tenantID != "" {
		if st, err := s.lookupTenantStatus(ctx, tenantID); err == nil && st != "" {
			tenantStatus = st
		}
	}
	// MFA signals come from the JWT claims via middleware helpers so authz
	// doesn't need to import auth/models. On routes without a resolved
	// session (service-to-service, AI sidecar internal endpoints) both
	// helpers return zero values and the engine stamps mfa_enrolled=false.
	amr, _ := middleware.GetAMR(ctx)
	mfaEnrolled := middleware.IsMFAEnrolled(ctx)
	clientIP, _ := middleware.GetClientIP(ctx)
	principal := cedar.Principal{
		UserUUID:     userUUID,
		SystemRole:   systemRole,
		TenantRoles:  tenantRoles,
		Capabilities: capabilities,
		MFAEnrolled:  mfaEnrolled,
		AMR:          amr,
	}
	resource := cedar.Resource{
		TenantUUID:   tenantID,
		TenantKind:   tenantKind,
		TenantStatus: tenantStatus,
	}
	// Evaluate (not IsAuthorized) so we can plumb ClientIP into the
	// request — the engine classifies it into context.ip_bucket for
	// ABAC policies. RequiredCapability stays empty here; callers that
	// want capability enforcement still go through the dedicated path.
	decision = s.cedarEngine.Evaluate(cedar.Request{
		Principal: principal,
		Action:    permission,
		Resource:  resource,
		ClientIP:  clientIP,
	})
	attrs := []any{
		slog.String("user_uuid", userUUID),
		slog.String("tenant_id", tenantID),
		slog.String("permission", permission),
		slog.Bool("role_allow", roleDecision),
		slog.Bool("cedar_allow", decision.Allowed),
		slog.String("matched_policy", decision.MatchedPolicy),
		slog.Int64("latency_us", time.Since(start).Microseconds()),
	}
	if len(decision.Errors) > 0 {
		attrs = append(attrs, slog.Any("cedar_errors", decision.Errors))
	}
	if decision.Allowed == roleDecision {
		s.logger.Debug("cedar: agree", attrs...)
	} else {
		s.logger.Warn("cedar: divergence", attrs...)
		// Phase 5.3: record the divergence as a Prometheus counter so
		// operators can graph the trend and decide when to flip Cedar
		// from shadow to enforce. outcome labels the disagreement
		// shape — role-table allowed only, Cedar allowed only, or
		// neither/both (the latter only fires on matched-policy drift).
		outcome := "neither"
		switch {
		case roleDecision && !decision.Allowed:
			outcome = "role_only"
		case !roleDecision && decision.Allowed:
			outcome = "cedar_only"
		case roleDecision && decision.Allowed:
			outcome = "both"
		}
		metrics.Default().RecordCedarDivergence(actionSuffix(permission), decision.MatchedPolicy, outcome)
	}
	// Cedar evaluation errors don't fail the shadow log, but they do
	// disqualify the decision from being load-bearing in enforce mode.
	if len(decision.Errors) > 0 {
		return decision, false
	}
	return decision, true
}

// applyCedarEnforcement is invoked only when the action is in the enforce
// allowlist and the engine loaded successfully at boot. Returns the verdict
// the caller of HasPermission should observe: Cedar's decision when the
// evaluation succeeded, or the role-table decision when Cedar errored
// (fail-open from Cedar's perspective; the role table is still the
// secondary gate). Always emits one orkestra_cedar_enforced_total tick so
// operators can see how often Cedar's verdict was load-bearing vs. agreed
// vs. fell back. The logger output here is at Info for agreement (high
// volume but useful baseline) and Warn for overrides (security-relevant).
func (s *Service) applyCedarEnforcement(permission string, roleDecision bool, decision cedar.Decision, cedarOK bool) bool {
	suffix := actionSuffix(permission)
	if !cedarOK {
		if s.logger != nil {
			s.logger.Error("cedar: enforce-mode evaluation failed; falling back to role-table verdict",
				slog.String("permission", permission),
				slog.Bool("role_allow", roleDecision),
				slog.Any("cedar_errors", decision.Errors))
		}
		metrics.Default().RecordCedarEnforced(suffix, "fallback_role")
		return roleDecision
	}
	var outcome string
	switch {
	case decision.Allowed && roleDecision:
		outcome = "agree_allow"
	case !decision.Allowed && !roleDecision:
		outcome = "agree_deny"
	case decision.Allowed && !roleDecision:
		outcome = "cedar_override_allow"
	default:
		outcome = "cedar_override_deny"
	}
	if s.logger != nil {
		if outcome == "cedar_override_allow" || outcome == "cedar_override_deny" {
			s.logger.Warn("cedar: enforce-mode override",
				slog.String("permission", permission),
				slog.String("outcome", outcome),
				slog.Bool("role_allow", roleDecision),
				slog.Bool("cedar_allow", decision.Allowed),
				slog.String("matched_policy", decision.MatchedPolicy))
		}
	}
	metrics.Default().RecordCedarEnforced(suffix, outcome)
	return decision.Allowed
}

// actionSuffix returns the dotted-key tail of a permission ("foo.bar.read"
// → "read"). Keys without a dot return as-is. Used for low-cardinality
// Prometheus labels on Cedar metrics.
func actionSuffix(permission string) string {
	if idx := strings.LastIndex(permission, "."); idx >= 0 && idx < len(permission)-1 {
		return permission[idx+1:]
	}
	return permission
}

func (s *Service) GetEffectivePermissions(ctx context.Context, userUUID, tenantID string) ([]string, error) {
	if userUUID == "" {
		return nil, errors.New("authz: userUUID required")
	}

	if cached, ok := s.cacheGet(ctx, userUUID, tenantID); ok {
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
	// inherits every system-level permission. developer is environment-gated:
	// dev/staging also inherits every system-level permission; production
	// restricts it to read-level system perms (D9 of the Org-scoped RBAC
	// plan) so a leaked developer token cannot mutate prod data or write
	// secrets. The shortcut must mirror the seeded-role permission set —
	// otherwise a production developer could skip the seeded list via the
	// shortcut and regain full access.
	switch systemRole {
	case "super_admin":
		perms["*"] = struct{}{}
	case "administrator":
		s.mu.RLock()
		for k := range s.systemPermissionSet {
			perms[k] = struct{}{}
		}
		s.mu.RUnlock()
	case "developer":
		s.mu.RLock()
		for k := range s.systemPermissionSet {
			if s.production {
				if !strings.HasSuffix(k, ".read") &&
					!strings.HasSuffix(k, ".view") &&
					!strings.HasSuffix(k, ".self") {
					continue
				}
			}
			perms[k] = struct{}{}
		}
		s.mu.RUnlock()
	}

	// Union of global bindings (tenantID="").
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

	// Union of tenant-scoped bindings.
	if tenantID != "" {
		scoped, err := s.repo.ListActiveBindingsForUser(ctx, userUUID, tenantID)
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
	s.cacheSet(ctx, userUUID, tenantID, out)
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
// have tenantId="" and IsSystem=true. Permission lists are derived from the
// permissions catalog that modules have registered by the time this runs.
// Call this after RegisterPermissions has been called for every module.
//
// Hierarchy (most to least privileged):
//
//	Platform-level (system roles, granted via global bindings):
//	  super_admin   — wildcard, full power, can assign every other role
//	  administrator — all permissions, cannot elevate peers to admin
//	  developer     — all permissions in dev/staging; .read/.view/.self in prod
//	  manager       — read/create/update, no delete, no admin
//	  operator      — read + self-service
//	  guest         — read-only
//
//	Tenant-level (org roles, granted via tenant-scoped bindings):
//	  org_owner   — every non-system permission within the tenant
//	  org_admin   — same as org_owner minus .delete suffixes
//	  org_member  — .read/.view/.self/.own across the tenant
//	  org_billing — billing/payments/subscriptions surface only
//	  org_viewer  — .read/.view across the tenant
//
// Both groups are seeded as IsSystem=true rows in the global catalog —
// the org-scoped semantics come from the binding's tenantId, not the
// role's. CreateBinding's separation rule keeps the two groups disjoint:
// system roles only via global bindings, org roles only via tenant
// bindings.
//
// The cascade distinction between administrator and developer is enforced
// at role-assignment time (commit C of the org-role split, 2026-04-24),
// not baked into the permission set.
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

	// Developer role is environment-gated (D9 of the Org-scoped RBAC plan):
	// in dev/staging it mirrors administrator so engineers can touch
	// anything while debugging; in production it collapses to read-only
	// (plus .view and .self) so a leaked or misused developer token can't
	// mutate data or exfil secrets. The env flag is captured at service
	// construction — changes require a reboot (or a manual reseed by a
	// super_admin wiping authz_roles and letting the lazy-heal kick in).
	developerPermissions := allKeys
	developerDescription := "Technical power user — all permissions. Cannot manage administrator or super_admin accounts."
	if s.production {
		developerPermissions = filter(allKeys, func(p string) bool {
			return strings.HasSuffix(p, ".read") ||
				strings.HasSuffix(p, ".view") ||
				strings.HasSuffix(p, ".self")
		})
		developerDescription = "Technical power user — PRODUCTION: read-only access (read/view/self suffixes only). Full access restored automatically in dev/staging."
	}

	// Org-scoped roles (Section B item #3 of the auth roadmap, 2026-04-24).
	// These are stored as global system rows (tenantId="", IsSystem=true)
	// so the catalog stays at a flat 11 roles, but they are intended to be
	// granted through tenant-scoped bindings (binding.tenantId != "") to
	// give a user power inside one tenant without elevating them at the
	// platform level. Crucially, every org-role permission set excludes
	// anything tagged System=true — a tenant owner cannot manage modules,
	// other tenants, or platform users no matter what binding they hold.
	// CreateBinding's separation rule (commit C) enforces the inverse:
	// system roles cannot be granted through a tenant-scoped binding.
	s.mu.RLock()
	nonSystem := filter(allKeys, func(p string) bool {
		_, isSystem := s.systemPermissionSet[p]
		return !isSystem
	})
	s.mu.RUnlock()

	orgOwner := nonSystem
	orgAdmin := filter(nonSystem, func(p string) bool {
		return !strings.HasSuffix(p, ".delete")
	})
	orgMember := filter(nonSystem, func(p string) bool {
		return strings.HasSuffix(p, ".read") ||
			strings.HasSuffix(p, ".view") ||
			strings.HasSuffix(p, ".self") ||
			strings.HasSuffix(p, ".own")
	})
	// org_billing scopes to the three finance-surface modules. Module
	// prefix matches the catalog naming convention (billing.invoice.read,
	// payments.transaction.refund, subscriptions.client.manage, …).
	orgBilling := filter(nonSystem, func(p string) bool {
		return strings.HasPrefix(p, "billing.") ||
			strings.HasPrefix(p, "payments.") ||
			strings.HasPrefix(p, "subscriptions.")
	})
	orgViewer := filter(nonSystem, func(p string) bool {
		return strings.HasSuffix(p, ".read") || strings.HasSuffix(p, ".view")
	})

	roles := []models.Role{
		{UUID: uuid.NewString(), Name: "super_admin", Description: "Full power — wildcard permission, can assign every role.", Permissions: []string{"*"}, IsSystem: true, IsActive: true},
		{UUID: uuid.NewString(), Name: "administrator", Description: "Organization administrator — all permissions. Cannot elevate peers to administrator or super_admin.", Permissions: allKeys, IsSystem: true, IsActive: true},
		{UUID: uuid.NewString(), Name: "developer", Description: developerDescription, Permissions: developerPermissions, IsSystem: true, IsActive: true},
		{UUID: uuid.NewString(), Name: "manager", Description: "Read/write, no admin, no delete.", Permissions: manager, IsSystem: true, IsActive: true},
		{UUID: uuid.NewString(), Name: "operator", Description: "Read-only + self-service.", Permissions: operator, IsSystem: true, IsActive: true},
		{UUID: uuid.NewString(), Name: "guest", Description: "Read-only access.", Permissions: guest, IsSystem: true, IsActive: true},
		{UUID: uuid.NewString(), Name: "org_owner", Description: "Tenant owner — every non-system permission within this tenant. Cannot manage modules, other tenants, or platform users.", Permissions: orgOwner, IsSystem: true, IsActive: true},
		{UUID: uuid.NewString(), Name: "org_admin", Description: "Tenant admin — every non-system permission except deletes. Cannot remove tenant resources.", Permissions: orgAdmin, IsSystem: true, IsActive: true},
		{UUID: uuid.NewString(), Name: "org_member", Description: "Tenant member — read across the tenant plus self/own scopes for personal resources.", Permissions: orgMember, IsSystem: true, IsActive: true},
		{UUID: uuid.NewString(), Name: "org_billing", Description: "Tenant billing — billing, payments, and subscriptions surface only.", Permissions: orgBilling, IsSystem: true, IsActive: true},
		{UUID: uuid.NewString(), Name: "org_viewer", Description: "Tenant viewer — read-only access to every read/view permission.", Permissions: orgViewer, IsSystem: true, IsActive: true},
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

func (s *Service) CreateRole(ctx context.Context, tenantID string, input models.CreateRoleInput) (*models.Role, error) {
	role := &models.Role{
		UUID:        uuid.NewString(),
		TenantID:    tenantID,
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

// GetRoleByName resolves a role by (tenantID, name). System roles use
// tenantID="" — the global catalog. Custom roles use the owning tenant
// UUID. Public so other modules (e.g. tenant's CreateTenant hook) can
// look up the system org_owner row by name without holding its UUID.
func (s *Service) GetRoleByName(ctx context.Context, tenantID, name string) (*models.Role, error) {
	if err := s.ensureSeeded(ctx); err != nil && s.logger != nil {
		s.logger.Warn("authz ensureSeeded failed in GetRoleByName",
			slog.String("error", err.Error()))
	}
	return s.repo.GetRoleByName(ctx, tenantID, name)
}

func (s *Service) ListRoles(ctx context.Context, tenantID string) ([]models.Role, error) {
	if err := s.ensureSeeded(ctx); err != nil && s.logger != nil {
		s.logger.Warn("authz ensureSeeded failed",
			slog.String("error", err.Error()))
	}
	return s.repo.ListRoles(ctx, tenantID)
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

func (s *Service) CreateBinding(ctx context.Context, tenantID, grantedBy string, input models.CreateBindingInput) (*models.Binding, error) {
	role, err := s.repo.GetRoleByUUID(ctx, input.RoleUUID)
	if err != nil {
		return nil, err
	}
	if !role.IsActive {
		return nil, ErrRoleInactive
	}
	// Separation rule: system roles only via global bindings, tenant-scope
	// roles only via tenant-scoped bindings. Applies always, even to the
	// "system" sentinel granter — a platform-issued auto-grant must still
	// respect the tier discipline.
	if err := validateBindingScope(role, tenantID); err != nil {
		return nil, err
	}
	// Cascade rule: caller cannot grant a role whose permissions exceed
	// their own. Bypassed for the platform-issued "system" granter.
	if grantedBy == "" {
		return nil, ErrGranterRequired
	}
	if grantedBy != granterSystem {
		granterPerms, err := s.GetEffectivePermissions(ctx, grantedBy, tenantID)
		if err != nil {
			return nil, fmt.Errorf("authz: resolve granter perms: %w", err)
		}
		if err := validateBindingCascade(role, granterPerms); err != nil {
			return nil, err
		}
	}
	b := &models.Binding{
		UUID:      uuid.NewString(),
		UserUUID:  input.UserUUID,
		TenantID:  tenantID,
		RoleUUID:  role.UUID,
		RoleName:  role.Name,
		GrantedBy: grantedBy,
		ExpiresAt: input.ExpiresAt,
	}
	if err := s.repo.CreateBinding(ctx, b); err != nil {
		return nil, err
	}
	s.cacheInvalidate(ctx, input.UserUUID)

	// Post-binding hook: privileged role grants eagerly start the MFA grace
	// clock so the 7-day window begins at promotion rather than at next
	// login. StartMFAGraceIfUnset on the user side is idempotent, so
	// repeated grants don't reset the clock.
	if s.startMFAGrace != nil && roleElevatesPrivilege(role.Name) {
		if err := s.startMFAGrace(ctx, input.UserUUID); err != nil {
			s.logger.Warn("authz: start MFA grace failed after binding",
				"userUUID", input.UserUUID,
				"role", role.Name,
				"error", err.Error(),
			)
		}
	}
	return b, nil
}

func (s *Service) ListBindings(ctx context.Context, tenantID string) ([]models.Binding, error) {
	return s.repo.ListBindingsByTenant(ctx, tenantID)
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

func (s *Service) cacheKey(userUUID, tenantID string) string {
	if tenantID == "" {
		tenantID = "-"
	}
	return "authz:cache:" + userUUID + ":" + tenantID
}

func (s *Service) cacheGet(ctx context.Context, userUUID, tenantID string) ([]string, bool) {
	if s.redis == nil {
		return nil, false
	}
	raw, err := s.redis.Get(ctx, s.cacheKey(userUUID, tenantID))
	if err != nil || raw == "" {
		return nil, false
	}
	var out []string
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		return nil, false
	}
	return out, true
}

func (s *Service) cacheSet(ctx context.Context, userUUID, tenantID string, perms []string) {
	if s.redis == nil {
		return
	}
	data, err := json.Marshal(perms)
	if err != nil {
		return
	}
	_ = s.redis.Set(ctx, s.cacheKey(userUUID, tenantID), string(data), 60*time.Second)
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

// validateBindingScope enforces the system/tenant separation rule:
// platform system roles need global bindings; everything else (org_*,
// custom roles) needs tenant-scoped bindings. Returns nil when (role,
// tenantID) form a legitimate pair. Pure function; safe to call without
// the repo. See ErrSystemRoleNotGrantableInTenant /
// ErrTenantRoleNotGrantableGlobally.
func validateBindingScope(role *models.Role, tenantID string) error {
	platformRole := isPlatformSystemRole(role)
	if platformRole && tenantID != "" {
		return ErrSystemRoleNotGrantableInTenant
	}
	if !platformRole && tenantID == "" {
		return ErrTenantRoleNotGrantableGlobally
	}
	return nil
}

// validateBindingCascade enforces the cascade rule: every permission the
// granted role would confer must already be present in the granter's
// effective set. Granter holding the wildcard "*" bypasses (super_admin
// can grant anything). A role asking for "*" requires the granter to also
// hold "*". Pure function; the wrapper in CreateBinding fetches granter
// perms via GetEffectivePermissions before calling this.
func validateBindingCascade(role *models.Role, granterPerms []string) error {
	granterSet := make(map[string]struct{}, len(granterPerms))
	granterWildcard := false
	for _, p := range granterPerms {
		if p == "*" {
			granterWildcard = true
		}
		granterSet[p] = struct{}{}
	}
	if granterWildcard {
		return nil
	}
	for _, p := range role.Permissions {
		if p == "*" {
			// Granter lacks wildcard; refusing super_admin grants from a
			// non-super_admin caller is the whole point of the cascade.
			return ErrInsufficientPermissionsToGrant
		}
		if _, ok := granterSet[p]; !ok {
			return ErrInsufficientPermissionsToGrant
		}
	}
	return nil
}

func filter(in []string, pred func(string) bool) []string {
	out := make([]string, 0, len(in))
	for _, s := range in {
		if pred(s) {
			out = append(out, s)
		}
	}
	return out
}

