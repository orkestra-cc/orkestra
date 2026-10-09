package services

// While the OAuth identity unique index is missing (migration 0010 not run
// on this environment), every OAuth entry point answers
// ErrOAuthStoreUnavailable BEFORE touching the store: the ownership-first
// flow detects a conflict by the duplicate key that index produces, and
// running it without the index would let two users silently share an
// identity again.

import (
	"context"
	"errors"
	"testing"

	"github.com/orkestra/backend/internal/core/auth/models"
	"github.com/orkestra/backend/internal/core/auth/repository"
	"github.com/orkestra/backend/pkg/sdk/iface"
)

// touchedStore panics on any read or write: the gate must refuse before
// the store is consulted at all.
type touchedStore struct {
	repository.OAuthProviderRepository
}

func (touchedStore) GetByProviderAndID(context.Context, models.OAuthProvider, string) (*models.OAuthProviderDoc, error) {
	panic("the store must not be touched while the OAuth store is degraded")
}

func (touchedStore) GetByUserUUID(context.Context, string) ([]*models.OAuthProviderDoc, error) {
	panic("the store must not be touched while the OAuth store is degraded")
}

type touchedUsers struct{ iface.UserProvider }

func (touchedUsers) GetUserByID(context.Context, string) (*iface.User, error) {
	panic("the user store must not be touched while the OAuth store is degraded")
}

func (touchedUsers) GetUserByEmail(context.Context, string) (*iface.UserManagementResponse, error) {
	panic("the user store must not be touched while the OAuth store is degraded")
}

func degradedService() *authService {
	return &authService{
		oauthProviderRepo: touchedStore{},
		userService:       touchedUsers{},
		oauthStoreGate:    func() bool { return true },
	}
}

func TestOAuthStoreGate_CallbackRefusesWhileDegraded(t *testing.T) {
	svc := degradedService()
	_, err := svc.HandleOAuthCallbackWithLinking(context.Background(), models.OAuthProviderGoogle,
		map[string]interface{}{"email": "v@example.com", "provider_id": "1234", "email_verified": true}, nil, nil, nil)
	if !errors.Is(err, ErrOAuthStoreUnavailable) {
		t.Fatalf("got %v, want ErrOAuthStoreUnavailable", err)
	}
}

func TestOAuthStoreGate_SelfLinkRefusesWhileDegraded(t *testing.T) {
	svc := degradedService()
	err := svc.SelfLinkOAuthFromCallback(context.Background(), "u-1", iface.OAuthProvider("google"),
		map[string]interface{}{"email": "v@example.com", "provider_id": "1234", "email_verified": true}, nil)
	if !errors.Is(err, ErrOAuthStoreUnavailable) {
		t.Fatalf("got %v, want ErrOAuthStoreUnavailable", err)
	}
}

// An unwired gate is open: a fork that never wires the boot check keeps
// today's behaviour, and the refusal needs a positive signal.
func TestOAuthStoreGate_NilGateIsOpen(t *testing.T) {
	svc := &authService{oauthProviderRepo: touchedStore{}, userService: touchedUsers{}}
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("with no gate the callback must reach the store (the fake panics there)")
		}
	}()
	_, _ = svc.HandleOAuthCallbackWithLinking(context.Background(), models.OAuthProviderGoogle,
		map[string]interface{}{"email": "v@example.com", "provider_id": "1234", "email_verified": true}, nil, nil, nil)
}
