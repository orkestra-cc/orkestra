package services

// Replay detection used to revoke the refresh FAMILY and nothing else: the
// session document stayed active, its sid was never denylisted, and no
// security-event row named the user. The attacker who provoked the replay
// — by exfiltrating the cookie and rotating it — kept the access token they
// had already minted until it expired, the session kept showing "active" in
// /me/sessions, and operators had no durable record to act on. These tests
// pin the three consequences a detected replay now has, alongside the
// family revocation that was already there.

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	authModels "github.com/orkestra/backend/internal/core/auth/models"
	"github.com/orkestra/backend/pkg/sdk/iface"
)

type replayEnv struct {
	*orchestrationEnv
	sessions *gateSessionRepo
	events   *fakeSecurityEventRepo
	rev      *fakeSessionRevocation
}

func newReplayEnv(t *testing.T) *replayEnv {
	t.Helper()
	priv := testRSAKey()
	jwt, err := NewJWTServiceWithAudience(priv, &priv.PublicKey, "test", AudienceOperator, 15*time.Minute, 7*24*time.Hour)
	if err != nil {
		t.Fatalf("jwt: %v", err)
	}
	jwt.SetTenantProvider(gateTenantProvider{})
	base := &orchestrationEnv{t: t, users: newGateUserFake(), refresh: newGateRefreshRepo(), oauth: &orchOAuthRepo{}, jwt: jwt}
	env := &replayEnv{orchestrationEnv: base, sessions: newGateSessionRepo(), events: &fakeSecurityEventRepo{}, rev: &fakeSessionRevocation{}}
	authSvc, err := NewAuthService(&AuthConfig{
		UserService:       env.users,
		TenantProvider:    gateTenantProvider{},
		OAuthProviderRepo: env.oauth,
		RefreshTokenRepo:  env.refresh,
		AuthSessionRepo:   env.sessions,
		JWTService:        jwt,
		FirstAdminClaimer: newGateClaimer(),
		SecurityEventRepo: env.events,
	})
	if err != nil {
		t.Fatalf("NewAuthService: %v", err)
	}
	authSvc.SetSessionRevocation(env.rev)
	env.auth = authSvc
	return env
}

// provokeReplay seeds a live session behind a refresh row, rotates once, and
// re-presents the superseded token outside the grace window: a genuine
// replay. Returns the user, the row and the error the replay answered with.
func provokeReplay(t *testing.T, env *replayEnv, ip string) (*iface.User, *authModels.RefreshTokenDoc, error) {
	t.Helper()
	user := seededUser()
	env.users.seed(user)
	raw, doc := env.issueAndSeedRefresh(user, "fam-replay-session")
	now := time.Now()
	env.sessions.seedSession(&authModels.AuthSessionDoc{
		UUID: doc.SessionUUID, UserUUID: user.UUID, IsActive: true,
		StartedAt: now, CreatedAt: now, ExpiresAt: now.Add(time.Hour),
	})
	hash := rotateOnce(t, env.orchestrationEnv, raw)
	env.refresh.backdateRevocation(hash, RefreshRotationGrace+time.Second)
	_, err := env.auth.RefreshTokensWithRiskAssessment(context.Background(), raw, &authModels.SecurityContext{IPAddress: ip})
	return user, doc, err
}

func TestRefreshReplay_TerminatesTheSessionAndDenylistsItsSID(t *testing.T) {
	env := newReplayEnv(t)
	_, doc, err := provokeReplay(t, env, "203.0.113.9")
	if !errors.Is(err, ErrRefreshTokenReplay) {
		t.Fatalf("got %v, want ErrRefreshTokenReplay", err)
	}
	sess, gerr := env.sessions.GetByUUID(context.Background(), doc.SessionUUID)
	if gerr != nil || sess == nil {
		t.Fatalf("session lookup: %v", gerr)
	}
	if sess.IsActive {
		t.Fatal("session still active after a detected replay — the attacker's session survives the family it was caught on")
	}
	found := false
	for _, sid := range env.rev.revoked {
		if sid == doc.SessionUUID {
			found = true
		}
	}
	if !found {
		t.Fatalf("sid %q not denylisted; the already-minted access token stays valid until it expires", doc.SessionUUID)
	}
}

func TestRefreshReplay_RecordsASecurityEventNamingTheUser(t *testing.T) {
	env := newReplayEnv(t)
	user, _, _ := provokeReplay(t, env, "203.0.113.9")

	var row *authModels.SecurityEvent
	for _, r := range env.events.rows {
		if r.EventType == "refresh_token_replay" {
			row = r
		}
	}
	if row == nil {
		t.Fatalf("no refresh_token_replay security event; rows = %+v", env.events.rows)
	}
	if row.UserUUID != user.UUID {
		t.Fatalf("event user = %q, want %q", row.UserUUID, user.UUID)
	}
	if row.IPAddress != "203.0.113.9" {
		t.Fatalf("event ip = %q, want the presenting address", row.IPAddress)
	}
	if row.Success {
		t.Fatal("a replay is a failure event")
	}
	for k, v := range row.Metadata {
		s, _ := v.(string)
		if strings.Contains(s, "eyJ") || len(s) >= 64 {
			t.Fatalf("metadata %q looks like token material: %q", k, s)
		}
	}
}

func TestRefreshReplay_DegradedDenylistStillAnswersReplay(t *testing.T) {
	env := newReplayEnv(t)
	env.rev.err = errors.New("redis: connection refused")
	_, doc, err := provokeReplay(t, env, "203.0.113.9")
	if !errors.Is(err, ErrRefreshTokenReplay) {
		t.Fatalf("got %v, want ErrRefreshTokenReplay even when the denylist is degraded — the verdict was reached", err)
	}
	sess, _ := env.sessions.GetByUUID(context.Background(), doc.SessionUUID)
	if sess == nil || sess.IsActive {
		t.Fatal("durable session termination must not depend on the denylist write")
	}
}
