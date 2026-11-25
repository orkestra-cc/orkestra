package auth

import (
	"log/slog"
	"net/http"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humachi"
	"github.com/go-chi/chi/v5"
	"github.com/orkestra/backend/internal/core/auth/handlers"
	"github.com/orkestra/backend/internal/core/auth/repository"
	"github.com/orkestra/backend/internal/core/auth/services"
	"github.com/orkestra/backend/pkg/sdk/iface"
	"github.com/orkestra/backend/pkg/sdk/module"
)

// pinDeviceIndexes is shared by operator_pin_devices and client_pin_devices.
func pinDeviceIndexes() []module.IndexSpec {
	return []module.IndexSpec{
		{Keys: map[string]int{"uuid": 1}, Unique: true},
		{OrderedKeys: []module.IndexKey{{Field: "userUuid", Direction: 1}, {Field: "deviceId", Direction: 1}}, Unique: true},
	}
}

// buildPINService wires one tier's PIN service. The credential store is
// resolved from the tier's user provider by type assertion
// (iface.PINCredentialStore, additive): a fork's provider that predates
// it leaves the method unavailable, with one WARN, rather than failing
// boot. Session revocation and the audit rows go through the tier's
// authService, which implements both seams.
func buildPINService(
	bundle *authTierBundle,
	users iface.UserProvider,
	actors iface.UserProvider,
	devices repository.PINDeviceRepository,
	codes services.PINEnrollmentCodeStore,
	passwords services.PasswordService,
	policy *services.AuthPolicyService,
	attempts services.AttemptCounter,
	authz services.PINPermissionChecker,
	logger *slog.Logger,
) *services.PINAuthService {
	store, ok := users.(iface.PINCredentialStore)
	if !ok {
		logger.Warn("auth: user provider lacks iface.PINCredentialStore; PIN sign-in is unavailable on this tier",
			slog.String("tier", string(bundle.tier)))
		store = nil
	}
	revoker, _ := bundle.authService.(services.PINSessionRevoker)
	events, _ := bundle.authService.(services.PINEventRecorder)
	svc := services.NewPINAuthService(services.PINAuthConfig{
		Login:     bundle.passwordSvc,
		Store:     store,
		Users:     users,
		Actors:    actors,
		Devices:   devices,
		Codes:     codes,
		Passwords: passwords,
		Policy:    policy,
		Audience:  bundle.policyAudience,
		Attempts:  attempts,
		Authz:     authz,
		Sessions:  revoker,
		Events:    events,
		Logger:    logger,
	})
	// Every refresh of a PIN session re-validates it against this service.
	if c, ok := bundle.authService.(interface {
		SetPINSessionChecker(services.PINSessionChecker)
	}); ok {
		c.SetPINSessionChecker(svc)
	}
	return svc
}

// registerPINRoutes mounts one surface's public and self-service PIN
// routes. Gates:
//   - sign-in and device enrollment: public (the service enforces the
//     surface toggle, the lockout and the device binding);
//   - GET /me/pin: RequireGlobal;
//   - PUT /me/pin: RequireGlobal + RequireStepUp(5m) +
//     RequireSystemPermission("auth.pin_login") — a credential change, and
//     only for a user allowed to hold a PIN (global scope, as the service
//     checks it);
//   - DELETE /me/pin and DELETE /me/pin/devices/{id}: RequireGlobal +
//     RequireStepUp(5m), no permission (removing is always allowed);
//   - POST /me/pin/enrollment-code: RequireGlobal + the enrolment-proof
//     gate (a fresh real sign-in or second factor; never a PIN session) +
//     the permission.
//
// gatePermission is false on the client surface: the authz evaluator
// resolves system roles from operator users only, so a client user's
// auth.pin_login is unanswerable there and the gate would refuse every
// client user. The service's eligibility rule (role ceiling, no
// privileged membership, PIN set by an administrator) still applies.
//
// The service refuses a PIN session on every self route as well, so a
// gate misconfiguration cannot turn a PIN session into a credential
// manager.
func (m *AuthModule) registerPINRoutes(public huma.API, protected chi.Router, mw module.RoleMiddleware, cfg huma.Config,
	h *handlers.PINHandler, mount handlers.RouteMount, enrolmentProof func(http.Handler) http.Handler, gatePermission bool) {
	if h == nil {
		return
	}
	h.RegisterPublicRoutes(public, mount)
	protected.Group(func(r chi.Router) {
		r.Use(mw.RequireGlobal())
		h.RegisterSelfReadRoutes(humachi.New(r, cfg), mount)
	})
	protected.Group(func(r chi.Router) {
		r.Use(mw.RequireGlobal())
		r.Use(mw.RequireStepUp(5 * time.Minute))
		if gatePermission {
			r.Use(mw.RequireSystemPermission("auth.pin_login"))
		}
		h.RegisterSelfSetRoute(humachi.New(r, cfg), mount)
	})
	protected.Group(func(r chi.Router) {
		r.Use(mw.RequireGlobal())
		r.Use(mw.RequireStepUp(5 * time.Minute))
		h.RegisterSelfRemoveRoutes(humachi.New(r, cfg), mount)
	})
	protected.Group(func(r chi.Router) {
		r.Use(mw.RequireGlobal())
		r.Use(enrolmentProof)
		if gatePermission {
			r.Use(mw.RequireSystemPermission("auth.pin_login"))
		}
		h.RegisterSelfEnrolmentRoutes(humachi.New(r, cfg), mount)
	})
}
