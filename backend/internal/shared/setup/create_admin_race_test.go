package setup

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"testing"

	"github.com/danielgtaylor/huma/v2"
	authServices "github.com/orkestra/backend/internal/core/auth/services"
	"github.com/orkestra/backend/internal/shared/config"
)

// The concurrent-first-install race: both requests see zero users, one wins
// the first-admin claim, and the loser's RegisterInitialAdmin fails with
// ErrInitialAdminExists. That is the same fact as "a user already exists",
// so the service must report it as ErrAlreadyCompleted.
func TestCreateInitialAdmin_LostFirstAdminRace_ReturnsErrAlreadyCompleted(t *testing.T) {
	admin := &stubAdmin{err: fmt.Errorf("register: %w", authServices.ErrInitialAdminExists)}
	svc := NewService(&stubUsers{count: 0}, admin, &fakeFinalizationStore{}, nil, nil, nil, nil)

	_, err := svc.CreateInitialAdmin(context.Background(), "second@example.com", "verysecretpw!", "Second Admin", "10.0.0.2")

	if !errors.Is(err, ErrAlreadyCompleted) {
		t.Fatalf("err = %v, want ErrAlreadyCompleted", err)
	}
}

// ... and the wire answer is the same 409 the sequential case gets, not the
// generic 400 "Could not create administrator account".
func TestCreateAdminHandler_LostFirstAdminRace_Returns409(t *testing.T) {
	admin := &stubAdmin{err: authServices.ErrInitialAdminExists}
	svc := NewService(&stubUsers{count: 0}, admin, &fakeFinalizationStore{}, nil, nil, nil, nil)
	h := NewHandler(svc, config.CookieConfig{})

	req := &CreateAdminRequest{}
	req.Body.Email, req.Body.Password, req.Body.FullName = "second@example.com", "verysecretpw!", "Second Admin"
	_, err := h.CreateAdmin(context.Background(), req)

	var se huma.StatusError
	if !errors.As(err, &se) {
		t.Fatalf("error is not a huma.StatusError: %v", err)
	}
	if se.GetStatus() != http.StatusConflict {
		t.Errorf("status = %d, want %d", se.GetStatus(), http.StatusConflict)
	}
}
