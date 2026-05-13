// Package setup drives the first-install onboarding flow.
//
// The package exposes two public HTTP endpoints (`GET /v1/setup/status` and
// `POST /v1/setup/admin`) that the frontend wizard consumes while a fresh
// Orkestra deployment has no users yet. "Setup completed" is defined
// implicitly as `userCount > 0` — no marker collection, no feature flag.
//
// Once any user exists, `POST /v1/setup/admin` is refused with 409. The
// wizard gates itself on `GET /v1/setup/status` so operators are never
// routed back into it after the first admin is created.
package setup

import (
	"context"
	"errors"
	"log/slog"
	"strings"

	"github.com/orkestra-cc/orkestra-sdk/iface"
	"github.com/orkestra-cc/orkestra-sdk/module"
	authModels "github.com/orkestra/backend/internal/core/auth/models"
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

// Status is the payload returned by GET /v1/setup/status.
type Status struct {
	SetupCompleted bool `json:"setupCompleted"`
	SMTPConfigured bool `json:"smtpConfigured"`
}

// Service owns the two setup endpoints' business logic.
type Service struct {
	users         iface.UserProvider
	admin         AdminCreator
	configService *module.ModuleConfigService
	logger        *slog.Logger
}

// NewService wires the setup service. All three dependencies are required.
func NewService(users iface.UserProvider, admin AdminCreator, cfg *module.ModuleConfigService, logger *slog.Logger) *Service {
	if logger == nil {
		logger = slog.Default()
	}
	return &Service{
		users:         users,
		admin:         admin,
		configService: cfg,
		logger:        logger,
	}
}

// Status reports whether the system has been initialized and whether the
// notification module has real SMTP credentials.
//
// On any DB error the method fails open (both flags false) so the wizard
// can still render and the operator can try again — we'd rather show the
// wizard redundantly than lock someone out of a deployment they own.
func (s *Service) Status(ctx context.Context) Status {
	out := Status{}

	count, err := s.users.GetUserCount(ctx, nil)
	if err != nil {
		s.logger.Warn("setup.Status: GetUserCount failed, assuming not completed",
			slog.String("error", err.Error()))
	} else {
		out.SetupCompleted = count > 0
	}

	out.SMTPConfigured = s.isSMTPConfigured(ctx)
	return out
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
	return s.admin.RegisterInitialAdmin(ctx, email, password, fullName, ip)
}
