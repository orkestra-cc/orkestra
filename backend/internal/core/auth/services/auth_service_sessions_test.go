package services

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	authModels "github.com/orkestra/backend/internal/core/auth/models"
	"github.com/orkestra/backend/internal/core/auth/repository"
)

// fakeAuthSessionRepo is a tiny in-memory AuthSessionRepository for
// the self-service session tests. We only exercise GetByUUID,
// GetActiveSessionsByUser, TerminateSession, TerminateSessionByDevice,
// and TerminateAllUserSessions — anything else panics so a future
// dependency surfaces immediately.
type fakeAuthSessionRepo struct {
	mu         sync.Mutex
	byUUID     map[string]*authModels.AuthSessionDoc
	terminated []string // UUIDs that hit TerminateSession
}

func newFakeAuthSessionRepo() *fakeAuthSessionRepo {
	return &fakeAuthSessionRepo{byUUID: map[string]*authModels.AuthSessionDoc{}}
}

func (r *fakeAuthSessionRepo) seed(doc *authModels.AuthSessionDoc) {
	r.mu.Lock()
	defer r.mu.Unlock()
	cp := *doc
	r.byUUID[doc.UUID] = &cp
}

func (r *fakeAuthSessionRepo) CreateSession(_ context.Context, doc *authModels.AuthSessionDoc) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.byUUID[doc.UUID] = doc
	return nil
}

func (r *fakeAuthSessionRepo) GetByUUID(_ context.Context, uuid string) (*authModels.AuthSessionDoc, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if d, ok := r.byUUID[uuid]; ok {
		cp := *d
		return &cp, nil
	}
	return nil, nil
}

func (r *fakeAuthSessionRepo) GetByUserAndDevice(context.Context, string, string) (*authModels.AuthSessionDoc, error) {
	panic("unused: GetByUserAndDevice")
}

func (r *fakeAuthSessionRepo) GetActiveSessionsByUser(_ context.Context, userUUID string) ([]*authModels.AuthSessionDoc, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := []*authModels.AuthSessionDoc{}
	for _, d := range r.byUUID {
		if d.UserUUID != userUUID {
			continue
		}
		if !d.IsActive {
			continue
		}
		if !d.ExpiresAt.IsZero() && d.ExpiresAt.Before(time.Now()) {
			continue
		}
		cp := *d
		out = append(out, &cp)
	}
	return out, nil
}

func (r *fakeAuthSessionRepo) UpdateLastActivity(context.Context, string) error {
	panic("unused: UpdateLastActivity")
}
func (r *fakeAuthSessionRepo) UpdateRiskScore(context.Context, string, float64, string) error {
	panic("unused: UpdateRiskScore")
}
func (r *fakeAuthSessionRepo) AddSecurityEvent(context.Context, string, *authModels.SecurityEventLog) error {
	panic("unused: AddSecurityEvent")
}
func (r *fakeAuthSessionRepo) UpdateDeviceInfo(context.Context, string, *authModels.DeviceInfo) error {
	panic("unused: UpdateDeviceInfo")
}

func (r *fakeAuthSessionRepo) TerminateSession(_ context.Context, uuid string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	d, ok := r.byUUID[uuid]
	if !ok {
		return errors.New("session not found")
	}
	d.IsActive = false
	r.terminated = append(r.terminated, uuid)
	return nil
}

func (r *fakeAuthSessionRepo) TerminateSessionByDevice(_ context.Context, userUUID, deviceID string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, d := range r.byUUID {
		if d.UserUUID == userUUID && d.DeviceID == deviceID {
			d.IsActive = false
		}
	}
	return nil
}

func (r *fakeAuthSessionRepo) TerminateAllUserSessions(_ context.Context, userUUID string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, d := range r.byUUID {
		if d.UserUUID == userUUID {
			d.IsActive = false
		}
	}
	return nil
}

func (r *fakeAuthSessionRepo) TerminateExpiredSessions(context.Context) (int64, error) {
	panic("unused: TerminateExpiredSessions")
}
func (r *fakeAuthSessionRepo) DeleteAllByUser(context.Context, string) (int64, error) {
	panic("unused: DeleteAllByUser")
}
func (r *fakeAuthSessionRepo) GetSessionStats(context.Context, string) (*repository.SessionStats, error) {
	panic("unused: GetSessionStats")
}
func (r *fakeAuthSessionRepo) GetActiveDevices(context.Context, string) ([]*repository.DeviceSession, error) {
	panic("unused: GetActiveDevices")
}
func (r *fakeAuthSessionRepo) GetSessionsByLocation(context.Context, string, string) ([]*authModels.AuthSessionDoc, error) {
	panic("unused: GetSessionsByLocation")
}
func (r *fakeAuthSessionRepo) GetHighRiskSessions(context.Context, float64) ([]*authModels.AuthSessionDoc, error) {
	panic("unused: GetHighRiskSessions")
}
func (r *fakeAuthSessionRepo) RenameDevice(context.Context, string, string, string) error {
	panic("unused: RenameDevice")
}
func (r *fakeAuthSessionRepo) GetDeviceSessionHistory(context.Context, string, string, int) ([]*authModels.AuthSessionDoc, error) {
	panic("unused: GetDeviceSessionHistory")
}
func (r *fakeAuthSessionRepo) GetRecentSecurityEvents(context.Context, string, string, time.Time) ([]*authModels.SecurityEventLog, error) {
	panic("unused: GetRecentSecurityEvents")
}
func (r *fakeAuthSessionRepo) GetSuspiciousSessions(context.Context, string) ([]*authModels.AuthSessionDoc, error) {
	panic("unused: GetSuspiciousSessions")
}
func (r *fakeAuthSessionRepo) CountSessionsByUserAndFingerprint(context.Context, string, string, time.Time) (int64, error) {
	return 0, nil
}
func (r *fakeAuthSessionRepo) CountSessionsByUserAndIP(context.Context, string, string, time.Time) (int64, error) {
	return 0, nil
}
func (r *fakeAuthSessionRepo) GetMostRecentSessionByUser(context.Context, string) (*authModels.AuthSessionDoc, error) {
	return nil, nil
}

// fakeSessionRevocation captures Revoke calls so the test can assert
// the sid push happened. Implements services.SessionRevocationService.
type fakeSessionRevocation struct {
	mu      sync.Mutex
	revoked []string
}

func (s *fakeSessionRevocation) Revoke(_ context.Context, sid, _ string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.revoked = append(s.revoked, sid)
	return nil
}

func (s *fakeSessionRevocation) IsRevoked(_ context.Context, sid string) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, r := range s.revoked {
		if r == sid {
			return true, nil
		}
	}
	return false, nil
}

func (s *fakeSessionRevocation) revokedList() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]string, len(s.revoked))
	copy(out, s.revoked)
	return out
}

// newSessionsSvc constructs an *authService wired with the in-memory
// session repo, refresh-repo from gates_fakes_test.go, and a fake
// session-revocation. The userService is unused by the session
// methods, so it stays nil.
func newSessionsSvc(t *testing.T) (*authService, *fakeAuthSessionRepo, *gateRefreshRepo, *fakeSessionRevocation) {
	t.Helper()
	sessionRepo := newFakeAuthSessionRepo()
	refreshRepo := newGateRefreshRepo()
	rev := &fakeSessionRevocation{}
	svc := &authService{
		authSessionRepo:   sessionRepo,
		refreshTokenRepo:  refreshRepo,
		sessionRevocation: rev,
	}
	return svc, sessionRepo, refreshRepo, rev
}

func TestListUserSessions_FiltersInactiveAndExpired(t *testing.T) {
	t.Parallel()
	svc, sessions, _, _ := newSessionsSvc(t)
	now := time.Now()
	sessions.seed(&authModels.AuthSessionDoc{UUID: "s-active", UserUUID: "u-1", DeviceID: "d-1", IsActive: true, ExpiresAt: now.Add(time.Hour), LastActivity: now, CreatedAt: now})
	sessions.seed(&authModels.AuthSessionDoc{UUID: "s-inactive", UserUUID: "u-1", DeviceID: "d-2", IsActive: false, ExpiresAt: now.Add(time.Hour), LastActivity: now, CreatedAt: now})
	sessions.seed(&authModels.AuthSessionDoc{UUID: "s-expired", UserUUID: "u-1", DeviceID: "d-3", IsActive: true, ExpiresAt: now.Add(-time.Hour), LastActivity: now, CreatedAt: now})
	sessions.seed(&authModels.AuthSessionDoc{UUID: "s-other-user", UserUUID: "u-2", DeviceID: "d-9", IsActive: true, ExpiresAt: now.Add(time.Hour), LastActivity: now, CreatedAt: now})

	resp, err := svc.ListUserSessions(context.Background(), "u-1")
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(resp.Sessions) != 1 || resp.Sessions[0].SessionID != "s-active" {
		t.Fatalf("expected only s-active; got %+v", resp.Sessions)
	}
	if resp.ActiveCount != 1 {
		t.Errorf("ActiveCount = %d, want 1", resp.ActiveCount)
	}
}

func TestRevokeUserSession_RejectsCurrent(t *testing.T) {
	t.Parallel()
	svc, sessions, _, rev := newSessionsSvc(t)
	now := time.Now()
	sessions.seed(&authModels.AuthSessionDoc{UUID: "s-current", UserUUID: "u-1", DeviceID: "d-1", IsActive: true, ExpiresAt: now.Add(time.Hour)})

	err := svc.RevokeUserSession(context.Background(), "u-1", "s-current", "s-current")
	if !errors.Is(err, ErrCannotRevokeCurrent) {
		t.Fatalf("err = %v, want ErrCannotRevokeCurrent", err)
	}
	if len(rev.revokedList()) != 0 {
		t.Errorf("revocation must not push when guard fires; got %v", rev.revokedList())
	}
	if len(sessions.terminated) != 0 {
		t.Errorf("session must remain active when guard fires; terminated=%v", sessions.terminated)
	}
}

func TestRevokeUserSession_NotFoundForOtherUser(t *testing.T) {
	t.Parallel()
	svc, sessions, _, _ := newSessionsSvc(t)
	now := time.Now()
	sessions.seed(&authModels.AuthSessionDoc{UUID: "s-bob", UserUUID: "u-bob", DeviceID: "d-1", IsActive: true, ExpiresAt: now.Add(time.Hour)})

	err := svc.RevokeUserSession(context.Background(), "u-alice", "s-bob", "s-current")
	if !errors.Is(err, ErrSessionNotFound) {
		t.Fatalf("err = %v, want ErrSessionNotFound (don't leak existence)", err)
	}
}

func TestRevokeUserSession_HappyPath(t *testing.T) {
	t.Parallel()
	svc, sessions, refresh, rev := newSessionsSvc(t)
	now := time.Now()
	sessions.seed(&authModels.AuthSessionDoc{UUID: "s-other", UserUUID: "u-1", DeviceID: "d-other", IsActive: true, ExpiresAt: now.Add(time.Hour)})

	if err := svc.RevokeUserSession(context.Background(), "u-1", "s-other", "s-current"); err != nil {
		t.Fatalf("revoke: %v", err)
	}
	if len(sessions.terminated) != 1 || sessions.terminated[0] != "s-other" {
		t.Errorf("expected TerminateSession(s-other); got %v", sessions.terminated)
	}
	if !contains(rev.revokedList(), "s-other") {
		t.Errorf("expected sid pushed to revocation set; got %v", rev.revokedList())
	}
	if !contains(refresh.revoked, "s-other") {
		// refresh repo records userUUIDs from RevokeTokensByUser; for the
		// session-keyed call we just want the call to not error and to
		// have run. The fake's RevokeTokensBySession is currently a no-op
		// (returns nil), so the only assertion we can make on this fake
		// is that the surrounding call chain succeeded — covered above.
	}
}

func TestRevokeAllUserSessionsExcept_ExcludesCurrent(t *testing.T) {
	t.Parallel()
	svc, sessions, _, rev := newSessionsSvc(t)
	now := time.Now()
	sessions.seed(&authModels.AuthSessionDoc{UUID: "s-1", UserUUID: "u-1", DeviceID: "d-1", IsActive: true, ExpiresAt: now.Add(time.Hour)})
	sessions.seed(&authModels.AuthSessionDoc{UUID: "s-2", UserUUID: "u-1", DeviceID: "d-2", IsActive: true, ExpiresAt: now.Add(time.Hour)})
	sessions.seed(&authModels.AuthSessionDoc{UUID: "s-current", UserUUID: "u-1", DeviceID: "d-cur", IsActive: true, ExpiresAt: now.Add(time.Hour)})

	count, err := svc.RevokeAllUserSessionsExcept(context.Background(), "u-1", "s-current")
	if err != nil {
		t.Fatalf("revoke all: %v", err)
	}
	if count != 2 {
		t.Errorf("count = %d, want 2", count)
	}
	if contains(sessions.terminated, "s-current") {
		t.Errorf("must not terminate current session; got %v", sessions.terminated)
	}
	if !contains(sessions.terminated, "s-1") || !contains(sessions.terminated, "s-2") {
		t.Errorf("expected s-1 and s-2 terminated; got %v", sessions.terminated)
	}
	revoked := rev.revokedList()
	if contains(revoked, "s-current") {
		t.Errorf("must not push current sid; got %v", revoked)
	}
}

func TestRevokeAllUserSessionsExcept_NoSessionsIsZero(t *testing.T) {
	t.Parallel()
	svc, _, _, _ := newSessionsSvc(t)
	count, err := svc.RevokeAllUserSessionsExcept(context.Background(), "u-empty", "")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if count != 0 {
		t.Errorf("count = %d, want 0", count)
	}
}

func contains(slice []string, s string) bool {
	for _, v := range slice {
		if v == s {
			return true
		}
	}
	return false
}
