package handlers

import (
	"context"
	"log/slog"

	"github.com/orkestra/backend/internal/core/auth/services"
	"github.com/orkestra/backend/internal/shared/middleware"
	"github.com/orkestra/backend/pkg/sdk/iface"
)

// The handler-side consequences of a credential change (spec §4.3 D16):
// session termination, and restarting the MFA enrolment grace clock.
//
// This lives on the handler layer and nowhere else, because the caller's
// session id is on the request claims: neither mfaService nor
// webAuthnService can see it, and threading it into them would put
// request-scoped identity into services that have none (ruling R5).
//
// The epoch, bumped inside the services, is the mechanism that ends MFA
// authority — including in the caller's own token, which no revocation can
// reach. Revocation answers the different question of whether the other
// bearers of the account may keep using it at all. That is why a failed
// revocation is logged rather than surfaced: the security-critical half
// already happened.

// revokeSessionsExceptCurrent ends every session the user holds except the
// one this request arrived on. The caller keeps working — they are signed
// in and just proved themselves — but their token's MFA authority is gone
// at the next gated request via the epoch.
//
// Follows the RevokeAllSessions precedent in self_user_auth_handler.go: an
// absent sid (a token minted before sid stamping) revokes everything, which
// is the safe direction.
func revokeSessionsExceptCurrent(ctx context.Context, sessions services.AuthService, userUUID, trigger string) {
	if sessions == nil {
		slog.Default().Warn("auth: session terminator not wired; a credential change left other sessions signed in",
			slog.String("user_uuid", userUUID),
			slog.String("trigger", trigger))
		return
	}
	currentSid, _ := middleware.GetSessionID(ctx)
	if _, err := sessions.RevokeAllUserSessionsExcept(ctx, userUUID, currentSid); err != nil {
		slog.Default().Warn("auth: failed to revoke sessions after a credential change; the MFA epoch still ended their MFA authority",
			slog.String("user_uuid", userUUID),
			slog.String("trigger", trigger),
			slog.String("error", err.Error()))
	}
}

// resetMFAGraceClock restarts the enrolment grace window after a removal
// took the user's LAST second factor.
//
// Without it the removal is a one-way door for anyone whose role obliges
// MFA — every administrator, by default. `MFAGraceStartedAt` is stamped at
// their first privileged login and nothing in the tree ever clears it, so a
// user who removes their factor months later meets a grace window that
// lapsed long ago: `enroll/begin` answers `reauthentication_required` once
// their `auth_time` goes stale, `/me/password-confirm` refuses them (D19),
// and the fresh login they are sent to take is itself refused with
// `mfa_enrollment_required` — "privileged, no factor, grace expired"
// (`PasswordAuthService.completeLogin`). A sole administrator ends up with
// no way back in. `AdminReset` has always restarted the clock for exactly
// this reason; the self paths simply never did.
//
// Best-effort, like every other consequence on these paths: the credential
// is already gone, and failing the caller would report a completed removal
// as not having happened.
func resetMFAGraceClock(ctx context.Context, users iface.UserProvider, userUUID, trigger string) {
	if users == nil {
		slog.Default().Warn("auth: user provider not wired; a credential removal left the enrolment grace clock at its old value",
			slog.String("user_uuid", userUUID),
			slog.String("trigger", trigger))
		return
	}
	if err := users.ResetMFAGrace(ctx, userUUID); err != nil {
		slog.Default().Warn("auth: failed to restart the MFA enrolment grace window after a credential removal; an obliged user may be refused at their next login",
			slog.String("user_uuid", userUUID),
			slog.String("trigger", trigger),
			slog.String("error", err.Error()))
	}
}
