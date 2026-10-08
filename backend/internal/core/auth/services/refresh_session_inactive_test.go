package services

// Neither refresh entry point used to look at the session document. A
// session revoked by the user, an admin, a password change, a replay verdict
// or the absolute cap had its refresh rows revoked by a bulk update — but a
// rotation racing that update could insert its successor AFTER the update
// scanned, leaving a live row under a dead session that nothing would ever
// look at again. These tests pin the rule that closes it: an inactive session
// document refuses both the rotation and the read-only mint, a store failure
// is an outage (503), and a MISSING document keeps the ADR-0017 compatibility
// behaviour untouched.

import (
	"context"
	"errors"
	"testing"
	"time"

	authModels "github.com/orkestra/backend/internal/core/auth/models"
	"github.com/orkestra/backend/pkg/sdk/iface"
)

func seedRefreshWithSession(t *testing.T, env *replayEnv, active bool) (*iface.User, string, *authModels.RefreshTokenDoc) {
	t.Helper()
	user := seededUser()
	env.users.seed(user)
	raw, doc := env.issueAndSeedRefresh(user, "fam-session-state")
	now := time.Now()
	env.sessions.seedSession(&authModels.AuthSessionDoc{
		UUID: doc.SessionUUID, UserUUID: user.UUID, IsActive: active,
		StartedAt: now, CreatedAt: now, ExpiresAt: now.Add(time.Hour),
	})
	return user, raw, doc
}

func TestRefreshSession_RotationRefusesATerminatedSession(t *testing.T) {
	env := newReplayEnv(t)
	_, raw, doc := seedRefreshWithSession(t, env, false)

	resp, err := env.auth.RefreshTokensWithRiskAssessment(context.Background(), raw, &authModels.SecurityContext{})
	if !errors.Is(err, ErrRefreshSessionInactive) {
		t.Fatalf("got (%v, %v), want ErrRefreshSessionInactive", resp, err)
	}
	if resp != nil {
		t.Fatal("a terminated session must not receive a token pair")
	}
	if active := env.refresh.activeFamilyMembers(doc.FamilyID); active != 1 {
		t.Fatalf("active rows = %d, want 1 — the refusal must happen BEFORE the rotating write", active)
	}
}

func TestRefreshSession_ReadOnlyMintRefusesATerminatedSession(t *testing.T) {
	env := newReplayEnv(t)
	_, raw, _ := seedRefreshWithSession(t, env, false)

	resp, err := env.auth.MintAccessTokenFromRefresh(context.Background(), raw, &authModels.SecurityContext{})
	if !errors.Is(err, ErrRefreshSessionInactive) {
		t.Fatalf("got (%v, %v), want ErrRefreshSessionInactive on session bootstrap too", resp, err)
	}
}

func TestRefreshSession_ActiveSessionStillRotates(t *testing.T) {
	env := newReplayEnv(t)
	_, raw, _ := seedRefreshWithSession(t, env, true)

	resp, err := env.auth.RefreshTokensWithRiskAssessment(context.Background(), raw, &authModels.SecurityContext{})
	if err != nil || resp == nil || resp.AccessToken == "" {
		t.Fatalf("active session: got (%v, %v)", resp, err)
	}
}

// The ADR-0017 compatibility window: a row with no session document behind
// it is still refreshed, and counted, until #277 tightens it. This change
// must not pre-empt that decision.
func TestRefreshSession_MissingSessionDocumentIsUnchanged(t *testing.T) {
	env := newReplayEnv(t)
	user := seededUser()
	env.users.seed(user)
	raw, _ := env.issueAndSeedRefresh(user, "fam-no-session")

	resp, err := env.auth.RefreshTokensWithRiskAssessment(context.Background(), raw, &authModels.SecurityContext{})
	if err != nil || resp == nil {
		t.Fatalf("missing session document must keep today's behaviour, got (%v, %v)", resp, err)
	}
}

func TestRefreshSession_StoreFailureIsAnOutageNotAVerdict(t *testing.T) {
	env := newReplayEnv(t)
	_, raw, _ := seedRefreshWithSession(t, env, true)
	env.sessions.failEveryGet(t)

	_, err := env.auth.RefreshTokensWithRiskAssessment(context.Background(), raw, &authModels.SecurityContext{})
	if !errors.Is(err, ErrSessionEnforcementUnavailable) {
		t.Fatalf("got %v, want ErrSessionEnforcementUnavailable — an unreadable session store is the 503 the cap already answers, not a sign-out", err)
	}
	if errors.Is(err, ErrRefreshSessionInactive) {
		t.Fatal("an outage must never be reported as a terminated session")
	}
}
