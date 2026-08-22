package services

// Absolute session cap enforcement (ADR-0017 D3/D4). These tests drive
// the two refresh paths end to end through the in-memory fakes: no Mongo,
// no Redis, and no wall-clock sleeps — every boundary is crossed by
// seeding the session's anchor in the past.

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	authModels "github.com/orkestra/backend/internal/core/auth/models"
	"github.com/orkestra/backend/internal/shared/utils"
	"github.com/orkestra/backend/pkg/sdk/iface"
	"github.com/orkestra/backend/pkg/sdk/metrics"
)

// capEnv wires an orchestration env whose session repo is reachable and
// whose policy carries an explicit cap.
type capEnv struct {
	*orchestrationEnv
	sessions *gateSessionRepo
	// events is wired on purpose: ADR-0017 D4 requires a
	// session_max_age_reached audit row, and without a repository the
	// nil guard in recordSessionCapEvent short-circuits, leaving the
	// required emitter entirely untested.
	events *fakeSecurityEventRepo
}

func newCapEnv(t *testing.T, capValue string) *capEnv {
	t.Helper()
	base := newOrchestrationEnv(t)
	sessions := newGateSessionRepo()
	events := &fakeSecurityEventRepo{}
	svc, err := NewAuthService(&AuthConfig{
		UserService:       base.users,
		TenantProvider:    gateTenantProvider{},
		OAuthProviderRepo: base.oauth,
		RefreshTokenRepo:  base.refresh,
		AuthSessionRepo:   sessions,
		JWTService:        base.jwt,
		FirstAdminClaimer: newGateClaimer(),
		SecurityEventRepo: events,
	})
	if err != nil {
		t.Fatalf("NewAuthService: %v", err)
	}
	svc.SetPolicy(newPolicy(map[string]string{"sessionAbsoluteTTL": capValue}))
	base.auth = svc
	return &capEnv{orchestrationEnv: base, sessions: sessions, events: events}
}

// capEvents returns just the cap's own audit rows for the user, so an
// unrelated event written by another path could never be mistaken for
// the one ADR-0017 D4 requires.
func (e *capEnv) capEvents(t *testing.T, userUUID string) []*authModels.SecurityEvent {
	t.Helper()
	rows, err := e.events.ListByUser(context.Background(), userUUID, 0)
	if err != nil {
		t.Fatalf("ListByUser: %v", err)
	}
	out := []*authModels.SecurityEvent{}
	for _, r := range rows {
		if r.EventType == "session_max_age_reached" {
			out = append(out, r)
		}
	}
	return out
}

// capEventFailures reads the REAL production counter through the real
// Prometheus exposition path rather than an injected double, so a
// recorder that quietly stops being called fails the test. Read as a
// delta because metrics.Default() is a process-wide singleton shared
// with every other test in the package; Register() is idempotent.
func capEventFailures(t *testing.T) float64 {
	t.Helper()
	if err := metrics.Default().Register(); err != nil {
		t.Fatalf("register default collector: %v", err)
	}
	rec := httptest.NewRecorder()
	metrics.Default().Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	const name = "orkestra_auth_session_cap_event_failures_total"
	for _, line := range strings.Split(rec.Body.String(), "\n") {
		if !strings.HasPrefix(line, name+" ") {
			continue
		}
		v, err := strconv.ParseFloat(strings.TrimSpace(strings.TrimPrefix(line, name+" ")), 64)
		if err != nil {
			t.Fatalf("parse %s: %v", name, err)
		}
		return v
	}
	t.Fatalf("%s is not exposed — the counter was renamed or unregistered", name)
	return 0
}

func (e *capEnv) seedUserAndSession(t *testing.T, startedAt time.Time) (*iface.User, string) {
	t.Helper()
	user := &iface.User{UUID: "u-cap", Email: "cap@example.com", Role: "operator", IsActive: true}
	e.users.seed(user)
	e.sessions.seedSession(&authModels.AuthSessionDoc{
		UUID: "sess-A", UserUUID: user.UUID, DeviceID: "dev-A",
		IsActive: true, StartedAt: startedAt, CreatedAt: startedAt,
	})
	token, _ := e.issueAndSeedRefresh(user, "fam-cap")
	return user, token
}

func TestRefresh_DeniedPastAbsoluteCap(t *testing.T) {
	env := newCapEnv(t, "24h")
	_, token := env.seedUserAndSession(t, time.Now().Add(-25*time.Hour))

	_, err := env.auth.RefreshTokensWithRiskAssessment(context.Background(), token, nil)
	if !errors.Is(err, ErrSessionMaxAgeReached) {
		t.Fatalf("refresh past the cap = %v, want ErrSessionMaxAgeReached", err)
	}
}

// The non-rotating path is NOT optional. /session mints access tokens
// without rotating the refresh cookie, so a client that calls only that
// endpoint would hold a session open forever if the cap lived on the
// rotation endpoint alone. ADR-0017 D3.
func TestMintAccessToken_DeniedPastAbsoluteCap(t *testing.T) {
	env := newCapEnv(t, "24h")
	_, token := env.seedUserAndSession(t, time.Now().Add(-25*time.Hour))

	_, err := env.auth.MintAccessTokenFromRefresh(context.Background(), token, &authModels.SecurityContext{})
	if !errors.Is(err, ErrSessionMaxAgeReached) {
		t.Fatalf("bootstrap past the cap = %v, want ErrSessionMaxAgeReached — /session must not be a bypass", err)
	}
}

// The bootstrap path must stay usable inside the cap: enforcing it is not
// licence to break the endpoint for every live session.
func TestMintAccessToken_AllowedWithinAbsoluteCap(t *testing.T) {
	env := newCapEnv(t, "24h")
	_, token := env.seedUserAndSession(t, time.Now().Add(-1*time.Hour))

	resp, err := env.auth.MintAccessTokenFromRefresh(context.Background(), token, &authModels.SecurityContext{})
	if err != nil {
		t.Fatalf("bootstrap inside the cap: %v", err)
	}
	if resp.AccessToken == "" {
		t.Fatal("bootstrap inside the cap must still mint an access token")
	}
}

// Rotation must not extend the cap. Two rotations, the second past the
// boundary, must fail even though the token presented was minted seconds
// earlier: the anchor is the session, not the token.
func TestRefresh_AllowedWithinAbsoluteCap(t *testing.T) {
	env := newCapEnv(t, "24h")
	_, token := env.seedUserAndSession(t, time.Now().Add(-23*time.Hour))

	resp, err := env.auth.RefreshTokensWithRiskAssessment(context.Background(), token, nil)
	if err != nil {
		t.Fatalf("refresh inside the cap: %v", err)
	}
	// Age the session past the cap and present the FRESH token.
	env.sessions.ageSession(t, "sess-A", -25*time.Hour)
	if _, err := env.auth.RefreshTokensWithRiskAssessment(context.Background(), resp.RefreshToken, nil); !errors.Is(err, ErrSessionMaxAgeReached) {
		t.Fatalf("second refresh = %v, want ErrSessionMaxAgeReached — rotation must not restart the clock", err)
	}
}

// The boundary is inclusive: a session whose age has reached the cap is
// over, not one tick short of it.
func TestRefresh_DeniedAtTheAbsoluteCapBoundary(t *testing.T) {
	env := newCapEnv(t, "24h")
	_, token := env.seedUserAndSession(t, time.Now().Add(-24*time.Hour))

	if _, err := env.auth.RefreshTokensWithRiskAssessment(context.Background(), token, nil); !errors.Is(err, ErrSessionMaxAgeReached) {
		t.Fatalf("refresh exactly at the cap = %v, want ErrSessionMaxAgeReached", err)
	}
}

func TestSessionCapExpiry_RevokesFamilyAndSession(t *testing.T) {
	env := newCapEnv(t, "24h")
	_, token := env.seedUserAndSession(t, time.Now().Add(-25*time.Hour))

	_, _ = env.auth.RefreshTokensWithRiskAssessment(context.Background(), token, nil)

	sess, _ := env.sessions.GetByUUID(context.Background(), "sess-A")
	if sess == nil || sess.IsActive {
		t.Errorf("session still active after cap expiry: %+v — reaching the cap is a logout, not a denial", sess)
	}
	doc, _ := env.refresh.GetByTokenAny(context.Background(), utils.HashRefreshToken(token))
	if doc == nil || !doc.IsRevoked {
		t.Errorf("refresh row not revoked after cap expiry: %+v", doc)
	}
	if doc != nil && doc.RevokedReason != authModels.RevokeReasonSessionMaxAge {
		t.Errorf("revoked reason = %q, want %q", doc.RevokedReason, authModels.RevokeReasonSessionMaxAge)
	}
}

func TestSessionCap_DisabledWhenUnset(t *testing.T) {
	env := newCapEnv(t, "")
	_, token := env.seedUserAndSession(t, time.Now().Add(-10*365*24*time.Hour))
	env.sessions.failEveryGet(t) // an empty cap must skip the query entirely

	if _, err := env.auth.RefreshTokensWithRiskAssessment(context.Background(), token, nil); err != nil {
		t.Fatalf("cap disabled must not query the session repo or block the refresh: %v", err)
	}
}

func TestSessionCap_MissingSessionRow(t *testing.T) {
	// Pins ADR-0017's temporary compatibility rule so changing it is
	// deliberate. A clean not-found PERMITS the refresh and increments
	// the anomaly counter; it must be tightened to fail-closed in the
	// first minor release after 30 consecutive production days at zero.
	env := newCapEnv(t, "24h")
	user := &iface.User{UUID: "u-orphan", Email: "orphan@example.com", Role: "operator", IsActive: true}
	env.users.seed(user)
	token, _ := env.issueAndSeedRefresh(user, "fam-orphan") // no session row seeded

	if _, err := env.auth.RefreshTokensWithRiskAssessment(context.Background(), token, nil); err != nil {
		t.Fatalf("missing session row must fail OPEN during the compatibility window: %v", err)
	}
}

func TestSessionCap_ZeroTimestampRowFallsBackToCreatedAt(t *testing.T) {
	// StartedAt zero but CreatedAt usable is NOT an anomaly — it has a
	// perfectly good anchor, and counting it would poison the 30-day
	// observation window that gates the fail-closed change.
	env := newCapEnv(t, "24h")
	user := &iface.User{UUID: "u-cap", Email: "cap@example.com", Role: "operator", IsActive: true}
	env.users.seed(user)
	env.sessions.seedSession(&authModels.AuthSessionDoc{
		UUID: "sess-A", UserUUID: user.UUID, DeviceID: "dev-A", IsActive: true,
		CreatedAt: time.Now().Add(-25 * time.Hour), // StartedAt deliberately zero
	})
	token, _ := env.issueAndSeedRefresh(user, "fam-cap")

	if _, err := env.auth.RefreshTokensWithRiskAssessment(context.Background(), token, nil); !errors.Is(err, ErrSessionMaxAgeReached) {
		t.Fatalf("CreatedAt must serve as the compatibility anchor: %v", err)
	}
}

// A row with neither timestamp has no anchor at all — that is the second
// anomaly kind, and it fails open under the same compatibility window.
func TestSessionCap_RowWithNoTimestampsFailsOpen(t *testing.T) {
	env := newCapEnv(t, "24h")
	user := &iface.User{UUID: "u-cap", Email: "cap@example.com", Role: "operator", IsActive: true}
	env.users.seed(user)
	env.sessions.seedSession(&authModels.AuthSessionDoc{
		UUID: "sess-A", UserUUID: user.UUID, DeviceID: "dev-A", IsActive: true,
	})
	token, _ := env.issueAndSeedRefresh(user, "fam-cap")

	if _, err := env.auth.RefreshTokensWithRiskAssessment(context.Background(), token, nil); err != nil {
		t.Fatalf("an anchorless row must fail OPEN during the compatibility window: %v", err)
	}
	if n := env.sessions.expiryTransitions("sess-A"); n != 0 {
		t.Fatalf("anchorless row expired the session %d times — it must be permitted, not terminated", n)
	}
}

func TestSessionCap_RepositoryErrorFailsClosed(t *testing.T) {
	// A database failure is not a compatibility miss. Compatibility
	// telemetry is not permission to accept refreshes during an outage.
	env := newCapEnv(t, "24h")
	_, token := env.seedUserAndSession(t, time.Now().Add(-1*time.Hour))
	env.sessions.failEveryGet(t)

	_, err := env.auth.RefreshTokensWithRiskAssessment(context.Background(), token, nil)
	if !errors.Is(err, ErrSessionEnforcementUnavailable) {
		t.Fatalf("repository error = %v, want ErrSessionEnforcementUnavailable (fail closed)", err)
	}
	if errors.Is(err, ErrSessionMaxAgeReached) {
		t.Fatal("an outage must never be reported as a cap expiry")
	}
}

// The same rule on the bootstrap path: an unreadable session store must
// not become a free access token.
func TestSessionCap_RepositoryErrorFailsClosedOnMint(t *testing.T) {
	env := newCapEnv(t, "24h")
	_, token := env.seedUserAndSession(t, time.Now().Add(-1*time.Hour))
	env.sessions.failEveryGet(t)

	resp, err := env.auth.MintAccessTokenFromRefresh(context.Background(), token, &authModels.SecurityContext{})
	if !errors.Is(err, ErrSessionEnforcementUnavailable) {
		t.Fatalf("repository error on mint = %v, want ErrSessionEnforcementUnavailable", err)
	}
	if resp != nil {
		t.Fatalf("no credentials may be minted while enforcement is unavailable: %+v", resp)
	}
}

// Durable revocation is part of the logout, so a failure there is an
// outage, not a cap expiry — the caller must not be told the session was
// cleanly terminated when its refresh tokens are still live.
func TestSessionCap_DurableRevocationErrorFailsClosed(t *testing.T) {
	env := newCapEnv(t, "24h")
	_, token := env.seedUserAndSession(t, time.Now().Add(-25*time.Hour))
	env.refresh.revokeBySessionErr = errors.New("refresh store unavailable")

	_, err := env.auth.RefreshTokensWithRiskAssessment(context.Background(), token, nil)
	if !errors.Is(err, ErrSessionEnforcementUnavailable) {
		t.Fatalf("durable revocation failure = %v, want ErrSessionEnforcementUnavailable", err)
	}
	if n := env.sessions.expiryTransitions("sess-A"); n != 0 {
		t.Fatalf("session flipped inactive (%d transitions) although refresh revocation failed — the steps must not half-apply", n)
	}
}

// A Redis denylist failure AFTER durable revocation is a degradation, not
// a failure: the logout happened, so the caller still clears the cookie —
// but the response must not claim a cleanly recorded cap expiry.
func TestSessionCap_DenylistFailureReportsDegraded(t *testing.T) {
	env := newCapEnv(t, "24h")
	_, token := env.seedUserAndSession(t, time.Now().Add(-25*time.Hour))
	env.auth.SetSessionRevocation(&fakeSessionRevocation{err: errors.New("sensitive redis endpoint")})

	_, err := env.auth.RefreshTokensWithRiskAssessment(context.Background(), token, nil)
	var degraded *SessionRevocationDegradedError
	if !errors.As(err, &degraded) {
		t.Fatalf("error = %v, want typed SessionRevocationDegradedError", err)
	}
	// The other half of the same rule, and the half that holds only
	// incidentally today: Unwrap() exposes the Redis cause, not the cap
	// sentinel. Wrapping the two together later — fmt.Errorf("%w: %w",
	// ErrSessionMaxAgeReached, degraded) — would make Task 11 report a
	// cleanly recorded cap expiry while the denylist is still behind,
	// and nothing else in the suite would notice.
	if errors.Is(err, ErrSessionMaxAgeReached) {
		t.Fatal("a degraded cap logout must not report a cleanly recorded cap expiry — Task 11 maps that sentinel to a specific client code")
	}
	sess, _ := env.sessions.GetByUUID(context.Background(), "sess-A")
	if sess == nil || sess.IsActive {
		t.Fatalf("durable session must remain terminated on Redis degradation: %+v", sess)
	}
	// "Degraded" means ONLY the denylist is behind. If the refresh rows
	// were still live this would be a fail-closed case, not a degraded
	// one, and the caller must not be told to clear its cookie and move on.
	doc, _ := env.refresh.GetByTokenAny(context.Background(), utils.HashRefreshToken(token))
	if doc == nil || !doc.IsRevoked {
		t.Fatalf("durable refresh revocation must have completed before the degradation: %+v", doc)
	}
}

func TestSessionCap_ConcurrentExpiryCountedOnce(t *testing.T) {
	env := newCapEnv(t, "24h")
	user, token := env.seedUserAndSession(t, time.Now().Add(-25*time.Hour))
	second, _ := env.issueAndSeedRefresh(user, "fam-cap-2")

	var wg sync.WaitGroup
	wg.Add(2)
	for _, tk := range []string{token, second} {
		go func(tk string) {
			defer wg.Done()
			_, _ = env.auth.RefreshTokensWithRiskAssessment(context.Background(), tk, nil)
		}(tk)
	}
	wg.Wait()

	if n := env.sessions.expiryTransitions("sess-A"); n != 1 {
		t.Fatalf("winning transitions = %d, want exactly 1 — two refreshes must not double-count the event or the metric", n)
	}
	// One audit row for one terminated session, whichever way the two
	// callers interleave. Note what this does NOT prove: in practice the
	// second caller is rejected before the cap check, because the
	// winner's RevokeTokensBySession already revoked its row. The
	// winner-only gate itself is pinned deterministically by
	// TestSessionCap_LosingTheExpiryCASEmitsNothing below.
	if rows := env.capEvents(t, user.UUID); len(rows) != 1 {
		t.Fatalf("cap audit rows = %d, want exactly 1 — the event must be emitted only by the caller that won the transition", len(rows))
	}
}

// The `if won` gate, exercised directly. Two goroutines racing the
// refresh path cannot reach it — the first one's refresh revocation
// invalidates the second one's row, so the second bails out with
// ErrInvalidRefreshToken long before the CAS. Calling the helper twice
// reproduces the losing branch deterministically: the session is already
// inactive, ExpireSessionForMaxAge reports (false, nil), and the loser
// must still return the cap sentinel while emitting NOTHING.
func TestSessionCap_LosingTheExpiryCASEmitsNothing(t *testing.T) {
	env := newCapEnv(t, "24h")
	user, _ := env.seedUserAndSession(t, time.Now().Add(-25*time.Hour))
	svc, ok := env.auth.(*authService)
	if !ok {
		t.Fatalf("NewAuthService returned %T, want *authService", env.auth)
	}
	sess, err := env.sessions.GetByUUID(context.Background(), "sess-A")
	if err != nil || sess == nil {
		t.Fatalf("seeded session unavailable: %+v %v", sess, err)
	}

	for i, want := range []string{"winner", "loser"} {
		if err := svc.expireSessionForMaxAge(context.Background(), sess); !errors.Is(err, ErrSessionMaxAgeReached) {
			t.Fatalf("call %d (%s) = %v, want ErrSessionMaxAgeReached — both callers see the same outcome", i, want, err)
		}
	}

	if n := env.sessions.expiryTransitions("sess-A"); n != 1 {
		t.Fatalf("winning transitions = %d, want exactly 1", n)
	}
	if rows := env.capEvents(t, user.UUID); len(rows) != 1 {
		t.Fatalf("cap audit rows = %d, want exactly 1 — the CAS loser must emit nothing", len(rows))
	}
}

// ADR-0017 D4 requires the session_max_age_reached audit row, so the
// emitter gets its own test rather than being inferred from the metric.
func TestSessionCapExpiry_RecordsSecurityEventOnce(t *testing.T) {
	env := newCapEnv(t, "24h")
	user, token := env.seedUserAndSession(t, time.Now().Add(-25*time.Hour))

	if _, err := env.auth.RefreshTokensWithRiskAssessment(context.Background(), token, nil); !errors.Is(err, ErrSessionMaxAgeReached) {
		t.Fatalf("refresh past the cap = %v, want ErrSessionMaxAgeReached", err)
	}

	rows := env.capEvents(t, user.UUID)
	if len(rows) != 1 {
		t.Fatalf("cap audit rows = %d, want exactly 1", len(rows))
	}
	if rows[0].UserUUID != user.UUID {
		t.Errorf("event UserUUID = %q, want %q", rows[0].UserUUID, user.UUID)
	}
	if !rows[0].Success {
		t.Errorf("a completed cap logout is a successful outcome, not a failure: %+v", rows[0])
	}
	if rows[0].Timestamp.IsZero() {
		t.Errorf("event must carry a timestamp: %+v", rows[0])
	}
}

// A failed audit write can never restore credentials — durable state is
// already terminated by the time it runs. It counts and logs, and the
// caller still gets the cap sentinel.
func TestSessionCap_EventInsertFailureStillExpiresTheSession(t *testing.T) {
	env := newCapEnv(t, "24h")
	user, token := env.seedUserAndSession(t, time.Now().Add(-25*time.Hour))
	env.events.insErr = errors.New("security event store unavailable")

	before := capEventFailures(t)
	_, err := env.auth.RefreshTokensWithRiskAssessment(context.Background(), token, nil)
	if !errors.Is(err, ErrSessionMaxAgeReached) {
		t.Fatalf("audit failure = %v, want ErrSessionMaxAgeReached — a failed audit write must not restore credentials", err)
	}
	if delta := capEventFailures(t) - before; delta != 1 {
		t.Errorf("orkestra_auth_session_cap_event_failures_total delta = %v, want 1", delta)
	}
	if rows := env.capEvents(t, user.UUID); len(rows) != 0 {
		t.Errorf("no row can have landed when the insert failed: %+v", rows)
	}
	sess, _ := env.sessions.GetByUUID(context.Background(), "sess-A")
	if sess == nil || sess.IsActive {
		t.Errorf("session must stay terminated regardless of the audit write: %+v", sess)
	}
}
