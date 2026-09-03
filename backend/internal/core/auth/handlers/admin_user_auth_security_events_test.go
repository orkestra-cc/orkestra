package handlers

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/danielgtaylor/huma/v2"

	"github.com/orkestra/backend/internal/core/auth/models"
	"github.com/orkestra/backend/internal/core/auth/services"
	"github.com/orkestra/backend/internal/shared/errcode"
)

// fakeEventRepo is the in-handler-test stand-in for SecurityEventRepository.
// Keeps a slice of rows + an injectable error so each test can drive a
// specific path (empty, paged, error). The real repo lives in
// repository/security_event_repository.go and is exercised against Mongo in
// integration tests.
type fakeEventRepo struct {
	mu     sync.Mutex
	rows   []*models.SecurityEvent
	listFn func(ctx context.Context, userUUID string, offset, limit int, since *time.Time) ([]*models.SecurityEvent, int64, error)
	listEr error
}

func (f *fakeEventRepo) Insert(_ context.Context, e *models.SecurityEvent) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.rows = append(f.rows, e)
	return nil
}

func (f *fakeEventRepo) ListByUser(_ context.Context, userUUID string, limit int) ([]*models.SecurityEvent, error) {
	return nil, errors.New("unused in security-events handler tests")
}

func (f *fakeEventRepo) ListByUserPaged(ctx context.Context, userUUID string, offset, limit int, since *time.Time) ([]*models.SecurityEvent, int64, error) {
	if f.listFn != nil {
		return f.listFn(ctx, userUUID, offset, limit, since)
	}
	if f.listEr != nil {
		return nil, 0, f.listEr
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	matches := make([]*models.SecurityEvent, 0, len(f.rows))
	for _, r := range f.rows {
		if r.UserUUID != userUUID {
			continue
		}
		if since != nil && !since.IsZero() && r.Timestamp.Before(*since) {
			continue
		}
		matches = append(matches, r)
	}
	total := int64(len(matches))
	if offset >= len(matches) {
		return []*models.SecurityEvent{}, total, nil
	}
	end := offset + limit
	if limit <= 0 || end > len(matches) {
		end = len(matches)
	}
	return matches[offset:end], total, nil
}

func (f *fakeEventRepo) DeleteAllByUser(_ context.Context, userUUID string) (int64, error) {
	return 0, errors.New("unused in security-events handler tests")
}

func TestGetSecurityEvents_RejectsEmptyUserID(t *testing.T) {
	h := &AdminUserAuthHandler{eventRepo: &fakeEventRepo{}}

	_, err := h.GetSecurityEvents(context.Background(), &AdminSecurityEventsRequest{UserID: ""})
	if err == nil {
		t.Fatal("expected 400 for empty userId")
	}
	se, ok := err.(huma.StatusError)
	if !ok || se.GetStatus() != 400 {
		t.Errorf("want 400, got %v", err)
	}
}

// TestGetSecurityEvents_DegradedBuildReturnsEmptyPage: a build that didn't
// wire SecurityEventRepository must still respond — empty page, no 500.
// Drives the SPA's empty-state instead of an error toast.
func TestGetSecurityEvents_DegradedBuildReturnsEmptyPage(t *testing.T) {
	h := &AdminUserAuthHandler{} // eventRepo intentionally nil

	resp, err := h.GetSecurityEvents(context.Background(), &AdminSecurityEventsRequest{UserID: "u-1", Limit: 50})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(resp.Body.Events) != 0 {
		t.Errorf("want empty events, got %d", len(resp.Body.Events))
	}
	if resp.Body.Total != 0 {
		t.Errorf("want total=0, got %d", resp.Body.Total)
	}
	if resp.Body.Limit != 50 {
		t.Errorf("Limit echo broken: got %d, want 50", resp.Body.Limit)
	}
}

// TestGetSecurityEvents_HappyPath_NewestFirst seeds three rows for the
// target user (and one for a different user that must NOT leak), then
// verifies the page comes back in the order the repo emits.
func TestGetSecurityEvents_HappyPath_NewestFirst(t *testing.T) {
	repo := &fakeEventRepo{}
	// Seed in deliberately-mixed order — the fake passes through
	// without sorting, so the rows must be inserted newest-first to
	// match the real Mongo `SetSort(timestamp:-1)`.
	repo.rows = []*models.SecurityEvent{
		{ID: "e3", UserUUID: "u-1", EventType: "admin_oauth_unlink", Timestamp: time.Date(2026, 5, 3, 0, 0, 0, 0, time.UTC)},
		{ID: "e2", UserUUID: "u-1", EventType: "self_session_revoke", Timestamp: time.Date(2026, 5, 2, 0, 0, 0, 0, time.UTC)},
		{ID: "e1", UserUUID: "u-1", EventType: "self_oauth_link", Timestamp: time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)},
		{ID: "other", UserUUID: "u-2", EventType: "self_session_revoke", Timestamp: time.Date(2026, 5, 3, 0, 0, 0, 0, time.UTC)},
	}
	h := &AdminUserAuthHandler{eventRepo: repo}

	resp, err := h.GetSecurityEvents(context.Background(), &AdminSecurityEventsRequest{UserID: "u-1", Limit: 10})
	if err != nil {
		t.Fatalf("GetSecurityEvents: %v", err)
	}
	if resp.Body.Total != 3 {
		t.Errorf("Total = %d, want 3 (cross-user row must not leak)", resp.Body.Total)
	}
	if len(resp.Body.Events) != 3 {
		t.Fatalf("Events = %d, want 3", len(resp.Body.Events))
	}
	if resp.Body.Events[0].ID != "e3" {
		t.Errorf("newest-first broken: first row = %q, want e3", resp.Body.Events[0].ID)
	}
	if resp.Body.Events[2].ID != "e1" {
		t.Errorf("oldest-last broken: last row = %q, want e1", resp.Body.Events[2].ID)
	}
}

// TestGetSecurityEvents_Pagination: offset + limit produce a sliding
// window the SPA can render with "Previous / Next" affordances.
func TestGetSecurityEvents_Pagination(t *testing.T) {
	repo := &fakeEventRepo{}
	for i := 0; i < 5; i++ {
		repo.rows = append(repo.rows, &models.SecurityEvent{
			ID:        string(rune('a' + i)),
			UserUUID:  "u-1",
			EventType: "login",
			Timestamp: time.Date(2026, 5, 1+i, 0, 0, 0, 0, time.UTC),
		})
	}
	h := &AdminUserAuthHandler{eventRepo: repo}

	resp, err := h.GetSecurityEvents(context.Background(), &AdminSecurityEventsRequest{
		UserID: "u-1", Offset: 2, Limit: 2,
	})
	if err != nil {
		t.Fatalf("GetSecurityEvents: %v", err)
	}
	if resp.Body.Total != 5 {
		t.Errorf("Total = %d, want 5 (matches the filter, not the page)", resp.Body.Total)
	}
	if len(resp.Body.Events) != 2 {
		t.Fatalf("page len = %d, want 2", len(resp.Body.Events))
	}
	if resp.Body.Offset != 2 || resp.Body.Limit != 2 {
		t.Errorf("echo fields wrong: offset=%d limit=%d", resp.Body.Offset, resp.Body.Limit)
	}
}

// TestGetSecurityEvents_SinceDaysAppliesFilter: a non-zero SinceDays
// builds a since timestamp the repo receives via the third argument.
// The fake honours `since`, so the response excludes the older row.
func TestGetSecurityEvents_SinceDaysAppliesFilter(t *testing.T) {
	repo := &fakeEventRepo{}
	now := time.Now().UTC()
	repo.rows = []*models.SecurityEvent{
		{ID: "recent", UserUUID: "u-1", EventType: "login", Timestamp: now.Add(-1 * 24 * time.Hour)},
		{ID: "stale", UserUUID: "u-1", EventType: "login", Timestamp: now.Add(-100 * 24 * time.Hour)},
	}
	h := &AdminUserAuthHandler{eventRepo: repo}

	resp, err := h.GetSecurityEvents(context.Background(), &AdminSecurityEventsRequest{
		UserID: "u-1", Limit: 10, SinceDays: 7,
	})
	if err != nil {
		t.Fatalf("GetSecurityEvents: %v", err)
	}
	if resp.Body.Total != 1 {
		t.Errorf("Total = %d, want 1 (sinceDays=7 must filter out 100d old row)", resp.Body.Total)
	}
	if len(resp.Body.Events) != 1 || resp.Body.Events[0].ID != "recent" {
		t.Errorf("stale row leaked through sinceDays filter")
	}
}

// TestGetSecurityEvents_RepoError_Surfaces500: any repo failure becomes
// a 500 so the SPA shows an error toast rather than a misleading empty
// page.
func TestGetSecurityEvents_RepoError_Surfaces500(t *testing.T) {
	repo := &fakeEventRepo{listEr: errors.New("mongo down")}
	h := &AdminUserAuthHandler{eventRepo: repo}

	_, err := h.GetSecurityEvents(context.Background(), &AdminSecurityEventsRequest{UserID: "u-1", Limit: 10})
	if err == nil {
		t.Fatal("expected error from broken repo")
	}
	se, ok := err.(huma.StatusError)
	if !ok || se.GetStatus() != 500 {
		t.Errorf("want 500, got %v", err)
	}
}

// PR 3 §4.3: the admin reset route refuses a method the target's surface
// rejects (409) and reports an unanswerable policy as an outage (503).
type fakeInviter struct{ err error }

func (f fakeInviter) AdminSendInvite(context.Context, string, string) error   { return f.err }
func (f fakeInviter) AdminResendVerification(context.Context, string) error   { return f.err }
func (f fakeInviter) AdminTriggerPasswordReset(context.Context, string) error { return f.err }

func TestSendPasswordReset_PasswordPolicyOutcomes(t *testing.T) {
	ctx := context.Background()
	req := &AdminSendPasswordResetRequest{UserID: "u1"}

	h := NewAdminUserAuthHandler(nil, fakeInviter{err: services.ErrPasswordLoginDisabled}, nil)
	_, err := h.SendPasswordReset(ctx, req)
	assertStatusAndCode(t, err, 409, "auth.password_login_disabled")

	h = NewAdminUserAuthHandler(nil, fakeInviter{err: fmt.Errorf("read passwordLoginEnabledAdmin: %w", services.ErrAuthPolicyUnavailable)}, nil)
	_, err = h.SendPasswordReset(ctx, req)
	assertStatusAndCode(t, err, 503, "auth.policy_unavailable")

}

// assertStatusAndCode asserts the HTTP status and, when wantCode is
// non-empty, the stable body code of an errcode envelope. A wantCode of
// "" accepts any body (the generic huma 401 has none).
func assertStatusAndCode(t *testing.T, err error, wantStatus int, wantCode string) {
	t.Helper()
	if err == nil {
		t.Fatal("want an error")
	}
	if got := statusOf(t, err); got != wantStatus {
		t.Fatalf("status = %d, want %d", got, wantStatus)
	}
	if wantCode == "" {
		return
	}
	var ec *errcode.Error
	if !errors.As(err, &ec) {
		t.Fatalf("want *errcode.Error, got %T (%v)", err, err)
	}
	if ec.Code != wantCode {
		t.Fatalf("code = %q, want %q", ec.Code, wantCode)
	}
}
