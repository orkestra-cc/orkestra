package handlers

import (
	"context"
	"log/slog"

	"github.com/orkestra/backend/internal/core/auth/services"
	"github.com/orkestra/backend/internal/shared/middleware"
)

// Session termination for credential changes (spec §4.3 D16).
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
