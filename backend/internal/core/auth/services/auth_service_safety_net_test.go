package services

// Phase 1.3 + 2.1 follow-on tests for surfaces the core-completion epic
// reworked. GetOAuthLinks now computes RequiresMFA from real factor state;
// RecordSecurityEvent / GetSecurityEvents now persist when a repository is
// wired (and stay no-op when it isn't, for backward compat with minimal
// builds that don't wire the repo).
//
// The AddOAuthLink stub had its sentinel test removed alongside the stub
// itself in Phase 1.2 — link creation goes through the OAuth flow exposed
// by self_user_auth_handler, not a service-level entry point.

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/orkestra/backend/internal/core/auth/models"
	"github.com/orkestra/backend/pkg/sdk/iface"
)

// TestGetOAuthLinks_RequiresMFAFalseWithoutFactor: a user with no enrolled
// factor (nor repo wired) reports RequiresMFA=false so the unlink path
// surfaces the password-reconfirm modal instead of step-up.
func TestGetOAuthLinks_RequiresMFAFalseWithoutFactor(t *testing.T) {
	t.Parallel()
	fake := newAdminUnlinkUserFake()
	fake.seed(&iface.User{
		UUID:         "u-multi",
		Role:         "administrator",
		PasswordHash: "x",
		OAuthLinks: []iface.OAuthLink{
			{Provider: "google", ProviderID: "g-1", Email: "u@x.com", IsActive: true, IsPrimary: true},
			{Provider: "github", ProviderID: "gh-1", Email: "u@x.com", IsActive: true},
		},
	})
	// mfaFactorRepo intentionally nil — emulates the test wiring before
	// MFA was bolted on. RequiresMFA must stay false in this degraded mode.
	svc := &authService{userService: fake}

	resp, err := svc.GetOAuthLinks(context.Background(), "u-multi")
	if err != nil {
		t.Fatalf("GetOAuthLinks: %v", err)
	}
	if len(resp.Links) != 2 || !resp.CanUnlink {
		t.Errorf("Links/CanUnlink mismatch: %+v", resp)
	}
	if resp.RequiresMFA {
		t.Errorf("RequiresMFA must be false when no MFA factor is wired")
	}
}

// TestGetOAuthLinks_RequiresMFATrueWithEnrolledTOTP: a user with a TOTP
// factor surfaces RequiresMFA=true so the SPA routes the unlink through
// the step-up modal, not the password-reconfirm one.
func TestGetOAuthLinks_RequiresMFATrueWithEnrolledTOTP(t *testing.T) {
	t.Parallel()
	fake := newAdminUnlinkUserFake()
	fake.seed(&iface.User{
		UUID:         "u-totp",
		Role:         "administrator",
		PasswordHash: "x",
		OAuthLinks: []iface.OAuthLink{
			{Provider: "google", ProviderID: "g-1", Email: "u@x.com", IsActive: true},
		},
	})
	enrolled := time.Now().Add(-7 * 24 * time.Hour)
	factors := newFakeFactorRepo()
	_ = factors.Insert(context.Background(), &models.MFAFactorDoc{
		UUID:       "fact-1",
		UserUUID:   "u-totp",
		Type:       models.MFAFactorTOTP,
		VerifiedAt: &enrolled,
	})
	svc := &authService{userService: fake, mfaFactorRepo: factors}

	resp, err := svc.GetOAuthLinks(context.Background(), "u-totp")
	if err != nil {
		t.Fatalf("GetOAuthLinks: %v", err)
	}
	if !resp.RequiresMFA {
		t.Errorf("user with TOTP factor must report RequiresMFA=true")
	}
}

// TestGetOAuthLinks_RequiresMFATrueWithWebAuthn: ditto via the embedded
// webauthnCredentials array on the WebAuthn factor row.
func TestGetOAuthLinks_RequiresMFATrueWithWebAuthn(t *testing.T) {
	t.Parallel()
	fake := newAdminUnlinkUserFake()
	fake.seed(&iface.User{
		UUID:         "u-wa",
		Role:         "operator",
		PasswordHash: "x",
		OAuthLinks: []iface.OAuthLink{
			{Provider: "google", ProviderID: "g-1", Email: "u@x.com", IsActive: true},
		},
	})
	factors := newFakeFactorRepo()
	_ = factors.Insert(context.Background(), &models.MFAFactorDoc{
		UUID:     "fact-wa",
		UserUUID: "u-wa",
		Type:     models.MFAFactorWebAuthn,
		WebAuthnCredentials: []models.WebAuthnCredential{
			{CredentialID: []byte{0x01, 0x02}, Name: "Touch ID", CreatedAt: time.Now()},
		},
	})
	svc := &authService{userService: fake, mfaFactorRepo: factors}

	resp, _ := svc.GetOAuthLinks(context.Background(), "u-wa")
	if !resp.RequiresMFA {
		t.Errorf("user with WebAuthn credential must report RequiresMFA=true")
	}
}

// TestGetOAuthLinks_RequiresMFAFalseWithEmptyWebAuthnArray: a WebAuthn factor
// row that exists but carries zero credentials must NOT count — the embedded
// array is the source of truth for "is a passkey enrolled".
func TestGetOAuthLinks_RequiresMFAFalseWithEmptyWebAuthnArray(t *testing.T) {
	t.Parallel()
	fake := newAdminUnlinkUserFake()
	fake.seed(&iface.User{
		UUID:         "u-wa-empty",
		Role:         "operator",
		PasswordHash: "x",
		OAuthLinks: []iface.OAuthLink{
			{Provider: "google", ProviderID: "g-1", IsActive: true},
		},
	})
	factors := newFakeFactorRepo()
	_ = factors.Insert(context.Background(), &models.MFAFactorDoc{
		UUID:                "fact-wa-empty",
		UserUUID:            "u-wa-empty",
		Type:                models.MFAFactorWebAuthn,
		WebAuthnCredentials: nil,
	})
	svc := &authService{userService: fake, mfaFactorRepo: factors}

	resp, _ := svc.GetOAuthLinks(context.Background(), "u-wa-empty")
	if resp.RequiresMFA {
		t.Errorf("WebAuthn factor row with 0 credentials must not flip RequiresMFA")
	}
}

// TestGetOAuthLinks_SingleLinkCannotUnlink: with only one link, CanUnlink is
// false. This is the only piece of GetOAuthLinks' logic that Phase 1.3 does
// NOT touch.
func TestGetOAuthLinks_SingleLinkCannotUnlink(t *testing.T) {
	t.Parallel()
	fake := newAdminUnlinkUserFake()
	fake.seed(&iface.User{
		UUID: "u-sole",
		OAuthLinks: []iface.OAuthLink{
			{Provider: "google", ProviderID: "g-1", Email: "u@x.com", IsActive: true, IsPrimary: true},
		},
	})
	svc := &authService{userService: fake}

	resp, err := svc.GetOAuthLinks(context.Background(), "u-sole")
	if err != nil {
		t.Fatalf("GetOAuthLinks: %v", err)
	}
	if resp.CanUnlink {
		t.Errorf("CanUnlink with a single link should be false")
	}
}

// TestGetOAuthLinks_PropagatesUserServiceError: a missing user surfaces as an
// error from the user provider; GetOAuthLinks must not swallow it. The fake
// returns errNotFound when no user is seeded, which is the closest off-the-
// shelf failure mode without growing the shared helper.
func TestGetOAuthLinks_PropagatesUserServiceError(t *testing.T) {
	t.Parallel()
	svc := &authService{userService: newAdminUnlinkUserFake()}

	_, err := svc.GetOAuthLinks(context.Background(), "no-such-user")
	if err == nil {
		t.Fatal("expected error from underlying user provider")
	}
}

// TestRecordSecurityEvent_NoRepoStillReturnsNil: a service constructed
// without SecurityEventRepo (minimal build / older test fixtures) must
// keep behaving as the pre-Phase-2.1 no-op so existing tests in the
// auth tree don't break. Real persistence is tested separately below.
func TestRecordSecurityEvent_NoRepoStillReturnsNil(t *testing.T) {
	t.Parallel()
	svc := &authService{}

	err := svc.RecordSecurityEvent(context.Background(), &models.SecurityEvent{
		ID:        "ev-1",
		UserUUID:  "u-1",
		EventType: "test_event",
		Timestamp: time.Now(),
	})
	if err != nil {
		t.Errorf("no-repo path must return nil, got %v", err)
	}
}

// TestGetSecurityEvents_NoRepoReturnsEmptySlice: same backward-compat
// path on the reader side.
func TestGetSecurityEvents_NoRepoReturnsEmptySlice(t *testing.T) {
	t.Parallel()
	svc := &authService{}

	events, err := svc.GetSecurityEvents(context.Background(), "u-1", 100)
	if err != nil {
		t.Errorf("no-repo path must return nil error, got %v", err)
	}
	if len(events) != 0 {
		t.Errorf("no-repo path must return an empty slice, got %d entries", len(events))
	}
}

// fakeSecurityEventRepo is an in-memory SecurityEventRepository for the
// Phase 2.1 persistence tests. Tracks Insert calls so a test can assert
// the row landed; ListByUser sorts newest-first like the real impl.
type fakeSecurityEventRepo struct {
	mu     sync.Mutex
	rows   []*models.SecurityEvent
	insErr error
}

func (f *fakeSecurityEventRepo) Insert(_ context.Context, e *models.SecurityEvent) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.insErr != nil {
		return f.insErr
	}
	clone := *e
	if clone.ID == "" {
		clone.ID = "fake-" + clone.EventType
	}
	if clone.Timestamp.IsZero() {
		clone.Timestamp = time.Now().UTC()
	}
	f.rows = append(f.rows, &clone)
	return nil
}

func (f *fakeSecurityEventRepo) ListByUser(_ context.Context, userUUID string, limit int) ([]*models.SecurityEvent, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if limit <= 0 {
		limit = 100
	}
	out := make([]*models.SecurityEvent, 0, len(f.rows))
	for i := len(f.rows) - 1; i >= 0; i-- {
		if f.rows[i].UserUUID == userUUID {
			out = append(out, f.rows[i])
			if len(out) >= limit {
				break
			}
		}
	}
	return out, nil
}

func (f *fakeSecurityEventRepo) ListByUserPaged(_ context.Context, userUUID string, offset, limit int, since *time.Time) ([]*models.SecurityEvent, int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	matches := make([]*models.SecurityEvent, 0, len(f.rows))
	for i := len(f.rows) - 1; i >= 0; i-- {
		if f.rows[i].UserUUID != userUUID {
			continue
		}
		if since != nil && !since.IsZero() && f.rows[i].Timestamp.Before(*since) {
			continue
		}
		matches = append(matches, f.rows[i])
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

func (f *fakeSecurityEventRepo) DeleteAllByUser(_ context.Context, userUUID string) (int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	kept := f.rows[:0]
	var deleted int64
	for _, r := range f.rows {
		if r.UserUUID == userUUID {
			deleted++
			continue
		}
		kept = append(kept, r)
	}
	f.rows = kept
	return deleted, nil
}

// TestRecordSecurityEvent_PersistsToRepoWhenWired: the production path.
// The service must hand events to the wired repo without further
// transformation; the repo is the system of record for the audit log.
func TestRecordSecurityEvent_PersistsToRepoWhenWired(t *testing.T) {
	t.Parallel()
	repo := &fakeSecurityEventRepo{}
	svc := &authService{securityEventRepo: repo}

	err := svc.RecordSecurityEvent(context.Background(), &models.SecurityEvent{
		UserUUID:  "u-1",
		EventType: "admin_oauth_unlink",
		IPAddress: "10.0.0.1",
		Success:   true,
	})
	if err != nil {
		t.Fatalf("RecordSecurityEvent: %v", err)
	}
	if len(repo.rows) != 1 {
		t.Fatalf("expected 1 row in repo, got %d", len(repo.rows))
	}
	if repo.rows[0].EventType != "admin_oauth_unlink" {
		t.Errorf("EventType lost in flight: %+v", repo.rows[0])
	}
	if repo.rows[0].IPAddress != "10.0.0.1" {
		t.Errorf("IPAddress lost in flight: %+v", repo.rows[0])
	}
}

// TestRecordSecurityEvent_RejectsNilEvent: a nil event is a programmer
// error — never silently dropped, always surfaced.
func TestRecordSecurityEvent_RejectsNilEvent(t *testing.T) {
	t.Parallel()
	repo := &fakeSecurityEventRepo{}
	svc := &authService{securityEventRepo: repo}

	if err := svc.RecordSecurityEvent(context.Background(), nil); err == nil {
		t.Fatal("expected error for nil event")
	}
	if len(repo.rows) != 0 {
		t.Errorf("nil event should not have inserted a row")
	}
}

// TestGetSecurityEvents_RoundTripsThroughRepo: writes then reads back,
// verifies the newest-first ordering and limit.
func TestGetSecurityEvents_RoundTripsThroughRepo(t *testing.T) {
	t.Parallel()
	repo := &fakeSecurityEventRepo{}
	svc := &authService{securityEventRepo: repo}

	for i, evt := range []string{"login", "password_reconfirm", "admin_oauth_unlink"} {
		if err := svc.RecordSecurityEvent(context.Background(), &models.SecurityEvent{
			UserUUID:  "u-1",
			EventType: evt,
			Timestamp: time.Date(2026, 5, 1+i, 0, 0, 0, 0, time.UTC),
		}); err != nil {
			t.Fatalf("seed %s: %v", evt, err)
		}
	}

	events, err := svc.GetSecurityEvents(context.Background(), "u-1", 10)
	if err != nil {
		t.Fatalf("GetSecurityEvents: %v", err)
	}
	if len(events) != 3 {
		t.Fatalf("want 3 events, got %d", len(events))
	}
	// Newest first.
	if events[0].EventType != "admin_oauth_unlink" {
		t.Errorf("expected newest first, got %q", events[0].EventType)
	}
	if events[2].EventType != "login" {
		t.Errorf("expected oldest last, got %q", events[2].EventType)
	}
}
