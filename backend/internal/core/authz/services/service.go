package services

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/orkestra/backend/internal/core/authz/cedar"
	"github.com/orkestra/backend/internal/core/authz/models"
	"github.com/orkestra/backend/internal/core/authz/repository"
	"github.com/orkestra/backend/internal/shared/middleware"
	"github.com/orkestra/backend/pkg/sdk/ctxauth"
	"github.com/orkestra/backend/pkg/sdk/iface"
	"github.com/orkestra/backend/pkg/sdk/metrics"
	"github.com/orkestra/backend/pkg/sdk/module"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
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

// ErrBindingExists is returned when CreateBinding targets a
// (tenantID, userUUID, roleID) tuple that already has a binding — surfaced
// from the authz_bindings unique compound index (see this module's
// CLAUDE.md and the 0009 migration) rather than a raw duplicate-key error.
// Callers that want "grant if absent, otherwise return the existing row"
// semantics should call EnsureBinding instead.
var ErrBindingExists = errors.New("authz: role already bound")

// ErrAuthzCacheUnavailable is returned when a GRANT was refused because
// the effective-permission cache could not be retired.
//
// A grant bumps the cache generation BEFORE it writes (see
// withGeneration): a counter the store cannot bump means the change's
// effect cannot be guaranteed, so the change is refused and nothing is
// written. Handlers answer 503 errcode.AuthzCacheUnavailable — the
// caller's request was fine, the server's cache store was not, and the
// retry is theirs to make.
//
// REVOCATIONS never return this (D27 as amended by P22). They write
// first and report the invalidation failure through logs and metrics,
// because a refused revocation leaves the privilege granted
// indefinitely while a written one leaves a stale verdict for at most
// the 60s TTL. Neither do platform-issued grants (P24).
//
// A deployment with no Redis wired never sees this: there are no cached
// verdicts to retire, so the invalidation is a no-op success. "No cache
// configured" is not "cache unavailable".
var ErrAuthzCacheUnavailable = errors.New("authz: permission cache unavailable")

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

// repoBackend is the narrow surface Service consumes from the
// repository. Declared as an interface so tests can inject an
// in-memory fake without standing up Mongo. *repository.Repository
// satisfies it via Go's structural typing — production wiring is
// unchanged.
type repoBackend interface {
	UpsertPermission(ctx context.Context, p *models.Permission) error
	ListPermissions(ctx context.Context) ([]models.Permission, error)
	ListAllPermissionKeys(ctx context.Context) ([]string, error)
	UpsertRole(ctx context.Context, role *models.Role) error
	UpdateRoleFields(ctx context.Context, uuid string, fields bson.M) error
	GetRoleByName(ctx context.Context, tenantID, name string) (*models.Role, error)
	GetRoleByUUID(ctx context.Context, uuid string) (*models.Role, error)
	CountSystemRoles(ctx context.Context) (int64, error)
	ListRoles(ctx context.Context, tenantID string) ([]models.Role, error)
	DeleteRole(ctx context.Context, uuid string) error
	CreateBinding(ctx context.Context, b *models.Binding) error
	EnsureBinding(ctx context.Context, b *models.Binding) (*models.Binding, error)
	DeleteBinding(ctx context.Context, tenantID, uuid string) error
	DeleteBindingsByRoleUUID(ctx context.Context, roleUUID string) (int64, error)
	DeleteBindingsByTenant(ctx context.Context, tenantUUID string) (int64, error)
	DeleteBindingsByUserAndTenant(ctx context.Context, userUUID, tenantUUID string) (int64, error)
	ListActiveBindingsForUser(ctx context.Context, userUUID, tenantID string) ([]models.Binding, error)
	ListBindingsByTenant(ctx context.Context, tenantID string) ([]models.Binding, error)
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
// invalidated when bindings or roles change. The cache key is
// generation-keyed (see the Cache section below), so an invalidation is
// one atomic INCR rather than a scan-and-delete.
type Service struct {
	repo  repoBackend
	redis module.RedisClient
	// mget is redis narrowed to the optional MGET extension, resolved
	// once by setRedis. Nil when the configured client does not provide
	// MGET, which disables the cache entirely (see MultiGetRedisClient).
	mget               MultiGetRedisClient
	logger             *slog.Logger
	userRoles          UserSystemRoleLookup
	startMFAGrace      MFAGraceStarter
	lookupCaps         TenantCapabilityLookup
	lookupTenantStatus TenantStatusLookup
	// lookupSessionRisk is wired post-InitAll via SetSessionRiskLookup
	// because the auth module (which owns the auth_sessions repo) does
	// not finish its own Init until after authz. Nil falls back to
	// zero risk on the Cedar principal — no divergence, no ABAC effect.
	lookupSessionRisk SessionRiskLookup
	// production restricts the developer system role to read-only, and
	// raises a Cedar shadow-mode divergence from Warn to Error — the two
	// places where the same deployment fact means "no one is watching a
	// rollout here".
	production bool

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

// SessionRiskLookup returns the most recent risk score for a session
// UUID, in [0.0, 1.0]. Stamped onto the Cedar principal as
// risk_score + risk_level so ABAC policies can reason about session
// risk alongside role and capability. Wired post-InitAll by main.go —
// authz.Init runs before the auth module has constructed its
// auth_sessions repo, so the setter pattern mirrors how the tenant
// module wires the OwnerRoleBinder into authz. Section C item #2 of
// the 2026-04-24 auth roadmap.
type SessionRiskLookup func(ctx context.Context, sessionID string) (float64, error)

type Config struct {
	Repo               *repository.Repository
	Redis              module.RedisClient
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
	// Wired through setRedis rather than the struct literal so the
	// optional MGET extension is resolved exactly once, here, and a
	// client that lacks it is reported at boot instead of silently on
	// every request.
	s.setRedis(cfg.Redis, cfg.Logger)
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

// SetSessionRiskLookup wires the sid → risk-score resolver. Called
// post-InitAll from main.go after the auth module has constructed its
// auth_sessions repository. Safe to call before the first request —
// the authz module's Init finishes well before any handler binds. A
// nil lookup falls back to zero risk on the Cedar principal, same as
// not wiring the setter at all.
func (s *Service) SetSessionRiskLookup(lookup SessionRiskLookup) {
	s.lookupSessionRisk = lookup
}

// riskLevelForScore mirrors auth/services.RiskLevelForScore without
// importing the auth package (authz sits below auth in the module
// dependency order). The two ladders must stay in sync — if one
// changes, update both. Guarded by a unit test (see service_test.go).
func riskLevelForScore(score float64) string {
	switch {
	case score >= 0.7:
		return "critical"
	case score >= 0.5:
		return "high"
	case score >= 0.3:
		return "medium"
	default:
		return "low"
	}
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
// they disagree the line is "cedar: divergence", at Error level in
// production and Warn everywhere else, so operators can triage before
// flipping enforcement.
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
	// Tenant roles belong to a TENANT-scoped decision. A global check
	// (tenantID == "") is made by RequireSystemPermission and by the
	// impersonation pre-check (middleware/auth.go), and for those a
	// membership role in whatever tenant the request happened to resolve
	// is not an input to the decision — stamping it is how a tenant
	// permit came to fire on a platform action. Spec §4.5 D24, the
	// second half of H-5 (the first half is system_actions.cedar's
	// forbid, which only reaches keys whose module prefix is "system").
	var tenantRoles []string
	if tenantID != "" {
		tenantRoles, _ = ctxauth.GetTenantRoles(ctx)
	}
	tenantKind := ctxauth.TenantKindFromContext(ctx)
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
	clientIP, _ := ctxauth.GetClientIP(ctx)
	// Risk signals: pull the session's most recent score via the lookup
	// callback (wired post-InitAll) and derive the level locally. Score
	// is in [0.0, 1.0]; the engine multiplies by 100 when stamping the
	// Long attribute. A lookup error degrades gracefully to zero risk.
	var riskScore float64
	var riskLevel string
	if s.lookupSessionRisk != nil {
		if sid, ok := middleware.GetSessionID(ctx); ok {
			if score, err := s.lookupSessionRisk(ctx, sid); err == nil {
				riskScore = score
				riskLevel = riskLevelForScore(score)
			} else if s.logger != nil {
				s.logger.Debug("cedar: session risk lookup failed",
					slog.String("sid", sid),
					slog.String("error", err.Error()))
			}
		}
	}
	principal := cedar.Principal{
		UserUUID:     userUUID,
		SystemRole:   systemRole,
		TenantRoles:  tenantRoles,
		Capabilities: capabilities,
		MFAEnrolled:  mfaEnrolled,
		AMR:          amr,
		RiskScore:    riskScore,
		RiskLevel:    riskLevel,
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
		// Warn outside production, Error in it. A divergence is a
		// disagreement about a real authorization decision, and in
		// production nobody is reading the shadow-mode telemetry the
		// way they do while rolling a policy out — the level is what
		// decides whether an alert fires. Below production the same
		// line is the expected output of policy work in progress, so
		// raising it there would train operators to ignore it.
		level := slog.LevelWarn
		if s.production {
			level = slog.LevelError
		}
		s.logger.Log(ctx, level, "cedar: divergence", attrs...)
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

// systemPermissionSnapshot copies the platform-reserved (System:true)
// key set under a single read lock, so a caller can test membership
// inside a loop that makes repository calls without holding s.mu across
// I/O. Same reason classifyPermissionKeys takes the lock once: the set
// is small (the union of every module's System permissions) and only
// rewritten by RegisterPermissions at boot, so one copy per cache miss
// is cheaper than a lock held across Mongo round trips.
func (s *Service) systemPermissionSnapshot() map[string]struct{} {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make(map[string]struct{}, len(s.systemPermissionSet))
	for k := range s.systemPermissionSet {
		out[k] = struct{}{}
	}
	return out
}

func (s *Service) GetEffectivePermissions(ctx context.Context, userUUID, tenantID string) ([]string, error) {
	if userUUID == "" {
		return nil, errors.New("authz: userUUID required")
	}

	// Read both generation counters ONCE and compose the read key and
	// the write key at the bottom of this function from the SAME pair.
	//
	// Re-reading them at write time is the lost-invalidation window: a
	// verdict computed BEFORE a concurrent INCR would be filed under the
	// generation current AFTER it, republishing the pre-bump answer as
	// the live entry for the full TTL. Writing under the older pair is
	// strictly safe — if nothing was invalidated the key is still
	// current, and if something was, the entry is born dead instead of
	// born stale.
	//
	// This closes the "the reader read the generations before the bump"
	// class deterministically. It does NOT close "the reader read after
	// the pre-bump but resolved Mongo before the write" — that one is
	// withGeneration's post-write bump (D27), which is best-effort. The
	// two are complementary, not alternatives.
	g, u, genOK := s.generations(ctx, userUUID)
	if genOK {
		if cached, ok := s.cacheGetAt(ctx, g, u, userUUID, tenantID); ok {
			return cached, nil
		}
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
	//
	// Platform-reserved keys are skipped here, which makes evaluator
	// rule 4 in authz/CLAUDE.md — System:true permissions require a
	// GLOBAL grant (by system role, or by a binding with an empty
	// orgID), never a per-org binding — enforced rather than incidental.
	// It held only because no seeded tenant role carries a platform key,
	// which is not a guarantee: existing data can carry a stale one, and
	// until the D21 validator landed anyone able to edit a custom role
	// could write one in. Skipping the key here makes the ones already in
	// the data inert; they stay stored, and role reads still show them.
	//
	// The wildcard is skipped for the same reason and as the same class:
	// D21 refuses "*" and a platform key with one error, and "*" is the
	// maximal case of the hazard, because HasPermission short-circuits on
	// it. Nothing legitimate is lost. A super_admin's wildcard reaches a
	// principal two ways, and this filter is on neither: the systemRole
	// shortcut above, and a GLOBAL binding to the seeded "super_admin"
	// role (which carries Permissions ["*"]) — validateBindingScope in
	// fact REQUIRES a platform role to be granted globally, refusing it
	// inside a tenant with ErrSystemRoleNotGrantableInTenant. So a
	// tenant-scoped binding conveying "*" is a shape the write path
	// already rejects, and the only thing this can drop is stale data,
	// which is the point.
	//
	// The GLOBAL branch above is deliberately NOT filtered: that is where
	// a platform role legitimately grants a platform key — the seeded
	// "administrator" role carries every registered key and is reached
	// through a global binding — and filtering it would strip the
	// operator console's own permissions. Closes the audit's L-9.
	//
	// The system-key set is snapshotted before the loop rather than read
	// under s.mu inside it: the loop makes repository calls, and holding
	// a lock across I/O is exactly what classifyPermissionKeys was
	// extracted to avoid.
	if tenantID != "" {
		scoped, err := s.repo.ListActiveBindingsForUser(ctx, userUUID, tenantID)
		if err != nil {
			return nil, err
		}
		systemKeys := s.systemPermissionSnapshot()
		for _, b := range scoped {
			role, err := s.repo.GetRoleByUUID(ctx, b.RoleUUID)
			if err != nil || !role.IsActive {
				continue
			}
			for _, p := range role.Permissions {
				if p == "*" {
					continue
				}
				if _, isSystem := systemKeys[p]; isSystem {
					continue
				}
				perms[p] = struct{}{}
			}
		}
	}

	out := make([]string, 0, len(perms))
	for k := range perms {
		out = append(out, k)
	}
	if genOK {
		s.cacheSetAt(ctx, g, u, userUUID, tenantID, out)
	}
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
	// payments.transaction.refund, subscriptions.subscription.manage, …).
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

// CreateRole builds a custom (non-system) role for one tenant.
//
// actor is the UUID of the caller the role is written on behalf of. Its
// effective permissions bound what the role may carry (D21): a role can
// never grant more than its author already holds, the same rule
// CreateBinding applies to a grant. The literal "system" is a sentinel
// that waives that cascade for platform-issued writes; no in-tree caller
// passes it today (SeedSystemRoles writes through the repository
// directly), and this package's own tests are its only users. The
// catalog and platform-key checks bind every caller, "system" included —
// see validateCustomRolePermissions.
func (s *Service) CreateRole(ctx context.Context, tenantID, actor string, input models.CreateRoleInput) (*models.Role, error) {
	name := strings.TrimSpace(input.Name)
	if name == "" {
		return nil, ErrRoleNameRequired
	}
	perms, err := s.validateCustomRolePermissions(ctx, tenantID, actor, input.Permissions)
	if err != nil {
		return nil, err
	}
	role := &models.Role{
		UUID:        uuid.NewString(),
		TenantID:    tenantID,
		Name:        name,
		Description: input.Description,
		Permissions: perms,
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
// The authz cache is retired because permission membership may change —
// through the gate for a patch that only adds, write-then-report for one
// that takes a permission away (P25; see the comment at the write).
//
// actor is the UUID of the caller the edit is written on behalf of, and
// bounds what the role may carry exactly as it does in CreateRole (D21).
// A patch that does not supply Permissions is not validated against it,
// so a role already holding a key no module declares any more can still
// be renamed or disabled. The literal "system" waives the cascade for
// platform-issued writes; no in-tree caller passes it today.
func (s *Service) UpdateRole(ctx context.Context, tenantID, roleUUID, actor string, input models.UpdateRoleInput) (*models.Role, error) {
	existing, err := s.repo.GetRoleByUUID(ctx, roleUUID)
	if err != nil {
		return nil, err
	}

	// Tenant-scope guard: a custom role may only be edited from within its own
	// tenant. Without this the {tenantId} path segment was decorative — the role
	// was resolved by UUID alone, so a member of tenant A could rewrite tenant
	// B's custom role. System roles (TenantID=="") are global platform config and
	// keep the immutable-except-IsActive behaviour enforced just below.
	if !existing.IsSystem && existing.TenantID != tenantID {
		return nil, repository.ErrNotFound
	}

	touchesImmutable := input.Name != nil || input.Description != nil || input.Permissions != nil
	if existing.IsSystem && touchesImmutable {
		return nil, ErrSystemRoleImmutable
	}

	fields := bson.M{}
	if input.Name != nil {
		name := strings.TrimSpace(*input.Name)
		if name == "" {
			return nil, ErrRoleNameRequired
		}
		fields["name"] = name
	}
	if input.Description != nil {
		fields["description"] = *input.Description
	}
	removesAccess := false
	if input.Permissions != nil {
		// An IsActive-only (or name-only) patch never reaches here, so a
		// role that already holds a stale key can still be disabled
		// (edge case 13).
		perms, err := s.validateCustomRolePermissions(ctx, tenantID, actor, input.Permissions)
		if err != nil {
			return nil, err
		}
		fields["permissions"] = perms
		// The direction of this patch (P25), from the two lists already
		// in hand — no extra read.
		removesAccess = !isPermissionSuperset(perms, existing.Permissions)
	}
	if input.IsActive != nil {
		fields["isActive"] = *input.IsActive
	}

	if len(fields) == 0 {
		return existing, nil
	}

	// The generation bump lands HERE, not at the top of the method: every
	// refusal above (cross-tenant 404, system-role 403, the D21 validator's
	// 422s) must retire nothing, or a request the service rejects becomes a
	// remotely triggerable cache flush. globalScope because a role reaches
	// its holders through bindings we would have to scan to enumerate.
	//
	// P25: the role editor routes by direction like every other mutation,
	// because a patch that drops a permission IS a revocation and the
	// decision behind D27 is that removing access is never blocked by a
	// cache wobble. A patch whose result is not a superset of the current
	// set writes first; anything else — a pure addition, a rename, an
	// isActive toggle — keeps the gate. A patch that both adds and removes
	// counts as removing, which is the safe direction: a stale verdict then
	// denies the ADDED keys (harmless, ≤60s) instead of keeping the removed
	// ones live indefinitely.
	write := s.withGeneration
	if removesAccess {
		write = s.writeThenInvalidate
	}
	if err := write(ctx, globalScope(), func() error {
		return s.repo.UpdateRoleFields(ctx, roleUUID, fields)
	}); err != nil {
		return nil, err
	}

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
func (s *Service) DeleteRole(ctx context.Context, tenantID, roleUUID string) error {
	existing, err := s.repo.GetRoleByUUID(ctx, roleUUID)
	if err != nil {
		return err
	}
	if existing.IsSystem {
		return ErrSystemRoleImmutable
	}
	// Tenant-scope guard: only a custom role owned by the acting tenant may be
	// deleted here — otherwise the {tenantId} path was decorative and a member of
	// tenant A could delete tenant B's role (cascading B's bindings).
	if existing.TenantID != tenantID {
		return repository.ErrNotFound
	}
	// A role delete is a REVOCATION: every binding pointing at it goes
	// with it. Write-then-report, never a refusal (D27 as amended by
	// P22) — refusing would leave the role and its grants live
	// indefinitely, where writing leaves a stale verdict for at most the
	// 60s TTL. The cascade delete and the role delete are one mutation
	// for invalidation purposes, so both sit inside the closure.
	return s.writeThenInvalidate(ctx, globalScope(), func() error {
		removed, err := s.repo.DeleteBindingsByRoleUUID(ctx, roleUUID)
		if err != nil {
			return fmt.Errorf("cascade bindings: %w", err)
		}
		if removed > 0 && s.logger != nil {
			s.logger.Info("authz: cascaded binding delete",
				slog.String("role", existing.Name),
				slog.Int64("bindings", removed))
		}
		return s.repo.DeleteRole(ctx, roleUUID)
	})
}

func (s *Service) ListPermissions(ctx context.Context) ([]models.Permission, error) {
	if err := s.ensureSeeded(ctx); err != nil && s.logger != nil {
		s.logger.Warn("authz ensureSeeded failed",
			slog.String("error", err.Error()))
	}
	return s.repo.ListPermissions(ctx)
}

// --- Bindings ---

// validateBindingGrant resolves the target role and runs the
// binding-creation validation pipeline shared by CreateBinding and
// EnsureBinding: role-active check, the system/tenant separation rule, and
// (for non-"system" granters) the permission cascade rule. Extracted so the
// two entry points can never drift apart on what a grant must satisfy.
func (s *Service) validateBindingGrant(ctx context.Context, tenantID, grantedBy string, input models.CreateBindingInput) (*models.Role, error) {
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
	return role, nil
}

// afterBindingGrant runs the post-persist side effect shared by
// CreateBinding and EnsureBinding: for a role that elevates privilege,
// eagerly start the target's MFA enrollment grace clock. Safe to call
// unconditionally, including on EnsureBinding's reused-existing-row
// path, because StartMFAGraceIfUnset (per its name) no-ops once the
// clock is already running, so a replayed ensure never resets it.
//
// Cache invalidation is NOT here any more: it moved into the
// withGeneration wrapper around each persist, so a cache that cannot be
// retired refuses the grant instead of following it (D27).
func (s *Service) afterBindingGrant(ctx context.Context, userUUID, roleName string) {
	if s.startMFAGrace != nil && roleElevatesPrivilege(roleName) {
		if err := s.startMFAGrace(ctx, userUUID); err != nil {
			s.logger.Warn("authz: start MFA grace failed after binding",
				"userUUID", userUUID,
				"role", roleName,
				"error", err.Error(),
			)
		}
	}
}

func (s *Service) CreateBinding(ctx context.Context, tenantID, grantedBy string, input models.CreateBindingInput) (*models.Binding, error) {
	role, err := s.validateBindingGrant(ctx, tenantID, grantedBy, input)
	if err != nil {
		return nil, err
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
	// validateBindingGrant has already refused every grant it is going to
	// refuse, so the wrapper opens here: a 403/404/409 from the validator
	// retires nothing. userScope because a binding changes exactly one
	// user's effective permissions — the per-user counter is what keeps
	// one grant from costing every other user a cold cache. A grant is
	// gated unless the platform issued it (P24).
	//
	// The duplicate-grant path below bumps before it discovers the
	// duplicate: the gate cannot know the tuple is taken until the insert
	// answers. The cost is one retirement of that user's own verdicts for
	// a request that wrote nothing — self-inflicted at worst, since the
	// caller had to already hold the grant they are replaying.
	if err := s.bindingGrantGeneration(ctx, grantedBy, userScope(input.UserUUID), func() error {
		if err := s.repo.CreateBinding(ctx, b); err != nil {
			// authz_bindings now carries a unique (tenantId, userUUID, roleId)
			// index (see this module's CLAUDE.md + the 0009 migration). A
			// plain CreateBinding on a tuple that is already granted surfaces
			// as E11000 here rather than silently doubling the row — mapped to
			// a sentinel so the handler can answer 409 instead of leaking the
			// raw duplicate-key error. An EXPIRED incumbent is not "already
			// granted": the repository reaps it and retries, so this path is
			// reached only for a live one. Callers that want the idempotent
			// grant-or-return-existing behavior should call EnsureBinding.
			if mongo.IsDuplicateKeyError(err) {
				return ErrBindingExists
			}
			return err
		}
		return nil
	}); err != nil {
		return nil, err
	}
	s.afterBindingGrant(ctx, input.UserUUID, role.Name)
	return b, nil
}

// EnsureBinding grants the (tenantID, input.UserUUID, input.RoleUUID) tuple
// if it does not already exist, and returns the persisted row either way.
// Runs the exact same validation pipeline as CreateBinding — role active,
// system/tenant separation, cascade rule for non-"system" granters — but
// persists via the concurrent-safe repo.EnsureBinding instead of a plain
// insert, so calling it more than once (a lost response, a crashed setup
// executor, an expired lease — see the tier-1 default-tenant-setup design)
// is safe. Never overwrites an existing row: its uuid, grantedBy, grantedAt
// and expiresAt all belong to whichever caller won the race. The
// OwnerRoleBinder hook (module.go) uses this, not CreateBinding, for
// exactly that reason.
func (s *Service) EnsureBinding(ctx context.Context, tenantID, grantedBy string, input models.CreateBindingInput) (*models.Binding, error) {
	role, err := s.validateBindingGrant(ctx, tenantID, grantedBy, input)
	if err != nil {
		return nil, err
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
	// Same placement and the same actor split as CreateBinding: after the
	// shared validation, around the persist. The upsert may reuse an
	// existing row rather than write one, and the bump runs either way —
	// an INCR on a user's own counter is cheap, and a replay that skipped
	// it could leave a verdict cached from before the original grant.
	//
	// The OwnerRoleBinder hook reaches this with grantedBy == "system", so
	// it takes the write-then-report shape: CreateTenant treats an error
	// from here as a failed creation and soft-deletes the tenant it just
	// made, and a transient INCR failure must not do that (P24).
	var out *models.Binding
	if err := s.bindingGrantGeneration(ctx, grantedBy, userScope(input.UserUUID), func() error {
		var err error
		out, err = s.repo.EnsureBinding(ctx, b)
		return err
	}); err != nil {
		return nil, err
	}
	s.afterBindingGrant(ctx, input.UserUUID, role.Name)
	return out, nil
}

func (s *Service) ListBindings(ctx context.Context, tenantID string) ([]models.Binding, error) {
	return s.repo.ListBindingsByTenant(ctx, tenantID)
}

// DeleteBinding removes a role binding, scoped to the acting tenant. The repo
// filters on (uuid, tenantId) and returns ErrNotFound when nothing matches, so
// a member of tenant A cannot revoke a binding in tenant B by UUID. Global /
// system-role bindings (tenantId=="") are not tenant-manageable and never match
// a non-empty tenant scope.
//
// A revocation, so write-then-report (D27 as amended by P22): the row
// goes first and the retirement follows. That ordering is also what
// keeps a miss free — the repo reports ErrNotFound, the closure returns
// it, and nothing is retired, so a caller cannot flush every user's
// verdicts by revoking UUIDs that do not exist.
func (s *Service) DeleteBinding(ctx context.Context, tenantID, uuid string) error {
	return s.writeThenInvalidate(ctx, globalScope(), func() error {
		return s.repo.DeleteBinding(ctx, tenantID, uuid)
	})
}

// RemoveBindingsByTenant drops every binding scoped to the given tenant
// and retires the effective-permission cache so any in-flight request can
// no longer consult a cached entry pointing at a now-deleted tenant.
// Called by the cascade hook the authz module registers on the tenant
// service. Returns the number of bindings removed for audit purposes.
//
// A cascade is a revocation, so it takes the write-then-report shape,
// and it could not take the gate's even if the direction argument did
// not settle it: how many rows the cascade affects is known only from
// the write, and the "nothing removed, nothing retired" guard below is a
// pinned contract (TestRemoveBindingsByTenant_NoMatch_DoesNotFlush).
//
// The bump failure is NOT returned. The post-delete hook that calls this
// runs inside the tenant module's delete flow; an error there is audited
// as tenant.cascade.hook_failed, which would be a false signal for a
// cascade that did remove its rows and only failed to retire a cache.
func (s *Service) RemoveBindingsByTenant(ctx context.Context, tenantUUID string) (int64, error) {
	n, err := s.repo.DeleteBindingsByTenant(ctx, tenantUUID)
	if err != nil {
		return 0, err
	}
	if n > 0 {
		s.invalidateAfterWrite(ctx, globalScope())
	}
	return n, nil
}

// RemoveBindingsByUserAndTenant drops every binding for one (user, tenant)
// pair and retires the effective-permission cache. Wired into the tenant
// module's member-unbind hook (SetMemberUnbinder) so removing a member or
// changing their tenant role never leaves a stale binding that keeps granting
// the old role's permissions. Returns the number of bindings removed.
//
// Same shape and the same reasons as RemoveBindingsByTenant above, with
// one more: tenant.SetMemberRoles calls this and then re-binds the new
// role. Returning a cache error here would abort between the two, and
// the member would be left unbound while the membership denorm still
// says they hold the role.
//
// It keeps the global scope this method has always used rather than
// narrowing to userScope(userUUID) — a narrowing is available and
// cheaper, but it is a behaviour change no caller asked for, so it
// belongs in its own commit.
func (s *Service) RemoveBindingsByUserAndTenant(ctx context.Context, userUUID, tenantUUID string) (int64, error) {
	n, err := s.repo.DeleteBindingsByUserAndTenant(ctx, userUUID, tenantUUID)
	if err != nil {
		return 0, err
	}
	if n > 0 {
		s.invalidateAfterWrite(ctx, globalScope())
	}
	return n, nil
}

// --- Cache ---
//
// The key carries two generation counters — a global one and a per-user
// one — so invalidation is a single atomic INCR rather than a KEYS scan
// followed by a DEL.
//
// The scan version had four problems: it enumerated keys on the hot
// path, it could partially fail leaving some verdicts live, it raced a
// concurrent read that repopulated between the scan and the delete, and
// its glob was built from a request body (the audit's L-11). An entry
// written under an older generation simply becomes unreachable and dies
// on its own 60s TTL — nothing has to find it to retire it.
//
// Both counters are read on EVERY cache read. Memoising them in the
// process would defeat the whole mechanism: a replica holding a stale
// generation would keep serving verdicts another replica already
// retired.

const (
	// authzGlobalGenKey counts flushes that affect every user.
	authzGlobalGenKey = "authz:gen"
	// authzUserGenPrefix + userUUID counts flushes for one user.
	authzUserGenPrefix = "authz:gen:"
	// authzCacheTTL bounds both how long a live verdict is served and
	// how long a retired entry lingers in Redis before expiring.
	authzCacheTTL = 60 * time.Second
)

// MultiGetRedisClient is the narrow optional extension the
// generation-keyed cache needs on top of module.RedisClient: one MGET
// that reads both generation counters in a single round trip.
//
// It is deliberately NOT a new method on module.RedisClient.
// module.RedisClient is an SDK contract a fork's own client type may
// implement, so adding a method to it is a breaking change for every
// fork. This mirrors AtomicTakeRedisClient in auth/services: declare
// the extension where it is consumed, assert for it once at
// construction, and degrade cleanly when a client does not have it.
type MultiGetRedisClient interface {
	module.RedisClient
	MGet(ctx context.Context, keys ...string) ([]interface{}, error)
}

// setRedis wires the cache client and, once, resolves its optional MGET
// extension. The assertion happens here rather than per call so the hot
// path costs nothing, and so a client that lacks MGET is reported once
// at boot instead of silently on every request.
//
// Without MGET the cache is bypassed entirely — no read and no write.
// That is correct, only slower: every check resolves from MongoDB,
// which is the fresh answer. Simulating MGET with two GETs is not an
// option, because the two counters could then be read at two different
// moments and compose a key that never existed.
func (s *Service) setRedis(client module.RedisClient, logger *slog.Logger) {
	s.redis = client
	s.mget = nil
	if client == nil {
		return
	}
	mg, ok := client.(MultiGetRedisClient)
	if !ok {
		if logger != nil {
			logger.Warn("authz: redis client has no MGet — the effective-permission cache is disabled and every check resolves from MongoDB",
				slog.String("remedy", "implement MGet(ctx, keys ...string) ([]interface{}, error) on the client passed as authz Config.Redis"))
		}
		return
	}
	s.mget = mg
}

// generations reads both counters in ONE MGET. A failure — or a client
// with no MGET at all — returns ok=false and the caller treats it as a
// cache miss: going to Mongo is the fresh answer, so a degraded Redis
// costs latency, never correctness.
func (s *Service) generations(ctx context.Context, userUUID string) (global, user int64, ok bool) {
	if s.redis == nil || s.mget == nil {
		return 0, 0, false
	}
	vals, err := s.mget.MGet(ctx, authzGlobalGenKey, authzUserGenPrefix+userUUID)
	if err != nil {
		return 0, 0, false
	}
	return parseGen(vals, 0), parseGen(vals, 1), true
}

// parseGen reads one MGET slot as a counter. A missing key (nil slot)
// or an unparseable value reads as generation 0 — the same value a
// never-bumped counter has, so a fresh deployment and a corrupted
// counter both simply mean "nothing retired yet".
func parseGen(vals []interface{}, i int) int64 {
	if i >= len(vals) || vals[i] == nil {
		return 0
	}
	var raw string
	switch v := vals[i].(type) {
	case string:
		raw = v
	case []byte:
		raw = string(v)
	case int64:
		return v
	default:
		return 0
	}
	n, err := strconv.ParseInt(raw, 10, 64)
	if err != nil {
		return 0
	}
	return n
}

// cacheKeyAt composes the key for a known pair of generations.
func cacheKeyAt(global, user int64, userUUID, tenantID string) string {
	if tenantID == "" {
		tenantID = "-"
	}
	return fmt.Sprintf("authz:cache:%d:%s:%d:%s", global, userUUID, user, tenantID)
}

// cacheKey folds both generations in. A missing counter reads as 0.
// Used by tests and by callers that do not already hold the
// generations; cacheGet and cacheSet read them once and call
// cacheKeyAt so a single operation is one MGET plus one GET/SET.
func (s *Service) cacheKey(ctx context.Context, userUUID, tenantID string) string {
	g, u, _ := s.generations(ctx, userUUID)
	return cacheKeyAt(g, u, userUUID, tenantID)
}

// cacheGetAt and cacheSetAt take a generation pair the caller already
// holds. GetEffectivePermissions reads the pair once and uses these two,
// so its read key and its write key come from the same instant — see the
// comment at the top of that function for why that matters.
func (s *Service) cacheGetAt(ctx context.Context, global, user int64, userUUID, tenantID string) ([]string, bool) {
	if s.redis == nil {
		return nil, false
	}
	raw, err := s.redis.Get(ctx, cacheKeyAt(global, user, userUUID, tenantID))
	if err != nil || raw == "" {
		return nil, false
	}
	var out []string
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		return nil, false
	}
	return out, true
}

func (s *Service) cacheSetAt(ctx context.Context, global, user int64, userUUID, tenantID string, perms []string) {
	if s.redis == nil {
		return
	}
	data, err := json.Marshal(perms)
	if err != nil {
		return
	}
	_ = s.redis.Set(ctx, cacheKeyAt(global, user, userUUID, tenantID), string(data), authzCacheTTL)
}

// cacheGet and cacheSet read the generations themselves. Convenience
// forms for callers that hold no pair; the hot path uses the *At forms.
func (s *Service) cacheGet(ctx context.Context, userUUID, tenantID string) ([]string, bool) {
	g, u, ok := s.generations(ctx, userUUID)
	if !ok {
		return nil, false
	}
	return s.cacheGetAt(ctx, g, u, userUUID, tenantID)
}

func (s *Service) cacheSet(ctx context.Context, userUUID, tenantID string, perms []string) {
	g, u, ok := s.generations(ctx, userUUID)
	if !ok {
		// The generations could not be read, so there is no key this
		// entry could be filed under that a reader would find. Writing
		// under a guessed generation would publish a verdict nobody can
		// retire; skip the write instead.
		return
	}
	s.cacheSetAt(ctx, g, u, userUUID, tenantID, perms)
}

// InvalidateUserPermissions implements iface.AuthzCacheInvalidator: one
// atomic INCR of the user's generation. Every entry written under the
// previous value becomes unreachable at once, in every tenant, with no
// scan and no glob built from caller input.
//
// It retires everything cached BEFORE the call. It does not by itself
// cover a reader that is mid-flight across it: GetEffectivePermissions
// files its result under the generation pair it read the cache with, so
// a verdict computed before this bump is born unreachable — but a reader
// that resolved Mongo across the bump can still publish a stale entry.
// That residue is what withGeneration's post-write bump exists for. The
// two mechanisms are complementary: this one is deterministic for the
// readers it covers, the post-write bump is best-effort for the rest.
//
// A nil Redis is a no-op SUCCESS: "no cache configured" is not "cache
// unavailable" — there is no cached verdict to retire, so reporting
// failure would refuse role edits on a deployment that never had the
// hazard. A configured Redis that cannot be bumped DOES return an
// error, and the caller decides what that means (D27).
//
// There is a third state between those two: a client with no MGET, where
// this replica bypasses the cache but a peer replica may not. The bump is
// issued there too — the counter is shared state, and no replica can know
// what its peers are running.
func (s *Service) InvalidateUserPermissions(ctx context.Context, userUUID string) error {
	if s.redis == nil {
		return nil
	}
	if _, err := s.redis.Incr(ctx, authzUserGenPrefix+userUUID); err != nil {
		return fmt.Errorf("authz: invalidate user permissions: %w", err)
	}
	return nil
}

// flushCache retires EVERY user's entries with one INCR of the global
// generation. Used by role update/delete, binding delete and the tenant
// cascades, where the set of affected users is not enumerable cheaply.
// Same nil-Redis contract as InvalidateUserPermissions.
func (s *Service) flushCache(ctx context.Context) error {
	if s.redis == nil {
		return nil
	}
	if _, err := s.redis.Incr(ctx, authzGlobalGenKey); err != nil {
		return fmt.Errorf("authz: flush cache: %w", err)
	}
	return nil
}

// --- The invalidation contract (D27) ---

// generationScope names which counter a mutation retires: one user's, or
// — when the affected user set is not cheaply enumerable — everyone's.
type generationScope struct {
	user string // "" means the global counter
}

// userScope retires one user's cached verdicts, in every tenant.
func userScope(userUUID string) generationScope { return generationScope{user: userUUID} }

// globalScope retires every user's cached verdicts. Used where the set
// of affected users cannot be enumerated without a scan: a role's
// permissions changed, a role was deleted, a binding was revoked.
func globalScope() generationScope { return generationScope{} }

// bumpGeneration retires the verdicts named by scope. The single seam
// every invalidation goes through, so a caller can never bump one
// counter on the way in and the other on the way out.
func (s *Service) bumpGeneration(ctx context.Context, scope generationScope) error {
	if scope.user != "" {
		return s.InvalidateUserPermissions(ctx, scope.user)
	}
	return s.flushCache(ctx)
}

// scopeAttr names the scope in a log line. globalScope carries no user,
// so it must not emit an empty user_uuid.
func scopeAttr(scope generationScope) slog.Attr {
	if scope.user != "" {
		return slog.String("user_uuid", scope.user)
	}
	return slog.String("scope", "global")
}

// D27 (as amended by ruling P22) splits mutations by DIRECTION, because
// a stale verdict does not mean the same thing in both:
//
//   - after a GRANT it is a DENY. The new privilege is late, never
//     wrongly held, so refusing the write costs nothing but a retry.
//   - after a REVOCATION it is an ALLOW. Refusing leaves the privilege
//     granted INDEFINITELY, where writing leaves it for at most the 60s
//     TTL — and with Redis fully down the cache is bypassed on reads, so
//     a written revocation takes effect immediately. Refusal is strictly
//     worse in exactly the case the gate was meant to protect.
//
// So grants go through withGeneration (gate), revocations and cascades
// through writeThenInvalidate (write-then-report). Platform-issued
// grants — the granterSystem sentinel — take the second shape too
// (ruling P24): they are internal steps of other modules' multi-step
// flows, and refusing one leaves a tenant half-created or a member
// unbound from a role their membership denorm still shows.

// invalidateAfterWrite retires the verdicts a write has ALREADY made
// stale. The failure is logged and counted, never returned: the row is
// gone (or written), so failing the call would report an outcome that
// did not happen and invite a retry of a change that already landed.
// This is the "surfaced, never a refusal" half of D27.
func (s *Service) invalidateAfterWrite(ctx context.Context, scope generationScope) {
	err := s.bumpGeneration(ctx, scope)
	if err == nil {
		return
	}
	metrics.Default().RecordAuthzCacheInvalidationFailure()
	if s.logger != nil {
		s.logger.ErrorContext(ctx, "authz: post-write cache invalidation failed; a verdict already cached may survive up to its TTL",
			scopeAttr(scope),
			slog.String("error", err.Error()))
	}
}

// withGeneration wraps a GRANT in pre-invalidate → write →
// post-invalidate.
//
// The PRE step is a GATE: a generation the store cannot bump means the
// change's effect cannot be guaranteed, so the change is REFUSED
// (ErrAuthzCacheUnavailable, 503) and nothing is written. Redis being
// unavailable already stops sessions, MFA challenges and OAuth state;
// refusing a new grant in that state is consistent, and the retry is the
// admin's. A deployment with no cache at all is not that state — there
// the bump is a no-op success and the mutation proceeds.
//
// The POST step covers the race the pre-bump cannot: a read that started
// before the pre-invalidation and resolved Mongo after the write
// publishes the OLD verdict under the NEW generation. It is complementary
// to the read-side fix in GetEffectivePermissions (which files a verdict
// under the generation pair it READ with, so readers that crossed the
// bump earlier are covered deterministically), not a substitute for it.
// Its failure is logged and counted but NOT fatal: the write has landed.
//
// Call it INSIDE the method, after that method's own validation, and
// only on the path that actually writes. Wrapping a whole method bumps
// the generation before its guards run, which turns a refused 403/404
// request into a remotely triggerable cache flush.
//
// Cost, stated honestly: a successful gated mutation issues TWO bumps.
// Under userScope that is two INCRs on one user's counter. Under
// globalScope it is two platform-wide retirements — the post-bump throws
// away whatever the fleet repopulated in the window, so one role edit
// costs two repopulation waves, not one extra round trip. UpdateRole is
// the only globalScope gate left after P22.
func (s *Service) withGeneration(ctx context.Context, scope generationScope, mutate func() error) error {
	if err := s.bumpGeneration(ctx, scope); err != nil {
		// The refusal is the operator-visible event: permission changes
		// are being turned away. Log and count it here, where the cause
		// exists — the handler renders a fixed 503 detail and has none.
		metrics.Default().RecordAuthzCacheInvalidationRefusal()
		if s.logger != nil {
			s.logger.ErrorContext(ctx, "authz: refusing a permission grant — the effective-permission cache could not be retired, so the change was not written",
				scopeAttr(scope),
				slog.String("error", err.Error()))
		}
		return fmt.Errorf("%w: %v", ErrAuthzCacheUnavailable, err)
	}
	if err := mutate(); err != nil {
		return err
	}
	s.invalidateAfterWrite(ctx, scope)
	return nil
}

// writeThenInvalidate wraps a REVOCATION (or a platform-issued grant):
// write first, then retire. A bump failure never refuses the write — see
// the direction argument above — it is logged and counted by
// invalidateAfterWrite and the caller is told the truth, which is that
// the change landed.
//
// It also removes a hazard the gate has here: with the bump first, a
// revocation that matches nothing still retires the cache, so a 404 with
// no write behind it would flush every user's verdicts at request rate.
// Writing first means only a real hit invalidates.
func (s *Service) writeThenInvalidate(ctx context.Context, scope generationScope, mutate func() error) error {
	if err := mutate(); err != nil {
		return err
	}
	s.invalidateAfterWrite(ctx, scope)
	return nil
}

// bindingGrantGeneration picks the shape a binding grant takes. A grant
// requested by a real actor is gated; one issued by the platform
// sentinel is not (ruling P24) — those are internal steps of the tenant
// module's own flows (CreateTenant's owner binding, SetMemberRoles'
// rebind), and a refusal there does not surface as a 503 to anyone. It
// surfaces as a half-finished tenant.
func (s *Service) bindingGrantGeneration(ctx context.Context, grantedBy string, scope generationScope, mutate func() error) error {
	if grantedBy == granterSystem {
		return s.writeThenInvalidate(ctx, scope, mutate)
	}
	return s.withGeneration(ctx, scope, mutate)
}

// --- helpers ---

// isPermissionSuperset reports whether next covers every key in prev —
// i.e. the patch takes nothing away. Used by UpdateRole to route a role
// edit by direction (P25). Both lists are already in hand: prev is the
// loaded role, next is the validator's cleaned output, so the test costs
// no round trip.
//
// The wildcard needs no special case: "*" is refused in a custom role by
// the D21 validator before this is reached, and a system role cannot
// have its permissions patched at all.
func isPermissionSuperset(next, prev []string) bool {
	if len(prev) == 0 {
		return true
	}
	have := make(map[string]struct{}, len(next))
	for _, p := range next {
		have[p] = struct{}{}
	}
	for _, p := range prev {
		if _, ok := have[p]; !ok {
			return false
		}
	}
	return true
}

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
