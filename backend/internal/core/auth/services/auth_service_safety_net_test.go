package services

// Safety-net tests pinning the *current* behaviour of three methods that the
// upcoming refactor will mutate:
//
//   - AddOAuthLink: stub returning a "not implemented" error — Phase 1.2 deletes it
//     entirely (the live link flow lives in self_user_auth_handler).
//   - GetOAuthLinks: hardcodes RequiresMFA=false ignoring real MFA state —
//     Phase 1.3 computes it from MFAFactorRepo.
//   - RecordSecurityEvent / GetSecurityEvents: no-op + empty list —
//     Phase 2.1 replaces with real persistence.
//
// When those phases land, the failing tests in this file are the signal to
// delete or update — they exist precisely to make sure the refactor does
// what we think it does.

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/orkestra/backend/internal/core/auth/models"
	userModels "github.com/orkestra/backend/internal/core/user/models"
)

// TestAddOAuthLink_StubReturnsNotImplemented locks the current placeholder
// behaviour. Phase 1.2 deletes both the method and this test.
func TestAddOAuthLink_StubReturnsNotImplemented(t *testing.T) {
	t.Parallel()
	svc := &authService{userService: newAdminUnlinkUserFake()}

	err := svc.AddOAuthLink(context.Background(), "u1", models.LinkOAuthProviderInput{
		Provider: models.OAuthProviderGoogle,
		Code:     "fake-code",
	})
	if err == nil {
		t.Fatal("AddOAuthLink stub must return an error; the live flow is /me/oauth/link/{provider}")
	}
	// The error message names the provider — keep the assertion loose so
	// Phase 1.2's deletion is the only place this contract changes.
	if !strings.Contains(err.Error(), "not yet implemented") {
		t.Errorf("error %q does not advertise the placeholder; live wiring may have been added without removing the stub", err)
	}
}

// TestGetOAuthLinks_BaselineReturnsLinksWithoutMFAFlag captures the current
// (buggy) RequiresMFA=false hardcoding. Phase 1.3 replaces it with a real
// computation, after which this test must be UPDATED, not deleted — we still
// want coverage that the field reflects reality.
func TestGetOAuthLinks_BaselineReturnsLinksWithoutMFAFlag(t *testing.T) {
	t.Parallel()
	fake := newAdminUnlinkUserFake()
	fake.seed(&userModels.User{
		UUID:         "u-multi",
		Role:         "administrator", // role that *should* trigger MFA
		PasswordHash: "x",
		OAuthLinks: []userModels.OAuthLink{
			{Provider: "google", ProviderID: "g-1", Email: "u@x.com", IsActive: true, IsPrimary: true},
			{Provider: "github", ProviderID: "gh-1", Email: "u@x.com", IsActive: true},
		},
	})
	svc := &authService{userService: fake}

	resp, err := svc.GetOAuthLinks(context.Background(), "u-multi")
	if err != nil {
		t.Fatalf("GetOAuthLinks: %v", err)
	}
	if len(resp.Links) != 2 {
		t.Errorf("Links count = %d, want 2", len(resp.Links))
	}
	if !resp.CanUnlink {
		t.Errorf("CanUnlink should be true when 2+ links present")
	}
	// Baseline bug: RequiresMFA always false. Phase 1.3 changes this to
	// reflect the user's enrolled factors + role policy.
	if resp.RequiresMFA {
		t.Errorf("baseline contract: RequiresMFA should currently be false (the field is unwired) — has Phase 1.3 already landed?")
	}
}

// TestGetOAuthLinks_SingleLinkCannotUnlink: with only one link, CanUnlink is
// false. This is the only piece of GetOAuthLinks' logic that Phase 1.3 does
// NOT touch.
func TestGetOAuthLinks_SingleLinkCannotUnlink(t *testing.T) {
	t.Parallel()
	fake := newAdminUnlinkUserFake()
	fake.seed(&userModels.User{
		UUID: "u-sole",
		OAuthLinks: []userModels.OAuthLink{
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

// TestRecordSecurityEvent_CurrentlyNoOpReturnsNil locks the current placeholder.
// Phase 2.1 implements real persistence; this test gets rewritten to assert
// the event landed in auth_security_events.
func TestRecordSecurityEvent_CurrentlyNoOpReturnsNil(t *testing.T) {
	t.Parallel()
	svc := &authService{}

	err := svc.RecordSecurityEvent(context.Background(), &models.SecurityEvent{
		ID:        "ev-1",
		UserUUID:  "u-1",
		EventType: "test_event",
		Timestamp: time.Now(),
	})
	if err != nil {
		t.Errorf("placeholder must return nil, got %v", err)
	}
}

// TestGetSecurityEvents_CurrentlyReturnsEmptySlice — same as above, the
// reader-side companion that Phase 2.3 replaces with a Mongo query.
func TestGetSecurityEvents_CurrentlyReturnsEmptySlice(t *testing.T) {
	t.Parallel()
	svc := &authService{}

	events, err := svc.GetSecurityEvents(context.Background(), "u-1", 100)
	if err != nil {
		t.Errorf("placeholder must return nil error, got %v", err)
	}
	if len(events) != 0 {
		t.Errorf("placeholder must return an empty slice, got %d entries", len(events))
	}
}
