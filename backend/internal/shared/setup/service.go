// Package setup drives the first-install onboarding flow.
//
// The package exposes two public HTTP endpoints (`GET /v1/setup/status` and
// `POST /v1/setup/admin`) that the frontend wizard consumes while a fresh
// Orkestra deployment has not finished bootstrapping. Setup progresses
// through three persistent phases — admin_required, tenant_required,
// complete — backed by the systeminit.FinalizationStore coordinator
// record rather than the old "at least one user exists" heuristic.
//
// Once any user exists, `POST /v1/setup/admin` is refused with 409. The
// wizard gates itself on `GET /v1/setup/status` so operators are never
// routed back into it after the first admin is created.
package setup

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	authModels "github.com/orkestra/backend/internal/core/auth/models"
	"github.com/orkestra/backend/internal/shared/systeminit"
	"github.com/orkestra/backend/pkg/sdk/iface"
	"github.com/orkestra/backend/pkg/sdk/module"
)

// ErrAlreadyCompleted is returned by CreateInitialAdmin when at least one
// user already exists. The HTTP layer maps it to 409 Conflict.
var ErrAlreadyCompleted = errors.New("setup already completed")

// AdminCreator is the narrow contract the setup service requires from the
// auth module. *services.PasswordAuthService satisfies it structurally via
// RegisterInitialAdmin — defining it here (rather than importing the auth
// type) keeps shared/setup free of cross-package coupling.
type AdminCreator interface {
	RegisterInitialAdmin(ctx context.Context, email, password, fullName, ip string) (*authModels.TokenResponse, error)
}

// Setup phases. Persistent and authoritative — see Service.Status. The
// legacy SetupCompleted field on Status is derived from PhaseComplete,
// never computed independently.
const (
	PhaseAdminRequired  = "admin_required"
	PhaseTenantRequired = "tenant_required"
	PhaseComplete       = "complete"
)

// Status is the payload returned by GET /v1/setup/status.
type Status struct {
	// SetupCompleted is derived from Phase == PhaseComplete; kept on the
	// wire for backward compatibility with clients written against the
	// old "at least one user exists" contract.
	SetupCompleted bool   `json:"setupCompleted"`
	Phase          string `json:"phase"`
	SMTPConfigured bool   `json:"smtpConfigured"`
}

// Service owns the two setup endpoints' business logic.
type Service struct {
	users         iface.UserProvider
	admin         AdminCreator
	store         systeminit.FinalizationStore
	configService *module.ModuleConfigService
	logger        *slog.Logger

	// lifecycle is the narrow iface.UserLifecycleStateProvider capability of
	// users, resolved once via type assertion at construction time. It
	// backs evaluateAccess's usable/recovery classification (finalizer
	// access probe + finalize POST). Deliberately NOT a widening of
	// iface.UserProvider itself — see that interface's doc comment in
	// pkg/sdk/iface/interfaces.go. nil when the wired UserProvider doesn't
	// implement it; evaluateAccess then fails closed rather than panicking.
	lifecycle iface.UserLifecycleStateProvider
}

// NewService wires the setup service. users, admin and store are required;
// cfg may be nil (SMTP status degrades to false); a nil logger falls back
// to slog.Default(). If users also implements iface.UserLifecycleStateProvider
// (the canonical user-module service does), Service.lifecycle is derived
// from it automatically.
func NewService(users iface.UserProvider, admin AdminCreator, store systeminit.FinalizationStore, cfg *module.ModuleConfigService, logger *slog.Logger) *Service {
	if logger == nil {
		logger = slog.Default()
	}
	svc := &Service{
		users:         users,
		admin:         admin,
		store:         store,
		configService: cfg,
		logger:        logger,
	}
	if lc, ok := users.(iface.UserLifecycleStateProvider); ok {
		svc.lifecycle = lc
	}
	return svc
}

// refreshTTLProvider is the optional capability the setup handler needs
// from whatever creates the first admin: the refresh-token lifetime, so
// the cookie it emits matches the one POST /v1/auth/login emits.
// Satisfied by *auth/services.PasswordAuthService.
type refreshTTLProvider interface {
	RefreshTokenTTL() time.Duration
}

// RefreshTokenTTL returns the deployment's refresh-token lifetime, or
// the legacy 7-day default when the creator does not expose one.
func (s *Service) RefreshTokenTTL() time.Duration {
	if p, ok := s.admin.(refreshTTLProvider); ok {
		if d := p.RefreshTokenTTL(); d > 0 {
			return d
		}
	}
	return 7 * 24 * time.Hour
}

// Status reports the authoritative setup phase plus the non-authoritative
// SMTP-configured hint.
//
// Fail-closed is deliberate: a failure to read either the operator user
// count or the finalization coordinator record returns (Status{}, err) —
// never an inferred phase. A caller must not be able to conclude "setup
// is incomplete" from a database outage, because that is exactly the
// state that unlocks the unauthenticated bootstrap paths (POST
// /v1/setup/admin). SMTPConfigured is different: it controls nothing
// about phase, authorization, or tenant creation, so it may still
// degrade to false on its own read failure (see isSMTPConfigured).
func (s *Service) Status(ctx context.Context) (Status, error) {
	count, err := s.users.GetUserCount(ctx, nil)
	if err != nil {
		return Status{}, fmt.Errorf("setup: read operator user count: %w", err)
	}
	if count == 0 {
		return Status{Phase: PhaseAdminRequired, SMTPConfigured: s.isSMTPConfigured(ctx)}, nil
	}
	rec, err := s.store.Get(ctx)
	if err != nil {
		return Status{}, fmt.Errorf("setup: read finalization coordinator: %w", err)
	}
	phase := PhaseTenantRequired
	if rec != nil && rec.CompletedAt != nil {
		phase = PhaseComplete
	}
	return Status{
		SetupCompleted: phase == PhaseComplete,
		Phase:          phase,
		SMTPConfigured: s.isSMTPConfigured(ctx),
	}, nil
}

// isSMTPConfigured returns true when the notification module has a non-noop
// provider with at least an SMTP host. Anything less means verification and
// password-reset mail would silently drop.
func (s *Service) isSMTPConfigured(ctx context.Context) bool {
	if s.configService == nil {
		return false
	}
	cfg, err := s.configService.GetConfig(ctx, "notification")
	if err != nil || cfg == nil {
		return false
	}
	provider := strings.TrimSpace(cfg.ConfigValues["email.provider"])
	host := strings.TrimSpace(cfg.ConfigValues["email.smtp.host"])
	return provider != "" && provider != "noop" && host != ""
}

// CreateInitialAdmin creates the first administrator account. It refuses
// with ErrAlreadyCompleted if any user already exists. On success it
// returns the full TokenResponse so the handler can set the refresh cookie
// and return the access token.
func (s *Service) CreateInitialAdmin(ctx context.Context, email, password, fullName, ip string) (*authModels.TokenResponse, error) {
	count, err := s.users.GetUserCount(ctx, nil)
	if err != nil {
		return nil, err
	}
	if count > 0 {
		return nil, ErrAlreadyCompleted
	}
	tokens, err := s.admin.RegisterInitialAdmin(ctx, email, password, fullName, ip)
	if err != nil {
		return nil, err
	}

	// The initial admin is a super_admin whose authority is a system role,
	// independent of any tenant. We deliberately do NOT create an internal
	// tenant here — tenant creation is an explicit operator choice in the
	// setup wizard's OrgStep (or skipped, leaving the platform at zero
	// tenants). See docs/superpowers/specs/2026-07-06-zero-tenant-setup-design.md.
	return tokens, nil
}

// --- Finalizer access probe / shared authorization seam ---
//
// FinalizationAccess and evaluateAccess back GET /v1/setup/finalization-access
// (this task) and, unchanged, the finalize POST's authorization decision
// (Task 5.5). See the "Finalizer access probe" section of
// docs/superpowers/specs/2026-08-23-tier1-default-tenant-setup-design.md.

// FinalizationAccess is the payload returned by GET
// /v1/setup/finalization-access. It carries exactly these three fields —
// on purpose: the caller evaluating "may I finalize?" may have no right to
// know who currently holds the binding, so the bound administrator's UUID,
// email, name, and lifecycle state must never appear here or in any error
// string this package returns.
type FinalizationAccess struct {
	CanFinalize      bool   `json:"canFinalize"`
	CanClaimRecovery bool   `json:"canClaimRecovery"`
	Reason           string `json:"reason"` // "", reasonBoundToAnotherAdmin, reasonRecoveryRequiresSuperAdmin
}

// Reason values carried on FinalizationAccess.Reason — the wire contract.
// Empty string means "no reason needed": either CanFinalize or
// CanClaimRecovery is true.
const (
	reasonBoundToAnotherAdmin        = "bound_to_another_admin"
	reasonRecoveryRequiresSuperAdmin = "recovery_requires_super_admin"
)

// roleSuperAdmin is the one system role allowed to claim recovery of an
// empty or unusable finalization binding. Matches the "super_admin" wire
// value used across auth/authz (e.g. shared/middleware/jwt_validator.go).
const roleSuperAdmin = "super_admin"

// evaluateAccess is the shared authorization seam for the read-only
// finalizer-access probe (this task) and the finalize POST's authorization
// (Task 5.5). It NEVER mutates the coordinator record — only Get is called
// — and never exposes the bound administrator's identity; callers translate
// the returned FinalizationAccess into an HTTP response (the probe) or an
// authorization decision (the POST, which performs the atomic claim this
// function deliberately does not).
//
// A user/coordinator lookup failure is returned as an error, never folded
// into a lifecycle class or an authorization outcome — the caller must map
// it to 503 setup.finalizer_state_unavailable. This is the load-bearing
// fail-closed rule: treating "we couldn't tell" as "the binding is gone"
// would let any operator wait out a transient database blip and then claim
// an in-progress setup.
//
// Logic: a nil record or empty rec.AdminUUID is treated as an empty
// binding (a legacy record from before this feature existed). When the
// binding is non-empty, its lifecycle state is resolved once; `active`
// means usable — the caller may finalize only if they ARE the bound
// admin, otherwise they're told the binding belongs to someone else.
// Every other state (and an empty binding) falls through to the recovery
// check: the caller must be an authenticated, active super_admin.
func (s *Service) evaluateAccess(ctx context.Context, callerUUID, callerSystemRole string) (FinalizationAccess, *systeminit.FinalizationRecord, error) {
	rec, err := s.store.Get(ctx)
	if err != nil {
		return FinalizationAccess{}, nil, fmt.Errorf("setup: read finalization coordinator: %w", err)
	}

	boundUUID := ""
	if rec != nil {
		boundUUID = rec.AdminUUID
	}

	if boundUUID != "" {
		state, err := s.userLifecycleState(ctx, boundUUID)
		if err != nil {
			return FinalizationAccess{}, nil, err
		}
		if state == iface.UserLifecycleActive {
			if callerUUID != "" && callerUUID == boundUUID {
				return FinalizationAccess{CanFinalize: true}, rec, nil
			}
			return FinalizationAccess{Reason: reasonBoundToAnotherAdmin}, rec, nil
		}
		// Bound admin is missing/deleted/inactive: falls through to the
		// recovery-eligibility check below, exactly like an empty binding.
	}

	// Binding is empty, or the bound administrator is unusable: only an
	// authenticated, active super_admin may claim recovery. The role check
	// runs first so a non-super_admin caller never triggers a lifecycle
	// lookup on themselves.
	if callerSystemRole != roleSuperAdmin {
		return FinalizationAccess{Reason: reasonRecoveryRequiresSuperAdmin}, rec, nil
	}
	callerState, err := s.userLifecycleState(ctx, callerUUID)
	if err != nil {
		return FinalizationAccess{}, nil, err
	}
	if callerState != iface.UserLifecycleActive {
		return FinalizationAccess{Reason: reasonRecoveryRequiresSuperAdmin}, rec, nil
	}
	return FinalizationAccess{CanClaimRecovery: true}, rec, nil
}

// userLifecycleState resolves userUUID's lifecycle class through the
// narrow provider derived at construction time. A nil provider (the wired
// UserProvider doesn't implement iface.UserLifecycleStateProvider) is a
// wiring defect, not a lifecycle fact — it fails closed exactly like a
// database error, never as iface.UserLifecycleMissing or any other state.
func (s *Service) userLifecycleState(ctx context.Context, userUUID string) (iface.UserLifecycleState, error) {
	if s.lifecycle == nil {
		return "", errors.New("setup: user lifecycle provider not configured")
	}
	state, err := s.lifecycle.UserLifecycleState(ctx, userUUID)
	if err != nil {
		return "", fmt.Errorf("setup: resolve user lifecycle state: %w", err)
	}
	return state, nil
}
