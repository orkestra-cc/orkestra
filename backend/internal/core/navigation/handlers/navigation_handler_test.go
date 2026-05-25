package handlers

import (
	"context"
	"errors"
	"testing"

	"github.com/orkestra/backend/internal/core/navigation/models"
	"github.com/orkestra/backend/internal/testkit"
)

// stubNavigationService captures the role argument so the handler's
// extraction + fallback logic is observable in tests.
type stubNavigationService struct {
	gotRole string
	resp    *models.NavigationResponse
	err     error
}

func (s *stubNavigationService) GetNavigationForUser(_ context.Context, userRole string) (*models.NavigationResponse, error) {
	s.gotRole = userRole
	if s.err != nil {
		return nil, s.err
	}
	if s.resp != nil {
		return s.resp, nil
	}
	return &models.NavigationResponse{UserRole: userRole}, nil
}

func TestGetNavigation_UsesRoleFromContext(t *testing.T) {
	stub := &stubNavigationService{}
	h := NewNavigationHandler(stub)

	ctx := testkit.NewIdentity("u1", "u1@example.com", "developer").
		ContextFor(context.Background(), "")

	resp, err := h.GetNavigation(ctx, &GetNavigationRequest{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if stub.gotRole != "developer" {
		t.Errorf("service called with role %q, want %q", stub.gotRole, "developer")
	}
	if resp.Body.UserRole != "developer" {
		t.Errorf("response UserRole = %q, want %q", resp.Body.UserRole, "developer")
	}
}

func TestGetNavigation_FallsBackToGuestWhenRoleMissing(t *testing.T) {
	stub := &stubNavigationService{}
	h := NewNavigationHandler(stub)

	// Bare context — no system role populated.
	_, err := h.GetNavigation(context.Background(), &GetNavigationRequest{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if stub.gotRole != "guest" {
		t.Errorf("missing role should default to %q, got %q", "guest", stub.gotRole)
	}
}

func TestGetNavigation_PropagatesServiceError(t *testing.T) {
	want := errors.New("downstream failure")
	stub := &stubNavigationService{err: want}
	h := NewNavigationHandler(stub)

	ctx := testkit.NewIdentity("u1", "u1@example.com", "administrator").
		ContextFor(context.Background(), "")
	_, err := h.GetNavigation(ctx, &GetNavigationRequest{})
	if !errors.Is(err, want) {
		t.Errorf("error = %v, want %v", err, want)
	}
}

func TestGetNavigation_ReturnsBodyVerbatim(t *testing.T) {
	canned := &models.NavigationResponse{
		Groups:     []models.RouteGroup{{Label: "Admin", Children: []models.NavItem{{Name: "Users", To: "/admin/users", Active: true}}}},
		Realms:     []models.NavRealm{{Key: "platform", Label: "Administration", Sections: []models.NavSection{{Label: "Admin", Children: []models.NavItem{{Name: "Users", To: "/admin/users", Active: true}}}}}},
		UserRole:   "administrator",
		TenantKind: "internal",
		CacheKey:   "nav:administrator:internal",
		ExpiresIn:  300,
	}
	stub := &stubNavigationService{resp: canned}
	h := NewNavigationHandler(stub)

	ctx := testkit.NewIdentity("u1", "u1@example.com", "administrator").
		ContextFor(context.Background(), "")
	resp, err := h.GetNavigation(ctx, &GetNavigationRequest{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Body.CacheKey != canned.CacheKey {
		t.Errorf("CacheKey = %q, want %q", resp.Body.CacheKey, canned.CacheKey)
	}
	if resp.Body.ExpiresIn != canned.ExpiresIn {
		t.Errorf("ExpiresIn = %d, want %d", resp.Body.ExpiresIn, canned.ExpiresIn)
	}
	if len(resp.Body.Realms) != 1 || resp.Body.Realms[0].Key != "platform" {
		t.Errorf("Realms not forwarded verbatim: %+v", resp.Body.Realms)
	}
}
