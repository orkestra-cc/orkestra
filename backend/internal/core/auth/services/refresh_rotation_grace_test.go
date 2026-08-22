package services

// Rotation-grace tests. RefreshRotationGrace exists so that the refresh
// path can tell two situations apart that both present a token already
// marked "rotated":
//
//   - several tabs of one app racing on the same cookie (benign), and
//   - a stolen token being replayed (an attack).
//
// Time alone cannot separate them, so the family fence carries the
// decision: a racing sibling runs against a healthy family, a replay that
// already tripped detection does not. These tests pin both directions,
// plus the boundary that keeps replay detection intact outside the window.

import (
	"context"
	"errors"
	"testing"
	"time"

	authModels "github.com/orkestra/backend/internal/core/auth/models"
	"github.com/orkestra/backend/internal/shared/utils"
)

// rotateOnce performs one successful rotation and returns the hash of the
// now-superseded token, which is what a racing sibling would re-present.
func rotateOnce(t *testing.T, env *orchestrationEnv, raw string) string {
	t.Helper()
	if _, err := env.auth.RefreshTokensWithRiskAssessment(context.Background(), raw, &authModels.SecurityContext{}); err != nil {
		t.Fatalf("seed rotation: %v", err)
	}
	return utils.HashRefreshToken(raw)
}

func TestRefreshGrace_WithinWindowIsRacedNotReplay(t *testing.T) {
	env := newOrchestrationEnv(t)
	user := seededUser()
	env.users.seed(user)
	raw, doc := env.issueAndSeedRefresh(user, "fam-grace-ok")
	rotateOnce(t, env, raw)

	_, err := env.auth.RefreshTokensWithRiskAssessment(context.Background(), raw, &authModels.SecurityContext{})
	if !errors.Is(err, ErrRefreshRotationRaced) {
		t.Fatalf("got %v, want ErrRefreshRotationRaced", err)
	}
	if revoked, _ := env.refresh.FamilyRevoked(context.Background(), doc.FamilyID); revoked {
		t.Fatal("family revoked inside the grace window — the sibling tab lost its session")
	}
	if active := env.refresh.activeFamilyMembers(doc.FamilyID); active != 1 {
		t.Fatalf("active successors = %d, want 1 (the winner's)", active)
	}
}

func TestRefreshGrace_OutsideWindowIsReplay(t *testing.T) {
	env := newOrchestrationEnv(t)
	user := seededUser()
	env.users.seed(user)
	raw, doc := env.issueAndSeedRefresh(user, "fam-grace-expired")
	hash := rotateOnce(t, env, raw)
	env.refresh.backdateRevocation(hash, RefreshRotationGrace+time.Second)

	_, err := env.auth.RefreshTokensWithRiskAssessment(context.Background(), raw, &authModels.SecurityContext{})
	if !errors.Is(err, ErrRefreshTokenReplay) {
		t.Fatalf("got %v, want ErrRefreshTokenReplay past the grace window", err)
	}
	if revoked, _ := env.refresh.FamilyRevoked(context.Background(), doc.FamilyID); !revoked {
		t.Fatal("family survived a genuine replay")
	}
	if active := env.refresh.activeFamilyMembers(doc.FamilyID); active != 0 {
		t.Fatalf("active successors after replay = %d, want 0", active)
	}
}

// A token presented inside the window but whose family is ALREADY revoked
// is a replay, not a race: the fence is exactly what tells them apart.
func TestRefreshGrace_RevokedFamilyInsideWindowIsReplay(t *testing.T) {
	env := newOrchestrationEnv(t)
	user := seededUser()
	env.users.seed(user)
	raw, doc := env.issueAndSeedRefresh(user, "fam-grace-dead")
	rotateOnce(t, env, raw)
	if _, err := env.refresh.RevokeFamily(context.Background(), doc.FamilyID, authModels.RevokeReasonReplayDetected); err != nil {
		t.Fatalf("RevokeFamily: %v", err)
	}

	_, err := env.auth.RefreshTokensWithRiskAssessment(context.Background(), raw, &authModels.SecurityContext{})
	if errors.Is(err, ErrRefreshRotationRaced) {
		t.Fatal("a revoked family must never be answered with a retry hint")
	}
	if err == nil {
		t.Fatal("rotation succeeded against a revoked family")
	}
}

// The grace path must never mint credentials — that property is what keeps
// the widened window from being an escalation for an attacker who replays
// a stolen token inside it.
func TestRefreshGrace_IssuesNoCredentials(t *testing.T) {
	env := newOrchestrationEnv(t)
	user := seededUser()
	env.users.seed(user)
	raw, _ := env.issueAndSeedRefresh(user, "fam-grace-nocreds")
	rotateOnce(t, env, raw)

	resp, err := env.auth.RefreshTokensWithRiskAssessment(context.Background(), raw, &authModels.SecurityContext{})
	if !errors.Is(err, ErrRefreshRotationRaced) {
		t.Fatalf("got %v, want ErrRefreshRotationRaced", err)
	}
	if resp != nil {
		t.Fatalf("grace path returned a token response: %+v", resp)
	}
}

// Revocations that are not rotations (logout, password change, new login)
// keep their existing meaning — the window applies to rotation only.
func TestRefreshGrace_NonRotatedRevocationIsUnaffected(t *testing.T) {
	env := newOrchestrationEnv(t)
	user := seededUser()
	env.users.seed(user)
	now := time.Now()
	raw, _ := env.issueAndSeedRefresh(user, "fam-grace-logout", func(d *authModels.RefreshTokenDoc) {
		d.IsRevoked = true
		d.RevokedAt = &now
		d.RevokedReason = "logout"
	})

	_, err := env.auth.RefreshTokensWithRiskAssessment(context.Background(), raw, &authModels.SecurityContext{})
	if !errors.Is(err, ErrInvalidRefreshToken) {
		t.Fatalf("got %v, want ErrInvalidRefreshToken", err)
	}
}
