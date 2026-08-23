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
}

// NewService wires the setup service. users, admin and store are required;
// cfg may be nil (SMTP status degrades to false); a nil logger falls back
// to slog.Default().
func NewService(users iface.UserProvider, admin AdminCreator, store systeminit.FinalizationStore, cfg *module.ModuleConfigService, logger *slog.Logger) *Service {
	if logger == nil {
		logger = slog.Default()
	}
	return &Service{
		users:         users,
		admin:         admin,
		store:         store,
		configService: cfg,
		logger:        logger,
	}
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
